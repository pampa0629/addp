import os
import json
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch
from urllib.request import Request, urlopen

from addp_common.workflow_runtime import ExecutionRegistry, WorkflowRunner
import api_server
from workflow_engine import SparkWorkflowRunner, execute_workflow
from runtime_server import RuntimeApplication, register_after_ready, post_worker_init


def task(identifier, operator='preview', params=None, dependencies=None):
    return {'id': identifier, 'operator': operator, 'params': params or {}, 'depends_on': dependencies or []}


class WorkflowRuntimeTest(unittest.TestCase):
    def setUp(self):
        api_server.executions.clear()

    def test_domain_runner_uses_common_core_and_declared_ports_with_nested_inputs(self):
        self.assertTrue(issubclass(SparkWorkflowRunner, WorkflowRunner))
        captured = []
        def operator(**params):
            captured.append(params)
            return {'rows': 20}
        definition = {'tasks': [task('a', params={'limit': {'$input': 'limit'}}),
            task('b', params={'nested': [{'input': {'$ref': 'a', 'port': 'default'}}]}, dependencies=['a'])]}
        with patch('workflow_engine.get_operator', return_value=operator):
            result = execute_workflow(3, 2, definition, {'limit': 5})
        self.assertEqual(result['status'], 'success')
        self.assertEqual(captured, [{'limit': 5}, {'nested': [{'input': {'rows': 20}}]}])
        self.assertEqual(result['all_results'], {'a': {'rows': 20}, 'b': {'rows': 20}})
        self.assertEqual(result['final_result'], {'rows': 20})

    def test_invalid_dag_never_opens_spark_session_or_invokes_operator(self):
        for definition in (
            {'tasks': [task('a', dependencies=['b']), task('b', dependencies=['a'])]},
            {'tasks': [task('a', 'missing')]},
        ):
            with patch('workflow_engine.get_spark_connector') as connector, patch('workflow_engine.get_operator') as operator:
                result = execute_workflow(3, 2, definition)
            self.assertEqual(result['error_code'], 'WORKFLOW_INVALID')
            connector.assert_not_called()
            operator.assert_not_called()

    def test_multiport_outputs_match_metadata_and_refs(self):
        from operator_metadata import get_operator_metadata
        metadata = get_operator_metadata() + [{'id': 'split', 'execution_modes': ['workflow'],
                    'output_ports': [{'name': 'east'}, {'name': 'west'}]}]
        with patch('workflow_engine.get_operator_metadata', return_value=metadata), patch('workflow_engine.get_operator') as get:
            get.side_effect = [lambda **p: {'east': 10, 'west': 12}, lambda **p: p['limit']]
            result = execute_workflow(3, 2, {'tasks': [task('a', 'split'),
                task('b', params={'limit': {'$ref': 'a', 'port': 'west'}}, dependencies=['a'])]})
        self.assertEqual(result['final_result'], 12)
        self.assertEqual(result['all_results']['a'], {'east': 10, 'west': 12})
        with patch('workflow_engine.get_operator_metadata', return_value=metadata), patch('workflow_engine.get_operator', return_value=lambda **p: {'east': 1}):
            result = execute_workflow(3, 2, {'tasks': [task('a', 'split')]})
        self.assertEqual(result['error_code'], 'EXECUTION_FAILED')

    def test_synchronous_post_and_other_request_threads_share_registry_snapshots(self):
        self.assertIsInstance(api_server.executions, ExecutionRegistry)
        body = {'engine_id': 3, 'workflow_def': {'tasks': [task('a')]},
                'runtime': {'tenant_id': 2, 'execution_authorization': {'id': 1, 'effects': ['read']}}}
        def execute(*args):
            self.assertEqual(api_server.executions.get('00000000-0000-4000-8000-000000000008').status, 'running')
            return {'status': 'success', 'final_result': {'rows': 20}, 'all_results': {'a': {'rows': 20}}, 'task_order': ['a']}
        with patch('api_server.uuid.uuid4', return_value='00000000-0000-4000-8000-000000000008'), patch('api_server.execute_workflow', side_effect=execute), api_server.app.test_client() as client:
            response = client.post('/api/workflow', json=body)
        self.assertEqual(response.status_code, 200)
        identifier = response.json['execution_id']
        results = []
        def query():
            with api_server.app.test_client() as other_client:
                results.append(other_client.get('/api/executions/' + identifier))
        threads = [threading.Thread(target=query) for _ in range(8)]
        for thread in threads: thread.start()
        for thread in threads: thread.join()
        self.assertEqual(len(results), 8)
        for result in results:
            self.assertEqual(result.status_code, 200)
            self.assertEqual(result.json['status'], 'success')
            self.assertEqual(result.json['result'], response.json['final_result'])
            self.assertEqual(result.json['all_results'], response.json['all_results'])
            self.assertEqual(result.json['progress'], 100)
        snapshot = api_server.executions.get(identifier)
        snapshot.result['rows'] = 999
        self.assertEqual(api_server.executions.get(identifier).result, {'rows': 20})

    def test_invalid_execution_authorization_is_400_before_work(self):
        with api_server.app.test_client() as client, patch('api_server.execute_workflow') as execute:
            result = client.post('/api/workflow', json={'engine_id': 3, 'workflow_def': {'tasks': [task('a')]}})
        self.assertEqual(result.status_code, 400)
        self.assertEqual(result.json['error_code'], 'WORKFLOW_INVALID')
        execute.assert_not_called()


class RuntimeEntryTest(unittest.TestCase):
    def test_real_gunicorn_serves_health_and_status_during_workflow_and_registration(self):
        with socket.socket() as socket_fixture:
            socket_fixture.bind(('127.0.0.1', 0))
            port = socket_fixture.getsockname()[1]
        with tempfile.TemporaryDirectory(prefix='addp-spark-http-test-') as directory:
            root = Path(directory)
            launcher = root / 'fixture.py'
            launcher.write_text('''
import os
from pathlib import Path
import time
import api_server
from runtime_server import run
def register():
    root = Path(os.environ['ADDP_HTTP_TEST_ROOT'])
    root.joinpath('registration-started').touch()
    while not root.joinpath('system-ready').exists():
        time.sleep(0.01)
    root.joinpath('registered').touch()
api_server.register_to_system_with_retry = register
api_server.uuid.uuid4 = lambda: '00000000-0000-4000-8000-000000000008'
def execute(*args):
    root = Path(os.environ['ADDP_HTTP_TEST_ROOT'])
    root.joinpath('workflow-started').touch()
    while not root.joinpath('workflow-release').exists():
        time.sleep(0.01)
    return {'status': 'success', 'final_result': {'rows': 20},
            'all_results': {'a': {'rows': 20}}, 'task_order': ['a']}
api_server.execute_workflow = execute
run()
''')
            environment = dict(os.environ, PORT=str(port), WORKFLOW_BIND_HOST='127.0.0.1', ADDP_HTTP_TEST_ROOT=directory,
                               PYTHONPATH=os.pathsep.join([str(Path(__file__).parent.resolve()),
                                                          os.environ.get('PYTHONPATH', '')]))
            with (root / 'server.log').open('w') as log:
                process = subprocess.Popen([sys.executable, str(launcher)], env=environment, stdout=log, stderr=log)
                try:
                    base = f'http://127.0.0.1:{port}'
                    deadline = time.monotonic() + 15
                    while True:
                        if process.poll() is not None:
                            self.fail((root / 'server.log').read_text())
                        try:
                            with urlopen(base + '/health', timeout=1) as response:
                                self.assertEqual(response.status, 200)
                            if (root / 'registration-started').exists():
                                break
                        except OSError:
                            pass
                        self.assertLess(time.monotonic(), deadline, (root / 'server.log').read_text())
                        time.sleep(0.05)
                    self.assertFalse((root / 'registered').exists())
                    body = {'engine_id': 3, 'workflow_def': {'tasks': [task('a')]},
                            'runtime': {'tenant_id': 2, 'execution_authorization': {'id': 1, 'effects': ['read']}}}
                    request = Request(base + '/api/workflow', data=json.dumps(body).encode(),
                                      headers={'Content-Type': 'application/json'})
                    submissions, errors = [], []
                    def submit():
                        try:
                            with urlopen(request, timeout=10) as response:
                                submissions.append(json.load(response))
                        except Exception as error:
                            errors.append(error)
                    submission = threading.Thread(target=submit)
                    submission.start()
                    while not (root / 'workflow-started').exists():
                        self.assertLess(time.monotonic(), deadline, errors)
                        time.sleep(0.01)
                    for _ in range(8):
                        with urlopen(base + '/health', timeout=1) as response:
                            self.assertEqual(json.load(response)['status'], 'healthy')
                        with urlopen(base + '/api/executions/00000000-0000-4000-8000-000000000008', timeout=1) as response:
                            self.assertEqual(json.load(response)['status'], 'running')
                    self.assertTrue(submission.is_alive())
                    self.assertFalse((root / 'registered').exists())
                    (root / 'workflow-release').touch()
                    submission.join(timeout=3)
                    self.assertFalse(submission.is_alive())
                    self.assertEqual(errors, [])
                    self.assertEqual(len(submissions), 1)
                    submitted = submissions[0]
                    for _ in range(8):
                        with urlopen(base + '/api/executions/' + submitted['execution_id'], timeout=3) as response:
                            snapshot = json.load(response)
                        self.assertEqual(snapshot['result'], submitted['final_result'])
                        self.assertEqual(snapshot['status'], 'success')
                    (root / 'system-ready').touch()
                    while not (root / 'registered').exists():
                        self.assertLess(time.monotonic(), deadline)
                        time.sleep(0.01)
                finally:
                    (root / 'workflow-release').touch()
                    process.terminate()
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=5)

    def test_entry_has_one_worker_multiple_threads_no_preload(self):
        with patch.dict(os.environ, {'PORT': '28098', 'WORKFLOW_BIND_HOST': '127.0.0.1', 'GUNICORN_CMD_ARGS': '--workers 9'}):
            application = RuntimeApplication()
        self.assertEqual(application.cfg.bind, ['127.0.0.1:28098'])
        self.assertEqual(application.cfg.workers, 1)
        self.assertEqual(application.cfg.threads, 4)
        self.assertFalse(application.cfg.preload_app)
        self.assertEqual(application.cfg.post_worker_init, post_worker_init)

    def test_registration_waits_for_http_readiness_then_reuses_unbounded_retry(self):
        events = []
        class Response:
            status = 200
            def __enter__(self): return self
            def __exit__(self, *args): pass
        def ready(*args, **kwargs):
            events.append('probe')
            if len(events) == 1: raise OSError('not listening')
            return Response()
        with patch('runtime_server.urlopen', side_effect=ready), patch('runtime_server.time.sleep', side_effect=lambda s: events.append('wait')), patch('api_server.register_to_system_with_retry', side_effect=lambda: events.append('register')):
            register_after_ready(28098)
        self.assertEqual(events, ['probe', 'wait', 'probe', 'register'])

    def test_hook_is_nonblocking_daemon_in_worker(self):
        with patch('runtime_server.threading.Thread') as thread:
            post_worker_init(None)
        thread.assert_called_once()
        self.assertTrue(thread.call_args.kwargs['daemon'])
        thread.return_value.start.assert_called_once()

    def test_stable_runtime_address_uses_shared_advertised_port(self):
        with patch.dict(os.environ, {'PORT': '8098', 'RUNTIME_PUBLIC_PORT': '28098', 'RUNTIME_HOST': 'spark-workflow-engine'}), patch('addp_common.client.register_runtime_engine', return_value=(202, '')) as register:
            self.assertTrue(api_server.register_to_system())
        self.assertEqual(register.call_args.args[3]['connection_info'], {'protocol': 'http', 'host': 'spark-workflow-engine', 'port': 28098})


if __name__ == '__main__':
    unittest.main()
