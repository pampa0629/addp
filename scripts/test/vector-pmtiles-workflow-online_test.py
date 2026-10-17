import copy
import importlib
import unittest

scene = importlib.import_module('scripts.test.vector-pmtiles-workflow-online')


class PMTilesWorkflowTest(unittest.TestCase):
    def success(self):
        source, target = 'addp://engine/11/path/pmtiles-source/points.geojson?type=object&item_id=7', 'addp://engine/12/path/pmtiles-target/result.pmtiles?type=object'
        native = {'archive': {'header_hash': 'abc'}, 'targets': {'result.pmtiles': {'size_bytes': 718}}}
        execution = {'status': 'success', 'outputs': {'convert': {'resource': {'locator': target, 'type': 'object', 'write_mode': 'create'}}}, 'metadata': {
            'lineage_facts': {'schema_version': 'addp.lineage-facts/v1', 'inputs': [{'locator': source}], 'outputs': [{'locator': target, 'write_mode': 'create'}]},
            'result': {'final_result': {'archive_format': 'pmtiles', 'spec_version': 3, 'tile_format': 'mvt', 'tile_compression': 'gzip', 'min_zoom': 0, 'max_zoom': 5, 'actual_max_zoom': 5, 'extent_srid': 4326, 'header_hash': 'abc', 'archive_size_bytes': 718},
                       'meta_scan_runs': [{'status': 'submitted', 'target_locator': target, 'execution_id': 'automatic-scan'}]}}}
        return source, target, native, execution

    def test_public_parameters_are_canonical_and_geometry_only_is_explicit(self):
        for polygons, mode in [(False,'create'), (True,'replace')]:
            definition = scene.definition('public-locator', 12, mode, polygons)
            params = definition['tasks'][0]['params']
            self.assertEqual(params['min_zoom'], 0)
            self.assertEqual(params['max_zoom'], 5)
            self.assertEqual(params['attributes'], ['name','rank'] if polygons else [])
            self.assertEqual(params['write_mode'], mode)
            self.assertFalse({'access_plan','tile','options'} & set(params))

    def test_success_binds_exact_source_target_scan_header_size_and_zoom(self):
        source,target,native,execution = self.success()
        self.assertEqual(scene.validate_success(execution, source,target,'create',native)[1], 'automatic-scan')
        for path, value in [(('outputs','convert','resource','write_mode'), 'replace'),
                            (('metadata','lineage_facts','inputs'), []),
                            (('metadata','result','meta_scan_runs'), []),
                            (('metadata','result','final_result','min_zoom'), 6),
                            (('metadata','result','final_result','archive_size_bytes'), 719),
                            (('metadata','result','final_result','header_hash'), 'wrong')]:
            mutated = copy.deepcopy(execution)
            cursor = mutated
            for part in path[:-1]: cursor = cursor[part]
            cursor[path[-1]] = value
            with self.subTest(path=path), self.assertRaises(scene.SuiteError):
                scene.validate_success(mutated,source,target,'create',native)

    def test_conflict_rejects_every_side_effect_and_requires_controlled_diagnostic(self):
        execution = {'status':'failed','metadata':{},'error_details': {'code':'develop.workflow.target_exists','diagnostic': {'stage':'result','reason':'target_exists'}}}
        snapshots = [{'sha256':'original'}, {'sha256':'original'}, {'scanned_at':'before'}, {'scanned_at':'before'}, ['scan'], ['scan'], {'edges':['original']}, {'edges':['original']}]
        scene.validate_conflict(execution, *snapshots)
        for index in (1,3,5,7):
            changed = copy.deepcopy(snapshots)
            changed[index] = {'unexpected':'mutation'}
            with self.subTest(index=index), self.assertRaises(scene.SuiteError): scene.validate_conflict(execution, *changed)
        for metadata in [{'lineage_facts': {'inputs':[1]}}, {'result':{'meta_scan_runs':[{}]}}, {'result':{'produced_targets':[{}]}}]:
            failed = copy.deepcopy(execution); failed['metadata'] = metadata
            with self.assertRaises(scene.SuiteError): scene.validate_conflict(failed,*snapshots)
        failed = copy.deepcopy(execution); failed['error_details'] = {'category':'execution_failed'}
        with self.assertRaises(scene.SuiteError): scene.validate_conflict(failed,*snapshots)

    def test_meta_facts_bind_the_current_physical_archive(self):
        native = {'targets': {'result.pmtiles': {'size_bytes': 718}}, 'archive': {'header_hash': 'abc'}}
        item = {'size_bytes':718, 'scanned_at':'now', 'attributes':{'item':{'format':'pmtiles'}, 'format_info':{'pmtiles':{
            'spec_version':3, 'tile_type':'mvt', 'tile_compression':'gzip', 'min_zoom':0, 'max_zoom':5, 'header_hash':'abc'}}}}
        scene.validate_item(item,native)
        item['attributes']['format_info']['pmtiles']['header_hash'] = 'old'
        with self.assertRaises(scene.SuiteError): scene.validate_item(item,native)

    def test_complete_scene_has_one_source_scan_no_manual_target_scan_and_stable_replacement(self):
        import json
        import tempfile
        from pathlib import Path
        from types import SimpleNamespace
        test = self
        baseline = {'sources': {'points.geojson': {'sha256':'source'}, 'areas.shp': {'sha256':'shp'}}, 'targets': {'keep.txt': {'sha256':'keep'}}}
        def native(polygons):
            return {'sources':baseline['sources'], 'targets':{**baseline['targets'], 'result.pmtiles':{'size_bytes':718, 'sha256':'after' if polygons else 'before'}},
                    'archive':{'header_hash':'abc', 'layer':'areas_after' if polygons else 'points_before', 'feature_count':50 if polygons else 2, 'tile_sha256':'tile'}}
        class Client:
            count = 0
            manual_scans = []
            def target(self):
                return {'id':9, 'fingerprint':'stable', 'full_name':'pmtiles-target/result.pmtiles', 'item_type':'object', 'size_bytes':718,
                    'scanned_at':'after' if self.count == 3 else 'before', 'attributes':{'item':{'format':'pmtiles'}, 'format_info':{'pmtiles':{
                        'spec_version':3,'tile_type':'mvt','tile_compression':'gzip','min_zoom':0,'max_zoom':5,'header_hash':'abc'}}}}
            def request(self, method, path, expected, body=None):
                result = None
                if path.endswith('/auth/context'):
                    result = {'principal':{'id':'5','type':'user'},'context':{'type':'tenant','tenant_id':'42'},
                        'token':{'type':'first_party_access_token'},'authorization':{'role_assignments':[{'role_key':'online.pmtiles','permissions':sorted(scene.PERMISSIONS)}]}}
                elif path.endswith('/workflow-engines'):
                    result = [{'id':20,'engine_type':'geopython_workflow','connection_status':'online'}]
                elif path.endswith('/scan/run/manual'):
                    self.manual_scans.append(body['engine_id']); result = {'execution_id':'source-scan'}
                elif path.startswith('/api/v1/meta/executions/'):
                    result = {'status':'success'}
                elif path == '/api/v1/develop/executions' and method == 'POST':
                    self.count += 1; result = {'execution_id':str(self.count)}
                elif path.startswith('/api/v1/develop/executions/') or path.startswith('/api/v1/monitor/executions/by-execution-id/'):
                    identifier = path.rsplit('/',1)[-1]
                    if identifier == '2':
                        result = {'status':'failed','metadata':{},'error_details':{'code':'develop.workflow.target_exists','diagnostic':{'stage':'result','reason':'target_exists'}}}
                    else:
                        source,target,_,result = test.success()
                        mode = 'replace' if identifier == '3' else 'create'
                        source = f'addp://engine/11/path/pmtiles-source/{"areas.shp?type=object&item_id=8" if identifier == "3" else "points.geojson?type=object&item_id=7"}'
                        result['outputs']['convert']['resource']['write_mode'] = mode
                        result['metadata']['lineage_facts']['inputs'][0]['locator'] = source
                        result['metadata']['lineage_facts']['outputs'][0]['write_mode'] = mode
                        result['metadata']['result']['meta_scan_runs'][0]['execution_id'] = 'scan-'+identifier
                        result['module'] = 'develop'
                elif path == '/api/v1/meta/engines/11/items':
                    result = [{'id':index,'fingerprint':str(index),'item_type':'object','full_name':'pmtiles-source/'+name,'size_bytes':100} for index,name in [(7,'points.geojson'),(8,'areas.shp')]]
                elif path == '/api/v1/meta/engines/12/items': result = [self.target()]
                elif path == '/api/v1/meta/items/9': result = self.target()
                elif path.startswith('/api/v1/meta/lineage/graph?'):
                    result = {'truncated':False, 'nodes':[{'item_id':9},{'item_id':8 if self.count == 3 else 7}], 'edges':[
                        {'source':{'item_id':8 if self.count == 3 else 7},'target':{'item_id':9},'evidence':{'execution_id':'3' if self.count == 3 else '1'},'status':'active','relation_kind':'derive'}]}
                elif path.startswith('/api/v1/meta/scan/runs?'):
                    result = {'total':2,'items':[{'execution_id':'source-scan'},{'execution_id':'scan-1'}]}
                else: raise AssertionError(f'unexpected HTTP operation {method} {path}')
                return SimpleNamespace(payload=result)
        with tempfile.TemporaryDirectory() as temporary:
            (Path(temporary)/'pmtiles-fixture-baseline.json').write_text(json.dumps(baseline))
            env = {'ADDP_ONLINE_TEST_TENANT_ID':'42','ADDP_ONLINE_PMTILES_SOURCE_ENGINE_ID':'11','ADDP_ONLINE_PMTILES_TARGET_ENGINE_ID':'12',
                'ADDP_ONLINE_TEST_TIMEOUT_SECONDS':'1','ADDP_ONLINE_ARTIFACT_DIR':temporary,'ADDP_ONLINE_TEST_RUN_ID':'pmtiles-unit'}
            client = Client()
            report = scene.run_scenario(Path(temporary), env, client,
                physical_runner=lambda repository,env,action: native(action == 'verify-replace'),
                browser_runner=lambda repository,env,evidence: evidence)
            self.assertEqual(client.manual_scans, [11])
            self.assertEqual(client.count, 3)
            self.assertEqual([phase['phase'] for phase in report['phases']], ['create','replace'])
            self.assertTrue(report['conflict']['scan_unchanged'])
            self.assertEqual([phase['target']['id'] for phase in report['phases']], [9,9])

    def test_suite_identity_includes_only_actual_scene_permissions(self):
        self.assertEqual(scene.PERMISSIONS - scene.raster.PERMISSIONS, {'manager.data_item.read','manager.derived_artifact.read'})
        self.assertNotIn('develop.task.create', scene.PERMISSIONS)
        self.assertNotIn('manager.derived_artifact.create', scene.PERMISSIONS)


if __name__ == '__main__': unittest.main()
