import importlib.util
import json
import math
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
    def test_manager_physical_refuses_non_hosted_owner_or_unknown_action_before_mutation(self):
        for values, action in (({}, 'seed'),
                ({'ADDP_ONLINE_HOSTED': '1', 'ONLINE_SUITE': 'manager-internal-artifact-lineage'}, 'unknown')):
            with self.subTest(action=action), patch.dict(os.environ, values, clear=True), patch.object(m, 'command') as command:
                with self.assertRaises(m.FixtureError): m.manager_physical(action)
                command.assert_not_called()

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

    def test_area_average_oracle_excludes_each_bands_holes_and_differs_from_nearest_and_early_math(self):
        first, second, alpha = [list(m.average_pixels(band)) for band in (1, 2, 3)]
        self.assertEqual(first[:6], [None, 22., None, 54., 0., 88.])
        self.assertEqual(second[:6], [12., None, None, 108., 0., 168.])
        self.assertAlmostEqual(first[6], 308/3)
        self.assertAlmostEqual(second[6], 608/3)
        self.assertEqual(alpha[:7], [255., 255., 0., 255., 255., 255., 255.])
        self.assertEqual(list(m.average_pixels(1, joint=True))[:6], [None, None, None, 162., 0., 256.])
        self.assertEqual(m.average_source_pixel(1, 1, 7), 60.)
        self.assertNotEqual(first[3], m.average_source_pixel(1, 1, 7))
        # Intersecting source masks before math wrongly discards one contributor from each band.
        early_math = (84+168+88+176)/2
        self.assertEqual(early_math, 258.)
        self.assertNotEqual(first[5]+second[5], early_math)
        self.assertEqual([sum(value is not None for value in values) for values in (first, second)], [16382, 16382])

    def test_bilinear_oracle_keeps_invalid_centres_and_independent_neighbour_weights(self):
        self.assertEqual(m.bilinear_value(1, 1, 13), 51.)
        self.assertNotEqual(m.bilinear_value(1, 1, 13), m.average_source_pixel(1, 0, 6))
        self.assertEqual(m.bilinear_value(2, 1, 13), 102.)
        self.assertIsNone(m.bilinear_value(1, 1, 20))
        self.assertEqual(m.bilinear_value(2, 1, 20), 123.)
        self.assertAlmostEqual(m.bilinear_value(1, 1, 22), 1120/13)
        self.assertAlmostEqual(m.bilinear_value(2, 1, 22), 2168/13)
        self.assertNotAlmostEqual(m.bilinear_value(1, 1, 22)+m.bilinear_value(2, 1, 22), 253.2)
        self.assertEqual(m.bilinear_value(1, 1, 17), 0.)
        self.assertIsNone(m.bilinear_value(1, 1, 8))
        for band in (1, 2):
            self.assertEqual(sum(value is not None for value in m.bilinear_pixels(band)), 262104)
        self.assertEqual(sum(value is not None for value in m.bilinear_pixels(1, joint=True)), 262080)
        self.assertEqual(sum(0 < value < 255 for value in m.bilinear_pixels(3)), 64)

    def test_fractional_area_oracle_uses_overlap_weights_and_independent_band_validity(self):
        first, second, alpha = [list(m.average_pixels(band, width=171)) for band in (1, 2, 3)]
        self.assertIsNone(first[0])
        self.assertAlmostEqual(second[0], 255/32)
        self.assertIsNone(second[2])
        self.assertIsNone(first[3])
        self.assertIsNone(second[3])
        self.assertEqual(alpha[3], 0.)
        self.assertAlmostEqual(first[4], 211179/4064)
        self.assertNotAlmostEqual(first[4], 54.)  # Incorrect equal contributor weights.
        self.assertAlmostEqual(first[5], 19239/1024)  # Covered source zeros contribute.
        self.assertAlmostEqual(first[7], 4370448/50317)
        self.assertAlmostEqual(second[7], 8564056/51341)
        self.assertNotAlmostEqual(first[7]+second[7], 4596762/18061)  # Premature joint mask.
        self.assertAlmostEqual(first[-1], 16776705/64)
        self.assertEqual([sum(value is not None for value in values) for values in (first, second)], [29239, 29239])
        self.assertEqual(sum(value is not None for value in m.average_pixels(1, joint=True, width=171)), 29238)
        self.assertEqual(sum(0 < value < 255 for value in alpha), 0)

    def test_source_declarations_keep_finite_and_nan_sentinels_on_the_same_grid(self):
        sources = {name: (spatial, nodata) for name, spatial, nodata in m.SOURCE_FILES}
        self.assertEqual(len(sources), 5)
        self.assertEqual(sources['multiband-average-finite.tif'], (False, -9999.))
        self.assertTrue(math.isnan(sources['multiband-average.tif'][1]))
        self.assertEqual(m.multiband_source_name('multiband-average-finite'), 'multiband-average-finite.tif')
        self.assertEqual(m.multiband_source_name('multiband-average-finite-joint'), 'multiband-average-finite.cog.tif')
        self.assertEqual(m.multiband_expectation('multiband-average-finite')['width'], 171)
        self.assertEqual(m.multiband_expectation('multiband-average-finite')['valid_pixels'], 29239)
        self.assertEqual(m.multiband_expectation('multiband-average-finite-joint')['valid_pixels'], 29238)

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
        image = 'localhost:5001/addp-geopython-workflow-engine:raster-online'
        with patch.dict(os.environ, {'ADDP_ONLINE_RASTER_RUNTIME_IMAGE': image}), \
             patch.object(m, 'command', return_value=json.dumps(evidence)) as command:
            self.assertEqual(m.physical_worker('seed', self.root), evidence)
        path = self.root / 'raster-source-sha256.json'
        self.assertEqual(json.loads(path.read_text()), evidence['source_sha256'])
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertIn(str(self.root) + ':/secrets:ro', command.call_args.args[0])
        self.assertIn(image, command.call_args.args[0])
        self.assertNotIn('addp-geopython-workflow-engine:dev', command.call_args.args[0])

    def test_physical_worker_rejects_missing_product_image_before_docker(self):
        with patch.dict(os.environ, {'ADDP_ONLINE_RASTER_RUNTIME_IMAGE': ''}), \
             patch.object(m, 'command') as command:
            with self.assertRaisesRegex(m.FixtureError, 'product Runtime image is required'):
                m.physical_worker('verify-create', self.root)
            command.assert_not_called()

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
