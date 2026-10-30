import asyncio
import json
import unittest
import uuid
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from ag_ui.core import StateSnapshotEvent

from protocol.a2ui import table_preview_surface, transfer_config_check_presentation, transfer_config_check_surface
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
        await self._assert_search_failure_order(interleaved=False)

    async def test_failure_after_presentation_retains_event_order_and_complete_text(self):
        await self._assert_search_failure_order(interleaved=True)

    async def _assert_search_failure_order(self, *, interleaved):
        from ag_ui.core import RunAgentInput
        from agents.events import AgentEvent
        from api.chat import chat

        message = "The index outlet is isolated; search remains unavailable"
        run_id = uuid.uuid4()
        db = _MessageDB()
        entry_db = SimpleNamespace(commit=AsyncMock())
        request = SimpleNamespace(
            state=SimpleNamespace(principal_id=1, tenant_id=1, token="fixture"),
            headers={"accept-language": "en-US"}, is_disconnected=AsyncMock(return_value=False),
        )
        body = RunAgentInput.model_validate({
            "threadId": "12", "runId": "protocol-1", "state": {}, "tools": [], "context": [], "forwardedProps": {},
            "messages": [{"id": "user-1", "role": "user", "content": "Outdoor"}],
        })

        async def failed_stream(*_args, **_kwargs):
            self.assertEqual(_kwargs["accept_language"], "en-US")
            if interleaved:
                yield AgentEvent(kind="text", payload={"delta": "已经获取的结果"})
                yield AgentEvent(kind="presentation", payload={"kind": "table_preview", "columns": ["name"],
                    "rows": [{"name": "railway"}], "total": 1, "truncated": False})
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
            patch("api.chat._get_owned_session", new=AsyncMock(return_value=SimpleNamespace(summary="", archived=False))),
            patch("api.chat._save_user_input", new=AsyncMock(return_value=[])),
            patch("api.chat.create_agent_run", new=AsyncMock(return_value=SimpleNamespace(id=run_id, checkpoint={}))),
            patch("api.chat._load_agent_history", new=AsyncMock(return_value=([{"role": "user", "content": "Outdoor"}], 1))),
            patch("api.chat.list_session_drafts", new=AsyncMock(return_value=[])),
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
        if interleaved:
            self.assertEqual(db.added[0].content, "已经获取的结果\n\n" + message)
            self.assertEqual([part["type"] for part in db.added[0].parts], ["text", "presentation_ref", "text"])
            self.assertEqual(db.added[0].parts[-1], {"type": "text", "text": "\n\n" + message})
            visible_events = [event["type"] for event in events if event["type"] in {"TEXT_MESSAGE_CONTENT", "ACTIVITY_SNAPSHOT"}]
            self.assertEqual(visible_events, ["TEXT_MESSAGE_CONTENT", "ACTIVITY_SNAPSHOT", "TEXT_MESSAGE_CONTENT"])
        else:
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

    async def test_formal_report_snapshot_persists_and_replays_without_owner_identity(self):
        from api.chat import _save_assistant_message

        report = {"status": "unknown", "observed_at": "2026-10-12T00:00:00Z", "checked": ["request_shape"],
                  "not_checked": ["business_rows"], "runtime_checks_required": ["per_value_encoding"], "diagnostics": [],
                  "principal_id": "private-user", "binding_hash": "private-binding"}
        content = {"operations": transfer_config_check_surface("surface:check", transfer_config_check_presentation(report))}
        parts = [{"type": "presentation_ref", "protocol": "a2ui", "catalog_id": "addp.catalog/v1",
                  "surface_id": "surface:check", "activity_type": "a2ui-surface", "content": content}]
        message_db = _MessageDB()
        with (patch("api.chat.AsyncSessionLocal", return_value=_SessionContext(message_db)),
              patch("api.chat.maybe_update_summary", new=AsyncMock())):
            await _save_assistant_message(session_id=12, message_id="check-message", content="草稿检查完成", parts=parts)
        event = await append_run_event(_DB(), agent_run_id=uuid.uuid4(), protocol_invocation_id="check-protocol",
            event_payload={"type": "ACTIVITY_SNAPSHOT", "messageId": "check-activity", "activityType": "a2ui-surface", "content": content})
        self.assertEqual(message_db.added[0].parts[0]["content"], event.payload["content"])
        self.assertEqual(json.loads(encode_replayed_event(event).split("data: ", 1)[1])["content"], content)
        self.assertNotIn("private-", json.dumps(message_db.added[0].parts))

    async def test_chat_projects_runtime_report_into_sse_and_saved_message(self):
        from ag_ui.core import RunAgentInput
        from agents.events import AgentEvent
        from api.chat import chat

        db = _MessageDB()
        run_id = uuid.uuid4()
        request = SimpleNamespace(state=SimpleNamespace(principal_id=1, tenant_id=1, token="fixture"),
                                  headers={}, is_disconnected=AsyncMock(return_value=False))
        body = RunAgentInput.model_validate({"threadId": "12", "runId": "check-protocol", "state": {}, "tools": [],
            "context": [], "forwardedProps": {}, "messages": [{"id": "check-user", "role": "user", "content": "只检查草稿"}]})
        presentation = transfer_config_check_presentation({"status": "unknown", "observed_at": "2026-10-12T00:00:00Z",
            "checked": [], "not_checked": ["business_rows"], "runtime_checks_required": [], "diagnostics": []})

        async def stream(*_args, **_kwargs):
            yield AgentEvent(kind="text", payload={"delta": "检查提示"})
            yield AgentEvent(kind="presentation", payload=presentation)
            yield AgentEvent(kind="text", payload={"delta": "只展示草稿，不创建"})

        with (patch("api.chat._get_owned_session", new=AsyncMock(return_value=SimpleNamespace(summary="", archived=False))),
              patch("api.chat._save_user_input", new=AsyncMock(return_value=[])),
              patch("api.chat.create_agent_run", new=AsyncMock(return_value=SimpleNamespace(id=run_id, checkpoint={}))),
              patch("api.chat._load_agent_history", new=AsyncMock(return_value=([{"role": "user", "content": "只检查草稿"}], 1))),
              patch("api.chat.list_session_drafts", new=AsyncMock(return_value=[])),
              patch("api.chat.AsyncSessionLocal", return_value=_SessionContext(db)),
              patch("api.chat.set_run_context_metrics", new=AsyncMock()),
              patch("api.chat.set_run_status", new=AsyncMock()) as status,
              patch("api.chat.append_run_event", new=AsyncMock(return_value=None)),
              patch("api.chat.refresh_run_metrics", new=AsyncMock()),
              patch("api.chat.maybe_update_summary", new=AsyncMock()),
              patch("api.chat.stream_agent_response", new=stream)):
            response = await chat(request, body, db=SimpleNamespace(commit=AsyncMock()))
            chunks = [chunk async for chunk in response.body_iterator]
        events = [json.loads(line[6:]) for chunk in chunks for line in chunk.splitlines() if line.startswith("data: ")]
        activity = next(event for event in events if event["type"] == "ACTIVITY_SNAPSHOT")
        part = next(part for part in db.added[0].parts if part["type"] == "presentation_ref")
        self.assertEqual(part["content"], activity["content"])
        self.assertEqual(part["content"]["operations"][1]["updateComponents"]["components"][0]["report"]["status"], "unknown")
        self.assertEqual([part["type"] for part in db.added[0].parts], ["text", "presentation_ref", "text"])
        self.assertEqual([part["text"] for part in db.added[0].parts if part["type"] == "text"], ["检查提示", "只展示草稿，不创建"])
        self.assertEqual(db.added[0].content, "检查提示只展示草稿，不创建")
        self.assertEqual(status.await_args.kwargs["status"], "completed")

    async def test_runtime_plan_has_identical_stream_and_persistent_message_projection(self):
        from ag_ui.core import RunAgentInput
        from agents.events import AgentEvent
        from api.chat import chat
        run_id=uuid.uuid4()
        todos=[{'content':'Inspect','status':'in_progress'},{'content':'Validate','status':'pending'}]
        updated_todos=[{'content':'Inspect','status':'completed'},{'content':'Validate','status':'pending'}]
        db=_MessageDB()
        request=SimpleNamespace(state=SimpleNamespace(principal_id=1,tenant_id=1,token='fixture'),headers={},is_disconnected=AsyncMock(return_value=False))
        body=RunAgentInput.model_validate(dict(threadId='12',runId='plan-stream',state={},messages=[{'id':'u','role':'user','content':'Plan a task'}],tools=[],context=[],forwardedProps={}))
        async def stream(*args,**kwargs):
            yield AgentEvent(kind='plan',payload={'todos':todos})
            yield AgentEvent(kind='text',payload={'delta':'Progress: '})
            yield AgentEvent(kind='plan',payload={'todos':updated_todos})
            yield AgentEvent(kind='text',payload={'delta':'Need your input'})
        with (patch('api.chat._get_owned_session',new=AsyncMock(return_value=SimpleNamespace(summary='',archived=False))),
              patch('api.chat._save_user_input',new=AsyncMock(return_value=[])),
              patch('api.chat.create_agent_run',new=AsyncMock(return_value=SimpleNamespace(id=run_id,checkpoint={}))),
              patch('api.chat._load_agent_history',new=AsyncMock(return_value=([{'role':'user','content':'Plan a task'}],1))),
              patch('api.chat.list_session_drafts',new=AsyncMock(return_value=[])),
              patch('api.chat.AsyncSessionLocal',return_value=_SessionContext(db)),
              patch('api.chat.set_run_context_metrics',new=AsyncMock()),
              patch('api.chat.set_run_status',new=AsyncMock()),
              patch('api.chat.append_run_event',new=AsyncMock(return_value=None)) as persisted,
              patch('api.chat.refresh_run_metrics',new=AsyncMock()),
              patch('api.chat.maybe_update_summary',new=AsyncMock()),
              patch('api.chat.stream_agent_response',new=stream)):
            response=await chat(request,body,db=SimpleNamespace(commit=AsyncMock()))
            chunks=[chunk async for chunk in response.body_iterator]
        events=[json.loads(line[6:]) for chunk in chunks for line in chunk.splitlines() if line.startswith('data: ')]
        plan_events=[item for item in events if item['type']=='ACTIVITY_SNAPSHOT']
        self.assertEqual(len(plan_events),2)
        self.assertEqual(len({item['messageId'] for item in plan_events}),1)
        event=plan_events[-1]
        self.assertEqual(event['activityType'],'addp-plan')
        self.assertEqual(event['content'],{'agent_run_id':str(run_id),'todos':updated_todos})
        self.assertEqual([part['type'] for part in db.added[0].parts],['presentation_ref','text'])
        self.assertEqual(db.added[0].parts[0]['content'],event['content'])
        self.assertEqual(db.added[0].parts[0]['surface_id'],event['messageId'])
        self.assertEqual(db.added[0].parts[0]['protocol'],'ag-ui')
        self.assertEqual(db.added[0].parts[1]['text'],'Progress: Need your input')
        self.assertEqual(sum(call.kwargs['event_payload'].get('activityType')=='addp-plan' for call in persisted.await_args_list),2)
        self.assertEqual(todos[1]['status'],'pending')

    async def test_completed_precheck_mints_exact_scoped_snapshot_into_stream_and_history(self):
        from ag_ui.core import RunAgentInput
        from agents.events import AgentEvent
        from api.chat import chat
        from services.drafts import draft_snapshot
        from tests.test_drafts import checked_step

        step = checked_step()
        snapshot = draft_snapshot(step)
        db = _MessageDB()
        request = SimpleNamespace(state=SimpleNamespace(principal_id=1, tenant_id=1, token="fixture"),
                                  headers={}, is_disconnected=AsyncMock(return_value=False))
        body = RunAgentInput.model_validate({"threadId": "12", "runId": "snapshot-protocol", "state": {}, "tools": [],
            "context": [], "forwardedProps": {}, "messages": [{"id": "input", "role": "user", "content": "检查草稿"}]})

        async def stream(*_args, **kwargs):
            self.assertEqual(kwargs["draft_catalog"], [])
            yield AgentEvent(kind="tool_start", payload={"tool_call_id": "check", "tool_name": step.tool_name, "args": step.input_data})
            yield AgentEvent(kind="tool_result", payload={"tool_call_id": "check", "tool_name": step.tool_name, "content": "{}", "is_error": False})
            yield AgentEvent(kind="text", payload={"delta": "只解释关键检查原因"})

        with (patch("api.chat._get_owned_session", new=AsyncMock(return_value=SimpleNamespace(summary="", archived=False))),
              patch("api.chat._save_user_input", new=AsyncMock(return_value=[])),
              patch("api.chat.create_agent_run", new=AsyncMock(return_value=SimpleNamespace(id=uuid.uuid4(), checkpoint={}))),
              patch("api.chat._load_agent_history", new=AsyncMock(return_value=([{"role": "user", "content": "检查草稿"}], 1))),
              patch("api.chat.list_session_drafts", new=AsyncMock(return_value=[])),
              patch("api.chat.AsyncSessionLocal", return_value=_SessionContext(db)),
              patch("api.chat.set_run_context_metrics", new=AsyncMock()),
              patch("api.chat.set_run_status", new=AsyncMock()),
              patch("api.chat.create_run_step", new=AsyncMock(return_value=step)),
              patch("api.chat.complete_run_step", new=AsyncMock()) as complete,
              patch("api.chat.read_checked_draft_step", new=AsyncMock(return_value=snapshot)) as restore,
              patch("api.chat.append_run_event", new=AsyncMock(return_value=None)),
              patch("api.chat.refresh_run_metrics", new=AsyncMock()),
              patch("api.chat.maybe_update_summary", new=AsyncMock()),
              patch("api.chat.stream_agent_response", new=stream)):
            response = await chat(request, body, db=SimpleNamespace(commit=AsyncMock()))
            chunks = [chunk async for chunk in response.body_iterator]
        self.assertEqual(complete.await_args.kwargs["status"], "completed")
        self.assertEqual(restore.await_args.kwargs, dict(session_id=12, user_id=1, tenant_id=1))
        self.assertEqual(restore.await_args.args[1], step.id)
        events = [json.loads(line[6:]) for chunk in chunks for line in chunk.splitlines() if line.startswith("data: ")]
        activity = next(event for event in events if event["type"] == "ACTIVITY_SNAPSHOT")
        part = next(part for part in db.added[0].parts if part["type"] == "presentation_ref")
        self.assertEqual(part["content"], activity["content"])
        self.assertEqual(replay_payload(activity)["content"], activity["content"])
        component = activity["content"]["operations"][1]["updateComponents"]["components"][0]
        self.assertEqual(component["reference"], snapshot["reference"])
        self.assertEqual(json.loads(component["parameters"]), step.input_data)
        self.assertEqual(component["mappings"][-1]["target_type"], "json")
        self.assertEqual([part["type"] for part in db.added[0].parts], ["presentation_ref", "text"])
        self.assertNotIn("fixture-user", json.dumps(part))

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
