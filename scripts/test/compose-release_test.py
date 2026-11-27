#!/usr/bin/env python3
"""Regression checks for immutable Compose images and production build boundaries."""

import hashlib
import importlib
import importlib.util
import json
import os
import re
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch


ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
spec = importlib.util.spec_from_file_location("compose_release", ROOT / "scripts/prod/release-images.py")
release = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = release
spec.loader.exec_module(release)
REFERENCE = "registry.invalid/addp-fixture@sha256:" + "a" * 64
IMAGE_ID = "sha256:" + "c" * 64


class HostedMySQLReleaseTest(unittest.TestCase):
    def test_fixture_file_carries_the_project_used_by_minio_initialization(self):
        entry = ROOT / "scripts/test/compose-mysql-platform-online.sh"
        fixture = entry.read_text().split("<<'YAML'\n", 1)[1].split("\nYAML\n", 1)[0]
        env = dict(os.environ, ADDP_MYSQL_FIXTURE_PROJECT="hosted-infra-test", ADDP_DEPLOYMENT_NETWORK="hosted-net",
                   MYSQL_FIXTURE_IMAGE="mysql:8.0", MYSQL_ROOT_PASSWORD="fixture", REDIS_PASSWORD="fixture",
                   MINIO_ROOT_USER="fixture", MINIO_ROOT_PASSWORD="fixture", GITHUB_WORKSPACE=str(ROOT))
        env.pop("COMPOSE_PROJECT_NAME", None)
        with tempfile.TemporaryDirectory() as directory:
            compose = Path(directory, "compose.yml")
            compose.write_text(fixture)
            command = ["docker", "compose", "--env-file", "/dev/null", "-f", compose]
            config = json.loads(subprocess.check_output([*command, "config", "--format", "json"], env=env))
            manager = json.loads(subprocess.check_output(
                [*command, "--profile", "manager", "config", "--format", "json"], env=env,
            ))
            develop = json.loads(subprocess.check_output(
                [*command, "--profile", "develop-file", "config", "--format", "json"], env=env,
            ))
        self.assertEqual(config["name"], "hosted-infra-test")
        self.assertEqual(config["networks"]["default"]["name"], "hosted-net")
        self.assertIn("minio", config["services"])
        self.assertNotIn("manager-search-redis", config["services"])
        self.assertNotIn("meilisearch", config["services"])
        self.assertNotIn("search-source", config["services"])
        self.assertNotIn("geopython-workflow-engine", config["services"])
        runtime = develop["services"]["geopython-workflow-engine"]
        self.assertEqual(runtime["environment"]["SYSTEM_URL"], "http://system-backend:8180")
        self.assertFalse(runtime.get("ports"))
        self.assertFalse(runtime.get("volumes"))
        self.assertIn("meilisearch", manager["services"])
        source = manager["services"]["search-source"]
        self.assertEqual(source["environment"]["POSTGRES_DB"], "search_fixture")
        self.assertFalse(source.get("ports"))
        mounts = manager["services"]["manager-search-redis"]["volumes"]
        script = next(mount["source"] for mount in mounts if mount["target"] == "/addp/manager-search-redis.sh")
        self.assertEqual(Path(script).resolve(), ROOT / "scripts/infra/manager-search-redis.sh")

    def test_hosted_selection_survives_production_env_loading(self):
        entry = ROOT / "scripts/test/compose-mysql-platform-online.sh"
        env_writer = entry.read_text().split("<<'PY'\n", 1)[1].split("\nPY\n", 1)[0]
        keys = (
            "ADDP_DEPLOYMENT_SCOPE", "ADDP_DEPLOYMENT_MODULES", "ADDP_DEPLOYMENT_PROJECT",
            "ADDP_DEPLOYMENT_NETWORK", "ADDP_INFRA_DATABASE_PROVIDER", "ADDP_MYSQL_HOST",
            "ADDP_MYSQL_PORT", "ADDP_MYSQL_SSLMODE", "ADDP_DATABASE_RUNTIME_USER",
            "ADDP_DATABASE_RUNTIME_PASSWORD", "REDIS_HOST", "REDIS_PORT", "NGINX_BIND_HOST", "NGINX_PORT",
            "ADDP_PUBLIC_ORIGIN", "ALLOWED_ORIGINS", "CONSOLE_URL", "PUBLIC_API_URL",
            "ADDP_OBSERVABILITY_LOGS_ENABLED",
        )
        env = dict(os.environ, **dict.fromkeys(keys, "fixture"))
        env.update(ADDP_INFRA_DATABASE_PROVIDER="mysql", NGINX_PORT="18080", REDIS_HOST="redis", REDIS_PORT="6379",
                   ADDP_DATABASE_DEPLOY_PASSWORD="deploy-fixture-only")
        with tempfile.TemporaryDirectory() as work:
            path = Path(work, ".env")
            path.write_text("ADDP_INFRA_DATABASE_PROVIDER=postgres\nNGINX_PORT=80\nREDIS_HOST=localhost\nREDIS_PORT=16379\n")
            subprocess.run([sys.executable, "-c", env_writer], cwd=work, env=env, check=True)
            sourced = subprocess.check_output(
                ["bash", "-ec", 'source .env; printf "%s %s %s:%s" "$ADDP_INFRA_DATABASE_PROVIDER" "$NGINX_PORT" "$REDIS_HOST" "$REDIS_PORT"'],
                cwd=work, text=True,
            )
            self.assertEqual(sourced, "mysql 18080 redis:6379")
            self.assertNotIn("ADDP_DATABASE_DEPLOY_", path.read_text())

    def test_manual_hosted_gate_shell_and_python_syntax(self):
        entry = ROOT / "scripts/test/compose-mysql-platform-online.sh"
        subprocess.run(["bash", "-n", entry], check=True)
        parts = entry.read_text().split("<<'PY'\n")
        self.assertEqual(len(parts), 4)
        for part in parts[1:]:
            compile(part.split("\nPY\n", 1)[0], str(entry), "exec")
        workflow = (ROOT / ".github/workflows/online-mysql-platform.yml").read_text()
        events = workflow.split("\non:\n", 1)[1].split("\npermissions:", 1)[0]
        self.assertEqual(re.findall(r"^  ([\w-]+):", events, re.MULTILINE), ["workflow_dispatch"])
        self.assertIn("options: [gateway, service-lineage, manager, develop-file]", events)
        for name in ("mysql-service-lineage-online.py", "mysql-manager-search-online.py", "mysql-develop-file-online.py"):
            probe = ROOT / "scripts/test" / name
            compile(probe.read_text(), str(probe), "exec")
        self.assertIn("\npermissions:\n  contents: read\n", workflow)
        self.assertIn(f"run: bash {entry.relative_to(ROOT)}", workflow)

    def test_develop_probe_uses_business_pid_and_requires_an_unleased_original_row(self):
        develop = importlib.import_module("scripts.test.mysql-develop-file-online")
        raw = ("Name:\tserver\nUid:\t10001\t10001\t10001\t10001\nGid:\t10001\t10001\t10001\t10001\n"
               "Groups:\t\nCapEff:\t0000000000000000\nNoNewPrivs:\t1")
        with patch.object(develop, "command", return_value=raw) as run:
            self.assertEqual(develop.develop_process_identity("container"),
                             dict(pid=1, uid=10001, gid=10001, supplementary_groups=[],
                                  effective_capabilities=0, no_new_privileges=True))
            run.assert_called_once_with(["docker", "exec", "container", "cat", "/proc/1/status"])
        for wrong in (raw.replace("server", "runtime-log"), raw.replace("10001", "0", 1), raw.replace("CapEff:\t0000000000000000", "CapEff:\t1")):
            with patch.object(develop, "command", return_value=wrong), self.assertRaises(develop.SuiteError):
                develop.develop_process_identity("container")
        waiting = dict(status="pending", attempt=0, lease_absent=True, unstarted=True, admission_state="pending",
                       admission_approval_ids=[], runtime_execution_id=None, authorization_id="7", runtime_status="-")
        develop.verify_pending(waiting, "7")
        for value in (dict(attempt=1), dict(lease_absent=False), dict(admission_approval_ids=["a"]), dict(authorization_id="8")):
            with self.subTest(value=value), self.assertRaises(develop.SuiteError):
                develop.verify_pending(waiting | value, "7")

    def test_develop_fixture_guard_does_not_select_native_mode_for_production(self):
        entry = (ROOT / "scripts/test/compose-mysql-platform-online.sh").read_text()
        fixture = re.search(r"(?m)^develop_file_fixture\(\) \{\n[^}]*^\}", entry).group()
        subprocess.run(["bash", "-ec", fixture + r'''
            python3() { [[ "$ADDP_ONLINE_HOST" == 1 && "$ADDP_ONLINE_HOSTED" == 1 ]]; }
            unset ADDP_ONLINE_HOST ADDP_ONLINE_HOSTED
            develop_file_fixture stop-nfs
            [[ -z "${ADDP_ONLINE_HOST+x}${ADDP_ONLINE_HOSTED+x}" ]]
        '''], check=True)
        self.assertNotRegex(entry, r"(?m)^\s*export .*\bADDP_ONLINE_(?:HOST|HOSTED)=")

    def test_develop_partial_approval_uses_the_public_omitted_receipt(self):
        develop = importlib.import_module("scripts.test.mysql-develop-file-online")
        original = dict(authorization_id="7", scope_digest="authority", execution_id="original")
        first = original | dict(state="approved", approval={"approval_id": "receipt"})
        second = original | dict(state="awaiting_approval")
        develop.verify_one_approval(first, second, "original", "receipt", ("7", "authority"))
        for value in (dict(approval=first["approval"]), dict(state="approved"), dict(execution_id="other")):
            with self.subTest(value=value), self.assertRaises(develop.SuiteError):
                develop.verify_one_approval(first, second | value, "original", "receipt", ("7", "authority"))

    def test_develop_nfs_exports_only_to_the_owned_bridge_and_loopback(self):
        nfs = importlib.import_module("scripts.utils.online_nfs_fixture")
        network = {"Name": "fixture-bridge", "Driver": "bridge", "Scope": "local",
                   "Labels": {"com.docker.compose.project": "fixture-infra", "com.docker.compose.network": "default"},
                   "IPAM": {"Config": [{"Subnet": "172.28.0.0/16", "Gateway": "172.28.0.1"}]}}
        with tempfile.TemporaryDirectory() as work, tempfile.TemporaryDirectory(prefix="addp-develop-file-", dir="/tmp") as owned:
            secret, root = Path(work).resolve(), Path(owned).resolve()
            export = ""

            def run(args, **kwargs):
                nonlocal export
                if args[:3] == ["docker", "network", "inspect"]:
                    return json.dumps([network])
                if args[:2] == ["sudo", "tee"]:
                    export = kwargs["input"]
                if args[:2] == ["showmount", "-e"]:
                    return "Export list:\n" + export
                if args == ["sudo", "exportfs", "-s"]:
                    return export
                return ""

            fixture = nfs.NFSExport("develop-file", run, publication_staging=True)
            with self.assertRaisesRegex(nfs.FixtureError, "owned Compose fixture bridge"):
                fixture.lifecycle("start", secret, "develop-file-1-1", compose_bridge=("fixture-bridge", "wrong"))
            with patch.object(nfs.tempfile, "mkdtemp", return_value=str(root)):
                evidence = fixture.lifecycle("start", secret, "develop-file-1-1", compose_bridge=("fixture-bridge", "fixture-infra"))
            self.assertEqual(evidence, {"started": True, "root_squash": True, "clients": ["127.0.0.1", "172.28.0.0/16"]})
            _, loopback, bridge = export.split()
            self.assertEqual(loopback, "127.0.0.1(rw,sync,no_subtree_check,insecure,root_squash)")
            self.assertEqual(bridge, "172.28.0.0/16(rw,sync,no_subtree_check,insecure,root_squash)")
            descriptor = json.loads((secret / "develop-file-nfs-engine.json").read_text())
            self.assertEqual(descriptor["connection_info"]["server"], "172.28.0.1")
            self.assertEqual((root / ".addp-publication").stat().st_mode & 0o7777, 0o700)


class ReleaseVerificationTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.root_patch = patch.object(release, "ROOT", self.root)
        self.root_patch.start()
        for name in (*release.COMPOSE_FILES.values(), "scripts/infra/traces-platform.yml", "scripts/prod/geopython-metrics.yml"):
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

    def test_infra_selection_is_frozen_and_unverified_full_stack_is_rejected(self):
        for key, selected in release.INFRA_PROVIDERS.items():
            with self.subTest(key=key):
                self.assertEqual(self.manifest["selection"][key], selected)
                with self.assertRaisesRegex(ValueError, key):
                    release.selection({key: "unverified"})

    def test_existing_scoped_mysql_selection(self):
        prefix = """
source "$1"
docker() {
    case "$1" in
      ps) echo "${SYSTEM_ID:-}" ;;
      inspect)
        case "$3" in
          *NetworkSettings*) echo "${NETWORK:-fixture-net}" ;;
          *Config.Env*) echo mysql ;;
          *) return 1 ;;
        esac ;;
      *) return 1 ;;
    esac
}
"""
        for settings, expected in [
            ({"SYSTEM_ID": "system1"}, "mysql"),
            ({"ADDP_INFRA_DATABASE_PROVIDER": "postgres"}, "postgres"),
            ({}, None),
            ({"SYSTEM_ID": "system1", "ADDP_INFRA_DATABASE_PROVIDER": "postgres"}, None),
            ({"SYSTEM_ID": "system1", "NETWORK":"other"}, None),
        ]:
            with self.subTest(settings=settings):
                env = dict(PATH=os.environ["PATH"], ADDP_DEPLOYMENT_SCOPE="system-meta-gateway",
                           ADDP_DEPLOYMENT_PROJECT="fixture", ADDP_DEPLOYMENT_NETWORK="fixture-net", **settings)
                result = subprocess.run(["bash", "-ec", prefix + 'addp_select_scoped_database && echo "$ADDP_INFRA_DATABASE_PROVIDER"',
                                         "_", str(ROOT / "scripts/infra/platform-database.sh")],
                                        env=env, text=True, capture_output=True)
                if expected:
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertEqual(result.stdout.splitlines()[-1], expected)
                else:
                    self.assertNotEqual(result.returncode, 0)


    def test_scoped_database_release_uses_only_external_infra(self):
        for provider in ("postgres", "mysql"):
            env = {"ADDP_DEPLOYMENT_SCOPE": "system-meta-gateway", "ADDP_INFRA_DATABASE_PROVIDER": provider}
            self.assertEqual(release.selection(env)["ADDP_INFRA_DATABASE_PROVIDER"], provider)
            for project in ("infra", "runtimes"):
                self.assertEqual(release.compose_model(project, env), {})
        with self.assertRaisesRegex(ValueError, "ADDP_INFRA_DATABASE_PROVIDER"):
            release.selection({"ADDP_INFRA_DATABASE_PROVIDER": "mysql"})

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

    def test_scoped_database_modules_match_installer_and_runtime_services(self):
        base = {"console", "nginx", "gateway", "system-backend", "meta-backend", "meta-worker", "meta-frontend"}
        for provider, modules in (("mysql", ""), ("mysql", "workbench,inference,system,manager,orchestrator,monitor,standard,model,service,asset,portal,agent,copilot,catalog,graph,ontology,develop,quality,transfer"), ("postgres", "system,workbench,orchestrator,monitor,standard,model,service,asset,portal,agent,copilot,catalog,graph,ontology,develop,quality,transfer")):
            with self.subTest(provider=provider, modules=modules), tempfile.TemporaryDirectory() as temporary:
                env = release.release_environment(dict(os.environ, ADDP_DEPLOYMENT_SCOPE="system-meta-gateway",
                    ADDP_DEPLOYMENT_NETWORK="test-net", ADDP_DEPLOYMENT_PROJECT="test-scoped", NGINX_PORT="8080",
                    ADDP_INFRA_DATABASE_PROVIDER=provider, ADDP_DEPLOYMENT_MODULES=modules),
                    "fixture", "registry.invalid", "linux/amd64")
                model = release.compose_model("platform", env)
                expected = base | {f"{m}-{part}" for m in modules.split(",") if m for part in ("backend", "frontend")}
                expected.discard("copilot-frontend")
                if "quality" in modules.split(","):
                    expected.add("quality-worker")
                if "transfer" in modules.split(","):
                    expected.update({"transfer-bounded-worker","transfer-continuous-worker"})
                self.assertEqual(set(model), expected)
                for owner in {"orchestrator", "monitor", "standard", "model", "service", "asset", "agent", "copilot", "catalog", "graph", "ontology", "develop", "quality", "transfer"}.intersection(modules.split(",")):
                    runtime = model[f"{owner}-backend"]["environment"]
                    self.assertEqual(runtime["ADDP_INFRA_DATABASE_PROVIDER"], provider)
                    for key in ("ADDP_DATABASE_RUNTIME_USER", "ADDP_DATABASE_RUNTIME_PASSWORD", "ADDP_MYSQL_HOST", "ADDP_MYSQL_PORT"):
                        self.assertIn(key, runtime)
                    for key in ("POSTGRES_PASSWORD", "ADDP_DATABASE_DEPLOY_USER", "ADDP_DATABASE_DEPLOY_PASSWORD"):
                        self.assertNotIn(key, runtime)
                if "portal-backend" in model:
                    portal = model["portal-backend"]["environment"]
                    self.assertEqual(portal["ASSET_URL"], "http://asset-backend:8183")
                    self.assertFalse(any(key.startswith(("ADDP_DATABASE_", "POSTGRES_", "ADDP_MYSQL_")) for key in portal))
                override = Path(temporary) / "release.yml"
                release.write_override(override, {name: IMAGE_ID for name in model})
                env.pop("COMPOSE_PROFILES", None)
                self.assertEqual(set(release.compose_model("platform", env, override)), expected)
        for scope, modules in (("full", "workbench"), ("system-meta-gateway", "unknown")):
            with self.assertRaisesRegex(ValueError, "ADDP_DEPLOYMENT_MODULES"):
                release.selection({"ADDP_DEPLOYMENT_SCOPE": scope, "ADDP_DEPLOYMENT_MODULES": modules})

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


class RuntimeHealthSelectionTest(unittest.TestCase):
    def run_health(self, enabled, missing_override=False):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            script = root / "scripts/prod/health-check.sh"
            script.parent.mkdir(parents=True)
            shutil.copyfile(ROOT / "scripts/prod/health-check.sh", script)
            for project, filename in release.COMPOSE_FILES.items():
                services = "  fixture:\n    image: registry:2\n"
                if project == "runtimes":
                    services += "  supermap-workflow-engine:\n    image: registry:2\n    profiles: [supermap-workflow]\n"
                (root / filename).write_text("services:\n" + services)
            override = root / "dist/compose-release/runtimes-images.yml"
            override.parent.mkdir(parents=True)
            if not missing_override:
                selected = {"fixture": IMAGE_ID}
                if enabled:
                    selected["supermap-workflow-engine"] = IMAGE_ID
                release.write_override(override, selected)
            tools = root / "tools"
            tools.mkdir()
            docker = tools / "docker"
            docker.write_text("#!/bin/sh\n"
                              "case \" $* \" in\n"
                              "  *' config '*) exec " + shlex.quote(shutil.which("docker")) + " \"$@\" ;;\n"
                              "  *' ps '*) echo fixture-container ;;\n"
                              "  *' port '*) echo 127.0.0.1:28080 ;;\n"
                              "  *' inspect '*) echo healthy ;;\n"
                              "  *) exit 90 ;;\nesac\n")
            docker.chmod(0o700)
            curl = tools / "curl"
            curl.write_text("#!/bin/sh\nexit 0\n")
            curl.chmod(0o700)
            env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"])
            env.pop("COMPOSE_PROFILES", None)
            return subprocess.run(["bash", str(script)], env=env, capture_output=True, text=True)

    def test_selected_supermap_is_checked_without_startup_profile_environment(self):
        result = self.run_health(True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("✓ supermap-workflow-engine: healthy", result.stdout)

    def test_unselected_supermap_is_not_required_for_health(self):
        result = self.run_health(False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("supermap-workflow-engine", result.stdout)

    def test_missing_frozen_runtime_selection_fails_health(self):
        result = self.run_health(False, missing_override=True)
        self.assertNotEqual(result.returncode, 0)


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
