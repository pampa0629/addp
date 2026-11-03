"""GRIB ordinary-user metadata, raw grid, source version and UI acceptance."""
from __future__ import annotations
import hashlib
import importlib
import json
from pathlib import Path
import subprocess
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
    require(attributes['GRIB_UNIT']['values']==['[K]'] and attributes['GRIB_ELEMENT']['values']==['TMP'],'GRIB units or element changed')
    require(attributes['GRIB_FORECAST_SECONDS']['values']==[str(field['forecast']*3600)] and attributes['GRIB_REF_TIME']['values']==['1767322800'] and attributes['GRIB_VALID_TIME']['values']==[str(1767322800+field['forecast']*3600)] and attributes['GRIB_SHORT_NAME']['values']==['2-HTGL'],'GRIB reference/valid time or level changed')
    require(variable['dimensions']==[dict(name='y',size=str(field['height']),axis_type='HORIZONTAL_Y',direction='NORTH_TO_SOUTH'),dict(name='x',size=str(field['width']),axis_type='HORIZONTAL_X',direction='WEST_TO_EAST')],'GRIB independent dimensions changed')
    require(variable['grid']['transform']==[field['west']-.5,1.,0.,field['north']+.5,0.,-1.] and variable['grid']['longitude_policy']=='unchanged' and bool(variable['grid']['crs_wkt']),'GRIB grid/longitude changed')
    require(result.get('spatial') is None,'GRIB gained unapproved map preview')


def validate_browser_case(case,observed):
    require(observed.get('locator')==case['locator'] and observed.get('values_verified') is True
        and observed.get('viewport_grid_verified') is True and observed.get('visible_message_count')==len(case['spec']['fields'])
        and observed.get('download_sha256')==case['spec']['sha256'] and observed.get('download_size')==case['spec']['size_bytes'],
        'GRIB real visible grid or original button download incomplete')


def validate_structural_metadata(detail):
    attrs=detail.get('attributes',{})
    require(set(attrs)=={'schema_version','item','type_info'} and attrs.get('schema_version')==1
        and attrs.get('item')=={'data_type':'container','format':'grib','layout':'single'},
        'GRIB Meta must expose only the canonical structural projection')
    container=attrs.get('type_info',{}).get('container',{})
    require(set(attrs['type_info'])=={'container'} and set(container)<= {'children','default_child'}
        and container.get('children')==[dict(name='message-'+str(i),child_kind='array',data_type='media') for i in range(1,4)],
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
    grants=REGISTRATION.ExactFileMetadataGrants(issuer,client,tenant)
    report['metadata_authorization']={'operator_principal_id':grants.operator,'recipient_principal_id':grants.recipient,'grants':[]}
    seeded=False;stage='seed'
    try:
        seeded=True;seed=physical('seed');require(seed=={'sources':FIXTURE.sources()},'GRIB source bytes differ from the independent oracle')
        bucket=environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET']
        for storage,engine,scope,prefix in [('minio',owner.positive_int(environment['ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID'],'MinIO'),'grib',bucket+'/'),('nfs',owner.positive_int(environment['ADDP_ONLINE_MANAGER_NFS_ENGINE_ID'],'NFS'),'grib','')]:
            stage='scan';owner.wait_for_meta_scan(client,engine,time.monotonic()+180,scope={'catalog_paths':[prefix+scope]})
            for spec in seed['sources']:
                stage='metadata_grant'
                visible=client.request('GET',f'/api/v1/meta/engines/{engine}/items',(200,)).payload
                require(isinstance(visible,list) and not any(v.get('full_name')==prefix+'grib/'+spec['name'] for v in visible),'GRIB Meta disclosed an unapproved source')
                receipt=grants.grant(engine,prefix+'grib/'+spec['name'],storage,data_type='container',format_name='grib',layout='single')
                report['metadata_authorization']['grants'].append(receipt)
                item=owner.find_fixture_item(client,engine,prefix+'grib/'+spec['name'],'GRIB '+storage)
                detail=client.request('GET',f"/api/v1/meta/items/{item['id']}",(200,)).payload
                validate_structural_metadata(detail)
                locator=owner.build_item_locator(engine,item)
                native=client.request('GET','/api/v1/manager/preview?'+urlencode({'locator':locator}),(200,)).payload
                attrs=native.get('data',{}).get('object',{}).get('attributes',{})
                (artifact/f"grib-{storage}-{spec['case_id']}-metadata.json").write_text(json.dumps({'meta':detail,'manager':native},ensure_ascii=False,indent=2)+'\n')
                require({k:attrs.get('item',{}).get(k) for k in ('data_type','format','layout')}=={'data_type':'container','format':'grib','layout':'single'},'GRIB canonical identity changed')
                info=attrs.get('format_info',{}).get('grib',{})
                require(info.get('source_version')=='sha256:'+spec['sha256'] and info.get('scan_complete') is True and info.get('encoding')=='GRIB2' and info.get('profile')=='grib2_regular_fields/v1','GRIB content version missing')
                require(not attrs.get('capabilities',{}).get('spatial'),'GRIB parent gained unified spatial extent')
                children=attrs.get('type_info',{}).get('container',{}).get('children',[])
                require([v['name'] for v in children]==['message-1','message-2','message-3'],'GRIB message identity or enumeration changed')
                stage='slice'
                for field in spec['fields']:
                    selection=dict(variable='message-'+str(field['number']),indices=[],x=0,y=0,width=field['width'],height=field['height'])
                    result=read(client,locator,selection);validate_slice(result,spec,field)
                    require(read(client,locator,selection)==result,'repeated GRIB slice changed')
                    (artifact/f"grib-{storage}-{spec['case_id']}-message{field['number']}.json").write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
                    window=dict(selection,x=1,y=1,width=1,height=1)
                    validate_slice(read(client,locator,window),spec,field,1,1,1,1)
                for change in ({'indices':[0]},{'variable':'message-01'},{'width':129}):
                    invalid=dict(selection,**change);denied=read(client,locator,invalid,400);require(denied.get('error_code')=='grid_slice_selection_invalid','invalid window was not a controlled rejection')
                stage='download';raw=client.request('GET','/api/v1/manager/downloads/file?'+urlencode({'locator':locator}),(200,)).raw
                require(len(raw)==spec['size_bytes'] and hashlib.sha256(raw).hexdigest()==spec['sha256'],'GRIB original download differs')
                (artifact/f"grib-{storage}-{spec['case_id']}.grb2").write_bytes(raw)
                require(client.request('GET',f"/api/v1/meta/items/{item['id']}",(200,)).payload==detail,'GRIB preview changed metadata')
                report['cases'].append(dict(source_case=storage,locator=locator,item_id=item['id'],spec=spec,metadata_verified=True,values_verified=True,mask_verified=True,repeat_stateless_verified=True,window_verified=True,invalid_selection_denied=True,original_download_verified=True))
        stage='source_drift';physical('drift')
        for case in report['cases']:
            changed=read(client,case['locator'],dict(variable='message-1',indices=[],x=0,y=0,width=4,height=3),409)
            require(changed.get('error_code')=='preview_source_refresh_required','changed GRIB did not stop preview')
        physical('restore');report['source_drift_denied']=True
        require(physical('verify').get('sources_unchanged') is True,'sources not restored exactly')
        stage='browser';context={'run_id':report['run_id'],'identity':report['identity'],'cases':report['cases']}
        if browser_runner:browser=browser_runner(context)
        else:
            browser_environment={k:v for k,v in environment.items() if k not in {'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN','ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'}}
            subprocess.run(['npm','exec','--','playwright','test','e2e/online/manager-grib.spec.js','--config=playwright.online.config.js'],cwd=Path(repository)/'console/frontend',env=dict(browser_environment,ADDP_ONLINE_MANAGER_GRIB_JSON=json.dumps(context)),check=True)
            browser=json.loads((artifact/'manager-grib-browser.json').read_text())
        require(browser.get('schema_version')=='addp.manager-grib-browser/v2' and browser.get('result')=='passed' and browser.get('run_id')==report['run_id'] and len(browser.get('cases',[]))==2*len(FIXTURE.sources()),'GRIB browser report incomplete')
        for case,observed in zip(report['cases'],browser['cases']):
            validate_browser_case(case,observed)
            for field in case['spec']['fields']:
                screenshot=artifact/f"grib-{case['source_case']}-{case['spec']['case_id']}-message{field['number']}-slice.png"
                require(screenshot.is_file() and screenshot.read_bytes().startswith(b'\x89PNG\r\n\x1a\n'),'GRIB visible slice screenshot missing')
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
