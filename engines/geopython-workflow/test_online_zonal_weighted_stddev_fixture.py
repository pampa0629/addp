"""Actual GDAL/CSV oracle and accurate rejection tests; no Hosted claims."""
import copy
import csv
import importlib
import json
import math
from pathlib import Path
import shutil
import subprocess
import sys
import os
import pytest
from workflow_engine import execute_workflow, GeoPythonWorkflowRunner
from addp_common.workflow_runtime.errors import WorkflowExecutionError

f = importlib.import_module('business.scripts.online-raster-zonal-weighted-stddev-fixture')
m = importlib.import_module('scripts.test.raster-zonal-weighted-stddev-persistence-online')

def localize(dag, root, target):
    for task in dag['tasks']:
        p = task['params']
        if task['operator'] == 'raster_load':
            source = root/p.pop('locator')
            p['access_plan'] = {'schema_version': 'addp.workflow.access-plan/v1', 'source': {'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(source)}}}
        elif task['operator'] == 'save':
            p.pop('target_parent_locator'); p.pop('target_name'); p.update(connection_info={'mount_path': str(target.parent)}, path=target.name)

@pytest.fixture(scope='module')
def zonal_sources(tmp_path_factory):
    root = tmp_path_factory.mktemp('zonal-weighted-stddev'); (root/'outputs').mkdir(); (root/'.addp-online-owner').write_text('owned')
    initial = f.seed(root); expected = f.oracle(); (root/f.ORACLE).write_bytes(f.encoded(expected))
    previews = {}
    for name in f.CASES:
        dag = m.definition({n:n for n in f.SOURCES}, 'parent', name); localize(dag, root, root/'outputs'/name)
        result = execute_workflow(dag); assert result['status'] == 'success', result
        previews[name] = json.loads(result['final_result'])
    return root, initial, expected, previews


def test_zonal_weighted_stddev_sixteen_real_csvs_complete_independent_oracle_and_raw_archive(zonal_sources, tmp_path):
    root, initial, expected, previews = zonal_sources
    report = f.verify(root, list(f.CASES), expected)
    assert report['sources'] == initial['sources'] and report['full_csv_checks'] == 16
    archive = f.archive_files(root/'outputs', tmp_path, list(f.CASES), {}, 'zonal-weighted-stddev-csvs')
    assert len(archive) == 16
    for name in f.CASES:
        m.validate_preview(previews[name], expected[name], name)
        assert (tmp_path/archive[name]['path']).read_bytes() == (root/'outputs'/name).read_bytes()
        assert archive[name]['size_bytes'] > 0
    assert len(expected['weighted.csv']) == 79 and previews['weighted.csv']['truncated'] is True
    assert expected['empty-zones.csv'] == [] and all(r['weighted_mean'] is None for r in expected['invalid-values.csv'])
    assert all([r[k] for k in f.WEIGHT_COLUMNS] == [0.,0.,None,None] for r in expected['zero.csv'])
    assert [r['zone_value'] for r in expected['int64.csv']] == [0,2**63-2,2**63-1]
    ones=f.read_csv(root/'outputs/ones.csv','ones.csv')
    assert all(r['weighted_stddev']==r['stddev']for r in ones)
    assert sum(f.enabled(n)for n in f.CASES)==13
    mixed=f.read_csv(root/'outputs/mixed-empty.csv','mixed-empty.csv')
    assert mixed[0]['weighted_stddev']==mixed[1]['weighted_stddev']==0.
    assert mixed[2]['weighted_stddev']==math.sqrt(18.75)and mixed[2]['stddev']!=mixed[2]['weighted_stddev']
    assert mixed[3]['weighted_stddev']is None and mixed[4]['weighted_stddev']is None
    assert sum(case[2] is not None for case in f.CASES.values()) == 15

@pytest.mark.parametrize('problem', f.FAILURES)
@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_zonal_weighted_stddev_ninety_eight_exact_submitted_failures_and_no_output(zonal_sources, tmp_path, monkeypatch, problem, language):
    import workflow_engine
    root, initial, expected, _ = zonal_sources
    dag = m.definition({n:n for n in f.SOURCES}, 'parent', 'coverage.csv', invalid=problem, language=language)
    target = tmp_path/'failed.csv'; localize(dag, root, target)
    workspaces = []; original = workflow_engine._execute_operator
    def track(name, params):
        if name == 'raster_zonal_statistics': workspaces.append(params['input_raster'].workspace)
        return original(name, params)
    monkeypatch.setattr(workflow_engine, '_execute_operator', track)
    with pytest.raises(WorkflowExecutionError) as error: GeoPythonWorkflowRunner(language=language).execute(dag)
    assert error.value.error_code == 'EXECUTION_FAILED' and str(error.value) == '任务 analysis 执行失败: '+m.MESSAGES[problem][language == 'en']
    assert workspaces and all(not p.exists() for p in workspaces) and not target.exists()
    assert f.verify(root, list(f.CASES), expected)['sources'] == initial['sources']

@pytest.mark.parametrize('fault', ['weighted-stddev', 'one-ulp', 'class', 'count', 'null', 'column-order', 'extra-row', 'nonfinite', 'source-mask', 'sidecar', 'source-symlink', 'target-symlink', 'directory-symlink'])
def test_zonal_weighted_stddev_real_physical_corruption_is_rejected(zonal_sources, tmp_path, fault):
    from osgeo import gdal
    import numpy as np
    original, _, expected, _ = zonal_sources; root = tmp_path/'changed'; shutil.copytree(original, root)
    path = root/'outputs'/'ones.csv'
    if fault in ('source-symlink','target-symlink','directory-symlink'):
        chosen = root/'values.tif' if fault == 'source-symlink' else root/'outputs' if fault == 'directory-symlink' else path
        moved = tmp_path/'moved';chosen.rename(moved);chosen.symlink_to(moved,target_is_directory=fault == 'directory-symlink')
    elif fault == 'source-mask':
        ds = gdal.Open(str(root/'values.tif'), gdal.GA_Update); ds.GetRasterBand(1).GetMaskBand().WriteArray(np.zeros((1,1), np.uint8), 10, 10); ds = None
    elif fault == 'sidecar': (root/'outputs'/'unrequested.csv').write_text('unexpected')
    else:
        with path.open(newline='') as stream: rows = list(csv.reader(stream))
        row_index = next(i for i, row in enumerate(rows[1:], 1) if row[-1] != '' and float(row[-1]) != 0.)
        if fault == 'weighted-stddev': rows[row_index][-1] = str(float(rows[row_index][-1])+1)
        elif fault == 'one-ulp': rows[row_index][-1] = repr(math.nextafter(float(rows[row_index][-1]), math.inf))
        elif fault == 'class': rows[row_index][0] = str(int(rows[row_index][0])+1)
        elif fault == 'count': rows[row_index][1] += '.0'
        elif fault == 'null': rows[row_index][-1] = ''
        elif fault == 'column-order': rows[0][0], rows[0][1] = rows[0][1], rows[0][0]
        elif fault == 'extra-row': rows.append(rows[row_index])
        elif fault == 'nonfinite': rows[row_index][-1] = 'Infinity'
        with path.open('w', newline='') as stream: csv.writer(stream).writerows(rows)
    with pytest.raises(f.FixtureError): f.verify(root, list(f.CASES), expected)

@pytest.mark.parametrize('fault', ['changed', 'deleted', 'manifest', 'extra'])
def test_zonal_weighted_stddev_csv_archive_never_repairs_or_overwrites(zonal_sources, tmp_path, fault):
    root, _, _, _ = zonal_sources
    receipts = f.archive_files(root/'outputs', tmp_path, list(f.CASES), {}, 'zonal-weighted-stddev-csvs')
    path = tmp_path/receipts['weighted.csv']['path']
    if fault == 'changed': path.chmod(0o644); path.write_bytes(path.read_bytes()+b' ')
    elif fault == 'deleted': path.unlink()
    elif fault == 'manifest': (tmp_path/'zonal-weighted-stddev-csvs-manifest.json').write_text('{}')
    else: (path.parent/'extra.csv').write_text('extra')
    with pytest.raises(f.FixtureError): f.archive_files(root/'outputs', tmp_path, list(f.CASES), receipts, 'zonal-weighted-stddev-csvs')


def test_zonal_weighted_stddev_worker_stdin_frozen_oracle_poison_and_cleanup(tmp_path):
    root = tmp_path/'source'; root.mkdir(); (root/'outputs').mkdir(); (root/'.addp-online-owner').write_text('owned')
    f.seed(root); scratch = tmp_path/'runtime-tmp'; scratch.mkdir()
    result = subprocess.run([sys.executable, '-', '--worker', 'oracle', str(root)], input=Path(f.__file__).read_text(),
        cwd=tmp_path, env={**os.environ, 'TMPDIR': str(scratch)}, capture_output=True, text=True, timeout=90)
    assert result.returncode == 0, result.stderr
    evidence = json.loads(result.stdout); assert len(evidence['expectations']) == 16 and evidence['full_source_pixels_verified'] is True
    path = root/f.ORACLE; path.chmod(0o644); path.write_bytes(path.read_bytes()+b' ')
    with pytest.raises(f.FixtureError, match='oracle cache hash changed'): f.worker('verify', root, [evidence['oracle_sha256']])
    assert f.worker('cleanup-oracle', root, []) == {'oracle_cache_removed': True} and not path.exists()
    assert not any(text in Path(f.__file__).read_text() for text in ('operators.raster_compute', 'np.percentile', 'np.quantile'))


@pytest.mark.parametrize('fault',['normalize-sum','drop-scale','zero-as-null','no-valid-as-zero','ignore-weights','native-nodata'])
def test_zonal_weighted_stddev_independent_oracle_rejects_wrong_weight_semantics(zonal_sources,tmp_path,fault):
    original,_,expected,_=zonal_sources;root=tmp_path/'changed';shutil.copytree(original,root)
    name = {'zero-as-null':'zero.csv','no-valid-as-zero':'invalid-values.csv','drop-scale':'scaled.csv','native-nodata':'int64.csv'}.get(fault,'weighted.csv')
    path=root/'outputs'/name
    with path.open(newline='') as stream: rows=list(csv.DictReader(stream)); fields=list(f.columns(name))
    row=next((r for r in rows if r['weighted_mean'] not in ('','0.0','0')),rows[0])
    if fault == 'normalize-sum': row['weighted_sum']=row['weighted_mean']
    elif fault == 'drop-scale': row['weight_sum']=str(float(row['weight_sum'])/8)
    elif fault == 'zero-as-null': row['weight_sum']=''
    elif fault == 'no-valid-as-zero': row['weight_sum']='0'
    elif fault == 'ignore-weights': row['weighted_sum']=row['sum']
    else: row['weighted_sum']=str(float(row['weighted_sum'])+1e30)
    with path.open('w',newline='') as stream:
        writer=csv.DictWriter(stream,fieldnames=fields);writer.writeheader();writer.writerows(rows)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.CASES),expected)


@pytest.mark.parametrize('fault',['sample-denominator','one-ulp-stddev','zero-stddev','zero-as-zero','disabled-extra-column','all-one-both'])
def test_zonal_weighted_stddev_rejects_stddev_semantics_and_unchanged_original_controls(zonal_sources,tmp_path,fault):
    original,_,expected,_=zonal_sources;root=tmp_path/'changed';shutil.copytree(original,root)
    name={'zero-as-zero':'zero.csv','disabled-extra-column':'control-false.csv','all-one-both':'ones.csv'}.get(fault,'weighted.csv')
    path=root/'outputs'/name
    with path.open(newline='')as stream:reader=csv.DictReader(stream);fields=reader.fieldnames;rows=list(reader)
    row=next((v for v in rows if v.get('weighted_stddev')not in(None,'','0.0','0')),rows[0])
    if fault=='sample-denominator':row['weighted_stddev']=str(float(row['weighted_stddev'])*math.sqrt(float(row['weight_sum'])/(float(row['weight_sum'])-1)))
    elif fault=='one-ulp-stddev':row['weighted_stddev']=repr(math.nextafter(float(row['weighted_stddev']),math.inf))
    elif fault in('zero-stddev','zero-as-zero'):row['weighted_stddev']='0'
    elif fault=='disabled-extra-column':fields.append('weighted_stddev');[v.update(weighted_stddev='0')for v in rows]
    else:
        changed=repr(math.nextafter(float(row['weighted_stddev']),math.inf));row['stddev']=row['weighted_stddev']=changed
    with path.open('w',newline='')as stream:writer=csv.DictWriter(stream,fieldnames=fields);writer.writeheader();writer.writerows(rows)
    with pytest.raises(f.FixtureError):f.verify(root,list(f.CASES),expected)
