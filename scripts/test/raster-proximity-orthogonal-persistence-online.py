#!/usr/bin/env python3
"""Eighteen original COGs, 60 accurate failures, 1251 full output rereads, fixed 600s."""
from __future__ import annotations
import copy
import hashlib
import importlib
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys
import time

base = importlib.import_module('scripts.test.raster-statistics-percentile-workflow-online')
fixture = importlib.import_module('business.scripts.online-raster-proximity-orthogonal-fixture')
lineage = importlib.import_module('scripts.utils.online_raster_lineage')
def source_edges(client, identifier):
    graph = obj(client.request('GET', f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={identifier}&direction=downstream', (200,)).payload, 'source graph')
    if graph.get('truncated') is not False: raise SuiteError('source graph is incomplete')
    return sorted(json.dumps(edge, sort_keys=True) for edge in array(graph.get('edges'), 'edges')
                  if edge.get('status') == 'active' and edge.get('relation_kind') == 'derive')
raster, support, readonly = base.raster, base.support, base.readonly
obj, array, positive, SuiteError = base.obj, base.array, base.positive, base.SuiteError
SUITE, FAILURES = fixture.SUITE, fixture.FAILURES
SubmittedLanguageClient, BudgetClient = base.SubmittedLanguageClient, base.BudgetClient
encoded, immutable, archive_response = base.encoded, base.immutable, base.archive_response
MESSAGES={}
def fixed(names,message):
    for name in names:MESSAGES[name]=(message,message)
fixed(('band_zero','band_high','band_boolean'),'band must be a valid one-based index')
fixed(('alpha_band','float','int64','uint64'),'Proximity requires a non-alpha integer data band of at most 32 bits')
fixed(('complex',),'Complex-valued bands are not supported by numeric analysis')
fixed(('targets_empty','targets_duplicate','targets_boolean','targets_fraction','targets_object','targets_range','targets_nonlist'),'target_values must be distinct integers within the selected band type')
fixed(('no_target','invalid_targets'),'Raster distance transform requires at least one valid target pixel')
for problem in ('geographic','compound','unknown_unit','rotated_x','rotated_y','orientation','tiny_shear','rounded_dot','south_up','rotated_mirror'):
    MESSAGES[problem]=('邻近距离需要已知线性单位的二维投影及保持北向轴手性的正交网格，请显式重投影', 'Proximity requires a two-dimensional projected orthogonal grid with known linear units and north-up handedness; reproject explicitly')
fixed(('missing_crs',),'CRS must be explicit and non-empty')
fixed(('missing_grid',),'Raster requires a geotransform')
fixed(('singular',),'Raster grid must have a finite invertible geotransform')

class DiagnosticClient(base.DiagnosticClient):
    diagnostic_name='proximity_orthogonal-submission-error.json'
    diagnostic_schema='addp.raster-proximity-orthogonal-submission-diagnostic/v1'

def case_inputs(name):return [fixture.CASES[name][0]]

def definition(sources,parent,name,*,invalid=None,language='zh-cn'):
    if language not in('zh-cn','en')or name not in fixture.TARGETS or invalid not in(None,*FAILURES)or(invalid and name!='wide.cog.tif'):raise SuiteError('unknown exact rectangular proximity case')
    source,band,codes=fixture.CASES[name]
    if invalid in('float','complex','int64','uint64','no_target','invalid_targets','geographic','compound','missing_crs','missing_grid','unknown_unit','rotated_x','rotated_y','orientation','singular','tiny_shear','rounded_dot','south_up','rotated_mirror'):source=invalid+'.tif'
    if invalid in('band_zero','band_high','band_boolean','alpha_band'):band={'band_zero':0,'band_high':99,'band_boolean':True,'alpha_band':4}[invalid]
    bad={'targets_empty':[],'targets_duplicate':[1,1],'targets_boolean':[True],'targets_fraction':[.5],'targets_object':[{}],'targets_range':[2**31],'targets_nonlist':{}}
    if invalid in bad:codes=copy.deepcopy(bad[invalid])
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'locator':sources[source]}},
       {'id':'distance','operator':'raster_proximity','depends_on':['source'],'params':{'input_raster':{'$ref':'source'},'band':band,'target_values':codes}},
       {'id':'save','operator':'raster_save','depends_on':['distance'],'params':{'input_raster':{'$ref':'distance'},'target_parent_locator':parent,'target_name':f'failed-{invalid}-{language}.cog.tif'if invalid else name,'write_mode':'create','profile':'cog','blocksize':128,'overview_resampling':'nearest'}}]
    return {'tasks':tasks}

def expected_lineage(sources,name,target):
    return {'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.source','locator':sources[case_inputs(name)[0]]}],
        'outputs':[{'port':'output.save','locator':target,'write_mode':'create'}],
        'operations':[{'kind':'derive','operator':'raster_save','input_ports':['input.source'],'output_ports':['output.save']}]}

validate_success=raster.validate_success
validate_target_item=raster.validate_target_item

def overview_count(name):
    h,w=fixture.case_shape(name);count=0
    while max(h,w)>128:h,w=max(1,h//2),max(1,w//2);count+=1
    return count

def check_physical(value,sources,previous,scope,names,expected_archive=None):
    if (value.get('runtime_workspaces_removed') is not True or value.get('full_source_pixels_verified') is not True
        or value.get('sources')!=sources or set(obj(value.get('targets'),'physical targets'))!=set(names)):raise SuiteError('complete source/target/cleanup proof differs')
    cache=obj(value.get('oracle_cache'),'oracle cache')
    if cache!={'scope_sha256':scope,'read_only_expected':True,'expected_arrays_loaded':len(names),'full_target_checks':len(names)}:raise SuiteError('frozen oracle/count proof differs')
    if type(cache['expected_arrays_loaded']) is not int or type(cache['full_target_checks']) is not int:raise SuiteError('exact integer full-check counts required')
    expected=obj(value.get('archived_expectations'),'frozen independent arrays')
    if set(expected)!={n+'.npy' for n in fixture.TARGETS} or (expected_archive is not None and expected!=expected_archive):raise SuiteError('frozen independent array archive changed')
    for name in fixture.TARGETS:
        fact=expected[name+'.npy'];size=fixture.case_shape(name)[0]*fixture.case_shape(name)[1]*8
        if set(fact)!={'path','size_bytes','sha256'} or fact['path']!='proximity_orthogonal-expectations/'+name+'.npy' or type(fact['size_bytes']) is not int or not size<fact['size_bytes']<size+1024 or not isinstance(fact['sha256'],str) or not re.fullmatch('[0-9a-f]{64}',fact['sha256']):raise SuiteError('independent array size/hash differs')
    archives=obj(value.get('archived_cogs'),'original COGs')
    if set(archives)!=set(names) or set(obj(value.get('archived_sources'),'source originals'))!=set(fixture.SOURCES):raise SuiteError('archive inventory differs')
    for source,fact in sources.items():
        if type(fact.get('size_bytes')) is not int or fact['size_bytes']<=0 or not isinstance(fact.get('sha256'),str) or not re.fullmatch('[0-9a-f]{64}',fact['sha256']) or value['archived_sources'][source]!={'path':'proximity_orthogonal-sources/'+source,**fact}:raise SuiteError('source original size/hash differs')
    flags=('cog_valid','full_pixel_and_mask_verification','independent_scalar_oracle_verified','original_coverage_verified','actual_metre_nearest_verified','square_results_preserved','proximity_orthogonal_verified','nearest_overviews_verified','explicit_selected_band_verified','legal_zero_verified')
    for name,fact in value['targets'].items():
        if (any(fact.get(k) is not True for k in flags) or type(fact.get('size_bytes')) is not int or fact['size_bytes']<=0
            or not isinstance(fact.get('sha256'),str) or not re.fullmatch('[0-9a-f]{64}',fact['sha256'])
            or type(fact.get('overview_count')) is not int or fact['overview_count']!=overview_count(name)
            or fact.get('has_overviews') is not bool(overview_count(name)) or fact.get('distance_unit')!='metre'
            or archives[name]!={'path':'proximity_orthogonal-cogs/'+name,**{k:fact[k] for k in ('size_bytes','sha256')}}):raise SuiteError('complete COG/size/hash/archive proof differs')
        if name in previous and fact!=previous[name]:raise SuiteError('previous original COG changed')


def invalid_diagnostic(repository, env, identifier, problem, language, timeout):
    cause = MESSAGES[problem][language == 'en']; root = Path(env.get('ADDP_RUNTIME_LOG_ROOT', repository/'logs/runtime'))
    if not root.is_absolute(): root = repository/root
    deadline = time.monotonic()+timeout
    while time.monotonic() < deadline:
        for path in (root/'develop').glob('*/*.jsonl'):
            for line in path.read_text().splitlines():
                try: event = json.loads(line)
                except json.JSONDecodeError: continue
                message = event.get('message', '')
                if (event.get('module_name') == 'develop' and event.get('role') == 'backend'
                    and f'[DevExecutor] 工作流引擎调用失败: execution_id={identifier} err=' in message
                    and message.endswith('任务 distance 执行失败: '+cause)):
                    return {'execution_id': identifier, 'task': 'distance', 'problem': problem, 'language': language, 'exact_cause': cause, 'source': 'develop_runtime_log'}
        time.sleep(min(1, max(0, deadline-time.monotonic())))
    raise SuiteError('missing exact rectangular proximity task and cause')

def fixture_call(repository, env, action, names, timeout):
    if action not in ('prepare-oracle', 'verify', 'cleanup-oracle'): raise SuiteError('unknown oracle action')
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-proximity-orthogonal-fixture.py', action, *names],
                            cwd=repository, env=dict(env), capture_output=True, text=True, timeout=min(150, timeout))
    if result.returncode: raise SuiteError('independent proximity_orthogonal COG verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'proximity_orthogonal physical evidence')

def current(execution, identity, identifier, dag, runtime, language, status):
    readonly.actor(execution, identity, 'develop'); readonly.execution_snapshot(execution, dag, runtime)
    if (execution.get('execution_id') != identifier or type(execution.get('tenant_id')) is not int
        or execution['tenant_id'] != int(identity['tenant_id']) or execution.get('task_type') != 'workflow'
        or execution.get('status') != status or execution['execution_config'].get('display_language') != language): raise SuiteError('current execution identity/status/language differs')

def run_scenario(repository, env, client, physical_runner=fixture_call, diagnostic_runner=invalid_diagnostic, lineage_reader=lineage.read_current):
    env = dict(env); started = time.monotonic(); budget = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    if not math.isfinite(budget) or budget != 600: raise SuiteError('rectangular proximity business budget must be exactly 600 seconds')
    def remaining():
        left = budget-(time.monotonic()-started)
        if left <= 0: raise SuiteError('rectangular proximity business budget exhausted')
        return left
    client = BudgetClient(client, remaining); directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR']); directory.mkdir(parents=True, exist_ok=True)
    stages = []; primary = cleanup_error = report = None
    def progress(status):
        (directory/'proximity_orthogonal-progress.json').write_bytes(encoded({'suite': SUITE, 'result': status,
            'budget_ms': 600000, 'elapsed_ms': round((time.monotonic()-started)*1000), 'stages': stages}))
    try:
        tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
        if tenant <= 1: raise SuiteError('proximity_orthogonal acceptance forbids default tenant')
        identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
        engine = positive(env['ADDP_ONLINE_PROXIMITY_ORTHOGONAL_NFS_ENGINE_ID'], 'NFS engine')
        scopes = {name: f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
        scan = support.wait_for_meta_scan(client, engine, time.monotonic()+remaining(), scope={'targets': list(scopes.values())})
        items = {name: support.find_fixture_item(client, engine, name, 'proximity_orthogonal source') for name in fixture.SOURCES}
        sources = {name: scopes[name]+f'&item_id={items[name]["id"]}' for name in fixture.SOURCES}
        before = {name: client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload for name, item in items.items()}
        source_scan_identity = base.validate_scan(raster.wait_execution(client, 'meta', scan, remaining()), scan, None, tenant, int(identity['principal_id']), engine)
        selected = [v for v in array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'runtimes')
                    if v.get('engine_type') == 'geopython_workflow' and v.get('connection_status') == 'online']
        if len(selected) != 1: raise SuiteError('exactly one same-source product Runtime is required')
        runtime = positive(selected[0].get('id'), 'Runtime'); parent = f'addp://engine/{engine}/path/outputs?type=directory'
        baseline=physical_runner(repository,env,'prepare-oracle',[],remaining());remaining()
        anchor=baseline.get('anchor_sha256');scope=baseline.get('scope_sha256')
        if any(not isinstance(v,str) or not re.fullmatch('[0-9a-f]{64}',v) for v in (anchor,scope)):raise SuiteError('frozen oracle identity missing')
        if type(baseline.get('expected_array_count')) is not int or baseline['expected_array_count']!=18:raise SuiteError('eighteen frozen independent arrays required')
        if type(baseline.get('cache_bytes')) is not int or not fixture.expected_cache_bytes()<baseline['cache_bytes']<fixture.expected_cache_bytes()+18*1024:raise SuiteError('per-case cache byte bound differs')
        env['ADDP_ONLINE_PROXIMITY_ORTHOGONAL_ORACLE_ANCHOR_SHA256']=anchor
        immutable(directory/'proximity_orthogonal-oracle-preparation.json',encoded(baseline))
        check_physical(baseline,baseline.get('sources'),{},scope,[])
        for name in fixture.SOURCES:
            if type(before[name].get('size_bytes')) is not int or before[name]['size_bytes'] != baseline['sources'][name]['size_bytes']: raise SuiteError('physical/Meta source size differs')
        if base.scan_ids(client) != [scan]: raise SuiteError('unexpected source initialization scan')
        target_metadata_before={}
        physical_calls, full_checks = 1, 0; cases, failures, names, scans, previous = [], [], [], [], {}
        for index, name in enumerate(fixture.CASES, 1):
            stages.append({'case': name, 'status': 'running'}); progress('running')
            language = 'zh-cn' if index % 2 else 'en'; dag = definition(sources, parent, name); target = parent.replace('/outputs?type=directory', '/outputs/'+name+'?type=file')
            identifier = raster.submit(SubmittedLanguageClient(client, language), runtime, dag, execution_timeout=min(180, max(1, int(remaining()))))
            execution = raster.wait_execution(client, 'develop', identifier, remaining()); current(execution, identity, identifier, dag, runtime, language, 'success')
            wanted = expected_lineage(sources, name, target)
            evidence = lineage.collect(env, SUITE, identifier, name, wanted, execution, tenant, int(identity['principal_id']), dag, lineage_reader); remaining()
            expectation=fixture.artifact_expectation(name)
            facts,output_scan=validate_success(execution,[sources[n] for n in case_inputs(name)],target,'create',expectation,resource_type='file')
            lineage.validate(facts,wanted,public=True)
            if output_scan in scans: raise SuiteError('duplicate automatic COG scan')
            scans.append(output_scan)
            scan_identity = base.validate_scan(raster.wait_execution(client, 'meta', output_scan, remaining()), output_scan, identifier, tenant, int(identity['principal_id']), engine)
            monitor = client.request('GET', '/api/v1/monitor/executions/by-execution-id/'+identifier, (200,)).payload
            readonly.actor(monitor, identity, 'develop')
            if monitor.get('execution_id') != identifier or type(monitor.get('tenant_id')) is not int or monitor.get('tenant_id') != tenant or monitor.get('status') != 'success' or obj(monitor.get('metadata'), 'metadata').get('lineage_facts') != facts: raise SuiteError('Monitor current output lineage differs')
            names.append(name); native = physical_runner(repository, env, 'verify', names, remaining()); remaining()
            check_physical(native,baseline['sources'],previous,scope,names,baseline['archived_expectations']); previous = copy.deepcopy(native['targets'])
            physical_calls += 1; full_checks += len(names)
            item0 = support.find_fixture_item(client, engine, 'outputs/'+name, 'COG')
            item = client.request('GET', f'/api/v1/meta/items/{item0["id"]}', (200,)).payload
            validate_target_item(item,expectation,native['targets'][name])
            artifact=obj(obj(execution['metadata'].get('result'),'result').get('final_result'),'artifact')
            size=raster.strict_sizes(artifact,native['targets'][name],item,name)
            target_metadata_before[name]=copy.deepcopy(item)
            cases.append({'name': name, 'execution_id': identifier, 'target_item_id': item['id'], 'target_locator': target,
                          'automatic_scan_execution_id': output_scan, 'scan_identity': scan_identity, 'size_evidence': size, 'lineage_evidence': evidence, 'physical': native['targets'][name]})
            stages[-1]['status'] = 'passed'; progress('running')
        for case in cases:
            case['lineage'] = [raster.wait_lineage(client, items[name]['id'], case['target_item_id'], case['execution_id'], remaining()) for name in case_inputs(case['name'])]
        if base.scan_ids(client) != sorted([scan, *scans]): raise SuiteError('unexpected automatic scan inventory')
        edges = {name: source_edges(client, item['id']) for name, item in items.items()}
        for problem in FAILURES:
            for language in ('zh-cn', 'en'):
                stages.append({'problem': problem, 'language': language, 'status': 'running'}); progress('running')
                dag = definition(sources, parent, 'wide.cog.tif', invalid=problem, language=language)
                identifier = raster.submit(SubmittedLanguageClient(client, language), runtime, dag, execution_timeout=min(180, max(1, int(remaining()))))
                execution = raster.wait_execution(client, 'develop', identifier, remaining(), expected='failed'); current(execution, identity, identifier, dag, runtime, language, 'failed')
                if obj(execution.get('error_details'), 'error details').get('category') != 'execution_failed': raise SuiteError('wrong failure category')
                diagnostic = diagnostic_runner(repository, env, identifier, problem, language, remaining()); remaining()
                if diagnostic != {'execution_id': identifier, 'task': 'distance', 'problem': problem, 'language': language, 'exact_cause': MESSAGES[problem][language == 'en'], 'source': 'develop_runtime_log'}: raise SuiteError('wrong exact failure task/cause')
                readonly.no_publication(execution)
                monitor = client.request('GET', '/api/v1/monitor/executions/by-execution-id/'+identifier, (200,)).payload
                readonly.actor(monitor, identity, 'develop'); readonly.no_publication(monitor)
                if monitor.get('execution_id') != identifier or type(monitor.get('tenant_id')) is not int or monitor.get('tenant_id') != tenant or monitor.get('status') != 'failed' or obj(monitor.get('error_details'), 'Monitor error').get('category') != 'execution_failed': raise SuiteError('Monitor failed identity/category differs')
                queried = SubmittedLanguageClient(client, 'en' if language == 'zh-cn' else 'zh-cn').request('GET', '/api/v1/develop/executions/'+identifier, (200,)).payload
                if any(queried.get(key) != execution.get(key) for key in ('error_msg', 'error_details', 'execution_config')): raise SuiteError('query language rewrote stored failure')
                if base.scan_ids(client) != sorted([scan, *scans]): raise SuiteError('failed execution added scan')
                if any(source_edges(client, item['id']) != edges[name] for name, item in items.items()): raise SuiteError('failed execution added derived lineage')
                native = physical_runner(repository, env, 'verify', names, remaining()); remaining(); check_physical(native,baseline['sources'],previous,scope,names,baseline['archived_expectations'])
                physical_calls += 1; full_checks += len(names)
                failures.append({'problem': problem, 'language': language, 'execution_id': identifier, 'diagnostic': diagnostic, 'no_publication_or_scan_or_lineage': True})
                stages[-1]['status'] = 'passed'; progress('running')
        if len(cases) != 18 or len(failures) != 60 or full_checks != 1251 or physical_calls != 79: raise SuiteError('all 18 successes, 60 failures, 1251 COG checks and 79 physical calls required')
        for name, item in items.items():
            if client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload != before[name]: raise SuiteError('source metadata changed')
        source_metadata_after={n:client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload for n,item in items.items()}
        target_metadata_after={case['name']:client.request('GET',f'/api/v1/meta/items/{case["target_item_id"]}',(200,)).payload for case in cases}
        if source_metadata_after != before or target_metadata_after != target_metadata_before:raise SuiteError('source or previous target Meta changed')
        immutable(directory/'proximity_orthogonal-meta-snapshots.json',encoded({'source_before':before,'source_after':source_metadata_after,'target_before':target_metadata_before,'target_after':target_metadata_after}))
        lineage.verify_archive(env, SUITE, cases); remaining()
        report = {'schema_version': 'addp.'+SUITE+'/v1', 'suite': SUITE, 'result': 'passed', 'identity': identity,
                  'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'sources': sources, 'runtime_engine_id': runtime,
                  'source_scan_execution_id': scan, 'source_scan_identity': source_scan_identity,
                  'cases': cases, 'failed_parameters': failures, 'full_target_checks': full_checks, 'physical_verification_calls': physical_calls,
                  'source_metadata_before':before,'source_metadata_after':source_metadata_after,'target_metadata_before':target_metadata_before,'target_metadata_after':target_metadata_after, 'physical': native, 'archived_cogs': native['archived_cogs'], 'source_unchanged': True, 'oracle_scope_sha256':scope,'oracle_anchor_sha256':anchor}
    except BaseException as error: primary = error
    finally:
        try:
            removed = physical_runner(repository, env, 'cleanup-oracle', [], remaining()); remaining()
            if removed.get('oracle_cache_removed') is not True: raise SuiteError('oracle cleanup proof missing')
        except BaseException as error: cleanup_error = error
    if primary is not None:
        progress('failed')
        if cleanup_error is not None: raise SuiteError(str(primary)+'; oracle cleanup also failed: '+type(cleanup_error).__name__) from primary
        raise primary
    if cleanup_error is not None: progress('failed'); raise cleanup_error
    report['oracle_cache_removed'] = True; remaining(); progress('passed'); remaining()
    report.update(elapsed_ms=round((time.monotonic()-started)*1000), business_budget_ms=600000)
    if report['elapsed_ms'] > 600000: raise SuiteError('business budget exceeded')
    return report

def main():
    env = dict(os.environ)
    report = run_scenario(Path(__file__).resolve().parents[2], env, DiagnosticClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30, env['ADDP_ONLINE_ARTIFACT_DIR']))
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'proximity_orthogonal-persistence.json').write_bytes(encoded(report)); print(json.dumps(report, ensure_ascii=False, sort_keys=True))

if __name__ == '__main__':
    try: main()
    except Exception as error:
        print(f'Rectangular proximity persistence acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr); raise SystemExit(1)
