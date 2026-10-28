"""Real crash/reclaim and stale-Worker fencing in the Quality Hosted case."""

from datetime import datetime
import hashlib
from importlib import import_module
from pathlib import Path
import time
import uuid

QUALITY = import_module('scripts.test.quality-dynamic-binding-online')
FAULTS = import_module('scripts.test.quality-bounded-recovery-faults')
require, SuiteError = QUALITY.require, QUALITY.SuiteError


def timestamp(value):
    require(isinstance(value, str), 'execution timestamp is missing')
    result = datetime.fromisoformat(value.replace('Z', '+00:00'))
    require(result.tzinfo is not None, 'execution timestamp has no timezone')
    return result


def wait_for(read, predicate, message, timeout=120):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = read()
        if predicate(value):
            return value
        time.sleep(.2)
    raise SuiteError(message)


def assert_pending(first, pending):
    require(pending['status'] == 'pending' and pending['attempt'] == 1
            and pending['execution_id'] == first['execution_id'] and pending['created_at'] == first['created_at']
            and all(pending.get(key) is None for key in ('started_at', 'completed_at', 'execution_time_ms', 'lease_token', 'lease_owner', 'lease_expires_at')),
            'recovered pending retained previous attempt time or lease')


def assert_reclaimed(first, second):
    require(second['status'] == 'running' and second['attempt'] == 2
            and second['execution_id'] == first['execution_id'] and second['created_at'] == first['created_at']
            and timestamp(second['started_at']) > timestamp(first['started_at'])
            and str(uuid.UUID(second['lease_token'])) == second['lease_token']
            and second['lease_token'] != first['lease_token'], 'reclaimed attempt identity/time/token mismatch')


def assert_projection(client, execution_id, expected, first=None):
    projected = {}
    for name, path in (('quality', f'/api/v1/quality/executions/{execution_id}'),
                       ('monitor', f'/api/v1/monitor/executions/by-execution-id/{execution_id}')):
        item = client.request('GET', path, (200,)).payload
        require(item.get('execution_id') == execution_id and item.get('status') == expected['status']
                and item.get('attempt') == expected['attempt'] and 'lease_token' not in item,
                'owner or Monitor projection changed identity/status or exposed a token')
        for key in ('created_at', 'started_at', 'completed_at'):
            require((item.get(key) is None and expected.get(key) is None)
                    or (item.get(key) is not None and expected.get(key) is not None
                        and timestamp(item[key]) == timestamp(expected[key])), 'execution time projection mismatch')
        require(item.get('execution_time_ms') == expected.get('execution_time_ms'), 'execution duration projection mismatch')
        if name == 'monitor':
            if expected['status'] == 'pending':
                minimum_queue = (timestamp(first['lease_expires_at']) - timestamp(first['created_at'])).total_seconds() * 1000
                require(type(item.get('queue_duration_ms')) is int and item['queue_duration_ms'] + 1 >= minimum_queue,
                        'pending Monitor queue time borrowed the previous start')
                require(item.get('run_duration_ms') == 0 and item.get('lease_state') == 'none',
                        'pending Monitor projection borrowed previous running time')
            if expected['status'] == 'success':
                require(item.get('run_duration_ms') == expected['execution_time_ms'], 'Monitor terminal duration mismatch')
            require(type(item.get('queue_duration_ms')) is int and item['queue_duration_ms'] >= 0,
                    'Monitor queue duration is invalid')
        projected[name] = {key: item.get(key) for key in ('status', 'attempt', 'created_at', 'started_at', 'completed_at', 'execution_time_ms', 'queue_duration_ms', 'run_duration_ms')}
    return projected


def assert_events(client, execution_id):
    page = client.request('GET', f'/api/v1/monitor/executions/by-execution-id/{execution_id}/events?limit=100', (200,)).payload
    items = page.get('items', [])
    require(page.get('has_more') is False and [(x.get('kind'), x.get('attempt')) for x in items]
            == [('started', 1), ('started', 2), ('progress', 2), ('completed', 2)],
            'recovered process events lost attempts or accepted a stale write')
    for item in items:
        require(set(item) <= {'id', 'execution_id', 'attempt', 'occurred_at', 'kind', 'step_id', 'counters'},
                'process event exposed a private field')
    return len(items)


def run_suite(client, tenant_id, run_id, fixtures, report, checkpoint=lambda: None):
    QUALITY.validate_fixtures(fixtures)
    report.update(schema_version='addp.online-suite/v1', suite=QUALITY.SUITE, acceptance_case='bounded-recovery',
                  run_id=run_id, tenant_id=str(tenant_id), result='failed', resources=[], executions=[],
                  checks=[], observations=[], cleanup='not_needed', uncertain_mutation=False)
    faults = None

    def create(path, body):
        report['uncertain_mutation'] = True
        checkpoint()
        item = client.request('POST', path, (201,), body).payload
        identifier = QUALITY.API.require_positive_int(item, 'id')
        QUALITY.API.assert_tenant(item, tenant_id, 'recovery fixture')
        report['resources'].append(f'{path}/{identifier}')
        report.update(uncertain_mutation=False, cleanup='pending')
        checkpoint()
        return item

    try:
        suffix = hashlib.sha256(run_id.encode()).hexdigest()
        rule = create(QUALITY.QUALITY + '/rules', {'code': 'recovery_rule_' + suffix, 'name': 'Bounded recovery ' + run_id,
                                                  'type': 'row_count', 'params': {'min': 2, 'max': 2}})
        plan = create(QUALITY.QUALITY + '/plans', {'code': 'recovery_plan_' + suffix, 'name': 'Bounded recovery ' + run_id,
                       'description': 'Hosted bounded recovery', 'table_bindings': [{'alias': 'target', 'locator': fixtures[0]['locator']}],
                       'check_items': [{'rule_key': str(uuid.uuid4()), 'rule_id': rule['id'], 'revision_no': 1,
                                        'bindings': {'table': 'target'}, 'severity': 'error', 'disabled': False}]})
        faults = FAULTS.HostedFaults(tenant_id, plan['id'])
        faults.pin('quality-worker-1')
        faults.pin('quality-worker-2')
        faults.pause('quality-worker-2', True)
        for mode in ('crash_replacement', 'stale_worker'):
            evidence = {'case': mode}
            report['observations'].append(evidence)
            checkpoint()
            with faults.locked('source'):
                report['uncertain_mutation'] = True
                checkpoint()
                started = client.request('POST', f"{QUALITY.QUALITY}/plans/{plan['id']}/run", (202,), {}).payload
                identity = started.get('execution_id')
                require(isinstance(identity, str) and str(uuid.UUID(identity)) == identity, 'invalid recovery execution UUID')
                report['executions'].append(identity)
                faults.track(identity)
                report['uncertain_mutation'] = False
                evidence['execution_id'] = identity
                checkpoint()
                first = wait_for(lambda: faults.snapshot(identity), lambda x: x['status'] == 'running', 'first Quality claim timed out')
                require(first['attempt'] == 1 and first.get('started_at') is not None and first.get('lease_token') is not None,
                        'first claim did not persist current attempt time and token')
                lease_seconds = (timestamp(first['lease_expires_at']) - timestamp(first['started_at'])).total_seconds()
                require(29 <= lease_seconds <= 31, 'temporary Worker lease configuration was not consumed')
                wait_for(faults.source_waiters, lambda count: count == 1,
                         'first Quality runtime did not reach its fixture SQL wait')
                with faults.locked('events'):
                    if mode == 'crash_replacement':
                        evidence['replacement'] = faults.crash_and_replace('quality-worker-1')
                    else:
                        faults.pause('quality-worker-1', True)
                        wait_for(lambda: faults.snapshot(identity), lambda x: x['expired'] is True,
                                 'Quality lease did not expire in real time', timeout=60)
                        faults.pause('quality-worker-2', False)
                    pending = wait_for(lambda: faults.snapshot(identity), lambda x: x['status'] == 'pending',
                                       'real Worker did not restore the expired execution to pending', timeout=90)
                    assert_pending(first, pending)
                    evidence['pending_projection'] = assert_projection(client, identity, pending, first)
                    evidence['pending_started_at_cleared'] = True
                    checkpoint()
                second = wait_for(lambda: faults.snapshot(identity), lambda x: x['status'] == 'running' and x['attempt'] == 2,
                                  'replacement claim did not commit')
                assert_reclaimed(first, second)
                evidence.update(execution_identity_preserved=True, created_at_preserved=True,
                                new_started_at=True, attempt_incremented=True, lease_token_changed=True)
                if mode == 'stale_worker':
                    faults.pause('quality-worker-1', False)
                    wait_for(lambda: faults.old_worker_rejected(identity), lambda rejected: rejected,
                             'resumed old Quality Worker did not reject its expired heartbeat', timeout=40)
                    current = faults.snapshot(identity)
                    require(all(current.get(key) == second.get(key) for key in ('status', 'attempt', 'started_at', 'lease_token')),
                            'resumed old Worker overwrote the current attempt')
                    evidence['old_worker_heartbeat_rejected'] = True
                checkpoint()
            completed = wait_for(lambda: faults.snapshot(identity), lambda x: x['status'] in QUALITY.TERMINAL,
                                 'recovered Quality execution did not finish')
            require(completed['status'] == 'success' and completed['attempt'] == 2
                    and completed['lease_token'] is None and completed['started_at'] == second['started_at'],
                    'recovered Quality execution did not complete the new attempt')
            elapsed_ms = (timestamp(completed['completed_at']) - timestamp(second['started_at'])).total_seconds() * 1000
            require(type(completed['execution_time_ms']) is int and abs(completed['execution_time_ms'] - elapsed_ms) <= 1,
                    'terminal duration borrowed the previous attempt time')
            evidence['terminal_projection'] = assert_projection(client, identity, completed)
            evidence['event_count'] = assert_events(client, identity)
            report['checks'].append(mode)
            checkpoint()
        report['result'] = 'passed'
    finally:
        primary_failure = report['result'] != 'passed'
        errors = []
        if faults is not None:
            try:
                faults.close()
            except (OSError, ValueError):
                errors.append('worker_recovery')
        if not report['uncertain_mutation']:
            # Never delete a definition while its execution is active or unknown.
            for identity in report['executions']:
                try:
                    item = client.request('GET', f'{QUALITY.QUALITY}/executions/{identity}', (200,)).payload
                    require(item.get('status') in QUALITY.TERMINAL, 'recovery execution is still active')
                except SuiteError:
                    errors.append('active_execution')
            if not errors:
                for path in reversed(report['resources']):
                    try:
                        current = client.request('GET', path, (200,)).payload
                        client.request('DELETE', path, (200,), {'version': current['version']})
                        client.request('GET', path, (404,))
                    except (SuiteError, KeyError):
                        errors.append('owner_resource')
        else:
            errors.append('uncertain_mutation')
        report['cleanup'] = 'failed' if errors else 'passed'
        if errors:
            report['result'] = 'failed'
            checkpoint()
            if not primary_failure:
                raise SuiteError('Quality recovery cleanup failed; Hosted destruction remains mandatory')
        else:
            report['temporary_residual_resources'] = 0
        checkpoint()
    return report
