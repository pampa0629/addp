import json
import os
import socket
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class DocumentNativeLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-document-lifecycle-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.runtime = self.root / "engines/document-workflow"
        self.runtime.mkdir(parents=True)
        (self.runtime / "venv/bin").mkdir(parents=True)
        (self.runtime / "venv/bin/python").symlink_to(sys.executable)
        (self.runtime / "native_setup.py").write_text('''
import os, sys
from pathlib import Path
action = sys.argv[1]
with Path(__file__).with_name('native-trace').open('a') as trace:
    trace.write(action + '\\n')
if action not in ('environment', 'current', 'prepare', 'check'): sys.exit(99)
if action == 'environment': print('/private/soffice\\n/private/fonts.conf')
if action == 'check' and os.environ.get('FAIL_CHECK'): sys.exit(1)
''')

    def shell(self, code, **environment):
        return subprocess.run(["bash", "-c", 'set -euo pipefail; source "$SOURCE_ROOT/scripts/dev/document-workflow.sh"; ' + code],
                              env={**os.environ, "ROOT_DIR": str(self.root), "SOURCE_ROOT": str(ROOT), **environment},
                              capture_output=True, text=True, timeout=15)

    def test_external_listener_prevents_dependency_mutation(self):
        result = self.shell('''
addp_dev_port_busy() { return 0; }
addp_dev_owned_listener() { return 1; }
addp_with_python_dependency_lock() { echo unexpected-install; return 99; }
addp_prepare_document_workflow
''')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("外部监听者", result.stderr)
        self.assertNotIn("unexpected-install", result.stdout)

    def test_live_python_drift_requires_stop(self):
        result = self.shell('''
addp_dev_port_busy() { return 0; }
addp_dev_owned_listener() { return 0; }
docker() { return 1; }
id() { echo 1001; }
addp_python_dependency_fingerprint() { echo changed; }
addp_python_dependencies_current() { return 1; }
addp_with_python_dependency_lock() { echo unexpected-install; return 99; }
addp_prepare_document_workflow
''')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("先停止", result.stderr)
        self.assertNotIn("unexpected-install", result.stdout)

    def test_retained_container_cannot_create_second_route_on_spare_port(self):
        result = self.shell('''
addp_dev_port_busy() { return 1; }
docker() { return 0; }
addp_with_python_dependency_lock() { echo unexpected-install; return 99; }
addp_prepare_document_workflow
''')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("删除旧 Document", result.stderr)
        self.assertNotIn("unexpected-install", result.stdout)

    def test_private_environment_clears_inherited_paths_and_gateway(self):
        result = self.shell("addp_document_native_environment; python3 -c 'import json,os; print(json.dumps(dict(os.environ)))'",
                            FONTCONFIG_PATH="/foreign", DYLD_LIBRARY_PATH="/foreign", PYTHONPATH="/foreign",
                            DOCUMENT_OBJECT_STORE_LOOPBACK_HOST="host.docker.internal", RUNTIME_PUBLIC_PORT="9999")
        self.assertEqual(result.returncode, 0, result.stderr)
        values = json.loads(result.stdout)
        self.assertEqual(values["DOCUMENT_LIBREOFFICE_BIN"], "/private/soffice")
        self.assertEqual(values["FONTCONFIG_FILE"], "/private/fonts.conf")
        for key in ("FONTCONFIG_PATH", "DYLD_LIBRARY_PATH", "PYTHONPATH", "DOCUMENT_OBJECT_STORE_LOOPBACK_HOST", "RUNTIME_PUBLIC_PORT"):
            self.assertNotIn(key, values)

    def test_preparation_checks_dependencies_without_converting(self):
        (self.runtime / "api_server.py").write_text("# Importable runtime without any conversion fixture.\n")
        for active in ("0", "1"):
            with self.subTest(active=active):
                trace = self.runtime / "native-trace"
                trace.unlink(missing_ok=True)
                result = self.shell('''
addp_dev_port_busy() { [ "$ACTIVE" = 1 ]; }
addp_dev_owned_listener() { return 0; }
docker() { return 1; }
id() { echo 1001; }
addp_document_python_current() { return 0; }
addp_python_dependency_fingerprint() { echo unchanged; }
addp_python_dependencies_current() { return 0; }
addp_with_python_dependency_lock() { echo prepare-dependencies; }
addp_sync_python_dependencies() { echo sync-dependencies; }
addp_prepare_document_workflow
''', ACTIVE=active)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(trace.read_text().splitlines(),
                                 ["environment", "current", "check"] if active == "1" else ["environment", "check"])
                self.assertEqual("sync-dependencies" in result.stdout, active == "0")
                result = self.shell('''
addp_dev_port_busy() { return 0; }
addp_dev_owned_listener() { return 0; }
docker() { return 1; }
id() { echo 1001; }
addp_document_python_current() { return 0; }
addp_python_dependency_fingerprint() { echo unchanged; }
addp_python_dependencies_current() { return 0; }
addp_prepare_document_workflow
''', FAIL_CHECK="1")
                self.assertNotEqual(result.returncode, 0, "failed native check must block preparation")

    def test_native_pid_readiness_and_failed_launch_cleanup(self):
        (self.runtime / "api_server.py").write_text('''
import http.server, json, os, sys
if os.environ.get('FAIL_START'): sys.exit(1)
if os.environ['WORKFLOW_BIND_HOST'] != '127.0.0.1': sys.exit(2)
if 'DOCUMENT_OBJECT_STORE_LOOPBACK_HOST' in os.environ: sys.exit(3)
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200); self.end_headers()
        self.wfile.write(json.dumps({'status': 'healthy'}).encode())
http.server.HTTPServer(('127.0.0.1', int(os.environ['PORT'])), Handler).serve_forever()
''')
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = str(sock.getsockname()[1])
        code = '''
source "$SOURCE_ROOT/scripts/dev/ports.sh"
source "$SOURCE_ROOT/scripts/dev/lifecycle-lock.sh"
trap 'if [ -f "$ROOT_DIR/.dev-pids/document-workflow-engine.pid" ]; then kill "$(cat "$ROOT_DIR/.dev-pids/document-workflow-engine.pid")" 2>/dev/null || true; fi' EXIT
addp_launch_document_workflow
'''
        result = self.shell(code, DOCUMENT_WORKFLOW_PORT=port, DOCUMENT_OBJECT_STORE_LOOPBACK_HOST='foreign')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        pidfile = self.root / '.dev-pids/document-workflow-engine.pid'
        self.assertTrue(pidfile.read_text().strip().isdigit())
        pidfile.unlink()
        result = self.shell(code, DOCUMENT_WORKFLOW_PORT=port, FAIL_START='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(pidfile.exists())


if __name__ == "__main__":
    unittest.main()
