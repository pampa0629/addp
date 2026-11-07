"""Actual GDAL/COG and the independently frozen Hosted viewshed matrix."""
import hashlib
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys
import pytest

fixture=importlib.import_module('business.scripts.online-raster-viewshed-fixture')
scene=importlib.import_module('scripts.test.raster-viewshed-persistence-online')


def localized(root,name,**kwargs):
    dag=scene.definition({n:n for n in fixture.SOURCES},'parent',name,**kwargs)
    load,save=dag['tasks'][0],dag['tasks'][-1]
    source=root/load['params'].pop('locator')
    load['params']['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(source)}}}
    params=save['params'];name=params.pop('target_name');params.pop('target_parent_locator');params.pop('write_mode')
    params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','target':{'kind':'file','format':'tiff','name':name,'write_mode':'create','access':{'method':'mounted_path','path':str(root/'outputs'/name)}}}
    return dag


@pytest.fixture(scope='module')
def viewshed_export(tmp_path_factory):
    root=tmp_path_factory.mktemp('viewshed-export');(root/'.addp-online-owner').write_text('viewshed-123-1');(root/'outputs').mkdir()
    runtime_tmp=tmp_path_factory.mktemp('viewshed-runtime-private')
    import tempfile
    with pytest.MonkeyPatch.context()as context:
        context.setattr(tempfile,'tempdir',str(runtime_tmp))
        frozen=fixture.seed(root)
        scope={'schema':fixture.CACHE_SCHEMA,'run_id':'viewshed-123-1','source_root':str(root.resolve()),
            'fixture_sha256':fixture.digest(Path(fixture.__file__))['sha256'],'sources':frozen['sources']}
        prepared=fixture.prepare_oracle(root,scope);control={'scope':scope,'arrays':prepared['arrays']}
        yield root,frozen,prepared,control
        assert fixture.cleanup_oracle(root,control)['oracle_cache_removed']



def test_viewshed_real_sixteen_cogs_complete_pixels_masks_and_immutable_originals(viewshed_export,tmp_path):
    from workflow_engine import execute_workflow
    import numpy as np
    root,frozen,prepared,control=viewshed_export;names=[];checks=0
    for name in fixture.TARGETS:
        result=execute_workflow(localized(root,name));assert result['status']=='success',result
        scene.validate_artifact(json.loads(result['final_result']),name)
        names.append(name);native=fixture.verify(root,names,control);native['runtime_workspaces_removed']=True
        native['archived_cogs']=fixture.archive_cogs(root,tmp_path,native)
        scene.check_and_archive(native,names,frozen['sources'],prepared['expectations'],tmp_path/'viewshed-cogs',{}if len(names)==1 else previous,fixture.scope_hash(control['scope']))
        previous=native['archived_cogs'];checks+=len(names)
        actual=fixture.load_expected(control,fixture.cache_directory(control['scope']),name)
        assert actual.dtype==np.uint8 and set(np.unique(actual))<={0,1,2,255}
    assert checks==136 and len(previous)==16
    for name,fact in previous.items():
        assert (tmp_path/fact['path']).read_bytes()==(root/'outputs'/name).read_bytes()
        assert fixture.digest(tmp_path/fact['path'])=={k:fact[k]for k in('size_bytes','sha256')}
    for problem in scene.FAILURES:
        for language in('zh-cn','en'):
            from workflow_engine import GeoPythonWorkflowRunner
            from addp_common.workflow_runtime import WorkflowExecutionError
            with pytest.raises(WorkflowExecutionError)as error:GeoPythonWorkflowRunner(language=language).execute(localized(root,'flat.cog.tif',invalid=problem,language=language))
            assert str(error.value)=='任务 view 执行失败: '+scene.diagnostic_fact('native',problem,language)['exact_cause']
            native=fixture.verify(root,names,control);native['runtime_workspaces_removed']=True;native['archived_cogs']=fixture.archive_cogs(root,tmp_path,native)
            assert scene.check_and_archive(native,names,frozen['sources'],prepared['expectations'],tmp_path/'viewshed-cogs',previous,fixture.scope_hash(control['scope']))==previous
            checks+=len(names)
    assert checks==712
    # Worker stdin must work without importing the project or a caller cwd.
    isolated=tmp_path/'worker';isolated.mkdir()
    result=subprocess.run([sys.executable,'-','--worker','verify',str(root),json.dumps(control),*names],
        input=Path(fixture.__file__).read_text(),text=True,capture_output=True,cwd=isolated,
        env={**os.environ,'TMPDIR':str(fixture.oracle_path(control['scope']).parent)},timeout=90)
    assert result.returncode==0,result.stderr
    assert json.loads(result.stdout)['runtime_workspaces_removed']is True
    (tmp_path/previous[names[0]]['path']).unlink()
    with pytest.raises((scene.SuiteError,fixture.FixtureError)):scene.check_and_archive(native,names,frozen['sources'],prepared['expectations'],tmp_path/'viewshed-cogs',previous,fixture.scope_hash(control['scope']))


@pytest.mark.parametrize('language',['zh-cn','en'])
@pytest.mark.parametrize('problem',scene.FAILURES)
def test_viewshed_real_exact_failure_node_language_and_zero_publication(viewshed_export,monkeypatch,language,problem):
    from addp_common.workflow_runtime import WorkflowExecutionError
    import workflow_engine
    root,frozen,_,_=viewshed_export;before={p.name:fixture.digest(p)for p in(root/'outputs').iterdir()}
    workspaces=[];original=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_viewshed':workspaces.append(params['input_raster'].workspace)
        return original(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    with pytest.raises(WorkflowExecutionError)as error:workflow_engine.GeoPythonWorkflowRunner(language=language).execute(localized(root,'flat.cog.tif',invalid=problem,language=language))
    assert str(error.value)=='任务 view 执行失败: '+scene.diagnostic_fact('native',problem,language)['exact_cause']
    assert error.value.error_code=='EXECUTION_FAILED'and workspaces and all(not p.exists()for p in workspaces)
    assert before=={p.name:fixture.digest(p)for p in(root/'outputs').iterdir()}
    assert frozen['sources']=={n:fixture.digest(root/n)for n in fixture.SOURCES}


@pytest.mark.parametrize('pollution',['pixels','nodata','mask','transform','crs','dtype','overview','cache','scope'])
def test_viewshed_independent_oracle_rejects_corrupt_actual_or_frozen_facts(viewshed_export,tmp_path,pollution):
    from osgeo import gdal,osr
    import numpy as np
    root,_,_,control=viewshed_export
    # Own copy: negative evidence must not poison another parametrized case.
    import shutil
    copied=Path(shutil.copytree(root,tmp_path/'export'))
    (copied/'outputs').mkdir(exist_ok=True)
    # Cache identity is deliberately the original root; never bypass its ownership checks.
    if pollution in('scope','cache'):
        import copy
        bad=copy.deepcopy(control)
        if pollution=='scope':bad['scope']['run_id']='viewshed-999-1'
        else:bad['arrays'][fixture.TARGETS[0]]['sha256']='b'*64
        with pytest.raises(fixture.FixtureError):fixture.verify(root,fixture.TARGETS,bad)
        return
    name=fixture.TARGETS[0];path=root/'outputs'/name
    if not path.exists():
        from workflow_engine import execute_workflow
        assert execute_workflow(localized(root,name))['status']=='success'
    payload=path.read_bytes()
    try:
        ds=gdal.OpenEx(str(path),gdal.OF_RASTER|gdal.OF_UPDATE,open_options=['IGNORE_COG_LAYOUT_BREAK=YES']);band=ds.GetRasterBand(1)
        if pollution=='pixels':band.WriteArray(np.array([[2]],np.uint8),256,256)
        elif pollution=='nodata':band.SetNoDataValue(254)
        elif pollution=='mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):band.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().WriteArray(np.zeros((fixture.HEIGHT,fixture.WIDTH),np.uint8))
        elif pollution=='transform':grid=list(ds.GetGeoTransform());grid[0]+=1;ds.SetGeoTransform(grid)
        elif pollution=='crs':crs=osr.SpatialReference();crs.ImportFromEPSG(32650);ds.SetSpatialRef(crs)
        elif pollution=='overview':band.GetOverview(0).WriteArray(np.array([[2]],np.uint8),0,0)
        band=ds=None
        if pollution=='dtype':
            source=gdal.Open(str(path));temp=path.with_suffix('.wrong.tif');wrong=gdal.Translate(str(temp),source,outputType=gdal.GDT_Float64);wrong=source=None;path.write_bytes(temp.read_bytes());temp.unlink()
        with pytest.raises(fixture.FixtureError):fixture.verify(root,[n for n in fixture.TARGETS if(root/'outputs'/n).exists()],control)
    finally:path.write_bytes(payload)
