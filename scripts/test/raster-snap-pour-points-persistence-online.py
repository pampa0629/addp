#!/usr/bin/env python3
"""Registered raster -> raster pour-point snapping -> COG -> exact automatic scan and current lineage."""
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
fixture=importlib.import_module('business.scripts.online-raster-snap-pour-points-fixture')
scan_ids=importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
validate_scan=importlib.import_module('scripts.test.raster-nibble-capacity-online').validate_scan
source_edges=importlib.import_module('scripts.test.raster-disk-pressure-online').source_edges
support=raster.support
obj,array,positive,SuiteError=raster.obj,raster.array,raster.positive,raster.SuiteError
SUITE=fixture.SUITE


class DiagnosticClient(rasterize.DiagnosticClient):
    diagnostic_name='snap-pour-points-submission-error.json'
    diagnostic_schema='addp.raster-snap-pour-points-submission-diagnostic/v1'


FAILURES=('collision','duplicate','empty','all_invalid','counts_values','points_values','coverage','alignment','radius','band','points_band','grid','nested')
SubmittedLanguageClient=importlib.import_module('scripts.test.raster-disk-pressure-online').SubmittedLanguageClient


def case_inputs(name):
    return list(fixture.CASES[name][:2])


def definition(sources,parent,name,*,invalid=None,language='zh-cn'):
    if language not in ('zh-cn','en') or name not in fixture.TARGETS or invalid not in (None,*FAILURES) or (invalid and name!='snap.cog.tif'):
        raise SuiteError('unknown exact pour-point snapping case')
    source,points,band,points_band,stage,radius=fixture.CASES[name]
    if invalid:
        source={'counts_values':'invalid-counts.tif','points_values':'id-counts.tif','grid':'rotated-counts.tif','nested':'directions.tif'}.get(invalid,'counts.tif')
        points={'collision':'collision-points.tif','duplicate':'duplicate-points.tif','empty':'empty-points.tif',
                'coverage':'missing-points.tif','alignment':'shifted-points.tif','counts_values':'id-points.tif',
                'points_values':'invalid-points.tif','nested':'nested-points.tif'}.get(invalid,'points.tif')
        stage='watershed' if invalid=='nested' else 'direct';radius=0. if invalid=='nested' else -1. if invalid=='radius' else 4.
        if invalid=='all_invalid':band=points_band=3
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'locator':sources[source]}},
           {'id':'points','operator':'raster_load','depends_on':[],'params':{'locator':sources[points]}}]
    prior='source';point_node='points'
    if stage=='fill':
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[prior],
            'params':{'input_raster':{'$ref':prior},'band':band,'elevation_unit':'metre'}});prior='fill';band=1
    if stage in ('fill','direction'):
        tasks.append({'id':'direction','operator':'raster_flow_direction_d8','depends_on':[prior],
            'params':{'input_raster':{'$ref':prior},'band':band,'elevation_unit':'metre'}});prior='direction';band=1
    if stage in ('fill','direction','watershed'):
        tasks.append({'id':'counts','operator':'raster_flow_accumulation_d8','depends_on':[prior],
            'params':{'input_raster':{'$ref':prior},'band':band}});prior='counts';band=1
    if stage=='reclass':
        tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':['points'],'params':{
            'input_raster':{'$ref':'points'},'band':points_band,'rules':[
                {'value':0,'class':0},{'value':91,'class':901},{'value':7,'class':70},
                {'value':88,'class':880},{'value':2**53-1,'class':2**53-1}]}});point_node='classes';points_band=1
    tasks.append({'id':'snap','operator':'raster_snap_pour_points','depends_on':[prior,point_node],
        'params':{'input_raster':{'$ref':prior},'points_raster':{'$ref':point_node},'band':99 if invalid=='band' else band,
            'points_band':99 if invalid=='points_band' else points_band,'radius_metres':radius}});prior='snap'
    if stage=='watershed':
        tasks.append({'id':'watershed','operator':'raster_watershed_d8','depends_on':['source','snap'],
            'params':{'input_raster':{'$ref':'source'},'points_raster':{'$ref':'snap'},'band':2,'points_band':1}});prior='watershed'
    tasks.append({'id':'save','operator':'raster_save','depends_on':[prior],
        'params':{'input_raster':{'$ref':prior},'target_parent_locator':parent,
            'target_name':f'failed-{invalid}-{language}.cog.tif' if invalid else name,
            'write_mode':'create','profile':'cog','blocksize':128,'overview_resampling':'nearest'}})
    return {'tasks':tasks}


MESSAGES={'collision': ('汇水点吸附碰撞：多个编号选到同一目标像元，整个工作流失败',
               'Pour-point snapping collision: multiple IDs selected the same target cell; the entire '
               'workflow fails'),
 'duplicate': ('汇水点吸附的正编号必须全图唯一，一个编号只能标记一个像元',
               'Pour-point snapping requires globally unique positive IDs, one cell per ID'),
 'empty': ('汇水点吸附必须至少选择一个有效汇水点', 'Pour-point snapping requires at least one effective selected point'),
 'counts_values': ('汇水点吸附的有效未加权累积计数必须为0至2^53−1内的精确整数',
                   'Pour-point snapping requires exact integer unweighted accumulation counts from 0 to '
                   '2^53-1'),
 'points_values': ('汇水点吸附的有效标记必须为0至2^53−1内的精确整数，0为未选择',
                   'Pour-point snapping requires exact integer point values from 0 to 2^53-1; zero means '
                   'unselected'),
 'coverage': ('汇水点吸附的计数与标记必须具有完全相同的有效覆盖，不能取交集或补零',
              'Pour-point snapping requires exactly the same effective coverage for counts and points; do '
              'not intersect coverage or fill missing values with zero'),
 'alignment': ('汇水点吸附的计数与标记必须具有相同尺寸、完整仿射网格和等价CRS，请先显式对齐',
               'Pour-point snapping requires counts and points to have the same dimensions, full affine grid '
               'and equivalent CRS; align explicitly'),
 'radius': ('汇水点吸附必须显式指定有限非负的米制半径，不接受布尔值或字符串',
            'Pour-point snapping requires an explicit finite nonnegative radius in metres, not a boolean or '
            'string'),
 'band': ('汇水点吸附必须显式选择有效的一基未加权累积计数波段',
          'Pour-point snapping requires an explicit valid one-based unweighted accumulation-count band'),
 'points_band': ('汇水点吸附必须显式选择有效的一基汇水点标记数据波段',
                 'Pour-point snapping requires an explicit valid one-based points data band'),
 'grid': ('汇水点吸附需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影',
          'Pour-point snapping requires a projected north-up grid with known linear units; reproject '
          'explicitly'),
 'all_invalid': ('汇水点吸附必须至少选择一个有效汇水点', 'Pour-point snapping requires at least one effective selected point'),
 'nested': ('D8 指定汇水点汇水区不支持嵌套汇水点：一个指定点不能沿下游到达另一指定点',
            'D8 specified-outlet watershed does not support nested points: a selected point cannot reach '
            'another selected point downstream')}


def invalid_diagnostic(repository,env,identifier,problem,language,timeout):
    # Public safe projection hides computation causes. The exact owned execution
    # log must contain the submitted-language cause and the correct task stage.
    if problem not in MESSAGES or language not in ('zh-cn','en'):raise SuiteError('unknown snapping diagnostic')
    cause=MESSAGES[problem][language=='en'];task='watershed' if problem=='nested' else 'snap'
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
                    and message.endswith('任务 '+task+' 执行失败: '+cause)):
                    return {'execution_id':identifier,'task':task,'cause':'invalid_'+problem,
                            'language':language,'source':'develop_runtime_log','exact_cause':cause}
        time.sleep(1)
    raise SuiteError('missing exact snapping task and submitted-language diagnostic')


def physical(repository,env,names):
    result=subprocess.run([sys.executable,'business/scripts/online-raster-snap-pour-points-fixture.py','verify',*names],
        cwd=repository,env=dict(env),text=True,capture_output=True,timeout=150)
    if result.returncode:raise SuiteError('independent raster pour-point snapping physical verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout),'raster pour-point snapping physical evidence')


def check_physical(native,previous):
    rasterize.check_physical(native,previous)
    for fact in obj(native.get('targets'),'physical targets').values():
        for key in ('full_pixel_and_mask_verification','cog_valid','has_overviews','snap_verified','point_ids_verified','unselected_zero_verified','radius_and_tie_verified','unit_cleared_verified'):
            if fact.get(key) is not True:raise SuiteError('missing strict D8 physical evidence: '+key)


def run_scenario(repository,env,client,physical_runner=physical,diagnostic_runner=invalid_diagnostic):
    timeout=float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('raster pour-point snapping acceptance forbids the default tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=raster.EDITOR_PERMISSIONS)
    engine=positive(env['ADDP_ONLINE_SNAP_POUR_POINTS_NFS_ENGINE_ID'],'NFS engine')
    scopes={name:f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
    source_scan=support.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'targets':list(scopes.values())})
    items={name:support.find_fixture_item(client,engine,name,'raster pour-point snapping source') for name in fixture.SOURCES}
    sources={name:scopes[name]+f'&item_id={items[name]["id"]}' for name in fixture.SOURCES}
    before={name:client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload for name,item in items.items()}
    runtimes=array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'workflow engines')
    selected=[r for r in runtimes if r.get('engine_type')=='geopython_workflow' and r.get('connection_status')=='online']
    if len(selected)!=1:raise SuiteError('expected exactly one online product GeoPython Runtime')
    runtime=positive(selected[0].get('id'),'Runtime engine')
    parent=f'addp://engine/{engine}/path/outputs?type=directory'
    cases,names,previous,scans=[],[],{},[]
    for name in fixture.TARGETS:
        expectation=fixture.artifact_expectation(name)
        target=f'addp://engine/{engine}/path/outputs/{name}?type=file'
        identifier=raster.submit(client,runtime,definition(sources,parent,name))
        execution=raster.wait_execution(client,'develop',identifier,timeout)
        inputs=case_inputs(name)
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
        for language in ('zh-cn','en'):
            identifier=raster.submit(SubmittedLanguageClient(client,language),runtime,definition(sources,parent,'snap.cog.tif',invalid=problem,language=language))
            failed=raster.wait_execution(client,'develop',identifier,timeout,expected='failed')
            if obj(failed.get('error_details'),'failed error').get('category')!='execution_failed':
                raise SuiteError('pour-point snapping failure has the wrong error category')
            diagnostic=diagnostic_runner(repository,env,identifier,problem,language,timeout)
            observed=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'failed Monitor execution')
            if (observed.get('status')!='failed' or observed.get('module')!='develop'
                or obj(observed.get('error_details'),'Monitor failure').get('category')!='execution_failed'
                or obj(observed.get('metadata'),'failed Monitor metadata').get('lineage_facts',{}).get('outputs')):
                raise SuiteError('Monitor failed status/category or absence of output lineage differs')
            opposite=SubmittedLanguageClient(client,'en' if language=='zh-cn' else 'zh-cn')
            queried=raster.wait_execution(opposite,'develop',identifier,timeout,expected='failed')
            if queried.get('error')!=failed.get('error') or queried.get('error_details')!=failed.get('error_details'):
                raise SuiteError('query language rewrote stored failure')
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
            failures.append({'problem':problem,'language':language,'execution_id':identifier,'status':'failed','no_publication_or_scan_or_lineage':True,'monitor_verified':True,'query_language_did_not_rewrite_failure':True,'diagnostic':diagnostic})
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
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'snap-pour-points-persistence.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))


if __name__=='__main__':
    try:main()
    except Exception as error:
        print(f'Raster pour-point snapping persistence acceptance failed: {error if isinstance(error,SuiteError) else type(error).__name__}',file=sys.stderr)
        raise SystemExit(1)
