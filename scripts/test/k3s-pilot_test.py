import argparse
import hashlib
import importlib.util
from io import BytesIO
import json
from pathlib import Path
import tarfile
from tempfile import TemporaryDirectory
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("k3s_pilot", Path(__file__).parents[1] / "prod/k3s-pilot.py")
pilot = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(pilot)
PACKAGE_SPEC = importlib.util.spec_from_file_location(
    "k3s_package", Path(__file__).parents[1] / "build/package-k3s-pilot.py")
packager = importlib.util.module_from_spec(PACKAGE_SPEC)
PACKAGE_SPEC.loader.exec_module(packager)


def bundle(arch, commit="a" * 40):
    names = pilot.MODULES + (pilot.INFRA if arch == "arm64" else ())
    return {"schema": "addp.k3s-pilot/v1", "commit": commit, "arch": arch,
            "images": {name: {"image": f"images.invalid/{name}:{arch}", "id": "sha256:" + "c" * 64}
                       for name in names},
            "service_secret_keys": ["CATALOG_SERVICE_CLIENT_SECRET", "GATEWAY_SERVICE_CLIENT_SECRET"]}


class PilotTest(unittest.TestCase):
    def test_system_manifest_includes_machine_credentials_with_digits(self):
        compose = (Path(__file__).parents[2] / "docker-compose.yml").read_text()
        keys = packager.system_secret_keys(compose)
        self.assertIn("MODEL3D_WORKFLOW_SERVICE_CLIENT_SECRET", keys)
        self.assertNotIn("LOG_OBSERVER_SERVICE_CLIENT_SECRET", keys)

    def test_runtime_identity_comes_from_the_exported_image_config(self):
        with TemporaryDirectory() as directory:
            archive = Path(directory) / "images.tar"
            tag = "images.invalid/system:arm64"
            config = b'{"architecture":"arm64","os":"linux"}'
            manifest = json.dumps([{"Config": "image.json", "RepoTags": [tag]}]).encode()
            with tarfile.open(archive, "w") as writer:
                for name, content in (("manifest.json", manifest), ("image.json", config)):
                    info = tarfile.TarInfo(name)
                    info.size = len(content)
                    writer.addfile(info, BytesIO(content))
            self.assertEqual(packager.archive_identities(archive), {tag: "sha256:" + hashlib.sha256(config).hexdigest()})

    def test_native_bindings_and_catalog_advertises_the_pod(self):
        for module, arch, node in (("system", "arm64", "arm"), ("gateway", "arm64", "arm"),
                                   ("catalog", "amd64", "intel")):
            endpoint, deployment = pilot.product_objects(module, bundle(arch)["images"], node, arch)
            pod = deployment["spec"]["template"]["spec"]
            self.assertEqual(pod["nodeSelector"], {"kubernetes.io/arch": arch, "kubernetes.io/hostname": node})
            self.assertFalse(pod["automountServiceAccountToken"])
            container, = pod["containers"]
            self.assertEqual(container["image"], f"images.invalid/{module}:{arch}")
            self.assertEqual(container["imagePullPolicy"], "Never")
            self.assertEqual(container["readinessProbe"]["httpGet"]["path"], "/health/ready")
            self.assertEqual(endpoint["spec"].get("type", "ClusterIP"), "ClusterIP")
            fields = {item["name"]: item for item in container["env"]}
            self.assertEqual(fields["SERVICE_HOST"]["valueFrom"]["fieldRef"]["fieldPath"], "status.podIP")
            self.assertEqual(fields["ADDP_RUNTIME_LOG_ROOT"]["value"], container["volumeMounts"][0]["mountPath"])

    def test_credentials_are_private_and_publisher_secrets_stay_outside_the_cluster(self):
        with TemporaryDirectory() as directory:
            target = Path(directory) / "site.json"
            manifest = bundle("arm64")
            pilot.create_credentials(manifest, target)
            self.assertEqual(target.stat().st_mode & 0o777, 0o600)
            data = pilot.read_private(target)
            with self.assertRaises(FileExistsError):
                pilot.create_credentials(manifest, target)
            self.assertEqual(pilot.read_private(target), data)
            resources = pilot.secret_objects(manifest, data)
            payload = json.dumps(resources)
            for value in data["publishers"].values():
                self.assertNotIn(value, payload)
            meili, = [entry for entry in resources if entry["metadata"]["name"] == "meili"]
            self.assertEqual(meili["stringData"], {"MEILI_MASTER_KEY": data["runtime"]["MEILISEARCH_MASTER_KEY"]})

    def test_cross_commit_pair_rejected_before_any_cluster_access(self):
        args = argparse.Namespace(arm64=Path("arm"), amd64=Path("amd"))
        with patch.object(pilot, "read_bundle", side_effect=[bundle("arm64"), bundle("amd64", "b" * 40)]):
            with patch.object(pilot, "Cluster") as cluster:
                with self.assertRaisesRegex(ValueError, "same source commit"):
                    pilot.install(args)
                cluster.assert_not_called()

    def test_dirty_database_never_starts_system(self):
        with TemporaryDirectory() as directory:
            root = Path(directory)
            args = argparse.Namespace(arm64=root, amd64=root, namespace="pilot", context="test",
                                      arm_node="arm", amd_node="intel", storage_class="local-path", credentials=root)
            (root / "redis-authentication.sh").write_text("# fixture")
            for module in pilot.MODULES:
                (root / f"{module}.json").write_text(json.dumps({"module_name": module}))
            with patch.object(pilot, "read_bundle", side_effect=[bundle("arm64"), bundle("amd64")]):
                with patch.object(pilot, "read_private"), patch.object(pilot, "secret_objects", return_value=[]):
                    with patch.object(pilot, "Cluster") as cluster:
                        instance = cluster.return_value
                        instance.node_arch.side_effect = ["arm64", "amd64"]
                        instance.run.return_value = b"f\n"
                        with self.assertRaisesRegex(ValueError, "empty application database"):
                            pilot.install(args)
                        self.assertEqual(instance.create.call_count, 2)
                        instance.ready.assert_called_once_with(*pilot.INFRA)


if __name__ == "__main__":
    unittest.main()
