import copy,hashlib,importlib,json,tempfile,uuid,urllib.parse
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
import unittest
scene=importlib.import_module('scripts.test.raster-sight-orthogonal-persistence-online');fixture=scene.fixture
FROZEN={n:({'row':{'distance_m':20.,'status':'blocked'},'preview_row':{'distance_m':20.,'status':'blocked'}}if n.endswith('.csv')else{'artifact':fixture.artifact_expectation(n),'state_counts':{'0':1,'1':2,'2':3,'255':fixture.source_spec(fixture.CASES[n][0])['width']*fixture.source_spec(fixture.CASES[n][0])['height']-6}})for n in fixture.TARGETS}
SOURCES={n:{'size_bytes':4096,'sha256':hashlib.sha256(n.encode()).hexdigest()}for n in fixture.SOURCES}
def raw_cog(name):return b'distance_m,status\n20.0,blocked\n'if name.endswith('.csv')else('COG:'+name).encode()
def native_fact(name):
 fact={'size_bytes':len(raw_cog(name)),'sha256':hashlib.sha256(raw_cog(name)).hexdigest()}
 if name.endswith('.csv'):
  import base64
  return {**fact,'row':FROZEN[name]['row'],'csv_base64':base64.b64encode(raw_cog(name)).decode(),'complete_csv_verified':True}
 return {**fact,'cog_valid':True,'has_overviews':name.startswith('survey'),'overview_count':3 if name.startswith('survey')else 0,'valid_pixels':6,'state_counts':copy.deepcopy(FROZEN[name]['state_counts']),'full_pixel_and_mask_verification':True,'independent_cell_clipping_verified':True,'nearest_overviews_verified':True}
def artifact(name):
 if name.endswith('.csv'):return {'type':'pandas_dataframe','schema':scene.sight.fixture.SCHEMA,'row_count':1,'preview_limit':50,'truncated':False,'preview_rows':[FROZEN[name]['preview_row']]}
 return {**fixture.artifact_expectation(name),'artifact_type':'raster','format':'tiff','profile':'cog','size_bytes':len(raw_cog(name)),'bands':[{'dtype':'Byte','nodata':255,'nodata_is_nan':False}]}
def private_attributes(name):
 if name.endswith('.csv'):return {'item':{'format':'csv'}}
 exp=fixture.artifact_expectation(name)
 return {'item':{'format':'tiff'},'type_info':{'media':{'width':exp['width'],'height':exp['height']}},'format_info':{'tiff':{'profile':'cog','is_cloud_optimized':True,'cog_check_level':'heuristic','is_tiled':True,'has_overviews':name.startswith('survey')}},'capabilities':{'spatial':{'srid':exp['extent_srid'],'extent':exp['extent']}}}
def physical(repository,env,action,names,timeout):
 assert 0<timeout<=600
 if action=='cleanup-oracle':return {'oracle_cache_removed':True}
 targets={n:native_fact(n)for n in names};manifest={}
 for n in names:
  p=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/scene.ARCHIVE_NAME/n
  if not p.exists():p.write_bytes(raw_cog(n))
  manifest[n]={'path':scene.ARCHIVE_NAME+'/'+n,**fixture.digest(p)}
 result={'sources':copy.deepcopy(SOURCES),'targets':targets,'archived_files':manifest,'runtime_workspaces_removed':True,'oracle_cache':{'scope_sha256':'a'*64,'read_only_expected':True,'expected_arrays_loaded':sum(n.endswith('.tif')for n in names),'full_target_checks':len(names)}}
 if action=='prepare-oracle':result.update(expectations=copy.deepcopy(FROZEN),scope_sha256='a'*64,anchor_sha256='b'*64,expected_array_count=8)
 return result
def diagnostic(repository,env,identifier,problem,language,timeout):return scene.diagnostic_fact(identifier,problem,language)
class Metadata:
 def __init__(self,env,client,*args):self.client=client.client
 def find(self,name,binding=None):return copy.deepcopy(self.client.items[name])
 def snapshot(self,identifier):return copy.deepcopy(next(i for i in self.client.items.values()if i['id']==identifier))
 def execution(self,*args):return {}
 def cleanup(self):return {'grants_revoked':True,'metadata_hidden':True}

class Client:
    timeout = 30

    def __init__(self, fault=None):
        self.fault = fault
        self.scans = {}
        self.executions = {}
        self.relationships = []
        self.items = {name: {'id': i+10, 'full_name': name, 'item_type': 'file', 'size_bytes': 4096,
                             'fingerprint': name, 'scanned_at': 'now'} for i, name in enumerate(fixture.SOURCES)}

    def submit(self, body, headers):
        dag = body['content']['workflow_definition']
        name = dag['tasks'][-1]['params']['target_name']
        success = name in fixture.CASES
        identifier = str(uuid.UUID(int=len(self.executions)+1))
        language = headers['Accept-Language']
        execution = {'execution_id': identifier, 'module': 'develop', 'task_type': 'workflow',
                     'status': 'success' if success else 'failed', 'triggered_by': 33,
                     'execution_config': {'engine_id': 7, 'display_language': language,
                                          'content': {'workflow_definition': dag}},
                     'error_msg': None if success else 'frozen-'+language,
                     'error_details': {} if success else {'category': 'execution_failed'},
                     'outputs': {}, 'metadata': {'result': {}}}
        if success:
            target = 'addp://engine/3/path/outputs/'+name+'?type=file'
            scan = 'scan-'+str(len(self.executions)+1)
            source = dag['tasks'][0]['params']['locator']
            expected = scene.expected_lineage(source, target)
            execution['outputs'] = {'save': {'resource': {'locator': target, 'type': 'file', 'write_mode': 'create'}}}
            execution['metadata'] = {'lineage_facts': {**expected, 'operations': None}, 'result': {
                'final_result': artifact(name),
                'meta_scan_runs': [{'status': 'submitted', 'target_locator': target, 'execution_id': scan}]}}
            self.scans[scan] = identifier
            size = len(raw_cog(name))
            item_id = 1000+len(self.executions)
            self.items['outputs/'+name] = {'id': item_id, 'full_name': 'outputs/'+name, 'item_type': 'file',
                                           'size_bytes': size+int(self.fault == 'size'), 'scanned_at': 'now', 'fingerprint': name,
                                           'attributes': private_attributes(name)}
            source_id = self.items[fixture.CASES[name][0]]['id']
            self.relationships.append({'source': {'item_id': source_id}, 'target': {'item_id': item_id},
                                       'evidence': {'execution_id': identifier}, 'status': 'active', 'relation_kind': 'derive'})
            if self.fault == 'extra-meta-relationship':
                self.relationships.append({**self.relationships[-1], 'source': {'item_id': 9999}})
            if self.fault == 'public-lineage':
                execution['metadata']['lineage_facts']['operations'] = expected['operations']
        elif self.fault == 'negative-scan':
            self.scans['unexpected'] = identifier
        elif self.fault == 'negative-edge':
            self.relationships.append({'source': {'item_id': self.items['north.tif']['id']},
                                       'target': {'item_id': 9999}, 'evidence': {'execution_id': identifier},
                                       'status': 'active', 'relation_kind': 'derive'})
        elif self.fault == 'source-meta':
            self.items['north.tif']['fingerprint'] = 'changed'
        elif self.fault == 'target-meta':
            self.items['outputs/north.cog.tif']['fingerprint'] = 'changed'
        self.executions[identifier] = copy.deepcopy(execution)
        return {'execution_id': identifier}

    def request(self, method, path, expected, body=None, headers=None):
        if path.endswith('/auth/context'):
            data = {'principal': {'id': '33', 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                    'token': {'type': 'first_party_access_token'}, 'authorization': {'role_assignments': [
                        {'role_key': 'raster-consumer', 'permissions': sorted(scene.raster.EDITOR_PERMISSIONS)}]}}
        elif path.endswith('/scan/run/manual'):
            raise scene.SuiteError('ordinary business source initialization returned HTTP 403')
        elif '/meta/executions/' in path:
            identifier = path.rsplit('/', 1)[-1]
            parent = self.scans[identifier]
            data = {'execution_id': identifier, 'parent_execution_id': parent,
                    'module': 'meta', 'task_type': 'scan', 'status': 'success', 'tenant_id': 2,
                    'triggered_by': 33, 'scan_context': {'engine_id': 4 if parent and self.fault == 'scan-engine' else 3}}
        elif path.endswith('/workflow-engines'):
            data = [{'id': 7, 'engine_type': 'geopython_workflow', 'connection_status': 'online'}]
        elif path.endswith('/engines/3/items'):
            data = list(self.items.values())
        elif '/meta/items/' in path:
            data = next(v for v in self.items.values() if v['id'] == int(path.rsplit('/', 1)[-1]))
        elif '/meta/scan/runs?' in path:
            ids=[n for n,parent in self.scans.items() if parent is not None]
            if self.fault=='consumer-extra-source-scan':ids.append('source-scan')
            data = {'total': len(ids), 'items': [{'execution_id': n} for n in ids]}
        elif '/meta/lineage/graph?' in path:
            query = urllib.parse.parse_qs(path.split('?', 1)[1])
            side = 'target' if query['direction'] == ['upstream'] else 'source'
            data = {'truncated': False, 'edges': [edge for edge in self.relationships
                    if edge[side]['item_id'] == int(query['item_id'][0])]}
        elif path == '/api/v1/develop/executions' and method == 'POST':
            data = self.submit(body, headers)
        elif '/develop/executions/' in path or '/monitor/executions/' in path:
            data = copy.deepcopy(self.executions[path.rsplit('/', 1)[-1]])
            if self.fault == 'language' and headers and data['status'] == 'failed':
                data['error_msg'] = 'rewritten'
            if self.fault == 'monitor' and '/monitor/' in path:
                data['execution_id'] = 'other'
            if '/monitor/' in path:
                for key in ('execution_config','outputs','error_msg'):data.pop(key,None)
                data['metadata'].pop('result',None)
        else:
            raise AssertionError((method, path, body))
        return SimpleNamespace(payload=copy.deepcopy(data), raw=json.dumps(data).encode(), status=expected[0])


class SourceClient:
    timeout=30
    def __init__(self,business):
        self.business=business
        self.calls=[]
    def request(self,method,path,expected,body=None,headers=None):
        self.calls.append((method,path))
        fault=self.business.fault
        if path.endswith('/auth/context'):
            registration=importlib.import_module('scripts.test.online-engine-registration')
            permissions=registration.SOURCE_INITIALIZER_PERMISSIONS|{'meta.scan_task.execute','meta.scan_task.read'}
            if fault=='source-extra-permission':permissions=permissions|{'develop.task.execute'}
            if fault=='source-missing-permission':permissions=permissions-{'meta.scan_task.execute'}
            data={'principal':{'id':'33'if fault=='source-same-principal'else'44','type':'user'},
                'context':{'type':'tenant','tenant_id':'3'if fault=='source-tenant'else'2'},
                'authorization':{'role_assignments':[{'role_key':'wrong'if fault=='source-role'else registration.SOURCE_INITIALIZER_ROLE,'permissions':sorted(permissions)}]}}
        elif path.endswith('/scan/run/manual'):
            assert body['targets']==[f'addp://engine/3/path/{n}?type=file'for n in fixture.SOURCES]
            self.business.scans['source-scan']=None
            data={'execution_id':'source-scan'}
        elif path.endswith('/meta/executions/source-scan'):
            data={'execution_id':'source-scan','parent_execution_id':'wrong'if fault=='source-parent'else None,
                'module':'meta','task_type':'scan','status':'success','tenant_id':2,
                'triggered_by':33 if fault=='source-actor'else 44,
                'scan_context':{'engine_id':4 if fault=='source-engine'else 3}}
        elif '/meta/scan/runs?'in path:
            ids=[n for n,parent in self.business.scans.items()if parent is None]
            if fault=='source-extra-scan':ids.append('unexpected')
            data={'total':len(ids),'items':[{'execution_id':n}for n in ids]}
        else:raise AssertionError(('preparer must never submit/read business execution',method,path))
        return SimpleNamespace(payload=copy.deepcopy(data),raw=json.dumps(data).encode(),status=expected[0])


class SightOrthogonalWorkflowTest(unittest.TestCase):
    def run_case(self, fault=None, worker=physical, diagnostic_runner=diagnostic, budget='600'):
        root = Path(self.enterContext(tempfile.TemporaryDirectory()))
        env = {'ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN':'separate-source-token','GATEWAY_URL':'http://ordinary-fixture.invalid','ADDP_ONLINE_TEST_USER_ACCESS_TOKEN':'ordinary-fixture-token', 'ONLINE_SUITE': scene.SUITE, 'ADDP_ONLINE_TEST_RUN_ID': 'sight-orthogonal-123-1',
               'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_SIGHT_ORTHOGONAL_NFS_ENGINE_ID': '3',
               'ADDP_ONLINE_TEST_TIMEOUT_SECONDS': budget, 'ADDP_ONLINE_ARTIFACT_DIR': str(root/'evidence')}
        client = Client(fault)
        self.source_client=SourceClient(client)
        if fault=='source-token-missing':env.pop('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN')
        self.evidence = root/'evidence'

        def reader(env, identifier, tenant, principal, dag):
            self.assertEqual((tenant, principal), (2, 33))
            execution = client.executions[identifier]
            self.assertEqual(execution['execution_config']['content']['workflow_definition'], dag)
            name = dag['tasks'][-1]['params']['target_name']
            expected = scene.expected_lineage(dag['tasks'][0]['params']['locator'],
                                              'addp://engine/3/path/outputs/'+name+'?type=file')
            if fault == 'internal-lineage':
                expected['operations'] = []
            return expected

        # Exercise the production entrypoint; only external I/O is isolated.
        import contextlib,io,os
        lifecycle=scene.view.run_scenario
        def isolated_lifecycle(repository,actual_env,actual_client,*args,**kwargs):
            self.assertIs(kwargs['profile'],scene)
            return lifecycle(repository,actual_env,actual_client,worker,diagnostic_runner,reader,metadata_factory=Metadata,profile=scene)
        output=io.StringIO()
        with patch.dict(os.environ,env,clear=True), patch.object(scene,'DiagnosticClient',return_value=client)as constructor, patch.object(scene.view.support,'GatewayClient',return_value=self.source_client), patch.object(scene.view,'run_scenario',side_effect=isolated_lifecycle), contextlib.redirect_stdout(output):
            scene.main()
        constructor.assert_called_once_with(env['GATEWAY_URL'],env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30,env['ADDP_ONLINE_ARTIFACT_DIR'])
        report=json.loads((self.evidence/(scene.PREFIX+'-persistence.json')).read_bytes())
        self.assertEqual(json.loads(output.getvalue()),report)
        return report

    def test_complete_fixed_matrix_rereads_and_original_manifest(self):
        calls = []

        def tracked(*args):
            calls.append(tuple(args[3]))
            return physical(*args)

        result = self.run_case(worker=tracked)
        self.assertEqual([len(names) for names in calls], [0, *range(1, 17), *([16]*40),0])
        self.assertEqual((len(result['cases']), len(result['failed_parameters']),
                          result['full_target_checks'], result['physical_verification_calls']), (16, 40, 776, 57))
        self.assertEqual({(case['problem'], case['language']) for case in result['failed_parameters']},
                         {(problem, language) for problem in scene.FAILURES for language in ('zh-cn', 'en')})
        self.assertEqual(len({case['execution_id'] for case in result['cases']+result['failed_parameters']}), 56)
        self.assertEqual(len(result['archived_files']), 16)
        manifest = json.loads((self.evidence/'sight-orthogonal-files-manifest.json').read_bytes())
        self.assertEqual(manifest, result['archived_files'])
        for name, value in manifest.items():
            data = (self.evidence/value['path']).read_bytes()
            self.assertEqual(data,raw_cog(name))
            self.assertEqual({'size_bytes':len(data),'sha256':hashlib.sha256(data).hexdigest()}, {key: value[key] for key in ('size_bytes', 'sha256')})
        self.assertEqual(result['source_scan_identity']['triggered_by'],44)
        source_identity=json.loads((self.evidence/'source-preparation-identity.json').read_bytes())
        self.assertEqual((source_identity['principal_id'],len(source_identity['permissions_verified'])),('44',13))
        self.assertNotIn('separate-source-token',json.dumps(result))
        for case in result['cases']:
            self.assertEqual(case['meta_relation']['execution_id'], case['execution_id'])
            self.assertEqual(case['scan_identity']['parent_execution_id'], case['execution_id'])
            self.assertEqual(case['meta_size_bytes'], case['physical_size_bytes'])

    def test_source_preparation_identity_scan_and_inventory_fail_closed(self):
        for fault in ('source-token-missing','source-same-principal','source-tenant','source-role',
                      'source-extra-permission','source-missing-permission','source-actor',
                      'source-parent','source-engine','source-extra-scan','consumer-extra-source-scan'):
            with self.subTest(fault=fault),self.assertRaises(scene.SuiteError):
                self.run_case(fault=fault)

    def test_old_business_initial_scan_is_refused_without_expanding_permissions(self):
        with patch.object(scene,'source_scan_client',side_effect=lambda env,client,identity,remaining:(client,identity)):
            with self.assertRaisesRegex(scene.SuiteError,'HTTP 403'):
                self.run_case()

    def test_identity_language_lineage_scan_size_and_metadata_fail_closed(self):
        for fault in ('size', 'public-lineage', 'internal-lineage', 'extra-meta-relationship', 'scan-engine', 'monitor',
                      'negative-scan', 'negative-edge', 'source-meta', 'target-meta', 'language'):
            with self.subTest(fault=fault), self.assertRaises(scene.SuiteError):
                self.run_case(fault=fault)

    def test_wrong_exact_cause_and_expanded_budget_fail(self):
        def wrong(*args):
            return {**diagnostic(*args), 'exact_cause': 'different'}
        with self.assertRaisesRegex(scene.SuiteError, 'wrong exact failure'):
            self.run_case(diagnostic_runner=wrong)
        with self.assertRaisesRegex(scene.SuiteError, 'exactly 600'):
            self.run_case(budget='601')

    def test_missing_actual_reread_or_changed_source_or_raw_bytes_fails(self):
        for fault in ('missing', 'source', 'raw', 'size', 'cleanup'):
            def broken(repository, env, action, names, timeout):
                report = physical(repository, env, action, names, timeout)
                if len(names) == 2:
                    if fault == 'missing':
                        report['targets'].pop(names[0])
                    elif fault == 'source':
                        report['sources']['north.tif']['size_bytes'] += 1
                    elif fault == 'raw':
                        (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'sight-orthogonal-files'/names[0]).write_bytes(b'corrupt')
                    elif fault == 'size':
                        report['targets'][names[0]]['size_bytes'] = True
                    else:
                        report['runtime_workspaces_removed'] = False
                return report
            with self.subTest(fault=fault), self.assertRaises(scene.SuiteError):
                self.run_case(worker=broken)





class SightMetadataTest(unittest.TestCase):
    def setUp(self):
        import os
        self.root=Path(self.enterContext(tempfile.TemporaryDirectory()));self.secret=self.root/'secret';self.secret.mkdir(mode=0o700);self.artifacts=self.root/'evidence';self.artifacts.mkdir()
        self.env={'ONLINE_SUITE':scene.SUITE,'ADDP_ONLINE_TEST':'1','ADDP_ONLINE_TEST_RUN_ID':'sight-orthogonal-123-1','ADDP_ONLINE_TEST_TIMEOUT_SECONDS':'600',
            'GITHUB_RUN_ID':'123','GITHUB_RUN_ATTEMPT':'1','ADDP_ONLINE_SECRET_DIR':str(self.secret),'ADDP_ONLINE_ARTIFACT_DIR':str(self.artifacts),
            'ADDP_ONLINE_TEST_TENANT_ID':'2','GATEWAY_URL':'http://fixture','ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN':'private-issuer'}
        self.container={'Id':'a'*64,'State':{'Running':True,'StartedAt':'2026-11-08T00:00:00Z'},'Config':{'Labels':{
            'com.docker.compose.project':'addp-infra','com.docker.compose.service':'postgres','com.docker.compose.project.working_dir':str(Path(__file__).resolve().parents[2])},'Env':['POSTGRES_DB=addp_online','POSTGRES_USER=addp','POSTGRES_PASSWORD=private']}}
        helper=importlib.import_module('scripts.test.online-engine-registration_test').OnlineEngineRegistrationTest()
        self.issuer,self.client,self.facts,self.state=helper.metadata_clients(3,'outputs/north.csv','nfs','table','csv','single');self.issuer.timeout=self.client.timeout=30
        issuer_request=self.issuer.request.side_effect
        def authorize(method,path,expected,body=None):
            result=issuer_request(method,path,expected,body)
            if path.endswith('/auth/context'):
                result=SimpleNamespace(payload=copy.deepcopy(result.payload))
                result.payload['authorization']['role_assignments'][0]['permissions']=sorted(scene.view.importlib.import_module('scripts.test.online-engine-registration').SOURCE_INITIALIZER_PERMISSIONS|{'meta.scan_task.execute','meta.scan_task.read'})
            return result
        self.issuer.request.side_effect=authorize
        self.public={'id':12,'tenant_id':2,'engine_id':3,'node_id':4,'name':'north.csv','full_name':'outputs/north.csv','fingerprint':self.facts['item_fingerprint'],'item_type':'file','attributes':{'item':{'format':'csv'}}}
        original=self.client.request.side_effect
        def request(method,path,expected,body=None):
            if path=='/api/v1/meta/engines/3/items':return SimpleNamespace(payload=[copy.deepcopy(self.public)]if self.state['visible']else[])
            return original(method,path,expected,body)
        self.client.request.side_effect=request
        self.enterContext(patch.dict(os.environ,self.env,clear=True));self.enterContext(patch.object(scene.lineage.BASE.HOSTED,'deployment_secret',return_value=self.secret));self.enterContext(patch.object(scene.lineage,'inspect',return_value=self.container));self.enterContext(patch.object(scene.view.support,'GatewayClient',return_value=self.issuer))
        receipt=self.secret/scene.lineage.RECEIPT;receipt.write_text(json.dumps({'schema':'addp.raster-lineage-owner/v1','suite':scene.SUITE,'run_id':self.env['ADDP_ONLINE_TEST_RUN_ID'],'container_id':self.container['Id'],'started_at':self.container['State']['StartedAt'],'postgres_user':'addp'}));receipt.chmod(0o600)
        self.owner=scene.ViewshedMetadata(self.env,self.client,3,{'tenant_id':'2','principal_id':'7'},str(uuid.UUID(int=9)),lambda:599)
        self.binding={'execution':str(uuid.UUID(int=11)),'scan':str(uuid.UUID(int=12)),'locator':'addp://engine/3/path/outputs/north.csv?type=file'}

    def test_csv_exact_grant_current_binding_and_revocation(self):
        with self.assertRaises(scene.SuiteError):self.owner.find('outputs/north.csv')
        self.assertEqual(self.owner.find('outputs/north.csv',binding=self.binding),self.public)
        receipt,=self.owner.report['issued'];self.assertEqual((receipt['action'],receipt['expiry_mode'],receipt['recipient_id']),('metadata_read','at_time','7'))
        self.assertEqual(self.owner.file_contract('outputs/north.csv'),('table','csv',('single',)))
        self.owner.cleanup();self.assertFalse(self.state['visible']);self.assertFalse(self.owner.report['pending']);self.assertNotIn('private-issuer',self.owner.path.read_text())

    def test_csv_private_snapshot_is_pinned_to_current_execution_scan_and_tenant(self):
        self.owner.find('outputs/north.csv',binding=self.binding)
        stored={**self.public,'size_bytes':1024,'scanned_depth':'deep','scanned_at':'now','data_updated_at':None}
        with patch.object(scene.raster.subprocess,'run',return_value=SimpleNamespace(returncode=0,stdout=json.dumps(stored),stderr=''))as query:
            self.assertEqual(self.owner.snapshot(12),stored)
        statement=query.call_args.kwargs['input']
        for text in('BEGIN READ ONLY','s.parent_execution_id=e.execution_id',"s.actor_principal_id=:'principal'","i.tenant_id=:'tenant'", "i.fingerprint=:'fingerprint'"):
            self.assertIn(text,statement)
        self.owner.cleanup()

    def test_source_snapshot_binds_preparer_while_outputs_keep_business_actor(self):
        public={**self.public,'id':13,'name':'north.tif','full_name':'north.tif'}
        self.owner.items[13]=public
        stored={**public,'size_bytes':4096}
        with patch.object(self.owner,'find',return_value=public),patch.object(self.owner,'query',return_value=stored)as query:
            self.assertEqual(self.owner.snapshot(13),stored)
        statement,values,_=query.call_args.args
        self.assertEqual((values['principal'],values['scan']),(self.owner.grants.operator,self.owner.source_scan))
        self.assertNotEqual(values['principal'],self.owner.identity['principal_id'])
        self.assertIn('s.parent_execution_id IS NULL',statement)
        self.assertIn("s.actor_principal_id=:'principal'",statement)
        self.assertIn("s.triggered_by=:'principal'",statement)
        self.assertNotIn('JOIN common.task_executions e',statement)
        self.owner.cleanup()

    def test_unknown_grant_write_outcome_remains_journalled(self):
        original=self.issuer.request.side_effect
        def lost(method,path,expected,body=None):
            result=original(method,path,expected,body)
            if method=='POST'and path.endswith('/access_grants'):raise TimeoutError('lost response')
            return result
        self.issuer.request.side_effect=lost
        with self.assertRaises(TimeoutError):self.owner.find('outputs/north.csv',binding=self.binding)
        self.assertTrue(self.owner.report['pending']);self.owner.cleanup();self.assertFalse(self.state['visible'])
