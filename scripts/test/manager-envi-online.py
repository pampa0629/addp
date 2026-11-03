"""ENVI exact pair, ordinary User source rights, COG/cache and owned cleanup."""
from __future__ import annotations
import hashlib
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

def validate_structural_metadata(detail,spec):
    attrs=detail.get('attributes',{})
    require(set(attrs)==({'schema_version','item','capabilities'} if spec['source_srid'] else {'schema_version','item'})
        and attrs.get('schema_version')==1 and attrs.get('item')==dict(data_type='media',format='envi',layout='multi'),
        'ENVI Meta must expose only structural identity and declared CRS')
    if spec['source_srid']:
        require(set(attrs['capabilities'])=={'spatial'},'unexpected Meta capability')
        spatial=attrs['capabilities']['spatial']
        require(spatial.get('srid')==spec['source_srid'] and set(spatial)<=
            {'srid','crs_ref','crs_definitions','geometry_columns','primary_geometry_column'},'Meta disclosed a content-derived spatial fact')
    return attrs


def validate_content_metadata(detail,preview,spec,expected):
    validate_structural_metadata(detail,spec)
    attrs=preview.get('data',{}).get('object',{}).get('attributes',{})
    require({k:attrs.get('item',{}).get(k) for k in ('data_type','format','layout')}==detail['attributes']['item'],'protected/public ENVI identity mismatch')
    info=attrs.get('format_info',{}).get('envi',{})
    require(info.get('profile')=='envi-standard-pair/v1' and info.get('scan_complete') is True and info.get('composition_complete') is True,'complete native proof missing')
    require(info.get('header_sha256')==spec['members'][spec['header']]['sha256'] and info.get('raster_sha256')==spec['sha256'] and info.get('interleave')==spec['interleave'],'ENVI source versions')
    require(attrs.get('storage',{}).get('total_size')==spec['size_bytes'] and info.get('band_count')==3
        and attrs.get('type_info',{}).get('media',{}).get('width')==attrs.get('type_info',{}).get('media',{}).get('height')==256,'ENVI complete dimensions/size')
    expected_meta=[dict(expected[0],extension='.img'),{k:v for k,v in dict(expected[1],extension='.hdr').items() if k!='primary'}]
    require(sorted(attrs['item'].get('refs',[]),key=lambda r:r['path'])==sorted(expected_meta,key=lambda r:r['path']),'ENVI exact refs')
    if spec['source_srid'] is None:require(not attrs.get('capabilities',{}).get('spatial'),'unlocated ENVI gained spatial facts')
    else:require(attrs.get('capabilities',{}).get('spatial',{}).get('srid')==spec['source_srid'],'ENVI explicit source CRS')
    return attrs


def validate_browser(owner,report,evidence,artifact):
    require(report.get('schema_version')=='addp.manager-raster-media-browser/v4','ENVI browser schema changed')
    MEDIA.validate_browser(owner,report,evidence)
    for case,observed in zip(evidence['cases'],report['cases']):
        require(all(observed.get(k) is True for k in ('metadata_projection_verified','content_metadata_verified','current_user_download_verified'))
            and observed.get('download_bundle',{}).get('members')==case['download_members'],'ENVI browser must prove public structure, guarded content and exact User ZIP')
        screenshot=Path(artifact)/('envi-'+case['case']+'.png')
        require(screenshot.is_file() and screenshot.read_bytes().startswith(b'\x89PNG\r\n\x1a\n'),'ENVI page screenshot missing')


def verify_source_revocation(client,authorization,receipt,model,preview_url,*,item_id):
    before=client.request('GET',preview_url,(206,),headers={'Range':'bytes=0-63'}).raw
    require(len(before)==64,'exact cached Range required before withdrawal')
    authorization.revoke(receipt['request_id'])
    client.request('GET',preview_url,(403,))
    client.request('GET',f'/api/v1/meta/items/{item_id}',(404,))
    renewed=authorization.grant(model)
    require(renewed['request_id']!=receipt['request_id'] and renewed['boundary_hash']==receipt['boundary_hash'],'reapproval must be a new command for the same exact composition')
    restored=client.request('GET',preview_url,(206,),headers={'Range':'bytes=0-63'}).raw
    require(restored==before,'reapproval must preserve the existing cached COG bytes')
    client.request('GET',f'/api/v1/meta/items/{item_id}',(200,))
    return renewed,dict(revoked_request_id=receipt['request_id'],renewed_request_id=renewed['request_id'],boundary_hash=renewed['boundary_hash'],range_bytes=64,range_sha256=hashlib.sha256(before).hexdigest())

def cleanup_fixture(client,fixture,timeout,tenant,owner,physical):
    # The owner inspects the terminal execution's actual results before deleting.
    # A failure before result creation has no output reference to fabricate.
    rows=owner.cleanup_managed_artifact(client,fixture,timeout,'raster_cog',owner.RASTER_TASK_TYPE)
    proof={}
    for row in rows:
        proof=physical('deleted-cog',dict(result=row,tenant_id=tenant))
        require(proof==dict(object_deleted=True,temporary_inputs_removed=True,source_unchanged=True),'incomplete exact physical cleanup')
    return dict(fixture.cleanup,results_observed=len(rows),**proof)

def run(repository,environment,owner,*,physical_runner=None,browser_runner=None):
    require(environment.get('ADDP_ONLINE_MANAGER_CASE')=='native-envi','independent ENVI case required')
    physical=physical_runner or (lambda action,request=None:FIXTURE.physical(action,repository,environment,request))
    artifact=Path(environment['ADDP_ONLINE_ARTIFACT_DIR']);tenant=owner.positive_int(environment['ADDP_ONLINE_TEST_TENANT_ID'],'ENVI Tenant')
    client=owner.GatewayClient(environment['GATEWAY_URL'],environment['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30)
    identity=owner.validate_user_identity(client,tenant)
    issuer=owner.GatewayClient(environment['GATEWAY_URL'],environment['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'],30)
    report=dict(source_authorization={'grants':[]},schema_version='addp.manager-envi/v3',case='native-envi',run_id=environment['ADDP_ONLINE_TEST_RUN_ID'],result='incomplete',identity=identity,cases=[],cleanup={})
    grants=[];fixtures=[];seeded=False;stage='seed';failure=None
    timeout=float(environment.get('ADDP_ONLINE_MANAGER_LINEAGE_CONVERGENCE_TIMEOUT_SECONDS','180'))
    try:
        seeded=True;seed=physical('seed');require(seed=={'sources':FIXTURE.sources()},'independent pair bytes changed')
        for storage,key,prefix in [('minio','ADDP_ONLINE_MANAGER_MINIO_ENGINE_ID',environment['ADDP_ONLINE_MANAGER_MINIO_BUCKET']+'/'),('nfs','ADDP_ONLINE_MANAGER_NFS_ENGINE_ID','')]:
            engine=owner.positive_int(environment[key],storage+' Engine')
            stage='scan';owner.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'catalog_paths':[prefix+'envi']})
            grant=GRANTS.ModelSourceAuthorization(issuer,client,{'tenant_id':tenant,'engine_id':engine,'principal_id':str(identity['principal_id'])},source_type=storage,consumer_permissions=owner.REQUIRED_PERMISSIONS,issuer_permissions=GRANTS.REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS)
            grants.append(grant)
            report['source_authorization'].update(operator_principal_id=grant.operator,recipient_principal_id=grant.recipient)
            for spec in seed['sources']:
                name=prefix+'envi/'+spec['name'];header=prefix+'envi/'+spec['header']
                stage='source_approval'
                visible=client.request('GET',f'/api/v1/meta/engines/{engine}/items',(200,)).payload
                require(isinstance(visible,list) and not any(v.get('full_name')==name for v in visible),'unapproved ENVI visible in consumer catalog')
                expected=[dict(path=name,role='raster',primary=True,required=True),dict(path=header,role='header',primary=False,required=True)]
                model=dict(format='envi',full_name=name,fingerprint=hashlib.sha256(f'{engine}:{name}'.encode()).hexdigest(),composition=expected)
                receipt=grant.grant(model)
                report['source_authorization']['grants'].append(dict(model=model,receipt=receipt))
                stage='metadata'
                item=owner.find_fixture_item(client,engine,name,'ENVI '+storage)
                require(item['fingerprint']==model['fingerprint'],'consumer source identity differs from approved System facts')
                detail=client.request('GET',f"/api/v1/meta/items/{item['id']}",(200,)).payload
                fixture=owner.ArtifactFixture('envi-'+storage+'-'+spec['case'],item,owner.build_item_locator(engine,item));fixtures.append(fixture)
                stage='download_withdrawal'
                path='/api/v1/manager/downloads/file?'+urlencode({'locator':fixture.locator})
                initial_receipt=receipt
                grant.revoke(initial_receipt['request_id'])
                client.request('GET',path,(403,))
                client.request('GET',f"/api/v1/meta/items/{item['id']}",(404,))
                receipt=grant.grant(model)
                require(receipt['request_id']!=initial_receipt['request_id'] and receipt['boundary_hash']==initial_receipt['boundary_hash'],'original read must be reapproved on the identical complete pair')
                report['source_authorization']['grants'].append(dict(model=model,receipt=receipt))
                client.request('GET',f"/api/v1/meta/items/{item['id']}",(200,))
                native=client.request('GET','/api/v1/manager/preview?'+urlencode({'locator':fixture.locator}),(200,)).payload
                validate_content_metadata(detail,native,spec,expected)
                # Download and content facts are protected by the same exact pair read Grant.
                path='/api/v1/manager/downloads/file?'+urlencode({'locator':fixture.locator})
                stage='download';raw=client.request('GET',path,(200,)).raw;owner.validate_model_bundle(raw,spec['members'])
                (artifact/f'envi-{storage}-{spec["case"]}-original.zip').write_bytes(raw)
                (artifact/f'envi-{storage}-{spec["case"]}-meta.json').write_text(json.dumps({'meta':detail,'manager':native},ensure_ascii=False,indent=2)+'\n')
                case=dict(spec,case=storage+'-'+spec['case'],item_id=item['id'],locator=fixture.locator,download_members=spec['members'],metadata_verified=True,metadata_projection_verified=True,content_metadata_verified=True,original_download_verified=True,original_withdrawal_verified=True,original_withdrawal=dict(revoked_request_id=initial_receipt['request_id'],renewed_request_id=receipt['request_id'],boundary_hash=receipt['boundary_hash']))
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
                        report['source_authorization']['grants'].append(dict(model=renamed,receipt=replacement))
                        renamed_members={spec['name']:spec['members'][spec['name']],spec['name']+'.hdr':spec['members'][spec['header']]}
                        owner.validate_model_bundle(client.request('GET',path,(200,)).raw,renamed_members)
                        grant.revoke(replacement['request_id'])
                        case['composition_change_reapproval_verified']=True
                    finally:
                        physical('header-layout',dict(mutation,changed=False))
                        owner.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'catalog_paths':[prefix+'envi']})
                    receipt=grant.grant(model)
                    report['source_authorization']['grants'].append(dict(model=model,receipt=receipt))
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
                # Cached bytes remain subject to the current exact source Grant.
                receipt,withdrawal=verify_source_revocation(client,grant,receipt,model,case['preview_url'],item_id=item['id'])
                report['source_authorization']['grants'].append(dict(model=model,receipt=receipt))
                case.update(current_source_revocation_verified=True,source_revocation=withdrawal)
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
        browser_environment={k:v for k,v in environment.items() if k not in {'ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN','ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'}}
        browser=browser_runner(evidence) if browser_runner else MEDIA.run_browser(repository,browser_environment,evidence,owner)
        require(browser.get('result')=='passed' and len(browser.get('cases',[]))==6,'complete ENVI browser proof')
        validate_browser(owner,browser,evidence,artifact)
        report['browser']=browser
        require(physical('verify')=={'sources_unchanged':True},'sources changed')
        report['sources_unchanged']=True;report['result']='passed'
        return report
    except BaseException as error:
        failure=error
        report.update(result='failed',stage=stage,error=type(error).__name__);raise
    finally:
        errors=[]
        for fixture in reversed(fixtures):
            if not fixture.task_id:continue
            try:
                report['cleanup'][fixture.format]=cleanup_fixture(client,fixture,timeout,tenant,owner,physical)
            except Exception as error:errors.append(type(error).__name__)
        if any(f.task_id for f in fixtures):
            try:
                report['artifact_residuals']=physical('residuals',dict(tenant_id=tenant))
                require(report['artifact_residuals']==dict(objects_absent=True,temporary_inputs_removed=True,source_unchanged=True),'ENVI artifact residuals')
            except Exception as error:errors.append(type(error).__name__)
        source_errors=[]
        for grant in reversed(grants):
            try:grant.cleanup()
            except Exception as error:source_errors.append(type(error).__name__)
        hidden=0
        for fixture in fixtures:
            try:
                client.request('GET',f"/api/v1/meta/items/{fixture.item['id']}",(404,))
                hidden+=1
            except Exception as error:source_errors.append(type(error).__name__)
        report['source_authorization']['cleanup']=dict(read_grants_revoked=not source_errors,metadata_details_hidden=hidden)
        errors.extend(source_errors)
        if seeded:
            try:report['source_cleanup']=physical('delete');require(report['source_cleanup']==dict(objects_removed=True,files_removed=True),'source cleanup')
            except Exception as error:errors.append(type(error).__name__)
        if errors:report.update(result='failed',cleanup_errors=errors)
        (artifact/'manager-envi.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
        if errors and failure is None:raise ValueError('ENVI cleanup failed: '+','.join(errors))
