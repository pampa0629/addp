"""Opt-in acceptance checks for real Access and Personal Geodatabase samples."""

from __future__ import annotations

import base64
import os
from pathlib import Path
import unittest

from osgeo import gdal
from shapely import from_wkb, get_srid

from operators.gdal_vector_dataset import (
    BATCH_PROTOCOL,
    detect,
    inspect,
    read_batch,
    read_open,
)


ONLINE_FLAG = "ADDP_ARCGIS_RUNTIME_ONLINE"
PGEO_FIXTURE = "ADDP_ARCGIS_PGEO_FIXTURE"
ACCESS_FIXTURE = "ADDP_ARCGIS_ACCESS_FIXTURE"
MATRIX_FIXTURE = "ADDP_ARCGIS_PGEO_MATRIX_FIXTURE"


def _source_plan(path: Path, source_format: str) -> dict:
    return {
        "schema_version": "addp.workflow.access-plan/v1",
        "source": {
            "kind": "file",
            "format": source_format,
            "access": {"method": "mounted_path", "path": str(path)},
        },
    }


def _required_fixture(name: str) -> Path:
    value = os.environ.get(name, "").strip()
    if not value:
        raise AssertionError(f"{name} is required")
    path = Path(value)
    if not path.is_file():
        raise AssertionError(f"fixture does not exist: {path}")
    return path


@unittest.skipUnless(os.environ.get(ONLINE_FLAG) == "1", f"set {ONLINE_FLAG}=1")
class RealMDBFormatAcceptanceTest(unittest.TestCase):
    def test_pgeo_identity_catalog_and_rows(self):
        path = _required_fixture(PGEO_FIXTURE)
        self.assertIsNotNone(gdal.GetDriverByName("PGeo"))
        self.assertIsNotNone(gdal.GetDriverByName("ODBC"))

        detection = detect(_source_plan(path, "access"))
        self.assertEqual(detection["candidate_format"], "access")
        self.assertEqual(detection["format"], "pgeo")
        self.assertEqual(detection["evidence"], {"driver": "PGeo"})

        pgeo_plan = _source_plan(path, "pgeo")
        inspected = inspect(pgeo_plan, child_limit=100)
        self.assertEqual(inspected["format"], "pgeo")
        self.assertEqual(inspected["format_info"]["driver"], "PGeo")
        self.assertEqual(inspected["container"]["child_count"], 25)

        children = {child["name"]: child for child in inspected["container"]["children"]}
        points = children["WGS84_Points"]
        self.assertEqual(points["child_kind"], "feature_class")
        self.assertEqual(points["row_count"], 265)
        self.assertEqual(points["column_count"], 26)
        self.assertEqual(points["native"]["geometry_type"], "Point")
        self.assertEqual(points["native"]["dimension"], 2)
        self.assertEqual(points["native"]["srid"], 4326)

        opened = read_open(BATCH_PROTOCOL, pgeo_plan, "WGS84_Points")
        self.assertEqual(opened["row_count"], 265)
        self.assertEqual(opened["spatial"]["primary_geometry_column"], "Shape")
        self.assertEqual(opened["spatial"]["srid"], 4326)
        self.assertEqual(opened["spatial"]["geometry_columns"][0]["geometry_type"], "Point")

        first = read_batch(BATCH_PROTOCOL, pgeo_plan, "WGS84_Points", 0, 1)["rows"]
        self.assertEqual(len(first), 1)
        self.assertIsNone(first[0]["Shape"])
        first_non_null = read_batch(BATCH_PROTOCOL, pgeo_plan, "WGS84_Points", 1, 1)["rows"]
        self.assertEqual(len(first_non_null), 1)
        geometry = from_wkb(base64.b64decode(first_non_null[0]["Shape"]))
        self.assertEqual(geometry.geom_type, "Point")
        self.assertEqual(get_srid(geometry), 4326)
        all_rows = read_batch(BATCH_PROTOCOL, pgeo_plan, "WGS84_Points", 0, 265)["rows"]
        self.assertEqual(sum(row["Shape"] is not None for row in all_rows), 250)
        self.assertEqual(len(read_batch(BATCH_PROTOCOL, pgeo_plan, "WGS84_Points", 264, 2)["rows"]), 1)
        self.assertEqual(read_batch(BATCH_PROTOCOL, pgeo_plan, "WGS84_Points", 265, 1)["rows"], [])

    def test_plain_access_is_not_promoted_to_pgeo(self):
        path = _required_fixture(ACCESS_FIXTURE)
        detection = detect(_source_plan(path, "access"))
        self.assertEqual(detection["candidate_format"], "access")
        self.assertEqual(detection["format"], "access")
        self.assertEqual(detection["evidence"], {"driver": ""})

    def test_pgeo_multigeometry_catalog_and_rows(self):
        path = _required_fixture(MATRIX_FIXTURE)
        detection = detect(_source_plan(path, "access"))
        self.assertEqual(detection["format"], "pgeo")

        pgeo_plan = _source_plan(path, "pgeo")
        inspected = inspect(pgeo_plan, child_limit=100)
        children = {child["name"]: child for child in inspected["container"]["children"]}
        self.assertEqual(children["Fault"]["native"]["geometry_type"], "MultiLineString")
        self.assertEqual(children["Loess"]["native"]["geometry_type"], "MultiPolygon")

        for layer, geometry_type in (("Fault", "MultiLineString"), ("Loess", "MultiPolygon")):
            opened = read_open(BATCH_PROTOCOL, pgeo_plan, layer)
            self.assertEqual(opened["spatial"]["geometry_columns"][0]["geometry_type"], geometry_type)
            geometry_field = opened["spatial"]["primary_geometry_column"]
            rows = read_batch(BATCH_PROTOCOL, pgeo_plan, layer, 0, 2)["rows"]
            non_null = next(row for row in rows if row.get(geometry_field) is not None)
            geometry = from_wkb(base64.b64decode(non_null[geometry_field]))
            self.assertEqual(geometry.geom_type, geometry_type)
            self.assertEqual(get_srid(geometry), 0)


if __name__ == "__main__":
    unittest.main(verbosity=2)
