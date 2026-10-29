#!/usr/bin/env python3
"""Two registered DEMs -> complete CSV -> exact scans/current-input lineage + raw CSV archive."""
from __future__ import annotations
import base64,hashlib,importlib,json,math,os,re,subprocess,sys,time
from pathlib import Path
raster=importlib.import_module('scripts.test.raster-workflow-online')
rasterize=importlib.import_module('scripts.test.vector-rasterize-persistence-online')
fixture=importlib.import_module('business.scripts.online-raster-surface-area-fixture')
scan_ids=importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
validate_scan=importlib.import_module('scripts.test.raster-nibble-capacity-online').validate_scan
source_edges=importlib.import_module('scripts.test.raster-disk-pressure-online').source_edges
SubmittedLanguageClient=importlib.import_module('scripts.test.raster-disk-pressure-online').SubmittedLanguageClient
support=raster.support
obj,array,positive,SuiteError=raster.obj,raster.array,raster.positive,raster.SuiteError
SUITE=fixture.SUITE
lineage_observer=importlib.import_module('scripts.utils.online_raster_lineage')

class DiagnosticClient(rasterize.DiagnosticClient):
    diagnostic_name='surface-area-submission-error.json'
    diagnostic_schema='addp.raster-surface-area-submission-diagnostic/v1'

FAILURES=('band','alpha','complex','complex_alpha','rotated','south_up','geographic','no_crs','no_grid',
          'unit','unit_conflict','unit_unknown','scale','offset','all_invalid','no_quad','width_one','height_one')


def case_inputs(name):return [fixture.CASES[name][0]]


def definition(sources,parent,name,*,invalid=None,language='zh-cn'):
    if name not in fixture.TARGETS or language not in ('zh-cn','en')or invalid not in (None,*FAILURES)or invalid and name!='mixed.csv':raise SuiteError('unknown exact surface area case')
    source,unit,stage=fixture.CASES[name];band=2
    if invalid:
        source={'complex':'complex.tif','complex_alpha':'complex-alpha.tif','rotated':'rotated.tif','south_up':'south-up.tif','geographic':'geographic.tif','no_crs':'no-crs.tif','no_grid':'no-grid.tif','unit_unknown':'unknown-unit.tif','scale':'scaled.tif','offset':'offset.tif','all_invalid':'invalid.tif','no_quad':'sparse.tif','width_one':'width-one.tif','height_one':'height-one.tif'}.get(invalid,source)
        if invalid=='band':band=99
        if invalid=='alpha':band=4
        if invalid=='unit':unit='yard'
        if invalid=='unit_conflict':unit='foot'
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'locator':sources[source]}}];prior='source'
    if stage=='fill':
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':band,'elevation_unit':unit}});prior='fill';band=1
    if stage=='reclassify':
        tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':band,'rules':[{'min':None,'max':0,'class':-2},{'min':0,'max':None,'class':3}]}});prior='classes';band=1
    tasks.extend([{'id':'surface_area','operator':'raster_surface_area','depends_on':[prior],'params':{'input_raster':{'$ref':prior},'band':band,'elevation_unit':unit}},
                  {'id':'save','operator':'save','depends_on':['surface_area'],'params':{'input_df':{'$ref':'surface_area'},'target_parent_locator':parent,'target_name':f'failed-{invalid}-{language}.csv'if invalid else name,'mode':'fail'}}])
    return {'tasks':tasks}


MESSAGES={
 'band':('DEM 三维表面积统计必须显式选择有效的一基高程数据波段','DEM surface area statistics requires an explicit valid one-based elevation band'),
 'alpha':('DEM 三维表面积统计不能选择 alpha 波段作为高程','DEM surface area statistics cannot use an alpha band as elevation'),
 'complex':('DEM 三维表面积统计不支持复数高程或复数 alpha 波段','DEM surface area statistics does not support complex elevation or alpha bands'),
 'unit':('DEM 三维表面积统计必须明确声明 metre、foot 或 us_survey_foot 高程单位','DEM surface area statistics requires elevation_unit metre, foot or us_survey_foot'),
}
MESSAGES['complex_alpha']=MESSAGES['complex']
for key in ('unit_conflict','unit_unknown'):MESSAGES[key]=('DEM 三维表面积统计高程波段单位未知或与声明单位冲突','DEM surface area statistics band unit is unknown or conflicts with elevation_unit')
for key in ('rotated','south_up','geographic','no_crs','no_grid'):MESSAGES[key]=('DEM 三维表面积统计需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影','DEM surface area statistics requires a projected north-up grid with known linear units; reproject explicitly')
for key in ('scale','offset'):MESSAGES[key]=('DEM 三维表面积统计前必须显式应用高程 scale/offset','DEM surface area statistics requires elevation scale/offset to be applied first')
for key in ('all_invalid','no_quad','width_one','height_one'):MESSAGES[key]=('DEM 三维表面积统计没有四角完整有效的面片，不能生成零面积结果','DEM surface area statistics has no complete valid quadrilaterals; a zero-area result cannot be produced')

def invalid_diagnostic(repository,env,identifier,problem,language,timeout):
    # Public safe projection hides computation causes. The exact owned execution
    # log must contain the submitted-language cause and the correct task stage.
    if problem not in MESSAGES or language not in ('zh-cn','en'):raise SuiteError('unknown surface area diagnostic')
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
                    and message.endswith('任务 surface_area 执行失败: '+cause)):
                    return {'execution_id':identifier,'task':'surface_area','cause':'invalid_'+problem,
                            'language':language,'source':'develop_runtime_log','exact_cause':cause}
        time.sleep(1)
    raise SuiteError('missing exact surface area task and submitted-language diagnostic')


def physical(repository,env,names):
    result=subprocess.run([sys.executable,'business/scripts/online-raster-surface-area-fixture.py','verify',*names],cwd=repository,env=dict(env),text=True,capture_output=True,timeout=150)
    if result.returncode:raise SuiteError('independent complete surface area CSV verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout),'surface area physical evidence')

def check_physical(native,previous):
    rasterize.check_physical(native,previous)
    if native.get('runtime_workspaces_removed') is not True:raise SuiteError('Runtime workspace cleanup proof missing')
    for name,fact in obj(native.get('targets'),'physical targets').items():
        if name not in fixture.TARGETS or fact.get('schema')!=list(fixture.COLUMNS) or type(fact.get('row_count')) is not int or fact['row_count']!=1:raise SuiteError('physical table schema differs')
        for key in ('complete_table_verified','independent_decimal_oracle_verified','count_and_area_conservation_verified','dual_diagonal_and_centre_domain_verified','source_measurement_geometry_verified'):
            if fact.get(key) is not True:raise SuiteError('strict physical CSV evidence missing: '+key)
        grid=fact.get('measurement_grid');factor=fact.get('horizontal_metres');spec=fixture.source_spec(fixture.CASES[name][0])
        if (grid!=list(spec['grid']) or isinstance(factor,bool) or not isinstance(factor,(int,float)) or not math.isfinite(factor)
            or factor<=0 or not math.isclose(factor,spec['horizontal'],rel_tol=1e-15,abs_tol=0)):
            raise SuiteError('source measurement grid or CRS units differ')
        try:payload=base64.b64decode(fact.get('csv_base64',''),validate=True);fixture.parse_csv(payload,name,fact)
        except (ValueError,fixture.FixtureError) as error:raise SuiteError('raw CSV bytes differ from complete oracle') from error
        if type(fact.get('size_bytes')) is not int or fact['size_bytes']<=0 or len(payload)!=fact['size_bytes'] or hashlib.sha256(payload).hexdigest()!=fact.get('sha256'):raise SuiteError('raw CSV digest or size differs')

def archive_csv(env,native):
    check_physical(native,{})
    directory=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'surface-area-csv'
    if directory.is_symlink():raise SuiteError('CSV archive directory must not be symlink')
    directory.mkdir(parents=True,exist_ok=True);names=set(native['targets'])
    if any(p.name not in names or p.is_symlink() or not p.is_file() for p in directory.iterdir()):raise SuiteError('unexpected archived CSV inventory')
    manifest_path=directory.parent/'surface-area-csv-manifest.json';previous={}
    if manifest_path.is_symlink():raise SuiteError('CSV manifest must not be symlink')
    existing={p.name for p in directory.iterdir()}
    if manifest_path.exists():
        previous=json.loads(manifest_path.read_text())
        if not isinstance(previous,dict)or set(previous)!=existing:raise SuiteError('previous CSV manifest inventory differs')
    elif existing:raise SuiteError('archived CSV has no original manifest')
    manifest={}
    for name,fact in native['targets'].items():
        if name not in fixture.TARGETS:raise SuiteError('unknown exact archived target')
        payload=base64.b64decode(fact['csv_base64'],validate=True);path=directory/name
        if path.exists():
            if path.read_bytes()!=payload:raise SuiteError('previous archived CSV changed')
        else:
            with path.open('xb') as stream:stream.write(payload)
        manifest[name]={'path':'surface-area-csv/'+name,'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}
        if manifest[name]['sha256']!=fact['sha256'] or manifest[name]['size_bytes']!=fact['size_bytes']:raise SuiteError('archived CSV byte identity differs')
    if any(name not in manifest or fact!=manifest[name]for name,fact in previous.items()):raise SuiteError('previous CSV manifest changed')
    path=directory.parent/'surface-area-csv-manifest.json'
    if path.is_symlink():raise SuiteError('CSV manifest must not be symlink')
    temporary=path.with_suffix('.tmp')
    if temporary.is_symlink():raise SuiteError('CSV manifest temporary path must not be symlink')
    temporary.write_text(json.dumps(manifest,sort_keys=True,indent=2)+'\n');temporary.replace(path)
    return manifest


def expected_lineage(bindings,target):
    if (set(bindings)!={'input.source'} or any(not isinstance(v,str) or not v for v in bindings.values())
        or not isinstance(target,str) or not target):
        raise SuiteError('surface area requires the exact current source locator and one target')
    return {'schema_version':'addp.lineage-facts/v1',
        'inputs':[{'port':p,'locator':bindings[p]} for p in sorted(bindings)],
        'outputs':[{'port':'output.save','locator':target,'write_mode':'create'}],
        'operations':[{'kind':'derive','operator':'save','input_ports':sorted(bindings),'output_ports':['output.save']}]}


def validate_success(execution,bindings,target):
    if obj(execution.get('outputs'),'outputs')!={'save':{'resource':{'locator':target,'type':'file','write_mode':'create'}}}:
        raise SuiteError('expected the exact named surface area CSV file output')
    metadata=obj(execution.get('metadata'),'metadata');facts=obj(metadata.get('lineage_facts'),'lineage')
    expected=expected_lineage(bindings,target)
    if set(facts)!=set(expected) or facts['schema_version']!=expected['schema_version']:
        raise SuiteError('surface area lineage envelope differs')
    for side in ('inputs','outputs'):
        actual={}
        for ref in array(facts[side],side):
            ref=obj(ref,'lineage resource');port=ref.get('port')
            if not isinstance(port,str) or not port or port in actual:raise SuiteError('duplicate or missing surface area lineage port')
            actual[port]=ref
        if actual!={ref['port']:ref for ref in expected[side]}:
            raise SuiteError('surface area role-bound source/target lineage differs')
    lineage_observer.validate(facts,expected,public=True)
    result=obj(metadata.get('result'),'result');scans=array(result.get('meta_scan_runs'),'scans')
    if (len(scans)!=1 or scans[0].get('status')!='submitted' or scans[0].get('target_locator')!=target
        or not isinstance(scans[0].get('execution_id'),str) or not scans[0]['execution_id']):
        raise SuiteError('expected one automatic exact surface area CSV scan')
    return facts,scans[0]['execution_id'],result


def record_lineage(env,identifier,name,bindings,target,execution,tenant,principal,workflow,reader):
    return lineage_observer.collect(env,SUITE,identifier,name,expected_lineage(bindings,target),execution,
                                    tenant,principal,workflow,reader)


def verify_lineage_archive(env,cases):
    lineage_observer.verify_archive(env,SUITE,cases)


def validate_preview(result,name,geometry=None):
    preview=obj(result,'table preview');schema=[{'name':n,'type':'int64' if i<3 else 'Float64'} for i,n in enumerate(fixture.COLUMNS)]
    if (preview.get('type')!='pandas_dataframe' or preview.get('schema')!=schema or type(preview.get('row_count')) is not int or preview['row_count']!=1
        or type(preview.get('preview_limit')) is not int or preview['preview_limit']!=50 or preview.get('truncated') is not False):raise SuiteError('table preview schema or bounds differ')
    rows=array(preview.get('preview_rows'),'preview rows')
    expected=obj(geometry.get('preview_serialized_row'),'serialized preview reference')if isinstance(geometry,dict)else None
    if expected is None or len(rows)!=1 or rows[0]!=expected:raise SuiteError('exact contracted serialized preview row differs')
    if set(expected)!=set(fixture.COLUMNS):raise SuiteError('serialized preview reference schema differs')
    for i,key in enumerate(fixture.COLUMNS):
        value=rows[0][key]
        if i<3:
            if type(value)is not int:raise SuiteError('preview counts must be exact integers')
        elif isinstance(value,bool)or not isinstance(value,(int,float))or not math.isfinite(value):raise SuiteError('preview values must be finite numbers')

def validate_item(item,native,name):
    size=item.get('size_bytes');physical_size=native.get('size_bytes')
    if type(size) is not int or size<=0 or type(physical_size) is not int or size!=physical_size or not item.get('scanned_at'):raise SuiteError('CSV physical/Meta sizes must be equal positive integers')
    descriptor=obj(obj(item.get('attributes'),'attributes').get('item'),'item facts')
    if item.get('item_type')!='file' or item.get('full_name')!='outputs/'+name or descriptor.get('format')!='csv':raise SuiteError('CSV item identity differs')
    return {'physical_size_bytes':physical_size,'meta_size_bytes':size}

def run_scenario(repository,env,client,physical_runner=physical,diagnostic_runner=invalid_diagnostic,lineage_reader=lineage_observer.read_current):
    timeout=float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('raster DEM surface area acceptance forbids the default tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=raster.EDITOR_PERMISSIONS)
    engine=positive(env['ADDP_ONLINE_SURFACE_AREA_NFS_ENGINE_ID'],'NFS engine')
    scopes={name:f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
    source_scan=support.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'targets':list(scopes.values())})
    items={name:support.find_fixture_item(client,engine,name,'raster DEM surface area source') for name in fixture.SOURCES}
    sources={name:scopes[name]+f'&item_id={items[name]["id"]}' for name in fixture.SOURCES}
    before={name:client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload for name,item in items.items()}
    runtimes=array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'workflow engines')
    selected=[r for r in runtimes if r.get('engine_type')=='geopython_workflow' and r.get('connection_status')=='online']
    if len(selected)!=1:raise SuiteError('expected exactly one online product GeoPython Runtime')
    runtime=positive(selected[0].get('id'),'Runtime engine')
    parent=f'addp://engine/{engine}/path/outputs?type=directory'
    cases,names,previous,scans=[],[],{},[]
    physical_calls=full_checks=0
    for name in fixture.TARGETS:
        target=f'addp://engine/{engine}/path/outputs/{name}?type=file'
        workflow=definition(sources,parent,name)
        identifier=raster.submit(client,runtime,workflow)
        execution=raster.wait_execution(client,'develop',identifier,timeout)
        inputs=case_inputs(name);bindings={'input.source':sources[inputs[0]]}
        evidence=record_lineage(env,identifier,name,bindings,target,execution,tenant,int(identity['principal_id']),workflow,lineage_reader)
        facts,scan,result=validate_success(execution,bindings,target)
        if scan in scans:raise SuiteError('automatic output scans must be distinct')
        scans.append(scan)
        scanned=raster.wait_execution(client,'meta',scan,timeout)
        scan_identity=validate_scan(scanned,scan,identifier,tenant,int(identity['principal_id']),engine)
        monitor=obj(client.request('GET',f'/api/v1/monitor/executions/by-execution-id/{identifier}',(200,)).payload,'Monitor execution')
        if monitor.get('status')!='success' or monitor.get('module')!='develop' or obj(monitor.get('metadata'),'Monitor metadata').get('lineage_facts')!=facts:
            raise SuiteError('Monitor status, owner or lineage differs')
        names.append(name);native=physical_runner(repository,env,names);check_physical(native,previous)
        physical_calls+=1;full_checks+=len(names)
        if set(native['targets'])!=set(names):raise SuiteError('physical CSV target inventory differs')
        validate_preview(result.get('final_result'),name,native['targets'][name]);archived=archive_csv(env,native);previous=native['targets'].copy()
        target_item=support.find_fixture_item(client,engine,'outputs/'+name,'statistics CSV')
        item=obj(client.request('GET',f'/api/v1/meta/items/{target_item["id"]}',(200,)).payload,'target detail')
        sizes=validate_item(item,native['targets'][name],name)
        lineage=[]
        cases.append({'name':name,'execution_id':identifier,'automatic_scan_execution_id':scan,'target_locator':target,
            'target_item_id':item['id'],'size_evidence':sizes,'physical':native['targets'][name],
            'lineage':lineage,'lineage_evidence':evidence,'scan_identity':scan_identity,'monitor_verified':True})
    for case in cases:
        case['lineage']=[raster.wait_lineage(client,items[n]['id'],case['target_item_id'],case['execution_id'],timeout) for n in case_inputs(case['name'])]
    before_scans=scan_ids(client)
    if before_scans!=sorted([source_scan,*scans]):raise SuiteError('unexpected source or output scan')
    failures=[]
    old_edges={name:source_edges(client,item['id']) for name,item in items.items()}
    for problem in FAILURES:
        for language in ('zh-cn','en'):
            identifier=raster.submit(SubmittedLanguageClient(client,language),runtime,definition(sources,parent,'mixed.csv',invalid=problem,language=language))
            failed=raster.wait_execution(client,'develop',identifier,timeout,expected='failed')
            if obj(failed.get('error_details'),'failed error').get('category')!='execution_failed':
                raise SuiteError('DEM surface area failure has the wrong error category')
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
            if set(native['targets'])!=set(names):raise SuiteError('negative physical CSV inventory differs')
            physical_calls+=1;full_checks+=len(names)
            failures.append({'problem':problem,'language':language,'execution_id':identifier,'status':'failed','no_publication_or_scan_or_lineage':True,'monitor_verified':True,'query_language_did_not_rewrite_failure':True,'diagnostic':diagnostic})
    for name,item in items.items():
        if client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload!=before[name]:
            raise SuiteError('source metadata changed')
    if physical_calls!=48 or full_checks!=510:raise SuiteError('all 48 calls and 510 full actual CSV checks are required')
    verify_lineage_archive(env,cases)
    archived=archive_csv(env,native)
    return {'schema_version':'addp.'+SUITE+'/v1','suite':SUITE,'result':'passed','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity':identity,'sources':sources,'source_scan_execution_id':source_scan,'source_unchanged':True,
        'cases':cases,'failed_parameters':failures,'physical_verification_calls':physical_calls,'full_target_checks':full_checks,
        'physical':native,'archived_csv':archived,'cleanup_boundary':'owned NFS export/directory and disposable deployment; no business DELETE acceptance'}


def main():
    env=dict(os.environ)
    report=run_scenario(Path(__file__).resolve().parents[2],env,DiagnosticClient(env['GATEWAY_URL'],env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30,env['ADDP_ONLINE_ARTIFACT_DIR']))
    if len(report['cases'])!=12 or len(report['failed_parameters'])!=36 or len(report['archived_csv'])!=12:raise SuiteError('incomplete fixed surface area matrix')
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'surface-area-persistence.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))

if __name__=='__main__':
    try:main()
    except Exception as error:
        print(f'Surface area persistence acceptance failed: {error if isinstance(error,SuiteError) else type(error).__name__}',file=sys.stderr);raise SystemExit(1)
