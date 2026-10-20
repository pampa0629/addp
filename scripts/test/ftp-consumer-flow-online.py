#!/usr/bin/env python3
"""Ordinary User FTP catalog, scan, native previews and authorized model GLB."""
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
MODEL = import_module('scripts.test.manager-internal-artifact-lineage-online')
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


def prepare_models(items, engine_id, samples):
    expected = {'ifc': ('models/ifc/model.ifc', 'addp_online_model_fixture.ifc'),
                '3ds': ('models/3ds/model.3ds', 'addp_online_model_fixture.3ds'),
                'png': ('models/3ds/texture.png', 'addp_online_exchange_texture.png')}
    require(isinstance(samples, list) and len(samples) == 3 and
            {sample.get('format') for sample in samples} == set(expected),
            'FTP model manifest requires exactly IFC, 3DS and its PNG')
    fixture = Path(__file__).resolve().parents[2] / 'business/fixtures/manager'
    by_format = {}
    for sample in samples:
        relative, source = expected[sample['format']]
        raw = (fixture / source).read_bytes()
        require(sample.get('full_name') == relative and type(sample.get('size')) is int and
                sample['size'] == len(raw) and sample.get('sha256') == hashlib.sha256(raw).hexdigest(),
                'FTP model manifest differs from the fixed physical fixture')
        by_format[sample['format']] = sample
    result = []
    for kind in ('ifc', '3ds'):
        sample = by_format[kind]
        matches = [item for item in items if item.get('full_name') == sample['full_name']]
        require(len(matches) == 1, 'FTP model scan must retain one exact primary DataItem')
        item = matches[0]
        attributes = item.get('attributes', {})
        facts = attributes.get('item', {})
        native = attributes.get('format_info', {}).get(kind, {})
        require(type(item.get('id')) is int and item['id'] > 0 and item.get('fingerprint') and
                item.get('item_type') == 'file' and attributes.get('schema_version') == 1 and
                all(facts.get(key) == value for key, value in
                    {'data_type': 'model_3d', 'layout': 'single', 'format': kind}.items()) and
                attributes.get('storage', {}).get('physical_path') == sample['full_name'] and
                native.get('scan_complete') is True,
                'FTP model identity, single layout or complete native facts are missing')
        refs = facts.get('refs')
        # IFC's fixed geometry profile has no external inputs; Meta may project
        # its sole primary from the committed single-file identity.
        if kind == 'ifc' and refs is None:
            refs = [{'path': sample['full_name'], 'role': 'model', 'required': True, 'primary': True}]
        expected_refs = [{'path': sample['full_name'], 'role': 'model', 'required': True, 'primary': True}]
        if kind == '3ds':
            require(native.get('texture_refs') == ['texture.png'],
                    'FTP 3DS must retain its exact texture declaration')
            expected_refs.append({'path': by_format['png']['full_name'], 'role': 'texture', 'required': True, 'primary': False})
        require(isinstance(refs, list) and len(refs) == len(expected_refs) and
                sorted([{key: ref.get(key, False) for key in ('path', 'role', 'required', 'primary')}
                        for ref in refs], key=lambda ref: ref['path']) ==
                sorted(expected_refs, key=lambda ref: ref['path']),
                'FTP model composition must contain only its primary and confirmed dependencies')
        result.append({**sample, 'item_id': item['id'], 'fingerprint': item['fingerprint'],
                       'locator': file_locator(engine_id, item), 'composition': expected_refs})
    return result


def run(client, tenant_id, engine_id, samples, model_samples, timeout):
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
            'scan_execution_id': execution_id, 'samples': result,
            'models': prepare_models(items, engine_id, model_samples)}


def validate_browser_report(actual, expected, artifact_dir):
    proof = {key: expected[key] for key in ('run_id', 'engine_id', 'tenant_id', 'principal_id')}
    proof.update(meta_ui_scan=True, previews=3, downloads=[{key: sample[key] for key in ('format', 'item_id', 'size', 'sha256')} for sample in expected['samples']])
    require({key: value for key, value in actual.items() if key != 'models'} == proof,
            'FTP browser identity, scan, preview or physical download proof differs')
    models = actual.get('models')
    require(isinstance(models, list) and len(models) == 2,
            'FTP browser must generate and reopen both model formats')
    for observed, source in zip(models, expected['models']):
        require(set(observed) == {'format', 'item_id', 'task_id', 'execution_id', 'result_id',
                                 'size', 'sha256', 'source_sha256', 'rendered', 'reopened',
                                 'cached_without_execution'} and
                observed.get('format') == source['format'] and observed.get('item_id') == source['item_id'] and
                all(type(observed.get(key)) is int and observed[key] > 0 for key in ('task_id', 'result_id', 'size')) and
                isinstance(observed.get('execution_id'), str) and bool(observed['execution_id']) and
                observed.get('source_sha256') == source['sha256'] and
                all(observed.get(key) is True for key in ('rendered', 'reopened', 'cached_without_execution')),
                'FTP browser model identity, source integrity, rendering or cache proof differs')
        raw = (artifact_dir / f"ftp-{source['format']}.glb").read_bytes()
        require(len(raw) == observed['size'] and hashlib.sha256(raw).hexdigest() == observed.get('sha256'),
                'FTP model report differs from actual downloaded GLB')
        try:
            MODEL.validate_model_glb(raw, source['format'])
        except MODEL.SuiteError as error:
            raise SuiteError(f"FTP {source['format']} GLB physical verification failed: {error}") from error


def run_browser(report):
    output = Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR')) / 'ftp-console.json'
    output.unlink(missing_ok=True)
    environment = dict(os.environ, ADDP_ONLINE_FTP_EXPECTATIONS=json.dumps(report), ADDP_ONLINE_FTP_BROWSER_REPORT=str(output))
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/ftp-consumer-flow.spec.js', '--config=playwright.online.config.js'],
                            cwd=Path(__file__).resolve().parents[2] / 'console/frontend', env=environment)
    require(result.returncode == 0 and output.is_file(), 'Real Console FTP acceptance failed or omitted evidence')
    actual = json.loads(output.read_text())
    validate_browser_report(actual, report, output.parent)
    return actual


def main():
    require(os.environ.get('ADDP_ONLINE_TEST') == '1' and os.environ.get('ADDP_ONLINE_HOSTED') == '1', 'FTP suite requires the Hosted Online entry')
    tenant = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_TEST_TENANT_ID'), 'tenant')
    engine = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_FTP_ENGINE_ID'), 'engine')
    timeout = float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '900'))
    samples = json.loads((Path(SUPPORT.required_environment('ADDP_FTP_ADMISSION_DIR')) / 'ftp-samples.json').read_text())
    model_samples = json.loads((Path(SUPPORT.required_environment('ADDP_FTP_ADMISSION_DIR')) / 'ftp-model-samples.json').read_text())
    client = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), min(timeout, 30))
    report = run(client, tenant, engine, samples, model_samples, timeout)
    report['browser'] = run_browser(report)
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'), 'ftp-consumer-flow.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except (SuiteError, SUPPORT.SuiteError, OSError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
