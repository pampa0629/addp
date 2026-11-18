import copy
import importlib
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

scene = importlib.import_module('scripts.test.raster-hillshade-orthogonal-persistence-online')
fixture = scene.fixture
platform = importlib.import_module('scripts.test.raster-slope-orthogonal-persistence-online_test')


class HillshadeOrthogonalScenarioTest(unittest.TestCase):
    def test_exact_matrix_and_all_dags_use_the_existing_hillshade_operator(self):
        sources = {name: f'addp://engine/3/path/{name}?type=file' for name in fixture.SOURCES}
        parent = 'addp://engine/3/path/outputs?type=directory'
        targets = tuple(fixture.TARGETS)
        self.assertEqual(scene.COUNTS, (16, 34, 680, 51))
        self.assertEqual(len(targets), 16)
        self.assertEqual(len(scene.FAILURES), 17)
        self.assertEqual(scene.FAILURES['unit-empty'], ('unit', 'north.tif', {'elevation_unit': ''}))
        self.assertNotIn('unit-bool', scene.FAILURES)
        self.assertEqual(len(set(sources.values())), len(sources))

        for name in targets:
            dag = scene.definition(sources, parent, name)
            load, hillshade, save = dag['tasks']
            self.assertEqual([load['operator'], hillshade['operator'], save['operator']],
                             ['raster_load', 'raster_hillshade', 'raster_save'])
            self.assertEqual(load['params']['locator'], sources[fixture.CASES[name][0]])
            self.assertEqual(hillshade['params'], {'input_raster': {'$ref': 'source'}, **fixture.parameters(name)})
            self.assertEqual(save['params']['target_name'], name)
            self.assertEqual(save['params']['profile'], 'cog')
            self.assertEqual(save['params']['overview_resampling'], 'nearest')
            self.assertEqual(save['params']['write_mode'], 'create')

        negative_targets = set()
        for problem, (_, source, _) in scene.FAILURES.items():
            for language in ('zh-cn', 'en'):
                dag = scene.definition(sources, parent, targets[0], invalid=problem, language=language)
                load, hillshade, save = dag['tasks']
                self.assertEqual(load['params']['locator'], sources[source])
                self.assertEqual(hillshade['operator'], 'raster_hillshade')
                if problem == 'unit-empty':
                    self.assertEqual(hillshade['params']['elevation_unit'], '')
                name = save['params']['target_name']
                self.assertNotIn(name, negative_targets)
                self.assertNotIn(name, targets)
                negative_targets.add(name)
                fact = scene.diagnostic_fact('execution', problem, language)
                self.assertEqual(fact['task'], 'hillshade')
                self.assertIn('DEM hillshade' if language == 'en' else 'DEM 地形晕渲', fact['exact_cause'])

    def test_uses_shared_scan_metadata_lineage_and_budget_runner(self):
        with patch.object(scene.view, 'run_scenario', return_value={'result': 'passed'}) as runner:
            report = scene.run_scenario(Path.cwd(), {}, object())
        self.assertEqual(report, {'result': 'passed'})
        profile = runner.call_args.kwargs['profile']
        self.assertIs(profile.source_scan_client, scene.orth.source_scan_client)
        self.assertIs(profile.check_scan_inventory, scene.orth.check_scan_inventory)
        self.assertIs(profile.ViewshedMetadata.fixture, fixture)
        self.assertEqual(profile.fixture.ANCHOR, 'ADDP_ONLINE_HILLSHADE_ORTHOGONAL_ORACLE_ANCHOR_SHA256')

    def test_batch_keeps_missing_evidence_pending_and_rejects_wrong_execution(self):
        relations = [{'source_item_id': 10, 'target_item_id': target, 'execution_id': str(target)}
                     for target in (21, 22)]
        queries = []

        def edges(client, target, direction):
            self.assertEqual(direction, 'upstream')
            queries.append(target)
            if target == 21 and queries.count(target) == 1:
                return []
            return [{'source': {'item_id': 10}, 'target': {'item_id': target},
                     'evidence': {'execution_id': str(target)}}]

        with patch.object(scene.view, 'active_derivations', side_effect=edges), patch.object(scene.time, 'sleep') as sleep:
            scene.collect_public_lineage(object(), relations, lambda: 2)
            self.assertEqual(queries, [21, 22, 21])
            sleep.assert_called_once_with(1)
        with patch.object(scene.view, 'active_derivations', return_value=[
            {'source': {'item_id': 10}, 'target': {'item_id': 21}, 'evidence': {'execution_id': 'old-run'}}
        ]):
            with self.assertRaisesRegex(scene.SuiteError, 'exactly one current'):
                scene.collect_public_lineage(object(), relations, lambda: 2)

    def test_revocation_archives_exact_receipts_and_observations_for_all_grants(self):
        registration = importlib.import_module('scripts.test.online-engine-registration')
        expiry = '2026-11-18T13:10:00Z'
        revoked_at = '2026-11-18T13:09:40Z'
        observed_at = '2026-11-18T13:09:41Z'
        paths = (*fixture.SOURCES, *('outputs/'+name for name in fixture.TARGETS))
        self.assertEqual(len(paths), 36)
        for fault in (None, 'wrong-receipt', 'expired', 'still-visible'):
            with self.subTest(fault=fault), tempfile.TemporaryDirectory() as directory:
                rows = [dict(request_id=str(index), engine=3, target={'name': name}, confirmed=True)
                        for index, name in enumerate(paths, 1)]

                class Issuer:
                    def request(self, method, path, expected, body):
                        identifier = path.split('/')[-2]
                        return SimpleNamespace(status=200, payload={
                            'request_id': 'stale' if identifier == '1' and fault == 'wrong-receipt' else identifier,
                            'revoked_request_ids': [identifier], 'revoked_by_principal_id': '44',
                            'revoked_by_membership_id': '55', 'reason': body['reason'],
                            'revoked_at': expiry if identifier == '1' and fault == 'expired' else revoked_at})

                class Consumer:
                    def request(self, method, path, expected, body):
                        target, = body['targets']
                        return SimpleNamespace(payload={'observed_at': observed_at, 'targets': [
                            dict(target=target, visible=fault == 'still-visible' and target == rows[0]['target'])
                        ]})

                owner = scene.ViewshedMetadata.__new__(scene.ViewshedMetadata)
                owner.report = {'issued': [{'request_id': r['request_id']} for r in rows],
                                'pending': rows[:], 'facts': [], 'cleanup': 'pending'}
                owner.expires_at = expiry
                owner.path = Path(directory)/'metadata.json'
                with patch.object(registration, 'source_grant_participants', return_value=('44', '33')):
                    owner.grants = registration.ExactFileMetadataGrants(
                        Issuer(), Consumer(), 2, on_change=owner.checkpoint, on_evidence=owner.capture)
                owner.grants.pending = rows[:]
                if fault:
                    with self.assertRaisesRegex(registration.MetadataGrantError, 'revoke_or_visibility_failed'):
                        owner.cleanup()
                    self.assertEqual(owner.report['pending'], rows[:1])
                    self.assertEqual(owner.report['cleanup'], 'failed')
                    if fault == 'still-visible':
                        self.assertEqual(owner.report['revocations']['1']['receipt']['request_id'], '1')
                        self.assertNotIn('visibility', owner.report['revocations']['1'])
                else:
                    owner.cleanup()
                    self.assertEqual(owner.report['pending'], [])
                    self.assertEqual(owner.report['cleanup'], {'grants_revoked': True, 'metadata_hidden': True})
                    self.assertEqual(len(owner.report['revocations']), 36)
                    for row in rows:
                        fact = owner.report['revocations'][row['request_id']]
                        self.assertEqual(fact['receipt']['request_id'], row['request_id'])
                        self.assertEqual(fact['visibility']['targets'], [dict(target=row['target'], visible=False)])

    def test_complete_platform_matrix_and_failure_inventory(self):
        frozen = {
            name: {
                'artifact': fixture.artifact_expectation(name),
                'valid_pixels': 0 if name.startswith('invalid') else 6,
                'zero_pixels': 0,
                'unit': '',
            }
            for name in fixture.TARGETS
        }
        source_bytes = {name: {'size_bytes': 4096, 'sha256': 'a'*64} for name in fixture.SOURCES}
        for fault in (None, 'negative-scan', 'extra-meta-relationship', 'language', 'size', 'budget'):
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
                    'ADDP_ONLINE_TEST_RUN_ID': 'hillshade-orthogonal-123-1',
                    'ADDP_ONLINE_TEST_TENANT_ID': '2',
                    'ADDP_ONLINE_HILLSHADE_ORTHOGONAL_NFS_ENGINE_ID': '3',
                    'ADDP_ONLINE_TEST_TIMEOUT_SECONDS': '600',
                    'ADDP_ONLINE_ARTIFACT_DIR': directory,
                }
                reads = []
                clock = [100.0]
                cleanup_budget = []

                class Metadata(platform.Metadata):
                    def __init__(self, env, client, engine, identity, scan, remaining, *, grant_remaining):
                        super().__init__(env, client)
                        self.remaining = remaining
                        self.expiry = grant_remaining()

                    def cleanup(self):
                        cleanup_budget.append(self.remaining())
                        self_outer.assertGreater(self.expiry, 570)
                        return super().cleanup()

                self_outer = self

                def physical(repository, env, action, names, timeout):
                    reads.append((action, len(names)))
                    if fault == 'budget' and action == 'verify':
                        clock[0] = 671.0
                    return platform.physical(repository, env, action, names, timeout)

                def lineage(_env, identifier, tenant, principal, dag):
                    self.assertEqual((tenant, principal), (2, 33))
                    self.assertEqual(client.executions[identifier]['execution_config']['content']['workflow_definition'], dag)
                    name = dag['tasks'][-1]['params']['target_name']
                    target = 'addp://engine/3/path/outputs/'+name+'?type=file'
                    return copy.deepcopy(scene.expected_lineage(dag['tasks'][0]['params']['locator'], target))

                def run():
                    original_graph = scene.view.active_derivations

                    def assert_batch(client, item, direction):
                        if direction == 'upstream':
                            self.assertEqual(sum(status['status'] == 'success' for status in platform_client.executions.values()), 16)
                            self.assertEqual(len(platform_client.executions), 16)
                        return original_graph(client, item, direction)

                    platform_client = client
                    with patch.object(scene.view.support, 'GatewayClient', return_value=initializer), \
                         patch.object(scene.time, 'monotonic', side_effect=lambda: clock[0]), \
                         patch.object(scene.view, 'active_derivations', side_effect=assert_batch):
                        return scene.run_scenario(Path.cwd(), env, client, physical, platform.diagnostic,
                                                  lineage, Metadata, hard_deadline=700.0)

                if fault:
                    with self.assertRaises(scene.SuiteError):
                        run()
                    if fault == 'budget':
                        self.assertEqual(cleanup_budget, [28.0])
                        self.assertEqual(reads[-1], ('cleanup-oracle', 0))
                        self.assertNotIn('"result":"passed"', (Path(directory)/(scene.PREFIX+'-progress.json')).read_text())
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
