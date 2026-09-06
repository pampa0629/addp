import importlib.util
import sys
import unittest
from pathlib import Path
from unittest.mock import Mock, patch


SCRIPT = Path(__file__).with_name("oceanbase-consumer-flow-online.py")
SPEC = importlib.util.spec_from_file_location("oceanbase_consumer_flow_online", SCRIPT)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class OceanBaseConsumerFlowOnlineTest(unittest.TestCase):
    def test_transfer_uses_bounded_watermark_and_idempotent_upsert(self) -> None:
        payload = ONLINE.transfer_payload("gate", "source", "target-parent")
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

    def test_engine_identity_cannot_fall_back_to_mysql(self) -> None:
        client = Mock()
        client.request.return_value = ONLINE.SUPPORT.Response(
            200,
            {
                "id": 17,
                "engine_type": "mysql",
                "lifecycle_state": "active",
                "connection_status": "online",
            },
        )

        with self.assertRaisesRegex(ONLINE.SuiteError, "engine_type=oceanbase"):
            ONLINE.validate_engine(client, 17, ONLINE.time.monotonic() + 1)

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
