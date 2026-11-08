"""Ordinary-user GB18030 read settings, pages and byte-exact downloads."""
from __future__ import annotations
import importlib
import json
from pathlib import Path
import subprocess
import time
from urllib.parse import urlencode

FIXTURE = importlib.import_module('business.scripts.online-delimited-fixture')
REGISTRATION = importlib.import_module('scripts.test.online-engine-registration')


def require(value, message):
    if not value:
        raise ValueError(message)


def validate_metadata(detail, spec):
    attrs = detail.get('attributes', {})
    require(attrs.get('item') == {'format': spec['format'], 'data_type': 'table', 'layout': 'single'}, 'delimited identity differs')
    table = attrs.get('type_info', {}).get('table', {})
    require('row_count' not in detail and attrs.get('schema_version') == 1
            and not set(attrs) - {'schema_version', 'item', 'type_info'}
            and 'row_count' not in table and 'native' not in table
            and [(f['name'], f['type']) for f in table.get('fields', [])] == list(zip(FIXTURE.COLUMNS, ['int', 'string', 'string'])),
            'delimited public metadata leaked content or lost structure')


def validate_content_metadata(preview, spec, fingerprint):
    table_data = preview.get('data', {})
    meta = table_data.get('item_meta', {})
    entries = meta.get('attributes', [])
    require(isinstance(entries, list) and all(isinstance(row, dict) and set(row) == {'key', 'value'}
            and isinstance(row['key'], str) and row['key'] for row in entries), 'delimited guarded metadata shape differs')
    attrs = {row['key']: row['value'] for row in entries}
    require(len(attrs) == len(entries), 'delimited guarded metadata keys duplicated')
    require(preview.get('preview_type') == 'table' and meta.get('fingerprint') == fingerprint
            and attrs.get('item') == {'format': spec['format'], 'data_type': 'table', 'layout': 'single'},
            'delimited guarded metadata identity differs')
    table = attrs.get('type_info', {}).get('table', {})
    require(meta.get('row_count') == 25 and table.get('row_count') == 25
            and table.get('native', {}).get('has_header') is True
            and [(f['name'], f['type']) for f in table.get('fields', [])] == list(zip(FIXTURE.COLUMNS, ['int', 'string', 'string']))
            and attrs.get('format_info', {}).get(spec['format'], {}).get('encoding') == 'gb18030'
            and attrs.get('storage', {}).get('content_hash') == spec['sha256']
            and not attrs.get('access_index', {}).get('table'), 'delimited encoding, fields, source or index differs')


def validate_page(preview, page):
    table = preview.get('data', {})
    start = (page - 1) * 20
    require(preview.get('preview_type') == 'table' and table.get('total') == 25
            and table.get('rows') == [FIXTURE.public_row(i) for i in range(start, min(start + 20, 25))],
            'delimited page lost four-byte characters, rows or quoted newlines')
    return len(table['rows'])


def run(repository, environment, owner, *, physical_runner=None, browser_runner=None):
    require(environment.get('ADDP_ONLINE_MANAGER_CASE') == 'delimited-read-settings', 'delimited requires its independent case')
    artifact = Path(environment['ADDP_ONLINE_ARTIFACT_DIR'])
    physical = physical_runner or (lambda action: FIXTURE.physical(action, repository, environment))
    client = owner.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30)
    initializer = owner.GatewayClient(environment['GATEWAY_URL'], environment['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], 30)
    report = {'schema_version': 'addp.manager-delimited/v1', 'case': 'delimited-read-settings',
              'run_id': environment['ADDP_ONLINE_TEST_RUN_ID'], 'result': 'failed', 'cases': []}
    tenant = owner.positive_int(environment['ADDP_ONLINE_TEST_TENANT_ID'], 'Tenant')
    report['identity'] = owner.validate_user_identity(client, tenant)
    require(len(report['identity']['permissions_verified']) == 10, 'delimited consumer permissions differ')
    grants = REGISTRATION.ExactFileMetadataGrants(initializer, client, tenant, scan_preparation=True)
    seeded = False
    stage = 'seed'
    try:
        seeded = True
        seed = physical('seed')
        require(seed == {'sources': FIXTURE.sources(), 'count': 25}, 'delimited seed differs')
        report['source'] = seed
        bucket = environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET']
        for storage, engine, prefix in [
            ('minio', owner.positive_int(environment['ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID'], 'MinIO'), bucket + '/'),
            ('nfs', owner.positive_int(environment['ADDP_ONLINE_MANAGER_NFS_ENGINE_ID'], 'NFS'), ''),
        ]:
            stage = 'basic_scan'
            targets = [f'addp://engine/{engine}/path/{prefix}{FIXTURE.DIRECTORY}/{s["name"]}?type=' + ('object' if storage == 'minio' else 'file') for s in seed['sources']]
            execution = owner.wait_for_meta_scan(initializer, engine, time.monotonic() + 180, depth='basic', scope={'targets': targets})
            require(isinstance(execution, str) and execution, 'delimited initialization execution absent')
            report.setdefault('initial_scans', []).append({'storage': storage, 'execution_id': execution})
            for spec in seed['sources']:
                full_name = prefix + FIXTURE.DIRECTORY + '/' + spec['name']
                receipt = grants.grant(engine, full_name, storage, data_type='table', format_name=spec['format'], layout='single')
                item = owner.find_fixture_item(client, engine, full_name, 'GB18030 ' + storage)
                report['cases'].append({'storage': storage, 'spec': spec, 'item_id': item['id'],
                    'fingerprint': item['fingerprint'], 'locator': owner.build_item_locator(engine, item), 'metadata_grant': receipt})
        stage = 'browser'
        if browser_runner:
            browser = browser_runner(report)
        else:
            browser_env = {k: v for k, v in environment.items() if k not in {'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN', 'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN'}}
            subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/manager-delimited.spec.js', '--config=playwright.online.config.js'],
                cwd=Path(repository) / 'console/frontend', env=dict(browser_env, ADDP_ONLINE_MANAGER_DELIMITED_JSON=json.dumps(report)), check=True)
            browser = json.loads((artifact / 'manager-delimited-browser.json').read_text())
        require(browser.get('schema_version') == 'addp.manager-delimited-browser/v1' and browser.get('result') == 'passed'
                and browser.get('run_id') == report['run_id'] and len(browser.get('cases', [])) == 4, 'delimited browser incomplete')
        flags = ('encoding_request_verified', 'header_mode_verified', 'ordinary_refresh_verified', 'reopened',
                 'header_restored', 'original_button_download_verified', 'metadata_projection_verified', 'content_metadata_verified')
        for case, observed in zip(report['cases'], browser['cases'], strict=True):
            spec = case['spec']
            require(observed.get('locator') == case['locator'] and all(observed.get(f) is True for f in flags)
                    and observed.get('download_sha256') == spec['sha256'] and observed.get('download_size') == spec['size_bytes'], 'delimited browser proof differs')
            for mode in ('header', 'headerless'):
                picture = artifact / f'delimited-{case["storage"]}-{spec["format"]}-{mode}.png'
                require(picture.is_file() and picture.read_bytes().startswith(b'\x89PNG\r\n\x1a\n'), 'delimited screenshot absent')
            stage = 'metadata'
            detail = client.request('GET', f'/api/v1/meta/items/{case["item_id"]}', (200,)).payload
            validate_metadata(detail, spec)
            (artifact / f'delimited-{case["storage"]}-{spec["format"]}-meta.json').write_text(json.dumps(detail, ensure_ascii=False, indent=2) + '\n')
            case['metadata_verified'] = True
            stage = 'api_pages'
            case['pages'] = []
            for page in (1, 2, 3):
                preview = client.request('GET', '/api/v1/manager/preview?' + urlencode({'locator': case['locator'], 'page': page, 'page_size': 20}), (200,)).payload
                validate_content_metadata(preview, spec, case['fingerprint'])
                case['pages'].append({'page': page, 'rows': validate_page(preview, page), 'values_verified': True})
                if page == 1:
                    (artifact / f'delimited-{case["storage"]}-{spec["format"]}-content.json').write_text(json.dumps(preview, ensure_ascii=False, indent=2) + '\n')
            case['content_metadata_verified'] = True
            stage = 'api_download'
            raw = client.request('GET', '/api/v1/manager/downloads/file?' + urlencode({'locator': case['locator']}), (200,)).raw
            require(raw == FIXTURE.source_bytes(spec['format']), 'delimited original bytes differ')
            (artifact / f'delimited-{case["storage"]}-{spec["name"]}').write_bytes(raw)
            case['original_download_verified'] = True
            require(client.request('GET', f'/api/v1/meta/items/{case["item_id"]}', (200,)).payload == detail, 'read-only checks modified metadata')
        stage = 'physical_verify'
        report['physical'] = physical('verify')
        require(report['physical'] == {'sources_unchanged': True}, 'delimited physical source changed')
        report.update(browser=browser, result='passed')
        return report
    except BaseException as error:
        report.update(result='failed', stage=stage, error=type(error).__name__)
        raise
    finally:
        errors = []
        try:
            report['metadata_cleanup'] = grants.cleanup()
        except Exception:
            errors.append('metadata_cleanup_failed')
        try:
            if seeded:
                report['physical_cleanup'] = physical('delete')
                require(report['physical_cleanup'] == {'objects_removed': True, 'files_removed': True}, 'delimited physical cleanup incomplete')
        except Exception:
            errors.append('physical_cleanup_failed')
        if errors:
            report.update(result='failed', cleanup_errors=errors)
        (artifact / 'manager-delimited.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')
        if errors:
            raise ValueError('; '.join(errors))
