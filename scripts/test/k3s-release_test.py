import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


SPEC = importlib.util.spec_from_file_location(
    "k3s_release", Path(__file__).resolve().parents[1] / "prod" / "k3s-release.py"
)
RELEASE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RELEASE)


def frozen(arch):
    projects = {
        "infra": {"postgres": f"registry.local/postgres-{arch}@sha256:" + "a" * 64},
        "platform": {"gateway": "registry.local/gateway@sha256:" + "b" * 64},
        "runtimes": {"python-workflow": "registry.local/python@sha256:" + "c" * 64},
    }
    if arch == "arm64":
        projects["runtimes"]["supermap-workflow-engine"] = "registry.local/supermap@sha256:" + "d" * 64
    metrics = "registry.local/redis-metrics@sha256:" + "e" * 64
    config = "1" if arch == "arm64" else "2"
    return {
        "schema": "addp.compose-release/v1", "source_commit": "3" * 40,
        "version": "example", "platform": f"linux/{arch}", "inputs": {"compose": "4" * 64},
        "selection": {RELEASE.SUPERMAP_OPTION: "true" if arch == "arm64" else "false",
                      "ADDP_REDIS_METRICS_ENABLED": "true"},
        "projects": projects, "sources": {"addp-redis-metrics:" + "5" * 64: metrics},
        "image_ids": {reference: "sha256:" + config * 64
                      for reference in [metrics, *(ref for p in projects.values() for ref in p.values())]},
    }


def deployment(*images):
    containers = [{"name": f"container-{index}", "image": RELEASE.PLACEHOLDER + image}
                  for index, image in enumerate(images)]
    return {"apiVersion": "apps/v1", "kind": "Deployment", "metadata": {"name": "test"},
            "spec": {"template": {"spec": {"containers": containers[:1], "initContainers": containers[1:]}}}}


class K3sReleaseTest(unittest.TestCase):
    def setUp(self):
        self.arm = frozen("arm64")
        self.amd = frozen("amd64")

    def pair(self):
        with tempfile.TemporaryDirectory() as temp:
            arm, amd = Path(temp) / "arm64.json", Path(temp) / "amd64.json"
            arm.write_text(json.dumps(self.arm))
            amd.write_text(json.dumps(self.amd))
            return RELEASE.pair(arm, amd)

    def test_both_native_archives_bind_same_release_reference(self):
        release = self.pair()
        arm = RELEASE.images_for_arch(release, "arm64")
        amd = RELEASE.images_for_arch(release, "amd64")
        gateway = RELEASE.image_ref(release, "platform/gateway")
        self.assertEqual(arm[gateway], "sha256:" + "1" * 64)
        self.assertEqual(amd[gateway], "sha256:" + "2" * 64)
        self.assertEqual(arm.keys() - amd.keys(), {RELEASE.image_ref(release, RELEASE.SUPERMAP)})
        self.assertEqual(arm.keys() & amd.keys(), {
            RELEASE.image_ref(release, item) for item in
            ("infra/postgres", "platform/gateway", "runtimes/python-workflow", "sources/addp-redis-metrics")
        })
        self.assertNotEqual(release["images"]["infra/postgres"]["arm64"]["source"],
                            release["images"]["infra/postgres"]["amd64"]["source"])

    def test_ref_is_bound_to_both_architectures_immutable_image_ids(self):
        first = self.pair()
        gateway = self.amd["projects"]["platform"]["gateway"]
        self.amd["image_ids"][gateway] = "sha256:" + "3" * 64
        second = self.pair()
        self.assertNotEqual(first["id"], second["id"])
        self.assertNotEqual(RELEASE.image_ref(first, "platform/gateway"),
                            RELEASE.image_ref(second, "platform/gateway"))
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "release.json"
            second["id"] = first["id"]
            path.write_text(json.dumps(second))
            with self.assertRaisesRegex(ValueError, "identity"):
                RELEASE.read_release(path)

    def test_pair_rejects_source_option_and_inventory_mismatch(self):
        amd = copy.deepcopy(self.amd)
        self.amd["source_commit"] = "6" * 40
        with self.assertRaisesRegex(ValueError, "source_commit"):
            self.pair()
        self.amd = copy.deepcopy(amd)
        self.amd["selection"]["ADDP_REDIS_METRICS_ENABLED"] = "false"
        with self.assertRaisesRegex(ValueError, "same options"):
            self.pair()
        self.amd = copy.deepcopy(amd)
        self.amd["sources"].clear()
        with self.assertRaisesRegex(ValueError, "metrics sources"):
            self.pair()

    def test_supermap_omission_on_both_architectures_is_explicit(self):
        del self.arm["projects"]["runtimes"]["supermap-workflow-engine"]
        with self.assertRaisesRegex(ValueError, "selected services"):
            self.pair()
        self.arm["selection"][RELEASE.SUPERMAP_OPTION] = "false"
        self.assertNotIn(RELEASE.SUPERMAP, self.pair()["images"])

    def test_render_uses_release_label_without_arch_or_node_for_shared_service(self):
        release = self.pair()
        template = deployment("platform/gateway")
        data = {"apiVersion": "v1", "kind": "List", "items": [template]}
        RELEASE.render(release, data)
        pod = template["spec"]["template"]["spec"]
        self.assertEqual(pod["nodeSelector"], {RELEASE.release_label(release): "ready"})
        self.assertEqual(pod["containers"][0]["image"], RELEASE.image_ref(release, "platform/gateway"))
        self.assertEqual(pod["containers"][0]["imagePullPolicy"], "Never")

    def test_render_checks_init_container_architecture_and_wrong_node_constraint(self):
        release = self.pair()
        template = deployment("platform/gateway", RELEASE.SUPERMAP)
        data = {"apiVersion": "v1", "kind": "List", "items": [template]}
        RELEASE.render(release, data)
        self.assertEqual(template["spec"]["template"]["spec"]["nodeSelector"]["kubernetes.io/arch"], "arm64")
        incompatible = deployment(RELEASE.SUPERMAP)
        pod = incompatible["spec"]["template"]["spec"]
        pod["nodeSelector"] = {"kubernetes.io/arch": "amd64"}
        data["items"] = [incompatible]
        with self.assertRaisesRegex(ValueError, "No compatible architecture"):
            RELEASE.render(release, data)

    def test_render_rejects_unfrozen_image_and_scheduler_bypass(self):
        release = self.pair()
        data = {"apiVersion": "v1", "kind": "List", "items": [deployment("platform/missing")]}
        with self.assertRaisesRegex(ValueError, "not in this ADDP release"):
            RELEASE.render(release, data)
        data["items"] = [deployment("platform/gateway")]
        data["items"][0]["spec"]["template"]["spec"]["nodeName"] = "machine-a"
        with self.assertRaisesRegex(ValueError, "nodeName"):
            RELEASE.render(release, data)


if __name__ == "__main__":
    unittest.main()
