"""Exercise Model -> Quality dynamic targets through dedicated Tenant owner APIs."""

from __future__ import annotations

import json
import math
import os
import re
import signal
import sys
import time
import uuid
from importlib import import_module
from pathlib import Path
from urllib.parse import urlsplit, unquote

API = import_module("scripts.utils.online-api")
SuiteError = API.SuiteError
SUITE = "quality-dynamic-binding"
QUALITY = "/api/v1/quality"
ORCH = "/api/v1/orchestrator"
MODEL = "/api/v1/model/logical-tables"
TERMINAL = {"success", "failed", "cancelled", "timeout"}
REQUIRED_PERMISSIONS = {
    "orchestrator.workflow.read", "orchestrator.workflow.create",
    "orchestrator.workflow.delete", "orchestrator.workflow.execute",
    "model.logical_model.read", "model.materialization.execute",
    "quality.rule.read", "quality.rule.create", "quality.rule.update", "quality.rule.delete",
    "quality.plan.read", "quality.plan.create", "quality.plan.update", "quality.plan.delete",
    "quality.plan.execute", "quality.issue.read", "monitor.execution.read",
}


def require(condition, message):
    if not condition:
        raise SuiteError(message)


def validate_fixtures(fixtures):
    require(isinstance(fixtures, list) and len(fixtures) == 2, "exactly two fixtures are required")
    for item in fixtures:
        require(isinstance(item, dict) and set(item) == {"logical_table_id", "locator", "row_count"},
                "fixture must contain logical_table_id, locator and row_count")
        API.require_positive_int(item, "logical_table_id")
        API.require_positive_int(item, "row_count")
        locator = item["locator"]
        require(isinstance(locator, str) and re.fullmatch(
            r"addp://engine/[1-9][0-9]*/path/[^/?#]+/[^/?#]+\?type=table", locator),
            "fixture requires an explicit PostgreSQL table ResourceLocator")
    require(fixtures[0]["logical_table_id"] != fixtures[1]["logical_table_id"], "fixture IDs must differ")
    require(unquote(fixtures[0]["locator"]) != unquote(fixtures[1]["locator"]), "fixture targets must differ")


def rows(client, path):
    """Bounded pagination; never silently truncate issue or overview assertions."""
    result = []
    for page in range(1, 101):
        payload = client.request("GET", f"{path}?page={page}&page_size=100", (200,)).payload
        data, total = payload.get("data"), payload.get("total")
        require(isinstance(data, list) and isinstance(total, int) and total >= 0,
                "invalid paginated response")
        result.extend(data)
        if len(result) == total:
            return result
        require(data and len(result) < total, "inconsistent paginated response")
    raise SuiteError("fixture Tenant exceeds bounded pagination limit")


def wait_execution(client, execution_id, timeout=120):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        item = client.request("GET", f"{ORCH}/executions/{execution_id}", (200,)).payload
        require(item.get("execution_id") == execution_id, "execution identity changed")
        if item.get("status") in TERMINAL:
            return item
        require(item.get("status") in {"pending", "running"}, "unknown execution status")
        time.sleep(1)
    raise SuiteError("orchestration did not reach a terminal state within the deadline")


def assert_quality(child, fixture, parent_id, passed, revision, plan_version, rule_key):
    expected_status = "success" if passed else "failed"
    require(child.get("status") == expected_status and child.get("parent_execution_id") == parent_id,
            "quality status or parent lineage mismatch")
    config, metadata = child.get("execution_config", {}), child.get("metadata", {})
    require(config.get("table_bindings") == [{"alias": "target", "locator": fixture["locator"]}]
            and config.get("task_version") == plan_version, "quality execution used the wrong target or version")
    require(re.fullmatch(r"[0-9a-f]{64}", config.get("target_key", "")) is not None,
            "quality execution has no canonical target key")
    require(metadata.get("passed") is passed and child.get("outputs", {}).get("passed") is passed,
            "quality output does not match the gate result")
    results = metadata.get("rules", [])
    require(len(results) == 1, "expected exactly one quality rule result")
    rule = results[0]
    require(rule.get("rule_key") == rule_key and rule.get("revision_no") == revision
            and rule.get("passed") is passed and rule.get("total_count") == fixture["row_count"]
            and rule.get("observed", {}).get("row_count") == fixture["row_count"]
            and rule.get("failed_count") == (0 if passed else 1), "quality rule result mismatch")
    if not passed:
        require(child.get("error_details", {}).get("code") == "quality.plan.rule_failed",
                "expected a quality failure, not an infrastructure error")
    return config["target_key"]


def run_suite(client, tenant_id, run_id, fixtures, report, checkpoint=lambda: None):
    validate_fixtures(fixtures)
    report.update(schema_version="addp.online-suite/v1", suite=SUITE, run_id=run_id,
                  tenant_id=str(tenant_id), result="failed", resources=[], executions=[],
                  checks=[], observations=[], cleanup="not_needed", uncertain_mutation=False)
    resources, executions = report["resources"], report["executions"]
    baselines = []
    for fixture in fixtures:
        table_id = fixture["logical_table_id"]
        table = client.request("GET", f"{MODEL}/{table_id}", (200,)).payload
        API.assert_tenant(table, tenant_id, "Model fixture")
        require(table.get("status") == "approved", "Model fixture must be approved")
        materialization = table.get("materialization", {})
        parsed = urlsplit(fixture["locator"])
        parent, name = parsed.path.rsplit("/", 1)
        parent_uri = "addp://engine" + parent + "?type=schema"
        require(materialization.get("target_name") == unquote(name)
                and materialization.get("target_parent_locator") == parent_uri,
                "Model fixture target does not match the explicit locator")
        task = client.request("GET", f"{ORCH}/task-providers/model/tasks/logical_table_materialization/{table_id}", (200,)).payload
        require(task.get("execution_contract", {}).get("input_defaults", {}).get("version") == table.get("version"),
                "Model task must supply its current version default")
        baselines.append(table)

    def create(path, body):
        # A lost response is not proof that a POST had no effect. Persist uncertainty first.
        report["uncertain_mutation"] = True
        checkpoint()
        item = client.request("POST", path, (201,), body).payload
        resource_id = API.require_positive_int(item, "id")
        resources.append(f"{path}/{resource_id}")
        report["uncertain_mutation"] = False
        report["cleanup"] = "pending"
        checkpoint()
        API.assert_tenant(item, tenant_id, "temporary resource")
        return item

    try:
        rule_body = {"code": f"online_quality_{run_id}", "name": f"Online gate {run_id}",
                     "type": "row_count", "params": {"min": 0, "max": 0}}
        rule = create(QUALITY + "/rules", rule_body)
        passing_rule = create(QUALITY + "/rules", {**rule_body, "code": f"online_after_{run_id}",
                                                   "params": {"min": 1}})
        rule_key = str(uuid.uuid4())

        def plan_body(code, rule_id, key):
            return {"code": code, "name": code, "description": f"{SUITE}; Run ID {run_id}",
                    "table_bindings": [{"alias": "target", "locator": fixtures[0]["locator"]}],
                    "check_items": [{"rule_key": key, "rule_id": rule_id, "revision_no": 1,
                                     "bindings": {"table": "target"}, "severity": "error", "disabled": False}]}

        body = plan_body(f"online_gate_{run_id}", rule["id"], rule_key)
        plan = create(QUALITY + "/plans", body)
        after = create(QUALITY + "/plans", plan_body(f"online_after_{run_id}", passing_rule["id"], str(uuid.uuid4())))
        plan_path, after_path = f"{QUALITY}/plans/{plan['id']}", f"{QUALITY}/plans/{after['id']}"
        orchestrations = []
        for index, fixture in enumerate(fixtures):
            def step(step_id, provider, task_type, task_id, parameters, dependencies):
                return {"id": step_id, "name": step_id, "provider": provider, "task_type": task_type,
                        "task_id": task_id, "parameters": parameters, "depends_on": dependencies, "timeout": 60}
            orchestrations.append(create(ORCH + "/orchestrations", {
                "name": f"Online quality {run_id} {index}", "description": SUITE, "enabled": False,
                "steps": [step("upstream", "model", "logical_table_materialization", fixture["logical_table_id"], {}, []),
                          step("quality", "quality", "quality_plan", plan["id"],
                               {"table_bindings": {"target": "{{upstream.outputs.target_locator}}"}}, ["upstream"]),
                          step("after", "quality", "quality_plan", after["id"], {}, ["quality"])],
            }))

        def execute(index, passed, revision):
            before = client.request("GET", after_path, (200,)).payload.get("last_execution_id")
            report["uncertain_mutation"] = True
            checkpoint()
            started = client.request("POST", f"{ORCH}/orchestrations/{orchestrations[index]['id']}/execute", (202,), {}).payload
            execution_id = started.get("execution_id")
            require(isinstance(execution_id, str) and str(uuid.UUID(execution_id)) == execution_id,
                    "invalid parent execution ID")
            executions.append(execution_id)
            report["uncertain_mutation"] = False
            checkpoint()
            parent = wait_execution(client, execution_id)
            expected_status = "success" if passed else "failed"
            require(parent.get("status") == expected_status, "parent did not enforce quality gate")
            steps = parent.get("metadata", {}).get("step_results", {})
            require(set(steps) == ({"upstream", "quality", "after"} if passed else {"upstream", "quality"}),
                    "downstream step ran despite failed gate or was missing after recovery")
            require(steps["upstream"].get("status") == "success"
                    and steps["upstream"].get("result", {}).get("outputs", {}).get("target_locator") == fixtures[index]["locator"],
                    "Model output did not provide the expected table")
            require(steps["quality"].get("status") == expected_status, "quality step status mismatch")
            current = client.request("GET", plan_path, (200,)).payload
            require(current.get("table_bindings") == body["table_bindings"] and current.get("version") == plan["version"],
                    "runtime binding changed the saved plan")
            child_id = current.get("last_execution_id")
            require(isinstance(child_id, str) and child_id, "quality execution was not recorded")
            child = client.request("GET", f"{QUALITY}/executions/{child_id}", (200,)).payload
            key = assert_quality(child, fixtures[index], execution_id, passed, revision, plan["version"], rule_key)
            report["observations"].append({"parent_execution_id": execution_id, "execution_id": child_id,
                                           "target_key": key, "revision_no": revision,
                                           "row_count": fixtures[index]["row_count"], "passed": passed})
            after_current = client.request("GET", after_path, (200,)).payload
            if passed:
                require(steps["after"].get("status") == "success"
                        and after_current.get("last_execution_id") != before, "downstream execution missing")
            else:
                require(after_current.get("last_execution_id") == before, "failed gate triggered downstream plan")
            report["checks"].append(f"target_{index}_{expected_status}_r{revision}")
            checkpoint()
            return key, child_id

        def issues():
            return [item for item in rows(client, QUALITY + "/issues") if item.get("plan_id") == plan["id"]]

        key_a, child_a = execute(0, False, 1)
        first = issues()
        require(len(first) == 1 and first[0].get("status") == "open" and first[0].get("target_key") == key_a,
                "first failed target did not create its issue")
        _, child_a = execute(0, False, 1)
        repeated = issues()
        require(len(repeated) == 1 and repeated[0]["id"] == first[0]["id"]
                and repeated[0].get("last_execution_id") == child_a, "repeated failure did not reuse its issue")
        key_b, child_b = execute(1, False, 1)
        require(key_a != key_b, "different targets share a result scope")
        failed = issues()
        require(len(failed) == 2 and {i.get("target_key") for i in failed} == {key_a, key_b}
                and all(i.get("status") == "open" for i in failed), "target failures were merged")
        report["checks"].append("issue_deduplication_and_target_isolation")

        client.request("PUT", f"{QUALITY}/rules/{rule['id']}", (200,),
                       {**rule_body, "version": rule["version"], "params": {"min": 1}})
        frozen = client.request("GET", plan_path, (200,)).payload
        require(frozen.get("version") == plan["version"] and frozen["check_items"][0]["revision_no"] == 1,
                "rule edit automatically upgraded plan")
        body["check_items"][0]["revision_no"] = 2
        plan = client.request("PUT", plan_path, (200,), {**body, "version": plan["version"]}).payload
        _, restored_a = execute(0, True, 2)
        partial = {i["target_key"]: i for i in issues()}
        require(set(partial) == {key_a, key_b} and partial[key_a]["status"] == "resolved"
                and partial[key_b]["status"] == "open" and partial[key_b]["last_execution_id"] == child_b,
                "target A recovery changed target B issue")
        overview = [i for i in rows(client, QUALITY + "/overview") if i.get("plan_id") == plan["id"]]
        require(len(overview) == 2 and {i["target_key"]: i["status"] for i in overview}
                == {key_a: "success", key_b: "failed"}, "overview mixed target observations")
        by_target = {i["target_key"]: i for i in overview}
        for target, execution_id, version, rate in ((key_a, restored_a, plan["version"], 100),
                                                     (key_b, child_b, frozen["version"], 0)):
            item = by_target[target]
            require(item.get("observed_execution_id") == execution_id and item.get("observed_version") == version
                    and item.get("total_rules") == 1 and item.get("pass_rate") == rate,
                    "overview mixed observed versions, execution IDs or rule pass rates")
        execute(1, True, 2)
        require(len(issues()) == 2 and all(i["status"] == "resolved" for i in issues()), "target B did not recover")
        for fixture, baseline in zip(fixtures, baselines):
            require(client.request("GET", f"{MODEL}/{fixture['logical_table_id']}", (200,)).payload == baseline,
                    "Model fixture definition changed")
        report["checks"].extend(["explicit_revision_upgrade", "independent_recovery", "fixture_and_defaults_unchanged"])
        report["result"] = "passed"
    finally:
        # A pending/lost enqueue could still own these definitions: do not race deletion.
        try:
            require(not report["uncertain_mutation"], "mutation outcome uncertain; inspect Run ID resources")
            for execution_id in executions:
                wait_execution(client, execution_id, timeout=60)
            for path in resources:
                if path.startswith(QUALITY + "/plans/"):
                    current = client.request("GET", path, (200, 404))
                    child_id = current.payload.get("last_execution_id")
                    if child_id:
                        child = client.request("GET", f"{QUALITY}/executions/{child_id}", (200,)).payload
                        require(child.get("status") in TERMINAL,
                                "quality child still active; do not delete its definitions")
            failures = []
            for path in reversed(resources):
                try:
                    if path.startswith(ORCH):
                        client.request("DELETE", path, (200,))
                        client.request("GET", path, (404,))
                    else:
                        API.cleanup_resource(client, path)
                except Exception:
                    failures.append(path)
            report["cleanup_failures"] = failures
            require(not failures, "temporary resource cleanup failed; inspect evidence IDs")
            plan_ids = {int(path.rsplit("/", 1)[1]) for path in resources if path.startswith(QUALITY + "/plans/")}
            require(not any(i.get("plan_id") in plan_ids for i in rows(client, QUALITY + "/issues")),
                    "deleted plan left residual issues")
            report["cleanup"] = "passed"
            report["temporary_residual_resources"] = 0
        except Exception:
            report["cleanup"] = "failed"
            report["result"] = "failed"
            raise
        finally:
            checkpoint()
    return report


def main():
    report, evidence = {}, None

    def checkpoint():
        if evidence is not None:
            temporary = evidence.with_suffix(".tmp")
            temporary.write_text(json.dumps(report, sort_keys=True) + "\n")
            temporary.replace(evidence)

    def interrupted(signum, frame):
        raise SuiteError("quality Online suite interrupted or timed out")

    try:
        require(os.environ.get("ADDP_ONLINE_TEST") == "1" and os.environ.get("ADDP_ONLINE_HOST") == "1"
                and os.environ.get("POSTGRES_DB") == "addp_online", "use make test-online on the dedicated addp_online deployment")
        tenant = int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"])
        require(tenant > 1, "nondefault dedicated Tenant required")
        run_id = os.environ["ADDP_ONLINE_TEST_RUN_ID"]
        require(re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,63}", run_id), "invalid Run ID")
        directory = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"])
        require(directory.is_absolute() and not directory.resolve().is_relative_to(Path(__file__).resolve().parents[2]),
                "evidence directory must be absolute and outside checkout")
        directory.mkdir(parents=True, exist_ok=True)
        evidence = directory / (SUITE + ".json")
        timeout = float(os.environ.get("ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS", "10"))
        require(math.isfinite(timeout) and 0 < timeout <= 30, "HTTP timeout must be within (0, 30]")
        token = os.environ["ADDP_ONLINE_TEST_USER_ACCESS_TOKEN"]
        require(bool(token), "dedicated User token required")
        fixtures = json.loads(os.environ["ADDP_ONLINE_QUALITY_FIXTURES_JSON"])
        validate_fixtures(fixtures)
        report["identity"] = API.validate_user_identity(API.GatewayClient(os.environ["SYSTEM_URL"], token, timeout),
                                                       tenant, REQUIRED_PERMISSIONS)
        for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGALRM):
            signal.signal(signum, interrupted)
        signal.alarm(600)
        run_suite(API.GatewayClient(os.environ["GATEWAY_URL"], token, timeout), tenant, run_id, fixtures, report, checkpoint)
    except (KeyError, ValueError, SuiteError) as error:
        report["result"] = "failed"
        message = str(error) if isinstance(error, SuiteError) else "invalid or missing Online fixture configuration"
        print(f"Quality Online suite failed: {message}", file=sys.stderr)
        return 1
    finally:
        signal.alarm(0)
        checkpoint()
    print(json.dumps(report, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
