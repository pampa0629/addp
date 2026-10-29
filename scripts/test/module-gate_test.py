#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import fcntl
import json
import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).with_name("module-gate.py")
SPEC = importlib.util.spec_from_file_location("module_gate", SCRIPT)
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)

COMMON_POSTGRES_SUCCESS_OUTPUT = (
    "--- PASS: TestProjectionStoreSchemaContractAgainstPostgres (0.01s)\n"
    "--- PASS: TestIntegrationHighGoTableWrite (0.01s)\n"
    "--- PASS: TestIntegrationPostgresKMLWriter (0.01s)\n"
    "--- PASS: TestIntegrationPostgresGraphTargetContract (0.01s)\n"
    "--- PASS: TestIntegrationResolvePostgresQueryOutputRewrite (0.01s)\n"
    "--- PASS: TestExecutionRetentionAssessmentAgainstPostgres (0.01s)\n"
    "--- PASS: TestExecutionLifecycleAgainstPostgres (0.01s)\n"
    "--- PASS: TestPostgresCleanupProfessionalResultAtomicity (0.01s)\n"
    "--- PASS: TestIntegrationPostgresGraphCleanupRuntime (0.01s)\n"
    "PASS"
)


class ModuleGateTest(unittest.TestCase):
    def test_go_only_selects_existing_go_steps_without_t2_preflight(self) -> None:
        steps = MODULE.plan_module(self.repository, "sample", go_only=True)
        self.assertEqual([step.command for step in steps], [("go", "test", "./...")])
        self.assertEqual(steps[0].excluded_environment, MODULE.GO_T1_EXCLUDED_ENVIRONMENT)
        MODULE.preflight_required_environment(steps, {})

    def test_go_only_refuses_non_go_owner(self) -> None:
        (self.repository / "sample/backend/go.mod").unlink()
        with self.assertRaisesRegex(MODULE.ModuleGateError, "no Go T1"):
            MODULE.plan_module(self.repository, "sample", go_only=True)

    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.repository = Path(self.temporary_directory.name)
        subprocess.run(["git", "init", "-q"], cwd=self.repository, check=True)
        for relative_path in (
            "sample/backend/go.mod",
            "sample/frontend/package.json",
            "scripts/test/sample-mongodb-security-gate.sh",
            "scripts/test/sample-postgres-gate.sh",
        ):
            path = self.repository / relative_path
            path.parent.mkdir(parents=True, exist_ok=True)
            content = (
                "#!/usr/bin/env bash\n# ADDP_T2_SERVICES=mongodb\n"
                if "mongodb" in relative_path
                else (
                    "#!/usr/bin/env bash\n"
                    "# ADDP_T2_SERVICES=postgres\n"
                    "# ADDP_T2_REQUIRED_ENV=SAMPLE_POSTGRES_TEST_DSN\n"
                )
                if relative_path.endswith("-gate.sh")
                else "{}\n"
            )
            path.write_text(content, encoding="utf-8")
        (self.repository / "Makefile").write_text(
            "test-platform:\n\t@true\n"
            "test-sample-eval:\n\t@true\n"
            "test-sample-frontend:\n\t@true\n"
            "test-sample-mongodb-security:\n\t@true\n"
            "test-sample-postgres:\n\t@true\n",
            encoding="utf-8",
        )
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def test_discovers_registered_nested_python_runtime(self) -> None:
        requirements = self.repository / "sample" / "runtime" / "requirements.txt"
        requirements.parent.mkdir(parents=True)
        requirements.write_text("pytest\n", encoding="utf-8")
        makefile = self.repository / "Makefile"
        makefile.write_text(makefile.read_text() + "test-runtime:\n\t@true\n")

        steps = MODULE.plan_module(self.repository, "sample")

        self.assertIn(("make", "test-runtime"), [step.command for step in steps])

    def test_cmake_runtime_requires_registered_standard_entry(self) -> None:
        manifest = self.repository / "native" / "runtime" / "CMakeLists.txt"
        manifest.parent.mkdir(parents=True)
        manifest.write_text("project(sample LANGUAGES CXX)\n", encoding="utf-8")
        with self.assertRaisesRegex(MODULE.ModuleGateError, "test-runtime is missing"):
            MODULE.plan_module(self.repository, "native")
        makefile = self.repository / "Makefile"
        makefile.write_text(makefile.read_text() + "test-runtime:\n\t@true\n")
        steps = MODULE.plan_module(self.repository, "native", include_platform=False)
        self.assertEqual([step.command for step in steps], [("make", "test-runtime")])

    def test_discovers_owner_with_only_an_owned_service_gate(self) -> None:
        gate = self.repository / 'scripts/test/business-redis-gate.sh'
        gate.write_text('#!/bin/bash\n# ADDP_T2_OWNED_SERVICES=redis\n')
        makefile = self.repository / 'Makefile'
        makefile.write_text(makefile.read_text() + 'test-business-redis:\n\t@true\n')
        self.assertIn('business', MODULE.discover_modules(self.repository))
        steps = MODULE.plan_module(self.repository, 'business', include_platform=False)
        self.assertEqual([step.command for step in steps], [('make', 'test-business-redis')])

    def test_plans_platform_and_discovered_module_gates(self) -> None:
        steps = MODULE.plan_module(self.repository, "sample")
        self.assertEqual(
            [
                ("make", "test-platform"),
                ("go", "test", "./..."),
                ("make", "test-sample-eval"),
                ("make", "test-sample-frontend"),
                ("make", "test-sample-mongodb-security"),
                ("make", "test-sample-postgres"),
            ],
            [step.command for step in steps],
        )
        self.assertEqual((("GOWORK", "off"),), steps[1].environment)
        self.assertEqual(
            ("*_POSTGRES_TEST_DSN", "ADDP_*_INTEGRATION", "ADDP_OBSERVABILITY_TRACES_ENABLED",
             "ADDP_TRACE_DEPLOYMENTS", "MONITOR_TEMPO_URL", "MONITOR_TRACE_TLS_DIR"),
            steps[1].excluded_environment,
        )
        self.assertEqual((), steps[-2].required_environment)
        self.assertEqual(
            (("SAMPLE_POSTGRES_TEST_DSN",),),
            steps[-1].required_environment,
        )

    def test_preflights_all_required_t2_environment_before_running_any_step(self) -> None:
        steps = MODULE.plan_module(self.repository, "sample")

        with mock.patch.object(MODULE.subprocess, "run") as run:
            with self.assertRaisesRegex(
                MODULE.ModuleGateError,
                "test-sample-postgres: SAMPLE_POSTGRES_TEST_DSN",
            ):
                MODULE.run_steps(steps, dry_run=False, base_environment={})

        run.assert_not_called()

    def test_postgres_preflight_points_to_actual_infra_mapping(self) -> None:
        steps = MODULE.plan_module(self.repository, "sample")

        with self.assertRaises(MODULE.ModuleGateError) as failure:
            MODULE.preflight_required_environment(steps, {})

        message = str(failure.exception)
        self.assertIn("bash scripts/infra/status.sh", message)
        self.assertIn("addp-postgres", message)
        self.assertNotIn("127.0.0.1:15432", message)

    def test_shared_postgres_gates_refuse_implicit_port_before_running_tests(self) -> None:
        repository = SCRIPT.parents[2]
        environment = os.environ.copy()
        environment.pop("ADDP_TEST_POSTGRES_PORT", None)
        environment.pop("ADDP_TEST_POSTGRES_PASSWORD", None)
        for owner in ("common", "quality", "transfer"):
            with self.subTest(owner=owner):
                gate = repository / f"scripts/test/{owner}-postgres-gate.sh"
                declaration = MODULE.required_t2_environment(gate.read_text())
                self.assertEqual(
                    (("ADDP_TEST_POSTGRES_PORT",), ("ADDP_TEST_POSTGRES_PASSWORD",)),
                    declaration,
                )
                result = subprocess.run(
                    ["bash", str(gate)],
                    cwd=repository,
                    env=environment,
                    capture_output=True,
                    text=True,
                    timeout=10,
                    check=False,
                )
                self.assertNotEqual(0, result.returncode)
                self.assertIn("ADDP_TEST_POSTGRES_PORT", result.stderr)
                self.assertNotIn("=== RUN", result.stdout)

    def test_discovers_owner_managed_disposable_services_without_hosted_registration(self) -> None:
        path = self.repository / "scripts/test/sample-falkor-gate.sh"
        path.write_text("# ADDP_T2_OWNED_SERVICES=falkordb\n", encoding="utf-8")
        makefile = self.repository / "Makefile"
        makefile.write_text(makefile.read_text() + "test-sample-falkor:\n\t@true\n")
        self.assertNotIn(str(path.relative_to(self.repository)), MODULE.hosted_t2_scripts(self.repository))
        commands = [step.command for step in MODULE.plan_module(self.repository, "sample")]
        self.assertEqual(commands.count(("make", "test-sample-falkor")), 1)

    def _run_common_postgres_group(self, arguments: list[str], output: str = COMMON_POSTGRES_SUCCESS_OUTPUT, extra_env: dict[str, str] | None = None) -> tuple[subprocess.CompletedProcess[str], list[str]]:
        repository = SCRIPT.parents[2]
        with tempfile.TemporaryDirectory(dir=self.repository) as directory:
            fixture = Path(directory)
            calls = fixture / "go-calls"
            go = fixture / "go"
            go.write_text(
                '#!/usr/bin/env bash\n'
                'printf "%s\\n" "$*" >> "$ADDP_GATE_TEST_GO_CALLS"\n'
                'if [[ "$2" == "./execution" && "${ADDP_GATE_TEST_EXECUTION_OUTPUT+present}" == "present" ]]; then\n'
                '  printf "%s\\n" "$ADDP_GATE_TEST_EXECUTION_OUTPUT"\n'
                'else\n'
                '  printf "%s\\n" "$ADDP_GATE_TEST_GO_OUTPUT"\n'
                'fi\n',
                encoding="utf-8",
            )
            go.chmod(0o755)
            environment = os.environ.copy()
            environment.update({
                "PATH": str(fixture) + os.pathsep + environment["PATH"],
                # Only mocked Go executes; no database or Docker operation is performed.
                "GITHUB_ACTIONS": "true",
                "ADDP_TEST_POSTGRES_HOST": "127.0.0.1",
                "ADDP_TEST_POSTGRES_PORT": "5432",
                "ADDP_TEST_POSTGRES_DATABASE": "addp_test",
                "ADDP_TEST_POSTGRES_PASSWORD": "fixture-password",
                "ADDP_GATE_TEST_GO_CALLS": str(calls),
                "ADDP_GATE_TEST_GO_OUTPUT": output,
            })
            environment.pop("ADDP_TEST_EXECUTION_POSTGRES_DSN", None)
            environment.pop("ADDP_GATE_TEST_EXECUTION_OUTPUT", None)
            for name in tuple(environment):
                if name.startswith("ADDP_TEST_HIGHGO_"):
                    environment.pop(name)
            environment.update(extra_env or {})
            result = subprocess.run(
                ["bash", str(repository / "scripts/test/common-postgres-gate.sh"), *arguments],
                cwd=repository, env=environment, capture_output=True, text=True,
                timeout=10, check=False,
            )
            return result, calls.read_text().splitlines() if calls.exists() else []

    def test_common_postgres_group_preserves_full_default_and_scopes_explicitly(self) -> None:
        result, calls = self._run_common_postgres_group([])
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(8, len(calls))
        self.assertIn("TestIntegrationPostgresCatalogPreciseReadOnly", calls[0])
        self.assertIn("TestIntegrationPostgresAnalyticalArithmetic", calls[0])
        self.assertIn("TestIntegrationResolvePostgresQuery(ReadSet|OutputLineage|OutputRewrite)", calls[0])
        self.assertIn("TestIntegrationPostgresQueryReadSession", calls[0])
        self.assertIn("./engine/plugins/highgo", calls[0])
        self.assertIn("./format/plugins/kml", calls[0])
        self.assertIn("TestIntegrationPostgresKMLWriter", calls[0])
        self.assertIn("TestIntegrationHighGoTableWrite", calls[0])
        self.assertIn("./execution", calls[1])
        self.assertEqual("test ./execution -run ^TestExecution(AuthorizationFactsMigration|Events|EventRetention|RetentionAssessment|Lifecycle)AgainstPostgres$ -count=1 -v", calls[1])
        self.assertEqual("test ./cleanup -run ^TestPostgresCleanupProfessionalResultAtomicity$ -count=1 -v", calls[2])
        self.assertIn("./exportartifact", calls[3])
        self.assertIn("TestExportExecutionSourceAgainstPostgres", calls[3])
        self.assertIn("./dataprotection/projectionstore", calls[4])
        self.assertIn("./dbbridge", calls[5])
        self.assertIn("./internal/repository -run ^TestIntegrationPostgresGraph", calls[6])
        self.assertEqual("test ./internal/service -run ^TestIntegrationPostgresGraphCleanupRuntime$ -count=1 -v", calls[7])


        result, calls = self._run_common_postgres_group(["--test", "query-read-set"])
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(1, len(calls))
        self.assertIn("-run ^(TestIntegrationResolvePostgresQuery(ReadSet|OutputLineage|OutputRewrite)|TestIntegrationPostgresQueryReadSession)", calls[0])
        self.assertNotIn("TestIntegrationPostgresAnalyticalArithmetic", calls[0])
        self.assertIn("query-read-set", result.stdout)

    def test_common_postgres_explicit_all_keeps_full_default(self) -> None:
        default, default_calls = self._run_common_postgres_group([])
        explicit, explicit_calls = self._run_common_postgres_group(["--test", "all"])
        self.assertEqual(0, default.returncode, default.stderr)
        self.assertEqual(0, explicit.returncode, explicit.stderr)
        self.assertEqual(8, len(explicit_calls))
        self.assertEqual(default_calls, explicit_calls)

    def test_common_postgres_retention_group_is_exactly_scoped(self) -> None:
        result, calls = self._run_common_postgres_group(["--test", "execution-retention"])
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(["test ./execution -run ^TestExecutionRetentionAssessmentAgainstPostgres$ -count=1 -v"], calls)
        self.assertIn("read-only group passed (not the full PostgreSQL gate)", result.stdout)

    def test_common_postgres_retention_requires_marker_and_refuses_skips(self) -> None:
        marker = "--- PASS: TestExecutionRetentionAssessmentAgainstPostgres (0.01s)"
        for arguments, count, error in (
            ([], 2, "Common execution PostgreSQL gate"),
            (["--test", "execution-retention"], 1, "Common execution retention gate"),
        ):
            for output in (
                "PASS [no tests to run]",
                "--- PASS: TestExecutionEventRetentionAgainstPostgres (0.01s)\nPASS",
                marker + "\n--- SKIP: TestExecutionRetentionAssessmentAgainstPostgres (0.00s)\nPASS",
            ):
                with self.subTest(arguments=arguments, output=output):
                    # Only the execution call changes: the default path must
                    # reach this guard after the existing Provider group passes.
                    result, calls = self._run_common_postgres_group(arguments,
                        extra_env={"ADDP_GATE_TEST_EXECUTION_OUTPUT": output})
                    self.assertNotEqual(0, result.returncode)
                    self.assertIn(error, result.stderr)
                    self.assertEqual(count, len(calls))
                    self.assertIn("./execution", calls[-1])

    def test_common_postgres_projection_store_group_and_skip_rejection(self) -> None:
        result, calls = self._run_common_postgres_group(["--test", "projection-store"])
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(["test ./dataprotection/projectionstore -run ^TestProjectionStore.*AgainstPostgres$ -count=1 -v"], calls)
        self.assertIn("not the full PostgreSQL gate", result.stdout)
        result, _ = self._run_common_postgres_group(["--test", "projection-store"], "--- SKIP: fixture")
        self.assertNotEqual(0, result.returncode)
        self.assertIn("refuses skipped tests", result.stderr)
        result, _ = self._run_common_postgres_group(["--test", "projection-store"], "PASS [no tests to run]")
        self.assertNotEqual(0, result.returncode)
        self.assertIn("requires the real schema and migration contract test", result.stderr)

    def test_common_postgres_requires_output_rewrite_equivalence_in_full_and_read_groups(self) -> None:
        for arguments in ([], ["--test", "query-read-set"]):
            with self.subTest(arguments=arguments):
                result, _ = self._run_common_postgres_group(arguments, "--- PASS: TestIntegrationHighGoTableWrite (0.01s)\nPASS")
                self.assertNotEqual(0, result.returncode)
                self.assertIn("requires the real output rewrite equivalence test", result.stderr)

    def test_common_postgres_source_checkout_keeps_the_same_registered_test(self) -> None:
        result, calls = self._run_common_postgres_group(["--source-root", str(SCRIPT.parents[2]), "--test", "query-read-set"])
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(1, len(calls))
        self.assertIn("Query(ReadSet|OutputLineage|OutputRewrite)", calls[0])

    def test_common_postgres_source_checkout_rejects_missing_or_unrelated_sources(self) -> None:
        result, calls = self._run_common_postgres_group(["--source-root"])
        self.assertEqual(2, result.returncode)
        self.assertEqual([], calls)
        result, calls = self._run_common_postgres_group(["--source-root", str(self.repository)])
        self.assertNotEqual(0, result.returncode)
        self.assertEqual([], calls)

    def test_meta_postgres_source_checkout_rejects_before_database_access(self) -> None:
        repository = SCRIPT.parents[2]
        environment = dict(os.environ)
        environment.pop("META_POSTGRES_TEST_DSN", None)
        for arguments, expected in ((["--source-root"], "requires a checkout path"),
                                    (["--source-root", str(self.repository)], "must have a source commit"),
                                    (["--source-root", str(repository), "--test", "authorization-facts"], "META_POSTGRES_TEST_DSN must reference")):
            with self.subTest(arguments=arguments):
                result = subprocess.run(["bash", str(repository / "scripts/test/meta-postgres-gate.sh"), *arguments],
                    cwd=repository, env=environment, capture_output=True, text=True, timeout=10)
                self.assertNotEqual(0, result.returncode)
                self.assertIn(expected, result.stderr)

        module = self.repository / "meta/backend/go.mod"
        module.parent.mkdir(parents=True)
        module.write_text("module github.com/addp/meta\n")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(["git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "fixture"], cwd=self.repository, check=True)
        result = subprocess.run(["bash", str(repository / "scripts/test/meta-postgres-gate.sh"), "--source-root", str(self.repository)],
            cwd=repository, env=environment, capture_output=True, text=True, timeout=10)
        self.assertNotEqual(0, result.returncode)
        self.assertIn("commit belongs to this repository", result.stderr)

    def test_meta_postgres_source_checkout_preserves_infra_owner(self) -> None:
        source = (SCRIPT.parents[2] / "scripts/test/meta-postgres-gate.sh").read_text()
        self.assertIn('source "$ROOT_DIR/scripts/infra/ports.sh"', source)
        self.assertIn('cd "$SOURCE_ROOT/meta/backend"', source)
        self.assertIn('addp_infra_verify_test_postgres_dsn "$META_POSTGRES_TEST_DSN"', source)

    def _run_meta_postgres_group(self, arguments: list[str], mode: str = "passed"):
        # Execute the real selector/guard shell with fake Go and Infra only.
        # No connection, database or shared owner lock is touched.
        with tempfile.TemporaryDirectory(dir=self.repository) as directory:
            root = Path(directory)
            script = root / "scripts/test/meta-postgres-gate.sh"
            script.parent.mkdir(parents=True)
            source = (SCRIPT.parents[2] / "scripts/test/meta-postgres-gate.sh").read_text()
            script.write_text(source.replace("/tmp/addp-meta-postgres-gate.lock", str(root / "meta.lock")))
            module = root / "meta/backend/go.mod"
            module.parent.mkdir(parents=True)
            module.write_text("module github.com/addp/meta\n")
            infra = root / "scripts/infra/ports.sh"
            infra.parent.mkdir(parents=True)
            infra.write_text('addp_infra_verify_test_postgres_dsn() { printf "%s\\n" "$1" > "$ADDP_META_TEST_INFRA"; }\n')
            binaries = root / "bin"
            binaries.mkdir()
            go = binaries / "go"
            go.write_text('''#!/usr/bin/env python3
import json, os, re, sys
from pathlib import Path
args = sys.argv[1:]
with Path(os.environ['ADDP_META_TEST_CALLS']).open('a') as output:
    output.write(json.dumps({'args':args,'cwd':os.getcwd()})+'\\n')
package = args[1]
pattern = args[args.index('-run')+1]
catalog = {
    './internal/repository':['TestClaimedComponentsAgainstPostgres'],
    './internal/service':['TestDataItemAuthorizationFactsAgainstPostgres','TestScanPlanFactsAgainstPostgres','TestProductionBasicScanAgainstPostgres'],
    './internal/scanadapter':['TestPreciseCatalogScanAgainstPostgres'],
    './internal/scanprocessor':['TestDelimitedEncodingRefreshAgainstPostgres'],
    './internal/scanruntime':['TestObjectModelRelatedRefsAgainstPostgres','TestFTPBasicCompositionAgainstPostgres'],
    './internal/api':['TestMetaScanExecutionReadAgainstPostgres'],
}
mode = os.environ['ADDP_META_TEST_MODE']
if mode == 'failure':
    sys.exit(17)
if mode == 'empty':
    print('PASS [no tests to run]')
else:
    for name in catalog[package]:
        if re.fullmatch(pattern,name) and mode != 'omit:'+name:
            print('--- PASS: '+name+' (0.01s)')
    if mode == 'skip':
        print('--- SKIP: required fixture (0.01s)')
    print('PASS')
''')
            go.chmod(0o755)
            calls, owner = root / "calls.jsonl", root / "infra-owner"
            env = dict(os.environ, PATH=f"{binaries}:{os.environ['PATH']}",
                       META_POSTGRES_TEST_DSN="postgres://fixture/addp_test",
                       ADDP_META_TEST_CALLS=str(calls), ADDP_META_TEST_INFRA=str(owner), ADDP_META_TEST_MODE=mode)
            result = subprocess.run(["bash", str(script), *arguments], cwd=root,
                                    env=env, capture_output=True, text=True, timeout=10)
            recorded = [json.loads(row) for row in calls.read_text().splitlines()] if calls.exists() else []
            for call in recorded:
                self.assertEqual(str(module.parent.resolve()), call['cwd'])
                self.assertEqual(['-count=1','-v'], call['args'][-2:])
            return result, recorded, owner.read_text().strip() if owner.exists() else None

    def test_meta_postgres_groups_select_registered_packages_and_real_required_tests(self) -> None:
        all_packages = ['repository','service','scanadapter','scanprocessor','scanruntime','api']
        for args, packages in (([],all_packages),(['--test','all'],all_packages),
                               (['--test','authorization-facts'],['service']),
                               (['--test','file-composition'],['scanruntime'])):
            with self.subTest(arguments=args):
                result, calls, owner = self._run_meta_postgres_group(args)
                self.assertEqual(0,result.returncode,result.stderr)
                self.assertEqual([f'./internal/{name}' for name in packages],[call['args'][1] for call in calls])
                self.assertEqual('postgres://fixture/addp_test',owner)
                if packages == ['service']:
                    self.assertNotIn('TestProductionBasicScanAgainstPostgres',result.stdout)
                    self.assertIn('--- PASS: TestDataItemAuthorizationFactsAgainstPostgres ',result.stdout)
                    self.assertIn('--- PASS: TestScanPlanFactsAgainstPostgres ',result.stdout)
                if packages == ['scanruntime']:
                    self.assertNotIn('TestDataItemAuthorizationFactsAgainstPostgres',result.stdout)
                    self.assertIn('--- PASS: TestObjectModelRelatedRefsAgainstPostgres ',result.stdout)
                    self.assertIn('--- PASS: TestFTPBasicCompositionAgainstPostgres ',result.stdout)

    def test_meta_postgres_each_group_rejects_missing_required_proofs_skips_zero_matches_and_go_failure(self) -> None:
        for group in ('all','authorization-facts','file-composition'):
            required = ('TestObjectModelRelatedRefsAgainstPostgres','TestFTPBasicCompositionAgainstPostgres') if group == 'file-composition' else ('TestDataItemAuthorizationFactsAgainstPostgres','TestScanPlanFactsAgainstPostgres')
            for mode in ('skip','empty','failure',*('omit:'+name for name in required)):
                with self.subTest(group=group,mode=mode):
                    result, calls, owner = self._run_meta_postgres_group(['--test',group],mode)
                    self.assertNotEqual(0,result.returncode)
                    self.assertTrue(calls)
                    self.assertEqual('postgres://fixture/addp_test',owner)
                    if mode == 'skip':
                        self.assertIn('refuses skipped tests',result.stderr)
                    elif mode == 'failure':
                        self.assertEqual(17,result.returncode)
                    else:
                        missing = mode.removeprefix('omit:') if mode.startswith('omit:') else required[0]
                        self.assertIn(f'requires the real {missing} test',result.stderr)

    def test_meta_postgres_invalid_group_fails_before_infra_and_go(self) -> None:
        for args in (['--test'],['--test','unknown'],['--unknown']):
            with self.subTest(arguments=args):
                result, calls, owner = self._run_meta_postgres_group(args)
                self.assertEqual(2,result.returncode)
                self.assertEqual([],calls)
                self.assertIsNone(owner)

    def test_meta_postgres_cross_checkout_lock_and_child_lifetime(self) -> None:
        lock = self.repository / "meta.lock"
        marker = self.repository / "go-started"
        source = (SCRIPT.parents[2] / "scripts/test/meta-postgres-gate.sh").read_text()
        script = self.repository / "scripts/test/meta-postgres-gate.sh"
        script.write_text(source.replace("/tmp/addp-meta-postgres-gate.lock", str(lock)))
        module = self.repository / "meta/backend/go.mod"
        module.parent.mkdir(parents=True)
        module.write_text("module github.com/addp/meta\n")
        utility = self.repository / "scripts/infra/ports.sh"
        utility.parent.mkdir(parents=True)
        utility.write_text("addp_infra_verify_test_postgres_dsn() { return 0; }\n")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(["git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "fixture"], cwd=self.repository, check=True)
        other = self.repository / "other-checkout"
        subprocess.run(["git", "clone", "-q", str(self.repository), str(other)], check=True)
        binaries = self.repository / "bin"
        binaries.mkdir()
        go = binaries / "go"
        go.write_text(f'''#!/usr/bin/env python3
import fcntl, sys
from pathlib import Path
with open({str(lock)!r}, "a") as held:
    try:
        fcntl.flock(held.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        pass
    else:
        sys.exit("gate failed to retain lock in test child")
Path({str(marker)!r}).touch()
print("--- PASS: TestDataItemAuthorizationFactsAgainstPostgres (0.01s)")
print("--- PASS: TestScanPlanFactsAgainstPostgres (0.01s)")
''')
        go.chmod(0o755)
        env = dict(os.environ, PATH=f"{binaries}:{os.environ['PATH']}", META_POSTGRES_TEST_DSN="postgres://fixture/addp_test")
        with lock.open("a") as held:
            fcntl.flock(held.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            denied = subprocess.run(["bash", str(other / "scripts/test/meta-postgres-gate.sh"), "--test", "authorization-facts"], env=env, capture_output=True, text=True, timeout=10)
            self.assertNotEqual(0, denied.returncode)
            self.assertIn("Meta PostgreSQL gate is already running", denied.stderr)
            self.assertFalse(marker.exists(), "conflicting gate reached database command")
            fcntl.flock(held.fileno(), fcntl.LOCK_UN)
            accepted = subprocess.run(["bash", str(script), "--test", "authorization-facts"], env=env, capture_output=True, text=True, timeout=10)
            self.assertEqual(0, accepted.returncode, accepted.stderr)
            self.assertTrue(marker.exists())
            fcntl.flock(held.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)

    def test_manager_postgres_source_checkout_rejects_before_database_access(self) -> None:
        repository = SCRIPT.parents[2]
        environment = dict(os.environ)
        environment.pop("MANAGER_POSTGRES_TEST_DSN", None)
        for arguments, expected in ((["--source-root"], "requires a checkout path"),
                                    (["--source-root", str(self.repository)], "must have a source commit"),
                                    (["--source-root", str(repository)], "MANAGER_POSTGRES_TEST_DSN must reference")):
            with self.subTest(arguments=arguments):
                result = subprocess.run(["bash", str(repository / "scripts/test/manager-postgres-gate.sh"), *arguments],
                    cwd=repository, env=environment, capture_output=True, text=True, timeout=10)
                self.assertNotEqual(0, result.returncode)
                self.assertIn(expected, result.stderr)
        module = self.repository / "manager/backend/go.mod"
        module.parent.mkdir(parents=True)
        module.write_text("module github.com/addp/manager\n")
        subprocess.run(["git", "add", "."], cwd=self.repository, check=True)
        subprocess.run(["git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "fixture"], cwd=self.repository, check=True)
        result = subprocess.run(["bash", str(repository / "scripts/test/manager-postgres-gate.sh"), "--source-root", str(self.repository)],
            cwd=repository, env=environment, capture_output=True, text=True, timeout=10)
        self.assertNotEqual(0, result.returncode)
        self.assertIn("commit belongs to this repository", result.stderr)

    def test_manager_postgres_source_checkout_keeps_infra_owner_and_full_suite(self) -> None:
        source = (SCRIPT.parents[2] / "scripts/test/manager-postgres-gate.sh").read_text()
        self.assertIn('source "$ROOT_DIR/scripts/infra/ports.sh"', source)
        self.assertIn('addp_infra_verify_test_postgres_dsn "$MANAGER_POSTGRES_TEST_DSN"', source)
        self.assertIn('cd "$SOURCE_ROOT/manager/backend"', source)
        self.assertIn("./internal/repository ./internal/service ./internal/preview", source)
        self.assertIn("^TestIntegrationPostgresManagerExecutionReadIsolation$", source)
        self.assertIn("Manager PostgreSQL gate refuses skipped tests", source)

    def test_common_postgres_graph_scope_requires_real_target_contract_and_rejects_skips(self) -> None:
        result, calls = self._run_common_postgres_group(["--test", "graph"])
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(2, len(calls))
        self.assertIn("./internal/repository -run ^TestIntegrationPostgresGraph", calls[0])
        self.assertEqual("test ./internal/service -run ^TestIntegrationPostgresGraphCleanupRuntime$ -count=1 -v", calls[1])
        self.assertNotIn("engine/plugins", calls[0])
        self.assertIn("Graph PostgreSQL target", result.stdout)
        result, _ = self._run_common_postgres_group(["--test", "graph"], "--- SKIP: fixture")
        self.assertNotEqual(0, result.returncode)
        self.assertIn("refuses skipped tests", result.stderr)
        result, _ = self._run_common_postgres_group(["--test", "graph"], "testing: warning: no tests to run\nPASS")
        self.assertNotEqual(0, result.returncode)
        self.assertIn("requires the real target contract", result.stderr)

    def test_common_postgres_kml_export_requires_real_writer_and_cleanup(self) -> None:
        result, calls = self._run_common_postgres_group(["--test", "kml-export"])
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(1, len(calls))
        self.assertIn("./format/plugins/kml -run ^TestIntegrationPostgresKMLWriter$", calls[0])
        self.assertNotIn("./engine/plugins", calls[0])
        self.assertIn("not the full gate", result.stdout)
        for output in ("--- SKIP: fixture", "testing: warning: no tests to run\nPASS"):
            result, _ = self._run_common_postgres_group(["--test", "kml-export"], output)
            self.assertNotEqual(0, result.returncode)

    def test_common_postgres_highgo_scope_requires_explicit_native_target(self) -> None:
        result, calls = self._run_common_postgres_group(["--test", "highgo-write"])
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(1, len(calls))
        self.assertIn("./engine/plugins/highgo -run ^TestIntegrationHighGoTableWrite$", calls[0])
        result, calls = self._run_common_postgres_group(["--highgo-native"])
        self.assertEqual(2, result.returncode)
        self.assertEqual([], calls)
        native_args = ["--test", "highgo-write", "--highgo-native"]
        result, calls = self._run_common_postgres_group(native_args)
        self.assertEqual(2, result.returncode)
        self.assertIn("ADDP_TEST_HIGHGO_HOST", result.stderr)
        self.assertEqual([], calls)
        native_env = {
            "ADDP_TEST_HIGHGO_HOST": "database.example.invalid",
            "ADDP_TEST_HIGHGO_PORT": "5866",
            "ADDP_TEST_HIGHGO_DATABASE": "explicit_target",
            "ADDP_TEST_HIGHGO_USER": "fixture-user",
            "ADDP_TEST_HIGHGO_PASSWORD": "fixture-password",
        }
        result, calls = self._run_common_postgres_group(native_args, extra_env=native_env)
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(1, len(calls))
        self.assertNotIn("./engine/plugins/postgresql", calls[0])
        self.assertIn("Native HighGo isolated write group passed", result.stdout)
        result, _ = self._run_common_postgres_group(native_args, "--- SKIP: fixture", native_env)
        self.assertNotEqual(0, result.returncode)
        self.assertIn("refuses skipped tests", result.stderr)
        result, _ = self._run_common_postgres_group(native_args, "testing: warning: no tests to run\nPASS", native_env)
        self.assertNotEqual(0, result.returncode)
        self.assertIn("requires the real write contract test", result.stderr)

    def test_common_postgres_group_rejects_skips_and_unknown_groups(self) -> None:
        result, calls = self._run_common_postgres_group(["--test", "query-read-set"], "--- SKIP: fixture")
        self.assertNotEqual(0, result.returncode)
        self.assertIn("refuses skipped tests", result.stderr)
        self.assertEqual(1, len(calls))
        for arguments in (["--test", "unknown"], ["--test"], ["--unknown"]):
            with self.subTest(arguments=arguments):
                result, calls = self._run_common_postgres_group(arguments)
                self.assertEqual(2, result.returncode)
                self.assertEqual([], calls)

    def test_runs_after_required_t2_environment_is_present(self) -> None:
        steps = MODULE.plan_module(self.repository, "sample")

        with mock.patch.object(MODULE.subprocess, "run") as run:
            MODULE.run_steps(
                steps,
                dry_run=False,
                base_environment={"SAMPLE_POSTGRES_TEST_DSN": "postgres://test"},
            )

        self.assertEqual(len(steps), run.call_count)

    def test_dry_run_does_not_require_t2_environment(self) -> None:
        steps = MODULE.plan_module(self.repository, "sample")

        with mock.patch.object(MODULE.subprocess, "run") as run:
            MODULE.run_steps(steps, dry_run=True, base_environment={})

        run.assert_not_called()

    def test_rejects_invalid_required_t2_environment_declaration(self) -> None:
        with self.assertRaisesRegex(
            MODULE.ModuleGateError,
            "uppercase names separated by commas",
        ):
            MODULE.required_t2_environment(
                "# ADDP_T2_REQUIRED_ENV=sample_postgres_test_dsn\n"
            )

    def test_accepts_one_of_multiple_environment_alternatives(self) -> None:
        requirements = MODULE.required_t2_environment(
            "# ADDP_T2_REQUIRED_ENV=PASSWORD|LOCAL_SCOPE,OTHER\n"
        )
        step = MODULE.Step(
            "sample integration T2",
            ("make", "test-sample"),
            self.repository,
            required_environment=requirements,
        )

        MODULE.preflight_required_environment(
            [step], {"LOCAL_SCOPE": "1", "OTHER": "set"}
        )

    def test_discovers_untracked_module_before_first_commit(self) -> None:
        for relative_path in (
            "fresh/backend/go.mod",
            "fresh/frontend/package.json",
            "scripts/test/fresh-postgres-gate.sh",
        ):
            path = self.repository / relative_path
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(
                "#!/usr/bin/env bash\n# ADDP_T2_SERVICES=postgres\n"
                if relative_path.endswith("-gate.sh")
                else "{}\n",
                encoding="utf-8",
            )
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8")
            + "test-fresh-frontend:\n\t@true\n"
            + "test-fresh-postgres:\n\t@true\n",
            encoding="utf-8",
        )

        steps = MODULE.plan_module(self.repository, "fresh")

        self.assertEqual(
            [
                ("make", "test-platform"),
                ("go", "test", "./..."),
                ("make", "test-fresh-frontend"),
                ("make", "test-fresh-postgres"),
            ],
            [step.command for step in steps],
        )

    def test_go_t1_environment_excludes_postgres_integration_opt_ins(self) -> None:
        step = MODULE.Step(
            "sample Go T1",
            ("go", "test", "./..."),
            self.repository,
            (("GOWORK", "off"),),
            MODULE.GO_T1_EXCLUDED_ENVIRONMENT,
        )

        environment = MODULE.step_environment(
            step,
            {
                "ADDP_SYSTEM_POSTGRES_TEST_DSN": "postgres://system-test",
                "STANDARD_POSTGRES_TEST_DSN": "postgres://standard-test",
                "ADDP_POSTGRES_INTEGRATION": "1",
                "ADDP_OCEANBASE_INTEGRATION": "1",
                "UNRELATED_TEST_FLAG": "kept",
            },
        )

        self.assertNotIn("ADDP_SYSTEM_POSTGRES_TEST_DSN", environment)
        self.assertNotIn("STANDARD_POSTGRES_TEST_DSN", environment)
        self.assertNotIn("ADDP_POSTGRES_INTEGRATION", environment)
        self.assertNotIn("ADDP_OCEANBASE_INTEGRATION", environment)
        self.assertEqual("kept", environment["UNRELATED_TEST_FLAG"])
        self.assertEqual("off", environment["GOWORK"])

    def test_platform_environment_excludes_t2_opt_ins_but_t2_retains_them(self) -> None:
        steps = MODULE.plan_module(self.repository, "sample")
        base = {
            "ADDP_SYSTEM_POSTGRES_TEST_DSN": "postgres://system-test",
            "SAMPLE_POSTGRES_TEST_DSN": "postgres://sample-test",
            "ADDP_POSTGRES_INTEGRATION": "1",
            "UNRELATED_TEST_FLAG": "kept",
        }
        environment = MODULE.step_environment(steps[0], base)
        self.assertNotIn("ADDP_SYSTEM_POSTGRES_TEST_DSN", environment)
        self.assertNotIn("SAMPLE_POSTGRES_TEST_DSN", environment)
        self.assertNotIn("ADDP_POSTGRES_INTEGRATION", environment)
        self.assertEqual("kept", environment["UNRELATED_TEST_FLAG"])
        self.assertEqual(base, MODULE.step_environment(steps[-1], base))

    def test_rejects_unknown_module(self) -> None:
        with self.assertRaisesRegex(MODULE.ModuleGateError, "unknown MODULE"):
            MODULE.plan_module(self.repository, "missing")

    def test_rejects_invalid_module_name(self) -> None:
        with self.assertRaisesRegex(MODULE.ModuleGateError, "lowercase ADDP module name"):
            MODULE.plan_module(self.repository, "sample;echo")

    def test_ignores_non_t2_gate_scripts(self) -> None:
        release_script = self.repository / "scripts/test/sample-cli-release-gate.sh"
        release_script.write_text("#!/usr/bin/env bash\n", encoding="utf-8")
        makefile = self.repository / "Makefile"
        makefile.write_text(
            makefile.read_text(encoding="utf-8")
            + "test-sample-cli-release:\n\t@true\n",
            encoding="utf-8",
        )

        steps = MODULE.plan_module(self.repository, "sample")

        self.assertNotIn(
            ("make", "test-sample-cli-release"),
            [step.command for step in steps],
        )

    def test_git_files_preserves_unicode_and_spaces(self) -> None:
        path = self.repository / "sample/frontend/中文 说明.md"
        path.write_text("说明\n", encoding="utf-8")
        subprocess.run(["git", "add", str(path)], cwd=self.repository, check=True)
        self.assertIn(
            "sample/frontend/中文 说明.md",
            MODULE.git_files(self.repository, "*/frontend/*"),
        )


if __name__ == "__main__":
    unittest.main()
