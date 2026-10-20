#!/usr/bin/env python3
"""Ordinary User FTP catalog, Meta scan and native Console preview/download."""
from importlib import import_module
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
from urllib.parse import quote, urlencode

API = import_module('scripts.utils.online-api')
SUPPORT = import_module('scripts.test.security-transfer-protection-online')
SEED = import_module('business.ftp.seed')
SuiteError = API.SuiteError
PERMISSIONS = {'system.engine_catalog.read', 'meta.catalog.read', 'meta.scan_task.execute',
               'meta.scan_task.read', 'manager.data_item.read', 'manager.content.read'}


def require(ok, message):
    if not ok:
        raise SuiteError(message)


def validate_preview(payload):
    data = payload.get('data', {})
    require(payload.get('preview_type') == 'table' and set(data.get('columns', [])) == {'id', 'name', 'amount'},
            'FTP shared table preview must expose the three native columns')
    rows = data.get('rows', [])
    require(isinstance(rows, list) and len(rows) == 3 and data.get('total') == 3,
            'FTP shared preview must contain all three example rows')
    require(all(str(row.get('id')) in {'1', '2', '3'} and not isinstance(row.get('id'), bool) for row in rows), 'FTP sample IDs must remain exact integers')
    try:
        actual = sorted([{'id': int(row['id']), 'name': row['name'], 'amount': float(row['amount'])} for row in rows], key=lambda row: row['id'])
    except (KeyError, TypeError, ValueError):
        raise SuiteError('FTP sample values are malformed') from None
    require(actual == SEED.ROWS, 'FTP preview changed native IDs, Chinese text or amounts')


def validate_item(item, sample):
    require(item.get('item_type') == 'file' and item.get('full_name') == sample['full_name'] and item.get('fingerprint'),
            'FTP Meta item must preserve its exact account-relative file identity')
    attributes = item.get('attributes', {})
    require(attributes.get('schema_version') == 1 and attributes.get('item', {}).get('data_type') == 'table',
            'FTP tabular file must reuse the existing table data type')
    fields = attributes.get('type_info', {}).get('table', {}).get('fields', [])
    require({field.get('name') for field in fields} == {'id', 'name', 'amount'},
            'FTP Meta scan omitted the native file schema')


def file_locator(engine_id, item):
    path = '/'.join(quote(part, safe='') for part in item['full_name'].split('/'))
    return f'addp://engine/{engine_id}/path/{path}?' + urlencode({'type': 'file', 'item_id': item['id']})


def run(client, tenant_id, engine_id, samples, timeout):
    require(tenant_id > 1, 'FTP Online requires a nondefault disposable Tenant')
    API.validate_user_identity(client, tenant_id, PERMISSIONS)
    context = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    selectors = client.request('GET', '/api/v1/system/engine-catalog/engines', (200,)).payload
    selected = [entry for entry in selectors if entry.get('id') == engine_id]
    require(len(selected) == 1 and selected[0].get('engine_type') == 'ftp' and 'connection_info' not in selected[0],
            'FTP must be in the ordinary User catalog without credentials')
    capabilities = selected[0].get('capabilities', {})
    store = capabilities.get('storage', {}).get('store', {})
    require(capabilities.get('engine_family') == 'file' and store.get('stream_read') is True and
            store.get('range_read') is True and not store.get('stream_write') and not store.get('delete'),
            'FTP must declare only the admitted read capabilities')
    endpoint = f'/api/v1/system/engines/{engine_id}/catalog/children'
    roots = client.request('POST', endpoint, (200,), {'path': {'version': 'catalog.path/v1', 'segments': []}}).payload['nodes']
    require(len(roots) == 1 and roots[0]['kind'] == 'root', 'FTP must expose one explicit account root')
    directories = client.request('POST', endpoint, (200,), {'path': roots[0]['path']}).payload['nodes']
    branches = [entry for entry in directories if entry['name'] == 'examples']
    require(len(branches) == 1 and branches[0]['kind'] == 'directory', 'FTP account root lost the examples directory')
    files = client.request('POST', endpoint, (200,), {'path': branches[0]['path']}).payload['nodes']
    for sample in samples:
        matches = [entry for entry in files if entry['name'] == sample['full_name'].split('/')[-1]]
        require(len(matches) == 1 and matches[0]['kind'] == 'file', 'FTP live catalog normalized or omitted a sample name')
        facts = client.request('POST', f'/api/v1/system/engines/{engine_id}/catalog/facts', (200,), {'path': matches[0]['path']}).payload
        storage = facts.get('storage', {})
        require(storage.get('path') == sample['full_name'] and storage.get('size_bytes') == sample['size'] and not storage.get('etag'),
                'FTP facts must preserve native path/size without inventing an ETag')
    execution_id = SUPPORT.wait_for_scan(client, engine_id, time.monotonic() + timeout)
    items = client.request('GET', f'/api/v1/meta/engines/{engine_id}/items', (200,)).payload
    result = []
    for sample in samples:
        matches = [item for item in items if item.get('full_name') == sample['full_name']]
        require(len(matches) == 1, 'FTP scan must create one exact DataItem for each sample')
        item = matches[0]
        validate_item(item, sample)
        locator = file_locator(engine_id, item)
        validate_preview(client.request('GET', '/api/v1/manager/preview?' + urlencode({'locator': locator}), (200,)).payload)
        binary = client.request('GET', '/api/v1/manager/downloads/file?' + urlencode({'locator': locator}), (200,), raw_response=True).payload
        require(len(binary) == sample['size'] and hashlib.sha256(binary).hexdigest() == sample['sha256'],
                'FTP API download differs from the physical seed file')
        result.append({**sample, 'item_id': item['id'], 'locator': locator})
    require({sample['format'] for sample in result} == {'csv', 'json', 'parquet'} and len(result) == 3,
            'FTP acceptance requires all three formats')
    return {'run_id': SUPPORT.required_environment('ADDP_ONLINE_TEST_RUN_ID'), 'engine_id': engine_id,
            'tenant_id': tenant_id, 'principal_id': str(context['principal']['id']),
            'scan_execution_id': execution_id, 'samples': result}


def validate_browser_report(actual, expected):
    proof = {key: expected[key] for key in ('run_id', 'engine_id', 'tenant_id', 'principal_id')}
    proof.update(meta_ui_scan=True, previews=3, downloads=[{key: sample[key] for key in ('format', 'item_id', 'size', 'sha256')} for sample in expected['samples']])
    require(actual == proof, 'FTP browser identity, scan, preview or physical download proof differs')


def run_browser(report):
    output = Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR')) / 'ftp-console.json'
    output.unlink(missing_ok=True)
    environment = dict(os.environ, ADDP_ONLINE_FTP_EXPECTATIONS=json.dumps(report), ADDP_ONLINE_FTP_BROWSER_REPORT=str(output))
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/ftp-consumer-flow.spec.js', '--config=playwright.online.config.js'],
                            cwd=Path(__file__).resolve().parents[2] / 'console/frontend', env=environment)
    require(result.returncode == 0 and output.is_file(), 'Real Console FTP acceptance failed or omitted evidence')
    actual = json.loads(output.read_text())
    validate_browser_report(actual, report)
    return actual


def main():
    require(os.environ.get('ADDP_ONLINE_TEST') == '1' and os.environ.get('ADDP_ONLINE_HOSTED') == '1', 'FTP suite requires the Hosted Online entry')
    tenant = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_TEST_TENANT_ID'), 'tenant')
    engine = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_FTP_ENGINE_ID'), 'engine')
    timeout = float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '900'))
    samples = json.loads((Path(SUPPORT.required_environment('ADDP_FTP_ADMISSION_DIR')) / 'ftp-samples.json').read_text())
    client = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), min(timeout, 30))
    report = run(client, tenant, engine, samples, timeout)
    report['browser'] = run_browser(report)
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'), 'ftp-consumer-flow.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except (SuiteError, SUPPORT.SuiteError, OSError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
