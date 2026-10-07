import json
import os
import socket
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class RasterNativeLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-raster-lifecycle-")
        self.addCleanup(self.temporary.cleanup)
        self.work = Path(self.temporary.name).resolve()
        self.runtime = self.work / "manager/raster-mosaic-runtime"
        self.runtime.mkdir(parents=True)
        self.python = self.runtime / "venv/bin/python"
        self.bin = self.work / "bin"
        self.bin.mkdir()
        self.source_python = self.bin / "python3.12"
        self.source_python.write_text("#!" + sys.executable + "\n" + r'''
import json, os, sys
from pathlib import Path
work = Path(os.environ["ROOT_DIR"])
with (work / "calls").open("a") as trace:
    trace.write(json.dumps(sys.argv[1:]) + "\n")
if sys.argv[1:3] == ["-m", "venv"]:
    assert "--clear" in sys.argv and "--system-site-packages" not in sys.argv
    runtime = Path(sys.argv[-1])
    (runtime / "bin").mkdir(parents=True, exist_ok=True)
    (runtime / "bin/python").write_bytes(Path(__file__).read_bytes())
    (runtime / "bin/python").chmod(0o755)
    (work / "inherited").unlink(missing_ok=True)
    sys.exit(0)
if len(sys.argv) > 2 and sys.argv[1] == "-c":
    if "sys.version_info" in sys.argv[2]: sys.exit(int((work / "inherited").exists()))
    if "import app" in sys.argv[2]: sys.exit(int(os.environ.get("FAIL_IMPORT", "0")))
    sys.exit(0)
if sys.argv[1:3] == ["-m", "pip"]:
    assert "requirements.txt" in sys.argv[-1] or sys.argv[-1] == "check"
    sys.exit(int(os.environ.get("FAIL_PIP", "0")))
if sys.argv[1] == "-": sys.exit(int(os.environ.get("FAIL_DRIVERS", "0")))
os.execv(sys.executable, [sys.executable] + sys.argv[1:])
''')
        self.source_python.chmod(0o755)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            self.port = str(sock.getsockname()[1])
        self.environment = dict(os.environ, ROOT_DIR=str(self.work), SOURCE_ROOT=str(ROOT),
                                RASTER_MOSAIC_RUNTIME_PORT=self.port, PATH=str(self.bin) + ":" + os.environ["PATH"])
        self.prepare = r'''
set -euo pipefail
source "$SOURCE_ROOT/scripts/dev/ports.sh"
source "$SOURCE_ROOT/scripts/dev/lifecycle-lock.sh"
source "$SOURCE_ROOT/scripts/dev/raster-mosaic-runtime.sh"
addp_gdal_native_environment() { unset GDAL_DRIVER_PATH PROJ_LIB; }
uname() { echo Linux; }
gdal-config() { echo 3.12.1; }
addp_prepare_raster_mosaic_runtime
'''

    def run_prepare(self, **environment):
        return subprocess.run(["bash", "-c", self.prepare], env=dict(self.environment, **environment),
                              capture_output=True, text=True, timeout=20)

    def calls(self):
        path = self.work / "calls"
        return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []

    def test_fresh_and_inherited_environments_are_isolated(self):
        for inherited in (False, True):
            if inherited:
                (self.work / "inherited").touch()
                (self.work / "calls").unlink()
            result = self.run_prepare()
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn(["-m", "venv", "--clear", str(self.runtime / "venv")], self.calls())
            self.assertTrue((self.work / ".dev-state/python-dependencies.lock").is_file())
            self.assertFalse((self.work / ".dev-state/geopython-odbc").exists())

    def test_healthy_environment_reuses_binding_and_syncs_full_requirements(self):
        self.assertEqual(self.run_prepare().returncode, 0)
        (self.work / "calls").unlink()
        result = self.run_prepare()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        calls = self.calls()
        self.assertFalse(any(call[:2] == ["-m", "venv"] for call in calls))
        self.assertIn(["-m", "pip", "install", "-r", str(self.runtime / "requirements.txt")], calls)
        self.assertIn(["-m", "pip", "check"], calls)
        self.assertFalse(any("--force-reinstall" in call for call in calls))

    def test_failures_do_not_publish_pid(self):
        for flag in ("FAIL_PIP", "FAIL_IMPORT", "FAIL_DRIVERS"):
            result = self.run_prepare(**{flag: "1"})
            self.assertNotEqual(result.returncode, 0, flag)
            self.assertFalse((self.work / ".dev-pids/raster-mosaic-runtime.pid").exists())

    def test_live_pid_and_foreign_listener_prevent_environment_mutation(self):
        pidfile = self.work / ".dev-pids/raster-mosaic-runtime.pid"
        pidfile.parent.mkdir()
        pidfile.write_text(str(os.getpid()))
        result = self.run_prepare()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])
        pidfile.unlink()
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", int(self.port)))
            sock.listen()
            result = self.run_prepare()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.calls(), [])

    def test_launch_owns_listener_and_cleans_up_failure(self):
        self.assertEqual(self.run_prepare().returncode, 0)
        (self.runtime / "health").write_text("ok")
        (self.runtime / "app.py").write_text('''
import http.server, os, sys
if os.environ.get("FAIL_START") == "1": sys.exit(1)
http.server.HTTPServer(("127.0.0.1", int(os.environ["PORT"])), http.server.SimpleHTTPRequestHandler).serve_forever()
''')
        # Exercise the standard Manager stop entry against disposable processes.
        for relative in ("scripts/dev/stop.sh", "scripts/dev/ports.sh", "scripts/dev/lifecycle-lock.sh", "scripts/utils/colors.sh"):
            target = self.work / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / relative, target)
        for tool in ("launchctl", "docker"):
            path = self.bin / tool
            path.write_text("#!/bin/bash\nexit 1\n")
            path.chmod(0o755)
        (self.work / ".dev-state/ports.env").write_text("RASTER_MOSAIC_RUNTIME_PORT=" + self.port + "\n")
        launch = self.prepare.replace("addp_prepare_raster_mosaic_runtime\n", r'''
addp_launch_raster_mosaic_runtime
pid=$(cat "$ROOT_DIR/.dev-pids/raster-mosaic-runtime.pid")
trap 'kill -TERM "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true' EXIT
addp_dev_owned_listener raster-mosaic-runtime "$RASTER_MOSAIC_RUNTIME_PORT"
[ "$(addp_dev_process_module raster-mosaic-runtime)" = manager ]
bash "$ROOT_DIR/scripts/dev/stop.sh" -manager
[ ! -f "$ROOT_DIR/.dev-pids/raster-mosaic-runtime.pid" ]
''')
        result = subprocess.run(["bash", "-c", launch], env=self.environment, capture_output=True, text=True, timeout=20)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        result = subprocess.run(["bash", "-c", launch], env=dict(self.environment, FAIL_START="1"), capture_output=True, text=True, timeout=20)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.work / ".dev-pids/raster-mosaic-runtime.pid").exists())


if __name__ == "__main__":
    unittest.main()
