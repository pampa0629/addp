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
import tempfile
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
                     'metadata error_details steps steps_attempt_unverified steps_truncated diagnostics_truncated '
                     'queue_duration_ms run_duration_ms lease_state recovery_reason'.split())
OWNER_FIELDS = set('id execution_id task_id status start_time end_time records_read records_written bytes_read '
                   'bytes_written error_details metadata trigger_type trigger_by'.split())
OWNER_METADATA = set('metadata_scan lineage_facts continuous stop_reason recovery_reason recovered_from_execution_id recovery_attempt '
                     'recovery_consecutive_failures recovery_backoff_seconds recovery_not_before recovery_circuit_state'.split())
PHYSICAL_ACTIONS = {'produce-initial', 'produce-after-recovery', 'produce-invalid', 'mutate-cdc', 'alter-cdc', 'verify-cdc-altered',
                    'verify-kafka-initial', 'verify-kafka-recovered', 'verify-cdc-initial',
                    'verify-cdc-mutated', 'verify-scheduled', 'verify-clean'}


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
        require(all(isinstance(item.get(key), int) and not isinstance(item[key], bool) and item[key] >= 0
                    for key in ('queue_duration_ms', 'run_duration_ms')),
                'Monitor durations must be nonnegative integers')
        require(item.get('lease_state') == 'none', 'Monitor continuous projection used a bounded lease state')
        metadata = item.get('metadata')
        require(isinstance(metadata, dict) and set(metadata) <= {
            'continuous', 'lineage_facts', 'stop_reason', 'recovery_reason', 'recovered_from_execution_id',
            'recovery_attempt', 'recovery_consecutive_failures', 'recovery_not_before', 'recovery_circuit_state'},
            'generic diagnostic exposed arbitrary metadata')
    else:
        require(isinstance(item.get('metadata'), dict) and set(item['metadata']) <= OWNER_METADATA,
                'Owner exposed arbitrary metadata')
        scan = item['metadata'].get('metadata_scan')
        if scan is not None:
            require(isinstance(scan, dict) and set(scan) <= {'status', 'attempt', 'execution_id', 'error_code'},
                    'Owner exposed private metadata scan facts')
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


def catalog_item(client, engine_id, tenant, name):
    items = client.request('GET', f'/api/v1/meta/engines/{engine_id}/items', (200,), response_type=list).payload
    require(isinstance(items, list), 'Meta engine items must be an array')
    matches = [item for item in items if isinstance(item, dict) and item.get('full_name') == 'public.' + name]
    require(len(matches) <= 1, 'Meta target identity is duplicated')
    if not matches:
        return None
    item = matches[0]
    require(item.get('engine_id') == engine_id and item.get('tenant_id') == tenant
            and item.get('item_type') == 'table' and bool(item.get('fingerprint')), 'Meta target identity changed')
    API.require_positive_int(item, 'id')
    API.require_positive_int(item, 'node_id')
    return item


def assert_scan_boundary(client, engine_id, sibling, boundary):
    require(API.read_meta_scan_boundary(client, engine_id, sibling) == boundary,
            'automatic target scan changed sibling facts or ancestor scan state')


def wait_target_scan(client, engine_id, tenant, name, expected_fields, sibling, boundary):
    def scanned():
        # Check every observation so a broadened scan's running ancestor state also fails.
        assert_scan_boundary(client, engine_id, sibling, boundary)
        item = catalog_item(client, engine_id, tenant, name)
        if item is None:
            return None
        require(item['node_id'] == sibling['node_id'] and item['id'] != sibling['id'], 'Meta target parent or identity changed')
        table = (item.get('attributes') or {}).get('type_info', {}).get('table', {})
        fields = table.get('fields', [])
        names = [field.get('name') for field in fields if isinstance(field, dict)]
        if (item.get('scanned_depth') != 'deep' or not item.get('scanned_at')
                or len(names) != len(fields) or len(names) != len(expected_fields) or set(names) != expected_fields):
            return None
        try:
            stamp = datetime.fromisoformat(item['scanned_at'].replace('Z', '+00:00'))
            require(stamp.tzinfo is not None, 'target scan timestamp needs a timezone')
        except (ValueError, TypeError, AttributeError):
            raise SuiteError('target scan timestamp is invalid') from None
        return {'target_locator': f'addp://engine/{engine_id}/path/public/{name}?type=table',
                'item_id': item['id'], 'scanned_at': item['scanned_at'], 'fields': sorted(names)}
    return until(scanned)


def assert_schema_scan_origin(initiator, approver, scan_id, approver_id):
    result = approver.request('GET', f'/api/v1/meta/executions/{scan_id}', (200,)).payload
    require(result.get('execution_id') == scan_id and not result.get('parent_execution_id'),
            'approval scan invented a parent execution')
    require(result.get('triggered_by') == int(approver_id), 'approval scan changed its approving User')
    require(result.get('status') not in {'failed', 'cancelled', 'timeout'}, 'approval target scan failed')
    assert_safe(result)
    initiator.request('GET', f'/api/v1/meta/executions/{scan_id}', (404,))
    return result


def apply_cdc_schema_approval(client, task_id, blocked_execution, mutate, *, initiator,
                              initial_scan_id, initiator_id, approver_id):
    require(int(initiator_id) > 0 and int(approver_id) > 0 and int(initiator_id) != int(approver_id),
            'schema approval requires two distinct User Principals')
    initial = initiator.request('GET', f'/api/v1/meta/executions/{initial_scan_id}', (200,)).payload
    require(initial.get('triggered_by') == int(initiator_id), 'initial scan changed its initiating User')
    client.request('GET', f'/api/v1/meta/executions/{initial_scan_id}', (404,))
    historical = detail(initiator, blocked_execution)
    require(historical.get('trigger_by') == int(initiator_id), 'blocked CDC changed its initiating User')
    path = f'{OWNER}/task-definitions/{task_id}/schema-change'
    change = client.request('GET', path, (200,)).payload
    approval = {'fields': [{'source': 'scan_extra', 'target': 'scan_extra', 'target_type': 'string', 'nullable': True}]}
    require(change.get('task_id') == task_id and change.get('execution_id') == blocked_execution
            and change.get('status') == 'pending' and change.get('approvable') is True
            and change.get('suggested_fields') == approval['fields'], 'CDC additive approval facts changed')
    applied = mutate(client, path + '/approve', approval)
    require(applied.get('id') == change.get('id') and applied.get('status') == 'applied'
            and applied.get('metadata_scan_status') == 'success' and applied.get('metadata_scan_attempt') == 1
            and applied.get('to_revision') == change.get('from_revision', 0) + 1, 'CDC approval scan did not submit exactly once')
    scan_id = canonical_uuid(applied.get('metadata_scan_execution_id'))
    def scanned():
        result = assert_schema_scan_origin(initiator, client, scan_id, approver_id)
        return result.get('status') == 'success'
    until(scanned)
    require(detail(initiator, blocked_execution).get('trigger_by') == historical['trigger_by'],
            'approval rewrote the historical CDC User')
    return path, approval, scan_id


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


def run_browser(phase, execution_ids, subject, run_id, secret, action=None):
    require(phase in {'live', 'recovery', 'revocation'}, 'unknown browser phase')
    require((action is not None) == (phase != 'recovery'), 'browser action binding changed')
    execution_ids = [canonical_uuid(value) for value in execution_ids]
    require(len(execution_ids) == (3 if phase == 'recovery' else 1), 'browser execution scope changed')
    required_checks = {
        'live': ['timeline', 'automatic_committed_progress', 'no_samples'],
        'recovery': ['old_failure', 'recovery_source', 'record_failure_code', 'no_samples'],
        'revocation': ['previously_readable', 'old_content_cleared', 'fresh_monitor_only_denied', 'no_samples'],
    }[phase]
    evidence = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / f'continuous-browser-{phase}.json'
    evidence.unlink(missing_ok=True)
    with tempfile.TemporaryDirectory(prefix='continuous-browser-', dir=secret) as temporary:
        control = Path(temporary)
        prefix = 'PARENT' if phase == 'revocation' else 'TEST'
        environment = dict(os.environ, ADDP_CONTINUOUS_BROWSER_PHASE=phase,
                           ADDP_CONTINUOUS_BROWSER_EXECUTIONS=json.dumps(execution_ids),
                           ADDP_CONTINUOUS_BROWSER_PRINCIPAL=str(subject['principal_id']),
                           ADDP_CONTINUOUS_BROWSER_CONTROL=str(control),
                           ADDP_CONTINUOUS_BROWSER_USERNAME=os.environ[f'ADDP_ONLINE_{prefix}_USER_USERNAME'],
                           ADDP_CONTINUOUS_BROWSER_PASSWORD=os.environ[f'ADDP_ONLINE_{prefix}_USER_PASSWORD'])
        marker = {'run_id': run_id, 'phase': phase, 'execution_id': execution_ids[0]}
        # Raw Playwright output may contain login fill arguments; keep it private.
        with (control / 'output.log').open('w') as output:
            process = subprocess.Popen(['npm', 'exec', '--', 'playwright', 'test',
                         '--config=playwright.online.config.js', '--output=' + str(control / 'playwright'),
                         'e2e/online/continuous-execution-events.spec.js'],
                         cwd=ROOT / 'console/frontend', env=environment, stdout=output,
                         stderr=subprocess.STDOUT, start_new_session=True)
            try:
                if action is not None:
                    deadline = time.monotonic() + 100
                    ready = control / 'ready.json'
                    while not ready.exists():
                        require(process.poll() is None and time.monotonic() < deadline,
                                'browser did not reach its authorized observation')
                        time.sleep(.2)
                    require(json.loads(ready.read_text()) == marker, 'browser control identity changed')
                    action()
                    release = control / 'release.tmp'
                    release.write_text(json.dumps(marker))
                    release.replace(control / 'release.json')
                require(process.wait(timeout=180) == 0, 'continuous browser acceptance failed: ' + phase)
            finally:
                if process.poll() is None:
                    os.killpg(process.pid, signal.SIGTERM)
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                        process.wait(timeout=5)
        require(evidence.is_file(), 'browser report missing')
        result = json.loads(evidence.read_text())
        expected = {'schema_version': 'addp.continuous-execution-browser/v1', 'phase': phase,
                    'run_id': run_id, 'tenant_id': str(subject['tenant_id']),
                    'principal_id': str(subject['principal_id']), 'execution_ids': execution_ids,
                    'checks': required_checks, 'result': 'passed'}
        require(result == expected, 'browser evidence contract changed')
        return result


def assert_scheduled_scan_origin(client, peer, execution, scan_id, principal):
    require(execution.get('trigger_type') == 'scheduled' and execution.get('trigger_by') == int(principal)
            and execution.get('status') == 'success', 'scheduled execution did not preserve its authorized User')
    scan = client.request('GET', f'/api/v1/meta/executions/{scan_id}', (200,)).payload
    require(scan.get('triggered_by') == int(principal)
            and scan.get('parent_execution_id') == execution['execution_id']
            and scan.get('status') not in {'failed', 'cancelled', 'timeout'},
            'scheduled scan did not inherit the exact producing execution and User')
    assert_safe(scan)
    parent = peer.request('GET', f'{OWNER}/executions/{execution["execution_id"]}', (200,)).payload
    assert_projection(parent, execution['execution_id'])
    hidden = peer.request('GET', f'/api/v1/meta/executions/{scan_id}', (404,)).payload
    require(isinstance(hidden, dict) and 'execution_id' not in hidden, 'scheduled scan leaked to another User')
    return scan.get('status') == 'success'


def scheduled_task_body(name, pg):
    config = task_config('cdc', pg, 1)
    config['runtime'] = {'boundary': 'bounded'}
    config['load'] = {'mode': 'snapshot'}
    config['source']['locator'] = f'addp://engine/{pg}/path/public/continuous_scan_sibling?type=table'
    config['target']['name'] = 'continuous_scheduled_target'
    config['target']['policy'] = {'apply_mode': 'replace'}
    return {'name': name, 'task_type': 'sync', 'enabled': True, 'schedule': '*/15 * * * * *',
            'auto_scan_metadata': True, 'batch_size': 100, 'config': config}


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
                        {'name': name, 'task_type': 'sync', 'enabled': False, 'auto_scan_metadata': True,
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
        formal_source_scan_id = canonical_uuid(scan.get('execution_id'))
        def scanned():
            item = client.request('GET', f'/api/v1/meta/executions/{formal_source_scan_id}', (200,)).payload
            require(item.get('status') not in {'failed', 'cancelled', 'timeout'}, 'physical source metadata scan failed')
            return item.get('status') == 'success'
        until(scanned)
        report['checks'].append('formal_source_catalog_scan')
        sibling = catalog_item(client, pg, tenant, 'continuous_scan_sibling')
        require(sibling is not None, 'physical scan sibling is missing')
        boundary = API.read_meta_scan_boundary(client, pg, sibling)
        require(len(boundary['ancestors']) >= 2, 'scan baseline must include schema and root')
        API.wait_meta_scan_tick([boundary['sibling'], *boundary['ancestors']])
        for kind in ('kafka', 'cdc'):
            require(catalog_item(client, pg, tenant, f'continuous_{kind}_target') is None,
                    'automatic scan requires a target absent from the baseline catalog')
        report['precise_scan_scope'] = {'sibling_item_id': sibling['id'],
            'ancestor_nodes_verified': len(boundary['ancestors']), 'scans': [], 'target_only_verified': False}
        def target_scan(kind, fields={'id', 'label'}):
            return wait_target_scan(client, pg, tenant, f'continuous_{kind}_target', fields, sibling, boundary)
        kafka_task = create('kafka')
        initial = launch(kafka_task)
        require(physical('produce-initial'), 'business Kafka input failed')
        until(lambda: physical('verify-kafka-initial'))
        until(lambda: any(e['kind'] == 'progress' and e['counters'].get('records_written', 0) >= 2
                          for e in events(client, f'{OWNER}/executions', initial)))
        lifecycle_events(events(client, f'{OWNER}/executions', initial))
        kafka_scan = target_scan('kafka')
        report['precise_scan_scope']['scans'].append({'phase': 'kafka_initial_prepare', **kafka_scan})
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
        def browser_progress():
            require(physical('produce-after-recovery'), 'browser committed progress fixture failed')
            until(lambda: physical('verify-kafka-recovered'))
        report['browser_live'] = run_browser('live', [initial], report['consumer_identity'],
                                             run_id, faults.secret, browser_progress)
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
        require(target_scan('kafka') == kafka_scan, 'Worker recovery repeated the initial target scan')
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
        require(failed.get('error_details', {}).get('code') == 'missing_source_key'
                and client.request('GET', f'{MONITOR}/{resumed}', (200,)).payload.get('error_details', {}).get('code') == 'missing_source_key',
                'record failure lost its stable code in Owner or Monitor')
        require(target_scan('kafka') == kafka_scan, 'manual resume or ordinary events repeated the initial target scan')
        report['checks'].append('kafka_pause_resume_and_record_failure_without_samples')
        report['browser_recovery'] = run_browser('recovery', [initial, recovery_id, resumed],
                                                 report['consumer_identity'], run_id, faults.secret)
        # A hung owner must not cause Monitor to release previously readable facts.
        def owner_unavailable():
            response = client.request('GET', f'{MONITOR}/{initial}', (502, 503))
            assert_safe(response.payload)
            require('execution_id' not in response.payload, 'unavailable Owner leaked an execution')
        faults.stop_owner_during(owner_unavailable)
        compare(initial, 'failed')
        report['checks'].append('owner_unavailable_fail_closed')
        report['browser_revocation'] = run_browser('revocation', [initial], report['reader_identity'],
            run_id, faults.secret, lambda: revoke(reader, admin, login, tenant, initial, mutate))
        report['checks'].append('revoked_token_and_fresh_monitor_only_reader_denied')
        cdc_task = create('cdc')
        cdc = launch(cdc_task)
        until(lambda: physical('verify-cdc-initial'))
        cdc_scan = target_scan('cdc')
        report['precise_scan_scope']['scans'].append({'phase': 'cdc_snapshot_completed', **cdc_scan})
        require(physical('mutate-cdc'), 'CDC mutation fixture failed')
        until(lambda: physical('verify-cdc-mutated'))
        until(lambda: any(e['kind'] == 'progress' and e['counters'].get('records_written', 0) >= 5
                          for e in events(client, f'{OWNER}/executions', cdc)))
        require(target_scan('cdc') == cdc_scan, 'ordinary CDC events repeated the initial target scan')
        require(target_scan('kafka') == kafka_scan, 'CDC scans changed the Kafka sibling target')
        require(physical('alter-cdc'), 'CDC additive schema fixture failed')
        blocked = wait_status(client, cdc, 'failed')
        require(blocked.get('error_details', {}).get('code') == 'schema_change_blocked', 'CDC schema drift did not block')
        compare(cdc, 'failed')
        assert_scan_boundary(client, pg, sibling, boundary)
        API.wait_meta_scan_tick([cdc_scan])
        initiating_user = report['consumer_identity']['principal_id']
        approving_user = report['peer_identity']['principal_id']
        path, approval, scan_id = apply_cdc_schema_approval(peer, cdc_task, cdc, mutate, initiator=client,
            initial_scan_id=formal_source_scan_id, initiator_id=initiating_user, approver_id=approving_user)
        migrated = target_scan('cdc', {'id', 'label', 'scan_extra'})
        require(migrated['scanned_at'] != cdc_scan['scanned_at'], 'approval reused old target metadata')
        report['precise_scan_scope']['scans'].append({'phase': 'cdc_schema_approval',
            'metadata_scan_execution_id': scan_id, **migrated})
        API.wait_meta_scan_tick([migrated])
        repeated = mutate(client, path + '/approve', approval)
        require(repeated.get('metadata_scan_attempt') == 1 and repeated.get('metadata_scan_execution_id') == scan_id,
                'repeated approval submitted another scan')
        require(assert_schema_scan_origin(client, peer, scan_id, approving_user).get('status') == 'success',
                'repeated approval changed the completed scan')
        report['schema_approval_ownership'] = {'initiator_principal_id': initiating_user,
            'approver_principal_id': approving_user, 'original_execution_id': cdc,
            'initial_scan_execution_id': formal_source_scan_id, 'approval_scan_execution_id': scan_id,
            'bidirectional_read_isolation_verified': True, 'original_execution_actor_preserved': True,
            'repeat_by_initiator_preserves_approver': True}
        report['checks'].append('cdc_distinct_approver_scan_origin_and_bidirectional_read_isolation')
        cdc_resumed = launch(cdc_task, 'resume')
        require(cdc_resumed != cdc and detail(client, cdc_resumed).get('trigger_by') == int(initiating_user),
                'schema approval resume reused execution identity or changed its User')
        until(lambda: physical('verify-cdc-altered'))
        until(lambda: any(e['kind'] == 'progress' and e['counters'].get('records_written', 0) > 0
                          for e in events(client, f'{OWNER}/executions', cdc_resumed)))
        require(target_scan('cdc', {'id', 'label', 'scan_extra'}) == migrated,
                'CDC resume or repeated approval repeated metadata scanning')
        require(target_scan('kafka') == kafka_scan, 'approval scan changed the Kafka sibling target')
        report['checks'].append('cdc_additive_approval_target_scan_and_committed_resume')
        report['precise_scan_scope']['target_only_verified'] = True
        report['checks'].append('initial_scans_preserve_sibling_and_ancestors')
        name = next(name for task, kind, name in tasks if task == cdc_task)
        mutate(client, f'{OWNER}/task-definitions/{cdc_task}/stop', {'confirmed': True, 'confirmation_text': name})
        wait_status(client, cdc_resumed, 'cancelled')
        compare(cdc_resumed, 'cancelled')
        require(physical('verify-clean'), 'CDC capture resources survived confirmed stop')
        report['checks'].append('cdc_snapshot_update_delete_insert_safe_events_and_confirmed_cleanup')
        # The existing Transfer deployment also runs its bounded worker. Exercise
        # a real owner timer, then pause before the next tick while this run drains.
        require(catalog_item(client, pg, tenant, 'continuous_scheduled_target') is None,
                'scheduled target must be absent from the baseline catalog')
        name = f'online-{run_id}-scheduled'
        definition = mutate(client, f'{OWNER}/task-definitions', scheduled_task_body(name, pg), (201,))
        task_id = API.require_positive_int(definition, 'id')
        tasks.append((task_id, 'scheduled', name))
        report['resources'].append(f'{OWNER}/task-definitions/{task_id}')
        checkpoint()
        require(definition.get('enabled') is True and definition.get('next_run_at'), 'schedule was not armed')
        def scheduled_run():
            runs = history(client, task_id)
            require(len(runs) <= 1, 'scheduled task created duplicate initial executions')
            return runs[0] if runs else None
        pending = until(scheduled_run)
        scheduled_id = canonical_uuid(pending.get('execution_id'))
        report['executions'].append(scheduled_id)
        checkpoint()
        mutate(client, f'{OWNER}/task-definitions/{task_id}/pause')
        execution = wait_status(client, scheduled_id, 'success')
        scan_feedback = execution.get('metadata', {}).get('metadata_scan', {})
        require(scan_feedback.get('status') == 'submitted', 'scheduled write did not submit its target scan')
        scheduled_scan_id = canonical_uuid(scan_feedback.get('execution_id'))
        until(lambda: assert_scheduled_scan_origin(client, peer, execution, scheduled_scan_id, initiating_user))
        precise = wait_target_scan(client, pg, tenant, 'continuous_scheduled_target', {'id', 'label'}, sibling, boundary)
        require(physical('verify-scheduled'), 'scheduled transfer did not commit the expected physical rowset')
        paused = client.request('GET', f'{OWNER}/task-definitions/{task_id}', (200,)).payload
        require(paused.get('enabled') is False and not paused.get('next_run_at'), 'pause left the schedule armed')
        assert_scan_boundary(client, pg, sibling, boundary)
        require(target_scan('cdc', {'id', 'label', 'scan_extra'}) == migrated
                and target_scan('kafka') == kafka_scan, 'scheduled scan changed existing continuous targets')
        report['precise_scan_scope']['scans'].append({'phase': 'bounded_scheduled',
            'metadata_scan_execution_id': scheduled_scan_id, **precise})
        report['scheduled_scan_ownership'] = {'execution_id': scheduled_id, 'scan_execution_id': scheduled_scan_id,
            'principal_id': initiating_user, 'trigger_type': 'scheduled', 'parent_and_user_verified': True,
            'parent_read_verified': True, 'peer_read_denied': True, 'physical_rowset_verified': True, 'schedule_paused': True}
        report['checks'].append('bounded_owner_schedule_current_subject_precise_scan_and_read_isolation')

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
                                         ('peer', peer, {'monitor.execution.read', 'transfer.task.read',
                                                         'transfer.task.update', 'meta.scan_task.read'}),
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
