import importlib.util
from pathlib import Path
import unittest
import json
import os
import tempfile
from unittest.mock import patch
from copy import deepcopy
import io
from types import SimpleNamespace

path = Path(__file__).with_name('elasticsearch-consumer-flow-online.py')
spec = importlib.util.spec_from_file_location('elasticsearch_online_test_target', path)
MODULE = importlib.util.module_from_spec(spec)
spec.loader.exec_module(MODULE)


class ElasticsearchConsumerContractTest(unittest.TestCase):
    def test_dotted_index_locator_round_trip(self):
        self.assertEqual(MODULE.locator(91, {'id': 7, 'full_name': 'addp_orders.v1'}),
                         'addp://engine/91/path/addp_orders.v1?type=index&item_id=7')

    def test_precision_and_projection_must_be_verified(self):
        row = {'order_id': '9007199254740993', 'customer': {'name': 'customer-0'}}
        MODULE.validate_rows([row], 1, projected=True)
        for bad in [dict(row, order_id=9007199254740992), dict(row, private='leak'),
                    dict(row, order_id='9007199254740994'), dict(row, customer={'name': 'wrong'})]:
            with self.assertRaises(MODULE.SuiteError):
                MODULE.validate_rows([bad], 1, projected=True)

    def test_full_fixture_detects_duplicate_records_and_incorrect_sorting(self):
        rows = [{'order_id': str(9007199254740993 + number),
                 'customer': {'name': 'customer-' + str(number)}} for number in range(25)]
        MODULE.validate_rows(rows, 25, projected=True, ordered=True)
        MODULE.validate_rows(list(reversed(rows)), 25, projected=True)
        for invalid in [rows[:-1] + [rows[0]], list(reversed(rows))]:
            with self.assertRaises(MODULE.SuiteError):
                MODULE.validate_rows(invalid, 25, projected=True, ordered=True)

    def test_nested_array_must_retain_fixture_contents(self):
        row = {'order_id': '9007199254740993', 'customer': {'name': 'customer-0'},
               'items': [{'sku': 'SKU-001', 'quantity': 1}]}
        MODULE.validate_rows([row], 1)
        for items in [[], {'sku': 'SKU-001', 'quantity': 1}, [{'sku': 'SKU-001', 'quantity': 2}]]:
            with self.assertRaises(MODULE.SuiteError):
                MODULE.validate_rows([dict(row, items=items)], 1)


class ElasticsearchPreflightTest(unittest.TestCase):
    def test_query_execution_requires_allowed_read_only_preflight(self):
        for allowed in (True, False):
            with self.subTest(allowed=allowed):
                calls = []
                def request(method, path, statuses, body=None):
                    calls.append(path)
                    if len(calls) == 1:
                        self.assertEqual(path, '/api/v1/develop/query-preflight')
                        self.assertEqual(body['query_type'], 'es_dsl')
                        self.assertEqual(body['target_locator'], 'target')
                        return SimpleNamespace(payload={'allowed': allowed, 'effect': 'read',
                            'requires_confirmation': False, 'diagnostics': []})
                    self.assertTrue(allowed, 'denied preflight must not create an execution')
                    if method == 'POST': return SimpleNamespace(payload={'execution_id': 'es-execution'})
                    return SimpleNamespace(payload={'status': 'success', 'metadata': {'result': {'summary': {'preview_rows': []}}}})
                client = SimpleNamespace(request=request)
                if allowed:
                    self.assertEqual(MODULE.execute_query(client, 7, 'target', {'query': {'match_all': {}}}, MODULE.time.monotonic() + 10), ('es-execution', []))
                    self.assertEqual(calls, ['/api/v1/develop/query-preflight', '/api/v1/develop/executions', '/api/v1/develop/executions/es-execution'])
                else:
                    with self.assertRaises(MODULE.SuiteError):
                        MODULE.execute_query(client, 7, 'target', {'query': {'match_all': {}}}, MODULE.time.monotonic() + 10)
                    self.assertEqual(calls, ['/api/v1/develop/query-preflight'])


class ElasticsearchSparkWorkflowTest(unittest.TestCase):
    def fixture(self):
        final = {'type': 'spark_dataframe', 'preview_rows': [{'rows': 25, 'amount_sum': 562.5,
                 'minimum_order_id': '9007199254740993'}]}
        nodes = {'orders': {'type': 'spark_dataframe', 'schema': [
                    {'name': 'order_id', 'type': 'bigint'}, {'name': 'tags', 'type': 'array<string>'},
                    {'name': 'items', 'type': 'array<struct<quantity:int,sku:string>>'}],
                  'preview_rows': [{'order_id': '9007199254740993', 'tags': ['sample', 'business'],
                    'customer': {'name': 'customer-0'}, 'items': [{'quantity': 1, 'sku': 'SKU-001'}]}]},
                 'empty': {'type': 'spark_dataframe', 'preview_rows': []},
                 'empty_total': {'type': 'spark_dataframe', 'preview_rows': [{'rows': 0}]}, 'summary': final}
        execution = {'status': 'success', 'metadata': {'result': {'runtime_execution_id': 'runtime-execution', 'final_result': final}}}
        return execution, nodes

    def test_workflow_uses_index_locators_and_explicit_arrays_without_connections(self):
        definition = MODULE.workflow('orders-locator', 'empty-locator')
        self.assertEqual(len(definition['tasks']), 4)
        for node in definition['tasks'][:2]:
            self.assertEqual(set(node['params']), {'source_type', 'locator', 'array_fields'})
            self.assertEqual(node['params']['source_type'], 'index')
            self.assertEqual(node['params']['array_fields'], ['tags'])
        self.assertEqual(definition['tasks'][-1]['depends_on'], ['orders', 'empty_total'])

    def test_formal_result_has_only_final_summary_and_runtime_reference(self):
        execution, _ = self.fixture()
        MODULE.validate_workflow_result(execution)
        for mutate in (lambda x: x.update(status='failed'), lambda x: x.update(outputs=[{}]),
                       lambda x: x['metadata']['result']['final_result']['preview_rows'][0].update(minimum_order_id=9007199254740992),
                       lambda x: x['metadata']['result'].update(produced_targets=[{}])):
            bad = deepcopy(execution); mutate(bad)
            with self.assertRaises(MODULE.SuiteError): MODULE.validate_workflow_result(bad)

    def test_all_eight_runtime_requests_preserve_typed_nodes_and_precision(self):
        execution, nodes = self.fixture()
        final = MODULE.validate_workflow_result(execution)
        snapshot = {'execution_id': 'runtime-execution', 'status': 'success', 'progress': 100,
                    'result': final, 'task_order': list(nodes), 'all_results': nodes}
        def response(*args, **kwargs): return io.BytesIO(json.dumps(snapshot).encode())
        with patch.object(MODULE.SPARK.urllib.request, 'urlopen', side_effect=response) as call:
            evidence = MODULE.SPARK.runtime_status_evidence('http://runtime', 'runtime-execution', final, 4, MODULE.validate_workflow_nodes)
            self.assertEqual((evidence['queries'], call.call_count), (8, 8))
        for mutate in (lambda x: x['orders']['preview_rows'][0].update(order_id=9007199254740992),
                       lambda x: x['orders']['preview_rows'][0].update(tags='sample'),
                       lambda x: x['orders']['schema'][0].update(type='double'),
                       lambda x: x['empty_total'].update(preview_rows=[{'rows': 1}])):
            bad = deepcopy(nodes); mutate(bad)
            with self.assertRaises(MODULE.SuiteError): MODULE.validate_workflow_nodes(bad)

    def test_operator_envelope_and_formal_submission_use_distinct_cluster(self):
        execution, _ = self.fixture()
        calls = []
        def request(method, path, statuses, body=None):
            calls.append(path)
            if path.endswith('workflow-engines'):
                payload = [{'id': 8, 'engine_type': 'spark_workflow', 'connection_status': 'online'}]
            elif path.endswith('operators'):
                payload = {'operators': [{'id': 'load', 'public_parameters': [{'name': 'source_resource',
                            'ui_config': {'selectable_node_types': ['table', 'file', 'index']}}]}]}
            elif method == 'POST':
                self.assertEqual(body['execution_config'], {'engine_id': 8, 'engine_specific': {'spark_cluster_id': 9}})
                payload = {'execution_id': 'formal-execution'}
            else: payload = execution
            return SimpleNamespace(payload=payload)
        with patch.dict(os.environ, ADDP_ONLINE_SPARK_RUNTIME_URL='http://runtime'), patch.object(MODULE.SPARK, 'runtime_status_evidence', return_value={}), patch.object(MODULE.SPARK, 'worker_evidence', return_value={}):
            report = MODULE.execute_workflow(SimpleNamespace(request=request), 'orders', 'empty', 9, MODULE.time.monotonic()+10)
        self.assertEqual(report['execution_id'], 'formal-execution')
        self.assertEqual(calls, ['/api/v1/develop/workflow-engines', '/api/v1/develop/workflow-engines/8/operators',
                               '/api/v1/develop/executions', '/api/v1/develop/executions/formal-execution'])


class ElasticsearchBrowserEvidenceTest(unittest.TestCase):
    def test_browser_requires_success_matching_identity_and_all_screenshots(self):
        report = {'engine_id': 7, 'tenant_id': 2, 'principal_id': '42', 'spark': {'execution_id': 'spark-execution'}}
        for failure in ('process', 'missing_report', 'identity', 'missing_screenshot', ''):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                evidence = dict(engine_id=7, tenant_id=2, principal_id='42', run_id='es-run', manager_rows=25, develop_rows=25,
                                empty_index=True, meta_ui_scan=True, develop_ui_query=True,
                                spark_execution_id='spark-execution', spark_workflow_result=True)
                if failure == 'identity': evidence['principal_id'] = 'other'
                def browser(command, cwd, env):
                    self.assertIn('e2e/online/elasticsearch-consumer-flow.spec.js', command)
                    self.assertEqual(json.loads(env['ADDP_ONLINE_ELASTICSEARCH_EXPECTATIONS']), report)
                    if failure != 'missing_report':
                        Path(env['ADDP_ONLINE_ELASTICSEARCH_BROWSER_REPORT']).write_text(json.dumps(evidence))
                    for name in ('meta', 'orders', 'empty', 'query', 'workflow'):
                        if failure != 'missing_screenshot' or name != 'query':
                            (directory / f'elasticsearch-{name}-console.png').write_bytes(b'screenshot')
                    return SimpleNamespace(returncode=1 if failure == 'process' else 0)
                with patch.dict(os.environ, ADDP_ONLINE_ARTIFACT_DIR=temporary, ADDP_ONLINE_TEST_RUN_ID='es-run'), patch.object(MODULE.subprocess, 'run', side_effect=browser):
                    if failure:
                        with self.assertRaises(MODULE.SuiteError): MODULE.run_browser(report)
                    else:
                        self.assertEqual(MODULE.run_browser(report), evidence)

if __name__ == '__main__':
    unittest.main()
