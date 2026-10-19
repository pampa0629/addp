import copy
import importlib
import json
from pathlib import Path
from types import SimpleNamespace
import unittest

m=importlib.import_module('scripts.test.raster-zonal-statistics-persistence-online')
ENV={'ADDP_ONLINE_TEST_TENANT_ID':'2','ADDP_ONLINE_ZONAL_STATISTICS_NFS_ENGINE_ID':'3','ADDP_ONLINE_TEST_RUN_ID':'zonal-statistics-123-1'}


def preview(name):
    rows=m.fixture.expected_rows(name)
    return {'type':'pandas_dataframe','schema':[{'name':c,'type':'int64' if i<4 else 'Float64'} for i,c in enumerate(m.fixture.COLUMNS)],
        'row_count':len(rows),'preview_limit':50,'preview_rows':rows[:50],'truncated':len(rows)>50}


def physical(repository,env,names):
    return {'runtime_workspaces_removed':True,'targets':{n:{'size_bytes':2048,'sha256':n,
        'complete_table_verified':True,'schema':list(m.fixture.COLUMNS),'row_count':len(m.fixture.expected_rows(n))} for n in names}}


class Client:
    actor=33
    corrupt_size=False
    failure_publishes=False
    failure_scans=False
    wrong_preview=False
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
                facts={'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.'+t['id'],'locator':t['params']['locator']} for t in tasks if 'locator' in t['params']],
                    'outputs':[{'port':'output.save','locator':target,'write_mode':'create'}]}
                result=preview(name)
                if self.wrong_preview:result['preview_rows'][0]['sum']=987654
                self.executions[identifier]={'status':'success','outputs':{'save':{'resource':{'locator':target,'type':'file','write_mode':'create'}}},
                    'metadata':{'lineage_facts':facts,'result':{'final_result':result,'meta_scan_runs':[{'status':'submitted','target_locator':target,'execution_id':scan}]}}}
                item_id=20+len(self.targets)
                self.targets[item_id]={'id':item_id,'item_type':'file','full_name':'outputs/'+name,'fingerprint':name,
                    'size_bytes':2049 if self.corrupt_size else 2048,'scanned_at':'2026-10-19T00:00:00Z','attributes':{'item':{'format':'csv'}}}
            data={'execution_id':identifier}
        elif '/develop/executions/' in path:data=self.executions[path.rsplit('/',1)[-1]]
        elif '/monitor/executions/' in path:data={'module':'develop',**self.executions[path.rsplit('/',1)[-1]]}
        elif path.endswith('/engines/3/items'):data=[*self.sources.values(),*self.targets.values()]
        elif '/meta/items/' in path:
            item_id=int(path.rsplit('/',1)[-1]);data=next((s for s in self.sources.values() if s['id']==item_id),self.targets.get(item_id))
        elif '/meta/lineage/graph?' in path:
            item_id=int(path.split('item_id=')[1].split('&')[0]);name=self.targets[item_id]['full_name'].split('/')[-1]
            data={'edges':[{'source':{'item_id':s['id']},'target':{'item_id':item_id},'status':'active','relation_kind':'derive',
                'evidence':{'execution_id':'exec-'+name}} for s in self.sources.values()]}
        elif '/meta/scan/runs?' in path:data={'total':len(self.scans),'items':[{'execution_id':s} for s in self.scans]}
        else:raise AssertionError((method,path,body))
        return SimpleNamespace(payload=copy.deepcopy(data))


class ZonalPersistenceTest(unittest.TestCase):
    def test_four_exact_public_dags_and_negative_parameters(self):
        sources={name:name for name in m.fixture.SOURCES}
        for name in m.fixture.TARGETS:
            tasks=m.definition(sources,'parent',name)['tasks']
            self.assertEqual(tasks[-1]['operator'],'save');self.assertEqual(tasks[-1]['params']['mode'],'fail')
            self.assertEqual(tasks[-2]['operator'],'raster_zonal_statistics');self.assertEqual(tasks[-2]['params']['band'],2)
        self.assertEqual(m.definition(sources,'parent','classified.csv',invalid='band')['tasks'][-2]['params']['zones_band'],99)
        self.assertEqual(m.definition(sources,'parent','classified.csv',invalid='grid')['tasks'][2]['operator'],'raster_resample')
        for name in ['../outside.csv','unknown.csv']:
            with self.assertRaises(m.SuiteError):m.definition(sources,'parent',name)

    def test_complete_scene_retains_twelve_permissions_two_source_lineage_and_four_scans(self):
        client=Client();report=m.run_scenario(Path('.'),ENV,client,physical)
        self.assertEqual(report['result'],'passed');self.assertEqual([len(c['lineage']) for c in report['cases']],[2]*4)
        self.assertEqual(len(report['identity']['permissions_verified']),12)
        self.assertEqual(len(client.scans),5);self.assertEqual(len(report['failed_parameters']),2)
        self.assertEqual(len([c for c in client.calls if c[0]=='POST' and c[1].endswith('/scan/run/manual')]),1)

    def test_strict_sizes_identity_full_preview_and_failure_publication_are_fail_closed(self):
        for attr,value in [('corrupt_size',True),('actor',True),('actor','33'),('actor',34),('failure_publishes',True),('failure_scans',True),('wrong_preview',True)]:
            client=Client();setattr(client,attr,value)
            with self.subTest(attr=attr,value=value),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,client,physical)

    def test_preview_rejects_unbounded_missing_or_wrong_schema_and_fake_null(self):
        for kind in ['extra','type','schema','null','count','limit','truncated']:
            value=preview('classified.csv')
            if kind=='extra':value['preview_rows'].append(value['preview_rows'][0])
            elif kind=='type':value['row_count']=True
            elif kind=='schema':value['schema'][0]['type']='float64'
            elif kind=='null':value['preview_rows'][-1]['sum']=0
            elif kind=='count':value['preview_rows'][0]['valid_count']=-1
            elif kind=='limit':value['preview_limit']=51
            else:value['truncated']=False if len(value['preview_rows'])>50 else True
            with self.subTest(kind=kind),self.assertRaises(m.SuiteError):m.validate_preview(value,'classified.csv')

    def test_prior_file_change_missing_workspace_or_content_evidence_are_rejected(self):
        for kind in ['prior','workspace','content','row_count']:
            def verify(repository,env,names):
                result=physical(repository,env,names)
                if kind=='workspace':result.pop('runtime_workspaces_removed')
                elif kind=='content':result['targets'][names[0]]['complete_table_verified']=False
                elif kind=='row_count':result['targets'][names[0]]['row_count']+=1
                elif len(names)>1:result['targets'][names[0]]['sha256']='changed'
                return result
            with self.subTest(kind=kind),self.assertRaises(m.SuiteError):m.run_scenario(Path('.'),ENV,Client(),verify)

    def test_size_types_values_and_csv_identity_are_strict(self):
        name='classified.csv';native=physical(None,None,[name])['targets'][name]
        valid={'size_bytes':2048,'scanned_at':'now','item_type':'file','full_name':'outputs/'+name,'attributes':{'item':{'format':'csv'}}}
        for size in (True,0,-1,'2048',2049):
            with self.subTest(size=size),self.assertRaises(m.SuiteError):m.validate_item({**valid,'size_bytes':size},native,name)
        for size in (True,0,'2048'):
            with self.subTest(physical_size=size),self.assertRaises(m.SuiteError):m.validate_item(valid,{**native,'size_bytes':size},name)
        with self.assertRaises(m.SuiteError):m.validate_item({**valid,'attributes':{'item':{'format':'tiff'}}},native,name)

    def test_diagnostic_uses_suite_specific_filename_without_another_implementation(self):
        self.assertTrue(issubclass(m.DiagnosticClient,m.rasterize.DiagnosticClient))
        self.assertEqual(m.DiagnosticClient.diagnostic_name,'zonal-statistics-submission-error.json')
