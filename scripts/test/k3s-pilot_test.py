import argparse
from copy import deepcopy
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
            with patch.dict("os.environ", {"ADDP_INFRA_DATABASE_PROVIDER": "postgres", "TZ": "Asia/Tokyo"}):
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
            self.assertEqual(fields["ADDP_INFRA_DATABASE_PROVIDER"]["value"], "postgres")
            self.assertEqual(fields["TZ"]["value"], "Asia/Tokyo")
            self.assertEqual(fields["SERVICE_HOST"]["valueFrom"]["fieldRef"]["fieldPath"], "status.podIP")
            self.assertEqual(fields["ADDP_RUNTIME_LOG_ROOT"]["value"], container["volumeMounts"][0]["mountPath"])

    def test_catalog_discovery_port_and_process_recovery(self):
        endpoint, deployment = pilot.product_objects("catalog", bundle("amd64")["images"], "intel", "amd64")
        port, = endpoint["spec"]["ports"]
        self.assertEqual(port, {"name": "http", "protocol": "TCP", "port": 8192, "targetPort": 8192})
        self.assertFalse(endpoint["spec"].get("publishNotReadyAddresses", False))
        container, = deployment["spec"]["template"]["spec"]["containers"]
        for key in ("startupProbe", "livenessProbe"):
            self.assertEqual(container[key]["httpGet"], {"port": 8192, "path": "/health/live"})
        self.assertEqual(container["readinessProbe"]["httpGet"], {"port": 8192, "path": "/health/ready"})

    def test_discovery_credentials_belong_only_to_system(self):
        _, system = pilot.product_objects("system", bundle("arm64")["images"], "arm", "arm64")
        _, catalog = pilot.product_objects("catalog", bundle("amd64")["images"], "intel", "amd64")
        _, role, binding, config = pilot.discovery_resources("pilot", "trusted-service", system, catalog)
        self.assertEqual(role["rules"][0]["resourceNames"], ["catalog-backend"])
        self.assertEqual({(tuple(rule["resources"]), tuple(rule["verbs"])) for rule in role["rules"]},
                         {(("services",), ("list", "watch")), (("endpointslices",), ("list", "watch"))})
        self.assertEqual(binding["subjects"][0]["namespace"], "pilot")
        self.assertEqual(json.loads(config["data"]["binding.json"])["service_uid"], "trusted-service")
        self.assertTrue(config["immutable"])
        self.assertEqual(system["spec"]["template"]["spec"]["serviceAccountName"], "catalog-discovery")
        self.assertEqual(catalog["spec"]["replicas"], 2)
        catalog_pod = catalog["spec"]["template"]["spec"]
        self.assertNotIn("serviceAccountName", catalog_pod)
        self.assertFalse(catalog_pod["automountServiceAccountToken"])
        self.assertIn({"name": "CATALOG_DISCOVERY_SOURCE", "value": "kubernetes"}, catalog_pod["containers"][0]["env"])

    def test_site_inspection_accepts_only_bound_ready_pod_addresses(self):
        namespace, uid = "pilot", "service-1"
        port = {"name": "http", "port": 8192, "protocol": "TCP"}
        svc = {"metadata": {"name": "catalog-backend", "namespace": namespace, "uid": uid},
               "spec": {"selector": {"app": "catalog"}, "ports": [port]}}
        owner = {"apiVersion": "v1", "kind": "Service", "name": "catalog-backend", "uid": uid, "controller": True}
        endpoint = {"targetRef": {"kind": "Pod", "namespace": namespace, "uid": "pod-1"},
                    "addresses": ["10.42.1.7"], "conditions": {"ready": True, "terminating": False}}
        labels = {"kubernetes.io/service-name": "catalog-backend",
                  "endpointslice.kubernetes.io/managed-by": "endpointslice-controller.k8s.io"}
        valid = {"metadata": {"namespace": namespace, "ownerReferences": [owner], "labels": labels},
                 "addressType": "IPv4", "endpoints": [endpoint], "ports": [port]}
        self.assertEqual(pilot.catalog_endpoints(namespace, uid, svc, [valid, valid]),
                         [{"pod_uid": "pod-1", "address": "10.42.1.7", "port": 8192}])
        for path, value in [
                (("metadata", "namespace"), "other"),
                (("metadata", "ownerReferences", 0, "uid"), "replaced"),
                (("metadata", "labels", "endpointslice.kubernetes.io/managed-by"), "foreign"),
                (("ports", 0, "protocol"), "UDP"), (("ports", 0, "name"), "admin"),
                (("endpoints", 0, "conditions", "terminating"), True),
                (("endpoints", 0, "conditions", "ready"), None),
                (("endpoints", 0, "targetRef", "namespace"), "other"),
                (("endpoints", 0, "targetRef", "uid"), "")]:
            with self.subTest(path=path):
                candidate = deepcopy(valid)
                parent = candidate
                for key in path[:-1]:
                    parent = parent[key]
                parent[path[-1]] = value
                self.assertEqual(pilot.catalog_endpoints(namespace, uid, svc, [candidate]), [])
        with self.assertRaisesRegex(ValueError, "binding"):
            pilot.catalog_endpoints(namespace, "retired-service", svc, [valid])
        svc["spec"]["publishNotReadyAddresses"] = True
        with self.assertRaisesRegex(ValueError, "binding"):
            pilot.catalog_endpoints(namespace, uid, svc, [valid])

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
            args = argparse.Namespace(arm64=root, amd64=root, namespace="pilot", context="test", catalog_source="system",
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
