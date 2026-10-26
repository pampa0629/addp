import importlib.util
import io
import os
import sys
import unittest
from pathlib import Path
from email.message import Message
from unittest.mock import Mock, patch


SCRIPT = Path(__file__).with_name("security-transfer-protection-online.py")
SPEC = importlib.util.spec_from_file_location("security_transfer_protection_online", SCRIPT)
ONLINE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE
SPEC.loader.exec_module(ONLINE)


class GatewayDownloadTest(unittest.TestCase):
    def test_expected_failure_reuses_task_lifecycle_and_keeps_default_success_strict(self):
        for status, expected_status, raises in (("failed", "failed", False), ("failed", "success", True), ("success", "failed", True)):
            client = Mock()
            client.request.side_effect = [ONLINE.Response(201, {"id": 11}),
                ONLINE.Response(200, {"execution_id": "one"}),
                ONLINE.Response(200, {"status": status})]
            owned = []
            with self.subTest(status=status, expected_status=expected_status):
                if raises:
                    with self.assertRaises(ONLINE.SuiteError) as failure:
                        ONLINE.create_and_run_task(client, {}, float("inf"), owned, expected_status=expected_status)
                    self.assertEqual(str(failure.exception), f"Transfer execution ended with status {status}; expected={expected_status}, task_id=11, execution_id=one")
                else:
                    self.assertEqual(ONLINE.create_and_run_task(client, {}, float("inf"), owned,
                        expected_status=expected_status), (11, {"status": status}))
            self.assertEqual(owned, [11])
            self.assertEqual(client.request.call_count, 3)

    def test_invalid_terminal_expectation_fails_before_creating_task(self):
        client = Mock()
        with self.assertRaises(ONLINE.SuiteError):
            ONLINE.create_and_run_task(client, {}, float("inf"), [], expected_status="cancelled")
        client.request.assert_not_called()

    def test_raw_download_reuses_authenticated_transport(self):
        raw = b'{"_id":"1"}\n{"_id":"2"}\n'
        reply = Mock(status=200, headers=Message())
        reply.read.return_value = raw
        reply.__enter__ = Mock(return_value=reply)
        reply.__exit__ = Mock(return_value=False)
        with patch.object(ONLINE.urllib.request, "urlopen", return_value=reply) as opened:
            result = ONLINE.GatewayClient("http://localhost:8000", "fixture-token", 30).request(
                "GET", "/api/v1/manager/exports/1/file", (200,), raw_response=True)
        self.assertEqual(result.payload, raw)
        self.assertEqual(opened.call_args.args[0].get_header("Authorization"), "Bearer fixture-token")

    def test_raw_download_does_not_accept_error_payload_as_file(self):
        reply = Mock(status=403, headers=Message())
        reply.read.return_value = b'{"error_code":"access_denied","error":"sensitive details"}'
        reply.__enter__ = Mock(return_value=reply)
        reply.__exit__ = Mock(return_value=False)
        with patch.object(ONLINE.urllib.request, "urlopen", return_value=reply), self.assertRaisesRegex(
                ONLINE.SuiteError, "HTTP 403 \(access_denied\)") as caught:
            ONLINE.GatewayClient("http://localhost:8000", "fixture-token", 30).request(
                "GET", "/api/v1/manager/exports/1/file", (200,), raw_response=True)
        self.assertNotIn("sensitive details", str(caught.exception))

    def test_cache_header_evidence_survives_json_raw_and_http_errors_without_cookies(self):
        for status, raw_response in ((200, False), (200, True), (409, False)):
            for values in ((), ("no-store",), ("no-store", "public")):
                with self.subTest(status=status, raw_response=raw_response, values=values):
                    headers = Message()
                    for value in values:headers.add_header("Cache-Control", value)
                    headers.add_header("Set-Cookie", "private-session-cookie")
                    raw = b"phone\n***\n" if raw_response else b'{"error_code":"query_protection_roles_unavailable"}'
                    reply = Mock(status=status, headers=headers)
                    reply.read.return_value = raw
                    reply.__enter__ = Mock(return_value=reply)
                    reply.__exit__ = Mock(return_value=False)
                    with patch.object(ONLINE.urllib.request, "urlopen", return_value=reply) as opened:
                        if status == 409:
                            opened.side_effect = ONLINE.urllib.error.HTTPError("http://localhost/exports/1", status, "Conflict", headers, io.BytesIO(raw))
                        result = ONLINE.GatewayClient("http://localhost:8000", "fixture-token", 30).request(
                            "GET", "/api/v1/develop/exports/1", (status,), raw_response=raw_response)
                    self.assertEqual(result.status, status)
                    self.assertEqual(result.cache_control, values)
                    self.assertEqual(result.payload, raw if raw_response else ONLINE.json.loads(raw))
                    self.assertNotIn("private-session-cookie", repr(result))


class SecurityTransferProtectionOnlineTest(unittest.TestCase):
    def test_enrollment_lookup_reads_every_page_before_reusing_fixture(self) -> None:
        client = Mock()
        client.request.side_effect = [
            ONLINE.Response(200, {"data": [{"id": "other"}], "total_pages": 2}),
            ONLINE.Response(
                200,
                {
                    "data": [
                        {
                            "id": "fixture-enrollment",
                            "target_snapshot": {
                                "engine_id": 9,
                                "full_name": "security_online.security_transfer_fixture",
                            },
                        }
                    ],
                    "total_pages": 2,
                },
            ),
            ONLINE.Response(
                200,
                {
                    "id": "fixture-enrollment",
                    "state": "active",
                    "owner_progress": [
                        {
                            "consumer_owner": "transfer",
                            "projection_state": "active",
                            "acknowledged": True,
                            "rules": [{"action": "export", "effect": "mask"}],
                        }
                    ],
                },
            ),
        ]

        enrollment, initialized = ONLINE.ensure_protected_enrollment(
            client,
            9,
            "security_online.security_transfer_fixture",
            "source-locator",
            float("inf"),
        )

        self.assertEqual(enrollment["id"], "fixture-enrollment")
        self.assertFalse(initialized)
        self.assertEqual(
            [call.args[1] for call in client.request.call_args_list],
            [
                "/api/v1/security/protection-enrollments?scope=current&page=1&page_size=100",
                "/api/v1/security/protection-enrollments?scope=current&page=2&page_size=100",
                "/api/v1/security/protection-enrollments/fixture-enrollment",
            ],
        )

    def test_stale_task_check_reads_every_page(self) -> None:
        client = Mock()
        client.request.side_effect = [
            ONLINE.Response(200, {"items": [{"id": 1, "name": "ordinary"}], "total": 101}),
            ONLINE.Response(200, {"items": [{"id": 2, "name": ONLINE.TASK_PREFIX + "stale"}], "total": 101}),
        ]

        with self.assertRaisesRegex(ONLINE.SuiteError, "stale Security Transfer Online tasks"):
            ONLINE.assert_no_stale_tasks(client)

        self.assertEqual(
            [call.args[1] for call in client.request.call_args_list],
            [
                "/api/v1/transfer/task-definitions?page=1&page_size=100",
                "/api/v1/transfer/task-definitions?page=2&page_size=100",
            ],
        )

    def test_find_item_rejects_wrong_data_item_type(self) -> None:
        client = Mock()
        client.request.return_value = ONLINE.Response(
            200,
            [
                {
                    "id": 1,
                    "node_id": 2,
                    "full_name": "security_online.security_transfer_fixture",
                    "item_type": "table",
                    "fingerprint": "source",
                }
            ],
        )

        with self.assertRaisesRegex(ONLINE.SuiteError, "must be a collection DataItem"):
            ONLINE.find_item(
                client,
                9,
                "security_online.security_transfer_fixture",
                "collection",
            )

    def test_validates_mask_projection_without_accepting_unacknowledged_owner(self) -> None:
        enrollment = {
            "owner_progress": [
                {
                    "consumer_owner": "transfer",
                    "projection_state": "active",
                    "acknowledged": True,
                    "rules": [{"action": "export", "effect": "mask"}],
                }
            ]
        }
        ONLINE.validate_transfer_projection(enrollment)

        enrollment["owner_progress"][0]["acknowledged"] = False
        with self.assertRaisesRegex(ONLINE.SuiteError, "not active and acknowledged"):
            ONLINE.validate_transfer_projection(enrollment)

    def test_task_payload_uses_safe_mql_output_lineage_and_single_bounded_route(self) -> None:
        statement = '{"aggregate":"security_transfer_fixture","pipeline":[{"$project":{"userInfo__phone":{"$ifNull":["$userInfo.phone",null]}}}]}'
        payload = ONLINE.task_payload(
            "run",
            "addp://engine/9/path/security_online/security_transfer_fixture?type=collection&item_id=1",
            "addp://engine/7/path/addp_online_security?type=schema&node_id=2",
            ONLINE.MASKED_TARGET,
            statement,
            [{"source": "userInfo__phone", "target": "userInfo__phone", "target_type": "string", "nullable": True}],
        )

        config = payload["config"]
        self.assertEqual(config["runtime"], {"boundary": "bounded"})
        self.assertEqual(config["source"]["query"], {"language": "mql", "statement": statement})
        self.assertEqual(config["target"]["policy"], {"apply_mode": "replace"})
        self.assertEqual(config["transforms"][0]["mode"], "project")
        self.assertNotIn("connection_info", str(payload))

    def test_validates_both_target_shapes_and_only_masked_phone_values(self) -> None:
        result = ONLINE.validate_target_previews(
            (
                ["_id", "display_name"],
                [
                    {"_id": "person-1", "display_name": "Alice"},
                    {"_id": "person-2", "display_name": "Bob"},
                    {"_id": "person-3", "display_name": "No phone"},
                ],
            ),
            (
                ["_id", "userInfo__phone"],
                [
                    {"_id": "person-1", "userInfo__phone": "138****5678"},
                    {"_id": "person-2", "userInfo__phone": "139****4321"},
                    {"_id": "person-3", "userInfo__phone": None},
                ],
            ),
        )

        self.assertEqual(result["excluded_row_count"], 3)
        self.assertEqual(result["masked_non_null_count"], 2)
        self.assertEqual(result["plaintext_value_count"], 0)

    def test_cleanup_deletes_only_owned_task_ids_and_verifies_absence(self) -> None:
        client = Mock()
        client.request.return_value = ONLINE.Response(200, {})

        ONLINE.cleanup_tasks(client, [11, 12])

        self.assertEqual(
            [(call.args[0], call.args[1], call.args[2]) for call in client.request.call_args_list],
            [
                ("DELETE", "/api/v1/transfer/task-definitions/12", (200,)),
                ("GET", "/api/v1/transfer/task-definitions/12", (404,)),
                ("DELETE", "/api/v1/transfer/task-definitions/11", (200,)),
                ("GET", "/api/v1/transfer/task-definitions/11", (404,)),
            ],
        )

    @patch.object(ONLINE, "cleanup_tasks")
    @patch.object(ONLINE, "create_and_run_task")
    @patch.object(ONLINE, "assert_no_stale_tasks")
    @patch.object(ONLINE, "ensure_protected_enrollment")
    @patch.object(ONLINE, "build_schema_locator", return_value="target-parent")
    @patch.object(ONLINE, "build_item_locator", return_value="source-locator")
    @patch.object(ONLINE, "find_item")
    @patch.object(ONLINE, "wait_for_scan", side_effect=["source-scan", "target-scan"])
    @patch.object(ONLINE, "validate_user_identity", return_value={"principal_type": "user"})
    def test_run_cleans_every_created_task_when_second_execution_fails(
        self,
        _identity,
        _scan,
        find_item,
        _build_item,
        _build_schema,
        _enrollment,
        _stale,
        create_and_run,
        cleanup,
    ) -> None:
        source = {"id": 1, "node_id": 2, "full_name": "security_online.security_transfer_fixture", "item_type": "collection", "fingerprint": "source"}
        target = {"id": 3, "node_id": 4, "full_name": "addp_online_security.transfer_excluded", "item_type": "table", "fingerprint": "target"}
        find_item.side_effect = [source, target]
        _enrollment.return_value = ({"id": "enrollment"}, False)

        def execute(_client, _payload, _deadline, owned):
            task_id = 11 if not owned else 12
            owned.append(task_id)
            if task_id == 12:
                raise ONLINE.SuiteError("second execution failed")
            return task_id, {"execution_id": "first", "records_read": 3, "records_written": 3}

        create_and_run.side_effect = execute
        with patch.dict(os.environ, {"ADDP_ONLINE_SECURITY_MONGODB_DATABASE": "security_online"}):
            with self.assertRaisesRegex(ONLINE.SuiteError, "second execution failed"):
                ONLINE.run_scenario(Mock(), 42, 9, 7, "run-1", 30)

        cleanup.assert_called_once_with(unittest.mock.ANY, [11, 12])


if __name__ == "__main__":
    unittest.main()
