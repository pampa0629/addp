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
import zipfile
from unittest.mock import patch
from types import SimpleNamespace

GATE = importlib.import_module("scripts.test.manager-sgm-local-gate")
CHECK = importlib.import_module("scripts.ci.check-online-ci-registration")
WORKER = importlib.import_module("scripts.test.manager-sgm-local-worker")


class LocalSGMContract(unittest.TestCase):
    def test_official_autumn_archive_extracts_only_pinned_members_and_preserves_layout(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / 'private.zip'
            files = {'AutumnTree.SGM': b'model', 'Textures/hongye.PNG': b'leaves', 'Textures/shugan.jpg': b'trunk'}
            with zipfile.ZipFile(archive, 'w') as bundle:
                for name, raw in files.items():
                    bundle.writestr('resources/RealspaceEditor/3D/Model/' + name, raw)
                bundle.writestr('../never-extract', b'private')
            with patch.dict(GATE.AUTUMN_FILES, {name: GATE.hashlib.sha256(raw).hexdigest() for name, raw in files.items()}, clear=True):
                GATE.extract_autumn(archive, root / 'extracted')
                self.assertEqual({p.relative_to(root / 'extracted').as_posix() for p in (root / 'extracted').rglob('*') if p.is_file()}, set(files))
                self.assertTrue(all((root / 'extracted' / name).read_bytes() == raw for name, raw in files.items()))
                self.assertTrue(all((root / 'extracted' / name).stat().st_mode & 0o777 == 0o600 for name in files))
            with self.assertRaises(GATE.T2.GateError):
                GATE.extract_autumn(archive, root / 'rejected')
            with self.assertRaises(GATE.T2.GateError):
                GATE.extract_autumn(root / 'missing', root / 'missing-output')

    def test_autumn_oracle_requires_both_used_images_and_exact_leaf_alpha_pixels(self):
        doc = {'meshes': [{'primitives': [
            {'attributes': {'POSITION': 0, 'TEXCOORD_0': 4}, 'indices': 1, 'material': 0},
            {'attributes': {'POSITION': 2, 'TEXCOORD_0': 5}, 'indices': 3, 'material': 1}]}],
            'accessors': [{'count': 1000}, {'count': 3000}, {'count': 8589}, {'count': 18819}],
            'images': [{'mimeType': 'image/jpeg', 'bufferView': 0}, {'mimeType': 'image/png', 'bufferView': 1}],
            'bufferViews': [{'byteOffset': 0, 'byteLength': 1}, {'byteOffset': 1, 'byteLength': 1}],
            'textures': [{'source': 0}, {'source': 1}], 'materials': [
                {'pbrMetallicRoughness': {'baseColorTexture': {'index': 0}}},
                {'alphaMode': 'BLEND', 'pbrMetallicRoughness': {'baseColorTexture': {'index': 1}}}]}
        raw = bytearray(30)
        struct.pack_into('<I', raw, 12, 0)
        raw[29] = 1
        histogram = [774205, 126685] + [0] * 253 + [147686]
        def image_open(stream):
            leaf = stream.read() == b'\1'
            image = SimpleNamespace(size=(1024, 1024), mode='RGBA' if leaf else 'RGB',
                getextrema=lambda: [(0, 255)] * 3 + [(0, 255) if leaf else (255, 255)], tobytes=lambda: b'decoded',
                getchannel=lambda _: SimpleNamespace(histogram=lambda: histogram if leaf else [0] * 255 + [1048576]))
            image.convert = lambda _: image
            return image
        summary = WORKER.glb_summary(raw, doc, image_open, 'autumn-tree')
        self.assertEqual((summary['vertices'], summary['triangles'], len(summary['images'])), (9589, 7273, 2))
        self.assertEqual(summary['images'][1]['alpha_pixel_counts'], [774205, 126685, 147686])
        for corrupt in (lambda d: d['images'].pop(),
                        lambda d: d['textures'][1].update(source=0),
                        lambda d: d['materials'][0].update(alphaMode='BLEND'),
                        lambda d: d['materials'][1].update(alphaMode='OPAQUE'),
                        lambda d: d['meshes'][0]['primitives'][1]['attributes'].pop('TEXCOORD_0')):
            broken = copy.deepcopy(doc)
            corrupt(broken)
            with self.assertRaises(ValueError):
                WORKER.glb_summary(raw, broken, image_open, 'autumn-tree')
        histogram[1] -= 1
        histogram[255] += 1
        with self.assertRaisesRegex(ValueError, 'alpha pixels'):
            WORKER.glb_summary(raw, doc, image_open, 'autumn-tree')

    def test_private_snapshot_keeps_normal_git_public_and_executable_modes(self):
        with tempfile.TemporaryDirectory() as directory:
            parent = Path(directory)
            source, checkout = parent / 'source', parent / 'checkout'
            source.mkdir()
            (source / 'manifest.json').write_text('{"scripts":[]}')
            executable = source / 'run.sh'
            executable.write_text('#!/bin/sh\nexit 0\n')
            executable.chmod(0o755)
            for args in (("init", "--initial-branch=main"), ("add", "."),
                         ("-c", "user.name=SGM test", "-c", "user.email=sgm@example.invalid", "commit", "-m", "fixture")):
                subprocess.run(['git', '-C', str(source), *args], check=True, capture_output=True)
            previous = os.umask(0o077)
            try:
                denied = parent / 'denied-checkout'
                subprocess.run(['git', 'clone', '--local', '--no-hardlinks', str(source), str(denied)],
                               check=True, capture_output=True)
                self.assertEqual((denied / 'manifest.json').stat().st_mode & 0o777, 0o600)
                GATE.clone_source(source, checkout)
            finally:
                os.umask(previous)
            self.assertEqual(parent.stat().st_mode & 0o777, 0o700)
            self.assertEqual((checkout / 'manifest.json').stat().st_mode & 0o777, 0o644)
            self.assertEqual((checkout / 'run.sh').stat().st_mode & 0o777, 0o755)
            self.assertEqual(checkout.stat().st_mode & 0o777, 0o755)

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
        image = SimpleNamespace(size=(256, 256), mode='RGBA', getextrema=lambda: [(0, 255)] * 4, tobytes=lambda: b"decoded",
            getchannel=lambda _: SimpleNamespace(histogram=lambda: [1] + [0] * 254 + [65535]))
        image.convert = lambda _: image
        self.assertEqual(WORKER.glb_summary(raw, doc, lambda _: image, 'compass')["triangles"], 94)
        for corrupt in (lambda d: d["accessors"][0].update(count=0),
                        lambda d: d["meshes"][0]["primitives"][0]["attributes"].pop("TEXCOORD_0"),
                        lambda d: d["materials"][0].update(alphaMode="OPAQUE"),
                        lambda d: d["materials"][0]["pbrMetallicRoughness"].clear(),
                        lambda d: d["textures"][0].update(source=1)):
            broken = copy.deepcopy(doc)
            corrupt(broken)
            with self.assertRaises(ValueError):
                WORKER.glb_summary(raw, broken, lambda _: image, 'compass')

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

    def test_runtime_registration_declares_extension_workflow_and_uses_real_create_test_contract(self):
        descriptor = GATE.sgm_runtime_descriptor()
        trace = []
        def request(method, path, expected, body):
            if path == '/api/v1/system/engines':
                self.assertEqual(body['engine_origin'], 'extension')
                capabilities = body['capabilities']
                self.assertEqual(capabilities['schema_version'], 'engine.capabilities/v1')
                self.assertEqual(capabilities['engine_type'], body['engine_type'])
                self.assertEqual(capabilities['compute']['workflow']['runtime_api'], 'addp.workflow/v1')
                self.assertIn('direct', capabilities['compute']['workflow']['supported_operator_mode'])
                return SimpleNamespace(status=201, payload={'id': 12, 'engine_type': 'supermap_workflow'})
            self.assertEqual((method, path), ('POST', '/api/v1/system/engines/12/test'))
            return SimpleNamespace(status=200, payload={'success': True})
        with patch.object(GATE.MANAGER, 'GatewayClient', return_value=SimpleNamespace(request=request)):
            self.assertEqual(GATE.register_engine({'SYSTEM_URL': 'http://127.0.0.1:12345'}, 'private-token', descriptor, trace), 12)
        self.assertEqual([event['http_status'] for event in trace], [201, 200])
        self.assertNotIn('private-token', json.dumps(trace))

    def test_registration_failure_keeps_status_code_but_no_response_body_or_secrets(self):
        trace = []
        response = SimpleNamespace(status=400, payload={'error_code': 'engine_type_unsupported', 'error': 'SECRET SDK BODY'})
        with patch.object(GATE.MANAGER, 'GatewayClient', return_value=SimpleNamespace(request=lambda *args: response)):
            with self.assertRaises(GATE.T2.GateError):
                GATE.register_engine({'SYSTEM_URL': 'http://127.0.0.1:12345'}, 'PRIVATE', GATE.sgm_runtime_descriptor(), trace)
        self.assertEqual(trace[0]['error_code'], 'engine_type_unsupported')
        self.assertNotIn('SECRET', json.dumps(trace))

    def test_failure_and_capability_diagnostics_exclude_private_payloads(self):
        error = GATE.MANAGER.SuiteError('GET private returned HTTP 403 (permission_denied) SECRET_MODEL')
        self.assertEqual(GATE.safe_failure(error), {'type': 'SuiteError', 'http_status': 403, 'error_code': 'permission_denied'})
        self.assertEqual(GATE.safe_failure(ValueError('SECRET')), {'type': 'ValueError'})
        facts = GATE.capability_facts({'status': 'unavailable', 'unavailable_reason': 'operator_unavailable',
            'source_kind': 'model_3d', 'render_source': '', 'connection_info': {'password': 'SECRET'},
            'available_actions': ['SECRET_MODEL', 'generate_model_3d_glb']})
        self.assertEqual(facts, {'status': 'unavailable', 'unavailable_reason': 'operator_unavailable',
            'source_kind': 'model_3d', 'glb_generation_available': True})
        self.assertNotIn('SECRET', json.dumps(facts))

    def test_discovery_readiness_only_polls_reads_and_never_repeats_business_commands(self):
        unavailable = {'unavailable_reason': 'operator_unavailable', 'available_actions': []}
        ready = {'available_actions': ['generate_model_3d_glb']}
        requests = []
        def request(method, path, expected):
            requests.append((method, path, expected))
            return SimpleNamespace(payload=ready)
        client = SimpleNamespace(request=request)
        with patch.object(GATE.time, 'sleep'):
            self.assertEqual(GATE.wait_for_generation_capability(client, 'locator=private', unavailable), ready)
        self.assertEqual(requests, [('GET', '/api/v1/manager/quick-view/capability?locator=private', (200,))])
        with self.assertRaises(GATE.T2.GateError):
            GATE.wait_for_generation_capability(client, '', unavailable, timeout=0)
        with self.assertRaises(GATE.T2.GateError):
            GATE.wait_for_generation_capability(client, '', {'unavailable_reason': 'invalid_source'})

    def test_unchanged_unavailable_default_mode_is_verified_without_redundant_switch(self):
        def request(method, path, expected):
            self.assertEqual(method, 'GET')
            return SimpleNamespace(payload={'preferred_mode': 'map_quick_view'})
        client = SimpleNamespace(request=request)
        fixture = GATE.MANAGER.ArtifactFixture('sgm', {}, 'addp://engine/1/path/source?type=object')
        fixture.previous_mode, fixture.mode_changed = 'map_quick_view', True
        GATE.prepare_preview_restore(client, fixture)
        GATE.MANAGER.cleanup_managed_artifact(client, fixture, 1)
        self.assertTrue(fixture.cleanup['preview_mode_restored'])
        fixture.previous_mode = 'basic_preview'
        GATE.prepare_preview_restore(client, fixture)
        self.assertTrue(fixture.mode_changed)

    def test_conversion_audit_uses_owned_result_detail_without_expanding_execution_projection(self):
        fixture = GATE.MANAGER.ArtifactFixture('sgm', {'fingerprint': 'owned'}, 'addp://engine/2/path/source')
        fixture.result_id, fixture.task_id, fixture.execution_id = 8, 9, 'execution-id'
        artifact = {'id': 8, 'tenant_id': 2, 'task_id': 9, 'last_execution_id': 'execution-id',
            'item_fingerprint': 'owned', 'source_format': 'sgm', 'status': 'ready', 'metadata': {
                'sgm_conversion': {'engine_id': 3, 'operator': 'sgm_to_osgb'},
                'workflow_runtime': {'engine_id': 1, 'engine_type': 'model3d_workflow', 'operator': 'osgb_to_glb'}}}
        def request(method, path, expected):
            self.assertEqual((method, path, expected), ('GET', '/api/v1/manager/model_3d_glb/8', (200,)))
            return SimpleNamespace(payload=artifact)
        client = SimpleNamespace(request=request)
        _, stages = GATE.verify_conversion_stages(client, fixture, 2, 3)
        self.assertEqual(stages, {'sgm_operator': 'sgm_to_osgb', 'sgm_engine_id': 3, 'glb_operator': 'osgb_to_glb', 'glb_engine_id': 1})
        for mutate in (lambda a: a.update(tenant_id=4),
                       lambda a: a['metadata']['sgm_conversion'].update(engine_id=4),
                       lambda a: a['metadata']['workflow_runtime'].update(operator='other')):
            original = copy.deepcopy(artifact)
            mutate(artifact)
            with self.assertRaises(GATE.T2.GateError):
                GATE.verify_conversion_stages(client, fixture, 2, 3)
            artifact = original

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
