import copy
import importlib
import json
from pathlib import Path
import tempfile
import time
from types import SimpleNamespace
import unittest

m = importlib.import_module('scripts.test.raster-disk-pressure-online')
ENV = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_DISK_PRESSURE_NFS_ENGINE_ID': '3',
       'ADDP_ONLINE_TEST_TIMEOUT_SECONDS': '600', 'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN': 'isolated-test-preparer'}


class Client:
    fault = None

    def __init__(self, log_path):
        self.log_path = log_path
        self.workflows = {}
        self.executions = {}; self.targets = {}; self.scans = []; self.headers = []; self.pressure_phase = None; self.parents = {}; self.proofs = {}
        self.source = {'id': 1, 'item_type': 'file', 'full_name': m.fixture.SOURCE, 'fingerprint': 'source-fingerprint', 'size_bytes': 1200}

    def edge(self, name, item):
        return {'source': {'item_id': 1}, 'target': {'item_id': item['id']}, 'status': 'active',
                'relation_kind': 'derive', 'evidence': {'execution_id': self.proofs[name]}}

    def request(self, method, path, expected, body=None, headers=None):
        self.headers.append((method, path, headers or {}))
        if path.endswith('/auth/context'):
            data = {'principal': {'id': 33, 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                    'token': {'type': 'first_party_access_token'}, 'authorization': {'role_assignments': [
                        {'role_key': 'raster-consumer', 'permissions': sorted(m.raster.EDITOR_PERMISSIONS)}]}}
        elif path.endswith('/workflow-engines'): data = [{'id': 7, 'engine_type': 'geopython_workflow', 'connection_status': 'online'}]
        elif method == 'POST' and path.endswith('/develop/executions'):
            tasks = body['content']['workflow_definition']['tasks']; name = tasks[-1]['params']['target_name']; identifier = f'exec-{len(self.executions)+1}-{name}'
            phase = self.pressure_phase
            if phase:
                working = 2*(16*(256**2*9+64))+8*1024**2
                required = 32*1024**2 if phase == 'distance' else 2*(2*(4096**2*9+64)+8*1024**2)
                language = headers['Accept-Language']
                stage = 'distance transform' if phase == 'distance' else 'raster encoding'
                if self.fault in ('phase', 'phase-and-cleanup'): stage = 'nibble output'
                task = 'compute' if phase == 'distance' else 'save'
                if self.fault == 'task': task = 'source'
                text = (f'栅格临时磁盘空间不足（阶段：{stage}）：' if language == 'zh-cn' or self.fault == 'language'
                        else f'Insufficient raster temporary disk space during {stage}: ')
                counts = f'required_bytes={required}, available_bytes={m.fixture.PRESSURE_FREE[phase]}, pending_bytes={working if phase == "distance" else 0}, safety_bytes={m.fixture.SAFETY}'
                code = 'NO_SPACE_LEFT' if self.fault == 'runtime-code' else 'EXECUTION_FAILED'
                message = (f'❌ [DevExecutor] 工作流引擎调用失败: execution_id={identifier} err=工作流运行时执行失败: '
                           f'{code}: 任务 {task} 执行失败: {text}{counts}')
                with self.log_path.open('a', encoding='utf-8') as target:
                    target.write(json.dumps({'module_name': 'develop', 'role': 'backend', 'message': message})+'\n')
                data = {'status': 'failed', 'execution_id': identifier, 'module': 'develop',
                        'error_details': {'category': 'resource_exhausted' if language == 'zh-cn' else 'execution_failed'},
                        'execution_config': {'display_language': language},
                        'outputs': {'save': {}} if self.fault == 'publish' else {}, 'metadata': {}}
                if self.fault == 'scan': self.scans.append('bad-scan')
            else:
                target = 'addp://engine/3/path/outputs/'+name+'?type=file'; scan = 'scan-'+identifier; self.parents[scan] = identifier; self.proofs[name] = identifier
                self.scans.append(scan); self.targets[name] = {'id': 10+len(self.targets), 'item_type': 'file', 'full_name': 'outputs/'+name,
                    'size_bytes': 2000, 'fingerprint': name, 'scanned_at': '2026-10-21T00:00:00Z', 'attributes': {
                        'item': {'format': 'tiff'}, 'type_info': {'media': {'width': 1024, 'height': 1024}},
                        'format_info': {'tiff': {'is_cloud_optimized': True, 'cog_check_level': 'heuristic', 'profile': 'cog', 'is_tiled': True, 'has_overviews': False}},
                        'capabilities': {'spatial': {'srid': 3857, 'extent': m.fixture.expectation()['extent']}}}}
                facts = {'schema_version': 'addp.lineage-facts/v1', 'inputs': [{'locator': tasks[0]['params']['locator']}],
                         'outputs': [{'locator': target, 'write_mode': 'create'}]}
                artifact = {k: v for k,v in m.fixture.expectation().items() if k in {'width', 'height', 'band_count', 'source_crs', 'extent_srid', 'transform'}}
                artifact.update(artifact_type='raster', profile='cog', format='tiff', size_bytes=2000,
                                bands=[{'dtype': 'Float64', 'nodata_is_nan': True, 'nodata': None}])
                data = {'status': 'success', 'outputs': {'save': {'resource': {'locator': target, 'type': 'file', 'write_mode': 'create'}}},
                        'metadata': {'lineage_facts': facts, 'result': {'final_result': artifact,
                            'meta_scan_runs': [{'execution_id': scan, 'target_locator': target, 'status': 'submitted'}]}}}
                if self.fault == 'size': artifact['size_bytes'] = 2001
            self.executions[identifier] = data
            self.workflows[identifier] = (body['content']['workflow_definition'], headers['Accept-Language'])
            data = {'execution_id': identifier}
        elif '/develop/executions/' in path:
            data = self.executions[path.rsplit('/', 1)[-1]]
            if self.fault == 'reread' and headers and data['status'] == 'failed':
                data = {**data, 'error_details': {'category': 'unknown'}}
        elif '/meta/executions/' in path:
            scan = path.rsplit('/', 1)[-1]; name = scan.removeprefix('scan-')
            data = {'status': 'success', 'execution_id': scan, 'module': 'meta', 'task_type': 'scan', 'parent_execution_id': self.parents[scan],
                    'tenant_id': 2, 'triggered_by': 34 if self.fault == 'actor' else 33, 'scan_context': {'engine_id': 3}}
        elif '/monitor/executions/' in path: data = {'module': 'develop', **self.executions[path.rsplit('/', 1)[-1]]}
        elif path.endswith('/engines/3/items'): data = [self.source, *self.targets.values()]
        elif '/meta/items/' in path:
            identifier = int(path.rsplit('/', 1)[-1]); data = self.source if identifier == 1 else next(v for v in self.targets.values() if v['id'] == identifier)
        elif '/meta/lineage/graph?' in path:
            data = {'edges': [self.edge(name, item) for name, item in self.targets.items()]}
        elif '/meta/scan/runs?' in path: data = {'total': len(self.scans), 'items': [{'execution_id': s} for s in self.scans]}
        else: raise AssertionError((method, path))
        return SimpleNamespace(payload=copy.deepcopy(data))


class Initializer:
    def __init__(self, client):
        self.business = client
        self.targets = []
        self.scans = []

    def request(self, method, path, expected, body=None, headers=None):
        if method == 'POST' and path == '/api/v1/meta/scan/run/manual':
            assert body['engine_id'] == 3 and not self.scans
            self.targets = body['targets']
            self.scans.append('source-scan')
            return SimpleNamespace(payload={'execution_id': 'source-scan'})
        if method == 'GET' and path == '/api/v1/meta/executions/source-scan':
            return SimpleNamespace(payload={'execution_id': 'source-scan', 'module': 'meta', 'task_type': 'scan',
                'status': 'success', 'tenant_id': 2, 'triggered_by': 33 if self.business.fault == 'source-actor' else 81,
                'scan_context': {'engine_id': 3}})
        if method == 'GET' and path.startswith('/api/v1/meta/scan/runs?'):
            return SimpleNamespace(payload={'total': len(self.scans), 'items': [{'execution_id': s} for s in self.scans]})
        raise AssertionError('preparer attempted a business request: '+path)


class Metadata:
    def __init__(self, case, env, client, engine, identity, scan, remaining, *, grant_remaining):
        self.business = case.client
        self.actions = case.actions
        self.path = Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'disk-pressure-metadata.json'
        self.report = {'issued': [], 'cleanup': 'pending'}
        self.pending = {}
        self.executions = []
        assert engine == 3 and identity['principal_id'] == '33' and scan == 'source-scan'
        assert 0 < remaining() < grant_remaining() <= 600

    def find(self, name, binding=None):
        if binding is None:
            assert name == m.fixture.SOURCE
            item = self.business.source
        else:
            assert name.removeprefix('outputs/') in m.fixture.TARGETS
            basename = name.removeprefix('outputs/')
            item = self.business.targets[basename]
            assert binding['execution'] == self.business.parents[binding['scan']] == self.business.proofs[basename]
            assert binding['locator'] == f'addp://engine/3/path/{name}?type=file'
        self.pending[item['id']] = item
        self.report['issued'].append(name)
        return item

    def snapshot(self, identifier):
        return self.pending[identifier]

    def execution(self, identifier, workflow, runtime, language, status):
        assert runtime == 7 and self.business.executions[identifier]['status'] == status
        assert self.business.workflows[identifier] == (workflow, language)
        self.executions.append(identifier)

    def cleanup(self):
        self.actions.append('revoke-and-check-hidden')
        if self.business.fault in ('cleanup', 'phase-and-cleanup'):
            raise m.SuiteError('revocation observation differs')
        self.pending.clear()
        self.report['cleanup'] = {'grants_revoked': True, 'metadata_hidden': True}


class DiskPressureOnlineTest(unittest.TestCase):
    def operate(self, repository, env, action, *names, timeout):
        self.assertNotIn('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN', env)
        self.assertGreater(timeout, 0)
        self.actions.append(action)
        if action == 'runtime-identity':
            self.snapshots += 1
            return {'container_id': str(self.snapshots) if self.restart else 'same', 'image_id': 'same', 'server_processes': [[1, '1'], [2, '2']]}
        if action in ('distance', 'encoding', 'release'):
            self.client.pressure_phase = action if action != 'release' else None
            return {'phase': action, 'available_bytes': m.fixture.PRESSURE_FREE.get(action, 4*m.fixture.GIB), 'filesystem_bytes': 4*m.fixture.GIB}
        return {'runtime_workspaces_removed': True, 'targets': {name: {'size_bytes': 2000, 'sha256': name,
            'cog_valid': True, 'has_overviews': False, 'full_pixel_and_mask_verification': True} for name in names}}

    def setUp(self): self.actions = []; self.snapshots = 0; self.restart = False

    def new_client(self):
        directory = Path(self.enterContext(tempfile.TemporaryDirectory()))
        log = directory/'develop'/'test-instance'/'events.jsonl'
        log.parent.mkdir(parents=True)
        client = Client(log)
        self.initializer = Initializer(client)
        return client, {**ENV, 'ADDP_RUNTIME_LOG_ROOT': str(directory), 'ADDP_ONLINE_ARTIFACT_DIR': str(directory)}

    def source_preparer(self, env, client, identity, remaining):
        self.assertIs(client.client, self.client)
        self.assertEqual(identity['principal_id'], '33')
        return self.initializer, {'principal_id': '81', 'tenant_id': '2'}

    def metadata_factory(self, *args, **kwargs):
        self.metadata = Metadata(self, *args, **kwargs)
        return self.metadata

    def run_pressure(self, env, *, budget=600):
        return m.run_scenario(Path('.'), env, self.client, self.operate,
                              hard_deadline=time.monotonic()+budget,
                              source_preparer=self.source_preparer, metadata_factory=self.metadata_factory)

    def test_eight_platform_executions_keep_identity_strict_scans_lineage_and_submitted_language(self):
        client, env = self.new_client(); self.client = client; result = self.run_pressure(env)
        self.assertEqual(result['result'], 'passed'); self.assertEqual(len(result['cases']), 4); self.assertEqual(len(result['failures']), 4)
        self.assertEqual(len(client.executions), 8); self.assertEqual(len(client.scans), 4)
        self.assertEqual(self.metadata.executions, list(client.executions))
        self.assertEqual(self.initializer.targets, ['addp://engine/3/path/classes.tif?type=file'])
        self.assertEqual(self.initializer.scans, ['source-scan'])
        self.assertEqual(result['source_scan_identity']['triggered_by'], 81)
        self.assertEqual(self.metadata.report['issued'], [m.fixture.SOURCE, *('outputs/'+name for name in m.fixture.TARGETS)])
        self.assertFalse(self.metadata.pending)
        self.assertEqual(result['metadata_cleanup'], {'grants_revoked': True, 'metadata_hidden': True})
        self.assertEqual({x['language'] for x in result['failures']}, {'zh-cn', 'en'})
        self.assertEqual({x['source'] for x in result['failures']}, {'develop_runtime_log'})
        self.assertEqual(self.actions[-1], 'release')
        self.assertEqual(self.snapshots, 9)

    def test_public_failure_category_matches_the_frozen_language(self):
        for language, expected, other in (
            ('zh-cn', 'resource_exhausted', 'execution_failed'),
            ('en', 'execution_failed', 'resource_exhausted'),
        ):
            execution = {
                'execution_id': 'exec-1', 'module': 'develop', 'status': 'failed', 'metadata': {},
                'error_details': {'category': expected}, 'execution_config': {'display_language': language},
            }
            with self.subTest(language=language):
                m.verify_failed_observation(execution, 'exec-1', language)
                for category in (other, 'unknown', None):
                    wrong = {**execution, 'error_details': {'category': category}}
                    with self.assertRaises(m.SuiteError):
                        m.verify_failed_observation(wrong, 'exec-1', language)

    def test_wrong_phase_language_publication_scan_actor_size_and_restart_are_not_accepted(self):
        for fault in ('phase', 'task', 'language', 'runtime-code', 'reread', 'publish', 'scan', 'actor', 'size', 'restart', 'source-actor', 'cleanup'):
            self.setUp(); client, env = self.new_client(); client.fault = fault; self.client = client; self.restart = fault == 'restart'
            with self.subTest(fault=fault), self.assertRaises(m.SuiteError):
                self.run_pressure(env)
            self.assertEqual(self.actions[-1], 'release')
            if fault != 'source-actor':
                self.assertIn('revoke-and-check-hidden', self.actions)

    def test_primary_failure_is_preserved_when_revocation_also_fails(self):
        client, env = self.new_client()
        self.client = client
        client.fault = 'phase-and-cleanup'
        with self.assertRaisesRegex(m.SuiteError, 'Runtime diagnostic.*metadata Grant cleanup failed'):
            self.run_pressure(env)
        self.assertEqual(self.actions[-2:], ['revoke-and-check-hidden', 'release'])

    def test_gate_deadline_is_required_and_cannot_expand(self):
        client, env = self.new_client()
        self.client = client
        for budget in (float('nan'), -1, 601):
            with self.subTest(budget=budget), self.assertRaisesRegex(m.SuiteError, '600-second'):
                self.run_pressure(env, budget=budget)
        self.assertFalse(self.actions)

    def test_no_physical_or_budget_assertion_is_replaced_by_successful_http(self):
        working = 2*(16*(256**2*9+64))+8*1024**2
        message = ('任务 compute 执行失败: 栅格临时磁盘空间不足（阶段：distance transform）：'
                   f'required_bytes={32*1024**2}, available_bytes={m.fixture.PRESSURE_FREE["distance"]}, '
                   f'pending_bytes={working}, safety_bytes={m.fixture.SAFETY}')
        observation = {'available_bytes': m.fixture.PRESSURE_FREE['distance']}
        m.verify_failure_message(message, 'distance', observation, 'zh-cn')
        for old, new in [('required_bytes=33554432', 'required_bytes=1'), ('pending_bytes='+str(working), 'pending_bytes=0'),
                         ('safety_bytes='+str(m.fixture.SAFETY), 'safety_bytes=1'), ('distance transform', 'raster encoding'),
                         ('栅格临时磁盘空间不足', 'No space left on device'), ('required_bytes=', '/raster-pressure-tmp required_bytes=')]:
            with self.subTest(change=old), self.assertRaises(m.SuiteError):
                m.verify_failure_message(message.replace(old, new), 'distance', observation, 'zh-cn')

    def test_exact_definitions_reject_unknown_operator_stage_and_keep_canonical_save(self):
        for op in ('nibble', 'proximity'):
            self.assertEqual(m.definition('source', 'parent', op, 'encoding'), m.definition('source', 'parent', op, 'recovered'))
            for stage in ('baseline', 'distance', 'encoding', 'recovered'):
                d = m.definition('source', 'parent', op, stage)
                self.assertEqual(d['tasks'][-1]['params']['blocksize'], 4096)
                self.assertEqual(d['tasks'][-1]['params']['write_mode'], 'create')
                self.assertEqual(d['tasks'][1]['params']['band'], 2)
        with self.assertRaises(m.SuiteError): m.definition('s', 'p', 'other', 'baseline')


if __name__ == '__main__': unittest.main()
