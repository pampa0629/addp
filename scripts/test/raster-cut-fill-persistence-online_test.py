import base64,copy,csv,hashlib,importlib,io,json,tempfile,unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch
m=importlib.import_module('scripts.test.raster-cut-fill-persistence-online')
ENV={'ONLINE_SUITE':'raster-cut-fill-persistence','ADDP_ONLINE_TEST_TENANT_ID':'2','ADDP_ONLINE_CUT_FILL_NFS_ENGINE_ID':'3','ADDP_ONLINE_TEST_RUN_ID':'cut-fill-123-1','ADDP_ONLINE_TEST_TIMEOUT_SECONDS':'0.05'}

def preview(name):return {'type':'pandas_dataframe','schema':[{'name':n,'type':'int64' if i<6 else 'Float64'} for i,n in enumerate(m.fixture.COLUMNS)],'row_count':1,'preview_limit':50,'preview_rows':[m.fixture.expected_row(name)],'truncated':False}

def payload(name):
    stream=io.StringIO(newline='');writer=csv.DictWriter(stream,fieldnames=m.fixture.COLUMNS);writer.writeheader();writer.writerow(m.fixture.expected_row(name));return stream.getvalue().encode()

def physical(repository,env,names):
    targets={}
    for name in names:
        raw=payload(name);targets[name]={'size_bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest(),'schema':list(m.fixture.COLUMNS),'row_count':1,'complete_table_verified':True,'independent_decimal_oracle_verified':True,'count_and_area_conservation_verified':True,'nonnegative_gross_and_net_difference_verified':True,'measurement_grid':list(m.fixture.source_spec(m.fixture.CASES[name][0])['grid']),'horizontal_metres':m.fixture.source_spec(m.fixture.CASES[name][0])['horizontal'],'source_measurement_geometry_verified':True,'csv_base64':base64.b64encode(raw).decode()}
    return {'runtime_workspaces_removed':True,'targets':targets}

def target_item(name,identifier):return {'id':identifier,'item_type':'file','full_name':'outputs/'+name,'size_bytes':len(payload(name)),'fingerprint':name,'scanned_at':'2026-10-27T00:00:00Z','attributes':{'item':{'format':'csv'}}}

def diagnostic(repository,env,identifier,problem,language,timeout):return {'execution_id':identifier,'task':'cut_fill','cause':'invalid_'+problem,'source':'develop_runtime_log','language':language,'exact_cause':m.MESSAGES[problem][language=='en']}

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
            if tasks[-1]['params'].get('mode')!='fail' or 'write_mode' in tasks[-1]['params']:
                raise AssertionError('CSV save requires the formal fail mode')
            if name.startswith('failed-'):
                self.executions[identifier]={'status':'failed','outputs':{'save':{}} if self.failure_publishes else {},'metadata':{'result':{}},'error_details':{'category':'execution_failed'}}
                if self.failure_scans:self.scans.append('invalid-scan')
            else:
                target='addp://engine/3/path/outputs/'+name+'?type=file';scan='scan-'+name;self.scans.append(scan)
                # Develop's node-scoped facts are ordered by node ID, not task declaration.
                facts={'schema_version':'addp.lineage-facts/v1',
                    'inputs':[{'port':'input.'+t['id'],'locator':t['params']['locator']} for t in sorted(tasks,key=lambda t:t['id']) if 'locator' in t['params']],
                    'outputs':[{'port':'output.save','locator':target,'write_mode':'create'}],
                    'operations':[{'kind':'derive','operator':'save','input_ports':['input.after','input.before'],'output_ports':['output.save']}]}
                artifact=preview(name)
                self.executions[identifier]={'status':'success','outputs':{'save':{'resource':{'locator':target,'type':'file','write_mode':'create'}}},
                    'metadata':{'lineage_facts':facts,'result':{'final_result':artifact,'meta_scan_runs':[{'status':'submitted','target_locator':target,'execution_id':scan}]}}}
                identifier_item=100+len(self.targets);self.targets[identifier_item]=target_item(name,identifier_item)
                if self.corrupt_size:self.targets[identifier_item]['size_bytes']+=1
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
        result=copy.deepcopy(data)
        if method=='GET' and ('/develop/executions/' in path or '/monitor/executions/' in path):
            facts=result.get('metadata',{}).get('lineage_facts')
            if isinstance(facts,dict):facts['operations']=None
        return SimpleNamespace(payload=result)


class CutFillPersistenceTest(unittest.TestCase):
    def run_scene(self,client=None,physical_runner=physical):
        with tempfile.TemporaryDirectory() as root:
            env={**ENV,'ADDP_ONLINE_ARTIFACT_DIR':root};client=client or Client()
            report=m.run_scenario(Path('.'),env,client,physical_runner,diagnostic,lambda e,i,t,p,w: copy.deepcopy(client.executions[i]['metadata']['lineage_facts']))
            self.assertEqual(len(list((Path(root)/'cut-fill-csv').glob('*.csv'))),12)
            self.assertEqual(len(list((Path(root)/'cut-fill-lineage').glob('*.json'))),12)
            return report,client

    def test_twelve_exact_dags_two_actual_sources_bands_units_and_existing_save(self):
        sources={n:n for n in m.fixture.SOURCES}
        self.assertEqual(len(m.fixture.CASES),12);self.assertEqual(len(m.FAILURES),18)
        for name in m.fixture.TARGETS:
            tasks=m.definition(sources,'parent',name)['tasks'];a,b,u,v,stage=m.fixture.CASES[name]
            self.assertEqual([t['params']['locator'] for t in tasks if t['operator']=='raster_load'],[a,b])
            self.assertEqual(m.case_inputs(name),[a,b]);self.assertEqual(tasks[-2]['operator'],'raster_cut_fill')
            self.assertEqual(tasks[-2]['params']['input_raster'],{'$ref':'before'})
            self.assertEqual(tasks[-2]['params']['after_raster'],{'$ref':{'direct':'after','fill':'fill','reclassify':'classes'}[stage]})
            if stage!='direct':self.assertEqual(tasks[-3]['params']['input_raster'],{'$ref':'after'})
            self.assertEqual(tasks[-2]['params']['elevation_unit'],u);self.assertEqual(tasks[-2]['params']['after_elevation_unit'],v)
            self.assertEqual(tasks[-2]['params']['band'],2);self.assertEqual(tasks[-2]['params']['after_band'],2 if stage=='direct' else 1)
            self.assertEqual(tasks[-1]['params'],{'input_df':{'$ref':'cut_fill'},'target_parent_locator':'parent','target_name':name,'mode':'fail'})
        for problem in m.FAILURES:
            for lang in ('zh-cn','en'):
                tasks=m.definition(sources,'parent','mixed.csv',invalid=problem,language=lang)['tasks'];self.assertEqual(tasks[-1]['params']['target_name'],f'failed-{problem}-{lang}.csv')

    def test_complete_matrix_exact_current_scan_lineage_and_original_csv_archive(self):
        report,client=self.run_scene();self.assertEqual(len(report['cases']),12);self.assertEqual(len(report['failed_parameters']),36);self.assertEqual(len(client.executions),48);self.assertEqual(len(client.scans),13)
        self.assertEqual(len(report['archived_csv']),12)
        for case in report['cases']:
            self.assertEqual(len(case['lineage']),2);self.assertTrue(case['monitor_verified']);self.assertEqual(case['size_evidence']['physical_size_bytes'],case['size_evidence']['meta_size_bytes'])
        headers=[h for method,path,h in client.headers if method=='POST' and path=='/api/v1/develop/executions']
        self.assertEqual(len(headers),48);self.assertEqual([h['Accept-Language'] for h in headers[12:]],['zh-cn','en']*18)

    def success_fixture(self,name='mixed.csv',reverse_declarations=False):
        sources={n:'addp://engine/3/path/'+n+'?type=file&item_id='+str(i+10) for i,n in enumerate(m.fixture.SOURCES)}
        workflow=m.definition(sources,'addp://engine/3/path/outputs?type=directory',name)
        if reverse_declarations:workflow['tasks'][:2]=workflow['tasks'][1::-1]
        client=Client();client.request('POST','/api/v1/develop/executions',(202,),{'content':{'workflow_definition':workflow}})
        target='addp://engine/3/path/outputs/'+name+'?type=file';a,b=m.case_inputs(name)
        return client.executions['exec-'+name],{'input.before':sources[a],'input.after':sources[b]},target

    def test_node_ports_bind_roles_independent_of_task_and_fact_array_order(self):
        for name in m.fixture.TARGETS:
            for reverse in (False,True):
                with self.subTest(name=name,reverse=reverse):
                    execution,bindings,target=self.success_fixture(name,reverse)
                    for reorder in (False,True):
                        facts=execution['metadata']['lineage_facts']
                        if reorder:facts['inputs'].reverse();facts['operations'][0]['input_ports'].reverse()
                        m.lineage_observer.validate(facts,m.expected_lineage(bindings,target),public=False)
                        public=copy.deepcopy(execution);public['metadata']['lineage_facts']['operations']=None
                        actual,scan,result=m.validate_success(public,bindings,target)
                        self.assertEqual(actual,{**facts,'operations':None});self.assertEqual(scan,'scan-'+name)
        ordinary,_,_=self.success_fixture();reversed_case,_,_=self.success_fixture('reversed.csv')
        self.assertNotEqual(ordinary['metadata']['lineage_facts']['inputs'],reversed_case['metadata']['lineage_facts']['inputs'])

    def test_role_swaps_duplicate_missing_and_extra_ports_or_relations_fail_closed(self):
        def mutate(execution,fault):
            facts=execution['metadata']['lineage_facts'];op=facts['operations'][0]
            if fault=='swap-resources':facts['inputs'][0]['locator'],facts['inputs'][1]['locator']=facts['inputs'][1]['locator'],facts['inputs'][0]['locator']
            elif fault=='swap-ports':facts['inputs'][0]['port'],facts['inputs'][1]['port']=facts['inputs'][1]['port'],facts['inputs'][0]['port']
            elif fault=='missing-source':facts['inputs'].pop()
            elif fault=='duplicate-port':facts['inputs'].append(copy.deepcopy(facts['inputs'][0]))
            elif fault=='extra-source':facts['inputs'].append({'port':'input.unused','locator':'addp://engine/3/path/unused?type=file'})
            elif fault=='no-ports':facts['inputs'][0].pop('port')
            elif fault=='wrong-source':facts['inputs'][0]['locator']='addp://engine/3/path/other?type=file'
            elif fault=='wrong-schema':facts['schema_version']='other'
            elif fault=='extra-envelope':facts['all_inputs']=facts['inputs']
            elif fault=='missing-operations':facts['operations']=[]
            elif fault=='extra-operation':facts['operations'].append(copy.deepcopy(op))
            elif fault=='operation-role':op['input_ports']=['input.before']
            elif fault=='operation-extra-source':op['input_ports'].append('input.unused')
            elif fault=='operation-duplicate':op['input_ports'].append('input.before')
            elif fault=='wrong-kind':op['kind']='copy'
            elif fault=='wrong-operator':op['operator']='raster_cut_fill'
            elif fault=='wrong-output-port':facts['outputs'][0]['port']='output.other'
            elif fault=='wrong-operation-output':op['output_ports']=['output.other']
            elif fault=='wrong-mode':facts['outputs'][0]['write_mode']='replace'
            elif fault=='extra-output':facts['outputs'].append(copy.deepcopy(facts['outputs'][0]))
            elif fault=='wrong-target':facts['outputs'][0]['locator']='addp://engine/3/path/other.csv?type=file'
            elif fault=='execution-output':execution['outputs']['save']['resource']['write_mode']='replace'
            elif fault=='no-scan':execution['metadata']['result']['meta_scan_runs']=[]
            else:raise AssertionError(fault)
        faults=('swap-resources','swap-ports','missing-source','duplicate-port','extra-source','no-ports','wrong-source','wrong-schema','extra-envelope','missing-operations','extra-operation','operation-role','operation-extra-source','operation-duplicate','wrong-kind','wrong-operator','wrong-output-port','wrong-operation-output','wrong-mode','extra-output','wrong-target','execution-output','no-scan')
        for fault in faults:
            execution,bindings,target=self.success_fixture();mutate(execution,fault)
            with self.subTest(fault=fault),self.assertRaises(m.SuiteError):m.lineage_observer.validate(execution['metadata']['lineage_facts'],m.expected_lineage(bindings,target),public=False);execution['metadata']['lineage_facts']['operations']=None;m.validate_success(execution,bindings,target)

    def test_failure_freezes_closed_actual_and_expected_facts_before_rejection(self):
        class Bad(Client):
            def request(self,method,path,expected,body=None,headers=None):
                result=super().request(method,path,expected,body,headers)
                if '/develop/executions/exec-mixed.csv' in path:
                    result.payload['metadata']['lineage_facts']['inputs'][0]['locator']='addp://engine/3/path/wrong.tif?type=file'
                    result.payload['metadata']['private_access_token']='NEVER_ARCHIVE'
                    result.payload['metadata']['lineage_facts']['private_token']='NEVER_ARCHIVE'
                return result
        with tempfile.TemporaryDirectory() as root:
            env={**ENV,'ADDP_ONLINE_ARTIFACT_DIR':root}
            with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),env,Bad(),physical,diagnostic,lambda *args: self.success_fixture()[0]['metadata']['lineage_facts'])
            path=Path(root)/'cut-fill-lineage'/'mixed.csv.json';data=json.loads(path.read_text())
            self.assertEqual(data['execution_id'],'exec-mixed.csv');self.assertEqual(data['case'],'mixed.csv')
            self.assertEqual([v['port'] for v in data['expected_internal']['inputs']],['input.after','input.before'])
            self.assertNotEqual(data['actual_public']['inputs'][0]['locator'],data['expected_internal']['inputs'][0]['locator'])
            self.assertNotIn('NEVER_ARCHIVE',path.read_text());self.assertFalse((Path(root)/'cut-fill-csv').exists())

    def test_lineage_evidence_is_immutable_and_rejects_symlinks_or_extra_inventory(self):
        for fault in ('overwrite','extra','symlink'):
            execution,bindings,target=self.success_fixture()
            with tempfile.TemporaryDirectory() as root:
                env={**ENV,'ADDP_ONLINE_ARTIFACT_DIR':root};raw=copy.deepcopy(execution['metadata']['lineage_facts']);execution['metadata']['lineage_facts']['operations']=None;proof=m.record_lineage(env,'owned','mixed.csv',bindings,target,execution,2,33,{},lambda *args: raw)
                path=Path(root)/proof['path'];cases=[{'name':'mixed.csv','lineage_evidence':proof}]
                m.verify_lineage_archive(env,cases)
                for field,bad in (('path','other.json'),('size_bytes',True),('sha256','bad')):
                    changed=copy.deepcopy(cases);changed[0]['lineage_evidence'][field]=bad
                    with self.subTest(field=field),self.assertRaises(m.SuiteError):m.verify_lineage_archive(env,changed)
                if fault=='overwrite':path.write_text('{}')
                elif fault=='extra':path.with_name('extra.json').write_text('{}')
                else:path.unlink();path.symlink_to(Path(root)/'missing')
                with self.subTest(fault=fault),self.assertRaises(m.SuiteError):m.verify_lineage_archive(env,cases)

    def test_identity_size_and_failure_publication_are_fail_closed(self):
        for field,value in [('actor',1),('corrupt_size',True),('failure_publishes',True),('failure_scans',True)]:
            client=Client();setattr(client,field,value)
            with self.subTest(field=field),self.assertRaises(m.SuiteError):self.run_scene(client)

    def test_complete_physical_proofs_and_csv_bytes_cannot_be_omitted(self):
        for key,bad in [('runtime_workspaces_removed',False),('complete_table_verified',False),('independent_decimal_oracle_verified',False),('count_and_area_conservation_verified',False),('nonnegative_gross_and_net_difference_verified',False),('size_bytes',True),('size_bytes',0),('sha256','bad'),('csv_base64','AAAA'),('row_count',True),('schema',[])]:
            report=physical(None,ENV,['mixed.csv'])
            if key=='runtime_workspaces_removed':report[key]=bad
            else:report['targets']['mixed.csv'][key]=bad
            with self.subTest(key=key,bad=bad),self.assertRaises(m.SuiteError):m.check_physical(report,{})

    def test_complete_preview_types_counts_and_bounds(self):
        for field,bad in [('row_count',True),('row_count',2),('preview_limit',True),('truncated',True),('schema',[]),('preview_rows',[])]:
            value=preview('mixed.csv');value[field]=bad
            with self.subTest(field=field),self.assertRaises(m.SuiteError):m.validate_preview(value,'mixed.csv')
        value=preview('mixed.csv');value['preview_rows'][0]['total_count']=True
        with self.assertRaises(m.SuiteError):m.validate_preview(value,'mixed.csv')

    def test_csv_meta_sizes_must_be_strict_positive_equal_ints(self):
        native=physical(None,ENV,['mixed.csv'])['targets']['mixed.csv']
        for field,bad in [('size_bytes',True),('size_bytes',0),('size_bytes','123'),('size_bytes',1),('scanned_at',None),('full_name','outputs/other.csv')]:
            item=target_item('mixed.csv',100);item[field]=bad
            with self.subTest(field=field),self.assertRaises(m.SuiteError):m.validate_item(item,native,'mixed.csv')

    def test_measurement_geometry_is_typed_and_bound_to_source(self):
        for key,bad in [('measurement_grid',[]),('horizontal_metres',True),('horizontal_metres',0),('horizontal_metres',float('nan')),('horizontal_metres',.3048),('source_measurement_geometry_verified',False)]:
            report=physical(None,ENV,['mixed.csv']);report['targets']['mixed.csv'][key]=bad
            with self.subTest(key=key,bad=bad),self.assertRaises(m.SuiteError):m.check_physical(report,{})

    def test_oracle_rows_are_not_mutable_cache_state(self):
        row=m.fixture.expected_row('mixed.csv');row['total_count']=True
        self.assertEqual(m.fixture.expected_row('mixed.csv')['total_count'],m.fixture.WIDTH*m.fixture.HEIGHT)

    def test_source_hash_guard_rejects_changed_source_bytes(self):
        with tempfile.TemporaryDirectory() as secret:
            root=Path(secret);(root/'cut-fill-source.json').write_text(json.dumps({'sources':{'before.tif':{'sha256':'old'}}}))
            with patch.object(m.fixture,'guard',return_value=root),patch.object(m.fixture,'owned_root',return_value=root),patch.object(m.fixture,'runtime_worker',return_value={'sources':{'before.tif':{'sha256':'changed'}}}),patch('sys.argv',['fixture','verify','mixed.csv']):
                with self.assertRaises(m.fixture.FixtureError):m.fixture.main('verify')

    def test_previous_physical_or_archive_bytes_cannot_change(self):
        native=physical(None,ENV,['mixed.csv']);previous=copy.deepcopy(native['targets']);native['targets']['mixed.csv']['sha256']='changed'
        with self.assertRaises(m.SuiteError):m.check_physical(native,previous)
        with tempfile.TemporaryDirectory() as root:
            env={**ENV,'ADDP_ONLINE_ARTIFACT_DIR':root};native=physical(None,ENV,['mixed.csv']);m.archive_csv(env,native)
            path=Path(root)/'cut-fill-csv'/'mixed.csv';path.write_bytes(b'changed')
            with self.assertRaises(m.SuiteError):m.archive_csv(env,native)

    def test_unexpected_or_symlink_archive_rejected(self):
        for fault in ('extra','symlink'):
            with tempfile.TemporaryDirectory() as root:
                directory=Path(root)/'cut-fill-csv';directory.mkdir();path=directory/'extra.csv'
                if fault=='extra':path.write_text('unexpected')
                else:path.symlink_to(Path(root)/'missing')
                with self.subTest(fault=fault),self.assertRaises(m.SuiteError):m.archive_csv({'ADDP_ONLINE_ARTIFACT_DIR':root},physical(None,ENV,['mixed.csv']))

    def test_exact_owned_execution_stage_role_and_submitted_language_diagnostic(self):
        with tempfile.TemporaryDirectory() as root:
            path=Path(root)/'develop'/'instance';path.mkdir(parents=True);log=path/'current.jsonl'
            for problem in m.FAILURES:
                for language in ('zh-cn','en'):
                    cause=m.MESSAGES[problem][language=='en'];event={'module_name':'develop','role':'backend','message':'[DevExecutor] 工作流引擎调用失败: execution_id=owned err=任务 cut_fill 执行失败: '+cause};log.write_text(json.dumps(event)+'\n')
                    self.assertEqual(m.invalid_diagnostic(Path('.'),{'ADDP_RUNTIME_LOG_ROOT':root},'owned',problem,language,.1)['exact_cause'],cause)
            event['message']=event['message'].replace('任务 cut_fill','任务 save');log.write_text(json.dumps(event)+'\n')
            with self.assertRaises(m.SuiteError):m.invalid_diagnostic(Path('.'),{'ADDP_RUNTIME_LOG_ROOT':root},'owned',problem,language,.01)

    def test_adversarial_preview_or_failed_language_rewrite_rejected(self):
        class Bad(Client):
            def request(self,method,path,expected,body=None,headers=None):
                response=super().request(method,path,expected,body,headers)
                if '/develop/executions/exec-failed-' in path and headers and headers.get('Accept-Language')=='en':response.payload['error']='rewritten'
                return response
        with self.assertRaises(m.SuiteError):self.run_scene(Bad())

if __name__=='__main__':unittest.main()
