"""Owner API assertions for the manually admitted local execution Trace gate."""
from __future__ import annotations

import base64
from datetime import datetime, timezone, timedelta
import hashlib
import hmac
import http.cookiejar
import importlib
import json
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
        client.timeout = life.request_timeout()
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


def login(env, who, platform=False, life=None):
    # Every login has a separate real HttpOnly refresh-cookie jar. Bearer
    # business requests still reuse the shared owner API client.
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    def exchange(path, body):
        request=urllib.request.Request(env['GATEWAY_URL']+path,data=json.dumps(body).encode(),method='POST',
                                       headers={'Content-Type':'application/json','Accept':'application/json'})
        with opener.open(request,timeout=life.request_timeout() if life else 10) as response:
            require(response.status==200,'formal_login_http_status')
            reply = json.load(response)
        if life:life.check()
        return reply
    prefix = 'ADDP_EXECUTION_TRACE_' + who
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
        require(identity.get('context',{}).get('type')=='tenant' and identity['context'].get('tenant_id')==env['ADDP_EXECUTION_TRACE_TENANT_ID'],'exact_tenant_identity_required')
    return client, identity


def safe_execution(value):
    if isinstance(value, dict):
        require('observability' not in value and not {'trace_id','span_id','authorization_ref','lease_token','lease_owner'}.intersection(value), 'private_execution_facts_exposed')
        for child in value.values(): safe_execution(child)
    elif isinstance(value,list):
        for child in value: safe_execution(child)


def execution_failure_diagnostic(item):
    result={key:item[key] for key in ('execution_id','status') if key in item}
    def closed_code(value):
        return value if isinstance(value,str) and re.fullmatch('[a-z][a-z0-9_.]{0,99}',value) else 'unavailable'
    result['error_code']=closed_code(item.get('error_details',{}).get('code'))
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


def run(life, env, report):
    life.phase='formal_password_and_authcontext'
    admin,admin_identity=login(env,'ADMIN',True,life)
    negative,negative_identity=login(env,'SECURITY',True,life)
    user,user_identity=login(env,'USER',life=life)
    preparer,preparer_identity=login(env,'PREPARER',life=life)
    require({'monitor.trace.read','platform.module_log.read'}<=permissions(admin_identity),'administrator_diagnostic_permissions_missing')
    require(not {'monitor.trace.read','platform.module_log.read'}.intersection(permissions(negative_identity)|permissions(user_identity)),'negative_identity_has_platform_diagnostic_permission')
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
    retained=[]
    for name,key in [('orchestrator','parent'),('develop','child')]:
        failed=executions[1][key]
        entry,body=next(pair for pair in checkpoints[(name,failed)] if pair[1]['phase']=='terminal')
        instance=owner_instances[name]
        query=urllib.parse.urlencode({'instance_id':instance['id'],'start':start,'end':utc()})
        path='/api/v1/monitor/platform/traces/'+entry['trace_id']+'?'+query
        deadline=time.monotonic()+90
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
        require(span.get('span_id')==entry['span_id'] and span.get('scope')=='addp.execution' and span.get('phase')=='terminal' and span.get('outcome')=='failed' and span.get('attempt')==body['attempt'],'retained_stage_contract_mismatch')
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
    report['retained']=[{k:v for k,v in row.items() if k not in ('start','end','schema')} for row in retained]
    return retained[1]


def finish(life,env,report):
    life.phase='owner_cleanup_and_logout'
    admin,negative,user,preparer=life.local_clients
    for path,identifier in reversed(life.local_definitions):
        user.request('DELETE',path+str(identifier),(200,204))
        user.request('GET',path+str(identifier),(404,))
    for client in (admin,negative,user,preparer):
        request=urllib.request.Request(env['GATEWAY_URL']+'/api/v1/system/logout',data=b'',method='POST',
                                       headers={'Authorization':'Bearer '+client.token})
        with client.browser_opener.open(request,timeout=life.request_timeout()) as response:
            require(response.status==204,'formal_logout_did_not_close_cookie_session')
        client.request('GET','/api/v1/system/auth/context',(401,))
    report['assertions'].update(formal_definitions_deleted=True,logout_old_session_denied=True)
