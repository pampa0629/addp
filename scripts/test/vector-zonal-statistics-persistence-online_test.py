import copy
import importlib
import json
from pathlib import Path
from types import SimpleNamespace
import unittest

m=importlib.import_module('scripts.test.vector-zonal-statistics-persistence-online')
ENV={'ADDP_ONLINE_TEST_TENANT_ID':'2','ADDP_ONLINE_VECTOR_ZONAL_STATISTICS_NFS_ENGINE_ID':'3','ADDP_ONLINE_TEST_RUN_ID':'vector-zonal-statistics-123-1'}


def physical(repository,env,names):
    return {'runtime_workspaces_removed':True,'targets':{n:{'size_bytes':2048,'sha256':n,
        'complete_geometry_attributes_statistics_verified':True,'schema':[list(v) for v in m.fixture.schema(n)],
        'layer_name':n.removesuffix('.gpkg'),'feature_count':m.fixture.FEATURE_COUNTS[n],'crs':'EPSG:4326'} for n in names}}


def attributes(name):
    layer=name.removesuffix('.gpkg')
    return {'item':{'layout':'single','data_type':'container','format':'geopackage'},
        'type_info':{'container':{'children':[{'name':layer,'child_kind':'layer','native':{'table':layer}}]}}}


class Client:
    actor=33
    corrupt_size=False
    failure_publishes=False
    failure_scans=False
    wrong_lineage=False
    def __init__(self):
        self.sources={n:{'id':i+10,'item_type':'file','full_name':n,'size_bytes':1024,'fingerprint':n} for i,n in enumerate(m.fixture.SOURCES)}
        self.targets={};self.executions={};self.scans=['source-scan'];self.calls=[]
    def request(self,method,path,expected,body=None):
        self.calls.append((method,path,body))
        if path.endswith('/auth/context'):
            data={'principal':{'id':33,'type':'user'},'context':{'type':'tenant','tenant_id':'2'},'token':{'type':'first_party_access_token'},
                'authorization':{'role_assignments':[{'role_key':'raster-consumer','permissions':sorted(m.raster.EDITOR_PERMISSIONS)}]}}
        elif path.endswith('/scan/run/manual'):
            assert body['targets']==['addp://engine/3/path/'+n+'?type=file' for n in m.fixture.SOURCES]
            data={'execution_id':'source-scan'}
        elif '/meta/executions/' in path:data={'status':'success','tenant_id':2,'triggered_by':self.actor}
        elif path.endswith('/workflow-engines'):data=[{'id':7,'engine_type':'geopython_workflow','connection_status':'online'}]
        elif path=='/api/v1/develop/executions' and method=='POST':
            tasks=body['content']['workflow_definition']['tasks'];params=tasks[-1]['params'];name=params['target_name'];identifier='exec-'+name
            assert tasks[-1]['operator']=='save' and params['mode']=='fail' and 'write_mode' not in params
            if name.startswith('failed-'):
                self.executions[identifier]={'status':'failed','outputs':{'save':{}} if self.failure_publishes else {},'metadata':{'result':{}}}
                if self.failure_scans:self.scans.append('failed-scan')
            else:
                target='addp://engine/3/path/outputs/'+name+'?type=file';scan='scan-'+name;self.scans.append(scan)
                facts={'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.'+t['id'],'locator':t['params']['locator']} for t in sorted(tasks,key=lambda t:t['id']) if 'locator' in t['params']],
                    'outputs':[{'port':'output.save','locator':target,'write_mode':'create'}]}
                result={'type':'geodataframe'}
                if self.wrong_lineage:facts['inputs'].pop()
                self.executions[identifier]={'status':'success','outputs':{'save':{'resource':{'locator':target,'type':'file','write_mode':'create'}}},
                    'metadata':{'lineage_facts':facts,'result':{'final_result':result,'meta_scan_runs':[{'status':'submitted','target_locator':target,'execution_id':scan}]}}}
                item_id=20+len(self.targets)
                self.targets[item_id]={'id':item_id,'item_type':'file','full_name':'outputs/'+name,'fingerprint':name,
                    'size_bytes':2049 if self.corrupt_size else 2048,'scanned_at':'2026-10-19T00:00:00Z','attributes':attributes(name)}
            data={'execution_id':identifier}
        elif '/develop/executions/' in path:data=self.executions[path.rsplit('/',1)[-1]]
        elif '/monitor/executions/' in path:data={'module':'develop',**self.executions[path.rsplit('/',1)[-1]]}
        elif path.endswith('/engines/3/items'):data=[*self.sources.values(),*self.targets.values()]
        elif '/meta/items/' in path:
            item_id=int(path.rsplit('/',1)[-1]);data=next((s for s in self.sources.values() if s['id']==item_id),self.targets.get(item_id))
        elif '/meta/lineage/graph?' in path:
            item_id=int(path.split('item_id=')[1].split('&')[0]);name=self.targets[item_id]['full_name'].split('/')[-1]
            data={'edges':[{'source':{'item_id':s['id']},'target':{'item_id':item_id},'status':'active','relation_kind':'derive',
                'evidence':{'execution_id':'exec-'+name}} for n,s in self.sources.items() if n in m.used_sources(name)]}
        elif '/meta/scan/runs?' in path:data={'total':len(self.scans),'items':[{'execution_id':s} for s in self.scans]}
        else:raise AssertionError((method,path,body))
        return SimpleNamespace(payload=copy.deepcopy(data))


class VectorZonalPersistenceTest(unittest.TestCase):
    def test_three_exact_public_dags_and_negative_parameters(self):
        sources={name:name for name in m.fixture.SOURCES}
        for name in m.fixture.TARGETS:
            tasks=m.definition(sources,'parent',name)['tasks']
            self.assertEqual(tasks[-1]['operator'],'save');self.assertEqual(tasks[-1]['params']['mode'],'fail')
            self.assertEqual(tasks[-2]['operator'],'raster_vector_zonal_statistics');self.assertEqual(tasks[-2]['params']['band'],2)
            self.assertEqual([t['params']['locator'] for t in sorted(tasks,key=lambda t:t['id']) if 'locator' in t['params']],list(m.used_sources(name)))
        classified=m.definition(sources,'parent','classified.gpkg')['tasks']
        self.assertEqual([t['operator'] for t in classified],['raster_load','raster_reclassify','raster_polygonize','raster_vector_zonal_statistics','save'])
        self.assertEqual(m.definition(sources,'parent','touched.gpkg')['tasks'][-2]['params']['field_prefix'],'stats_')
        for fault,value in [('band',99),('collision','occupied_')]:
            params=m.definition(sources,'parent','center.gpkg',invalid=fault)['tasks'][-2]['params']
            self.assertEqual(params['band' if fault=='band' else 'field_prefix'],value)
        for name in ['../outside.gpkg','unknown.gpkg']:
            with self.assertRaises(m.SuiteError):m.definition(sources,'parent',name)

    def test_scene_preserves_twelve_permissions_all_used_inputs_and_exact_scans(self):
        client=Client();report=m.run_scenario(Path('.'),ENV,client,physical)
        self.assertEqual(report['result'],'passed');self.assertEqual([len(c['lineage']) for c in report['cases']],[2,2,1])
        self.assertEqual(len(report['identity']['permissions_verified']),12)
        self.assertEqual(len(client.scans),4);self.assertEqual(len(report['failed_parameters']),2)
        self.assertEqual(len([c for c in client.calls if c[0]=='POST' and c[1].endswith('/scan/run/manual')]),1)

    def test_strict_sizes_scan_identity_all_inputs_and_failure_publication_fail_closed(self):
        for attr,value in [('corrupt_size',True),('actor',True),('actor','33'),('actor',34),('failure_publishes',True),('failure_scans',True),('wrong_lineage',True)]:
            client=Client();setattr(client,attr,value)
            with self.subTest(attr=attr,value=value),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,client,physical)

    def test_prior_file_change_missing_workspace_or_content_evidence_are_rejected(self):
        for kind in ['prior','workspace','content','schema','count','crs']:
            def verify(repository,env,names):
                result=physical(repository,env,names);fact=result['targets'][names[0]]
                if kind=='workspace':result.pop('runtime_workspaces_removed')
                elif kind=='content':fact['complete_geometry_attributes_statistics_verified']=False
                elif kind=='schema':fact['schema'][0][1]='Real'
                elif kind=='count':fact['feature_count']=True
                elif kind=='crs':fact['crs']='EPSG:3857'
                elif len(names)>1:fact['sha256']='changed'
                return result
            with self.subTest(kind=kind),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Client(),verify)

    def test_physical_and_meta_size_types_values_and_identity_are_strict(self):
        name='center.gpkg';native=physical(None,None,[name])['targets'][name]
        valid={'size_bytes':2048,'scanned_at':'now','item_type':'file','full_name':'outputs/'+name,'attributes':attributes(name)}
        for size in (True,0,-1,'2048',2049):
            with self.subTest(size=size),self.assertRaises(m.SuiteError):m.validate_item({**valid,'size_bytes':size},native,name)
        for size in (True,0,'2048'):
            with self.subTest(physical_size=size),self.assertRaises(m.SuiteError):m.validate_item(valid,{**native,'size_bytes':size},name)
        with self.assertRaises(m.SuiteError):m.validate_item({**valid,'attributes':{'item':{'format':'tiff'}}},native,name)

    def test_diagnostic_reuses_sanitizer_with_suite_specific_filename(self):
        self.assertTrue(issubclass(m.DiagnosticClient,m.rasterize.DiagnosticClient))
        self.assertEqual(m.DiagnosticClient.diagnostic_name,'vector-zonal-statistics-submission-error.json')

    def test_lineage_requires_canonical_node_order_and_rejects_duplicates_or_missing_reads(self):
        client=Client();m.run_scenario(Path('.'),ENV,client,physical)
        execution=client.executions['exec-center.gpkg']
        target=execution['outputs']['save']['resource']['locator']
        facts=execution['metadata']['lineage_facts'];expected=[r['locator'] for r in facts['inputs']]
        self.assertEqual([r['port'] for r in facts['inputs']],['input.faces','input.values'])
        for inputs in (list(reversed(facts['inputs'])),facts['inputs'][:1],facts['inputs']+[facts['inputs'][0]]):
            changed=copy.deepcopy(execution);changed['metadata']['lineage_facts']['inputs']=inputs
            with self.subTest(inputs=inputs),self.assertRaises(m.SuiteError):m.validate_success(changed,expected,target)
