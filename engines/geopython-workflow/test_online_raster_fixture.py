"""Exercise the T4 independent byte verifier with real GDAL and local S3 transport."""
import importlib.util
import json
from pathlib import Path
import struct
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
