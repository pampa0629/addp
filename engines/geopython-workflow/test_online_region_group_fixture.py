"""Actual GDAL region/control COGs and immutable independent oracle evidence."""
import importlib,json,os,shutil,subprocess,sys
from pathlib import Path
import numpy as np
from osgeo import gdal
import pytest
from workflow_engine import execute_workflow,GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError
sys.path.insert(0,str(Path(__file__).resolve().parents[2]))
f=importlib.import_module('business.scripts.online-raster-region-group-fixture')
m=importlib.import_module('scripts.test.raster-region-group-persistence-online')
helper=importlib.import_module('test_online_focal_percentile_fixture')

def native_definition(name,root,**kwargs):
    dag=helper.native_definition(m,f,root,name,**kwargs)
    for task in dag['tasks']:
        if task['operator']=='save':
            params=task['params'];params.pop('target_parent_locator');target=root/'outputs'/params.pop('target_name')
            params.update(connection_info={'mount_path':str(target.parent)},path=target.name)
    return dag

@pytest.fixture(scope='module')
def region_scene(tmp_path_factory,request):
    root=tmp_path_factory.mktemp('region-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('region-group-123-1');initial=f.seed(root)
    control=helper.cache_for(f,root,request)
    for name in f.TARGETS:
        result=execute_workflow(native_definition(name,root));assert result['status']=='success',result
        if not name.endswith('.csv'):assert json.loads(result['final_result'])['size_bytes']==(root/'outputs'/name).stat().st_size>0
        else:assert json.loads(result['final_result'])['type']=='pandas_dataframe'
    return root,initial,control

def test_region_hosted_eighteen_mixed_results_complete_oracle_and_original_bytes(region_scene,tmp_path):
    root,initial,control=region_scene;report=f.verify(root,list(f.TARGETS),control)
    assert report['sources']==initial['sources']and report['full_source_pixels_verified']is True and len(report['targets'])==18
    archived=f.archive_outputs(root,tmp_path,report,{});expected_archive=f.archive_expectations(root,tmp_path,{})
    assert len(expected_archive)==18 and len([n for n in f.TARGETS if n.endswith('.csv')])==2
    for name,fact in report['targets'].items():
        assert (tmp_path/archived[name]['path']).read_bytes()==(root/'outputs'/name).read_bytes()
        np.testing.assert_array_equal(np.load(tmp_path/expected_archive[name+'.npy']['path'],allow_pickle=False),f.expected_pixels(name))
        if name.endswith('.csv'):
            assert fact['complete_table_verified']is True and fact['pixel_conservation_verified']is True
        else:assert fact['full_pixel_and_mask_verification']is True and fact['overview_count']==f.expected_overview_count(name)>0
    assert f.case_shape('cross-four.cog.tif')==(515,519)and f.case_shape('unlocated.cog.tif')==(1,512)
    assert f.case_shape('int64.cog.tif')==(512,1)
    assert np.nanmax(f.expected_pixels('four.cog.tif'))>np.nanmax(f.expected_pixels('eight.cog.tif'))
    np.testing.assert_array_equal(f.expected_pixels('four.cog.tif'),f.expected_pixels('repeat-four.cog.tif'))
    assert not np.isfinite(f.expected_pixels('invalid-four.cog.tif')).any()
    assert not np.isfinite(f.expected_pixels('invalid-eight.cog.tif')).any()

@pytest.mark.parametrize('problem',f.FAILURES)
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_region_hosted_thirty_six_exact_submitted_failures_no_output_and_clean_workspace(region_scene,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    root,initial,control=region_scene;target=tmp_path/'failed.cog.tif';dag=native_definition('four.cog.tif',root,invalid=problem,language=language,target=target)
    workspaces=[];original=workflow_engine._execute_operator
    def track(name,params):
        if name=='raster_region_group':workspaces.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',track)
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute(dag)
    assert error.value.error_code=='EXECUTION_FAILED' and str(error.value)=='任务 region 执行失败: '+m.MESSAGES[problem][language=='en']
    assert workspaces and all(not p.exists() for p in workspaces) and not target.exists()
    assert f.verify(root,list(f.TARGETS),control)['sources']==initial['sources']

@pytest.mark.parametrize('fault',['value','one-ulp','hole','mask','grid','crs','nodata','unit','scale','overview','source-mask','sidecar','missing'])
def test_region_hosted_actual_pixel_mask_metadata_and_overview_corruption_rejected(region_scene,tmp_path,request,fault):
    original,_,_=region_scene;root=tmp_path/'changed';shutil.copytree(original,root);control=helper.cache_for(f,root,request)
    path=root/'outputs'/'four.cog.tif'
    if fault=='sidecar':(root/'outputs'/'extra.tif').write_bytes(b'extra')
    elif fault=='missing':path.unlink()
    elif fault=='source-mask':
        ds=gdal.Open(str(root/'values.tif'),gdal.GA_Update);ds.GetRasterBand(1).GetMaskBand().Fill(0);ds=None
    else:
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if fault in ('value','one-ulp','hole'):
            row,col=(0,1) if fault=='hole' else (62,62);wanted=f.expected_pixels('four.cog.tif')[row,col]
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
def test_region_hosted_original_cog_archive_never_repairs_or_overwrites(region_scene,tmp_path,fault):
    root,_,control=region_scene;report=f.verify(root,list(f.TARGETS),control);previous=f.archive_outputs(root,tmp_path,report,{})
    path=tmp_path/'region-outputs'/'four.cog.tif'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'region-outputs-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'region-outputs-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.tif').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/'outputs'/'four.cog.tif')
    with pytest.raises(f.FixtureError):f.archive_outputs(root,tmp_path,report,previous)

@pytest.mark.parametrize('fault',['bytes','missing','extra','scope','shape'])
def test_region_hosted_frozen_cache_rejects_hash_shape_inventory_or_run_change(region_scene,tmp_path,request,fault):
    original,_,_=region_scene;root=tmp_path/'cache';shutil.copytree(original,root);control=helper.cache_for(f,root,request);directory=f.oracle_path(control['scope']);path=directory/'four.cog.tif.npy'
    if fault=='bytes':path.chmod(0o600);path.write_bytes(path.read_bytes()+b'x')
    elif fault=='missing':path.unlink()
    elif fault=='extra':(directory/'unexpected.npy').write_bytes(b'x')
    elif fault=='scope':control['scope']['run_id']='region-group-999-1'
    else:
        path.chmod(0o600)
        with path.open('wb') as out:np.save(out,np.array([[1.]]))
        path.chmod(0o400);control['arrays']['four.cog.tif']=f.digest(path)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS),control)

def test_region_hosted_stdin_worker_has_no_repository_dependency_and_exact_cleanup(region_scene,tmp_path):
    original,initial,control=region_scene;root=tmp_path/'child';shutil.copytree(original,root);control={'scope':{**control['scope'],'source_root':str(root.resolve())}};scratch=tmp_path/'scratch';scratch.mkdir();cwd=tmp_path/'empty';cwd.mkdir()
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
def test_region_hosted_frozen_expected_archive_rejects_tampering(region_scene,tmp_path,fault):
    root,_,_=region_scene;previous=f.archive_expectations(root,tmp_path,{})
    path=tmp_path/'region-expectations'/'four.cog.tif.npy'
    if fault=='bytes':path.chmod(0o644);path.write_bytes(b'changed')
    elif fault=='deleted':path.unlink()
    elif fault=='directory':shutil.rmtree(path.parent);(tmp_path/'region-expectations-manifest.json').unlink()
    elif fault=='manifest':(tmp_path/'region-expectations-manifest.json').write_text('{}')
    elif fault=='extra':(path.parent/'extra.npy').write_bytes(b'extra')
    else:path.unlink();path.symlink_to(root/f.EXPECTED_EXPORT/'four.cog.tif.npy')
    with pytest.raises(f.FixtureError):f.archive_expectations(root,tmp_path,previous)


@pytest.mark.parametrize('fault',['eight-instead-four','class-global','anchor-swap','fill-invalid','start-zero','round-label'])
def test_region_hosted_independent_semantic_mutations_cannot_pass(region_scene,fault):
    name='four.cog.tif';expected=f.expected_pixels(name);bad=expected.copy()
    if fault=='eight-instead-four':bad=f.expected_pixels('eight.cog.tif')
    elif fault=='class-global':values,valid=f.expected_inputs(name);bad=np.where(valid,values,np.nan)
    elif fault=='anchor-swap':bad=np.where(expected==1,2,np.where(expected==2,1,expected))
    elif fault=='fill-invalid':bad[0,1]=1
    elif fault=='start-zero':bad=bad-1
    else:bad[np.isfinite(bad)]=np.nextafter(bad[np.isfinite(bad)],np.inf)
    with pytest.raises(f.FixtureError):f.assert_pixels(bad,expected,name)

def test_region_hosted_oracle_never_calls_product_kernel_or_reducer(monkeypatch):
    import operators.raster_compute as product
    def forbidden(*a,**kw):raise AssertionError('product oracle dependency')
    for name in('raster_region_group','_classification_components','_classification_rows','_region_group_numbers'):monkeypatch.setattr(product,name,forbidden)
    f.expected_pixels.cache_clear()
    try:
        for name in f.TARGETS:assert f.expected_pixels(name).shape==f.case_shape(name)
    finally:f.expected_pixels.cache_clear()


@pytest.mark.parametrize('fault',['schema','count','classification','sum','missing','duplicate','nonfinite'])
def test_region_hosted_csv_corruption_cannot_pass(region_scene,tmp_path,request,fault):
    import csv,io
    original,_,_=region_scene;root=tmp_path/'changed';shutil.copytree(original,root);control=helper.cache_for(f,root,request)
    path=root/'outputs'/'stats-four.csv';reader=csv.DictReader(io.StringIO(path.read_text()));rows=list(reader);columns=reader.fieldnames
    if fault=='schema':columns=list(reversed(columns))
    elif fault=='missing':rows.pop()
    elif fault=='duplicate':rows.append(rows[0])
    else:rows[0][{'count':'valid_count','classification':'mean','sum':'sum','nonfinite':'sum'}[fault]]='nan'if fault=='nonfinite'else'1234567'
    with path.open('w')as out:
        writer=csv.DictWriter(out,fieldnames=columns);writer.writeheader();writer.writerows(rows)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS),control)
