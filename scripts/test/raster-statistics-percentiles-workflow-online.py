#!/usr/bin/env python3
"""Sixteen successes, forty exact failures, fifty-seven complete source checks."""
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
fixture = importlib.import_module('business.scripts.online-raster-statistics-percentiles-fixture')
raster, readonly, support = base.raster, base.readonly, base.support
obj, array, positive, SuiteError = base.obj, base.array, base.positive, base.SuiteError
SubmittedLanguageClient, BudgetClient = base.SubmittedLanguageClient, base.BudgetClient
encoded, immutable, archive_response = base.encoded, base.immutable, base.archive_response
current_execution, compare = base.current_execution, base.compare
scan_ids, validate_scan = base.scan_ids, base.validate_scan
SUITE, FAILURES = fixture.SUITE, fixture.FAILURES
INVALID_LISTS = {
    'list_null': None, 'list_boolean': True, 'list_number': 50, 'list_string': '50',
    'list_object': {'p': 50}, 'list_empty': [], 'item_boolean': [True], 'item_string': ['50'],
    'item_null': [None], 'item_list': [[50]], 'item_object': [{}], 'item_low': [-1],
    'item_high': [101], 'duplicate': [25, 25], 'descending': [75, 25],
}
MESSAGES = {**{name: base.PERCENTILES_MESSAGES for name in INVALID_LISTS},
            **{name: ('band must be a valid one-based index',)*2 for name in FAILURES if name.startswith('band_')},
            'complex': ('Complex-valued bands are not supported by numeric analysis',)*2}


class DiagnosticClient(base.DiagnosticClient):
    diagnostic_name = 'statistics-percentiles-submission-error.json'
    diagnostic_schema = 'addp.raster-statistics-percentiles-submission-diagnostic/v1'


def definition(sources, name, *, invalid=None):
    if name not in fixture.CASES or invalid not in (None, *FAILURES) or (invalid and name != 'quartiles'):
        raise SuiteError('unknown exact multi-percentile case')
    case = fixture.CASES[name]; source = 'complex.tif' if invalid == 'complex' else case['source']
    params = {'input_raster': {'$ref': 'source'}, 'band': case['band']}
    if 'percentiles' in case: params['percentiles'] = copy.deepcopy(case['percentiles'])
    if invalid in INVALID_LISTS: params['percentiles'] = copy.deepcopy(INVALID_LISTS[invalid])
    if invalid and invalid.startswith('band_'):
        params['band'] = {'band_zero': 0, 'band_high': 99, 'band_boolean': True, 'band_string': '2'}[invalid]
    tasks = [{'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': sources[source]}}]
    if case.get('stage') == 'reclassify':
        tasks.append({'id': 'classes', 'operator': 'raster_reclassify', 'depends_on': ['source'],
                      'params': {'input_raster': {'$ref': 'source'}, 'band': 2, 'rules': fixture.class_rules(case)}})
        params.update(input_raster={'$ref': 'classes'}, band=1)
    tasks.append({'id': 'analysis', 'operator': 'raster_statistics', 'depends_on': ['classes' if case.get('stage') else 'source'], 'params': params})
    return {'tasks': tasks}


def fixture_call(repository, env, action, timeout):
    if action not in {'oracle', 'verify'}: raise SuiteError('unknown read-only physical action')
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-statistics-percentiles-fixture.py', action],
                            cwd=repository, env=dict(env), text=True, capture_output=True, timeout=min(150, timeout))
    if result.returncode: raise SuiteError('multi-percentile physical verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'physical statistics evidence')


def check_physical(native, baseline=None):
    base.check_physical(native, baseline)
    if native.get('fixture_sources_sha256') != fixture.fixture_sources():
        raise SuiteError('source or matrix fixture code binding differs')
    if baseline is not None and native['fixture_sources_sha256'] != baseline['fixture_sources_sha256']:
        raise SuiteError('frozen fixture source changed')


def invalid_diagnostic(repository, env, identifier, problem, language, timeout):
    cause = MESSAGES[problem][language == 'en']
    root = Path(env.get('ADDP_RUNTIME_LOG_ROOT', repository/'logs/runtime'))
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
                    return {'execution_id': identifier, 'task': 'analysis', 'problem': problem, 'language': language,
                            'exact_cause': cause, 'source': 'develop_runtime_log'}
        time.sleep(min(1, max(0, deadline-time.monotonic())))
    raise SuiteError('missing exact statistics operator and submitted-language cause')


def run_scenario(repository, env, client, physical_runner=fixture_call, diagnostic_runner=invalid_diagnostic):
    started = time.monotonic(); budget = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    if not math.isfinite(budget) or budget != 600: raise SuiteError('statistics business budget must be exactly 600 seconds')
    def remaining():
        value = budget-(time.monotonic()-started)
        if value <= 0: raise SuiteError('statistics business budget exhausted')
        return value
    client = BudgetClient(client, remaining)
    directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR']); directory.mkdir(parents=True, exist_ok=True)
    responses = directory/'statistics-responses'; responses.mkdir(exist_ok=False)
    stages, receipts = [], {}
    def progress(status):
        (directory/'statistics-percentiles-progress.json').write_bytes(encoded({'suite': SUITE, 'result': status,
            'elapsed_ms': round((time.monotonic()-started)*1000), 'budget_ms': 600000, 'stages': stages}))
    progress('running')
    try:
        tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
        if tenant <= 1: raise SuiteError('statistics acceptance forbids the default tenant')
        identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
        engine = positive(env['ADDP_ONLINE_STATISTICS_PERCENTILES_NFS_ENGINE_ID'], 'NFS engine')
        scopes = {name: f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
        scan = support.wait_for_meta_scan(client, engine, time.monotonic()+remaining(), scope={'targets': list(scopes.values())})
        items = {name: support.find_fixture_item(client, engine, name, 'statistics source') for name in fixture.SOURCES}
        sources = {name: scopes[name]+f'&item_id={items[name]["id"]}' for name in fixture.SOURCES}
        before = {name: client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload for name, item in items.items()}
        scanned = raster.wait_execution(client, 'meta', scan, remaining())
        scan_identity = validate_scan(scanned, scan, None, tenant, int(identity['principal_id']), engine)
        runtimes = array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'workflow engines')
        selected = [v for v in runtimes if v.get('engine_type') == 'geopython_workflow' and v.get('connection_status') == 'online']
        if len(selected) != 1: raise SuiteError('exactly one same-source product Runtime is required')
        runtime = positive(selected[0].get('id'), 'Runtime')
        graphs = {name: readonly.graph(client, item['id']) for name, item in items.items()}
        baseline = physical_runner(repository, env, 'oracle', remaining()); remaining(); check_physical(baseline)
        expectations = copy.deepcopy(obj(baseline.get('expectations'), 'independent expectations'))
        if set(expectations) != set(fixture.CASES): raise SuiteError('fixed sixteen oracle objects are required')
        oracle_receipt = immutable(directory/'statistics-expectations.json', encoded(expectations))
        for name in fixture.SOURCES:
            if type(before[name].get('size_bytes')) is not int or before[name]['size_bytes'] != baseline['sources'][name]['size_bytes']:
                raise SuiteError('positive physical/Meta source size differs')
        scans = scan_ids(client)
        if scans != [scan]: raise SuiteError('only the exact source initialization scan is permitted')
        cases, failures, prior = [], [], []
        physical_calls, previous_checks = 1, 0
        matrix = [(name, None, 'zh-cn' if index % 2 == 0 else 'en') for index, name in enumerate(fixture.CASES)]
        matrix += [('quartiles', problem, language) for problem in FAILURES for language in ('zh-cn', 'en')]
        for index, (name, problem, language) in enumerate(matrix, 1):
            stage = {'index': index, 'name': name, 'problem': problem, 'language': language, 'status': 'running'}
            stages.append(stage); progress('running')
            dag = definition(sources, name, invalid=problem); status = 'failed' if problem else 'success'
            identifier = raster.submit(SubmittedLanguageClient(client, language), runtime, dag, execution_timeout=min(180, max(1, int(remaining()))))
            raster.wait_execution(client, 'develop', identifier, remaining(), expected=status)
            response = client.request('GET', '/api/v1/develop/executions/'+identifier, (200,))
            execution = obj(response.payload, 'professional execution')
            current_execution(execution, identity, identifier, dag, runtime, language, status)
            receipts[f'{index:02}-develop'] = archive_response(responses, f'{index:02}-develop', response)
            monitor_response = client.request('GET', '/api/v1/monitor/executions/by-execution-id/'+identifier, (200,))
            monitor = obj(monitor_response.payload, 'Monitor')
            readonly.actor(monitor, identity, 'develop'); readonly.no_publication(monitor)
            if (monitor.get('execution_id') != identifier or type(monitor.get('tenant_id')) is not int
                or monitor['tenant_id'] != tenant or monitor.get('status') != status): raise SuiteError('Monitor current identity/status differs')
            receipts[f'{index:02}-monitor'] = archive_response(responses, f'{index:02}-monitor', monitor_response)
            if problem:
                diagnostic = diagnostic_runner(repository, env, identifier, problem, language, remaining())
                expected_diagnostic = {'execution_id': identifier, 'task': 'analysis', 'problem': problem, 'language': language,
                                       'exact_cause': MESSAGES[problem][language == 'en'], 'source': 'develop_runtime_log'}
                if diagnostic != expected_diagnostic: raise SuiteError('exact current execution diagnostic differs')
                opposite = SubmittedLanguageClient(client, 'en' if language == 'zh-cn' else 'zh-cn')
                reread = opposite.request('GET', '/api/v1/develop/executions/'+identifier, (200,)).payload
                if any(reread.get(key) != execution.get(key) for key in ('error_msg', 'error_details', 'execution_config')):
                    raise SuiteError('query language rewrote stored failure')
                failures.append({'problem': problem, 'language': language, 'execution_id': identifier, 'diagnostic': diagnostic})
            else:
                actual = raster.transient_result(execution); compare(actual, expectations[name])
                completed = {case['name']: case['json_result'] for case in cases}
                if fixture.CASES[name]['source'] == 'values.tif' and not fixture.CASES[name].get('stage'):
                    if {k:v for k,v in actual.items() if k != 'percentiles'} != completed.get('original', actual):
                        raise SuiteError('requested list changed original statistics')
                if name == 'endpoints' and [item['value'] for item in actual['percentiles']] != [actual['min'], actual['max']]:
                    raise SuiteError('endpoint values differ from min/max')
                if name == 'p50-control' and actual['percentiles'][0] != completed['quartiles']['percentiles'][2]:
                    raise SuiteError('same-source singleton P50 and batch differ')
                if name == 'reversed' and actual['percentiles'] != completed['fractional']['percentiles']:
                    raise SuiteError('source ordering changed the complete percentile list')
                if name == 'odd' and [actual['percentiles'][i]['value'] for i in (1, 2)] != [0., 0.]:
                    raise SuiteError('equal zero results for distinct percentiles were merged or changed')
                if name == 'constant' and (len(actual['percentiles']) != 5 or any(item['value'] != 0. for item in actual['percentiles'])):
                    raise SuiteError('constant zero batch lost requested positions')
                cases.append({'name': name, 'execution_id': identifier, 'source_locator': sources[fixture.CASES[name]['source']],
                              'json_result': actual, 'complete_object_verified': True})
            native = physical_runner(repository, env, 'verify', remaining()); remaining(); physical_calls += 1; check_physical(native, baseline)
            if scan_ids(client) != scans: raise SuiteError('read-only statistics added a scan')
            source_name = 'complex.tif' if problem == 'complex' else fixture.CASES[name]['source']
            if readonly.graph(client, items[source_name]['id']) != graphs[source_name]: raise SuiteError('read-only statistics changed source lineage')
            for previous_id, previous_result, previous_config in prior:
                previous = client.request('GET', '/api/v1/develop/executions/'+previous_id, (200,)).payload
                readonly.actor(previous, identity, 'develop'); readonly.no_publication(previous)
                if (previous.get('execution_id') != previous_id or type(previous.get('tenant_id')) is not int
                    or previous['tenant_id'] != tenant or previous.get('status') != 'success'
                    or raster.transient_result(previous) != previous_result
                    or previous.get('execution_config') != previous_config): raise SuiteError('previous execution result changed')
                previous_checks += 1
            if not problem: prior.append((identifier, copy.deepcopy(actual), copy.deepcopy(execution['execution_config'])))
            immutable(directory/'statistics-expectations.json', encoded(expectations))
            stage.update(status='passed', execution_id=identifier); progress('running')
        if len(cases) != 16 or len(failures) != 40 or physical_calls != 57 or previous_checks != 760:
            raise SuiteError('all 56 calls, 57 physical checks and 760 previous-result checks are required')
        for name, item in items.items():
            if client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload != before[name] or readonly.graph(client, item['id']) != graphs[name]:
                raise SuiteError('source metadata or lineage changed')
        for receipt in receipts.values():
            path = responses/receipt['path']
            if path.is_symlink() or not path.is_file() or path.stat().st_size != receipt['size_bytes'] or hashlib.sha256(path.read_bytes()).hexdigest() != receipt['sha256']:
                raise SuiteError('previous original response bytes changed')
        immutable(directory/'statistics-responses-manifest.json', encoded(receipts))
        remaining(); progress('passed'); remaining()
        elapsed_ms = round((time.monotonic()-started)*1000)
        if elapsed_ms > 600000: raise SuiteError('statistics final evidence exceeded the business budget')
        return {'schema_version': 'addp.raster-statistics-percentiles-workflow/v1', 'suite': SUITE, 'result': 'passed',
                'identity': identity, 'cases': cases, 'failed_parameters': failures, 'sources': sources,
                'source_scan_execution_id': scan, 'source_scan_identity': scan_identity, 'physical': native, 'oracle_receipt': oracle_receipt,
                'raw_response_receipts': receipts, 'physical_verification_calls': physical_calls,
                'previous_result_rechecks': previous_checks, 'elapsed_ms': elapsed_ms, 'budget_ms': 600000,
                'no_resource_outputs_or_derive': True, 'only_source_scan_verified': True, 'source_unchanged': True}
    except BaseException:
        if stages and stages[-1]['status'] == 'running': stages[-1]['status'] = 'failed'
        progress('failed'); raise


def main():
    env = dict(os.environ)
    result = run_scenario(Path(__file__).resolve().parents[2], env,
        DiagnosticClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30, env['ADDP_ONLINE_ARTIFACT_DIR']))
    immutable(Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'statistics-percentiles-workflow.json', encoded(result))
    print(json.dumps(result, ensure_ascii=False, allow_nan=False))


if __name__ == '__main__':
    try: main()
    except Exception as error:
        print(f'whole-raster statistics acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr)
        raise SystemExit(1)
