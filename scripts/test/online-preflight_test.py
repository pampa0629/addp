import importlib.util
import json
import os
import subprocess
import sys
import tempfile
import threading
import unittest
from argparse import Namespace
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("online-preflight.py")
SPEC = importlib.util.spec_from_file_location("online_preflight", SCRIPT)
ONLINE_PREFLIGHT = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE_PREFLIGHT
SPEC.loader.exec_module(ONLINE_PREFLIGHT)


class HealthHandler(BaseHTTPRequestHandler):
    commit = ""

    def do_GET(self):
        payload = {
            "status": "ok",
            "module": "system",
            "build_id": "build-1",
            "git_commit": self.commit,
            "source_fingerprint": "sha256:test",
            "built_at": "2026-08-21T00:00:00Z",
            "started_at": "2026-08-21T00:00:01Z",
        }
        body = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_args):
        return


class OnlinePreflightTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.repository = Path(self.temp.name)
        subprocess.run(["git", "init", "-q", str(self.repository)], check=True)
        subprocess.run(["git", "-C", str(self.repository), "config", "user.name", "test"], check=True)
        subprocess.run(["git", "-C", str(self.repository), "config", "user.email", "test@example.com"], check=True)
        (self.repository / "tracked").write_text("clean\n")
        subprocess.run(["git", "-C", str(self.repository), "add", "tracked"], check=True)
        subprocess.run(["git", "-C", str(self.repository), "commit", "-qm", "initial"], check=True)
        HealthHandler.commit = subprocess.run(
            ["git", "-C", str(self.repository), "rev-parse", "HEAD"],
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), HealthHandler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.temp.cleanup()

    def args(self):
        return Namespace(
            environment_only=False,
            repository=self.repository,
            service=[f"system=http://127.0.0.1:{self.server.server_port}"],
            timeout=2.0,
        )

    @patch.dict(
        os.environ,
        {
            "ADDP_ONLINE_TEST": "1",
            "ADDP_ONLINE_TEST_TENANT_ID": "42",
            "ADDP_ONLINE_TEST_RUN_ID": "run-42",
            "POSTGRES_DB": "addp_online",
        },
        clear=True,
    )
    def test_accepts_clean_loopback_deployment_with_matching_build(self):
        report = ONLINE_PREFLIGHT.run_preflight(self.args())
        self.assertEqual(report["network_boundary"], "loopback-only")
        self.assertEqual(report["services"]["system"]["git_commit"], HealthHandler.commit)

    def test_rejects_non_loopback_service(self):
        with self.assertRaisesRegex(ONLINE_PREFLIGHT.PreflightError, "loopback"):
            ONLINE_PREFLIGHT.parse_service("system=https://example.com")

    def test_requires_explicit_valid_service_port(self):
        for declaration in (
            "system=http://localhost",
            "system=http://localhost:0",
            "system=http://localhost:invalid",
        ):
            with self.subTest(declaration=declaration):
                with self.assertRaisesRegex(ONLINE_PREFLIGHT.PreflightError, "port"):
                    ONLINE_PREFLIGHT.parse_service(declaration)

    def test_rejects_non_positive_timeout(self):
        args = self.args()
        args.timeout = 0
        with self.assertRaisesRegex(ONLINE_PREFLIGHT.PreflightError, "timeout"):
            ONLINE_PREFLIGHT.run_preflight(args)

    @patch.dict(
        os.environ,
        {
            "ADDP_ONLINE_TEST": "1",
            "ADDP_ONLINE_TEST_TENANT_ID": "1",
            "ADDP_ONLINE_TEST_RUN_ID": "run-1",
            "POSTGRES_DB": "addp_online",
        },
        clear=True,
    )
    def test_rejects_default_tenant(self):
        with self.assertRaisesRegex(ONLINE_PREFLIGHT.PreflightError, "non-default Tenant"):
            ONLINE_PREFLIGHT.run_preflight(self.args())

    @patch.dict(
        os.environ,
        {
            "ADDP_ONLINE_TEST": "1",
            "ADDP_ONLINE_TEST_TENANT_ID": "42",
            "ADDP_ONLINE_TEST_RUN_ID": "run-42",
            "POSTGRES_DB": "addp_online",
        },
        clear=True,
    )
    def test_rejects_dirty_repository(self):
        (self.repository / "tracked").write_text("dirty\n")
        with self.assertRaisesRegex(ONLINE_PREFLIGHT.PreflightError, "clean repository"):
            ONLINE_PREFLIGHT.run_preflight(self.args())

    @patch.dict(
        os.environ,
        {
            "ADDP_ONLINE_TEST": "1",
            "ADDP_ONLINE_TEST_TENANT_ID": "42",
            "ADDP_ONLINE_TEST_RUN_ID": "run-42",
            "POSTGRES_DB": "addp_online",
        },
        clear=True,
    )
    def test_rejects_service_built_from_another_commit(self):
        HealthHandler.commit = "0" * 40
        with self.assertRaisesRegex(ONLINE_PREFLIGHT.PreflightError, "git_commit"):
            ONLINE_PREFLIGHT.run_preflight(self.args())

    @patch.dict(
        os.environ,
        {
            "ADDP_ONLINE_TEST": "1",
            "ADDP_ONLINE_TEST_TENANT_ID": "42",
            "POSTGRES_DB": "addp_online",
        },
        clear=True,
    )
    def test_requires_explicit_run_id(self):
        with self.assertRaisesRegex(ONLINE_PREFLIGHT.PreflightError, "RUN_ID"):
            ONLINE_PREFLIGHT.run_preflight(self.args())

    @patch.dict(
        os.environ,
        {
            "ADDP_ONLINE_TEST": "1",
            "ADDP_ONLINE_TEST_TENANT_ID": "42",
            "ADDP_ONLINE_TEST_RUN_ID": "run-42",
            "POSTGRES_DB": "addp",
        },
        clear=True,
    )
    def test_rejects_default_business_database(self):
        with self.assertRaisesRegex(ONLINE_PREFLIGHT.PreflightError, "addp_online"):
            ONLINE_PREFLIGHT.run_preflight(self.args())

    @patch.dict(
        os.environ,
        {
            "ADDP_ONLINE_TEST": "1",
            "ADDP_ONLINE_TEST_TENANT_ID": "42",
            "POSTGRES_DB": "addp_online",
        },
        clear=True,
    )
    def test_environment_only_does_not_require_run_id_or_services(self):
        report = ONLINE_PREFLIGHT.validate_online_environment(require_run_id=False)
        self.assertEqual(report["database"], "addp_online")
        self.assertEqual(report["tenant_id"], "42")


if __name__ == "__main__":
    unittest.main()
