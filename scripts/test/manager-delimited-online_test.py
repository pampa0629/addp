import copy
import csv
import hashlib
import importlib
import io
import json
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

SCENE = importlib.import_module('scripts.test.manager-delimited-online')
FIXTURE = SCENE.FIXTURE
OWNER = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')


class Client:
    def __init__(self, failure=None):
        self.failure, self.items = failure, {}

    def item(self, engine, full_name, *_):
        format_name = Path(full_name).suffix[1:]
        identifier = engine * 10 + ('csv', 'tsv').index(format_name)
        spec = next(s for s in FIXTURE.sources() if s['format'] == format_name)
        self.items[identifier] = {'id': identifier, 'fingerprint': str(identifier), 'full_name': full_name,
            'row_count': 25, 'attributes': {'item': {'format': format_name, 'layout': 'single', 'data_type': 'table'},
                'type_info': {'table': {'row_count': 25, 'native': {'has_header': True},
                    'fields': [{'name': n, 'type': t} for n, t in zip(FIXTURE.COLUMNS, ['int', 'string', 'string'])]}},
                'format_info': {format_name: {'encoding': 'gb18030'}}, 'storage': {'content_hash': spec['sha256']}}}
        return copy.deepcopy(self.items[identifier])

    def request(self, method, path, expected, body=None):
        if '/meta/items/' in path:
            result = copy.deepcopy(self.items[int(path.rsplit('/', 1)[1])])
            if self.failure == 'metadata':
                result['attributes']['format_info']['csv'] = {'encoding': 'gbk'}
        elif '/manager/preview?' in path:
            query = parse_qs(urlsplit(path).query)
            start = (int(query['page'][0]) - 1) * 20
            result = {'preview_type': 'table', 'data': {'total': 25,
                      'rows': [FIXTURE.public_row(i) for i in range(start, min(start + 20, 25))]}}
            if self.failure == 'page' and result['data']['rows']:
                result['data']['rows'][0][FIXTURE.COLUMNS[1]] = 'wrong'
        elif '/downloads/file?' in path:
            format_name = 'csv' if '.csv' in path else 'tsv'
            return SimpleNamespace(raw=FIXTURE.source_bytes(format_name) + (b'wrong' if self.failure == 'download' else b''))
        else:
            raise AssertionError('unexpected request: ' + path)
        return SimpleNamespace(payload=result)


class DelimitedCaseTest(unittest.TestCase):
    def run_scene(self, failure=None):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        client = Client(failure)
        grants = SimpleNamespace(grant=lambda *a, **k: {'confirmed': True}, cleanup=lambda: {'grants_revoked': True, 'metadata_hidden': True})
        actions, scans = [], []
        def scan(*a, **kw):
            scans.append(kw)
            return 'scan-' + str(len(scans))
        helper = SimpleNamespace(GatewayClient=lambda *a: client, positive_int=lambda v, _: int(v),
            validate_user_identity=lambda *a: {'tenant_id': 2, 'principal_id': '33', 'permissions_verified': list(OWNER.REQUIRED_PERMISSIONS)},
            wait_for_meta_scan=scan, find_fixture_item=lambda c, e, name, *a: client.item(e, name),
            build_item_locator=lambda e, item: f'addp://engine/{e}/path/{item["full_name"]}?type=file')
        def physical(action):
            actions.append(action)
            if action == 'seed':
                if failure == 'seed': raise ValueError('seed failed')
                return {'sources': FIXTURE.sources(), 'count': 25}
            if action == 'verify': return {'sources_unchanged': failure != 'physical'}
            return {'objects_removed': True, 'files_removed': failure != 'cleanup'}
        def browser(context):
            cases = []
            for case in context['cases']:
                for mode in ('header', 'headerless'):
                    (root / f'delimited-{case["storage"]}-{case["spec"]["format"]}-{mode}.png').write_bytes(b'\x89PNG\r\n\x1a\nfixture')
                cases.append({'locator': case['locator'], **dict.fromkeys(('encoding_request_verified', 'header_mode_verified',
                    'ordinary_refresh_verified', 'reopened', 'header_restored', 'original_button_download_verified'), True),
                    'download_sha256': case['spec']['sha256'], 'download_size': case['spec']['size_bytes']})
            if failure == 'browser': cases[0]['reopened'] = False
            return {'schema_version': 'addp.manager-delimited-browser/v1', 'result': 'passed', 'run_id': 'manager-1-1', 'cases': cases}
        env = {'ADDP_ONLINE_MANAGER_CASE': 'delimited-read-settings', 'ADDP_ONLINE_ARTIFACT_DIR': str(root), 'GATEWAY_URL': 'http://fixture',
               'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'consumer', 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'initializer',
               'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_TEST_RUN_ID': 'manager-1-1', 'ADDP_ONLINE_MANAGER_MINIO_BUCKET': 'source',
               'ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID': '12', 'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '14'}
        with patch.object(SCENE.REGISTRATION, 'ExactFileMetadataGrants', return_value=grants) as factory:
            if failure:
                with self.assertRaises(ValueError): SCENE.run(root, env, helper, physical_runner=physical, browser_runner=browser)
            else:
                SCENE.run(root, env, helper, physical_runner=physical, browser_runner=browser)
            self.assertTrue(factory.call_args.kwargs['scan_preparation'])
        return actions, scans, json.loads((root / 'manager-delimited.json').read_text())

    def test_four_sources_basic_scan_pages_originals_and_cleanup(self):
        actions, scans, report = self.run_scene()
        self.assertEqual(report['result'], 'passed')
        self.assertEqual(actions, ['seed', 'verify', 'delete'])
        self.assertEqual(len(scans), 2)
        self.assertTrue(all(s['depth'] == 'basic' and len(s['scope']['targets']) == 2 for s in scans))
        self.assertEqual(len(report['cases']), 4)
        self.assertTrue(all([p['rows'] for p in c['pages']] == [20, 5, 0] for c in report['cases']))

    def test_failure_does_not_pass_and_cleanup_runs(self):
        for failure in ('seed', 'metadata', 'page', 'download', 'physical', 'browser', 'cleanup'):
            with self.subTest(failure=failure):
                actions, _, report = self.run_scene(failure)
                self.assertEqual(report['result'], 'failed')
                self.assertEqual(actions[-1], 'delete')

    def test_fixed_bytes_independent_decode_and_hashes(self):
        hashes = {'csv': '2b6c1b28550498cbc90b51215b48077cb7d756bff7ae189bd5cf64b3758bf738',
                  'tsv': 'd95d3306ed02a6824bc0cfcf31e0a1ea524cd72b95f8a9dabfa021a42a646fc1'}
        for spec in FIXTURE.sources():
            raw = FIXTURE.source_bytes(spec['format'])
            self.assertEqual(len(raw), 1079)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), hashes[spec['format']])
            rows = list(csv.reader(io.StringIO(raw.decode('gb18030'), newline=''), delimiter=',' if spec['format'] == 'csv' else '\t'))
            self.assertEqual(rows[0], FIXTURE.COLUMNS)
            self.assertEqual(rows[1:], [[str(i), f'城市𠀀😀{i}', '第一行\r\n第二行,含逗号'] for i in range(25)])

    def test_wrong_page_and_wrong_read_facts_rejected(self):
        with self.assertRaises(ValueError): SCENE.validate_page({'preview_type': 'table', 'data': {'rows': [], 'total': 26}}, 3)
        detail = Client().item(12, 'test/gb18030-quoted.csv')
        for mutation in ('index', 'encoding', 'header', 'hash'):
            changed = copy.deepcopy(detail)
            if mutation == 'index': changed['attributes']['access_index'] = {'table': {'kind': 'stale'}}
            if mutation == 'encoding': changed['attributes']['format_info']['csv']['encoding'] = 'gbk'
            if mutation == 'header': changed['attributes']['type_info']['table']['native']['has_header'] = False
            if mutation == 'hash': changed['attributes']['storage']['content_hash'] = 'wrong'
            with self.assertRaises(ValueError): SCENE.validate_metadata(changed, FIXTURE.sources()[0])

    def test_profile_rejects_unowned_environment(self):
        self.assertEqual(OWNER.manager_case({'ADDP_ONLINE_MANAGER_CASE': 'delimited-read-settings'}), 'delimited-read-settings')
        for action in ('seed', 'verify', 'delete', 'foreign'):
            with self.assertRaises(ValueError): FIXTURE.physical(action, Path('/tmp'), {})

    def test_physical_worker_source_drift_and_owned_cleanup(self):
        store = {}
        class Minio:
            def __init__(self, *a, **kw): pass
            def list_objects(self, bucket, prefix, **kw): return [k for k in store if k.startswith(prefix)]
            def put_object(self, bucket, name, data, length, **kw): store[name] = data.read()
            def get_object(self, bucket, name): return SimpleNamespace(read=lambda: store[name], close=lambda: None, release_conn=lambda: None)
            def remove_object(self, bucket, name): store.pop(name, None)
        with tempfile.TemporaryDirectory() as directory, patch.dict('sys.modules', {'minio': SimpleNamespace(Minio=Minio)}):
            request = {'nfs_root': directory, 'storage': dict(endpoint='fixture', access_key='fixture', secret_key='fixture', bucket='fixture')}
            self.assertEqual(FIXTURE.worker(dict(request, action='seed'))['sources'], FIXTURE.sources())
            self.assertTrue(FIXTURE.worker(dict(request, action='verify'))['sources_unchanged'])
            file = Path(directory) / FIXTURE.DIRECTORY / FIXTURE.sources()[0]['name']
            file.write_bytes(b'changed')
            with self.assertRaisesRegex(AssertionError, 'source_changed'): FIXTURE.worker(dict(request, action='verify'))
            self.assertEqual(FIXTURE.worker(dict(request, action='delete')), {'objects_removed': True, 'files_removed': True})
            self.assertEqual(store, {})
            self.assertEqual(list(Path(directory).iterdir()), [])
