"""Actual GDAL protected sieve COGs and immutable independent oracle evidence."""
import importlib,json,os,shutil,subprocess,sys
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest
from workflow_engine import execute_workflow,GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError
sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
f=importlib.import_module('business.scripts.online-raster-sieve-protected-fixture')
m=importlib.import_module('scripts.test.raster-sieve-protected-persistence-online')
helper=importlib.import_module('test_online_focal_percentile_fixture')

@pytest.fixture(scope='module')
def protected_scene(tmp_path_factory,request):
    root=tmp_path_factory.mktemp('sieve-protected-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('sieve-protected-123-1');initial=f.seed(root)
    for name in f.TARGETS:
        result=execute_workflow(helper.native_definition(m,f,root,name));assert result['status']=='success',result
        assert json.loads(result['final_result'])['size_bytes']==(root/'outputs'/name).stat().st_size>0
    return root,initial,helper.cache_for(f,root,request)

def test_protected_sieve_hosted_sixteen_real_cogs_complete_oracle_protected_controls_and_original_bytes(protected_scene,tmp_path):
    root,initial,control=protected_scene;report=f.verify(root,list(f.TARGETS),control)
    assert report['sources']==initial['sources'] and report['full_source_pixels_verified'] is True and len(report['targets'])==16
    archived=f.archive_cogs(root,tmp_path,report,{})
    expectations=f.archive_expectations(root,tmp_path,{})
    assert len(expectations)==16
    for name in f.TARGETS:
        path=tmp_path/expectations[name+'.npy']['path']
        assert path.read_bytes()==(f.oracle_path(control['scope'])/(name+'.npy')).read_bytes()
        np.testing.assert_array_equal(np.load(path,allow_pickle=False),f.expected_pixels(name))
    for name,fact in report['targets'].items():
        assert fact['full_pixel_and_mask_verification'] is True and fact['overview_count']>0
        assert (tmp_path/archived[name]['path']).read_bytes()==(root/'outputs'/name).read_bytes()
    assert f.verify(root,list(f.TARGETS),control)['comparisons']==[list(pair)for pair in f.COMPARISONS]
    protected=f.expected_pixels('protected-four.cog.tif');plain=f.expected_pixels('omitted.cog.tif')
    assert protected[20,25]==9 and plain[20,25]==2
    assert f.expected_pixels('zero-negative.cog.tif')[90,90]==0 and f.expected_pixels('zero-negative.cog.tif')[230,20]==-5
    donors=f.expected_pixels('donor-eligibility.cog.tif')
    assert donors[200,25]==7 and donors[210,22]==8 and donors[220,25]==9 and donors[220,27]==2
    assert protected[90,90]==7 and f.expected_pixels('protected-eight.cog.tif')[90,90]==0
    original,valid=f.expected_inputs('all-protected.cog.tif')
    np.testing.assert_array_equal(f.expected_pixels('all-protected.cog.tif'),np.where(valid,original,np.nan))
    assert not np.isfinite(f.expected_pixels('invalid.cog.tif')).any()
    for name in ('int64.cog.tif','uint64.cog.tif'):
        expected=f.expected_pixels(name);assert expected[20,20]==9 and expected[30,30]==0 and np.isnan(expected[512,517])

@pytest.mark.parametrize('problem',f.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_protected_sieve_hosted_fifty_two_exact_submitted_failures_no_output_and_clean_workspace(protected_scene,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    root,initial,control=protected_scene;target=tmp_path/'failed.cog.tif';dag=helper.native_definition(m,f,root,'protected-four.cog.tif',invalid=problem,language=language,target=target)
    workspaces=[];original=workflow_engine._execute_operator
    def track(name,params):
        if name=='raster_sieve':workspaces.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',track)
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute(dag)
    assert error.value.error_code=='EXECUTION_FAILED' and str(error.value)=='任务 sieve 执行失败: '+m.MESSAGES[problem][language=='en']
    assert workspaces and all(not p.exists() for p in workspaces) and not target.exists()
    assert f.verify(root,list(f.TARGETS),control)['sources']==initial['sources']

@pytest.mark.parametrize('fault',['value','one-ulp','hole','mask','grid','crs','nodata','unit','scale','overview','source-mask','sidecar','missing'])
def test_protected_sieve_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected(protected_scene,tmp_path,request,fault):
    original,_,_=protected_scene;root=tmp_path/'changed';shutil.copytree(original,root);control=helper.cache_for(f,root,request)
    path=root/'outputs'/'protected-four.cog.tif'
    if fault=='sidecar':(root/'outputs'/'extra.tif').write_bytes(b'extra')
    elif fault=='missing':path.unlink()
    elif fault=='source-mask':
        ds=gdal.Open(str(root/'classes.tif'),gdal.GA_Update);ds.GetRasterBand(1).GetMaskBand().Fill(0);ds=None
    else:
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if fault in ('value','one-ulp','hole'):
            row,col=(510,511) if fault=='hole' else (60,60);wanted=f.expected_pixels('protected-four.cog.tif')[row,col]
            value=0. if fault=='hole' else np.nextafter(wanted,np.inf) if fault=='one-ulp' else 123.
            band.WriteArray(np.array([[value]]),col,row)
        elif fault=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().Fill(255)
        elif fault=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif fault=='crs':ds.SetProjection('')
        elif fault=='nodata':band.SetNoDataValue(-999.)
        elif fault=='unit':band.SetUnitType('metre')
        elif fault=='scale':band.SetScale(2.)
        else:band.GetOverview(0).Fill(99.)
        band=ds=None
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS),control)

@pytest.mark.parametrize('fault',['bytes','deleted','directory','manifest','extra','symlink'])
def test_protected_sieve_hosted_original_cog_archive_never_repairs_or_overwrites(protected_scene,tmp_path,fault):
    root,_,control=protected_scene;report=f.verify(root,list(f.TARGETS),control);previous=f.archive_cogs(root,tmp_path,report,{})
    path=tmp_path/'sieve-protected-cogs'/'protected-four.cog.tif'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'sieve-protected-cogs-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'sieve-protected-cogs-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.tif').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/'outputs'/'protected-four.cog.tif')
    with pytest.raises(f.FixtureError):f.archive_cogs(root,tmp_path,report,previous)

@pytest.mark.parametrize('fault',['bytes','missing','extra','scope','shape'])
def test_protected_sieve_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change(protected_scene,tmp_path,request,fault):
    original,_,_=protected_scene;root=tmp_path/'cache';shutil.copytree(original,root);control=helper.cache_for(f,root,request);directory=f.oracle_path(control['scope']);path=directory/'protected-four.cog.tif.npy'
    if fault=='bytes':path.chmod(0o600);path.write_bytes(path.read_bytes()+b'x')
    elif fault=='missing':path.unlink()
    elif fault=='extra':(directory/'unexpected.npy').write_bytes(b'x')
    elif fault=='scope':control['scope']['run_id']='sieve-protected-999-1'
    else:
        path.chmod(0o600)
        with path.open('wb') as out:np.save(out,np.array([[1.]]))
        path.chmod(0o400);control['arrays']['protected-four.cog.tif']=f.digest(path)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS),control)

def test_protected_sieve_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup(protected_scene,tmp_path):
    original,initial,control=protected_scene;root=tmp_path/'child';shutil.copytree(original,root);control={'scope':{**control['scope'],'source_root':str(root.resolve())}};scratch=tmp_path/'scratch';scratch.mkdir();cwd=tmp_path/'empty';cwd.mkdir()
    env={**os.environ,'PYTHONPATH':'','TMPDIR':str(scratch),'PYTHONDONTWRITEBYTECODE':'1'}
    def call(action,payload,names=()):
        result=subprocess.run([sys.executable,'-I','-c','import sys;exec(compile(sys.stdin.read(),"<worker>","exec"))','--worker',action,str(root),json.dumps(payload),*names],input=Path(f.__file__).read_bytes(),cwd=cwd,env=env,capture_output=True,timeout=150)
        assert result.returncode==0,result.stderr.decode();return json.loads(result.stdout)
    child=call('prepare-oracle',control['scope'])
    try:
        report=call('verify',child,list(f.TARGETS));assert report['sources']==initial['sources'] and report['oracle_cache']['full_target_checks']==16
    finally:assert call('cleanup-oracle',child)['oracle_cache_removed'] is True
    assert not list(scratch.iterdir())

@pytest.mark.parametrize('fault',['bytes','deleted','directory','manifest','extra','symlink'])
def test_protected_sieve_hosted_frozen_expected_archive_rejects_tampering(protected_scene,tmp_path,fault):
    root,_,_=protected_scene;previous=f.archive_expectations(root,tmp_path,{})
    path=tmp_path/'sieve-protected-expectations'/'protected-four.cog.tif.npy'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'sieve-protected-expectations-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'sieve-protected-expectations-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.npy').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/f.EXPECTED_EXPORT/'protected-four.cog.tif.npy')
    with pytest.raises(f.FixtureError):f.archive_expectations(root,tmp_path,previous)

@pytest.mark.parametrize('fault',['protected_replaced','small_protected_donor','large_protected_refused','small_transit','inclusive_threshold','growth','global_classes','wrong_connectivity','implicit_scale','zero_discarded','nodata_fill','no_donor','empty_protection_applied'])
def test_protected_sieve_hosted_independent_oracle_rejects_wrong_protection_and_old_semantics(protected_scene,tmp_path,request,fault):
    original,_,_=protected_scene;root=tmp_path/'semantics';shutil.copytree(original,root);control=helper.cache_for(f,root,request)
    name='donor-eligibility.cog.tif'if fault in('small_protected_donor','large_protected_refused','small_transit')else'omitted.cog.tif'if fault in('inclusive_threshold','growth','global_classes')else'zero-negative.cog.tif'if fault=='zero_discarded'else'unlocated.cog.tif'if fault=='no_donor'else'empty.cog.tif'if fault=='empty_protection_applied'else'protected-four.cog.tif'
    bad=f.expected_pixels(name).copy()
    if fault=='protected_replaced':bad[20,25]=2
    elif fault=='small_protected_donor':bad[210,22]=9
    elif fault=='large_protected_refused':bad[200,25]=8
    elif fault=='small_transit':bad[220,25:29]=2
    elif fault=='inclusive_threshold':bad[60,25:30]=2
    elif fault=='growth':bad[50,25:27]=1
    elif fault=='global_classes':bad[70,25]=1
    elif fault=='wrong_connectivity':bad=f.expected_pixels('protected-eight.cog.tif').copy()
    elif fault=='implicit_scale':bad=bad*2+7
    elif fault=='zero_discarded':bad[90,90]=np.nan
    elif fault=='nodata_fill':bad[510,511]=0
    elif fault=='empty_protection_applied':bad[20,25]=9
    else:bad[np.isfinite(bad)]=7
    assert not np.array_equal(bad,f.expected_pixels(name),equal_nan=True)
    path=root/'outputs'/name;ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);ds.GetRasterBand(1).WriteArray(bad);ds=None
    with pytest.raises(f.FixtureError,match='complete independent protected sieve pixels or mask differ'):f.verify(root,list(f.TARGETS),control)


@pytest.mark.parametrize('language',['zh-cn','en'])
def test_protected_sieve_native_only_integral_float_rejection(protected_scene,tmp_path,monkeypatch,language):
    """Native float admission; excluded from the Hosted matrix after JSON normalization."""
    import workflow_engine
    root,initial,control=protected_scene;target=tmp_path/'native-only-float.cog.tif'
    dag=helper.native_definition(m,f,root,'protected-four.cog.tif',invalid='protected_item_object',language=language,target=target)
    next(task for task in dag['tasks']if task['id']=='sieve')['params']['protected_values']=[1.0]
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_sieve':workspaces.append(params['input_raster'].workspace)
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    with pytest.raises(WorkflowExecutionError)as error:GeoPythonWorkflowRunner(language=language).execute(dag)
    assert error.value.error_code=='EXECUTION_FAILED'and str(error.value)=='任务 sieve 执行失败: '+m.MESSAGES['protected_item_object'][language=='en']
    assert workspaces and all(not p.exists()for p in workspaces)and not target.exists()
    assert f.verify(root,list(f.TARGETS),control)['sources']==initial['sources']
