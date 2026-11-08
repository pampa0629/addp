#!/usr/bin/env python3
"""Verify FastAPI Swagger checks in isolated, dependency-free application fixtures."""

import json
import os
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


class SwaggerRouteCoverageTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-swagger-check-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.scripts = self.root / "scripts/swagger"
        self.scripts.mkdir(parents=True)
        for name in ("check-route-coverage.sh", "gen-swagger.sh", "verify-swagger.sh"):
            shutil.copy2(ROOT / "scripts/swagger" / name, self.scripts / name)
        self.schema = {"paths": {"/example": {"get": {"summary": "测试 | Test"}}}}
        for module in ("agent", "copilot"):
            backend = self.root / module / "backend"
            (backend / "venv/bin").mkdir(parents=True)
            interpreter = backend / "venv/bin/python"
            interpreter.write_text(
                "#!/bin/sh\n"
                f"export ADDP_SWAGGER_FIXTURE_OWNER={shlex.quote(module)}\n"
                f"exec {shlex.quote(sys.executable)} \"$@\"\n"
            )
            interpreter.chmod(0o755)
            self.application(module)
            self.document(module, self.schema)
        source = (self.scripts / "check-route-coverage.sh").read_text()
        owners = re.search(r"^GO_MODULES=\(([^)]*)\)", source, re.MULTILINE).group(1).split()
        for owner in owners:
            self.go_fixture(documented=True, module=owner)

    def application(self, module, source=None):
        if source is None:
            source = (
                "import os\nfrom pathlib import Path\n"
                "if os.environ.get('ADDP_SWAGGER_FIXTURE_OWNER'):\n"
                " Path('selected-runtime').write_text(os.environ['ADDP_SWAGGER_FIXTURE_OWNER'])\n"
                "class App:\n"
                f" def openapi(self): return {self.schema!r}\n"
                "app = App()\n"
            )
        (self.root / module / "backend/main.py").write_text(source)

    def document(self, module, value):
        (self.root / module / "backend/openapi.json").write_text(
            json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
        )

    def run_script(self, name, *modules, legacy_overrides=False):
        return subprocess.run(
            ["bash", str(self.scripts / name), *modules],
            cwd=self.root,
            env=dict(os.environ, SWAGGER_COVERAGE_WARN_ONLY="1" if legacy_overrides else "0"),
            capture_output=True, text=True, timeout=15,
        )

    def check(self, *modules, legacy_overrides=False):
        return self.run_script("check-route-coverage.sh", *modules, legacy_overrides=legacy_overrides)

    def test_matching_projections_use_each_owner_runtime(self):
        result = self.check("agent", "copilot")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(result.stdout.count("FastAPI OpenAPI 投影一致"), 2)
        for module in ("agent", "copilot"):
            self.assertEqual((self.root / module / "backend/selected-runtime").read_text(), module)

    def test_missing_owner_runtime_cannot_fall_back_or_warn(self):
        for module in ("agent", "copilot"):
            with self.subTest(module=module):
                (self.root / module / "backend/venv/bin/python").unlink()
                result = self.check(module, legacy_overrides=True)
                self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
                self.assertNotIn("本次仅告警", result.stdout)
                self.assertNotIn("投影一致", result.stdout)

    def test_import_failure_cannot_warn(self):
        self.application("agent", "raise ModuleNotFoundError('fixture dependency unavailable')\n")
        result = self.check("agent", legacy_overrides=True)
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertNotIn("本次仅告警", result.stdout)

    def test_openapi_export_failure_cannot_warn(self):
        self.application("agent", "class App:\n def openapi(self): raise RuntimeError('schema failed')\napp = App()\n")
        result = self.check("agent", legacy_overrides=True)
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertNotIn("本次仅告警", result.stdout)

    def test_application_exit_cannot_skip_comparison(self):
        for status in (0, 1):
            with self.subTest(status=status):
                self.application("agent", f"raise SystemExit({status})\n")
                result = self.check("agent", legacy_overrides=True)
                self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
                self.assertNotIn("本次仅告警", result.stdout)

    def test_invalid_openapi_export_cannot_warn(self):
        self.application("agent", "class App:\n def openapi(self): return None\napp = App()\n")
        self.document("agent", None)
        result = self.check("agent", legacy_overrides=True)
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)

    def test_missing_document_cannot_warn(self):
        (self.root / "agent/backend/openapi.json").unlink()
        result = self.check("agent", legacy_overrides=True)
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertNotIn("本次仅告警", result.stdout)

    def test_projection_mismatch_fails(self):
        self.document("agent", {"paths": {}})
        result = self.check("agent")
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("投影与运行时路由不一致", result.stdout)

    def test_legacy_override_cannot_downgrade_completed_comparison(self):
        self.document("agent", {"paths": {}})
        result = self.check("agent", legacy_overrides=True)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertNotIn("本次仅告警", result.stdout)

    def test_environment_failure_wins_over_warning_in_another_module(self):
        self.document("agent", {"paths": {}})
        (self.root / "copilot/backend/venv/bin/python").unlink()
        result = self.check("agent", "copilot", legacy_overrides=True)
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
        self.assertIn("投影与运行时路由不一致", result.stdout)
        self.assertNotIn("本次仅告警", result.stdout)

    def go_fixture(self, documented, module="system"):
        backend = self.root / module / "backend"
        for folder in ("internal/api", "cmd/server", "docs"):
            (backend / folder).mkdir(parents=True, exist_ok=True)
        (backend / "internal/api/router.go").write_text(
            f'api := router.Group("/api/v1/{module}")\napi.GET("/example", handler)\n'
        )
        (backend / "cmd/server/main.go").write_text(f'// @BasePath /api/v1/{module}\n')
        (backend / "docs/swagger.json").write_text(json.dumps({
            "basePath": f"/api/v1/{module}", "paths": {"/example": {"get": {}}} if documented else {}
        }))

    def test_go_projection_mismatch_cannot_be_downgraded(self):
        self.go_fixture(documented=False)
        result = self.check("system", legacy_overrides=True)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertNotIn("本次仅告警", result.stdout)

    def test_missing_registered_go_input_fails(self):
        for name in ("internal/api/router.go", "cmd/server/main.go", "docs/swagger.json"):
            with self.subTest(name=name):
                path = self.root / "system/backend" / name
                original = path.read_bytes()
                path.unlink()
                result = self.check("system", legacy_overrides=True)
                self.assertEqual(result.returncode, 2, result.stdout + result.stderr)
                self.assertNotIn("跳过", result.stdout)
                path.write_bytes(original)

    def test_matching_go_projection_passes(self):
        self.go_fixture(documented=True)
        result = self.check("system")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_unreadable_go_document_is_a_check_error(self):
        (self.root / "system/backend/docs/swagger.json").write_text("invalid JSON")
        result = self.check("system", legacy_overrides=True)
        self.assertEqual(result.returncode, 2, result.stdout + result.stderr)

    def test_independent_verifier_propagates_coverage_failure(self):
        self.document("agent", {"paths": {}})
        result = self.run_script("verify-swagger.sh", "--coverage-only", legacy_overrides=True)
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertNotIn("本次仅告警", result.stdout)
        self.assertNotIn("验证 Swagger 文档可访问性", result.stdout)

    def test_independent_coverage_verifier_passes_without_running_services(self):
        result = self.run_script("verify-swagger.sh", "--coverage-only")
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn("验证 Swagger 文档可访问性", result.stdout)

    def test_generation_without_owner_runtime_fails(self):
        (self.root / "agent/backend/venv/bin/python").unlink()
        result = self.run_script("gen-swagger.sh", "agent")
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_generation_and_check_share_owner_runtime_and_serialization(self):
        for module in ("agent", "copilot"):
            self.document(module, {"paths": {}})
        generated = self.run_script("gen-swagger.sh", "agent", "copilot")
        self.assertEqual(generated.returncode, 0, generated.stdout + generated.stderr)
        checked = self.check("agent", "copilot")
        self.assertEqual(checked.returncode, 0, checked.stdout + checked.stderr)
        for module in ("agent", "copilot"):
            document = (self.root / module / "backend/openapi.json").read_text()
            self.assertEqual(json.loads(document), self.schema)
            self.assertTrue(document.endswith("\n"))



class SwaggerToolVersionTest(unittest.TestCase):
    def run_generation(self, version, *, install_fails=False):
        with tempfile.TemporaryDirectory(prefix='addp-swag-version-') as temporary:
            root = Path(temporary)
            scripts = root / 'scripts/swagger'
            scripts.mkdir(parents=True)
            shutil.copy2(ROOT / 'scripts/swagger/gen-swagger.sh', scripts / 'gen-swagger.sh')
            tools = root / 'tools'
            tools.mkdir()
            home = root / 'home'
            backend = root / 'system/backend'
            (backend / 'cmd/server').mkdir(parents=True)
            (backend / 'cmd/server/main.go').write_text('package main\n')
            (backend / 'go.mod').write_text('module example/system\n')
            swag_body = (
                '#!' + sys.executable + '\n# MODULE_VERSION=@VERSION@\nimport sys\nfrom pathlib import Path\n'
                "if sys.argv[1:] == ['--version']:\n print('swag version v1.16.4');sys.exit(0)\n"
                "root=Path.cwd().parents[1]\n"
                "with (root/'generated-by').open('a') as out:out.write(str(Path(sys.argv[0]))+'\\n')\n"
                "Path('docs').mkdir(exist_ok=True)\n"
                "for name in ('docs.go','swagger.json','swagger.yaml'):Path('docs',name).write_text('canonical')\n"
            )
            if version is not None:
                binary = tools / 'swag'
                binary.write_text(swag_body.replace('@VERSION@', version))
                binary.chmod(0o755)
            go = tools / 'go'
            go.write_text('#!' + sys.executable + '\n' +
                'import json,sys,os,re\nfrom pathlib import Path\n' +
                'root=Path(' + repr(str(root)) + ')\n' +
                "if sys.argv[1]=='env':print(json.dumps({'GOWORK':'off'}))\n" +
                "elif sys.argv[1]=='list':print(json.dumps({'Path':'example/system','Main':True,'Dir':str(root/'system/backend')}))\n" +
                "elif sys.argv[1:3]==['version','-m']:\n print('\\tmod\\tgithub.com/swaggo/swag\\t'+re.search(r'MODULE_VERSION=(v[0-9.]+)',Path(sys.argv[3]).read_text()).group(1))\n" +
                "elif sys.argv[1]=='install':\n" +
                " (root/'install-args').write_text(' '.join(sys.argv[1:]))\n" +
                (' sys.exit(7)\n' if install_fails else
                 " binary=Path(os.environ['HOME'])/'go/bin/swag'\n binary.parent.mkdir(parents=True)\n binary.write_text(" + repr(swag_body.replace('@VERSION@', 'v1.16.6')) + ")\n binary.chmod(0o755)\n"))
            go.chmod(0o755)
            env = dict(os.environ, HOME=str(home), PATH=str(tools)+os.pathsep+'/usr/bin:/bin')
            result = subprocess.run(['bash', str(scripts / 'gen-swagger.sh'), 'system'], cwd=root,
                                    env=env, capture_output=True, text=True, timeout=15)
            install = (root / 'install-args').read_text() if (root / 'install-args').exists() else None
            generated = (root / 'generated-by').read_text().splitlines() if (root / 'generated-by').exists() else []
            return result, install, generated

    def test_missing_tool_installs_the_declared_version(self):
        result, install, generated = self.run_generation(None)
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        self.assertEqual(install, 'install github.com/swaggo/swag/cmd/swag@v1.16.6')
        self.assertEqual(len(generated), 1)
        self.assertTrue(generated[0].endswith('/home/go/bin/swag'))

    def test_outdated_path_tool_is_replaced_before_generation(self):
        result, install, generated = self.run_generation('v1.16.4')
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        self.assertEqual(install, 'install github.com/swaggo/swag/cmd/swag@v1.16.6')
        self.assertEqual(len(generated), 1)
        self.assertTrue(generated[0].endswith('/home/go/bin/swag'))

    def test_matching_tool_is_reused_without_installation(self):
        result, install, generated = self.run_generation('v1.16.6')
        self.assertEqual(result.returncode, 0, result.stdout+result.stderr)
        self.assertIsNone(install)
        self.assertEqual(len(generated), 1)
        self.assertTrue(generated[0].endswith('/tools/swag'))

    def test_failed_install_does_not_run_the_old_generator(self):
        result, install, generated = self.run_generation('v1.16.4', install_fails=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(install, 'install github.com/swaggo/swag/cmd/swag@v1.16.6')
        self.assertEqual(generated, [])


if __name__ == "__main__":
    unittest.main()
