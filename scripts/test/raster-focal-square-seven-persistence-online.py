#!/usr/bin/env python3
"""Eighteen COGs, 60 accurate failures, and 1251 full physical checks in 600s."""
from __future__ import annotations

import copy
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
circle = importlib.import_module('scripts.test.raster-focal-circle-weighted-persistence-online')
percentiles = importlib.import_module('scripts.test.raster-focal-percentiles-persistence-online')
weighted_mean = importlib.import_module('scripts.test.raster-focal-weighted-mean-persistence-online')
weighted_stddev = importlib.import_module('scripts.test.raster-focal-weighted-stddev-persistence-online')
weighted_sum = importlib.import_module('scripts.test.raster-focal-weighted-sum-persistence-online')
variety = importlib.import_module('scripts.test.raster-focal-variety-persistence-online')
fixture = importlib.import_module('business.scripts.online-raster-focal-square-seven-fixture')
lineage = importlib.import_module('scripts.utils.online_raster_lineage')
raster, support, readonly = base.raster, base.support, base.readonly
obj, array, positive, SuiteError = base.obj, base.array, base.positive, base.SuiteError
SubmittedLanguageClient, BudgetClient = base.SubmittedLanguageClient, base.BudgetClient
encoded, immutable, archive_response = base.encoded, base.immutable, base.archive_response
source_edges = circle.source_edges
current = percentiles.current
SUITE, FAILURES = fixture.SUITE, fixture.FAILURES

WINDOWS = {'window_boolean': True, 'window_string': '7', 'window_null': None, 'window_even': 4, 'window_nine': 9}
LISTS = {
    'list_null': None,
    'list_empty': [],
    'list_duplicate': [50.0, 50.0],
    'list_descending': [75.0, 25.0],
    'list_range': [101.0],
}
BANDS = {'band_zero': 0, 'band_high': 99, 'band_boolean': True, 'band_alpha': 4}
WEIGHT_METHOD = {
    'mean_weights_size': 'weighted_mean',
    'stddev_weights_size': 'weighted_stddev',
    'sum_weights_size': 'weighted_sum',
    'weights_missing': 'weighted_mean',
    'weights_zero': 'weighted_stddev',
    'weights_negative': 'weighted_sum',
    'weights_boolean': 'weighted_mean',
    'weights_ragged': 'weighted_stddev',
}
WEIGHT_MESSAGES = {
    'weighted_mean': weighted_mean.MESSAGES['weights_ragged'],
    'weighted_stddev': weighted_stddev.MESSAGES['weights_ragged'],
    'weighted_sum': weighted_sum.MESSAGES['weights_ragged'],
}
MESSAGES = {
    **dict.fromkeys(('window_missing', *WINDOWS), percentiles.MESSAGES['square_size']),
    'square_radius': percentiles.MESSAGES['square_radius'],
    'method': circle.MESSAGES['method'],
    **dict.fromkeys(('list_missing', *LISTS), percentiles.MESSAGES['list_missing']),
    'unused_percentiles': percentiles.MESSAGES['unused'],
    **{problem: WEIGHT_MESSAGES[method] for problem, method in WEIGHT_METHOD.items()},
    'unused_weights': circle.MESSAGES['unused_weights'],
    **{problem: percentiles.MESSAGES[problem] for problem in BANDS},
    'mode_fractional': variety.MESSAGES['fractional'],
    'variety_unsafe': variety.MESSAGES['positive_unsafe'],
}


class DiagnosticClient(base.DiagnosticClient):
    diagnostic_name = 'raster-focal-square-seven-submission-error.json'
    diagnostic_schema = 'addp.raster-focal-square-seven-submission-diagnostic/v1'


def case_inputs(name):
    return [fixture.CASES[name][0]]


def definition(sources, parent, name, *, invalid=None, language='zh-cn'):
    if (
        name not in fixture.CASES
        or invalid not in (None, *FAILURES)
        or language not in ('zh-cn', 'en')
        or (invalid and name != 'mean.cog.tif')
    ):
        raise SuiteError('unknown exact seven-pixel case')
    source, size, method, kernel, reclassify = fixture.CASES[name]
    if invalid == 'mode_fractional':
        source, method = 'fractional.tif', 'mode'
    elif invalid == 'variety_unsafe':
        source, method = 'unsafe.tif', 'variety'

    tasks = [{'id': 'source', 'operator': 'raster_load', 'depends_on': [], 'params': {'locator': sources[source]}}]
    prior, band = 'source', 2
    if reclassify:
        tasks.append(
            {
                'id': 'classes',
                'operator': 'raster_reclassify',
                'depends_on': ['source'],
                'params': {'input_raster': {'$ref': 'source'}, 'band': 2, 'rules': fixture.class_rules()},
            }
        )
        prior, band = 'classes', 1

    params = {
        'input_raster': {'$ref': prior},
        'band': band,
        'method': method,
        'window_type': 'square_pixels',
        'window_size': size,
    }
    if method == 'percentile':
        params['percentiles'] = list(fixture.PERCENTILES)
    if kernel:
        params['weights'] = [list(row) for row in fixture.KERNELS[kernel]]
    if invalid == 'window_missing':
        params.pop('window_size')
    elif invalid in WINDOWS:
        params['window_size'] = WINDOWS[invalid]
    elif invalid == 'square_radius':
        params['radius_metres'] = 1.0
    elif invalid == 'method':
        params['method'] = 'invalid-method'
    elif invalid == 'list_missing':
        params['method'] = 'percentile'
    elif invalid in LISTS:
        params.update(method='percentile', percentiles=LISTS[invalid])
    elif invalid == 'unused_percentiles':
        params['percentiles'] = [50.0]
    elif invalid in WEIGHT_METHOD:
        params['method'] = WEIGHT_METHOD[invalid]
        weights = [list(row) for row in fixture.ASYMMETRIC]
        if invalid.endswith('_size'):
            weights = [[1] * 5 for _ in range(5)]
        elif invalid == 'weights_zero':
            weights = [[0] * 7 for _ in range(7)]
        elif invalid == 'weights_negative':
            weights[0][0] = -1.0
        elif invalid == 'weights_boolean':
            weights[0][0] = True
        elif invalid == 'weights_ragged':
            weights[0].pop()
        if invalid != 'weights_missing':
            params['weights'] = weights
    elif invalid == 'unused_weights':
        params['weights'] = [[1] * 7 for _ in range(7)]
    elif invalid in BANDS:
        params['band'] = BANDS[invalid]

    tasks.extend(
        [
            {'id': 'focal', 'operator': 'raster_focal_statistics', 'depends_on': [prior], 'params': params},
            {
                'id': 'save',
                'operator': 'raster_save',
                'depends_on': ['focal'],
                'params': {
                    'input_raster': {'$ref': 'focal'},
                    'target_parent_locator': parent,
                    'target_name': f'failed-{invalid}-{language}.cog.tif' if invalid else name,
                    'write_mode': 'create',
                    'profile': 'cog',
                    'blocksize': fixture.BLOCK_SIZE,
                    'overview_resampling': 'nearest',
                },
            },
        ]
    )
    return {'tasks': tasks}


def expected_lineage(sources, name, target):
    return {
        'schema_version': 'addp.lineage-facts/v1',
        'inputs': [{'port': 'input.source', 'locator': sources[case_inputs(name)[0]]}],
        'outputs': [{'port': 'output.save', 'locator': target, 'write_mode': 'create'}],
        'operations': [
            {
                'kind': 'derive',
                'operator': 'raster_save',
                'input_ports': ['input.source'],
                'output_ports': ['output.save'],
            }
        ],
    }


def check_physical(value, sources, previous, anchor, names):
    if (
        value.get('runtime_workspaces_removed') is not True
        or value.get('full_source_pixels_verified_at_baseline') is not True
        or value.get('sources') != sources
        or set(obj(value.get('targets'), 'targets')) != set(names)
        or type(value.get('full_cog_checks')) is not int
        or value['full_cog_checks'] != len(names)
        or value.get('oracle_sha256') != anchor
    ):
        raise SuiteError('complete source/COG/count/cleanup/oracle proof differs')
    oracle = obj(value.get('archived_oracle'), 'original oracle')
    if (
        set(oracle) != {fixture.ORACLE}
        or oracle[fixture.ORACLE].get('sha256') != anchor
        or oracle[fixture.ORACLE].get('path') != 'raster-focal-square-seven-oracle/' + fixture.ORACLE
        or type(oracle[fixture.ORACLE].get('size_bytes')) is not int
        or oracle[fixture.ORACLE]['size_bytes'] <= 0
    ):
        raise SuiteError('frozen independent oracle archive differs')
    archives = obj(value.get('archived_cogs'), 'original COGs')
    if set(archives) != set(names) or set(obj(value.get('archived_sources'), 'original sources')) != set(
        fixture.SOURCES
    ):
        raise SuiteError('archive inventory differs')
    for n, fact in sources.items():
        if (
            type(fact.get('size_bytes')) is not int
            or fact['size_bytes'] <= 0
            or not isinstance(fact.get('sha256'), str)
            or not re.fullmatch('[0-9a-f]{64}', fact['sha256'])
            or value['archived_sources'][n] != {'path': 'raster-focal-square-seven-sources/' + n, **fact}
        ):
            raise SuiteError('source original size/hash differs')
    flags = (
        'cog_valid',
        'full_pixel_and_mask_verification',
        'independent_scalar_oracle_verified',
        'band_order_and_descriptions_verified',
        'nearest_overviews_verified',
    )
    if value.get('actual_control_pairs_verified') != fixture.actual_pairs(names):
        raise SuiteError('actual cross-output comparisons missing')
    for n, fact in value['targets'].items():
        if (
            fact.get('band_count') != fixture.band_count(n)
            or type(fact.get('band_count')) is not int
            or fact.get('band_descriptions') != fixture.descriptions(n)
            or any(fact.get(k) is not True for k in flags)
            or type(fact.get('size_bytes')) is not int
            or fact['size_bytes'] <= 0
            or not isinstance(fact.get('sha256'), str)
            or not re.fullmatch('[0-9a-f]{64}', fact['sha256'])
            or type(fact.get('overview_count')) is not int
            or fact['overview_count'] != fixture.overview_count(n)
            or fact.get('has_overviews') is not bool(fixture.overview_count(n))
            or archives[n]
            != {'path': 'raster-focal-square-seven-cogs/' + n, **{k: fact[k] for k in ('size_bytes', 'sha256')}}
        ):
            raise SuiteError('complete original COG/overview/size/hash differs')
        if n in previous and fact != previous[n]:
            raise SuiteError('previous complete COG changed')


def invalid_diagnostic(repository, env, identifier, problem, language, timeout):
    cause = MESSAGES[problem][language == 'en']
    root = Path(env.get('ADDP_RUNTIME_LOG_ROOT', repository / 'logs/runtime'))
    if not root.is_absolute():
        root = repository / root
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        for path in (root / 'develop').glob('*/*.jsonl'):
            for line in path.read_text().splitlines():
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    continue
                message = event.get('message', '')
                if (
                    event.get('module_name') == 'develop'
                    and event.get('role') == 'backend'
                    and f'[DevExecutor] 工作流引擎调用失败: execution_id={identifier} err=' in message
                    and message.endswith('任务 focal 执行失败: ' + cause)
                ):
                    return {
                        'execution_id': identifier,
                        'task': 'focal',
                        'problem': problem,
                        'language': language,
                        'exact_cause': cause,
                        'source': 'develop_runtime_log',
                    }
        time.sleep(min(1, max(0, deadline - time.monotonic())))
    raise SuiteError('missing exact seven-pixel square task and cause')


def fixture_call(repository, env, action, names, timeout):
    if action not in ('oracle', 'verify', 'cleanup-oracle'):
        raise SuiteError('unknown oracle action')
    result = subprocess.run(
        [sys.executable, 'business/scripts/online-raster-focal-square-seven-fixture.py', action, *names],
        cwd=repository,
        env=dict(env),
        capture_output=True,
        text=True,
        timeout=min(150, timeout),
    )
    if result.returncode:
        raise SuiteError('independent seven-pixel square COG verification failed: ' + result.stderr[-1000:])
    return obj(json.loads(result.stdout), 'seven-pixel square physical evidence')


def run_scenario(
    repository,
    env,
    client,
    physical_runner=fixture_call,
    diagnostic_runner=invalid_diagnostic,
    lineage_reader=lineage.read_current,
):
    env = dict(env)
    now = time.monotonic()
    started = float(env['ADDP_ONLINE_FOCAL_SQUARE_SEVEN_STARTED'])
    budget = float(env['ADDP_ONLINE_TEST_TIMEOUT_SECONDS'])
    if not math.isfinite(started) or not 0 < started <= now:
        raise SuiteError('business start must bind the initialization deadline')
    if not math.isfinite(budget) or budget != 600:
        raise SuiteError('seven-pixel square business budget must be exactly 600 seconds')

    def remaining():
        left = budget - (time.monotonic() - started)
        if left <= 0:
            raise SuiteError('seven-pixel square business budget exhausted')
        return left

    client = BudgetClient(client, remaining)
    directory = Path(env['ADDP_ONLINE_ARTIFACT_DIR'])
    directory.mkdir(parents=True, exist_ok=True)
    (directory / 'focal-square-seven-terminal-responses').mkdir()
    stages = []
    primary = cleanup_error = report = None

    def progress(status):
        (directory / 'raster-focal-square-seven-progress.json').write_bytes(
            encoded(
                {
                    'suite': SUITE,
                    'result': status,
                    'budget_ms': 600000,
                    'elapsed_ms': round((time.monotonic() - started) * 1000),
                    'stages': stages,
                }
            )
        )

    try:
        remaining()
        tenant = positive(env['ADDP_ONLINE_TEST_TENANT_ID'], 'tenant')
        if tenant <= 1:
            raise SuiteError('seven-pixel square acceptance forbids default tenant')
        identity = raster.validate_identity(client, tenant, required_permissions=raster.EDITOR_PERMISSIONS)
        engine = positive(env['ADDP_ONLINE_FOCAL_SQUARE_SEVEN_NFS_ENGINE_ID'], 'NFS engine')
        scopes = {name: f'addp://engine/{engine}/path/{name}?type=file' for name in fixture.SOURCES}
        scan = support.wait_for_meta_scan(
            client, engine, time.monotonic() + remaining(), scope={'targets': list(scopes.values())}
        )
        items = {
            name: support.find_fixture_item(client, engine, name, 'seven-pixel square source')
            for name in fixture.SOURCES
        }
        sources = {name: scopes[name] + f'&item_id={items[name]["id"]}' for name in fixture.SOURCES}
        before = {
            name: client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload
            for name, item in items.items()
        }
        immutable(directory / 'source-meta-before.json', encoded(before))
        source_scan_identity = base.validate_scan(
            raster.wait_execution(client, 'meta', scan, remaining()),
            scan,
            None,
            tenant,
            int(identity['principal_id']),
            engine,
        )
        selected = [
            v
            for v in array(client.request('GET', '/api/v1/develop/workflow-engines', (200,)).payload, 'runtimes')
            if v.get('engine_type') == 'geopython_workflow' and v.get('connection_status') == 'online'
        ]
        if len(selected) != 1:
            raise SuiteError('exactly one same-source product Runtime is required')
        runtime = positive(selected[0].get('id'), 'Runtime')
        parent = f'addp://engine/{engine}/path/outputs?type=directory'
        baseline = physical_runner(repository, env, 'oracle', [], remaining())
        remaining()
        anchor = baseline.get('oracle_sha256')
        if not isinstance(anchor, str) or not re.fullmatch('[0-9a-f]{64}', anchor):
            raise SuiteError('missing frozen independent oracle digest')
        env['ADDP_ONLINE_FOCAL_SQUARE_SEVEN_ORACLE_SHA256'] = anchor
        check_physical(baseline, baseline.get('sources'), {}, anchor, [])
        for name in fixture.SOURCES:
            if (
                type(before[name].get('size_bytes')) is not int
                or before[name]['size_bytes'] != baseline['sources'][name]['size_bytes']
            ):
                raise SuiteError('physical/Meta source size differs')
        if base.scan_ids(client) != [scan]:
            raise SuiteError('unexpected source initialization scan')
        physical_calls, full_checks = 1, 0
        cases, failures, names, scans, previous = [], [], [], [], {}
        for index, name in enumerate(fixture.CASES, 1):
            stages.append({'case': name, 'status': 'running'})
            progress('running')
            language = 'zh-cn' if index % 2 else 'en'
            dag = definition(sources, parent, name)
            target = parent.replace('/outputs?type=directory', '/outputs/' + name + '?type=file')
            identifier = raster.submit(
                SubmittedLanguageClient(client, language),
                runtime,
                dag,
                execution_timeout=min(180, max(1, int(remaining()))),
            )
            execution = raster.wait_execution(client, 'develop', identifier, remaining())
            current(execution, identity, identifier, dag, runtime, language, 'success')
            wanted = expected_lineage(sources, name, target)
            evidence = lineage.collect(
                env,
                SUITE,
                identifier,
                name,
                wanted,
                execution,
                tenant,
                int(identity['principal_id']),
                dag,
                lineage_reader,
            )
            remaining()
            expectation = fixture.artifact_expectation(name)
            facts, output_scan = raster.validate_success(
                execution,
                [sources[n] for n in case_inputs(name)],
                target,
                'create',
                expectation,
                resource_type='file',
            )
            lineage.validate(facts, wanted, public=True)
            artifact = obj(execution['metadata']['result']['final_result'], 'Develop raster artifact')
            circle.validate_affine(artifact.get('transform'), expectation, 'artifact')
            if output_scan in scans:
                raise SuiteError('duplicate automatic COG scan')
            scans.append(output_scan)
            scan_identity = base.validate_scan(
                raster.wait_execution(client, 'meta', output_scan, remaining()),
                output_scan,
                identifier,
                tenant,
                int(identity['principal_id']),
                engine,
            )
            monitor_response = client.request(
                'GET', '/api/v1/monitor/executions/by-execution-id/' + identifier, (200,)
            )
            monitor = monitor_response.payload
            readonly.actor(monitor, identity, 'develop')
            if (
                monitor.get('execution_id') != identifier
                or type(monitor.get('tenant_id')) is not int
                or monitor.get('tenant_id') != tenant
                or monitor.get('status') != 'success'
                or obj(monitor.get('metadata'), 'metadata').get('lineage_facts') != facts
            ):
                raise SuiteError('Monitor current output lineage differs')
            archive_response(
                directory / 'focal-square-seven-terminal-responses',
                f'{index:02}-develop',
                client.request('GET', '/api/v1/develop/executions/' + identifier, (200,)),
            )
            archive_response(
                directory / 'focal-square-seven-terminal-responses', f'{index:02}-monitor', monitor_response
            )
            names.append(name)
            native = physical_runner(repository, env, 'verify', names, remaining())
            remaining()
            check_physical(native, baseline['sources'], previous, anchor, names)
            previous = copy.deepcopy(native['targets'])
            physical_calls += 1
            full_checks += len(names)
            item0 = support.find_fixture_item(client, engine, 'outputs/' + name, 'COG')
            item = client.request('GET', f'/api/v1/meta/items/{item0["id"]}', (200,)).payload
            raster.validate_target_item(item, expectation, native['targets'][name])
            artifact = obj(obj(execution['metadata'].get('result'), 'result').get('final_result'), 'artifact')
            sizes = raster.strict_sizes(artifact, native['targets'][name], item, name)
            immutable(directory / ('target-' + name + '-meta-before.json'), encoded(item))
            cases.append(
                {
                    'name': name,
                    'execution_id': identifier,
                    'target_item_id': item['id'],
                    'target_locator': target,
                    'automatic_scan_execution_id': output_scan,
                    'scan_identity': scan_identity,
                    'size_evidence': sizes,
                    'lineage_evidence': evidence,
                    'physical': native['targets'][name],
                }
            )
            stages[-1]['status'] = 'passed'
            progress('running')
        for case in cases:
            case['lineage'] = [
                raster.wait_lineage(
                    client, items[name]['id'], case['target_item_id'], case['execution_id'], remaining()
                )
                for name in case_inputs(case['name'])
            ]
        if base.scan_ids(client) != sorted([scan, *scans]):
            raise SuiteError('unexpected automatic scan inventory')
        edges = {name: source_edges(client, item['id']) for name, item in items.items()}
        for problem in FAILURES:
            for language in ('zh-cn', 'en'):
                stages.append({'problem': problem, 'language': language, 'status': 'running'})
                progress('running')
                dag = definition(sources, parent, 'mean.cog.tif', invalid=problem, language=language)
                identifier = raster.submit(
                    SubmittedLanguageClient(client, language),
                    runtime,
                    dag,
                    execution_timeout=min(180, max(1, int(remaining()))),
                )
                execution = raster.wait_execution(client, 'develop', identifier, remaining(), expected='failed')
                current(execution, identity, identifier, dag, runtime, language, 'failed')
                if obj(execution.get('error_details'), 'error details').get('category') != 'execution_failed':
                    raise SuiteError('wrong failure category')
                diagnostic = diagnostic_runner(repository, env, identifier, problem, language, remaining())
                remaining()
                if diagnostic != {
                    'execution_id': identifier,
                    'task': 'focal',
                    'problem': problem,
                    'language': language,
                    'exact_cause': MESSAGES[problem][language == 'en'],
                    'source': 'develop_runtime_log',
                }:
                    raise SuiteError('wrong exact failure task/cause')
                archive_response(
                    directory / 'focal-square-seven-terminal-responses',
                    f'failed-{problem}-{language}-develop',
                    client.request('GET', '/api/v1/develop/executions/' + identifier, (200,)),
                )
                readonly.no_publication(execution)
                monitor_response = client.request(
                    'GET', '/api/v1/monitor/executions/by-execution-id/' + identifier, (200,)
                )
                monitor = monitor_response.payload
                archive_response(
                    directory / 'focal-square-seven-terminal-responses',
                    f'failed-{problem}-{language}-monitor',
                    monitor_response,
                )
                readonly.actor(monitor, identity, 'develop')
                readonly.no_publication(monitor)
                if (
                    monitor.get('execution_id') != identifier
                    or type(monitor.get('tenant_id')) is not int
                    or monitor.get('tenant_id') != tenant
                    or monitor.get('status') != 'failed'
                    or obj(monitor.get('error_details'), 'Monitor error').get('category') != 'execution_failed'
                ):
                    raise SuiteError('Monitor failed identity/category differs')
                queried = (
                    SubmittedLanguageClient(client, 'en' if language == 'zh-cn' else 'zh-cn')
                    .request('GET', '/api/v1/develop/executions/' + identifier, (200,))
                    .payload
                )
                if any(
                    queried.get(key) != execution.get(key)
                    for key in ('error_msg', 'error_details', 'execution_config')
                ):
                    raise SuiteError('query language rewrote stored failure')
                if base.scan_ids(client) != sorted([scan, *scans]):
                    raise SuiteError('failed execution added scan')
                if any(source_edges(client, item['id']) != edges[name] for name, item in items.items()):
                    raise SuiteError('failed execution added derived lineage')
                native = physical_runner(repository, env, 'verify', names, remaining())
                remaining()
                check_physical(native, baseline['sources'], previous, anchor, names)
                physical_calls += 1
                full_checks += len(names)
                failures.append(
                    {
                        'problem': problem,
                        'language': language,
                        'execution_id': identifier,
                        'diagnostic': diagnostic,
                        'no_publication_or_scan_or_lineage': True,
                    }
                )
                stages[-1]['status'] = 'passed'
                progress('running')
        if len(cases) != 18 or len(failures) != 60 or full_checks != 1251 or physical_calls != 79:
            raise SuiteError('all 18 successes, 60 failures, 1251 COG checks and 79 physical calls required')
        target_metadata_before = {
            case['target_item_id']: json.loads(
                (directory / ('target-' + case['name'] + '-meta-before.json')).read_bytes()
            )
            for case in cases
        }
        target_metadata_after = {
            identifier: client.request('GET', f'/api/v1/meta/items/{identifier}', (200,)).payload
            for identifier in target_metadata_before
        }
        immutable(directory / 'target-meta-after.json', encoded(target_metadata_after))
        if target_metadata_after != target_metadata_before:
            raise SuiteError('target metadata changed')
        source_metadata_after = {
            name: client.request('GET', f'/api/v1/meta/items/{item["id"]}', (200,)).payload
            for name, item in items.items()
        }
        immutable(directory / 'source-meta-after.json', encoded(source_metadata_after))
        if source_metadata_after != before:
            raise SuiteError('source metadata changed')
        lineage.verify_archive(env, SUITE, cases)
        remaining()
        report = {
            'schema_version': 'addp.' + SUITE + '/v1',
            'suite': SUITE,
            'result': 'passed',
            'identity': identity,
            'run_id': env['ADDP_ONLINE_TEST_RUN_ID'],
            'sources': sources,
            'runtime_engine_id': runtime,
            'source_scan_execution_id': scan,
            'source_scan_identity': source_scan_identity,
            'cases': cases,
            'failed_parameters': failures,
            'full_target_checks': full_checks,
            'physical_verification_calls': physical_calls,
            'physical': native,
            'archived_cogs': native['archived_cogs'],
            'source_unchanged': True,
            'source_meta_unchanged': True,
            'target_meta_unchanged': True,
            'oracle_sha256': anchor,
        }
    except BaseException as error:
        primary = error
    finally:
        try:
            removed = physical_runner(repository, env, 'cleanup-oracle', [], remaining())
            remaining()
            if removed.get('oracle_cache_removed') is not True:
                raise SuiteError('oracle cleanup proof missing')
        except BaseException as error:
            cleanup_error = error
    if primary is not None:
        progress('failed')
        if cleanup_error is not None:
            raise SuiteError(
                str(primary) + '; oracle cleanup also failed: ' + type(cleanup_error).__name__
            ) from primary
        raise primary
    if cleanup_error is not None:
        progress('failed')
        raise cleanup_error
    report['oracle_cache_removed'] = True
    remaining()
    progress('passed')
    remaining()
    report.update(elapsed_ms=round((time.monotonic() - started) * 1000), business_budget_ms=600000)
    if report['elapsed_ms'] > 600000:
        raise SuiteError('business budget exceeded')
    return report


def main():
    env = dict(os.environ)
    report = run_scenario(
        Path(__file__).resolve().parents[2],
        env,
        DiagnosticClient(
            env['GATEWAY_URL'], env['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], 30, env['ADDP_ONLINE_ARTIFACT_DIR']
        ),
    )
    (Path(env['ADDP_ONLINE_ARTIFACT_DIR']) / 'raster-focal-square-seven-persistence.json').write_bytes(
        encoded(report)
    )
    print(json.dumps(report, ensure_ascii=False, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(
            f'seven-pixel square persistence acceptance failed: {error if isinstance(error, SuiteError) else type(error).__name__}',
            file=sys.stderr,
        )
        raise SystemExit(1)
