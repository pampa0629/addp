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
SERVICE_READER_PERMISSIONS = {"service.definition.read"}
PREPARER_PERMISSIONS = {
    "graph.ontology.create", "graph.ontology.read", "graph.ontology.delete",
    "graph.build_task.read", "graph.build_task.create", "graph.build_task.update",
    "graph.build_task.execute", "graph.build_task.cancel", "graph.build_task.delete",
    "graph.graph.create", "graph.graph.read", "graph.graph.update", "graph.graph.delete",
    "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read",
    "security.enrollment.create", "security.enrollment.read", "security.enrollment.update",
    "service.definition.create", "service.definition.read", "service.definition.update", "service.definition.delete",
}


def verify_service_graph_result(payload):
    require(isinstance(payload, dict) and payload.get("rows_count") == 3 and
            payload.get("columns") == ["id", "name"] and payload.get("rows") == [
                {"id": "p1", "name": "Fixture one"}, {"id": "p2", "name": "Fixture two"},
                {"id": "p3", "name": "Fixture three"}], "Service graph query must return the exact native synthetic rows")


def verify_service_graph_denial(payload):
    require(isinstance(payload, dict) and set(payload) == {"error"} and
            "service data protection gate is required" in str(payload["error"]) and
            "protected resource read set is unresolved" in str(payload["error"]),
            "Service graph protection must refuse the unresolved read without output")


def verify_service_graph_foreign(report, definition, tenant_id, body, public_access):
    env = SUPPORT.required_environment
    foreign_tenant = SUPPORT.positive_int(env("ADDP_ONLINE_GRAPH_SERVICE_FOREIGN_TENANT_ID"), "foreign Service Tenant")
    require(foreign_tenant > 1 and foreign_tenant != tenant_id, "Foreign Service Tenant must be distinct and nondefault")
    token = env("ADDP_ONLINE_GRAPH_SERVICE_FOREIGN_ACCESS_TOKEN")
    foreign = Client(env("GATEWAY_URL"), token, 30)
    user = identity(foreign, foreign_tenant, SERVICE_READER_PERMISSIONS)
    user["tenant_id"] = str(foreign_tenant)
    others = {entry["principal_id"] for name, entry in report["identities"].items() if name != "service_foreign"}
    require(user["principal_id"] not in others, "Foreign Service reader must be a separate ordinary User")
    require(report["identities"].get("service_foreign", user) == user, "Foreign Service identity changed between checks")
    report["identities"]["service_foreign"] = user
    path = "/api/gquery/" + definition["service_name"]
    checks = []
    for entry, origin in (("dto_gateway", env("GATEWAY_URL")), ("console_http", env("CONSOLE_URL"))):
        client = Client(origin, token, 30)
        management = client.request("GET", f"/api/v1/service/graph/{definition['id']}", (404,))
        require(management.status == 404 and management.payload == {"error": "Service not found"},
                "Foreign Service detail must be absent without exposing its DTO")
        execution = client.request("POST", path, (404,), body)
        require(execution.status == 404 and execution.payload == {"error": f"service '{definition['service_name']}' not found"},
                "Foreign Service execution must return only the Tenant-scoped not-found error")
        checks.append({"entry": entry, "public_access": public_access, "management_status": 404,
                       "execution_status": 404, "result_absent": True})
    return checks


def run_service_graph_browser(report, definition, tenant_id, phase):
    env = SUPPORT.required_environment
    reader = Client(env("GATEWAY_URL"), env("ADDP_ONLINE_GRAPH_SERVICE_ACCESS_TOKEN"), 30)
    user = identity(reader, tenant_id, SERVICE_READER_PERMISSIONS)
    others = {entry["principal_id"] for name, entry in report["identities"].items() if name != "service"}
    require(user["principal_id"] not in others, "Service browser must use its own minimum-permission User")
    require(report["identities"].get("service", user) == user, "Service User changed between phases")
    report["identities"]["service"] = user
    artifacts = Path(env("ADDP_ONLINE_ARTIFACT_DIR"))
    output = artifacts / f"service-graph-{phase}-browser.json"
    screenshot = artifacts / f"service-graph-{phase}-console.png"
    for path in (output, screenshot):
        path.unlink(missing_ok=True)
    expected = {"run_id": env("ADDP_ONLINE_TEST_RUN_ID"), "tenant_id": str(tenant_id),
                "principal_id": user["principal_id"], "service_id": definition["id"],
                "service_name": definition["service_name"], "dto_endpoint": definition["endpoints"]["execute"],
                "phase": phase, "engine_type": "neo4j"}
    environment = {key: value for key, value in os.environ.items() if not key.endswith("_ACCESS_TOKEN")}
    environment.update(ADDP_ONLINE_SERVICE_GRAPH_EXPECTATIONS=json.dumps(expected),
                       ADDP_ONLINE_SERVICE_GRAPH_REPORT=str(output))
    result = subprocess.run(["npm", "exec", "--", "playwright", "test", "e2e/online/graph-protection.spec.js",
                             "--config=playwright.online.config.js", "--grep", "ordinary Service graph reader"],
                            cwd=Path(__file__).resolve().parents[2] / "console/frontend", env=environment, timeout=180)
    require(result.returncode == 0 and output.is_file() and screenshot.is_file(), "Service graph Console evidence missing or failed")
    evidence = json.loads(output.read_text())
    expected.update(schema_version="addp.service-graph-browser/v1", result="passed", permissions=sorted(SERVICE_READER_PERMISSIONS),
                    page_origin=env("CONSOLE_URL"), page_path="/api/gquery/" + definition["service_name"],
                    dto_address_unchanged=True, user_bearer=True, exact_request=True, result_visible=phase == "unmanaged",
                    protection_refused=phase == "protected", browser_errors=0, unexpected_writes=0, release_assets=True,
                    public_https_verified=False)
    require(type(evidence) is dict and set(evidence) == set(expected) and
            all(type(evidence[key]) is type(value) and evidence[key] == value for key, value in expected.items()),
            "Service browser evidence differs from the real identity, addresses, result or refusal")
    return evidence


def run_service_graph_phase(preparer, anonymous, report, definition, tenant_id, phase):
    env = SUPPORT.required_environment
    reader = Client(env("GATEWAY_URL"), env("ADDP_ONLINE_GRAPH_SERVICE_ACCESS_TOKEN"), 30)
    dto = reader.request("GET", f"/api/v1/service/graph/{definition['id']}", (200,)).payload
    path = "/api/gquery/" + definition["service_name"]
    endpoint = dto["endpoints"]["execute"]
    require(endpoint == env("GATEWAY_URL").rstrip("/") + path, "DTO must preserve the configured Gateway address and canonical path")
    require(dto["id"] == definition["id"] and dto["service_name"] == definition["service_name"], "Service definition identity changed")
    require(dto.get("public_access") is False, "Service phase must begin with the saved private access state")
    body = {"parameters": {}, "page": 1, "page_size": 20}
    expected_status = 200 if phase == "unmanaged" else 500
    check = verify_service_graph_result if phase == "unmanaged" else verify_service_graph_denial
    check(reader.request("POST", path, (expected_status,), body).payload)
    foreign_checks = verify_service_graph_foreign(report, dto, tenant_id, body, False)
    if phase == "unmanaged":
        anonymous.request("POST", path, (401,), body)
        Client(env("GATEWAY_URL"), "invalid-service-graph-token", 30).request("POST", path, (401,), body)
        for alias in ("/api/v1/gquery/", "/api/v1/service/gquery/"):
            Client(env("SERVICE_URL"), reader.token, 30).request("POST", alias + dto["service_name"], (404,), body, raw_response=True)
        preparer.request("PUT", f"/api/v1/service/graph/{dto['id']}", (200,), {"status": "inactive"})
        reader.request("POST", path, (400,), body)
        preparer.request("PUT", f"/api/v1/service/graph/{dto['id']}", (200,), {"status": "active"})
    enabled = preparer.request("PUT", f"/api/v1/service/graph/{dto['id']}", (200,), {"public_access": True}).payload
    require(enabled.get("public_access") is True, "Service public access state was not saved")
    check(anonymous.request("POST", path, (expected_status,), body).payload)
    foreign_checks.extend(verify_service_graph_foreign(report, dto, tenant_id, body, True))
    require(len(foreign_checks) == 4 and {(item["entry"], item["public_access"]) for item in foreign_checks} ==
            {(entry, public) for entry in ("dto_gateway", "console_http") for public in (False, True)},
            "Foreign Service checks must cover both HTTP entries in private and public states")
    disabled = preparer.request("PUT", f"/api/v1/service/graph/{dto['id']}", (200,), {"public_access": False}).payload
    require(disabled.get("public_access") is False, "Service private access state was not restored")
    browser = run_service_graph_browser(report, dto, tenant_id, phase)
    return {"http_status": expected_status, "dto_endpoint": endpoint, "private_user": True, "public_anonymous": True,
            "native_rows_verified": phase == "unmanaged", "protection_refused_without_output": phase == "protected", "browser": browser,
            "cross_tenant_runtime_verified": True, "foreign_checks": foreign_checks, "public_https_verified": False}


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
    return fixture.DENSE_NODES, fixture.DENSE_EDGES, fixture.dense_relationship_properties


def verify_dense_reference(nodes, edges):
    labels, relations, properties = dense_fixture_contract()
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
    by_fixture = {properties(index, source, target)["fixture_id"]: (source, target, kind, properties(index, source, target)) for index, (source, target, kind) in enumerate(relations)}
    seen = set()
    for edge in edges:
        require(set(edge) == {"id", "source", "target", "type", "properties"} and type(edge["id"]) is str and bool(edge["id"]) and
                edge["source"] in reverse and edge["target"] in reverse, "Dense native edge endpoints differ")
        require(type(edge["properties"]) is dict, "Dense native relation properties missing")
        key = edge["properties"].get("fixture_id")
        wanted = by_fixture.get(key)
        require(wanted is not None and key not in seen and edge["properties"] == wanted[3] and
                (reverse[edge["source"]], reverse[edge["target"]], edge["type"]) == wanted[:3], "Dense native relation properties/identity differ")
        require(type(edge["properties"]["active"]) is bool and type(edge["properties"]["ordinal"]) is int and type(edge["properties"]["score"]) in (int, float), "Dense native relation property types differ")
        seen.add(key)
        actual.append((reverse[edge["source"]], reverse[edge["target"]], edge["type"]))
    require(sorted(actual) == sorted(relations), "Dense native relation direction/type/multiplicity differs")
    return ids


def verify_relationship_pixels(relation_pixels, edges):
    require(type(relation_pixels) is list and [item.get("type") for item in relation_pixels] == sorted({edge["type"] for edge in edges}), "Dense visible relationship types missing")
    for sample in relation_pixels:
        require(set(sample) == {"type", "color", "pixelCount", "stableFrames"} and type(sample["pixelCount"]) is int and sample["pixelCount"] >= 40 and
                type(sample["stableFrames"]) is int and sample["stableFrames"] >= 10 and type(sample["color"]) is list and len(sample["color"]) == 3 and
                all(type(value) is int and 0 <= value <= 255 for value in sample["color"]), "Dense relation strokes not visibly stable")
    require(len({tuple(sample["color"]) for sample in relation_pixels}) == len(relation_pixels), "Dense relation colors collide")


def verify_dense_frame(stage, members, edges, shapes, search=False):
    require(stage["nodes"] == members and stage["edges"] == edges, "Dense frame native membership differs")
    verify_relationship_pixels(stage["relationship_pixels"], edges)
    groups = stage["groups"]
    wanted_shapes = [shape for shape in shapes if any(sorted(node["labels"]) == sorted(shape["labels"]) for node in members)]
    require(type(groups) is list and len(groups) == len(wanted_shapes), "Dense color group missing")
    positions = []
    require(len({(group["frame"]["width"], group["frame"]["height"], group["frame"]["stableFrames"]) for group in groups}) == 1, "Dense groups did not settle simultaneously")
    for group, shape in zip(groups, wanted_shapes):
        require(set(group) == {"name", "labels", "frame", "hits"} and group["name"] == shape["name"] and group["labels"] == sorted(shape["labels"]), "Dense color shape differs")
        group_ids = {node["properties"]["id"]: node["id"] for node in members if sorted(node["labels"]) == sorted(shape["labels"])}
        verify_pixel_frame(group["frame"], group["hits"], group_ids)
        require((group["frame"]["color"] != group["frame"]["fillColor"]) if search else (group["frame"]["color"] == group["frame"]["fillColor"]), "Dense search/ordinary color evidence differs")
        positions.extend((hit["native_id"], hit["x"], hit["y"]) for hit in group["hits"])
    require(len({tuple(group["frame"]["fillColor"]) for group in groups}) == len(groups), "Dense label colors collide")
    require(len({(x, y) for _, x, y in positions}) == len(members), "Dense color groups duplicate visible nodes")
    return positions


def verify_dense(visual, expected):
    require(type(visual) is dict and set(visual) == set(expected) | {"schema_version", "stages", "requests", "relationships", "relation_filters", "node_filters"} and
            all(visual.get(key) == value for key, value in expected.items()) and
            visual["schema_version"] == "addp.graph-dense-stable-frame/v5", "Dense evidence identity differs")
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
        positions = verify_dense_frame(stage, members, edges, shapes, index == 0)
        if index == 0:
            require(stage["http_status"] == 200 and stage["request"] == {"query": "Dense", "limit": 30}, "Dense search request differs")
        if index == 2:
            require(stage["http_status"] == 200 and stage["request"] == {"target": {"kind": "entity", "id": ids["n01"]}, "depth": 2, "node_limit": 200, "relationship_limit": 400}, "Dense expansion request differs")
        if index in (3, 4):
            wanted = [group["name"] for group in stage["groups"]]
            require(stage["selected_shapes"] == sorted(wanted), "Dense filter selection differs")
        if index >= 5:
            layout, label = [("dagre", "分层"), ("circular", "环形"), ("radial", "辐射"), ("force", "力导向")][index-5]
            require(stage["layout"] == layout and stage["selected_label"] == label and sorted(positions) != previous_positions, "Dense layout did not visibly change")
        previous_positions = sorted(positions)
    verify_relationships(visual["relationships"], expected, stages[-4:])
    verify_dense_filters(visual["relation_filters"], expected, stages[4])
    verify_node_filters(visual["node_filters"], expected, visual["relation_filters"]["stages"][3])
    root = f"/api/v1/graph/graphs/{expected['graph_id']}"
    require(visual["requests"] == [{"method": "POST", "path": root + "/search", "body": stages[0]["request"]},
        {"method": "POST", "path": root + "/expand", "body": stages[2]["request"]}], "Dense filter/layout made unexpected requests")


def verify_relationships(layouts, expected, stages):
    require(type(layouts) is list and [layout.get("layout") for layout in layouts] == ["dagre", "circular", "radial", "force"], "Relationship layouts incomplete")
    for layout, stage in zip(layouts, stages):
        verify_relationship_frame(layout, expected["edges"], stage)


def verify_relationship_frame(layout, native_edges, stage):
    edges = {edge["id"]: edge for edge in native_edges}
    number = lambda value: type(value) in (int, float) and math.isfinite(value)
    display = lambda value: str(value).lower() if type(value) is bool else format(value, 'g') if type(value) in (int, float) else str(value)
    require(set(layout) == {"layout", "width", "height", "stable_frames", "node_geometry", "attempts", "hits"} and type(layout["stable_frames"]) is int and layout["stable_frames"] >= 10 and
            type(layout["attempts"]) is int and layout["attempts"] >= len(edges) and all(number(layout[key]) and layout[key] > 0 for key in ("width", "height")), "Relationship frame is invalid")
    centers = {hit["native_id"]: (hit["x"], hit["y"], pixel["box"]) for group in stage["groups"] for pixel, hit in zip(group["frame"]["nodes"], group["hits"])}
    require(all((group["frame"]["width"], group["frame"]["height"]) == (layout["width"], layout["height"]) for group in stage["groups"]), "Relationship viewport differs")
    colors = {hit["native_id"]: group["frame"]["fillColor"] for group in stage["groups"] for hit in group["hits"]}
    exposed = {h["native_id"]: pixel["geometry"] for group in stage["groups"] if group["frame"].get("schema_version") == "addp.graph-exposed-pixels/v1" for h,pixel in zip(group["hits"],group["frame"]["nodes"])}
    geometry = layout["node_geometry"]
    require(type(geometry) is list and len(geometry) == len(centers) and {node.get("native_id") for node in geometry} == set(centers), "Painted node geometry identity missing/duplicated")
    for node in geometry:
        require(set(node) == {"native_id", "x", "y", "rx", "ry", "color", "alpha"} and
                all(number(node[key]) for key in ("x", "y", "rx", "ry", "alpha")) and node["alpha"] == 1 and
                node["color"] == colors[node["native_id"]] and all(type(v) is int for v in node["color"]), "Painted node geometry types/color differ")
        x, y, box = centers[node["native_id"]]
        extent = max(box[2]-box[0],box[3]-box[1])/2
        require(node["rx"] > 0 and node["ry"] > 0 and math.hypot((node["x"]-x)/node["rx"],(node["y"]-y)/node["ry"]) < 1 and abs(node["rx"]-node["ry"]) < .1 and
                (all(node[k] == exposed[node["native_id"]][k] for k in ("x","y","rx","ry","color","alpha")) if node["native_id"] in exposed else extent-1 <= node["rx"] <= extent+4) and
                1 < node["x"]-node["rx"] <= box[0]+1 and box[2]-1 <= node["x"]+node["rx"] < layout["width"]-1 and
                1 < node["y"]-node["ry"] <= box[1]+1 and box[3]-1 <= node["y"]+node["ry"] < layout["height"]-1,
                "Painted circle differs from visible pixel core or viewport")
    painted = {node["native_id"]:node for node in geometry}
    hits = layout["hits"]
    require(type(hits) is list and len(hits) == len(edges) and {hit.get("native_id") for hit in hits} == set(edges), "Relationship mouse identities incomplete/duplicated")
    for hit in hits:
        require(set(hit) == {"fixture_id", "native_id", "source", "target", "type", "properties", "path", "path_source", "path_target", "x", "y", "t", "pixel"}, "Relationship hit fields differ")
        edge = edges[hit["native_id"]]
        require(all(hit[key] == edge[key] for key in ("source", "target", "type")) and hit["fixture_id"] == edge["properties"]["fixture_id"] and
                hit["properties"] == {key: display(value) for key, value in edge["properties"].items()}, "Relationship DOM type/properties/native binding differs")
        require({hit["path_source"], hit["path_target"]} == {edge["source"], edge["target"]}, "Relationship painted endpoints differ")
        require(all(number(hit[key]) for key in ("x", "y", "t")) and 0 < hit["t"] < 1 and 1 < hit["x"] < layout["width"]-1 and 1 < hit["y"] < layout["height"]-1, "Relationship click outside viewport")
        require(not any(box[0]-2 <= hit["x"] <= box[2]+2 and box[1]-2 <= hit["y"] <= box[3]+2 for _, _, box in centers.values()), "Relationship click overlaps node")
        pixel = hit["pixel"]
        require(type(pixel) is list and len(pixel) == 4 and all(type(channel) is int and 0 <= channel <= 255 for channel in pixel) and pixel[3] > 0 and
                all(abs(pixel[channel] - next(sample["color"] for sample in stage["relationship_pixels"] if sample["type"] == edge["type"])[channel]) <= 16 for channel in range(3)), "Relationship click has no matching relation pixel")
        path = hit["path"]
        require(type(path) is list and len(path) == 2 and path[0][0] == "M" and len(path[0]) == 3 and path[1][0] in ("L", "Q", "C") and
                len(path[1]) == {"L": 3, "Q": 5, "C": 7}[path[1][0]] and all(number(value) for segment in path for value in segment[1:]), "Relationship painted path malformed")
        head, tail = path[0][1:], path[1][-2:]
        first, last = path[1][1:3], head if len(path[1]) == 3 else path[1][-4:-2]
        for endpoint, tangent, leaving, identifier in ((head,[first[i]-head[i] for i in (0,1)],True,hit['path_source']),(tail,[tail[i]-last[i] for i in (0,1)],False,hit['path_target'])):
            candidates=[]
            for key,node in painted.items():
                boundary=abs(math.hypot((endpoint[0]-node['x'])/node['rx'],(endpoint[1]-node['y'])/node['ry'])-1)*min(node['rx'],node['ry'])
                direction=((endpoint[0]-node['x'])*tangent[0]+(endpoint[1]-node['y'])*tangent[1])*(1 if leaving else -1)
                if boundary <= 2 and direction > 0: candidates.append(key)
            require(candidates == [identifier], 'Relationship painted/native endpoint identity or outward/inward direction differs')
        if exposed:
            require(all((math.hypot((hit['x']-n['x'])/n['rx'],(hit['y']-n['y'])/n['ry'])-1)*min(n['rx'],n['ry']) > 2 for n in painted.values()), 'Exposed relationship click overlaps an actual circle')
        t, q = hit["t"], 1-hit["t"]
        start, segment = path[0][1:], path[1]
        if segment[0] == "L": point = [q*start[i]+t*segment[i+1] for i in (0, 1)]
        elif segment[0] == "Q": point = [q*q*start[i]+2*q*t*segment[i+1]+t*t*segment[i+3] for i in (0, 1)]
        else: point = [q*q*q*start[i]+3*q*q*t*segment[i+1]+3*q*t*t*segment[i+3]+t*t*t*segment[i+5] for i in (0, 1)]
        require(math.hypot(point[0]-hit["x"], point[1]-hit["y"]) < .05, "Relationship click is not on painted curve")


def verify_dense_filters(filters, expected, baseline_stage):
    require(type(filters) is dict and set(filters) == {"baseline", "stages"}, "Relationship filter evidence missing")
    baseline = filters["baseline"]
    require(type(baseline) is dict and baseline.get("layout") == "force", "Filter baseline layout differs")
    verify_relationship_frame(baseline, expected["edges"], baseline_stage)
    stages = filters["stages"]
    require(type(stages) is list and len(stages) == 4, "Relationship filter roundtrip incomplete")
    palette = {sample["type"]: sample["color"] for sample in baseline_stage["relationship_pixels"]}
    modes = [["Parallel"], [], ["Link"], ["Link", "Parallel"]]
    for index, (stage, selected) in enumerate(zip(stages, modes)):
        require(type(stage) is dict and set(stage) == {"phase", "layout", "selected_types", "nodes", "edges", "groups", "relationship_pixels", "absent_pixels", "relationships", "probes"} and
                stage["phase"] == "relation-filter-" + str(index) and stage["layout"] == "force" and stage["selected_types"] == selected, "Relationship filter selection differs")
        edges = [edge for edge in expected["edges"] if edge["type"] in selected]
        verify_dense_frame(stage, expected["nodes"], edges, expected["shapes"])
        absent = stage["absent_pixels"]
        require(type(absent) is list and [sample.get("type") for sample in absent] == sorted(set(palette)-set(selected)), "Filtered relationship absence missing")
        for sample in absent:
            require(set(sample) == {"type", "color", "pixelCount", "stableFrames"} and sample["color"] == palette[sample["type"]] and
                    type(sample["pixelCount"]) is int and sample["pixelCount"] == 0 and type(sample["stableFrames"]) is int and sample["stableFrames"] >= 10, "Filtered relationship stroke remains visible")
        if index in (0, 2):
            require(type(stage["relationships"]) is dict and stage["relationships"].get("layout") == "force", "Filtered mouse report missing")
            verify_relationship_frame(stage["relationships"], edges, stage)
        else: require(stage["relationships"] is None, "Unexpected filtered mouse report")
        if index == 1:
            wanted = [{key: hit[key] for key in ("native_id", "x", "y")} | {"relation_selected": False} for hit in baseline["hits"]]
            require(stage["probes"] == wanted and all(probe["relation_selected"] is False for probe in stage["probes"]), "Hidden relationships remain clickable or probes missing")
        else: require(stage["probes"] == [], "Unexpected hidden relationship probes")



def verify_node_filters(filters, expected, before):
    require(type(filters) is dict and set(filters) == {"baseline", "empty", "restored"}, "Node filter evidence missing")
    baseline = filters["baseline"]
    require(type(baseline) is dict and baseline.get("layout") == "force", "Node filter baseline differs")
    verify_relationship_frame(baseline, expected["edges"], before)
    empty = filters["empty"]
    require(type(empty) is dict and set(empty) == {"phase", "selected_shapes", "selected_types", "width", "height", "nodes", "edges", "canvas_visible", "hint_visible", "loading_absent", "absent_pixels", "node_probes", "relationship_probes"} and
            empty["phase"] == "node-filter-empty" and empty["selected_shapes"] == [] and empty["selected_types"] == before["selected_types"] and
            empty["nodes"] == [] and empty["edges"] == [] and all(empty[key] is True for key in ("canvas_visible", "hint_visible", "loading_absent")), "Node empty selection/state differs")
    require(all(type(empty[key]) in (int,float) and math.isfinite(empty[key]) and empty[key] > 0 and empty[key] == baseline[key] for key in ("width", "height")), "Node empty viewport differs")
    palette = [{"type": "node:"+group["name"], "color": group["frame"]["fillColor"]} for group in before["groups"]] + [
        {"type": "relationship:"+sample["type"], "color": sample["color"]} for sample in before["relationship_pixels"]]
    samples = empty["absent_pixels"]
    require(type(samples) is list and len(samples) == len(palette), "Node empty color evidence missing")
    for sample,wanted in zip(samples,palette):
        require(type(sample) is dict and set(sample) == {"type", "color", "pixelCount", "stableFrames"} and all(sample[key] == value for key,value in wanted.items()) and
                type(sample["pixelCount"]) is int and sample["pixelCount"] == 0 and type(sample["stableFrames"]) is int and sample["stableFrames"] >= 10, "Hidden node/relationship pixels remain")
    require(len({sample["stableFrames"] for sample in samples}) == 1, "Empty colors did not settle together")
    for key,points in (("node_probes",[hit for group in before["groups"] for hit in group["hits"]]),("relationship_probes",baseline["hits"])):
        probes = empty[key]
        wanted = [{field:hit[field] for field in ("native_id", "x", "y")} | {"selection_absent":True} for hit in points]
        require(type(probes) is list and probes == wanted and all(probe["selection_absent"] is True for probe in probes), "Hidden node/relationship selection or missing probes")
    restored = filters["restored"]
    require(type(restored) is dict and set(restored) == {"phase", "layout", "selected_shapes", "nodes", "edges", "groups", "relationship_pixels"} and
            restored["phase"] == "node-filter-restored" and restored["layout"] == "force" and restored["selected_shapes"] == sorted(shape["name"] for shape in expected["shapes"]), "Node restored selection differs")
    verify_dense_frame(restored, expected["nodes"], expected["edges"], expected["shapes"])
    require(all(group["frame"][key] == empty[key] for group in restored["groups"] for key in ("width", "height")), "Node restored viewport differs")


def run_dense(preparer, reader, tenant_id, target_path, report, previous_control_graph_id, *, case="full"):
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
        edges = sorted([{key: edge[key] for key in ("id", "source", "target", "type", "properties")} for edge in native["edges"]], key=lambda edge: edge["id"])
        verify_dense_reference(nodes, edges)
        snapshot = reader.request("GET", f"/api/v1/graph/graphs/{graph_id}/browse-snapshot", (200,)).payload
        require(snapshot["stats"]["node_count"] == 12 and snapshot["stats"]["relationship_count"] == 72 and snapshot["stats"].get("by_label") == {"Person": 6, "Employee": 3, "Organization": 6, "Partner": 3}, "Dense stats incomplete or overlapping labels double-counted")
        require(len(snapshot["schema"]["node_shapes"]) == 4 and all(type(shape.get("count")) is int and shape["count"] == 3 for shape in snapshot["schema"]["node_shapes"]), "Dense full-label-set counts differ")
        shapes = sorted([{"name": shape["name"], "labels": sorted(shape["labels"])} for shape in snapshot["schema"]["node_shapes"]], key=lambda shape: shape["name"])
        expected = {"run_id": SUPPORT.required_environment("ADDP_ONLINE_TEST_RUN_ID"), "tenant_id": str(tenant_id), "principal_id": context["principal_id"],
            "engine_type": engine_type(), "engine_id": target_path["engine_id"], "graph_id": graph_id, "nodes": nodes, "edges": edges, "shapes": shapes}
        if case in {"endpoint-geometry", "endpoint-same-color"}:
            if case == "endpoint-same-color": expected["geometry_case"] = "same-color"
            report["same_color_geometry" if case == "endpoint-same-color" else "endpoint_geometry"] = run_endpoint_browser(expected)
            return
        require(case == "full", "Unknown Graph dense acceptance case")
        artifact = Path(SUPPORT.required_environment("ADDP_ONLINE_ARTIFACT_DIR"))
        raw = artifact / "graph-dense-stable-frame.json"
        raw.unlink(missing_ok=True)
        (artifact / "graph-relationship-hit-frame.json").unlink(missing_ok=True)
        (artifact / "graph-relationship-filter-frame.json").unlink(missing_ok=True)
        (artifact / "graph-node-filter-frame.json").unlink(missing_ok=True)
        for pattern in ("relationship-*-console.png", "relation-filter-*-console.png", "node-filter-*-console.png"):
            for path in artifact.glob(pattern): path.unlink()
        diagnostics = artifact / "graph-dense-browser-diagnostics.json"
        diagnostics.unlink(missing_ok=True)
        for path in artifact.glob("dense-*-console.png"): path.unlink()
        env = {key: value for key, value in os.environ.items() if not key.endswith(("_ACCESS_TOKEN", "_USERNAME", "_PASSWORD"))}
        for key in ("ADDP_ONLINE_TEST_USER_USERNAME", "ADDP_ONLINE_TEST_USER_PASSWORD"):
            env[key] = SUPPORT.required_environment(key)
        env["ADDP_ONLINE_GRAPH_DENSE_EXPECTATIONS"] = json.dumps(expected)
        result = subprocess.run(["npm", "exec", "--", "playwright", "test", "e2e/online/graph-protection.spec.js", "--config=playwright.online.config.js", "--grep", "multi-label dense", "--reporter=line"], cwd=Path(__file__).resolve().parents[2] / "console/frontend", env=env, timeout=600)
        require(result.returncode == 0 and raw.is_file(), "Dense Console evidence missing or failed")
        require(diagnostics.is_file() and json.loads(diagnostics.read_text()) == {"schema_version": "addp.graph-browser-diagnostics/v1", "entries": []}, "Dense browser diagnostics missing or dirty")
        visual = json.loads(raw.read_text())
        verify_dense(visual, expected)
        interaction_file = artifact / "graph-relationship-hit-frame.json"
        require(interaction_file.is_file(), "Relationship mouse/property evidence missing")
        require(json.loads(interaction_file.read_text()) == {**expected, "schema_version": "addp.graph-relationship-hit-frame/v2", "layouts": visual["relationships"]}, "Independent relationship evidence differs")
        filter_file = artifact / "graph-relationship-filter-frame.json"
        require(filter_file.is_file(), "Independent relationship filter evidence missing")
        require(json.loads(filter_file.read_text()) == {**expected, "schema_version": "addp.graph-relationship-filter-frame/v1", **visual["relation_filters"]}, "Independent relationship filter evidence differs")
        for stage in visual["relation_filters"]["stages"]:
            require((artifact / (stage["phase"] + "-console.png")).is_file(), "Relationship filter screenshot missing")
        node_filter_file = artifact / "graph-node-filter-frame.json"
        require(node_filter_file.is_file(), "Independent node filter evidence missing")
        require(json.loads(node_filter_file.read_text()) == {**expected, "schema_version": "addp.graph-node-filter-frame/v1", **visual["node_filters"]}, "Independent node filter evidence differs")
        for phase in ("node-filter-empty", "node-filter-restored"):
            require((artifact / (phase + "-console.png")).is_file(), "Node filter screenshot missing")
        for layout in ("dagre", "circular", "radial", "force"):
            require((artifact / ("relationship-" + layout + "-console.png")).is_file(), "Relationship screenshot missing")
        for stage in visual["stages"]:
            require((artifact / (stage["phase"] + "-console.png")).is_file(), "Dense screenshot missing")
        report["dense"] = {"schema_version": "addp.graph-dense-online/v5", "passed": True, **expected, "stages": [stage["phase"] for stage in visual["stages"]], "relationships": visual["relationships"], "relation_filters": visual["relation_filters"], "node_filters": visual["node_filters"]}
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
    service_ids = []
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
        if engine_type() == "neo4j":
            definition = preparer.request("POST", "/api/v1/service/graph", (201,), {
                "service_name": "service_graph_" + SUPPORT.required_environment("ADDP_ONLINE_TEST_RUN_ID").replace("-", "_").replace(".", "_"), "title": "Disposable Service graph acceptance",
                "engine_id": engine_id, "database_name": "neo4j", "config_type": "cypher",
                "cypher_query": "MATCH (n:Person) RETURN n.id AS id, n.name AS name ORDER BY id",
                "data_config": {"result_type": "table"}, "max_records": 20, "public_access": False,
            }).payload
            service_ids.append(SUPPORT.positive_int(definition.get("id"), "Service graph id"))
            report["service_graph_unmanaged"] = run_service_graph_phase(preparer, anonymous, report, definition, tenant_id, "unmanaged")
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
        if engine_type() == "neo4j":
            report["service_graph_protected"] = run_service_graph_phase(preparer, anonymous, report, definition, tenant_id, "protected")
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
        for service_id in service_ids:
            try:
                preparer.request("DELETE", f"/api/v1/service/graph/{service_id}", (200,))
                preparer.request("GET", f"/api/v1/service/graph/{service_id}", (404,))
            except Exception:
                cleanup_errors.append("delete synthetic Service graph definition")
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


def graph_case(environment):
    value = environment.get("ADDP_ONLINE_GRAPH_CASE", "full")
    require(type(value) is str and value in {"full", "endpoint-geometry", "endpoint-same-color"}, "Unknown Graph acceptance case")
    return value


def verify_endpoint_geometry(visual, expected):
    require(type(visual) is dict and set(visual) == set(expected) | {"schema_version", "parking", "drags", "before_drag", "baseline", "phases", "requests", "target_edge_id"} and
            visual["schema_version"] == "addp.graph-endpoint-geometry/v1" and all(type(visual.get(k)) is type(v) and visual[k] == v for k, v in expected.items()), "Endpoint geometry evidence identity differs")
    ids = verify_dense_reference(expected["nodes"], expected["edges"])
    source, target, nearby = ids["n01"], ids["n12"], ids["n04"]
    edge = next(e for e in expected["edges"] if e["source"] == source and e["target"] == target and e["type"] == "Parallel")
    require(visual["target_edge_id"] == edge["id"], "Endpoint target native relationship differs")
    edges = [e for e in expected["edges"] if e["type"] == "Parallel"]
    before, baseline, phases = visual["before_drag"], visual["baseline"], visual["phases"]
    require(set(before) == {"nodes", "edges", "groups", "relationship_pixels"}, "Endpoint initial frame fields differ")
    verify_dense_frame(before, expected["nodes"], edges, expected["shapes"])
    require(set(baseline) == set(before) | {"phase", "layout", "relationships"} and baseline["phase"] == "endpoint-baseline" and baseline["layout"] == "force", "Endpoint baseline differs")
    verify_dense_frame(baseline, expected["nodes"], edges, expected["shapes"])
    verify_relationship_frame(baseline["relationships"], [edge], baseline)
    require(type(phases) is list and len(phases) == 2, "Endpoint source/target cases missing")
    parking = [{"native_id": ids[key], "x": 70 + index % 6 * 115, "y": 90 if index < 6 else 680} for index, key in enumerate(sorted(ids))]
    require(visual["parking"] == parking, "Endpoint fixed parking geometry differs")
    positions = {h["native_id"]: {"x": h["x"], "y": h["y"]} for g in before["groups"] for h in g["hits"]}
    planned = [{"native_id": p["native_id"], "to": {"x": p["x"], "y": p["y"]}} for p in parking] + [
        {"native_id": source, "to": {"x": 200, "y": 330}}, {"native_id": target, "to": {"x": 550, "y": 330}}]
    initial_hit = baseline["relationships"]["hits"][0]
    paint = {n["native_id"]: n for n in baseline["relationships"]["node_geometry"]}
    def endpoint(hit, owner):
        return hit["path"][0][1:] if hit["path_source"] == owner else hit["path"][1][-2:]
    for phase, side, owner in zip(phases, ("source", "target"), (source, target)):
        require(set(phase) == set(baseline) | {"side", "owner", "nearby", "planned_center", "old_nearest", "old_pixel_boundary"} and
                phase["phase"] == "endpoint-near-" + side and phase["layout"] == "force" and phase["side"] == side and phase["owner"] == owner and phase["nearby"] == nearby,
                "Endpoint nearby case identity differs")
        verify_dense_frame(phase, expected["nodes"], edges, expected["shapes"])
        verify_relationship_frame(phase["relationships"], [edge], phase)
        anchor = endpoint(initial_hit, owner)
        destination = {"x": anchor[0] + (anchor[0]-paint[owner]["x"])*.8, "y": anchor[1] + (anchor[1]-paint[owner]["y"])*.8}
        require(phase["planned_center"] == destination, "Endpoint nearby position differs from fixed geometry")
        planned.append({"native_id": nearby, "to": destination})
        hit = phase["relationships"]["hits"][0]
        require(hit["path"] == initial_hit["path"], "Unrelated node drag changed the target curve")
        anchor = endpoint(hit, owner)
        pixels = {h["native_id"]: (h["x"], h["y"], p["box"]) for g in phase["groups"] for h, p in zip(g["hits"], g["frame"]["nodes"])}
        center_nearest = min(pixels, key=lambda k: math.hypot(pixels[k][0]-anchor[0], pixels[k][1]-anchor[1]))
        def pixel_boundary(k):
            x, y, box = pixels[k]
            rx, ry = (box[2]-box[0])/2, (box[3]-box[1])/2
            return abs(math.hypot((anchor[0]-x)/rx, (anchor[1]-y)/ry)-1)*min(rx, ry)
        require(phase["old_nearest"] == center_nearest == nearby and phase["old_pixel_boundary"] == min(pixels, key=pixel_boundary) == nearby,
                "Fixed geometry does not expose both previous endpoint assignment errors")
    drags = visual["drags"]
    require(type(drags) is list and len(drags) == 16, "Endpoint real mouse drag sequence incomplete")
    for index, (drag, wanted) in enumerate(zip(drags, planned)):
        require(set(drag) == {"native_id", "from", "to"} and drag["native_id"] == wanted["native_id"] and drag["to"] == wanted["to"] and drag["from"] == positions[drag["native_id"]], "Endpoint drag coordinates/identity differ")
        positions[drag["native_id"]] = drag["to"]
        if index >= 13:
            stage = baseline if index == 13 else phases[index-14]
            circles = {n["native_id"]: n for n in stage["relationships"]["node_geometry"]}
            require(all(math.hypot(circles[k]["x"]-v["x"], circles[k]["y"]-v["y"]) < 2 for k,v in positions.items()), "Real dragged circles do not match fixed target positions")
    root = f"/api/v1/graph/graphs/{expected['graph_id']}"
    require(visual["requests"] == [{"method": "POST", "path": root + "/search", "body": {"query": "Dense", "limit": 30}},
        {"method": "POST", "path": root + "/expand", "body": {"target": {"kind": "entity", "id": source}, "depth": 2, "node_limit": 200, "relationship_limit": 400}}],
        "Endpoint geometry issued unexpected API requests")


def verify_exposed_frame(stage, expected, edges):
    require(stage['nodes'] == expected['nodes'] and stage['edges'] == edges, 'Exposed native membership differs')
    verify_relationship_pixels(stage['relationship_pixels'], edges)
    number = lambda value: type(value) in (int, float) and math.isfinite(value)
    groups = stage['groups']; shapes = expected['shapes']
    require(type(groups) is list and len(groups) == len(shapes), 'Exposed label groups incomplete')
    native = {node['properties']['id']: node for node in expected['nodes']}
    observed = {}; snapshots = set(); colors = set()
    for group, shape in zip(groups, shapes):
        require(set(group) == {'name','labels','frame','hits'} and group['name'] == shape['name'] and group['labels'] == shape['labels'], 'Exposed label identity differs')
        frame = group['frame']; hits = group['hits']
        require(type(frame) is dict and set(frame) == {'schema_version','width','height','color','fillColor','margin','nodes','stableFrames'} and
                frame['schema_version'] == 'addp.graph-exposed-pixels/v1' and type(frame['margin']) is int and frame['margin'] == 3 and
                all(number(frame[key]) and frame[key] > 0 for key in ('width','height')) and type(frame['stableFrames']) is int and frame['stableFrames'] >= 10,
                'Exposed pixel frame not stable')
        color = frame['color']
        require(type(color) is list and len(color) == 3 and all(type(v) is int and 0 <= v <= 255 for v in color) and frame['fillColor'] == color, 'Exposed fill color differs')
        snapshots.add((frame['width'],frame['height'],frame['stableFrames'])); colors.add(tuple(color))
        members = {key:node for key,node in native.items() if node['labels'] == shape['labels']}
        require(type(hits) is list and type(frame['nodes']) is list and len(hits) == len(frame['nodes']) == len(members), 'Exposed visible nodes missing')
        require({h.get('source_id') for h in hits} == set(members), 'Exposed source identities incomplete')
        for pixel, hit in zip(frame['nodes'],hits):
            require(type(hit) is dict and set(hit) == {'source_id','native_id','x','y'} and hit['source_id'] in members and hit['native_id'] == members[hit['source_id']]['id'], 'Exposed actual click native identity differs')
            require(type(pixel) is dict and set(pixel) == {'x','y','box','pixelCount','geometry','samples','pixel'} and all(number(pixel[k]) for k in ('x','y')) and
                    pixel['x'] == hit['x'] and pixel['y'] == hit['y'] and type(pixel['pixelCount']) is int and pixel['pixelCount'] >= 40, 'Exposed click/pixel count differs')
            box = pixel['box']; geometry = pixel['geometry']
            require(type(box) is list and len(box) == 4 and all(number(v) for v in box) and 1 < box[0] <= hit['x'] <= box[2] < frame['width']-1 and
                    1 < box[1] <= hit['y'] <= box[3] < frame['height']-1 and box[2]-box[0] >= 3 and box[3]-box[1] >= 3, 'Exposed pixel bounds invalid')
            require(type(geometry) is dict and set(geometry) == {'x','y','rx','ry','color','alpha','order'} and all(number(geometry[k]) for k in ('x','y','rx','ry','alpha')) and
                    geometry['alpha'] == 1 and type(geometry['order']) is int and geometry['order'] >= 0 and geometry['color'] == color and
                    geometry['rx'] > 3 and geometry['ry'] > 3 and abs(geometry['rx']-geometry['ry']) < .1 and
                    1 < geometry['x']-geometry['rx'] <= box[0] and box[2] <= geometry['x']+geometry['rx'] < frame['width']-1 and
                    1 < geometry['y']-geometry['ry'] <= box[1] and box[3] <= geometry['y']+geometry['ry'] < frame['height']-1, 'Exposed drawn circle differs')
            samples = pixel['samples']
            require(type(samples) is list and len(samples) == 5 and len({(point['x'],point['y']) for point in samples}) == 5, 'Exposed raw pixel samples missing')
            for point in [dict(x=hit['x'],y=hit['y'],pixel=pixel['pixel']),*samples]:
                require(type(point) is dict and set(point) == {'x','y','pixel'} and all(number(point[k]) for k in ('x','y')) and
                        box[0] <= point['x'] <= box[2] and box[1] <= point['y'] <= box[3], 'Exposed raw pixel coordinate differs')
                rgba = point['pixel']
                require(type(rgba) is list and len(rgba) == 4 and all(type(v) is int and 0 <= v <= 255 for v in rgba) and rgba[3] > 200 and
                        all(abs(rgba[i]-color[i]) <= 12 for i in range(3)), 'Exposed sample is not actual node fill')
            require(hit['native_id'] not in observed, 'Exposed duplicate native identity')
            observed[hit['native_id']] = dict(geometry=geometry,hit=hit,pixel=pixel,width=frame['width'],height=frame['height'])
    require(len(snapshots) == 1 and len(colors) == len(shapes) and set(observed) == {n['id'] for n in expected['nodes']} and
            {v['geometry']['order'] for v in observed.values()} == set(range(len(observed))), 'Exposed global frame/paint circle identity differs')
    for key, entry in observed.items():
        own = entry['geometry']
        for point in [entry['hit'],*entry['pixel']['samples']]:
            for other_key, other_entry in observed.items():
                circle = other_entry['geometry']
                distance = math.hypot((point['x']-circle['x'])/circle['rx'],(point['y']-circle['y'])/circle['ry'])
                clearance = ((1-distance) if other_key == key else (distance-1))*min(circle['rx'],circle['ry'])
                require(clearance > 3, 'Exposed point lies inside another circle or unsafe border')
    return observed


def verify_overlap_paint(overlap, entries, owner, nearby):
    number = lambda v: type(v) in (int,float) and math.isfinite(v)
    require(type(overlap) is dict and set(overlap) == {'point','pixel','cleared_at','fills','stable_frames','hit'} and
            type(overlap['stable_frames']) is int and overlap['stable_frames'] >= 10 and type(overlap['cleared_at']) is int and overlap['cleared_at'] > 0, 'Overlap paint origin/frame missing')
    point = overlap['point']; color = entries[owner]['geometry']['color']
    require(type(point) is dict and set(point) == {'x','y'} and all(number(v) for v in point.values()), 'Overlap point invalid')
    for key in (owner,nearby):
        circle=entries[key]['geometry']
        require((1-math.hypot((point['x']-circle['x'])/circle['rx'],(point['y']-circle['y'])/circle['ry']))*min(circle['rx'],circle['ry']) > 3, 'Overlap sample not inside both circles')
    rgba=overlap['pixel']
    require(type(rgba) is list and len(rgba) == 4 and all(type(v) is int and 0 <= v <= 255 for v in rgba) and rgba[3] > 200 and all(abs(rgba[i]-color[i]) <= 12 for i in range(3)), 'Overlap actual pixel is not shared fill')
    fills=overlap['fills']; previous=overlap['cleared_at']
    require(type(fills) is list and bool(fills), 'Overlap paint operations missing')
    for fill in fills:
        require(type(fill) is dict and set(fill) == {'native_id','sequence','x','y','rx','ry','color','alpha','clip'} and fill['native_id'] in entries and
                type(fill['sequence']) is int and fill['sequence'] > previous, 'Overlap paint sequence/native identity invalid')
        previous=fill['sequence']; circle=entries[fill['native_id']]['geometry']
        require(all(type(fill[k]) is type(circle[k]) and fill[k] == circle[k] for k in ('x','y','rx','ry','color','alpha')), 'Overlap paint geometry differs')
        require(math.hypot((point['x']-fill['x'])/fill['rx'],(point['y']-fill['y'])/fill['ry']) < 1, 'Overlap paint misses the point')
        clip=fill['clip']
        require(clip is None or (type(clip) is list and len(clip) == 4 and all(number(v) for v in clip) and clip[0] <= point['x'] < clip[2] and clip[1] <= point['y'] < clip[3]), 'Overlap repaint was clipped away from the sampled point')
    top=fills[-1]['native_id']; hit=overlap['hit']
    require(top in (owner,nearby) and type(hit) is dict and set(hit) == {'source_id','native_id','x','y'} and hit == dict(entries[top]['hit'],x=point['x'],y=point['y']), 'Overlap real mouse selection differs from last visible paint')


def verify_same_color_geometry(visual, expected):
    require(type(visual) is dict and set(visual) == set(expected) | {'schema_version','parking','drags','before_drag','baseline','phases','requests','target_edge_id'} and
            visual['schema_version'] == 'addp.graph-same-color-geometry/v1' and all(type(visual.get(k)) is type(v) and visual[k] == v for k,v in expected.items()) and
            expected.get('geometry_case') == 'same-color', 'Same-color evidence header differs')
    ids=verify_dense_reference(expected['nodes'],expected['edges']); source,target=ids['n01'],ids['n12']
    edge=next(e for e in expected['edges'] if e['source'] == source and e['target'] == target and e['type'] == 'Parallel')
    edges=[e for e in expected['edges'] if e['type'] == 'Parallel']
    require(visual['target_edge_id'] == edge['id'], 'Same-color target native relationship differs')
    before=visual['before_drag']; require(set(before) == {'nodes','edges','groups','relationship_pixels'}, 'Same-color initial frame fields differ')
    previous=verify_exposed_frame(before,expected,edges)
    baseline=visual['baseline']; require(set(baseline) == set(before) | {'phase','layout','relationships'} and baseline['phase'] == 'same-color-baseline' and baseline['layout'] == 'force', 'Same-color baseline fields differ')
    base_entries=verify_exposed_frame(baseline,expected,edges); verify_relationship_frame(baseline['relationships'],[edge],baseline)
    require(all((entry['width'],entry['height']) == (base_entries[key]['width'],base_entries[key]['height']) and all(entry['geometry'][k] == base_entries[key]['geometry'][k] for k in ('rx','ry','color')) for key,entry in previous.items()), 'Same-color initial circle style/viewport differs')
    parking=[dict(native_id=ids[key],x=70+i%6*115,y=90 if i<6 else 680) for i,key in enumerate(sorted(ids))]
    require(visual['parking'] == parking, 'Same-color fixed parking differs')
    planned=[dict(native_id=p['native_id'],point={k:p[k] for k in ('x','y')}) for p in parking]+[dict(native_id=source,point=dict(x=200,y=330)),dict(native_id=target,point=dict(x=550,y=330))]
    phases=visual['phases']; require(type(phases) is list and len(phases) == 6, 'Same-color near/overlap/restoration stages missing')
    for phase,(side,state) in zip(phases,[(side,state) for side in ('source','target') for state in ('near','overlap','restored')]):
        owner=source if side == 'source' else target; nearby=ids['n02' if side == 'source' else 'n11']
        require(set(phase) == set(baseline) | {'side','state','owner','nearby','planned_center'} | ({'overlap'} if state == 'overlap' else set()) and
                phase['phase'] == 'same-color-'+side+'-'+state and phase['layout'] == 'force' and phase['side'] == side and phase['state'] == state and phase['owner'] == owner and phase['nearby'] == nearby,
                'Same-color stage identity differs')
        entries=verify_exposed_frame(phase,expected,edges); verify_relationship_frame(phase['relationships'],[edge],phase)
        require(all((entry['width'],entry['height']) == (base_entries[key]['width'],base_entries[key]['height']) and all(entry['geometry'][k] == base_entries[key]['geometry'][k] for k in ('rx','ry','color')) for key,entry in entries.items()), 'Same-color circle style/viewport changed across stages')
        require(entries[owner]['geometry']['color'] == entries[nearby]['geometry']['color'] and next(n['labels'] for n in expected['nodes'] if n['id'] == owner) == next(n['labels'] for n in expected['nodes'] if n['id'] == nearby), 'Same-color nearby node has different labels/fill')
        hit=baseline['relationships']['hits'][0]; anchor=hit['path'][0][1:] if hit['path_source'] == owner else hit['path'][1][-2:]
        circle=base_entries[owner]['geometry']; delta=[anchor[0]-circle['x'],anchor[1]-circle['y']]
        factor=(circle['rx']+base_entries[nearby]['geometry']['rx']+2)/math.hypot(*delta) if state == 'near' else .8
        parked=next(p for p in parking if p['native_id'] == nearby)
        destination={k:parked[k] for k in ('x','y')} if state == 'restored' else {k:circle[k]+delta[i]*factor for i,k in enumerate(('x','y'))}
        require(phase['planned_center'] == destination and phase['relationships']['hits'][0]['path'] == hit['path'], 'Same-color fixed target or relationship path changed')
        planned.append(dict(native_id=nearby,point=destination))
        actual=entries[nearby]['geometry']; distance=math.hypot(actual['x']-entries[owner]['geometry']['x'],actual['y']-entries[owner]['geometry']['y'])
        if state == 'near': require(distance > actual['rx']+entries[owner]['geometry']['rx'], 'Near circles unexpectedly overlap')
        elif state == 'overlap':
            require(0 < distance < min(actual['rx'],entries[owner]['geometry']['rx']), 'Partial occlusion must cover both centers without coincidence')
            verify_overlap_paint(phase['overlap'],entries,owner,nearby)
        else:
            require(all(math.hypot(entries[k]['geometry']['x']-base_entries[k]['geometry']['x'],entries[k]['geometry']['y']-base_entries[k]['geometry']['y']) < 2 for k in entries), 'Native circle identities did not restore to baseline positions')
    drags=visual['drags']; require(type(drags) is list and len(drags) == 20, 'Same-color real drag sequence incomplete')
    centers={key:dict(x=v['geometry']['x'],y=v['geometry']['y']) for key,v in previous.items()}
    number=lambda v:type(v) in (int,float) and math.isfinite(v)
    for index,(drag,wanted) in enumerate(zip(drags,planned)):
        require(type(drag) is dict and set(drag) == {'native_id','from','to','center_before','center_after','planned_center'} and drag['native_id'] == wanted['native_id'] and drag['planned_center'] == wanted['point'] and
                all(type(drag[k]) is dict and set(drag[k]) == {'x','y'} and all(number(v) for v in drag[k].values()) for k in ('from','to','center_before','center_after','planned_center')), 'Same-color mouse drag fields differ')
        key=drag['native_id']; radius=previous[key]['geometry']['rx']
        require(math.hypot(*(drag['center_before'][k]-centers[key][k] for k in ('x','y'))) < .01 and math.hypot(*(drag['from'][k]-drag['center_before'][k] for k in ('x','y'))) < radius-3 and
                all(abs(drag['to'][k]-drag['from'][k]-(wanted['point'][k]-drag['center_before'][k])) < .00001 for k in ('x','y')) and
                math.hypot(*(drag['center_after'][k]-wanted['point'][k] for k in ('x','y'))) < 2, 'Same-color drag did not preserve exposed grab offset/native geometry')
        centers[key]=drag['center_after']
        if index >= 13:
            stage=baseline if index == 13 else phases[index-14]
            actual=verify_exposed_frame(stage,expected,edges)
            require(all(math.hypot(actual[k]['geometry']['x']-v['x'],actual[k]['geometry']['y']-v['y']) < .01 for k,v in centers.items()), 'Same-color actual drawn circles differ after drag')
    root=f"/api/v1/graph/graphs/{expected['graph_id']}"
    require(visual['requests'] == [dict(method='POST',path=root+'/search',body=dict(query='Dense',limit=30)),dict(method='POST',path=root+'/expand',body=dict(target=dict(kind='entity',id=source),depth=2,node_limit=200,relationship_limit=400))], 'Same-color geometry issued unexpected requests')


def run_endpoint_browser(expected):
    artifact = Path(SUPPORT.required_environment("ADDP_ONLINE_ARTIFACT_DIR"))
    same_color = expected.get("geometry_case") == "same-color"
    prefix = "graph-same-color-geometry" if same_color else "graph-endpoint-geometry"
    raw = artifact / (prefix + ".json")
    for pattern in (prefix+"*.json", ("same-color" if same_color else "endpoint")+"-*-console.png"):
        for path in artifact.glob(pattern): path.unlink()
    environment = {k: v for k,v in os.environ.items() if not k.endswith(("_ACCESS_TOKEN", "_USERNAME", "_PASSWORD"))}
    for key in ("ADDP_ONLINE_TEST_USER_USERNAME", "ADDP_ONLINE_TEST_USER_PASSWORD"):
        environment[key] = SUPPORT.required_environment(key)
    environment["ADDP_ONLINE_GRAPH_GEOMETRY_EXPECTATIONS"] = json.dumps(expected)
    result = subprocess.run(["npm", "exec", "--", "playwright", "test", "e2e/online/graph-protection.spec.js", "--config=playwright.online.config.js", "--grep", "same-color nearby endpoint geometry" if same_color else "fixed nearby endpoint geometry", "--reporter=line"],
        cwd=Path(__file__).resolve().parents[2] / "console/frontend", env=environment, timeout=300)
    require(result.returncode == 0 and raw.is_file(), "Endpoint real Console evidence missing or failed")
    visual = json.loads(raw.read_text())
    (verify_same_color_geometry if same_color else verify_endpoint_geometry)(visual, expected)
    for stage in [visual["baseline"], *visual["phases"]]:
        require((artifact / (stage["phase"] + "-console.png")).is_file(), "Endpoint screenshot missing")
        require(json.loads((artifact / ("graph-" + stage["phase"] + "-frame.json")).read_text()) ==
                {**expected, "schema_version": "addp.graph-same-color-frame/v1" if same_color else "addp.graph-endpoint-geometry-frame/v1", "stage": stage}, "Independent endpoint frame differs")
        if "overlap" in stage: require((artifact / (stage["phase"]+"-hit-console.png")).is_file(), "Overlap native selection screenshot missing")
    require(json.loads((artifact / (prefix+"-diagnostics.json")).read_text()) == {"schema_version": "addp.graph-browser-diagnostics/v1", "entries": []}, "Endpoint browser diagnostics not clean")
    return visual


def run_endpoint_case(preparer, reader, engine_id, tenant_id, report, *, case="endpoint-geometry"):
    require(case in {"endpoint-geometry","endpoint-same-color"}, "Unknown endpoint preparation case")
    report["engine_type"] = engine_type()
    report["identities"] = {"reader": identity(reader, tenant_id, READER_PERMISSIONS), "preparer": identity(preparer, tenant_id, PREPARER_PERMISSIONS)}
    require(len({v["principal_id"] for v in report["identities"].values()}) == 2, "Endpoint identities must remain distinct")
    ontology_id = control_id = None
    failed = False
    try:
        ontology_id = create_ontology(preparer)
        create_schema(preparer, ontology_id)
        target = target_paths(engine_id)[1]
        control = preparer.request("POST", "/api/v1/graph/graphs", (201,), {"name": "Hosted endpoint owned control", "ontology_id": ontology_id,
            "engine_id": target["engine_id"], "index_management": "managed", "catalog_path": target["catalog_path"]}).payload
        control_id = SUPPORT.positive_int(control.get("id"), "endpoint control graph")
        require(control.get("is_public") is False, "Endpoint control must remain private")
        # Managed indexes are created by the first non-empty search, not by graph creation.
        # The native reset must still find exactly this deleted control's owned index.
        found = reader.request("GET", f"/api/v1/graph/kg/{control_id}/search?q=Fixture&page_size=100", (200,)).payload
        require(type(found.get("total")) is int and found["total"] == 3 and len(found.get("data", [])) == 3 and
                {(item.get("properties", {}).get("id"), item.get("properties", {}).get("name")) for item in found["data"]} ==
                {("p1", "Fixture one"), ("p2", "Fixture two"), ("p3", "Fixture three")}, "Endpoint managed control search differs")
        previous = control_id
        preparer.request("DELETE", f"/api/v1/graph/graphs/{control_id}", (200,))
        preparer.request("GET", f"/api/v1/graph/graphs/{control_id}", (404,))
        control_id = None
        run_dense(preparer, reader, tenant_id, target, report, previous, case=case)
    except Exception:
        failed = True
        raise
    finally:
        errors = []
        for kind, identifier in (("graphs", control_id), ("ontologies", ontology_id)):
            if identifier:
                try:
                    preparer.request("DELETE", f"/api/v1/graph/{kind}/{identifier}", (200,))
                    preparer.request("GET", f"/api/v1/graph/{kind}/{identifier}", (404,))
                except Exception: errors.append("delete endpoint " + kind)
        report["cleanup"] = {"errors": errors, "domain_resources_remaining": 0 if not errors else None}
        if not failed: require(not errors, "Endpoint domain cleanup failed")


def main():
    report = {"schema_version": "addp.graph-protection-online/v16", "passed": False}
    output = Path(SUPPORT.required_environment("ADDP_ONLINE_ARTIFACT_DIR")) / "graph-protection.json"
    try:
        require(os.environ.get("ADDP_ONLINE_HOSTED") == "1" and os.environ.get("GITHUB_ACTIONS") == "true", "This suite only runs in disposable Hosted deployments")
        env = SUPPORT.required_environment
        gateway, graph = env("GATEWAY_URL"), env("GRAPH_URL")
        case = graph_case(os.environ)
        if case in {"endpoint-geometry", "endpoint-same-color"}:
            report.update(schema_version="addp.graph-same-color-online/v1" if case == "endpoint-same-color" else "addp.graph-endpoint-geometry-online/v1", case=case)
            run_endpoint_case(Client(gateway, env("ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN"), 30), Client(gateway, env("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"), 30),
                SUPPORT.positive_int(env("ADDP_ONLINE_TEST_ENGINE_ID"), "engine id"), SUPPORT.positive_int(env("ADDP_ONLINE_TEST_TENANT_ID"), "tenant id"), report, case=case)
            report["passed"] = True
            return 0
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
