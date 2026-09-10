import importlib.util
import sys
import unittest
from pathlib import Path
from unittest.mock import Mock


SCRIPT = Path(__file__).with_name("security-mysql-owner-protection-online.py")
SPEC = importlib.util.spec_from_file_location(
    "security_mysql_owner_protection_online", SCRIPT
)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class SecurityMySQLOwnerProtectionOnlineTest(unittest.TestCase):
    def test_owner_actions_cover_the_four_projection_bindings(self) -> None:
        self.assertEqual(
            ONLINE.OWNER_ACTIONS,
            {
                "manager": "preview",
                "develop": "query",
                "service": "service_execute",
                "transfer": "export",
            },
        )

    def test_governance_requires_email_detector_and_suppress_baseline(self) -> None:
        client = Mock()
        client.request.side_effect = [
            ONLINE.SUPPORT.Response(
                200,
                [
                    {
                        "id": "11",
                        "code": "email",
                        "default_security_grade_id": "21",
                    }
                ],
            ),
            ONLINE.SUPPORT.Response(
                200,
                [
                    {
                        "id": "31",
                        "capability_key": ONLINE.EMAIL_DETECTOR,
                        "sensitive_data_type_id": "11",
                        "enabled": True,
                    }
                ],
            ),
            ONLINE.SUPPORT.Response(
                200,
                [
                    {
                        "id": "41",
                        "sensitive_data_type_id": "11",
                        "security_grade_id": "21",
                        "effect": "suppress",
                        "enabled": True,
                    }
                ],
            ),
        ]

        governance = ONLINE.validate_governance(client)

        self.assertEqual(
            governance,
            {
                "sensitive_data_type_id": "11",
                "detector_id": "31",
                "baseline_id": "41",
                "effect": "suppress",
            },
        )

    def test_email_suppression_keeps_all_non_sensitive_rows(self) -> None:
        evidence = ONLINE.assert_email_suppressed(
            [{"id": value, "customer_code": f"C-{value}"} for value in range(1, 6)],
            "manager",
            ["id", "customer_code"],
        )

        self.assertEqual(evidence, {"rows": 5, "email_field_present": False})
        with self.assertRaisesRegex(ONLINE.SuiteError, "exposed"):
            ONLINE.assert_email_suppressed(
                [{"id": value, "email": None} for value in range(1, 6)],
                "manager",
                ["id", "email"],
            )

    def test_transfer_uses_native_source_without_a_field_dropping_transform(self) -> None:
        payload = ONLINE.transfer_payload("test", "source", "target")
        config = payload["config"]

        self.assertEqual(config["runtime"], {"boundary": "bounded"})
        self.assertNotIn("query", config["source"])
        self.assertEqual(config["transforms"], [])
        self.assertEqual(config["target"]["policy"], {"apply_mode": "replace"})

    def test_table_service_payload_leaves_stable_key_to_service_snapshot(self) -> None:
        payload = ONLINE.service_payload(
            "online-service", 17, "addp://engine/17/path/public/customers"
        )

        self.assertEqual(
            payload["data_config"]["locator"],
            "addp://engine/17/path/public/customers",
        )
        self.assertNotIn("stable_key", payload["data_config"])


if __name__ == "__main__":
    unittest.main()
