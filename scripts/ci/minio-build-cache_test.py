import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("minio_cache", Path(__file__).with_name("minio-build-cache.py"))
CACHE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CACHE)
CACHED = "#7 [build 2/2] RUN git clone fixed-source && go build\n#7 CACHED\n"
COLD = "#7 [build 2/2] RUN git clone fixed-source && go build\n#7 DONE 3.2s\n"


class MinioBuildCacheTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="addp-minio-cache-test-")
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name).resolve()
        self.repository = self.base / "repository"
        self.repository.mkdir()
        self.context = self.repository / "scripts/infra"
        self.context.mkdir(parents=True)
        (self.context / "Dockerfile.minio").write_text("FROM fixed@sha256:123 AS build\nRUN compile\n")
        (self.repository / "docker-compose.infra.yml").write_text("services:\n  minio:\n    image: addp-minio:fixed\n")
        (self.repository / "scripts/ci").mkdir()
        (self.repository / "scripts/ci/minio-build-cache.py").write_text("cache contract v1")
        subprocess.run(["git", "init", "-q", str(self.repository)], check=True)
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        self.directory = self.base / "addp-minio-build-cache"
        self.environment = patch.dict(os.environ, {"RUNNER_TEMP": str(self.base)})
        self.environment.start()
        self.addCleanup(self.environment.stop)
        quiet = patch("builtins.print")
        quiet.start()
        self.addCleanup(quiet.stop)
        self.commands = []
        self.uncached = False
        self.binary = "a" * 64 + "  /usr/local/bin/minio\n" + "b" * 64 + "  /usr/local/bin/mc"

    def fake_execute(self, repository, command, log=None):
        self.commands.append(command)
        if command[:3] == ["docker", "image", "inspect"]:
            return json.dumps([{"Os": "linux", "Architecture": "amd64", "Id": "sha256:" + "a" * 64}])
        if command[:3] == ["docker", "image", "save"]:
            Path(command[command.index("--output") + 1]).write_bytes(b"owned image archive")
        if command[:2] == ["docker", "build"] or (command[:2] == ["docker", "compose"]):
            return COLD if self.uncached and "--cache-from" in command else CACHED
        if "sha256sum" in command:
            return self.binary
        return ""

    def build(self, require_hit=False):
        with patch.object(CACHE, "require_runner"), patch.object(CACHE, "execute", side_effect=self.fake_execute):
            return CACHE.build(self.repository, self.directory, require_hit)

    def test_every_build_input_changes_key(self):
        for relative in ("scripts/infra/Dockerfile.minio", "scripts/ci/minio-build-cache.py",
                         "docker-compose.infra.yml"):
            path = self.repository / relative
            before = CACHE.input_key(self.repository)
            path.write_text(path.read_text() + "\nchanged")
            self.assertNotEqual(before, CACHE.input_key(self.repository), relative)
        before = CACHE.input_key(self.repository)
        (self.context / ".dockerignore").write_text(".env\n")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        self.assertNotEqual(before, CACHE.input_key(self.repository))

    def test_cache_path_cannot_escape_runner_or_follow_symlinks(self):
        for path in (self.repository, self.base / "other"):
            with self.assertRaises(ValueError):
                CACHE.cache_path(path)
        self.directory.symlink_to(self.repository, target_is_directory=True)
        with self.assertRaises(ValueError):
            CACHE.cache_path(self.directory)

    def test_cold_build_and_restored_build_preserve_binaries_and_compose(self):
        first = self.build()
        self.assertFalse(first["cache_hit"])
        self.assertGreater(first["archive_bytes"], 0)
        second = self.build(require_hit=True)
        self.assertTrue(second["cache_hit"])
        self.assertEqual(first["binary_sha256"], second["binary_sha256"])
        ordinary = [c for c in self.commands if c[:2] == ["docker", "compose"] and len(c) == 8]
        self.assertTrue(ordinary)
        self.assertTrue(all(c[-2:] == ["build", "minio"] for c in ordinary))
        self.assertFalse(any("up" in c or "prune" in c or "push" in c for c in self.commands))

    def test_warm_proof_must_be_a_real_run_cache_hit(self):
        self.assertTrue(CACHE.all_runs_cached(CACHED))
        self.assertFalse(CACHE.all_runs_cached(COLD))
        self.assertFalse(CACHE.all_runs_cached("#7 CACHED\n"))
        self.assertFalse(CACHE.all_runs_cached(CACHED + "#8 [build 3/3] RUN network\n#8 DONE 1s\n"))
        self.build()
        self.uncached = True
        with self.assertRaisesRegex(ValueError, "executed a Dockerfile RUN"):
            self.build(require_hit=True)

    def test_corrupt_archive_stops_before_docker_load(self):
        self.build()
        (self.directory / "images.tar").write_bytes(b"corrupt")
        self.commands.clear()
        with self.assertRaisesRegex(ValueError, "archive digest differs"):
            self.build()
        self.assertEqual([], self.commands)

    def test_input_mismatch_and_unexpected_names_cannot_be_loaded(self):
        self.build()
        receipt_path = self.directory / "receipt.json"
        receipt = json.loads(receipt_path.read_text())
        receipt["images"]["foreign-image:tag"] = receipt["images"].pop(CACHE.BUILDER_IMAGE)
        receipt_path.write_text(json.dumps(receipt))
        self.commands.clear()
        with self.assertRaisesRegex(ValueError, "unexpected image names"):
            self.build()
        self.assertEqual([], self.commands)

    def test_missing_cache_cannot_pass_required_warm_verification(self):
        with self.assertRaisesRegex(ValueError, "requires an exact cache hit"):
            self.build(require_hit=True)
        self.assertEqual([], self.commands)

    def test_changed_input_rejects_old_receipt_before_load(self):
        self.build()
        (self.context / "Dockerfile.minio").write_text("FROM different-fixed-input\n")
        self.commands.clear()
        with self.assertRaisesRegex(ValueError, "input key or archive digest differs"):
            self.build()
        self.assertEqual([], self.commands)

    def test_incomplete_archive_and_wrong_platform_fail(self):
        self.build()
        (self.directory / "images.tar").unlink()
        self.commands.clear()
        with self.assertRaisesRegex(ValueError, "regular receipt and archive"):
            self.build()
        self.assertEqual([], self.commands)
        with patch.object(CACHE, "execute", return_value=json.dumps([
                {"Os": "linux", "Architecture": "arm64", "Id": "sha256:other"}])):
            with self.assertRaisesRegex(ValueError, "platform differs"):
                CACHE.image_identity(self.repository, "addp-minio:fixed")

    def test_binary_change_fails_without_saving_new_cache(self):
        self.build()
        self.commands.clear()
        self.binary = "c" * 64 + "  /usr/local/bin/minio\n" + "d" * 64 + "  /usr/local/bin/mc"
        with self.assertRaisesRegex(ValueError, "binaries differ"):
            self.build()
        self.assertFalse(any(c[:3] == ["docker", "image", "save"] for c in self.commands))

    def test_build_failure_propagates_before_save_or_product_start(self):
        with patch.object(CACHE, "require_runner"), patch.object(CACHE, "execute",
                side_effect=subprocess.CalledProcessError(17, ["docker", "build"])) as run:
            with self.assertRaises(subprocess.CalledProcessError):
                CACHE.build(self.repository, self.directory, False)
            self.assertEqual(1, run.call_count)
        self.assertFalse((self.directory / "receipt.json").exists())

    def test_refuses_local_or_dirty_runner(self):
        with patch.dict(os.environ, {"GITHUB_ACTIONS": "false", "RUNNER_OS": "Linux"}), \
                patch.object(CACHE.platform, "system", return_value="Linux"), \
                patch.object(CACHE.platform, "machine", return_value="x86_64"):
            with self.assertRaisesRegex(ValueError, "disposable GitHub"):
                CACHE.require_runner(self.repository)
        with patch.dict(os.environ, {"GITHUB_ACTIONS": "true", "RUNNER_OS": "Linux", "RUNNER_ENVIRONMENT": "github-hosted"}), \
                patch.object(CACHE.platform, "system", return_value="Linux"), \
                patch.object(CACHE.platform, "machine", return_value="x86_64"):
            with self.assertRaisesRegex(ValueError, "clean checkout"):
                CACHE.require_runner(self.repository)

    def test_failed_attempt_cannot_publish_the_restored_success_receipt(self):
        self.directory.mkdir()
        (self.directory / "receipt.json").write_text('{"result":"passed"}')
        with patch.dict(os.environ, {"GITHUB_ACTIONS": "true"}), \
                patch.object(CACHE.sys, "argv", ["cache", "build", "--cache-dir", str(self.directory)]), \
                patch.object(CACHE, "build", side_effect=ValueError("corrupt cache")):
            self.assertEqual(1, CACHE.main())
        current = json.loads((self.base / "addp-minio-cache-attempt.json").read_text())
        self.assertEqual("failed", current["result"])
        self.assertEqual("corrupt cache", current["error"])

    def test_compose_build_does_not_read_a_real_unselected_service_password(self):
        command = ["docker", "compose", "build", "minio"]
        with patch.dict(os.environ, {"INFRA_FALKORDB_PASSWORD": "do-not-use"}), \
                patch.object(CACHE.subprocess, "run", return_value=subprocess.CompletedProcess(command, 0, "")) as run:
            CACHE.execute(self.repository, command)
        value = run.call_args.kwargs["env"]["INFRA_FALKORDB_PASSWORD"]
        self.assertGreater(len(value), 30)
        self.assertNotEqual("do-not-use", value)
        self.assertNotIn(value, run.call_args.args[0])

    def test_standard_registration_keeps_two_runners_and_manager_pilot(self):
        make = (ROOT / "Makefile").read_text()
        self.assertIn("test-infra-minio-cache:", make)
        self.assertIn("\t@$(MAKE) test-infra-minio-cache", make)
        workflow = (ROOT / ".github/workflows/infra-minio-cache.yml").read_text()
        self.assertEqual(2, workflow.count("runs-on: ubuntu-24.04"))
        self.assertIn("needs: prepare", workflow)
        self.assertIn("require-hit: 'true'", workflow)
        self.assertNotIn("cancel-in-progress", workflow)
        action = (ROOT / ".github/actions/prepare-minio-build-cache/action.yml").read_text()
        self.assertNotIn("restore-keys", action)
        self.assertNotIn("continue-on-error", action)
        hosted = (ROOT / ".github/workflows/online-t4-gates.yml").read_text()
        manager = hosted.split("  manager-hosted-t4:", 1)[1].split("  orchestrator-hosted-t4:", 1)[0]
        self.assertEqual(1, manager.count("uses: ./.github/actions/prepare-minio-build-cache"))
        self.assertLess(manager.index("Validate Hosted Manager readiness"), manager.index("Prepare fixed-source Infra MinIO"))
        self.assertLess(manager.index("Prepare fixed-source Infra MinIO"), manager.index("Run Hosted Manager Online gate"))


if __name__ == "__main__":
    unittest.main()
