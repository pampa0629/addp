#!/usr/bin/env python3
"""Exercise the existing schema ownership gates in isolated Git repositories.

Usage: python3 scripts/test/schema-ownership-gates_test.py [TestClass]
"""

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).parents[2]
BASH = shutil.which("bash")
GIT = shutil.which("git")


class SchemaOwnershipCases:
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-schema-owner-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.repository = self.root / "repository"
        self.repository.mkdir()
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.scratch = self.root / "scratch"
        self.scratch.mkdir()
        for name in ("git", "rg", "mktemp", "rm"):
            command = shutil.which(name)
            if not command:
                self.fail(f"schema ownership regression tests require {name}")
            (self.bin / name).symlink_to(command)
        subprocess.run([GIT, "init", "-q", str(self.repository)], check=True)
        self.write("sample/backend/clean_test.go", "package sample\n")

    def write(self, path, content, tracked=True):
        target = self.repository / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content)
        if tracked:
            subprocess.run([GIT, "-C", str(self.repository), "add", "--", path], check=True)

    def fake(self, name, body):
        target = self.bin / name
        target.unlink()
        target.write_text(f"#!{BASH}\n{body}\n")
        target.chmod(0o755)

    def run_gate(self):
        result = subprocess.run(
            [BASH, str(ROOT / "scripts/test" / self.script)],
            env=dict(os.environ, PATH=str(self.bin), ADDP_REPOSITORY_ROOT=str(self.repository),
                     TMPDIR=str(self.scratch), ADDP_TEST_REAL_GIT=GIT),
            capture_output=True, text=True, timeout=10,
        )
        self.assertEqual(list(self.scratch.iterdir()), [], "gate left temporary files")
        return result

    def test_no_matches_passes(self):
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("gate passed", result.stdout)

    def test_exact_allowed_owners_pass(self):
        for path in self.allowed:
            self.write(path, self.schema)
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_violation_preserves_spaces_unicode_and_newlines(self):
        path = "sample/backend/非法 空格\nsource_test.go"
        self.write(path, self.schema)
        result = self.run_gate()
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn(path, result.stderr)
        self.assertNotIn("gate passed", result.stdout)

    def test_missing_ripgrep_fails(self):
        (self.bin / "rg").unlink()
        result = self.run_gate()
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertIn("required command is missing: rg", result.stderr)
        self.assertNotIn("gate passed", result.stdout)

    def test_search_errors_with_partial_output_fail(self):
        for status in (2, 127):
            with self.subTest(status=status):
                self.fake("rg", f"printf 'partial\\0'; echo search-broken >&2; exit {status}")
                result = self.run_gate()
                self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
                self.assertIn(f"ripgrep search failed (exit status {status})", result.stderr)
                self.assertIn("search-broken", result.stderr)
                self.assertNotIn("gate passed", result.stdout)

    def test_git_listing_error_with_partial_output_fails(self):
        self.fake("git", 'if [ "$3" = ls-files ]; then printf "sample/backend/clean_test.go\\0"; exit 128; fi\n'
                         'exec "$ADDP_TEST_REAL_GIT" "$@"')
        result = self.run_gate()
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertIn("Git file enumeration failed", result.stderr)
        self.assertNotIn("gate passed", result.stdout)

    def test_missing_git_fails(self):
        (self.bin / "git").unlink()
        result = self.run_gate()
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertIn("required command is missing: git", result.stderr)

    def test_empty_input_fails(self):
        (self.repository / "sample/backend/clean_test.go").unlink()
        result = self.run_gate()
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertNotIn("gate passed", result.stdout)

    def test_untracked_file_scope_is_preserved(self):
        self.write("sample/backend/untracked_test.go", self.schema, tracked=False)
        result = self.run_gate()
        self.assertEqual(result.returncode, self.untracked_status, result.stdout + result.stderr)


class ExecutionFixtureGateTest(SchemaOwnershipCases, unittest.TestCase):
    script = "check-execution-test-fixtures.sh"
    schema = 'create\n table if not exists "common"."task_executions" (id integer);\n'
    allowed = (
        "common/execution/repository_test.go",
        "manager/backend/internal/repository/database_test.go",
        "system/backend/internal/iam/execution_authorization_postgres_test.go",
        "system/backend/internal/migration/runner_postgres_test.go",
    )
    untracked_status = 0


class ProjectionStoreGateTest(SchemaOwnershipCases, unittest.TestCase):
    script = "check-protection-projection-store-ownership.sh"
    schema = 'CREATE TABLE protection_projection_entries (id integer);\n'
    allowed = (
        "common/dataprotection/projectionstore/store.go",
        "common/dataprotection/projectionstore/schema.go",
        "common/dataprotection/projectionstore/store_postgres_integration_test.go",
    )
    untracked_status = 1


if __name__ == "__main__":
    unittest.main()
