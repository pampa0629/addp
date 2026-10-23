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
import urllib.parse
import urllib.request

sys.path.insert(0, str(__import__('pathlib').Path(__file__).resolve().parents[2]))
API = importlib.import_module('scripts.utils.online-api')


def require(value, code):
    if not value:
        raise API.SuiteError(code)


def utc(value=None):
    return (value or datetime.now(timezone.utc)).isoformat(timespec='microseconds').replace('+00:00', 'Z')


def permissions(context):
    return {p for a in context.get('authorization', {}).get('role_assignments', []) for p in a.get('permissions', [])}


def guard_client(client, life):
    request = client.request
    def guarded(*args, **kwargs):
        client.timeout = life.request_timeout()
        reply = request(*args, **kwargs)
        life.check()
        return reply
    client.request = guarded
    return client


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
        # Formal TOTP anti-replay remains active, including subsequent browser login.
        remaining=30-time.time()%30
        if remaining < 2:
            life.wait(remaining+.1) if life else time.sleep(remaining+.1)
        counter=struct.pack('>Q', int(time.time())//30)
        key=base64.b32decode(env[prefix+'_TOTP'], casefold=False)
        digest=hmac.new(key,counter,hashlib.sha1).digest(); offset=digest[-1]&15
        code=str((struct.unpack('>I',digest[offset:offset+4])[0]&0x7fffffff)%1000000).zfill(6)
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
