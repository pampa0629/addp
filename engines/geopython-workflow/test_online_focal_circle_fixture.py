"""Actual GDAL circle/control COGs and immutable independent oracle evidence."""
import importlib,json,os,shutil,subprocess,sys
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest
from workflow_engine import execute_workflow,GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError
sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
f=importlib.import_module('business.scripts.online-raster-focal-circle-fixture')
m=importlib.import_module('scripts.test.raster-focal-circle-persistence-online')
helper=importlib.import_module('test_online_focal_percentile_fixture')

@pytest.fixture(scope='module')
def circle_scene(tmp_path_factory,request):
    root=tmp_path_factory.mktemp('circle-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('focal-circle-123-1');initial=f.seed(root)
    control=helper.cache_for(f,root,request)
    for name in f.TARGETS:
        result=execute_workflow(helper.native_definition(m,f,root,name));assert result['status']=='success',result
        assert json.loads(result['final_result'])['size_bytes']==(root/'outputs'/name).stat().st_size>0
    return root,initial,control

def test_circle_hosted_eighteen_real_cogs_complete_oracle_circle_boundaries_controls_and_original_bytes(circle_scene,tmp_path):
    root,initial,control=circle_scene;report=f.verify(root,list(f.TARGETS),control)
    assert report['sources']==initial['sources'] and report['full_source_pixels_verified'] is True and len(report['targets'])==18
    archived=f.archive_cogs(root,tmp_path,report,{})
    expectations=f.archive_expectations(root,tmp_path,{})
    assert len(expectations)==18
    for name in f.TARGETS:
        path=tmp_path/expectations[name+'.npy']['path']
        assert path.read_bytes()==(f.oracle_path(control['scope'])/(name+'.npy')).read_bytes()
        np.testing.assert_array_equal(np.load(path,allow_pickle=False),f.expected_pixels(name))
    for name,fact in report['targets'].items():
        assert fact['full_pixel_and_mask_verification'] is True and fact['overview_count']>0
        assert (tmp_path/archived[name]['path']).read_bytes()==(root/'outputs'/name).read_bytes()
    assert f.expected_pixels('circle-sum.cog.tif')[62,62]==9
    assert f.expected_pixels('below.cog.tif')[62,62]==5
    assert f.expected_pixels('above.cog.tif')[62,62]==9
    assert not np.isfinite(f.expected_pixels('invalid.cog.tif')).any()
    np.testing.assert_array_equal(f.expected_pixels('circle-median.cog.tif'),f.expected_pixels('percentile-50.cog.tif'))
    values,valid=f.expected_inputs('center.cog.tif');np.testing.assert_array_equal(f.expected_pixels('center.cog.tif'),np.where(valid,values,np.nan))
    assert f.case_shape('circle-min.cog.tif')==(1,519)and f.case_shape('circle-max.cog.tif')==(512,1)
    assert f.expected_pixels('circle-stddev.cog.tif')[23,23]==0
    assert f.expected_pixels('circle-variety.cog.tif')[60,60]==13
    assert f.expected_pixels('circle-mode.cog.tif')[42,42]==-2 and f.expected_pixels('circle-mode.cog.tif')[52,52]==2
    assert f.source_spec('foot.tif')['epsg']==2222 and f.source_spec('survey.tif')['epsg']==2277
    assert f.case_shape('circle-mean.cog.tif')==(515,519)
    assert set(m.fixture.CASES[n][2]for n in m.fixture.TARGETS)>=set(('sum','mean','min','max','stddev','mode','median','percentile','variety'))

@pytest.mark.parametrize('problem',f.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_circle_hosted_one_hundred_exact_submitted_failures_no_output_and_clean_workspace(circle_scene,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    root,initial,control=circle_scene;target=tmp_path/'failed.cog.tif';dag=helper.native_definition(m,f,root,'circle-sum.cog.tif',invalid=problem,language=language,target=target)
    workspaces=[];original=workflow_engine._execute_operator
    def track(name,params):
        if name=='raster_focal_statistics':workspaces.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',track)
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute(dag)
    assert error.value.error_code=='EXECUTION_FAILED' and str(error.value)=='任务 focal 执行失败: '+m.MESSAGES[problem][language=='en']
    assert workspaces and all(not p.exists() for p in workspaces) and not target.exists()
    assert f.verify(root,list(f.TARGETS),control)['sources']==initial['sources']

@pytest.mark.parametrize('fault',['value','one-ulp','hole','mask','grid','crs','nodata','unit','scale','overview','source-mask','sidecar','missing'])
def test_circle_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected(circle_scene,tmp_path,request,fault):
    original,_,_=circle_scene;root=tmp_path/'changed';shutil.copytree(original,root);control=helper.cache_for(f,root,request)
    path=root/'outputs'/'circle-sum.cog.tif'
    if fault=='sidecar':(root/'outputs'/'extra.tif').write_bytes(b'extra')
    elif fault=='missing':path.unlink()
    elif fault=='source-mask':
        ds=gdal.Open(str(root/'values.tif'),gdal.GA_Update);ds.GetRasterBand(1).GetMaskBand().Fill(0);ds=None
    else:
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if fault in ('value','one-ulp','hole'):
            row,col=(0,1) if fault=='hole' else (62,62);wanted=f.expected_pixels('circle-sum.cog.tif')[row,col]
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
def test_circle_hosted_original_cog_archive_never_repairs_or_overwrites(circle_scene,tmp_path,fault):
    root,_,control=circle_scene;report=f.verify(root,list(f.TARGETS),control);previous=f.archive_cogs(root,tmp_path,report,{})
    path=tmp_path/'circle-cogs'/'circle-sum.cog.tif'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'circle-cogs-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'circle-cogs-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.tif').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/'outputs'/'circle-sum.cog.tif')
    with pytest.raises(f.FixtureError):f.archive_cogs(root,tmp_path,report,previous)

@pytest.mark.parametrize('fault',['bytes','missing','extra','scope','shape'])
def test_circle_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change(circle_scene,tmp_path,request,fault):
    original,_,_=circle_scene;root=tmp_path/'cache';shutil.copytree(original,root);control=helper.cache_for(f,root,request);directory=f.oracle_path(control['scope']);path=directory/'circle-sum.cog.tif.npy'
    if fault=='bytes':path.chmod(0o600);path.write_bytes(path.read_bytes()+b'x')
    elif fault=='missing':path.unlink()
    elif fault=='extra':(directory/'unexpected.npy').write_bytes(b'x')
    elif fault=='scope':control['scope']['run_id']='focal-circle-999-1'
    else:
        path.chmod(0o600)
        with path.open('wb') as out:np.save(out,np.array([[1.]]))
        path.chmod(0o400);control['arrays']['circle-sum.cog.tif']=f.digest(path)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS),control)

def test_circle_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup(circle_scene,tmp_path):
    original,initial,control=circle_scene;root=tmp_path/'child';shutil.copytree(original,root);control={'scope':{**control['scope'],'source_root':str(root.resolve())}};scratch=tmp_path/'scratch';scratch.mkdir();cwd=tmp_path/'empty';cwd.mkdir()
    env={**os.environ,'PYTHONPATH':'','TMPDIR':str(scratch),'PYTHONDONTWRITEBYTECODE':'1'}
    def call(action,payload,names=()):
        result=subprocess.run([sys.executable,'-I','-c','import sys;exec(compile(sys.stdin.read(),"<worker>","exec"))','--worker',action,str(root),json.dumps(payload),*names],input=Path(f.__file__).read_bytes(),cwd=cwd,env=env,capture_output=True,timeout=150)
        assert result.returncode==0,result.stderr.decode();return json.loads(result.stdout)
    child=call('prepare-oracle',control['scope'])
    try:
        report=call('verify',child,list(f.TARGETS));assert report['sources']==initial['sources'] and report['oracle_cache']['full_target_checks']==18
    finally:assert call('cleanup-oracle',child)['oracle_cache_removed'] is True
    assert not list(scratch.iterdir())

@pytest.mark.parametrize('fault',['bytes','deleted','directory','manifest','extra','symlink'])
def test_circle_hosted_frozen_expected_archive_rejects_tampering(circle_scene,tmp_path,fault):
    root,_,_=circle_scene;previous=f.archive_expectations(root,tmp_path,{})
    path=tmp_path/'circle-expectations'/'circle-sum.cog.tif.npy'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'circle-expectations-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'circle-expectations-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.npy').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/f.EXPECTED_EXPORT/'circle-sum.cog.tif.npy')
    with pytest.raises(f.FixtureError):f.archive_expectations(root,tmp_path,previous)


@pytest.mark.parametrize('fault',['square','radius-epsilon','no-unit','axis-swap','center-invalid'])
def test_circle_hosted_independent_semantic_mutations_cannot_pass(circle_scene,fault):
    root,_,control=circle_scene
    name='below.cog.tif'if fault=='radius-epsilon'else'foot.cog.tif'if fault=='no-unit'else'circle-min.cog.tif'if fault=='axis-swap'else'circle-sum.cog.tif'
    expected=f.expected_pixels(name);bad=expected.copy()
    if fault=='center-invalid':bad[0,1]=0.
    elif fault=='radius-epsilon':bad[62,62]=9.
    elif fault=='square':bad[62,62]=25.
    else:
        # Ignoring the foot conversion or swapping the 1/4-metre axes leaves only the centre.
        values,valid=f.expected_inputs(name);bad=np.where(valid,values,np.nan)
    with pytest.raises(f.FixtureError):f.assert_pixels(bad,expected,name)

def test_circle_hosted_oracle_never_calls_product_kernel_or_reducer(monkeypatch):
    import operators.raster_compute as product
    def forbidden(*a,**kw):raise AssertionError('product oracle dependency')
    monkeypatch.setattr(product,'_focal_circle_window',forbidden)
    monkeypatch.setattr(product,'raster_focal_statistics',forbidden)
    f.expected_pixels.cache_clear()
    try:
        for name in f.TARGETS:assert f.expected_pixels(name).shape==f.case_shape(name)
    finally:f.expected_pixels.cache_clear()
