"""Real GDAL/CSV regressions for the independent dual-DEM cut/fill Hosted suite."""
import base64,importlib,json,shutil,subprocess,sys
from pathlib import Path
import pytest

@pytest.fixture(scope='module')
def cut_fill_persistence(tmp_path_factory):
    fixture=importlib.import_module('business.scripts.online-raster-cut-fill-fixture')
    scene=importlib.import_module('scripts.test.raster-cut-fill-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('cut-fill-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('cut-fill-123-1')
    initial=fixture.seed(root)
    for name in fixture.TARGETS:
        definition=scene.definition({n:n for n in fixture.SOURCES},'parent',name)
        localize_cut_fill(definition,root,root/'outputs'/name)
        result=execute_workflow(definition);assert result['status']=='success',result
    return fixture,scene,root,initial


def localize_cut_fill(definition,root,target):
    for task in definition['tasks']:
        params=task['params']
        if task['operator']=='raster_load':
            path=root/params.pop('locator');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(path)}}}
        elif task['operator']=='save':
            params.pop('target_parent_locator');params.pop('target_name');params.update(connection_info={'mount_path':str(target.parent)},path=target.name)


def test_cut_fill_persistence_real_gdal_twelve_csv_chains_complete_oracle_and_original_bytes(cut_fill_persistence,tmp_path):
    f,m,root,initial=cut_fill_persistence;report=f.verify(root,list(f.TARGETS));report['runtime_workspaces_removed']=True;m.check_physical(report,{})
    assert report['sources']==initial['sources'] and len(report['targets'])==12
    archive=m.archive_csv({'ADDP_ONLINE_ARTIFACT_DIR':str(tmp_path)},report)
    assert len(archive)==12
    for name,fact in report['targets'].items():
        assert base64.b64decode(fact['csv_base64'])==(root/'outputs'/name).read_bytes()==(tmp_path/'cut-fill-csv'/name).read_bytes()
        assert archive[name]['size_bytes']==fact['size_bytes']>0
        assert fact['complete_table_verified'] and fact['independent_decimal_oracle_verified']
    a,b=f.expected_row('mixed.csv'),f.expected_row('reversed.csv')
    assert a['cut_volume_m3']==b['fill_volume_m3'] and a['fill_volume_m3']==b['cut_volume_m3'] and a['net_increase_m3']==-b['net_increase_m3']
    assert f.expected_row('unchanged.csv')['cut_count']==f.expected_row('unchanged.csv')['fill_count']==0
    assert f.expected_row('fill-only.csv')['cut_count']==0 and f.expected_row('cut-only.csv')['fill_count']==0
    assert f.expected_row('filled.csv')['fill_count']==f.WIDTH*f.HEIGHT
    assert len(f.COLUMNS)==13


@pytest.mark.parametrize('problem',('before_band','after_band','before_unit','after_unit_conflict','before_alpha','after_complex','before_scale','after_offset','crs','size','transform','rotated','south_up','geographic','no_crs','no_grid','empty','disjoint'))
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_cut_fill_persistence_thirty_six_exact_failures_no_publication(cut_fill_persistence,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    f,m,root,initial=cut_fill_persistence;definition=m.definition({n:n for n in f.SOURCES},'parent','mixed.csv',invalid=problem,language=language);target=tmp_path/'failed.csv';localize_cut_fill(definition,root,target)
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_cut_fill':workspaces.extend(params[n].workspace for n in ('input_raster','after_raster'))
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute(definition)
    assert error.value.error_code=='EXECUTION_FAILED' and str(error.value)=='任务 cut_fill 执行失败: '+m.MESSAGES[problem][language=='en']
    assert workspaces and all(not p.exists() for p in workspaces) and not target.exists()
    assert f.verify(root,list(f.TARGETS))['sources']==initial['sources']


@pytest.mark.parametrize('fault',['value','one_ulp','net_sign','gross_swap','count','area_units','nan','empty_value','extra_row','column_order','missing_column','extra_column','decimal_count','sidecar','source_sidecar','missing_owner','missing','symlink'])
def test_cut_fill_persistence_oracle_rejects_csv_corruption(cut_fill_persistence,tmp_path,fault):
    import csv,math
    f,m,original,_=cut_fill_persistence;root=tmp_path/'changed';shutil.copytree(original,root);path=root/'outputs'/'mixed.csv'
    if fault in ('sidecar','source_sidecar','missing_owner','missing','symlink'):
        if fault=='sidecar':(root/'outputs'/'mixed.csv.extra').write_text('unexpected')
        elif fault=='source_sidecar':(root/'before.tif.aux.xml').write_text('unexpected')
        elif fault=='missing_owner':(root/'.addp-online-owner').unlink()
        elif fault=='missing':path.unlink()
        else:path.unlink();path.symlink_to(original/'outputs'/'mixed.csv')
    else:
        with path.open(newline='') as stream:rows=list(csv.reader(stream))
        if fault=='value':rows[1][10]=str(float(rows[1][10])+1)
        elif fault=='one_ulp':rows[1][10]=repr(math.nextafter(float(rows[1][10]),math.inf))
        elif fault=='net_sign':rows[1][12]=str(-float(rows[1][12]))
        elif fault=='gross_swap':rows[1][10],rows[1][11]=rows[1][11],rows[1][10]
        elif fault=='count':rows[1][1]=str(int(rows[1][1])+1)
        elif fault=='area_units':rows[1][6]=str(float(rows[1][6])/.3048**2)
        elif fault=='nan':rows[1][11]='NaN'
        elif fault=='empty_value':rows[1][9]=''
        elif fault=='extra_row':rows.append(rows[1])
        elif fault=='column_order':rows[0][0],rows[0][1]=rows[0][1],rows[0][0]
        elif fault=='missing_column':rows=[r[:-1] for r in rows]
        elif fault=='extra_column':rows[0].append('unexpected');rows[1].append('0')
        elif fault=='decimal_count':rows[1][0]+='.0'
        with path.open('w',newline='') as stream:csv.writer(stream).writerows(rows)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS))


def test_cut_fill_persistence_independent_period_count_matches_full_scalar_counts(cut_fill_persistence):
    from decimal import Decimal,localcontext
    f,_,_,_=cut_fill_persistence
    # A full-width 519x515 scalar pass independently checks the closed 35-period count.
    a=f.expected_row('mixed.csv');counts=[0,0,0]
    for r in range(f.HEIGHT):
        for c in range(f.WIDTH):
            p,pa,pm=f.stored_pixel('before.tif',r,c);q,qa,qm=f.stored_pixel('after.tif',r,c)
            import math
            if not(pm and qm and math.isfinite(p) and math.isfinite(q) and p!=-9999 and q!=-9999 and math.isfinite(pa) and pa>0 and math.isfinite(qa) and qa>0):continue
            counts[0 if p>q else 1 if p<q else 2]+=1
    assert counts==[a['cut_count'],a['fill_count'],a['unchanged_count']]


def test_cut_fill_persistence_worker_runs_from_stdin_without_repository_imports(cut_fill_persistence,tmp_path):
    f,_,root,_=cut_fill_persistence
    code=Path(f.__file__).read_text();env=__import__('os').environ.copy();env['TMPDIR']=str(tmp_path)
    result=subprocess.run([sys.executable,'-','--worker','verify',str(root),*f.TARGETS],input=code,text=True,capture_output=True,cwd=tmp_path,env=env,timeout=90)
    assert result.returncode==0,result.stderr
    report=json.loads(result.stdout);assert report['runtime_workspaces_removed'] and len(report['targets'])==12
