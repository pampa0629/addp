import importlib
import tempfile
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

m = importlib.import_module('scripts.test.raster-nibble-capacity-online')


class NibbleCapacityTest(unittest.TestCase):
    def test_policy_restoration_preserves_primary_failure_and_remains_strict(self):
        original = {key: 2 for key in m.POLICY_FIELDS}
        platform = Mock()
        platform.request.side_effect = [SimpleNamespace(payload={'policy': {'version': 7}}),
                                        SimpleNamespace(payload={'policy': original})]
        report = {}
        m.restore_policy(platform, '/policy', original, report, None)
        self.assertTrue(report['policy_restored'])
        self.assertEqual(platform.request.call_args.args[-1], {**original, 'version': 7})
        primary = m.raster.SuiteError('GET /develop/executions/id returned HTTP 401 (authentication_required)')
        restoration = m.raster.SuiteError('GET /policy returned HTTP 401 (authentication_required)')
        platform.request.side_effect = restoration
        report = {}
        with self.assertRaises(m.raster.SuiteError) as caught:
            m.restore_policy(platform, '/policy', original, report, primary)
        self.assertIn(str(primary), str(caught.exception))
        self.assertIn('policy restoration failed: '+str(restoration), str(caught.exception))
        self.assertIs(caught.exception.__cause__, primary)
        self.assertNotIn('policy_restored', report)
        with self.assertRaisesRegex(m.raster.SuiteError, 'authentication_required'):
            m.restore_policy(platform, '/policy', original, report, None)
        platform.request.side_effect = [SimpleNamespace(payload={'policy': {'version': 7}}),
                                        SimpleNamespace(payload={'policy': {**original, 'running': 3}})]
        with self.assertRaisesRegex(m.raster.SuiteError, 'capacity policy restoration failed'):
            m.restore_policy(platform, '/policy', original, report, None)

    def test_sampler_shutdown_retains_polling_failure_and_redacts_unexpected_errors(self):
        sampler = m.Sampler(Path('/unused-cgroup'), None, '/policy', Path('/tmp/owned/runtime-tmp'))
        primary = m.raster.SuiteError('execution poll failed')
        secondary = m.raster.SuiteError('resource observation failed')
        with patch.object(m.capacity.Sampler, 'finish', side_effect=secondary):
            with self.assertRaises(m.raster.SuiteError) as caught:
                sampler.finish(primary)
            self.assertEqual(str(caught.exception), 'execution poll failed; capacity sampler shutdown failed: resource observation failed')
            self.assertIs(caught.exception.__cause__, primary)
            with self.assertRaises(m.raster.SuiteError) as caught:
                sampler.finish()
            self.assertIs(caught.exception, secondary)
        combined = m.combined_failure(ValueError('private credential'), RuntimeError('another secret'), 'cleanup')
        self.assertEqual(str(combined), 'ValueError; cleanup failed: RuntimeError')

    def test_matrix_has_sixteen_canonical_save_chains_and_original_identity(self):
        groups = list(m.fixture.groups()); self.assertEqual(len(groups), 8)
        names = [n for s, d, r in groups for n in m.fixture.target_names(s, d, r)]
        self.assertEqual(len(set(names)), 16)
        self.assertEqual(set(s for s, _, _ in groups), {4096, 10980})
        self.assertEqual(len(m.raster.EDITOR_PERMISSIONS), 12)
        for name in names:
            tasks = m.definition('source', 'parent', name)['tasks']
            self.assertEqual(tasks[-2]['params']['target_values'], [7])
            self.assertEqual(tasks[-1]['params']['write_mode'], 'create')
            self.assertEqual(tasks[-1]['params']['profile'], 'cog')
        with self.assertRaises(m.raster.SuiteError): m.definition('source', 'parent', '../outside')

    def test_disk_sampling_and_bound_reject_missing_or_residual_data(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory); workspace = root/'addp-raster-one'; workspace.mkdir()
            (workspace/'distance.distance').write_bytes(b'x'*1024)
            fact = m.disk_sample(root); self.assertEqual(fact['private_logical_bytes'], 1024)
            self.assertEqual(fact['workspace_count'], 1)
            zero = {key: 0 for key in fact}
            samples = [{'disk': fact}, {'disk': zero}]
            self.assertEqual(m.validate_disk(samples, 8, 1)['private_logical_peak_bytes'], 1024)
            for changed in ([], [{'disk': zero}], [{'disk': fact}], [{'disk': {**fact, 'private_logical_bytes': 99999}}, {'disk': zero}]):
                with self.assertRaises(m.raster.SuiteError): m.validate_disk(changed, 8, 1)
            (root/'foreign').mkdir()
            with self.assertRaises(m.raster.SuiteError): m.disk_sample(root)

    def test_disk_preflight_is_size_and_concurrency_derived(self):
        with patch.object(m.fixture.os, 'statvfs', return_value=SimpleNamespace(f_bavail=10**15, f_frsize=1)):
            one = m.fixture.require_disk(Path('/unused'), 10980, 1)
            two = m.fixture.require_disk(Path('/unused'), 10980, 2)
        self.assertEqual(two['required_free_bytes']-one['required_free_bytes'], 10980**2*51)
        with patch.object(m.fixture.os, 'statvfs', return_value=SimpleNamespace(f_bavail=1, f_frsize=1)):
            with self.assertRaises(m.fixture.FixtureError): m.fixture.require_disk(Path('/unused'), 10980, 2)

    def test_reuses_cgroup_limits_and_admission_validation(self):
        c = m.capacity
        r = {'enabled': True, 'cache_mib': 64, 'pending_restart': False, 'applied_version': 7,
             'running': 0, 'waiting': 0, 'running_limit': 2, 'waiting_limit': 2, 'memory_limit_bytes': 2*1024**3,
             'effective_cpu': 4, 'advice': {'running': 2, 'waiting': 2, 'cache_mib': 64, 'basis': 'cpu-and-memory-budget-heuristic'}}
        metrics = {'oom': 0, 'oom_kill': 0, 'memory_peak_bytes': 12345, 'cpu_usage_usec': 1, 'process_peak_rss_bytes': 100}
        samples = [{**metrics, 'runtime': {**r, 'running': 2}}, {**metrics, 'runtime': r}]
        self.assertEqual(c.validate_measurement(samples, metrics, metrics, 64, 2, 7)['max_running'], 2)
        for bad in ({**metrics, 'oom': 1}, {**metrics, 'oom_kill': 1}):
            with self.assertRaises(m.raster.SuiteError): c.validate_measurement(samples, metrics, bad, 64, 2, 7)
        with self.assertRaises(m.raster.SuiteError): c.validate_measurement(samples[1:], metrics, metrics, 64, 2, 7)

    def test_disk_probe_runs_on_host_and_preserves_failure_diagnostics(self):
        sampler = m.Sampler(Path('/unused-cgroup'), None, '/policy', Path('/tmp/owned/runtime-tmp'))
        def base_sample(instance): instance.samples.append({'runtime': {}})
        with patch.object(m.capacity.Sampler, 'sample', base_sample), patch.dict(m.os.environ, ADDP_ONLINE_TEST_RUN_ID='nibble-capacity-123-1'):
            with patch.object(m.fixture, 'command', return_value='{"private_logical_bytes":0}') as command:
                sampler.sample()
                self.assertEqual(command.call_args.args[0][:3], ['sudo', '-n', 'python3'])
                self.assertEqual(sampler.samples[-1]['disk']['private_logical_bytes'], 0)
            error = m.subprocess.CalledProcessError(1, ['probe'], stderr='owned disk probe denied')
            with patch.object(m.fixture, 'command', side_effect=error):
                with self.assertRaisesRegex(m.raster.SuiteError, 'owned disk probe denied'): sampler.sample()

    def test_scan_must_bind_the_exact_parent_engine_user_and_tenant(self):
        scan = {'execution_id': 'scan', 'parent_execution_id': 'parent', 'module': 'meta', 'task_type': 'scan',
                'status': 'success', 'tenant_id': 2, 'triggered_by': 33, 'scan_context': {'engine_id': 7}}
        self.assertEqual(m.validate_scan(scan, 'scan', 'parent', 2, 33, 7)['scan_context'], {'engine_id': 7})
        for key, value in (('execution_id', 'other'), ('parent_execution_id', 'other'), ('tenant_id', 3),
                           ('triggered_by', 1), ('status', 'failed'), ('module', 'develop'),
                           ('scan_context', {'engine_id': 8}), ('tenant_id', '2'), ('triggered_by', '33')):
            with self.subTest(key=key), self.assertRaises(m.raster.SuiteError):
                m.validate_scan({**scan, key: value}, 'scan', 'parent', 2, 33, 7)
