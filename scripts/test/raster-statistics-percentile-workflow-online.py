#!/usr/bin/env python3
"""Sixteen read-only full-raster results and 24 exact submitted-language failures."""
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

raster = importlib.import_module('scripts.test.raster-workflow-online')
rasterize = importlib.import_module('scripts.test.vector-rasterize-persistence-online')
readonly = importlib.import_module('scripts.test.raster-band-recommendation-workflow-online')
fixture = importlib.import_module('business.scripts.online-raster-statistics-percentile-fixture')
SubmittedLanguageClient = importlib.import_module('scripts.test.raster-disk-pressure-online').SubmittedLanguageClient
scan_ids = importlib.import_module('scripts.test.vector-pmtiles-workflow-online').scan_ids
validate_scan = importlib.import_module('scripts.test.raster-nibble-capacity-online').validate_scan
support = raster.support
obj, array, positive, SuiteError = raster.obj, raster.array, raster.positive, raster.SuiteError
SUITE = fixture.SUITE
FAILURES = fixture.FAILURES
PERCENTILE_MESSAGES = ('percentile必须为0至100之间的有限数值，且不能为布尔值',
                       'percentile must be a finite number from 0 to 100, excluding booleans')
MESSAGES = {**{name: PERCENTILE_MESSAGES for name in FAILURES if name.startswith('percentile_')},
            **{name: ('band must be a valid one-based index',)*2 for name in FAILURES if name.startswith('band_')},
            'complex': ('Complex-valued bands are not supported by numeric analysis',)*2}


class DiagnosticClient(rasterize.DiagnosticClient):
    diagnostic_name = 'statistics-percentile-submission-error.json'
    diagnostic_schema = 'addp.raster-statistics-percentile-submission-diagnostic/v1'


def encoded(value):
    return (json.dumps(value, sort_keys=True, ensure_ascii=False, allow_nan=False, indent=2)+'\n').encode()


def immutable(path, raw):
    if path.is_symlink(): raise SuiteError('symlink evidence is forbidden')
    if path.exists():
        if not path.is_file() or path.read_bytes() != raw: raise SuiteError('previous archived evidence changed')
    else:
        with path.open('xb') as stream: stream.write(raw)
        path.chmod(0o444)
    return {'path': path.name, 'size_bytes': len(raw), 'sha256': hashlib.sha256(raw).hexdigest()}


def definition(sources, name, *, invalid=None):
    if name not in fixture.CASES or invalid not in (None, *FAILURES) or (invalid and name != 'p25'):
        raise SuiteError('unknown exact whole-raster statistics case')
    case = fixture.CASES[name]; source = 'complex.tif' if invalid == 'complex' else case['source']
    params = {'input_raster': {'$ref': 'source'}, 'band': case['band']}
    if 'percentile' in case: params['percentile'] = case['percentile']
    if invalid and invalid.startswith('percentile_'):
        params['percentile'] = {'percentile_null': None, 'percentile_boolean': True, 'percentile_string': '25',
                                'percentile_array': [25], 'percentile_object': {'p': 25},
                                'percentile_low': -1, 'percentile_high': 101}[invalid]
    if invalid and invalid.startswith('band_'):
        params['band'] = {'band_zero': 0, 'band_high': 99, 'band_boolean': True, 'band_string': '2'}[invalid]
    tasks = [{'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': sources[source]}}]
    if case.get('stage') == 'reclassify':
        tasks.append({'id': 'classes', 'operator': 'raster_reclassify', 'depends_on': ['source'],
                      'params': {'input_raster': {'$ref': 'source'}, 'band': 2, 'rules': fixture.class_rules()}})
        params.update(input_raster={'$ref': 'classes'}, band=1)
    tasks.append({'id': 'analysis', 'operator': 'raster_statistics', 'depends_on': ['classes' if case.get('stage') else 'source'], 'params': params})
    return {'tasks': tasks}


def compare(actual, expected):
    if not isinstance(actual, dict) or actual.keys() != expected.keys(): raise SuiteError('complete statistics object keys differ')
    for key, target in expected.items():
        value = actual[key]
        if type(target) is float:
            if type(value) not in (int, float) or not math.isfinite(value): raise SuiteError('nonfinite or nonnumeric statistic')
            if key == 'percentile_value':
                equal = value == target if target == 0 or expected.get('percentile') in (0., 50., 100.) else math.isclose(value, target, rel_tol=4e-15, abs_tol=0.)
            elif key in ('percentile', 'min', 'max'):
                equal = value == target
            else:
                equal = math.isclose(value, target, rel_tol=1e-10, abs_tol=1e-12)
            if not equal: raise SuiteError('independent full-domain statistic differs: '+key)
        elif type(value) is not type(target) or value != target:
            raise SuiteError('exact statistic/count/null differs: '+key)


def fixture_call(repository, env, action, timeout):
    if action not in {'oracle', 'verify'}: raise SuiteError('unknown read-only physical action')
    result = subprocess.run([sys.executable, 'business/scripts/online-raster-statistics-percentile-fixture.py', action],
                            cwd=repository, env=dict(env), text=True, capture_output=True, timeout=min(150, timeout))
    if result.returncode: raise SuiteError('independent statistics physical verification failed: '+result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'physical statistics evidence')


def check_physical(native, baseline=None):
    if native.get('no_outputs') is not True or native.get('runtime_workspaces_removed') is not True:
        raise SuiteError('read-only publication or Runtime workspace residual')
    sources = obj(native.get('sources'), 'source hashes'); physical = obj(native.get('source_physical'), 'physical sources')
    archive = obj(native.get('archived_sources'), 'archived original sources')
    if set(sources) != set(fixture.SOURCES) or set(physical) != set(sources) or set(archive) != set(sources):
        raise SuiteError('complete source inventory differs')
    for name, fact in sources.items():
        if (set(fact) != {'size_bytes', 'sha256'} or type(fact['size_bytes']) is not int or fact['size_bytes'] <= 0
            or not isinstance(fact['sha256'], str) or not re.fullmatch('[0-9a-f]{64}', fact['sha256'])
            or physical[name] != {**fact, 'full_pixels_and_mask_verified': True, 'grid_crs_scale_verified': True}
            or any(physical[name].get(flag) is not True for flag in ('full_pixels_and_mask_verified', 'grid_crs_scale_verified'))
            or archive[name] != {'path': 'statistics-sources/'+name, **fact}):
            raise SuiteError('full source pixels/mask/grid/hash/archive evidence differs')
    if baseline is not None and any(native[key] != baseline[key] for key in ('sources', 'source_physical', 'archived_sources')):
        raise SuiteError('source or previous archived source changed')


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


def current_execution(execution, identity, identifier, dag, runtime, language, status):
    readonly.actor(execution, identity, 'develop')
    if (execution.get('execution_id') != identifier or type(execution.get('tenant_id')) is not int
        or execution['tenant_id'] != int(identity['tenant_id']) or execution.get('task_type') != 'workflow'
        or execution.get('status') != status): raise SuiteError('current execution identity/type/status differs')
    readonly.execution_snapshot(execution, dag, runtime)
    if execution['execution_config'].get('display_language') != language: raise SuiteError('submitted language snapshot differs')
    readonly.no_publication(execution)


def archive_response(directory, label, response):
    raw = response.raw
    def invalid_constant(value): raise SuiteError('nonfinite JSON constant in original response')
    if type(response.status) is not int or response.status != 200 or not isinstance(raw, bytes) or not raw:
        raise SuiteError('original response bytes are missing or differ from the parsed response')
    try: parsed = json.loads(raw, parse_constant=invalid_constant)
    except (ValueError, TypeError) as error: raise SuiteError('original response is not strict JSON') from error
    if parsed != response.payload: raise SuiteError('original response bytes differ from the parsed response')
    # Never archive headers/tokens or accept a credential-bearing professional projection.
    if any(word in raw.lower() for word in (b'access_token', b'secret_key', b'password', b'access_plan', b'workspace')):
        raise SuiteError('private fields in professional response')
    receipt = immutable(directory/(label+'.json'), raw)
    return {**receipt, 'http_status': response.status}


class BudgetClient:
    def __init__(self, client, remaining): self.client, self.remaining = client, remaining

    def request(self, *args, **kwargs):
        left = self.remaining(); original = getattr(self.client, 'timeout', None)
        if original is not None: self.client.timeout = min(original, left)
        try: response = self.client.request(*args, **kwargs)
        finally:
            if original is not None: self.client.timeout = original
        self.remaining()
        return response


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
        (directory/'statistics-percentile-progress.json').write_bytes(encoded({'suite': SUITE, 'result': status,
            'elapsed_ms': round((time.monotonic()-started)*1000), 'budget_ms': 600000, 'stages': stages}))
    progress('running')
    try:
        tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
        if tenant <= 1: raise SuiteError('statistics acceptance forbids the default tenant')
        identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
        engine = positive(env['ADDP_ONLINE_STATISTICS_PERCENTILE_NFS_ENGINE_ID'], 'NFS engine')
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
        matrix += [('p25', problem, language) for problem in FAILURES for language in ('zh-cn', 'en')]
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
                if name in ('p0', 'p100') and actual['percentile_value'] != actual['min' if name == 'p0' else 'max']:
                    raise SuiteError('percentile endpoint does not equal the corresponding statistic')
                if name == 'p33-reversed' and actual['percentile_value'] != cases[7]['json_result']['percentile_value']:
                    raise SuiteError('source ordering changed the exact percentile')
                if name in ('p0', 'p100', 'p25', 'p75', 'p33') and {k:v for k,v in actual.items() if not k.startswith('percentile')} != cases[0]['json_result']:
                    raise SuiteError('requested percentile changed original statistics')
                cases.append({'name': name, 'execution_id': identifier, 'source_locator': sources[fixture.CASES[name]['source']],
                              'json_result': actual, 'complete_object_verified': True})
            native = physical_runner(repository, env, 'verify', remaining()); remaining(); physical_calls += 1; check_physical(native, baseline)
            if scan_ids(client) != scans: raise SuiteError('read-only statistics added a scan')
            source_name = 'complex.tif' if problem == 'complex' else fixture.CASES[name]['source']
            if readonly.graph(client, items[source_name]['id']) != graphs[source_name]: raise SuiteError('read-only statistics changed source lineage')
            for previous_id, previous_result in prior:
                previous = client.request('GET', '/api/v1/develop/executions/'+previous_id, (200,)).payload
                readonly.actor(previous, identity, 'develop')
                if (previous.get('execution_id') != previous_id or type(previous.get('tenant_id')) is not int
                    or previous['tenant_id'] != tenant or previous.get('status') != 'success'
                    or raster.transient_result(previous) != previous_result): raise SuiteError('previous execution result changed')
                previous_checks += 1
            if not problem: prior.append((identifier, copy.deepcopy(actual)))
            immutable(directory/'statistics-expectations.json', encoded(expectations))
            stage.update(status='passed', execution_id=identifier); progress('running')
        if len(cases) != 16 or len(failures) != 24 or physical_calls != 41 or previous_checks != 504:
            raise SuiteError('all 40 calls, 41 physical checks and 504 previous-result checks are required')
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
        return {'schema_version': 'addp.raster-statistics-percentile-workflow/v1', 'suite': SUITE, 'result': 'passed',
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
    immutable(Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'statistics-percentile-workflow.json', encoded(result))
    print(json.dumps(result, ensure_ascii=False, allow_nan=False))


if __name__ == '__main__':
    try: main()
    except Exception as error:
        print(f'whole-raster statistics acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}', file=sys.stderr)
        raise SystemExit(1)
