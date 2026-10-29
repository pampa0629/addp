"""The IAM database must have one destructive gate owner across checkouts."""

import os
import re
from pathlib import Path
import shutil
import shlex
import signal
import subprocess
import tempfile
import time
import unittest


# Platform T0 can run alongside builds and browser gates. Process startup is
# bounded, but is not a five-second performance requirement of the lock contract.
PROCESS_TIMEOUT_SECONDS = 30


class SystemIAMGateLockTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.lock = self.root / "iam.lock"
        self.started = self.root / "started"
        self.release = self.root / "release"
        self.processes = []
        source = Path(__file__).with_name("system-iam-postgres-gate.sh").read_text()
        self.scripts = []
        for name in ("first", "second"):
            checkout = self.root / name
            (checkout / "scripts/test").mkdir(parents=True)
            (checkout / "system/backend").mkdir(parents=True)
            script = checkout / "scripts/test/system-iam-postgres-gate.sh"
            script.write_text(source.replace("/tmp/addp-system-iam-postgres-gate.lock", str(self.lock)))
            utility = checkout / "scripts/infra/ports.sh"
            utility.parent.mkdir(parents=True)
            shutil.copyfile(Path(__file__).parents[1] / "infra/ports.sh", utility)
            log_utility = checkout / "scripts/utils/observability-env.sh"
            log_utility.parent.mkdir(parents=True)
            shutil.copyfile(Path(__file__).parents[1] / "utils/observability-env.sh", log_utility)
            (checkout / "system/backend/go.mod").write_text("module github.com/addp/system\n")
            subprocess.run(["git", "init", "-q"], cwd=checkout, check=True)
            subprocess.run(["git", "add", "."], cwd=checkout, check=True)
            subprocess.run(["git", "-c", "user.name=Gate Fixture", "-c", "user.email=gate@example.test", "commit", "-qm", "fixture"], cwd=checkout, check=True)
            self.scripts.append(script)
        bin_dir = self.root / "bin"
        bin_dir.mkdir()
        go = bin_dir / "go"
        go.write_text(f'''#!/usr/bin/env python3
import os, sys, time
from pathlib import Path
with open(os.environ["TEST_GO_CWD"], "a") as trace:
    trace.write(str(Path.cwd()) + "\\n")
with open(os.environ["TEST_GO_TRACE"], "a") as trace:
    marker = " owned-crash-recovery-enabled" if os.environ.get("ADDP_SYSTEM_DATABASE_CRASH_RECOVERY_INTEGRATION") == "1" else ""
    trace.write(" ".join(sys.argv[1:]) + marker + "\\n")
if os.environ.get("TEST_GO_HOLD") == "1" and "./internal/testsupport" in sys.argv:
    Path(os.environ["TEST_STARTED"]).touch()
    deadline = time.monotonic() + {2 * PROCESS_TIMEOUT_SECONDS}
    while not Path(os.environ["TEST_RELEASE"]).exists():
        if time.monotonic() >= deadline:
            sys.exit("test holder timed out")
        time.sleep(0.02)
if os.environ.get("TEST_GO_ORPHAN") == "1":
    Path(os.environ["ADDP_SYSTEM_DATABASE_CRASH_RECOVERY_STATE_DIR"], "crash-recovery-owner").write_text("12345678-1234-1234-1234-123456789abc")
    Path(os.environ["TEST_DOCKER_STATE"]).touch()
if os.environ.get("TEST_GO_SKIP") == "1":
    print("--- SKIP: TestIAMDatabaseCrashRecovery")
if "./internal/iam" in sys.argv:
    import re
    pattern = sys.argv[sys.argv.index("-run") + 1]
    if re.search(pattern, "TestCleanupAuthorizationAgainstPostgres"):
        result = os.environ.get("TEST_GO_CLEANUP_RESULT", "pass")
        if result == "pass":
            print("--- PASS: TestCleanupAuthorizationAgainstPostgres (0.01s)")
        elif result == "skip":
            print("--- SKIP: TestCleanupAuthorizationAgainstPostgres (0.00s)")
        elif result == "wrong":
            print("--- PASS: TestCleanupAuthorizationAgainstPostgresExtra (0.01s)")
        elif result == "fail":
            print("--- FAIL: TestCleanupAuthorizationAgainstPostgres (0.01s)")
            sys.exit(37)
sys.exit(int(os.environ.get("TEST_GO_STATUS", "0")))
''')
        go.chmod(0o755)
        docker = bin_dir / "docker"
        docker.write_text('''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
state = Path(os.environ["TEST_DOCKER_STATE"])
args = sys.argv[1:]
if args[0] == "ps":
    if state.exists(): print("a" * 64)
elif args[0] == "inspect":
    print(json.dumps({"Id": "a"*64, "Config": {"Labels": {"addp.test.iam-crash-recovery":
        "foreign" if os.environ.get("TEST_DOCKER_FOREIGN") == "1" else "12345678-1234-1234-1234-123456789abc"}},
        "Mounts": [{"Type": "volume", "Name": "b"*64, "Destination": "/var/lib/postgresql/data"}]}))
elif args[0] == "rm":
    Path(os.environ["TEST_DOCKER_REMOVED"]).write_text(" ".join(args))
    state.unlink()
elif args[:2] != ["volume", "ls"]:
    sys.exit(99)
''')
        docker.chmod(0o755)
        self.environment = dict(os.environ, PATH=f"{bin_dir}:{os.environ['PATH']}",
                                TMPDIR=str(self.root),
                                ADDP_SYSTEM_POSTGRES_TEST_DSN="postgres://fixture/addp_iam_test",
                                GITHUB_ACTIONS="true",
                                TEST_DOCKER_STATE=str(self.root / "docker-state"), TEST_DOCKER_REMOVED=str(self.root / "docker-removed"),
                                TEST_STARTED=str(self.started), TEST_RELEASE=str(self.release), TEST_GO_CWD=str(self.root / "cwd-trace"))

    def tearDown(self):
        for process in self.processes:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
            process.communicate(timeout=PROCESS_TIMEOUT_SECONDS)
        self.temporary.cleanup()

    def run_gate(self, index=1, arguments=(), **overrides):
        environment = dict(self.environment, TEST_GO_TRACE=str(self.root / f"trace-{index}"))
        environment.update(overrides)
        return subprocess.run(["bash", str(self.scripts[index]), "--package", "migration", *arguments],
                              env=environment, capture_output=True, text=True, timeout=PROCESS_TIMEOUT_SECONDS)

    def start_holder(self):
        environment = dict(self.environment, TEST_GO_TRACE=str(self.root / "trace-0"), TEST_GO_HOLD="1")
        process = subprocess.Popen(["bash", str(self.scripts[0]), "--package", "iam"],
                                   env=environment, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   text=True, start_new_session=True)
        self.processes.append(process)
        deadline = time.monotonic() + PROCESS_TIMEOUT_SECONDS
        while not self.started.exists() and time.monotonic() < deadline and process.poll() is None:
            time.sleep(0.02)
        self.assertTrue(self.started.exists(), "holder did not reach its first database command")
        return process

    def test_task_authorization_filter_selects_registered_iam_and_migration_tests(self):
        for package, pattern in [('iam', '^TestTaskAuthorizationSubjectAgainstPostgres$'),
                                 ('migration', '^Test(TransferTaskAuthorizationForwardMigration|Runner)AgainstPostgres$')]:
            result = self.run_gate(arguments=('--package', package, '--test', 'task-authorization'))
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn(f'./internal/{package} -run {pattern}', (self.root / 'trace-1').read_text())
        result = self.run_gate(arguments=('--package', 'api', '--test', 'task-authorization'))
        self.assertEqual(result.returncode, 2)

    def test_crash_recovery_owns_its_database_and_never_resets_external_dsn(self):
        result = self.run_gate(arguments=("--package", "api", "--test", "database-crash-recovery"),
                               ADDP_SYSTEM_POSTGRES_TEST_DSN="", GITHUB_ACTIONS="")
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertIn("./internal/api -run ^TestIAMDatabaseCrashRecovery$", trace)
        self.assertIn("owned-crash-recovery-enabled", trace)
        self.assertNotIn("./internal/testsupport", trace)
        result = self.run_gate(arguments=("--test", "database-crash-recovery"))
        self.assertEqual(result.returncode, 2)
        self.assertIn("requires --package api", result.stderr)

    def test_default_gate_includes_crash_recovery_before_shared_database_reset(self):
        result = self.run_gate(arguments=("--package", ""))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertLess(trace.index("^TestIAMDatabaseCrashRecovery$"), trace.index("^TestResetDisposablePostgresForGate$"))
        self.assertEqual(trace.count("owned-crash-recovery-enabled"), 1)

    def test_failed_crash_test_cleans_only_its_owned_partial_container(self):
        result = self.run_gate(arguments=("--package", "api", "--test", "database-crash-recovery"),
                               TEST_GO_ORPHAN="1", TEST_GO_STATUS="37")
        self.assertEqual(result.returncode, 37, result.stderr)
        self.assertEqual((self.root / "docker-removed").read_text(), "rm --force --volumes " + "a" * 64)
        self.assertFalse((self.root / "docker-state").exists())

    def test_crash_cleanup_rejects_foreign_container_identity(self):
        result = self.run_gate(arguments=("--package", "api", "--test", "database-crash-recovery"),
                               TEST_GO_ORPHAN="1", TEST_DOCKER_FOREIGN="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("refusing foreign", result.stderr)
        self.assertFalse((self.root / "docker-removed").exists())

    def test_crash_recovery_refuses_unexecuted_test(self):
        result = self.run_gate(arguments=("--package", "api", "--test", "database-crash-recovery"), TEST_GO_SKIP="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("refuses skipped tests", result.stderr)

    def test_unified_data_authorizer_filter_is_bound_to_migration(self):
        result = self.run_gate(arguments=("--package", "migration", "--test", "unified-data-authorizer"))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("./internal/migration -run ^Test(UnifiedDataAuthorizer(AssignedSessionRefresh|ForwardMigration)|Runner)AgainstPostgres$", (self.root / "trace-1").read_text())
        result = self.run_gate(arguments=("--package", "iam", "--test", "unified-data-authorizer"))
        self.assertEqual(result.returncode, 2)
        self.assertIn("requires --package migration", result.stderr)

    def test_execution_subject_roles_uses_existing_ea_postgres_suite(self):
        result = self.run_gate(arguments=("--package", "iam", "--test", "execution-subject-roles"))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("./internal/iam -run ^TestExecutionAuthorizationServiceAgainstPostgres$", (self.root / "trace-1").read_text())
        result = self.run_gate(arguments=("--package", "api", "--test", "execution-subject-roles"))
        self.assertEqual(result.returncode, 2)
        self.assertIn("requires --package iam", result.stderr)

    def test_cleanup_authorization_focus_runs_only_reset_and_exact_iam_test(self):
        result = self.run_gate(arguments=("--package", "iam", "--test", "cleanup-authorization"))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        commands = (self.root / "trace-1").read_text().splitlines()
        self.assertEqual(len(commands), 2, commands)
        self.assertIn("./internal/testsupport -run ^TestResetDisposablePostgresForGate$", commands[0])
        self.assertIn("./internal/iam -run ^TestCleanupAuthorizationAgainstPostgres$", commands[1])
        self.assertNotIn("owned-crash-recovery-enabled", "\n".join(commands))

    def test_cleanup_authorization_rejects_other_packages_before_database_commands(self):
        for package in ("", "api", "migration"):
            with self.subTest(package=package):
                result = self.run_gate(arguments=("--package", package, "--test", "cleanup-authorization"))
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertIn("cleanup-authorization requires --package iam", result.stderr)
                self.assertFalse((self.root / "trace-1").exists())

    def test_cleanup_authorization_requires_real_exact_test_pass(self):
        for outcome in ("missing", "wrong", "skip", "fail"):
            with self.subTest(outcome=outcome):
                result = self.run_gate(arguments=("--package", "iam", "--test", "cleanup-authorization"),
                                       TEST_GO_CLEANUP_RESULT=outcome)
                self.assertNotEqual(result.returncode, 0, result.stdout)
                if outcome in ("missing", "wrong"):
                    self.assertIn("requires the real Cleanup authorization test", result.stderr)
                elif outcome == "skip":
                    self.assertIn("refuses skipped tests", result.stderr)
                else:
                    self.assertEqual(result.returncode, 37, result.stderr)

    def test_default_iam_discovery_includes_cleanup_authorization(self):
        result = self.run_gate(arguments=("--package", ""))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        commands = (self.root / "trace-1").read_text().splitlines()
        iam = [shlex.split(line) for line in commands if line.startswith("test ./internal/iam ")]
        self.assertEqual(len(iam), 1, commands)
        self.assertEqual(iam[0][iam[0].index("-run") + 1], "AgainstPostgres$")
        self.assertIn("--- PASS: TestCleanupAuthorizationAgainstPostgres ", result.stdout)

    def test_standard_unit_publication_filter_is_bound_to_migration(self):
        result = self.run_gate(arguments=("--package", "migration", "--test", "standard-platform-units"))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("./internal/migration -run ^TestStandardPlatformUnitsForwardMigrationAgainstPostgres$", (self.root / "trace-1").read_text())
        result = self.run_gate(arguments=("--package", "iam", "--test", "standard-platform-units"))
        self.assertEqual(result.returncode, 2)

    def test_resource_alert_rule_publication_filter_is_bound_to_migration(self):
        result = self.run_gate(arguments=("--package", "migration", "--test", "resource-alert-rules"))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("./internal/migration -run ^Test(PlatformResourceAlertRulesForwardMigration|Runner)AgainstPostgres$", (self.root / "trace-1").read_text())
        result = self.run_gate(arguments=("--package", "iam", "--test", "resource-alert-rules"))
        self.assertEqual(result.returncode, 2)
        self.assertIn("requires --package migration", result.stderr)

    def test_platform_alert_publication_filter_is_bound_to_migration(self):
        result = self.run_gate(arguments=("--package", "migration", "--test", "platform-alert-management"))
        self.assertEqual(result.returncode, 0, result.stderr)
        expected = "^Test(PlatformAlertManagementForwardMigration|PlatformServiceAvailabilityRulesForwardMigration|PlatformHTTPQualityForwardMigration|PlatformTraceForwardMigration|Runner)AgainstPostgres$"
        trace = (self.root / "trace-1").read_text().splitlines()
        prefix = "test ./internal/migration -run "
        selected = [line.removeprefix(prefix).split(" ", 1)[0] for line in trace if line.startswith(prefix)]
        self.assertEqual(selected, [expected])
        pattern = re.compile(selected[0])
        for name in ("PlatformAlertManagementForwardMigration", "PlatformServiceAvailabilityRulesForwardMigration",
                     "PlatformHTTPQualityForwardMigration", "PlatformTraceForwardMigration", "Runner"):
            with self.subTest(selected=name):
                self.assertIsNotNone(pattern.search("Test" + name + "AgainstPostgres"))
        for name in ("TestPlatformResourceAlertRulesForwardMigrationAgainstPostgres",
                     "TestUnregisteredPublicationAgainstPostgres",
                     "PrefixTestPlatformServiceAvailabilityRulesForwardMigrationAgainstPostgres",
                     "TestPlatformServiceAvailabilityRulesForwardMigrationAgainstPostgresExtra",
                     "TestPlatformServiceAvailabilityRulesForwardMigration"):
            with self.subTest(rejected=name):
                self.assertIsNone(pattern.search(name))
        result = self.run_gate(arguments=("--package", "iam", "--test", "platform-alert-management"))
        self.assertEqual(result.returncode, 2)
        self.assertIn("requires --package migration", result.stderr)

    def test_concurrent_checkout_is_rejected_before_reset_and_lock_is_reusable(self):
        first = self.start_holder()
        second = self.run_gate()
        self.assertNotEqual(second.returncode, 0)
        self.assertIn("already running", second.stderr)
        self.assertFalse((self.root / "trace-1").exists())
        self.release.touch()
        stdout, stderr = first.communicate(timeout=PROCESS_TIMEOUT_SECONDS)
        self.assertEqual(first.returncode, 0, stdout + stderr)
        self.assertTrue(self.lock.exists())
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_failed_gate_releases_lock(self):
        failed = self.run_gate(TEST_GO_STATUS="1")
        self.assertNotEqual(failed.returncode, 0)
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_authorization_facts_filter_selects_only_registered_owner_tests(self):
        for package, pattern in (
            ("migration", "^Test(Meta|Catalog)AuthorizationFactsForwardMigrationAgainstPostgres$"),
            ("api", "^TestEngineAccessDelegationAgainstPostgres$"),
        ):
            result = self.run_gate(arguments=("--package", package, "--test", "authorization-facts"))
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn(f"./internal/{package} -run {pattern}", (self.root / "trace-1").read_text())
        result = self.run_gate(arguments=("--package", "iam", "--test", "authorization-facts"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package migration or api", result.stderr)

    def test_file_composition_filter_keeps_formal_grant_owner_and_rejects_other_packages(self):
        result = self.run_gate(arguments=("--package", "engineaccess", "--test", "file-composition"))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("./internal/engineaccess -run ^TestFulfillmentArbitrationAgainstPostgres$/^production_first_acceptance_and_historical_retry$/^file_composition_source_grants$", (self.root / "trace-1").read_text())
        rejected = self.run_gate(arguments=("--package", "iam", "--test", "file-composition"))
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("file-composition requires --package engineaccess", rejected.stderr)

    def test_meta_collection_filter_covers_qualification_and_forward_migration(self):
        for package, pattern in (
            ("engineaccess", "^TestFulfillmentArbitrationAgainstPostgres$/^meta_collection_qualification$"),
            ("migration", "^TestMetaCollectionQualification(Runtime|Approval)ForwardMigrationAgainstPostgres$"),
        ):
            result = self.run_gate(arguments=("--package", package, "--test", "meta-collection"))
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn(f"./internal/{package} -run {pattern}", (self.root / "trace-1").read_text())
        rejected = self.run_gate(arguments=("--package", "iam", "--test", "meta-collection"))
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("meta-collection requires --package engineaccess or migration", rejected.stderr)

    def test_manager_source_filter_covers_both_purposes_and_no_other_package(self):
        result = self.run_gate(arguments=("--package", "engineaccess", "--test", "manager-source"))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertIn("Manager_complete_source_execution_authorization", trace)
        self.assertIn("file_composition_source_grants", trace)
        self.assertIn("|ftp|nfs)", trace)
        rejected = self.run_gate(arguments=("--package", "iam", "--test", "manager-source"))
        self.assertNotEqual(rejected.returncode, 0)
        self.assertIn("manager-source requires --package engineaccess", rejected.stderr)

    def test_full_gate_sets_a_finite_timeout_for_every_package(self):
        result = self.run_gate(arguments=("--package", ""))
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = (self.root / "trace-1").read_text().splitlines()
        self.assertTrue(commands)
        for command in commands:
            arguments = shlex.split(command)
            self.assertEqual(arguments.count("-timeout=20m"), 1, command)

    def test_engine_deployments_filter_uses_declaration_and_publication_owners(self):
        for package, pattern in (("repository", "^TestEngineDeploymentDeclarationsAgainstPostgres$"), ("api", "^TestEngineDeploymentHTTPAuditAgainstPostgres$"), ("migration", "^Test(EngineDeploymentPublication|Runner)AgainstPostgres$")):
            result = self.run_gate(arguments=("--package", package, "--test", "engine-deployments"))
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn(f"./internal/{package} -run {pattern}", (self.root / "trace-1").read_text())
        result = self.run_gate(arguments=("--package", "iam", "--test", "engine-deployments"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package repository, api or migration", result.stderr)

    def test_host_nodes_filter_keeps_default_discovery_and_rejects_other_packages(self):
        result = self.run_gate(arguments=("--package", "repository", "--test", "host-nodes"))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("./internal/repository -run ^TestHostNodeIdentityVersionAndBindingAgainstPostgres$", (self.root / "trace-1").read_text())
        result = self.run_gate(arguments=("--package", "iam", "--test", "host-nodes"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package repository", result.stderr)

    def test_observability_identity_filter_uses_existing_repository_case(self):
        result = self.run_gate(arguments=("--package", "repository", "--test", "observability-identity"))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("./internal/repository -run ^TestObservabilityIdentityProjectionAgainstPostgres$", (self.root / "trace-1").read_text())
        result = self.run_gate(arguments=("--package", "iam", "--test", "observability-identity"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package repository", result.stderr)

    def test_process_cgroup_repair_filter_is_migration_only(self):
        result = self.run_gate(arguments=("--package", "migration", "--test", "process-cgroup-repair"))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("./internal/migration -run ^TestProcessCgroupDirtyMigrationRepairAgainstPostgres$", (self.root / "trace-1").read_text())
        result = self.run_gate(arguments=("--package", "repository", "--test", "process-cgroup-repair"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package migration", result.stderr)

    def test_coordination_filter_preserves_complete_default_discovery(self):
        result = self.run_gate(arguments=("--test", "engine-access-coordination"))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertIn("./internal/testsupport -run ^TestResetDisposablePostgresForGate$", trace)
        self.assertIn("./internal/migration -run ^Test(SourceDeny|SourceDenyRelease|FulfillmentOutcome|FulfillmentGrant|FulfillmentGrantRevocation|FulfillmentGrantRevocationExpiry|FulfillmentRecoveryPermission|FulfillmentHandlingPermission|FulfillmentBasis|ApprovalRequirement|SharingExpiry|SourceGrantRelations)ForwardMigrationAgainstPostgres$", trace)
        (self.root / "trace-1").unlink()
        result = self.run_gate(arguments=("--package", ""))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertIn("./internal/migration -run AgainstPostgres$", trace)
        self.assertIn("./internal/engineaccess -run AgainstPostgres$", trace)
        self.assertIn("./cmd/online-test-fixture -run AgainstPostgres$", trace)
        result = self.run_gate(arguments=("--package", "iam", "--test", "engine-access-coordination"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package migration", result.stderr)

    def test_catalog_integrity_filter_covers_role_publication_and_full_catalog(self):
        result = self.run_gate(arguments=("--test", "catalog-integrity"))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertIn("./internal/migration -run ^Test(Runner|EngineAccessDelegationRoleForwardMigration|SourceDataAuthorizerRoleForwardMigration)AgainstPostgres$", trace)
        result = self.run_gate(arguments=("--package", "iam", "--test", "catalog-integrity"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package migration", result.stderr)

    def test_source_grant_relations_filter_uses_migration_package(self):
        result = self.run_gate(arguments=("--test", "source-grant-relations"))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertIn("./internal/migration -run ^Test(SourceGrantRelations|SourceDataItemBoundary(Identity)?|SourceSingleRecordTargets)ForwardMigrationAgainstPostgres$", trace)
        result = self.run_gate(arguments=("--package", "iam", "--test", "source-grant-relations"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package migration", result.stderr)

    def test_terminated_gate_releases_lock(self):
        first = self.start_holder()
        os.killpg(first.pid, signal.SIGTERM)
        first.communicate(timeout=PROCESS_TIMEOUT_SECONDS)
        self.assertNotEqual(first.returncode, 0)
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_credential_context_filter_runs_shared_validation_consumers(self):
        result = self.run_gate(arguments=("--package", "iam", "--test", "credential-context"))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertIn("./internal/iam -run ^Test(DelegationService|ExecutionAuthorizationService|NotebookSessionAuthorizationService|CredentialValidationEvidence|BrowserAuthenticationClock)AgainstPostgres$", trace)
        result = self.run_gate(arguments=("--test", "credential-context"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package iam", result.stderr)

    def test_source_checkout_is_explicit_and_keeps_the_original_infra_entry(self):
        checkout = self.root / "review"
        subprocess.run(["git", "clone", "--shared", "-q", str(self.scripts[1].parents[2]), str(checkout)], check=True)
        # The source checkout's Infra scripts must not become the connection authority.
        (checkout / "scripts/infra/ports.sh").unlink()
        result = self.run_gate(arguments=("--source-root", str(checkout)))
        self.assertEqual(result.returncode, 0, result.stderr)
        working_directories = (self.root / "cwd-trace").read_text().splitlines()
        self.assertTrue(working_directories)
        self.assertEqual(set(working_directories), {str((checkout / "system/backend").resolve())})
        self.assertIn("./internal/testsupport -run ^TestResetDisposablePostgresForGate$", (self.root / "trace-1").read_text())

    def test_unrelated_source_is_rejected_before_any_database_command(self):
        unrelated = self.root / "unrelated"
        (unrelated / "system/backend").mkdir(parents=True)
        (unrelated / "system/backend/go.mod").write_text("module github.com/addp/system\n")
        subprocess.run(["git", "init", "-q"], cwd=unrelated, check=True)
        subprocess.run(["git", "add", "."], cwd=unrelated, check=True)
        subprocess.run(["git", "-c", "user.name=Gate Fixture", "-c", "user.email=gate@example.test", "commit", "-qm", "unrelated"], cwd=unrelated, check=True)
        result = self.run_gate(arguments=("--source-root", str(unrelated)))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must be an ADDP checkout", result.stderr)
        self.assertFalse((self.root / "trace-1").exists())

    def test_transfer_create_filter_keeps_default_migration_discovery(self):
        result = self.run_gate(arguments=("--test", "transfer-task-create"))
        self.assertEqual(result.returncode, 0, result.stderr)
        trace = (self.root / "trace-1").read_text()
        self.assertIn("./internal/migration -run ^TestTransferTaskCreateDelegationMigrationAgainstPostgres$", trace)
        (self.root / "trace-1").unlink()
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("./internal/migration -run AgainstPostgres$", (self.root / "trace-1").read_text())
        result = self.run_gate(arguments=("--package", "iam", "--test", "transfer-task-create"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("requires --package migration", result.stderr)


if __name__ == "__main__":
    unittest.main()
