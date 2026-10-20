#!/usr/bin/env python3
"""Real User/Gateway/native graph protection acceptance on disposable Hosted."""
from __future__ import annotations

import importlib.util
import json
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


def run_browser(report, graph_id, engine_id, tenant_id):
    env = SUPPORT.required_environment
    env("ADDP_ONLINE_TEST_USER_USERNAME")
    env("ADDP_ONLINE_TEST_USER_PASSWORD")
    artifacts = Path(env("ADDP_ONLINE_ARTIFACT_DIR"))
    output = artifacts / "graph-protection-browser.json"
    diagnostics = artifacts / "graph-browser-diagnostics.json"
    screenshots = [artifacts / name for name in ("graph-private-browse-console.png", "graph-private-knowledge-console.png",
                   *(f"graph-private-theme-{mode}.png" for mode in ("light", "dark", "blue", "purple")))]
    for path in [output, diagnostics, *screenshots]:
        path.unlink(missing_ok=True)
    expected = {"run_id": env("ADDP_ONLINE_TEST_RUN_ID"), "tenant_id": str(tenant_id),
                "principal_id": report["identities"]["reader"]["principal_id"], "engine_id": engine_id, "graph_id": graph_id, "engine_type": engine_type()}
    # The browser authenticates the ordinary User through the formal login UI.
    # It must not inherit any API fixture's preparation or consumption tokens.
    environment = {key: value for key, value in os.environ.items() if not key.endswith("_ACCESS_TOKEN")}
    environment.update(ADDP_ONLINE_GRAPH_BROWSER_EXPECTATIONS=json.dumps(expected), ADDP_ONLINE_GRAPH_BROWSER_REPORT=str(output))
    result = subprocess.run(["npm", "exec", "--", "playwright", "test", "e2e/online/graph-protection.spec.js",
                             "--config=playwright.online.config.js"],
                            cwd=Path(__file__).resolve().parents[2] / "console/frontend", env=environment, timeout=240)
    require(result.returncode == 0 and output.is_file() and all(path.is_file() for path in screenshots),
            "Ordinary Graph Console acceptance failed or omitted browser evidence")
    require(diagnostics.is_file() and json.loads(diagnostics.read_text()) == {
        "schema_version": "addp.graph-browser-diagnostics/v1", "entries": []
    }, "Graph Console omitted clean browser diagnostics")
    evidence = json.loads(output.read_text())
    expected.update(schema_version="addp.graph-protection-browser/v3", result="passed", permissions=sorted(READER_PERMISSIONS),
                    node_count=3, relationship_count=2, browse_snapshot=True, canvas_visible=True, native_search=True, knowledge_search=True,
                    public_switch_disabled=True, graph_writes=0, failed_business_responses=0, browser_errors=0,
                    release_assets=True, deep_link_reload=True, theme_modes=["light", "dark", "blue", "purple"], theme_canvas_pixels=True, theme_typography=True)
    require(isinstance(evidence, dict) and set(evidence) == set(expected) | {"anonymous_refresh_401"} and
            all(type(evidence[key]) is type(value) and evidence[key] == value for key, value in expected.items()) and
            type(evidence["anonymous_refresh_401"]) is int and 0 <= evidence["anonymous_refresh_401"] <= 1,
            "Graph Console evidence differs from the same minimum-permission User, native graph or clean diagnostics")
    return evidence


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
        expected.update(protection_required=True, protected_result_absent=True, protection_stage="preparation", control_readable=True)
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
        report["browser"] = run_browser(report, target, engine_id, tenant_id)
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
    report = {"schema_version": "addp.graph-protection-online/v5", "passed": False}
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
