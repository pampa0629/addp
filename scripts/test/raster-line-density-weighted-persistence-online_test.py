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
m = importlib.import_module('scripts.test.raster-line-density-weighted-persistence-online')

def artifact(name):
    spec=m.fixture.artifact_expectation(name)
    return {**{k:spec[k] for k in ('width','height','band_count','source_crs','extent_srid','transform')},'artifact_type':'raster','format':'tiff','profile':'cog','size_bytes':256,'bands':[{'dtype':'Float64','nodata_is_nan':True,'nodata':None}]}

def target_item(name,identifier):
    spec=m.fixture.artifact_expectation(name)
    return {'id':identifier,'full_name':'outputs/'+name,'item_type':'file','size_bytes':256,'fingerprint':name,'scanned_at':'now','attributes':{
        'item':{'format':'tiff'},'type_info':{'media':{'width':spec['width'],'height':spec['height']}},
        'format_info':{'tiff':{'profile':'cog','is_cloud_optimized':True,'cog_check_level':'heuristic','is_tiled':True,'has_overviews':bool(m.fixture.overview_count(name))}},
        'capabilities':{'spatial':{'srid':spec['extent_srid'],'extent':spec['extent']}} if spec['extent_srid'] else {}}}
def expectations():return {n:[[0.]] for n in m.fixture.CASES}

def physical(repository,env,action,names,timeout):
 assert 0<timeout<=600
 if action=='cleanup-oracle':return {'oracle_cache_removed':True}
 sources={n:{'size_bytes':4096,'sha256':hashlib.sha256(n.encode()).hexdigest()}for n in m.fixture.SOURCES}
 flags=('cog_valid','full_pixel_and_mask_verification','independent_scalar_oracle_verified','template_mask_ignored_verified','nearest_overviews_verified','legal_zero_verified','line_length_and_full_circle_area_verified','line_density_units_verified','unnormalized_weights_verified')
 targets={n:{'size_bytes':256,'sha256':hashlib.sha256(n.encode()).hexdigest(),'overview_count':m.fixture.overview_count(n),'has_overviews':bool(m.fixture.overview_count(n)),'weight_field':m.fixture.CASES[n][3],'weight_field_parameter_omitted':m.fixture.CASES[n][3] is None,'unit':m.fixture.output_unit(n),**{k:True for k in flags}}for n in names}
 result={'actual_scale_controls_verified':[list(v)for v in m.fixture.SCALE_CONTROLS if all(n in names for n in v[:2])],'actual_control_pairs_verified':[list(pair)for pair in m.fixture.CONTROLS if all(n in names for n in pair)],'sources':sources,'targets':targets,'full_cog_checks':len(names),'runtime_workspaces_removed':True,'full_source_pixels_verified':True,'full_source_vectors_verified':True,'oracle_sha256':'a'*64,
  'archived_oracle':{m.fixture.ORACLE:{'path':'raster-line-density-weighted-oracle/'+m.fixture.ORACLE,'size_bytes':2048,'sha256':'a'*64}},
  'archived_sources':{n:{'path':'raster-line-density-weighted-sources/'+n,**v}for n,v in sources.items()},
  'archived_cogs':{n:{'path':'raster-line-density-weighted-cogs/'+n,**{k:v[k]for k in('size_bytes','sha256')}}for n,v in targets.items()}}
 if action=='oracle':result['expectations']=expectations()
 return result

def diagnostic(repository, env, identifier, problem, language, timeout):
    return {'execution_id': identifier, 'task': 'density', 'problem': problem, 'language': language,
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

class NewHostedWorkflowTest(unittest.TestCase):
    def setUp(self):
        self.work=tempfile.TemporaryDirectory();self.addCleanup(self.work.cleanup);self.root=Path(self.work.name)
        self.env={'ONLINE_SUITE':m.SUITE,'ADDP_ONLINE_TEST_TENANT_ID':'2','ADDP_ONLINE_RASTER_LINE_DENSITY_WEIGHTED_NFS_ENGINE_ID':'3',
                  'ADDP_ONLINE_TEST_TIMEOUT_SECONDS':'600','ADDP_ONLINE_TEST_RUN_ID':'raster-line-density-weighted-123-1','ADDP_ONLINE_ARTIFACT_DIR':str(self.root/'evidence')}
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
    def test_full_matrix_16_success_40_failures_776_checks_and_cleanup(self):
        client=Client();calls=[]
        def worker(*args): calls.append((args[2],list(args[3]))); return physical(*args)
        report=self.run_case(client,worker)
        self.assertEqual((len(report['cases']),len(report['failed_parameters']),report['full_target_checks'],report['physical_verification_calls']),(16,40,776,57))
        self.assertEqual(sum(len(names) for action,names in calls if action=='verify'),776)
        self.assertEqual(len(self.relations),32);self.assertTrue(report['oracle_cache_removed'])
        self.assertEqual(report['source_scan_identity']['parent_execution_id'],None)
        self.assertTrue(all(c['scan_identity']['parent_execution_id']==c['execution_id'] for c in report['cases']))
        self.assertEqual({(x['problem'],x['language']) for x in report['failed_parameters']},{(p,l) for p in m.FAILURES for l in ('zh-cn','en')})
        self.assertEqual(len(list((self.root/'evidence/raster-line-density-weighted-lineage').iterdir())),16)
        offset=0
        for case in report['cases']:
            count=len(m.case_inputs(case['name']));observed=self.relations[offset:offset+count];offset+=count
            self.assertEqual([r[0] for r in observed],[client.items[n]['id'] for n in m.case_inputs(case['name'])])
            self.assertTrue(all(r[1:]==(case['target_item_id'],case['execution_id']) for r in observed))
    def test_role_bound_lineage_rejects_wrong_roles_operations_and_public_projection(self):
        wanted=m.expected_lineage({n:n for n in m.fixture.SOURCES},'mixed.cog.tif','target')
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
        good=physical(None,None,'verify',['mixed.cog.tif'],600)
        m.check_physical(good,good['sources'],{},'a'*64,['mixed.cog.tif'])
        for fault in ('count','bool-count','source','cleanup','flag','size','bool-size','archive','previous','anchor'):
            bad=copy.deepcopy(good);previous={}
            if fault=='count':bad['full_cog_checks']=0
            elif fault=='bool-count':bad['full_cog_checks']=True
            elif fault=='source':bad['sources']={}
            elif fault=='cleanup':bad['runtime_workspaces_removed']=False
            elif fault=='flag':bad['targets']['mixed.cog.tif']['template_mask_ignored_verified']=1
            elif fault=='size':bad['targets']['mixed.cog.tif']['size_bytes']=0
            elif fault=='bool-size':bad['targets']['mixed.cog.tif']['size_bytes']=True
            elif fault=='archive':bad['archived_cogs']={}
            elif fault=='anchor':bad['oracle_sha256']='b'*64
            else:previous={'mixed.cog.tif':{**good['targets']['mixed.cog.tif'],'size_bytes':257}}
            with self.subTest(fault=fault),self.assertRaises(m.SuiteError):m.check_physical(bad,good['sources'],previous,'a'*64,['mixed.cog.tif'])
    def test_failures_preserve_primary_and_cleanup_error(self):
        actions=[]
        def broken(repo,env,action,names,timeout):
            actions.append(action)
            if action=='oracle':raise m.SuiteError('first oracle failed')
            raise m.SuiteError('cleanup also failed')
        with self.assertRaisesRegex(m.SuiteError,'first oracle failed; oracle cleanup also failed'):self.run_case(worker=broken)
        self.assertEqual(actions,['oracle','cleanup-oracle'])
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

    def test_all_source_and_target_meta_snapshots_and_two_inputs(self):
        self.run_case()
        evidence=self.root/'evidence'
        before=json.loads((evidence/'source-meta-before.json').read_bytes());after=json.loads((evidence/'source-meta-after.json').read_bytes())
        self.assertEqual(before,after);self.assertEqual(len(before),len(m.fixture.SOURCES))
        self.assertEqual(len(json.loads((evidence/'target-meta-after.json').read_bytes())),16)
        for name in m.fixture.CASES:self.assertTrue((evidence/('target-'+name+'-meta-before.json')).is_file())
        for kind in ('source','target'):
            with self.subTest(kind=kind),tempfile.TemporaryDirectory()as directory:
                self.env['ADDP_ONLINE_ARTIFACT_DIR']=directory;client=Client();original=client.request;changed=set()
                def request(method,path,expected,body=None,headers=None):
                    result=original(method,path,expected,body,headers)
                    if '/meta/items/'in path and len(client.executions)>16:
                        identifier=int(path.rsplit('/',1)[-1]);hit=identifier<1000 if kind=='source'else identifier>=1000
                        if hit:result.payload['scanned_at']='changed'
                    return result
                client.request=request
                with self.assertRaisesRegex(m.SuiteError,'metadata changed'):self.run_case(client)

    def test_fixed_case_dags_roles_and_complete_preview(self):
        sources={n:n for n in m.fixture.SOURCES}
        for name in m.fixture.CASES:
            dag=m.definition(sources,'parent',name);op=next(t for t in dag['tasks']if t['id']=='density')
            self.assertEqual(op['params']['radius_metres'],m.fixture.CASES[name][2])
            self.assertEqual(set(op['params']),{'input_gdf','reference_raster','radius_metres'}|({'weight_field'}if m.fixture.CASES[name][3]is not None else set()))
            self.assertEqual(op['operator'],'raster_line_density');self.assertEqual(dag['tasks'][-1]['params']['write_mode'],'create')
            self.assertEqual([v['port']for v in m.expected_lineage(sources,name,'target')['inputs']],['input.lines','input.reference'])
            good=Client().preview(name);m.raster.validate_raster_artifact(good,m.fixture.artifact_expectation(name))
            for key,value in(('width',0),('source_crs','EPSG:4326'),('bands',[])):
                bad=copy.deepcopy(good);bad[key]=value
                with self.assertRaises(m.SuiteError):m.raster.validate_raster_artifact(bad,m.fixture.artifact_expectation(name))

    def test_incomplete_source_graph_is_not_silently_accepted(self):
        client=Client()
        with patch.object(client,'request',return_value=SimpleNamespace(payload={'edges':[],'truncated':None})):
            with self.assertRaisesRegex(m.SuiteError,'source graph is incomplete'):m.source_edges(client,10)

    def test_all_fixed_failure_dags_have_two_actual_sources(self):
        sources={n:n for n in m.fixture.SOURCES}
        self.assertEqual(len(m.FAILURES),20)
        for problem in m.FAILURES:
            dag=m.definition(sources,'parent','mixed.cog.tif',invalid=problem)
            self.assertEqual(dag['tasks'][-1]['params']['target_name'],'failed-'+problem+'-zh-cn.cog.tif')
            self.assertEqual(len([t for t in dag['tasks']if t['operator']in('load','raster_load')]),2)

    def test_overview_and_template_isolation_proof_cannot_weaken(self):
        for name in ('mixed.cog.tif','foot-block.cog.tif'):
            good=physical(None,None,'verify',[name],600)
            for key,value in(('overview_count',True),('has_overviews',not bool(m.fixture.overview_count(name))),('template_mask_ignored_verified',False),('nearest_overviews_verified',False)):
                bad=copy.deepcopy(good);bad['targets'][name][key]=value
                with self.subTest(name=name,key=key),self.assertRaises(m.SuiteError):m.check_physical(bad,good['sources'],{},'a'*64,[name])

    def test_raw_terminal_inventory_and_sizes_are_strict(self):
        self.run_case()
        self.assertEqual(len(list((self.root/'evidence/line-density-weighted-terminal-responses').glob('*.json'))),112)
        for fault in ('artifact','physical','meta'):
            values=[{'size_bytes':256}for _ in range(3)];values[('artifact','physical','meta').index(fault)]['size_bytes']=True
            with self.subTest(fault=fault),self.assertRaises(m.SuiteError):m.raster.strict_sizes(*values)

    def test_actual_point_order_template_and_duplicate_controls_cannot_weaken(self):
        names=list(m.fixture.CASES);good=physical(None,None,'verify',names,600)
        for key in('actual_control_pairs_verified','actual_scale_controls_verified','line_length_and_full_circle_area_verified','line_density_units_verified','unnormalized_weights_verified'):
            bad=copy.deepcopy(good)
            if key.startswith('actual_'):bad[key]=[]
            else:bad['targets']['mixed.cog.tif'][key]=False
            with self.subTest(key=key),self.assertRaises(m.SuiteError):m.check_physical(bad,good['sources'],{},'a'*64,names)


    def test_all_nine_weight_field_failure_values_are_json_exact_and_not_silently_dropped(self):
        values={'field_null':None,'field_empty':'','field_blank':' ','field_bool':True,'field_number':1,'field_array':[],'field_object':{},'field_missing':'absent','field_geometry':'shape'}
        self.assertEqual(m.FIELD_FAILURES,values)
        for problem,wanted in values.items():
            dag=m.definition({n:n for n in m.fixture.SOURCES},'parent','mixed.cog.tif',invalid=problem)
            actual=dag['tasks'][-2]['params']['weight_field'];self.assertEqual(type(actual),type(wanted));self.assertEqual(actual,wanted);json.dumps(dag,allow_nan=False)
        omitted=m.definition({n:n for n in m.fixture.SOURCES},'parent','omitted.cog.tif')['tasks'][-2]['params']
        self.assertNotIn('weight_field',omitted)


    def test_all_twenty_failure_sources_keep_accurate_weight_geometry_and_crs_roles(self):
        self.assertEqual(len(m.FAILURES),20)
        for problem in m.FAILURES:
            dag=m.definition({n:n for n in m.fixture.SOURCES},'parent','mixed.cog.tif',invalid=problem)
            self.assertEqual(dag['tasks'][1]['id'],'lines')
            wanted=problem+'.gpkg'if problem not in m.FIELD_FAILURES and problem!='zero_radius'else'lines.gpkg'
            self.assertEqual(dag['tasks'][1]['params']['locator'],wanted)
            self.assertEqual(dag['tasks'][0]['params']['locator'],'template.tif')
            self.assertEqual(dag['tasks'][2]['operator'],'raster_line_density')
            if problem.startswith('zero_'):self.assertEqual(dag['tasks'][2]['params']['weight_field'],'zero'if problem=='zero_radius'else'mass')
            if problem=='zero_radius':self.assertEqual(dag['tasks'][2]['params']['radius_metres'],0)

    def test_both_doubled_and_retraced_actual_saved_controls_are_required(self):
        names=list(m.fixture.CASES);good=physical(None,None,'verify',names,600)
        self.assertEqual(good['actual_scale_controls_verified'],[list(v)for v in m.fixture.SCALE_CONTROLS])
        for value in ([],[list(m.fixture.SCALE_CONTROLS[0])],[[*m.fixture.SCALE_CONTROLS[0][:2],3],list(m.fixture.SCALE_CONTROLS[1])]):
            bad=copy.deepcopy(good);bad['actual_scale_controls_verified']=value
            with self.assertRaises(m.SuiteError):m.check_physical(bad,good['sources'],{},'a'*64,names)

    def test_weight_selector_units_omission_and_unnormalized_proof_cannot_weaken(self):
        for name in('omitted.cog.tif','mixed.cog.tif'):
            good=physical(None,None,'verify',[name],600);m.check_physical(good,good['sources'],{},'a'*64,[name])
            for key,value in(('weight_field','absent'),('weight_field_parameter_omitted',1 if name.startswith('omitted')else 0),('unit','wrong'),('unnormalized_weights_verified',False)):
                bad=copy.deepcopy(good);bad['targets'][name][key]=value
                with self.subTest(name=name,key=key),self.assertRaises(m.SuiteError):m.check_physical(bad,good['sources'],{},'a'*64,[name])
