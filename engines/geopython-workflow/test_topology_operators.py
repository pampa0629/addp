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
    execution = execute_workflow({'tasks': tasks})
    assert execution['status'] == 'success', execution.get('error')
    actual = fixture.worker('verify', tmp_path)
    assert actual['source'] == seeded['source']
    assert [actual['targets'][name]['feature_count'] for name in fixture.TARGETS] == [4,4]
    for name in fixture.TARGETS:
        path = tmp_path / 'outputs' / name
        original = path.read_bytes()
        content = json.loads(original)
        if name == 'faces.geojson':
            content['features'][0]['geometry']['coordinates'][0][0][0] += .1
        else:
            content['features'][0]['properties']['issue_type'] = 'cut_edge'
        path.write_text(json.dumps(content))
        with pytest.raises(fixture.FixtureError):
            fixture.worker('verify', tmp_path)
        path.write_bytes(original)
