from importlib import import_module
from contextlib import contextmanager
import base64
import copy
import hashlib
import io
import json
import os
from pathlib import Path
import tempfile
import subprocess
import sys
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, unquote, urlsplit

M = import_module('scripts.test.ftp-consumer-flow-online')
GLB = import_module('scripts.test.manager-internal-artifact-lineage-online_test')


def preview():
    return {'preview_type': 'table', 'data': {'columns': ['name', 'amount', 'id'], 'total': 3, 'rows': copy.deepcopy(M.SEED.ROWS)}}


def model_samples():
    with tempfile.TemporaryDirectory() as directory:
        return M.SEED.write_model_samples(directory)


class FakeClient:
    def __init__(self):
        self.calls = []
        self.samples = [{'format': kind, 'full_name': 'examples/中文 %样例.' + kind, 'size': len(kind), 'sha256': hashlib.sha256(kind.encode()).hexdigest()} for kind in ('csv', 'json', 'parquet')]
        self.items = [{'id': i + 1, 'node_id': i + 10, 'item_type': 'file', 'full_name': sample['full_name'], 'fingerprint': 'fp-' + str(i), 'attributes': {'schema_version': 1, 'item': {'data_type': 'table'}, 'type_info': {'table': {'fields': [{'name': name} for name in ('id', 'amount', 'name')]}}}} for i, sample in enumerate(self.samples)]
        self.model_samples = model_samples()
        for i, sample in enumerate(self.model_samples[:2]):
            kind, path = sample['format'], sample['full_name']
            refs = [{'path': path, 'role': 'model', 'required': True, 'primary': True}]
            native = {'scan_complete': True}
            if kind == '3ds':
                refs.append({'path': 'models/3ds/texture.png', 'role': 'texture', 'required': True})
                native['texture_refs'] = ['texture.png']
            self.items.append({'id': 4 + i, 'engine_id': 17, 'item_type': 'file', 'full_name': path,
                               'fingerprint': hashlib.sha256(f'17:{path}'.encode()).hexdigest(),
                               'attributes': {'schema_version': 1,
                                              'item': {'data_type': 'model_3d', 'layout': 'single', 'format': kind, 'refs': refs},
                                              'storage': {'physical_path': path}, 'format_info': {kind: native}}})
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
        elif url.path == '/api/v1/manager/engines':
            payload = {'data': [{'id': 17}]}
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
        return SimpleNamespace(status=200, payload=payload)


class FakeSourceIssuer:
    def __init__(self, report):
        self.report = report
        self.calls = []
        self.principal = '9'
        self.grants = {}
        self.denies = set()
        self.unknown_write = False
        self.mutate_facts = None

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, body))
        if path.endswith('/auth/context'):
            return SimpleNamespace(payload={'principal': {'id': self.principal, 'type': 'user'},
                'context': {'type': 'tenant', 'tenant_id': '2'}, 'token': {'type': 'first_party_access_token'},
                'authorization': {'role_assignments': [{'role_key': 'online.ftp_initializer', 'permissions': sorted(M.ISSUER_PERMISSIONS)}]}})
        if path.endswith('/authorization-facts'):
            model = next(model for model in self.report['models'] if model['fingerprint'] in path)
            composition = {'format': model['format'], 'refs': [
                {'target': M.file_target(17, ref['path']), 'role': ref['role'], 'required': ref['required'], 'primary': ref['primary']}
                for ref in model['composition']]}
            result = {'schema_version': 'meta.data_item_authorization_facts/v1', 'engine_id': '17',
                      'item_fingerprint': model['fingerprint'], 'full_name': model['full_name'], 'item_type': 'file',
                      'basis_state': 'complete', 'boundary_hash': 'a' * 64, 'observed_at': '2026-10-20T12:00:00Z',
                      'boundary': {'layout': 'single', 'file_composition': composition}}
            if self.mutate_facts:
                self.mutate_facts(result)
            return SimpleNamespace(payload=result)
        if path.endswith('/access_grants'):
            if self.unknown_write:
                raise M.SuiteError('unknown synthetic write outcome')
            if any(all(existing[key] == body[key] for key in ('item_fingerprint', 'recipient_type', 'recipient_id', 'action'))
                   for existing in self.grants.values()):
                raise M.SuiteError('engine_access_grant_relation_exists')
            model = next(model for model in self.report['models'] if model['fingerprint'] == body['item_fingerprint'])
            composition = {'format': model['format'], 'refs': [
                {'target': M.file_target(17, ref['path']), 'role': ref['role'], 'required': ref['required'], 'primary': ref['primary']}
                for ref in model['composition']]}
            self.grants[body['request_id']] = copy.deepcopy(body)
            return SimpleNamespace(payload={**body, 'engine_id': '17', 'approval_mode': 'independent',
                                           'boundary_hash': body['expected_boundary_hash'], 'file_composition': composition,
                                           'expires_at': None, 'operator_principal_id': self.principal, 'revocation': None})
        if path.endswith('/revoke'):
            command = path.split('/')[-2]
            self.revoke_statuses = expected
            status = 200 if self.grants.pop(command, None) is not None else 404
            return SimpleNamespace(status=status, payload={'request_id': command})
        if path.endswith('/access_denies'):
            self.denies.add(body['deny_id'])
            return SimpleNamespace(payload={**body, 'engine_id': '17', 'established_by_principal_id': self.principal})
        if path.endswith('/release'):
            command = path.split('/')[-2]
            self.release_statuses = expected
            return SimpleNamespace(status=200 if command in self.denies else 404, payload={'deny_id': command})
        raise AssertionError(path)


class FTPConsumerTest(unittest.TestCase):
    def owned_detail(self, kind='3ds'):
        model, = [row for row in M.prepare_models(FakeClient().items, 17, model_samples()) if row['format'] == kind]
        execution = '12345678-1234-4234-8234-123456789abc'
        preparation = 'abcdefab-1234-4234-8234-123456789abc'
        prefix = f'temp/model3d-input/tenant_2/{execution}/attempt_1/{preparation}/'
        published = {'format': kind, 'task_id': 30, 'result_id': 20, 'execution_id': execution, 'size': 123, 'sha256': 'a' * 64}
        detail = {'id': 20, 'tenant_id': 2, 'task_id': 30, 'last_execution_id': execution, 'item_id': model['item_id'],
                  'item_fingerprint': model['fingerprint'], 'source_engine_id': 17, 'source_format': kind,
                  'size_bytes': 123, 'status': 'ready', 'storage_ref': json.dumps({'type': 'object', 'provider': 'addp_object_storage',
                  'bucket': 'manager', 'object': f'tenant_2/model3d-quick-view/executions/{execution}/model.glb'}),
                  'metadata': {'model3d_output_owner': 'manager_execution', 'model3d_output_lifecycle': {
                      'input_preparations': [{'execution_id': execution, 'attempt': 1, 'preparation_id': preparation,
                          'released': True, 'watch': False, 'cleaned': True,
                          'objects': [{'bucket': 'manager', 'object': prefix + name} for name in
                                      (('model.3ds', 'texture.png') if kind == '3ds' else ('model.ifc',))]}]}}}
        return model, published, detail

    def failed_detail(self, detail, published):
        failed = 'fedcba98-1234-4234-8234-123456789abc'
        changed = copy.deepcopy(detail)
        state = changed['metadata']['model3d_output_lifecycle']
        row = copy.deepcopy(state['input_preparations'][0])
        old_preparation = row['preparation_id']
        row.update(execution_id=failed, preparation_id='ff' + old_preparation[2:])
        for obj in row['objects']:
            obj['object'] = obj['object'].replace(published['execution_id'], failed).replace(old_preparation, row['preparation_id'])
        state['input_preparations'].append(row)
        ref = json.loads(detail['storage_ref'])
        ref['object'] = ref['object'].replace(published['execution_id'], failed)
        state['garbage'] = [{'execution_id': failed, 'watch': True, 'storage_ref': json.dumps(ref)}]
        return failed, changed

    def test_exact_owner_details_and_settled_inputs_reject_foreign_or_unknown_cleanup(self):
        model, published, detail = self.owned_detail()
        client = SimpleNamespace(request=lambda *args: SimpleNamespace(payload=detail))
        report = {'engine_id': 17, 'tenant_id': 2}
        self.assertIs(M.owned_model_details(client, report, model, published), detail)
        inputs = M.owned_input_objects(detail, published['execution_id'], model['composition'])
        self.assertEqual(len(inputs), 2)
        self.assertTrue(all(row['exists'] is False for row in inputs))
        mutations = [lambda value: value.update(tenant_id=3), lambda value: value.update(task_id=31),
                     lambda value: value.update(last_execution_id='other'), lambda value: value.update(item_fingerprint='b' * 64)]
        for mutate in mutations:
            changed = copy.deepcopy(detail)
            mutate(changed)
            with self.assertRaises(M.SuiteError):
                M.owned_model_details(SimpleNamespace(request=lambda *args: SimpleNamespace(payload=changed)), report, model, published)
        for key, value in (('released', False), ('watch', True), ('cleaned', False), ('attempt', True), ('objects', [])):
            changed = copy.deepcopy(detail)
            changed['metadata']['model3d_output_lifecycle']['input_preparations'][0][key] = value
            with self.subTest(key=key), self.assertRaises(M.SuiteError):
                M.owned_input_objects(changed, published['execution_id'], model['composition'])
        changed = copy.deepcopy(detail)
        changed['metadata']['model3d_output_lifecycle']['input_preparations'][0]['objects'][1]['object'] += '-foreign'
        with self.assertRaises(M.SuiteError):
            M.owned_input_objects(changed, published['execution_id'], model['composition'])

    def test_physical_probe_uses_observed_port_and_private_stdin_without_object_enumeration(self):
        objects = [{'bucket': 'manager', 'object': 'temp/exact.png', 'exists': False}]
        invocations = []
        def command(args, **kwargs):
            invocations.append((args, kwargs))
            if args[0] == 'bash':
                return SimpleNamespace(returncode=0)
            return SimpleNamespace(returncode=0, stdout=json.dumps(objects))
        def value(args, **kwargs):
            if 'port' in args:
                return '127.0.0.1:49123\n'
            return json.dumps({'com.docker.compose.project': 'addp-infra', 'com.docker.compose.service': 'minio'})
        with patch.dict(os.environ, MINIO_ROOT_USER='synthetic-key', MINIO_ROOT_PASSWORD='synthetic-secret'), \
                patch.object(M, 'model3d_product_identity', return_value={'container': 'addp-ftp-model3d-runtime'}), \
                patch.object(M.subprocess, 'run', side_effect=command), patch.object(M.subprocess, 'check_output', side_effect=value):
            self.assertEqual(M.verify_physical_objects(objects), objects)
        args, kwargs = invocations[-1]
        self.assertEqual(args[:5], ['docker', 'exec', '-i', 'addp-ftp-model3d-runtime', 'python'])
        payload = json.loads(kwargs['input'])
        self.assertEqual(payload['endpoint'], '127.0.0.1:49123')
        self.assertEqual(payload['objects'], objects)
        self.assertNotIn('synthetic-secret', ' '.join(args))
        compile(M.OBJECT_PROBE, '<exact object probe>', 'exec')

    def test_physical_probe_counts_only_exact_no_such_key_as_absent(self):
        class S3Failure(Exception):
            def __init__(self, code):
                self.code = code
        for code in ('NoSuchKey', 'AccessDenied', 'NoSuchBucket', 'transport-timeout'):
            client = SimpleNamespace(stat_object=lambda *args: None)
            def fail(*args):
                if code == 'transport-timeout':
                    raise TimeoutError('synthetic transport')
                raise S3Failure(code)
            client.stat_object = fail
            modules = {'minio': SimpleNamespace(Minio=lambda *args, **kwargs: client),
                       'minio.error': SimpleNamespace(S3Error=S3Failure),
                       'urllib3': SimpleNamespace(PoolManager=lambda **kwargs: None, Timeout=lambda **kwargs: None)}
            payload = {'endpoint': '127.0.0.1:49123', 'access_key': 'synthetic', 'secret_key': 'synthetic',
                       'objects': [{'bucket': 'manager', 'object': 'exact', 'exists': False}]}
            output, errors = io.StringIO(), io.StringIO()
            with self.subTest(code=code), patch.dict(sys.modules, modules), \
                    patch.object(sys, 'stdin', io.StringIO(json.dumps(payload))), \
                    patch.object(sys, 'stdout', output), patch.object(sys, 'stderr', errors):
                if code == 'NoSuchKey':
                    exec(M.OBJECT_PROBE, {})
                else:
                    with self.assertRaises(SystemExit) as failed:
                        exec(M.OBJECT_PROBE, {})
                    self.assertEqual(failed.exception.code, 1)
            if code == 'NoSuchKey':
                self.assertEqual(json.loads(output.getvalue()), payload['objects'])
            else:
                self.assertEqual(output.getvalue(), '')
                self.assertNotIn('synthetic transport', errors.getvalue())

    def test_product_delete_uses_original_owned_result_after_source_withdrawal(self):
        model, published, detail = self.owned_detail()
        report = {'models': [model], 'browser': {'models': [published]}, 'engine_id': 17, 'tenant_id': 2}
        calls = []
        def request(method, path, statuses, body=None):
            calls.append((method, path, statuses, body))
            return SimpleNamespace(payload=detail if statuses == (200,) and method == 'GET' else {})
        with patch.object(M, 'verify_physical_objects', side_effect=lambda rows: rows) as physical:
            result = M.delete_owned_models(SimpleNamespace(request=request), report)
        self.assertEqual(calls, [('GET', '/api/v1/manager/model_3d_glb/20', (200,), None),
                                 ('DELETE', '/api/v1/manager/model_3d_glb/20', (200,), None),
                                 ('GET', '/api/v1/manager/model_3d_glb/20', (404,), None),
                                 ('GET', '/api/v1/manager/model_3d_glb/20/content', (404,), None)])
        self.assertEqual(len(physical.call_args.args[0]), 3)
        self.assertTrue(all(row['exists'] is False for row in result[0]['objects']))

    def test_product_delete_keeps_failed_inputs_and_output_watch_in_exact_post_delete_probe(self):
        model, published, detail = self.owned_detail('ifc')
        failed, detail = self.failed_detail(detail, published)
        report = {'models': [model], 'browser': {'models': [published]}, 'engine_id': 17, 'tenant_id': 2,
                  'failed_generation': {'format': 'ifc', 'execution_id': failed}}
        client = SimpleNamespace(request=lambda *args: SimpleNamespace(payload=detail))
        with patch.object(M, 'verify_physical_objects', side_effect=lambda rows: rows) as physical:
            M.delete_owned_models(client, report)
        self.assertEqual(len(physical.call_args.args[0]), 4)
        self.assertEqual(sum(failed in row['object'] for row in physical.call_args.args[0]), 2)

    def test_invalid_ifc_fixture_restores_equal_size_source_on_consumer_failure(self):
        model, _, _ = self.owned_detail('ifc')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            M.SEED.write_model_samples(root / 'data')
            target = root / 'data/models/ifc/model.ifc'
            original = target.read_bytes()
            with patch.object(M, 'owned_ftp_fixture', return_value=root), self.assertRaisesRegex(RuntimeError, 'synthetic consumer'):
                with M.invalid_ifc_fixture(model):
                    self.assertEqual(len(target.read_bytes()), len(original))
                    self.assertNotEqual(target.read_bytes(), original)
                    raise RuntimeError('synthetic consumer')
            self.assertEqual(target.read_bytes(), original)

    def test_failed_rebuild_uses_formal_execute_and_preserves_old_ready_with_new_input_facts(self):
        model, published, before = self.owned_detail('ifc')
        failed, after = self.failed_detail(before, published)
        report = {'models': [model], 'browser': {'models': [published]}, 'engine_id': 17, 'tenant_id': 2}
        calls, events = [], []
        submitted = False
        def request(method, path, statuses, body=None):
            nonlocal submitted
            calls.append((method, path, statuses, body))
            if method == 'POST':
                submitted = True
                return SimpleNamespace(payload={'execution_id': failed, 'status': 'pending'})
            if '/executions/' in path:
                return SimpleNamespace(payload={'execution_id': failed, 'status': 'failed'})
            return SimpleNamespace(payload=after if submitted else before)
        @contextmanager
        def invalid(source):
            events.append('invalid')
            try:
                yield
            finally:
                events.append('restored')
        with patch.object(M, 'invalid_ifc_fixture', invalid), patch.object(M, 'verify_physical_objects', side_effect=lambda rows: rows) as physical, \
                patch.object(M, 'verify_cached_results', return_value=[{'status': 200}]):
            result = M.verify_failed_generation(SimpleNamespace(request=request), report, 30)
        self.assertEqual(events, ['invalid', 'restored'])
        self.assertEqual([row for row in calls if row[0] == 'POST'], [
            ('POST', '/api/v1/manager/tasks/model_3d_glb_generation/30/execute', (202,),
             {'parameters': {'existing_result_action': 'overwrite'}})])
        self.assertEqual(result['preserved_execution_id'], published['execution_id'])
        self.assertEqual(result['execution_id'], failed)
        self.assertEqual(len(physical.call_args.args[0]), 3)
        self.assertEqual(sum(row['exists'] is True for row in result['objects']), 1)
        self.assertEqual(sum(failed in row['object'] for row in result['objects']), 2)

    def product_profile(self):
        raw = GLB.fixture_glb('ifc')
        return {'platform': 'Linux-x86_64', 'binary': '/opt/addp/model3d-workflow/bin/IfcConvert',
                'binary_version': 'IfcConvert 0.8.4-e8eb5e4', 'binary_sha256': 'a' * 64,
                'operator_sha256': hashlib.sha256((M.Path(__file__).resolve().parents[2] / 'engines/model3d-workflow/operators.py').read_bytes()).hexdigest(),
                'temporary_root_removed': True, 'sentinel_thread_stopped': True,
                'cases': [{'reference_kind': kind, 'center_model': True, 'http_connections': 0, 'http_methods': [],
                           'images': 0, 'textures': 0, 'size': len(raw), 'sha256': hashlib.sha256(raw).hexdigest(),
                           'glb_b64': base64.b64encode(raw).decode()} for kind in ('http', 'fifo')]}

    def product_identity(self, args, **kwargs):
        expression = args[-2]
        return {'{{index .Config.Labels "com.addp.online-runtime"}}': 'ftp-consumer-flow',
                '{{index .Config.Labels "com.addp.online-run-id"}}': 'owned-run', '{{.Image}}': 'sha256:owned',
                '{{.Id}}': 'sha256:owned', '{{.Os}}/{{.Architecture}}': 'linux/amd64',
                '{{json .Config.Cmd}}': '["python","api_server.py"]', '{{.State.Running}}': 'true',
                'rev-parse': 'synthetic-commit'}[expression]

    def test_linux_product_profile_uses_exact_owned_runtime_and_physical_glb_oracle(self):
        with tempfile.TemporaryDirectory() as directory, \
                patch.dict(os.environ, ADDP_ONLINE_TEST='1', ADDP_ONLINE_HOSTED='1', ADDP_ONLINE_TEST_RUN_ID='owned-run'), \
                patch.object(M.subprocess, 'check_output', side_effect=self.product_identity), \
                patch.object(M.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout=json.dumps(self.product_profile()))) as invoke:
            report = M.verify_ifc_product_profile(Path(__file__).resolve().parents[2], Path(directory))
            call = invoke.call_args
            self.assertEqual(call.args[0][:7], ['docker', 'exec', '-i', '--workdir', '/app', 'addp-ftp-model3d-runtime', 'python'])
            self.assertEqual(call.args[0][-1], M.IFC_PRODUCT_PROFILE_PROBE)
            self.assertEqual(set(json.loads(call.kwargs['input'])), {'ifc_b64', 'png_b64'})
            self.assertEqual(call.kwargs['timeout'], 60)
            self.assertNotEqual(report['binary_sha256'], report['converter_archive_sha256'])
            self.assertEqual([case['physical_glb']['vertex_count'] for case in report['cases']], [24, 24])
            self.assertTrue(all('glb_b64' not in case for case in report['cases']))
            self.assertEqual(len(list(Path(directory).glob('*.glb'))), 2)
            compile(M.IFC_PRODUCT_PROFILE_PROBE, '<IFC product probe>', 'exec')

    def test_linux_product_profile_rejects_wrong_owner_platform_image_entry_before_exec(self):
        expressions = ['{{index .Config.Labels "com.addp.online-runtime"}}',
                       '{{index .Config.Labels "com.addp.online-run-id"}}', '{{.Image}}',
                       '{{.Os}}/{{.Architecture}}', '{{json .Config.Cmd}}', '{{.State.Running}}']
        for expression in expressions:
            def identity(args, **kwargs):
                return ('["wrong"]' if expression == '{{json .Config.Cmd}}' else 'wrong') if args[-2] == expression else self.product_identity(args, **kwargs)
            with self.subTest(expression=expression), tempfile.TemporaryDirectory() as directory, \
                    patch.dict(os.environ, ADDP_ONLINE_TEST='1', ADDP_ONLINE_HOSTED='1', ADDP_ONLINE_TEST_RUN_ID='owned-run'), \
                    patch.object(M.subprocess, 'check_output', side_effect=identity), patch.object(M.subprocess, 'run') as invoke, \
                    self.assertRaises(M.SuiteError):
                M.verify_ifc_product_profile(Path(__file__).resolve().parents[2], Path(directory))
            invoke.assert_not_called()

    def test_linux_product_profile_fails_native_timeout_and_wrong_external_or_physical_report(self):
        mutations = [lambda report: report['cases'][0].update(http_connections=1),
                     lambda report: report['cases'][0].update(http_methods=['HEAD']),
                     lambda report: report['cases'][0].update(images=1),
                     lambda report: report['cases'][0].update(textures=1),
                     lambda report: report.update(temporary_root_removed=False),
                     lambda report: report.update(sentinel_thread_stopped=False),
                     lambda report: report.update(operator_sha256='b' * 64),
                     lambda report: report['cases'].pop(),
                     lambda report: report['cases'][0].update(sha256='b' * 64)]
        for mutate in mutations:
            report = self.product_profile()
            mutate(report)
            with tempfile.TemporaryDirectory() as directory, \
                    patch.dict(os.environ, ADDP_ONLINE_TEST='1', ADDP_ONLINE_HOSTED='1', ADDP_ONLINE_TEST_RUN_ID='owned-run'), \
                    patch.object(M.subprocess, 'check_output', side_effect=self.product_identity), \
                    patch.object(M.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout=json.dumps(report))), \
                    self.assertRaises(M.SuiteError):
                M.verify_ifc_product_profile(Path(__file__).resolve().parents[2], Path(directory))
        for fault in ('timeout', 'nonzero'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory, \
                    patch.dict(os.environ, ADDP_ONLINE_TEST='1', ADDP_ONLINE_HOSTED='1', ADDP_ONLINE_TEST_RUN_ID='owned-run'), \
                    patch.object(M.subprocess, 'check_output', side_effect=self.product_identity), \
                    patch.object(M.subprocess, 'run', side_effect=subprocess.TimeoutExpired('docker exec', 60) if fault == 'timeout' else None,
                                 return_value=SimpleNamespace(returncode=1, stderr='bounded synthetic failure')), \
                    self.assertRaises(M.SuiteError):
                M.verify_ifc_product_profile(Path(__file__).resolve().parents[2], Path(directory))

    def test_exact_catalog_scan_preview_and_download(self):
        client = FakeClient()
        with patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='owned-run'), patch.object(M, 'scan_scope', return_value='scan-owned') as scan:
            report = M.run(client, 2, 17, client.samples, client.model_samples, 30)
        self.assertEqual(len(report['samples']), 3)
        self.assertEqual(report['principal_id'], '8')
        self.assertEqual([model['format'] for model in report['models']], ['ifc', '3ds'])
        self.assertEqual(len([call for call in client.calls if '/downloads/file?' in call[1]]), 3)
        self.assertTrue(all('%25' in sample['locator'] and '&item_id=' in sample['locator'] for sample in report['samples']))
        self.assertEqual(scan.call_args_list[0].args, (client, 17, ['models'], 'basic', 30))
        self.assertEqual(scan.call_args_list[1].args, (client, 17, ['examples'], 'deep', 30))

    def test_model_scope_scan_uses_force_basic_and_rejects_wrong_completion_depth(self):
        identifier = '12345678-1234-4234-8234-123456789abc'
        for observed_depth in ('basic', 'deep'):
            calls = []
            def request(method, path, statuses, body=None):
                calls.append((method, path, statuses, body))
                return SimpleNamespace(payload={'execution_id': identifier} if method == 'POST' else
                                       {'execution_id': identifier, 'status': 'success', 'scan_context': {'scan_depth': observed_depth}})
            client = SimpleNamespace(request=request)
            if observed_depth == 'basic':
                self.assertEqual(M.scan_scope(client, 17, ['models/3ds'], 'basic', 30), identifier)
            else:
                with self.assertRaises(M.SuiteError):
                    M.scan_scope(client, 17, ['models/3ds'], 'basic', 30)
            self.assertEqual(calls[0], ('POST', '/api/v1/meta/scan/run/manual', (201,), {
                'engine_id': 17, 'catalog_paths': ['models/3ds'], 'scan_depth': 'basic', 'trigger_type': 'manual', 'force': True}))

    def test_rejects_admin_write_capability_and_corrupt_download(self):
        for fault in ('admin', 'write', 'bad_download'):
            with self.subTest(fault=fault):
                client = FakeClient()
                setattr(client, fault, True)
                with patch.object(M, 'scan_scope', return_value='scan'), self.assertRaises((M.SuiteError, M.SUPPORT.SuiteError)):
                    M.run(client, 2, 17, client.samples, client.model_samples, 30)

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
        client = FakeClient()
        expected['models'] = M.prepare_models(client.items, 17, client.model_samples)
        proof = {key: expected[key] for key in ('run_id', 'engine_id', 'tenant_id', 'principal_id')}
        proof.update(meta_ui_scan=True, previews=3, downloads=copy.deepcopy(expected['samples']))
        proof['models'] = []
        with tempfile.TemporaryDirectory() as directory:
            artifacts = Path(directory)
            for source in expected['models']:
                raw = GLB.fixture_glb(source['format'])
                (artifacts / f"ftp-{source['format']}.glb").write_bytes(raw)
                proof['models'].append({'format': source['format'], 'item_id': source['item_id'],
                                        'task_id': 10, 'execution_id': 'owned-execution', 'result_id': 20,
                                        'size': len(raw), 'sha256': hashlib.sha256(raw).hexdigest(),
                                        'source_sha256': source['sha256'], 'rendered': True,
                                        'reopened': True, 'cached_without_execution': True})
            M.validate_browser_report(proof, expected, artifacts)
            for key, value in (('principal_id', 'another'), ('meta_ui_scan', False), ('downloads', []), ('previews', 2), ('models', [])):
                changed = {**proof, key: value}
                with self.assertRaises(M.SuiteError):
                    M.validate_browser_report(changed, expected, artifacts)
            for field, value in (('item_id', 99), ('reopened', False), ('cached_without_execution', False), ('source_sha256', 'b' * 64)):
                changed = copy.deepcopy(proof)
                changed['models'][0][field] = value
                with self.assertRaises(M.SuiteError):
                    M.validate_browser_report(changed, expected, artifacts)
            # Even a matching report checksum cannot turn arbitrary bytes or a
            # different texture into a physically verified model.
            for raw in (b'glTFinvalid', GLB.fixture_glb('3ds', texture_png=b'not-a-png')):
                changed = copy.deepcopy(proof)
                (artifacts / 'ftp-3ds.glb').write_bytes(raw)
                changed['models'][1].update(size=len(raw), sha256=hashlib.sha256(raw).hexdigest())
                with self.assertRaises(M.SuiteError):
                    M.validate_browser_report(changed, expected, artifacts)

    def test_model_metadata_rejects_incomplete_expanded_or_changed_composition(self):
        mutations = [lambda item: item['attributes']['format_info']['3ds'].update(scan_complete=False),
                     lambda item: item['attributes']['format_info']['3ds'].update(texture_refs=['TEXTURE.PNG']),
                     lambda item: item['attributes']['item']['refs'].pop(),
                     lambda item: item['attributes']['item']['refs'].append({'path': 'models/3ds/other.png', 'role': 'texture_2', 'required': True}),
                     lambda item: item['attributes']['item']['refs'][1].update(path='models/other/texture.png'),
                     lambda item: item['attributes']['item'].update(layout='multi'),
                     lambda item: item['attributes']['storage'].update(physical_path='models/wrong.3ds'),
                     lambda item: item.update(engine_id=18),
                     lambda item: item.update(fingerprint='a' * 64),
                     lambda item: item['attributes']['item']['refs'][1].update(required=1)]
        for mutate in mutations:
            client = FakeClient()
            mutate(client.items[-1])
            with self.assertRaises(M.SuiteError):
                M.prepare_models(client.items, 17, client.model_samples)
        client = FakeClient()
        for samples in (client.model_samples[:2], client.model_samples + client.model_samples[:1]):
            with self.assertRaises(M.SuiteError):
                M.prepare_models(client.items, 17, samples)
        changed = copy.deepcopy(client.model_samples)
        changed[0]['sha256'] = 'a' * 64
        with self.assertRaises(M.SuiteError):
            M.prepare_models(client.items, 17, changed)

    def test_browser_process_does_not_inherit_preparer_credentials(self):
        with tempfile.TemporaryDirectory() as directory:
            observations = {}

            def browser_command(args, *, cwd, env):
                observations['engine_credential'] = 'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN' in env
                observations['source_credential'] = 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN' in env
                observations['consumer_credential'] = env.get('ADDP_ONLINE_TEST_USER_ACCESS_TOKEN') == 'consumer-test-token'
                Path(env['ADDP_ONLINE_FTP_BROWSER_REPORT']).write_text('{}')
                return SimpleNamespace(returncode=0)

            with patch.dict(os.environ, ADDP_ONLINE_ARTIFACT_DIR=directory,
                            ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN='engine-test-token',
                            ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='source-test-token',
                            ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer-test-token'), \
                    patch.object(M.subprocess, 'run', side_effect=browser_command), \
                    patch.object(M, 'validate_browser_report') as validate:
                M.run_browser({'run_id': 'owned-run'})
            self.assertFalse(observations['engine_credential'])
            self.assertFalse(observations['source_credential'])
            self.assertTrue(observations['consumer_credential'])
            validate.assert_called_once_with({}, {'run_id': 'owned-run'}, Path(directory))

    def test_model_grants_use_distinct_user_and_server_snapshot_then_revoke_exact_commands(self):
        consumer = FakeClient()
        report = {'engine_id': 17, 'tenant_id': 2, 'principal_id': '8',
                  'models': M.prepare_models(consumer.items, 17, consumer.model_samples)}
        issuer = FakeSourceIssuer(report)
        authorization = M.ModelSourceAuthorization(issuer, consumer, report)
        receipts = [authorization.grant(model) for model in report['models']]
        self.assertEqual(len(receipts), 2)
        commands = [body for method, path, body in issuer.calls if path.endswith('/access_grants')]
        self.assertEqual([body['recipient_id'] for body in commands], ['8', '8'])
        for body in commands:
            self.assertIs(body['initialize_approval'], True)
            self.assertEqual(body['catalog_path']['segments'][-1]['term'], 'file')
            self.assertNotIn('file_composition', body)
            self.assertNotIn('actor_principal_id', body)
        with self.assertRaises(M.SuiteError):
            authorization.revoke('unowned-command')
        deny = authorization.deny(report['models'][1])
        deny_body = next(body for _, path, body in issuer.calls if path.endswith('/access_denies'))
        self.assertNotIn('expected_boundary_hash', deny_body)
        self.assertNotIn('file_composition', deny_body)
        self.assertEqual(deny_body['item_fingerprint'], report['models'][1]['fingerprint'])
        with self.assertRaises(M.SuiteError):
            authorization.release('unowned-deny')
        authorization.release(deny)
        self.assertEqual(issuer.release_statuses, (200,))
        authorization.revoke(receipts[1]['request_id'])
        receipts.append(authorization.grant(report['models'][1], initialize=False))
        self.assertIs([body for _, path, body in issuer.calls if path.endswith('/access_grants')][-1]['initialize_approval'], False)
        authorization.cleanup()
        self.assertEqual(authorization.grants, [])
        self.assertEqual(issuer.revoke_statuses, (200,))
        self.assertEqual({path.split('/')[-2] for _, path, _ in issuer.calls if path.endswith('/revoke')},
                         {row['request_id'] for row in receipts})

    def test_model_grants_reject_incomplete_or_expanded_system_facts_before_any_write(self):
        mutations = [lambda facts: facts.update(basis_state='incomplete'),
                     lambda facts: facts.update(item_fingerprint='b' * 64),
                     lambda facts: facts.update(boundary_hash='invalid'),
                     lambda facts: facts['boundary']['file_composition']['refs'].pop(),
                     lambda facts: facts['boundary'].update(native_target=M.file_target(17, 'models/ifc/model.ifc'))]
        for mutate in mutations:
            consumer = FakeClient()
            report = {'engine_id': 17, 'tenant_id': 2, 'principal_id': '8',
                      'models': M.prepare_models(consumer.items, 17, consumer.model_samples)}
            issuer = FakeSourceIssuer(report)
            issuer.mutate_facts = mutate
            authorization = M.ModelSourceAuthorization(issuer, consumer, report)
            with self.assertRaises(M.SuiteError):
                authorization.grant(report['models'][1])
            self.assertFalse(any(path.endswith('/access_grants') for _, path, _ in issuer.calls))
        issuer = FakeSourceIssuer(report)
        issuer.principal = '8'
        with self.assertRaises(M.SuiteError):
            M.ModelSourceAuthorization(issuer, consumer, report)

    def test_grant_preparation_respects_one_active_stable_relation(self):
        consumer = FakeClient()
        report = {'engine_id': 17, 'tenant_id': 2, 'principal_id': '8',
                  'models': M.prepare_models(consumer.items, 17, consumer.model_samples)}
        issuer = FakeSourceIssuer(report)
        authorization = M.ModelSourceAuthorization(issuer, consumer, report)
        first = authorization.grant(report['models'][1])
        with self.assertRaisesRegex(M.SuiteError, 'engine_access_grant_relation_exists'):
            authorization.grant(report['models'][1], initialize=False)
        authorization.revoke(first['request_id'])
        second = authorization.grant(report['models'][1], initialize=False)
        self.assertNotEqual(first['request_id'], second['request_id'])
        self.assertEqual(len(issuer.grants), 1)
        authorization.cleanup()
        self.assertEqual(issuer.grants, {})

    def test_unknown_model_grant_write_is_tracked_and_cleaned_by_its_original_command(self):
        consumer = FakeClient()
        report = {'engine_id': 17, 'tenant_id': 2, 'principal_id': '8',
                  'models': M.prepare_models(consumer.items, 17, consumer.model_samples)}
        issuer = FakeSourceIssuer(report)
        issuer.unknown_write = True
        authorization = M.ModelSourceAuthorization(issuer, consumer, report)
        with self.assertRaises(M.SuiteError):
            authorization.grant(report['models'][0])
        self.assertEqual(len(authorization.grants), 1)
        original = authorization.grants[0]
        authorization.cleanup()
        self.assertEqual(authorization.grants, [])
        self.assertEqual(issuer.revoke_statuses, (200, 404))
        self.assertTrue(any(path.endswith('/' + original + '/revoke') for _, path, _ in issuer.calls))

    def test_withdrawal_checks_actual_cached_content_with_same_qualified_user(self):
        consumer = FakeClient()
        report = {'tenant_id': 2, 'principal_id': '8', 'browser': {'models': [
            {'format': 'ifc', 'result_id': 20}, {'format': '3ds', 'result_id': 21}]}}
        original = consumer.request
        requests = []

        def request(method, path, expected, body=None, *, raw_response=False):
            if '/model_3d_glb/' in path:
                requests.append((method, path, expected))
                return SimpleNamespace(payload={'error': 'source denied'})
            return original(method, path, expected, body)

        with patch.object(consumer, 'request', side_effect=request):
            self.assertEqual(M.verify_cached_results(consumer, report, {'ifc': 403, '3ds': 403}), [
                {'format': 'ifc', 'result_id': 20, 'status': 403},
                {'format': '3ds', 'result_id': 21, 'status': 403}])
        self.assertEqual(requests, [('GET', '/api/v1/manager/model_3d_glb/20/content', (403,)),
                                    ('GET', '/api/v1/manager/model_3d_glb/21/content', (403,))])
        changed = copy.deepcopy(report)
        changed['principal_id'] = '9'
        with self.assertRaises(M.SuiteError):
            M.verify_cached_results(consumer, changed, {'ifc': 403, '3ds': 403})

    def test_source_withdrawal_does_not_count_security_failure_or_leaked_data_as_source_denial(self):
        consumer = FakeClient()
        original = consumer.request
        report = {'tenant_id': 2, 'principal_id': '8', 'browser': {'models': [{'format': 'ifc', 'result_id': 20}]}}
        for payload in ({'error': 'protection pending', 'error_code': 'security_protection_required'},
                        {'error': 'source denied', 'data': {'private': 'unexpected'}}):
            def request(method, path, statuses, body=None, **kwargs):
                return SimpleNamespace(payload=payload) if '/model_3d_glb/' in path else original(method, path, statuses, body)
            with patch.object(consumer, 'request', side_effect=request), self.assertRaises(M.SuiteError):
                M.verify_cached_results(consumer, report, {'ifc': 403})

    def test_deny_control_keeps_other_model_readable_and_release_restores_exact_bytes(self):
        consumer = FakeClient()
        binaries = {kind: GLB.fixture_glb(kind) for kind in ('ifc', '3ds')}
        models = [{'format': kind, 'result_id': 20 + i, 'size': len(binaries[kind]),
                   'sha256': hashlib.sha256(binaries[kind]).hexdigest()} for i, kind in enumerate(('ifc', '3ds'))]
        report = {'tenant_id': 2, 'principal_id': '8', 'browser': {'models': models}}
        original = consumer.request

        def request(method, path, expected, body=None, *, raw_response=False):
            if '/model_3d_glb/' in path:
                kind = 'ifc' if '/20/' in path else '3ds'
                return SimpleNamespace(payload=binaries[kind] if raw_response else {'error': 'source denied'})
            return original(method, path, expected, body)

        with patch.object(consumer, 'request', side_effect=request):
            self.assertEqual([row['status'] for row in M.verify_cached_results(consumer, report, {'ifc': 200, '3ds': 403})], [200, 403])
            self.assertEqual([row['status'] for row in M.verify_cached_results(consumer, report, {'ifc': 200, '3ds': 200})], [200, 200])
            binaries['3ds'] = b'changed'
            with self.assertRaises(M.SuiteError):
                M.verify_cached_results(consumer, report, {'ifc': 200, '3ds': 200})

    def test_composition_fixture_changes_only_owned_declared_file_and_restores_on_failure(self):
        client = FakeClient()
        model = M.prepare_models(client.items, 17, client.model_samples)[1]
        class ExpectedConsumerFailure(Exception):
            pass
        for fail in (False, True):
            with self.subTest(fail=fail), tempfile.TemporaryDirectory() as directory:
                root = Path(directory).resolve()
                M.SEED.write_model_samples(root / 'data')
                name = 'addp-ftp-123456abcdef'
                (root / 'state.json').write_text(json.dumps({'name': name, 'label': 'org.addp.ftp.owner=' + name}))
                container = {'State': {'Running': True}, 'Config': {'Labels': {'org.addp.ftp.owner': name}},
                             'Mounts': [{'Source': str(root / 'data'), 'Destination': '/srv/sftpgo/data', 'RW': False}]}
                main = root / 'data/models/3ds/model.3ds'
                original = main.read_bytes()
                with patch.dict(os.environ, ADDP_FTP_ADMISSION_DIR=directory, ADDP_ONLINE_SECRET_DIR=directory), \
                        patch.object(M.subprocess, 'run', return_value=SimpleNamespace(stdout=json.dumps([container]))) as inspect:
                    try:
                        with M.changed_texture_fixture(model) as changed:
                            self.assertEqual(main.read_bytes(), original.replace(b'texture.png', b'newname.png'))
                            self.assertEqual(changed['fingerprint'], model['fingerprint'])
                            self.assertEqual(changed['item_id'], model['item_id'])
                            self.assertNotEqual(changed['sha256'], model['sha256'])
                            self.assertEqual(changed['composition'][1]['path'], 'models/3ds/newname.png')
                            self.assertEqual((main.parent / 'newname.png').read_bytes(), (main.parent / 'texture.png').read_bytes())
                            if fail:
                                raise ExpectedConsumerFailure('synthetic consumer failure')
                    except ExpectedConsumerFailure:
                        self.assertTrue(fail)
                    self.assertEqual(main.read_bytes(), original)
                    self.assertFalse((main.parent / 'newname.png').exists())
                    inspect.assert_called_once_with(['docker', 'inspect', name], check=True, capture_output=True, text=True)

    def test_composition_fixture_rejects_foreign_mount_links_and_existing_texture(self):
        client = FakeClient()
        model = M.prepare_models(client.items, 17, client.model_samples)[1]
        for fault in ('foreign_label', 'writable', 'wrong_mount', 'link', 'preexisting'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                root = Path(directory).resolve()
                M.SEED.write_model_samples(root / 'data')
                name = 'addp-ftp-123456abcdef'
                (root / 'state.json').write_text(json.dumps({'name': name, 'label': 'org.addp.ftp.owner=' + name}))
                container = {'State': {'Running': True}, 'Config': {'Labels': {'org.addp.ftp.owner': name}},
                             'Mounts': [{'Source': str(root / 'data'), 'Destination': '/srv/sftpgo/data', 'RW': False}]}
                main = root / 'data/models/3ds/model.3ds'
                original = main.read_bytes()
                replacement = main.parent / 'newname.png'
                if fault == 'foreign_label':
                    container['Config']['Labels']['org.addp.ftp.owner'] = 'foreign'
                elif fault == 'writable':
                    container['Mounts'][0]['RW'] = True
                elif fault == 'wrong_mount':
                    container['Mounts'][0]['Source'] = str(root / 'other')
                elif fault == 'link':
                    main.rename(main.with_name('other.3ds'))
                    main.symlink_to('other.3ds')
                else:
                    replacement.write_bytes(b'foreign')
                with patch.dict(os.environ, ADDP_FTP_ADMISSION_DIR=directory, ADDP_ONLINE_SECRET_DIR=directory), \
                        patch.object(M.subprocess, 'run', return_value=SimpleNamespace(stdout=json.dumps([container]))), \
                        self.assertRaises((M.SuiteError, FileExistsError)):
                    with M.changed_texture_fixture(model):
                        self.fail('unsafe fixture must not yield')
                self.assertEqual(main.read_bytes(), original)
                if fault == 'preexisting':
                    self.assertEqual(replacement.read_bytes(), b'foreign')

    def test_composition_fixture_cleans_owned_preparation_failures_before_yield(self):
        client = FakeClient()
        model = M.prepare_models(client.items, 17, client.model_samples)[1]
        for fault in ('texture-write', 'texture-chmod', 'main-chmod', 'replace'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                root = Path(directory).resolve()
                M.SEED.write_model_samples(root / 'data')
                name = 'addp-ftp-123456abcdef'
                (root / 'state.json').write_text(json.dumps({'name': name, 'label': 'org.addp.ftp.owner=' + name}))
                container = {'State': {'Running': True}, 'Config': {'Labels': {'org.addp.ftp.owner': name}},
                             'Mounts': [{'Source': str(root / 'data'), 'Destination': '/srv/sftpgo/data', 'RW': False}]}
                main = root / 'data/models/3ds/model.3ds'
                original = main.read_bytes()
                fdopen, chmod, replace = M.os.fdopen, M.os.fchmod, M.os.replace
                counts = []

                def fchmod(*args):
                    counts.append(True)
                    if fault == 'texture-chmod' or (fault == 'main-chmod' and len(counts) == 2):
                        raise OSError('synthetic chmod failure')
                    return chmod(*args)

                @contextmanager
                def broken_stream(*args):
                    with fdopen(*args) as stream:
                        class Writer:
                            def write(self, content):
                                stream.write(content[:3])
                                stream.flush()
                                raise OSError('synthetic partial texture write')
                            def fileno(self):
                                return stream.fileno()
                        yield Writer()

                with patch.dict(os.environ, ADDP_FTP_ADMISSION_DIR=directory, ADDP_ONLINE_SECRET_DIR=directory), \
                        patch.object(M.subprocess, 'run', return_value=SimpleNamespace(stdout=json.dumps([container]))), \
                        patch.object(M.os, 'fdopen', broken_stream if fault == 'texture-write' else fdopen), \
                        patch.object(M.os, 'fchmod', side_effect=fchmod), \
                        patch.object(M.os, 'replace', side_effect=OSError('synthetic replace failure') if fault == 'replace' else replace), \
                        self.assertRaises(OSError):
                    with M.changed_texture_fixture(model):
                        self.fail('failed preparation must not yield')
                self.assertEqual(main.read_bytes(), original)
                self.assertEqual(sorted(path.name for path in main.parent.iterdir()), ['model.3ds', 'texture.png'])

    def test_composition_rule_matrix_rechecks_old_deny_then_old_grant_and_stale_cache(self):
        client = FakeClient()
        report = {'engine_id': 17, 'models': M.prepare_models(client.items, 17, client.model_samples)}
        source = report['models'][1]
        report['source_authorizations'] = [{'item_fingerprint': source['fingerprint'], 'request_id': 'grant-A'}]
        changed = copy.deepcopy(source)
        changed['composition'][1]['path'] = 'models/3ds/newname.png'
        events = []

        class Authorization:
            def facts(self, model):
                return {'boundary_hash': 'b' * 64 if model is changed else 'a' * 64}
            def grant(self, model, *, initialize):
                if initialize is not False:
                    raise AssertionError('composition approval must not initialize the existing requirement')
                name = 'B' if model is changed else 'A-new'
                events.append('grant-' + name)
                return {'request_id': 'grant-' + name}
            def revoke(self, command):
                events.append('revoke-' + command)
            def release(self, command):
                events.append('release-' + command)

        @contextmanager
        def fixture(model):
            self.assertIs(model, source)
            try:
                yield changed
            finally:
                events.append('restore-physical')

        def scan(*args):
            events.append('scan')
            return 'scan-B'
        def check(client, engine, model, status):
            self.assertEqual(engine, 17)
            self.assertIs(model, changed)
            events.append('check-' + str(status))
            return status
        with patch.object(M, 'changed_texture_fixture', fixture), patch.object(M, 'scan_scope', side_effect=scan), \
                patch.object(M, 'source_read_control', side_effect=check), \
                patch.object(M, 'verify_cached_results', return_value=[{'status': 403}]) as cache:
            result = M.verify_composition_rules(client, Authorization(), report, 'deny-A', 30)
        self.assertEqual(events, ['scan', 'revoke-grant-A', 'grant-B', 'check-403', 'release-deny-A',
                                  'check-200', 'restore-physical', 'scan', 'revoke-grant-B', 'grant-A-new'])
        cache.assert_called_once_with(client, report, {'ifc': 200, '3ds': 403})
        self.assertNotEqual(result['original_boundary_hash'], result['changed_boundary_hash'])

    def test_withdrawn_browser_report_requires_both_original_models_and_no_new_execution(self):
        expected = {'run_id': 'owned', 'engine_id': 17, 'tenant_id': 2, 'principal_id': '8',
                    'models': [{'format': kind, 'item_id': i + 1} for i, kind in enumerate(('ifc', '3ds'))],
                    'browser': {'models': [{'result_id': i + 20, 'execution_id': 'execution-' + str(i)} for i in range(2)]}}
        actual = {key: expected[key] for key in ('run_id', 'engine_id', 'tenant_id', 'principal_id')}
        actual.update(browser_phase='withdrawn', models=[
            {'format': source['format'], 'item_id': source['item_id'], 'result_id': published['result_id'],
             'status': 403, 'error_visible': True, 'content_successes': 0, 'generation_requests': 0,
             'unchanged_execution_id': published['execution_id'], 'unchanged_execution_status': 'success'}
            for source, published in zip(expected['models'], expected['browser']['models'])])
        M.validate_withdrawal_browser_report(actual, expected)
        with_failure = copy.deepcopy(expected)
        with_failure['failed_generation'] = {'format': 'ifc', 'execution_id': 'failed-rebuild', 'status': 'failed'}
        failure_proof = copy.deepcopy(actual)
        failure_proof['models'][0].update(unchanged_execution_id='failed-rebuild', unchanged_execution_status='failed')
        M.validate_withdrawal_browser_report(failure_proof, with_failure)
        with self.assertRaises(M.SuiteError):
            M.validate_withdrawal_browser_report(actual, with_failure)
        for key, value in (('status', 200), ('error_visible', False), ('content_successes', 1),
                           ('generation_requests', 1), ('unchanged_execution_id', 'new-execution'), ('result_id', 99)):
            changed = copy.deepcopy(actual)
            changed['models'][1][key] = value
            with self.subTest(key=key), self.assertRaises(M.SuiteError):
                M.validate_withdrawal_browser_report(changed, expected)
        actual['models'].pop()
        with self.assertRaises(M.SuiteError):
            M.validate_withdrawal_browser_report(actual, expected)


if __name__ == '__main__':
    unittest.main()
