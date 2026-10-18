import copy
import importlib
from pathlib import Path
from types import SimpleNamespace
import unittest

m = importlib.import_module('scripts.test.raster-contour-persistence-online')
persistence = importlib.import_module('scripts.utils.online_vector_persistence')


def item(name='levels.gpkg', identifier=20):
    return {'id': identifier, 'item_type': 'file', 'full_name': 'outputs/' + name,
        'fingerprint': name, 'size_bytes': 98304, 'scanned_at': '2026-10-18T00:00:00Z',
        'attributes': {'item': {'layout': 'single', 'data_type': 'container', 'format': 'geopackage'},
            'type_info': {'container': {'children': [{'name': name[:-5], 'child_kind': 'layer', 'native': {'table': name[:-5]}}]}}}}


def physical(name):
    return {'size_bytes': 98304, 'sha256': name, 'layer_name': name[:-5], 'feature_count': len(m.fixture.TARGETS[name]),
            'levels_metres': m.fixture.TARGETS[name], 'vertex_count': 100, 'crs': 'EPSG:3857'}


def execution(source, target, scan='scan-levels.gpkg'):
    facts = {'schema_version': 'addp.lineage-facts/v1', 'inputs': [{'locator': source}],
             'outputs': [{'locator': target, 'write_mode': 'replace'}]}
    return {'status': 'success', 'outputs': {'save': {'resource': {'locator': target, 'type': 'file', 'write_mode': 'replace'}}},
        'metadata': {'lineage_facts': facts, 'result': {'meta_scan_runs': [{'status': 'submitted', 'target_locator': target, 'execution_id': scan}]}}}


class Client:
    def __init__(self, source_name='dem.tif'):
        self.source_name = source_name
        self.source = {'id': 10, 'item_type': 'file', 'full_name': source_name, 'fingerprint': 'dem', 'size_bytes': 24991}
        self.targets = {}; self.executions = {}; self.calls = []; self.actor = 33
    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, body))
        if path.endswith('/auth/context'):
            data = {'principal': {'id': 33, 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                'token': {'type': 'first_party_access_token'}, 'authorization': {'role_assignments': [
                    {'role_key': 'contour-consumer', 'permissions': sorted(m.raster.EDITOR_PERMISSIONS)}]}}
        elif path.endswith('/scan/run/manual'):
            if body['targets'] != [f'addp://engine/3/path/{self.source_name}?type=file']: raise AssertionError(body)
            data = {'execution_id': 'source-scan'}
        elif '/meta/executions/' in path:
            # Meta exposes ScanExecutionResponse/Observation, never private actor fields.
            data = {'status': 'success', 'tenant_id': 2, 'triggered_by': self.actor}
        elif path.endswith('/workflow-engines'):
            data = [{'id': 7, 'engine_type': 'geopython_workflow', 'connection_status': 'online'}]
        elif path == '/api/v1/develop/executions' and method == 'POST':
            tasks = body['content']['workflow_definition']['tasks']; name = tasks[-1]['params']['target_name']
            target = 'addp://engine/3/path/outputs/' + name + '?type=file'
            identifier = 'exec-' + name
            self.executions[identifier] = execution(tasks[0]['params']['locator'], target, 'scan-' + name)
            self.targets[len(self.targets) + 20] = item(name, len(self.targets) + 20)
            data = {'execution_id': identifier}
        elif '/develop/executions/' in path:
            data = self.executions[path.rsplit('/', 1)[-1]]
        elif '/monitor/executions/' in path:
            data = {'module': 'develop', **self.executions[path.rsplit('/', 1)[-1]]}
        elif path.endswith('/engines/3/items'):
            data = [self.source, *self.targets.values()]
        elif '/meta/items/' in path:
            identifier = int(path.rsplit('/', 1)[-1]); data = self.source if identifier == 10 else self.targets[identifier]
        elif '/meta/lineage/graph?' in path:
            identifier = int(path.split('item_id=')[1].split('&')[0]); target = self.targets[identifier]
            data = {'edges': [{'source': {'item_id': 10}, 'target': {'item_id': identifier}, 'status': 'active',
                    'relation_kind': 'derive', 'evidence': {'execution_id': 'exec-' + target['full_name'].split('/')[-1]}}]}
        else:
            raise AssertionError((method, path, body))
        return SimpleNamespace(payload=copy.deepcopy(data))


class ContourPersistenceTest(unittest.TestCase):
    def test_existing_save_contract_and_two_public_contour_modes(self):
        for name in m.fixture.TARGETS:
            tasks = m.definition('source', 'parent', name)['tasks']
            self.assertEqual([t['operator'] for t in tasks], ['raster_load', 'raster_contour', 'save'])
            self.assertEqual(tasks[-1]['params'], {'input_df': {'$ref': 'contour'}, 'target_parent_locator': 'parent', 'target_name': name, 'mode': 'replace'})
            self.assertEqual(tasks[1]['params']['elevation_unit'], 'metre')
            self.assertEqual('levels' in tasks[1]['params'], name == 'levels.gpkg')
        with self.assertRaises(m.SuiteError): m.definition('source', 'parent', '../other.gpkg')

    def test_exact_file_scan_stable_output_and_lineage_are_required(self):
        original = execution('source', 'target')
        persistence.validate_success(original, 'source', 'target')
        for mutate in (
            lambda e: e['outputs']['save']['resource'].update(type='object'),
            lambda e: e['metadata']['lineage_facts']['inputs'][0].update(locator='sibling'),
            lambda e: e['metadata']['lineage_facts']['outputs'][0].update(write_mode='create'),
            lambda e: e['metadata']['result']['meta_scan_runs'][0].update(target_locator='parent'),
            lambda e: e['metadata']['result']['meta_scan_runs'].clear(),
        ):
            value = copy.deepcopy(original); mutate(value)
            with self.subTest(mutate=mutate), self.assertRaises(m.SuiteError): persistence.validate_success(value, 'source', 'target')

    def test_sizes_are_positive_integers_and_container_does_not_inherit_layer_spatial(self):
        persistence.validate_item(item(), physical('levels.gpkg'), 'levels.gpkg')
        for size in (True, 0, -1, 98304., '98304', 98303):
            value = item(); value['size_bytes'] = size
            with self.subTest(size=size), self.assertRaises(m.SuiteError): persistence.validate_item(value, physical('levels.gpkg'), 'levels.gpkg')
        for mutate in (lambda e: e['attributes'].update(capabilities={'spatial': {'srid': 3857}}),
                       lambda e: e['attributes']['type_info'].update(table={'fields': []}),
                       lambda e: e['attributes']['item'].update(data_type='table')):
            value = item(); mutate(value)
            with self.assertRaises(m.SuiteError): persistence.validate_item(value, physical('levels.gpkg'), 'levels.gpkg')

    def test_complete_api_chain_uses_ordinary_identity_and_does_not_rescan_outputs_manually(self):
        client = Client()
        def verify(repository, env, names):
            return {'source': {'sha256': 'source', 'size_bytes': 24991}, 'runtime_workspaces_removed': True,
                    'targets': {name: physical(name) for name in names}}
        env = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_CONTOUR_NFS_ENGINE_ID': '3', 'ADDP_ONLINE_TEST_RUN_ID': 'contour-123-1'}
        report = m.run_scenario(Path('.'), env, client, verify)
        self.assertEqual((report['suite'], report['result'], report['run_id']), (m.SUITE, 'passed', 'contour-123-1'))
        self.assertEqual(len(report['cases']), 2)
        self.assertEqual([c['target_item_id'] for c in report['cases']], [20, 21])
        self.assertEqual(len([c for c in client.calls if c[0] == 'POST' and '/scan/' in c[1]]), 1)
        self.assertTrue(report['source_unchanged'])

    def test_scan_identity_mismatch_blocks_physical_or_lineage_acceptance(self):
        client = Client(); client.actor = 34
        env = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_CONTOUR_NFS_ENGINE_ID': '3', 'ADDP_ONLINE_TEST_RUN_ID': 'contour-123-1'}
        with self.assertRaisesRegex(m.SuiteError, 'identity'):
            m.run_scenario(Path('.'), env, client, lambda *args: self.fail('must stop before acceptance'))

    def test_scan_initiator_requires_a_positive_integer_from_the_safe_projection(self):
        env = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_CONTOUR_NFS_ENGINE_ID': '3', 'ADDP_ONLINE_TEST_RUN_ID': 'contour-123-1'}
        for actor in (None, True, 0, -1, '33', 33.):
            client = Client(); client.actor = actor
            with self.subTest(actor=actor), self.assertRaisesRegex(m.SuiteError, 'identity'):
                m.run_scenario(Path('.'), env, client, lambda *args: self.fail('must stop before acceptance'))

    def test_second_save_cannot_change_first_file(self):
        client = Client()
        def verify(repository, env, names):
            data = {name: physical(name) for name in names}
            if len(names) == 2: data['levels.gpkg']['sha256'] = 'changed'
            return {'targets': data, 'runtime_workspaces_removed': True}
        env = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_CONTOUR_NFS_ENGINE_ID': '3', 'ADDP_ONLINE_TEST_RUN_ID': 'contour-123-1'}
        with self.assertRaisesRegex(m.SuiteError, 'earlier GeoPackage'): m.run_scenario(Path('.'), env, client, verify)

    def test_missing_runtime_workspace_proof_is_not_a_pass(self):
        client = Client()
        env = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_CONTOUR_NFS_ENGINE_ID': '3', 'ADDP_ONLINE_TEST_RUN_ID': 'contour-123-1'}
        with self.assertRaisesRegex(m.SuiteError, 'workspace cleanup'):
            m.run_scenario(Path('.'), env, client, lambda *args: {'targets': {'levels.gpkg': physical('levels.gpkg')}})
