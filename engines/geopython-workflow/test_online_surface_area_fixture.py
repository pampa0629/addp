"""Real GDAL/CSV regressions for the independent dual-DEM surface area Hosted suite."""
import base64,importlib,json,shutil,subprocess,sys
from pathlib import Path
import pytest

@pytest.fixture(scope='module')
def surface_area_persistence(tmp_path_factory):
    fixture=importlib.import_module('business.scripts.online-raster-surface-area-fixture')
    scene=importlib.import_module('scripts.test.raster-surface-area-persistence-online')
    from workflow_engine import execute_workflow
    root=tmp_path_factory.mktemp('surface-area-export');(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('surface-area-123-1')
    initial=fixture.seed(root)
    for name in fixture.TARGETS:
        definition=scene.definition({n:n for n in fixture.SOURCES},'parent',name)
        localize_surface_area(definition,root,root/'outputs'/name)
        result=execute_workflow(definition);assert result['status']=='success',result
        physical=fixture.verify(root,[*list(fixture.TARGETS)[:list(fixture.TARGETS).index(name)+1]])
        scene.validate_preview(json.loads(result['all_results']['surface_area']),name,physical['targets'][name])
    return fixture,scene,root,initial


def localize_surface_area(definition,root,target):
    for task in definition['tasks']:
        params=task['params']
        if task['operator']=='raster_load':
            path=root/params.pop('locator');params['access_plan']={'schema_version':'addp.workflow.access-plan/v1','source':{'kind':'file','format':'tiff','access':{'method':'mounted_path','path':str(path)}}}
        elif task['operator']=='save':
            params.pop('target_parent_locator');params.pop('target_name');params.update(connection_info={'mount_path':str(target.parent)},path=target.name)


def test_surface_area_persistence_real_gdal_twelve_csv_chains_complete_oracle_and_original_bytes(surface_area_persistence,tmp_path):
    f,m,root,initial=surface_area_persistence;report=f.verify(root,list(f.TARGETS));report['runtime_workspaces_removed']=True;m.check_physical(report,{})
    assert report['sources']==initial['sources']and len(report['targets'])==12
    archive=m.archive_csv({'ADDP_ONLINE_ARTIFACT_DIR':str(tmp_path)},report);assert len(archive)==12
    for name,fact in report['targets'].items():
        assert base64.b64decode(fact['csv_base64'])==(root/'outputs'/name).read_bytes()==(tmp_path/'surface-area-csv'/name).read_bytes()
        assert archive[name]['size_bytes']==fact['size_bytes']>0 and fact['complete_table_verified']and fact['independent_decimal_oracle_verified']
        row=f.parse_csv((root/'outputs'/name).read_bytes(),name,fact)[0]
        assert row['total_quad_count']==row['valid_quad_count']+row['invalid_quad_count']
        assert row['projected_area_m2']<=row['surface_area_low_m2']<=row['surface_area_mean_m2']<=row['surface_area_high_m2']
        assert row['surface_to_projected_ratio']>=1
    flat=f.expected_row('flat.csv');assert flat['projected_area_m2']==flat['surface_area_mean_m2']==(f.WIDTH-1)*(f.HEIGHT-1)*6 and flat['surface_to_projected_ratio']==1
    assert f.expected_row('single.csv')['total_quad_count']==f.expected_row('single.csv')['valid_quad_count']==1
    assert f.expected_row('diagonals.csv')['surface_area_low_m2']<f.expected_row('diagonals.csv')['surface_area_high_m2']
    assert f.expected_row('filled.csv')['surface_to_projected_ratio']==1
    assert f.expected_row('int64.csv')['surface_to_projected_ratio']>1 and f.expected_row('uint64.csv')['surface_to_projected_ratio']>1
    assert len(f.COLUMNS)==8


@pytest.mark.parametrize('problem',('band','alpha','complex','complex_alpha','rotated','south_up','geographic','no_crs','no_grid','unit','unit_conflict','unit_unknown','scale','offset','all_invalid','no_quad','width_one','height_one'))
@pytest.mark.parametrize('language',['zh-cn','en'])
def test_surface_area_persistence_thirty_six_exact_failures_no_publication(surface_area_persistence,tmp_path,monkeypatch,problem,language):
    import workflow_engine
    from workflow_engine import GeoPythonWorkflowRunner
    from addp_common.workflow_runtime.errors import WorkflowExecutionError
    f,m,root,initial=surface_area_persistence;definition=m.definition({n:n for n in f.SOURCES},'parent','mixed.csv',invalid=problem,language=language);target=tmp_path/'failed.csv';localize_surface_area(definition,root,target)
    workspaces=[];execute=workflow_engine._execute_operator
    def capture(name,params):
        if name=='raster_surface_area':workspaces.append(params['input_raster'].workspace)
        return execute(name,params)
    monkeypatch.setattr(workflow_engine,'_execute_operator',capture)
    with pytest.raises(WorkflowExecutionError) as error:GeoPythonWorkflowRunner(language=language).execute(definition)
    assert error.value.error_code=='EXECUTION_FAILED' and str(error.value)=='任务 surface_area 执行失败: '+m.MESSAGES[problem][language=='en']
    assert workspaces and all(not p.exists() for p in workspaces) and not target.exists()
    assert f.verify(root,list(f.TARGETS))['sources']==initial['sources']


@pytest.mark.parametrize('fault',['value','one_ulp','negative_area','diagonal_swap','count','area_units','nan','empty_value','extra_row','column_order','missing_column','extra_column','decimal_count','sidecar','source_sidecar','missing_owner','missing','symlink'])
def test_surface_area_persistence_oracle_rejects_csv_corruption(surface_area_persistence,tmp_path,fault):
    import csv,math
    f,m,original,_=surface_area_persistence;root=tmp_path/'changed';shutil.copytree(original,root);path=root/'outputs'/'mixed.csv'
    if fault in ('sidecar','source_sidecar','missing_owner','missing','symlink'):
        if fault=='sidecar':(root/'outputs'/'mixed.csv.extra').write_text('unexpected')
        elif fault=='source_sidecar':(root/'mixed.tif.aux.xml').write_text('unexpected')
        elif fault=='missing_owner':(root/'.addp-online-owner').unlink()
        elif fault=='missing':path.unlink()
        else:path.unlink();path.symlink_to(original/'outputs'/'mixed.csv')
    else:
        with path.open(newline='') as stream:rows=list(csv.reader(stream))
        if fault=='value':rows[1][6]=str(float(rows[1][6])+1)
        elif fault=='one_ulp':rows[1][6]=repr(math.nextafter(float(rows[1][6]),math.inf))
        elif fault=='negative_area':rows[1][7]=str(-float(rows[1][7]))
        elif fault=='diagonal_swap':rows[1][4],rows[1][5]=rows[1][5],rows[1][4]
        elif fault=='count':rows[1][1]=str(int(rows[1][1])+1)
        elif fault=='area_units':rows[1][3]=str(float(rows[1][3])/.3048**2)
        elif fault=='nan':rows[1][5]='NaN'
        elif fault=='empty_value':rows[1][6]=''
        elif fault=='extra_row':rows.append(rows[1])
        elif fault=='column_order':rows[0][0],rows[0][1]=rows[0][1],rows[0][0]
        elif fault=='missing_column':rows=[r[:-1] for r in rows]
        elif fault=='extra_column':rows[0].append('unexpected');rows[1].append('0')
        elif fault=='decimal_count':rows[1][0]+='.0'
        with path.open('w',newline='') as stream:csv.writer(stream).writerows(rows)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS))


def test_surface_area_persistence_independent_full_scalar_four_corner_count(surface_area_persistence):
    import numpy as np
    f,_,_,_=surface_area_persistence;values,alpha,mask=f.source_arrays('mixed.tif');spec=f.source_spec('mixed.tif')
    valid=np.isfinite(values)&(values!=spec['nodata'])&np.isfinite(alpha)&(alpha>0)&(mask!=0)
    count=sum(bool(valid[r,c]and valid[r,c+1]and valid[r+1,c]and valid[r+1,c+1])for r in range(f.HEIGHT-1)for c in range(f.WIDTH-1))
    expected=f.expected_row('mixed.csv');assert expected['valid_quad_count']==count and expected['invalid_quad_count']>0


@pytest.mark.parametrize('fault',['half','one_diagonal','frame_area','ratio','mean_from_extrema'])
def test_surface_area_independent_csv_oracle_rejects_wrong_semantics(surface_area_persistence,tmp_path,fault):
    import csv
    f,_,original,_=surface_area_persistence;root=tmp_path/'changed';shutil.copytree(original,root);path=root/'outputs'/'diagonals.csv'
    with path.open(newline='')as stream:rows=list(csv.reader(stream))
    if fault=='half':rows[1][6]=repr(float(rows[1][6])/2)
    elif fault=='one_diagonal':rows[1][6]=rows[1][4]
    elif fault=='frame_area':rows[1][3]=str(f.WIDTH*f.HEIGHT*6)
    elif fault=='ratio':rows[1][7]='1.0'
    else:rows[1][4]=rows[1][5]=rows[1][6]
    with path.open('w',newline='')as stream:csv.writer(stream).writerows(rows)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.TARGETS))


@pytest.mark.parametrize('fault',['bytes','size','hash','missing_entry','extra_entry','missing_manifest','symlink'])
def test_surface_area_original_csv_manifest_never_silently_repaired(surface_area_persistence,tmp_path,fault):
    f,m,root,_=surface_area_persistence;facts=f.verify(root,list(f.TARGETS));facts['runtime_workspaces_removed']=True;env={'ADDP_ONLINE_ARTIFACT_DIR':str(tmp_path)};m.archive_csv(env,facts)
    path=tmp_path/'surface-area-csv'/'mixed.csv';manifest=tmp_path/'surface-area-csv-manifest.json'
    if fault=='bytes':path.write_bytes(b'changed')
    elif fault=='missing_manifest':manifest.unlink()
    elif fault=='symlink':manifest.unlink();manifest.symlink_to(tmp_path/'missing')
    else:
        value=json.loads(manifest.read_text())
        if fault=='missing_entry':value.pop('mixed.csv')
        elif fault=='extra_entry':value['extra.csv']=value['mixed.csv']
        else:value['mixed.csv']['size_bytes'if fault=='size'else'sha256']=1 if fault=='size'else'bad'
        manifest.write_text(json.dumps(value))
    with pytest.raises(m.SuiteError):m.archive_csv(env,facts)


def test_surface_area_persistence_worker_runs_from_stdin_without_repository_imports(surface_area_persistence,tmp_path):
    f,_,root,_=surface_area_persistence
    code=Path(f.__file__).read_text();env=__import__('os').environ.copy();env['TMPDIR']=str(tmp_path)
    result=subprocess.run([sys.executable,'-','--worker','verify',str(root),*f.TARGETS],input=code,text=True,capture_output=True,cwd=tmp_path,env=env,timeout=90)
    assert result.returncode==0,result.stderr
    report=json.loads(result.stdout);assert report['runtime_workspaces_removed'] and len(report['targets'])==12


def test_surface_area_host_oracle_requires_no_numpy_pandas_or_gdal(surface_area_persistence,tmp_path):
    f,_,_,_=surface_area_persistence
    code="import builtins,sys; original=builtins.__import__\ndef guarded(name,*a,**k):\n if name.split('.')[0]in ('numpy','pandas','osgeo'):raise AssertionError('Runtime dependency leaked into host oracle')\n return original(name,*a,**k)\nbuiltins.__import__=guarded;sys.argv=['fixture','--worker'];scope={'__name__':'fixture'};exec(compile(sys.stdin.read(),'<fixture>','exec'),scope);assert len([scope['expected_row'](n)for n in scope['TARGETS']])==12\n"
    result=subprocess.run([sys.executable,'-I','-c',code],input=Path(f.__file__).read_text(),text=True,capture_output=True,cwd=tmp_path,timeout=30)
    assert result.returncode==0,result.stderr
