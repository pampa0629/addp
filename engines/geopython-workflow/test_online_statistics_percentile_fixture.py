"""Actual GDAL source/object proof for the independent read-only Hosted fixture."""
import copy
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys

import numpy as np
import pytest
from osgeo import gdal
from operators import OPERATORS
from operators.raster_compute import raster_load, raster_workspace, raster_statistics, raster_reclassify, raster_error_language

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
fixture = importlib.import_module('business.scripts.online-raster-statistics-percentile-fixture')
runner = importlib.import_module('scripts.test.raster-statistics-percentile-workflow-online')


@pytest.fixture(scope='module')
def statistics_sources(tmp_path_factory):
    root = tmp_path_factory.mktemp('whole-statistics')
    (root/'outputs').mkdir(); (root/'.addp-online-owner').write_text('statistics-percentile-123-1')
    original = fixture.seed(root)
    yield root, original, fixture.oracle()


def plan(path):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(path)}}}


def test_statistics_percentile_sixteen_complete_objects_real_gdal_and_immutable_sources(statistics_sources):
    root, original, expectations = statistics_sources
    assert len(fixture.CASES) == 16 and len(fixture.FAILURES) == 12
    assert fixture.verify(root)['sources'] == original['sources']
    actuals = {}
    for name, case in fixture.CASES.items():
        with raster_workspace():
            source = raster_load(plan(root/case['source'])); workspace = source.workspace
            before = set(workspace.iterdir()); band = case['band']
            if case.get('stage') == 'reclassify': source = raster_reclassify(source, fixture.class_rules(), band=band); band = 1
            kwargs = {'percentile': case['percentile']} if 'percentile' in case else {}
            actual = raster_statistics(source, band=band, **kwargs)
            runner.compare(actual, expectations[name]); actuals[name] = actual
            if not case.get('stage'): assert set(workspace.iterdir()) == before
        assert not workspace.exists()
    assert actuals['p33']['percentile_value'] == actuals['p33-reversed']['percentile_value']
    assert actuals['int64']['percentile_value'] == actuals['uint64']['percentile_value'] == 12.
    assert actuals['int64']['valid_count'] == actuals['uint64']['valid_count'] == 3
    assert actuals['coverage']['valid_count'] == 2 and actuals['coverage']['percentile_value'] == -3.
    assert actuals['single']['percentile_value'] == 0. and actuals['invalid']['percentile_value'] is None
    assert actuals['p50-even']['percentile_value'] == 2. and actuals['p50-odd']['percentile_value'] == 0.
    assert fixture.verify(root)['sources'] == original['sources'] and not list((root/'outputs').iterdir())


@pytest.mark.parametrize('problem', fixture.FAILURES)
@pytest.mark.parametrize('language', ['zh-cn', 'en'])
def test_statistics_percentile_twenty_four_exact_submitted_language_failures(statistics_sources, problem, language):
    root, original, _ = statistics_sources
    dag = runner.definition({name: str(root/name) for name in fixture.SOURCES}, 'p25', invalid=problem)
    params = copy.deepcopy(dag['tasks'][-1]['params']); params.pop('input_raster')
    source_name = 'complex.tif' if problem == 'complex' else 'values.tif'
    with raster_workspace(), raster_error_language(language):
        source = raster_load(plan(root/source_name)); files = set(source.workspace.iterdir())
        with pytest.raises(ValueError) as caught: raster_statistics(source, **params)
        assert str(caught.value) == runner.MESSAGES[problem][language == 'en']
        assert set(source.workspace.iterdir()) == files
    assert {name: fixture.digest(root/name) for name in fixture.SOURCES} == original['sources']


@pytest.mark.parametrize('fault', ['last-pixel', 'mask', 'grid', 'scale', 'sidecar', 'output', 'archive'])
def test_statistics_percentile_full_source_and_archive_tampering_is_rejected(statistics_sources, tmp_path, fault):
    import shutil
    source, _, _ = statistics_sources; root = tmp_path/'copy'; shutil.copytree(source, root)
    if fault == 'output': (root/'outputs'/'unexpected.tif').write_bytes(b'x')
    elif fault == 'sidecar': (root/'values.tif.aux.xml').write_text('unexpected')
    elif fault == 'archive':
        facts = fixture.verify(root)['sources']; artifact = tmp_path/'artifact'; artifact.mkdir()
        fixture.archive_sources(root, artifact, facts)
        path = artifact/'statistics-sources'/'values.tif'; path.chmod(0o644)
        raw = path.read_bytes(); path.write_bytes(raw[:-1]+bytes([raw[-1]^1]))
        with pytest.raises(fixture.FixtureError): fixture.archive_sources(root, artifact, facts)
        return
    else:
        dataset = gdal.Open(str(root/'values.tif'), gdal.GA_Update)
        if fault == 'last-pixel': dataset.GetRasterBand(2).WriteArray(np.array([[123.]]), fixture.WIDTH-1, fixture.HEIGHT-1)
        elif fault == 'mask': dataset.GetRasterBand(1).GetMaskBand().WriteArray(np.zeros((1,1),np.uint8), 8, 8)
        elif fault == 'scale': dataset.GetRasterBand(2).SetScale(1.)
        else: dataset.SetGeoTransform((0, 1, 0, 0, 0, -1))
        dataset = None
    with pytest.raises(fixture.FixtureError): fixture.verify(root)


@pytest.mark.parametrize('fault', ['percentile','count-type','null','zero','extra','nan','mean','p50-ulp'])
def test_statistics_percentile_object_oracle_rejects_wrong_semantics(statistics_sources, fault):
    _, _, oracle = statistics_sources
    name = 'p50-even' if fault == 'p50-ulp' else 'single' if fault == 'zero' else 'invalid' if fault == 'null' else 'p33'
    expected = oracle[name]; changed = copy.deepcopy(expected)
    if fault == 'percentile': changed['percentile_value'] += .125
    elif fault == 'count-type': changed['valid_count'] = float(changed['valid_count'])
    elif fault == 'null': changed['percentile_value'] = 0.
    elif fault == 'zero': changed['percentile_value'] = None
    elif fault == 'extra': changed['private_path'] = '/tmp/forbidden'
    elif fault == 'nan': changed['percentile_value'] = float('nan')
    elif fault == 'mean': changed['mean'] += 1.
    else: changed['percentile_value'] = float(np.nextafter(changed['percentile_value'], np.inf))
    with pytest.raises(runner.SuiteError): runner.compare(changed, expected)


def test_statistics_percentile_worker_is_repository_independent_and_full_arrays_are_rechecked(statistics_sources, tmp_path):
    root, original, _ = statistics_sources
    source = Path(fixture.__file__).read_text()
    scratch = tmp_path/'runtime-tmp'; scratch.mkdir()
    result = subprocess.run([sys.executable, '-', '--worker', 'oracle', str(root)], input=source,
                            cwd=tmp_path, env={**os.environ, 'TMPDIR': str(scratch)}, text=True, capture_output=True, timeout=90)
    assert result.returncode == 0, result.stderr
    native = json.loads(result.stdout)
    assert native['sources'] == original['sources'] and len(native['expectations']) == 16
    assert native['runtime_workspaces_removed'] is True
    assert not any(key in source for key in ('operators.raster_compute', 'np.percentile', 'np.quantile', 'raster_statistics('))


def test_statistics_percentile_complex_alpha_remains_separate_native_mixed_mem_evidence(monkeypatch):
    import operators.raster_compute as compute
    source = gdal.GetDriverByName('MEM').Create('', 2, 2, 0)
    source.AddBand(gdal.GDT_Float64); source.GetRasterBand(1).Fill(1)
    source.AddBand(gdal.GDT_CFloat64); source.GetRasterBand(2).SetColorInterpretation(gdal.GCI_AlphaBand)
    monkeypatch.setattr(compute, '_open', lambda value: source)
    for language, message in [('zh-cn','复数alpha'),('en','complex alpha')]:
        with raster_workspace(), raster_error_language(language), pytest.raises(ValueError, match=message):
            raster_statistics(object(), percentile=50)
    source = None
