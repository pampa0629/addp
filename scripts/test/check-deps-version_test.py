#!/usr/bin/env python3
"""Dependency policy must inspect both canonical Go require forms."""

from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "utils/check-deps-version.sh"


class DependencyVersionTest(unittest.TestCase):
    def run_gate(self, declaration: str) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory(prefix="addp-dependency-policy-") as temporary:
            root = Path(temporary)
            script = root / "scripts/utils/check-deps-version.sh"
            script.parent.mkdir(parents=True)
            shutil.copyfile(SCRIPT, script)
            spec = root / "docs/spec/addp技术栈规约.md"
            spec.parent.mkdir(parents=True)
            spec.write_text("`cel.dev/cel-go@v0.32.0`\n", encoding="utf-8")
            module = root / "sample/backend/go.mod"
            module.parent.mkdir(parents=True)
            module.write_text("module example.test/sample\n\ngo 1.24.2\n\n" + declaration, encoding="utf-8")
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            return subprocess.run(["bash", str(script)], capture_output=True, text=True)

    def test_supported_require_forms_are_counted(self):
        for declaration in (
            "require cel.dev/cel-go v0.32.0\n",
            "require (\n\tcel.dev/cel-go v0.32.0\n)\n",
            "require cel.dev/cel-go v0.32.0 // indirect\n",
        ):
            with self.subTest(declaration=declaration):
                result = self.run_gate(declaration)
                self.assertEqual(0, result.returncode, result.stdout + result.stderr)
                self.assertIn("cel.dev/cel-go: v0.32.0 (1 个模块声明)", result.stdout)

    def test_mismatch_fails_in_both_forms(self):
        for declaration in (
            "require cel.dev/cel-go v0.31.0\n",
            "require (\n\tcel.dev/cel-go v0.31.0\n)\n",
        ):
            with self.subTest(declaration=declaration):
                result = self.run_gate(declaration)
                self.assertEqual(1, result.returncode, result.stdout + result.stderr)
                self.assertIn("sample/backend/go.mod 声明 v0.31.0", result.stdout, result.stderr)

    def test_comments_are_not_requirements(self):
        result = self.run_gate("// require cel.dev/cel-go v0.31.0\nrequire example.test/other v1.0.0\n")
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        self.assertIn("0 处 go.mod 声明", result.stdout)

    def test_exclude_block_is_not_a_requirement(self):
        result = self.run_gate("exclude (\n\tcel.dev/cel-go v0.31.0\n)\n")
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        self.assertIn("0 处 go.mod 声明", result.stdout)


if __name__ == "__main__":
    unittest.main()
