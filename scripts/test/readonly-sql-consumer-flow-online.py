#!/usr/bin/env python3
"""One exact read-only SQL consumer flow, with engine-owned native expectations."""
from __future__ import annotations
from decimal import Decimal
from dataclasses import dataclass
import argparse
from importlib import import_module
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import time
import uuid
from urllib.parse import urlencode

API=import_module('scripts.utils.online-api')
SUPPORT=import_module('scripts.test.security-transfer-protection-online')
REGISTRATION=import_module('scripts.test.online-engine-registration')
PERMISSIONS={'system.engine_catalog.read','meta.catalog.read','manager.data_item.read','manager.content.read','develop.task.read','develop.task.execute','develop.data_read.execute','system.execution_authorization.create'}


@dataclass(frozen=True)
class ConsumerProfile:
    engine_type: str
    namespace: str
    namespace_term: str
    source_columns: tuple[str, ...]
    sample_name: str
    empty_value: object
    text_cast: str
    catalog_tables: tuple[str, ...]

PROFILES = {
    'gaussdb': ConsumerProfile('gaussdb', 'addp_acceptance', 'schema', ('id','name','amount','optional_text'), '高斯正式接入中文', None, 'TEXT', ('source',)),
    'starrocks': ConsumerProfile('starrocks', 'business', 'database', ('name','id','amount','optional_text'), '正式接入中文', '', 'CHAR(128)', ('source','customers','products','orders')),
}


def require(ok,message):
    if not ok:
        raise API.SuiteError(message)


def private_directory(profile):
    fixture = import_module('business.scripts.online-gaussdb-consumer-fixture' if profile.engine_type == 'gaussdb' else 'business.starrocks.admission')
    return fixture.directory()


def prepare(profile, tenant, engine):
    private = private_directory(profile)
    consumer = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), 30)
    preparer = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'), 30)
    API.validate_user_identity(consumer, tenant, PERMISSIONS)
    participants = REGISTRATION.source_grant_participants(preparer, consumer, tenant, scan_preparation=True)
    scan = SUPPORT.wait_for_scan(preparer, engine, time.monotonic()+180, scan_depth='deep', targets=[
        f'addp://engine/{engine}/path/{profile.namespace}/{name}?type=table' for name in profile.catalog_tables])
    journal = private / 'readonly-sql-grants.json'
    with open(journal, 'x', opener=lambda path, flags: os.open(path, flags, 0o600)) as output:
        json.dump([], output)
    reads, metadata = [], []
    def checkpoint():
        journal.write_text(json.dumps(reads+metadata))
    def on_pending(engine_id, request_id, target):
        reads.append(dict(engine=engine_id, request_id=request_id, target=target, action='read'))
        checkpoint()
    if profile.engine_type == 'gaussdb':
        verify_gaussdb_future_table(preparer, consumer, engine, private, participants, journal)
    REGISTRATION.initialize_exact_record_read_grants(preparer, consumer, tenant,
        [(engine, profile.namespace_term, profile.namespace, 'source')], scan_preparation=True, on_pending=on_pending)
    def on_metadata(rows):
        metadata[:] = rows
        checkpoint()
    grants = REGISTRATION.ExactRecordGrants(preparer, consumer, tenant, action='metadata_read', scan_preparation=True, on_change=on_metadata)
    for name in profile.catalog_tables[1:]:
        grants.grant(engine, profile.namespace, name, 'table')
        consumer.request('POST', '/api/v1/system/engine-access/read-observations/current-source', (403,), {'targets':[grants.target(engine,profile.namespace,name,'table')]})
    (private / 'readonly-sql-scan.json').write_text(json.dumps(scan))


def verify_gaussdb_future_table(preparer, consumer, engine, private, participants, journal):
    fixture = import_module('business.scripts.online-gaussdb-consumer-fixture')
    state = json.loads((private / 'gaussdb-state.json').read_text())
    operator, recipient = participants
    future = REGISTRATION.ExactRecordGrants.target(engine, 'addp_acceptance', 'node_future', 'table', 'schema')
    scope = dict(future, segments=future['segments'][:-1])
    command = str(uuid.uuid4())
    base = f'/api/v1/system/engines/{engine}/access_grants'
    body = dict(request_id=command, catalog_path=scope, node_recursive=False, action='read',
                recipient_type='user', recipient_id=recipient, expiry_mode='until_revoked',
                reason='Disposable GaussDB schema read for a future table')
    # Before the POST, persist the command and an existing leaf for the outer
    # cleanup hook. This also covers a lost POST or revoke response.
    current = REGISTRATION.ExactRecordGrants.target(engine, 'addp_acceptance', 'source', 'table', 'schema')
    journal.write_text(json.dumps([dict(engine=engine, request_id=command, target=current, action='read')]))
    receipt = preparer.request('POST', base, (201,), body).payload
    require(all(receipt.get(key) == body[key] for key in
                ('request_id', 'catalog_path', 'recipient_type', 'recipient_id', 'action', 'expiry_mode')) and
            receipt.get('node_recursive') is False and receipt.get('engine_id') == str(engine) and
            receipt.get('operator_principal_id') == operator and not receipt.get('item_fingerprint'),
            'GaussDB schema Grant did not bind the exact node and participants')
    # Test-owned isolated GaussDB. The original source role keeps native SELECT
    # even after platform withdrawal, so a native ACL cannot mask a regression.
    fixture.sql(state, """BEGIN;
CREATE TABLE addp_acceptance.node_future(id INTEGER PRIMARY KEY);
INSERT INTO addp_acceptance.node_future VALUES (37);
GRANT SELECT ON addp_acceptance.node_future TO fixture_reader;
CREATE SCHEMA node_control;
CREATE TABLE node_control.outside_source(id INTEGER PRIMARY KEY);
INSERT INTO node_control.outside_source VALUES (83);
GRANT USAGE ON SCHEMA node_control TO fixture_reader;
GRANT SELECT ON node_control.outside_source TO fixture_reader;
COMMIT;""")
    scan = SUPPORT.wait_for_scan(preparer, engine, time.monotonic()+180, scan_depth='basic',
                                 targets=[f'addp://engine/{engine}/path/addp_acceptance/node_future?type=table',
                                          f'addp://engine/{engine}/path/node_control/outside_source?type=table'])
    items = consumer.request('GET', f'/api/v1/meta/engines/{engine}/items?branch=addp_acceptance', (200,)).payload
    future_items = [item for item in items if item.get('full_name') == 'addp_acceptance.node_future']
    require(len(future_items) == 1, 'new table missing under the original schema grant')
    locator = SUPPORT.build_item_locator(engine, future_items[0])
    query = 'SELECT id FROM addp_acceptance.node_future ORDER BY id'
    before = fixture.table_read_count(state, 'node_future')
    allowed = sql_execution(consumer, engine, query, locator)
    require(allowed.get('status') == 'success' and
            allowed.get('metadata', {}).get('result', {}).get('summary', {}).get('preview_rows') == [{'id':37}],
            'schema Grant did not return the new table row')
    after = fixture.table_read_count(state, 'node_future')
    require(after > before, 'allowed query was absent from the owned source execution log')
    existing, = [item for item in items if item.get('full_name') == 'addp_acceptance.source']
    current_before = fixture.table_read_count(state, 'source')
    existing_result = sql_execution(consumer, engine,
        'SELECT name FROM addp_acceptance.source WHERE id = 9007199254740993', SUPPORT.build_item_locator(engine, existing))
    require(existing_result.get('status') == 'success' and
            existing_result.get('metadata', {}).get('result', {}).get('summary', {}).get('preview_rows') == [{'name':'高斯正式接入中文'}],
            'schema Grant did not return the existing table row')
    current_reads = fixture.table_read_count(state, 'source') - current_before
    require(current_reads > 0, 'existing table query was absent from the owned source log')
    outside_before = fixture.table_read_count(state, 'outside_source', 'node_control')
    outside_result = sql_execution(consumer, engine, 'SELECT id FROM node_control.outside_source ORDER BY id',
                                      f'addp://engine/{engine}/path/node_control/outside_source?type=table')
    assert_source_read_denied(outside_result)
    outside_reads = fixture.table_read_count(state, 'outside_source', 'node_control') - outside_before
    require(outside_reads == 0, 'schema Grant allowed a source read outside its namespace')
    preparer.request('POST', base + '/' + command + '/revoke', (200,), {'reason':'End future-table read'})
    denied = sql_execution(consumer, engine, query, locator)
    require(denied['execution_id'] != allowed['execution_id'], 'replayed the previous execution')
    assert_source_read_denied(denied)
    final = fixture.table_read_count(state, 'node_future')
    require(final == after, 'withdrawn execution issued a new source SELECT')
    evidence = dict(engine_id=engine, principal_id=recipient, scope=scope, grant=command, scan=scan,
                    allowed_execution=allowed['execution_id'], denied_execution=denied['execution_id'],
                    allowed_source_selects=after-before, denied_source_selects=final-after,
                    existing_execution=existing_result['execution_id'], existing_source_selects=current_reads,
                    outside_denied_execution=outside_result['execution_id'], outside_source_selects=outside_reads)
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'), 'gaussdb-node-read.json').write_text(json.dumps(evidence))
    journal.write_text('[]')


def cleanup(profile):
    pending = json.loads((private_directory(profile) / 'readonly-sql-grants.json').read_text())
    preparer = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'), 30)
    consumer = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), 30)
    revoked, errors = [], []
    for row in pending:
        try:
            preparer.request('POST', f"/api/v1/system/engines/{row['engine']}/access_grants/{row['request_id']}/revoke", (200,404), {'reason':'Disposable read-only SQL acceptance finished'})
            observed=consumer.request('POST', '/api/v1/system/engine-access/metadata-checks/meta-query', (200,), {'targets':[row['target']]}).payload
            require(isinstance(observed,dict) and observed.get('observed_at') and observed.get('targets')==[dict(target=row['target'],visible=False)], 'revoked record remains visible')
            consumer.request('POST', '/api/v1/system/engine-access/read-observations/current-source', (403,), {'targets':[row['target']]})
            revoked.append(row)
        except (RuntimeError,OSError,ValueError):
            errors.append(row['request_id'])
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'), profile.engine_type+'-grant-cleanup.json').write_text(json.dumps(dict(grants=revoked,failed_requests=errors,metadata_hidden=not errors,read_denied=not errors)))
    require(not errors,'read-only SQL exact Grant cleanup incomplete')


def validate_structure(item,profile):
    attrs=item.get('attributes',{})
    table=attrs.get('type_info',{}).get('table',{})
    require(item.get('row_count') is None and 'row_count' not in table and not attrs.get('capabilities'), 'public Meta leaked content statistics or enrichment')
    validate_fields(item,profile)


def select_source(items, profile):
    expected={profile.namespace+'.'+table for table in profile.catalog_tables}
    require(isinstance(items,list) and len(items)==len(expected) and
            {item.get('full_name') for item in items}==expected and
            all(item.get('item_type')=='table' for item in items),
            'scan leaked ungranted tables or omitted the expected examples')
    return next(item for item in items if item['full_name']==profile.namespace+'.source')


def validate_fields(item, profile):
    require(item.get('item_type')=='table' and item.get('full_name')==profile.namespace+'.source' and bool(item.get('fingerprint')), 'scanned read-only SQL item identity differs')
    attrs=item.get('attributes',{})
    require(attrs.get('schema_version')==1 and attrs.get('item',{}).get('data_type')=='table', 'Meta item attributes required')
    table=attrs.get('type_info',{}).get('table',{})
    fields=table.get('fields',[])
    require(table.get('primary_key')==['name','id'], f"read-only SQL native primary key order differs: {table.get('primary_key')!r}")
    require({f['name'] for f in fields if f.get('primary_key')}=={'name','id'}, 'read-only SQL primary key membership differs')
    require([f['name'] for f in fields]==list(profile.source_columns), 'read-only SQL source column order differs')
    by_name={field['name']:field for field in fields}
    require(by_name['id'].get('type')=='bigint' and by_name['id'].get('primary_key') is True and fields[2].get('type')=='decimal' and fields[2].get('precision')==38 and fields[2].get('scale')==18, 'read-only SQL bigint/Decimal schema lost')
    caps=attrs.get('capabilities',{})
    require(not any(key in caps for key in ('indexing','constraints','partitioning','spatial')), 'uncertified enrichment persisted')


def validate_item(item,profile):
    validate_fields(item,profile)
    count=item['attributes']['type_info']['table'].get('row_count')
    require(count==2,f'read-only SQL exact row count differs: {count!r}')


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
    require(not os.environ.get('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN') and not os.environ.get('ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'),'preparation credentials leaked into consumer stage')
    scan=json.loads((private_directory(profile)/'readonly-sql-scan.json').read_text())
    items=client.request('GET',f'/api/v1/meta/engines/{engine}/items?branch={profile.namespace}',(200,)).payload
    item=select_source(items,profile)
    validate_structure(item,profile)
    metadata_only=[]
    for name in profile.catalog_tables[1:]:
        record=next(row for row in items if row['full_name']==profile.namespace+'.'+name)
        locator=SUPPORT.build_item_locator(engine,record)
        client.request('GET','/api/v1/manager/preview?'+urlencode({'locator':locator,'page':1,'page_size':20}),(403,))
        target=REGISTRATION.ExactRecordGrants.target(engine,profile.namespace,name,'table')
        client.request('POST','/api/v1/system/engine-access/read-observations/current-source',(403,),{'targets':[target]})
        metadata_only.append(dict(target=target,locator=locator,preview_status=403,content_readable=False))
    target=SUPPORT.build_item_locator(engine,item)
    preview=client.request('GET','/api/v1/manager/preview?'+urlencode({'locator':target,'page':1,'page_size':20}),(200,)).payload
    rows=validate_preview(preview,profile)
    protected={row['key']:row['value'] for row in preview['data']['item_meta']['attributes']}
    validate_item(dict(item,attributes=protected),profile)
    report={'suite':profile.engine_type+'-consumer-flow','engine_type':profile.engine_type,'namespace':profile.namespace,'source_columns':list(profile.source_columns),'sample_name':profile.sample_name,'text_cast':profile.text_cast,'run_id':SUPPORT.required_environment('ADDP_ONLINE_TEST_RUN_ID'),'tenant_id':tenant,'principal_id':str(context['principal']['id']),'engine_id':engine,'engine_name':matches[0]['name'],'scan_execution_id':scan,'item_id':item['id'],'locator':target,'rows':rows,'identity':identity,'permissions':sorted(PERMISSIONS),'catalog_tables':list(profile.catalog_tables),'metadata_only':metadata_only}
    report['browser']=run_browser(report)
    return report


def sql_execution(client, engine, query, locator):
    submitted = client.request('POST', '/api/v1/develop/executions', (200,), {
        'dev_type': 'query', 'trigger_type': 'manual', 'execution_config': {'engine_id': engine},
        'content': {'query_type': 'sql', 'query': query, 'target_locator': locator},
        'timeout': 120,
    }).payload['execution_id']
    require(isinstance(submitted, str) and submitted, 'missing Develop execution ID')
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        result = client.request('GET', f'/api/v1/develop/executions/{submitted}/result', (200,)).payload
        require(result.get('execution_id') == submitted, 'wrong execution result')
        if result.get('status') not in ('pending', 'running'):
            break
        time.sleep(1)
    else:
        raise API.SuiteError('Develop execution did not reach a terminal state within 120 seconds')
    return result


def assert_source_read_denied(result):
    details = result.get('error_details') or {}
    diagnostic = details.get('diagnostic') or {}
    require(result.get('status') == 'failed' and details.get('code') == 'develop.query.source_read_denied' and
            diagnostic.get('stage') == 'authorization' and diagnostic.get('reason') == 'permission_denied',
            'execution did not fail during source authorization')
    require((result.get('metadata') or {}).get('result') is None and not result.get('rows_affected'),
            'unauthorized execution returned a result')


def verify_gaussdb_revoked_execution(client, tenant, engine, directory):
    report = json.loads((directory / 'gaussdb-consumer-flow.json').read_text())
    browser = report['browser']
    run_id = SUPPORT.required_environment('ADDP_ONLINE_TEST_RUN_ID')
    API.validate_user_identity(client, tenant, PERMISSIONS)
    identity = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    require(browser['principal_id'] == str(identity['principal']['id']) and
            browser['tenant_id'] == tenant and browser['engine_id'] == engine and
            browser['run_id'] == run_id, 'revocation check must use the original consumer and engine')
    cleanup = json.loads((directory / 'gaussdb-grant-cleanup.json').read_text())
    target = REGISTRATION.ExactRecordGrants.target(engine, 'addp_acceptance', 'source', 'table', 'schema')
    grants = cleanup['grants']
    require(cleanup['read_denied'] is True and not cleanup['failed_requests'] and len(grants) == 1 and
            grants[0]['engine'] == engine and grants[0]['action'] == 'read' and grants[0]['target'] == target,
            'confirmed revocation of the fixture source required')
    first = browser['queries'][0]
    require(first['status'] == 'success' and len(first['rows']) == 2 and
            first['query'] == 'SELECT id,name,amount,optional_text FROM addp_acceptance.source ORDER BY id',
            'successful reference query required before revocation')
    result = sql_execution(client, engine, first['query'], first['target_locator'])
    submitted = result['execution_id']
    require(submitted not in {q['execution_id'] for q in browser['queries']},
            'revocation check requires a fresh execution')
    assert_source_read_denied(result)
    details = result['error_details']
    diagnostic = details['diagnostic']
    evidence = dict(run_id=run_id, tenant_id=tenant, engine_id=engine, principal_id=browser['principal_id'],
                    allowed_execution=first['execution_id'], denied_execution=submitted, code=details['code'],
                    stage=diagnostic['stage'], result_delivered=False)
    (directory / 'gaussdb-revoked-execution.json').write_text(json.dumps(evidence))


STARROCKS_READ = "SELECT optional_text FROM business.source WHERE id = 2"
STARROCKS_JOIN = ("SELECT a.name FROM business.source a "
                  "JOIN business.customers b ON a.id = b.customer_id")


def starrocks_business_selects(before, after):
    require(after.startswith(before), 'StarRocks FE audit window was replaced or rotated')
    ids = []
    for line in after[len(before):].splitlines():
        metadata, separator, statement = line.partition('|Stmt=')
        if not separator or '|User=business_reader|' not in metadata:
            continue
        normalized = statement.replace('`', '').lower()
        if not re.match(r"^\s*select\b", normalized) or not re.search(
                r"\b(?:from|join)\s+(?:business\.)?(?:source|customers)\b", normalized):
            continue
        match = re.search(r"\|QueryId=([^|]+)", metadata)
        require(match and match[1] not in ('', 'null', 'NaN'), 'missing native StarRocks query ID')
        ids.append(match[1])
    return ids


def verify_starrocks_reads(client, tenant, engine, directory, revoked):
    fixture = import_module('business.starrocks.admission')
    identity = API.validate_user_identity(client, tenant, PERMISSIONS)
    context = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    principal_id = str(context['principal']['id'])
    locator = f'addp://engine/{engine}/path/business/source?type=table'
    evidence_path = directory / 'starrocks-develop-source-reads.json'
    if revoked:
        evidence = json.loads(evidence_path.read_text())
        require(evidence['engine_id'] == engine and evidence['identity'] == identity and
                evidence['principal_id'] == principal_id and
                set(evidence['checks']) == {'authorized', 'ungranted_join'},
                'revocation check requires the original StarRocks consumer and successful test window')
        cleanup = json.loads((directory / 'starrocks-grant-cleanup.json').read_text())
        target = REGISTRATION.ExactRecordGrants.target(engine, 'business', 'source', 'table')
        require(not cleanup['failed_requests'] and any(row['engine'] == engine and
                row['action'] == 'read' and row['target'] == target for row in cleanup['grants']),
                'confirmed revocation of the fixture source required')
        cases = [('revoked', STARROCKS_READ, False)]
    else:
        evidence = dict(identity=identity, principal_id=principal_id, engine_id=engine, checks={})
        cases = [('authorized', STARROCKS_READ, True), ('ungranted_join', STARROCKS_JOIN, False)]
    checks = evidence['checks']
    try:
        for phase, query, allowed in cases:
            before = fixture.read_business_audit()
            result = sql_execution(client, engine, query, locator)
            execution_id = result['execution_id']
            require(execution_id not in {check['execution_id'] for check in checks.values()},
                    'expected a new StarRocks execution ID')
            after = fixture.read_business_audit()
            ids = starrocks_business_selects(before, after)
            details = result.get('error_details') or {}
            diagnostic = details.get('diagnostic') or {}
            checks[phase] = dict(execution_id=execution_id, status=result.get('status'),
                                 query=query, source_selects=len(ids), native_query_ids=ids,
                                 audit_start_bytes=len(before.encode()), audit_end_bytes=len(after.encode()),
                                 code=details.get('code'), stage=diagnostic.get('stage'),
                                 reason=diagnostic.get('reason'))
            if allowed:
                summary = result.get('metadata', {}).get('result', {}).get('summary', {})
                require(result.get('status') == 'success' and summary.get('preview_rows') == [{'optional_text': ''}],
                        'authorized StarRocks query did not return the fixture row')
                require(len(ids) == 1, 'expected exactly one StarRocks source business SELECT')
            else:
                assert_source_read_denied(result)
                require(not ids, 'denied StarRocks execution emitted a business SELECT')
    finally:
        evidence_path.write_text(json.dumps(evidence, ensure_ascii=False, indent=2))


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--engine-type',choices=PROFILES,required=True)
    parser.add_argument('operation',nargs='?',choices=['prepare','cleanup','verify-revoked'])
    arguments=parser.parse_args()
    profile=PROFILES[arguments.engine_type]
    require(os.environ.get('ADDP_ONLINE_HOSTED')=='1' and os.environ.get('ADDP_ONLINE_TEST')=='1','Hosted Online entry required')
    tenant=SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_TEST_TENANT_ID'),'Tenant')
    engine=SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_CONSUMER_ENGINE_ID'),'read-only SQL')
    if arguments.operation:
        require(os.environ.get('GITHUB_ACTIONS')=='true' and os.environ.get('RUNNER_OS')=='Linux' and os.environ.get('ADDP_ONLINE_HOST')=='1','disposable Hosted Linux required')
        if arguments.operation=='prepare': prepare(profile,tenant,engine)
        elif arguments.operation=='cleanup': cleanup(profile)
        else:
            require(not os.environ.get('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN')
                    and not os.environ.get('ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'), 'isolated SQL consumer required')
            client=SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'),SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'),30)
            directory = Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'))
            if profile.engine_type == 'starrocks':
                verify_starrocks_reads(client, tenant, engine, directory, revoked=True)
            else:
                verify_gaussdb_revoked_execution(client, tenant, engine, directory)
        return 0
    client=SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'),SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'),30)
    report=run(client,tenant,engine,float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','900')),profile)
    directory = Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'))
    (directory / (profile.engine_type+'-consumer-flow.json')).write_text(json.dumps(report,ensure_ascii=False,indent=2))
    if profile.engine_type == 'starrocks':
        verify_starrocks_reads(client, tenant, engine, directory, revoked=False)
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))
    return 0


if __name__=='__main__':
    try:
        raise SystemExit(main())
    except (RuntimeError,OSError,ValueError,KeyError) as error:
        print(str(error),file=sys.stderr)
        raise SystemExit(1)
