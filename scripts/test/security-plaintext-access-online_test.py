import importlib.util
import sys
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch
from datetime import datetime, timedelta, timezone
from contextlib import ExitStack
import time


SCRIPT = Path(__file__).with_name("security-plaintext-access-online.py")
SPEC = importlib.util.spec_from_file_location(
    "security_protection_exemption_online", SCRIPT
)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class SecurityPlaintextAccessOnlineTest(unittest.TestCase):
    def test_applicant_rejects_custom_role_with_approval_permission(self):
        permissions = list(ONLINE.APPLICANT_PERMISSIONS)
        context = {"principal": {"type": "user", "id": "41"},
                   "context": {"type": "tenant", "tenant_id": "2"},
                   "token": {"type": "first_party_access_token"},
                   "authorization": {"role_assignments": [{"role_key": "online.applicant", "permissions": permissions}]}}
        client = Mock()
        client.request.return_value = ONLINE.SUPPORT.Response(200, context)
        self.assertEqual(ONLINE.validate_user_identity(client, 2, ONLINE.APPLICANT_PERMISSIONS, "applicant")["principal_id"], "41")
        permissions.append("security.protection_access_request.update")
        with self.assertRaisesRegex(ONLINE.SuiteError, 'minimum suite permissions'):
            ONLINE.validate_user_identity(client, 2, ONLINE.APPLICANT_PERMISSIONS, "applicant")

    def test_permissions_separate_applicant_and_approver(self) -> None:
        self.assertIn(
            "security.protection_access_request.create", ONLINE.APPLICANT_PERMISSIONS
        )
        self.assertNotIn(
            "security.protection_access_request.update", ONLINE.APPLICANT_PERMISSIONS
        )
        self.assertIn(
            "security.protection_access_request.update", ONLINE.APPROVER_PERMISSIONS
        )
        self.assertNotIn(
            "security.protection_access_request.create", ONLINE.APPROVER_PERMISSIONS
        )

    def test_phone_values_distinguish_masked_and_plaintext(self) -> None:
        masked = [
            {"id": 1, "phone": "138****5678"},
            {"id": 2.0, "phone": "139****4321"},
            {"id": 3, "phone": None},
        ]
        raw = [
            {"id": 1, "phone": "13812345678"},
            {"id": 2, "phone": "13987654321"},
            {"id": 3, "phone": None},
        ]

        self.assertFalse(
            ONLINE.assert_phone_values(masked, ONLINE.MASKED_VALUES, "baseline")[
                "plaintext"
            ]
        )
        self.assertTrue(
            ONLINE.assert_phone_values(raw, ONLINE.RAW_VALUES, "authorized")[
                "plaintext"
            ]
        )

    def test_access_targets_are_scoped_to_manager_preview(self) -> None:
        client = Mock()
        client.request.return_value = ONLINE.SUPPORT.Response(200, {"data": []})

        self.assertEqual(ONLINE.access_targets(client, "sha256:target"), [])

        path = client.request.call_args.args[1]
        self.assertIn("target_identity=sha256%3Atarget", path)
        self.assertIn("consumer_owner=manager", path)
        self.assertIn("action=preview", path)

    def test_pending_scope_and_numeric_approval_version(self):
        applicant, approver = Mock(), Mock()
        applicant.request.return_value = ONLINE.SUPPORT.Response(201, {"id": "request-1", "state": "pending", "version": "1"})
        approver.request.side_effect = [
            ONLINE.SUPPORT.Response(200, {"data": [{"id": "request-1", "version": "2"}], "total_pages": 1}),
            ONLINE.SUPPORT.Response(200, {"state": "approved", "exemption_id": "grant-1"}),
        ]
        request_id, grant_id, expiry = ONLINE.approve_access_request(applicant, approver, "assessment-1", 40)
        self.assertEqual((request_id, grant_id), ("request-1", "grant-1"))
        self.assertGreater(expiry, datetime.now(timezone.utc))
        self.assertIn("scope=pending", approver.request.call_args_list[0].args[1])
        self.assertEqual(approver.request.call_args_list[1].args[3]["version"], 2)
        self.assertNotIn("subject_id", applicant.request.call_args_list[0].args[3])
        self.assertEqual(applicant.request.call_args_list[1].args[2], (403,))

    def test_revocation_reads_only_owned_grant_and_uses_numeric_version(self):
        approver = Mock()
        approver.request.side_effect = [ONLINE.SUPPORT.Response(200, {"effective_state": "active", "version": "2"}),
                                       ONLINE.SUPPORT.Response(200, {"effective_state": "revoked"})]
        ONLINE.revoke_exemption(approver, "grant-1")
        calls = approver.request.call_args_list
        self.assertEqual(calls[0].args[:2], ("GET", "/api/v1/security/protection-exemptions/grant-1"))
        self.assertEqual(calls[1].args[0], "DELETE")
        self.assertEqual(calls[1].args[3]["version"], 2)

    def test_history_filters_current_authorization_before_pagination(self):
        for state in ("expired", "revoked"):
            with self.subTest(state=state):
                approver = Mock()
                approver.request.return_value = ONLINE.SUPPORT.Response(200, {
                    "data": [{"id": "request-1", "state": "approved", "authorization_state": state}], "total_pages": 1})
                ONLINE.verify_approval_history(approver, "request-1", state)
                path = approver.request.call_args.args[1]
                self.assertIn("scope=history", path)
                self.assertIn("state=approved", path)
                self.assertIn("authorization_state=" + state, path)
                approver.request.return_value = ONLINE.SUPPORT.Response(200, {"data": [], "total_pages": 1})
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.verify_approval_history(approver, "request-1", state)

    def test_scenario_preserves_two_users_and_expiry_read_without_security_refresh(self):
        applicant, approver = Mock(), Mock()
        events = []
        def approve(*args):
            count = sum(event == 'approve' for event in events)
            events.append('approve')
            return f'request-{count}', f'grant-{count}', datetime.now(timezone.utc)
        def preview(client, locator, values, phase, deadline):
            events.append(phase)
            return {"rows": 3, "plaintext": values == ONLINE.RAW_VALUES}
        with patch.object(ONLINE, 'validate_user_identity', side_effect=[{"principal_id": "41"}, {"principal_id": "42"}]), \
             patch.object(ONLINE, 'wait_for_scan', return_value='scan'), \
             patch.object(ONLINE, 'find_item', return_value={"fingerprint": "target"}), \
             patch.object(ONLINE, 'build_item_locator', return_value='locator'), \
             patch.object(ONLINE, 'ensure_enrollment', return_value=({"id": "enrollment"}, True)), \
             patch.object(ONLINE, 'ensure_phone_assessment', return_value=({"id": "assessment"}, True)), \
             patch.object(ONLINE, 'access_targets', return_value=[{"assessment_id": "assessment", "requestable": True}]), \
             patch.object(ONLINE, 'approve_access_request', side_effect=approve), \
             patch.object(ONLINE, 'wait_for_manager_values', side_effect=preview), \
             patch.object(ONLINE, 'verify_approval_history', side_effect=lambda *args: events.append('history-' + args[-1]) or {}), \
             patch.object(ONLINE, 'revoke_exemption', side_effect=lambda *args: events.append('revoke')), \
             patch.object(ONLINE, 'exercise_security_unavailability', side_effect=lambda *args: events.append('outage') or {'request_id': 'outage-request', 'exemption_id': 'outage-grant'}):
            report = ONLINE.run_scenario(applicant, approver, 2, 17, 120)
        self.assertEqual(events, ['baseline', 'approve', 'authorized', 'other-subject', 'expired', 'history-expired',
                                  'approve', 'second-approval', 'other-subject-second-approval', 'revoke', 'revoked', 'history-revoked', 'outage'])
        self.assertEqual(report['schema_version'], 'addp.security-plaintext-access-online/v4')
        self.assertIn('outage-request', report['audit_retained']['access_request_ids'])
        self.assertIn('outage-grant', report['audit_retained']['exemption_ids'])
        self.assertEqual(report['security_unavailability']['request_id'], 'outage-request')
        self.assertFalse(report['lifecycle']['expired_without_security_refresh']['plaintext'])
        self.assertFalse(report['lifecycle']['revoked_applicant']['plaintext'])
        self.assertTrue(report['lifecycle']['second_authorized_applicant']['plaintext'])
        applicant.request.assert_not_called()
        approver.request.assert_called_once()
        self.assertEqual(approver.request.call_args.args[2], (403,))

    def test_initializer_prepares_exact_table_for_each_distinct_user_and_initializes_requirement_once(self):
        with patch.object(ONLINE.SUPPORT, 'require_hosted_initialization'), \
             patch.object(ONLINE, 'required_environment', return_value='fixture-token'), \
             patch.object(ONLINE, 'GatewayClient') as clients, \
             patch.object(ONLINE, 'validate_user_identity', side_effect=[{"principal_id": "41"}, {"principal_id": "42"}]), \
             patch.object(ONLINE.SUPPORT, 'initialize_fresh_governance') as governance, \
             patch.object(ONLINE.REGISTRATION, 'initialize_exact_record_read_grants') as grants:
            ONLINE.initialize_hosted(2, 17, 'http://gateway')
        self.assertEqual(grants.call_count, 2)
        self.assertEqual(grants.call_args_list[0].args[3], [(17, 'schema', 'addp_online_security', 'exemption_source')])
        self.assertTrue(grants.call_args_list[0].kwargs['initialize_approval'])
        self.assertFalse(grants.call_args_list[1].kwargs['initialize_approval'])
        definition = governance.call_args.args[2][0]
        self.assertEqual(definition['detector'], 'addp.detector.phone_metadata/v2')

    def test_same_user_is_rejected_before_scan_or_any_governance_write(self):
        with patch.object(ONLINE, 'validate_user_identity', return_value={"principal_id": "41"}), \
             patch.object(ONLINE, 'wait_for_scan') as scan:
            with self.assertRaisesRegex(ONLINE.SuiteError, 'two different Users'):
                ONLINE.run_scenario(Mock(), Mock(), 2, 17, 120)
            scan.assert_not_called()

    def browser_fixture(self, directory):
        env = {key: 'fixture-value' for key in (
            'ADDP_ONLINE_TEST_USER_USERNAME', 'ADDP_ONLINE_TEST_USER_PASSWORD',
            'ADDP_ONLINE_TEST_APPROVER_USERNAME', 'ADDP_ONLINE_TEST_APPROVER_PASSWORD',
            'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN', 'ADDP_ONLINE_TEST_APPROVER_ACCESS_TOKEN',
            'GATEWAY_URL', 'CONSOLE_URL')}
        env.update(ADDP_ONLINE_ARTIFACT_DIR=directory, ADDP_ONLINE_TEST_RUN_ID='run-1', ADDP_ONLINE_TEST_TENANT_ID='2')
        api = {'identities': {'applicant': {'principal_id': '41'}, 'approver': {'principal_id': '42'}},
               'fixture': {'source_locator': 'addp://engine/17/path/addp_online_security/exemption_source?type=table',
                           'security_assessment_id': 'assessment'},
               'audit_retained': {'access_request_ids': ['api-1', 'api-2']}}
        report = {'schema_version': 'addp.security-plaintext-access-browser/v1', 'result': 'passed',
                  'run_id': 'run-1', 'tenant_id': '2', 'applicant_id': '41', 'approver_id': '42',
                  'records': 3, 'request_ids': ['browser-1', 'browser-2'],
                  'browser_warning_errors': 0, 'failed_business_responses': 0, 'anonymous_refresh_401': 2}
        for flag in ('isolated_browser_contexts', 'same_users_verified', 'request_from_manager_ui',
                     'approval_from_security_ui', 'authorized_applicant_verified', 'other_subject_protected',
                     'expired_preview_protected', 'expired_history_verified', 'revocation_from_security_ui',
                     'revoked_preview_protected', 'revoked_history_verified', 'null_preserved'):
            report[flag] = True
        return env, api, report

    def test_browser_report_rejects_missing_evidence_wrong_identity_and_reused_requests(self):
        env, api, report = self.browser_fixture('/tmp/fixture')
        self.assertEqual(ONLINE.validate_plaintext_browser_report(report, env, api), report)
        mutations = [(key, False) for key, value in report.items() if value is True]
        mutations += [('applicant_id', '42'), ('approver_id', '41'), ('tenant_id', '3'),
                      ('records', 2), ('records', True), ('failed_business_responses', 1),
                      ('browser_warning_errors', 1), ('anonymous_refresh_401', 3),
                      ('request_ids', ['browser-1', 'browser-1']), ('request_ids', ['api-1', 'browser-1']),
                      ('request_ids', 'browser-1'), ('request_ids', ['', 'browser-2'])]
        for key, value in mutations:
            with self.subTest(key=key, value=value):
                invalid = dict(report, **{key: value})
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.validate_plaintext_browser_report(invalid, env, api)
        for key in report:
            with self.subTest(missing=key):
                invalid = dict(report)
                del invalid[key]
                with self.assertRaises(ONLINE.SuiteError):
                    ONLINE.validate_plaintext_browser_report(invalid, env, api)
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.validate_plaintext_browser_report(dict(report, access_token='never-publish'), env, api)

    def test_browser_dispatch_requires_fresh_report_and_all_protected_screenshots(self):
        with tempfile.TemporaryDirectory() as directory:
            env, api, report = self.browser_fixture(directory)
            root = Path(directory)
            report_path = root / 'security-plaintext-access-browser.json'
            report_path.write_text(json.dumps(report))
            for phase in ('pending', 'expired', 'revoked'):
                (root / f'security-plaintext-{phase}.png').write_bytes(b'old')
            result = Mock(returncode=0, stdout='', stderr='')
            with patch.object(ONLINE.subprocess, 'run', return_value=result):
                with self.assertRaisesRegex(ONLINE.SuiteError, 'missing'):
                    ONLINE.run_plaintext_browser(Path('/repo'), env, api)
            self.assertFalse(report_path.exists())
            def execute(*args, **kwargs):
                self.assertIn('e2e/online/security-plaintext-access.spec.js', args[0])
                self.assertEqual(kwargs['cwd'], Path('/repo/console/frontend'))
                self.assertEqual(kwargs['env']['ADDP_ONLINE_SECURITY_ASSESSMENT_ID'], 'assessment')
                self.assertEqual(kwargs['env']['ADDP_ONLINE_SECURITY_PLAINTEXT_LOCATOR'], api['fixture']['source_locator'])
                report_path.write_text(json.dumps(report))
                for phase in ('pending', 'expired', 'revoked'):
                    (root / f'security-plaintext-{phase}.png').write_bytes(b'protected')
                return result
            with patch.object(ONLINE.subprocess, 'run', side_effect=execute):
                self.assertEqual(ONLINE.run_plaintext_browser(Path('/repo'), env, api), report)
            for phase in ('pending', 'expired', 'revoked'):
                def missing(*args, **kwargs):
                    value = execute(*args, **kwargs)
                    (root / f'security-plaintext-{phase}.png').unlink()
                    return value
                with patch.object(ONLINE.subprocess, 'run', side_effect=missing):
                    with self.assertRaisesRegex(ONLINE.SuiteError, 'missing'):
                        ONLINE.run_plaintext_browser(Path('/repo'), env, api)
            with patch.object(ONLINE.subprocess, 'run', return_value=Mock(returncode=1, stdout='', stderr='')):
                with self.assertRaisesRegex(ONLINE.SuiteError, 'status 1'):
                    ONLINE.run_plaintext_browser(Path('/repo'), env, api)
            del env['ADDP_ONLINE_TEST_APPROVER_PASSWORD']
            with patch.object(ONLINE.subprocess, 'run') as process:
                with self.assertRaisesRegex(ONLINE.SuiteError, 'APPROVER_PASSWORD'):
                    ONLINE.run_plaintext_browser(Path('/repo'), env, api)
                process.assert_not_called()


class SecurityUnavailabilityTest(unittest.TestCase):
    def process(self, newer=False):
        return {"instance_id": "old" if not newer else "new",
                "started_at": "2026-10-12T00:00:00Z" if not newer else "2026-10-12T00:01:00Z",
                "build_id": "build", "git_commit": "commit", "source_fingerprint": "sha256:source"}

    def exercise(self, *, preview_error=None, probe_error=None, manager_changed=False,
                 recovery_error=None, identity_changed=False):
        events = []
        self.last_events = events
        applicant, approver = Mock(), Mock()
        now = [datetime(2026, 10, 12, tzinfo=timezone.utc)]
        expiry = now[0] + timedelta(seconds=90)
        def lifecycle(repository, operation, deadline):
            events.append(operation)
            if operation == 'start' and recovery_error:
                raise ONLINE.SuiteError('recovery failed')
        def preview(client, locator, expected, phase):
            events.append(phase)
            if preview_error and phase == preview_error:
                raise ONLINE.SuiteError('unexpected preview')
            return {"rows": 3, "plaintext": expected == ONLINE.RAW_VALUES}
        def probe():
            events.append('refused')
            if probe_error:
                raise ONLINE.SuiteError('Security still listening')
            return now[0].isoformat()
        def wait(expires_at, deadline):
            events.append('wait-expiry')
            now[0] = expires_at + timedelta(seconds=1)
        def ready(module):
            if module == 'security':
                return self.process('start' in events)
            return self.process(manager_changed and 'stop' in events)
        identities = [{"principal_id": "41"}, {"principal_id": "42"}]
        with ExitStack() as stack:
            stack.enter_context(patch.object(ONLINE.SUPPORT, 'require_hosted_security_lifecycle'))
            stack.enter_context(patch.object(ONLINE.SUPPORT, 'ready_process', side_effect=ready))
            stack.enter_context(patch.object(ONLINE, 'approve_access_request', return_value=('outage-request', 'outage-grant', expiry)))
            stack.enter_context(patch.object(ONLINE, 'wait_for_manager_values', return_value={"rows": 3, "plaintext": True}))
            stack.enter_context(patch.object(ONLINE, 'wait_for_current_grant'))
            stack.enter_context(patch.object(ONLINE, 'run_security_lifecycle', side_effect=lifecycle))
            stack.enter_context(patch.object(ONLINE, 'security_listener_refused', side_effect=probe))
            stack.enter_context(patch.object(ONLINE, 'manager_evidence', side_effect=preview))
            stack.enter_context(patch.object(ONLINE, 'wait_for_grant_expiry', side_effect=wait))
            stack.enter_context(patch.object(ONLINE, 'validate_user_identity', side_effect=[{"principal_id": "different"} if identity_changed else identities[0], identities[1]]))
            stack.enter_context(patch.object(ONLINE, 'verify_approval_history', return_value={"state": "approved", "authorization_state": "expired"}))
            wall = stack.enter_context(patch.object(ONLINE, 'datetime', wraps=datetime))
            wall.now.side_effect = lambda *args: now[0]
            report = ONLINE.exercise_security_unavailability(applicant, approver, 2, 'locator', 'target',
                'assessment', identities, time.monotonic() + 120)
        return report, events

    def test_outage_proves_both_subjects_before_and_after_real_expiry_then_recovers(self):
        report, events = self.exercise()
        self.assertEqual(events, ['stop', 'refused', 'authorized_applicant', 'refused',
            'refused', 'protected_other_subject', 'refused', 'wait-expiry',
            'refused', 'expired_applicant', 'refused', 'refused', 'expired_other_subject', 'refused', 'start'])
        self.assertEqual(len(report['connection_refusals']), 8)
        self.assertTrue(report['authorized_applicant']['plaintext'])
        for key in ('protected_other_subject', 'expired_applicant', 'expired_other_subject'):
            self.assertEqual(report[key], {"rows": 3, "plaintext": False})
        self.assertTrue(report['manager_process_unchanged'])
        self.assertTrue(report['same_users_verified'])
        self.assertEqual(report['security_process']['after'], self.process(True))
        self.assertEqual(report['authorization_seconds'], 90)

    def test_unavailable_probe_accepts_only_refused_listener(self):
        for error in (ConnectionRefusedError(), TimeoutError(), OSError('other transport failure'), None):
            with self.subTest(error=error), patch.object(ONLINE.socket, 'create_connection', side_effect=error):
                if isinstance(error, ConnectionRefusedError):
                    self.assertIsInstance(ONLINE.security_listener_refused(), str)
                else:
                    with self.assertRaises(ONLINE.SuiteError):
                        ONLINE.security_listener_refused()

    def test_each_outage_failure_always_attempts_restoration(self):
        for options in ({'preview_error': 'authorized_applicant'}, {'preview_error': 'expired_applicant'},
                        {'probe_error': True}, {'manager_changed': True}):
            with self.subTest(options=options):
                with self.assertRaises(ONLINE.SuiteError):
                    self.exercise(**options)
                self.assertEqual(self.last_events[-1], 'start')
        for options in ({'recovery_error': True}, {'identity_changed': True}):
            with self.subTest(options=options), self.assertRaises(ONLINE.SuiteError):
                self.exercise(**options)

    def test_lifecycle_is_hosted_only_and_does_not_export_user_credentials(self):
        with patch.dict(ONLINE.os.environ, {}, clear=True), patch.object(ONLINE.subprocess, 'run') as process:
            with self.assertRaises(ONLINE.SuiteError):
                ONLINE.run_security_lifecycle(Path('/personal'), 'stop', time.monotonic() + 60)
            process.assert_not_called()
        with tempfile.TemporaryDirectory() as directory, \
             patch.dict(ONLINE.os.environ, {'ADDP_ONLINE_ARTIFACT_DIR': directory, 'SYSTEM_URL': 'http://system',
                'ADDP_ONLINE_TEST_USER_PASSWORD': 'private', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'token',
                'ADDP_ONLINE_TEST_APPROVER_PASSWORD': 'private', 'ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN': 'prep'}), \
             patch.object(ONLINE.SUPPORT, 'require_hosted_security_lifecycle'), \
             patch.object(ONLINE.subprocess, 'run', return_value=Mock(returncode=0)) as process:
            ONLINE.run_security_lifecycle(Path('/repo'), 'start', time.monotonic() + 60)
            self.assertEqual(process.call_args.args[0], ['bash', 'scripts/dev/start.sh', '-security'])
            env = process.call_args.kwargs['env']
            self.assertEqual(env['SYSTEM_URL'], 'http://system')
            self.assertEqual(env['SKIP_MODTIDY'], '1')
            for key in ('ADDP_ONLINE_TEST_USER_PASSWORD', 'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN',
                        'ADDP_ONLINE_TEST_APPROVER_PASSWORD', 'ADDP_ONLINE_FIXTURE_SECURITY_ACCESS_TOKEN'):
                self.assertNotIn(key, env)
            for result in (Mock(returncode=1), ONLINE.subprocess.TimeoutExpired('start', 60)):
                with patch.object(ONLINE.subprocess, 'run', side_effect=result if isinstance(result, BaseException) else None,
                                  return_value=result), self.assertRaises(ONLINE.SuiteError):
                    ONLINE.run_security_lifecycle(Path('/repo'), 'start', time.monotonic() + 60)
            process.reset_mock()
            with self.assertRaises(ONLINE.SuiteError):
                ONLINE.run_security_lifecycle(Path('/repo'), 'stop', time.monotonic() - 1)
            process.assert_not_called()

    def test_missing_null_field_and_duplicate_row_do_not_count_as_preserved(self):
        rows = [{'id': int(k), 'phone': v} for k, v in ONLINE.MASKED_VALUES.items()]
        for invalid in (rows[:2] + [{'id': 3}], rows + [rows[0]]):
            with self.subTest(invalid=invalid), self.assertRaises(ONLINE.SuiteError):
                ONLINE.assert_phone_values(invalid, ONLINE.MASKED_VALUES, 'outage')


if __name__ == "__main__":
    unittest.main()
