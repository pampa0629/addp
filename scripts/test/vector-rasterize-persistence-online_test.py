import copy
import importlib
from pathlib import Path
from types import SimpleNamespace
import unittest

m=importlib.import_module('scripts.test.vector-rasterize-persistence-online')
ENV={'ADDP_ONLINE_TEST_TENANT_ID':'2','ADDP_ONLINE_RASTERIZE_NFS_ENGINE_ID':'3','ADDP_ONLINE_TEST_RUN_ID':'rasterize-123-1'}


def physical(repository,env,names):
    return {'runtime_workspaces_removed':True,'targets':{name:{'size_bytes':2048,'sha256':name,'cog_valid':True,'has_overviews':False} for name in names}}


def target_item(name,identifier):
    expected=m.fixture.artifact_expectation()
    return {'id':identifier,'item_type':'file','full_name':'outputs/'+name,'fingerprint':name,'size_bytes':2048,'scanned_at':'2026-10-19T00:00:00Z',
        'attributes':{'item':{'format':'tiff'},'type_info':{'media':{'width':8,'height':6}},
            'format_info':{'tiff':{'profile':'geotiff','is_tiled':True,'has_overviews':False}},
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
        elif '/meta/executions/' in path:data={'status':'success','tenant_id':2,'triggered_by':self.actor}
        elif path.endswith('/workflow-engines'):data=[{'id':7,'engine_type':'geopython_workflow','connection_status':'online'}]
        elif path=='/api/v1/develop/executions' and method=='POST':
            tasks=body['content']['workflow_definition']['tasks'];name=tasks[-1]['params']['target_name'];identifier='exec-'+name
            if tasks[-1]['params'].get('write_mode')!='create' or 'mode' in tasks[-1]['params']:
                raise AssertionError('raster_save requires the formal write_mode parameter')
            if name=='invalid.cog.tif':
                self.executions[identifier]={'status':'failed','outputs':{'save':{}} if self.failure_publishes else {},'metadata':{'result':{}}}
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
            identifier=int(path.split('item_id=')[1].split('&')[0]);name=self.targets[identifier]['full_name'].split('/')[-1]
            used=['reference.tif'] if name=='roundtrip.cog.tif' else list(self.sources)
            data={'edges':[{'source':{'item_id':self.sources[n]['id']},'target':{'item_id':identifier},'status':'active','relation_kind':'derive',
                'evidence':{'execution_id':'exec-'+name}} for n in used]}
        elif '/meta/scan/runs?' in path:data={'total':len(self.scans),'items':[{'execution_id':s} for s in self.scans]}
        else:raise AssertionError((method,path,body))
        return SimpleNamespace(payload=copy.deepcopy(data))


class RasterizePersistenceTest(unittest.TestCase):
    def test_raster_save_keys_match_the_existing_formal_raster_workflow(self):
        canonical=m.raster.workflow('source',3,'b1','create')['tasks'][-1]['params']
        for name in m.fixture.TARGETS:
            tasks=m.definition({n:n for n in m.fixture.SOURCES},'parent',name)['tasks']
            self.assertEqual(set(tasks[-1]['params']),set(canonical))
            self.assertEqual(tasks[-1]['params']['write_mode'],canonical['write_mode'])

    def test_three_exact_dags_and_negative_share_the_public_canonical_nodes(self):
        sources={name:name for name in m.fixture.SOURCES}
        for name in m.fixture.TARGETS:
            tasks=m.definition(sources,'parent',name)['tasks']
            self.assertEqual([t['operator'] for t in tasks],['raster_load','raster_polygonize' if name=='roundtrip.cog.tif' else 'load','vector_rasterize','raster_save'])
            self.assertEqual(tasks[-1]['params']['profile'],'cog');self.assertEqual(tasks[-1]['params']['write_mode'],'create')
            self.assertEqual(tasks[2]['params'].get('all_touched',False),name=='constant.cog.tif')
        self.assertEqual(m.definition(sources,'parent','field.cog.tif',invalid=True)['tasks'][2]['params']['field'],'missing_field')
        with self.assertRaises(m.SuiteError):m.definition(sources,'parent','../outside.tif')

    def test_complete_scene_uses_two_precise_sources_three_automatic_scans_and_two_source_lineage(self):
        client=Client();report=m.run_scenario(Path('.'),ENV,client,physical)
        self.assertEqual(report['result'],'passed');self.assertEqual([len(c['lineage']) for c in report['cases']],[2,2,1])
        self.assertEqual(len([c for c in client.calls if c[0]=='POST' and c[1].endswith('/scan/run/manual')]),1)
        self.assertEqual(len(client.scans),4);self.assertTrue(report['failed_parameter']['no_publication_or_scan'])
        self.assertEqual(len(report['identity']['permissions_verified']),12)

    def test_strict_sizes_safe_scan_identity_and_negative_publication_are_fail_closed(self):
        for attr,value in [('corrupt_size',True),('actor',True),('actor','33'),('actor',34),('failure_publishes',True),('failure_scans',True)]:
            client=Client();setattr(client,attr,value)
            with self.subTest(attr=attr,value=value),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,client,physical)

    def test_physical_prior_file_change_and_missing_workspace_evidence_are_rejected(self):
        for kind in ['prior','workspace']:
            def verify(repository,env,names):
                result=physical(repository,env,names)
                if kind=='workspace':result.pop('runtime_workspaces_removed')
                elif len(names)>1:result['targets'][names[0]]['sha256']='changed'
                return result
            with self.subTest(kind=kind),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Client(),verify)

    def test_original_object_target_contract_remains_strict(self):
        client=Client();m.run_scenario(Path('.'),ENV,client,physical)
        execution=client.executions['exec-field.cog.tif'];sources=[i['locator'] for i in execution['metadata']['lineage_facts']['inputs']]
        target=execution['outputs']['save']['resource']['locator']
        with self.assertRaisesRegex(m.SuiteError,'wrong locator/type'):
            m.raster.validate_success(execution,sources,target,'create',m.fixture.artifact_expectation())


class DiagnosticClientTest(unittest.TestCase):
    def test_only_failed_submission_is_retained_without_credentials_or_extra_requests(self):
        import json,tempfile,os
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as directory:
            client=m.DiagnosticClient('http://localhost:8000','owned-token-for-test',30,directory)
            response=SimpleNamespace(status=500,payload={'error':'preparation failed: password=hidden-test; Bearer owned-token-for-test https://user:private-test@host/a','secret_key':'must-not-be-copied'})
            with patch.object(m.raster.GatewayClient,'request',return_value=response) as request:
                with self.assertRaisesRegex(m.SuiteError,'sanitized'):
                    client.request('POST','/api/v1/develop/executions',(200,),{'content':{}})
                self.assertEqual(request.call_count,1);self.assertEqual(request.call_args.args[2],(200,500))
            path=Path(directory)/'rasterize-submission-error.json';record=json.loads(path.read_text())
            self.assertEqual(record['status'],500)
            for secret in ['hidden-test','owned-token-for-test','private-test','must-not-be-copied']:
                self.assertNotIn(secret,path.read_text())
            self.assertEqual(os.stat(path).st_mode & 0o777,0o600)

    def test_success_and_unrelated_routes_keep_existing_client_contract(self):
        import tempfile
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as directory:
            client=m.DiagnosticClient('http://localhost:8000','token',30,directory)
            response=SimpleNamespace(status=200,payload={})
            with patch.object(m.raster.GatewayClient,'request',return_value=response) as request:
                self.assertIs(client.request('POST','/api/v1/develop/executions',(200,),{}),response)
                self.assertEqual(request.call_args.args[2],(200,500))
                self.assertIs(client.request('GET','/api/v1/system/auth/context',(200,)),response)
                self.assertEqual(request.call_args.args[2],(200,))
            self.assertEqual(list(Path(directory).iterdir()),[])
