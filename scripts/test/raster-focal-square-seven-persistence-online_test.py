import copy
import hashlib
import importlib
import json
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch

m = importlib.import_module('scripts.test.raster-focal-square-seven-persistence-online')
prior = importlib.import_module('scripts.test.raster-focal-percentiles-persistence-online_test')


def physical(repository, env, action, names, timeout):
    assert 0 < timeout <= 600
    if action == 'cleanup-oracle':
        return {'oracle_cache_removed': True}
    sources = {
        name: {'size_bytes': 4096, 'sha256': hashlib.sha256(name.encode()).hexdigest()} for name in m.fixture.SOURCES
    }
    flags = (
        'cog_valid',
        'full_pixel_and_mask_verification',
        'independent_scalar_oracle_verified',
        'band_order_and_descriptions_verified',
        'nearest_overviews_verified',
    )
    targets = {
        name: {
            'size_bytes': 256,
            'sha256': hashlib.sha256(name.encode()).hexdigest(),
            'band_count': m.fixture.band_count(name),
            'band_descriptions': m.fixture.descriptions(name),
            'overview_count': m.fixture.overview_count(name),
            'has_overviews': bool(m.fixture.overview_count(name)),
            **dict.fromkeys(flags, True),
        }
        for name in names
    }
    prefix = m.fixture.PREFIX
    return {
        'sources': sources,
        'targets': targets,
        'full_cog_checks': len(names),
        'runtime_workspaces_removed': True,
        'full_source_pixels_verified_at_baseline': True,
        'oracle_sha256': 'a' * 64,
        'actual_control_pairs_verified': m.fixture.actual_pairs(names),
        'archived_oracle': {
            m.fixture.ORACLE: {'path': prefix + '-oracle/' + m.fixture.ORACLE, 'size_bytes': 2048, 'sha256': 'a' * 64}
        },
        'archived_sources': {name: {'path': prefix + '-sources/' + name, **fact} for name, fact in sources.items()},
        'archived_cogs': {
            name: {'path': prefix + '-cogs/' + name, 'size_bytes': fact['size_bytes'], 'sha256': fact['sha256']}
            for name, fact in targets.items()
        },
    }


def diagnostic(repository, env, identifier, problem, language, timeout):
    return {
        'execution_id': identifier,
        'task': 'focal',
        'problem': problem,
        'language': language,
        'exact_cause': m.MESSAGES[problem][language == 'en'],
        'source': 'develop_runtime_log',
    }


class HostedFocalSquareSevenWorkflowTest(unittest.TestCase):
    def setUp(self):
        work = tempfile.TemporaryDirectory()
        self.addCleanup(work.cleanup)
        self.root = Path(work.name)
        self.env = {
            'ONLINE_SUITE': m.SUITE,
            'ADDP_ONLINE_TEST_TENANT_ID': '2',
            'ADDP_ONLINE_FOCAL_SQUARE_SEVEN_NFS_ENGINE_ID': '3',
            'ADDP_ONLINE_TEST_TIMEOUT_SECONDS': '600',
            'ADDP_ONLINE_TEST_RUN_ID': m.fixture.PREFIX + '-123-1',
            'ADDP_ONLINE_ARTIFACT_DIR': str(self.root / 'evidence'),
        }
        self.enterContext(patch.object(prior, 'm', m))

    def run_case(self, client=None, worker=physical, diag=diagnostic):
        client = client or prior.Client()

        def reader(env, identifier, tenant, principal, dag):
            self.assertEqual((tenant, principal), (2, 33))
            self.assertIn(identifier, client.executions)
            name = dag['tasks'][-1]['params']['target_name']
            return m.expected_lineage(client.locators(), name, 'addp://engine/3/path/outputs/' + name + '?type=file')

        self.env.setdefault('ADDP_ONLINE_FOCAL_SQUARE_SEVEN_STARTED', str(time.monotonic()))
        with patch.object(m.raster, 'wait_lineage', return_value={'active': True}):
            return m.run_scenario(self.root, self.env, client, worker, diag, reader)

    def test_exact_18_success_60_failures_1251_full_reads_and_cleanup(self):
        calls = []

        def worker(*args):
            calls.append((args[2], list(args[3])))
            return physical(*args)

        report = self.run_case(worker=worker)
        self.assertEqual(
            (
                len(report['cases']),
                len(report['failed_parameters']),
                report['full_target_checks'],
                report['physical_verification_calls'],
            ),
            (18, 60, 1251, 79),
        )
        self.assertEqual(sum(len(names) for action, names in calls if action == 'verify'), 1251)
        self.assertEqual(calls[0], ('oracle', []))
        self.assertEqual(calls[-1], ('cleanup-oracle', []))
        self.assertEqual(set(report['archived_cogs']), set(m.fixture.CASES))
        self.assertEqual(len(list((self.root / 'evidence/focal-square-seven-terminal-responses').iterdir())), 156)
        self.assertTrue(report['oracle_cache_removed'])
        self.assertTrue(
            all(case['scan_identity']['parent_execution_id'] == case['execution_id'] for case in report['cases'])
        )
        self.assertEqual(
            {(row['problem'], row['language']) for row in report['failed_parameters']},
            {(problem, language) for problem in m.FAILURES for language in ('zh-cn', 'en')},
        )
        self.assertEqual(len(list((self.root / 'evidence/raster-focal-square-seven-lineage').iterdir())), 18)

    def test_every_dag_has_exact_window_and_weight_matrix_and_focal_failure(self):
        sources = {name: name for name in m.fixture.SOURCES}
        for name, (source, size, method, kernel, reclassify) in m.fixture.CASES.items():
            tasks = m.definition(sources, 'parent', name)['tasks']
            self.assertEqual(tasks[0]['params']['locator'], source)
            self.assertEqual(len(tasks), 4 if reclassify else 3)
            self.assertEqual(tasks[-2]['id'], 'focal')
            params = tasks[-2]['params']
            self.assertEqual((params['window_size'], params['method']), (size, method))
            if kernel:
                self.assertEqual(params['weights'], [list(row) for row in m.fixture.KERNELS[kernel]])
        self.assertEqual(set(m.MESSAGES), set(m.FAILURES))
        self.assertEqual(len(m.FAILURES), 30)
        for problem in m.FAILURES:
            for language in ('zh-cn', 'en'):
                tasks = m.definition(sources, 'parent', 'mean.cog.tif', invalid=problem, language=language)['tasks']
                self.assertEqual(tasks[-2]['id'], 'focal')
                self.assertEqual(tasks[-1]['params']['target_name'], f'failed-{problem}-{language}.cog.tif')
                json.dumps(tasks, allow_nan=False)

    def test_full_physical_inventory_sizes_and_immutable_archives_are_strict(self):
        names = list(m.fixture.CASES)
        good = physical(None, None, 'verify', names, 600)
        m.check_physical(good, good['sources'], {}, 'a' * 64, names)
        for field, bad_value in (
            ('full_cog_checks', True),
            ('oracle_sha256', 'x' * 64),
            ('archived_cogs', {}),
            ('actual_control_pairs_verified', []),
            ('runtime_workspaces_removed', False),
        ):
            with self.subTest(field=field), self.assertRaises(m.SuiteError):
                m.check_physical({**good, field: bad_value}, good['sources'], {}, 'a' * 64, names)
        changed = copy.deepcopy(good['targets'])
        changed[names[-1]]['sha256'] = 'b' * 64
        with self.assertRaisesRegex(m.SuiteError, 'previous complete COG changed'):
            m.check_physical(good, good['sources'], changed, 'a' * 64, names)

    def test_stage_scan_language_and_cleanup_are_strict(self):
        for fault in ('focal', 'scan', 'language', 'cleanup'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                self.env['ADDP_ONLINE_ARTIFACT_DIR'] = directory
                client, worker, diag = prior.Client(), physical, diagnostic
                if fault == 'focal':

                    def diag(*args):
                        return {**diagnostic(*args), 'task': 'save'}

                elif fault == 'scan':
                    client.added_scan = True
                elif fault == 'language':
                    client.rewrite = True
                else:

                    def worker(*args):
                        return {} if args[2] == 'cleanup-oracle' else physical(*args)

                expected = {
                    'focal': 'wrong exact failure',
                    'scan': 'failed execution added scan',
                    'language': 'query language rewrote',
                    'cleanup': 'oracle cleanup proof',
                }
                with self.assertRaisesRegex(m.SuiteError, expected[fault]):
                    self.run_case(client, worker, diag)

    def test_budget_includes_prior_initialization_and_stops_at_exhaustion(self):
        self.env['ADDP_ONLINE_TEST_TIMEOUT_SECONDS'] = '601'
        with self.assertRaisesRegex(m.SuiteError, 'exactly 600'):
            self.run_case()
        self.env['ADDP_ONLINE_TEST_TIMEOUT_SECONDS'] = '600'
        self.env['ADDP_ONLINE_FOCAL_SQUARE_SEVEN_STARTED'] = str(time.monotonic() - 601)
        with self.assertRaisesRegex(m.SuiteError, 'budget exhausted'):
            self.run_case()
