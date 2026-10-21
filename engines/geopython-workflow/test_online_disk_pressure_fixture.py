import importlib
import json
from pathlib import Path
import shutil
from types import SimpleNamespace
import numpy as np
from osgeo import gdal
import pytest

fixture = importlib.import_module('business.scripts.online-raster-disk-pressure-fixture')
scene = importlib.import_module('scripts.test.raster-disk-pressure-online')


def native_definition(root, op, stage):
    d = scene.definition('source', 'parent', op, stage)
    for t in d['tasks']:
        p = t['params']
        if t['operator'] == 'raster_load':
            p.pop('locator')
            p['access_plan'] = {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
                'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(root/fixture.SOURCE)}}}
        elif t['operator'] == 'raster_save':
            p.pop('target_parent_locator'); name = p.pop('target_name'); mode = p.pop('write_mode')
            p['access_plan'] = {'schema_version': 'addp.workflow.access-plan/v1', 'target': {
                'kind': 'file', 'format': 'tiff', 'name': name, 'write_mode': mode,
                'access': {'method': 'mounted_path', 'path': str(root/'outputs'/name)}}}
    return d


@pytest.fixture(scope='module')
def pressure_scene(tmp_path_factory):
    from workflow_engine import execute_workflow
    root = tmp_path_factory.mktemp('disk-pressure'); (root/'outputs').mkdir()
    original = fixture.seed(root)
    for op in ('nibble', 'proximity'):
        result = execute_workflow(native_definition(root, op, 'baseline'))
        assert result['status'] == 'success', result
        artifact = json.loads(result['final_result'])
        assert artifact['size_bytes'] == (root/'outputs'/f'{op}-baseline.cog.tif').stat().st_size > 0
    return root, original


def test_pressure_fixed_grid_real_gdal_oracle_and_exact_cog_blocks(pressure_scene):
    root, original = pressure_scene
    result = fixture.verify(root, ['nibble-baseline.cog.tif', 'proximity-baseline.cog.tif'])
    assert result['sources'] == original['sources']
    assert all(v['checked_pixels'] == 1024**2 and v['blocksize'] == 4096
               and v['full_pixel_and_mask_verification'] and v['cog_valid'] and not v['has_overviews']
               for v in result['targets'].values())


@pytest.mark.parametrize('op', ['nibble', 'proximity'])
@pytest.mark.parametrize('phase', ['distance', 'encoding'])
def test_pressure_native_exact_phase_rejection_and_same_process_recovery(pressure_scene, tmp_path, monkeypatch, op, phase):
    from operators import raster_compute as compute
    from workflow_engine import execute_workflow
    root = tmp_path/'case'; shutil.copytree(pressure_scene[0], root)
    if phase == 'encoding':
        assert native_definition(root, op, phase) == native_definition(root, op, 'recovered')
    prior = fixture.verify(root, ['nibble-baseline.cog.tif', 'proximity-baseline.cog.tif'])
    real = compute.shutil.disk_usage
    monkeypatch.setattr(compute.shutil, 'disk_usage', lambda path: SimpleNamespace(free=fixture.PRESSURE_FREE[phase]))
    result = execute_workflow(native_definition(root, op, phase))
    assert result['status'] == 'failed', result
    evidence = scene.verify_failure({'status': 'failed', 'error_msg': result['error'], 'metadata': {}, 'outputs': {}},
                                    phase, {'available_bytes': fixture.PRESSURE_FREE[phase]}, 'zh-cn')
    assert evidence['phase'] == ('distance transform' if phase == 'distance' else 'raster encoding')
    assert not compute._DISK_RESERVATIONS
    assert fixture.verify(root, ['nibble-baseline.cog.tif', 'proximity-baseline.cog.tif']) == prior
    monkeypatch.setattr(compute.shutil, 'disk_usage', real)
    result = execute_workflow(native_definition(root, op, 'recovered'))
    assert result['status'] == 'success', result
    actual = fixture.verify(root, ['nibble-baseline.cog.tif', 'proximity-baseline.cog.tif', f'{op}-recovered.cog.tif'])
    assert actual['sources'] == prior['sources']
    assert not compute._DISK_RESERVATIONS


@pytest.mark.parametrize('corruption', ['pixels', 'mask', 'grid', 'unit', 'sidecar', 'missing'])
def test_pressure_oracle_rejects_corrupt_or_extra_publications(pressure_scene, tmp_path, corruption):
    root = tmp_path/'bad'; shutil.copytree(pressure_scene[0], root)
    path = root/'outputs'/'proximity-baseline.cog.tif'
    if corruption in ('pixels', 'mask', 'grid', 'unit'):
        ds = gdal.OpenEx(str(path), gdal.OF_RASTER|gdal.OF_UPDATE, open_options=['IGNORE_COG_LAYOUT_BREAK=YES'])
        if corruption == 'pixels': ds.GetRasterBand(1).WriteRaster(512, 512, 1, 1, np.array([123.], dtype='float64').tobytes())
        elif corruption == 'mask':
            with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'): ds.CreateMaskBand(gdal.GMF_PER_DATASET)
            ds.GetRasterBand(1).GetMaskBand().Fill(255)
        elif corruption == 'grid': ds.SetGeoTransform((0, 1, 0, 0, 0, -1))
        else: ds.GetRasterBand(1).SetUnitType('foot')
        ds = None
    elif corruption == 'sidecar': (root/'outputs'/'unexpected.tmp').write_bytes(b'partial')
    else: path.unlink()
    with pytest.raises((fixture.FixtureError, RuntimeError, AttributeError)):
        fixture.verify(root, ['nibble-baseline.cog.tif', 'proximity-baseline.cog.tif'])
