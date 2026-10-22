import copy
import importlib
from pathlib import Path
from types import SimpleNamespace
import unittest

m=importlib.import_module('scripts.test.raster-thin-persistence-online')
ENV={'ADDP_ONLINE_TEST_TENANT_ID':'2','ADDP_ONLINE_THIN_NFS_ENGINE_ID':'3','ADDP_ONLINE_TEST_RUN_ID':'thin-123-1'}


def physical(repository,env,names):
    return {'runtime_workspaces_removed':True,'targets':{name:{'size_bytes':2048,'sha256':name,'cog_valid':True,'has_overviews':True,'full_pixel_and_mask_verification':True,'exact_integer_pixels_verified':True,'binary_skeleton_verified':True} for name in names}}


def diagnostic(repository,env,identifier,problem,timeout):
    return {'execution_id':identifier,'task':'thin','cause':'invalid_'+problem,'source':'develop_runtime_log'}


def target_item(name,identifier):
    expected=m.fixture.artifact_expectation()
    return {'id':identifier,'item_type':'file','full_name':'outputs/'+name,'fingerprint':name,'size_bytes':2048,'scanned_at':'2026-10-19T00:00:00Z',
        'attributes':{'item':{'format':'tiff'},'type_info':{'media':{'width':m.fixture.WIDTH,'height':m.fixture.HEIGHT}},
            'format_info':{'tiff':{'profile':'cog','is_tiled':True,'has_overviews':True}},
            'capabilities':{'spatial':{'srid':4326,'extent':expected['extent']}}}}


class Client:
    actor=33
    corrupt_size=False
    failure_publishes=False
    failure_scans=False
    def __init__(self):
        self.sources={name:{'id':i+10,'item_type':'file','full_name':name,'size_bytes':1024,'fingerprint':name} for i,name in enumerate(m.fixture.SOURCES)}
        self.targets={};self.executions={};self.scans=['source-scan'];self.calls=[]
    def request(self,method,path,expected,body=None):
        self.calls.append((method,path,body))
        if path.endswith('/auth/context'):
            data={'principal':{'id':33,'type':'user'},'context':{'type':'tenant','tenant_id':'2'},
                'token':{'type':'first_party_access_token'},'authorization':{'role_assignments':[{'role_key':'raster-consumer','permissions':sorted(m.raster.EDITOR_PERMISSIONS)}]}}
        elif path.endswith('/scan/run/manual'):
            assert body['targets']==['addp://engine/3/path/'+name+'?type=file' for name in m.fixture.SOURCES]
            data={'execution_id':'source-scan'}
        elif '/meta/executions/' in path:
            scan=path.rsplit('/',1)[-1];data={'execution_id':scan,'parent_execution_id':'exec-'+scan.removeprefix('scan-'),
                'module':'meta','task_type':'scan','status':'success','tenant_id':2,'triggered_by':self.actor,'scan_context':{'engine_id':3}}

        elif path.endswith('/workflow-engines'):data=[{'id':7,'engine_type':'geopython_workflow','connection_status':'online'}]
        elif path=='/api/v1/develop/executions' and method=='POST':
            tasks=body['content']['workflow_definition']['tasks'];name=tasks[-1]['params']['target_name'];identifier='exec-'+name
            if tasks[-1]['params'].get('write_mode')!='create' or 'mode' in tasks[-1]['params']:
                raise AssertionError('raster_save requires the formal write_mode parameter')
            if name.startswith('failed-'):
                self.executions[identifier]={'status':'failed','outputs':{'save':{}} if self.failure_publishes else {},'metadata':{'result':{}},'error_details':{'category':'execution_failed'}}
                if self.failure_scans:self.scans.append('invalid-scan')
            else:
                target='addp://engine/3/path/outputs/'+name+'?type=file';scan='scan-'+name;self.scans.append(scan)
                facts={'schema_version':'addp.lineage-facts/v1','inputs':[{'locator':t['params']['locator']} for t in tasks if 'locator' in t['params']],
                    'outputs':[{'locator':target,'write_mode':'create'}]}
                expectation=m.fixture.artifact_expectation()
                artifact={k:v for k,v in expectation.items() if k in {'width','height','band_count','source_crs','extent_srid','transform'}}
                artifact.update(artifact_type='raster',format='tiff',profile='cog',size_bytes=2048,
                    bands=[{'dtype':'Float64','nodata_is_nan':True,'nodata':None}])
                self.executions[identifier]={'status':'success','outputs':{'save':{'resource':{'locator':target,'type':'file','write_mode':'create'}}},
                    'metadata':{'lineage_facts':facts,'result':{'final_result':artifact,'meta_scan_runs':[{'status':'submitted','target_locator':target,'execution_id':scan}]}}}
                identifier_item=20+len(self.targets);self.targets[identifier_item]=target_item(name,identifier_item)
                if self.corrupt_size:self.targets[identifier_item]['size_bytes']=2049
            data={'execution_id':identifier}
        elif '/develop/executions/' in path:data=self.executions[path.rsplit('/',1)[-1]]
        elif '/monitor/executions/' in path:data={'module':'develop',**self.executions[path.rsplit('/',1)[-1]]}
        elif path.endswith('/engines/3/items'):data=[*self.sources.values(),*self.targets.values()]
        elif '/meta/items/' in path:
            identifier=int(path.rsplit('/',1)[-1]);data=next((s for s in self.sources.values() if s['id']==identifier),self.targets.get(identifier))
        elif '/meta/lineage/graph?' in path:
            identifier=int(path.split('item_id=')[1].split('&')[0]);edges=[]
            for target_id,target in self.targets.items():
                name=target['full_name'].split('/')[-1]
                for source in self.sources.values():
                    if identifier not in (source['id'],target_id):continue
                    edges.append({'source':{'item_id':source['id']},'target':{'item_id':target_id},'status':'active','relation_kind':'derive',
                                  'evidence':{'execution_id':'exec-'+name}})
            data={'edges':edges}
        elif '/meta/scan/runs?' in path:data={'total':len(self.scans),'items':[{'execution_id':s} for s in self.scans]}
        else:raise AssertionError((method,path,body))
        return SimpleNamespace(payload=copy.deepcopy(data))


class ThinPersistenceTest(unittest.TestCase):
    def test_three_exact_dags_reclassification_and_invalid_parameters_use_canonical_save(self):
        sources={n:n for n in m.fixture.SOURCES}
        canonical=m.raster.workflow('source',3,'b1','create')['tasks'][-1]['params']
        for name in m.fixture.TARGETS:
            tasks=m.definition(sources,'parent',name)['tasks']
            self.assertEqual(set(tasks[-1]['params']),set(canonical))
            self.assertEqual(tasks[-1]['params']['write_mode'],'create')
            self.assertEqual(tasks[-2]['operator'],'raster_thin')
            self.assertEqual(set(tasks[-2]['params']),{'input_raster','band','foreground_value'})
            self.assertEqual(tasks[-2]['params']['band'],m.fixture.CASES[name])
            self.assertEqual(len(tasks),4 if name.startswith('reclassified') else 3)
        for fault in m.FAILURES:
            params=m.definition(sources,'parent','thin.cog.tif',invalid=fault)['tasks'][-2]['params']
            self.assertEqual(params['band'],{'band':99,'fractional':4,'overflow':5,'foreground':2}[fault])
            self.assertEqual(params['foreground_value'],0.5 if fault=='foreground' else 0)
        for name in ('../outside.tif','unknown.tif'):
            with self.assertRaises(m.SuiteError):m.definition(sources,'parent',name)

    def test_complete_scene_preserves_twelve_permissions_three_exact_scans_and_current_source_lineage(self):
        client=Client();report=m.run_scenario(Path('.'),ENV,client,physical,diagnostic)
        self.assertEqual(report['result'],'passed');self.assertEqual([len(c['lineage']) for c in report['cases']],[1]*3)
        self.assertEqual(len(client.scans),4);self.assertEqual(len(report['failed_parameters']),4)
        self.assertEqual(len(report['identity']['permissions_verified']),12)
        self.assertEqual(len([c for c in client.calls if c[0]=='POST' and c[1].endswith('/scan/run/manual')]),1)

    def test_strict_sizes_safe_scan_identity_and_failure_publication_are_fail_closed(self):
        for attr,value in [('corrupt_size',True),('actor',True),('actor','33'),('actor',34),('failure_publishes',True),('failure_scans',True)]:
            client=Client();setattr(client,attr,value)
            with self.subTest(attr=attr,value=value),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,client,physical,diagnostic)

    def test_physical_prior_file_change_missing_workspace_and_incomplete_content_are_rejected(self):
        for kind in ['prior','workspace','full_pixel_and_mask_verification','cog_valid','has_overviews','exact_integer_pixels_verified','binary_skeleton_verified']:
            def verify(repository,env,names):
                result=physical(repository,env,names)
                if kind=='workspace':result.pop('runtime_workspaces_removed')
                elif kind=='prior':
                    if len(names)>1:result['targets'][names[0]]['sha256']='changed'
                else:result['targets'][names[0]][kind]=False
                return result
            with self.subTest(kind=kind),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Client(),verify,diagnostic)

    def test_artifact_physical_and_meta_sizes_remain_strict_positive_equal_integers(self):
        for position in range(3):
            for size in (True,0,-1,'2048',2049):
                entries=[{'size_bytes':2048} for _ in range(3)];entries[position]['size_bytes']=size
                with self.subTest(position=position,size=size),self.assertRaises(m.SuiteError):m.raster.strict_sizes(*entries,'clean.tif')

    def test_original_object_output_contract_remains_strict(self):
        client=Client();m.run_scenario(Path('.'),ENV,client,physical,diagnostic)
        execution=client.executions['exec-thin.cog.tif']
        target=execution['outputs']['save']['resource']['locator']
        sources=[i['locator'] for i in execution['metadata']['lineage_facts']['inputs']]
        with self.assertRaisesRegex(m.SuiteError,'wrong locator/type'):
            m.raster.validate_success(execution,sources,target,'create',m.fixture.artifact_expectation())

    def test_diagnostic_reuses_sanitizer_with_suite_specific_filename(self):
        self.assertTrue(issubclass(m.DiagnosticClient,m.rasterize.DiagnosticClient))
        self.assertEqual(m.DiagnosticClient.diagnostic_name,'thin-submission-error.json')

    def test_exact_scan_parent_engine_tenant_and_principal_are_required(self):
        for field,value in [('execution_id','foreign'),('parent_execution_id','foreign'),('tenant_id',True),('tenant_id',3),
                            ('triggered_by',34),('engine_id',True),('engine_id',4),('module','develop'),('task_type','other')]:
            class Changed(Client):
                def request(self,method,path,expected,body=None):
                    response=super().request(method,path,expected,body)
                    if '/meta/executions/' in path and not path.endswith('/source-scan'):
                        if field=='engine_id':response.payload['scan_context']['engine_id']=value
                        else:response.payload[field]=value
                    return response
            with self.subTest(field=field,value=value),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Changed(),physical,diagnostic)

    def test_failed_execution_cannot_add_active_derived_lineage(self):
        class Changed(Client):
            def request(self,method,path,expected,body=None):
                response=super().request(method,path,expected,body)
                if '/meta/lineage/graph?' in path and any(e['status']=='failed' for e in self.executions.values()):
                    response.payload['edges'].append({'status':'active','relation_kind':'derive','evidence':{'execution_id':'failed'}})
                return response
        with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Changed(),physical,diagnostic)

    def test_exact_failure_diagnostic_rejects_wrong_execution_task_cause_and_owner(self):
        import json,tempfile
        from unittest.mock import patch
        message='[DevExecutor] 工作流引擎调用失败: execution_id=exec-123 err=任务 thin 执行失败: 栅格细化必须显式选择有效的一基数据波段'
        original={'module_name':'develop','role':'backend','message':message}
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);path=root/'logs/runtime/develop/backend/test.jsonl';path.parent.mkdir(parents=True)
            path.write_text(json.dumps(original)+'\n')
            self.assertEqual(m.invalid_diagnostic(root,{},'exec-123','band',1)['task'],'thin')
            for field,value in [('module_name','meta'),('role','worker'),('message',message.replace('exec-123','exec-1234')),
                                ('message',message.replace('任务 thin','任务 save')),('message',message.replace('栅格细化','其他计算'))]:
                event={**original,field:value};path.write_text(json.dumps(event)+'\n')
                with self.subTest(field=field,value=value),patch.object(m.time,'sleep'),self.assertRaises(m.SuiteError):
                    m.invalid_diagnostic(root,{},'exec-123','band',.002)

    def test_safe_failure_category_is_required(self):
        class Wrong(Client):
            def request(self,method,path,expected,body=None):
                response=super().request(method,path,expected,body)
                if '/develop/executions/exec-failed-' in path:response.payload['error_details']={'category':'permission_denied'}
                return response
        with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Wrong(),physical,diagnostic)
