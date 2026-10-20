"""Independent OIF oracles and real GDAL / workflow contract regressions."""

import hashlib
import itertools
import json
import math
import time

import numpy as np
import pytest
from osgeo import gdal, osr

from operators import list_operators
from operators.raster_band_recommendation import raster_recommend_band_combination
from operators.raster_compute import raster_load, raster_workspace


def _source(path):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(path)}}}


def _raster(path, values, *, alpha=None, mask=None, located=True, datatype=gdal.GDT_Float64):
    values = np.asarray(values)
    if values.ndim == 2:
        values = values[None]
    count, height, width = values.shape
    ds = gdal.GetDriverByName('GTiff').Create(str(path), width, height, count + (alpha is not None), datatype)
    if located:
        ds.SetGeoTransform((110, 2, .5, 30, .25, -3))
        crs = osr.SpatialReference()
        crs.ImportFromEPSG(3857)
        ds.SetProjection(crs.ExportToWkt())
    for index, array in enumerate(values, 1):
        band = ds.GetRasterBand(index)
        band.WriteArray(array)
        band.SetNoDataValue(-9999)
        band.SetScale(17 + index)
        band.SetOffset(500 + index)
    if alpha is not None:
        band = ds.GetRasterBand(count + 1)
        band.WriteArray(alpha)
        band.SetColorInterpretation(gdal.GCI_AlphaBand)
    if mask is not None:
        with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
            ds.CreateMaskBand(gdal.GMF_PER_DATASET)
        ds.GetRasterBand(1).GetMaskBand().WriteArray(np.asarray(mask, dtype=np.uint8) * 255)
    band = ds = None
    return path


def _recommend(path, bands):
    original = hashlib.sha256(path.read_bytes()).hexdigest()
    with raster_workspace():
        source = raster_load(_source(path))
        workspace = source.workspace
        result = raster_recommend_band_combination(source, bands)
    assert not workspace.exists()
    assert hashlib.sha256(path.read_bytes()).hexdigest() == original
    json.dumps(result, allow_nan=False)
    return result


def _oracle(values):
    # NumPy's full sample statistics are independent of the product's scaled,
    # three-pass block accumulation and score exponent arithmetic.
    # Boolean-selected bands may be column-contiguous; make each sample row
    # contiguous so NumPy uses its pairwise summation along the pixel axis.
    values = np.ascontiguousarray(values)
    stddev = values.std(axis=1, ddof=0)
    correlations = np.corrcoef(values)
    rows = []
    for indices in itertools.combinations(range(len(values)), 3):
        if any(stddev[index] == 0 for index in indices):
            continue
        denominator = sum(abs(correlations[i, j]) for i, j in itertools.combinations(indices, 2))
        score = None if denominator == 0 else sum(stddev[index] for index in indices) / denominator
        rows.append((tuple(index + 1 for index in indices), score))
    return stddev, correlations, sorted(rows, key=lambda row: (row[1] is not None, -(row[1] or 0), row[0]))


@pytest.mark.parametrize('located', [False, True])
def test_band_recommendation_numpy_oracle_and_candidate_order(tmp_path, located):
    rng = np.random.default_rng(17)
    values = rng.integers(-30, 70, size=(5, 9, 13)).astype(float)
    path = _raster(tmp_path / 'bands.tif', values, located=located)
    result = _recommend(path, [5, 3, 1, 4, 2])
    assert result == _recommend(path, [1, 2, 3, 4, 5])
    stddev, correlation, ranked = _oracle(values.reshape(5, -1))
    assert result['candidate_bands'] == [1, 2, 3, 4, 5]
    assert result['method'] == 'oif' and result['strategy'] == 'full' and result['value_domain'] == 'stored'
    assert (result['common_valid_count'], result['common_invalid_count']) == (117, 0)
    np.testing.assert_allclose([row['stddev'] for row in result['band_statistics']], stddev, rtol=1e-14)
    np.testing.assert_allclose([row['mean'] for row in result['band_statistics']], values.mean(axis=(1, 2)), rtol=1e-14)
    np.testing.assert_allclose(result['correlation_matrix'], correlation, atol=1e-14)
    assert [tuple(row['bands']) for row in result['rankings']] == [row[0] for row in ranked]
    np.testing.assert_allclose([row['score'] for row in result['rankings']], [row[1] for row in ranked], rtol=1e-13)
    assert result['recommended_bands'] == result['rankings'][0]['bands']
    assert result['excluded_combinations'] == []


def test_band_recommendation_joint_domain_cross_blocks_and_validity(tmp_path, monkeypatch):
    import operators.raster_band_recommendation as module
    y, x = np.indices((515, 519))
    values = np.stack([x + y, 2 * x - y, (x % 7) * (y % 13), x * 0 + 4., x * 0 + np.nan]).astype(float)
    values[0, 0, 1] = -9999
    values[1, 512, 511] = np.nan
    values[2, 511, 513] = np.inf
    values[3, 513, 512] = -9999  # The excluded constant still restricts the common domain.
    alpha = np.full(x.shape, 128.)
    alpha[0, 2] = 0
    alpha[1, 0] = -1
    alpha[1, 1] = np.nan
    alpha[1, 2] = np.inf
    mask = np.ones(x.shape, bool)
    mask[510, 511] = False
    valid = ((values[:4] != -9999) & np.isfinite(values[:4])).all(axis=0) & np.isfinite(alpha) & (alpha > 0) & mask
    assert values[0, 0, 0] == 0 and valid[0, 0]
    path = _raster(tmp_path / 'cross-block.tif', values, alpha=alpha, mask=mask)
    blocks = []
    original = module._blocks
    def observed(dataset, indices, **kwargs):
        assert indices == [1, 2, 3, 4]  # Band 5's all-invalid values cannot affect the common domain.
        for a, b, arrays, masks in original(dataset, indices, **kwargs):
            assert all(array.shape[0] <= 512 and array.shape[1] <= 512 for array in arrays)
            blocks.append((a, b))
            yield a, b, arrays, masks
    monkeypatch.setattr(module, '_blocks', observed)
    result = _recommend(path, [4, 2, 3, 1])
    assert (result['common_valid_count'], result['common_invalid_count']) == (int(valid.sum()), int((~valid).sum()))
    selected = values[:3, valid]
    stddev, correlation, _ = _oracle(selected)
    np.testing.assert_allclose([row['stddev'] for row in result['band_statistics'][:3]], stddev, rtol=1e-13)
    np.testing.assert_allclose(np.asarray(result['correlation_matrix'], object)[:3, :3].astype(float), correlation, atol=1e-13)
    assert result['band_statistics'][3] == {'band': 4, 'mean': 4., 'stddev': 0., 'score_status': 'zero_variance'}
    assert result['correlation_matrix'][3] == [None] * 4
    assert [row['bands'] for row in result['excluded_combinations']] == [[1, 2, 4], [1, 3, 4], [2, 3, 4]]
    assert result['recommended_bands'] == [1, 2, 3]
    assert max(a for a, b in blocks) > 0 and max(b for a, b in blocks) > 0


def test_band_recommendation_unbounded_and_exact_ties(tmp_path):
    # Orthogonal Walsh rows give exact zero cross-products using binary values.
    values = np.array([[1, 1, -1, -1], [1, -1, 1, -1], [1, -1, -1, 1], [1, 1, -1, -1]], float).reshape(4, 2, 2)
    result = _recommend(_raster(tmp_path / 'orthogonal.tif', values), [4, 3, 2, 1])
    assert [row['bands'] for row in result['rankings']] == [[1, 2, 3], [2, 3, 4], [1, 2, 4], [1, 3, 4]]
    assert [row['score_status'] for row in result['rankings']] == ['unbounded', 'unbounded', 'finite', 'finite']
    assert [row['score'] for row in result['rankings']] == [None, None, 3., 3.]
    assert [row['rank'] for row in result['rankings']] == [1, 2, 3, 4]


def test_band_recommendation_complete_positive_and_negative_correlation(tmp_path):
    values = np.array([[-2, 0, 2, 4], [4, 0, -4, -8], [-6, 0, 6, 12]], float).reshape(3, 2, 2)
    result = _recommend(_raster(tmp_path / 'correlated.tif', values), [1, 2, 3])
    np.testing.assert_allclose(result['correlation_matrix'], [[1, -1, 1], [-1, 1, -1], [1, -1, 1]], atol=2e-15)
    assert result['rankings'][0]['score'] == pytest.approx(2 * math.sqrt(5), rel=1e-14)


def test_band_recommendation_large_base_small_differences(tmp_path):
    offsets = np.array([[0, 1, 2, 3, 4, 5, 6, 7], [2, 1, 4, 3, 6, 5, 8, 7], [0, 3, 1, 6, 2, 7, 4, 5]], float)
    values = (2.0 ** 48 + offsets).reshape(3, 2, 4)
    result = _recommend(_raster(tmp_path / 'large-base.tif', values), [1, 2, 3])
    stddev, correlation, _ = _oracle(offsets)
    np.testing.assert_allclose([row['stddev'] for row in result['band_statistics']], stddev, rtol=1e-14)
    np.testing.assert_allclose(result['correlation_matrix'], correlation, atol=2e-15)


@pytest.mark.parametrize('magnitude', [np.finfo(float).max, 1e-300, np.nextafter(0., 1.)])
def test_band_recommendation_extreme_finite_values_avoid_intermediate_overflow(tmp_path, magnitude):
    values = np.tile(np.array([magnitude, -magnitude, magnitude, -magnitude]), (3, 1)).reshape(3, 2, 2)
    result = _recommend(_raster(tmp_path / 'extreme.tif', values), [1, 2, 3])
    assert all(row['mean'] == 0 and row['stddev'] == magnitude for row in result['band_statistics'])
    assert result['rankings'][0]['score'] == magnitude


def test_band_recommendation_unrepresentable_finite_score_fails(tmp_path):
    magnitude = np.finfo(float).max
    # Each standard deviation is finite, but tiny nonzero correlations make the score exceed Float64.
    values = magnitude * np.array([[1, 1, -1, -1], [1, -1, 1, -1], [1, -.9, -1, 1]], float)
    path = _raster(tmp_path / 'score-overflow.tif', values.reshape(3, 2, 2))
    with raster_workspace(), pytest.raises(ValueError, match='score.*representable'):
        raster_recommend_band_combination(raster_load(_source(path)), [1, 2, 3])


@pytest.mark.parametrize('bands', [None, '1,2,3', (), [], [1, 2], list(range(1, 18)), [1, 1, 2], [True, 2, 3], [1., 2, 3], [0, 1, 2], [-1, 1, 2], [1, 2, 99]])
def test_band_recommendation_invalid_candidates(tmp_path, bands):
    path = _raster(tmp_path / 'source.tif', np.arange(12.).reshape(3, 2, 2))
    with raster_workspace(), pytest.raises(ValueError):
        raster_recommend_band_combination(raster_load(_source(path)), bands)


@pytest.mark.parametrize('kind', ['no-valid', 'one-valid', 'all-constant', 'two-variable', 'alpha', 'complex-data', 'complex-alpha'])
def test_band_recommendation_invalid_domains_and_data(tmp_path, kind):
    values = np.arange(12.).reshape(3, 2, 2)
    if kind == 'no-valid':
        values[0] = -9999
    elif kind == 'one-valid':
        values[0] = [[0, -9999], [-9999, -9999]]
    elif kind == 'all-constant':
        values[:] = 7
    elif kind == 'two-variable':
        values[2] = 7
    alpha = np.ones((2, 2)) if kind in ('alpha', 'complex-alpha') else None
    datatype = gdal.GDT_CFloat64 if kind.startswith('complex-') else gdal.GDT_Float64
    path = _raster(tmp_path / 'invalid.tif', values, alpha=alpha, datatype=datatype)
    bands = [1, 2, 4] if kind == 'alpha' else [1, 2, 3]
    with raster_workspace(), pytest.raises(ValueError):
        raster_recommend_band_combination(raster_load(_source(path)), bands)


def test_band_recommendation_sixteen_candidates_full_combinations(tmp_path):
    values = np.arange(8., dtype=float)
    path = _raster(tmp_path / 'sixteen.tif', np.stack([(index + 1) * values for index in range(16)]).reshape(16, 2, 4))
    result = _recommend(path, list(range(16, 0, -1)))
    assert len(result['rankings']) == 560 and result['excluded_combinations'] == []
    assert result['recommended_bands'] == [14, 15, 16]


@pytest.mark.parametrize('invalid', [False, True])
def test_band_recommendation_real_async_api_serialization_and_cleanup(tmp_path, monkeypatch, invalid):
    import api_server
    import workflow_engine
    values = np.array([[1, 1, -1, -1], [1, -1, 1, -1], [1, -1, -1, 1]], float).reshape(3, 2, 2)
    path = _raster(tmp_path / 'source.tif', values)
    snapshot = path.read_bytes()
    workspaces = []
    execute = workflow_engine._execute_operator
    def observed(name, params):
        if name == 'raster_recommend_band_combination':
            workspaces.append(params['input_raster'].workspace)
        return execute(name, params)
    monkeypatch.setattr(workflow_engine, '_execute_operator', observed)
    tasks = [{'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'access_plan': _source(path)}},
             {'id': 'recommend', 'operator': 'raster_recommend_band_combination', 'depends_on': ['source'],
              'params': {'input_raster': {'$ref': 'source'}, 'bands': [1, 2, 99] if invalid else [3, 2, 1]}}]
    client = api_server.app.test_client()
    response = client.post('/api/workflow', json={'workflow_def': {'tasks': tasks}, 'input_data': {}, 'runtime': {
        'tenant_id': 7, 'execution_authorization': {'id': 1, 'effects': ['read']}}})
    assert response.status_code == 202, response.json
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        status = client.get('/api/executions/' + response.json['execution_id']).json
        if status['status'] in ('success', 'failed'):
            break
        time.sleep(.01)
    assert status['status'] == ('failed' if invalid else 'success'), status
    assert workspaces and all(not workspace.exists() for workspace in workspaces)
    assert path.read_bytes() == snapshot
    if not invalid:
        result = json.loads(status['result'])
        assert result['recommended_bands'] == [1, 2, 3]
        assert result['rankings'][0]['score_status'] == 'unbounded' and result['rankings'][0]['score'] is None
        json.dumps(result, allow_nan=False)
    assert client.post('/api/operators/raster_recommend_band_combination/invoke', json={'params': {}}).status_code == 403


def test_band_recommendation_metadata_and_executable_example(tmp_path):
    spec = next(item for item in list_operators() if item['id'] == 'raster_recommend_band_combination')
    assert spec['category_path'] == ['栅格处理', '栅格统计']
    assert spec['effects'] == ['read'] and spec['execution_modes'] == ['workflow']
    assert spec['attributes']['resource_groups'] == ['raster']
    assert spec['output_ports'][0]['type'] == 'object'
    params = {param['name']: param for param in spec['parameters']}
    assert params['bands']['required'] and params['bands']['type'] == 'array' and params['bands']['item_type'] == 'integer'
    assert params['input_raster']['param_type'] == 'input'
    from workflow_engine import execute_workflow
    path = _raster(tmp_path / 'example.tif', np.arange(18.).reshape(3, 2, 3))
    result = execute_workflow({'tasks': [
        {'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'access_plan': _source(path)}},
        {'id': 'recommend', **spec['detailed_description']['workflow_example'], 'depends_on': ['source']}]})
    assert result['status'] == 'success', result
    assert json.loads(result['final_result'])['recommended_bands'] == [1, 2, 3]
