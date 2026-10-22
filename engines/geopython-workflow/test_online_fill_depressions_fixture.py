"""Real GDAL regression for the independent fill_depressions Hosted fixture."""
import importlib
import json
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

@pytest.fixture(scope='module')
def fill_depressions_persistence(tmp_path_factory):
    module=importlib.import_module('business.scripts.online-raster-fill-depressions-fixture')
    scene=importlib.import_module('scripts.test.raster-fill-depressions-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('fill_depressions-export');(root/'outputs').mkdir()
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


def test_fill_depressions_persistence_real_gdal_three_cog_chains_and_complete_pixels(fill_depressions_persistence):
    module,root,initial=fill_depressions_persistence
    actual=module.verify(root,list(module.TARGETS))
    assert actual['sources']==initial['sources']
    assert len(actual['targets'])==3
    for name,fact in actual['targets'].items():
        assert fact['full_pixel_and_mask_verification'] and fact['cog_valid'] and fact['has_overviews']
        assert fact['unit_metres_verified']
        assert (fact['valid_pixels']==0 if name=='empty-filled.cog.tif' else 0<fact['valid_pixels']<module.WIDTH*module.HEIGHT)
    pixels=module.expected_pixels('filled.cog.tif')
    assert np.isnan(module.expected_pixels('empty-filled.cog.tif')).all()
    assert (pixels[np.isfinite(pixels)]>=-5).all()
    assert all(fact['depression_fill_verified'] and fact['open_outlets_verified'] and fact['never_lowered_verified'] for fact in actual['targets'].values())
    direct=module.expected_pixels('filled.cog.tif');feet=module.expected_pixels('feet-filled.cog.tif')
    np.testing.assert_array_equal(direct*.3048,feet)
    assert actual['targets']['filled.cog.tif']['raised_pixels']>0
    assert direct[510,512]==22. and direct[110,110]==24. and direct[390,390]==7.
    assert direct[1,1]==4. and direct[250,250]==12.



@pytest.mark.parametrize('corruption',['value','one_unit','seam','invalid_center','mask','crs','grid','dtype','nodata','scale','unit','overviews','sidecar','source_sidecar','missing','extra'])
def test_fill_depressions_persistence_oracle_rejects_corruption(fill_depressions_persistence,tmp_path,corruption):
    import shutil
    import numpy as np
    from osgeo import gdal
    module,original,_=fill_depressions_persistence
    root=tmp_path/'changed';shutil.copytree(original,root)
    path=root/'outputs'/'filled.cog.tif'
    if corruption in ('value','one_unit','seam','invalid_center','mask','crs','grid','nodata','scale','unit'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
        if corruption in ('value','one_unit','seam','invalid_center'):
            x,y={'value':(0,0),'one_unit':(0,0),'seam':(509,508),'invalid_center':(350,250)}[corruption]
            value=module.expected_pixels('filled.cog.tif')[y,x]+1 if corruption=='one_unit' else 123456.
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
    elif corruption=='sidecar':(root/'outputs'/'filled.cog.tif.aux.xml').write_text('unexpected')
    elif corruption=='source_sidecar':(root/'dem.tif.aux.xml').write_text('unexpected')
    elif corruption=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_bytes(path.read_bytes())
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))


@pytest.mark.parametrize('problem',['band','grid','unit','scale'])
def test_fill_depressions_persistence_invalid_parameters_fail_without_publication(fill_depressions_persistence,tmp_path,problem):
    import shutil
    from workflow_engine import execute_workflow
    module,original,initial=fill_depressions_persistence
    scene=importlib.import_module('scripts.test.raster-fill-depressions-persistence-online')
    root=tmp_path/'failed';shutil.copytree(original,root)
    definition=scene.definition({n:n for n in module.SOURCES},'parent','filled.cog.tif',invalid=problem)
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
    expected={'band':'DEM 填洼必须显式选择有效的一基高程数据波段',
              'grid':'DEM 填洼需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影',
              'unit':'DEM 填洼高程波段单位未知或与声明单位冲突',
              'scale':'DEM 填洼前必须显式应用高程 scale/offset'}
    assert result['error_code']=='EXECUTION_FAILED' and result['error']=='任务 fill 执行失败: '+expected[problem],result
    assert not target.exists()
    assert module.verify(root,list(module.TARGETS))['sources']==initial['sources']


def test_fill_depressions_fixture_validity_units_and_explicit_band(fill_depressions_persistence):
    module,root,_=fill_depressions_persistence
    values,alpha,mask=module.source_arrays()
    assert values[0,0]==4 and alpha[0,0]==128
    expected=module.expected_pixels('filled.cog.tif')
    assert expected[0,100]==0 and np.isnan(expected[360:365,410:415]).all()
    ds=gdal.Open(str(root/'dem.tif'))
    assert ds.RasterCount==4 and np.all(ds.GetRasterBand(3).ReadAsArray()==-9999)
    assert ds.GetRasterBand(2).GetUnitType()=='metre';ds=None
    with pytest.raises(module.FixtureError):module.seed(root)


def test_fill_depressions_fixture_source_receipt_rejects_any_byte_change(fill_depressions_persistence,tmp_path,monkeypatch):
    import shutil
    module,original,initial=fill_depressions_persistence
    root=tmp_path/'sources';shutil.copytree(original,root);secret=tmp_path/'secret';secret.mkdir()
    (secret/'fill-depressions-source.json').write_text(json.dumps(initial))
    monkeypatch.setattr(module,'guard',lambda:secret)
    monkeypatch.setattr(module,'owned_root',lambda _:root)
    monkeypatch.setattr(module,'runtime_worker',lambda action,owned,names:module.verify(owned,names))
    monkeypatch.setattr(module.sys,'argv',['fixture','verify',*module.TARGETS])
    assert module.main('verify')['sources']==initial['sources']
    with (root/'feet.tif').open('ab') as stream:stream.write(b'changed source bytes')
    with pytest.raises(module.FixtureError,match='source file changed'):module.main('verify')
