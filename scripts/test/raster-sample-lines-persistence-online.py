#!/usr/bin/env python3
"""Sixteen complete GeoPackages, 52 exact failures, 968 real GeoPackage rereads, fixed 600s."""
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
legacy = importlib.import_module('scripts.test.raster-zonal-statistics-persistence-online')
fixture = importlib.import_module('business.scripts.online-raster-sample-lines-fixture')
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
MESSAGES = {'band_zero': ('点位栅格采样必须显式选择有效的一基数据波段', 'Raster point sampling requires an explicit valid one-based data band'), 'band_high': ('点位栅格采样必须显式选择有效的一基数据波段', 'Raster point sampling requires an explicit valid one-based data band'), 'band_boolean': ('点位栅格采样必须显式选择有效的一基数据波段', 'Raster point sampling requires an explicit valid one-based data band'), 'band_string': ('点位栅格采样必须显式选择有效的一基数据波段', 'Raster point sampling requires an explicit valid one-based data band'), 'band_null': ('点位栅格采样必须显式选择有效的一基数据波段', 'Raster point sampling requires an explicit valid one-based data band'), 'alpha': ('点位栅格采样不能选择alpha作为数据波段', 'Raster point sampling cannot select alpha as data'), 'method_null': ('沿线栅格采样方法必须为cell或bilinear', 'Raster line sampling method must be cell or bilinear'), 'method_unknown': ('沿线栅格采样方法必须为cell或bilinear', 'Raster line sampling method must be cell or bilinear'), 'spacing_zero': ('沿线采样间距须为可精确表示的有限正实数米值，不能为布尔值', 'Line sampling spacing must be a representable finite positive real number of metres, not a boolean'), 'spacing_negative': ('沿线采样间距须为可精确表示的有限正实数米值，不能为布尔值', 'Line sampling spacing must be a representable finite positive real number of metres, not a boolean'), 'spacing_boolean': ('沿线采样间距须为可精确表示的有限正实数米值，不能为布尔值', 'Line sampling spacing must be a representable finite positive real number of metres, not a boolean'), 'spacing_string': ('沿线采样间距须为可精确表示的有限正实数米值，不能为布尔值', 'Line sampling spacing must be a representable finite positive real number of metres, not a boolean'), 'spacing_null': ('沿线采样间距须为可精确表示的有限正实数米值，不能为布尔值', 'Line sampling spacing must be a representable finite positive real number of metres, not a boolean'), 'spacing_list': ('沿线采样间距须为可精确表示的有限正实数米值，不能为布尔值', 'Line sampling spacing must be a representable finite positive real number of metres, not a boolean'), 'id_geometry': ('沿线采样须显式指定现有非几何、非空且唯一的标量编号字段', 'Line sampling requires an explicit existing non-geometry, non-null unique scalar identifier field'), 'id_null': ('沿线采样须显式指定现有非几何、非空且唯一的标量编号字段', 'Line sampling requires an explicit existing non-geometry, non-null unique scalar identifier field'), 'id_unknown': ('沿线采样须显式指定现有非几何、非空且唯一的标量编号字段', 'Line sampling requires an explicit existing non-geometry, non-null unique scalar identifier field'), 'id_duplicate': ('沿线采样须显式指定现有非几何、非空且唯一的标量编号字段', 'Line sampling requires an explicit existing non-geometry, non-null unique scalar identifier field'), 'different_crs': ('线与栅格须具有相同明确的二维投影CRS及相同已知有限正水平线性单位', 'Lines and raster require the same explicit two-dimensional projected CRS with matching known finite positive horizontal linear units'), 'geographic': ('线与栅格须具有相同明确的二维投影CRS及相同已知有限正水平线性单位', 'Lines and raster require the same explicit two-dimensional projected CRS with matching known finite positive horizontal linear units'), 'point': ('沿线栅格采样仅接受非空、有效、有限XY的二维非零长度LineString', 'Raster line sampling requires non-empty valid finite XY nonzero-length two-dimensional LineStrings'), 'polygon': ('沿线栅格采样仅接受非空、有效、有限XY的二维非零长度LineString', 'Raster line sampling requires non-empty valid finite XY nonzero-length two-dimensional LineStrings'), 'multiline': ('沿线栅格采样仅接受非空、有效、有限XY的二维非零长度LineString', 'Raster line sampling requires non-empty valid finite XY nonzero-length two-dimensional LineStrings'), 'zero_line': ('沿线栅格采样仅接受非空、有效、有限XY的二维非零长度LineString', 'Raster line sampling requires non-empty valid finite XY nonzero-length two-dimensional LineStrings'), 'z_line': ('沿线栅格采样仅接受非空、有效、有限XY的二维非零长度LineString', 'Raster line sampling requires non-empty valid finite XY nonzero-length two-dimensional LineStrings'), 'collision_distance': ('沿线采样结果字段不能覆盖原字段或活动几何列', 'Line sampling result columns must not overwrite source fields or active geometry'), 'collision_value': ('沿线采样结果字段不能覆盖原字段或活动几何列', 'Line sampling result columns must not overwrite source fields or active geometry'), 'collision_status': ('沿线采样结果字段不能覆盖原字段或活动几何列', 'Line sampling result columns must not overwrite source fields or active geometry')}
vector_persistence=importlib.import_module('scripts.utils.online_vector_persistence')

class DiagnosticClient(base.DiagnosticClient):
    diagnostic_name = 'raster-sample-lines-submission-error.json'
    diagnostic_schema = 'addp.raster-sample-lines-submission-diagnostic/v1'

def case_inputs(name):return [fixture.CASES[name][1],fixture.CASES[name][0]]
def definition(sources,parent,name,*,invalid=None,language='zh-cn'):
 if name not in fixture.CASES or invalid not in(None,*FAILURES)or language not in('zh-cn','en')or(invalid and name!='cell.gpkg'):raise SuiteError('unknown exact line sampling case')
 raster,vector,mode,method,spacing=fixture.CASES[name]
 if invalid in('different_crs','geographic','point','polygon','multiline','zero_line','z_line','collision_distance','collision_value','collision_status'):vector=invalid.replace('_','-')+'.gpkg'
 if invalid=='geographic':raster='geographic.tif'
 if invalid=='id_duplicate':vector='duplicate.gpkg'
 tasks=[{'id':'values','operator':'raster_load','depends_on':[],'params':{'locator':sources[raster]}},{'id':'lines','operator':'load','depends_on':[],'params':{'locator':sources[vector]}}]
 params={'input_raster':{'$ref':'values'},'input_gdf':{'$ref':'lines'},'band':2,'id_field':'source_row','spacing_metres':spacing,'method':method}
 if invalid and invalid.startswith('band_'):params['band']={'zero':0,'high':99,'boolean':True,'string':'2','null':None}[invalid[5:]]
 if invalid=='alpha':params['band']=3
 if invalid=='method_null':params['method']=None
 if invalid=='method_unknown':params['method']='nearest'
 if invalid and invalid.startswith('spacing_'):params['spacing_metres']={'zero':0,'negative':-1,'boolean':True,'string':'1','null':None,'list':[1]}[invalid[8:]]
 if invalid=='id_geometry':params['id_field']='shape'
 if invalid in('id_null','id_unknown'):params['id_field']=None if invalid=='id_null'else'absent'
 tasks.append({'id':'sample','operator':'raster_sample_lines','depends_on':['values','lines'],'params':params})
 tasks.append({'id':'save','operator':'save','depends_on':['sample'],'params':{'input_df':{'$ref':'sample'},'target_parent_locator':parent,'target_name':f'failed-{invalid}-{language}.gpkg'if invalid else name,'mode':'fail'}})
 return {'tasks':tasks}

def expected_lineage(sources,name,target):
 inputs=[{'port':'input.'+role,'locator':sources[source]}for role,source in zip(('lines','values'),case_inputs(name))]
 return {'schema_version':'addp.lineage-facts/v1','inputs':inputs,'outputs':[{'port':'output.save','locator':target,'write_mode':'create'}],
 'operations':[{'kind':'derive','operator':'save','input_ports':[v['port']for v in inputs],'output_ports':['output.save']}]}

def validate_preview(preview,expected,name):
 if not isinstance(preview,dict)or preview.get('type')!='FeatureCollection'or len(array(preview.get('features'),'features'))!=len(expected):raise SuiteError('complete point projection differs')
 for feature,wanted in zip(preview['features'],expected):
  geometry=obj(feature.get('geometry'),'geometry')
  if feature.get('type')!='Feature'or geometry.get('type')!='Point'or geometry.get('coordinates')!=wanted['geometry_coordinates']:raise SuiteError('exact point projection/order differs')
  try:fixture.validate_properties(obj(feature.get('properties'),'properties'),wanted['properties'],name)
  except fixture.FixtureError as error:raise SuiteError(str(error))from error

def check_physical(value, sources, previous, anchor, names):
    if (value.get('runtime_workspaces_removed') is not True or value.get('full_source_pixels_verified') is not True or value.get('full_source_vectors_verified') is not True
        or type(value.get('full_gpkg_checks')) is not int or value['full_gpkg_checks'] != len(names)
        or value.get('oracle_sha256') != anchor or value.get('sources') != sources
        or set(obj(value.get('targets'), 'physical targets')) != set(names)):
        raise SuiteError('complete source/GeoPackage/count/cache/cleanup evidence differs')
    cache=obj(value.get('archived_oracle'),'original frozen oracle')
    if set(cache)!={fixture.ORACLE}or cache[fixture.ORACLE].get('sha256')!=anchor or cache[fixture.ORACLE].get('path')!='raster-sample-lines-oracle/'+fixture.ORACLE or type(cache[fixture.ORACLE].get('size_bytes'))is not int or cache[fixture.ORACLE]['size_bytes']<=0:raise SuiteError('frozen oracle archive differs')
    archives = obj(value.get('archived_gpkgs'), 'original GeoPackages')
    if set(archives) != set(names) or set(obj(value.get('archived_sources'), 'original sources')) != set(fixture.SOURCES): raise SuiteError('archive inventory differs')
    for source, fact in sources.items():
        if (type(fact.get('size_bytes')) is not int or fact['size_bytes'] <= 0 or not isinstance(fact.get('sha256'), str)
            or not re.fullmatch('[0-9a-f]{64}', fact['sha256']) or value['archived_sources'][source] != {'path': 'raster-sample-lines-sources/'+source, **fact}): raise SuiteError('original source hash/size differs')
    for name, fact in value['targets'].items():
        storage_ids = fact.get('storage_fids')
        storage_column = fact.get('storage_fid_column')
        if (fact.get('independent_storage_fid_verified') is not True
            or not isinstance(storage_column,str) or not storage_column
            or storage_column.casefold() in {field.casefold() for field,_ in fixture.schema(name)}
            or not isinstance(storage_ids,list) or any(type(identifier) is not int for identifier in storage_ids)
            or len(set(storage_ids)) != fact.get('feature_count') or len(storage_ids) != fact.get('feature_count')):
            raise SuiteError('independent unique storage FID proof differs')
        if (fact.get('complete_geometry_attributes_statistics_verified') is not True or fact.get('schema')!=[list(v) for v in fixture.schema(name)]
            or fact.get('crs')!='EPSG:'+str(fixture.vector_crs(fixture.CASES[name][1])) or not isinstance(fact.get('layer_name'),str) or not fact['layer_name']
            or type(fact.get('feature_count')) is not int or fact['feature_count']<0
            or type(fact.get('size_bytes')) is not int or fact['size_bytes']<=0
            or not isinstance(fact.get('sha256'),str) or not re.fullmatch('[0-9a-f]{64}',fact['sha256'])
            or archives[name]!={'path':'raster-sample-lines-gpkgs/'+name,**{k:fact[k] for k in ('size_bytes','sha256')}}):raise SuiteError('full GeoPackage schema/size/hash/archive proof differs')
        if name in previous and fact!=previous[name]:raise SuiteError('previous complete GeoPackage changed')

def validate_item(item,fact,name,expected_rows):
    sizes=vector_persistence.validate_item(item,fact,name)
    if fact['feature_count']!=len(expected_rows):raise SuiteError('GeoPackage feature count differs')
    return sizes

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
                    and message.endswith('任务 sample 执行失败: '+cause)):
                    return {'execution_id': identifier, 'task': 'sample', 'problem': problem, 'language': language, 'exact_cause': cause, 'source': 'develop_runtime_log'}
        time.sleep(min(1, max(0, deadline-time.monotonic())))
    raise SuiteError('missing exact point sampling task and cause')

def fixture_call(repository, env, action, names, timeout):
    if action not in ('oracle', 'verify', 'cleanup-oracle'): raise SuiteError('unknown oracle action')
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-sample-lines-fixture.py', action, *names],
                            cwd=repository, env=dict(env), capture_output=True, text=True, timeout=min(150, timeout))
    if result.returncode: raise SuiteError('independent point sampling GeoPackage verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'point sampling physical evidence')

def current(execution, identity, identifier, dag, runtime, language, status):
    readonly.actor(execution, identity, 'develop'); readonly.execution_snapshot(execution, dag, runtime)
    if (execution.get('execution_id') != identifier or type(execution.get('tenant_id')) is not int
        or execution['tenant_id'] != int(identity['tenant_id']) or execution.get('task_type') != 'workflow'
        or execution.get('status') != status or execution['execution_config'].get('display_language') != language): raise SuiteError('current execution identity/status/language differs')

def run_scenario(repository, env, client, physical_runner=fixture_call, diagnostic_runner=invalid_diagnostic, lineage_reader=lineage.read_current):
    env = dict(env); started = time.monotonic(); budget = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    if not math.isfinite(budget) or budget != 600: raise SuiteError('point sampling business budget must be exactly 600 seconds')
    def remaining():
        left = budget-(time.monotonic()-started)
        if left <= 0: raise SuiteError('point sampling business budget exhausted')
        return left
    client = BudgetClient(client, remaining); directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR']); directory.mkdir(parents=True, exist_ok=True)
    stages = []; primary = cleanup_error = report = None
    def progress(status):
        (directory/'raster-sample-lines-progress.json').write_bytes(encoded({'suite': SUITE, 'result': status,
            'budget_ms': 600000, 'elapsed_ms': round((time.monotonic()-started)*1000), 'stages': stages}))
    try:
        tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
        if tenant <= 1: raise SuiteError('point sampling acceptance forbids default tenant')
        identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
        engine = positive(env['ADDP_ONLINE_RASTER_SAMPLE_LINES_NFS_ENGINE_ID'], 'NFS engine')
        scopes = {name: f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
        scan = support.wait_for_meta_scan(client, engine, time.monotonic()+remaining(), scope={'targets': list(scopes.values())})
        items = {name: support.find_fixture_item(client, engine, name, 'point sampling source') for name in fixture.SOURCES}
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
        expected = copy.deepcopy(expected); env['ADDP_ONLINE_RASTER_SAMPLE_LINES_ORACLE_SHA256'] = anchor
        immutable(directory/'raster-sample-lines-expectations.json', encoded(expected))
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
            facts, output_scan, result = legacy.validate_success(execution, [sources[n] for n in case_inputs(name)], target)
            lineage.validate(facts, wanted, public=True); validate_preview(result.get('final_result'), expected[name], name)
            if output_scan in scans: raise SuiteError('duplicate automatic GeoPackage scan')
            scans.append(output_scan)
            scan_identity = base.validate_scan(raster.wait_execution(client, 'meta', output_scan, remaining()), output_scan, identifier, tenant, int(identity['principal_id']), engine)
            monitor = client.request('GET', '/api/v1/monitor/executions/by-execution-id/'+identifier, (200,)).payload
            readonly.actor(monitor, identity, 'develop')
            if monitor.get('execution_id') != identifier or type(monitor.get('tenant_id')) is not int or monitor.get('tenant_id') != tenant or monitor.get('status') != 'success' or obj(monitor.get('metadata'), 'metadata').get('lineage_facts') != facts: raise SuiteError('Monitor current output lineage differs')
            names.append(name); native = physical_runner(repository, env, 'verify', names, remaining()); remaining()
            check_physical(native, baseline['sources'], previous, anchor, names); previous = copy.deepcopy(native['targets'])
            physical_calls += 1; full_checks += len(names)
            item0 = support.find_fixture_item(client, engine, 'outputs/'+name, 'GeoPackage')
            item = client.request('GET', f'/api/v1/meta/items/{item0["id"]}', (200,)).payload
            size = validate_item(item, native['targets'][name], name, expected[name])
            immutable(directory/('target-'+name+'-meta-before.json'),encoded(item))
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
                dag = definition(sources, parent, 'cell.gpkg', invalid=problem, language=language)
                identifier = raster.submit(SubmittedLanguageClient(client, language), runtime, dag, execution_timeout=min(180, max(1, int(remaining()))))
                execution = raster.wait_execution(client, 'develop', identifier, remaining(), expected='failed'); current(execution, identity, identifier, dag, runtime, language, 'failed')
                if obj(execution.get('error_details'), 'error details').get('category') != 'execution_failed': raise SuiteError('wrong failure category')
                diagnostic = diagnostic_runner(repository, env, identifier, problem, language, remaining()); remaining()
                if diagnostic != {'execution_id': identifier, 'task': 'sample', 'problem': problem, 'language': language, 'exact_cause': MESSAGES[problem][language == 'en'], 'source': 'develop_runtime_log'}: raise SuiteError('wrong exact failure task/cause')
                readonly.no_publication(execution)
                monitor = client.request('GET', '/api/v1/monitor/executions/by-execution-id/'+identifier, (200,)).payload
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
        if len(cases) != 16 or len(failures) != 56 or full_checks != 1032 or physical_calls != 73: raise SuiteError('all 16 successes, 56 failures, 1032 GeoPackage checks and 73 physical calls required')
        target_metadata_before={case['target_item_id']:json.loads((directory/('target-'+case['name']+'-meta-before.json')).read_bytes())for case in cases}
        target_metadata_after={identifier:client.request('GET',f'/api/v1/meta/items/{identifier}',(200,)).payload for identifier in target_metadata_before}
        immutable(directory/'target-meta-after.json',encoded(target_metadata_after))
        if target_metadata_after != target_metadata_before:raise SuiteError('target metadata changed')
        source_metadata_after={name:client.request('GET',f'/api/v1/meta/items/{item["id"]}',(200,)).payload for name,item in items.items()}
        immutable(directory/'source-meta-after.json',encoded(source_metadata_after))
        if source_metadata_after != before:raise SuiteError('source metadata changed')
        lineage.verify_archive(env, SUITE, cases); remaining()
        immutable(directory/'raster-sample-lines-expectations.json', encoded(expected))
        report = {'schema_version': 'addp.'+SUITE+'/v1', 'suite': SUITE, 'result': 'passed', 'identity': identity,
                  'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'sources': sources, 'runtime_engine_id': runtime,
                  'source_scan_execution_id': scan, 'source_scan_identity': source_scan_identity,
                  'cases': cases, 'failed_parameters': failures, 'full_target_checks': full_checks, 'physical_verification_calls': physical_calls,
                  'physical': native, 'archived_gpkgs': native['archived_gpkgs'], 'source_unchanged': True,'source_meta_unchanged':True,'target_meta_unchanged':True,'oracle_sha256': anchor}
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
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'raster-sample-lines-persistence.json').write_bytes(encoded(report)); print(json.dumps(report, ensure_ascii=False, sort_keys=True))

if __name__ == '__main__':
    try: main()
    except Exception as error:
        print(f'Point sampling persistence acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr); raise SystemExit(1)
