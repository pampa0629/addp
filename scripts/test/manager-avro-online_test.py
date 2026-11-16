import base64
import hashlib
import importlib
import json
from pathlib import Path
from types import SimpleNamespace
import tempfile
import subprocess
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
        self.task_bodies = {}

    def item(self, engine, full_name, *_):
        stem = Path(full_name).stem
        nested = stem.startswith('nested-')
        enumeration = stem.startswith('enum-')
        mapping = stem.startswith('map-')
        fixed = stem.startswith('fixed-')
        codec = stem.removeprefix('nested-').removeprefix('enum-').removeprefix('map-').removeprefix('fixed-')
        item_id = engine * 10 + FIXTURE.CODECS.index(codec) + (12 if fixed else 9 if mapping else 6 if enumeration else 3 if nested else 0)
        expected = [('id', 'bigint', False), ('small', 'int', False), ('flag', 'bool', True),
                    ('label', 'string', True), ('ratio', 'float', False), ('value', 'double', False),
                    ('payload', 'bytes', True), ('day', 'date', True), ('amount', 'decimal', True), ('fixed_amount', 'decimal', True),
                    ('stamp_ms','timestamp',True), ('stamp_us','timestamp',True), ('stamp_ns','timestamp',True)]
        fields = [dict(name=name, type=kind, nullable=nullable, **({'precision':38,'scale':6} if kind == 'decimal' else {}))
                  for name, kind, nullable in expected]
        for f in fields[-3:]:
            f.update(native_type='long', temporal={'unit':FIXTURE.TIMESTAMPS[f['name']][0], 'timezone':'UTC'})
        if nested:
            fields = SCENE.nested_field_facts()
            if self.failure == 'nested-meta': fields[2]['element']['type'] = 'string'
        if enumeration:
            fields = SCENE.enum_field_facts()
            if self.failure == 'enum-meta':fields[1]['enum']['symbols'].reverse()
        if mapping:
            fields = SCENE.map_field_facts()
            if self.failure == 'map-meta':fields[1]['map_value']['type']='string'
        if fixed:
            fields = SCENE.fixed_field_facts()
            if self.failure == 'fixed-meta':fields[1]['fixed']['size']=3
        result = {'id': item_id, 'engine_id': engine, 'full_name': full_name, 'fingerprint': str(item_id),
                  'attributes': {'item': {'format': 'avro', 'data_type': 'table', 'layout': 'single'},
                                 'type_info': {'table': {'fields': fields}}}}
        if nested and self.failure == 'nested-private-facts': result['attributes']['format_info']={'avro':{'schema':FIXTURE.NESTED_SCHEMA}}
        self.items[item_id] = result
        return result

    def request(self, method, path, expected, body=None):
        if '/meta/items/' in path:
            result = self.items[int(path.rsplit('/', 1)[1])]
        elif '/manager/preview?' in path:
            q = parse_qs(urlsplit(path).query)
            nested = 'nested-' in q['locator'][0]
            enumeration = 'enum-' in q['locator'][0]
            mapping = 'map-' in q['locator'][0]
            fixed = 'fixed-' in q['locator'][0]
            page = int(q['page'][0])
            start = (page - 1) * 20
            result = {'preview_type': 'table', 'data': {'total': FIXTURE.COUNT, 'has_more': page != 126,
                      'rows': [(FIXTURE.fixed_public_row if fixed else FIXTURE.map_public_row if mapping else FIXTURE.enum_public_row if enumeration else FIXTURE.nested_public_row if nested else FIXTURE.public_row)(i) for i in range(start, min(start + 20, FIXTURE.COUNT))]}}
            if self.failure == 'nested-page' and nested:
                result['data']['rows'][0]['id'] = 9007199254740992
            if fixed and self.failure == 'fixed-page':result['data']['rows'][0]['token']='AAAA'
            if mapping and self.failure == 'map-page':result['data']['rows'][0]['nested']['outer']['']='rounded'
            if enumeration and self.failure == 'enum-page':result['data']['rows'][0]['state']='invalid'
            if self.failure == 'page':
                result['data']['rows'][0]['id'] = 9007199254740992
        elif '/manager/downloads/file?' in path:
            if expected == (403,):
                return SimpleNamespace(status=403, headers={'Content-Type':'application/json'}, payload={'error':'denied'})
            codec = next(c for c in FIXTURE.CODECS if c + '.avro' in path)
            return SimpleNamespace(raw=(FIXTURE.fixed_source_bytes if 'fixed-' in path else FIXTURE.map_source_bytes if 'map-' in path else FIXTURE.enum_source_bytes if 'enum-' in path else FIXTURE.nested_source_bytes if 'nested-' in path else FIXTURE.source_bytes)(codec) + (b'bad' if self.failure == 'download' else b''))
        elif method == 'POST' and path.endswith('/task-definitions'):
            self.tasks.append(len(self.tasks) + 1)
            self.assert_body = body
            self.task_bodies[self.tasks[-1]] = body
            result = {'id': self.tasks[-1]}
        elif path.endswith('/start'):
            result = {'execution_id': 'execution-' + path.split('/')[-2]}
        elif '/transfer/executions/' in path:
            body = self.task_bodies[int(path.rsplit('-',1)[1])]
            nested = 'nested-' in body['config']['source']['locator'] or 'enum-' in body['config']['source']['locator'] or 'map-' in body['config']['source']['locator'] or 'fixed-' in body['config']['source']['locator']
            result = {'status':'failed' if (nested and self.failure != 'nested-transfer') or self.failure == 'transfer' else 'success',
                      'records_read':0 if nested else FIXTURE.COUNT, 'records_written': (1 if self.failure == 'nested-count' else 0) if nested else FIXTURE.COUNT,
                      'error_details':{'category':'connection_failed' if self.failure == 'nested-reason' else 'execution_failed'}}
            if nested and self.failure == 'legacy-error': result['error_msg'] = SCENE.REFUSAL_MESSAGES['nested_preview_only']
            if nested and self.failure == 'missing-count': result.pop('records_written')
        elif method == 'DELETE':
            self.deleted.append(int(path.rsplit('/', 1)[1]))
            result = {}
        elif expected == (404,):
            assert int(path.rsplit('/', 1)[1]) in self.deleted
            result = {}
        else:
            raise AssertionError((method, path))
        return SimpleNamespace(payload=result)


class AvroRuntimeRefusalProofTest(unittest.TestCase):
    def proof(self, fault=None, kind='nested_preview_only'):
        execution_id = '11111111-2222-4333-8444-555555555555'
        actual_id = '99999999-2222-4333-8444-555555555555' if fault == 'execution' else execution_id
        tenant = 3 if fault == 'tenant' else 2
        error = 'open target failed' if fault == 'reason' else SCENE.REFUSAL_MESSAGES[kind]
        message = ('[rows:' + ('0' if fault == 'uncommitted' else '1') + '] '
                   'UPDATE "common"."task_executions" SET "error_details"=\'' + json.dumps({'message': error}) +
                   '\',"status"=\'failed\' WHERE (execution_id = \'' + actual_id +
                   f"' AND tenant_id = {tenant} AND execution_boundary = 'bounded')")
        event = {'module_name': 'meta' if fault == 'module' else 'transfer',
                 'role': 'backend' if fault == 'role' else 'worker', 'message': message}
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            log = root / 'logs/runtime/transfer/worker/run.jsonl'
            log.parent.mkdir(parents=True)
            line = json.dumps(event)
            if fault != 'missing': log.write_text((line + '\n') * (2 if fault == 'duplicate' else 1))
            proof = SCENE.verify_runtime_refusals(root, {execution_id:kind}, 2, timeout=0)
            self.assertEqual(proof, [{'execution_id': execution_id, 'kind': kind,
                                     'log_file': 'logs/runtime/transfer/worker/run.jsonl', 'line': 1,
                                     'sha256': hashlib.sha256(line.encode()).hexdigest()}])
            self.assertNotIn(SCENE.REFUSAL_MESSAGES[kind], json.dumps(proof))

    def test_exact_worker_tenant_and_persisted_refusal(self):
        self.proof()
        self.proof(kind='enum_preview_only')
        self.proof(kind='map_preview_only')

    def test_wrong_scope_role_reason_missing_and_duplicate_evidence_fail(self):
        for fault in ('execution', 'tenant', 'module', 'role', 'reason', 'uncommitted', 'missing', 'duplicate'):
            with self.subTest(fault=fault), self.assertRaises(ValueError): self.proof(fault)


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
                return {'sources': FIXTURE.sources(), 'nested_sources':FIXTURE.nested_sources(), 'enum_sources':FIXTURE.enum_sources(), 'map_sources':FIXTURE.map_sources(), 'fixed_sources':FIXTURE.fixed_sources(), 'count': FIXTURE.COUNT}
            if action == 'verify':
                raw = b'parquet'
                return {'sources_unchanged': True, 'nested_sources_verified':6, 'nested_outputs_absent':failure != 'nested-output', 'enum_sources_verified':6, 'enum_outputs_absent':failure != 'enum-output', 'map_sources_verified':6, 'map_outputs_absent':failure != 'map-output', 'fixed_sources_verified':6, 'fixed_outputs_absent':failure != 'fixed-output', 'outputs': [
                    {'object': f'avro-output/from-{s}-{c}.parquet', 'size_bytes': len(raw),
                     'sha256': hashlib.sha256(raw).hexdigest(), 'content_base64': base64.b64encode(raw).decode(),
                     'all_values_and_nulls_verified': failure != 'physical', 'native_timestamp_ticks_verified': failure != 'ticks',
                     'timestamp_units': {name:unit for name,(unit,_,_) in FIXTURE.TIMESTAMPS.items()}}
                    for s in ('minio', 'nfs') for c in FIXTURE.CODECS]}
            return {'objects_removed': True, 'files_removed': True, 'outputs_removed': failure != 'cleanup'}

        def browser(context):
            outcomes = {'cases':[], 'nested_cases':[], 'enum_cases':[], 'map_cases':[], 'fixed_cases':[]}
            for name in outcomes:
                for case in context[name]:
                    nested = case.get('nested') is True
                    enumeration = case.get('enum') is True
                    mapping = case.get('map') is True
                    fixed = case.get('fixed') is True
                    prefix = f'avro-{case["storage"]}-' + ('fixed-' if fixed else 'map-' if mapping else 'enum-' if enumeration else 'nested-' if nested else '') + case['codec']
                    (root / (prefix+'-page2.png')).write_bytes(b'\x89PNG\r\n\x1a\nfixture')
                    if nested:(root / (prefix+'-detail.png')).write_bytes(b'\x89PNG\r\n\x1a\nfixture')
                    if enumeration or mapping or fixed:(root / (prefix+'-declaration.png')).write_bytes(b'\x89PNG\r\n\x1a\nfixture')
                    if mapping or fixed:(root / (prefix+'-detail.png')).write_bytes(b'\x89PNG\r\n\x1a\nfixture')
                    outcomes[name].append({'locator':case['locator'],'page_button_verified':failure != 'browser',
                        'values_verified':True, **({'fixed_declaration_verified':failure != 'fixed-browser','raw_bytes_verified':failure != 'fixed-bytes','structured_dialogs_verified':True} if fixed else {}), **({'map_declaration_verified':failure != 'map-browser','structured_dialogs_verified':True} if mapping else {}), **({'enum_declaration_verified':failure != 'enum-browser', 'structured_dialogs_verified':True} if enumeration else {}), **({'nested_values_verified':True,'structured_dialogs_verified':failure != 'nested-browser'} if nested else {}),
                        **({'original_download_disabled':True} if case['storage']=='nfs' else {'download_sha256':case['spec']['sha256'],'download_size':case['spec']['size_bytes']})})
            return {'schema_version':'addp.manager-avro-browser/v7','result':'passed','run_id':'manager-1-1',**outcomes}

        environment = {'ADDP_ONLINE_MANAGER_CASE': 'native-table-avro', 'ADDP_ONLINE_ARTIFACT_DIR': str(root),
            'GATEWAY_URL': 'http://fixture', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'consumer',
            'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'initializer', 'ADDP_ONLINE_TEST_TENANT_ID': '2',
            'ADDP_ONLINE_TEST_RUN_ID': 'manager-1-1', 'ADDP_ONLINE_MANAGER_MINIO_BUCKET': 'source',
            'ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID': '12', 'ADDP_ONLINE_MANAGER_NFS_ENGINE_ID': '14'}
        runtime_refusals = lambda repo, ids, tenant: [{'execution_id':key,'kind':'wrong' if failure=='runtime-refusal' else kind} for key,kind in ids.items()]
        with patch.object(SCENE.REGISTRATION, 'ExactFileMetadataGrants', return_value=grants):
            if failure:
                with self.assertRaises(ValueError):
                    SCENE.run(root, environment, helpers, physical_runner=physical, browser_runner=browser, runtime_refusal_runner=runtime_refusals)
            else:
                SCENE.run(root, environment, helpers, physical_runner=physical, browser_runner=browser, runtime_refusal_runner=runtime_refusals)
        return client, actions, json.loads((root / 'manager-avro.json').read_text())

    def test_complete_six_sources_and_batch_outputs(self):
        client, actions, report = self.run_scene()
        self.assertEqual(report['result'], 'passed')
        self.assertEqual(len(report['cases']), 6)
        self.assertEqual(len(report['nested_cases']), 6)
        self.assertEqual(len(report['enum_cases']), 6)
        self.assertEqual(len(report['map_cases']), 6)
        self.assertEqual(len(report['fixed_cases']),6)
        self.assertTrue(all(c['transfer']['rejection_reason']=='fixed_preview_only' for c in report['fixed_cases']))
        self.assertTrue(all(c['transfer']['rejection_reason']=='map_preview_only' for c in report['map_cases']))
        self.assertTrue(all(c['transfer']['rejection_reason']=='enum_preview_only' for c in report['enum_cases']))
        self.assertTrue(all(c['transfer']['preview_only_refusal_verified'] for c in report['nested_cases']))
        self.assertEqual(len(client.tasks), 30)
        self.assertEqual(len(report['physical']['outputs']), 6)
        self.assertEqual(client.deleted, list(reversed(client.tasks)))
        self.assertEqual(actions, ['seed', 'verify', 'delete'])
        self.assertEqual(client.assert_body['config']['source']['format'], 'avro')

    def test_failures_cannot_pass_and_always_clean_owned_sources_and_tasks(self):
        for failure in ('seed', 'page', 'download', 'transfer', 'physical', 'ticks', 'browser', 'cleanup', 'nested-meta', 'nested-private-facts', 'nested-page', 'nested-transfer', 'nested-reason', 'nested-count', 'nested-browser', 'nested-output', 'runtime-refusal', 'legacy-error', 'missing-count', 'enum-meta', 'enum-page', 'enum-browser', 'enum-output', 'map-meta', 'map-page', 'map-browser', 'map-output', 'fixed-meta', 'fixed-page', 'fixed-browser', 'fixed-bytes', 'fixed-output'):
            with self.subTest(failure=failure):
                client, actions, report = self.run_scene(failure)
                self.assertEqual(report['result'], 'failed')
                if failure == 'enum-meta': self.assertEqual(report['stage'], 'metadata_scan')
                self.assertEqual(actions[-1], 'delete')
                self.assertEqual(client.deleted, list(reversed(client.tasks)))

    def test_page_validation_rejects_wrong_total_and_binary_or_bigint_corruption(self):
        for key, value in [('id', 9007199254740992), ('payload', 'bad'), ('day', '1970-01-01'), ('amount', '0.000000'), ('fixed_amount', '0.000000'), ('stamp_ms','1970-01-01T00:00:00Z'), ('stamp_us','1970-01-01T00:00:00.000Z'), ('stamp_ns',9007199254740992)]:
            rows = [FIXTURE.public_row(i) for i in range(20, 40)]
            rows[0][key] = value
            with self.assertRaises(ValueError):
                SCENE.validate_page({'preview_type': 'table', 'data': {'rows': rows, 'total': 2503, 'has_more': True}}, 2, 20)

    def test_nested_page_validation_rejects_lost_structure_or_precision(self):
        for key, value in [('detail', '{}'), ('items', []), ('grid', [9007199254740992]), ('fixed_values', [None, 0.1])]:
            rows = [FIXTURE.nested_public_row(i) for i in range(20,40)]
            rows[1][key] = value
            with self.assertRaises(ValueError):
                SCENE.validate_page({'preview_type':'table','data':{'rows':rows,'total':2503,'has_more':True}},2,20,nested=True)

    def test_browser_nested_oracle_matches_all_fixture_values(self):
        source = (Path(__file__).resolve().parents[2] / 'console/frontend/e2e/online/manager-avro.spec.js').read_text()
        start = source.index('function expectedRow(')
        functions = source[start:source.index('\ntest(',start)]
        result = subprocess.run(['node','--input-type=commonjs','-e',functions+'\nconsole.log(JSON.stringify(Array.from({length:2503},(_,i)=>expectedNestedRow(i))))'],check=True,capture_output=True,text=True)
        self.assertEqual(json.loads(result.stdout),[FIXTURE.nested_public_row(i) for i in range(2503)])

    def test_browser_enum_oracle_matches_all_fixture_values(self):
        source = (Path(__file__).resolve().parents[2] / 'console/frontend/e2e/online/manager-avro.spec.js').read_text()
        start = source.index('function expectedRow(')
        functions = source[start:source.index('\ntest(',start)]
        result = subprocess.run(['node','--input-type=commonjs','-e',functions+'\nconsole.log(JSON.stringify(Array.from({length:2503},(_,i)=>expectedEnumRow(i))))'],check=True,capture_output=True,text=True)
        self.assertEqual(json.loads(result.stdout),[FIXTURE.enum_public_row(i) for i in range(2503)])

    def test_browser_map_oracle_matches_all_fixture_values(self):
        source = (Path(__file__).resolve().parents[2] / 'console/frontend/e2e/online/manager-avro.spec.js').read_text()
        start = source.index('function expectedRow(')
        functions = source[start:source.index('\ntest(',start)]
        result = subprocess.run(['node','--input-type=commonjs','-e',functions+'\nconsole.log(JSON.stringify(Array.from({length:2503},(_,i)=>expectedMapRow(i))))'],check=True,capture_output=True,text=True)
        self.assertEqual(json.loads(result.stdout),[FIXTURE.map_public_row(i) for i in range(2503)])

    def test_fixture_profile_and_unknown_case_rejected_before_launch(self):
        self.assertEqual(OWNER.manager_case({'ADDP_ONLINE_MANAGER_CASE': 'native-table-avro'}), 'native-table-avro')
        for action in ('seed', 'verify', 'delete', 'foreign'):
            with self.assertRaises(ValueError):
                FIXTURE.physical(action, Path('/tmp'), {})

    def test_browser_logical_oracle_matches_all_fixture_values(self):
        repository = Path(__file__).resolve().parents[2]
        source = (repository / 'console/frontend/e2e/online/manager-avro.spec.js').read_text()
        start = source.index('function expectedRow(')
        function = source[start:source.index('\ntest(', start)]
        script = function + "\nconsole.log(JSON.stringify(Array.from({length:2503},(_,i)=>expectedRow(i))))"
        completed = subprocess.run(['node', '--input-type=module', '-e', script], check=True, capture_output=True, text=True)
        self.assertEqual(json.loads(completed.stdout), [FIXTURE.public_row(i) for i in range(FIXTURE.COUNT)])

    def test_standard_avro_header_codec_and_repeatability(self):
        specs = FIXTURE.sources()
        self.assertEqual([s['codec'] for s in specs], ['null', 'deflate', 'snappy'])
        self.assertEqual(len({s['sha256'] for s in specs}), 3)
        for spec in specs:
            raw = FIXTURE.source_bytes(spec['codec'])
            self.assertTrue(raw.startswith(b'Obj\x01'))
            self.assertEqual(len(raw), spec['size_bytes'])
            self.assertEqual(hashlib.sha256(raw).hexdigest(), spec['sha256'])
