"""Real GDAL regression for the independent thin Hosted fixture."""
import importlib
import json
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

@pytest.fixture(scope='module')
def thin_persistence(tmp_path_factory):
    module=importlib.import_module('business.scripts.online-raster-thin-fixture')
    scene=importlib.import_module('scripts.test.raster-thin-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('thin-export');(root/'outputs').mkdir()
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


def test_thin_persistence_real_gdal_three_cog_chains_and_complete_pixels(thin_persistence):
    module,root,initial=thin_persistence
    actual=module.verify(root,list(module.TARGETS))
    assert actual['sources']==initial['sources']
    assert len(actual['targets'])==3
    for name,fact in actual['targets'].items():
        assert fact['full_pixel_and_mask_verification'] and fact['cog_valid'] and fact['has_overviews']
        assert fact['exact_integer_pixels_verified']
        assert (fact['valid_pixels']==0 if name=='empty-thin.cog.tif' else 0<fact['valid_pixels']<module.WIDTH*module.HEIGHT)
    pixels=module.expected_pixels('thin.cog.tif')
    assert np.isnan(module.expected_pixels('empty-thin.cog.tif')).all()
    assert set(pixels[np.isfinite(pixels)])=={0.,1.}
    assert all(fact['binary_skeleton_verified'] for fact in actual['targets'].values())
    direct=module.expected_pixels('thin.cog.tif');reclassified=module.expected_pixels('reclassified-thin.cog.tif')
    np.testing.assert_array_equal(direct,reclassified)
    assert 0<int((direct==1).sum())<int((module.source_arrays()[0]==0).sum())



@pytest.mark.parametrize('corruption',['value','one_unit','seam','invalid_center','mask','crs','grid','dtype','nodata','scale','overviews','sidecar','missing','extra'])
def test_thin_persistence_oracle_rejects_corruption(thin_persistence,tmp_path,corruption):
    import shutil
    import numpy as np
    from osgeo import gdal
    module,original,_=thin_persistence
    root=tmp_path/'changed';shutil.copytree(original,root)
    path=root/'outputs'/'thin.cog.tif'
    if corruption in ('value','one_unit','seam','invalid_center','mask','crs','grid','nodata','scale'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
        if corruption in ('value','one_unit','seam','invalid_center'):
            x,y={'value':(0,0),'one_unit':(0,0),'seam':(509,508),'invalid_center':(509,509)}[corruption]
            value=module.expected_pixels('thin.cog.tif')[y,x]+1 if corruption=='one_unit' else 123456.
            ds.GetRasterBand(1).WriteRaster(x,y,1,1,np.array([value],dtype=np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        elif corruption=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            ds.GetRasterBand(1).GetMaskBand().Fill(255)
        elif corruption=='crs':ds.SetProjection('')
        elif corruption=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif corruption=='nodata':ds.GetRasterBand(1).SetNoDataValue(-999)
        else:ds.GetRasterBand(1).SetScale(2.)
        ds=None
    elif corruption in ('dtype','overviews'):
        alternate=root/'replacement.tif'
        ds=gdal.Translate(str(alternate),str(path),format='GTiff',outputType=gdal.GDT_Float32 if corruption=='dtype' else gdal.GDT_Float64)
        ds=None;alternate.replace(path)
    elif corruption=='sidecar':(root/'outputs'/'thin.cog.tif.aux.xml').write_text('unexpected')
    elif corruption=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_bytes(path.read_bytes())
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))


@pytest.mark.parametrize('problem',['band','fractional','overflow','foreground'])
def test_thin_persistence_invalid_classes_fail_without_publication(thin_persistence,tmp_path,problem):
    import shutil
    from workflow_engine import execute_workflow
    module,original,initial=thin_persistence
    scene=importlib.import_module('scripts.test.raster-thin-persistence-online')
    root=tmp_path/'failed';shutil.copytree(original,root)
    definition=scene.definition({n:n for n in module.SOURCES},'parent','thin.cog.tif',invalid=problem)
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
    result=execute_workflow(definition)
    assert result['status']=='failed',result
    assert not target.exists()
    assert module.verify(root,list(module.TARGETS))['sources']==initial['sources']


def test_thin_fixture_raw_classes_hidden_invalids_and_empty_band(thin_persistence):
    module,root,_=thin_persistence
    values,alpha,mask,continuous,_,_=module.source_arrays()
    valid=np.isfinite(values)&(values!=-9999)&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    assert {-module.SAFE_INTEGER,-3,module.SAFE_INTEGER-1,module.SAFE_INTEGER,0}<=set(values[valid])
    assert values[509,509]==.5 and values[508,510]==2**53 and not valid[509,509] and not valid[508,510]
    assert continuous[0,0]==1.5 and alpha[0,0]==128 and valid[0,0]
    source=gdal.Open(str(root/'values.tif'))
    assert source.RasterCount==6 and np.all(source.GetRasterBand(6).ReadAsArray()==-9999)
    source=None
    with pytest.raises(module.FixtureError):module.seed(root)
