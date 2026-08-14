import base64
from pathlib import Path

import pytest
from osgeo import gdal, ogr, osr
from shapely import from_wkb, get_srid

from operators.gdal_vector_dataset import (
    BATCH_PROTOCOL,
    INSPECTION_SCHEMA,
    _authority_code,
    _feature_row,
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

    write_open(BATCH_PROTOCOL, _target_plan(target), "regions", opened["fields"], opened["spatial"])
    write_batch(BATCH_PROTOCOL, _target_plan(target), "regions", opened["fields"], opened["spatial"], 0, rows[:1])
    write_batch(BATCH_PROTOCOL, _target_plan(target), "regions", opened["fields"], opened["spatial"], 1, rows[1:])
    closed = write_close(BATCH_PROTOCOL, _target_plan(target), "regions", opened["fields"], opened["spatial"], 2)
    assert closed["row_count"] == 2

    reopened = read_open(BATCH_PROTOCOL, _source_plan(target), "regions")
    assert reopened["row_count"] == 2
    assert reopened["spatial"]["srid"] == 4326


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
        "column_count": 3,
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
    assert child["column_count"] == 1
    assert child["native"] == {"table": "codes"}
    opened = read_open(BATCH_PROTOCOL, _source_plan(source), "codes")
    assert opened["spatial"] == {}
    assert opened["fields"] == [{
        "name": "code",
        "type": "string",
        "native_type": "String",
        "nullable": True,
        "ordinal_position": 1,
    }]


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
        def __init__(self, plan):
            del plan

        def __enter__(self):
            return FakeDataset()

        def __exit__(self, exc_type, exc_value, traceback):
            del exc_type, exc_value, traceback

    monkeypatch.setattr("operators.gdal_vector_dataset._opened_source", FakeOpenedSource)
    monkeypatch.setattr("operators.gdal_vector_dataset._describe_layer", lambda layer: ([{
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
