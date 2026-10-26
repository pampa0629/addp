import contextlib
import json
import os
import re
import shlex
import signal
import socket
import shutil
import subprocess
import sys
import tempfile
import time
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
                expected = "addp_prepare_gdal_arrow_dependencies" if job == "geopython-workflow-tests" else "addp_prepare_gdal_binding"
                self.assertIn(expected, block)
                self.assertNotIn('-m pip install "GDAL==', block)


class RasterNativeLifecycleTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        super().setUpClass()
        cls.launcher_directory = tempfile.TemporaryDirectory(prefix="addp-raster-dispatcher-")
        cls.addClassCleanup(cls.launcher_directory.cleanup)
        cls.launcher = Path(cls.launcher_directory.name) / "python"
        dispatcher = Path(cls.launcher_directory.name) / "fake-python.py"
        # Invoke generated Python through the existing interpreter; executing a
        # fresh Python shebang file can spend the fixture's deadline on startup.
        # Each simulated venv links to the same launcher instead of copying it.
        cls.launcher.write_text(
            "#!/bin/sh\nexec " + shlex.quote(sys.executable) + " "
            + shlex.quote(str(dispatcher)) + ' "$@"\n')
        dispatcher.write_text(r'''
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
    (runtime / "bin/python").symlink_to((work / "bin/python3.12").resolve())
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
        cls.launcher.chmod(0o755)

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
        self.source_python.symlink_to(self.launcher)
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

    @contextlib.contextmanager
    def fixture_process(self, script, **environment):
        process = subprocess.Popen(["bash", "-c", script], env=dict(self.environment, **environment),
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                                   start_new_session=True)
        try:
            yield process
        finally:
            # A timed-out shell cannot run its trap. Reap only this fixture's
            # process group, including children that outlive their parent shell.
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.communicate()

    def run_fixture(self, script, **environment):
        with self.fixture_process(script, **environment) as process:
            stdout, stderr = process.communicate(timeout=20)
            return subprocess.CompletedProcess(process.args, process.returncode, stdout, stderr)

    def run_prepare(self, **environment):
        return self.run_fixture(self.prepare, **environment)

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
        with contextlib.ExitStack() as cleanup:
            processes = [cleanup.enter_context(self.fixture_process(self.prepare, SLOW_INSTALL="1"))
                         for _ in range(3)]
            for process in processes:
                stdout, stderr = process.communicate(timeout=20)
                self.assertEqual(process.returncode, 0, stdout + stderr)
        self.assertEqual(sum(call[:3] == ["-m", "pip", "install"] for call in self.calls()), 1)

    def test_timeout_and_exception_reap_fixture_listener(self):
        listener = self.work / "listener.py"
        ready = self.work / "listener-ready"
        listener.write_text('''
import http.server, os
from pathlib import Path
server = http.server.HTTPServer(("127.0.0.1", int(os.environ["RASTER_MOSAIC_RUNTIME_PORT"])), http.server.BaseHTTPRequestHandler)
Path(os.environ["ROOT_DIR"], "listener-ready").touch()
server.serve_forever()
''')
        script = shlex.quote(sys.executable) + " " + shlex.quote(str(listener)) + " &\nwait\n"
        for mode, expected_error in (("timeout", subprocess.TimeoutExpired), ("exception", RuntimeError)):
            with self.subTest(mode=mode):
                ready.unlink(missing_ok=True)
                with self.assertRaises(expected_error):
                    with self.fixture_process(script) as process:
                        deadline = time.monotonic() + 20
                        while not ready.exists() and process.poll() is None and time.monotonic() < deadline:
                            time.sleep(0.02)
                        self.assertTrue(ready.exists(), "fixture listener did not start")
                        if mode == "timeout":
                            process.communicate(timeout=0.05)
                        else:
                            raise RuntimeError("fixture assertion failed")
                with socket.socket() as sock:
                    sock.settimeout(1)
                    self.assertNotEqual(sock.connect_ex(("127.0.0.1", int(self.port))), 0)

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
        for relative in ("scripts/dev/stop.sh", "scripts/dev/ports.sh", "scripts/dev/lifecycle-lock.sh", "scripts/utils/colors.sh", "scripts/utils/deployment-inventory.py"):
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
        result = self.run_fixture(launch)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        result = self.run_fixture(launch, FAIL_START="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.work / ".dev-pids/raster-mosaic-runtime.pid").exists())


class NativeArrowIdentityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        import importlib.util
        spec = importlib.util.spec_from_file_location('gdal_arrow', ROOT / 'scripts/dev/gdal-arrow.py')
        cls.helper = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.helper)

    def test_identity_includes_content_not_just_versions_or_symlink_names(self):
        with tempfile.TemporaryDirectory() as temporary:
            library = Path(temporary) / 'libarrow.dylib'
            library.write_bytes(b'first bottle')
            first = self.helper.sha256(library)
            library.write_bytes(b'replaced same version')
            second = self.helper.sha256(library)
            self.assertNotEqual(first, second)
            manifest = {'version': '22.0.0', 'library': str(library), 'sha256': first}
            original = self.helper.fingerprint(manifest)
            manifest['sha256'] = second
            self.assertNotEqual(original, self.helper.fingerprint(manifest))

    def test_only_arrow_cpp_family_is_matched(self):
        for name in ('libarrow.2200.0.0.dylib', 'libarrow_dataset.so.2200', 'libparquet.so.25'):
            self.assertRegex(name, self.helper.ARROW_LIBRARY)
        for name in ('libarrow_python.2200.dylib', 'libgdal.dylib', 'libordinary.so'):
            self.assertNotRegex(name, self.helper.ARROW_LIBRARY)

    def test_non_arrow_gdal_does_not_require_arrow_pkg_config(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as temporary:
            library = Path(temporary) / 'libgdal.so'
            library.write_bytes(b'GDAL')
            values = {('gdal-config', '--libs'): '-L' + temporary + ' -lgdal',
                      ('gdal-config', '--version'): '3.12.1'}
            with patch.object(self.helper, 'output', side_effect=lambda *a: values[a]), \
                 patch.object(self.helper, 'linked_libraries', return_value=[Path('/lib/libc.so')]):
                self.assertIsNone(self.helper.native_manifest()['arrow'])

    def test_declared_arrow_must_be_the_library_gdal_links(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            gdal = directory / 'libgdal.so'
            gdal.write_bytes(b'GDAL')
            arrow = directory / 'other/libarrow.so.2200'
            arrow.parent.mkdir()
            arrow.write_bytes(b'ARROW')
            values = {('gdal-config', '--libs'): '-L' + temporary + ' -lgdal',
                      ('gdal-config', '--version'): '3.12.1',
                      ('pkg-config', '--variable=prefix', str((arrow.parent / 'pkgconfig/arrow.pc').resolve())): temporary,
                      ('pkg-config', '--variable=libdir', str((arrow.parent / 'pkgconfig/arrow.pc').resolve())): temporary}
            with patch.object(self.helper, 'output', side_effect=lambda *a: values[a]), \
                 patch.object(self.helper, 'linked_libraries', return_value=[arrow]):
                with self.assertRaisesRegex(RuntimeError, '来源不一致'):
                    self.helper.native_manifest()


class NativeArrowTransactionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='addp-arrow-transaction-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.runtime = self.root / 'engines/geopython-workflow'
        (self.runtime / 'venv/bin').mkdir(parents=True)
        (self.runtime / 'requirements.txt').write_text('fixture==1.0\n')
        (self.root / 'common-python').mkdir()
        (self.root / 'common-python/pyproject.toml').write_text('[project]\nname="fixture"\n')
        helper = self.root / 'scripts/dev/gdal-arrow.py'
        helper.parent.mkdir(parents=True)
        helper.write_text(r'''import json, os, sys
from pathlib import Path
root=Path(os.environ['ROOT_DIR'])
command=sys.argv[1]
if command=='manifest':
 if os.environ.get('FAIL_MANIFEST')=='1': sys.exit(1)
 print(json.dumps({'arrow':{'version':'22.0.0','prefix':'/native'},'build_requirements':['cython==3.1.8'],'key':(root/'native').read_text()}))
elif command=='build':
 cache=Path(sys.argv[sys.argv.index('--cache')+1]); (cache/'pyarrow-fixture.whl').write_bytes(b'wheel')
 with (root/'events').open('a') as f: f.write('source-build\n')
 sys.exit(int(os.environ.get('FAIL_BUILD','0')))
elif command=='fingerprint':
 print(json.loads(Path(sys.argv[3]).read_text())['key'])
elif command=='admit':
 with (root/'events').open('a') as f: f.write('admit '+sys.argv[-1]+'\n')
 sys.exit(int(os.environ.get('FAIL_ADMISSION','0')))
''')
        launcher = self.runtime / 'venv/bin/python'
        wrapper = self.root / 'python.py'
        launcher.write_text('#!/bin/sh\nexec '+shlex.quote(sys.executable)+' '+shlex.quote(str(wrapper))+' "$@"\n')
        launcher.chmod(0o755)
        wrapper.write_text(r'''import os, sys
from pathlib import Path
root=Path(os.environ['ROOT_DIR'])
if len(sys.argv)>2 and sys.argv[1]=='-c' and 'from osgeo import' in sys.argv[2]: sys.exit(0)
if sys.argv[1:3]==['-m','pip']:
 with (root/'events').open('a') as f: f.write('pip '+' '.join(sys.argv[3:])+'\n')
 if sys.argv[3]=='wheel':
  cache=Path(sys.argv[sys.argv.index('--wheel-dir')+1]); (cache/'pyarrow-fixture.whl').write_bytes(b'wheel')
 sys.exit(int(os.environ.get('FAIL_PIP','0')))
os.execv(sys.executable,[sys.executable]+sys.argv[1:])
''')
        (self.root / 'native').write_text('native-a')
        self.environment = dict(os.environ, ROOT_DIR=str(self.root), SOURCE_ROOT=str(ROOT))
        self.script = r'''
set -euo pipefail
source "$SOURCE_ROOT/scripts/dev/lifecycle-lock.sh"
source "$SOURCE_ROOT/scripts/dev/gdal-env.sh"
# Actual transaction and resolver fingerprint, fake external compilers/native
# binding only. The filesystem/admission are real in the Native owner test.
addp_install_gdal_binding() { echo gdal-binding >> "$ROOT_DIR/events"; }
addp_install_gdal_arrow_dependencies "$ROOT_DIR/engines/geopython-workflow" fixture "$ROOT_DIR/common-python"
'''

    def prepare(self, **environment):
        return subprocess.run(['bash', '-c', self.script], env=dict(self.environment, **environment),
                              capture_output=True, text=True, timeout=30)

    def events(self):
        return (self.root / 'events').read_text().splitlines()

    def test_native_identity_cache_hit_and_same_version_library_replacement(self):
        result = self.prepare()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertTrue((self.root / "events").exists(), result.stdout + result.stderr)
        events = self.events()
        self.assertEqual(sum(line == 'source-build' for line in events), 1)
        self.assertEqual(sum(line.startswith('admit') for line in events), 2)
        (self.root / 'events').unlink()
        result = self.prepare()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse(any(line.startswith('pip install') or line == 'source-build' for line in self.events()))
        self.assertEqual(sum(line.startswith('admit') for line in self.events()), 2)
        (self.root / 'native').write_text('native-b')
        (self.root / 'events').unlink()
        result = self.prepare()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(sum(line == 'source-build' for line in self.events()), 1)
        self.assertEqual((self.runtime / 'venv/.addp-arrow-native-fingerprint').read_text().strip(), 'native-b')

    def test_failed_native_admission_invalidates_both_preparation_records(self):
        self.assertEqual(self.prepare().returncode, 0)
        result = self.prepare(FAIL_ADMISSION='1')
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        for name in ('.addp-dependency-fingerprint', '.addp-arrow-native-fingerprint'):
            self.assertFalse((self.runtime / 'venv' / name).exists())
        self.assertEqual(self.prepare().returncode, 0)

    def test_failed_discovery_invalidates_a_previous_success(self):
        self.assertEqual(self.prepare().returncode, 0)
        result = self.prepare(FAIL_MANIFEST='1')
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        for name in ('.addp-dependency-fingerprint', '.addp-arrow-native-fingerprint'):
            self.assertFalse((self.runtime / 'venv' / name).exists())

    def test_concurrent_native_transactions_compile_once(self):
        for name in ('gdal-env.sh', 'lifecycle-lock.sh'):
            shutil.copy2(ROOT / 'scripts/dev' / name, self.root / 'scripts/dev' / name)
        binary = self.root / 'bin'
        binary.mkdir()
        gdal = binary / 'gdal-config'
        gdal.write_text('#!/bin/sh\necho 3.12.1\n')
        gdal.chmod(0o755)
        script = self.script.replace(
            'addp_install_gdal_arrow_dependencies "$ROOT_DIR/engines/geopython-workflow"',
            'addp_prepare_gdal_arrow_dependencies "$ROOT_DIR/engines/geopython-workflow"')
        environment = dict(self.environment, PATH=str(binary) + os.pathsep + os.environ['PATH'])
        processes = [subprocess.Popen(['bash', '-c', script], env=environment,
                      stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                      start_new_session=True) for _ in range(3)]
        try:
            for process in processes:
                stdout, stderr = process.communicate(timeout=30)
                self.assertEqual(process.returncode, 0, stdout + stderr)
        finally:
            for process in processes:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.communicate()
        self.assertEqual(self.events().count('source-build'), 1)
        self.assertEqual(sum('pip install -r ' in line for line in self.events()), 1)
        self.assertEqual(sum(line.startswith('admit ') for line in self.events()), 6)

    def test_failed_build_does_not_publish_success(self):
        result = self.prepare(FAIL_BUILD='1')
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse((self.runtime / 'venv/.addp-arrow-native-fingerprint').exists())
        self.assertFalse((self.runtime / 'venv/.addp-dependency-fingerprint').exists())


if __name__ == "__main__":
    unittest.main()
