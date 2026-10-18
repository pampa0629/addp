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

    @patch.object(GATE.subprocess, "run")
    def test_readiness_command_can_observe_nonzero_without_throwing(self, run):
        run.return_value = subprocess.CompletedProcess(["health"], 1)
        self.assertEqual(GATE.command(["health"], check=False).returncode, 1)
        self.assertFalse(run.call_args.kwargs["check"])
        GATE.command(["build"])
        self.assertTrue(run.call_args.kwargs["check"])

    def test_facts_pin_official_version_architecture_and_driver(self):
        facts = GATE.IMAGE.media_facts("x86_64")
        self.assertEqual(facts["GAUSSDB_OFFICIAL_VERSION"], "V2.0-10.0.0")
        self.assertIn("_Ubuntu_X86_20260915102549.zip", facts["GAUSSDB_OFFICIAL_MEDIA_URL"])
        self.assertEqual(facts["GAUSSDB_OFFICIAL_MEDIA_SHA256"], "c53cf2021d86c4820e8c6fddf13a098e1b2b1f4a2ca142c6dcf1ff2fa9ab54a8")
        self.assertEqual(facts["GAUSSDB_GO_MODULE"], "github.com/HuaweiCloudDeveloper/gaussdb-go")
        self.assertEqual(facts["GAUSSDB_GO_VERSION"], "v1.0.0-rc1")
        self.assertTrue(facts["GAUSSDB_GO_SUM"].startswith("h1:"))
        arm = GATE.IMAGE.media_facts("aarch64")
        self.assertIn("_Ubuntu_ARM_", arm["GAUSSDB_OFFICIAL_MEDIA_URL"])
        self.assertEqual(arm["GAUSSDB_OFFICIAL_MEDIA_SHA256"], "a6442f7c12c396aae2e96e3dde8b4eca2eac45ab26dce1735efc530f447990e5")
        self.assertIn("ubuntu:22.04@sha256:", facts["GAUSSDB_OFFICIAL_BASE_IMAGE"])
        with self.assertRaises(subprocess.CalledProcessError):
            subprocess.check_call(["bash", "-ec", 'source "$1"; gaussdb_select_official_media invalid', "facts", str(GATE.ROOT / "scripts/lib/gaussdb-official-media.sh")], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    @patch.object(GATE.IMAGE.urllib.request, "urlopen")
    def test_download_verifies_content_and_reuses_only_verified_cache(self, open_url):
        content = b"official archive"
        open_url.return_value.__enter__.return_value = io.BytesIO(content)
        digest = hashlib.sha256(content).hexdigest()
        path = GATE.IMAGE.download("https://official.example/media.zip", digest, self.root)
        self.assertEqual(path.read_bytes(), content)
        GATE.IMAGE.download("https://official.example/media.zip", digest, self.root)
        self.assertEqual(open_url.call_count, 1)
        path.write_bytes(b"wrong cache")
        open_url.return_value.__enter__.return_value = io.BytesIO(content)
        self.assertEqual(GATE.IMAGE.download("https://official.example/media.zip", digest, self.root).read_bytes(), content)
        self.assertEqual(open_url.call_count, 2)

    @patch.object(GATE.IMAGE.urllib.request, "urlopen")
    def test_wrong_download_hash_never_installs_partial(self, open_url):
        open_url.return_value.__enter__.return_value = io.BytesIO(b"wrong")
        with self.assertRaisesRegex(RuntimeError, "SHA256 mismatch"):
            GATE.IMAGE.download("https://official.example/media.zip", "0" * 64, self.root)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_nested_kernel_verifies_inner_digest_and_does_not_execute(self):
        binary = b"synthetic installer"
        inner = tar_bytes({"./kernel_64bit.bin": binary, "./kernel_64bit.sha256": hashlib.sha256(binary).hexdigest().encode()})
        outer = tar_bytes({"server_64bit_Server.tar.gz": inner})
        archive = self.root / "media.zip"
        with zipfile.ZipFile(archive, "w") as package:
            package.writestr("Ubuntu/server/media_Server_X86_Developer.tar.gz", outer)
        destination = self.root / "kernel.bin"
        GATE.IMAGE.extract_kernel(archive, destination, "amd64")
        self.assertEqual(destination.read_bytes(), binary)

    def test_rejects_ambiguous_archive_member(self):
        with self.assertRaisesRegex(RuntimeError, "exactly one"):
            GATE.IMAGE.unique_member(["a.bin", "b.bin"], ".bin")

    def test_arm_kernel_is_selected_without_x86_fallback(self):
        binary = b"synthetic arm installer"
        inner = tar_bytes({"./kernel_64bit.bin": binary, "./kernel_64bit.sha256": hashlib.sha256(binary).hexdigest().encode()})
        outer = tar_bytes({"server_64bit_Server.tar.gz": inner})
        archive = self.root / "arm.zip"
        with zipfile.ZipFile(archive, "w") as package:
            package.writestr("Ubuntu/media_Server_ARM_Developer.tar.gz", outer)
        destination = self.root / "kernel.bin"
        GATE.IMAGE.extract_kernel(archive, destination, "arm64")
        self.assertEqual(destination.read_bytes(), binary)
        with self.assertRaisesRegex(RuntimeError, "exactly one"):
            GATE.IMAGE.extract_kernel(archive, destination, "amd64")

    @patch.object(GATE, "command")
    def test_public_driver_requires_both_pinned_checksums(self, command):
        facts = GATE.IMAGE.media_facts("x86_64")
        valid = {"Path": facts["GAUSSDB_GO_MODULE"], "Version": facts["GAUSSDB_GO_VERSION"],
                 "Sum": facts["GAUSSDB_GO_SUM"], "GoModSum": facts["GAUSSDB_GO_MOD_SUM"]}
        for field in ("Path", "Version", "Sum", "GoModSum"):
            invalid = {**valid, field: "mismatch"}
            command.return_value = subprocess.CompletedProcess([], 0, json.dumps(invalid))
            with self.assertRaisesRegex(RuntimeError, "driver identity or checksum"):
                GATE.verify_driver(facts, self.root, {})
        command.return_value = subprocess.CompletedProcess([], 0, json.dumps(valid))
        GATE.verify_driver(facts, self.root, {})

    def test_native_architecture_must_match_daemon_without_emulation(self):
        self.assertEqual(GATE.IMAGE.go_architecture("aarch64"), "arm64")
        self.assertEqual(GATE.IMAGE.go_architecture("x86_64"), "amd64")
        with self.assertRaisesRegex(RuntimeError, "unsupported"):
            GATE.IMAGE.go_architecture("riscv64")
        GATE.IMAGE.verify_daemon_architecture("arm64", "linux/aarch64")
        with self.assertRaisesRegex(RuntimeError, "matching native"):
            GATE.IMAGE.verify_daemon_architecture("arm64", "linux/x86_64")

    @patch.object(GATE, "command")
    @patch.object(GATE, "inspect")
    def test_cleanup_checks_owner_and_each_residual(self, inspect, command):
        inspect.side_effect = [{"Config": {"Labels": {GATE.LABEL: "run"}}}, None,
                               {"Labels": {GATE.LABEL: "run"}}, None, None,
                               {"Config": {"Labels": {GATE.LABEL: "run"}}}, None]
        GATE.clean_owned("run", "container", "network", "image")
        self.assertEqual(command.call_count, 3)

    @patch.object(GATE, "command")
    @patch.object(GATE, "inspect")
    def test_foreign_container_is_not_deleted_and_other_owned_cleanup_continues(self, inspect, command):
        inspect.side_effect = [{"Config": {"Labels": {GATE.LABEL: "foreign"}}},
                               {"Labels": {GATE.LABEL: "run"}}, None, None,
                               {"Config": {"Labels": {GATE.LABEL: "run"}}}, None]
        with self.assertRaisesRegex(RuntimeError, "different owner"):
            GATE.clean_owned("run", "container", "network", "image")
        self.assertEqual(command.call_count, 2)
        self.assertNotIn("container", str(command.call_args_list))

    @patch.object(GATE, "command")
    @patch.object(GATE, "inspect")
    def test_residual_container_fails_even_when_other_cleanup_succeeds(self, inspect, command):
        state = {"Config": {"Labels": {GATE.LABEL: "run"}}}
        inspect.side_effect = [state, state, None, None, None]
        with self.assertRaisesRegex(RuntimeError, "remains"):
            GATE.clean_owned("run", "container", "network", "image")

    @patch.object(GATE.platform, "system", return_value="Darwin")
    def test_macos_is_not_reported_as_linux_t5(self, system):
        with self.assertRaisesRegex(RuntimeError, "native Linux amd64/arm64"):
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
    def test_compile_only_checks_all_four_targets_and_host_registration_without_t5_report(self, prepare, command):
        prepare.return_value = self.root / "probe"
        GATE.compile_probe()
        builds = [call for call in command.call_args_list if call.args[0][:3] == ["go", "test", "-c"]]
        self.assertEqual({(call.kwargs["env"]["GOOS"], call.kwargs["env"]["GOARCH"]) for call in builds},
                         {("linux", "amd64"), ("linux", "arm64"), ("darwin", "amd64"), ("darwin", "arm64")})
        self.assertTrue(all(call.kwargs["env"]["CGO_ENABLED"] == "0" for call in builds))
        self.assertIn("-run=^TestGaussDBDriverCoexistence$", command.call_args.args[0])
        self.assertEqual(list(self.root.iterdir()), [])

    def run_fake_lifecycle(self, fail_probe=False, fail_readiness=False, architecture="x86_64"):
        resources = {}
        report = self.root / "report.json"
        starts = []

        def inspect(kind, name):
            return resources.get((kind, name))

        def command(args, **kwargs):
            if kwargs.get("output"):
                kwargs["output"].write_text("native log\n")
            if args[:2] == ["docker", "info"]:
                return subprocess.CompletedProcess(args, 0, "linux/" + architecture + "\n", "")
            if args[:2] == ["docker", "build"]:
                self.assertEqual((Path(args[-1]) / "entrypoint.sh").read_text(), (GATE.ROOT / "business/gaussdb/entrypoint.sh").read_text())
                self.assertEqual((Path(args[-1]) / "Dockerfile").read_text(), (GATE.ROOT / "business/gaussdb/Dockerfile").read_text())
                self.assertIn("GAUSSDB_BASE_IMAGE=fixed", args)
                owner = args[args.index("--label") + 1].split("=", 1)[1]
                resources[("image", args[args.index("-t") + 1])] = {"Id": "sha256:fixture", "Architecture": GATE.IMAGE.go_architecture(architecture), "Config": {"Labels": {GATE.LABEL: owner}}}
            elif args[:3] in (["docker", "network", "create"], ["docker", "volume", "create"]):
                owner = args[args.index("--label") + 1].split("=", 1)[1]
                resources[(args[1], args[-1])] = {"Labels": {GATE.LABEL: owner}}
            elif args[:2] == ["docker", "run"]:
                starts.append(kwargs["env"])
                self.assertNotIn(kwargs["env"]["GAUSSDB_PASSWORD"], " ".join(args))
                self.assertNotEqual(kwargs["env"]["GAUSSDB_PASSWORD"], kwargs["env"]["GAUSSDB_APP_PASSWORD"])
                owner = args[args.index("--label") + 1].split("=", 1)[1]
                resources[("container", args[args.index("--name") + 1])] = {"Config": {"Labels": {GATE.LABEL: owner}}, "State": {"Running": not fail_readiness}, "NetworkSettings": {"Ports": {"5432/tcp": [{"HostPort": "35432"}]}}}
            elif args[:2] == ["docker", "rm"]:
                resources.pop(("container", args[-1]))
            elif args[:3] in (["docker", "network", "rm"], ["docker", "image", "rm"], ["docker", "volume", "rm"]):
                resources.pop((args[1], args[-1]))
            elif args[:2] == ["go", "test"]:
                self.assertIn("password=" + starts[0]["GAUSSDB_APP_PASSWORD"], kwargs["env"]["ADDP_TEST_GAUSSDB_DSN"])
                kwargs["output"].write_text(kwargs["env"]["ADDP_TEST_GAUSSDB_DSN"])
                if fail_probe:
                    raise subprocess.CalledProcessError(1, args)
            elif args[-1] == "--health":
                return subprocess.CompletedProcess(args, 0, "healthy", "diagnostic " + starts[0]["GAUSSDB_APP_PASSWORD"])
            return subprocess.CompletedProcess(args, 0, "", "")

        def native_sql(args, **kwargs):
            statement = kwargs["input"]
            self.assertNotIn(statement, " ".join(args))
            return subprocess.CompletedProcess(args, 0, "持久化保留中文|42\n" if "SELECT (SELECT customer_name" in statement else "", "")

        with patch.object(GATE.platform, "system", return_value="Linux"), \
             patch.object(GATE.platform, "machine", return_value=architecture), \
             patch.object(GATE.shutil, "which", return_value="/available"), \
             patch.object(GATE.IMAGE, "download", return_value=self.root / "archive"), \
             patch.object(GATE.IMAGE, "extract_kernel"), \
             patch.object(GATE, "prepare_probe", return_value=self.root / "probe"), \
             patch.object(GATE, "inspect", side_effect=inspect), \
             patch.object(GATE, "command", side_effect=command), \
             patch.object(GATE.subprocess, "run", side_effect=native_sql), \
             patch.dict(GATE.os.environ, {"ADDP_GAUSSDB_CERTIFICATION_REPORT": str(report)}), \
             patch.object(GATE.IMAGE, "media_facts", return_value={name: "fixed" for name in GATE.IMAGE.FACT_NAMES}):
            if fail_probe:
                with self.assertRaises(subprocess.CalledProcessError): GATE.main()
            elif fail_readiness:
                with self.assertRaisesRegex(RuntimeError, "exited"): GATE.main()
            else:
                GATE.main()
                self.assertEqual(len(starts), 2)
        self.assertEqual(resources, {})
        self.assertNotIn("password=", report.read_text())
        health_log = report.with_suffix(".health.log")
        if not fail_readiness:
            entries = [json.loads(line) for line in health_log.read_text().splitlines()]
            self.assertEqual(len(entries), 2)
            self.assertEqual(entries[0], {"returncode": 0, "stdout": "healthy", "stderr": "diagnostic [REDACTED]"})
        return json.loads(report.read_text())

    def test_arm_native_lifecycle_has_separate_architecture_and_image_evidence(self):
        report = self.run_fake_lifecycle(architecture="aarch64")
        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["runner"]["architecture"], "arm64")
        self.assertEqual(report["image"]["id"], "sha256:fixture")
        self.assertTrue(report["business_lifecycle"]["container_recreated"])

    def test_owner_success_waits_for_probe_and_all_cleanup(self):
        report = self.run_fake_lifecycle()
        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["cleanup"], {"result": "passed", "residual_resources": 0})

    def test_native_probe_failure_is_reported_failed_and_resources_still_removed(self):
        report = self.run_fake_lifecycle(fail_probe=True)
        self.assertEqual(report["result"], "failed")
        self.assertEqual(report["cleanup"]["result"], "passed")

    def test_business_initialization_failure_never_runs_probe_and_still_cleans_up(self):
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
        self.assertIn("ubuntu-24.04-arm", job)
        self.assertIn("fail-fast: false", job)
        self.assertIn("name: gaussdb-official-media-certification-${{ matrix.arch }}", job)
        self.assertIn("make test-gaussdb-certification-build", job)

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
        self.assertNotIn("github.com/HuaweiCloudDeveloper/gaussdb-go", result.stdout)

    def test_probe_module_is_standalone_and_preserves_the_pinned_driver_and_assertions(self):
        original_mod = (GATE.ROOT / "common/go.mod").read_bytes()
        original_sum = (GATE.ROOT / "common/go.sum").read_bytes()
        with patch.object(GATE, "verify_driver") as verify:
            probe = GATE.prepare_probe(self.root, GATE.IMAGE.media_facts("x86_64"))
        verify.assert_called_once()
        module = json.loads(subprocess.check_output(["go", "mod", "edit", "-json"],
                            cwd=probe, text=True, env={**GATE.os.environ, "GOWORK": "off"}))
        common = json.loads(subprocess.check_output(["go", "mod", "edit", "-json"],
                            cwd=GATE.ROOT / "common", text=True, env={**GATE.os.environ, "GOWORK": "off"}))
        self.assertEqual(module["Module"]["Path"], "addp.local/gaussdb-certification")
        self.assertEqual(module["Go"], common["Go"])
        postgres = next(item for item in common["Require"] if item["Path"] == "github.com/lib/pq")
        self.assertIn(postgres, module["Require"])
        self.assertFalse(module.get("Replace"))
        self.assertIn({"Path": "github.com/HuaweiCloudDeveloper/gaussdb-go", "Version": "v1.0.0-rc1"}, module["Require"])
        self.assertEqual((probe / "certification_integration_test.go").read_bytes(), GATE.PROBE_SOURCE.read_bytes())
        self.assertEqual((GATE.ROOT / "common/go.mod").read_bytes(), original_mod)
        self.assertEqual((GATE.ROOT / "common/go.sum").read_bytes(), original_sum)


if __name__ == "__main__":
    unittest.main()
