import copy
import hashlib
import importlib
import json
import tempfile
from pathlib import Path
from types import SimpleNamespace
import unittest
import uuid
from unittest.mock import patch
m = importlib.import_module('scripts.test.raster-sieve-protected-persistence-online')

def artifact(name):
    spec=m.fixture.artifact_expectation(name)
    return {**{k:spec[k] for k in ('width','height','band_count','source_crs','extent_srid','transform')},'artifact_type':'raster','format':'tiff','profile':'cog','size_bytes':256,'bands':[{'dtype':'Float64','nodata_is_nan':True,'nodata':None}]}

def target_item(name,identifier):
    spec=m.fixture.artifact_expectation(name)
    return {'id':identifier,'full_name':'outputs/'+name,'item_type':'file','size_bytes':256,'fingerprint':name,'scanned_at':'now','attributes':{
        'item':{'format':'tiff'},'type_info':{'media':{'width':spec['width'],'height':spec['height']}},
        'format_info':{'tiff':{'profile':'cog','is_cloud_optimized':True,'cog_check_level':'heuristic','is_tiled':True,'has_overviews':True}},
        'capabilities':{'spatial':{'srid':spec['extent_srid'],'extent':spec['extent']}} if spec['extent_srid'] else {}}}

def physical(repository,env,action,names,timeout):
    assert 0<timeout<=600
    if action=='cleanup-oracle':return {'oracle_cache_removed':True}
    sources={name:{'size_bytes':4096,'sha256':hashlib.sha256(name.encode()).hexdigest()} for name in m.fixture.SOURCES}
    flags=('cog_valid','has_overviews','full_pixel_and_mask_verification','independent_bfs_oracle_verified','original_coverage_verified','protected_classes_verified','donor_threshold_verified','nearest_overviews_verified','frozen_components_verified','legal_zero_verified')
    targets={name:{'size_bytes':256,'sha256':hashlib.sha256(name.encode()).hexdigest(),'overview_count':3,**{k:True for k in flags}} for name in names}
    result={'sources':sources,'targets':targets,'runtime_workspaces_removed':True,'full_source_pixels_verified':True,
        'oracle_cache':{'scope_sha256':'a'*64,'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)},
        'archived_expectations':{n+'.npy':{'path':'sieve-protected-expectations/'+n+'.npy','size_bytes':m.fixture.case_shape(n)[0]*m.fixture.case_shape(n)[1]*8+128,'sha256':hashlib.sha256(n.encode()).hexdigest()} for n in m.fixture.TARGETS},
        'archived_sources':{n:{'path':'sieve-protected-sources/'+n,**v} for n,v in sources.items()},
        'archived_cogs':{n:{'path':'sieve-protected-cogs/'+n,**{k:v[k] for k in ('size_bytes','sha256')}} for n,v in targets.items()},
        'comparisons':[list(pair) for pair in m.fixture.COMPARISONS if all(n in names for n in pair)]}
    if action=='prepare-oracle':result.update(scope_sha256='a'*64,anchor_sha256='b'*64,expected_array_count=16,cache_bytes=m.fixture.expected_cache_bytes()+16*128)
    return result

def diagnostic(repository, env, identifier, problem, language, timeout):
    return {'execution_id': identifier, 'task': 'sieve', 'problem': problem, 'language': language,
            'exact_cause': m.MESSAGES[problem][language == 'en'], 'source': 'develop_runtime_log'}

class Client:
    timeout = 30
    mutation = None
    added_scan = False
    added_edge = False
    rewrite = False
    scan_fault = False
    def __init__(self):
        self.scans, self.executions, self.scan_parents = [], {}, {}
        self.items = {name: {'id': i+10, 'full_name': name, 'item_type': 'file', 'size_bytes': 4096, 'fingerprint': name, 'scanned_at': 'now'} for i,name in enumerate(m.fixture.SOURCES)}
    def locators(self): return {n: f'addp://engine/3/path/{n}?type=file&item_id={self.items[n]["id"]}' for n in m.fixture.SOURCES}
    def request(self, method, path, expected, body=None, headers=None):
        if path.endswith('/auth/context'):
            data = {'principal': {'id':33,'type':'user'}, 'context': {'type':'tenant','tenant_id':'2'}, 'token': {'type':'first_party_access_token'},
                    'authorization': {'role_assignments': [{'role_key':'raster-consumer','permissions':sorted(m.raster.EDITOR_PERMISSIONS)}]}}
        elif path.endswith('/scan/run/manual'):
            assert body['targets'] == [f'addp://engine/3/path/{n}?type=file' for n in m.fixture.SOURCES]
            self.scans.append('source-scan'); self.scan_parents['source-scan'] = None; data = {'execution_id':'source-scan'}
        elif '/meta/executions/' in path:
            identifier = path.rsplit('/',1)[-1]
            data = {'execution_id':identifier,'parent_execution_id':self.scan_parents[identifier],'module':'meta','task_type':'scan','status':'success','tenant_id':2,'triggered_by':33,'scan_context':{'engine_id':4 if self.scan_fault else 3}}
        elif path.endswith('/workflow-engines'): data = [{'id':7,'engine_type':'geopython_workflow','connection_status':'online'}]
        elif path.endswith('/engines/3/items'): data = list(self.items.values())
        elif '/meta/items/' in path: data = next(copy.deepcopy(v) for v in self.items.values() if v['id'] == int(path.rsplit('/',1)[-1]))
        elif '/meta/scan/runs?' in path: data = {'total':len(self.scans), 'items':[{'execution_id':n} for n in self.scans]}
        elif '/meta/lineage/graph?' in path:
            data = {'truncated':False,'nodes':[], 'edges':[{'status':'active','relation_kind':'derive','execution_id':'bad'}] if self.added_edge and len(self.executions)>16 else []}
        elif path == '/api/v1/develop/executions' and method == 'POST':
            dag = body['content']['workflow_definition']; target_name = dag['tasks'][-1]['params']['target_name']; success = target_name in m.fixture.CASES
            identifier = str(uuid.UUID(int=len(self.executions)+1)); language = headers['Accept-Language']
            data = {'execution_id':identifier,'tenant_id':2,'module':'develop','task_type':'workflow','status':'success' if success else 'failed','triggered_by':33,
                    'execution_config':{'engine_id':7,'display_language':language,'content':{'workflow_definition':copy.deepcopy(dag)}}, 'error_msg':None if success else 'frozen-'+language,
                    'error_details':{} if success else {'category':'execution_failed'}, 'outputs':{}, 'metadata':{'result':{}}}
            if success:
                target = 'addp://engine/3/path/outputs/'+target_name+'?type=file'; scan = 'scan-'+str(len(self.executions)+1)
                wanted = m.expected_lineage(self.locators(), target_name, target)
                data['outputs'] = {'save':{'resource':{'locator':target,'type':'file','write_mode':'create'}}}
                data['metadata'] = {'lineage_facts':{**wanted,'operations':None}, 'result':{'final_result':self.preview(target_name),
                    'meta_scan_runs':[{'status':'submitted','target_locator':target,'execution_id':scan}]}}
                self.scans.append(scan); self.scan_parents[scan] = identifier
                self.items['outputs/'+target_name]=target_item(target_name,1000+len(self.executions))
            elif self.added_scan: self.scans.append('bad')
            if self.mutation: self.mutation(data)
            self.executions[identifier] = copy.deepcopy(data); data = {'execution_id':identifier}
        elif '/develop/executions/' in path or '/monitor/executions/' in path:
            data = copy.deepcopy(self.executions[path.rsplit('/',1)[-1]])
            if self.rewrite and headers and data['status']=='failed': data['error_msg']='rewritten'
        else: raise AssertionError((method,path,body))
        return SimpleNamespace(payload=copy.deepcopy(data),raw=json.dumps(data).encode(),status=expected[0])
    def preview(self,name):return artifact(name)

class ProtectedSieveWorkflowTest(unittest.TestCase):
    def setUp(self):
        self.work=tempfile.TemporaryDirectory();self.addCleanup(self.work.cleanup);self.root=Path(self.work.name)
        self.env={'ONLINE_SUITE':m.SUITE,'ADDP_ONLINE_TEST_TENANT_ID':'2','ADDP_ONLINE_SIEVE_PROTECTED_NFS_ENGINE_ID':'3',
                  'ADDP_ONLINE_TEST_TIMEOUT_SECONDS':'600','ADDP_ONLINE_TEST_RUN_ID':'sieve-protected-123-1','ADDP_ONLINE_ARTIFACT_DIR':str(self.root/'evidence')}
        self.relations=[]
    def run_case(self, client=None, worker=physical, diagnostic_runner=diagnostic):
        client=client or Client()
        def reader(env, identifier, tenant, principal, dag):
            assert tenant==2 and principal==33 and identifier in client.executions
            name=dag['tasks'][-1]['params']['target_name']; target='addp://engine/3/path/outputs/'+name+'?type=file'
            return m.expected_lineage(client.locators(), name, target)
        def relation(client, source, target, execution, timeout):
            self.relations.append((source,target,execution));return {'source':source,'target':target,'execution_id':execution}
        with patch.object(m.raster,'wait_lineage',side_effect=relation):
            return m.run_scenario(self.root,self.env,client,worker,diagnostic_runner,reader)
    def test_full_matrix_16_success_52_failures_968_checks_and_cleanup(self):
        client=Client();calls=[]
        def worker(*args): calls.append((args[2],list(args[3]))); return physical(*args)
        report=self.run_case(client,worker)
        self.assertEqual((len(report['cases']),len(report['failed_parameters']),report['full_target_checks'],report['physical_verification_calls']),(16,52,968,69))
        self.assertEqual(sum(len(names) for action,names in calls if action=='verify'),968)
        self.assertEqual(len(self.relations),16);self.assertTrue(report['oracle_cache_removed'])
        self.assertEqual(report['source_scan_identity']['parent_execution_id'],None)
        self.assertTrue(all(c['scan_identity']['parent_execution_id']==c['execution_id'] for c in report['cases']))
        self.assertEqual({(x['problem'],x['language']) for x in report['failed_parameters']},{(p,l) for p in m.FAILURES for l in ('zh-cn','en')})
        self.assertEqual(len(list((self.root/'evidence/sieve-protected-lineage').iterdir())),16)
        offset=0
        for case in report['cases']:
            count=len(m.case_inputs(case['name']));observed=self.relations[offset:offset+count];offset+=count
            self.assertEqual([r[0] for r in observed],[client.items[n]['id'] for n in m.case_inputs(case['name'])])
            self.assertTrue(all(r[1:]==(case['target_item_id'],case['execution_id']) for r in observed))
    def test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection(self):
        wanted=m.expected_lineage({n:n for n in m.fixture.SOURCES},'protected-four.cog.tif','target')
        for public in (False,True):
            good={**wanted,'operations':None} if public else wanted
            m.lineage.validate(good,wanted,public=public)
            for fault in ('role','missing','duplicate','operator','projection'):
                bad=copy.deepcopy(good)
                if fault=='role': bad['inputs'][0]['locator']='foreign'
                elif fault=='missing': bad['inputs'].pop()
                elif fault=='duplicate': bad['inputs'].append(bad['inputs'][0])
                elif fault=='operator': bad['operations']=[]
                else: bad['operations']=wanted['operations'] if public else None
                with self.subTest(public=public,fault=fault),self.assertRaises(m.SuiteError):m.lineage.validate(bad,wanted,public=public)
    def test_strict_physical_counts_sizes_original_archives_and_cleanup_cannot_weaken(self):
        good=physical(None,None,'verify',['protected-four.cog.tif'],600)
        m.check_physical(good,good['sources'],{},'a'*64,['protected-four.cog.tif'])
        for fault in ('count','bool-count','source','cleanup','flag','size','bool-size','archive','previous','anchor','source-pixels'):
            bad=copy.deepcopy(good);previous={}
            if fault=='count':bad['oracle_cache']['full_target_checks']=0
            elif fault=='bool-count':bad['oracle_cache']['full_target_checks']=True
            elif fault=='source':bad['sources']={}
            elif fault=='cleanup':bad['runtime_workspaces_removed']=False
            elif fault=='source-pixels':bad['full_source_pixels_verified']=False
            elif fault=='flag':bad['targets']['protected-four.cog.tif']['protected_classes_verified']=1
            elif fault=='size':bad['targets']['protected-four.cog.tif']['size_bytes']=0
            elif fault=='bool-size':bad['targets']['protected-four.cog.tif']['size_bytes']=True
            elif fault=='archive':bad['archived_cogs']={}
            elif fault=='anchor':bad['oracle_cache']['scope_sha256']='c'*64
            else:previous={'protected-four.cog.tif':{**good['targets']['protected-four.cog.tif'],'size_bytes':257}}
            with self.subTest(fault=fault),self.assertRaises(m.SuiteError):m.check_physical(bad,good['sources'],previous,'a'*64,['protected-four.cog.tif'])
    def test_failures_preserve_primary_and_cleanup_error(self):
        actions=[]
        def broken(repo,env,action,names,timeout):
            actions.append(action)
            if action=='prepare-oracle':raise m.SuiteError('first oracle failed')
            raise m.SuiteError('cleanup also failed')
        with self.assertRaisesRegex(m.SuiteError,'first oracle failed; oracle cleanup also failed'):self.run_case(worker=broken)
        self.assertEqual(actions,['prepare-oracle','cleanup-oracle'])
    def test_exact_failure_stage_language_scan_lineage_and_budget_are_enforced(self):
        for fault in ('diagnostic','query','scan','edge','identity','parent','budget','cleanup'):
            with self.subTest(fault=fault),tempfile.TemporaryDirectory() as directory:
                self.env['ADDP_ONLINE_ARTIFACT_DIR']=directory;client=Client();diag=diagnostic;worker=physical;self.env['ADDP_ONLINE_TEST_TIMEOUT_SECONDS']='600'
                if fault=='diagnostic':
                    def diag(*args):return {**diagnostic(*args),'task':'wrong'}
                elif fault=='query':client.rewrite=True
                elif fault=='scan':client.added_scan=True
                elif fault=='edge':client.added_edge=True
                elif fault=='identity':client.mutation=lambda value:value.update(triggered_by=34)
                elif fault=='parent':client.scan_fault=True
                elif fault=='budget':self.env['ADDP_ONLINE_TEST_TIMEOUT_SECONDS']='601'
                else:
                    def worker(*args):return {'oracle_cache_removed':False} if args[2]=='cleanup-oracle' else physical(*args)
                expected = {'diagnostic':'wrong exact failure', 'query':'query language rewrote', 'scan':'failed execution added scan', 'edge':'failed execution added derived', 'identity':'execution owner', 'parent':'scan identity', 'budget':'exactly 600', 'cleanup':'oracle cleanup proof'}[fault]
                with self.assertRaisesRegex(m.SuiteError,expected):self.run_case(client,worker,diag)

    def test_incomplete_source_graph_is_not_silently_accepted(self):
        client=Client()
        with patch.object(client,'request',return_value=SimpleNamespace(payload={'edges':[],'truncated':None})):
            with self.assertRaisesRegex(m.SuiteError,'source graph is incomplete'):m.source_edges(client,10)

class FrozenExpectedReceiptTest(unittest.TestCase):
    def test_verified_expected_hash_cannot_adapt_to_changed_archive(self):
        good=physical(None,{},'verify',['protected-four.cog.tif'],600)
        changed=copy.deepcopy(good);changed['archived_expectations']['protected-four.cog.tif.npy']['sha256']='f'*64
        with self.assertRaisesRegex(m.SuiteError,'frozen independent array archive changed'):
            m.check_physical(changed,good['sources'],{},'a'*64,['protected-four.cog.tif'],good['archived_expectations'])


class HostedJSONBoundaryTest(unittest.TestCase):
    def test_hosted_object_case_survives_json_and_native_float_is_not_dispatched(self):
        sources={n:n for n in m.fixture.SOURCES}
        dag=m.definition(sources,'parent','protected-four.cog.tif',invalid='protected_item_object')
        body=json.loads(json.dumps(dag));params=next(t['params']for t in body['tasks']if t['id']=='sieve')
        self.assertEqual(params['protected_values'],[{}]);self.assertEqual(len(m.FAILURES),26)
        self.assertNotIn('protected_item_integral_float',m.FAILURES)
        with self.assertRaisesRegex(m.SuiteError,'unknown exact protected sieve case'):
            m.definition(sources,'parent','protected-four.cog.tif',invalid='protected_item_integral_float')
        dag=m.definition(sources,'parent','protected-four.cog.tif',invalid='connectivity_fractional')
        self.assertEqual(next(t['params']for t in dag['tasks']if t['id']=='sieve')['connectivity'],4.5)
