import copy
import importlib.util
import csv
import io
import shutil
import sys
import tempfile
import time
import unittest
import urllib.parse
from pathlib import Path
from types import SimpleNamespace
from unittest import mock
from uuid import uuid4

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
        self.next_item_id = 1
        self.retired_reads = set()

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, copy.deepcopy(body)))
        if method == "POST":
            self.executions += 1
            depth = body["scan_depth"]
            grouped = body.get("ref_groups")
            if body.get("targets"):
                for target in body["targets"]:
                    locator = urllib.parse.urlsplit(target)
                    if locator.scheme != "addp" or locator.netloc != "engine" or locator.path.split("/", 3)[:3] != ["", "9", "path"]:
                        raise OWNER.SuiteError("precise target must use an exact Engine locator")
                    if urllib.parse.parse_qs(locator.query) != {"type": ["object" if self.bucket else "file"]}:
                        raise OWNER.SuiteError("precise target must retain its native leaf type")
                    self.make_item(urllib.parse.unquote(locator.path.split("/", 3)[3]), depth)
                return SimpleNamespace(payload={"execution_id": f"boundary-{self.executions}"})
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
                        for extension in extensions:
                            if extension != '.shp' and self.fault != "retain-component":
                                self.items.pop(scope + "/" + name[:-4] + extension, None)
                    if depth == "deep":
                        item["attributes"].update(type_info={"table": {"fields": [{"name": "NAME"}]}}, capabilities={"spatial": {"crs": "EPSG:4326"}})
                item = self.items[scope + "/roads.shp"]
                if scope.endswith("-convergence") and iteration == 0:
                    if self.fault == "replace-seeded-primary": item["id"] += 1000
                    if self.fault == "replace-seeded-fingerprint": item["fingerprint"] += ":changed"
                    if self.fault == "move-seeded-parent": item["node_id"] += 1
                    if self.fault == "delete-unclaimed-single": self.items.pop(scope + "/roads.qmd", None)
                    if grouped and self.fault == "delete-unclaimed-component": self.items.pop(scope + "/parcels.dbf", None)
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
        item = next((item for item in self.items.values() if item["id"] == item_id), None)
        if expected == (404,):
            if item is not None or self.fault == "retired-detail-readable":
                raise OWNER.SuiteError("retired component detail is still readable")
            self.retired_reads.add(item_id)
            return SimpleNamespace(payload={"error": "not_found"})
        if item is None:
            raise OWNER.SuiteError("item detail unexpectedly missing")
        return SimpleNamespace(payload=copy.deepcopy(item))

    def make_item(self, name, depth):
        if name not in self.items:
            self.items[name] = {"id": self.next_item_id, "full_name": name, "fingerprint": "fingerprint:" + name,
                "node_id": 7, "item_type": "object" if self.bucket else "file", "scanned_depth": "basic",
                "attributes": {"item": {"layout": "single", "format": "unknown", "data_type": "unknown"}}}
            self.next_item_id += 1
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
                self.assertEqual([case["mode"] for case in report], ["directory", "refs", "directory", "refs"])
                self.assertEqual([case["initial_state"] for case in report], ["empty", "empty", "independent_components", "independent_components"])
                scans = [body for method, _, body in api.calls if method == "POST"]
                self.assertEqual([scan["scan_depth"] for scan in scans], ["basic", "basic", "deep", "basic", "basic", "deep", "basic", "basic", "basic", "deep", "basic", "basic", "basic", "deep", "basic"])
                self.assertTrue(all(scan["force"] for scan in scans[:8]))
                self.assertEqual([scan["force"] for scan in scans[8:]], [False, False, False, True, False, False, False])
                self.assertEqual(len(scans[7]["targets"]), 10)
                self.assertEqual(len(scans[11]["targets"]), 10)
                outside = (bucket + "/" if bucket else "") + "scan-boundary/outside/roads.dbf"
                term = "object" if bucket else "file"
                self.assertEqual(scans[0]["targets"], [f"addp://engine/9/path/{outside}?type={term}"])
                self.assertNotIn("catalog_paths", scans[0])
                self.assertNotIn("ref_groups", scans[0])
                self.assertEqual(len(scans[4]["ref_groups"][0]["refs"]), 4)
                self.assertNotIn("catalog_paths", scans[4])
                self.assertEqual([len(case["items"]) for case in report], [4, 1, 4, 1])
                self.assertEqual([len(case["removed_components"]) for case in report], [0, 0, 6, 3])
                self.assertEqual([len(case["preserved_initial_items"]) for case in report], [0, 0, 2, 6])
                retired_ids = {item["id"] for case in report for item in case["removed_components"]}
                self.assertEqual(api.retired_reads, retired_ids)

    def test_rejects_boundary_regressions_and_failed_execution(self):
        for fault in ("basic-split", "basic-enrich", "claim-unrelated", "replace-id", "move-parent", "duplicate-ref", "outside-ref", "outside-delete", "no-deep-fields", "depth-downgrade", "discard-deep", "refs-enumerate-sibling", "execution-failure"):
            with self.subTest(fault=fault), self.assertRaises(OWNER.SuiteError):
                OWNER.verify_content_boundaries(BoundaryAPI("fixture", fault), 9, "fixture", time.monotonic() + 10)

    def test_rejects_incomplete_convergence_and_collateral_cleanup(self):
        for bucket in ("", "fixture"):
            for fault in ("retain-component", "replace-seeded-primary", "replace-seeded-fingerprint", "move-seeded-parent", "delete-unclaimed-single", "delete-unclaimed-component", "retired-detail-readable"):
                with self.subTest(bucket=bucket, fault=fault), self.assertRaises(OWNER.SuiteError):
                    OWNER.verify_content_boundaries(BoundaryAPI(bucket, fault), 9, bucket, time.monotonic() + 10)


class IndexSourceAPI:
    def __init__(self, root, fault=""):
        self.root, self.fault = root, fault
        self.items, self.calls = {}, []
        self.refreshes = 0

    def snapshot(self, item_id):
        path = self.root / f"index-source-versions/source.{('csv', 'tsv')[item_id - 1]}"
        return {"id": item_id, "fingerprint": f"item-{item_id}", "full_name": str(path.relative_to(self.root)),
                "attributes": {"access_index": {"table": {"kind": "sparse_row_index", "source": {
                    "size_bytes": path.stat().st_size, "last_modified_at": str(path.stat().st_mtime_ns)}}}}}

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, body))
        status, raw = 200, b""
        if path == "/api/v1/meta/scan/run/manual":
            suffix = body["targets"][0].split("?")[0].rsplit(".", 1)[1]
            item_id = ("csv", "tsv").index(suffix) + 1
            self.items[item_id] = self.snapshot(item_id)
            status, payload = 201, {"execution_id": "scan-1"}
        elif "/meta/executions/" in path:
            payload = {"status": "success"}
        elif path == "/api/v1/meta/engines/9/items":
            payload = list(self.items.values())
        elif "/meta/items/" in path:
            payload = self.items[int(path.rsplit("/", 1)[1])]
        elif path.endswith("/items/refresh"):
            self.refreshes += 1
            item_id = body["item_id"]
            if not (self.fault == "stale-refresh" and self.refreshes == 1):
                self.items[item_id] = self.snapshot(item_id)
                if self.fault == "replace-identity" and self.refreshes == 1:
                    self.items[item_id]["fingerprint"] = "replacement-id"
            payload = {"status": "success"}
        elif "/manager/preview?" in path or "/manager/storage-stream?" in path:
            query = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)
            locator = urllib.parse.urlsplit(query["locator"][0])
            item_id = int(urllib.parse.parse_qs(locator.query)["item_id"][0])
            source = self.root / self.items[item_id]["full_name"]
            raw = source.read_bytes()
            if "/storage-stream?" in path:
                payload = {}
            elif self.items[item_id]["attributes"] != self.snapshot(item_id)["attributes"] and self.fault != "allow-stale":
                status, payload = 409, {"error_code": "preview_source_refresh_required"}
                if self.fault == "leak-rows":
                    payload["rows"] = []
                if self.fault == "wrong-code":
                    payload["error_code"] = "unknown"
                if self.fault == "auto-refresh":
                    self.items[item_id] = self.snapshot(item_id)
            else:
                reader = csv.DictReader(io.StringIO(raw.decode()), delimiter="\t" if source.suffix == ".tsv" else ",")
                rows = [{**row, "id": int(row["id"])} for row in reader]
                page = int(query["page"][0])
                table = {"mode": "table", "columns": reader.fieldnames, "rows": rows[page-1:page], "total": 2, "has_more": page == 1}
                payload = {"preview_type": "table", "data": table}
                if self.fault == "bare-table":
                    payload = table
                if self.fault == "wrong-preview-type":
                    payload["preview_type"] = "object"
        else:
            raise AssertionError(f"unexpected {method} {path}")
        if status not in expected:
            raise OWNER.SuiteError(f"unexpected HTTP status {status}")
        return SimpleNamespace(status=status, payload=copy.deepcopy(payload), raw=raw)


class IndexSourceVersionsOnlineTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path("/tmp") / ("addp-online-manager-nfs." + uuid4().hex[:6])
        self.root.mkdir()
        self.addCleanup(lambda: shutil.rmtree(self.root))
        (self.root / ".addp-online-owner").write_text("manager-42-1\n")
        (self.root / "index-source-versions").mkdir()
        self.originals = {}
        for suffix, delimiter in (("csv", ","), ("tsv", "\t")):
            content = "id,name\n1,Alice\n2,Bobby\n".replace(",", delimiter).encode()
            path = self.root / "index-source-versions" / ("source." + suffix)
            path.write_bytes(content)
            self.originals[path] = content
        (Path(self.temp.name) / "nfs-root").write_text(str(self.root) + "\n")
        self.env = {"ADDP_ONLINE_HOST": "1", "ADDP_ONLINE_HOSTED": "1", "GITHUB_ACTIONS": "true", "RUNNER_OS": "Linux",
                    "ADDP_ONLINE_SECRET_DIR": self.temp.name, "ADDP_ONLINE_TEST_RUN_ID": "manager-42-1",
                    "ADDP_ONLINE_MANAGER_NFS_ENGINE_ID": "9"}
        self.enterContext(mock.patch.object(OWNER.sys, "platform", "linux"))
        self.enterContext(mock.patch.object(OWNER.os, "uname", return_value=SimpleNamespace(machine="x86_64")))

    def invoke(self, api):
        return OWNER.verify_nfs_index_source_versions(api, self.env, time.monotonic() + 30)

    def test_same_size_change_rejects_both_pages_refreshes_and_restores(self):
        api = IndexSourceAPI(self.root)
        result = self.invoke(api)
        self.assertEqual(result["schema_version"], "addp.index-source-versions/v1")
        self.assertEqual(result["result"], "passed")
        self.assertEqual([case["format"] for case in result["cases"]], ["csv", "tsv"])
        self.assertEqual(api.refreshes, 4)
        for case in result["cases"]:
            self.assertEqual(case["stale_pages_rejected"], [1, 2])
            self.assertNotEqual(case["original_sha256"], case["changed_sha256"])
            self.assertTrue(case["source_and_preview_restored"])
        for path, original in self.originals.items():
            self.assertEqual(path.read_bytes(), original)

    def test_rejects_regressions_and_restores_physical_bytes_on_failure(self):
        for fault in ("allow-stale", "leak-rows", "wrong-code", "auto-refresh", "stale-refresh", "replace-identity", "bare-table", "wrong-preview-type"):
            with self.subTest(fault=fault), self.assertRaises(OWNER.SuiteError):
                self.invoke(IndexSourceAPI(self.root, fault))
            for path, original in self.originals.items():
                self.assertEqual(path.read_bytes(), original)

    def test_rejects_unowned_hosts_and_fixture_paths_before_api_calls(self):
        api = IndexSourceAPI(self.root)
        for key, value in (("GITHUB_ACTIONS", "false"), ("ADDP_ONLINE_HOST", "0"), ("ADDP_ONLINE_HOSTED", "0"), ("RUNNER_OS", "macOS"),
                           ("ADDP_ONLINE_TEST_RUN_ID", "another-run")):
            with self.subTest(key=key), mock.patch.dict(self.env, {key: value}), self.assertRaises(OWNER.SuiteError):
                self.invoke(api)
        source = next(iter(self.originals))
        source.unlink()
        source.symlink_to(self.root / ".addp-online-owner")
        with self.assertRaises(OWNER.SuiteError):
            self.invoke(api)
        self.assertFalse(api.calls)
