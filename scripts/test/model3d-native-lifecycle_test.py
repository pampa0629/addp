import json
import os
import socket
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class Model3DNativeLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-model3d-lifecycle-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.runtime = self.root / "engines/model3d-workflow"
        self.runtime.mkdir(parents=True)
        (self.runtime / "venv/bin").mkdir(parents=True)
        (self.runtime / "venv/bin/python").symlink_to(sys.executable)
        (self.runtime / "native_setup.py").write_text("import sys; print('/private/tools') if sys.argv[1] == 'environment' else None\n")
        (self.runtime / "blender_setup.py").write_text("import sys; print('/private/blender') if sys.argv[1] == 'environment' else None\n")

    def shell(self, code, **environment):
        return subprocess.run(["bash", "-c", 'set -euo pipefail; source "$SOURCE_ROOT/scripts/dev/model3d-workflow.sh"; ' + code],
                              env={**os.environ, "ROOT_DIR": str(self.root), "SOURCE_ROOT": str(ROOT), **environment},
                              capture_output=True, text=True, timeout=15)

    def test_start_and_restart_have_one_native_owner(self):
        start = (ROOT / 'scripts/dev/start.sh').read_text()
        restart = (ROOT / 'scripts/dev/restart.sh').read_text()
        self.assertIn('source "$ROOT_DIR/scripts/dev/model3d-workflow.sh"', start)
        self.assertNotIn('addp_prepare_model3d_workflow', restart)
        self.assertIn('exec env SKIP_MODTIDY=1', restart)
        hosted = (ROOT / 'scripts/test/online-hosted-manager-gate.sh').read_text()
        self.assertIn('start_online_model3d_runtime', hosted)
        self.assertNotIn('-document-workflow -model3d-workflow', hosted)
        self.assertFalse((ROOT / 'engines/model3d-workflow/scripts/converters').exists())

    def test_external_listener_prevents_dependency_mutation(self):
        result = self.shell('''
addp_dev_port_busy() { return 0; }
addp_dev_owned_listener() { return 1; }
addp_with_python_dependency_lock() { echo unexpected-install; return 99; }
addp_prepare_model3d_workflow
''')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("外部监听者", result.stderr)
        self.assertNotIn("unexpected-install", result.stdout)

    def test_live_python_drift_requires_stop(self):
        result = self.shell('''
addp_dev_port_busy() { return 0; }
addp_dev_owned_listener() { return 0; }
docker() { return 1; }
addp_python_dependency_fingerprint() { echo changed; }
addp_python_dependencies_current() { return 1; }
addp_with_python_dependency_lock() { echo unexpected-install; return 99; }
addp_prepare_model3d_workflow
''')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("先停止", result.stderr)
        self.assertNotIn("unexpected-install", result.stdout)

    def test_retained_container_cannot_create_second_route_on_spare_port(self):
        result = self.shell('''
addp_dev_port_busy() { return 1; }
docker() { return 0; }
addp_with_python_dependency_lock() { echo unexpected-install; return 99; }
addp_prepare_model3d_workflow
''')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("删除旧 Model3D", result.stderr)
        self.assertNotIn("unexpected-install", result.stdout)

    def test_private_environment_clears_inherited_paths_and_gateway(self):
        result = self.shell("addp_model3d_native_environment; python3 -c 'import json,os; print(json.dumps(dict(os.environ)))'",
                            GDAL_DATA="/foreign", DYLD_LIBRARY_PATH="/foreign", PYTHONPATH="/foreign",
                            MODEL3D_CONVERTER_IMAGE="host.docker.internal", RUNTIME_PUBLIC_PORT="9999")
        self.assertEqual(result.returncode, 0, result.stderr)
        values = json.loads(result.stdout)
        self.assertEqual(values["MODEL3D_CONVERTER_BIN"], "/private/tools/bin/_3dtile")
        self.assertEqual(values["PROJ_DATA"], "/private/tools/bin/proj")
        for key in ("DYLD_LIBRARY_PATH", "PYTHONPATH", "MODEL3D_CONVERTER_IMAGE", "RUNTIME_PUBLIC_PORT"):
            self.assertNotIn(key, values)

    def test_native_pid_readiness_and_failed_launch_cleanup(self):
        (self.runtime / "api_server.py").write_text('''
import http.server, json, os, sys
if os.environ.get('FAIL_START'): sys.exit(1)
if os.environ['WORKFLOW_BIND_HOST'] != '127.0.0.1': sys.exit(2)
if 'MODEL3D_CONVERTER_IMAGE' in os.environ: sys.exit(3)
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
trap 'if [ -f "$ROOT_DIR/.dev-pids/model3d-workflow-engine.pid" ]; then kill "$(cat "$ROOT_DIR/.dev-pids/model3d-workflow-engine.pid")" 2>/dev/null || true; fi' EXIT
addp_launch_model3d_workflow
'''
        result = self.shell(code, MODEL3D_WORKFLOW_PORT=port, MODEL3D_CONVERTER_IMAGE='foreign')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        pidfile = self.root / '.dev-pids/model3d-workflow-engine.pid'
        self.assertTrue(pidfile.read_text().strip().isdigit())
        pidfile.unlink()
        result = self.shell(code, MODEL3D_WORKFLOW_PORT=port, FAIL_START='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(pidfile.exists())


class Model3DSKPRepairLifecycleTests(unittest.TestCase):
    shell = Model3DNativeLifecycleTests.shell
    def setUp(self):
        Model3DNativeLifecycleTests.setUp(self)
        (self.runtime / 'api_server.py').write_text('')
        (self.runtime / 'node_modules/@mkkellogg/gaussian-splats-3d').mkdir(parents=True)
        (self.runtime / 'skp_dependency.py').write_text('''
import os, sys
from pathlib import Path
operation = sys.argv[1]
if operation == 'prepare' and os.environ.get('ADDP_TEST_LOCK') != '1':
    raise RuntimeError('repair preparation requires dependency lock')
with Path(os.environ['ROOT_DIR'], 'repair-calls').open('a') as stream:
    stream.write(operation + '\\n')
if operation == 'verify' and os.environ.get('REPAIR_DRIFT'):
    raise RuntimeError('repair missing or changed')
''')

    def test_stopped_runtime_prepares_under_dependency_lock_then_verifies(self):
        result = self.shell('''
addp_dev_port_busy() { return 1; }
docker() { return 1; }
addp_model3d_native_environment() { :; }
addp_model3d_python_current() { :; }
addp_sync_python_dependencies() { :; }
addp_with_python_dependency_lock() {
  shift
  if [ "$1" = bash ]; then return 0; fi
  ADDP_TEST_LOCK=1 "$@"
}
addp_prepare_model3d_workflow
''')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / 'repair-calls').read_text(), 'prepare\nverify\n')

    def test_active_runtime_only_verifies_and_refuses_drift(self):
        result = self.shell('''
addp_dev_port_busy() { return 0; }
addp_dev_owned_listener() { return 0; }
docker() { return 1; }
addp_model3d_native_environment() { :; }
addp_model3d_python_current() { :; }
addp_python_dependency_fingerprint() { echo current; }
addp_python_dependencies_current() { return 0; }
addp_with_python_dependency_lock() { echo unexpected-install; return 99; }
addp_prepare_model3d_workflow
''', REPAIR_DRIFT='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('先停止', result.stderr)
        self.assertNotIn('unexpected-install', result.stdout)
        self.assertEqual((self.root / 'repair-calls').read_text(), 'verify\n')


if __name__ == "__main__":
    unittest.main()
