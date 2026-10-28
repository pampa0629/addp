#!/usr/bin/env python3
"""Registered raster -> raster DEM topographic position index -> COG -> exact automatic scan and current lineage."""
from __future__ import annotations
from contextlib import contextmanager
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
fixture=importlib.import_module('business.scripts.online-raster-tpi-fixture')
scan_ids=importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
validate_scan=importlib.import_module('scripts.test.raster-nibble-capacity-online').validate_scan
source_edges=importlib.import_module('scripts.test.raster-disk-pressure-online').source_edges
support=raster.support
obj,array,positive,SuiteError=raster.obj,raster.array,raster.positive,raster.SuiteError
SUITE=fixture.SUITE
lineage_observer=importlib.import_module('scripts.utils.online_raster_lineage')
class TpiProgress(importlib.import_module('scripts.test.raster-profile-curvature-persistence-online').CurvatureProgress):
    SUITES={SUITE:'tpi-progress.json'}
    STAGES=importlib.import_module('scripts.test.raster-profile-curvature-persistence-online').CurvatureProgress.STAGES|{'identity','oracle_prepare','oracle_cleanup'}
    MEASURES={'submit_wait','internal_lineage','diagnostic','monitor_query','absence_checks','physical_verify_archive','metadata','archive_receipts'}

    @contextmanager
    def measure(self,name):
        if name not in self.MEASURES or not self.stages or self.stages[-1]['status']!='running':raise SuiteError('unknown TPI measurement')
        start=time.monotonic()
        try:yield
        finally:
            values=self.stages[-1].setdefault('substage_ms',{});values[name]=values.get(name,0)+round((time.monotonic()-start)*1000)

    def physical_profile(self,native):
        self.stages[-1]['physical_profile_ms']={'worker_total_ms':native['worker_total_ms'],'worker':native['verification_timing_ms'],'host':native['host_timing_ms']}

    def snapshot(self):
        value=super().snapshot();value['schema_version']='addp.raster-tpi-progress/v1';return value


class DiagnosticClient(rasterize.DiagnosticClient):
    diagnostic_name='tpi-submission-error.json'
    diagnostic_schema='addp.raster-tpi-submission-diagnostic/v1'


FAILURES=('band','alpha','complex','rotated','south_up','geographic','no_crs','no_grid','unit','unit_conflict','scale','offset','unit_unknown')
SubmittedLanguageClient=importlib.import_module('scripts.test.raster-disk-pressure-online').SubmittedLanguageClient


def case_inputs(name):return [fixture.CASES[name][0]]


def definition(sources,parent,name,*,invalid=None,language='zh-cn'):
    if language not in ('zh-cn','en') or name not in fixture.TARGETS or invalid not in (None,*FAILURES) or (invalid and name!='tpi.cog.tif'):
        raise SuiteError('unknown exact tpi case')
    source,band,unit,stage=fixture.CASES[name]
    if invalid:
        source={'rotated':'rotated.tif','south_up':'south-up.tif','geographic':'geographic.tif','no_crs':'no-crs.tif','no_grid':'no-grid.tif','scale':'scaled.tif','offset':'offset.tif','complex':'complex.tif','unit_unknown':'unknown-unit.tif'}.get(invalid,source)
        if invalid=='unit':unit='yard'
        if invalid=='unit_conflict':unit='foot'
        if invalid=='alpha':band=4
        if invalid=='band':band=99
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'locator':sources[source]}}];prior='source'
    if stage=='fill':
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':band,'elevation_unit':unit}});prior='fill';band=1
    if stage=='reclassify':
        tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':band,
            'rules':[{'min':None,'max':0,'class':0},{'min':0,'max':None,'class':1}]}});prior='classes';band=1
    tasks.extend([{'id':'tpi','operator':'raster_tpi','depends_on':[prior],
        'params':{'input_raster':{'$ref':prior},'band':band,'elevation_unit':unit}},
        {'id':'save','operator':'raster_save','depends_on':['tpi'],'params':{
            'input_raster':{'$ref':'tpi'},'target_parent_locator':parent,'target_name':f'failed-{invalid}-{language}.cog.tif' if invalid else name,
            'write_mode':'create','profile':'cog','blocksize':128,'overview_resampling':'nearest'}}])
    return {'tasks':tasks}


MESSAGES={
 'band':('DEM 地形位置指数必须显式选择有效的一基高程数据波段','DEM topographic position index requires an explicit valid one-based elevation band'),
 'alpha':('DEM 地形位置指数不能选择 alpha 波段作为高程','DEM topographic position index cannot use an alpha band as elevation'),
 'complex':('DEM 地形位置指数不支持复数高程或复数 alpha 波段','DEM topographic position index does not support complex elevation or alpha bands'),
 'unit':('DEM 地形位置指数必须明确声明 metre、foot 或 us_survey_foot 高程单位','DEM topographic position index requires elevation_unit metre, foot or us_survey_foot'),
}
for key in ('unit_conflict','unit_unknown'):
    MESSAGES[key]=('DEM 地形位置指数高程波段单位未知或与声明单位冲突','DEM topographic position index band unit is unknown or conflicts with elevation_unit')
for key in ('rotated','south_up','geographic','no_crs','no_grid'):
    MESSAGES[key]=('DEM 地形位置指数需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影','DEM topographic position index requires a projected north-up grid with known linear units; reproject explicitly')
for key in ('scale','offset'):
    MESSAGES[key]=('DEM 地形位置指数前必须显式应用高程 scale/offset','DEM topographic position index requires elevation scale/offset to be applied first')


def invalid_diagnostic(repository,env,identifier,problem,language,timeout):
    # Public safe projection hides computation causes. The exact owned execution
    # log must contain the submitted-language cause and the correct task stage.
    if problem not in MESSAGES or language not in ('zh-cn','en'):raise SuiteError('unknown tpi diagnostic')
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
                    and message.endswith('任务 tpi 执行失败: '+cause)):
                    return {'execution_id':identifier,'task':'tpi','cause':'invalid_'+problem,
                            'language':language,'source':'develop_runtime_log','exact_cause':cause}
        time.sleep(1)
    raise SuiteError('missing exact tpi task and submitted-language diagnostic')


def fixture_call(repository,env,action,names=()):
    if action not in ('verify','prepare-oracle','cleanup-oracle'):raise SuiteError('unknown exact oracle action')
    result=subprocess.run([sys.executable,'business/scripts/online-raster-tpi-fixture.py',action,*names],
        cwd=repository,env=dict(env),text=True,capture_output=True,timeout=150)
    if result.returncode:raise SuiteError('independent raster DEM topographic position index physical verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout),'raster DEM topographic position index physical evidence')


def physical(repository,env,names):return fixture_call(repository,env,'verify',names)


def check_physical(native,previous,scope_hash):
    rasterize.check_physical(native,previous)
    if native.get('runtime_workspaces_removed') is not True:raise SuiteError('Runtime workspace cleanup proof missing')
    cache=obj(native.get('oracle_cache'),'oracle cache proof')
    targets=obj(native.get('targets'),'targets')
    if (cache!={'scope_sha256':scope_hash,'read_only_expected':True,'expected_arrays_loaded':len(targets),'full_target_checks':len(targets)}
        or cache.get('read_only_expected')is not True or type(cache.get('expected_arrays_loaded'))is not int or type(cache.get('full_target_checks'))is not int):raise SuiteError('oracle cache scope/count proof differs')
    for key,fields in [('verification_timing_ms',{'source_hash_ms','cache_load_ms','target_hash_ms','pixels_mask_metadata_ms','cog_layout_ms','overviews_ms'}),('host_timing_ms',{'worker_transport_ms','source_receipt_ms','archive_ms'})]:
        values=obj(native.get(key),'physical timings')
        if set(values)!=fields or any(type(v)is not int or not 0<=v<=600000 for v in values.values()):raise SuiteError('invalid bounded physical timings')
    if type(native.get('worker_total_ms'))is not int or not 0<=native['worker_total_ms']<=600000:raise SuiteError('invalid bounded worker timing')
    targets=obj(native.get('targets'),'targets');archive=obj(native.get('archived_cogs'),'archived COGs')
    if set(targets)!=set(archive):raise SuiteError('original COG archive inventory differs')
    for name,fact in targets.items():
        if name not in fixture.TARGETS:raise SuiteError('unknown exact TPI target')
        for key in ('full_pixel_and_mask_verification','cog_valid','independent_decimal_oracle_verified','complete_neighbourhood_verified','metre_unit_verified','nearest_overviews_verified','eight_equal_neighbours_verified','legal_zero_verified'):
            if fact.get(key) is not True:raise SuiteError('strict TPI physical evidence missing: '+key)
        if type(fact.get('size_bytes')) is not int or fact['size_bytes']<=0 or not isinstance(fact.get('sha256'),str) or not re.fullmatch('[0-9a-f]{64}',fact['sha256']):raise SuiteError('invalid physical COG size/hash')
        for key in ('valid_pixels','zero_pixels'):
            if type(fact.get(key)) is not int or fact[key]<0:raise SuiteError('invalid TPI semantic count')
        expected=(fixture.HEIGHT-2)*(fixture.WIDTH-2)
        if fact['valid_pixels']>(fixture.HEIGHT-2)*(fixture.WIDTH-2) or fact['zero_pixels']>fact['valid_pixels']:raise SuiteError('zero count exceeds valid coverage')
        if name in ('flat.cog.tif','plane.cog.tif','saddle.cog.tif','filled.cog.tif') and (fact['valid_pixels'],fact['zero_pixels'])!=(expected,expected):raise SuiteError('analytic valid-zero count differs')
        if name in ('invalid.cog.tif','sparse.cog.tif') and (fact['valid_pixels'] or fact['zero_pixels']):raise SuiteError('empty complete neighbourhood must stay NoData')
        if fact.get('has_overviews') is not True or type(fact.get('overview_count')) is not int or fact['overview_count']!=3:raise SuiteError('exact overview evidence differs')
        evidence=obj(archive[name],'COG archive receipt')
        if evidence!={'path':'tpi-cogs/'+name,'size_bytes':fact['size_bytes'],'sha256':fact['sha256']}:raise SuiteError('original COG archive byte identity differs')


def expected_lineage(source,target):
    expected={'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.source','locator':source}],
        'outputs':[{'port':'output.save','locator':target,'write_mode':'create'}],
        'operations':[{'kind':'derive','operator':'raster_save','input_ports':['input.source'],'output_ports':['output.save']}]}
    return expected


def validate_lineage(facts,source,target):
    lineage_observer.validate(facts,expected_lineage(source,target),public=False)


def _run_scenario(repository,env,client,progress,scope_hash,tenant,identity,physical_runner,diagnostic_runner,lineage_reader):
    timeout=float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    full_checks=0;physical_calls=0
    engine=positive(env['ADDP_ONLINE_TPI_NFS_ENGINE_ID'],'NFS engine')
    scopes={name:f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
    with progress.stage('source_scan'):
        source_scan=support.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'targets':list(scopes.values())})
        items={name:support.find_fixture_item(client,engine,name,'raster DEM topographic position index source') for name in fixture.SOURCES}
        sources={name:scopes[name]+f'&item_id={items[name]["id"]}' for name in fixture.SOURCES}
        before={name:client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload for name,item in items.items()}
    runtimes=array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'workflow engines')
    selected=[r for r in runtimes if r.get('engine_type')=='geopython_workflow' and r.get('connection_status')=='online']
    if len(selected)!=1:raise SuiteError('expected exactly one online product GeoPython Runtime')
    runtime=positive(selected[0].get('id'),'Runtime engine')
    parent=f'addp://engine/{engine}/path/outputs?type=directory'
    cases,names,previous,scans=[],[],{},[]
    for name in fixture.TARGETS:
        case_index=len(cases)+1
        expectation=fixture.artifact_expectation(name)
        target=f'addp://engine/{engine}/path/outputs/{name}?type=file'
        with progress.stage('execution',case_index):
            with progress.measure('submit_wait'):
                workflow=definition(sources,parent,name)
                identifier=raster.submit(client,runtime,workflow)
                execution=raster.wait_execution(client,'develop',identifier,timeout)
            inputs=case_inputs(name)
            with progress.measure('internal_lineage'):
                evidence=lineage_observer.collect(env,SUITE,identifier,name,expected_lineage(sources[inputs[0]],target),execution,
                                                   tenant,int(identity['principal_id']),workflow,lineage_reader)
            facts,scan=raster.validate_success(execution,[sources[n] for n in inputs],target,'create',expectation,resource_type='file')
            lineage_observer.validate(facts,expected_lineage(sources[inputs[0]],target),public=True)
            if scan in scans:raise SuiteError('automatic output scans must be distinct')
            scans.append(scan)
        with progress.stage('automatic_scan',case_index):
            scanned=raster.wait_execution(client,'meta',scan,timeout)
            scan_identity=validate_scan(scanned,scan,identifier,tenant,int(identity['principal_id']),engine)
            monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'Monitor execution')
            if monitor.get('status')!='success' or monitor.get('module')!='develop' or obj(monitor.get('metadata'),'Monitor metadata').get('lineage_facts')!=facts:
                raise SuiteError('Monitor status, owner or lineage differs')
        with progress.stage('physical',case_index):
            names.append(name)
            with progress.measure('physical_verify_archive'):
                native=physical_runner(repository,env,names);check_physical(native,previous,scope_hash);previous=native['targets'].copy()
            progress.physical_profile(native);full_checks+=len(names);physical_calls+=1
            with progress.measure('metadata'):
                target_item=support.find_fixture_item(client,engine,'outputs/'+name,'raster COG')
                item=obj(client.request('GET',f'/api/v1/meta/items/{target_item["id"]}',(200,)).payload,'target detail')
                raster.validate_target_item(item,expectation,native['targets'][name])
                artifact=obj(obj(execution['metadata'].get('result'),'result').get('final_result'),'artifact')
                sizes=raster.strict_sizes(artifact,native['targets'][name],item,name)
        cases.append({'name':name,'execution_id':identifier,'automatic_scan_execution_id':scan,'target_locator':target,
            'target_item_id':item['id'],'size_evidence':sizes,'physical':native['targets'][name],
            'lineage':[],'lineage_evidence':evidence,'scan_identity':scan_identity,'monitor_verified':True})
    # Preserve every exact relation check, but let the ordinary collector work
    # while the other independent success chains run, rather than one tick each.
    for case_index,case in enumerate(cases,1):
        with progress.stage('lineage',case_index):
            case['lineage']=[raster.wait_lineage(client,items[n]['id'],case['target_item_id'],case['execution_id'],timeout)
                             for n in case_inputs(case['name'])]
    before_scans=scan_ids(client)
    if before_scans!=sorted([source_scan,*scans]):raise SuiteError('unexpected source or output scan')
    failures=[]
    old_edges={name:source_edges(client,item['id']) for name,item in items.items()}
    for problem in FAILURES:
        for language in ('zh-cn','en'):
            with progress.stage('negative',len(failures)+1):
                with progress.measure('submit_wait'):
                    identifier=raster.submit(SubmittedLanguageClient(client,language),runtime,definition(sources,parent,'tpi.cog.tif',invalid=problem,language=language))
                    failed=raster.wait_execution(client,'develop',identifier,timeout,expected='failed')
                if obj(failed.get('error_details'),'failed error').get('category')!='execution_failed':
                    raise SuiteError('DEM topographic position index failure has the wrong error category')
                with progress.measure('diagnostic'):
                    diagnostic=diagnostic_runner(repository,env,identifier,problem,language,timeout)
                with progress.measure('monitor_query'):
                    observed=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'failed Monitor execution')
                    if (observed.get('status')!='failed' or observed.get('module')!='develop'
                        or obj(observed.get('error_details'),'Monitor failure').get('category')!='execution_failed'
                        or obj(observed.get('metadata'),'failed Monitor metadata').get('lineage_facts',{}).get('outputs')):
                        raise SuiteError('Monitor failed status/category or absence of output lineage differs')
                    opposite=SubmittedLanguageClient(client,'en' if language=='zh-cn' else 'zh-cn')
                    queried=raster.wait_execution(opposite,'develop',identifier,timeout,expected='failed')
                    if queried.get('error')!=failed.get('error') or queried.get('error_details')!=failed.get('error_details'):
                        raise SuiteError('query language rewrote stored failure')
                with progress.measure('absence_checks'):
                    failed_result=obj(failed.get('metadata'),'failed metadata').get('result')
                    if failed_result is not None:failed_result=obj(failed_result,'failed result')
                    if failed.get('outputs') or (failed_result is not None and failed_result.get('meta_scan_runs')):
                        raise SuiteError('invalid parameter published a target or automatic scan')
                    if scan_ids(client)!=before_scans:raise SuiteError('invalid parameter added a scan')
                    if any(source_edges(client,item['id'])!=old_edges[name] for name,item in items.items()):
                        raise SuiteError('invalid parameter added derived lineage')
                    if failed.get('metadata',{}).get('lineage_facts',{}).get('outputs'):
                        raise SuiteError('invalid parameter published output lineage')
                with progress.measure('physical_verify_archive'):
                    native=physical_runner(repository,env,names);check_physical(native,previous,scope_hash)
                progress.physical_profile(native);full_checks+=len(names);physical_calls+=1
                failures.append({'problem':problem,'language':language,'execution_id':identifier,'status':'failed','no_publication_or_scan_or_lineage':True,'monitor_verified':True,'query_language_did_not_rewrite_failure':True,'diagnostic':diagnostic})
    with progress.stage('source_metadata'):
        for name,item in items.items():
            if client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload!=before[name]:
                raise SuiteError('source metadata changed')
        with progress.measure('archive_receipts'):lineage_observer.verify_archive(env,SUITE,cases)
    if full_checks!=429 or physical_calls!=39:raise SuiteError('all 429 full target checks are required')
    return {'schema_version':'addp.'+SUITE+'/v1','suite':SUITE,'result':'passed','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity':identity,'sources':sources,'source_scan_execution_id':source_scan,'source_unchanged':True,
        'cases':cases,'failed_parameters':failures,'full_target_checks':full_checks,'physical_verification_calls':physical_calls,
        'physical':native,'archived_cogs':native['archived_cogs'],'cleanup_boundary':'owned NFS export/directory and disposable deployment; no business DELETE acceptance'}


def run_scenario(repository,env,client,physical_runner=physical,diagnostic_runner=invalid_diagnostic,lineage_reader=lineage_observer.read_current,oracle_runner=fixture_call):
    # Both first construction and cleanup run inside the unchanged business child budget.
    env=dict(env);progress=TpiProgress(env,SUITE);primary=None;cleanup_error=None;report=None;prepared=None
    try:
        with progress.stage('identity'):
            tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
            if tenant<=1:raise SuiteError('raster DEM topographic position index acceptance forbids the default tenant')
            identity=raster.validate_identity(client,tenant,required_permissions=raster.EDITOR_PERMISSIONS)
        with progress.stage('oracle_prepare'):
            prepared=obj(oracle_runner(repository,env,'prepare-oracle'),'oracle preparation')
            for key in ('scope_sha256','anchor_sha256'):
                if not isinstance(prepared.get(key),str)or not re.fullmatch('[0-9a-f]{64}',prepared[key]):raise SuiteError('invalid frozen oracle identity')
            if type(prepared.get('expected_array_count'))is not int or prepared['expected_array_count']!=13:raise SuiteError('oracle requires thirteen independent arrays')
            if type(prepared.get('cache_bytes'))is not int or not 13*fixture.WIDTH*fixture.HEIGHT*8<prepared['cache_bytes']<13*(fixture.WIDTH*fixture.HEIGHT*8+1024):raise SuiteError('oracle byte bound differs')
            build=obj(prepared.get('timing_ms'),'oracle prepare timings')
            if set(build)!={'expected_build_ms','cache_write_ms'}or any(type(v)is not int or not 0<=v<=600000 for v in build.values()):raise SuiteError('oracle preparation timings differ')
            env['ADDP_ONLINE_TPI_ORACLE_ANCHOR_SHA256']=prepared['anchor_sha256']
        report=_run_scenario(repository,env,client,progress,prepared['scope_sha256'],tenant,identity,physical_runner,diagnostic_runner,lineage_reader)
    except BaseException as error:primary=error
    finally:
        try:
            with progress.stage('oracle_cleanup'):
                removed=obj(oracle_runner(repository,env,'cleanup-oracle'),'oracle cleanup')
                if removed.get('oracle_cache_removed')is not True:raise SuiteError('oracle cleanup proof missing')
        except BaseException as error:cleanup_error=error
    if primary is not None:
        if cleanup_error is not None:
            detail=str(primary)if isinstance(primary,SuiteError)else type(primary).__name__
            raise SuiteError(detail+'; oracle cleanup also failed: '+type(cleanup_error).__name__)from primary
        raise primary
    if cleanup_error is not None:raise cleanup_error
    report['oracle_cache']={'preparation':prepared,'removed':True,'verification_calls':report['physical_verification_calls'],'full_target_checks':report['full_target_checks']}
    report['stage_progress']=progress.finish();return report


def main():
    env=dict(os.environ)
    report=run_scenario(Path(__file__).resolve().parents[2],env,
        DiagnosticClient(env['GATEWAY_URL'],env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30,env['ADDP_ONLINE_ARTIFACT_DIR']))
    if len(report['cases'])!=13 or len(report['failed_parameters'])!=26 or len(report['archived_cogs'])!=13:raise SuiteError('incomplete fixed TPI matrix')
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'tpi-persistence.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))


if __name__=='__main__':
    try:main()
    except Exception as error:
        print(f'Raster DEM topographic position index persistence acceptance failed: {error if isinstance(error,SuiteError) else type(error).__name__}',file=sys.stderr)
        raise SystemExit(1)
