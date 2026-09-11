import importlib.util
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
    tidb = ONLINE.PROFILES["tidb"]

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
        self.assertEqual(self.tidb.namespace_kind, "database")
        self.assertEqual(self.tidb.identifier_quote, "`")
        baseline, final = ONLINE.expected_rows(self.opengauss)
        self.assertEqual(baseline[0]["item_code"], "OG-1001")
        self.assertEqual(final[-1]["item_code"], "OG-1006")
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
            ONLINE.SUPPORT.Response(200, {"id": 9}),
            ONLINE.SUPPORT.Response(200, {"message": "deleted"}),
            ONLINE.SUPPORT.Response(404, {"error_code": "not_found"}),
        ]

        ONLINE.cleanup_service(client, 9)

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
