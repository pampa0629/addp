#!/usr/bin/env python3
"""Regression checks for immutable Compose images and production build boundaries."""

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch


ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("compose_release", ROOT / "scripts/prod/release-images.py")
release = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = release
spec.loader.exec_module(release)
REFERENCE = "registry.invalid/addp-fixture@sha256:" + "a" * 64
IMAGE_ID = "sha256:" + "c" * 64


class ReleaseVerificationTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.root_patch = patch.object(release, "ROOT", self.root)
        self.root_patch.start()
        for name in (*release.COMPOSE_FILES.values(), "scripts/infra/traces-platform.yml"):
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("services: {}\n")
        self.directory = self.root / "dist/compose-release"
        self.directory.mkdir(parents=True)
        self.filename = self.directory / "manifest.json"
        self.manifest = {
            "schema": "addp.compose-release/v1", "source_commit": "b" * 40,
            "build_root": "/builder/source", "platform": "linux/arm64",
            "version": "fixture", "registry": "registry.invalid",
            "selection": release.selection({}), "inputs": release.compose_inputs(),
            "projects": {project: {project + "-service": REFERENCE} for project in release.COMPOSE_FILES},
            "sources": {}, "image_ids": {REFERENCE: IMAGE_ID},
        }
        self.models = {project: {project + "-service": {"image": IMAGE_ID, "pull_policy": "never"}}
                       for project in release.COMPOSE_FILES}
        self.save()

    def tearDown(self):
        self.root_patch.stop()
        self.temporary.cleanup()

    def save(self):
        self.filename.write_text(json.dumps(self.manifest))

    def verify(self, env=None):
        with patch.object(release, "native_platform", return_value="linux/arm64"), \
             patch.object(release, "inspect_image", return_value={"Id": IMAGE_ID}) as inspect, \
             patch.object(release, "execute") as execute, \
             patch.object(release, "compose_model", side_effect=lambda project, *_: self.models[project]):
            result = release.verify(self.filename, env or {})
            return result, inspect, execute

    def test_fixed_release_is_verified_without_build_or_mutable_tag_pull(self):
        result, inspect, execute = self.verify()
        self.assertEqual(result, self.filename.resolve())
        inspect.assert_called_once()
        execute.assert_not_called()

    def test_missing_manifest_stops_before_docker(self):
        self.filename.unlink()
        with patch.object(release, "native_platform") as engine:
            with self.assertRaises(FileNotFoundError):
                release.verify(self.filename, {})
            engine.assert_not_called()

    def test_changed_compose_file_is_rejected(self):
        (self.root / "docker-compose.yml").write_text("services: {changed: {}}")
        with self.assertRaisesRegex(ValueError, "Compose source inputs"):
            self.verify()

    def test_capability_change_requires_another_prepared_release(self):
        with self.assertRaisesRegex(ValueError, "Capability selection"):
            self.verify({"ADDP_OBSERVABILITY_LOGS_ENABLED": "false"})

    def test_supermap_selection_change_requires_another_prepared_release(self):
        with self.assertRaisesRegex(ValueError, "Capability selection"):
            self.verify({"ADDP_SUPERMAP_WORKFLOW_ENABLED": "false"})

    def test_wrong_architecture_is_rejected(self):
        self.manifest["platform"] = "linux/amd64"
        self.save()
        with self.assertRaisesRegex(ValueError, "platform differs"):
            self.verify()

    def test_mutable_tag_is_rejected(self):
        self.manifest["projects"]["infra"]["infra-service"] = "registry.invalid/image:latest"
        self.save()
        with self.assertRaisesRegex(ValueError, "repository digest"):
            self.verify()

    def test_build_definition_cannot_survive_the_production_merge(self):
        self.models["runtimes"]["runtimes-service"]["build"] = {"context": "."}
        with self.assertRaisesRegex(ValueError, "Compose image is not frozen"):
            self.verify()

    def test_changed_image_or_extra_service_is_rejected(self):
        self.models["infra"]["unexpected"] = {"image": IMAGE_ID, "pull_policy": "never"}
        with self.assertRaisesRegex(ValueError, "services differ"):
            self.verify()
        del self.models["infra"]["unexpected"]
        self.models["platform"]["platform-service"]["image"] = "registry.invalid/image:latest"
        with self.assertRaisesRegex(ValueError, "not frozen"):
            self.verify()

    def test_missing_local_image_is_pulled_by_digest(self):
        error = subprocess.CalledProcessError(1, ["docker", "image", "inspect"])
        with patch.object(release, "native_platform", return_value="linux/arm64"), \
             patch.object(release, "inspect_image", side_effect=[error, {"Id": IMAGE_ID}]), \
             patch.object(release, "execute") as execute, \
             patch.object(release, "compose_model", side_effect=lambda project, *_: self.models[project]):
            release.verify(self.filename, {})
            self.assertEqual(execute.call_args.args[0], ["docker", "pull", REFERENCE])

    def test_digest_pull_must_match_the_frozen_image_id(self):
        error = subprocess.CalledProcessError(1, ["docker", "image", "inspect"])
        with patch.object(release, "native_platform", return_value="linux/arm64"), \
             patch.object(release, "inspect_image", side_effect=[error, {"Id": "sha256:" + "d" * 64}]), \
             patch.object(release, "execute"):
            with self.assertRaisesRegex(ValueError, "image ID differs"):
                release.verify(self.filename, {})

    def test_failed_digest_pull_never_falls_back_to_build(self):
        error = subprocess.CalledProcessError(1, ["docker", "image", "inspect"])
        with patch.object(release, "native_platform", return_value="linux/arm64"), \
             patch.object(release, "inspect_image", side_effect=error), \
             patch.object(release, "execute", side_effect=error) as execute:
            with self.assertRaises(subprocess.CalledProcessError):
                release.verify(self.filename, {})
            self.assertEqual(execute.call_args.args[0], ["docker", "pull", REFERENCE])

    def test_actual_repository_extends_files_are_part_of_the_input_fingerprint(self):
        (self.root / "scripts/infra/shared.yml").write_text("services: {}\n")
        (self.root / "docker-compose.infra.yml").write_text(
            "services:\n  sample:\n    extends: {file: scripts/infra/shared.yml, service: sample}\n")
        self.assertIn("scripts/infra/shared.yml", release.compose_inputs())

    def test_override_removes_build_and_disables_implicit_pull(self):
        path = self.directory / "infra-images.yml"
        release.write_override(path, {"minio": REFERENCE})
        self.assertIn("build: !reset null", path.read_text())
        self.assertIn("profiles: !reset []", path.read_text())
        self.assertIn("pull_policy: never", path.read_text())
        self.assertIn(REFERENCE, path.read_text())

    def test_current_release_switch_keeps_the_previous_release(self):
        self.filename.unlink()
        self.directory.rmdir()
        first = self.root / "dist/releases/first"
        second = self.root / "dist/releases/second"
        first.mkdir(parents=True)
        second.mkdir()
        (first / "manifest.json").write_text("first")
        (second / "manifest.json").write_text("second")
        release.activate(first)
        release.activate(second)
        self.assertEqual((self.directory / "manifest.json").read_text(), "second")
        self.assertEqual((first / "manifest.json").read_text(), "first")


class RuntimeSelectionTest(unittest.TestCase):
    def runtime_model(self, enabled, platform="linux/amd64"):
        env = release.release_environment(dict(os.environ, ADDP_SUPERMAP_WORKFLOW_ENABLED=enabled),
                                          "fixture", "registry.invalid", platform)
        return release.compose_model("runtimes", env), env

    def test_amd64_release_explicitly_excludes_supermap(self):
        model, _ = self.runtime_model("false")
        self.assertNotIn("supermap-workflow-engine", model)
        self.assertIn("geopython-workflow-engine", model)
        self.assertTrue(all(service.get("platform", "linux/amd64") == "linux/amd64" for service in model.values()))

    def test_supermap_selection_is_strict_boolean(self):
        for value in ("auto", "TRUE", ""):
            with self.subTest(value=value), self.assertRaisesRegex(ValueError, "true or false"):
                release.selection({"ADDP_SUPERMAP_WORKFLOW_ENABLED": value})

    def test_runtime_override_freezes_selected_profiles_for_standard_startup(self):
        for enabled in ("true", "false"):
            with self.subTest(enabled=enabled), tempfile.TemporaryDirectory() as temporary:
                model, env = self.runtime_model(enabled, "linux/arm64")
                override = Path(temporary) / "runtimes-images.yml"
                release.write_override(override, {name: IMAGE_ID for name in model})
                # Startup's shared environment selects observability profiles only.
                env.pop("COMPOSE_PROFILES", None)
                actual = release.compose_model("runtimes", env, override)
                self.assertEqual(set(actual), set(model))
                self.assertEqual("supermap-workflow-engine" in actual, enabled == "true")
                self.assertTrue(all(service["image"] == IMAGE_ID for service in actual.values()))

    def test_selected_arm64_supermap_is_rejected_before_amd64_build(self):
        model, _ = self.runtime_model("true")
        with patch.object(release, "native_platform", return_value="linux/amd64"), \
             patch.object(release, "subprocess") as process, \
             patch.object(release, "output", return_value="b" * 40), \
             patch.object(release, "compose_model", side_effect=lambda project, *_: model if project == "runtimes" else {}), \
             patch.object(release, "execute") as execute:
            process.run.return_value.returncode = 0
            with self.assertRaisesRegex(ValueError, "Selected service platform differs.*supermap-workflow-engine"):
                release.prepare("fixture", "registry.invalid")
            execute.assert_not_called()


class OntologyPackageCoverageTest(unittest.TestCase):
    def setUp(self):
        self.registration = release.load_module("ontology_release_registration", "scripts/ci/select-image-services.py")
        self.registrations = self.registration.registered_services(ROOT)
        env = release.release_environment(os.environ, "fixture", "registry.invalid", "linux/arm64")
        # Resolve the real Compose definition, rather than inventing a service fixture.
        self.frontend = release.compose_model("platform", env)["ontology-frontend"]
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)

    def tearDown(self):
        self.temporary.cleanup()

    def prepare(self, registrations):
        models = {"infra": {}, "platform": {"ontology-frontend": self.frontend}, "runtimes": {}}
        reference = "registry.invalid/addp-ontology-frontend@sha256:" + "a" * 64
        with patch.object(release, "ROOT", self.root), \
             patch.object(release, "native_platform", return_value="linux/arm64"), \
             patch.object(release, "subprocess") as process, \
             patch.object(release, "output", return_value="b" * 40), \
             patch.object(release, "load_module", return_value=self.registration), \
             patch.object(self.registration, "registered_services", return_value=registrations), \
             patch.object(release, "compose_model", side_effect=lambda project, *_: models[project]), \
             patch.object(release, "execute") as execute, \
             patch.object(release, "publish_image", return_value=reference), \
             patch.object(release, "inspect_image", return_value={"Id": IMAGE_ID}), \
             patch.object(release, "prepare_metric_sources", return_value={}), \
             patch.object(release, "input_fingerprints", return_value={}), \
             patch.object(release, "verify"):
            process.run.return_value.returncode = 0
            release.prepare("fixture", "registry.invalid")
            return json.loads((self.root / "dist/compose-release/manifest.json").read_text()), execute.call_args_list

    def test_real_ontology_registration_reaches_manifest_and_offline_export(self):
        manifest, commands = self.prepare(self.registrations)
        reference = manifest["projects"]["platform"]["ontology-frontend"]
        self.assertEqual(manifest["image_ids"][reference], IMAGE_ID)
        self.assertIn("--services ontology-frontend", commands[1].args[0][-1])
        self.assertIn(IMAGE_ID, (self.root / "dist/compose-release/platform-images.yml").read_text())

        # Exercise the actual exporter without a Docker daemon or real image archives.
        source = (ROOT / "scripts/build/package.sh").read_text()
        exporter = source[source.index("export_images() {"):source.index("# Function: Create load-images.sh")]
        executable = self.root / "bin/docker"
        executable.parent.mkdir()
        executable.write_text("#!/usr/bin/env python3\nimport json,os,sys\nfrom pathlib import Path\n"
                              "Path(os.environ['EXPORT_TRACE']).write_text(json.dumps(sys.argv[1:]))\n"
                              "Path(sys.argv[sys.argv.index('-o')+1]).write_bytes(b'image archive fixture')\n")
        executable.chmod(0o755)
        trace = self.root / "export.json"
        subprocess.run(["bash", "-ec", exporter + "\nexport_images\n"],
                       env=dict(os.environ, OUTPUT_DIR=str(self.root), EXPORT_TRACE=str(trace),
                                PATH=str(executable.parent) + os.pathsep + os.environ["PATH"]),
                       capture_output=True, text=True, check=True)
        images = self.root / "images"
        self.assertEqual(json.loads(trace.read_text()), ["save", "-o", str(images / "release.tar.partial"), IMAGE_ID])
        self.assertEqual((images / "references.txt").read_text().splitlines(), [IMAGE_ID])
        self.assertTrue((images / "release.tar").is_file())
        self.assertFalse((images / "release.tar.partial").exists())

    def test_missing_ontology_build_registration_rejects_release_preparation(self):
        registrations = [(name, directory) for name, directory in self.registrations if name != "ontology-frontend"]
        with self.assertRaisesRegex(ValueError, "Product image is not registered: ontology-frontend"):
            self.prepare(registrations)
        self.assertFalse((self.root / "dist/compose-release/manifest.json").exists())


class ProductionServiceAddressTest(unittest.TestCase):
    def test_meta_processes_resolve_transfer_receipts_via_compose_service(self):
        result = subprocess.run([
            "docker", "compose", "--env-file", ".env.example", "-f", "docker-compose.yml",
            "config", "--format", "json",
        ], cwd=ROOT, capture_output=True, text=True, check=True)
        services = json.loads(result.stdout)["services"]
        for name in ("meta-backend", "meta-worker"):
            with self.subTest(service=name):
                self.assertEqual(services[name]["environment"].get("TRANSFER_URL"),
                                 "http://transfer-backend:8083")


class ProductionMetricImageTest(unittest.TestCase):
    def test_source_consumes_frozen_image_from_builder_and_never_builds(self):
        module = release.load_module("production_metric_image_test", "scripts/utils/metrics-source.py")
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "input.go").write_text("package fixture")
            digest = hashlib.sha256(b"input.go\0package fixture").hexdigest()
            manifest = root / "manifest.json"
            manifest.write_text(json.dumps({"build_root": "/builder/source", "sources": {
                "addp-redis-metrics:" + digest: REFERENCE}, "image_ids": {REFERENCE: IMAGE_ID}}))
            docker = Mock()
            source = module.Source(docker, "infra", root, root, "redis", "fixture")
            image = "sha256:" + "c" * 64
            fact = {"Id": image, "RepoDigests": [], "Config": {"Labels": {source.owner: "/builder/source"}}}
            docker.run.return_value = json.dumps([fact])
            with patch.dict(os.environ, {"ADDP_RELEASE_MANIFEST": str(manifest)}):
                self.assertEqual(source.build(["input.go"]), image)
            self.assertTrue(all(call.args[0][0] == "image" for call in docker.run.call_args_list))
            self.assertEqual(source.root, root)

    def test_unpublished_source_is_rejected_before_build(self):
        module = release.load_module("unpublished_metric_image_test", "scripts/utils/metrics-source.py")
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "input.go").write_text("package fixture")
            manifest = root / "manifest.json"
            manifest.write_text(json.dumps({"sources": {}}))
            docker = Mock()
            source = module.Source(docker, "infra", root, root, "redis", "fixture")
            with patch.dict(os.environ, {"ADDP_RELEASE_MANIFEST": str(manifest)}), \
                 self.assertRaisesRegex(ValueError, "missing from the prepared"):
                source.build(["input.go"])
            docker.run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
