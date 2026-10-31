"""Actual GDAL area sieve COGs and immutable independent oracle evidence."""
import importlib,json,os,shutil,subprocess,sys
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest
from workflow_engine import execute_workflow,GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError
sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
f=importlib.import_module('business.scripts.online-raster-sieve-area-fixture')
m=importlib.import_module('scripts.test.raster-sieve-area-persistence-online')
helper=importlib.import_module('test_online_focal_percentile_fixture')

@pytest.fixture(scope='module')
def area_scene(tmp_path_factory,request):
    root=tmp_path_factory.mktemp('sieve-area-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('sieve-area-123-1');initial=f.seed(root)
    for name in f.TARGETS:
        result=execute_workflow(helper.native_definition(m,f,root,name));assert result['status']=='success',result
        assert json.loads(result['final_result'])['size_bytes']==(root/'outputs'/name).stat().st_size>0
    return root,initial,helper.cache_for(f,root,request)

def test_area_sieve_hosted_sixteen_real_cogs_complete_oracle_protected_controls_and_original_bytes(area_scene,tmp_path):
    root,initial,control=area_scene;report=f.verify(root,list(f.TARGETS),control)
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
    protected=f.expected_pixels('area-four.cog.tif');plain=f.expected_pixels('omitted.cog.tif')
    assert protected[20,25]==9 and plain[20,25]==2
    assert protected[200,25]==7 and protected[210,22]==8 and protected[220,25]==9 and protected[220,27]==2
    assert protected[90,90]==7 and f.expected_pixels('area-eight.cog.tif')[90,90]==0
    assert f.expected_pixels('equal.cog.tif')[260,20]==4 and f.expected_pixels('below.cog.tif')[260,20]==4
    assert f.expected_pixels('above.cog.tif')[260,20]==6
    assert f.expected_pixels('equal.cog.tif')[270,20]==5 and f.expected_pixels('above.cog.tif')[270,20]==8
    assert f.expected_pixels('feet.cog.tif')[260,20]==6 and f.expected_pixels('survey-feet.cog.tif')[260,20]==6
    assert f.expected_pixels('resolution-10.cog.tif')[280,20]==6 and f.expected_pixels('resolution-30.cog.tif')[280,20]==4
    assert not np.isfinite(f.expected_pixels('invalid.cog.tif')).any()
    for name in ('int64.cog.tif','uint64.cog.tif'):
        expected=f.expected_pixels(name);assert expected[20,20]==9 and expected[30,30]==0 and np.isnan(expected[512,517])
    for name,fact in report['targets'].items():
        assert fact['area_comparison_verified']is True and fact['linear_units_verified']is True and fact['threshold_contract']==f.threshold_contract(name)

@pytest.mark.parametrize('problem',f.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_area_sieve_hosted_sixty_four_exact_submitted_failures_no_output_and_clean_workspace(area_scene,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    root,initial,control=area_scene;target=tmp_path/'failed.cog.tif';dag=helper.native_definition(m,f,root,'area-four.cog.tif',invalid=problem,language=language,target=target)
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
def test_area_sieve_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected(area_scene,tmp_path,request,fault):
    original,_,_=area_scene;root=tmp_path/'changed';shutil.copytree(original,root);control=helper.cache_for(f,root,request)
    path=root/'outputs'/'area-four.cog.tif'
    if fault=='sidecar':(root/'outputs'/'extra.tif').write_bytes(b'extra')
    elif fault=='missing':path.unlink()
    elif fault=='source-mask':
        ds=gdal.Open(str(root/'classes.tif'),gdal.GA_Update);ds.GetRasterBand(1).GetMaskBand().Fill(0);ds=None
    else:
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if fault in ('value','one-ulp','hole'):
            row,col=(510,511) if fault=='hole' else (60,60);wanted=f.expected_pixels('area-four.cog.tif')[row,col]
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
def test_area_sieve_hosted_original_cog_archive_never_repairs_or_overwrites(area_scene,tmp_path,fault):
    root,_,control=area_scene;report=f.verify(root,list(f.TARGETS),control);previous=f.archive_cogs(root,tmp_path,report,{})
    path=tmp_path/'sieve-area-cogs'/'area-four.cog.tif'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'sieve-area-cogs-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'sieve-area-cogs-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.tif').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/'outputs'/'area-four.cog.tif')
    with pytest.raises(f.FixtureError):f.archive_cogs(root,tmp_path,report,previous)

@pytest.mark.parametrize('fault',['bytes','missing','extra','scope','shape'])
def test_area_sieve_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change(area_scene,tmp_path,request,fault):
    original,_,_=area_scene;root=tmp_path/'cache';shutil.copytree(original,root);control=helper.cache_for(f,root,request);directory=f.oracle_path(control['scope']);path=directory/'area-four.cog.tif.npy'
    if fault=='bytes':path.chmod(0o600);path.write_bytes(path.read_bytes()+b'x')
    elif fault=='missing':path.unlink()
    elif fault=='extra':(directory/'unexpected.npy').write_bytes(b'x')
    elif fault=='scope':control['scope']['run_id']='sieve-area-999-1'
    else:
        path.chmod(0o600)
        with path.open('wb') as out:np.save(out,np.array([[1.]]))
        path.chmod(0o400);control['arrays']['area-four.cog.tif']=f.digest(path)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS),control)

def test_area_sieve_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup(area_scene,tmp_path):
    original,initial,control=area_scene;root=tmp_path/'child';shutil.copytree(original,root);control={'scope':{**control['scope'],'source_root':str(root.resolve())}};scratch=tmp_path/'scratch';scratch.mkdir();cwd=tmp_path/'empty';cwd.mkdir()
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
def test_area_sieve_hosted_frozen_expected_archive_rejects_tampering(area_scene,tmp_path,fault):
    root,_,_=area_scene;previous=f.archive_expectations(root,tmp_path,{})
    path=tmp_path/'sieve-area-expectations'/'area-four.cog.tif.npy'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'sieve-area-expectations-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'sieve-area-expectations-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.npy').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/f.EXPECTED_EXPORT/'area-four.cog.tif.npy')
    with pytest.raises(f.FixtureError):f.archive_expectations(root,tmp_path,previous)

@pytest.mark.parametrize('fault',['protected_replaced','small_protected_donor','large_protected_refused','small_transit','inclusive_threshold','rounded_area','wrong_unit','wrong_connectivity','implicit_scale','zero_discarded','nodata_fill','resolution_ignored','empty_protection_applied','donor_equal_rejected'])
def test_area_sieve_hosted_independent_oracle_rejects_area_protection_and_original_semantic_tampering(area_scene,tmp_path,request,fault):
    original,_,_=area_scene;root=tmp_path/'semantics';shutil.copytree(original,root);control=helper.cache_for(f,root,request)
    name={'inclusive_threshold':'equal.cog.tif','rounded_area':'feet.cog.tif','wrong_unit':'survey-feet.cog.tif',
          'resolution_ignored':'resolution-30.cog.tif','empty_protection_applied':'empty.cog.tif','donor_equal_rejected':'equal.cog.tif','zero_discarded':'area-eight.cog.tif'}.get(fault,'area-four.cog.tif')
    bad=f.expected_pixels(name).copy()
    if fault=='protected_replaced':bad[20,25]=2
    elif fault=='small_protected_donor':bad[210,22]=9
    elif fault=='large_protected_refused':bad[200,25]=8
    elif fault=='small_transit':bad[220,25:29]=2
    elif fault=='inclusive_threshold':bad[260,20:22]=6
    elif fault in('rounded_area','wrong_unit'):bad[260,20:22]=4
    elif fault=='donor_equal_rejected':bad[270,20]=8
    elif fault=='resolution_ignored':bad[280,20:22]=6
    elif fault=='wrong_connectivity':bad=f.expected_pixels('area-eight.cog.tif').copy()
    elif fault=='implicit_scale':bad=bad*2+7
    elif fault=='zero_discarded':bad[np.isfinite(bad)&(bad==0)]=np.nan
    elif fault=='nodata_fill':bad[510,511]=0
    else:bad[20,25]=9
    assert not np.array_equal(bad,f.expected_pixels(name),equal_nan=True)
    path=root/'outputs'/name;ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);ds.GetRasterBand(1).WriteArray(bad);ds=None
    with pytest.raises(f.FixtureError,match='complete independent area sieve pixels or mask differ'):f.verify(root,list(f.TARGETS),control)


@pytest.mark.parametrize('language',['zh-cn','en'])
def test_area_sieve_native_only_integral_float_rejection(area_scene,tmp_path,monkeypatch,language):
    """Native float admission; excluded from the Hosted matrix after JSON normalization."""
    import workflow_engine
    root,initial,control=area_scene;target=tmp_path/'native-only-float.cog.tif'
    dag=helper.native_definition(m,f,root,'area-four.cog.tif',invalid='protected_item_object',language=language,target=target)
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


def test_area_sieve_source_crs_roundtrip_and_complete_native_source_facts(tmp_path):
    (tmp_path/'outputs').mkdir();f.seed(tmp_path);f.verify_sources(tmp_path)
