import copy
import importlib
import json
import tempfile
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch

m = importlib.import_module('scripts.test.vector-topology-persistence-online')
base = importlib.import_module('scripts.test.raster-contour-persistence-online_test')


def execution():
    outputs = {key: f'addp://engine/3/path/outputs/{name}?type=file' for key, name in zip(m.fixture.PORTS, m.fixture.TARGETS)}
    inputs = [{'port': 'input.'+node, 'locator': f'addp://engine/3/path/{name}?type=file&item_id={identifier}'}
              for identifier, (node, name) in enumerate(zip(('load', 'load_invalid', 'snap_input', 'snap_reference', 'overlap_input', 'gap_input', 'gap_coverage', 'dangling_input'), m.fixture.SOURCES), 10)]
    facts = {'schema_version': 'addp.lineage-facts/v1', 'inputs': sorted(inputs, key=lambda entry: entry['port']), 'outputs': [{'locator': uri, 'write_mode': 'create'} for uri in outputs.values()]}
    return {'status': 'success', 'outputs': {'save_'+key: {'resource': {'locator': uri, 'type': 'file', 'write_mode': 'create'}} for key, uri in outputs.items()},
        'metadata': {'lineage_facts': facts, 'result': {'meta_scan_runs': [{'status': 'submitted', 'target_locator': uri, 'execution_id': 'scan-'+key} for key, uri in outputs.items()]}}}


class Client(base.Client):
    def __init__(self):
        super().__init__()
        self.source.update(full_name='source.geojson', size_bytes=500)
        for identifier, name in enumerate(m.fixture.TARGETS, 20):
            self.targets[identifier] = {'id': identifier, 'fingerprint': name, 'item_type': 'file', 'full_name': 'outputs/'+name,
                'size_bytes': 1000, 'scanned_at': '2026-10-18T00:00:00Z', 'attributes': {'item': {'format': 'geojson'}}}
        self.invalid_source = {**self.source, 'id': 11, 'full_name': 'invalid.geojson', 'fingerprint': 'invalid'}
        self.snap_sources = {identifier: {**self.source, 'id': identifier, 'full_name': name, 'fingerprint': name} for identifier, name in enumerate(m.fixture.SOURCES[2:],12)}
        for identifier,name in ((23,'snapped.gpkg'),(24,'overlaps.gpkg'),(25,'gaps.gpkg'),(26,'dangling.gpkg')):
            self.targets[identifier] = base.item(name,identifier); self.targets[identifier]['size_bytes'] = 1000
        self.permissions = sorted(m.PERMISSIONS)
        self.executions['execution'] = execution()
        self.scan_tenant = 2

    def request(self, method, path, expected, body=None):
        if path.endswith('/auth/context'):
            data = {'principal': {'id': 33, 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'}, 'token': {'type': 'first_party_access_token'},
                'authorization': {'role_assignments': [{'role_key': 'topology-consumer', 'permissions': self.permissions}]}}
        elif path.endswith('/scan/run/manual'):
            self.calls.append((method,path,body))
            if body['targets'] not in [[f'addp://engine/3/path/{name}?type=file'] for name in m.fixture.SOURCES]: raise AssertionError(body)
            data = {'execution_id': 'source-scan-'+body['targets'][0].split('/path/')[1].split('?')[0]}
        elif path == '/api/v1/develop/executions' and method == 'POST':
            self.calls.append((method,path,body))
            identifier = 'failed-snap' if body['content']['workflow_definition']['tasks'][-1]['params']['target_name']=='failed-snap.gpkg' else 'execution'
            self.executions['failed-snap'] = {'status': 'failed', 'metadata': {}}
            data = {'execution_id': identifier}
        elif '/meta/executions/' in path:
            data = {'status': 'success', 'tenant_id': self.scan_tenant, 'triggered_by': self.actor}
        elif path.endswith('/engines/3/items'):
            data = [self.source, self.invalid_source, *self.snap_sources.values(), *self.targets.values()]
        elif path.startswith('/api/v1/meta/scan/runs?'):
            identifiers = ['source-scan-'+name for name in m.fixture.SOURCES]+['scan-'+port for port in m.fixture.PORTS]
            data = {'total': len(identifiers), 'items': [{'execution_id': value} for value in identifiers]}
        elif path in ('/api/v1/meta/items/12','/api/v1/meta/items/13','/api/v1/meta/items/14','/api/v1/meta/items/15','/api/v1/meta/items/16','/api/v1/meta/items/17'):
            data = self.snap_sources[int(path.rsplit('/',1)[1])]
        elif path == '/api/v1/meta/items/11':
            data = self.invalid_source
        elif '/meta/lineage/graph?' in path:
            identifier = int(path.split('item_id=')[1].split('&')[0])
            data = {'truncated': False, 'edges': [{'source': {'item_id': source}, 'target': {'item_id': identifier}, 'status': 'active', 'relation_kind': 'derive', 'evidence': {'execution_id': 'execution'}} for source in ([17] if identifier == 26 else [15,16] if identifier == 25 else [14] if identifier == 24 else [12,13] if identifier == 23 else [11] if identifier == 22 else [10])]}
        else:
            return super().request(method, path, expected, body)
        return SimpleNamespace(payload=copy.deepcopy(data))


def physical(*args):
    return {'sources': {name: {'sha256': name, 'size_bytes': 500} for name in m.fixture.SOURCES}, 'exact_inventory_verified': True,
            'targets': {name: {'sha256': name, 'size_bytes': 1000, **({'layer_name': name.removesuffix('.gpkg'), 'geometry_column': 'geom', 'fid_column': 'fid'} if name.endswith('.gpkg') else {})} for name in m.fixture.TARGETS}}


ENV = {'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_TOPOLOGY_NFS_ENGINE_ID': '3', 'ADDP_ONLINE_TEST_RUN_ID': 'topology-1-1'}


class TopologyOnlineTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.env = {**ENV, "ADDP_ONLINE_ARTIFACT_DIR": directory.name}

    def test_public_dag_keeps_default_faces_diagnostics_and_checked_repair_branch(self):
        tasks = m.definition('source','parent','invalid','snap-source','snap-ref','overlap-source','gap-source','coverage-source','dangling-source')['tasks']
        self.assertEqual([node['operator'] for node in tasks], ['load','polygonize','save','save','load','check_geometry','repair_geometry','check_geometry','save','load','load','snap_geometry','save','load','check_polygon_overlap','save','load','load','check_polygon_gap','save','load','check_dangling_points','save'])
        self.assertEqual(tasks[2]['params']['input_df'], {'$ref': 'build'})
        self.assertEqual(tasks[3]['params']['input_df'], {'$ref': 'build', 'port': 'diagnostics'})
        self.assertTrue(all(node['params']['mode'] == 'fail' for node in tasks if node['operator'] == 'save'))

    def test_exact_seven_outputs_and_scans_are_mandatory(self):
        value = execution()
        targets = {key: value['outputs']['save_'+key]['resource']['locator'] for key in m.fixture.PORTS}
        sources = [f'addp://engine/3/path/{name}?type=file&item_id={identifier}' for identifier, name in enumerate(m.fixture.SOURCES, 10)]
        m.validate_success(value, sources, targets)
        for mutate in (lambda v: v['outputs'].pop('save_diagnostics'), lambda v: v['outputs'].pop('save_repaired'), lambda v: v['outputs'].pop('save_overlaps'), lambda v: v['outputs'].pop('save_gaps'), lambda v: v['outputs'].pop('save_dangling'),
            lambda v: v['metadata']['lineage_facts']['inputs'].pop(),
            lambda v: v['metadata']['lineage_facts']['inputs'][1].update(locator=v['metadata']['lineage_facts']['inputs'][0]['locator']), lambda v: v['metadata']['result']['meta_scan_runs'].pop(),
            lambda v: v['outputs']['save_faces']['resource'].pop('write_mode'),
            lambda v: v['metadata']['lineage_facts']['outputs'][0].pop('write_mode'),
            lambda v: v['metadata']['lineage_facts']['outputs'][1].update(write_mode='fail'),
            lambda v: v['metadata']['result']['meta_scan_runs'][1].update(execution_id='scan-faces'),
            lambda v: v['metadata']['result']['meta_scan_runs'][0].update(target_locator='parent'),
            lambda v: v['metadata']['lineage_facts']['outputs'].pop()):
            changed = copy.deepcopy(value); mutate(changed)
            with self.subTest(mutate=mutate), self.assertRaises(m.SuiteError): m.validate_success(changed, sources, targets)

    def test_lineage_inputs_bind_exact_read_ports_independently_of_array_order(self):
        value = execution()
        sources = [f'addp://engine/3/path/{name}?type=file&item_id={identifier}' for identifier, name in enumerate(m.fixture.SOURCES, 10)]
        targets = {key: value['outputs']['save_'+key]['resource']['locator'] for key in m.fixture.PORTS}
        self.assertEqual([entry['port'] for entry in value['metadata']['lineage_facts']['inputs']],
                         ['input.dangling_input', 'input.gap_coverage', 'input.gap_input', 'input.load', 'input.load_invalid', 'input.overlap_input', 'input.snap_input', 'input.snap_reference'])
        m.validate_success(value, sources, targets)
        value['metadata']['lineage_facts']['inputs'].reverse()
        m.validate_success(value, sources, targets)
        def swap_locators(inputs):
            inputs[0]['locator'], inputs[1]['locator'] = inputs[1]['locator'], inputs[0]['locator']
        for mutate in (
            lambda inputs: inputs[0].pop('port'),
            lambda inputs: inputs[0].update(port='input.foreign'),
            lambda inputs: inputs[0].update(port=inputs[1]['port']),
            lambda inputs: inputs.append(copy.deepcopy(inputs[0])),
            lambda inputs: inputs.extend([{'port':'input.unused','locator':'unrelated'}]),
            swap_locators,
        ):
            changed = copy.deepcopy(value)
            mutate(changed['metadata']['lineage_facts']['inputs'])
            with self.subTest(mutate=mutate), self.assertRaises(m.SuiteError):
                m.validate_success(changed, sources, targets)

    def test_complete_chain_preserves_same_user_and_does_not_manually_scan_outputs(self):
        client = Client()
        report = m.run_scenario(Path('.'), self.env, client, physical, lambda repo, env, evidence: evidence, diagnostic_runner=lambda *args: {'cause':'invalid_snapped_geometry'})
        self.assertEqual(report['result'], 'passed'); self.assertEqual(len(report['outputs']), 7)
        self.assertEqual(len(report['identity']['permissions_verified']), 11)
        self.assertEqual(len([call for call in client.calls if call[0] == 'POST' and '/scan/' in call[1]]), 8)
        self.assertEqual(report['schema_version'],'addp.vector-topology-persistence/v6')
        self.assertEqual(report['physical'],report['post_browser_physical'])
        self.assertEqual(report['stable_outputs'],execution()['outputs'])
        self.assertEqual(report['browser']['overlap_expected'],m.fixture.overlap_expected_features())
        self.assertEqual(report['browser']['gap_expected'],m.fixture.gap_expected_features())
        self.assertEqual(report['browser']['dangling_expected'],m.fixture.dangling_expected_features())

    def test_browser_requires_current_v6_and_exact_seventh_output_evidence(self):
        evidence={'execution_id':'current','outputs':[{'port':'gaps'}],'overlap_expected':m.fixture.overlap_expected_features(),'gap_expected':m.fixture.gap_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        for schema,changed,valid in [('addp.topology-browser/v5',False,False),('addp.topology-browser/v6',True,False),('addp.topology-browser/v6',False,True)]:
            def run(*args,**kwargs):
                report={**evidence,'schema_version':schema,'result':'passed'}
                if changed:report['outputs']=[]
                path.write_text(json.dumps(report))
                return SimpleNamespace(returncode=0)
            with self.subTest(schema=schema,changed=changed),patch.object(m.subprocess,'run',side_effect=run):
                if valid:self.assertEqual(m.browser(Path('.'),self.env,evidence)['result'],'passed')
                else:
                    with self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)

    def test_browser_cannot_omit_gap_oracle_from_current_report(self):
        evidence={'execution_id':'current','outputs':[{'port':'gaps'}],'gap_expected':m.fixture.gap_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        def run(*args,**kwargs):
            path.write_text(json.dumps({'execution_id':'current','outputs':evidence['outputs'],
                'schema_version':'addp.topology-browser/v6','result':'passed'}))
            return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=run),self.assertRaises(m.SuiteError):
            m.browser(Path('.'),self.env,evidence)

    def test_lineage_failure_retains_all_seven_strict_sizes_and_incomplete_physical_checkpoint(self):
        self.env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'] = 'must-not-archive'
        with patch.object(m.raster, 'wait_lineage', side_effect=m.SuiteError('lineage unavailable')):
            with self.assertRaisesRegex(m.SuiteError, 'lineage unavailable'):
                m.run_scenario(Path('.'), self.env, Client(), physical)
        saved = Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-before-lineage.json'
        checkpoint = json.loads(saved.read_text())
        self.assertEqual((checkpoint['stage'], checkpoint['result']), ('before_lineage', 'incomplete'))
        self.assertEqual(checkpoint['physical'], physical())
        self.assertEqual([entry['size_evidence'] for entry in checkpoint['outputs']],
                         [{'physical_size_bytes': 1000, 'meta_size_bytes': 1000}]*3 + [{'physical_size_bytes': 1000, 'meta_size_bytes': 1000, 'layer_name': name} for name in ('snapped','overlaps','gaps','dangling')])
        self.assertTrue(all(entry['triggered_by'] == 33 and entry['tenant_id'] == 2 for entry in checkpoint['automatic_scans']))
        self.assertEqual(checkpoint['lineage_facts'], execution()['metadata']['lineage_facts'])
        self.assertNotIn('must-not-archive', saved.read_text())
        self.assertFalse((saved.parent/'topology-persistence.json').exists())

    def test_failed_snap_rejects_extra_scan_and_wrong_monitor_status(self):
        class WrongClient(Client):
            submitted_failure = False
            def request(self, method, path, expected, body=None):
                response = super().request(method,path,expected,body)
                if method == 'POST' and response.payload.get('execution_id') == 'failed-snap':
                    self.submitted_failure = True
                if self.submitted_failure and self.problem == 'scan' and path.startswith('/api/v1/meta/scan/runs?'):
                    response.payload['items'].append({'execution_id':'unexpected'})
                    response.payload['total'] += 1
                if self.problem == 'monitor' and '/monitor/executions/' in path and path.endswith('failed-snap'):
                    response.payload['status'] = 'success'
                return response
        for problem in ('scan','monitor'):
            client = WrongClient(); client.problem = problem
            with self.subTest(problem=problem), self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'),self.env,client,physical,
                    lambda *args:self.fail('failure must stop before browser'), diagnostic_runner=lambda *args:{})

    def test_browser_failure_preserves_verified_negative_case_without_claiming_suite_success(self):
        self.env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'] = 'must-not-archive'
        def unavailable(*args): raise m.SuiteError('map failed')
        with self.assertRaisesRegex(m.SuiteError,'map failed'):
            m.run_scenario(Path('.'),self.env,Client(),physical,unavailable,
                diagnostic_runner=lambda *args:{'cause':'invalid_snapped_geometry'})
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-snap-failure.json'
        saved=json.loads(path.read_text())
        self.assertEqual((saved['result'],saved['stage']),('incomplete','collapsed_snap_validated'))
        self.assertEqual(saved['failed_snap']['execution_id'],'failed-snap')
        self.assertEqual(saved['failed_snap']['observation'],{'status':'failed','outputs':None,'lineage_facts':None,'meta_scan_runs':None})
        self.assertEqual(len(saved['scan_inventory']),15)
        self.assertEqual(saved['identity']['principal_id'],'33')
        self.assertNotIn('must-not-archive',path.read_text())
        self.assertFalse((path.parent/'topology-persistence.json').exists())

    def test_geo_package_container_boundary_and_both_snap_lineage_sources_are_required(self):
        item = Client().targets[23]
        m.validate_item(item,physical()['targets']['snapped.gpkg'],'snapped.gpkg')
        for mutate in (
            lambda value:value['attributes']['type_info']['container']['children'].clear(),
            lambda value:value['attributes']['item'].update(data_type='vector'),
            lambda value:value.update(size_bytes=True),
        ):
            changed=copy.deepcopy(item);mutate(changed)
            with self.subTest(mutate=mutate), self.assertRaises(m.SuiteError):
                m.validate_item(changed,physical()['targets']['snapped.gpkg'],'snapped.gpkg')
        seen=[]
        def lineage(client,source_id,target_id,execution_id,timeout):
            seen.append((source_id,target_id,execution_id));return {}
        with patch.object(m.raster,'wait_lineage',side_effect=lineage):
            m.run_scenario(Path('.'),self.env,Client(),physical,lambda repo,env,evidence:evidence,
                           diagnostic_runner=lambda *args:{})
        self.assertEqual(seen,[(10,20,'execution'),(10,21,'execution'),(11,22,'execution'),
                               (12,23,'execution'),(13,23,'execution'),(14,24,'execution'),(15,25,'execution'),(16,25,'execution'),(17,26,'execution')])

    def test_gap_dag_binds_both_read_roles_and_rejects_missing_or_swapped_facts(self):
        tasks=m.definition('line','parent','invalid','snap','reference','overlap','gap-source','coverage','dangling')['tasks'][:-3]
        self.assertEqual(tasks[-4]['params'], {'locator':'gap-source'})
        self.assertEqual(tasks[-3]['params'], {'locator':'coverage'})
        self.assertEqual(tasks[-2]['params'], {'input_gdf':{'$ref':'gap_input'},'coverage_gdf':{'$ref':'gap_coverage'}})
        self.assertEqual(tasks[-2]['depends_on'],['gap_input','gap_coverage'])
        sources=[f'addp://engine/3/path/{name}?type=file&item_id={identifier}' for identifier,name in enumerate(m.fixture.SOURCES,10)]
        targets={port:f'addp://engine/3/path/outputs/{name}?type=file' for port,name in zip(m.fixture.PORTS,m.fixture.TARGETS)}
        for problem in ('missing','swap','duplicate','scan'):
            value=execution();inputs=value['metadata']['lineage_facts']['inputs']
            if problem=='missing': inputs.pop(0)
            elif problem=='swap': inputs[0]['locator'],inputs[1]['locator']=inputs[1]['locator'],inputs[0]['locator']
            elif problem=='duplicate': inputs[0]['port']=inputs[1]['port']
            else: value['metadata']['result']['meta_scan_runs'][-1]['execution_id']='scan-overlaps'
            with self.subTest(problem=problem),self.assertRaises(m.SuiteError):m.validate_success(value,sources,targets)

    def test_gap_missing_lineage_on_either_source_blocks_browser_acceptance(self):
        for missing_source in (15,16):
            def lineage(client,source_id,target_id,execution_id,timeout):
                if target_id==25 and source_id==missing_source:raise m.SuiteError('gap lineage missing')
                return {}
            with self.subTest(source=missing_source),patch.object(m.raster,'wait_lineage',side_effect=lineage):
                with self.assertRaisesRegex(m.SuiteError,'gap lineage missing'):
                    m.run_scenario(Path('.'),self.env,Client(),physical,lambda *args:self.fail('both gap sources required'))

    def test_gap_missing_physical_source_and_wrong_scan_identity_cannot_pass(self):
        def missing(*args):
            value=physical();value['sources'].pop('gap-coverage.gpkg');return value
        with self.assertRaisesRegex(m.SuiteError,'physical target inventory'):
            m.run_scenario(Path('.'),self.env,Client(),missing,lambda *args:self.fail('missing boundary must fail'))
        class WrongGapScan(Client):
            def request(self,method,path,expected,body=None):
                response=super().request(method,path,expected,body)
                if '/meta/executions/scan-gaps' in path:response.payload['triggered_by']=44
                return response
        with self.assertRaisesRegex(m.SuiteError,'User or Tenant'):
            m.run_scenario(Path('.'),self.env,WrongGapScan(),lambda *args:self.fail('wrong gap scan actor'))

    def test_extra_permission_or_wrong_scan_actor_blocks_acceptance(self):
        for change in ('permission','actor'):
            client = Client()
            if change == 'permission': client.permissions.append('develop.task.create')
            else: client.actor = 44
            with self.subTest(change=change), self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'), self.env, client, lambda *args: self.fail('must stop before physical acceptance'))

    def test_scan_identity_requires_public_integer_user_and_tenant(self):
        for field, values in (('actor', (None, True, 0, -1, '33', 33.)),
                              ('scan_tenant', (None, True, 0, -1, 3, '2', 2.))):
            for value in values:
                client = Client(); setattr(client, field, value)
                with self.subTest(field=field, value=value), self.assertRaisesRegex(m.SuiteError, 'User or Tenant'):
                    m.run_scenario(Path('.'), self.env, client, lambda *args: self.fail('must stop before physical acceptance'))

    def test_private_actor_cannot_replace_public_initiator(self):
        class PrivateOnlyClient(Client):
            def request(self, method, path, expected, body=None):
                if '/meta/executions/' in path:
                    return SimpleNamespace(payload={'status': 'success', 'tenant_id': 2, 'actor_principal_id': 33})
                return super().request(method, path, expected, body)
        with self.assertRaisesRegex(m.SuiteError, 'User or Tenant'):
            m.run_scenario(Path('.'), self.env, PrivateOnlyClient(), lambda *args: self.fail('private actor must not be consumed'))

    def test_invalid_size_or_browser_modification_cannot_pass(self):
        for size in (True, 0, 1000., '1000', 999):
            item = Client().targets[20]; item['size_bytes'] = size
            with self.subTest(size=size), self.assertRaises(m.SuiteError): m.validate_item(item, physical()['targets']['faces.geojson'], 'faces.geojson')
        calls = []
        def changed(*args):
            value = physical(); calls.append(1)
            if len(calls) > 1: value['sources']['invalid.geojson']['sha256'] = 'modified'
            return value
        with self.assertRaisesRegex(m.SuiteError, 'changed scans, sources or saved outputs'):
            m.run_scenario(Path('.'), self.env, Client(), changed, lambda repo, env, evidence: evidence, diagnostic_runner=lambda *args: {})

    def test_snap_public_dag_and_failure_no_publication_contract(self):
        for failed, tolerance, name in ((False,.75,'snapped.gpkg'),(True,1.25,'failed-snap.gpkg')):
            tasks = m.snap_definition('source','reference','parent',failed=failed)['tasks']
            self.assertEqual(tasks[2]['params'], {'input_gdf':{'$ref':'snap_input'},'reference_gdf':{'$ref':'snap_reference'},'tolerance':tolerance})
            self.assertEqual(tasks[-1]['params']['target_name'],name)
        self.assertEqual(m.output_sources('snapped'),(2,3))
        self.assertEqual(m.output_sources('overlaps'),(4,))
        self.assertEqual(m.output_sources('gaps'),(5,6))
        for value in ({'status':'success','metadata':{}}, {'status':'failed','outputs':{'save':{}},'metadata':{}},
                      {'status':'failed','metadata':{'lineage_facts':{'outputs':[{'locator':'unexpected'}]}}},
                      {'status':'failed','metadata':{'result':{'meta_scan_runs':[{'execution_id':'unexpected'}]}}}):
            with self.subTest(value=value), self.assertRaises(m.SuiteError): m.validate_failed_snap(value)
        m.validate_failed_snap({'status':'failed','metadata':{}})

    def test_collapsed_snap_diagnostic_rejects_wrong_owner_execution_and_cause(self):
        directory = Path(self.env['ADDP_ONLINE_ARTIFACT_DIR']); log = directory/'develop'/'backend'/'runtime.jsonl'
        log.parent.mkdir(parents=True)
        event = {'module_name':'develop','role':'backend','message':'[DevExecutor] 工作流引擎调用失败: execution_id=failed-snap err=任务 snap 执行失败: snapping produced invalid geometries; reduce tolerance or revise reference'}
        env = {'ADDP_RUNTIME_LOG_ROOT':str(directory)}
        for field, value in (('module_name','manager'),('role','frontend'),('message','foreign failure')):
            wrong = {**event,field:value};log.write_text(json.dumps(wrong)+'\n')
            with self.subTest(field=field), self.assertRaises(m.SuiteError): m.collapsed_snap_diagnostic(Path('.'),env,'failed-snap',timeout=0)
        log.write_text(json.dumps(event)+'\n')
        with self.assertRaises(m.SuiteError): m.collapsed_snap_diagnostic(Path('.'),env,'other-execution',timeout=0)
        self.assertEqual(m.collapsed_snap_diagnostic(Path('.'),env,'failed-snap',timeout=0)['cause'],'invalid_snapped_geometry')


    def test_dangling_dag_and_current_report_require_original_source_evidence(self):
        tasks=m.definition('line','parent','invalid','snap','reference','overlap','gap','coverage','dangle-source')['tasks']
        self.assertEqual(tasks[-3]['params'],{'locator':'dangle-source'})
        self.assertEqual(tasks[-2]['params'],{'input_gdf':{'$ref':'dangling_input'},'id_field':'route_key'})
        self.assertEqual(tasks[-1]['params']['target_name'],'dangling.gpkg')
        self.assertEqual(m.output_sources('dangling'),(7,))
        evidence={'execution_id':'current','outputs':[{'port':'dangling'}],'dangling_expected':m.fixture.dangling_expected_features()}
        path=Path(self.env['ADDP_ONLINE_ARTIFACT_DIR'])/'topology-browser.json'
        def run(*args,**kwargs):
            path.write_text(json.dumps({'execution_id':'current','outputs':evidence['outputs'],
                'schema_version':'addp.topology-browser/v6','result':'passed'}))
            return SimpleNamespace(returncode=0)
        with patch.object(m.subprocess,'run',side_effect=run),self.assertRaises(m.SuiteError):m.browser(Path('.'),self.env,evidence)

    def test_dangling_missing_lineage_blocks_browser_acceptance(self):
        def lineage(client,source_id,target_id,execution_id,timeout):
            if source_id==17 and target_id==26:raise m.SuiteError('dangling lineage missing')
            return {}
        with patch.object(m.raster,'wait_lineage',side_effect=lineage),self.assertRaisesRegex(m.SuiteError,'dangling lineage missing'):
            m.run_scenario(Path('.'),self.env,Client(),physical,
                lambda *args:self.fail('missing source lineage must stop before browser'))

    def test_point_map_identity_comparison_is_locale_independent_and_keeps_multiplicity(self):
        # Execute the actual Online Point assertion in separate Node/browser
        # realms. Their default collations differ on the original Chinese IDs.
        script = r'''
import assert from 'node:assert/strict'
import fs from 'node:fs'
import vm from 'node:vm'
import { webMercatorPreviewCoordinate } from './manager/frontend/e2e/geometry-preview-support.js'
const source = fs.readFileSync('console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8')
const start = source.indexOf("    if (output.port === 'dangling') {", source.indexOf('const table = frame.locator'))
const end = source.indexOf("    if (output.port === 'gaps') {",start)
assert(start > 0 && end > start)
const check = new (Object.getPrototypeOf(async function(){}).constructor)('evidence','renderer','expect','table','webMercatorPreviewCoordinate','output',source.slice(start,end))
const expected = JSON.parse(fs.readFileSync(0,'utf8'))
assert.equal(expected.length,17)
const originalCompare = String.prototype.localeCompare
String.prototype.localeCompare = function(other){ return originalCompare.call(this,other,'en-US') }
const nodeOrder = ['孤线','干线'].sort((a,b) => a.localeCompare(b))
const browserOrder = ['孤线','干线'].sort(new Intl.Collator('zh-CN').compare)
assert.notDeepEqual(nodeOrder,browserOrder,'the regression must exercise different collations')
const expectedPoints = expected.map(row => ({source_id:row.properties.source_id,coordinates:webMercatorPreviewCoordinate(row.geometry.coordinates)}))
async function run(points) {
  const features = points.map(point => ({get:name => point[name],getGeometry:() => ({getCoordinates:() => point.coordinates})}))
  const layer = { getSource: () => ({ getFeatures: () => features }) }
  const map = {
    getView: () => ({ getAnimating: () => false, getProjection: () => ({ getCode: () => 'EPSG:3857' }) }),
    getLayers: () => ({ getArray: () => [layer] })
  }
  const element = {__vueParentComponent:{setupState:{mapInstance:map}}}
  const context = vm.createContext({element})
  vm.runInContext("const compare=String.prototype.localeCompare;String.prototype.localeCompare=function(other){return compare.call(this,other,'zh-CN')}",context)
  const renderer = {evaluate:async fn => JSON.parse(JSON.stringify(vm.runInContext('('+fn.toString()+')(element)',context)))}
  const expect = {poll:fn => ({toEqual:async value => assert.deepEqual(await fn(),value)})}
  const table = {locator:() => ({})}; const assertion = expect
  const callable = Object.assign(() => ({toContainText:async() => {}}),assertion)
  await check({dangling_expected:expected},renderer,callable,table,webMercatorPreviewCoordinate,{port:'dangling'})
}
await run([...expectedPoints].reverse())
for (const kind of ['missing','duplicate','wrong-source','wrong-coordinate','micro-gap-rounded','source-coordinate-mismatch']) {
  const points = structuredClone(expectedPoints)
  if(kind==='missing') points.pop()
  if(kind==='duplicate') points.push(structuredClone(points[0]))
  if(kind==='wrong-source') points[0].source_id='闭环'
  if(kind==='wrong-coordinate') points[0].coordinates[0]+=1
  if(kind==='micro-gap-rounded') points.find(p => p.source_id==='微乙').coordinates=webMercatorPreviewCoordinate([251,0])
  if(kind==='source-coordinate-mismatch') [points[0].source_id,points[2].source_id]=[points[2].source_id,points[0].source_id]
  await assert.rejects(run(points),{code:'ERR_ASSERTION'},kind)
}
console.log('cross-realm Chinese/English Point identity and six corruptions verified')
'''
        result = m.subprocess.run(['node','--input-type=module','-e',script],
            input=json.dumps(m.fixture.dangling_expected_features(),ensure_ascii=False),
            text=True,capture_output=True,timeout=30)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)

    def test_point_map_pixels_reject_empty_water_and_keep_line_polygon_checks(self):
        script = r'''
import assert from 'node:assert/strict'
import fs from 'node:fs'
const source = fs.readFileSync('console/frontend/e2e/online/vector-topology-persistence.spec.js','utf8')
const start = source.indexOf('    let ',source.indexOf("    if (output.port === 'dangling') {",source.indexOf('const table = frame.locator')))
const end = source.indexOf('    const rendered =',start)
assert(start > 0 && end > start)
const check = new (Object.getPrototypeOf(async function(){}).constructor)('renderer','expect','output',source.slice(start,end))
async function run(port,rgba) {
  const data=Uint8ClampedArray.from(Array.from({length:40},() => rgba).flat())
  const canvases=[{getContext:() => ({getImageData:() => ({data})}),width:40,height:1}]
  const renderer={locator:() => ({evaluateAll:async(fn,arg) => fn(canvases,arg)})}
  const expect={poll:fn => ({toBeGreaterThan:async minimum => assert((await fn())>minimum)})}
  await check(renderer,expect,{port})
}
await run('dangling',[64,158,255,255])
await assert.rejects(run('dangling',[173,216,230,255]),{code:'ERR_ASSERTION'},'basemap water cannot count as Points')
await assert.rejects(run('dangling',[103,194,58,255]),{code:'ERR_ASSERTION'},'green line/polygon pixels cannot substitute Points')
await assert.rejects(run('dangling',[64,158,255,0]),{code:'ERR_ASSERTION'},'transparent Points are not visible')
await run('gaps',[103,194,58,255])
await assert.rejects(run('gaps',[64,158,255,255]),{code:'ERR_ASSERTION'},'old green geometry assertion remains strict')
console.log('Point foreground pixels and preserved polygon assertions verified')
'''
        result=m.subprocess.run(['node','--input-type=module','-e',script],
            text=True,capture_output=True,timeout=30)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)
