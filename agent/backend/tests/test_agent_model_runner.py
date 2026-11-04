"""Deterministic safety/orchestration tests; never call a real model or owner."""
import copy
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import AsyncMock, patch

from addp_common.client import Message, ToolCall
from addp_common.tools import ToolExecutionError
from tests.test_deerflow_runtime import _InferenceFixture, _call, _model

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'evals/agent-scenarios'))
import model_runner
from model_cases import CASE_NAMES, QUERY_LOCATOR, QUERY_TEXT, case_fixture, historical_snapshot, transfer_facts
from evaluator import load_scenario


def route(skill=None, **kwargs):
    return Message(role='assistant', tool_calls=[ToolCall(id='route', name='RouteDecision',
                   arguments={'skill': skill, **kwargs})])


class ModelRunnerTests(unittest.IsolatedAsyncioTestCase):
    def test_cases_reuse_unique_scenario_contract(self):
        for name in CASE_NAMES:
            scenario = load_scenario(ROOT / 'evals/agent-scenarios' / name)
            self.assertEqual(scenario['schema'], 'addp.agent-scenario/v1')
            self.assertEqual(len(model_runner._contract_digest(name)), 64)
        snapshot = historical_snapshot()
        self.assertEqual(snapshot['report']['status'], 'unknown')
        self.assertEqual(snapshot['arguments']['config']['transforms'][0]['fields'][1]['target_type'], 'json')

    async def test_write_and_unlisted_read_blocked_before_any_network(self):
        violations = []
        executor = model_runner.fixture_executor(case_fixture('model-engine-inventory'), violations)('http://unused', 'not-a-token')
        with patch('addp_common.client.base.BaseClient._request', new=AsyncMock()) as network:
            for name in ['workflow.run', 'transfer.task.create', 'transfer.task.start', 'data.preview', 'unknown.tool']:
                with self.assertRaises(ToolExecutionError):
                    await executor.call(name, {}, agent_run_id='fixture', tool_call_id='call')
        network.assert_not_awaited()
        self.assertEqual(violations, ['model_tool_blocked'] * 5)

    async def test_changed_parameters_and_invalid_schema_stay_failures(self):
        violations = []
        executor = model_runner.fixture_executor(case_fixture('model-engine-inventory'), violations)('http://unused', '')
        for args, code in [({'capability': 'all'}, 'model_parameters_changed'),
                           ({'capability': 'invented'}, 'invalid_arguments')]:
            with self.assertRaises(ToolExecutionError) as caught:
                await executor.call('engine.list', args, agent_run_id='fixture', tool_call_id='call')
            self.assertEqual(caught.exception.code, code)
        result = await executor.call('engine.list', {'capability': 'workflow'}, agent_run_id='fixture', tool_call_id='call')
        self.assertEqual(len(result), 2)
        self.assertEqual(violations, ['model_parameters_changed', 'invalid_arguments'])

    async def test_check_arguments_compared_in_full_including_extra_fields(self):
        violations = []
        fixture = case_fixture('model-transfer-precheck')
        executor = model_runner.fixture_executor(fixture, violations)('http://unused', '')
        arguments = copy.deepcopy(transfer_facts()[3])
        arguments['description'] = 'changed'
        with self.assertRaises(ToolExecutionError) as caught:
            await executor.call('transfer.task.config.check', arguments, agent_run_id='fixture', tool_call_id='call')
        self.assertEqual(caught.exception.code, 'model_parameters_changed')

    async def test_precheck_fixture_admits_formal_parent_reads_but_rejects_other_resources(self):
        violations = []
        fixture = case_fixture('model-transfer-precheck')
        executor = model_runner.fixture_executor(fixture, violations)('http://unused', '')
        target = transfer_facts()[2]
        result = await executor.call('resource.children.list', {'engine_id': 312, 'parent_locator': target['locator']},
                                     agent_run_id='fixture', tool_call_id='call')
        self.assertEqual(result, target)
        root = await executor.call('resource.children.list', {'engine_id':312},agent_run_id='fixture',tool_call_id='root')
        self.assertEqual(root['locator'],'addp://engine/312/path?type=engine')
        self.assertEqual(root['children'],[target])
        repeated = await executor.call('resource.children.list', {'engine_id':312,'parent_locator':root['locator']},
                                       agent_run_id='fixture',tool_call_id='root-again')
        self.assertEqual(repeated,root)
        with self.assertRaises(ToolExecutionError):
            await executor.call('resource.children.list', {'engine_id': 999}, agent_run_id='fixture', tool_call_id='call')
        self.assertEqual(violations, ['model_parameters_changed'])

    async def test_real_routing_harness_and_manifest_adapter_engine_case(self):
        client = _InferenceFixture([
            route('workflow-analysis'),
            Message(role='assistant', tool_calls=[_call('engine.list', {'capability': 'workflow'})]),
            Message(role='assistant', content='VECTOR_FIXTURE_ONLINE online; VECTOR_FIXTURE_OFFLINE offline'),
        ])
        with patch('addp_common.client.base.BaseClient._request', new=AsyncMock()) as network:
            sample = await model_runner.run_case('model-engine-inventory', 1, _model(client))
        self.assertEqual(sample['status'], 'passed', sample)
        self.assertTrue(all(sample['assertions'].values()))
        self.assertEqual([t['name'] for t in sample['tools']], ['engine.list'])
        self.assertIsInstance(sample['first_text_ms'], int)
        self.assertGreater(sample['body']['model_characters'],0)
        self.assertEqual(sample['model_identities'][0]['profile_version'], 1)
        self.assertEqual(len(sample['model_identities'][0]['deployment_sha256']), 64)
        network.assert_not_awaited()
        serialized = json.dumps(sample)
        self.assertNotIn('VECTOR_FIXTURE_', serialized)
        self.assertNotIn('synthetic-no-user-access', serialized)

    async def test_real_query_route_retains_engine_language_and_resource(self):
        arguments = {'query': '按status统计数量', 'engine_id': 321, 'query_language': 'sql',
                     'engine_context': {'dialect': 'postgresql'},
                     'resources': [{'role': 'input', 'locator': QUERY_LOCATOR, 'engine_id': 321}]}
        client = _InferenceFixture([
            route('query-generation'),
            Message(role='assistant', tool_calls=[_call('resource.ancestors.get', {'engine_id': 321, 'locator': QUERY_LOCATOR}, 'ancestors')]),
            Message(role='assistant', tool_calls=[_call('resource.facts.get', {'locator': QUERY_LOCATOR}, 'facts')]),
            Message(role='assistant', tool_calls=[_call('query.draft.generate', arguments, 'draft')]),
            Message(role='assistant', content=QUERY_TEXT),
        ])
        sample = await model_runner.run_case('model-query-draft', 1, _model(client))
        self.assertEqual(sample['status'], 'passed', sample)

    async def test_historical_route_only_reads_exact_snapshot_and_streams_explanation(self):
        snapshot = historical_snapshot()
        client = _InferenceFixture([
            route(draft_action='inspect', draft_ref=snapshot['reference'], draft_explanation=case_fixture('model-historical-draft')['expected_scope']),
            Message(role='assistant', content='members按json保留数组；unknown仍是历史预检状态。'),
        ])
        sample = await model_runner.run_case('model-historical-draft', 1, _model(client))
        self.assertEqual(sample['status'], 'passed', sample)
        self.assertEqual(sample['skill'], 'none')
        self.assertEqual([t['name'] for t in sample['tools']], ['draft.inspect'])
        self.assertNotIn(snapshot['reference'], json.dumps(sample))

    async def test_key_topics_record_runtime_text_without_counting_private_route_or_model_body(self):
        fixture = case_fixture('model-historical-boundaries')
        snapshot = fixture['snapshot']
        decision = route(draft_action='inspect', draft_ref=snapshot['reference'], draft_explanation=fixture['expected_scope'])
        decision.content = 'private-routing-reason'
        sample = await model_runner.run_case('model-historical-boundaries', 1, _model(_InferenceFixture([decision])))
        self.assertEqual(sample['status'],'passed',sample)
        self.assertGreater(sample['body']['runtime_characters'],0)
        self.assertEqual(sample['body']['model_characters'],0)
        self.assertEqual(sample['body']['model_chunks'],0)
        self.assertIsNone(sample['body']['first_model_text_ms'])
        self.assertNotIn('private-routing-reason',json.dumps(sample))
        self.assertNotIn('model_body_nonempty',sample['assertions'])

    async def test_same_name_ambiguity_can_ask_without_model_stream_or_draft_read(self):
        decision = route(draft_action='inspect', draft_explanation={'topics':['status','policy','boundary','authorization'],'fields':[]})
        sample = await model_runner.run_case('model-historical-ambiguous',1,_model(_InferenceFixture([decision])))
        self.assertEqual(sample['status'],'passed',sample)
        self.assertEqual(sample['tools'],[])
        self.assertEqual(sample['body']['model_characters'],0)

    def test_v1_model_report_is_rejected_without_compatibility_parser(self):
        report = self.empty_failure_report()
        report['schema'] = 'addp.agent-model-regression/v1'
        with self.assertRaises(ValueError):
            model_runner.validate_report(report)

    async def test_real_transfer_route_checks_exact_parameters_without_creating(self):
        arguments = transfer_facts()[3]
        client = _InferenceFixture([
            route(needs_platform_semantics=True),
            Message(role='assistant', tool_calls=[ToolCall(id='platform-route', name='PlatformRouteDecision',
                    arguments={'capability': 'transfer.task.create'})]),
            Message(role='assistant', tool_calls=[_call('transfer.task.config.check', arguments)]),
            Message(role='assistant', content='unknown，当前只做预检，没有创建或运行。'),
        ])
        sample = await model_runner.run_case('model-transfer-precheck', 1, _model(client))
        self.assertEqual(sample['status'], 'passed', sample)
        self.assertTrue(sample['assertions']['report_preserved'])

    async def test_short_fixture_requires_owner_defaults_and_exact_field_choices(self):
        fixture=case_fixture('model-transfer-short-precheck')
        violations=[]
        executor=model_runner.fixture_executor(fixture,violations)('http://unused','')
        choices={**fixture['expected']['transfer.task.draft.build'],'name':'confirmed name',
                 'fields':[{'path':['_id'],'target':'_id'},{'path':['members'],'target':'members'}]}
        result=await executor.call('transfer.task.draft.build',choices,agent_run_id='run',tool_call_id='build')
        self.assertEqual(result['arguments']['config']['transforms'][0]['fields'][1]['target_type'],'json')
        await executor.call('transfer.task.config.check',result['arguments'],agent_run_id='run',tool_call_id='check')
        for key,value in [('target_type','array'),('target_type','json'),('nullable',True),('precision',18),('scale',2)]:
            changed=copy.deepcopy(choices);changed['fields'][1][key]=value
            with self.assertRaises(ToolExecutionError) as caught:
                await executor.call('transfer.task.draft.build',changed,agent_run_id='run',tool_call_id='wrong')
            self.assertEqual(caught.exception.code,'model_build_types_changed')
        choices['fields'][0]['target']='invented'
        with self.assertRaises(ToolExecutionError) as caught:
            await executor.call('transfer.task.draft.build',choices,agent_run_id='run',tool_call_id='wrong')
        self.assertEqual(caught.exception.code,'model_build_fields_changed')

    async def test_short_request_discovers_builds_and_checks_owner_result(self):
        fixture = case_fixture('model-transfer-short-precheck')
        self.assertEqual(fixture['responses']['data.search']['results'], [transfer_facts()[1]])
        definition,source,parent,_,_ = transfer_facts()
        arguments = fixture['responses']['transfer.task.draft.build']['arguments']
        choices = {**fixture['expected']['transfer.task.draft.build'], 'name':arguments['name'],
                   'fields':[{'path':['_id'],'target':'_id'},{'path':['members'],'target':'members'}]}
        client = _InferenceFixture([
            route(needs_platform_semantics=True),
            Message(role='assistant',tool_calls=[ToolCall(id='platform-route',name='PlatformRouteDecision',arguments={'capability':'transfer.task.create'})]),
            Message(role='assistant',tool_calls=[_call('resource.facts.get',{'locator':source['locator']})]),
            Message(role='assistant',tool_calls=[_call('transfer.task.draft.build',choices)]),
            Message(role='assistant',tool_calls=[_call('transfer.task.config.check',arguments)]),
        ])
        sample=await model_runner.run_case('model-transfer-short-precheck',1,_model(client))
        self.assertEqual(sample['status'],'passed',sample)
        self.assertTrue(sample['assertions']['report_preserved'])

    async def test_failed_call_not_erased_when_model_recovers(self):
        client = _InferenceFixture([
            route('workflow-analysis'),
            Message(role='assistant', tool_calls=[_call('engine.list', {'capability': 'all'}, 'wrong')]),
            Message(role='assistant', tool_calls=[_call('engine.list', {'capability': 'workflow'}, 'correct')]),
            Message(role='assistant', content='VECTOR_FIXTURE_ONLINE VECTOR_FIXTURE_OFFLINE'),
        ])
        sample = await model_runner.run_case('model-engine-inventory', 1, _model(client))
        self.assertEqual(sample['status'], 'failed')
        self.assertIn('model_parameters_changed', sample['violation_codes'])

    async def test_unnecessary_clarification_is_not_success(self):
        client = _InferenceFixture([
            route('workflow-analysis'),
            Message(role='assistant', tool_calls=[_call('request_clarification', {
                'prompt': '是否继续', 'reason': 'needs_confirmation',
                'options': [{'label': '是', 'value': 'yes'}]})]),
        ])
        sample = await model_runner.run_case('model-engine-inventory', 1, _model(client))
        self.assertEqual(sample['status'], 'failed')
        self.assertFalse(sample['assertions']['trace_contract'])

    async def test_timeout_is_failed_and_does_not_leak_exception_text(self):
        async def stream(**kwargs):
            import asyncio
            await asyncio.sleep(10)
            yield None
        with patch('agents.main_agent.stream_agent_response', new=stream):
            sample = await model_runner.run_case('model-engine-inventory', 1, object(), timeout=0.001)
        self.assertEqual(sample['error_code'], 'model_case_timeout')
        self.assertEqual(sample['status'], 'failed')

    def test_report_refuses_repository_and_existing_target(self):
        with self.assertRaises(ValueError):
            model_runner.write_report(ROOT / 'model-report.json', {})
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'result.json'
            report = self.empty_failure_report()
            model_runner.write_report(path, report)
            with self.assertRaises(FileExistsError):
                model_runner.write_report(path, report)
            self.assertEqual(json.loads(path.read_text()), report)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(list(Path(directory).iterdir()), [path])

    @staticmethod
    def empty_failure_report():
        return {'schema': model_runner.SCHEMA, 'created_at': '2026-11-03T00:00:00+00:00',
                'source': {'revision': 'a' * 40, 'worktree_dirty': True},
                'mode': 'real_model_synthetic_owners', 'status': 'failed',
                'repetitions': 1,
                'answer_review': 'required', 'error_code': 'model_runtime_unavailable', 'samples': []}

    def test_report_strictly_rejects_private_fields_and_false_success(self):
        for change in ('trace', 'prompt', 'source_token', 'status'):
            report = self.empty_failure_report()
            if change == 'status':
                report['status'] = 'passed'
            else:
                report[change] = 'private-value'
            with self.assertRaises(ValueError):
                model_runner.validate_report(report)

    async def test_missing_credentials_fail_without_network_and_no_scripted_fallback(self):
        from config import settings
        with patch.object(settings, 'AGENT_SERVICE_CLIENT_SECRET', ''), patch('model_runner.run_case', new=AsyncMock()) as run:
            report = await model_runner.run_suite(1)
        run.assert_not_awaited()
        self.assertEqual(report['status'], 'failed')
        self.assertEqual(report['error_code'], 'model_runtime_unavailable')
        model_runner.validate_report(report)

    def test_public_entry_is_explicit_and_existing_ci_discovers_tests(self):
        makefile = (ROOT / 'Makefile').read_text()
        self.assertIn('test-agent-model:', makefile)
        script = (ROOT / 'scripts/test/agent-evaluation-gate.sh').read_text()
        self.assertIn('model_runner.py', script)
        self.assertIn('ADDP_AGENT_MODEL_TENANT_ID', script)
        self.assertIn('POSTGRES_PORT', script)
        workflow = (ROOT / '.github/workflows/platform-ci.yml').read_text()
        self.assertIn('run: make test-agent-eval', workflow)
        self.assertNotIn('run: make test-agent-model', workflow)
