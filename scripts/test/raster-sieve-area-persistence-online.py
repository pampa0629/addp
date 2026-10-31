#!/usr/bin/env python3
"""Sixteen original COGs, 64 accurate failures, 1160 full output rereads, fixed 600s."""
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
fixture = importlib.import_module('business.scripts.online-raster-sieve-area-fixture')
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
MESSAGES={'threshold_missing': ('小斑块筛除的min_pixels与min_area_m2必须且只能提供一个', 'Raster sieve requires exactly one of min_pixels or min_area_m2'), 'threshold_both': ('小斑块筛除的min_pixels与min_area_m2必须且只能提供一个', 'Raster sieve requires exactly one of min_pixels or min_area_m2'), 'threshold_pixel_null': ('小斑块筛除的min_pixels与min_area_m2必须且只能提供一个', 'Raster sieve requires exactly one of min_pixels or min_area_m2'), 'threshold_area_null_with_pixel': ('小斑块筛除的min_pixels与min_area_m2必须且只能提供一个', 'Raster sieve requires exactly one of min_pixels or min_area_m2'), 'area_null': ('小斑块筛除的min_area_m2必须为有限正数，单位为平方米', 'Raster sieve min_area_m2 must be a finite positive number in square metres'), 'area_boolean': ('小斑块筛除的min_area_m2必须为有限正数，单位为平方米', 'Raster sieve min_area_m2 must be a finite positive number in square metres'), 'area_zero': ('小斑块筛除的min_area_m2必须为有限正数，单位为平方米', 'Raster sieve min_area_m2 must be a finite positive number in square metres'), 'area_negative': ('小斑块筛除的min_area_m2必须为有限正数，单位为平方米', 'Raster sieve min_area_m2 must be a finite positive number in square metres'), 'area_string': ('小斑块筛除的min_area_m2必须为有限正数，单位为平方米', 'Raster sieve min_area_m2 must be a finite positive number in square metres'), 'area_array': ('小斑块筛除的min_area_m2必须为有限正数，单位为平方米', 'Raster sieve min_area_m2 must be a finite positive number in square metres'), 'area_object': ('小斑块筛除的min_area_m2必须为有限正数，单位为平方米', 'Raster sieve min_area_m2 must be a finite positive number in square metres'), 'grid_geographic': ('小斑块筛除面积模式需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影', 'Raster sieve area mode requires a projected north-up grid with known linear units; reproject explicitly'), 'grid_no_crs': ('小斑块筛除面积模式需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影', 'Raster sieve area mode requires a projected north-up grid with known linear units; reproject explicitly'), 'grid_no_transform': ('小斑块筛除面积模式需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影', 'Raster sieve area mode requires a projected north-up grid with known linear units; reproject explicitly'), 'grid_rotated_x': ('小斑块筛除面积模式需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影', 'Raster sieve area mode requires a projected north-up grid with known linear units; reproject explicitly'), 'grid_rotated_y': ('小斑块筛除面积模式需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影', 'Raster sieve area mode requires a projected north-up grid with known linear units; reproject explicitly'), 'grid_unknown_unit': ('小斑块筛除面积模式需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影', 'Raster sieve area mode requires a projected north-up grid with known linear units; reproject explicitly'), 'grid_compound': ('小斑块筛除面积模式需要已知线性单位的二维投影、北向无旋转网格，请先显式重投影', 'Raster sieve area mode requires a projected north-up grid with known linear units; reproject explicitly'), 'band': ('小斑块筛除必须显式选择有效的一基数据波段', 'Raster sieve requires an explicit valid one-based data band'), 'alpha': ('小斑块筛除不能选择alpha作为数据波段', 'Raster sieve cannot use an alpha band as data'), 'complex': ('小斑块筛除不支持复数数据或复数alpha', 'Raster sieve does not support complex data or alpha'), 'connectivity_value': ('小斑块筛除必须显式选择整数4或8邻接', 'Raster sieve requires explicit integer connectivity 4 or 8'), 'connectivity_fractional': ('小斑块筛除必须显式选择整数4或8邻接', 'Raster sieve requires explicit integer connectivity 4 or 8'), 'connectivity_boolean': ('小斑块筛除必须显式选择整数4或8邻接', 'Raster sieve requires explicit integer connectivity 4 or 8'), 'fractional': ('小斑块筛除的有效分类必须为安全范围内的精确整数', 'Raster sieve valid classes must be exact integers within the safe range'), 'unsafe': ('小斑块筛除的有效分类必须为安全范围内的精确整数', 'Raster sieve valid classes must be exact integers within the safe range'), 'int64': ('小斑块筛除的有效分类必须为安全范围内的精确整数', 'Raster sieve valid classes must be exact integers within the safe range'), 'uint64': ('小斑块筛除的有效分类必须为安全范围内的精确整数', 'Raster sieve valid classes must be exact integers within the safe range'), 'protected_null': ('小斑块筛除的protected_values必须为不重复的安全整数列表，可为空', 'Raster sieve protected_values must be a list of distinct exact safe integers, possibly empty'), 'protected_item_object': ('小斑块筛除的protected_values必须为不重复的安全整数列表，可为空', 'Raster sieve protected_values must be a list of distinct exact safe integers, possibly empty'), 'protected_duplicate': ('小斑块筛除的protected_values必须为不重复的安全整数列表，可为空', 'Raster sieve protected_values must be a list of distinct exact safe integers, possibly empty'), 'protected_positive_unsafe': ('小斑块筛除的protected_values必须为不重复的安全整数列表，可为空', 'Raster sieve protected_values must be a list of distinct exact safe integers, possibly empty')}

class DiagnosticClient(base.DiagnosticClient):
    diagnostic_name='sieve-area-submission-error.json'
    diagnostic_schema='addp.raster-sieve-area-submission-diagnostic/v1'

def case_inputs(name):return [fixture.CASES[name][0]]

def definition(sources,parent,name,*,invalid=None,language='zh-cn'):
    if language not in('zh-cn','en')or name not in fixture.TARGETS or invalid not in(None,*FAILURES)or(invalid and name!='area-four.cog.tif'):raise SuiteError('unknown exact area sieve case')
    source,minimum,connectivity,protection,stage=fixture.CASES[name];band=2
    if invalid:
        source={'fractional':'fractional.tif','unsafe':'unsafe.tif','int64':'int64.tif','uint64':'uint64.tif','complex':'complex.tif',
                'grid_geographic':'geographic.tif','grid_no_crs':'no-crs.tif','grid_no_transform':'no-transform.tif','grid_rotated_x':'rotated-x.tif',
                'grid_rotated_y':'rotated-y.tif','grid_unknown_unit':'unknown-unit.tif','grid_compound':'compound.tif'}.get(invalid,source)
        if invalid=='band':band=0
        elif invalid=='alpha':band=4
        if invalid.startswith('connectivity_'):connectivity={'connectivity_value':6,'connectivity_fractional':4.5,'connectivity_boolean':True}[invalid]
    tasks=[{'id':'source','operator':'raster_load','depends_on':[],'params':{'locator':sources[source]}}];prior='source'
    if stage=='reclassify':
        tasks.append({'id':'classes','operator':'raster_reclassify','depends_on':['source'],'params':{'input_raster':{'$ref':'source'},'band':2,'rules':fixture.class_rules()}});prior='classes';band=1
    params={'input_raster':{'$ref':prior},'band':band,'connectivity':connectivity,'min_pixels'if stage=='pixels'else'min_area_m2':minimum}
    if protection is not None:params['protected_values']=list(protection)
    if invalid=='threshold_missing':params.pop('min_area_m2')
    elif invalid in('threshold_both','threshold_pixel_null'):params['min_pixels']=None if invalid=='threshold_pixel_null'else 5
    elif invalid=='threshold_area_null_with_pixel':params.update(min_pixels=5,min_area_m2=None)
    elif invalid and invalid.startswith('area_'):
        params['min_area_m2']={'area_null':None,'area_boolean':True,'area_zero':0,'area_negative':-1,'area_string':'30','area_array':[],'area_object':{}}[invalid]
    elif invalid and invalid.startswith('protected_'):
        params['protected_values']={'protected_null':None,'protected_item_object':[{}],'protected_duplicate':[9,9],'protected_positive_unsafe':[2**53]}[invalid]
    tasks.extend([{'id':'sieve','operator':'raster_sieve','depends_on':[prior],'params':params},
        {'id':'save','operator':'raster_save','depends_on':['sieve'],'params':{'input_raster':{'$ref':'sieve'},'target_parent_locator':parent,'target_name':f'failed-{invalid}-{language}.cog.tif'if invalid else name,'write_mode':'create','profile':'cog','blocksize':128,'overview_resampling':'nearest'}}])
    return {'tasks':tasks}

def expected_lineage(sources,name,target):
    return {'schema_version':'addp.lineage-facts/v1','inputs':[{'port':'input.source','locator':sources[case_inputs(name)[0]]}],
        'outputs':[{'port':'output.save','locator':target,'write_mode':'create'}],
        'operations':[{'kind':'derive','operator':'raster_save','input_ports':['input.source'],'output_ports':['output.save']}]}

validate_success=raster.validate_success
validate_target_item=raster.validate_target_item

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
        if set(fact)!={'path','size_bytes','sha256'} or fact['path']!='sieve-area-expectations/'+name+'.npy' or type(fact['size_bytes']) is not int or not size<fact['size_bytes']<size+1024 or not isinstance(fact['sha256'],str) or not re.fullmatch('[0-9a-f]{64}',fact['sha256']):raise SuiteError('independent array size/hash differs')
    archives=obj(value.get('archived_cogs'),'original COGs')
    if set(archives)!=set(names) or set(obj(value.get('archived_sources'),'source originals'))!=set(fixture.SOURCES):raise SuiteError('archive inventory differs')
    for source,fact in sources.items():
        if type(fact.get('size_bytes')) is not int or fact['size_bytes']<=0 or not isinstance(fact.get('sha256'),str) or not re.fullmatch('[0-9a-f]{64}',fact['sha256']) or value['archived_sources'][source]!={'path':'sieve-area-sources/'+source,**fact}:raise SuiteError('source original size/hash differs')
    flags=('cog_valid','has_overviews','full_pixel_and_mask_verification','independent_bfs_oracle_verified','original_coverage_verified','protected_classes_verified','donor_threshold_verified','nearest_overviews_verified','frozen_components_verified','legal_zero_verified','area_comparison_verified','linear_units_verified')
    for name,fact in value['targets'].items():
        contract=obj(fact.get('threshold_contract'),'threshold contract');wanted_contract=fixture.threshold_contract(name)
        if (fact.get('threshold_contract')!=fixture.threshold_contract(name) or type(contract.get('value'))is not type(wanted_contract['value']) or type(contract.get('linear_unit_to_metre'))is not float or any(fact.get(k) is not True for k in flags) or type(fact.get('size_bytes')) is not int or fact['size_bytes']<=0
            or not isinstance(fact.get('sha256'),str) or not re.fullmatch('[0-9a-f]{64}',fact['sha256'])
            or type(fact.get('overview_count')) is not int or fact['overview_count']<=0
            or archives[name]!={'path':'sieve-area-cogs/'+name,**{k:fact[k] for k in ('size_bytes','sha256')}}):raise SuiteError('complete COG/size/hash/archive proof differs')
        if name in previous and fact!=previous[name]:raise SuiteError('previous original COG changed')
    wanted=[list(pair) for pair in fixture.COMPARISONS if all(n in names for n in pair)]
    if value.get('comparisons')!=wanted:raise SuiteError('actual protected/control comparison differs')


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
                    and message.endswith('任务 sieve 执行失败: '+cause)):
                    return {'execution_id': identifier, 'task': 'sieve', 'problem': problem, 'language': language, 'exact_cause': cause, 'source': 'develop_runtime_log'}
        time.sleep(min(1, max(0, deadline-time.monotonic())))
    raise SuiteError('missing exact area sieve task and cause')

def fixture_call(repository, env, action, names, timeout):
    if action not in ('prepare-oracle', 'verify', 'cleanup-oracle'): raise SuiteError('unknown oracle action')
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-sieve-area-fixture.py', action, *names],
                            cwd=repository, env=dict(env), capture_output=True, text=True, timeout=min(150, timeout))
    if result.returncode: raise SuiteError('independent sieve-area COG verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'sieve-area physical evidence')

def current(execution, identity, identifier, dag, runtime, language, status):
    readonly.actor(execution, identity, 'develop'); readonly.execution_snapshot(execution, dag, runtime)
    if (execution.get('execution_id') != identifier or type(execution.get('tenant_id')) is not int
        or execution['tenant_id'] != int(identity['tenant_id']) or execution.get('task_type') != 'workflow'
        or execution.get('status') != status or execution['execution_config'].get('display_language') != language): raise SuiteError('current execution identity/status/language differs')

def run_scenario(repository, env, client, physical_runner=fixture_call, diagnostic_runner=invalid_diagnostic, lineage_reader=lineage.read_current):
    env = dict(env); started = time.monotonic(); budget = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    if not math.isfinite(budget) or budget != 600: raise SuiteError('area sieve business budget must be exactly 600 seconds')
    def remaining():
        left = budget-(time.monotonic()-started)
        if left <= 0: raise SuiteError('area sieve business budget exhausted')
        return left
    client = BudgetClient(client, remaining); directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR']); directory.mkdir(parents=True, exist_ok=True)
    stages = []; primary = cleanup_error = report = None
    def progress(status):
        (directory/'sieve-area-progress.json').write_bytes(encoded({'suite': SUITE, 'result': status,
            'budget_ms': 600000, 'elapsed_ms': round((time.monotonic()-started)*1000), 'stages': stages}))
    try:
        tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
        if tenant <= 1: raise SuiteError('sieve-area acceptance forbids default tenant')
        identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
        engine = positive(env['ADDP_ONLINE_SIEVE_AREA_NFS_ENGINE_ID'], 'NFS engine')
        scopes = {name: f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
        scan = support.wait_for_meta_scan(client, engine, time.monotonic()+remaining(), scope={'targets': list(scopes.values())})
        items = {name: support.find_fixture_item(client, engine, name, 'sieve-area source') for name in fixture.SOURCES}
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
        if type(baseline.get('expected_array_count')) is not int or baseline['expected_array_count']!=16:raise SuiteError('sixteen frozen independent arrays required')
        if type(baseline.get('cache_bytes')) is not int or not fixture.expected_cache_bytes()<baseline['cache_bytes']<fixture.expected_cache_bytes()+16*1024:raise SuiteError('per-case cache byte bound differs')
        env['ADDP_ONLINE_SIEVE_AREA_ORACLE_ANCHOR_SHA256']=anchor
        immutable(directory/'sieve-area-oracle-preparation.json',encoded(baseline))
        check_physical(baseline,baseline.get('sources'),{},scope,[])
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
                dag = definition(sources, parent, 'area-four.cog.tif', invalid=problem, language=language)
                identifier = raster.submit(SubmittedLanguageClient(client, language), runtime, dag, execution_timeout=min(180, max(1, int(remaining()))))
                execution = raster.wait_execution(client, 'develop', identifier, remaining(), expected='failed'); current(execution, identity, identifier, dag, runtime, language, 'failed')
                if obj(execution.get('error_details'), 'error details').get('category') != 'execution_failed': raise SuiteError('wrong failure category')
                diagnostic = diagnostic_runner(repository, env, identifier, problem, language, remaining()); remaining()
                if diagnostic != {'execution_id': identifier, 'task': 'sieve', 'problem': problem, 'language': language, 'exact_cause': MESSAGES[problem][language == 'en'], 'source': 'develop_runtime_log'}: raise SuiteError('wrong exact failure task/cause')
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
        if len(cases) != 16 or len(failures) != 64 or full_checks != 1160 or physical_calls != 81: raise SuiteError('all 16 successes, 64 failures, 1160 COG checks and 81 physical calls required')
        for name, item in items.items():
            if client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload != before[name]: raise SuiteError('source metadata changed')
        lineage.verify_archive(env, SUITE, cases); remaining()
        report = {'schema_version': 'addp.'+SUITE+'/v1', 'suite': SUITE, 'result': 'passed', 'identity': identity,
                  'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'sources': sources, 'runtime_engine_id': runtime,
                  'source_scan_execution_id': scan, 'source_scan_identity': source_scan_identity,
                  'cases': cases, 'failed_parameters': failures, 'full_target_checks': full_checks, 'physical_verification_calls': physical_calls,
                  'physical': native, 'archived_cogs': native['archived_cogs'], 'source_unchanged': True, 'oracle_scope_sha256':scope,'oracle_anchor_sha256':anchor}
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
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'sieve-area-persistence.json').write_bytes(encoded(report)); print(json.dumps(report, ensure_ascii=False, sort_keys=True))

if __name__ == '__main__':
    try: main()
    except Exception as error:
        print(f'Raster sieve-area persistence acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr); raise SystemExit(1)
