import copy
import importlib
from pathlib import Path
import unittest
import tempfile

m = importlib.import_module('scripts.test.raster-workflow-online')


class Client:
    def __init__(self):
        self.calls = []
        self.executions = {}
        self.source = {'id': 10, 'item_type': 'object', 'full_name': 'raster-source/source.tif', 'fingerprint': 'source-fp'}
        self.target = {'id': 20, 'item_type': 'object', 'full_name': 'raster-target/result.cog.tif', 'fingerprint': 'target-fp',
                       'attributes': {'item': {'format': 'tiff'}, 'type_info': {'media': {'width': 256, 'height': 256}},
                                      'format_info': {'tiff': {'profile': 'cog', 'is_tiled': True}},
                                      'capabilities': {'spatial': {'srid': 4326, 'extent': [110, 17.76, 112.56, 20.32]}}}}
        self.context = {'principal': {'id': 7, 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                        'token': {'type': 'first_party_access_token'}, 'authorization': {'role_assignments': [
                            {'role_key': 'online.raster_workflow', 'permissions': sorted(m.PERMISSIONS)}]}}
        self.mutate = lambda execution: execution
        self.monitor_mismatch = False
        self.no_graph = False

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
        elif path == '/api/v1/meta/engines/1/items': result = [self.source]
        elif path == '/api/v1/meta/engines/2/items': result = [self.target]
        elif path == '/api/v1/meta/items/20': result = self.target
        elif method == 'POST' and path == '/api/v1/develop/executions':
            definition = body['content']['workflow_definition']
            mode = definition['tasks'][-1]['params']['write_mode']
            identifier = f'run-{len(self.executions) + 1}'
            status = 'failed' if identifier == 'run-2' else 'success'
            source = definition['tasks'][0]['params']['locator']
            target = 'addp://engine/2/path/raster-target/result.cog.tif?type=object'
            metadata = {} if status == 'failed' else {
                'outputs': {'save': {'resource': {'locator': target, 'type': 'object', 'write_mode': mode}}},
                'lineage_facts': {'schema_version': 'addp.lineage-facts/v1', 'inputs': [{'locator': source}],
                                  'outputs': [{'locator': target, 'write_mode': mode}],
                                  'operations': [{'kind': 'derive', 'operator': 'develop', 'input_ports': ['input'], 'output_ports': ['output']}]},
                'result': {'final_result': {'artifact_type': 'raster', 'format': 'tiff', 'profile': 'cog',
                                          'width': 256, 'height': 256, 'band_count': 1, 'source_crs': 'EPSG:4326',
                                          'extent_srid': 4326, 'transform': [110, .01, 0, 20.32, 0, -.01],
                                          'bands': [{'dtype': 'Float64', 'nodata_is_nan': True}]},
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
            result = {'truncated': self.no_graph, 'edges': [{'source': {'item_id': 10}, 'target': {'item_id': 20},
                      'status': 'active', 'relation_kind': 'derive', 'evidence': {'execution_id': 'run-3'}}]}
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
        return {'sha256': 'new' if action == 'verify-replace' else 'original', 'cog_valid': True,
                'source_unchanged': True, 'valid_pixels': 65535}

    def run_scene(self, physical=None):
        return m.run_scenario(Path('.'), self.env, self.client, physical or self.physical,
                              lambda repo, env, evidence: evidence,
                              lambda repo, env, identifier, timeout: {'execution_id': identifier,
                                  'cause': 'target_already_exists', 'source': 'develop_runtime_log'})

    def test_complete_chain_only_uses_source_manual_scan_and_owner_automatic_target_scan(self):
        report = self.run_scene()
        self.assertEqual([item['status'] for item in report['executions']], ['success', 'failed', 'success'])
        self.assertEqual(self.physical_actions, ['verify-create', 'verify-create', 'verify-replace'])
        self.assertEqual(report['automatic_target_scan_execution_id'], 'auto-run-3')
        manual = [body for method, path, body in self.client.calls if method == 'POST' and 'scan/run' in path]
        self.assertEqual(len(manual), 1)
        self.assertEqual(manual[0]['engine_id'], 1)
        self.assertFalse(any('collect' in path for _, path, _ in self.client.calls))

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
            m.validate_success(professional, '', '', 'create')

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

    def test_rejects_incorrect_target_metadata(self):
        self.client.target['attributes']['type_info']['media']['width'] = 1
        with self.assertRaises(m.SuiteError): self.run_scene()


if __name__ == '__main__': unittest.main()
