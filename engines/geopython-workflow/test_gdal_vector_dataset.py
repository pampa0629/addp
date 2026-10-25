import base64
from pathlib import Path

import pytest
from osgeo import gdal, ogr, osr
from shapely import from_wkb, get_srid

from operators.gdal_vector_dataset import (
    BATCH_PROTOCOL,
    DETECTION_SCHEMA,
    INSPECTION_SCHEMA,
    _authority_code,
    _feature_row,
    detect,
    inspect,
    read_batch,
    read_open,
    write_abort,
    write_batch,
    write_close,
    write_open,
)


def _source_plan(path: Path):
    return {
        "schema_version": "addp.workflow.access-plan/v1",
        "source": {
            "kind": "directory",
            "format": "filegdb",
            "access": {"method": "mounted_path", "path": str(path)},
        },
    }


def _target_plan(path: Path):
    return {
        "schema_version": "addp.workflow.access-plan/v1",
        "target": {
            "kind": "directory",
            "format": "filegdb",
            "name": path.name,
            "write_mode": "replace",
            "access": {"method": "mounted_path", "path": str(path)},
        },
    }


def _access_source_plan(path: Path):
    return {
        "schema_version": "addp.workflow.access-plan/v1",
        "source": {
            "kind": "file",
            "format": "access",
            "access": {"method": "mounted_path", "path": str(path)},
        },
    }


def test_detect_refines_access_candidate_to_pgeo(tmp_path, monkeypatch):
    source = tmp_path / "source.mdb"
    source.write_bytes(b"mdb")

    class FakeDriver:
        def GetDescription(self):
            return "PGeo"

    class FakeDataset:
        def GetDriver(self):
            return FakeDriver()

    monkeypatch.setattr("operators.gdal_vector_dataset._required_driver", lambda *args, **kwargs: object())
    monkeypatch.setattr("operators.gdal_vector_dataset.gdal.OpenEx", lambda *args, **kwargs: FakeDataset())
    result = detect(_access_source_plan(source))

    assert result == {
        "schema_version": DETECTION_SCHEMA,
        "candidate_format": "access",
        "format": "pgeo",
        "evidence": {"driver": "PGeo"},
    }


def test_detect_keeps_non_pgeo_mdb_as_access(tmp_path, monkeypatch):
    source = tmp_path / "source.mdb"
    source.write_bytes(b"mdb")

    def reject_pgeo(*args, **kwargs):
        raise RuntimeError("not a Personal Geodatabase")

    monkeypatch.setattr("operators.gdal_vector_dataset._required_driver", lambda *args, **kwargs: object())
    monkeypatch.setattr("operators.gdal_vector_dataset.gdal.OpenEx", reject_pgeo)
    result = detect(_access_source_plan(source))

    assert result["schema_version"] == DETECTION_SCHEMA
    assert result["candidate_format"] == "access"
    assert result["format"] == "access"
    assert result["evidence"] == {"driver": ""}


def _create_source(path: Path):
    driver = gdal.GetDriverByName("OpenFileGDB")
    if driver is None or driver.GetMetadataItem(gdal.DCAP_CREATE) != "YES":
        pytest.skip("writable OpenFileGDB driver is unavailable")
    dataset = driver.Create(str(path), 0, 0, 0, gdal.GDT_Unknown)
    spatial_reference = osr.SpatialReference()
    spatial_reference.ImportFromEPSG(4326)
    layer = dataset.CreateLayer("regions", srs=spatial_reference, geom_type=ogr.wkbMultiPolygon)
    layer.CreateField(ogr.FieldDefn("id", ogr.OFTInteger))
    layer.CreateField(ogr.FieldDefn("name", ogr.OFTString))
    for row_id, x in ((1, 0), (2, 2)):
        feature = ogr.Feature(layer.GetLayerDefn())
        feature.SetField("id", row_id)
        feature.SetField("name", f"region-{row_id}")
        geometry = ogr.CreateGeometryFromWkt(
            f"MULTIPOLYGON ((({x} 0, {x + 1} 0, {x + 1} 1, {x} 1, {x} 0)))"
        )
        geometry.AssignSpatialReference(spatial_reference)
        feature.SetGeometry(geometry)
        assert layer.CreateFeature(feature) == ogr.OGRERR_NONE
    dataset = None


def test_filegdb_stateless_batch_round_trip(tmp_path):
    source = tmp_path / "source.gdb"
    target = tmp_path / "target.gdb"
    _create_source(source)

    opened = read_open(BATCH_PROTOCOL, _source_plan(source), "regions")
    assert opened["row_count"] == 2
    assert opened["spatial"]["srid"] == 4326
    assert opened["spatial"]["geometry_columns"][0]["geometry_type"] == "MultiPolygon"

    rows = read_batch(BATCH_PROTOCOL, _source_plan(source), "regions", 0, 10)["rows"]
    assert [row["id"] for row in rows] == [1, 2]
    geometry = from_wkb(base64.b64decode(rows[0]["SHAPE"]))
    assert get_srid(geometry) == 4326
    assert read_batch(BATCH_PROTOCOL, _source_plan(source), "regions", 2, 10)["rows"] == []

    write_open(BATCH_PROTOCOL, _target_plan(target), "regions", opened["fields"], opened["spatial"])
    write_batch(BATCH_PROTOCOL, _target_plan(target), "regions", opened["fields"], opened["spatial"], 0, rows[:1])
    write_batch(BATCH_PROTOCOL, _target_plan(target), "regions", opened["fields"], opened["spatial"], 1, rows[1:])
    closed = write_close(BATCH_PROTOCOL, _target_plan(target), "regions", opened["fields"], opened["spatial"], 2)
    assert closed["row_count"] == 2

    reopened = read_open(BATCH_PROTOCOL, _source_plan(target), "regions")
    assert reopened["row_count"] == 2
    assert reopened["spatial"]["srid"] == 4326


def test_filegdb_writer_maps_source_objectid_to_system_fid(tmp_path):
    target = tmp_path / "objectid.gdb"
    fields = [
        {"name": "OBJECTID", "type": "int", "nullable": False},
        {"name": "name", "type": "string", "nullable": True},
        {"name": "Shape", "type": "geometry", "nullable": True},
    ]
    spatial = {
        "geometry_columns": [{
            "name": "Shape",
            "geometry_type": "Point",
            "dimension": 2,
            "srid": 4326,
        }],
        "primary_geometry_column": "Shape",
        "srid": 4326,
    }
    point = ogr.CreateGeometryFromWkt("POINT (116.397 39.908)")
    row = {
        "OBJECTID": 7,
        "name": "beijing",
        "Shape": base64.b64encode(point.ExportToWkb()).decode("ascii"),
    }

    write_open(BATCH_PROTOCOL, _target_plan(target), "points", fields, spatial)
    write_batch(BATCH_PROTOCOL, _target_plan(target), "points", fields, spatial, 0, [row])
    write_close(BATCH_PROTOCOL, _target_plan(target), "points", fields, spatial, 1)

    dataset = gdal.OpenEx(str(target), gdal.OF_VECTOR, allowed_drivers=["OpenFileGDB"])
    layer = dataset.GetLayerByName("points")
    assert layer.GetFIDColumn() == "OBJECTID"
    assert layer.GetLayerDefn().GetFieldIndex("OBJECTID") == -1
    feature = layer.GetFeature(7)
    assert feature is not None
    assert feature.GetField("name") == "beijing"
    dataset = None

    reopened = read_open(BATCH_PROTOCOL, _source_plan(target), "points")
    assert reopened["fields"][0] == {
        "name": "OBJECTID",
        "type": "int",
        "native_type": "FID",
        "nullable": False,
        "ordinal_position": 1,
    }
    reopened_rows = read_batch(BATCH_PROTOCOL, _source_plan(target), "points", 0, 10)["rows"]
    assert reopened_rows[0]["OBJECTID"] == 7


def test_filegdb_inspect_returns_lightweight_container_children(tmp_path):
    source = tmp_path / "source.gdb"
    _create_source(source)

    result = inspect(_source_plan(source), child_limit=10)

    assert result["schema_version"] == INSPECTION_SCHEMA
    assert result["format"] == "filegdb"
    assert result["container"]["child_count"] == 1
    assert result["container"]["default_child"] == "regions"
    child = result["container"]["children"][0]
    assert child == {
        "name": "regions",
        "child_kind": "feature_class",
        "data_type": "table",
        "row_count": 2,
        "column_count": 4,
        "native": {
            "table": "regions",
            "geometry_column": "SHAPE",
            "geometry_type": "MultiPolygon",
            "dimension": 2,
            "srid": 4326,
        },
    }
    assert "fields" not in child
    assert result["format_info"]["driver"] == "OpenFileGDB"


def test_filegdb_inspect_empty_container_has_no_children(tmp_path):
    source = tmp_path / "empty.gdb"
    driver = gdal.GetDriverByName("OpenFileGDB")
    if driver is None or driver.GetMetadataItem(gdal.DCAP_CREATE) != "YES":
        pytest.skip("writable OpenFileGDB driver is unavailable")
    dataset = driver.Create(str(source), 0, 0, 0, gdal.GDT_Unknown)
    assert dataset is not None
    dataset = None

    result = inspect(_source_plan(source))

    assert result["container"] == {
        "child_count": 0,
        "default_child": "",
        "resource_count": 1,
        "children": [],
    }
    assert result["format_info"]["driver"] == "OpenFileGDB"
    assert result["format_info"]["layer_count"] == 0


def test_filegdb_inspect_rejects_invalid_container(tmp_path):
    source = tmp_path / "invalid.gdb"
    source.mkdir()
    (source / "garbage").write_text("not a FileGDB")

    with pytest.raises(RuntimeError):
        inspect(_source_plan(source))


def test_filegdb_inspect_attribute_table_has_no_geometry_column(tmp_path):
    source = tmp_path / "attributes.gdb"
    driver = gdal.GetDriverByName("OpenFileGDB")
    if driver is None or driver.GetMetadataItem(gdal.DCAP_CREATE) != "YES":
        pytest.skip("writable OpenFileGDB driver is unavailable")
    dataset = driver.Create(str(source), 0, 0, 0, gdal.GDT_Unknown)
    layer = dataset.CreateLayer("codes", geom_type=ogr.wkbNone)
    layer.CreateField(ogr.FieldDefn("code", ogr.OFTString))
    dataset = None

    result = inspect(_source_plan(source))

    child = result["container"]["children"][0]
    assert child["child_kind"] == "table"
    assert child["column_count"] == 2
    assert child["native"] == {"table": "codes"}
    opened = read_open(BATCH_PROTOCOL, _source_plan(source), "codes")
    assert opened["spatial"] == {}
    assert opened["fields"] == [
        {
            "name": "OBJECTID",
            "type": "int",
            "native_type": "FID",
            "nullable": False,
            "ordinal_position": 1,
        },
        {
            "name": "code",
            "type": "string",
            "native_type": "String",
            "nullable": True,
            "ordinal_position": 2,
        },
    ]


def test_inspect_skips_unreadable_dataset_layers(monkeypatch):
    source = Path("/virtual/source.mdb")

    class FakeLayer:
        def __init__(self, name, row_count=None):
            self.name = name
            self.row_count = row_count

        def GetName(self):
            return self.name

        def GetFeatureCount(self, force=1):
            del force
            if self.row_count is None:
                raise RuntimeError("saved query is not readable")
            return self.row_count

    class FakeDriver:
        def GetDescription(self):
            return "PGeo"

    class FakeDataset:
        layers = [FakeLayer("invalid"), FakeLayer("first", 2), FakeLayer("second", 3)]

        def GetLayerCount(self):
            return len(self.layers)

        def GetLayerByIndex(self, index):
            return self.layers[index]

        def GetDriver(self):
            return FakeDriver()

    class FakeOpenedSource:
        empty_filegdb = False

        def __init__(self, plan, *, inspect_empty_filegdb=False):
            del plan
            assert inspect_empty_filegdb

        def __enter__(self):
            return FakeDataset()

        def __exit__(self, exc_type, exc_value, traceback):
            del exc_type, exc_value, traceback

    monkeypatch.setattr("operators.gdal_vector_dataset._opened_source", FakeOpenedSource)
    monkeypatch.setattr("operators.gdal_vector_dataset._describe_layer", lambda layer, **kwargs: ([{
        "name": f"{layer.name}_id",
        "type": "int",
    }], {}))

    result = inspect({
        "schema_version": "addp.workflow.access-plan/v1",
        "source": {
            "kind": "file",
            "format": "pgeo",
            "access": {"method": "mounted_path", "path": str(source)},
        },
    }, child_limit=1)

    assert result["container"]["child_count"] == 2
    assert result["container"]["default_child"] == "first"
    assert [child["name"] for child in result["container"]["children"]] == ["first"]
    assert result["format_info"]["layer_count"] == 3
    assert result["format_info"]["skipped_layer_count"] == 1
    assert result["format_info"]["children_truncated"] is True


def test_authority_code_accepts_only_exact_epsg_match():
    class Match:
        def __init__(self, authority, code):
            self.authority = authority
            self.code = code

        def GetAuthorityName(self, target):
            del target
            return self.authority

        def GetAuthorityCode(self, target):
            del target
            return self.code

    class SpatialReference:
        def AutoIdentifyEPSG(self):
            raise RuntimeError("root authority is unavailable")

        def GetAuthorityCode(self, target):
            del target
            return None

        def FindMatches(self):
            return [(Match("EPSG", "26729"), 25), (Match("EPSG", "4326"), 100)]

    assert _authority_code(SpatialReference()) == 4326


def test_feature_row_normalizes_gdal_missing_geometry_sentinel():
    class FakeFeature:
        def GetGeometryRef(self):
            geometry = ogr.Geometry(ogr.wkbPoint)
            geometry.AddPoint_2D(-float("1.7976931348623157e+308"), -float("1.7976931348623157e+308"))
            return geometry

    assert _feature_row(FakeFeature(), [], "SHAPE", 4326) == {"SHAPE": None}


def test_filegdb_abort_removes_partial_scope(tmp_path):
    target = tmp_path / "partial.gdb"
    fields = [{"name": "id", "type": "int", "nullable": False}]
    write_open(BATCH_PROTOCOL, _target_plan(target), "items", fields, None)
    assert target.is_dir()
    write_abort(BATCH_PROTOCOL, _target_plan(target), "items", fields, None)
    assert not target.exists()


def _exchange_plan(path, format_name):
    return {"schema_version": "addp.workflow.access-plan/v1", "source": {
        "kind": "file", "format": format_name,
        "access": {"method": "mounted_path", "path": str(path)},
    }}


def test_registered_native_filegdb_batch_commit_error_and_abort(tmp_path):
    """The public registry must preserve batch state in files across processes."""
    from workflow_engine import execute_single_operator

    def invoke(operation, **params):
        return execute_single_operator('vector_dataset.' + operation, params)

    target = tmp_path / 'isolated.gdb'
    plan = _target_plan(target)
    fields = [{'name': 'name', 'type': 'string', 'nullable': False}]
    shared = dict(protocol=BATCH_PROTOCOL, access_plan=plan, layer='sites', fields=fields, spatial=None)
    assert invoke('write_open', **shared)['status'] == 'success'
    assert invoke('write_batch', **shared, offset=0, rows=[{'name': '长沙'}])['status'] == 'success'
    failed = invoke('write_batch', **shared, offset=0, rows=[{'name': '重复'}])
    assert failed['status'] == 'failed' and 'offset' in failed['error']
    assert invoke('write_close', **shared, expected_row_count=1)['status'] == 'success'
    read = dict(protocol=BATCH_PROTOCOL, access_plan=_source_plan(target), layer='sites')
    assert invoke('read_open', **read)['result']['row_count'] == 1
    assert invoke('read_batch', **read, offset=0, limit=10)['result']['rows'][0]['name'] == '长沙'
    assert invoke('write_abort', **shared)['status'] == 'success'
    assert not target.exists()


def test_native_process_keeps_access_plan_out_of_arguments(monkeypatch):
    import json
    import subprocess
    from operators import get_operator

    plan = {'source': {'access': {'secret_key': 'private-test-secret'}}}
    calls = []

    def run(command, **kwargs):
        calls.append(command)
        assert 'private-test-secret' not in ' '.join(command)
        assert json.loads(kwargs['input'])['params']['access_plan'] == plan
        return subprocess.CompletedProcess(command, 0, stdout='{"result": {"ok": true}}')

    from operators import _native_process
    monkeypatch.setattr(_native_process, 'run', run)
    assert get_operator('vector_dataset.inspect')(access_plan=plan) == {'ok': True}
    assert len(calls) == 1


def test_kml_multilayer_geometry_and_pagination(tmp_path):
    source = tmp_path / "sites.kml"
    source.write_text('''<?xml version="1.0"?><kml xmlns="http://www.opengis.net/kml/2.2"><Document>
      <Folder><name>sites</name>
       <Placemark><name>长沙</name><Point><coordinates>112.98,28.19,8</coordinates></Point></Placemark>
       <Placemark><name>株洲</name><Point><coordinates>113.13,27.83,9</coordinates></Point></Placemark>
      </Folder><Folder><name>roads</name><Placemark><name>route</name>
       <LineString><coordinates>112.98,28.19 113.13,27.83</coordinates></LineString>
      </Placemark></Folder></Document></kml>''')
    original = source.read_bytes()
    plan = _exchange_plan(source, "kml")
    result = inspect(plan)
    assert {child["name"] for child in result["container"]["children"]} == {"sites", "roads"}
    assert result["format_info"]["driver"] == "KML"
    opened = read_open(BATCH_PROTOCOL, plan, "sites")
    assert opened["row_count"] == 2
    assert opened["spatial"]["srid"] == 4326
    first = read_batch(BATCH_PROTOCOL, plan, "sites", 0, 1)["rows"][0]
    second = read_batch(BATCH_PROTOCOL, plan, "sites", 1, 1)["rows"][0]
    assert first["Name"] == "长沙" and second["Name"] == "株洲"
    geom_field = opened["spatial"]["primary_geometry_column"]
    point = from_wkb(base64.b64decode(first[geom_field]))
    assert get_srid(point) == 4326
    assert list(point.coords) == [(112.98, 28.19, 8.0)]
    assert read_batch(BATCH_PROTOCOL, plan, "sites", 2, 1)["rows"] == []
    assert source.read_bytes() == original and list(tmp_path.iterdir()) == [source]


def test_gml_standard_latitude_longitude_axis_and_isolated_sidecars(tmp_path):
    source = tmp_path / "sites.gml"
    source.write_text('''<gml:FeatureCollection xmlns:gml="http://www.opengis.net/gml" xmlns:app="urn:addp:test">
      <gml:featureMember><app:sites gml:id="p1"><app:name>长沙</app:name><app:position>
       <gml:Point srsName="urn:ogc:def:crs:EPSG::4326"><gml:pos>28.19 112.98</gml:pos></gml:Point>
      </app:position></app:sites></gml:featureMember>
      <gml:featureMember><app:roads gml:id="r1"><app:name>route</app:name><app:position>
       <gml:LineString srsName="urn:ogc:def:crs:EPSG::4326"><gml:posList>28.19 112.98 27.83 113.13</gml:posList></gml:LineString>
      </app:position></app:roads></gml:featureMember>
     </gml:FeatureCollection>''')
    # Adjacent schemas must not influence the source-only access contract.
    schema = tmp_path / "sites.xsd"
    schema.write_text("not a schema")
    plan = _exchange_plan(source, "gml")
    result = inspect(plan)
    assert {child["name"] for child in result["container"]["children"]} == {"sites", "roads"}
    opened = read_open(BATCH_PROTOCOL, plan, "sites")
    assert opened["spatial"]["srid"] == 4326
    row = read_batch(BATCH_PROTOCOL, plan, "sites", 0, 1)["rows"][0]
    point = from_wkb(base64.b64decode(row[opened["spatial"]["primary_geometry_column"]]))
    assert list(point.coords) == [(112.98, 28.19)]
    assert row["name"] == "长沙"
    assert set(tmp_path.iterdir()) == {source, schema}


def test_topojson_quantization_reverse_arcs_and_unknown_crs(tmp_path):
    source = tmp_path / "projected.topojson"
    source.write_text('''{"type":"Topology","transform":{"scale":[10,20],"translate":[500000,3000000]},
     "objects":{
      "routes":{"type":"GeometryCollection","geometries":[
       {"type":"LineString","properties":{"name":"forward"},"arcs":[0]},
       {"type":"LineString","properties":{"name":"reverse"},"arcs":[-1]}]},
      "sites":{"type":"GeometryCollection","geometries":[{"type":"Point","coordinates":[2,3],"properties":{"name":"station"}}]}},
     "arcs":[[[0,0],[1,2],[2,1]]]}''')
    plan = _exchange_plan(source, "topojson")
    result = inspect(plan)
    assert {child["name"] for child in result["container"]["children"]} == {"routes", "sites"}
    assert all("srid" not in child["native"] for child in result["container"]["children"])
    opened = read_open(BATCH_PROTOCOL, plan, "routes")
    assert not opened["spatial"].get("srid")
    assert not opened["spatial"].get("crs_ref")
    field = opened["spatial"]["primary_geometry_column"]
    rows = read_batch(BATCH_PROTOCOL, plan, "routes", 0, 10)["rows"]
    forward, reverse = [from_wkb(base64.b64decode(row[field])) for row in rows]
    assert get_srid(forward) == 0
    assert list(forward.coords) == [(500000,3000000),(500010,3000040),(500030,3000060)]
    assert list(reverse.coords) == list(forward.coords)[::-1]
    points = read_batch(BATCH_PROTOCOL, plan, "sites", 0, 1)["rows"]
    assert list(from_wkb(base64.b64decode(points[0][field])).coords) == [(500020,3000060)]


@pytest.mark.parametrize("source_format,content", [
    ("kml", '<!DOCTYPE kml [<!ENTITY x SYSTEM "file:///etc/passwd">]><kml>&x;</kml>'),
    ("kml", '<kml><NetworkLink><Link><href>https://example.invalid/data.kml</href></Link></NetworkLink></kml>'),
    ("kml", '<kml><Placemark><Model/></Placemark></kml>'),
    ("kml", '<kml><GroundOverlay/></kml>'),
    ("gml", '<root xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="urn:test file:///outside/test.xsd"/>'),
    ("gml", '<root xmlns:xlink="http://www.w3.org/1999/xlink" xlink:href="#missing"/>'),
    ("gml", '<root srsName="file:///outside/secret.prj"/>'),
    ("gml", '<root srsName="https://example.invalid/custom-crs.wkt"/>'),
])
def test_vector_xml_rejects_dependencies_before_gdal_open(tmp_path, monkeypatch, source_format, content):
    source = tmp_path / ("unsafe." + source_format)
    source.write_text(content)
    def unexpected_open(*args, **kwargs):
        pytest.fail("GDAL must not open rejected XML")
    monkeypatch.setattr("operators.gdal_vector_dataset.gdal.OpenEx", unexpected_open)
    with pytest.raises(ValueError, match="unsupported"):
        inspect(_exchange_plan(source, source_format))
    assert list(tmp_path.iterdir()) == [source]


def test_topojson_geometry_field_does_not_overwrite_named_attribute(tmp_path):
    source = tmp_path / "sites.topojson"
    source.write_text('''{"type":"Topology","objects":{"sites":{"type":"GeometryCollection","geometries":[
      {"type":"Point","coordinates":[112.98,28.19],"properties":{"geometry":"keep this attribute"}}]}},"arcs":[]}''')
    plan = _exchange_plan(source, "topojson")
    opened = read_open(BATCH_PROTOCOL, plan, "sites")
    field = opened["spatial"]["primary_geometry_column"]
    assert field != "geometry"
    row = read_batch(BATCH_PROTOCOL, plan, "sites", 0, 1)["rows"][0]
    assert row["geometry"] == "keep this attribute"
    assert list(from_wkb(base64.b64decode(row[field])).coords) == [(112.98,28.19)]


def test_gml_without_srs_preserves_coordinates_without_guessing_crs(tmp_path):
    source = tmp_path / "local.gml"
    source.write_text('''<gml:FeatureCollection xmlns:gml="http://www.opengis.net/gml" xmlns:app="urn:test">
      <gml:featureMember><app:sites><app:position><gml:Point><gml:pos>500000 3000000</gml:pos></gml:Point></app:position></app:sites></gml:featureMember>
      </gml:FeatureCollection>''')
    plan = _exchange_plan(source, "gml")
    opened = read_open(BATCH_PROTOCOL, plan, "sites")
    assert not opened["spatial"].get("srid") and not opened["spatial"].get("crs_ref")
    row = read_batch(BATCH_PROTOCOL, plan, "sites", 0, 1)["rows"][0]
    point = from_wkb(base64.b64decode(row[opened["spatial"]["primary_geometry_column"]]))
    assert get_srid(point) == 0
    assert list(point.coords) == [(500000,3000000)]


def test_exchange_object_store_uses_exact_file_without_listing(tmp_path, monkeypatch):
    source_data = b'<kml xmlns="http://www.opengis.net/kml/2.2"><Document><Folder><name>sites</name><Placemark><Point><coordinates>112.98,28.19</coordinates></Point></Placemark></Folder></Document></kml>'
    downloads = []
    class Client:
        def fget_object(self, bucket, object_name, path):
            downloads.append((bucket, object_name))
            Path(path).write_bytes(source_data)
    monkeypatch.setattr("addp_common.workflow_access._object_store_client", lambda access: Client())
    plan = {"schema_version":"addp.workflow.access-plan/v1", "source":{
        "kind":"file", "format":"kml", "access":{
            "method":"object_store", "endpoint":"storage.invalid:9000", "access_key":"test", "secret_key":"test",
            "bucket":"business", "object":"models/sites.kml"}}}
    inspected = inspect(plan)
    assert inspected["container"]["default_child"] == "sites"
    assert read_open(BATCH_PROTOCOL, plan, "sites")["spatial"]["srid"] == 4326
    assert len(read_batch(BATCH_PROTOCOL, plan, "sites", 0, 1)["rows"]) == 1
    assert downloads == [("business", "models/sites.kml")]*3


@pytest.mark.parametrize("geometry,arcs", [
    ({"type":"LineString", "arcs":[9]}, [[[1,2],[3,4]]]),
    ({"type":"LineString", "arcs":[-2]}, [[[1,2],[3,4]]]),
    ({"type":"Point", "coordinates":[1,2,3]}, []),
    ({"type":"Point", "coordinates":[True,2]}, []),
    ({"type":"Unknown", "coordinates":[1,2]}, []),
    ({"type":"GeometryCollection", "geometries":[{"type":"GeometryCollection", "geometries":[]}]}, []),
    (None, []),
])
def test_topojson_rejects_lossy_driver_inputs(tmp_path, monkeypatch, geometry, arcs):
    import json
    source = tmp_path / "invalid.topojson"
    source.write_text(json.dumps({"type":"Topology", "objects":{"sample":geometry},"arcs":arcs}))
    def unexpected_open(*args, **kwargs):
        pytest.fail("GDAL must not silently discard unsupported source geometry")
    monkeypatch.setattr("operators.gdal_vector_dataset.gdal.OpenEx", unexpected_open)
    with pytest.raises(ValueError):
        inspect(_exchange_plan(source, "topojson"))


def test_gml_schema_annotation_does_not_fetch_external_schema(tmp_path, monkeypatch):
    source = tmp_path / "sites.gml"
    source.write_text('''<gml:FeatureCollection xmlns:gml="http://www.opengis.net/gml" xmlns:app="urn:test"
       xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="urn:test https://example.invalid/test.xsd">
      <gml:featureMember><app:sites><app:name>station</app:name><app:position>
       <gml:Point srsName="EPSG:4326"><gml:pos>112.98 28.19</gml:pos></gml:Point>
      </app:position></app:sites></gml:featureMember></gml:FeatureCollection>''')
    original_open = gdal.OpenEx
    calls = []
    def controlled_open(*args, **kwargs):
        calls.append(kwargs)
        assert kwargs["allowed_drivers"] == ["GML"]
        assert kwargs["open_options"] == ["DOWNLOAD_SCHEMA=NO", "USE_SCHEMA_IMPORT=NO"]
        return original_open(*args, **kwargs)
    monkeypatch.setattr("operators.gdal_vector_dataset.gdal.OpenEx", controlled_open)
    result = inspect(_exchange_plan(source, "gml"))
    assert result["container"]["default_child"] == "sites"
    assert calls and list(tmp_path.iterdir()) == [source]


def test_kml_common_writer_golden_is_readable_by_native_driver():
    """Go T1 checks exact writer bytes against this same independently read file."""
    fixture = Path(__file__).resolve().parents[2] / "common/format/plugins/kml/testdata/simple-features.kml"
    # Assert the writer's sole XML representation before independent native read.
    # GDAL 3.8 LIBKML parses bool SchemaData through an integer field; the XML
    # boolean literals 1/0 work without changing the declared bool type.
    import xml.etree.ElementTree as ET
    root = ET.fromstring(fixture.read_bytes())
    ns = {"k": "http://www.opengis.net/kml/2.2"}
    flag_field = root.find(".//k:SimpleField[@name='flag']", ns)
    assert flag_field is not None and flag_field.attrib["type"] == "bool"
    assert [value.text for value in root.findall(".//k:SimpleData[@name='flag']", ns)] == ["1", "0"] * 4
    dataset = gdal.OpenEx(str(fixture), gdal.OF_VECTOR, allowed_drivers=["LIBKML"])
    assert dataset is not None
    assert dataset.GetLayerCount() == 1
    layer = dataset.GetLayerByIndex(0)
    assert layer.GetFeatureCount(force=1) == 8
    assert layer.GetSpatialRef().GetAuthorityCode(None) == "4326"
    expected = [
        "POINT (120 30)", "LINESTRING (120 30,121 31)",
        "POLYGON ((0 0,4 0,4 4,0 4,0 0),(1 1,1 2,2 2,2 1,1 1))",
        "MULTIPOINT ((1 2),(3 4))", "MULTILINESTRING ((1 2,3 4),(5 6,7 8))",
        "MULTIPOLYGON (((0 0,1 0,1 1,0 0)))",
        "GEOMETRYCOLLECTION (POINT (1 2),LINESTRING (3 4,5 6))", None,
    ]
    for index, wkt in enumerate(expected):
        feature = layer.GetNextFeature()
        assert feature is not None
        assert feature.GetField("seq") == index
        assert feature.GetField("measure") == 1.25
        assert feature.GetField("flag") == int(index % 2 == 0)
        if index == 2:
            assert not feature.IsFieldSetAndNotNull(feature.GetFieldIndex("标题"))
        else:
            assert feature.GetField("标题") == ("" if index == 1 else '中文 <&> "quote"')
        geometry = feature.GetGeometryRef()
        if wkt is None:
            assert geometry is None
        else:
            assert geometry is not None
            assert geometry.Equals(ogr.CreateGeometryFromWkt(wkt)), geometry.ExportToWkt()
    assert layer.GetNextFeature() is None
