"""ENVI exact pair, ordinary User source rights, COG/cache and owned cleanup."""
from __future__ import annotations
import importlib
import json
from pathlib import Path
import time
from urllib.parse import urlencode

FIXTURE=importlib.import_module('business.scripts.online-envi-fixture')
GRANTS=importlib.import_module('scripts.test.ftp-consumer-flow-online')
MEDIA=importlib.import_module('scripts.test.manager-img-online')

def require(value,message):
    if not value:raise ValueError(message)

def run(repository,environment,owner,*,physical_runner=None,browser_runner=None):
    require(environment.get('ADDP_ONLINE_MANAGER_CASE')=='native-envi','independent ENVI case required')
    physical=physical_runner or (lambda action,request=None:FIXTURE.physical(action,repository,environment,request))
    artifact=Path(environment['ADDP_ONLINE_ARTIFACT_DIR']);tenant=owner.positive_int(environment['ADDP_ONLINE_TEST_TENANT_ID'],'ENVI Tenant')
    client=owner.GatewayClient(environment['GATEWAY_URL'],environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30)
    identity=owner.validate_user_identity(client,tenant)
    issuer=owner.GatewayClient(environment['GATEWAY_URL'],environment['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'],30)
    report=dict(schema_version='addp.manager-envi/v1',case='native-envi',run_id=environment['ADDP_ONLINE_TEST_RUN_ID'],result='incomplete',identity=identity,cases=[],cleanup={})
    grants=[];fixtures=[];seeded=False;stage='seed'
    timeout=float(environment.get('ADDP_ONLINE_MANAGER_LINEAGE_CONVERGENCE_TIMEOUT_SECONDS','180'))
    try:
        seeded=True;seed=physical('seed');require(seed=={'sources':FIXTURE.sources()},'independent pair bytes changed')
        for storage,key,prefix in [('minio','ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID',environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET']+'/'),('nfs','ADDP_ONLINE_MANAGER_NFS_ENGINE_ID','')]:
            engine=owner.positive_int(environment[key],storage+' Engine')
            stage='scan';owner.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'catalog_paths':[prefix+'envi']})
            grant=GRANTS.ModelSourceAuthorization(issuer,client,{'tenant_id':tenant,'engine_id':engine,'principal_id':str(identity['principal_id'])},source_type=storage,consumer_permissions=owner.REQUIRED_PERMISSIONS)
            grants.append(grant)
            for spec in seed['sources']:
                stage='metadata';name=prefix+'envi/'+spec['name'];header=prefix+'envi/'+spec['header']
                item=owner.find_fixture_item(client,engine,name,'ENVI '+storage)
                detail=client.request('GET',f"/api/v1/meta/items/{item['id']}",(200,)).payload;attrs=detail['attributes']
                require({k:attrs['item'].get(k) for k in ('data_type','format','layout')}=={'data_type':'media','format':'envi','layout':'multi'},'ENVI identity')
                info=attrs['format_info']['envi'];require(info.get('profile')=='envi-standard-pair/v1' and info.get('scan_complete') is True and info.get('composition_complete') is True,'complete native proof missing')
                require(info.get('header_sha256')==spec['members'][spec['header']]['sha256'] and info.get('raster_sha256')==spec['sha256'] and info.get('interleave')==spec['interleave'],'ENVI source versions')
                require(item['size_bytes']==spec['size_bytes'] and info.get('band_count')==3 and attrs['type_info']['media']['width']==attrs['type_info']['media']['height']==256,'ENVI complete dimensions/size')
                expected=[dict(path=name,role='raster',primary=True,required=True),dict(path=header,role='header',primary=False,required=True)]
                expected_meta=[dict(expected[0],extension='.img'),{k:v for k,v in dict(expected[1],extension='.hdr').items() if k!='primary'}]
                require(sorted(attrs['item']['refs'],key=lambda r:r['path'])==sorted(expected_meta,key=lambda r:r['path']),'ENVI exact refs')
                if spec['source_srid'] is None:require(not attrs.get('capabilities',{}).get('spatial'),'unlocated ENVI gained spatial facts')
                else:require(attrs.get('capabilities',{}).get('spatial',{}).get('srid')==spec['source_srid'],'ENVI explicit source CRS')
                model=dict(format='envi',full_name=name,fingerprint=item['fingerprint'],composition=expected)
                fixture=owner.ArtifactFixture('envi-'+storage+'-'+spec['case'],item,owner.build_item_locator(engine,item));fixtures.append(fixture)
                # No model-only or directory grant may satisfy this pair.
                path='/api/v1/manager/downloads/file?'+urlencode({'locator':fixture.locator})
                client.request('GET',path,(403,));receipt=grant.grant(model)
                stage='download';raw=client.request('GET',path,(200,)).raw;owner.validate_model_bundle(raw,spec['members'])
                (artifact/f'envi-{storage}-{spec["case"]}-original.zip').write_bytes(raw)
                (artifact/f'envi-{storage}-{spec["case"]}-meta.json').write_text(json.dumps(detail,ensure_ascii=False,indent=2)+'\n')
                case=dict(spec,case=storage+'-'+spec['case'],item_id=item['id'],locator=fixture.locator,download_members=spec['members'],metadata_verified=True,original_download_verified=True)
                report['cases'].append(case)
                if spec['case']=='bsq':
                    stage='composition-change'
                    mutation=dict(source_type=storage,case='bsq',changed=True)
                    renamed=dict(model,composition=[expected[0],dict(expected[1],path=prefix+'envi/'+spec['name']+'.hdr')])
                    try:
                        physical('header-layout',mutation)
                        owner.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'catalog_paths':[prefix+'envi']})
                        newfacts=grant.facts(renamed)
                        require(newfacts['boundary_hash']!=receipt['boundary_hash'],'changed pair retained the old approval boundary')
                        client.request('GET',path,(403,))
                        grant.revoke(receipt['request_id'])
                        replacement=grant.grant(renamed)
                        renamed_members={spec['name']:spec['members'][spec['name']],spec['name']+'.hdr':spec['members'][spec['header']]}
                        owner.validate_model_bundle(client.request('GET',path,(200,)).raw,renamed_members)
                        grant.revoke(replacement['request_id'])
                        case['composition_change_reapproval_verified']=True
                    finally:
                        physical('header-layout',dict(mutation,changed=False))
                        owner.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'catalog_paths':[prefix+'envi']})
                    receipt=grant.grant(model)
                if spec['source_srid'] is None:
                    cap=client.request('GET','/api/v1/manager/quick-view/capability?'+urlencode({'locator':fixture.locator}),(200,)).payload
                    require(not cap.get('can_use_quick_view') and 'generate_raster_cog' not in cap.get('available_actions',[]),'unlocated ENVI permits map/generation')
                    continue
                stage='generation'
                def physical_cog(action,request):
                    results=client.request('GET','/api/v1/manager/raster_cog?'+urlencode({'task_id':fixture.task_id,'page':1,'page_size':100}),(200,)).payload['data']
                    require(len(results)==1,'exact COG result required')
                    proof=physical('cog' if action=='verify' else 'deleted-cog',dict(result=results[0],tenant_id=tenant,case=spec['case'],sha256=request.get('sha256')))
                    if action=='verify':return dict(proof,source_unchanged=True)
                    return proof
                owner.generate_raster_cog(client,fixture,tenant,timeout,physical_cog,source_facts=spec)
                result=client.request('GET',f'/api/v1/manager/raster_cog/{fixture.result_id}',(200,)).payload
                case.update(task_id=fixture.task_id,execution_id=fixture.execution_id,result_id=fixture.result_id,preview_url=fixture.artifact['preview_url'],artifact=fixture.artifact,result=result)
                # The same stable source Deny must also stop reads of derived COG.
                denied=grant.deny(model);client.request('GET',case['preview_url'],(403,));grant.release(denied)
                client.request('GET',case['preview_url'],(206,),headers={'Range':'bytes=0-63'})
                case['current_source_deny_verified']=True
                if spec['case']=='bsq':
                    stage='cache-source-version'
                    mutation=dict(source_type=storage,case='bsq',changed=True)
                    try:
                        physical('header-version',mutation)
                        owner.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'catalog_paths':[prefix+'envi']})
                        client.request('GET',case['preview_url'],(403,))
                        case['changed_header_cache_rejected']=True
                    finally:
                        physical('header-version',dict(mutation,changed=False))
                        owner.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'catalog_paths':[prefix+'envi']})
                    client.request('GET',case['preview_url'],(206,),headers={'Range':'bytes=0-63'})
        stage='browser';evidence=dict(source_format='envi',run_id=report['run_id'],identity=identity,cases=report['cases'])
        browser=browser_runner(evidence) if browser_runner else MEDIA.run_browser(repository,environment,evidence,owner)
        require(browser.get('result')=='passed' and len(browser.get('cases',[]))==6,'complete ENVI browser proof')
        report['browser']=browser
        require(physical('verify')=={'sources_unchanged':True},'sources changed')
        report['sources_unchanged']=True;report['result']='passed'
        return report
    except BaseException as error:
        report.update(result='failed',stage=stage,error=type(error).__name__);raise
    finally:
        errors=[]
        for fixture in reversed(fixtures):
            if not fixture.task_id:continue
            try:
                row=next((c.get('result') for c in report['cases'] if c.get('task_id')==fixture.task_id),None)
                owner.cleanup_managed_artifact(client,fixture,timeout,'raster_cog',owner.RASTER_TASK_TYPE)
                require(row is not None,'missing owned output reference')
                proof=physical('deleted-cog',dict(result=row,tenant_id=tenant))
                require(proof==dict(object_deleted=True,temporary_inputs_removed=True,source_unchanged=True),'incomplete exact physical cleanup')
                report['cleanup'][fixture.format]=dict(fixture.cleanup,**proof)
            except Exception as error:errors.append(type(error).__name__)
        for grant in reversed(grants):
            try:grant.cleanup()
            except Exception as error:errors.append(type(error).__name__)
        if seeded:
            try:report['source_cleanup']=physical('delete');require(report['source_cleanup']==dict(objects_removed=True,files_removed=True),'source cleanup')
            except Exception as error:errors.append(type(error).__name__)
        if errors:report.update(result='failed',cleanup_errors=errors)
        (artifact/'manager-envi.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
        if errors:raise ValueError('ENVI cleanup failed: '+','.join(errors))
