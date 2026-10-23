"""Real GDAL regression for the independent stream_link_d8 Hosted fixture."""
import importlib
import json
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

@pytest.fixture(scope='module')
def stream_link_d8_persistence(tmp_path_factory):
    module=importlib.import_module('business.scripts.online-raster-stream-link-d8-fixture')
    scene=importlib.import_module('scripts.test.raster-stream-link-d8-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('stream_link_d8-export');(root/'outputs').mkdir()
    # The real NFS lifecycle always creates this ownership marker.
    (root/'.addp-online-owner').write_text('stream-link-d8-123-1')
    initial=module.seed(root)
    for name in module.TARGETS:
        definition=scene.definition({n:n for n in module.SOURCES},'parent',name)
        for task in definition['tasks']:
            params=task['params']
            if task['operator']=='raster_load':
                path=root/params.pop('locator')
                params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{
                    'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(path)}}}
            elif task['operator']=='raster_save':
                params.pop('target_parent_locator');target=root/'outputs'/params.pop('target_name')
                mode=params.pop('write_mode')
                params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{
                    'kind':'file','format':'tiff','name':target.name,'write_mode':mode,
                    'access':{'method':'mounted_path','path':str(target)}}}
        result=execute_workflow(definition)
        assert result['status']=='success',result
        artifact=json.loads(result['final_result'])
        assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    return module,root,initial



def test_stream_link_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels(stream_link_d8_persistence):
    module,root,initial=stream_link_d8_persistence
    actual=module.verify(root,list(module.TARGETS))
    assert actual['sources']==initial['sources'] and len(actual['targets'])==6
    for name,fact in actual['targets'].items():
        assert all(fact[key] for key in ('full_pixel_and_mask_verification','cog_valid','has_overviews','stream_links_verified','start_order_verified','unique_coverage_verified','stream_closure_verified','confluence_verified','zero_background_verified'))
        if name in ('invalid-links.cog.tif','empty-links.cog.tif'):
            assert fact['river_pixels']==fact['link_count']==0 and fact['link_ids']==fact['link_pixel_counts']==fact['start_coordinates']==[]
            assert fact['zero_pixels']==fact['valid_pixels']
        else:assert fact['river_pixels']>0 and fact['zero_pixels']>0 and fact['link_count']>=1
        assert fact['size_bytes']==(root/'outputs'/name).stat().st_size>0
    fact=actual['targets']['links.cog.tif']
    assert fact['link_count']==15 and fact['river_pixels']==21 and fact['valid_pixels']==3628 and fact['zero_pixels']==3607
    assert fact['start_coordinates']==[list(cell) for cell in module.DIRECT_STARTS]
    assert fact['link_ids']==list(range(1,16))
    assert fact['link_pixel_counts']==[1,2,2,1,1,1,1,1,1,1,3,3,1,1,1]
    expected=module.expected_pixels('links.cog.tif')
    assert expected[510,512]==8 and expected[512,512]==13 and np.all(expected[511,513:516]==11)
    assert np.all(expected[511,516:]==12) and np.isnan(expected[514,510])
    assert expected[508,40]==1 and expected[508,20]==3 and expected[508,21]==2


@pytest.mark.parametrize('corruption',['value','one_unit','fraction','seam','invalid_center','mask','crs','grid','dtype','nodata','scale','unit','overviews','overview_links','sidecar','source_sidecar','missing_owner','missing','extra'])
def test_stream_link_d8_persistence_oracle_rejects_corruption(stream_link_d8_persistence,tmp_path,corruption):
    import shutil
    module,original,_=stream_link_d8_persistence
    root=tmp_path/'changed';shutil.copytree(original,root);path=root/'outputs'/'links.cog.tif'
    if corruption in ('value','one_unit','fraction','seam','invalid_center','mask','crs','grid','nodata','scale','unit','overview_links'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
        band=ds.GetRasterBand(1)
        if corruption in ('value','one_unit','fraction','seam','invalid_center'):
            y,x=(100,100) if corruption=='invalid_center' else (511,512) if corruption=='seam' else (511,518)
            value=12.000001 if corruption=='fraction' else 13 if corruption=='one_unit' else 12345.
            band.WriteRaster(x,y,1,1,np.array([value],np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        elif corruption=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().Fill(255)
        elif corruption=='crs':ds.SetProjection('')
        elif corruption=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif corruption=='nodata':band.SetNoDataValue(-999)
        elif corruption=='unit':band.SetUnitType('metre')
        elif corruption=='overview_links':band.GetOverview(0).Fill(16)
        else:band.SetScale(2.)
        band=ds=None
    elif corruption in ('dtype','overviews'):
        alternate=root/'replacement.tif';ds=gdal.Translate(str(alternate),str(path),format='GTiff',outputType=gdal.GDT_Float32 if corruption=='dtype' else gdal.GDT_Float64);ds=None;alternate.replace(path)
    elif corruption=='sidecar':(root/'outputs'/'links.cog.tif.aux.xml').write_text('unexpected')
    elif corruption=='source_sidecar':(root/'directions.tif.aux.xml').write_text('unexpected')
    elif corruption=='missing_owner':(root/'.addp-online-owner').unlink()
    elif corruption=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_bytes(path.read_bytes())
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))


@pytest.mark.parametrize('problem',['break','mask_values','coverage','alignment','code','cycle','band','stream_band'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_stream_link_d8_persistence_invalid_parameters_fail_without_publication(stream_link_d8_persistence,tmp_path,problem,language):
    import shutil
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    module,original,initial=stream_link_d8_persistence;root=tmp_path/'failed';shutil.copytree(original,root)
    scene=importlib.import_module('scripts.test.raster-stream-link-d8-persistence-online')
    definition=scene.definition({n:n for n in module.SOURCES},'parent','links.cog.tif',invalid=problem,language=language)
    for task in definition['tasks']:
        params=task['params']
        if task['operator']=='raster_load':
            source=root/params.pop('locator');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(source)}}}
        elif task['operator']=='raster_save':
            params.pop('target_parent_locator');target=root/'outputs'/params.pop('target_name');mode=params.pop('write_mode')
            params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{'kind':'file','format':'tiff','name':target.name,'write_mode':mode,'access':{'method':'mounted_path','path':str(target)}}}
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner(language=language).execute(definition)
    assert caught.value.error_code=='EXECUTION_FAILED'
    assert str(caught.value)=='任务 links 执行失败: '+scene.MESSAGES[problem][language=='en']
    assert not target.exists() and module.verify(root,list(module.TARGETS))['sources']==initial['sources']


@pytest.mark.parametrize('fault',['confluence_upstream','topological_numbering','source_wrong','outlet_zero','background_nodata','invalid_zero','fraction','crossing_merge','terminal_source'])
def test_stream_link_d8_independent_oracle_rejects_wrong_numbering(fault):
    module=importlib.import_module('business.scripts.online-raster-stream-link-d8-fixture')
    actual=module.expected_pixels('links.cog.tif').copy()
    cell,value={'confluence_upstream':((511,513),8),'topological_numbering':((512,512),11),
        'source_wrong':((509,511),1),'outlet_zero':((511,518),0),'background_nodata':((509,50),np.nan),
        'invalid_zero':((100,100),0),'fraction':((511,518),12.000001),
        'crossing_merge':((508,20),2),'terminal_source':((508,40),4)}[fault]
    actual[cell]=value
    with pytest.raises(module.FixtureError):module.link_evidence(actual,'links.cog.tif')


def test_stream_link_d8_fixture_validity_zero_alpha_cross_block_and_both_selected_bands(stream_link_d8_persistence):
    module,root,_=stream_link_d8_persistence
    for name in ('directions.tif','streams.tif'):
        ds=gdal.Open(str(root/name));assert ds.RasterCount==4
        assert np.all(ds.GetRasterBand(1).ReadAsArray()==.25)
        assert np.all(ds.GetRasterBand(3).ReadAsArray()==-9999)
        assert ds.GetRasterBand(4).ReadAsArray()[511,515]==128
        assert ds.GetRasterBand(1).GetMaskBand().ReadAsArray()[514,510]==0
        ds=None
    assert module.expected_pixels('links.cog.tif')[509,50]==0
    assert module.expected_pixels('links.cog.tif')[511,515]==11
    with pytest.raises(module.FixtureError):module.seed(root)


def test_stream_link_d8_fixture_source_receipt_rejects_any_byte_change(stream_link_d8_persistence,tmp_path,monkeypatch):
    import shutil
    module,original,initial=stream_link_d8_persistence;root=tmp_path/'sources';shutil.copytree(original,root);secret=tmp_path/'secret';secret.mkdir()
    (secret/'stream-link-d8-source.json').write_text(json.dumps(initial));monkeypatch.setattr(module,'guard',lambda:secret);monkeypatch.setattr(module,'owned_root',lambda _:root)
    monkeypatch.setattr(module,'runtime_worker',lambda action,owned,names:module.verify(owned,names));monkeypatch.setattr(module.sys,'argv',['fixture','verify',*module.TARGETS])
    assert module.main('verify')['sources']==initial['sources']
    with (root/'streams.tif').open('ab') as stream:stream.write(b'changed source bytes')
    with pytest.raises(module.FixtureError,match='source file changed'):module.main('verify')


def test_stream_link_d8_fixture_preserves_exact_uint64_nodata_adjacent_invalid_value(stream_link_d8_persistence):
    module,root,_=stream_link_d8_persistence;ds=gdal.Open(str(root/'code.tif'));band=ds.GetRasterBand(2)
    assert band.DataType==gdal.GDT_UInt64 and band.GetNoDataValueAsUInt64()==2**53
    assert int(band.ReadAsArray()[509,100])==2**53+1 and int(band.ReadAsArray()[0,0])==2**53
    band=ds=None


def test_stream_link_d8_fixture_backward_count_and_threshold_have_independent_row_oracle():
    module=importlib.import_module('business.scripts.online-raster-stream-link-d8-fixture')
    directions=module.expected_directions('dem-links.cog.tif');counts=module.backward_oracle(directions)
    np.testing.assert_array_equal(counts[module.FIRST_ROW:,:],np.broadcast_to(np.arange(module.WIDTH),(7,module.WIDTH)))
    expected=np.where(np.isfinite(directions),0.,np.nan);expected[module.FIRST_ROW:,2:]=np.arange(1,module.HEIGHT-module.FIRST_ROW+1)[:,None]
    np.testing.assert_array_equal(module.expected_pixels('dem-links.cog.tif'),expected)


def test_stream_link_d8_fixture_fresh_verifier_keeps_prior_file_checks(stream_link_d8_persistence):
    module,root,initial=stream_link_d8_persistence
    for method in (module.expected_directions,module.expected_streams,module.expected_pixels):method.cache_clear()
    result=module.verify(root,list(module.TARGETS))
    assert result==module.verify(root,list(module.TARGETS)) and result['sources']==initial['sources']
