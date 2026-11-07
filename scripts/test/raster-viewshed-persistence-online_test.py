import copy,hashlib,importlib,json,tempfile,uuid,urllib.parse
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
import unittest
scene=importlib.import_module('scripts.test.raster-viewshed-persistence-online');fixture=scene.fixture
FROZEN={n:{'artifact':fixture.artifact_expectation(n),'state_counts':{'0':1,'1':2,'2':3,'255':fixture.WIDTH*fixture.HEIGHT-6}}for n in fixture.TARGETS}
SOURCES={n:{'size_bytes':4096,'sha256':hashlib.sha256(n.encode()).hexdigest()}for n in fixture.SOURCES}
def raw_cog(name):return ('COG:'+name).encode()
def native_fact(name):
 return {'size_bytes':len(raw_cog(name)),'sha256':hashlib.sha256(raw_cog(name)).hexdigest(),'cog_valid':True,'has_overviews':True,'overview_count':3,'valid_pixels':6,'state_counts':copy.deepcopy(FROZEN[name]['state_counts']),
 'full_pixel_and_mask_verification':True,'independent_cell_clipping_verified':True,'nearest_overviews_verified':True}
def artifact(name):
 return {**fixture.artifact_expectation(name),'artifact_type':'raster','format':'tiff','profile':'cog','size_bytes':len(raw_cog(name)),
 'bands':[{'dtype':'Byte','nodata':255,'nodata_is_nan':False}]}
def private_attributes(name):
 exp=fixture.artifact_expectation(name)
 return {'item':{'format':'tiff'},'type_info':{'media':{'width':exp['width'],'height':exp['height']}},'format_info':{'tiff':{'profile':'cog','is_cloud_optimized':True,'cog_check_level':'heuristic','is_tiled':True,'has_overviews':True}},'capabilities':{'spatial':{'srid':exp['extent_srid'],'extent':exp['extent']}}}
def physical(repository,env,action,names,timeout):
 assert 0<timeout<=600
 if action=='cleanup-oracle':return {'oracle_cache_removed':True}
 targets={n:native_fact(n)for n in names};manifest={}
 for n in names:
  p=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'viewshed-cogs'/n
  if not p.exists():p.write_bytes(raw_cog(n))
  manifest[n]={'path':'viewshed-cogs/'+n,**fixture.digest(p)}
 result={'sources':copy.deepcopy(SOURCES),'targets':targets,'archived_cogs':manifest,'runtime_workspaces_removed':True,'oracle_cache':{'scope_sha256':'a'*64,'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}}
 if action=='prepare-oracle':result.update(expectations=copy.deepcopy(FROZEN),scope_sha256='a'*64,anchor_sha256='b'*64,expected_array_count=16)
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
            self.relationships.append({'source': {'item_id': self.items['flat.tif']['id']},
                                       'target': {'item_id': 9999}, 'evidence': {'execution_id': identifier},
                                       'status': 'active', 'relation_kind': 'derive'})
        elif self.fault == 'source-meta':
            self.items['flat.tif']['fingerprint'] = 'changed'
        elif self.fault == 'target-meta':
            self.items['outputs/flat.cog.tif']['fingerprint'] = 'changed'
        self.executions[identifier] = copy.deepcopy(execution)
        return {'execution_id': identifier}

    def request(self, method, path, expected, body=None, headers=None):
        if path.endswith('/auth/context'):
            data = {'principal': {'id': 33, 'type': 'user'}, 'context': {'type': 'tenant', 'tenant_id': '2'},
                    'token': {'type': 'first_party_access_token'}, 'authorization': {'role_assignments': [
                        {'role_key': 'raster-consumer', 'permissions': sorted(scene.raster.EDITOR_PERMISSIONS)}]}}
        elif path.endswith('/scan/run/manual'):
            assert body['targets'] == [f'addp://engine/3/path/{n}?type=file' for n in fixture.SOURCES]
            self.scans['source-scan'] = None
            data = {'execution_id': 'source-scan'}
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
            data = {'total': len(self.scans), 'items': [{'execution_id': n} for n in self.scans]}
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


class ViewshedWorkflowTest(unittest.TestCase):
    def run_case(self, fault=None, worker=physical, diagnostic_runner=diagnostic, budget='600'):
        root = Path(self.enterContext(tempfile.TemporaryDirectory()))
        env = {'ONLINE_SUITE': scene.SUITE, 'ADDP_ONLINE_TEST_RUN_ID': 'viewshed-123-1',
               'ADDP_ONLINE_TEST_TENANT_ID': '2', 'ADDP_ONLINE_VIEWSHED_NFS_ENGINE_ID': '3',
               'ADDP_ONLINE_TEST_TIMEOUT_SECONDS': budget, 'ADDP_ONLINE_ARTIFACT_DIR': str(root/'evidence')}
        client = Client(fault)
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

        return scene.run_scenario(root, env, client, worker, diagnostic_runner, reader,metadata_factory=Metadata)

    def test_complete_fixed_matrix_rereads_and_original_manifest(self):
        calls = []

        def tracked(*args):
            calls.append(tuple(args[3]))
            return physical(*args)

        result = self.run_case(worker=tracked)
        self.assertEqual([len(names) for names in calls], [0, *range(1, 17), *([16]*36),0])
        self.assertEqual((len(result['cases']), len(result['failed_parameters']),
                          result['full_target_checks'], result['physical_verification_calls']), (16, 36, 712, 53))
        self.assertEqual({(case['problem'], case['language']) for case in result['failed_parameters']},
                         {(problem, language) for problem in scene.FAILURES for language in ('zh-cn', 'en')})
        self.assertEqual(len({case['execution_id'] for case in result['cases']+result['failed_parameters']}), 52)
        self.assertEqual(len(result['archived_cogs']), 16)
        manifest = json.loads((self.evidence/'viewshed-cogs-manifest.json').read_bytes())
        self.assertEqual(manifest, result['archived_cogs'])
        for name, value in manifest.items():
            data = (self.evidence/value['path']).read_bytes()
            self.assertEqual(data,raw_cog(name))
            self.assertEqual({'size_bytes':len(data),'sha256':hashlib.sha256(data).hexdigest()}, {key: value[key] for key in ('size_bytes', 'sha256')})
        for case in result['cases']:
            self.assertEqual(case['meta_relation']['execution_id'], case['execution_id'])
            self.assertEqual(case['scan_identity']['parent_execution_id'], case['execution_id'])
            self.assertEqual(case['meta_size_bytes'], case['physical_size_bytes'])

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
                        report['sources']['flat.tif']['size_bytes'] += 1
                    elif fault == 'raw':
                        (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'viewshed-cogs'/names[0]).write_bytes(b'corrupt')
                    elif fault == 'size':
                        report['targets'][names[0]]['size_bytes'] = True
                    else:
                        report['runtime_workspaces_removed'] = False
                return report
            with self.subTest(fault=fault), self.assertRaises(scene.SuiteError):
                self.run_case(worker=broken)




class ViewshedMetadataTest(unittest.TestCase):
    def setUp(self):
        import os
        self.root=Path(self.enterContext(tempfile.TemporaryDirectory()));self.secret=self.root/'secret';self.secret.mkdir(mode=0o700)
        self.artifacts=self.root/'artifacts';self.artifacts.mkdir()
        self.env={'ONLINE_SUITE':scene.SUITE,'ADDP_ONLINE_TEST':'1','ADDP_ONLINE_TEST_RUN_ID':'viewshed-123-1','ADDP_ONLINE_TEST_TIMEOUT_SECONDS':'600',
            'GITHUB_RUN_ID':'123','GITHUB_RUN_ATTEMPT':'1','ADDP_ONLINE_SECRET_DIR':str(self.secret),'ADDP_ONLINE_ARTIFACT_DIR':str(self.artifacts),
            'ADDP_ONLINE_TEST_TENANT_ID':'2','GATEWAY_URL':'http://fixture','ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN':'private-issuer'}
        self.container={'Id':'a'*64,'State':{'Running':True,'StartedAt':'2026-11-07T00:00:00Z'},'Config':{
            'Labels':{'com.docker.compose.project':'addp-infra','com.docker.compose.service':'postgres','com.docker.compose.project.working_dir':str(Path(__file__).resolve().parents[2])},
            'Env':['POSTGRES_DB=addp_online','POSTGRES_USER=addp','POSTGRES_PASSWORD=private']}}
        helper=importlib.import_module('scripts.test.online-engine-registration_test').OnlineEngineRegistrationTest()
        self.issuer,self.client,self.facts,self.state=helper.metadata_clients(3,'flat.tif','nfs','media','tiff','single')
        self.issuer.timeout=30;self.client.timeout=30
        self.public={'id':12,'tenant_id':2,'engine_id':3,'node_id':4,'name':'flat.tif','full_name':'flat.tif','fingerprint':self.facts['item_fingerprint'],'item_type':'file','attributes':{'item':{'format':'tiff'}}}
        original=self.client.request.side_effect
        def request(method,path,expected,body=None):
            if path=='/api/v1/meta/engines/3/items':return SimpleNamespace(payload=[copy.deepcopy(self.public)]if self.state['visible']else[])
            return original(method,path,expected,body)
        self.client.request.side_effect=request
        self.enterContext(patch.dict(os.environ,self.env,clear=True))
        self.enterContext(patch.object(scene.lineage.BASE.HOSTED,'deployment_secret',return_value=self.secret))
        self.enterContext(patch.object(scene.lineage,'inspect',return_value=self.container))
        self.enterContext(patch.object(scene.support,'GatewayClient',return_value=self.issuer))
        receipt=self.secret/scene.lineage.RECEIPT
        receipt.write_text(json.dumps({'schema':'addp.raster-lineage-owner/v1','suite':scene.SUITE,'run_id':'viewshed-123-1',
            'container_id':self.container['Id'],'started_at':self.container['State']['StartedAt'],'postgres_user':'addp'}));receipt.chmod(0o600)
        self.owner=scene.ViewshedMetadata(self.env,self.client,3,{'tenant_id':'2','principal_id':'7'},str(uuid.UUID(int=9)),lambda:599)

    def test_owner_receipt_must_match_initial_isolated_postgres(self):
        path=self.secret/scene.lineage.RECEIPT;value=json.loads(path.read_text())
        value['container_id']='b'*64;path.write_text(json.dumps(value))
        with self.assertRaises(scene.SuiteError):scene.ViewshedMetadata(self.env,self.client,3,{'tenant_id':'2','principal_id':'7'},str(uuid.UUID(int=9)),lambda:599)

    def test_exact_nfs_grant_precedes_ordinary_listing_and_is_revoked(self):
        self.assertEqual(self.owner.find('flat.tif'),self.public)
        receipt,=self.owner.report['issued'];self.assertEqual(receipt['action'],'metadata_read')
        self.assertEqual(receipt['expiry_mode'],'at_time');self.assertEqual(receipt['recipient_id'],'7')
        for name in('other.tif','../flat.tif','outputs/flat.cog.tif'):
            with self.subTest(name=name),self.assertRaises(scene.SuiteError):self.owner.find(name)
        self.owner.cleanup();self.assertFalse(self.state['visible']);self.assertFalse(self.owner.report['pending'])
        self.assertNotIn('private-issuer',self.owner.path.read_text())

    def test_readonly_source_snapshot_is_fresh_and_pinned_to_scan_user_container(self):
        self.owner.find('flat.tif')
        stored={**self.public,'size_bytes':1024,'scanned_depth':'deep','scanned_at':'now','data_updated_at':None}
        with patch.object(scene.raster.subprocess,'run',return_value=SimpleNamespace(returncode=0,stdout=json.dumps(stored),stderr=''))as query:
            self.assertEqual(self.owner.snapshot(12),stored)
            stored['size_bytes']=2048;query.return_value.stdout=json.dumps(stored)
            self.assertEqual(self.owner.snapshot(12)['size_bytes'],2048)
        statement=query.call_args.kwargs['input']
        for text in('BEGIN READ ONLY',"s.actor_principal_id=:'principal'","i.fingerprint=:'fingerprint'",'s.parent_execution_id IS NULL',"i.created_at>=:'started_at'"):
            self.assertIn(text,statement)
        self.assertIn('addp_online',query.call_args.args[0]);self.owner.cleanup()

    def test_foreign_or_restarted_container_and_public_derived_leak_fail_closed(self):
        import os
        for changed in('run','container','restart','database','projection'):
            current=copy.deepcopy(self.container);env=dict(self.env)
            if changed=='run':env['ADDP_ONLINE_TEST_RUN_ID']='viewshed-124-1'
            if changed=='container':current['Id']='b'*64
            if changed=='restart':current['State']['StartedAt']='2026-11-07T01:00:00Z'
            if changed=='database':current['Config']['Env'][0]='POSTGRES_DB=addp_test'
            if changed=='projection':self.public['size_bytes']=1
            with self.subTest(changed=changed),patch.dict(os.environ,env,clear=True),patch.object(scene.lineage,'inspect',return_value=current),self.assertRaises(Exception):self.owner.find('flat.tif')
        self.public.pop('size_bytes',None);self.owner.cleanup()

    def test_current_execution_private_identity_and_snapshot_are_exact(self):
        identifier=str(uuid.UUID(int=11));dag=scene.definition({n:n for n in fixture.SOURCES},'parent','flat.cog.tif')
        expected={'execution_id':identifier,'tenant_id':2,'principal_id':7,'triggered_by':7,
            'module':'develop','task_type':'workflow','status':'failed','snapshot_matches':True}
        with patch.object(self.owner,'query',return_value=expected)as query:
            self.assertEqual(self.owner.execution(identifier,dag,9,'en','failed'),expected)
            statement,values,fields=query.call_args.args
            for text in ('BEGIN READ ONLY',"e.tenant_id=:'tenant'", "e.actor_principal_id=:'principal'",'snapshot_matches'):
                self.assertIn(text,statement)
            self.assertEqual(values['language'],'en');self.assertEqual(values['runtime'],9)
            for key,bad in (('tenant_id',3),('principal_id',True),('status','success'),('snapshot_matches',1)):
                query.return_value={**expected,key:bad}
                with self.subTest(key=key),self.assertRaises(scene.SuiteError):self.owner.execution(identifier,dag,9,'en','failed')

    def test_unknown_write_outcome_remains_journalled_for_cleanup(self):
        original=self.issuer.request.side_effect
        def lost(method,path,expected,body=None):
            result=original(method,path,expected,body)
            if method=='POST'and path.endswith('/access_grants'):raise TimeoutError('lost response')
            return result
        self.issuer.request.side_effect=lost
        with self.assertRaises(TimeoutError):self.owner.find('flat.tif')
        self.assertTrue(self.owner.report['pending']);self.owner.cleanup();self.assertFalse(self.state['visible'])
