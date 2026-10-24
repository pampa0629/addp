import copy
import importlib
from pathlib import Path
from types import SimpleNamespace
import unittest

m=importlib.import_module('scripts.test.raster-stream-to-vector-d8-persistence-online')
ENV={'ADDP_ONLINE_TEST_TENANT_ID':'2','ADDP_ONLINE_STREAM_TO_VECTOR_D8_NFS_ENGINE_ID':'3','ADDP_ONLINE_TEST_RUN_ID':'stream-to-vector-d8-123-1'}


def physical(repository,env,names):
    result={'runtime_workspaces_removed':True,'targets':{}}
    for name in names:
        empty=name in ('empty.gpkg','invalid.gpkg')
        result['targets'][name]={'size_bytes':2048,'sha256':name,'layer_name':name.removesuffix('.gpkg'),
            'schema':[list(v) for v in m.fixture.SCHEMA],'crs':'EPSG:'+str(m.fixture.epsg(name)),
            'feature_count':0 if empty else 1,'edge_count':0 if empty else 2,'vertex_count':0 if empty else 3,
            'skipped_terminal_link_ids':[2] if name in ('metre.gpkg','foot.gpkg','survey.gpkg') else [],
            **{key:True for key in m.REQUIRED_PHYSICAL}}
    return result


def diagnostic(repository,env,identifier,problem,language,timeout):
    return {'execution_id':identifier,'task':'lines','cause':'invalid_'+problem,'source':'develop_runtime_log','language':language}


def target_item(name,identifier):
    return {'id':identifier,'item_type':'file','full_name':'outputs/'+name,'fingerprint':name,'size_bytes':2048,'scanned_at':'2026-10-24T00:00:00Z',
        'attributes':{'item':{'layout':'single','data_type':'container','format':'geopackage'},
        'type_info':{'container':{'children':[{'name':name.removesuffix('.gpkg'),'child_kind':'layer','native':{'table':name.removesuffix('.gpkg')}}]}}}}


class Client:
    actor=33
    corrupt_size=False
    failure_publishes=False
    failure_scans=False
    def __init__(self):
        self.sources={name:{'id':i+10,'item_type':'file','full_name':name,'size_bytes':1024,'fingerprint':name} for i,name in enumerate(m.fixture.SOURCES)}
        self.targets={};self.executions={};self.scans=['source-scan'];self.calls=[];self.headers=[]
    def request(self,method,path,expected,body=None,headers=None):
        self.calls.append((method,path,body));self.headers.append((method,path,headers))
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
            if tasks[-1]['params'].get('mode')!='fail' or 'write_mode' in tasks[-1]['params']:raise AssertionError('generic save requires mode=fail')
            if name.startswith('failed-'):
                self.executions[identifier]={'status':'failed','outputs':{'save':{}} if self.failure_publishes else {},'metadata':{'result':{}},'error_details':{'category':'execution_failed'}}
                if self.failure_scans:self.scans.append('invalid-scan')
            else:
                target='addp://engine/3/path/outputs/'+name+'?type=file';scan='scan-'+name;self.scans.append(scan)
                facts={'schema_version':'addp.lineage-facts/v1','inputs':[{'locator':t['params']['locator']} for t in tasks if 'locator' in t['params']],
                    'outputs':[{'locator':target,'write_mode':'create'}]}
                artifact={'status':'success','output_type':'file','path':name}
                self.executions[identifier]={'status':'success','outputs':{'save':{'resource':{'locator':target,'type':'file','write_mode':'create'}}},
                    'metadata':{'lineage_facts':facts,'result':{'final_result':artifact,'meta_scan_runs':[{'status':'submitted','target_locator':target,'execution_id':scan}]}}}
                identifier_item=100+len(self.targets);self.targets[identifier_item]=target_item(name,identifier_item)
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
                    inputs=[fact['locator'] for fact in self.executions['exec-'+name]['metadata']['lineage_facts']['inputs']]
                    if not any('/path/'+source['full_name']+'?' in locator for locator in inputs):continue
                    if identifier not in (source['id'],target_id):continue
                    edges.append({'source':{'item_id':source['id']},'target':{'item_id':target_id},'status':'active','relation_kind':'derive',
                                  'evidence':{'execution_id':'exec-'+name}})
            data={'edges':edges}
        elif '/meta/scan/runs?' in path:data={'total':len(self.scans),'items':[{'execution_id':s} for s in self.scans]}
        else:raise AssertionError((method,path,body))
        return SimpleNamespace(payload=copy.deepcopy(data))


class StreamToVectorD8PersistenceTest(unittest.TestCase):
    def test_eight_exact_dags_two_roles_and_generic_vector_save(self):
        sources={n:n for n in m.fixture.SOURCES}
        for name in m.fixture.TARGETS:
            tasks=m.definition(sources,'parent',name)['tasks'];lines=tasks[-2]
            self.assertEqual(tasks[-1]['operator'],'save')
            self.assertEqual(tasks[-1]['params'],{'input_df':{'$ref':'lines'},'target_parent_locator':'parent','target_name':name,'mode':'fail'})
            self.assertEqual(lines['operator'],'raster_stream_to_vector_d8')
            self.assertEqual(set(lines['params']),{'input_raster','stream_raster','band','stream_band'})
            direction,streams,band,stream_band,stage=m.fixture.CASES[name]
            self.assertEqual(lines['params']['band'],1 if stage in ('direction','fill') else band)
            self.assertEqual(lines['params']['stream_band'],stream_band)
            self.assertEqual([t['params']['locator'] for t in tasks if t['operator']=='raster_load'],m.case_inputs(name))
        for problem in m.FAILURES:
            tasks=m.definition(sources,'parent','metre.gpkg',invalid=problem)['tasks'];lines=tasks[-2]
            self.assertEqual(lines['params']['band'],99 if problem=='band' else 2)
            self.assertEqual(lines['params']['stream_band'],99 if problem=='stream_band' else 2)
        for name in ('../outside.gpkg','unknown.gpkg'):
            with self.assertRaises(m.SuiteError):m.definition(sources,'parent',name)

    def test_complete_scene_preserves_twelve_permissions_eight_exact_scans_and_current_source_lineage(self):
        client=Client();report=m.run_scenario(Path('.'),ENV,client,physical,diagnostic)
        self.assertEqual(report['result'],'passed');self.assertEqual([len(c['lineage']) for c in report['cases']],[2,2,2,1,1,1,2,2])
        self.assertEqual(len(client.scans),9);self.assertEqual(len(report['failed_parameters']),20)
        self.assertFalse({item['id'] for item in client.sources.values()} & set(client.targets))
        self.assertEqual(len(report['identity']['permissions_verified']),12)
        self.assertEqual(len([c for c in client.calls if c[0]=='POST' and c[1].endswith('/scan/run/manual')]),1)

    def test_strict_sizes_safe_scan_identity_and_failure_publication_are_fail_closed(self):
        for attr,value in [('corrupt_size',True),('actor',True),('actor','33'),('actor',34),('failure_publishes',True),('failure_scans',True)]:
            client=Client();setattr(client,attr,value)
            with self.subTest(attr=attr,value=value),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,client,physical,diagnostic)

    def test_physical_prior_file_change_missing_workspace_and_incomplete_content_are_rejected(self):
        for kind in ['prior','workspace',*m.REQUIRED_PHYSICAL]:
            def verify(repository,env,names):
                result=physical(repository,env,names)
                if kind=='workspace':result.pop('runtime_workspaces_removed')
                elif kind=='prior':
                    if len(names)>1:result['targets'][names[0]]['sha256']='changed'
                else:result['targets'][names[0]][kind]=False
                return result
            with self.subTest(kind=kind),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Client(),verify,diagnostic)



    def test_diagnostic_reuses_sanitizer_with_suite_specific_filename(self):
        self.assertTrue(issubclass(m.DiagnosticClient,m.rasterize.DiagnosticClient))
        self.assertEqual(m.DiagnosticClient.diagnostic_name,'stream-to-vector-d8-submission-error.json')

    def test_exact_scan_parent_engine_tenant_and_principal_are_required(self):
        for field,value in [('execution_id','foreign'),('parent_execution_id','foreign'),('tenant_id',True),('tenant_id',3),
                            ('triggered_by',34),('engine_id',True),('engine_id',4),('module','develop'),('task_type','other')]:
            class Changed(Client):
                def request(self,method,path,expected,body=None,headers=None):
                    response=super().request(method,path,expected,body,headers)
                    if '/meta/executions/' in path and not path.endswith('/source-scan'):
                        if field=='engine_id':response.payload['scan_context']['engine_id']=value
                        else:response.payload[field]=value
                    return response
            with self.subTest(field=field,value=value),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Changed(),physical,diagnostic)

    def test_failed_execution_cannot_add_active_derived_lineage(self):
        class Changed(Client):
            def request(self,method,path,expected,body=None,headers=None):
                response=super().request(method,path,expected,body,headers)
                if '/meta/lineage/graph?' in path and any(e['status']=='failed' for e in self.executions.values()):
                    response.payload['edges'].append({'status':'active','relation_kind':'derive','evidence':{'execution_id':'failed'}})
                return response
        with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Changed(),physical,diagnostic)

    def test_exact_failure_diagnostic_rejects_wrong_execution_task_cause_and_owner(self):
        import json,tempfile
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);path=root/'logs/runtime/develop/backend/test.jsonl';path.parent.mkdir(parents=True)
            for language in ('zh-cn','en'):
                message='[DevExecutor] 工作流引擎调用失败: execution_id=exec-123 err=任务 lines 执行失败: '+m.MESSAGES['band'][language=='en']
                original={'module_name':'develop','role':'backend','message':message};path.write_text(json.dumps(original)+'\n')
                self.assertEqual(m.invalid_diagnostic(root,{},'exec-123','band',language,1)['task'],'lines')
                for field,value in [('module_name','meta'),('role','worker'),('message',message.replace('exec-123','exec-1234')),
                                    ('message',message.replace('任务 lines','任务 save')),('message',message.replace(m.MESSAGES['band'][language=='en'],m.MESSAGES['alignment'][language=='en']))]:
                    event={**original,field:value};path.write_text(json.dumps(event)+'\n')
                    with self.subTest(field=field,value=value),patch.object(m.time,'sleep'),self.assertRaises(m.SuiteError):
                        m.invalid_diagnostic(root,{},'exec-123','band',language,.002)

    def test_all_twenty_failures_use_real_submission_language_headers_and_unique_targets(self):
        client=Client();report=m.run_scenario(Path('.'),ENV,client,physical,diagnostic)
        self.assertEqual({(f['problem'],f['language']) for f in report['failed_parameters']},{(p,l) for p in m.FAILURES for l in ('zh-cn','en')})
        submitted=[h['Accept-Language'] for method,path,h in client.headers if method=='POST' and path=='/api/v1/develop/executions' and h]
        self.assertEqual(submitted,['zh-cn','en']*10)
        self.assertEqual(len(client.executions),28)


    def test_safe_failure_category_is_required(self):
        class Wrong(Client):
            def request(self,method,path,expected,body=None,headers=None):
                response=super().request(method,path,expected,body,headers)
                if '/develop/executions/exec-failed-' in path:response.payload['error_details']={'category':'permission_denied'}
                return response
        with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Wrong(),physical,diagnostic)

    def test_registered_but_unused_sources_cannot_enter_current_execution_inputs(self):
        class Wrong(Client):
            def request(self,method,path,expected,body=None,headers=None):
                response=super().request(method,path,expected,body,headers)
                if '/develop/executions/exec-metre.gpkg' in path:
                    response.payload['metadata']['lineage_facts']['inputs'].append({'locator':'addp://engine/3/path/code.tif?type=file&item_id=11'})
                return response
        with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Wrong(),physical,diagnostic)

    def test_failed_monitor_cannot_hide_failure_or_publish_lineage(self):
        class Wrong(Client):
            def request(self,method,path,expected,body=None,headers=None):
                response=super().request(method,path,expected,body,headers)
                if '/monitor/executions/' in path and 'failed-' in path:response.payload['status']='success'
                return response
        with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Wrong(),physical,diagnostic)

    def test_opposite_query_cannot_rewrite_the_failure(self):
        class Wrong(Client):
            def request(self,method,path,expected,body=None,headers=None):
                response=super().request(method,path,expected,body,headers)
                if '/develop/executions/' in path and 'failed-' in path and headers:
                    response.payload['error']='query changed old error'
                return response
        with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Wrong(),physical,diagnostic)


    def test_both_actual_registered_inputs_are_required_and_cannot_be_collapsed(self):
        for mutation in ('missing_mask','duplicate_direction','foreign_mask'):
            class Wrong(Client):
                def request(self,method,path,expected,body=None,headers=None):
                    response=super().request(method,path,expected,body,headers)
                    if '/develop/executions/exec-metre.gpkg' in path:
                        inputs=response.payload['metadata']['lineage_facts']['inputs']
                        if mutation=='missing_mask':inputs.pop()
                        elif mutation=='duplicate_direction':inputs[1]=inputs[0]
                        else:inputs[1]={'locator':'addp://engine/3/path/code.tif?type=file&item_id=19'}
                    return response
            with self.subTest(mutation=mutation),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Wrong(),physical,diagnostic)




    def test_vector_sizes_container_boundary_and_typed_physical_counts_fail_closed(self):
        for position in ('meta','physical'):
            for size in (True,0,-1,'2048',2049):
                item=target_item('metre.gpkg',100);fact=physical(None,None,['metre.gpkg'])['targets']['metre.gpkg']
                (item if position=='meta' else fact)['size_bytes']=size
                with self.subTest(position=position,size=size),self.assertRaises(m.SuiteError):m.vector_persistence.validate_item(item,fact,'metre.gpkg')
        for key,value in [('feature_count',True),('feature_count',0),('edge_count',0),('vertex_count',1),('crs','EPSG:4326'),('schema',[]),('skipped_terminal_link_ids',[])]:
            def verify(repository,env,names):
                result=physical(repository,env,names);result['targets'][names[0]][key]=value;return result
            with self.subTest(key=key,value=value),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Client(),verify,diagnostic)

    def test_no_raster_artifact_and_precise_gpkg_child_boundary(self):
        class Wrong(Client):
            def request(self,method,path,expected,body=None,headers=None):
                response=super().request(method,path,expected,body,headers)
                if '/develop/executions/exec-metre.gpkg' in path:response.payload['metadata']['result']['final_result']={'artifact_type':'raster'}
                return response
        with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Wrong(),physical,diagnostic)
        item=target_item('metre.gpkg',100);item['attributes']['type_info']['container']['children'].append({'name':'foreign'})
        with self.assertRaises(m.SuiteError):m.vector_persistence.validate_item(item,physical(None,None,['metre.gpkg'])['targets']['metre.gpkg'],'metre.gpkg')
