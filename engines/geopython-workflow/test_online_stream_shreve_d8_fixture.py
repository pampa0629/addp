"""Real GDAL regression for the independent stream_shreve_d8 Hosted fixture."""
import importlib
import json
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

@pytest.fixture(scope='module')
def stream_shreve_d8_persistence(tmp_path_factory):
    module=importlib.import_module('business.scripts.online-raster-stream-shreve-d8-fixture')
    scene=importlib.import_module('scripts.test.raster-stream-shreve-d8-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('stream_shreve_d8-export');(root/'outputs').mkdir()
    # The real NFS lifecycle always creates this ownership marker.
    (root/'.addp-online-owner').write_text('stream-shreve-d8-123-1')
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



def test_stream_shreve_d8_persistence_real_gdal_eight_cog_chains_and_complete_pixels(stream_shreve_d8_persistence):
    module,root,initial=stream_shreve_d8_persistence
    actual=module.verify(root,list(module.TARGETS))
    assert actual['sources']==initial['sources'] and len(actual['targets'])==8
    for name,fact in actual['targets'].items():
        assert all(fact[key] for key in ('full_pixel_and_mask_verification','cog_valid','has_overviews','independent_method_oracle_verified','stream_closure_verified','confluence_verified','zero_background_verified'))
        if name=='invalid-shreve.cog.tif':assert fact['valid_pixels']==fact['river_pixels']==fact['zero_pixels']==0 and fact['max_order'] is None
        elif name=='empty-shreve.cog.tif':assert fact['river_pixels']==0 and fact['zero_pixels']==fact['valid_pixels']>0 and fact['max_order']==0
        else:assert fact['river_pixels']>0 and fact['zero_pixels']>0 and fact['max_order']>=1
        assert fact['size_bytes']==(root/'outputs'/name).stat().st_size>0
    fact=actual['targets']['shreve.cog.tif']
    assert fact['river_pixels']==18 and fact['valid_pixels']==3628 and fact['zero_pixels']==3610
    assert fact['headwater_count']==8 and fact['outlet_coordinates']==[[508,20],[508,21],[511,518]]
    assert fact['outlet_magnitudes']==[1,1,6] and fact['headwater_conservation_verified'] is True
    expected=module.expected_pixels('shreve.cog.tif')
    assert expected[510,512]==expected[512,512]==2
    assert np.all(expected[511,513:516]==5) and np.all(expected[511,516:]==6)
    assert expected[511,515]==5 and np.isnan(expected[514,510])
    assert expected[508,20]==expected[508,21]==expected[509,20]==expected[509,21]==1
    assert fact['max_order']==6 and actual['default_strahler_equivalent_verified'] is True
    for name in module.TARGETS:
        fact=actual['targets'][name];method=module.CASES[name][5]
        assert fact['method_parameter']==method and fact['method']==(method or 'strahler')
        assert fact['headwater_conservation_verified' if method=='shreve' else 'strahler_verified'] is True
    default=gdal.Open(str(root/'outputs'/'default-strahler.cog.tif'))
    explicit=gdal.Open(str(root/'outputs'/'explicit-strahler.cog.tif'))
    np.testing.assert_array_equal(default.ReadAsArray(),explicit.ReadAsArray())
    assert default.ReadAsArray()[511,518]==3 and expected[511,518]==6
    default=explicit=None


@pytest.mark.parametrize('corruption',['value','one_unit','fraction','seam','invalid_center','mask','crs','grid','dtype','nodata','scale','unit','overviews','overview_grade','sidecar','source_sidecar','missing_owner','missing','extra'])
def test_stream_shreve_d8_persistence_oracle_rejects_corruption(stream_shreve_d8_persistence,tmp_path,corruption):
    import shutil
    module,original,_=stream_shreve_d8_persistence
    root=tmp_path/'changed';shutil.copytree(original,root);path=root/'outputs'/'shreve.cog.tif'
    if corruption in ('value','one_unit','fraction','seam','invalid_center','mask','crs','grid','nodata','scale','unit','overview_grade'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
        band=ds.GetRasterBand(1)
        if corruption in ('value','one_unit','fraction','seam','invalid_center'):
            y,x=(100,100) if corruption=='invalid_center' else (511,512) if corruption=='seam' else (511,518)
            value=6.000001 if corruption=='fraction' else 7 if corruption=='one_unit' else 12345.
            band.WriteRaster(x,y,1,1,np.array([value],np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        elif corruption=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().Fill(255)
        elif corruption=='crs':ds.SetProjection('')
        elif corruption=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif corruption=='nodata':band.SetNoDataValue(-999)
        elif corruption=='unit':band.SetUnitType('metre')
        elif corruption=='overview_grade':band.GetOverview(0).Fill(7)
        else:band.SetScale(2.)
        band=ds=None
    elif corruption in ('dtype','overviews'):
        alternate=root/'replacement.tif';ds=gdal.Translate(str(alternate),str(path),format='GTiff',outputType=gdal.GDT_Float32 if corruption=='dtype' else gdal.GDT_Float64);ds=None;alternate.replace(path)
    elif corruption=='sidecar':(root/'outputs'/'shreve.cog.tif.aux.xml').write_text('unexpected')
    elif corruption=='source_sidecar':(root/'directions.tif.aux.xml').write_text('unexpected')
    elif corruption=='missing_owner':(root/'.addp-online-owner').unlink()
    elif corruption=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_bytes(path.read_bytes())
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))


@pytest.mark.parametrize('problem',['break','mask_values','coverage','alignment','code','cycle','band','stream_band','method'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_stream_shreve_d8_persistence_invalid_parameters_fail_without_publication(stream_shreve_d8_persistence,tmp_path,problem,language):
    import shutil
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    module,original,initial=stream_shreve_d8_persistence;root=tmp_path/'failed';shutil.copytree(original,root)
    scene=importlib.import_module('scripts.test.raster-stream-shreve-d8-persistence-online')
    definition=scene.definition({n:n for n in module.SOURCES},'parent','shreve.cog.tif',invalid=problem,language=language)
    for task in definition['tasks']:
        params=task['params']
        if task['operator']=='raster_load':
            source=root/params.pop('locator');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(source)}}}
        elif task['operator']=='raster_save':
            params.pop('target_parent_locator');target=root/'outputs'/params.pop('target_name');mode=params.pop('write_mode')
            params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{'kind':'file','format':'tiff','name':target.name,'write_mode':mode,'access':{'method':'mounted_path','path':str(target)}}}
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner(language=language).execute(definition)
    assert caught.value.error_code=='EXECUTION_FAILED'
    assert str(caught.value)=='任务 grade 执行失败: '+scene.MESSAGES[problem][language=='en']
    assert not target.exists() and module.verify(root,list(module.TARGETS))['sources']==initial['sources']


@pytest.mark.parametrize('fault',['max_instead_of_sum','missing_tributary','source_two','outlet_zero','background_nodata','invalid_zero','fraction','crossing_merge','nonriver_donor'])
def test_stream_shreve_d8_independent_oracle_rejects_wrong_magnitude(fault):
    module=importlib.import_module('business.scripts.online-raster-stream-shreve-d8-fixture')
    actual=module.expected_pixels('shreve.cog.tif').copy()
    cell,value={'max_instead_of_sum':((511,513),3),'missing_tributary':((511,516),5),
        'source_two':((509,511),2),'outlet_zero':((511,518),0),'background_nodata':((509,50),np.nan),
        'invalid_zero':((100,100),0),'fraction':((511,518),6.000001),
        'crossing_merge':((508,20),2),'nonriver_donor':((510,512),3)}[fault]
    actual[cell]=value
    with pytest.raises(module.FixtureError):module.order_evidence(actual,'shreve.cog.tif')


def test_stream_shreve_d8_fixture_validity_zero_alpha_cross_block_and_both_selected_bands(stream_shreve_d8_persistence):
    module,root,_=stream_shreve_d8_persistence
    for name in ('directions.tif','streams.tif'):
        ds=gdal.Open(str(root/name));assert ds.RasterCount==4
        assert np.all(ds.GetRasterBand(1).ReadAsArray()==.25)
        assert np.all(ds.GetRasterBand(3).ReadAsArray()==-9999)
        assert ds.GetRasterBand(4).ReadAsArray()[511,515]==128
        assert ds.GetRasterBand(1).GetMaskBand().ReadAsArray()[514,510]==0
        ds=None
    assert module.expected_pixels('shreve.cog.tif')[509,50]==0
    assert module.expected_pixels('shreve.cog.tif')[511,515]==5
    with pytest.raises(module.FixtureError):module.seed(root)


def test_stream_shreve_d8_fixture_source_receipt_rejects_any_byte_change(stream_shreve_d8_persistence,tmp_path,monkeypatch):
    import shutil
    module,original,initial=stream_shreve_d8_persistence;root=tmp_path/'sources';shutil.copytree(original,root);secret=tmp_path/'secret';secret.mkdir()
    (secret/'stream-shreve-d8-source.json').write_text(json.dumps(initial));monkeypatch.setattr(module,'guard',lambda:secret);monkeypatch.setattr(module,'owned_root',lambda _:root)
    monkeypatch.setattr(module,'runtime_worker',lambda action,owned,names:module.verify(owned,names));monkeypatch.setattr(module.sys,'argv',['fixture','verify',*module.TARGETS])
    assert module.main('verify')['sources']==initial['sources']
    with (root/'streams.tif').open('ab') as stream:stream.write(b'changed source bytes')
    with pytest.raises(module.FixtureError,match='source file changed'):module.main('verify')


def test_stream_shreve_d8_fixture_preserves_exact_uint64_nodata_adjacent_invalid_value(stream_shreve_d8_persistence):
    module,root,_=stream_shreve_d8_persistence;ds=gdal.Open(str(root/'code.tif'));band=ds.GetRasterBand(2)
    assert band.DataType==gdal.GDT_UInt64 and band.GetNoDataValueAsUInt64()==2**53
    assert int(band.ReadAsArray()[509,100])==2**53+1 and int(band.ReadAsArray()[0,0])==2**53
    band=ds=None


def test_stream_shreve_d8_fixture_backward_count_and_threshold_have_independent_row_oracle():
    module=importlib.import_module('business.scripts.online-raster-stream-shreve-d8-fixture')
    directions=module.expected_directions('dem-shreve.cog.tif');counts=module.backward_oracle(directions)
    np.testing.assert_array_equal(counts[module.FIRST_ROW:,:],np.broadcast_to(np.arange(module.WIDTH),(7,module.WIDTH)))
    expected=np.where(np.isfinite(directions),0.,np.nan);expected[module.FIRST_ROW:,2:]=1
    np.testing.assert_array_equal(module.expected_pixels('dem-shreve.cog.tif'),expected)


def test_stream_shreve_d8_fixture_fresh_verifier_keeps_prior_file_checks(stream_shreve_d8_persistence):
    module,root,initial=stream_shreve_d8_persistence
    for method in (module.expected_directions,module.expected_streams,module.expected_pixels):method.cache_clear()
    result=module.verify(root,list(module.TARGETS))
    assert result==module.verify(root,list(module.TARGETS)) and result['sources']==initial['sources']


def test_stream_shreve_d8_fixture_detects_corrupted_strahler_comparison(stream_shreve_d8_persistence,tmp_path):
    import shutil
    module,original,_=stream_shreve_d8_persistence;root=tmp_path/'pair';shutil.copytree(original,root)
    ds=gdal.OpenEx(str(root/'outputs'/'explicit-strahler.cog.tif'),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
    ds.GetRasterBand(1).WriteRaster(518,511,1,1,np.array([6.],np.float64).tobytes(),buf_type=gdal.GDT_Float64);ds=None
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))
