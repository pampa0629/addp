import hashlib
import importlib.util
import io
import json
import re
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

SCRIPT = Path(__file__).with_name("gaussdb-official-media-release-gate.py")
SPEC = importlib.util.spec_from_file_location("gaussdb_release", SCRIPT)
GATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GATE)


def tar_bytes(files):
    stream = io.BytesIO()
    with tarfile.open(fileobj=stream, mode="w:gz") as archive:
        for name, data in files.items():
            entry = tarfile.TarInfo(name)
            entry.size = len(data)
            archive.addfile(entry, io.BytesIO(data))
    return stream.getvalue()


class GaussDBCertificationTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)

    def tearDown(self):
        self.temporary.cleanup()

    def test_facts_pin_official_version_architecture_and_driver(self):
        facts = GATE.media_facts()
        self.assertEqual(facts["GAUSSDB_OFFICIAL_VERSION"], "V2.0-10.0.0")
        self.assertIn("_Ubuntu_X86_20260915102549.zip", facts["GAUSSDB_OFFICIAL_MEDIA_URL"])
        self.assertEqual(facts["GAUSSDB_OFFICIAL_MEDIA_SHA256"], "c53cf2021d86c4820e8c6fddf13a098e1b2b1f4a2ca142c6dcf1ff2fa9ab54a8")
        self.assertEqual(facts["GAUSSDB_OFFICIAL_DRIVER_SHA256"], "d3735c8f1906491e9789d0a11bfda6020ef6db34583306fe4a0c4eedf1498da7")
        self.assertIn("ubuntu:22.04@sha256:", facts["GAUSSDB_OFFICIAL_BASE_IMAGE"])
        with self.assertRaises(subprocess.CalledProcessError):
            subprocess.check_call(["bash", "-ec", 'source "$1"; gaussdb_select_official_media invalid', "facts", str(GATE.ROOT / "scripts/lib/gaussdb-official-media.sh")], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    @patch.object(GATE.urllib.request, "urlopen")
    def test_download_verifies_content_and_reuses_only_verified_cache(self, open_url):
        content = b"official archive"
        open_url.return_value.__enter__.return_value = io.BytesIO(content)
        digest = hashlib.sha256(content).hexdigest()
        path = GATE.download("https://official.example/media.zip", digest, self.root)
        self.assertEqual(path.read_bytes(), content)
        GATE.download("https://official.example/media.zip", digest, self.root)
        self.assertEqual(open_url.call_count, 1)
        path.write_bytes(b"wrong cache")
        open_url.return_value.__enter__.return_value = io.BytesIO(content)
        self.assertEqual(GATE.download("https://official.example/media.zip", digest, self.root).read_bytes(), content)
        self.assertEqual(open_url.call_count, 2)

    @patch.object(GATE.urllib.request, "urlopen")
    def test_wrong_download_hash_never_installs_partial(self, open_url):
        open_url.return_value.__enter__.return_value = io.BytesIO(b"wrong")
        with self.assertRaisesRegex(RuntimeError, "SHA256 mismatch"):
            GATE.download("https://official.example/media.zip", "0" * 64, self.root)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_nested_kernel_verifies_inner_digest_and_does_not_execute(self):
        binary = b"synthetic installer"
        inner = tar_bytes({"./kernel_64bit.bin": binary, "./kernel_64bit.sha256": hashlib.sha256(binary).hexdigest().encode()})
        outer = tar_bytes({"server_64bit_Server.tar.gz": inner})
        archive = self.root / "media.zip"
        with zipfile.ZipFile(archive, "w") as package:
            package.writestr("Ubuntu/server/media_Server_X86_Developer.tar.gz", outer)
        destination = self.root / "kernel.bin"
        GATE.extract_kernel(archive, destination)
        self.assertEqual(destination.read_bytes(), binary)

    def test_rejects_ambiguous_archive_member(self):
        with self.assertRaisesRegex(RuntimeError, "exactly one"):
            GATE.unique_member(["a.bin", "b.bin"], ".bin")

    def test_driver_extraction_refuses_path_traversal(self):
        inner = tar_bytes({"../escaped": b"must reject"})
        outer = tar_bytes({"driver.tar.gz": inner})
        archive = self.root / "driver.zip"
        with zipfile.ZipFile(archive, "w") as package:
            package.writestr("Ubuntu/client/go/driver.tar.gz", outer)
        destination = self.root / "output"
        with self.assertRaisesRegex(RuntimeError, "escapes"):
            GATE.extract_driver(archive, "Ubuntu", destination)
        self.assertFalse((self.root / "escaped").exists())

    @patch.object(GATE, "command")
    @patch.object(GATE, "inspect")
    def test_cleanup_checks_owner_and_each_residual(self, inspect, command):
        inspect.side_effect = [{"Config": {"Labels": {GATE.LABEL: "run"}}}, None,
                               {"Labels": {GATE.LABEL: "run"}}, None,
                               {"Config": {"Labels": {GATE.LABEL: "run"}}}, None]
        GATE.clean_owned("run", "container", "network", "image")
        self.assertEqual(command.call_count, 3)

    @patch.object(GATE, "command")
    @patch.object(GATE, "inspect")
    def test_foreign_container_is_not_deleted_and_other_owned_cleanup_continues(self, inspect, command):
        inspect.side_effect = [{"Config": {"Labels": {GATE.LABEL: "foreign"}}},
                               {"Labels": {GATE.LABEL: "run"}}, None,
                               {"Config": {"Labels": {GATE.LABEL: "run"}}}, None]
        with self.assertRaisesRegex(RuntimeError, "different owner"):
            GATE.clean_owned("run", "container", "network", "image")
        self.assertEqual(command.call_count, 2)
        self.assertNotIn("container", str(command.call_args_list))

    @patch.object(GATE, "command")
    @patch.object(GATE, "inspect")
    def test_residual_container_fails_even_when_other_cleanup_succeeds(self, inspect, command):
        state = {"Config": {"Labels": {GATE.LABEL: "run"}}}
        inspect.side_effect = [state, state, None, None]
        with self.assertRaisesRegex(RuntimeError, "remains"):
            GATE.clean_owned("run", "container", "network", "image")

    @patch.object(GATE.platform, "system", return_value="Darwin")
    def test_macos_is_not_reported_as_linux_t5(self, system):
        with self.assertRaisesRegex(RuntimeError, "native Linux x86_64"):
            GATE.main()

    @patch.object(GATE.subprocess, "run")
    def test_docker_inspection_failure_is_not_treated_as_zero_residual(self, run):
        run.return_value = subprocess.CompletedProcess([], 1, "", "Cannot connect to the Docker daemon")
        with self.assertRaisesRegex(RuntimeError, "unknown"):
            GATE.inspect("container", "owned")
        run.return_value = subprocess.CompletedProcess([], 1, "", "Error: No such container: owned")
        self.assertIsNone(GATE.inspect("container", "owned"))

    @patch.object(GATE, "command")
    @patch.object(GATE, "prepare_probe")
    @patch.object(GATE, "extract_driver")
    @patch.object(GATE, "download")
    def test_compile_only_cross_compiles_without_native_runtime_or_t5_report(self, download, extract, prepare, command):
        prepare.return_value = self.root / "probe"
        GATE.compile_probe()
        args = command.call_args.args[0]
        self.assertEqual(args[:3], ["go", "test", "-c"])
        self.assertEqual(command.call_args.kwargs["env"]["GOOS"], "linux")
        self.assertEqual(command.call_args.kwargs["env"]["GOARCH"], "amd64")
        self.assertEqual(list(self.root.iterdir()), [])

    def run_fake_lifecycle(self, fail_probe=False, fail_readiness=False):
        resources = {}
        probes = []
        initialization = []
        initial_secrets = []
        report = self.root / "report.json"

        def native_sql(args, **kwargs):
            self.assertIn("ON_ERROR_STOP=1", args)
            self.assertIn("-i", args)
            statement = kwargs["input"]
            if statement.startswith("ALTER ROLE omm PASSWORD "):
                self.assertNotIn(statement, " ".join(args))
                self.assertNotIn(initial_secrets[0], statement)
                initialization.append(statement)
                return subprocess.CompletedProcess(args, 0, "ALTER ROLE\n", "")
            self.assertEqual(statement, "SELECT 1")
            self.assertEqual(len(initialization), 1, "initial password must be changed before SQL readiness")
            if fail_readiness:
                return subprocess.CompletedProcess(args, 1, "", "SQL rejected")
            return subprocess.CompletedProcess(args, 0, "1\n", "")

        def inspect(kind, name):
            return resources.get((kind, name))

        def command(args, **kwargs):
            if args[:2] == ["docker", "info"]:
                return subprocess.CompletedProcess(args, 0, "linux/x86_64\n", "")
            if kwargs.get("output"):
                kwargs["output"].write_text("native log\n")
            if args[:2] == ["docker", "build"]:
                entrypoint = (Path(args[-1]) / "entrypoint.sh").read_text()
                self.assertIn("/data/db/gs_hba.conf", entrypoint)
                self.assertNotIn("/data/db/pg_hba.conf", entrypoint)
                self.assertIn("sha256", entrypoint)
                owner = args[args.index("--label") + 1].split("=", 1)[1]
                resources[("image", args[args.index("-t") + 1])] = {"Config": {"Labels": {GATE.LABEL: owner}}}
            elif args[:3] == ["docker", "network", "create"]:
                owner = args[args.index("--label") + 1].split("=", 1)[1]
                resources[("network", args[-1])] = {"Labels": {GATE.LABEL: owner}}
            elif args[:2] == ["docker", "run"]:
                initial_secrets.append(kwargs["env"]["GAUSS_CERT_SECRET"])
                owner = args[args.index("--label") + 1].split("=", 1)[1]
                resources[("container", args[args.index("--name") + 1])] = {
                    "Config": {"Labels": {GATE.LABEL: owner}}, "State": {"Running": True},
                    "NetworkSettings": {"Ports": {"5432/tcp": [{"HostPort": "35432"}]}}}
            elif args[:2] == ["docker", "rm"]:
                resources.pop(("container", args[-1]))
            elif args[:3] in (["docker", "network", "rm"], ["docker", "image", "rm"]):
                resources.pop((args[1], args[-1]))
            elif args[:2] == ["go", "test"]:
                probes.append(kwargs["env"])
                kwargs["output"].write_text(kwargs["env"]["ADDP_TEST_GAUSSDB_DSN"])
                if fail_probe:
                    raise subprocess.CalledProcessError(1, args)
            elif args[:2] == ["docker", "logs"]:
                kwargs["output"].write_text(initialization[0] + initial_secrets[0])
            return subprocess.CompletedProcess(args, 0)

        with patch.object(GATE.platform, "system", return_value="Linux"), \
             patch.object(GATE.platform, "machine", return_value="x86_64"), \
             patch.object(GATE.shutil, "which", return_value="/available"), \
             patch.object(GATE, "download", return_value=self.root / "archive"), \
             patch.object(GATE, "extract_kernel"), \
             patch.object(GATE, "extract_driver", return_value=self.root / "driver"), \
             patch.object(GATE, "prepare_probe", return_value=self.root / "probe"), \
             patch.object(GATE, "inspect", side_effect=inspect), \
             patch.object(GATE, "command", side_effect=command), \
             patch.object(GATE.subprocess, "run", side_effect=native_sql), \
             patch.dict(GATE.os.environ, {"ADDP_GAUSSDB_CERTIFICATION_REPORT": str(report)}):
            # media_facts calls real Bash through check_output; avoid intercepting it.
            facts = {name: "fixed" for name in GATE.FACT_NAMES}
            with patch.object(GATE, "media_facts", return_value=facts):
                if fail_probe:
                    with self.assertRaises(subprocess.CalledProcessError):
                        GATE.main()
                elif fail_readiness:
                    with self.assertRaisesRegex(RuntimeError, "SQL readiness failed"):
                        GATE.main()
                else:
                    GATE.main()
        self.assertEqual(resources, {})
        self.assertEqual(len(initialization), 1)
        self.assertNotIn(initial_secrets[0], report.with_suffix(".container.log").read_text())
        self.assertIn("PASSWORD '[REDACTED]'", report.with_suffix(".container.log").read_text())
        if fail_readiness:
            self.assertEqual(probes, [])
            return json.loads(report.read_text())
        self.assertEqual(probes[0]["GOENV_REGIST_ALL"], "false")
        self.assertEqual(probes[0]["ADDP_GAUSSDB_CERTIFICATION"], "1")
        self.assertNotIn("password=", report.read_text())
        self.assertIn("password=[REDACTED]", report.with_suffix(".probe.log").read_text())
        return json.loads(report.read_text())

    def test_owner_success_waits_for_probe_and_all_cleanup(self):
        report = self.run_fake_lifecycle()
        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["cleanup"], {"result": "passed", "residual_resources": 0})

    def test_native_probe_failure_is_reported_failed_and_resources_still_removed(self):
        report = self.run_fake_lifecycle(fail_probe=True)
        self.assertEqual(report["result"], "failed")
        self.assertEqual(report["cleanup"]["result"], "passed")

    def test_sql_rejection_after_password_change_never_runs_probe_and_still_cleans_up(self):
        report = self.run_fake_lifecycle(fail_readiness=True)
        self.assertEqual(report["result"], "failed")
        self.assertEqual(report["phase"], "readiness")
        self.assertEqual(report["cleanup"], {"result": "passed", "residual_resources": 0})

    def test_focused_dispatch_does_not_trigger_other_owners_or_false_aggregate_failures(self):
        workflow = (GATE.ROOT / ".github/workflows/release-and-t2-gates.yml").read_text()
        guard = "inputs.release_suite != 'gaussdb-official-media'"
        for name in ("selection", "cli-product-macos", "system-iam-postgres"):
            job = re.search(r"(?ms)^  " + name + r":\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow).group(1)
            self.assertIn(guard, next(line for line in job.splitlines() if line.startswith("    if:")))
        job = re.search(r"(?ms)^  gaussdb-official-media-certification:\n(.*?)(?=^  [a-z][a-z0-9-]*:\n|\Z)", workflow).group(1)
        self.assertNotIn("needs: selection", job)
        self.assertIn("make test-release RELEASE_SUITE=gaussdb-official-media", job)

    def test_term_becomes_an_unwindable_exit_instead_of_bypassing_cleanup(self):
        with self.assertRaises(SystemExit) as stopped:
            GATE.terminate(15, None)
        self.assertEqual(stopped.exception.code, 143)

    @patch.object(GATE.subprocess, "run")
    def test_readiness_preserves_client_loader_errors_instead_of_only_timeout(self, run):
        run.return_value = subprocess.CompletedProcess([], 127, "", "gsql: loader failure")
        log = self.root / "readiness.log"
        result = GATE.probe_sql("owned", "SELECT 1", log)
        self.assertEqual(result.returncode, 127)
        self.assertEqual(json.loads(log.read_text()), {"returncode": 127, "stdout": "", "stderr": "gsql: loader failure"})

    @patch.object(GATE.subprocess, "run")
    def test_readiness_retains_partial_output_when_client_times_out(self, run):
        run.side_effect = subprocess.TimeoutExpired([], 15, output=b"partial output", stderr=b"partial error")
        log = self.root / "readiness.log"
        with self.assertRaises(subprocess.TimeoutExpired):
            GATE.probe_sql("owned", "SELECT 1", log)
        self.assertEqual(json.loads(log.read_text()), {"timed_out": True, "stdout": "partial output", "stderr": "partial error"})

    def test_common_package_discovery_cannot_resolve_the_external_certification_driver(self):
        # go mod tidy examines all ordinary build tags. Exercise real discovery
        # with the former tag enabled, without network or dependency mutations.
        result = subprocess.run(
            ["go", "list", "-e", "-json", "-tags=gaussdb_certification", "./engine/certification/..."],
            cwd=GATE.ROOT / "common", capture_output=True, text=True,
            env={**GATE.os.environ, "GOWORK": "off", "GOFLAGS": "-mod=readonly", "GOPROXY": "off", "GOSUMDB": "off"},
            timeout=60, check=True)
        self.assertNotIn("gitee.com/opengauss/openGauss-connector-go-pq", result.stdout)
        self.assertNotIn("gitcode.com/opengauss/openGauss-connector-go-pq", result.stdout)

    def test_probe_module_is_standalone_and_preserves_the_pinned_driver_and_assertions(self):
        driver = self.root / "driver"
        driver.mkdir()
        (driver / "go.mod").write_text("module gitee.com/opengauss/openGauss-connector-go-pq\n\ngo 1.13\n")
        original_mod = (GATE.ROOT / "common/go.mod").read_bytes()
        original_sum = (GATE.ROOT / "common/go.sum").read_bytes()
        probe = GATE.prepare_probe(self.root, driver)
        module = json.loads(subprocess.check_output(["go", "mod", "edit", "-json"],
                            cwd=probe, text=True, env={**GATE.os.environ, "GOWORK": "off"}))
        common = json.loads(subprocess.check_output(["go", "mod", "edit", "-json"],
                            cwd=GATE.ROOT / "common", text=True, env={**GATE.os.environ, "GOWORK": "off"}))
        self.assertEqual(module["Module"]["Path"], "addp.local/gaussdb-certification")
        self.assertEqual(module["Go"], common["Go"])
        postgres = next(item for item in common["Require"] if item["Path"] == "github.com/lib/pq")
        self.assertIn(postgres, module["Require"])
        self.assertEqual(module["Replace"][0]["New"]["Path"], str(driver))
        self.assertEqual((probe / "certification_integration_test.go").read_bytes(), GATE.PROBE_SOURCE.read_bytes())
        self.assertEqual((GATE.ROOT / "common/go.mod").read_bytes(), original_mod)
        self.assertEqual((GATE.ROOT / "common/go.sum").read_bytes(), original_sum)


if __name__ == "__main__":
    unittest.main()
