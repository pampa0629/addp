import copy
import hashlib
import importlib
import json
import tempfile
import unittest
import xml.etree.ElementTree as ET
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

KML = importlib.import_module('scripts.test.manager-kml-export-online')
MANAGER = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')
GOLDEN = Path(__file__).parents[2] / 'common/format/plugins/kml/testdata/simple-features.kml'


def write_browser_fixture(root, context):
    report = {'schema_version': 'addp.manager-kml-export-browser/v1', 'result': 'passed',
              'run_id': context['run_id'], 'identity': context['identity'],
              'warnings': [], 'failed_responses': [], 'cases': []}
    for source in context['cases']:
        data = ET.fromstring(GOLDEN.read_bytes())
        data.find('k:Document/k:Folder/k:name', KML.NS).text = source['table']
        raw = ET.tostring(data)
        (root / source['download_file']).write_bytes(raw)
        for key in ('screenshot', 'dialog_screenshot'):
            (root / source[key]).write_bytes(b'unit screenshot witness')
        report['cases'].append({'source_case': source['source_case'], 'locator': source['locator'],
            'suggested_filename': source['filename'] + '.kml', 'actual_button_download': True,
            'monitor_verified': True, 'sha256': hashlib.sha256(raw).hexdigest(), 'size_bytes': len(raw)})
    (root / 'manager-kml-export-browser.json').write_text(json.dumps(report))
    return report


class KMLPhysicalOracleTest(unittest.TestCase):
    def test_complete_independent_xml_and_numeric_boolean_values(self):
        proof = KML.validate_kml(GOLDEN.read_bytes(), 'Layer #0 中文 &')
        self.assertEqual(proof['placemarks'], 8)
        self.assertEqual(proof['nonempty_geometries'], 7)
        self.assertEqual(proof['null_geometry'], 1)
        self.assertTrue(proof['null_and_empty_preserved'])

    def test_wrong_geometry_scalar_null_schema_or_folder_never_pass(self):
        raw = GOLDEN.read_text()
        for before, after in [
            ('120,30', '120,31'), ('<SimpleData name="flag">1', '<SimpleData name="flag">0'),
            ('type="bool"', 'type="string"'), ('Layer #0 中文 &amp;', 'Renamed'),
            ('<SimpleData name="标题"></SimpleData>', ''),
            ('<Placemark>', '<Placemark><name>unexpected geometry child</name>'),
        ]:
            with self.subTest(before=before):
                self.assertIn(before, raw)
                with self.assertRaises((ValueError, KeyError)):
                    KML.validate_kml(raw.replace(before, after, 1).encode(), 'Layer #0 中文 &')
        for raw in [b'', b'<kml/>', b'<!DOCTYPE kml><kml/>', GOLDEN.read_bytes()[:100]]:
            with self.assertRaises((ValueError, ET.ParseError)):
                KML.validate_kml(raw, 'Layer #0 中文 &')

    def test_browser_proof_requires_real_saved_bytes_filename_identity_and_screenshot(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            identity = {'principal_id': '8', 'tenant_id': '7'}
            context = {'run_id': 'run1', 'identity': identity, 'cases': []}
            for source_case in ('wgs84', 'mercator'):
                case = {'source_case': source_case, 'locator': 'addp://engine/1/path/public/' + source_case,
                        'filename': source_case, 'table': source_case,
                        'download_file': source_case + '.kml', 'screenshot': source_case + '.png',
                        'dialog_screenshot': source_case + '-dialog.png'}
                context['cases'].append(case)
            report = write_browser_fixture(root, context)
            self.assertEqual(len(KML.validate_browser_report(report, context, root)), 2)
            for mutate in [lambda r:r.update(run_id='other'), lambda r:r.update(warnings=['bad']),
                           lambda r:r.update(failed_responses=[{'status':403}]),
                           lambda r:r['cases'][0].update(actual_button_download=False),
                           lambda r:r['cases'][0].update(sha256='0'*64),
                           lambda r:r['cases'][0].update(suggested_filename='other.kml'),
                           lambda r:r['cases'].pop()]:
                wrong = copy.deepcopy(report)
                mutate(wrong)
                with self.assertRaises(ValueError):
                    KML.validate_browser_report(wrong, context, root)
            (root / 'wgs84.png').unlink()
            with self.assertRaises(ValueError):
                KML.validate_browser_report(report, context, root)


class KMLCaseRoutingTest(unittest.TestCase):
    def test_real_grant_helper_receipts_reach_browser_without_an_id_field(self):
        registration = importlib.import_module('scripts.test.online-engine-registration')
        def auth(principal, permissions):
            return {'principal': {'type': 'user', 'id': str(principal)},
                'context': {'type': 'tenant', 'tenant_id': '2'},
                'token': {'type': 'first_party_access_token'},
                'authorization': {'role_assignments': [{'role_key': 'kml-fixture', 'permissions': sorted(permissions)}]}}
        items = [{'id': 101 + i, 'full_name': 'public.kml_export_' + name, 'item_type': 'table',
                  'fingerprint': hashlib.sha256(('4:public.kml_export_' + name).encode()).hexdigest()}
                 for i, name in enumerate(('wgs84', 'mercator'))]
        def target(item):
            return {'version': 'catalog.path/v1', 'engine_id': 4, 'segments': [
                {'term': 'server', 'kind': 'server', 'name': ''},
                {'term': 'schema', 'kind': 'namespace', 'name': 'public'},
                {'term': 'table', 'kind': 'table', 'name': item['full_name'].split('.')[1]}]}
        receipts, read_checks = [], []
        def consumer_request(method, path, expected, body=None):
            if path == '/api/v1/system/auth/context':
                return SimpleNamespace(payload=auth(7, KML.PERMISSIONS))
            if path == '/api/v1/meta/engines/4/items?branch=public':
                return SimpleNamespace(payload=copy.deepcopy(items))
            if path.startswith('/api/v1/meta/items/'):
                return SimpleNamespace(payload=copy.deepcopy(next(item for item in items if str(item['id']) == path.split('/')[-1])))
            self.assertEqual((method, path), ('POST', '/api/v1/system/engine-access/read-checks/manager-preview'))
            self.assertEqual(len(body['targets']), 1)
            granted = any(receipt['catalog_path'] == body['targets'][0] for receipt in receipts)
            self.assertEqual(expected, (200,) if granted else (403,))
            read_checks.append(expected[0])
            return SimpleNamespace(payload={'observed_at': '2026-10-25T00:00:00Z'} if granted else {})
        def initializer_request(method, path, expected, body=None):
            if path == '/api/v1/system/auth/context':
                return SimpleNamespace(payload=auth(8, registration.SOURCE_INITIALIZER_PERMISSIONS))
            if method == 'GET':
                item = next(item for item in items if item['fingerprint'] == path.split('/')[-2])
                return SimpleNamespace(payload={'schema_version': 'meta.data_item_authorization_facts/v1',
                    'engine_id': '4', 'item_fingerprint': item['fingerprint'], 'item_type': 'table',
                    'full_name': item['full_name'], 'basis_state': 'complete',
                    'boundary': {'layout': 'single', 'native_target': target(item)},
                    'boundary_hash': 'a' * 64, 'observed_at': '2026-10-25T00:00:00Z'})
            self.assertEqual((method, path, expected), ('POST', '/api/v1/system/engines/4/access_grants', (201,)))
            # The public SourceGrantView identifies issuance by request_id;
            # request-only fields and an internal database id are not returned.
            receipt = {key: copy.deepcopy(body[key]) for key in ('request_id', 'catalog_path',
                'requirement_version', 'recipient_type', 'recipient_id', 'action', 'expiry_mode', 'item_fingerprint', 'reason')}
            receipt.update(engine_id='4', approval_mode='independent', boundary_hash=body['expected_boundary_hash'],
                granted_at='2026-10-25T00:00:00Z', catalog_request_id=None, expires_at=None,
                operator_principal_id='8', operator_membership_id='9', revocation=None)
            self.assertNotIn('id', receipt)
            receipts.append(receipt)
            return SimpleNamespace(payload=receipt)
        consumer = mock.Mock(request=mock.Mock(side_effect=consumer_request))
        initializer = mock.Mock(request=mock.Mock(side_effect=initializer_request))
        def client(_base, token, _timeout):
            return {'consumer': consumer, 'initializer': initializer}[token]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            env = dict(GATEWAY_URL='http://127.0.0.1:8000', ADDP_ONLINE_TEST_TENANT_ID='2',
                ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer', ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='initializer',
                ADDP_ONLINE_TEST_USER_USERNAME='unit-user', ADDP_ONLINE_TEST_USER_PASSWORD='unit-password',
                ADDP_ONLINE_MANAGER_PG_ENGINE_ID='4', ADDP_ONLINE_ARTIFACT_DIR=directory,
                ADDP_ONLINE_TEST_RUN_ID='kml-unit', ADDP_ONLINE_MANAGER_CASE='kml-export')
            def browser(command, **kwargs):
                self.assertEqual(read_checks, [403, 200, 403, 200])
                self.assertEqual(len(receipts), 2)
                self.assertIn('e2e/online/manager-kml-export.spec.js', command)
                context = json.loads(kwargs['env']['ADDP_ONLINE_MANAGER_KML_JSON'])
                self.assertEqual([source['source_srid'] for source in context['cases']], [4326, 3857])
                write_browser_fixture(root, context)
            with mock.patch.object(MANAGER, 'GatewayClient', side_effect=client), \
                 mock.patch.object(MANAGER, 'wait_for_meta_scan', return_value='meta-unit'), \
                 mock.patch.object(KML, 'source_snapshot', return_value={'counts': [8, 8]}), \
                 mock.patch.object(KML.subprocess, 'run', side_effect=browser) as launched:
                report = KML.run(Path('/repository'), env, MANAGER)
            launched.assert_called_once()
            self.assertEqual(report['result'], 'passed')
            self.assertEqual(report['source_grants'], [{key: receipt[key] for key in
                ('request_id', 'item_fingerprint', 'boundary_hash')} for receipt in receipts])
            self.assertEqual(len({grant['request_id'] for grant in report['source_grants']}), 2)
            self.assertTrue(report['source_unchanged'])
            self.assertTrue(report['source_metadata_unchanged'])

    def test_case_routes_without_requiring_the_other_cases_storage_and_models(self):
        self.assertEqual(MANAGER.manager_case({'ADDP_ONLINE_MANAGER_CASE':'kml-export'}), 'kml-export')
        with mock.patch.dict(MANAGER.os.environ, {'ADDP_ONLINE_MANAGER_CASE':'kml-export'}, clear=True), \
             mock.patch.object(KML, 'run', return_value={'case':'kml-export'}) as run, \
             mock.patch.object(MANAGER, 'required_environment') as old_environment:
            self.assertEqual(MANAGER.main(), 0)
            run.assert_called_once()
            old_environment.assert_not_called()

    def test_failure_report_is_written_before_disposable_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            env = {key:'test' for key in ('GATEWAY_URL','ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
                   'ADDP_ONLINE_TEST_USER_USERNAME','ADDP_ONLINE_TEST_USER_PASSWORD','ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN')}
            env.update(ADDP_ONLINE_TEST_TENANT_ID='7', ADDP_ONLINE_MANAGER_PG_ENGINE_ID='8',
                       GATEWAY_URL='http://127.0.0.1:8000',
                       ADDP_ONLINE_ARTIFACT_DIR=directory, ADDP_ONLINE_TEST_RUN_ID='kml-test', ADDP_ONLINE_MANAGER_CASE='kml-export')
            with mock.patch.object(MANAGER, 'validate_user_identity', side_effect=ValueError('identity failed')):
                with self.assertRaises(ValueError):
                    KML.run(Path('/repository'), env, MANAGER)
            report = json.loads(Path(directory, 'manager-kml-export.json').read_text())
            self.assertEqual(report['result'], 'failed')
            self.assertNotIn('source_unchanged', report)

    def test_kml_identity_is_eight_permissions_not_the_full_case_ten(self):
        self.assertEqual(len(KML.PERMISSIONS), 8)
        self.assertIn('transfer.task.read', KML.PERMISSIONS)
        old = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online_test')
        context = old.FakeGatewayClient().request('GET', '/api/v1/system/auth/context', (200,)).payload
        for assignment in context['authorization']['role_assignments']:
            assignment['permissions'] = sorted(KML.PERMISSIONS)
        client = mock.Mock()
        client.request.return_value.payload = context
        tenant = int(context['context']['tenant_id'])
        identity = MANAGER.validate_user_identity(client, tenant, KML.PERMISSIONS)
        self.assertEqual(identity['permissions_verified'], sorted(KML.PERMISSIONS))
        with self.assertRaises(MANAGER.SuiteError):
            MANAGER.validate_user_identity(client, tenant)

        for assignment in context['authorization']['role_assignments']:
            assignment['permissions'].remove('transfer.task.read')
        with self.assertRaises(MANAGER.SuiteError):
            MANAGER.validate_user_identity(client, tenant, KML.PERMISSIONS)
