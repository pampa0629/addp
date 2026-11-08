#!/usr/bin/env python3
"""Sixteen complete CSVs, thirty-six exact bilingual failures and 712 actual rereads."""
from __future__ import annotations

import base64
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
csv_flow = importlib.import_module('scripts.test.raster-zonal-statistics-persistence-online')
fixture = importlib.import_module('business.scripts.online-raster-line-of-sight-fixture')
lineage = importlib.import_module('scripts.utils.online_raster_lineage')
raster, support, readonly = base.raster, base.support, base.readonly
obj, array, positive, SuiteError = base.obj, base.array, base.positive, base.SuiteError
SubmittedLanguageClient, BudgetClient = base.SubmittedLanguageClient, base.BudgetClient
encoded, immutable = base.encoded, base.immutable
SUITE = fixture.SUITE

# Each case is an ordinary JSON request that must reach the actual sight node.
FAILURES = {
    'observer_short': ('xy', 'flat.tif', {'observer_xy': [.5]}),
    'target_long': ('xy', 'flat.tif', {'target_xy': [4.5, 2.5, 1.]}),
    'observer_negative': ('height', 'flat.tif', {'observer_height_m': -1.}),
    'target_negative': ('height', 'flat.tif', {'target_height_m': -1.}),
    'coincident': ('coincident', 'flat.tif', {'target_xy': [.5, 2.5]}),
    'observer_outside': ('outside', 'flat.tif', {'observer_xy': [-.5, 2.5]}),
    'target_outside': ('outside', 'flat.tif', {'target_xy': [6., 2.5]}),
    'observer_invalid': ('invalid', 'nodata.tif', {'observer_xy': [2.5, 2.5]}),
    'target_invalid': ('invalid', 'mask.tif', {'target_xy': [2.5, 2.5]}),
    'band_zero': ('band', 'flat.tif', {'band': 0}),
    'band_high': ('band', 'flat.tif', {'band': 4}),
    'band_alpha': ('alpha', 'alpha.tif', {'band': 3}),
    'geographic': ('grid', 'geographic.tif', {}),
    'rotated': ('grid', 'rotated.tif', {}),
    'south_up': ('grid', 'south-up.tif', {}),
    'unit_invalid': ('unit', 'flat.tif', {'elevation_unit': 'yard'}),
    'unit_conflict': ('unit-conflict', 'flat.tif', {'elevation_unit': 'foot'}),
    'scale': ('scale', 'scaled.tif', {}),
}
ERRORS = {
    'xy': ('DEM 两点通视的每个端点必须是两个有限投影坐标 [x,y]', 'DEM line of sight requires two finite projected coordinates [x,y] for each endpoint'),
    'height': ('DEM 两点通视的两端离地高度必须是有限非负米数', 'DEM line of sight requires finite nonnegative heights above ground in metres'),
    'coincident': ('DEM 两点通视的观察点和目标点不能重合', 'DEM line of sight requires distinct observer and target locations'),
    'outside': ('DEM 两点通视的端点超出栅格范围', 'DEM line of sight endpoint is outside the raster'),
    'invalid': ('DEM 两点通视的端点落入无效高程像元', 'DEM line of sight endpoint has no valid elevation'),
    'band': ('DEM 两点通视必须显式选择有效的一基高程数据波段', 'DEM line of sight requires an explicit valid one-based elevation band'),
    'alpha': ('DEM 两点通视不能选择 alpha 波段作为高程', 'DEM line of sight cannot use an alpha band as elevation'),
    'grid': ('DEM 两点通视需要已知线性单位的二维投影及保持北向轴手性的正交网格，请显式重投影', 'DEM line of sight requires a two-dimensional projected orthogonal grid with known linear units and north-up handedness; reproject explicitly'),
    'unit': ('DEM 两点通视必须明确声明 metre、foot 或 us_survey_foot 高程单位', 'DEM line of sight requires elevation_unit metre, foot or us_survey_foot'),
    'unit-conflict': ('DEM 两点通视高程波段单位未知或与声明单位冲突', 'DEM line of sight band unit is unknown or conflicts with elevation_unit'),
    'scale': ('DEM 两点通视前必须显式应用高程 scale/offset', 'DEM line of sight requires elevation scale/offset to be applied first'),
}


class DiagnosticClient(base.DiagnosticClient):
    diagnostic_name = 'line-of-sight-submission-error.json'
    diagnostic_schema = 'addp.raster-line-of-sight-submission-diagnostic/v1'


def definition(sources, parent, name, *, invalid=None, language='zh-cn'):
    if (name not in fixture.CASES or invalid not in (None, *FAILURES)
            or language not in ('zh-cn', 'en') or (invalid and name != 'flat.csv')):
        raise SuiteError('unexpected line-of-sight case')
    source = name.replace('.csv', '.tif')
    params = fixture.parameters(name)
    if invalid:
        _, source, changed = FAILURES[invalid]
        params.update(changed)
    return {'tasks': [
        {'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': sources[source]}},
        {'id': 'sight', 'operator': 'raster_line_of_sight', 'depends_on': ['source'],
         'params': {'input_raster': {'$ref': 'source'}, **params}},
        {'id': 'save', 'operator': 'save', 'depends_on': ['sight'],
         'params': {'input_df': {'$ref': 'sight'}, 'target_parent_locator': parent, 'mode': 'fail',
                    'target_name': f'failed-{invalid}-{language}.csv' if invalid else name}},
    ]}


def expected_lineage(source, target):
    return {'schema_version': 'addp.lineage-facts/v1',
            'inputs': [{'port': 'input.source', 'locator': source}],
            'outputs': [{'port': 'output.save', 'locator': target, 'write_mode': 'create'}],
            'operations': [{'kind': 'derive', 'operator': 'save', 'input_ports': ['input.source'],
                            'output_ports': ['output.save']}]}


def validate_preview(preview, expected):
    preview = obj(preview, 'table preview')
    if (preview.get('type') != 'pandas_dataframe' or preview.get('schema') != fixture.SCHEMA
            or type(preview.get('row_count')) is not int or preview['row_count'] != 1
            or type(preview.get('preview_limit')) is not int or preview['preview_limit'] != 50
            or preview.get('truncated') is not False or preview.get('preview_rows') != [expected]
            or type(preview['preview_rows'][0]['distance_m']) not in (int, float)):
        raise SuiteError('complete bounded CSV preview differs')


def physical(repository, env, action, names, timeout):
    result = subprocess.run(
        [sys.executable, 'business/scripts/online-raster-line-of-sight-fixture.py', action, *names],
        cwd=repository, env=env, capture_output=True, text=True, timeout=min(150, timeout))
    if result.returncode:
        raise SuiteError('owned DEM/CSV physical verification failed: '+result.stderr[-1200:])
    return obj(json.loads(result.stdout), 'physical report')


def verify_sources(facts):
    if set(facts) != set(fixture.SOURCES):
        raise SuiteError('physical source inventory differs')
    for fact in facts.values():
        if (type(fact.get('size_bytes')) is not int or fact['size_bytes'] <= 0
                or not isinstance(fact.get('sha256'), str) or not re.fullmatch('[0-9a-f]{64}', fact['sha256'])):
            raise SuiteError('invalid source size or SHA-256')


def check_and_archive(report, names, sources, frozen, archive, previous):
    if report.get('runtime_workspaces_removed') is not True or report.get('sources') != sources:
        raise SuiteError('source bytes changed or Runtime workspace cleanup proof is missing')
    targets = obj(report.get('targets'), 'physical targets')
    if set(targets) != set(names) or {p.name for p in archive.iterdir()} != set(previous):
        raise SuiteError('physical or archived CSV inventory differs')
    receipts = {}
    for name, fact in targets.items():
        try:
            payload = base64.b64decode(fact.get('csv_base64', ''), validate=True)
            row = fixture.parse_csv(payload, frozen[name]['row'])
        except (ValueError, fixture.FixtureError) as error:
            raise SuiteError(f'complete original CSV differs: {name}') from error
        digest = fixture.fingerprint(payload)
        if (type(fact.get('size_bytes')) is not int or fact['size_bytes'] <= 0
                or any(fact.get(key) != value for key, value in digest.items())):
            raise SuiteError('physical CSV size or SHA-256 differs from raw bytes')
        receipt = {'path': archive.name+'/'+name, **digest, 'row': row}
        if name in previous and previous[name] != receipt:
            raise SuiteError('previous CSV changed')
        immutable(archive/name, payload)
        receipts[name] = receipt
    return receipts


def diagnostic_fact(identifier, problem, language):
    return {'execution_id': identifier, 'task': 'sight', 'problem': problem, 'language': language,
            'exact_cause': ERRORS[FAILURES[problem][0]][language == 'en'], 'source': 'develop_runtime_log'}


def invalid_diagnostic(repository, env, identifier, problem, language, timeout):
    expected = diagnostic_fact(identifier, problem, language)
    root = Path(env.get('ADDP_RUNTIME_LOG_ROOT', repository/'logs/runtime'))
    if not root.is_absolute():
        root = repository/root
    deadline = time.monotonic()+timeout
    while time.monotonic() < deadline:
        for path in (root/'develop').glob('*/*.jsonl'):
            for line in path.read_text().splitlines():
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    continue
                message = event.get('message', '')
                if (event.get('module_name') == 'develop' and event.get('role') == 'backend'
                        and f'[DevExecutor] 工作流引擎调用失败: execution_id={identifier} err=' in message
                        and message.endswith('任务 sight 执行失败: '+expected['exact_cause'])):
                    return expected
        time.sleep(min(1, max(0, deadline-time.monotonic())))
    raise SuiteError('missing exact line-of-sight node and submitted-language diagnostic')


def active_derivations(client, item, direction):
    graph = obj(client.request('GET', f'/api/v1/meta/lineage/graph?subject_kind=data_item&item_id={item}&direction={direction}',
                               (200,)).payload, 'source lineage graph')
    if graph.get('truncated') is not False:
        raise SuiteError('Meta lineage graph is incomplete')
    return [edge for edge in array(graph.get('edges'), 'edges')
            if edge.get('status') == 'active' and edge.get('relation_kind') == 'derive']


def source_edges(client, item):
    return sorted(json.dumps(edge, sort_keys=True) for edge in active_derivations(client, item, 'downstream'))


def item_snapshot(client, item):
    return obj(client.request('GET', f'/api/v1/meta/items/{item}', (200,)).payload, 'Meta item')


def current(execution, identity, identifier, dag, runtime, language, status):
    readonly.actor(execution, identity, 'develop')
    readonly.execution_snapshot(execution, dag, runtime)
    if (execution.get('execution_id') != identifier or type(execution.get('tenant_id')) is not int
            or execution['tenant_id'] != int(identity['tenant_id']) or execution.get('task_type') != 'workflow'
            or execution.get('status') != status or execution['execution_config'].get('display_language') != language):
        raise SuiteError('current execution identity, status or submitted language differs')


def run_scenario(repository, env, client, physical_runner=physical, diagnostic_runner=invalid_diagnostic,
                 lineage_reader=lineage.read_current):
    started = time.monotonic()
    budget = float(env.get('ADDP_ONLINE_TEST_TIMEOUT_SECONDS', '600'))
    if budget != 600:
        raise SuiteError('line-of-sight business budget must be exactly 600 seconds')

    def remaining():
        left = budget-(time.monotonic()-started)
        if left <= 0:
            raise SuiteError('line-of-sight business budget exhausted')
        return left

    client = BudgetClient(client, remaining)
    directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    directory.mkdir(parents=True, exist_ok=True)
    archive = directory/'line-of-sight-csv'
    archive.mkdir(exist_ok=False)
    stages = []

    def progress(status):
        (directory/'line-of-sight-progress.json').write_bytes(encoded({
            'suite': SUITE, 'result': status, 'budget_ms': 600000,
            'elapsed_ms': round((time.monotonic()-started)*1000), 'stages': stages,
        }))

    def record_terminal(identifier, dag, language, status):
        execution = raster.wait_execution(client, 'develop', identifier, remaining(), expected=status)
        current(execution, identity, identifier, dag, runtime, language, status)
        monitor = client.request('GET', '/api/v1/monitor/executions/by-execution-id/'+identifier, (200,)).payload
        readonly.actor(monitor, identity, 'develop')
        if (monitor.get('execution_id') != identifier or type(monitor.get('tenant_id')) is not int
                or monitor['tenant_id'] != tenant or monitor.get('status') != status):
            raise SuiteError('Monitor execution identity or terminal state differs')
        case = dag['tasks'][-1]['params']['target_name']
        immutable(directory/f'develop-{case}.json', encoded(execution))
        immutable(directory/f'monitor-{case}.json', encoded(monitor))
        return execution, monitor

    progress('running')
    try:
        tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
        if tenant <= 1:
            raise SuiteError('line-of-sight acceptance forbids the default tenant')
        identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
        principal = int(identity['principal_id'])
        engine = positive(env['ADDP_ONLINE_LINE_OF_SIGHT_NFS_ENGINE_ID'], 'NFS engine')
        scopes = {name: f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
        scan = support.wait_for_meta_scan(client, engine, time.monotonic()+remaining(), scope={'targets': list(scopes.values())})
        items = {name: support.find_fixture_item(client, engine, name, 'line-of-sight DEM source') for name in scopes}
        sources = {name: scopes[name]+f'&item_id={items[name]["id"]}' for name in scopes}
        before = {name: item_snapshot(client, item['id']) for name, item in items.items()}
        immutable(directory/'source-meta-before.json', encoded(before))
        scanned = raster.wait_execution(client, 'meta', scan, remaining())
        source_scan = base.validate_scan(scanned, scan, None, tenant, principal, engine)
        immutable(directory/'meta-source-scan.json', encoded(scanned))
        if base.scan_ids(client) != [scan]:
            raise SuiteError('unexpected source initialization scan')
        runtimes = array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'Runtime engines')
        selected = [value for value in runtimes if value.get('engine_type') == 'geopython_workflow'
                    and value.get('connection_status') == 'online']
        if len(selected) != 1:
            raise SuiteError('requires exactly one online same-source product Runtime')
        runtime = positive(selected[0].get('id'), 'Runtime')
        parent = f'addp://engine/{engine}/path/outputs?type=directory'

        baseline = physical_runner(repository, env, 'oracle', [], remaining())
        frozen = obj(baseline.get('expectations'), 'frozen independent reference')
        source_bytes = obj(baseline.get('sources'), 'physical sources')
        verify_sources(source_bytes)
        if (set(frozen) != set(fixture.CASES) or any(frozen[n]['row']['status'] != status
                                                    for n, status in fixture.CASES.items())):
            raise SuiteError('sixteen frozen independent three-state references required')
        oracle = immutable(directory/'line-of-sight-expectations.json', encoded(frozen))
        check_and_archive(baseline, [], source_bytes, frozen, archive, {})
        if any(type(before[n].get('size_bytes')) is not int or before[n]['size_bytes'] != fact['size_bytes']
               for n, fact in source_bytes.items()):
            raise SuiteError('source physical and Meta sizes differ')

        cases, failures, names, scans = [], [], [], []
        target_metadata, previous = {}, {}
        full_checks, physical_calls = 0, 1
        execution_ids = set()

        def submit(dag, language, status):
            identifier = raster.submit(SubmittedLanguageClient(client, language), runtime, dag,
                                       execution_timeout=min(180, max(1, int(remaining()))))
            if identifier in execution_ids:
                raise SuiteError('duplicate Develop execution identity')
            execution_ids.add(identifier)
            execution, monitor = record_terminal(identifier, dag, language, status)
            return identifier, execution, monitor

        for index, name in enumerate(fixture.TARGETS):
            stages.append({'case': name, 'status': 'running'})
            progress('running')
            language = 'zh-cn' if index % 2 == 0 else 'en'
            dag = definition(sources, parent, name)
            identifier, execution, monitor = submit(dag, language, 'success')
            source_name = name.replace('.csv', '.tif')
            source = sources[source_name]
            target = f'addp://engine/{engine}/path/outputs/{name}?type=file'
            expected = expected_lineage(source, target)
            facts, automatic_scan, result = csv_flow.validate_success(execution, [source], target)
            lineage.validate(facts, expected, public=True)
            proof = lineage.collect(env, SUITE, identifier, name, expected, execution, tenant, principal, dag, lineage_reader)
            if obj(monitor.get('metadata'), 'Monitor metadata').get('lineage_facts') != facts:
                raise SuiteError('Monitor current public lineage differs')
            validate_preview(result.get('final_result'), frozen[name]['preview_row'])
            if automatic_scan in [scan, *scans]:
                raise SuiteError('duplicate automatic CSV scan')
            scans.append(automatic_scan)
            scanned = raster.wait_execution(client, 'meta', automatic_scan, remaining())
            scan_identity = base.validate_scan(scanned, automatic_scan, identifier, tenant, principal, engine)
            immutable(directory/f'meta-{name}.json', encoded(scanned))
            names.append(name)
            native = physical_runner(repository, env, 'verify', names, remaining())
            previous = check_and_archive(native, names, source_bytes, frozen, archive, previous)
            physical_calls += 1
            full_checks += len(names)
            hit = support.find_fixture_item(client, engine, 'outputs/'+name, 'line-of-sight CSV')
            item = item_snapshot(client, hit['id'])
            descriptor = obj(obj(item.get('attributes'), 'Meta attributes').get('item'), 'Meta file')
            if (item.get('id') != hit['id'] or hit['id'] in target_metadata or not item.get('scanned_at')
                    or item.get('item_type') != 'file' or item.get('full_name') != 'outputs/'+name
                    or descriptor.get('format') != 'csv' or type(item.get('size_bytes')) is not int
                    or item['size_bytes'] != previous[name]['size_bytes']):
                raise SuiteError('CSV identity or equal positive physical/Meta size differs')
            target_metadata[hit['id']] = item
            immutable(directory/f'target-{name}-meta-before.json', encoded(item))
            source_id = items[source_name]['id']
            relation = raster.wait_lineage(client, source_id, hit['id'], identifier, remaining())
            edges = active_derivations(client, hit['id'], 'upstream')
            if len(edges) != 1 or (edges[0].get('source', {}).get('item_id'),
                                   edges[0].get('target', {}).get('item_id'),
                                   edges[0].get('evidence', {}).get('execution_id')) != (source_id, hit['id'], identifier):
                raise SuiteError('Meta must expose exactly one current DEM-to-CSV derivation')
            cases.append({'name': name, 'execution_id': identifier, 'target_item_id': hit['id'],
                          'target_locator': target, 'automatic_scan_execution_id': automatic_scan,
                          'scan_identity': scan_identity, 'lineage_evidence': proof, 'meta_relation': relation,
                          'physical_size_bytes': previous[name]['size_bytes'], 'meta_size_bytes': item['size_bytes']})
            stages[-1]['status'] = 'passed'
            progress('running')

        expected_scans = sorted([scan, *scans])
        if base.scan_ids(client) != expected_scans:
            raise SuiteError('unexpected successful scan inventory')
        edges = {name: source_edges(client, item['id']) for name, item in items.items()}
        for problem in FAILURES:
            for language in ('zh-cn', 'en'):
                stages.append({'problem': problem, 'language': language, 'status': 'running'})
                progress('running')
                dag = definition(sources, parent, 'flat.csv', invalid=problem, language=language)
                identifier, execution, monitor = submit(dag, language, 'failed')
                for terminal in (execution, monitor):
                    readonly.no_publication(terminal)
                    if obj(terminal.get('error_details'), 'terminal error').get('category') != 'execution_failed':
                        raise SuiteError('wrong failed execution category')
                diagnostic = diagnostic_runner(repository, env, identifier, problem, language, remaining())
                if diagnostic != diagnostic_fact(identifier, problem, language):
                    raise SuiteError('wrong exact failure node, language or cause')
                opposite = SubmittedLanguageClient(client, 'en' if language == 'zh-cn' else 'zh-cn')
                queried = opposite.request('GET', '/api/v1/develop/executions/'+identifier, (200,)).payload
                if any(queried.get(key) != execution.get(key) for key in ('error_msg', 'error_details', 'execution_config')):
                    raise SuiteError('query language rewrote stored failure')
                if base.scan_ids(client) != expected_scans:
                    raise SuiteError('failed execution added an automatic scan')
                if any(source_edges(client, item['id']) != edges[n] for n, item in items.items()):
                    raise SuiteError('failed execution added derived lineage')
                native = physical_runner(repository, env, 'verify', names, remaining())
                previous = check_and_archive(native, names, source_bytes, frozen, archive, previous)
                physical_calls += 1
                full_checks += len(names)
                failures.append({**diagnostic, 'no_publication_or_scan_or_lineage': True})
                stages[-1]['status'] = 'passed'
                progress('running')

        if (len(cases), len(failures), full_checks, physical_calls) != (16, 36, 712, 53):
            raise SuiteError('requires 16 successful chains, 36 failures and 712 actual CSV checks')
        final_targets = {key: item_snapshot(client, key) for key in target_metadata}
        final_sources = {name: item_snapshot(client, item['id']) for name, item in items.items()}
        immutable(directory/'target-meta-after.json', encoded(final_targets))
        immutable(directory/'source-meta-after.json', encoded(final_sources))
        if final_targets != target_metadata or final_sources != before:
            raise SuiteError('source or prior target Meta snapshot changed')
        lineage.verify_archive(env, SUITE, cases)
        csv_manifest = {name: {key: value for key, value in fact.items() if key != 'row'} for name, fact in previous.items()}
        manifest = immutable(directory/'line-of-sight-csv-manifest.json', encoded(csv_manifest))
        progress('passed')
        remaining()
    except BaseException:
        progress('failed')
        raise
    return {'schema_version': 'addp.'+SUITE+'/v1', 'suite': SUITE, 'result': 'passed',
            'run_id': env['ADDP_ONLINE_TEST_RUN_ID'], 'identity': identity, 'runtime_engine_id': runtime,
            'sources': sources, 'physical_sources': source_bytes, 'source_scan_identity': source_scan,
            'cases': cases, 'failed_parameters': failures, 'archived_csvs': csv_manifest,
            'manifest': manifest, 'oracle': oracle, 'source_and_target_metadata_unchanged': True,
            'full_target_checks': full_checks, 'physical_verification_calls': physical_calls,
            'elapsed_ms': round((time.monotonic()-started)*1000), 'business_budget_ms': 600000}


def main():
    env = dict(os.environ)
    client = DiagnosticClient(env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30,
                              env['ADDP_ONLINE_ARTIFACT_DIR'])
    report = run_scenario(Path(__file__).resolve().parents[2], env, client)
    immutable(Path(env['ADDP_ONLINE_ARTIFACT_DIR'])/'line-of-sight-persistence.json', encoded(report))
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'Line-of-sight acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}',
              file=sys.stderr)
        raise SystemExit(1)
