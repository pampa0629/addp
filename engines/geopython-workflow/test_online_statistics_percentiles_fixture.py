"""Actual GDAL, exact analysis failures and standalone source-bound Worker proof."""
import copy
import hashlib
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys

import numpy as np
import pytest
from osgeo import gdal
from operators.raster_compute import raster_load, raster_workspace, raster_statistics, raster_reclassify, raster_error_language
from workflow_engine import GeoPythonWorkflowRunner
from addp_common.workflow_runtime import WorkflowExecutionError

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
f = importlib.import_module('business.scripts.online-raster-statistics-percentiles-fixture')
m = importlib.import_module('scripts.test.raster-statistics-percentiles-workflow-online')
original = importlib.import_module('business.scripts.online-raster-statistics-percentile-fixture')


def plan(path):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(path)}}}


@pytest.fixture(scope='module')
def multi_sources(tmp_path_factory):
    root = tmp_path_factory.mktemp('multi-statistics'); (root/'outputs').mkdir()
    (root/'.addp-online-owner').write_text('statistics-percentiles-123-1')
    baseline = f.seed(root)
    return root, baseline, f.oracle()


def test_statistics_percentiles_sixteen_complete_objects_and_original_suite_unchanged(multi_sources):
    root, baseline, oracle = multi_sources
    assert len(f.CASES) == 16 and len(f.FAILURES) == 20 and len(f.SOURCES) == 11
    assert len(original.FAILURES) == 12 and original.CASES['p25']['percentiles'] == [25]
    actuals = {}
    for name, case in f.CASES.items():
        with raster_workspace():
            source = raster_load(plan(root/case['source'])); workspace = source.workspace; band = case['band']
            if case.get('stage'): source = raster_reclassify(source, f.class_rules(case), band=band); band = 1
            kwargs = {'percentiles': case['percentiles']} if 'percentiles' in case else {}
            actual = raster_statistics(source, band=band, **kwargs)
            m.compare(actual, oracle[name]); actuals[name] = actual
            for item in actual.get('percentiles', []):
                assert item == raster_statistics(source, band=band, percentiles=[item['percentile']])['percentiles'][0]
        assert not workspace.exists()
    assert actuals['fractional']['percentiles'] == actuals['reversed']['percentiles']
    assert actuals['p50-control']['percentiles'][0] == actuals['quartiles']['percentiles'][2]
    assert [v['value'] for v in actuals['odd']['percentiles']][1:3] == [0., 0.]
    assert [v['value'] for v in actuals['constant']['percentiles']] == [0.]*5
    assert all(v['value'] is None for v in actuals['invalid']['percentiles'])
    assert f.verify(root)['sources'] == baseline['sources'] and not list((root/'outputs').iterdir())


@pytest.mark.parametrize('problem', f.FAILURES)
@pytest.mark.parametrize('language', ['zh-cn', 'en'])
def test_statistics_percentiles_forty_exact_analysis_failures_preserve_sources(multi_sources, problem, language):
    root, baseline, _ = multi_sources
    dag = m.definition({n:n for n in f.SOURCES}, 'quartiles', invalid=problem)
    task = dag['tasks'][0]; name = task['params'].pop('locator'); task['params']['access_plan'] = plan(root/name)
    with pytest.raises(WorkflowExecutionError) as caught: GeoPythonWorkflowRunner(language=language).execute(dag)
    assert caught.value.error_code == 'EXECUTION_FAILED'
    assert str(caught.value) == '任务 analysis 执行失败: '+m.MESSAGES[problem][language == 'en']
    assert {name:f.digest(root/name) for name in f.SOURCES} == baseline['sources']
    assert not list((root/'outputs').iterdir())


@pytest.mark.parametrize('fault', ['missing-item', 'reordered', 'duplicate-p', 'last-value', 'bool-p', 'bool-value', 'extra', 'null', 'nan'])
def test_statistics_percentiles_full_ordered_oracle_rejects_every_item_tampering(multi_sources, fault):
    _, _, oracle = multi_sources; expected = oracle['quartiles']; value = copy.deepcopy(expected)
    if fault == 'missing-item': value['percentiles'].pop()
    elif fault == 'reordered': value['percentiles'].reverse()
    elif fault == 'duplicate-p': value['percentiles'][-1]['percentile'] = 0.
    elif fault == 'last-value': value['percentiles'][-1]['value'] += 1.
    elif fault == 'bool-p': value['percentiles'][0]['percentile'] = True
    elif fault == 'bool-value': value['percentiles'][0]['value'] = False
    elif fault == 'extra': value['percentiles'][1]['private_path'] = '/tmp/forbidden'
    elif fault == 'null': value['percentiles'][2]['value'] = None
    else: value['percentiles'][2]['value'] = float('nan')
    with pytest.raises(m.SuiteError): m.compare(value, expected)


@pytest.mark.parametrize('fault', ['last-pixel', 'mask', 'grid', 'scale', 'sidecar', 'output', 'archive'])
def test_statistics_percentiles_full_source_and_archive_tampering_fails(multi_sources, tmp_path, fault):
    import shutil
    source, _, _ = multi_sources; root = tmp_path/'copy'; shutil.copytree(source, root)
    if fault == 'output': (root/'outputs'/'unexpected.tif').write_bytes(b'x')
    elif fault == 'sidecar': (root/'values.tif.aux.xml').write_text('unexpected')
    elif fault == 'archive':
        facts = f.verify(root)['sources']; artifact = tmp_path/'artifact'; artifact.mkdir(); f.archive_sources(root, artifact, facts)
        path = artifact/'statistics-sources'/'values.tif'; path.chmod(0o644); raw=path.read_bytes();path.write_bytes(raw[:-1]+bytes([raw[-1]^1]))
        with pytest.raises(f.FixtureError): f.archive_sources(root, artifact, facts)
        return
    else:
        ds = gdal.Open(str(root/'values.tif'), gdal.GA_Update)
        if fault == 'last-pixel': ds.GetRasterBand(2).WriteArray(np.array([[123.]]), 1024, 512)
        elif fault == 'mask': ds.GetRasterBand(1).GetMaskBand().WriteArray(np.zeros((1,1),np.uint8), 8, 8)
        elif fault == 'scale': ds.GetRasterBand(2).SetScale(1.)
        else: ds.SetGeoTransform((0,1,0,0,0,-1))
        ds = None
    with pytest.raises(f.FixtureError): f.verify(root)


def test_statistics_percentiles_standalone_worker_binds_both_sources_and_rechecks_all_arrays(multi_sources, tmp_path):
    root, baseline, expected = multi_sources; scratch = tmp_path/'runtime-tmp'; scratch.mkdir()
    payload = f.worker_source()
    result = subprocess.run([sys.executable, '-', '--worker', 'oracle', str(root)], input=payload,
        cwd=tmp_path, env={**os.environ, 'TMPDIR':str(scratch)}, text=True, capture_output=True, timeout=90)
    assert result.returncode == 0, result.stderr
    actual = json.loads(result.stdout)
    assert actual['sources'] == baseline['sources'] and actual['expectations'] == expected
    assert actual['fixture_sources_sha256'] == f.fixture_sources() and actual['runtime_workspaces_removed'] is True
    assert not any(token in payload for token in ('operators.raster_compute', 'np.percentile', 'np.quantile'))
    assert len(original.FAILURES) == 12 and original.CASES['p25']['percentiles'] == [25]
