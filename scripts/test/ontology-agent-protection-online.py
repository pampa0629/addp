"""Real owner topology with a bounded, explicitly controlled OpenAI model."""

from __future__ import annotations

import copy
import hashlib
import json
import os
import re
import secrets
import threading
import time
import urllib.request
import uuid
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from importlib import import_module
from pathlib import Path

API = import_module("scripts.utils.online-api")
SECURITY = import_module("scripts.test.security-mysql-owner-protection-online")
LIFECYCLE = import_module("scripts.test.ontology-revision-lifecycle-online")
SUITE = "ontology-agent-protection"
SOURCE = "addp_online_security.spatial_algorithm_source"
OBJECT_ID = "13812345678"  # Fixed synthetic source value, never an actual person.
RULE = "eligible_record"
READER_PERMISSIONS = {
    "agent.session.create", "agent.session.read", "agent.run.create", "agent.run.execute",
    "agent.run.read", "ontology.semantic.read", "service.data_read.execute",
}
TOOLS = ["ontology.classes.list", "ontology.class.context", "ontology.rule.evaluate"]


def require(condition, message):
    if not condition:
        raise API.SuiteError(message)


class ControlledModel:
    """Only select tools and copy their observed version bindings; never evaluate."""

    def __init__(self, ontology_id):
        self.ontology_id = ontology_id
        self.calls = []

    def reply(self, body):
        functions = {item["function"]["name"]: item["function"]
                     for item in body.get("tools", [])}
        if "RouteDecision" in functions:
            name, args = "RouteDecision", {"skill": "ontology-exploration", "needs_platform_semantics": False}
        else:
            observed = []
            for message in body.get("messages", []):
                if message.get("role") != "tool":
                    continue
                try:
                    value = json.loads(message.get("content", ""))
                except (TypeError, ValueError):
                    continue
                if isinstance(value, dict):
                    observed.append(value)
            contexts = [v for v in observed if "class" in v and "rules" in v]
            directories = [v for v in observed if "classes" in v]
            if contexts:
                context = contexts[-1]
                require(context.get("ontology_id") == self.ontology_id
                        and any(r.get("id") == RULE and r.get("business_evaluable") is True
                                for r in context["rules"]), "controlled model lacks a current evaluable rule")
                name = "ontology__rule__evaluate"
                args = {key: context[key] for key in ("ontology_id", "revision", "generation", "activation_version")}
                args.update(rule_id=RULE, object_id=OBJECT_ID)
            elif directories:
                directory = directories[-1]
                require(any(c["id"] == "record" for c in directory["classes"]),
                        "controlled model lacks the registered class")
                name = "ontology__class__context"
                args = {key: directory[key] for key in ("ontology_id", "revision", "generation", "activation_version")}
                args["class_id"] = "record"
            elif "ontology__classes__list" in functions:
                name, args = "ontology__classes__list", {"ontology_id": self.ontology_id}
            else:
                require(not functions, "controlled model received an unexpected tool set")
                self.calls.append("summary")
                return {"role": "assistant", "content": "Synthetic record rule acceptance"}
        require(name in functions, "controlled model selected an unavailable tool")
        self.calls.append(name)
        require(len(self.calls) <= 16, "controlled model call bound exceeded")
        return {"role": "assistant", "content": None, "tool_calls": [{
            "id": "call_" + uuid.uuid4().hex, "type": "function",
            "function": {"name": name, "arguments": json.dumps(args, ensure_ascii=False)},
        }]}


@contextmanager
def model_server(model):
    credential = secrets.token_urlsafe(32)

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def do_POST(self):
            if self.path != "/v1/chat/completions" or self.headers.get("Authorization") != "Bearer " + credential:
                self.send_error(403)
                return
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= 512 * 1024:
                self.send_error(413)
                return
            try:
                body = json.loads(self.rfile.read(length))
                require(body.get("model") == "controlled-ontology", "unexpected upstream model")
                message = model.reply(body)
                payload = {"id": "fixture_" + uuid.uuid4().hex, "object": "chat.completion",
                           "model": "controlled-ontology", "created": int(time.time()),
                           "choices": [{"index": 0, "message": message,
                                        "finish_reason": "tool_calls" if message.get("tool_calls") else "stop"}],
                           "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}}
                if body.get("stream"):
                    raw = ('data: ' + json.dumps({"choices": [{"index": 0, "delta": message,
                                                               "finish_reason": "stop"}]}) + '\n\ndata: [DONE]\n\n').encode()
                    content_type = "text/event-stream"
                else:
                    raw = json.dumps(payload).encode()
                    content_type = "application/json"
                self.send_response(200)
                self.send_header("Content-Type", content_type)
                self.send_header("Content-Length", str(len(raw)))
                self.end_headers()
                self.wfile.write(raw)
            except (API.SuiteError, KeyError, TypeError, ValueError):
                self.send_error(422)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield f"http://127.0.0.1:{server.server_port}/v1", credential
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
        require(not thread.is_alive(), "controlled model did not stop")


def configure_model(client, endpoint, credential):
    prefix = "/api/v1/inference"
    provider = client.request("POST", prefix + "/provider-connections", (201,), {
        "name": "Ontology controlled model", "scope_type": "tenant",
        "adapter_type": "openai_compatible", "endpoint": endpoint,
    }).payload
    client.request("PUT", prefix + "/provider-connections/" + provider["id"] + "/credential",
                   (200,), {"credential": credential})
    deployment = client.request("POST", prefix + "/model-deployments", (201,), {
        "provider_connection_id": provider["id"], "name": "Ontology fixture",
        "upstream_model": "controlled-ontology", "operations": ["chat"], "modalities": ["text"],
    }).payload
    profile = client.request("POST", prefix + "/model-profiles", (201,), {
        "name": "Ontology fixture", "code": "ontology-protection",
        "scope_type": "tenant", "model_deployment_id": deployment["id"],
    }).payload
    for scenario in ("reasoning", "general-chat"):
        path = "/api/v1/agent/settings/inference-bindings/" + scenario
        current = client.request("GET", path, (200,)).payload
        client.request("PUT", path, (200,), {"version": current["version"], "model_profile_id": profile["id"]})
    return {"provider_id": provider["id"], "deployment_id": deployment["id"], "profile_id": profile["id"]}


def run_agent(client, ontology_id, expected, binding):
    session = client.request("POST", "/api/v1/agent/sessions", (201,),
                             {"title": "Isolated protected rule " + expected}).payload
    session_id = API.require_positive_int(session, "id")
    payload = {"threadId": str(session_id), "runId": str(uuid.uuid4()), "state": {},
               "tools": [], "context": [], "forwardedProps": {}, "messages": [{
                   "id": str(uuid.uuid4()), "role": "user",
                   "content": f"本体 {ontology_id} 中，记录 {OBJECT_ID} 符合 {RULE} 吗？",
               }]}
    request = urllib.request.Request(client.base_url + "/api/v1/agent/chat",
        data=json.dumps(payload, ensure_ascii=False).encode(), method="POST",
        headers={"Authorization": "Bearer " + client.token, "Content-Type": "application/json",
                 "Accept": "text/event-stream", "Accept-Language": "zh-cn"})
    events, byte_count = [], 0
    with urllib.request.urlopen(request, timeout=60) as response:
        require(response.status == 200 and response.headers.get_content_type() == "text/event-stream",
                "Agent did not return the formal SSE protocol")
        for line in response:
            byte_count += len(line)
            require(byte_count <= 2 * 1024 * 1024, "Agent SSE exceeded the acceptance bound")
            if line.startswith(b"data:"):
                event = json.loads(line[5:])
                require(event.get("type") != "RUN_ERROR", "Agent reported a run error")
                events.append(event)
    names = [e["toolCallName"] for e in events if e.get("type") == "TOOL_CALL_START"]
    require(names == TOOLS, "Agent did not execute exactly the three ontology tools")
    outcomes = []
    for event in events:
        if event.get("type") == "TOOL_CALL_RESULT":
            try:
                value = json.loads(event["content"])
            except (ValueError, TypeError):
                continue
            if isinstance(value, dict) and "outcome" in value:
                outcomes.append(value)
    require(len(outcomes) == 1, "Agent requires one authoritative rule result")
    outcome = outcomes[0]
    require(outcome["outcome"] == expected and outcome["rule_id"] == RULE and outcome["ontology_id"] == ontology_id
            and all(outcome[key] == binding[key] for key in ("revision", "generation", "activation_version", "digest")),
            "Agent rule outcome or active binding differs")
    text = "".join(e.get("delta", "") for e in events if e.get("type") == "TEXT_MESSAGE_CONTENT")
    expected_text = "现有业务证据不足，无法判断该记录是否符合规则。" if expected == "unknown" else "该记录符合规则。"
    require(text.startswith(expected_text) and RULE in text and f"修订 {binding['revision']}" in text,
            "Agent did not preserve the authoritative decision")
    require(OBJECT_ID not in text, "Agent response exposed a source value")
    runs = client.request("GET", f"/api/v1/agent/sessions/{session_id}/runs", (200,)).payload["data"]
    require(len(runs) == 1, "Agent session has an unexpected run count")
    run = client.request("GET", "/api/v1/agent/runs/" + runs[0]["id"], (200,)).payload
    require(run["status"] == "completed" and run["skill_name"] == "ontology-exploration"
            and run["error_code"] is None, "persisted AgentRun did not complete")
    steps = [s for s in run["steps"] if s["step_type"] == "tool_call"]
    require([s["tool_name"] for s in steps] == TOOLS
            and all(s["status"] == "completed" and s["error_code"] is None for s in steps),
            "persisted steps differ from the formal tool trace")
    return {**binding, "ontology_id": ontology_id, "session_id": session_id,
            "agent_run_id": run["id"], "outcome": expected, "tools": names,
            "persisted_completed": True, "raw_values_absent": True}


def create_service(client, engine_id, source, name):
    fields = source["attributes"]["type_info"]["table"]["fields"]
    output = []
    for original, alias in (("value_a", "record_id"), ("value_b", "status")):
        field = copy.deepcopy(next(f for f in fields if f["name"] == original))
        field["name"] = alias
        field.pop("path", None)
        field["nullable"] = alias != "record_id"
        output.append(field)
    payload = {
        "service_name": name, "title": "Isolated single-record rule facts",
        "config_type": "sql", "engine_id": engine_id,
        "sql_query": f"SELECT value_a AS record_id, value_b AS status FROM {SOURCE} WHERE value_a = :record_id",
        "named_parameters": [{"name": "record_id", "type": "string", "required": True}],
        "output_contract": {"table": {"fields": output, "primary_key": ["record_id"]}},
        "data_config": {"default_fields": ["record_id", "status"], "stable_key": ["record_id"]},
        "protocols": {"rest_api": {"enabled": True, "formats": ["json"]}},
        "public_access": False,
    }
    return client.request("POST", "/api/v1/service/query", (201,), payload).payload


def definition(service, version):
    return {
        "classes": [{"id": "record", "name": "合成记录", "parents": []}],
        "properties": [{"id": "record_status", "class_id": "record", "key": "status",
                        "name": "合成规则字段", "kind": "string"}],
        "relations": [],
        "rules": [{"id": RULE, "class_id": "record", "expression": "status != 'draft'",
                   "basis": "Synthetic protection regression; source values are fixture data.",
                   "inputs": [{"variable": "status", "property_id": "record_status", "on_absent": "unknown"}],
                   "service_binding": {
                       "service_name": service["service_name"], "service_id": service["id"],
                       "service_version": version, "id_parameter": "record_id", "id_field": "record_id",
                       "inputs": [{"variable": "status", "field": "status", "conversion": "string"}],
                   }}],
    }


def prepare_ontology(client, ontology_id, d):
    path = "/api/v1/ontology/ontologies/" + ontology_id
    client.request("GET", path, (404,))
    draft = client.request("POST", path + "/revisions", (201,), {"revision": 1, "definition": d}).payload
    reviewed = client.request("POST", path + "/revisions/1/submit", (200,), {"version": draft["version"]}).payload
    accepted = client.request("POST", path + "/revisions/1/publish", (202,), {"version": reviewed["version"]}).payload
    LIFECYCLE.wait_active(client, path, accepted, draft["digest"])
    return {"revision": 1, "generation": accepted["generation"], "activation_version": 1, "digest": draft["digest"]}


def wait_service_projection(client, enrollment_id, deadline, previous=None):
    while time.monotonic() < deadline:
        enrollment = client.request("GET", "/api/v1/security/protection-enrollments/" + enrollment_id, (200,)).payload
        progresses = [p for p in enrollment.get("owner_progress", []) if p.get("consumer_owner") == "service"]
        require(len(progresses) <= 1, "duplicate Service projection progress")
        if progresses:
            progress = progresses[0]
            changed = previous is None or (
                progress.get("projection_id") == previous["projection_id"]
                and int(progress.get("revision") or "0") > int(previous["revision"])
                and progress.get("published_cursor") != previous["published_cursor"])
            if (enrollment.get("state") == "active" and progress.get("projection_state") == "active"
                    and progress.get("acknowledged") is True and changed):
                return progress
        time.sleep(0.5)
    raise API.SuiteError("Service did not acknowledge this protection revision")


def run_suite(preparer, reader, authorizer, tenant_id, engine_id, run_id, report, checkpoint):
    suffix = hashlib.sha256(run_id.encode()).hexdigest()[:24]
    ontology_id = "protected_record_" + suffix
    report.update(schema_version="addp.ontology-agent-protection/v1", suite=SUITE, run_id=run_id,
                  tenant_id=str(tenant_id), model_mode="controlled_openai_compatible", result="failed",
                  cleanup="deployment_teardown_required", cases=[])
    report["stage"] = "source_preparation"
    checkpoint()
    SECURITY.REGISTRATION.source_grant_participants(authorizer, preparer, tenant_id, scan_preparation=True)
    SECURITY.wait_for_scan(authorizer, engine_id, time.monotonic() + 180, scan_depth="basic")
    SECURITY.REGISTRATION.initialize_exact_record_read_grants(
        authorizer, preparer, tenant_id, [(engine_id, "schema", "addp_online_security", "spatial_algorithm_source")],
        scan_preparation=True)
    source = SECURITY.find_item(preparer, engine_id, SOURCE, "table")
    locator = SECURITY.build_item_locator(engine_id, source)
    service = create_service(preparer, engine_id, source, "ontology-protected-" + suffix)
    policy_id = None
    model = ControlledModel(ontology_id)
    with model_server(model) as (endpoint, credential):
        report["stage"] = "model_configuration"
        checkpoint()
        report["model_configuration"] = configure_model(preparer, endpoint, credential)
        report["stage"] = "unprotected_fact_read"
        checkpoint()
        path = "/api/query/" + service["service_name"] + "/query"
        request = {"parameters": {"record_id": OBJECT_ID}, "select": ["record_id", "status"],
                   "format": "json", "page": {"limit": 2}}
        original = reader.request("POST", path, (200,), request).payload
        require(original["data"] == [{"record_id": OBJECT_ID, "status": OBJECT_ID}]
                and original["page"]["has_more"] is False, "synthetic source is not unique and unprotected")
        version = original["service_version"]
        report["service_binding"] = {"service_id": service["id"], "service_name": service["service_name"],
                                     "service_version": version}
        fact_request = dict(request, require_untransformed=True, expected_service_id=service["id"],
                            expected_service_version=version)
        fact = reader.request("POST", path, (200,), fact_request).payload
        require(fact["fact_status"] == "verified" and fact["data"] == original["data"],
                "positive fact read must verify every selected field")
        report["stage"] = "ontology_publication"
        checkpoint()
        binding = prepare_ontology(preparer, ontology_id, definition(service, version))
        report["stage"] = "agent_positive"
        checkpoint()
        report["cases"].append(run_agent(reader, ontology_id, "matched", binding))
        checkpoint()
        try:
            report["stage"] = "security_projection"
            checkpoint()
            enrollment_id, _ = SECURITY.ensure_enrollment(preparer, engine_id, SOURCE, locator)
            deadline = time.monotonic() + 120
            while time.monotonic() < deadline:
                enrollment = preparer.request("GET", "/api/v1/security/protection-enrollments/" + enrollment_id, (200,)).payload
                if enrollment.get("latest_source_snapshot_hash"):
                    break
                time.sleep(1)
            else:
                raise API.SuiteError("Enrollment did not observe the current source structure")
            previous = wait_service_projection(preparer, enrollment_id, deadline)
            types = SECURITY.definition_array(preparer, "/api/v1/security/sensitive-data-types")
            phone = next(t for t in types if t["code"] == "phone")
            assessment = preparer.request("POST", "/api/v1/security/assessments", (201,), {
                "enrollment_id": enrollment_id, "enrollment_version": enrollment["version"], "component_key": "value_b",
                "sensitive_data_type_id": phone["id"], "security_grade_id": phone["default_security_grade_id"],
                "rationale": "Isolated constant-mask rule regression",
            }).payload
            previous = wait_service_projection(preparer, enrollment_id, deadline, previous)
            policy = preparer.request("POST", "/api/v1/security/protection-policies", (201,), {
                "assessment_id": assessment["id"], "consumer_owner": "service", "action": "service_execute",
                "effect": "mask", "algorithm": SECURITY.CONSTANT_ALGORITHM, "parameters": {"value": "draft"},
                "invalid_value_effect": "suppress", "rationale": "Typed mask must not become a business fact",
            }).payload
            policy_id = str(policy["id"])
            wait_service_projection(preparer, enrollment_id, deadline, previous)
            report["stage"] = "protected_fact_read"
            checkpoint()
            masked = reader.request("POST", path, (200,), request).payload
            require(masked["data"] == [{"record_id": OBJECT_ID, "status": "draft"}],
                    "normal query did not deliver the controlled typed mask")
            unknown = reader.request("POST", path, (200,), fact_request).payload
            require(unknown["fact_status"] == "unknown" and unknown["data"] == []
                    and not unknown["page"].get("has_more") and not unknown["page"].get("next_cursor"),
                    "protected fields were delivered as usable rule facts")
            report["typed_mask_observed"] = True
            report["unknown_fact_read_empty"] = True
            report["stage"] = "agent_protected"
            checkpoint()
            report["cases"].append(run_agent(reader, ontology_id, "unknown", binding))
            report["model_calls"] = model.calls
            require(model.calls.count("ontology__rule__evaluate") == 2,
                    "controlled model did not drive both owner evaluations exactly once")
            report["result"] = "passed"
        finally:
            if policy_id:
                preparer.request("DELETE", "/api/v1/security/protection-policies/" + policy_id, (200,))
            head = preparer.request("GET", "/api/v1/ontology/ontologies/" + ontology_id + "/revisions/1", (200,)).payload
            preparer.request("POST", "/api/v1/ontology/ontologies/" + ontology_id + "/revisions/1/withdraw",
                             (200,), {"version": head["version"]})
            preparer.request("DELETE", "/api/v1/service/query/" + str(service["id"]), (200,))
            report["owned_resources_retired"] = True
            checkpoint()


def validate_report(report, run_id):
    require(report.get("schema_version") == "addp.ontology-agent-protection/v1"
            and report.get("suite") == SUITE and report.get("run_id") == run_id
            and report.get("result") == "passed"
            and report.get("model_mode") == "controlled_openai_compatible"
            and report.get("typed_mask_observed") is True
            and report.get("unknown_fact_read_empty") is True
            and report.get("owned_resources_retired") is True,
            "incomplete protected-rule acceptance report")
    cases = report.get("cases", [])
    require(len(cases) == 2 and [c.get("outcome") for c in cases] == ["matched", "unknown"],
            "positive and protected cases are required")
    require(cases[0].get("session_id") != cases[1].get("session_id")
            and cases[0].get("agent_run_id") != cases[1].get("agent_run_id"),
            "protected case reused a positive AgentRun")
    for case in cases:
        require(case.get("tools") == TOOLS and case.get("persisted_completed") is True
                and case.get("raw_values_absent") is True
                and all(case.get(key) is not None and case.get(key) == cases[0].get(key)
                        for key in ("ontology_id", "revision", "generation", "activation_version", "digest")),
                "Agent evidence or pinned rule revision differs")
    service = report.get("service_binding", {})
    require(isinstance(service.get("service_id"), int) and service["service_id"] > 0
            and bool(service.get("service_name")) and len(service.get("service_version", "")) == 64,
            "published rule service binding is missing")
    require(report.get("model_calls", []).count("ontology__rule__evaluate") == 2,
            "missing or repeated controlled evaluation call")


def main():
    report = {"result": "failed", "suite": SUITE}
    destination = None

    def checkpoint():
        if destination:
            destination.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")

    try:
        required = {"ADDP_ONLINE_TEST": "1", "ADDP_ONLINE_HOST": "1", "ADDP_ONLINE_HOSTED": "1",
                    "GITHUB_ACTIONS": "true", "RUNNER_OS": "Linux", "POSTGRES_DB": "addp_online"}
        require(all(os.environ.get(k) == v for k, v in required.items()), "dedicated Hosted admission is required")
        artifacts = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]).resolve()
        require(not artifacts.is_relative_to(Path(__file__).resolve().parents[2]), "evidence must be outside checkout")
        destination = artifacts / (SUITE + ".json")
        artifacts.mkdir(parents=True, exist_ok=True)
        tenant = int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"])
        gateway = os.environ["GATEWAY_URL"]
        def client(key):
            return SECURITY.GatewayClient(gateway, os.environ[key], 30)
        preparer = client("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN")
        reader = client("ADDP_ONLINE_ONTOLOGY_READER_ACCESS_TOKEN")
        report["identity"] = API.validate_user_identity(
            API.GatewayClient(os.environ["SYSTEM_URL"], reader.token, 30), tenant, READER_PERMISSIONS)
        governor = client("ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN")
        SECURITY.initialize_governance(governor, tenant)
        run_suite(preparer, reader, client("ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN"),
                  tenant, int(os.environ["ADDP_ONLINE_CONSUMER_ENGINE_ID"]),
                  os.environ["ADDP_ONLINE_TEST_RUN_ID"], report, checkpoint)
        validate_report(report, os.environ["ADDP_ONLINE_TEST_RUN_ID"])
    except Exception as error:
        report["result"] = "failed"
        report["error_type"] = type(error).__name__
        report["error_sha256"] = hashlib.sha256(str(error).encode()).hexdigest()
        failure = re.fullmatch(r"(?:GET|POST|PUT|DELETE) \S+ returned HTTP ([0-9]{3}) \(([a-z0-9_]+)\)", str(error))
        if failure:
            report["http_failure"] = {"status": int(failure[1]), "error_code": failure[2]}
        print("Ontology Agent protection acceptance failed: " + type(error).__name__)
        return 1
    finally:
        checkpoint()
    print(json.dumps(report, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
