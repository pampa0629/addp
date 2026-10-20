import copy
import hashlib
import importlib
import itertools
from pathlib import Path
from types import SimpleNamespace
import unittest

m = importlib.import_module('scripts.test.raster-band-recommendation-workflow-online')
ENV = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_BAND_RECOMMENDATION_NFS_ENGINE_ID': '3',
       'ADDP_ONLINE_TEST_RUN_ID': 'band-recommendation-123-1'}


def expectation(name):
    bands = sorted(m.fixture.CASES[name]['bands'])
    size = len(bands)
    if name == 'orthogonal':
        statistics = [{'band': band, 'mean': 0., 'stddev': 1., 'score_status': 'eligible'} for band in bands]
        matrix = [[1. if i == j or {i, j} == {0, 3} else 0. for j in range(size)] for i in range(size)]
        ranked = [{'rank': i + 1, 'bands': combination, 'score_status': 'unbounded' if i < 2 else 'finite',
                   'score': None if i < 2 else 3.} for i, combination in enumerate([[1, 2, 3], [2, 3, 4], [1, 2, 4], [1, 3, 4]])]
        excluded = []
    else:
        statistics = [{'band': band, 'mean': 4., 'stddev': float(band) if band != 16 else 0.,
                       'score_status': 'eligible' if band != 16 else 'zero_variance'} for band in bands]
        matrix = [[None if 16 in (i + 1, j + 1) else 1. for j in range(size)] for i in range(size)]
        ranked, excluded = [], []
        for combination in itertools.combinations(bands, 3):
            if 16 in combination:
                excluded.append({'bands': list(combination), 'reason': 'zero_variance'})
            else:
                ranked.append({'bands': list(combination), 'score': sum(combination) / 3., 'score_status': 'finite'})
        ranked.sort(key=lambda row: (-row['score'], row['bands']))
        for rank, row in enumerate(ranked, 1):
            row['rank'] = rank
    return {'method': 'oif', 'strategy': 'full', 'value_domain': 'stored', 'candidate_bands': bands,
            'common_valid_count': 8, 'common_invalid_count': 0, 'band_statistics': statistics,
            'correlation_matrix': matrix, 'rankings': ranked, 'excluded_combinations': excluded, 'recommended_bands': ranked[0]['bands']}


def physical(repository, env):
    sources = {name: {'sha256': hashlib.sha256(name.encode()).hexdigest(), 'size_bytes': 4096} for name in m.fixture.SOURCES}
    return {'sources': sources, 'source_physical': {name: {**fact, 'full_pixels_and_mask_verified': True, 'grid_crs_scale_verified': True}
             for name, fact in sources.items()}, 'no_outputs': True, 'runtime_workspaces_removed': True,
            'expectations': {name: expectation(name) for name in m.fixture.CASES}}


class Client:
    actor = 33
    source_actor = 33
    mutation = None
    adds_scan = False
    changed_meta = False
    changed_graph = False
    monitor_lineage = False
    def __init__(self):
        self.sources = {name: {'id': i + 10, 'full_name': name, 'item_type': 'file', 'size_bytes': 4096,
                              'fingerprint': name, 'scanned_at': '2026-10-20T00:00:00Z'} for i, name in enumerate(m.fixture.SOURCES)}
        self.executions = {}
        self.scans = []
        self.calls = []

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, copy.deepcopy(body)))
        if path.endswith('/auth/context'):
            data = {'principal': {'id': 33, 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                    'token': {'type': 'first_party_access_token'}, 'authorization': {'role_assignments': [
                        {'role_key': 'raster-consumer', 'permissions': sorted(m.raster.EDITOR_PERMISSIONS)}]}}
        elif path.endswith('/scan/run/manual'):
            assert body['targets'] == ['addp://engine/3/path/' + name + '?type=file' for name in m.fixture.SOURCES]
            self.scans.append('source-scan')
            data = {'execution_id': 'source-scan'}
        elif '/meta/executions/' in path:
            data = {'status': 'success', 'triggered_by': self.source_actor}
        elif path.endswith('/workflow-engines'):
            data = [{'id': 7, 'engine_type': 'geopython_workflow', 'connection_status': 'online'}]
        elif path == '/api/v1/develop/executions' and method == 'POST':
            dag = body['content']['workflow_definition']
            source = dag['tasks'][0]['params']['locator']
            bands = dag['tasks'][-1]['params']['bands']
            name = next((name for name, case in m.fixture.CASES.items()
                         if bands == case['bands'] and f'/path/{case["source"]}?' in source), None)
            identifier = 'exec-' + str(len(self.executions) + 1)
            execution = {'execution_id': identifier, 'module': 'develop', 'triggered_by': self.actor,
                         'status': 'success' if name else 'failed', 'outputs': {},
                         'execution_config': {'engine_id': 7, 'content': {'workflow_definition': copy.deepcopy(dag)}},
                         'metadata': {'result': {'summary': {'has_result': True}, 'final_result': expectation(name)}} if name else {'result': {}}}
            if self.mutation is not None:
                self.mutation(execution)
            self.executions[identifier] = execution
            if self.adds_scan:
                self.scans.append('unexpected-' + identifier)
            data = {'execution_id': identifier}
        elif '/develop/executions/' in path:
            data = self.executions[path.rsplit('/', 1)[-1]]
        elif '/monitor/executions/' in path:
            execution = self.executions[path.rsplit('/', 1)[-1]]
            data = {'module': 'develop', 'status': execution['status'], 'triggered_by': self.actor,
                    'metadata': {'lineage_facts': {'bad': True}} if self.monitor_lineage else {}}
        elif path.endswith('/engines/3/items'):
            data = list(self.sources.values())
        elif '/meta/items/' in path:
            data = copy.deepcopy(next(value for value in self.sources.values() if value['id'] == int(path.rsplit('/', 1)[-1])))
            if self.changed_meta and self.executions:
                data['size_bytes'] += 1
        elif '/meta/lineage/graph?' in path:
            assert 'direction=both' in path
            data = {'nodes': [], 'edges': [{'relation_kind': 'derive'}] if self.changed_graph and self.executions else [], 'truncated': False}
        elif '/meta/scan/runs?' in path:
            data = {'total': len(self.scans), 'items': [{'execution_id': identifier} for identifier in self.scans]}
        else:
            raise AssertionError((method, path, body))
        return SimpleNamespace(payload=copy.deepcopy(data))


class BandRecommendationWorkflowTest(unittest.TestCase):
    def test_three_readonly_dags_and_five_failures_use_exact_sources_and_no_save(self):
        sources = {name: name for name in m.fixture.SOURCES}
        for invalid, inventory in ((False, m.fixture.CASES), (True, m.fixture.FAILURES)):
            for name, case in inventory.items():
                tasks = m.definition(sources, name, invalid=invalid)['tasks']
                self.assertEqual([task['operator'] for task in tasks], ['raster_load', 'raster_recommend_band_combination'])
                self.assertEqual(tasks[0]['params'], {'locator': sources[case['source']]})
                self.assertEqual(tasks[-1]['params'], {'input_raster': {'$ref': 'source'}, 'bands': case['bands']})
        with self.assertRaises(m.SuiteError):
            m.definition(sources, '../outside.tif')

    def test_only_source_scan_full_objects_twelve_permissions_and_no_derive(self):
        client = Client()
        report = m.run_scenario(Path('.'), ENV, client, physical)
        self.assertEqual(report['result'], 'passed')
        self.assertEqual(len(report['identity']['permissions_verified']), 12)
        self.assertEqual(len(report['cases']), 3)
        self.assertEqual(len(report['failed_parameters']), 5)
        self.assertEqual(client.scans, ['source-scan'])
        self.assertEqual(report['cases'][0]['json_result'], report['cases'][1]['json_result'])
        self.assertEqual(len(report['cases'][0]['json_result']['rankings']), 455)
        self.assertEqual(len(report['cases'][0]['json_result']['excluded_combinations']), 105)
        self.assertTrue(report['no_resource_outputs_or_derive'] and report['only_source_scan_verified'])

    def test_wrong_user_source_snapshot_runtime_publication_and_scan_are_rejected(self):
        mutations = [lambda e: e.update(triggered_by=True), lambda e: e.update(triggered_by='33'),
                     lambda e: e['execution_config'].update(engine_id=8),
                     lambda e: e['execution_config']['content']['workflow_definition']['tasks'][0]['params'].update(locator='wrong'),
                     lambda e: e.update(outputs={'save': {'resource': {'locator': 'wrong'}}}),
                     lambda e: e['metadata'].update(lineage_facts={'bad': True}),
                     lambda e: e['metadata']['result'].update(meta_scan_runs=[{'execution_id': 'wrong'}])]
        for mutation in mutations:
            client = Client()
            client.mutation = mutation
            with self.subTest(mutation=mutation), self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'), ENV, client, physical)
        for attr, value in [('actor', 34), ('source_actor', True), ('adds_scan', True), ('changed_meta', True),
                            ('changed_graph', True), ('monitor_lineage', True)]:
            client = Client()
            setattr(client, attr, value)
            with self.subTest(attr=attr), self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'), ENV, client, physical)

    def test_failure_must_not_publish_resources_or_scan(self):
        for fault in ('outputs', 'lineage', 'scan'):
            def mutation(execution):
                if execution['status'] != 'failed':
                    return
                if fault == 'outputs':
                    execution['outputs'] = {'unexpected': {}}
                elif fault == 'lineage':
                    execution['metadata']['lineage_facts'] = {'bad': True}
                else:
                    execution['metadata']['result']['meta_scan_runs'] = [{'execution_id': 'wrong'}]
            client = Client()
            client.mutation = mutation
            with self.subTest(fault=fault), self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'), ENV, client, physical)

    def test_complete_tail_result_null_and_counts_fail_closed(self):
        for fault in ('last-rank', 'nan', 'count-bool', 'missing', 'null-score'):
            def mutation(execution):
                if execution['status'] != 'success':
                    return
                value = execution['metadata']['result']['final_result']
                if fault == 'last-rank':
                    value['rankings'][-1]['bands'] = [1, 2, 3]
                elif fault == 'nan':
                    value['band_statistics'][0]['mean'] = float('nan')
                elif fault == 'count-bool':
                    value['common_valid_count'] = True
                elif fault == 'missing':
                    value.pop('correlation_matrix')
                else:
                    value['rankings'][0]['score'] = None
            client = Client()
            client.mutation = mutation
            with self.subTest(fault=fault), self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'), ENV, client, physical)

    def test_physical_source_size_complete_content_workspace_and_prior_digest_are_strict(self):
        for fault in ('size-bool', 'size-zero', 'source', 'pixels', 'grid', 'workspace', 'outputs', 'oracle'):
            calls = []
            def verify(repository, env):
                native = physical(repository, env)
                calls.append(1)
                source = m.fixture.SOURCES[0]
                if fault == 'size-bool':
                    native['sources'][source]['size_bytes'] = True
                elif fault == 'size-zero':
                    native['sources'][source]['size_bytes'] = 0
                elif fault == 'source' and len(calls) > 1:
                    native['sources'][source]['sha256'] = 'f' * 64
                    native['source_physical'][source]['sha256'] = 'f' * 64
                elif fault in ('pixels', 'grid'):
                    native['source_physical'][source]['full_pixels_and_mask_verified' if fault == 'pixels' else 'grid_crs_scale_verified'] = False
                elif fault == 'workspace':
                    native['runtime_workspaces_removed'] = False
                elif fault == 'outputs':
                    native['no_outputs'] = False
                elif fault == 'oracle':
                    native['expectations']['full']['band_statistics'][0]['mean'] = float('inf')
                return native
            with self.subTest(fault=fault), self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'), ENV, Client(), verify)

    def test_diagnostic_reuses_existing_sanitizer(self):
        self.assertTrue(issubclass(m.DiagnosticClient, m.rasterize.DiagnosticClient))
        self.assertEqual(m.DiagnosticClient.diagnostic_name, 'band-recommendation-submission-error.json')
