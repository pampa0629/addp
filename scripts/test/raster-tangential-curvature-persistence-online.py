#!/usr/bin/env python3
"""Registered raster -> raster DEM tangential curvature -> COG -> exact automatic scan and current lineage."""
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
fixture=importlib.import_module('business.scripts.online-raster-tangential-curvature-fixture')
scan_ids=importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
validate_scan=importlib.import_module('scripts.test.raster-nibble-capacity-online').validate_scan
source_edges=importlib.import_module('scripts.test.raster-disk-pressure-online').source_edges
support=raster.support
obj,array,positive,SuiteError=raster.obj,raster.array,raster.positive,raster.SuiteError
SUITE=fixture.SUITE


class DiagnosticClient(rasterize.DiagnosticClient):
    diagnostic_name='tangential-curvature-submission-error.json'
    diagnostic_schema='addp.raster-tangential-curvature-submission-diagnostic/v1'


FAILURES=('band','alpha','complex','rotated','south_up','geographic','no_crs','no_grid','unit','unit_conflict','scale','offset','method')
SubmittedLanguageClient=importlib.import_module('scripts.test.raster-disk-pressure-online').SubmittedLanguageClient


def case_inputs(name):return [fixture.CASES[name][0]]


def definition(sources,parent,name,*,invalid=None,language='zh-cn'):
    if language not in ('zh-cn','en') or name not in fixture.TARGETS or invalid not in (None,*FAILURES) or (invalid and name!='tangential.cog.tif'):
        raise SuiteError('unknown exact curvature case')
    source,band,unit,stage,method=fixture.CASES[name]
    if invalid:
        source={'rotated':'rotated.tif','south_up':'south-up.tif','geographic':'geographic.tif','no_crs':'no-crs.tif','no_grid':'no-grid.tif','scale':'scaled.tif','offset':'offset.tif','complex':'complex.tif'}.get(invalid,source)
        if invalid=='unit':unit='yard'
        if invalid=='unit_conflict':unit='foot'
        if invalid=='alpha':band=4
        if invalid=='band':band=99
        if invalid=='method':method='bad'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'locator':sources[source]}}];prior='source'
    if stage=='fill':
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':band,'elevation_unit':unit}});prior='fill';band=1
    if stage=='reclassify':
        tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':band,
            'rules':[{'min':None,'max':0,'class':0},{'min':0,'max':None,'class':1}]}});prior='classes';band=1
    tasks.extend([{'id':'curvature','operator':'raster_curvature','depends_on':[prior],
        'params':{'input_raster':{'$ref':prior},'band':band,'elevation_unit':unit,**({'method':method} if method is not None else {})}},
        {'id':'save','operator':'raster_save','depends_on':['curvature'],'params':{
            'input_raster':{'$ref':'curvature'},'target_parent_locator':parent,'target_name':f'failed-{invalid}-{language}.cog.tif' if invalid else name,
            'write_mode':'create','profile':'cog','blocksize':128,'overview_resampling':'nearest'}}])
    return {'tasks':tasks}


MESSAGES={
 'method':('DEM 曲率的 method 必须为 mean、profile 或 tangential','DEM curvature method must be mean, profile or tangential'),
 'band':('DEM 切向曲率必须显式选择有效的一基高程数据波段','DEM tangential curvature requires an explicit valid one-based elevation band'),
 'alpha':('DEM 切向曲率不能选择 alpha 波段作为高程','DEM tangential curvature cannot use an alpha band as elevation'),
 'complex':('DEM 切向曲率不支持复数高程或复数 alpha 波段','DEM tangential curvature does not support complex elevation or alpha bands'),
 'unit':('DEM 切向曲率必须明确声明 metre、foot 或 us_survey_foot 高程单位','DEM tangential curvature requires elevation_unit metre, foot or us_survey_foot'),
 'unit_conflict':('DEM 切向曲率高程波段单位未知或与声明单位冲突','DEM tangential curvature band unit is unknown or conflicts with elevation_unit'),
}
for key in ('rotated','south_up','geographic','no_crs','no_grid'):
    MESSAGES[key]=('DEM 切向曲率需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影','DEM tangential curvature requires a projected north-up grid with known linear units; reproject explicitly')
for key in ('scale','offset'):
    MESSAGES[key]=('DEM 切向曲率前必须显式应用高程 scale/offset','DEM tangential curvature requires elevation scale/offset to be applied first')


def invalid_diagnostic(repository,env,identifier,problem,language,timeout):
    # Public safe projection hides computation causes. The exact owned execution
    # log must contain the submitted-language cause and the correct task stage.
    if problem not in MESSAGES or language not in ('zh-cn','en'):raise SuiteError('unknown curvature diagnostic')
    cause=MESSAGES[problem][language=='en']
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
                    and message.endswith('任务 curvature 执行失败: '+cause)):
                    return {'execution_id':identifier,'task':'curvature','cause':'invalid_'+problem,
                            'language':language,'source':'develop_runtime_log','exact_cause':cause}
        time.sleep(1)
    raise SuiteError('missing exact curvature task and submitted-language diagnostic')


def physical(repository,env,names):
    result=subprocess.run([sys.executable,'business/scripts/online-raster-tangential-curvature-fixture.py','verify',*names],
        cwd=repository,env=dict(env),text=True,capture_output=True,timeout=150)
    if result.returncode:raise SuiteError('independent raster DEM tangential curvature physical verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout),'raster DEM tangential curvature physical evidence')


def check_physical(native,previous):
    rasterize.check_physical(native,previous)
    targets=obj(native.get('targets'),'physical targets')
    pair=all(n in targets for n in ('mean-default.cog.tif','mean-explicit.cog.tif'))
    if native.get('mean_controls_equal') is not pair:raise SuiteError('missing exact default/explicit mean equivalence')
    present='tangential.cog.tif' in targets
    for key in ('tiny_gradient_verified','plan_distinction_verified'):
        if native.get(key) is not present:raise SuiteError('missing strict tangential semantic evidence: '+key)
    triple=all(n in targets for n in ('tangential.cog.tif','mean-explicit.cog.tif','profile-control.cog.tif'))
    pixels=native.get('directional_identity_pixels')
    if native.get('directional_identity_verified') is not triple or type(pixels) is not int or (pixels<=0 if triple else pixels!=0):
        raise SuiteError('missing saved mean/profile/tangential identity evidence')
    for name,fact in targets.items():
        if name not in fixture.CASES:raise SuiteError('unknown exact curvature target')
        for key in ('full_pixel_and_mask_verification','cog_valid','independent_quadratic_oracle_verified','complete_neighbourhood_verified','inverse_metre_unit_verified','nearest_overviews_verified','nine_equal_weights_verified','flat_zero_verified','zero_gradient_nodata_verified'):
            if fact.get(key) is not True:raise SuiteError('missing strict curvature physical evidence: '+key)
        if fact.get('profile_control_verified') is not (fixture.CASES[name][4]=='profile'):raise SuiteError('incorrect profile control evidence')
        if fact.get('mean_control_verified') is not (fixture.CASES[name][4] in (None,'mean')):raise SuiteError('incorrect mean control evidence')
        if fact.get('curvature_method')!=(fixture.CASES[name][4] or 'mean'):raise SuiteError('incorrect curvature method evidence')
        for key in ('flat_centres','undefined_stationary_centres'):
            count=fact.get(key)
            if type(count) is not int or count<0:raise SuiteError('invalid stationary semantic count')
        if name=='stationary.cog.tif' and fact['undefined_stationary_centres']!=1:raise SuiteError('stationary sample was not verified')
        if name=='flat.cog.tif' and fact['flat_centres']!=(fixture.HEIGHT-2)*(fixture.WIDTH-2):raise SuiteError('flat sample was not verified')
        h,w=fixture.expected_shape(name);count=0
        while max(h,w)>128:h,w=max(1,h//2),max(1,w//2);count+=1
        if fact.get('has_overviews') is not (count>0) or type(fact.get('overview_count')) is not int or fact['overview_count']!=count:
            raise SuiteError('incorrect exact overview evidence for actual dimensions')


def run_scenario(repository,env,client,physical_runner=physical,diagnostic_runner=invalid_diagnostic):
    timeout=float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('raster DEM tangential curvature acceptance forbids the default tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=raster.EDITOR_PERMISSIONS)
    engine=positive(env['ADDP_ONLINE_TANGENTIAL_CURVATURE_NFS_ENGINE_ID'],'NFS engine')
    scopes={name:f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
    source_scan=support.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'targets':list(scopes.values())})
    items={name:support.find_fixture_item(client,engine,name,'raster DEM tangential curvature source') for name in fixture.SOURCES}
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
            identifier=raster.submit(SubmittedLanguageClient(client,language),runtime,definition(sources,parent,'tangential.cog.tif',invalid=problem,language=language))
            failed=raster.wait_execution(client,'develop',identifier,timeout,expected='failed')
            if obj(failed.get('error_details'),'failed error').get('category')!='execution_failed':
                raise SuiteError('DEM tangential curvature failure has the wrong error category')
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
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'tangential-curvature-persistence.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))


if __name__=='__main__':
    try:main()
    except Exception as error:
        print(f'Raster DEM tangential curvature persistence acceptance failed: {error if isinstance(error,SuiteError) else type(error).__name__}',file=sys.stderr)
        raise SystemExit(1)
