"""Real GDAL regressions for independent DEM mean-curvature Hosted acceptance."""
import importlib,json,shutil
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

@pytest.fixture(scope='module')
def curvature_persistence(tmp_path_factory):
    module=importlib.import_module('business.scripts.online-raster-curvature-fixture')
    scene=importlib.import_module('scripts.test.raster-curvature-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('curvature-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('curvature-123-1')
    initial=module.seed(root)
    for name in module.TARGETS:
        definition=scene.definition({n:n for n in module.SOURCES},'parent',name)
        for task in definition['tasks']:
            params=task['params']
            if task['operator']=='raster_load':
                path=root/params.pop('locator');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(path)}}}
            elif task['operator']=='raster_save':
                params.pop('target_parent_locator');target=root/'outputs'/params.pop('target_name');mode=params.pop('write_mode')
                params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{'kind':'file','format':'tiff','name':target.name,'write_mode':mode,'access':{'method':'mounted_path','path':str(target)}}}
        result=execute_workflow(definition);assert result['status']=='success',result
        artifact=json.loads(result['final_result']);assert type(artifact['size_bytes']) is int and artifact['size_bytes']==target.stat().st_size>0
    return module,root,initial


def test_curvature_persistence_real_gdal_eight_cog_chains_and_complete_pixels(curvature_persistence):
    module,root,initial=curvature_persistence;facts=module.verify(root,list(module.TARGETS))
    assert facts['sources']==initial['sources'] and len(facts['targets'])==8
    for name,fact in facts['targets'].items():
        assert all(fact[k] for k in ('full_pixel_and_mask_verification','cog_valid','independent_quadratic_oracle_verified','complete_neighbourhood_verified','inverse_metre_unit_verified','nearest_overviews_verified','nine_equal_weights_verified'))
        assert fact['size_bytes']==(root/'outputs'/name).stat().st_size>0
        assert fact['overview_count']==(0 if name=='small.cog.tif' else 3)
        assert fact['has_overviews']==(name!='small.cog.tif')
        if name in ('invalid.cog.tif','sparse.cog.tif','small.cog.tif'):assert fact['valid_pixels']==0
        else:assert fact['valid_pixels']>0
    assert np.isfinite(module.expected_dem('sparse.cog.tif')).sum()>0
    assert not np.isfinite(module.expected_pixels('sparse.cog.tif')).any()
    assert module.expected_dem('small.cog.tif').shape==(2,7)
    expected=module.expected_pixels('curvature.cog.tif')
    for row,col,wanted in [(60,60,0.),(60,200,-.03),(60,360,.03),(340,60,0.),(340,360,1/12+1/27)]:
        assert expected[row,col]==pytest.approx(wanted,rel=1e-10,abs=1e-11)
    assert np.isnan(expected[[0,-1],:]).all() and np.isnan(expected[:,[0,-1]]).all()
    assert np.isnan(expected[512:515,511:514]).all() and np.isfinite(expected[500,500])
    for name in ('feet.cog.tif','survey-feet.cog.tif'):
        np.testing.assert_allclose(module.expected_pixels(name),expected,rtol=module.RTOL,atol=module.ATOL,equal_nan=True)
    for name in module.SOURCES:
        ds=gdal.Open(str(root/name));assert ds.GetFileList()==[str(root/name)];ds=None


@pytest.mark.parametrize('problem',('band','alpha','complex','rotated','south_up','geographic','no_crs','no_grid','unit','unit_conflict','scale','offset'))
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_curvature_persistence_twenty_four_failures_no_publication(curvature_persistence,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    module,root,initial=curvature_persistence;scene=importlib.import_module('scripts.test.raster-curvature-persistence-online')
    definition=scene.definition({n:n for n in module.SOURCES},'parent','curvature.cog.tif',invalid=problem,language=language);target=tmp_path/'failed.cog.tif'
    for task in definition['tasks']:
        params=task['params']
        if task['operator']=='raster_load':
            path=root/params.pop('locator');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(path)}}}
        elif task['operator']=='raster_save':
            params.pop('target_parent_locator');params.pop('target_name');mode=params.pop('write_mode');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{'kind':'file','format':'tiff','name':target.name,'write_mode':mode,'access':{'method':'mounted_path','path':str(target)}}}
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_curvature':workspaces.append(params['input_raster'].workspace)
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute(definition)
    assert error.value.error_code=='EXECUTION_FAILED'
    assert str(error.value)=='任务 curvature 执行失败: '+scene.MESSAGES[problem][language=='en']
    assert workspaces and all(not p.exists() for p in workspaces) and not target.exists()
    assert module.verify(root,list(module.TARGETS))['sources']==initial['sources']


@pytest.mark.parametrize('fault',['value','tiny_error','sign','edge_zero','hole_zero','mask','crs','grid','dtype','nodata','scale','unit','overviews','overview_value','sidecar','source_sidecar','missing_owner','missing','extra'])
def test_curvature_persistence_oracle_rejects_corruption(curvature_persistence,tmp_path,fault):
    module,original,_=curvature_persistence;root=tmp_path/'changed';shutil.copytree(original,root);path=root/'outputs'/'curvature.cog.tif'
    if fault in ('value','tiny_error','sign','edge_zero','hole_zero','mask','crs','grid','nodata','scale','unit','overview_value'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if fault in ('value','tiny_error','sign','edge_zero','hole_zero'):
            row,col=(0,50) if fault=='edge_zero' else (513,512) if fault=='hole_zero' else (60,200)
            wanted=module.expected_pixels('curvature.cog.tif')[row,col]
            value=0. if fault in ('edge_zero','hole_zero') else -wanted if fault=='sign' else wanted+1e-6 if fault=='tiny_error' else 12345.
            band.WriteRaster(col,row,1,1,np.array([value],np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        elif fault=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().Fill(255)
        elif fault=='crs':ds.SetProjection('')
        elif fault=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif fault=='nodata':band.SetNoDataValue(-999.)
        elif fault=='unit':band.SetUnitType('metre')
        elif fault=='overview_value':band.GetOverview(0).Fill(1234.)
        else:band.SetScale(2.)
        band=ds=None
    elif fault in ('dtype','overviews'):
        alt=root/'replacement.tif';ds=gdal.Translate(str(alt),str(path),format='GTiff',outputType=gdal.GDT_Float32 if fault=='dtype' else gdal.GDT_Float64);ds=None;alt.replace(path)
    elif fault=='sidecar':(root/'outputs'/'curvature.cog.tif.aux.xml').write_text('unexpected')
    elif fault=='source_sidecar':(root/'dem.tif.msk').write_text('unexpected')
    elif fault=='missing_owner':(root/'.addp-online-owner').unlink()
    elif fault=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_text('unexpected')
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS))


@pytest.mark.parametrize('model',['forced_centre','laplacian','wrong_units'])
def test_curvature_independent_oracle_rejects_wrong_semantics(curvature_persistence,tmp_path,model):
    module,original,_=curvature_persistence;root=tmp_path/'changed';shutil.copytree(original,root)
    values=module.expected_dem('curvature.cog.tif');windows=np.lib.stride_tricks.sliding_window_view(values,(3,3));valid=np.isfinite(windows).all(axis=(-2,-1))
    samples=windows[valid].reshape(-1,9);relative=(samples-samples[:,4:5]).T
    x=np.tile(np.arange(-1,2),3);y=np.repeat(np.arange(1,-2,-1),3);design=np.column_stack((x*x,x*y,y*y,x,y,np.ones(9)))
    fit=np.linalg.lstsq(design[:,:5] if model=='forced_centre' else design,relative,rcond=None)[0]
    a,b,c,p,q=fit[:5];p/=2.;q/=3.;r=2*a/4.;s=b/6.;t=2*c/9.
    wrong=np.full(values.shape,np.nan)
    curvature=-((1+q*q)*r-2*p*q*s+(1+p*p)*t)/(2*(1+p*p+q*q)**1.5)
    if model=='laplacian':curvature=-(r+t)
    if model=='wrong_units':curvature*=100.
    wrong[1:-1,1:-1][valid]=curvature
    ds=gdal.OpenEx(str(root/'outputs'/'curvature.cog.tif'),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);ds.GetRasterBand(1).WriteArray(wrong);ds=None
    with pytest.raises(module.FixtureError,match='complete pixel values differ'):module.verify(root,list(module.TARGETS))


def test_curvature_fixture_source_receipt_rejects_any_byte_change(curvature_persistence,tmp_path,monkeypatch):
    import shutil
    module,original,initial=curvature_persistence
    root=tmp_path/'sources';shutil.copytree(original,root);secret=tmp_path/'secret';secret.mkdir()
    (secret/'curvature-source.json').write_text(json.dumps(initial))
    monkeypatch.setattr(module,'guard',lambda:secret)
    monkeypatch.setattr(module,'owned_root',lambda _:root)
    monkeypatch.setattr(module,'runtime_worker',lambda action,owned,names:module.verify(owned,names))
    monkeypatch.setattr(module.sys,'argv',['fixture','verify',*module.TARGETS])
    assert module.main('verify')['sources']==initial['sources']
    with (root/'bowl.tif').open('ab') as stream:stream.write(b'changed source bytes')
    with pytest.raises(module.FixtureError,match='source file changed'):module.main('verify')




def test_curvature_worker_runs_from_stdin_without_repository_imports(curvature_persistence,tmp_path):
    import os,subprocess,sys
    module,root,initial=curvature_persistence
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
            assert len(result['targets'])==8 and all(t['complete_neighbourhood_verified'] for t in result['targets'].values())


@pytest.mark.parametrize('value',[1234.,float('nan')])
def test_curvature_nearest_overview_oracle_detects_values_without_layout_validation(curvature_persistence,tmp_path,value):
    module,root,_=curvature_persistence;path=tmp_path/'overview.tif';shutil.copy2(root/'outputs'/'curvature.cog.tif',path)
    ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1);overview=band.GetOverview(0)
    assert np.isfinite(overview.ReadAsArray()[30,100])
    overview.WriteRaster(100,30,1,1,np.array([value],np.float64).tobytes(),buf_type=gdal.GDT_Float64)
    with pytest.raises(module.FixtureError,match='complete nearest overview pixels or mask differ'):
        module.nearest_overviews(ds,module.expected_pixels('curvature.cog.tif'))
    overview=band=ds=None
