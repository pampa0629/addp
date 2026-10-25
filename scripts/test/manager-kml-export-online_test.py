import copy
import importlib
import json
import tempfile
import unittest
import xml.etree.ElementTree as ET
from pathlib import Path
from unittest import mock

KML = importlib.import_module('scripts.test.manager-kml-export-online')
MANAGER = importlib.import_module('scripts.test.manager-internal-artifact-lineage-online')
GOLDEN = Path(__file__).parents[2] / 'common/format/plugins/kml/testdata/simple-features.kml'


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
            report = {'schema_version': 'addp.manager-kml-export-browser/v1', 'result': 'passed',
                      'run_id': 'run1', 'identity': identity, 'warnings': [], 'failed_responses': [], 'cases': []}
            for source_case in ('wgs84', 'mercator'):
                case = {'source_case': source_case, 'locator': 'addp://engine/1/path/public/' + source_case,
                        'filename': source_case, 'table': source_case,
                        'download_file': source_case + '.kml', 'screenshot': source_case + '.png',
                        'dialog_screenshot': source_case + '-dialog.png'}
                data = ET.fromstring(GOLDEN.read_bytes())
                data.find('k:Document/k:Folder/k:name', KML.NS).text = source_case
                raw = ET.tostring(data)
                (root / case['download_file']).write_bytes(raw)
                (root / case['screenshot']).write_bytes(b'screenshot witness')
                (root / case['dialog_screenshot']).write_bytes(b'dialog screenshot witness')
                proof = KML.validate_kml(raw, source_case)
                context['cases'].append(case)
                report['cases'].append({'source_case': source_case, 'locator': case['locator'],
                    'suggested_filename': source_case + '.kml', 'actual_button_download': True,
                    'monitor_verified': True, 'sha256': proof['sha256'], 'size_bytes': len(raw)})
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

    def test_kml_identity_is_seven_permissions_not_the_full_case_ten(self):
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
