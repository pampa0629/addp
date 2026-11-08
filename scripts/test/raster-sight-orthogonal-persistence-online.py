#!/usr/bin/env python3
"""16 actual CSV/COG chains, 40 exact refusals, 776 rereads, one 600s budget."""
from __future__ import annotations
import base64,importlib,json,math,re,subprocess,sys,time
from pathlib import Path
view=importlib.import_module('scripts.test.raster-viewshed-persistence-online')
sight=importlib.import_module('scripts.test.raster-line-of-sight-persistence-online')
fixture=importlib.import_module('business.scripts.online-raster-sight-orthogonal-fixture')
PREFIX=fixture.PREFIX
SUITE=fixture.SUITE
ARCHIVE_NAME='sight-orthogonal-files'
MANIFEST_KEY='archived_files'
COUNTS=(16,40,776,57)
SuiteError=view.SuiteError
raster,lineage,base=view.raster,view.lineage,view.base
encoded,obj=view.encoded,view.obj
FAILURES={
 'sight-shear':('sight','grid','shear.tif',{}), 'view-shear':('view','grid','shear.tif',{}),
 'sight-near':('sight','grid','near-orthogonal.tif',{}), 'view-near':('view','grid','near-orthogonal.tif',{}),
 'sight-reflect-column':('sight','grid','reflect-column.tif',{}), 'view-reflect-column':('view','grid','reflect-column.tif',{}),
 'sight-reflect-row':('sight','grid','reflect-row.tif',{}), 'view-reflect-row':('view','grid','reflect-row.tif',{}),
 'sight-geographic':('sight','grid','geographic.tif',{}), 'view-geographic':('view','grid','geographic.tif',{}),
 'sight-no-crs':('sight','grid','no-crs.tif',{}), 'view-no-crs':('view','grid','no-crs.tif',{}),
 'sight-no-grid':('sight','grid','no-grid.tif',{}), 'view-no-grid':('view','grid','no-grid.tif',{}),
 'sight-degenerate':('sight','grid','degenerate.tif',{}), 'view-degenerate':('view','grid','degenerate.tif',{}),
 'sight-coincident':('sight','coincident','rotated.tif',{'coincident':True}),
 'sight-outside':('sight','outside','rotated.tif',{'outside':True}),
 'view-radius':('view','radius','rotated.tif',{'radius_metres':0.}),
 'view-outside':('view','outside','rotated.tif',{'outside':True})}

class DiagnosticClient(base.DiagnosticClient):
    diagnostic_name=PREFIX+'-submission-error.json'
    diagnostic_schema='addp.'+PREFIX+'-submission-diagnostic/v1'


def definition(sources,parent,name,*,invalid=None,language='zh-cn'):
    if invalid:
        kind,_,source,change=FAILURES[invalid];template='rotated.csv'if kind=='sight'else'rotated.cog.tif';params=fixture.parameters(template)
        change=dict(change)
        if change.pop('coincident',False):params['target_xy']=params['observer_xy'][:]
        if change.pop('outside',False):
            spec=fixture.source_spec(source);params['target_xy'if kind=='sight'else'observer_xy']=fixture.world(spec['grid'],(spec['width'],8.5))
        params.update(change);name=f'failed-{invalid}-{language}'+('.csv'if kind=='sight'else'.cog.tif')
    else:source,kind=fixture.CASES[name];params=fixture.parameters(name)
    save={'input_df':{'$ref':kind},'target_parent_locator':parent,'target_name':name,'mode':'fail'}if kind=='sight'else{
        'input_raster':{'$ref':kind},'target_parent_locator':parent,'target_name':name,'write_mode':'create','profile':'cog','blocksize':128,'overview_resampling':'nearest'}
    return {'tasks':[{'id':'source','operator':'raster_load','depends_on':[],'params':{'locator':sources[source]}},
        {'id':kind,'operator':'raster_line_of_sight'if kind=='sight'else'raster_viewshed','depends_on':['source'],'params':{'input_raster':{'$ref':'source'},**params}},
        {'id':'save','operator':'save'if kind=='sight'else'raster_save','depends_on':[kind],'params':save}]}


def expected_lineage(source,target):
    return (sight if target.split('?')[0].endswith('.csv')else view).expected_lineage(source,target)


def diagnostic_fact(identifier,problem,language):
    kind,code,_,_=FAILURES[problem]
    return {'execution_id':identifier,'task':kind,'problem':problem,'language':language,
        'exact_cause':(sight if kind=='sight'else view).ERRORS[code][language=='en'],'source':'develop_runtime_log'}


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
                if(event.get('module_name')=='develop'and event.get('role')=='backend'and f'[DevExecutor] 工作流引擎调用失败: execution_id={identifier} err='in message and message.endswith('任务 '+expected['task']+' 执行失败: '+expected['exact_cause'])):return expected
        time.sleep(min(1,max(0,deadline-time.monotonic())))
    raise SuiteError('missing exact sight/view node and submitted-language refusal')


def physical(repository,env,action,names,timeout):
    response=subprocess.run([sys.executable,'business/scripts/online-raster-sight-orthogonal-fixture.py',action,*names],cwd=repository,env=env,capture_output=True,text=True,timeout=min(150,timeout))
    if response.returncode:raise SuiteError('owned sight physical verification failed: '+response.stderr[-1200:])
    return obj(json.loads(response.stdout),'physical report')


class ViewshedMetadata(view.ViewshedMetadata):
    prefix=PREFIX
    suite=SUITE
    fixture=fixture
    def file_contract(self,name):
        return ('table','csv',('single',))if name.endswith('.csv')else super().file_contract(name)


def success_language(index):
    return 'zh-cn'if(index//2)%2==0 else'en'


def verify_sources(facts):
    if set(facts)!=set(fixture.SOURCES):raise SuiteError('source inventory differs')
    for fact in facts.values():
        if type(fact.get('size_bytes'))is not int or fact['size_bytes']<=0 or not isinstance(fact.get('sha256'),str)or not re.fullmatch('[0-9a-f]{64}',fact['sha256']):raise SuiteError('source size/hash differs')


def validate_frozen(baseline):
    frozen=obj(baseline.get('expectations'),'frozen references')
    if set(frozen)!=set(fixture.TARGETS)or baseline.get('expected_array_count')!=8:raise SuiteError('eight CSV and eight COG references required')
    for key in('scope_sha256','anchor_sha256'):
        if not isinstance(baseline.get(key),str)or not re.fullmatch('[0-9a-f]{64}',baseline[key]):raise SuiteError('oracle scope/anchor differs')
    for name,fact in frozen.items():
        if name.endswith('.csv'):
            row=fact.get('row',{})
            if set(row)!={'distance_m','status'}or type(row['distance_m'])is not float or not math.isfinite(row['distance_m'])or row['distance_m']<=0 or row['status']not in('visible','blocked','indeterminate')or set(fact.get('preview_row',{}))!=set(row):raise SuiteError('CSV oracle differs')
        else:
            expected=fixture.artifact_expectation(name);counts=fact.get('state_counts',{})
            if fact.get('artifact')!=expected or set(counts)!={'0','1','2','255'}or any(type(v)is not int or v<0 for v in counts.values())or sum(counts.values())!=expected['width']*expected['height']:raise SuiteError('full COG oracle differs')


def validate_final(result,name,frozen):
    if name.endswith('.csv'):sight.validate_preview(result.get('final_result'),frozen['preview_row'])
    else:
        expected=fixture.artifact_expectation(name);artifact=obj(result.get('final_result'),'COG artifact')
        raster.validate_raster_artifact(artifact,expected,dtype='Byte')
        if artifact.get('transform')!=expected['transform']or artifact.get('extent')!=expected['extent']:raise SuiteError('full affine/extent differs')


def validate_persisted(result,native,item,name):
    if name.endswith('.csv'):
        # save's resource declaration is checked by the shared validate_success.
        if type(item.get('size_bytes'))is not int or item['size_bytes']!=native['size_bytes']or item['size_bytes']<=0:raise SuiteError('CSV physical/Meta positive sizes differ')
        return {'physical':native['size_bytes'],'meta':item['size_bytes'],'declaration_contract':'resource-target-type-write-mode'}
    raster.validate_target_item(item,fixture.artifact_expectation(name),native)
    return raster.strict_sizes(result['final_result'],native,item,name)


def check_and_archive(report,names,sources,frozen,archive,previous,scope_hash):
    targets=obj(report.get('targets'),'physical targets');manifest=obj(report.get('archived_files'),'original manifest')
    if report.get('runtime_workspaces_removed')is not True or report.get('sources')!=sources or set(targets)!=set(names)or set(manifest)!=set(names)or{p.name for p in archive.iterdir()}!=set(names):raise SuiteError('source/runtime/actual/archive inventory differs')
    if names and report.get('oracle_cache')!={'scope_sha256':scope_hash,'read_only_expected':True,'expected_arrays_loaded':sum(n.endswith('.tif')for n in names),'full_target_checks':len(names)}:raise SuiteError('oracle scope or complete rereads differ')
    result={}
    for name,fact in targets.items():
        raw=fixture.digest(archive/name);receipt={'path':archive.name+'/'+name,**raw}
        if any(type(fact.get(k))is not type(v)or fact[k]!=v for k,v in raw.items())or manifest[name]!=receipt or(name in previous and previous[name]!=receipt):raise SuiteError('original bytes/size/hash or prior receipt changed')
        if name.endswith('.csv'):
            payload=base64.b64decode(fact.get('csv_base64',''),validate=True)
            if payload!=(archive/name).read_bytes()or fact.get('complete_csv_verified')is not True or fixture.parse_csv(payload,frozen[name]['row'])!=fact.get('row'):raise SuiteError('complete CSV differs')
        else:
            if any(fact.get(k)is not True for k in('cog_valid','full_pixel_and_mask_verification','independent_cell_clipping_verified','nearest_overviews_verified'))or type(fact.get('has_overviews'))is not bool or type(fact.get('overview_count'))is not int or type(fact.get('valid_pixels'))is not int or not isinstance(fact.get('state_counts'),dict)or any(type(v)is not int for v in fact['state_counts'].values())or fact.get('state_counts')!=frozen[name]['state_counts']or fact.get('valid_pixels')!=sum(frozen[name]['state_counts'][str(c)]for c in(0,1,2)):raise SuiteError('complete COG pixels/mask/reference differs')
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
    except Exception as error:print('Sight orthogonal acceptance failed: '+(str(error)if isinstance(error,SuiteError)else type(error).__name__),file=sys.stderr);raise SystemExit(1)
