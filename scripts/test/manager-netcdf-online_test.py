import copy
import importlib
import struct
import tempfile
import types
import json
from pathlib import Path
import unittest
from unittest import mock

SCENE=importlib.import_module('scripts.test.manager-netcdf-online')
FIXTURE=SCENE.FIXTURE

class NetCDFOracleTest(unittest.TestCase):
    def test_exact_fixture_inventory_rejects_missing_or_changed_bytes(self):
        request={'sources':FIXTURE.sources(),'source_bytes':{s['name']:FIXTURE.source_bytes(s).hex() for s in FIXTURE.sources()}}
        self.assertEqual(len(FIXTURE._owned_sources(request)),7)
        missing=copy.deepcopy(request);missing['sources'].pop()
        with self.assertRaises(ValueError):FIXTURE._owned_sources(missing)
        changed=copy.deepcopy(request);changed['source_bytes']['nc4-int64.nc']='00'
        with self.assertRaises(ValueError):FIXTURE._owned_sources(changed)

    def test_byte_oracle_classic_and_netcdf4_signatures_and_exact_int64(self):
        self.assertEqual([s['version'] for s in FIXTURE.sources()],[1,2,5,4,4,4,4])
        for spec in FIXTURE.sources():
            raw=FIXTURE.source_bytes(spec)
            if spec['version']==4:self.assertEqual(raw[:8],b'\x89HDF\r\n\x1a\n')
            else:self.assertEqual(raw[:4],b'CDF'+bytes([spec['version']]))
            self.assertEqual(len(raw),spec['size_bytes'])
        self.assertIn(struct.pack('>q',9007199254740993),FIXTURE.build(5))
        self.assertEqual(FIXTURE.expected_values(FIXTURE.sources()[2],1,1,1,2,2),[['9007199254741010','9007199254741011'],['9007199254741014','9007199254741015']])

    def response(self,spec,layer=0):
        return dict(schema_version='gdal.netcdf.slice/v1',source_version='sha256:'+spec['sha256'],format='netcdf',value_mode='raw_storage',
          selection=dict(variable='/temperature',indices=[layer],x=0,y=0,width=4,height=3),values=FIXTURE.expected_values(spec,layer),
          valid=[[not(layer==0 and y==1 and x==2) for x in range(4)] for y in range(3)],
          variable={'attributes':{'scale_factor':{'values':['0.5']},'add_offset':{'values':['273.15']}}},
          spatial={'transform':[499995.,10.,0.,2999995.,0.,10.],'footprint':{'geometry':{'type':'Polygon'}}} if spec['projected'] else None)

    def test_acceptance_rejects_precision_mask_grid_and_identity_drift(self):
        for spec in FIXTURE.sources():
            result=self.response(spec);SCENE.validate_slice(result,spec,0)
            for edit in [lambda r:r['values'][0].__setitem__(0,'lost'),lambda r:r['valid'][1].__setitem__(2,True),lambda r:r.__setitem__('source_version','wrong'),lambda r:r['selection'].__setitem__('indices',[1])]:
                bad=copy.deepcopy(result);edit(bad)
                with self.assertRaises(ValueError):SCENE.validate_slice(bad,spec,0)
            bad=copy.deepcopy(result)
            if spec['projected']:bad['spatial']['transform'][1]=100
            else:bad['spatial']={'srid':4326}
            with self.assertRaises(ValueError):SCENE.validate_slice(bad,spec,0)

    def test_structural_metadata_excludes_native_values_and_statistics(self):
        detail=dict(attributes=dict(schema_version=1,item=dict(data_type='container',format='netcdf',layout='single'),
            type_info=dict(container=dict(children=[dict(name='/temperature',child_kind='array',data_type='media'),dict(name='/time',child_kind='array',data_type='unknown')]))))
        SCENE.validate_structural_metadata(detail)
        for path,key,value in [(('attributes',),'format_info',{'netcdf':{'source_version':'hash'}}),
            (('attributes','type_info','container','children',0),'native',{'values':[1]}),
            (('attributes','type_info','container'),'child_count',2)]:
            bad=copy.deepcopy(detail);obj=bad
            for step in path:obj=obj[step]
            obj[key]=value
            with self.assertRaises(ValueError):SCENE.validate_structural_metadata(bad)
        bad=copy.deepcopy(detail);bad['attributes']['type_info']['container']['children'].append(copy.deepcopy(bad['attributes']['type_info']['container']['children'][0]))
        with self.assertRaises(ValueError):SCENE.validate_structural_metadata(bad)

    def test_browser_report_rejects_missing_mask_time_and_download_proof(self):
        spec=FIXTURE.sources()[0];case=dict(locator='exact-source',spec=spec)
        observed=dict(locator='exact-source',values_verified=True,mask_verified=True,time_index_button_verified=True,download_sha256=spec['sha256'],download_size=spec['size_bytes'])
        SCENE.validate_browser_case(case,observed)
        for key,value in [('mask_verified',False),('time_index_button_verified',False),('download_sha256','wrong')]:
            bad=dict(observed);bad[key]=value
            with self.assertRaises(ValueError):SCENE.validate_browser_case(case,bad)

    def test_exact_metadata_grant_precedes_user_catalog_and_is_cleaned_on_failure(self):
        events=[]
        class Grants:
            operator='8';recipient='7'
            def __init__(self,*args):pass
            def grant(self,engine,name,storage,**identity):
                self_test.assertEqual(identity,dict(data_type='container',format_name='netcdf',layout='single'))
                events.append(('grant',engine,name));return {'action':'metadata_read'}
            def cleanup(self):events.append(('revoke',));return dict(grants_revoked=True,metadata_hidden=True)
        self_test=self
        def find(*args):
            self.assertEqual(events[-1][0],'grant')
            raise ValueError('consumer catalog failure')
        def physical(action):
            events.append((action,))
            if action=='seed':return {'sources':FIXTURE.sources()}
            self.assertEqual(action,'delete');return dict(objects_removed=True,files_removed=True)
        client=mock.Mock();client.request.return_value=types.SimpleNamespace(payload=[])
        owner=types.SimpleNamespace(GatewayClient=mock.Mock(return_value=client),positive_int=lambda value,label:int(value),
            validate_user_identity=lambda *args:{'principal_id':'7'},wait_for_meta_scan=mock.Mock(),find_fixture_item=find)
        with tempfile.TemporaryDirectory() as d:
            env=dict(ADDP_ONLINE_MANAGER_CASE='native-netcdf',ADDP_ONLINE_ARTIFACT_DIR=d,ADDP_ONLINE_TEST_RUN_ID='test',
                GATEWAY_URL='http://local',ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer',ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='issuer',
                ADDP_ONLINE_TEST_TENANT_ID='2',ADDP_ONLINE_MANAGER_MINIO_BUCKET='bucket',ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID='12',ADDP_ONLINE_MANAGER_NFS_ENGINE_ID='14')
            with mock.patch.object(SCENE.REGISTRATION,'ExactFileMetadataGrants',Grants):
                with self.assertRaisesRegex(ValueError,'consumer catalog failure'):
                    SCENE.run(Path('/repo'),env,owner,physical_runner=physical)
            report=json.loads((Path(d)/'manager-netcdf.json').read_text())
            self.assertEqual(report['result'],'failed');self.assertEqual(report['schema_version'],'addp.manager-netcdf/v2')
            self.assertEqual(report['stage'],'metadata_grant')
            self.assertEqual(report['metadata_authorization']['cleanup'],dict(grants_revoked=True,metadata_hidden=True,details_hidden=0))
            self.assertEqual(events[-2:],[('revoke',),('delete',)])

    def test_full_owner_flow_uses_protected_metadata_and_withdraws_all_fourteen_grants(self):
        from urllib.parse import urlsplit,parse_qs
        specs=FIXTURE.sources();items={};approved=set();events=[];drift=[False];deleted=[False]
        def item_key(locator):
            return int(parse_qs(urlsplit(locator).query)['item_id'][0])
        class Grants:
            operator='8';recipient='7'
            def __init__(self,*args):pass
            def grant(self,engine,name,storage,**identity):
                approved.add((engine,name));events.append(('grant',engine,name))
                return dict(action='metadata_read',engine_id=str(engine),full_name=name)
            def cleanup(self):approved.clear();events.append(('revoke',));return dict(grants_revoked=True,metadata_hidden=True)
        def attributes(spec):
            return dict(item=dict(data_type='container',format='netcdf',layout='single'),
                format_info=dict(netcdf=dict(source_version='sha256:'+spec['sha256'],scan_complete=True,encoding=spec['encoding'],composition=dict(profile='root_numeric_v1',complete=True))),
                type_info=dict(container=dict(children=[dict(name='/temperature',child_kind='array',data_type='media',native=dict(type=spec['native_type'],dimensions=[dict(name=n,size=v) for n,v in [('/time','2'),('/y','3'),('/x','4')]])),
                    dict(name='/time',child_kind='array',data_type='unknown',native=dict(attributes=dict(calendar=dict(values=['360_day'])),slice_supported=False))])))
        def detail(row):
            children=[{k:v[k] for k in ('name','child_kind','data_type')} for v in attributes(row['spec'])['type_info']['container']['children']]
            return dict(id=row['id'],attributes=dict(schema_version=1,item=dict(data_type='container',format='netcdf',layout='single'),type_info=dict(container=dict(children=children))))
        class Client:
            def request(this,method,path,status,body=None):
                self.assertEqual(method,'GET');url=urlsplit(path);query=parse_qs(url.query)
                if url.path.startswith('/api/v1/meta/engines/'):
                    engine=int(url.path.split('/')[5]);return types.SimpleNamespace(payload=[dict(full_name=r['full_name']) for r in items.values() if r['engine']==engine and (engine,r['full_name']) in approved])
                if url.path.startswith('/api/v1/meta/items/'):
                    row=items[int(url.path.split('/')[-1])]
                    visible=(row['engine'],row['full_name']) in approved
                    self.assertEqual(status,(200,) if visible else (404,))
                    return types.SimpleNamespace(payload=detail(row) if visible else {})
                locator=query['locator'][0];row=items[item_key(locator)];spec=row['spec']
                self.assertIn((row['engine'],row['full_name']),approved)
                if url.path=='/api/v1/manager/downloads/file':return types.SimpleNamespace(raw=FIXTURE.source_bytes(spec))
                self.assertEqual(url.path,'/api/v1/manager/preview')
                if 'grid_slice' not in query:return types.SimpleNamespace(payload=dict(data=dict(object=dict(attributes=attributes(spec)))))
                if status==(400,):return types.SimpleNamespace(payload=dict(error_code='grid_slice_selection_invalid'))
                if status==(409,):
                    self.assertTrue(drift[0]);return types.SimpleNamespace(payload=dict(error_code='preview_source_refresh_required'))
                selection=json.loads(query['grid_slice'][0]);layer=selection['indices'][0];x,y,w,h=[selection[k] for k in ('x','y','width','height')]
                result=self.response(spec,layer);result['selection']=selection
                result['values']=FIXTURE.expected_values(spec,layer,x,y,w,h)
                result['valid']=[[not(layer==0 and yy==1 and xx==2) for xx in range(x,x+w)] for yy in range(y,y+h)]
                if spec['projected']:result['spatial']['transform']=[500000.+(x-.5)*10.,10.,0.,3000000.+(y-.5)*10.,0.,10.]
                return types.SimpleNamespace(payload=dict(data=dict(object=dict(content=dict(json=result)))))
        def find(client,engine,name,label):
            self.assertIn((engine,name),approved)
            spec=next(s for s in specs if name.endswith('/'+s['name']));row=dict(id=len(items)+1,engine=engine,full_name=name,spec=spec)
            items[row['id']]=row;return row
        def physical(action):
            if action=='seed':return dict(sources=specs)
            if action=='drift':drift[0]=True;return {}
            if action=='restore':drift[0]=False;return {}
            if action=='verify':self.assertFalse(drift[0]);return dict(sources_unchanged=True)
            self.assertEqual(action,'delete');self.assertFalse(approved);deleted[0]=True;return dict(objects_removed=True,files_removed=True)
        client=Client()
        owner=types.SimpleNamespace(GatewayClient=lambda *args:client,positive_int=lambda value,label:int(value),
            validate_user_identity=lambda *args:dict(principal_id='7'),wait_for_meta_scan=mock.Mock(),find_fixture_item=find,
            build_item_locator=lambda engine,row:f"addp://engine/{engine}/path/{row['full_name']}?type=file&item_id={row['id']}")
        with tempfile.TemporaryDirectory() as d:
            env=dict(ADDP_ONLINE_MANAGER_CASE='native-netcdf',ADDP_ONLINE_ARTIFACT_DIR=d,ADDP_ONLINE_TEST_RUN_ID='test',
                GATEWAY_URL='http://local',ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer',ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='issuer',ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN='engine',
                ADDP_ONLINE_TEST_TENANT_ID='2',ADDP_ONLINE_MANAGER_MINIO_BUCKET='bucket',ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID='12',ADDP_ONLINE_MANAGER_NFS_ENGINE_ID='14')
            def browser(command,**options):
                self.assertNotIn('ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN',options['env']);self.assertNotIn('ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN',options['env'])
                context=json.loads(options['env']['ADDP_ONLINE_MANAGER_NETCDF_JSON']);outcomes=[]
                for case in context['cases']:
                    spec=case['spec'];outcomes.append(dict(locator=case['locator'],values_verified=True,mask_verified=True,time_index_button_verified=True,download_sha256=spec['sha256'],download_size=spec['size_bytes']))
                    for suffix in (['page','grid'] if spec['projected'] else ['page']):
                        (Path(d)/f"netcdf-{case['source_case']}-{spec['case_id']}-{suffix}.png").write_bytes(b'\x89PNG\r\n\x1a\n')
                (Path(d)/'manager-netcdf-browser.json').write_text(json.dumps(dict(schema_version='addp.manager-netcdf-browser/v1',result='passed',run_id='test',cases=outcomes,browser_errors=[])))
            with mock.patch.object(SCENE.REGISTRATION,'ExactFileMetadataGrants',Grants),mock.patch.object(SCENE.subprocess,'run',side_effect=browser):
                report=SCENE.run(Path('/repo'),env,owner,physical_runner=physical)
            self.assertEqual(report['result'],'passed');self.assertEqual(len(report['cases']),14);self.assertEqual(len(events),15)
            self.assertEqual(report['metadata_authorization']['cleanup'],dict(grants_revoked=True,metadata_hidden=True,details_hidden=14))
            self.assertTrue(deleted[0]);self.assertEqual(len(list(Path(d).glob('*-metadata.json'))),14)

    def test_physical_guard_refuses_non_hosted_before_docker(self):
        with mock.patch.object(FIXTURE.subprocess,'run') as run:
            with self.assertRaises(ValueError):FIXTURE.physical('seed',Path('/repository'),{})
            run.assert_not_called()

    def test_worker_cleanup_removes_only_named_fixture_files_and_objects(self):
        import sys,types
        class Client:
            def __init__(self,*args,**kwargs):pass
            def remove_object(self,bucket,name):removed.append((bucket,name))
            def list_objects(self,*args,**kwargs):return []
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);(root/'netcdf').mkdir();(root/'unrelated').write_text('keep');removed=[]
            for name in FIXTURE.NAMES:(root/'netcdf'/name).write_bytes(b'owned')
            with mock.patch.dict(sys.modules,{'minio':types.SimpleNamespace(Minio=Client)}):
                result=FIXTURE.worker({'storage':{'endpoint':'local','access_key':'a','secret_key':'b','bucket':'test'},'nfs_root':str(root),'action':'delete'})
            self.assertEqual(result,{'objects_removed':True,'files_removed':True})
            self.assertEqual(removed,[('test','netcdf/'+name) for name in FIXTURE.NAMES]);self.assertEqual((root/'unrelated').read_text(),'keep')
