#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import shutil
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check-engine-plugin-registration.py")
SPEC = importlib.util.spec_from_file_location("engine_plugin_registration", SCRIPT)
assert SPEC and SPEC.loader
CHECKER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CHECKER)


class EnginePluginRegistrationTest(unittest.TestCase):
    def setUp(self) -> None:
        self.root = Path(tempfile.mkdtemp(prefix="addp-engine-plugin-registration-"))
        self.addCleanup(lambda: shutil.rmtree(self.root))
        self._plugin("alpha", "general")
        self._plugin("beta", "extension")
        self._write(
            "common/engine/plugins/builtin/general/init.go",
            'package general\n\nimport _ "github.com/addp/common/engine/plugins/alpha"\n',
        )
        self._write(
            "common/engine/plugins/builtin/extension/init.go",
            'package extension\n\nimport _ "github.com/addp/common/engine/plugins/beta"\n',
        )
        self._write(
            "common/engine/plugins/builtin/all/init.go",
            "package all\n\nimport (\n"
            '\t_ "github.com/addp/common/engine/plugins/builtin/extension"\n'
            '\t_ "github.com/addp/common/engine/plugins/builtin/general"\n'
            ")\n",
        )
        self._write("app/main.go", "package main\n")
        self._write(
            "Makefile",
            "test-platform:\n\t@$(MAKE) test-engine-plugin-registration\n\n"
            "test-engine-plugin-registration:\n"
            "\t@python3 scripts/ci/check-engine-plugin-registration_test.py\n"
            '\t@python3 scripts/ci/check-engine-plugin-registration.py --repository "$(CURDIR)"\n',
        )

    def _write(self, relative: str, content: str) -> None:
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def _plugin(self, package: str, origin: str) -> None:
        self._write(
            f"common/engine/plugins/{package}/plugin.go",
            f'''package {package}\n\nimport "github.com/addp/common/engine/plugin"\n\n'''
            "type Plugin struct{}\n\n"
            "func init() { plugin.Register(&Plugin{}) }\n"
            f'func (p *Plugin) EngineOrigin() string {{ return "{origin}" }}\n',
        )

    def test_accepts_complete_origin_correct_registration(self) -> None:
        self.assertEqual([], CHECKER.validate(self.root))

    def test_rejects_plugin_missing_from_aggregate(self) -> None:
        self._plugin("gamma", "general")
        errors = CHECKER.validate(self.root)
        self.assertTrue(any("plugins/gamma" in error and "got []" in error for error in errors), errors)

    def test_discovers_aliased_register_call_and_split_origin_method(self) -> None:
        self._write(
            "common/engine/plugins/gamma/plugin.go",
            '''package gamma\n\nimport engineplugin "github.com/addp/common/engine/plugin"\n\n'''
            "type Plugin struct{}\n\n"
            "func init() { engineplugin.Register(&Plugin{}) }\n",
        )
        self._write(
            "common/engine/plugins/gamma/origin.go",
            'package gamma\n\nfunc (p *Plugin) EngineOrigin() string { return "general" }\n',
        )
        path = self.root / "common/engine/plugins/builtin/general/init.go"
        path.write_text(
            path.read_text(encoding="utf-8")
            + 'import _ "github.com/addp/common/engine/plugins/gamma"\n',
            encoding="utf-8",
        )
        self.assertEqual([], CHECKER.validate(self.root))

    def test_rejects_plugin_in_wrong_aggregate(self) -> None:
        self._write(
            "common/engine/plugins/builtin/general/init.go",
            'package general\n\nimport _ "github.com/addp/common/engine/plugins/beta"\n',
        )
        errors = CHECKER.validate(self.root)
        self.assertTrue(any("beta" in error and "not 'general'" in error for error in errors), errors)
        self.assertTrue(any("alpha" in error and "got []" in error for error in errors), errors)

    def test_rejects_stale_aggregate_import(self) -> None:
        path = self.root / "common/engine/plugins/builtin/general/init.go"
        path.write_text(
            path.read_text(encoding="utf-8")
            + 'import _ "github.com/addp/common/engine/plugins/missing"\n',
            encoding="utf-8",
        )
        errors = CHECKER.validate(self.root)
        self.assertTrue(any("missing" in error and "plugin.Register" in error for error in errors), errors)

    def test_rejects_duplicate_aggregate_import(self) -> None:
        path = self.root / "common/engine/plugins/builtin/general/init.go"
        path.write_text(path.read_text(encoding="utf-8") * 2, encoding="utf-8")
        errors = CHECKER.validate(self.root)
        self.assertTrue(any("duplicate blank import" in error and "alpha" in error for error in errors), errors)

    def test_rejects_upper_layer_concrete_blank_import(self) -> None:
        self._write(
            "app/main.go",
            'package main\n\nimport _ "github.com/addp/common/engine/plugins/alpha"\n',
        )
        errors = CHECKER.validate(self.root)
        self.assertTrue(any("app/main.go" in error and "concrete Engine Plugin alpha" in error for error in errors), errors)

    def test_allows_test_specific_concrete_blank_import(self) -> None:
        self._write(
            "app/main_test.go",
            'package main\n\nimport _ "github.com/addp/common/engine/plugins/alpha"\n',
        )
        self.assertEqual([], CHECKER.validate(self.root))

    def test_rejects_all_aggregate_direct_plugin_import(self) -> None:
        self._write(
            "common/engine/plugins/builtin/all/init.go",
            'package all\n\nimport _ "github.com/addp/common/engine/plugins/alpha"\n',
        )
        errors = CHECKER.validate(self.root)
        self.assertTrue(any("builtin/all/init.go" in error and "must import only" in error for error in errors), errors)

    def test_rejects_missing_make_registration(self) -> None:
        self._write("Makefile", "test-platform:\n\t@true\n")
        errors = CHECKER.validate(self.root)
        self.assertTrue(any("Makefile registration is missing" in error for error in errors), errors)


if __name__ == "__main__":
    unittest.main()
