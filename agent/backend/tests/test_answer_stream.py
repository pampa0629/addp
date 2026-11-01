import asyncio
import anyio
from contextlib import aclosing
import unittest
from unittest.mock import patch

from addp_common.client.inference import ChatStreamEvent, Message, Usage
from agents.answer_stream import coalesce_events
from agents.events import AgentEvent, text_event
from graph.factory import AgentFactory
from tests.test_deerflow_runtime import _InferenceFixture, _call, _model


class AnswerStreamingTests(unittest.IsolatedAsyncioTestCase):
    async def test_real_harness_only_streams_admitted_answer_before_generation_finishes(self):
        release, finished, closed = asyncio.Event(), asyncio.Event(), asyncio.Event()

        class Client(_InferenceFixture):
            async def chat_stream(inner, **arguments):
                inner.calls.append(arguments)
                try:
                    yield ChatStreamEvent(schema_version='addp.inference-stream/v1', type='start', sequence=1, deployment_id='d', profile_version=1)
                    yield ChatStreamEvent(schema_version='addp.inference-stream/v1', type='text_delta', sequence=2, delta='你🙂')
                    await release.wait()
                    yield ChatStreamEvent(schema_version='addp.inference-stream/v1', type='text_delta', sequence=3, delta='好')
                    yield ChatStreamEvent(schema_version='addp.inference-stream/v1', type='completed', sequence=4, usage=Usage())
                    finished.set()
                finally:
                    closed.set()

        client = Client([Message(role='assistant', content='PRIVATE DECISION', tool_calls=[_call('answer_ready')])])
        events = self.run_harness(client)
        received = []
        while True:
            event = await asyncio.wait_for(anext(events), 3)
            received.append(event)
            if event.kind == 'text':
                break
        self.assertFalse(finished.is_set())
        self.assertEqual(received[-1].payload['delta'], '你🙂')
        self.assertNotIn('tools', client.calls[-1])
        release.set()
        received += [event async for event in events]
        self.assertEqual(''.join(event.payload['delta'] for event in received if event.kind == 'text'), '你🙂好')
        self.assertNotIn('PRIVATE DECISION', str(received))
        self.assertTrue(closed.is_set())

    async def test_answer_phase_uses_original_budget_and_does_not_run_a_late_tool(self):
        client = _InferenceFixture([Message(role='assistant', tool_calls=[_call('answer_ready')])])
        with self.assertRaisesRegex(RuntimeError, 'agent_iteration_limit_reached'):
            _ = [event async for event in self.run_harness(client, limit=1)]
        self.assertEqual(len(client.calls), 1)
        client = _InferenceFixture([Message(role='assistant', tool_calls=[_call('answer_ready'), _call('data.search', {'query': 'forbidden'}, 'second')])])
        with patch('tools.langchain_tools.ToolExecutor.call') as owner:
            with self.assertRaisesRegex(RuntimeError, 'invalid_answer_ready_batch'):
                _ = [event async for event in self.run_harness(client)]
            owner.assert_not_called()

    async def test_decision_text_without_answer_control_is_not_public(self):
        class Client(_InferenceFixture):
            async def chat(inner, **arguments):
                from addp_common.client.inference import ChatResponse
                return ChatResponse(schema_version='addp.inference/v1', message=Message(role='assistant', content='PRIVATE'), usage=Usage(), deployment_id='d', profile_version=1)
        with self.assertRaisesRegex(RuntimeError, 'answer_phase_required'):
            _ = [event async for event in self.run_harness(Client([]))]

    async def run_harness(self, client, limit=4):
        async with aclosing(AgentFactory.run(task_context={'skill_name': 'data-browse', 'user_request': '查询', 'context_summary': '', 'user_id': 1, 'tenant_id': 1, 'token': 'fixture', 'agent_run_id': 'answer-stream'},
            skill_body='查询完单独调用answer_ready', allowed_tool_names=[], max_iterations=limit, llm=_model(client))) as events:
            async for event in events:
                yield event

    async def test_closing_real_harness_during_answer_closes_the_blocked_inference_iterator(self):
        closed = asyncio.Event()
        class Client(_InferenceFixture):
            async def chat_stream(inner, **arguments):
                try:
                    yield ChatStreamEvent(schema_version='addp.inference-stream/v1', type='start', sequence=1, deployment_id='d', profile_version=1)
                    yield ChatStreamEvent(schema_version='addp.inference-stream/v1', type='text_delta', sequence=2, delta='partial')
                    await asyncio.Future()
                finally:
                    closed.set()
        events = self.run_harness(Client([Message(role='assistant', tool_calls=[_call('answer_ready')])]))
        while (await anext(events)).kind != 'text':
            pass
        await events.aclose()
        await asyncio.wait_for(closed.wait(), 1)

    async def test_batch_flushes_without_waiting_for_next_upstream_delta_and_cancels_producer(self):
        cancellation, closed, blocked = asyncio.Event(), asyncio.Event(), asyncio.Event()
        async def source():
            try:
                yield text_event('first')
                yield text_event('second')
                blocked.set()
                await asyncio.Future()
            finally:
                closed.set()
        stream = coalesce_events(source(), cancellation)
        self.assertEqual((await anext(stream)).payload['delta'], 'first')
        self.assertEqual((await asyncio.wait_for(anext(stream), 1)).payload['delta'], 'second')
        self.assertTrue(blocked.is_set())
        cancellation.set()
        with self.assertRaises(asyncio.CancelledError):
            await anext(stream)
        self.assertTrue(closed.is_set())

    async def test_batch_preserves_text_presentation_order_and_partial_text_on_failure(self):
        async def source():
            yield text_event('a')
            yield text_event('b')
            yield AgentEvent(kind='presentation', payload={'kind': 'table_preview'})
            yield text_event('c')
            raise RuntimeError('upstream failed')
        result = []
        with self.assertRaisesRegex(RuntimeError, 'upstream failed'):
            async for event in coalesce_events(source()):
                result.append(event)
        self.assertEqual([e.kind for e in result], ['text', 'text', 'presentation', 'text'])
        self.assertEqual(''.join(e.payload['delta'] for e in result if e.kind == 'text'), 'abc')

    async def test_asgi_level_cancellation_waits_for_upstream_cleanup(self):
        closed = asyncio.Event()
        async def source():
            try:
                yield text_event('partial')
                await asyncio.Future()
            finally:
                await asyncio.sleep(0)
                closed.set()
        stream = coalesce_events(source())
        self.assertEqual((await anext(stream)).payload['delta'], 'partial')
        with anyio.CancelScope() as scope:
            scope.cancel()
            await anext(stream)
        self.assertTrue(closed.is_set())
