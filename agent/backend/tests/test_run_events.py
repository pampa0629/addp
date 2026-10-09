import asyncio
import json
import unittest
import uuid
from types import SimpleNamespace
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
    async def test_checkpoint_consumption_acknowledges_only_after_commit(self):
        from agents.events import AgentEvent
        from api.chat import _persist_checkpoint_event

        committed = asyncio.get_running_loop().create_future()
        order = []

        class Transaction(_Transaction):
            async def __aexit__(inner, *_args):
                self.assertFalse(committed.done())
                order.append("commit")
                return False

        db = SimpleNamespace(begin=lambda: Transaction())

        async def update(*_args, **_kwargs):
            self.assertFalse(committed.done())
            order.append("update")

        event = AgentEvent(kind="checkpoint", payload={"tool_call_id": "create", "checkpoint": {}, "facts": {}}, checkpoint_committed=committed)
        with (patch("api.chat.AsyncSessionLocal", return_value=_SessionContext(db)),
              patch("api.chat.update_run_checkpoint", new=AsyncMock(side_effect=update)),
              patch("api.chat.attach_step_facts", new=AsyncMock()) as facts):
            await _persist_checkpoint_event(
                event, uuid.uuid4(), {"create": uuid.uuid4()},
                request=SimpleNamespace(is_disconnected=AsyncMock(return_value=False)),
                cancellation_signal=asyncio.Event(),
            )
        self.assertEqual(order, ["update", "commit"])
        self.assertTrue(committed.done())
        self.assertIsNone(committed.result())
        facts.assert_awaited_once()

    async def test_checkpoint_update_or_commit_failure_cancels_write_barrier(self):
        from agents.events import AgentEvent
        from api.chat import _persist_checkpoint_event

        for stage in ("update", "commit", "cancel"):
            committed = asyncio.get_running_loop().create_future()

            class Transaction(_Transaction):
                async def __aexit__(inner, *_args):
                    if stage == "commit":
                        raise RuntimeError("fixture_commit_failure")
                    return False

            failure = asyncio.CancelledError() if stage == "cancel" else RuntimeError("fixture_update_failure") if stage == "update" else None
            update = AsyncMock(side_effect=failure)
            db = SimpleNamespace(begin=lambda: Transaction())
            event = AgentEvent(kind="checkpoint", payload={"checkpoint": {}}, checkpoint_committed=committed)
            with (patch("api.chat.AsyncSessionLocal", return_value=_SessionContext(db)),
                  patch("api.chat.update_run_checkpoint", new=update),
                  self.assertRaises(asyncio.CancelledError if stage == "cancel" else RuntimeError)):
                await _persist_checkpoint_event(
                    event, uuid.uuid4(), {},
                    request=SimpleNamespace(is_disconnected=AsyncMock(return_value=False)),
                    cancellation_signal=asyncio.Event(),
                )
            self.assertTrue(committed.cancelled())

    async def test_cancellation_or_disconnect_during_commit_never_acknowledges_write(self):
        from agents.events import AgentEvent
        from api.chat import _persist_checkpoint_event

        for disconnected in (False, True):
            committed = asyncio.get_running_loop().create_future()
            signal = asyncio.Event()

            class Transaction(_Transaction):
                async def __aexit__(inner, *_args):
                    if not disconnected:
                        signal.set()
                    return False

            event = AgentEvent(kind="checkpoint", payload={"checkpoint": {}}, checkpoint_committed=committed)
            db = SimpleNamespace(begin=lambda: Transaction())
            with (patch("api.chat.AsyncSessionLocal", return_value=_SessionContext(db)),
                  patch("api.chat.update_run_checkpoint", new=AsyncMock()),
                  self.assertRaises(ConnectionAbortedError if disconnected else asyncio.CancelledError)):
                await _persist_checkpoint_event(
                    event, uuid.uuid4(), {},
                    request=SimpleNamespace(is_disconnected=AsyncMock(return_value=disconnected)),
                    cancellation_signal=signal,
                )
            self.assertTrue(committed.cancelled())

    async def test_search_failure_is_visible_persisted_and_never_completed(self):
        from ag_ui.core import RunAgentInput
        from agents.events import AgentEvent
        from api.chat import chat

        message = "The index outlet is isolated; search remains unavailable"
        run_id = uuid.uuid4()
        db = _MessageDB()
        entry_db = SimpleNamespace(commit=AsyncMock())
        request = SimpleNamespace(
            state=SimpleNamespace(principal_id=1, tenant_id=1, token="fixture"),
            headers={}, is_disconnected=AsyncMock(return_value=False),
        )
        body = RunAgentInput.model_validate({
            "threadId": "12", "runId": "protocol-1", "state": {}, "tools": [], "context": [], "forwardedProps": {},
            "messages": [{"id": "user-1", "role": "user", "content": "Outdoor"}],
        })

        async def failed_stream(*_args, **_kwargs):
            yield AgentEvent(kind="tool_start", payload={"tool_call_id": "search", "tool_name": "data.search", "args": {"query": "Outdoor"}})
            yield AgentEvent(kind="tool_result", payload={
                "tool_call_id": "search", "tool_name": "data.search", "is_error": True,
                "error_source": "owner", "error_code": "manager_search_isolated",
                "content": json.dumps({"error": {"code": "manager_search_isolated", "message": message}}),
            })
            yield AgentEvent(kind="run_failed", payload={
                "error_source": "owner", "error_code": "manager_search_isolated", "message": message,
            })

        with (
            patch("api.chat._get_owned_session", new=AsyncMock(return_value=SimpleNamespace(summary=""))),
            patch("api.chat._save_user_input", new=AsyncMock(return_value=[])),
            patch("api.chat.create_agent_run", new=AsyncMock(return_value=SimpleNamespace(id=run_id, checkpoint={}))),
            patch("api.chat._load_agent_history", new=AsyncMock(return_value=([{"role": "user", "content": "Outdoor"}], 1))),
            patch("api.chat.AsyncSessionLocal", return_value=_SessionContext(db)),
            patch("api.chat.set_run_context_metrics", new=AsyncMock()),
            patch("api.chat.create_run_step", new=AsyncMock(return_value=SimpleNamespace(id=42))),
            patch("api.chat.complete_run_step", new=AsyncMock()) as complete_step,
            patch("api.chat.set_run_status", new=AsyncMock()) as set_status,
            patch("api.chat.append_run_event", new=AsyncMock(return_value=None)),
            patch("api.chat.refresh_run_metrics", new=AsyncMock()),
            patch("api.chat.maybe_update_summary", new=AsyncMock()),
            patch("api.chat.stream_agent_response", new=failed_stream),
        ):
            response = await chat(request, body, db=entry_db)
            chunks = [chunk async for chunk in response.body_iterator]

        events = [json.loads(line[6:]) for chunk in chunks for line in chunk.splitlines() if line.startswith("data: ")]
        self.assertEqual(events[-1]["type"], "RUN_ERROR")
        self.assertEqual(events[-1]["code"], "manager_search_isolated")
        self.assertEqual(events[-1]["message"], message)
        self.assertNotIn("RUN_FINISHED", [event["type"] for event in events])
        self.assertEqual(events[-2]["snapshot"]["status"], "failed")
        self.assertEqual(complete_step.await_args.kwargs["status"], "failed")
        self.assertEqual(set_status.await_count, 1)
        self.assertEqual(set_status.await_args.kwargs["status"], "failed")
        self.assertEqual(set_status.await_args.kwargs["error_source"], "owner")
        self.assertEqual(set_status.await_args.kwargs["error_code"], "manager_search_isolated")
        self.assertEqual(db.added[0].content, message)
        self.assertEqual(db.added[0].parts, [{"type": "text", "text": message}])

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
