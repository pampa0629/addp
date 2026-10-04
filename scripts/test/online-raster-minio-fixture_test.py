import importlib.util
import json
import os
from pathlib import Path
import stat
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

PATH = Path(__file__).parents[2] / 'business/scripts/online-raster-minio-fixture.py'
spec = importlib.util.spec_from_file_location('raster_physical_fixture', PATH)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class RasterPhysicalFixtureTest(unittest.TestCase):
    def test_analysis_oracle_counts_boundaries_and_outside_range_independently(self):
        results = m.analysis_expectations()
        self.assertEqual(results['statistics-band-2']['mean'], 65538)
        self.assertEqual(results['statistics-band-2']['min'], 4)
        self.assertEqual(results['statistics-band-2']['max'], 131072)
        self.assertIsNone(results['statistics-all-invalid']['mean'])
        self.assertEqual(results['histogram-auto']['edges'], [2, 16385.5, 32769, 49152.5, 65536])
        self.assertEqual(results['histogram-auto']['counts'], [16384, 16383, 16384, 16384])
        self.assertEqual(results['histogram-range']['counts'], [8192, 8192, 8192, 8193])
        self.assertEqual(results['histogram-range']['outside_count'], 32766)

    def test_multiband_independent_oracle_keeps_separate_holes_partial_alpha_zero_and_joint_mask(self):
        first, second, alpha = [list(m.multiband_pixels(band)) for band in (1, 2, 3)]
        self.assertEqual(first[:6], [None, 2., None, 4., 0., 6.])
        self.assertEqual(second[:6], [2., None, None, 8., 0., 12.])
        self.assertEqual(alpha[:6], [128., 128., 0., 128., 255., 255.])
        self.assertEqual(list(m.multiband_pixels(1, joint=True))[:6], [None, None, None, 12., 0., 18.])
        self.assertEqual([sum(value is not None for value in values) for values in (first, second)], [16382, 16382])
        self.assertEqual(m.multiband_expectation('multiband-joint')['valid_pixels'], 16381)
        source = list(m.multiband_pixels(1, source=True))
        self.assertEqual(source[4:6], [1e6, 1e6])
        self.assertEqual(source[:256], source[256:512])
        for case in m.MULTIBAND_CASES: self.assertIn('verify-' + case, m.ACTIONS)

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='addp-raster-fixture-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve() / 'addp-online-secret-test'
        self.root.mkdir(mode=0o700)
        self.env = {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1',
                    'RUNNER_TEMP': self.temp.name, 'ADDP_ONLINE_SECRET_DIR': str(self.root), 'ADDP_ONLINE_TEST_RUN_ID': 'raster-test'}
        self.commands = []

    def command(self, args, **kwargs):
        self.commands.append(args)
        return ''

    def test_admission_rejects_shared_personal_and_public_secret_directory(self):
        with patch.dict(os.environ, self.env), patch.object(m.os, 'uname', return_value=SimpleNamespace(sysname='Linux', machine='x86_64')):
            self.assertEqual(m.environment(), self.root)
            for changes in ({'GITHUB_ACTIONS': 'false'}, {'ADDP_ONLINE_HOSTED': '0'}, {'ADDP_ONLINE_SECRET_DIR': self.temp.name}):
                with patch.dict(os.environ, changes), self.assertRaises(m.FixtureError): m.environment()
            self.root.chmod(0o755)
            with self.assertRaises(m.FixtureError): m.environment()

    def test_start_keeps_distinct_credentials_owner_only_and_uses_tmpfs_without_platform_api(self):
        with patch.dict(os.environ, self.env), patch.object(m, 'owned', return_value=False), patch.object(m, 'command', self.command), \
             patch.object(m.urllib.request, 'urlopen') as request:
            request.return_value.__enter__.return_value.status = 200
            m.start(self.root)
        config = json.loads((self.root / 'raster-fixture.json').read_text())
        self.assertNotEqual(config['source']['secret_key'], config['target']['secret_key'])
        self.assertNotEqual(config['source']['endpoint'], config['target']['endpoint'])
        for file in self.root.iterdir():
            self.assertEqual(stat.S_IMODE(file.stat().st_mode), 0o600)
        for role in ('source', 'target'):
            descriptor = json.loads((self.root / f'raster-{role}-engine.json').read_text())
            self.assertEqual(descriptor['engine_type'], 'minio')
            self.assertIn('raster-test', descriptor['name'])
        self.assertEqual(len(self.commands), 2)
        for args in self.commands:
            self.assertIn('type=tmpfs,destination=/data', args)
            self.assertIn('com.addp.online-fixture=raster-workflow', args)
            self.assertIn('--env-file', args)
            for entry in config.values(): self.assertNotIn(entry['secret_key'], ' '.join(args))

    def test_seed_retains_source_fingerprints_in_private_directory(self):
        evidence = {'seeded': True, 'source_sha256': {'source.tif': 'source-hash', 'spatial.tif': 'spatial-hash'}}
        with patch.object(m, 'command', return_value=json.dumps(evidence)) as command:
            self.assertEqual(m.physical_worker('seed', self.root), evidence)
        path = self.root / 'raster-source-sha256.json'
        self.assertEqual(json.loads(path.read_text()), evidence['source_sha256'])
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertIn(str(self.root) + ':/secrets:ro', command.call_args.args[0])

    def test_partial_start_failure_cleans_both_owned_containers(self):
        def fail(args, **kwargs): raise m.FixtureError('run failed')
        with patch.dict(os.environ, self.env), patch.object(m, 'owned', return_value=False), patch.object(m, 'command', fail), \
             patch.object(m, 'stop') as stop:
            with self.assertRaises(m.FixtureError): m.start(self.root)
            stop.assert_called_once()

    def test_stop_never_removes_foreign_containers_and_attempts_all_owned_cleanup(self):
        def ownership(name):
            if name == 'addp-raster-target': raise m.FixtureError('foreign')
            return False
        with patch.object(m, 'owned', side_effect=ownership) as owned, patch.object(m, 'command', self.command):
            with self.assertRaises(m.FixtureError): m.stop()
            self.assertEqual({call.args[0] for call in owned.call_args_list}, set(m.CONTAINERS))
            self.assertEqual(self.commands, [])

    def test_docker_daemon_failure_is_not_reported_as_zero_residuals(self):
        with patch.object(m.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, '', 'daemon unavailable')), \
             patch.object(m, 'command', side_effect=m.FixtureError('daemon unavailable')):
            with self.assertRaises(m.FixtureError): m.owned('addp-raster-source')

    def test_owned_container_inspection_rejects_foreign_label(self):
        raw = json.dumps([{'Config': {'Labels': {'com.addp.online-fixture': 'other'}}}])
        with patch.object(m.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, raw, '')):
            with self.assertRaises(m.FixtureError): m.owned('addp-raster-source')


if __name__ == '__main__': unittest.main()
