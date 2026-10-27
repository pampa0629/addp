import base64
import hashlib
import importlib
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

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
    def exercise(self, fail=False, seed_fail=False, cleanup_fail=False):
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
            build_item_locator=OWNER.build_item_locator,
        )

        def physical(action):
            actions.append(action)
            if action == "seed":
                if seed_fail:
                    raise ValueError("ORC seed unavailable")
                return {
                    "count": 2503,
                    "pyorc": "0.12.0",
                    "sha256": hashlib.sha256(b"orc").hexdigest(),
                    "size_bytes": 3,
                }
            if action == "verify":
                return {
                    "source_sha256": hashlib.sha256(b"orc").hexdigest(),
                    "outputs": [
                        {
                            "object": "orc-output/from-" + name + ".parquet",
                            "rows": 2503,
                            "sha256": hashlib.sha256(b"parquet-fixture").hexdigest(),
                            "size_bytes": len(b"parquet-fixture"),
                            "content_base64": base64.b64encode(b"parquet-fixture").decode(),
                            "all_values_and_nulls_verified": True,
                        }
                        for name in ("minio", "nfs")
                    ],
                }
            if cleanup_fail:
                raise ValueError("ORC cleanup unavailable")
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
        self.assertEqual(report['cases'][0]['locator'],
                         'addp://engine/12/path/sample/orc/table.orc?type=object&item_id=12')
        self.assertEqual(report['cases'][1]['locator'],
                         'addp://engine/14/path/orc/table.orc?type=file&item_id=14')
        self.assertTrue((root / "manager-orc.json").is_file())

    def test_value_failure_never_passes_but_still_cleans(self):
        root, client, actions, run = self.exercise(True)
        with self.assertRaisesRegex(ValueError, "exact values"):
            run()
        report = json.loads((root / "manager-orc.json").read_text())
        self.assertEqual(report["result"], "failed")
        self.assertEqual(actions, ["seed", "delete"])

    def test_cleanup_failure_retains_primary_failure_and_both_reported_stages(self):
        root, client, actions, run = self.exercise(seed_fail=True, cleanup_fail=True)
        with self.assertRaisesRegex(ValueError, "ORC seed unavailable"):
            run()
        report = json.loads((root / "manager-orc.json").read_text())
        self.assertEqual(report["failure_stage"], "physical_seed")
        self.assertEqual(report["cleanup"]["result"], "failed")
        self.assertEqual(report["cleanup"]["errors"], ["owned_output_delete_failed"])
        self.assertEqual(actions, ["seed", "delete"])

    def test_physical_container_delivers_json_to_worker_stdin(self):
        fixture = importlib.import_module("business.scripts.online-orc-fixture")
        root = Path('/tmp/addp-online-manager-nfs.' + secrets.token_hex(3))
        root.mkdir(mode=0o700)
        self.addCleanup(shutil.rmtree, root)
        (root / '.addp-online-owner').write_text('manager-123-1')
        with tempfile.TemporaryDirectory() as directory:
            secret = Path(directory)
            (secret / 'nfs-root').write_text(str(root))
            env = {
                'ADDP_ONLINE_HOSTED':'1', 'GITHUB_ACTIONS':'true', 'RUNNER_OS':'Linux',
                'ADDP_ONLINE_MANAGER_CASE':'native-table-orc',
                'ADDP_ONLINE_SECRET_DIR':str(secret), 'ADDP_ONLINE_TEST_RUN_ID':'manager-123-1',
                'ADDP_ONLINE_MANAGER_MINIO_PORT':'59002',
                'ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY':'unit-access',
                'ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY':'unit-not-a-secret',
                'ADDP_ONLINE_MANAGER_MINIO_BUCKET':'unit-bucket',
                'ADDP_ONLINE_RASTER_RUNTIME_IMAGE':'unit-image',
            }
            real_run = subprocess.run
            delivered = []
            def launch(command, **kwargs):
                self.assertIn('--user', command, 'fixture files must belong to the host lifecycle owner')
                self.assertEqual(command[command.index('--user')+1], f'{os.getuid()}:{os.getgid()}')
                # Docker closes container STDIN unless interactive mode is set.
                worker_input = kwargs['input'] if '-i' in command or '--interactive' in command else ''
                result = real_run(
                    [sys.executable, '-c', 'import json,sys; print(json.dumps(json.load(sys.stdin)))'],
                    **dict(kwargs, input=worker_input),
                )
                if result.returncode == 0:
                    delivered.append(json.loads(result.stdout)['action'])
                return result
            with patch.object(fixture.subprocess, 'run', side_effect=launch):
                for action in ['seed', 'verify', 'delete']:
                    self.assertEqual(fixture.physical(action, Path('/tmp'), env)['action'], action)
            self.assertEqual(delivered, ['seed', 'verify', 'delete'])
            failure = SimpleNamespace(returncode=1, stdout='{"error_code":"output_columns_or_count"}',
                                      stderr='private credentials must not enter diagnostics')
            with patch.object(fixture.subprocess, 'run', return_value=failure):
                with self.assertRaisesRegex(ValueError, 'output_columns_or_count') as raised:
                    fixture.physical('verify', Path('/tmp'), env)
                self.assertNotIn('private credentials', str(raised.exception))

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
