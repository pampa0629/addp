"""Real GDAL regressions for independent D8 distance-to-stream Hosted acceptance."""
import importlib
import json
import shutil
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

@pytest.fixture(scope='module')
def hand_d8_persistence(tmp_path_factory):
    module=importlib.import_module('business.scripts.online-raster-hand-d8-fixture')
    scene=importlib.import_module('scripts.test.raster-hand-d8-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('hand_d8-export');(root/'outputs').mkdir()
    # The real NFS lifecycle always creates this ownership marker.
    (root/'.addp-online-owner').write_text('hand-d8-123-1')
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



def test_hand_d8_persistence_real_gdal_eight_cog_chains_and_complete_pixels(hand_d8_persistence):
    module,root,initial=hand_d8_persistence;actual=module.verify(root,list(module.TARGETS))
    assert actual['sources']==initial['sources'] and len(actual['targets'])==8
    for name,fact in actual['targets'].items():
        assert all(fact[k] for k in ('full_pixel_and_mask_verification','cog_valid','has_overviews','nearest_overviews_verified','independent_hand_oracle_verified','first_hit_verified','river_zero_verified','unreachable_nodata_verified','stream_closure_verified','metre_unit_verified','non_increasing_elevation_verified','flat_zero_verified'))
        assert fact['size_bytes']==(root/'outputs'/name).stat().st_size>0
        if name in ('empty.cog.tif','invalid.cog.tif'):assert fact['valid_pixels']==fact['river_pixels']==0
        else:assert fact['valid_pixels']>fact['river_pixels']>0
        if name in ('hand.cog.tif','feet.cog.tif','survey-feet.cog.tif'):assert fact['flat_nonriver_pixels']>0
    expected=module.expected_pixels('hand.cog.tif')
    assert expected[511,515]==expected[508,20]==0
    assert expected[509,511]>0 and expected[510,516]==0
    assert np.isnan(expected[514,100]) and np.isnan(expected[510,100])
    fact=actual['targets']['hand.cog.tif']
    assert fact['source_valid_pixels']>fact['valid_pixels'] and fact['unreachable_pixels']>0
    assert fact['valid_pixels']+fact['unreachable_pixels']==fact['source_valid_pixels']
    for name,factor in [('feet.cog.tif',.3048),('survey-feet.cog.tif',1200/3937)]:
        np.testing.assert_allclose(module.expected_pixels(name),expected*factor,rtol=module.RTOL,atol=module.ATOL,equal_nan=True)
    assert np.isnan(module.expected_pixels('empty.cog.tif')).all() and np.isnan(module.expected_pixels('invalid.cog.tif')).all()


@pytest.mark.parametrize('problem',('uphill_before', 'uphill_after', 'uphill_empty', 'mixed_original', 'code', 'terminal', 'outside', 'invalid', 'cycle', 'break', 'mask_values', 'coverage', 'dem_coverage', 'alignment', 'dem_alignment', 'grid', 'band', 'stream_band', 'elevation_band', 'unit', 'unit_conflict'))
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_hand_d8_persistence_forty_two_failures_no_publication(hand_d8_persistence,tmp_path,problem,language):
    from workflow_engine import execute_workflow
    import operators.raster_compute as compute
    module,root,initial=hand_d8_persistence
    scene=importlib.import_module('scripts.test.raster-hand-d8-persistence-online')
    definition=scene.definition({n:n for n in module.SOURCES},'parent','hand.cog.tif',invalid=problem,language=language)
    target=tmp_path/'failed.cog.tif'
    for task in definition['tasks']:
        params=task['params']
        if task['operator']=='raster_load':
            path=root/params.pop('locator');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(path)}}}
        elif task['operator']=='raster_save':
            params.pop('target_parent_locator');params.pop('target_name');mode=params.pop('write_mode')
            params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{'kind':'file','format':'tiff','name':target.name,'write_mode':mode,'access':{'method':'mounted_path','path':str(target)}}}
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute(definition)
    assert error.value.error_code=='EXECUTION_FAILED'
    assert str(error.value)=='任务 hand 执行失败: '+scene.MESSAGES[problem][language=='en']
    assert not target.exists() and module.verify(root,list(module.TARGETS))['sources']==initial['sources']


@pytest.mark.parametrize('fault',['value','tiny_error','seam','river_nonzero','missed_zero','invalid_zero','mask','crs','grid','dtype','nodata','scale','unit','overviews','overview_value','sidecar','source_sidecar','missing_owner','missing','extra'])
def test_hand_d8_persistence_oracle_rejects_corruption(hand_d8_persistence,tmp_path,fault):
    module,original,_=hand_d8_persistence;root=tmp_path/'changed';shutil.copytree(original,root);path=root/'outputs'/'hand.cog.tif'
    if fault in ('value','tiny_error','seam','river_nonzero','missed_zero','invalid_zero','mask','crs','grid','nodata','scale','unit','overview_value'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if fault in ('value','tiny_error','seam','river_nonzero','missed_zero','invalid_zero'):
            y,x={'seam':(512,511),'river_nonzero':(511,515),'missed_zero':(514,100),'invalid_zero':(100,100)}.get(fault,(509,511))
            value=0 if fault in ('missed_zero','invalid_zero') else 12345. if fault=='value' else float(module.expected_pixels('hand.cog.tif')[y,x])+1e-6
            band.WriteRaster(x,y,1,1,np.array([value],np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        elif fault=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().Fill(255)
        elif fault=='crs':ds.SetProjection('')
        elif fault=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif fault=='nodata':band.SetNoDataValue(-999)
        elif fault=='unit':band.SetUnitType('foot')
        elif fault=='overview_value':band.GetOverview(0).Fill(1234)
        else:band.SetScale(2)
        band=ds=None
    elif fault in ('dtype','overviews'):
        alt=root/'replacement.tif';ds=gdal.Translate(str(alt),str(path),format='GTiff',outputType=gdal.GDT_Float32 if fault=='dtype' else gdal.GDT_Float64);ds=None;alt.replace(path)
    elif fault=='sidecar':(root/'outputs'/'hand.cog.tif.aux.xml').write_text('unexpected')
    elif fault=='source_sidecar':(root/'directions.tif.msk').write_text('unexpected')
    elif fault=='missing_owner':(root/'.addp-online-owner').unlink()
    elif fault=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_text('unexpected')
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))


@pytest.mark.parametrize('fault',['false_zero','no_height','outlet_height','no_unit_conversion','euclidean','flat_nodata'])
def test_hand_d8_independent_oracle_rejects_wrong_semantics(hand_d8_persistence,fault):
    module,_,_=hand_d8_persistence;name='feet.cog.tif' if fault=='no_unit_conversion' else 'hand.cog.tif'
    wrong=module.expected_pixels(name).copy()
    if fault=='false_zero':wrong[514,100]=0
    elif fault=='no_height':wrong[np.isfinite(wrong)]=0
    elif fault=='outlet_height':
        z=module.expected_elevations(name);wrong[509,511]=z[509,511]-z[511,518]
    elif fault=='no_unit_conversion':wrong=module.expected_pixels('hand.cog.tif').copy()
    elif fault=='flat_nodata':wrong[510,516]=np.nan
    else:wrong[509,511]=2*np.hypot(2,3)  # horizontal distance cannot replace an elevation difference
    with pytest.raises(module.FixtureError):module.hand_evidence(wrong,name)


@pytest.mark.parametrize('name,epsg,factor',[('hand.cog.tif',2277,1.),('feet.cog.tif',3857,.3048),('survey-feet.cog.tif',2222,1200/3937)])
def test_hand_d8_fixture_units_grid_and_output_mask(hand_d8_persistence,name,epsg,factor):
    module,root,_=hand_d8_persistence;ds=gdal.Open(str(root/'outputs'/name));band=ds.GetRasterBand(1);expected=module.expected_pixels(name)
    assert ds.GetSpatialRef().GetAuthorityCode(None)==str(epsg) and ds.GetGeoTransform()==module.TRANSFORM
    assert band.GetUnitType()=='metre' and band.DataType==gdal.GDT_Float64 and np.isnan(band.GetNoDataValue())
    assert ds.GetSpatialRef().GetLinearUnits()!=factor
    assert module.hand_evidence(band.ReadAsArray(),name)['metres_per_elevation_unit']==factor
    np.testing.assert_array_equal(band.GetMaskBand().ReadAsArray()!=0,np.isfinite(expected));band=ds=None
    ds=gdal.Open(str(root/'code.tif'));band=ds.GetRasterBand(2)
    assert band.DataType==gdal.GDT_UInt64 and band.GetNoDataValueAsUInt64()==2**53 and int(band.ReadAsArray()[509,100])==2**53+1;band=ds=None


@pytest.mark.parametrize('fault',['value','mask'])
def test_hand_d8_nearest_overview_oracle_rejects_full_pyramid_corruption(hand_d8_persistence,tmp_path,fault):
    module,root,_=hand_d8_persistence;path=tmp_path/'overview.tif'
    shutil.copyfile(root/'outputs'/'hand.cog.tif',path)
    ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1);overview=band.GetOverview(0)
    module.nearest_overviews(ds,module.expected_pixels('hand.cog.tif'),'hand.cog.tif')
    if fault=='value':overview.Fill(999)
    else:
        row,col=map(int,np.argwhere(np.isfinite(overview.ReadAsArray()))[0])
        overview.WriteRaster(col,row,1,1,np.array([np.nan],np.float64).tobytes(),buf_type=gdal.GDT_Float64)
    ds.FlushCache()
    with pytest.raises(module.FixtureError,match='complete nearest overview'):module.nearest_overviews(ds,module.expected_pixels('hand.cog.tif'),'hand.cog.tif')
    overview=band=ds=None


def test_hand_d8_fixture_source_receipt_rejects_any_byte_change(hand_d8_persistence,tmp_path,monkeypatch):
    import shutil
    module,original,initial=hand_d8_persistence
    root=tmp_path/'sources';shutil.copytree(original,root);secret=tmp_path/'secret';secret.mkdir()
    (secret/'hand-d8-source.json').write_text(json.dumps(initial))
    monkeypatch.setattr(module,'guard',lambda:secret)
    monkeypatch.setattr(module,'owned_root',lambda _:root)
    monkeypatch.setattr(module,'runtime_worker',lambda action,owned,names:module.verify(owned,names))
    monkeypatch.setattr(module.sys,'argv',['fixture','verify',*module.TARGETS])
    assert module.main('verify')['sources']==initial['sources']
    with (root/'bowl.tif').open('ab') as stream:stream.write(b'changed source bytes')
    with pytest.raises(module.FixtureError,match='source file changed'):module.main('verify')




def test_hand_d8_worker_runs_from_stdin_without_repository_imports(hand_d8_persistence,tmp_path):
    import os,subprocess,sys
    module,root,initial=hand_d8_persistence
    cwd=tmp_path/'empty';cwd.mkdir();scratch=tmp_path/'scratch';scratch.mkdir()
    env={**os.environ,'PYTHONPATH':'','TMPDIR':str(scratch),'PYTHONDONTWRITEBYTECODE':'1'}
    code=Path(module.__file__).read_text()
    seeded=tmp_path/'seeded';seeded.mkdir();(seeded/'outputs').mkdir();(seeded/'.addp-online-owner').write_text('owned')
    for action,owned,names in [('seed',seeded,[]),('verify',root,list(module.TARGETS))]:
        done=subprocess.run([sys.executable,'-I','-c','import sys; exec(compile(sys.stdin.read(), "<fixture-worker>", "exec"))','--worker',action,str(owned),*names],input=code,text=True,capture_output=True,cwd=cwd,env=env,timeout=150)
        assert done.returncode==0,done.stderr
        result=json.loads(done.stdout)
        assert set(result['sources'])==set(module.SOURCES)
        if action=='verify':
            assert result['sources']==initial['sources'] and result['runtime_workspaces_removed']
            assert len(result['targets'])==8 and all(t['unreachable_nodata_verified'] for t in result['targets'].values())
