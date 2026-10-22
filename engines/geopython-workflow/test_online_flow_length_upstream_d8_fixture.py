"""Real GDAL regression for the independent flow_length_upstream_d8 Hosted fixture."""
import importlib
import json
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

@pytest.fixture(scope='module')
def flow_length_upstream_d8_persistence(tmp_path_factory):
    module=importlib.import_module('business.scripts.online-raster-flow-length-upstream-d8-fixture')
    scene=importlib.import_module('scripts.test.raster-flow-length-upstream-d8-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('flow_length_upstream_d8-export');(root/'outputs').mkdir()
    # The real NFS lifecycle always creates this ownership marker.
    (root/'.addp-online-owner').write_text('flow-length-upstream-d8-123-1')
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


def test_flow_length_upstream_d8_persistence_real_gdal_six_cog_chains_and_complete_pixels(flow_length_upstream_d8_persistence):
    module,root,initial=flow_length_upstream_d8_persistence
    actual=module.verify(root,list(module.TARGETS))
    assert actual['sources']==initial['sources']
    assert len(actual['targets'])==6
    for name,fact in actual['targets'].items():
        assert fact['full_pixel_and_mask_verification'] and fact['cog_valid'] and fact['has_overviews']
        assert fact['metre_unit_verified'] and fact['centre_steps_verified'] and fact['sources_zero']
        assert fact['longest_path_verified'] and fact['confluence_max_verified'] and fact['outlets_receive_upstream']
        if name!='empty-length.cog.tif':
            assert fact['source_pixels']>0 and fact['confluence_pixels']>0
            assert fact['terminal_pixels']==fact['fed_terminal_pixels']==2
        assert (fact['valid_pixels']==0 if name=='empty-length.cog.tif' else 0<fact['valid_pixels']<module.WIDTH*module.HEIGHT)
    direct=module.expected_pixels('length.cog.tif')
    assert direct[0,1]>1800 and direct[511,517]==0 and direct[512,518]>0
    np.testing.assert_array_equal(direct,module.expected_pixels('dem-length.cog.tif'))
    np.testing.assert_array_equal(direct,module.expected_pixels('filled-length.cog.tif'))
    assert np.isnan(module.expected_pixels('empty-length.cog.tif')).all()



@pytest.mark.parametrize('corruption',['value','one_unit','tiny_value','seam','invalid_center','mask','crs','grid','dtype','nodata','scale','unit','overviews','sidecar','source_sidecar','missing_owner','missing','extra'])
def test_flow_length_upstream_d8_persistence_oracle_rejects_corruption(flow_length_upstream_d8_persistence,tmp_path,corruption):
    import shutil
    import numpy as np
    from osgeo import gdal
    module,original,_=flow_length_upstream_d8_persistence
    root=tmp_path/'changed';shutil.copytree(original,root)
    path=root/'outputs'/'length.cog.tif'
    if corruption in ('value','one_unit','tiny_value','seam','invalid_center','mask','crs','grid','nodata','scale','unit'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
        if corruption in ('value','one_unit','tiny_value','seam','invalid_center'):
            x,y={'value':(0,0),'one_unit':(0,0),'tiny_value':(509,508),'seam':(509,508),'invalid_center':(515,250)}[corruption]
            value=module.expected_pixels('length.cog.tif')[y,x]+(1e-6 if corruption=='tiny_value' else 1) if corruption in ('one_unit','tiny_value') else 123456.
            ds.GetRasterBand(1).WriteRaster(x,y,1,1,np.array([value],dtype=np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        elif corruption=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            ds.GetRasterBand(1).GetMaskBand().Fill(255)
        elif corruption=='crs':ds.SetProjection('')
        elif corruption=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif corruption=='nodata':ds.GetRasterBand(1).SetNoDataValue(-999)
        elif corruption=='unit':ds.GetRasterBand(1).SetUnitType('foot')
        else:ds.GetRasterBand(1).SetScale(2.)
        ds=None
    elif corruption in ('dtype','overviews'):
        alternate=root/'replacement.tif'
        ds=gdal.Translate(str(alternate),str(path),format='GTiff',outputType=gdal.GDT_Float32 if corruption=='dtype' else gdal.GDT_Float64)
        ds=None;alternate.replace(path)
    elif corruption=='sidecar':(root/'outputs'/'length.cog.tif.aux.xml').write_text('unexpected')
    elif corruption=='source_sidecar':(root/'direct.tif.aux.xml').write_text('unexpected')
    elif corruption=='missing_owner':(root/'.addp-online-owner').unlink()
    elif corruption=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_bytes(path.read_bytes())
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))


@pytest.mark.parametrize('problem',['code','terminal','outside','invalid','cycle','band','grid'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_flow_length_upstream_d8_persistence_invalid_parameters_fail_without_publication(flow_length_upstream_d8_persistence,tmp_path,problem,language):
    import shutil
    from workflow_engine import execute_workflow
    module,original,initial=flow_length_upstream_d8_persistence
    scene=importlib.import_module('scripts.test.raster-flow-length-upstream-d8-persistence-online')
    root=tmp_path/'failed';shutil.copytree(original,root)
    definition=scene.definition({n:n for n in module.SOURCES},'parent','length.cog.tif',invalid=problem,language=language)
    for task in definition['tasks']:
        params=task['params']
        if task['operator']=='raster_load':
            source=root/params.pop('locator')
            params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{
                'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(source)}}}
        elif task['operator']=='raster_save':
            params.pop('target_parent_locator');target=root/'outputs'/params.pop('target_name')
            mode=params.pop('write_mode')
            params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{
                'kind':'file','format':'tiff','name':target.name,'write_mode':mode,
                'access':{'method':'mounted_path','path':str(target)}}}
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    with pytest.raises(WorkflowExecutionError) as caught:
        GeoPythonWorkflowRunner(language=language).execute(definition)
    assert caught.value.error_code=='EXECUTION_FAILED'
    assert str(caught.value)=='任务 length 执行失败: '+scene.MESSAGES[problem][language=='en']
    assert not target.exists()
    assert module.verify(root,list(module.TARGETS))['sources']==initial['sources']


def test_flow_length_upstream_d8_fixture_validity_units_and_explicit_band(flow_length_upstream_d8_persistence):
    module,root,_=flow_length_upstream_d8_persistence
    values,alpha,mask=module.source_arrays()
    assert values[511,517]==0 and alpha[511,517]==128 and mask[510,516]==0
    expected=module.expected_pixels('length.cog.tif')
    assert expected[0,1]>1800 and expected[511,517]==0 and np.isnan(expected[510,516:519]).all()
    ds=gdal.Open(str(root/'direct.tif'))
    assert ds.RasterCount==4 and np.all(ds.GetRasterBand(3).ReadAsArray()==-9999)
    assert ds.GetRasterBand(2).GetUnitType()=='metre';ds=None
    with pytest.raises(module.FixtureError):module.seed(root)


def test_flow_length_upstream_d8_fixture_source_receipt_rejects_any_byte_change(flow_length_upstream_d8_persistence,tmp_path,monkeypatch):
    import shutil
    module,original,initial=flow_length_upstream_d8_persistence
    root=tmp_path/'sources';shutil.copytree(original,root);secret=tmp_path/'secret';secret.mkdir()
    (secret/'flow-length-upstream-d8-source.json').write_text(json.dumps(initial))
    monkeypatch.setattr(module,'guard',lambda:secret)
    monkeypatch.setattr(module,'owned_root',lambda _:root)
    monkeypatch.setattr(module,'runtime_worker',lambda action,owned,names:module.verify(owned,names))
    monkeypatch.setattr(module.sys,'argv',['fixture','verify',*module.TARGETS])
    assert module.main('verify')['sources']==initial['sources']
    with (root/'bowl.tif').open('ab') as stream:stream.write(b'changed source bytes')
    with pytest.raises(module.FixtureError,match='source file changed'):module.main('verify')


@pytest.mark.parametrize('fault',['half_pixel','terminal_zero','source_changed','invalid_filled','negative','diagonal_as_axis','wrong_units','sum','shortest'])
def test_flow_length_upstream_d8_distance_oracle_rejects_wrong_lengths(fault):
    import math
    module=importlib.import_module('business.scripts.online-raster-flow-length-upstream-d8-fixture')
    actual=module.expected_pixels('length.cog.tif').copy()
    if fault=='half_pixel':actual+=1
    elif fault=='terminal_zero':actual[0,1]=0
    elif fault=='source_changed':actual[511,517]=1
    elif fault=='invalid_filled':actual[250,515]=0
    elif fault=='negative':actual[250,250]=-1
    elif fault=='wrong_units':actual*=.3048
    elif fault=='diagonal_as_axis':actual[512,518]=2
    else:
        # Find a real confluence with unequal tributary lengths. Replacing MAX
        # by either SUM or shortest must be rejected by the independent oracle.
        directions=module.expected_directions('length.cog.tif')
        candidates={}
        for dy,dx,code in module.NEIGHBOURS:
            rows,cols=np.nonzero(directions==code)
            for row,col in zip(rows,cols):
                candidates.setdefault((row+dy,col+dx),[]).append(actual[row,col]+math.hypot(dx*2,dy*3))
        cell,distances=next((cell,ds) for cell,ds in candidates.items() if len(ds)>1 and max(ds)>min(ds)>0)
        assert actual[cell]==pytest.approx(max(distances))
        actual[cell]=sum(distances) if fault=='sum' else min(distances)
    with pytest.raises(module.FixtureError):module.distance_evidence(actual,'length.cog.tif')


@pytest.mark.parametrize('name,code,factor',[('length.cog.tif',3857,1.),('feet-length.cog.tif',2222,.3048),('survey-feet-length.cog.tif',2277,1200/3937)])
def test_flow_length_upstream_d8_fixture_three_horizontal_units_remain_in_original_crs(flow_length_upstream_d8_persistence,name,code,factor):
    import math
    from osgeo import osr
    module,root,_=flow_length_upstream_d8_persistence
    source,_,_=module.CASES[name]
    ds=gdal.Open(str(root/source));crs=osr.SpatialReference(wkt=ds.GetProjection())
    assert crs.GetAuthorityCode(None)==str(code)
    assert crs.GetLinearUnits()==pytest.approx(factor,abs=1e-15)
    assert ds.GetGeoTransform()==module.TRANSFORM
    ds=None
    expected=module.expected_pixels(name)
    metre=module.expected_pixels('length.cog.tif')
    np.testing.assert_allclose(expected,metre*factor,rtol=module.RTOL,atol=module.ATOL,equal_nan=True)
    assert expected[0,1]>0
    # The isolated component has three no-inflow centres feeding one open
    # terminal; MAX chooses the diagonal rather than adding tributaries.
    assert expected[511,517]==expected[511,518]==0
    assert expected[512,517]==0
    assert expected[512,518]==pytest.approx(math.hypot(2,3)*factor)
    assert module.artifact_expectation(name)['extent_srid']==code


def test_flow_length_upstream_d8_fixture_preserves_exact_uint64_nodata_adjacent_code(flow_length_upstream_d8_persistence):
    module,root,_=flow_length_upstream_d8_persistence
    ds=gdal.Open(str(root/'code.tif'));band=ds.GetRasterBand(2)
    assert band.DataType==gdal.GDT_UInt64
    assert band.GetNoDataValueAsUInt64()==2**53
    assert int(band.ReadAsArray()[250,250])==2**53+1
    assert int(band.ReadAsArray()[0,0])==2**53
    band=ds=None
