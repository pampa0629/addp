#!/usr/bin/env python3
"""16 hillshade COGs, 34 precise bilingual refusals and 680 complete rereads in 600s."""
from __future__ import annotations
import importlib,json,math,os,re,subprocess,sys,time,traceback
from pathlib import Path
view=importlib.import_module('scripts.test.raster-viewshed-persistence-online')
orth=importlib.import_module('scripts.test.raster-sight-orthogonal-persistence-online')
fixture=importlib.import_module('business.scripts.online-raster-hillshade-orthogonal-fixture')
PREFIX,SUITE=fixture.PREFIX,fixture.SUITE
ARCHIVE_NAME='hillshade-orthogonal-cogs'
MANIFEST_KEY='archived_cogs'
COUNTS=(16,34,680,51)
SuiteError=view.SuiteError
raster,lineage,base=view.raster,view.lineage,view.base
encoded,obj=view.encoded,view.obj
expected_lineage=view.expected_lineage
source_scan_client=orth.source_scan_client
check_scan_inventory=orth.check_scan_inventory
FAILURES={
 'shear':('grid','shear.tif',{}),'near-orthogonal':('grid','near-orthogonal.tif',{}),
 'reflect-column':('grid','reflect-column.tif',{}),'reflect-row':('grid','reflect-row.tif',{}),
 'geographic':('grid','geographic.tif',{}),'no-crs':('grid','no-crs.tif',{}),
 'no-grid':('grid','no-grid.tif',{}),'degenerate':('grid','degenerate.tif',{}),
 'band-zero':('band','north.tif',{'band':0}),'band-high':('band','north.tif',{'band':99}),
 'alpha':('alpha','north.tif',{'band':4}),'unit':('unit','north.tif',{'elevation_unit':'yard'}),
 'unit-bool':('unit','north.tif',{'elevation_unit':True}),
 'unit-conflict':('unit-conflict','north.tif',{'elevation_unit':'foot'}),
 'unit-unknown':('unit-conflict','unknown-unit.tif',{}),
 'scale':('scale','scaled.tif',{}),'offset':('scale','offset.tif',{})}
ERRORS={
 'grid':('DEM 地形晕渲需要已知线性单位的二维投影及保持北向轴手性的正交网格，请显式重投影','DEM hillshade requires a two-dimensional projected orthogonal grid with known linear units and north-up handedness; reproject explicitly'),
 'band':('DEM 地形晕渲必须显式选择有效的一基高程数据波段','DEM hillshade requires an explicit valid one-based elevation band'),
 'alpha':('DEM 地形晕渲不能选择 alpha 波段作为高程','DEM hillshade cannot use an alpha band as elevation'),
 'unit':('DEM 地形晕渲必须明确声明 metre、foot 或 us_survey_foot 高程单位','DEM hillshade requires elevation_unit metre, foot or us_survey_foot'),
 'unit-conflict':('DEM 地形晕渲高程波段单位未知或与声明单位冲突','DEM hillshade band unit is unknown or conflicts with elevation_unit'),
 'scale':('DEM 地形晕渲前必须显式应用高程 scale/offset','DEM hillshade requires elevation scale/offset to be applied first')}

class DiagnosticClient(base.DiagnosticClient):
    diagnostic_name=PREFIX+'-submission-error.json'
    diagnostic_schema='addp.'+PREFIX+'-submission-diagnostic/v1'

class ViewshedMetadata(orth.ViewshedMetadata):
    prefix=PREFIX
    suite=SUITE
    fixture=fixture


def success_language(index):return 'zh-cn'if index%2==0 else'en'


def definition(sources,parent,name,*,invalid=None,language='zh-cn'):
    if name not in fixture.CASES or language not in('zh-cn','en')or invalid not in(None,*FAILURES):raise SuiteError('unexpected hillshade case')
    source=fixture.CASES[name][0];params=fixture.parameters(name)
    if invalid:
        _,source,changed=FAILURES[invalid];params.update(changed);name=f'failed-{invalid}-{language}.cog.tif'
    return {'tasks':[
        {'id':'source','operator':'raster_load','depends_on':[],'params':{'locator':sources[source]}},
        {'id':'hillshade','operator':'raster_hillshade','depends_on':['source'],'params':{'input_raster':{'$ref':'source'},**params}},
        {'id':'save','operator':'raster_save','depends_on':['hillshade'],'params':{'input_raster':{'$ref':'hillshade'},'target_parent_locator':parent,'target_name':name,'write_mode':'create','profile':'cog','blocksize':128,'overview_resampling':'nearest'}}]}


def diagnostic_fact(identifier,problem,language):
    return {'execution_id':identifier,'task':'hillshade','problem':problem,'language':language,
        'exact_cause':ERRORS[FAILURES[problem][0]][language=='en'],'source':'develop_runtime_log'}


def invalid_diagnostic(repository,env,identifier,problem,language,timeout):
    expected=diagnostic_fact(identifier,problem,language);root=Path(env.get('ADDP_RUNTIME_LOG_ROOT',repository/'logs/runtime'))
    if not root.is_absolute():root=repository/root
    deadline=time.monotonic()+timeout
    while time.monotonic()<deadline:
        for path in(root/'develop').glob('*/*.jsonl'):
            for line in path.read_text().splitlines():
                try:event=json.loads(line)
                except json.JSONDecodeError:continue
                message=event.get('message','')
                if(event.get('module_name')=='develop'and event.get('role')=='backend'and f'[DevExecutor] 工作流引擎调用失败: execution_id={identifier} err='in message and message.endswith('任务 hillshade 执行失败: '+expected['exact_cause'])):return expected
        time.sleep(min(1,max(0,deadline-time.monotonic())))
    raise SuiteError('missing exact hillshade node and submitted-language refusal')


def physical(repository,env,action,names,timeout):
    response=subprocess.run([sys.executable,'business/scripts/online-raster-hillshade-orthogonal-fixture.py',action,*names],cwd=repository,env=env,capture_output=True,text=True,timeout=min(150,timeout))
    if response.returncode:raise SuiteError('owned hillshade physical verification failed: '+response.stderr[-1200:])
    return obj(json.loads(response.stdout),'physical report')


def verify_sources(facts):
    if set(facts)!=set(fixture.SOURCES):raise SuiteError('hillshade source inventory differs')
    for fact in facts.values():
        if type(fact.get('size_bytes'))is not int or fact['size_bytes']<=0 or not re.fullmatch('[0-9a-f]{64}',str(fact.get('sha256',''))):raise SuiteError('hillshade source size/hash differs')


def validate_frozen(baseline):
    frozen=obj(baseline.get('expectations'),'hillshade frozen references')
    if set(frozen)!=set(fixture.CASES)or baseline.get('expected_array_count')!=16:raise SuiteError('sixteen full hillshade references required')
    for key in('scope_sha256','anchor_sha256'):
        if not re.fullmatch('[0-9a-f]{64}',str(baseline.get(key,''))):raise SuiteError('hillshade frozen anchor differs')
    for name,fact in frozen.items():
        spec=fixture.artifact_expectation(name)
        if fact.get('artifact')!=spec or fact.get('unit')!=fixture.CASES[name][1]or any(type(fact.get(k))is not int for k in('valid_pixels','zero_pixels'))or not 0<=fact['zero_pixels']<=fact['valid_pixels']<=spec['width']*spec['height']:raise SuiteError('hillshade reference facts differ')


def validate_final(result,name,frozen):
    artifact=obj(result.get('final_result'),'hillshade COG artifact');expected=fixture.artifact_expectation(name)
    raster.validate_raster_artifact(artifact,expected)
    if artifact.get('transform')!=expected['transform']or artifact.get('extent')!=expected['extent']:raise SuiteError('hillshade full affine/extent differs')


def validate_persisted(result,native,item,name):
    raster.validate_target_item(item,fixture.artifact_expectation(name),native)
    return raster.strict_sizes(result['final_result'],native,item,name)


def check_and_archive(report,names,sources,frozen,archive,previous,scope_hash):
    targets=obj(report.get('targets'),'physical targets');manifest=obj(report.get(MANIFEST_KEY),'original manifest')
    if report.get('runtime_workspaces_removed')is not True or report.get('sources')!=sources or set(targets)!=set(names)or set(manifest)!=set(names)or{p.name for p in archive.iterdir()}!=set(names):raise SuiteError('hillshade source/runtime/actual/archive inventory differs')
    if names and report.get('oracle_cache')!={'scope_sha256':scope_hash,'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}:raise SuiteError('hillshade oracle scope or complete rereads differ')
    result={}
    for name,fact in targets.items():
        receipt={'path':archive.name+'/'+name,**fixture.digest(archive/name)}
        if (type(fact.get('size_bytes'))is not int or fact['size_bytes']<=0 or any(fact.get(k)!=receipt[k]for k in('size_bytes','sha256'))or manifest[name]!=receipt or(name in previous and previous[name]!=receipt)
            or any(fact.get(k)is not True for k in('cog_valid','full_pixel_and_mask_verification','independent_horn_verified','nearest_overviews_verified'))
            or type(fact.get('has_overviews'))is not bool or type(fact.get('overview_count'))is not int or any(type(fact.get(k))is not int or fact[k]!=frozen[name][k]for k in('valid_pixels','zero_pixels'))or fact.get('unit')!=frozen[name]['unit']):raise SuiteError('hillshade complete COG or original byte facts differ')
        result[name]=receipt
    return result


def run_scenario(repository,env,client,physical_runner=physical,diagnostic_runner=invalid_diagnostic,lineage_reader=lineage.read_current,metadata_factory=None):
    return view.run_scenario(repository,env,client,physical_runner,diagnostic_runner,lineage_reader,metadata_factory,profile=sys.modules[__name__])


def main():
    env=dict(os.environ);client=DiagnosticClient(env['GATEWAY_URL'],env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'],30,env['ADDP_ONLINE_ARTIFACT_DIR'])
    report=run_scenario(Path(__file__).resolve().parents[2],env,client)
    view.immutable(Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/(PREFIX+'-persistence.json'),encoded(report));print(json.dumps(report,sort_keys=True))
if __name__=='__main__':
    try:main()
    except Exception as error:
        if not isinstance(error,SuiteError):
            attribute=error.name if isinstance(error,AttributeError)else None
            diagnostic={'exception_type':type(error).__name__,
                'attribute':attribute if isinstance(attribute,str)and re.fullmatch(r'[A-Za-z_][A-Za-z_0-9]*',attribute)else None,
                'frames':[{'file':Path(frame.filename).name,'function':frame.name,'line':frame.lineno}for frame in traceback.extract_tb(error.__traceback__)]}
            print('Hillshade orthogonal diagnostic: '+json.dumps(diagnostic,sort_keys=True),file=sys.stderr)
        print('Hillshade orthogonal acceptance failed: '+(str(error)if isinstance(error,SuiteError)else type(error).__name__),file=sys.stderr)
        raise SystemExit(1)
