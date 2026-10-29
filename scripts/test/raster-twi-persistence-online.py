#!/usr/bin/env python3
"""Registered raster -> raster DEM terrain twi -> COG -> exact automatic scan and current lineage."""
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
fixture=importlib.import_module('business.scripts.online-raster-twi-fixture')
scan_ids=importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
validate_scan=importlib.import_module('scripts.test.raster-nibble-capacity-online').validate_scan
source_edges=importlib.import_module('scripts.test.raster-disk-pressure-online').source_edges
support=raster.support
obj,array,positive,SuiteError=raster.obj,raster.array,raster.positive,raster.SuiteError
SUITE=fixture.SUITE
lineage_observer=importlib.import_module('scripts.utils.online_raster_lineage')
class TwiProgress(importlib.import_module('scripts.test.raster-profile-curvature-persistence-online').CurvatureProgress):
    SUITES={SUITE:'twi-progress.json'}
    STAGES=importlib.import_module('scripts.test.raster-profile-curvature-persistence-online').CurvatureProgress.STAGES|{'identity','oracle_prepare','oracle_cleanup'}
    MEASURES={'submit_wait','internal_lineage','diagnostic','monitor_query','absence_checks','physical_verify_archive','metadata','archive_receipts'}

    @contextmanager
    def stage(self,name,index=0):
        # TWI owns 14 successes and 52 negatives; do not widen older suites.
        maximum=52 if name=='negative'else 14 if name in('execution','automatic_scan','physical','lineage')else 0
        minimum=1 if maximum else 0
        if name not in self.STAGES or type(index)is not int or not minimum<=index<=maximum or len(self.stages)>=113:
            raise SuiteError('unknown exact TWI progress stage')
        start=time.monotonic();entry={'stage':name,'case_index':index,'status':'running','started_ms':round((start-self.started)*1000)}
        self.stages.append(entry);self.flush()
        try:yield
        except BaseException:
            entry.update(status='failed',duration_ms=round((time.monotonic()-start)*1000));self.result='failed';self.flush();raise
        else:
            entry.update(status='passed',duration_ms=round((time.monotonic()-start)*1000));self.flush()

    @contextmanager
    def measure(self,name):
        if name not in self.MEASURES or not self.stages or self.stages[-1]['status']!='running':raise SuiteError('unknown TWI measurement')
        start=time.monotonic()
        try:yield
        finally:
            values=self.stages[-1].setdefault('substage_ms',{});values[name]=values.get(name,0)+round((time.monotonic()-start)*1000)

    def physical_profile(self,native):
        self.stages[-1]['physical_profile_ms']={'worker_total_ms':native['worker_total_ms'],'worker':native['verification_timing_ms'],'host':native['host_timing_ms']}

    def snapshot(self):
        value=super().snapshot();value['schema_version']='addp.raster-twi-progress/v1';return value


class DiagnosticClient(rasterize.DiagnosticClient):
    diagnostic_name='twi-submission-error.json'
    diagnostic_schema='addp.raster-twi-submission-diagnostic/v1'


FAILURES=('area_band','slope_band','area_unit_missing','area_unit_wrong','slope_unit','slope_unit_conflict',
          'zero_area','negative_area','negative_slope','angle90','size','origin','crs','rectangular','rotated','south_up','geographic','no_crs','no_grid',
          'area_alpha','slope_alpha','complex','area_scale','area_offset','slope_scale','slope_offset')
SubmittedLanguageClient=importlib.import_module('scripts.test.raster-disk-pressure-online').SubmittedLanguageClient


def case_inputs(name):return [n for n in fixture.CASES[name][:2]if n]


def source_bindings(sources,name):
    area,slope,_,stage=fixture.CASES[name]
    return {'input.source':sources[area]}if stage in('dem','fill')else{'input.area':sources[area],'input.slope':sources[slope]}


def definition(sources,parent,name,*,invalid=None,language='zh-cn'):
    if language not in('zh-cn','en')or name not in fixture.TARGETS or invalid not in(None,*FAILURES)or(invalid and name!='degree.cog.tif'):raise SuiteError('unknown exact TWI case')
    a,s,unit,stage=fixture.CASES[name];area_band=slope_band=2
    if invalid:
        a={'area_unit_missing':'area-no-unit.tif','area_unit_wrong':'area-wrong-unit.tif','zero_area':'area-zero.tif','negative_area':'area-negative.tif',
           'rectangular':'area-rectangular.tif','rotated':'area-rotated.tif','south_up':'area-south-up.tif','geographic':'area-geographic.tif','no_crs':'area-no-crs.tif','no_grid':'area-no-grid.tif','complex':'area-complex.tif','area_scale':'area-scale.tif','area_offset':'area-offset.tif'}.get(invalid,a)
        s={'slope_unit_conflict':'slope-conflict.tif','negative_slope':'slope-negative.tif','angle90':'slope-90.tif','size':'slope-size.tif','origin':'slope-origin.tif','crs':'slope-crs.tif','slope_scale':'slope-scale.tif','slope_offset':'slope-offset.tif'}.get(invalid,s)
        if invalid=='area_band':area_band=99
        if invalid=='slope_band':slope_band=99
        if invalid=='area_alpha':area_band=4
        if invalid=='slope_alpha':slope_band=4
        if invalid=='slope_unit':unit='radian'
    if stage in('dem','fill'):
        tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'locator':sources[a]}}];prior='source'
        if stage=='fill':
            tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':2,'elevation_unit':'metre'}});prior='fill';area_band=1
        tasks.extend([
            {'id':'slope','operator':'raster_slope','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':area_band,'elevation_unit':'metre','slope_unit':unit}},
            {'id':'direction','operator':'raster_flow_direction_d8','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':area_band,'elevation_unit':'metre'}},
            {'id':'area','operator':'raster_flow_accumulation_d8','depends_on':['direction'],'params':{'input_raster':{'$ref':'direction'},'band':1,'measure':'area'}}])
        area_band=slope_band=1;area_node='area';slope_node='slope'
    else:
        tasks=[{'id':node,'operator':'raster_load','depends_on':[],'params':{'locator':sources[file]}}for node,file in(('area',a),('slope',s))]
        area_node='area';slope_node='slope'
        if stage=='reclassify':
            tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':['slope'],'params':{'input_raster':{'$ref':'slope'},'band':2,'rules':[{'value':0,'class':0},{'value':1,'class':100},{'value':2,'class':200}]}})
            slope_node='classes';slope_band=1
    tasks.extend([{'id':'twi','operator':'raster_twi','depends_on':[area_node,slope_node],'params':{'input_raster':{'$ref':area_node},'slope_raster':{'$ref':slope_node},'band':area_band,'slope_band':slope_band,'slope_unit':unit}},
        {'id':'save','operator':'raster_save','depends_on':['twi'],'params':{'input_raster':{'$ref':'twi'},'target_parent_locator':parent,'target_name':f'failed-{invalid}-{language}.cog.tif'if invalid else name,'write_mode':'create','profile':'cog','blocksize':128,'overview_resampling':'nearest'}}])
    return {'tasks':tasks}


MESSAGES={'area_band': ('地形湿度指数必须显式选择有效的一基面积数据波段', 'Topographic wetness index requires an explicit valid one-based area data band'), 'slope_band': ('地形湿度指数必须显式选择有效的一基坡度数据波段', 'Topographic wetness index requires an explicit valid one-based slope data band'), 'area_unit_missing': ('地形湿度指数面积波段必须声明m2单位，请显式提供计入自身的平方米汇流面积', 'Topographic wetness index area band must declare m2; explicitly supply contributing area including self in square metres'), 'area_unit_wrong': ('地形湿度指数面积波段必须声明m2单位，请显式提供计入自身的平方米汇流面积', 'Topographic wetness index area band must declare m2; explicitly supply contributing area including self in square metres'), 'slope_unit': ('地形湿度指数必须显式声明degree或percent坡度单位', 'Topographic wetness index requires explicit slope_unit degree or percent'), 'slope_unit_conflict': ('地形湿度指数坡度波段单位未知或与声明冲突', 'Topographic wetness index slope band unit is unknown or conflicts with slope_unit'), 'zero_area': ('地形湿度指数的有效汇流面积必须严格大于零', 'Topographic wetness index valid contributing area must be strictly positive'), 'negative_area': ('地形湿度指数的有效汇流面积必须严格大于零', 'Topographic wetness index valid contributing area must be strictly positive'), 'negative_slope': ('地形湿度指数的有效坡度不得为负，角度必须小于90度', 'Topographic wetness index valid slope must be nonnegative and degrees must be below 90'), 'angle90': ('地形湿度指数的有效坡度不得为负，角度必须小于90度', 'Topographic wetness index valid slope must be nonnegative and degrees must be below 90'), 'size': ('地形湿度指数的面积与坡度必须具有相同CRS、尺寸及完整网格，请先显式对齐', 'Topographic wetness index area and slope must have the same CRS, dimensions and complete grid; align explicitly first'), 'origin': ('地形湿度指数的面积与坡度必须具有相同CRS、尺寸及完整网格，请先显式对齐', 'Topographic wetness index area and slope must have the same CRS, dimensions and complete grid; align explicitly first'), 'crs': ('地形湿度指数的面积与坡度必须具有相同CRS、尺寸及完整网格，请先显式对齐', 'Topographic wetness index area and slope must have the same CRS, dimensions and complete grid; align explicitly first'), 'rectangular': ('地形湿度指数需要已知线性单位的二维投影、北向无旋转方形网格', 'Topographic wetness index requires a projected north-up square grid with known linear units'), 'rotated': ('地形湿度指数需要已知线性单位的二维投影、北向无旋转方形网格', 'Topographic wetness index requires a projected north-up square grid with known linear units'), 'south_up': ('地形湿度指数需要已知线性单位的二维投影、北向无旋转方形网格', 'Topographic wetness index requires a projected north-up square grid with known linear units'), 'geographic': ('地形湿度指数需要已知线性单位的二维投影、北向无旋转方形网格', 'Topographic wetness index requires a projected north-up square grid with known linear units'), 'no_crs': ('地形湿度指数需要已知线性单位的二维投影、北向无旋转方形网格', 'Topographic wetness index requires a projected north-up square grid with known linear units'), 'no_grid': ('地形湿度指数需要已知线性单位的二维投影、北向无旋转方形网格', 'Topographic wetness index requires a projected north-up square grid with known linear units'), 'area_alpha': ('地形湿度指数不能选择alpha作为数据波段', 'Topographic wetness index cannot use an alpha band as data'), 'slope_alpha': ('地形湿度指数不能选择alpha作为数据波段', 'Topographic wetness index cannot use an alpha band as data'), 'complex': ('地形湿度指数不支持复数数据或复数alpha波段', 'Topographic wetness index does not support complex data or alpha bands'), 'area_scale': ('地形湿度指数前必须显式应用面积及坡度scale/offset', 'Topographic wetness index requires area and slope scale/offset to be applied first'), 'area_offset': ('地形湿度指数前必须显式应用面积及坡度scale/offset', 'Topographic wetness index requires area and slope scale/offset to be applied first'), 'slope_scale': ('地形湿度指数前必须显式应用面积及坡度scale/offset', 'Topographic wetness index requires area and slope scale/offset to be applied first'), 'slope_offset': ('地形湿度指数前必须显式应用面积及坡度scale/offset', 'Topographic wetness index requires area and slope scale/offset to be applied first')}


def invalid_diagnostic(repository,env,identifier,problem,language,timeout):
    # Public safe projection hides computation causes. The exact owned execution
    # log must contain the submitted-language cause and the correct task stage.
    if problem not in MESSAGES or language not in ('zh-cn','en'):raise SuiteError('unknown twi diagnostic')
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
                    and message.endswith('任务 twi 执行失败: '+cause)):
                    return {'execution_id':identifier,'task':'twi','cause':'invalid_'+problem,
                            'language':language,'source':'develop_runtime_log','exact_cause':cause}
        time.sleep(1)
    raise SuiteError('missing exact twi task and submitted-language diagnostic')


def fixture_call(repository,env,action,names=()):
    if action not in ('verify','prepare-oracle','cleanup-oracle'):raise SuiteError('unknown exact oracle action')
    result=subprocess.run([sys.executable,'business/scripts/online-raster-twi-fixture.py',action,*names],
        cwd=repository,env=dict(env),text=True,capture_output=True,timeout=150)
    if result.returncode:raise SuiteError('independent raster DEM terrain twi physical verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout),'raster DEM terrain twi physical evidence')


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
        if name not in fixture.TARGETS:raise SuiteError('unknown exact TWI target')
        for key in ('full_pixel_and_mask_verification','cog_valid','independent_decimal_oracle_verified','joint_coverage_verified','fixed_area_over_width_verified','dimensionless_unit_verified','nearest_overviews_verified','no_slope_threshold_verified','legal_signed_zero_verified'):
            if fact.get(key)is not True:raise SuiteError('strict TWI physical evidence missing: '+key)
        if type(fact.get('size_bytes'))is not int or fact['size_bytes']<=0 or not isinstance(fact.get('sha256'),str)or not re.fullmatch('[0-9a-f]{64}',fact['sha256']):raise SuiteError('invalid physical COG size/hash')
        for key in ('valid_pixels','zero_pixels','negative_pixels'):
            if type(fact.get(key))is not int or not 0<=fact[key]<=fixture.WIDTH*fixture.HEIGHT:raise SuiteError('invalid TWI semantic count')
        if fact['zero_pixels']+fact['negative_pixels']>fact['valid_pixels']:raise SuiteError('signed counts exceed valid coverage')
        if name in('filled.cog.tif','invalid.cog.tif','disjoint.cog.tif','zero-slope.cog.tif')and fact['valid_pixels']:raise SuiteError('empty TWI must stay NoData')
        if name=='tiny.cog.tif'and fact['valid_pixels']==0:raise SuiteError('small nonzero slope was discarded')
        if name in('degree.cog.tif','percent.cog.tif')and(not fact['zero_pixels']or not fact['negative_pixels']):raise SuiteError('signed index and legal zero must be present')
        if fact.get('has_overviews')is not True or type(fact.get('overview_count'))is not int or fact['overview_count']!=3:raise SuiteError('exact overview evidence differs')
        receipt=obj(archive[name],'original COG archive receipt')
        if type(receipt.get('size_bytes'))is not int or receipt!={'path':'twi-cogs/'+name,'size_bytes':fact['size_bytes'],'sha256':fact['sha256']}:raise SuiteError('original COG archive byte identity differs')


def expected_lineage(bindings,target):
    if set(bindings)not in ({'input.source'},{'input.area','input.slope'})or any(not isinstance(v,str)or not v for v in bindings.values())or not isinstance(target,str)or not target:raise SuiteError('TWI requires exact role-bound source locators and one target')
    return {'schema_version':'addp.lineage-facts/v1','inputs':[{'port':p,'locator':bindings[p]}for p in sorted(bindings)],
        'outputs':[{'port':'output.save','locator':target,'write_mode':'create'}],
        'operations':[{'kind':'derive','operator':'raster_save','input_ports':sorted(bindings),'output_ports':['output.save']}]}


def validate_lineage(facts,bindings,target):
    lineage_observer.validate(facts,expected_lineage(bindings,target),public=False)


def _run_scenario(repository,env,client,progress,scope_hash,tenant,identity,physical_runner,diagnostic_runner,lineage_reader):
    timeout=float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    full_checks=0;physical_calls=0
    engine=positive(env['ADDP_ONLINE_TWI_NFS_ENGINE_ID'],'NFS engine')
    scopes={name:f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
    with progress.stage('source_scan'):
        source_scan=support.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'targets':list(scopes.values())})
        items={name:support.find_fixture_item(client,engine,name,'raster DEM terrain twi source') for name in fixture.SOURCES}
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
                evidence=lineage_observer.collect(env,SUITE,identifier,name,expected_lineage(source_bindings(sources,name),target),execution,
                                                   tenant,int(identity['principal_id']),workflow,lineage_reader)
            facts,scan=raster.validate_success(execution,[sources[n] for n in inputs],target,'create',expectation,resource_type='file')
            lineage_observer.validate(facts,expected_lineage(source_bindings(sources,name),target),public=True)
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
                    identifier=raster.submit(SubmittedLanguageClient(client,language),runtime,definition(sources,parent,'degree.cog.tif',invalid=problem,language=language))
                    failed=raster.wait_execution(client,'develop',identifier,timeout,expected='failed')
                if obj(failed.get('error_details'),'failed error').get('category')!='execution_failed':
                    raise SuiteError('DEM terrain twi failure has the wrong error category')
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
    if full_checks!=833 or physical_calls!=66:raise SuiteError('all 66 calls and 833 full target checks are required')
    return {'schema_version':'addp.'+SUITE+'/v1','suite':SUITE,'result':'passed','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity':identity,'sources':sources,'source_scan_execution_id':source_scan,'source_unchanged':True,
        'cases':cases,'failed_parameters':failures,'full_target_checks':full_checks,'physical_verification_calls':physical_calls,
        'physical':native,'archived_cogs':native['archived_cogs'],'cleanup_boundary':'owned NFS export/directory and disposable deployment; no business DELETE acceptance'}


def run_scenario(repository,env,client,physical_runner=physical,diagnostic_runner=invalid_diagnostic,lineage_reader=lineage_observer.read_current,oracle_runner=fixture_call):
    # Both first construction and cleanup run inside the unchanged business child budget.
    env=dict(env);progress=TwiProgress(env,SUITE);primary=None;cleanup_error=None;report=None;prepared=None
    try:
        with progress.stage('identity'):
            tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
            if tenant<=1:raise SuiteError('raster DEM terrain twi acceptance forbids the default tenant')
            identity=raster.validate_identity(client,tenant,required_permissions=raster.EDITOR_PERMISSIONS)
        with progress.stage('oracle_prepare'):
            prepared=obj(oracle_runner(repository,env,'prepare-oracle'),'oracle preparation')
            for key in ('scope_sha256','anchor_sha256'):
                if not isinstance(prepared.get(key),str)or not re.fullmatch('[0-9a-f]{64}',prepared[key]):raise SuiteError('invalid frozen oracle identity')
            if type(prepared.get('expected_array_count'))is not int or prepared['expected_array_count']!=14:raise SuiteError('oracle requires fourteen independent arrays')
            if type(prepared.get('cache_bytes'))is not int or not 14*fixture.WIDTH*fixture.HEIGHT*8<prepared['cache_bytes']<14*(fixture.WIDTH*fixture.HEIGHT*8+1024):raise SuiteError('oracle byte bound differs')
            build=obj(prepared.get('timing_ms'),'oracle prepare timings')
            if set(build)!={'expected_build_ms','cache_write_ms'}or any(type(v)is not int or not 0<=v<=600000 for v in build.values()):raise SuiteError('oracle preparation timings differ')
            env['ADDP_ONLINE_TWI_ORACLE_ANCHOR_SHA256']=prepared['anchor_sha256']
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
    if len(report['cases'])!=14 or len(report['failed_parameters'])!=52 or len(report['archived_cogs'])!=14:raise SuiteError('incomplete fixed TWI matrix')
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'twi-persistence.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))


if __name__=='__main__':
    try:main()
    except Exception as error:
        print(f'Raster DEM terrain twi persistence acceptance failed: {error if isinstance(error,SuiteError) else type(error).__name__}',file=sys.stderr)
        raise SystemExit(1)
