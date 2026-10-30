"""Real GDAL preparation for independently archived PERCENTILE Hosted acceptance."""
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
def percentile_persistence(tmp_path_factory,request):
    module=importlib.import_module('business.scripts.online-raster-focal-percentile-fixture');scene=importlib.import_module('scripts.test.raster-focal-percentile-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('percentile-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('focal-percentile-123-1');initial=module.seed(root)
    for name in module.TARGETS:
        result=execute_workflow(native_definition(scene,module,root,name));assert result['status']=='success',result
        artifact=json.loads(result['final_result']);assert type(artifact['size_bytes']) is int and artifact['size_bytes']==(root/'outputs'/name).stat().st_size>0
    control=cache_for(module,root,request)
    return module,root,initial,control


def test_percentile_persistence_real_gdal_sixteen_chains_complete_controls_pixels_masks_and_cogs(percentile_persistence,tmp_path):
    module,root,initial,control=percentile_persistence;facts=module.verify(root,list(module.TARGETS),control)
    assert facts['sources']==initial['sources'] and len(facts['targets'])==16
    archived=module.archive_cogs(root,tmp_path,facts);assert len(archived)==16
    assert facts['comparisons']==[list(p)for p in module.COMPARISONS]
    for name,fact in facts['targets'].items():
        assert all(fact[k]for k in ('full_pixel_and_mask_verification','cog_valid','independent_fraction_oracle_verified',
                                   'original_coverage_verified','linear_position_verified','percentile_encoding_verified',
                                   'nearest_overviews_verified','explicit_selected_band_verified','legal_zero_verified'))
        assert fact['size_bytes']==(root/'outputs'/name).stat().st_size>0 and fact['overview_count']==3
        assert(tmp_path/archived[name]['path']).read_bytes()==(root/'outputs'/name).read_bytes()
        ds=gdal.Open(str(root/'outputs'/name));module.assert_pixels(ds.ReadAsArray(),module.expected_pixels(name),name);ds=None
        assert (fact['valid_pixels']==0) if name=='invalid.cog.tif'else fact['valid_pixels']>0
    for left,right in module.COMPARISONS:
        np.testing.assert_array_equal(module.expected_pixels(left),module.expected_pixels(right))
    assert np.isfinite(module.expected_pixels('p25-3.cog.tif')[203,203])
    assert not np.isfinite(module.expected_pixels('p25-3.cog.tif')[510,511])
    assert module.expected_pixels('p25-3.cog.tif')[203,203]==0


@pytest.mark.parametrize('problem',('percentile_missing','percentile_null','percentile_boolean','percentile_string','percentile_array',
          'percentile_low','percentile_high','unused','method','window_value','window_boolean','window_string',
          'band','band_zero','band_boolean','band_string','alpha','complex'))
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_percentile_persistence_thirty_six_accurate_submitted_language_failures_no_publication(percentile_persistence,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    module,root,initial,control=percentile_persistence;scene=importlib.import_module('scripts.test.raster-focal-percentile-persistence-online');target=tmp_path/'failed.cog.tif'
    definition=native_definition(scene,module,root,'p25-3.cog.tif',invalid=problem,language=language,target=target)
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_focal_statistics':workspaces.append(params['input_raster'].workspace)
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    with pytest.raises(WorkflowExecutionError)as error:GeoPythonWorkflowRunner(language=language).execute(definition)
    assert error.value.error_code=='EXECUTION_FAILED'and str(error.value)=='任务 focal 执行失败: '+scene.MESSAGES[problem][language=='en']
    assert workspaces and all(not p.exists()for p in workspaces)and not target.exists()
    assert{n:module.digest(root/n)for n in module.SOURCES}==initial['sources']


@pytest.mark.parametrize('language',['zh-cn','en'])
def test_percentile_complex_alpha_is_separate_native_mixed_mem_evidence_only(monkeypatch,language):
    import operators.raster_compute as product
    ds=gdal.GetDriverByName('MEM').Create('',3,3,1,gdal.GDT_Float64);ds.GetRasterBand(1).Fill(1)
    assert ds.AddBand(gdal.GDT_CFloat64)==gdal.CE_None
    ds.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    assert ds.GetRasterBand(1).DataType==gdal.GDT_Float64 and ds.GetRasterBand(2).DataType==gdal.GDT_CFloat64
    monkeypatch.setattr(product,'_open',lambda _:ds)
    monkeypatch.setattr(product,'_numeric_output',lambda *a,**k:pytest.fail('must reject alpha before allocating output'))
    with product.raster_workspace(),product.raster_error_language(language),pytest.raises(ValueError,match='Complex-valued bands'):
        product.raster_focal_statistics(object(),1,3,'percentile',25)
    ds=None


@pytest.mark.parametrize('fault',['value','one_ulp','sign','hole_zero','mask','crs','grid','dtype','nodata','scale','unit','overviews','overview_value','overview_one_ulp','sidecar','source_sidecar','missing_owner','missing','extra'])
def test_percentile_persistence_oracle_rejects_physical_corruption(percentile_persistence,tmp_path,fault,request):
    module,original,_,_=percentile_persistence;root=tmp_path/'changed';shutil.copytree(original,root);control=cache_for(module,root,request);path=root/'outputs'/'p25-3.cog.tif'
    if fault in ('value','one_ulp','sign','hole_zero','mask','crs','grid','nodata','scale','unit','overview_value','overview_one_ulp'):
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if fault in ('value','one_ulp','sign','hole_zero'):
            row,col=(0,1) if fault=='edge_zero' else (510,511) if fault=='hole_zero' else (60,60)
            wanted=module.expected_pixels('p25-3.cog.tif')[row,col]
            if fault!='hole_zero':assert np.isfinite(wanted)and wanted!=0
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
        elif fault=='overview_one_ulp':
            level=band.GetOverview(0);wanted=level.ReadAsArray(50,50,1,1)[0,0];assert np.isfinite(wanted)and wanted!=0
            level.WriteRaster(50,50,1,1,np.array([np.nextafter(wanted,np.inf)],np.float64).tobytes(),buf_type=gdal.GDT_Float64);level=None
        else:band.SetScale(2.)
        band=ds=None
    elif fault in ('dtype','overviews'):
        alt=root/'replacement.tif';ds=gdal.Translate(str(alt),str(path),format='GTiff',outputType=gdal.GDT_Float32 if fault=='dtype' else gdal.GDT_Float64);ds=None;alt.replace(path)
    elif fault=='sidecar':(root/'outputs'/'percentile.cog.tif.aux.xml').write_text('unexpected')
    elif fault=='source_sidecar':(root/'values.tif.msk').write_text('unexpected')
    elif fault=='missing_owner':(root/'.addp-online-owner').unlink()
    elif fault=='missing':path.unlink()
    else:(root/'outputs'/'extra.tif').write_text('unexpected')
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS),control)


@pytest.mark.parametrize('fault',['nearest_rank','median_instead','implicit_scale','zero_discarded','hole_filled','one_ulp'])
def test_percentile_oracle_rejects_wrong_rank_precision_scale_and_coverage(percentile_persistence,tmp_path,fault,request,monkeypatch):
    module,original,_,_=percentile_persistence;root=tmp_path/'semantics';shutil.copytree(original,root)
    control=cache_for(module,root,request);saved=module.expected_pixels
    def wrong(name):
        expected=saved(name).copy()
        if name!='p25-3.cog.tif':return expected
        if fault=='median_instead':return saved('p50-3.cog.tif')
        if fault=='nearest_rank':
            values,valid=module.expected_inputs(name)
            for row,col in zip(*np.nonzero(valid)):
                sample=sorted(float(values[y,x])for y in range(max(0,row-1),min(module.HEIGHT,row+2))
                              for x in range(max(0,col-1),min(module.WIDTH,col+2))if valid[y,x])
                expected[row,col]=sample[round((len(sample)-1)*.25)]
        elif fault=='implicit_scale':expected=expected*2+7
        elif fault=='zero_discarded':expected[expected==0]=np.nan
        elif fault=='hole_filled':expected[~np.isfinite(expected)]=0.
        else:expected[0,0]=np.nextafter(expected[0,0],np.inf)
        return expected
    monkeypatch.setattr(module,'expected_pixels',wrong)
    # New expectation construction is allowed only before the immutable anchor;
    # the actual old files must fail against each deliberately wrong semantic oracle.
    module.cleanup_oracle(root,control);control=cache_for(module,root,request)
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS),control)


@pytest.mark.parametrize('fault',['bytes','size','hash','missing_entry','extra','symlink','missing_manifest','manifest_symlink'])
def test_percentile_original_cog_archive_cannot_change_or_silently_repair(percentile_persistence,tmp_path,fault):
    module,root,_,control=percentile_persistence;facts=module.verify(root,list(module.TARGETS),control);module.archive_cogs(root,tmp_path,facts)
    path=tmp_path/'percentile-cogs'/'p25-3.cog.tif';manifest=tmp_path/'percentile-cogs-manifest.json'
    if fault=='bytes':path.write_bytes(b'changed')
    elif fault in ('size','hash','missing_entry'):
        value=json.loads(manifest.read_text())
        if fault=='missing_entry':value.pop('p25-3.cog.tif')
        else:value['p25-3.cog.tif']['size_bytes' if fault=='size' else 'sha256']=1 if fault=='size' else 'bad'
        manifest.write_text(json.dumps(value))
    elif fault=='extra':path.with_name('extra.tif').write_text('extra')
    elif fault=='symlink':path.unlink();path.symlink_to(root/'outputs'/'p25-3.cog.tif')
    elif fault=='missing_manifest':manifest.unlink()
    else:manifest.unlink();manifest.symlink_to(tmp_path/'missing')
    with pytest.raises(module.FixtureError):module.archive_cogs(root,tmp_path,facts)


def test_percentile_worker_stdin_is_repository_independent_and_sources_remain_immutable(percentile_persistence,tmp_path,monkeypatch):
    module,root,initial,control=percentile_persistence;cwd=tmp_path/'empty';cwd.mkdir();scratch=tmp_path/'scratch';scratch.mkdir()
    env={**os.environ,'PYTHONPATH':'','TMPDIR':str(scratch),'PYTHONDONTWRITEBYTECODE':'1'}
    def invoke(action,payload,names=()):
        done=subprocess.run([sys.executable,'-I','-c','import sys; exec(compile(sys.stdin.read(),"<fixture-worker>","exec"))','--worker',action,str(root),json.dumps(payload),*names],input=Path(module.__file__).read_text(),text=True,capture_output=True,cwd=cwd,env=env,timeout=150)
        assert done.returncode==0,done.stderr;return json.loads(done.stdout)
    child_control=invoke('prepare-oracle',control['scope'])
    try:
        facts=invoke('verify',child_control,module.TARGETS);assert facts['sources']==initial['sources']
        assert facts['oracle_cache']['full_target_checks']==16
        secret=tmp_path/'secret';secret.mkdir();(secret/'focal-percentile-source.json').write_text(json.dumps(initial));anchor=secret/module.CONTROL_NAME;anchor.write_bytes(module.encoded({'scope':child_control['scope'],'arrays':child_control['arrays']}));anchor.chmod(0o600)
        # The host bridge models the same isolated Runtime TMPDIR used by stdin workers.
        # Do not interpret unrelated host scratch directories as disposable Runtime residue.
        monkeypatch.setattr(module.tempfile,'gettempdir',lambda:str(scratch))
        monkeypatch.setattr(module,'guard',lambda:secret);monkeypatch.setattr(module,'owned_root',lambda _:root);monkeypatch.setattr(module,'runtime_worker',lambda action,owned,names:module.worker(action,owned,names));monkeypatch.setattr(module.sys,'argv',['fixture','verify',*module.TARGETS])
        monkeypatch.setenv('ADDP_ONLINE_TEST_RUN_ID','focal-percentile-123-1');monkeypatch.setenv('ADDP_ONLINE_FOCAL_PERCENTILE_ORACLE_ANCHOR_SHA256',module.digest(anchor)['sha256'])
        monkeypatch.setenv('ADDP_ONLINE_ARTIFACT_DIR',str(tmp_path/'artifacts'));assert module.main('verify')['sources']==initial['sources']
        source=root/'values.tif';original=source.read_bytes()
        try:
            source.write_bytes(b'changed source')
            with pytest.raises(module.FixtureError,match='source file changed'):module.main('verify')
        finally:source.write_bytes(original)
    finally:assert invoke('cleanup-oracle',child_control)['oracle_cache_removed']
    assert not list(scratch.glob('addp-percentile-oracle-*'))


def test_percentile_cached_expectation_never_caches_actual_pixels_or_rebuilds_on_hit(percentile_persistence,monkeypatch):
    module,root,_,control=percentile_persistence
    def forbidden(*args):raise AssertionError('warm verification must not rebuild the oracle')
    monkeypatch.setattr(module,'expected_pixels',forbidden)
    reads=[];mask_reads=[];read=gdal.Band.ReadAsArray
    def actual_read(band,*args,**kwargs):
        if (band.XSize,band.YSize)==(module.WIDTH,module.HEIGHT):
            dataset=band.GetDataset()
            if dataset is None:mask_reads.append(True)
            else:reads.append(Path(dataset.GetDescription()).name)
        return read(band,*args,**kwargs)
    monkeypatch.setattr(gdal.Band,'ReadAsArray',actual_read)
    facts=module.verify(root,list(module.TARGETS),control);assert facts['oracle_cache']['full_target_checks']==16
    assert set(reads)==set(module.TARGETS) and len(mask_reads)>=len(module.TARGETS)
    directory=module.validate_cache(control);answer=module.load_expected(control,directory,'p25-3.cog.tif');assert not answer.flags.writeable


@pytest.mark.parametrize('fault',['bytes','missing','symlink','mode','marker','extra','run','fixture','root','source_hash','shape'])
def test_percentile_oracle_cache_poison_or_cross_scope_fails(percentile_persistence,tmp_path,request,fault):
    import copy
    module,original,_,_=percentile_persistence;root=tmp_path/'owned';shutil.copytree(original,root);control=cache_for(module,root,request);directory=module.oracle_path(control['scope']);path=directory/'p25-3.cog.tif.npy'
    if fault=='bytes':
        raw=path.read_bytes();path.chmod(0o600);path.write_bytes(raw[:-1]+bytes([raw[-1]^1]));path.chmod(0o400)
    elif fault=='missing':path.unlink()
    elif fault=='symlink':path.unlink();path.symlink_to(root/'outputs/percentile.cog.tif')
    elif fault=='mode':path.chmod(0o600)
    elif fault=='marker':
        marker=directory/module.CACHE_MARKER;marker.chmod(0o600);marker.write_text('{}')
    elif fault=='extra':(directory/'extra').touch()
    elif fault in ('run','fixture','root'):
        control=copy.deepcopy(control);key={'run':'run_id','fixture':'fixture_sha256','root':'source_root'}[fault];control['scope'][key]='focal-percentile-999-1'if fault=='run'else'b'*64 if fault=='fixture'else str(tmp_path)
    elif fault=='source_hash':control['scope']['sources']['values.tif']['sha256']='b'*64
    else:
        path.chmod(0o600)
        with path.open('wb')as f:np.save(f,np.zeros((1,1)),allow_pickle=False)
        path.chmod(0o400);control['arrays']['p25-3.cog.tif']=module.digest(path)
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS),control)


def test_percentile_oracle_cleanup_and_reuse_refusal(percentile_persistence,tmp_path,request):
    module,original,_,_=percentile_persistence;root=tmp_path/'owned';shutil.copytree(original,root);control=cache_for(module,root,request)
    with pytest.raises(FileExistsError):module.prepare_oracle(root,control['scope'])
    assert module.cleanup_oracle(root,control)['oracle_cache_removed'];assert not module.oracle_path(control['scope']).exists()
    with pytest.raises(module.FixtureError):module.verify(root,list(module.TARGETS),control)


def test_percentile_partial_oracle_preparation_always_has_an_owned_cleanup(percentile_persistence,tmp_path,request,monkeypatch):
    module,original,_,_=percentile_persistence;root=tmp_path/'owned';shutil.copytree(original,root)
    scope={'schema':module.CACHE_SCHEMA,'run_id':'focal-percentile-123-1','source_root':str(root.resolve()),'fixture_sha256':module.digest(Path(module.__file__))['sha256'],'sources':{n:module.digest(root/n)for n in module.SOURCES}}
    request.addfinalizer(lambda:shutil.rmtree(module.oracle_path(scope),ignore_errors=True))
    saved=module.expected_pixels
    def fail(name):
        if name=='p75-5.cog.tif':raise module.FixtureError('injected independent expectation failure')
        return saved(name)
    monkeypatch.setattr(module,'expected_pixels',fail)
    with pytest.raises(module.FixtureError):module.prepare_oracle(root,scope)
    assert module.cleanup_oracle(root,{'scope':scope})['oracle_cache_removed'];assert not module.oracle_path(scope).exists()


def test_percentile_cache_test_cleanup_freezes_created_path_before_scope_mutation(percentile_persistence,tmp_path):
    from types import SimpleNamespace
    module,original,_,_=percentile_persistence;root=tmp_path/'owned';shutil.copytree(original,root);finalizers=[]
    control=cache_for(module,root,SimpleNamespace(addfinalizer=finalizers.append));directory=module.oracle_path(control['scope'])
    try:
        control['scope']['sources']['values.tif']['sha256']='b'*64
        assert module.oracle_path(control['scope'])!=directory
        finalizers[0]();assert not directory.exists()
    finally:shutil.rmtree(directory,ignore_errors=True)
