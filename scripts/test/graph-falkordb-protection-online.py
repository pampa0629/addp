#!/usr/bin/env python3
"""Real User/Gateway/native graph protection acceptance on disposable Hosted."""
from __future__ import annotations

import importlib.util
import json
import os
import sys
import time
import urllib.parse
from pathlib import Path

SPEC = importlib.util.spec_from_file_location("graph_online_support", Path(__file__).with_name("security-transfer-protection-online.py"))
SUPPORT = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = SUPPORT
SPEC.loader.exec_module(SUPPORT)
Client, SuiteError = SUPPORT.GatewayClient, SUPPORT.SuiteError
OWNERS = {"graph", "develop", "manager", "service", "transfer"}
GRAPHS = ("graph_protection_fixture", "graph_protection_control")
READER_PERMISSIONS = {"graph.graph.read"}
OUTSIDER_PERMISSIONS = {"meta.catalog.read"}
PREPARER_PERMISSIONS = {
    "graph.ontology.create", "graph.ontology.read", "graph.ontology.delete",
    "graph.graph.create", "graph.graph.read", "graph.graph.update", "graph.graph.delete",
    "meta.catalog.read", "meta.scan_task.execute", "meta.scan_task.read",
    "security.enrollment.create", "security.enrollment.read", "security.enrollment.update",
}


def require(condition, message):
    if not condition:
        raise SuiteError(message)


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
    require(enrollment.get("state") in {"enrolling", "active"}, "Enrollment unexpectedly stopped protecting")
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
        "properties": [{"name": p, "data_type": "string", "searchable": True} for p in ("id", "name")],
    }).payload
    client.request("POST", f"/api/v1/graph/ontologies/{oid}/relation-types", (201,), {
        "name": "Link", "source_type_id": entity["id"], "target_type_id": entity["id"], "directed": True,
    })


def run(preparer, reader, outsider, anonymous, direct_reader, direct_outsider, direct_anonymous, engine_id, tenant_id, deadline, report):
    report["identities"] = {"reader": identity(reader, tenant_id, READER_PERMISSIONS),
                            "outsider": identity(outsider, tenant_id, OUTSIDER_PERMISSIONS),
                            "preparer": identity(preparer, tenant_id, PREPARER_PERMISSIONS)}
    require(len({x["principal_id"] for x in report["identities"].values()}) == 3, "Fixture identities must be distinct")
    ontology_id = None
    graph_ids = []
    enrollment_id = None
    try:
        ontology_id = create_ontology(preparer)
        create_schema(preparer, ontology_id)
        for name in GRAPHS:
            graph = preparer.request("POST", "/api/v1/graph/graphs", (201,), {
                "name": name, "ontology_id": ontology_id, "engine_id": engine_id, "index_management": "managed",
                "catalog_path": {"version": "catalog.path/v1", "engine_id": engine_id, "segments": [
                    {"term": "server", "kind": "server", "name": ""}, {"term": "graph", "kind": "graph", "name": name}]},
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
        set_public(preparer, target, True)
        report["public_unmanaged"] = {"gateway": probe(anonymous, target, ids, 200), "graph": probe(direct_anonymous, target, ids, 200)}
        SUPPORT.wait_for_scan(preparer, engine_id, deadline)
        item = SUPPORT.find_item(preparer, engine_id, GRAPHS[0], "graph")
        locator = f"addp://engine/{engine_id}/path/{urllib.parse.quote(GRAPHS[0], safe='')}?type=graph&item_id={item['id']}"
        enrollment = preparer.request("POST", "/api/v1/security/protection-enrollments", (201,), {"locator": locator, "validity_mode": "permanent"}).payload
        enrollment_id = enrollment["id"]
        require(enrollment["target"]["resource_identity"] == item["fingerprint"], "Enrollment must match the precise Meta graph identity")
        require(enrollment["target_snapshot"]["engine_id"] == engine_id and enrollment["target_snapshot"]["full_name"] == GRAPHS[0], "Enrollment must retain the exact graph and Engine binding")
        installed = wait_owners(preparer, enrollment_id, deadline)
        report["owners"] = {p["consumer_owner"]: {k: p[k] for k in ("projection_id", "revision", "published_cursor", "acknowledged")} for p in installed["owner_progress"]}
        report["public_protected"] = {"gateway": probe(anonymous, target, ids, 403, "protection"), "graph": probe(direct_anonymous, target, ids, 403, "protection")}
        report["authenticated_protected"] = {"gateway": probe(reader, target, ids, 403, "protection"), "graph": probe(direct_reader, target, ids, 403, "protection")}
        report["public_outsider_protected"] = {"gateway": probe(outsider, target, ids, 403, "protection"), "graph": probe(direct_outsider, target, ids, 403, "protection")}
        report["same_engine_control"] = {"gateway": probe(reader, control, control_ids, 200), "graph": probe(direct_reader, control, control_ids, 200)}
        set_public(preparer, target, False)
        report["private_protected_anonymous"] = probe(anonymous, target, ids, 401)
        report["private_protected_reader"] = probe(reader, target, ids, 403, "protection")
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
        require(not cleanup_errors, "Synthetic Graph domain cleanup failed")


def main():
    report = {"schema_version": "addp.graph-falkordb-protection-online/v1", "passed": False}
    output = Path(SUPPORT.required_environment("ADDP_ONLINE_ARTIFACT_DIR")) / "graph-falkordb-protection.json"
    try:
        require(os.environ.get("ADDP_ONLINE_HOSTED") == "1" and os.environ.get("GITHUB_ACTIONS") == "true", "This suite only runs in disposable Hosted deployments")
        env = SUPPORT.required_environment
        gateway, graph = env("GATEWAY_URL"), env("GRAPH_URL")
        reader, outsider = env("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"), env("ADDP_ONLINE_GRAPH_OUTSIDER_ACCESS_TOKEN")
        run(Client(gateway, env("ADDP_ONLINE_GRAPH_PREPARER_ACCESS_TOKEN"), 30),
            Client(gateway, reader, 30), Client(gateway, outsider, 30), Client(gateway, "", 30),
            Client(graph, reader, 30), Client(graph, outsider, 30), Client(graph, "", 30),
            SUPPORT.positive_int(env("ADDP_ONLINE_TEST_ENGINE_ID"), "engine id"),
            SUPPORT.positive_int(env("ADDP_ONLINE_TEST_TENANT_ID"), "tenant id"), time.monotonic() + 480, report)
        report["passed"] = True
        return 0
    except Exception as error:
        report["error_type"] = type(error).__name__
        print("Graph protection acceptance failed: " + str(error), file=sys.stderr)
        return 1
    finally:
        output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")


if __name__ == "__main__":
    raise SystemExit(main())
