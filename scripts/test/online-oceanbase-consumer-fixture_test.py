import json
import os
import subprocess
import tempfile
import unittest
from importlib import import_module
from pathlib import Path
from unittest.mock import Mock, patch

NODE = import_module("scripts.test.database-node-read-online")
FIXTURE = Path(__file__).resolve().parents[2] / "business/scripts/online-oceanbase-consumer-fixture.sh"


class OceanBaseFixtureTest(unittest.TestCase):
    def test_refuses_persistent_runner_before_docker(self):
        result = subprocess.run(["bash", FIXTURE, "start"], capture_output=True, text=True,
                                env={"PATH": os.defpath, "ADDP_ONLINE_HOST": "1"})
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires the disposable GitHub Hosted Linux suite", result.stderr)

    def test_hosted_lifecycle_and_foreign_container(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            for name, content in {
                "uname": "echo Linux",
                "docker": """case "$1" in
  container) test -f "$STATE" ;;
  inspect) [[ "$*" != *Config.Labels* ]] || cat "$STATE" ;;
  run) echo "$OWNER" > "$STATE" ;;
  exec) cat >> "$INPUT"; case "$*" in
    *'updated_at >'*) echo 2 ;;
    *'COUNT(*)'*source*) echo 5 ;;
    *'COUNT(*)'*target*) echo 0 ;;
    *'SELECT 1'*) echo 1 ;;
  esac ;;
  port) echo 127.0.0.1:52123 ;;
  rm) rm "$STATE" ;;
  ps) test ! -f "$STATE" || echo existing ;;
esac""",
            }.items():
                tool = root / name
                tool.write_text("#!/bin/bash\n" + content + "\n")
                tool.chmod(0o755)
            state, input_sql, descriptor = root / "state", root / "input", root / "engine.json"
            env = dict(os.environ, PATH=tmp+":"+os.environ["PATH"], STATE=str(state), INPUT=str(input_sql),
                       OWNER="oceanbase-consumer-flow:12:1", GITHUB_RUN_ID="12", GITHUB_RUN_ATTEMPT="1",
                       ADDP_ONLINE_HOST="1", ADDP_ONLINE_HOSTED="1", GITHUB_ACTIONS="true", RUNNER_OS="Linux",
                       ONLINE_SUITE_INPUT="oceanbase-consumer-flow", ADDP_ONLINE_SECRET_DIR=tmp,
                       ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(descriptor))
            def run(action):
                return subprocess.run(["bash", FIXTURE, action], env=env, stdin=subprocess.DEVNULL,
                                      capture_output=True, text=True, timeout=10)
            for action in ("start", "create-future", "advance", "stop"):
                result = run(action)
                self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse(state.exists())
            self.assertEqual(descriptor.stat().st_mode & 0o777, 0o600)
            connection = NODE.REG.load_descriptor(descriptor)["connection_info"]
            self.assertEqual((connection["user"], connection["port"]), ("ob_consumer@test", 52123))
            sql = input_sql.read_text()
            for fragment in ("ENGINE=InnoDB", "CREATE TABLE sales.node_future", "CREATE TABLE outside_sales.node_future",
                             "WHERE id = 2", "VALUES (6, 'OB-1006'", "GRANT SELECT ON outside_sales.*"):
                self.assertIn(fragment, sql)
            state.write_text("foreign")
            self.assertNotEqual(run("stop").returncode, 0)
            self.assertEqual(state.read_text(), "foreign")

    def test_grant_order_and_revocation_before_the_last_preview(self):
        preparer, consumer = Mock(), Mock()
        events = []
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            journal = root / "pending.json"
            def issue(method, path, statuses, body):
                if path.endswith("/revoke"):
                    events.append("revoke")
                    return
                self.assertEqual(json.loads(journal.read_text())[-1]["request_id"], body["request_id"])
                self.assertEqual(body["catalog_path"]["segments"][-1]["name"], "sales")
                self.assertEqual((body["action"], body["node_recursive"]), ("read", False))
                events.append("grant")
                return Mock(payload=dict(body, engine_id="5", operator_principal_id="31"))
            preparer.request.side_effect = issue
            with (
                patch.object(NODE.REG, "source_grant_participants", return_value=("31", "32")),
                patch.object(NODE.FLOW, "validate_user_identity"),
                patch.object(NODE.REG, "initialize_exact_record_read_grants") as exact,
                patch.object(NODE.FLOW, "wait_for_scan", side_effect=lambda *a: events.append("scan") or "scanned"),
                patch.object(NODE.subprocess, "run", side_effect=lambda *a, **k: events.append("create")),
                patch.object(NODE, "observe_preview", side_effect=lambda *a: events.append(a[3])),
            ):
                NODE.verify(preparer, consumer, 3, 5, journal, root, "oceanbase")
            self.assertEqual(exact.call_args.args[3], [
                (5, "database", "oceanbase_fixture", NODE.FLOW.SOURCE_TABLE),
                (5, "database", "oceanbase_fixture", NODE.FLOW.TARGET_TABLE),
            ])
            self.assertEqual(events, ["scan", "before_grant", "grant", "existing", "create",
                                      "scan", "future", "same_name_outside", "revoke", "revoked"])
            report = json.loads((root / "oceanbase-node-read.json").read_text())
            self.assertEqual(report["develop_per_table_authorization"], "not_verified")

    def test_manager_response_and_native_source_counts(self):
        for expected, delta, valid in [(37, 1, True), (37, 0, False), (None, 0, True), (None, 1, False)]:
            checks, consumer = {}, Mock()
            with self.subTest(expected=expected, delta=delta), patch.object(
                NODE.subprocess, "check_output", side_effect=["127", str(delta)]
            ), patch.object(NODE.FLOW.SUPPORT, "preview_rows", return_value=(["id"], [{"id": 37}])):
                if valid:
                    NODE.observe_preview(consumer, 5, checks, "sample", "sales", "node_existing", expected)
                else:
                    with self.assertRaises(NODE.FLOW.SuiteError):
                        NODE.observe_preview(consumer, 5, checks, "sample", "sales", "node_existing", expected)
                self.assertEqual(checks["sample"]["source_selects"], delta)
                if expected is None:
                    self.assertEqual(consumer.request.call_args.args[2], (403,))
