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


class DefinitionProbeClient:
    def __init__(self) -> None:
        self.types = []
        self.baselines = []
        self.next_type_id = 100
        self.next_baseline_id = 200

    def request(self, method, path, expected, body=None):
        if method == "GET" and path == "/api/v1/security/sensitive-data-types":
            return ONLINE.SUPPORT.Response(200, list(self.types))
        if method == "GET" and path == "/api/v1/security/protection-baselines":
            return ONLINE.SUPPORT.Response(200, list(self.baselines))
        if method == "POST" and path == "/api/v1/security/sensitive-data-types":
            protection = body["default_protection"]
            if protection["algorithm"] != ONLINE.STRUCTURED_MASK_ALGORITHM:
                return ONLINE.SUPPORT.Response(400, {"error_code": "bad_request"})
            type_id = str(self.next_type_id)
            baseline_id = str(self.next_baseline_id)
            self.next_type_id += 1
            self.next_baseline_id += 1
            created = {"id": type_id, **body, "version": "1"}
            created.pop("default_protection")
            self.types.append(created)
            self.baselines.append(
                {
                    "id": baseline_id,
                    "sensitive_data_type_id": type_id,
                    "security_grade_id": str(body["default_security_grade_id"]),
                    **protection,
                    "enabled": True,
                    "version": "1",
                }
            )
            return ONLINE.SUPPORT.Response(201, created)
        if method == "DELETE" and path.startswith(
            "/api/v1/security/sensitive-data-types/"
        ):
            type_id = path.rsplit("/", 1)[1]
            self.types = [item for item in self.types if item["id"] != type_id]
            self.baselines = [
                item
                for item in self.baselines
                if item["sensitive_data_type_id"] != type_id
            ]
            return ONLINE.SUPPORT.Response(200, {"message": "deleted"})
        raise AssertionError(f"unexpected request: {method} {path} {expected} {body}")


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
                        "security_classification_id": "12",
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
                "security_classification_id": "12",
                "security_grade_id": "21",
                "detector_id": "31",
                "baseline_id": "41",
                "effect": "suppress",
            },
        )

    def test_default_protection_probe_is_atomic_and_leaves_no_resources(self) -> None:
        client = DefinitionProbeClient()

        evidence = ONLINE.exercise_default_protection_transaction(
            client, 12, 21
        )

        self.assertEqual(evidence["effect"], "mask")
        self.assertEqual(evidence["algorithm"], ONLINE.STRUCTURED_MASK_ALGORITHM)
        self.assertEqual(evidence["invalid_request_status"], 400)
        self.assertTrue(evidence["rollback_verified"])
        self.assertTrue(evidence["cleanup_verified"])
        self.assertEqual(client.types, [])
        self.assertEqual(client.baselines, [])
        self.assertIn("security.sensitive_data_type.create", ONLINE.REQUIRED_PERMISSIONS)
        self.assertIn("security.sensitive_data_type.delete", ONLINE.REQUIRED_PERMISSIONS)

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
