import copy
import importlib
from pathlib import Path
from types import SimpleNamespace
import unittest

m=importlib.import_module('scripts.test.raster-nibble-persistence-online')
ENV={'ADDP_ONLINE_TEST_TENANT_ID':'2','ADDP_ONLINE_NIBBLE_NFS_ENGINE_ID':'3','ADDP_ONLINE_TEST_RUN_ID':'nibble-123-1'}


def physical(repository,env,names):
    return {'runtime_workspaces_removed':True,'targets':{name:{'size_bytes':2048,'sha256':name,'cog_valid':True,'has_overviews':True,'full_pixel_and_mask_verification':True,'exact_integer_pixels_verified':True} for name in names}}


def target_item(name,identifier):
    expected=m.fixture.artifact_expectation()
    return {'id':identifier,'item_type':'file','full_name':'outputs/'+name,'fingerprint':name,'size_bytes':2048,'scanned_at':'2026-10-19T00:00:00Z',
        'attributes':{'item':{'format':'tiff'},'type_info':{'media':{'width':m.fixture.WIDTH,'height':m.fixture.HEIGHT}},
            'format_info':{'tiff':{'profile':'cog','is_tiled':True,'has_overviews':True}},
            'capabilities':{'spatial':{'srid':3857,'extent':expected['extent']}}}}


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
            if name.startswith('failed-'):
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
            used=list(self.sources)
            data={'edges':[{'source':{'item_id':self.sources[n]['id']},'target':{'item_id':identifier},'status':'active','relation_kind':'derive',
                'evidence':{'execution_id':'exec-'+name}} for n in used]}
        elif '/meta/scan/runs?' in path:data={'total':len(self.scans),'items':[{'execution_id':s} for s in self.scans]}
        else:raise AssertionError((method,path,body))
        return SimpleNamespace(payload=copy.deepcopy(data))


class NibblePersistenceTest(unittest.TestCase):
    def test_three_exact_dags_reclassification_and_invalid_parameters_use_canonical_save(self):
        sources={n:n for n in m.fixture.SOURCES}
        canonical=m.raster.workflow('source',3,'b1','create')['tasks'][-1]['params']
        for name in m.fixture.TARGETS:
            tasks=m.definition(sources,'parent',name)['tasks']
            self.assertEqual(set(tasks[-1]['params']),set(canonical))
            self.assertEqual(tasks[-1]['params']['write_mode'],'create')
            self.assertEqual(tasks[-2]['operator'],'raster_nibble')
            self.assertEqual(tasks[-2]['params']['target_values'],m.fixture.CASES[name])
            self.assertEqual(tasks[-2]['params']['band'],1 if name.startswith('reclassified') else 2)
            self.assertEqual(len(tasks),4 if name.startswith('reclassified') else 3)
        for fault in ('band','unsafe','no-donor'):
            params=m.definition(sources,'parent','nibble.cog.tif',invalid=fault)['tasks'][-2]['params']
            self.assertEqual(params['band'],{'band':99,'unsafe':4,'no-donor':5}[fault])
            self.assertEqual(params['target_values'],[0] if fault=='no-donor' else [7,8])
        for name in ('../outside.tif','unknown.tif'):
            with self.assertRaises(m.SuiteError):m.definition(sources,'parent',name)

    def test_complete_scene_preserves_twelve_permissions_three_exact_scans_and_current_source_lineage(self):
        client=Client();report=m.run_scenario(Path('.'),ENV,client,physical)
        self.assertEqual(report['result'],'passed');self.assertEqual([len(c['lineage']) for c in report['cases']],[1]*3)
        self.assertEqual(len(client.scans),4);self.assertEqual(len(report['failed_parameters']),3)
        self.assertEqual(len(report['identity']['permissions_verified']),12)
        self.assertEqual(len([c for c in client.calls if c[0]=='POST' and c[1].endswith('/scan/run/manual')]),1)

    def test_strict_sizes_safe_scan_identity_and_failure_publication_are_fail_closed(self):
        for attr,value in [('corrupt_size',True),('actor',True),('actor','33'),('actor',34),('failure_publishes',True),('failure_scans',True)]:
            client=Client();setattr(client,attr,value)
            with self.subTest(attr=attr,value=value),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,client,physical)

    def test_physical_prior_file_change_missing_workspace_and_incomplete_content_are_rejected(self):
        for kind in ['prior','workspace','full_pixel_and_mask_verification','cog_valid','has_overviews','exact_integer_pixels_verified']:
            def verify(repository,env,names):
                result=physical(repository,env,names)
                if kind=='workspace':result.pop('runtime_workspaces_removed')
                elif kind=='prior':
                    if len(names)>1:result['targets'][names[0]]['sha256']='changed'
                else:result['targets'][names[0]][kind]=False
                return result
            with self.subTest(kind=kind),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Client(),verify)

    def test_artifact_physical_and_meta_sizes_remain_strict_positive_equal_integers(self):
        for position in range(3):
            for size in (True,0,-1,'2048',2049):
                entries=[{'size_bytes':2048} for _ in range(3)];entries[position]['size_bytes']=size
                with self.subTest(position=position,size=size),self.assertRaises(m.SuiteError):m.raster.strict_sizes(*entries,'nibble.tif')

    def test_original_object_output_contract_remains_strict(self):
        client=Client();m.run_scenario(Path('.'),ENV,client,physical)
        execution=client.executions['exec-nibble.cog.tif']
        target=execution['outputs']['save']['resource']['locator']
        sources=[i['locator'] for i in execution['metadata']['lineage_facts']['inputs']]
        with self.assertRaisesRegex(m.SuiteError,'wrong locator/type'):
            m.raster.validate_success(execution,sources,target,'create',m.fixture.artifact_expectation())

    def test_diagnostic_reuses_sanitizer_with_suite_specific_filename(self):
        self.assertTrue(issubclass(m.DiagnosticClient,m.rasterize.DiagnosticClient))
        self.assertEqual(m.DiagnosticClient.diagnostic_name,'nibble-submission-error.json')
