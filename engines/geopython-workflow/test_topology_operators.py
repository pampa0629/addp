"""Native planar linework and existing multiport persistence contracts."""

import json
import time

import geopandas as gpd
from geopandas.testing import assert_geodataframe_equal
import numpy as np
import pandas as pd
import pytest
import shapely
from shapely.geometry import LineString, MultiLineString, Point, Polygon, box

from operators import get_operator, list_operators
from workflow_engine import execute_workflow


def lines(geometries, crs="EPSG:3857"):
    return gpd.GeoDataFrame(
        {"source_name": [f"line-{i}" for i in range(len(geometries))]},
        geometry=geometries, crs=crs,
    ).rename_geometry("boundary")


def line_overlap_frame(geometries, ids=None, crs="EPSG:3857"):
    frame = lines(geometries, crs=crs)
    frame["route_key"] = pd.Series(range(len(frame)), dtype="Int64") if ids is None else ids
    return frame


def line_overlap(frame, id_field="route_key"):
    return get_operator("check_line_overlap")(input_gdf=frame, id_field=id_field)


def split_at_intersections(frame, id_field="route_key"):
    return get_operator("split_line_intersections")(input_gdf=frame, id_field=id_field)


def test_split_line_intersections_crossings_t_junctions_vertices_contacts_and_attributes():
    frame = line_overlap_frame([
        LineString([(0, 0), (1, 0), (3, 0)]), LineString([(1, -1), (1, 1)]),
        LineString([(2, -1), (2, 0)]), LineString([(3, 0), (4, 1)]),
    ], pd.Series([2**60 + i for i in range(4)], dtype="Int64"))
    frame["grade"] = pd.Series([None, 3, 4, 5], dtype="Int64")
    frame["label"] = pd.Series(["甲", None, "丙", "丁"], dtype="string")
    frame.index = [9, 9, 2, 1]
    before = frame.copy(deep=True)
    result = split_at_intersections(frame)
    assert result.columns.tolist() == frame.columns.tolist() + ["source_id", "part_no", "segment_no"]
    expected_rows = [0, 0, 0, 1, 1, 2, 3]
    assert result.source_id.tolist() == [frame.route_key.iloc[i] for i in expected_rows]
    assert result.source_id.dtype == frame.route_key.dtype
    assert result.part_no.tolist() == [1] * 7
    assert result.segment_no.tolist() == [1, 2, 3, 1, 2, 1, 1]
    expected = [[(0, 0), (1, 0)], [(1, 0), (2, 0)], [(2, 0), (3, 0)],
                [(1, -1), (1, 0)], [(1, 0), (1, 1)], [(2, -1), (2, 0)], [(3, 0), (4, 1)]]
    assert [list(g.coords) for g in result.geometry] == expected
    attributes = frame.iloc[expected_rows].reset_index(drop=True).drop(columns="boundary")
    pd.testing.assert_frame_equal(result[attributes.columns], attributes)
    assert result.geometry.name == "boundary" and result.crs == frame.crs
    assert result.index.equals(pd.RangeIndex(7))
    assert result.part_no.dtype == result.segment_no.dtype == "int64"
    assert_geodataframe_equal(frame, before)


def test_split_line_intersections_multipart_direction_and_duplicate_cut_deduplication():
    frame = line_overlap_frame([
        MultiLineString([[(4, 0), (2, 0), (0, 0)], [(4, 3), (0, 3)]]),
        LineString([(1, -1), (1, 0)]), LineString([(1, 0), (1, 1)]),
    ])
    result = split_at_intersections(frame)
    assert [list(g.coords) for g in result.geometry] == [
        [(4, 0), (2, 0), (1, 0)], [(1, 0), (0, 0)], [(4, 3), (0, 3)],
        [(1, -1), (1, 0)], [(1, 0), (1, 1)],
    ]
    assert result.part_no.tolist() == [1, 1, 2, 1, 1]
    assert result.segment_no.tolist() == [1, 2, 1, 1, 1]


def test_split_line_intersections_closed_seam_and_repeated_original_coordinates():
    frame = line_overlap_frame([
        LineString([(0, 0), (0, 0), (2, 0), (2, 2), (0, 2), (0, 0)]),
        LineString([(1, -1), (1, 0)]),
    ])
    result = split_at_intersections(frame)
    assert [list(g.coords) for g in result.geometry] == [
        [(0, 0), (0, 0), (1, 0)], [(1, 0), (2, 0), (2, 2), (0, 2), (0, 0)],
        [(1, -1), (1, 0)],
    ]
    assert all(g.is_valid and not g.is_empty for g in result.geometry)
    assert list(split_at_intersections(frame.iloc[:1]).geometry.iloc[0].coords) == list(frame.geometry.iloc[0].coords)


def test_split_line_intersections_endpoint_contact_preserves_trailing_repeated_vertices():
    vertices = [(0, 0), (0, 0), (1, 0), (1, 0), (1, 0)]
    result = split_at_intersections(line_overlap_frame([
        LineString(vertices), LineString([(1, 0), (2, 1)]),
    ]))
    assert len(result) == 2
    assert list(result.geometry.iloc[0].coords) == vertices


@pytest.mark.parametrize("crs", ["EPSG:3857", "EPSG:2263"])
def test_split_line_intersections_binary_tiny_pieces_without_cumulative_distance_rounding(crs):
    tiny = 2**-40
    frame = line_overlap_frame([
        LineString([(-2**50, 0), (0, 0), (1, 0)]),
        LineString([(tiny, -1), (tiny, 1)]), LineString([(2*tiny, -1), (2*tiny, 1)]),
    ], crs=crs)
    result = split_at_intersections(frame)
    assert [list(g.coords) for g in result.geometry[:3]] == [
        [(-2**50, 0), (0, 0), (tiny, 0)], [(tiny, 0), (2*tiny, 0)], [(2*tiny, 0), (1, 0)],
    ]
    assert result.crs == frame.crs


def test_split_line_intersections_nonbinary_diagonal_crossing_uses_one_shared_cut_coordinate():
    result = split_at_intersections(line_overlap_frame([
        LineString([(0, 0), (7, 3)]), LineString([(1, 2), (5, 0)]),
    ]))
    point = result.geometry.iloc[0].coords[-1]
    assert len(result) == 4
    assert point == result.geometry.iloc[1].coords[0] == result.geometry.iloc[2].coords[-1] == result.geometry.iloc[3].coords[0]


@pytest.mark.parametrize("reverse", [False, True])
def test_split_line_intersections_steep_path_orders_cuts_when_minor_axis_rounds_equal(reverse):
    x = 2**50
    vertices = [(x, 0), (x + 1, 64)]
    if reverse:
        vertices.reverse()
    frame = line_overlap_frame([LineString(vertices)] + [LineString([(x - 1, y), (x + 2, y)]) for y in range(1, 5)])
    result = split_at_intersections(frame)
    pieces = result.loc[result.source_id == 0].geometry
    expected_y = [0, 1, 2, 3, 4, 64]
    if reverse:
        expected_y.reverse()
    assert [(g.coords[0][1], g.coords[-1][1]) for g in pieces] == list(zip(expected_y, expected_y[1:]))


def test_split_line_intersections_matches_independent_integer_grid_path_oracle():
    horizontal = [LineString([(0, y), (6, y)]) for y in range(5)]
    vertical = [LineString([(x, 5), (x, -1)]) for x in range(1, 6)]
    frame = line_overlap_frame(horizontal + vertical)
    result = split_at_intersections(frame)
    expected = []
    for row in range(10):
        vertices = ([(x, row) for x in range(7)] if row < 5
                    else [(row - 4, y) for y in range(5, -2, -1)])
        for number, (start, end) in enumerate(zip(vertices, vertices[1:]), 1):
            expected.append((row, 1, number, [start, end]))
    actual = [(row.source_id, row.part_no, row.segment_no, list(row.boundary.coords)) for row in result.itertuples()]
    assert actual == expected


@pytest.mark.parametrize("field", [None, "missing", "boundary"])
def test_split_line_intersections_rejects_missing_or_geometry_identity(field):
    with pytest.raises(ValueError, match="id_field"):
        split_at_intersections(line_overlap_frame([LineString([(0, 0), (1, 0)])]), field)


def test_split_line_intersections_requires_active_geodataframe_and_unique_columns():
    with pytest.raises(ValueError, match="GeoDataFrame"):
        split_at_intersections(pd.DataFrame({"route_key": [1]}))
    frame = line_overlap_frame([LineString([(0, 0), (1, 0)])])
    with pytest.raises(ValueError, match="active geometry"):
        split_at_intersections(gpd.GeoDataFrame({"route_key": [1]}))
    frame.columns = ["route_key", "boundary", "route_key"]
    with pytest.raises(ValueError, match="id_field"):
        split_at_intersections(frame)


@pytest.mark.parametrize("geometries", [
    [LineString([(0, 0), (2, 2), (0, 2), (2, 0)])],
    [LineString([(0, 0), (2, 0), (1, 0)])],
    [MultiLineString([[(0, 0), (1, 0)], [(1, 0), (2, 1)]])],
    [MultiLineString([[(0, 0), (2, 0)], [(1, -1), (1, 1)]])],
    [LineString([(0, 0), (2, 0)]), LineString([(1, 0), (3, 0)])],
    [LineString([(0, 0), (2, 0)]), LineString([(2, 0), (0, 0)])],
])
def test_split_line_intersections_rejects_all_internal_contacts_and_positive_overlap(geometries):
    frame = line_overlap_frame(geometries)
    before = frame.copy(deep=True)
    with pytest.raises(ValueError, match="internal|overlap"):
        split_at_intersections(frame)
    assert_geodataframe_equal(frame, before)


@pytest.mark.parametrize("field", ["source_id", "part_no", "segment_no"])
def test_split_line_intersections_rejects_reserved_column_conflicts(field):
    frame = line_overlap_frame([LineString([(0, 0), (1, 0)])])
    frame[field] = 1
    with pytest.raises(ValueError, match="field|column"):
        split_at_intersections(frame)


@pytest.mark.parametrize("empty", [False, True])
def test_split_line_intersections_empty_and_no_crossing_keep_schema_and_identifier_types(empty):
    frame = line_overlap_frame([LineString([(0, 0), (1, 0)])], pd.Series(["甲"], dtype="string"))
    if empty:
        frame = frame.iloc[:0]
    result = split_at_intersections(frame)
    assert len(result) == len(frame)
    assert result.source_id.dtype == frame.route_key.dtype and result.crs == frame.crs
    assert result.geometry.name == frame.geometry.name
    assert result.part_no.dtype == result.segment_no.dtype == "int64"


@pytest.mark.parametrize("shape", [None, LineString(), Point(0, 0),
    LineString([(0, 0, 0), (1, 1, 1)]), LineString([(0, 0), (float("inf"), 1)])])
def test_split_line_intersections_rejects_unsupported_geometries(shape):
    with pytest.raises(ValueError):
        split_at_intersections(line_overlap_frame([shape]))


@pytest.mark.parametrize("crs", [None, "EPSG:4326"])
def test_split_line_intersections_rejects_unknown_or_geographic_crs(crs):
    with pytest.raises(ValueError, match="projected"):
        split_at_intersections(line_overlap_frame([LineString([(0, 0), (1, 0)])], crs=crs))


@pytest.mark.parametrize("ids", [[None, 2], [1, 1], ["", "b"], ["  ", "b"], [[1], [2]]])
def test_split_line_intersections_requires_unique_explicit_scalar_ids(ids):
    with pytest.raises(ValueError, match="id_field"):
        split_at_intersections(line_overlap_frame([LineString([(0, 0), (1, 0)]), LineString([(2, 0), (3, 0)])], ids))


@pytest.mark.parametrize("empty", [False, True])
def test_split_line_intersections_workflow_native_save_reload_and_failure_no_publish(tmp_path, empty):
    from osgeo import ogr
    frame = line_overlap_frame([LineString([(0, 0), (2, 0)]), LineString([(1, -1), (1, 1)])], ["甲", "乙"])
    if empty:
        frame = frame.iloc[:0]
    frame.to_file(tmp_path / "source.gpkg", layer="routes", driver="GPKG")
    original = (tmp_path / "source.gpkg").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "source.gpkg"}, "depends_on": []},
        {"id": "split", "operator": "split_line_intersections", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_key"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "split"}, "connection_info": connection, "path": "pieces.gpkg", "mode": "fail"}, "depends_on": ["split"]},
    ]
    execution = execute_workflow({"tasks": tasks})
    assert execution["status"] == "success", execution
    dataset = ogr.Open(str(tmp_path / "pieces.gpkg")); layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == (0 if empty else 4)
    assert layer.GetSpatialRef().GetAuthorityCode(None) == "3857"
    assert [field.name for field in layer.schema] == ["source_name", "route_key", "source_id", "part_no", "segment_no"]
    expected = [[(0, 0), (1, 0)], [(1, 0), (2, 0)], [(1, -1), (1, 0)], [(1, 0), (1, 1)]]
    for index, feature in enumerate(layer):
        assert feature.GetGeometryRef().GetGeometryName() == "LINESTRING"
        assert feature.GetGeometryRef().GetCoordinateDimension() == 2
        assert feature.GetGeometryRef().GetPoints() == expected[index]
        assert feature.GetField("source_id") == feature.GetField("route_key") == ("甲" if index < 2 else "乙")
        assert feature.GetField("source_name") == f"line-{index // 2}"
        assert feature.GetField("part_no") == 1 and feature.GetField("segment_no") == index % 2 + 1
    dataset = None
    assert (tmp_path / "source.gpkg").read_bytes() == original
    bad = line_overlap_frame([LineString([(0, 0), (2, 0)]), LineString([(1, 0), (3, 0)])])
    bad.to_file(tmp_path / "bad.gpkg", driver="GPKG")
    tasks[0]["params"]["path"] = "bad.gpkg"; tasks[2]["params"]["path"] = "rejected.gpkg"
    assert execute_workflow({"tasks": tasks})["status"] == "failed"
    assert not (tmp_path / "rejected.gpkg").exists()


def test_split_line_intersections_async_discovery_workflow_only_and_required_identity(tmp_path, monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    frame = line_overlap_frame([LineString([(0, 0), (2, 0)]), LineString([(1, -1), (1, 1)])], [17, 23])
    frame.to_file(tmp_path / "source.gpkg", driver="GPKG")
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": {"engine_type": "nfs", "mount_path": str(tmp_path)}, "path": "source.gpkg"}, "depends_on": []},
        {"id": "split", "operator": "split_line_intersections", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_key"}, "depends_on": ["load"]},
    ]
    monkeypatch.setattr(api_server, "executions", ExecutionRegistry())
    with api_server.app.test_client() as client:
        op = next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"] == "split_line_intersections")
        assert op["display_name"] == "交点打断" and op["category_path"] == ["矢量处理", "几何处理"]
        assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
        assert [(p["name"], p["required"]) for p in op["parameters"]] == [("input_gdf", True), ("id_field", True)]
        assert client.post("/api/operators/split_line_intersections/invoke", json={"params": {}}).status_code == 403
        response = client.post("/api/workflow", json={"workflow_def": {"tasks": tasks}, "runtime": {"tenant_id": 7, "execution_authorization": {"id": 1, "effects": ["read"]}}})
        assert response.status_code == 202
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            state = client.get("/api/executions/" + response.get_json()["execution_id"]).get_json()
            if state["status"] in ("success", "failed"):
                break
            time.sleep(.01)
        assert state["status"] == "success", state
        assert len(json.loads(state["result"])["features"]) == 4
    tasks[1]["params"].pop("id_field")
    assert execute_workflow({"tasks": tasks})["status"] == "failed"


def test_line_overlap_reports_partial_contained_reversed_and_resampled_pairs_without_source_changes():
    frame = line_overlap_frame([
        LineString([(0, 0), (4, 0)]), LineString([(2, 0), (6, 0)]),
        LineString([(1, 0), (3, 0)]), LineString([(4, 0), (2, 0), (0, 0)]),
    ], pd.Series([90, 10, 30, 20], dtype="Int64"))
    frame.index = [8, 8, 2, 9]
    before = frame.copy(deep=True)
    result = line_overlap(frame)
    assert result.columns.tolist() == ["source_id_a", "source_id_b", "overlap_length", "geometry"]
    assert list(zip(result.source_id_a, result.source_id_b, result.overlap_length)) == [
        (90, 10, 2), (90, 30, 2), (90, 20, 4), (10, 30, 1), (10, 20, 2), (30, 20, 2),
    ]
    expected = [(2, 4), (1, 3), (0, 4), (2, 3), (2, 4), (1, 3)]
    for geometry, (start, end) in zip(result.geometry, expected):
        assert geometry.geom_type == "MultiLineString" and geometry.is_valid
        assert geometry.equals(LineString([(start, 0), (end, 0)]))
    assert result.crs == frame.crs
    assert_geodataframe_equal(frame, before)


def test_line_overlap_keeps_disconnected_parts_and_excludes_contact_points():
    a = MultiLineString([[(0, 0), (3, 0)], [(0, 2), (3, 2)], [(5, 0), (5, 2)]])
    b = MultiLineString([[(1, 0), (2, 0)], [(1, 2), (2, 2)], [(4, 1), (6, 1)]])
    result = line_overlap(line_overlap_frame([a, b], ["线甲", "线乙"]))
    assert len(result) == 1 and result.overlap_length.tolist() == [2]
    assert result.geometry.iloc[0].equals(MultiLineString([[(1, 0), (2, 0)], [(1, 2), (2, 2)]]))
    assert len(result.geometry.iloc[0].geoms) == 2


def test_line_overlap_does_not_report_crossings_contacts_parallel_lines_or_self_overlap():
    frame = line_overlap_frame([
        LineString([(0, 0), (2, 0)]), LineString([(1, -1), (1, 1)]),
        LineString([(2, 0), (3, 1)]), LineString([(0, 2**-40), (0.5, 2**-40)]),
    ])
    assert line_overlap(frame).empty
    for geometry in [LineString([(0, 0), (2, 0), (0, 0)]),
                     MultiLineString([[(0, 0), (2, 0)], [(1, 0), (3, 0)]])]:
        assert line_overlap(line_overlap_frame([geometry])).empty


def test_line_overlap_counts_each_common_position_once_per_pair_for_retraced_parts():
    frame = line_overlap_frame([
        LineString([(0, 0), (4, 0), (0, 0)]),
        MultiLineString([[(1, 0), (3, 0)], [(3, 0), (1, 0)], [(2, 0), (4, 0)]]),
    ])
    result = line_overlap(frame)
    assert result.overlap_length.tolist() == [3]
    assert result.geometry.iloc[0].equals(LineString([(1, 0), (4, 0)]))


def test_line_overlap_closed_lines_and_direction_have_identical_normalized_output():
    ring = LineString([(0, 0), (2, 0), (2, 1), (0, 1), (0, 0)])
    reverse = shapely.reverse(ring)
    result = line_overlap(line_overlap_frame([ring, reverse]))
    other = line_overlap(line_overlap_frame([reverse, ring]))
    assert result.overlap_length.tolist() == [6]
    assert result.geometry.iloc[0].equals(ring)
    assert result.geometry.iloc[0].wkb == other.geometry.iloc[0].wkb


@pytest.mark.parametrize("crs", ["EPSG:3857", "EPSG:2263"])
@pytest.mark.parametrize("length", [2.0, 2**-40])
def test_line_overlap_retains_positive_lengths_in_source_units(crs, length):
    frame = line_overlap_frame([LineString([(0, 0), (length, 0)])] * 3, crs=crs)
    result = line_overlap(frame)
    assert list(zip(result.source_id_a, result.source_id_b)) == [(0, 1), (0, 2), (1, 2)]
    assert result.overlap_length.tolist() == [length] * 3
    assert result.crs == frame.crs


@pytest.mark.parametrize("ids", [pd.Series([2**60, 2**60+1], dtype="Int64"), pd.Series(["甲", "乙"], dtype="string")])
def test_line_overlap_retains_identifier_dtypes_and_empty_schema(ids):
    frame = line_overlap_frame([LineString([(0, 0), (1, 0)])] * 2, ids)
    result = line_overlap(frame)
    assert result.source_id_a.tolist() == ids.iloc[:1].tolist()
    assert result.source_id_b.tolist() == ids.iloc[1:].tolist()
    for source in [frame.iloc[:0], frame.iloc[:1], frame]:
        output = line_overlap(source)
        assert output.source_id_a.dtype == output.source_id_b.dtype == ids.dtype
        assert output.overlap_length.dtype == np.dtype("float64")
        assert output.crs == frame.crs


@pytest.mark.parametrize("ids", [[1, 1], [None, 1], [" ", "x"], [[1], [2]]])
def test_line_overlap_rejects_invalid_source_ids(ids):
    with pytest.raises(ValueError, match="identifiers"):
        line_overlap(line_overlap_frame([LineString([(0, 0), (1, 0)])] * 2, ids))


@pytest.mark.parametrize("id_field", [None, "missing", "boundary", ""])
def test_line_overlap_requires_explicit_nongeometry_identity(id_field):
    with pytest.raises(ValueError, match="id_field"):
        line_overlap(line_overlap_frame([LineString([(0, 0), (1, 0)])]), id_field)


@pytest.mark.parametrize("crs", [None, "EPSG:4326"])
def test_line_overlap_rejects_unknown_or_geographic_crs(crs):
    with pytest.raises(ValueError, match="projected"):
        line_overlap(line_overlap_frame([], crs=crs))


@pytest.mark.parametrize("geometry", [None, LineString(), Point(0, 0), box(0, 0, 1, 1),
    shapely.from_wkt("MULTILINESTRING (EMPTY, (0 0, 1 0))"),
    LineString([(0, 0), (0, 0)]), LineString([(0, 0, 1), (1, 0, 1)]),
    shapely.from_wkt("LINESTRING M (0 0 1, 1 0 1)"), LineString([(0, 0), (float("inf"), 1)])])
def test_line_overlap_rejects_unsupported_geometry(geometry):
    with pytest.raises(ValueError):
        line_overlap(line_overlap_frame([geometry]))


def test_line_overlap_rejects_nonfinite_output_length():
    frame = line_overlap_frame([LineString([(-1e308, 0), (1e308, 0)])] * 2)
    with np.errstate(over="ignore", invalid="ignore"):
        with pytest.raises(ValueError, match="finite positive"):
            line_overlap(frame)


@pytest.mark.parametrize("length", [2**-600, 1e200])
def test_line_overlap_retains_representable_lengths_without_squared_distance_underflow_or_overflow(length):
    frame = line_overlap_frame([LineString([(0, 0), (length, 0)])] * 2)
    result = line_overlap(frame)
    assert result.overlap_length.tolist() == [length]
    assert list(result.geometry.iloc[0].geoms[0].coords) == [(0, 0), (length, 0)]


def test_line_overlap_matches_independent_interval_pair_oracle():
    intervals = [(0, 2), (1, 4), (4, 5), (2, 3), (0, 2), (10, 12)]
    expected = [(a, b, min(y, v)-max(x, u))
                for a, (x, y) in enumerate(intervals) for b, (u, v) in enumerate(intervals)
                if a < b and min(y, v) > max(x, u)]
    result = line_overlap(line_overlap_frame([LineString([(x, 0), (y, 0)]) for x, y in intervals]))
    assert list(zip(result.source_id_a, result.source_id_b, result.overlap_length)) == expected


def test_line_overlap_discovery_workflow_save_reload_and_native_coordinates(tmp_path):
    import api_server
    from osgeo import ogr
    tiny = 2**-40
    source = line_overlap_frame([
        LineString([(0, 0), (4, 0)]), LineString([(3, 0), (1, 0)]),
        LineString([(0, 10), (tiny, 10)]), LineString([(tiny, 10), (0, 10)]),
    ], ["线甲", "线乙", "微甲", "微乙"])
    source.to_file(tmp_path / "lines.gpkg", layer="routes", driver="GPKG")
    before = (tmp_path / "lines.gpkg").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "lines.gpkg"}, "depends_on": []},
        {"id": "check", "operator": "check_line_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_key"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "overlaps.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]
    execution = execute_workflow({"tasks": tasks})
    assert execution["status"] == "success", execution.get("error")
    dataset = ogr.Open(str(tmp_path / "overlaps.gpkg"))
    layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == 2 and layer.GetSpatialRef().GetAuthorityCode(None) == "3857"
    expected = [("线甲", "线乙", 2, [(1, 0), (3, 0)]), ("微甲", "微乙", tiny, [(0, 10), (tiny, 10)])]
    for feature, (a, b, length, points) in zip(layer, expected):
        geometry = feature.GetGeometryRef()
        assert geometry.GetGeometryName() == "MULTILINESTRING" and geometry.IsValid()
        assert geometry.GetGeometryCount() == 1
        assert (feature.GetField("source_id_a"), feature.GetField("source_id_b"), feature.GetField("overlap_length")) == (a, b, length)
        assert geometry.Length() == length
        assert [p[:2] for p in geometry.GetGeometryRef(0).GetPoints()] == points
    dataset = None
    assert (tmp_path / "lines.gpkg").read_bytes() == before
    with api_server.app.test_client() as client:
        op = next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"] == "check_line_overlap")
        assert op["display_name"] == "线叠检查" and op["category_path"] == ["矢量处理", "几何处理"]
        assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
        assert [(p["name"], p["param_type"], p["required"]) for p in op["parameters"]] == [("input_gdf", "input", True), ("id_field", "param", True)]
        assert [(p["name"], p["type"], p["is_default"]) for p in op["output_ports"]] == [("result", "geodataframe", True)]
        assert client.post("/api/operators/check_line_overlap/invoke", json={"params": {}}).status_code == 403


def test_line_overlap_invalid_identity_fails_before_save(tmp_path):
    source = line_overlap_frame([LineString([(0, 0), (2, 0)])] * 2, ["重复", "重复"])
    source.to_file(tmp_path / "lines.gpkg", driver="GPKG")
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    result = execute_workflow({"tasks": [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "lines.gpkg"}, "depends_on": []},
        {"id": "check", "operator": "check_line_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_key"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "invalid-result.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]})
    assert result["status"] == "failed"
    assert not (tmp_path / "invalid-result.gpkg").exists()


def self_overlap(frame, id_field="route_key"):
    return get_operator("check_line_self_overlap")(input_gdf=frame, id_field=id_field)


@pytest.mark.parametrize("geometry, expected", [
    (LineString([(0, 0), (4, 0), (0, 0)]), MultiLineString([[(0, 0), (4, 0)]])),
    (LineString([(0, 0), (4, 0), (1, 0)]), MultiLineString([[(1, 0), (4, 0)]])),
    (LineString([(0, 0), (0, 0), (4, 0), (4, 0), (0, 0)]), MultiLineString([[(0, 0), (4, 0)]])),
    (MultiLineString([[(0, 0), (4, 0)], [(3, 0), (1, 0)], [(1, 0), (3, 0)]]), MultiLineString([[(1, 0), (3, 0)]])),
    (MultiLineString([[(0, 0), (4, 0)], [(2, 0), (6, 0)], [(0, 2), (4, 2)], [(2, 2), (6, 2)], [(6, 0), (7, 1)]]), MultiLineString([[(2, 0), (4, 0)], [(2, 2), (4, 2)]])),
    (LineString([(0, 0), (2, 0), (2, 2), (0, 2), (0, 0), (2, 0)]), MultiLineString([[(0, 0), (2, 0)]])),
])
def test_line_self_overlap_reports_original_retracing_and_multipart_positions(geometry, expected):
    result = self_overlap(line_overlap_frame([geometry], ["路线甲"]))
    assert result.columns.tolist() == ["source_id", "overlap_length", "geometry"]
    assert result.source_id.tolist() == ["路线甲"]
    assert result.geometry.iloc[0].geom_type == "MultiLineString"
    assert result.geometry.iloc[0].equals(expected)
    assert result.overlap_length.tolist() == [expected.length]


@pytest.mark.parametrize("geometry", [
    LineString([(0, 0), (1, 0), (2, 0)]),
    LineString([(0, 0), (0, 0), (2, 0)]),
    LineString([(0, 0), (2, 2), (0, 2), (2, 0)]),
    LineString([(0, 0), (2, 0), (2, 2), (0, 2), (0, 0)]),
    MultiLineString([[(0, 0), (2, 0)], [(1, 0), (1, 1)], [(2, 0), (3, 0)]]),
    MultiLineString([[(0, 0), (2, 0)], [(0, 2**-40), (2, 2**-40)]]),
])
def test_line_self_overlap_ignores_points_closed_seams_zero_segments_and_near_lines(geometry):
    assert self_overlap(line_overlap_frame([geometry])).empty



@pytest.mark.parametrize("visits", [2, 4, 8])
def test_line_self_overlap_deduplicates_only_generated_intersections(visits, monkeypatch):
    from operators import geometric_operators as operators
    original_intersection = operators.shapely.intersection
    original_union = operators.shapely.union_all
    compared, merged = [], []

    def intersect(a, b):
        compared.extend((left.wkb, right.wkb) for left, right in zip(np.atleast_1d(a), np.atleast_1d(b)))
        return original_intersection(a, b)

    def merge(parts, *args, **kwargs):
        merged.append(len(parts))
        return original_union(parts, *args, **kwargs)

    monkeypatch.setattr(operators.shapely, "intersection", intersect)
    monkeypatch.setattr(operators.shapely, "union_all", merge)
    source = MultiLineString([[(0, 0), (8, 0)] if i % 2 else [(8, 0), (0, 0)] for i in range(visits)])
    frame = line_overlap_frame([source, source], ["first", "second"])
    before = frame.copy(deep=True)
    result = self_overlap(frame)
    # Every original pair must still be examined, but repeated positive pieces
    # must not reach GEOS union as a quadratic duplicate geometry collection.
    assert len(compared) == visits * (visits - 1)
    assert merged == [1, 1]
    assert result.source_id.tolist() == ["first", "second"]
    assert result.overlap_length.tolist() == [8, 8]
    assert all(list(g.geoms[0].coords) == [(0, 0), (8, 0)] for g in result.geometry)
    assert_geodataframe_equal(frame, before)


@pytest.mark.parametrize("count, expected_sizes", [(64, [2016]), (65, [2048, 32])])
def test_line_self_overlap_batch2048_preserves_all_original_pair_order(count, expected_sizes, monkeypatch):
    from operators import geometric_operators as operators
    original = operators.shapely.intersection
    segments = [LineString([(0, 0), (i + 1, 0)]) for i in range(count)]
    compared, sizes = [], []

    def intersect(left, right):
        assert isinstance(left, np.ndarray) and isinstance(right, np.ndarray)
        sizes.append(len(left))
        compared.extend((int(a.coords[-1][0]) - 1, int(b.coords[-1][0]) - 1)
                        for a, b in zip(left, right))
        return original(left, right)

    monkeypatch.setattr(operators.shapely, "intersection", intersect)
    result = list(operators._self_overlap_intersections(segments))
    assert sizes == expected_sizes
    assert compared == [(a, b) for a in range(count) for b in range(a + 1, count)]
    assert [list(part.coords) for part in result] == [[(0, 0), (i + 1, 0)] for i in range(count - 1)]


def test_line_self_overlap_batch2048_exact_limit_and_no_empty_tail(monkeypatch):
    from operators import geometric_operators as operators
    original = operators.shapely.intersection
    # C(64,2) + 32 disjoint duplicate pairs = exactly 2048 bbox pairs.
    segments = [LineString([(0, 0), (1, 0)])] * 64
    segments += [LineString([(3 * i, 0), (3 * i + 1, 0)]) for i in range(1, 33) for _ in range(2)]
    sizes = []

    def intersect(left, right):
        assert isinstance(left, np.ndarray) and isinstance(right, np.ndarray)
        sizes.append(len(left))
        return original(left, right)

    monkeypatch.setattr(operators.shapely, "intersection", intersect)
    assert len(list(operators._self_overlap_intersections(segments))) == 33
    assert sizes == [2048]
    assert list(operators._self_overlap_intersections([])) == []
    assert list(operators._self_overlap_intersections(segments[:1])) == []
    assert sizes == [2048]


def test_line_self_overlap_batch2048_dedup_spans_batches_but_not_sources(monkeypatch):
    from operators import geometric_operators as operators
    original_intersection, original_union = operators.shapely.intersection, operators.shapely.union_all
    sizes, merged = [], []

    def intersect(left, right):
        assert isinstance(left, np.ndarray) and isinstance(right, np.ndarray)
        sizes.append(len(left))
        return original_intersection(left, right)

    def merge(parts, *args, **kwargs):
        merged.append(len(parts))
        return original_union(parts, *args, **kwargs)

    monkeypatch.setattr(operators.shapely, "intersection", intersect)
    monkeypatch.setattr(operators.shapely, "union_all", merge)
    source = MultiLineString([[(0, 0), (8, 0)] if i % 2 else [(8, 0), (0, 0)] for i in range(65)])
    frame = line_overlap_frame([source, source], ["first", "second"])
    before = frame.copy(deep=True)
    result = self_overlap(frame)
    assert sizes == [2048, 32, 2048, 32]
    assert merged == [1, 1]
    assert result.source_id.tolist() == ["first", "second"]
    assert result.overlap_length.tolist() == [8, 8]
    assert all(list(shape.geoms[0].coords) == [(0, 0), (8, 0)] for shape in result.geometry)
    assert_geodataframe_equal(frame, before)


def test_line_self_overlap_batch2048_failure_does_not_fallback_or_save(tmp_path, monkeypatch):
    from operators import geometric_operators as operators
    source = line_overlap_frame([MultiLineString([[(0, 0), (8, 0)]] * 65)])
    source.to_file(tmp_path / "source.gpkg", layer="routes", driver="GPKG")
    before = (tmp_path / "source.gpkg").read_bytes()
    original, calls = operators.shapely.intersection, []

    def intersect(left, right):
        assert isinstance(left, np.ndarray) and isinstance(right, np.ndarray)
        calls.append(len(left))
        if len(calls) == 2:
            raise RuntimeError("batch intersection failure")
        return original(left, right)

    monkeypatch.setattr(operators.shapely, "intersection", intersect)
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "source.gpkg"}, "depends_on": []},
        {"id": "check", "operator": "check_line_self_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_key"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "rejected.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]
    assert execute_workflow({"tasks": tasks})["status"] == "failed"
    assert calls == [2048, 32]
    assert not (tmp_path / "rejected.gpkg").exists()
    assert (tmp_path / "source.gpkg").read_bytes() == before


def test_line_self_overlap_exact_collection_retains_tiny_differences_and_segmentation():
    tiny = 2**-40
    source = MultiLineString([[(0, 0), (4, 0)], [(4, 0), (0, 0)],
        [(0, 0), (1, 0), (4, 0)], [(0, tiny), (tiny, tiny)], [(tiny, tiny), (0, tiny)]])
    result = self_overlap(line_overlap_frame([source]))
    shape = result.geometry.iloc[0]
    assert result.overlap_length.tolist() == [4 + tiny]
    assert shape.covers(Point(.5, 0)) and shape.covers(Point(3, 0))
    assert shape.covers(Point(tiny / 2, tiny))
    assert not shape.covers(Point(.5, tiny))
    assert sorted([list(part.coords) for part in shape.geoms]) == [
        [(0, 0), (1, 0)], [(0, tiny), (tiny, tiny)], [(1, 0), (4, 0)]]


def test_line_self_overlap_does_not_compare_different_features():
    frame = line_overlap_frame([LineString([(0, 0), (4, 0)])] * 2)
    assert self_overlap(frame).empty
    assert line_overlap(frame).overlap_length.tolist() == [4]


@pytest.mark.parametrize("visits", [2, 3, 7])
def test_line_self_overlap_metric_is_independent_of_visits_direction_and_vertex_sampling(visits):
    coarse = MultiLineString([[(0, 0), (4, 0)]] * visits)
    sampled = MultiLineString([[(4, 0), (3, 0), (1, 0), (0, 0)]] * visits)
    for geometry in [coarse, sampled, shapely.reverse(sampled)]:
        result = self_overlap(line_overlap_frame([geometry]))
        assert result.overlap_length.tolist() == [4]
        assert result.geometry.iloc[0].equals(LineString([(0, 0), (4, 0)]))
    a = self_overlap(line_overlap_frame([sampled])).geometry.iloc[0]
    b = self_overlap(line_overlap_frame([shapely.reverse(sampled)])).geometry.iloc[0]
    assert a.wkb == b.wkb


@pytest.mark.parametrize("crs", ["EPSG:3857", "EPSG:2263"])
@pytest.mark.parametrize("length", [2**-40, 1e-200, 1e200])
def test_line_self_overlap_preserves_finite_lengths_and_source_units(crs, length):
    result = self_overlap(line_overlap_frame([LineString([(0, 0), (length, 0), (0, 0)])], crs=crs))
    assert result.overlap_length.tolist() == [length]
    assert result.crs.to_string() == crs
    assert list(result.geometry.iloc[0].geoms[0].coords) == [(0, 0), (length, 0)]


@pytest.mark.parametrize("ids", [pd.Series(["丙", "甲", "乙"], dtype="string"), pd.Series([90, 3, 12], dtype="Int64")])
def test_line_self_overlap_preserves_identity_order_schema_and_source(ids):
    frame = line_overlap_frame([LineString([(0, 0), (2, 0), (0, 0)]), LineString([(0, 0), (1, 0)]),
                                LineString([(0, 2), (1, 2), (0, 2)])], ids)
    frame.index = [8, 8, 1]
    before = frame.copy(deep=True)
    result = self_overlap(frame)
    assert result.source_id.tolist() == ids.iloc[[0, 2]].tolist()
    assert result.source_id.dtype == ids.dtype
    assert result.crs == frame.crs and result.overlap_length.tolist() == [2, 1]
    assert_geodataframe_equal(frame, before)
    for source in [frame.iloc[:0], frame.iloc[[1]]]:
        output = self_overlap(source)
        assert output.empty and output.source_id.dtype == ids.dtype
        assert output.overlap_length.dtype == "float64" and output.crs == frame.crs
        assert output.columns.tolist() == result.columns.tolist()


@pytest.mark.parametrize("ids", [[None, "b"], ["a", "a"], [" ", "b"], [[1], [2]]])
def test_line_self_overlap_rejects_invalid_ids(ids):
    with pytest.raises(ValueError, match="identifier"):
        self_overlap(line_overlap_frame([LineString([(0, 0), (1, 0)])] * 2, ids))


@pytest.mark.parametrize("field", [None, "", "missing", "boundary"])
def test_line_self_overlap_requires_explicit_id(field):
    with pytest.raises(ValueError, match="id_field"):
        self_overlap(line_overlap_frame([LineString([(0, 0), (1, 0)])]), field)


@pytest.mark.parametrize("crs", [None, "EPSG:4326"])
def test_line_self_overlap_rejects_nonprojected_crs(crs):
    with pytest.raises(ValueError, match="projected CRS"):
        self_overlap(line_overlap_frame([], crs=crs))


@pytest.mark.parametrize("geometry", [None, LineString(), Point(0, 0), box(0, 0, 1, 1),
    LineString([(0, 0), (0, 0)]), LineString([(0, 0, 1), (1, 0, 2)]),
    shapely.from_wkt("LINESTRING M (0 0 1, 1 0 2)"), LineString([(0, 0), (np.inf, 1)])])
def test_line_self_overlap_rejects_unsupported_geometry(geometry):
    with pytest.raises(ValueError):
        self_overlap(line_overlap_frame([geometry]))


def test_line_self_overlap_rejects_nonfinite_total_length():
    frame = line_overlap_frame([MultiLineString([[(0, 0), (1e308, 0)], [(1e308, 0), (0, 0)],
                                                  [(0, 2), (1e308, 2)], [(1e308, 2), (0, 2)]])])
    with np.errstate(over="ignore", invalid="ignore"):
        with pytest.raises(ValueError, match="finite positive Float64"):
            self_overlap(frame)


def test_line_self_overlap_matches_independent_integer_coverage_oracle():
    # Half-open integer cells give independent exact coverage counts. No GEOS
    # overlay or production function is used to construct the expected union.
    intervals = [(0, 5), (3, 8), (8, 6), (10, 12), (12, 10), (20, 21)]
    expected_cells = [x for x in range(21) if sum(min(a, b) <= x < max(a, b) for a, b in intervals) >= 2]
    source = MultiLineString([[(a, 0), (b, 0)] for a, b in intervals])
    result = self_overlap(line_overlap_frame([source]))
    assert result.overlap_length.tolist() == [len(expected_cells)]
    geometry = result.geometry.iloc[0]
    for x in range(21):
        assert geometry.covers(Point(x + .5, 0)) == (x in expected_cells)


@pytest.mark.parametrize("empty", [False, True])
def test_line_self_overlap_workflow_native_save_reload_and_discovery(tmp_path, empty):
    import api_server
    from osgeo import ogr
    tiny = 2**-40
    source = line_overlap_frame([LineString([(0, 0), (4, 0), (1, 0)]),
        MultiLineString([[(0, 10), (tiny, 10)], [(tiny, 10), (0, 10)]])], ["往返", "微线"])
    if empty:
        source = line_overlap_frame([LineString([(0, 0), (4, 0)])], ["正常"])
    source.to_file(tmp_path / "source.gpkg", layer="routes", driver="GPKG")
    original = (tmp_path / "source.gpkg").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "source.gpkg"}, "depends_on": []},
        {"id": "check", "operator": "check_line_self_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "route_key"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "self-overlaps.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]
    result = execute_workflow({"tasks": tasks})
    assert result["status"] == "success", result.get("error")
    dataset = ogr.Open(str(tmp_path / "self-overlaps.gpkg"))
    layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == (0 if empty else 2)
    assert layer.GetSpatialRef().GetAuthorityCode(None) == "3857"
    assert [field.name for field in layer.schema] == ["source_id", "overlap_length"]
    # Empty GeoDataFrames carry a geometry dtype, not a concrete subtype.
    # The existing save path preserves fields/CRS and declares generic geometry.
    assert layer.GetGeomType() == (ogr.wkbUnknown if empty else ogr.wkbMultiLineString)
    if not empty:
        expected = [("往返", 3, [(1, 0), (4, 0)]), ("微线", tiny, [(0, 10), (tiny, 10)])]
        for feature, (identity, length, points) in zip(layer, expected):
            geometry = feature.GetGeometryRef()
            assert feature.GetField("source_id") == identity and feature.GetField("overlap_length") == length
            assert geometry.GetGeometryName() == "MULTILINESTRING" and geometry.IsValid()
            assert geometry.GetGeometryCount() == 1 and geometry.Length() == length
            assert [p[:2] for p in geometry.GetGeometryRef(0).GetPoints()] == points
    dataset = None
    assert (tmp_path / "source.gpkg").read_bytes() == original
    with api_server.app.test_client() as client:
        op = next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"] == "check_line_self_overlap")
        assert op["display_name"] == "线内自叠" and op["category_path"] == ["矢量处理", "几何处理"]
        assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
        assert [(p["name"], p["param_type"], p["required"]) for p in op["parameters"]] == [("input_gdf", "input", True), ("id_field", "param", True)]
        assert [(p["name"], p["type"], p["is_default"]) for p in op["output_ports"]] == [("result", "geodataframe", True)]
        assert client.post("/api/operators/check_line_self_overlap/invoke", json={"params": {}}).status_code == 403
    tasks[1]["params"]["id_field"] = "missing_id"
    tasks[2]["params"]["path"] = "rejected.gpkg"
    assert execute_workflow({"tasks": tasks})["status"] == "failed"
    assert not (tmp_path / "rejected.gpkg").exists()


def polygonize(frame):
    return get_operator("polygonize")(input_gdf=frame)


def test_polygonize_nodes_intersections_and_removes_duplicate_edges_without_mutating_source():
    frame = lines([
        box(0, 0, 4, 4).boundary,
        LineString([(-1, 2), (5, 2)]),
        MultiLineString([[(2, -1), (2, 5)], [(0, 0), (4, 0)]]),
        LineString([(4, 0), (0, 0)]),
    ])
    frame.index = [3, 5, 7, 9]
    original = frame.copy(deep=True)
    result = polygonize(frame)
    faces, diagnostics = result["result"], result["diagnostics"]

    assert len(faces) == 4
    assert set(faces.geom_type) == {"Polygon"}
    assert sorted(faces.area) == [4, 4, 4, 4]
    assert shapely.union_all(faces.geometry.array).equals(box(0, 0, 4, 4))
    assert faces.columns.tolist() == ["geometry"]
    assert diagnostics.columns.tolist() == ["issue_type", "geometry"]
    assert diagnostics.issue_type.tolist() == ["dangle"] * 4
    assert diagnostics.length.sum() == 4
    assert faces.crs == diagnostics.crs == frame.crs
    assert_geodataframe_equal(frame, original)


def test_polygonize_keeps_nested_faces_holes_and_self_crossing_regions():
    nested = polygonize(lines([box(0, 0, 4, 4).boundary, box(1, 1, 3, 3).boundary]))
    faces = nested["result"]
    assert sorted(faces.area) == [4, 12]
    assert sorted(len(face.interiors) for face in faces.geometry) == [0, 1]
    assert nested["diagnostics"].empty
    assert faces.geometry.iloc[0].intersection(faces.geometry.iloc[1]).area == 0

    crossing = polygonize(lines([LineString([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)])]))
    assert sorted(crossing["result"].area) == [1, 1]
    assert crossing["result"].is_valid.all()
    assert crossing["diagnostics"].empty


def test_polygonize_distinguishes_cut_edges_from_dangles():
    bridge = LineString([(1, 0.5), (2, 0.5)])
    tail = LineString([(3, 0.5), (4, 0.5)])
    result = polygonize(lines([box(0, 0, 1, 1).boundary, box(2, 0, 3, 1).boundary, bridge, tail]))
    assert len(result["result"]) == 2
    diagnostics = result["diagnostics"]
    assert set(diagnostics.issue_type) == {"cut_edge", "dangle"}
    assert diagnostics.loc[diagnostics.issue_type == "cut_edge"].geometry.iloc[0].equals(bridge)
    assert diagnostics.loc[diagnostics.issue_type == "dangle"].geometry.iloc[0].equals(tail)


def test_polygonize_does_not_snap_small_gaps_or_invent_polygons():
    open_ring = LineString([(0, 0), (1, 0), (1, 1), (0, 1), (0, 1e-12)])
    result = polygonize(lines([open_ring]))
    assert result["result"].empty
    assert result["diagnostics"].issue_type.tolist() == ["dangle"]
    assert result["diagnostics"].geometry.iloc[0].equals(open_ring)


@pytest.mark.parametrize("crs", [None, "EPSG:4326", "EPSG:3857"])
def test_polygonize_preserves_empty_schema_and_crs(crs):
    result = polygonize(lines([], crs=crs))
    assert result["result"].empty and result["diagnostics"].empty
    assert result["result"].columns.tolist() == ["geometry"]
    assert result["diagnostics"].columns.tolist() == ["issue_type", "geometry"]
    assert str(result["diagnostics"].issue_type.dtype) == "string"
    for output in result.values():
        assert output.crs == lines([], crs=crs).crs
        assert json.loads(output.to_json())["features"] == []


@pytest.mark.parametrize("geometry,message", [
    (None, "non-null"), (LineString(), "empty"), (Point(0, 0), "LineString"),
    (Polygon([(0, 0), (1, 0), (1, 1)]), "LineString"),
    (LineString([(0, 0, 1), (1, 1, 2)]), "two-dimensional"),
    (shapely.from_wkt("LINESTRING M (0 0 1, 1 1 2)"), "two-dimensional"),
    (LineString([(0, 0), (np.inf, 1)]), "finite coordinates"),
    (LineString([(0, 0), (0, 0)]), "valid lines"),
    (shapely.from_wkt("MULTILINESTRING (EMPTY, (0 0, 1 1))"), "empty"),
])
def test_polygonize_rejects_unsupported_rows_without_silently_dropping_them(geometry, message):
    with pytest.raises(ValueError, match=message):
        polygonize(lines([box(0, 0, 1, 1).boundary, geometry]))


@pytest.mark.parametrize("frame", [None, pd.DataFrame({"x": [1]}), gpd.GeoDataFrame({"x": [1]})])
def test_polygonize_requires_an_active_geodataframe(frame):
    with pytest.raises(ValueError, match="GeoDataFrame|geometry"):
        polygonize(frame)


def test_polygonize_metadata_declares_existing_workflow_ports_only():
    operator = next(op for op in list_operators() if op["id"] == "polygonize")
    assert operator["display_name"] == "拓扑构面"
    assert operator["category_path"] == ["矢量处理", "几何处理"]
    assert operator["execution_modes"] == ["workflow"]
    assert operator["effects"] == ["read"]
    assert [(p["name"], p["param_type"]) for p in operator["parameters"]] == [("input_gdf", "input")]
    assert [(p["name"], p["type"], p["is_default"]) for p in operator["output_ports"]] == [
        ("result", "geodataframe", True), ("diagnostics", "geodataframe", False),
    ]


def test_polygonize_runtime_discovery_async_execution_and_direct_rejection(monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry

    monkeypatch.setattr(api_server, "executions", ExecutionRegistry())
    with api_server.app.test_client() as client:
        response = client.get("/api/operators")
        assert response.status_code == 200
        discovered = next(op for op in response.get_json()["operators"] if op["id"] == "polygonize")
        assert [p["name"] for p in discovered["output_ports"]] == ["result", "diagnostics"]
        source = lines([box(0, 0, 1, 1).boundary], crs="EPSG:4326")
        params = {"input_gdf": json.loads(source.to_json())}
        assert client.post("/api/operators/polygonize/invoke", json={"params": params}).status_code == 403
        workflow = {"tasks": [{"id": "faces", "operator": "polygonize", "params": params, "depends_on": []}]}
        assert client.post("/api/workflow", json={"workflow_def": workflow}).status_code == 400
        submitted = client.post("/api/workflow", json={"workflow_def": workflow, "runtime": {
            "tenant_id": 7, "execution_authorization": {"id": 1, "effects": ["read"]},
        }})
        assert submitted.status_code == 202
        execution_id = submitted.get_json()["execution_id"]
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            response = client.get("/api/executions/" + execution_id)
            assert response.status_code == 200
            state = response.get_json()
            if state["status"] in {"success", "failed"}:
                break
            time.sleep(0.01)
        assert state["status"] == "success", state
        preview = json.loads(state["result"])
        assert len(preview["result"]["features"]) == 1
        assert preview["diagnostics"]["features"] == []


@pytest.mark.parametrize("has_closed_region", [True, False])
def test_polygonize_workflow_saves_and_reloads_both_ports(tmp_path, has_closed_region):
    source = lines([box(0, 0, 1, 1).boundary, LineString([(1, 1), (2, 2)])] if has_closed_region
                   else [LineString([(0, 0), (1, 1)])])
    source.to_file(tmp_path / "source.geojson", driver="GeoJSON")
    source_bytes = (tmp_path / "source.geojson").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "source.geojson"}, "depends_on": []},
        {"id": "build", "operator": "polygonize", "params": {"input_gdf": {"$ref": "load"}}, "depends_on": ["load"]},
    ]
    for name, reference in [("faces", {"$ref": "build"}), ("diagnostics", {"$ref": "build", "port": "diagnostics"})]:
        tasks.append({"id": name, "operator": "save", "params": {
            "input_df": reference, "connection_info": connection, "path": name + ".geojson",
        }, "depends_on": ["build"]})
    execution = execute_workflow({"tasks": tasks})
    assert execution["status"] == "success", execution.get("error")
    preview = json.loads(execution["all_results"]["build"])
    assert set(preview) == {"result", "diagnostics"}
    faces = gpd.read_file(tmp_path / "faces.geojson")
    diagnostics = gpd.read_file(tmp_path / "diagnostics.geojson")
    assert len(faces) == int(has_closed_region)
    assert len(diagnostics) == 1
    assert diagnostics.issue_type.tolist() == ["dangle"]
    assert faces.crs == diagnostics.crs == source.crs
    assert (tmp_path / "source.geojson").read_bytes() == source_bytes
    if has_closed_region:
        assert faces.geometry.iloc[0].equals(box(0, 0, 1, 1))
    assert "source_name" not in faces.columns and "source_name" not in diagnostics.columns


def test_hosted_topology_physical_oracle_accepts_real_twelve_saves_and_rejects_corruption(tmp_path):
    import importlib
    fixture = importlib.import_module('business.scripts.online-vector-topology-fixture')
    (tmp_path / 'outputs').mkdir()
    (tmp_path / '.addp-online-owner').write_text('topology-native')
    seeded = fixture.worker('seed', tmp_path)
    connection = {'engine_type': 'nfs', 'mount_path': str(tmp_path)}
    tasks = [
        {'id': 'load', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'source.geojson'}, 'depends_on': []},
        {'id': 'build', 'operator': 'polygonize', 'params': {'input_gdf': {'$ref': 'load'}}, 'depends_on': ['load']},
    ]
    for port, name in zip(('result','diagnostics'), fixture.TARGETS):
        reference = {'$ref': 'build'} if port == 'result' else {'$ref': 'build', 'port': port}
        tasks.append({'id': 'save_' + port, 'operator': 'save', 'depends_on': ['build'], 'params': {
            'input_df': reference, 'connection_info': connection, 'path': 'outputs/' + name, 'mode': 'fail'}})
    tasks.extend([
        {'id': 'invalid', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'invalid.geojson'}, 'depends_on': []},
        {'id': 'check', 'operator': 'check_geometry', 'params': {'input_gdf': {'$ref': 'invalid'}}, 'depends_on': ['invalid']},
        {'id': 'repair', 'operator': 'repair_geometry', 'params': {'input_gdf': {'$ref': 'check'}}, 'depends_on': ['check']},
        {'id': 'recheck', 'operator': 'check_geometry', 'params': {'input_gdf': {'$ref': 'repair'}, 'valid_field': 'after_valid', 'reason_field': 'after_reason'}, 'depends_on': ['repair']},
        {'id': 'save_repaired', 'operator': 'save', 'params': {'input_df': {'$ref': 'recheck'}, 'connection_info': connection, 'path': 'outputs/repaired.geojson', 'mode': 'fail'}, 'depends_on': ['recheck']},
    ])
    tasks.extend([
        {'id': 'snap_input', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'snap-input.gpkg'}, 'depends_on': []},
        {'id': 'snap_reference', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'snap-reference.gpkg'}, 'depends_on': []},
        {'id': 'snap', 'operator': 'snap_geometry', 'params': {'input_gdf': {'$ref': 'snap_input'}, 'reference_gdf': {'$ref': 'snap_reference'}, 'tolerance': .75}, 'depends_on': ['snap_input','snap_reference']},
        {'id': 'save_snapped', 'operator': 'save', 'params': {'input_df': {'$ref': 'snap'}, 'connection_info': connection, 'path': 'outputs/snapped.gpkg', 'mode': 'fail'}, 'depends_on': ['snap']},
    ])
    tasks.extend([
        {'id': 'overlap_input', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'overlap-input.gpkg'}, 'depends_on': []},
        {'id': 'overlap', 'operator': 'check_polygon_overlap', 'params': {'input_gdf': {'$ref': 'overlap_input'}, 'id_field': 'parcel_id'}, 'depends_on': ['overlap_input']},
        {'id': 'save_overlaps', 'operator': 'save', 'params': {'input_df': {'$ref': 'overlap'}, 'connection_info': connection, 'path': 'outputs/overlaps.gpkg', 'mode': 'fail'}, 'depends_on': ['overlap']},
    ])
    tasks.extend([
        {'id': 'gap_input', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'gap-input.gpkg'}, 'depends_on': []},
        {'id': 'gap_coverage', 'operator': 'load', 'params': {'connection_info': connection, 'path': 'gap-coverage.gpkg'}, 'depends_on': []},
        {'id': 'gap', 'operator': 'check_polygon_gap', 'params': {'input_gdf': {'$ref': 'gap_input'}, 'coverage_gdf': {'$ref': 'gap_coverage'}}, 'depends_on': ['gap_input','gap_coverage']},
        {'id': 'save_gaps', 'operator': 'save', 'params': {'input_df': {'$ref': 'gap'}, 'connection_info': connection, 'path': 'outputs/gaps.gpkg', 'mode': 'fail'}, 'depends_on': ['gap']},
    ])
    tasks.extend([
        {'id':'dangling_input','operator':'load','params':{'connection_info':connection,'path':'dangling-input.gpkg'},'depends_on':[]},
        {'id':'dangling','operator':'check_dangling_points','params':{'input_gdf':{'$ref':'dangling_input'},'id_field':'route_key'},'depends_on':['dangling_input']},
        {'id':'save_dangling','operator':'save','params':{'input_df':{'$ref':'dangling'},'connection_info':connection,'path':'outputs/dangling.gpkg','mode':'fail'},'depends_on':['dangling']},
    ])
    tasks.extend([
        {'id':'line_overlap_input','operator':'load','params':{'connection_info':connection,'path':'line-overlap-input.gpkg'},'depends_on':[]},
        {'id':'line_overlap','operator':'check_line_overlap','params':{'input_gdf':{'$ref':'line_overlap_input'},'id_field':'route_key'},'depends_on':['line_overlap_input']},
        {'id':'save_line_overlaps','operator':'save','params':{'input_df':{'$ref':'line_overlap'},'connection_info':connection,'path':'outputs/line-overlaps.gpkg','mode':'fail'},'depends_on':['line_overlap']},
    ])
    tasks.extend([
        {'id':'line_self_overlap_input','operator':'load','params':{'connection_info':connection,'path':'line-self-overlap-input.gpkg'},'depends_on':[]},
        {'id':'line_self_overlap','operator':'check_line_self_overlap','params':{'input_gdf':{'$ref':'line_self_overlap_input'},'id_field':'route_key'},'depends_on':['line_self_overlap_input']},
        {'id':'save_line_self_overlaps','operator':'save','params':{'input_df':{'$ref':'line_self_overlap'},'connection_info':connection,'path':'outputs/line-self-overlaps.gpkg','mode':'fail'},'depends_on':['line_self_overlap']},
    ])
    tasks.extend([
        {'id':'line_intersection_input','operator':'load','params':{'connection_info':connection,'path':'line-intersection-input.gpkg'},'depends_on':[]},
        {'id':'line_intersection','operator':'check_line_intersection','params':{'input_gdf':{'$ref':'line_intersection_input'},'id_field':'route_key'},'depends_on':['line_intersection_input']},
        {'id':'save_line_intersections','operator':'save','params':{'input_df':{'$ref':'line_intersection'},'connection_info':connection,'path':'outputs/line-intersections.gpkg','mode':'fail'},'depends_on':['line_intersection']},
    ])
    tasks.extend([
        {'id':'line_self_intersection_input','operator':'load','params':{'connection_info':connection,'path':'line-self-intersection-input.gpkg'},'depends_on':[]},
        {'id':'line_self_intersection','operator':'check_line_self_intersection','params':{'input_gdf':{'$ref':'line_self_intersection_input'},'id_field':'route_key'},'depends_on':['line_self_intersection_input']},
        {'id':'save_line_self_intersections','operator':'save','params':{'input_df':{'$ref':'line_self_intersection'},'connection_info':connection,'path':'outputs/line-self-intersections.gpkg','mode':'fail'},'depends_on':['line_self_intersection']},
    ])
    tasks.extend([
        {'id':'split_line_input','operator':'load','params':{'connection_info':connection,'path':'split-line-input.gpkg'},'depends_on':[]},
        {'id':'split_line','operator':'split_line_intersections','params':{'input_gdf':{'$ref':'split_line_input'},'id_field':'route_key'},'depends_on':['split_line_input']},
        {'id':'save_split_lines','operator':'save','params':{'input_df':{'$ref':'split_line'},'connection_info':connection,'path':'outputs/split-lines.gpkg','mode':'fail'},'depends_on':['split_line']},
    ])
    execution = execute_workflow({'tasks': tasks})
    assert execution['status'] == 'success', execution.get('error')
    actual = fixture.worker('verify', tmp_path)
    assert actual['sources'] == seeded['sources']
    assert [actual['targets'][name]['feature_count'] for name in fixture.TARGETS] == [4,4,2,5,7,8,17,9,8,10,15,20]
    for name in fixture.TARGETS[:3]:
        path = tmp_path / 'outputs' / name
        original = path.read_bytes()
        content = json.loads(original)
        if name == 'faces.geojson':
            content['features'][0]['geometry']['coordinates'][0][0][0] += .1
        elif name == 'diagnostics.geojson':
            content['features'][0]['properties']['issue_type'] = 'cut_edge'
        else:
            content['features'][0]['properties']['after_valid'] = False
        path.write_text(json.dumps(content))
        with pytest.raises(fixture.FixtureError):
            fixture.worker('verify', tmp_path)
        path.write_bytes(original)


    # Corrupt the real GeoPackage, not an in-memory mock of the snap result.
    from osgeo import ogr, osr
    path = tmp_path / 'outputs/snapped.gpkg'; original = path.read_bytes()
    for problem in ('chain', 'boundary', 'tie', 'insert', 'attribute', 'crs', 'count'):
        dataset = ogr.Open(str(path), 1); layer = dataset.GetLayer(0)
        if problem == 'crs':
            dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem == 'count':
            layer.DeleteFeature(1)
        else:
            feature = layer.GetFeature(1 if problem in ('chain','insert','attribute') else 2 if problem=='boundary' else 3)
            if problem == 'attribute': feature.SetField('grade', 999)
            else:
                coordinates = [[1,0],[10,0]] if problem=='chain' else [[.75,10],[10,10]] if problem=='boundary' else [[.5,20],[10,20]] if problem=='tie' else [[.5,0],[1,0],[10,0]]
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps({'type':'LineString','coordinates':coordinates})))
            layer.SetFeature(feature)
        dataset = None
        with pytest.raises(fixture.FixtureError): fixture.worker('verify', tmp_path)
        path.write_bytes(original)
    path = tmp_path / 'outputs/overlaps.gpkg'; original = path.read_bytes()
    for problem in ('source_id', 'area', 'tiny_area', 'part', 'coordinate', 'crs', 'count'):
        dataset = ogr.Open(str(path), 1); layer = dataset.GetLayer(0)
        if problem == 'crs':
            dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem == 'count':
            layer.DeleteFeature(7)
        else:
            feature = layer.GetFeature(7 if problem=='tiny_area' else 6 if problem=='part' else 1)
            if problem == 'source_id': feature.SetField('source_id_a', '共边')
            elif problem in ('area','tiny_area'): feature.SetField('overlap_area', 0)
            else:
                shape = json.loads(feature.GetGeometryRef().ExportToJson())
                if problem == 'part': shape['coordinates'].pop()
                else: shape['coordinates'][0][0][0][0] += .25
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(shape)))
            layer.SetFeature(feature)
        dataset = None
        with pytest.raises(fixture.FixtureError): fixture.worker('verify', tmp_path)
        path.write_bytes(original)
    path = tmp_path / 'outputs/gaps.gpkg'; original = path.read_bytes()
    for problem in ('area','tiny_area','coordinate','hole','filled','crs','count','order','duplicate'):
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
        if problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='count':layer.DeleteFeature(8)
        elif problem in ('order','duplicate'):
            first,second=layer.GetFeature(1),layer.GetFeature(2)
            shape=first.GetGeometryRef().Clone();first.SetGeometry(second.GetGeometryRef())
            if problem=='order':layer.SetFeature(first)
            second.SetGeometry(shape);layer.SetFeature(second)
        else:
            feature=layer.GetFeature(8 if problem=='tiny_area' else 3 if problem=='hole' else 1)
            if problem in ('area','tiny_area'):feature.SetField('gap_area',0 if problem=='area' else 1e-12)
            else:
                geometry=json.loads(feature.GetGeometryRef().ExportToJson())
                if problem=='hole':geometry['coordinates'].pop()
                elif problem=='filled':geometry={'type':'Polygon','coordinates':[fixture.rectangle(100,0,106,4)]}
                else:geometry['coordinates'][0][0][0]-=.25
                changed=ogr.CreateGeometryFromJson(json.dumps(geometry));feature.SetGeometry(changed)
                feature.SetField('gap_area',changed.GetArea())
            layer.SetFeature(feature)
        dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    for name in ('gap-input.gpkg','gap-coverage.gpkg'):
        path=tmp_path/name;original=path.read_bytes()
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0);feature=layer.GetFeature(1)
        feature.SetField('source_name','foreign-source');layer.SetFeature(feature);dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    path=tmp_path/'outputs/dangling.gpkg';original=path.read_bytes()
    for problem in ('source_id','fid_as_id','coordinate','tiny','count','duplicate','order','crs'):
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
        if problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='count':layer.DeleteFeature(17)
        elif problem in ('duplicate','order'):
            first,second=layer.GetFeature(1),layer.GetFeature(2);shape=first.GetGeometryRef().Clone()
            if problem=='order':first.SetGeometry(second.GetGeometryRef());assert layer.SetFeature(first)==0
            second.SetGeometry(shape);assert layer.SetFeature(second)==0
        else:
            feature=layer.GetFeature(14 if problem=='tiny' else 1)
            if problem in ('source_id','fid_as_id'):feature.SetField('source_id','闭环' if problem=='source_id' else '1')
            else:
                geometry=feature.GetGeometryRef();point=ogr.Geometry(ogr.wkbPoint)
                point.AddPoint_2D(251 if problem=='tiny' else geometry.GetX()+.25,geometry.GetY());feature.SetGeometry(point)
            assert layer.SetFeature(feature)==0
        dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    path=tmp_path/'dangling-input.gpkg';original=path.read_bytes()
    dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0);feature=layer.GetFeature(1)
    feature.SetField('route_key','foreign-source');assert layer.SetFeature(feature)==0;dataset=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
    path.write_bytes(original)
    path=tmp_path/'outputs/line-overlaps.gpkg';original=path.read_bytes()
    for problem in ('source_id','fid_as_id','length','tiny','part','coordinate','count','duplicate','order','crs'):
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
        if problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='count':layer.DeleteFeature(9)
        elif problem in ('duplicate','order'):
            first,second=layer.GetFeature(1),layer.GetFeature(2)
            props={key:first.GetField(key) for key in ('source_id_a','source_id_b','overlap_length')}
            shape=first.GetGeometryRef().Clone()
            if problem=='order':
                for key in props:first.SetField(key,second.GetField(key))
                first.SetGeometry(second.GetGeometryRef());assert layer.SetFeature(first)==0
            for key,value in props.items():second.SetField(key,value)
            second.SetGeometry(shape);assert layer.SetFeature(second)==0
        else:
            feature=layer.GetFeature(7 if problem=='tiny' else 6 if problem=='part' else 1)
            if problem in ('source_id','fid_as_id'):feature.SetField('source_id_a','接点' if problem=='source_id' else '1')
            elif problem in ('length','tiny'):feature.SetField('overlap_length',0 if problem=='length' else 1e-12)
            else:
                shape=json.loads(feature.GetGeometryRef().ExportToJson())
                if problem=='part':shape['coordinates'].pop()
                else:shape['coordinates'][0][0][0]+=.25
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(shape)))
            assert layer.SetFeature(feature)==0
        dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    path=tmp_path/'line-overlap-input.gpkg';original=path.read_bytes()
    dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0);feature=layer.GetFeature(1)
    feature.SetField('route_key','foreign-source');assert layer.SetFeature(feature)==0;dataset=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
    path.write_bytes(original)
    path=tmp_path/'outputs/line-self-overlaps.gpkg';original=path.read_bytes()
    for problem in ('source_id','fid_as_id','length','tiny','part','coordinate','count','duplicate','order','crs'):
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
        if problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='count':layer.DeleteFeature(8)
        elif problem in ('duplicate','order'):
            first,second=layer.GetFeature(1),layer.GetFeature(2)
            props={key:first.GetField(key) for key in ('source_id','overlap_length')}
            shape=first.GetGeometryRef().Clone()
            if problem=='order':
                for key in props:first.SetField(key,second.GetField(key))
                first.SetGeometry(second.GetGeometryRef());assert layer.SetFeature(first)==0
            for key,value in props.items():second.SetField(key,value)
            second.SetGeometry(shape);assert layer.SetFeature(second)==0
        else:
            feature=layer.GetFeature(5 if problem=='tiny' else 4 if problem=='part' else 1)
            if problem in ('source_id','fid_as_id'):feature.SetField('source_id','接点' if problem=='source_id' else '1')
            elif problem in ('length','tiny'):feature.SetField('overlap_length',0 if problem=='length' else 1e-12)
            else:
                shape=json.loads(feature.GetGeometryRef().ExportToJson())
                if problem=='part':shape['coordinates'].pop()
                else:shape['coordinates'][0][0][0]+=.25
                feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps(shape)))
            assert layer.SetFeature(feature)==0
        dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    path=tmp_path/'line-self-overlap-input.gpkg';original=path.read_bytes()
    dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0);feature=layer.GetFeature(1)
    feature.SetField('route_key','foreign-source');assert layer.SetFeature(feature)==0;dataset=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
    path.write_bytes(original)
    path=tmp_path/'outputs/line-intersections.gpkg';original=path.read_bytes()
    for problem in ('source_a','source_b','fid_as_id','coordinate','tiny','count','duplicate','order','crs'):
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
        if problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem=='count':layer.DeleteFeature(10)
        elif problem in ('duplicate','order'):
            first,second=layer.GetFeature(1),layer.GetFeature(2)
            props={key:first.GetField(key) for key in ('source_id_a','source_id_b')};shape=first.GetGeometryRef().Clone()
            if problem=='order':
                for key in props:first.SetField(key,second.GetField(key))
                first.SetGeometry(second.GetGeometryRef());assert layer.SetFeature(first)==0
            for key,value in props.items():second.SetField(key,value)
            second.SetGeometry(shape);assert layer.SetFeature(second)==0
        else:
            feature=layer.GetFeature(10 if problem=='tiny' else 1)
            if problem in ('source_a','source_b','fid_as_id'):
                feature.SetField('source_id_b' if problem=='source_b' else 'source_id_a','1' if problem=='fid_as_id' else '叠甲')
            else:
                shape=feature.GetGeometryRef().Clone();shape.SetPoint_2D(0,0 if problem=='tiny' else 603,120 if problem=='tiny' else 100);feature.SetGeometry(shape)
            assert layer.SetFeature(feature)==0
        dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    path=tmp_path/'line-intersection-input.gpkg';original=path.read_bytes()
    dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0);feature=layer.GetFeature(1)
    feature.SetField('route_key','foreign-source');assert layer.SetFeature(feature)==0;dataset=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
    path.write_bytes(original)
    path=tmp_path/'outputs/line-self-intersections.gpkg';original=path.read_bytes()
    for problem in ('source','fid_as_id','coordinate','tiny','count','duplicate','order','crs','overlap-point','global-dedup'):
        dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0)
        if problem=='crs':dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
        elif problem in ('count','global-dedup'):layer.DeleteFeature(15 if problem=='count' else 11)
        elif problem in ('duplicate','order'):
            first,second=layer.GetFeature(1),layer.GetFeature(2)
            identity=first.GetField('source_id');shape=first.GetGeometryRef().Clone()
            if problem=='order':
                first.SetField('source_id',second.GetField('source_id'));first.SetGeometry(second.GetGeometryRef());assert layer.SetFeature(first)==0
            second.SetField('source_id',identity);second.SetGeometry(shape);assert layer.SetFeature(second)==0
        else:
            feature=layer.GetFeature(9 if problem=='tiny' else 1)
            if problem in ('source','fid_as_id'):feature.SetField('source_id','1' if problem=='fid_as_id' else '重复')
            else:
                shape=feature.GetGeometryRef().Clone()
                shape.SetPoint_2D(0,0 if problem=='tiny' else 902 if problem=='overlap-point' else 802,160 if problem=='tiny' else 140 if problem=='overlap-point' else 141)
                if problem=='overlap-point':feature.SetField('source_id','叠支')
                feature.SetGeometry(shape)
            assert layer.SetFeature(feature)==0
        dataset=None
        with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
        path.write_bytes(original)
    path=tmp_path/'line-self-intersection-input.gpkg';original=path.read_bytes()
    dataset=ogr.Open(str(path),1);layer=dataset.GetLayer(0);feature=layer.GetFeature(1)
    feature.SetField('route_key','foreign-source');assert layer.SetFeature(feature)==0;dataset=None
    with pytest.raises(fixture.FixtureError):fixture.worker('verify',tmp_path)
    path.write_bytes(original)
    baseline = fixture.worker('verify', tmp_path)
    failed_tasks = [task.copy() for task in tasks if task['id'] in ('snap_input','snap_reference','snap','save_snapped')]
    failed_tasks[2]['params']['tolerance'] = 1.25
    failed_tasks[3]['params']['path'] = 'outputs/failed-snap.gpkg'
    failed = execute_workflow({'tasks': failed_tasks})
    assert failed['status'] == 'failed'
    assert 'snapping produced invalid geometries' in str(failed)
    assert not (tmp_path/'outputs/failed-snap.gpkg').exists()
    assert fixture.worker('verify', tmp_path) == baseline


@pytest.mark.parametrize("problem", ["source_id", "route_key", "null", "grade", "part_no", "segment_no", "direction", "tiny", "count", "crs", "field_type"])
def test_hosted_split_line_fixture_rejects_real_physical_corruption(tmp_path, problem):
    import importlib
    from osgeo import ogr
    fixture = importlib.import_module('business.scripts.online-vector-topology-fixture')
    fixture.seed_split_line(tmp_path/'source.gpkg')
    before = (tmp_path/'source.gpkg').read_bytes()
    connection = {'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks = [
        {'id':'load','operator':'load','params':{'connection_info':connection,'path':'source.gpkg'},'depends_on':[]},
        {'id':'split','operator':'split_line_intersections','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key'},'depends_on':['load']},
        {'id':'save','operator':'save','params':{'input_df':{'$ref':'split'},'connection_info':connection,'path':'pieces.gpkg','mode':'fail'},'depends_on':['split']},
    ]
    execution = execute_workflow({'tasks':tasks})
    assert execution['status']=='success', execution
    assert fixture.verify_split_line(tmp_path/'pieces.gpkg')['feature_count']==20
    assert fixture.verify_split_line(tmp_path/'source.gpkg',source=True)['feature_count']==11
    assert (tmp_path/'source.gpkg').read_bytes()==before
    dataset = ogr.Open(str(tmp_path/'pieces.gpkg'),1); layer=dataset.GetLayer(0)
    if problem=='count': layer.DeleteFeature(20)
    elif problem=='crs': dataset.ExecuteSQL('UPDATE gpkg_geometry_columns SET srs_id=4326')
    elif problem=='field_type':
        assert layer.AlterFieldDefn(layer.GetLayerDefn().GetFieldIndex('grade'),ogr.FieldDefn('grade',ogr.OFTReal),ogr.ALTER_TYPE_FLAG)==0
    else:
        feature=layer.GetFeature(4 if problem=='null' else 16 if problem=='tiny' else 1)
        if problem in ('source_id','route_key'): feature.SetField(problem,'1')
        elif problem=='null': feature.SetField('note','')
        elif problem in ('grade','part_no','segment_no'): feature.SetField(problem,999)
        else:
            geometry=feature.GetGeometryRef()
            points=[list(point[:2]) for point in geometry.GetPoints()]
            if problem=='direction': points.reverse()
            else: points[-1][0]=1e-12
            feature.SetGeometry(ogr.CreateGeometryFromJson(json.dumps({'type':'LineString','coordinates':points})))
        assert layer.SetFeature(feature)==0
    dataset=None
    with pytest.raises(fixture.FixtureError): fixture.verify_split_line(tmp_path/'pieces.gpkg')
    assert (tmp_path/'source.gpkg').read_bytes()==before


# Single-feature validity is distinct from polygonize's whole-network face building.
def test_geometry_check_preserves_features_and_reports_valid_invalid_missing_and_empty():
    bow = Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)])
    frame = lines([box(0, 0, 1, 1), bow, None, Polygon(), Point(2, 3)])
    frame.index = [9, 9, 3, 7, 1]
    before = frame.copy(deep=True)
    checked = get_operator("check_geometry")(frame)
    assert checked.geometry_valid.tolist() == [True, False, False, True, True]
    assert checked.geometry_reason.iloc[0] == "Valid Geometry"
    assert checked.geometry_reason.iloc[1].startswith("Self-intersection")
    assert checked.geometry_reason.iloc[2] == "Missing Geometry"
    assert str(checked.geometry_valid.dtype) == "bool"
    assert str(checked.geometry_reason.dtype) == "string"
    assert_geodataframe_equal(checked.drop(columns=["geometry_valid", "geometry_reason"]), before)
    assert_geodataframe_equal(frame, before)


@pytest.mark.parametrize("kwargs", [
    {"valid_field": "source_name"}, {"reason_field": "boundary"},
    {"valid_field": "x", "reason_field": "x"}, {"valid_field": ""},
    {"reason_field": " reason "}, {"valid_field": 2},
])
def test_geometry_check_rejects_output_field_collision_and_bad_names(kwargs):
    with pytest.raises(ValueError, match="field"):
        get_operator("check_geometry")(lines([Point(0, 0)]), **kwargs)


def test_geometry_repair_keeps_multipart_collapsed_parts_and_valid_source_exactly():
    bow = Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)])
    collapsed = Polygon([(0, 0), (1, 1), (1, 2), (1, 1), (0, 0)])
    mixed = Polygon([(0, 0), (2, 0), (2, 2), (1, 1), (1, 2), (1, 1), (0, 0)])
    frame = lines([bow, collapsed, mixed, box(9, 9, 10, 10), None, Polygon()])
    frame.index = [5, 5, 2, 8, 4, 1]
    before = frame.copy(deep=True)
    repaired = get_operator("repair_geometry")(frame)
    assert repaired.geometry.iloc[0].geom_type == "MultiPolygon"
    assert sorted(part.area for part in repaired.geometry.iloc[0].geoms) == [1, 1]
    assert repaired.geometry.iloc[1].geom_type == "MultiLineString"
    assert repaired.geometry.iloc[1].equals(MultiLineString([[(0, 0), (1, 1)], [(1, 1), (1, 2)]]))
    assert repaired.geometry.iloc[2].geom_type == "GeometryCollection"
    assert {part.geom_type for part in repaired.geometry.iloc[2].geoms} == {"Polygon", "LineString"}
    assert repaired.geometry.iloc[3].wkb == frame.geometry.iloc[3].wkb
    assert repaired.geometry.iloc[4] is None and repaired.geometry.iloc[5].is_empty
    assert repaired.geometry.dropna().is_valid.all()
    assert repaired.drop(columns="boundary").equals(frame.drop(columns="boundary"))
    assert repaired.geometry.name == "boundary" and repaired.crs == frame.crs
    assert_geodataframe_equal(frame, before)


@pytest.mark.parametrize("crs", [None, "OGC:CRS84", "EPSG:3857"])
def test_geometry_validity_empty_batch_and_custom_fields_keep_crs(crs):
    frame = lines([], crs=crs)
    checked = get_operator("check_geometry")(frame, valid_field="ok", reason_field="why")
    assert checked.columns.tolist() == ["source_name", "boundary", "ok", "why"]
    assert checked.empty and checked.crs == frame.crs
    assert str(checked.ok.dtype) == "bool" and str(checked.why.dtype) == "string"
    assert_geodataframe_equal(get_operator("repair_geometry")(frame), frame)


@pytest.mark.parametrize("name", ["check_geometry", "repair_geometry"])
@pytest.mark.parametrize("frame", [None, pd.DataFrame({"x": [1]}), gpd.GeoDataFrame({"x": [1]})])
def test_geometry_validity_requires_active_geodataframe(name, frame):
    with pytest.raises(ValueError, match="GeoDataFrame|geometry"):
        get_operator(name)(frame)


@pytest.mark.parametrize("name", ["check_geometry", "repair_geometry"])
@pytest.mark.parametrize("wkt", ["POINT Z (1 2 3)", "POINT M (1 2 3)", "GEOMETRYCOLLECTION (POINT Z (1 2 3))", "POINT (Infinity 2)"])
def test_geometry_validity_rejects_nonfinite_or_extra_dimensions(name, wkt):
    frame = lines([shapely.from_wkt(wkt)])
    with pytest.raises(ValueError, match="finite|XY"):
        get_operator(name)(frame)


def test_geometry_repair_workflow_check_recheck_save_reload_and_discovery(tmp_path):
    source = lines([Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)])], crs="OGC:CRS84")
    source.to_file(tmp_path / "invalid.geojson", driver="GeoJSON")
    original = (tmp_path / "invalid.geojson").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    execution = execute_workflow({"tasks": [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "invalid.geojson"}, "depends_on": []},
        {"id": "check", "operator": "check_geometry", "params": {"input_gdf": {"$ref": "load"}}, "depends_on": ["load"]},
        {"id": "repair", "operator": "repair_geometry", "params": {"input_gdf": {"$ref": "check"}}, "depends_on": ["check"]},
        {"id": "recheck", "operator": "check_geometry", "params": {"input_gdf": {"$ref": "repair"}, "valid_field": "after_valid", "reason_field": "after_reason"}, "depends_on": ["repair"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "recheck"}, "connection_info": connection, "path": "repaired.geojson", "mode": "fail"}, "depends_on": ["recheck"]},
    ]})
    assert execution["status"] == "success", execution.get("error")
    output = gpd.read_file(tmp_path / "repaired.geojson")
    assert output.source_name.tolist() == ["line-0"]
    assert output.geometry_valid.tolist() == [False] and output.after_valid.tolist() == [True]
    assert output.geometry.iloc[0].geom_type == "MultiPolygon" and output.geometry.iloc[0].area == 2
    assert output.crs == source.crs
    assert (tmp_path / "invalid.geojson").read_bytes() == original
    import api_server
    with api_server.app.test_client() as client:
        discovered = {op["id"]: op for op in client.get("/api/operators").get_json()["operators"]}
        for name in ["check_geometry", "repair_geometry"]:
            op = discovered[name]
            assert op["category_path"] == ["矢量处理", "几何处理"]
            assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
            assert [(p["name"], p["type"], p["is_default"]) for p in op["output_ports"]] == [("result", "geodataframe", True)]


@pytest.mark.parametrize("name", ["check_geometry", "repair_geometry"])
def test_geometry_validity_runtime_async_requires_context_and_rejects_direct(name, monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    monkeypatch.setattr(api_server, "executions", ExecutionRegistry())
    frame = lines([Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)])])
    params = {"input_gdf": json.loads(frame.to_json())}
    with api_server.app.test_client() as client:
        assert client.post(f"/api/operators/{name}/invoke", json={"params": params}).status_code == 403
        workflow = {"tasks": [{"id": "compute", "operator": name, "params": params, "depends_on": []}]}
        assert client.post("/api/workflow", json={"workflow_def": workflow}).status_code == 400
        response = client.post("/api/workflow", json={"workflow_def": workflow, "runtime": {
            "tenant_id": 7, "execution_authorization": {"id": 1, "effects": ["read"]},
        }})
        assert response.status_code == 202
        execution_id = response.get_json()["execution_id"]
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            state = client.get("/api/executions/" + execution_id).get_json()
            if state["status"] in {"success", "failed"}:
                break
            time.sleep(0.01)
        assert state["status"] == "success", state
        feature = json.loads(state["result"])["features"][0]
        if name == "check_geometry":
            assert feature["properties"]["geometry_valid"] is False
            assert feature["geometry"]["type"] == "Polygon"
        else:
            assert feature["geometry"]["type"] == "MultiPolygon"
        assert feature["properties"]["source_name"] == "line-0"


@pytest.mark.parametrize("crs", [None, "OGC:CRS84", "EPSG:3857"])
def test_geometry_repair_keeps_crs_and_repairs_hole_outside_shell_and_degenerate_line(crs):
    misplaced_hole = Polygon(box(0, 0, 2, 2).exterior.coords, [box(3, 3, 4, 4).exterior.coords])
    frame = lines([misplaced_hole, LineString([(5, 5), (5, 5)]), Point(6, 6)], crs=crs)
    repaired = get_operator("repair_geometry")(frame)
    assert repaired.crs == frame.crs and repaired.geometry.name == frame.geometry.name
    assert repaired.geometry.iloc[0].equals(shapely.union_all([box(0, 0, 2, 2), box(3, 3, 4, 4)]))
    assert repaired.geometry.iloc[1].equals(Point(5, 5))
    assert repaired.geometry.iloc[2].wkb == frame.geometry.iloc[2].wkb
    assert repaired.is_valid.all()


def test_geometry_repair_saves_geometry_collection_and_collapsed_lines_without_discarding(tmp_path):
    mixed = Polygon([(0, 0), (2, 0), (2, 2), (1, 1), (1, 2), (1, 1), (0, 0)])
    collapsed = Polygon([(0, 0), (1, 1), (1, 2), (1, 1), (0, 0)])
    frame = lines([mixed, collapsed], crs="OGC:CRS84")
    repaired = get_operator("repair_geometry")(frame)
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    get_operator("save")(input_df=repaired, connection_info=connection, path="mixed.geojson", mode="fail")
    reloaded = get_operator("load")(connection_info=connection, path="mixed.geojson")
    assert reloaded.geom_type.tolist() == ["GeometryCollection", "MultiLineString"]
    assert reloaded.source_name.tolist() == frame.source_name.tolist()
    assert reloaded.crs == frame.crs
    assert all(actual.equals(expected) for actual, expected in zip(reloaded.geometry, repaired.geometry))


def snap(frame, reference, tolerance):
    return get_operator("snap_geometry")(input_gdf=frame, reference_gdf=reference, tolerance=tolerance)


def test_reference_snap_preserves_rows_attributes_reference_and_uses_all_reference_vertices():
    frame = lines([Point(.1, .1), LineString([(10.1, 0), (20, 0)]), None, Point()])
    frame.index = [7, 7, 2, 9]
    frame['grade'] = pd.array([1, 2, None, 4], dtype='Int64')
    reference = lines([Point(0, 0), LineString([(10, 0), (10, 4)]), None, Point(), Point(0, 0)])
    reference.index = [99, 3, 8, 8, 0]
    before, reference_before = frame.copy(deep=True), reference.copy(deep=True)
    result = snap(frame, reference, .2)
    assert result.geometry.iloc[0].equals(Point(0, 0))
    assert list(result.geometry.iloc[1].coords) == [(10, 0), (20, 0)]
    assert result.geometry.iloc[2] is None and result.geometry.iloc[3].is_empty
    assert result.geometry.name == 'boundary' and result.crs == frame.crs
    pd.testing.assert_frame_equal(result.drop(columns='boundary'), before.drop(columns='boundary'))
    assert result.index.tolist() == [7, 7, 2, 9]
    assert_geodataframe_equal(frame, before)
    assert_geodataframe_equal(reference, reference_before)
    assert_geodataframe_equal(snap(frame, reference.iloc[::-1], .2), result)


@pytest.mark.parametrize('crs', ['EPSG:3857', 'EPSG:2263'])
def test_reference_snap_tolerance_is_in_coordinate_units_including_feet(crs):
    frame, reference = lines([Point(.25, 0)], crs), lines([Point(0, 0)], crs)
    assert snap(frame, reference, .3).geometry.iloc[0].equals(Point(0, 0))
    assert snap(frame, reference, .2).geometry.iloc[0].equals(Point(.25, 0))


def test_reference_snap_zero_and_exact_tolerance_boundary_are_unchanged():
    frame, reference = lines([Point(1, 0)]), lines([Point(0, 0)])
    assert_geodataframe_equal(snap(frame, reference, 0), frame)
    assert_geodataframe_equal(snap(frame, reference, 1), frame)
    assert snap(frame, reference, np.nextafter(1., 2.)).geometry.iloc[0].equals(Point(0, 0))


def test_reference_snap_does_not_insert_vertices_project_to_segments_or_new_intersections():
    frame = lines([LineString([(0, 0), (10, 0)])])
    assert_geodataframe_equal(snap(frame, lines([Point(5, 0)]), 0), frame)
    result = snap(frame, lines([Point(5, .1)]), .2)
    assert_geodataframe_equal(result, frame)
    point = lines([Point(5, .1)])
    assert_geodataframe_equal(snap(point, lines([LineString([(0, 0), (10, 0)])]), .2), point)
    crossing = lines([LineString([(0, 0), (10, 0)]), LineString([(5, -5), (5, 5)])])
    assert_geodataframe_equal(snap(point, crossing, .2), point)


def test_reference_snap_preserves_polygon_holes_and_nested_multipart_geometry():
    polygon = Polygon(box(0, 0, 10, 10).exterior.coords, [box(3, 3, 4, 4).exterior.coords])
    frame = lines([polygon, shapely.GeometryCollection([Point(.1, .1), LineString([(20.1, 0), (25, 0)])])])
    reference = lines([Point(.1, 0), Point(0, 0), Point(20, 0)])
    result = snap(frame, reference, .2)
    assert result.geometry.iloc[0].geom_type == 'Polygon'
    assert len(result.geometry.iloc[0].interiors) == 1
    assert result.geometry.iloc[0].interiors[0].equals(polygon.interiors[0])
    mixed = result.geometry.iloc[1]
    assert mixed.geom_type == 'GeometryCollection' and len(mixed.geoms) == 2
    assert mixed.geoms[1].equals(LineString([(20, 0), (25, 0)]))
    assert result.is_valid.all()


def test_reference_snap_tied_candidates_have_a_stable_reference_vertex_order():
    frame = lines([Point(0, 0)])
    reference = lines([Point(1, 0), Point(-1, 0), Point(1, 0)])
    first = snap(frame, reference, 1.5)
    assert first.geometry.iloc[0].equals(Point(-1, 0))
    assert_geodataframe_equal(first, snap(frame, reference.iloc[::-1], 1.5))


@pytest.mark.parametrize('reference', [[], [None, Point()]])
def test_reference_snap_empty_reference_is_identity_and_empty_input_keeps_schema(reference):
    frame = lines([Point(1, 2)])
    assert_geodataframe_equal(snap(frame, lines(reference), 1), frame)
    empty = lines([])
    assert_geodataframe_equal(snap(empty, lines([Point(0, 0)]), 1), empty)


@pytest.mark.parametrize('tolerance', [-1, np.nan, np.inf, -np.inf, True, np.bool_(False), '1', None, [1], 10**400])
def test_reference_snap_rejects_invalid_tolerance(tolerance):
    with pytest.raises(ValueError, match='tolerance'):
        snap(lines([Point(0, 0)]), lines([Point(0, 0)]), tolerance)


@pytest.mark.parametrize('source_crs,reference_crs', [
    (None, 'EPSG:3857'), ('EPSG:3857', None), (None, None),
    ('EPSG:4326', 'EPSG:4326'), ('OGC:CRS84', 'OGC:CRS84'),
    ('EPSG:3857', 'EPSG:32650'), ('EPSG:3857', 'EPSG:2263'),
])
def test_reference_snap_rejects_unknown_geographic_and_mismatched_crs(source_crs, reference_crs):
    with pytest.raises(ValueError, match='CRS'):
        snap(lines([Point(0, 0)], source_crs), lines([Point(0, 0)], reference_crs), 1)


@pytest.mark.parametrize('side', ['input', 'reference'])
@pytest.mark.parametrize('geometry,message', [
    (Point(1, 2, 3), 'two-dimensional'),
    (shapely.from_wkt('POINT M (1 2 3)'), 'two-dimensional'),
    (Point(np.inf, 0), 'finite'),
    (Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)]), 'valid'),
])
def test_reference_snap_rejects_unsupported_geometry_on_either_side(side, geometry, message):
    bad, good = lines([geometry]), lines([Point(0, 0)])
    with pytest.raises(ValueError, match=message):
        snap(bad if side == 'input' else good, bad if side == 'reference' else good, 1)


@pytest.mark.parametrize('side', ['input', 'reference'])
@pytest.mark.parametrize('frame', [None, pd.DataFrame({'x': [1]}), gpd.GeoDataFrame({'x': [1]})])
def test_reference_snap_requires_two_active_geodataframes(side, frame):
    good = lines([Point(0, 0)])
    with pytest.raises(ValueError, match='GeoDataFrame|active geometry'):
        snap(frame if side == 'input' else good, frame if side == 'reference' else good, 1)


def test_reference_snap_rejects_collapsed_result_without_repair_or_partial_output():
    frame = lines([Point(5, 5), LineString([(.1, 0), (.2, 0)])])
    before = frame.copy(deep=True)
    with pytest.raises(ValueError, match='snap.*valid|invalid.*snap'):
        snap(frame, lines([Point(5.05, 5), Point(.15, 0)]), .1)
    assert_geodataframe_equal(frame, before)


def test_reference_snap_uses_original_positions_once_and_preserves_exact_reference_hits():
    frame = lines([Point(0, 0), Point(.5, 0), LineString([(0, 2), (3, 2)])])
    reference = lines([Point(1, 0), Point(.5, 0), Point(.5, 2), Point(1, 2)])
    result = snap(frame, reference, .75)
    assert result.geometry.iloc[0].equals(Point(.5, 0))
    assert result.geometry.iloc[1].equals(Point(.5, 0))
    assert list(result.geometry.iloc[2].coords) == [(.5, 2), (3, 2)]
    before, after = shapely.get_coordinates(frame.geometry), shapely.get_coordinates(result.geometry)
    assert len(before) == len(after)
    assert (np.hypot(*(after-before).T) < .75).all()


@pytest.mark.parametrize('source,reference,tolerance,moves', [
    (1e200, 1.1e200, 2e199, True),
    (0., 1e-200, 2e-200, True),
    (-np.finfo(float).max, np.finfo(float).max, np.finfo(float).max, False),
])
def test_reference_snap_finite_distances_do_not_overflow_or_underflow(source, reference, tolerance, moves):
    frame = lines([Point(source, 0)])
    result = snap(frame, lines([Point(reference, 0)]), tolerance)
    assert result.geometry.iloc[0].equals(Point(reference if moves else source, 0))


def test_reference_snap_runtime_discovery_and_two_input_workflow_save_reload(tmp_path, monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    source = lines([LineString([(.1, .1), (5, 0)])])
    reference = lines([Point(0, 0)])
    for name, frame in [('source', source), ('reference', reference)]:
        frame.to_file(tmp_path / (name+'.gpkg'), layer=name, driver='GPKG')
    original = {n: (tmp_path / (n+'.gpkg')).read_bytes() for n in ['source', 'reference']}
    connection = {'engine_type': 'nfs', 'mount_path': str(tmp_path)}
    tasks = [{'id': n, 'operator': 'load', 'params': {'connection_info': connection, 'path': n+'.gpkg'}, 'depends_on': []} for n in ['source', 'reference']]
    tasks += [
        {'id': 'snap', 'operator': 'snap_geometry', 'params': {'input_gdf': {'$ref': 'source'}, 'reference_gdf': {'$ref': 'reference'}, 'tolerance': .2}, 'depends_on': ['source', 'reference']},
        {'id': 'save', 'operator': 'save', 'params': {'input_df': {'$ref': 'snap'}, 'connection_info': connection, 'path': 'snapped.gpkg', 'mode': 'fail'}, 'depends_on': ['snap']},
    ]
    execution = execute_workflow({'tasks': tasks})
    assert execution['status'] == 'success', execution.get('error')
    saved = gpd.read_file(tmp_path/'snapped.gpkg')
    assert list(saved.geometry.iloc[0].coords) == [(0, 0), (5, 0)]
    assert saved.source_name.tolist() == source.source_name.tolist() and saved.crs == source.crs
    assert {n: (tmp_path / (n+'.gpkg')).read_bytes() for n in original} == original
    monkeypatch.setattr(api_server, 'executions', ExecutionRegistry())
    with api_server.app.test_client() as client:
        op = next(o for o in client.get('/api/operators').get_json()['operators'] if o['id']=='snap_geometry')
        assert op['display_name']=='参考吸附' and op['category_path']==['矢量处理', '几何处理']
        assert op['execution_modes']==['workflow'] and op['effects']==['read']
        assert [(x['name'],x['param_type']) for x in op['parameters']]==[('input_gdf','input'),('reference_gdf','input'),('tolerance','param')]
        assert [(x['name'],x['type'],x['is_default']) for x in op['output_ports']]==[('result','geodataframe',True)]
        params = {'input_gdf': json.loads(source.to_json()), 'reference_gdf': json.loads(reference.to_json()), 'tolerance': .2}
        assert client.post('/api/operators/snap_geometry/invoke', json={'params': params}).status_code==403
        # Literal GeoJSON inputs enter the Runtime as EPSG:4326. Projected
        # geometries must arrive through the existing load/reproject nodes.
        workflow = {'tasks': tasks[:-1]}
        assert client.post('/api/workflow', json={'workflow_def':workflow}).status_code==400
        response = client.post('/api/workflow', json={'workflow_def':workflow,'runtime':{'tenant_id':7,'execution_authorization':{'id':1,'effects':['read']}}})
        assert response.status_code==202
        deadline = time.monotonic()+5
        while time.monotonic()<deadline:
            state = client.get('/api/executions/'+response.get_json()['execution_id']).get_json()
            if state['status'] in {'success','failed'}:break
            time.sleep(.01)
        assert state['status']=='success',state
        assert json.loads(state['result'])['features'][0]['geometry']['coordinates']==[[0.,0.],[5.,0.]]


def overlap_frame(geometries, ids=None, crs="EPSG:3857"):
    return gpd.GeoDataFrame(
        {"parcel_id": ids if ids is not None else list(range(len(geometries))),
         "business_value": ["unchanged"] * len(geometries)},
        geometry=geometries, crs=crs,
    ).rename_geometry("footprint")


def overlap(frame, id_field="parcel_id"):
    return get_operator("check_polygon_overlap")(input_gdf=frame, id_field=id_field)


def test_polygon_overlap_reports_partial_contained_and_duplicate_pairs_without_changing_source():
    frame = overlap_frame([box(0, 0, 4, 4), box(2, 0, 6, 4), box(1, 1, 2, 2), box(0, 0, 4, 4)], [90, 10, 30, 40])
    frame.index = [7, 7, 1, 9]
    original = frame.copy(deep=True)
    result = overlap(frame)
    assert result.columns.tolist() == ["source_id_a", "source_id_b", "overlap_area", "geometry"]
    assert list(zip(result.source_id_a, result.source_id_b, result.overlap_area)) == [
        (90, 10, 8), (90, 30, 1), (90, 40, 16), (10, 40, 8), (30, 40, 1),
    ]
    assert result.geom_type.tolist() == ["MultiPolygon"] * 5
    assert result.is_valid.all() and result.crs == frame.crs
    assert result.geometry.iloc[0].equals(box(2, 0, 4, 4))
    assert_geodataframe_equal(frame, original)


def test_polygon_overlap_ignores_line_point_contacts_and_respects_holes():
    ring = Polygon(box(0, 0, 4, 4).exterior.coords, [box(1, 1, 3, 3).exterior.coords])
    result = overlap(overlap_frame([ring, box(1, 1, 3, 3), box(4, 0, 5, 4), box(4, 4, 5, 5), box(10, 10, 11, 11)]))
    assert result.empty
    assert result.columns.tolist() == ["source_id_a", "source_id_b", "overlap_area", "geometry"]
    assert result.overlap_area.dtype == np.dtype("float64")


def test_polygon_overlap_keeps_all_polygon_parts_in_one_pair_and_drops_contact_lines():
    a = shapely.MultiPolygon([box(0, 0, 2, 2), box(4, 0, 5, 1), box(8, 0, 10, 2)])
    b = shapely.MultiPolygon([box(1, 0, 3, 2), box(5, 0, 6, 1), box(9, 0, 11, 2)])
    assert a.intersection(b).geom_type == "GeometryCollection"
    result = overlap(overlap_frame([a, b], ["地块甲", "地块乙"]))
    assert len(result) == 1 and result.overlap_area.tolist() == [4]
    assert len(result.geometry.iloc[0].geoms) == 2
    assert result.geometry.iloc[0].equals(shapely.MultiPolygon([box(1, 0, 2, 2), box(9, 0, 10, 2)]))


@pytest.mark.parametrize("crs,area", [("EPSG:3857", 1e-12), ("EPSG:2263", 1e-12)])
def test_polygon_overlap_has_no_small_area_threshold_and_does_not_convert_square_units(crs, area):
    result = overlap(overlap_frame([box(0, 0, 1, area), box(0, 0, 1, area)], crs=crs))
    assert result.overlap_area.tolist() == [area]
    assert result.crs == overlap_frame([], crs=crs).crs


@pytest.mark.parametrize("ids", [pd.Series([2**63+1, 2**63+2], dtype="UInt64"), pd.Series(["地块/甲", "地块%乙"], dtype="string")])
def test_polygon_overlap_preserves_identifier_dtype_without_float_or_string_coercion(ids):
    result = overlap(overlap_frame([box(0, 0, 2, 2)] * 2, ids))
    for name, position in [("source_id_a", 0), ("source_id_b", 1)]:
        assert result[name].dtype == ids.dtype
        assert result[name].iloc[0] == ids.iloc[position]


@pytest.mark.parametrize("geometries", [[], [Polygon()], [box(0, 0, 1, 1)], [Polygon(), box(0, 0, 1, 1)]])
def test_polygon_overlap_empty_and_single_feature_results_keep_typed_schema(geometries):
    frame = overlap_frame(geometries, pd.Series(range(len(geometries)), dtype="Int64"))
    result = overlap(frame)
    assert result.empty and result.crs == frame.crs
    assert result.source_id_a.dtype == result.source_id_b.dtype == frame.parcel_id.dtype
    assert result.overlap_area.dtype == np.dtype("float64")


@pytest.mark.parametrize("ids", [[None, "b"], ["a", "a"], ["", "b"], ["  ", "b"], [["a"], ["b"]]])
def test_polygon_overlap_rejects_missing_duplicate_blank_or_nonscalar_ids(ids):
    with pytest.raises(ValueError, match="scalar|non-empty|unique"):
        overlap(overlap_frame([box(0, 0, 1, 1)] * 2, ids))


@pytest.mark.parametrize("id_field", [None, "", "absent", "footprint"])
def test_polygon_overlap_requires_an_explicit_existing_nongeometry_id_field(id_field):
    with pytest.raises(ValueError, match="id_field"):
        overlap(overlap_frame([box(0, 0, 1, 1)]), id_field)


@pytest.mark.parametrize("crs", [None, "EPSG:4326", "OGC:CRS84"])
def test_polygon_overlap_rejects_unknown_or_geographic_crs_even_for_empty_input(crs):
    with pytest.raises(ValueError, match="projected CRS"):
        overlap(overlap_frame([], crs=crs))


@pytest.mark.parametrize("geometry,message", [
    (None, "non-null"), (Point(0, 0), "Polygon"),
    (LineString([(0, 0), (1, 1)]), "Polygon"),
    (Polygon([(0, 0), (2, 2), (0, 2), (2, 0), (0, 0)]), "valid"),
    (shapely.from_wkt("POLYGON Z ((0 0 1, 1 0 1, 1 1 1, 0 0 1))"), "two-dimensional"),
    (shapely.from_wkt("POLYGON M ((0 0 1, 1 0 1, 1 1 1, 0 0 1))"), "two-dimensional"),
    (shapely.from_wkt("POLYGON ((0 0, Infinity 0, 1 1, 0 0))"), "finite"),
])
def test_polygon_overlap_rejects_unsupported_geometries_without_implicit_repair(geometry, message):
    with pytest.raises(ValueError, match=message):
        overlap(overlap_frame([geometry]))


def test_polygon_overlap_rejects_nonfinite_or_underflowed_result_area():
    for size in [1e160, 1e-170]:
        frame = overlap_frame([box(0, 0, size, size)] * 2)
        with np.errstate(over="ignore", under="ignore", invalid="ignore"):
            with pytest.raises(ValueError, match="finite positive.*area"):
                overlap(frame)


def test_polygon_overlap_requires_an_active_geodataframe():
    for frame in [pd.DataFrame({"parcel_id": [1]}), gpd.GeoDataFrame({"parcel_id": [1]})]:
        with pytest.raises(ValueError, match="GeoDataFrame|active geometry"):
            overlap(frame)


def test_polygon_overlap_rejects_geometry_id_columns_and_ambiguous_column_names():
    frame = overlap_frame([box(0, 0, 1, 1)])
    frame["secondary_geometry"] = frame.geometry.copy()
    with pytest.raises(ValueError, match="id_field"):
        overlap(frame, "secondary_geometry")
    frame["another_id"] = frame.parcel_id
    frame = frame.rename(columns={"another_id": "parcel_id"})
    with pytest.raises(ValueError, match="id_field"):
        overlap(frame)


def test_polygon_overlap_invalid_id_fails_workflow_before_publishing_a_file(tmp_path):
    source = overlap_frame([box(0, 0, 2, 2), box(1, 1, 3, 3)], ["duplicate", "duplicate"])
    source.to_file(tmp_path / "invalid-ids.gpkg", driver="GPKG")
    before = (tmp_path / "invalid-ids.gpkg").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    result = execute_workflow({"tasks": [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "invalid-ids.gpkg"}, "depends_on": []},
        {"id": "check", "operator": "check_polygon_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "parcel_id"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "must-not-exist.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]})
    assert result["status"] == "failed" and "unique scalar identifiers" in str(result)
    assert not (tmp_path / "must-not-exist.gpkg").exists()
    assert (tmp_path / "invalid-ids.gpkg").read_bytes() == before


def test_polygon_overlap_matches_independent_integer_grid_pair_oracle():
    # Exact rectangle occupancy supplies an area oracle independent of GEOS
    # intersection; dense and disjoint envelopes exercise pair de-duplication.
    bounds = [(0, 0, 4, 4), (1, 2, 6, 5), (2, 1, 3, 6), (8, 8, 9, 9), (0, 0, 4, 4)]
    frame = overlap_frame([box(*bound) for bound in bounds])
    cells = [{(x, y) for x in range(a, c) for y in range(b, d)} for a, b, c, d in bounds]
    expected = [(a, b, len(cells[a] & cells[b])) for a in range(len(cells)) for b in range(a+1, len(cells)) if cells[a] & cells[b]]
    result = overlap(frame)
    assert list(zip(result.source_id_a, result.source_id_b, result.overlap_area)) == expected


def test_polygon_overlap_runtime_discovery_workflow_save_reload_and_source_bytes(tmp_path, monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    source = overlap_frame([box(0, 0, 4, 4), box(2, 0, 6, 4), box(1, 1, 2, 2)], ["甲", "乙", "丙"])
    source.to_file(tmp_path / "parcels.gpkg", layer="parcels", driver="GPKG")
    before = (tmp_path / "parcels.gpkg").read_bytes()
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": "load", "operator": "load", "params": {"connection_info": connection, "path": "parcels.gpkg"}, "depends_on": []},
        {"id": "check", "operator": "check_polygon_overlap", "params": {"input_gdf": {"$ref": "load"}, "id_field": "parcel_id"}, "depends_on": ["load"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "overlaps.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]
    execution = execute_workflow({"tasks": tasks})
    assert execution["status"] == "success", execution.get("error")
    saved = gpd.read_file(tmp_path / "overlaps.gpkg")
    assert list(zip(saved.source_id_a, saved.source_id_b, saved.overlap_area)) == [("甲", "乙", 8), ("甲", "丙", 1)]
    assert saved.crs == source.crs and saved.geom_type.tolist() == ["MultiPolygon"] * 2
    assert saved.geometry.iloc[0].equals(box(2, 0, 4, 4))
    assert (tmp_path / "parcels.gpkg").read_bytes() == before
    from osgeo import ogr
    dataset = ogr.Open(str(tmp_path / "overlaps.gpkg"))
    layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == 2
    assert layer.GetSpatialRef().GetAuthorityCode(None) == "3857"
    records = []
    for feature in layer:
        geometry = feature.GetGeometryRef()
        assert geometry.GetGeometryName() == "MULTIPOLYGON" and geometry.IsValid()
        records.append((feature.GetField("source_id_a"), feature.GetField("source_id_b"), feature.GetField("overlap_area"), geometry.GetArea()))
    assert records == [("甲", "乙", 8, 8), ("甲", "丙", 1, 1)]
    dataset = None
    monkeypatch.setattr(api_server, "executions", ExecutionRegistry())
    with api_server.app.test_client() as client:
        op = next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"] == "check_polygon_overlap")
        assert op["display_name"] == "面叠检查" and op["category_path"] == ["矢量处理", "几何处理"]
        assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
        assert [(p["name"], p["param_type"], p["required"]) for p in op["parameters"]] == [("input_gdf", "input", True), ("id_field", "param", True)]
        assert [(p["name"], p["type"], p["is_default"]) for p in op["output_ports"]] == [("result", "geodataframe", True)]
        assert client.post("/api/operators/check_polygon_overlap/invoke", json={"params": {}}).status_code == 403
        workflow = {"tasks": tasks[:-1]}
        assert client.post("/api/workflow", json={"workflow_def": workflow}).status_code == 400
        response = client.post("/api/workflow", json={"workflow_def": workflow, "runtime": {"tenant_id": 7, "execution_authorization": {"id": 1, "effects": ["read"]}}})
        assert response.status_code == 202
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            state = client.get("/api/executions/" + response.get_json()["execution_id"]).get_json()
            if state["status"] in {"success", "failed"}:
                break
            time.sleep(.01)
        assert state["status"] == "success", state
        assert len(json.loads(state["result"])["features"]) == 2


def gap(frame, coverage):
    return get_operator("check_polygon_gap")(input_gdf=frame, coverage_gdf=coverage)


def test_polygon_gap_reports_internal_and_edge_gaps_without_mutating_sources():
    frame = overlap_frame([Polygon(box(0, 0, 3, 4).exterior.coords, [box(1, 1, 2, 2).exterior.coords])])
    coverage = lines([box(0, 0, 4, 4)]).rename_geometry("extent")
    frame.index = [19]
    originals = [frame.copy(deep=True), coverage.copy(deep=True)]
    result = gap(frame, coverage)
    assert result.columns.tolist() == ["gap_area", "geometry"]
    assert result.gap_area.tolist() == [1, 4]
    assert result.geom_type.tolist() == ["Polygon", "Polygon"]
    assert result.geometry.iloc[0].equals(box(1, 1, 2, 2))
    assert result.geometry.iloc[1].equals(box(3, 0, 4, 4))
    assert result.crs == frame.crs == coverage.crs
    for source, before in zip([frame, coverage], originals):
        assert_geodataframe_equal(source, before)


def test_polygon_gap_unions_both_layers_and_ignores_outside_coverage_and_row_order():
    coverage = lines([box(0, 0, 4, 4), box(2, 0, 6, 4), box(0, 0, 4, 4)])
    frame = lines([box(-2, -2, 1, 8), box(3, 0, 4, 4), box(3, 0, 4, 4), box(10, 0, 11, 1)])
    result = gap(frame, coverage)
    assert result.gap_area.tolist() == [8, 8]
    assert result.geometry.iloc[0].equals(box(1, 0, 3, 4))
    assert result.geometry.iloc[1].equals(box(4, 0, 6, 4))
    reversed_source = frame.iloc[::-1].copy()
    reversed_source.geometry = shapely.reverse(reversed_source.geometry.array)
    assert_geodataframe_equal(gap(reversed_source, coverage.iloc[::-1]), result)


def test_polygon_gap_excludes_coverage_holes_and_retains_islands_as_result_holes():
    boundary = Polygon(box(0, 0, 6, 6).exterior.coords, [box(1, 1, 2, 2).exterior.coords])
    coverage = lines([boundary])
    result = gap(lines([box(3, 3, 4, 4)]), coverage)
    assert len(result) == 1 and result.gap_area.tolist() == [34]
    assert len(result.geometry.iloc[0].interiors) == 2
    assert not result.geometry.iloc[0].contains(Point(1.5, 1.5))
    assert not result.geometry.iloc[0].contains(Point(3.5, 3.5))
    assert gap(lines([box(0, 0, 6, 6)]), coverage).empty


def test_polygon_gap_splits_multipart_and_point_contacts_into_separate_rows():
    coverage = lines([shapely.MultiPolygon([box(0, 0, 1, 1), box(1, 1, 2, 2), box(3, 0, 4, 1)])])
    result = gap(lines([]), coverage)
    assert result.gap_area.tolist() == [1, 1, 1]
    assert result.geom_type.tolist() == ["Polygon"] * 3
    assert [geometry.bounds for geometry in result.geometry] == [(0, 0, 1, 1), (1, 1, 2, 2), (3, 0, 4, 1)]


@pytest.mark.parametrize("crs", ["EPSG:3857", "EPSG:2263"])
def test_polygon_gap_keeps_tiny_exact_positive_area_and_source_square_units(crs):
    tiny = 2. ** -40
    result = gap(lines([], crs), lines([box(0, 0, 1, tiny)], crs))
    assert result.gap_area.tolist() == [tiny]
    assert result.geometry.iloc[0].bounds == (0, 0, 1, tiny)
    assert result.crs == lines([], crs).crs


@pytest.mark.parametrize("geometries", [[], [Polygon()], [shapely.MultiPolygon([])]])
def test_polygon_gap_empty_input_reports_entire_range_without_inventing_source_ids(geometries):
    result = gap(lines(geometries), lines([box(0, 0, 2, 2)]))
    assert result.columns.tolist() == ["gap_area", "geometry"]
    assert result.gap_area.tolist() == [4]
    assert result.geometry.iloc[0].equals(box(0, 0, 2, 2))


@pytest.mark.parametrize("source", [[box(0, 0, 2, 2)], [box(-1, -1, 3, 3)], [box(0, 0, 1, 2), box(1, 0, 2, 2)]])
def test_polygon_gap_full_coverage_returns_typed_empty_schema(source):
    result = gap(lines(source), lines([box(0, 0, 2, 2), Polygon()]))
    assert result.empty and result.columns.tolist() == ["gap_area", "geometry"]
    assert result.gap_area.dtype == np.dtype("float64") and result.crs.to_epsg() == 3857


@pytest.mark.parametrize("coverage", [[], [Polygon()], [shapely.MultiPolygon([]), Polygon()]])
def test_polygon_gap_rejects_absent_effective_coverage(coverage):
    with pytest.raises(ValueError, match="coverage_gdf.*non-empty"):
        gap(lines([]), lines(coverage))
    with pytest.raises(TypeError, match="coverage_gdf"):
        get_operator("check_polygon_gap")(input_gdf=lines([]))


@pytest.mark.parametrize("source_crs,coverage_crs", [(None, "EPSG:3857"), ("EPSG:3857", None), ("EPSG:4326", "EPSG:4326"), ("EPSG:3857", "EPSG:2263")])
def test_polygon_gap_rejects_unknown_geographic_or_different_crs_before_empty_result(source_crs, coverage_crs):
    with pytest.raises(ValueError, match="CRS"):
        gap(lines([], source_crs), lines([box(0, 0, 1, 1)], coverage_crs))


@pytest.mark.parametrize("side", ["input", "coverage"])
@pytest.mark.parametrize("geometry,message", [
    (None, "non-null"), (Point(0, 0), "Polygon"), (LineString([(0, 0), (1, 1)]), "Polygon"),
    (shapely.GeometryCollection([box(0, 0, 1, 1)]), "Polygon"),
    (Polygon([(0, 0), (1, 1), (0, 1), (1, 0), (0, 0)]), "valid"),
    (shapely.from_wkt("POLYGON Z ((0 0 1, 1 0 1, 1 1 1, 0 0 1))"), "XY"),
    (shapely.from_wkt("POLYGON M ((0 0 1, 1 0 1, 1 1 1, 0 0 1))"), "XY"),
    (Polygon([(0, 0), (1, 0), (float('inf'), 1), (0, 0)]), "finite"),
])
def test_polygon_gap_rejects_unsupported_geometry_on_either_side(side, geometry, message):
    good, bad = lines([box(0, 0, 2, 2)]), lines([geometry])
    with pytest.raises(ValueError, match=message):
        gap(bad if side == "input" else good, bad if side == "coverage" else good)


@pytest.mark.parametrize("side", ["input", "coverage"])
@pytest.mark.parametrize("frame", [None, pd.DataFrame(), gpd.GeoDataFrame({"value": [1]})])
def test_polygon_gap_requires_two_active_geodataframes(side, frame):
    good = lines([box(0, 0, 1, 1)])
    with pytest.raises(ValueError, match="GeoDataFrame|active geometry"):
        gap(frame if side == "input" else good, frame if side == "coverage" else good)


@pytest.mark.parametrize("size", [1e200, 1e-200])
def test_polygon_gap_fails_unrepresentable_area_without_discarding_a_gap(size):
    with np.errstate(over="ignore", under="ignore", invalid="ignore"):
        with pytest.raises(ValueError, match="finite positive Float64"):
            gap(lines([]), lines([box(0, 0, size, size)]))


def test_polygon_gap_matches_an_independent_integer_cell_oracle():
    boundary = [(0, 0, 6, 4), (4, 0, 8, 2)]
    source = [(-1, -1, 1, 5), (2, 0, 3, 4), (4, 1, 7, 3), (5, 0, 6, 1)]
    def cells(bounds):
        return {(x, y) for a, b, c, d in bounds for x in range(a, c) for y in range(b, d)}
    expected = cells(boundary) - cells(source)
    result = gap(lines([box(*bound) for bound in source]), lines([box(*bound) for bound in boundary]))
    assert result.gap_area.sum() == len(expected)
    for x in range(-1, 9):
        for y in range(-1, 6):
            matches = sum(geometry.contains(Point(x + .5, y + .5)) for geometry in result.geometry)
            assert matches == int((x, y) in expected)


@pytest.mark.parametrize("fully_covered", [False, True])
def test_polygon_gap_two_source_workflow_save_reload_independent_gdal_and_source_bytes(tmp_path, fully_covered):
    from osgeo import ogr
    source = lines([box(0, 0, 4 if fully_covered else 3, 4)])
    coverage = lines([box(0, 0, 4, 4)])
    for name, frame in [("source", source), ("coverage", coverage)]:
        frame.to_file(tmp_path / f"{name}.gpkg", layer=name, driver="GPKG")
    before = {name: (tmp_path / f"{name}.gpkg").read_bytes() for name in ["source", "coverage"]}
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    tasks = [
        {"id": name, "operator": "load", "params": {"connection_info": connection, "path": f"{name}.gpkg"}, "depends_on": []}
        for name in ["source", "coverage"]
    ] + [
        {"id": "check", "operator": "check_polygon_gap", "params": {"input_gdf": {"$ref": "source"}, "coverage_gdf": {"$ref": "coverage"}}, "depends_on": ["source", "coverage"]},
        {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "gaps.gpkg", "mode": "fail"}, "depends_on": ["check"]},
    ]
    execution = execute_workflow({"tasks": tasks})
    assert execution["status"] == "success", execution.get("error")
    saved = gpd.read_file(tmp_path / "gaps.gpkg")
    assert saved.crs == source.crs and saved.gap_area.tolist() == ([] if fully_covered else [4])
    dataset = ogr.Open(str(tmp_path / "gaps.gpkg"))
    layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == int(not fully_covered)
    assert layer.GetSpatialRef().GetAuthorityCode(None) == "3857"
    if not fully_covered:
        feature = layer.GetNextFeature()
        geometry = feature.GetGeometryRef()
        assert geometry.GetGeometryName() == "POLYGON" and geometry.IsValid()
        assert feature.GetField("gap_area") == geometry.GetArea() == 4
        assert set(geometry.GetGeometryRef(0).GetPoints()) == {(3, 0), (3, 4), (4, 4), (4, 0)}
    dataset = None
    assert all((tmp_path / f"{name}.gpkg").read_bytes() == data for name, data in before.items())


def test_polygon_gap_invalid_or_missing_coverage_fails_before_publishing(tmp_path):
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    lines([box(0, 0, 1, 1)]).to_file(tmp_path / "source.gpkg", driver="GPKG")
    lines([]).to_file(tmp_path / "empty.gpkg", driver="GPKG")
    for extra_params, message in [({}, "coverage_gdf"), ({"coverage_gdf": {"$ref": "coverage"}}, "non-empty")]:
        result = execute_workflow({"tasks": [
            {"id": "source", "operator": "load", "params": {"connection_info": connection, "path": "source.gpkg"}, "depends_on": []},
            {"id": "coverage", "operator": "load", "params": {"connection_info": connection, "path": "empty.gpkg"}, "depends_on": []},
            {"id": "check", "operator": "check_polygon_gap", "params": {"input_gdf": {"$ref": "source"}, **extra_params}, "depends_on": ["source", "coverage"]},
            {"id": "save", "operator": "save", "params": {"input_df": {"$ref": "check"}, "connection_info": connection, "path": "must-not-exist.gpkg", "mode": "fail"}, "depends_on": ["check"]},
        ]})
        assert result["status"] == "failed"
        assert message in str(result)
        assert not (tmp_path / "must-not-exist.gpkg").exists()


@pytest.mark.parametrize("crs,authority", [("EPSG:3857", "3857"), ("EPSG:2263", "2263")])
def test_polygon_gap_native_save_retains_exact_tiny_coordinates_and_square_units(tmp_path, crs, authority):
    from osgeo import ogr
    tiny = 2. ** -40
    frame = lines([box(0, tiny, 1, 1)], crs)
    frame["gap_area"] = [-1]  # Diagnostic output must not copy source attributes.
    result = gap(frame, lines([box(0, 0, 1, 1)], crs))
    assert result.gap_area.tolist() == [tiny]
    get_operator("save")(
        input_df=result, connection_info={"engine_type": "nfs", "mount_path": str(tmp_path)},
        path="tiny.gpkg", mode="fail",
    )
    dataset = ogr.Open(str(tmp_path / "tiny.gpkg"))
    layer = dataset.GetLayer(0)
    assert layer.GetFeatureCount() == 1 and layer.GetSpatialRef().GetAuthorityCode(None) == authority
    feature = layer.GetNextFeature()
    geometry = feature.GetGeometryRef()
    assert geometry.GetArea() == feature.GetField("gap_area") == tiny
    assert set(geometry.GetGeometryRef(0).GetPoints()) == {(0, 0), (0, tiny), (1, tiny), (1, 0)}
    dataset = None


def test_polygon_gap_discovery_two_required_input_ports_async_workflow_and_direct_rejection(tmp_path, monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    connection = {"engine_type": "nfs", "mount_path": str(tmp_path)}
    for name, geometry in [("source", box(0, 0, 3, 4)), ("coverage", box(0, 0, 4, 4))]:
        lines([geometry]).to_file(tmp_path / f"{name}.gpkg", driver="GPKG")
    tasks = [
        {"id": name, "operator": "load", "params": {"connection_info": connection, "path": f"{name}.gpkg"}, "depends_on": []}
        for name in ["source", "coverage"]
    ] + [{"id": "check", "operator": "check_polygon_gap", "params": {"input_gdf": {"$ref": "source"}, "coverage_gdf": {"$ref": "coverage"}}, "depends_on": ["source", "coverage"]}]
    monkeypatch.setattr(api_server, "executions", ExecutionRegistry())
    with api_server.app.test_client() as client:
        op = next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"] == "check_polygon_gap")
        assert op["display_name"] == "面缝检查" and op["category_path"] == ["矢量处理", "几何处理"]
        assert op["execution_modes"] == ["workflow"] and op["effects"] == ["read"]
        assert [(p["name"], p["param_type"], p["required"]) for p in op["parameters"]] == [("input_gdf", "input", True), ("coverage_gdf", "input", True)]
        assert [(p["name"], p["type"], p["is_default"]) for p in op["output_ports"]] == [("result", "geodataframe", True)]
        assert client.post("/api/operators/check_polygon_gap/invoke", json={"params": {}}).status_code == 403
        workflow = {"tasks": tasks}
        assert client.post("/api/workflow", json={"workflow_def": workflow}).status_code == 400
        response = client.post("/api/workflow", json={"workflow_def": workflow, "runtime": {"tenant_id": 7, "execution_authorization": {"id": 1, "effects": ["read"]}}})
        assert response.status_code == 202
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            state = client.get("/api/executions/" + response.get_json()["execution_id"]).get_json()
            if state["status"] in {"success", "failed"}:
                break
            time.sleep(.01)
        assert state["status"] == "success", state
        features = json.loads(state["result"])["features"]
        assert len(features) == 1 and features[0]["properties"]["gap_area"] == 4


def dangles(geometries, ids=None, crs="EPSG:3857"):
    frame = lines(geometries, crs)
    frame["line_id"] = ids if ids is not None else list(range(len(frame)))
    return get_operator("check_dangling_points")(input_gdf=frame, id_field="line_id")


@pytest.mark.parametrize("geometries,expected", [
    ([LineString([(0,0),(2,0)])], [(0,0,0),(0,2,0)]),
    ([LineString([(0,0),(1,0)]),LineString([(1,0),(2,0)])], [(0,0,0),(1,2,0)]),
    ([LineString([(0,0),(4,0)]),LineString([(2,0),(2,2)])], [(0,0,0),(0,4,0),(1,2,2)]),
    ([LineString([(0,0),(2,2)]),LineString([(0,2),(2,0)])], [(0,0,0),(0,2,2),(1,0,2),(1,2,0)]),
    ([box(0,0,2,2).boundary], []),
    ([LineString([(0,0),(2,2),(0,2),(2,0)])], [(0,0,0),(0,2,0)]),
    ([LineString([(0,0),(4,0)]),LineString([(4,0),(0,0)])], [(0,0,0),(0,4,0),(1,0,0),(1,4,0)]),
    ([LineString([(0,0),(4,0)]),LineString([(2,0),(6,0)])], [(0,0,0),(1,6,0)]),
    ([LineString([(0,0),(4,0)]),LineString([(1,0),(3,0)])], [(0,0,0),(0,4,0)]),
    ([box(0,0,2,2).boundary,LineString([(0,0),(-1,0)])], [(1,-1,0)]),
    ([MultiLineString([[(0,0),(1,0)],[(1,0),(2,0)],[(1,0),(0,0)]])], [(0,0,0),(0,2,0)]),
])
def test_dangling_points_reports_geometric_degree_one_with_exact_source_binding(geometries, expected):
    result = dangles(geometries)
    assert result.columns.tolist() == ["source_id", "geometry"]
    assert list(zip(result.source_id, result.geometry.x, result.geometry.y)) == expected
    assert result.geom_type.tolist() == ["Point"]*len(expected)
    assert result.crs.to_epsg() == 3857


@pytest.mark.parametrize("crs", ["EPSG:3857","EPSG:2263"])
def test_dangling_points_keeps_binary_tiny_disconnect_without_tolerance(crs):
    tiny=2.**-40
    result=dangles([LineString([(0,0),(1,0)]),LineString([(1+tiny,0),(2,0)])],crs=crs)
    assert list(zip(result.source_id,result.geometry.x)) == [(0,0),(0,1),(1,1+tiny),(1,2)]
    assert str(result.crs)==crs


def test_dangling_points_preserves_source_ids_order_dtype_custom_geometry_and_source():
    import pandas as pd
    frame=lines([LineString([(0,0),(4,0)]),LineString([(2,0),(6,0)]),LineString([(10,0),(11,0)])])
    frame["line_id"]=pd.array([2**60+7,2**60+3,2**60+1],dtype="Int64")
    frame["source_id"]=["business"]*3
    frame=frame.rename_geometry("route");frame.index=[9,9,1];before=frame.copy(deep=True)
    result=get_operator("check_dangling_points")(input_gdf=frame,id_field="line_id")
    assert result.source_id.tolist()==[2**60+7,2**60+3,2**60+1,2**60+1]
    assert str(result.source_id.dtype)=="Int64"
    assert list(zip(result.geometry.x,result.geometry.y))==[(0,0),(6,0),(10,0),(11,0)]
    assert_geodataframe_equal(frame,before)


def test_dangling_points_empty_batch_retains_source_id_schema_and_crs():
    import pandas as pd
    frame=lines([]);frame["identity"]=pd.Series([],dtype="string")
    result=get_operator("check_dangling_points")(input_gdf=frame,id_field="identity")
    assert result.empty and result.columns.tolist()==["source_id","geometry"]
    assert str(result.source_id.dtype)=="string" and result.crs==frame.crs


@pytest.mark.parametrize("value", [None,"", "missing", "geometry", "boundary"])
def test_dangling_points_rejects_missing_or_geometry_id_field(value):
    with pytest.raises(ValueError,match="id_field"):
        get_operator("check_dangling_points")(input_gdf=lines([LineString([(0,0),(1,0)])]),id_field=value)


@pytest.mark.parametrize("ids", [[None,1],[1,1],[" ","x"],[[1],[2]]])
def test_dangling_points_rejects_non_scalar_missing_blank_or_duplicate_ids(ids):
    with pytest.raises(ValueError,match="identifiers"):
        dangles([LineString([(0,0),(1,0)]),LineString([(2,0),(3,0)])],ids)


@pytest.mark.parametrize("geometry", [None,Point(0,0),box(0,0,1,1),LineString(),
    LineString([(0,0),(0,0)]),LineString([(0,0,0),(1,0,0)]),
    shapely.from_wkt("LINESTRING M (0 0 1,1 0 1)"),LineString([(0,0),(float('inf'),0)])])
def test_dangling_points_rejects_missing_empty_invalid_non_line_or_non_xy(geometry):
    with pytest.raises(ValueError):dangles([geometry])


@pytest.mark.parametrize("crs", [None,"EPSG:4326","OGC:CRS84"])
def test_dangling_points_rejects_unknown_or_geographic_crs(crs):
    with pytest.raises(ValueError,match="projected CRS"):
        dangles([LineString([(0,0),(1,0)])],crs=crs)


def test_dangling_points_requires_active_geodataframe_and_unique_columns():
    import pandas as pd
    operator=get_operator("check_dangling_points")
    for frame in [pd.DataFrame({"id":[1]}),gpd.GeoDataFrame({"id":[1]})]:
        with pytest.raises(ValueError):operator(input_gdf=frame,id_field="id")
    frame=lines([LineString([(0,0),(1,0)])]);frame["id"]=1;frame["other"]=2
    frame.columns=["source_name","boundary","id","id"]
    with pytest.raises(ValueError,match="id_field"):operator(input_gdf=frame,id_field="id")


@pytest.mark.parametrize("closed", [False,True])
def test_dangling_points_workflow_native_point_save_reload_and_source_unchanged(tmp_path,closed):
    from osgeo import ogr
    frame=lines([box(0,0,1,1).boundary if closed else LineString([(0,0),(1,0)])]);frame["line_id"]=[19]
    frame.to_file(tmp_path/"source.gpkg",driver="GPKG");before=(tmp_path/"source.gpkg").read_bytes()
    connection={"engine_type":"nfs","mount_path":str(tmp_path)}
    result=execute_workflow({"tasks":[
        {"id":"load","operator":"load","params":{"connection_info":connection,"path":"source.gpkg"},"depends_on":[]},
        {"id":"check","operator":"check_dangling_points","params":{"input_gdf":{"$ref":"load"},"id_field":"line_id"},"depends_on":["load"]},
        {"id":"save","operator":"save","params":{"input_df":{"$ref":"check"},"connection_info":connection,"path":"dangles.gpkg","mode":"fail"},"depends_on":["check"]}]})
    assert result["status"]=="success",result.get("error")
    dataset=ogr.Open(str(tmp_path/"dangles.gpkg"),0);layer=dataset.GetLayer(0)
    assert layer.GetFeatureCount()==(0 if closed else 2) and layer.GetSpatialRef().GetAuthorityCode(None)=="3857"
    rows=[(f.GetField("source_id"),f.GetGeometryRef().GetX(),f.GetGeometryRef().GetY()) for f in layer]
    assert rows==([] if closed else [(19,0,0),(19,1,0)])
    dataset=None
    assert (tmp_path/"source.gpkg").read_bytes()==before


def test_dangling_points_discovery_workflow_only_and_required_explicit_id():
    op=next(o for o in list_operators() if o["id"]=="check_dangling_points")
    assert op["display_name"]=="悬点检查" and op["category_path"]==["矢量处理","几何处理"]
    assert op["execution_modes"]==["workflow"] and op["effects"]==["read"]
    assert [(p["name"],p["param_type"],p["required"]) for p in op["parameters"]]==[("input_gdf","input",True),("id_field","param",True)]
    assert [(p["name"],p["type"],p["is_default"]) for p in op["output_ports"]]==[("result","geodataframe",True)]


def test_dangling_points_async_runtime_discovery_direct_denial_and_no_implicit_id(tmp_path,monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    frame=lines([LineString([(0,0),(1,0)])]);frame["line_id"]=[17]
    frame.to_file(tmp_path/"source.gpkg",driver="GPKG")
    connection={"engine_type":"nfs","mount_path":str(tmp_path)}
    tasks=[{"id":"load","operator":"load","params":{"connection_info":connection,"path":"source.gpkg"},"depends_on":[]},
           {"id":"check","operator":"check_dangling_points","params":{"input_gdf":{"$ref":"load"},"id_field":"line_id"},"depends_on":["load"]}]
    monkeypatch.setattr(api_server,"executions",ExecutionRegistry())
    with api_server.app.test_client() as client:
        assert any(o["id"]=="check_dangling_points" for o in client.get("/api/operators").get_json()["operators"])
        assert client.post("/api/operators/check_dangling_points/invoke",json={"params":{}}).status_code==403
        assert client.post("/api/workflow",json={"workflow_def":{"tasks":tasks}}).status_code==400
        response=client.post("/api/workflow",json={"workflow_def":{"tasks":tasks},"runtime":{"tenant_id":7,"execution_authorization":{"id":1,"effects":["read"]}}})
        assert response.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            state=client.get('/api/executions/'+response.get_json()['execution_id']).get_json()
            if state['status'] in ('success','failed'):break
            time.sleep(.01)
        assert state['status']=='success',state
        features=json.loads(state['result'])['features']
        assert [feature['properties']['source_id'] for feature in features]==[17,17]
        assert [feature['geometry'] for feature in features]==[
            {'type':'Point','coordinates':[0,0]}, {'type':'Point','coordinates':[1,0]}]
    tasks[1]['params'].pop('id_field')
    tasks.append({"id":"save","operator":"save","params":{"input_df":{"$ref":"check"},"connection_info":connection,"path":"must-not-exist.gpkg","mode":"fail"},"depends_on":["check"]})
    failed=execute_workflow({'tasks':tasks})
    assert failed['status']=='failed' and 'id_field' in str(failed)
    assert not (tmp_path/'must-not-exist.gpkg').exists()


def test_dangling_points_source_lookup_uses_exact_lines_not_bounding_boxes():
    result=dangles([box(0,0,4,4).boundary,LineString([(1,1),(2,1)])],["ring","isolated"])
    assert list(zip(result.source_id,result.geometry.x,result.geometry.y))==[("isolated",1,1),("isolated",2,1)]


def test_polygon_overlap_empty_string_ids_use_shared_scalar_validation():
    import pandas as pd
    frame=overlap_frame([]);frame["parcel_id"]=pd.Series([],dtype="string")
    result=overlap(frame)
    assert result.empty and str(result.source_id_a.dtype)==str(result.source_id_b.dtype)=="string"


# Distinct-feature point intersections complement positive-length overlap checks.
def line_intersection(frame, id_field="route_key"):
    return get_operator("check_line_intersection")(input_gdf=frame, id_field=id_field)


@pytest.mark.parametrize("other,expected", [
    (LineString([(1,-1),(1,1)]), [(1,0)]),
    (LineString([(1,0),(1,1)]), [(1,0)]),
    (LineString([(2,0),(3,1)]), [(2,0)]),
    (LineString([(2,0),(3,0)]), [(2,0)]),
    (LineString([(0,0),(1,0),(2,0)]), []),
    (LineString([(2,0),(0,0)]), []),
    (LineString([(0,2**-40),(2,2**-40)]), []),
    (LineString([(2+2**-40,0),(3,0)]), []),
])
def test_line_intersection_exact_point_contacts_without_overlap_endpoints(other, expected):
    result=line_intersection(line_overlap_frame([LineString([(0,0),(2,0)]),other],["甲","乙"]))
    assert list(zip(result.source_id_a,result.source_id_b))==[("甲","乙")]*len(expected)
    assert [list(point.coords)[0] for point in result.geometry]==expected
    assert result.geometry.geom_type.tolist()==["Point"]*len(expected)


def test_line_intersection_mixed_collection_keeps_only_isolated_points():
    a=MultiLineString([[(0,0),(3,0)],[(0,2),(3,2)],[(5,0),(5,2)]])
    b=MultiLineString([[(1,0),(2,0)],[(1,2),(2,2)],[(4,1),(6,1)]])
    frame=line_overlap_frame([a,b],["主线","支线"])
    result=line_intersection(frame)
    assert list(zip(result.source_id_a,result.source_id_b,result.geometry.x,result.geometry.y))==[("主线","支线",5,1)]
    assert line_overlap(frame).overlap_length.tolist()==[2]


def test_line_intersection_pair_identity_point_dedup_order_and_source_unchanged():
    frame=line_overlap_frame([
        MultiLineString([[(0,0),(4,0)],[(4,0),(0,0)]]),
        MultiLineString([[(3,-1),(3,1)],[(1,1),(1,-1)],[(1,-1),(1,1)]]),
        LineString([(1,-2),(1,2)]),
    ],pd.Series([90,10,30],dtype="Int64"))
    frame.index=[8,8,2];before=frame.copy(deep=True)
    result=line_intersection(frame)
    assert result.columns.tolist()==["source_id_a","source_id_b","geometry"]
    assert list(zip(result.source_id_a,result.source_id_b,result.geometry.x,result.geometry.y))==[
        (90,10,1,0),(90,10,3,0),(90,30,1,0)]
    # The last pair overlaps along a line; its endpoints are not point intersections.
    assert result.source_id_a.dtype==result.source_id_b.dtype==frame.route_key.dtype
    assert result.crs==frame.crs
    assert_geodataframe_equal(frame,before)
    reversed_frame=frame.copy();reversed_frame[frame.geometry.name]=shapely.reverse(frame.geometry.array)
    assert_geodataframe_equal(line_intersection(reversed_frame),result)


def test_line_intersection_three_sources_same_point_retain_three_pairs():
    frame=line_overlap_frame([LineString([(-1,0),(1,0)]),LineString([(0,-1),(0,1)]),
                              LineString([(-1,-1),(1,1)])],["丙","甲","乙"])
    result=line_intersection(frame)
    assert list(zip(result.source_id_a,result.source_id_b))==[("丙","甲"),("丙","乙"),("甲","乙")]
    assert list(zip(result.geometry.x,result.geometry.y))==[(0,0)]*3


@pytest.mark.parametrize("geometry", [LineString([(0,0),(2,2),(0,2),(2,0)]),
    MultiLineString([[(0,0),(2,0)],[(1,-1),(1,1)]])])
def test_line_intersection_does_not_check_within_single_feature(geometry):
    assert line_intersection(line_overlap_frame([geometry])).empty


@pytest.mark.parametrize("crs",["EPSG:3857","EPSG:2263"])
def test_line_intersection_retains_distinct_binary_tiny_points_and_original_units(crs):
    tiny=2**-40
    frame=line_overlap_frame([LineString([(0,0),(1,0)]),
        MultiLineString([[(tiny,-1),(tiny,1)],[(0,-1),(0,1)]])],crs=crs)
    result=line_intersection(frame)
    assert list(zip(result.geometry.x,result.geometry.y))==[(0,0),(tiny,0)]
    assert result.crs==frame.crs


def test_line_intersection_bbox_candidates_require_real_intersection():
    frame=line_overlap_frame([LineString([(0,0),(2,2)]),LineString([(0,1),(.4,1.4)])])
    assert line_intersection(frame).empty


@pytest.mark.parametrize("ids",[pd.Series([2**60,2**60+1],dtype="Int64"),pd.Series(["甲","乙"],dtype="string")])
def test_line_intersection_preserves_identifier_type_and_empty_schema(ids):
    frame=line_overlap_frame([LineString([(0,0),(2,0)]),LineString([(1,-1),(1,1)])],ids)
    for source in [frame.iloc[:0],frame.iloc[:1],frame]:
        result=line_intersection(source)
        assert result.columns.tolist()==["source_id_a","source_id_b","geometry"]
        assert result.source_id_a.dtype==result.source_id_b.dtype==ids.dtype and result.crs==source.crs
    result=line_intersection(frame)
    assert result.source_id_a.tolist()==ids.iloc[:1].tolist() and result.source_id_b.tolist()==ids.iloc[1:].tolist()


@pytest.mark.parametrize("ids",[[1,1],[None,1],[" ","x"],[[1],[2]]])
def test_line_intersection_rejects_invalid_source_ids(ids):
    with pytest.raises(ValueError,match="identifiers"):
        line_intersection(line_overlap_frame([LineString([(0,0),(1,0)])]*2,ids))


@pytest.mark.parametrize("field",[None,"","missing","boundary"])
def test_line_intersection_requires_explicit_nongeometry_id(field):
    with pytest.raises(ValueError,match="id_field"):
        line_intersection(line_overlap_frame([LineString([(0,0),(1,0)])]),field)


@pytest.mark.parametrize("crs",[None,"EPSG:4326"])
def test_line_intersection_rejects_unknown_or_geographic_crs(crs):
    with pytest.raises(ValueError,match="projected"):
        line_intersection(line_overlap_frame([],crs=crs))


@pytest.mark.parametrize("geometry",[None,LineString(),Point(0,0),box(0,0,1,1),
    shapely.from_wkt("MULTILINESTRING (EMPTY, (0 0, 1 0))"),LineString([(0,0),(0,0)]),
    LineString([(0,0,1),(1,0,1)]),shapely.from_wkt("LINESTRING M (0 0 1, 1 0 1)"),
    LineString([(0,0),(float("inf"),1)])])
def test_line_intersection_rejects_unsupported_geometry(geometry):
    with pytest.raises(ValueError):line_intersection(line_overlap_frame([geometry]))


def test_line_intersection_requires_active_geodataframe_and_unique_columns():
    operator=get_operator("check_line_intersection")
    with pytest.raises(ValueError):operator(input_gdf=pd.DataFrame(),id_field="route_key")
    frame=line_overlap_frame([LineString([(0,0),(1,0)])])
    frame=pd.concat([frame,frame[["route_key"]]],axis=1)
    with pytest.raises(ValueError):operator(input_gdf=frame,id_field="route_key")
    frame=line_overlap_frame([LineString([(0,0),(1,0)])]).drop(columns="boundary")
    with pytest.raises(ValueError):operator(input_gdf=frame,id_field="route_key")


def test_line_intersection_matches_independent_integer_grid_oracle():
    horizontal=[(0,4,0),(2,6,2),(-2,2,4)];vertical=[(1,-1,5),(3,0,4),(5,1,3)]
    shapes=[LineString([(a,y),(b,y)]) for a,b,y in horizontal]+[LineString([(x,a),(x,b)]) for x,a,b in vertical]
    frame=line_overlap_frame(shapes)
    expected=[(i,len(horizontal)+j,x,y) for i,(a,b,y) in enumerate(horizontal)
              for j,(x,c,d) in enumerate(vertical) if a<=x<=b and c<=y<=d]
    result=line_intersection(frame)
    assert list(zip(result.source_id_a,result.source_id_b,result.geometry.x,result.geometry.y))==expected


@pytest.mark.parametrize("empty",[False,True])
def test_line_intersection_workflow_native_point_save_reload_and_discovery(tmp_path,empty):
    from osgeo import ogr
    tiny=2**-40
    source=line_overlap_frame([LineString([(0,0),(2,0)]),
        MultiLineString([[(2,-1),(2,1)],[(tiny,-1),(tiny,1)],[(0,-1),(0,1)]])],["路线甲","路线乙"])
    if empty:source=source.iloc[:1]
    source.to_file(tmp_path/"source.gpkg",layer="routes",driver="GPKG")
    original=(tmp_path/"source.gpkg").read_bytes();connection={"engine_type":"nfs","mount_path":str(tmp_path)}
    tasks=[{"id":"load","operator":"load","params":{"connection_info":connection,"path":"source.gpkg"},"depends_on":[]},
        {"id":"check","operator":"check_line_intersection","params":{"input_gdf":{"$ref":"load"},"id_field":"route_key"},"depends_on":["load"]},
        {"id":"save","operator":"save","params":{"input_df":{"$ref":"check"},"connection_info":connection,"path":"points.gpkg","mode":"fail"},"depends_on":["check"]}]
    execution=execute_workflow({"tasks":tasks});assert execution["status"]=="success",execution.get("error")
    dataset=ogr.Open(str(tmp_path/"points.gpkg"));layer=dataset.GetLayer(0)
    assert layer.GetFeatureCount()==(0 if empty else 3)
    assert layer.GetSpatialRef().GetAuthorityCode(None)=="3857"
    assert [field.name for field in layer.schema]==["source_id_a","source_id_b"]
    assert layer.GetGeomType()==(ogr.wkbUnknown if empty else ogr.wkbPoint)
    if not empty:
        for i,(feature,x) in enumerate(zip(layer,[0,tiny,2]),1):
            geometry=feature.GetGeometryRef()
            assert feature.GetFID()==i and feature.GetField("source_id_a")=="路线甲" and feature.GetField("source_id_b")=="路线乙"
            assert geometry.GetGeometryName()=="POINT" and geometry.IsValid() and geometry.GetCoordinateDimension()==2
            assert geometry.GetPoint_2D()==(x,0)
    dataset=None
    assert (tmp_path/"source.gpkg").read_bytes()==original
    tasks[1]["params"]["id_field"]="missing";tasks[2]["params"]["path"]="rejected.gpkg"
    assert execute_workflow({"tasks":tasks})["status"]=="failed"
    assert not (tmp_path/"rejected.gpkg").exists()


def test_line_intersection_async_runtime_discovery_and_required_identity(tmp_path,monkeypatch):
    import api_server
    from addp_common.workflow_runtime import ExecutionRegistry
    frame=line_overlap_frame([LineString([(0,0),(2,0)]),LineString([(1,-1),(1,1)])],[17,23])
    frame.to_file(tmp_path/"source.gpkg",driver="GPKG")
    connection={"engine_type":"nfs","mount_path":str(tmp_path)}
    tasks=[{"id":"load","operator":"load","params":{"connection_info":connection,"path":"source.gpkg"},"depends_on":[]},
        {"id":"check","operator":"check_line_intersection","params":{"input_gdf":{"$ref":"load"},"id_field":"route_key"},"depends_on":["load"]}]
    monkeypatch.setattr(api_server,"executions",ExecutionRegistry())
    with api_server.app.test_client() as client:
        op=next(o for o in client.get("/api/operators").get_json()["operators"] if o["id"]=="check_line_intersection")
        assert op["display_name"]=="线交检查" and op["category_path"]==["矢量处理","几何处理"]
        assert op["execution_modes"]==["workflow"] and op["effects"]==["read"]
        assert [(p["name"],p["param_type"],p["required"]) for p in op["parameters"]]==[("input_gdf","input",True),("id_field","param",True)]
        assert [(p["name"],p["type"],p["is_default"]) for p in op["output_ports"]]==[("result","geodataframe",True)]
        assert client.post("/api/operators/check_line_intersection/invoke",json={"params":{}}).status_code==403
        assert client.post("/api/workflow",json={"workflow_def":{"tasks":tasks}}).status_code==400
        response=client.post("/api/workflow",json={"workflow_def":{"tasks":tasks},"runtime":{"tenant_id":7,"execution_authorization":{"id":1,"effects":["read"]}}})
        assert response.status_code==202
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            state=client.get('/api/executions/'+response.get_json()['execution_id']).get_json()
            if state['status'] in ('success','failed'):break
            time.sleep(.01)
        assert state['status']=='success',state
        features=json.loads(state['result'])['features']
        assert len(features)==1 and features[0]['type']=='Feature'
        assert features[0]['properties']=={'source_id_a':17,'source_id_b':23}
        assert features[0]['geometry']=={'type':'Point','coordinates':[1,0]}
    tasks[1]['params'].pop('id_field')
    failed=execute_workflow({'tasks':tasks})
    assert failed['status']=='failed' and 'id_field' in str(failed)



def line_self_intersection(frame, id_field="route_key"):
    return get_operator("check_line_self_intersection")(input_gdf=frame,id_field=id_field)


@pytest.mark.parametrize("shape,expected",[
    (LineString([(0,0),(2,2),(0,2),(2,0)]),[(1,1)]),
    (LineString([(0,0),(1,0),(1,1),(0,1),(1,0),(2,0)]),[(1,0)]),
    (MultiLineString([[(0,0),(2,0)],[(1,-1),(1,1)],[(2,0),(3,1)]]),[(1,0),(2,0)]),
    (MultiLineString([[(0,0),(2,0)],[(2,0),(2,2)],[(2,2),(0,2)],[(0,2),(0,0)]]),[(0,0),(0,2),(2,0),(2,2)]),
])
def test_line_self_intersection_reports_nonadjacent_and_cross_part_isolated_contacts(shape,expected):
    frame=line_overlap_frame([shape],["线路甲"]);before=frame.copy(deep=True)
    result=line_self_intersection(frame)
    assert result.columns.tolist()==["source_id","geometry"]
    assert result.source_id.tolist()==["线路甲"]*len(expected)
    assert [list(point.coords)[0] for point in result.geometry]==expected
    assert result.geometry.geom_type.tolist()==["Point"]*len(expected) and result.crs==frame.crs
    assert_geodataframe_equal(frame,before)
    reversed_frame=frame.copy();reversed_frame[frame.geometry.name]=shapely.reverse(frame.geometry.array)
    assert_geodataframe_equal(line_self_intersection(reversed_frame),result)


@pytest.mark.parametrize("shape",[
    LineString([(0,0),(1,0),(2,1)]),
    LineString([(0,0),(1,0),(1,0),(2,1)]),
    LineString([(0,0),(0,0),(2,0),(2,2),(0,2),(0,0),(0,0)]),
    LineString([(0,0),(2,0),(2,2),(0,2),(0,0)]),
    LineString([(0,0),(4,0),(0,0)]),
    MultiLineString([[(0,0),(1,0),(2,0)],[(0,0),(1,0),(2,0)]]),
    MultiLineString([[(0,0),(4,0)],[(4,0),(2,0),(0,0)]]),
    MultiLineString([[(0,0),(2,0)],[(0,2**-40),(2,2**-40)]]),
])
def test_line_self_intersection_excludes_normal_vertices_closed_seams_retracing_and_near_parts(shape):
    assert line_self_intersection(line_overlap_frame([shape])).empty


@pytest.mark.parametrize("x,expected",[(1,[(1,0)]),(2,[]),(3,[]),(4,[]),(5,[(5,0)])])
def test_line_self_intersection_excludes_overlap_positions_including_endpoints_and_other_branches(x,expected):
    shape=MultiLineString([[(0,0),(4,0)],[(2,0),(6,0)],[(x,-1),(x,1)]])
    result=line_self_intersection(line_overlap_frame([shape]))
    assert list(zip(result.geometry.x,result.geometry.y))==expected


def test_line_self_intersection_collects_adjacent_retracing_before_filtering_crossings():
    frame=line_overlap_frame([MultiLineString([[(0,0),(4,0),(0,0)],[(2,-1),(2,1)]])])
    assert line_self_intersection(frame).empty
    assert self_overlap(frame).overlap_length.tolist()==[4]


def test_line_self_intersection_preserves_isolated_crossing_beside_overlap_and_tiny_coverage():
    shape=MultiLineString([[(0,0),(4,0)],[(2,0),(6,0)],[(3,-1),(3,1)],
        [(10,0),(12,2),(10,2),(12,0)]])
    result=line_self_intersection(line_overlap_frame([shape]))
    assert list(zip(result.geometry.x,result.geometry.y))==[(11,1)]
    tiny=2**-600
    frame=line_overlap_frame([MultiLineString([[(0,0),(tiny,0)],[(tiny,0),(0,0)],
        [(tiny/2,-tiny),(tiny/2,tiny)]])])
    assert line_self_intersection(frame).empty


@pytest.mark.parametrize("crs",["EPSG:3857","EPSG:2263"])
def test_line_self_intersection_preserves_binary_distinct_points_without_threshold(crs):
    tiny=2**-40
    shape=MultiLineString([[(0,0),(1,0)],[(0,-1),(0,1)],[(tiny,-1),(tiny,1)]])
    result=line_self_intersection(line_overlap_frame([shape],crs=crs))
    assert list(zip(result.geometry.x,result.geometry.y))==[(0,0),(tiny,0)] and result.crs==crs


def test_line_self_intersection_does_not_compare_distinct_features_or_deduplicate_between_sources():
    cross=line_overlap_frame([LineString([(0,0),(2,0)]),LineString([(1,-1),(1,1)])])
    assert line_self_intersection(cross).empty and len(line_intersection(cross))==1
    bow=LineString([(0,0),(2,2),(0,2),(2,0)])
    frame=line_overlap_frame([bow,bow],["乙","甲"]);frame.index=[5,5]
    result=line_self_intersection(frame)
    assert result.source_id.tolist()==["乙","甲"] and list(zip(result.geometry.x,result.geometry.y))==[(1,1)]*2


@pytest.mark.parametrize("ids",[pd.Series([2**60,2**60+1],dtype="Int64"),pd.Series(["甲","乙"],dtype="string")])
def test_line_self_intersection_keeps_identifier_type_and_empty_schema(ids):
    bow=LineString([(0,0),(2,2),(0,2),(2,0)])
    frame=line_overlap_frame([bow,LineString([(0,0),(1,0)])],ids)
    for source in [frame.iloc[:0],frame.iloc[1:],frame]:
        result=line_self_intersection(source)
        assert result.columns.tolist()==["source_id","geometry"] and result.source_id.dtype==ids.dtype and result.crs==source.crs
    assert line_self_intersection(frame).source_id.tolist()==ids.iloc[:1].tolist()


@pytest.mark.parametrize("ids",[[1,1],[None,1],[" ","x"],[[1],[2]]])
def test_line_self_intersection_rejects_invalid_identifiers(ids):
    with pytest.raises(ValueError,match="identifiers"):
        line_self_intersection(line_overlap_frame([LineString([(0,0),(1,0)])]*2,ids))


@pytest.mark.parametrize("field",[None,"","missing","boundary"])
def test_line_self_intersection_requires_explicit_nongeometry_identity(field):
    with pytest.raises(ValueError,match="id_field"):
        line_self_intersection(line_overlap_frame([LineString([(0,0),(1,0)])]),field)


@pytest.mark.parametrize("crs",[None,"EPSG:4326"])
def test_line_self_intersection_rejects_nonprojected_crs(crs):
    with pytest.raises(ValueError,match="projected"):
        line_self_intersection(line_overlap_frame([],crs=crs))


@pytest.mark.parametrize("shape",[None,LineString(),Point(0,0),box(0,0,1,1),
    shapely.from_wkt("MULTILINESTRING (EMPTY, (0 0, 1 0))"),LineString([(0,0),(0,0)]),
    LineString([(0,0,1),(1,0,1)]),shapely.from_wkt("LINESTRING M (0 0 1, 1 0 1)"),
    LineString([(0,0),(float('inf'),1)])])
def test_line_self_intersection_rejects_unsupported_geometry(shape):
    with pytest.raises(ValueError):line_self_intersection(line_overlap_frame([shape]))


def test_line_self_intersection_requires_active_geodataframe_and_unique_columns():
    operator=get_operator("check_line_self_intersection")
    with pytest.raises(ValueError):operator(input_gdf=pd.DataFrame(),id_field="route_key")
    frame=line_overlap_frame([LineString([(0,0),(1,0)])])
    with pytest.raises(ValueError):operator(input_gdf=pd.concat([frame,frame[['route_key']]],axis=1),id_field="route_key")
    with pytest.raises(ValueError):operator(input_gdf=frame.drop(columns='boundary'),id_field="route_key")


def test_line_self_intersection_matches_independent_integer_grid_oracle():
    horizontal=[(0,4,0),(2,6,2),(-2,2,4)];vertical=[(1,-1,5),(3,0,4),(5,1,3)]
    parts=[[(a,y),(b,y)] for a,b,y in horizontal]+[[(x,a),(x,b)] for x,a,b in vertical]
    expected=sorted({(x,y) for a,b,y in horizontal for x,c,d in vertical if a<=x<=b and c<=y<=d})
    result=line_self_intersection(line_overlap_frame([MultiLineString(parts)]))
    assert list(zip(result.geometry.x,result.geometry.y))==expected


@pytest.mark.parametrize("empty",[False,True])
def test_line_self_intersection_workflow_native_point_save_reload_and_discovery(tmp_path,empty):
    import api_server
    from osgeo import ogr
    tiny=2**-40
    shape=MultiLineString([[(0,0),(1,0)],[(0,-1),(0,1)],[(tiny,-1),(tiny,1)]])
    if empty:shape=LineString([(0,0),(1,0),(1,0),(2,1)])
    source=line_overlap_frame([shape],["原线甲"]);source.to_file(tmp_path/'source.gpkg',layer='routes',driver='GPKG')
    original=(tmp_path/'source.gpkg').read_bytes();connection={'engine_type':'nfs','mount_path':str(tmp_path)}
    tasks=[{'id':'load','operator':'load','params':{'connection_info':connection,'path':'source.gpkg'},'depends_on':[]},
           {'id':'check','operator':'check_line_self_intersection','params':{'input_gdf':{'$ref':'load'},'id_field':'route_key'},'depends_on':['load']},
           {'id':'save','operator':'save','params':{'input_df':{'$ref':'check'},'connection_info':connection,'path':'points.gpkg','mode':'fail'},'depends_on':['check']}]
    execution=execute_workflow({'tasks':tasks});assert execution['status']=='success',execution.get('error')
    dataset=ogr.Open(str(tmp_path/'points.gpkg'));layer=dataset.GetLayer(0)
    assert layer.GetFeatureCount()==(0 if empty else 2) and layer.GetSpatialRef().GetAuthorityCode(None)=='3857'
    assert [field.name for field in layer.schema]==['source_id']
    assert layer.GetGeomType()==(ogr.wkbUnknown if empty else ogr.wkbPoint)
    if not empty:
        for feature,x in zip(layer,[0,tiny]):
            point=feature.GetGeometryRef()
            assert feature.GetField('source_id')=='原线甲' and point.GetGeometryName()=='POINT' and point.IsValid()
            assert point.GetCoordinateDimension()==2 and point.GetPoint_2D()==(x,0)
    dataset=None
    assert (tmp_path/'source.gpkg').read_bytes()==original
    tasks[1]['params'].pop('id_field');tasks[2]['params']['path']='rejected.gpkg'
    assert execute_workflow({'tasks':tasks})['status']=='failed' and not (tmp_path/'rejected.gpkg').exists()
    with api_server.app.test_client() as client:
        op=next(o for o in client.get('/api/operators').get_json()['operators'] if o['id']=='check_line_self_intersection')
        assert op['display_name']=='线内自交' and op['category_path']==['矢量处理','几何处理']
        assert op['execution_modes']==['workflow'] and op['effects']==['read']
        assert [(p['name'],p['param_type'],p['required']) for p in op['parameters']]==[('input_gdf','input',True),('id_field','param',True)]
        assert [(p['name'],p['type'],p['is_default']) for p in op['output_ports']]==[('result','geodataframe',True)]
        assert client.post('/api/operators/check_line_self_intersection/invoke',json={'params':{}}).status_code==403
