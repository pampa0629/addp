"""Exercise observer rejection against actual local HTTP; no product acceptance claim."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from importlib import import_module
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import unittest
from unittest.mock import patch

scenario = import_module("scripts.test.develop-notebook-storage-online")


class NotebookObserverTest(unittest.TestCase):
    def setUp(self):
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                cookie = self.headers.get("Cookie", "")
                if self.path == "/open":
                    self.send_response(200)
                    self.send_header("Set-Cookie", "addp_notebook_session=private; Path=/owned/")
                    self.end_headers()
                    self.wfile.write(b'{}')
                elif self.path == "/owned/bytes" and "addp_notebook_session=private" in cookie:
                    self.send_response(200)
                    self.end_headers()
                    self.wfile.write(b'\x00notebook\xff')
                else:
                    self.send_response(401)
                    self.end_headers()

            def log_message(self, *args):
                pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.environment = patch.dict(os.environ, {"GATEWAY_URL": "http://127.0.0.1:" + str(self.server.server_port)})
        self.environment.start()
        self.addCleanup(self.environment.stop)
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)

    def test_cookie_scope_and_binary_bytes_are_preserved(self):
        client = scenario.Client("ordinary-user")
        client.request("GET", "/open", (200,))
        self.assertEqual(client.request("GET", "/owned/bytes", (200,), raw=True), b'\x00notebook\xff')
        self.assertEqual(client.cookie_header("/other/bytes"), "")
        self.assertNotEqual(client.cookie_header("/owned/bytes"), "")

    def test_wrong_identity_status_cannot_pass(self):
        client = scenario.Client("other-user")
        with self.assertRaises(scenario.api.SuiteError):
            client.request("GET", "/owned/bytes", (200,), raw=True)
        self.assertEqual(client.request("GET", "/owned/bytes", (401,)), {})

    def test_same_size_corruption_cannot_pass(self):
        with self.assertRaises(scenario.api.SuiteError):
            scenario.check_bytes(b"abc", b"abd", "cross-language")
        self.assertEqual(scenario.check_bytes(b"abc", b"abc", "cross-language")["bytes"], 3)

    def test_reachable_workspace_never_counts_as_cleaned(self):
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            self.assertFalse(scenario.process_gone({"workspace": directory, "processes": {}}))
        self.assertTrue(scenario.process_gone({"workspace": directory, "processes": {}}))

    def test_hosted_registration_pins_product_and_preserves_standard_lifecycle(self):
        root = Path(__file__).resolve().parents[2]
        script = (root / "scripts/test/online-hosted-notebook-storage-gate.sh").read_text()
        for fragment in ('source "$ROOT_DIR/scripts/utils/hosted-online.sh"',
                         'git -C "$ROOT_DIR" rev-parse HEAD', 'JUPYTER_SESSION_TTL_SECONDS=300',
                         'bash scripts/dev/start.sh -jupyter', 'bash scripts/dev/start.sh -develop',
                         'develop=notebook-http-documents', 'make -C "$HARNESS" test-online'):
            self.assertIn(fragment, script)
        self.assertNotIn("keepalive.sh", script)
        self.assertNotIn("--exact-process -jupyter", script)


class NotebookFixtureCompileTest(unittest.TestCase):
    def test_identity_patch_compiles_without_modifying_product_checkout(self):
        root = Path(__file__).resolve().parents[2]
        with tempfile.TemporaryDirectory(prefix="addp-notebook-identity-check-") as directory:
            checkout = Path(directory) / "identity"
            subprocess.run(["git", "clone", "--shared", "--no-checkout", str(root), str(checkout)], check=True, capture_output=True)
            subprocess.run(["git", "checkout", "--detach", "HEAD"], cwd=checkout, check=True, capture_output=True)
            subprocess.run(["git", "apply", "--unidiff-zero", str(root / "scripts/test/develop-notebook-identity.patch")], cwd=checkout, check=True, capture_output=True)
            test_file = checkout / "system/backend/cmd/online-test-fixture/notebook_storage_fixture_test.go"
            test_file.write_text('''package main
import ("testing"; "reflect")
func TestNotebookStoragePermissionBoundary(t *testing.T) {
    got, err := suitePermissions("develop-notebook-storage")
    expected := []string{"develop.notebook.create", "develop.notebook.read", "develop.notebook.update", "develop.notebook.delete", "develop.task.read", "system.engine.read"}
    if err != nil || !reflect.DeepEqual(got,expected) { t.Fatalf("unexpected ordinary-user permissions: %v %v",got,err) }
    if len(suiteSourceInitializerPermissions("develop-notebook-storage")) != 0 { t.Fatal("Notebook fixture expanded source initializer permissions") }
}
''')
            result = subprocess.run(["go", "test", "./cmd/online-test-fixture", "-run", "^TestNotebookStoragePermissionBoundary$", "-count=1"],
                                    cwd=checkout / "system/backend", env={**os.environ, "GOWORK": "off"}, capture_output=True, text=True, timeout=240)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
