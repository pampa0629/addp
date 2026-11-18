from contextlib import redirect_stdout
import hashlib
import importlib
import json
import io
import os
import subprocess
from datetime import datetime, timedelta, timezone
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, call, patch

CASE = importlib.import_module("scripts.test.orchestrator-scheduled-file-online")
CHILD = "554dffd5-8d1b-4f74-9abe-6ca4f7cfc84d"
PARENT = "04319c9f-6f80-4222-a533-217ee11cdc39"
IDENTITY = (83, 104, 2)
TARGET = {"engine_id": "42"}
GRANT = {"item_fingerprint": "a" * 64, "boundary_hash": "b" * 64}


class ScheduledControlTest(unittest.TestCase):
    def test_source_seed_waits_after_connection_reset_before_writing(self):
        with tempfile.TemporaryDirectory() as work:
            directory = Path(work)
            port = {"HostIp": "127.0.0.1", "HostPort": "49000"}
            container = {"NetworkSettings": {"Ports": {"9000/tcp": [port]}}}
            with (
                patch.dict(os.environ, ADDP_ONLINE_ARTIFACT_DIR=work),
                patch.object(CASE, "owner", return_value=(directory, "orchestrator-1-1")),
                patch.object(CASE, "source_container", side_effect=[None, container]),
                patch.object(CASE, "docker"),
                patch.object(CASE, "objectstore") as source,
                patch.object(CASE.time, "sleep"),
                patch("http.client.HTTPConnection.request") as request,
                patch("http.client.HTTPConnection.getresponse", side_effect=[
                    ConnectionResetError(104, "Connection reset by peer"), SimpleNamespace(status=200),
                ]),
            ):
                CASE.seed()
            self.assertEqual([request.args[:2] for request in request.call_args_list],
                             [("GET", "/minio/health/ready")] * 2)
            self.assertEqual(source.call_args_list, [call("mb"), call("pipe", key=CASE.KEY, data=CASE.SOURCE)])
            evidence = json.loads((directory / CASE.SOURCE_STARTUP_REPORT).read_text())
            self.assertEqual(evidence["readiness"], {
                "attempts": 2, "result": "ready",
                "first_not_ready": {"transport_error": "ConnectionResetError", "errno": 104},
                "last_probe": {"http_status": 200},
            })

    def test_source_seed_exits_without_writing_when_readiness_budget_expires(self):
        with tempfile.TemporaryDirectory() as work:
            directory = Path(work)
            port = {"HostIp": "127.0.0.1", "HostPort": "49000"}
            container = {"NetworkSettings": {"Ports": {"9000/tcp": [port]}}}
            with (
                patch.dict(os.environ, ADDP_ONLINE_ARTIFACT_DIR=work),
                patch.object(CASE, "owner", return_value=(directory, "orchestrator-1-1")),
                patch.object(CASE, "source_container", side_effect=[None, container]),
                patch.object(CASE, "docker"),
                patch.object(CASE, "objectstore") as source,
                patch.object(CASE.time, "monotonic", side_effect=[100, 100, 160, 160]),
                patch.object(CASE.time, "sleep"),
                patch("http.client.HTTPConnection.request"),
                patch("http.client.HTTPConnection.getresponse", return_value=SimpleNamespace(status=503)),
                self.assertRaisesRegex(CASE.API.SuiteError, "source readiness timed out"),
            ):
                CASE.seed()
            source.assert_not_called()
            evidence = json.loads((directory / CASE.SOURCE_STARTUP_REPORT).read_text())
            self.assertEqual(evidence["readiness"], {
                "attempts": 1, "result": "timed_out",
                "first_not_ready": {"http_status": 503}, "last_probe": {"http_status": 503},
            })

    def test_source_cleanup_archives_only_closed_startup_signals_before_removal(self):
        dangerous = b"FATAL secret=never-archive"
        container = {
            "Id": "8" * 64, "Mounts": [{"Type": "tmpfs"}], "RestartCount": 1,
            "State": {"Status": "exited", "ExitCode": 1, "OOMKilled": False, "Error": dangerous.decode()},
            "Config": {"Env": ["MINIO_ROOT_PASSWORD=never-archive"]},
        }
        for available in (True, False):
            with self.subTest(available=available), tempfile.TemporaryDirectory() as work:
                path = Path(work) / CASE.SOURCE_STARTUP_REPORT
                path.write_text('{"readiness":{"result":"timed_out"}}')
                logged = SimpleNamespace(stdout=b"MinIO Object Storage Server\nAPI: private\n" + dangerous)
                error = subprocess.TimeoutExpired("docker logs", 10)
                def before_remove(*args):
                    self.assertEqual(args, ("rm", "-fv", container["Id"]))
                    evidence = json.loads(path.read_text())
                    self.assertEqual(evidence["readiness"], {"result": "timed_out"})
                    self.assertEqual(evidence["container_before_removal"], {
                        "status": "exited", "oom_killed": False, "exit_code": 1,
                        "restart_count": 1, "state_error_present": True,
                    })
                    self.assertEqual(evidence["startup_log"]["available"], available)
                    if available:
                        self.assertTrue(evidence["startup_log"]["banner_seen"])
                        self.assertTrue(evidence["startup_log"]["api_announced"])
                        self.assertTrue(evidence["startup_log"]["error_signal_seen"])
                    self.assertNotIn("never-archive", path.read_text())
                    self.assertNotIn("private", path.read_text())

                with (
                    patch.dict(os.environ, ADDP_ONLINE_ARTIFACT_DIR=work),
                    patch.object(CASE, "source_container", side_effect=[container, None]) as inspect,
                    patch.object(CASE, "docker", side_effect=before_remove) as docker,
                    patch.object(CASE.subprocess, "run", return_value=logged, side_effect=None if available else error) as logs,
                ):
                    CASE.stop()
                docker.assert_called_once()
                self.assertEqual(inspect.call_count, 2)
                logs.assert_called_once_with(["docker", "logs", "--tail", "40", container["Id"]],
                                             stdout=subprocess.PIPE, stderr=subprocess.STDOUT, check=True, timeout=10)

    def test_exact_source_and_two_slots_with_month_rollover(self):
        self.assertEqual(len(CASE.SOURCE), 165)
        self.assertEqual(hashlib.sha256(CASE.SOURCE).hexdigest(),
                         "c4b518c151a098fe073aec79f3788257001c71115dcd84c08cd55ba85ebc4847")
        for minute in range(60):
            now = datetime(2026, 12, 31, 23, minute, 59, tzinfo=timezone.utc)
            first, second, cron = CASE.slots(now)
            self.assertGreaterEqual(first - now, timedelta(minutes=2))
            self.assertEqual(second - first, timedelta(minutes=8))
            self.assertLessEqual(second - now, timedelta(minutes=22))
            self.assertEqual((first.year, first.month, first.day, first.hour),
                             (second.year, second.month, second.day, second.hour))
            self.assertEqual(cron, f"CRON_TZ=UTC {first.minute},{second.minute} {first.hour} {first.day} {first.month} *")

    def test_issuance_event_excludes_private_headers_intent_and_response(self):
        request = json.dumps(dict(execution_id=CHILD, intent_digest="do-not-record")).encode()
        for status, payload, error in (
            (201, {"authorization": {"lease_secret": "do-not-record"}}, None),
            (403, {"error": "do-not-record", "error_code": "permission_denied"}, "permission_denied"),
            (403, {"error_code": "bearer example"}, "invalid"),
        ):
            with self.subTest(status=status, code=error):
                event = CASE.issue_event(request, status, json.dumps(payload).encode())
                self.assertEqual(event, dict(execution_id=CHILD, status=status, error_code=error))

    def test_grant_withdrawal_requires_receipt_and_history_and_never_retries(self):
        command = "60765b01-a7a0-4834-89cc-6a59165aebbc"
        receipt = {"request_id": command, "revoked_at": "2026-11-18T08:20:00Z", "revoked_request_ids": [command]}
        for lost in (True, False):
            with self.subTest(lost=lost):
                grants = object.__new__(CASE.ExactScheduledGrant)
                grants.authorizer = Mock()
                grants.pending = [{"engine": 42, "request_id": command, "target": TARGET, "confirmed": True}]
                grants.observe = Mock()
                report = {}
                grants.authorizer.request.side_effect = [
                    CASE.API.SuiteError("reply lost") if lost else SimpleNamespace(payload=receipt),
                    SimpleNamespace(payload={"total": 1, "data": [{"request_id": command, "revocation": receipt}]}),
                ]
                if lost:
                    with self.assertRaisesRegex(CASE.API.SuiteError, "reply lost"):
                        grants.withdraw(report, Mock())
                    with self.assertRaisesRegex(CASE.API.SuiteError, "already attempted"):
                        grants.withdraw(report, Mock())
                    self.assertEqual(grants.authorizer.request.call_count, 1)
                    self.assertNotIn("grant_revoked_at", report)
                else:
                    self.assertEqual(grants.withdraw(report, Mock()).isoformat(), "2026-11-18T08:20:00+00:00")
                    self.assertFalse(grants.pending)
                    grants.observe.assert_called_once_with(TARGET, False, phase="after_withdrawal")

    def test_dispatcher_report_is_emitted_even_when_the_first_action_fails(self):
        with tempfile.TemporaryDirectory() as work:
            directory = Path(work)
            report = directory / "orchestrator-scheduled-file.json"
            sha = "b" * 40
            (directory / "scheduled-build.json").write_text(json.dumps({"sha": sha}))
            env = {"ADDP_ONLINE_ARTIFACT_DIR": work, "ADDP_ONLINE_ORCHESTRATOR_SOURCE_COMMIT": sha,
                   "GATEWAY_URL": "http://127.0.0.1:8000", "ADDP_ONLINE_TEST_TENANT_ID": "3",
                   "ADDP_ONLINE_CONSUMER_ENGINE_ID": "42", "ADDP_ONLINE_TEST_USER_USERNAME": "scheduled",
                   "ADDP_ONLINE_TEST_USER_PASSWORD": "fixture-only", "ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN": "initializer"}
            client = Mock()
            client.request.return_value = SimpleNamespace(payload={"next_action": "session_issued",
                                                                  "session": {"access_token": "user"}})
            for passed in (True, False):
                with self.subTest(passed=passed), patch.dict(os.environ, env, clear=True), \
                        patch.object(CASE, "owner", return_value=(directory, "orchestrator-1-1")), \
                        patch.object(CASE.API, "GatewayClient", return_value=client), \
                        patch.object(CASE.API, "validate_user_identity"), patch.object(CASE.HOST, "HostedFaults"), \
                        patch.object(CASE.signal, "signal"), redirect_stdout(io.StringIO()) as out, \
                        patch.object(CASE, "case", side_effect=None if passed else CASE.API.SuiteError("first action failed")):
                    self.assertEqual(CASE.run(), 0 if passed else 1)
                    evidence = json.loads(report.read_text())
                    self.assertEqual(json.loads(out.getvalue()), evidence)
                    self.assertEqual(evidence["result"], "passed" if passed else "failed")
                    if not passed:
                        self.assertEqual(evidence["first_failure"], "first action failed")
                    self.assertNotIn("fixture-only", out.getvalue())

    def test_positive_and_negative_require_a_durable_child_with_correct_origin(self):
        parent = dict(execution_id=PARENT, attempt=1, actor_principal_id=83,
                      actor_tenant_membership_id=104, issued_authorization_version=2)
        rejected = dict(id=CHILD, parent=PARENT, expected_child=True, status="failed", trigger="scheduled",
                        attempt=0, actor=83, membership=104, version=2, code="develop.workflow.file_authorization_rejected",
                        parent_claim={"execution_id": PARENT, "attempt": 1}, no_output=True, started=False,
                        admission="rejected", scope=None, source_reads=None, runtime=False, issued=False,
                        private_admission=False, runtime_status=None, input_directory="")
        self.assertEqual(CASE.check_execution({"children": [rejected]}, parent, IDENTITY, False, TARGET, "7", GRANT), CHILD)
        for change in ({"attempt": 1}, {"issued": True}, {"runtime": True}, {"expected_child": False},
                       {"parent_claim": None}, {"source_reads": []}, {"status": "success"},
                       {"no_output": False}, {"membership": 88}):
            with self.subTest(change=change), self.assertRaises(CASE.API.SuiteError):
                CASE.check_execution({"children": [dict(rejected, **change)]}, parent, IDENTITY, False, TARGET, "7", GRANT)
        for children in ([], [rejected, rejected]):
            with self.assertRaisesRegex(CASE.API.SuiteError, "one durable child"):
                CASE.check_execution({"children": children}, parent, IDENTITY, False, TARGET, "7", GRANT)

        with tempfile.TemporaryDirectory() as directory:
            staging = Path(directory) / "addp-develop-inputs-owned"
            success = dict(rejected, status="success", attempt=1, started=True, admission="ready", code=None,
                           scope={"sources": [dict(GRANT, target=TARGET, engine_version="7", format="geojson")]},
                           source_reads=[dict(node_id="load", engine_version="7", read_set={"paths": [TARGET]})],
                           runtime=True, runtime_status="success", issued=True, private_admission=True, input_directory=str(staging))
            for change in ({}, {"scope": {"sources": []}}, {"source_reads": []}):
                with self.subTest(change=change):
                    observed = {"children": [dict(success, **change)]}
                    if change:
                        with self.assertRaises(CASE.API.SuiteError):
                            CASE.check_execution(observed, parent, IDENTITY, True, TARGET, "7", GRANT)
                    else:
                        self.assertEqual(CASE.check_execution(observed, parent, IDENTITY, True, TARGET, "7", GRANT), CHILD)
            staging.mkdir()
            with patch.object(CASE.time, "monotonic", side_effect=(0, 11)), \
                 self.assertRaisesRegex(CASE.API.SuiteError, "directory was not released"):
                CASE.check_execution({"children": [success]}, parent, IDENTITY, True, TARGET, "7", GRANT)


if __name__ == "__main__":
    unittest.main()
