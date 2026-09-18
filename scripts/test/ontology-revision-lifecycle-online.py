"""Accept native Beijing Outdoor revision publication through real Gateway/IAM."""

from __future__ import annotations

import hashlib
import json
import math
import os
import re
import signal
import sys
import time
from importlib import import_module
from pathlib import Path

API = import_module("scripts.utils.online-api")
SUITE = "ontology-revision-lifecycle"
BASE = "/api/v1/ontology/ontologies"
PERMISSIONS = {"ontology.revision.read", "ontology.revision.update", "ontology.revision.publish",
               "system.execution_authorization.create"}


def outdoor_definition():
    # Synthetic type-level semantics only; no claim about actual business rows.
    fixture = Path(__file__).resolve().parents[2] / "ontology/backend/internal/semantic/testdata/beijing_outdoor_online.json"
    return json.loads(fixture.read_text())


def require(condition, message):
    if not condition:
        raise API.SuiteError(message)


def revision_matches(item, ontology_id, revision, tenant_id, status):
    require(item.get("ontology_id") == ontology_id and item.get("revision") == revision
            and item.get("status") == status, "revision identity or lifecycle mismatch")
    scope = item.get("snapshot", {}).get("definition", {}).get("scope", {})
    require(scope == {"tenant_id": tenant_id, "ontology_id": ontology_id, "revision": revision},
            "snapshot scope must come from the authenticated Tenant and exact revision")
    require(bool(re.fullmatch(r"[0-9a-f]{64}", item.get("digest", ""))), "invalid snapshot digest")
    return API.require_positive_int(item, "version")


def wait_active(client, path, accepted, digest, *, timeout=60, clock=time.monotonic, sleep=time.sleep):
    deadline = clock() + timeout
    while clock() < deadline:
        projection = client.request("GET", f"{path}/projections/{accepted['generation']}", (200,)).payload
        require(all(projection.get(k) == accepted[k] for k in ("ontology_id", "revision", "generation", "execution_id"))
                and projection.get("digest") == digest and projection.get("predecessor_generation") is None,
                "projection provenance mismatch")
        status = projection.get("status")
        require(status in {"pending", "building", "ready"}, "projection did not become ready")
        if status == "ready":
            head = client.request("GET", path, (200,)).payload
            require(head.get("ontology_id") == accepted["ontology_id"]
                    and head.get("active_revision") == accepted["revision"]
                    and head.get("active_generation") == accepted["generation"], "ready projection is not active")
            require(head.get("activation_version") == projection.get("baseline_version", -1) + 1,
                    "activation did not advance its baseline exactly once")
            return head
        sleep(min(0.2, max(0, deadline - clock())))
    raise API.SuiteError("projection activation timed out")


def run_suite(client, tenant_id, run_id, report, checkpoint=lambda: None, wait=wait_active):
    ontology_id = "beijing_outdoor_" + hashlib.sha256(run_id.encode()).hexdigest()[:24]
    path = f"{BASE}/{ontology_id}"
    report.update({"schema_version": "addp.online-suite/v1", "suite": SUITE, "run_id": run_id,
                   "tenant_id": str(tenant_id), "ontology_id": ontology_id, "result": "failed",
                   "cleanup": "deployment_teardown_required", "checks": [], "generations": []})
    checkpoint()
    # Refuse reuse after an interrupted run; there is no delete/history rewrite API.
    client.request("GET", path, (404,))
    published = []
    active = None
    for number in (1, 2):
        revision_path = f"{path}/revisions/{number}"
        definition = outdoor_definition()
        if number == 2:
            definition["classes"][0]["name"] = "北京户外活动观察（修订二）"
        item = client.request("POST", path + "/revisions", (201,),
                              {"revision": number, "definition": definition}).payload
        version = revision_matches(item, ontology_id, number, tenant_id, "draft")
        snapshot, digest = item["snapshot"], item["digest"]
        # The definition supplied to the API must actually be retained, modulo canonical ordering.
        for key, members in definition.items():
            stored = snapshot["definition"].get(key)
            require(isinstance(stored, list) and sorted(stored, key=lambda x: x["id"]) == sorted(members, key=lambda x: x["id"]),
                    "published snapshot lost native definition members")
        if active is not None:
            head = client.request("GET", path, (200,)).payload
            require(all(head.get(k) == active[k] for k in ("active_revision", "active_generation", "activation_version")),
                    "draft creation changed the active revision")
        reviewed = client.request("POST", revision_path + "/submit", (200,), {"version": version}).payload
        reviewed_version = revision_matches(reviewed, ontology_id, number, tenant_id, "in_review")
        require(reviewed_version > version, "review must advance the edit version")
        conflict = client.request("POST", revision_path + "/publish", (409,), {"version": version}).payload
        require(conflict.get("error_code") == "resource_version_conflict", "stale publication was not rejected")
        client.request("GET", revision_path + "/projection", (404,))
        accepted = client.request("POST", revision_path + "/publish", (202,), {"version": reviewed_version}).payload
        require(accepted.get("ontology_id") == ontology_id and accepted.get("revision") == number,
                "publication intent identity mismatch")
        require(all(isinstance(accepted.get(k), str) and re.fullmatch(r"[0-9a-f-]{36}", accepted[k])
                    for k in ("generation", "execution_id")), "publication intent requires immutable generation/execution IDs")
        report["generations"].append({k: accepted[k] for k in ("revision", "generation", "execution_id")})
        checkpoint()
        latest = client.request("GET", revision_path + "/projection", (200,)).payload
        require(all(latest.get(k) == accepted[k] for k in ("generation", "execution_id", "revision")),
                "latest projection cannot recover the accepted publication")
        active = wait(client, path, accepted, digest)
        stored = client.request("GET", revision_path, (200,)).payload
        revision_matches(stored, ontology_id, number, tenant_id, "published")
        require(stored.get("snapshot") == snapshot and stored.get("digest") == digest
                and stored.get("version") == accepted.get("version")
                and stored.get("initial_generation") == accepted["generation"]
                and stored.get("initial_execution_id") == accepted["execution_id"], "frozen revision changed during publication")
        published.append(stored)
        report["checks"].append(f"revision_{number}_activated")
        checkpoint()
    require(published[0]["initial_generation"] != published[1]["initial_generation"], "revisions reused a generation")
    require(client.request("GET", path + "/revisions/1", (200,)).payload == published[0], "second publication changed the first revision")
    # Withdraw the active revision: the older published revision must not be restored implicitly.
    for number in (2, 1):
        result = client.request("POST", f"{path}/revisions/{number}/withdraw", (200,),
                                {"version": published[number - 1]["version"]}).payload
        revision_matches(result, ontology_id, number, tenant_id, "withdrawn")
        head = client.request("GET", path, (200,)).payload
        require(head.get("active_revision") is None and head.get("active_generation") is None,
                "withdrawal implicitly restored another revision")
        require(head.get("activation_version") == active["activation_version"] + 1,
                "withdrawal activation version mismatch")
    report["checks"].extend(["stale_versions_rejected", "latest_projection_discovery", "immutable_history", "withdrawal_without_fallback"])
    report["result"] = "passed"
    checkpoint()
    return report


def main():
    report, evidence = {}, None

    def checkpoint():
        if evidence is not None:
            temporary = evidence.with_suffix(".tmp")
            temporary.write_text(json.dumps(report, ensure_ascii=False, sort_keys=True) + "\n")
            temporary.replace(evidence)

    def interrupted(signum, frame):
        raise API.SuiteError("Ontology Online acceptance interrupted")

    try:
        require(all(os.environ.get(k) == v for k, v in {
            "ADDP_ONLINE_TEST": "1", "ADDP_ONLINE_HOST": "1", "ADDP_ONLINE_HOSTED": "1",
            "GITHUB_ACTIONS": "true", "RUNNER_OS": "Linux", "POSTGRES_DB": "addp_online",
        }.items()), "use make test-online on the disposable Hosted deployment")
        tenant_id = int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"])
        require(tenant_id > 1, "nondefault dedicated Tenant required")
        run_id = os.environ["ADDP_ONLINE_TEST_RUN_ID"]
        require(bool(re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,63}", run_id)), "invalid Run ID")
        directory = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
        require(directory.is_absolute() and not directory.resolve().is_relative_to(Path(__file__).resolve().parents[2]),
                "evidence must be outside checkout")
        directory.mkdir(parents=True, exist_ok=True)
        evidence = directory / (SUITE + ".json")
        timeout = float(os.environ.get("ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS", "10"))
        require(math.isfinite(timeout) and 0 < timeout <= 30, "HTTP timeout must be within (0, 30] seconds")
        token = os.environ["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"]
        require(bool(token), "User Access Token required")
        report["identity"] = API.validate_user_identity(API.GatewayClient(os.environ["SYSTEM_URL"], token, timeout), tenant_id, PERMISSIONS)
        signal.signal(signal.SIGINT, interrupted)
        signal.signal(signal.SIGTERM, interrupted)
        client = API.GatewayClient(os.environ["GATEWAY_URL"], token, timeout)
        run_suite(client, tenant_id, run_id, report, checkpoint)
    except (KeyError, ValueError, API.SuiteError) as error:
        report["result"] = "failed"
        message = str(error) if isinstance(error, API.SuiteError) else "invalid or missing Online configuration"
        print(f"Ontology Online suite failed: {message}", file=sys.stderr)
        return 1
    finally:
        checkpoint()
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
