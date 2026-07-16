import json
import unittest

from ag_ui.core import ActivitySnapshotEvent, RunAgentInput, RunStartedEvent
from ag_ui.encoder import EventEncoder

from protocol.a2ui import clarification_surface, workflow_dag_surface
from services.interactions import normalize_options


class AGUIProtocolTests(unittest.TestCase):
    def test_chat_openapi_declares_only_sse_success_response(self):
        from main import app

        specification = app.openapi()
        operation = specification["paths"]["/api/v1/agent/chat"]["post"]
        self.assertEqual(
            operation["responses"]["200"]["content"],
            {"text/event-stream": {}},
        )
        self.assertEqual(operation["security"], [{"BearerAuth": []}])
        self.assertEqual(
            specification["components"]["securitySchemes"]["BearerAuth"]["scheme"],
            "bearer",
        )
        self.assertNotIn("security", specification["paths"]["/health"]["get"])

    def test_agent_run_openapi_exposes_owned_run_detail(self):
        from main import app

        specification = app.openapi()
        operation = specification["paths"]["/api/v1/agent/runs/{agent_run_id}"]["get"]
        self.assertEqual(operation["security"], [{"BearerAuth": []}])
        self.assertIn("x-ai-hint", operation)
        self.assertIn("200", operation["responses"])
        self.assertIn("404", operation["responses"])
        run_schema = specification["components"]["schemas"]["AgentRunResponse"]["properties"]
        self.assertIn("metrics", run_schema)
        self.assertIn("context_metrics", run_schema)
        self.assertIn("error_source", run_schema)
        self.assertIn("error_code", run_schema)
        self.assertNotIn("error_type", run_schema)

    def test_agent_run_openapi_exposes_replay_cancel_and_retry_contracts(self):
        from main import app

        specification = app.openapi()
        replay = specification["paths"]["/api/v1/agent/runs/{agent_run_id}/events"]["get"]
        cancel = specification["paths"]["/api/v1/agent/runs/{agent_run_id}/cancel"]["post"]
        retry = specification["paths"]["/api/v1/agent/runs/{agent_run_id}/retry"]["post"]

        self.assertEqual(replay["security"], [{"BearerAuth": []}])
        self.assertEqual(replay["responses"]["200"]["content"], {"text/event-stream": {}})
        self.assertEqual(cancel["security"], [{"BearerAuth": []}])
        self.assertEqual(retry["responses"]["200"]["content"], {"text/event-stream": {}})

    def test_standard_run_input_uses_camel_case_contract(self):
        body = RunAgentInput.model_validate(
            {
                "threadId": "12",
                "runId": "run-1",
                "state": {},
                "messages": [{"id": "m1", "role": "user", "content": "hello"}],
                "tools": [],
                "context": [],
                "forwardedProps": {},
            }
        )
        self.assertEqual(body.thread_id, "12")
        self.assertEqual(body.messages[0].id, "m1")

    def test_sse_encoder_emits_ag_ui_json(self):
        encoded = EventEncoder().encode(RunStartedEvent(thread_id="12", run_id="run-1"))
        self.assertTrue(encoded.startswith("data: "))
        payload = json.loads(encoded.removeprefix("data: ").strip())
        self.assertEqual(payload["type"], "RUN_STARTED")
        self.assertEqual(payload["threadId"], "12")
        self.assertEqual(payload["runId"], "run-1")

    def test_a2ui_workflow_surface_uses_v09_contract(self):
        operations = workflow_dag_surface("surface-1", {"tasks": []})
        self.assertEqual(operations[0]["version"], "v0.9")
        self.assertEqual(operations[0]["createSurface"]["catalogId"], "addp.catalog/v1")
        root = operations[1]["updateComponents"]["components"][0]
        self.assertEqual(root["id"], "root")
        self.assertEqual(root["component"], "WorkflowDag")

        activity = ActivitySnapshotEvent(
            message_id="a1",
            activity_type="a2ui-surface",
            content={"operations": operations},
        )
        payload = json.loads(activity.model_dump_json(by_alias=True))
        self.assertEqual(payload["activityType"], "a2ui-surface")

    def test_clarification_options_preserve_candidate_fact(self):
        candidates = [
            {
                "name": "railway",
                "location": {"locator": "addp://engine/1/path/public/railway?type=table"},
            }
        ]
        options = normalize_options(candidates)
        self.assertEqual(options[0]["label"], "railway")
        self.assertEqual(options[0]["value"], candidates[0]["location"]["locator"])
        self.assertEqual(options[0]["candidate"], candidates[0])

        operations = clarification_surface(
            "surface-2",
            interaction_id="1b842c47-cdf4-4228-af6e-25bfbaa8609b",
            prompt="请选择数据源",
            options=options,
        )
        root = operations[1]["updateComponents"]["components"][0]
        self.assertEqual(root["component"], "ClarificationChoice")
        self.assertEqual(root["options"], options)


if __name__ == "__main__":
    unittest.main()
