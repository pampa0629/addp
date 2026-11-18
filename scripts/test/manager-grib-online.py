"""GRIB ordinary-user metadata, raw grid, source version and UI acceptance."""
from __future__ import annotations
import hashlib
from datetime import datetime, timezone
import importlib
import json
from pathlib import Path
import subprocess
import struct
import time
from urllib.parse import urlencode

FIXTURE=importlib.import_module('business.scripts.online-grib-fixture')
REGISTRATION=importlib.import_module('scripts.test.online-engine-registration')
PERMISSIONS={'manager.content.read','manager.data_item.read','meta.catalog.read','meta.scan_task.execute','meta.scan_task.read','monitor.execution.read'}

def require(value,message):
    if not value:raise ValueError(message)


def validate_slice(result,spec,field,x=0,y=0,width=None,height=None):
    width=field['width'] if width is None else width;height=field['height'] if height is None else height
    require(result.get('schema_version')=='gdal.grib.slice/v1' and result.get('source_version')=='sha256:'+spec['sha256'] and result.get('format')=='grib' and result.get('value_mode')=='decoded_native_units','GRIB slice identity mismatch')
    require(result['selection']==dict(variable='message-'+str(field['number']),indices=[],x=x,y=y,width=width,height=height),'GRIB exact message/window mismatch')
    values,valid=FIXTURE.expected(field,x,y,width,height)
    require(result['values']==values and result['valid']==valid,'GRIB numerical values or bitmap changed')
    variable=result['variable'];attributes=variable['attributes']
    require(attributes['GRIB_UNIT']['values']==[field.get('unit','[K]')] and attributes['GRIB_ELEMENT']['values']==[field.get('element','TMP')],'GRIB units or element changed')
    require(attributes['GRIB_FORECAST_SECONDS']['values']==[str(field.get('start_hour',field['forecast'])*3600)] and attributes['GRIB_REF_TIME']['values']==['1767322800'] and attributes['GRIB_VALID_TIME']['values']==[str(1767322800+field['forecast']*3600)] and (spec.get('edition')==1 or attributes['GRIB_SHORT_NAME']['values']==[field.get('short_name','2-HTGL')]),'GRIB reference/valid time or level changed')
    if spec.get('edition')==1:
        message=variable['message']
        require(message['edition']==1 and message['parameter']==dict(centre=7,subcentre=0,process=81,table_version=2,number=11),'GRIB1 exact parameter identity changed')
        level=message['level'];time_facts=message['time']
        require(level['type_code']==field['level'] and level['raw_octets']==field['level_raw'],'GRIB1 level code or raw bounds changed')
        require(time_facts['range_indicator']==field['time_indicator'] and time_facts['p1']==field['p1'] and time_facts['p2']==field['p2'] and time_facts['unit_code']==1,'GRIB1 time interval changed')
        require(time_facts['operation']=={0:'instant',4:'accumulation',10:'instant'}[field['time_indicator']] and time_facts['reference_utc']=='2026-01-02T03:00:00+00:00','GRIB1 time statistic changed')
        if field['level']==101:require(level['unit']=='kPa' and level['upper']==50 and level['lower']==100,'GRIB1 layer boundaries or units changed')
        if field['level']==100:require(level['unit']=='hPa' and level['value']==500,'GRIB1 single pressure level changed')
        if field['level']==105:require(level['unit']=='m' and level['value']==2,'GRIB1 height level changed')
        start_hours=field['forecast'] if field['time_indicator']==10 else field['p1']
        utc=lambda hours:datetime.fromtimestamp(1767322800+hours*3600,timezone.utc).isoformat()
        require(time_facts['start_utc']==utc(start_hours) and time_facts['end_utc']==utc(field['forecast']) and time_facts['included_count']==(2 if field['time_indicator']==4 else 0) and time_facts['missing_count']==0,'GRIB1 interval endpoints or sample counts changed')
        require(message['packing']==dict(bits_per_value=field['bits'],original_integer=False,binary_scale=field['binary_scale'],decimal_scale=field['decimal_scale'],reference_ibm_hex={0.:'00000000',-5.:'c1500000',280.:'43118000'}[field['reference']]),'GRIB1 source packing declaration changed')
        for key in ('parameter','time','level','packing'):
            require(json.loads(attributes['GRIB1_'+key.upper()]['values'][0])==message[key],'GRIB1 displayed semantics differ from exact facts')
    if spec.get('packing')=='complex-spatial':
        require(attributes.get('GRIB2_DECODE_PRECISION',{}).get('values')==['float32_scaling_float64_output'],'complex GRIB precision disclosure missing')
        FIXTURE.precision_bound(field,result['values'],result['valid'],x,y)
        message=variable['message'];utc=lambda h:datetime.fromtimestamp(1767322800+h*3600,timezone.utc).isoformat()
        require(message['edition']==2 and message['data_template']==3 and message['product_template']==field['product_template'],'complex GRIB template changed')
        require(message['parameter']==dict(centre=7,subcentre=0,master_table=2,local_table=0,discipline=0,category=field['parameter'][0],number=field['parameter'][1]),'complex GRIB parameter identity changed')
        expected_time=dict(reference_utc=utc(0),unit_code=1,forecast_value=field['start_hour'],start_utc=utc(field['start_hour']),end_utc=utc(field['forecast']),operation='instant')
        if field['product_template']==8:expected_time.update(operation='accumulation',statistical_process=1,increment_type=2,interval_unit=1,interval_length=field['forecast']-field['start_hour'],missing_count=0)
        require(message['time']==expected_time,'complex GRIB original interval changed')
        expected_level=dict(type_code=103 if field['height_m'] else 1,raw_scale=0,raw_value=field['height_m'],reference='above_ground' if field['height_m'] else 'surface')
        if field['height_m']:expected_level.update(value=field['height_m'],unit='m')
        require(message['level']==expected_level,'complex GRIB surface changed')
        points=field['width']*field['height']-int(field['missing'])
        require(message['packing']==dict(data_template=3,order=2,extra_octets=2,groups=(points+3)//4,missing_value_management=0,reference_ieee_hex=struct.pack('>f',field['reference']).hex(),binary_scale=field['binary_scale'],decimal_scale=field['decimal_scale']),'complex GRIB packing facts changed')
        for key in ('parameter','time','level','packing'):require(json.loads(attributes['GRIB2_'+key.upper()]['values'][0])==message[key],'complex GRIB displayed facts differ')
    if spec.get('packing') == 'ccsds':
        message = variable['message']
        cumulative = field.get('product_template') == 8
        initial_zero = cumulative and field['forecast'] == 0
        require(message['edition'] == 2 and message['data_template'] == 42
                and message['product_template'] == field.get('product_template', 0),
                'ECMWF CCSDS templates changed')
        end = datetime.fromtimestamp(1767322800 + field['forecast'] * 3600, timezone.utc).isoformat()
        start = '2026-01-02T03:00:00+00:00' if cumulative else end
        expected_facts = dict(
            parameter=dict(centre=98, subcentre=0, master_table=32, local_table=int(cumulative),
                           discipline=0, category=field['parameter'][0], number=field['parameter'][1]),
            time=dict(reference_utc='2026-01-02T03:00:00+00:00', unit_code=1,
                      forecast_value=field.get('start_hour',field['forecast']), start_utc=start,
                      end_utc=end, operation='instant'),
            packing=dict(data_template=42, bits_per_value=0 if initial_zero else 12 if cumulative else 8,
                         flags=14, block_size=32,
                         reference_sample_interval=128, reference_ieee_hex=struct.pack('>f', field['base']).hex(),
                         binary_scale=field.get('binary_scale',-2), decimal_scale=0),
            local=dict(definition=1, section_bytes=17, mars_class='od', mars_type='fc',
                       mars_stream='oper', experiment_version='0001'),
        )
        if cumulative:
            expected_facts['time'].update(operation='accumulation', statistical_process=1, increment_type=2,
                                          interval_unit=1, interval_length=field['forecast'], increment_unit=13,
                                          increment_value=450, missing_count=0)
            expected_facts['level'] = dict(type_code=1, raw_scale=None, raw_value=None, reference='surface')
        else:
            height_m = field['height_m']
            expected_facts['level'] = dict(type_code=103, raw_scale=0, raw_value=height_m,
                                           reference='above_ground', value=height_m, unit='m')
        for key, expected in expected_facts.items():
            require(message[key] == expected and json.loads(attributes['GRIB2_' + key.upper()]['values'][0]) == expected,
                    f'ECMWF CCSDS {key} differs from source')
        precision = 'exact_zero_float64_output' if initial_zero else 'float32_scaling_float64_output'
        require(attributes['GRIB2_DECODE_PRECISION']['values'] == [precision],
                'ECMWF CCSDS precision disclosure missing')
    require(variable['dimensions']==[dict(name='y',size=str(field['height']),axis_type='HORIZONTAL_Y',direction='NORTH_TO_SOUTH'),dict(name='x',size=str(field['width']),axis_type='HORIZONTAL_X',direction='WEST_TO_EAST')],'GRIB independent dimensions changed')
    spacing = field.get('spacing', 1)
    require(variable['grid']['transform']==[field['west']-spacing/2,spacing,0.,field['north']+spacing/2,0.,-spacing] and variable['grid']['longitude_policy']=='unchanged' and bool(variable['grid']['crs_wkt']),'GRIB grid/longitude changed')
    require(result.get('spatial') is None,'GRIB gained unapproved map preview')


def validate_original_download(response,storage,spec):
    if storage=='nfs':
        headers={key.lower():value for key,value in response.headers.items()}
        require(response.status==403 and headers.get('content-type','').startswith('application/json')
            and 'content-disposition' not in headers and isinstance(response.payload,dict)
            and set(response.payload)=={'error'} and isinstance(response.payload['error'],str) and response.payload['error'],
            'GRIB NFS original must be a JSON denial without an attachment')
        return dict(http_status=403,json_error=True,attachment=False)
    require(response.status==200 and len(response.raw)==spec['size_bytes'] and hashlib.sha256(response.raw).hexdigest()==spec['sha256'],
        'GRIB MinIO original differs from the complete source')
    return dict(http_status=200,sha256=spec['sha256'],size_bytes=spec['size_bytes'])


def validate_browser_case(case,observed):
    require(observed.get('locator')==case['locator'] and observed.get('values_verified') is True
        and observed.get('viewport_grid_verified') is True and observed.get('visible_message_count')==len(case['spec']['fields']),
        'GRIB real visible grid incomplete')
    if case['source_case']=='nfs':
        require(observed.get('original_download_disabled') is True and observed.get('original_download_denied_verified') is True
            and 'download_sha256' not in observed and 'download_size' not in observed,'GRIB NFS download denial or disabled button incomplete')
    else:
        require(observed.get('download_sha256')==case['spec']['sha256'] and observed.get('download_size')==case['spec']['size_bytes'],
            'GRIB MinIO original button download incomplete')


def validate_structural_metadata(detail, expected_names):
    attrs=detail.get('attributes',{})
    require(set(attrs)=={'schema_version','item','type_info'} and attrs.get('schema_version')==1
        and attrs.get('item')=={'data_type':'container','format':'grib','layout':'single'},
        'GRIB Meta must expose only the canonical structural projection')
    container=attrs.get('type_info',{}).get('container',{})
    require(set(attrs['type_info'])=={'container'} and set(container)<= {'children','default_child'}
        and container.get('children')==[dict(name=name,child_kind='array',data_type='media') for name in expected_names],
        'GRIB Meta container must not disclose native content or derived statistics')


def read(client,locator,selection,status=200):
    response=client.request('GET','/api/v1/manager/preview?'+urlencode({'locator':locator,'grid_slice':json.dumps(selection,separators=(',',':'))}),(status,)).payload
    return response.get('data',{}).get('object',{}).get('content',{}).get('json') if status==200 else response


def run(repository,environment,owner,*,physical_runner=None,browser_runner=None):
    require(environment.get('ADDP_ONLINE_MANAGER_CASE')=='native-grib','GRIB requires its independent case')
    artifact=Path(environment['ADDP_ONLINE_ARTIFACT_DIR']);physical=physical_runner or (lambda action:FIXTURE.physical(action,repository,environment))
    report={'schema_version':'addp.manager-grib/v2','case':'native-grib','run_id':environment['ADDP_ONLINE_TEST_RUN_ID'],'result':'incomplete','cases':[],'cleanup':{}}
    client=owner.GatewayClient(environment['GATEWAY_URL'],environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30)
    tenant=owner.positive_int(environment['ADDP_ONLINE_TEST_TENANT_ID'],'GRIB tenant')
    report['identity']=owner.validate_user_identity(client,tenant,PERMISSIONS)
    issuer=owner.GatewayClient(environment['GATEWAY_URL'],environment['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'],30)
    grants=REGISTRATION.ExactFileMetadataGrants(issuer,client,tenant,scan_preparation=True)
    report['metadata_authorization']={'operator_principal_id':grants.operator,'recipient_principal_id':grants.recipient,'initializer_permissions_verified':sorted(REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS|{'meta.scan_task.execute','meta.scan_task.read'}),'grants':[]}
    seeded=False;stage='seed'
    try:
        seeded=True;seed=physical('seed');require(seed=={'sources':FIXTURE.sources()},'GRIB source bytes differ from the independent oracle')
        bucket=environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET']
        for storage,engine,scope,prefix in [('minio',owner.positive_int(environment['ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID'],'MinIO'),'grib',bucket+'/'),('nfs',owner.positive_int(environment['ADDP_ONLINE_MANAGER_NFS_ENGINE_ID'],'NFS'),'grib','')]:
            stage='scan'
            scan_client=issuer if storage=='nfs' else client
            scan_execution=owner.wait_for_meta_scan(scan_client,engine,time.monotonic()+180,scope={'catalog_paths':[prefix+scope]})
            report.setdefault('source_scans',[]).append(dict(storage=storage,engine_id=engine,catalog_paths=[prefix+scope],principal_id=grants.operator if storage=='nfs' else grants.recipient,execution_id=scan_execution))
            for spec in seed['sources']:
                stage='metadata_grant'
                visible=client.request('GET',f'/api/v1/meta/engines/{engine}/items',(200,)).payload
                require(isinstance(visible,list) and not any(v.get('full_name')==prefix+'grib/'+spec['name'] for v in visible),'GRIB Meta disclosed an unapproved source')
                receipt=grants.grant(engine,prefix+'grib/'+spec['name'],storage,data_type='container',format_name='grib',layout='single')
                report['metadata_authorization']['grants'].append(receipt)
                item=owner.find_fixture_item(client,engine,prefix+'grib/'+spec['name'],'GRIB '+storage)
                detail=client.request('GET',f"/api/v1/meta/items/{item['id']}",(200,)).payload
                expected_names = [f"message-{field['number']}" for field in spec['fields']]
                validate_structural_metadata(detail, expected_names)
                locator=owner.build_item_locator(engine,item)
                native=client.request('GET','/api/v1/manager/preview?'+urlencode({'locator':locator}),(200,)).payload
                attrs=native.get('data',{}).get('object',{}).get('attributes',{})
                (artifact/f"grib-{storage}-{spec['case_id']}-metadata.json").write_text(json.dumps({'meta':detail,'manager':native},ensure_ascii=False,indent=2)+'\n')
                require({k:attrs.get('item',{}).get(k) for k in ('data_type','format','layout')}=={'data_type':'container','format':'grib','layout':'single'},'GRIB canonical identity changed')
                info=attrs.get('format_info',{}).get('grib',{})
                require(info.get('source_version')=='sha256:'+spec['sha256'] and info.get('scan_complete') is True and info.get('encoding')=='GRIB'+str(spec.get('edition',2)) and info.get('profile')=='grib'+str(spec.get('edition',2))+'_regular_fields/v1','GRIB content version missing')
                require(not attrs.get('capabilities',{}).get('spatial'),'GRIB parent gained unified spatial extent')
                children=attrs.get('type_info',{}).get('container',{}).get('children',[])
                require([v['name'] for v in children]==expected_names,'GRIB message identity or enumeration changed')
                stage='slice';precision_audits=[]
                for field in spec['fields']:
                    selection=dict(variable='message-'+str(field['number']),indices=[],x=0,y=0,width=field['width'],height=field['height'])
                    result=read(client,locator,selection);validate_slice(result,spec,field)
                    if spec.get('packing')=='complex-spatial':precision_audits.append(dict(message=field['number'],**FIXTURE.precision_bound(field,result['values'],result['valid'])))
                    require(read(client,locator,selection)==result,'repeated GRIB slice changed')
                    (artifact/f"grib-{storage}-{spec['case_id']}-message{field['number']}.json").write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
                    window=dict(selection,x=1,y=1,width=1,height=1)
                    validate_slice(read(client,locator,window),spec,field,1,1,1,1)
                for change in ({'indices':[0]},{'variable':'message-01'},{'width':129}):
                    invalid=dict(selection,**change);denied=read(client,locator,invalid,400);require(denied.get('error_code')=='grid_slice_selection_invalid','invalid window was not a controlled rejection')
                stage='download'
                original=client.request('GET','/api/v1/manager/downloads/file?'+urlencode({'locator':locator}),(403 if storage=='nfs' else 200,))
                original_proof=validate_original_download(original,storage,spec)
                if storage=='minio':(artifact/f"grib-{storage}-{spec['case_id']}{Path(spec['name']).suffix}").write_bytes(original.raw)
                else:(artifact/f"grib-{storage}-{spec['case_id']}-download-denied.json").write_text(json.dumps(original_proof,indent=2)+'\n')
                require(client.request('GET',f"/api/v1/meta/items/{item['id']}",(200,)).payload==detail,'GRIB preview changed metadata')
                report['cases'].append(dict(source_case=storage,locator=locator,item_id=item['id'],spec=spec,metadata_verified=True,values_verified=True,mask_verified=True,precision_audits=precision_audits,repeat_stateless_verified=True,window_verified=True,invalid_selection_denied=True,original_download_verified=storage=='minio',original_download=original_proof))
        stage='source_drift';physical('drift')
        for case in report['cases']:
            changed=read(client,case['locator'],dict(variable='message-1',indices=[],x=0,y=0,width=4,height=3),409)
            require(changed.get('error_code')=='preview_source_refresh_required','changed GRIB did not stop preview')
        physical('restore');report['source_drift_denied']=True
        require(physical('verify').get('sources_unchanged') is True,'sources not restored exactly')
        stage = 'browser'
        browser_cases = []
        for case in report['cases']:
            windows = []
            for field in case['spec']['fields']:
                values, valid = FIXTURE.expected(field)
                windows.append(dict(variable=f"message-{field['number']}", values=values, valid=valid))
            browser_cases.append(dict(case, expected_windows=windows))
        context = dict(run_id=report['run_id'], identity=report['identity'], cases=browser_cases)
        if browser_runner:browser=browser_runner(context)
        else:
            browser_environment={k:v for k,v in environment.items() if k not in {'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN','ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'}}
            subprocess.run(['npm','exec','--','playwright','test','e2e/online/manager-grib.spec.js','--config=playwright.online.config.js'],cwd=Path(repository)/'console/frontend',env=dict(browser_environment,ADDP_ONLINE_MANAGER_GRIB_JSON=json.dumps(context)),check=True)
            browser=json.loads((artifact/'manager-grib-browser.json').read_text())
        require(browser.get('schema_version')=='addp.manager-grib-browser/v2' and browser.get('result')=='passed' and browser.get('run_id')==report['run_id'] and len(browser.get('cases',[]))==2*len(FIXTURE.sources()),'GRIB browser report incomplete')
        for case,observed in zip(report['cases'],browser['cases']):
            validate_browser_case(case,observed)
            for field in case['spec']['fields']:
                prefix = f"grib-{case['source_case']}-{case['spec']['case_id']}-message{field['number']}-slice"
                suffixes = ('.png', '-start.png') if field.get('product_template') == 8 and case['spec'].get('packing') == 'ccsds' else ('.png',)
                for suffix in suffixes:
                    screenshot = artifact / (prefix + suffix)
                    require(screenshot.is_file() and screenshot.read_bytes().startswith(b'\x89PNG\r\n\x1a\n'),
                            'GRIB visible slice screenshot missing')
        if not physical_runner:
            inspected=subprocess.run(['docker','exec','addp-manager-raster-runtime','python','-c',
                'import pathlib,json,tempfile; print(json.dumps([p.name for p in pathlib.Path(tempfile.gettempdir()).glob("addp-grib-*")]))'],
                capture_output=True,text=True,check=True,timeout=30)
            require(json.loads(inspected.stdout)==[],'GRIB runtime left temporary source snapshots')
            report['runtime_temporary_files_removed']=True
        report['browser']=browser;report['sources_unchanged']=physical('verify')['sources_unchanged'];report['result']='passed'
        return report
    except BaseException as error:
        report.update(result='failed',stage=stage,error=type(error).__name__);raise
    finally:
        cleanup_errors=[]
        try:
            report['metadata_authorization']['cleanup']=grants.cleanup()
            for case in report['cases']:
                client.request('GET',f"/api/v1/meta/items/{case['item_id']}",(404,))
            report['metadata_authorization']['cleanup']['details_hidden']=len(report['cases'])
        except Exception as error:
            cleanup_errors.append(type(error).__name__+': metadata Grant cleanup failed')
        try:
            if seeded:report['cleanup']=physical('delete')
            require(report['cleanup']=={'objects_removed':True,'files_removed':True},'GRIB physical cleanup incomplete')
        except Exception as error:
            cleanup_errors.append(type(error).__name__+': physical source cleanup failed')
        if cleanup_errors:
            report['result']='failed';report['cleanup_errors']=cleanup_errors
        (artifact/'manager-grib.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
        if cleanup_errors:raise ValueError('; '.join(cleanup_errors))
