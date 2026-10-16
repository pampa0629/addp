"""Deterministic local SGM lifecycle contracts; never loads SDK or contacts Docker."""
import copy
import contextlib
import importlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import struct
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace

GATE = importlib.import_module("scripts.test.manager-sgm-local-gate")
CHECK = importlib.import_module("scripts.ci.check-online-ci-registration")
WORKER = importlib.import_module("scripts.test.manager-sgm-local-worker")


class LocalSGMContract(unittest.TestCase):
    def test_no_github_profile_is_admitted_before_mutation(self):
        for environment in ({"GITHUB_ACTIONS": "true"}, {"GITHUB_ACTIONS": "false"},
                            {"ADDP_ONLINE_HOSTED": "1"}, {"ADDP_ONLINE_OWNER_MANAGED": "1"}):
            with self.subTest(environment=environment), patch.dict(os.environ, environment, clear=True), \
                 patch.object(GATE.T2, "command") as command:
                with self.assertRaises(GATE.T2.GateError):
                    GATE.preflight()
                command.assert_not_called()

    def test_non_arm64_host_and_remote_docker_are_refused(self):
        with patch.dict(os.environ, {}, clear=True), patch.object(GATE.platform, "system", return_value="Darwin"), \
             patch.object(GATE.platform, "machine", return_value="x86_64"), patch.object(GATE.T2, "command") as command:
            with self.assertRaises(GATE.T2.GateError):
                GATE.preflight()
            command.assert_not_called()
        with patch.dict(os.environ, {"DOCKER_HOST": "tcp://remote:2376"}, clear=True), \
             patch.object(GATE.platform, "system", return_value="Darwin"), patch.object(GATE.platform, "machine", return_value="arm64"), \
             patch.object(GATE.T2, "command") as command:
            with self.assertRaises(GATE.T2.GateError):
                GATE.preflight()
            command.assert_not_called()

    def test_compose_preserves_product_entry_and_environment_but_owns_all_resources(self):
        doc = {"services": {"model3d-workflow-engine": {
            "image": "mutable", "entrypoint": ["python", "api_server.py"],
            "environment": {"SYSTEM_URL": "http://system-backend:8180"},
            "container_name": "personal-model", "restart": "always", "depends_on": {"system": {}},
            "networks": {"addp-network": {}}, "ports": [{"published": "8101", "host_ip": "0.0.0.0"}],
            "volumes": [{"type": "bind", "source": "/private/data", "target": "/data", "read_only": True}]},
            "postgres": {"image": "postgres", "volumes": [{"type": "volume", "source": "postgres_data", "target": "/var/lib/postgresql/data"}]}}}
        original = copy.deepcopy(doc)
        config = GATE.isolate_compose(doc, list(doc["services"]), "exclusive", {"model3d-workflow-engine": "sha256:" + "a" * 64})
        self.assertEqual(doc, original)
        self.assertTrue(config["networks"]["isolated"]["internal"])
        for name, service in config["services"].items():
            self.assertEqual(service["labels"][GATE.LABEL], "exclusive")
            self.assertEqual(service["container_name"], "exclusive-" + name)
            self.assertNotIn("depends_on", service)
            for mount in service["volumes"]:
                self.assertEqual(mount["type"], "volume")
                self.assertIn(mount["source"], config["volumes"])
                self.assertNotIn("/private", json.dumps(mount))
        runtime = config["services"]["model3d-workflow-engine"]
        self.assertEqual(runtime["entrypoint"], original["services"]["model3d-workflow-engine"]["entrypoint"])
        self.assertEqual(runtime["environment"], original["services"]["model3d-workflow-engine"]["environment"])
        self.assertNotIn("ports", runtime)
        self.assertEqual(set(runtime["networks"]), {"isolated"})
        self.assertEqual(set(config["services"]["postgres"]["networks"]), {"isolated", "ingress"})
        self.assertNotIn("internal", config["networks"]["ingress"])
        self.assertEqual(config["services"]["postgres"]["ports"], [{"target": 5432, "host_ip": "127.0.0.1", "published": "0", "protocol": "tcp"}])

    def test_host_network_or_privileged_product_change_requires_review(self):
        for unsafe in ({"network_mode": "host"}, {"privileged": True}):
            with self.subTest(unsafe=unsafe), self.assertRaises(GATE.T2.GateError):
                GATE.isolate_compose({"services": {"x": {"image": "x", **unsafe}}}, ["x"], "run", {})

    def test_all_failures_cleanup_owned_resources_and_discard_private_logs(self):
        output = io.StringIO()
        with patch.object(GATE, "preflight"), patch.object(GATE, "run", side_effect=ValueError("SECRET_MODEL_BYTES")), \
             patch.object(GATE, "check_network_transport", return_value={}), \
             patch.dict(os.environ, {}, clear=True), patch.object(GATE.T2.Lifecycle, "docker", return_value=""), \
             contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
            self.assertEqual(GATE.main(), 1)
        self.assertNotIn("SECRET_MODEL_BYTES", output.getvalue())

    def test_task_recovery_uses_complete_typed_task_list_and_exact_source(self):
        def client(rows, total):
            def request(method, path, expected):
                self.assertEqual((method, path, expected), ("GET", "/api/v1/manager/tasks?task_type=model_3d_glb_generation&page=1&page_size=100", (200,)))
                return SimpleNamespace(payload={"items": rows, "total": total})
            return SimpleNamespace(request=request)
        task = {"id": 1, "config": {"source": {"item_fingerprint": "owned"}}}
        other = {"id": 2, "config": {"source": {"item_fingerprint": "other"}}}
        self.assertEqual(GATE.model_tasks(client([task, other], 2), "owned"), [task])
        self.assertEqual(GATE.model_tasks(client([other], 1), "owned"), [])
        for rows, total in (([task, task], 2), ([task], 101)):
            with self.assertRaises(GATE.T2.GateError):
                GATE.model_tasks(client(rows, total), "owned")

    def test_cleanup_failure_never_reports_a_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(GATE, "preflight"), patch.object(GATE, "run"), patch.dict(os.environ, {}, clear=True), \
                 patch.object(GATE, "check_network_transport", return_value={}), \
                 patch.object(GATE.T2.Lifecycle, "cleanup", return_value=["network"]), \
                 patch.object(GATE.T2.tempfile, "mkdtemp", return_value=directory), contextlib.redirect_stdout(io.StringIO()):
                self.assertEqual(GATE.main(), 1)

    def test_artifact_oracle_rejects_missing_bound_texture_alpha_uv_and_geometry(self):
        doc = {"meshes": [{"primitives": [{"attributes": {"POSITION": 0, "TEXCOORD_0": 2}, "indices": 1, "material": 0}]}],
               "accessors": [{"count": 136}, {"count": 282}], "images": [{"mimeType": "image/png", "bufferView": 0}],
               "bufferViews": [{"byteLength": 1}], "textures": [{"source": 0}],
               "materials": [{"alphaMode": "BLEND", "pbrMetallicRoughness": {"baseColorTexture": {"index": 0}}}]}
        raw = bytearray(29)
        struct.pack_into("<I", raw, 12, 0)
        image = SimpleNamespace(size=(256, 256), getextrema=lambda: [(0, 255)] * 4, tobytes=lambda: b"decoded")
        image.convert = lambda _: image
        self.assertEqual(WORKER.glb_summary(raw, doc, lambda _: image)["triangles"], 94)
        for corrupt in (lambda d: d["accessors"][0].update(count=0),
                        lambda d: d["meshes"][0]["primitives"][0]["attributes"].pop("TEXCOORD_0"),
                        lambda d: d["materials"][0].update(alphaMode="OPAQUE"),
                        lambda d: d["materials"][0]["pbrMetallicRoughness"].clear(),
                        lambda d: d["textures"][0].update(source=1)):
            broken = copy.deepcopy(doc)
            corrupt(broken)
            with self.assertRaises(ValueError):
                WORKER.glb_summary(raw, broken, lambda _: image)

    def test_failed_build_diagnostics_expose_only_categories_not_private_output(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "private.log"
            with patch.object(GATE.T2.subprocess, "Popen") as popen:
                popen.return_value.communicate.return_value = (
                    b"SECRET_PRIVATE_MODEL Image result: supermap-workflow-engine, exit=1", b"Failed to fetch 502 Bad Gateway PASSWORD")
                popen.return_value.returncode = 1
                with self.assertRaises(GATE.T2.GateError):
                    GATE.T2.command(["make"], failure_log=path)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            facts = GATE.build_diagnostics(path)
            self.assertEqual(facts["categories"], ["apt_fetch_failed", "http_502"])
            self.assertEqual(facts["services"], [{"service": "supermap-workflow-engine", "exit": 1}])
            self.assertNotIn("SECRET", json.dumps(facts))
            self.assertNotIn("PASSWORD", json.dumps(facts))

    def test_wrong_port_ownership_or_nonloopback_is_rejected(self):
        with patch.object(GATE.T2, "command"):
            life = GATE.T2.Lifecycle(prefix="local-", label=GATE.LABEL)
        try:
            document = {"Config": {"Labels": {GATE.LABEL: life.run_id}}, "NetworkSettings": {"Ports": {"8180/tcp": [{"HostIp": "127.0.0.1", "HostPort": "54321"}]}}}
            with patch.object(life, "docker", return_value=json.dumps([document])):
                self.assertEqual(GATE.published_port(life, "system-backend", 8180), 54321)
            document["NetworkSettings"]["Ports"]["8180/tcp"][0]["HostIp"] = "0.0.0.0"
            with patch.object(life, "docker", return_value=json.dumps([document])), self.assertRaises(GATE.T2.GateError):
                GATE.published_port(life, "system-backend", 8180)
        finally:
            shutil.rmtree(life.work)

    def test_minio_initializer_can_target_owned_compose_without_reimplementing_buckets(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            compose = root / "owned.json"
            compose.write_text("{}")
            docker = root / "docker"
            record = root / "trace"
            docker.write_text('#!/bin/bash\nprintf "%s\\n" "$*" >> "$TRACE"\nif [[ "$*" == *" mc ls "* ]]; then exit 1; fi\n')
            docker.chmod(0o700)
            env = {**os.environ, "PATH": str(root) + ":" + os.environ["PATH"], "TRACE": str(record),
                   "ADDP_INFRA_RESOLVED": "1", "COMPOSE_DISABLE_ENV_FILE": "1"}
            result = subprocess.run(["bash", str(GATE.ROOT / "scripts/infra/init-minio.sh"), "--compose-file", str(compose)],
                                    env=env, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            lines = record.read_text().splitlines()
            self.assertTrue(all(str(compose) in line for line in lines if line.startswith("compose -f")))
            self.assertTrue(any('ilm rule add --prefix temp/ --expire-days 7' in line for line in lines))

    def test_platform_registration_rejects_missing_manual_entry_or_public_invocation(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "scripts/test").mkdir(parents=True)
            (root / ".github/workflows").mkdir(parents=True)
            for relative in ("Makefile", "scripts/test/manager-sgm-local-gate.py"):
                shutil.copy2(GATE.ROOT / relative, root / relative)
            CHECK.validate_local_sgm_profile(root)
            (root / ".github/workflows/public.yml").write_text("run: make test-manager-sgm-local-runner")
            CHECK.validate_local_sgm_profile(root)
            (root / ".github/workflows/public.yml").write_text("run: make test-manager-sgm-local")
            with self.assertRaisesRegex(CHECK.RegistrationError, "GitHub Actions"):
                CHECK.validate_local_sgm_profile(root)
            (root / ".github/workflows/public.yml").unlink()
            (root / "Makefile").write_text("test-manager-sgm-local:\n")
            with self.assertRaises(CHECK.RegistrationError):
                CHECK.validate_local_sgm_profile(root)


if __name__ == "__main__":
    unittest.main()
