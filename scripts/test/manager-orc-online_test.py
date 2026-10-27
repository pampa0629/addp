import hashlib
import importlib
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest

SCENE = importlib.import_module("scripts.test.manager-orc-online")
OWNER = importlib.import_module("scripts.test.manager-internal-artifact-lineage-online")


class Client:
    def __init__(self, fail=False):
        self.fail = fail
        self.tasks = []
        self.deleted = []

    def item(self, engine):
        return {
            "id": engine,
            "item_type": "object" if engine == 12 else "file",
            "full_name": "sample/orc/table.orc" if engine == 12 else "orc/table.orc",
            "fingerprint": "synthetic-" + str(engine),
            "size_bytes": 3,
            "attributes": {
                "item": {"format": "orc", "data_type": "table", "layout": "single"},
                "type_info": {
                    "table": {
                        "row_count": 2503,
                        "fields": [
                            {
                                "name": n,
                                "type": t,
                                "nullable": True,
                                **({"precision": 38, "scale": 9} if n == "measure" else {}),
                            }
                            for n, t in [
                                ("id", "bigint"),
                                ("measure", "decimal"),
                                ("flag", "bool"),
                                ("label", "string"),
                                ("blob", "bytes"),
                            ]
                        ],
                    }
                },
                "format_info": {
                    "orc": {"source_version": "version-" + str(engine), "validation": "footer"}
                },
            },
        }

    def request(self, method, path, expected, body=None):
        from urllib.parse import parse_qs, urlsplit

        if "/meta/items/" in path:
            value = self.item(int(path.rsplit("/", 1)[1]))
        elif "/manager/preview?" in path:
            q = parse_qs(urlsplit(path).query)
            page = int(q["page"][0])
            start = (page - 1) * 20
            rows = [
                {
                    "id": str(2**53 + i),
                    "measure": f"12345678901234567890.{i:09d}",
                    "flag": None if i % 7 == 0 else i % 2 == 0,
                    "label": f"ORC记录-{i:04d}",
                }
                for i in range(start, min(start + 20, 2503))
            ]
            if self.fail:
                rows[0]["id"] = "corrupted"
            value = {
                "preview_type": "table",
                "data": {"rows": rows, "total": 2503, "has_more": page != 126},
            }
        elif "/manager/downloads/" in path:
            return SimpleNamespace(raw=b"orc", payload=None)
        elif path == "/api/v1/transfer/task-definitions" and method == "POST":
            assert (
                body["config"]["source"]["format"] == "orc"
                and body["config"]["target"]["format"] == "parquet"
            )
            task = 100 + len(self.tasks)
            self.tasks.append(task)
            value = {"id": task}
        elif path.endswith("/start"):
            value = {"execution_id": "execution-" + path.split("/")[-2]}
        elif "/transfer/executions/" in path:
            value = {"status": "success", "records_read": 2503, "records_written": 2503}
        elif method == "DELETE":
            self.deleted.append(int(path.rsplit("/", 1)[1]))
            value = {}
        elif 404 in expected:
            assert int(path.rsplit("/", 1)[1]) in self.deleted
            value = {}
        else:
            raise AssertionError((method, path))
        return SimpleNamespace(payload=value)


class ORCSceneTest(unittest.TestCase):
    def exercise(self, fail=False):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        root = Path(temp.name)
        client = Client(fail)
        actions = []
        env = {
            "ADDP_ONLINE_MANAGER_CASE": "native-table-orc",
            "ADDP_ONLINE_ARTIFACT_DIR": str(root),
            "ADDP_ONLINE_TEST_RUN_ID": "manager-unit",
            "GATEWAY_URL": "http://gateway.invalid",
            "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "unit-not-a-token",
            "ADDP_ONLINE_TEST_TENANT_ID": "2",
            "ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID": "12",
            "ADDP_ONLINE_MANAGER_NFS_ENGINE_ID": "14",
            "ADDP_ONLINE_MANAGER_MINIO_BUCKET": "sample",
        }

        def identity(c, t, p):
            self.assertEqual(t, 2)
            self.assertEqual(len(p), 11)
            self.assertNotIn("manager.derived_artifact.create", p)
            return {"principal_id": "33", "tenant_id": "2", "permissions_verified": sorted(p)}

        helpers = SimpleNamespace(
            GatewayClient=lambda *args: client,
            positive_int=OWNER.positive_int,
            validate_user_identity=identity,
            wait_for_meta_scan=lambda *a, **k: "scan-id",
            find_fixture_item=lambda c, e, *args: client.item(e),
            build_item_locator=lambda e, i: f"addp://engine/{e}/path/"
            + i["full_name"]
            + "?type="
            + i["item_type"],
        )

        def physical(action):
            actions.append(action)
            if action == "seed":
                return {
                    "count": 2503,
                    "pyorc": "0.12.0",
                    "sha256": hashlib.sha256(b"orc").hexdigest(),
                    "size_bytes": 3,
                }
            if action == "verify":
                return {
                    "source_sha256": hashlib.sha256(b"orc").hexdigest(),
                    "outputs": [{"all_values_and_nulls_verified": True}] * 2,
                }
            return {"remaining_owned_outputs": 0, "output_objects_removed": True}

        browser = lambda context: {
            "result": "passed",
            "run_id": context["run_id"],
            "cases": [
                {
                    "page_button_verified": True,
                    "original_button_download_verified": True,
                    "sha256": hashlib.sha256(b"orc").hexdigest(),
                    "size_bytes": 3,
                }
            ]
            * 2,
        }
        return (
            root,
            client,
            actions,
            lambda: SCENE.run(
                Path(__file__).resolve().parents[2],
                env,
                helpers,
                physical_runner=physical,
                browser_runner=browser,
            ),
        )

    def test_two_sources_real_task_contract_and_cleanup(self):
        root, client, actions, run = self.exercise()
        report = run()
        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["cleanup"]["result"], "passed")
        self.assertEqual(client.deleted, list(reversed(client.tasks)))
        self.assertEqual(actions, ["seed", "verify", "delete"])
        self.assertEqual([c["source_case"] for c in report["cases"]], ["minio", "nfs"])
        self.assertTrue((root / "manager-orc.json").is_file())

    def test_value_failure_never_passes_but_still_cleans(self):
        root, client, actions, run = self.exercise(True)
        with self.assertRaisesRegex(ValueError, "exact values"):
            run()
        report = json.loads((root / "manager-orc.json").read_text())
        self.assertEqual(report["result"], "failed")
        self.assertEqual(actions, ["seed", "delete"])

    def test_dispatch_case_and_fixture_guards(self):
        self.assertEqual(
            OWNER.manager_case({"ADDP_ONLINE_MANAGER_CASE": "native-table-orc"}), "native-table-orc"
        )
        fixture = importlib.import_module("business.scripts.online-orc-fixture")
        for action in ["seed", "verify", "delete"]:
            with self.assertRaises(ValueError):
                fixture.physical(action, Path("/tmp"), {})


if __name__ == "__main__":
    unittest.main()
