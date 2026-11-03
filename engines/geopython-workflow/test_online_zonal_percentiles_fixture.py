"""Actual GDAL/CSV oracle and accurate rejection tests; no Hosted claims."""
import copy
import hashlib
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

f = importlib.import_module('business.scripts.online-raster-zonal-percentiles-fixture')
m = importlib.import_module('scripts.test.raster-zonal-percentiles-persistence-online')

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
    root = tmp_path_factory.mktemp('zonal-percentiles'); (root/'outputs').mkdir(); (root/'.addp-online-owner').write_text('zonal-percentiles-123-1')
    initial = f.seed(root); expected = f.oracle(); (root/f.ORACLE).write_bytes(f.encoded(expected))
    previews = {}
    for name in f.CASES:
        dag = m.definition({n:n for n in f.SOURCES}, 'parent', name); localize(dag, root, root/'outputs'/name)
        result = execute_workflow(dag); assert result['status'] == 'success', result
        previews[name] = json.loads(result['final_result'])
    return root, initial, expected, previews


def test_zonal_percentiles_sixteen_real_csvs_complete_independent_oracle_and_raw_archive(zonal_sources, tmp_path):
    root, initial, expected, previews = zonal_sources
    report = f.verify(root, list(f.CASES), expected)
    assert report['sources'] == initial['sources'] and report['full_csv_checks'] == 16
    archive = f.archive_files(root/'outputs', tmp_path, list(f.CASES), {}, 'zonal-percentiles-csvs')
    assert len(archive) == 16
    for name in f.CASES:
        m.validate_preview(previews[name], expected[name], name)
        assert (tmp_path/archive[name]['path']).read_bytes() == (root/'outputs'/name).read_bytes()
        assert archive[name]['size_bytes'] > 0
    assert len(expected['many-zones.csv']) == 79 and previews['many-zones.csv']['truncated'] is True
    assert expected['empty-zones.csv'] == [] and all(r['percentile_value_1'] is None for r in expected['invalid-values.csv'])
    assert [r['zone_value'] for r in expected['int64.csv']] == [0, 2**63-2, 2**63-1]
    assert expected['reversed.csv'] == expected['batch.csv']
    assert expected['p50.csv'][1]['valid_count'] == 4 and expected['p50.csv'][1]['percentile_value_1'] == 2.5
    assert expected['p50.csv'][0]['valid_count'] == 3 and expected['p50.csv'][0]['percentile_value_1'] == 0.
    assert expected['p50.csv'][2]['percentile_value_1'] == 0.
    assert report['actual_single_controls_verified'] == list(f.CONTROLS)
    for name in ('batch.csv',*f.CONTROLS):
        assert [{k:r[k]for k in f.BASE_COLUMNS}for r in expected[name]] == expected['original.csv']

@pytest.mark.parametrize('problem', f.FAILURES)
@pytest.mark.parametrize('language', ['zh-cn','en'])
def test_zonal_percentiles_eighty_exact_submitted_failures_and_no_output(zonal_sources, tmp_path, monkeypatch, problem, language):
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

@pytest.mark.parametrize('fault', ['percentile', 'one-ulp', 'class', 'count', 'null', 'column-order', 'extra-row', 'nonfinite', 'source-mask', 'sidecar'])
def test_zonal_percentiles_real_physical_corruption_is_rejected(zonal_sources, tmp_path, fault):
    from osgeo import gdal
    import numpy as np
    original, _, expected, _ = zonal_sources; root = tmp_path/'changed'; shutil.copytree(original, root)
    path = root/'outputs'/'batch.csv'
    if fault == 'source-mask':
        ds = gdal.Open(str(root/'base-values.tif'), gdal.GA_Update); ds.GetRasterBand(1).GetMaskBand().WriteArray(np.zeros((1,1), np.uint8), 0, 0); ds = None
    elif fault == 'sidecar': (root/'outputs'/'unrequested.csv').write_text('unexpected')
    else:
        with path.open(newline='') as stream: rows = list(csv.reader(stream))
        row_index = next(i for i, row in enumerate(rows[1:], 1) if row[-1] != '' and float(row[-1]) != 0.)
        if fault == 'percentile': rows[row_index][-1] = str(float(rows[row_index][-1])+1)
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
def test_zonal_percentiles_csv_archive_never_repairs_or_overwrites(zonal_sources, tmp_path, fault):
    root, _, _, _ = zonal_sources
    receipts = f.archive_files(root/'outputs', tmp_path, list(f.CASES), {}, 'zonal-percentiles-csvs')
    path = tmp_path/receipts['p25.csv']['path']
    if fault == 'changed': path.chmod(0o644); path.write_bytes(path.read_bytes()+b' ')
    elif fault == 'deleted': path.unlink()
    elif fault == 'manifest': (tmp_path/'zonal-percentiles-csvs-manifest.json').write_text('{}')
    else: (path.parent/'extra.csv').write_text('extra')
    with pytest.raises(f.FixtureError): f.archive_files(root/'outputs', tmp_path, list(f.CASES), receipts, 'zonal-percentiles-csvs')


def test_zonal_percentiles_worker_stdin_frozen_oracle_poison_and_cleanup(tmp_path):
    root = tmp_path/'source'; root.mkdir(); (root/'outputs').mkdir(); (root/'.addp-online-owner').write_text('zonal-percentiles-123-1')
    f.seed(root); scratch = tmp_path/'runtime-tmp'; scratch.mkdir()
    result = subprocess.run([sys.executable, '-', '--worker', 'oracle', str(root), hashlib.sha256(Path(f.__file__).read_bytes()).hexdigest()], input=Path(f.__file__).read_text(),
        cwd=tmp_path, env={**os.environ, 'TMPDIR': str(scratch)}, capture_output=True, text=True, timeout=90)
    assert result.returncode == 0, result.stderr
    evidence = json.loads(result.stdout); assert len(evidence['expectations']) == 16 and evidence['full_source_pixels_verified'] is True
    path = root/f.ORACLE; path.chmod(0o644); path.write_bytes(path.read_bytes()+b' ')
    with pytest.raises(f.FixtureError, match='oracle cache hash changed'): f.worker('verify', root, [evidence['oracle_sha256'], hashlib.sha256(Path(f.__file__).read_bytes()).hexdigest()])
    assert f.worker('cleanup-oracle', root, []) == {'oracle_cache_removed': True} and not path.exists()
    assert not any(text in Path(f.__file__).read_text() for text in ('operators.raster_compute', 'np.percentile', 'np.quantile'))

@pytest.mark.parametrize('fault',['writable','script','run','source','symlink'])
def test_zonal_percentiles_oracle_binds_exact_source_script_and_run(tmp_path, fault, monkeypatch):
    root=tmp_path/'source';root.mkdir();(root/'outputs').mkdir();(root/'.addp-online-owner').write_text('zonal-percentiles-123-1');f.seed(root)
    scratch=tmp_path/'scratch';scratch.mkdir();monkeypatch.setattr(f.tempfile,'gettempdir',lambda:str(scratch))
    sha=hashlib.sha256(Path(f.__file__).read_bytes()).hexdigest();e=f.worker('oracle',root,[sha]);path=root/f.ORACLE
    if fault=='writable':path.chmod(0o644)
    elif fault=='script':sha='b'*64
    elif fault=='run':(root/'.addp-online-owner').write_text('zonal-percentiles-123-2')
    elif fault=='source':
        source=root/'base-values.tif';source.write_bytes(source.read_bytes()+b' ')
    else:
        destination=root/'redirect';path.rename(destination);path.symlink_to(destination)
    with pytest.raises(f.FixtureError):f.worker('verify',root,[e['oracle_sha256'],sha])

def test_zonal_percentiles_oracle_is_independent_of_product_and_percentile_helpers(monkeypatch):
    import operators.raster_compute as c
    import numpy as np
    def forbidden(*a,**kw):raise AssertionError('product cannot define expected output')
    monkeypatch.setattr(c,'raster_zonal_statistics',forbidden)
    monkeypatch.setattr(np,'percentile',forbidden);monkeypatch.setattr(np,'quantile',forbidden)
    assert set(f.oracle())==set(f.CASES)
