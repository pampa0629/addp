"""Reject incomplete or fabricated evidence before launching the Hosted case."""
import copy
import json
import os
from importlib import import_module
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import secrets
import unittest
from types import SimpleNamespace
from urllib.parse import parse_qs, urlsplit
from unittest.mock import patch

scenario = import_module("scripts.test.infra-object-storage-online")
ROOT = Path(__file__).resolve().parents[2]
EXECUTION = "11111111-1111-4111-8111-111111111111"


def session(owner, identity):
    return {"id": identity, "tenant_id": 7, "user_id": 9, "status": "success",
            "transfer_execution_id": EXECUTION, "target_locator": f"addp-infra://object/manager/tenant_7/export/{owner}/x/file.csv?type=object",
            "target_parent_locator": f"addp-infra://object/manager/tenant_7/export/{owner}/x?type=prefix",
            "execution_request_digest": "a" * 64, "created_at": "2026-11-14T10:00:00Z",
            "updated_at": "2026-11-14T10:01:00Z"}


class ObserverTest(unittest.TestCase):
    def test_imported_source_uses_the_persisted_meta_item_identity(self):
        item = {"id": 17, "full_name": "public." + scenario.TABLE, "item_type": "table"}
        client = unittest.mock.Mock()
        client.request.return_value = SimpleNamespace(payload=[item])
        locator, item_id = scenario.read_source_item_locator(client, 3)
        self.assertEqual(item_id, 17)
        self.assertEqual(parse_qs(urlsplit(locator).query), {"type": ["table"], "item_id": ["17"]})
        client.request.assert_called_once_with("GET", "/api/v1/meta/engines/3/items?branch=public", (200,), response_type=list)
        for items in ([], [item, item]):
            client.request.return_value = SimpleNamespace(payload=items)
            with self.assertRaises(scenario.api.SuiteError):
                scenario.read_source_item_locator(client, 3)
        client.request.return_value = SimpleNamespace(payload=[{key: value for key, value in item.items() if key != "id"}])
        with self.assertRaises(scenario.etl.SUPPORT.SuiteError):
            scenario.read_source_item_locator(client, 3)

    def test_initial_authorization_waits_only_for_pending_and_binds_execution(self):
        current = {"execution_id": EXECUTION, "task_id": 3, "status": "success",
                   "metadata": {"initial_authorization": {"status": "pending"}}}
        issued = copy.deepcopy(current)
        issued["metadata"]["initial_authorization"] = {"status": "issued"}
        client = unittest.mock.Mock()
        client.request.side_effect = [SimpleNamespace(payload=current), SimpleNamespace(payload=issued)]
        report = {}
        with patch.object(scenario.time, "sleep"):
            scenario.wait_initial_authorization(client, EXECUTION, 3, report)
        self.assertEqual(report["initial_authorization"], {"status": "issued"})
        self.assertIn("authorization_observed_at", report)
        for invalid in (current | {"metadata": {}}, current | {"task_id": 4},
                        current | {"metadata": {"initial_authorization": {"status": "denied"}}}):
            client.request.side_effect = None
            client.request.return_value = SimpleNamespace(payload=invalid)
            with self.assertRaises(scenario.api.SuiteError):
                scenario.wait_initial_authorization(client, EXECUTION, 3, {})
        client.request.return_value = SimpleNamespace(payload=current)
        with patch.object(scenario.time, "monotonic", side_effect=[0, 181]):
            with self.assertRaises(scenario.api.SuiteError):
                scenario.wait_initial_authorization(client, EXECUTION, 3, {})

    def test_two_session_age_is_atomic_and_scoped(self):
        rows = {"manager": session("manager", 11), "develop": session("develop", 12)}
        sql = scenario.age_sql(rows)
        self.assertTrue(sql.startswith("BEGIN;"))
        self.assertTrue(sql.endswith("COMMIT;"))
        self.assertEqual(sql.count("GET DIAGNOSTICS affected=ROW_COUNT"), 2)
        self.assertEqual(sql.count("IF affected<>1"), 2)
        for owner, row in rows.items():
            self.assertIn(f"UPDATE {owner}.export_sessions SET created_at=", sql)
            for condition in (f'id={row["id"]}', "tenant_id=7", "user_id=9", "status='success'",
                              "transfer_execution_id='" + EXECUTION + "'", "target_locator='", "target_parent_locator='",
                              "execution_request_digest='", "created_at='", "updated_at='"):
                self.assertIn(condition, sql)
        self.assertNotIn("DELETE", sql)
        self.assertNotIn("task_executions", sql)
        with self.assertRaises(scenario.api.SuiteError):
            scenario.age_sql({"manager": rows["manager"]})
        rows["develop"]["status"] = "running"
        with self.assertRaises(scenario.api.SuiteError):
            scenario.age_sql(rows)

    def test_timestamp_fixture_rejects_changed_non_time_facts(self):
        before = {"manager": session("manager", 11), "develop": session("develop", 12)}
        after = copy.deepcopy(before)
        after["manager"]["target_locator"] += "-wrong"
        exports = {owner: {"session_id": row["id"], "execution_id": EXECUTION, "request_digest": row["execution_request_digest"],
                           "target_locator": row["target_locator"], "target_parent_locator": row["target_parent_locator"]}
                   for owner, row in before.items()}
        with patch.object(scenario, "session_row", side_effect=[before["manager"], before["develop"], after["manager"]]), \
             patch.object(scenario, "pg") as database:
            with self.assertRaisesRegex(scenario.api.SuiteError, "non-time"):
                scenario.timestamp_fixture(exports, 7, "9")
            self.assertEqual(database.call_count, 1)
            self.assertEqual(database.call_args.kwargs, {"write": True})

    def test_csv_checks_actual_values_and_row_count(self):
        self.assertEqual(scenario.csv_proof(b"id,label\n1,alpha\n2,beta\n")["rows"], 2)
        for data in (b"id,label\n1,changed\n2,beta\n", b"id,label\n1,alpha\n", b"id,label\n1,alpha\n2,beta\n3,beta\n"):
            with self.subTest(data=data), self.assertRaises(scenario.api.SuiteError):
                scenario.csv_proof(data)

    def test_csv_grant_uses_the_ordinary_user_read_observation(self):
        registration = scenario.etl.REGISTRATION
        target = registration.ExactRecordGrants.target(3, "public", scenario.TABLE, "table", "schema")
        facts = {"item_fingerprint": "a" * 64, "boundary_hash": "b" * 64}
        authorizer, consumer = (unittest.mock.Mock() for _ in range(2))

        def authorizer_request(method, route, codes, body):
            if route.endswith("/revoke"):
                self.assertEqual((method, codes), ("POST", (200,)))
                return SimpleNamespace(payload={})
            self.assertEqual((method, route, codes), ("POST", "/api/v1/system/engines/3/access_grants", (201,)))
            return SimpleNamespace(payload=body | {"engine_id": "3", "boundary_hash": facts["boundary_hash"],
                                                   "approval_mode": "independent", "operator_principal_id": "8"})

        item = {"engine_id": 3, "id": 17, "full_name": "public." + scenario.TABLE,
                "item_type": "table", "fingerprint": facts["item_fingerprint"]}
        authorizer.request.side_effect = authorizer_request
        consumer.request.side_effect = [SimpleNamespace(payload=value) for value in
                                        ({}, {}, {"observed_at": "now"}, item, {}, {})]
        report = {}
        with patch.object(scenario, "csv_source_rows", return_value=[[1, "alpha"], [2, "beta"]]), \
             patch.object(scenario.etl, "wait_for_scan", return_value=EXECUTION), \
             patch.object(registration, "source_grant_participants", return_value=("8", "9")), \
             patch.object(registration, "read_exact_record_authorization_facts", return_value=facts):
            with scenario.prepare_csv_source(authorizer, consumer, 3, 7, report) as locator:
                self.assertEqual(parse_qs(urlsplit(locator).query)["item_id"], ["17"])
        item_route = f"/api/v1/meta/items/by-catalog-path?engine_id=3&catalog_path=public.{scenario.TABLE}"
        source_route = "/api/v1/system/engine-access/read-observations/current-source"
        self.assertEqual([call.args for call in consumer.request.call_args_list], [
            ("GET", item_route, (404,)), ("POST", source_route, (403,), {"targets": [target]}),
            ("POST", source_route, (200,), {"targets": [target]}), ("GET", item_route, (200,)),
            ("POST", source_route, (403,), {"targets": [target]}), ("GET", item_route, (404,)),
        ])
        self.assertEqual(report["grant_cleanup"], {"grant_revoked": True, "content_read_http": 403, "meta_item_http": 404})

    def test_csv_rejects_duplicate_export_without_submitting_import(self):
        def export_once(_, module, locator, engine, tenant, principal, evidence):
            evidence.update(path=f"/{module}/exports/1", execution_id=EXECUTION)

        owner, outsider, foreign = (unittest.mock.Mock() for _ in range(3))
        with patch.object(scenario, "export_once", side_effect=export_once) as export, \
             patch.object(scenario, "pg", return_value=[["develop", EXECUTION], ["manager", EXECUTION], ["manager", EXECUTION]]), \
             patch.object(scenario.etl, "verify_unreadable_source_execution") as reject_import:
            with self.assertRaisesRegex(scenario.api.SuiteError, "exactly one Transfer export execution"):
                scenario.verify_exports(owner, outsider, foreign, "source", 5, 7, "9", 8, {}, None)
        self.assertEqual([call.args[1] for call in export.call_args_list], ["manager", "develop"])
        reject_import.assert_not_called()
        owner.request.assert_not_called()

    def test_reference_is_engine_independent_and_kind_checked(self):
        self.assertEqual(scenario.key_from_ref("addp-infra://object/manager/tenant_7/export/a%20b.csv?type=object", "object"),
                         "tenant_7/export/a b.csv")
        for locator in ("addp-infra://minio/manager/a?type=object", "addp-infra://object/standard/a?type=object",
                        "addp-infra://object/manager/a?type=prefix", "addp-infra://object/manager/../a?type=object"):
            with self.subTest(locator=locator), self.assertRaises(scenario.api.SuiteError):
                scenario.key_from_ref(locator, "object")

    def test_terminal_proof_rejects_missing_or_wrong_attempt_started_event(self):
        row = {"execution_id": EXECUTION, "module": "transfer", "tenant_id": 7, "triggered_by": 9, "source": "manager",
               "status": "success", "attempt": 1, "started_at": "now", "completed_at": "later",
               "metadata": {}, "events": [{"kind": "started", "attempt": 1}, {"kind": "completed", "attempt": 1}]}
        with patch.object(scenario, "pg", return_value=copy.deepcopy(row)):
            scenario.execution_proof(EXECUTION, 7, "9", source="manager")
        for events in ([{"kind": "completed", "attempt": 1}],
                       [{"kind": "started", "attempt": 2}, {"kind": "completed", "attempt": 1}]):
            with patch.object(scenario, "pg", return_value=row | {"events": events}), self.assertRaises(scenario.api.SuiteError):
                scenario.execution_proof(EXECUTION, 7, "9", source="manager")

    def test_iam_patch_applies_only_to_the_pinned_fixture_command(self):
        with tempfile.TemporaryDirectory(prefix="addp-object-identity-test-") as directory:
            subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), directory],
                           capture_output=True, check=True)
            subprocess.run(["git", "checkout", "--detach", "a5a2c7d90"], cwd=directory,
                           capture_output=True, check=True)
            target = Path(directory) / "system/backend/cmd/online-test-fixture/main.go"
            subprocess.run(["git", "apply", "--unidiff-zero", str(ROOT / "scripts/test/infra-object-identity.patch")],
                           cwd=directory, capture_output=True, check=True)
            self.assertEqual(subprocess.run(["gofmt", "-l", str(target)], capture_output=True, check=True).stdout, b"")
            text = target.read_text()
            self.assertIn('case "infra-object-storage":', text)
            self.assertIn('"external-online-object-outsider"', text)
            self.assertIn('"external-online-object-foreign"', text)
            self.assertIn('config.AccessTokenTTL = time.Hour', text)
            changed = subprocess.check_output(["git", "diff", "--name-only"], cwd=directory, text=True).splitlines()
            self.assertEqual(changed, ["system/backend/cmd/online-test-fixture/main.go"])
            result = subprocess.run(["go", "test", "./cmd/online-test-fixture", "-run", "^$"],
                                    cwd=Path(directory) / "system/backend", env=dict(os.environ, GOWORK="off"),
                                    capture_output=True, text=True, timeout=300)
            self.assertEqual(result.returncode, 0, result.stderr)

    def test_host_uses_clean_product_and_original_cleanup_interval(self):
        host = (ROOT / "scripts/test/online-hosted-infra-object-gate.sh").read_text()
        self.assertIn('/infra-object-product"', host)
        self.assertIn('/infra-object-identity"', host)
        self.assertIn('source "$ROOT_DIR/scripts/utils/hosted-online.sh"', host)
        self.assertIn('export ADDP_INFRA_OBJECTSTORE_BINDINGS=', host)
        self.assertNotIn("keepalive.sh", host)
        self.assertNotIn("restart.sh", host)
        self.assertNotIn("RETENTION=", host)
        self.assertIn("test-infra-object-barrier-postgres", host)
        steps = ["modules=(-security -meta -transfer -manager -develop)",
                 'hosted_prepare_selected_native_database "${modules[@]}"',
                 'for target in "${modules[@]}"; do',
                 'bash scripts/dev/start.sh "$target"']
        self.assertEqual([host.index(step) for step in steps], sorted(host.index(step) for step in steps))

    def test_csv_identity_patch_compiles_against_the_fixed_product(self):
        with tempfile.TemporaryDirectory(prefix="addp-csv-export-identity-test-") as directory:
            subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), directory],
                           capture_output=True, check=True)
            subprocess.run(["git", "checkout", "--detach", "5db515160aa9e027fc187eaac43465677f70366f"],
                           cwd=directory, capture_output=True, check=True)
            patch_file = ROOT / "scripts/test/infra-object-csv-export-identity.patch"
            subprocess.run(["git", "apply", "--ignore-space-change", str(patch_file)],
                           cwd=directory, capture_output=True, check=True)
            path = "system/backend/cmd/online-test-fixture/main.go"
            changed = subprocess.check_output(["git", "diff", "--name-only"], cwd=directory, text=True).splitlines()
            self.assertEqual(changed, [path])
            self.assertEqual(subprocess.check_output(["gofmt", "-l", str(Path(directory) / path)]), b"")
            result = subprocess.run(["go", "test", "./cmd/online-test-fixture", "-run", "^$"],
                                    cwd=Path(directory) / "system/backend", env=dict(os.environ, GOWORK="off"),
                                    capture_output=True, text=True, timeout=300)
            self.assertEqual(result.returncode, 0, result.stderr)

    def test_source_fixture_refuses_non_hosted_execution_before_docker(self):
        result = subprocess.run(["bash", str(ROOT / "scripts/test/infra-object-source-fixture.sh"), "start"],
                                env={"PATH": os.environ["PATH"], "GITHUB_ACTIONS": "false"},
                                capture_output=True, text=True, timeout=5)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("isolated Hosted object suite required", result.stderr)

    def test_generated_source_descriptor_passes_the_standard_registration_contract(self):
        with tempfile.TemporaryDirectory(prefix="addp-object-descriptor-") as directory:
            root = Path(directory)
            docker = root / "docker"
            docker.write_text(f"#!{sys.executable}\n" + '''import os, sys
args = sys.argv[1:]
if args[:2] == ['container', 'inspect']:
    sys.exit(1)
if args[0] == 'run':
    print('disposable-fixture')
elif args[0] == 'exec' and args[-1] == 'SELECT current_database()':
    print('object_fixture')
elif args[:2] == ['exec', '-i']:
    with open(os.environ['CAPTURE_SQL'], 'a') as output:
        output.write(sys.stdin.read())
else:
    sys.exit(99)
''')
            docker.chmod(0o700)
            descriptor = root / "engine.json"
            sql = root / "fixture.sql"
            environment = dict(os.environ, PATH=str(root) + os.pathsep + os.environ["PATH"], CAPTURE_SQL=str(sql),
                               GITHUB_ACTIONS="true", RUNNER_OS="Linux", ADDP_ONLINE_HOSTED="1",
                               ADDP_ONLINE_HOST="1", ONLINE_SUITE="infra-object-storage", POSTGRES_DB="addp_online",
                               POSTGRES_PASSWORD="fixture-only", ADDP_ONLINE_SECRET_DIR=str(root),
                               ADDP_ONLINE_FIXTURE_ENGINE_DESCRIPTOR_FILE=str(descriptor))
            result = subprocess.run(["bash", str(ROOT / "scripts/test/infra-object-source-fixture.sh"), "start"],
                                    env=environment, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            record = scenario.etl.REGISTRATION.load_descriptor(descriptor)
            self.assertNotIn("CREATE TABLE", sql.read_text())
            self.assertEqual(record["engine_type"], "postgresql")
            self.assertTrue(record["description"])
            self.assertEqual(descriptor.stat().st_mode & 0o777, 0o600)
            record.pop("description")
            descriptor.write_text(json.dumps(record))
            with self.assertRaises(scenario.etl.REGISTRATION.RegistrationError):
                scenario.etl.REGISTRATION.load_descriptor(descriptor)
            descriptor.unlink()
            sql.unlink()
            environment["ONLINE_SUITE"] = "infra-object-csv-export"
            result = subprocess.run(["bash", str(ROOT / "scripts/test/infra-object-source-fixture.sh"), "start"],
                                    env=environment, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            sql_text = sql.read_text()
            self.assertIn("INSERT INTO public.object_import_points VALUES (1, 'alpha'), (2, 'beta')", sql_text)
            self.assertIn("GRANT SELECT ON public.object_import_points TO object_operator", sql_text)
            self.assertNotIn("USAGE, CREATE", sql_text)

    def test_original_product_cannot_be_replaced_by_fixture_patch(self):
        patch_text = (ROOT / "scripts/test/infra-object-identity.patch").read_text()
        changed = [line[6:] for line in patch_text.splitlines() if line.startswith("+++ b/")]
        self.assertEqual(changed, ["system/backend/cmd/online-test-fixture/main.go"])
        workflow = (ROOT / ".github/workflows/online-t4-gates.yml").read_text()
        job = workflow.split("  infra-object-storage-hosted:\n", 1)[1].split("\n  online-t4:\n", 1)[0]
        self.assertIn('git -C "$RUNNER_TEMP/infra-object-product" checkout --detach "$INFRA_OBJECT_SOURCE_COMMIT"', job)
        self.assertIn('git -C "$RUNNER_TEMP/infra-object-identity" apply', job)
        self.assertNotIn('git -C "$RUNNER_TEMP/infra-object-product" apply', job)


def verify_barrier_postgres():
    """Use the actual observer barrier on an owned PG, never the shared Infra."""
    container = "addp-object-barrier-" + secrets.token_hex(6)
    process = None
    owned = False
    def sql(statement):
        result = subprocess.run(["docker", "exec", "-i", container, "psql", "-XqAt", "-v", "ON_ERROR_STOP=1",
                                 "-U", "postgres", "-d", "addp_test"], input=statement, text=True,
                                capture_output=True, timeout=8)
        if result.returncode:
            raise RuntimeError("barrier verification SQL failed: " + result.stderr.strip())
        return result.stdout.strip()
    try:
        environment = dict(os.environ, POSTGRES_PASSWORD=secrets.token_hex(24))
        subprocess.run(["docker", "run", "-d", "--name", container,
                        "--label", "com.addp.test=infra-object-barrier", "-e", "POSTGRES_PASSWORD",
                        "-e", "POSTGRES_DB=addp_test", "addp-postgres-pgvector:latest"],
                       env=environment, capture_output=True, check=True, timeout=30)
        owned = True
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            # Initdb uses a temporary Unix-socket server before the real listener starts.
            ready = subprocess.run(["docker", "exec", container, "pg_isready", "-h", "127.0.0.1",
                                    "-U", "postgres", "-d", "addp_test"], capture_output=True, timeout=8)
            if ready.returncode == 0 and sql("SELECT current_database()") == "addp_test":
                break
            time.sleep(.5)
        else:
            raise RuntimeError("owned PG readiness timed out")
        sql("CREATE EXTENSION IF NOT EXISTS postgis; CREATE ROLE object_operator; "
            "GRANT USAGE, CREATE ON SCHEMA public TO object_operator;")
        with patch.object(scenario, "SOURCE_CONTAINER", container), patch.object(scenario, "SOURCE_DATABASE", "addp_test"):
            assert scenario.pg(f"SELECT to_json(to_regclass('public.{scenario.TABLE}') IS NULL)", source=True) is True
            with scenario.hold_target(creating=True):
                # This is the provider's catalog existence read under its actual role.
                assert sql("SET ROLE object_operator; SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_class c "
                           "JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace "
                           f"WHERE n.nspname='public' AND c.relname='{scenario.TABLE}')") == "f"
                process = subprocess.Popen(["docker", "exec", "-i", container, "psql", "-XqAt", "-v", "ON_ERROR_STOP=1",
                                            "-U", "postgres", "-d", "addp_test"], stdin=subprocess.PIPE,
                                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                process.stdin.write("SET ROLE object_operator; SET statement_timeout='20s'; "
                                    f"CREATE TABLE public.{scenario.TABLE} (id integer,label text,shape geometry(Point,4326));\n")
                process.stdin.close()
                deadline = time.monotonic() + 8
                while time.monotonic() < deadline:
                    waiting = sql("SELECT count(*) FROM pg_locks WHERE relation='pg_catalog.pg_class'::regclass "
                                  "AND mode='RowExclusiveLock' AND NOT granted")
                    if waiting == "1":
                        break
                    if process.poll() is not None:
                        raise RuntimeError("CREATE was not blocked by the actual barrier")
                    time.sleep(.1)
                else:
                    raise RuntimeError("catalog creation wait not observed")
                assert sql(f"SELECT to_regclass('public.{scenario.TABLE}') IS NULL") == "t"
            assert process.wait(timeout=8) == 0, "CREATE did not complete after barrier release"
            assert sql(f"SELECT to_regclass('public.{scenario.TABLE}') IS NOT NULL") == "t"
        print("barrier=passed catalog_read=passed create_wait=observed release=passed target=created")
    finally:
        if owned:
            subprocess.run(["docker", "rm", "-fv", container], capture_output=True, check=True, timeout=20)
            result = subprocess.run(["docker", "container", "inspect", container], capture_output=True, timeout=8)
            if result.returncode == 0:
                raise RuntimeError("owned PG container remains")
            print("barrier_infra_cleanup=zero_residuals")
        if process is not None:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=5)
            process.stdout.close()
            process.stderr.close()


if __name__ == "__main__":
    if sys.argv[1:] == ["--postgres-barrier"]:
        verify_barrier_postgres()
    else:
        unittest.main()
