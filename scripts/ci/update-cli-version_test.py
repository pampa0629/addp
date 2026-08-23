#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("update-cli-version.py")
SPEC = importlib.util.spec_from_file_location("update_cli_version", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(MODULE)


class UpdateCliVersionTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        self._write("common-python/addp_common/__init__.py", '__version__ = "1.2.3"\n')
        self._write("README.md", "version-1.2.3-blue.svg\n")
        self._write(
            "common-python/README.md",
            "当前版本为 `1.2.3`。\nRELEASE=v1.2.3\n",
        )
        self._write("common-python/common-python实施报告.md", "当前版本为 `1.2.3`。\n")
        self._write("scripts/README.md", "**Version**: 1.2.3\n")

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def _write(self, relative_path: str, content: str) -> None:
        path = self.repository / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def test_updates_all_registered_version_facts(self) -> None:
        updated = MODULE.update_version(self.repository, "1.3.0")
        self.assertEqual(5, len(updated))
        for path in updated:
            text = path.read_text(encoding="utf-8")
            self.assertNotIn("1.2.3", text)
            self.assertIn("1.3.0", text)

    def test_rejects_non_stable_version(self) -> None:
        with self.assertRaisesRegex(MODULE.VersionUpdateError, "X.Y.Z"):
            MODULE.prepare_updates(self.repository, "1.3.0-rc1")

    def test_rejects_same_or_lower_version(self) -> None:
        for version in ("1.2.3", "1.2.2"):
            with self.subTest(version=version), self.assertRaisesRegex(
                MODULE.VersionUpdateError, "must be greater"
            ):
                MODULE.prepare_updates(self.repository, version)

    def test_does_not_write_partial_update_when_contract_is_incomplete(self) -> None:
        version_file = self.repository / "common-python/addp_common/__init__.py"
        original = version_file.read_text(encoding="utf-8")
        self._write("scripts/README.md", "missing version contract\n")
        with self.assertRaisesRegex(MODULE.VersionUpdateError, "found 0"):
            MODULE.update_version(self.repository, "1.3.0")
        self.assertEqual(original, version_file.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
