import base64
import hashlib
import importlib
import json
import subprocess
from pathlib import Path
from types import SimpleNamespace
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit

SCENE = importlib.import_module('scripts.test.manager-feather-online')
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
        fields = [{'name': n, 'type': t, 'nullable': n in FIXTURE.NULLABLE} for n,t in zip(FIXTURE.FIELD_NAMES,FIXTURE.FIELD_TYPES)]
        result = {'id': item_id, 'engine_id': engine, 'full_name': full_name, 'fingerprint': str(item_id),
                  'attributes': {'schema_version':1,'item': {'format': 'feather', 'data_type': 'table', 'layout': 'single'},
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
            item=next(v for v in self.items.values() if v['full_name'] in q['locator'][0])
            codec=Path(item['full_name']).stem
            attrs={'schema_version':1,'item':{'format':'feather','data_type':'table','layout':'single'},
                   'type_info':{'table':{'row_count':FIXTURE.COUNT,'fields':[
                       {'name':n,'type':t,'native_type':native,'nullable':n in FIXTURE.NULLABLE}
                       for n,t,native in zip(FIXTURE.FIELD_NAMES,FIXTURE.FIELD_TYPES,FIXTURE.NATIVE_TYPES)]}},
                   'format_info':{'feather':{'version':2,'record_batches':3,'source_version':'fixed','validation':'batch_structure'}},
                   'storage':{'content_hash':hashlib.sha256(FIXTURE.source_bytes(codec)).hexdigest()}}
            result['data']['item_meta']={'fingerprint':item['fingerprint'],'row_count':FIXTURE.COUNT,
                'attributes':[{'key':k,'value':v} for k,v in attrs.items()]}
            if self.failure=='content':attrs['storage']['content_hash']='wrong'
            if self.failure == 'page':
                result['data']['rows'][0]['id'] = 9007199254740992
        elif '/manager/downloads/file?' in path:
            codec = next(c for c in FIXTURE.CODECS if c + '.feather' in path)
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


class FeatherSceneTest(unittest.TestCase):
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
                return {'sources': [{'codec':c,'name':c+'.feather','size_bytes':len(FIXTURE.source_bytes(c)),'sha256':hashlib.sha256(FIXTURE.source_bytes(c)).hexdigest()} for c in FIXTURE.CODECS], 'count': FIXTURE.COUNT}
            if action == 'verify':
                raw = b'parquet'
                return {'sources_unchanged': True, 'outputs': [
                    {'object': f'feather-output/from-{s}-{c}.parquet', 'size_bytes': len(raw),
                     'sha256': hashlib.sha256(raw).hexdigest(), 'content_base64': base64.b64encode(raw).decode(),
                     'all_values_and_nulls_verified': failure != 'physical'}
                    for s in ('minio', 'nfs') for c in FIXTURE.CODECS]}
            return {'objects_removed': True, 'files_removed': True, 'outputs_removed': failure != 'cleanup'}

        def browser(context):
            rows = []
            for case in context['cases']:
                (root / f'feather-{case["storage"]}-{case["codec"]}-page2.png').write_bytes(b'\x89PNG\r\n\x1a\nfixture')
                rows.append({'locator': case['locator'], 'page_button_verified': failure != 'browser', 'original_button_download_verified':True,
                             'values_verified': True, 'download_sha256': case['spec']['sha256'],
                             'download_size': case['spec']['size_bytes']})
            return {'schema_version': 'addp.manager-feather-browser/v1', 'result': 'passed', 'run_id': 'manager-1-1', 'cases': rows,'page_errors':[]}

        environment = {'ADDP_ONLINE_MANAGER_CASE': 'native-table-feather', 'ADDP_ONLINE_ARTIFACT_DIR': str(root),
            'GATEWAY_URL': 'http://fixture', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'consumer',
            'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'initializer', 'ADDP_ONLINE_TEST_TENANT_ID': '2',
            'ADDP_ONLINE_TEST_RUN_ID': 'manager-1-1', 'ADDP_ONLINE_MANAGER_MINIO_BUCKET': 'source',
            'ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID': '12', 'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '14'}
        with patch.object(SCENE.REGISTRATION, 'ExactFileMetadataGrants', return_value=grants), patch.object(FIXTURE,'source_bytes',side_effect=lambda c: ('fixture-'+c).encode()):
            if failure:
                with self.assertRaises(ValueError):
                    SCENE.run(root, environment, helpers, physical_runner=physical, browser_runner=browser)
            else:
                SCENE.run(root, environment, helpers, physical_runner=physical, browser_runner=browser)
        return client, actions, json.loads((root / 'manager-feather.json').read_text())

    def test_complete_six_sources_and_batch_outputs(self):
        client, actions, report = self.run_scene()
        self.assertEqual(report['result'], 'passed')
        self.assertEqual(len(report['cases']), 6)
        self.assertEqual(len(report['physical']['outputs']), 6)
        self.assertEqual(client.deleted, list(reversed(client.tasks)))
        self.assertEqual(actions, ['seed', 'verify', 'delete'])
        self.assertEqual(client.assert_body['config']['source']['format'], 'feather')

    def test_failures_cannot_pass_and_always_clean_owned_sources_and_tasks(self):
        for failure in ('seed', 'page', 'content', 'download', 'transfer', 'physical', 'browser', 'cleanup'):
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
        self.assertEqual(OWNER.manager_case({'ADDP_ONLINE_MANAGER_CASE': 'native-table-feather'}), 'native-table-feather')
        for action in ('seed', 'verify', 'delete', 'foreign'):
            with self.assertRaises(ValueError):
                FIXTURE.physical(action, Path('/tmp'), {})

    def test_scalar_oracle_and_guarded_projection_boundaries(self):
        self.assertEqual(FIXTURE.CODECS,('uncompressed','lz4','zstd'))
        self.assertEqual(FIXTURE.public_row(1)['id'],'-9007199254740994')
        self.assertEqual(FIXTURE.public_row(1)['payload'],'AAH/')
        self.assertEqual(FIXTURE.public_row(0)['label'],None)
        self.assertEqual(FIXTURE.public_row(5)['label'],'')
        self.assertEqual(FIXTURE.public_row(3)['payload'],'')
        self.assertEqual(len(SCENE.PERMISSIONS),11)
        with self.assertRaises(ValueError):SCENE.validate_content({'data':{}},{'sha256':'fixture'},'id')


    def test_browser_oracle_matches_all_source_scalars_including_positive_zero(self):
        # Execute the real JS oracle. Python's integer unary minus yields +0;
        # JS unary minus applied to numeric zero yields -0 and deep equality differs.
        code = r"""
const fs = require('node:fs'), vm = require('node:vm'), assert = require('node:assert/strict')
const source = fs.readFileSync(process.argv[1], 'utf8')
const declaration = source.slice(source.indexOf('function expectedRow('), source.indexOf('\ntest('))
const row = vm.runInNewContext(declaration + '; expectedRow', { Buffer })
const expected = JSON.parse(fs.readFileSync(0, 'utf8'))
for (let i = 0; i < expected.length; i++) assert.deepStrictEqual({ ...row(i) }, expected[i], `source row ${i}`)
"""
        path = Path(__file__).resolve().parents[2] / 'console/frontend/e2e/online/manager-feather.spec.js'
        completed = subprocess.run(['node', '-e', code, str(path)],
            input=json.dumps([FIXTURE.public_row(i) for i in range(FIXTURE.COUNT)], ensure_ascii=False),
            capture_output=True, text=True, timeout=15)
        self.assertEqual(completed.returncode, 0, completed.stderr)
