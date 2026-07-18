import json
import unittest
import uuid
from unittest.mock import AsyncMock, patch

from ag_ui.core import StateSnapshotEvent

from protocol.a2ui import table_preview_surface
from services.messages import bounded_message_parts
from services.run_events import RUN_EVENT_MAX_BYTES, append_run_event, encode_replayed_event, replay_payload


class _Result:
    def __init__(self, value):
        self.value = value

    def scalar_one(self):
        return self.value


class _DB:
    def __init__(self):
        self.added = []
        self.execute_count = 0
        self.flush_count = 0

    async def execute(self, _statement):
        self.execute_count += 1
        return _Result(0)

    def add(self, value):
        self.added.append(value)

    async def flush(self):
        self.flush_count += 1


class _Transaction:
    async def __aenter__(self):
        return self

    async def __aexit__(self, _type, _value, _traceback):
        return False


class _SessionContext:
    def __init__(self, db):
        self.db = db

    async def __aenter__(self):
        return self.db

    async def __aexit__(self, _type, _value, _traceback):
        return False


class _MessageDB:
    def __init__(self):
        self.added = []

    def begin(self):
        return _Transaction()

    def add(self, value):
        self.added.append(value)


class AgentRunEventTests(unittest.IsolatedAsyncioTestCase):
    def test_tool_arguments_are_not_replayable(self):
        self.assertIsNone(
            replay_payload(
                {
                    "type": "TOOL_CALL_ARGS",
                    "toolCallId": "call-1",
                    "delta": '{"locator":"addp://engine/8/path/public/railway"}',
                }
            )
        )

    def test_tool_result_is_replaced_with_safe_progress_text(self):
        payload = replay_payload(
            {
                "type": "TOOL_CALL_RESULT",
                "messageId": "message-1",
                "toolCallId": "call-1",
                "content": '{"locator":"addp://engine/8/path/public/railway","rows":[1,2]}',
                "role": "tool",
            }
        )

        self.assertEqual(payload["content"], "工具调用已完成；详情见运行审计")
        self.assertNotIn("locator", json.dumps(payload, ensure_ascii=False))

    def test_json_mode_ag_ui_payload_is_replayable(self):
        payload = StateSnapshotEvent(snapshot={"status": "running"}).model_dump(
            mode="json",
            by_alias=True,
            exclude_none=True,
        )

        self.assertEqual(replay_payload(payload)["type"], "STATE_SNAPSHOT")

    def test_a2ui_message_part_and_replay_event_keep_the_same_surface(self):
        content = {
            "operations": table_preview_surface(
                "surface-table",
                {"columns": ["name"], "rows": [{"name": "railway"}], "total": 1, "truncated": False},
            )
        }
        parts = bounded_message_parts(
            [
                {
                    "type": "presentation_ref",
                    "protocol": "a2ui",
                    "catalog_id": "addp.catalog/v1",
                    "surface_id": "surface-table",
                    "activity_type": "a2ui-surface",
                    "content": content,
                }
            ]
        )
        replayed = replay_payload(
            {
                "type": "ACTIVITY_SNAPSHOT",
                "messageId": "activity-1",
                "activityType": "a2ui-surface",
                "content": content,
            }
        )

        self.assertEqual(parts[0]["content"], replayed["content"])

    async def test_a2ui_surface_persists_identically_in_message_and_run_event(self):
        from api.chat import _save_assistant_message

        content = {
            "operations": table_preview_surface(
                "surface-table",
                {"columns": ["name"], "rows": [{"name": "railway"}], "total": 1, "truncated": False},
            )
        }
        message_parts = [
            {
                "type": "presentation_ref",
                "protocol": "a2ui",
                "catalog_id": "addp.catalog/v1",
                "surface_id": "surface-table",
                "activity_type": "a2ui-surface",
                "content": content,
            }
        ]
        message_db = _MessageDB()
        with (
            patch("api.chat.AsyncSessionLocal", return_value=_SessionContext(message_db)),
            patch("api.chat.maybe_update_summary", new=AsyncMock()),
        ):
            await _save_assistant_message(
                session_id=12,
                message_id="message-1",
                content="预览完成",
                parts=message_parts,
            )
        run_event_db = _DB()
        event = await append_run_event(
            run_event_db,
            agent_run_id=uuid.uuid4(),
            protocol_invocation_id="protocol-1",
            event_payload={
                "type": "ACTIVITY_SNAPSHOT",
                "messageId": "activity-1",
                "activityType": "a2ui-surface",
                "content": content,
            },
        )

        self.assertEqual(message_db.added[0].parts[0]["content"], event.payload["content"])
        self.assertIsNot(message_db.added[0].parts, message_parts)

    def test_replay_event_rejects_payload_over_total_byte_limit(self):
        with self.assertRaisesRegex(ValueError, "run event exceeds"):
            replay_payload(
                {
                    "type": "ACTIVITY_SNAPSHOT",
                    "messageId": "activity-1",
                    "activityType": "a2ui-surface",
                    "content": {"value": "x" * RUN_EVENT_MAX_BYTES},
                }
            )

    async def test_appended_event_has_run_local_sequence_and_sse_id(self):
        db = _DB()
        event = await append_run_event(
            db,
            agent_run_id=uuid.uuid4(),
            protocol_invocation_id="protocol-1",
            event_payload={"type": "STATE_SNAPSHOT", "snapshot": {"status": "running"}},
        )

        self.assertEqual(event.sequence, 1)
        self.assertEqual(event.event_type, "STATE_SNAPSHOT")
        self.assertEqual(db.flush_count, 1)
        self.assertTrue(encode_replayed_event(event).startswith("id: 1\ndata: "))


if __name__ == "__main__":
    unittest.main()
