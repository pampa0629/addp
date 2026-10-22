#!/usr/bin/env python3
"""Registered raster -> raster DEM filling -> COG -> exact automatic scan and current lineage."""
from __future__ import annotations
import importlib
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import time

raster=importlib.import_module('scripts.test.raster-workflow-online')
rasterize=importlib.import_module('scripts.test.vector-rasterize-persistence-online')
fixture=importlib.import_module('business.scripts.online-raster-fill-depressions-fixture')
scan_ids=importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
validate_scan=importlib.import_module('scripts.test.raster-nibble-capacity-online').validate_scan
source_edges=importlib.import_module('scripts.test.raster-disk-pressure-online').source_edges
support=raster.support
obj,array,positive,SuiteError=raster.obj,raster.array,raster.positive,raster.SuiteError
SUITE=fixture.SUITE


class DiagnosticClient(rasterize.DiagnosticClient):
    diagnostic_name='fill-depressions-submission-error.json'
    diagnostic_schema='addp.raster-fill-depressions-submission-diagnostic/v1'


FAILURES=('band','grid','unit','scale')


def definition(sources,parent,name,*,invalid=None):
    if name not in fixture.TARGETS or invalid not in (None,*FAILURES) or (invalid and name!='filled.cog.tif'):
        raise SuiteError('unknown exact DEM filling case')
    source_name,band,unit=fixture.CASES[name]
    source_name={'grid':'rotated.tif','scale':'scaled.tif'}.get(invalid,source_name)
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'locator':sources[source_name]}},
        {'id':'fill','operator':'raster_fill_depressions','depends_on':['source'],
         'params':{'input_raster':{'$ref':'source'},'band':99 if invalid=='band' else band,
                   'elevation_unit':'foot' if invalid=='unit' else unit}},
        {'id':'save','operator':'raster_save','depends_on':['fill'],
         'params':{'input_raster':{'$ref':'fill'},'target_parent_locator':parent,
                   'target_name':f'failed-{invalid}.cog.tif' if invalid else name,
                   'write_mode':'create','profile':'cog','blocksize':128}}]
    return {'tasks':tasks}


def invalid_diagnostic(repository,env,identifier,problem,timeout):
    # The ordinary user's safe observation hides raw computation errors. Match
    # only our exact execution and bounded cause in the owned deployment logs.
    messages={'band':'DEM 填洼必须显式选择有效的一基高程数据波段',
              'grid':'DEM 填洼需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影',
              'unit':'DEM 填洼高程波段单位未知或与声明单位冲突',
              'scale':'DEM 填洼前必须显式应用高程 scale/offset'}
    if problem not in messages:raise SuiteError('unknown DEM filling diagnostic')
    root=Path(env.get('ADDP_RUNTIME_LOG_ROOT',repository/'logs/runtime'))
    if not root.is_absolute():root=repository/root
    deadline=time.monotonic()+timeout
    while time.monotonic()<deadline:
        for path in (root/'develop').glob('*/*.jsonl'):
            for line in path.read_text().splitlines():
                try:event=json.loads(line)
                except json.JSONDecodeError:continue
                message=event.get('message','')
                if (event.get('module_name')=='develop' and event.get('role')=='backend'
                    and f'[DevExecutor] 工作流引擎调用失败: execution_id={identifier} err=' in message
                    and message.endswith('任务 fill 执行失败: '+messages[problem])):
                    return {'execution_id':identifier,'task':'fill','cause':'invalid_'+problem,'source':'develop_runtime_log'}
        time.sleep(1)
    raise SuiteError('missing exact DEM filling task and parameter failure diagnostic')


def physical(repository,env,names):
    result=subprocess.run([sys.executable,'business/scripts/online-raster-fill-depressions-fixture.py','verify',*names],
        cwd=repository,env=dict(env),text=True,capture_output=True,timeout=150)
    if result.returncode:raise SuiteError('independent raster DEM filling physical verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout),'raster DEM filling physical evidence')


def check_physical(native,previous):
    rasterize.check_physical(native,previous)
    for fact in obj(native.get('targets'),'physical targets').values():
        if (fact.get('full_pixel_and_mask_verification') is not True or fact.get('cog_valid') is not True
            or fact.get('has_overviews') is not True or fact.get('depression_fill_verified') is not True or fact.get('unit_metres_verified') is not True or fact.get('open_outlets_verified') is not True or fact.get('never_lowered_verified') is not True):
            raise SuiteError('complete pixels/mask or COG/overview evidence missing')


def run_scenario(repository,env,client,physical_runner=physical,diagnostic_runner=invalid_diagnostic):
    timeout=float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('raster DEM filling acceptance forbids the default tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=raster.EDITOR_PERMISSIONS)
    engine=positive(env['ADDP_ONLINE_FILL_DEPRESSIONS_NFS_ENGINE_ID'],'NFS engine')
    scopes={name:f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
    source_scan=support.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'targets':list(scopes.values())})
    items={name:support.find_fixture_item(client,engine,name,'raster DEM filling source') for name in fixture.SOURCES}
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
        inputs=[fixture.CASES[name][0]]
        facts,scan=raster.validate_success(execution,[sources[n] for n in inputs],target,'create',expectation,resource_type='file')
        if scan in scans:raise SuiteError('automatic output scans must be distinct')
        scans.append(scan)
        scanned=raster.wait_execution(client,'meta',scan,timeout)
        scan_identity=validate_scan(scanned,scan,identifier,tenant,int(identity['principal_id']),engine)
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
            'lineage':lineage,'scan_identity':scan_identity,'monitor_verified':True})
    before_scans=scan_ids(client)
    if before_scans!=sorted([source_scan,*scans]):raise SuiteError('unexpected source or output scan')
    failures=[]
    old_edges={name:source_edges(client,item['id']) for name,item in items.items()}
    for problem in FAILURES:
        identifier=raster.submit(client,runtime,definition(sources,parent,'filled.cog.tif',invalid=problem))
        failed=raster.wait_execution(client,'develop',identifier,timeout,expected='failed')
        if obj(failed.get('error_details'),'failed error').get('category')!='execution_failed':
            raise SuiteError('DEM filling failure has the wrong error category')
        diagnostic=diagnostic_runner(repository,env,identifier,problem,timeout)
        failed_result=obj(failed.get('metadata'),'failed metadata').get('result')
        if failed_result is not None:failed_result=obj(failed_result,'failed result')
        if failed.get('outputs') or (failed_result is not None and failed_result.get('meta_scan_runs')):
            raise SuiteError('invalid parameter published a target or automatic scan')
        if scan_ids(client)!=before_scans:raise SuiteError('invalid parameter added a scan')
        if any(source_edges(client,item['id'])!=old_edges[name] for name,item in items.items()):
            raise SuiteError('invalid parameter added derived lineage')
        if failed.get('metadata',{}).get('lineage_facts',{}).get('outputs'):
            raise SuiteError('invalid parameter published output lineage')
        native=physical_runner(repository,env,names);check_physical(native,previous)
        failures.append({'problem':problem,'execution_id':identifier,'status':'failed','no_publication_or_scan_or_lineage':True,'diagnostic':diagnostic})
    for name,item in items.items():
        if client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload!=before[name]:
            raise SuiteError('source metadata changed')
    return {'schema_version':'addp.'+SUITE+'/v1','suite':SUITE,'result':'passed','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity':identity,'sources':sources,'source_scan_execution_id':source_scan,'source_unchanged':True,
        'cases':cases,'failed_parameters':failures,
        'physical':native,'cleanup_boundary':'owned NFS export/directory and disposable deployment; no business DELETE acceptance'}


def main():
    env=dict(os.environ)
    report=run_scenario(Path(__file__).resolve().parents[2],env,
        DiagnosticClient(env['GATEWAY_URL'],env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30,env['ADDP_ONLINE_ARTIFACT_DIR']))
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'fill-depressions-persistence.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))


if __name__=='__main__':
    try:main()
    except Exception as error:
        print(f'Raster DEM filling persistence acceptance failed: {error if isinstance(error,SuiteError) else type(error).__name__}',file=sys.stderr)
        raise SystemExit(1)
