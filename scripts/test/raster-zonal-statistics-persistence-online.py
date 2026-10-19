#!/usr/bin/env python3
"""Registered aligned rasters -> complete CSV -> exact scans/current execution lineage."""
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
fixture=importlib.import_module('business.scripts.online-raster-zonal-statistics-fixture')
scan_ids=importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
support=raster.support
obj,array,positive,SuiteError=raster.obj,raster.array,raster.positive,raster.SuiteError
SUITE=fixture.SUITE


class DiagnosticClient(rasterize.DiagnosticClient):
    diagnostic_name='zonal-statistics-submission-error.json'
    diagnostic_schema='addp.raster-zonal-statistics-submission-diagnostic/v1'


def definition(sources,parent,name,*,invalid=None):
    if name not in fixture.TARGETS or invalid not in (None,'band','grid') or (invalid and name!='classified.csv'):
        raise SuiteError('unknown exact zonal statistics case')
    tasks=[{'id':key,'operator':'raster_load','depends_on':[],'params':{'locator':sources[key+'.tif']}} for key in ('values','zones')]
    zones='zones';zones_band={'classified.csv':2,'reclassified.csv':1,'many-zones.csv':3,'empty.csv':4}[name]
    if name=='reclassified.csv':
        tasks.append({'id':'reclassified','operator':'raster_reclassify','depends_on':['zones'],'params':{
            'input_raster':{'$ref':'zones'},'band':2,'rules':[{'value':c,'class':v} for c,v in [(-3,10),(0,10),(2,20),(7,30)]]}})
        zones='reclassified'
    if invalid=='grid':
        tasks.append({'id':'bad_grid','operator':'raster_resample','depends_on':['zones'],
            'params':{'input_raster':{'$ref':'zones'},'size':[6,3],'resampling':'nearest'}})
        zones='bad_grid'
    tasks.append({'id':'stats','operator':'raster_zonal_statistics','depends_on':['values',zones],
        'params':{'input_raster':{'$ref':'values'},'zones_raster':{'$ref':zones},'band':2,'zones_band':99 if invalid=='band' else zones_band}})
    tasks.append({'id':'save','operator':'save','depends_on':['stats'],'params':{
        'input_df':{'$ref':'stats'},'target_parent_locator':parent,
        'target_name':f'failed-{invalid}.csv' if invalid else name,'mode':'fail'}})
    return {'tasks':tasks}


def physical(repository,env,names):
    result=subprocess.run([sys.executable,'business/scripts/online-raster-zonal-statistics-fixture.py','verify',*names],
        cwd=repository,env=dict(env),text=True,capture_output=True,timeout=150)
    if result.returncode:raise SuiteError('independent complete CSV verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout),'zonal physical evidence')


def validate_success(execution,sources,target):
    if obj(execution.get('outputs'),'outputs')!={'save':{'resource':{'locator':target,'type':'file','write_mode':'create'}}}:
        raise SuiteError('expected the exact named CSV file output')
    metadata=obj(execution.get('metadata'),'metadata');facts=obj(metadata.get('lineage_facts'),'lineage')
    if (facts.get('schema_version')!='addp.lineage-facts/v1'
        or [item.get('locator') for item in array(facts.get('inputs'),'inputs')]!=sources
        or [(item.get('locator'),item.get('write_mode')) for item in array(facts.get('outputs'),'lineage outputs')]!=[(target,'create')]):
        raise SuiteError('zonal source/target lineage differs')
    result=obj(metadata.get('result'),'result');scans=array(result.get('meta_scan_runs'),'scans')
    if (len(scans)!=1 or scans[0].get('status')!='submitted' or scans[0].get('target_locator')!=target
        or not isinstance(scans[0].get('execution_id'),str) or not scans[0]['execution_id']):
        raise SuiteError('expected one automatic exact CSV scan')
    return facts,scans[0]['execution_id'],result


def validate_preview(result,name):
    preview=obj(result,'table preview');rows=fixture.expected_rows(name)
    expected_schema=[{'name':column,'type':'int64' if i<4 else 'Float64'} for i,column in enumerate(fixture.COLUMNS)]
    if (preview.get('type')!='pandas_dataframe' or preview.get('schema')!=expected_schema
        or type(preview.get('row_count')) is not int or preview['row_count']!=len(rows)
        or type(preview.get('preview_limit')) is not int or preview['preview_limit']!=50
        or type(preview.get('truncated')) is not bool or preview['truncated']!=(len(rows)>50)):
        raise SuiteError('bounded table preview metadata differs')
    try:fixture.validate_records(array(preview.get('preview_rows'),'preview rows'),rows[:50])
    except fixture.FixtureError as error:raise SuiteError(str(error)) from error


def validate_item(item,native,name):
    size=item.get('size_bytes');physical_size=native.get('size_bytes')
    if type(size) is not int or size<=0 or type(physical_size) is not int or size!=physical_size or not item.get('scanned_at'):
        raise SuiteError('CSV physical/Meta sizes must be equal positive integers')
    descriptor=obj(obj(item.get('attributes'),'attributes').get('item'),'item facts')
    if item.get('item_type')!='file' or item.get('full_name')!='outputs/'+name or descriptor.get('format')!='csv':
        raise SuiteError('CSV file identity or format differs')
    if (native.get('complete_table_verified') is not True or native.get('schema')!=list(fixture.COLUMNS)
        or type(native.get('row_count')) is not int or native['row_count']!=len(fixture.expected_rows(name))):
        raise SuiteError('independent complete CSV schema/content evidence missing')
    return {'physical_size_bytes':physical_size,'meta_size_bytes':size}


def run_scenario(repository,env,client,physical_runner=physical):
    timeout=float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('zonal acceptance forbids the default tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=raster.EDITOR_PERMISSIONS)
    engine=positive(env['ADDP_ONLINE_ZONAL_STATISTICS_NFS_ENGINE_ID'],'NFS engine')
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
        facts,scan,result=validate_success(execution,list(sources.values()),target)
        validate_preview(result.get('final_result'),name)
        if scan in scans:raise SuiteError('automatic output scans must be distinct')
        scans.append(scan);scanned=raster.wait_execution(client,'meta',scan,timeout)
        if (type(scanned.get('tenant_id')) is not int or scanned['tenant_id']!=tenant
            or type(scanned.get('triggered_by')) is not int or scanned['triggered_by']!=int(identity['principal_id'])):
            raise SuiteError('automatic CSV scan changed tenant/user identity')
        monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'Monitor execution')
        if monitor.get('status')!='success' or monitor.get('module')!='develop' or obj(monitor.get('metadata'),'Monitor metadata').get('lineage_facts')!=facts:
            raise SuiteError('Monitor status, owner or lineage differs')
        names.append(name);native=physical_runner(repository,env,names);rasterize.check_physical(native,previous);previous=native['targets'].copy()
        found=support.find_fixture_item(client,engine,'outputs/'+name,'statistics CSV')
        item=obj(client.request('GET',f'/api/v1/meta/items/{found["id"]}',(200,)).payload,'CSV detail')
        sizes=validate_item(item,native['targets'][name],name)
        lineage=[raster.wait_lineage(client,items[n]['id'],item['id'],identifier,timeout) for n in fixture.SOURCES]
        cases.append({'name':name,'execution_id':identifier,'automatic_scan_execution_id':scan,'target_locator':target,
            'target_item_id':item['id'],'size_evidence':sizes,'physical':native['targets'][name],
            'lineage':lineage,'monitor_verified':True,'bounded_preview_verified':True})
    before_scans=scan_ids(client)
    if before_scans!=sorted([source_scan,*scans]):raise SuiteError('unexpected source or output scan')
    failures=[]
    for problem in ('band','grid'):
        identifier=raster.submit(client,runtime,definition(sources,parent,'classified.csv',invalid=problem))
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
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'zonal-statistics-persistence.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))


if __name__=='__main__':
    try:main()
    except Exception as error:
        print(f'Zonal statistics persistence acceptance failed: {error if isinstance(error,SuiteError) else type(error).__name__}',file=sys.stderr)
        raise SystemExit(1)
