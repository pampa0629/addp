"""Real GDAL regression for independent original-donor nibble persistence."""
import importlib
import json
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

@pytest.fixture(scope='module')
def nibble_persistence(tmp_path_factory):
    module=importlib.import_module('business.scripts.online-raster-nibble-fixture')
    scene=importlib.import_module('scripts.test.raster-nibble-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('nibble-export');(root/'outputs').mkdir()
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


def test_nibble_persistence_real_gdal_three_cog_chains_and_complete_pixels(nibble_persistence):
    module,root,initial=nibble_persistence
    actual=module.verify(root,list(module.TARGETS))
    assert actual['sources']==initial['sources']
    assert len(actual['targets'])==3
    for fact in actual['targets'].values():
        assert fact['full_pixel_and_mask_verification'] and fact['cog_valid'] and fact['has_overviews']
        assert fact['exact_integer_pixels_verified']
        assert 0<fact['valid_pixels']<module.WIDTH*module.HEIGHT
    # Four-way row-first and horizontal column-first ties; centre is not a donor.
    pixels=module.expected_pixels('nibble.cog.tif')
    assert pixels[12,12]==0. and pixels[30,32]==module.SAFE_INTEGER
    assert pixels[0,0]==-module.SAFE_INTEGER
    assert {0.,-3.,-float(module.SAFE_INTEGER),float(module.SAFE_INTEGER-1),float(module.SAFE_INTEGER)} <= set(pixels[np.isfinite(pixels)])
    assert np.isnan(pixels[100:103]).all()
    original=module.source_arrays()[0]
    assert original[12,12] in (7,8) and original[30,32] in (7,8)
    unchanged=module.expected_pixels('unchanged.cog.tif')
    assert unchanged[12,12]==original[12,12] and unchanged[30,32]==original[30,32]


@pytest.mark.parametrize('corruption',['value','one_unit','seam','invalid_center','mask','crs','grid','dtype','nodata','scale','unit','overviews','sidecar','missing','extra'])
def test_nibble_persistence_oracle_rejects_corruption(nibble_persistence,tmp_path,corruption):
    import shutil
    import numpy as np
    from osgeo import gdal
    module,original,_=nibble_persistence
    root=tmp_path/'changed';shutil.copytree(original,root)
    path=root/'outputs'/'nibble.cog.tif'
    if corruption in ('value','one_unit','seam','invalid_center','mask','crs','grid','nodata','scale','unit'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
        if corruption in ('value','one_unit','seam','invalid_center'):
            x,y={'value':(0,0),'one_unit':(0,0),'seam':(509,508),'invalid_center':(509,509)}[corruption]
            value=module.expected_pixels('nibble.cog.tif')[y,x]+1 if corruption=='one_unit' else 123456.
            ds.GetRasterBand(1).WriteRaster(x,y,1,1,np.array([value],dtype=np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        elif corruption=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            ds.GetRasterBand(1).GetMaskBand().Fill(255)
        elif corruption=='crs':ds.SetProjection('')
        elif corruption=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif corruption=='nodata':ds.GetRasterBand(1).SetNoDataValue(-999)
        elif corruption=='unit':ds.GetRasterBand(1).SetUnitType('metre')
        else:ds.GetRasterBand(1).SetScale(2.)
        ds=None
    elif corruption in ('dtype','overviews'):
        alternate=root/'replacement.tif'
        ds=gdal.Translate(str(alternate),str(path),format='GTiff',outputType=gdal.GDT_Float32 if corruption=='dtype' else gdal.GDT_Float64)
        ds=None;alternate.replace(path)
    elif corruption=='sidecar':(root/'outputs'/'nibble.cog.tif.aux.xml').write_text('unexpected')
    elif corruption=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_bytes(path.read_bytes())
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))


@pytest.mark.parametrize('problem',['band','unsafe','no-donor'])
def test_nibble_persistence_invalid_classes_fail_without_publication(nibble_persistence,tmp_path,problem):
    import shutil
    from workflow_engine import execute_workflow
    module,original,initial=nibble_persistence
    scene=importlib.import_module('scripts.test.raster-nibble-persistence-online')
    root=tmp_path/'failed';shutil.copytree(original,root)
    definition=scene.definition({n:n for n in module.SOURCES},'parent','nibble.cog.tif',invalid=problem)
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
