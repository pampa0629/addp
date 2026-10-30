#!/usr/bin/env python3
"""Sixteen complete CSVs, 56 exact failures, 1032 real CSV rereads, fixed 600s."""
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
fixture = importlib.import_module('business.scripts.online-raster-zonal-percentile-fixture')
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
MESSAGES = {name: base.PERCENTILE_MESSAGES for name in FAILURES[:7]}
MESSAGES.update({name: ('band must be a valid one-based index',)*2 for name in FAILURES if '_band_' in name})
MESSAGES.update({name: ('Complex-valued bands are not supported by numeric analysis',)*2 for name in ('values_complex', 'zones_complex')})
MESSAGES.update({name: ('Zonal statistics require data bands, not alpha',)*2 for name in ('values_alpha', 'zones_alpha')})
MESSAGES.update({'fractional_zone': ('floating classification values must be exact safe integers',)*2,
    'overflow_zone': ('classification values must fit signed Int64',)*2,
    'missing_crs': ('CRS must be explicit and non-empty',)*2,
    'missing_grid': ('Raster requires a geotransform',)*2,
    'singular_grid': ('Raster grid must have a finite invertible geotransform',)*2})
MESSAGES.update({name: ('Raster grids must match; use raster_align before combining rasters',)*2
                 for name in ('different_crs', 'different_size', 'shifted_grid', 'rotated_grid')})

class DiagnosticClient(base.DiagnosticClient):
    diagnostic_name = 'zonal-percentile-submission-error.json'
    diagnostic_schema = 'addp.raster-zonal-percentile-submission-diagnostic/v1'

def case_inputs(name): return list(fixture.CASES[name][:2])

def definition(sources, parent, name, *, invalid=None, language='zh-cn'):
    if name not in fixture.CASES or invalid not in (None, *FAILURES) or language not in ('zh-cn', 'en') or (invalid and name != 'coverage.csv'):
        raise SuiteError('unknown exact zonal percentile case')
    v, z, p, zone_band = fixture.CASES[name]; band = 2
    if invalid == 'values_complex': v = 'complex.tif'
    if invalid == 'zones_complex': z = 'complex.tif'
    if invalid == 'fractional_zone': z = 'fractional-zone.tif'
    if invalid == 'overflow_zone': z = 'overflow-zone.tif'
    if invalid in FAILURES[-7:]: z = invalid+'.tif'
    tasks = [{'id': key, 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': sources[source]}}
             for key, source in [('values', v), ('zones', z)]]
    zone_ref = 'zones'
    if name == 'reclassified.csv':
        tasks.append({'id': 'classes', 'operator': 'raster_reclassify', 'depends_on': ['zones'],
                      'params': {'input_raster': {'$ref': 'zones'}, 'band': 2, 'rules': fixture.class_rules()}})
        zone_ref, zone_band = 'classes', 1
    params = {'input_raster': {'$ref': 'values'}, 'zones_raster': {'$ref': zone_ref}, 'band': band, 'zones_band': zone_band}
    if p is not None: params['percentile'] = p
    if invalid in FAILURES[:7]: params['percentile'] = {'percentile_null': None, 'percentile_boolean': True,
        'percentile_string': '25', 'percentile_array': [25], 'percentile_object': {'p': 25}, 'percentile_low': -1, 'percentile_high': 101}[invalid]
    if invalid and '_band_' in invalid:
        params['band' if invalid.startswith('values_') else 'zones_band'] = {'zero': 0, 'high': 99, 'boolean': True, 'string': '2'}[invalid.rsplit('_', 1)[-1]]
    if invalid == 'values_alpha': params['band'] = 3
    if invalid == 'zones_alpha': params['zones_band'] = 3
    tasks.append({'id': 'analysis', 'operator': 'raster_zonal_statistics', 'depends_on': ['values', zone_ref], 'params': params})
    tasks.append({'id': 'save', 'operator': 'save', 'depends_on': ['analysis'], 'params': {
        'input_df': {'$ref': 'analysis'}, 'target_parent_locator': parent,
        'target_name': f'failed-{invalid}-{language}.csv' if invalid else name, 'mode': 'fail'}})
    return {'tasks': tasks}

def expected_lineage(sources, name, target):
    inputs = [{'port': 'input.'+role, 'locator': sources[source]} for role, source in zip(('values', 'zones'), case_inputs(name))]
    return {'schema_version': 'addp.lineage-facts/v1', 'inputs': inputs,
        'outputs': [{'port': 'output.save', 'locator': target, 'write_mode': 'create'}],
        'operations': [{'kind': 'derive', 'operator': 'save', 'input_ports': ['input.values', 'input.zones'], 'output_ports': ['output.save']}]}

def validate_preview(preview, expected, name):
    schema = [{'name': key, 'type': 'int64' if i < 4 else 'Float64'} for i, key in enumerate(fixture.columns(name))]
    if (not isinstance(preview, dict) or preview.get('type') != 'pandas_dataframe' or preview.get('schema') != schema
        or type(preview.get('row_count')) is not int or preview['row_count'] != len(expected)
        or type(preview.get('preview_limit')) is not int or preview['preview_limit'] != 50
        or type(preview.get('truncated')) is not bool or preview['truncated'] != (len(expected) > 50)):
        raise SuiteError('bounded table preview differs')
    # The existing DataFrame JSON projection uses double_precision=15.
    # Complete CSV values remain checked against the unrounded independent oracle.
    displayed = copy.deepcopy(expected[:50])
    for row in displayed:
        for key in ('min', 'max'):
            if row[key] is not None and abs(row[key]) >= 1e16: row[key] = float(format(row[key], '.15g'))
    try: fixture.validate_records(array(preview.get('preview_rows'), 'preview rows'), displayed, name)
    except fixture.FixtureError as error: raise SuiteError(str(error)) from error

def check_physical(value, sources, previous, anchor, names):
    if (value.get('runtime_workspaces_removed') is not True or value.get('full_source_pixels_verified') is not True
        or type(value.get('full_csv_checks')) is not int or value['full_csv_checks'] != len(names)
        or value.get('oracle_sha256') != anchor or value.get('sources') != sources
        or set(obj(value.get('targets'), 'physical targets')) != set(names)):
        raise SuiteError('complete source/CSV/count/cache/cleanup evidence differs')
    archives = obj(value.get('archived_csvs'), 'original CSVs')
    if set(archives) != set(names) or set(obj(value.get('archived_sources'), 'original sources')) != set(fixture.SOURCES): raise SuiteError('archive inventory differs')
    for source, fact in sources.items():
        if (type(fact.get('size_bytes')) is not int or fact['size_bytes'] <= 0 or not isinstance(fact.get('sha256'), str)
            or not re.fullmatch('[0-9a-f]{64}', fact['sha256']) or value['archived_sources'][source] != {'path': 'zonal-percentile-sources/'+source, **fact}): raise SuiteError('original source hash/size differs')
    for name, fact in value['targets'].items():
        if (fact.get('complete_table_verified') is not True or fact.get('schema') != fixture.columns(name)
            or type(fact.get('row_count')) is not int or fact['row_count'] < 0
            or type(fact.get('size_bytes')) is not int or fact['size_bytes'] <= 0
            or not isinstance(fact.get('sha256'), str) or not re.fullmatch('[0-9a-f]{64}', fact['sha256'])
            or archives[name] != {'path': 'zonal-percentile-csvs/'+name, **{k: fact[k] for k in ('size_bytes', 'sha256')}}): raise SuiteError('full CSV schema/size/hash/archive proof differs')
        if name in previous and fact != previous[name]: raise SuiteError('previous complete CSV changed')

def validate_item(item, fact, name, expected_rows):
    if (type(item.get('size_bytes')) is not int or item['size_bytes'] <= 0 or item['size_bytes'] != fact['size_bytes']
        or not item.get('scanned_at') or item.get('item_type') != 'file' or item.get('full_name') != 'outputs/'+name
        or obj(obj(item.get('attributes'), 'attributes').get('item'), 'item').get('format') != 'csv'
        or fact['row_count'] != len(expected_rows)):
        raise SuiteError('CSV physical/Meta positive size or identity differs')
    return {'physical_size_bytes': fact['size_bytes'], 'meta_size_bytes': item['size_bytes']}

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
                    and message.endswith('任务 analysis 执行失败: '+cause)):
                    return {'execution_id': identifier, 'task': 'analysis', 'problem': problem, 'language': language, 'exact_cause': cause, 'source': 'develop_runtime_log'}
        time.sleep(min(1, max(0, deadline-time.monotonic())))
    raise SuiteError('missing exact zonal percentile task and cause')

def fixture_call(repository, env, action, names, timeout):
    if action not in ('oracle', 'verify', 'cleanup-oracle'): raise SuiteError('unknown oracle action')
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-zonal-percentile-fixture.py', action, *names],
                            cwd=repository, env=dict(env), capture_output=True, text=True, timeout=min(150, timeout))
    if result.returncode: raise SuiteError('independent zonal CSV verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'zonal physical evidence')

def current(execution, identity, identifier, dag, runtime, language, status):
    readonly.actor(execution, identity, 'develop'); readonly.execution_snapshot(execution, dag, runtime)
    if (execution.get('execution_id') != identifier or type(execution.get('tenant_id')) is not int
        or execution['tenant_id'] != int(identity['tenant_id']) or execution.get('task_type') != 'workflow'
        or execution.get('status') != status or execution['execution_config'].get('display_language') != language): raise SuiteError('current execution identity/status/language differs')

def run_scenario(repository, env, client, physical_runner=fixture_call, diagnostic_runner=invalid_diagnostic, lineage_reader=lineage.read_current):
    env = dict(env); started = time.monotonic(); budget = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    if not math.isfinite(budget) or budget != 600: raise SuiteError('zonal percentile business budget must be exactly 600 seconds')
    def remaining():
        left = budget-(time.monotonic()-started)
        if left <= 0: raise SuiteError('zonal percentile business budget exhausted')
        return left
    client = BudgetClient(client, remaining); directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR']); directory.mkdir(parents=True, exist_ok=True)
    stages = []; primary = cleanup_error = report = None
    def progress(status):
        (directory/'zonal-percentile-progress.json').write_bytes(encoded({'suite': SUITE, 'result': status,
            'budget_ms': 600000, 'elapsed_ms': round((time.monotonic()-started)*1000), 'stages': stages}))
    try:
        tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
        if tenant <= 1: raise SuiteError('zonal acceptance forbids default tenant')
        identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
        engine = positive(env['ADDP_ONLINE_ZONAL_PERCENTILE_NFS_ENGINE_ID'], 'NFS engine')
        scopes = {name: f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
        scan = support.wait_for_meta_scan(client, engine, time.monotonic()+remaining(), scope={'targets': list(scopes.values())})
        items = {name: support.find_fixture_item(client, engine, name, 'zonal source') for name in fixture.SOURCES}
        sources = {name: scopes[name]+f'&item_id={items[name]["id"]}' for name in fixture.SOURCES}
        before = {name: client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload for name, item in items.items()}
        source_scan_identity = base.validate_scan(raster.wait_execution(client, 'meta', scan, remaining()), scan, None, tenant, int(identity['principal_id']), engine)
        selected = [v for v in array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'runtimes')
                    if v.get('engine_type') == 'geopython_workflow' and v.get('connection_status') == 'online']
        if len(selected) != 1: raise SuiteError('exactly one same-source product Runtime is required')
        runtime = positive(selected[0].get('id'), 'Runtime'); parent = f'addp://engine/{engine}/path/outputs?type=directory'
        baseline = physical_runner(repository, env, 'oracle', [], remaining()); remaining()
        anchor = baseline.get('oracle_sha256'); expected = obj(baseline.get('expectations'), 'independent expectations')
        if set(expected) != set(fixture.CASES) or not isinstance(anchor, str) or not re.fullmatch('[0-9a-f]{64}', anchor): raise SuiteError('sixteen frozen independent expectations required')
        expected = copy.deepcopy(expected); env['ADDP_ONLINE_ZONAL_PERCENTILE_ORACLE_SHA256'] = anchor
        immutable(directory/'zonal-percentile-expectations.json', encoded(expected))
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
            if output_scan in scans: raise SuiteError('duplicate automatic CSV scan')
            scans.append(output_scan)
            scan_identity = base.validate_scan(raster.wait_execution(client, 'meta', output_scan, remaining()), output_scan, identifier, tenant, int(identity['principal_id']), engine)
            monitor = client.request('GET', '/api/v1/monitor/executions/by-execution-id/'+identifier, (200,)).payload
            readonly.actor(monitor, identity, 'develop')
            if monitor.get('execution_id') != identifier or type(monitor.get('tenant_id')) is not int or monitor.get('tenant_id') != tenant or monitor.get('status') != 'success' or obj(monitor.get('metadata'), 'metadata').get('lineage_facts') != facts: raise SuiteError('Monitor current output lineage differs')
            names.append(name); native = physical_runner(repository, env, 'verify', names, remaining()); remaining()
            check_physical(native, baseline['sources'], previous, anchor, names); previous = copy.deepcopy(native['targets'])
            physical_calls += 1; full_checks += len(names)
            item0 = support.find_fixture_item(client, engine, 'outputs/'+name, 'CSV')
            item = client.request('GET', f'/api/v1/meta/items/{item0["id"]}', (200,)).payload
            size = validate_item(item, native['targets'][name], name, expected[name])
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
                dag = definition(sources, parent, 'coverage.csv', invalid=problem, language=language)
                identifier = raster.submit(SubmittedLanguageClient(client, language), runtime, dag, execution_timeout=min(180, max(1, int(remaining()))))
                execution = raster.wait_execution(client, 'develop', identifier, remaining(), expected='failed'); current(execution, identity, identifier, dag, runtime, language, 'failed')
                if obj(execution.get('error_details'), 'error details').get('category') != 'execution_failed': raise SuiteError('wrong failure category')
                diagnostic = diagnostic_runner(repository, env, identifier, problem, language, remaining()); remaining()
                if diagnostic != {'execution_id': identifier, 'task': 'analysis', 'problem': problem, 'language': language, 'exact_cause': MESSAGES[problem][language == 'en'], 'source': 'develop_runtime_log'}: raise SuiteError('wrong exact failure task/cause')
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
        if len(cases) != 16 or len(failures) != 56 or full_checks != 1032 or physical_calls != 73: raise SuiteError('all 16 successes, 56 failures, 1032 CSV checks and 73 physical calls required')
        for name, item in items.items():
            if client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload != before[name]: raise SuiteError('source metadata changed')
        lineage.verify_archive(env, SUITE, cases); remaining()
        immutable(directory/'zonal-percentile-expectations.json', encoded(expected))
        report = {'schema_version': 'addp.'+SUITE+'/v1', 'suite': SUITE, 'result': 'passed', 'identity': identity,
                  'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'sources': sources, 'runtime_engine_id': runtime,
                  'source_scan_execution_id': scan, 'source_scan_identity': source_scan_identity,
                  'cases': cases, 'failed_parameters': failures, 'full_target_checks': full_checks, 'physical_verification_calls': physical_calls,
                  'physical': native, 'archived_csvs': native['archived_csvs'], 'source_unchanged': True, 'oracle_sha256': anchor}
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
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'zonal-percentile-persistence.json').write_bytes(encoded(report)); print(json.dumps(report, ensure_ascii=False, sort_keys=True))

if __name__ == '__main__':
    try: main()
    except Exception as error:
        print(f'Zonal percentile persistence acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr); raise SystemExit(1)
