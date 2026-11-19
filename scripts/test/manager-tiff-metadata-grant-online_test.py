import hashlib
import importlib
import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock


SCENE = importlib.import_module("scripts.test.manager-tiff-metadata-grant-online")
OWNER = importlib.import_module("scripts.test.manager-internal-artifact-lineage-online")
BOUNDARY_HASH = "a" * 64
FINGERPRINT = hashlib.sha256(b"7:source.tif").hexdigest()
OBSERVED = "2026-11-19T12:01:00+00:00"


class TIFFMetadataGrantTest(unittest.TestCase):
    def exercise(self, *, leak_content=False, lose_create_receipt=False, wrong_revoke=False):
        visible = False
        commands = []
        timeline = []
        target = SCENE.REGISTRATION.ExactFileMetadataGrants.target(7, "source.tif", "nfs")
        composition = {
            "format": "tiff", "source_layout": "single",
            "refs": [{"target": target, "primary": True, "required": True}],
        }

        def request(actor, method, route, expected, body=None):
            nonlocal visible
            if route == "/api/v1/system/auth/context":
                permissions = (SCENE.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS |
                               {"meta.scan_task.execute", "meta.scan_task.read"}) if actor == "issuer" else {"meta.catalog.read"}
                role = SCENE.REGISTRATION.SOURCE_INITIALIZER_ROLE if actor == "issuer" else "metadata-reader"
                payload = {
                    "principal": {"type": "user", "id": "3" if actor == "issuer" else "5"},
                    "context": {"type": "tenant", "tenant_id": "2"},
                    "token": {"type": "first_party_access_token"},
                    "authorization": {"role_assignments": [{"role_key": role, "permissions": list(permissions)}]},
                }
                status = 200
            elif route.endswith("/authorization-facts"):
                self.assertEqual(actor, "issuer")
                self.assertTrue(route.endswith(f"/7/data-items/{FINGERPRINT}/authorization-facts"))
                status = 200
                payload = {
                    "schema_version": "meta.data_item_authorization_facts/v1",
                    "engine_id": "7", "item_fingerprint": FINGERPRINT, "full_name": "source.tif", "item_type": "file",
                    "basis_state": "complete", "boundary": {"layout": "single", "file_composition": composition},
                    "boundary_hash": BOUNDARY_HASH, "observed_at": OBSERVED,
                }
            elif route.endswith("/metadata-checks/meta-query"):
                self.assertEqual(actor, "consumer")
                self.assertEqual(body, {"targets": [target]})
                status, payload = 200, {"observed_at": OBSERVED, "targets": [{"target": target, "visible": visible}]}
            elif route.endswith("/access_grants"):
                self.assertEqual(actor, "issuer")
                self.assertEqual(body["action"], "metadata_read")
                commands.append(body["request_id"])
                visible = True
                if lose_create_receipt:
                    raise OWNER.SuiteError("simulated lost create response")
                status, payload = 201, dict(body, engine_id="7", boundary_hash=BOUNDARY_HASH,
                                            file_composition=composition, operator_principal_id="3",
                                            approval_mode="independent")
            elif route.endswith("/revoke"):
                self.assertEqual(actor, "issuer")
                self.assertTrue(route.endswith(f"/access_grants/{commands[0]}/revoke"))
                visible = False
                status = 200
                payload = {
                    "request_id": commands[0], "revoked_request_ids": ["wrong"] if wrong_revoke else commands,
                    "revoked_by_principal_id": "3", "revoked_by_membership_id": "8",
                    "revoked_at": OBSERVED, "reason": body["reason"],
                }
            else:
                self.assertEqual(actor, "consumer")
                self.assertEqual(route, "/api/v1/meta/items/by-catalog-path?engine_id=7&catalog_path=source.tif")
                status = 200 if visible else 404
                timeline.append(status)
                payload = {
                    "id": 18, "tenant_id": 2, "engine_id": 7, "item_type": "file", "full_name": "source.tif",
                    "fingerprint": FINGERPRINT, "size_bytes": 42 if leak_content else None,
                    "attributes": {"schema_version": 1, "item": {"data_type": "media", "format": "tiff", "layout": "single"}},
                } if visible else {}
            self.assertIn(status, expected)
            return SimpleNamespace(payload=payload, status=status)

        def client(_gateway, actor, _timeout):
            return SimpleNamespace(request=lambda *args: request(actor, *args))

        with (tempfile.TemporaryDirectory() as directory,
              mock.patch.object(importlib.import_module("scripts.test.orchestrator-execution-faults"), "deployment_secret"),
              mock.patch.object(OWNER, "GatewayClient", side_effect=client),
              mock.patch.object(OWNER, "wait_for_meta_scan", return_value="scan-id") as scan):
            env = {
                "ADDP_ONLINE_MANAGER_CASE": SCENE.CASE, "ADDP_ONLINE_TEST_RUN_ID": "manager-42-1",
                "ADDP_ONLINE_TEST_TENANT_ID": "2", "ADDP_ONLINE_MANAGER_NFS_ENGINE_ID": "7",
                "GATEWAY_URL": "http://localhost", "ADDP_ONLINE_ARTIFACT_DIR": directory,
                "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "consumer", "ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN": "issuer",
            }
            if leak_content or lose_create_receipt or wrong_revoke:
                with self.assertRaises(OWNER.SuiteError):
                    SCENE.run(env, OWNER)
            else:
                SCENE.run(env, OWNER)
            self.assertEqual(scan.call_args.kwargs["scope"], {"targets": ["addp://engine/7/path/source.tif?type=file"]})
            self.assertFalse(visible)
            return json.loads((Path(directory) / f"{SCENE.CASE}.json").read_text()), timeline

    def test_real_grant_helper_requires_three_meta_observations_and_exact_revocation(self):
        report, timeline = self.exercise()
        self.assertEqual(timeline, [404, 200, 404])
        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["pending"], [])
        self.assertEqual(report["revocation"]["receipt"]["request_id"], report["grant"]["request_id"])
        self.assertEqual(report["revocation"]["meta_status"], 404)

    def test_content_leak_and_lost_create_response_still_clean_the_fixture_grant(self):
        for options in ({"leak_content": True}, {"lose_create_receipt": True}):
            with self.subTest(options=options):
                report, timeline = self.exercise(**options)
                self.assertEqual(report["result"], "failed")
                self.assertEqual(timeline[0], 404)
                self.assertEqual(timeline[-1], 404)
                self.assertEqual(report["pending"], [])
                self.assertEqual(report["revocation"]["meta_status"], 404)

    def test_mismatched_revocation_receipt_keeps_pending_failure_evidence(self):
        report, timeline = self.exercise(wrong_revoke=True)
        self.assertEqual(timeline, [404, 200])
        self.assertEqual(report["result"], "failed")
        self.assertEqual(report["phase"], "revocation")
        self.assertEqual(report["cleanup"]["result"], "failed")
        self.assertEqual(report["pending"][0]["request_id"], report["grant"]["request_id"])
