#!/usr/bin/env python3
"""Graph-owned review extension of the existing disposable protection suite."""
import json
import os
import subprocess
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path

PERMISSIONS = {"graph.graph.read", "graph.review.read", "graph.review.approve", "graph.review.reject", "graph.review.update"}
MATERIAL = "ADDP Hosted review fixture/v1"
EXPECTED_STATUS = {"approve": "approved", "reject": "rejected", "modify": "modified", "batch-before": "approved",
                   "bad-native": "pending", "batch-after": "approved", "review-link": "approved"}


def target_path(suite, engine_id):
    target = suite.target_paths(engine_id)[0]
    if suite.engine_type() == "falkordb":
        target["catalog_path"]["segments"][-1]["name"] = "graph_review_fixture"
        return target["catalog_path"]
    review_id = suite.SUPPORT.positive_int(suite.SUPPORT.required_environment("ADDP_ONLINE_GRAPH_REVIEW_ENGINE_ID"), "review engine id")
    suite.require(review_id not in {engine_id, suite.target_paths(engine_id)[1]["engine_id"]}, "Review needs an independent Neo4j engine")
    target["catalog_path"]["engine_id"] = review_id
    return target["catalog_path"]


def upload(client, path, suite):
    boundary = "addp-" + uuid.uuid4().hex
    body = (f'--{boundary}\r\nContent-Disposition: form-data; name="files"; filename="review.txt"\r\n'
            f'Content-Type: text/plain\r\n\r\n{MATERIAL}\r\n--{boundary}--\r\n').encode()
    request = urllib.request.Request(client.base_url + path, data=body, method="POST", headers={
        "Authorization": "Bearer " + client.token, "Content-Type": "multipart/form-data; boundary=" + boundary})
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            suite.require(response.status == 201, "Review material upload failed")
            data = json.load(response)
    except urllib.error.HTTPError as error:
        raise suite.SuiteError(f"Review material upload returned HTTP {error.code}") from None
    suite.require(isinstance(data, list) and len(data) == 1 and data[0].get("status") == "pending", "Material was not persisted")
    return suite.SUPPORT.positive_int(data[0]["id"], "review material id")


def browser(suite, expected):
    env = suite.SUPPORT.required_environment
    artifacts = Path(env("ADDP_ONLINE_ARTIFACT_DIR"))
    output = artifacts / "graph-review-browser.json"
    screenshots = [artifacts / f"graph-review-{phase}-console.png" for phase in ("pending", "modified", "partial-failure")]
    for path in [output, *screenshots]:
        path.unlink(missing_ok=True)
    environment = {key: value for key, value in os.environ.items() if not key.endswith(("_ACCESS_TOKEN", "_USERNAME", "_PASSWORD"))}
    environment.update(ADDP_ONLINE_GRAPH_REVIEW_USERNAME=env("ADDP_ONLINE_GRAPH_REVIEW_USERNAME"),
                       ADDP_ONLINE_GRAPH_REVIEW_PASSWORD=env("ADDP_ONLINE_GRAPH_REVIEW_PASSWORD"),
                       ADDP_ONLINE_GRAPH_REVIEW_EXPECTATIONS=json.dumps(expected), ADDP_ONLINE_GRAPH_REVIEW_REPORT=str(output))
    result = subprocess.run(["npm", "exec", "--", "playwright", "test", "e2e/online/graph-review.spec.js",
                             "--config=playwright.online.config.js"],
                            cwd=Path(__file__).resolve().parents[2] / "console/frontend", env=environment, timeout=240)
    suite.require(result.returncode == 0 and output.is_file() and all(p.is_file() for p in screenshots), "Review Console evidence missing or failed")
    evidence = json.loads(output.read_text())
    required = dict(expected, schema_version="addp.graph-review-browser/v1", result="passed", permissions=sorted(PERMISSIONS),
                    single_approve=True, single_reject=True, modify=True, relation_approve=True,
                    batch_status=500, batch_continued=True, selection_cleared=True, refreshed=True,
                    unexpected_http_failures=0, browser_errors=0, controlled_extraction=True, real_model=False)
    suite.require(type(evidence) is dict and set(evidence) == set(required) and all(type(evidence[key]) is type(value) and evidence[key] == value for key, value in required.items()), "Review Console report differs from the exact fixture and actions")
    return evidence


def verify_items(suite, items, principal_id):
    by_source = {item["source_text"]: item for item in items}
    suite.require(len(items) == len(EXPECTED_STATUS) and set(by_source) == set(EXPECTED_STATUS), "Review queue membership differs")
    for source, status in EXPECTED_STATUS.items():
        item = by_source[source]
        suite.require(item["status"] == status, "Review status mismatch: " + source)
        if status == "pending":
            suite.require(not item.get("element_id") and not item.get("reviewed_by") and not item.get("reviewed_at"), "Failed item was marked reviewed")
        else:
            suite.require(str(item.get("reviewed_by")) == principal_id and bool(item.get("reviewed_at")), "Wrong review attribution")
            if status == "rejected":
                suite.require(not item.get("element_id"), "Rejected item was written")
            else:
                suite.require(bool(item.get("element_id")), "Approved/modified item lacks native identity")
        if status == "modified":
            suite.require(item.get("final_content", {}).get("properties", {}).get("name") == "Modified fixture", "Modified value was not persisted")
    return by_source


def run(suite, preparer, reader, engine_id, tenant_id, ontology_id, control_id, report):
    env = suite.SUPPORT.required_environment
    reviewer = suite.Client(env("GATEWAY_URL"), env("ADDP_ONLINE_GRAPH_REVIEW_ACCESS_TOKEN"), 30)
    identity = suite.identity(reviewer, tenant_id, PERMISSIONS)
    suite.require(identity["principal_id"] not in {x["principal_id"] for x in report["identities"].values()}, "Reviewer must be a separate User")
    report["identities"]["reviewer"] = identity
    catalog = target_path(suite, engine_id)
    gid = tid = mid = None
    failed = False
    evidence = report["review"] = {"controlled_extraction": True, "real_model": False}
    try:
        graph = preparer.request("POST", "/api/v1/graph/graphs", (201,), {
            "name": "Hosted synthetic review " + env("ADDP_ONLINE_TEST_RUN_ID"), "ontology_id": ontology_id,
            "engine_id": catalog["engine_id"], "catalog_path": catalog, "index_management": "managed"}).payload
        gid = suite.SUPPORT.positive_int(graph.get("id"), "review graph id")
        suite.require(graph.get("is_public") is False, "Review graph must remain private")
        root = f"/api/v1/graph/graphs/{gid}"
        tid = suite.SUPPORT.positive_int(preparer.request("POST", root + "/build/tasks", (201,), {
            "name": "Hosted controlled extraction", "confidence_threshold": 0.7}).payload.get("id"), "build task id")
        task_path = root + f"/build/tasks/{tid}"
        mid = upload(preparer, task_path + "/materials", suite)
        preparer.request("POST", task_path + "/run", (200,))
        deadline = time.monotonic() + 120
        while True:
            task = preparer.request("GET", task_path, (200,)).payload
            if task["status"] in {"success", "failed", "timeout", "cancelled"}:
                break
            suite.require(time.monotonic() < deadline, "Controlled build timed out")
            time.sleep(1)
        suite.require(task["status"] == "success" and bool(task.get("execution_id")), "Controlled build did not succeed")
        suite.require(task.get("stats", {}).get("auto_written") == 2 and task["stats"].get("pending_review") == 7, "Build did not create the expected real review queue")
        materials = preparer.request("GET", task_path + "/materials", (200,)).payload
        suite.require(len(materials) == 1 and materials[0]["id"] == mid and materials[0]["status"] == "completed" and
                      materials[0]["processed_chunks"] == materials[0]["total_chunks"] == 1, "Stored material was not processed exactly once")
        queued = reviewer.request("GET", root + "/review?page_size=100", (200,)).payload
        suite.require(queued["total"] == 7 and all(i["status"] == "pending" for i in queued["data"]), "Expected seven pending items")
        ids = {i["source_text"]: i["id"] for i in queued["data"]}
        suite.require(set(ids) == set(EXPECTED_STATUS), "Controlled queue contents differ")
        reader.request("GET", root + "/review", (403,))
        reader.request("POST", root + f"/review/{ids['approve']}/approve", (403,))
        wrong_root = f"/api/v1/graph/graphs/{control_id}/review"
        for suffix in (f"/{ids['approve']}/approve", f"/{ids['reject']}/reject"):
            reviewer.request("POST", wrong_root + suffix, (404,))
        reviewer.request("PUT", wrong_root + f"/{ids['modify']}", (404,), {"final_content": queued["data"][0]["content"]})
        reviewer.request("POST", wrong_root + "/batch/approve", (404,), {"ids": [ids["approve"], ids["batch-after"]]})
        suite.require(all(i["status"] == "pending" for i in reviewer.request("GET", root + "/review?page_size=100", (200,)).payload["data"]), "Cross-graph commands changed review state")
        evidence["browser"] = browser(suite, {"run_id": env("ADDP_ONLINE_TEST_RUN_ID"), "tenant_id": str(tenant_id),
            "principal_id": identity["principal_id"], "engine_type": suite.engine_type(), "graph_id": gid, "items": ids})
        items = reviewer.request("GET", root + "/review?page_size=100", (200,)).payload["data"]
        by_source = verify_items(suite, items, identity["principal_id"])
        reviewer.request("POST", root + f"/review/{ids['approve']}/approve", (409,))
        nodes = reviewer.request("GET", f"/api/v1/graph/kg/{gid}/entities/Person", (200,)).payload
        properties = {n["properties"]["id"]: n for n in nodes["data"]}
        suite.require(nodes["total"] == 6 and set(properties) == {"p1", "p2", "approve", "modify", "batch-before", "batch-after"}, "Native graph contains missing or rejected entities")
        for key in ("approve", "modify", "batch-before", "batch-after"):
            suite.require(properties[key]["id"] == by_source[key]["element_id"], "Native entity identity differs from review persistence")
        suite.require(properties["modify"]["properties"]["name"] == "Modified fixture", "Native graph lost the modified property")
        stats = reviewer.request("GET", f"/api/v1/graph/kg/{gid}/stats", (200,)).payload
        suite.require(stats["node_count"] == 6 and stats["relationship_count"] == 1, "Native graph counts differ")
        neighbors = reviewer.request("GET", f"/api/v1/graph/kg/{gid}/nodes/{properties['p1']['id']}/neighbors", (200,)).payload
        suite.require(len(neighbors.get("neighbors", [])) == 1 and neighbors["neighbors"][0]["node"]["id"] == properties["p2"]["id"], "Approved native relationship is missing")
        expanded = reviewer.request("POST", root.rsplit("/review", 1)[0] + "/expand", (200,), {
            "target": {"kind": "entity", "id": properties["p1"]["id"]}, "depth": 1, "node_limit": 10, "relationship_limit": 10}).payload
        edges = expanded.get("edges", [])
        suite.require(len(edges) == 1 and edges[0]["id"] == by_source["review-link"]["element_id"] and
                      edges[0]["source"] == properties["p1"]["id"] and edges[0]["target"] == properties["p2"]["id"], "Native relationship identity differs from review persistence")
        evidence.update(passed=True, execution_id=task["execution_id"], status_by_source=EXPECTED_STATUS,
                        native_node_count=6, native_relationship_count=1, cross_graph_404=True, repeated_409=True, reader_403=True)
    except Exception:
        failed = True
        raise
    finally:
        errors = []
        if tid:
            try:
                task_state = preparer.request("GET", f"/api/v1/graph/graphs/{gid}/build/tasks/{tid}", (200,)).payload.get("status")
                if task_state in {"pending", "running"}:
                    preparer.request("POST", f"/api/v1/graph/graphs/{gid}/build/tasks/{tid}/cancel", (200,))
                if mid:
                    preparer.request("DELETE", f"/api/v1/graph/graphs/{gid}/build/tasks/{tid}/materials/{mid}", (200,))
                preparer.request("DELETE", f"/api/v1/graph/graphs/{gid}/build/tasks/{tid}", (200,))
                preparer.request("GET", f"/api/v1/graph/graphs/{gid}/build/tasks/{tid}", (404,))
                suite.require(reviewer.request("GET", f"/api/v1/graph/graphs/{gid}/review", (200,)).payload["total"] == 0, "Review items remained after task deletion")
            except Exception:
                errors.append("delete synthetic review task/material/queue")
        if gid:
            try:
                preparer.request("DELETE", f"/api/v1/graph/graphs/{gid}", (200,))
                preparer.request("GET", f"/api/v1/graph/graphs/{gid}", (404,))
            except Exception:
                errors.append("delete synthetic review graph")
        evidence["cleanup"] = {"errors": errors, "domain_resources_remaining": None if errors else 0}
        if not failed:
            suite.require(not errors, "Review fixture cleanup failed")
