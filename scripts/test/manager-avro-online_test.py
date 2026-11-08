import base64
import hashlib
import importlib
import json
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

SCENE = importlib.import_module('scripts.test.manager-avro-online')
FIXTURE = SCENE.FIXTURE
OWNER = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')


class Client:
    def __init__(self, failure=None):
        self.failure = failure
        self.tasks = []
        self.deleted = []
        self.items = {}

    def item(self, engine, full_name, *_):
        codec = Path(full_name).stem
        item_id = engine * 10 + FIXTURE.CODECS.index(codec)
        fields = [{'name': f['name'], 'type': {'long': 'bigint', 'boolean': 'bool'}.get(
            f['type'][1] if isinstance(f['type'], list) else f['type'],
            f['type'][1] if isinstance(f['type'], list) else f['type']), 'nullable': isinstance(f['type'], list)}
            for f in FIXTURE.SCHEMA['fields']]
        result = {'id': item_id, 'engine_id': engine, 'full_name': full_name, 'fingerprint': str(item_id),
                  'attributes': {'item': {'format': 'avro', 'data_type': 'table', 'layout': 'single'},
                                 'type_info': {'table': {'fields': fields}}}}
        self.items[item_id] = result
        return result

    def request(self, method, path, expected, body=None):
        if '/meta/items/' in path:
            result = self.items[int(path.rsplit('/', 1)[1])]
        elif '/manager/preview?' in path:
            q = parse_qs(urlsplit(path).query)
            page = int(q['page'][0])
            start = (page - 1) * 20
            result = {'preview_type': 'table', 'data': {'total': FIXTURE.COUNT, 'has_more': page != 126,
                      'rows': [FIXTURE.public_row(i) for i in range(start, min(start + 20, FIXTURE.COUNT))]}}
            if self.failure == 'page':
                result['data']['rows'][0]['id'] = 9007199254740992
        elif '/manager/downloads/file?' in path:
            codec = next(c for c in FIXTURE.CODECS if c + '.avro' in path)
            return SimpleNamespace(raw=FIXTURE.source_bytes(codec) + (b'bad' if self.failure == 'download' else b''))
        elif method == 'POST' and path.endswith('/task-definitions'):
            self.tasks.append(len(self.tasks) + 1)
            self.assert_body = body
            result = {'id': self.tasks[-1]}
        elif path.endswith('/start'):
            result = {'execution_id': 'execution-' + path.split('/')[-2]}
        elif '/transfer/executions/' in path:
            result = {'status': 'failed' if self.failure == 'transfer' else 'success',
                      'records_read': FIXTURE.COUNT, 'records_written': FIXTURE.COUNT}
        elif method == 'DELETE':
            self.deleted.append(int(path.rsplit('/', 1)[1]))
            result = {}
        elif expected == (404,):
            assert int(path.rsplit('/', 1)[1]) in self.deleted
            result = {}
        else:
            raise AssertionError((method, path))
        return SimpleNamespace(payload=result)


class AvroSceneTest(unittest.TestCase):
    def run_scene(self, failure=None):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name)
        client = Client(failure)
        actions = []
        grants = SimpleNamespace(operator='1', recipient='2', grant=lambda *a, **kw: {'request_id': 'fixture'},
                                 cleanup=lambda: {'grants_revoked': True, 'metadata_hidden': True})
        helpers = SimpleNamespace(GatewayClient=lambda *args: client, positive_int=lambda v, n: int(v),
            validate_user_identity=lambda *args: {'principal_id': '2'}, wait_for_meta_scan=lambda *a, **kw: 'scan',
            find_fixture_item=lambda c, e, name, *a: client.item(e, name),
            build_item_locator=lambda e, item: f'addp://engine/{e}/path/{item["full_name"]}?type=file')

        def physical(action):
            actions.append(action)
            if action == 'seed':
                if failure == 'seed':
                    raise ValueError('seed failed')
                return {'sources': FIXTURE.sources(), 'count': FIXTURE.COUNT}
            if action == 'verify':
                raw = b'parquet'
                return {'sources_unchanged': True, 'outputs': [
                    {'object': f'avro-output/from-{s}-{c}.parquet', 'size_bytes': len(raw),
                     'sha256': hashlib.sha256(raw).hexdigest(), 'content_base64': base64.b64encode(raw).decode(),
                     'all_values_and_nulls_verified': failure != 'physical'}
                    for s in ('minio', 'nfs') for c in FIXTURE.CODECS]}
            return {'objects_removed': True, 'files_removed': True, 'outputs_removed': failure != 'cleanup'}

        def browser(context):
            rows = []
            for case in context['cases']:
                (root / f'avro-{case["storage"]}-{case["codec"]}-page2.png').write_bytes(b'\x89PNG\r\n\x1a\nfixture')
                rows.append({'locator': case['locator'], 'page_button_verified': failure != 'browser',
                             'values_verified': True, 'download_sha256': case['spec']['sha256'],
                             'download_size': case['spec']['size_bytes']})
            return {'schema_version': 'addp.manager-avro-browser/v1', 'result': 'passed', 'run_id': 'manager-1-1', 'cases': rows}

        environment = {'ADDP_ONLINE_MANAGER_CASE': 'native-table-avro', 'ADDP_ONLINE_ARTIFACT_DIR': str(root),
            'GATEWAY_URL': 'http://fixture', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'consumer',
            'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'initializer', 'ADDP_ONLINE_TEST_TENANT_ID': '2',
            'ADDP_ONLINE_TEST_RUN_ID': 'manager-1-1', 'ADDP_ONLINE_MANAGER_MINIO_BUCKET': 'source',
            'ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID': '12', 'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '14'}
        with patch.object(SCENE.REGISTRATION, 'ExactFileMetadataGrants', return_value=grants):
            if failure:
                with self.assertRaises(ValueError):
                    SCENE.run(root, environment, helpers, physical_runner=physical, browser_runner=browser)
            else:
                SCENE.run(root, environment, helpers, physical_runner=physical, browser_runner=browser)
        return client, actions, json.loads((root / 'manager-avro.json').read_text())

    def test_complete_six_sources_and_batch_outputs(self):
        client, actions, report = self.run_scene()
        self.assertEqual(report['result'], 'passed')
        self.assertEqual(len(report['cases']), 6)
        self.assertEqual(len(report['physical']['outputs']), 6)
        self.assertEqual(client.deleted, list(reversed(client.tasks)))
        self.assertEqual(actions, ['seed', 'verify', 'delete'])
        self.assertEqual(client.assert_body['config']['source']['format'], 'avro')

    def test_failures_cannot_pass_and_always_clean_owned_sources_and_tasks(self):
        for failure in ('seed', 'page', 'download', 'transfer', 'physical', 'browser', 'cleanup'):
            with self.subTest(failure=failure):
                client, actions, report = self.run_scene(failure)
                self.assertEqual(report['result'], 'failed')
                self.assertEqual(actions[-1], 'delete')
                self.assertEqual(client.deleted, list(reversed(client.tasks)))

    def test_page_validation_rejects_wrong_total_and_binary_or_bigint_corruption(self):
        for key, value in [('id', 9007199254740992), ('payload', 'bad')]:
            rows = [FIXTURE.public_row(i) for i in range(20, 40)]
            rows[0][key] = value
            with self.assertRaises(ValueError):
                SCENE.validate_page({'preview_type': 'table', 'data': {'rows': rows, 'total': 2503, 'has_more': True}}, 2, 20)

    def test_fixture_profile_and_unknown_case_rejected_before_launch(self):
        self.assertEqual(OWNER.manager_case({'ADDP_ONLINE_MANAGER_CASE': 'native-table-avro'}), 'native-table-avro')
        for action in ('seed', 'verify', 'delete', 'foreign'):
            with self.assertRaises(ValueError):
                FIXTURE.physical(action, Path('/tmp'), {})

    def test_standard_avro_header_codec_and_repeatability(self):
        specs = FIXTURE.sources()
        self.assertEqual([s['codec'] for s in specs], ['null', 'deflate', 'snappy'])
        self.assertEqual(len({s['sha256'] for s in specs}), 3)
        for spec in specs:
            raw = FIXTURE.source_bytes(spec['codec'])
            self.assertTrue(raw.startswith(b'Obj\x01'))
            self.assertEqual(len(raw), spec['size_bytes'])
            self.assertEqual(hashlib.sha256(raw).hexdigest(), spec['sha256'])
