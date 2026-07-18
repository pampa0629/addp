import json
import unittest

from ag_ui.core import ActivitySnapshotEvent, RunAgentInput, RunStartedEvent
from ag_ui.encoder import EventEncoder

from protocol.a2ui import (
    approval_request_surface,
    clarification_surface,
    map_view_surface,
    preview_presentations,
    resource_picker_surface,
    table_preview_surface,
    workflow_dag_surface,
)
from services.interactions import normalize_options


class AGUIProtocolTests(unittest.TestCase):
    def test_workflow_run_step_projection_does_not_persist_workflow_payload(self):
        from api.chat import _tool_input_projection

        projection = _tool_input_projection(
            "workflow.run",
            {
                "workflow_engine_id": 20,
                "workflow_definition": {"tasks": [{"id": "secret-task", "params": {"secret": "value"}}]},
                "engine_specific": {"runtime_secret": "value"},
            },
        )
        self.assertEqual(projection["workflow_engine_id"], 20)
        self.assertEqual(projection["task_count"], 1)
        self.assertNotIn("workflow_definition", projection)
        self.assertNotIn("engine_specific", projection)

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

    def test_owner_approval_surface_only_exposes_projection(self):
        operations = approval_request_surface(
            "surface-3",
            interaction_id="1b842c47-cdf4-4228-af6e-25bfbaa8609b",
            owner="develop",
            owner_interaction_id="46ea0d75-b9bc-4b25-8d4a-441947081813",
            open_url="/develop/approvals/46ea0d75-b9bc-4b25-8d4a-441947081813",
            request_fingerprint="a" * 64,
            request_summary={"workflow_engine_id": 20, "task_count": 2},
            expires_at="2026-07-17T10:15:00Z",
        )
        root = operations[1]["updateComponents"]["components"][0]
        self.assertEqual(root["component"], "ApprovalRequest")
        self.assertEqual(root["requestSummary"]["task_count"], 2)
        self.assertNotIn("workflow_definition", root)

    def test_preview_presentations_are_bounded_and_require_explicit_wgs84(self):
        presentations = preview_presentations(
            {
                "columns": ["name", "shape", "details"],
                "rows": [
                    {
                        "name": "railway",
                        "shape": {
                            "type": "LineString",
                            "coordinates": [[104.0, 30.0], [104.1, 30.1]],
                            "bbox": [104.0, 30.0, 104.1, 30.1],
                        },
                        "details": {"secret": "not-a-scalar"},
                    }
                ],
                "total": 2,
                "geometry_column": "shape",
                "source_crs": "EPSG:4326",
            }
        )
        self.assertEqual([item["kind"] for item in presentations], ["table_preview", "map_view"])
        self.assertIsNone(presentations[0]["rows"][0]["shape"])
        self.assertIsNone(presentations[0]["rows"][0]["details"])
        self.assertTrue(presentations[0]["truncated"])
        self.assertEqual(presentations[1]["features"][0]["geometry"]["type"], "LineString")
        self.assertNotIn("bbox", presentations[1]["features"][0]["geometry"])
        self.assertEqual(presentations[1]["features"][0]["properties"], {"name": "railway"})

        non_wgs84 = preview_presentations(
            {
                "columns": ["shape"],
                "rows": [{"shape": {"type": "Point", "coordinates": [500000, 3300000]}}],
                "total": 1,
                "geometry_column": "shape",
                "source_crs": "EPSG:32650",
            }
        )
        self.assertEqual([item["kind"] for item in non_wgs84], ["table_preview"])

    def test_new_catalog_surfaces_only_expose_bounded_projection(self):
        table = table_preview_surface(
            "surface-table",
            {"columns": ["name"], "rows": [{"name": "railway"}], "total": 1, "truncated": False},
        )
        self.assertEqual(table[1]["updateComponents"]["components"][0]["component"], "TablePreview")

        map_surface = map_view_surface(
            "surface-map",
            {
                "crs": "EPSG:4326",
                "features": [
                    {
                        "type": "Feature",
                        "geometry": {"type": "Point", "coordinates": [104, 30]},
                        "properties": {},
                    }
                ],
                "height": 360,
                "truncated": False,
            },
        )
        map_root = map_surface[1]["updateComponents"]["components"][0]
        self.assertEqual(map_root["component"], "MapView")
        self.assertNotIn("url", map_root)

        resource = resource_picker_surface(
            "surface-resource",
            interaction_id="1b842c47-cdf4-4228-af6e-25bfbaa8609b",
            prompt="请选择铁路数据",
            options=[
                {
                    "label": "public.railway",
                    "value": "addp://engine/8/path/public/railway?type=table&item_id=60",
                    "candidate": {
                        "locator": "addp://engine/8/path/public/railway?type=table&item_id=60",
                        "engine_id": 8,
                        "full_name": "public.railway",
                        "column_metadata": [{"name": "secret"}],
                    },
                }
            ],
        )
        candidate = resource[1]["updateComponents"]["components"][0]["options"][0]["candidate"]
        self.assertEqual(candidate["full_name"], "public.railway")
        self.assertNotIn("column_metadata", candidate)

    def test_map_projection_applies_coordinate_limit_to_the_whole_surface(self):
        coordinates = [[float(index), 30.0] for index in range(1500)]
        presentations = preview_presentations(
            {
                "columns": ["shape"],
                "rows": [
                    {"shape": {"type": "LineString", "coordinates": coordinates}},
                    {"shape": {"type": "LineString", "coordinates": coordinates}},
                ],
                "total": 2,
                "geometry_column": "shape",
                "source_crs": "EPSG:4326",
            }
        )

        map_presentation = presentations[1]
        self.assertEqual(len(map_presentation["features"]), 1)
        self.assertTrue(map_presentation["truncated"])

    def test_surface_rejects_total_payload_over_byte_limit(self):
        with self.assertRaisesRegex(ValueError, "A2UI surface exceeds"):
            table_preview_surface(
                "surface-large",
                {
                    "columns": [f"column_{index}" for index in range(50)],
                    "rows": [
                        {f"column_{index}": "x" * 2000 for index in range(50)}
                        for _ in range(100)
                    ],
                    "total": 100,
                    "truncated": False,
                },
            )


if __name__ == "__main__":
    unittest.main()
