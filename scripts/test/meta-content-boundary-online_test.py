import copy
import importlib.util
import sys
import time
import unittest
import urllib.parse
from pathlib import Path
from types import SimpleNamespace

SCRIPT = Path(__file__).with_name("manager-internal-artifact-lineage-online.py")
SPEC = importlib.util.spec_from_file_location("content_boundary_online_owner", SCRIPT)
OWNER = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = OWNER
SPEC.loader.exec_module(OWNER)


class BoundaryAPI:
    def __init__(self, bucket="", fault=""):
        self.bucket, self.fault = bucket, fault
        self.items, self.calls, self.executions = {}, [], 0
        self.sequence = {}

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, copy.deepcopy(body)))
        if method == "POST":
            self.executions += 1
            depth = body["scan_depth"]
            grouped = body.get("ref_groups")
            if body.get("targets"):
                locator = urllib.parse.urlsplit(body["targets"][0])
                if locator.scheme != "addp" or locator.netloc != "engine" or locator.path.split("/", 3)[:3] != ["", "9", "path"]:
                    raise OWNER.SuiteError("outside target must use an exact Engine locator")
                if urllib.parse.parse_qs(locator.query) != {"type": ["object" if self.bucket else "file"]}:
                    raise OWNER.SuiteError("outside target must retain its native leaf type")
                scope = urllib.parse.unquote(locator.path.split("/", 3)[3])
            else:
                scope = grouped[0]["primary"].rsplit("/", 1)[0] if grouped else body["catalog_paths"][0]
                if not grouped and scope.endswith(".dbf"):
                    raise OWNER.SuiteError("a file cannot be enumerated as a directory")
            if scope.endswith("roads.dbf"):
                self.make_item(scope, depth)
            else:
                iteration = self.sequence.get(scope, 0)
                self.sequence[scope] = iteration + 1
                names = ["roads.shp"] if grouped else ["roads.shp", "parcels.shp", "roads.qmd", "readme.txt"]
                for name in names:
                    item = self.make_item(scope + "/" + name, depth)
                    if name.endswith(".shp"):
                        extensions = [".shp", ".shx", ".dbf", ".cpg" if name == "roads.shp" else ".prj"]
                        item["attributes"]["item"] = {"layout": "multi", "format": "shapefile", "data_type": "table", "file_count": 4,
                            "refs": [{"path": scope + "/" + name[:-4] + ext, "primary": ext == ".shp"} for ext in extensions]}
                    if depth == "deep":
                        item["attributes"].update(type_info={"table": {"fields": [{"name": "NAME"}]}}, capabilities={"spatial": {"crs": "EPSG:4326"}})
                item = self.items[scope + "/roads.shp"]
                if iteration == 0 and self.fault == "basic-split":
                    self.make_item(scope + "/roads.dbf", depth)
                if iteration == 0 and self.fault == "basic-enrich":
                    item["attributes"]["type_info"] = {"table": {"fields": ["unexpected"]}}
                if iteration == 0 and self.fault == "claim-unrelated":
                    self.items.pop(scope + "/roads.qmd", None)
                if iteration == 1 and self.fault == "replace-id":
                    item["id"] += 1000
                if iteration == 1 and self.fault == "move-parent":
                    item["node_id"] += 1
                if iteration == 1 and self.fault == "duplicate-ref":
                    item["attributes"]["item"]["refs"].append(copy.deepcopy(item["attributes"]["item"]["refs"][0]))
                if iteration == 1 and self.fault == "outside-ref":
                    item["attributes"]["item"]["refs"][0]["path"] = scope + "/../outside/roads.shp"
                if iteration == 1 and self.fault == "outside-delete":
                    self.items = {name: value for name, value in self.items.items() if "/outside/" not in name}
                if iteration == 1 and self.fault == "no-deep-fields":
                    item["attributes"].pop("type_info", None)
                if iteration == 2 and self.fault == "depth-downgrade":
                    item["scanned_depth"] = "basic"
                if iteration == 2 and self.fault == "discard-deep":
                    item["attributes"].pop("type_info", None)
                if grouped and iteration == 0 and self.fault == "refs-enumerate-sibling":
                    self.make_item(scope + "/parcels.shp", depth)
            return SimpleNamespace(payload={"execution_id": f"boundary-{self.executions}"})
        if "/executions/" in path:
            return SimpleNamespace(payload={"status": "failed" if self.fault == "execution-failure" else "success"})
        if "/engines/" in path:
            return SimpleNamespace(payload=copy.deepcopy(list(self.items.values())))
        item_id = int(path.rsplit("/", 1)[1])
        return SimpleNamespace(payload=copy.deepcopy(next(item for item in self.items.values() if item["id"] == item_id)))

    def make_item(self, name, depth):
        if name not in self.items:
            self.items[name] = {"id": len(self.items) + 1, "full_name": name, "fingerprint": "fingerprint:" + name,
                "node_id": 7, "item_type": "object" if self.bucket else "file", "scanned_depth": "basic",
                "attributes": {"item": {"layout": "single", "format": "unknown", "data_type": "unknown"}}}
        item = self.items[name]
        if depth == "deep":
            item["scanned_depth"] = "deep"
        return item


class ContentBoundaryOnlineTest(unittest.TestCase):
    def test_same_depth_sequence_for_both_storage_models_and_entries(self):
        for bucket in ("", "fixture"):
            with self.subTest(bucket=bucket):
                api = BoundaryAPI(bucket)
                report = OWNER.verify_content_boundaries(api, 9, bucket, time.monotonic() + 10)
                self.assertEqual([case["mode"] for case in report], ["directory", "refs"])
                scans = [body for method, _, body in api.calls if method == "POST"]
                self.assertEqual([scan["scan_depth"] for scan in scans], ["basic", "basic", "deep", "basic", "basic", "deep", "basic"])
                self.assertTrue(all(scan["force"] for scan in scans))
                outside = (bucket + "/" if bucket else "") + "scan-boundary/outside/roads.dbf"
                term = "object" if bucket else "file"
                self.assertEqual(scans[0]["targets"], [f"addp://engine/9/path/{outside}?type={term}"])
                self.assertNotIn("catalog_paths", scans[0])
                self.assertNotIn("ref_groups", scans[0])
                self.assertEqual(len(scans[4]["ref_groups"][0]["refs"]), 4)
                self.assertNotIn("catalog_paths", scans[4])
                self.assertEqual([len(case["items"]) for case in report], [4, 1])

    def test_rejects_boundary_regressions_and_failed_execution(self):
        for fault in ("basic-split", "basic-enrich", "claim-unrelated", "replace-id", "move-parent", "duplicate-ref", "outside-ref", "outside-delete", "no-deep-fields", "depth-downgrade", "discard-deep", "refs-enumerate-sibling", "execution-failure"):
            with self.subTest(fault=fault), self.assertRaises(OWNER.SuiteError):
                OWNER.verify_content_boundaries(BoundaryAPI("fixture", fault), 9, "fixture", time.monotonic() + 10)
