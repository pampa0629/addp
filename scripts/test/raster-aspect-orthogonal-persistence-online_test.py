import copy
import importlib
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

scene = importlib.import_module('scripts.test.raster-aspect-orthogonal-persistence-online')
fixture = scene.fixture
platform = importlib.import_module('scripts.test.raster-slope-orthogonal-persistence-online_test')


class AspectOrthogonalScenarioTest(unittest.TestCase):
    def test_exact_matrix_and_all_dags_use_the_existing_aspect_operator(self):
        sources = {name: f'addp://engine/3/path/{name}?type=file' for name in fixture.SOURCES}
        parent = 'addp://engine/3/path/outputs?type=directory'
        targets = tuple(fixture.TARGETS)
        self.assertEqual(scene.COUNTS, (16, 34, 680, 51))
        self.assertEqual(len(targets), 16)
        self.assertEqual(len(scene.FAILURES), 17)
        self.assertEqual(scene.FAILURES['unit-empty'], ('unit', 'north-n.tif', {'elevation_unit': ''}))
        self.assertNotIn('unit-bool', scene.FAILURES)
        self.assertEqual(len(set(sources.values())), len(sources))

        for name in targets:
            dag = scene.definition(sources, parent, name)
            load, aspect, save = dag['tasks']
            self.assertEqual([load['operator'], aspect['operator'], save['operator']],
                             ['raster_load', 'raster_aspect', 'raster_save'])
            self.assertEqual(load['params']['locator'], sources[fixture.CASES[name][0]])
            self.assertEqual(aspect['params'], {'input_raster': {'$ref': 'source'}, **fixture.parameters(name)})
            self.assertEqual(save['params']['target_name'], name)
            self.assertEqual(save['params']['profile'], 'cog')
            self.assertEqual(save['params']['overview_resampling'], 'nearest')
            self.assertEqual(save['params']['write_mode'], 'create')

        negative_targets = set()
        for problem, (_, source, _) in scene.FAILURES.items():
            for language in ('zh-cn', 'en'):
                dag = scene.definition(sources, parent, targets[0], invalid=problem, language=language)
                load, aspect, save = dag['tasks']
                self.assertEqual(load['params']['locator'], sources[source])
                self.assertEqual(aspect['operator'], 'raster_aspect')
                name = save['params']['target_name']
                self.assertNotIn(name, negative_targets)
                self.assertNotIn(name, targets)
                negative_targets.add(name)
                fact = scene.diagnostic_fact('execution', problem, language)
                self.assertEqual(fact['task'], 'aspect')
                self.assertIn('DEM aspect' if language == 'en' else 'DEM 坡向', fact['exact_cause'])

    def test_uses_shared_scan_metadata_lineage_and_budget_runner(self):
        with patch.object(scene.view, 'run_scenario', return_value={'result': 'passed'}) as runner:
            report = scene.run_scenario(Path.cwd(), {}, object())
        self.assertEqual(report, {'result': 'passed'})
        profile = runner.call_args.kwargs['profile']
        self.assertIs(profile.source_scan_client, scene.orth.source_scan_client)
        self.assertIs(profile.check_scan_inventory, scene.orth.check_scan_inventory)
        self.assertIs(profile.ViewshedMetadata.fixture, fixture)
        self.assertEqual(profile.fixture.ANCHOR, 'ADDP_ONLINE_ASPECT_ORTHOGONAL_ORACLE_ANCHOR_SHA256')

    def test_complete_platform_matrix_and_failure_inventory(self):
        frozen = {
            name: {
                'artifact': fixture.artifact_expectation(name),
                'valid_pixels': 0 if name.startswith('invalid') else 6,
                'zero_pixels': 0,
                'unit': 'degree',
            }
            for name in fixture.TARGETS
        }
        source_bytes = {name: {'size_bytes': 4096, 'sha256': 'a'*64} for name in fixture.SOURCES}
        for fault in (None, 'negative-scan', 'extra-meta-relationship', 'language', 'size'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory, patch.multiple(
                platform, scene=scene, fixture=fixture, FROZEN=frozen, SOURCES=source_bytes,
            ):
                client = platform.Client(fault)
                initializer = platform.SourceClient(client)
                env = {
                    'ONLINE_SUITE': scene.SUITE,
                    'GATEWAY_URL': 'http://fixture.invalid',
                    'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'fixture-consumer-token',
                    'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'fixture-source-token',
                    'ADDP_ONLINE_TEST_RUN_ID': 'aspect-orthogonal-123-1',
                    'ADDP_ONLINE_TEST_TENANT_ID': '2',
                    'ADDP_ONLINE_ASPECT_ORTHOGONAL_NFS_ENGINE_ID': '3',
                    'ADDP_ONLINE_TEST_TIMEOUT_SECONDS': '600',
                    'ADDP_ONLINE_ARTIFACT_DIR': directory,
                }
                reads = []

                def physical(repository, env, action, names, timeout):
                    reads.append((action, len(names)))
                    return platform.physical(repository, env, action, names, timeout)

                def lineage(_env, identifier, tenant, principal, dag):
                    self.assertEqual((tenant, principal), (2, 33))
                    self.assertEqual(client.executions[identifier]['execution_config']['content']['workflow_definition'], dag)
                    name = dag['tasks'][-1]['params']['target_name']
                    target = 'addp://engine/3/path/outputs/'+name+'?type=file'
                    return copy.deepcopy(scene.expected_lineage(dag['tasks'][0]['params']['locator'], target))

                def run():
                    with patch.object(scene.view.support, 'GatewayClient', return_value=initializer):
                        return scene.run_scenario(Path.cwd(), env, client, physical, platform.diagnostic,
                                                  lineage, platform.Metadata)

                if fault:
                    with self.assertRaises(scene.SuiteError):
                        run()
                    continue
                result = run()
                self.assertEqual(result['result'], 'passed')
                self.assertEqual((len(result['cases']), len(result['failed_parameters']),
                                  result['full_target_checks'], result['physical_verification_calls']), scene.COUNTS)
                self.assertEqual(reads, [('prepare-oracle', 0), *[('verify', n) for n in range(1, 17)],
                                         *([('verify', 16)]*34), ('cleanup-oracle', 0)])
                self.assertEqual(len(result['archived_cogs']), 16)
                for case in result['cases']:
                    self.assertEqual(case['meta_relation']['execution_id'], case['execution_id'])
                    self.assertEqual(case['scan_identity']['parent_execution_id'], case['execution_id'])
