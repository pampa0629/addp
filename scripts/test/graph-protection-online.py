#!/usr/bin/env python3
"""Real User/Gateway/native graph protection acceptance on disposable Hosted."""
from __future__ import annotations

import importlib.util
import json
import math
import os
import subprocess
import sys
import time
import urllib.parse
import uuid
from pathlib import Path

SPEC = importlib.util.spec_from_file_location("graph_online_support", Path(__file__).with_name("security-transfer-protection-online.py"))
SUPPORT = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = SUPPORT
SPEC.loader.exec_module(SUPPORT)
Client, SuiteError = SUPPORT.GatewayClient, SUPPORT.SuiteError
OWNERS = {"graph", "develop", "manager", "service", "transfer"}
GRAPHS = ("graph_protection_fixture", "graph_protection_control")
READER_PERMISSIONS = {"graph.graph.read"}
DEVELOP_PERMISSIONS = {"develop.task.read", "develop.task.execute", "develop.data_read.execute",
                       "system.execution_authorization.create", "meta.catalog.read"}
OUTSIDER_PERMISSIONS = {"meta.catalog.read"}
PREPARER_PERMISSIONS = {
    "graph.ontology.create", "graph.ontology.read", "graph.ontology.delete",
    "graph.build_task.read", "graph.build_task.create", "graph.build_task.update",
    "graph.build_task.execute", "graph.build_task.cancel", "graph.build_task.delete",
    "graph.graph.create", "graph.graph.read", "graph.graph.update", "graph.graph.delete",
    "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read",
    "security.enrollment.create", "security.enrollment.read", "security.enrollment.update",
}


def require(condition, message):
    if not condition:
        raise SuiteError(message)


def engine_type():
    value = SUPPORT.required_environment("ADDP_ONLINE_GRAPH_ENGINE")
    require(value in {"falkordb", "neo4j"}, "Unsupported Graph acceptance engine")
    return value


def target_paths(engine_id):
    kind = engine_type()
    control_id = engine_id if kind == "falkordb" else SUPPORT.positive_int(
        SUPPORT.required_environment("ADDP_ONLINE_GRAPH_CONTROL_ENGINE_ID"), "control engine id")
    require(kind != "neo4j" or control_id != engine_id, "Neo4j control must be an independent engine instance")
    paths = []
    for name, eid in zip(GRAPHS, (engine_id, control_id)):
        names = [name] if kind == "falkordb" else ["neo4j", "graph"]
        segments = [{"term": "server", "kind": "server", "name": ""}]
        if kind == "neo4j":
            segments.append({"term": "database", "kind": "namespace", "name": "neo4j"})
        segments.append({"term": "graph", "kind": "graph", "name": names[-1]})
        paths.append({"engine_id": eid, "full_name": ".".join(names), "names": names,
                      "catalog_path": {"version": "catalog.path/v1", "engine_id": eid, "segments": segments}})
    return paths


def identity(client, tenant_id, permissions):
    context = client.request("GET", "/api/v1/system/auth/context", (200,)).payload
    require(context["principal"]["type"] == "user", "A real User identity is required")
    require(context["context"]["type"] == "tenant" and context["context"]["tenant_id"] == str(tenant_id), "Wrong fixture Tenant")
    require(context["token"]["type"] in {"first_party_access_token", "oauth_access_token"}, "A User Access Token is required")
    assignments = context["authorization"]["role_assignments"]
    roles = {a["role_key"] for a in assignments}
    actual = {p for a in assignments for p in a["permissions"]}
    require(not roles & SUPPORT.FORBIDDEN_ADMIN_ROLES, "Administrator roles must not enter Graph acceptance")
    require(actual == permissions, "Fixture identity does not have exactly its minimum permissions")
    return {"principal_id": str(context["principal"]["id"]), "permissions": sorted(actual), "administrator": False}


def converged(enrollment, released=False):
    progress = enrollment.get("owner_progress", [])
    require(len(progress) == len(OWNERS) and {p.get("consumer_owner") for p in progress} == OWNERS, "All five protection Owners must be present exactly once")
    if released:
        return enrollment.get("state") == "released" and all(p.get("acknowledged") is True and p.get("validity") == "released" for p in progress)
    require(enrollment.get("state") in {"activating", "enrolling", "active"}, "Enrollment unexpectedly stopped protecting")
    if enrollment["state"] == "activating":
        return False
    return all(p.get("acknowledged") is True and p.get("validity") == "valid" and p.get("projection_id") and p.get("revision") and p.get("published_cursor") for p in progress)


def wait_owners(client, enrollment_id, deadline, released=False):
    while time.monotonic() < deadline:
        data = client.request("GET", "/api/v1/security/protection-enrollments/" + enrollment_id, (200,)).payload
        if converged(data, released):
            return data
        time.sleep(1)
    raise SuiteError("Five-Owner projection confirmation timed out")


def routes(ids):
    first, last = ids["p1"], ids["p3"]
    return [
        ("GET", "entities/Person", None),
        ("GET", "entities/Person/" + first, None),
        ("GET", "nodes/" + first + "/neighbors", None),
        ("GET", "search?q=Fixture", None),
        ("GET", "ontology", None),
        ("GET", "stats", None),
        ("POST", "paths", {"source_node_id": first, "target_node_id": last, "max_hops": 3}),
        ("POST", "subgraph", {"node_id": first, "depth": 1, "limit": 20}),
    ]


def probe(client, graph_id, ids, expected, reason=None):
    results = []
    for method, route, body in routes(ids):
        response = client.request(method, f"/api/v1/graph/kg/{graph_id}/{route}", (expected,), body)
        payload = response.payload
        if reason == "permission":
            require(payload.get("error_code") == "permission_denied", "Expected the functional permission boundary")
        elif reason == "protection":
            require(payload.get("error") == "图谱数据已纳入数据保护，当前读取方式暂不支持安全执行" or
                    "data protection" in str(payload.get("error", "")).lower() or
                    "数据保护" in str(payload.get("error", "")), "Expected a Security denial, not an unrelated 403")
            require("data" not in payload and "nodes" not in payload and "properties" not in payload, "Protected response leaked graph data")
        elif expected == 200:
            require(isinstance(payload, (dict, list)) and bool(payload), "Successful graph response must contain data")
            if route == "stats":
                require(payload.get("node_count") == 3 and payload.get("relationship_count") == 2, "Native graph counts changed")
            if route in {"entities/Person", "search?q=Fixture"}:
                require(payload.get("total") == 3 and len(payload.get("data", [])) == 3, "Synthetic entities/search must return three real results")
                require({e.get("properties", {}).get("id") for e in payload["data"]} == {"p1", "p2", "p3"}, "Synthetic properties were not preserved")
            elif route.startswith("entities/Person/"):
                require(payload.get("id") == ids["p1"] and payload.get("properties", {}).get("name") == "Fixture one", "Entity detail did not return the real synthetic entity")
            elif route.endswith("/neighbors"):
                neighbors = payload.get("neighbors", [])
                require(len(neighbors) == 1 and neighbors[0].get("node", {}).get("properties", {}).get("id") == "p2", "Expected the real adjacent Person")
            elif route == "paths":
                require(len(payload.get("nodes", [])) == 3 and len(payload.get("edges", [])) == 2, "Expected the native two-hop path")
            elif route == "subgraph":
                require(len(payload.get("nodes", [])) == 2 and len(payload.get("edges", [])) == 1, "Expected the real one-hop subgraph")
            elif route == "ontology":
                require([e.get("name") for e in payload.get("entity_types", [])] == ["Person"] and
                        [r.get("name") for r in payload.get("relation_types", [])] == ["Link"], "Synthetic ontology mapping changed")
        results.append({"method": method, "route": route, "status": response.status, "boundary": reason or "data"})
    return results


def set_public(preparer, graph_id, public):
    result = preparer.request("PUT", f"/api/v1/graph/graphs/{graph_id}", (200,), {"is_public": public}).payload
    require(result.get("is_public") is public, "Public state was not persisted canonically")
    require(preparer.request("GET", f"/api/v1/graph/graphs/{graph_id}", (200,)).payload.get("is_public") is public, "Public state read-back mismatch")


def create_ontology(client):
    ontology = client.request("POST", "/api/v1/graph/ontologies", (201,), {"name": "Hosted synthetic Graph protection"}).payload
    return SUPPORT.positive_int(ontology.get("id"), "ontology id")


def create_schema(client, oid):
    entity = client.request("POST", f"/api/v1/graph/ontologies/{oid}/entity-types", (201,), {
        "name": "Person", "node_labels": ["Person"], "display_property": "name",
        "properties": [{"name": p, "data_type": "string", "searchable": True, "unique": p == "id"} for p in ("id", "name")],
    }).payload
    client.request("POST", f"/api/v1/graph/ontologies/{oid}/relation-types", (201,), {
        "name": "Link", "source_type_id": entity["id"], "target_type_id": entity["id"], "directed": True,
    })


def verify_search_frame(visual, expected):
    require(type(visual) is dict and set(visual) == set(expected) | {"schema_version", "frame", "hits"} and
            all(type(visual.get(key)) is type(value) and visual[key] == value for key, value in expected.items()) and
            visual.get("schema_version") == "addp.graph-search-stable-frame/v2", "Search frame identity differs")
    verify_pixel_frame(visual.get("frame"), visual.get("hits"), expected["native_ids"])
    require(visual["frame"]["color"] != visual["frame"]["fillColor"], "Search frame omitted its distinct highlight color")


def verify_pixel_frame(frame, hits, native_ids):
    number = lambda value: type(value) in (int, float) and math.isfinite(value)
    require(type(frame) is dict and set(frame) == {"width", "height", "color", "fillColor", "nodes", "stableFrames"} and
            all(number(frame.get(key)) and frame[key] > 0 for key in ("width", "height")) and
            type(frame.get("stableFrames")) is int and frame["stableFrames"] >= 10 and
            all(type(frame.get(key)) is list and len(frame[key]) == 3 and
                all(type(value) is int and 0 <= value <= 255 for value in frame[key]) for key in ("color", "fillColor")), "Search frame did not settle")
    nodes = frame.get("nodes")
    require(type(nodes) is list and type(hits) is list and len(nodes) == len(hits) == len(native_ids), "Graph frame needs every expected visible hit")
    for node, hit in zip(nodes, hits):
        require(type(node) is dict and set(node) == {"x", "y", "box", "fillBox", "pixelCount", "fillPixelCount"} and
                all(type(node.get(key)) is list and len(node[key]) == 4 and all(number(v) for v in node[key]) for key in ("box", "fillBox")) and
                all(number(node.get(key)) for key in ("x", "y")) and
                all(type(node.get(key)) is int and node[key] >= 40 for key in ("pixelCount", "fillPixelCount")), "Missing search pixel bounds")
        x0, y0, x1, y1 = node["box"]
        require(1 < x0 < node["x"] < x1 < frame["width"] - 1 and
                1 < y0 < node["y"] < y1 < frame["height"] - 1, "Search node outside visible viewport")
        fx0, fy0, fx1, fy1 = node["fillBox"]
        require(x0 <= fx0 < fx1 <= x1 and y0 <= fy0 < fy1 <= y1 and
                fx1 - fx0 >= 9 and fy1 - fy0 >= 9 and x1 - x0 >= 9 and y1 - y0 >= 9 and
                0.7 < (fx1 - fx0 + 1) / (fy1 - fy0 + 1) < 1.4 and
                0.7 < (x1 - x0 + 1) / (y1 - y0 + 1) < 1.4, "Highlight must surround the visible fill core")
        require(type(hit) is dict and set(hit) == {"source_id", "native_id", "x", "y"} and
                hit["x"] == node["x"] and hit["y"] == node["y"] and type(hit["source_id"]) is str and
                type(hit["native_id"]) is str and hit["native_id"] == native_ids.get(hit["source_id"]),
                "Search click did not bind the actual native node")
    require(len({hit["source_id"] for hit in hits}) == len(native_ids) and
            {hit["source_id"] for hit in hits} == set(native_ids), "Search clicks missed native identities")


def verify_native_edges(edges, ids):
    require(type(edges) is list and len(edges) == 2 and all(type(edge) is dict and
            set(edge) == {"id", "source", "target", "type"} and all(type(value) is str and value for value in edge.values()) and
            edge["type"] == "Link" for edge in edges), "Exploration needs the two original native relations")
    require(len({edge["id"] for edge in edges}) == 2 and
            {(edge["source"], edge["target"]) for edge in edges} == {(ids["p1"], ids["p2"]), (ids["p2"], ids["p3"])}, "Exploration native relation endpoints differ")


def native_path_edges(reader, graph_id, ids):
    path = reader.request("POST", f"/api/v1/graph/kg/{graph_id}/paths", (200,), {
        "source_node_id": ids["p1"], "target_node_id": ids["p3"], "max_hops": 3,
    }).payload
    nodes = path.get("nodes", [])
    require(type(nodes) is list and len(nodes) == 3 and
            {node.get("properties", {}).get("id"): node.get("id") for node in nodes} == ids,
            "Native reference path must bind all three fixture entities")
    raw_edges = path.get("edges", [])
    require(type(raw_edges) is list and all(type(edge) is dict for edge in raw_edges), "Native reference path omitted relations")
    edges = [{key: edge.get(key) for key in ("id", "source", "target", "type")} for edge in raw_edges]
    verify_native_edges(edges, ids)
    return sorted(edges, key=lambda edge: edge["id"])


def verify_exploration(visual, expected):
    require(type(visual) is dict and set(visual) == set(expected) | {"schema_version", "stages", "requests", "native_edges"} and
            all(type(visual.get(key)) is type(value) and visual[key] == value for key, value in expected.items()) and
            visual.get("schema_version") == "addp.graph-exploration-stable-frame/v2", "Exploration identity differs")
    ids = expected["native_ids"]
    edges, stages = visual.get("native_edges"), visual.get("stages")
    verify_native_edges(edges, ids)
    labels = [("dagre", "分层"), ("circular", "环形"), ("radial", "辐射"), ("force", "力导向")]
    phases = ["expand-1", "expand-2", *("layout-" + key for key, _ in labels)]
    require(type(stages) is list and len(stages) == len(phases), "Exploration omitted expansion or layout frames")
    requests, previous_positions, color = [], None, None
    for index, (stage, phase) in enumerate(zip(stages, phases)):
        keys = {"phase", "native_ids", "edges", "frame", "hits"} | ({"request", "http_status"} if index < 2 else {"layout", "selected_label"})
        require(type(stage) is dict and set(stage) == keys and stage["phase"] == phase, "Exploration stage/order differs")
        wanted = {key: ids[key] for key in ("p1", "p2")} if index == 0 else ids
        wanted_edges = [edge for edge in edges if edge["source"] in wanted.values() and edge["target"] in wanted.values()]
        require(stage["native_ids"] == wanted and stage["edges"] == wanted_edges, "Expansion lost or changed native membership")
        verify_pixel_frame(stage["frame"], stage["hits"], wanted)
        if color is None: color = stage["frame"]["color"]
        require(stage["frame"]["color"] == stage["frame"]["fillColor"] == color, "Exploration fill observation color changed")
        positions = {hit["native_id"]: (hit["x"], hit["y"]) for hit in stage["hits"]}
        if index < 2:
            request = stage["request"]
            wanted_request = {"target": {"kind": "entity", "id": ids["p1" if index == 0 else "p2"]}, "depth": 1, "node_limit": 200, "relationship_limit": 400}
            require(type(stage["http_status"]) is int and stage["http_status"] == 200 and request == wanted_request and
                    all(type(request[key]) is int for key in ("depth", "node_limit", "relationship_limit")), "Wrong native expand command/status")
            requests.append({"method": "POST", "path": f"/api/v1/graph/graphs/{expected['graph_id']}/expand", "body": wanted_request})
        else:
            require(stage["layout"] == labels[index-2][0] and stage["selected_label"] == labels[index-2][1] and
                    positions != previous_positions, "Layout selection did not change visible node geometry")
        previous_positions = positions
    require(visual["requests"] == requests, "Layout change issued requests or expansion sequence differed")


def run_browser(report, graph_id, engine_id, tenant_id, native_ids, native_edges):
    verify_native_edges(native_edges, native_ids)
    env = SUPPORT.required_environment
    env("ADDP_ONLINE_TEST_USER_USERNAME")
    env("ADDP_ONLINE_TEST_USER_PASSWORD")
    artifacts = Path(env("ADDP_ONLINE_ARTIFACT_DIR"))
    output = artifacts / "graph-protection-browser.json"
    diagnostics = artifacts / "graph-browser-diagnostics.json"
    visual = artifacts / "graph-search-stable-frame.json"
    exploration = artifacts / "graph-exploration-stable-frame.json"
    screenshots = [artifacts / name for name in ("graph-private-browse-console.png", "graph-private-knowledge-console.png",
                   *(f"graph-private-theme-{mode}.png" for mode in ("light", "dark", "blue", "purple")),
                   *(f"graph-{phase}-console.png" for phase in ("expand-1", "expand-2", "layout-dagre", "layout-circular", "layout-radial", "layout-force")))]
    for path in [output, diagnostics, visual, exploration, *screenshots]:
        path.unlink(missing_ok=True)
    expected = {"run_id": env("ADDP_ONLINE_TEST_RUN_ID"), "tenant_id": str(tenant_id),
                "principal_id": report["identities"]["reader"]["principal_id"], "engine_id": engine_id, "graph_id": graph_id, "engine_type": engine_type(), "native_ids": native_ids, "native_edges": native_edges}
    # The browser authenticates the ordinary User through the formal login UI.
    # It must not inherit any API fixture's preparation or consumption tokens.
    environment = {key: value for key, value in os.environ.items() if not key.endswith("_ACCESS_TOKEN")}
    environment.update(ADDP_ONLINE_GRAPH_BROWSER_EXPECTATIONS=json.dumps(expected), ADDP_ONLINE_GRAPH_BROWSER_REPORT=str(output))
    result = subprocess.run(["npm", "exec", "--", "playwright", "test", "e2e/online/graph-protection.spec.js",
                             "--config=playwright.online.config.js", "--grep", "ordinary Graph reader"],
                            cwd=Path(__file__).resolve().parents[2] / "console/frontend", env=environment, timeout=240)
    require(result.returncode == 0 and output.is_file() and all(path.is_file() for path in screenshots),
            "Ordinary Graph Console acceptance failed or omitted browser evidence")
    require(diagnostics.is_file() and json.loads(diagnostics.read_text()) == {
        "schema_version": "addp.graph-browser-diagnostics/v1", "entries": []
    }, "Graph Console omitted clean browser diagnostics")
    require(visual.is_file(), "Search stable frame evidence missing")
    verify_search_frame(json.loads(visual.read_text()), expected)
    require(exploration.is_file(), "Expansion/layout stable frame evidence missing")
    verify_exploration(json.loads(exploration.read_text()), expected)
    evidence = json.loads(output.read_text())
    expected.update(schema_version="addp.graph-protection-browser/v7", result="passed", permissions=sorted(READER_PERMISSIONS),
                    node_count=3, relationship_count=2, browse_snapshot=True, canvas_visible=True, native_search=True, knowledge_search=True, search_stable_visible=True, search_node_hits=3,
                    expansion_stable_visible=True, layout_stable_visible=True, layout_modes=["dagre", "circular", "radial", "force"],
                    public_switch_disabled=True, graph_writes=0, failed_business_responses=0, browser_errors=0,
                    release_assets=True, deep_link_reload=True, theme_modes=["light", "dark", "blue", "purple"], theme_canvas_pixels=True, theme_typography=True)
    require(isinstance(evidence, dict) and set(evidence) == set(expected) | {"anonymous_refresh_401"} and
            all(type(evidence[key]) is type(value) and evidence[key] == value for key, value in expected.items()) and
            type(evidence["anonymous_refresh_401"]) is int and 0 <= evidence["anonymous_refresh_401"] <= 1,
            "Graph Console evidence differs from the same minimum-permission User, native graph or clean diagnostics")
    return evidence


def dense_fixture_contract():
    spec = importlib.util.spec_from_file_location("dense_native_fixture", Path(__file__).resolve().parents[2] / "business/scripts/online-graph-fixture.py")
    fixture = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(fixture)
    return fixture.DENSE_NODES, fixture.DENSE_EDGES


def verify_dense_reference(nodes, edges):
    labels, relations = dense_fixture_contract()
    require(type(nodes) is list and len(nodes) == len(labels), "Dense native node count differs")
    ids = {}
    for node in nodes:
        key = node.get("properties", {}).get("id")
        require(key in labels and key not in ids and type(node.get("id")) is str and bool(node["id"]) and
                sorted(node.get("labels", [])) == sorted(labels[key]) and node["properties"] == {"id": key, "name": "Dense " + key[1:]}, "Dense native labels/properties differ")
        ids[key] = node["id"]
    require(len(set(ids.values())) == len(labels) and set(ids) == set(labels), "Dense native identity collision")
    reverse = {value: key for key, value in ids.items()}
    require(type(edges) is list and len(edges) == len(relations) and len({e.get("id") for e in edges}) == len(relations), "Dense native edge count/identity differs")
    actual = []
    for edge in edges:
        require(set(edge) == {"id", "source", "target", "type"} and type(edge["id"]) is str and bool(edge["id"]) and
                edge["source"] in reverse and edge["target"] in reverse, "Dense native edge endpoints differ")
        actual.append((reverse[edge["source"]], reverse[edge["target"]], edge["type"]))
    require(sorted(actual) == sorted(relations), "Dense native relation direction/type/multiplicity differs")
    return ids


def verify_dense(visual, expected):
    require(type(visual) is dict and set(visual) == set(expected) | {"schema_version", "stages", "requests"} and
            all(visual.get(key) == value for key, value in expected.items()) and
            visual["schema_version"] == "addp.graph-dense-stable-frame/v1", "Dense evidence identity differs")
    ids = verify_dense_reference(expected["nodes"], expected["edges"])
    shapes = expected["shapes"]
    label_sets = {tuple(sorted(labels)) for labels in dense_fixture_contract()[0].values()}
    require(len(shapes) == len(label_sets) and {tuple(sorted(shape["labels"])) for shape in shapes} == label_sets and
            len({shape["name"] for shape in shapes}) == len(shapes), "Dense shape membership differs")
    phases = ["dense-search", "dense-search-cleared", "dense-expand", "dense-filter", "dense-filter-restored"] + ["dense-layout-"+key for key in ("dagre", "circular", "radial", "force")]
    stages = visual["stages"]
    require(type(stages) is list and [stage.get("phase") for stage in stages] == phases, "Dense stages missing")
    previous_positions = None
    for index, stage in enumerate(stages):
        members = [node for node in expected["nodes"] if index != 3 or sorted(node["labels"]) == ["Employee", "Person"]]
        selected_ids = {node["id"] for node in members}
        edges = [] if index < 2 else [edge for edge in expected["edges"] if edge["source"] in selected_ids and edge["target"] in selected_ids]
        extra = {"request", "http_status"} if index in (0, 2) else {"selected_shapes"} if index in (3, 4) else {"layout", "selected_label"} if index >= 5 else set()
        require(set(stage) == {"phase", "nodes", "edges", "groups", "relationship_pixels"} | extra and
                stage["nodes"] == members and stage["edges"] == edges, "Dense stage native membership differs")
        relation_pixels = stage["relationship_pixels"]
        require(type(relation_pixels) is list and [item.get("type") for item in relation_pixels] == sorted({edge["type"] for edge in edges}), "Dense visible relationship types missing")
        for sample in relation_pixels:
            require(set(sample) == {"type", "color", "pixelCount", "stableFrames"} and type(sample["pixelCount"]) is int and sample["pixelCount"] >= 40 and
                    type(sample["stableFrames"]) is int and sample["stableFrames"] >= 10 and type(sample["color"]) is list and len(sample["color"]) == 3 and
                    all(type(value) is int and 0 <= value <= 255 for value in sample["color"]), "Dense relation strokes not visibly stable")
        require(len({tuple(sample["color"]) for sample in relation_pixels}) == len(relation_pixels), "Dense relation colors collide")
        groups = stage["groups"]
        wanted_shapes = [shape for shape in shapes if any(sorted(node["labels"]) == sorted(shape["labels"]) for node in members)]
        require(type(groups) is list and len(groups) == len(wanted_shapes), "Dense color group missing")
        positions = []
        require(len({(group["frame"]["width"], group["frame"]["height"], group["frame"]["stableFrames"]) for group in groups}) == 1, "Dense groups did not settle simultaneously")
        for group, shape in zip(groups, wanted_shapes):
            require(set(group) == {"name", "labels", "frame", "hits"} and group["name"] == shape["name"] and group["labels"] == sorted(shape["labels"]), "Dense color shape differs")
            group_ids = {node["properties"]["id"]: node["id"] for node in members if sorted(node["labels"]) == sorted(shape["labels"])}
            verify_pixel_frame(group["frame"], group["hits"], group_ids)
            require((group["frame"]["color"] != group["frame"]["fillColor"]) if index == 0 else (group["frame"]["color"] == group["frame"]["fillColor"]), "Dense search/ordinary color evidence differs")
            positions.extend((hit["native_id"], hit["x"], hit["y"]) for hit in group["hits"])
        require(len({tuple(group["frame"]["fillColor"]) for group in groups}) == len(groups), "Dense label colors collide")
        require(len({(x, y) for _, x, y in positions}) == len(members), "Dense color groups duplicate visible nodes")
        if index == 0:
            require(stage["http_status"] == 200 and stage["request"] == {"query": "Dense", "limit": 30}, "Dense search request differs")
        if index == 2:
            require(stage["http_status"] == 200 and stage["request"] == {"target": {"kind": "entity", "id": ids["n01"]}, "depth": 2, "node_limit": 200, "relationship_limit": 400}, "Dense expansion request differs")
        if index in (3, 4):
            wanted = [shape["name"] for shape in wanted_shapes]
            require(stage["selected_shapes"] == sorted(wanted), "Dense filter selection differs")
        if index >= 5:
            layout, label = [("dagre", "分层"), ("circular", "环形"), ("radial", "辐射"), ("force", "力导向")][index-5]
            require(stage["layout"] == layout and stage["selected_label"] == label and sorted(positions) != previous_positions, "Dense layout did not visibly change")
        previous_positions = sorted(positions)
    root = f"/api/v1/graph/graphs/{expected['graph_id']}"
    require(visual["requests"] == [{"method": "POST", "path": root + "/search", "body": stages[0]["request"]},
        {"method": "POST", "path": root + "/expand", "body": stages[2]["request"]}], "Dense filter/layout made unexpected requests")


def run_dense(preparer, reader, tenant_id, target_path, report, previous_control_graph_id):
    ontology_id = graph_id = None
    failed = False
    try:
        seeded = subprocess.run([sys.executable, str(Path(__file__).resolve().parents[2] / "business/scripts/online-graph-fixture.py"), "exploration", "--previous-control-graph-id", str(previous_control_graph_id)], capture_output=True, text=True, timeout=300)
        require(seeded.returncode == 0, "Owned native dense fixture preparation failed")
        print(seeded.stdout.strip())
        ontology_id = SUPPORT.positive_int(preparer.request("POST", "/api/v1/graph/ontologies", (201,), {"name": "Hosted dense Graph exploration"}).payload.get("id"), "dense ontology id")
        for labels in sorted({tuple(labels) for labels in dense_fixture_contract()[0].values()}):
            preparer.request("POST", f"/api/v1/graph/ontologies/{ontology_id}/entity-types", (201,), {
                "name": "+".join(labels), "node_labels": list(labels), "display_property": "name",
                "properties": [{"name": key, "data_type": "string", "searchable": True, "unique": key == "id"} for key in ("id", "name")]})
        graph = preparer.request("POST", "/api/v1/graph/graphs", (201,), {"name": "Hosted multi-label dense fixture", "ontology_id": ontology_id,
            "engine_id": target_path["engine_id"], "catalog_path": target_path["catalog_path"], "index_management": "managed"}).payload
        graph_id = SUPPORT.positive_int(graph.get("id"), "dense graph id")
        require(graph.get("is_public") is False, "Dense graph must remain private")
        context = identity(reader, tenant_id, READER_PERMISSIONS)
        require(context == report["identities"]["reader"], "Dense phase changed the original reader identity")
        found = reader.request("GET", f"/api/v1/graph/kg/{graph_id}/search?q=Dense&page_size=100", (200,)).payload
        require(found.get("total") == 12 and len(found.get("data", [])) == 12, "Dense independent search incomplete")
        anchor = next(entity["id"] for entity in found["data"] if entity["properties"]["id"] == "n01")
        native = reader.request("POST", f"/api/v1/graph/kg/{graph_id}/subgraph", (200,), {"node_id": anchor, "depth": 2, "limit": 100}).payload
        nodes = sorted([{"id": node["id"], "labels": sorted(node["labels"]), "properties": node["properties"]} for node in native["nodes"]], key=lambda node: node["id"])
        edges = sorted([{key: edge[key] for key in ("id", "source", "target", "type")} for edge in native["edges"]], key=lambda edge: edge["id"])
        verify_dense_reference(nodes, edges)
        snapshot = reader.request("GET", f"/api/v1/graph/graphs/{graph_id}/browse-snapshot", (200,)).payload
        require(snapshot["stats"]["node_count"] == 12 and snapshot["stats"]["relationship_count"] == 72 and snapshot["stats"].get("by_label") == {"Person": 6, "Employee": 3, "Organization": 6, "Partner": 3}, "Dense stats incomplete or overlapping labels double-counted")
        require(len(snapshot["schema"]["node_shapes"]) == 4 and all(type(shape.get("count")) is int and shape["count"] == 3 for shape in snapshot["schema"]["node_shapes"]), "Dense full-label-set counts differ")
        shapes = sorted([{"name": shape["name"], "labels": sorted(shape["labels"])} for shape in snapshot["schema"]["node_shapes"]], key=lambda shape: shape["name"])
        expected = {"run_id": SUPPORT.required_environment("ADDP_ONLINE_TEST_RUN_ID"), "tenant_id": str(tenant_id), "principal_id": context["principal_id"],
            "engine_type": engine_type(), "engine_id": target_path["engine_id"], "graph_id": graph_id, "nodes": nodes, "edges": edges, "shapes": shapes}
        artifact = Path(SUPPORT.required_environment("ADDP_ONLINE_ARTIFACT_DIR"))
        raw = artifact / "graph-dense-stable-frame.json"
        raw.unlink(missing_ok=True)
        diagnostics = artifact / "graph-dense-browser-diagnostics.json"
        diagnostics.unlink(missing_ok=True)
        for path in artifact.glob("dense-*-console.png"): path.unlink()
        env = {key: value for key, value in os.environ.items() if not key.endswith(("_ACCESS_TOKEN", "_USERNAME", "_PASSWORD"))}
        for key in ("ADDP_ONLINE_TEST_USER_USERNAME", "ADDP_ONLINE_TEST_USER_PASSWORD"):
            env[key] = SUPPORT.required_environment(key)
        env["ADDP_ONLINE_GRAPH_DENSE_EXPECTATIONS"] = json.dumps(expected)
        result = subprocess.run(["npm", "exec", "--", "playwright", "test", "e2e/online/graph-protection.spec.js", "--config=playwright.online.config.js", "--grep", "multi-label dense", "--reporter=line"], cwd=Path(__file__).resolve().parents[2] / "console/frontend", env=env, timeout=300)
        require(result.returncode == 0 and raw.is_file(), "Dense Console evidence missing or failed")
        require(diagnostics.is_file() and json.loads(diagnostics.read_text()) == {"schema_version": "addp.graph-browser-diagnostics/v1", "entries": []}, "Dense browser diagnostics missing or dirty")
        visual = json.loads(raw.read_text())
        verify_dense(visual, expected)
        for stage in visual["stages"]:
            require((artifact / (stage["phase"] + "-console.png")).is_file(), "Dense screenshot missing")
        report["dense"] = {"schema_version": "addp.graph-dense-online/v1", "passed": True, **expected, "stages": [stage["phase"] for stage in visual["stages"]]}
    except Exception:
        failed = True
        raise
    finally:
        errors = []
        for kind, identifier in (("graphs", graph_id), ("ontologies", ontology_id)):
            if identifier:
                try:
                    preparer.request("DELETE", f"/api/v1/graph/{kind}/{identifier}", (200,))
                    preparer.request("GET", f"/api/v1/graph/{kind}/{identifier}", (404,))
                except Exception: errors.append("delete dense " + kind)
        report["dense_cleanup"] = {"errors": errors, "domain_resources_remaining": 0 if not errors else None}
        if not failed: require(not errors, "Dense domain cleanup failed")


def run_develop_browser(report, engine_id, tenant_id, phase):
    require(phase in {"unmanaged", "protected"}, "Unknown Develop acceptance phase")
    env = SUPPORT.required_environment
    developer = identity(Client(env("GATEWAY_URL"), env("ADDP_ONLINE_GRAPH_DEVELOP_ACCESS_TOKEN"), 30), tenant_id, DEVELOP_PERMISSIONS)
    others = {entry["principal_id"] for key, entry in report["identities"].items() if key != "develop"}
    require(developer["principal_id"] not in others, "Develop must use its separate minimum-permission User")
    require(report["identities"].get("develop", developer) == developer, "Develop User changed between protection phases")
    report["identities"]["develop"] = developer
    artifacts = Path(env("ADDP_ONLINE_ARTIFACT_DIR"))
    output = artifacts / f"develop-graph-{phase}-browser.json"
    names = ("graph", "table", *("theme-" + mode for mode in ("light", "dark", "blue", "purple"))) if phase == "unmanaged" else ("protected", "control")
    screenshots = [artifacts / f"develop-graph-{name}-console.png" for name in names]
    for path in [output, *screenshots]:
        path.unlink(missing_ok=True)
    expected = {"run_id": env("ADDP_ONLINE_TEST_RUN_ID"), "tenant_id": str(tenant_id),
                "principal_id": developer["principal_id"], "engine_id": engine_id, "phase": phase, "engine_type": engine_type(),
                "targets": target_paths(engine_id)}
    environment = {key: value for key, value in os.environ.items()
                   if not key.endswith(("_ACCESS_TOKEN", "_USERNAME", "_PASSWORD"))}
    environment.update(ADDP_ONLINE_GRAPH_DEVELOP_USERNAME=env("ADDP_ONLINE_GRAPH_DEVELOP_USERNAME"),
                       ADDP_ONLINE_GRAPH_DEVELOP_PASSWORD=env("ADDP_ONLINE_GRAPH_DEVELOP_PASSWORD"),
                       ADDP_ONLINE_DEVELOP_GRAPH_EXPECTATIONS=json.dumps(expected),
                       ADDP_ONLINE_DEVELOP_GRAPH_REPORT=str(output))
    result = subprocess.run(["npm", "exec", "--", "playwright", "test", "e2e/online/develop-graph-query.spec.js",
                             "--config=playwright.online.config.js"],
                            cwd=Path(__file__).resolve().parents[2] / "console/frontend", env=environment, timeout=240)
    require(result.returncode == 0 and output.is_file() and all(path.is_file() for path in screenshots),
            "Ordinary Develop Console acceptance failed or omitted evidence")
    evidence = json.loads(output.read_text())
    expected.update(schema_version="addp.develop-graph-query-browser/v1", result="passed", permissions=sorted(DEVELOP_PERMISSIONS),
                    graph_nodes=3, graph_relationships=2, graph_rows=2, graph_canvas=True,
                    browser_errors=0, failed_business_responses=0, unexpected_writes=0)
    if phase == "unmanaged":
        expected.update(theme_modes=["light", "dark", "blue", "purple"], theme_canvas_pixels=True,
                        theme_editor_colors=True, theme_completion_colors=True,
                        typed_parameters=True, table_rows=1, table_visible=True, graph_table_visible=True)
    else:
        # This fixture enrolls the graph without publishing protection rules.
        # FalkorDB proves the exact enrolling graph; Neo4j cannot prove its ReadSet.
        reason = {"falkordb": "protection_not_ready", "neo4j": "protection_lineage_unverified"}[engine_type()]
        expected.update(protection_required=True, protected_result_absent=True, protection_stage="preparation",
                        protection_reason=reason, control_readable=True)
    require(isinstance(evidence, dict) and set(evidence) == set(expected) | {"anonymous_refresh_401", "execution_ids"} and
            all(type(evidence[key]) is type(value) and evidence[key] == value for key, value in expected.items()) and
            type(evidence["anonymous_refresh_401"]) is int and 0 <= evidence["anonymous_refresh_401"] <= 1 and
            isinstance(evidence["execution_ids"], list) and len(set(evidence["execution_ids"])) == 2 and
            all(isinstance(value, str) and uuid.UUID(value).version == 4 for value in evidence["execution_ids"]),
            "Develop evidence differs from its same User, native query, protection boundary or clean diagnostics")
    return evidence


def run(preparer, reader, outsider, anonymous, direct_reader, direct_outsider, direct_anonymous, engine_id, tenant_id, deadline, report):
    report["identities"] = {"reader": identity(reader, tenant_id, READER_PERMISSIONS),
                            "outsider": identity(outsider, tenant_id, OUTSIDER_PERMISSIONS),
                            "preparer": identity(preparer, tenant_id, PREPARER_PERMISSIONS)}
    require(len({x["principal_id"] for x in report["identities"].values()}) == 3, "Fixture identities must be distinct")
    ontology_id = None
    graph_ids = []
    enrollment_id = None
    operation_failed = False
    try:
        ontology_id = create_ontology(preparer)
        create_schema(preparer, ontology_id)
        targets = target_paths(engine_id)
        report["engine_type"] = engine_type()
        report["targets"] = targets
        for name, target_path in zip(GRAPHS, targets):
            graph = preparer.request("POST", "/api/v1/graph/graphs", (201,), {
                "name": name, "ontology_id": ontology_id, "engine_id": target_path["engine_id"], "index_management": "managed",
                "catalog_path": target_path["catalog_path"],
            }).payload
            graph_ids.append(SUPPORT.positive_int(graph.get("id"), "graph id"))
            require(graph.get("is_public") is False, "Synthetic graphs must begin private")
        target, control = graph_ids
        entities = reader.request("GET", f"/api/v1/graph/kg/{target}/entities/Person", (200,)).payload["data"]
        ids = {e["properties"]["id"]: e["id"] for e in entities}
        require(set(ids) == {"p1", "p2", "p3"}, "Fixture entity identities must be discovered from native data")
        control_entities = reader.request("GET", f"/api/v1/graph/kg/{control}/entities/Person", (200,)).payload["data"]
        control_ids = {e["properties"]["id"]: e["id"] for e in control_entities}
        report["private"] = {}
        for name, r, o, a in (("gateway", reader, outsider, anonymous), ("graph", direct_reader, direct_outsider, direct_anonymous)):
            report["private"][name] = {"reader": probe(r, target, ids, 200), "outsider": probe(o, target, ids, 403, "permission"), "anonymous": probe(a, target, ids, 401)}
            o.request("PUT", f"/api/v1/graph/graphs/{target}", (403,), {"is_public": True})
            r.request("PUT", f"/api/v1/graph/graphs/{target}", (403,), {"is_public": True})
        report["browser"] = run_browser(report, target, engine_id, tenant_id, ids, native_path_edges(reader, target, ids))
        SUPPORT.wait_for_scan(preparer, engine_id, deadline)
        if targets[1]["engine_id"] != engine_id:
            SUPPORT.wait_for_scan(preparer, targets[1]["engine_id"], deadline)
        report["develop_unmanaged"] = run_develop_browser(report, engine_id, tenant_id, "unmanaged")
        set_public(preparer, target, True)
        report["public_unmanaged"] = {"gateway": probe(anonymous, target, ids, 200), "graph": probe(direct_anonymous, target, ids, 200)}
        item = SUPPORT.find_item(preparer, engine_id, targets[0]["full_name"], "graph")
        native_path = "/".join(urllib.parse.quote(name, safe="") for name in targets[0]["names"])
        locator = f"addp://engine/{engine_id}/path/{native_path}?type=graph&item_id={item['id']}"
        enrollment = preparer.request("POST", "/api/v1/security/protection-enrollments", (201,), {"locator": locator, "validity_mode": "permanent"}).payload
        enrollment_id = enrollment["id"]
        require(enrollment["target"]["resource_identity"] == item["fingerprint"], "Enrollment must match the precise Meta graph identity")
        require(enrollment["target_snapshot"]["engine_id"] == engine_id and enrollment["target_snapshot"]["full_name"] == targets[0]["full_name"], "Enrollment must retain the exact graph and Engine binding")
        installed = wait_owners(preparer, enrollment_id, deadline)
        report["owners"] = {p["consumer_owner"]: {k: p[k] for k in ("projection_id", "revision", "published_cursor", "acknowledged")} for p in installed["owner_progress"]}
        report["public_protected"] = {"gateway": probe(anonymous, target, ids, 403, "protection"), "graph": probe(direct_anonymous, target, ids, 403, "protection")}
        report["authenticated_protected"] = {"gateway": probe(reader, target, ids, 403, "protection"), "graph": probe(direct_reader, target, ids, 403, "protection")}
        report["public_outsider_protected"] = {"gateway": probe(outsider, target, ids, 403, "protection"), "graph": probe(direct_outsider, target, ids, 403, "protection")}
        report["same_engine_control" if engine_type() == "falkordb" else "separate_engine_control"] = {"gateway": probe(reader, control, control_ids, 200), "graph": probe(direct_reader, control, control_ids, 200)}
        report["develop_protected"] = run_develop_browser(report, engine_id, tenant_id, "protected")
        set_public(preparer, target, False)
        report["private_protected_anonymous"] = probe(anonymous, target, ids, 401)
        report["private_protected_reader"] = probe(reader, target, ids, 403, "protection")
        run_review(preparer, reader, engine_id, tenant_id, ontology_id, control, report)
        preparer.request("DELETE", f"/api/v1/graph/graphs/{control}", (200,))
        preparer.request("GET", f"/api/v1/graph/graphs/{control}", (404,))
        graph_ids.remove(control)
        run_dense(preparer, reader, tenant_id, targets[1], report, control)
    except Exception as error:
        operation_failed = True
        report["failure"] = {"error_type": type(error).__name__, "message": str(error)}
        raise
    finally:
        cleanup_errors = []
        for gid in graph_ids:
            try:
                set_public(preparer, gid, False)
            except Exception:
                cleanup_errors.append("close synthetic graph")
        if enrollment_id:
            try:
                current = preparer.request("GET", "/api/v1/security/protection-enrollments/" + enrollment_id, (200,)).payload
                preparer.request("POST", f"/api/v1/security/protection-enrollments/{enrollment_id}/releases", (200,), {"version": current["version"], "basis": "manual", "reason": "Dispose of dedicated synthetic Hosted Graph fixture"})
                wait_owners(preparer, enrollment_id, time.monotonic() + 120, True)
            except Exception:
                cleanup_errors.append("release five-Owner enrollment")
        for gid in graph_ids:
            try:
                preparer.request("DELETE", f"/api/v1/graph/graphs/{gid}", (200,))
                preparer.request("GET", f"/api/v1/graph/graphs/{gid}", (404,))
            except Exception:
                cleanup_errors.append("delete synthetic graph configuration")
        if ontology_id:
            try:
                preparer.request("DELETE", f"/api/v1/graph/ontologies/{ontology_id}", (200,))
                preparer.request("GET", f"/api/v1/graph/ontologies/{ontology_id}", (404,))
            except Exception:
                cleanup_errors.append("delete synthetic ontology")
        report["cleanup"] = {"errors": cleanup_errors, "domain_resources_remaining": 0 if not cleanup_errors else None,
                             "identity_and_native_data_lifecycle": "disposable Hosted Infra and graph fixture"}
        if not operation_failed:
            require(not cleanup_errors, "Synthetic Graph domain cleanup failed")


def run_review(preparer, reader, engine_id, tenant_id, ontology_id, control_id, report):
    spec = importlib.util.spec_from_file_location("graph_review_online", Path(__file__).with_name("graph-review-online.py"))
    review = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(review)
    review.run(sys.modules[__name__], preparer, reader, engine_id, tenant_id, ontology_id, control_id, report)


def main():
    report = {"schema_version": "addp.graph-protection-online/v10", "passed": False}
    output = Path(SUPPORT.required_environment("ADDP_ONLINE_ARTIFACT_DIR")) / "graph-protection.json"
    try:
        require(os.environ.get("ADDP_ONLINE_HOSTED") == "1" and os.environ.get("GITHUB_ACTIONS") == "true", "This suite only runs in disposable Hosted deployments")
        env = SUPPORT.required_environment
        gateway, graph = env("GATEWAY_URL"), env("GRAPH_URL")
        reader, outsider = env("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"), env("ADDP_ONLINE_GRAPH_OUTSIDER_ACCESS_TOKEN")
        run(Client(gateway, env("ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN"), 30),
            Client(gateway, reader, 30), Client(gateway, outsider, 30), Client(gateway, "", 30),
            Client(graph, reader, 30), Client(graph, outsider, 30), Client(graph, "", 30),
            SUPPORT.positive_int(env("ADDP_ONLINE_TEST_ENGINE_ID"), "engine id"),
            SUPPORT.positive_int(env("ADDP_ONLINE_TEST_TENANT_ID"), "tenant id"), time.monotonic() + 720, report)
        report["passed"] = True
        return 0
    except Exception as error:
        report["error_type"] = type(error).__name__
        print("Graph protection acceptance failed: " + str(error), file=sys.stderr)
        return 1
    finally:
        output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
        # The standard Online dispatcher consumes the last stdout line as JSON.
        print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == "__main__":
    raise SystemExit(main())
