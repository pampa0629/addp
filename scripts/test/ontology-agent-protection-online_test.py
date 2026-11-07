"""Bounded protocol and evidence checks; these doubles do not claim T4."""

import copy
import importlib
import io
import json
import unittest
import urllib.error
import urllib.request
from unittest.mock import patch

ONLINE = importlib.import_module("scripts.test.ontology-agent-protection-online")
PIN = {"ontology_id": "test_ontology", "revision": 2, "generation": "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
       "activation_version": 3, "digest": "a" * 64}


class AgentResponse(io.BytesIO):
    status = 200

    def __init__(self, events):
        super().__init__(b"".join(b"data: " + json.dumps(e).encode() + b"\n\n" for e in events))
        from email.message import Message
        self.headers = Message()
        self.headers["Content-Type"] = "text/event-stream"


class AgentClient:
    base_url = "http://127.0.0.1:8000"
    token = "fixture-only"

    def __init__(self, defect=None):
        self.defect = defect

    def request(self, method, path, expected, body=None):
        self.last_request = body
        if path.endswith("/sessions"):
            value = {"id": 1}
        elif path.endswith("/runs"):
            value = {"data": [{"id": "run"}]}
        else:
            value = {"status": "waiting" if self.defect == "waiting" else "completed",
                     "skill_name": "ontology-exploration", "error_code": None, "id": "run",
                     "steps": [{"step_type": "tool_call", "tool_name": name, "status": "completed", "error_code": None}
                               for name in ONLINE.TOOLS]}
        return ONLINE.API.Response(200, value)


def trace(outcome="unknown"):
    return [
        *[{"type": "TOOL_CALL_START", "toolCallName": name} for name in ONLINE.TOOLS],
        {"type": "TOOL_CALL_RESULT", "content": json.dumps({**PIN, "rule_id": ONLINE.RULE, "outcome": outcome})},
        {"type": "TEXT_MESSAGE_CONTENT", "delta": (
            "现有业务证据不足，无法判断该记录是否符合规则。依据本体 test_ontology 的修订 2，规则 eligible_record。"
            if outcome == "unknown" else "该记录符合规则。依据本体 test_ontology 的修订 2，规则 eligible_record。")},
        {"type": "RUN_FINISHED"},
    ]


class ControlledModelTests(unittest.TestCase):
    def tools(self, *names):
        return [{"type": "function", "function": {"name": n, "parameters": {}}} for n in names]

    def test_model_copies_observed_binding_and_never_evaluates(self):
        model = ONLINE.ControlledModel("test_ontology")
        offered = self.tools("ontology__classes__list", "ontology__class__context", "ontology__rule__evaluate")
        messages = []
        result = model.reply({"tools": offered, "messages": messages})
        self.assertEqual(result["tool_calls"][0]["function"]["name"], "ontology__classes__list")
        messages.append({"role": "tool", "content": json.dumps({**PIN, "classes": [{"id": "record"}]})})
        result = model.reply({"tools": offered, "messages": messages})
        call = result["tool_calls"][0]["function"]
        args = json.loads(call["arguments"])
        self.assertEqual(call["name"], "ontology__class__context")
        self.assertEqual(args["generation"], PIN["generation"])
        messages.append({"role": "tool", "content": json.dumps({
            **PIN, "class": {"id": "record"}, "rules": [{"id": ONLINE.RULE, "business_evaluable": True}],
        })})
        result = model.reply({"tools": offered, "messages": messages})
        call = result["tool_calls"][0]["function"]
        self.assertEqual(call["name"], "ontology__rule__evaluate")
        self.assertEqual(json.loads(call["arguments"]), {
            **{k: PIN[k] for k in ("ontology_id", "revision", "generation", "activation_version")},
            "rule_id": ONLINE.RULE, "object_id": ONLINE.OBJECT_ID,
        })
        self.assertIsNone(result["content"])

    def test_model_refuses_foreign_or_non_evaluable_context(self):
        offered = self.tools("ontology__rule__evaluate")
        for context in (
            {**PIN, "ontology_id": "other", "class": {}, "rules": [{"id": ONLINE.RULE, "business_evaluable": True}]},
            {**PIN, "class": {}, "rules": [{"id": ONLINE.RULE}]},
        ):
            with self.subTest(context=context):
                with self.assertRaises(ONLINE.API.SuiteError):
                    ONLINE.ControlledModel("test_ontology").reply({
                        "tools": offered, "messages": [{"role": "tool", "content": json.dumps(context)}],
                    })

    def test_provider_requires_its_ephemeral_credential(self):
        model = ONLINE.ControlledModel("test_ontology")
        with ONLINE.model_server(model) as (endpoint, credential):
            body = json.dumps({"model": "controlled-ontology", "tools": self.tools("RouteDecision")}).encode()
            req = urllib.request.Request(endpoint + "/chat/completions", data=body, method="POST",
                                         headers={"Content-Type": "application/json"})
            with self.assertRaises(urllib.error.HTTPError) as failed:
                urllib.request.urlopen(req, timeout=2)
            self.assertEqual(failed.exception.code, 403)
            failed.exception.close()
            self.assertEqual(model.calls, [])
            req.add_header("Authorization", "Bearer " + credential)
            with urllib.request.urlopen(req, timeout=2) as response:
                value = json.load(response)
            args = json.loads(value["choices"][0]["message"]["tool_calls"][0]["function"]["arguments"])
            self.assertEqual(args["skill"], "ontology-exploration")


class AgentEvidenceTests(unittest.TestCase):
    def test_report_rejects_incomplete_or_reused_evidence(self):
        cases = [{**PIN, "session_id": i, "agent_run_id": f"run-{i}", "outcome": outcome, "tools": ONLINE.TOOLS,
                  "persisted_completed": True, "raw_values_absent": True}
                 for i, outcome in enumerate(("matched", "unknown"), 1)]
        report = {"schema_version": "addp.ontology-agent-protection/v1", "suite": ONLINE.SUITE,
                  "run_id": "test-run", "result": "passed", "model_mode": "controlled_openai_compatible",
                  "typed_mask_observed": True, "unknown_fact_read_empty": True,
                  "owned_resources_retired": True, "cases": cases,
                  "model_calls": ["ontology__rule__evaluate"] * 2,
                  "service_binding": {"service_id": 42, "service_name": "private-fixture", "service_version": "a" * 64}}
        ONLINE.validate_report(report, "test-run")
        for defect in ("run_id", "typed_mask_observed", "unknown_fact_read_empty", "owned_resources_retired",
                       "session_id", "agent_run_id", "revision", "digest", "tools", "model_calls", "service_binding"):
            with self.subTest(defect=defect):
                invalid = copy.deepcopy(report)
                if defect in ("session_id", "agent_run_id"):
                    invalid["cases"][1][defect] = invalid["cases"][0][defect]
                elif defect in ("revision", "digest", "tools"):
                    invalid["cases"][1][defect] = None
                else:
                    invalid.pop(defect)
                with self.assertRaises(ONLINE.API.SuiteError):
                    ONLINE.validate_report(invalid, "test-run")

    def test_unknown_requires_formal_result_version_and_persisted_steps(self):
        with patch.object(ONLINE.urllib.request, "urlopen", return_value=AgentResponse(trace())):
            receipt = ONLINE.run_agent(AgentClient(), "test_ontology", "unknown", PIN)
        self.assertEqual(receipt["outcome"], "unknown")
        self.assertTrue(receipt["persisted_completed"])

    def test_finished_sse_does_not_replace_owner_or_persistence_evidence(self):
        for defect in ("missing_result", "wrong_ontology", "wrong_revision", "wrong_digest", "matched", "raw_value", "waiting", "extra_tool"):
            with self.subTest(defect=defect):
                events = copy.deepcopy(trace())
                if defect == "missing_result":
                    events = [e for e in events if e["type"] != "TOOL_CALL_RESULT"]
                elif defect in ("wrong_ontology", "wrong_revision", "wrong_digest", "matched"):
                    result = json.loads(events[3]["content"])
                    key, value = {"wrong_ontology": ("ontology_id", "other"), "wrong_revision": ("revision", 9), "wrong_digest": ("digest", "b" * 64),
                                  "matched": ("outcome", "matched")}[defect]
                    result[key] = value
                    events[3]["content"] = json.dumps(result)
                elif defect == "raw_value":
                    events[4]["delta"] += ONLINE.OBJECT_ID
                elif defect == "extra_tool":
                    events.insert(0, {"type": "TOOL_CALL_START", "toolCallName": "data.preview"})
                with patch.object(ONLINE.urllib.request, "urlopen", return_value=AgentResponse(events)):
                    with self.assertRaises(ONLINE.API.SuiteError):
                        ONLINE.run_agent(AgentClient(defect), "test_ontology", "unknown", PIN)

    def test_rule_binding_has_no_hypothetical_inputs_or_credentials(self):
        d = ONLINE.definition({"id": 42, "service_name": "private-fixture"}, "a" * 64)
        binding = d["rules"][0]["service_binding"]
        self.assertEqual(binding["service_id"], 42)
        self.assertEqual(binding["service_version"], "a" * 64)
        self.assertEqual(binding["inputs"], [{"variable": "status", "field": "status", "conversion": "string"}])
        self.assertNotIn("scope", d)


if __name__ == "__main__":
    unittest.main()
