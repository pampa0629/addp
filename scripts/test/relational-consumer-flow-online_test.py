import importlib.util
import hashlib
import os
import sys
import unittest
from pathlib import Path
from unittest.mock import Mock, patch


SCRIPT = Path(__file__).with_name("relational-consumer-flow-online.py")
SPEC = importlib.util.spec_from_file_location("relational_consumer_flow_online", SCRIPT)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class RelationalConsumerFlowOnlineTest(unittest.TestCase):
    oceanbase = ONLINE.PROFILES["oceanbase"]
    opengauss = ONLINE.PROFILES["opengauss"]
    kingbase = ONLINE.PROFILES["kingbase"]
    tidb = ONLINE.PROFILES["tidb"]

    def source_clients(self):
        """Persisted records exist independently of the recipient's visible list."""
        client, issuer = Mock(), Mock()
        state = {"scanned": False, "granted": set(), "events": []}
        items = [{"id": i, "node_id": 2, "item_type": "table", "full_name": "public." + name,
                  "fingerprint": hashlib.sha256(f"17:public.{name}".encode()).hexdigest()}
                 for i, name in enumerate((ONLINE.SOURCE_TABLE, ONLINE.TARGET_TABLE), 1)]
        def context(principal, permissions, role):
            return {"principal": {"type": "user", "id": principal},
                    "context": {"type": "tenant", "tenant_id": "2"},
                    "token": {"type": "first_party_access_token"},
                    "authorization": {"role_assignments": [{"role_key": role, "permissions": sorted(permissions)}]}}
        def consume(method, path, expected, body=None):
            if path.endswith("/auth/context"):
                return ONLINE.SUPPORT.Response(200, context("7", ONLINE.REQUIRED_PERMISSIONS, "online.opengauss_consumer_flow"))
            if path == "/api/v1/meta/scan/run/manual":
                self.assertEqual(body["engine_id"], 17)
                state["events"].append("scan")
                return ONLINE.SUPPORT.Response(201, {"execution_id": "scan-1"})
            if path == "/api/v1/meta/executions/scan-1":
                state["scanned"] = True
                return ONLINE.SUPPORT.Response(200, {"status": "success"})
            if path.startswith("/api/v1/meta/engines/17/items"):
                state["events"].append("recipient_catalog")
                return ONLINE.SUPPORT.Response(200, [item for item in items if item["fingerprint"] in state["granted"]])
            self.assertEqual(path, "/api/v1/system/engine-access/read-observations/current-source")
            name = body["targets"][0]["segments"][-1]["name"]
            item = next(item for item in items if item["full_name"] == "public." + name)
            allowed = item["fingerprint"] in state["granted"]
            self.assertEqual(expected, (200,) if allowed else (403,))
            state["events"].append("allowed" if allowed else "denied")
            return ONLINE.SUPPORT.Response(expected[0], {"observed_at": "2026-11-03T00:00:00Z"} if allowed else {})
        def authorize(method, path, expected, body=None):
            if path.endswith("/auth/context"):
                return ONLINE.SUPPORT.Response(200, context("8", ONLINE.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS, ONLINE.REGISTRATION.SOURCE_INITIALIZER_ROLE))
            if path.endswith("/authorization-facts"):
                self.assertTrue(state["scanned"])
                state["events"].append("authoritative_facts")
                fingerprint = path.split("/")[-2]
                item = next(item for item in items if item["fingerprint"] == fingerprint)
                target = {"version": "catalog.path/v1", "engine_id": 17, "segments": [
                    {"term": "server", "kind": "server", "name": ""},
                    {"term": "schema", "kind": "namespace", "name": "public"},
                    {"term": "table", "kind": "table", "name": item["full_name"][7:]}]}
                return ONLINE.SUPPORT.Response(200, {"schema_version": "meta.data_item_authorization_facts/v1",
                    "engine_id": "17", "item_fingerprint": fingerprint, "item_type": "table",
                    "full_name": item["full_name"], "basis_state": "complete", "boundary_hash": "a" * 64,
                    "observed_at": "2026-11-03T00:00:00Z", "boundary": {"layout": "single", "native_target": target}})
            self.assertEqual((method, path, expected), ("POST", "/api/v1/system/engines/17/access_grants", (201,)))
            self.assertEqual((body["action"], body["recipient_id"]), ("read", "7"))
            state["events"].append("grant")
            state["granted"].add(body["item_fingerprint"])
            return ONLINE.SUPPORT.Response(201, dict(body, engine_id="17", approval_mode="independent",
                revocation=None, boundary_hash=body["expected_boundary_hash"]))
        client.request.side_effect, issuer.request.side_effect = consume, authorize
        return client, issuer, state

    def test_source_grants_make_only_two_persisted_tables_visible_before_consumption(self):
        client, issuer, state = self.source_clients()
        with self.assertRaisesRegex(ONLINE.SuiteError, "Meta must return exactly one"):
            ONLINE.find_item(client, 17, "public." + ONLINE.SOURCE_TABLE, "table")
        state["events"].clear()
        result = ONLINE.initialize_source_reads(client, issuer, 2, 17, "public", self.opengauss, ONLINE.time.monotonic() + 30)
        self.assertEqual(result, {"engine_id": "17", "scan_execution_id": "scan-1", "exact_read_grants": 2})
        self.assertEqual(state["events"], ["scan"] + ["authoritative_facts", "denied", "grant", "allowed", "recipient_catalog"] * 2)
        self.assertEqual(len(state["granted"]), 2)
        for table in (ONLINE.SOURCE_TABLE, ONLINE.TARGET_TABLE):
            self.assertEqual(ONLINE.find_item(client, 17, "public." + table, "table")["full_name"], "public." + table)

    def test_source_preparation_refuses_other_profiles_and_namespaces_without_io(self):
        for profile, namespace in ((self.oceanbase, "public"), (self.kingbase, "public"),
                                   (self.tidb, "public"), (self.opengauss, "other")):
            with self.subTest(engine=profile.engine_type, namespace=namespace):
                client, issuer = Mock(), Mock()
                with self.assertRaisesRegex(ONLINE.SuiteError, "limited to the approved"):
                    ONLINE.initialize_source_reads(client, issuer, 2, 17, namespace, profile, 30)
                client.request.assert_not_called()
                issuer.request.assert_not_called()

    def test_incomplete_source_facts_stop_preparation_before_any_grant(self):
        client, issuer, state = self.source_clients()
        original = issuer.request.side_effect
        def incomplete(method, path, expected, body=None):
            result = original(method, path, expected, body)
            if path.endswith("/authorization-facts"):
                result.payload["basis_state"] = "incomplete"
            return result
        issuer.request.side_effect = incomplete
        with self.assertRaisesRegex(ONLINE.SuiteError, "complete matching"):
            ONLINE.initialize_source_reads(client, issuer, 2, 17, "public", self.opengauss, ONLINE.time.monotonic() + 30)
        self.assertEqual(state["granted"], set())
        self.assertNotIn("recipient_catalog", state["events"])

    @patch.dict(os.environ, {"ADDP_ONLINE_TEST": "1", "ADDP_ONLINE_TEST_TENANT_ID": "2",
        "ADDP_ONLINE_CONSUMER_ENGINE_TYPE": "opengauss", "ADDP_ONLINE_CONSUMER_ENGINE_ID": "17",
        "ADDP_ONLINE_CONSUMER_NAMESPACE": "public", "GATEWAY_URL": "http://127.0.0.1:8000",
        "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "consumer-token", "ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN": "issuer-token"}, clear=True)
    def test_business_entry_refuses_initializer_credential_before_consumer_calls(self):
        with patch.object(sys, "argv", [str(SCRIPT)]), patch.object(ONLINE, "GatewayClient") as client, patch.object(ONLINE, "run_scenario") as run:
            with self.assertRaisesRegex(ONLINE.SuiteError, "credential must be removed"):
                ONLINE.main()
            client.return_value.request.assert_not_called()
            run.assert_not_called()

    @patch.dict(os.environ, {"ADDP_ONLINE_TEST": "1", "ADDP_ONLINE_TEST_TENANT_ID": "2",
        "ADDP_ONLINE_CONSUMER_ENGINE_TYPE": "opengauss", "ADDP_ONLINE_CONSUMER_ENGINE_ID": "17",
        "ADDP_ONLINE_CONSUMER_NAMESPACE": "public", "GATEWAY_URL": "http://127.0.0.1:8000",
        "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "consumer-token", "ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN": "issuer-token",
        "GITHUB_ACTIONS": "true", "RUNNER_OS": "Linux", "ADDP_ONLINE_HOSTED": "1", "ADDP_ONLINE_HOST": "1"}, clear=True)
    def test_source_entry_rejects_personal_or_remote_profiles_before_io(self):
        for key, value in (("GITHUB_ACTIONS", "false"), ("RUNNER_OS", "Darwin"),
                           ("ADDP_ONLINE_HOSTED", "0"), ("ADDP_ONLINE_HOST", "0"),
                           ("GATEWAY_URL", "https://remote.invalid")):
            with self.subTest(key=key), patch.dict(os.environ, {key: value}), patch.object(sys, "argv", [str(SCRIPT), "--initialize-source-read"]), patch.object(ONLINE, "GatewayClient") as client:
                with self.assertRaisesRegex(ONLINE.SuiteError, "Hosted Linux profile|loopback Gateway"):
                    ONLINE.main()
                client.return_value.request.assert_not_called()

    def test_transfer_uses_bounded_watermark_and_idempotent_upsert(self) -> None:
        payload = ONLINE.transfer_payload(
            "gate", "source", "target-parent", self.oceanbase
        )
        config = payload["config"]

        self.assertEqual(config["runtime"], {"boundary": "bounded"})
        self.assertEqual(
            config["load"],
            {
                "mode": "incremental",
                "change_detection": {
                    "type": "watermark",
                    "field": "updated_at",
                    "tie_breaker": ["id"],
                    "start": "committed",
                    "end": "execution_upper_bound",
                },
            },
        )
        self.assertEqual(
            config["target"]["policy"],
            {"apply_mode": "upsert", "keys": ["id"]},
        )
        self.assertEqual(config["transforms"], [])

    def test_rows_normalize_mysql_protocol_scalars_and_detect_duplicates(self) -> None:
        rows = [
            {"id": 2.0, "item_code": "OB-1002", "quantity": 5.0, "amount": 44.5},
            {"id": 1, "item_code": "OB-1001", "quantity": 2, "amount": "19.9000"},
        ]

        normalized = ONLINE.normalize_rows(rows, "owner")

        self.assertEqual(
            normalized,
            [
                {"id": "1", "item_code": "OB-1001", "quantity": 2, "amount": "19.90"},
                {"id": "2", "item_code": "OB-1002", "quantity": 5, "amount": "44.50"},
            ],
        )
        with self.assertRaisesRegex(ONLINE.SuiteError, "duplicate stable keys"):
            ONLINE.normalize_rows([rows[0], rows[0]], "owner")

    def test_profiles_share_assertions_but_keep_native_namespace_and_dialect(self) -> None:
        self.assertEqual(self.oceanbase.namespace_kind, "database")
        self.assertEqual(self.oceanbase.identifier_quote, "`")
        self.assertEqual(self.opengauss.namespace_kind, "schema")
        self.assertEqual(self.opengauss.identifier_quote, '"')
        self.assertEqual(self.kingbase.namespace_kind, "schema")
        self.assertEqual(self.kingbase.identifier_quote, '"')
        self.assertEqual(self.tidb.namespace_kind, "database")
        self.assertEqual(self.tidb.identifier_quote, "`")
        baseline, final = ONLINE.expected_rows(self.opengauss)
        self.assertEqual(baseline[0]["item_code"], "OG-1001")
        self.assertEqual(final[-1]["item_code"], "OG-1006")
        kingbase_baseline, kingbase_final = ONLINE.expected_rows(self.kingbase)
        self.assertEqual(kingbase_baseline[0]["item_code"], "KB-1001")
        self.assertEqual(kingbase_final[-1]["item_code"], "KB-1006")
        tidb_baseline, tidb_final = ONLINE.expected_rows(self.tidb)
        self.assertEqual(tidb_baseline[0]["item_code"], "TIDB-1001")
        self.assertEqual(tidb_final[-1]["item_code"], "TIDB-1006")

    def test_table_service_payload_leaves_stable_key_to_service_snapshot(self) -> None:
        payload = ONLINE.service_payload(
            "online-service", 17, "addp://engine/17/path/public/orders", self.opengauss
        )

        self.assertEqual(
            payload["data_config"]["locator"],
            "addp://engine/17/path/public/orders",
        )
        self.assertNotIn("stable_key", payload["data_config"])

    @patch.object(ONLINE.time, "sleep")
    @patch.object(ONLINE.time, "monotonic", return_value=1.0)
    def test_failed_meta_scan_reports_execution_diagnostics(
        self, _monotonic: Mock, _sleep: Mock
    ) -> None:
        client = Mock()
        client.request.side_effect = [
            ONLINE.SUPPORT.Response(201, {"execution_id": "scan-1"}),
            ONLINE.SUPPORT.Response(
                200,
                {
                    "execution_id": "scan-1",
                    "status": "failed",
                    "current_step": "执行失败: openGauss catalog query failed",
                    "error_details": {
                        "message": "openGauss catalog query failed",
                        "failed_targets_count": 1,
                    },
                },
            ),
        ]

        with self.assertRaises(ONLINE.SuiteError) as raised:
            ONLINE.wait_for_scan(client, 17, 10.0)

        message = str(raised.exception)
        self.assertIn("scan-1", message)
        self.assertIn("openGauss catalog query failed", message)
        self.assertIn("failed_targets_count", message)

    def test_namespace_locator_uses_profile_catalog_level(self) -> None:
        item = {"node_id": 27}
        self.assertEqual(
            ONLINE.build_namespace_locator(17, item, "public", self.opengauss),
            "addp://engine/17/path/public?type=schema&node_id=27",
        )

    @patch.object(ONLINE.time, "sleep")
    @patch.object(ONLINE.time, "monotonic", side_effect=(1.0, 2.0))
    def test_restarts_the_same_transfer_task_and_reads_terminal_metrics(
        self, _monotonic: Mock, _sleep: Mock
    ) -> None:
        client = Mock()
        client.request.side_effect = [
            ONLINE.SUPPORT.Response(200, {"execution_id": "execution-1"}),
            ONLINE.SUPPORT.Response(
                200,
                {
                    "execution_id": "execution-1",
                    "status": "success",
                    "records_read": 2,
                    "records_written": 2,
                },
            ),
        ]

        execution = ONLINE.run_task(client, 17, 10.0)
        evidence = ONLINE.assert_transfer_counts(execution, 2, "incremental")

        self.assertEqual(evidence["execution_id"], "execution-1")
        self.assertEqual(evidence["records_read"], 2)
        self.assertEqual(
            client.request.call_args_list[0].args[:3],
            ("POST", "/api/v1/transfer/task-definitions/17/start", (200,)),
        )

    def test_consumer_identity_does_not_require_system_engine_control_plane(self) -> None:
        self.assertIn(
            "system.execution_authorization.create", ONLINE.REQUIRED_PERMISSIONS
        )
        self.assertNotIn("system.engine.read", ONLINE.REQUIRED_PERMISSIONS)
        self.assertTrue(
            all(
                not permission.startswith("system.engine.")
                for permission in ONLINE.REQUIRED_PERMISSIONS
            )
        )

    def test_manager_preview_uses_locator_and_requires_expected_schema(self) -> None:
        client = Mock()
        client.request.return_value = ONLINE.SUPPORT.Response(
            200,
            {
                "preview_type": "table",
                "data": {
                    "columns": [
                        "id",
                        "item_code",
                        "quantity",
                        "amount",
                        "updated_at",
                    ],
                    "rows": [
                        {
                            "id": 1,
                            "item_code": "OB-1001",
                            "quantity": 2,
                            "amount": "19.90",
                            "updated_at": "2026-09-06T10:00:00Z",
                        }
                    ],
                },
            },
        )

        rows = ONLINE.manager_rows(
            client, "addp://engine/47/path/db/table", self.oceanbase
        )

        self.assertEqual(rows[0]["item_code"], "OB-1001")
        request_path = client.request.call_args.args[1]
        self.assertIn("/api/v1/manager/preview?", request_path)
        self.assertIn("locator=addp%3A%2F%2Fengine%2F47%2Fpath%2Fdb%2Ftable", request_path)

        client.request.return_value = ONLINE.SUPPORT.Response(
            200,
            {
                "preview_type": "table",
                "data": {"columns": ["id"], "rows": []},
            },
        )
        with self.assertRaisesRegex(ONLINE.SuiteError, "unexpected columns"):
            ONLINE.manager_rows(
                client, "addp://engine/47/path/db/table", self.oceanbase
            )

    def test_cleanup_service_confirms_the_resource_is_absent(self) -> None:
        client = Mock()
        client.request.side_effect = [
            ONLINE.SUPPORT.Response(200, {"id": 9, "version": 4}),
            ONLINE.SUPPORT.Response(200, {"message": "deleted"}),
            ONLINE.SUPPORT.Response(404, {"error_code": "not_found"}),
        ]

        ONLINE.cleanup_service(client, 9)
        self.assertEqual(client.request.call_args_list[1].args[3], {"version": 4})

        self.assertEqual(
            [call.args[:3] for call in client.request.call_args_list],
            [
                ("GET", "/api/v1/service/query/9", (200, 404)),
                ("DELETE", "/api/v1/service/query/9", (200,)),
                ("GET", "/api/v1/service/query/9", (404,)),
            ],
        )


if __name__ == "__main__":
    unittest.main()
