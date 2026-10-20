from importlib import import_module
import copy
import hashlib
import os
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, unquote, urlsplit

M = import_module('scripts.test.ftp-consumer-flow-online')


def preview():
    return {'preview_type': 'table', 'data': {'columns': ['name', 'amount', 'id'], 'total': 3, 'rows': copy.deepcopy(M.SEED.ROWS)}}


class FakeClient:
    def __init__(self):
        self.calls = []
        self.samples = [{'format': kind, 'full_name': 'examples/中文 %样例.' + kind, 'size': len(kind), 'sha256': hashlib.sha256(kind.encode()).hexdigest()} for kind in ('csv', 'json', 'parquet')]
        self.items = [{'id': i + 1, 'node_id': i + 10, 'item_type': 'file', 'full_name': sample['full_name'], 'fingerprint': 'fp-' + str(i), 'attributes': {'schema_version': 1, 'item': {'data_type': 'table'}, 'type_info': {'table': {'fields': [{'name': name} for name in ('id', 'amount', 'name')]}}}} for i, sample in enumerate(self.samples)]
        self.bad_download = False
        self.admin = False
        self.write = False

    def request(self, method, path, expected, body=None, *, raw_response=False):
        self.calls.append((method, path, body))
        url = urlsplit(path)
        if url.path.endswith('/auth/context'):
            payload = {'principal': {'id': '8', 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'}, 'token': {'type': 'first_party_access_token'}, 'authorization': {'role_assignments': [{'role_key': 'tenant.administrator' if self.admin else 'online.ftp', 'permissions': sorted(M.PERMISSIONS)}]}}
        elif url.path.endswith('/engine-catalog/engines'):
            payload = [{'id': 17, 'engine_type': 'ftp', 'capabilities': {'engine_family': 'file', 'storage': {'store': {'stream_read': True, 'range_read': True, 'stream_write': self.write}}}}]
        elif url.path.endswith('/catalog/children'):
            segments = body['path']['segments']
            if not segments:
                payload = {'nodes': [{'kind': 'root', 'path': {'version': 'catalog.path/v1', 'segments': [{'term': 'root', 'kind': 'root', 'name': ''}]}}]}
            elif len(segments) == 1:
                payload = {'nodes': [{'name': 'examples', 'kind': 'directory', 'path': {'segments': segments + [{'term': 'directory', 'kind': 'directory', 'name': 'examples'}]}}]}
            else:
                payload = {'nodes': [{'name': sample['full_name'].split('/')[-1], 'kind': 'file', 'path': {'segments': segments + [{'term': 'file', 'kind': 'file', 'name': sample['full_name'].split('/')[-1]}]}} for sample in self.samples]}
        elif url.path.endswith('/catalog/facts'):
            name = body['path']['segments'][-1]['name']
            sample = next(sample for sample in self.samples if sample['full_name'].endswith('/' + name))
            payload = {'storage': {'path': sample['full_name'], 'size_bytes': sample['size']}}
        elif '/meta/engines/' in url.path:
            payload = self.items
        elif url.path.endswith('/preview'):
            payload = preview()
        elif url.path.endswith('/downloads/file'):
            locator = parse_qs(url.query)['locator'][0]
            name = unquote(urlsplit(locator).path).split('/')[-1]
            payload = b'corrupted' if self.bad_download else name.split('.')[-1].encode()
        else:
            raise AssertionError(path)
        return SimpleNamespace(payload=payload)


class FTPConsumerTest(unittest.TestCase):
    def test_exact_catalog_scan_preview_and_download(self):
        client = FakeClient()
        with patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='owned-run'), patch.object(M.SUPPORT, 'wait_for_scan', return_value='scan-owned'):
            report = M.run(client, 2, 17, client.samples, 30)
        self.assertEqual(len(report['samples']), 3)
        self.assertEqual(report['principal_id'], '8')
        self.assertEqual(len([call for call in client.calls if '/downloads/file?' in call[1]]), 3)
        self.assertTrue(all('%25' in sample['locator'] and '&item_id=' in sample['locator'] for sample in report['samples']))

    def test_rejects_admin_write_capability_and_corrupt_download(self):
        for fault in ('admin', 'write', 'bad_download'):
            with self.subTest(fault=fault):
                client = FakeClient()
                setattr(client, fault, True)
                with patch.object(M.SUPPORT, 'wait_for_scan', return_value='scan'), self.assertRaises((M.SuiteError, M.SUPPORT.SuiteError)):
                    M.run(client, 2, 17, client.samples, 30)

    def test_changed_or_partial_preview_and_schema_are_rejected(self):
        mutations = (lambda p: p['data']['rows'].pop(), lambda p: p['data']['rows'][0].update(name='wrong'), lambda p: p['data']['rows'][0].update(amount='100.26'), lambda p: p['data'].update(total=4), lambda p: p['data']['rows'][0].update(id=1.5))
        for mutate in mutations:
            value = preview()
            mutate(value)
            with self.assertRaises(M.SuiteError):
                M.validate_preview(value)
        client = FakeClient()
        client.items[0]['attributes']['type_info']['table']['fields'].pop()
        with self.assertRaises(M.SuiteError):
            M.validate_item(client.items[0], client.samples[0])

    def test_browser_must_prove_identity_ui_scan_all_previews_and_download_hashes(self):
        expected = {'run_id': 'owned', 'engine_id': 17, 'tenant_id': 2, 'principal_id': '8', 'samples': [{'format': 'csv', 'item_id': 1, 'size': 3, 'sha256': 'a' * 64}]}
        proof = {key: expected[key] for key in ('run_id', 'engine_id', 'tenant_id', 'principal_id')}
        proof.update(meta_ui_scan=True, previews=3, downloads=copy.deepcopy(expected['samples']))
        M.validate_browser_report(proof, expected)
        for key, value in (('principal_id', 'another'), ('meta_ui_scan', False), ('downloads', []), ('previews', 2)):
            changed = {**proof, key: value}
            with self.assertRaises(M.SuiteError):
                M.validate_browser_report(changed, expected)


if __name__ == '__main__':
    unittest.main()
