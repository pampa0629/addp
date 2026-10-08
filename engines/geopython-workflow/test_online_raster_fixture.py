"""Exercise the T4 independent byte verifier with real GDAL and local S3 transport."""
import importlib.util
import hashlib
import math
import importlib
import json
from pathlib import Path
import struct
import time
from unittest.mock import patch

from osgeo import gdal, osr
import pytest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('online_raster_fixture', ROOT / 'business/scripts/online-raster-minio-fixture.py')
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)


class LocalMinio:
    objects = {}

    def __init__(self, endpoint, **kwargs): self.endpoint = endpoint
    def make_bucket(self, bucket): pass
    def fput_object(self, bucket, name, path, **kwargs): self.objects[self.endpoint, bucket, name] = Path(path).read_bytes()
    def fget_object(self, bucket, name, path): Path(path).write_bytes(self.objects[self.endpoint, bucket, name])
    def list_objects(self, bucket, **kwargs):
        return [type('Object', (), {'object_name': name}) for endpoint, bucket_name, name in self.objects if (endpoint, bucket_name) == (self.endpoint, bucket)]


@pytest.fixture
def physical(tmp_path):
    config = tmp_path / 'fixture.json'
    config.write_text(json.dumps({role: {'endpoint': role, 'bucket': 'raster-' + role, 'access_key': 'test', 'secret_key': 'test'} for role in ('source', 'target')}))
    LocalMinio.objects = {}
    with patch('minio.Minio', LocalMinio):
        seeded = fixture.worker('seed', config)
        assert seeded['seeded']
        fixture.private_json(config.with_name('raster-source-sha256.json'), seeded['source_sha256'])
        yield config


def target(tmp_path, factor=1, cog=True, crs=4326, corrupt=False, nodata=float('nan')):
    size = fixture.SIZE
    path = tmp_path / 'computed.tif'
    dataset = gdal.GetDriverByName('GTiff').Create(str(path), size, size, 1, gdal.GDT_Float64)
    dataset.SetGeoTransform(fixture.TRANSFORM)
    reference = osr.SpatialReference(); reference.ImportFromEPSG(crs)
    dataset.SetProjection(reference.ExportToWkt())
    band = dataset.GetRasterBand(1)
    band.SetNoDataValue(nodata)
    values = [float('nan')] + [float(factor * value) for value in range(2, size * size + 1)]
    if corrupt: values[-1] += 1
    band.WriteRaster(0, 0, size, size, struct.pack(f'<{len(values)}d', *values), buf_type=gdal.GDT_Float64)
    dataset = None
    if cog:
        cog_path = tmp_path / 'cog.tif'
        result = gdal.Translate(str(cog_path), str(path), format='COG', creationOptions=['BLOCKSIZE=128', 'COMPRESS=DEFLATE'])
        result = None
        path = cog_path
    LocalMinio.objects['target', 'raster-target', 'result.cog.tif'] = path.read_bytes()


def spatial_target(tmp_path, overlap, *, wrong_overlap=False, corrupt=False, crs=3857, transform=None):
    from operators.raster_compute import (raster_workspace, raster_load, raster_reproject,
        raster_band_math, raster_clip, raster_mosaic, raster_save)
    source = tmp_path / 'spatial.tif'
    source.write_bytes(LocalMinio.objects['source', 'raster-source', 'spatial.tif'])
    path = tmp_path / f'mosaic-{overlap}.cog.tif'
    with raster_workspace():
        loaded = raster_load({'schema_version': 'addp.workflow.access-plan/v1', 'source': {
            'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(source)}}})
        projected = raster_reproject(loaded, 'EPSG:3857', [1, 1], 'nearest')
        left = raster_clip(raster_band_math(projected, 'b2-b1'), 'EPSG:3857', bbox=[0, 0, 160, 256])
        right = raster_clip(raster_band_math(projected, 'b2+b1'), 'EPSG:3857', bbox=[96, 0, 256, 256])
        mosaic = raster_mosaic(left, right, 'EPSG:3857', [1, 1],
            ('last' if overlap == 'first' else 'first') if wrong_overlap else overlap)
        raster_save(mosaic, {'schema_version': 'addp.workflow.access-plan/v1', 'target': {
            'kind': 'file', 'format': 'tiff', 'name': path.name, 'write_mode': 'create',
            'access': {'method': 'mounted_path', 'path': str(path)}}}, profile='cog', blocksize=128)
    if corrupt or crs != 3857 or transform is not None:
        edited = tmp_path / 'edited.tif'
        dataset = gdal.Translate(str(edited), str(path), format='GTiff')
        if corrupt:
            dataset.GetRasterBand(1).WriteRaster(159, 128, 1, 1, struct.pack('<d', -1), buf_type=gdal.GDT_Float64)
        if crs != 3857:
            reference = osr.SpatialReference(); reference.ImportFromEPSG(crs)
            dataset.SetProjection(reference.ExportToWkt())
        if transform is not None: dataset.SetGeoTransform(transform)
        dataset = None
        dataset = gdal.Translate(str(path), str(edited), format='COG', creationOptions=['BLOCKSIZE=128'])
        dataset = None
    LocalMinio.objects['target', 'raster-target', path.name] = path.read_bytes()


def test_real_reproject_clip_mosaic_outputs_pass_independent_all_pixel_oracle(physical, tmp_path):
    target(tmp_path, factor=3)
    spatial_target(tmp_path, 'first')
    first = fixture.worker('verify-mosaic-first', physical)
    assert (first['valid_pixels'], first['invalid_pixels'], first['overlap']) == (65534, 2, 'first')
    spatial_target(tmp_path, 'last')
    last = fixture.worker('verify-mosaic-last', physical)
    assert last['sha256'] != first['sha256']
    assert last['first_sha256'] == first['sha256']
    assert last['baseline_sha256'] == first['baseline_sha256']


@pytest.mark.parametrize('case_name', fixture.ANALYSIS_CASES)
def test_async_analysis_json_matches_independent_fixture_oracle(physical, tmp_path, monkeypatch, case_name):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    source = tmp_path / 'source.tif'
    original = LocalMinio.objects['source', 'raster-source', 'source.tif']
    source.write_bytes(original)
    definition = scene.analysis_workflow('source-locator', case_name)
    definition['tasks'][0]['params'] = {'access_plan': {'schema_version': 'addp.workflow.access-plan/v1',
        'source': {'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(source)}}}}
    client = api_server.app.test_client()
    response = client.post('/api/workflow', json={'workflow_def': definition, 'input_data': {}, 'runtime': {
        'tenant_id': 7, 'execution_authorization': {'id': 1, 'effects': ['read']}}})
    assert response.status_code == 202
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        status = client.get('/api/executions/' + response.json['execution_id']).json
        if status['status'] in ('success', 'failed'): break
        time.sleep(.01)
    assert status['status'] == 'success', status
    numeric = json.loads(status['result'])
    # The oracle uses only Python statistics and deterministic pixel formulas.
    scene.validate_analysis({'metadata': {'result': {'summary': {'has_result': True}, 'final_result': numeric}}},
        fixture.analysis_expectations()[case_name])
    assert source.read_bytes() == original


def test_analysis_physical_verification_keeps_all_cog_hashes_and_rejects_extra_objects(physical, tmp_path):
    target(tmp_path, factor=3)
    spatial_target(tmp_path, 'first')
    first = fixture.worker('verify-mosaic-first', physical)
    spatial_target(tmp_path, 'last')
    last = fixture.worker('verify-mosaic-last', physical)
    result = fixture.worker('verify-analysis', physical)
    assert result['sha256'] == last['baseline_sha256']
    assert result['first_sha256'] == first['sha256']
    assert result['last_sha256'] == last['sha256']
    LocalMinio.objects['target', 'raster-target', 'analysis.json'] = b'{}'
    with pytest.raises(fixture.FixtureError, match='unexpected'): fixture.worker('verify-analysis', physical)


@pytest.mark.parametrize('kwargs', [{'wrong_overlap': True}, {'corrupt': True}, {'crs': 4326},
                                   {'transform': (0, 2, 0, 256, 0, -1)}])
def test_spatial_oracle_rejects_wrong_seam_pixels_crs_and_grid(physical, tmp_path, kwargs):
    target(tmp_path, factor=3)
    spatial_target(tmp_path, 'first', **kwargs)
    with pytest.raises(fixture.FixtureError): fixture.worker('verify-mosaic-first', physical)


def test_spatial_verification_rejects_changes_to_prior_artifacts_and_source(physical, tmp_path):
    target(tmp_path, factor=3)
    spatial_target(tmp_path, 'first')
    target(tmp_path, factor=1)
    with pytest.raises(fixture.FixtureError): fixture.worker('verify-mosaic-first', physical)
    target(tmp_path, factor=3)
    source = tmp_path / 'changed.tif'
    source.write_bytes(LocalMinio.objects['source', 'raster-source', 'spatial.tif'])
    dataset = gdal.Open(str(source), gdal.GA_Update)
    dataset.GetRasterBand(1).WriteRaster(127, 127, 1, 1, struct.pack('<d', 1), buf_type=gdal.GDT_Float64)
    dataset = None
    LocalMinio.objects['source', 'raster-source', 'spatial.tif'] = source.read_bytes()
    with pytest.raises(fixture.FixtureError): fixture.worker('verify-mosaic-first', physical)


def test_source_metadata_only_change_and_missing_fingerprint_are_rejected(physical, tmp_path):
    target(tmp_path)
    source = tmp_path / 'metadata-change.tif'
    source.write_bytes(LocalMinio.objects['source', 'raster-source', 'source.tif'])
    dataset = gdal.Open(str(source), gdal.GA_Update)
    dataset.SetMetadataItem('fixture_change', 'metadata only')
    dataset = None
    LocalMinio.objects['source', 'raster-source', 'source.tif'] = source.read_bytes()
    with pytest.raises(fixture.FixtureError, match='source bytes'): fixture.worker('verify-create', physical)
    physical.with_name('raster-source-sha256.json').unlink()
    with pytest.raises(fixture.FixtureError, match='fingerprints are missing'): fixture.worker('verify-create', physical)


def test_independent_fixture_verifies_create_replace_pixels_and_cog(physical, tmp_path):
    target(tmp_path)
    created = fixture.worker('verify-create', physical)
    assert created['valid_pixels'] == fixture.SIZE ** 2 - 1
    assert created['cog_valid'] and created['source_unchanged']
    target(tmp_path, factor=3)
    replaced = fixture.worker('verify-replace', physical)
    assert replaced['sha256'] != created['sha256']
    assert replaced['factor'] == 3


@pytest.mark.parametrize('kwargs', [{'cog': False}, {'crs': 3857}, {'corrupt': True}, {'nodata': -9999}, {'factor': 3}])
def test_independent_fixture_rejects_incorrect_persisted_bytes(physical, tmp_path, kwargs):
    target(tmp_path, **kwargs)
    with pytest.raises(fixture.FixtureError): fixture.worker('verify-create', physical)


def test_independent_fixture_rejects_source_changes_and_partial_objects(physical, tmp_path):
    target(tmp_path)
    LocalMinio.objects['target', 'raster-target', 'partial.tmp'] = b'partial'
    with pytest.raises(fixture.FixtureError): fixture.worker('verify-create', physical)
    del LocalMinio.objects['target', 'raster-target', 'partial.tmp']
    source_path = tmp_path / 'source.tif'
    source_path.write_bytes(LocalMinio.objects['source', 'raster-source', 'source.tif'])
    source = gdal.Open(str(source_path), gdal.GA_Update)
    source.GetRasterBand(1).SetNoDataValue(-8888)
    source = None
    LocalMinio.objects['source', 'raster-source', 'source.tif'] = source_path.read_bytes()
    with pytest.raises(fixture.FixtureError): fixture.worker('verify-create', physical)


@pytest.mark.parametrize('case_name', fixture.GRID_CASES)
def test_async_grid_outputs_pass_independent_pixel_and_alpha_oracle(physical, tmp_path, monkeypatch, case_name):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    # Reuse the complete persistence protocol rather than testing a second DAG.
    target(tmp_path, factor=3)
    spatial_target(tmp_path, 'first')
    spatial_target(tmp_path, 'last')
    for prior in fixture.GRID_CASES[:fixture.GRID_CASES.index(case_name)]:
        grid_target(tmp_path, scene, api_server, prior)
    result, output = grid_target(tmp_path, scene, api_server, case_name)
    evidence = fixture.worker('verify-' + case_name, physical)
    expected = fixture.grid_expectation(case_name)
    assert evidence['has_overviews'] is (case_name=='clip-polygon')
    assert evidence['valid_pixels'] == expected['valid_pixels']
    assert evidence['invalid_pixels'] == expected['width'] * expected['height'] - expected['valid_pixels']
    assert result['band_count'] == expected['band_count']
    if case_name == 'clip-polygon':
        from operators.raster_compute import raster_workspace, raster_load, raster_statistics, raster_band_math
        with raster_workspace():
            raster = raster_load(source_access_plan(output))
            assert raster_statistics(raster)['valid_count'] == 26623
            assert raster_statistics(raster_band_math(raster, 'b1*2'))['valid_count'] == 26623


def source_access_plan(path):
    return {'schema_version': 'addp.workflow.access-plan/v1', 'source': {
        'kind': 'file', 'format': 'tiff', 'access': {'method': 'mounted_path', 'path': str(path)}}}


def single_source_execution(tmp_path, scene, api_server, case_name):
    source_name = (fixture.NON_COG_SOURCE if case_name == 'validate-cog-invalid' else
                   'build-overviews.cog.tif' if case_name == 'info-overviews' else
                   'to-cog.cog.tif' if case_name == 'validate-cog-valid' else 'source.tif')
    role = 'target' if source_name.endswith('.cog.tif') else 'source'
    source = tmp_path / ('utility-source-' + source_name)
    original = LocalMinio.objects[role, 'raster-' + role, source_name]
    source.write_bytes(original)
    definition = (scene.outside_workflow if case_name in fixture.OUTSIDE_CASES else scene.aggregate_workflow if case_name in fixture.AGGREGATE_CASES else scene.reclass_workflow if case_name in fixture.RECLASS_CASES else scene.utility_workflow)(
        'source-locator', 2, case_name)
    output = tmp_path / (case_name + '.cog.tif')
    plan = source_access_plan(source)
    if case_name in fixture.UTILITY_CASES + fixture.RECLASS_CASES + fixture.AGGREGATE_CASES + fixture.OUTSIDE_CASES:
        plan['target'] = {'kind': 'file', 'format': 'tiff', 'name': output.name, 'write_mode': 'create',
            'access': {'method': 'mounted_path', 'path': str(output)}}
    if case_name == 'to-cog':
        params = definition['tasks'][0]['params']
        options = {key: params[key] for key in ('blocksize', 'overview_resampling')}
        params.clear()
        params.update(access_plan=plan, options=options)
    else:
        definition['tasks'][0]['params'] = {'access_plan': {key: value for key, value in plan.items() if key != 'target'}}
        if case_name in fixture.UTILITY_CASES + fixture.RECLASS_CASES + fixture.AGGREGATE_CASES + fixture.OUTSIDE_CASES:
            params = definition['tasks'][-1]['params']
            for key in ('target_parent_locator', 'target_name', 'write_mode'): params.pop(key)
            params['access_plan'] = {key: value for key, value in plan.items() if key != 'source'}
    client = api_server.app.test_client()
    response = client.post('/api/workflow', json={'workflow_def': definition, 'input_data': {}, 'runtime': {
        'tenant_id': 7, 'execution_authorization': {'id': 1, 'effects': ['read', 'write'] if case_name in fixture.UTILITY_CASES + fixture.RECLASS_CASES + fixture.AGGREGATE_CASES + fixture.OUTSIDE_CASES else ['read']}}})
    assert response.status_code == 202
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        status = client.get('/api/executions/' + response.json['execution_id']).json
        if status['status'] in ('success', 'failed'): break
        time.sleep(.01)
    assert status['status'] == 'success', status
    assert source.read_bytes() == original
    result = json.loads(status['result'])
    if case_name in fixture.UTILITY_CASES + fixture.RECLASS_CASES + fixture.AGGREGATE_CASES + fixture.OUTSIDE_CASES:
        assert result['size_bytes'] == output.stat().st_size > 0
        LocalMinio.objects['target', 'raster-target', output.name] = output.read_bytes()
    return result, output


def grid_target(tmp_path, scene, api_server, case_name):
    multiband = case_name in fixture.MULTIBAND_CASES
    joint = case_name.endswith('-joint')
    source_name = fixture.multiband_source_name(case_name) if multiband else 'spatial.tif'
    source = tmp_path / source_name
    source.write_bytes(LocalMinio.objects['target' if joint else 'source', 'raster-target' if joint else 'raster-source', source_name])
    original = source.read_bytes()
    output = tmp_path / (case_name + '.cog.tif')
    definition = (scene.multiband_workflow if multiband else scene.grid_workflow)('source-locator', 2, case_name)
    definition['tasks'][0]['params'] = {'access_plan': source_access_plan(source)}
    definition['tasks'][-1]['params'] = {'input_raster': {'$ref': 'grid', 'port': 'default'},
        'profile': 'cog', 'blocksize': 128, 'access_plan': {'schema_version': 'addp.workflow.access-plan/v1',
            'target': {'kind': 'file', 'format': 'tiff', 'name': output.name, 'write_mode': 'create',
                       'access': {'method': 'mounted_path', 'path': str(output)}}}}
    client = api_server.app.test_client()
    response = client.post('/api/workflow', json={'workflow_def': definition, 'input_data': {}, 'runtime': {
        'tenant_id': 7, 'execution_authorization': {'id': 1, 'effects': ['read', 'write']}}})
    assert response.status_code == 202
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        status = client.get('/api/executions/' + response.json['execution_id']).json
        if status['status'] in ('success', 'failed'): break
        time.sleep(.01)
    assert status['status'] == 'success', status
    assert source.read_bytes() == original
    LocalMinio.objects['target', 'raster-target', output.name] = output.read_bytes()
    return json.loads(status['result']), output


@pytest.mark.parametrize('fault', ['pixel', 'hole', 'alpha', 'nodata', 'crs', 'grid', 'extra'])
def test_grid_oracle_rejects_corrupt_pixels_masks_georeferencing_and_extra_objects(physical, tmp_path, monkeypatch, fault):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    target(tmp_path, factor=3)
    spatial_target(tmp_path, 'first')
    spatial_target(tmp_path, 'last')
    for case in fixture.GRID_CASES:
        _, output = grid_target(tmp_path, scene, api_server, case)
    edited = tmp_path / 'changed-polygon.tif'
    ds = gdal.Translate(str(edited), str(output), format='GTiff')
    if fault == 'pixel': ds.GetRasterBand(1).WriteRaster(0, 0, 1, 1, struct.pack('<d', -1), buf_type=gdal.GDT_Float64)
    if fault == 'hole': ds.GetRasterBand(1).WriteRaster(40, 140, 1, 1, struct.pack('<d', 1), buf_type=gdal.GDT_Float64)
    if fault == 'alpha': ds.GetRasterBand(2).WriteRaster(40, 140, 1, 1, struct.pack('<d', 255), buf_type=gdal.GDT_Float64)
    if fault == 'nodata': ds.GetRasterBand(1).DeleteNoDataValue()
    if fault == 'crs':
        crs = osr.SpatialReference(); crs.ImportFromEPSG(3857); ds.SetProjection(crs.ExportToWkt())
    if fault == 'grid': ds.SetGeoTransform((0,1,0,192,0,-1))
    ds = None
    ds = gdal.Translate(str(output), str(edited), format='COG', creationOptions=['BLOCKSIZE=128'])
    ds = None
    LocalMinio.objects['target', 'raster-target', output.name] = output.read_bytes()
    if fault == 'extra': LocalMinio.objects['target', 'raster-target', 'partial.tmp'] = b'partial'
    with pytest.raises(fixture.FixtureError): fixture.worker('verify-clip-polygon', physical)


def multiband_targets(tmp_path, scene, api_server, until='multiband-average-joint'):
    target(tmp_path, factor=3)
    spatial_target(tmp_path, 'first')
    spatial_target(tmp_path, 'last')
    for case in fixture.GRID_CASES:
        grid_target(tmp_path, scene, api_server, case)
    evidence = []
    for case in fixture.MULTIBAND_CASES[:fixture.MULTIBAND_CASES.index(until)+1]:
        _, output = grid_target(tmp_path, scene, api_server, case)
        evidence.append(fixture.worker('verify-' + case, tmp_path / 'fixture.json'))
    return evidence, tmp_path / 'multiband-joint.cog.tif'


def test_async_multiband_save_reload_joint_math_passes_independent_all_pixel_oracle(physical, tmp_path, monkeypatch):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    evidence, output = multiband_targets(tmp_path, scene, api_server)
    assert evidence[0]['band_valid_pixels'] == [16382, 16382]
    assert evidence[0]['partial_alpha_pixels'] == 3
    assert evidence[1]['band_valid_pixels'] == [16381]
    assert evidence[1]['preserved_sha256']['multiband-alpha.cog.tif'] == evidence[0]['sha256']
    from operators.raster_compute import raster_workspace, raster_load, raster_statistics
    with raster_workspace():
        stats = raster_statistics(raster_load(source_access_plan(output)))
        assert (stats['valid_count'], stats['invalid_count'], stats['min']) == (16381, 3, 0.)


@pytest.mark.parametrize('fault', ['first-hole', 'second-hole', 'alpha', 'zero', 'joint-hole', 'nodata', 'extra'])
def test_multiband_oracle_rejects_corrupt_independent_holes_alpha_joint_mask_and_artifacts(physical, tmp_path, monkeypatch, fault):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    _, joint_path = multiband_targets(tmp_path, scene, api_server)
    joint = fault == 'joint-hole'
    output = joint_path if joint else tmp_path / 'multiband-alpha.cog.tif'
    edited = tmp_path / 'corrupt-multiband.tif'
    dataset = gdal.Translate(str(edited), str(output), format='GTiff')
    if fault == 'first-hole': dataset.GetRasterBand(1).WriteRaster(0, 0, 1, 1, struct.pack('<d', 1), buf_type=gdal.GDT_Float64)
    if fault == 'second-hole': dataset.GetRasterBand(2).WriteRaster(1, 0, 1, 1, struct.pack('<d', 1), buf_type=gdal.GDT_Float64)
    if fault == 'alpha': dataset.GetRasterBand(3).WriteRaster(3, 0, 1, 1, struct.pack('<d', 255), buf_type=gdal.GDT_Float64)
    if fault == 'zero': dataset.GetRasterBand(1).WriteRaster(4, 0, 1, 1, struct.pack('<d', float('nan')), buf_type=gdal.GDT_Float64)
    if fault == 'joint-hole': dataset.GetRasterBand(1).WriteRaster(1, 0, 1, 1, struct.pack('<d', 1), buf_type=gdal.GDT_Float64)
    if fault == 'nodata': dataset.GetRasterBand(2).DeleteNoDataValue()
    dataset = None
    dataset = gdal.Translate(str(output), str(edited), format='COG', creationOptions=['BLOCKSIZE=128'])
    dataset = None
    LocalMinio.objects['target', 'raster-target', output.name] = output.read_bytes()
    if fault == 'extra': LocalMinio.objects['target', 'raster-target', 'partial.tmp'] = b'partial'
    with pytest.raises(fixture.FixtureError): fixture.worker('verify-multiband-average-joint', physical)


def test_async_average_multiband_saved_then_reloaded_math_passes_independent_area_oracle(physical, tmp_path, monkeypatch):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    evidence, _ = multiband_targets(tmp_path, scene, api_server)
    average, joint = evidence[2:]
    assert average['band_valid_pixels'] == [16382, 16382]
    assert average['partial_alpha_pixels'] == 0
    assert joint['band_valid_pixels'] == [16381]
    assert joint['preserved_sha256']['multiband-average.cog.tif'] == average['sha256']
    dataset = gdal.Open(str(tmp_path / 'multiband-average.cog.tif'))
    first = dataset.GetRasterBand(1).ReadRaster(5, 0, 2, 1, buf_type=gdal.GDT_Float64)
    second = dataset.GetRasterBand(2).ReadRaster(5, 0, 2, 1, buf_type=gdal.GDT_Float64)
    assert struct.unpack('<2d', first) == pytest.approx([88., 308/3], rel=1e-10, abs=1e-8)
    assert struct.unpack('<2d', second) == pytest.approx([168., 608/3], rel=1e-10, abs=1e-8)
    dataset = None
    dataset = gdal.Open(str(tmp_path / 'multiband-average-joint.cog.tif'))
    assert struct.unpack('<d', dataset.GetRasterBand(1).ReadRaster(5, 0, 1, 1, buf_type=gdal.GDT_Float64))[0] == pytest.approx(256.)


@pytest.mark.parametrize('fault', ['nearest', 'early-math', 'band-hole', 'transparent', 'alpha', 'zero', 'nodata'])
def test_average_oracle_rejects_wrong_kernel_early_joint_mask_and_saved_band_facts(physical, tmp_path, monkeypatch, fault):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    multiband_targets(tmp_path, scene, api_server)
    joint = fault == 'early-math'
    output = tmp_path / ('multiband-average-joint.cog.tif' if joint else 'multiband-average.cog.tif')
    edited = tmp_path / 'corrupt-average.tif'
    dataset = gdal.Translate(str(edited), str(output), format='GTiff')
    band, column, value = 1, 3, 60.
    if fault == 'early-math': column, value = 5, 258.
    if fault == 'band-hole': band, column, value = 2, 5, 172.
    if fault == 'transparent': column, value = 2, 1e6
    if fault == 'alpha': band, column, value = 3, 3, 128.
    if fault == 'zero': column, value = 4, float('nan')
    if fault == 'nodata': dataset.GetRasterBand(2).DeleteNoDataValue()
    else: dataset.GetRasterBand(band).WriteRaster(column, 0, 1, 1, struct.pack('<d', value), buf_type=gdal.GDT_Float64)
    dataset = None
    dataset = gdal.Translate(str(output), str(edited), format='COG', creationOptions=['BLOCKSIZE=128'])
    dataset = None
    LocalMinio.objects['target', 'raster-target', output.name] = output.read_bytes()
    with pytest.raises(fixture.FixtureError, match='multiband|NoData'):
        fixture.worker('verify-multiband-average-joint', physical)


def test_async_bilinear_saved_then_reloaded_math_passes_independent_neighbour_oracle(physical, tmp_path, monkeypatch):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    evidence, _ = multiband_targets(tmp_path, scene, api_server, until='multiband-bilinear-joint')
    bilinear, joint = evidence[4:]
    assert bilinear['band_valid_pixels'] == [262104, 262104]
    assert bilinear['partial_alpha_pixels'] == 64
    assert joint['band_valid_pixels'] == [262080]
    assert bilinear['has_overviews'] and joint['has_overviews']
    assert joint['preserved_sha256']['multiband-bilinear.cog.tif'] == bilinear['sha256']
    dataset = gdal.Open(str(tmp_path / 'multiband-bilinear.cog.tif'))
    for band, expected in ((1, 1120/13), (2, 2168/13)):
        actual = struct.unpack('<d', dataset.GetRasterBand(band).ReadRaster(22, 1, 1, 1, buf_type=gdal.GDT_Float64))[0]
        assert actual == pytest.approx(expected, rel=1e-10, abs=1e-8)
    dataset = None
    dataset = gdal.Open(str(tmp_path / 'multiband-bilinear-joint.cog.tif'))
    actual = struct.unpack('<d', dataset.GetRasterBand(1).ReadRaster(22, 1, 1, 1, buf_type=gdal.GDT_Float64))[0]
    assert actual == pytest.approx(3288/13, rel=1e-10, abs=1e-8)


@pytest.mark.parametrize('fault', ['nearest', 'early-math', 'central-hole', 'band-hole', 'transparent', 'alpha', 'zero', 'nodata'])
def test_bilinear_oracle_rejects_wrong_kernel_central_fill_joint_mask_and_saved_facts(physical, tmp_path, monkeypatch, fault):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    multiband_targets(tmp_path, scene, api_server, until='multiband-bilinear-joint')
    output = tmp_path / ('multiband-bilinear-joint.cog.tif' if fault == 'early-math' else 'multiband-bilinear.cog.tif')
    edited = tmp_path / 'corrupt-bilinear.tif'
    dataset = gdal.Translate(str(edited), str(output), format='GTiff')
    band, column, value = 1, 13, 48.
    if fault == 'early-math': column, value = 22, 253.2
    if fault == 'central-hole': column, value = 20, 123.
    if fault == 'band-hole': column, value = 22, 70.
    if fault == 'transparent': column, value = 8, 1e6
    if fault == 'alpha': band, value = 3, 255.
    if fault == 'zero': column, value = 17, float('nan')
    if fault == 'nodata': dataset.GetRasterBand(2).DeleteNoDataValue()
    else: dataset.GetRasterBand(band).WriteRaster(column, 1, 1, 1, struct.pack('<d', value), buf_type=gdal.GDT_Float64)
    dataset = None
    dataset = gdal.Translate(str(output), str(edited), format='COG', creationOptions=['BLOCKSIZE=128'])
    dataset = None
    LocalMinio.objects['target', 'raster-target', output.name] = output.read_bytes()
    with pytest.raises(fixture.FixtureError, match='multiband|NoData'):
        fixture.worker('verify-multiband-bilinear-joint', physical)


def test_async_fractional_average_saved_reload_passes_independent_area_oracle(physical, tmp_path, monkeypatch):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    evidence, _ = multiband_targets(tmp_path, scene, api_server, until='multiband-average-fractional-joint')
    average, joint = evidence[6:]
    assert average['band_valid_pixels'] == [29239, 29239]
    assert average['partial_alpha_pixels'] == 0
    assert joint['band_valid_pixels'] == [29238]
    assert average['has_overviews'] and joint['has_overviews']
    assert len(joint['preserved_sha256']) == 13
    assert joint['preserved_sha256']['multiband-average-fractional.cog.tif'] == average['sha256']
    dataset = gdal.Open(str(tmp_path / 'multiband-average-fractional.cog.tif'))
    for band, column, expected in ((1, 4, 211179/4064), (1, 5, 19239/1024),
                                  (1, 7, 4370448/50317), (2, 7, 8564056/51341)):
        actual = struct.unpack('<d', dataset.GetRasterBand(band).ReadRaster(column, 0, 1, 1, buf_type=gdal.GDT_Float64))[0]
        assert actual == pytest.approx(expected, rel=1e-10, abs=1e-8)
    dataset = None
    dataset = gdal.Open(str(tmp_path / 'multiband-average-fractional-joint.cog.tif'))
    actual = struct.unpack('<d', dataset.GetRasterBand(1).ReadRaster(7, 0, 1, 1, buf_type=gdal.GDT_Float64))[0]
    assert actual == pytest.approx(4370448/50317+8564056/51341, rel=1e-10, abs=1e-8)


@pytest.mark.parametrize('fault', ['equal-weight', 'early-math', 'band-hole', 'transparent', 'alpha', 'zero-contribution', 'nodata'])
def test_fractional_oracle_rejects_wrong_weights_joint_mask_and_saved_band_facts(physical, tmp_path, monkeypatch, fault):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    multiband_targets(tmp_path, scene, api_server, until='multiband-average-fractional-joint')
    output = tmp_path / ('multiband-average-fractional-joint.cog.tif' if fault == 'early-math' else 'multiband-average-fractional.cog.tif')
    edited = tmp_path / 'corrupt-fractional.tif'
    dataset = gdal.Translate(str(edited), str(output), format='GTiff')
    band, column, value = 1, 4, 54.
    if fault == 'early-math': column, value = 7, 4596762/18061
    if fault == 'band-hole': band, column, value = 2, 2, 22.
    if fault == 'transparent': column, value = 3, 1e6
    if fault == 'alpha': band, column, value = 3, 4, 128.
    if fault == 'zero-contribution': column, value = 5, 3509/64
    if fault == 'nodata': dataset.GetRasterBand(2).DeleteNoDataValue()
    else: dataset.GetRasterBand(band).WriteRaster(column, 0, 1, 1, struct.pack('<d', value), buf_type=gdal.GDT_Float64)
    dataset = None
    dataset = gdal.Translate(str(output), str(edited), format='COG', creationOptions=['BLOCKSIZE=128'])
    dataset = None
    LocalMinio.objects['target', 'raster-target', output.name] = output.read_bytes()
    with pytest.raises(fixture.FixtureError, match='multiband|NoData'):
        fixture.worker('verify-multiband-average-fractional-joint', physical)


def test_async_finite_average_saved_reload_matches_nan_results_and_independent_area_oracle(physical, tmp_path, monkeypatch):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    evidence, _ = multiband_targets(tmp_path, scene, api_server, until='multiband-average-finite-joint')
    average, joint = evidence[8:]
    assert average['band_nodata'] == -9999. and average['band_nodata_is_nan'] is False
    assert joint['band_nodata'] is None and joint['band_nodata_is_nan'] is True
    assert average['band_valid_pixels'] == [29239, 29239]
    assert joint['band_valid_pixels'] == [29238]
    assert average['partial_alpha_pixels'] == 0
    assert average['has_overviews'] and joint['has_overviews']
    assert len(joint['preserved_sha256']) == 15
    assert joint['preserved_sha256']['multiband-average-finite.cog.tif'] == average['sha256']
    source = gdal.Open(str(tmp_path / 'multiband-average-finite.tif'))
    for band, column in ((1, 0), (2, 2)):
        assert source.GetRasterBand(band).GetNoDataValue() == -9999.
        assert struct.unpack('<d', source.GetRasterBand(band).ReadRaster(column, 0, 1, 1, buf_type=gdal.GDT_Float64))[0] == -9999.
    source = None
    for suffix in ('', '-joint'):
        finite = gdal.Open(str(tmp_path / ('multiband-average-finite'+suffix+'.cog.tif')))
        nan = gdal.Open(str(tmp_path / ('multiband-average-fractional'+suffix+'.cog.tif')))
        for index in range(1, finite.RasterCount+1):
            nodata = finite.GetRasterBand(index).GetNoDataValue()
            assert math.isnan(nodata) if suffix else nodata == -9999.
            count = finite.RasterXSize*finite.RasterYSize
            actual = struct.unpack(f'<{count}d', finite.GetRasterBand(index).ReadRaster(buf_type=gdal.GDT_Float64))
            expected = struct.unpack(f'<{count}d', nan.GetRasterBand(index).ReadRaster(buf_type=gdal.GDT_Float64))
            assert all((math.isnan(a) if suffix else a == -9999.) if math.isnan(b) else a == pytest.approx(b, rel=1e-10, abs=1e-8)
                       for a, b in zip(actual, expected))
        finite = nan = None


@pytest.mark.parametrize('fault', ['source-hole', 'source-nodata'])
def test_finite_source_verifier_checks_pixels_and_declaration_even_with_matching_hash(physical, tmp_path, fault):
    source = tmp_path / 'multiband-average-finite.tif'
    source.write_bytes(LocalMinio.objects['source', 'raster-source', source.name])
    dataset = gdal.Open(str(source), gdal.GA_Update)
    if fault == 'source-hole':
        dataset.GetRasterBand(1).WriteRaster(0, 0, 1, 1, struct.pack('<d', 0.), buf_type=gdal.GDT_Float64)
    else:
        dataset.GetRasterBand(1).SetNoDataValue(float('nan'))
    dataset = None
    LocalMinio.objects['source', 'raster-source', source.name] = source.read_bytes()
    fingerprint_path = physical.with_name('raster-source-sha256.json')
    fingerprints = json.loads(fingerprint_path.read_text())
    fingerprints[source.name] = hashlib.sha256(source.read_bytes()).hexdigest()
    fingerprint_path.write_text(json.dumps(fingerprints))
    with pytest.raises(fixture.FixtureError, match='source pixels/NoData'):
        fixture.worker('verify-create', physical)


@pytest.mark.parametrize('fault', ['saved-hole', 'saved-nodata', 'joint-mask'])
def test_finite_oracle_rejects_lost_persisted_holes_nodata_and_joint_validity(physical, tmp_path, monkeypatch, fault):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    multiband_targets(tmp_path, scene, api_server, until='multiband-average-finite-joint')
    output = tmp_path / ('multiband-average-finite-joint.cog.tif' if fault == 'joint-mask' else 'multiband-average-finite.cog.tif')
    edited = tmp_path / 'corrupt-finite.tif'
    dataset = gdal.Translate(str(edited), str(output), format='GTiff')
    if fault == 'saved-nodata': dataset.GetRasterBand(2).DeleteNoDataValue()
    else:
        dataset.GetRasterBand(1).WriteRaster(0, 0, 1, 1, struct.pack('<d', 255/32 if fault == 'joint-mask' else 0.), buf_type=gdal.GDT_Float64)
    dataset = None
    dataset = gdal.Translate(str(output), str(edited), format='COG', creationOptions=['BLOCKSIZE=128'])
    dataset = None
    LocalMinio.objects['target', 'raster-target', output.name] = output.read_bytes()
    with pytest.raises(fixture.FixtureError, match='multiband|NoData'):
        fixture.worker('verify-multiband-average-finite-joint', physical)


@pytest.fixture(scope='module')
def utility_baseline_cache():
    return {}


@pytest.fixture
def utility_artifacts(physical, tmp_path, monkeypatch, utility_baseline_cache):
    monkeypatch.syspath_prepend(str(ROOT))
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    source_hashes = {key: hashlib.sha256(value).hexdigest() for key, value in LocalMinio.objects.items()}
    if not utility_baseline_cache:
        multiband_targets(tmp_path, scene, api_server, fixture.MULTIBAND_CASES[-1])
        utility_baseline_cache.update(sources=source_hashes,
            targets={key: value for key, value in LocalMinio.objects.items() if key[0] == 'target'})
    else:
        # Reuse immutable, already validated baseline bytes only for identical
        # source fingerprints; each test still computes both utility outputs.
        assert source_hashes == utility_baseline_cache['sources']
        LocalMinio.objects.update(utility_baseline_cache['targets'])
    for case in fixture.UTILITY_CASES:
        result, output = single_source_execution(tmp_path, scene, api_server, case)
        evidence = fixture.worker('verify-' + case, physical)
        assert evidence['size_bytes'] == result['size_bytes'] == output.stat().st_size > 0
        assert evidence['band_valid_pixels'] == [65535, 65535]
        assert evidence['overview_sizes'] == ([[128, 128], [64, 64]] if case == 'build-overviews' else [[128, 128]])
    return scene, api_server, physical


def test_async_utility_save_reload_info_and_cog_verdicts_match_physical_oracle(utility_artifacts, tmp_path):
    scene, api_server, physical = utility_artifacts
    before = {key: hashlib.sha256(value).hexdigest() for key, value in LocalMinio.objects.items()}
    evidence = fixture.worker('verify-utility-queries', physical)
    for case in fixture.UTILITY_JSON_CASES:
        result, _ = single_source_execution(tmp_path, scene, api_server, case)
        execution = {'metadata': {'result': {'summary': {'has_result': True}, 'final_result': result}}}
        assert scene.validate_utility_json(execution, case, evidence) == result
    assert {key: hashlib.sha256(value).hexdigest() for key, value in LocalMinio.objects.items()} == before


@pytest.mark.parametrize('fault', ['base-pixel', 'second-band', 'overview-pixel', 'overview-hole', 'missing-level', 'extra'])
def test_utility_physical_oracle_rejects_pixels_levels_masks_and_partial_artifacts(utility_artifacts, tmp_path, fault):
    _, _, physical = utility_artifacts
    if fault == 'extra':
        LocalMinio.objects['target', 'raster-target', 'partial-upload.tif'] = b'partial'
    else:
        name = 'build-overviews.cog.tif'
        source = tmp_path / 'corrupt-utility-source.tif'
        source.write_bytes(LocalMinio.objects['target', 'raster-target', name])
        edited = tmp_path / 'corrupt-utility.tif'
        ds = gdal.Translate(str(edited), str(source), format='GTiff', creationOptions=['TILED=YES', 'COPY_SRC_OVERVIEWS=YES'])
        if fault == 'missing-level':
            ds.BuildOverviews('NONE', [])
        else:
            band = ds.GetRasterBand(2 if fault in ('second-band', 'overview-pixel', 'overview-hole') else 1)
            if fault.startswith('overview-'): band = band.GetOverview(1)
            position = 0 if fault == 'overview-hole' else 1
            band.WriteRaster(position, position, 1, 1, struct.pack('<d', 123), buf_type=gdal.GDT_Float64)
            band = None
        ds = None
        changed = tmp_path / 'corrupt-utility.cog.tif'
        ds = gdal.Translate(str(changed), str(edited), format='COG', creationOptions=['BLOCKSIZE=128'])
        ds = None
        LocalMinio.objects['target', 'raster-target', name] = changed.read_bytes()
    with pytest.raises(fixture.FixtureError): fixture.worker('verify-utility-queries', physical)


def foundation_execution(tmp_path, scene, api_server, case_name):
    definition = scene.foundation_workflow('source-locator', 'reference-locator', 2, case_name)
    sources = []
    for task, role, name in ((definition['tasks'][0], 'source', 'spatial.tif'),
                             (definition['tasks'][1], 'target', 'mosaic-first.cog.tif')):
        source = tmp_path / (case_name + '-' + name)
        original = LocalMinio.objects[role, 'raster-' + role, name]
        source.write_bytes(original)
        task['params'] = {'access_plan': source_access_plan(source)}
        sources.append((source, original))
    output = tmp_path / (case_name + '.cog.tif')
    params = definition['tasks'][-1]['params']
    for key in ('target_parent_locator', 'target_name', 'write_mode'):
        params.pop(key)
    params['access_plan'] = {'schema_version': 'addp.workflow.access-plan/v1', 'target': {
        'kind': 'file', 'format': 'tiff', 'name': output.name, 'write_mode': 'create',
        'access': {'method': 'mounted_path', 'path': str(output)}}}
    client = api_server.app.test_client()
    response = client.post('/api/workflow', json={'workflow_def': definition, 'input_data': {}, 'runtime': {
        'tenant_id': 7, 'execution_authorization': {'id': 1, 'effects': ['read', 'write']}}})
    assert response.status_code == 202
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        status = client.get('/api/executions/' + response.json['execution_id']).json
        if status['status'] in ('success', 'failed'):
            break
        time.sleep(.01)
    assert status['status'] == 'success', status
    assert all(source.read_bytes() == original for source, original in sources)
    result = json.loads(status['result'])
    assert result['size_bytes'] == output.stat().st_size > 0
    LocalMinio.objects['target', 'raster-target', output.name] = output.read_bytes()
    return result, output


@pytest.fixture
def foundation_artifacts(utility_artifacts, tmp_path):
    scene, api_server, physical = utility_artifacts
    preserved = {key[2]: hashlib.sha256(value).hexdigest() for key, value in LocalMinio.objects.items() if key[0] == 'target'}
    for case in fixture.FOUNDATION_CASES:
        result, output = foundation_execution(tmp_path, scene, api_server, case)
        evidence = fixture.worker('verify-' + case, physical)
        assert evidence['size_bytes'] == result['size_bytes'] == output.stat().st_size > 0
        assert evidence['preserved_sha256'] == preserved
        assert evidence['band_valid_pixels'] == [65534] * result['band_count']
        assert evidence['color_interpretations'] == (['Red', 'Green', 'Blue'] if case == 'multiraster-rgb' else ['Gray'])
        preserved[output.name] = evidence['sha256']
    return physical


def test_async_multiraster_weighted_rgb_match_independent_physical_oracle(foundation_artifacts):
    evidence = fixture.worker('verify-multiraster-rgb', foundation_artifacts)
    assert len(evidence['preserved_sha256']) == 19
    assert evidence['invalid_pixels'] == 2
    assert evidence['overview_sizes'] == [[128, 128]]


@pytest.fixture
def reclass_artifacts(foundation_artifacts, tmp_path):
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    preserved = {key[2]: hashlib.sha256(value).hexdigest() for key, value in LocalMinio.objects.items() if key[0] == 'target'}
    for case in fixture.RECLASS_CASES:
        result, output = single_source_execution(tmp_path, scene, api_server, case)
        evidence = fixture.worker('verify-' + case, foundation_artifacts)
        assert evidence['size_bytes'] == result['size_bytes'] == output.stat().st_size > 0
        assert evidence['preserved_sha256'] == preserved
        assert evidence['band_valid_pixels'] == [fixture.reclass_expectation(case)['valid_pixels']]
        preserved[output.name] = evidence['sha256']
    return foundation_artifacts


def test_async_reclassification_default_and_keep_match_physical_oracle(reclass_artifacts):
    evidence = fixture.worker('verify-reclassify-keep', reclass_artifacts)
    assert len(evidence['preserved_sha256']) == 21
    assert evidence['band_valid_pixels'] == [65535]
    assert evidence['overview_sizes'] == [[128, 128]]


@pytest.fixture
def aggregate_artifacts(reclass_artifacts,tmp_path):
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    preserved = {key[2]:hashlib.sha256(value).hexdigest() for key,value in LocalMinio.objects.items() if key[0]=='target'}
    for case in fixture.AGGREGATE_CASES:
        result,output = single_source_execution(tmp_path,scene,api_server,case)
        evidence = fixture.worker('verify-'+case,reclass_artifacts)
        assert evidence['size_bytes']==result['size_bytes']==output.stat().st_size>0
        assert evidence['preserved_sha256']==preserved
        assert evidence['band_valid_pixels']==[4472] and evidence['invalid_pixels']==0
        assert evidence['overview_sizes']==[[43,26]]
        preserved[output.name]=evidence['sha256']
    return reclass_artifacts


def test_async_aggregation_partial_groups_and_source_nodata_match_physical_oracle(aggregate_artifacts):
    evidence = fixture.worker('verify-aggregate-sum',aggregate_artifacts)
    assert len(evidence['preserved_sha256'])==23
    assert evidence['valid_pixels']==4472
    assert list(fixture.aggregate_pixels('aggregate-sum'))[-1]==131072.
    assert list(fixture.aggregate_pixels('aggregate-mean'))[-1]==131072.


def test_aggregation_physical_oracle_rejects_pixels_edge_validity_and_preservation_faults(aggregate_artifacts,tmp_path):
    baseline = dict(LocalMinio.objects)
    try:
        for fault in ['interior','edge','nodata-average','overview','mask','prior','extra','source']:
            LocalMinio.objects.clear()
            LocalMinio.objects.update(baseline)
            if fault=='extra':
                LocalMinio.objects['target','raster-target','partial.tif']=b'partial'
            elif fault in ('source','prior'):
                key = ('source','raster-source','source.tif') if fault=='source' else ('target','raster-target','reclassify-keep.cog.tif')
                LocalMinio.objects[key]+=b'changed'
            else:
                name = 'aggregate-mean.cog.tif'
                original = tmp_path/'aggregate-original.tif'
                original.write_bytes(LocalMinio.objects['target','raster-target',name])
                edited = tmp_path/'aggregate-corrupt.tif'
                dataset = gdal.Translate(str(edited),str(original),format='GTiff')
                band = dataset.GetRasterBand(1)
                x,y = (85,51) if fault=='edge' else (0,0) if fault=='nodata-average' else (1,1)
                if fault=='mask':
                    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK','YES'):
                        dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
                    mask = bytearray([255]*4472)
                    mask[87]=0
                    band.GetMaskBand().WriteRaster(0,0,86,52,bytes(mask),buf_type=gdal.GDT_Byte)
                elif fault!='overview':
                    value = next(fixture.aggregate_pixels('aggregate-mean'))*14/15 if fault=='nodata-average' else 123.
                    band.WriteRaster(x,y,1,1,struct.pack('<d',value),buf_type=gdal.GDT_Float64)
                dataset.BuildOverviews('NEAREST',[2])
                if fault=='overview':
                    band.GetOverview(0).WriteRaster(1,0,1,1,struct.pack('<d',123.),buf_type=gdal.GDT_Float64)
                band = dataset = None
                output = gdal.Translate(str(original),str(edited),format='COG',creationOptions=['BLOCKSIZE=128'])
                output = None
                LocalMinio.objects['target','raster-target',name]=original.read_bytes()
            if fault=='prior':
                evidence=fixture.worker('verify-aggregate-sum',aggregate_artifacts)
                assert evidence['preserved_sha256']['reclassify-keep.cog.tif'] != hashlib.sha256(
                    baseline['target','raster-target','reclassify-keep.cog.tif']).hexdigest()
            else:
                try:
                    fixture.worker('verify-aggregate-sum',aggregate_artifacts)
                except fixture.FixtureError:
                    pass
                else:
                    pytest.fail('Physical oracle accepted corruption: '+fault)

    finally:
        LocalMinio.objects.clear()
        LocalMinio.objects.update(baseline)


@pytest.mark.parametrize('fault', ['gap', 'zero-class', 'source-nodata', 'kept-value', 'overview', 'nodata', 'extra', 'source'])
def test_reclassification_physical_oracle_rejects_gap_class_validity_and_preservation_faults(reclass_artifacts, tmp_path, fault):
    if fault == 'extra':
        LocalMinio.objects['target', 'raster-target', 'partial.tif'] = b'partial'
    elif fault == 'source':
        key = ('source', 'raster-source', 'source.tif')
        LocalMinio.objects[key] += b'changed'
    else:
        name = ('reclassify-keep' if fault in ('kept-value', 'source-nodata') else 'reclassify-nodata') + '.cog.tif'
        source = tmp_path / 'reclass-corrupt-source.tif'
        source.write_bytes(LocalMinio.objects['target', 'raster-target', name])
        edited = tmp_path / 'reclass-corrupt.tif'
        dataset = gdal.Translate(str(edited), str(source), format='GTiff')
        band = dataset.GetRasterBand(1)
        if fault == 'nodata':
            band.DeleteNoDataValue()
        elif fault == 'overview':
            dataset.BuildOverviews('NEAREST', [2])
            band.GetOverview(0).WriteRaster(1, 0, 1, 1, struct.pack('<d', 123.), buf_type=gdal.GDT_Float64)
        else:
            position = 1 if fault == 'zero-class' else 0 if fault == 'source-nodata' else 511
            band.WriteRaster(position % 256, position // 256, 1, 1, struct.pack('<d', 123.), buf_type=gdal.GDT_Float64)
        band = None
        dataset = None
        dataset = gdal.Translate(str(source), str(edited), format='COG', creationOptions=['BLOCKSIZE=128'])
        dataset = None
        LocalMinio.objects['target', 'raster-target', name] = source.read_bytes()
    with pytest.raises(fixture.FixtureError):
        fixture.worker('verify-reclassify-keep', reclass_artifacts)


@pytest.mark.parametrize('fault', ['weighted-pixel', 'rgb-green', 'rgb-hole', 'rgb-colour', 'overview', 'nodata', 'grid', 'extra', 'source'])
def test_multiraster_physical_oracle_rejects_value_colour_mask_grid_and_source_faults(foundation_artifacts, tmp_path, fault):
    if fault == 'extra':
        LocalMinio.objects['target', 'raster-target', 'partial.tif'] = b'partial'
    elif fault == 'source':
        key = ('source', 'raster-source', 'spatial.tif')
        LocalMinio.objects[key] = LocalMinio.objects[key] + b'changed'
    else:
        name = 'multiraster-weighted.cog.tif' if fault == 'weighted-pixel' else 'multiraster-rgb.cog.tif'
        source = tmp_path / 'multiraster-corrupt-source.tif'
        source.write_bytes(LocalMinio.objects['target', 'raster-target', name])
        edited = tmp_path / 'multiraster-corrupt.tif'
        ds = gdal.Translate(str(edited), str(source), format='GTiff', creationOptions=['TILED=YES', 'COPY_SRC_OVERVIEWS=YES'])
        if fault == 'rgb-colour':
            ds.GetRasterBand(1).SetColorInterpretation(gdal.GCI_BlueBand)
            ds.GetRasterBand(3).SetColorInterpretation(gdal.GCI_RedBand)
        elif fault == 'nodata':
            ds.GetRasterBand(2).DeleteNoDataValue()
        elif fault == 'grid':
            transform = list(ds.GetGeoTransform())
            transform[0] += 1
            ds.SetGeoTransform(transform)
        else:
            band = ds.GetRasterBand(2 if fault in ('rgb-green', 'rgb-hole', 'overview') else 1)
            if fault == 'overview': band = band.GetOverview(0)
            position = 0 if fault == 'rgb-hole' else 1
            band.WriteRaster(position, position, 1, 1, struct.pack('<d', 123), buf_type=gdal.GDT_Float64)
            band = None
        ds = None
        changed = tmp_path / 'multiraster-corrupt.cog.tif'
        ds = gdal.Translate(str(changed), str(edited), format='COG', creationOptions=['BLOCKSIZE=128'])
        ds = None
        LocalMinio.objects['target', 'raster-target', name] = changed.read_bytes()
    with pytest.raises(fixture.FixtureError):
        fixture.worker('verify-multiraster-rgb', foundation_artifacts)


class ManagerLocalMinio(LocalMinio):
    def stat_object(self, bucket, name):
        from minio.error import S3Error
        key = (self.endpoint, bucket, name)
        if key not in self.objects:
            raise S3Error('NoSuchKey', 'missing', name, '', '', None)
        return type('Stat', (), {'size': len(self.objects[key])})

    def list_objects(self, bucket, prefix='', **kwargs):
        return [value for value in super().list_objects(bucket, **kwargs) if value.object_name.startswith(prefix)]


@pytest.fixture
def manager_physical(tmp_path):
    config = tmp_path / 'manager-raster-fixture.json'
    config.write_text(json.dumps({
        'source': {'endpoint': 'source', 'bucket': 'business', 'object': 'raster/source.tif', 'access_key': 'test', 'secret_key': 'test'},
        'infra': {'endpoint': 'infra', 'access_key': 'test', 'secret_key': 'test'},
    }))
    LocalMinio.objects = {}
    with patch('minio.Minio', ManagerLocalMinio):
        seeded = fixture.manager_worker('seed', config)
        fixture.private_json(tmp_path / 'manager-raster-source.json', seeded)
        yield config


def manager_target(config, *, cog=True, corrupt=None):
    source = config.parent / 'source.tif'
    source.write_bytes(LocalMinio.objects['source', 'business', 'raster/source.tif'])
    dataset = gdal.Open(str(source), gdal.GA_Update)
    if corrupt == 'pixels':
        band = dataset.GetRasterBand(2)
        band.WriteRaster(255, 255, 1, 1, struct.pack('<d', -2.), buf_type=gdal.GDT_Float64)
        band = None
    elif corrupt == 'nodata':
        dataset.GetRasterBand(1).SetNoDataValue(-1.)
    elif corrupt == 'grid':
        dataset.SetGeoTransform((109, .01, 0, 20.32, 0, -.01))
    elif corrupt == 'crs':
        crs = osr.SpatialReference(); crs.ImportFromEPSG(3857)
        dataset.SetProjection(crs.ExportToWkt())
    dataset = None
    output = config.parent / 'result.tif'
    result = gdal.Translate(str(output), str(source), format='COG' if cog else 'GTiff',
                            creationOptions=['BLOCKSIZE=128', 'COMPRESS=DEFLATE'] if cog else [])
    result = None
    raw = output.read_bytes()
    fingerprint = 'e' * 64
    key = f'tenant_42/cog/{fingerprint}/source.cog.tif'
    LocalMinio.objects['infra', 'manager', key] = raw
    request = {'tenant_id': 42, 'fingerprint': fingerprint,
               'locator': f'addp-infra://minio/manager/{key}?type=object',
               'size_bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}
    fixture.private_json(config.parent / 'manager-raster-request.json', request)
    return request, key


def test_manager_cog_physical_bytes_and_delete(manager_physical):
    request, key = manager_target(manager_physical)
    evidence = fixture.manager_worker('verify', manager_physical)
    assert evidence == {'cog_valid': True, 'source_unchanged': True, 'pixels_verified': 131072,
                        'size_bytes': request['size_bytes'], 'sha256': request['sha256']}
    with pytest.raises(fixture.FixtureError, match='still exists physically'):
        fixture.manager_worker('deleted', manager_physical)
    del LocalMinio.objects['infra', 'manager', key]
    assert fixture.manager_worker('deleted', manager_physical) == {
        'object_deleted': True, 'source_unchanged': True, 'residual_objects': 0}


@pytest.mark.parametrize('corrupt', ['pixels', 'nodata', 'grid', 'crs'])
def test_manager_cog_rejects_corrupted_raster(manager_physical, corrupt):
    manager_target(manager_physical, corrupt=corrupt)
    with pytest.raises(fixture.FixtureError, match='changed'):
        fixture.manager_worker('verify', manager_physical)


@pytest.mark.parametrize('field', ['size_bytes', 'sha256', 'locator'])
def test_manager_cog_rejects_size_hash_or_foreign_locator(manager_physical, field):
    request, _ = manager_target(manager_physical)
    request[field] = request[field] + 1 if field == 'size_bytes' else 'foreign'
    (manager_physical.parent / 'manager-raster-request.json').unlink()
    fixture.private_json(manager_physical.parent / 'manager-raster-request.json', request)
    with pytest.raises(fixture.FixtureError):
        fixture.manager_worker('verify', manager_physical)


def test_manager_cog_rejects_source_modification(manager_physical):
    manager_target(manager_physical)
    LocalMinio.objects['source', 'business', 'raster/source.tif'] += b'changed'
    with pytest.raises(fixture.FixtureError, match='source changed'):
        fixture.manager_worker('verify', manager_physical)
    with pytest.raises(fixture.FixtureError, match='source changed'):
        fixture.manager_worker('deleted', manager_physical)


def test_manager_cog_rejects_non_cog_layout(manager_physical):
    manager_target(manager_physical, cog=False)
    with pytest.raises(fixture.FixtureError, match='not a valid COG'):
        fixture.manager_worker('verify', manager_physical)


@pytest.fixture
def outside_artifacts(aggregate_artifacts, tmp_path):
    scene = importlib.import_module('scripts.test.raster-workflow-online')
    import api_server
    preserved = {key[2]: hashlib.sha256(value).hexdigest() for key, value in LocalMinio.objects.items() if key[0] == 'target'}
    result, output = single_source_execution(tmp_path, scene, api_server, 'clip-outside')
    evidence = fixture.worker('verify-clip-outside', aggregate_artifacts)
    assert evidence['size_bytes'] == result['size_bytes'] == output.stat().st_size > 0
    assert evidence['preserved_sha256'] == preserved and len(preserved) == 24
    assert evidence['band_valid_pixels'] == [53247, 53247]
    assert evidence['invalid_pixels'] == 12289
    assert evidence['color_interpretations'] == ['Gray', 'Undefined', 'Alpha']
    assert evidence['overview_sizes'] == [[128, 128]]
    assert result['width'] == result['height'] == 256 and result['band_count'] == 3
    assert result['transform'] == list(fixture.TRANSFORM)
    assert len([key for key in LocalMinio.objects if key[0] == 'target']) == 25
    return aggregate_artifacts


def test_async_outside_clipping_hole_alpha_full_grid_match_physical_oracle(outside_artifacts):
    evidence = fixture.worker('verify-clip-outside', outside_artifacts)
    assert evidence['valid_pixels'] == 53247
    pixels = list(fixture.outside_pixels('clip-outside'))
    assert pixels[0] is None
    assert pixels[80 * 256 + 80] is None
    assert pixels[120 * 256 + 120] == 120 * 256 + 121
    assert pixels[20 * 256 + 20] == 20 * 256 + 21


def test_outside_physical_oracle_rejects_hole_coverage_alpha_masks_and_preservation(outside_artifacts, tmp_path):
    baseline = dict(LocalMinio.objects)
    try:
        for fault in ('inside', 'hole', 'source-nodata', 'alpha', 'band-two', 'overview', 'mask', 'grid', 'prior', 'extra', 'source'):
            LocalMinio.objects.clear()
            LocalMinio.objects.update(baseline)
            name = 'clip-outside.cog.tif'
            if fault == 'extra':
                LocalMinio.objects['target', 'raster-target', 'partial.tif'] = b'partial'
            elif fault in ('source', 'prior'):
                key = ('source', 'raster-source', 'source.tif') if fault == 'source' else ('target', 'raster-target', 'aggregate-sum.cog.tif')
                LocalMinio.objects[key] += b'changed'
            else:
                original = tmp_path / 'outside-original.tif'
                original.write_bytes(baseline['target', 'raster-target', name])
                edited = tmp_path / 'outside-corrupt.tif'
                dataset = gdal.Translate(str(edited), str(original), format='GTiff')
                band = dataset.GetRasterBand(3 if fault == 'alpha' else 2 if fault == 'band-two' else 1)
                x, y = (80, 80) if fault == 'inside' else (120, 120) if fault == 'hole' else (0, 0) if fault == 'source-nodata' else (20, 20)
                if fault == 'mask':
                    with gdal.config_option('GDAL_TIFF_INTERNAL_MASK', 'YES'):
                        dataset.CreateMaskBand(gdal.GMF_PER_DATASET)
                    mask = bytearray([255] * (256 * 256))
                    mask[20 * 256 + 20] = 0
                    band.GetMaskBand().WriteRaster(0, 0, 256, 256, bytes(mask), buf_type=gdal.GDT_Byte)
                elif fault == 'grid':
                    dataset.SetGeoTransform((110, .02, 0, 20.32, 0, -.01))
                elif fault != 'overview':
                    value = float('nan') if fault == 'hole' else 0. if fault == 'alpha' else 123.
                    band.WriteRaster(x, y, 1, 1, struct.pack('<d', value), buf_type=gdal.GDT_Float64)
                dataset.BuildOverviews('NEAREST', [2])
                if fault == 'overview':
                    band.GetOverview(0).WriteRaster(10, 10, 1, 1, struct.pack('<d', 123.), buf_type=gdal.GDT_Float64)
                band = dataset = None
                output = gdal.Translate(str(original), str(edited), format='COG', creationOptions=['BLOCKSIZE=128'])
                output = None
                LocalMinio.objects['target', 'raster-target', name] = original.read_bytes()
            if fault == 'prior':
                evidence = fixture.worker('verify-clip-outside', outside_artifacts)
                assert evidence['preserved_sha256']['aggregate-sum.cog.tif'] != hashlib.sha256(
                    baseline['target', 'raster-target', 'aggregate-sum.cog.tif']).hexdigest()
            else:
                with pytest.raises(fixture.FixtureError):
                    fixture.worker('verify-clip-outside', outside_artifacts)
    finally:
        LocalMinio.objects.clear()
        LocalMinio.objects.update(baseline)
