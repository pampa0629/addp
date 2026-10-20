from importlib import import_module
import copy
import hashlib
import os
from pathlib import Path
import tempfile
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
            return SimpleNamespace(status=200 if command in self.grants else 404, payload={'request_id': command})
        if path.endswith('/access_denies'):
            self.denies.add(body['deny_id'])
            return SimpleNamespace(payload={**body, 'engine_id': '17', 'established_by_principal_id': self.principal})
        if path.endswith('/release'):
            command = path.split('/')[-2]
            self.release_statuses = expected
            return SimpleNamespace(status=200 if command in self.denies else 404, payload={'deny_id': command})
        raise AssertionError(path)


class FTPConsumerTest(unittest.TestCase):
    def test_exact_catalog_scan_preview_and_download(self):
        client = FakeClient()
        with patch.dict(os.environ, ADDP_ONLINE_TEST_RUN_ID='owned-run'), patch.object(M.SUPPORT, 'wait_for_scan', return_value='scan-owned'):
            report = M.run(client, 2, 17, client.samples, client.model_samples, 30)
        self.assertEqual(len(report['samples']), 3)
        self.assertEqual(report['principal_id'], '8')
        self.assertEqual([model['format'] for model in report['models']], ['ifc', '3ds'])
        self.assertEqual(len([call for call in client.calls if '/downloads/file?' in call[1]]), 3)
        self.assertTrue(all('%25' in sample['locator'] and '&item_id=' in sample['locator'] for sample in report['samples']))

    def test_rejects_admin_write_capability_and_corrupt_download(self):
        for fault in ('admin', 'write', 'bad_download'):
            with self.subTest(fault=fault):
                client = FakeClient()
                setattr(client, fault, True)
                with patch.object(M.SUPPORT, 'wait_for_scan', return_value='scan'), self.assertRaises((M.SuiteError, M.SUPPORT.SuiteError)):
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


if __name__ == '__main__':
    unittest.main()
