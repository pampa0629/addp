"""Real GDAL regression for the independent flow_accumulation_weighted_d8 Hosted fixture."""
import importlib
import json
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

@pytest.fixture(scope='module')
def flow_accumulation_weighted_d8_persistence(tmp_path_factory):
    module=importlib.import_module('business.scripts.online-raster-flow-accumulation-weighted-d8-fixture')
    scene=importlib.import_module('scripts.test.raster-flow-accumulation-weighted-d8-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('flow_accumulation_weighted_d8-export');(root/'outputs').mkdir()
    # The real NFS lifecycle always creates this ownership marker.
    (root/'.addp-online-owner').write_text('flow-accumulation-weighted-d8-123-1')
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



def test_flow_accumulation_weighted_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels(flow_accumulation_weighted_d8_persistence):
    module,root,initial=flow_accumulation_weighted_d8_persistence
    actual=module.verify(root,list(module.TARGETS))
    assert actual['sources']==initial['sources'] and len(actual['targets'])==6
    for name,fact in actual['targets'].items():
        assert all(fact[key] for key in ('full_pixel_and_mask_verification','cog_valid','has_overviews','weighted_sum_verified','self_exclusion_verified','mass_balance_verified','valid_zero_verified','weight_unit_verified'))
        if name=='invalid-weighted.cog.tif':assert fact['valid_pixels']==fact['zero_pixels']==fact['weight_total']==0
        else:assert fact['valid_pixels']>0 and fact['zero_pixels']>0 and fact['weight_total']>0
        assert fact['weight_total']==fact['outlet_total_with_self']
        assert fact['size_bytes']==(root/'outputs'/name).stat().st_size>0
        assert fact['unit']==module.expected_unit(name)
    assert actual['targets']['ones.cog.tif']['all_ones_equivalent_verified']
    assert actual['targets']['weighted.cog.tif']['valid_pixels']==3628
    assert actual['targets']['dem-weighted.cog.tif']['valid_pixels']==3633
    assert actual['targets']['weighted.cog.tif']['unit']=='kg'
    assert actual['targets']['dem-weighted.cog.tif']['unit']=='m3'
    assert actual['targets']['reclass.cog.tif']['unit']==''


@pytest.mark.parametrize('corruption',['value','one_unit','fraction','seam','invalid_center','mask','crs','grid','dtype','nodata','scale','unit','overviews','overview_sum','sidecar','source_sidecar','missing_owner','missing','extra'])
def test_flow_accumulation_weighted_d8_persistence_oracle_rejects_corruption(flow_accumulation_weighted_d8_persistence,tmp_path,corruption):
    import shutil
    module,original,_=flow_accumulation_weighted_d8_persistence
    root=tmp_path/'changed';shutil.copytree(original,root);path=root/'outputs'/'weighted.cog.tif'
    if corruption in ('value','one_unit','fraction','seam','invalid_center','mask','crs','grid','nodata','scale','unit','overview_sum'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
        band=ds.GetRasterBand(1)
        if corruption in ('value','one_unit','fraction','seam','invalid_center'):
            y,x=(100,100) if corruption=='invalid_center' else (511,512) if corruption=='seam' else (511,518)
            value=module.expected_pixels('weighted.cog.tif')[y,x]+(1e-6 if corruption=='fraction' else 1) if corruption in ('fraction','one_unit') else 12345.
            band.WriteRaster(x,y,1,1,np.array([value],np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        elif corruption=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().Fill(255)
        elif corruption=='crs':ds.SetProjection('')
        elif corruption=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif corruption=='nodata':band.SetNoDataValue(-999)
        elif corruption=='unit':band.SetUnitType('metre')
        elif corruption=='overview_sum':band.GetOverview(0).Fill(12345.125)
        else:band.SetScale(2.)
        band=ds=None
    elif corruption in ('dtype','overviews'):
        alternate=root/'replacement.tif';ds=gdal.Translate(str(alternate),str(path),format='GTiff',outputType=gdal.GDT_Float32 if corruption=='dtype' else gdal.GDT_Float64);ds=None;alternate.replace(path)
    elif corruption=='sidecar':(root/'outputs'/'weighted.cog.tif.aux.xml').write_text('unexpected')
    elif corruption=='source_sidecar':(root/'directions.tif.aux.xml').write_text('unexpected')
    elif corruption=='missing_owner':(root/'.addp-online-owner').unlink()
    elif corruption=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_bytes(path.read_bytes())
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))


@pytest.mark.parametrize('problem',['negative','coverage','alignment','code','cycle','band','weights_band','overflow'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_flow_accumulation_weighted_d8_persistence_invalid_parameters_fail_without_publication(flow_accumulation_weighted_d8_persistence,tmp_path,problem,language):
    import shutil
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    module,original,initial=flow_accumulation_weighted_d8_persistence;root=tmp_path/'failed';shutil.copytree(original,root)
    scene=importlib.import_module('scripts.test.raster-flow-accumulation-weighted-d8-persistence-online')
    definition=scene.definition({n:n for n in module.SOURCES},'parent','weighted.cog.tif',invalid=problem,language=language)
    for task in definition['tasks']:
        params=task['params']
        if task['operator']=='raster_load':
            source=root/params.pop('locator');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(source)}}}
        elif task['operator']=='raster_save':
            params.pop('target_parent_locator');target=root/'outputs'/params.pop('target_name');mode=params.pop('write_mode')
            params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{'kind':'file','format':'tiff','name':target.name,'write_mode':mode,'access':{'method':'mounted_path','path':str(target)}}}
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner(language=language).execute(definition)
    assert caught.value.error_code=='EXECUTION_FAILED'
    assert str(caught.value)=='任务 weighted 执行失败: '+scene.MESSAGES[problem][language=='en']
    assert not target.exists() and module.verify(root,list(module.TARGETS))['sources']==initial['sources']


@pytest.mark.parametrize('fault',['include_self','count_instead_weight','outlet_zero','zero_nodata','invalid_zero','fraction','missing_tributary'])
def test_weighted_d8_independent_walk_oracle_rejects_wrong_sums(fault):
    module=importlib.import_module('business.scripts.online-raster-flow-accumulation-weighted-d8-fixture')
    actual=module.expected_pixels('weighted.cog.tif').copy()
    if fault=='include_self':actual[508,10]+=module.expected_weights('weighted.cog.tif')[508,10]
    elif fault=='count_instead_weight':actual[510,10]=2
    elif fault=='outlet_zero':actual[511,518]=0
    elif fault=='zero_nodata':actual[508,0]=np.nan
    elif fault=='invalid_zero':actual[100,100]=0
    elif fault=='missing_tributary':actual[511,513]-=1
    else:actual[511,518]+=1e-6
    with pytest.raises(module.FixtureError):module.weighted_evidence(actual,'weighted.cog.tif')


def test_flow_accumulation_weighted_d8_fixture_validity_zero_alpha_cross_block_and_both_selected_bands(flow_accumulation_weighted_d8_persistence):
    module,root,_=flow_accumulation_weighted_d8_persistence
    for name in ('directions.tif','weights.tif'):
        ds=gdal.Open(str(root/name));assert ds.RasterCount==4
        assert np.all(ds.GetRasterBand(1).ReadAsArray()==.25)
        assert np.all(ds.GetRasterBand(3).ReadAsArray()==-9999)
        assert ds.GetRasterBand(4).ReadAsArray()[511,515]==128
        assert ds.GetRasterBand(1).GetMaskBand().ReadAsArray()[514,510]==0
        ds=None
    assert module.expected_weights('weighted.cog.tif')[508,0]==0 and module.expected_pixels('weighted.cog.tif')[508,0]==0
    assert np.isfinite(module.expected_pixels('weighted.cog.tif')[511,515])
    with pytest.raises(module.FixtureError):module.seed(root)


def test_flow_accumulation_weighted_d8_fixture_source_receipt_rejects_any_byte_change(flow_accumulation_weighted_d8_persistence,tmp_path,monkeypatch):
    import shutil
    module,original,initial=flow_accumulation_weighted_d8_persistence;root=tmp_path/'sources';shutil.copytree(original,root);secret=tmp_path/'secret';secret.mkdir()
    (secret/'flow-accumulation-weighted-d8-source.json').write_text(json.dumps(initial));monkeypatch.setattr(module,'guard',lambda:secret);monkeypatch.setattr(module,'owned_root',lambda _:root)
    monkeypatch.setattr(module,'runtime_worker',lambda action,owned,names:module.verify(owned,names));monkeypatch.setattr(module.sys,'argv',['fixture','verify',*module.TARGETS])
    assert module.main('verify')['sources']==initial['sources']
    with (root/'weights.tif').open('ab') as stream:stream.write(b'changed source bytes')
    with pytest.raises(module.FixtureError,match='source file changed'):module.main('verify')


def test_flow_accumulation_weighted_d8_fixture_preserves_exact_uint64_nodata_adjacent_invalid_value(flow_accumulation_weighted_d8_persistence):
    module,root,_=flow_accumulation_weighted_d8_persistence;ds=gdal.Open(str(root/'code.tif'));band=ds.GetRasterBand(2)
    assert band.DataType==gdal.GDT_UInt64 and band.GetNoDataValueAsUInt64()==2**53
    assert int(band.ReadAsArray()[509,100])==2**53+1 and int(band.ReadAsArray()[0,0])==2**53
    band=ds=None


def test_weighted_d8_fixture_ones_reduction_has_independent_column_oracle():
    module=importlib.import_module('business.scripts.online-raster-flow-accumulation-weighted-d8-fixture')
    expected=module.expected_pixels('ones.cog.tif')
    np.testing.assert_array_equal(expected[module.FIRST_ROW:,50],np.arange(7))
    # A two-source confluence receives both original donor weights, and an
    # open terminal never includes its own contribution in its reported sum.
    directions=module.expected_directions('weighted.cog.tif');weights=module.expected_weights('weighted.cog.tif')
    assert directions[509,511]==2 and directions[509,513]==8
    assert module.expected_pixels('weighted.cog.tif')[510,512]>=weights[509,511]+weights[509,513]


def test_flow_accumulation_weighted_d8_fixture_fresh_verifier_keeps_prior_file_checks(flow_accumulation_weighted_d8_persistence):
    module,root,initial=flow_accumulation_weighted_d8_persistence
    for method in (module.expected_directions,module.expected_weights,module.expected_pixels):method.cache_clear()
    result=module.verify(root,list(module.TARGETS))
    assert result==module.verify(root,list(module.TARGETS)) and result['sources']==initial['sources']
