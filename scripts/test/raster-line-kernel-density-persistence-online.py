#!/usr/bin/env python3
"""Twenty-four complete COGs, 60 exact failures, 1740 real COG rereads, fixed 600s."""
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
fixture = importlib.import_module('business.scripts.online-raster-line-kernel-density-fixture')
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
MESSAGE_GROUPS = {
 'method':('线密度方法必须为uniform或quartic','Line density method must be uniform or quartic'),
 'weight_field':('线密度权重字段必须显式选择现有非几何数值列名','Line density weight field must name an existing non-geometry numeric column'),
 'weight':('线密度权重必须为有限非布尔非负实数，缺失值不可跳过','Line density weights must be finite non-boolean nonnegative real numbers; missing values cannot be skipped'),
 'radius': ('线密度搜索半径必须为有限非布尔正米数','Line density search radius must be a finite positive number of metres, excluding booleans'),
 'geometry': ('线密度仅接受非空、有效、有限XY且每个分量具有正长度的二维LineString或MultiLineString','Line density requires non-empty valid finite XY two-dimensional LineStrings or MultiLineStrings with positive length in every component'),
 'crs': ('线密度线与模板必须具有相同的明确二维投影CRS及相同已知水平线性单位','Line density lines and template must share an explicit two-dimensional projected CRS with equal known horizontal linear units'),
}
FIELD_FAILURES={'field_null':None,'field_empty':'','field_blank':' ','field_bool':True,'field_number':1,'field_array':[],'field_object':{},'field_missing':'absent','field_geometry':'shape'}
METHOD_FAILURES={'method_null': None, 'method_true': True, 'method_false': False, 'method_number': 1, 'method_list': [], 'method_object': {}, 'method_empty': '', 'method_blank': ' ', 'method_case': 'Quartic', 'method_unknown': 'gaussian'}
MESSAGES={p:MESSAGE_GROUPS['method'if p.startswith('method_')else'weight_field'if p.startswith('field_')else'weight'if p in('missing','negative','string','outside_missing','outside_negative')else'crs'if p=='zero_crs'else'radius'if p=='zero_radius'else'geometry']for p in FAILURES}

class DiagnosticClient(base.DiagnosticClient):
    diagnostic_name = 'raster-line-kernel-density-submission-error.json'
    diagnostic_schema = 'addp.raster-line-kernel-density-submission-diagnostic/v1'

def case_inputs(name):return [fixture.CASES[name][1],fixture.CASES[name][0]]
def definition(sources,parent,name,*,invalid=None,language='zh-cn'):
 if name not in fixture.CASES or invalid not in(None,*FAILURES)or language not in('zh-cn','en')or(invalid and name!='mixed.cog.tif'):raise SuiteError('unknown exact weighted line-density case')
 template,lines,radius,field,method=fixture.CASES[name]
 if invalid in FAILURES and invalid+'.gpkg'in fixture.VECTORS:lines=invalid+'.gpkg'
 if invalid=='zero_radius':radius,field=0,'zero'
 tasks=[{'id':'reference','operator':'raster_load','depends_on':[],'params':{'locator':sources[template]}},{'id':'lines','operator':'load','depends_on':[],'params':{'locator':sources[lines]}}]
 params={'input_gdf':{'$ref':'lines'},'reference_raster':{'$ref':'reference'},'radius_metres':radius}
 if method is not None:params['method']=method
 if invalid in METHOD_FAILURES:params['method']=METHOD_FAILURES[invalid]
 if field is not None:params['weight_field']=field
 if invalid in FIELD_FAILURES:params['weight_field']=FIELD_FAILURES[invalid]
 tasks.append({'id':'density','operator':'raster_line_density','depends_on':['reference','lines'],'params':params})
 tasks.append({'id':'save','operator':'raster_save','depends_on':['density'],'params':{'input_raster':{'$ref':'density'},'target_parent_locator':parent,'target_name':f'failed-{invalid}-{language}.cog.tif'if invalid else name,'write_mode':'create','profile':'cog','blocksize':128,'overview_resampling':'nearest'}})
 return {'tasks':tasks}

def expected_lineage(sources,name,target):
 inputs=[{'port':'input.'+role,'locator':sources[source]}for role,source in zip(('lines','reference'),case_inputs(name))]
 return {'schema_version':'addp.lineage-facts/v1','inputs':inputs,'outputs':[{'port':'output.save','locator':target,'write_mode':'create'}],
 'operations':[{'kind':'derive','operator':'raster_save','input_ports':[v['port']for v in inputs],'output_ports':['output.save']}]}

validate_success = raster.validate_success
validate_target_item = raster.validate_target_item

artifact_expectation = fixture.artifact_expectation

def check_physical(value,sources,previous,anchor,names):
 if (value.get('runtime_workspaces_removed')is not True or value.get('full_source_pixels_verified')is not True or value.get('full_source_vectors_verified')is not True or value.get('sources')!=sources or set(obj(value.get('targets'),'targets'))!=set(names) or type(value.get('full_cog_checks'))is not int or value['full_cog_checks']!=len(names) or value.get('oracle_sha256')!=anchor):raise SuiteError('complete source/COG/count/cleanup/oracle proof differs')
 if value.get('actual_control_pairs_verified')!=[list(pair)for pair in fixture.CONTROLS if all(n in names for n in pair)]:raise SuiteError('actual point-order or template-isolation comparisons missing')
 if value.get('actual_scale_controls_verified')!=[list(v)for v in fixture.SCALE_CONTROLS if all(n in names for n in v[:2])]:raise SuiteError('actual doubled and retraced density controls missing')
 if value.get('actual_method_controls_verified')!=[list(v)for v in fixture.METHOD_CONTROLS if all(n in names for n in v)]:raise SuiteError('actual uniform/quartic witness missing')
 oracle=obj(value.get('archived_oracle'),'original oracle')
 if set(oracle)!={fixture.ORACLE}or oracle[fixture.ORACLE].get('sha256')!=anchor or oracle[fixture.ORACLE].get('path')!='raster-line-kernel-density-oracle/'+fixture.ORACLE or type(oracle[fixture.ORACLE].get('size_bytes'))is not int or oracle[fixture.ORACLE]['size_bytes']<=0:raise SuiteError('frozen independent oracle archive differs')
 archives=obj(value.get('archived_cogs'),'original COGs')
 if set(archives)!=set(names)or set(obj(value.get('archived_sources'),'original sources'))!=set(fixture.SOURCES):raise SuiteError('archive inventory differs')
 for n,fact in sources.items():
  if type(fact.get('size_bytes'))is not int or fact['size_bytes']<=0 or not isinstance(fact.get('sha256'),str)or not re.fullmatch('[0-9a-f]{64}',fact['sha256'])or value['archived_sources'][n]!={'path':'raster-line-kernel-density-sources/'+n,**fact}:raise SuiteError('source original size/hash differs')
 flags=('cog_valid','full_pixel_and_mask_verification','independent_scalar_oracle_verified','template_mask_ignored_verified','nearest_overviews_verified','legal_zero_verified','line_length_and_full_circle_area_verified','line_density_units_verified','unnormalized_weights_verified','line_density_method_verified')
 for n,fact in value['targets'].items():
  if fact.get('method')!=(fixture.CASES[n][4]or'uniform')or fact.get('method_parameter_omitted')is not(fixture.CASES[n][4]is None):raise SuiteError('method selector/omission proof differs')
  if fact.get('weight_field')!=fixture.CASES[n][3]or fact.get('weight_field_parameter_omitted')is not(fixture.CASES[n][3]is None)or fact.get('unit')!=fixture.output_unit(n):raise SuiteError('weighted field/omission/unit proof differs')
  if any(fact.get(k)is not True for k in flags)or type(fact.get('size_bytes'))is not int or fact['size_bytes']<=0 or not isinstance(fact.get('sha256'),str)or not re.fullmatch('[0-9a-f]{64}',fact['sha256'])or type(fact.get('overview_count'))is not int or fact['overview_count']!=fixture.overview_count(n) or fact.get('has_overviews')is not bool(fixture.overview_count(n)) or archives[n]!={'path':'raster-line-kernel-density-cogs/'+n,**{k:fact[k]for k in('size_bytes','sha256')}}:raise SuiteError('complete original COG/overview/size/hash differs')
  if n in previous and fact!=previous[n]:raise SuiteError('previous complete COG changed')

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
                    and message.endswith('任务 density 执行失败: '+cause)):
                    return {'execution_id': identifier, 'task': 'density', 'problem': problem, 'language': language, 'exact_cause': cause, 'source': 'develop_runtime_log'}
        time.sleep(min(1, max(0, deadline-time.monotonic())))
    raise SuiteError('missing exact line density task and cause')

def fixture_call(repository, env, action, names, timeout):
    if action not in ('oracle', 'verify', 'cleanup-oracle'): raise SuiteError('unknown oracle action')
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-line-kernel-density-fixture.py', action, *names],
                            cwd=repository, env=dict(env), capture_output=True, text=True, timeout=min(150, timeout))
    if result.returncode: raise SuiteError('independent line density COG verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'line density physical evidence')

def current(execution, identity, identifier, dag, runtime, language, status):
    readonly.actor(execution, identity, 'develop'); readonly.execution_snapshot(execution, dag, runtime)
    if (execution.get('execution_id') != identifier or type(execution.get('tenant_id')) is not int
        or execution['tenant_id'] != int(identity['tenant_id']) or execution.get('task_type') != 'workflow'
        or execution.get('status') != status or execution['execution_config'].get('display_language') != language): raise SuiteError('current execution identity/status/language differs')

def run_scenario(repository, env, client, physical_runner=fixture_call, diagnostic_runner=invalid_diagnostic, lineage_reader=lineage.read_current):
    env = dict(env); started = time.monotonic(); budget = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    if not math.isfinite(budget) or budget != 600: raise SuiteError('line density business budget must be exactly 600 seconds')
    def remaining():
        left = budget-(time.monotonic()-started)
        if left <= 0: raise SuiteError('line density business budget exhausted')
        return left
    client = BudgetClient(client, remaining); directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR']); directory.mkdir(parents=True, exist_ok=True); (directory/'line-kernel-density-terminal-responses').mkdir()
    stages = []; primary = cleanup_error = report = None
    def progress(status):
        (directory/'raster-line-kernel-density-progress.json').write_bytes(encoded({'suite': SUITE, 'result': status,
            'budget_ms': 600000, 'elapsed_ms': round((time.monotonic()-started)*1000), 'stages': stages}))
    try:
        tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
        if tenant <= 1: raise SuiteError('line density acceptance forbids default tenant')
        identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
        engine = positive(env['ADDP_ONLINE_RASTER_LINE_KERNEL_DENSITY_NFS_ENGINE_ID'], 'NFS engine')
        scopes = {name: f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
        scan = support.wait_for_meta_scan(client, engine, time.monotonic()+remaining(), scope={'targets': list(scopes.values())})
        items = {name: support.find_fixture_item(client, engine, name, 'line density source') for name in fixture.SOURCES}
        sources = {name: scopes[name]+f'&item_id={items[name]["id"]}' for name in fixture.SOURCES}
        before = {name: client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload for name, item in items.items()}
        immutable(directory/'source-meta-before.json',encoded(before))
        source_scan_identity = base.validate_scan(raster.wait_execution(client, 'meta', scan, remaining()), scan, None, tenant, int(identity['principal_id']), engine)
        selected = [v for v in array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'runtimes')
                    if v.get('engine_type') == 'geopython_workflow' and v.get('connection_status') == 'online']
        if len(selected) != 1: raise SuiteError('exactly one same-source product Runtime is required')
        runtime = positive(selected[0].get('id'), 'Runtime'); parent = f'addp://engine/{engine}/path/outputs?type=directory'
        baseline = physical_runner(repository, env, 'oracle', [], remaining()); remaining()
        anchor = baseline.get('oracle_sha256'); expected = obj(baseline.get('expectations'), 'independent expectations')
        if set(expected) != set(fixture.CASES) or not isinstance(anchor, str) or not re.fullmatch('[0-9a-f]{64}', anchor): raise SuiteError('sixteen frozen independent expectations required')
        expected = copy.deepcopy(expected); env['ADDP_ONLINE_RASTER_LINE_KERNEL_DENSITY_ORACLE_SHA256'] = anchor
        immutable(directory/'raster-line-kernel-density-expectations.json', encoded(expected))
        check_physical(baseline, baseline.get('sources'), {}, anchor, [])
        for name in fixture.SOURCES:
            if type(before[name].get('size_bytes')) is not int or before[name]['size_bytes'] != baseline['sources'][name]['size_bytes']: raise SuiteError('physical/Meta source size differs')
        if base.scan_ids(client) != [scan]: raise SuiteError('unexpected source initialization scan')
        physical_calls, full_checks = 1, 0; cases, failures, names, scans, previous = [], [], [], [], {}
        for index, name in enumerate(fixture.CASES, 1):
            stages.append({'case': name, 'status': 'running'}); progress('running')
            language = 'zh-cn' if index % 2 else 'en'; dag = definition(sources, parent, name); target = parent.replace('/outputs?type=directory', '/outputs/'+name+'?type=file')
            identifier = raster.submit(SubmittedLanguageClient(client, language), runtime, dag, execution_timeout=min(180, max(1, int(remaining()))))
            execution = raster.wait_execution(client, 'develop', identifier, remaining()); current(execution, identity, identifier, dag, runtime, language, 'success')
            wanted = expected_lineage(sources, name, target)
            evidence = lineage.collect(env, SUITE, identifier, name, wanted, execution, tenant, int(identity['principal_id']), dag, lineage_reader); remaining()
            expectation=artifact_expectation(name)
            facts,output_scan=validate_success(execution,[sources[n]for n in case_inputs(name)],target,'create',expectation,resource_type='file')
            lineage.validate(facts,wanted,public=True)
            if output_scan in scans:raise SuiteError('duplicate automatic COG scan')
            scans.append(output_scan)
            scan_identity=base.validate_scan(raster.wait_execution(client,'meta',output_scan,remaining()),output_scan,identifier,tenant,int(identity['principal_id']),engine)
            monitor_response=client.request('GET','/api/v1/monitor/executions/by-execution-id/'+identifier,(200,));monitor=monitor_response.payload
            readonly.actor(monitor,identity,'develop')
            if monitor.get('execution_id')!=identifier or type(monitor.get('tenant_id'))is not int or monitor.get('tenant_id')!=tenant or monitor.get('status')!='success'or obj(monitor.get('metadata'),'metadata').get('lineage_facts')!=facts:raise SuiteError('Monitor current output lineage differs')
            archive_response(directory/'line-kernel-density-terminal-responses',f'{index:02}-develop',client.request('GET','/api/v1/develop/executions/'+identifier,(200,)))
            archive_response(directory/'line-kernel-density-terminal-responses',f'{index:02}-monitor',monitor_response)
            names.append(name);native=physical_runner(repository,env,'verify',names,remaining());remaining()
            check_physical(native,baseline['sources'],previous,anchor,names);previous=copy.deepcopy(native['targets']);physical_calls+=1;full_checks+=len(names)
            item0=support.find_fixture_item(client,engine,'outputs/'+name,'COG');item=client.request('GET',f'/api/v1/meta/items/{item0["id"]}',(200,)).payload
            validate_target_item(item,expectation,native['targets'][name])
            artifact=obj(obj(execution['metadata'].get('result'),'result').get('final_result'),'artifact');sizes=raster.strict_sizes(artifact,native['targets'][name],item,name)
            immutable(directory/('target-'+name+'-meta-before.json'),encoded(item))
            cases.append({'name':name,'execution_id':identifier,'target_item_id':item['id'],'target_locator':target,'scan_identity':scan_identity,'size_evidence':sizes,'lineage_evidence':evidence,'physical':native['targets'][name]})
            stages[-1]['status'] = 'passed'; progress('running')
        for case in cases:
            case['lineage'] = [raster.wait_lineage(client, items[name]['id'], case['target_item_id'], case['execution_id'], remaining()) for name in case_inputs(case['name'])]
        if base.scan_ids(client) != sorted([scan, *scans]): raise SuiteError('unexpected automatic scan inventory')
        edges = {name: source_edges(client, item['id']) for name, item in items.items()}
        for problem in FAILURES:
            for language in ('zh-cn', 'en'):
                stages.append({'problem': problem, 'language': language, 'status': 'running'}); progress('running')
                dag = definition(sources, parent, 'mixed.cog.tif', invalid=problem, language=language)
                identifier = raster.submit(SubmittedLanguageClient(client, language), runtime, dag, execution_timeout=min(180, max(1, int(remaining()))))
                execution = raster.wait_execution(client, 'develop', identifier, remaining(), expected='failed'); current(execution, identity, identifier, dag, runtime, language, 'failed')
                if obj(execution.get('error_details'), 'error details').get('category') != 'execution_failed': raise SuiteError('wrong failure category')
                diagnostic = diagnostic_runner(repository, env, identifier, problem, language, remaining()); remaining()
                if diagnostic != {'execution_id': identifier, 'task': 'density', 'problem': problem, 'language': language, 'exact_cause': MESSAGES[problem][language == 'en'], 'source': 'develop_runtime_log'}: raise SuiteError('wrong exact failure task/cause')
                archive_response(directory/'line-kernel-density-terminal-responses',f'failed-{problem}-{language}-develop',client.request('GET','/api/v1/develop/executions/'+identifier,(200,)))
                readonly.no_publication(execution)
                monitor = client.request('GET', '/api/v1/monitor/executions/by-execution-id/'+identifier, (200,)).payload
                archive_response(directory/'line-kernel-density-terminal-responses',f'failed-{problem}-{language}-monitor',client.request('GET','/api/v1/monitor/executions/by-execution-id/'+identifier,(200,)))
                readonly.actor(monitor, identity, 'develop'); readonly.no_publication(monitor)
                if monitor.get('execution_id') != identifier or type(monitor.get('tenant_id')) is not int or monitor.get('tenant_id') != tenant or monitor.get('status') != 'failed' or obj(monitor.get('error_details'), 'Monitor error').get('category') != 'execution_failed': raise SuiteError('Monitor failed identity/category differs')
                queried = SubmittedLanguageClient(client, 'en' if language == 'zh-cn' else 'zh-cn').request('GET', '/api/v1/develop/executions/'+identifier, (200,)).payload
                if any(queried.get(key) != execution.get(key) for key in ('error_msg', 'error_details', 'execution_config')): raise SuiteError('query language rewrote stored failure')
                if base.scan_ids(client) != sorted([scan, *scans]): raise SuiteError('failed execution added scan')
                if any(source_edges(client, item['id']) != edges[name] for name, item in items.items()): raise SuiteError('failed execution added derived lineage')
                native = physical_runner(repository, env, 'verify', names, remaining()); remaining(); check_physical(native, baseline['sources'], previous, anchor, names)
                physical_calls += 1; full_checks += len(names)
                failures.append({'problem': problem, 'language': language, 'execution_id': identifier, 'diagnostic': diagnostic, 'no_publication_or_scan_or_lineage': True})
                stages[-1]['status'] = 'passed'; progress('running')
        if len(cases) != 24 or len(failures) != 60 or full_checks != 1740 or physical_calls != 85: raise SuiteError('all 24 successes, 60 failures, 1740 COG checks and 85 physical calls required')
        target_metadata_before={case['target_item_id']:json.loads((directory/('target-'+case['name']+'-meta-before.json')).read_bytes())for case in cases}
        target_metadata_after={identifier:client.request('GET',f'/api/v1/meta/items/{identifier}',(200,)).payload for identifier in target_metadata_before}
        immutable(directory/'target-meta-after.json',encoded(target_metadata_after))
        if target_metadata_after != target_metadata_before:raise SuiteError('target metadata changed')
        source_metadata_after={name:client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload for name,item in items.items()}
        immutable(directory/'source-meta-after.json',encoded(source_metadata_after))
        if source_metadata_after != before:raise SuiteError('source metadata changed')
        lineage.verify_archive(env, SUITE, cases); remaining()
        immutable(directory/'raster-line-kernel-density-expectations.json', encoded(expected))
        report = {'schema_version': 'addp.'+SUITE+'/v1', 'suite': SUITE, 'result': 'passed', 'identity': identity,
                  'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'sources': sources, 'runtime_engine_id': runtime,
                  'source_scan_execution_id': scan, 'source_scan_identity': source_scan_identity,
                  'cases': cases, 'failed_parameters': failures, 'full_target_checks': full_checks, 'physical_verification_calls': physical_calls,
                  'physical': native, 'archived_cogs': native['archived_cogs'], 'source_unchanged': True,'source_meta_unchanged':True,'target_meta_unchanged':True,'oracle_sha256': anchor}
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
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'raster-line-kernel-density-persistence.json').write_bytes(encoded(report)); print(json.dumps(report, ensure_ascii=False, sort_keys=True))

if __name__ == '__main__':
    try: main()
    except Exception as error:
        print(f'line density persistence acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr); raise SystemExit(1)
