import importlib.util
import sys
import unittest
from pathlib import Path
from unittest.mock import Mock, patch
from datetime import datetime, timezone


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
             patch.object(ONLINE, 'revoke_exemption', side_effect=lambda *args: events.append('revoke')):
            report = ONLINE.run_scenario(applicant, approver, 2, 17, 120)
        self.assertEqual(events, ['baseline', 'approve', 'authorized', 'other-subject', 'expired', 'history-expired',
                                  'approve', 'second-approval', 'other-subject-second-approval', 'revoke', 'revoked', 'history-revoked'])
        self.assertEqual(report['schema_version'], 'addp.security-plaintext-access-online/v2')
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


if __name__ == "__main__":
    unittest.main()
