import json
import tempfile
import unittest
from importlib import import_module
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch

NODE = import_module("scripts.test.tidb-node-read-online")


class TiDBNodeReadTest(unittest.TestCase):
    def test_receipt_precedes_new_tables_and_five_executions_get_distinct_windows(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            journal = root / "pending.json"
            events, statements = [], []
            preparer, consumer = Mock(), Mock()

            def issue(method, path, expected, body):
                if path.endswith("/revoke"):
                    events.append("revoked")
                    return
                self.assertEqual(body["action"], "read")
                self.assertFalse(body["node_recursive"])
                self.assertEqual(body["catalog_path"]["segments"][-1]["name"], "tidb_scope")
                self.assertEqual(json.loads(journal.read_text())[-1]["request_id"], body["request_id"])
                events.append("issued")
                return SimpleNamespace(payload=dict(body, engine_id="5", operator_principal_id="31"))

            def query(_, engine, locator, deadline, statement, language):
                statements.append(statement)
                index = len(statements)
                events.append(f"exec-{index}")
                if index <= 2:
                    return {
                        "execution_id": str(index),
                        "status": "success",
                        "metadata": {"result": {"summary": {"preview_rows": [{"id": ("37", "41")[index - 1]}]}}},
                    }
                return {
                    "execution_id": str(index),
                    "status": "failed",
                    "error_details": {
                        "code": "develop.query.source_read_denied",
                        "diagnostic": {"stage": "authorization", "reason": "permission_denied"},
                    },
                }

            preparer.request.side_effect = issue
            with (
                patch.object(NODE.REG, "source_grant_participants", return_value=("31", "32")),
                patch.object(NODE.FLOW, "validate_user_identity"),
                patch.object(NODE.REG, "initialize_exact_record_read_grants") as baseline,
                patch.object(
                    NODE.FLOW, "wait_for_scan", side_effect=lambda *a: events.append("scan") or "scan-id"
                ),
                patch.object(NODE.subprocess, "run", side_effect=lambda *a, **k: events.append("created")),
                patch.object(NODE.EXECUTIONS, "develop_execution", side_effect=query),
                patch.object(NODE, "counts", side_effect=[(n, 0) for n in (0, 1, 1, 2, 2, 2, 2, 2, 2, 2)]),
            ):
                NODE.verify(preparer, consumer, 3, 5, journal, root)
            self.assertEqual(
                events,
                [
                    "scan",
                    "issued",
                    "exec-1",
                    "created",
                    "scan",
                    "exec-2",
                    "exec-3",
                    "exec-4",
                    "revoked",
                    "exec-5",
                ],
            )
            self.assertEqual(statements[1], statements[-1])
            self.assertIn("JOIN tidb_outside.node_future", statements[3])
            self.assertEqual({target[2] for target in baseline.call_args.args[3]}, {"tidb_fixture"})
            evidence = json.loads((root / "tidb-node-read.json").read_text())
            self.assertEqual([c["source_selects"] for c in evidence["checks"].values()], [1, 1, 0, 0, 0])
            self.assertEqual([c["execution_id"] for c in evidence["checks"].values()], list("12345"))
            self.assertEqual(
                [evidence["checks"][phase]["returned_id"] for phase in ("existing", "future")],
                ["37", "41"],
            )

    def test_invalid_read_results_leave_only_bounded_evidence(self):
        for rows, returned_type, returned_id in (
            ([{"id": 37}], "int", 37),
            ([{"id": "38"}], "str", "38"),
            ([{"id": "37", "other": "private"}], "str", "37"),
            ([{"id": "private"}], "str", None),
            ([{"id": "37"}, {"id": "37"}], None, None),
            ([], None, None),
        ):
            with self.subTest(rows=rows):
                result = {
                    "execution_id": "sample",
                    "status": "success",
                    "metadata": {"result": {"summary": {"preview_rows": rows}}},
                }
                checks = {}
                with patch.object(NODE, "counts", side_effect=[(3, 4), (4, 5)]):
                    with self.assertRaisesRegex(NODE.FLOW.SuiteError, "expected one native SELECT and row"):
                        NODE.observe(Mock(return_value=result), 5, checks, "existing", "tidb_scope", "node_existing", 37)
                observation = checks["existing"]
                self.assertEqual(observation["source_selects"], 1)
                self.assertEqual(observation["preview_row_count"], len(rows))
                self.assertEqual(observation.get("returned_id_type"), returned_type)
                self.assertEqual(observation.get("returned_id"), returned_id)
                self.assertNotIn("private", json.dumps(observation))

    def test_denial_requires_zero_business_reads_and_the_authorization_reason(self):
        denied = {
            "execution_id": "sample",
            "status": "failed",
            "error_details": {
                "code": "develop.query.source_read_denied",
                "diagnostic": {"stage": "authorization", "reason": "permission_denied"},
            },
        }
        for result, counts in (
            (denied, [(0, 0), (1, 0)]),
            (dict(denied, error_details={}), [(0, 0), (0, 1)]),
        ):
            checks = {}
            with patch.object(NODE, "counts", side_effect=counts), self.assertRaises(NODE.FLOW.SuiteError):
                NODE.observe(
                    Mock(return_value=result), 5, checks, "revoked", "tidb_scope", "node_future", None
                )
            self.assertEqual(checks["revoked"]["execution_id"], "sample")

    def test_cleanup_continues_after_one_failure_and_retains_its_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            journal = root / "pending.json"
            rows = [{"request_id": "one"}, {"request_id": "two"}]
            NODE.write_json(journal, rows)
            with patch.object(NODE, "revoke", side_effect=[OSError("failed"), None]) as revoke:
                with self.assertRaises(NODE.FLOW.SuiteError):
                    NODE.cleanup(Mock(), Mock(), journal, root)
            self.assertEqual(revoke.call_count, 2)
            self.assertEqual(json.loads(journal.read_text()), rows[:1])
            self.assertEqual(
                json.loads((root / "tidb-grant-cleanup.json").read_text()),
                {"revoked": ["two"], "failed": ["one"]},
            )
