"""Real GDAL regression for the independent stream_order_d8 Hosted fixture."""
import importlib
import json
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

@pytest.fixture(scope='module')
def stream_order_d8_persistence(tmp_path_factory):
    module=importlib.import_module('business.scripts.online-raster-stream-order-d8-fixture')
    scene=importlib.import_module('scripts.test.raster-stream-order-d8-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('stream_order_d8-export');(root/'outputs').mkdir()
    # The real NFS lifecycle always creates this ownership marker.
    (root/'.addp-online-owner').write_text('stream-order-d8-123-1')
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



def test_stream_order_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels(stream_order_d8_persistence):
    module,root,initial=stream_order_d8_persistence
    actual=module.verify(root,list(module.TARGETS))
    assert actual['sources']==initial['sources'] and len(actual['targets'])==6
    for name,fact in actual['targets'].items():
        assert all(fact[key] for key in ('full_pixel_and_mask_verification','cog_valid','has_overviews','strahler_verified','stream_closure_verified','confluence_verified','zero_background_verified'))
        if name=='invalid-order.cog.tif':assert fact['valid_pixels']==fact['river_pixels']==fact['zero_pixels']==0 and fact['max_order'] is None
        elif name=='empty-order.cog.tif':assert fact['river_pixels']==0 and fact['zero_pixels']==fact['valid_pixels']>0 and fact['max_order']==0
        else:assert fact['river_pixels']>0 and fact['zero_pixels']>0 and fact['max_order']>=1
        assert fact['size_bytes']==(root/'outputs'/name).stat().st_size>0
    expected=module.expected_pixels('order.cog.tif')
    assert expected[510,512]==expected[512,512]==2
    assert np.all(expected[511,513:]==3) and expected[511,516]==3
    assert expected[511,515]==3 and np.isnan(expected[514,510])
    assert actual['targets']['order.cog.tif']['max_order']==3


@pytest.mark.parametrize('corruption',['value','one_unit','fraction','seam','invalid_center','mask','crs','grid','dtype','nodata','scale','unit','overviews','overview_grade','sidecar','source_sidecar','missing_owner','missing','extra'])
def test_stream_order_d8_persistence_oracle_rejects_corruption(stream_order_d8_persistence,tmp_path,corruption):
    import shutil
    module,original,_=stream_order_d8_persistence
    root=tmp_path/'changed';shutil.copytree(original,root);path=root/'outputs'/'order.cog.tif'
    if corruption in ('value','one_unit','fraction','seam','invalid_center','mask','crs','grid','nodata','scale','unit','overview_grade'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
        band=ds.GetRasterBand(1)
        if corruption in ('value','one_unit','fraction','seam','invalid_center'):
            y,x=(100,100) if corruption=='invalid_center' else (511,512) if corruption=='seam' else (511,518)
            value=3.000001 if corruption=='fraction' else 2 if corruption=='one_unit' else 12345.
            band.WriteRaster(x,y,1,1,np.array([value],np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        elif corruption=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().Fill(255)
        elif corruption=='crs':ds.SetProjection('')
        elif corruption=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif corruption=='nodata':band.SetNoDataValue(-999)
        elif corruption=='unit':band.SetUnitType('metre')
        elif corruption=='overview_grade':band.GetOverview(0).Fill(4)
        else:band.SetScale(2.)
        band=ds=None
    elif corruption in ('dtype','overviews'):
        alternate=root/'replacement.tif';ds=gdal.Translate(str(alternate),str(path),format='GTiff',outputType=gdal.GDT_Float32 if corruption=='dtype' else gdal.GDT_Float64);ds=None;alternate.replace(path)
    elif corruption=='sidecar':(root/'outputs'/'order.cog.tif.aux.xml').write_text('unexpected')
    elif corruption=='source_sidecar':(root/'directions.tif.aux.xml').write_text('unexpected')
    elif corruption=='missing_owner':(root/'.addp-online-owner').unlink()
    elif corruption=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_bytes(path.read_bytes())
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))


@pytest.mark.parametrize('problem',['break','mask_values','coverage','alignment','code','cycle','band','stream_band'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_stream_order_d8_persistence_invalid_parameters_fail_without_publication(stream_order_d8_persistence,tmp_path,problem,language):
    import shutil
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    module,original,initial=stream_order_d8_persistence;root=tmp_path/'failed';shutil.copytree(original,root)
    scene=importlib.import_module('scripts.test.raster-stream-order-d8-persistence-online')
    definition=scene.definition({n:n for n in module.SOURCES},'parent','order.cog.tif',invalid=problem,language=language)
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


@pytest.mark.parametrize('fault',['highest_not_incremented','lower_increments','source_two','outlet_zero','background_nodata','invalid_zero','fraction'])
def test_stream_order_d8_independent_oracle_rejects_wrong_grading(fault):
    module=importlib.import_module('business.scripts.online-raster-stream-order-d8-fixture')
    actual=module.expected_pixels('order.cog.tif').copy()
    if fault=='highest_not_incremented':actual[511,513]=2
    elif fault=='lower_increments':actual[511,516]=4
    elif fault=='source_two':actual[509,511]=2
    elif fault=='outlet_zero':actual[511,518]=0
    elif fault=='background_nodata':actual[509,50]=np.nan
    elif fault=='invalid_zero':actual[100,100]=0
    else:actual[511,518]=3.000001
    with pytest.raises(module.FixtureError):module.order_evidence(actual,'order.cog.tif')


def test_stream_order_d8_fixture_validity_zero_alpha_cross_block_and_both_selected_bands(stream_order_d8_persistence):
    module,root,_=stream_order_d8_persistence
    for name in ('directions.tif','streams.tif'):
        ds=gdal.Open(str(root/name));assert ds.RasterCount==4
        assert np.all(ds.GetRasterBand(1).ReadAsArray()==.25)
        assert np.all(ds.GetRasterBand(3).ReadAsArray()==-9999)
        assert ds.GetRasterBand(4).ReadAsArray()[511,515]==128
        assert ds.GetRasterBand(1).GetMaskBand().ReadAsArray()[514,510]==0
        ds=None
    assert module.expected_pixels('order.cog.tif')[509,50]==0
    assert module.expected_pixels('order.cog.tif')[511,515]==3
    with pytest.raises(module.FixtureError):module.seed(root)


def test_stream_order_d8_fixture_source_receipt_rejects_any_byte_change(stream_order_d8_persistence,tmp_path,monkeypatch):
    import shutil
    module,original,initial=stream_order_d8_persistence;root=tmp_path/'sources';shutil.copytree(original,root);secret=tmp_path/'secret';secret.mkdir()
    (secret/'stream-order-d8-source.json').write_text(json.dumps(initial));monkeypatch.setattr(module,'guard',lambda:secret);monkeypatch.setattr(module,'owned_root',lambda _:root)
    monkeypatch.setattr(module,'runtime_worker',lambda action,owned,names:module.verify(owned,names));monkeypatch.setattr(module.sys,'argv',['fixture','verify',*module.TARGETS])
    assert module.main('verify')['sources']==initial['sources']
    with (root/'streams.tif').open('ab') as stream:stream.write(b'changed source bytes')
    with pytest.raises(module.FixtureError,match='source file changed'):module.main('verify')


def test_stream_order_d8_fixture_preserves_exact_uint64_nodata_adjacent_invalid_value(stream_order_d8_persistence):
    module,root,_=stream_order_d8_persistence;ds=gdal.Open(str(root/'code.tif'));band=ds.GetRasterBand(2)
    assert band.DataType==gdal.GDT_UInt64 and band.GetNoDataValueAsUInt64()==2**53
    assert int(band.ReadAsArray()[509,100])==2**53+1 and int(band.ReadAsArray()[0,0])==2**53
    band=ds=None


def test_stream_order_d8_fixture_backward_count_and_threshold_have_independent_row_oracle():
    module=importlib.import_module('business.scripts.online-raster-stream-order-d8-fixture')
    directions=module.expected_directions('dem-order.cog.tif');counts=module.backward_oracle(directions)
    np.testing.assert_array_equal(counts[module.FIRST_ROW:,:],np.broadcast_to(np.arange(module.WIDTH),(7,module.WIDTH)))
    expected=np.where(np.isfinite(directions),0.,np.nan);expected[module.FIRST_ROW:,2:]=1
    np.testing.assert_array_equal(module.expected_pixels('dem-order.cog.tif'),expected)


def test_stream_order_d8_fixture_fresh_verifier_keeps_prior_file_checks(stream_order_d8_persistence):
    module,root,initial=stream_order_d8_persistence
    for method in (module.expected_directions,module.expected_streams,module.expected_pixels):method.cache_clear()
    result=module.verify(root,list(module.TARGETS))
    assert result==module.verify(root,list(module.TARGETS)) and result['sources']==initial['sources']
