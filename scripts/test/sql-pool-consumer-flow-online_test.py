from copy import deepcopy
from importlib import import_module
import unittest

POOL = import_module("scripts.test.sql-pool-consumer-flow-online")


class SQLPoolEvidenceTest(unittest.TestCase):
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
        expected = {"run_id": "run-pool", "principal_id": "4",
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
