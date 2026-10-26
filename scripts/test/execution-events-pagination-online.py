"""Real user/Owner/Gateway/Console reads over bounded Common-writer fixtures."""
from __future__ import annotations
from datetime import datetime,timezone,timedelta
from importlib import import_module
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile

BASE=import_module('scripts.test.execution-retention-readonly-online')
API=BASE.API
ROOT=BASE.ROOT
SUITE='execution-events-pagination'
PREFIX='/api/v1/monitor/executions/by-execution-id'
PERMISSIONS={'monitor.execution.read','meta.scan_task.read'}
FIELDS={'id','execution_id','attempt','occurred_at','kind','step_id','counters'}
PRIVATE={'execution_config','outputs','parameters','result','lease_token','lease_owner','lease_expires_at','execution_authorization_id','authorization_ref','actor_principal_id','actor_tenant_membership_id','issued_authorization_version'}
COUNTERS={'progress','batch_index','batch_records','records_read','records_written','bytes_read','bytes_written','items_scanned','catalog_nodes_scanned','fields_scanned'}
require=BASE.require


def assert_safe(value):
 if isinstance(value,dict):
  require(not PRIVATE.intersection(value),'private diagnostic fields exposed')
  for v in value.values():assert_safe(v)
 elif isinstance(value,list):
  for v in value:assert_safe(v)


def assert_page(page,execution_id,after,expected):
 require(set(page)=={'items','next_cursor','has_more','retained_after'},'event page not closed')
 assert_safe(page)
 items=page['items'];wanted=[i for i in expected if i>after][:100]
 require(isinstance(items,list) and [i.get('id') for i in items]==wanted,'event page lost order or identity')
 require(type(page['has_more']) is bool and page['has_more']==(len([i for i in expected if i>after])>100),'event page lost has_more')
 require(type(page['next_cursor']) is int and page['next_cursor']==(wanted[-1] if wanted else 0),'event cursor differs')
 cutoff=datetime.fromisoformat(page['retained_after'].replace('Z','+00:00'))
 require(cutoff.tzinfo is not None and abs((datetime.now(timezone.utc)-timedelta(days=30)-cutoff).total_seconds())<90,'event retention cutoff differs from 30 days')
 for item in items:
  require(set(item)<=FIELDS and FIELDS-{'step_id'}<=set(item),'event fields not closed')
  require(item['execution_id']==execution_id and type(item['attempt']) is int and item['attempt']==1,'event ownership differs')
  require(type(item['id']) is int and item['kind'] in {'started','progress','completed','failed','cancelled','timeout','truncated'},'invalid event identity or kind')
  when=datetime.fromisoformat(item['occurred_at'].replace('Z','+00:00'))
  require(when>=cutoff and when<=datetime.now(timezone.utc),'event outside retention window')
  require(isinstance(item['counters'],dict) and set(item['counters'])<=COUNTERS and all(type(v) is int and v>=0 for v in item['counters'].values()),'event counters not closed')
  if item['kind']=='truncated':require(not item.get('step_id') and not item['counters'],'truncation marker retained private context')
 return items


def walk(client,execution_id,expected,nonce):
 after=0;items=[];pages=0
 while True:
  page=client.request('GET',f'{PREFIX}/{execution_id}/events?after={after}&limit=100',(200,)).payload
  require(nonce not in json.dumps(page),'event page exposed private fixture')
  rows=assert_page(page,execution_id,after,expected);items.extend(rows);pages+=1
  if not page['has_more']:break
  after=page['next_cursor'];require(pages<=13,'event pagination did not converge')
 require([i['id'] for i in items]==expected and len({i['id'] for i in items})==len(items),'duplicate or missing paginated events')
 empty=client.request('GET',f'{PREFIX}/{execution_id}/events?after={expected[-1]}&limit=100',(200,)).payload
 assert_page(empty,execution_id,expected[-1],expected)
 return items,pages


def snapshot():
 return json.loads(BASE.sql("BEGIN READ ONLY; SELECT json_build_object('executions',(SELECT count(*) FROM common.task_executions),"
  "'events',(SELECT count(*) FROM common.execution_events),"
  "'history_hash',(SELECT md5(string_agg(row_to_json(e)::text,'' ORDER BY id)) FROM common.task_executions e),"
  "'events_hash',(SELECT md5(string_agg(row_to_json(e)::text,'' ORDER BY id)) FROM common.execution_events e)); COMMIT;",SUITE))


def assert_invisible(client,ids,cursors):
 for identity,cursor in zip(ids,cursors):
  for suffix in ('',f'/events?after={cursor}&limit=100'):
   body=client.request('GET',PREFIX+'/'+identity+suffix,(404,)).payload
   require(set(body)==({'error'} if not suffix else {'error','error_code'}) and (not suffix or body['error_code']=='execution_not_found'),'denied execution leaked diagnostic facts')
 listing=client.request('GET','/api/v1/monitor/executions?module=meta&page=1&page_size=100',(200,)).payload
 require(listing.get('total')==0 and listing.get('executions')==[],'unauthorized execution list or count leaked')


def run_private(command,cwd,environment,output_path,timeout):
 with output_path.open('w') as output:
  process=subprocess.Popen(command,cwd=cwd,env=environment,stdout=output,stderr=subprocess.STDOUT,start_new_session=True)
  try:return process.wait(timeout=timeout)
  finally:
   if process.poll() is None:
    os.killpg(process.pid,signal.SIGTERM)
    try:process.wait(timeout=5)
    except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait(timeout=5)


def browser(secret,artifact,state):
 checks=['real_password_auth_and_owner_identity','quota_ten_pages_and_storage_marker','history_display_limit_with_real_next_page',
         'all_page_ids_and_dom_projection','refresh_and_reload_reset_first_page','close_and_reopen_reset_first_page','safe_diagnostics']
 with tempfile.TemporaryDirectory(prefix='event-pagination-browser-',dir=secret) as directory:
  private=Path(directory)
  environment=dict(os.environ,ADDP_EVENTS_PAGINATION_INPUT=json.dumps({'run_id':os.environ['ADDP_ONLINE_TEST_RUN_ID'],
       'tenant_id':str(state['tenant_id']),'principal_id':state['principal_id'],'execution_ids':state['execution_ids'],
       'ids':state['ids'],'nonce':state['nonce']}))
  code=run_private(['npm','exec','--','playwright','test','--config=playwright.online.config.js','--output='+str(private/'output'),
      'e2e/online/execution-events-pagination.spec.js'],ROOT/'console/frontend',environment,private/'playwright.log',240)
  require(code==0,'event pagination browser failed')
  result=json.loads((artifact/'execution-events-pagination-browser.json').read_text())
  require(set(result)=={'schema_version','result','checks','quota_rows','history_rows','refresh_rows'} and result['schema_version']=='addp.execution-events-browser/v1'
    and result['result']=='passed' and result['checks']==checks and result['quota_rows']==result['history_rows']==1000 and result['refresh_rows']==100,
    'browser report incomplete')
  return result


def run():
 secret,artifact=BASE.boundary(SUITE)
 report=dict(schema_version='addp.online-suite/v1',suite=SUITE,run_id=os.environ['ADDP_ONLINE_TEST_RUN_ID'],result='failed',
    fixture_source='synthetic_execution_common_lease_writer',owner_worker_verified=False,overview_physical_deletion_enabled=False,
    professional_result_lifecycle_verified=False,cleanup='pending_deployment_destruction',checks=[])
 stage='identity'
 try:
  client=API.GatewayClient(os.environ['GATEWAY_URL'],os.environ['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],10)
  report['identity']=API.validate_user_identity(client,int(os.environ['ADDP_ONLINE_TEST_TENANT_ID']),PERMISSIONS)
  peers=[]
  for label,tenant in [('PEER',int(os.environ['ADDP_ONLINE_TEST_TENANT_ID'])),('FOREIGN',int(os.environ['ADDP_ONLINE_EVENTS_FOREIGN_TENANT_ID']))]:
   peer=API.GatewayClient(os.environ['GATEWAY_URL'],os.environ[f'ADDP_ONLINE_EVENTS_{label}_ACCESS_TOKEN'],10)
   API.validate_user_identity(peer,tenant,PERMISSIONS);peers.append(peer)
  stage='fixture'
  code=run_private(['make','test-execution-event-pagination-fixture'],ROOT,dict(os.environ),secret/'event-fixture-output.log',180)
  require(code==0,'Common event fixture failed')
  state=json.loads((secret/'event-pagination-fixture.json').read_text());ids=state['execution_ids']
  require(len(ids)==2 and len(set(ids))==2 and state['tenant_id']==int(os.environ['ADDP_ONLINE_TEST_TENANT_ID']) and state['quota_write_attempts']==1005,'invalid fixture state')
  before=snapshot();require(before['executions']==2 and before['events']==2201,'fixture row counts differ')
  report['checks'].append('common_real_lease_writer_and_terminal_status')
  stage='api_pages';counts={};cursors=[]
  for name,identity in zip(('quota','history'),ids):
   detail=client.request('GET',PREFIX+'/'+identity,(200,)).payload
   assert_safe(detail);require(detail.get('metadata')=={},'synthetic private metadata was projected');require(state['nonce'] not in json.dumps(detail) and detail['status']=='success' and detail['progress']==100,'safe terminal overview differs')
   items,pages=walk(client,identity,state['ids'][name],state['nonce']);counts[name]=dict(events=len(items),pages=pages)
   markers=[i for i in items if i['kind']=='truncated']
   if name=='quota':require(pages==10 and len(items)==1000 and len(markers)==1 and items[-1]['kind']=='truncated','storage truncation missing')
   else:require(pages==13 and len(items)==1201 and not markers,'history display fixture incorrectly truncated at collection')
   cursors.append(items[99]['id'])
   for query in ('after=-1&limit=100','after=bad&limit=100','after=0&limit=101'):
    error=client.request('GET',PREFIX+'/'+identity+'/events?'+query,(400,)).payload
    require(set(error)=={'error','error_code'} and error['error_code']=='invalid_params','invalid page query not rejected')
  report['api']=counts;report['checks'].extend(['ten_and_thirteen_real_api_pages','ids_order_and_no_duplicates','closed_fields_and_retention_window','collection_quota_and_display_cap_distinct','invalid_page_parameters_rejected'])
  stage='owner_scope'
  for peer in peers:assert_invisible(peer,ids,cursors)
  report['checks'].append('same_tenant_peer_and_foreign_cursor_denied_without_count_leak')
  stage='browser';report['browser']=browser(secret,artifact,state)
  report['checks'].append('real_console_owner_authorized_pagination_and_truncation')
  stage='read_preservation';require(snapshot()==before,'diagnostic reads changed execution or event facts')
  report['checks'].append('reads_preserve_full_execution_and_event_rows')
  report['result']='passed'
 except Exception:
  report.update(error_code='execution_events_pagination_failed',failed_stage=stage)
  raise
 finally:(artifact/f'{SUITE}.json').write_text(json.dumps(report,sort_keys=True)+'\n')
 print(json.dumps(report,sort_keys=True))


def main():
 try:run()
 except Exception as error:
  print('execution_events_pagination_failed:'+type(error).__name__,file=__import__('sys').stderr)
  return 1
 return 0

if __name__=='__main__':raise SystemExit(main())
