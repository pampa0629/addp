#!/usr/bin/env python3
"""Authenticated Redis live catalog, unknown DataItems and native Console preview."""
from __future__ import annotations
import base64
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import urllib.parse

path = Path(__file__).with_name('security-transfer-protection-online.py')
spec = importlib.util.spec_from_file_location('redis_online_support', path)
SUPPORT = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = SUPPORT
spec.loader.exec_module(SUPPORT)
SuiteError = SUPPORT.SuiteError


def require(condition, message):
    if not condition:
        raise SuiteError(message)


def bytes_value(value):
    raw = value if isinstance(value, bytes) else value.encode('utf8')
    binary = b'\x00' in raw
    try:
        text = raw.decode('utf8')
    except UnicodeDecodeError:
        binary = True
    return {'encoding': 'base64' if binary else 'utf8',
            'value': base64.b64encode(raw).decode('ascii') if binary else text,
            'byte_length': len(raw)}


def sample_contract():
    scalar = {'string': 'ADDP Redis sample', 'counter': '9007199254740993',
              'ttl': 'expires', 'binary': b'\x00\xffADDP'}
    entries = {
        'hash': [{'field': bytes_value('name'), 'value': bytes_value('customer-0')},
                 {'field': bytes_value('order_id'), 'value': bytes_value('9007199254740993')}],
        'list': [{'index': n, 'value': bytes_value(v)} for n, v in enumerate(['one', 'two'])],
        'set': [{'value': bytes_value(v)} for v in ['north', 'south']],
        'zset': [{'value': bytes_value(v), 'score': str(n)} for n, v in enumerate(['first', 'second'], 1)],
        'stream': [{'id': '1-0', 'fields': [{'name': bytes_value('event'), 'value': bytes_value('created')}]}],
    }
    result = []
    for name in ['string', 'counter', 'hash', 'list', 'set', 'zset', 'stream', 'ttl', 'binary']:
        raw_key = ('addp:sample:' + name).encode() + (b'\x00\xff' if name == 'binary' else b'')
        sample = {'sample': name, 'key': 'k:' + base64.urlsafe_b64encode(raw_key).decode().rstrip('='),
                  'native_type': 'string' if name in scalar else name,
                  'length': 0 if name in scalar else len(entries[name])}
        if name in scalar:
            sample['value'] = bytes_value(scalar[name])
            sample['length'] = sample['value']['byte_length']
        else:
            sample['entries'] = entries[name]
        result.append(sample)
    return result


def validate_facts(facts, sample):
    require(isinstance(facts, dict), 'Redis live facts must be an object')
    require(facts.get('native_type') == sample['native_type'] and
            type(facts.get('length')) is int and facts['length'] == sample['length'],
            'Redis native type or length differs from the Business sample')
    ttl = facts.get('ttl_millis')
    require(type(ttl) is int and (0 < ttl <= 3600000 if sample['sample'] == 'ttl' else ttl == -1),
            'Redis TTL must preserve live milliseconds or persistent -1')


def validate_preview(payload, sample):
    preview = SUPPORT._object(payload, 'Manager preview')
    require(preview.get('preview_type') == 'key_value', 'Manager preview must use the native key renderer')
    data = SUPPORT._object(preview.get('data'), 'Manager native preview')
    require(data.get('mode') == 'key_value' and not data.get('columns') and not data.get('rows'),
            'Redis must use native key_value mode without synthetic table records')
    native = SUPPORT._object(data.get('key_value'), 'key value preview')
    validate_facts(native.get('facts'), sample)
    require(native.get('truncated') is False, 'Small Business Redis sample was truncated')
    if 'value' in sample:
        require(native.get('value') == sample['value'] and native.get('entries') == [],
                'Redis scalar bytes or big integer text changed')
    else:
        require(native.get('value') is None, 'Redis collection unexpectedly returned a scalar')
        actual = SUPPORT._array(native.get('entries'), 'Redis native entries')
        expected = sample['entries']
        if sample['sample'] in {'hash', 'set'}:
            actual = sorted(actual, key=lambda entry: json.dumps(entry, sort_keys=True))
            expected = sorted(expected, key=lambda entry: json.dumps(entry, sort_keys=True))
        require(actual == expected, 'Redis entries lost values, order, score strings or stream field pairs')
    return native


def locator(engine_id, item):
    name = urllib.parse.quote(item['full_name'], safe=':')
    query = urllib.parse.urlencode({'type': 'key', 'item_id': item['id']})
    return f'addp://engine/{engine_id}/path/{name}?{query}'


def validate_items(items, samples):
    require(isinstance(items, list) and len(items) == len(samples), 'Meta must retain exactly nine Redis key DataItems')
    by_name = {item.get('full_name'): item for item in items if isinstance(item, dict)}
    require(set(by_name) == {sample['key'] for sample in samples}, 'Meta canonical key identities differ from source bytes')
    for item in by_name.values():
        SUPPORT.positive_int(item.get('id'), 'key DataItem ID')
        SUPPORT.positive_int(item.get('node_id'), 'key node ID')
        require(item.get('item_type') == 'key' and bool(item.get('fingerprint')), 'Meta key identity or fingerprint is missing')
        require(item.get('attributes') == {'item': {'layout': 'single', 'data_type': 'unknown'}, 'schema_version': 1},
                'Meta must persist unknown identity without native values, type or TTL')
    return by_name


def run(client, tenant_id, engine_id, timeout):
    require(tenant_id > 1, 'Redis Online requires a nondefault tenant')
    context = SUPPORT._object(client.request('GET', '/api/v1/system/auth/context', (200,)).payload, 'identity')
    principal = SUPPORT._object(context.get('principal'), 'principal')
    tenant = SUPPORT._object(context.get('context'), 'tenant context')
    roles = SUPPORT._array(context.get('authorization', {}).get('role_assignments'), 'roles')
    require(principal.get('type') == 'user' and tenant.get('type') == 'tenant' and str(tenant.get('tenant_id')) == str(tenant_id),
            'Redis Online consumer identity mismatch')
    require(roles and not any(role.get('role_key') in SUPPORT.FORBIDDEN_ADMIN_ROLES for role in roles),
            'Redis Online must use a regular consumer user')
    selectors = SUPPORT._array(client.request('GET', '/api/v1/system/engine-catalog/engines', (200,)).payload, 'catalog selectors')
    selected = [entry for entry in selectors if entry.get('id') == engine_id]
    require(len(selected) == 1 and selected[0].get('engine_type') == 'redis', 'Registered Redis is missing from live catalog selectors')
    require('connection_info' not in selected[0], 'Catalog selector exposed engine credentials')
    catalog = selected[0].get('capabilities', {}).get('storage', {}).get('catalog', {})
    require(catalog.get('supported') is True and catalog.get('real_time') is True, 'Redis must advertise live catalog support')
    samples = sample_contract()
    root = SUPPORT._object(client.request('POST', f'/api/v1/system/engines/{engine_id}/catalog/children', (200,),
        {'path': {'version': 'catalog.path/v1', 'segments': []}}).payload, 'root catalog').get('nodes')
    require(isinstance(root, list) and len(root) == 1 and root[0].get('kind') == 'server', 'Redis must expose one structural server root')
    leaves = SUPPORT._object(client.request('POST', f'/api/v1/system/engines/{engine_id}/catalog/children', (200,),
        {'path': root[0]['path']}).payload, 'key catalog').get('nodes')
    require(isinstance(leaves, list) and len(leaves) == 9 and
            {leaf.get('name') for leaf in leaves} == {s['key'] for s in samples}, 'Live catalog must expose nine canonical keys')
    by_key = {leaf['name']: leaf for leaf in leaves}
    for sample in samples:
        leaf = by_key[sample['key']]
        require(leaf.get('kind') == 'key' and leaf.get('role') == 'leaf', 'Redis catalog key must be a leaf')
        facts = client.request('POST', f'/api/v1/system/engines/{engine_id}/catalog/facts', (200,), {'path': leaf['path']}).payload
        validate_facts(SUPPORT._object(facts, 'live facts').get('key_value'), sample)
    scan = SUPPORT.wait_for_scan(client, engine_id, time.monotonic() + timeout)
    items = validate_items(client.request('GET', f'/api/v1/meta/engines/{engine_id}/items', (200,)).payload, samples)
    for sample in samples:
        item = items[sample['key']]
        target = locator(engine_id, item)
        validate_preview(client.request('GET', '/api/v1/manager/preview?' + urllib.parse.urlencode({'locator': target}), (200,)).payload, sample)
        sample.update(item_id=item['id'], locator=target)
    # Native previews are bounded samples, not a repeatable table pagination contract.
    client.request('GET', '/api/v1/manager/preview?' + urllib.parse.urlencode({'locator': samples[0]['locator'], 'page': 2}), (400,))
    return {'engine_id': engine_id, 'tenant_id': tenant_id, 'principal_id': principal['id'],
            'scan_execution_id': scan, 'samples': samples, 'table_pagination_rejected': True}


def run_browser(report):
    artifacts = Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR')).resolve()
    report_file = artifacts / 'redis-console.json'
    report_file.unlink(missing_ok=True)
    environment = dict(os.environ, ADDP_ONLINE_REDIS_EXPECTATIONS=json.dumps(report), ADDP_ONLINE_REDIS_BROWSER_REPORT=str(report_file))
    result = subprocess.run(['npm', 'exec', '--', 'playwright', 'test', 'e2e/online/redis-consumer-flow.spec.js',
                             '--config=playwright.online.config.js'], cwd=Path(__file__).resolve().parents[2] / 'console/frontend', env=environment)
    require(result.returncode == 0 and report_file.is_file(), 'Real Console Redis browser acceptance failed or omitted evidence')
    evidence = json.loads(report_file.read_text())
    require(evidence == {'run_id': SUPPORT.required_environment('ADDP_ONLINE_TEST_RUN_ID'),
                        'engine_id': report['engine_id'], 'tenant_id': report['tenant_id'], 'principal_id': str(report['principal_id']),
                        'samples': [sample['sample'] for sample in report['samples']], 'meta_ui_scan': True},
            'Console browser evidence does not match the API identity and samples')
    return evidence


def main():
    require(os.environ.get('ADDP_ONLINE_TEST') == '1' and os.environ.get('ADDP_ONLINE_HOSTED') == '1', 'Redis suite requires the Hosted Online entry')
    tenant = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_TEST_TENANT_ID'), 'tenant')
    engine = SUPPORT.positive_int(SUPPORT.required_environment('ADDP_ONLINE_REDIS_ENGINE_ID'), 'engine')
    timeout = float(os.environ.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '900'))
    client = SUPPORT.GatewayClient(SUPPORT.required_environment('GATEWAY_URL'), SUPPORT.required_environment('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'), min(timeout, 30))
    report = run(client, tenant, engine, timeout)
    report['browser'] = run_browser(report)
    Path(SUPPORT.required_environment('ADDP_ONLINE_ARTIFACT_DIR'), 'redis-consumer-flow.json').write_text(json.dumps(report, ensure_ascii=False, indent=2))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))
    return 0


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (SuiteError, OSError, ValueError, KeyError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
