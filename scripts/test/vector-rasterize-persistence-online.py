#!/usr/bin/env python3
"""Registered vectors/reference -> COG -> exact automatic scan and current lineage."""
from __future__ import annotations
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time

raster=importlib.import_module('scripts.test.raster-workflow-online')
fixture=importlib.import_module('business.scripts.online-vector-rasterize-fixture')
scan_ids=importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
support=raster.support
obj,array,positive,SuiteError=raster.obj,raster.array,raster.positive,raster.SuiteError
SUITE=fixture.SUITE


def definition(sources,parent,name,*,invalid=False):
    if name not in fixture.TARGETS or (invalid and name!='field.cog.tif'):
        raise SuiteError('unknown exact rasterization case')
    tasks=[{'id':'reference','operator':'raster_load','depends_on':[],'params':{'locator':sources['reference.tif']}}]
    if name=='roundtrip.cog.tif':
        tasks.append({'id':'faces','operator':'raster_polygonize','depends_on':['reference'],
            'params':{'input_raster':{'$ref':'reference'},'band':1,'connectivity':8}})
        params={'field':'class_value'}
    else:
        tasks.append({'id':'faces','operator':'load','depends_on':[],'params':{'locator':sources['zones.geojson']}})
        params={'burn_value':-1.5,'all_touched':True} if name=='constant.cog.tif' else {'field':'missing_field' if invalid else 'class_code','all_touched':False}
    tasks.append({'id':'burn','operator':'vector_rasterize','depends_on':['reference','faces'],
        'params':{'input_gdf':{'$ref':'faces'},'reference_raster':{'$ref':'reference'},**params}})
    tasks.append({'id':'save','operator':'raster_save','depends_on':['burn'],
        'params':{'input_raster':{'$ref':'burn'},'target_parent_locator':parent,
                  'target_name':'invalid.cog.tif' if invalid else name,'mode':'create','profile':'cog','blocksize':128}})
    return {'tasks':tasks}


def physical(repository,env,names):
    result=subprocess.run([sys.executable,'business/scripts/online-vector-rasterize-fixture.py','verify',*names],
        cwd=repository,env=dict(env),text=True,capture_output=True,timeout=150)
    if result.returncode:raise SuiteError('independent rasterization physical verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout),'rasterization physical evidence')


def check_physical(native,previous):
    if native.get('runtime_workspaces_removed') is not True:
        raise SuiteError('Runtime execution workspace cleanup was not verified')
    for name,fact in previous.items():
        if obj(native.get('targets'),'physical targets').get(name)!=fact:
            raise SuiteError('later execution changed an earlier COG')


def run_scenario(repository,env,client,physical_runner=physical):
    timeout=float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('rasterization acceptance forbids the default tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=raster.EDITOR_PERMISSIONS)
    engine=positive(env['ADDP_ONLINE_RASTERIZE_NFS_ENGINE_ID'],'NFS engine')
    scopes={name:f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
    source_scan=support.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'targets':list(scopes.values())})
    items={name:support.find_fixture_item(client,engine,name,'rasterization source') for name in fixture.SOURCES}
    sources={name:scopes[name]+f'&item_id={items[name]["id"]}' for name in fixture.SOURCES}
    before={name:client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload for name,item in items.items()}
    runtimes=array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'workflow engines')
    selected=[r for r in runtimes if r.get('engine_type')=='geopython_workflow' and r.get('connection_status')=='online']
    if len(selected)!=1:raise SuiteError('expected exactly one online product GeoPython Runtime')
    runtime=positive(selected[0].get('id'),'Runtime engine')
    parent=f'addp://engine/{engine}/path/outputs?type=directory'
    cases,names,previous,scans=[],[],{},[]
    expectation=fixture.artifact_expectation()
    for name in fixture.TARGETS:
        target=f'addp://engine/{engine}/path/outputs/{name}?type=file'
        identifier=raster.submit(client,runtime,definition(sources,parent,name))
        execution=raster.wait_execution(client,'develop',identifier,timeout)
        inputs=['reference.tif'] if name=='roundtrip.cog.tif' else list(fixture.SOURCES)
        facts,scan=raster.validate_success(execution,[sources[n] for n in inputs],target,'create',expectation,resource_type='file')
        if scan in scans:raise SuiteError('automatic output scans must be distinct')
        scans.append(scan)
        scanned=raster.wait_execution(client,'meta',scan,timeout)
        if (type(scanned.get('tenant_id')) is not int or scanned['tenant_id']!=tenant
            or type(scanned.get('triggered_by')) is not int or scanned['triggered_by']!=int(identity['principal_id'])):
            raise SuiteError('automatic scan changed the tenant/user identity')
        monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'Monitor execution')
        if monitor.get('status')!='success' or monitor.get('module')!='develop' or obj(monitor.get('metadata'),'Monitor metadata').get('lineage_facts')!=facts:
            raise SuiteError('Monitor status, owner or lineage differs')
        names.append(name);native=physical_runner(repository,env,names);check_physical(native,previous);previous=native['targets'].copy()
        target_item=support.find_fixture_item(client,engine,'outputs/'+name,'raster COG')
        item=obj(client.request('GET',f'/api/v1/meta/items/{target_item["id"]}',(200,)).payload,'target detail')
        raster.validate_target_item(item,expectation,native['targets'][name])
        artifact=obj(obj(execution['metadata'].get('result'),'result').get('final_result'),'artifact')
        sizes=raster.strict_sizes(artifact,native['targets'][name],item,name)
        lineage=[raster.wait_lineage(client,items[n]['id'],item['id'],identifier,timeout) for n in inputs]
        cases.append({'name':name,'execution_id':identifier,'automatic_scan_execution_id':scan,'target_locator':target,
            'target_item_id':item['id'],'size_evidence':sizes,'physical':native['targets'][name],
            'lineage':lineage,'monitor_verified':True})
    before_scans=scan_ids(client)
    if before_scans!=sorted([source_scan,*scans]):raise SuiteError('unexpected source or output scan')
    identifier=raster.submit(client,runtime,definition(sources,parent,'field.cog.tif',invalid=True))
    failed=raster.wait_execution(client,'develop',identifier,timeout,expected='failed')
    failed_result=obj(failed.get('metadata'),'failed metadata').get('result')
    if failed_result is not None:failed_result=obj(failed_result,'failed result')
    if failed.get('outputs') or (failed_result is not None and failed_result.get('meta_scan_runs')):
        raise SuiteError('invalid parameter published a target or automatic scan')
    if scan_ids(client)!=before_scans:raise SuiteError('invalid parameter added a scan')
    native=physical_runner(repository,env,names);check_physical(native,previous)
    for name,item in items.items():
        if client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload!=before[name]:
            raise SuiteError('source metadata changed')
    return {'schema_version':'addp.'+SUITE+'/v1','suite':SUITE,'result':'passed','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity':identity,'sources':sources,'source_scan_execution_id':source_scan,'source_unchanged':True,
        'cases':cases,'failed_parameter':{'execution_id':identifier,'status':'failed','no_publication_or_scan':True},
        'physical':native,'cleanup_boundary':'owned NFS export/directory and disposable deployment; no business DELETE acceptance'}


def main():
    env=dict(os.environ)
    report=run_scenario(Path(__file__).resolve().parents[2],env,
        raster.GatewayClient(env['GATEWAY_URL'],env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30))
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'rasterize-persistence.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))


if __name__=='__main__':
    try:main()
    except Exception as error:
        print(f'Rasterization persistence acceptance failed: {error if isinstance(error,SuiteError) else type(error).__name__}',file=sys.stderr)
        raise SystemExit(1)
