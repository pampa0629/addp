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


def test_hosted_topology_physical_oracle_accepts_real_dual_save_and_rejects_corruption(tmp_path):
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
    execution = execute_workflow({'tasks': tasks})
    assert execution['status'] == 'success', execution.get('error')
    actual = fixture.worker('verify', tmp_path)
    assert actual['sources'] == seeded['sources']
    assert [actual['targets'][name]['feature_count'] for name in fixture.TARGETS] == [4,4,2]
    for name in fixture.TARGETS:
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
