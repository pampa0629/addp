import asyncio
import unittest
import uuid
from contextlib import ExitStack, asynccontextmanager
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from ag_ui.core import RunAgentInput

from agents.events import text_event
from api.chat import chat
from models.run import AgentRun
from services.runtime import run_cancellation_registry
from tests.test_run_events import _MessageDB, _Result, _SessionContext


class StreamingPersistenceTests(unittest.IsolatedAsyncioTestCase):
    @asynccontextmanager
    async def invocation(self, source):
        run = SimpleNamespace(id=uuid.uuid4(), checkpoint={}, status='running')

        class DB(_MessageDB):
            async def execute(self, statement):
                if statement.column_descriptions[0].get('entity') is AgentRun:
                    return _Result(run)
                return await super().execute(statement)

        db = DB()
        async def status(_db, **values):
            run.status = values['status']
        async def record(_db, **values):
            if values['event_payload']['type'] == 'TEXT_MESSAGE_CONTENT':
                # emit() has projected the entire visible prefix before appending the replay event.
                self.assertEqual(len(db.added), 1)
                self.assertFalse(db.added[0].is_complete)
            return None
        replacements = {
            '_get_owned_session': AsyncMock(return_value=SimpleNamespace(summary='', archived=False)),
            '_save_user_input': AsyncMock(return_value=[]),
            'create_agent_run': AsyncMock(return_value=run),
            '_load_agent_history': AsyncMock(return_value=([{'role':'user','content':'只解释'}], 1)),
            'list_session_drafts': AsyncMock(return_value=[]),
            'AsyncSessionLocal': lambda: _SessionContext(db),
            'set_run_context_metrics': AsyncMock(),
            'set_run_status': status,
            'append_run_event': record,
            'refresh_run_metrics': AsyncMock(),
            'maybe_update_summary': AsyncMock(),
            'stream_agent_response': source,
        }
        body = RunAgentInput.model_validate({'threadId':'12','runId':'stream-test','state':{},'tools':[],
            'context':[], 'forwardedProps':{}, 'messages':[{'id':'user','role':'user','content':'只解释'}]})
        request = SimpleNamespace(state=SimpleNamespace(principal_id=1, tenant_id=1, token='fixture'),
            headers={}, is_disconnected=AsyncMock(return_value=False))
        with ExitStack() as stack:
            for name, value in replacements.items():
                stack.enter_context(patch('api.chat.' + name, new=value))
            response = await chat(request, body, db=SimpleNamespace(commit=AsyncMock()))
            try:
                yield response.body_iterator, db, run
            finally:
                await response.body_iterator.aclose()

    async def first_text(self, stream):
        while 'TEXT_MESSAGE_CONTENT' not in await anext(stream):
            pass

    async def test_partial_is_saved_before_visible_and_normal_finish_upserts_it(self):
        release = asyncio.Event()
        async def source(*args, **kwargs):
            yield text_event('你🙂')
            await release.wait()
            yield text_event('好')
        async with self.invocation(source) as (stream, db, run):
            await self.first_text(stream)
            self.assertEqual((db.added[0].content, db.added[0].is_complete, run.status), ('你🙂', False, 'running'))
            release.set()
            _ = [chunk async for chunk in stream]
            self.assertEqual(len(db.added), 1)
            self.assertEqual((db.added[0].content, db.added[0].is_complete, run.status), ('你🙂好', True, 'completed'))

    async def test_cancel_while_upstream_blocked_closes_source_and_keeps_partial_incomplete(self):
        closed = asyncio.Event()
        async def source(*args, **kwargs):
            try:
                yield text_event('partial')
                await asyncio.Future()
            finally:
                closed.set()
        async with self.invocation(source) as (stream, db, run):
            await self.first_text(stream)
            run.status = 'cancelled'  # The cancel endpoint commits before signalling the process registry.
            run_cancellation_registry.cancel(run.id)
            with self.assertRaises(asyncio.CancelledError):
                await anext(stream)
            self.assertTrue(closed.is_set())
            self.assertEqual(run.status, 'cancelled')
            self.assertFalse(db.added[0].is_complete)

    async def test_asgi_disconnect_while_upstream_blocked_fails_run_without_losing_partial(self):
        closed = asyncio.Event()
        async def source(*args, **kwargs):
            try:
                yield text_event('partial')
                await asyncio.Future()
            finally:
                closed.set()
        async with self.invocation(source) as (stream, db, run):
            await self.first_text(stream)
            pending = asyncio.create_task(anext(stream))
            await asyncio.sleep(0)  # Enter the consumer wait; this is not a time-based delivery oracle.
            pending.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await pending
            self.assertTrue(closed.is_set())
            self.assertEqual(run.status, 'failed')
            self.assertFalse(db.added[0].is_complete)

    async def test_failure_preserves_partial_but_does_not_mark_it_complete(self):
        async def source(*args, **kwargs):
            yield text_event('partial')
            raise RuntimeError('upstream-failed')
        async with self.invocation(source) as (stream, db, run):
            chunks = [chunk async for chunk in stream]
            self.assertIn('RUN_ERROR', chunks[-1])
            self.assertEqual(run.status, 'failed')
            self.assertEqual(db.added[0].content, 'partial')
            self.assertFalse(db.added[0].is_complete)

    async def test_disconnect_during_first_protocol_send_cleans_up_before_model_starts(self):
        async def source(*args, **kwargs):
            raise AssertionError('model must not start before the initial state is sent')
            yield
        async with self.invocation(source) as (stream, db, run):
            self.assertIn('RUN_STARTED', await anext(stream))
            await stream.aclose()
            self.assertEqual(run.status, 'failed')
            self.assertEqual(db.added, [])
