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

f = importlib.import_module('business.scripts.online-raster-zonal-class-frequency-fixture')
m = importlib.import_module('scripts.test.raster-zonal-class-frequency-persistence-online')

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
    root = tmp_path_factory.mktemp('zonal-class-frequency'); (root/'outputs').mkdir(); (root/'.addp-online-owner').write_text('owned')
    initial = f.seed(root); expected = f.oracle(); (root/f.ORACLE).write_bytes(f.encoded(expected))
    previews = {}
    for name in f.CASES:
        dag = m.definition({n:n for n in f.SOURCES}, 'parent', name); localize(dag, root, root/'outputs'/name)
        result = execute_workflow(dag); assert result['status'] == 'success', result
        previews[name] = json.loads(result['final_result'])
        proof = f.verify(root, list(previews), expected); assert proof['full_csv_checks'] == len(previews) and proof['sources'] == initial['sources']
    return root, initial, expected, previews


def test_zonal_class_frequency_sixteen_real_csvs_complete_independent_oracle_and_raw_archive(zonal_sources, tmp_path):
    root, initial, expected, previews = zonal_sources
    report = f.verify(root, list(f.CASES), expected)
    assert report['sources'] == initial['sources'] and report['full_csv_checks'] == 16
    archive = f.archive_files(root/'outputs', tmp_path, list(f.CASES), {}, 'zonal-class-frequency-csvs')
    assert len(archive) == 16
    for name in f.CASES:
        m.validate_preview(previews[name], expected[name], name)
        assert (tmp_path/archive[name]['path']).read_bytes() == (root/'outputs'/name).read_bytes()
        assert archive[name]['size_bytes'] > 0
    assert len(expected['cross-blocks.csv']) > 50 and previews['cross-blocks.csv']['truncated'] is True
    assert expected['empty-zones.csv'] == []
    assert all(row['class_value'] is None and row['count'] == 0 and row['proportion'] is None for row in expected['invalid-values.csv'])
    assert any(row['class_value'] == 0 and row['count'] > 0 for row in expected['direct.csv'])
    assert any(row['class_value'] is None for row in expected['mixed-diagnostic.csv'])
    assert any(abs(row['zone_value']) > 2**53 for row in expected['native.csv'])
    assert expected['original.csv'] == expected['summary.csv']
    for name in f.CASES:
        if f.CASES[name][2].startswith('summary'): continue
        for zone in {row['zone_value'] for row in expected[name]}:
            members = [row for row in expected[name] if row['zone_value'] == zone]
            assert sum(row['count'] for row in members) == members[0]['valid_count']
            assert members[0]['total_count'] == members[0]['valid_count']+members[0]['invalid_count']

@pytest.mark.parametrize('problem', f.FAILURES)
@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_zonal_class_frequency_sixty_four_exact_submitted_failures_and_no_output(zonal_sources, tmp_path, monkeypatch, problem, language):
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

@pytest.mark.parametrize('fault', ['denominator', 'one-ulp', 'class', 'count', 'null', 'column-order', 'extra-row', 'nonfinite', 'source-mask', 'sidecar', 'diagnostic-zero', 'diagnostic-drop', 'order', 'real-zero'])
def test_zonal_class_frequency_real_physical_corruption_is_rejected(zonal_sources, tmp_path, fault):
    from osgeo import gdal
    import numpy as np
    original, _, expected, _ = zonal_sources; root = tmp_path/'changed'; shutil.copytree(original, root)
    name = 'mixed-diagnostic.csv' if fault.startswith('diagnostic') else 'direct.csv'
    path = root/'outputs'/name
    if fault == 'source-mask':
        ds = gdal.Open(str(root/'values.tif'), gdal.GA_Update); ds.GetRasterBand(1).GetMaskBand().WriteArray(np.zeros((1,1), np.uint8), 0, 0); ds = None
    elif fault == 'sidecar': (root/'outputs'/'unrequested.csv').write_text('unexpected')
    else:
        with path.open(newline='') as stream: rows = list(csv.reader(stream))
        i = 1 if fault.startswith('diagnostic') else next(i for i,row in enumerate(rows[1:],1) if row[3] and float(row[3]) > 0 and row[4] != row[5])
        if fault == 'denominator': rows[i][3] = repr(int(rows[i][2])/int(rows[i][4]))
        elif fault == 'one-ulp': rows[i][3] = repr(math.nextafter(float(rows[i][3]), math.inf))
        elif fault == 'class': rows[i][1] = str(int(rows[i][1])+1)
        elif fault == 'count': rows[i][2] += '.0'
        elif fault == 'null': rows[i][3] = ''
        elif fault == 'column-order': rows[0][0], rows[0][1] = rows[0][1], rows[0][0]
        elif fault == 'extra-row': rows.append(rows[i])
        elif fault == 'nonfinite': rows[i][3] = 'Infinity'
        elif fault == 'order': rows[1], rows[2] = rows[2], rows[1]
        elif fault == 'real-zero': next(row for row in rows[1:] if row[1] == '0')[1] = ''
        else:
            i = next(i for i,row in enumerate(rows[1:],1) if row[1] == '')
            if fault == 'diagnostic-zero': rows[i][1], rows[i][3] = '0', '0.0'
            else: rows.pop(i)
        with path.open('w', newline='') as stream: csv.writer(stream).writerows(rows)
    with pytest.raises(f.FixtureError): f.verify(root, list(f.CASES), expected)

@pytest.mark.parametrize('fault', ['changed', 'deleted', 'manifest', 'extra'])
def test_zonal_class_frequency_csv_archive_never_repairs_or_overwrites(zonal_sources, tmp_path, fault):
    root, _, _, _ = zonal_sources
    receipts = f.archive_files(root/'outputs', tmp_path, list(f.CASES), {}, 'zonal-class-frequency-csvs')
    path = tmp_path/receipts['direct.csv']['path']
    if fault == 'changed': path.chmod(0o644); path.write_bytes(path.read_bytes()+b' ')
    elif fault == 'deleted': path.unlink()
    elif fault == 'manifest': (tmp_path/'zonal-class-frequency-csvs-manifest.json').write_text('{}')
    else: (path.parent/'extra.csv').write_text('extra')
    with pytest.raises(f.FixtureError): f.archive_files(root/'outputs', tmp_path, list(f.CASES), receipts, 'zonal-class-frequency-csvs')


@pytest.mark.parametrize('fault', ['hash', 'scope', 'writable', 'source', 'script'])
def test_zonal_class_frequency_worker_stdin_frozen_oracle_poison_and_cleanup(tmp_path, fault):
    import hashlib
    root = (tmp_path/'source').resolve(); root.mkdir(); (root/'outputs').mkdir(); (root/'.addp-online-owner').write_text(f.PREFIX+'-123-1')
    f.seed(root); scratch = tmp_path/'runtime-tmp'; scratch.mkdir(); script = Path(f.__file__).read_bytes(); sha = hashlib.sha256(script).hexdigest()
    result = subprocess.run([sys.executable, '-', '--worker', 'oracle', str(root), sha], input=script.decode(),
        cwd=tmp_path, env={**os.environ, 'TMPDIR': str(scratch)}, capture_output=True, text=True, timeout=90)
    assert result.returncode == 0, result.stderr
    evidence = json.loads(result.stdout); assert len(evidence['expectations']) == 16 and evidence['full_source_pixels_verified'] is True
    path = root/f.ORACLE; anchor = evidence['oracle_sha256']
    if fault == 'hash': path.chmod(0o644); path.write_bytes(path.read_bytes()+b' ')
    elif fault == 'scope':
        path.chmod(0o644); raw = json.loads(path.read_bytes()); raw['scope']['run_id'] = 'foreign'; path.write_bytes(f.encoded(raw)); path.chmod(0o444); anchor = hashlib.sha256(path.read_bytes()).hexdigest()
    elif fault == 'writable': path.chmod(0o644)
    elif fault == 'source': (root/'values.tif').write_bytes((root/'values.tif').read_bytes()+b' ')
    else: sha = 'b'*64
    with pytest.raises(f.FixtureError): f.worker('verify', root, [anchor, sha])
    assert f.worker('cleanup-oracle', root, []) == {'oracle_cache_removed': True} and not path.exists()
    assert not any(text in Path(f.__file__).read_text() for text in ('operators.raster_compute', 'np.unique', 'np.histogram'))
