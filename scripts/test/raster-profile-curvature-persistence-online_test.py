import copy
import importlib
from pathlib import Path
from types import SimpleNamespace
import unittest

m=importlib.import_module('scripts.test.raster-profile-curvature-persistence-online')
ENV={'ADDP_ONLINE_TEST_TENANT_ID':'2','ADDP_ONLINE_PROFILE_CURVATURE_NFS_ENGINE_ID':'3','ADDP_ONLINE_TEST_RUN_ID':'profile-curvature-123-1'}


def physical(repository,env,names):
    return {'runtime_workspaces_removed':True,'mean_controls_equal':all(n in names for n in ('mean-default.cog.tif','mean-explicit.cog.tif')),'targets':{name:{'size_bytes':2048,'sha256':name,'cog_valid':True,
        'has_overviews':name not in ('small.cog.tif','stationary.cog.tif'),'overview_count':0 if name in ('small.cog.tif','stationary.cog.tif') else 3,
        'curvature_method':m.fixture.CASES[name][4] or 'mean','mean_control_verified':m.fixture.CASES[name][4] in (None,'mean'),
        'flat_centres':(m.fixture.HEIGHT-2)*(m.fixture.WIDTH-2) if name=='flat.cog.tif' else 0,
        'undefined_stationary_centres':1 if name=='stationary.cog.tif' else 0,
        **{k:True for k in ('full_pixel_and_mask_verification','independent_quadratic_oracle_verified','complete_neighbourhood_verified','inverse_metre_unit_verified','nearest_overviews_verified','nine_equal_weights_verified','flat_zero_verified','zero_gradient_nodata_verified')}} for name in names}}


def diagnostic(repository,env,identifier,problem,language,timeout):
    return {'execution_id':identifier,'task':'curvature','cause':'invalid_'+problem,'source':'develop_runtime_log','language':language}


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
                facts={'schema_version':'addp.lineage-facts/v1','inputs':[{'locator':t['params']['locator']} for t in tasks if 'locator' in t['params']],
                    'outputs':[{'locator':target,'write_mode':'create'}]}
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
        return SimpleNamespace(payload=copy.deepcopy(data))


class ProfileCurvaturePersistenceTest(unittest.TestCase):
    def test_twelve_exact_dags_explicit_band_unit_and_canonical_save(self):
        sources={n:n for n in m.fixture.SOURCES};canonical=m.raster.workflow('source',3,'b1','create')['tasks'][-1]['params']
        for name in m.fixture.TARGETS:
            tasks=m.definition(sources,'parent',name)['tasks'];source,band,unit,stage,method=m.fixture.CASES[name]
            self.assertEqual(set(tasks[-1]['params'])-{'overview_resampling'},set(canonical))
            self.assertEqual(tasks[-1]['params']['write_mode'],'create');self.assertEqual(tasks[-1]['params']['overview_resampling'],'nearest')
            self.assertEqual(tasks[-2]['operator'],'raster_curvature');params=tasks[-2]['params']
            self.assertEqual(set(params),{'input_raster','band','elevation_unit'}|({'method'} if method is not None else set()))
            self.assertEqual(params.get('method'),method)
            self.assertEqual(params['band'],1 if stage!='direct' else band);self.assertEqual(params['elevation_unit'],unit)
            self.assertEqual(m.case_inputs(name),[source]);self.assertEqual(len(tasks),3 if stage=='direct' else 4)
            self.assertEqual(tasks[0]['params']['locator'],source)
            if stage=='fill':
                self.assertEqual(tasks[1]['operator'],'raster_fill_depressions')
                self.assertEqual(tasks[1]['params'],{'input_raster':{'$ref':'source'},'band':2,'elevation_unit':unit})
                self.assertEqual(params['input_raster'],{'$ref':'fill'})
            if stage=='reclassify':
                self.assertEqual(tasks[1]['operator'],'raster_reclassify');self.assertEqual(tasks[1]['params']['band'],2)
                self.assertEqual(params['input_raster'],{'$ref':'classes'})
        self.assertEqual(len(m.FAILURES),13)
        for problem in m.FAILURES:
            for language in ('zh-cn','en'):
                tasks=m.definition(sources,'parent','profile.cog.tif',invalid=problem,language=language)['tasks']
                self.assertEqual(tasks[-2]['params']['band'],99 if problem=='band' else 4 if problem=='alpha' else 2)
                self.assertEqual(tasks[-1]['params']['target_name'],f'failed-{problem}-{language}.cog.tif')
                self.assertEqual(tasks[-2]['params']['method'],'bad' if problem=='method' else 'profile')
        for name in ('../outside.tif','unknown.tif'):
            with self.assertRaises(m.SuiteError):m.definition(sources,'parent',name)

    def test_complete_scene_preserves_twelve_permissions_twelve_exact_scans_and_current_source_lineage(self):
        client=Client();report=m.run_scenario(Path('.'),ENV,client,physical,diagnostic)
        self.assertEqual(report['result'],'passed');self.assertEqual([len(c['lineage']) for c in report['cases']],[1]*12)
        self.assertEqual(len(client.scans),13);self.assertEqual(len(report['failed_parameters']),26)
        self.assertFalse({item['id'] for item in client.sources.values()} & set(client.targets))
        self.assertEqual(len(report['identity']['permissions_verified']),12)
        self.assertEqual(len([c for c in client.calls if c[0]=='POST' and c[1].endswith('/scan/run/manual')]),1)

    def test_strict_sizes_safe_scan_identity_and_failure_publication_are_fail_closed(self):
        for attr,value in [('corrupt_size',True),('actor',True),('actor','33'),('actor',34),('failure_publishes',True),('failure_scans',True)]:
            client=Client();setattr(client,attr,value)
            with self.subTest(attr=attr,value=value),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,client,physical,diagnostic)

    def test_physical_prior_file_change_missing_workspace_and_incomplete_content_are_rejected(self):
        for kind in ['prior','workspace','full_pixel_and_mask_verification','cog_valid','has_overviews','independent_quadratic_oracle_verified','complete_neighbourhood_verified','inverse_metre_unit_verified','nearest_overviews_verified','nine_equal_weights_verified','flat_zero_verified','zero_gradient_nodata_verified']:
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
            for size in (True,0,-1,'2048',2048.0,2049):
                entries=[{'size_bytes':2048} for _ in range(3)];entries[position]['size_bytes']=size
                with self.subTest(position=position,size=size),self.assertRaises(m.SuiteError):m.raster.strict_sizes(*entries,'clean.tif')

    def test_original_object_output_contract_remains_strict(self):
        client=Client();m.run_scenario(Path('.'),ENV,client,physical,diagnostic)
        execution=client.executions['exec-profile.cog.tif']
        target=execution['outputs']['save']['resource']['locator']
        sources=[i['locator'] for i in execution['metadata']['lineage_facts']['inputs']]
        with self.assertRaisesRegex(m.SuiteError,'wrong locator/type'):
            m.raster.validate_success(execution,sources,target,'create',m.fixture.artifact_expectation('profile.cog.tif'))

    def test_diagnostic_reuses_sanitizer_with_suite_specific_filename(self):
        self.assertTrue(issubclass(m.DiagnosticClient,m.rasterize.DiagnosticClient))
        self.assertEqual(m.DiagnosticClient.diagnostic_name,'profile-curvature-submission-error.json')

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
                message='[DevExecutor] 工作流引擎调用失败: execution_id=exec-123 err=任务 curvature 执行失败: '+m.MESSAGES['band'][language=='en']
                original={'module_name':'develop','role':'backend','message':message};path.write_text(json.dumps(original)+'\n')
                self.assertEqual(m.invalid_diagnostic(root,{},'exec-123','band',language,1)['task'],'curvature')
                for field,value in [('module_name','meta'),('role','worker'),('message',message.replace('exec-123','exec-1234')),
                                    ('message',message.replace('任务 curvature','任务 save')),('message',message.replace(m.MESSAGES['band'][language=='en'],m.MESSAGES['rotated'][language=='en']))]:
                    event={**original,field:value};path.write_text(json.dumps(event)+'\n')
                    with self.subTest(field=field,value=value),patch.object(m.time,'sleep'),self.assertRaises(m.SuiteError):
                        m.invalid_diagnostic(root,{},'exec-123','band',language,.002)

    def test_all_twenty_six_failures_use_real_submission_language_headers_and_unique_targets(self):
        client=Client();report=m.run_scenario(Path('.'),ENV,client,physical,diagnostic)
        self.assertEqual({(f['problem'],f['language']) for f in report['failed_parameters']},{(p,l) for p in m.FAILURES for l in ('zh-cn','en')})
        submitted=[h['Accept-Language'] for method,path,h in client.headers if method=='POST' and path=='/api/v1/develop/executions' and h]
        self.assertEqual(submitted,['zh-cn','en']*13)
        self.assertEqual(len(client.executions),38)


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
                if '/develop/executions/exec-profile.cog.tif' in path:
                    item=self.sources['bowl.tif']
                    response.payload['metadata']['lineage_facts']['inputs'].append({'locator':f'addp://engine/3/path/bowl.tif?type=file&item_id={item["id"]}'})
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

    def test_small_image_does_not_require_nonexistent_overviews(self):
        client=Client();report=m.run_scenario(Path('.'),ENV,client,physical,diagnostic)
        small=next(c for c in report['cases'] if c['name']=='small.cog.tif')
        self.assertFalse(small['physical']['has_overviews']);self.assertEqual(small['physical']['overview_count'],0)
        for value in (True,1,None,'0'):
            def corrupted(repository,env,names):
                result=physical(repository,env,names)
                if 'small.cog.tif' in names:result['targets']['small.cog.tif']['overview_count']=value
                return result
            with self.subTest(value=value),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Client(),corrupted,diagnostic)


    def test_semantic_method_and_stationary_counts_are_strict(self):
        for name,key,value in [('profile.cog.tif','curvature_method','mean'),('mean-default.cog.tif','curvature_method','profile'),('mean-default.cog.tif','mean_control_verified',False),
                ('stationary.cog.tif','undefined_stationary_centres',0),('stationary.cog.tif','undefined_stationary_centres',True),
                ('flat.cog.tif','flat_centres',0),('flat.cog.tif','flat_centres','265221')]:
            def corrupt(repository,env,names):
                result=physical(repository,env,names)
                if name in names:result['targets'][name][key]=value
                return result
            with self.subTest(name=name,key=key,value=value),self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'),ENV,Client(),corrupt,diagnostic)


    def test_default_explicit_mean_pair_requires_positive_equality_evidence(self):
        def corrupt(repository,env,names):
            result=physical(repository,env,names)
            if 'mean-explicit.cog.tif' in names:result['mean_controls_equal']=False
            return result
        with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Client(),corrupt,diagnostic)

    def test_lineage_waits_after_all_successful_writes_and_before_negative_cases(self):
        from unittest.mock import patch
        client=Client();original=m.raster.wait_lineage;checked=[]
        def wait(client,source,target,execution,timeout):
            self.assertEqual(len(client.targets),12,'do not serialize the collector tick per artifact')
            self.assertFalse(any(e['status']=='failed' for e in client.executions.values()))
            value=original(client,source,target,execution,timeout);checked.append(value);return value
        with patch.object(m.raster,'wait_lineage',side_effect=wait):
            report=m.run_scenario(Path('.'),ENV,client,physical,diagnostic)
        self.assertEqual(len(checked),12)
        self.assertEqual([c['lineage'][0] for c in report['cases']],checked)
        self.assertEqual(len(report['failed_parameters']),26)

    def test_batched_lineage_still_rejects_missing_or_wrong_current_execution_edges(self):
        from unittest.mock import patch
        for fault in ('missing','execution','source','target','status','kind','truncated'):
            class Wrong(Client):
                def request(self,method,path,expected,body=None,headers=None):
                    response=super().request(method,path,expected,body,headers)
                    if '/meta/lineage/graph?' in path:
                        self.assertion_hook()
                        if fault=='missing':response.payload['edges']=[]
                        elif fault=='truncated':response.payload['truncated']=True
                        else:
                            for edge in response.payload['edges']:
                                if fault=='execution':edge['evidence']['execution_id']='prior-execution'
                                if fault=='source':edge['source']['item_id']=-1
                                if fault=='target':edge['target']['item_id']=-1
                                if fault=='status':edge['status']='stale'
                                if fault=='kind':edge['relation_kind']='serve'
                    return response
                def assertion_hook(self):
                    if len(self.targets)!=12:raise AssertionError('lineage was not deferred')
            client=Wrong()
            with self.subTest(fault=fault),patch.object(m.raster.time,'sleep'),self.assertRaises(m.SuiteError):
                m.run_scenario(Path('.'),{**ENV,'ADDP_ONLINE_TEST_TIMEOUT_SECONDS':'.002'},client,physical,diagnostic)
            self.assertFalse(any(e['status']=='failed' for e in client.executions.values()))

    def test_closed_progress_preserves_all_success_and_negative_stage_counts(self):
        import json,tempfile
        from collections import Counter
        with tempfile.TemporaryDirectory() as directory:
            env={**ENV,'ADDP_ONLINE_ARTIFACT_DIR':directory,'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN':'private-token-never-record'}
            report=m.run_scenario(Path('.'),env,Client(),physical,diagnostic)
            path=Path(directory)/m.CurvatureProgress.SUITES[m.SUITE]
            raw=path.read_text();record=json.loads(raw)
            self.assertNotIn('private-token-never-record',raw)
            self.assertEqual(set(record),{'schema_version','suite','result','elapsed_ms','stages'})
            self.assertEqual(record['result'],'passed');self.assertEqual(record['suite'],m.SUITE)
            self.assertEqual(record['stages'],report['stage_progress']['stages'])
            self.assertEqual(Counter(s['stage'] for s in record['stages']),
                {'source_scan':1,'execution':12,'automatic_scan':12,'physical':12,'lineage':12,'negative':26,'source_metadata':1})
            for stage in record['stages']:
                self.assertEqual(set(stage),{'stage','case_index','status','started_ms','duration_ms'})
                self.assertEqual(stage['status'],'passed');self.assertGreaterEqual(stage['duration_ms'],0)
            self.assertFalse(path.with_suffix('.tmp').exists())

    def test_progress_records_running_then_failed_physical_stage_without_private_error(self):
        import json,tempfile
        with tempfile.TemporaryDirectory() as directory:
            env={**ENV,'ADDP_ONLINE_ARTIFACT_DIR':directory};path=Path(directory)/m.CurvatureProgress.SUITES[m.SUITE]
            def broken(repository,env,names):
                record=json.loads(path.read_text());self.assertEqual(record['result'],'running')
                self.assertEqual(record['stages'][-1]['stage'],'physical')
                self.assertEqual(record['stages'][-1]['status'],'running')
                self.assertEqual(record['stages'][-1]['case_index'],1)
                raise m.SuiteError('private SQL/password/metadata must never enter progress')
            with self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),env,Client(),broken,diagnostic)
            raw=path.read_text();record=json.loads(raw)
            self.assertNotIn('private SQL',raw);self.assertEqual(record['result'],'failed')
            self.assertEqual(record['stages'][-1]['status'],'failed')
            self.assertEqual([s['stage'] for s in record['stages']],['source_scan','execution','automatic_scan','physical'])

    def test_progress_rejects_unknown_stage_case_or_suite_before_writing(self):
        with self.assertRaises(m.SuiteError):m.CurvatureProgress({},'foreign-suite')
        progress=m.CurvatureProgress({},m.SUITE)
        for stage,index in [('private value',1),('lineage',True),('physical',-1),('negative',27)]:
            with self.subTest(stage=stage,index=index),self.assertRaises(m.SuiteError):
                with progress.stage(stage,index):pass
        self.assertEqual(progress.stages,[])
