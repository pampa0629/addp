"""Real GDAL preparation for independently archived SIEVE Hosted acceptance."""
import importlib,json,os,shutil,subprocess,sys
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest

def native_definition(scene,module,root,name,*,invalid=None,language='zh-cn',target=None):
    definition=scene.definition({n:n for n in module.SOURCES},'parent',name,invalid=invalid,language=language)
    for task in definition['tasks']:
        params=task['params']
        if task['operator']=='raster_load':
            path=root/params.pop('locator');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(path)}}}
        elif task['operator']=='raster_save':
            params.pop('target_parent_locator');path=target or root/'outputs'/params['target_name'];params.pop('target_name');mode=params.pop('write_mode')
            params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{'kind':'file','format':'tiff','name':path.name,'write_mode':mode,'access':{'method':'mounted_path','path':str(path)}}}
    return definition

def cache_for(module,root,request):
    scope={'schema':module.CACHE_SCHEMA,'run_id':(root/'.addp-online-owner').read_text(),'fixture_sha256':module.digest(Path(module.__file__))['sha256'],
           'source_root':str(root.resolve()),'sources':{n:module.digest(root/n)for n in module.SOURCES}}
    control=module.prepare_oracle(root,scope)
    directory=module.oracle_path(scope)
    request.addfinalizer(lambda:shutil.rmtree(directory,ignore_errors=True))
    return control

@pytest.fixture(scope='module')
def sieve_persistence(tmp_path_factory,request):
    module=importlib.import_module('business.scripts.online-raster-sieve-fixture');scene=importlib.import_module('scripts.test.raster-sieve-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('sieve-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('sieve-123-1');initial=module.seed(root)
    for name in module.TARGETS:
        result=execute_workflow(native_definition(scene,module,root,name));assert result['status']=='success',result
        artifact=json.loads(result['final_result']);assert type(artifact['size_bytes']) is int and artifact['size_bytes']==(root/'outputs'/name).stat().st_size>0
    control=cache_for(module,root,request)
    return module,root,initial,control


def test_sieve_persistence_real_gdal_six_chains_exact_pixels_mask_and_original_cogs(sieve_persistence,tmp_path):
    module,root,initial,control=sieve_persistence;facts=module.verify(root,list(module.TARGETS),control)
    assert facts['sources']==initial['sources'] and len(facts['targets'])==6
    archived=module.archive_cogs(root,tmp_path,facts);assert len(archived)==6
    for name,fact in facts['targets'].items():
        assert all(fact[k]for k in ('full_pixel_and_mask_verification','cog_valid','independent_bfs_oracle_verified','original_coverage_verified','frozen_components_verified','classification_encoding_verified','nearest_overviews_verified','explicit_connectivity_verified','legal_zero_verified'))
        assert fact['size_bytes']==(root/'outputs'/name).stat().st_size>0 and fact['overview_count']==3
        assert(tmp_path/archived[name]['path']).read_bytes()==(root/'outputs'/name).read_bytes()
        ds=gdal.Open(str(root/'outputs'/name));np.testing.assert_array_equal(ds.ReadAsArray(),module.expected_pixels(name));ds=None
        if name=='invalid.cog.tif':assert fact['valid_pixels']==0
        else:assert fact['valid_pixels']>0
    assert not np.array_equal(module.expected_pixels('four.cog.tif'),module.expected_pixels('eight.cog.tif'),equal_nan=True)
    original,valid=module.expected_inputs('identity.cog.tif')
    np.testing.assert_array_equal(module.expected_pixels('identity.cog.tif'),np.where(valid,original,np.nan))
    original,valid=module.expected_inputs('no-donor.cog.tif')
    np.testing.assert_array_equal(module.expected_pixels('no-donor.cog.tif'),np.where(valid,original,np.nan))
    expected=module.expected_pixels('four.cog.tif')
    assert expected[20,25]==2 and expected[30,25]==-5 # largest original donor / class tie
    assert expected[40,25]==1 and expected[40,27]==2 # no small-component transit
    assert expected[50,26]==2 # frozen size6, not a grown size5
    assert expected[60,25]==9 and expected[500,512]==3 # equal threshold / cross-block
    assert expected[70,25]==2 # disconnected class1 never counted together
    assert expected[113,114]==9 and module.expected_pixels('eight.cog.tif')[113,114]==-2
    assert expected[90,90]==7 and module.expected_pixels('eight.cog.tif')[90,90]==0
    assert expected[512,450]==-(2**53-1) and expected[151,151]==2**53-1
    assert facts['targets']['four.cog.tif']['zero_pixels']==0


@pytest.mark.parametrize('problem',('band','threshold_zero','threshold_negative','threshold_fractional','threshold_boolean','connectivity_value','connectivity_fractional','connectivity_boolean','fractional','unsafe','int64','uint64','alpha','complex'))
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_sieve_persistence_twenty_eight_accurate_bilingual_failures_no_publication(sieve_persistence,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    module,root,initial,control=sieve_persistence;scene=importlib.import_module('scripts.test.raster-sieve-persistence-online');target=tmp_path/'failed.cog.tif'
    definition=native_definition(scene,module,root,'four.cog.tif',invalid=problem,language=language,target=target)
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_sieve':workspaces.append(params['input_raster'].workspace)
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    with pytest.raises(WorkflowExecutionError)as error:GeoPythonWorkflowRunner(language=language).execute(definition)
    assert error.value.error_code=='EXECUTION_FAILED'and str(error.value)=='任务 sieve 执行失败: '+scene.MESSAGES[problem][language=='en']
    assert workspaces and all(not p.exists()for p in workspaces)and not target.exists()
    assert{n:module.digest(root/n)for n in module.SOURCES}==initial['sources']


@pytest.mark.parametrize('fault',['value','one_ulp','sign','hole_zero','mask','crs','grid','dtype','nodata','scale','unit','overviews','overview_value','sidecar','source_sidecar','missing_owner','missing','extra'])
def test_sieve_persistence_oracle_rejects_physical_corruption(sieve_persistence,tmp_path,fault,request):
    module,original,_,_=sieve_persistence;root=tmp_path/'changed';shutil.copytree(original,root);control=cache_for(module,root,request);path=root/'outputs'/'four.cog.tif'
    if fault in ('value','one_ulp','sign','hole_zero','mask','crs','grid','nodata','scale','unit','overview_value'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if fault in ('value','one_ulp','sign','hole_zero'):
            row,col=(0,1) if fault=='edge_zero' else (510,511) if fault=='hole_zero' else (20,25)
            wanted=module.expected_pixels('four.cog.tif')[row,col]
            value=0. if fault in ('edge_zero','hole_zero') else np.nextafter(wanted,np.inf) if fault=='one_ulp' else -wanted if fault=='sign' else 12345.
            band.WriteRaster(col,row,1,1,np.array([value],np.float64).tobytes(),buf_type=gdal.GDT_Float64)
        elif fault=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().Fill(255)
        elif fault=='crs':ds.SetProjection('')
        elif fault=='grid':ds.SetGeoTransform((1,1,0,1,0,-1))
        elif fault=='nodata':band.SetNoDataValue(-999.)
        elif fault=='unit':band.SetUnitType('foot')
        elif fault=='overview_value':band.GetOverview(0).Fill(1234.)
        else:band.SetScale(2.)
        band=ds=None
    elif fault in ('dtype','overviews'):
        alt=root/'replacement.tif';ds=gdal.Translate(str(alt),str(path),format='GTiff',outputType=gdal.GDT_Float32 if fault=='dtype' else gdal.GDT_Float64);ds=None;alt.replace(path)
    elif fault=='sidecar':(root/'outputs'/'sieve.cog.tif.aux.xml').write_text('unexpected')
    elif fault=='source_sidecar':(root/'classes.tif.msk').write_text('unexpected')
    elif fault=='missing_owner':(root/'.addp-online-owner').unlink()
    elif fault=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_text('unexpected')
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS),control)


@pytest.mark.parametrize('fault',['wrong_connectivity','threshold_inclusive','class_global_count','small_transit','iterated_growth','nodata_fill','no_donor_replaced','implicit_scale','zero_discarded'])
def test_sieve_independent_oracle_rejects_wrong_semantics(sieve_persistence,tmp_path,fault,request):
    module,original,_,_=sieve_persistence;root=tmp_path/'changed';shutil.copytree(original,root);control=cache_for(module,root,request)
    name='no-donor.cog.tif'if fault=='no_donor_replaced'else'eight.cog.tif'if fault=='zero_discarded'else'four.cog.tif'
    bad=module.expected_pixels(name).copy()
    if fault=='wrong_connectivity':bad=module.expected_pixels('eight.cog.tif').copy()
    elif fault=='threshold_inclusive':bad[60,25:30]=2
    elif fault=='class_global_count':bad[70,25]=1
    elif fault=='small_transit':bad[40,25:27]=2
    elif fault=='iterated_growth':bad[50,25:27]=1
    elif fault=='nodata_fill':bad[510,511]=0
    elif fault=='no_donor_replaced':bad[np.isfinite(bad)]=7
    elif fault=='implicit_scale':bad=bad*2+7
    else:bad[90,90]=np.nan
    path=root/'outputs'/name;ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);ds.GetRasterBand(1).WriteArray(bad);ds=None
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS),control)


@pytest.mark.parametrize('fault',['bytes','size','hash','missing_entry','extra','symlink','manifest_symlink','missing_manifest'])
def test_sieve_original_cog_archive_cannot_change_or_silently_repair(sieve_persistence,tmp_path,fault):
    module,root,_,control=sieve_persistence;facts=module.verify(root,list(module.TARGETS),control);module.archive_cogs(root,tmp_path,facts)
    path=tmp_path/'sieve-cogs'/'four.cog.tif';manifest=tmp_path/'sieve-cogs-manifest.json'
    if fault=='bytes':path.write_bytes(b'changed')
    elif fault in ('size','hash','missing_entry'):
        value=json.loads(manifest.read_text())
        if fault=='missing_entry':value.pop('four.cog.tif')
        else:value['four.cog.tif']['size_bytes' if fault=='size' else 'sha256']=1 if fault=='size' else 'bad'
        manifest.write_text(json.dumps(value))
    elif fault=='extra':path.with_name('extra.tif').write_text('extra')
    elif fault=='symlink':path.unlink();path.symlink_to(root/'outputs'/'four.cog.tif')
    elif fault=='missing_manifest':manifest.unlink()
    else:manifest.unlink();manifest.symlink_to(tmp_path/'missing')
    with pytest.raises(module.FixtureError):module.archive_cogs(root,tmp_path,facts)


def test_sieve_worker_stdin_is_repository_independent_and_sources_remain_immutable(sieve_persistence,tmp_path,monkeypatch):
    module,root,initial,control=sieve_persistence;cwd=tmp_path/'empty';cwd.mkdir();scratch=tmp_path/'scratch';scratch.mkdir()
    env={**os.environ,'PYTHONPATH':'','TMPDIR':str(scratch),'PYTHONDONTWRITEBYTECODE':'1'}
    def invoke(action,payload,names=()):
        done=subprocess.run([sys.executable,'-I','-c','import sys; exec(compile(sys.stdin.read(),"<fixture-worker>","exec"))','--worker',action,str(root),json.dumps(payload),*names],input=Path(module.__file__).read_text(),text=True,capture_output=True,cwd=cwd,env=env,timeout=150)
        assert done.returncode==0,done.stderr;return json.loads(done.stdout)
    child_control=invoke('prepare-oracle',control['scope'])
    try:
        facts=invoke('verify',child_control,module.TARGETS);assert facts['sources']==initial['sources']
        assert facts['oracle_cache']['full_target_checks']==6
        secret=tmp_path/'secret';secret.mkdir();(secret/'sieve-source.json').write_text(json.dumps(initial));anchor=secret/module.CONTROL_NAME;anchor.write_bytes(module.encoded({'scope':child_control['scope'],'arrays':child_control['arrays']}));anchor.chmod(0o600)
        # The host bridge models the same isolated Runtime TMPDIR used by stdin workers.
        # Do not interpret unrelated host scratch directories as disposable Runtime residue.
        monkeypatch.setattr(module.tempfile,'gettempdir',lambda:str(scratch))
        monkeypatch.setattr(module,'guard',lambda:secret);monkeypatch.setattr(module,'owned_root',lambda _:root);monkeypatch.setattr(module,'runtime_worker',lambda action,owned,names:module.worker(action,owned,names));monkeypatch.setattr(module.sys,'argv',['fixture','verify',*module.TARGETS])
        monkeypatch.setenv('ADDP_ONLINE_TEST_RUN_ID','sieve-123-1');monkeypatch.setenv('ADDP_ONLINE_SIEVE_ORACLE_ANCHOR_SHA256',module.digest(anchor)['sha256'])
        monkeypatch.setenv('ADDP_ONLINE_ARTIFACT_DIR',str(tmp_path/'artifacts'));assert module.main('verify')['sources']==initial['sources']
        source=root/'classes.tif';original=source.read_bytes()
        try:
            source.write_bytes(b'changed source')
            with pytest.raises(module.FixtureError,match='source file changed'):module.main('verify')
        finally:source.write_bytes(original)
    finally:assert invoke('cleanup-oracle',child_control)['oracle_cache_removed']
    assert not list(scratch.glob('addp-sieve-oracle-*'))


def test_sieve_cached_expectation_never_caches_actual_pixels_or_rebuilds_on_hit(sieve_persistence,monkeypatch):
    module,root,_,control=sieve_persistence
    def forbidden(*args):raise AssertionError('warm verification must not rebuild the oracle')
    monkeypatch.setattr(module,'expected_pixels',forbidden)
    facts=module.verify(root,list(module.TARGETS),control);assert facts['oracle_cache']['full_target_checks']==6
    directory=module.validate_cache(control);answer=module.load_expected(control,directory,'four.cog.tif');assert not answer.flags.writeable


@pytest.mark.parametrize('fault',['bytes','missing','symlink','mode','marker','extra','run','fixture','root','source_hash','shape'])
def test_sieve_oracle_cache_poison_or_cross_scope_fails(sieve_persistence,tmp_path,request,fault):
    import copy
    module,original,_,_=sieve_persistence;root=tmp_path/'owned';shutil.copytree(original,root);control=cache_for(module,root,request);directory=module.oracle_path(control['scope']);path=directory/'four.cog.tif.npy'
    if fault=='bytes':
        raw=path.read_bytes();path.chmod(0o600);path.write_bytes(raw[:-1]+bytes([raw[-1]^1]));path.chmod(0o400)
    elif fault=='missing':path.unlink()
    elif fault=='symlink':path.unlink();path.symlink_to(root/'outputs/sieve.cog.tif')
    elif fault=='mode':path.chmod(0o600)
    elif fault=='marker':
        marker=directory/module.CACHE_MARKER;marker.chmod(0o600);marker.write_text('{}')
    elif fault=='extra':(directory/'extra').touch()
    elif fault in ('run','fixture','root'):
        control=copy.deepcopy(control);key={'run':'run_id','fixture':'fixture_sha256','root':'source_root'}[fault];control['scope'][key]='sieve-999-1'if fault=='run'else'b'*64 if fault=='fixture'else str(tmp_path)
    elif fault=='source_hash':control['scope']['sources']['classes.tif']['sha256']='b'*64
    else:
        path.chmod(0o600)
        with path.open('wb')as f:np.save(f,np.zeros((1,1)),allow_pickle=False)
        path.chmod(0o400);control['arrays']['four.cog.tif']=module.digest(path)
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS),control)


def test_sieve_oracle_cleanup_and_reuse_refusal(sieve_persistence,tmp_path,request):
    module,original,_,_=sieve_persistence;root=tmp_path/'owned';shutil.copytree(original,root);control=cache_for(module,root,request)
    with pytest.raises(FileExistsError):module.prepare_oracle(root,control['scope'])
    assert module.cleanup_oracle(root,control)['oracle_cache_removed'];assert not module.oracle_path(control['scope']).exists()
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS),control)


def test_sieve_partial_oracle_preparation_always_has_an_owned_cleanup(sieve_persistence,tmp_path,request,monkeypatch):
    module,original,_,_=sieve_persistence;root=tmp_path/'owned';shutil.copytree(original,root)
    scope={'schema':module.CACHE_SCHEMA,'run_id':'sieve-123-1','source_root':str(root.resolve()),'fixture_sha256':module.digest(Path(module.__file__))['sha256'],'sources':{n:module.digest(root/n)for n in module.SOURCES}}
    request.addfinalizer(lambda:shutil.rmtree(module.oracle_path(scope),ignore_errors=True))
    saved=module.expected_pixels
    def fail(name):
        if name=='eight.cog.tif':raise module.FixtureError('injected independent expectation failure')
        return saved(name)
    monkeypatch.setattr(module,'expected_pixels',fail)
    with pytest.raises(module.FixtureError):module.prepare_oracle(root,scope)
    assert module.cleanup_oracle(root,{'scope':scope})['oracle_cache_removed'];assert not module.oracle_path(scope).exists()


def test_sieve_cache_test_cleanup_freezes_created_path_before_scope_mutation(sieve_persistence,tmp_path):
    from types import SimpleNamespace
    module,original,_,_=sieve_persistence;root=tmp_path/'owned';shutil.copytree(original,root);finalizers=[]
    control=cache_for(module,root,SimpleNamespace(addfinalizer=finalizers.append));directory=module.oracle_path(control['scope'])
    try:
        control['scope']['sources']['classes.tif']['sha256']='b'*64
        assert module.oracle_path(control['scope'])!=directory
        finalizers[0]();assert not directory.exists()
    finally:shutil.rmtree(directory,ignore_errors=True)
