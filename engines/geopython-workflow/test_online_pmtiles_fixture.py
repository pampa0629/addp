"""The Hosted physical oracle must accept genuine archives and reject corruption."""
import importlib
import json
from pathlib import Path
import sys

from osgeo import gdal
import pytest
from operators.vector_tile_operators import vector_to_pmtiles

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
fixture = importlib.import_module('business.scripts.online-vector-pmtiles-fixture')


@pytest.mark.parametrize('polygons', [False, True])
def test_online_pmtiles_native_archive(tmp_path, polygons):
    source = tmp_path/'source.geojson'
    source.write_text(json.dumps(fixture.features(polygons), ensure_ascii=False))
    if polygons:
        shp = tmp_path/'areas.shp'
        dataset = gdal.VectorTranslate(str(shp), str(source), format='ESRI Shapefile', layerCreationOptions=['ENCODING=UTF-8'])
        assert dataset is not None
        dataset = None
        source = shp
    target = tmp_path/'output.pmtiles'
    plan = {'schema_version': 'addp.workflow.access-plan/v1',
        'source': {'kind': 'file', 'format': 'shapefile' if polygons else 'geojson', 'access': {'method': 'mounted_path', 'path': str(source)}},
        'target': {'kind': 'file', 'format': 'pmtiles', 'name': target.name, 'write_mode': 'create', 'access': {'method': 'mounted_path', 'path': str(target)}}}
    tile = {'min_zoom': 0, 'max_zoom': 5, 'extent': [120,30,121.52,30.72] if polygons else [120,30,121,31], 'extent_srid': 4326, 'source_srs': 'EPSG:4326'}
    vector_to_pmtiles(plan, tile, {'layer_name': 'areas_after' if polygons else 'points_before', 'attributes': ['name','rank'] if polygons else []})
    evidence = fixture.inspect_archive(target.read_bytes(), polygons)
    assert evidence['feature_count'] == (50 if polygons else 2)
    assert evidence['min_zoom'] == 0 and evidence['max_zoom'] == 5
    with pytest.raises(fixture.shared.FixtureError):
        fixture.inspect_archive(target.read_bytes(), not polygons)
    dataset = gdal.OpenEx(str(target), gdal.OF_VECTOR)
    records = [json.loads(value.ExportToJson()) for value in dataset.GetLayerByIndex(0)]
    # Coordinates and attributes are checked independently from the product encoder.
    fixture.validate_records(records, polygons)
    with pytest.raises(fixture.shared.FixtureError): fixture.validate_records(records[:-1], polygons)
    if polygons:
        corrupted = json.loads(json.dumps(records))
        geom = corrupted[0]['geometry']
        ring = geom['coordinates'][0][0] if geom['type'] == 'MultiPolygon' else geom['coordinates'][0]
        ring[1], ring[2] = ring[2], ring[1]
        with pytest.raises(fixture.shared.FixtureError): fixture.validate_records(corrupted, True)
    records[0]['properties']['secret'] = 'leaked'
    with pytest.raises(fixture.shared.FixtureError): fixture.validate_records(records, polygons)
    records[0]['properties'].pop('secret')
    if polygons:
        records[0]['properties']['rank'] = records[1]['properties']['rank']
    else:
        records[0]['geometry']['coordinates'][0] += 10000
    with pytest.raises(fixture.shared.FixtureError): fixture.validate_records(records, polygons)
