"""Classic NetCDF ordinary-user metadata, raw grid, source version and UI acceptance."""
from __future__ import annotations
import hashlib
import importlib
import json
from pathlib import Path
import subprocess
import time
from urllib.parse import urlencode

FIXTURE=importlib.import_module('business.scripts.online-netcdf-fixture')
PERMISSIONS={'manager.content.read','manager.data_item.read','meta.catalog.read','meta.scan_task.execute','meta.scan_task.read','monitor.execution.read'}

def require(value,message):
    if not value:raise ValueError(message)


def validate_slice(result,spec,layer,x=0,y=0,width=4,height=3):
    require(result.get('schema_version')=='gdal.netcdf.slice/v1' and result.get('source_version')=='sha256:'+spec['sha256'] and result.get('format')=='netcdf' and result.get('value_mode')=='raw_storage','NetCDF slice identity mismatch')
    require(result['selection']==dict(variable='/temperature',indices=[layer],x=x,y=y,width=width,height=height),'NetCDF slice selection mismatch')
    require(result['values']==FIXTURE.expected_values(spec['version'],layer,x,y,width,height),'NetCDF exact grid values changed')
    require(result['valid']==[[not(layer==0 and row==1 and col==2) for col in range(x,x+width)] for row in range(y,y+height)],'NetCDF valid mask changed')
    require(result['variable']['attributes']['scale_factor']['values']==['0.5'] and result['variable']['attributes']['add_offset']['values']==['273.15'],'NetCDF scale/offset facts changed')
    if spec['projected']:
        spatial=result.get('spatial') or {}
        require(spatial.get('transform')==[500000.+(x-.5)*10.,10.,0.,3000000.+(y-.5)*10.,0.,10.] and spatial.get('footprint',{}).get('geometry',{}).get('type')=='Polygon','NetCDF explicit grid positioning missing')
    else:require(result.get('spatial') is None,'unlocated NetCDF gained a CRS')


def read(client,locator,selection,status=200):
    response=client.request('GET','/api/v1/manager/preview?'+urlencode({'locator':locator,'grid_slice':json.dumps(selection,separators=(',',':'))}),(status,)).payload
    return response.get('data',{}).get('object',{}).get('content',{}).get('json') if status==200 else response


def run(repository,environment,owner,*,physical_runner=None,browser_runner=None):
    require(environment.get('ADDP_ONLINE_MANAGER_CASE')=='native-netcdf','NetCDF requires its independent case')
    artifact=Path(environment['ADDP_ONLINE_ARTIFACT_DIR']);physical=physical_runner or (lambda action:FIXTURE.physical(action,repository,environment))
    report={'schema_version':'addp.manager-netcdf/v1','case':'native-netcdf','run_id':environment['ADDP_ONLINE_TEST_RUN_ID'],'result':'incomplete','cases':[],'cleanup':{}}
    client=owner.GatewayClient(environment['GATEWAY_URL'],environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30)
    tenant=owner.positive_int(environment['ADDP_ONLINE_TEST_TENANT_ID'],'NetCDF tenant')
    report['identity']=owner.validate_user_identity(client,tenant,PERMISSIONS)
    seeded=False;stage='seed'
    try:
        seeded=True;seed=physical('seed');require(seed=={'sources':FIXTURE.sources()},'NetCDF source bytes differ from the independent oracle')
        bucket=environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET']
        for storage,engine,scope,prefix in [('minio',owner.positive_int(environment['ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID'],'MinIO'),'netcdf',bucket+'/'),('nfs',owner.positive_int(environment['ADDP_ONLINE_MANAGER_NFS_ENGINE_ID'],'NFS'),'netcdf','')]:
            stage='scan';owner.wait_for_meta_scan(client,engine,time.monotonic()+180,scope={'catalog_paths':[prefix+scope]})
            for spec in seed['sources']:
                item=owner.find_fixture_item(client,engine,prefix+'netcdf/'+spec['name'],'NetCDF '+storage)
                detail=client.request('GET',f"/api/v1/meta/items/{item['id']}",(200,)).payload;attrs=detail.get('attributes',{})
                require({k:attrs.get('item',{}).get(k) for k in ('data_type','format','layout')}=={'data_type':'container','format':'netcdf','layout':'single'},'NetCDF canonical identity changed')
                info=attrs.get('format_info',{}).get('netcdf',{})
                require(info.get('source_version')=='sha256:'+spec['sha256'] and info.get('scan_complete') is True and info.get('encoding')==f"CDF-{spec['version']}",'NetCDF content version missing')
                require(not attrs.get('capabilities',{}).get('spatial'),'parent container gained unified spatial extent')
                children=attrs.get('type_info',{}).get('container',{}).get('children',[])
                temperature,=[v['native'] for v in children if v.get('name')=='/temperature']
                require([(d['name'],d['size']) for d in temperature['dimensions']]==[('/time','2'),('/y','3'),('/x','4')],'NetCDF dimension order or size changed')
                coordinate,=[v['native'] for v in children if v.get('name')=='/time']
                require(coordinate['attributes']['calendar']['values']==['360_day'] and coordinate['slice_supported'] is False,'time coordinate silently converted to Gregorian or grid')
                locator=owner.build_item_locator(engine,item);stage='slice'
                selection=dict(variable='/temperature',indices=[0],x=0,y=0,width=4,height=3)
                for layer in (0,1):
                    selection['indices']=[layer];result=read(client,locator,selection);validate_slice(result,spec,layer)
                    require(read(client,locator,selection)==result,'repeated stateless slice changed')
                    (artifact/f"netcdf-{storage}-{spec['version']}-layer{layer}.json").write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
                selection.update(indices=[1],x=1,y=1,width=2,height=2);validate_slice(read(client,locator,selection),spec,1,1,1,2,2)
                for change in ({'indices':[2]},{'variable':'NETCDF:/etc/passwd:secret'},{'width':129}):
                    invalid=dict(selection,**change);denied=read(client,locator,invalid,400);require(denied.get('error_code')=='grid_slice_selection_invalid','invalid window was not a controlled rejection')
                stage='download';raw=client.request('GET','/api/v1/manager/downloads/file?'+urlencode({'locator':locator}),(200,)).raw
                require(len(raw)==spec['size_bytes'] and hashlib.sha256(raw).hexdigest()==spec['sha256'],'NetCDF original download differs')
                (artifact/f"netcdf-{storage}-{spec['version']}.nc").write_bytes(raw)
                require(client.request('GET',f"/api/v1/meta/items/{item['id']}",(200,)).payload==detail,'NetCDF preview changed metadata')
                report['cases'].append(dict(source_case=storage,locator=locator,item_id=item['id'],spec=spec,metadata_verified=True,values_verified=True,mask_verified=True,repeat_stateless_verified=True,window_verified=True,invalid_selection_denied=True,original_download_verified=True))
        stage='source_drift';physical('drift')
        for case in [c for c in report['cases'] if c['spec']['version']==5]:
            changed=read(client,case['locator'],dict(variable='/temperature',indices=[0],x=0,y=0,width=4,height=3),409)
            require(changed.get('error_code')=='preview_source_refresh_required','changed NetCDF did not stop preview')
        physical('restore');report['source_drift_denied']=True
        require(physical('verify').get('sources_unchanged') is True,'sources not restored exactly')
        stage='browser';context={'run_id':report['run_id'],'identity':report['identity'],'cases':report['cases']}
        if browser_runner:browser=browser_runner(context)
        else:
            subprocess.run(['npm','exec','--','playwright','test','e2e/online/manager-netcdf.spec.js','--config=playwright.online.config.js'],cwd=Path(repository)/'console/frontend',env=dict(environment,ADDP_ONLINE_MANAGER_NETCDF_JSON=json.dumps(context)),check=True)
            browser=json.loads((artifact/'manager-netcdf-browser.json').read_text())
        require(browser.get('result')=='passed' and browser.get('run_id')==report['run_id'] and len(browser.get('cases',[]))==6,'NetCDF browser report incomplete')
        for case,observed in zip(report['cases'],browser['cases']):
            require(observed.get('locator')==case['locator'] and observed.get('values_verified') is True and observed.get('download_sha256')==case['spec']['sha256'] and observed.get('download_size')==case['spec']['size_bytes'],'NetCDF real UI or original button download incomplete')
        if not physical_runner:
            inspected=subprocess.run(['docker','exec','addp-manager-raster-runtime','python','-c',
                'import pathlib,json,tempfile; print(json.dumps([p.name for p in pathlib.Path(tempfile.gettempdir()).glob("addp-netcdf-*")]))'],
                capture_output=True,text=True,check=True,timeout=30)
            require(json.loads(inspected.stdout)==[],'NetCDF runtime left temporary source snapshots')
            report['runtime_temporary_files_removed']=True
        report['browser']=browser;report['sources_unchanged']=physical('verify')['sources_unchanged'];report['result']='passed'
        return report
    except BaseException as error:
        report.update(result='failed',stage=stage,error=type(error).__name__);raise
    finally:
        try:
            if seeded:report['cleanup']=physical('delete')
            require(report['cleanup']=={'objects_removed':True,'files_removed':True},'NetCDF physical cleanup incomplete')
        except BaseException:
            report['result']='failed';raise
        finally:(artifact/'manager-netcdf.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
