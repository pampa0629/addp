"""Accept exact metric revision withdrawal and explicit Service rebinding via Gateway."""

from __future__ import annotations

import hashlib
import json
import math
import os
import re
import signal
import sys
from pathlib import Path

from importlib import import_module

_API = import_module("scripts.utils.online-api")
GatewayClient = _API.GatewayClient
SuiteError = _API.SuiteError
assert_tenant = _API.assert_tenant
cleanup_resource = _API.cleanup_resource
require_positive_int = _API.require_positive_int
validate_user_identity = _API.validate_user_identity



REQUIRED_PERMISSIONS = {
    "model.metric_implementation.read", "model.metric_implementation.create",
    "model.metric_implementation.update", "model.metric_implementation.publish",
    "model.metric_implementation.offline", "service.definition.read",
    "service.definition.create", "service.definition.update",
    "service.definition.delete", "service.data_read.execute", "system.execution_authorization.create",
}
MODEL = "/api/v1/model/metric-implementations"
SERVICE = "/api/v1/service/query"
SUITE = "metric-service-revision-lifecycle"
FIXTURE = import_module("scripts.test.metric-service-fixture")


def exact_revision(item, revision_id, status):
    revisions = [r for r in item.get("revisions", []) if r.get("id") == revision_id]
    if len(revisions) != 1 or revisions[0].get("status") != status:
        raise SuiteError(f"expected exact revision {revision_id} to be {status}")
    return revisions[0]


def validate_fixture_query(query, expected):
    if not isinstance(query, dict) or set(query) not in ({"select", "page"}, {"parameters", "select", "page"}):
        raise SuiteError("metric query must contain only parameters, select and page")
    if "parameters" in query and (not isinstance(query["parameters"], dict) or not query["parameters"]):
        raise SuiteError("metric query parameters must be nonempty when provided")
    fields = query["select"]
    if (not isinstance(fields, list) or not fields
            or not all(isinstance(f, str) and f for f in fields)
            or len(set(fields)) != len(fields)):
        raise SuiteError("metric query requires unique selected fields")
    page = query["page"]
    if not isinstance(page, dict) or set(page) != {"limit"}:
        raise SuiteError("metric query requires a bounded first page without cursor")
    limit = require_positive_int(page, "limit")
    if limit > 100:
        raise SuiteError("metric fixture query limit must not exceed 100")
    if (not isinstance(expected, list) or not expected or len(expected) > limit
            or not all(isinstance(row, dict) and set(row) == set(fields) for row in expected)):
        raise SuiteError("expected metric data must be nonempty and match selected fields and limit")


def assert_binding(service, implementation_id, revision_id, tenant_id, name):
    assert_tenant(service, tenant_id, "Query Service")
    source = service.get("data_config", {}).get("source_snapshot", {}).get("metric_source", {})
    if (service.get("config_type") != "analytical" or service.get("status") != "active"
            or service.get("public_access") is not False or service.get("service_name") != name
            or source.get("implementation_id") != implementation_id
            or source.get("revision_id") != revision_id):
        raise SuiteError("Service identity, access, active status or exact revision binding changed")


def assert_query(client, name, query, expected, service_version):
    result = client.request("POST", f"/api/query/{name}/query", (200,), query).payload
    actual = result.get("data")
    if expected and "group_key" in expected[0] and isinstance(actual, list):
        keys = [row.get("group_key") for row in actual if isinstance(row, dict)]
        data_matches = (len(actual) == len(expected) and len(keys) == len(actual)
                        and all(isinstance(key, str) for key in keys)
                        and len(set(keys)) == len(keys)
                        and {row["group_key"]: row for row in actual} ==
                        {row["group_key"]: row for row in expected})
    else:
        data_matches = actual == expected
    if (not data_matches or result.get("page", {}).get("has_more") is not False
            or result.get("service_version") != service_version or not service_version):
        raise SuiteError("metric query did not return the complete expected data and service version")


def run_suite(client, tenant_id, run_id, template_id, template_revision_id, query, expected,
              report, checkpoint=lambda: None):
    validate_fixture_query(query, expected)
    template_path = f"{MODEL}/{template_id}"
    template = client.request("GET", template_path, (200,)).payload
    assert_tenant(template, tenant_id, "metric fixture")
    baseline = exact_revision(template, template_revision_id, "published")
    # Validate the exact fixture before creating retained history; never choose latest.
    client.request("POST", f"{template_path}/revisions/{template_revision_id}/plan", (200,), {})
    definition_revision = require_positive_int(baseline, "metric_definition_revision_id")
    contract = baseline.get("contract")
    if not isinstance(contract, dict) or not contract:
        raise SuiteError("metric fixture contract must be nonempty")
    service_path = None
    report.update({"schema_version": "addp.online-suite/v1", "suite": SUITE,
                   "run_id": run_id, "tenant_id": str(tenant_id), "result": "failed",
                   "fixture": {"implementation_id": template_id, "revision_id": template_revision_id},
                   "retained_history": {}, "checks": [], "cleanup": "not_needed"})
    checkpoint()
    try:
        item = client.request("POST", MODEL, (201,), {
            "fact_table_id": require_positive_int(template, "fact_table_id"),
            "metric_definition_id": require_positive_int(template, "metric_definition_id"),
            "name": f"Online metric {run_id}", "note": f"{SUITE}; retained test history; Run ID {run_id}",
        }).payload
        implementation_id = require_positive_int(item, "id")
        if implementation_id == template_id:
            raise SuiteError("new implementation reused the read-only fixture identity")
        path = f"{MODEL}/{implementation_id}"
        report["retained_history"] = {"implementation_id": implementation_id, "revision_ids": []}
        checkpoint()
        assert_tenant(item, tenant_id, "metric implementation")

        def publish_revision(current):
            draft = client.request("PUT", path + "/draft", (200,), {
                "version": require_positive_int(current, "version"),
                "metric_definition_revision_id": definition_revision, "contract": contract,
            }).payload
            candidates = [r for r in draft.get("revisions", []) if r.get("status") == "draft"]
            if len(candidates) != 1:
                raise SuiteError("new implementation must have exactly one draft")
            revision_id = require_positive_int(candidates[0], "id")
            report["retained_history"]["revision_ids"].append(revision_id)
            checkpoint()
            published = client.request("POST", f"{path}/revisions/{revision_id}/publish", (200,), {
                "version": require_positive_int(draft, "version"),
            }).payload
            exact_revision(published, revision_id, "published")
            return published, revision_id

        item, old_revision = publish_revision(item)
        name = "online_metric_" + run_id.replace("-", "_").replace(".", "_")
        service = client.request("POST", SERVICE, (201,), {
            "service_name": name, "title": f"Online metric {run_id}", "config_type": "analytical",
            "metric_source": {"implementation_id": implementation_id, "revision_id": old_revision},
            "public_access": False, "max_features": query["page"]["limit"],
        }).payload
        service_id = require_positive_int(service, "id")
        service_path = f"{SERVICE}/{service_id}"
        report.update({"temporary_service_id": service_id, "cleanup": "pending"})
        checkpoint()
        assert_binding(service, implementation_id, old_revision, tenant_id, name)
        version = require_positive_int(service, "version")
        assert_query(client, name, query, expected, service.get("service_version"))
        report["checks"].append("initial_query")

        item, new_revision = publish_revision(item)
        if new_revision == old_revision:
            raise SuiteError("new publication reused the old revision identity")
        unchanged = client.request("GET", service_path, (200,)).payload
        assert_binding(unchanged, implementation_id, old_revision, tenant_id, name)
        if unchanged.get("version") != version or unchanged.get("service_version") != service.get("service_version"):
            raise SuiteError("publishing a new revision mutated the bound Service")
        assert_query(client, name, query, expected, service.get("service_version"))
        report["checks"].append("no_automatic_rebinding")

        withdrawn = client.request("POST", f"{path}/revisions/{old_revision}/withdraw", (200,), {
            "version": require_positive_int(item, "version"),
        }).payload
        exact_revision(withdrawn, old_revision, "withdrawn")
        exact_revision(withdrawn, new_revision, "published")
        denied = client.request("POST", f"{path}/revisions/{old_revision}/plan", (409,), {}).payload
        if denied.get("error_code") != "metric_implementation_state_conflict":
            raise SuiteError("withdrawn Model revision did not return its state conflict")
        denied = client.request("POST", f"/api/query/{name}/query", (500,), query).payload
        if denied.get("error_code") != "query_execution_failed" or "data" in denied:
            raise SuiteError("withdrawn revision query did not fail closed")
        unchanged = client.request("GET", service_path, (200,)).payload
        assert_binding(unchanged, implementation_id, old_revision, tenant_id, name)
        if unchanged.get("version") != version or unchanged.get("service_version") != service.get("service_version"):
            raise SuiteError("withdrawal mutated the bound Service")
        report["checks"].append("withdrawn_query_rejected")
        checkpoint()

        rebound = client.request("PUT", service_path + "/metric-source", (200,), {
            "version": version,
            "metric_source": {"implementation_id": implementation_id, "revision_id": new_revision},
        }).payload
        assert_binding(rebound, implementation_id, new_revision, tenant_id, name)
        if rebound.get("id") != service_id or require_positive_int(rebound, "version") <= version:
            raise SuiteError("explicit rebinding did not preserve identity and advance version")
        assert_query(client, name, query, expected, rebound.get("service_version"))
        report["checks"].append("explicit_rebinding_restores_query")
        # Read back the fixture to prove this run has not changed its lifecycle/version.
        final_template = client.request("GET", template_path, (200,)).payload
        if final_template != template:
            raise SuiteError("read-only metric fixture changed during acceptance")
        report["row_count"] = len(expected)
        report["result_checksum"] = hashlib.sha256(json.dumps(expected, sort_keys=True).encode()).hexdigest()
        report["result"] = "passed"
    finally:
        if service_path is not None:
            try:
                cleanup_resource(client, service_path)
                report["cleanup"] = "passed"
                report["temporary_residual_resources"] = 0
            except Exception:
                report["cleanup"] = "failed"
                report["result"] = "failed"
                raise SuiteError(f"cleanup failed for Query Service {report['temporary_service_id']}")
            finally:
                checkpoint()
        else:
            checkpoint()
    return report


def main():
    report = {}
    evidence = None
    def checkpoint():
        if evidence is not None:
            temporary = evidence.with_suffix(".tmp")
            temporary.write_text(json.dumps(report, ensure_ascii=False, sort_keys=True) + "\n")
            temporary.replace(evidence)

    def interrupted(signum, frame):
        raise SuiteError("Online metric acceptance interrupted")

    try:
        if (os.environ.get("ADDP_ONLINE_TEST") != "1" or os.environ.get("ADDP_ONLINE_HOST") != "1"
                or os.environ.get("POSTGRES_DB") != "addp_online"
                or os.environ.get("ADDP_ONLINE_HOSTED") != "1"
                or os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("RUNNER_OS") != "Linux"):
            raise SuiteError("use make test-online on the dedicated addp_online deployment")
        tenant_id = int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"])
        if tenant_id <= 1:
            raise SuiteError("a nondefault dedicated Tenant is required")
        run_id = os.environ["ADDP_ONLINE_TEST_RUN_ID"]
        if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,63}", run_id):
            raise SuiteError("invalid Online Run ID")
        directory = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
        repository = Path(__file__).resolve().parents[2]
        if not directory.is_absolute() or directory.resolve().is_relative_to(repository):
            raise SuiteError("Online evidence directory must be absolute and outside checkout")
        directory.mkdir(parents=True, exist_ok=True)
        evidence = directory / f"{SUITE}.json"
        timeout = float(os.environ.get("ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS", "10"))
        if not math.isfinite(timeout) or not 0 < timeout <= 30:
            raise SuiteError("Online HTTP timeout must be within (0, 30] seconds")
        token = os.environ["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"]
        if not token:
            raise SuiteError("Online User Access Token is required")
        engine_id = int(os.environ["ADDP_ONLINE_CONSUMER_ENGINE_ID"])
        if engine_id <= 0:
            raise SuiteError("disposable metric engine identity is required")
        engine_type = os.environ["ADDP_ONLINE_METRIC_ENGINE_TYPE"]
        if engine_type not in FIXTURE.NAMESPACES:
            raise SuiteError("unsupported metric fixture engine type")
        report["identity"] = validate_user_identity(
            GatewayClient(os.environ["SYSTEM_URL"], token, timeout), tenant_id,
            REQUIRED_PERMISSIONS | FIXTURE.REQUIRED_PERMISSIONS,
        )
        signal.signal(signal.SIGTERM, interrupted)
        signal.signal(signal.SIGINT, interrupted)
        client = GatewayClient(os.environ["GATEWAY_URL"], token, timeout)
        report.update({"schema_version": "addp.online-suite/v1", "suite": SUITE,
                       "run_id": run_id, "tenant_id": str(tenant_id),
                       "engine_type": engine_type, "result": "failed"})
        checkpoint()
        template_id, revision_id = FIXTURE.prepare(client, engine_id, tenant_id, engine_type, report, checkpoint)
        grouped = report["grouped_fixture"]
        report["cases"] = {"count_distinct": {}, "sum_decimal_by_group": {}}
        checkpoint()
        run_suite(client, tenant_id, run_id, template_id, revision_id,
                  FIXTURE.QUERY, FIXTURE.EXPECTED_DATA, report["cases"]["count_distinct"], checkpoint)
        grouped_run_id = run_id[:40] + "_area_" + hashlib.sha256(run_id.encode()).hexdigest()[:12]
        run_suite(client, tenant_id, grouped_run_id, grouped["implementation_id"], grouped["revision_id"],
                  FIXTURE.GROUPED_QUERY, FIXTURE.GROUPED_EXPECTED_DATA,
                  report["cases"]["sum_decimal_by_group"], checkpoint)
        report["result"] = "passed"
        report["cleanup"] = "passed"

    except (KeyError, ValueError, SuiteError) as error:
        report["result"] = "failed"
        # Do not print JSON/parser input, query data, or credentials from environment errors.
        message = str(error) if isinstance(error, SuiteError) else "invalid or missing Online fixture configuration"
        print(f"Metric lifecycle Online suite failed: {message}", file=sys.stderr)
        return 1
    finally:
        checkpoint()
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
