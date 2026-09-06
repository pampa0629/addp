import importlib.util
import sys
import unittest
from pathlib import Path
from unittest.mock import Mock


SCRIPT = Path(__file__).with_name("security-plaintext-access-online.py")
SPEC = importlib.util.spec_from_file_location(
    "security_protection_exemption_online", SCRIPT
)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class SecurityPlaintextAccessOnlineTest(unittest.TestCase):
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

    def test_revoke_active_grants_uses_numeric_version(self) -> None:
        client = Mock()
        client.request.side_effect = [
            ONLINE.SUPPORT.Response(
                200,
                {
                    "data": [
                        {
                            "id": "grant-1",
                            "assessment_id": "assessment-1",
                            "consumer_owner": "manager",
                            "action": "preview",
                            "subject_type": "user",
                            "subject_id": "41",
                            "effective_state": "active",
                            "version": "2",
                        }
                    ],
                    "total_pages": 1,
                },
            ),
            ONLINE.SUPPORT.Response(200, {"effective_state": "revoked"}),
        ]

        ONLINE.revoke_active_grants(
            client,
            "enrollment-1",
            "assessment-1",
            "41",
            "cleanup",
        )

        delete_call = client.request.call_args_list[1]
        self.assertEqual(delete_call.args[0], "DELETE")
        self.assertEqual(delete_call.args[3]["version"], 2)


if __name__ == "__main__":
    unittest.main()
