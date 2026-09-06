from __future__ import annotations

import importlib.util
import tempfile
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).with_name("check-engine-startup-isolation.py")
SPEC = importlib.util.spec_from_file_location("check_engine_startup_isolation", SCRIPT_PATH)
assert SPEC and SPEC.loader
CHECKER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CHECKER)


class EngineStartupIsolationCheckTest(unittest.TestCase):
    def repository(
        self,
        *,
        manager_case: str = "START_MANAGER_BACKEND=true",
        manager_dep: str = "",
        runtime_dep: str = "",
    ) -> Path:
        root = Path(tempfile.mkdtemp(prefix="addp-engine-startup-isolation-"))
        files = {
            "scripts/dev/start.sh": f"""case $SELECTED_MODULE in
    manager)
      {manager_case}
      ;;
    duckdb)
      START_DUCKDB=true
      ;;
esac
pointcloud-workflow common-python/addp_common/module_lifecycle.py
document-workflow common-python/addp_common/module_lifecycle.py
""",
            "docker-compose.yml": f"""services:
  manager-backend:
    image: manager
{manager_dep}  duckdb-engine:
    image: duckdb
{runtime_dep}""",
            "system/backend/cmd/server/main.go": "package main\nfunc main() {}\n",
            "system/backend/internal/config/config.go": "package config\n",
            "manager/backend/cmd/server/main.go": """package main
func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()
    registrationDone := client.RegisterAndHeartbeat(ctx, request)
    <-ctx.Done()
    <-registrationDone.Done()
}
""",
            "common/client/system_service.go": "func (c *SystemServiceClient) RegisterAndHeartbeat(ctx context.Context, request *ModuleRegistrationRequest) *ModuleRegistrationLifecycle { return nil }\n",
            "scripts/dev/restart.sh": "pointcloud-workflow document-workflow common-python/addp_common/module_lifecycle.py\n",
            "engines/pointcloud-workflow/Dockerfile": "COPY common-python/addp_common/module_lifecycle.py /common-python/addp_common/module_lifecycle.py\n",
            "engines/document-workflow/Dockerfile": "COPY common-python/addp_common/module_lifecycle.py /common-python/addp_common/module_lifecycle.py\n",
            "common-python/addp_common/module_lifecycle.py": "\n",
            "Makefile": "test-document-workflow: ## test\n\t@true\n",
            ".github/workflows/platform-ci.yml": "uses: ./.github/actions/prepare-python-gate\nengines/document-workflow/requirements-dev.txt\nrun: make test-document-workflow\n",
            ".env.example": "POSTGRES_HOST=localhost\n",
        }
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content, encoding="utf-8")
        self.addCleanup(lambda: __import__("shutil").rmtree(root))
        return root

    def test_accepts_explicit_runtime_only(self) -> None:
        self.assertEqual(CHECKER.validate(self.repository()), [])

    def test_rejects_module_runtime_flag(self) -> None:
        errors = CHECKER.validate(self.repository(manager_case="START_DUCKDB=true"))
        self.assertTrue(any("case manager" in error and "START_DUCKDB" in error for error in errors), errors)

    def test_rejects_compose_runtime_dependency(self) -> None:
        dependency = "    depends_on:\n      duckdb-engine:\n        condition: service_healthy\n"
        errors = CHECKER.validate(self.repository(manager_dep=dependency))
        self.assertTrue(any("manager-backend" in error and "duckdb-engine" in error for error in errors), errors)

    def test_rejects_runtime_system_start_order_dependency(self) -> None:
        dependency = "    depends_on:\n      system-backend:\n        condition: service_healthy\n"
        errors = CHECKER.validate(self.repository(runtime_dep=dependency))
        self.assertTrue(any("duckdb-engine" in error and "System startup order" in error for error in errors), errors)

    def test_rejects_system_boot_runtime_call(self) -> None:
        root = self.repository()
        (root / "system/backend/cmd/server/main.go").write_text("RegisterBuiltinRuntime()\n", encoding="utf-8")
        errors = CHECKER.validate(root)
        self.assertTrue(any("RegisterBuiltinRuntime" in error for error in errors), errors)

    def test_rejects_background_module_registration_context(self) -> None:
        root = self.repository()
        (root / "manager/backend/cmd/server/main.go").write_text(
            """registrationDone := client.RegisterAndHeartbeat(context.Background(), request)
<-registrationDone.Done()
""",
            encoding="utf-8",
        )
        errors = CHECKER.validate(root)
        self.assertTrue(any("context.Background" in error for error in errors), errors)
        self.assertTrue(any("signal lifecycle" in error for error in errors), errors)

    def test_rejects_ignored_registration_completion(self) -> None:
        root = self.repository()
        (root / "manager/backend/cmd/server/main.go").write_text(
            """ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()
client.RegisterAndHeartbeat(ctx, request)
<-ctx.Done()
""",
            encoding="utf-8",
        )
        errors = CHECKER.validate(root)
        self.assertTrue(any("ignores" in error for error in errors), errors)

    def test_rejects_unwaited_registration_completion(self) -> None:
        root = self.repository()
        (root / "manager/backend/cmd/server/main.go").write_text(
            """ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
defer stop()
registrationDone := client.RegisterAndHeartbeat(ctx, request)
<-ctx.Done()
""",
            encoding="utf-8",
        )
        errors = CHECKER.validate(root)
        self.assertTrue(any("does not wait" in error for error in errors), errors)

    def test_rejects_pointcloud_image_missing_common_runtime_module(self) -> None:
        root = self.repository()
        (root / "engines/pointcloud-workflow/Dockerfile").write_text(
            "FROM python:3.12-slim\n",
            encoding="utf-8",
        )
        errors = CHECKER.validate(root)
        self.assertTrue(
            any("module_lifecycle.py" in error and "Dockerfile" in error for error in errors),
            errors,
        )

    def test_rejects_pointcloud_fingerprint_missing_common_runtime_module(self) -> None:
        root = self.repository()
        (root / "scripts/dev/restart.sh").write_text("#!/bin/bash\n", encoding="utf-8")
        errors = CHECKER.validate(root)
        self.assertTrue(
            any("module_lifecycle.py" in error and "restart.sh" in error for error in errors),
            errors,
        )

    def test_rejects_document_image_missing_common_runtime_module(self) -> None:
        root = self.repository()
        (root / "engines/document-workflow/Dockerfile").write_text(
            "FROM python:3.12-slim\n",
            encoding="utf-8",
        )
        errors = CHECKER.validate(root)
        self.assertTrue(
            any("document-workflow/Dockerfile" in error and "module_lifecycle.py" in error for error in errors),
            errors,
        )

    def test_rejects_missing_document_workflow_test_registration(self) -> None:
        root = self.repository()
        (root / ".github/workflows/platform-ci.yml").write_text("name: platform\n", encoding="utf-8")
        errors = CHECKER.validate(root)
        self.assertTrue(any("Document Workflow gate" in error for error in errors), errors)


if __name__ == "__main__":
    unittest.main()
