import json
import os
import re
import socket
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class GDALCIPreparationTests(unittest.TestCase):
    def test_both_ci_runtimes_use_shared_binding_repair(self):
        workflow = (ROOT / ".github/workflows/platform-ci.yml").read_text()
        for job in ("raster-mosaic-runtime-tests", "geopython-workflow-tests"):
            with self.subTest(job=job):
                block = re.split(r"\n  (?=\S)", workflow.split("\n  " + job + ":\n", 1)[1], maxsplit=1)[0]
                self.assertIn("source scripts/dev/lifecycle-lock.sh", block)
                self.assertIn("source scripts/dev/gdal-env.sh", block)
                self.assertIn("addp_gdal_native_environment", block)
                self.assertIn("addp_prepare_gdal_binding", block)
                self.assertNotIn('-m pip install "GDAL==', block)


class RasterNativeLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-raster-lifecycle-")
        self.addCleanup(self.temporary.cleanup)
        self.work = Path(self.temporary.name).resolve()
        self.runtime = self.work / "manager/raster-mosaic-runtime"
        self.runtime.mkdir(parents=True)
        (self.runtime / "requirements.txt").write_text("flask==3.0.0\nnumpy>=1.26,<2.3\n")
        self.site = self.work / "site"
        self.metadata = self.site / "fixture-1.0.dist-info"
        self.metadata.mkdir(parents=True)
        (self.metadata / "METADATA").write_text("Name: fixture\nVersion: 1.0\n")
        self.python = self.runtime / "venv/bin/python"
        self.bin = self.work / "bin"
        self.bin.mkdir()
        self.source_python = self.bin / "python3.12"
        self.source_python.write_text("#!" + sys.executable + "\n" + r'''
import json, os, shutil, sys
from pathlib import Path
work = Path(os.environ["ROOT_DIR"])
with (work / "calls").open("a") as trace:
    trace.write(json.dumps(sys.argv[1:]) + "\n")
if sys.argv[1:3] == ["-m", "venv"]:
    assert "--clear" in sys.argv and "--system-site-packages" not in sys.argv
    runtime = Path(sys.argv[-1])
    shutil.rmtree(runtime, ignore_errors=True)
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
    if sys.argv[3] == "check":
        sys.exit(int(os.environ.get("FAIL_CHECK", "0")))
    assert sys.argv[3] == "install" and "-r" in sys.argv and "-e" not in sys.argv
    if os.environ.get("CHANGE_INPUT") == "1":
        (work / "manager/raster-mosaic-runtime/requirements.txt").write_text("flask==3.1.0\n")
    if os.environ.get("SLOW_INSTALL") == "1":
        import time
        time.sleep(0.2)
    sys.exit(int(os.environ.get("FAIL_PIP", "0")))
if sys.argv[1] == "-":
    code = sys.stdin.read()
    if "importlib.metadata" in code:
        sys.argv = sys.argv[1:]
        exec(compile(code, "<dependency-fingerprint>", "exec"))
        sys.exit(0)
    sys.exit(int(os.environ.get("FAIL_DRIVERS", "0")))
os.execv(sys.executable, [sys.executable] + sys.argv[1:])
''')
        self.source_python.chmod(0o755)
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            self.port = str(sock.getsockname()[1])
        self.environment = dict(os.environ, ROOT_DIR=str(self.work), SOURCE_ROOT=str(ROOT),
                                RASTER_MOSAIC_RUNTIME_PORT=self.port, PYTHONPATH=str(self.site),
                                PATH=str(self.bin) + ":" + os.environ["PATH"])
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

    def test_healthy_environment_skips_install_but_keeps_native_checks(self):
        self.assertEqual(self.run_prepare().returncode, 0)
        self.assertTrue((self.runtime / "venv/.addp-dependency-fingerprint").is_file())
        (self.work / "calls").unlink()
        result = self.run_prepare()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        calls = self.calls()
        self.assertFalse(any(call[:2] == ["-m", "venv"] for call in calls))
        self.assertFalse(any(call[:3] == ["-m", "pip", "install"] for call in calls))
        self.assertIn(["-m", "pip", "check"], calls)
        self.assertTrue(any(call[:1] == ["-c"] and "gdal_array" in call[1] for call in calls))
        self.assertIn(["-"], calls)
        self.assertIn("依赖未变化，跳过安装", result.stdout)
        self.assertFalse(any("--force-reinstall" in call for call in calls))

    def test_requirements_and_installed_metadata_changes_resync(self):
        self.assertEqual(self.run_prepare().returncode, 0)
        nested = self.runtime / "extra.txt"
        for path, contents in (
            (self.runtime / "requirements.txt", "-r extra.txt\n"),
            (nested, "flask==3.1.0\n"),
            (self.metadata / "METADATA", "Name: fixture\nVersion: 2.0\n"),
            (self.metadata / "direct_url.json", '{"url": "file:///other-source"}'),
        ):
            with self.subTest(path=path):
                if not nested.exists():
                    nested.write_text("flask==3.0.0\n")
                path.write_text(contents)
                (self.work / "calls").unlink()
                result = self.run_prepare()
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertEqual(sum(call[:3] == ["-m", "pip", "install"] for call in self.calls()), 1)

    def test_undeclared_common_package_does_not_invalidate_dependencies(self):
        self.assertEqual(self.run_prepare().returncode, 0)
        common = self.work / "common-python"
        common.mkdir()
        (common / "pyproject.toml").write_text("changed unrelated package\n")
        (self.work / "calls").unlink()
        result = self.run_prepare()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse(any(call[:3] == ["-m", "pip", "install"] for call in self.calls()))

    def test_failed_sync_removes_record_and_recovers(self):
        for flag in ("FAIL_CHECK", "FAIL_PIP", "CHANGE_INPUT"):
            with self.subTest(flag=flag):
                self.assertEqual(self.run_prepare().returncode, 0)
                fingerprint = self.runtime / "venv/.addp-dependency-fingerprint"
                if flag != "FAIL_CHECK":
                    fingerprint.unlink()
                result = self.run_prepare(**{flag: "1"})
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertFalse(fingerprint.exists())
                self.assertEqual(self.run_prepare().returncode, 0)
                self.assertTrue(fingerprint.is_file())

    def test_concurrent_preparations_install_once(self):
        self.assertEqual(self.run_prepare().returncode, 0)
        (self.runtime / "venv/.addp-dependency-fingerprint").unlink()
        (self.work / "calls").unlink()
        processes = [subprocess.Popen(["bash", "-c", self.prepare],
                     env=dict(self.environment, SLOW_INSTALL="1"),
                     stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True) for _ in range(3)]
        for process in processes:
            stdout, stderr = process.communicate(timeout=20)
            self.assertEqual(process.returncode, 0, stdout + stderr)
        self.assertEqual(sum(call[:3] == ["-m", "pip", "install"] for call in self.calls()), 1)

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
