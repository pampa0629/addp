import json
import tempfile
from pathlib import Path
import unittest
from unittest.mock import AsyncMock, patch

from pydantic import ValidationError
from addp_common.client import Message
from services.plans import PlanInput, plan_todos
from services.schema import migration_catalog
from tests.test_deerflow_runtime import _call, _collect, _InferenceFixture


class PlanTests(unittest.IsolatedAsyncioTestCase):
    def test_plan_contract_refuses_unbounded_or_unknown_content(self):
        for items in ([{'content':'x'*301,'status':'pending'}], [{'content':'x','status':'approved'}],
                      [{'content':'x','status':'pending','authorization':True}], [{'content':'','status':'pending'}],
                      [{'content':str(i),'status':'pending'} for i in range(13)]):
            with self.assertRaises(ValidationError): plan_todos(items)
        self.assertEqual(plan_todos([{'content':'  inspect  ','status':'pending'}]), [{'content':'inspect','status':'pending'}])

    async def test_real_harness_tracks_a_plan_without_owner_calls_or_completion_fabrication(self):
        todo=[{'content':'Inspect inputs','status':'in_progress'},{'content':'Validate configuration','status':'pending'}]
        client=_InferenceFixture([Message(role='assistant',tool_calls=[_call('write_todos',{'todos':todo},'plan-1')]),
                                  Message(role='assistant',content='Need your input')])
        with patch('tools.langchain_tools.ToolExecutor.call',new=AsyncMock()) as executor:
            events=await _collect(client,[])
        executor.assert_not_awaited()
        self.assertEqual([e.payload['todos'] for e in events if e.kind=='plan'],[todo])
        result=next(e for e in events if e.kind=='tool_result')
        self.assertEqual(json.loads(result.payload['content']),{'plan_items':2})
        self.assertEqual(todo[1]['status'],'pending')

    async def test_clarification_priority_drops_same_batch_plan(self):
        todo=[{'content':'Inspect','status':'completed'}]
        client=_InferenceFixture([Message(role='assistant',tool_calls=[_call('write_todos',{'todos':todo},'plan-1'),
            _call('request_clarification',{'prompt':'Choose a scope','reason':'scope','options':[{'label':'all','value':'all'}]},'clarify')])])
        events=await _collect(client,[])
        self.assertFalse(any(event.kind=='plan' for event in events))
        self.assertTrue(any(event.kind=='interaction_required' for event in events))

    async def test_semantic_resume_rebuilds_plan_without_private_framework_state(self):
        todo=[{'content':'Already inspected','status':'completed'},{'content':'Await user input','status':'pending'}]
        client=_InferenceFixture([Message(role='assistant',content='Continue with the pending choice')])
        await _collect(client,[],plan=todo)
        self.assertIn('Already inspected',client.calls[0]['messages'][-1].content)
        self.assertIn('pending',client.calls[0]['messages'][-1].content)

    async def test_duplicate_plan_updates_are_rejected_before_owner_side_effects(self):
        client=_InferenceFixture([Message(role='assistant',tool_calls=[_call('write_todos',{'todos':[]},'plan-1'),_call('write_todos',{'todos':[]},'plan-2')])])
        with patch('tools.langchain_tools.ToolExecutor.call',new=AsyncMock()) as executor:
            with self.assertRaisesRegex(ValueError,'invalid_plan_batch'):
                await _collect(client,[])
        executor.assert_not_awaited()

    async def test_invalid_plan_has_no_owner_side_effect(self):
        client=_InferenceFixture([Message(role='assistant',tool_calls=[_call('write_todos',{'todos':[{'content':'x'*301,'status':'pending'}]},'plan-1')]),Message(role='assistant',content='Stop')])
        with patch('tools.langchain_tools.ToolExecutor.call',new=AsyncMock()) as executor:
            events=await _collect(client,[])
        executor.assert_not_awaited()
        self.assertFalse(any(event.kind=='plan' for event in events))
        self.assertEqual(next(event.payload['error_code'] for event in events if event.kind=='tool_result'),'invalid_plan')


class MigrationCatalogTests(unittest.TestCase):
    def test_catalog_is_contiguous_and_immutable(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            with self.assertRaisesRegex(ValueError,'empty'): migration_catalog(root)
            (root/'000002_gap.sql').write_text('SELECT 1;')
            with self.assertRaisesRegex(ValueError,'invalid'): migration_catalog(root)
            (root/'000002_gap.sql').rename(root/'000001_initial.sql')
            before=migration_catalog(root)
            (root/'000001_initial.sql').write_text('SELECT 2;')
            self.assertNotEqual(before[0][2],migration_catalog(root)[0][2])
