import copy
import importlib
from pathlib import Path
import unittest
import tempfile
import urllib.parse

m = importlib.import_module('scripts.test.raster-workflow-online')


def footprint_result(case_name):
    # Hand-written expected boundary for independent missing/transparent source cells.
    if case_name == 'footprint-diagonal':
        polygons = [[[(i, i), (i+1, i), (i+1, i+1), (i, i+1), (i, i)]] for i in range(64, 68)]
    elif case_name == 'footprint-pinch':
        polygons = [[[(64, 64), (68, 64), (68, 68), (64, 68), (64, 64)],
                     [(65, 65), (66, 65), (66, 66), (65, 66), (65, 65)],
                     [(66, 66), (67, 66), (67, 67), (66, 67), (66, 66)]]]
    elif case_name == 'footprint-holes':
        polygons = [[[(1, 0), (256, 0), (256, 256), (0, 256), (0, 1), (1, 1), (1, 0)],
                     [(64, 64), (192, 64), (192, 192), (64, 192), (64, 64)]],
                    [[(96, 96), (160, 96), (160, 160), (96, 160), (96, 96)]]]
    else:
        ring = {
            'footprint-rotated': [(0, 0), (4, 0), (4, 2), (6, 2), (6, 0), (256, 0), (256, 256), (0, 256), (0, 0)],
            'footprint-any': [(0, 0), (4, 0), (4, 2), (6, 2), (6, 0), (256, 0), (256, 256), (0, 256), (0, 0)],
            'footprint-all': [(6, 0), (256, 0), (256, 256), (0, 256), (0, 2), (6, 2), (6, 0)],
            'footprint-band-2': [(0, 0), (2, 0), (2, 2), (6, 2), (6, 0), (256, 0), (256, 256), (0, 256), (0, 0)],
        }[case_name]
        polygons = [[ring]]
    transform = m.fixture.footprint_transform(case_name)
    coordinates = [[[[transform[0]+x*transform[1]+y*transform[2], transform[3]+x*transform[4]+y*transform[5]]
                     for x, y in ring] for ring in polygon] for polygon in polygons]
    return {'type': 'FeatureCollection', 'features': [{'id': '0', 'type': 'Feature', 'properties': {},
        'geometry': {'type': 'MultiPolygon', 'coordinates': coordinates}}]}



class Client:
    def __init__(self):
        self.calls = []
        self.executions = {}
        self.source = {'id': 10, 'item_type': 'object', 'full_name': 'raster-source/source.tif', 'fingerprint': 'source-fp'}
        self.target = {'id': 20, 'item_type': 'object', 'full_name': 'raster-target/result.cog.tif', 'fingerprint': 'target-fp', 'size_bytes': 1024,
                       'attributes': {'item': {'format': 'tiff'}, 'type_info': {'media': {'width': 256, 'height': 256}},
                                      'format_info': {'tiff': {'profile': 'cog', 'is_tiled': True, 'has_overviews': True}},
                                      'capabilities': {'spatial': {'srid': 4326, 'extent': [110, 17.76, 112.56, 20.32]}}}}
        self.spatial_source = {'id': 11, 'item_type': 'object', 'full_name': 'raster-source/spatial.tif', 'fingerprint': 'spatial-fp'}
        self.multiband_source = {'id': 12, 'item_type': 'object', 'full_name': 'raster-source/multiband.tif', 'fingerprint': 'multiband-fp'}
        self.average_source = {'id': 13, 'item_type': 'object', 'full_name': 'raster-source/multiband-average.tif', 'fingerprint': 'average-fp'}
        self.finite_source = {'id': 14, 'item_type': 'object', 'full_name': 'raster-source/multiband-average-finite.tif', 'fingerprint': 'finite-fp'}
        self.topology_source = {'id':17, 'item_type':'object', 'full_name':'raster-source/' + m.fixture.TOPOLOGY_SOURCE, 'fingerprint':'topology-fp'}
        self.rotated_source = {'id':16, 'item_type':'object', 'full_name':'raster-source/' + m.fixture.ROTATED_SOURCE, 'fingerprint':'rotated-fp'}
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
        for item_id, name in list(enumerate(m.fixture.AGGREGATE_SINGLE_BAND_CASES + m.fixture.OUTSIDE_CASES + m.fixture.UPDATE_CASES, 42)) + list(enumerate(m.fixture.AGGREGATE_MULTIBAND_CASES, 60)):
            item = copy.deepcopy(self.target)
            expected = m.fixture.computed_expectation(name)
            item.update(id=item_id, full_name=f'raster-target/{name}.cog.tif', fingerprint=name, size_bytes=1024)
            item['attributes']['type_info']['media'] = {'width': expected['width'], 'height': expected['height']}
            item['attributes']['capabilities']['spatial'] = {'srid':4326, 'extent':expected['extent']}
            self.targets[item_id] = item
        for item_id, key in enumerate(m.fixture.TILE_WINDOWS, 48):
            item = copy.deepcopy(self.target); expectation = m.fixture.tile_expectation(key)
            item.update(id=item_id, full_name=f'raster-target/tiles-basic/{key}.tif', fingerprint=key, size_bytes=1024)
            item['attributes']['type_info']['media'] = {'width': expectation['width'], 'height': expectation['height']}
            item['attributes']['capabilities']['spatial'] = {'srid':4326, 'extent':expectation['extent']}
            overviews = key != 'r160_c160'
            item['attributes']['format_info']['tiff'] = {'profile': 'cog' if overviews else 'geotiff', 'is_tiled': True, 'has_overviews':overviews}
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
        elif path == '/api/v1/meta/engines/1/items': result = [self.source, self.spatial_source, self.multiband_source, self.average_source, self.finite_source, self.non_cog_source, self.rotated_source, self.topology_source]
        elif path == '/api/v1/meta/engines/2/items': result = list(self.targets.values())
        elif path.startswith('/api/v1/meta/items/'): result = self.targets[int(path.rsplit('/', 1)[1])]
        elif method == 'POST' and path == '/api/v1/develop/executions':
            definition = body['content']['workflow_definition']
            if definition['tasks'][-1]['id'] == 'analysis':
                identifier = f'run-{len(self.executions) + 1}'
                operator = definition['tasks'][-1]['operator']
                if operator == 'raster_info':
                    final_result = m.fixture.utility_info_expectation('physical-wkt')
                elif operator == 'raster_footprint':
                    params = definition['tasks'][-1]['params']
                    case_name = (next(case for case, band in m.fixture.TOPOLOGY_BANDS.items() if params.get('bands') == [band]) if m.fixture.TOPOLOGY_SOURCE in definition['tasks'][0]['params']['locator'] else
                        'footprint-rotated' if m.fixture.ROTATED_SOURCE in definition['tasks'][0]['params']['locator'] else
                        'footprint-holes' if 'clip-outside.cog.tif' in definition['tasks'][0]['params']['locator'] else
                        'footprint-band-2' if params.get('bands') == [2] else
                        'footprint-all' if params.get('validity') == 'all' else 'footprint-any')
                    final_result = footprint_result(case_name)
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
            if definition['tasks'][-1]['operator'] == 'raster_save_tiles':
                identifier = f'run-{len(self.executions)+1}'
                source = definition['tasks'][0]['params']['locator']
                mode = definition['tasks'][-1]['params']['write_mode']
                prefix = definition['tasks'][-1]['params']['target_name']
                failed = prefix == m.fixture.TILE_FAILURE_PREFIX or prefix in m.fixture.TILE_CLEANUP_CASES
                count = m.fixture.TILE_CLEANUP_CASES[prefix][1] if prefix in m.fixture.TILE_CLEANUP_CASES else 1 if failed else 4
                size = 2048 if mode == 'replace' else 1024
                tiles, resources, targets, scans = {}, {}, [], []
                for key, window in m.fixture.TILE_WINDOWS.items():
                    if list(m.fixture.TILE_WINDOWS).index(key) >= count: continue
                    expectation = m.fixture.tile_expectation(key)
                    levels = [[window[2]//2,window[3]//2]] if key != 'r160_c160' else []
                    tiles[key] = {**{k:v for k,v in expectation.items() if k not in ('band_nodata','valid_pixels')},
                        'name':key+'.tif','window':list(window),'artifact_type':'raster','format':'tiff','profile':'cog','size_bytes':size,'uploaded_files':1,
                        'bands':[{'dtype':'Float64','nodata':-9999.,'nodata_is_nan':False,'overviews':levels}]*2}
                    locator = f'addp://engine/2/path/raster-target/{prefix}/{key}.tif?type=object'
                    resources[key] = {'resource':{'locator':locator,'type':'object','write_mode':mode}}
                    targets.append({'task_id':'save_tiles','tile_key':key,'locator':locator,'type':'object'})
                    scans.append({'status':'submitted','target_locator':locator,'execution_id':'auto-'+identifier+'-'+key})
                    if mode == 'replace': self.targets[48 + list(m.fixture.TILE_WINDOWS).index(key)]['size_bytes'] = size
                if failed:
                    base_id = 52 if prefix == m.fixture.TILE_FAILURE_PREFIX else 53 if prefix == 'tiles-cleanup-mid' else 54
                    for index, key in enumerate(tiles):
                        self.targets[base_id+index] = copy.deepcopy(self.targets[48+index])
                        self.targets[base_id+index].update(id=base_id+index, full_name='raster-target/'+prefix+'/'+key+'.tif', size_bytes=1024)
                execution = {'execution_id':identifier,'module':'develop','status':'failed' if failed else 'success','metadata':{
                    'outputs':{'save_tiles':{'tiles':resources}},
                    'lineage_facts':{'schema_version':'addp.lineage-facts/v1','inputs':[{'locator':source}],
                        'outputs':[{'locator':item['locator'],'write_mode':mode} for item in targets]},
                    'result':{'final_result':{'artifact_type':'raster_tiles','source_width':256,'source_height':256,'tile_size':[160,160],
                        'columns':2,'rows':2,'total_tiles':4,'completed_tiles':count,'tiles':tiles},'produced_targets':targets,'meta_scan_runs':scans}}}
                if failed:
                    execution['metadata'].pop('lineage_facts')
                    if count < 4:
                        execution['metadata']['result']['final_result'].update(failed_tile=m.fixture.TILE_FAILURE_KEY, publication='not_attempted' if prefix in m.fixture.TILE_CLEANUP_CASES else 'unknown')
                self.executions[identifier] = self.mutate(execution)
                return m.support.Response(200, {'execution_id':identifier}, {})
            mode = definition['tasks'][-1]['params']['write_mode']
            identifier = f'run-{len(self.executions) + 1}'
            status = 'failed' if identifier == 'run-2' else 'success'
            sources = [task['params']['locator'] for task in definition['tasks'] if task['operator'] in ('raster_load', 'raster_to_cog')]
            target = f'addp://engine/2/path/raster-target/{definition["tasks"][-1]["params"]["target_name"]}?type=object'
            spatial = 'mosaic' in definition['tasks'][-1]['depends_on']
            case_name = definition['tasks'][-1]['params']['target_name'].removesuffix('.cog.tif')
            utility = case_name in m.fixture.UTILITY_CASES
            expectation = m.fixture.utility_expectation() if utility else m.fixture.computed_expectation(case_name) if case_name in m.fixture.GRID_CASES + m.fixture.MULTIBAND_CASES + m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES + m.fixture.OUTSIDE_CASES + m.fixture.UPDATE_CASES else m.fixture.artifact_expectation(spatial)
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
                                                    **({'color_interpretation': (['Gray', 'Undefined'] if case_name in m.fixture.UPDATE_CASES + m.fixture.AGGREGATE_MULTIBAND_CASES else ['Gray', 'Undefined', 'Alpha'] if case_name in m.fixture.OUTSIDE_CASES else ['Red', 'Green', 'Blue'] if case_name == 'multiraster-rgb' else ['Gray'])[index], 'overviews': [[expectation['width']//2, expectation['height']//2]]} if case_name in m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES + m.fixture.OUTSIDE_CASES + m.fixture.UPDATE_CASES else {})}
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
            locator = f"addp://engine/2/path/{self.targets[target_id]['full_name']}?type=object"
            latest = next(execution for execution in reversed(list(self.executions.values()))
                if execution['status'] == 'success' and any(output['locator'] == locator
                    for output in execution.get('metadata', {}).get('lineage_facts', {}).get('outputs', [])))
            sources = [self.source, self.spatial_source, self.multiband_source, self.average_source,
                       self.finite_source, self.non_cog_source, self.rotated_source, self.topology_source]
            by_locator = {m.support.build_item_locator(1, item): item['id'] for item in sources}
            by_locator.update({m.support.build_item_locator(2, item): item['id'] for item in self.targets.values()})
            result = {'truncated': self.no_graph, 'edges': [
                {'source': {'item_id': by_locator[source['locator']]}, 'target': {'item_id': target_id},
                 'status': 'active', 'relation_kind': self.relation_kind,
                 'evidence': {'execution_id': latest['execution_id']}}
                for source in latest['metadata']['lineage_facts']['inputs']]}
        else:
            raise AssertionError(f'unexpected API: {method} {path}')
        return m.support.Response(200, result, {})


class RasterWorkflowOnlineTest(unittest.TestCase):
    def test_size_failure_identifies_numeric_values_without_echoing_untrusted_strings(self):
        with self.assertRaisesRegex(m.SuiteError, r'result.cog.tif.*artifact=int:12, object=NoneType:None, DataItem=int:13'):
            m.strict_sizes({'size_bytes':12},{},{'size_bytes':13},'result.cog.tif')
        with self.assertRaises(m.SuiteError) as error:
            m.strict_sizes({'size_bytes':12},{'size_bytes':'private credential'},{'size_bytes':12})
        self.assertIn('object=str',str(error.exception))
        self.assertNotIn('private credential',str(error.exception))

    def test_all_confirmed_writes_archive_strict_three_party_sizes(self):
        report = self.run_scene()
        records = [sizes for outputs in report['size_evidence'].values() for sizes in outputs.values()]
        # 28 final single rasters plus the initial replaced raster, eight tile writes,
        # and six confirmed tiles across the three failed executions.
        self.assertEqual(len(records), 45)
        self.assertEqual(len({name for outputs in report['size_evidence'].values() for name in outputs}), 40)
        for sizes in records:
            self.assertEqual(set(sizes), {'artifact_size_bytes', 'object_size_bytes', 'data_item_size_bytes'})
            self.assertTrue(all(type(value) is int and value > 0 for value in sizes.values()))
            self.assertEqual(len(set(sizes.values())), 1)
        self.assertNotIn('run-2', report['size_evidence'])

    def test_basic_spatial_grid_and_multiband_reject_all_three_size_faults(self):
        cases = [('result', 'verify-create'), ('result', 'verify-replace'),
                 ('mosaic-first', 'verify-mosaic-first'), ('clip-polygon', 'verify-clip-polygon'),
                 ('multiband-alpha', 'verify-multiband-alpha')]
        for name, action in cases:
            for owner in ('artifact', 'physical', 'item'):
                for value in (1024.0, True, 0, -1, 1025, None):
                    with self.subTest(name=name, action=action, owner=owner, value=value):
                        self.setUp()
                        if owner == 'artifact':
                            def mutate(execution):
                                metadata = execution['metadata']
                                resource = metadata.get('outputs', {}).get('save', {}).get('resource', {})
                                matches = resource.get('locator', '').endswith('/'+name+'.cog.tif?type=object')
                                replace = resource.get('write_mode') == 'replace'
                                if matches and (name != 'result' or replace == (action == 'verify-replace')):
                                    metadata['result']['final_result']['size_bytes'] = value
                                return execution
                            self.client.mutate = mutate
                        def physical(repo, env, current_action):
                            payload = self.physical(repo, env, current_action)
                            if current_action == action:
                                if owner == 'physical': payload['size_bytes'] = value
                                if owner == 'item':
                                    for target in self.client.targets.values():
                                        if target['full_name'] == 'raster-target/'+name+'.cog.tif':
                                            target['size_bytes'] = value
                            return payload
                        with self.assertRaisesRegex(m.SuiteError, 'positive integer'):
                            self.run_scene(physical)

    def test_update_keeps_base_grid_and_both_real_source_lineage(self):
        report=self.run_scene()
        case=report['update_cases'][0]
        self.assertEqual([edge['source_item_id'] for edge in case['lineage']],[10,12])
        self.assertEqual([edge['target_item_id'] for edge in case['lineage']],[47,47])
        self.assertEqual(case['physical']['band_valid_pixels'],[65535,65536])
        self.assertEqual(len(case['physical']['preserved_sha256']),27)
        self.assertEqual([b['case_name'] for b in case['browser']],['update-valid-base','update-valid-patch'])
        for b in case['browser']:
            self.assertEqual(len(b['source_locators']),2)
        definition=next(body['content']['workflow_definition'] for method,path,body in self.client.calls
            if method=='POST' and path=='/api/v1/develop/executions' and
            any(task['operator']=='raster_update' for task in body['content']['workflow_definition']['tasks']))
        self.assertEqual([task['operator'] for task in definition['tasks']],
            ['raster_load','raster_load','raster_clip','raster_align','raster_update','raster_build_overviews','raster_save'])
        self.assertEqual(definition['tasks'][4]['params']['input_raster']['$ref'],'base')
        self.assertEqual(definition['tasks'][4]['params']['update_raster']['$ref'],'align')
        self.assertTrue(all(len(c['physical']['preserved_sha256'])==27 for c in report['footprint_cases']))

    def test_update_rejects_size_band_validity_sources_and_prior_mutation(self):
        for fault in ('declared-size','fractional-size','item-size','grid','colors','second-band','levels','source','prior','input'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    if execution['execution_id']=='run-39':
                        artifact=execution['metadata']['result']['final_result']
                        if fault=='declared-size': artifact['size_bytes']=1
                        if fault=='fractional-size': artifact['size_bytes']=1024.0
                        if fault=='grid': artifact['transform'][1]=.02
                        if fault=='colors': artifact['bands'][1]['color_interpretation']='Alpha'
                        if fault=='input': execution['metadata']['lineage_facts']['inputs'].pop()
                    return execution
                self.client.mutate=mutate
                if fault=='item-size': self.client.targets[47]['size_bytes']=1025
                def physical(repo,env,action):
                    payload=self.physical(repo,env,action)
                    if action=='verify-update-valid':
                        if fault=='second-band': payload['band_valid_pixels']=[65535,65535]
                        if fault=='levels': payload['overview_sizes']=[[64,64]]
                        if fault=='source': payload['source_unchanged']=False
                        if fault=='prior': payload['preserved_sha256']['clip-outside.cog.tif']='changed'
                    return payload
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_physical_adapter_dispatches_aggregation_expectation(self):
        import json
        from subprocess import CompletedProcess
        from unittest.mock import patch
        for case in m.fixture.AGGREGATE_SINGLE_BAND_CASES:
            payload = {'cog_valid': True, 'source_unchanged': True, 'valid_pixels': 4472}
            with self.subTest(case=case), patch.object(m.subprocess, 'run', return_value=CompletedProcess([],0,json.dumps(payload),'')):
                self.assertEqual(m.physical(Path('.'), {}, 'verify-'+case), payload)
            # A full-source count cannot satisfy a smaller aggregation grid.
            for wrong in (65535, 4471, True):
                invalid = {**payload, 'valid_pixels': wrong}
                with self.subTest(case=case,count=wrong), patch.object(m.subprocess, 'run', return_value=CompletedProcess([],0,json.dumps(invalid),'')):
                    with self.assertRaises(m.SuiteError):
                        m.physical(Path('.'), {}, 'verify-'+case)

    def test_physical_adapter_dispatches_outside_expectation(self):
        import json
        from subprocess import CompletedProcess
        from unittest.mock import patch
        for count in (53247, 65535, 12288, 53246, True):
            payload = {'cog_valid': True, 'source_unchanged': True, 'valid_pixels': count}
            with self.subTest(count=count), patch.object(m.subprocess, 'run', return_value=CompletedProcess([], 0, json.dumps(payload), '')):
                if count == 53247:
                    self.assertEqual(m.physical(Path('.'), {}, 'verify-clip-outside'), payload)
                else:
                    with self.assertRaises(m.SuiteError):
                        m.physical(Path('.'), {}, 'verify-clip-outside')

    def test_outside_clipping_saves_both_bands_alpha_and_uses_automatic_lineage(self):
        case = self.run_scene()['outside_cases'][0]
        self.assertEqual(case['lineage']['source_item_id'], 10)
        self.assertEqual(case['lineage']['target_item_id'], 46)
        self.assertEqual(case['physical']['band_valid_pixels'], [53247, 53247])
        self.assertEqual(case['physical']['invalid_pixels'], 12289)
        self.assertEqual(case['physical']['color_interpretations'], ['Gray', 'Undefined', 'Alpha'])
        self.assertEqual(len(case['physical']['preserved_sha256']), 26)
        definition = [body['content']['workflow_definition'] for method, path, body in self.client.calls
            if method == 'POST' and path == '/api/v1/develop/executions' and any(task['operator'] == 'raster_clip' and task['params'].get('mode') == 'outside' for task in body['content']['workflow_definition']['tasks'])][-1]
        self.assertEqual([task['operator'] for task in definition['tasks']],
            ['raster_load', 'raster_clip', 'raster_build_overviews', 'raster_save'])
        self.assertEqual(definition['tasks'][1]['params']['mode'], 'outside')
        self.assertEqual(len(definition['tasks'][1]['params']['geometry']['coordinates']), 2)
        self.assertEqual(case['browser']['source_locators'], [case['browser']['source_locator']])

    def test_outside_clipping_rejects_size_grid_alpha_band_counts_and_prior_mutation(self):
        for fault in ('declared-size', 'fractional-size', 'item-size', 'grid', 'colors', 'counts', 'levels', 'source', 'prior'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    if execution['execution_id'] == 'run-38':
                        artifact = execution['metadata']['result']['final_result']
                        if fault == 'declared-size': artifact['size_bytes'] = 1
                        if fault == 'fractional-size': artifact['size_bytes'] = 1024.0
                        if fault == 'grid': artifact['width'] = 255
                        if fault == 'colors': artifact['bands'][-1]['color_interpretation'] = 'Undefined'
                    return execution
                self.client.mutate = mutate
                if fault == 'item-size': self.client.targets[46]['size_bytes'] = 1025
                def physical(repo, env, action):
                    payload = self.physical(repo, env, action)
                    if action == 'verify-clip-outside':
                        if fault == 'counts': payload['band_valid_pixels'] = [53247, 65535]
                        if fault == 'levels': payload['overview_sizes'] = [[64, 64]]
                        if fault == 'source': payload['source_unchanged'] = False
                        if fault == 'prior': payload['preserved_sha256']['aggregate-sum.cog.tif'] = 'changed'
                    return payload
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_footprint_default_union_explicit_intersection_and_readonly_browser(self):
        cases = self.run_scene()['footprint_cases']
        self.assertEqual([item['case_name'] for item in cases], ['footprint-any', 'footprint-all', 'footprint-band-2', 'footprint-holes', 'footprint-rotated', 'footprint-diagonal', 'footprint-pinch'])
        for case in cases:
            self.assertEqual(case['browser']['source_item_id'], 17 if case['case_name'] in m.fixture.TOPOLOGY_BANDS else 46 if case['case_name'] == 'footprint-holes' else 16 if case['case_name'] == 'footprint-rotated' else 12)
            self.assertEqual(case['browser']['source_name'], m.fixture.footprint_source_name(case['case_name']))
            self.assertEqual(case['browser']['result_kind'], 'json')
            self.assertEqual(case['browser']['expected_result'], case['json_result'])
            self.assertEqual(len(case['physical']['preserved_sha256']), 27)
        definitions = [body['content']['workflow_definition'] for method, path, body in self.client.calls
            if method == 'POST' and path == '/api/v1/develop/executions'
            and body['content']['workflow_definition']['tasks'][-1]['operator'] == 'raster_footprint']
        self.assertNotIn('validity', definitions[0]['tasks'][-1]['params'])
        self.assertEqual(definitions[1]['tasks'][-1]['params']['validity'], 'all')
        self.assertEqual(definitions[2]['tasks'][-1]['params']['bands'], [2])
        self.assertNotIn('validity', definitions[2]['tasks'][-1]['params'])
        self.assertIn('raster-target/clip-outside.cog.tif?', definitions[3]['tasks'][0]['params']['locator'])
        self.assertNotIn('bands', definitions[3]['tasks'][-1]['params'])
        self.assertNotIn('validity', definitions[3]['tasks'][-1]['params'])
        self.assertIn('raster-source/' + m.fixture.ROTATED_SOURCE + '?', definitions[4]['tasks'][0]['params']['locator'])
        self.assertEqual(definitions[4]['tasks'][-1]['params'], {'input_raster':{'$ref':'load','port':'default'}})
        self.assertEqual([sum(m.fixture.footprint_pixels(c['case_name'])) for c in cases], [65532, 65524, 65528, 53247, 65532, 4, 14])

    def test_topology_footprints_select_independent_bands(self):
        for case, band in m.fixture.TOPOLOGY_BANDS.items():
            definition = m.footprint_workflow('topology-source', case)
            self.assertEqual(definition['tasks'][-1]['params']['bands'], [band])
            self.assertNotIn('validity', definition['tasks'][-1]['params'])

    def test_topology_footprints_reject_corner_merge_self_touch_and_displacement(self):
        for fault in ('merged', 'self-touch', 'displaced', 'duplicate-edge'):
            with self.subTest(fault=fault):
                case = 'footprint-diagonal' if fault in ('merged', 'displaced') else 'footprint-pinch'
                result = footprint_result(case)
                polygons = result['features'][0]['geometry']['coordinates']
                if fault == 'merged':
                    # A single self-touching exterior encloses exactly the same four cells.
                    grid = ([(64, 64)] + [point for i in range(64, 68) for point in ((i+1, i), (i+1, i+1))] +
                            [point for i in range(67, 63, -1) for point in ((i, i+1), (i, i))])
                    gt = m.fixture.TRANSFORM
                    polygons[:] = [[[[gt[0]+x*gt[1], gt[3]+y*gt[5]] for x, y in grid]]]
                elif fault == 'self-touch':
                    first, second = polygons[0][1:]
                    # One figure-eight hole retains area/coverage but has an invalid ring.
                    shared = first[2]
                    polygons[0][1:] = [[shared] + first[3:] + first[1:3] + second[1:] ]
                elif fault == 'displaced':
                    for point in polygons[0][0]: point[0] += .01
                else:
                    polygons[0][0].insert(1, polygons[0][0][0][:])
                execution = {'metadata': {'result': {'summary': {'has_result': True}, 'final_result': result}}}
                with self.assertRaises(m.SuiteError): m.validate_footprint(execution, case)

    def test_footprint_oracle_accepts_equivalent_ring_start_and_orientation(self):
        for case in m.fixture.FOOTPRINT_CASES:
            result = footprint_result(case)
            ring = result['features'][0]['geometry']['coordinates'][0][0]
            ring = list(reversed(ring[:-1]))
            ring = ring[2:] + ring[:2]
            result['features'][0]['geometry']['coordinates'][0][0] = ring + [ring[0]]
            execution = {'metadata': {'result': {'summary': {'has_result': True}, 'final_result': result}}}
            self.assertEqual(m.validate_footprint(execution, case), result)

    def test_footprint_rejects_invalid_coverage_grid_area_schema_and_side_effects(self):
        for fault in ('wrong-rule', 'wrong-cells', 'crs', 'extent', 'off-grid', 'diagonal', 'unclosed', 'nonfinite', 'bool', 'crs-grid', 'type', 'properties', 'scan', 'output', 'lineage'):
            with self.subTest(fault=fault):
                result = footprint_result('footprint-any')
                geometry = result['features'][0]['geometry']
                ring = geometry['coordinates'][0][0]
                execution = {'metadata': {'result': {'summary': {'has_result': True}, 'final_result': result}}}
                if fault == 'wrong-rule': execution['metadata']['result']['final_result'] = footprint_result('footprint-all')
                if fault == 'crs': result['crs'] = {'type': 'name', 'properties': {'name': 'EPSG:3857'}}
                if fault == 'wrong-cells':
                    for index in (1, 2): ring[index][0] = 110.02
                    for index in (3, 4): ring[index][0] = 110.04
                if fault == 'extent': geometry['coordinates'] = footprint_result('footprint-any')['features'][0]['geometry']['coordinates']; geometry['coordinates'][0][0] = [[110,20.32],[112.56,20.32],[112.56,17.76],[110,17.76],[110,20.32]]
                if fault == 'off-grid': ring[2][0] += .005
                if fault == 'diagonal': ring[2][0] += .01
                if fault == 'unclosed': ring.pop()
                if fault == 'nonfinite': ring[2][0] = float('nan')
                if fault == 'bool': ring[2][0] = True
                if fault == 'crs-grid': ring[2][0] = 200
                if fault == 'type': geometry['type'] = 'Polygon'
                if fault == 'properties': result['features'][0]['properties'] = {'pixels': 65532}
                if fault == 'scan': execution['metadata']['result']['meta_scan_runs'] = [{}]
                if fault == 'output': execution['outputs'] = {'unexpected': {}}
                if fault == 'lineage': execution['metadata']['lineage_facts'] = {'inputs': []}
                with self.assertRaises(m.SuiteError): m.validate_footprint(execution, 'footprint-any')

    def test_selected_band_footprint_rejects_union_intersection_and_same_area_wrong_band(self):
        wrong_band = footprint_result('footprint-band-2')
        # Band 1 loses cell 0 instead of cell 1. Its area is the same as band 2,
        # so only the independent per-pixel coverage check can reject it.
        ring = [(2, 0), (4, 0), (4, 2), (6, 2), (6, 0), (256, 0), (256, 256), (0, 256), (0, 2), (2, 2), (2, 0)]
        wrong_band['features'][0]['geometry']['coordinates'] = [[[[110+x*.01, 20.32-y*.01] for x, y in ring]]]
        for result in (footprint_result('footprint-any'), footprint_result('footprint-all'), wrong_band):
            execution = {'metadata': {'result': {'summary': {'has_result': True}, 'final_result': result}}}
            with self.assertRaises(m.SuiteError): m.validate_footprint(execution, 'footprint-band-2')

    def test_footprint_holes_rejects_filling_missing_island_and_same_area_shift(self):
        for fault in ('filled', 'missing-island', 'shifted-hole'):
            with self.subTest(fault=fault):
                result = footprint_result('footprint-holes')
                polygons = result['features'][0]['geometry']['coordinates']
                if fault == 'filled': polygons[0].pop()
                if fault == 'missing-island': polygons.pop()
                if fault == 'shifted-hole':
                    for point in polygons[0][1]: point[0] -= .32
                execution = {'metadata': {'result': {'summary': {'has_result': True}, 'final_result': result}}}
                message = 'coverage differs' if fault == 'shifted-hole' else 'topology differs'
                with self.assertRaisesRegex(m.SuiteError, message): m.validate_footprint(execution, 'footprint-holes')

    def test_footprint_holes_rejects_mutating_its_input_cog(self):
        def physical(repo, env, action):
            payload = self.physical(repo, env, action)
            if len(self.client.executions) == 43 and action == 'verify-update-valid':
                payload['preserved_sha256']['clip-outside.cog.tif'] = 'changed'
            return payload
        with self.assertRaisesRegex(m.SuiteError, 'changed an accepted artifact'): self.run_scene(physical)

    def test_rotated_footprint_rejects_dropped_or_swapped_rotation_and_half_pixel_shift(self):
        ring = [(0,0),(4,0),(4,2),(6,2),(6,0),(256,0),(256,256),(0,256),(0,0)]
        for transform in ((110,.01,0,20.32,0,-.02), (110,.01,.002,20.32,.004,-.02),
                          (110.005,.01,.004,20.321,.002,-.02)):
            with self.subTest(transform=transform):
                result = footprint_result('footprint-rotated')
                result['features'][0]['geometry']['coordinates'] = [[[[transform[0]+x*transform[1]+y*transform[2],
                    transform[3]+x*transform[4]+y*transform[5]] for x,y in ring]]]
                execution = {'metadata':{'result':{'summary':{'has_result':True},'final_result':result}}}
                with self.assertRaisesRegex(m.SuiteError, 'source pixel grid'):
                    m.validate_footprint(execution,'footprint-rotated')

    def test_rotated_footprint_rejects_same_area_wrong_invalid_cells(self):
        result = footprint_result('footprint-rotated')
        # Move the 2x2 transparent cell left by one cell; total area stays constant.
        transform = (110,.01,.004,20.32,.002,-.02)
        ring = [(0,0),(2,0),(2,2),(4,2),(4,0),(256,0),(256,256),(0,256),(0,0)]
        result['features'][0]['geometry']['coordinates'] = [[[[transform[0]+x*transform[1]+y*transform[2],
            transform[3]+x*transform[4]+y*transform[5]] for x,y in ring]]]
        execution = {'metadata':{'result':{'summary':{'has_result':True},'final_result':result}}}
        with self.assertRaisesRegex(m.SuiteError, 'coverage differs'):
            m.validate_footprint(execution,'footprint-rotated')

    def test_footprint_rejects_changes_to_last_and_prior_cog(self):
        for fault in ('last', 'prior'):
            with self.subTest(fault=fault):
                self.setUp()
                def physical(repo, env, action):
                    payload = self.physical(repo, env, action)
                    if len(self.client.executions) == 40 and action == 'verify-update-valid':
                        if fault == 'last': payload['sha256'] = 'changed'
                        else: payload['preserved_sha256']['aggregate-sum.cog.tif'] = 'changed'
                    return payload
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def setUp(self):
        self.client = Client()
        self.env = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_RASTER_SOURCE_ENGINE_ID': '1',
                    'ADDP_ONLINE_RASTER_TARGET_ENGINE_ID': '2', 'ADDP_ONLINE_TEST_RUN_ID': 'raster-test',
                    'ADDP_ONLINE_TEST_TIMEOUT_SECONDS': '.01'}
        self.physical_actions = []

    def physical(self, repo, env, action):
        self.physical_actions.append(action)
        if action.startswith('verify-mosaic-'):
            overlap = action.removeprefix('verify-mosaic-')
            return {'size_bytes': 1024, 'sha256': 'spatial-' + overlap, 'cog_valid': True, 'has_overviews': True, 'source_unchanged': True,
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
        if case_name in m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES + m.fixture.OUTSIDE_CASES + m.fixture.UPDATE_CASES:
            preserved = {'result.cog.tif': 'new', 'mosaic-first.cog.tif': 'spatial-first', 'mosaic-last.cog.tif': 'spatial-last'}
            for prior in m.fixture.GRID_CASES + m.fixture.MULTIBAND_CASES + m.fixture.UTILITY_CASES + (m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES + m.fixture.OUTSIDE_CASES + m.fixture.UPDATE_CASES)[:(m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES + m.fixture.OUTSIDE_CASES + m.fixture.UPDATE_CASES).index(case_name)]:
                preserved[prior + '.cog.tif'] = prior
            return {'sha256': case_name, 'case_name': case_name, 'preserved_sha256': preserved,
                'cog_valid': True, 'has_overviews': True, 'source_unchanged': True, 'valid_pixels': m.fixture.computed_expectation(case_name)['valid_pixels'],
                'invalid_pixels': m.fixture.computed_expectation(case_name)['width'] * m.fixture.computed_expectation(case_name)['height'] - m.fixture.computed_expectation(case_name)['valid_pixels'],
                'band_valid_pixels': m.fixture.computed_expectation(case_name).get('band_valid_pixels', [m.fixture.computed_expectation(case_name)['valid_pixels']] * (m.fixture.computed_expectation(case_name)['band_count'] - (case_name in m.fixture.OUTSIDE_CASES))),
                'color_interpretations': ['Gray', 'Undefined'] if case_name in m.fixture.UPDATE_CASES + m.fixture.AGGREGATE_MULTIBAND_CASES else ['Gray', 'Undefined', 'Alpha'] if case_name in m.fixture.OUTSIDE_CASES else ['Red', 'Green', 'Blue'] if case_name == 'multiraster-rgb' else ['Gray'],
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
            return {**extra, 'size_bytes': 1024, 'sha256': case_name, 'case_name': case_name, 'preserved_sha256': preserved,
                    'cog_valid': True, 'has_overviews': case_name=='clip-polygon' or 'bilinear' in case_name or 'fractional' in case_name or 'finite' in case_name,
                    'source_unchanged': True, 'valid_pixels': m.fixture.computed_expectation(case_name)['valid_pixels']}
        if action == 'verify-tiles-failure' or action in m.fixture.CLEANUP_ACTIONS:
            prefix = action.removeprefix('verify-')
            count = m.fixture.TILE_CLEANUP_CASES[prefix][1] if prefix in m.fixture.TILE_CLEANUP_CASES else 1
            initial = self.physical(repo, env, 'verify-tiles')
            replaced = self.physical(repo, env, 'verify-tiles-replace')
            cleanup = {}
            if prefix in m.fixture.TILE_CLEANUP_CASES:
                key = m.fixture.TILE_CLEANUP_CASES[prefix][0]
                prior = {m.fixture.TILE_FAILURE_PREFIX: {'r0_c0': 'r0_c0'}}
                if prefix == 'tiles-cleanup-final': prior['tiles-cleanup-mid'] = {'r0_c0':'r0_c0'}
                cleanup = {'preserved_failure_sha256': prior, 'cleanup_fault': {'prefix':prefix,'tile_key':key,
                    'backend_status':200,'obstacle_created':True,'workspace_removed':True,
                    'sha256':key,'size_bytes':1024}}
            return {**initial, **cleanup, 'tiles': {key: initial['tiles'][key] for key in list(m.fixture.TILE_WINDOWS)[:count]},
                'unrelated_sha256': replaced['unrelated_sha256'],
                'preserved_tiles_sha256': {key: tile['sha256'] for key, tile in replaced['tiles'].items()}}
        if action in ('verify-tiles', 'prepare-tiles-replace', 'verify-tiles-replace'):
            preserved = {'result.cog.tif':'new','mosaic-first.cog.tif':'spatial-first','mosaic-last.cog.tif':'spatial-last'}
            for prior in m.fixture.GRID_CASES+m.fixture.MULTIBAND_CASES+m.fixture.UTILITY_CASES+m.fixture.FOUNDATION_CASES+m.fixture.RECLASS_CASES+m.fixture.AGGREGATE_CASES+m.fixture.OUTSIDE_CASES+m.fixture.UPDATE_CASES:
                preserved[prior+'.cog.tif'] = prior
            return {'source_unchanged':True,'preserved_sha256':preserved,'tiles':{key:{
                'cog_valid':True,'source_unchanged':True,'size_bytes':2048 if action == 'verify-tiles-replace' else 1024,
                'sha256':key+'-replaced' if action == 'verify-tiles-replace' else key,
                'compression':'NONE' if action == 'verify-tiles-replace' else 'DEFLATE',
                'has_overviews':key!='r160_c160','overview_sizes':[[window[2]//2,window[3]//2]] if key!='r160_c160' else [],
                'valid_pixels':m.fixture.tile_expectation(key)['valid_pixels']}
                for key,window in m.fixture.TILE_WINDOWS.items()},
                **({'unrelated_sha256':m.hashlib.sha256(m.fixture.TILE_KEEP_BYTES).hexdigest()} if action != 'verify-tiles' else {})}
        if action == 'verify-analysis':
            return {'sha256': 'new', 'cog_valid': True, 'source_unchanged': True, 'valid_pixels': 65535,
                    'first_sha256': 'spatial-first', 'last_sha256': 'spatial-last'}
        return {'size_bytes': 1024, 'sha256': 'new' if action == 'verify-replace' else 'original', 'cog_valid': True, 'has_overviews': True,
                'source_unchanged': True, 'valid_pixels': 65535}

    def run_scene(self, physical=None):
        return m.run_scenario(Path('.'), self.env, self.client, physical or self.physical,
                              lambda repo, env, evidence: evidence,
                              lambda repo, env, identifier, timeout: {'execution_id': identifier,
                                  'cause': 'target_already_exists', 'source': 'develop_runtime_log'})

    def test_physical_failure_propagates_only_named_private_event_diagnostics(self):
        import json
        from subprocess import CompletedProcess
        from unittest.mock import patch
        for reason in ('cleanup_event_permission_denied','cleanup_event_missing','physical_worker_failed','PermissionError: /private/secret'):
            stderr = 'Raster physical fixture failed: '+reason
            with patch.object(m.subprocess, 'run', return_value=CompletedProcess([],1,'',stderr)):
                with self.assertRaises(m.SuiteError) as error: m.physical(Path('.'),{},'verify-tiles-cleanup-mid')
                self.assertNotIn('/private', str(error.exception))
                if reason != 'PermissionError: /private/secret': self.assertIn(reason, str(error.exception))
                else: self.assertNotIn('PermissionError', str(error.exception))

    def test_cleanup_failures_keep_confirmed_one_or_four_resources_and_fresh_scans(self):
        report = self.run_scene()
        scans = {scan['execution_id'] for scan in report['tiling_automatic_scan_runs'] + report['tiling_replace_automatic_scan_runs'] + report['tiling_failed_publication']['automatic_scan_runs']}
        for prefix, (_, count) in m.fixture.TILE_CLEANUP_CASES.items():
            case = report['tiling_cleanup_failures'][prefix]
            manifest = case['manifest']
            self.assertEqual(case['status'], 'failed')
            self.assertEqual(manifest['completed_tiles'], count)
            self.assertEqual(len(case['confirmed_items']), count)
            self.assertEqual(len(case['automatic_scan_runs']), count)
            if count < 4:
                self.assertEqual(manifest['failed_tile'], 'r0_c160')
                self.assertEqual(manifest['publication'], 'not_attempted')
            else:
                self.assertNotIn('failed_tile', manifest)
                self.assertNotIn('publication', manifest)
            current = {scan['execution_id'] for scan in case['automatic_scan_runs']}
            self.assertFalse(scans & current)
            scans.update(current)
            self.assertTrue(case['physical']['cleanup_fault']['workspace_removed'])
            self.assertNotIn('workspace', case['physical']['cleanup_fault'])

    def test_cleanup_failures_reject_wrong_boundary_missing_confirmation_residuals_and_prior_mutation(self):
        for prefix in m.fixture.TILE_CLEANUP_CASES:
            for fault in ('publication','count','missing','backend','obstacle','workspace','hash','size','prior','private-path'):
                with self.subTest(prefix=prefix, fault=fault):
                    self.setUp()
                    def mutate(execution):
                        result = execution.get('metadata', {}).get('result', {})
                        if any('/'+prefix+'/' in scan.get('target_locator','') for scan in result.get('meta_scan_runs', [])):
                            manifest = result['final_result']
                            if fault == 'publication': manifest['publication'] = 'unknown'
                            if fault == 'count': manifest['completed_tiles'] = 0
                            if fault == 'missing': manifest['tiles'].pop('r0_c0')
                        return execution
                    self.client.mutate = mutate
                    def physical(repo, env, action):
                        evidence = self.physical(repo, env, action)
                        if action == 'verify-'+prefix:
                            event = evidence['cleanup_fault']
                            if fault == 'backend': event['backend_status'] = 403
                            if fault == 'obstacle': event['obstacle_created'] = False
                            if fault == 'workspace': event['workspace_removed'] = False
                            if fault == 'hash': event['sha256'] = 'wrong'
                            if fault == 'size': event['size_bytes'] = 1024.0
                            if fault == 'prior': evidence['preserved_failure_sha256'] = {}
                            if fault == 'private-path': event['workspace'] = '/tmp/private'
                        return evidence
                    with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_failed_batch_retains_one_confirmed_resource_and_fresh_scan_without_success_lineage(self):
        report = self.run_scene()
        partial = report['tiling_failed_publication']
        self.assertEqual(partial['status'], 'failed')
        self.assertEqual(partial['manifest']['completed_tiles'], 1)
        self.assertEqual(partial['manifest']['failed_tile'], 'r0_c160')
        self.assertEqual(partial['manifest']['publication'], 'unknown')
        self.assertEqual(set(partial['manifest']['tiles']), {'r0_c0'})
        self.assertEqual(partial['manifest']['tiles']['r0_c0']['size_bytes'], partial['confirmed_items']['r0_c0']['data_item_size_bytes'])
        self.assertEqual(partial['confirmed_items']['r0_c0']['data_item_size_bytes'], partial['physical']['tiles']['r0_c0']['size_bytes'])
        self.assertEqual(len(partial['automatic_scan_runs']), 1)
        self.assertIn('/tiles-write-failure/r0_c0.tif?', partial['outputs']['save_tiles']['tiles']['r0_c0']['resource']['locator'])
        self.assertNotIn('lineage_facts', self.client.executions[partial['execution_id']]['metadata'])
        self.assertEqual(partial['physical']['preserved_tiles_sha256'], {
            c['tile_key']: c['physical']['sha256'] for c in report['tiling_replace_cases']})

    def test_failed_batch_rejects_invalid_partial_publication_outputs_scans_and_lineage(self):
        for fault in ('count', 'failed-key', 'publication', 'missing', 'locator', 'unconfirmed', 'no-scan', 'old-scan', 'lineage', 'size', 'float-size', 'bool-size', 'meta-size', 'extra-meta'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    if execution['status'] == 'failed' and 'result' in execution['metadata']:
                        metadata = execution['metadata']; result = metadata['result']; manifest = result['final_result']
                        if fault == 'count': manifest['completed_tiles'] = 2
                        if fault == 'failed-key': manifest['failed_tile'] = 'r160_c0'
                        if fault == 'publication': manifest['publication'] = 'not_attempted'
                        if fault == 'missing': manifest['tiles'] = {}
                        if fault == 'locator': metadata['outputs']['save_tiles']['tiles']['r0_c0']['resource']['locator'] = 'wrong'
                        if fault == 'unconfirmed': metadata['outputs']['save_tiles']['tiles']['r0_c160'] = metadata['outputs']['save_tiles']['tiles']['r0_c0']
                        if fault == 'no-scan': result['meta_scan_runs'] = []
                        if fault == 'old-scan': result['meta_scan_runs'][0]['execution_id'] = 'auto-run-48-r0_c0'
                        if fault == 'lineage': metadata['lineage_facts'] = {'outputs': []}
                        if fault in ('size', 'float-size', 'bool-size'): manifest['tiles']['r0_c0']['size_bytes'] = {'size':1025, 'float-size':1024., 'bool-size':True}[fault]
                        if fault == 'meta-size': self.client.targets[52]['size_bytes'] = 1025
                        if fault == 'extra-meta':
                            self.client.targets[53] = {**self.client.targets[52], 'id':53, 'full_name':'raster-target/tiles-write-failure/r0_c160.tif'}
                    return execution
                self.client.mutate = mutate
                with self.assertRaises(m.SuiteError): self.run_scene()

    def test_failed_batch_rejects_changed_accepted_bytes_and_wrong_physical_prefix(self):
        for fault in ('source', 'prior', 'tiles', 'sentinel', 'extra', 'size', 'pixel-count', 'layout', 'compression'):
            with self.subTest(fault=fault):
                self.setUp()
                def physical(repo, env, action):
                    result = self.physical(repo, env, action)
                    if action == 'verify-tiles-failure':
                        if fault == 'source': result['source_unchanged'] = False
                        if fault == 'prior': result['preserved_sha256']['update-valid.cog.tif'] = 'wrong'
                        if fault == 'tiles': result['preserved_tiles_sha256']['r0_c0'] = 'wrong'
                        if fault == 'sentinel': result['unrelated_sha256'] = 'wrong'
                        if fault == 'extra': result['tiles']['r0_c160'] = result['tiles']['r0_c0']
                        if fault == 'size': result['tiles']['r0_c0']['size_bytes'] = 1025
                        if fault == 'pixel-count': result['tiles']['r0_c0']['valid_pixels'] = 25600
                        if fault == 'layout': result['tiles']['r0_c0']['cog_valid'] = False
                        if fault == 'compression': result['tiles']['r0_c0']['compression'] = 'NONE'
                    return result
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_tiles_publish_four_individual_outputs_with_strict_sizes_and_prior_hashes(self):
        report = self.run_scene()
        cases = report['tiling_cases']
        self.assertEqual([case['tile_key'] for case in cases], list(m.fixture.TILE_WINDOWS))
        self.assertEqual(len(report['tiling_automatic_scan_runs']),4)
        self.assertEqual(len(report['tiling_preserved_sha256']),30)
        for case in cases:
            self.assertEqual(case['artifact']['size_bytes'],case['physical']['size_bytes'])
            self.assertEqual(case['artifact']['size_bytes'],case['data_item_size_bytes'])
            self.assertEqual(case['lineage']['source_item_id'],10)
            self.assertEqual(len(case['browser']['target_locators']),4)

    def test_batch_replace_keeps_identity_and_proves_four_new_writes_scans_and_relations(self):
        report = self.run_scene()
        first, replaced = report['tiling_cases'], report['tiling_replace_cases']
        self.assertEqual(len(replaced), 4)
        for before, after in zip(first, replaced):
            self.assertEqual(before['tile_key'], after['tile_key'])
            self.assertEqual(before['browser']['target_item_id'], after['browser']['target_item_id'])
            self.assertEqual(before['browser']['target_locator'], after['browser']['target_locator'])
            self.assertNotEqual(before['physical']['sha256'], after['physical']['sha256'])
            self.assertNotEqual(before['artifact']['size_bytes'], after['artifact']['size_bytes'])
            self.assertEqual(after['artifact']['size_bytes'], after['physical']['size_bytes'])
            self.assertEqual(after['artifact']['size_bytes'], after['data_item_size_bytes'])
            self.assertEqual(after['physical']['compression'], 'NONE')
            self.assertEqual(after['lineage']['execution_id'], 'run-48')
            self.assertEqual(after['browser']['case_name'], 'tiles-replace-' + after['tile_key'])
        self.assertEqual(report['tiling_replace_unrelated_sha256'], m.hashlib.sha256(m.fixture.TILE_KEEP_BYTES).hexdigest())
        self.assertFalse({x['execution_id'] for x in report['tiling_automatic_scan_runs']} &
            {x['execution_id'] for x in report['tiling_replace_automatic_scan_runs']})
        definitions = [body['content']['workflow_definition'] for method, path, body in self.client.calls
            if method == 'POST' and path == '/api/v1/develop/executions']
        params = next(d['tasks'][-1]['params'] for d in reversed(definitions) if d['tasks'][-1]['params']['target_name'] == 'tiles-basic')
        self.assertEqual((params['write_mode'], params['compression'], params['target_name']), ('replace', 'NONE', 'tiles-basic'))

    def test_batch_replace_rejects_old_bytes_size_compression_sentinel_or_changed_prior_files(self):
        for fault in ('bytes', 'same-size', 'physical-size', 'compression', 'sentinel', 'prior', 'source', 'missing'):
            with self.subTest(fault=fault):
                self.setUp()

                def mutate(execution):
                    if fault == 'same-size' and execution['execution_id'] == 'run-48':
                        execution['metadata']['result']['final_result']['tiles']['r0_c0']['size_bytes'] = 1024
                        self.client.targets[48]['size_bytes'] = 1024
                    return execution

                self.client.mutate = mutate

                def physical(repo, env, action):
                    result = self.physical(repo, env, action)
                    if action == 'verify-tiles-replace':
                        tile = result['tiles']['r0_c0']
                        if fault == 'bytes': tile['sha256'] = 'r0_c0'
                        if fault == 'same-size':
                            tile['size_bytes'] = 1024
                        if fault == 'physical-size': tile['size_bytes'] = 2049
                        if fault == 'compression': tile['compression'] = 'DEFLATE'
                        if fault == 'sentinel': result['unrelated_sha256'] = 'changed'
                        if fault == 'prior': result['preserved_sha256']['update-valid.cog.tif'] = 'changed'
                        if fault == 'source': result['source_unchanged'] = False
                        if fault == 'missing': result['tiles'].pop('r160_c160')
                    return result

                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_batch_replace_rejects_changed_identity_meta_size_reused_scan_and_old_lineage(self):
        for fault in ('identity', 'meta-size', 'scan', 'mode', 'lineage'):
            with self.subTest(fault=fault):
                self.setUp()
                request = self.client.request

                def mutate(execution):
                    if execution['execution_id'] == 'run-48':
                        if fault == 'meta-size': self.client.targets[48]['size_bytes'] = 2049
                        if fault == 'scan': execution['metadata']['result']['meta_scan_runs'][0]['execution_id'] = 'auto-run-47-r0_c0'
                        if fault == 'mode': execution['metadata']['outputs']['save_tiles']['tiles']['r0_c0']['resource']['write_mode'] = 'create'
                    return execution

                def checked_request(method, path, expected, body=None):
                    response = request(method, path, expected, body)
                    if 'run-48' in self.client.executions:
                        if fault == 'identity' and path == '/api/v1/meta/engines/2/items':
                            response.payload = copy.deepcopy(response.payload)
                            next(item for item in response.payload if item['id'] == 48)['id'] = 999
                        if fault == 'lineage' and path.startswith('/api/v1/meta/lineage/graph'):
                            response.payload['edges'][0]['evidence']['execution_id'] = 'run-47'
                    return response

                self.client.mutate = mutate
                self.client.request = checked_request
                with self.assertRaises(m.SuiteError): self.run_scene()

    def test_last_replacement_browser_failure_still_fails(self):
        checked = []

        def browser(repo, env, evidence):
            checked.append(evidence['case_name'])
            if evidence['case_name'] == 'tiles-replace-r160_c160':
                raise m.SuiteError('replacement browser failed')
            return evidence

        with self.assertRaisesRegex(m.SuiteError, 'replacement browser failed'):
            m.run_scenario(Path('.'), self.env, self.client, self.physical, browser,
                lambda repo, env, identifier, timeout: {'execution_id': identifier, 'cause': 'target_already_exists'})
        self.assertEqual(len(checked), 53)

    def test_persistent_lineage_and_all_browsers_finish_after_physical_writes(self):
        request = self.client.request
        browsers = []
        graphs = []

        def converged_request(method, path, expected, body=None):
            if path.startswith('/api/v1/meta/lineage/graph'):
                self.assertIn('verify-tiles', self.physical_actions)
                self.assertEqual(len(self.client.executions), 46 if 'verify-tiles-replace' in self.physical_actions else 45)
                graphs.append(path)
            return request(method, path, expected, body)

        def browser(repo, env, evidence):
            if evidence.get('result_kind') != 'json':
                self.assertIn('verify-tiles', self.physical_actions)
                self.assertEqual(len(self.client.executions), 46 if evidence['case_name'].startswith('tiles-replace-') else 45)
            browsers.append(evidence['case_name'])
            return evidence

        self.client.request = converged_request
        report = m.run_scenario(Path('.'), self.env, self.client, self.physical, browser,
            lambda repo, env, identifier, timeout: {'execution_id': identifier, 'cause': 'target_already_exists'})
        self.assertEqual(report['result'], 'passed')
        self.assertEqual(len(browsers), 53)
        self.assertEqual(len(set(browsers)), 53)
        self.assertEqual(len(graphs), 39)
        self.assertEqual(report['browser']['case_name'], 'band-math')
        for case in report['tiling_cases']:
            self.assertTrue(case['lineage'])
            self.assertEqual(case['browser']['case_name'], 'tiles-' + case['tile_key'])

    def test_deferred_truncated_lineage_still_fails_the_whole_suite(self):
        self.client.no_graph = True
        with self.assertRaises(m.SuiteError):
            self.run_scene()
        self.assertIn('verify-tiles', self.physical_actions)

    def test_last_tile_missing_or_wrong_execution_lineage_still_fails(self):
        for fault in ('missing', 'execution'):
            with self.subTest(fault=fault):
                self.setUp()
                request = self.client.request

                def last_tile_request(method, path, expected, body=None):
                    response = request(method, path, expected, body)
                    if (path.startswith('/api/v1/meta/lineage/graph')
                        and urllib.parse.parse_qs(urllib.parse.urlparse(path).query)['item_id'] == ['51']):
                        if fault == 'missing':
                            response.payload['edges'] = []
                        else:
                            response.payload['edges'][0]['evidence']['execution_id'] = 'another-execution'
                    return response

                self.client.request = last_tile_request
                with self.assertRaisesRegex(m.SuiteError, 'automatic raster lineage did not converge'):
                    self.run_scene()
                self.assertIn('verify-tiles', self.physical_actions)

    def test_last_deferred_browser_failure_cannot_return_a_passed_report(self):
        checked = []

        def browser(repo, env, evidence):
            checked.append(evidence['case_name'])
            if evidence['case_name'] == 'tiles-r160_c160':
                raise m.SuiteError('last tile browser failed')
            return evidence

        with self.assertRaisesRegex(m.SuiteError, 'last tile browser failed'):
            m.run_scenario(Path('.'), self.env, self.client, self.physical, browser,
                lambda repo, env, identifier, timeout: {'execution_id': identifier, 'cause': 'target_already_exists'})
        self.assertEqual(len(checked), 49)
        self.assertIn('verify-tiles', self.physical_actions)

    def test_tiles_reject_wrong_grid_partial_manifest_scope_locator_and_missing_scan(self):
        for fault in ('grid','partial','locator','scan','private','lineage','window','produced'):
            with self.subTest(fault=fault):
                self.setUp()
                def mutate(execution):
                    if execution['execution_id']=='run-47':
                        data=execution['metadata']; result=data['result']; manifest=result['final_result']
                        if fault=='grid': manifest['tile_size']=[128,128]
                        if fault=='partial': manifest['completed_tiles']=3
                        if fault=='locator': data['outputs']['save_tiles']['tiles']['r0_c0']['resource']['locator']='addp://engine/2/path/raster-target?type=bucket'
                        if fault=='scan': result['meta_scan_runs'].pop()
                        if fault=='private': manifest['tiles']['r0_c0']['workspace']='/private/runtime'
                        if fault=='lineage': data['lineage_facts']['outputs'].pop()
                        if fault=='window': manifest['tiles']['r0_c0']['window']=[0,0,128,128]
                        if fault=='produced': result['produced_targets'].pop()
                    return execution
                self.client.mutate=mutate
                with self.assertRaises(m.SuiteError): self.run_scene()

    def test_tiles_keep_strict_positive_triple_size_and_preservation_guards(self):
        for fault in ('zero','physical-size','meta-size','validity','prior','missing','overview','layout'):
            with self.subTest(fault=fault):
                self.setUp()
                if fault=='meta-size': self.client.targets[48]['size_bytes']=1025
                def physical(repo,env,action):
                    result=self.physical(repo,env,action)
                    if action=='verify-tiles':
                        fact=result['tiles']['r0_c0']
                        if fault=='zero': fact['size_bytes']=0
                        if fault=='physical-size': fact['size_bytes']=1025
                        if fault=='validity': fact['valid_pixels']-=1
                        if fault=='prior': result['preserved_sha256']['update-valid.cog.tif']='changed'
                        if fault=='missing': result['tiles'].pop('r160_c160')
                        if fault=='overview': fact['overview_sizes']=[]
                        if fault=='layout': fact['cog_valid']=False
                    return result
                with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_complete_chain_only_uses_source_manual_scan_and_owner_automatic_target_scan(self):
        report = self.run_scene()
        self.assertEqual([item['status'] for item in report['executions']], ['success', 'failed', 'success'])
        self.assertEqual(self.physical_actions, ['verify-create', 'verify-create', 'verify-replace',
            'verify-mosaic-first', 'verify-mosaic-last'] + ['verify-analysis'] * 4 + ['verify-' + name for name in m.fixture.GRID_CASES + m.fixture.MULTIBAND_CASES + m.fixture.UTILITY_CASES] + ['verify-utility-queries'] * 3 + ['verify-' + name for name in m.fixture.FOUNDATION_CASES + m.fixture.RECLASS_CASES + m.fixture.AGGREGATE_CASES + m.fixture.OUTSIDE_CASES + m.fixture.UPDATE_CASES] + ['verify-update-valid'] * len(m.fixture.FOOTPRINT_CASES) + ['verify-tiles', 'prepare-tiles-replace', 'verify-tiles-replace', 'verify-tiles-failure', 'verify-tiles', 'verify-tiles-replace', 'verify-tiles-cleanup-mid', 'verify-tiles', 'verify-tiles-replace', 'verify-tiles-cleanup-final', 'verify-tiles', 'verify-tiles-replace'])
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
            'raster_statistics', 'raster_histogram', 'raster_align', 'raster_stack', 'raster_select_bands', 'raster_reclassify', 'raster_aggregate', 'raster_footprint', 'raster_update', 'raster_tile', 'raster_save_tiles'})
        submitted = [definition for definition in submitted if any(
            task['params'].get('target_name') in {'build-overviews.cog.tif', 'to-cog.cog.tif'}
            for task in definition['tasks'])]
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
        cases = [case for case in report['aggregate_cases'] if case['case_name'] in m.fixture.AGGREGATE_SINGLE_BAND_CASES]
        self.assertEqual([case['case_name'] for case in cases],list(m.fixture.AGGREGATE_SINGLE_BAND_CASES))
        self.assertEqual([case['lineage']['target_item_id'] for case in cases],[42,43,44,45])
        self.assertEqual(len(cases[-1]['physical']['preserved_sha256']),25)
        for case in cases:
            self.assertEqual(case['lineage']['source_item_id'],10)
            sizes = [case['artifact']['size_bytes'],case['physical']['size_bytes'],case['data_item_size_bytes']]
            self.assertTrue(all(type(size) is int and size > 0 for size in sizes))
            self.assertEqual(len(set(sizes)),1)
            self.assertEqual(case['physical']['band_valid_pixels'],[4472])
            self.assertEqual(case['physical']['invalid_pixels'],0)
            self.assertEqual(case['physical']['overview_sizes'],[[43,26]])
            self.assertEqual(case['browser']['source_locators'],[case['browser']['source_locator']])
        definitions = [body['content']['workflow_definition'] for method,path,body in self.client.calls
            if method=='POST' and path=='/api/v1/develop/executions' and any(task['operator'] == 'raster_aggregate' for task in body['content']['workflow_definition']['tasks']) and body['content']['workflow_definition']['tasks'][-1]['params']['target_name'].removesuffix('.cog.tif') in m.fixture.AGGREGATE_SINGLE_BAND_CASES]
        for definition,method in zip(definitions,['mean','sum','min','max']):
            self.assertEqual([task['operator'] for task in definition['tasks']],
                ['raster_load','raster_select_bands','raster_aggregate','raster_build_overviews','raster_save'])
            self.assertEqual(definition['tasks'][1]['params']['bands'],[2])
            self.assertEqual(definition['tasks'][2]['params']['factors'],[3,5])
            self.assertEqual(definition['tasks'][2]['params']['method'],method)

    def test_multiband_aggregation_uses_exact_source_direct_alpha_dag_and_individual_evidence(self):
        report = self.run_scene()
        cases = [case for case in report['aggregate_cases'] if case['case_name'] in m.fixture.AGGREGATE_MULTIBAND_CASES]
        self.assertEqual([case['case_name'] for case in cases], list(m.fixture.AGGREGATE_MULTIBAND_CASES))
        self.assertEqual([case['lineage']['source_item_id'] for case in cases], [12, 16])
        scans = {case['automatic_target_scan_execution_id'] for case in report['aggregate_cases']}
        self.assertEqual(len(scans), 6)
        for case in cases:
            name = case['case_name']
            config = m.fixture.aggregate_config(name)
            self.assertEqual(case['physical']['band_valid_pixels'], m.fixture.aggregate_expectation(name)['band_valid_pixels'])
            self.assertEqual(case['physical']['color_interpretations'], ['Gray', 'Undefined'])
            self.assertEqual(case['artifact']['band_count'], 2)
            self.assertEqual(case['browser']['source_name'], config['source_name'])
            self.assertEqual(case['browser']['source_locators'], [case['browser']['source_locator']])
            self.assertEqual(report['size_evidence'][case['execution_id']][name+'.cog.tif'],
                {'artifact_size_bytes':1024, 'object_size_bytes':1024, 'data_item_size_bytes':1024})
            definition = next(body['content']['workflow_definition'] for method, path, body in self.client.calls
                if method == 'POST' and path == '/api/v1/develop/executions'
                and body['content']['workflow_definition']['tasks'][-1]['params'].get('target_name') == name+'.cog.tif')
            self.assertEqual([task['operator'] for task in definition['tasks']],
                ['raster_load', 'raster_aggregate', 'raster_build_overviews', 'raster_save'])
            self.assertEqual(definition['tasks'][1]['params']['factors'], config['factors'])
            self.assertEqual(definition['tasks'][1]['params']['method'], config['method'])
            self.assertEqual(definition['tasks'][1]['params']['input_raster'], {'$ref':'load', 'port':'default'})

    def test_multiband_aggregation_rejects_source_alpha_counts_rotation_preservation_and_size_faults(self):
        for case in m.fixture.AGGREGATE_MULTIBAND_CASES:
            for fault in ('source', 'alpha-band', 'rotation', 'second-band-count', 'mask-count', 'overview', 'physical-size', 'prior'):
                with self.subTest(case=case, fault=fault):
                    self.setUp()
                    def mutate(execution):
                        facts = execution.get('metadata', {}).get('lineage_facts', {})
                        if any('/'+case+'.cog.tif?' in output['locator'] for output in facts.get('outputs', [])):
                            artifact = execution['metadata']['result']['final_result']
                            if fault == 'source': facts['inputs'][0]['locator'] = 'different-source'
                            if fault == 'alpha-band': artifact['bands'][1]['color_interpretation'] = 'Alpha'
                            if fault == 'rotation': artifact['transform'][2] += .01
                        return execution
                    self.client.mutate = mutate
                    def physical(repo, env, action):
                        evidence = self.physical(repo, env, action)
                        if action == 'verify-'+case:
                            if fault == 'second-band-count': evidence['band_valid_pixels'][1] -= 1
                            if fault == 'mask-count': evidence['invalid_pixels'] += 1
                            if fault == 'overview': evidence['overview_sizes'] = [[42, 26]]
                            if fault == 'physical-size': evidence['size_bytes'] = 1024.
                            if fault == 'prior': evidence['preserved_sha256']['aggregate-max.cog.tif'] = 'changed'
                        return evidence
                    with self.assertRaises(m.SuiteError): self.run_scene(physical)

    def test_min_max_rejects_unmatched_sizes_non_integer_sizes_grid_and_physical_facts(self):
        for method in ('min','max'):
            name = 'aggregate-'+method
            for fault in ('declared-size','artifact-float','artifact-bool','native-size','native-float','all-zero','all-negative','item-size','item-float','item-bool','grid','levels','counts','source','prior'):
                with self.subTest(method=method, fault=fault):
                    self.setUp()
                    def mutate(execution):
                        outputs = execution.get('metadata', {}).get('lineage_facts', {}).get('outputs', [])
                        if any('/'+name+'.cog.tif?' in item['locator'] for item in outputs):
                            artifact = execution['metadata']['result']['final_result']
                            if fault in ('all-zero','all-negative'): artifact['size_bytes'] = 0 if fault == 'all-zero' else -1
                            if fault == 'declared-size': artifact['size_bytes'] = 1
                            if fault == 'artifact-float': artifact['size_bytes'] = 1024.
                            if fault == 'artifact-bool': artifact['size_bytes'] = True
                            if fault == 'grid': artifact['width'] = 85
                        return execution
                    self.client.mutate = mutate
                    target = next(item for item in self.client.targets.values() if item['full_name']=='raster-target/'+name+'.cog.tif')
                    if fault in ('all-zero','all-negative'): target['size_bytes'] = 0 if fault == 'all-zero' else -1
                    if fault == 'item-size': target['size_bytes'] = 1025
                    if fault == 'item-float': target['size_bytes'] = 1024.
                    if fault == 'item-bool': target['size_bytes'] = True
                    def physical(repo,env,action):
                        payload = self.physical(repo,env,action)
                        if action == 'verify-'+name:
                            if fault in ('all-zero','all-negative'): payload['size_bytes'] = 0 if fault == 'all-zero' else -1
                            if fault == 'native-size': payload['size_bytes'] = 1025
                            if fault == 'native-float': payload['size_bytes'] = 1024.
                            if fault == 'levels': payload['overview_sizes'] = [[42,26]]
                            if fault == 'counts': payload['band_valid_pixels'] = [4471]
                            if fault == 'source': payload['source_unchanged'] = False
                            if fault == 'prior': payload['preserved_sha256']['aggregate-sum.cog.tif'] = 'changed'
                        return payload
                    with self.assertRaises(m.SuiteError): self.run_scene(physical)

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
