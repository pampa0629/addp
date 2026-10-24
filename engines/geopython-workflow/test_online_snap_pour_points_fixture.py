"""Real GDAL regression for the independent snap_pour_points Hosted fixture."""
import importlib
import json
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

@pytest.fixture(scope='module')
def snap_pour_points_persistence(tmp_path_factory):
    module=importlib.import_module('business.scripts.online-raster-snap-pour-points-fixture')
    scene=importlib.import_module('scripts.test.raster-snap-pour-points-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('snap_pour_points-export');(root/'outputs').mkdir()
    # The real NFS lifecycle always creates this ownership marker.
    (root/'.addp-online-owner').write_text('snap-pour-points-123-1')
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



def test_snap_pour_points_persistence_real_gdal_eight_cog_chains_and_complete_pixels(snap_pour_points_persistence):
    module,root,initial=snap_pour_points_persistence;actual=module.verify(root,list(module.TARGETS))
    assert actual['sources']==initial['sources'] and len(actual['targets'])==8
    for name,fact in actual['targets'].items():
        assert all(fact[key] for key in ('full_pixel_and_mask_verification','cog_valid','has_overviews','snap_verified','point_ids_verified','unselected_zero_verified','radius_and_tie_verified','unit_cleared_verified'))
        assert fact['valid_pixels']>0 and fact['zero_pixels']>0 and fact['unit']==''
        assert fact['size_bytes']==(root/'outputs'/name).stat().st_size>0
    assert module.crs('foot.cog.tif').GetLinearUnits()==.3048
    assert module.crs('survey.cog.tif').GetLinearUnits()==pytest.approx(1200/3937,rel=1e-15)
    np.testing.assert_array_equal(module.expected_pixels('zero.cog.tif'),module.expected_points('zero.cog.tif'))
    assert module.expected_pixels('foot.cog.tif')[512,512]==7
    assert module.expected_pixels('survey.cog.tif')[511,512]==7
    assert module.expected_pixels('snap.cog.tif')[512,512]==7
    # Row and column ties are separate, known mathematical cases, not source-order assertions.
    assert module.expected_pixels('reclass.cog.tif')[99,100]==901
    assert module.expected_pixels('reclass.cog.tif')[511,51]==880


@pytest.mark.parametrize('corruption',['value','one_unit','fraction','seam','invalid_center','mask','crs','grid','dtype','nodata','scale','unit','overviews','overview_sum','sidecar','source_sidecar','missing_owner','missing','extra'])
def test_snap_pour_points_persistence_oracle_rejects_corruption(snap_pour_points_persistence,tmp_path,corruption):
    import shutil
    module,original,_=snap_pour_points_persistence
    root=tmp_path/'changed';shutil.copytree(original,root);path=root/'outputs'/'snap.cog.tif'
    if corruption in ('value','one_unit','fraction','seam','invalid_center','mask','crs','grid','nodata','scale','unit','overview_sum'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
        band=ds.GetRasterBand(1)
        if corruption in ('value','one_unit','fraction','seam','invalid_center'):
            y,x=(100,100) if corruption=='invalid_center' else (511,512) if corruption=='seam' else (511,518)
            value=module.expected_pixels('snap.cog.tif')[y,x]+(1e-6 if corruption=='fraction' else 1) if corruption in ('fraction','one_unit') else 12345.
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
    elif corruption=='sidecar':(root/'outputs'/'snap.cog.tif.aux.xml').write_text('unexpected')
    elif corruption=='source_sidecar':(root/'directions.tif.aux.xml').write_text('unexpected')
    elif corruption=='missing_owner':(root/'.addp-online-owner').unlink()
    elif corruption=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_bytes(path.read_bytes())
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))


@pytest.mark.parametrize('problem',['collision','duplicate','empty','all_invalid','counts_values','points_values','coverage','alignment','radius','band','points_band','grid','nested'])
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_snap_pour_points_persistence_invalid_parameters_fail_without_publication(snap_pour_points_persistence,tmp_path,monkeypatch,problem,language):
    import shutil
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    module,original,initial=snap_pour_points_persistence;root=tmp_path/'failed';shutil.copytree(original,root)
    scene=importlib.import_module('scripts.test.raster-snap-pour-points-persistence-online')
    definition=scene.definition({n:n for n in module.SOURCES},'parent','snap.cog.tif',invalid=problem,language=language)
    for task in definition['tasks']:
        params=task['params']
        if task['operator']=='raster_load':
            source=root/params.pop('locator');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(source)}}}
        elif task['operator']=='raster_save':
            params.pop('target_parent_locator');target=root/'outputs'/params.pop('target_name');mode=params.pop('write_mode')
            params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{'kind':'file','format':'tiff','name':target.name,'write_mode':mode,'access':{'method':'mounted_path','path':str(target)}}}
    import workflow_engine
    completed=[];workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        result=execute(name,params);completed.append(name)
        if hasattr(result,'workspace'):workspaces.append(result.workspace)
        return result
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    with pytest.raises(WorkflowExecutionError) as caught:GeoPythonWorkflowRunner(language=language).execute(definition)
    assert workspaces and all(not path.exists() for path in workspaces)
    assert 'raster_save' not in completed
    assert ('raster_snap_pour_points' in completed)==(problem=='nested')
    assert caught.value.error_code=='EXECUTION_FAILED'
    assert str(caught.value)=='任务 '+('watershed' if problem=='nested' else 'snap')+' 执行失败: '+scene.MESSAGES[problem][language=='en']
    assert not target.exists() and module.verify(root,list(module.TARGETS))['sources']==initial['sources']


def test_snap_pour_points_fixture_cross_divide_uses_two_distinct_open_outlets():
    module=importlib.import_module('business.scripts.online-raster-snap-pour-points-fixture')
    directions=module.expected_directions('snap.cog.tif')
    def outlet(row,col):
        steps={1:(0,1),4:(1,0),64:(-1,0)}
        while directions[row,col]!=0:
            dy,dx=steps[directions[row,col]];row,col=row+dy,col+dx
        return row,col
    assert outlet(511,510)!=outlet(512,512)
    assert module.expected_points('snap.cog.tif')[511,510]==module.expected_pixels('snap.cog.tif')[512,512]==7
    # Five source units exactly: radius=5 includes the cross-divide diagonal centre.
    assert ((512-511)*3)**2+((512-510)*2)**2==25


def test_snap_pour_points_fixture_source_receipt_rejects_byte_change(snap_pour_points_persistence,tmp_path,monkeypatch):
    import shutil
    module,original,initial=snap_pour_points_persistence;root=tmp_path/'sources';shutil.copytree(original,root);secret=tmp_path/'secret';secret.mkdir()
    (secret/'snap-pour-points-source.json').write_text(json.dumps(initial));monkeypatch.setattr(module,'guard',lambda:secret);monkeypatch.setattr(module,'owned_root',lambda _:root)
    monkeypatch.setattr(module,'runtime_worker',lambda action,owned,names:module.verify(owned,names));monkeypatch.setattr(module.sys,'argv',['fixture','verify',*module.TARGETS])
    assert module.main('verify')['sources']==initial['sources']
    with (root/'points.tif').open('ab') as stream:stream.write(b'changed source bytes')
    with pytest.raises(module.FixtureError,match='source file changed'):module.main('verify')


@pytest.mark.parametrize('name',['invalid-counts.tif','invalid-points.tif'])
def test_snap_pour_points_fixture_preserves_uint64_nodata_adjacent_illegal_value(snap_pour_points_persistence,name):
    module,root,_=snap_pour_points_persistence;ds=gdal.Open(str(root/name));band=ds.GetRasterBand(2)
    assert band.DataType==gdal.GDT_UInt64 and band.GetNoDataValueAsUInt64()==2**53
    assert int(band.ReadAsArray()[509,100])==2**53+1 and int(band.ReadAsArray()[0,0])==2**53;band=ds=None


def test_snap_pour_points_fixture_fresh_physical_checks(snap_pour_points_persistence):
    module,root,initial=snap_pour_points_persistence
    for method in (module.expected_directions,module.expected_points,module.expected_snapped,module.expected_pixels):method.cache_clear()
    result=module.verify(root,list(module.TARGETS))
    assert result==module.verify(root,list(module.TARGETS)) and result['sources']==initial['sources']


@pytest.mark.parametrize('action',['seed','verify'])
def test_snap_pour_points_worker_is_self_contained_without_repository_imports(snap_pour_points_persistence,tmp_path,action):
    import os,sys
    from operators._native_process import run
    module,original,initial=snap_pour_points_persistence
    empty=tmp_path/'empty';empty.mkdir();private=tmp_path/'tmp';private.mkdir()
    root=original if action=='verify' else tmp_path/'seed'
    if action=='seed':root.mkdir();(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('owned')
    env=dict(os.environ,PYTHONPATH='',TMPDIR=str(private))
    result=run([sys.executable,'-','--worker',action,str(root),*(module.TARGETS if action=='verify' else ())],
               input=Path(module.__file__).read_text(),cwd=empty,env=env,text=True,capture_output=True,timeout=120)
    assert result.returncode==0,result.stderr
    facts=json.loads(result.stdout)
    assert set(facts['sources'])==set(module.SOURCES)
    assert all(facts['sources'][name]==module.digest(root/name) for name in module.SOURCES)
    if action=='verify':assert facts['runtime_workspaces_removed'] is True and set(facts['targets'])==set(module.TARGETS)
