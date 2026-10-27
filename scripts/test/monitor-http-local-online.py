"""Real formal identities and product HTTP/page assertions; no API mocks."""
from __future__ import annotations
from datetime import datetime,timezone,timedelta
import importlib.util
import json
from pathlib import Path
import re
import urllib.parse
import urllib.request
import time

spec=importlib.util.spec_from_file_location('http_local_identity',Path(__file__).with_name('monitor-traces-local-online.py'))
auth=importlib.util.module_from_spec(spec);spec.loader.exec_module(auth)
require=auth.require
PREFIX='/api/v1/monitor/platform/'

class RequestBudget:
    def __init__(self):self.control=0;self.controlled=0;self.last=None
    def before(self,path):
        if path.startswith(('/api/v1/agent/','/api/v1/copilot/')):
            self.controlled+=1;require(self.controlled<=300,'controlled_request_limit')
            now=time.monotonic()
            require(self.last is None or now-self.last>=1,'controlled_request_rate_limit')
            self.last=now
        else:
            self.control+=1;require(self.control<=200,'control_request_limit')


def observation(client,instances,window=60,endpoint='http_observations',extra=None):
    query={'instance_ids':','.join(str(row['id']) for row in instances),'window_seconds':window,**(extra or {})}
    return client.request('GET',PREFIX+endpoint+'?'+urllib.parse.urlencode(query),(200,)).payload


def valid_point(process,key):
    points=[p for row in process.get('observations',[]) for series in row.get('series',[])
        if series.get('metric_key')==key for p in series.get('points',[]) if p.get('data_state')=='valid']
    require(points and all(isinstance(p.get('value'),(float,int)) for p in points),'valid_metric_point_required')
    return points[-1]['value']


def validate_live(payload,instances):
    rows=payload.get('data',[])
    require(len(rows)==len(instances) and payload.get('window_seconds')==60,'http_response_identity')
    expected={row['id']:row for row in instances}
    require({row.get('subject',{}).get('id') for row in rows}==set(expected),'http_response_subjects')
    for row in rows:
        subject=row['subject'];owner=expected[subject['id']]
        require(all(subject.get(k)==owner[k] for k in ('module_name','role','instance_id')),'http_subject_changed')
        require(row['collection']['state']=='collecting','http_source_not_collecting')
        require(valid_point(row,'http.requests.per_second')>0,'http_qps_not_positive')
        require(valid_point(row,'http.client_errors.percent')>0,'http_4xx_not_visible')
        require(valid_point(row,'http.failures.percent')==0,'http_4xx_misclassified_as_failure')
        for key in ('http.duration.mean_seconds','http.duration.p95_seconds','http.duration.p99_seconds','http.active_requests'):
            require(valid_point(row,key)>=0,'http_metric_negative')
    return rows


def require_history(payload):
    require(any(point.get('data_state')=='valid' for row in payload.get('data',[]) for observation in row.get('observations',[])
        for series in observation.get('series',[]) for point in series.get('points',[])), 'http_history_no_valid_points')


def logout(life,env,clients):
    for client in clients:
        life.before_api_request('/api/v1/system/logout')
        request=urllib.request.Request(env['GATEWAY_URL']+'/api/v1/system/logout',data=b'',method='POST',
            headers={'Authorization':'Bearer '+client.token})
        with client.browser_opener.open(request,timeout=life.request_timeout()) as response:
            require(response.status==204,'formal_logout_failed')
        client.request('GET','/api/v1/system/auth/context',(401,))


def browser(life,env,report,instances,stopped=False):
    require(not stopped,'browser_runs_before_single_source_stop')
    require(life.request_budget.control+80<=200,'browser_control_reserve_insufficient')
    input={'schema':'addp.http-quality-browser/v1','run_id':life.run_id,
        'instances':[{k:row[k] for k in ('id','module_name','instance_id','role')} for row in instances]}
    life.private_json('http-browser.json',input)
    selected={k:v for k,v in env.items() if k.startswith('ADDP_HTTP_LOCAL_ADMIN_')}
    selected.update(ADDP_HTTP_LOCAL='1',CONSOLE_URL='http://nginx',ADDP_HTTP_LOCAL_BROWSER_CONFIG_FILE='/private/http-browser.json',
        ADDP_HTTP_LOCAL_BROWSER_REPORT_FILE='/private/http-browser-report.json',ADDP_ONLINE_ARTIFACT_DIR='/tmp/http-browser')
    secret=life.work/'browser.env';secret.write_text('\n'.join(k+'='+v for k,v in selected.items())+'\n');secret.chmod(0o600)
    life.phase='formal_console_http_quality_page'
    try:
        life.docker('run','--name',life.run_id+'-browser','--label',life.label+'='+life.run_id,
            '--label',life.label+'.role=oneshot','--cpus','.5','--memory','1g','--network',life.network,
            '--env-file',str(secret),'--mount',f'type=bind,source={life.checkout},target=/workspace',
            '--mount',f'type=bind,source={life.work},target=/private','-w','/workspace/console/frontend',life.browser_image,
            'node','node_modules/@playwright/test/cli.js','test','--config=playwright.online.config.js',
            'http-quality-local.spec.js','--reporter=line',timeout=300)
    finally:
        receipt_path=life.work/'http-browser-report.json'
        if receipt_path.is_file():report['browser']=json.loads(receipt_path.read_text())
    receipt=json.loads((life.work/'http-browser-report.json').read_text())
    require(receipt.get('result')=='passed' and receipt.get('run_id')==life.run_id,'formal_browser_receipt_failed')
    require(type(receipt.get('api_requests')) is int and life.request_budget.control+receipt['api_requests']<=200,'control_request_limit')
    life.request_budget.control+=receipt['api_requests']
    report['browser']=receipt


def source_counts(life,phase):
    """Four read-only mTLS GETs total, using the real product's Prometheus parser."""
    script=r'''import json,ssl,sys,urllib.request
from prometheus_client.parser import text_string_to_metric_families
context=ssl.create_default_context(cafile="/tls/ca.crt")
context.load_cert_chain("/tls/client.crt","/tls/client.key")
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPSHandler(context=context))
result={}
for module,address in json.loads(sys.argv[1]).items():
    port=19444 if module=="agent" else 19445
    with opener.open("https://"+address+":"+str(port)+"/metrics",timeout=5) as response:
        if response.status!=200:raise RuntimeError("metrics_status")
        raw=response.read((4<<20)+1)
    if len(raw)>4<<20:raise RuntimeError("metrics_limit")
    routes={"/api/v1/agent/sessions","/api/v1/agent/sessions/{session_id}","/api/v1/copilot/settings/inference-bindings/{scenario_code}"}
    rows={}
    for family in text_string_to_metric_families(raw.decode()):
        for sample in family.samples:
            labels=sample.labels
            if sample.name=="http_server_request_duration_seconds_count" and labels.get("http_request_method")=="GET" and labels.get("http_route") in routes:
                key=labels["http_route"]+"|"+labels["http_response_status_code"]
                rows[key]=rows.get(key,0)+sample.value
    result[module]=rows
print(json.dumps(result))
'''
    deployment=life.work/'metrics-deployment'
    mounts=[part for source,target in [('source-ca.crt','ca.crt'),('collector.crt','client.crt'),('collector.key','client.key')]
        for part in ('--mount',f'type=bind,source={deployment/source},target=/tls/{target},readonly')]
    return json.loads(life.docker('run','--name',life.run_id+'-counts-'+phase,'--label',life.label+'='+life.run_id,
        '--label',life.label+'.role=oneshot','--cpus','.25','--memory','256m','--network',life.network,
        '--read-only',*mounts,'--entrypoint','python',life.images['agent-backend'],'-c',script,
        json.dumps(life.addresses),timeout=15,output_limit=65536))


def verify_increments(before,after):
    expected={'agent':{'/api/v1/agent/sessions|200':15,'/api/v1/agent/sessions/{session_id}|404':15,
                      '/api/v1/agent/sessions/{session_id}|422':15},
              'copilot':{'/api/v1/copilot/settings/inference-bindings/{scenario_code}|200':15,
                         '/api/v1/copilot/settings/inference-bindings/{scenario_code}|404':15}}
    for module,counts in expected.items():
        for key,value in counts.items():
            require(after.get(module,{}).get(key,0)-before.get(module,{}).get(key,0)==value,'controlled_source_increment_mismatch')
    return expected


def run(life,env,report):
    budget=RequestBudget();life.request_budget=budget;life.before_api_request=budget.before
    life.phase='real_password_mfa_identities'
    clients=[]
    for who,platform in [('ADMIN',True),('SECURITY',True),('USER',False)]:
        client,identity=auth.login(env,who,platform=platform,life=life,identity_prefix='ADDP_HTTP_LOCAL_')
        clients.append(client)
        if who=='ADMIN':require('monitor.http_observation.read' in auth.permissions(identity),'admin_http_permission_missing')
        if who=='USER':require(auth.permissions(identity)=={'agent.session.read'},'ordinary_user_authority_changed')
    admin,security,user=clients
    life.phase='real_source_registration'
    instances=[]
    for attempt in range(20):
        rows=auth.instance_rows(admin,'page=1&page_size=100&role=backend&status=up')
        instances=[row for row in rows if row['module_name'] in ('agent','copilot')]
        if len(instances)==2:break
        life.wait(5)
    require(len(instances)==2 and {r['module_name'] for r in instances}=={'agent','copilot'},'python_source_registration_missing')
    denied=PREFIX+'http_observations?instance_ids='+str(instances[0]['id'])+'&window_seconds=60'
    security.request('GET',denied,(403,));user.request('GET',denied,(403,))
    anonymous=auth.API.GatewayClient(env['GATEWAY_URL'],'',10);auth.guard_client(anonymous,life)
    cases={
        'agent':[(user,'/api/v1/agent/sessions',200),(user,'/api/v1/agent/sessions/0',404),
                 (user,'/api/v1/agent/sessions/not-an-integer',422),(anonymous,'/api/v1/agent/sessions',401)],
        'copilot':[(admin,'/api/v1/copilot/settings/inference-bindings/navigation_guide',200),
                   (admin,'/api/v1/copilot/settings/inference-bindings/local_missing',404),
                   (user,'/api/v1/copilot/settings/inference-bindings/navigation_guide',403),
                   (anonymous,'/api/v1/copilot/settings/inference-bindings/navigation_guide',401)]}
    before=source_counts(life,'before')
    life.phase='bounded_readonly_python_requests'
    results={name:{} for name in cases}
    report['controlled_requests']=results
    for index in range(60):
        for name,cycle in cases.items():
            client,path,status=cycle[index%len(cycle)]
            client.request('GET',path,(status,))
            results[name][str(status)]=results[name].get(str(status),0)+1
            life.wait(1.05)
    report['controlled_source_increments']=verify_increments(before,source_counts(life,'after'))
    report['source_tls_gets']=4
    # Natural sampling/scraping only. No time offsets or synthetic metric writes.
    life.wait(20)
    live=observation(admin,instances)
    validate_live(live,instances)
    report['live_summary']=live
    report['controlled_requests']=results
    report['assertions'].update(real_password_mfa=True,tenant_minimal_permission=True,unauthorized_observation_denied=True,
        two_product_sources_collecting=True,one_minute_qps_4xx_and_seven_metrics=True)
    history_queries={}
    for row in instances:
        detail=observation(admin,[row],endpoint='http_interfaces',extra={'sort':'qps'})
        routes=detail['data'][0]['observations']
        expected='/api/v1/agent/sessions/{session_id}' if row['module_name']=='agent' else '/api/v1/copilot/settings/inference-bindings/{scenario_code}'
        # Route spelling is a product contract, checked against the actual normalized series.
        require(any(item.get('route')==expected and item.get('method')=='GET' for item in routes),'normalized_product_route_missing')
        require(not any('not-an-integer' in item.get('route','') or 'local_missing' in item.get('route','') for item in routes),
            'raw_parameter_route_leak')
        end=datetime.now(timezone.utc).replace(microsecond=0)-timedelta(seconds=15)
        history_queries[row['module_name']]={'start':(end-timedelta(minutes=5)).isoformat(),'end':end.isoformat()}
        history=observation(admin,[row],endpoint='http_trends',extra=history_queries[row['module_name']])
        require_history(history)
    report['assertions']['normalized_ranking_and_history']=True
    report['not_accepted']=['5m/15m完整窗口','模型与工具调用','5xx与告警通知重演','Hosted','容量与长期保留']
    browser(life,env,report,instances)
    life.phase='single_planned_copilot_stop'
    row=json.loads(life.docker('inspect',life.run_id+'-copilot-backend'))[0]
    require(row['State']['Running'] and row['Config']['Labels'].get(life.label)==life.run_id,'copilot_stop_owner_mismatch')
    life.planned_stops=frozenset({row['Id']})
    life.docker('stop','--time','15',row['Id'],timeout=25)
    stopped=json.loads(life.docker('inspect',row['Id']))[0]
    require(not stopped['State']['Running'] and stopped['State']['ExitCode']==0 and not stopped['State']['OOMKilled'],
        'copilot_planned_stop_not_clean')
    life.wait(70)
    after=observation(admin,instances)
    by_module={r['subject']['module_name']:r for r in after['data']}
    require(by_module['agent']['collection']['state']=='collecting','agent_source_lost')
    require(by_module['copilot']['collection']['state']!='collecting' and not by_module['copilot']['observations'],
        'stopped_source_must_not_be_healthy_zero')
    report['source_stop_summary']=after
    report['assertions']['single_source_stop_isolation']=True
    life.phase='stopped_source_retained_history'
    stopped_owner=next(row for row in instances if row['module_name']=='copilot')
    retained=observation(admin,[stopped_owner],endpoint='http_trends',extra=history_queries['copilot'])
    require_history(retained)  # Known product gap; retain this accepted assertion until scope is confirmed.
    report['assertions']['stopped_history_readable']=True
    life.phase='formal_logout_and_revocation'
    logout(life,env,clients)
    report['assertions']['logout_old_sessions_denied']=True
    report['request_counts']={'controlled':budget.controlled,'control':budget.control,'concurrency':1,'qps_max':1}
