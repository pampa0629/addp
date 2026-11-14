"""Owner API assertions for the manually admitted local execution Trace gate."""
from __future__ import annotations

import base64
from datetime import datetime, timezone, timedelta
import hashlib
import hmac
import http.cookiejar
import importlib
import json
import math
import os
from pathlib import Path
import stat
import re
import struct
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

sys.path.insert(0, str(__import__('pathlib').Path(__file__).resolve().parents[2]))
API = importlib.import_module('scripts.utils.online-api')


class LocalTraceAssertionFailure(API.SuiteError):
    def __init__(self,code):
        super().__init__(code if re.fullmatch('[a-z0-9_]+',code) else 'local_trace_assertion_failed')


def require(value, code):
    if not value:
        raise LocalTraceAssertionFailure(code)


def utc(value=None):
    return (value or datetime.now(timezone.utc)).isoformat(timespec='microseconds').replace('+00:00', 'Z')


def permissions(context):
    return {p for a in context.get('authorization', {}).get('role_assignments', []) for p in a.get('permissions', [])}


def guard_client(client, life):
    request = client.request
    def guarded(*args, **kwargs):
        if hasattr(life,"before_api_request"): life.before_api_request(args[1] if len(args)>1 else kwargs.get("path",""))
        client.timeout = life.request_timeout()
        if getattr(life,'phase','') in ('natural_tail_sampling','tenant_stage_observations') and hasattr(life,'stage_observation_deadline'):
            remaining=life.stage_observation_deadline-time.monotonic()
            require(remaining>0,'tenant_stage_observation_timeout')
            client.timeout=min(client.timeout,5,remaining)
        try:
            reply = request(*args, **kwargs)
        except API.SuiteError as error:
            matched=re.fullmatch(r'(?:GET|POST|PUT|DELETE) /\S+ returned HTTP ([1-5][0-9]{2}) \(([a-z][a-z0-9_]*)\)',str(error))
            if matched:life.api_failure={'http_status':int(matched[1]),'error_code':matched[2]}
            raise
        life.check()
        return reply
    client.request = guarded
    return client


PREPARATION_ROLE = 'tenant.infrastructure_administrator'
PREPARATION_PERMISSIONS = {
    'monitor.health.read', 'system.cleanup.execute', 'system.cleanup.read',
    'system.engine.create', 'system.engine.delete', 'system.engine.execute', 'system.engine.read', 'system.engine.update',
    'system.engine_catalog.read', 'system.engine_raster_policy.read', 'system.engine_raster_policy.update',
}


def totp(secret, life=None):
    require(isinstance(secret,str) and re.fullmatch('[A-Z2-7]{16,128}',secret),'totp_secret_contract_invalid')
    remaining=30-time.time()%30
    if remaining<2:
        life.wait(remaining+.1) if life else time.sleep(remaining+.1)
    counter=struct.pack('>Q',int(time.time())//30)
    digest=hmac.new(base64.b32decode(secret),counter,hashlib.sha1).digest();offset=digest[-1]&15
    return str((struct.unpack('>I',digest[offset:offset+4])[0]&0x7fffffff)%1000000).zfill(6)


def browser_json(client,life,path,body,expected=(200,)):
    if hasattr(life,"before_api_request"): life.before_api_request(path)
    request=urllib.request.Request(client.base_url+path,data=json.dumps(body).encode(),method='POST',
        headers={'Content-Type':'application/json','Accept':'application/json','Authorization':'Bearer '+client.token})
    try:
        response=client.browser_opener.open(request,timeout=life.request_timeout())
    except urllib.error.HTTPError as error:
        response=error
    with response:
        status=response.status
        payload=json.load(response)
    life.check()
    if status not in expected:
        code=payload.get('error_code','unknown') if isinstance(payload,dict) else 'unknown'
        life.api_failure={'http_status':status,'error_code':code if isinstance(code,str) and re.fullmatch('[a-z][a-z0-9_]*',code) else 'unknown'}
        raise LocalTraceAssertionFailure('formal_browser_session_http_status')
    require(isinstance(payload,dict),'formal_browser_session_response_invalid')
    return payload


def replace_browser_token(client,reply):
    token=reply.get('access_token')
    require(isinstance(token,str) and token.startswith('addp_at_') and token!=client.token,'formal_browser_token_not_rotated')
    client.token=token


def prepare_engine_authority(client,identity,env,life,report):
    life.phase='preparer_real_totp_enrollment'
    require({a.get('role_key') for a in identity['authorization']['role_assignments']}=={'tenant.administrator'},
            'preparer_must_begin_as_initial_administrator')
    require(not {'system.engine.create','system.engine.execute'}.intersection(permissions(identity)),
            'preparer_engine_authority_must_not_be_pregranted')
    membership=identity.get('context',{}).get('tenant_membership_id')
    require(isinstance(membership,str) and re.fullmatch('[1-9][0-9]*',membership),'preparer_membership_missing')
    enrolled=browser_json(client,life,'/api/v1/system/auth/mfa/totp-enrollments',
        {'current_password':env['ADDP_EXECUTION_TRACE_PREPARER_PASSWORD']},(201,))
    verified=browser_json(client,life,'/api/v1/system/auth/mfa/totp-enrollment-verifications',
        {'enrollment_token':enrolled['enrollment_token'],'code':totp(enrolled.get('secret'),life)})
    client.request('GET','/api/v1/system/auth/context',(401,))
    replace_browser_token(client,verified)
    current=client.request('GET','/api/v1/system/auth/context',(200,)).payload
    require(current.get('principal')==identity['principal'] and current.get('context')==identity['context'],
            'preparer_mfa_changed_identity')
    facts=current.get('authentication',{})
    expiry=facts.get('step_up_expires_at')
    require(facts.get('assurance_level')=='aal2' and isinstance(expiry,str) and
            datetime.fromisoformat(expiry.replace('Z','+00:00'))>datetime.now(timezone.utc),'preparer_real_step_up_missing')
    life.phase='preparer_formal_builtin_role_assignment'
    rows=client.request('GET','/api/v1/system/tenant/roles',(200,),response_type=list).payload
    selected=[row for row in rows if row.get('role_key')==PREPARATION_ROLE]
    require(len(selected)==1 and selected[0].get('role_type')=='tenant_builtin' and selected[0].get('immutable') is True and
            set(selected[0].get('permission_keys',[]))==PREPARATION_PERMISSIONS,'preparer_builtin_role_contract_drift')
    role_id=selected[0].get('id')
    require(isinstance(role_id,str) and re.fullmatch('[1-9][0-9]*',role_id),'preparer_builtin_role_id_invalid')
    deadline=utc(datetime.now(timezone.utc)+timedelta(seconds=max(1,3600-(time.monotonic()-life.started))))
    assigned=client.request('POST','/api/v1/system/tenant/role_assignments',(201,),
        {'membership_id':membership,'role_ids':[role_id],'valid_until':deadline,
         'reason':'Disposable local execution trace engine preparation'},response_type=list).payload
    require(len(assigned)==1 and assigned[0].get('role_key')==PREPARATION_ROLE and
            assigned[0].get('membership_id')==membership,'preparer_assignment_not_exact')
    client.request('GET','/api/v1/system/auth/context',(401,))
    replace_browser_token(client,browser_json(client,life,'/api/v1/system/refresh',{}))
    final=client.request('GET','/api/v1/system/auth/context',(200,)).payload
    require(final.get('principal')==identity['principal'] and final.get('context')==identity['context'] and
            final.get('delegation') is None and final.get('authentication',{}).get('assurance_level')=='aal2',
            'preparer_refreshed_identity_changed')
    require({a.get('role_key') for a in final.get('authorization',{}).get('role_assignments',[])}==
            {'tenant.administrator',PREPARATION_ROLE} and permissions(final)==permissions(identity)|PREPARATION_PERMISSIONS,
            'preparer_authority_not_exact')
    report['assertions'].update(preparer_real_mfa_and_builtin_role=True,preparer_old_tokens_denied=True)
    return final


def login(env, who, platform=False, life=None, *, identity_prefix="ADDP_EXECUTION_TRACE_"):
    # Every login has a separate real HttpOnly refresh-cookie jar. Bearer
    # business requests still reuse the shared owner API client.
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    def exchange(path, body):
        if life and hasattr(life,"before_api_request"): life.before_api_request(path)
        request=urllib.request.Request(env['GATEWAY_URL']+path,data=json.dumps(body).encode(),method='POST',
                                       headers={'Content-Type':'application/json','Accept':'application/json'})
        with opener.open(request,timeout=life.request_timeout() if life else 10) as response:
            require(response.status==200,'formal_login_http_status')
            reply = json.load(response)
        if life:life.check()
        return reply
    prefix = identity_prefix + who
    reply = exchange('/api/v1/system/login', {
        'username': env[prefix+'_USERNAME'], 'password': env[prefix+'_PASSWORD']})
    if platform:
        require(reply.get('next_action') == 'verify_mfa' and not reply.get('session'), 'platform_password_must_require_mfa')
        challenge = reply['mfa']
        require(challenge.get('method') == 'totp', 'totp_challenge_required')
        code=totp(env[prefix+'_TOTP'],life)
        reply=exchange('/api/v1/system/auth/mfa-verifications',{
            'challenge_token':challenge['challenge_token'],'code':code})
    require(reply.get('next_action') == 'session_issued', 'password_login_did_not_issue_session')
    token=reply.get('session',{}).get('access_token')
    require(isinstance(token,str) and token.startswith('addp_at_'),'formal_access_token_required')
    client=API.GatewayClient(env['GATEWAY_URL'],token,10)
    client.browser_opener=opener
    if life:guard_client(client,life)
    identity=client.request('GET','/api/v1/system/auth/context',(200,)).payload
    require(identity.get('principal',{}).get('type')=='user' and identity.get('delegation') is None,'nondelegated_user_required')
    require(identity.get('token',{}).get('type')=='first_party_access_token','password_issued_token_required')
    if platform:
        require(identity.get('context',{}).get('type')=='platform' and identity.get('authentication',{}).get('assurance_level') in ('aal2','aal3'),'platform_mfa_identity_required')
    else:
        require(identity.get('context',{}).get('type')=='tenant' and identity['context'].get('tenant_id')==env[identity_prefix+'TENANT_ID'],'exact_tenant_identity_required')
    return client, identity


def safe_execution(value):
    if isinstance(value, dict):
        require('observability' not in value and not {'trace_id','span_id','authorization_ref','lease_token','lease_owner','access_token','password'}.intersection(value), 'private_execution_facts_exposed')
        for child in value.values(): safe_execution(child)
    elif isinstance(value,list):
        for child in value: safe_execution(child)


def execution_failure_diagnostic(item):
    result={key:item[key] for key in ('execution_id','status') if key in item}
    def closed_code(value):
        return value if isinstance(value,str) and re.fullmatch('[a-z][a-z0-9_.]{0,99}',value) else 'unavailable'
    result['error_code']=closed_code(item.get('error_details',{}).get('code'))
    diagnostic=item.get('error_details',{}).get('diagnostic')
    if isinstance(diagnostic,dict):
        result['diagnostic']={key:closed_code(diagnostic.get(key)) for key in ('stage','reason')}
    step=item.get('metadata',{}).get('step_results',{}).get('query',{})
    if isinstance(step,dict):
        result['query_step']={key:closed_code(step.get(key)) for key in ('status','phase','error_code')}
    return result


def wait_execution(client, execution_id, module):
    deadline=time.monotonic()+120
    while time.monotonic()<deadline:
        item=client.request('GET',f'/api/v1/{module}/executions/{execution_id}',(200,)).payload
        safe_execution(item)
        require(item.get('execution_id')==execution_id,'execution_identity_mismatch')
        if item.get('status') in ('success','failed','timeout','cancelled'): return item
        require(item.get('status') in ('pending','running'),'unknown_execution_state')
        time.sleep(1)
    raise API.SuiteError('execution_convergence_timeout')


def instance_rows(client, query=''):
    payload=client.request('GET','/api/v1/system/platform/module-instances?'+(query or 'page=1&page_size=100'),(200,)).payload
    rows=payload.get('data')
    require(isinstance(rows,list),'runtime_instance_list_shape')
    return rows


def checkpoint_rows(client, instance, execution_id, start, end, trace_id=None):
    query={'from':start,'to':end,'limit':'100','keyword':execution_id}
    if trace_id: query['trace_id']=trace_id
    path='/api/v1/system/platform/modules/'+instance['module_name']+'/instances/'+instance['instance_id']+'/logs?'+urllib.parse.urlencode(query)
    payload=client.request('GET',path,(200,)).payload
    require(not payload.get('has_more'),'checkpoint_query_must_be_complete')
    result=[]
    for entry in payload.get('entries',[]):
        try: body=json.loads(entry.get('message',''))
        except (ValueError,TypeError): continue
        if body.get('msg')!='execution.checkpoint' or body.get('execution_id')!=execution_id: continue
        require(set(body)=={'time','level','msg','execution_id','phase','attempt','outcome'},'checkpoint_payload_not_minimal')
        require(re.fullmatch('[0-9a-f]{32}',entry.get('trace_id','')) and re.fullmatch('[0-9a-f]{16}',entry.get('span_id','')) and int(entry['trace_id'],16)>0 and int(entry['span_id'],16)>0,'checkpoint_missing_real_context')
        result.append((entry,body))
    return result


def tenant_stage_read(client, life, execution_id):
    """One bounded real Gateway GET, including the response metadata oracle."""
    path = '/api/v1/monitor/executions/by-execution-id/' + execution_id + '/trace-stages'
    if hasattr(life, 'before_api_request'): life.before_api_request(path)
    request = urllib.request.Request(client.base_url + path, headers={
        'Accept': 'application/json', 'Authorization': 'Bearer ' + client.token})
    try:
        remaining = life.stage_observation_deadline-time.monotonic()
        require(remaining > 0, 'tenant_stage_observation_timeout')
        response = client.browser_opener.open(request, timeout=min(5, life.request_timeout(), remaining))
    except urllib.error.HTTPError as error:
        response = error
    except (urllib.error.URLError, TimeoutError):
        raise LocalTraceAssertionFailure('tenant_stage_transport_failed')
    with response:
        require(response.status == 200, 'tenant_stage_http_failed')
        require(response.headers.get('Cache-Control') == 'no-store', 'tenant_stage_cache_contract_failed')
        request_id = response.headers.get('X-Request-ID', '')
        require(re.fullmatch(r'[A-Za-z0-9_-]{1,100}', request_id), 'tenant_stage_request_identity_missing')
        raw = response.read(8193)
    life.check()
    require(len(raw) <= 8192, 'tenant_stage_response_budget_exceeded')
    try: value = json.loads(raw)
    except (ValueError, UnicodeError): raise LocalTraceAssertionFailure('tenant_stage_response_invalid')
    return validate_tenant_stages(value, execution_id), request_id


def validate_tenant_stages(value, execution_id):
    require(isinstance(value, dict) and set(value) == {'execution_id', 'supported', 'stages'} and
            value['execution_id'] == execution_id and value['supported'] is True and
            isinstance(value['stages'], list) and len(value['stages']) == 2, 'tenant_stage_dto_invalid')
    slots = {}
    require([stage.get('slot') for stage in value['stages'] if isinstance(stage,dict)] == ['enqueue','latest'], 'tenant_stage_slots_invalid')
    for stage in value['stages']:
        require(isinstance(stage, dict) and stage.get('slot') in ('enqueue', 'latest') and
                stage['slot'] not in slots, 'tenant_stage_slots_invalid')
        state = stage.get('state')
        require(state in ('available', 'not_found'), 'tenant_stage_fresh_reference_unavailable')
        fields = {'slot', 'state'}
        if state == 'available':
            fields |= {'phase', 'attempt', 'outcome', 'start', 'end', 'duration_ms'}
            attempt, duration = stage.get('attempt'), stage.get('duration_ms')
            require(type(attempt) is int and type(duration) in (int, float) and math.isfinite(duration) and duration >= 0,
                    'tenant_stage_number_invalid')
            require(stage.get('outcome') in ('success', 'failed', 'timeout', 'cancelled', 'rejected'), 'tenant_stage_outcome_invalid')
            require(stage.get('phase') in (('enqueue',) if stage['slot'] == 'enqueue' else ('claim', 'dispatch', 'terminal')) and
                    (attempt == 0 if stage['slot'] == 'enqueue' else attempt >= 1), 'tenant_stage_phase_invalid')
            try:
                start, end = (datetime.fromisoformat(stage[key].replace('Z', '+00:00')) for key in ('start', 'end'))
                require(start.tzinfo is not None and end.tzinfo is not None and end >= start and
                        abs((end-start).total_seconds()*1000-duration) < .001, 'tenant_stage_time_invalid')
            except (ValueError, TypeError, KeyError, AttributeError):
                raise LocalTraceAssertionFailure('tenant_stage_time_invalid')
        require(set(stage) == fields, 'tenant_stage_private_or_missing_fields')
        slots[stage['slot']] = stage
    return value


def tenant_stage_observations(user, admin, life, report, executions, checkpoints, instances, terminals, start):
    life.phase = 'tenant_stage_observations'
    window = life.stage_observation_deadline - 90
    requests, observed, latest = [], [], {}
    previous_round = time.monotonic()
    for round_index in range(6):
        if round_index:
            remaining = previous_round + 15 - time.monotonic()
            if remaining > 0: life.wait(remaining)
        require(time.monotonic() < window+90, 'tenant_stage_observation_timeout')
        previous_round = time.monotonic()
        for owner, key in (('orchestrator', 'parent'), ('develop', 'child')):
            execution_id = executions[1][key]
            require(time.monotonic() < window+85, 'tenant_stage_observation_timeout')
            value, request_id = tenant_stage_read(user, life, execution_id)
            requests.append((owner, request_id))
            latest[(owner, 'failure')] = value
            observed.append({'round': round_index, 'module': owner, 'sample': 'failure',
                             'stages': value['stages'], 'elapsed_seconds': round(time.monotonic()-window,3)})
        if all(value['stages'][1]['state'] == 'available' for value in latest.values()): break
    require(time.monotonic() < window+80, 'tenant_stage_observation_timeout')
    for owner, key in (('orchestrator', 'parent'), ('develop', 'child')):
        execution_id = executions[0][key]
        value, request_id = tenant_stage_read(user, life, execution_id)
        requests.append((owner, request_id)); latest[(owner, 'success')] = value
        observed.append({'module': owner, 'sample': 'success', 'stages': value['stages'], 'elapsed_seconds': round(time.monotonic()-window,3)})
    require(len(requests) <= 14 and time.monotonic() < window+90, 'tenant_stage_observation_timeout')
    report['tenant_stage_observations'] = observed
    cases = []
    for owner, key in (('orchestrator', 'parent'), ('develop', 'child')):
        value = latest[(owner, 'failure')]
        stage = value['stages'][1]; reference = terminals[owner]
        require(stage['state'] == 'available' and stage['phase'] == 'terminal' and stage['outcome'] == 'failed' and
                stage['attempt'] == 1 and all(stage[field] == reference[field] for field in
                ('phase', 'attempt', 'outcome', 'start', 'end', 'duration_ms')), 'tenant_stage_terminal_mismatch')
        cases.append({'module': owner, 'execution_id': value['execution_id'], 'kind': 'readable', 'stages': value['stages']})
        candidate = next((latest[(owner, label)] for label in ('failure', 'success')
                          if latest[(owner, label)]['stages'][0]['state'] == 'not_found'), None)
        require(candidate is not None, 'tenant_stage_normal_missing_not_observed')
        execution_id = candidate['execution_id']
        entry, body = next(pair for pair in checkpoints[(owner, execution_id)] if pair[1]['phase'] == 'enqueue')
        require(body['attempt'] == 0, 'tenant_stage_enqueue_checkpoint_invalid')
        query = urllib.parse.urlencode({'instance_id': instances[owner]['id'], 'start': start, 'end': utc()})
        missing = admin.request('GET', '/api/v1/monitor/platform/traces/'+entry['trace_id']+'?'+query, (404,))
        require(missing.payload.get('error_code') == 'trace_not_found', 'tenant_stage_missing_not_normal')
        detail = user.request('GET', '/api/v1/monitor/executions/by-execution-id/'+execution_id, (200,)).payload
        safe_execution(detail)
        events = user.request('GET', '/api/v1/monitor/executions/by-execution-id/'+execution_id+'/events?after=0&limit=10', (200,)).payload
        safe_execution(events)
        cases.append({'module': owner, 'execution_id': execution_id, 'kind': 'missing', 'stages': candidate['stages']})
    report['assertions'].update(tenant_stage_readable_exact=True, tenant_stage_normal_missing=True,
                                tenant_stage_business_facts_preserved=True)
    life.tenant_stage_requests = requests
    return {'schema': 'addp.tenant-stage-browser/v1', 'run_id': life.run_id, 'cases': cases}


def tenant_stage_request_chain(admin, life, report):
    """Existing completion logs correlate real Gateway/Monitor/Owner reads without sampling."""
    life.phase = 'tenant_stage_request_chain'
    rows = instance_rows(admin)
    expected = {'gateway': ('ingress', '/api/v1/:module/*path'),
                'monitor': ('backend', '/api/v1/monitor/executions/by-execution-id/:execution_id/trace-stages'),
                'orchestrator': ('backend', '/api/v1/orchestrator/execution-read-scope'),
                'develop': ('backend', '/api/v1/develop/execution-read-scope')}
    selected = {}
    for module, (role, route) in expected.items():
        found = [row for row in rows if row.get('module_name') == module and row.get('role') == role]
        require(len(found) == 1, 'tenant_stage_chain_instance_not_unique')
        selected[module] = found[0]
    for owner, request_id in life.tenant_stage_requests:
        for module, (_, route) in expected.items():
            instance = selected[module]
            query = urllib.parse.urlencode({'from': life.tenant_stage_log_start, 'to': utc(), 'keyword': request_id, 'limit': 100})
            path = '/api/v1/system/platform/modules/'+module+'/instances/'+instance['instance_id']+'/logs?'+query
            response = admin.request('GET', path, (200,)).payload
            require(response.get('has_more') is False, 'tenant_stage_chain_logs_incomplete')
            matches = []
            for entry in response.get('entries', []):
                try: body = json.loads(entry.get('message', ''))
                except (ValueError, TypeError): continue
                if body.get('msg') == 'http.request.completed' and body.get('request_id') == request_id and body.get('route') == route:
                    matches.append(body)
            require(len(matches) == (0 if module not in ('gateway', 'monitor', owner) else 1) and
                    all(body.get('method') == 'GET' and body.get('status') == 200 for body in matches),
                    'tenant_stage_chain_owner_or_route_mismatch')
    report['assertions']['tenant_stage_gateway_monitor_single_owner'] = True


def run(life, env, report):
    life.phase='formal_password_and_authcontext'
    admin,admin_identity=login(env,'ADMIN',True,life)
    negative,negative_identity=login(env,'SECURITY',True,life)
    user,user_identity=login(env,'USER',life=life)
    preparer,preparer_identity=login(env,'PREPARER',life=life)
    require({'monitor.trace.read','platform.module_log.read'}<=permissions(admin_identity),'administrator_diagnostic_permissions_missing')
    require(not {'monitor.trace.read','platform.module_log.read'}.intersection(permissions(negative_identity)|permissions(user_identity)),'negative_identity_has_platform_diagnostic_permission')
    require(user_identity.get('client',{}).get('scope_mode')=='unrestricted','business_client_scope_not_unrestricted')
    require(permissions(user_identity)=={
        'orchestrator.workflow.create','orchestrator.workflow.read','orchestrator.workflow.delete','orchestrator.workflow.execute',
        'develop.task.create','develop.task.read','develop.task.delete','develop.task.execute','develop.data_read.execute',
        'monitor.execution.read','system.execution_authorization.create'},'business_user_not_minimal')
    require(preparer_identity['principal']['id']!=user_identity['principal']['id'],'preparation_and_execution_identity_must_differ')
    report['assertions']={'formal_password_mfa_authcontext':True,'tenant_user_minimal':True}
    life.local_clients=(admin,negative,user,preparer)
    life.local_definitions=[]
    preparer_identity=prepare_engine_authority(preparer,preparer_identity,env,life,report)
    unchanged=user.request('GET','/api/v1/system/auth/context',(200,)).payload
    require(permissions(unchanged)==permissions(user_identity),'business_permissions_changed_during_preparation')
    life.phase='synthetic_engine_registration'
    descriptor={'name':life.run_id+'-source','engine_type':'postgresql','engine_origin':'general',
        'description':'Disposable local constant query source','connection_info':{
            'host':'business-postgres','port':5432,'database':'trace_fixture','user':env['POSTGRES_USER'],
            'password':env['POSTGRES_PASSWORD'],'sslmode':'disable'}}
    engine=preparer.request('POST','/api/v1/system/engines',(200,201),descriptor).payload
    engine_id=engine.get('id');require(isinstance(engine_id,int) and engine_id>0,'engine_registration_identity_missing')
    tested=preparer.request('POST',f'/api/v1/system/engines/{engine_id}/test',(200,)).payload
    require(tested.get('success') is True,'registered_engine_connection_failed')
    report['assertions']['formal_synthetic_engine_registration']=True
    start=utc(datetime.now(timezone.utc)-timedelta(seconds=2))
    executions=[]
    for label,sql,expected in [('success','SELECT 1 AS fixture_value','success'),('failure','SELECT 1 / 0 AS fixture_value','failed')]:
        life.phase='bounded_query_'+label
        task=user.request('POST','/api/v1/develop/task-definitions',(200,),{
            'name':life.run_id+'-'+label,'dev_type':'query','content':{'query':sql,'query_type':'sql'},
            'execution_config':{'engine_id':engine_id},'timeout':30,'editor_layout':{}}).payload
        safe_execution(task)
        task_id=task.get('id');require(isinstance(task_id,int),'query_definition_identity_missing')
        life.local_definitions.append(('/api/v1/develop/task-definitions/',task_id))
        definition=user.request('POST','/api/v1/orchestrator/orchestrations',(201,),{
            'name':life.run_id+'-'+label,'enabled':False,'editor_layout':{},'steps':[{
                'id':'query','name':'Query','provider':'develop','task_type':'query','task_id':task_id,
                'parameters':{},'depends_on':[],'timeout':60}]}).payload
        workflow_id=definition.get('id');require(isinstance(workflow_id,int),'orchestration_definition_identity_missing')
        life.local_definitions.append(('/api/v1/orchestrator/orchestrations/',workflow_id))
        admitted=user.request('POST',f'/api/v1/orchestrator/orchestrations/{workflow_id}/execute',(202,)).payload
        execution_id=admitted.get('execution_id');require(isinstance(execution_id,str),'pending_execution_identity_missing')
        parent=wait_execution(user,execution_id,'orchestrator')
        if parent['status']!=expected:
            report['execution_failure']=execution_failure_diagnostic(parent)
            diagnostic_child=parent.get('metadata',{}).get('step_results',{}).get('query',{}).get('result',{}).get('execution_id')
            if isinstance(diagnostic_child,str):
                report['execution_failure']['child']=execution_failure_diagnostic(wait_execution(user,diagnostic_child,'develop'))
        require(parent['status']==expected,'orchestration_terminal_status_mismatch')
        child_id=parent.get('metadata',{}).get('step_results',{}).get('query',{}).get('result',{}).get('execution_id')
        require(isinstance(child_id,str),'child_execution_identity_missing')
        child=wait_execution(user,child_id,'develop')
        require(child['status']==expected and child.get('parent_execution_id')==execution_id,'bounded_child_terminal_mismatch')
        projection=user.request('GET',f'/api/v1/monitor/executions/by-execution-id/{execution_id}/tree',(200,)).payload
        safe_execution(projection)
        executions.append({'parent':execution_id,'child':child_id,'expected':expected})
    report['executions']=executions
    report['assertions']['success_failure_parent_child_closed']=True
    life.phase='real_checkpoint_logs'
    rows=instance_rows(admin)
    owner_instances={}
    for name in ('orchestrator','develop'):
        selected=[r for r in rows if r.get('module_name')==name and r.get('role')=='backend']
        require(len(selected)==1,'owned_runtime_instance_not_unique')
        owner_instances[name]=selected[0]
    deadline=time.monotonic()+90
    checkpoints={}
    while time.monotonic()<deadline:
        end=utc()
        found={}
        for execution in executions:
            for name,key in [('orchestrator','parent'),('develop','child')]:
                values=checkpoint_rows(admin,owner_instances[name],execution[key],start,end)
                phases={body['phase'] for _,body in values}
                if phases=={'enqueue','claim','dispatch','terminal'}:
                    require(len(values)==4,'duplicate_execution_checkpoint')
                    terminal=[(entry,body) for entry,body in values if body['phase']=='terminal'][0]
                    require(terminal[1]['outcome']==execution['expected'],'checkpoint_and_execution_status_disagree')
                    require(all(body['attempt']==(0 if body['phase']=='enqueue' else 1) for _,body in values),'checkpoint_attempt_not_real')
                    found[(name,execution[key])]=values
        if len(found)==4:
            checkpoints=found;break
        time.sleep(2)
    require(len(checkpoints)==4,'real_owner_stage_checkpoints_missing')
    report['assertions']['all_stage_checkpoints_real_context']=True
    # Ordinary stages remain independent 5% samples. Only failure terminals are mandatory.
    life.phase='natural_tail_sampling'
    life.wait(125)
    life.stage_observation_deadline=time.monotonic()+90
    retained=[]
    terminals={}
    for name,key in [('orchestrator','parent'),('develop','child')]:
        failed=executions[1][key]
        entry,body=next(pair for pair in checkpoints[(name,failed)] if pair[1]['phase']=='terminal')
        instance=owner_instances[name]
        query=urllib.parse.urlencode({'instance_id':instance['id'],'start':start,'end':utc()})
        path='/api/v1/monitor/platform/traces/'+entry['trace_id']+'?'+query
        deadline=life.stage_observation_deadline
        while True:
            response=admin.request('GET',path,(200,404))
            if response.status==200:break
            require(response.payload.get('error_code')=='trace_not_found' and time.monotonic()<deadline,'failure_terminal_not_retained')
            time.sleep(2)
        traces=response.payload.get('data',[])
        require(len(traces)==1 and traces[0].get('trace_id')==entry['trace_id'],'exact_trace_identity_mismatch')
        spans=traces[0].get('spans',[])
        require(len(spans)==1,'stage_must_be_independent_root')
        span=spans[0]
        require(span.get('module_name')==name and span.get('instance_id')==instance['instance_id'] and span.get('role')=='backend' and span.get('parent_span_id')=='' and span.get('kind')=='internal' and span.get('span_id')==entry['span_id'] and span.get('scope')=='addp.execution' and span.get('phase')=='terminal' and span.get('outcome')=='failed' and span.get('attempt')==body['attempt'],'retained_stage_contract_mismatch')
        terminals[name]=span
        expected_links={e['trace_id']+'/'+e['span_id']:e for e,b in checkpoints[(name,failed)] if b['phase'] in ('enqueue','dispatch')}
        known=set(expected_links)
        links=span.get('links',[])
        require(len(links)==2 and {link['trace_id']+'/'+link['span_id'] for link in links}==known and all(link['module_name']==name and link['role']=='backend' and link['instance_id']==instance['instance_id'] and link['runtime_instance_id']==instance['id'] for link in links),'persisted_links_do_not_match_real_checkpoints')
        linked_ids=','.join(str(link['runtime_instance_id']) for link in links)
        # Exact System User query establishes current target identities even if independently unsampled.
        resolved=instance_rows(admin,'ids='+urllib.parse.quote(linked_ids)) if len(set(linked_ids.split(',')))==len(links) else instance_rows(admin,'ids='+str(links[0]['runtime_instance_id']))
        require(all(any(row['id']==link['runtime_instance_id'] and row['instance_id']==link['instance_id'] and row['module_name']==link['module_name'] for row in resolved) for link in links),'link_target_current_identity_mismatch')
        exact=checkpoint_rows(admin,instance,failed,start,utc(),entry['trace_id'])
        require(any(e['span_id']==entry['span_id'] for e,_ in exact),'exact_trace_span_log_missing')
        for denied in (negative,user):
            denied.request('GET',path,(403,))
            log_path='/api/v1/system/platform/modules/'+name+'/instances/'+instance['instance_id']+'/logs?'+urllib.parse.urlencode({'from':start,'to':utc(),'trace_id':entry['trace_id']})
            denied.request('GET',log_path,(403,))
        retained.append({'schema':'addp.execution-trace-browser/v1','run_id':life.run_id,'module_name':name,
                         'instance_id':instance['instance_id'],'runtime_instance_id':instance['id'],'execution_id':failed,
                         'trace_id':entry['trace_id'],'span_id':entry['span_id'],'started_at':span['start'],
                         'start':start,'end':utc(),'links':links})
    report['assertions'].update(retained_failure_stages_and_links=True,exact_trace_span_logs=True,tenant_and_wrong_platform_denied=True)
    # Save only safe references, never SQL, data or raw API payloads.
    report['retained']=[{'module_name':row['module_name'],'execution_id':row['execution_id']} for row in retained]
    tenant = tenant_stage_observations(user,admin,life,report,executions,checkpoints,owner_instances,terminals,start)
    tenant['tenant_id']=user_identity['context']['tenant_id']
    tenant['user_id']=str(user_identity['principal']['id'])
    tenant['navigation']={'engine_id':engine_id,'query_concurrency':20,'query_per_engine_concurrency':5,'backend_instances':1}
    life.tenant_stage_log_start=start
    return {'platform':retained[1],'tenant':tenant}


def navigation_ledger(life):
    path=life.navigation_ledger
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW)
    try:
        info=os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_nlink==1 and info.st_mode&0o077==0 and info.st_size<=4096,'navigation_ledger_file_invalid')
        raw=os.read(fd,4097)
    finally:os.close(fd)
    try:value=json.loads(raw)
    except (ValueError,UnicodeDecodeError):raise LocalTraceAssertionFailure('navigation_ledger_invalid')
    binding=life.navigation_binding
    require(isinstance(value,dict) and set(value)=={'schema','run_id','tenant_id','user_id','engine_id','definitions','executions','reads'} and
            value['schema']=='addp.execution-navigation-ownership/v1' and type(value['engine_id']) is int and all(value[k]==v for k,v in binding.items()),'navigation_ledger_binding_invalid')
    require(type(value['reads']) is int and 0<=value['reads']<=45,'navigation_ledger_budget_invalid')
    source=(life.checkout/'console/frontend/e2e/online/execution-navigation-local-support.mjs').read_text()
    sql=re.search(r"export const navigationSQL = '([^']+)'",source)
    require(sql is not None,'navigation_sql_contract_missing')
    sql_hash=hashlib.sha256(sql[1].encode()).hexdigest()
    def digest(values):return hashlib.sha256(json.dumps(values,separators=(',',':'),ensure_ascii=False).encode()).hexdigest()
    prefix=[binding[k] for k in ('run_id','tenant_id','user_id','engine_id')]
    definitions,executions=value['definitions'],value['executions']
    require(isinstance(definitions,list) and len(definitions)<=6 and isinstance(executions,list) and len(executions)<=7,'navigation_ledger_limit')
    for slot,row in enumerate(definitions):
        require(isinstance(row,dict) and set(row)=={'slot','digest','id'} and type(row['slot']) is int and row['slot']==slot and
                row['digest']==digest(prefix+['definition',slot,sql_hash]) and (row['id'] is None or type(row['id']) is int and row['id']>0),
                'navigation_ledger_definition_invalid')
    mapping=[0,1,2,3,4,5,0]
    for slot,row in enumerate(executions):
        require(isinstance(row,dict) and set(row)=={'slot','definition','digest','id'} and type(row['slot']) is int and row['slot']==slot and
                type(row['definition']) is int and row['definition']==mapping[slot] and row['definition']<len(definitions),'navigation_ledger_execution_invalid')
        task=definitions[row['definition']]['id']
        require(task is not None and row['digest']==digest(prefix+['execution',slot,row['definition'],task]) and
                (row['id'] is None or isinstance(row['id'],str) and re.fullmatch(r'[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}',row['id'])),
                'navigation_ledger_execution_invalid')
    for rows in (definitions,executions):
        ids=[row['id'] for row in rows if row['id'] is not None]
        require(len(ids)==len(set(ids)),'navigation_ledger_duplicate_identity')
    return value,sql[1]


def finish_navigation(life,user,report):
    if not hasattr(life,'navigation_ledger'):return []
    failures=[]
    try:value,sql=navigation_ledger(life)
    except (OSError,ValueError,LocalTraceAssertionFailure):
        report['navigation_cleanup']={'result':'failed','error_code':'navigation_ledger_unavailable'}
        return ['navigation_ledger_unavailable']
    terminal=set()
    pending=[row for row in value['executions'] if row['id'] is not None]
    if any(row['id'] is None for row in value['definitions']+value['executions']):failures.append('navigation_unknown_admission')
    # The durable counter is reserved before each read, so a killed browser cannot reset the 45-read cap.
    deadline=min(time.monotonic()+60,life.started+3600)
    while pending and value['reads']<45 and time.monotonic()<deadline:
        row=pending[0]
        try:
            life.wait(2)
            if time.monotonic()>=deadline:break
            pending.pop(0)
            value['reads']+=1
            temporary=life.navigation_ledger.with_name('navigation-ownership-cleanup.tmp')
            with open(temporary,'x',opener=lambda path,flags:os.open(path,flags,0o600)) as out:
                out.write(json.dumps(value,separators=(',',':')));out.flush();os.fsync(out.fileno())
            os.replace(temporary,life.navigation_ledger)
            directory=os.open(life.navigation_ledger.parent,os.O_RDONLY)
            try:os.fsync(directory)
            finally:os.close(directory)
            item=user.request('GET','/api/v1/develop/executions/'+row['id'],(200,)).payload
            safe_execution(item)
            require(item.get('execution_id')==row['id'] and item.get('module')=='develop' and item.get('task_type')=='query' and
                    item.get('source_task_id')==str(value['definitions'][row['definition']]['id']) and str(item.get('triggered_by'))==value['user_id'],
                    'navigation_cleanup_execution_binding_invalid')
            if item.get('status') in ('success','failed','timeout','cancelled'):terminal.add(row['id'])
            else:
                require(item.get('status') in ('pending','running'),'navigation_cleanup_state_invalid');pending.append(row)
        except Exception:
            failures.append('navigation_execution_read_failed');break
    if pending:failures.append('navigation_execution_not_terminal')
    deleted=0
    for row in reversed(value['definitions']):
        if row['id'] is None:continue
        associated=[item for item in value['executions'] if item['definition']==row['slot']]
        if any(item['id'] not in terminal for item in associated):continue
        path='/api/v1/develop/task-definitions/'+str(row['id'])
        try:
            task=user.request('GET',path,(200,)).payload
            require(task.get('id')==row['id'] and str(task.get('tenant_id'))==value['tenant_id'] and str(task.get('created_by'))==value['user_id'] and
                    task.get('name')==value['run_id']+'-navigation-'+str(row['slot']) and task.get('dev_type')=='query' and task.get('timeout')==30 and
                    task.get('execution_config',{}).get('engine_id')==value['engine_id'] and task.get('content',{}).get('query')==sql,
                    'navigation_cleanup_definition_binding_invalid')
            user.request('DELETE',path,(200,204));user.request('GET',path,(404,));deleted+=1
        except Exception:failures.append('navigation_definition_cleanup_failed')
    report['navigation_cleanup']={'result':'failed' if failures else 'passed','definitions':len(value['definitions']),
        'executions':len(value['executions']),'deleted':deleted,'terminal':len(terminal),'reads':value['reads']}
    return failures


def finish(life,env,report):
    life.phase='owner_cleanup_and_logout'
    admin,negative,user,preparer=life.local_clients
    definition_errors, session_errors = finish_navigation(life,user,report), []
    for path,identifier in reversed(life.local_definitions):
        try:
            user.request('DELETE',path+str(identifier),(200,204))
            user.request('GET',path+str(identifier),(404,))
        except Exception:
            definition_errors.append('definition_cleanup_failed')
    for client in (admin,negative,user,preparer):
        try:
            request=urllib.request.Request(env['GATEWAY_URL']+'/api/v1/system/logout',data=b'',method='POST',
                                           headers={'Authorization':'Bearer '+client.token})
            with client.browser_opener.open(request,timeout=life.request_timeout()) as response:
                require(response.status==204,'formal_logout_did_not_close_cookie_session')
            client.request('GET','/api/v1/system/auth/context',(401,))
        except Exception:
            session_errors.append('session_cleanup_failed')
    report['assertions'].update(formal_definitions_deleted=not definition_errors,logout_old_session_denied=not session_errors)
    require(not definition_errors and not session_errors,'formal_cleanup_incomplete')
