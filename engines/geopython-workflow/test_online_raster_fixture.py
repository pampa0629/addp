"""Exercise the T4 independent byte verifier with real GDAL and local S3 transport."""
import importlib.util
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
