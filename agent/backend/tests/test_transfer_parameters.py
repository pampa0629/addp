import copy
import json
import unittest
from agents.transfer_parameters import explicit_user_arguments, BUILD_TOOL, parameter_reference
from agents.platform_conditions import CHECK_TOOL, CREATE_TOOL
from addp_common.client import Message
from tests.test_deerflow_runtime import _call
from tests.test_transfer_precheck_runtime import TransferPrecheckRuntimeTests as _RuntimeHarness
from tests.test_platform_conditions import platform_fixture


class ExplicitTransferParameterTests(unittest.TestCase):
    def test_unique_complete_request_without_nested_recovery(self):
        arguments = platform_fixture()[3]
        text = json.dumps(arguments, ensure_ascii=False)
        for body in (text, '只检查\n' + text, '配置\n```json\n'+text+'\n```'):
            self.assertEqual(explicit_user_arguments(body), arguments)
        for body in (
            text+'\n'+text, '{"wrapper":'+text+',"unfinished":',
            '{"wrapper":'+text+'}', '['+text+']', text+'\n{"config":',
            '{"name":"duplicate","name":"duplicate","wrapper":'+text+'}',
            json.dumps(text), text[:-1], '{"name":"partial"}\n'+text,
        ):
            with self.subTest(body=body[:40]):
                self.assertIsNone(explicit_user_arguments(body))


class TransferParameterRuntimeTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.h = _RuntimeHarness()
        self.h.setUp()
        self.allowed = ['platform.capability.context', BUILD_TOOL, CHECK_TOOL, CREATE_TOOL]
        self.choices = {'name':'fixture', 'source_locator':self.h.arguments['config']['source']['locator'],
                        'target':{'parent_locator':self.h.arguments['config']['target']['parent_locator'], 'name':'target','apply_mode':'replace'},
                        'row_grain':{'mode':'document'},'fields':[{'path':['_id'],'target':'id'}]}

    def build_call(self):
        return _call(BUILD_TOOL, self.choices)

    def owner_checks(self):
        return [r for r in self.h.requests if r[1] == '/api/v1/transfer/task-config-checks']

    async def test_constructed_arguments_remain_exact_and_audited(self):
        for present in (False,True):
            self.setUp()
            if present:
                self.h.arguments.update(description='  literal "description"  ',batch_size=500)
                self.h.arguments['config']['transforms'][0]['fields'].reverse()
            original = copy.deepcopy(self.h.arguments)
            events, _ = await self.h.run_factory([
                Message(role='assistant',tool_calls=[self.build_call()]),
                Message(role='assistant',tool_calls=[_call(CHECK_TOOL,copy.deepcopy(original))]),
            ],allowed=self.allowed)
            checks=self.owner_checks()
            self.assertEqual(len(checks),1)
            expected={**original,'description':original.get('description',''),'batch_size':original.get('batch_size',1000),
                      'task_type':'sync','schedule':'','enabled':False,'auto_scan_metadata':False}
            self.assertEqual(checks[0][2]['json'],expected)
            evidence=[e.payload['facts']['constructed_draft'] for e in events if e.kind=='checkpoint' and 'constructed_draft' in e.payload.get('facts',{})]
            self.assertEqual(len(evidence),1)
            self.assertEqual(evidence[0]['arguments'],original)
            checkpoint=[e.payload['checkpoint'] for e in events if e.kind=='checkpoint'][-1]
            self.assertEqual(set(checkpoint['parameter_source']),{'kind','id','digest'})
            self.assertNotIn('arguments',checkpoint['parameter_source'])
            starts={e.payload['tool_call_id'] for e in events if e.kind=='tool_start'}
            self.assertTrue(all(e.payload['tool_call_id'] in starts for e in events if e.kind=='tool_result'))
            self.assertEqual(self.h.writes(),[])

    async def test_changed_or_unbuilt_parameters_never_reach_precheck(self):
        for change in ('unbuilt','name','description','batch_size','order','query'):
            self.setUp()
            changed=copy.deepcopy(self.h.arguments)
            if change=='name':changed['name']='invented'
            elif change=='description':changed['description']='invented'
            elif change=='batch_size':changed['batch_size']=500
            elif change=='order':changed['config']['transforms'][0]['fields'].reverse()
            elif change=='query':changed['config']['source']['query']={'language':'mql','statement':'{"aggregate":"invented","pipeline":[]}'}
            responses=[] if change=='unbuilt' else [Message(role='assistant',tool_calls=[self.build_call()])]
            responses.append(Message(role='assistant',tool_calls=[_call(CHECK_TOOL,changed)]))
            events,_=await self.h.run_factory(responses,allowed=self.allowed)
            self.assertEqual(self.owner_checks(),[])
            self.assertTrue(any(e.kind=='run_failed' for e in events))
            starts={e.payload['tool_call_id'] for e in events if e.kind=='tool_start'}
            self.assertTrue(all(e.payload['tool_call_id'] in starts for e in events if e.kind=='tool_result'))

    async def test_failed_build_drops_same_batch_check_and_stops(self):
        self.h.build_status=502
        self.h.build_result={'error':'dependency unavailable','error_code':'draft_facts_unavailable'}
        events,client=await self.h.run_factory([Message(role='assistant',tool_calls=[
            _call(CHECK_TOOL,self.h.arguments),self.build_call(),_call(CREATE_TOOL,self.h.arguments),
        ])],allowed=self.allowed)
        self.assertEqual(len(client.calls),1)
        self.assertEqual(self.owner_checks(),[])
        self.assertEqual(self.h.writes(),[])
        self.assertTrue(any(e.kind=='run_failed' for e in events))

    async def test_user_supplied_complete_configuration_uses_same_exact_check(self):
        events,_=await self.h.run_factory([Message(role='assistant',tool_calls=[_call(CHECK_TOOL,self.h.arguments)])],
                                        allowed=self.allowed,user_request='按原参数只预检\n'+json.dumps(self.h.arguments))
        self.assertEqual(len(self.owner_checks()),1)
        self.assertFalse(any(r[1].endswith('task-config-drafts') for r in self.h.requests))

    async def test_clarification_restore_requires_the_bound_source(self):
        self.h.checkpoint['parameter_source']=parameter_reference('build','build-1',self.h.arguments)
        reads=[]
        async def read(reference):
            reads.append(copy.deepcopy(reference));return copy.deepcopy(self.h.arguments)
        events,_=await self.h.run_factory([Message(role='assistant',tool_calls=[_call(CHECK_TOOL,self.h.arguments)])],
                                        allowed=self.allowed,resuming=True,parameter_reader=read)
        self.assertEqual(len(reads),1)
        self.assertEqual(len(self.owner_checks()),1)
        self.h.requests.clear()
        async def corrupt(_):return {**self.h.arguments,'name':'different'}
        with self.assertRaisesRegex(ValueError,'source_invalid'):
            await self.h.run_factory([],allowed=self.allowed,resuming=True,parameter_reader=corrupt)
        self.assertEqual(self.owner_checks(),[])

    async def test_ambiguous_user_input_pauses_without_reconstructing_or_orphan_results(self):
        self.h.checkpoint['parameter_source_issue']=True
        events,_=await self.h.run_factory([Message(role='assistant',tool_calls=[self.build_call()])],allowed=self.allowed)
        self.assertEqual(self.owner_checks(),[])
        self.assertFalse(any(r[1].endswith('task-config-drafts') for r in self.h.requests))
        interaction=next(e for e in events if e.kind=='interaction_required')
        self.assertEqual(interaction.payload['reason'],'transfer_parameter_source')
        starts={e.payload['tool_call_id'] for e in events if e.kind=='tool_start'}
        self.assertTrue(all(e.payload['tool_call_id'] in starts for e in events if e.kind=='tool_result'))
        self.assertFalse(any(e.kind=='run_failed' for e in events))

    async def test_rejected_parameters_have_matching_ag_ui_and_persisted_steps(self):
        import uuid
        from types import SimpleNamespace
        from unittest.mock import AsyncMock, patch
        from ag_ui.core import RunAgentInput
        from api.chat import chat
        from tests.test_run_events import _MessageDB, _SessionContext

        changed={**self.h.arguments,'name':'changed'}
        runtime_events,_=await self.h.run_factory([
            Message(role='assistant',tool_calls=[self.build_call()]),
            Message(role='assistant',tool_calls=[_call(CHECK_TOOL,changed)]),
        ],allowed=self.allowed)
        async def stream(*_args,**_kwargs):
            for event in runtime_events:
                yield event
        db=_MessageDB()
        request=SimpleNamespace(state=SimpleNamespace(principal_id=1,tenant_id=1,token='fixture'),
                                headers={'accept-language':'en-US'},is_disconnected=AsyncMock(return_value=False))
        body=RunAgentInput.model_validate({'threadId':'12','runId':'protocol-source','state':{},'tools':[],
            'context':[],'forwardedProps':{},'messages':[{'id':'user-source','role':'user','content':'先给草稿'}]})
        with (
            patch('api.chat._get_owned_session',new=AsyncMock(return_value=SimpleNamespace(summary='',archived=False))),
            patch('api.chat._save_user_input',new=AsyncMock(return_value=[])),
            patch('api.chat.create_agent_run',new=AsyncMock(return_value=SimpleNamespace(id=uuid.uuid4(),checkpoint={}))),
            patch('api.chat._load_agent_history',new=AsyncMock(return_value=([{'role':'user','content':'先给草稿'}],1))),
            patch('api.chat.list_session_drafts',new=AsyncMock(return_value=[])),
            patch('api.chat.AsyncSessionLocal',return_value=_SessionContext(db)),
            patch('api.chat.set_run_context_metrics',new=AsyncMock()),
            patch('api.chat.create_run_step',new=AsyncMock(side_effect=lambda *_a,**_k:SimpleNamespace(id=uuid.uuid4()))) as created,
            patch('api.chat.complete_run_step',new=AsyncMock()) as completed,
            patch('api.chat.update_run_checkpoint',new=AsyncMock()),
            patch('api.chat.attach_step_facts',new=AsyncMock()) as facts,
            patch('api.chat.set_run_status',new=AsyncMock()),
            patch('api.chat.append_run_event',new=AsyncMock()),
            patch('api.chat.refresh_run_metrics',new=AsyncMock()),
            patch('api.chat.maybe_update_summary',new=AsyncMock()),
            patch('api.chat.stream_agent_response',new=stream),
        ):
            response=await chat(request,body,db=SimpleNamespace(commit=AsyncMock()))
            chunks=[chunk async for chunk in response.body_iterator]
        events=[json.loads(line[6:]) for chunk in chunks for line in chunk.splitlines() if line.startswith('data: ')]
        starts={event['toolCallId'] for event in events if event['type']=='TOOL_CALL_START'}
        results=[event for event in events if event['type']=='TOOL_CALL_RESULT']
        self.assertTrue(all(event['toolCallId'] in starts for event in results))
        self.assertEqual(created.await_count,completed.await_count)
        self.assertEqual(completed.await_args.kwargs['status'],'failed')
        self.assertEqual(completed.await_args.kwargs['error_code'],'transfer_parameter_source_mismatch')
        evidence=[call.kwargs['facts']['constructed_draft'] for call in facts.await_args_list if 'constructed_draft' in call.kwargs['facts']]
        self.assertEqual(evidence[0]['arguments'],self.h.arguments)
        self.assertEqual(events[-1]['type'],'RUN_ERROR')

    async def test_second_build_requires_a_new_user_instruction(self):
        events,_=await self.h.run_factory([
            Message(role='assistant',tool_calls=[self.build_call()]),
            Message(role='assistant',tool_calls=[self.build_call()]),
        ],allowed=self.allowed)
        builds=[r for r in self.h.requests if r[1].endswith('task-config-drafts')]
        self.assertEqual(len(builds),1)
        self.assertEqual(self.owner_checks(),[])
        self.assertTrue(any(e.kind=='run_failed' for e in events))

    async def test_supplied_original_configuration_cannot_be_rebuilt(self):
        events,_=await self.h.run_factory([Message(role='assistant',tool_calls=[self.build_call()])],
            allowed=self.allowed,user_request='按原参数只检查\n'+json.dumps(self.h.arguments))
        self.assertFalse(any(r[1].endswith('task-config-drafts') for r in self.h.requests))
        self.assertEqual(self.owner_checks(),[])
        self.assertTrue(any(e.kind=='run_failed' for e in events))
