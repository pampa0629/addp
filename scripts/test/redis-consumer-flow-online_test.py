import copy
import importlib.util
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

spec = importlib.util.spec_from_file_location('redis_online_target', Path(__file__).with_name('redis-consumer-flow-online.py'))
MODULE = importlib.util.module_from_spec(spec)
spec.loader.exec_module(MODULE)


def preview(sample):
    native = {'facts': {'native_type': sample['native_type'], 'length': sample['length'],
                       'ttl_millis': 123456 if sample['sample'] == 'ttl' else -1}, 'entries': [], 'truncated': False}
    native.update({key: copy.deepcopy(sample[key]) for key in ('value', 'entries') if key in sample})
    return {'preview_type': 'key_value', 'data': {'mode': 'key_value', 'columns': [], 'rows': [], 'key_value': native}}


class FakeClient:
    def __init__(self):
        self.calls = []
        self.samples = MODULE.sample_contract()
        self.items = [{'id': n, 'node_id': 10, 'item_type': 'key', 'full_name': sample['key'], 'fingerprint': 'fp-' + str(n),
                       'attributes': {'schema_version': 1, 'item': {'layout': 'single', 'data_type': 'unknown'}}}
                      for n, sample in enumerate(self.samples, 1)]
        self.context = {'principal': {'id': 8, 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                        'authorization': {'role_assignments': [{'role_key': 'online.redis_consumer_flow'}]}}
        self.selectors = [{'id': 17, 'engine_type': 'redis', 'capabilities': {'storage': {'catalog': {'supported': True, 'real_time': True}}}}]

    def request(self, method, path, statuses, body=None):
        self.calls.append((method, path, statuses, body))
        url = urlsplit(path)
        if url.path.endswith('/auth/context'): payload = self.context
        elif url.path.endswith('/engine-catalog/engines'): payload = self.selectors
        elif url.path.endswith('/catalog/children'):
            if not body['path']['segments']:
                payload = {'nodes': [{'kind': 'server', 'path': {'version': 'catalog.path/v1', 'segments': [{'term': 'server', 'kind': 'server', 'name': ''}]}}]}
            else:
                payload = {'nodes': [{'name': s['key'], 'kind': 'key', 'role': 'leaf', 'path': {'segments': [{'name': s['key']}]}} for s in self.samples]}
        elif url.path.endswith('/catalog/facts'):
            sample = next(s for s in self.samples if s['key'] == body['path']['segments'][-1]['name'])
            payload = {'key_value': preview(sample)['data']['key_value']['facts']}
        elif url.path.endswith('/scan/run/manual'): payload = {'execution_id': 'redis-scan'}
        elif url.path.endswith('/executions/redis-scan'): payload = {'status': 'success'}
        elif url.path.endswith('/items'): payload = self.items
        elif url.path.endswith('/manager/preview'):
            query = parse_qs(url.query)
            if query.get('page') == ['2']:
                assert statuses == (400,)
                payload = {'error_code': 'invalid_preview_page'}
            else:
                item_id = int(parse_qs(urlsplit(query['locator'][0]).query)['item_id'][0])
                payload = preview(self.samples[item_id - 1])
        else: raise AssertionError(f'unexpected route {method} {url.path}')
        return SimpleNamespace(payload=copy.deepcopy(payload))


class RedisConsumerContractTest(unittest.TestCase):
    def test_full_consumer_flow_checks_all_live_facts_and_previews_without_management_access(self):
        client = FakeClient()
        report = MODULE.run(client, 2, 17, 10)
        self.assertEqual(len(report['samples']), 9)
        self.assertEqual(report['scan_execution_id'], 'redis-scan')
        facts = [call for call in client.calls if call[1].endswith('/catalog/facts')]
        self.assertEqual(len(facts), 9)
        self.assertTrue(report['table_pagination_rejected'])
        self.assertFalse(any(path == '/api/v1/system/engines/17' for _, path, _, _ in client.calls))
        self.assertEqual([call[3]['force'] for call in client.calls if call[1].endswith('/scan/run/manual')], [True])

    def test_consumer_identity_and_live_catalog_fail_closed(self):
        for mutation in ('admin', 'tenant', 'machine', 'capability', 'credentials'):
            with self.subTest(mutation=mutation):
                client = FakeClient()
                if mutation == 'admin': client.context['authorization']['role_assignments'][0]['role_key'] = next(iter(MODULE.SUPPORT.FORBIDDEN_ADMIN_ROLES))
                if mutation == 'tenant': client.context['context']['tenant_id'] = '3'
                if mutation == 'machine': client.context['principal']['type'] = 'service'
                if mutation == 'capability': client.selectors[0]['capabilities']['storage']['catalog']['real_time'] = False
                if mutation == 'credentials': client.selectors[0]['connection_info'] = {}
                with self.assertRaises(MODULE.SuiteError): MODULE.run(client, 2, 17, 10)
        with self.assertRaises(MODULE.SuiteError): MODULE.run(FakeClient(), 1, 17, 10)

    def test_native_shapes_precision_binary_order_and_ttl_are_verified(self):
        for sample in MODULE.sample_contract(): MODULE.validate_preview(preview(sample), sample)
        samples = {sample['sample']: sample for sample in MODULE.sample_contract()}
        for name, mutate in [
            ('counter', lambda p: p['data']['key_value']['value'].update(value=9007199254740992)),
            ('binary', lambda p: p['data']['key_value']['value'].update(encoding='utf8')),
            ('ttl', lambda p: p['data']['key_value']['facts'].update(ttl_millis=-1)),
            ('list', lambda p: p['data']['key_value']['entries'].reverse()),
            ('set', lambda p: p['data']['key_value']['entries'].__setitem__(1, p['data']['key_value']['entries'][0])),
            ('zset', lambda p: p['data']['key_value']['entries'][0].update(score=1)),
            ('stream', lambda p: p['data']['key_value']['entries'][0].update(fields={'event': 'created'})),
            ('hash', lambda p: p['data'].update(mode='table')),
            ('string', lambda p: p['data']['key_value'].update(truncated=True))]:
            with self.subTest(name=name):
                payload = preview(samples[name]); mutate(payload)
                with self.assertRaises(MODULE.SuiteError): MODULE.validate_preview(payload, samples[name])
        for name in ('hash', 'set'):
            payload = preview(samples[name]); payload['data']['key_value']['entries'].reverse()
            MODULE.validate_preview(payload, samples[name])

    def test_meta_unknown_identity_rejects_duplicates_values_or_native_type_persistence(self):
        client = FakeClient()
        for mutate in [lambda v: v.__setitem__(1, copy.deepcopy(v[0])),
                       lambda v: v[0]['attributes']['item'].update(data_type='key_value'),
                       lambda v: v[0]['attributes'].update(native_type='string'),
                       lambda v: v[0].update(fingerprint=''), lambda v: v.pop()]:
            items = copy.deepcopy(client.items); mutate(items)
            with self.assertRaises(MODULE.SuiteError): MODULE.validate_items(items, client.samples)

    def test_binary_key_locator_round_trip_is_one_canonical_component(self):
        sample = MODULE.sample_contract()[-1]
        self.assertEqual(sample['value'], {'encoding': 'base64', 'value': 'AP9BRERQ', 'byte_length': 6})
        target = MODULE.locator(17, {'id': 9, 'full_name': sample['key']})
        self.assertEqual(parse_qs(urlsplit(target).query), {'type': ['key'], 'item_id': ['9']})
        self.assertNotIn('=', sample['key'])
        self.assertIn('/path/k:', target)

    def test_browser_success_requires_matching_evidence_and_cannot_reuse_stale_report(self):
        report = MODULE.run(FakeClient(), 2, 17, 10)
        with tempfile.TemporaryDirectory() as temporary, patch.dict(os.environ, ADDP_ONLINE_ARTIFACT_DIR=temporary, ADDP_ONLINE_TEST_RUN_ID='r1'):
            target = Path(temporary, 'redis-console.json')
            target.write_text('{}')
            with patch.object(MODULE.subprocess, 'run', return_value=SimpleNamespace(returncode=0)):
                with self.assertRaises(MODULE.SuiteError): MODULE.run_browser(report)
            self.assertFalse(target.exists())
            def browser(*args, **kwargs):
                evidence = {'run_id': 'r1', 'engine_id': 17, 'tenant_id': 2, 'principal_id': '8',
                            'samples': [s['sample'] for s in report['samples']], 'meta_ui_scan': True}
                Path(kwargs['env']['ADDP_ONLINE_REDIS_BROWSER_REPORT']).write_text(json.dumps(evidence))
                return SimpleNamespace(returncode=0)
            with patch.object(MODULE.subprocess, 'run', side_effect=browser):
                self.assertTrue(MODULE.run_browser(report)['meta_ui_scan'])


if __name__ == '__main__': unittest.main()
