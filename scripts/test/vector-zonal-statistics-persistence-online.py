#!/usr/bin/env python3
"""Registered vector zones -> complete GeoPackage -> exact scans/current execution lineage."""
from __future__ import annotations
import importlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time

raster=importlib.import_module('scripts.test.raster-workflow-online')
rasterize=importlib.import_module('scripts.test.vector-rasterize-persistence-online')
fixture=importlib.import_module('business.scripts.online-vector-zonal-statistics-fixture')
scan_ids=importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
support=raster.support
obj,array,positive,SuiteError=raster.obj,raster.array,raster.positive,raster.SuiteError
SUITE=fixture.SUITE
vector_persistence=importlib.import_module('scripts.utils.online_vector_persistence')


class DiagnosticClient(rasterize.DiagnosticClient):
    diagnostic_name='vector-zonal-statistics-submission-error.json'
    diagnostic_schema='addp.vector-zonal-statistics-submission-diagnostic/v1'


def used_sources(name):
    # Develop emits physical reads in stable node-ID order: faces, values.
    return ('values.tif',) if name=='classified.gpkg' else ('zones.geojson','values.tif')


def definition(sources,parent,name,*,invalid=None):
    if name not in fixture.TARGETS or invalid not in (None,'band','collision') or (invalid and name!='center.gpkg'):
        raise SuiteError('unknown exact vector-zone statistics case')
    tasks=[{'id':'values','operator':'raster_load','depends_on':[], 'params':{'locator':sources['values.tif']}}]
    if name=='classified.gpkg':
        tasks.extend([
            {'id':'classes','operator':'raster_reclassify','depends_on':['values'],'params':{
                'input_raster':{'$ref':'values'},'band':2,'rules':[
                    {'min':-100,'max':512,'class':-3},{'min':512,'max':1024,'class':0},{'min':1024,'max':2048,'class':7}]}},
            {'id':'faces','operator':'raster_polygonize','depends_on':['classes'],'params':{
                'input_raster':{'$ref':'classes'},'band':1,'connectivity':4}}])
    else:
        tasks.append({'id':'faces','operator':'load','depends_on':[],'params':{'locator':sources['zones.geojson']}})
    tasks.append({'id':'stats','operator':'raster_vector_zonal_statistics','depends_on':['values','faces'],'params':{
        'input_raster':{'$ref':'values'},'input_gdf':{'$ref':'faces'},'band':99 if invalid=='band' else 2,
        'all_touched':name=='touched.gpkg','field_prefix':'occupied_' if invalid=='collision' else 'stats_' if name=='touched.gpkg' else 'zonal_'}})
    tasks.append({'id':'save','operator':'save','depends_on':['stats'],'params':{
        'input_df':{'$ref':'stats'},'target_parent_locator':parent,
        'target_name':f'failed-{invalid}.gpkg' if invalid else name,'mode':'fail'}})
    return {'tasks':tasks}


def physical(repository,env,names):
    result=subprocess.run([sys.executable,'business/scripts/online-vector-zonal-statistics-fixture.py','verify',*names],
        cwd=repository,env=dict(env),text=True,capture_output=True,timeout=150)
    if result.returncode:raise SuiteError('independent complete vector-zone verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout),'zonal physical evidence')


# Generic save and exact execution/scan facts use the existing table persistence contract.
validate_success=importlib.import_module('scripts.test.raster-zonal-statistics-persistence-online').validate_success


def validate_item(item,native,name):
    sizes=vector_persistence.validate_item(item,native,name)
    if (native.get('complete_geometry_attributes_statistics_verified') is not True
        or native.get('schema')!=[list(v) for v in fixture.schema(name)]
        or type(native.get('feature_count')) is not int or native['feature_count']!=fixture.FEATURE_COUNTS[name]
        or native.get('crs')!='EPSG:4326'):
        raise SuiteError('independent complete GeoPackage evidence missing')
    return sizes


def run_scenario(repository,env,client,physical_runner=physical):
    timeout=float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('zonal acceptance forbids the default tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=raster.EDITOR_PERMISSIONS)
    engine=positive(env['ADDP_ONLINE_VECTOR_ZONAL_STATISTICS_NFS_ENGINE_ID'],'NFS engine')
    scopes={name:f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
    source_scan=support.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'targets':list(scopes.values())})
    items={name:support.find_fixture_item(client,engine,name,'zonal source') for name in fixture.SOURCES}
    sources={name:scopes[name]+f'&item_id={items[name]["id"]}' for name in fixture.SOURCES}
    before={name:client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload for name,item in items.items()}
    runtimes=array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'workflow engines')
    selected=[r for r in runtimes if r.get('engine_type')=='geopython_workflow' and r.get('connection_status')=='online']
    if len(selected)!=1:raise SuiteError('expected exactly one online product GeoPython Runtime')
    runtime=positive(selected[0].get('id'),'Runtime');parent=f'addp://engine/{engine}/path/outputs?type=directory'
    cases,names,previous,scans=[],[],{},[]
    for name in fixture.TARGETS:
        target=f'addp://engine/{engine}/path/outputs/{name}?type=file'
        identifier=raster.submit(client,runtime,definition(sources,parent,name))
        execution=raster.wait_execution(client,'develop',identifier,timeout)
        facts,scan,result=validate_success(execution,[sources[n] for n in used_sources(name)],target)
        if scan in scans:raise SuiteError('automatic output scans must be distinct')
        scans.append(scan);scanned=raster.wait_execution(client,'meta',scan,timeout)
        if (type(scanned.get('tenant_id')) is not int or scanned['tenant_id']!=tenant
            or type(scanned.get('triggered_by')) is not int or scanned['triggered_by']!=int(identity['principal_id'])):
            raise SuiteError('automatic GeoPackage scan changed tenant/user identity')
        monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'Monitor execution')
        if monitor.get('status')!='success' or monitor.get('module')!='develop' or obj(monitor.get('metadata'),'Monitor metadata').get('lineage_facts')!=facts:
            raise SuiteError('Monitor status, owner or lineage differs')
        names.append(name);native=physical_runner(repository,env,names);rasterize.check_physical(native,previous);previous=native['targets'].copy()
        found=support.find_fixture_item(client,engine,'outputs/'+name,'statistics GeoPackage')
        item=obj(client.request('GET',f'/api/v1/meta/items/{found["id"]}',(200,)).payload,'GeoPackage detail')
        sizes=validate_item(item,native['targets'][name],name)
        lineage=[raster.wait_lineage(client,items[n]['id'],item['id'],identifier,timeout) for n in used_sources(name)]
        cases.append({'name':name,'execution_id':identifier,'automatic_scan_execution_id':scan,'target_locator':target,
            'target_item_id':item['id'],'size_evidence':sizes,'physical':native['targets'][name],
            'lineage':lineage,'monitor_verified':True})
    before_scans=scan_ids(client)
    if before_scans!=sorted([source_scan,*scans]):raise SuiteError('unexpected source or output scan')
    failures=[]
    for problem in ('band','collision'):
        identifier=raster.submit(client,runtime,definition(sources,parent,'center.gpkg',invalid=problem))
        failed=raster.wait_execution(client,'develop',identifier,timeout,expected='failed')
        result=obj(failed.get('metadata'),'failed metadata').get('result')
        if result is not None:result=obj(result,'failed result')
        if failed.get('outputs') or (result is not None and result.get('meta_scan_runs')):raise SuiteError('invalid zonal parameters published a target or scan')
        if scan_ids(client)!=before_scans:raise SuiteError('invalid zonal parameters added a scan')
        native=physical_runner(repository,env,names);rasterize.check_physical(native,previous)
        failures.append({'problem':problem,'execution_id':identifier,'status':'failed','no_publication_or_scan':True})
    for name,item in items.items():
        if client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload!=before[name]:raise SuiteError('source metadata changed')
    return {'schema_version':'addp.'+SUITE+'/v1','suite':SUITE,'result':'passed','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity':identity,'sources':sources,'source_scan_execution_id':source_scan,'source_unchanged':True,
        'cases':cases,'failed_parameters':failures,'physical':native,
        'cleanup_boundary':'owned NFS export/directory and disposable deployment; no business DELETE acceptance'}


def main():
    env=dict(os.environ)
    report=run_scenario(Path(__file__).resolve().parents[2],env,
        DiagnosticClient(env['GATEWAY_URL'],env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30,env['ADDP_ONLINE_ARTIFACT_DIR']))
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'vector-zonal-statistics-persistence.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))


if __name__=='__main__':
    try:main()
    except Exception as error:
        print(f'Vector-zone statistics persistence acceptance failed: {error if isinstance(error,SuiteError) else type(error).__name__}',file=sys.stderr)
        raise SystemExit(1)
