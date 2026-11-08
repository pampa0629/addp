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


def validate_page(preview, page, size):
    table = preview.get('data', {})
    start = (page - 1) * size
    require(preview.get('preview_type') == 'table' and table.get('total') == FIXTURE.COUNT
            and table.get('has_more') is (start + size < FIXTURE.COUNT)
            and table.get('rows') == [FIXTURE.public_row(i) for i in range(start, min(start + size, FIXTURE.COUNT))],
            'Avro table page lost values, precision or continuation')
    return table


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
    report = {'schema_version': 'addp.manager-avro/v1', 'case': 'native-table-avro',
              'run_id': environment['ADDP_ONLINE_TEST_RUN_ID'], 'result': 'failed', 'cases': []}
    report['identity'] = owner.validate_user_identity(client, tenant, PERMISSIONS)
    grants = REGISTRATION.ExactFileMetadataGrants(initializer, client, tenant, scan_preparation=True)
    tasks = []
    seeded = False
    stage = 'seed'
    try:
        seeded = True
        seed = physical('seed')
        require(seed == {'sources': FIXTURE.sources(), 'count': FIXTURE.COUNT}, 'Avro physical seed differs')
        report['source'] = seed
        bucket = environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET']
        for storage, engine, prefix in [
            ('minio', owner.positive_int(environment['ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID'], 'MinIO'), bucket + '/'),
            ('nfs', owner.positive_int(environment['ADDP_ONLINE_MANAGER_NFS_ENGINE_ID'], 'NFS'), ''),
        ]:
            stage = 'metadata_scan'
            targets = [f'addp://engine/{engine}/path/{prefix}avro/{s["name"]}?type=' + ('object' if storage == 'minio' else 'file') for s in seed['sources']]
            owner.wait_for_meta_scan(initializer, engine, time.monotonic() + 180, scope={'targets': targets})
            for spec in seed['sources']:
                full_name = prefix + 'avro/' + spec['name']
                receipt = grants.grant(engine, full_name, storage, data_type='table', format_name='avro', layout='single')
                item = owner.find_fixture_item(client, engine, full_name, 'Avro ' + storage)
                detail = client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload
                attrs = detail.get('attributes', {})
                require(attrs.get('item') == {'format': 'avro', 'layout': 'single', 'data_type': 'table'}, 'Avro canonical identity differs')
                fields = attrs.get('type_info', {}).get('table', {}).get('fields', [])
                require([(f['name'], f['type']) for f in fields] == [
                    ('id', 'bigint'), ('small', 'int'), ('flag', 'bool'), ('label', 'string'),
                    ('ratio', 'float'), ('value', 'double'), ('payload', 'bytes')], 'Avro declared fields differ')
                require([f.get('nullable', False) for f in fields] == [False, False, True, True, False, False, True], 'Avro schema nullability differs')
                locator = owner.build_item_locator(engine, item)
                case = {'storage': storage, 'codec': spec['codec'], 'locator': locator,
                        'item_id': item['id'], 'spec': spec, 'metadata_grant': receipt, 'pages': []}
                report['cases'].append(case)
                (artifact / f'avro-{storage}-{spec["codec"]}-meta.json').write_text(json.dumps(detail, ensure_ascii=False, indent=2) + '\n')
                stage = 'paged_preview'
                for page in (1, 2, 126):
                    preview = client.request('GET', '/api/v1/manager/preview?' + urlencode({'locator': locator, 'page': page, 'page_size': 20}), (200,)).payload
                    table = validate_page(preview, page, 20)
                    case['pages'].append({'page': page, 'rows': len(table['rows']), 'values_verified': True})
                stage = 'original_download'
                raw = client.request('GET', '/api/v1/manager/downloads/file?' + urlencode({'locator': locator}), (200,)).raw
                require(len(raw) == spec['size_bytes'] and hashlib.sha256(raw).hexdigest() == spec['sha256'], 'Avro original differs')
                (artifact / f'avro-source-{storage}-{spec["name"]}').write_bytes(raw)
                case['original_download_verified'] = True
                stage = 'transfer_batch'
                target = f'from-{storage}-{spec["codec"]}.parquet'
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
                    if status == 'success':
                        require(execution.get('records_read') == FIXTURE.COUNT and execution.get('records_written') == FIXTURE.COUNT, 'Avro batch counts differ')
                        break
                    require(status not in {'failed', 'cancelled', 'timeout'} and time.monotonic() < deadline, 'Avro Transfer did not succeed')
                    time.sleep(1)
                case['transfer'] = {'execution_id': execution_id, 'records_read': FIXTURE.COUNT, 'records_written': FIXTURE.COUNT, 'status': 'success'}
                require(client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload == detail, 'Avro read modified metadata')
        stage = 'physical_verification'
        verified = physical('verify')
        require(verified.get('sources_unchanged') is True and len(verified.get('outputs', [])) == 6, 'Avro output/source proof incomplete')
        expected = {f'avro-output/from-{s}-{c}.parquet' for s in ('minio', 'nfs') for c in FIXTURE.CODECS}
        require({v.get('object') for v in verified['outputs']} == expected, 'Avro output identities differ')
        for output in verified['outputs']:
            raw = base64.b64decode(output.pop('content_base64'), validate=True)
            require(output.get('all_values_and_nulls_verified') is True and 0 < len(raw) < 2 * 1024 * 1024
                    and len(raw) == output['size_bytes'] and hashlib.sha256(raw).hexdigest() == output['sha256'], 'Avro physical output differs')
            filename = 'avro-result-' + Path(output['object']).name
            (artifact / filename).write_bytes(raw)
            output['archive_file'] = filename
        report['physical'] = verified
        stage = 'browser'
        context = {'run_id': report['run_id'], 'identity': report['identity'], 'cases': report['cases']}
        if browser_runner:
            browser = browser_runner(context)
        else:
            browser_env = {k: v for k, v in environment.items() if k not in {'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN', 'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'}}
            subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/manager-avro.spec.js', '--config=playwright.online.config.js'],
                           cwd=Path(repository) / 'console/frontend', env=dict(browser_env, ADDP_ONLINE_MANAGER_AVRO_JSON=json.dumps(context)), check=True)
            browser = json.loads((artifact / 'manager-avro-browser.json').read_text())
        require(browser.get('schema_version') == 'addp.manager-avro-browser/v1' and browser.get('result') == 'passed'
                and browser.get('run_id') == report['run_id'] and len(browser.get('cases', [])) == 6, 'Avro browser incomplete')
        for case, observed in zip(report['cases'], browser['cases'], strict=True):
            require(observed.get('locator') == case['locator'] and observed.get('page_button_verified') is True
                    and observed.get('values_verified') is True and observed.get('download_sha256') == case['spec']['sha256']
                    and observed.get('download_size') == case['spec']['size_bytes'], 'Avro browser lost values or original bytes')
            screenshot = artifact / f'avro-{case["storage"]}-{case["codec"]}-page2.png'
            require(screenshot.is_file() and screenshot.read_bytes().startswith(b'\x89PNG\r\n\x1a\n'), 'Avro screenshot missing')
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
