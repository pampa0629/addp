#!/usr/bin/env python3
"""Two registered DEMs -> complete CSV -> exact scans/current-input lineage + raw CSV archive."""
from __future__ import annotations
import base64,hashlib,importlib,json,math,os,re,subprocess,sys,time
from pathlib import Path
raster=importlib.import_module('scripts.test.raster-workflow-online')
rasterize=importlib.import_module('scripts.test.vector-rasterize-persistence-online')
fixture=importlib.import_module('business.scripts.online-raster-cut-fill-fixture')
scan_ids=importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
validate_scan=importlib.import_module('scripts.test.raster-nibble-capacity-online').validate_scan
source_edges=importlib.import_module('scripts.test.raster-disk-pressure-online').source_edges
SubmittedLanguageClient=importlib.import_module('scripts.test.raster-disk-pressure-online').SubmittedLanguageClient
support=raster.support
obj,array,positive,SuiteError=raster.obj,raster.array,raster.positive,raster.SuiteError
SUITE=fixture.SUITE
lineage_observer=importlib.import_module('scripts.utils.online_raster_lineage')

class DiagnosticClient(rasterize.DiagnosticClient):
    diagnostic_name='cut-fill-submission-error.json'
    diagnostic_schema='addp.raster-cut-fill-submission-diagnostic/v1'

FAILURES=('before_band','after_band','before_unit','after_unit_conflict','before_alpha','after_complex',
          'before_scale','after_offset','crs','size','transform','rotated','south_up','geographic','no_crs','no_grid','empty','disjoint')

def case_inputs(name):return list(fixture.CASES[name][:2])

def definition(sources,parent,name,*,invalid=None,language='zh-cn'):
    if name not in fixture.TARGETS or language not in ('zh-cn','en') or invalid not in (None,*FAILURES) or invalid and name!='mixed.csv':raise SuiteError('unknown exact cut/fill case')
    before,after,unit,after_unit,stage=fixture.CASES[name];band=after_band=2
    if invalid:
        before={'before_scale':'scaled.tif','south_up':'south-up.tif','no_crs':'no-crs.tif','empty':'empty.tif','disjoint':'left.tif'}.get(invalid,before)
        after={'after_complex':'complex.tif','after_offset':'offset.tif','crs':'other-crs.tif','size':'size.tif','transform':'shifted.tif','rotated':'rotated.tif','geographic':'geographic.tif','no_grid':'no-grid.tif','disjoint':'right.tif'}.get(invalid,after)
        if invalid=='before_band':band=99
        if invalid=='after_band':after_band=99
        if invalid=='before_unit':unit='yard'
        if invalid=='after_unit_conflict':after_unit='foot'
        if invalid=='before_alpha':band=4
    tasks=[{'id':key,'operator':'raster_load','depends_on':[],'params':{'locator':sources[source]}} for key,source in (('before',before),('after',after))]
    later='after'
    if stage=='fill':
        tasks.append({'id':'fill','operator':'raster_fill_depressions','depends_on':['after'],'params':{'input_raster':{'$ref':'after'},'band':2,'elevation_unit':after_unit}});later='fill';after_band=1
    if stage=='reclassify':
        tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':['after'],'params':{'input_raster':{'$ref':'after'},'band':2,'rules':[{'min':None,'max':0,'class':-2},{'min':0,'max':None,'class':3}]}});later='classes';after_band=1
    tasks.extend([{'id':'cut_fill','operator':'raster_cut_fill','depends_on':['before',later],
                   'params':{'input_raster':{'$ref':'before'},'after_raster':{'$ref':later},'band':band,'after_band':after_band,'elevation_unit':unit,'after_elevation_unit':after_unit}},
                  {'id':'save','operator':'save','depends_on':['cut_fill'],'params':{'input_df':{'$ref':'cut_fill'},'target_parent_locator':parent,'target_name':f'failed-{invalid}-{language}.csv' if invalid else name,'mode':'fail'}}])
    return {'tasks':tasks}

MESSAGES={'before_band': ('双DEM填挖方施工前必须显式选择有效的一基高程数据波段', 'DEM cut/fill before requires an explicit valid one-based elevation band'),
 'after_band': ('双DEM填挖方施工后必须显式选择有效的一基高程数据波段', 'DEM cut/fill after requires an explicit valid one-based elevation band'),
 'before_unit': ('双DEM填挖方施工前必须明确声明 metre、foot 或 us_survey_foot 高程单位', 'DEM cut/fill before requires elevation_unit metre, foot or us_survey_foot'),
 'after_unit_conflict': ('双DEM填挖方施工后高程波段单位未知或与声明单位冲突', 'DEM cut/fill after band unit is unknown or conflicts with elevation_unit'),
 'before_alpha': ('双DEM填挖方施工前不能选择 alpha 波段作为高程', 'DEM cut/fill before cannot use an alpha band as elevation'),
 'after_complex': ('双DEM填挖方施工后不支持复数高程或复数 alpha 波段', 'DEM cut/fill after does not support complex elevation or alpha bands'),
 'before_scale': ('双DEM填挖方施工前前必须显式应用高程 scale/offset', 'DEM cut/fill before requires elevation scale/offset to be applied first'),
 'after_offset': ('双DEM填挖方施工后前必须显式应用高程 scale/offset', 'DEM cut/fill after requires elevation scale/offset to be applied first'),
 'rotated': ('双DEM填挖方施工后需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影',
             'DEM cut/fill after requires a projected north-up grid with known linear units; reproject explicitly'),
 'south_up': ('双DEM填挖方施工前需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影',
              'DEM cut/fill before requires a projected north-up grid with known linear units; reproject explicitly'),
 'geographic': ('双DEM填挖方施工后需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影',
                'DEM cut/fill after requires a projected north-up grid with known linear units; reproject explicitly'),
 'no_crs': ('双DEM填挖方施工前需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影',
            'DEM cut/fill before requires a projected north-up grid with known linear units; reproject explicitly'),
 'no_grid': ('双DEM填挖方施工后需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影',
             'DEM cut/fill after requires a projected north-up grid with known linear units; reproject explicitly'),
 'crs': ('双DEM填挖方的两份DEM必须具有相同CRS、尺寸和完整网格，请先显式对齐', 'DEM cut/fill inputs must have the same CRS, dimensions and complete grid; align explicitly first'),
 'size': ('双DEM填挖方的两份DEM必须具有相同CRS、尺寸和完整网格，请先显式对齐',
          'DEM cut/fill inputs must have the same CRS, dimensions and complete grid; align explicitly first'),
 'transform': ('双DEM填挖方的两份DEM必须具有相同CRS、尺寸和完整网格，请先显式对齐',
               'DEM cut/fill inputs must have the same CRS, dimensions and complete grid; align explicitly first'),
 'empty': ('双DEM填挖方没有共同有效像元，不能生成零土方结果', 'DEM cut/fill has no jointly valid pixels; a zero-volume result cannot be produced'),
 'disjoint': ('双DEM填挖方没有共同有效像元，不能生成零土方结果', 'DEM cut/fill has no jointly valid pixels; a zero-volume result cannot be produced')}

def invalid_diagnostic(repository,env,identifier,problem,language,timeout):
    # Public safe projection hides computation causes. The exact owned execution
    # log must contain the submitted-language cause and the correct task stage.
    if problem not in MESSAGES or language not in ('zh-cn','en'):raise SuiteError('unknown cut/fill diagnostic')
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
                    and message.endswith('任务 cut_fill 执行失败: '+cause)):
                    return {'execution_id':identifier,'task':'cut_fill','cause':'invalid_'+problem,
                            'language':language,'source':'develop_runtime_log','exact_cause':cause}
        time.sleep(1)
    raise SuiteError('missing exact cut/fill task and submitted-language diagnostic')


def physical(repository,env,names):
    result=subprocess.run([sys.executable,'business/scripts/online-raster-cut-fill-fixture.py','verify',*names],cwd=repository,env=dict(env),text=True,capture_output=True,timeout=150)
    if result.returncode:raise SuiteError('independent complete cut/fill CSV verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout),'cut/fill physical evidence')

def check_physical(native,previous):
    rasterize.check_physical(native,previous)
    if native.get('runtime_workspaces_removed') is not True:raise SuiteError('Runtime workspace cleanup proof missing')
    for name,fact in obj(native.get('targets'),'physical targets').items():
        if name not in fixture.TARGETS or fact.get('schema')!=list(fixture.COLUMNS) or type(fact.get('row_count')) is not int or fact['row_count']!=1:raise SuiteError('physical table schema differs')
        for key in ('complete_table_verified','independent_decimal_oracle_verified','count_and_area_conservation_verified','nonnegative_gross_and_net_difference_verified','source_measurement_geometry_verified'):
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
    directory=Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'cut-fill-csv'
    if directory.is_symlink():raise SuiteError('CSV archive directory must not be symlink')
    directory.mkdir(parents=True,exist_ok=True);names=set(native['targets'])
    if any(p.name not in names or p.is_symlink() or not p.is_file() for p in directory.iterdir()):raise SuiteError('unexpected archived CSV inventory')
    manifest={}
    for name,fact in native['targets'].items():
        if name not in fixture.TARGETS:raise SuiteError('unknown exact archived target')
        payload=base64.b64decode(fact['csv_base64'],validate=True);path=directory/name
        if path.exists():
            if path.read_bytes()!=payload:raise SuiteError('previous archived CSV changed')
        else:
            with path.open('xb') as stream:stream.write(payload)
        manifest[name]={'path':'cut-fill-csv/'+name,'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'size_bytes':path.stat().st_size}
        if manifest[name]['sha256']!=fact['sha256'] or manifest[name]['size_bytes']!=fact['size_bytes']:raise SuiteError('archived CSV byte identity differs')
    path=directory.parent/'cut-fill-csv-manifest.json'
    if path.is_symlink():raise SuiteError('CSV manifest must not be symlink')
    temporary=path.with_suffix('.tmp')
    if temporary.is_symlink():raise SuiteError('CSV manifest temporary path must not be symlink')
    temporary.write_text(json.dumps(manifest,sort_keys=True,indent=2)+'\n');temporary.replace(path)
    return manifest


def expected_lineage(bindings,target):
    if (set(bindings)!={'input.before','input.after'} or any(not isinstance(v,str) or not v for v in bindings.values())
        or not isinstance(target,str) or not target):
        raise SuiteError('cut/fill requires both exact role-bound source locators and one target')
    return {'schema_version':'addp.lineage-facts/v1',
        'inputs':[{'port':p,'locator':bindings[p]} for p in sorted(bindings)],
        'outputs':[{'port':'output.save','locator':target,'write_mode':'create'}],
        'operations':[{'kind':'derive','operator':'save','input_ports':sorted(bindings),'output_ports':['output.save']}]}


def validate_success(execution,bindings,target):
    if obj(execution.get('outputs'),'outputs')!={'save':{'resource':{'locator':target,'type':'file','write_mode':'create'}}}:
        raise SuiteError('expected the exact named cut/fill CSV file output')
    metadata=obj(execution.get('metadata'),'metadata');facts=obj(metadata.get('lineage_facts'),'lineage')
    expected=expected_lineage(bindings,target)
    if set(facts)!=set(expected) or facts['schema_version']!=expected['schema_version']:
        raise SuiteError('cut/fill lineage envelope differs')
    for side in ('inputs','outputs'):
        actual={}
        for ref in array(facts[side],side):
            ref=obj(ref,'lineage resource');port=ref.get('port')
            if not isinstance(port,str) or not port or port in actual:raise SuiteError('duplicate or missing cut/fill lineage port')
            actual[port]=ref
        if actual!={ref['port']:ref for ref in expected[side]}:
            raise SuiteError('cut/fill role-bound source/target lineage differs')
    lineage_observer.validate(facts,expected,public=True)
    result=obj(metadata.get('result'),'result');scans=array(result.get('meta_scan_runs'),'scans')
    if (len(scans)!=1 or scans[0].get('status')!='submitted' or scans[0].get('target_locator')!=target
        or not isinstance(scans[0].get('execution_id'),str) or not scans[0]['execution_id']):
        raise SuiteError('expected one automatic exact cut/fill CSV scan')
    return facts,scans[0]['execution_id'],result


def record_lineage(env,identifier,name,bindings,target,execution,tenant,principal,workflow,reader):
    return lineage_observer.collect(env,SUITE,identifier,name,expected_lineage(bindings,target),execution,
                                    tenant,principal,workflow,reader)


def verify_lineage_archive(env,cases):
    lineage_observer.verify_archive(env,SUITE,cases)


def validate_preview(result,name,geometry=None):
    preview=obj(result,'table preview');schema=[{'name':n,'type':'int64' if i<6 else 'Float64'} for i,n in enumerate(fixture.COLUMNS)]
    if (preview.get('type')!='pandas_dataframe' or preview.get('schema')!=schema or type(preview.get('row_count')) is not int or preview['row_count']!=1
        or type(preview.get('preview_limit')) is not int or preview['preview_limit']!=50 or preview.get('truncated') is not False):raise SuiteError('table preview schema or bounds differ')
    try:fixture.validate_records(array(preview.get('preview_rows'),'preview rows'),name,preview=True,geometry=geometry)
    except fixture.FixtureError as error:raise SuiteError(str(error)) from error

def validate_item(item,native,name):
    size=item.get('size_bytes');physical_size=native.get('size_bytes')
    if type(size) is not int or size<=0 or type(physical_size) is not int or size!=physical_size or not item.get('scanned_at'):raise SuiteError('CSV physical/Meta sizes must be equal positive integers')
    descriptor=obj(obj(item.get('attributes'),'attributes').get('item'),'item facts')
    if item.get('item_type')!='file' or item.get('full_name')!='outputs/'+name or descriptor.get('format')!='csv':raise SuiteError('CSV item identity differs')
    return {'physical_size_bytes':physical_size,'meta_size_bytes':size}

def run_scenario(repository,env,client,physical_runner=physical,diagnostic_runner=invalid_diagnostic,lineage_reader=lineage_observer.read_current):
    timeout=float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS','600'))
    tenant=positive(env['ADDP_ONLINE_TEST_TENANT_ID'],'tenant')
    if tenant<=1:raise SuiteError('raster DEM cut/fill acceptance forbids the default tenant')
    identity=raster.validate_identity(client,tenant,required_permissions=raster.EDITOR_PERMISSIONS)
    engine=positive(env['ADDP_ONLINE_CUT_FILL_NFS_ENGINE_ID'],'NFS engine')
    scopes={name:f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
    source_scan=support.wait_for_meta_scan(client,engine,time.monotonic()+timeout,scope={'targets':list(scopes.values())})
    items={name:support.find_fixture_item(client,engine,name,'raster DEM cut/fill source') for name in fixture.SOURCES}
    sources={name:scopes[name]+f'&item_id={items[name]["id"]}' for name in fixture.SOURCES}
    before={name:client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload for name,item in items.items()}
    runtimes=array(client.request('GET','/api/v1/develop/workflow-engines',(200,)).payload,'workflow engines')
    selected=[r for r in runtimes if r.get('engine_type')=='geopython_workflow' and r.get('connection_status')=='online']
    if len(selected)!=1:raise SuiteError('expected exactly one online product GeoPython Runtime')
    runtime=positive(selected[0].get('id'),'Runtime engine')
    parent=f'addp://engine/{engine}/path/outputs?type=directory'
    cases,names,previous,scans=[],[],{},[]
    for name in fixture.TARGETS:
        target=f'addp://engine/{engine}/path/outputs/{name}?type=file'
        workflow=definition(sources,parent,name)
        identifier=raster.submit(client,runtime,workflow)
        execution=raster.wait_execution(client,'develop',identifier,timeout)
        inputs=case_inputs(name);bindings={'input.before':sources[inputs[0]],'input.after':sources[inputs[1]]}
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
                raise SuiteError('DEM cut/fill failure has the wrong error category')
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
    verify_lineage_archive(env,cases)
    archived=archive_csv(env,native)
    return {'schema_version':'addp.'+SUITE+'/v1','suite':SUITE,'result':'passed','run_id':env['ADDP_ONLINE_TEST_RUN_ID'],
        'identity':identity,'sources':sources,'source_scan_execution_id':source_scan,'source_unchanged':True,
        'cases':cases,'failed_parameters':failures,
        'physical':native,'archived_csv':archived,'cleanup_boundary':'owned NFS export/directory and disposable deployment; no business DELETE acceptance'}


def main():
    env=dict(os.environ)
    report=run_scenario(Path(__file__).resolve().parents[2],env,DiagnosticClient(env['GATEWAY_URL'],env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30,env['ADDP_ONLINE_ARTIFACT_DIR']))
    if len(report['cases'])!=12 or len(report['failed_parameters'])!=36 or len(report['archived_csv'])!=12:raise SuiteError('incomplete fixed cut/fill matrix')
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'cut-fill-persistence.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    print(json.dumps(report,ensure_ascii=False,sort_keys=True))

if __name__=='__main__':
    try:main()
    except Exception as error:
        print(f'Cut/fill persistence acceptance failed: {error if isinstance(error,SuiteError) else type(error).__name__}',file=sys.stderr);raise SystemExit(1)
