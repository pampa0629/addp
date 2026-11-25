import copy
import struct
import importlib
from pathlib import Path
import tempfile
import types
import sys
import unittest
from unittest import mock

SCENE=importlib.import_module('scripts.test.manager-grib-online')
FIXTURE=SCENE.FIXTURE

class GRIBOracleTest(unittest.TestCase):
    def test_closed_inventory_and_bytes_before_physical_mutation(self):
        request={'sources':FIXTURE.sources(),'source_bytes':{s['name']:FIXTURE.source_bytes(s).hex() for s in FIXTURE.sources()}}
        self.assertEqual(len(FIXTURE._owned_sources(request)),11)
        for change in ('missing','tampered'):
            bad=copy.deepcopy(request)
            if change=='missing':bad['sources'].pop()
            else:bad['source_bytes'][FIXTURE.NAMES[0]]='00'
            with self.assertRaises(ValueError):FIXTURE._owned_sources(bad)
        for spec in FIXTURE.sources():
            raw=FIXTURE.source_bytes(spec);offset=0;count=0
            while offset<len(raw):
                self.assertEqual(raw[offset:offset+4],b'GRIB');self.assertEqual(raw[offset+7],spec.get('edition',2))
                length=int.from_bytes(raw[offset+4:offset+7] if spec.get('edition')==1 else raw[offset+8:offset+16],'big');self.assertEqual(raw[offset+length-4:offset+length],b'7777');offset+=length;count+=1
            self.assertEqual((offset,count),(len(raw),len(spec['fields'])))
        self.assertEqual([s['sha256'] for s in FIXTURE.sources()[:2]],['98d4150bb752fbdd54219060d67e7f3446e0263c6fff1bd2fbe8d4992f61f110','addd8a56405b421b3645e592808af3c576ee501ed325e93651f30a891ca4dce6'])

    def test_temperature_map_has_two_pinned_official_messages(self):
        raw = FIXTURE.map_source(Path(__file__).resolve().parents[2])
        offset = 0
        for length in (1350847, 1349013):
            self.assertEqual(raw[offset:offset+4], b'GRIB')
            self.assertEqual(int.from_bytes(raw[offset+8:offset+16], 'big'), length)
            offset += length
            self.assertEqual(raw[offset-4:offset], b'7777')
        self.assertEqual(len(raw), offset)
        self.assertNotIn(FIXTURE.MAP_KEY.split('/')[-1], FIXTURE.NAMES)

    def test_map_proof_rejects_shared_cache_wrong_version_or_missing_range(self):
        version = 'sha256:' + FIXTURE.MAP_SHA
        tasks = [dict(message=f'message-{i}', task_id=i, execution_id=f'exec-{i}') for i in (1, 2)]
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            visits = []
            for step, i in enumerate((1,2,1,2), 1):
                path = f'/api/v1/manager/raster_cog/{i}/content'
                screenshot = f'grib-temperature-map-{step}.png'
                (root/screenshot).write_bytes(b'\x89PNG\r\n\x1a\n')
                visits.append(dict(message=f'message-{i}', preview_url=path+'?source_version='+version,
                    screenshot=screenshot, ranges=[dict(path=path, version=version, range='bytes 0-63/256')]))
            proof = dict(run_id='run', locator='item', source_version=version, result='passed', generations=tasks, visits=visits)
            self.assertEqual(len(SCENE.map_browser_artifacts(proof,'run','item',root)), 2)
            for key, value in (('preview_url',visits[1]['preview_url']),('ranges',[]),('message','message-2')):
                bad = copy.deepcopy(proof)
                bad['visits'][2][key] = value
                with self.assertRaises(ValueError):
                    SCENE.map_browser_artifacts(bad,'run','item',root)

    def response(self,spec,field):
        selection=FIXTURE.selection(field)
        values,valid=FIXTURE.expected(field,selection['x'],selection['y'],selection['width'],selection['height'])
        reference_time=spec.get('reference_time',1767322800)
        spacing=field.get('spacing',1)
        result = dict(schema_version='gdal.grib.slice/v1',format='grib',source_version='sha256:'+spec['sha256'],value_mode='decoded_native_units',
            selection=selection,values=values,valid=valid,spatial=None,
            variable=dict(attributes={'GRIB_UNIT':{'values':['[K]']},'GRIB_REF_TIME':{'values':[str(reference_time)]},'GRIB_VALID_TIME':{'values':[str(reference_time+field['forecast']*3600)]},'GRIB_SHORT_NAME':{'values':['2-HTGL']},'GRIB_ELEMENT':{'values':['TMP']},'GRIB_FORECAST_SECONDS':{'values':[str(field['forecast']*3600)]}},
              dimensions=[dict(name='y',size=str(field['height']),axis_type='HORIZONTAL_Y',direction='NORTH_TO_SOUTH'),dict(name='x',size=str(field['width']),axis_type='HORIZONTAL_X',direction='WEST_TO_EAST')],
              grid=dict(transform=[field['west']-spacing/2,spacing,0.,field['north']+spacing/2,0.,-spacing],longitude_policy='unchanged',crs_wkt='source CRS')))

        if spec.get('edition')==1:
            from datetime import datetime, timezone
            utc=lambda h:datetime.fromtimestamp(reference_time+h*3600,timezone.utc).isoformat()
            level={'type_code':field['level'],'raw_octets':field['level_raw']}
            level.update({100:dict(unit='hPa',value=500),101:dict(unit='kPa',upper=50,lower=100),105:dict(unit='m',value=2)}[field['level']])
            facts=dict(edition=1,parameter=dict(centre=7,subcentre=0,process=81,table_version=2,number=11),level=level,
                time=dict(unit_code=1,range_indicator=field['time_indicator'],p1=field['p1'],p2=field['p2'],operation='accumulation' if field['time_indicator']==4 else 'instant',reference_utc=utc(0),
                    start_utc=utc(field['forecast'] if field['time_indicator']==10 else field['p1']),end_utc=utc(field['forecast']),included_count=2 if field['time_indicator']==4 else 0,missing_count=0),
                packing=dict(bits_per_value=field['bits'],original_integer=False,binary_scale=field['binary_scale'],decimal_scale=field['decimal_scale'],reference_ibm_hex={0.:'00000000',-5.:'c1500000',280.:'43118000'}[field['reference']]))
            result['variable']['message']=facts
            import json
            for key in ('parameter','time','level','packing'):result['variable']['attributes']['GRIB1_'+key.upper()]={'values':[json.dumps(facts[key])]}
        if spec.get('packing')=='complex-spatial':
            import json
            from datetime import datetime,timezone
            utc=lambda h:datetime.fromtimestamp(reference_time+h*3600,timezone.utc).isoformat()
            a=result['variable']['attributes'];a['GRIB2_DECODE_PRECISION']={'values':['float32_scaling_float64_output']};a['GRIB_UNIT']['values']=[field['unit']];a['GRIB_ELEMENT']['values']=[field['element']];a['GRIB_SHORT_NAME']['values']=[field['short_name']];a['GRIB_FORECAST_SECONDS']['values']=[str(field['start_hour']*3600)]
            time=dict(reference_utc=utc(0),unit_code=1,forecast_value=field['start_hour'],start_utc=utc(field['start_hour']),end_utc=utc(field['forecast']),operation='instant')
            if field['product_template']==8:time.update(operation='accumulation',statistical_process=1,increment_type=2,interval_unit=1,interval_length=field['forecast']-field['start_hour'],missing_count=0)
            level=dict(type_code=103 if field['height_m'] else 1,raw_scale=0,raw_value=field['height_m'],reference='above_ground' if field['height_m'] else 'surface')
            if field['height_m']:level.update(value=field['height_m'],unit='m')
            facts=dict(edition=2,data_template=3,product_template=field['product_template'],parameter=dict(centre=7,subcentre=0,master_table=2,local_table=field.get('local_table',0),discipline=0,category=field['parameter'][0],number=field['parameter'][1]),time=time,level=level,
                packing=dict(data_template=3,order=2,extra_octets=2,groups=field.get('groups',(field['width']*field['height']-int(field['missing'])+3)//4),missing_value_management=0,reference_ieee_hex=struct.pack('>f',field['reference']).hex(),binary_scale=field['binary_scale'],decimal_scale=field['decimal_scale']))
            result['variable']['message']=facts
            for key in ('parameter','time','level','packing'):a['GRIB2_'+key.upper()]={'values':[json.dumps(facts[key])]}
        return result

    def test_grib1_oracle_rejects_lost_time_layer_parameter_or_packing(self):
        for spec in FIXTURE.sources()[2:4]:
            for field in spec['fields']:
                result=self.response(spec,field);SCENE.validate_slice(result,spec,field)
                for category,key,value in [('time','start_utc','wrong'),('time','range_indicator',99),('level','raw_octets',[0,0]),('parameter','table_version',128),('packing','binary_scale',1)]:
                    bad=copy.deepcopy(result);bad['variable']['message'][category][key]=value
                    with self.assertRaises(ValueError):SCENE.validate_slice(bad,spec,field)

    def test_complex_oracle_preserves_unit_packing_and_cumulative_interval(self):
        for spec in FIXTURE.sources()[4:6]:
            for field in spec['fields']:
                result=self.response(spec,field);SCENE.validate_slice(result,spec,field)
                for category,key,value in [('time','start_utc','wrong'),('time','forecast_value',99),('level','raw_value',99),('parameter','number',99),('packing','order',1),('packing','groups',99)]:
                    bad=copy.deepcopy(result);bad['variable']['message'][category][key]=value
                    with self.assertRaises(ValueError):SCENE.validate_slice(bad,spec,field)

    def test_complex_oracle_rejects_false_precision_or_one_float32_ulp(self):
        spec=FIXTURE.sources()[4];field=spec['fields'][1]
        result=self.response(spec,field);SCENE.validate_slice(result,spec,field)
        bad=copy.deepcopy(result);bad['variable']['attributes']['GRIB2_DECODE_PRECISION']['values']=['Float64']
        with self.assertRaises(ValueError):SCENE.validate_slice(bad,spec,field)
        bad=copy.deepcopy(result);v=float(bad['values'][0][0]);bits=int.from_bytes(struct.pack('>f',v),'big')
        bad['values'][0][0]=repr(struct.unpack('>f',(bits+1).to_bytes(4,'big'))[0])
        with self.assertRaises(ValueError):SCENE.validate_slice(bad,spec,field)

    def test_original_noaa_windows_reject_stale_values_and_time_on_switch(self):
        spec = next(spec for spec in FIXTURE.sources() if spec['name']==FIXTURE.NOAA_INTERVALS)
        first, second = spec['fields']
        old, new = self.response(spec,first), self.response(spec,second)
        SCENE.validate_slice(old,spec,first)
        SCENE.validate_slice(new,spec,second)
        self.assertEqual(old['variable']['message']['time']['end_utc'],new['variable']['message']['time']['end_utc'])
        self.assertNotEqual(old['values'],new['values'])
        with self.assertRaisesRegex(ValueError,'numerical values'):
            SCENE.validate_slice(dict(new,values=old['values']),spec,second)
        stale = copy.deepcopy(new)
        stale['variable']['message']['time'] = old['variable']['message']['time']
        with self.assertRaisesRegex(ValueError,'original interval'):
            SCENE.validate_slice(stale,spec,second)
        stale = copy.deepcopy(new)
        stale['variable']['attributes']['GRIB2_TIME'] = old['variable']['attributes']['GRIB2_TIME']
        with self.assertRaisesRegex(ValueError,'displayed facts'):
            SCENE.validate_slice(stale,spec,second)

    def test_acceptance_rejects_unit_value_mask_longitude_and_message_drift(self):
        spec=FIXTURE.sources()[0];field=spec['fields'][1];result=self.response(spec,field);SCENE.validate_slice(result,spec,field)
        self.assertEqual((result['values'][0][0],result['valid'][0][0]),('9999.0',True))
        for change in [lambda r:r.__setitem__('value_mode','raw_storage'),lambda r:r['values'][0].__setitem__(1,'7.1'),lambda r:r['valid'][0].__setitem__(0,False),lambda r:r['selection'].__setitem__('variable','message-1'),lambda r:r['variable']['grid']['transform'].__setitem__(0,-110.5),lambda r:r['variable']['attributes']['GRIB_UNIT'].__setitem__('values',['[C]'])]:
            bad=copy.deepcopy(result);change(bad)
            with self.assertRaises(ValueError):SCENE.validate_slice(bad,spec,field)

    def test_browser_dom_presence_cannot_replace_viewport_evidence(self):
        spec=FIXTURE.sources()[0];case=dict(locator='exact-source',spec=spec,source_case='minio')
        observed=dict(locator='exact-source',values_verified=True,viewport_grid_verified=True,visible_message_count=3,download_sha256=spec['sha256'],download_size=spec['size_bytes'])
        SCENE.validate_browser_case(case,observed)
        for field,value in [('viewport_grid_verified',False),('visible_message_count',2),('download_sha256','wrong')]:
            bad=dict(observed);bad[field]=value
            with self.assertRaises(ValueError):SCENE.validate_browser_case(case,bad)
        del observed['viewport_grid_verified']
        with self.assertRaises(ValueError):SCENE.validate_browser_case(case,observed)

    def test_structural_meta_rejects_content_and_derived_statistics(self):
        detail=dict(attributes=dict(schema_version=1,item=dict(data_type='container',format='grib',layout='single'),
            type_info=dict(container=dict(children=[dict(name='message-'+str(i),child_kind='array',data_type='media') for i in range(1,4)]))))
        names = ['message-1', 'message-2', 'message-3']
        SCENE.validate_structural_metadata(detail, names)
        for path,key,value in [(('attributes',),'format_info',{'grib':{'source_version':'hash'}}),
            (('attributes','type_info','container','children',0),'native',{'values':[9999]}),
            (('attributes','type_info','container'),'child_count',3)]:
            bad=copy.deepcopy(detail);obj=bad
            for step in path:obj=obj[step]
            obj[key]=value
            with self.assertRaises(ValueError):SCENE.validate_structural_metadata(bad, names)

    def test_grants_precede_consumer_catalog_and_cleanup_survives_business_failure(self):
        events=[]
        class Grants:
            operator='8';recipient='7'
            def __init__(self,*args,**options):
                if options!={'scan_preparation':True}:raise AssertionError('GRIB requires exact scan preparation')
            def grant(self,engine,name,storage,**identity):
                events.append(('grant',engine,name,storage,identity));return {'action':'metadata_read'}
            def cleanup(self):events.append(('revoke',));return dict(grants_revoked=True,metadata_hidden=True)
        def find(*args):
            self.assertEqual(events[-1][0],'grant')
            raise ValueError('consumer catalog failure')
        def physical(action):
            events.append((action,))
            if action=='seed':return {'sources':FIXTURE.sources()}
            self.assertEqual(action,'delete');return dict(objects_removed=True,files_removed=True)
        client=mock.Mock();client.request.return_value=types.SimpleNamespace(payload=[])
        owner=types.SimpleNamespace(GatewayClient=mock.Mock(return_value=client),positive_int=lambda value,label:int(value),
            validate_user_identity=lambda *args:{'principal_id':'7'},wait_for_meta_scan=mock.Mock(return_value='controlled-initialization'),find_fixture_item=find)
        with tempfile.TemporaryDirectory() as d:
            env=dict(ADDP_ONLINE_MANAGER_CASE='native-grib',ADDP_ONLINE_ARTIFACT_DIR=d,ADDP_ONLINE_TEST_RUN_ID='test',
                GATEWAY_URL='http://local',ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer',ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='issuer',
                ADDP_ONLINE_TEST_TENANT_ID='2',ADDP_ONLINE_MANAGER_MINIO_BUCKET='bucket',ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID='12',ADDP_ONLINE_MANAGER_NFS_ENGINE_ID='14')
            with mock.patch.object(SCENE.REGISTRATION,'ExactFileMetadataGrants',Grants):
                with self.assertRaisesRegex(ValueError,'consumer catalog failure'):
                    SCENE.run(Path('/repo'),env,owner,physical_runner=physical)
            import json
            report=json.loads((Path(d)/'manager-grib.json').read_text())
            self.assertEqual(report['result'],'failed');self.assertEqual(report['stage'],'metadata_grant')
            self.assertEqual(report['metadata_authorization']['cleanup'],dict(grants_revoked=True,metadata_hidden=True,details_hidden=0))
            self.assertEqual(events[-2:],[('revoke',),('delete',)])

    def test_non_hosted_guard_never_starts_container(self):
        with mock.patch.object(FIXTURE.subprocess,'run') as run:
            with self.assertRaises(ValueError):FIXTURE.physical('seed',Path('/repository'),{})
            run.assert_not_called()

    def test_cleanup_keeps_unrelated_sources(self):
        removed=[]
        class Client:
            def __init__(self,*args,**kwargs):pass
            def remove_object(self,bucket,name):removed.append((bucket,name))
            def list_objects(self,*args,**kwargs):return []
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);(root/'grib').mkdir();(root/'unrelated').write_text('keep')
            for name in FIXTURE.NAMES:(root/'grib'/name).write_bytes(b'owned')
            with mock.patch.dict(sys.modules,{'minio':types.SimpleNamespace(Minio=Client)}):
                result=FIXTURE.worker({'storage':dict(endpoint='local',access_key='a',secret_key='b',bucket='test'),'nfs_root':str(root),'action':'delete'})
            self.assertEqual(result,dict(objects_removed=True,files_removed=True));self.assertEqual((root/'unrelated').read_text(),'keep')
            self.assertEqual(removed,[('test','grib/'+name) for name in FIXTURE.NAMES])


    def test_nfs_initialization_uses_qualified_issuer_and_minio_keeps_consumer(self):
        observed=[];consumer=mock.Mock();issuer=mock.Mock()
        class Grants:
            operator='8';recipient='7'
            def __init__(self,authorizer,reader,tenant,**options):
                assert authorizer is issuer and reader is consumer and tenant==2
                assert options=={'scan_preparation':True}
            def cleanup(self):return dict(grants_revoked=True,metadata_hidden=True)
        def scan(client,engine,deadline,**options):
            observed.append((client,engine,options['scope']))
            if engine==14:
                self.assertIs(client,issuer)
                raise ValueError('verified NFS initialization identity')
            self.assertIs(client,consumer)
            return 'minio-initialization'
        def physical(action):
            if action=='seed':return {'sources':[]}
            self.assertEqual(action,'delete');return dict(objects_removed=True,files_removed=True)
        owner=types.SimpleNamespace(GatewayClient=lambda url,token,timeout:issuer if token=='issuer' else consumer,
            positive_int=lambda value,label:int(value),validate_user_identity=lambda *args:{'principal_id':'7'},wait_for_meta_scan=scan)
        with tempfile.TemporaryDirectory() as directory:
            env=dict(ADDP_ONLINE_MANAGER_CASE='native-grib',ADDP_ONLINE_ARTIFACT_DIR=directory,ADDP_ONLINE_TEST_RUN_ID='identity-only',GATEWAY_URL='http://local',
                ADDP_ONLINE_TEST_USER_ACCESS_TOKEN='consumer',ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN='issuer',ADDP_ONLINE_TEST_TENANT_ID='2',
                ADDP_ONLINE_MANAGER_MINIO_BUCKET='bucket',ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID='12',ADDP_ONLINE_MANAGER_NFS_ENGINE_ID='14')
            # No message assertions are mocked into a passing business report:
            # stop immediately at the NFS scan after observing both callers.
            with mock.patch.object(FIXTURE,'sources',return_value=[]),mock.patch.object(SCENE.REGISTRATION,'ExactFileMetadataGrants',Grants):
                with self.assertRaisesRegex(ValueError,'verified NFS initialization identity'):SCENE.run(Path('/repo'),env,owner,physical_runner=physical)
            import json
            report=json.loads((Path(directory)/'manager-grib.json').read_text())
            self.assertEqual(report['result'],'failed')
            self.assertEqual(report['source_scans'],[dict(storage='minio',engine_id=12,catalog_paths=['bucket/grib'],principal_id='7',execution_id='minio-initialization')])
            self.assertEqual(len(report['metadata_authorization']['initializer_permissions_verified']),13)
        self.assertEqual([(engine,scope) for _,engine,scope in observed],[(12,{'catalog_paths':['bucket/grib']}),(14,{'catalog_paths':['grib']})])


    def test_nfs_original_denial_cannot_masquerade_as_a_download(self):
        spec=FIXTURE.sources()[2];raw=FIXTURE.source_bytes(spec)
        good=types.SimpleNamespace(status=403,headers={'Content-Type':'application/json'},payload={'error':'access denied'},raw=b'{"error":"access denied"}')
        self.assertEqual(SCENE.validate_original_download(good,'nfs',spec),dict(http_status=403,json_error=True,attachment=False))
        for key,value in [('status',200),('headers',{'Content-Type':'application/octet-stream'}),('headers',{'Content-Type':'application/json','Content-Disposition':'attachment; filename=source.grib1'}),('payload',{'ok':True})]:
            bad=copy.deepcopy(good);setattr(bad,key,value)
            with self.assertRaises(ValueError):SCENE.validate_original_download(bad,'nfs',spec)
        original=types.SimpleNamespace(status=200,raw=raw)
        self.assertEqual(SCENE.validate_original_download(original,'minio',spec)['sha256'],spec['sha256'])
        original.raw=raw[:-1]
        with self.assertRaises(ValueError):SCENE.validate_original_download(original,'minio',spec)
        case=dict(locator='nfs-source',source_case='nfs',spec=spec)
        observed=dict(locator='nfs-source',values_verified=True,viewport_grid_verified=True,visible_message_count=3,original_download_disabled=True,original_download_denied_verified=True)
        SCENE.validate_browser_case(case,observed)
        for key,value in [('original_download_disabled',False),('original_download_denied_verified',False),('download_sha256',spec['sha256']),('download_size',spec['size_bytes'])]:
            with self.assertRaises(ValueError):SCENE.validate_browser_case(case,dict(observed,**{key:value}))
