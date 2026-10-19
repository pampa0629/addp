import importlib
import io
import json
import os
import tempfile
import unittest
from pathlib import Path
from contextlib import redirect_stderr
from unittest.mock import patch
from urllib.error import HTTPError

SCENE = importlib.import_module('scripts.test.platform-administrator-governance-online')
API = SCENE.API


def environment():
    env = {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOST': '1',
           'ADDP_ONLINE_HOSTED': '1', 'ADDP_ONLINE_TEST': '1', 'ONLINE_SUITE': SCENE.SUITE,
           'POSTGRES_DB': 'addp_online', 'ADDP_ONLINE_TEST_RUN_ID': 'governance-test',
           'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'tenant-token', 'ADDP_ONLINE_SECRET_DIR': '/tmp/private',
           'GATEWAY_URL': 'http://127.0.0.1:8000', 'SYSTEM_URL': 'http://127.0.0.1:8180',
           'CONSOLE_URL': 'http://127.0.0.1:5170'}
    for n, role in enumerate((*SCENE.ROLES, 'successor'), 1):
        for key, value in {'id': str(n), 'username': role, 'password': 'fixture-password', 'totp': 'JBSWY3DPEHPK3PXP'}.items():
            env[f'ADDP_ONLINE_GOV_{role.upper()}_{key.upper()}'] = value
    return env


def identity(role):
    actions = ['read'] + ([] if role == 'audit' else ['create']) + ([] if role == 'security' else ['approve', 'reject'])
    return {'principal': {'id': '7', 'type': 'user'}, 'context': {'type': 'platform'},
            'authentication': {'assurance_level': 'aal2'}, 'token': {'type': 'first_party_access_token'},
            'authorization': {'role_assignments': [{'role_key': f'platform.{role}_administrator',
             'scope': {'type': 'platform'}, 'permissions': ['iam.platform_administrator_change.'+a for a in actions]}]}}


class AdministratorGovernanceOnlineTest(unittest.TestCase):
    def test_removed_route_requires_404_without_assuming_a_json_error_contract(self):
        scene = SCENE.Scenario(environment())
        scene.actors['system'] = API.GatewayClient(environment()['GATEWAY_URL'], 'officer-token', 10)
        for status in (404, 200, 401, 403):
            with self.subTest(status=status):
                response = HTTPError('http://127.0.0.1/removed', status, 'status', {}, io.BytesIO(b'404 page not found'))
                with patch('urllib.request.urlopen', side_effect=response):
                    if status == 404:
                        scene.assert_legacy_route_removed()
                    else:
                        with self.assertRaises(API.SuiteError): scene.assert_legacy_route_removed()

    def test_failure_diagnostics_do_not_copy_http_response_or_credentials(self):
        failure = API.SuiteError('POST /login returned HTTP 401 (raw-secret-never-export)')
        with patch.object(SCENE, 'validate_hosted'), patch.object(SCENE.PREFLIGHT, 'run_preflight'), \
             patch.dict(os.environ, environment(), clear=True), patch.object(SCENE.Scenario, 'run', side_effect=failure):
            output = io.StringIO()
            with redirect_stderr(output): self.assertEqual(SCENE.main(), 1)
            self.assertIn('http-401', output.getvalue())
            self.assertNotIn('raw-secret-never-export', output.getvalue())

    def test_profile_rejects_personal_development_before_any_request(self):
        valid = environment()
        self.addCleanup(patch.stopall)
        patch.object(SCENE.sys, 'platform', 'linux').start()
        patch.object(SCENE.platform, 'machine', return_value='x86_64').start()
        SCENE.validate_hosted(valid)
        for key, value in [('GITHUB_ACTIONS', 'false'), ('RUNNER_OS', 'macOS'), ('ADDP_ONLINE_HOSTED', '0'),
                           ('POSTGRES_DB', 'addp'), ('POSTGRES_DB', 'addp_iam_test'), ('ONLINE_SUITE', 'platform-node-metrics'),
                           ('ADDP_ONLINE_OWNER_MANAGED', '1'), ('GATEWAY_URL', 'http://example.com:8000'),
                           ('CONSOLE_URL', 'http://127.0.0.1:5170/?token=secret')]:
            with self.subTest(key=key, value=value), self.assertRaises((API.SuiteError, SCENE.PREFLIGHT.PreflightError)):
                SCENE.validate_hosted({**valid, key: value})
        with patch.object(SCENE.sys, 'platform', 'darwin'), self.assertRaises(API.SuiteError):
            SCENE.validate_hosted(valid)
        with patch.dict(os.environ, {}, clear=True), patch.object(API.GatewayClient, 'request') as request:
            self.assertEqual(SCENE.main(), 1)
            request.assert_not_called()

    def test_authoritative_officers_reject_wrong_scope_role_assurance_and_daily_audit_write(self):
        for role in SCENE.ROLES:
            SCENE.assert_identity(identity(role), role, '7')
        cases = []
        for key, value in [('principal', {'id': '8', 'type': 'user'}), ('context', {'type': 'tenant', 'tenant_id': '2'}),
                           ('token', {'type': 'service_access_token'}), ('authentication', {'assurance_level': 'aal1'}),
                           ('delegation', {'id': '9'})]:
            cases.append({**identity('audit'), key: value})
        extra = identity('audit');extra['authorization']['role_assignments'][0]['permissions'].append('iam.user.create');cases.append(extra)
        extra = identity('audit');extra['authorization']['role_assignments'].append(identity('system')['authorization']['role_assignments'][0]);cases.append(extra)
        for value in cases:
            with self.assertRaises(API.SuiteError): SCENE.assert_identity(value, 'audit', '7')

    def test_review_uses_observed_version_and_requires_real_reviewer_attribution(self):
        scene = SCENE.Scenario(environment())
        calls = []
        class Client:
            def request(self, method, path, expected, body):
                calls.append((method, path, expected, body))
                return API.Response(200, {'review': {'principal_id': '3'}})
        scene.actors['audit'] = Client()
        scene.review('audit', {'id': '11', 'version': '4'})
        self.assertEqual(calls[0][3], {'version': '4', 'reason': 'governance-test:approve'})
        scene.credentials['audit']['id'] = '7'
        with self.assertRaises(API.SuiteError): scene.review('audit', {'id': '11', 'version': '4'})

    def test_browser_receipt_cannot_hide_failure_or_export_secrets(self):
        with tempfile.TemporaryDirectory() as folder:
            env = {**environment(), 'ADDP_ONLINE_ARTIFACT_DIR': folder}
            scene = SCENE.Scenario(env)
            receipt = Path(folder)/'administrator-browser.json'
            def browser(*args, **kwargs):
                self.assertIn('--output=/tmp/private/governance-browser-output', args[0])
                self.assertTrue(kwargs['capture_output'])
                receipt.write_text(json.dumps({key: True for key in SCENE.BROWSER_PROOFS}))
                return type('Result', (), {'returncode': 0})()
            with patch.object(SCENE.subprocess, 'run', side_effect=browser):
                self.assertEqual(set(scene.browser()), SCENE.BROWSER_PROOFS)
                with self.assertRaises(API.SuiteError): scene.browser()
            receipt.unlink()
            def bad(*args, **kwargs):
                value = {key: True for key in SCENE.BROWSER_PROOFS};value['recovery_token'] = 'never-export'
                receipt.write_text(json.dumps(value));return type('Result', (), {'returncode': 0})()
            with patch.object(SCENE.subprocess, 'run', side_effect=bad), self.assertRaises(API.SuiteError):scene.browser()

    def test_totp_matches_known_rfc_counter(self):
        self.assertEqual(SCENE.totp('GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ', 59), '287082')


if __name__ == '__main__': unittest.main()
