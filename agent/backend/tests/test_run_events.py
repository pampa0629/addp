import json
import unittest
import uuid

from ag_ui.core import StateSnapshotEvent

from services.run_events import append_run_event, encode_replayed_event, replay_payload


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
