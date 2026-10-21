import copy
import importlib
from pathlib import Path
from types import SimpleNamespace
import unittest

m = importlib.import_module('scripts.test.raster-disk-pressure-online')
ENV = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_DISK_PRESSURE_NFS_ENGINE_ID': '3'}


class Client:
    fault = None

    def __init__(self):
        self.executions = {}; self.targets = {}; self.scans = ['source-scan']; self.headers = []; self.pressure_phase = None; self.parents = {}; self.proofs = {}
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
        elif path.endswith('/scan/run/manual'): data = {'execution_id': 'source-scan'}
        elif path.endswith('/workflow-engines'): data = [{'id': 7, 'engine_type': 'geopython_workflow', 'connection_status': 'online'}]
        elif method == 'POST' and path.endswith('/develop/executions'):
            tasks = body['content']['workflow_definition']['tasks']; name = tasks[-1]['params']['target_name']; identifier = f'exec-{len(self.executions)+1}-{name}'
            phase = self.pressure_phase
            if phase:
                working = 2*(16*(256**2*9+64))+8*1024**2
                required = 32*1024**2 if phase == 'distance' else 2*(2*(4096**2*9+64)+8*1024**2)
                text = '栅格临时磁盘空间不足' if (headers or {}).get('Accept-Language') == 'zh-cn' or self.fault == 'language' else 'Insufficient raster temporary disk space'
                stage = 'distance transform' if phase == 'distance' else 'raster encoding'
                if self.fault == 'phase': stage = 'nibble output'
                message = f'{text}: {stage}: required_bytes={required}, available_bytes={m.fixture.PRESSURE_FREE[phase]}, pending_bytes={working if phase == "distance" else 0}, safety_bytes={m.fixture.SAFETY}'
                data = {'status': 'failed', 'error_msg': message, 'outputs': {'save': {}} if self.fault == 'publish' else {}, 'metadata': {}}
                if self.fault == 'scan': self.scans.append('bad-scan')
            else:
                target = 'addp://engine/3/path/outputs/'+name+'?type=file'; scan = 'scan-'+identifier; self.parents[scan] = identifier; self.proofs[name] = identifier
                self.scans.append(scan); self.targets[name] = {'id': 10+len(self.targets), 'item_type': 'file', 'full_name': 'outputs/'+name,
                    'size_bytes': 2000, 'fingerprint': name, 'scanned_at': '2026-10-21T00:00:00Z', 'attributes': {
                        'item': {'format': 'tiff'}, 'type_info': {'media': {'width': 1024, 'height': 1024}},
                        'format_info': {'tiff': {'profile': 'geotiff', 'is_tiled': True, 'has_overviews': False}},
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
            data = {'execution_id': identifier}
        elif '/develop/executions/' in path: data = self.executions[path.rsplit('/', 1)[-1]]
        elif '/meta/executions/' in path:
            scan = path.rsplit('/', 1)[-1]; name = scan.removeprefix('scan-')
            data = {'status': 'success', 'execution_id': scan, 'module': 'meta', 'task_type': 'scan', 'parent_execution_id': self.parents.get(scan, 'source-exec'),
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


class DiskPressureOnlineTest(unittest.TestCase):
    def operate(self, repository, env, action, *names):
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

    def test_eight_platform_executions_keep_identity_strict_scans_lineage_and_submitted_language(self):
        client = Client(); self.client = client; result = m.run_scenario(Path('.'), ENV, client, self.operate)
        self.assertEqual(result['result'], 'passed'); self.assertEqual(len(result['cases']), 4); self.assertEqual(len(result['failures']), 4)
        self.assertEqual(len(client.executions), 8); self.assertEqual(len(client.scans), 5)
        self.assertEqual({x['language'] for x in result['failures']}, {'zh-cn', 'en'})
        self.assertEqual(self.actions[-1], 'release')
        self.assertEqual(self.snapshots, 9)

    def test_wrong_phase_language_publication_scan_actor_size_and_restart_are_not_accepted(self):
        for fault in ('phase', 'language', 'publish', 'scan', 'actor', 'size', 'restart'):
            self.setUp(); client = Client(); client.fault = fault; self.client = client; self.restart = fault == 'restart'
            with self.subTest(fault=fault), self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'), ENV, client, self.operate)
            self.assertEqual(self.actions[-1], 'release')

    def test_no_physical_or_budget_assertion_is_replaced_by_successful_http(self):
        working = 2*(16*(256**2*9+64))+8*1024**2
        message = f'栅格临时磁盘空间不足 distance transform required_bytes={32*1024**2}, available_bytes={m.fixture.PRESSURE_FREE["distance"]}, pending_bytes={working}, safety_bytes={m.fixture.SAFETY}'
        good = {'status': 'failed', 'error_msg': message, 'metadata': {}, 'outputs': {}}
        observation = {'available_bytes': m.fixture.PRESSURE_FREE['distance']}
        m.verify_failure(good, 'distance', observation, 'zh-cn')
        for old, new in [('required_bytes=33554432', 'required_bytes=1'), ('pending_bytes='+str(working), 'pending_bytes=0'),
                         ('safety_bytes='+str(m.fixture.SAFETY), 'safety_bytes=1'), ('distance transform', 'raster encoding')]:
            value = copy.deepcopy(good); value['error_msg'] = message.replace(old, new)
            with self.subTest(change=old), self.assertRaises(m.SuiteError): m.verify_failure(value, 'distance', observation, 'zh-cn')

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
