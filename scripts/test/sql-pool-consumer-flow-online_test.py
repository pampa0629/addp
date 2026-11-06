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


class SQLPoolEvidenceTest(unittest.TestCase):
    def test_verified_browser_output_is_returned_to_online_dispatcher(self):
        evidence = {"schema_version": "addp.sql-pool-console/v1", "run_id": "run-pool", "cases": []}
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

    def test_browser_evidence_rejects_missing_or_reused_physical_sessions(self):
        expected = {"run_id": "run-pool", "principal_id": "4", "operator_principal_id": "6",
                    "cases": [{"engine_id": 5, "engine_type": "postgresql"}, {"engine_id": 8, "engine_type": "mysql"}]}
        evidence = {"schema_version": "addp.sql-pool-console/v1", "run_id": "run-pool",
                    "consumer_principal_id": "4", "operator_principal_id": "6", "cases": []}
        for item in expected["cases"]:
            engine = item["engine_id"]
            evidence["cases"].append({
                **item, "before_version": 1, "after_version": 2, "max_open": 2, "idle_before": 1, "idle_after": 0,
                "consumers": {
                    "manager": {"before_rows": 1, "after_rows": 1, "old_ids": [1], "new_ids": [3], "active_after": [2, 9]},
                    "develop": {"before_rows": 1, "after_rows": 1, "old_ids": [2], "new_ids": [4], "active_after": [9],
                                "execution_ids": [f"old-{engine}", f"new-{engine}"]},
                },
            })
        POOL.validate_browser(evidence, expected)
        with self.assertRaisesRegex(POOL.SUPPORT.SuiteError, "registration user"):
            POOL.validate_browser({**evidence, "operator_principal_id": "7"}, expected)
        for scope, field, replacement in [
            ("manager", "new_ids", []), ("manager", "after_rows", 2), ("manager", "active_after", [1, 2]),
            ("develop", "old_ids", [1]), ("develop", "new_ids", [3]),
            ("develop", "active_after", [4]), ("develop", "execution_ids", ["old-8", "new-8"]),
        ]:
            with self.subTest(scope=scope, field=field, replacement=replacement):
                invalid = deepcopy(evidence)
                invalid["cases"][0]["consumers"][scope][field] = replacement
                with self.assertRaises(POOL.SUPPORT.SuiteError):
                    POOL.validate_browser(invalid, expected)
