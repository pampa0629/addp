"""Independent lattice oracle and real persisted capacity DAG at bounded test size."""
import importlib
import json
from pathlib import Path
import shutil
import numpy as np
from osgeo import gdal
import pytest

fixture = importlib.import_module('business.scripts.online-raster-nibble-capacity-fixture')
scene = importlib.import_module('scripts.test.raster-nibble-capacity-online')


@pytest.mark.parametrize('size,spacing', [(9, 4), (13, 5), (17, 2), (1, 4)])
def test_capacity_analytical_oracle_matches_original_donor_bruteforce(size, spacing):
    values, _, valid = fixture.block_facts(0, 0, size, size, spacing)
    expected, actual_valid = fixture.expected_block(0, 0, size, size, size, spacing)
    rows, columns = np.indices((size, size)); best = np.full((size, size), 10**9, dtype=np.int64)
    oracle = values.astype(np.float64)
    for row, column in np.argwhere(valid & (values != 7)):
        squared = (rows-row)**2+(columns-column)**2
        use = valid & (values == 7) & (squared < best)
        oracle[use] = values[row, column]; best[use] = squared[use]
    oracle[~valid] = np.nan
    np.testing.assert_array_equal(expected, oracle)
    np.testing.assert_array_equal(actual_valid, valid)
    for y in range(0, size, 3):
        for x in range(0, size, 3):
            block, _ = fixture.expected_block(x, y, min(3, size-x), min(3, size-y), size, spacing)
            np.testing.assert_array_equal(block, oracle[y:y+3, x:x+3])


@pytest.fixture
def capacity_output(tmp_path, monkeypatch):
    from workflow_engine import execute_workflow
    monkeypatch.setattr(fixture, 'SIZES', (515,))
    monkeypatch.setattr(fixture, 'SPACING', {'sparse': 128, 'dense': 2})
    monkeypatch.setattr(fixture, 'SOURCES', ('classes-515-sparse.tif', 'classes-515-dense.tif'))
    (tmp_path/'outputs').mkdir(); (tmp_path/'runtime-tmp').mkdir()
    initial = fixture.seed(tmp_path)
    names = []
    for density in fixture.SPACING:
        name = fixture.target_names(515, density, 1)[0]; names.append(name)
        source = tmp_path/f'classes-515-{density}.tif'; target = tmp_path/'outputs'/name
        dag = scene.definition(str(source), 'parent', name)
        for task in dag['tasks']:
            params = task['params']
            if task['operator'] == 'raster_load':
                params.pop('locator'); params['access_plan'] = {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
                    'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(source)}}}
            if task['operator'] == 'raster_save':
                for key in ('target_parent_locator', 'target_name', 'write_mode'): params.pop(key)
                params['access_plan'] = {'schema_version': 'addp.workflow.access-plan/v1', 'target': {
                    'kind': 'file', 'format': 'tiff', 'name': name, 'write_mode': 'create',
                    'access': {'method': 'mounted_path', 'path': str(target)}}}
        result = execute_workflow(dag)
        assert result['status'] == 'success', result
        assert json.loads(result['final_result'])['size_bytes'] == target.stat().st_size > 0
    return tmp_path, names, initial


def test_nibble_capacity_real_gdal_complete_pixels_and_previous_immutable(capacity_output):
    root, names, initial = capacity_output
    actual = fixture.verify(root, names, {})
    assert actual['sources'] == initial['sources']
    assert all(f['checked_pixels'] == 515**2 and f['cog_valid'] for f in actual['targets'].values())
    assert fixture.verify(root, names, actual['targets']) == actual


@pytest.mark.parametrize('corruption', ['pixel', 'mask', 'grid', 'nodata', 'sidecar', 'previous'])
def test_nibble_capacity_oracle_rejects_corrupt_or_previous_output(capacity_output, corruption):
    root, names, _ = capacity_output
    previous = fixture.verify(root, names, {})['targets'] if corruption == 'previous' else {}
    target = root/'outputs'/names[0]
    if corruption == 'sidecar':
        (root/'outputs'/'extra.aux.xml').write_text('bad')
    else:
        dataset = gdal.OpenEx(str(target), gdal.OF_RASTER | gdal.OF_UPDATE, open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
        band = dataset.GetRasterBand(1)
        if corruption in {'pixel', 'previous'}:
            band.WriteArray(np.array([[12345.]]), 0, 0)
        elif corruption == 'mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'): dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
            band.GetMaskBand().Fill(255)
        elif corruption == 'grid': dataset.SetGeoTransform((1, 1, 0, 1, 0, -1))
        else: band.SetNoDataValue(-9999)
        band = dataset = None
    with pytest.raises(fixture.FixtureError): fixture.verify(root, names, previous)
