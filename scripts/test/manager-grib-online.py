"""GRIB ordinary-user metadata, raw grid, source version and UI acceptance."""
from __future__ import annotations
import hashlib
from datetime import datetime, timezone
import importlib
import json
import math
import re
from pathlib import Path
import subprocess
import struct
import time
from urllib.parse import parse_qs, urlencode, urlsplit

FIXTURE=importlib.import_module('business.scripts.online-grib-fixture')
REGISTRATION=importlib.import_module('scripts.test.online-engine-registration')
PERMISSIONS={'manager.content.read','manager.data_item.read','manager.derived_artifact.create','manager.derived_artifact.read','manager.derived_artifact.delete','meta.catalog.read','meta.scan_task.execute','meta.scan_task.read','monitor.execution.read'}

def require(value,message):
    if not value:raise ValueError(message)


def validate_slice(result,spec,field,x=None,y=None,width=None,height=None):
    default = FIXTURE.selection(field)
    x = default['x'] if x is None else x
    y = default['y'] if y is None else y
    width = default['width'] if width is None else width
    height = default['height'] if height is None else height
    reference_time = spec.get('reference_time', 1767322800)
    require(result.get('schema_version')=='gdal.grib.slice/v1' and result.get('source_version')=='sha256:'+spec['sha256'] and result.get('format')=='grib' and result.get('value_mode')=='decoded_native_units','GRIB slice identity mismatch')
    require(result['selection']==dict(variable='message-'+str(field['number']),indices=[],x=x,y=y,width=width,height=height),'GRIB exact message/window mismatch')
    values,valid=FIXTURE.expected(field,x,y,width,height)
    require(result['values']==values and result['valid']==valid,'GRIB numerical values or bitmap changed')
    variable=result['variable'];attributes=variable['attributes']
    require(attributes['GRIB_UNIT']['values']==[field.get('unit','[K]')] and attributes['GRIB_ELEMENT']['values']==[field.get('element','TMP')],'GRIB units or element changed')
    require(attributes['GRIB_FORECAST_SECONDS']['values']==[str(field.get('start_hour',field['forecast'])*3600)] and attributes['GRIB_REF_TIME']['values']==[str(reference_time)] and attributes['GRIB_VALID_TIME']['values']==[str(reference_time+field['forecast']*3600)] and (spec.get('edition')==1 or attributes['GRIB_SHORT_NAME']['values']==[field.get('short_name','2-HTGL')]),'GRIB reference/valid time or level changed')
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
        message=variable['message'];utc=lambda h:datetime.fromtimestamp(reference_time+h*3600,timezone.utc).isoformat()
        require(message['edition']==2 and message['data_template']==3 and message['product_template']==field['product_template'],'complex GRIB template changed')
        require(message['parameter']==dict(centre=7,subcentre=0,master_table=2,local_table=field.get('local_table',0),discipline=0,category=field['parameter'][0],number=field['parameter'][1]),'complex GRIB parameter identity changed')
        expected_time=dict(reference_utc=utc(0),unit_code=1,forecast_value=field['start_hour'],start_utc=utc(field['start_hour']),end_utc=utc(field['forecast']),operation='instant')
        if field['product_template']==8:expected_time.update(operation='accumulation',statistical_process=1,increment_type=2,interval_unit=1,interval_length=field['forecast']-field['start_hour'],missing_count=0)
        require(message['time']==expected_time,'complex GRIB original interval changed')
        expected_level=dict(type_code=103 if field['height_m'] else 1,raw_scale=0,raw_value=field['height_m'],reference='above_ground' if field['height_m'] else 'surface')
        if field['height_m']:expected_level.update(value=field['height_m'],unit='m')
        require(message['level']==expected_level,'complex GRIB surface changed')
        points=field['width']*field['height']-int(field['missing'])
        require(message['packing']==dict(data_template=3,order=2,extra_octets=2,groups=field.get('groups',(points+3)//4),missing_value_management=0,reference_ieee_hex=struct.pack('>f',field['reference']).hex(),binary_scale=field['binary_scale'],decimal_scale=field['decimal_scale']),'complex GRIB packing facts changed')
        for key in ('parameter','time','level','packing'):require(json.loads(attributes['GRIB2_'+key.upper()]['values'][0])==message[key],'complex GRIB displayed facts differ')
    if spec.get('packing') == 'ccsds':
        message = variable['message']
        cumulative = field.get('product_template') == 8
        initial_zero = cumulative and field['forecast'] == 0
        require(message['edition'] == 2 and message['data_template'] == 42
                and message['product_template'] == field.get('product_template', 0),
                'ECMWF CCSDS templates changed')
        reference = datetime.fromtimestamp(reference_time, timezone.utc).isoformat()
        end = datetime.fromtimestamp(reference_time + field['forecast'] * 3600, timezone.utc).isoformat()
        start = reference if cumulative else end
        expected_facts = dict(
            parameter=dict(centre=98, subcentre=0, master_table=32, local_table=int(cumulative),
                           discipline=0, category=field['parameter'][0], number=field['parameter'][1]),
            time=dict(reference_utc=reference, unit_code=1,
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
                                          increment_value=field.get('increment_seconds',450), missing_count=0)
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
    transform = [field['west']-spacing/2,spacing,0.,field['north']+spacing/2,0.,-spacing]
    if spacing == 0.1:
        # GDAL derives longitude spacing from the two bounding centers.
        actual_spacing = variable['grid']['transform'][1]
        require(abs(actual_spacing-spacing) <= math.ulp(spacing),'GRIB longitude spacing changed')
        transform[1] = actual_spacing
    require(variable['grid']['transform']==transform and variable['grid']['longitude_policy']=='unchanged' and bool(variable['grid']['crs_wkt']),'GRIB grid/longitude changed')
    require(result.get('spatial') is None,'GRIB gained unapproved map preview')


def validate_original_download(response, spec, *, allowed):
    if not allowed:
        headers={key.lower():value for key,value in response.headers.items()}
        require(response.status==403 and headers.get('content-type','').startswith('application/json')
            and 'content-disposition' not in headers and isinstance(response.payload,dict)
            and set(response.payload)=={'error'} and isinstance(response.payload['error'],str) and response.payload['error'],
            'GRIB NFS original must be a JSON denial without an attachment')
        return dict(http_status=403,json_error=True,attachment=False)
    require(response.status==200 and len(response.raw)==spec['size_bytes'] and hashlib.sha256(response.raw).hexdigest()==spec['sha256'],
        'GRIB original differs from the complete source')
    return dict(http_status=200,sha256=spec['sha256'],size_bytes=spec['size_bytes'])


def validate_browser_case(case,observed):
    require(observed.get('locator')==case['locator'] and observed.get('values_verified') is True
        and observed.get('viewport_grid_verified') is True and observed.get('visible_message_count')==len(case['spec']['fields']),
        'GRIB real visible grid incomplete')
    if case['spec']['case_id'] == 'noaa-gfs-intervals':
        require(observed.get('distinct_accumulation_windows_verified') is True,
                'GRIB page did not verify both original accumulation windows')
    if case['spec']['case_id']=='ecmwf-precipitation-0p1':
        require(observed.get('high_resolution_accumulation_verified') is True,
                'GRIB page did not verify both 0.1 degree accumulation fields')
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


def map_browser_artifacts(proof, run_id, locator, directory, case, spec):
    messages = ['message-1', 'message-2']
    version = 'sha256:' + spec['sha256']
    require(proof.get('result') == 'passed' and proof.get('run_id') == run_id
            and proof.get('case') == case and proof.get('locator') == locator and proof.get('source_version') == version,
            'GRIB browser map evidence belongs to a different run or source')
    visits, generations = proof['visits'], proof['generations']
    require([v['message'] for v in visits] == messages * 2 and
            [v['message'] for v in generations] == messages and
            len({v['task_id'] for v in generations}) == len({v['execution_id'] for v in generations}) == 2,
            'two distinct GRIB generations and four visits required')
    urls = [v['preview_url'] for v in visits]
    require(urls[:2] == urls[2:] and urls[0] != urls[1], 'GRIB message cache mismatch')
    for step, visit in enumerate(visits, 1):
        if 'intervals' in spec:
            require(visit.get('accumulation_interval') == spec['intervals'][(step - 1) % len(messages)],
                    'NOAA map did not display the original start, common end and duration')
        url = urlsplit(visit['preview_url'])
        require(not url.netloc and not url.scheme and not url.fragment
                and re.fullmatch(r'/api/v1/manager/raster_cog/[1-9][0-9]*/content', url.path)
                and parse_qs(url.query) == {'source_version': [version]}, 'unexpected GRIB content URL')
        require(any(r.get('path') == url.path and r.get('version') == version and
                    re.fullmatch(r'bytes \d+-\d+/\d+', r.get('range', '')) for r in visit['ranges']),
                'browser did not read this protected COG with Range')
        screenshot = f'grib-{case}-map-{step}.png'
        require(visit['screenshot'] == screenshot and (directory/screenshot).read_bytes().startswith(b'\x89PNG\r\n\x1a\n'),
                'GRIB map screenshot missing')
    return dict(zip(messages, urls[:2]))


def run(repository,environment,owner,*,physical_runner=None,browser_runner=None):
    require(environment.get('ADDP_ONLINE_MANAGER_CASE')=='native-grib','GRIB requires its independent case')
    artifact=Path(environment['ADDP_ONLINE_ARTIFACT_DIR']);physical=physical_runner or (lambda action, **kwargs:FIXTURE.physical(action,repository,environment,**kwargs))
    report={'schema_version':'addp.manager-grib/v4','case':'native-grib','run_id':environment['ADDP_ONLINE_TEST_RUN_ID'],'result':'incomplete','cases':[],'cleanup':{}}
    client=owner.GatewayClient(environment['GATEWAY_URL'],environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30)
    tenant=owner.positive_int(environment['ADDP_ONLINE_TEST_TENANT_ID'],'GRIB tenant')
    report['identity']=owner.validate_user_identity(client,tenant,PERMISSIONS)
    issuer=owner.GatewayClient(environment['GATEWAY_URL'],environment['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'],30)
    grants=REGISTRATION.ExactFileMetadataGrants(issuer,client,tenant,scan_preparation=True)
    report['metadata_authorization']={'operator_principal_id':grants.operator,'recipient_principal_id':grants.recipient,'initializer_permissions_verified':sorted(REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS|{'meta.scan_task.execute','meta.scan_task.read'}),'grants':[]}
    map_seeded = False
    map_items = {}
    map_grants = {}
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
                    selection=FIXTURE.selection(field)
                    result=read(client,locator,selection);validate_slice(result,spec,field)
                    if spec.get('packing')=='complex-spatial':precision_audits.append(dict(message=field['number'],**FIXTURE.precision_bound(field,result['values'],result['valid'],selection['x'],selection['y'])))
                    require(read(client,locator,selection)==result,'repeated GRIB slice changed')
                    (artifact/f"grib-{storage}-{spec['case_id']}-message{field['number']}.json").write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
                    window=dict(selection,x=selection['x']+1,y=selection['y']+1,width=1,height=1)
                    validate_slice(read(client,locator,window),spec,field,window['x'],window['y'],1,1)
                for change in ({'indices':[0]},{'variable':'message-01'},{'width':129}):
                    invalid=dict(selection,**change);denied=read(client,locator,invalid,400);require(denied.get('error_code')=='grid_slice_selection_invalid','invalid window was not a controlled rejection')
                stage='download'
                original=client.request('GET','/api/v1/manager/downloads/file?'+urlencode({'locator':locator}),(403 if storage=='nfs' else 200,))
                original_proof=validate_original_download(original, spec, allowed=storage == 'minio')
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
                selection = FIXTURE.selection(field)
                values, valid = FIXTURE.expected(field, selection['x'], selection['y'], selection['width'], selection['height'])
                windows.append(dict(selection=selection, values=values, valid=valid))
            browser_cases.append(dict(case, expected_windows=windows))
        context = dict(run_id=report['run_id'], identity=report['identity'], cases=browser_cases)
        stage = 'grib_map_source'
        maps = report['maps'] = {}
        context['maps'] = {}
        map_seeded = True
        require(physical('map-seed') == dict(sources=FIXTURE.MAP_SOURCES), 'map seed mismatch')
        rules = importlib.import_module('scripts.test.ftp-consumer-flow-online')
        for storage, engine, prefix, scan_client in (
            ('minio', int(environment['ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID']), bucket+'/', client),
            ('nfs', int(environment['ADDP_ONLINE_MANAGER_NFS_ENGINE_ID']), '', issuer),
        ):
            stage = f'grib_map_scan_{storage}'
            scope = {'catalog_paths': [prefix + 'grib-map']}
            scan = owner.wait_for_meta_scan(scan_client, engine, time.monotonic()+180, scope=scope)
            for kind, spec in FIXTURE.MAP_SOURCES.items():
                case = f'{storage}-{kind}'
                stage = f'grib_map_source_{case}'
                map_report = maps[case] = {'cleanup': {}}
                name = prefix + spec['key']
                require(not (artifact / f'manager-grib-{case}-map-browser.json').exists(),
                        'stale map browser evidence in run directory')
                map_report['source'] = dict(storage=storage, kind=kind, engine_id=engine,
                    full_name=name, scan_scope=scope,
                    scan_principal_id=grants.operator if storage == 'nfs' else grants.recipient)
                map_report['scan'] = scan
                visible = client.request('GET', f'/api/v1/meta/engines/{engine}/items', (200,)).payload
                require(isinstance(visible, list) and not any(row.get('full_name') == name for row in visible),
                        f'unapproved {case} map source visible in the consumer catalog')
                model = dict(format='grib', full_name=name, fingerprint=hashlib.sha256(f'{engine}:{name}'.encode()).hexdigest(),
                             composition=[dict(path=name, role='container', required=True, primary=True)])
                map_grant = map_grants[case] = rules.ModelSourceAuthorization(issuer, client,
                    dict(tenant_id=tenant, engine_id=engine, principal_id=report['identity']['principal_id']),
                    source_type=storage, consumer_permissions=PERMISSIONS,
                    issuer_permissions=REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS | {'meta.scan_task.execute','meta.scan_task.read'})
                map_report['read_grant'] = map_grant.grant(model)
                item = owner.find_fixture_item(client, engine, name, f'GRIB exact {storage} map source')
                require(item['fingerprint'] == model['fingerprint'], 'map source fingerprint differs')
                locator = owner.build_item_locator(engine, item)
                map_items[case] = (item, locator)
                download_allowed = storage == 'minio'
                original = client.request('GET', '/api/v1/manager/downloads/file?' + urlencode({'locator': locator}),
                                          (200,) if download_allowed else (403,))
                map_report['original'] = validate_original_download(
                    original, spec, allowed=download_allowed)
                context['maps'][case] = dict(locator=locator, version='sha256:'+spec['sha256'], profile=spec['profile'])
                if 'intervals' in spec:
                    context['maps'][case]['intervals'] = spec['intervals']
        stage = 'browser'
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
                if case['spec']['case_id'] in ('noaa-gfs-intervals','ecmwf-precipitation-0p1'):
                    suffixes += ('-time.png',)
                for suffix in suffixes:
                    screenshot = artifact / (prefix + suffix)
                    require(screenshot.is_file() and screenshot.read_bytes().startswith(b'\x89PNG\r\n\x1a\n'),
                            'GRIB visible slice screenshot missing')
        content_paths = set()
        for case, map_report in maps.items():
            stage = f'grib_map_pixels_{case}'
            map_item, locator = map_items[case]
            kind = map_report['source']['kind']
            spec = FIXTURE.MAP_SOURCES[kind]
            proof = json.loads((artifact / f'manager-grib-{case}-map-browser.json').read_text())
            urls = map_browser_artifacts(proof, report['run_id'], locator, artifact, case, spec)
            pixels = map_report['pixels'] = {}
            for message, url in urls.items():
                cog = client.request('GET', url, (200,)).raw
                pixels[message] = physical('map-inspect', kind=kind, message=message, cog=cog)
                require(pixels[message]['sha256'] == hashlib.sha256(cog).hexdigest(), 'map readback hash differs')
            first, second = pixels.values()
            require(first['sha256'] != second['sha256'], 'map time fields crossed')
            if kind == 'temperature':
                require(first['equator_kelvin'] != second['equator_kelvin'], 'map time fields crossed')
                require(proof.get('date_line', {}).get('final_view', {}).get('seamColors') is True
                        and (artifact/f'grib-{case}-map-date-line.png').read_bytes().startswith(b'\x89PNG\r\n\x1a\n'),
                        'date line page evidence missing')
            else:
                require(all(visit.get('precipitation_style_verified') is True for visit in proof['visits']),
                        'precipitation browser zero or interval evidence missing')
                if kind == 'noaa-precipitation':
                    require(all(v['unit'] == 'kg/m²' and v['verified_pixels'] == 4096 * 2048 for v in pixels.values()),
                            'NOAA raw units and full COG pixel evidence required')
                    map_report['same_end_distinct_intervals_verified'] = True
            for url in urls.values():
                stale = urlsplit(url).path + '?' + urlencode({'source_version':'sha256:'+'0'*64})
                denied = client.request('GET', stale, (403,), headers={'Range':'bytes=0-63'})
                require(isinstance(denied.payload,dict) and bool(denied.payload.get('error')), 'stale map Range denial missing')
            map_report['stale_version_range_denied'] = True
            map_report['browser'] = proof

            stage = f'grib_map_revocation_{case}'
            range_header = {'Range': 'bytes=0-63'}
            range_checks = map_report['source_revocation'] = {'results': {}}
            for message, url in urls.items():
                allowed = client.request('GET', url, (206,), headers=range_header)
                headers = {name.lower(): value for name, value in allowed.headers.items()}
                require(len(allowed.raw) == 64 and headers.get('content-range') == f"bytes 0-63/{pixels[message]['size_bytes']}",
                        f'{message} original Range did not return the expected COG bytes before revocation')
                range_checks['results'][message] = {'url': url, 'range': range_header['Range'], 'before': allowed.status}
            map_grants[case].revoke(map_report['read_grant']['request_id'])
            range_checks['source_detail_status'] = client.request('GET', f"/api/v1/meta/items/{map_item['id']}", (404,)).status
            for message, url in urls.items():
                result_path = urlsplit(url).path.removesuffix('/content')
                result = client.request('GET', result_path, (200,)).payload
                require(result.get('status') == 'ready' and result.get('item_fingerprint') == map_item['fingerprint'],
                        f'{message} cached result must still be ready after source revocation')
                denied = client.request('GET', url, (403,), headers=range_header)
                headers = {name.lower(): value for name, value in denied.headers.items()}
                require(headers.get('content-type', '').startswith('application/json')
                        and 'content-range' not in headers and 'content-disposition' not in headers
                        and isinstance(denied.payload, dict) and bool(denied.payload.get('error')),
                        f'{message} revoked Range must return a JSON denial without COG bytes')
                range_checks['results'][message].update(result_id=result['id'], result_status=result['status'], after=denied.status)
            paths = {urlsplit(url).path for url in urls.values()}
            require(content_paths.isdisjoint(paths), 'map results shared across sources or display profiles')
            content_paths.update(paths)
        report['grib_map_source_isolation_verified'] = True
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
        if map_seeded:
            for case, map_report in maps.items():
                cleanup = map_report['cleanup']
                cleanup['tasks'] = []
                map_item, locator = map_items.get(case, (None, None))
                map_grant = map_grants.get(case)
                map_proof_path = artifact / f'manager-grib-{case}-map-browser.json'
                if map_item is not None and map_proof_path.exists():
                    try:
                        partial = json.loads(map_proof_path.read_text())
                        require(partial['run_id'] == report['run_id'] and partial['case'] == case and partial['locator'] == locator, 'foreign task evidence')
                        for task in partial['generations']:
                            try:
                                fixture = owner.ArtifactFixture('grib', map_item, locator,
                                    owner.positive_int(task['task_id'], 'map task'), task['execution_id'])
                                removed = owner.cleanup_managed_artifact(client, fixture, 180, 'raster_cog', owner.RASTER_TASK_TYPE)
                                if partial.get('result') == 'passed':
                                    require(len(removed) == 1 and
                                        any(v['message'] == task['message'] and urlsplit(v['preview_url']).path ==
                                            f"/api/v1/manager/raster_cog/{removed[0]['id']}/content" for v in partial['visits']), 'map cleanup result mismatch')
                                cleanup['tasks'].append(dict(task_id=fixture.task_id, **fixture.cleanup))
                            except Exception as error:
                                cleanup_errors.append(f"{case} map task {task.get('task_id')}: {type(error).__name__}")
                    except Exception as error:
                        cleanup_errors.append(case+' map task evidence: '+type(error).__name__)
                try:
                    if map_grant is not None:
                        map_grant.cleanup()
                        if map_item is not None:
                            client.request('GET', f"/api/v1/meta/items/{map_item['id']}", (404,))
                        cleanup['read_grant_revoked'] = True
                except Exception as error:
                    cleanup_errors.append(case+' map Grant cleanup: '+type(error).__name__)
            try:
                report['grib_maps_storage_cleanup'] = physical('map-delete')
                require(report['grib_maps_storage_cleanup'] == dict(
                    nfs_source_removed=True, minio_source_removed=True, cog_objects_removed=True, temporary_inputs_removed=True),
                    'map storage cleanup incomplete')
            except Exception as error:
                cleanup_errors.append('map storage cleanup: '+type(error).__name__)
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
