"""Offline assertions for formal local Trace owner APIs; no services or credentials."""
from copy import deepcopy
import importlib
import json
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

M = importlib.import_module('scripts.test.monitor-traces-local-online')
BUSINESS_PERMISSIONS = {
    'orchestrator.workflow.create', 'orchestrator.workflow.read', 'orchestrator.workflow.delete',
    'orchestrator.workflow.execute', 'develop.task.create', 'develop.task.read', 'develop.task.delete',
    'develop.task.execute', 'develop.data_read.execute', 'monitor.execution.read',
    'system.execution_authorization.create',
}
STAMP = '2026-10-23T00:00:00Z'
EXECUTIONS = {
    ('orchestrator', 'success'): '11111111-1111-4111-8111-111111111111',
    ('develop', 'success'): '22222222-2222-4222-8222-222222222222',
    ('orchestrator', 'failure'): '33333333-3333-4333-8333-333333333333',
    ('develop', 'failure'): '44444444-4444-4444-8444-444444444444',
}


def identity(who):
    granted = ({'monitor.trace.read', 'platform.module_log.read', 'platform.module.read'} if who == 'ADMIN'
               else BUSINESS_PERMISSIONS if who == 'USER' else set())
    return {'principal': {'id': who, 'type': 'user'}, 'delegation': None,
            'context': {'type': 'platform' if who in ('ADMIN', 'SECURITY') else 'tenant', 'tenant_id': '7'},
            'token': {'type': 'first_party_access_token'}, 'authentication': {'assurance_level': 'aal2'},
            'authorization': {'role_assignments': [{'permissions': sorted(granted)}]}}


class Clock:
    def __init__(self): self.value = 0
    def __call__(self):
        self.value += 1
        return self.value


class FormalAPI:
    """Strict real-owner DTO fixtures with faults injected only in responses."""
    def __init__(self, fault=None):
        self.fault, self.calls, self.tasks, self.workflows, self.waits = fault, [], {}, {}, []
        self.instances = [{'id': number, 'module_name': module, 'role': 'backend', 'instance_id': module+'-instance'}
                          for number, module in enumerate(('orchestrator', 'develop'), 1)]
        self.logs = {}
        for number, ((module, label), execution_id) in enumerate(EXECUTIONS.items(), 1):
            self.logs[(module, execution_id)] = [self.entry(module, execution_id, phase, index, number,
                'failed' if label == 'failure' and phase == 'terminal' else 'success')
                for index, phase in enumerate(('enqueue', 'claim', 'dispatch', 'terminal'), 1)]

    @staticmethod
    def entry(module, execution_id, phase, index, number, outcome):
        body = {'time': STAMP, 'level': 'INFO', 'msg': 'execution.checkpoint', 'execution_id': execution_id,
                'phase': phase, 'attempt': 0 if phase == 'enqueue' else 1, 'outcome': outcome}
        return {'module_name': module, 'instance_id': module+'-instance', 'message': json.dumps(body),
                'trace_id': f'{number * 16 + index:032x}', 'span_id': f'{number * 16 + index:016x}'}

    def client(self, who):
        return SimpleNamespace(request=lambda method, path, expected, body=None: self.request(who, method, path, expected, body))

    def request(self, who, method, url, expected, body):
        path, query = urlsplit(url).path, parse_qs(urlsplit(url).query)
        self.calls.append((who, method, path, query, deepcopy(body)))
        status, payload = 200, {}
        if who != 'ADMIN' and ('/platform/traces/' in path or path.endswith('/logs')):
            status = 403
        elif path == '/api/v1/system/engines' and method == 'POST':
            assert who == 'PREPARER' and body['engine_type'] == 'postgresql'
            assert body['connection_info']['database'] == 'trace_fixture'
            status, payload = 201, {'id': 9}
        elif path == '/api/v1/system/engines/9/test':
            payload = {'success': self.fault != 'connection_failed'}
        elif path == '/api/v1/develop/task-definitions' and method == 'POST':
            assert who == 'USER' and set(body) == {'name', 'dev_type', 'content', 'execution_config', 'timeout', 'editor_layout'}
            assert body['dev_type'] == 'query' and body['execution_config'] == {'engine_id': 9}
            assert body['content'] in ({'query': 'SELECT 1 AS fixture_value', 'query_type': 'sql'},
                                       {'query': 'SELECT 1 / 0 AS fixture_value', 'query_type': 'sql'})
            label = 'failure' if '1 / 0' in body['content']['query'] else 'success'
            number = len(self.tasks) + 1; self.tasks[number] = label
            payload = {'id': number}  # Actual Develop create status is 200.
        elif path == '/api/v1/orchestrator/orchestrations' and method == 'POST':
            assert who == 'USER' and body['enabled'] is False and len(body['steps']) == 1
            step = body['steps'][0]
            assert step['provider'] == 'develop' and step['task_type'] == 'query' and step['parameters'] == {}
            number = len(self.workflows) + 1; self.workflows[number] = self.tasks[step['task_id']]
            status, payload = 201, {'id': number}
        elif path.endswith('/execute') and '/orchestrations/' in path:
            label = self.workflows[int(path.split('/')[-2])]
            status, payload = 202, {'execution_id': EXECUTIONS[('orchestrator', label)]}
        elif '/executions/' in path and path.endswith('/tree'):
            payload = {'children': [{'execution_id': EXECUTIONS[('develop', 'failure')]}]}
        elif '/executions/' in path:
            module, execution_id = path.split('/')[3], path.split('/')[-1]
            label = next(label for (owner, label), value in EXECUTIONS.items() if owner == module and value == execution_id)
            payload = {'execution_id': execution_id, 'status': 'failed' if label == 'failure' else 'success'}
            if module == 'orchestrator':
                payload['metadata'] = {'step_results': {'query': {'result': {'execution_id': EXECUTIONS[('develop', label)]}}}}
                if self.fault == 'wrong_parent_terminal' and label == 'failure': payload['status'] = 'success'
            else:
                payload['parent_execution_id'] = EXECUTIONS[('orchestrator', label)]
                if self.fault == 'wrong_child_parent': payload['parent_execution_id'] = EXECUTIONS[('orchestrator', 'success')]
            if self.fault == 'private_execution': payload['metadata'] = {'nested': {'observability': {}}}
        elif path == '/api/v1/system/platform/module-instances':
            assert who == 'ADMIN'
            rows = self.instances
            if 'ids' in query:
                ids = {int(value) for value in query['ids'][0].split(',')}
                rows = [row for row in rows if row['id'] in ids]
            else:
                assert query == {'page': ['1'], 'page_size': ['100']}
            payload = {'data': deepcopy(rows)}
        elif path.endswith('/logs'):
            module = path.split('/')[6]; execution_id = query['keyword'][0]
            entries = deepcopy(self.logs[(module, execution_id)])
            if 'trace_id' in query: entries = [entry for entry in entries if entry['trace_id'] == query['trace_id'][0]]
            if self.fault == 'duplicate_checkpoint': entries.append(deepcopy(entries[-1]))
            if self.fault == 'checkpoint_sensitive':
                value = json.loads(entries[0]['message']); value['query'] = 'private-query'; entries[0]['message'] = json.dumps(value)
            if self.fault == 'wrong_checkpoint_terminal':
                value = json.loads(entries[-1]['message']); value['outcome'] = 'success'; entries[-1]['message'] = json.dumps(value)
            payload = {'has_more': False, 'entries': entries}
        elif '/platform/traces/' in path:
            trace_id = path.split('/')[-1]
            matched = [(module, execution_id, entries) for (module, execution_id), entries in self.logs.items()
                       if entries[-1]['trace_id'] == trace_id]
            assert len(matched) == 1, 'Only exact failed terminal traces may be required by sampling assertions'
            module, execution_id, entries = matched[0]
            assert execution_id == EXECUTIONS[(module, 'failure')]
            instance = next(row for row in self.instances if row['module_name'] == module)
            links = [{'trace_id': entry['trace_id'], 'span_id': entry['span_id'], 'module_name': module, 'role': 'backend',
                      'instance_id': instance['instance_id'], 'runtime_instance_id': instance['id'], 'started_at': STAMP}
                     for entry in (entries[0], entries[2])]
            if self.fault == 'duplicate_links': links[1] = deepcopy(links[0])
            if self.fault == 'wrong_stage_link': links[1].update(trace_id=entries[1]['trace_id'], span_id=entries[1]['span_id'])
            span = {'span_id': entries[-1]['span_id'], 'scope': 'addp.execution', 'phase': 'terminal', 'outcome': 'failed',
                    'attempt': 1, 'start': STAMP, 'end': STAMP, 'links': links}
            if self.fault == 'wrong_trace_span': span['span_id'] = 'f' * 16
            payload = {'coverage': 'sampled', 'data': [{'trace_id': trace_id, 'spans': [span]}]}
        else:
            raise AssertionError('unexpected formal owner API '+method+' '+path)
        if status not in expected:
            raise M.API.SuiteError('unexpected_fixture_http_status_'+str(status))
        return M.API.Response(status, payload)

    def run(self):
        life = SimpleNamespace(run_id='local-trace-fixture', wait=lambda seconds: self.waits.append(seconds))
        env = {'POSTGRES_USER': 'fixture', 'POSTGRES_PASSWORD': 'fixture-private-password'}
        report = {}
        with patch.object(M, 'login', side_effect=lambda env, who, platform=False: (self.client(who), identity(who))), \
             patch.object(M.time, 'monotonic', new=Clock()), patch.object(M.time, 'sleep'):
            retained = M.run(life, env, report)
        return retained, report


class LocalExecutionTraceOwnerTests(unittest.TestCase):
    def test_real_owner_dtos_parent_child_and_natural_failure_sampling(self):
        server = FormalAPI()
        retained, report = server.run()
        self.assertEqual(retained['execution_id'], EXECUTIONS[('develop', 'failure')])
        self.assertEqual(retained['started_at'], STAMP)
        self.assertEqual(server.waits, [125])
        traced = [call for call in server.calls if '/platform/traces/' in call[2] and call[0] == 'ADMIN']
        self.assertEqual(len(traced), 2)
        self.assertTrue(report['assertions']['tenant_and_wrong_platform_denied'])
        safe_report = json.dumps(report)
        for secret in ('fixture-private-password', 'SELECT ', 'private-query'):
            self.assertNotIn(secret, safe_report)

    def test_wrong_terminal_lineage_private_facts_and_exact_trace_are_rejected(self):
        for fault in ('connection_failed', 'wrong_parent_terminal', 'wrong_child_parent', 'private_execution',
                      'duplicate_checkpoint', 'checkpoint_sensitive', 'wrong_checkpoint_terminal',
                      'wrong_trace_span', 'duplicate_links', 'wrong_stage_link'):
            with self.subTest(fault=fault), self.assertRaises(M.API.SuiteError):
                FormalAPI(fault).run()

    def test_normalized_checkpoint_requires_nonzero_real_context_and_minimal_body(self):
        instance = FormalAPI().instances[0]
        entry = FormalAPI.entry('orchestrator', EXECUTIONS[('orchestrator', 'failure')], 'terminal', 1, 1, 'failed')
        for key, value in [('trace_id', '0' * 32), ('span_id', '0' * 16)]:
            bad = dict(entry, **{key: value})
            client = SimpleNamespace(request=lambda *args, **kwargs: M.API.Response(200, {'entries': [bad]}))
            with self.subTest(key=key), self.assertRaises(M.API.SuiteError):
                M.checkpoint_rows(client, instance, EXECUTIONS[('orchestrator', 'failure')], STAMP, STAMP)

    def test_real_cookie_jars_are_separate_and_formal_logout_rejects_old_tokens(self):
        from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
        from threading import Thread
        issued={}; revoked=set()
        class Handler(BaseHTTPRequestHandler):
            def log_message(self,*args): pass
            def send(self,status,payload=None,cookie=None):
                self.send_response(status)
                self.send_header('Content-Type','application/json')
                if cookie:self.send_header('Set-Cookie',cookie)
                self.end_headers()
                if payload is not None:self.wfile.write(json.dumps(payload).encode())
            def do_POST(self):
                self.rfile.read(int(self.headers.get('Content-Length','0')))
                if self.path=='/api/v1/system/login':
                    token='addp_at_fixture_'+str(len(issued)+1); cookie='refresh_'+str(len(issued)+1)
                    issued[token]=cookie
                    self.send(200,{'next_action':'session_issued','session':{'access_token':token}},
                              'local_refresh='+cookie+'; HttpOnly; Path=/api/v1/system; SameSite=Strict')
                elif self.path=='/api/v1/system/logout':
                    token=self.headers.get('Authorization','').removeprefix('Bearer ')
                    if self.headers.get('Cookie')!='local_refresh='+issued.get(token,'absent'):
                        self.send(401,{});return
                    revoked.add(token);self.send(204)
                else:self.send(404,{})
            def do_GET(self):
                token=self.headers.get('Authorization','').removeprefix('Bearer ')
                self.send(401 if token in revoked else 200,{} if token in revoked else identity('USER'))
        server=ThreadingHTTPServer(('127.0.0.1',0),Handler)
        thread=Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            env={'GATEWAY_URL':'http://127.0.0.1:'+str(server.server_port),
                 'ADDP_EXECUTION_TRACE_USER_USERNAME':'fixture','ADDP_EXECUTION_TRACE_USER_PASSWORD':'fixture-private-password',
                 'ADDP_EXECUTION_TRACE_TENANT_ID':'7'}
            clients=[]
            for _ in range(4):
                client,context=M.login(env,'USER');clients.append(client)
                self.assertEqual(context['context']['tenant_id'],'7')
            self.assertEqual(len({id(c.browser_opener) for c in clients}),4)
            life=SimpleNamespace(local_clients=clients,local_definitions=[],phase='test')
            report={'assertions':{}}
            M.finish(life,env,report)
            self.assertEqual(len(revoked),4)
            self.assertTrue(report['assertions']['logout_old_session_denied'])
        finally:
            server.shutdown();server.server_close();thread.join(timeout=2)


if __name__ == '__main__':
    unittest.main()
