import copy
import importlib
from pathlib import Path
import unittest
import tempfile
import urllib.parse

m = importlib.import_module('scripts.test.raster-workflow-online')


class Client:
    def __init__(self):
        self.calls = []
        self.executions = {}
        self.source = {'id': 10, 'item_type': 'object', 'full_name': 'raster-source/source.tif', 'fingerprint': 'source-fp'}
        self.target = {'id': 20, 'item_type': 'object', 'full_name': 'raster-target/result.cog.tif', 'fingerprint': 'target-fp',
                       'attributes': {'item': {'format': 'tiff'}, 'type_info': {'media': {'width': 256, 'height': 256}},
                                      'format_info': {'tiff': {'profile': 'cog', 'is_tiled': True, 'has_overviews': True}},
                                      'capabilities': {'spatial': {'srid': 4326, 'extent': [110, 17.76, 112.56, 20.32]}}}}
        self.spatial_source = {'id': 11, 'item_type': 'object', 'full_name': 'raster-source/spatial.tif', 'fingerprint': 'spatial-fp'}
        self.multiband_source = {'id': 12, 'item_type': 'object', 'full_name': 'raster-source/multiband.tif', 'fingerprint': 'multiband-fp'}
        self.average_source = {'id': 13, 'item_type': 'object', 'full_name': 'raster-source/multiband-average.tif', 'fingerprint': 'average-fp'}
        self.finite_source = {'id': 14, 'item_type': 'object', 'full_name': 'raster-source/multiband-average-finite.tif', 'fingerprint': 'finite-fp'}
        self.non_cog_source = {'id': 15, 'item_type': 'object', 'full_name': 'raster-source/non-cog.tif', 'fingerprint': 'non-cog-fp'}
        self.targets = {20: self.target}
        for item_id, overlap in [(21, 'first'), (22, 'last')]:
            item = copy.deepcopy(self.target)
            item.update(id=item_id, full_name=f'raster-target/mosaic-{overlap}.cog.tif', fingerprint='mosaic-' + overlap)
            item['attributes']['capabilities']['spatial'] = {'srid': 3857, 'extent': [0, 0, 256, 256]}
            self.targets[item_id] = item
        for item_id, name in enumerate(m.fixture.GRID_CASES + m.fixture.MULTIBAND_CASES, 23):
            expectation = m.fixture.computed_expectation(name)
            item = copy.deepcopy(self.target)
            item.update(id=item_id, full_name=f'raster-target/{name}.cog.tif', fingerprint=name)
            item['attributes']['type_info']['media'] = {'width': expectation['width'], 'height': expectation['height']}
            item['attributes']['capabilities']['spatial'] = {'srid': 4326, 'extent': expectation['extent']}
            item['attributes']['format_info']['tiff'] = {'profile': 'cog' if name=='clip-polygon' or 'bilinear' in name or 'fractional' in name or 'finite' in name else 'geotiff',
                'is_tiled': True, 'has_overviews': name=='clip-polygon' or 'bilinear' in name or 'fractional' in name or 'finite' in name}
            self.targets[item_id] = item
        for item_id, name in enumerate(m.fixture.UTILITY_CASES, 36):
            item = copy.deepcopy(self.target)
            item.update(id=item_id, full_name=f'raster-target/{name}.cog.tif', fingerprint=name, size_bytes=1024)
            self.targets[item_id] = item
        for item_id, name in enumerate(m.fixture.FOUNDATION_CASES, 38):
            item = copy.deepcopy(self.targets[21])
            item.update(id=item_id, full_name=f'raster-target/{name}.cog.tif', fingerprint=name, size_bytes=1024)
            self.targets[item_id] = item
        for item_id, name in enumerate(m.fixture.RECLASS_CASES, 40):
            item = copy.deepcopy(self.target)
            item.update(id=item_id, full_name=f'raster-target/{name}.cog.tif', fingerprint=name, size_bytes=1024)
            self.targets[item_id] = item
        for item_id, name in enumerate(m.fixture.AGGREGATE_CASES, 42):
            item = copy.deepcopy(self.target)
            expected = m.fixture.aggregate_expectation(name)
            item.update(id=item_id, full_name=f'raster-target/{name}.cog.tif', fingerprint=name, size_bytes=1024)
            item['attributes']['type_info']['media'] = {'width': expected['width'], 'height': expected['height']}
            item['attributes']['capabilities']['spatial'] = {'srid':4326, 'extent':expected['extent']}
            self.targets[item_id] = item
        self.context = {'principal': {'id': 7, 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                        'token': {'type': 'first_party_access_token'}, 'authorization': {'role_assignments': [
                            {'role_key': 'online.raster_workflow', 'permissions': sorted(m.PERMISSIONS)}]}}
        self.mutate = lambda execution: execution
        self.monitor_mismatch = False
        self.no_graph = False
        self.relation_kind = 'derive'

    @staticmethod
    def project(execution, professional):
        result = copy.deepcopy(execution)
        stored = result.pop('metadata')
        result['metadata'] = {}
        if stored.get('lineage_facts'):
            facts = copy.deepcopy(stored['lineage_facts'])
            facts.pop('operations', None)
            result['metadata']['lineage_facts'] = facts
        if professional:
            result['outputs'] = stored.get('outputs', {})
            if 'result' in stored:
                result['metadata']['result'] = stored['result']
        if not result['metadata']:
            result.pop('metadata')
        return result

    def request(self, method, path, expected, body=None):
        self.calls.append((method, path, body))
        if path == '/api/v1/system/auth/context': result = self.context
        elif path == '/api/v1/develop/workflow-engines':
            result = [{'id': 3, 'engine_type': 'geopython_workflow', 'connection_status': 'online'}]
        elif path == '/api/v1/meta/scan/run/manual':
            assert body['engine_id'] == 1, 'manual target scan must never hide a broken automatic scan'
            result = {'execution_id': 'source-scan'}
        elif path.startswith('/api/v1/meta/executions/'):
            result = {'status': 'success'}
        elif path == '/api/v1/meta/engines/1/items': result = [self.source, self.spatial_source, self.multiband_source, self.average_source, self.finite_source, self.non_cog_source]
        elif path == '/api/v1/meta/engines/2/items': result = list(self.targets.values())
        elif path.startswith('/api/v1/meta/items/'): result = self.targets[int(path.rsplit('/', 1)[1])]
        elif method == 'POST' and path == '/api/v1/develop/executions':
            definition = body['content']['workflow_definition']
            if definition['tasks'][-1]['id'] == 'analysis':
                identifier = f'run-{len(self.executions) + 1}'
                operator = definition['tasks'][-1]['operator']
                if operator == 'raster_info':
                    final_result = m.fixture.utility_info_expectation('physical-wkt')
                elif operator == 'validate_cog':
                    valid = 'to-cog.cog.tif' in definition['tasks'][0]['params']['locator']
                    final_result = {'valid': valid, 'warnings': [], 'errors': [] if valid else ['not tiled']}
                else:
                    case_name = next(name for name in m.fixture.ANALYSIS_CASES
                        if definition == m.analysis_workflow(definition['tasks'][0]['params']['locator'], name))
                    final_result = m.fixture.analysis_expectations()[case_name]
                self.executions[identifier] = self.mutate({'execution_id': identifier, 'module': 'develop', 'status': 'success',
                    'metadata': {'result': {'summary': {'has_result': True},
                        'final_result': final_result}}})
                return m.support.Response(200, {'execution_id': identifier}, {})
            mode = definition['tasks'][-1]['params']['write_mode']
            identifier = f'run-{len(self.executions) + 1}'
            status = 'failed' if identifier == 'run-2' else 'success'
            sources = [task['params']['locator'] for task in definition['tasks'] if task['operator'] in ('raster_load', 'raster_to_cog')]
            target = f'addp://engine/2/path/raster-target/{definition["tasks"][-1]["params"]["target_name"]}?type=object'
            spatial = 'mosaic' in definition['tasks'][-1]['depends_on']
            case_name = definition['tasks'][-1]['params']['target_name'].removesuffix('.cog.tif')
            utility = case_name in m.fixture.UTILITY_CASES
            expectation = m.fixture.utility_expectation() if utility else m.fixture.computed_expectation(case_name) if case_name in m.fixture.GRID_CASES + m.fixture.MULTIBAND_CASES + m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES else m.fixture.artifact_expectation(spatial)
            metadata = {} if status == 'failed' else {
                'outputs': {definition['tasks'][-1]['id']: {'resource': {'locator': target, 'type': 'object', 'write_mode': mode}}},
                'lineage_facts': {'schema_version': 'addp.lineage-facts/v1', 'inputs': [{'locator': source} for source in sources],
                                  'outputs': [{'locator': target, 'write_mode': mode}],
                                  'operations': [{'kind': 'derive', 'operator': 'develop', 'input_ports': ['input'], 'output_ports': ['output']}]},
                'result': {'final_result': {'artifact_type': 'raster', 'format': 'tiff', 'profile': 'cog',
                                          'size_bytes': 1024,
                                          **{key: value for key, value in expectation.items() if key not in ('valid_pixels', 'band_nodata')},
                                          'bands': [{'dtype': 'Float64', 'nodata_is_nan': expectation['band_nodata'] is None, 'nodata': expectation['band_nodata'],
                                                    **({'overviews': [[128, 128], [64, 64]] if case_name == 'build-overviews' else [[128, 128]]} if utility else {}),
                                                    **({'color_interpretation': (['Red', 'Green', 'Blue'] if case_name == 'multiraster-rgb' else ['Gray'])[index], 'overviews': [[expectation['width']//2, expectation['height']//2]]} if case_name in m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES else {})}
                                                    for index in range(expectation['band_count'])]},
                           'meta_scan_runs': [{'status': 'submitted', 'target_locator': target, 'execution_id': 'auto-' + identifier}]},
            }
            self.executions[identifier] = self.mutate({'execution_id': identifier, 'module': 'develop', 'status': status, 'metadata': metadata,
                                                     'error_details': {'category': 'execution_failed'} if status == 'failed' else {}})
            result = {'execution_id': identifier}
        elif path.startswith('/api/v1/develop/executions/'):
            result = self.project(self.executions[path.rsplit('/', 1)[1]], professional=True)
        elif path.startswith('/api/v1/monitor/executions/by-execution-id/'):
            result = self.project(self.executions[path.rsplit('/', 1)[1]], professional=False)
            if self.monitor_mismatch: result['status'] = 'running'
        elif path.startswith('/api/v1/meta/lineage/graph?'):
            target_id = int(urllib.parse.parse_qs(urllib.parse.urlsplit(path).query)['item_id'][0])
            source_id, identifier = (10, 'run-3') if target_id == 20 else (11, 'run-' + str(target_id - (17 if target_id <= 22 else 13)))
            if target_id == 26: source_id = 12
            if target_id == 27: source_id = 26
            if target_id == 28: source_id = 13
            if target_id == 29: source_id = 28
            if target_id == 30: source_id = 13
            if target_id == 31: source_id = 30
            if target_id == 32: source_id = 13
            if target_id == 33: source_id = 32
            if target_id == 34: source_id = 14
            if target_id == 35: source_id = 34
            if target_id in (36, 37, 40, 41, 42, 43): source_id = 10
            if target_id in (40, 41, 42, 43): identifier = 'run-' + str(target_id - 10)
            result = {'truncated': self.no_graph, 'edges': [{'source': {'item_id': source_id}, 'target': {'item_id': target_id},
                      'status': 'active', 'relation_kind': self.relation_kind, 'evidence': {'execution_id': identifier}}]}
            if target_id in (38, 39):
                result['edges'] = [{'source': {'item_id': source_id}, 'target': {'item_id': target_id},
                    'status': 'active', 'relation_kind': self.relation_kind,
                    'evidence': {'execution_id': 'run-' + str(target_id - 10)}} for source_id in (11, 21)]
        else:
            raise AssertionError(f'unexpected API: {method} {path}')
        return m.support.Response(200, result, {})


class RasterWorkflowOnlineTest(unittest.TestCase):
    def setUp(self):
        self.client = Client()
        self.env = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID': '1',
                    'ADDP_ONLINE_RASTER_TARGET_ENGINE_ID': '2', 'ADDP_ONLINE_TEST_RUN_ID': 'raster-test'}
        self.physical_actions = []

    def physical(self, repo, env, action):
        self.physical_actions.append(action)
        if action.startswith('verify-mosaic-'):
            overlap = action.removeprefix('verify-mosaic-')
            return {'sha256': 'spatial-' + overlap, 'cog_valid': True, 'has_overviews': True, 'source_unchanged': True,
                    'valid_pixels': 65534, 'invalid_pixels': 2, 'overlap': overlap,
                    'baseline_sha256': 'new', 'first_sha256': 'spatial-first'}
        case_name = action.removeprefix('verify-')
        if case_name in m.fixture.UTILITY_CASES or action == 'verify-utility-queries':
            preserved = {'result.cog.tif': 'new', 'mosaic-first.cog.tif': 'spatial-first', 'mosaic-last.cog.tif': 'spatial-last'}
            for prior in m.fixture.GRID_CASES + m.fixture.MULTIBAND_CASES:
                preserved[prior + '.cog.tif'] = prior
            accepted = m.fixture.UTILITY_CASES if action == 'verify-utility-queries' else m.fixture.UTILITY_CASES[:m.fixture.UTILITY_CASES.index(case_name)]
            for prior in accepted: preserved[prior + '.cog.tif'] = prior
            return {'sha256': case_name, 'case_name': case_name, 'preserved_sha256': preserved,
                'cog_valid': True, 'has_overviews': True, 'source_unchanged': True, 'valid_pixels': 65535,
                'band_valid_pixels': [65535, 65535], 'size_bytes': 1024, 'crs': 'physical-wkt',
                'overview_sizes': [[128, 128], [64, 64]] if case_name != 'to-cog' else [[128, 128]]}
        if case_name in m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES:
            preserved = {'result.cog.tif': 'new', 'mosaic-first.cog.tif': 'spatial-first', 'mosaic-last.cog.tif': 'spatial-last'}
            for prior in m.fixture.GRID_CASES + m.fixture.MULTIBAND_CASES + m.fixture.UTILITY_CASES + (m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES)[:(m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES).index(case_name)]:
                preserved[prior + '.cog.tif'] = prior
            return {'sha256': case_name, 'case_name': case_name, 'preserved_sha256': preserved,
                'cog_valid': True, 'has_overviews': True, 'source_unchanged': True, 'valid_pixels': m.fixture.computed_expectation(case_name)['valid_pixels'],
                'invalid_pixels': m.fixture.computed_expectation(case_name)['width'] * m.fixture.computed_expectation(case_name)['height'] - m.fixture.computed_expectation(case_name)['valid_pixels'],
                'band_valid_pixels': [m.fixture.computed_expectation(case_name)['valid_pixels']] * m.fixture.computed_expectation(case_name)['band_count'],
                'color_interpretations': ['Red', 'Green', 'Blue'] if case_name == 'multiraster-rgb' else ['Gray'],
                'size_bytes': 1024, 'overview_sizes': [[m.fixture.computed_expectation(case_name)['width']//2, m.fixture.computed_expectation(case_name)['height']//2]]}
        if case_name in m.fixture.GRID_CASES + m.fixture.MULTIBAND_CASES:
            preserved = {'result.cog.tif': 'new', 'mosaic-first.cog.tif': 'spatial-first', 'mosaic-last.cog.tif': 'spatial-last'}
            cases = m.fixture.GRID_CASES + m.fixture.MULTIBAND_CASES
            for prior in cases[:cases.index(case_name)]:
                preserved[prior + '.cog.tif'] = prior
            extra = {}
            if case_name in m.fixture.MULTIBAND_CASES:
                joint = case_name.endswith('-joint')
                expectation = m.fixture.computed_expectation(case_name)
                extra = {'band_valid_pixels': [expectation['valid_pixels']] * (1 if joint else 2),
                    'invalid_pixels': expectation['width'] * expectation['height'] - expectation['valid_pixels'],
                    'partial_alpha_pixels': 64 if case_name == 'multiband-bilinear' else 3 if case_name == 'multiband-alpha' else 0,
                    'band_nodata': expectation['band_nodata'], 'band_nodata_is_nan': expectation['band_nodata'] is None}
            return {**extra, 'sha256': case_name, 'case_name': case_name, 'preserved_sha256': preserved,
                    'cog_valid': True, 'has_overviews': case_name=='clip-polygon' or 'bilinear' in case_name or 'fractional' in case_name or 'finite' in case_name,
                    'source_unchanged': True, 'valid_pixels': m.fixture.computed_expectation(case_name)['valid_pixels']}
        if action == 'verify-analysis':
            return {'sha256': 'new', 'cog_valid': True, 'source_unchanged': True, 'valid_pixels': 65535,
                    'first_sha256': 'spatial-first', 'last_sha256': 'spatial-last'}
        return {'sha256': 'new' if action == 'verify-replace' else 'original', 'cog_valid': True, 'has_overviews': True,
                'source_unchanged': True, 'valid_pixels': 65535}

    def run_scene(self, physical=None):
        return m.run_scenario(Path('.'), self.env, self.client, physical or self.physical,
                              lambda repo, env, evidence: evidence,
                              lambda repo, env, identifier, timeout: {'execution_id': identifier,
                                  'cause': 'target_already_exists', 'source': 'develop_runtime_log'})

    def test_complete_chain_only_uses_source_manual_scan_and_owner_automatic_target_scan(self):
        report = self.run_scene()
        self.assertEqual([item['status'] for item in report['executions']], ['success', 'failed', 'success'])
        self.assertEqual(self.physical_actions, ['verify-create', 'verify-create', 'verify-replace',
            'verify-mosaic-first', 'verify-mosaic-last'] + ['verify-analysis'] * 4 + ['verify-' + name for name in m.fixture.GRID_CASES + m.fixture.MULTIBAND_CASES + m.fixture.UTILITY_CASES] + ['verify-utility-queries'] * 3 + ['verify-' + name for name in m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES])
        self.assertEqual(report['automatic_target_scan_execution_id'], 'auto-run-3')
        self.assertEqual([item['case_name'] for item in report['spatial_cases']], ['mosaic-first', 'mosaic-last'])
        self.assertEqual([item['lineage']['source_item_id'] for item in report['spatial_cases']], [11, 11])
        self.assertEqual([item['lineage']['target_item_id'] for item in report['spatial_cases']], [21, 22])
        self.assertEqual([item['browser']['target_name'] for item in report['spatial_cases']],
                         ['mosaic-first.cog.tif', 'mosaic-last.cog.tif'])
        manual = [body for method, path, body in self.client.calls if method == 'POST' and 'scan/run' in path]
        self.assertEqual(len(manual), 1)
        self.assertEqual(manual[0]['engine_id'], 1)
        self.assertFalse(any('collect' in path for _, path, _ in self.client.calls))
        self.assertEqual([case['case_name'] for case in report['analysis_cases']], list(m.fixture.ANALYSIS_CASES))
        self.assertEqual([case['case_name'] for case in report['grid_cases']], list(m.fixture.GRID_CASES))
        self.assertEqual([case['lineage']['target_item_id'] for case in report['grid_cases']], [23, 24, 25])
        self.assertEqual([case['case_name'] for case in report['multiband_cases']], list(m.fixture.MULTIBAND_CASES))
        self.assertEqual([case['lineage']['source_item_id'] for case in report['multiband_cases']], [12, 26, 13, 28, 13, 30, 13, 32, 14, 34])
        self.assertEqual([case['lineage']['target_item_id'] for case in report['multiband_cases']], [26, 27, 28, 29, 30, 31, 32, 33, 34, 35])
        self.assertEqual(report['multiband_cases'][1]['browser']['source_name'], 'multiband-alpha.cog.tif')

    def test_utilities_execute_all_remaining_operators_and_read_their_persisted_results(self):
        report = self.run_scene()
        cases = report['utility_cases']
        self.assertEqual([case['case_name'] for case in cases], list(m.fixture.UTILITY_CASES + m.fixture.UTILITY_JSON_CASES))
        submitted = [body['content']['workflow_definition'] for method, path, body in self.client.calls
            if method == 'POST' and path == '/api/v1/develop/executions']
        self.assertEqual({task['operator'] for definition in submitted for task in definition['tasks']}, {
            'raster_load', 'raster_save', 'raster_info', 'validate_cog', 'raster_to_cog', 'raster_build_overviews',
            'raster_reproject', 'raster_resample', 'raster_clip', 'raster_mosaic', 'raster_band_math',
            'raster_statistics', 'raster_histogram', 'raster_align', 'raster_stack', 'raster_select_bands', 'raster_reclassify', 'raster_aggregate'})
        tail = len(m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES)
        submitted = submitted[-(len(m.fixture.UTILITY_CASES + m.fixture.UTILITY_JSON_CASES) + tail):-tail]
        self.assertEqual([task['operator'] for task in submitted[0]['tasks']],
            ['raster_load', 'raster_build_overviews', 'raster_save'])
        self.assertEqual(submitted[0]['tasks'][1]['params']['levels'], [2, 4])
        self.assertEqual([task['operator'] for task in submitted[1]['tasks']], ['raster_to_cog'])
        conversion = submitted[1]['tasks'][0]['params']
        self.assertNotIn('options', conversion, 'Runtime-only options must be derived by Develop')
        self.assertEqual(conversion['blocksize'], 128)
        self.assertEqual(conversion['overview_resampling'], 'nearest')
        self.assertEqual(cases[0]['lineage']['source_item_id'], 10)
        self.assertEqual(cases[1]['lineage']['source_item_id'], 10)
        self.assertEqual(cases[2]['browser']['source_item_id'], 36)
        self.assertEqual(cases[3]['browser']['source_item_id'], 15)
        self.assertEqual(cases[4]['browser']['source_item_id'], 37)
        self.assertEqual(cases[2]['json_result']['bands'][1]['overviews'], [[128, 128], [64, 64]])
        self.assertFalse(cases[3]['json_result']['valid'])
        self.assertTrue(cases[4]['json_result']['valid'])
        self.assertTrue(all(case['browser']['result_kind'] == 'json' for case in cases[2:]))

    def test_multiraster_binds_both_engines_and_every_source_in_console(self):
        report = self.run_scene()
        cases = report['foundation_cases']
        self.assertEqual([case['case_name'] for case in cases], list(m.fixture.FOUNDATION_CASES))
        for case in cases:
            self.assertEqual([edge['source_item_id'] for edge in case['lineage']], [11, 21])
            self.assertEqual([browser['source_item_id'] for browser in case['browser']], [11, 21])
            self.assertEqual(len({browser['case_name'] for browser in case['browser']}), 2)
            self.assertTrue(all(edge['execution_id'] == case['execution_id'] for edge in case['lineage']))
            self.assertEqual(case['physical']['size_bytes'], 1024)
        submitted = [body['content']['workflow_definition'] for method, path, body in self.client.calls
            if method == 'POST' and path == '/api/v1/develop/executions'
            and any(task['operator'] == 'raster_align' for task in body['content']['workflow_definition']['tasks'])]
        self.assertEqual([task['operator'] for task in submitted[0]['tasks']],
            ['raster_load', 'raster_load', 'raster_align', 'raster_select_bands', 'raster_stack', 'raster_band_math', 'raster_save'])
        self.assertEqual(submitted[0]['tasks'][5]['params']['expression'], '0.25*b1+0.75*b2')
        self.assertEqual(submitted[1]['tasks'][5]['params']['bands'], [1, 2, 1])
        self.assertEqual(submitted[1]['tasks'][5]['params']['color_model'], 'rgb')
        self.assertEqual(submitted[0]['tasks'][1]['params']['locator'], cases[0]['browser'][1]['source_locator'])

    def test_multiraster_rejects_missing_extra_duplicate_sources_and_wrong_execution_edge(self):
        for fault in ('missing', 'extra', 'duplicate', 'edge-missing', 'edge-execution'):
            with self.subTest(fault=fault):
                self.setUp()
                self.env['ADDP_ONLINE_TEST_TIMEOUT_SECONDS'] = '.01'
                def mutate(execution):
                    if execution['execution_id'] == 'run-28':
                        inputs = execution['metadata']['lineage_facts']['inputs']
                        if fault == 'missing': inputs.pop()
                        if fault == 'extra': inputs.append({'locator': 'extra-locator'})
                        if fault == 'duplicate': inputs[1] = copy.deepcopy(inputs[0])
                    return execution
                self.client.mutate = mutate
                original = self.client.request
                def request(method, path, expected, body=None):
                    response = original(method, path, expected, body)
                    if path.startswith('/api/v1/meta/lineage/graph?') and 'item_id=38&' in path:
                        if fault == 'edge-missing': response.payload['edges'].pop()
                        if fault == 'edge-execution': response.payload['edges'][1]['evidence']['execution_id'] = 'another-run'
                    return response
                self.client.request = request
                with self.assertRaises(m.SuiteError): self.run_scene()

    def test_aggregation_checks_partial_grid_selected_band_and_preservation(self):
        report = self.run_scene()
        cases = report['aggregate_cases']
        self.assertEqual([case['case_name'] for case in cases],list(m.fixture.AGGREGATE_CASES))
        self.assertEqual([case['lineage']['target_item_id'] for case in cases],[42,43])
        self.assertEqual(len(cases[-1]['physical']['preserved_sha256']),23)
        for case in cases:
            self.assertEqual(case['lineage']['source_item_id'],10)
            self.assertEqual(case['physical']['band_valid_pixels'],[4472])
            self.assertEqual(case['physical']['invalid_pixels'],0)
            self.assertEqual(case['physical']['overview_sizes'],[[43,26]])
            self.assertEqual(case['browser']['source_locators'],[case['browser']['source_locator']])
        definitions = [body['content']['workflow_definition'] for method,path,body in self.client.calls
            if method=='POST' and path=='/api/v1/develop/executions'][-2:]
        for definition,method in zip(definitions,['mean','sum']):
            self.assertEqual([task['operator'] for task in definition['tasks']],
                ['raster_load','raster_select_bands','raster_aggregate','raster_build_overviews','raster_save'])
            self.assertEqual(definition['tasks'][1]['params']['bands'],[2])
            self.assertEqual(definition['tasks'][2]['params']['factors'],[3,5])
            self.assertEqual(definition['tasks'][2]['params']['method'],method)

    def test_aggregation_rejects_size_grid_levels_counts_source_and_prior_faults(self):
        for fault in ('declared-size','item-size','grid','levels','counts','source','prior'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    if execution['execution_id']=='run-32':
                        artifact = execution['metadata']['result']['final_result']
                        if fault=='declared-size': artifact['size_bytes']=1
                        if fault=='grid': artifact['width']=85
                    return execution
                self.client.mutate=mutate
                if fault=='item-size': self.client.targets[42]['size_bytes']=1025
                def physical(repo,env,action):
                    payload=self.physical(repo,env,action)
                    if action=='verify-aggregate-mean':
                        if fault=='levels': payload['overview_sizes']=[[128,128]]
                        if fault=='counts': payload['band_valid_pixels']=[4471]
                        if fault=='source': payload['source_unchanged']=False
                        if fault=='prior': payload['preserved_sha256']['reclassify-keep.cog.tif']='changed'
                    return payload
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_reclassification_checks_default_keep_selected_band_and_exact_source(self):
        report = self.run_scene()
        cases = report['reclass_cases']
        self.assertEqual([case['case_name'] for case in cases], list(m.fixture.RECLASS_CASES))
        self.assertEqual([case['lineage']['target_item_id'] for case in cases], [40, 41])
        self.assertEqual([case['physical']['band_valid_pixels'] for case in cases], [[49663], [65535]])
        self.assertEqual(len(cases[-1]['physical']['preserved_sha256']), 21)
        for case in cases:
            self.assertEqual(case['lineage']['source_item_id'], 10)
            self.assertEqual(case['browser']['source_locators'], [case['browser']['source_locator']])
            self.assertEqual(case['browser']['source_name'], 'source.tif')
            self.assertEqual(case['physical']['size_bytes'], 1024)
        definitions = [body['content']['workflow_definition'] for method, path, body in self.client.calls
            if method == 'POST' and path == '/api/v1/develop/executions' and any(task['operator']=='raster_reclassify' for task in body['content']['workflow_definition']['tasks'])]
        self.assertNotIn('unmatched', definitions[0]['tasks'][1]['params'])
        self.assertEqual(definitions[1]['tasks'][1]['params']['unmatched'], 'keep')
        for definition in definitions:
            self.assertEqual([task['operator'] for task in definition['tasks']],
                             ['raster_load', 'raster_reclassify', 'raster_save'])
            self.assertEqual(definition['tasks'][1]['params']['band'], 2)
            self.assertIsInstance(definition['tasks'][1]['params']['rules'], list)

    def test_reclassification_rejects_size_counts_nodata_source_and_prior_faults(self):
        for fault in ('declared-size', 'item-size', 'counts', 'invalid-count', 'nodata', 'source', 'prior'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    if execution['execution_id'] == 'run-30':
                        artifact = execution['metadata']['result']['final_result']
                        if fault == 'declared-size': artifact['size_bytes'] = 1
                        if fault == 'nodata': artifact['bands'][0]['nodata_is_nan'] = False
                    return execution
                self.client.mutate = mutate
                if fault == 'item-size': self.client.targets[40]['size_bytes'] = 1025
                def physical(repo, env, action):
                    payload = self.physical(repo, env, action)
                    if action == 'verify-reclassify-nodata':
                        if fault == 'counts': payload['band_valid_pixels'] = [65535]
                        if fault == 'invalid-count': payload['invalid_pixels'] = 0
                        if fault == 'source': payload['source_unchanged'] = False
                        if fault == 'prior': payload['preserved_sha256']['multiraster-rgb.cog.tif'] = 'changed'
                    return payload
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_multiraster_rejects_size_channel_validity_source_and_preservation_faults(self):
        for fault in ('declared-size', 'fractional-size', 'physical-size', 'item-size', 'counts', 'colour', 'levels', 'summary-colour', 'summary-levels', 'source', 'prior'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    if execution['execution_id'] == 'run-29':
                        artifact = execution['metadata']['result']['final_result']
                        if fault == 'declared-size': artifact['size_bytes'] = 1
                        if fault == 'fractional-size': artifact['size_bytes'] = 1024.0
                        if fault == 'summary-colour': artifact['bands'][1]['color_interpretation'] = 'Undefined'
                        if fault == 'summary-levels': artifact['bands'][1]['overviews'] = []
                    return execution
                self.client.mutate = mutate
                if fault == 'item-size': self.client.targets[39]['size_bytes'] = 1025
                def physical(repo, env, action):
                    payload = self.physical(repo, env, action)
                    if action == 'verify-multiraster-rgb':
                        if fault == 'physical-size': payload['size_bytes'] = 0
                        if fault == 'counts': payload['band_valid_pixels'][1] = 65536
                        if fault == 'colour': payload['color_interpretations'].reverse()
                        if fault == 'levels': payload['overview_sizes'] = []
                        if fault == 'source': payload['source_unchanged'] = False
                        if fault == 'prior': payload['preserved_sha256']['multiraster-weighted.cog.tif'] = 'changed'
                    return payload
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_utility_persistence_rejects_size_levels_second_band_and_prior_artifact_faults(self):
        for fault in ('declared-size', 'fractional-size', 'physical-size', 'levels', 'counts', 'prior', 'scan', 'output-node', 'item-size', 'item-size-mismatch'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    if execution['execution_id'] == 'run-23':
                        metadata = execution['metadata']
                        artifact = metadata['result']['final_result']
                        if fault == 'declared-size': artifact['size_bytes'] = 1
                        if fault == 'fractional-size': artifact['size_bytes'] = 1024.0
                        if fault == 'levels': artifact['bands'][1]['overviews'].pop()
                        if fault == 'scan': metadata['result']['meta_scan_runs'] = []
                    if execution['execution_id'] == 'run-24' and fault == 'output-node':
                        execution['metadata']['outputs']['save'] = execution['metadata']['outputs'].pop('convert')
                    return execution
                self.client.mutate = mutate
                if fault == 'item-size': self.client.targets[36]['size_bytes'] = 0
                if fault == 'item-size-mismatch': self.client.targets[36]['size_bytes'] = 1025
                def physical(repo, env, action):
                    payload = self.physical(repo, env, action)
                    if action == 'verify-build-overviews':
                        if fault == 'physical-size': payload['size_bytes'] = 0
                        if fault == 'counts': payload['band_valid_pixels'][1] = 65536
                        if fault == 'prior': payload['preserved_sha256']['result.cog.tif'] = 'changed'
                    return payload
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_utility_queries_reject_wrong_facts_cog_verdicts_and_persistent_side_effects(self):
        for fault in ('info', 'private', 'invalid-valid', 'valid-errors', 'error-shape', 'output', 'scan', 'lineage', 'prior'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    metadata = execution['metadata']
                    if execution['execution_id'] == 'run-25':
                        actual = metadata['result']['final_result']
                        if fault == 'info': actual['bands'][1]['nodata'] = 0
                        if fault == 'private': actual['workspace'] = '/tmp/private'
                        if fault == 'output': metadata['outputs'] = {'analysis': {'resource': {}}}
                        if fault == 'scan': metadata['result']['meta_scan_runs'] = [{'status': 'submitted'}]
                        if fault == 'lineage': metadata['lineage_facts'] = {'inputs': [{}]}
                    if execution['execution_id'] == 'run-26':
                        actual = metadata['result']['final_result']
                        if fault == 'invalid-valid': actual.update(valid=True, errors=[])
                        if fault == 'error-shape': actual['errors'] = 'not tiled'
                    if execution['execution_id'] == 'run-27' and fault == 'valid-errors':
                        metadata['result']['final_result']['errors'] = ['layout corrupted']
                    return execution
                self.client.mutate = mutate
                def physical(repo, env, action):
                    payload = self.physical(repo, env, action)
                    if action == 'verify-utility-queries' and fault == 'prior':
                        payload['preserved_sha256']['to-cog.cog.tif'] = 'changed'
                    return payload
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_multiband_cases_reject_missing_per_band_counts_partial_alpha_and_reloaded_source(self):
        for fault in ('counts', 'alpha', 'invalid', 'prior', 'source'):
            with self.subTest(fault=fault):
                self.setUp()
                def physical(repo, env, action):
                    result = self.physical(repo, env, action)
                    if action == 'verify-multiband-alpha':
                        if fault == 'counts': result['band_valid_pixels'] = [16384, 16384]
                        if fault == 'alpha': result['partial_alpha_pixels'] = 0
                        if fault == 'invalid': result['invalid_pixels'] = 0
                        if fault == 'prior': result['preserved_sha256']['clip-polygon.cog.tif'] = 'changed'
                    return result
                def mutate(execution):
                    if execution['execution_id'] == 'run-14' and fault == 'source':
                        execution['metadata']['lineage_facts']['inputs'][0]['locator'] = 'wrong-source'
                    return execution
                self.client.mutate = mutate
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_multiband_targets_keep_the_original_strict_size_guard(self):
        for size in (0, -1, True, '001', '1.0'):
            with self.subTest(size=size):
                self.setUp()
                self.client.targets[26]['size_bytes'] = size
                with self.assertRaises(m.SuiteError): self.run_scene()
                self.assertNotIn('run-14', self.client.executions)

    def test_average_workflows_keep_the_kernel_and_reload_the_exact_persisted_source(self):
        report = self.run_scene()
        submitted = {body['content']['workflow_definition']['tasks'][-1]['params']['target_name']:
                     body['content']['workflow_definition']['tasks']
                     for method, path, body in self.client.calls
                     if method == 'POST' and path == '/api/v1/develop/executions'
                     and body['content']['workflow_definition']['tasks'][-1]['id'] == 'save'}
        average = submitted['multiband-average.cog.tif']
        joint = submitted['multiband-average-joint.cog.tif']
        self.assertEqual(average[1]['params']['resampling'], 'average')
        self.assertEqual(average[1]['params']['size'], [128, 128])
        self.assertEqual(joint[1]['params']['expression'], 'b1+b2')
        self.assertEqual(joint[0]['params']['locator'], report['multiband_cases'][3]['browser']['source_locator'])
        self.assertEqual(report['multiband_cases'][3]['browser']['source_name'], 'multiband-average.cog.tif')
        self.assertEqual(report['multiband_cases'][2]['physical']['partial_alpha_pixels'], 0)

    def test_bilinear_workflow_keeps_the_kernel_overviews_and_persisted_source(self):
        report = self.run_scene()
        submitted = {body['content']['workflow_definition']['tasks'][-1]['params']['target_name']:
                     body['content']['workflow_definition']['tasks']
                     for method, path, body in self.client.calls
                     if method == 'POST' and path == '/api/v1/develop/executions'
                     and body['content']['workflow_definition']['tasks'][-1]['id'] == 'save'}
        tasks = submitted['multiband-bilinear.cog.tif']
        joint = submitted['multiband-bilinear-joint.cog.tif']
        self.assertEqual(tasks[1]['params']['size'], [512, 512])
        self.assertEqual(tasks[1]['params']['resampling'], 'bilinear')
        self.assertEqual(joint[1]['params']['expression'], 'b1+b2')
        self.assertEqual(joint[0]['params']['locator'], report['multiband_cases'][5]['browser']['source_locator'])
        self.assertEqual(report['multiband_cases'][5]['browser']['source_name'], 'multiband-bilinear.cog.tif')
        self.assertEqual(report['multiband_cases'][4]['physical']['partial_alpha_pixels'], 64)
        self.assertTrue(report['multiband_cases'][4]['physical']['has_overviews'])

    def test_bilinear_cases_reject_wrong_validity_alpha_overviews_reloaded_source_and_size(self):
        for fault in ('counts', 'alpha', 'overview', 'source', 'size'):
            with self.subTest(fault=fault):
                self.setUp()
                def physical(repo, env, action):
                    result = self.physical(repo, env, action)
                    if action == 'verify-multiband-bilinear':
                        if fault == 'counts': result['band_valid_pixels'] = [262144, 262144]
                        if fault == 'alpha': result['partial_alpha_pixels'] = 0
                    return result
                def mutate(execution):
                    if execution['execution_id'] == 'run-18' and fault == 'source':
                        execution['metadata']['lineage_facts']['inputs'][0]['locator'] = 'wrong-source'
                    return execution
                self.client.mutate = mutate
                if fault == 'overview': self.client.targets[30]['attributes']['format_info']['tiff']['has_overviews'] = False
                if fault == 'size': self.client.targets[30]['size_bytes'] = 0
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_fractional_average_keeps_area_kernel_grid_and_exact_reloaded_source(self):
        report = self.run_scene()
        submitted = {body['content']['workflow_definition']['tasks'][-1]['params']['target_name']:
                     body['content']['workflow_definition']['tasks']
                     for method, path, body in self.client.calls
                     if method == 'POST' and path == '/api/v1/develop/executions'
                     and body['content']['workflow_definition']['tasks'][-1]['id'] == 'save'}
        average = submitted['multiband-average-fractional.cog.tif']
        joint = submitted['multiband-average-fractional-joint.cog.tif']
        self.assertEqual(average[1]['params']['resampling'], 'average')
        self.assertEqual(average[1]['params']['size'], [171, 171])
        self.assertEqual(joint[1]['params']['expression'], 'b1+b2')
        self.assertEqual(joint[0]['params']['locator'], report['multiband_cases'][7]['browser']['source_locator'])
        self.assertEqual(report['multiband_cases'][7]['browser']['source_name'], 'multiband-average-fractional.cog.tif')
        self.assertEqual(report['multiband_cases'][6]['physical']['band_valid_pixels'], [29239, 29239])
        self.assertEqual(report['multiband_cases'][7]['physical']['band_valid_pixels'], [29238])
        self.assertTrue(report['multiband_cases'][6]['physical']['has_overviews'])

    def test_fractional_cases_reject_wrong_counts_alpha_grid_overviews_reload_and_size(self):
        for fault in ('counts', 'alpha', 'grid', 'overview', 'source', 'size'):
            with self.subTest(fault=fault):
                self.setUp()
                def physical(repo, env, action):
                    result = self.physical(repo, env, action)
                    if action == 'verify-multiband-average-fractional':
                        if fault == 'counts': result['band_valid_pixels'] = [29241, 29241]
                        if fault == 'alpha': result['partial_alpha_pixels'] = 1
                    return result
                def mutate(execution):
                    if execution['execution_id'] == 'run-19' and fault == 'grid':
                        execution['metadata']['result']['final_result']['transform'][1] = .02
                    if execution['execution_id'] == 'run-20' and fault == 'source':
                        execution['metadata']['lineage_facts']['inputs'][0]['locator'] = 'wrong-source'
                    return execution
                self.client.mutate = mutate
                if fault == 'overview': self.client.targets[32]['attributes']['format_info']['tiff']['has_overviews'] = False
                if fault == 'size': self.client.targets[32]['size_bytes'] = 0
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_finite_average_reads_its_own_source_and_reloads_the_persisted_cog(self):
        report = self.run_scene()
        submitted = {body['content']['workflow_definition']['tasks'][-1]['params']['target_name']:
                     body['content']['workflow_definition']['tasks']
                     for method, path, body in self.client.calls
                     if method == 'POST' and path == '/api/v1/develop/executions'
                     and body['content']['workflow_definition']['tasks'][-1]['id'] == 'save'}
        average = submitted['multiband-average-finite.cog.tif']
        joint = submitted['multiband-average-finite-joint.cog.tif']
        self.assertEqual(average[1]['params']['resampling'], 'average')
        self.assertEqual(average[1]['params']['size'], [171, 171])
        self.assertEqual(report['multiband_cases'][8]['browser']['source_name'], 'multiband-average-finite.tif')
        self.assertEqual(joint[0]['params']['locator'], report['multiband_cases'][9]['browser']['source_locator'])
        self.assertEqual(report['multiband_cases'][9]['browser']['source_name'], 'multiband-average-finite.cog.tif')
        self.assertEqual(report['multiband_cases'][8]['physical']['band_valid_pixels'], [29239, 29239])
        self.assertEqual(report['multiband_cases'][9]['physical']['band_valid_pixels'], [29238])

    def test_finite_cases_reject_wrong_counts_nodata_sources_and_strict_size(self):
        for fault in ('counts', 'nodata', 'nan', 'missing-nodata', 'physical-nodata', 'physical-flag', 'source', 'reload', 'size'):
            with self.subTest(fault=fault):
                self.setUp()
                def physical(repo, env, action):
                    result = self.physical(repo, env, action)
                    if action == 'verify-multiband-average-finite':
                        if fault == 'counts': result['band_valid_pixels'] = [29241, 29241]
                        if fault == 'physical-nodata': result['band_nodata'] = None
                        if fault == 'physical-flag': result['band_nodata_is_nan'] = True
                    return result
                def mutate(execution):
                    if execution['execution_id'] == 'run-21':
                        if fault == 'nodata': execution['metadata']['result']['final_result']['bands'][0]['nodata'] = -8888.
                        if fault == 'nan': execution['metadata']['result']['final_result']['bands'][0].update(nodata=None, nodata_is_nan=True)
                        if fault == 'missing-nodata': execution['metadata']['result']['final_result']['bands'][0].pop('nodata')
                        if fault == 'source': execution['metadata']['lineage_facts']['inputs'][0]['locator'] = 'nan-source'
                    if execution['execution_id'] == 'run-22' and fault == 'reload':
                        execution['metadata']['lineage_facts']['inputs'][0]['locator'] = 'original-source'
                    return execution
                self.client.mutate = mutate
                if fault == 'size': self.client.targets[34]['size_bytes'] = 0
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_rejects_admin_extra_permissions_default_tenant_and_shared_engine_before_writes(self):
        for fault in ('admin', 'permission', 'tenant', 'engine'):
            with self.subTest(fault=fault):
                self.setUp()
                if fault == 'admin': self.client.context['authorization']['role_assignments'][0]['role_key'] = 'tenant.administrator'
                if fault == 'permission': self.client.context['authorization']['role_assignments'][0]['permissions'].append('system.engine.create')
                if fault == 'tenant': self.env['ADDP_ONLINE_TEST_TENANT_ID'] = '1'
                if fault == 'engine': self.env['ADDP_ONLINE_RASTER_TARGET_ENGINE_ID'] = '1'
                with self.assertRaises(m.SuiteError): self.run_scene()
                self.assertFalse(any(method == 'POST' for method, _, _ in self.client.calls))

    def test_rejects_wrong_output_missing_auto_scan_and_private_artifact(self):
        for fault in ('locator', 'scan', 'private', 'lineage', 'failed_output', 'crs', 'nodata', 'wrong_failure'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    metadata = execution['metadata']
                    if execution['status'] == 'success':
                        if fault == 'locator': metadata['outputs']['save']['resource']['locator'] = 'other'
                        if fault == 'scan': metadata['result']['meta_scan_runs'][0]['status'] = 'failed'
                        if fault == 'private': metadata['result']['final_result']['access_plan'] = {'secret_key': 'private'}
                        if fault == 'lineage': metadata['lineage_facts']['inputs'][0]['locator'] = 'wrong-source'
                        if fault == 'crs': metadata['result']['final_result']['source_crs'] = 'EPSG:3857'
                        if fault == 'nodata': metadata['result']['final_result']['bands'][0]['nodata_is_nan'] = False
                    elif fault == 'failed_output': metadata['outputs'] = {'save': {'resource': {}}}
                    elif fault == 'wrong_failure': execution['error_details']['category'] = 'timeout'
                    return execution
                self.client.mutate = mutate
                with self.assertRaises(m.SuiteError): self.run_scene()

    def test_rejects_monitor_mismatch_truncated_graph_and_failed_create_overwrite(self):
        for fault in ('monitor', 'graph', 'overwrite'):
            with self.subTest(fault=fault):
                self.setUp()
                self.client.monitor_mismatch = fault == 'monitor'
                self.client.no_graph = fault == 'graph'
                def physical(repo, env, action):
                    payload = self.physical(repo, env, action)
                    if len(self.physical_actions) == 2: payload['sha256'] = 'overwritten'
                    return payload
                with self.assertRaises(m.SuiteError): self.run_scene(physical if fault == 'overwrite' else None)

    def test_public_projection_has_top_level_outputs_and_only_safe_monitor_metadata(self):
        self.run_scene()
        execution = self.client.executions['run-1']
        professional = self.client.project(execution, True)
        monitor = self.client.project(execution, False)
        self.assertIn('outputs', professional)
        self.assertNotIn('outputs', professional['metadata'])
        self.assertNotIn('operations', professional['metadata']['lineage_facts'])
        self.assertEqual(set(monitor['metadata']), {'lineage_facts'})
        self.assertNotIn('outputs', monitor)
        professional['metadata']['outputs'] = professional.pop('outputs')
        with self.assertRaises(m.SuiteError):
            m.validate_success(professional, [], '', 'create')

    def test_duplicate_create_requires_exact_owner_execution_target_and_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / 'logs/runtime/develop/instance/events.jsonl'
            path.parent.mkdir(parents=True)
            import json
            for fault in ('execution', 'target', 'cause', 'owner', 'role', None):
                with self.subTest(fault=fault):
                    event = {'module_name': 'develop', 'role': 'backend', 'message':
                        '[DevExecutor] 引擎执行完成: execution_id=run-2 errorMessage=工作流执行失败: '
                        '工作流运行时执行失败: 任务 save 执行失败: target object already exists: raster-target/result.cog.tif'}
                    if fault == 'execution': event['message'] = event['message'].replace('run-2', 'other')
                    if fault == 'target': event['message'] = event['message'].replace('result.cog.tif', 'other.tif')
                    if fault == 'cause': event['message'] = event['message'].replace('target object already exists', 'permission denied')
                    if fault == 'owner': event['module_name'] = 'manager'
                    if fault == 'role': event['role'] = 'frontend'
                    path.write_text(json.dumps(event) + '\n{')
                    if fault:
                        with self.assertRaises(m.SuiteError):
                            m.duplicate_create_diagnostic(root, {}, 'run-2', .001)
                    else:
                        self.assertEqual(m.duplicate_create_diagnostic(root, {}, 'run-2', 1)['cause'], 'target_already_exists')

    def test_spatial_cases_preserve_operator_dependencies_and_exact_grid(self):
        self.run_scene()
        submitted = [body['content']['workflow_definition']['tasks'] for method, path, body in self.client.calls
                     if method == 'POST' and path == '/api/v1/develop/executions']
        for tasks, overlap in zip(submitted[3:5], ('first', 'last')):
            self.assertEqual([task['operator'] for task in tasks], ['raster_load', 'raster_reproject',
                'raster_band_math', 'raster_band_math', 'raster_clip', 'raster_clip', 'raster_mosaic', 'raster_save'])
            self.assertEqual(tasks[1]['params']['target_crs'], 'EPSG:3857')
            self.assertEqual(tasks[1]['params']['resolution'], [1, 1])
            self.assertEqual(tasks[6]['depends_on'], ['left_clip', 'right_clip'])
            self.assertEqual(tasks[6]['params']['other_raster'], {'$ref': 'right_clip', 'port': 'default'})
            self.assertEqual(tasks[6]['params']['overlap'], overlap)

    def test_spatial_cases_reject_wrong_artifact_scan_lineage_and_prior_hashes(self):
        for fault in ('crs', 'grid', 'scan', 'locator', 'lineage', 'baseline', 'first', 'unchanged_last'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    if execution['execution_id'] == 'run-4':
                        metadata = execution['metadata']
                        if fault == 'crs': metadata['result']['final_result']['source_crs'] = 'EPSG:4326'
                        if fault == 'grid': metadata['result']['final_result']['transform'][1] = 2
                        if fault == 'scan': metadata['result']['meta_scan_runs'][0]['target_locator'] = 'wrong-target'
                        if fault == 'locator': metadata['outputs']['save']['resource']['locator'] = 'wrong-target'
                        if fault == 'lineage': metadata['lineage_facts']['inputs'][0]['locator'] = 'wrong-source'
                    return execution
                self.client.mutate = mutate
                def physical(repo, env, action):
                    payload = self.physical(repo, env, action)
                    if action == 'verify-mosaic-first' and fault == 'baseline': payload['baseline_sha256'] = 'modified'
                    if action == 'verify-mosaic-last':
                        if fault == 'first': payload['first_sha256'] = 'modified'
                        if fault == 'unchanged_last': payload['sha256'] = 'spatial-first'
                    return payload
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_graph_rejects_reference_relationship_for_spatial_derivation(self):
        from unittest.mock import patch
        self.client.relation_kind = 'reference'
        with patch.object(m.time, 'monotonic', side_effect=[0, 0, 2]), patch.object(m.time, 'sleep'):
            with self.assertRaises(m.SuiteError):
                m.wait_lineage(self.client, 11, 21, 'run-4', .1)

    def test_rejects_incorrect_target_metadata(self):
        self.client.target['attributes']['type_info']['media']['width'] = 1
        with self.assertRaises(m.SuiteError): self.run_scene()

    def test_small_cog_keeps_physical_validity_separate_from_metadata_hint(self):
        report = self.run_scene()
        self.assertEqual(len(report['grid_cases']),3)
        self.assertEqual(self.client.targets[23]['attributes']['format_info']['tiff']['profile'],'geotiff')

    def test_rejects_invalid_physical_cog_missing_or_inconsistent_overview_and_profile_facts(self):
        for fault in ('invalid-cog','missing-overviews','overviews','profile','tiled'):
            with self.subTest(fault=fault):
                self.setUp()
                tiff = self.client.targets[23]['attributes']['format_info']['tiff']
                if fault=='overviews': tiff['has_overviews']=True
                if fault=='profile': tiff['profile']='cog'
                if fault=='tiled': tiff['is_tiled']=False
                def physical(repo,env,action):
                    result = self.physical(repo,env,action)
                    if action=='verify-resample-size':
                        if fault=='invalid-cog': result['cog_valid']=False
                        if fault=='missing-overviews': result.pop('has_overviews',None)
                    return result
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_analysis_rejects_incorrect_numeric_values_nulls_and_private_fields(self):
        faults = {
            'statistics-band-2': {'band': 1, 'mean': 65537, 'stddev': 0, 'invalid_count': 0, 'valid_count': True,
                                  'nodata_ratio': float('nan'), 'secret_key': 'private'},
            'statistics-all-invalid': {'min': 0, 'mean': float('nan'), 'invalid_count': 0},
            'histogram-auto': {'edges': [2, 16385, 32769, 49152.5, 65536], 'counts': [16383] * 4},
            'histogram-range': {'outside_count': 0, 'counts': [8192] * 4},
        }
        for name, changes in faults.items():
            for key, value in changes.items():
                with self.subTest(case=name, field=key):
                    expected = m.fixture.analysis_expectations()[name]
                    execution = {'metadata': {'result': {'summary': {'has_result': True},
                        'final_result': {**expected, key: value}}}}
                    with self.assertRaises(m.SuiteError): m.validate_analysis(execution, expected)

    def test_analysis_rejects_persistent_outputs_scans_lineage_and_missing_preview(self):
        for fault in ('outputs', 'lineage', 'scan', 'target', 'preview', 'summary'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    if execution['execution_id'] == 'run-6':
                        metadata = execution['metadata']
                        if fault == 'outputs': metadata['outputs'] = {'analysis': {'resource': {'locator': 'fake'}}}
                        if fault == 'lineage': metadata['lineage_facts'] = {'outputs': [{'locator': 'fake'}]}
                        if fault == 'scan': metadata['result']['meta_scan_runs'] = [{'execution_id': 'fake'}]
                        if fault == 'target': metadata['result']['produced_targets'] = [{'locator': 'fake'}]
                        if fault == 'preview': metadata['result'].pop('final_result')
                        if fault == 'summary': metadata['result']['summary']['has_result'] = False
                    return execution
                self.client.mutate = mutate
                with self.assertRaises(m.SuiteError): self.run_scene()

    def test_analysis_preserves_all_prior_artifact_hashes(self):
        for field in ('sha256', 'first_sha256', 'last_sha256'):
            with self.subTest(field=field):
                self.setUp()
                def physical(repo, env, action):
                    payload = self.physical(repo, env, action)
                    if action == 'verify-analysis': payload[field] = 'modified'
                    return payload
                with self.assertRaises(m.SuiteError): self.run_scene(physical)


    def test_grid_cases_reject_dimensions_missing_alpha_and_prior_artifact_changes(self):
        for fault in ('dimensions', 'alpha', 'nodata', 'preservation'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    if execution['execution_id'] == 'run-12':
                        artifact = execution['metadata']['result']['final_result']
                        if fault == 'dimensions': artifact['height'] = 191
                        if fault == 'alpha': artifact['bands'].pop()
                        if fault == 'nodata': artifact['bands'][0]['nodata_is_nan'] = False
                    return execution
                self.client.mutate = mutate
                def physical(repo, env, action):
                    payload = self.physical(repo, env, action)
                    if action == 'verify-clip-polygon' and fault == 'preservation':
                        payload['preserved_sha256']['resample-size.cog.tif'] = 'modified'
                    return payload
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_grid_workflows_keep_size_resolution_exclusive_and_explicit_boundary_crs(self):
        self.run_scene()
        submitted = [body['content']['workflow_definition']['tasks'] for method, path, body in self.client.calls
                     if method == 'POST' and path == '/api/v1/develop/executions']
        size, resolution, polygon = [tasks[2] for tasks in submitted
            if tasks[-1]['params'].get('target_name', '').removesuffix('.cog.tif') in m.fixture.GRID_CASES]
        self.assertEqual(size['params']['size'], [128, 128])
        self.assertNotIn('resolution', size['params'])
        self.assertEqual(resolution['params']['resolution'], [2*m.fixture.ANGULAR_METRE, 4*m.fixture.ANGULAR_METRE])
        self.assertNotIn('size', resolution['params'])
        self.assertEqual(polygon['params']['boundary_crs'], 'EPSG:3857')
        self.assertEqual(len(polygon['params']['geometry']['coordinates']), 2)


if __name__ == '__main__': unittest.main()


class ResourcePolicyClient:
    def __init__(self, scope, state):
        self.scope, self.state = scope, state

    def request(self, method, path, expected, body=None):
        if path.endswith('/engines'):
            return m.support.Response(200, [{'id': 3}], {})
        platform = '/platform/' in path
        status = 200
        if self.scope == 'consumer' or (self.scope == 'tenant' and platform):
            status = 403
        elif method == 'PUT':
            record = self.state['policy' if platform else 'quota']
            allowed = set(record) - {'engine_id', 'tenant_id'}
            if set(body) != allowed:
                status = 400
            elif body['version'] != record['version']:
                status = 409
            else:
                record.update(body)
                record['version'] += 1
        assert status in expected, (method, path, status, expected)
        p, q = self.state['policy'], self.state['quota']
        runtime = {'enabled': True, 'applied_version': p['version'], 'cache_mib': 256, 'pending_restart': p['cache_mib'] != 256,
                   'running': 0, 'waiting': 0, 'running_limit': p['running'], 'waiting_limit': p['waiting']}
        runtime.update(self.state.get('runtime', {}))
        return m.support.Response(status, copy.deepcopy({'policy': p, 'quota': q, 'runtime': runtime,
            'effective_running': q['running'] if q['running'] is not None else p['default_tenant_running'],
            'effective_waiting': q['waiting'] if q['waiting'] is not None else p['default_tenant_waiting']}), {})


class ResourcePolicyAcceptanceTest(unittest.TestCase):
    def setUp(self):
        self.state = {'policy': {'engine_id': 3, 'version': 1, 'running': 2, 'waiting': 2, 'cache_mib': 256,
                                'default_tenant_running': 2, 'default_tenant_waiting': 2},
                      'quota': {'engine_id': 3, 'tenant_id': 2, 'version': 1, 'running': None, 'waiting': None}}
        self.platform = ResourcePolicyClient('platform', self.state)
        self.tenant = ResourcePolicyClient('tenant', self.state)
        self.consumer = ResourcePolicyClient('consumer', self.state)

    def test_management_versions_scope_cache_restart_and_inheritance(self):
        setup = m.prepare_resource_policy(self.platform, self.tenant, self.consumer)
        self.assertEqual((self.state['policy']['running'], self.state['quota']['waiting']), (1, 0))
        evidence = m.finish_resource_policy(self.platform, self.tenant, setup, 1)
        self.assertEqual(evidence['restored_applied_version'], 4)
        self.assertFalse(evidence['consumer_admin_permission'])
        self.assertNotIn('token', str(evidence).lower())
        self.assertIsNone(self.state['quota']['running'])

    def test_declared_default_and_actual_runtime_counts_are_required(self):
        self.state['policy']['cache_mib'] = 512
        with self.assertRaises(m.SuiteError):
            m.prepare_resource_policy(self.platform, self.tenant, self.consumer)
        self.state['policy']['cache_mib'] = 256
        setup = m.prepare_resource_policy(self.platform, self.tenant, self.consumer)
        self.state['runtime'] = {'running': 1}
        with self.assertRaises(m.SuiteError):
            m.finish_resource_policy(self.platform, self.tenant, setup, 1)
        self.state['runtime'] = {'enabled': False}
        with self.assertRaises(m.SuiteError):
            m.finish_resource_policy(self.platform, self.tenant, setup, 0)
