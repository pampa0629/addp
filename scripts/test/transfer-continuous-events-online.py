"""Continuous Worker and safe tenant execution events on disposable Hosted T4."""
from __future__ import annotations

from datetime import datetime
from importlib import import_module
import json
import math
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import uuid

API = import_module('scripts.utils.online-api')
FAULTS = import_module('scripts.test.continuous-execution-faults')
SuiteError = API.SuiteError
SUITE = 'transfer-continuous-events'
ROOT = Path(__file__).resolve().parents[2]
OWNER = '/api/v1/transfer'
MONITOR = '/api/v1/monitor/executions/by-execution-id'
AUTH = '/api/v1/system/auth/context'
IAM = '/api/v1/system/tenant/role_assignments'
TERMINAL = {'success', 'failed', 'cancelled', 'timeout'}
REQUIRED = {'transfer.task.create', 'transfer.task.delete', 'transfer.task.execute', 'transfer.task.read',
            'transfer.task.update', 'monitor.execution.read', 'meta.catalog.read',
            'meta.scan_task.execute', 'meta.scan_task.read', 'system.execution_authorization.create'}
PRIVATE = {'execution_config', 'authorization_ref', 'execution_authorization_id', 'parameters',
           'raw_response', 'runtime_response', 'connection_info', 'password', 'access_token', 'lease_token',
           'lease_owner', 'lease_expires_at', 'lease_until', 'owner_instance_id', 'fencing_token',
           'actor_principal_id', 'actor_tenant_membership_id', 'issued_authorization_version',
           'authorization_expires_at'}
EVENT_FIELDS = {'id', 'execution_id', 'attempt', 'occurred_at', 'kind', 'step_id', 'counters'}
MONITOR_FIELDS = set('id execution_id module task_type source source_task_id source_task_name '
                     'parent_execution_id status progress current_step execution_boundary retry_of_execution_id '
                     'attempt max_attempts trigger_type triggered_by started_at completed_at created_at updated_at '
                     'execution_time_ms rows_affected records_read records_written bytes_read bytes_written '
                     'metadata error_details steps steps_attempt_unverified steps_truncated diagnostics_truncated'.split())
OWNER_FIELDS = set('id execution_id task_id status start_time end_time records_read records_written bytes_read '
                   'bytes_written error_details metadata trigger_type trigger_by'.split())
OWNER_METADATA = set('lineage_facts continuous stop_reason recovery_reason recovered_from_execution_id recovery_attempt '
                     'recovery_consecutive_failures recovery_backoff_seconds recovery_not_before recovery_circuit_state'.split())
PHYSICAL_ACTIONS = {'produce-initial', 'produce-after-recovery', 'produce-invalid', 'mutate-cdc',
                    'verify-kafka-initial', 'verify-kafka-recovered', 'verify-cdc-initial',
                    'verify-cdc-mutated', 'verify-clean'}


def require(condition, message):
    if not condition:
        raise SuiteError(message)


def canonical_uuid(value):
    try:
        parsed = uuid.UUID(value)
        require(parsed.int > 0 and str(parsed) == value, 'execution UUID must be canonical')
    except (ValueError, TypeError, AttributeError):
        raise SuiteError('execution UUID must be canonical') from None
    return value


def assert_safe(value):
    if isinstance(value, dict):
        require(not PRIVATE.intersection(value), 'diagnostic exposed private execution fields')
        for child in value.values():
            assert_safe(child)
    elif isinstance(value, list):
        for child in value:
            assert_safe(child)
    elif isinstance(value, str):
        require('business-value-sentinel' not in value, 'diagnostic exposed business samples')


def assert_projection(item, execution_id, *, monitor=False, tenant=None):
    require(isinstance(item, dict) and set(item) <= (MONITOR_FIELDS if monitor else OWNER_FIELDS),
            'execution projection exposed undeclared fields')
    require(item.get('execution_id') == execution_id, 'execution identity changed')
    assert_safe(item)
    if monitor:
        require(item.get('module') == 'transfer' and item.get('task_type') == 'sync'
                and item.get('execution_boundary') == 'continuous', 'Monitor changed execution ownership/boundary')
        metadata = item.get('metadata')
        require(isinstance(metadata, dict) and set(metadata) <= {
            'continuous', 'lineage_facts', 'stop_reason', 'recovery_reason', 'recovered_from_execution_id',
            'recovery_attempt', 'recovery_consecutive_failures', 'recovery_not_before', 'recovery_circuit_state'},
            'generic diagnostic exposed arbitrary metadata')
    else:
        require(isinstance(item.get('metadata'), dict) and set(item['metadata']) <= OWNER_METADATA,
                'Owner exposed arbitrary metadata')
    error = item.get('error_details') or {}
    require(isinstance(error, dict) and set(error) <= {'category', 'code', 'error_code', 'message', 'failed_targets_count'},
            'execution exposed private failure details')


def events(client, prefix, execution_id):
    """Drain cursor pages and reject duplicates, unknown fields, samples and offset payloads."""
    cursor, result = 0, []
    for _ in range(600):
        page = client.request('GET', f'{prefix}/{execution_id}/events?after={cursor}&limit=2', (200,)).payload
        require(set(page) == {'items', 'next_cursor', 'has_more', 'retained_after'}, 'event page contract changed')
        require(isinstance(page['items'], list) and len(page['items']) <= 2 and isinstance(page['has_more'], bool),
                'event pagination is invalid')
        try:
            retained = datetime.fromisoformat(page['retained_after'].replace('Z', '+00:00'))
            require(retained.tzinfo is not None, 'retention boundary needs a timezone')
        except (ValueError, TypeError, AttributeError):
            raise SuiteError('retention boundary is invalid') from None
        assert_safe(page)
        for item in page['items']:
            require(isinstance(item, dict) and set(item) <= EVENT_FIELDS, 'event exposed undeclared fields')
            identity = API.require_positive_int(item, 'id')
            require(identity > cursor and item.get('execution_id') == execution_id and item.get('attempt') == 1,
                    'event cursor, identity or continuous attempt changed')
            require(item.get('kind') in {'started', 'progress', 'completed', 'failed', 'cancelled', 'timeout', 'truncated'},
                    'event kind is invalid')
            require(isinstance(item.get('occurred_at'), str) and bool(item['occurred_at']), 'event timestamp missing')
            counters = item.get('counters')
            require(isinstance(counters, dict) and set(counters) <= {'records_read', 'records_written'},
                    'continuous event exposed undeclared counters')
            require(all(isinstance(n, int) and not isinstance(n, bool) and n >= 0 for n in counters.values()),
                    'event counters must be nonnegative integers')
            cursor = identity
            result.append(item)
        require(isinstance(page['next_cursor'], int) and not isinstance(page['next_cursor'], bool)
                and page['next_cursor'] == cursor, 'event next_cursor does not match the last fact')
        if not page['has_more']:
            return result
        require(bool(page['items']), 'event page did not advance')
    raise SuiteError('event page count exceeded the execution budget')


def lifecycle_events(items, terminal=None):
    kinds = [item['kind'] for item in items]
    require(kinds and kinds[0] == 'started' and kinds.count('started') == 1, 'Worker started event missing or duplicated')
    require(any(item['kind'] == 'progress' and item['counters'].get('records_written', 0) > 0 for item in items),
            'committed progress event missing')
    endings = [kind for kind in kinds if kind in {'completed', 'failed', 'cancelled', 'timeout'}]
    require(endings == ([terminal] if terminal else []), 'terminal events missing or duplicated')
    if terminal:
        require(kinds[-1] == terminal, 'events appeared after the terminal fact')


def task_config(kind, pg, kafka):
    require(kind in {'kafka', 'cdc'} and pg > 0 and kafka > 0, 'invalid physical source identity')
    source = {'locator': f'addp://engine/{pg}/path/public/continuous_source?type=table',
              'data_type': 'table', 'representation': 'native'}
    detection = {'type': 'cdc', 'bootstrap': 'initial_snapshot'}
    if kind == 'kafka':
        source = {'locator': f'addp://engine/{kafka}/path/continuous_events?type=topic', 'representation': 'native',
                  'change_stream': {'envelope': 'record', 'encoding': 'json',
                                    'key': {'source': 'value', 'fields': ['id']},
                                    'start': {'mode': 'committed', 'initial': 'earliest'}, 'poll_batch_size': 100}}
        detection = {'type': 'kafka'}
    return {'runtime': {'boundary': 'continuous', 'record_failure': {'mode': 'block'}},
            'load': {'mode': 'incremental', 'change_detection': detection}, 'source': source,
            'target': {'parent_locator': f'addp://engine/{pg}/path/public?type=schema',
                       'name': f'continuous_{kind}_target', 'data_type': 'table', 'representation': 'native',
                       'policy': {'apply_mode': 'upsert' if kind == 'kafka' else 'upsert_delete', 'keys': ['id']}},
            'transforms': [{'type': 'field_mapping', 'version': 'v1', 'mode': 'project',
                            'fields': [{'source': 'id', 'target': 'id', 'target_type': 'bigint', 'nullable': False},
                                       {'source': 'label', 'target': 'label', 'target_type': 'string', 'nullable': False}]}]}


def physical(action):
    require(action in PHYSICAL_ACTIONS, 'invalid physical fixture action')
    result = subprocess.run(['bash', 'business/scripts/online-continuous-fixture.sh', action], cwd=ROOT,
                            capture_output=True, timeout=20)
    # stdout/stderr can contain business values or database connection facts; never publish them.
    return result.returncode == 0


def until(operation, timeout=120):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = operation()
        if value:
            return value
        time.sleep(1)
    raise SuiteError('continuous execution did not converge before the deadline')


def detail(client, execution_id):
    item = client.request('GET', f'{OWNER}/executions/{execution_id}', (200,)).payload
    assert_projection(item, execution_id)
    return item


def wait_status(client, execution_id, expected):
    def check():
        item = detail(client, execution_id)
        if item.get('status') == expected:
            return item
        require(item.get('status') not in TERMINAL, 'execution reached an unexpected terminal status')
    return until(check)


def history(client, task_id):
    page = client.request('GET', f'{OWNER}/task-definitions/{task_id}/executions?page_size=100', (200,)).payload
    require(isinstance(page.get('data'), list) and page.get('total') == len(page['data']) <= 100,
            'fixture history is incomplete')
    for item in page['data']:
        assert_projection(item, canonical_uuid(item.get('execution_id')))
    return page['data']


def invisible(client, execution_id, owner_status, monitor_status):
    for suffix in ('', '/progress', '/events?after=0&limit=2'):
        response = client.request('GET', f'{OWNER}/executions/{execution_id}{suffix}', (owner_status,))
        assert_safe(response.payload)
        require('execution_id' not in response.payload and 'items' not in response.payload, 'Owner denial leaked execution facts')
    for suffix in ('', '/events?after=0&limit=2', '/tree'):
        response = client.request('GET', f'{MONITOR}/{execution_id}{suffix}', (monitor_status,))
        assert_safe(response.payload)
        require('execution_id' not in response.payload and 'items' not in response.payload, 'Monitor denial leaked execution facts')


def identity(client, tenant, permissions):
    summary = API.validate_user_identity(client, tenant, permissions)
    context = client.request('GET', AUTH, (200,)).payload
    binding = (context.get('principal', {}).get('id'), context.get('context', {}).get('tenant_membership_id'))
    require(all(isinstance(part, str) and part.isdecimal() and int(part) > 0 for part in binding), 'User binding is incomplete')
    summary['principal_id'], summary['membership_id'] = binding
    return summary, binding


def revoke(reader, admin, login, tenant, execution_id, mutate):
    _, binding = identity(reader, tenant, {'monitor.execution.read', 'transfer.task.read'})
    target_id = os.environ['ADDP_ONLINE_PARENT_ASSIGNMENT_ID']
    username = os.environ['ADDP_ONLINE_PARENT_USER_USERNAME']
    require(re.fullmatch(r'[1-9][0-9]*', target_id) and username == 'external-online-transfer-reader', 'dedicated Owner reader required')
    controller = admin.request('GET', AUTH, (200,)).payload
    require(controller.get('principal', {}).get('type') == 'user' and
            controller.get('context', {}).get('tenant_id') == str(tenant), 'IAM controller Tenant changed')
    assignments = controller.get('authorization', {}).get('role_assignments', [])
    roles = {item['role_key'] for item in assignments}
    permissions = {p for item in assignments for p in item['permissions']}
    require('tenant.administrator' in roles and not (roles & (API.FORBIDDEN_ADMIN_ROLES - {'tenant.administrator'}))
            and {'iam.tenant_role_assignment.read', 'iam.tenant_role_assignment.revoke'} <= permissions,
            'dedicated Tenant IAM controller required')
    require(controller.get('principal', {}).get('id') != binding[0], 'controller cannot be the reader')
    path = f'{IAM}/{target_id}'

    def assignment(item, status):
        require(item.get('id') == target_id and item.get('principal_type') == 'user' and
                (item.get('principal_id'), item.get('membership_id')) == binding and
                item.get('username') == username and item.get('role_key') == 'online.external_online_transfer_reader' and
                item.get('scope_type') == 'tenant' and item.get('status') == status, 'Owner assignment binding changed')
    assignment(admin.request('GET', path, (200,)).payload, 'active')
    assignment(mutate(admin, path + '/revoke', {'reason': 'Disposable continuous execution Owner read revocation'}, (200,)), 'revoked')
    invisible(reader, execution_id, 401, 401)
    issued = mutate(login, '/api/v1/system/login', {'username': username,
                     'password': os.environ['ADDP_ONLINE_PARENT_USER_PASSWORD']}, (200,))
    token = (issued.get('session') or {}).get('access_token')
    require(issued.get('next_action') == 'session_issued' and isinstance(token, str) and token != reader.token,
            'formal re-login must issue a fresh reader token')
    fresh = API.GatewayClient(reader.base_url, token, reader.timeout)
    _, fresh_binding = identity(fresh, tenant, {'monitor.execution.read'})
    require(fresh_binding == binding, 're-login changed the reader identity')
    fresh_context = fresh.request('GET', AUTH, (200,)).payload
    require(not any('transfer.task.read' in item['permissions'] for item in fresh_context['authorization']['role_assignments']),
            'fresh reader retained revoked Owner permission')
    invisible(fresh, execution_id, 403, 404)


def run_suite(client, denied, foreign, peer, reader, admin, login, tenant, run_id, pg, kafka, report, checkpoint, faults):
    report.update(schema_version='addp.online-suite/v1', suite=SUITE, run_id=run_id, tenant_id=str(tenant),
                  result='failed', checks=[], resources=[], executions=[], cleanup='not_started',
                  history_cleanup='pending_deployment_destruction', uncertain_mutation=False)
    tasks = []

    def mutate(actor, path, body=None, expected=(200,)):
        report['uncertain_mutation'] = True
        checkpoint()
        response = actor.request('POST', path, expected, body).payload
        report['uncertain_mutation'] = False
        checkpoint()
        return response

    def create(kind):
        name = f'online-{run_id}-{kind}'
        result = mutate(client, f'{OWNER}/task-definitions',
                        {'name': name, 'task_type': 'sync', 'enabled': False, 'auto_scan_metadata': False,
                         'batch_size': 100, 'config': task_config(kind, pg, kafka)}, (201,))
        task_id = API.require_positive_int(result, 'id')
        API.assert_tenant(result, tenant, 'continuous task')
        tasks.append((task_id, kind, name))
        report['resources'].append(f'{OWNER}/task-definitions/{task_id}')
        checkpoint()
        return task_id

    def launch(task_id, action='start'):
        result = mutate(client, f'{OWNER}/task-definitions/{task_id}/{action}')
        if action == 'resume':
            result = result.get('execution', {})
        execution_id = canonical_uuid(result.get('execution_id'))
        assert_projection(result, execution_id)
        report['executions'].append(execution_id)
        checkpoint()
        wait_status(client, execution_id, 'running')
        return execution_id

    def compare(execution_id, terminal=None):
        owner = events(client, f'{OWNER}/executions', execution_id)
        generic = events(client, MONITOR, execution_id)
        lifecycle_events(owner, terminal)
        require(owner == generic, 'Owner and Monitor event facts differ')
        projected = client.request('GET', f'{MONITOR}/{execution_id}', (200,)).payload
        assert_projection(projected, execution_id, monitor=True, tenant=tenant)
        assert_safe(client.request('GET', f'{OWNER}/executions/{execution_id}/progress', (200,)).payload)
        return owner

    try:
        # Scan physical PostgreSQL facts through Meta; no fabricated catalog/task/runtime rows.
        scan = mutate(client, '/api/v1/meta/scan/run/manual',
                      {'engine_id': pg, 'scan_depth': 'deep', 'trigger_type': 'manual', 'force': True}, (201,))
        scan_id = canonical_uuid(scan.get('execution_id'))
        def scanned():
            item = client.request('GET', f'/api/v1/meta/executions/{scan_id}', (200,)).payload
            require(item.get('status') not in {'failed', 'cancelled', 'timeout'}, 'physical source metadata scan failed')
            return item.get('status') == 'success'
        until(scanned)
        report['checks'].append('formal_source_catalog_scan')
        kafka_task = create('kafka')
        initial = launch(kafka_task)
        require(physical('produce-initial'), 'business Kafka input failed')
        until(lambda: physical('verify-kafka-initial'))
        until(lambda: any(e['kind'] == 'progress' and e['counters'].get('records_written', 0) >= 2
                          for e in events(client, f'{OWNER}/executions', initial)))
        lifecycle_events(events(client, f'{OWNER}/executions', initial))
        report['checks'].append('kafka_physical_initial_commit_and_events')
        invisible(denied, initial, 403, 404)
        invisible(foreign, initial, 404, 404)
        denied.request('GET', f'{OWNER}/task-definitions/{kafka_task}/executions', (403,))
        hidden = foreign.request('GET', f'{OWNER}/executions?task_id={kafka_task}', (200,)).payload
        require(hidden.get('total') == 0 and hidden.get('data') == [], 'foreign history total leaked')
        for actor in (peer, reader):
            assert_projection(actor.request('GET', f'{MONITOR}/{initial}', (200,)).payload, initial, monitor=True, tenant=tenant)
            lifecycle_events(events(actor, f'{OWNER}/executions', initial))
        report['checks'].append('owner_permissions_foreign_tenant_and_peer_history')
        report['worker_fault'] = faults.kill_worker_and_replace()
        expired = wait_status(client, initial, 'failed')
        require(expired.get('error_details', {}).get('code') == 'lease_expired' and
                client.request('GET', f'{MONITOR}/{initial}', (200,)).payload.get('error_details', {}).get('category') == 'coordinator_lost', 'lease failure lost its stable cause')
        frozen = compare(initial, 'failed')
        def recovered():
            return next((item for item in history(client, kafka_task)
                         if item['metadata'].get('recovered_from_execution_id') == initial
                         and item['status'] == 'running'), None)
        recovery = until(recovered)
        recovery_id = canonical_uuid(recovery['execution_id'])
        require(recovery_id != initial and recovery['metadata'].get('recovery_reason') == 'lease_expired',
                'recovery must create a new execution UUID with a stable reason')
        report['executions'].append(recovery_id)
        require(physical('produce-after-recovery'), 'Kafka recovery input failed')
        until(lambda: physical('verify-kafka-recovered'))
        until(lambda: any(e['kind'] == 'progress' and e['counters'].get('records_written', 0) > 0
                          for e in events(client, f'{OWNER}/executions', recovery_id)))
        require(events(client, f'{OWNER}/executions', initial) == frozen, 'old execution accepted events after recovery')
        report['checks'].append('real_worker_sigkill_lease_expiry_new_uuid_committed_resume')
        mutate(client, f'{OWNER}/task-definitions/{kafka_task}/pause')
        wait_status(client, recovery_id, 'cancelled')
        compare(recovery_id, 'cancelled')
        resumed = launch(kafka_task, 'resume')
        require(resumed not in {initial, recovery_id}, 'manual resume reused an execution identity')
        # Supply a valid update to witness committed progress in this new execution, then a bad record.
        require(physical('produce-after-recovery'), 'Kafka resumed input failed')
        until(lambda: any(e['kind'] == 'progress' and e['counters'].get('records_written', 0) > 0
                          for e in events(client, f'{OWNER}/executions', resumed)))
        require(physical('produce-invalid'), 'Kafka invalid-record fixture failed')
        failed = wait_status(client, resumed, 'failed')
        mutate(client, f'{OWNER}/task-definitions/{kafka_task}/stop')
        compare(resumed, 'failed')
        require(failed.get('error_details', {}).get('category') in {'execution_failed', 'invalid_input'},
                'record failure lost its safe cause category')
        report['checks'].append('kafka_pause_resume_and_record_failure_without_samples')
        # A hung owner must not cause Monitor to release previously readable facts.
        def owner_unavailable():
            response = client.request('GET', f'{MONITOR}/{initial}', (502, 503))
            assert_safe(response.payload)
            require('execution_id' not in response.payload, 'unavailable Owner leaked an execution')
        faults.stop_owner_during(owner_unavailable)
        compare(initial, 'failed')
        report['checks'].append('owner_unavailable_fail_closed')
        revoke(reader, admin, login, tenant, initial, mutate)
        report['checks'].append('revoked_token_and_fresh_monitor_only_reader_denied')
        cdc_task = create('cdc')
        cdc = launch(cdc_task)
        until(lambda: physical('verify-cdc-initial'))
        require(physical('mutate-cdc'), 'CDC mutation fixture failed')
        until(lambda: physical('verify-cdc-mutated'))
        until(lambda: any(e['kind'] == 'progress' and e['counters'].get('records_written', 0) >= 5
                          for e in events(client, f'{OWNER}/executions', cdc)))
        name = next(name for task, kind, name in tasks if task == cdc_task)
        mutate(client, f'{OWNER}/task-definitions/{cdc_task}/stop', {'confirmed': True, 'confirmation_text': name})
        wait_status(client, cdc, 'cancelled')
        compare(cdc, 'cancelled')
        require(physical('verify-clean'), 'CDC capture resources survived confirmed stop')
        report['checks'].append('cdc_snapshot_update_delete_insert_safe_events_and_confirmed_cleanup')
    finally:
        report['cleanup'] = 'started'
        checkpoint()
        failures = []
        for task_id, kind, name in reversed(tasks):
            try:
                current = client.request('GET', f'{OWNER}/task-definitions/{task_id}', (200,)).payload
                if current.get('desired_state') != 'stopped':
                    mutate(client, f'{OWNER}/task-definitions/{task_id}/stop',
                           {'confirmed': True, 'confirmation_text': name} if kind == 'cdc' else None)
                # Stop must converge before delete, including recovery admitted concurrently with failure.
                until(lambda: all(item['status'] in TERMINAL for item in history(client, task_id)))
                client.request('DELETE', f'{OWNER}/task-definitions/{task_id}', (200,))
                client.request('GET', f'{OWNER}/task-definitions/{task_id}', (404,))
            except (SuiteError, subprocess.SubprocessError):
                failures.append(task_id)
        report['cleanup'] = 'failed' if failures else 'passed'
        report['residual_resources'] = len(failures)
        checkpoint()
        require(not failures, 'continuous task cleanup failed')
    # Definition deletion must retain the authorized execution history and event facts.
    compare(initial, 'failed')
    assert_projection(peer.request('GET', f'{MONITOR}/{initial}', (200,)).payload, initial, monitor=True, tenant=tenant)
    invisible(denied, initial, 403, 404)
    report['checks'].append('deleted_definition_history_retained_with_owner_permissions')
    require(not report['uncertain_mutation'], 'an uncertain mutation cannot pass')
    report['result'] = 'passed'
    checkpoint()


def main():
    report, evidence = {}, None
    def checkpoint():
        if evidence is not None:
            temporary = evidence.with_suffix('.tmp')
            temporary.write_text(json.dumps(report, sort_keys=True) + '\n')
            temporary.replace(evidence)
    def interrupted(signum, frame):
        raise SuiteError('continuous Hosted acceptance interrupted')
    try:
        faults = FAULTS.HostedFaults()  # Local/shared deployments fail before any network or mutation.
        require(os.environ.get('ADDP_ONLINE_TEST') == '1', 'standard Online entrypoint required')
        tenant = int(os.environ['ADDP_ONLINE_TEST_TENANT_ID'])
        pg, kafka = [int(os.environ[f'ADDP_ONLINE_CONTINUOUS_{kind}_ENGINE_ID']) for kind in ('PG', 'KAFKA')]
        require(tenant > 1 and pg > 0 and kafka > 0 and pg != kafka, 'dedicated Tenant and Engines required')
        run_id = os.environ['ADDP_ONLINE_TEST_RUN_ID']
        require(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,63}', run_id), 'invalid Run ID')
        directory = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']).resolve()
        require(directory.is_absolute() and not directory.is_relative_to(ROOT) and
                directory != faults.secret and not directory.is_relative_to(faults.secret), 'evidence must be outside checkout/secrets')
        evidence = directory / (SUITE + '.json')
        timeout = float(os.environ.get('ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS', '10'))
        require(math.isfinite(timeout) and 5 < timeout <= 30, 'HTTP timeout must cover Owner authorization timeout')
        gateway = os.environ['GATEWAY_URL']
        names = ['TEST', 'READ', 'FOREIGN', 'PEER', 'PARENT', 'ADMIN']
        tokens = [os.environ[f'ADDP_ONLINE_{name}_USER_ACCESS_TOKEN'] for name in names]
        require(all(tokens) and len(set(tokens)) == len(tokens), 'six distinct fixture User tokens required')
        client, denied, foreign, peer, reader, admin = [API.GatewayClient(gateway, token, timeout) for token in tokens]
        identities = []
        for name, actor, permissions in [('consumer', client, REQUIRED), ('monitor_only', denied, {'monitor.execution.read'}),
                                         ('peer', peer, {'monitor.execution.read', 'transfer.task.read'}),
                                         ('reader', reader, {'monitor.execution.read', 'transfer.task.read'})]:
            summary, binding = identity(actor, tenant, permissions)
            identities.append(binding[0])
            report[name + '_identity'] = summary
        foreign_context = foreign.request('GET', AUTH, (200,)).payload
        foreign_tenant = int(foreign_context.get('context', {}).get('tenant_id', '0'))
        require(foreign_tenant > 1 and foreign_tenant != tenant, 'foreign fixture Tenant must differ')
        report['foreign_identity'], binding = identity(foreign, foreign_tenant, {'monitor.execution.read', 'transfer.task.read'})
        identities.append(binding[0])
        require(len(set(identities)) == 5, 'fixture business Users must be distinct')
        for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGALRM):
            signal.signal(signum, interrupted)
        signal.alarm(850)
        run_suite(client, denied, foreign, peer, reader, admin, API.GatewayClient(gateway, '', timeout),
                  tenant, run_id, pg, kafka, report, checkpoint, faults)
    except (KeyError, ValueError, SuiteError, subprocess.SubprocessError, OSError) as error:
        report['result'] = 'failed'
        # Never persist raw API errors, subprocess output, config, source values or credentials.
        print('Continuous Hosted suite failed: ' + (str(error) if isinstance(error, SuiteError) else 'fixture/lifecycle error'), file=sys.stderr)
        return 1
    finally:
        signal.alarm(0)
        checkpoint()
    print(json.dumps(report, sort_keys=True))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
