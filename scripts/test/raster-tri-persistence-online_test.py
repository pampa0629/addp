import copy,hashlib,importlib,json,tempfile,unittest
from pathlib import Path
from types import SimpleNamespace
m=importlib.import_module('scripts.test.raster-tri-persistence-online')
ENV={'ONLINE_SUITE':'raster-tri-persistence','ADDP_ONLINE_TEST_TENANT_ID':'2','ADDP_ONLINE_TRI_NFS_ENGINE_ID':'3','ADDP_ONLINE_TEST_RUN_ID':'tri-123-1','ADDP_ONLINE_TEST_TIMEOUT_SECONDS':'0.05'}

def physical(repository,env,names):
    targets={};archive={};count=(m.fixture.HEIGHT-2)*(m.fixture.WIDTH-2)
    for name in names:
        empty=name in ('invalid.cog.tif','sparse.cog.tif');zero=name in ('flat.cog.tif','filled.cog.tif')
        targets[name]={'size_bytes':2048,'sha256':hashlib.sha256(name.encode()).hexdigest(),'has_overviews':True,'overview_count':3,'valid_pixels':0 if empty else count,'zero_pixels':count if zero else 0,
            **{k:True for k in ('full_pixel_and_mask_verification','cog_valid','independent_decimal_oracle_verified','complete_neighbourhood_verified','metre_unit_verified','nearest_overviews_verified','eight_squared_differences_verified','legal_zero_verified','nonnegative_verified')}}
        archive[name]={'path':'tri-cogs/'+name,'size_bytes':2048,'sha256':targets[name]['sha256']}
    return {'runtime_workspaces_removed':True,'targets':targets,'archived_cogs':archive,
            'oracle_cache':{'scope_sha256':'a'*64,'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)},
            'verification_timing_ms':{k:0 for k in ('source_hash_ms','cache_load_ms','target_hash_ms','pixels_mask_metadata_ms','cog_layout_ms','overviews_ms')},
            'host_timing_ms':{k:0 for k in ('worker_transport_ms','source_receipt_ms','archive_ms')},'worker_total_ms':1}

def oracle(repository,env,action,names=()):
    if action=='prepare-oracle':return {'scope_sha256':'a'*64,'anchor_sha256':'b'*64,'expected_array_count':13,'cache_bytes':13*(m.fixture.WIDTH*m.fixture.HEIGHT*8+128),'timing_ms':{'expected_build_ms':1,'cache_write_ms':1}}
    if action=='cleanup-oracle':return {'oracle_cache_removed':True}
    raise AssertionError(action)


def diagnostic(repository,env,identifier,problem,language,timeout):
    return {'execution_id':identifier,'task':'tri','cause':'invalid_'+problem,'source':'develop_runtime_log','language':language,'exact_cause':m.MESSAGES[problem][language=='en']}

def target_item(name,identifier):
    expected=m.fixture.artifact_expectation(name)
    return {'id':identifier,'item_type':'file','full_name':'outputs/'+name,'fingerprint':name,'size_bytes':2048,'scanned_at':'2026-10-19T00:00:00Z',
        'attributes':{'item':{'format':'tiff'},'type_info':{'media':{'width':expected['width'],'height':expected['height']}},
            'format_info':{'tiff':{'profile':'geotiff' if name in ('small.cog.tif','stationary.cog.tif') else 'cog','is_tiled':True,'has_overviews':name not in ('small.cog.tif','stationary.cog.tif')}},
            'capabilities':{'spatial':{'srid':expected['extent_srid'],'extent':expected['extent']}}}}


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
            if tasks[-1]['params'].get('write_mode')!='create' or 'mode' in tasks[-1]['params']:
                raise AssertionError('raster_save requires the formal write_mode parameter')
            if name.startswith('failed-'):
                self.executions[identifier]={'status':'failed','outputs':{'save':{}} if self.failure_publishes else {},'metadata':{'result':{}},'error_details':{'category':'execution_failed'}}
                if self.failure_scans:self.scans.append('invalid-scan')
            else:
                target='addp://engine/3/path/outputs/'+name+'?type=file';scan='scan-'+name;self.scans.append(scan)
                facts={'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.source','locator':t['params']['locator']} for t in tasks if 'locator' in t['params']],
                    'outputs':[{'port':'output.save','locator':target,'write_mode':'create'}],
                    'operations':[{'kind':'derive','operator':'raster_save','input_ports':['input.source'],'output_ports':['output.save']}]}
                expectation=m.fixture.artifact_expectation(name)
                artifact={k:v for k,v in expectation.items() if k in {'width','height','band_count','source_crs','extent_srid','transform'}}
                artifact.update(artifact_type='raster',format='tiff',profile='cog',size_bytes=2048,
                    bands=[{'dtype':'Float64','nodata_is_nan':True,'nodata':None}])
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
        result=copy.deepcopy(data)
        if method=='GET' and ('/develop/executions/' in path or '/monitor/executions/' in path):
            facts=result.get('metadata',{}).get('lineage_facts')
            if isinstance(facts,dict):facts['operations']=None
        return SimpleNamespace(payload=result)


class TriPersistenceTest(unittest.TestCase):
    def run_scene(self,client=None,physical_runner=physical):
        with tempfile.TemporaryDirectory() as root:
            client=client or Client();env={**ENV,'ADDP_ONLINE_ARTIFACT_DIR':root}
            return m.run_scenario(Path('.'),env,client,physical_runner,diagnostic,
                                 lambda e,i,t,p,w: copy.deepcopy(client.executions[i]['metadata']['lineage_facts']),oracle)

    def test_thirteen_exact_dags_required_band_unit_and_original_save(self):
        self.assertEqual(len(m.fixture.TARGETS),13);self.assertEqual(len(m.FAILURES),13)
        sources={n:n for n in m.fixture.SOURCES}
        for name in m.fixture.TARGETS:
            tasks=m.definition(sources,'parent',name)['tasks'];source,band,unit,stage=m.fixture.CASES[name]
            self.assertEqual(tasks[0]['params'],{'locator':source})
            self.assertEqual(tasks[-2]['operator'],'raster_tri')
            self.assertEqual(tasks[-2]['params'],{'input_raster':{'$ref':{'direct':'source','fill':'fill','reclassify':'classes'}[stage]},'band':band if stage=='direct' else 1,'elevation_unit':unit})
            self.assertEqual(tasks[-1]['params'],{'input_raster':{'$ref':'tri'},'target_parent_locator':'parent','target_name':name,'write_mode':'create','profile':'cog','blocksize':128,'overview_resampling':'nearest'})
        for problem in m.FAILURES:
            for lang in ('zh-cn','en'):
                tasks=m.definition(sources,'parent','tri.cog.tif',invalid=problem,language=lang)['tasks'];self.assertEqual(tasks[-1]['params']['target_name'],f'failed-{problem}-{lang}.cog.tif')

    def test_complete_thirteen_success_twenty_six_bilingual_failures_and_exact_archive(self):
        client=Client();report=self.run_scene(client)
        self.assertEqual(report['result'],'passed');self.assertEqual(len(report['cases']),13);self.assertEqual(len(report['failed_parameters']),26);self.assertEqual(len(client.executions),39)
        self.assertEqual(len(client.scans),14);self.assertEqual([len(c['lineage']) for c in report['cases']],[1]*13)
        self.assertEqual(len(report['archived_cogs']),13);self.assertEqual(len(report['identity']['permissions_verified']),12)
        self.assertEqual(report['stage_progress']['schema_version'],'addp.raster-tri-progress/v1')
        self.assertEqual(len([c for c in client.calls if c[0]=='POST' and c[1].endswith('/scan/run/manual')]),1)
        headers=[h for method,path,h in client.headers if method=='POST' and path=='/api/v1/develop/executions']
        self.assertEqual([h['Accept-Language'] for h in headers[13:]],['zh-cn','en']*13)

    def test_identity_scan_sizes_and_failure_publication_remain_fail_closed(self):
        for attr,value in [('corrupt_size',True),('actor',True),('actor','33'),('actor',34),('failure_publishes',True),('failure_scans',True)]:
            client=Client();setattr(client,attr,value)
            with self.subTest(attr=attr,value=value),self.assertRaises(m.SuiteError):self.run_scene(client)

    def test_strict_positive_equal_sizes_have_no_boolean_or_string_coercion(self):
        for position in range(3):
            for size in (True,0,-1,'2048',2048.0,2049):
                entries=[{'size_bytes':2048} for _ in range(3)];entries[position]['size_bytes']=size
                with self.subTest(position=position,size=size),self.assertRaises(m.SuiteError):m.raster.strict_sizes(*entries,'tri.cog.tif')

    def test_physical_flags_counts_and_original_cog_receipts_are_mandatory(self):
        for key,bad in [('full_pixel_and_mask_verification',False),('cog_valid',False),('independent_decimal_oracle_verified',False),('complete_neighbourhood_verified',False),('metre_unit_verified',False),('nearest_overviews_verified',False),('eight_squared_differences_verified',False),('legal_zero_verified',False),('overview_count',True),('overview_count',2),('has_overviews',False),('zero_pixels',True),('valid_pixels',-1),('size_bytes',True),('sha256','bad')]:
            native=physical(None,ENV,['tri.cog.tif']);native['targets']['tri.cog.tif'][key]=bad
            with self.subTest(key=key,bad=bad),self.assertRaises(m.SuiteError):m.check_physical(native,{},'a'*64)
        for name in ('flat.cog.tif','plane.cog.tif','saddle.cog.tif','filled.cog.tif','invalid.cog.tif','sparse.cog.tif'):
            native=physical(None,ENV,[name]);native['targets'][name]['valid_pixels']=1
            with self.subTest(name=name),self.assertRaises(m.SuiteError):m.check_physical(native,{},'a'*64)
        for key,bad in [('path','wrong.tif'),('sha256','bad'),('size_bytes',True)]:
            native=physical(None,ENV,['tri.cog.tif']);native['archived_cogs']['tri.cog.tif'][key]=bad
            with self.subTest(key=key),self.assertRaises(m.SuiteError):m.check_physical(native,{},'a'*64)
        native=physical(None,ENV,['tri.cog.tif']);native['archived_cogs']={}
        with self.assertRaises(m.SuiteError):m.check_physical(native,{},'a'*64)

    def test_previous_cogs_and_source_metadata_cannot_change(self):
        def corrupt(repo,env,names):
            native=physical(repo,env,names)
            if len(names)>1:native['targets'][names[0]]['sha256']='changed'
            return native
        with self.assertRaises(m.SuiteError):self.run_scene(physical_runner=corrupt)
        class Bad(Client):
            def request(self,method,path,expected,body=None,headers=None):
                result=super().request(method,path,expected,body,headers)
                if any(e['status']=='failed' for e in self.executions.values()) and '/meta/items/' in path:
                    result.payload['size_bytes']+=1
                return result
        with self.assertRaises(m.SuiteError):self.run_scene(Bad())

    def test_exact_source_node_output_create_and_derive_cannot_be_weakened(self):
        client=Client();report=self.run_scene(client);execution=client.executions['exec-tri.cog.tif'];facts=execution['metadata']['lineage_facts'];source=facts['inputs'][0]['locator'];target=facts['outputs'][0]['locator']
        for fault in ('source','port','mode','operation','extra'):
            changed=copy.deepcopy(facts)
            if fault=='source':changed['inputs'][0]['locator']='foreign'
            elif fault=='port':changed['inputs'][0]['port']='input.unused'
            elif fault=='mode':changed['outputs'][0]['write_mode']='replace'
            elif fault=='operation':changed['operations'][0]['input_ports']=[]
            else:changed['inputs'].append(copy.deepcopy(changed['inputs'][0]))
            with self.subTest(fault=fault),self.assertRaises(m.SuiteError):m.validate_lineage(changed,source,target)

    def test_tri_rejection_freezes_public_and_internal_difference_before_failure(self):
        client=Client()
        def reader(env,identifier,tenant,principal,workflow):
            facts=copy.deepcopy(client.executions[identifier]['metadata']['lineage_facts']);facts['operations']=None;return facts
        with tempfile.TemporaryDirectory() as root:
            env={**ENV,'ADDP_ONLINE_ARTIFACT_DIR':root}
            with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),env,client,physical,diagnostic,reader,oracle)
            path=Path(root)/'tri-lineage/tri.cog.tif.json';value=json.loads(path.read_text())
            self.assertEqual(value['actual_internal']['operations'],{'invalid_type':'NoneType'})
            self.assertEqual(value['actual_public']['operations'],{'invalid_type':'NoneType'})
            self.assertTrue(value['expected_internal']['operations']);self.assertEqual(len(client.executions),1)

    def test_failed_execution_cannot_add_active_derive_or_rewrite_language(self):
        for fault in ('lineage','language'):
            class Bad(Client):
                def request(self,method,path,expected,body=None,headers=None):
                    result=super().request(method,path,expected,body,headers)
                    if any(e['status']=='failed' for e in self.executions.values()):
                        if fault=='lineage' and '/meta/lineage/graph?' in path:result.payload['edges'].append({'status':'active','relation_kind':'derive','evidence':{'execution_id':'failed'}})
                        if fault=='language' and '/develop/executions/exec-failed-' in path and headers and headers.get('Accept-Language')=='en':result.payload['error']='rewritten'
                    return result
            with self.subTest(fault=fault),self.assertRaises(m.SuiteError):self.run_scene(Bad())

    def test_each_failure_requires_owned_tri_node_and_exact_submitted_cause(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);logs=root/'develop'/'backend';logs.mkdir(parents=True);path=logs/'current.jsonl'
            for problem in m.FAILURES:
                for language in ('zh-cn','en'):
                    cause=m.MESSAGES[problem][language=='en'];event={'module_name':'develop','role':'backend','message':'[DevExecutor] 工作流引擎调用失败: execution_id=owned err=任务 tri 执行失败: '+cause};path.write_text(json.dumps(event)+'\n')
                    result=m.invalid_diagnostic(Path('.'),{'ADDP_RUNTIME_LOG_ROOT':directory},'owned',problem,language,.1);self.assertEqual(result['exact_cause'],cause)
            event['message']=event['message'].replace('任务 tri','任务 save');path.write_text(json.dumps(event)+'\n')
            with self.assertRaises(m.SuiteError):m.invalid_diagnostic(Path('.'),{'ADDP_RUNTIME_LOG_ROOT':directory},'owned',problem,language,.01)

    def test_exact_scan_parent_engine_tenant_user_and_module_are_checked(self):
        for field,value in [('execution_id','foreign'),('parent_execution_id','foreign'),('tenant_id',True),('tenant_id',3),('triggered_by',34),('engine_id',True),('engine_id',4),('module','develop'),('task_type','other')]:
            class Bad(Client):
                def request(self,method,path,expected,body=None,headers=None):
                    result=super().request(method,path,expected,body,headers)
                    if '/meta/executions/' in path and not path.endswith('/source-scan'):
                        if field=='engine_id':result.payload['scan_context']['engine_id']=value
                        else:result.payload[field]=value
                    return result
            with self.subTest(field=field,value=value),self.assertRaises(m.SuiteError):self.run_scene(Bad())

    def test_oracle_preparation_and_cleanup_are_inside_progress_and_all_429_checks_remain(self):
        report=self.run_scene();cache=report['oracle_cache']
        self.assertEqual((cache['verification_calls'],cache['full_target_checks']),(39,429));self.assertTrue(cache['removed'])
        stages=report['stage_progress']['stages'];self.assertEqual(stages[0]['stage'],'identity');self.assertEqual(stages[1]['stage'],'oracle_prepare');self.assertEqual(stages[-1]['stage'],'oracle_cleanup')
        self.assertEqual(len(stages),83)
        measured=[s for s in stages if s['stage']in('execution','physical','negative')]
        self.assertTrue(all(s['substage_ms']for s in measured));self.assertTrue(all('physical_profile_ms'in s for s in stages if s['stage']in('physical','negative')))

    def test_oracle_failures_cleanup_and_preserve_first_failure(self):
        for fault in ('prepare','business','cleanup','both'):
            calls=[];client=Client()
            def run_oracle(repo,env,action):
                calls.append(action)
                if action=='prepare-oracle'and fault in ('prepare','both'):raise m.SuiteError('first preparation failure')
                if action=='cleanup-oracle'and fault in ('cleanup','both'):raise m.SuiteError('cleanup failure')
                return oracle(repo,env,action)
            def run_physical(*args):
                if fault=='business':raise m.SuiteError('first business failure')
                return physical(*args)
            with tempfile.TemporaryDirectory()as directory:
                with self.assertRaises(m.SuiteError)as caught:m.run_scenario(Path('.'),{**ENV,'ADDP_ONLINE_ARTIFACT_DIR':directory},client,run_physical,diagnostic,lambda e,i,t,p,w:client.executions[i]['metadata']['lineage_facts'],run_oracle)
                self.assertEqual(calls,['prepare-oracle','cleanup-oracle'])
                if fault in ('prepare','both'):self.assertIn('first preparation failure',str(caught.exception));self.assertFalse(client.executions)
                if fault=='both':self.assertIn('cleanup also failed',str(caught.exception))
                if fault=='business':self.assertIn('first business failure',str(caught.exception))
                progress=json.loads((Path(directory)/'tri-progress.json').read_text());self.assertEqual(progress['result'],'failed')

    def test_cache_and_timing_proofs_cannot_be_omitted_or_rebound(self):
        for field,value in [('scope_sha256','c'*64),('read_only_expected',False),('read_only_expected',1),('expected_arrays_loaded',True),('full_target_checks',0)]:
            native=physical(None,ENV,['tri.cog.tif']);native['oracle_cache'][field]=value
            with self.subTest(field=field),self.assertRaises(m.SuiteError):m.check_physical(native,{},'a'*64)
        for key in ('verification_timing_ms','host_timing_ms','worker_total_ms'):
            native=physical(None,ENV,['tri.cog.tif']);native.pop(key)
            with self.subTest(key=key),self.assertRaises(m.SuiteError):m.check_physical(native,{},'a'*64)

if __name__=='__main__':unittest.main()
