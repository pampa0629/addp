from copy import deepcopy
from contextlib import redirect_stdout
from importlib import import_module
from io import StringIO
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

POOL = import_module("scripts.test.sql-pool-consumer-flow-online")
DISPATCH = import_module("scripts.test.online-gate")
CI = import_module("scripts.ci.check-online-ci-registration")


class SQLPoolEvidenceTest(unittest.TestCase):
    def test_mysql_observer_rejects_unverified_native_log_type_or_conversion(self):
        valid = {"argument_type": "mediumblob", "binary_lower": "53454C454354", "decoded_lower": "73656C656374"}
        for facts in (valid, {**valid, "decoded_lower": valid["binary_lower"]}, {**valid, "argument_type": "mediumtext"}):
            with self.subTest(facts=facts), patch.object(POOL, "sql", return_value=POOL.subprocess.CompletedProcess(
                [], 0, stdout=json.dumps(facts), stderr=""
            )):
                if facts == valid:
                    self.assertEqual(POOL.mysql_log_contract(), valid)
                else:
                    with self.assertRaisesRegex(POOL.SUPPORT.SuiteError, "native log text contract"):
                        POOL.mysql_log_contract()

    def test_page_lifecycle_cannot_omit_system_frontend(self):
        root = Path(__file__).resolve().parents[2]
        registry = {POOL.NAME: DISPATCH.SUITES[POOL.NAME]}
        CI.validate_sql_pool_profile(root, registry)
        lifecycle = root / "scripts/test/online-hosted-sql-pool-gate.sh"
        original = Path.read_text

        def without_system(path, *args, **kwargs):
            text = original(path, *args, **kwargs)
            return text.replace("in -system -security", "in -security") if path == lifecycle else text

        with patch.object(Path, "read_text", without_system):
            with self.assertRaisesRegex(CI.RegistrationError, "System frontend lifecycle"):
                CI.validate_sql_pool_profile(root, registry)

    def test_verified_browser_output_is_returned_to_online_dispatcher(self):
        evidence = {"schema_version": "addp.sql-pool-console/v2", "run_id": "run-pool", "cases": []}
        processes = {"system": [19, "9"]}
        with tempfile.TemporaryDirectory() as tmp:
            private = Path(tmp)
            (private / "pool-expectations.json").write_text("{}")

            def browser(*args, env, **kwargs):
                Path(env["ADDP_ONLINE_POOL_REPORT"]).write_text(json.dumps(evidence))
                return POOL.subprocess.CompletedProcess(args, 0)

            stdout = StringIO()
            with patch.object(POOL, "process_identity", return_value=processes), \
                 patch.object(POOL, "validate_browser") as validate, \
                 patch.object(POOL.subprocess, "run", side_effect=browser), redirect_stdout(stdout):
                POOL.run(private, private)
            validate.assert_called_once_with(evidence, {})
            self.assertEqual(DISPATCH._parse_stage_report("scenario", stdout.getvalue()), evidence)

    def test_native_postgres_evidence_requires_reader_and_sample_table_select(self):
        lines = """
pid=27 user=pool_reader LOG:  statement: SELECT * FROM "public"."pool_source" LIMIT 1
pid=28 user=pool_reader LOG:  execute stmtcache_1: SELECT id, label
    FROM public.pool_source
pid=29 user=pool_reader LOG:  statement: SELECT 'pool_source' FROM pg_class
pid=30 user=fixture_admin LOG:  statement: SELECT * FROM public.pool_source
pid=31 user=pool_reader LOG:  statement: SELECT * FROM public.pool_source_other
pid=32 user=pool_reader LOG:  statement: SELECT 1
pid=33 user=pool_reader LOG:  statement: UPDATE pool_source SET id=0
"""
        self.assertEqual(POOL.pg_statement_ids(lines), [27, 28])

    def test_native_postgres_evidence_separates_credential_changes(self):
        lines = """
pid=41 user=pool_reader LOG:  statement: SELECT id FROM public.pool_source
pid=42 user=pool_reader_next LOG:  execute stmt_1: SELECT id FROM public.pool_source
pid=43 user=pool_reader_next_extra LOG:  statement: SELECT id FROM public.pool_source
pid=44 user=pool_reader_next LOG:  statement: SELECT id FROM public.pool_source_other
"""
        self.assertEqual(POOL.pg_statement_ids(lines, "pool_reader"), [41])
        self.assertEqual(POOL.pg_statement_ids(lines, "pool_reader_next"), [42])
        with self.assertRaisesRegex(POOL.SUPPORT.SuiteError, "unknown SQL pool fixture reader"):
            POOL.pg_statement_ids(lines, "fixture_admin")

    def connection_evidence(self):
        expected = {"run_id": "run-pool", "principal_id": "4", "operator_principal_id": "6", "cases": []}
        evidence = {"schema_version": "addp.sql-pool-console/v2", "run_id": "run-pool",
                    "consumer_principal_id": "4", "operator_principal_id": "6", "cases": []}
        for engine_id, engine in ((5, "postgresql"), (8, "mysql")):
            source = {"container_id": engine, "database": "pool_fixture", "native_id": "native-" + engine}
            expected["cases"].append({"engine_id": engine_id, "engine_type": engine, "source_identity": source})
            case = {"engine_id": engine_id, "engine_type": engine, "source_identity": source, "changes": []}
            previous_ids = {"manager": [1], "develop": [2]}
            previous_execution = f"{engine}-initial"
            for index, phase in enumerate(POOL.PHASES):
                reader = POOL.READERS[0] if phase == "address" else POOL.READERS[1]
                old_reader = POOL.READERS[0] if phase in ("address", "account") else POOL.READERS[1]
                fields = {"address": ["host"], "account": ["password", "user"], "password": ["password"], "pool": ["pool_max_idle_conns"]}[phase]
                change = {"phase": phase, "before_version": index + 1, "after_version": index + 2,
                          "changed_fields": fields, "same_engine_confirmed": phase == "address",
                          "host": "localhost", "database": "pool_fixture", "reader": reader,
                          "max_open": 2, "max_idle": 0 if phase == "pool" else 1,
                          "connection_online": True, "consumers": {}}
                for offset, consumer in enumerate(("manager", "develop")):
                    fresh = [3 + 2 * index + offset]
                    proof = {"old_ids": previous_ids[consumer], "new_ids": fresh, "old_active_after": [] if phase in ("account", "pool") else fresh,
                             "active_after": [] if phase == "pool" else fresh, "old_reader": old_reader,
                             "reader": reader, "before_rows": 1, "after_rows": 1}
                    if consumer == "develop":
                        following = f"{engine}-{phase}"
                        proof["execution_ids"] = [previous_execution, following]
                        previous_execution = following
                    change["consumers"][consumer] = proof
                    previous_ids[consumer] = fresh
                if phase == "password":
                    change["password_proof"] = {"engine": engine, "run_id": "run-pool", "reader": POOL.READERS[1],
                        "source_identity": source, "old_password_rejected": True, "new_password_accepted": True}
                case["changes"].append(change)
            evidence["cases"].append(case)
        return evidence, expected

    def test_browser_evidence_rejects_source_drift_and_unproven_configuration_phases(self):
        evidence, expected = self.connection_evidence()
        POOL.validate_browser(evidence, expected)
        mutations = [
            lambda c: c["changes"].pop(),
            lambda c: c["source_identity"].update(native_id="different-source"),
            lambda c: c["changes"][0].update(database="another-database"),
            lambda c: c["changes"][0].update(same_engine_confirmed=False),
            lambda c: c["changes"][1].update(before_version=1),
            lambda c: c["changes"][1].update(password="must-not-be-in-artifacts"),
            lambda c: c["changes"][2]["password_proof"].update(old_password_rejected=False),
            lambda c: c["changes"][2].pop("password_proof"),
        ]
        for mutation in mutations:
            invalid = deepcopy(evidence)
            mutation(invalid["cases"][0])
            with self.assertRaises(POOL.SUPPORT.SuiteError):
                POOL.validate_browser(invalid, expected)
        with self.assertRaisesRegex(POOL.SUPPORT.SuiteError, "registration user"):
            POOL.validate_browser({**evidence, "operator_principal_id": "7"}, expected)

    def test_browser_evidence_rejects_cross_reader_or_reused_physical_sessions(self):
        evidence, expected = self.connection_evidence()
        for phase, scope, field, replacement in [
            (0, "manager", "new_ids", []), (0, "manager", "after_rows", 2),
            (0, "manager", "old_active_after", [1]), (0, "manager", "active_after", []),
            (1, "manager", "old_ids", [90]), (1, "manager", "old_reader", "pool_reader_next"),
            (1, "develop", "reader", "pool_reader"), (2, "develop", "new_ids", [7]),
            (3, "develop", "active_after", [10]), (1, "develop", "execution_ids", ["wrong", "wrong"]),
        ]:
            with self.subTest(phase=phase, scope=scope, field=field):
                invalid = deepcopy(evidence)
                invalid["cases"][0]["changes"][phase]["consumers"][scope][field] = replacement
                with self.assertRaises(POOL.SUPPORT.SuiteError):
                    POOL.validate_browser(invalid, expected)

    def test_password_rotation_requires_authentication_denial_not_network_failure(self):
        for engine in POOL.PROFILES:
            for authentication_error in (True, False):
                with self.subTest(engine=engine, authentication_error=authentication_error), tempfile.TemporaryDirectory() as tmp:
                    private = Path(tmp)
                    credentials = {"initial": "0" * 48, "account": "a" * 48, "rotated": "b" * 48}
                    (private / f"{engine}-credentials.json").write_text(json.dumps(credentials))
                    denied = "password authentication failed" if engine == "postgresql" else "ERROR 1045 (28000)"
                    replies = [POOL.subprocess.CompletedProcess([], 2, stdout="", stderr=denied if authentication_error else "network failed"),
                               POOL.subprocess.CompletedProcess([], 0, stdout="1\n", stderr="")]
                    output = StringIO()
                    with patch.object(POOL, "fixture_identity", return_value={"native_id": "same"}), \
                         patch.object(POOL, "sql", return_value=POOL.subprocess.CompletedProcess([], 0, stdout="[]")), \
                         patch.dict(POOL.os.environ, {"ADDP_ONLINE_ARTIFACT_DIR": tmp}), \
                         patch.object(POOL, "docker", side_effect=replies), redirect_stdout(output):
                        if authentication_error:
                            POOL.rotate_password(engine, private, private, "run-pool")
                            proof = json.loads(output.getvalue())
                            self.assertTrue(proof["old_password_rejected"])
                            self.assertFalse(any(value in output.getvalue() for value in credentials.values()))
                            self.assertEqual(json.loads((private / "sql-pool-password-probes.jsonl").read_text()), proof)
                            with self.assertRaisesRegex(POOL.SUPPORT.SuiteError, "cannot be repeated"):
                                POOL.rotate_password(engine, private, private, "run-pool")
                        else:
                            with self.assertRaisesRegex(POOL.SUPPORT.SuiteError, "authentication proof failed"):
                                POOL.rotate_password(engine, private, private, "run-pool")
                            self.assertFalse((private / "sql-pool-password-probes.jsonl").exists())
