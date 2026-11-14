"""Avro scalar metadata, real pages, original downloads and bounded Transfer."""
from __future__ import annotations

import base64
import hashlib
import importlib
import json
from pathlib import Path
import subprocess
import time
from urllib.parse import urlencode

FIXTURE = importlib.import_module('business.scripts.online-avro-fixture')
REGISTRATION = importlib.import_module('scripts.test.online-engine-registration')
PERMISSIONS = importlib.import_module('scripts.test.manager-orc-online').PERMISSIONS


def require(value, message):
    if not value:
        raise ValueError(message)


def validate_page(preview, page, size, *, nested=False):
    table = preview.get('data', {})
    start = (page - 1) * size
    require(preview.get('preview_type') == 'table' and table.get('total') == FIXTURE.COUNT
            and table.get('has_more') is (start + size < FIXTURE.COUNT)
            and table.get('rows') == [(FIXTURE.nested_public_row if nested else FIXTURE.public_row)(i) for i in range(start, min(start + size, FIXTURE.COUNT))],
            'Avro table page lost values, precision or continuation')
    return table



def nested_field_facts():
    def field(name,kind,native,nullable=True,**extra):
        return dict(({'name':name} if name else {}),type=kind,native_type=native,nullable=nullable,**extra)
    detail=[field('uid','bigint','long'),field('amount','decimal','bytes',precision=38,scale=6),
            field('fixed_amount','decimal','example.NMoney',precision=38,scale=6),field('day','date','int'),
            *[field(n,'timestamp','long',temporal={'unit':u,'timezone':'UTC'}) for n,(u,_,_) in FIXTURE.TIMESTAMPS.items()],
            field('payload','bytes','bytes'),field('label','string','string')]
    record=lambda name:field(name,'json','example.Detail',fields=detail)
    return [field('id','bigint','long',False),record('detail'),
            field('items','array','array',element_type='json',element=record('')),
            field('grid','array','array',element_type='array',element=field('','array','array',element_type='bigint',element=field('','bigint','long'))),
            field('fixed_values','array','array',False,element_type='decimal',element=field('','decimal','example.NMoney',precision=38,scale=6))]

def declared_field_facts(fields):
    keys={'name','type','native_type','nullable','precision','scale','temporal','element_type'}
    result=[]
    for field in fields:
        value={k:v for k,v in field.items() if k in keys}
        if 'fields' in field:value['fields']=declared_field_facts(field['fields'])
        if 'element' in field:value['element']=declared_field_facts([field['element']])[0]
        result.append(value)
    return result


def task_body(locator, parent, name):
    return {'name': 'online-avro-' + name, 'task_type': 'sync', 'enabled': False,
            'auto_scan_metadata': False, 'batch_size': 1000, 'config': {
                'runtime': {'boundary': 'bounded'}, 'load': {'mode': 'snapshot'},
                'source': {'locator': locator, 'data_type': 'table', 'representation': 'encoded', 'format': 'avro'},
                'target': {'parent_locator': parent, 'name': name, 'data_type': 'table',
                           'representation': 'encoded', 'format': 'parquet', 'policy': {'apply_mode': 'replace'}},
                'batch_size': 1000}}


def run(repository, environment, owner, *, physical_runner=None, browser_runner=None):
    require(environment.get('ADDP_ONLINE_MANAGER_CASE') == 'native-table-avro', 'Avro requires its independent case')
    artifact = Path(environment['ADDP_ONLINE_ARTIFACT_DIR'])
    physical = physical_runner or (lambda action: FIXTURE.physical(action, repository, environment))
    client = owner.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
    initializer = owner.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], 30)
    tenant = owner.positive_int(environment['ADDP_ONLINE_TEST_TENANT_ID'], 'Avro tenant')
    report = {'schema_version': 'addp.manager-avro/v4', 'case': 'native-table-avro',
              'run_id': environment['ADDP_ONLINE_TEST_RUN_ID'], 'result': 'failed', 'cases': [], 'nested_cases': []}
    report['identity'] = owner.validate_user_identity(client, tenant, PERMISSIONS)
    grants = REGISTRATION.ExactFileMetadataGrants(initializer, client, tenant, scan_preparation=True)
    tasks = []
    seeded = False
    stage = 'seed'
    try:
        seeded = True
        seed = physical('seed')
        require(seed == {'sources': FIXTURE.sources(), 'nested_sources': FIXTURE.nested_sources(), 'count': FIXTURE.COUNT}, 'Avro physical seed differs')
        report['source'] = seed
        bucket = environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET']
        for storage, engine, prefix in [
            ('minio', owner.positive_int(environment['ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID'], 'MinIO'), bucket + '/'),
            ('nfs', owner.positive_int(environment['ADDP_ONLINE_MANAGER_NFS_ENGINE_ID'], 'NFS'), ''),
        ]:
            stage = 'metadata_scan'
            targets = [f'addp://engine/{engine}/path/{prefix}avro/{s["name"]}?type=' + ('object' if storage == 'minio' else 'file') for s in seed['sources']+seed['nested_sources']]
            owner.wait_for_meta_scan(initializer, engine, time.monotonic() + 180, scope={'targets': targets})
            for spec in seed['sources']+seed['nested_sources']:
                full_name = prefix + 'avro/' + spec['name']
                receipt = grants.grant(engine, full_name, storage, data_type='table', format_name='avro', layout='single')
                item = owner.find_fixture_item(client, engine, full_name, 'Avro ' + storage)
                detail = client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload
                attrs = detail.get('attributes', {})
                require(attrs.get('item') == {'format': 'avro', 'layout': 'single', 'data_type': 'table'}, 'Avro canonical identity differs')
                fields = attrs.get('type_info', {}).get('table', {}).get('fields', [])
                nested=spec.get('nested') is True
                if nested:
                    require(declared_field_facts(fields)==nested_field_facts(), 'Avro nested declared type tree differs')
                    require(attrs.get('format_info',{}).get('avro',{}).get('schema')==FIXTURE.NESTED_SCHEMA, 'Avro nested source schema changed')
                else:
                    require([(f['name'], f['type']) for f in fields] == [
                        ('id', 'bigint'), ('small', 'int'), ('flag', 'bool'), ('label', 'string'),
                        ('ratio', 'float'), ('value', 'double'), ('payload', 'bytes'),
                        ('day', 'date'), ('amount', 'decimal'), ('fixed_amount', 'decimal'),
                        ('stamp_ms','timestamp'), ('stamp_us','timestamp'), ('stamp_ns','timestamp')], 'Avro declared fields differ')
                    require([f.get('nullable', False) for f in fields] == [False, False, True, True, False, False, True, True, True, True, True, True, True], 'Avro schema nullability differs')
                    require(all(f.get('precision') == 38 and f.get('scale') == 6 for f in fields if f['type'] == 'decimal'), 'Avro decimal definition lost precision or scale')
                    for f in fields[-3:]:
                        require(f.get('native_type') == 'long' and f.get('temporal') == {'unit':FIXTURE.TIMESTAMPS[f['name']][0], 'timezone':'UTC'}, 'Avro UTC timestamp declaration differs')
                locator = owner.build_item_locator(engine, item)
                case = {'storage': storage, 'codec': spec['codec'], 'locator': locator,
                        'item_id': item['id'], 'spec': spec, 'metadata_grant': receipt, 'pages': []}
                (report['nested_cases'] if nested else report['cases']).append(case)
                case['nested']=nested
                (artifact / f'avro-{storage}-{"nested-" if nested else ""}{spec["codec"]}-meta.json').write_text(json.dumps(detail, ensure_ascii=False, indent=2) + '\n')
                stage = 'paged_preview'
                for page in (1, 2, 126):
                    preview = client.request('GET', '/api/v1/manager/preview?' + urlencode({'locator': locator, 'page': page, 'page_size': 20}), (200,)).payload
                    table = validate_page(preview, page, 20, nested=nested)
                    case['pages'].append({'page': page, 'rows': len(table['rows']), 'values_verified': True})
                stage = 'original_download'
                download_path = '/api/v1/manager/downloads/file?' + urlencode({'locator': locator})
                if storage == 'nfs':
                    denied = client.request('GET', download_path, (403,))
                    headers = {key.lower(): value for key, value in denied.headers.items()}
                    require(denied.status == 403 and headers.get('content-type', '').startswith('application/json')
                            and 'content-disposition' not in headers and isinstance(denied.payload, dict)
                            and set(denied.payload) == {'error'} and isinstance(denied.payload['error'], str)
                            and denied.payload['error'], 'Avro NFS original download must return a JSON denial without an attachment')
                    case['original_download_denied'] = {'http_status': 403, 'json_error': True, 'attachment': False}
                else:
                    raw = client.request('GET', download_path, (200,)).raw
                    require(len(raw) == spec['size_bytes'] and hashlib.sha256(raw).hexdigest() == spec['sha256'], 'Avro original differs')
                    (artifact / f'avro-source-{storage}-{spec["name"]}').write_bytes(raw)
                    case['original_download_verified'] = True
                stage = 'transfer_batch'
                target = f'{"rejected-nested-" if nested else ""}from-{storage}-{spec["codec"]}.parquet'
                body = task_body(locator, f'addp://engine/{environment["ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID"]}/path/{bucket}/avro-output?type=prefix', target)
                created = client.request('POST', '/api/v1/transfer/task-definitions', (201,), body).payload
                task_id = owner.positive_int(created.get('id'), 'Avro task')
                tasks.append(task_id)
                started = client.request('POST', f'/api/v1/transfer/task-definitions/{task_id}/start', (200, 201), {}).payload
                execution_id = started.get('execution_id')
                require(isinstance(execution_id, str) and execution_id, 'Avro execution identity absent')
                deadline = time.monotonic() + 180
                while True:
                    execution = client.request('GET', f'/api/v1/transfer/executions/{execution_id}', (200,)).payload
                    status = execution.get('status')
                    if nested and status == 'failed':
                        require(execution.get('records_read') == 0 and execution.get('records_written') == 0
                                and 'preview only' in execution.get('error_msg',''), 'Avro nested transfer failed for an unexpected reason or wrote rows')
                        break
                    if status == 'success':
                        require(not nested, 'Avro nested batch unexpectedly succeeded')
                        require(execution.get('records_read') == FIXTURE.COUNT and execution.get('records_written') == FIXTURE.COUNT, 'Avro batch counts differ')
                        break
                    require(status not in {'failed', 'cancelled', 'timeout'} and time.monotonic() < deadline, 'Avro Transfer did not succeed')
                    time.sleep(1)
                case['transfer'] = {'execution_id': execution_id, 'records_read': 0 if nested else FIXTURE.COUNT, 'records_written': 0 if nested else FIXTURE.COUNT,
                                    'status': 'failed' if nested else 'success', **({'nested_refusal_verified':True} if nested else {})}
                require(client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload == detail, 'Avro read modified metadata')
        stage = 'physical_verification'
        verified = physical('verify')
        require(verified.get('sources_unchanged') is True and len(verified.get('outputs', [])) == 6, 'Avro output/source proof incomplete')
        require(verified.get('nested_outputs_absent') is True and verified.get('nested_sources_verified') == 6, 'Avro nested physical proof incomplete')
        expected = {f'avro-output/from-{s}-{c}.parquet' for s in ('minio', 'nfs') for c in FIXTURE.CODECS}
        require({v.get('object') for v in verified['outputs']} == expected, 'Avro output identities differ')
        for output in verified['outputs']:
            raw = base64.b64decode(output.pop('content_base64'), validate=True)
            require(output.get('all_values_and_nulls_verified') is True and 0 < len(raw) < 2 * 1024 * 1024
                    and len(raw) == output['size_bytes'] and hashlib.sha256(raw).hexdigest() == output['sha256'], 'Avro physical output differs')
            filename = 'avro-result-' + Path(output['object']).name
            (artifact / filename).write_bytes(raw)
            output['archive_file'] = filename
        require(all(v.get('native_timestamp_ticks_verified') is True
                    and v.get('timestamp_units') == {name:unit for name,(unit,_,_) in FIXTURE.TIMESTAMPS.items()}
                    for v in verified['outputs']), 'Avro native timestamp proof incomplete')
        report['physical'] = verified
        stage = 'browser'
        context = {'run_id': report['run_id'], 'identity': report['identity'], 'cases': report['cases'], 'nested_cases':report['nested_cases']}
        if browser_runner:
            browser = browser_runner(context)
        else:
            browser_env = {k: v for k, v in environment.items() if k not in {'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN', 'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'}}
            subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/manager-avro.spec.js', '--config=playwright.online.config.js'],
                           cwd=Path(repository) / 'console/frontend', env=dict(browser_env, ADDP_ONLINE_MANAGER_AVRO_JSON=json.dumps(context)), check=True)
            browser = json.loads((artifact / 'manager-avro-browser.json').read_text())
        require(browser.get('schema_version') == 'addp.manager-avro-browser/v4' and browser.get('result') == 'passed'
                and browser.get('run_id') == report['run_id'] and len(browser.get('cases', [])) == 6 and len(browser.get('nested_cases', [])) == 6, 'Avro browser incomplete')
        for case, observed in zip(report['cases']+report['nested_cases'], browser['cases']+browser['nested_cases'], strict=True):
            require(observed.get('locator') == case['locator'] and observed.get('page_button_verified') is True
                    and observed.get('values_verified') is True, 'Avro browser lost page values')
            if case.get('nested'):
                require(observed.get('nested_values_verified') is True and observed.get('structured_dialogs_verified') is True, 'Avro nested dialogs not verified')
            if case['storage'] == 'nfs':
                require(observed.get('original_download_disabled') is True
                        and 'download_sha256' not in observed and 'download_size' not in observed,
                        'Avro NFS browser download must remain disabled')
            else:
                require(observed.get('download_sha256') == case['spec']['sha256']
                        and observed.get('download_size') == case['spec']['size_bytes'], 'Avro browser original differs')
            screenshot = artifact / f'avro-{case["storage"]}-{"nested-" if case.get("nested") else ""}{case["codec"]}-page2.png'
            require(screenshot.is_file() and screenshot.read_bytes().startswith(b'\x89PNG\r\n\x1a\n'), 'Avro screenshot missing')
            if case.get('nested'):
                detail_image = artifact / f'avro-{case["storage"]}-nested-{case["codec"]}-detail.png'
                require(detail_image.is_file() and detail_image.read_bytes().startswith(b'\x89PNG\r\n\x1a\n'), 'Avro nested dialog screenshot missing')
        report.update(browser=browser, result='passed')
        return report
    except BaseException as error:
        report.update(result='failed', stage=stage, error=type(error).__name__)
        raise
    finally:
        errors = []
        for task_id in reversed(tasks):
            try:
                client.request('DELETE', f'/api/v1/transfer/task-definitions/{task_id}', (200,))
                client.request('GET', f'/api/v1/transfer/task-definitions/{task_id}', (404,))
            except Exception:
                errors.append('task_cleanup_failed')
        report['tasks_deleted'] = len(tasks) if not errors else None
        try:
            report['metadata_cleanup'] = grants.cleanup()
        except Exception:
            errors.append('metadata_cleanup_failed')
        try:
            if seeded:
                report['physical_cleanup'] = physical('delete')
                require(report['physical_cleanup'] == {'objects_removed': True, 'files_removed': True, 'outputs_removed': True}, 'Avro physical cleanup incomplete')
        except Exception:
            errors.append('physical_cleanup_failed')
        if errors:
            report.update(result='failed', cleanup_errors=errors)
        (artifact / 'manager-avro.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        if errors:
            raise ValueError('; '.join(errors))
