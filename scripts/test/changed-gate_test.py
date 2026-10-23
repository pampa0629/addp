#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("changed-gate.py")
SPEC = importlib.util.spec_from_file_location("changed_gate", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class ChangedGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        files = {
            "common/go.mod": "module github.com/addp/common\n",
            "sample/backend/go.mod": (
                "module example.com/sample\nrequire github.com/addp/common v0.0.0\n"
            ),
            "sample/frontend/package.json": (
                '{"dependencies":{"@addp/common-frontend":"file:../../common-frontend"}}\n'
            ),
            "other/frontend/package.json": "{}\n",
            "alias/frontend/package.json": "{}\n",
            "alias/frontend/vite.config.js": (
                "resolve(__dirname, '../../common-frontend/basic/src')\n"
            ),
            "common-python/pyproject.toml": "[project]\nname='common-python'\n",
            "agent/backend/requirements.txt": "-e ../../common-python\n",
            "agent/frontend/package.json": "{}\n",
            "Makefile": (
                "test-platform:\n\t@true\n"
                "test-sample-frontend:\n\t@true\n"
                "test-other-frontend:\n\t@true\n"
                "test-agent-eval: test-agent-frontend\n\t@true\n"
                "test-agent-frontend:\n\t@true\n"
                "test-common-python:\n\t@true\n"
            ),
        }
        for relative_path, content in files.items():
            path = self.repository / relative_path
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content, encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(
            ["git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "fixture"],
            cwd=self.repository,
            check=True,
        )

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def test_common_changes_expand_to_go_consumers(self) -> None:
        self.assertEqual(
            ["common", "sample"],
            MODULE.affected_modules(self.repository, ["common/client/system.go"]),
        )

    def test_common_python_health_changes_select_importing_runtime_owner(self) -> None:
        for name, content in {
            "engines/workflow/requirements.txt": "Flask==3.0.0\n",
            "engines/workflow/api_server.py": "from addp_common.workflow_runtime.health import runtime_health\n",
            "engines/runtime/CMakeLists.txt": "cmake_minimum_required(VERSION 3.20)\n",
        }.items():
            path = self.repository / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        self.assertEqual(["agent", "common-python", "engines"], MODULE.affected_modules(
            self.repository, ["common-python/addp_common/workflow_runtime/health.py"]))

    def test_shared_browser_launcher_selects_its_package_consumers(self) -> None:
        package = self.repository / 'other/frontend/package.json'
        package.write_text('{"scripts":{"test:e2e":"node ../../scripts/test/frontend-browser-gate.mjs"}}')
        self.assertEqual(['other'], MODULE.affected_modules(self.repository, ['scripts/test/frontend-browser-gate.mjs']))

    def test_raster_physical_fixture_and_scene_changes_select_native_engine_gate(self) -> None:
        module = self.repository / 'engines/duckdb/go.mod'
        module.parent.mkdir(parents=True)
        module.write_text('module example.com/duckdb\n')
        subprocess.run(['git', 'add', '.'], cwd=self.repository, check=True)
        for path in ('business/scripts/online-raster-disk-pressure-fixture.py','scripts/test/raster-disk-pressure-online.py','business/scripts/online-raster-band-recommendation-fixture.py','scripts/test/raster-band-recommendation-workflow-online.py','business/scripts/online-raster-nibble-capacity-fixture.py','scripts/test/raster-nibble-capacity-online.py','business/scripts/online-raster-nibble-fixture.py','scripts/test/raster-nibble-persistence-online.py','business/scripts/online-raster-fill-depressions-fixture.py','business/scripts/online-raster-flow-direction-d8-fixture.py', 'business/scripts/online-raster-flow-accumulation-d8-fixture.py', 'business/scripts/online-raster-drainage-basins-d8-fixture.py', 'business/scripts/online-raster-flow-length-d8-fixture.py', 'business/scripts/online-raster-flow-length-upstream-d8-fixture.py', 'business/scripts/online-raster-stream-order-d8-fixture.py','scripts/test/raster-fill-depressions-persistence-online.py','scripts/test/raster-flow-direction-d8-persistence-online.py', 'scripts/test/raster-flow-accumulation-d8-persistence-online.py', 'scripts/test/raster-drainage-basins-d8-persistence-online.py', 'scripts/test/raster-flow-length-d8-persistence-online.py', 'scripts/test/raster-flow-length-upstream-d8-persistence-online.py', 'scripts/test/raster-stream-order-d8-persistence-online.py','business/scripts/online-raster-thin-fixture.py','scripts/test/raster-thin-persistence-online.py','business/scripts/online-raster-boundary-clean-fixture.py','scripts/test/raster-boundary-clean-persistence-online.py','business/scripts/online-raster-focal-mode-fixture.py','scripts/test/raster-focal-mode-persistence-online.py','business/scripts/online-vector-zonal-statistics-fixture.py','scripts/test/vector-zonal-statistics-persistence-online.py','business/scripts/online-raster-proximity-fixture.py','scripts/test/raster-proximity-persistence-online.py','business/scripts/online-raster-focal-statistics-fixture.py','scripts/test/raster-focal-statistics-persistence-online.py','business/scripts/online-raster-zonal-statistics-fixture.py','scripts/test/raster-zonal-statistics-persistence-online.py','business/scripts/online-vector-rasterize-fixture.py','scripts/test/vector-rasterize-persistence-online.py','business/scripts/online-raster-polygonize-fixture.py', 'scripts/test/raster-polygonize-persistence-online.py',
                     'scripts/utils/online_vector_persistence.py', 'scripts/utils/online_nfs_fixture.py', 'business/scripts/online-raster-contour-fixture.py', 'scripts/test/raster-contour-persistence-online.py',
                     'business/scripts/online-raster-minio-fixture.py', 'scripts/test/raster-workflow-online.py',
                     'business/scripts/online-raster-capacity-fixture.py', 'scripts/test/raster-workflow-capacity-online.py',
                     'scripts/test/vector-topology-capacity.py', 'scripts/test/vector-topology-capacity_test.py', 'scripts/utils/capacity_resources.py'):
            with self.subTest(path=path):
                self.assertEqual(['engines'], MODULE.affected_modules(self.repository, [path]))
        self.assertEqual([], MODULE.affected_modules(self.repository, ['business/scripts/unrelated.py']))

    def test_changed_platform_uses_the_same_t2_environment_isolation(self) -> None:
        step = MODULE.plan_changed(self.repository, ["sample/backend/example.go"])[0]
        self.assertEqual(MODULE.MODULE_GATE.platform_step(self.repository), step)
        environment = MODULE.MODULE_GATE.step_environment(step, {
            "ADDP_SYSTEM_POSTGRES_TEST_DSN": "postgres://test",
            "ADDP_POSTGRES_INTEGRATION": "1",
            "UNRELATED_TEST_FLAG": "kept",
        })
        self.assertEqual({"UNRELATED_TEST_FLAG": "kept"}, environment)

    def test_owned_compose_change_selects_declared_gate_owner(self) -> None:
        path = self.repository / "scripts/test/sample-graph-gate.sh"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("# ADDP_T2_OWNED_SERVICES=graph\n# ADDP_T2_COMPOSE_FILE=scripts/test/isolated.yml\n")
        self.assertEqual(["sample"], MODULE.affected_modules(self.repository, ["scripts/test/isolated.yml"]))

    def test_declared_external_inputs_select_gate_owner(self) -> None:
        path = self.repository / "scripts/test/sample-graph-gate.sh"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("# ADDP_T2_INPUT_FILES=docker-compose.infra.yml .env.example\n")
        self.assertEqual(["sample"], MODULE.affected_modules(self.repository, ["docker-compose.infra.yml"]))
        self.assertEqual(["sample"], MODULE.affected_modules(self.repository, [".env.example"]))

    def test_declared_subtree_selects_owner_for_deleted_input_without_prefix_collision(self) -> None:
        path = self.repository / "scripts/test/sample-graph-gate.sh"
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("# ADDP_T2_INPUT_FILES=shared-input/\n")
        self.assertEqual(["sample"], MODULE.affected_modules(self.repository, ["shared-input/deleted.txt"]))
        self.assertEqual([], MODULE.affected_modules(self.repository, ["shared-input-other/other.txt"]))

    def test_orchestrator_changes_include_quality_reference_gate(self) -> None:
        for name in ("quality", "orchestrator"):
            path = self.repository / name / "backend" / "go.mod"
            path.parent.mkdir(parents=True)
            path.write_text("module example.com/" + name + "\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        self.assertEqual(
            ["orchestrator", "quality"],
            MODULE.affected_modules(self.repository, ["orchestrator/backend/migrations/005_quality_plan_references.sql"]),
        )

    def test_common_frontend_changes_expand_to_package_and_alias_consumers(self) -> None:
        self.assertEqual(
            ["alias", "sample"],
            MODULE.affected_modules(self.repository, ["common-frontend/basic/index.js"]),
        )

    def test_service_frontend_changes_include_console_query_editor_gate(self) -> None:
        for name in ("console", "service"):
            path = self.repository / name / "frontend" / "package.json"
            path.parent.mkdir(parents=True)
            path.write_text("{}\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        for path in ("service/frontend/src/views/QueryServiceForm.vue", "service/frontend/e2e/query-form.js", "service/frontend/package-lock.json"):
            self.assertEqual(["console", "service"], MODULE.affected_modules(self.repository, [path]))
        self.assertEqual(["service"], MODULE.affected_modules(self.repository, ["service/backend/internal/service/query.go"]))

    def test_catalog_frontend_changes_include_console_coverage_navigation_gate(self) -> None:
        for name in ("catalog", "console"):
            path = self.repository / name / "frontend" / "package.json"
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("{}\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        for path in ("catalog/frontend/src/views/GovernanceCoverage.vue", "catalog/frontend/package-lock.json"):
            self.assertEqual(["catalog", "console"], MODULE.affected_modules(self.repository, [path]))
        self.assertEqual(["catalog"], MODULE.affected_modules(self.repository, ["catalog/backend/internal/service/governance_coverage.go"]))

    def test_system_frontend_changes_include_console_module_query_gate(self) -> None:
        for name in ("system", "console"):
            path = self.repository / name / "frontend" / "package.json"
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("{}\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        for path in ("system/frontend/src/views/Modules.vue", "system/frontend/package-lock.json"):
            self.assertEqual(["console", "system"], MODULE.affected_modules(self.repository, [path]))
        self.assertEqual(["system"], MODULE.affected_modules(self.repository, ["system/backend/internal/service/module.go"]))

    def test_security_frontend_changes_include_console_authorization_refresh_gate(self) -> None:
        for name in ("security", "console"):
            path = self.repository / name / "frontend" / "package.json"
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("{}\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        for path in ("security/frontend/src/views/ProtectionEnrollmentList.vue", "security/frontend/package-lock.json"):
            self.assertEqual(["console", "security"], MODULE.affected_modules(self.repository, [path]))
        self.assertEqual(["security"], MODULE.affected_modules(self.repository, ["security/backend/internal/service/enrollment.go"]))

    def test_monitor_frontend_changes_include_console_target_navigation_gate(self) -> None:
        for name in ("monitor", "console"):
            path = self.repository / name / "frontend/package.json"
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("{}\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        for path in ("monitor/frontend/src/views/MonitoringTargets.vue", "monitor/frontend/e2e/monitoring-targets.fixture.js", "monitor/frontend/package-lock.json"):
            self.assertEqual(["console", "monitor"], MODULE.affected_modules(self.repository, [path]))
        self.assertEqual(["monitor"], MODULE.affected_modules(self.repository, ["monitor/backend/internal/service/resource_observation.go"]))

    def test_graph_frontend_changes_include_console_ontology_navigation_gate(self) -> None:
        for name in ("graph", "console"):
            path = self.repository / name / "frontend/package.json"
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("{}\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        for path in ("graph/frontend/src/views/OntologyDetail.vue", "graph/frontend/package-lock.json"):
            self.assertEqual(["console", "graph"], MODULE.affected_modules(self.repository, [path]))
        self.assertEqual(["graph"], MODULE.affected_modules(self.repository, ["graph/backend/internal/service/ontology.go"]))

    def test_console_delivery_route_changes_include_workbench_browser_gate(self) -> None:
        console_manifest = self.repository / "console/frontend/package.json"
        console_manifest.parent.mkdir(parents=True)
        console_manifest.write_text("{}\n", encoding="utf-8")
        path = self.repository / "workbench/frontend/package.json"
        path.parent.mkdir(parents=True)
        path.write_text("{}\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        for changed in (
            "console/frontend/vite.config.js",
            "console/frontend/src/config/portalConfig.js",
            "console/frontend/package-lock.json",
        ):
            with self.subTest(path=changed):
                self.assertEqual(["console", "workbench"], MODULE.affected_modules(self.repository, [changed]))

    def test_consumer_scan_skips_tracked_files_deleted_from_worktree(self) -> None:
        (self.repository / "sample/frontend/package.json").unlink()
        self.assertEqual(
            ["alias"],
            MODULE.affected_modules(self.repository, ["common-frontend/basic/index.js"]),
        )

    def test_common_python_changes_expand_to_registered_consumers(self) -> None:
        self.assertEqual(
            ["agent", "common-python"],
            MODULE.affected_modules(self.repository, ["common-python/addp_common/client.py"]),
        )

    def test_platform_skill_changes_select_agent_gate(self) -> None:
        for path in (
            "skills/workflow-analysis/SKILL.md",
            "skills/workflow-analysis/references/workflow-contract.md",
            "skills/workflow-analysis/agents/addp.yaml",
        ):
            with self.subTest(path=path):
                self.assertEqual(
                    ["agent"], MODULE.affected_modules(self.repository, [path])
                )
                commands = [step.command for step in MODULE.plan_changed(self.repository, [path])]
                self.assertIn(("make", "test-agent-eval"), commands)

    def test_instruction_docs_do_not_select_agent_runtime(self) -> None:
        self.assertEqual(
            [],
            MODULE.affected_modules(
                self.repository, ["AGENTS.md", "docs/skills/addp-Skill规范.md"]
            ),
        )

    def test_evaluation_scenarios_and_gate_scripts_map_to_owner(self) -> None:
        self.assertEqual(
            ["agent"],
            MODULE.affected_modules(
                self.repository,
                ["evals/agent-scenarios/registry.json"],
            ),
        )

        gate = self.repository / "scripts/test/sample-postgres-gate.sh"
        gate.parent.mkdir(parents=True, exist_ok=True)
        gate.write_text("#!/usr/bin/env bash\n", encoding="utf-8")
        subprocess.run(["git", "add", str(gate)], cwd=self.repository, check=True)
        self.assertEqual(
            ["sample"],
            MODULE.affected_modules(
                self.repository,
                ["scripts/test/sample-postgres-gate.sh"],
            ),
        )

        mysql_gate = self.repository / "scripts/test/common-mysql-data-protection-gate.sh"
        mysql_gate.write_text("#!/usr/bin/env bash\n", encoding="utf-8")
        self.assertEqual(
            ["common"],
            MODULE.affected_modules(
                self.repository,
                ["scripts/test/common-mysql-data-protection-gate.sh"],
            ),
        )

    def test_gate_control_changes_select_all_registered_modules(self) -> None:
        self.assertEqual(
            ["agent", "alias", "common", "common-python", "other", "sample"],
            MODULE.affected_modules(self.repository, [".github/workflows/platform-ci.yml"]),
        )

    def test_changed_files_between_uses_merge_base_range(self) -> None:
        base = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            cwd=self.repository,
            check=True,
            capture_output=True,
            text=True,
        ).stdout.strip()
        (self.repository / "sample/new.go").write_text("package sample\n", encoding="utf-8")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(
            ["git", "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "change"],
            cwd=self.repository,
            check=True,
        )
        self.assertEqual(
            ["sample/new.go"],
            MODULE.changed_files_between(self.repository, base, "HEAD"),
        )

    def test_changed_files_include_tracked_and_untracked_worktree_changes(self) -> None:
        (self.repository / "sample/backend/go.mod").write_text("changed\n", encoding="utf-8")
        untracked = self.repository / "other/frontend/new.js"
        untracked.write_text("new\n", encoding="utf-8")
        self.assertEqual(
            ["other/frontend/new.js", "sample/backend/go.mod"],
            MODULE.changed_files(self.repository, None),
        )


if __name__ == "__main__":
    unittest.main()
