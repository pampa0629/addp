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
               else BUSINESS_PERMISSIONS if who == 'USER' else {'iam.tenant_role.read','iam.tenant_role_assignment.create'} if who == 'PREPARER' else set())
    return {'principal': {'id': who, 'type': 'user'}, 'delegation': None,
            'context': {'type': 'platform' if who in ('ADMIN', 'SECURITY') else 'tenant', 'tenant_id': '7', 'tenant_membership_id': '9'},
            'client': {'scope_mode':'unrestricted'}, 'token': {'type': 'first_party_access_token'}, 'authentication': {'assurance_level': 'aal2'},
            'authorization': {'role_assignments': [{'role_key': 'tenant.administrator' if who=='PREPARER' else 'local.execution_trace', 'permissions': sorted(granted)}]}}


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
        if path=='/api/v1/system/auth/context':
            payload=identity(who)
        elif who != 'ADMIN' and ('/platform/traces/' in path or path.endswith('/logs')):
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
        elif path.endswith('/events'):
            payload={'items':[]}
        elif '/monitor/executions/by-execution-id/' in path and not path.endswith('/tree'):
            payload={'execution_id':path.split('/')[-1],'status':'failed'}
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
                       if any(entry['trace_id'] == trace_id for entry in entries)]
            assert len(matched) == 1, 'Only exact failed terminal traces may be required by sampling assertions'
            module, execution_id, entries = matched[0]
            if entries[0]['trace_id'] == trace_id:
                return M.API.Response(404, {'error_code':'trace_not_found'})
            assert execution_id == EXECUTIONS[(module, 'failure')]
            instance = next(row for row in self.instances if row['module_name'] == module)
            links = [{'trace_id': entry['trace_id'], 'span_id': entry['span_id'], 'module_name': module, 'role': 'backend',
                      'instance_id': instance['instance_id'], 'runtime_instance_id': instance['id'], 'started_at': STAMP}
                     for entry in (entries[0], entries[2])]
            if self.fault == 'duplicate_links': links[1] = deepcopy(links[0])
            if self.fault == 'wrong_stage_link': links[1].update(trace_id=entries[1]['trace_id'], span_id=entries[1]['span_id'])
            span = {'module_name':module,'instance_id':instance['instance_id'],'role':'backend','parent_span_id':'','kind':'internal','span_id': entries[-1]['span_id'], 'scope': 'addp.execution', 'phase': 'terminal', 'outcome': 'failed',
                    'attempt': 1, 'start': STAMP, 'end': STAMP, 'duration_ms': 0, 'links': links}
            if self.fault == 'wrong_trace_span': span['span_id'] = 'f' * 16
            payload = {'coverage': 'sampled', 'data': [{'trace_id': trace_id, 'spans': [span]}]}
        else:
            raise AssertionError('unexpected formal owner API '+method+' '+path)
        if status not in expected:
            raise M.API.SuiteError('unexpected_fixture_http_status_'+str(status))
        return M.API.Response(status, payload)

    def stage_read(self,client,life,execution_id):
        owner,label=next(key for key,value in EXECUTIONS.items() if value==execution_id)
        stage={'slot':'latest','state':'available','phase':'terminal','attempt':1,
               'outcome':'failed' if label=='failure' else 'success','start':STAMP,'end':STAMP,'duration_ms':0}
        value={'execution_id':execution_id,'supported':True,'stages':[{'slot':'enqueue','state':'not_found'},stage]}
        return M.validate_tenant_stages(value,execution_id),'request-'+owner

    def run(self):
        life = SimpleNamespace(run_id='local-trace-fixture', wait=lambda seconds: self.waits.append(seconds))
        env = {'POSTGRES_USER': 'fixture', 'POSTGRES_PASSWORD': 'fixture-private-password'}
        report = {}
        with patch.object(M, 'login', side_effect=lambda env, who, platform=False, life=None: (self.client(who), identity(who))), \
             patch.object(M,'prepare_engine_authority',side_effect=lambda client,context,env,life,report:context), \
             patch.object(M.time, 'monotonic', new=Clock()), patch.object(M.time, 'sleep'):
            with patch.object(M,'tenant_stage_read',side_effect=self.stage_read):
                retained = M.run(life, env, report)
        return retained, report


class PreparationMFAContractTests(unittest.TestCase):
    def exercise(self,fault=None):
        from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
        from threading import Thread
        from datetime import datetime,timezone,timedelta
        import time
        state={'stage':'base','old_denials':0,'assignment':False,'browser_cookie_checks':0}
        initial=identity('PREPARER')
        secret='GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ'
        class Handler(BaseHTTPRequestHandler):
            def log_message(self,*args):pass
            def send(self,status,payload,cookie=None):
                self.send_response(status);self.send_header('Content-Type','application/json')
                if cookie:self.send_header('Set-Cookie','local_refresh='+cookie+'; HttpOnly; Path=/api/v1/system; SameSite=Strict')
                self.end_headers();self.wfile.write(json.dumps(payload).encode())
            def context(self,final=False):
                value=deepcopy(initial)
                value['authentication']={'assurance_level':'aal1' if fault=='wrong_aal' else 'aal2',
                    'step_up_expires_at':(datetime.now(timezone.utc)+timedelta(hours=-1 if fault=='expired_step_up' else 1)).isoformat()}
                if final:
                    value['authorization']['role_assignments'].append({'role_key':M.PREPARATION_ROLE,'permissions':sorted(M.PREPARATION_PERMISSIONS)})
                    if fault=='extra_permission':value['authorization']['role_assignments'][-1]['permissions'].append('system.extra.read')
                return value
            def do_GET(self):
                token=self.headers.get('Authorization','').removeprefix('Bearer ')
                if self.path=='/api/v1/system/auth/context':
                    if (token=='addp_at_base' and state['stage']!='base') or (token=='addp_at_mfa' and state['stage']=='assigned'):
                        state['old_denials']+=1;self.send(401,{})
                    else:self.send(200,self.context(final=token=='addp_at_refreshed'))
                elif self.path=='/api/v1/system/tenant/roles':
                    granted=set(M.PREPARATION_PERMISSIONS)
                    if fault=='role_drift':granted.add('system.extra.read')
                    self.send(200,[{'id':'20','role_key':M.PREPARATION_ROLE,'role_type':'tenant_builtin','immutable':True,'permission_keys':sorted(granted)}])
                else:self.send(404,{})
            def do_POST(self):
                body=json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))))
                token=self.headers.get('Authorization','').removeprefix('Bearer ')
                if self.path=='/api/v1/system/auth/mfa/totp-enrollments':
                    assert token=='addp_at_base' and body=={'current_password':'fixture-private-password'}
                    self.send(201,{'enrollment_token':'private-enrollment','secret':secret})
                elif self.path=='/api/v1/system/auth/mfa/totp-enrollment-verifications':
                    assert token=='addp_at_base' and self.headers.get('Cookie')=='local_refresh=base-cookie'
                    assert body=={'enrollment_token':'private-enrollment','code':'287082'}
                    state['browser_cookie_checks']+=1;state['stage']='mfa'
                    self.send(200,{'access_token':'addp_at_base' if fault=='unrotated' else 'addp_at_mfa'},'mfa-cookie')
                elif self.path=='/api/v1/system/tenant/role_assignments':
                    assert token=='addp_at_mfa' and body['membership_id']=='9' and body['role_ids']==['20'] and 'valid_until' in body
                    state['assignment']=True;state['stage']='assigned'
                    self.send(201,[{'role_key':M.PREPARATION_ROLE,'membership_id':'10' if fault=='wrong_membership' else '9'}])
                elif self.path=='/api/v1/system/refresh':
                    assert self.headers.get('Cookie')=='local_refresh=mfa-cookie'
                    state['browser_cookie_checks']+=1
                    self.send(200,{'access_token':'addp_at_refreshed'},'refreshed-cookie')
                else:self.send(404,{})
        server=ThreadingHTTPServer(('127.0.0.1',0),Handler);thread=Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            import http.cookiejar,urllib.request
            base='http://127.0.0.1:'+str(server.server_port)
            client=M.API.GatewayClient(base,'addp_at_base',10)
            jar=http.cookiejar.CookieJar()
            client.browser_opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(jar))
            jar.set_cookie(http.cookiejar.Cookie(0,'local_refresh','base-cookie',None,False,'127.0.0.1',False,False,'/api/v1/system',True,False,None,True,None,None,{},False))
            life=SimpleNamespace(started=time.monotonic(),request_timeout=lambda:10,check=lambda:None,wait=lambda seconds:None)
            report={'assertions':{}}
            with patch.object(M.time,'time',return_value=59):
                final=M.prepare_engine_authority(client,initial,{'ADDP_EXECUTION_TRACE_PREPARER_PASSWORD':'fixture-private-password'},life,report)
            self.assertEqual(state['old_denials'],2)
            self.assertEqual(state['browser_cookie_checks'],2)
            self.assertTrue(state['assignment'])
            self.assertEqual(M.permissions(final),M.permissions(initial)|M.PREPARATION_PERMISSIONS)
            self.assertEqual(client.token,'addp_at_refreshed')
            self.assertTrue(report['assertions']['preparer_real_mfa_and_builtin_role'])
            for private in (secret,'private-enrollment','fixture-private-password','addp_at_','cookie'):
                self.assertNotIn(private,json.dumps(report))
        finally:
            server.shutdown();server.server_close();thread.join(timeout=2)

    def test_real_wire_cookie_rotation_mfa_role_assignment_and_refreshed_context(self):self.exercise()

    def test_failed_mfa_role_boundary_and_session_replacement_are_rejected(self):
        for fault in ('wrong_aal','expired_step_up','unrotated','role_drift','wrong_membership','extra_permission'):
            with self.subTest(fault=fault),self.assertRaises(M.API.SuiteError):self.exercise(fault)

    def test_expected_preparation_role_matches_product_manifest(self):
        from pathlib import Path
        text=(Path(M.__file__).resolve().parents[2]/'system/authorization/builtin_roles.yaml').read_text()
        block=text.split('  - key: '+M.PREPARATION_ROLE+'\n',1)[1].split('  - key: ',1)[0]
        permissions={line.strip().removeprefix('- ') for line in block.split('    permissions:\n',1)[1].splitlines() if line.strip()}
        self.assertEqual(permissions,M.PREPARATION_PERMISSIONS)


class LocalExecutionTraceOwnerTests(unittest.TestCase):
    def test_execution_failure_diagnostic_keeps_codes_and_drops_raw_error_sql_and_data(self):
        item={'execution_id':EXECUTIONS[('orchestrator','success')],'status':'failed',
            'error_details':{'code':'orchestrator.execution.contract_unavailable','message':'PRIVATE SQL PASSWORD',
                'diagnostic':{'stage':'preparation','reason':'data_protection_not_ready','context':'PRIVATE SQL PASSWORD'}},
            'metadata':{'step_results':{'query':{'status':'failed','phase':'terminal','error_code':'orchestrator.execution.contract_unavailable','error':'PRIVATE SQL PASSWORD','result':{'rows':['PRIVATE']}}}}}
        value=M.execution_failure_diagnostic(item)
        self.assertEqual(value['error_code'],'orchestrator.execution.contract_unavailable')
        self.assertEqual(value['query_step']['phase'],'terminal')
        self.assertEqual(value['diagnostic'],{'stage':'preparation','reason':'data_protection_not_ready'})
        self.assertNotIn('PRIVATE',json.dumps(value));self.assertNotIn('result',json.dumps(value))
        item['error_details']['code']='PRIVATE SQL PASSWORD'
        self.assertEqual(M.execution_failure_diagnostic(item)['error_code'],'unavailable')

    def test_safe_assertion_and_transport_diagnostics_exclude_private_text(self):
        with self.assertRaisesRegex(M.LocalTraceAssertionFailure,'^local_trace_assertion_failed$'):
            M.require(False,'password=PRIVATE-SENTINEL')
        def rejected(*args,**kwargs):raise M.API.SuiteError('POST /api/v1/system/engines returned HTTP 403 (permission_denied)')
        client=SimpleNamespace(request=rejected)
        life=SimpleNamespace(request_timeout=lambda:10,check=lambda:None)
        M.guard_client(client,life)
        with self.assertRaises(M.API.SuiteError):client.request('POST','/owned',(201,),{})
        self.assertEqual(life.api_failure,{'http_status':403,'error_code':'permission_denied'})

    def test_guard_failure_stops_http_before_any_new_business_request(self):
        calls=[]
        client=SimpleNamespace(request=lambda *a,**k:calls.append(a))
        def failed():raise RuntimeError('resource_guard_failed')
        life=SimpleNamespace(request_timeout=failed,check=lambda:None)
        M.guard_client(client,life)
        with self.assertRaisesRegex(RuntimeError,'resource_guard_failed'):
            client.request('POST','/owned-mutation',(201,),{})
        self.assertEqual(calls,[])

    def test_real_owner_dtos_parent_child_and_natural_failure_sampling(self):
        server = FormalAPI()
        retained, report = server.run()
        self.assertEqual(retained['platform']['execution_id'], EXECUTIONS[('develop', 'failure')])
        self.assertEqual(retained['platform']['started_at'], STAMP)
        self.assertEqual(server.waits, [125])
        traced = [call for call in server.calls if '/platform/traces/' in call[2] and call[0] == 'ADMIN']
        self.assertEqual(len(traced), 4)
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
            life=SimpleNamespace(local_clients=clients,local_definitions=[],phase='test',
                                 request_timeout=lambda:10,check=lambda:None)
            for _ in range(4):
                client,context=M.login(env,'USER',life=life);clients.append(client)
                self.assertEqual(context['context']['tenant_id'],'7')
            self.assertEqual(len({id(c.browser_opener) for c in clients}),4)
            report={'assertions':{}}
            M.finish(life,env,report)
            self.assertEqual(len(revoked),4)
            self.assertTrue(report['assertions']['logout_old_session_denied'])
        finally:
            server.shutdown();server.server_close();thread.join(timeout=2)


if __name__ == '__main__':
    unittest.main()

class TenantStageAcceptanceTests(unittest.TestCase):
    def value(self):
        return {'execution_id':EXECUTIONS[('develop','failure')],'supported':True,'stages':[
            {'slot':'enqueue','state':'not_found'},
            {'slot':'latest','state':'available','phase':'terminal','attempt':1,'outcome':'failed',
             'start':STAMP,'end':STAMP,'duration_ms':0}]}

    def test_closed_dto_rejects_private_fields_missing_time_and_false_normal_absence(self):
        value=self.value()
        self.assertEqual(M.validate_tenant_stages(value,value['execution_id']),value)
        for mutate in (
            lambda v:v.update(trace_id='private'),lambda v:v.update(supported=False),
            lambda v:v['stages'].reverse(),lambda v:v['stages'][0].update(state='not_recorded'),
            lambda v:v['stages'][0].update(state='outside_window'),
            lambda v:v['stages'][0].update(attempt=0),
            lambda v:v['stages'][1].update(instance_id='private'),
            lambda v:v['stages'][1].update(attempt=True),lambda v:v['stages'][1].update(duration_ms=float('nan')),
            lambda v:v['stages'][1].update(phase='enqueue'),lambda v:v['stages'][1].update(start='bad'),
            lambda v:v['stages'][1].update(end='2026-10-22T00:00:00Z'),lambda v:v['stages'][1].update(outcome='PRIVATE'),
        ):
            candidate=deepcopy(value);mutate(candidate)
            with self.subTest(value=candidate),self.assertRaises(M.LocalTraceAssertionFailure):
                M.validate_tenant_stages(candidate,value['execution_id'])

    def test_real_wire_get_is_bounded_no_store_request_id_and_no_query_or_body(self):
        from http.server import BaseHTTPRequestHandler,ThreadingHTTPServer
        from threading import Thread
        import urllib.request
        state={'fault':None,'count':0};value=self.value()
        class Handler(BaseHTTPRequestHandler):
            def log_message(self,*args):pass
            def do_GET(self):
                state['count']+=1
                assert self.path=='/api/v1/monitor/executions/by-execution-id/'+value['execution_id']+'/trace-stages'
                assert self.headers.get('Authorization')=='Bearer private-token'
                assert self.headers.get('Content-Length') is None
                self.send_response(503 if state['fault']=='upstream' else 200)
                self.send_header('Cache-Control','public' if state['fault']=='cache' else 'no-store')
                self.send_header('X-Request-ID','' if state['fault']=='request_id' else 'request-identity')
                self.end_headers()
                raw=b'X'*8193 if state['fault']=='budget' else b'not-json' if state['fault']=='json' else json.dumps(value).encode()
                self.wfile.write(raw)
        server=ThreadingHTTPServer(('127.0.0.1',0),Handler);thread=Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            client=SimpleNamespace(base_url='http://127.0.0.1:'+str(server.server_port),token='private-token',
                                   browser_opener=urllib.request.build_opener(urllib.request.ProxyHandler({})))
            life=SimpleNamespace(request_timeout=lambda:5,check=lambda:None,stage_observation_deadline=M.time.monotonic()+90)
            actual,request_id=M.tenant_stage_read(client,life,value['execution_id'])
            self.assertEqual(actual,value);self.assertEqual(request_id,'request-identity')
            for fault in ('upstream','cache','request_id','budget','json'):
                state['fault']=fault
                with self.subTest(fault=fault),self.assertRaises(M.LocalTraceAssertionFailure):
                    M.tenant_stage_read(client,life,value['execution_id'])
            self.assertEqual(state['count'],6) # no hidden retry
        finally:server.shutdown();server.server_close();thread.join(timeout=2)

    def test_sampling_candidate_shortfall_and_mismatched_terminal_never_pass(self):
        for fault in ('all_sampled','wrong_terminal'):
            server=FormalAPI();original=server.stage_read
            def read(client,life,execution_id):
                value,request_id=original(client,life,execution_id)
                if fault=='all_sampled':value['stages'][0]={'slot':'enqueue','state':'available','phase':'enqueue','attempt':0,
                    'outcome':'success','start':STAMP,'end':STAMP,'duration_ms':0}
                else:value['stages'][1]['duration_ms']=1
                return value,request_id
            server.stage_read=read
            with self.subTest(fault=fault),self.assertRaises(M.LocalTraceAssertionFailure):server.run()
            self.assertEqual(len(server.tasks),2);self.assertEqual(len(server.workflows),2)

    def test_shared_sampling_and_stage_window_and_four_browser_cases(self):
        server=FormalAPI();result,report=server.run()
        self.assertEqual(len(result['tenant']['cases']),4)
        self.assertEqual({(r['module'],r['kind'])for r in result['tenant']['cases']},
                         {(m,k)for m in ('orchestrator','develop')for k in ('readable','missing')})
        self.assertNotIn('trace_id',json.dumps(report));self.assertNotIn('span_id',json.dumps(report))
        self.assertNotIn('instance_id',json.dumps(result['tenant']))
        self.assertTrue(report['assertions']['tenant_stage_readable_exact'])
        self.assertTrue(report['assertions']['tenant_stage_normal_missing'])

    def test_six_round_cap_never_reexecutes_business_to_manufacture_a_sample(self):
        server=FormalAPI();original=server.stage_read;calls=[]
        def missing(client,life,execution_id):
            calls.append(execution_id);value,request_id=original(client,life,execution_id)
            if len(calls)<=12:value['stages'][1]={'slot':'latest','state':'not_found'}
            return value,request_id
        server.stage_read=missing
        with self.assertRaisesRegex(M.LocalTraceAssertionFailure,'tenant_stage_terminal_mismatch'):server.run()
        self.assertEqual(len(calls),14)
        self.assertEqual(len(server.tasks),2);self.assertEqual(len(server.workflows),2)

    def test_chain_correlates_exact_owner_and_fails_on_missing_or_extra_route(self):
        rows=[{'module_name':name,'role':'ingress' if name=='gateway' else 'backend','instance_id':name+'-instance'}
              for name in ('gateway','monitor','orchestrator','develop')]
        routes={'gateway':'/api/v1/:module/*path','monitor':'/api/v1/monitor/executions/by-execution-id/:execution_id/trace-stages',
                'orchestrator':'/api/v1/orchestrator/execution-read-scope','develop':'/api/v1/develop/execution-read-scope'}
        for fault in (None,'other_owner','missing','pagination','status'):
            def request(method,path,expected):
                if '/module-instances?' in path:return M.API.Response(200,{'data':rows})
                module=path.split('/')[6]
                body={'msg':'http.request.completed','request_id':'request-one','route':routes[module],'method':'GET','status':200}
                if fault=='status':body['status']=503
                entries=[] if module=='develop' else [{'message':json.dumps(body)}]
                if fault=='other_owner' and module=='develop':entries=[{'message':json.dumps(body)}]
                if fault=='missing' and module=='orchestrator':entries=[]
                return M.API.Response(200,{'has_more':fault=='pagination','entries':entries})
            life=SimpleNamespace(tenant_stage_requests=[('orchestrator','request-one')],tenant_stage_log_start=STAMP)
            report={'assertions':{}}
            if fault:
                with self.subTest(fault=fault),self.assertRaises(M.LocalTraceAssertionFailure):M.tenant_stage_request_chain(SimpleNamespace(request=request),life,report)
            else:
                M.tenant_stage_request_chain(SimpleNamespace(request=request),life,report)
                self.assertTrue(report['assertions']['tenant_stage_gateway_monitor_single_owner'])

    def test_original_observation_deadline_is_not_reset_by_an_owner_read(self):
        calls=[]
        client=SimpleNamespace(request=lambda *args: calls.append(args))
        life=SimpleNamespace(request_timeout=lambda:10,check=lambda:None,phase='tenant_stage_observations',stage_observation_deadline=90)
        M.guard_client(client,life)
        with patch.object(M.time,'monotonic',return_value=89):client.request('GET','/owned',(200,))
        self.assertEqual(client.timeout,1)
        with patch.object(M.time,'monotonic',return_value=90),self.assertRaisesRegex(M.LocalTraceAssertionFailure,'observation_timeout'):
            client.request('GET','/owned',(200,))
        self.assertEqual(len(calls),1)

    def test_formal_cleanup_attempts_every_session_even_after_a_definition_failure(self):
        calls=[]
        class Reply:
            status=204
            def __enter__(self):return self
            def __exit__(self,*args):pass
        class Opener:
            def open(self,request,**kwargs):calls.append(('logout',request.headers['Authorization']));return Reply()
        def request(method,path,expected):
            calls.append((method,path))
            if method=='DELETE' and path.endswith('/2'):raise M.API.SuiteError('private error not archived')
            return M.API.Response(expected[0],{})
        clients=[SimpleNamespace(token='private-'+str(i),browser_opener=Opener(),request=request) for i in range(4)]
        life=SimpleNamespace(local_clients=clients,local_definitions=[('/definition/',1),('/definition/',2)],request_timeout=lambda:5)
        report={'assertions':{}}
        with self.assertRaisesRegex(M.LocalTraceAssertionFailure,'formal_cleanup_incomplete'):
            M.finish(life,{'GATEWAY_URL':'http://fixture'},report)
        self.assertEqual(sum(call[0]=='logout' for call in calls),4)
        self.assertIn(('DELETE','/definition/1'),calls)
        self.assertFalse(report['assertions']['formal_definitions_deleted'])
        self.assertTrue(report['assertions']['logout_old_session_denied'])
        self.assertNotIn('private',json.dumps(report))
