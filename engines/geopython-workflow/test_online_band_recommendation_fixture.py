"""Real GDAL checks for the independent read-only Hosted OIF oracle."""
import copy
import importlib
from pathlib import Path
import sys

import numpy as np
from osgeo import gdal
import pytest

from operators.raster_band_recommendation import raster_recommend_band_combination
from operators.raster_compute import raster_load, raster_workspace

# This file also runs alone through the registered Native entry; do not rely on
# a previously collected fixture module to add the repository import root.
sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
fixture = importlib.import_module('business.scripts.online-raster-band-recommendation-fixture')
runner = importlib.import_module('scripts.test.raster-band-recommendation-workflow-online')


@pytest.fixture
def band_recommendation_fixture(tmp_path):
    (tmp_path / 'outputs').mkdir()
    (tmp_path / '.addp-online-owner').write_text('band-recommendation-123-1')
    original = fixture.seed(tmp_path)
    yield tmp_path, original


def test_band_recommendation_fixture_complete_sources_and_full_product_object(band_recommendation_fixture):
    root, original = band_recommendation_fixture
    physical = fixture.verify(root)
    assert physical['sources'] == original['sources']
    assert all(fact['full_pixels_and_mask_verified'] and fact['grid_crs_scale_verified'] for fact in physical['source_physical'].values())
    results = {}
    for name, case in fixture.CASES.items():
        with raster_workspace():
            source = raster_load({'schema_version': 'addp.workflow.access-plan/v1', 'source': {
                'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(root / case['source'])}}})
            result = raster_recommend_band_combination(source, case['bands'])
            runner.compare(result, physical['expectations'][name])
            results[name] = result
    full = results['full']
    assert full == results['full-reversed']
    assert len(full['rankings']) == 455 and len(full['excluded_combinations']) == 105
    assert all(row['score_status'] == 'finite' for row in full['rankings'])
    assert full['common_valid_count'] == fixture.WIDTH * fixture.HEIGHT - 10
    assert full['common_invalid_count'] == 10
    assert full['band_statistics'][15]['score_status'] == 'zero_variance'
    assert [row['bands'] for row in results['orthogonal']['rankings']] == [[1, 2, 3], [2, 3, 4], [1, 2, 4], [1, 3, 4]]
    assert [row['score'] for row in results['orthogonal']['rankings']] == [None, None, 3., 3.]
    assert fixture.verify(root)['sources'] == original['sources']
    assert list((root / 'outputs').iterdir()) == []


@pytest.mark.parametrize('fault', list(fixture.FAILURES))
def test_band_recommendation_fixture_all_failure_domains_fail_without_publication(band_recommendation_fixture, fault):
    root, original = band_recommendation_fixture
    case = fixture.FAILURES[fault]
    with raster_workspace(), pytest.raises(ValueError):
        source = raster_load({'schema_version': 'addp.workflow.access-plan/v1', 'source': {
            'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(root / case['source'])}}})
        raster_recommend_band_combination(source, case['bands'])
    assert {name: fixture.digest(root / name) for name in fixture.SOURCES} == original['sources']
    assert not list((root / 'outputs').iterdir())


@pytest.mark.parametrize('fault', ['last-pixel', 'mask', 'grid', 'scale', 'output', 'sidecar'])
def test_band_recommendation_fixture_rejects_source_and_readonly_corruption(band_recommendation_fixture, fault):
    root, _ = band_recommendation_fixture
    if fault == 'output':
        (root / 'outputs' / 'forbidden.tif').write_bytes(b'x')
    elif fault == 'sidecar':
        (root / 'candidates.tif.aux.xml').write_text('unexpected')
    else:
        dataset = gdal.Open(str(root / fixture.SOURCES[0]), gdal.GA_Update)
        if fault == 'last-pixel':
            dataset.GetRasterBand(15).WriteArray(np.array([[123456.]]), fixture.WIDTH - 1, fixture.HEIGHT - 1)
        elif fault == 'mask':
            dataset.GetRasterBand(1).GetMaskBand().WriteArray(np.array([[0]], np.uint8), 10, 10)
        elif fault == 'grid':
            transform = list(fixture.TRANSFORM)
            transform[0] += 1
            dataset.SetGeoTransform(transform)
        else:
            dataset.GetRasterBand(1).SetScale(1.)
        dataset = None
    with pytest.raises(fixture.FixtureError):
        fixture.verify(root)


@pytest.mark.parametrize('fault', ['last-rank', 'score', 'constant', 'correlation', 'recommendation', 'nan', 'count-type', 'extra-key'])
def test_band_recommendation_fixture_object_comparison_is_complete_and_strict(fault):
    expected = fixture.expected_result('orthogonal')
    changed = copy.deepcopy(expected)
    if fault == 'last-rank':
        changed['rankings'][-1]['rank'] = 99
    elif fault == 'score':
        changed['rankings'][-1]['score'] += 1
    elif fault == 'constant':
        changed['excluded_combinations'].append({'bands': [1, 2, 3], 'reason': 'zero_variance'})
    elif fault == 'correlation':
        changed['correlation_matrix'][0][1] = .5
    elif fault == 'recommendation':
        changed['recommended_bands'] = [1, 3, 4]
    elif fault == 'nan':
        changed['band_statistics'][0]['mean'] = float('nan')
    elif fault == 'count-type':
        changed['common_valid_count'] = True
    else:
        changed['rgb'] = [1, 2, 3]
    with pytest.raises(runner.SuiteError):
        runner.compare(changed, expected)
