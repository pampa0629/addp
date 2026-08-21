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
""",
            "docker-compose.yml": f"""services:
  manager-backend:
    image: manager
{manager_dep}  duckdb-engine:
    image: duckdb
{runtime_dep}""",
            "system/backend/cmd/server/main.go": "package main\nfunc main() {}\n",
            "system/backend/internal/config/config.go": "package config\n",
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


if __name__ == "__main__":
    unittest.main()
