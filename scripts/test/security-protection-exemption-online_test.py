import importlib.util
import sys
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path
from unittest.mock import Mock


SCRIPT = Path(__file__).with_name("security-protection-exemption-online.py")
SPEC = importlib.util.spec_from_file_location(
    "security_protection_exemption_online", SCRIPT
)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class SecurityProtectionExemptionOnlineTest(unittest.TestCase):
    def test_owner_actions_cover_only_the_four_supported_bindings(self) -> None:
        self.assertEqual(
            ONLINE.OWNER_ACTIONS,
            {
                "manager": "preview",
                "develop": "query",
                "service": "service_execute",
                "transfer": "export",
            },
        )

    def test_transfer_payload_uses_one_bounded_sql_route(self) -> None:
        payload = ONLINE.transfer_payload("test", "source", "target")

        config = payload["config"]
        self.assertEqual(config["runtime"], {"boundary": "bounded"})
        self.assertEqual(
            config["source"]["query"]["language"],
            "sql",
        )
        self.assertIn(
            'FROM "addp_online_security"."exemption_source"',
            config["source"]["query"]["statement"],
        )
        self.assertEqual(config["target"]["policy"], {"apply_mode": "replace"})
        self.assertEqual(
            [field["source"] for field in config["transforms"][0]["fields"]],
            ["id", "phone"],
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
            ONLINE.assert_phone_values(masked, ONLINE.MASKED_VALUES, "manager")[
                "plaintext"
            ]
        )
        self.assertTrue(
            ONLINE.assert_phone_values(raw, ONLINE.RAW_VALUES, "manager")[
                "plaintext"
            ]
        )

    def test_activate_reuses_fixed_exemption_aggregates(self) -> None:
        client = Mock()
        client.request.side_effect = [
            ONLINE.Response(
                200,
                {
                    "data": [
                        {
                            "id": "manager-exemption",
                            "assessment_id": "assessment-1",
                            "consumer_owner": "manager",
                            "action": "preview",
                            "version": "4",
                        }
                    ],
                    "total_pages": 1,
                },
            ),
            ONLINE.Response(200, {"id": "manager-exemption", "effective_state": "active"}),
            ONLINE.Response(201, {"id": "develop-exemption", "effective_state": "active"}),
            ONLINE.Response(201, {"id": "service-exemption", "effective_state": "active"}),
            ONLINE.Response(201, {"id": "transfer-exemption", "effective_state": "active"}),
        ]
        expires_at = datetime.now(timezone.utc) + timedelta(minutes=1)

        activated = ONLINE.activate_exemptions(
            client, "enrollment-1", "assessment-1", expires_at
        )

        self.assertEqual(len(activated), 4)
        calls = client.request.call_args_list
        self.assertEqual(calls[1].args[:3], (
            "PUT",
            "/api/v1/security/protection-exemptions/manager-exemption",
            (200,),
        ))
        self.assertEqual(calls[1].args[3]["version"], "4")
        self.assertEqual(
            [call.args[3].get("consumer_owner") for call in calls[2:]],
            ["develop", "service", "transfer"],
        )

    def test_expired_phase_reads_owners_without_security_refresh(self) -> None:
        calls: list[str] = []
        readers = {
            owner: (
                lambda phase, current=owner: (
                    calls.append(f"{current}:{phase}")
                    or [
                        {"id": 1, "phone": "138****5678"},
                        {"id": 2, "phone": "139****4321"},
                        {"id": 3, "phone": None},
                    ]
                )
            )
            for owner in ONLINE.OWNER_ACTIONS
        }

        evidence = ONLINE.validate_owner_phase(
            readers, "expired", ONLINE.MASKED_VALUES
        )

        self.assertEqual(set(evidence), set(ONLINE.OWNER_ACTIONS))
        self.assertEqual(
            calls,
            [
                "manager:expired",
                "develop:expired",
                "service:expired",
                "transfer:expired",
            ],
        )
        self.assertFalse(any("security" in call for call in calls))

    def test_cleanup_revokes_only_still_active_exemptions(self) -> None:
        client = Mock()
        client.request.side_effect = [
            ONLINE.Response(
                200,
                {
                    "id": "active",
                    "effective_state": "active",
                    "version": "2",
                },
            ),
            ONLINE.Response(200, {"id": "active", "effective_state": "revoked"}),
            ONLINE.Response(
                200,
                {
                    "id": "expired",
                    "effective_state": "expired",
                    "version": "5",
                },
            ),
        ]

        ONLINE.revoke_active_exemptions(
            client, [{"id": "active"}, {"id": "expired"}]
        )

        self.assertEqual(
            [(call.args[0], call.args[1]) for call in client.request.call_args_list],
            [
                ("GET", "/api/v1/security/protection-exemptions/active"),
                ("DELETE", "/api/v1/security/protection-exemptions/active"),
                ("GET", "/api/v1/security/protection-exemptions/expired"),
            ],
        )


if __name__ == "__main__":
    unittest.main()
