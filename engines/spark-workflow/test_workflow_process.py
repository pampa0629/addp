"""Invocation binding, parent-only discovery and per-execution result boundaries."""
from contextlib import contextmanager
from copy import deepcopy
from decimal import Decimal
import json
from pathlib import Path
import sys
import unittest
from unittest.mock import MagicMock, Mock, patch

from pyspark.errors import AnalysisException

import spark_connector
import workflow_driver
import workflow_process
from query_process import run_driver


def definition():
    return {'tasks': [{'id': 'q', 'operator': 'sql', 'params': {'query': 'SELECT 1'}, 'depends_on': []}]}


def engine(identifier):
    return {'id': identifier, 'tenant_id': 7, 'engine_type': 'spark',
            'connection_info': {'host': f'cluster-{identifier}', 'port': 7077,
                                'web_ui_url': f'http://cluster-{identifier}:8080'}}


class WorkflowProcessTest(unittest.TestCase):
    def test_missing_workflow_view_survives_driver_and_http_snapshot(self):
        import api_server
        from workflow_engine import run_workflow

        missing = AnalysisException('private view and query')
        missing.getErrorClass = Mock(return_value='TABLE_OR_VIEW_NOT_FOUND')
        body = {'engine_id': 19, 'workflow_def': definition(), 'runtime': {
            'tenant_id': 7, 'execution_authorization': {'id': 8, 'effects': ['read']}}}
        with patch('spark_connector.current_session') as session, \
             patch('workflow_process.get_engine', return_value=engine(19)), \
             patch('workflow_process.run_driver', side_effect=lambda *_a, **_k: run_workflow(19, 7, definition())), \
             api_server.app.test_client() as client:
            session.return_value.sql.side_effect = missing
            response = client.post('/api/workflow', json=body)
            self.assertEqual(response.status_code, 500)
            self.assertEqual(response.json['error_code'], 'SQL_TABLE_OR_VIEW_NOT_FOUND')
            saved = client.get('/api/executions/' + response.json['execution_id'])
            self.assertEqual(saved.json['error_code'], response.json['error_code'])
            self.assertNotIn('private view', response.get_data(as_text=True) + saved.get_data(as_text=True))

    def test_http_freezes_localized_driver_failure_without_private_fields(self):
        import api_server
        body = {'engine_id': 19, 'workflow_def': definition(), 'runtime': {
            'tenant_id': 7, 'execution_authorization': {'id': 8, 'effects': ['read']}}}
        for language, expected in [('zh-cn', 'Spark 工作流执行超时。'), ('en', 'Spark workflow execution timed out.')]:
            with patch('api_server.execute_workflow', return_value={
                    'status': 'failed', 'error_code': 'EXECUTION_FAILED', 'driver_failure': 'timeout'}), \
                 api_server.app.test_client() as client:
                response = client.post('/api/workflow', json=body, headers={'Accept-Language': language})
                self.assertEqual(response.status_code, 500)
                self.assertEqual(response.json['error'], expected)
                self.assertNotIn('driver_failure', response.json)
                saved = client.get('/api/executions/' + response.json['execution_id'], headers={'Accept-Language': 'en' if language == 'zh-cn' else 'zh-cn'})
                self.assertEqual(saved.json['error'], expected)

    def test_parent_freezes_a_b_a_without_starting_a_parent_jvm(self):
        calls = []
        def driver(command, request, **options):
            calls.append(deepcopy(request))
            self.assertEqual(Path(command[1]).name, 'workflow_driver.py')
            self.assertEqual(options, {'timeout': 600, 'parse_float': float})
            return {'status': 'success', 'final_result': request['engine_id']}
        with patch('workflow_process.get_engine', side_effect=lambda identifier, tenant: engine(identifier)), \
             patch('workflow_process.run_driver', side_effect=driver), \
             patch('spark_connector.invocation_session', side_effect=AssertionError('parent JVM')):
            results = [workflow_process.execute_workflow(i, 7, definition()) for i in (19, 20, 19)]
        self.assertEqual([r['final_result'] for r in results], [19, 20, 19])
        self.assertEqual([r['connection']['host'] for r in calls], ['cluster-19', 'cluster-20', 'cluster-19'])
        self.assertTrue(all(set(r) == {'engine_id','tenant_id','connection','mode','workflow_def','input_data'} for r in calls))

    def test_identity_mismatch_and_invalid_dag_never_spawn(self):
        for changed in ({'id': 20}, {'tenant_id': 8}, {'engine_type': 'postgresql'}):
            with self.subTest(changed=changed), patch('workflow_process.get_engine', return_value=dict(engine(19), **changed)), \
                 patch('workflow_process.run_driver') as driver:
                result = workflow_process.execute_workflow(19, 7, definition())
            self.assertEqual(result['status'], 'failed')
            driver.assert_not_called()
        with patch('workflow_process.get_engine') as discovery, patch('workflow_process.run_driver') as driver:
            result = workflow_process.execute_workflow(19, 7, {'tasks': []})
        self.assertEqual(result['error_code'], 'WORKFLOW_INVALID')
        discovery.assert_not_called()
        driver.assert_not_called()

    def test_driver_materializes_summaries_before_stopping_session(self):
        order = []
        @contextmanager
        def session(*args):
            order.append('start')
            try:
                yield object()
            finally:
                order.append('stop')
        request = {'engine_id': 19, 'tenant_id': 7, 'connection': engine(19)['connection_info'],
                   'mode': 'workflow', 'workflow_def': definition(), 'input_data': {}}
        def serialize(value):
            self.assertEqual(order, ['start'])
            order.append('serialize')
            return {'status': 'success', 'final_result': 'summary'}
        with patch('workflow_driver.invocation_session', session), patch('workflow_driver.run_workflow', return_value={'frame': object()}), \
             patch('workflow_driver.serialize_workflow_value', side_effect=serialize):
            self.assertEqual(workflow_driver.execute_invocation(request)['final_result'], 'summary')
        self.assertEqual(order, ['start', 'serialize', 'stop'])

    def test_shared_session_is_rejected_and_mismatch_stops_it(self):
        builder = MagicMock()
        builder.appName.return_value = builder
        builder.master.return_value = builder
        builder.config.return_value = builder
        builder.getOrCreate.return_value.sparkContext.master = 'spark://wrong:7077'
        with patch('spark_connector.configure_spark_dependencies', return_value=builder), \
             patch.object(spark_connector.SparkSession, '_instantiatedSession', object()):
            with self.assertRaisesRegex(ValueError, 'fresh Driver'):
                with spark_connector.invocation_session(19, 7, engine(19)['connection_info']):
                    self.fail('reused a live session')
        builder.getOrCreate.assert_not_called()
        with patch.object(spark_connector.SparkSession, '_instantiatedSession', None), \
             patch.object(spark_connector.SparkContext, '_active_spark_context', object()):
            with self.assertRaisesRegex(ValueError, 'fresh Driver'):
                with spark_connector.invocation_session(19, 7, engine(19)['connection_info']):
                    self.fail('reused a live SparkContext')
        with patch('spark_connector.configure_spark_dependencies', return_value=builder), \
             patch.object(spark_connector.SparkSession, '_instantiatedSession', None):
            with self.assertRaisesRegex(ValueError, 'master mismatch'):
                with spark_connector.invocation_session(19, 7, engine(19)['connection_info']):
                    self.fail('wrong Master')
        builder.getOrCreate.return_value.stop.assert_called_once()
        with self.assertRaises(ValueError):
            spark_connector.current_session(19, 7)

    def test_serialized_workflow_double_stays_number_and_exact_values_stay_strings(self):
        from workflow_result import serialize_json_value
        body = serialize_json_value({'double': 1.25, 'long': 9007199254740993, 'decimal': Decimal('1.000000000000000001')})
        source = 'import json,pathlib,sys; json.load(sys.stdin); pathlib.Path(sys.argv[1]).write_text(' + repr(json.dumps(body)) + ')'
        result = run_driver([sys.executable, '-c', source], {}, timeout=5, parse_float=float)
        self.assertEqual(result, {'double': 1.25, 'long': '9007199254740993', 'decimal': '1.000000000000000001'})
        self.assertIs(type(result['double']), float)


if __name__ == '__main__':
    unittest.main()
