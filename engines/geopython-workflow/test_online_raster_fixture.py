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
        assert fixture.worker('seed', config)['seeded']
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
