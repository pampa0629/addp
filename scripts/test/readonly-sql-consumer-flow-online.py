#!/usr/bin/env python3
"""One exact read-only SQL consumer flow, with engine-owned native expectations."""
from __future__ import annotations
from decimal import Decimal
from dataclasses import dataclass
import argparse
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


@dataclass(frozen=True)
class ConsumerProfile:
    engine_type: str
    namespace: str
    source_columns: tuple[str, ...]
    sample_name: str
    empty_value: object
    text_cast: str

PROFILES = {
    'gaussdb': ConsumerProfile('gaussdb', 'addp_acceptance', ('id','name','amount','optional_text'), '高斯正式接入中文', None, 'TEXT'),
    'starrocks': ConsumerProfile('starrocks', 'business', ('name','id','amount','optional_text'), '正式接入中文', '', 'CHAR(128)'),
}


def require(ok,message):
    if not ok:
        raise API.SuiteError(message)


def validate_item(item, profile):
    require(item.get('item_type')=='table' and item.get('full_name')==profile.namespace+'.source' and bool(item.get('fingerprint')), 'scanned read-only SQL item identity differs')
    attrs=item.get('attributes',{})
    require(attrs.get('schema_version')==1 and attrs.get('item',{}).get('data_type')=='table', 'Meta item attributes required')
    table=attrs.get('type_info',{}).get('table',{})
    fields=table.get('fields',[])
    require(table.get('primary_key')==['name','id'], f"read-only SQL native primary key order differs: {table.get('primary_key')!r}")
    require({f['name'] for f in fields if f.get('primary_key')}=={'name','id'}, 'read-only SQL primary key membership differs')
    require(table.get('row_count')==2, f"read-only SQL exact row count differs: {table.get('row_count')!r}")
    require([f['name'] for f in fields]==list(profile.source_columns), 'read-only SQL source column order differs')
    by_name={field['name']:field for field in fields}
    require(by_name['id'].get('type')=='bigint' and by_name['id'].get('primary_key') is True and fields[2].get('type')=='decimal' and fields[2].get('precision')==38 and fields[2].get('scale')==18, 'read-only SQL bigint/Decimal schema lost')
    caps=attrs.get('capabilities',{})
    require(not any(key in caps for key in ('indexing','constraints','partitioning','spatial')), 'uncertified enrichment persisted')


def validate_preview(payload, profile):
    data=payload.get('data',{})
    require(payload.get('preview_type')=='table' and data.get('columns')==list(profile.source_columns) and data.get('total')==2, 'read-only SQL preview shape differs')
    rows=data.get('rows',[])
    require(len(rows)==2 and all(isinstance(r.get('id'),str) and isinstance(r.get('amount'),str) for r in rows), 'bigint/Decimal must enter JSON as exact strings')
    values={r['id']:r for r in rows}
    require(set(values)=={'2','9007199254740993'}, 'bigint JSON lost precision')
    require(values['9007199254740993']['name']==profile.sample_name and values['9007199254740993']['amount']=='12345678901234567890.123456789012345678', 'Chinese or Decimal(38,18) changed')
    require(values['2']['name']=='保留尾空格  ' and Decimal(values['2']['amount'])==Decimal('-0.000000000000000001'), 'trailing spaces or negative Decimal changed')
    require(values['9007199254740993']['optional_text'] is None and values['2']['optional_text']==profile.empty_value, 'native NULL/empty-string semantics changed')
    return rows


def run_browser(report):
    output=Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'))/(report['engine_type']+'-console.json')
    output.unlink(missing_ok=True)
    environment={**os.environ,'ADDP_ONLINE_READONLY_SQL_EXPECTATIONS':json.dumps(report),'ADDP_ONLINE_READONLY_SQL_BROWSER_REPORT':str(output)}
    result=subprocess.run(['npm','exec','--','playwright','test','e2e/online/readonly-sql-consumer-flow.spec.js','--config=playwright.online.config.js'],cwd=Path(__file__).resolve().parents[2]/'console/frontend',env=environment)
    require(result.returncode==0 and output.is_file(), 'read-only SQL Console acceptance failed or omitted evidence')
    actual=json.loads(output.read_text())
    validate_browser_report(actual,report,PROFILES[report['engine_type']])
    return actual


def validate_browser_report(actual,report,profile):
    expected={key:report[key] for key in ('run_id','tenant_id','principal_id','engine_id')}
    require({key:actual.get(key) for key in expected}==expected,'Console consumer identity evidence differs')
    queries=actual.get('queries',[])
    require(len(queries)==2 and len({q.get('execution_id') for q in queries})==2, 'two distinct workbench executions required')
    for index,query in enumerate(queries):
        require(query.get('status')=='success' and query.get('effect')=='read', 'workbench read execution failed')
        fields=query.get('fields',[])
        require([field.get('name') for field in fields]==['id','name','amount','optional_text']+(['bound_text'] if index else []), 'workbench output schema differs')
        require(fields[0].get('type')=='bigint' and fields[2].get('type')=='decimal' and fields[2].get('precision')==38 and fields[2].get('scale')==18, 'workbench real bigint/Decimal schema lost')
        if index==0:
            validate_preview({'preview_type':'table','data':{'columns':list(profile.source_columns),'total':2,'rows':query.get('rows')}}, profile)
        else:
            rows=query.get('rows',[])
            expected_rows=[row for row in queries[0]['rows'] if int(row['id'])>=3]
            require(len(rows)==len(expected_rows)==1, 'integer parameter did not filter the source')
            require({key:rows[0].get(key) for key in expected_rows[0]}==expected_rows[0], 'parameter query changed exact values')
    require(all(row.get('bound_text')=="中文' OR 1=1 --" for row in queries[1]['rows']), 'text parameter was not bound exactly')


def run(client,tenant,engine,timeout,profile):
    require(tenant>1,'disposable nondefault Tenant required')
    identity=API.validate_user_identity(client,tenant,PERMISSIONS)
    context=client.request('GET','/api/v1/system/auth/context',(200,)).payload
    actual={p for role in context['authorization']['role_assignments'] for p in role['permissions']}
    require(actual==PERMISSIONS,'consumer has extra permissions')
    engines=client.request('GET','/api/v1/system/engine-catalog/engines',(200,)).payload
    matches=[e for e in engines if e['id']==engine]
    require(len(matches)==1 and matches[0].get('engine_type')==profile.engine_type and 'connection_info' not in matches[0], 'read-only SQL registration identity or credential boundary differs')
    storage=matches[0].get('capabilities',{}).get('storage',{})
    require(storage.get('store',{}).get('batch_read') is True and not storage.get('store',{}).get('batch_write'), 'read-only capabilities differ')
    scan=SUPPORT.wait_for_scan(client,engine,time.monotonic()+timeout)
    items=client.request('GET',f'/api/v1/meta/engines/{engine}/items?branch={profile.namespace}',(200,)).payload
    require(isinstance(items,list) and len(items)==1,'scan leaked ungranted tables or omitted the source')
    item=items[0]
    validate_item(item,profile)
    target=SUPPORT.build_item_locator(engine,item)
    preview=client.request('GET','/api/v1/manager/preview?'+urlencode({'locator':target,'page':1,'page_size':20}),(200,)).payload
    rows=validate_preview(preview,profile)
    report={'suite':profile.engine_type+'-consumer-flow','engine_type':profile.engine_type,'namespace':profile.namespace,'source_columns':list(profile.source_columns),'sample_name':profile.sample_name,'text_cast':profile.text_cast,'run_id':SUPPORT.required_environment('ADDP_ONLINE_TEST_RUN_ID'),'tenant_id':tenant,'principal_id':str(context['principal']['id']),'engine_id':engine,'engine_name':matches[0]['name'],'scan_execution_id':scan,'item_id':item['id'],'locator':target,'rows':rows,'identity':identity}
    report['browser']=run_browser(report)
    return report


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--engine-type',choices=PROFILES,required=True)
    profile=PROFILES[parser.parse_args().engine_type]
    require(os.environ.get('ADDP_ONLINE_HOSTED')=='1' and os.environ.get('ADDP_ONLINE_TEST')=='1','Hosted Online entry required')
    tenant=SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_TEST_TENANT_ID'),'Tenant')
    engine=SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_CONSUMER_ENGINE_ID'),'read-only SQL')
    client=SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'),SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'),30)
    report=run(client,tenant,engine,float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','900')),profile)
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'),profile.engine_type+'-consumer-flow.json').write_text(json.dumps(report,ensure_ascii=False,indent=2))
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))
    return 0


if __name__=='__main__':
    try:
        raise SystemExit(main())
    except (RuntimeError,OSError,ValueError,KeyError) as error:
        print(str(error),file=sys.stderr)
        raise SystemExit(1)
