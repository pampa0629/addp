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


def test_hosted_topology_physical_oracle_accepts_real_six_saves_and_rejects_corruption(tmp_path):
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
    execution = execute_workflow({'tasks': tasks})
    assert execution['status'] == 'success', execution.get('error')
    actual = fixture.worker('verify', tmp_path)
    assert actual['sources'] == seeded['sources']
    assert [actual['targets'][name]['feature_count'] for name in fixture.TARGETS] == [4,4,2,5,7,8]
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
    baseline = fixture.worker('verify', tmp_path)
    failed_tasks = [task.copy() for task in tasks if task['id'] in ('snap_input','snap_reference','snap','save_snapped')]
    failed_tasks[2]['params']['tolerance'] = 1.25
    failed_tasks[3]['params']['path'] = 'outputs/failed-snap.gpkg'
    failed = execute_workflow({'tasks': failed_tasks})
    assert failed['status'] == 'failed'
    assert 'snapping produced invalid geometries' in str(failed)
    assert not (tmp_path/'outputs/failed-snap.gpkg').exists()
    assert fixture.worker('verify', tmp_path) == baseline


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
