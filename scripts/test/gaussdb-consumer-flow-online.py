#!/usr/bin/env python3
"""GaussDB independent registration, SELECT-only scan and exact Console preview."""
from __future__ import annotations
from decimal import Decimal
from importlib import import_module
import json
import os
from pathlib import Path
import subprocess
import sys
import time
from urllib.parse import urlencode

API=import_module('scripts.utils.online-api')
SUPPORT=import_module('scripts.test.security-transfer-protection-online')
PERMISSIONS={'system.engine_catalog.read','meta.catalog.read','meta.scan_task.execute','meta.scan_task.read','manager.data_item.read','manager.content.read','develop.task.read','develop.task.execute','develop.data_read.execute','system.execution_authorization.create'}


def require(ok,message):
    if not ok:
        raise API.SuiteError(message)


def validate_item(item):
    require(item.get('item_type')=='table' and item.get('full_name')=='addp_acceptance.source' and bool(item.get('fingerprint')), 'scanned GaussDB item identity differs')
    attrs=item.get('attributes',{})
    require(attrs.get('schema_version')==1 and attrs.get('item',{}).get('data_type')=='table', 'Meta item attributes required')
    table=attrs.get('type_info',{}).get('table',{})
    fields=table.get('fields',[])
    require(table.get('primary_key')==['name','id'], f"GaussDB native primary key order differs: {table.get('primary_key')!r}")
    require({f['name'] for f in fields if f.get('primary_key')}=={'name','id'}, 'GaussDB primary key membership differs')
    require(table.get('row_count')==2, f"GaussDB exact row count differs: {table.get('row_count')!r}")
    require([f['name'] for f in fields]==['id','name','amount','optional_text'], 'GaussDB source column order differs')
    require(fields[0].get('type')=='bigint' and fields[0].get('primary_key') is True and fields[2].get('type')=='decimal' and fields[2].get('precision')==38 and fields[2].get('scale')==18, 'GaussDB bigint/Decimal schema lost')
    caps=attrs.get('capabilities',{})
    require(not any(key in caps for key in ('indexing','constraints','partitioning','spatial')), 'uncertified enrichment persisted')


def validate_preview(payload):
    data=payload.get('data',{})
    require(payload.get('preview_type')=='table' and data.get('columns')==['id','name','amount','optional_text'] and data.get('total')==2, 'GaussDB preview shape differs')
    rows=data.get('rows',[])
    require(len(rows)==2 and all(isinstance(r.get('id'),str) and isinstance(r.get('amount'),str) for r in rows), 'bigint/Decimal must enter JSON as exact strings')
    values={r['id']:r for r in rows}
    require(set(values)=={'2','9007199254740993'}, 'bigint JSON lost precision')
    require(values['9007199254740993']['name']=='高斯正式接入中文' and values['9007199254740993']['amount']=='12345678901234567890.123456789012345678', 'Chinese or Decimal(38,18) changed')
    require(values['2']['name']=='保留尾空格  ' and Decimal(values['2']['amount'])==Decimal('-0.000000000000000001'), 'trailing spaces or negative Decimal changed')
    # Business uses the native default A compatibility mode: an empty string is
    # stored as NULL. Do not rewrite the database mode or invent an empty value.
    require(all(r['optional_text'] is None for r in rows), 'native A-mode null semantics changed')
    return rows


def run_browser(report):
    output=Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'))/'gaussdb-console.json'
    output.unlink(missing_ok=True)
    environment={**os.environ,'ADDP_ONLINE_GAUSSDB_EXPECTATIONS':json.dumps(report),'ADDP_ONLINE_GAUSSDB_BROWSER_REPORT':str(output)}
    result=subprocess.run(['npm','exec','--','playwright','test','e2e/online/gaussdb-consumer-flow.spec.js','--config=playwright.online.config.js'],cwd=Path(__file__).resolve().parents[2]/'console/frontend',env=environment)
    require(result.returncode==0 and output.is_file(), 'GaussDB Console acceptance failed or omitted evidence')
    actual=json.loads(output.read_text())
    validate_browser_report(actual,report)
    return actual


def validate_browser_report(actual,report):
    expected={key:report[key] for key in ('run_id','tenant_id','principal_id','engine_id')}
    require({key:actual.get(key) for key in expected}==expected,'Console consumer identity evidence differs')
    queries=actual.get('queries',[])
    require(len(queries)==2 and len({q.get('execution_id') for q in queries})==2, 'two distinct workbench executions required')
    for query in queries:
        require(query.get('status')=='success' and query.get('effect')=='read', 'workbench read execution failed')
        validate_preview({'preview_type':'table','data':{'columns':['id','name','amount','optional_text'],'total':2,'rows':query.get('rows')}})
    require(all(row.get('bound_text')=="中文' OR 1=1 --" for row in queries[1]['rows']), 'text parameter was not bound exactly')


def run(client,tenant,engine,timeout):
    require(tenant>1,'disposable nondefault Tenant required')
    identity=API.validate_user_identity(client,tenant,PERMISSIONS)
    context=client.request('GET','/api/v1/system/auth/context',(200,)).payload
    actual={p for role in context['authorization']['role_assignments'] for p in role['permissions']}
    require(actual==PERMISSIONS,'consumer has extra permissions')
    engines=client.request('GET','/api/v1/system/engine-catalog/engines',(200,)).payload
    matches=[e for e in engines if e['id']==engine]
    require(len(matches)==1 and matches[0].get('engine_type')=='gaussdb' and 'connection_info' not in matches[0], 'GaussDB registration identity or credential boundary differs')
    storage=matches[0].get('capabilities',{}).get('storage',{})
    require(storage.get('store',{}).get('batch_read') is True and not storage.get('store',{}).get('batch_write'), 'read-only capabilities differ')
    scan=SUPPORT.wait_for_scan(client,engine,time.monotonic()+timeout)
    items=client.request('GET',f'/api/v1/meta/engines/{engine}/items?branch=addp_acceptance',(200,)).payload
    require(isinstance(items,list) and len(items)==1,'scan leaked ungranted tables or omitted the source')
    item=items[0]
    validate_item(item)
    target=SUPPORT.build_item_locator(engine,item)
    preview=client.request('GET','/api/v1/manager/preview?'+urlencode({'locator':target,'page':1,'page_size':20}),(200,)).payload
    rows=validate_preview(preview)
    report={'suite':'gaussdb-consumer-flow','run_id':SUPPORT.required_environment('ADDP_ONLINE_TEST_RUN_ID'),'tenant_id':tenant,'principal_id':str(context['principal']['id']),'engine_id':engine,'engine_name':matches[0]['name'],'scan_execution_id':scan,'item_id':item['id'],'locator':target,'rows':rows,'identity':identity}
    report['browser']=run_browser(report)
    return report


def main():
    require(os.environ.get('ADDP_ONLINE_HOSTED')=='1' and os.environ.get('ADDP_ONLINE_TEST')=='1','Hosted Online entry required')
    tenant=SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_TEST_TENANT_ID'),'Tenant')
    engine=SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_CONSUMER_ENGINE_ID'),'GaussDB')
    client=SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'),SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'),30)
    report=run(client,tenant,engine,float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','900')))
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'),'gaussdb-consumer-flow.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))
    return 0


if __name__=='__main__':
    try:
        raise SystemExit(main())
    except (RuntimeError,OSError,ValueError,KeyError) as error:
        print(str(error),file=sys.stderr)
        raise SystemExit(1)
