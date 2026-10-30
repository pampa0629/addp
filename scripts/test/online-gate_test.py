import importlib.util
import fcntl
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


SCRIPT = Path(__file__).with_name("online-gate.py")
SPEC = importlib.util.spec_from_file_location("online_gate", SCRIPT)
ONLINE_GATE = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
sys.modules[SPEC.name] = ONLINE_GATE
SPEC.loader.exec_module(ONLINE_GATE)


class OnlineGateTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="addp-online-gate-")
        self.root = Path(self.temporary.name)
        self.repository = self.root / "repository"
        self.repository.mkdir()
        self.suites = {
            "owner-scenario": ONLINE_GATE.Suite(
                command=("owner-gate", "--online"),
                services=(("system", "SYSTEM_URL"), ("model", "MODEL_URL")),
            )
        }
        self.environment = {
            "ADDP_ONLINE_TEST": "1",
            "ADDP_ONLINE_TEST_TENANT_ID": "42",
            "POSTGRES_DB": "addp_online",
            "SYSTEM_URL": "http://127.0.0.1:8180",
            "MODEL_URL": "http://127.0.0.1:8087",
            "ADDP_ONLINE_TEST_USER_ACCESS_TOKEN": "addp_at_never-persist-this",
            "MANAGER_SERVICE_CLIENT_SECRET": "never-persist-this-secret",
            "ADDP_ONLINE_ARTIFACT_DIR": str(self.root / "artifacts"),
        }

    def tearDown(self):
        self.temporary.cleanup()

    def test_kml_case_probes_transfer_before_owner_and_keeps_other_cases_unchanged(self):
        for case in ('full', 'native-raster-media', 'kml-export'):
            with self.subTest(case=case), patch.object(ONLINE_GATE, 'run_stage', side_effect=[({}, 1), ({}, 1)]) as stage:
                env = dict(self.environment, ADDP_ONLINE_MANAGER_CASE=case, TRANSFER_URL='http://127.0.0.1:8083',
                           ADDP_ONLINE_TEST_RUN_ID='kml-profile-' + case)
                suite = {'manager-internal-artifact-lineage': ONLINE_GATE.Suite(('owner',), (('system', 'SYSTEM_URL'),))}
                report = ONLINE_GATE.run_online('manager-internal-artifact-lineage', self.repository, env, suite)
                command = stage.call_args_list[0].args[1]
                self.assertEqual('transfer=http://127.0.0.1:8083' in command, case == 'kml-export')
                self.assertEqual('transfer' in report['service_addresses'], case == 'kml-export')

    def execution_report(self, profile):
        import uuid
        report = dict(schema_version=f'addp.{profile}-execution-online/v1', result='passed',
                      control_cleanup='passed', hosted_destruction='required',
                      checks=sorted(ONLINE_GATE.EXECUTION_CHECKS[profile]))
        if profile == 'graph':
            report.update(graph_id=1, engine_type='falkordb', controlled_extraction=True, real_model=False,
                          reopen_success='blocked_provider_identity', running_crash=dict(
                              graph_id=2, task_id=3, execution_id=str(uuid.uuid4()),
                              result='passed', attempt=1, live_attempt_observed=True, no_replay=True,
                              unresolved_hold=True, remote_return_terminal_unchanged=True))
        else:
            report.update(principal_id='1', browser=dict(schema_version='addp.cleanup-execution-browser/v1', result='passed',
                              run_id='execution-test', tenant_id='42', principal_id='2', request_id=str(uuid.uuid4()),
                              execution_id=str(uuid.uuid4()), phases=[dict(phase=phase, real_pagination=True,
                                  result_read_before_revoke=True, real_denial=True, previous_view_cleared=True, late_result_rejected=True)
                                  for phase in ('refresh','pagination')]),
                          uncertain_revocation=False, physical_effect_acceptance='not_run',
                          owner_results=[dict(module=owner, action=action, execution_id=str(uuid.uuid4()),
                                              receipt_result_equal=True, other_user_denied=True, get_no_write=True)
                                         for owner in sorted(ONLINE_GATE.EXECUTION_OWNERS) for action in ('scan', 'execute')])
        return report

    def test_execution_dispatch_is_unique_and_keeps_peer_out_of_other_cases(self):
        import json
        for profile in ('graph', 'cleanup'):
            name = 'graph-protection' if profile == 'graph' else 'cleanup-execution'
            env = dict(self.environment, ADDP_ONLINE_GRAPH_ENGINE='falkordb', ADDP_ONLINE_TEST_RUN_ID='execution-test',
                       ADDP_ONLINE_GRAPH_CASE='execution-lifecycle', ADDP_ONLINE_GRAPH_PEER_URL='http://127.0.0.1:18186')
            for _, variable in ONLINE_GATE.SUITES[name].services:
                env.setdefault(variable, 'http://127.0.0.1:8000')
            report = self.execution_report(profile)
            directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR']); directory.mkdir(exist_ok=True)
            (directory / f'{profile}-execution.json').write_text(json.dumps(report))
            if profile == 'cleanup': (directory / 'cleanup-browser.json').write_text(json.dumps(report['browser']))
            with patch.object(ONLINE_GATE, 'run_stage', side_effect=[({}, 1), (report, 1)]) as stage:
                actual = ONLINE_GATE.run_online(name, self.repository, env)
            self.assertEqual(actual['result'], 'passed')
            self.assertIn('--execution-profile', stage.call_args_list[0].args[1])
            self.assertEqual(stage.call_args_list[1].args[1], (sys.executable, '-m', f'scripts.test.{profile}-execution-online'))
            self.assertEqual('graph-peer=http://127.0.0.1:18186' in stage.call_args_list[0].args[1], profile == 'graph')
        for case in ('full', 'endpoint-geometry', 'endpoint-same-color'):
            self.assertIsNone(ONLINE_GATE.execution_profile('graph-protection', {'ADDP_ONLINE_GRAPH_CASE': case}))
        for case in ('', 'all', 'lifecycle'):
            with self.assertRaises(ONLINE_GATE.OnlineGateError):
                ONLINE_GATE.execution_profile('graph-protection', {'ADDP_ONLINE_GRAPH_CASE': case})

    def test_execution_report_rejects_missing_failed_or_forged_scope(self):
        import copy
        env = {'ADDP_ONLINE_GRAPH_ENGINE': 'falkordb', 'ADDP_ONLINE_TEST_RUN_ID':'execution-test', 'ADDP_ONLINE_TEST_TENANT_ID':'42'}
        for profile in ('graph', 'cleanup'):
            valid = self.execution_report(profile)
            mutations = [lambda r: r.pop('schema_version'), lambda r: r.update(result='failed'),
                         lambda r: r.update(control_cleanup='failed'), lambda r: r.update(hosted_destruction='passed'),
                         lambda r: r.update(checks=r['checks'][:-1]), lambda r: r['checks'].append(r['checks'][0])]
            if profile == 'graph':
                mutations += [lambda r: r.update(engine_type='neo4j'), lambda r: r.update(real_model=True),
                              lambda r: r.update(reopen_success='passed'), lambda r: r.pop('running_crash'),
                              lambda r: r['running_crash'].update(no_replay=False),
                              lambda r: r['running_crash'].update(task_id=True),
                              lambda r: r['running_crash'].update(task_id='00000000-0000-0000-0000-000000000001'),
                              lambda r: r['running_crash'].update(graph_id=r['graph_id']),
                              lambda r: r['running_crash'].update(execution_id='not-uuid')]
            else:
                mutations += [lambda r: r.update(uncertain_revocation=True), lambda r: r.update(physical_effect_acceptance='passed'),
                              lambda r: r['owner_results'].pop(),
                              lambda r: r['owner_results'].__setitem__(1, r['owner_results'][0]),
                              lambda r: r['owner_results'][0].update(get_no_write=False),
                              lambda r: r['owner_results'][0].update(execution_id='not-uuid'),
                              lambda r: r.pop('browser'), lambda r: r['browser'].update(run_id='other-run'),
                              lambda r: r['browser']['phases'][0].update(late_result_rejected=1),
                              lambda r: r['browser'].update(access_token='forbidden')]
            ONLINE_GATE.validate_execution_report(profile, valid, env)
            for mutate in mutations:
                value = copy.deepcopy(valid); mutate(value)
                with self.subTest(profile=profile, mutation=mutate), self.assertRaises(ONLINE_GATE.OnlineStageError):
                    ONLINE_GATE.validate_execution_report(profile, value, env)

    def test_execution_archive_is_required_and_must_match_stdout(self):
        import json
        directory = self.root / 'execution-reports'; directory.mkdir()
        report = self.execution_report('graph'); env = {'ADDP_ONLINE_GRAPH_ENGINE': 'falkordb'}
        path = directory / 'graph-execution.json'
        for content in (None, '{', json.dumps(dict(report, result='failed'))):
            if content is not None: path.write_text(content)
            with self.assertRaises(ONLINE_GATE.OnlineStageError):
                ONLINE_GATE.validate_execution_archive('graph', report, directory, env)
        path.write_text(json.dumps(report))
        ONLINE_GATE.validate_execution_archive('graph', report, directory, env)
        path.unlink(); path.symlink_to(directory / 'missing')
        with self.assertRaises(ONLINE_GATE.OnlineStageError):
            ONLINE_GATE.validate_execution_archive('graph', report, directory, env)

    def test_cleanup_browser_archive_cannot_be_missing_or_disagree(self):
        import json
        report = self.execution_report('cleanup')
        env = {'ADDP_ONLINE_TEST_RUN_ID':'execution-test','ADDP_ONLINE_TEST_TENANT_ID':'42'}
        directory = self.root / 'browser-reports'; directory.mkdir()
        (directory / 'cleanup-execution.json').write_text(json.dumps(report))
        browser = directory / 'cleanup-browser.json'
        with self.assertRaises(ONLINE_GATE.OnlineStageError):
            ONLINE_GATE.validate_execution_archive('cleanup', report, directory, env)
        browser.write_text(json.dumps(dict(report['browser'], access_token='forbidden')))
        with self.assertRaises(ONLINE_GATE.OnlineStageError):
            ONLINE_GATE.validate_execution_archive('cleanup', report, directory, env)
        browser.write_text(json.dumps(report['browser']))
        ONLINE_GATE.validate_execution_archive('cleanup', report, directory, env)

    @patch.object(ONLINE_GATE.time, "monotonic", side_effect=(10.0, 12.0, 14.0))
    @patch.object(
        ONLINE_GATE,
        "run_stage",
        side_effect=(
            ({"schema_version": "addp.online-preflight/v1", "git_commit": "abc"}, 10),
            ({"schema_version": "addp.online-suite/v1", "residual_resources": 0}, 20),
        ),
    )
    def test_dispatches_preflight_before_owner_gate(self, run_stage, _monotonic):
        report = ONLINE_GATE.run_online(
            "owner-scenario", self.repository, self.environment, self.suites
        )

        self.assertEqual(run_stage.call_count, 2)
        preflight = run_stage.call_args_list[0]
        owner = run_stage.call_args_list[1]
        self.assertEqual(preflight.args[0], "preflight")
        self.assertIn("online-preflight.py", preflight.args[1][1])
        self.assertIn("system=http://127.0.0.1:8180", preflight.args[1])
        self.assertEqual(owner.args, ("scenario", ("owner-gate", "--online")))
        self.assertEqual(preflight.kwargs["timeout"], 900)
        self.assertEqual(owner.kwargs["timeout"], 898)
        self.assertRegex(self.environment["ADDP_ONLINE_TEST_RUN_ID"], r"^run-[0-9a-f]{32}$")
        self.assertEqual(report["result"], "passed")
        self.assertEqual(report["failure_stage"], None)
        persisted = (self.root / "artifacts/online-report.json").read_text(encoding="utf-8")
        self.assertIn('"schema_version": "addp.online-gate/v1"', persisted)
        self.assertNotIn("addp_at_", persisted)
        self.assertNotIn("never-persist-this-secret", persisted)

    def test_registers_only_executable_online_suites(self):
        self.assertEqual(
            set(ONLINE_GATE.SUITES),
            {
                "compose-public-origin",
                "consumer-engine-recovery",
                "elasticsearch-consumer-flow",
                "hdfs-spark-consumer-flow",
                "redis-consumer-flow",
                "ftp-consumer-flow",
                "highgo-consumer-flow",
                "gaussdb-consumer-flow",
                "starrocks-consumer-flow",
                "catalog-domain-overview",
                "enterprise-catalog-publishing",
                "kingbase-consumer-flow",
                "manager-internal-artifact-lineage",
                "manager-hybrid-search",
                "metric-service-revision-lifecycle",
                "ontology-revision-lifecycle",
                "orchestrator-execution",
                "execution-retention-readonly",
                "execution-events-pagination",
                "execution-retention-hourly",
                "execution-events-retention",
                "platform-administrator-governance",
                "platform-node-metrics",
                "pg-source-authorization",
                "module-registry-recovery",
                "workflow-runtime-health-recovery",
                "oceanbase-consumer-flow",
                "opengauss-consumer-flow",
                "quality-dynamic-binding",
                "raster-workflow",
                "raster-contour-persistence",
                "raster-polygonize-persistence",
                "vector-rasterize-persistence",
                "vector-zonal-statistics-persistence",
                "raster-proximity-persistence",
                "raster-focal-mode-persistence", "raster-boundary-clean-persistence", "raster-thin-persistence", "raster-fill-depressions-persistence", "raster-flow-direction-d8-persistence", "raster-flow-accumulation-d8-persistence", "raster-drainage-basins-d8-persistence", "raster-flow-length-d8-persistence", "raster-flow-accumulation-area-d8-persistence", "raster-distance-to-stream-d8-persistence", "raster-hand-d8-persistence", "raster-curvature-persistence", "raster-profile-curvature-persistence", "raster-cut-fill-persistence", "raster-surface-area-persistence", "raster-twi-persistence", "raster-spi-persistence", "raster-sieve-persistence", "raster-focal-percentile-persistence", "raster-tangential-curvature-persistence", "raster-plan-curvature-persistence", "raster-tpi-persistence", "raster-tri-persistence", "raster-roughness-persistence", "raster-flow-length-upstream-d8-persistence", "raster-stream-order-d8-persistence", "raster-stream-shreve-d8-persistence", "raster-stream-to-vector-d8-persistence", "raster-stream-link-d8-persistence", "raster-flow-accumulation-weighted-d8-persistence", "raster-watershed-d8-persistence", "raster-subcatchments-d8-persistence", "raster-snap-pour-points-persistence", "raster-band-recommendation-workflow", "raster-statistics-percentile-workflow", "raster-nibble-persistence", "raster-nibble-capacity", "raster-disk-pressure",
                "raster-focal-statistics-persistence",
                "raster-zonal-statistics-persistence",
                "vector-pmtiles-workflow",
                "vector-topology-persistence",
                "raster-workflow-capacity",
                "security-mysql-owner-protection",
                "security-plaintext-access",
                "graph-protection",
                "cleanup-execution",
                "cleanup-native-effects",
                "security-transfer-protection",
                "standard-model-reference-deletion",
                "tidb-consumer-flow",
                "transfer-insert-only-mysql",
                "transfer-relational-sql-etl",
                "transfer-continuous-events",
                "workbench-service-consumption",
            },
        )
        gaussdb = ONLINE_GATE.SUITES["gaussdb-consumer-flow"]
        self.assertFalse(gaussdb.nightly)
        self.assertEqual(gaussdb.command, (sys.executable, "-m", "scripts.test.readonly-sql-consumer-flow-online", "--engine-type", "gaussdb"))
        self.assertEqual(gaussdb.services, (("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL"), ("meta", "META_URL"), ("manager", "MANAGER_URL"), ("security", "SECURITY_URL"), ("develop", "DEVELOP_URL")))
        starrocks = ONLINE_GATE.SUITES["starrocks-consumer-flow"]
        self.assertFalse(starrocks.nightly)
        self.assertEqual(starrocks.command, (sys.executable, "-m", "scripts.test.readonly-sql-consumer-flow-online", "--engine-type", "starrocks"))
        self.assertEqual(starrocks.services, gaussdb.services)
        suite = ONLINE_GATE.SUITES["standard-model-reference-deletion"]
        elasticsearch_suite = ONLINE_GATE.SUITES["elasticsearch-consumer-flow"]
        self.assertEqual(
            elasticsearch_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("meta", "META_URL"),
                ("manager", "MANAGER_URL"),
                ("develop", "DEVELOP_URL"),
            ),
        )
        self.assertFalse(elasticsearch_suite.nightly)
        self.assertEqual(
            suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("standard", "STANDARD_URL"),
                ("model", "MODEL_URL"),
            ),
        )
        registry_suite = ONLINE_GATE.SUITES["module-registry-recovery"]
        self.assertEqual(
            registry_suite.services,
            (("gateway", "GATEWAY_URL"), ("system", "SYSTEM_URL")),
        )
        oceanbase_suite = ONLINE_GATE.SUITES["oceanbase-consumer-flow"]
        self.assertEqual(
            oceanbase_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("meta", "META_URL"),
                ("manager", "MANAGER_URL"),
                ("transfer", "TRANSFER_URL"),
                ("develop", "DEVELOP_URL"),
                ("service", "SERVICE_URL"),
            ),
        )
        opengauss_suite = ONLINE_GATE.SUITES["opengauss-consumer-flow"]
        self.assertEqual(opengauss_suite.command, oceanbase_suite.command)
        self.assertEqual(opengauss_suite.services, oceanbase_suite.services)
        self.assertTrue(ONLINE_GATE.SUITES["compose-public-origin"].nightly)
        self.assertTrue(opengauss_suite.nightly)
        self.assertTrue(ONLINE_GATE.SUITES["metric-service-revision-lifecycle"].nightly)
        self.assertFalse(oceanbase_suite.nightly)
        tidb_suite = ONLINE_GATE.SUITES["tidb-consumer-flow"]
        self.assertEqual(tidb_suite.command, oceanbase_suite.command)
        self.assertEqual(tidb_suite.services, oceanbase_suite.services)
        self.assertFalse(tidb_suite.nightly)
        kingbase_suite = ONLINE_GATE.SUITES["kingbase-consumer-flow"]
        self.assertEqual(kingbase_suite.command, oceanbase_suite.command)
        self.assertEqual(kingbase_suite.services, oceanbase_suite.services)
        self.assertFalse(kingbase_suite.nightly)
        transfer_suite = ONLINE_GATE.SUITES["transfer-insert-only-mysql"]
        self.assertEqual(
            transfer_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("meta", "META_URL"),
                ("transfer", "TRANSFER_URL"),
            ),
        )
        sql_etl_suite = ONLINE_GATE.SUITES["transfer-relational-sql-etl"]
        self.assertEqual(sql_etl_suite.services, transfer_suite.services + (("manager", "MANAGER_URL"), ("develop", "DEVELOP_URL"), ("orchestrator", "ORCHESTRATOR_URL"), ("monitor", "MONITOR_URL"), ("security", "SECURITY_URL")))
        continuous_suite = ONLINE_GATE.SUITES["transfer-continuous-events"]
        self.assertIn(("security", "SECURITY_URL"), continuous_suite.services)
        hybrid_search_suite = ONLINE_GATE.SUITES["manager-hybrid-search"]
        self.assertEqual(
            hybrid_search_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("meta", "META_URL"),
                ("manager", "MANAGER_URL"),
                ("inference", "INFERENCE_URL"),
            ),
        )
        consumer_suite = ONLINE_GATE.SUITES["consumer-engine-recovery"]
        self.assertEqual(
            consumer_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("manager", "MANAGER_URL"),
                ("service", "SERVICE_URL"),
            ),
        )
        catalog_suite = ONLINE_GATE.SUITES["enterprise-catalog-publishing"]
        self.assertFalse(catalog_suite.nightly, "new real acceptance case remains manually dispatched until first pass")
        self.assertEqual(
            catalog_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("meta", "META_URL"),
                ("catalog", "CATALOG_URL"),
                ("asset", "ASSET_URL"),
                ("portal", "PORTAL_URL"),
            ),
        )
        workbench_suite = ONLINE_GATE.SUITES["workbench-service-consumption"]
        self.assertEqual(
            workbench_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("service", "SERVICE_URL"),
                ("workbench", "WORKBENCH_URL"),
            ),
        )
        security_suite = ONLINE_GATE.SUITES["security-transfer-protection"]
        self.assertEqual(
            security_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("meta", "META_URL"),
                ("security", "SECURITY_URL"),
                ("graph", "GRAPH_URL"),
                ("transfer", "TRANSFER_URL"),
                ("manager", "MANAGER_URL"),
            ),
        )
        exemption_suite = ONLINE_GATE.SUITES["security-plaintext-access"]
        self.assertEqual(
            exemption_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("meta", "META_URL"),
                ("security", "SECURITY_URL"),
                ("graph", "GRAPH_URL"),
                ("manager", "MANAGER_URL"),
                ("develop", "DEVELOP_URL"),
            ),
        )
        mysql_security_suite = ONLINE_GATE.SUITES[
            "security-mysql-owner-protection"
        ]
        self.assertEqual(
            mysql_security_suite.services,
            (
                ("gateway", "GATEWAY_URL"),
                ("system", "SYSTEM_URL"),
                ("meta", "META_URL"),
                ("security", "SECURITY_URL"),
                ("graph", "GRAPH_URL"),
                ("manager", "MANAGER_URL"),
                ("develop", "DEVELOP_URL"),
                ("service", "SERVICE_URL"),
                ("transfer", "TRANSFER_URL"),
                ("monitor", "MONITOR_URL"),
            ),
        )

    @patch.object(
        ONLINE_GATE,
        "run_stage",
        side_effect=(({"schema_version": "preflight"}, 1), ({"schema_version": "suite"}, 1)),
    )
    def test_preserves_explicit_run_id(self, _run_stage):
        self.environment["ADDP_ONLINE_TEST_RUN_ID"] = "nightly-42"
        ONLINE_GATE.run_online(
            "owner-scenario", self.repository, self.environment, self.suites
        )
        self.assertEqual(self.environment["ADDP_ONLINE_TEST_RUN_ID"], "nightly-42")

    def test_rejects_unregistered_suite(self):
        with self.assertRaisesRegex(ONLINE_GATE.OnlineGateError, "registered suites"):
            ONLINE_GATE.run_online("missing", self.repository, self.environment, self.suites)

    def test_requires_explicit_online_switch(self):
        self.environment.pop("ADDP_ONLINE_TEST")
        with self.assertRaisesRegex(ONLINE_GATE.OnlineGateError, "exactly 1"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )

    def test_requires_all_suite_service_urls(self):
        self.environment.pop("MODEL_URL")
        with self.assertRaisesRegex(ONLINE_GATE.OnlineGateError, "MODEL_URL"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )

    @patch.object(
        ONLINE_GATE,
        "run_stage",
        side_effect=ONLINE_GATE.OnlineStageError(
            "preflight", "online_preflight_failed", "invalid service URL"
        ),
    )
    def test_preflight_owns_service_url_validation_and_failure_report(self, run_stage):
        self.environment["MODEL_URL"] = "http://localhost:invalid"
        with self.assertRaisesRegex(ONLINE_GATE.OnlineStageError, "invalid service URL"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )

        self.assertEqual(run_stage.call_count, 1)
        report = (self.root / "artifacts/online-report.json").read_text(encoding="utf-8")
        self.assertIn('"error_code": "online_preflight_failed"', report)
        self.assertIn('"service_addresses": {}', report)

    def test_rejects_non_positive_timeout(self):
        self.environment["ADDP_ONLINE_TEST_TIMEOUT_SECONDS"] = "0"
        with self.assertRaisesRegex(ONLINE_GATE.OnlineGateError, "greater than zero"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )

    @patch.object(
        ONLINE_GATE,
        "run_stage",
        side_effect=ONLINE_GATE.OnlineStageError(
            "preflight", "online_preflight_timeout", "timed out"
        ),
    )
    def test_records_timeout_with_stable_error_code(self, _run_stage):
        with self.assertRaisesRegex(ONLINE_GATE.OnlineStageError, "timed out"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )
        report = (self.root / "artifacts/online-report.json").read_text(encoding="utf-8")
        self.assertIn('"error_code": "online_preflight_timeout"', report)
        self.assertIn('"failure_stage": "preflight"', report)

    @patch.object(ONLINE_GATE.time, "monotonic", side_effect=(10.0, 12.0, 13.0))
    @patch.object(
        ONLINE_GATE,
        "run_stage",
        return_value=({"schema_version": "preflight"}, 1),
    )
    def test_rejects_exhausted_total_timeout(self, run_stage, _monotonic):
        self.environment["ADDP_ONLINE_TEST_TIMEOUT_SECONDS"] = "1"
        with self.assertRaisesRegex(ONLINE_GATE.OnlineStageError, "total timeout"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )
        self.assertEqual(run_stage.call_count, 1)

    def test_rejects_concurrent_same_suite_and_run_id(self):
        self.environment["ADDP_ONLINE_TEST_RUN_ID"] = "nightly-42"
        lock_root = self.root / "locks"
        lock_root.mkdir()
        lock_path = lock_root / "owner-scenario--nightly-42.lock"
        with lock_path.open("a+") as lock:
            fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(ONLINE_GATE.OnlineStageError, "already active"):
                ONLINE_GATE.run_online(
                    "owner-scenario",
                    self.repository,
                    self.environment,
                    self.suites,
                    lock_root=lock_root,
                )
        report = (self.root / "artifacts/online-report.json").read_text(encoding="utf-8")
        self.assertIn('"error_code": "online_run_active"', report)

    def test_rejects_artifact_directory_inside_repository(self):
        self.environment["ADDP_ONLINE_ARTIFACT_DIR"] = str(self.repository / "artifacts")
        with self.assertRaisesRegex(ONLINE_GATE.OnlineGateError, "outside"):
            ONLINE_GATE.run_online(
                "owner-scenario", self.repository, self.environment, self.suites
            )


if __name__ == "__main__":
    unittest.main()
