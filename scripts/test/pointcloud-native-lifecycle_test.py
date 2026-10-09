import json
import os
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class PointCloudNativeLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='addp-pointcloud-lifecycle-')
        self.addCleanup(self.temporary.cleanup)
        self.work = Path(self.temporary.name)
        self.runtime = self.work / 'engines/pointcloud-workflow'
        self.runtime.mkdir(parents=True)
        for name in ('native-packages.txt',):
            shutil.copy2(ROOT / 'engines/pointcloud-workflow' / name, self.runtime / name)
        scripts = self.work / 'scripts/dev'
        scripts.mkdir(parents=True)
        shutil.copy2(ROOT / 'scripts/dev/pointcloud-workflow.sh', scripts / 'pointcloud-workflow.sh')
        self.environment = dict(os.environ, ROOT_DIR=str(self.work), SOURCE_ROOT=str(ROOT))
        self.helper = 'source "$SOURCE_ROOT/scripts/dev/pointcloud-workflow.sh"; '

    def run_shell(self, code, **environment):
        return subprocess.run(['bash', '-c', 'set -euo pipefail; ' + self.helper + code],
                              env=dict(self.environment, **environment), capture_output=True,
                              text=True, timeout=15)

    def populate_packages(self, e57='2.10.2'):
        metadata = self.runtime / 'venv/conda-meta'
        metadata.mkdir(parents=True, exist_ok=True)
        for name, version in {'python': '3.12.12', 'pip': '25.3', 'libpdal-core': '2.10.2', 'libpdal-e57': e57}.items():
            (metadata / (name + '.json')).write_text(json.dumps(dict(name=name, version=version)))

    def test_native_versions_and_unknown_venv(self):
        self.assertNotEqual(self.run_shell('addp_pointcloud_packages_current').returncode, 0)
        prefix = self.runtime / 'venv'
        prefix.mkdir()
        result = self.run_shell('addp_install_pointcloud_native_packages')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('不是独立 Conda', result.stderr)
        self.assertTrue(prefix.exists())
        self.populate_packages()
        self.assertEqual(self.run_shell('addp_pointcloud_packages_current').returncode, 0)
        self.populate_packages(e57='2.9.0')
        self.assertNotEqual(self.run_shell('addp_pointcloud_packages_current').returncode, 0)

    def test_private_environment_erases_foreign_native_resources(self):
        result = self.run_shell('addp_pointcloud_native_environment; python3 -c \'import json,os; print(json.dumps(dict(os.environ)))\'',
                                PROJ_LIB='/foreign', PDAL_DRIVER_PATH='/foreign', GDAL_DRIVER_PATH='/foreign',
                                GDAL_DATA='/foreign', DYLD_LIBRARY_PATH='/foreign', PYTHONPATH='/foreign')
        self.assertEqual(result.returncode, 0, result.stderr)
        values = json.loads(result.stdout)
        for name in ('PROJ_LIB', 'GDAL_DRIVER_PATH', 'DYLD_LIBRARY_PATH', 'PYTHONPATH'):
            self.assertNotIn(name, values)
        self.assertEqual(values['POINTCLOUD_PDAL_BIN'], str(self.runtime / 'venv/bin/pdal'))
        self.assertEqual(values['GDAL_DATA'], str(self.runtime / 'venv/share/gdal'))
        self.assertEqual(values['PROJ_DATA'], str(self.runtime / 'venv/share/proj'))
        self.assertEqual(values['PDAL_DRIVER_PATH'], str(self.runtime / 'venv/lib'))

    def test_external_listener_and_live_environment_block_mutation(self):
        code = '''
addp_dev_port_busy() { return 0; }
addp_dev_owned_listener() { return "${OWNED:-1}"; }
addp_with_python_dependency_lock() { echo unexpected-install; return 99; }
addp_prepare_pointcloud_workflow
'''
        result = self.run_shell(code)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('外部监听者', result.stderr)
        self.assertNotIn('unexpected-install', result.stdout)
        result = self.run_shell(code, OWNED='0')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('先停止', result.stderr)
        self.assertNotIn('unexpected-install', result.stdout)

    def test_launch_real_pid_health_and_failure_cleanup(self):
        # Isolated disposable HTTP server; never calls System or user services.
        binary = self.runtime / 'venv/bin/python'
        binary.parent.mkdir(parents=True)
        binary.symlink_to(sys.executable)
        (self.runtime / 'api_server.py').write_text('''
import http.server, json, os, sys
assert os.environ['WORKFLOW_BIND_HOST'] == '127.0.0.1'
assert 'POINTCLOUD_OBJECT_STORE_LOOPBACK_HOST' not in os.environ
assert 'RUNTIME_PUBLIC_PORT' not in os.environ
if os.environ.get('FAIL_START'): sys.exit(1)
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200); self.end_headers()
        self.wfile.write(json.dumps({'status': 'healthy'}).encode())
http.server.HTTPServer(('127.0.0.1', int(os.environ['PORT'])), Handler).serve_forever()
''')
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = str(sock.getsockname()[1])
        # lsof ownership follows the production helper; cleanup always stops our own child.
        code = '''
source "$SOURCE_ROOT/scripts/dev/ports.sh"
source "$SOURCE_ROOT/scripts/dev/lifecycle-lock.sh"
trap 'if [ -f "$ROOT_DIR/.dev-pids/pointcloud-workflow-engine.pid" ]; then kill "$(cat "$ROOT_DIR/.dev-pids/pointcloud-workflow-engine.pid")" 2>/dev/null || true; fi' EXIT
addp_launch_pointcloud_workflow
'''
        result = self.run_shell(code, POINTCLOUD_WORKFLOW_PORT=port, RUNTIME_PUBLIC_PORT='1', POINTCLOUD_OBJECT_STORE_LOOPBACK_HOST='foreign')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        pidfile = self.work / '.dev-pids/pointcloud-workflow-engine.pid'
        self.assertTrue(pidfile.read_text().strip().isdigit())
        pidfile.unlink()
        result = self.run_shell(code, POINTCLOUD_WORKFLOW_PORT=port, FAIL_START='1')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(pidfile.exists())

    def test_readiness_rejects_degraded_runtime_with_python_optimization(self):
        for path in ('scripts/dev/start.sh', 'scripts/dev/pointcloud-workflow.sh'):
            checks = re.findall(r"python3 -c '([^']*json.load\(sys.stdin\)[^']*)'", (ROOT / path).read_text())
            self.assertTrue(checks, path)
            for check in checks:
                for health, expected in (({'status': 'healthy'}, 0), ({'status': 'degraded'}, 1), ({}, 1)):
                    with self.subTest(path=path, health=health):
                        result = subprocess.run([sys.executable, '-O', '-c', check], input=json.dumps(health),
                                                capture_output=True, text=True, timeout=5)
                        self.assertEqual(result.returncode, expected, result.stderr)

    def test_one_route_and_ci_registration(self):
        start = (ROOT / 'scripts/dev/start.sh').read_text()
        restart = (ROOT / 'scripts/dev/restart.sh').read_text()
        stop = (ROOT / 'scripts/dev/stop.sh').read_text()
        for source in (start, restart):
            self.assertNotIn('ensure_pointcloud_workflow_image', source)
            self.assertNotIn('pointcloud_workflow_source_fingerprint', source)
        self.assertIn('scripts/dev/pointcloud-workflow.sh', start)
        self.assertIn('exec env SKIP_MODTIDY=1', restart)
        self.assertNotIn('addp_prepare_pointcloud_workflow', restart)
        self.assertNotIn('for name in pointcloud-workflow ', stop)
        self.assertIn('pointcloud-workflow-engine POINTCLOUD_WORKFLOW_PORT', (ROOT / 'scripts/dev/ports.sh').read_text())
        workflow = (ROOT / '.github/workflows/platform-ci.yml').read_text()
        self.assertIn('run: make test-pointcloud-workflow', workflow)
        self.assertIn('run: make test-pointcloud-native', workflow)
        hosted = (ROOT / '.github/workflows/online-t4-gates.yml').read_text().split('  manager-hosted-t4:', 1)[1].split('\n  orchestrator-hosted-t4:', 1)[0]
        self.assertIn('conda-incubator/setup-miniconda@fc2d68f', hosted)


if __name__ == '__main__':
    unittest.main()
