"""Continuous Worker and safe tenant execution events on disposable Hosted T4."""
from __future__ import annotations

from datetime import datetime, timezone
from importlib import import_module
import json
import hashlib
import hmac
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import math
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import tempfile
import threading
import time
import uuid
import urllib.error

API = import_module('scripts.utils.online-api')
FAULTS = import_module('scripts.test.continuous-execution-faults')
REGISTRATION = import_module('scripts.test.online-engine-registration')
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
            'meta.scan_task.execute', 'meta.scan_task.read', 'system.execution_authorization.create',
            'monitor.alert_incident.read', 'monitor.alert_incident.update',
            'monitor.notification_destination.create', 'monitor.notification_destination.read',
            'monitor.notification_destination.delete', 'monitor.notification_delivery.read'}
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
OWNER_METADATA = set('initial_authorization metadata_scan lineage_facts continuous stop_reason recovery_reason recovered_from_execution_id recovery_attempt '
                     'recovery_consecutive_failures recovery_backoff_seconds recovery_not_before recovery_circuit_state'.split())
PHYSICAL_ACTIONS = {'produce-initial', 'produce-after-recovery', 'produce-invalid', 'mutate-cdc', 'alter-cdc', 'verify-cdc-altered',
                    'verify-kafka-initial', 'verify-kafka-recovered', 'verify-cdc-initial',
                    'verify-cdc-mutated', 'verify-scheduled', 'verify-clean', 'risk-mutate-cdc',
                    'risk-pause-source', 'risk-unpause-source'}
OWNER_OUTAGE_CHECKS = ['previously_readable', 'event_transport_failure_isolated', 'event_transport_recovered',
                       'real_owner_503', 'old_details_tree_steps_events_cleared', 'old_list_and_total_cleared',
                       'recovered_authorized_reads', 'no_samples']
PHASES = {'preflight', 'source_scan', 'source_catalog', 'kafka_initial', 'kafka_visibility', 'kafka_browser', 'worker_recovery',
          'kafka_record_failure', 'owner_unavailable', 'reader_revocation', 'kafka_hard_delete', 'kafka_graceful',
          'cdc_initial', 'cdc_owner_outage',
          'cdc_mutation', 'cdc_monitor_initial', 'cdc_pause', 'cdc_buffer_growth', 'cdc_monitor_resume',
          'cdc_approval', 'cdc_resume', 'cdc_risk', 'cdc_stop', 'scheduled_create', 'scheduled_admission',
          'scheduled_pause', 'scheduled_completion', 'scheduled_scan', 'scheduled_result', 'cleanup', 'deleted_history'}


class KafkaHardDeleteFailure(SuiteError):
    """Only closed failure facts are published; the original exception stays private."""
    def __init__(self, step, error, delete_status):
        super().__init__('Kafka hard-delete verification failed')
        cause = error.__cause__
        if isinstance(cause, TimeoutError) or isinstance(cause, urllib.error.URLError) and isinstance(cause.reason, TimeoutError):
            reason = 'transport_timeout'
        elif isinstance(cause, urllib.error.URLError):
            reason = 'transport_unavailable'
        elif isinstance(cause, json.JSONDecodeError):
            reason = 'invalid_response'
        elif isinstance(error, subprocess.SubprocessError):
            reason = 'observer_failed'
        elif isinstance(error, SuiteError) and str(error) == 'continuous Hosted acceptance interrupted':
            reason = 'interrupted'
        else:
            reason = 'contract_failed'
        self.diagnostic = {'step': step, 'reason': reason, 'delete_request_timeout_seconds': 60}
        if delete_status is not None:
            self.diagnostic['delete_http_status'] = delete_status


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
        if 'initial_authorization' in item['metadata']:
            initial = item['metadata']['initial_authorization']
            require(isinstance(initial, dict) and set(initial) <= {'status', 'code'}
                    and isinstance(initial.get('status'), str) and initial['status'] in {'pending', 'issued', 'denied'}
                    and ('code' not in initial or isinstance(initial['code'], str)),
                    'Owner exposed invalid initial authorization status facts')
            codes = {'pending': {None, 'system_unavailable', 'authorization_pending', 'scan_pending', 'scan_failed'},
                     'issued': {None},
                     'denied': {'production_ineligible', 'source_access_denied', 'execution_permission_denied',
                                'target_history_exists', 'engine_changed'}}
            require(initial.get('code') in codes[initial['status']],
                    'Owner exposed invalid initial authorization status facts')
        scan = item['metadata'].get('metadata_scan')
        if scan is not None:
            require(isinstance(scan, dict) and set(scan) <= {'status', 'attempt', 'execution_id', 'basic_execution_id', 'error_code'},
                    'Owner exposed private metadata scan facts')
    error = item.get('error_details') or {}
    require(isinstance(error, dict) and set(error) <= {'category', 'code', 'error_code', 'message', 'failed_targets_count'},
            'execution exposed private failure details')


def events(client, prefix, execution_id, *, evidence=None, after_first=None):
    """Drain cursor pages and reject duplicates, unknown fields, samples and offset payloads."""
    cursor, first_cursor, result = 0, 0, []
    for page_index in range(600):
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
            try:
                occurred = datetime.fromisoformat(item['occurred_at'].replace('Z', '+00:00'))
                require(occurred.tzinfo is not None and occurred >= retained,
                        'event timestamp is outside its retention window')
            except (KeyError, ValueError, TypeError, AttributeError):
                raise SuiteError('event timestamp is invalid') from None
            counters = item.get('counters')
            require(isinstance(counters, dict) and set(counters) <= {'records_read', 'records_written'},
                    'continuous event exposed undeclared counters')
            require(all(isinstance(n, int) and not isinstance(n, bool) and n >= 0 for n in counters.values()),
                    'event counters must be nonnegative integers')
            cursor = identity
            result.append(item)
        require(isinstance(page['next_cursor'], int) and not isinstance(page['next_cursor'], bool)
                and page['next_cursor'] == (cursor if page['items'] else 0),
                'event next_cursor does not match the last fact')
        if page_index == 0:
            first_cursor = cursor
            if after_first is not None:
                require(first_cursor > 0, 'live pagination did not capture an initial cursor')
                after_first()
                # New commits can follow a page that was the tail when read.
                continue
        if not page['has_more']:
            if evidence is not None:
                evidence.update(page_size=2, page_count=page_index + 1, event_count=len(result),
                                first_cursor=first_cursor, last_cursor=cursor)
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
    require(kind in {'kafka', 'cdc', 'kafka-graceful'} and pg > 0 and kafka > 0, 'invalid physical source identity')
    is_kafka = kind != 'cdc'
    source = {'locator': f'addp://engine/{pg}/path/public/continuous_source?type=table',
              'data_type': 'table', 'representation': 'native'}
    detection = {'type': 'cdc', 'bootstrap': 'initial_snapshot'}
    if is_kafka:
        source = {'locator': f'addp://engine/{kafka}/path/continuous_events?type=topic', 'representation': 'native',
                  'change_stream': {'envelope': 'record', 'encoding': 'json',
                                    'key': {'source': 'value', 'fields': ['id']},
                                    'start': {'mode': 'committed', 'initial': 'earliest'}, 'poll_batch_size': 100}}
        detection = {'type': 'kafka'}
    return {'runtime': {'boundary': 'continuous',
                        'record_failure': {'mode': 'dead_letter' if kind == 'kafka-graceful' else 'block'}},
            'load': {'mode': 'incremental', 'change_detection': detection}, 'source': source,
            'target': {'parent_locator': f'addp://engine/{pg}/path/public?type=schema',
                       'name': f"continuous_{kind.replace('-', '_')}_target", 'data_type': 'table', 'representation': 'native',
                       'policy': {'apply_mode': 'upsert' if is_kafka else 'upsert_delete', 'keys': ['id']}},
            'transforms': [{'type': 'field_mapping', 'version': 'v1', 'mode': 'project',
                            'fields': [{'source': 'id', 'target': 'id', 'target_type': 'bigint', 'nullable': False},
                                       {'source': 'label', 'target': 'label', 'target_type': 'string', 'nullable': False}]}]}


def physical(action, *, observe=None):
    require(action in PHYSICAL_ACTIONS, 'invalid physical fixture action')
    require(observe is None or action in {'verify-cdc-initial', 'verify-kafka-initial'},
            'physical observation is limited to initial target probes')
    result = subprocess.run(['bash', 'business/scripts/online-continuous-fixture.sh', action], cwd=ROOT,
                            capture_output=True, timeout=20)
    # stdout/stderr can contain business values or database connection facts; never publish them.
    if observe is not None:
        observe({0: 'matched', 2: 'query_failed', 3: 'rows_mismatch'}.get(result.returncode, 'command_failed'))
    return result.returncode == 0


def until(operation, timeout=120):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = operation()
        if value:
            return value
        time.sleep(1)
    raise SuiteError('continuous execution did not converge before the deadline')


CDC_RISK_CODES = {'cdc_source_recovery_lost', 'cdc_buffer_recovery_lost'}
ALERTS = '/api/v1/monitor/alerts'
DESTINATIONS = '/api/v1/monitor/webhook-destinations'
DELIVERIES = '/api/v1/monitor/webhook-deliveries'


class CDCRiskReceiver:
    """Actual loopback HTTP receipts, authenticated independently of delivery audit."""
    def __init__(self, task_id):
        self.task_id = task_id
        self.secret = os.urandom(32).hex()
        self.receipts = {}
        self.failure = False
        self.lock = threading.Lock()
        receiver = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def do_POST(self):
                self.connection.settimeout(3)
                try:
                    require(self.path == '/cdc-risk', 'unexpected webhook path')
                    require(self.headers.get('Content-Type') == 'application/json'
                            and self.headers.get('Transfer-Encoding') is None, 'invalid webhook framing')
                    for header in ('Content-Length', 'X-ADDP-Webhook-ID', 'X-ADDP-Webhook-Timestamp',
                                   'X-ADDP-Webhook-Signature'):
                        require(len(self.headers.get_all(header, [])) == 1, 'ambiguous webhook header')
                    length = int(self.headers['Content-Length'])
                    require(0 < length <= 65536, 'webhook exceeds bound')
                    raw = self.rfile.read(length)
                    require(len(raw) == length, 'incomplete webhook')
                    receiver.accept(raw, self.headers)
                    self.send_response(202)
                except Exception:
                    with receiver.lock:
                        receiver.failure = True
                    self.send_response(400)
                self.send_header('Content-Length', '0')
                self.end_headers()

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.server.daemon_threads = True
        self.url = f'http://127.0.0.1:{self.server.server_port}/cdc-risk'
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()

    def accept(self, raw, headers):
        timestamp = headers['X-ADDP-Webhook-Timestamp']
        require(re.fullmatch(r'[0-9]{10}', timestamp) and abs(time.time() - int(timestamp)) <= 30,
                'webhook timestamp invalid')
        signature = 'v1=' + hmac.new(self.secret.encode(), timestamp.encode() + b'.' + raw, hashlib.sha256).hexdigest()
        require(hmac.compare_digest(signature, headers['X-ADDP-Webhook-Signature']), 'webhook signature invalid')

        def unique(pairs):
            result = dict(pairs)
            require(len(result) == len(pairs), 'duplicate webhook field')
            return result

        body = json.loads(raw, object_pairs_hook=unique)
        require(set(body) == {'schema_version', 'delivery_id', 'event', 'incident', 'console_url'}
                and body['schema_version'] == 'monitor.alert.webhook/v1', 'webhook schema invalid')
        delivery = canonical_uuid(body['delivery_id'])
        require(headers['X-ADDP-Webhook-ID'] == delivery, 'webhook delivery identity mismatch')
        event, incident = body['event'], body['incident']
        require(set(event) == {'event_id', 'type', 'occurred_at', 'from_severity', 'to_severity'}
                and event['type'] in {'opened', 'resolved'}, 'webhook event invalid')
        canonical_uuid(event['event_id'])
        require(set(incident) == {'id', 'module', 'task_type', 'source_task_id', 'execution_id', 'signal_code',
                                  'severity', 'status', 'opened_at', 'resolved_at'}, 'webhook incident fields invalid')
        assert_safe(body)
        if incident['signal_code'] not in CDC_RISK_CODES:
            return  # The same tenant may also emit ordinary continuous-task alerts.
        require(incident['module'] == 'transfer' and incident['task_type'] == 'sync'
                and incident['source_task_id'] == str(self.task_id) and incident['severity'] == 'critical'
                and type(incident['id']) is int and incident['id'] > 0, 'webhook risk identity mismatch')
        require(incident['status'] == ('open' if event['type'] == 'opened' else 'resolved'),
                'webhook risk transition mismatch')
        receipt = {'delivery_id': delivery, 'event_id': event['event_id'], 'incident_id': incident['id'],
                   'event_type': event['type'], 'signal_code': incident['signal_code'], 'http_status': 202}
        with self.lock:
            require(len(self.receipts) < 16 or delivery in self.receipts, 'webhook receipt bound exceeded')
            require(delivery not in self.receipts or self.receipts[delivery] == receipt, 'delivery identity reused')
            self.receipts[delivery] = receipt

    def snapshot(self):
        with self.lock:
            require(not self.failure, 'signed CDC webhook receiver rejected a request')
            return list(self.receipts.values())

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)
        require(not self.thread.is_alive() and self.server.socket.fileno() == -1, 'webhook receiver survived cleanup')


def cdc_risk_alerts(client, task_id, generation):
    page = client.request('GET', ALERTS + '?module=transfer&page_size=100', (200,)).payload
    require(type(page) is dict and type(page.get('data')) is list and page.get('total', 101) <= 100,
            'risk alert inventory incomplete')
    result = {}
    for item in page['data']:
        if item.get('source_task_id') != str(task_id) or item.get('signal_code') not in CDC_RISK_CODES:
            continue
        assert_safe(item)
        require(item.get('tenant_id') == int(os.environ['ADDP_ONLINE_TEST_TENANT_ID'])
                and item.get('module') == 'transfer' and item.get('task_type') == 'sync'
                and item.get('severity') == 'critical' and item.get('details', {}).get('generation') == str(generation),
                'risk alert binding invalid')
        code = item['signal_code']
        require(code not in result, 'duplicate CDC generation risk incident')
        result[code] = item
    return result


def cdc_risk_sample(client, task_id, generation, field, status, *, after):
    capture = client.request('GET', f'{OWNER}/task-definitions/{task_id}', (200,)).payload.get('capture') or {}
    require(capture.get('generation') == generation, 'risk fault changed generation')
    value = capture.get(field)
    if value is None:
        return None
    assert_safe(value)
    sampled = cdc_sample_time(value, datetime.now(timezone.utc))
    if sampled is None or sampled <= after or value.get('status') != status:
        return None
    return value


def cdc_risk_container(variable, service):
    FAULTS.SUPPORT.deployment_secret()
    container_id = os.environ.get(variable, '')
    require(re.fullmatch(r'[0-9a-f]{64}', container_id), 'risk fault container not pinned')
    result = subprocess.run(['docker', 'inspect', container_id], check=True, capture_output=True, text=True, timeout=10)
    container, = json.loads(result.stdout)
    labels = container['Config']['Labels']
    require(container['Id'] == container_id and container['State']['Running'] is True
            and labels.get('com.docker.compose.project') == 'addp-infra'
            and labels.get('com.docker.compose.service') == service
            and labels.get('com.docker.compose.project.working_dir') == str(ROOT), 'risk fault container ownership mismatch')
    return container_id


def cdc_risk_trim(binding, offset):
    require(type(offset) is int and offset > 0, 'risk trim offset invalid')
    broker = cdc_risk_container('ADDP_ONLINE_DLQ_REDPANDA_ID', 'redpanda')
    require(bool(os.environ.get('INFRA_KAFKA_ADMIN_PASSWORD')), 'risk trim Infra owner identity missing')
    # Shell program is constant; the private topic and position are positional args.
    subprocess.run(['docker', 'exec', '-e', 'INFRA_KAFKA_ADMIN_PASSWORD', '-e', 'INFRA_KAFKA_ADMIN_USERNAME', broker, 'sh', '-c',
        'exec rpk topic trim-prefix "$1" --offset "$2" --partitions 0 --no-confirm '
        '-X brokers=redpanda:29092 -X "user=${INFRA_KAFKA_ADMIN_USERNAME:-admin}" -X "pass=$INFRA_KAFKA_ADMIN_PASSWORD" '
        '-X sasl.mechanism=SCRAM-SHA-256', '_', binding['topic'], str(offset)],
        check=True, capture_output=True, text=True, timeout=20)


def verify_cdc_risk_closure(client, tenant, task_id, engine_id, execution_id, name, run_id,
                            mutate, launch, report, checkpoint):
    proof = report['cdc_risk_closure'] = {'task_id': task_id, 'cleanup': 'pending', 'stage': 'baseline'}
    healthy = until(lambda: cdc_task_sample(client, task_id, engine_id))
    generation = healthy['generation']
    proof['generation'] = generation
    binding = cdc_capture_binding(tenant, task_id, engine_id, generation)
    receiver = CDCRiskReceiver(task_id)
    destination_id = None
    paused = False

    def stage(value):
        proof['stage'] = value
        checkpoint()

    def alerts_ready(codes, status):
        values = cdc_risk_alerts(client, task_id, generation)
        return values if set(values) == set(codes) and all(v['status'] == status for v in values.values()) else None

    def delivered(incidents, event_type):
        receipts = receiver.snapshot()
        rows = client.request('GET', DELIVERIES + f'?destination_id={destination_id}&page_size=100', (200,)).payload
        require(type(rows) is dict and type(rows.get('data')) is list and rows.get('total', 101) <= 100,
                'risk delivery audit incomplete')
        result = []
        for code, incident in incidents.items():
            matched = [r for r in rows['data'] if r['incident_id'] == incident['id'] and r['event_type'] == event_type]
            require(len(matched) <= 1, 'risk transition created duplicate delivery')
            if not matched or matched[0]['status'] != 'delivered':
                return None
            row = matched[0]
            receipt = next((r for r in receipts if r['delivery_id'] == row['delivery_id']), None)
            if receipt is None:
                return None
            require(receipt['signal_code'] == code and receipt['incident_id'] == incident['id']
                    and receipt['event_type'] == event_type and row.get('last_http_status') == 202,
                    'risk receipt and delivery audit differ')
            result.append(receipt)
        return result

    try:
        require(not cdc_risk_alerts(client, task_id, generation), 'healthy task already has loss incidents')
        destination = mutate(client, DESTINATIONS, {'name': f'online-{run_id}-cdc-risk', 'url': receiver.url,
            'secret': receiver.secret, 'enabled': True, 'event_types': ['opened', 'resolved']}, (201,))
        destination_id = API.require_positive_int(destination, 'id')
        proof['destination_id'] = destination_id
        stage('buffer_fault')
        mutate(client, f'{OWNER}/task-definitions/{task_id}/pause')
        wait_status(client, execution_id, 'cancelled')
        before = until(lambda: cdc_task_sample(client, task_id, engine_id, generation))
        require(physical('risk-mutate-cdc'), 'risk source mutation failed')
        def growth():
            sample = cdc_task_sample(client, task_id, engine_id, generation)
            if sample is None:
                return None
            require(sample['buffer']['committed_offset'] == before['buffer']['committed_offset'],
                    'paused risk consumer committed events')
            return sample if int(sample['buffer']['latest_offset']) > int(before['buffer']['committed_offset']) else None
        grown = until(growth)
        cdc_risk_trim(binding, int(grown['buffer']['latest_offset']))
        trimmed_at = datetime.now(timezone.utc)
        lost = until(lambda: cdc_risk_sample(client, task_id, generation, 'buffer_observation', 'available', after=trimmed_at))
        require(lost.get('recovery_state') == 'lost' and int(lost['earliest_offset']) > int(lost['committed_offset']),
                'trim did not prove lost recovery range')
        buffer_alert = until(lambda: alerts_ready({'cdc_buffer_recovery_lost'}, 'open'))
        require(buffer_alert['cdc_buffer_recovery_lost']['details']['reason'] == 'buffer_position_lost', 'wrong buffer loss reason')
        proof['opened_deliveries'] = until(lambda: delivered(buffer_alert, 'opened'))
        stage('source_fault')
        connect = cdc_risk_container('ADDP_ONLINE_CDC_CONNECT_ID', 'kafka-connect')
        subprocess.run(['docker', 'exec', connect, 'curl', '-fsS', '--max-time', '10', '-X', 'DELETE',
            'http://127.0.0.1:8083/connectors/' + binding['connector']], check=True, capture_output=True, timeout=15)
        def inactive():
            value = subprocess.run(['bash', 'business/scripts/online-continuous-fixture.sh', 'observe-cdc-source',
                binding['slot'], binding['publication']], cwd=ROOT, check=True, capture_output=True, text=True, timeout=15)
            native = json.loads(value.stdout)
            require(native.get('identity_matches') is True, 'source fault ownership mismatch')
            return native.get('slot_active') is False
        until(inactive, timeout=30)
        subprocess.run(['bash', 'business/scripts/online-continuous-fixture.sh', 'risk-drop-slot',
            binding['slot'], binding['publication']], cwd=ROOT, check=True, capture_output=True, timeout=15)
        dropped_at = datetime.now(timezone.utc)
        until(lambda: cdc_risk_sample(client, task_id, generation, 'postgresql_observation', 'missing', after=dropped_at))
        opened = until(lambda: alerts_ready(CDC_RISK_CODES, 'open'))
        require(opened['cdc_source_recovery_lost']['details']['reason'] == 'slot_missing', 'wrong source loss reason')
        proof['opened_deliveries'] = until(lambda: delivered(opened, 'opened'))
        source_id = opened['cdc_source_recovery_lost']['id']
        mutate(client, f'{ALERTS}/{source_id}/acknowledge')
        stage('missing_sample')
        paused = True  # Also unpause if the pause command outcome becomes uncertain.
        require(physical('risk-pause-source'), 'source pause failed')
        paused_at = datetime.now(timezone.utc)
        until(lambda: cdc_risk_sample(client, task_id, generation, 'postgresql_observation', 'unavailable', after=paused_at))
        frozen = cdc_risk_alerts(client, task_id, generation)['cdc_source_recovery_lost']
        require(frozen['id'] == source_id and frozen['status'] == 'acknowledged', 'missing sample closed acknowledged risk')
        # Span two normal 15-second evaluations without changing product intervals.
        deadline = time.monotonic() + 32
        samples = 0
        while time.monotonic() < deadline:
            value = cdc_risk_alerts(client, task_id, generation)['cdc_source_recovery_lost']
            require(value['id'] == source_id and value['status'] == 'acknowledged'
                    and value['last_observed_at'] == frozen['last_observed_at'] and not value.get('resolved_at'),
                    'unknown source sample changed acknowledged incident')
            require(not any(r['event_type'] == 'resolved' for r in receiver.snapshot()), 'unknown sample sent recovery')
            samples += 1
            time.sleep(2)
        proof['missing_sample_checks'] = samples
        proof['acknowledged_incident_preserved'] = True
        require(physical('risk-unpause-source'), 'source unpause failed')
        paused = False
        restored_at = datetime.now(timezone.utc)
        until(lambda: cdc_risk_sample(client, task_id, generation, 'postgresql_observation', 'missing', after=restored_at))
        still_active = cdc_risk_alerts(client, task_id, generation)
        require(still_active['cdc_source_recovery_lost']['id'] == source_id
                and still_active['cdc_source_recovery_lost']['status'] == 'acknowledged'
                and still_active['cdc_buffer_recovery_lost']['status'] == 'open', 'fault disappeared without explicit close')
        proof['confirmed_fault_after_sampling_resumed'] = True
        stage('confirmed_close')
        mutate(client, f'{OWNER}/task-definitions/{task_id}/stop', {'confirmed': True, 'confirmation_text': name})
        closed = until(lambda: alerts_ready(CDC_RISK_CODES, 'resolved'))
        require({v['id'] for v in closed.values()} == {v['id'] for v in opened.values()}, 'close replaced risk incidents')
        proof['resolved_deliveries'] = until(lambda: delivered(closed, 'resolved'))
        require(physical('verify-clean'), 'fault generation capture resources remain')
        stage('new_generation')
        new_execution = launch(task_id)
        def rebuilt():
            value = cdc_task_sample(client, task_id, engine_id)
            return value if value and value['generation'] > generation and value['buffer']['lag_offsets'] == '0' else None
        new = until(rebuilt)
        require(new['generation'] > generation and new['execution_id'] == new_execution
                and new['buffer']['lag_offsets'] == '0', 'new capture generation not healthy')
        require(all(v['status'] == 'resolved' for v in cdc_risk_alerts(client, task_id, generation).values()),
                'healthy new generation reopened old risks')
        mutate(client, f'{OWNER}/task-definitions/{task_id}/stop', {'confirmed': True, 'confirmation_text': name})
        wait_status(client, new_execution, 'cancelled')
        require(physical('verify-clean'), 'new generation capture resources remain')
        proof.update(new_generation=new['generation'], new_execution_id=new_execution,
                     original_incident_ids={k: v['id'] for k, v in opened.items()}, result='passed')
        stage('completed')
    finally:
        original_failure = sys.exc_info()[0] is not None
        cleanup_errors = []
        if paused:
            try:
                require(physical('risk-unpause-source'), 'fault source remained paused')
            except Exception:
                cleanup_errors.append('source_unpause_failed')
        try:
            if destination_id is not None:
                client.request('DELETE', f'{DESTINATIONS}/{destination_id}', (200,))
                remaining = client.request('GET', DESTINATIONS, (200,), response_type=list).payload
                require(isinstance(remaining, list) and all(d['id'] != destination_id for d in remaining),
                        'risk webhook destination remains')
                proof['destination_deleted'] = True
        except Exception:
            cleanup_errors.append('destination_cleanup_failed')
        try:
            receiver.close()
            proof['receiver_closed'] = True
        except Exception:
            cleanup_errors.append('receiver_cleanup_failed')
        proof['cleanup'] = 'failed' if cleanup_errors else 'passed'
        proof['cleanup_failures'] = cleanup_errors
        checkpoint()
        if cleanup_errors and not original_failure:
            raise SuiteError('CDC risk acceptance cleanup failed')


def cdc_capture_binding(tenant, task_id, engine_id, generation):
    require_transfer_fixture(tenant, task_id)
    postgres_id = os.environ.get('ADDP_ONLINE_SCAN_POSTGRES_ID', '')
    require(type(generation) is int and generation > 0 and type(engine_id) is int and engine_id > 0
            and os.environ.get('ADDP_ONLINE_CONTINUOUS_PG_ENGINE_ID') == str(engine_id)
            and re.fullmatch(r'[0-9a-f]{64}', postgres_id), 'CDC observation fixture identity mismatch')
    postgres = import_module('scripts.test.execution-retention-readonly-online')
    query = """BEGIN READ ONLY;
SET LOCAL statement_timeout='3s'; SET LOCAL lock_timeout='1s';
SELECT jsonb_build_object('slot', p.slot_name, 'publication', p.publication_name, 'connector', r.connector_name, 'topic', r.topic_name)
FROM transfer.capture_resources r JOIN transfer.postgresql_capture_resources p ON p.capture_resource_id=r.id
JOIN transfer.transfer_tasks t ON t.id=r.task_id AND t.tenant_id=r.tenant_id
WHERE r.tenant_id=:'tenant'::bigint AND r.task_id=:'task'::bigint
 AND r.generation=:'generation'::bigint AND r.source_engine_id=:'engine'::bigint
 AND r.source_type='postgresql' AND r.source_database='continuous_fixture'
 AND r.source_schema='public' AND r.source_table='continuous_source' AND r.status='running'
 AND t.deleted_at IS NULL AND p.slot_owned AND p.publication_owned;
COMMIT;
"""
    try:
        binding = json.loads(postgres.sql(query, expected_suite=SUITE, postgres_id=postgres_id,
            variables={'tenant': tenant, 'task': task_id, 'engine': engine_id, 'generation': generation}))
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        raise SuiteError('CDC observation binding unavailable') from None
    require(isinstance(binding, dict) and set(binding) == {'slot', 'publication', 'connector', 'topic'}
            and all(isinstance(binding[k], str) and re.fullmatch(r'[a-z0-9_]{1,63}', binding[k])
                    for k in ('slot', 'publication'))
            and isinstance(binding['connector'], str) and re.fullmatch(r'[a-zA-Z0-9_.-]{1,255}', binding['connector'])
            and isinstance(binding['topic'], str) and re.fullmatch(r'__addp_cdc\.[a-zA-Z0-9_.-]{1,240}', binding['topic']),
            'CDC observation binding invalid')
    return binding


def cdc_native_observation(binding):
    """Exact, read-only physical probes; raw names and responses never enter the report."""
    container_id = os.environ.get('ADDP_ONLINE_CDC_CONNECT_ID', '')
    require(re.fullmatch(r'[0-9a-f]{64}', container_id), 'CDC observation Connect identity mismatch')
    try:
        inspected = subprocess.run(['docker', 'inspect', container_id], check=True,
                                   capture_output=True, text=True, timeout=10)
        container, = json.loads(inspected.stdout)
        labels = container['Config']['Labels']
        require(container['Id'] == container_id and container['State']['Running'] is True
                and labels.get('com.docker.compose.project') == 'addp-infra'
                and labels.get('com.docker.compose.service') == 'kafka-connect'
                and labels.get('com.docker.compose.project.working_dir') == str(ROOT),
                'CDC observation Connect identity mismatch')
        source = subprocess.run(['bash', 'business/scripts/online-continuous-fixture.sh', 'observe-cdc-source',
                                 binding['slot'], binding['publication']], cwd=ROOT,
                                check=True, capture_output=True, text=True, timeout=20)
        pg = json.loads(source.stdout)
        bean = 'debezium.postgres:context=streaming,server=' + binding['connector'] + ',type=connector-metrics'
        request = {'type': 'read', 'mbean': bean, 'attribute': ['Connected', 'MilliSecondsBehindSource']}
        response = subprocess.run(['docker', 'exec', '-i', container_id, 'curl', '-fsS', '--max-time', '5',
                                   '-H', 'Content-Type: application/json', '--data-binary', '@-',
                                   'http://127.0.0.1:8778/jolokia/'], input=json.dumps(request),
                                  check=True, capture_output=True, text=True, timeout=10)
        delay = json.loads(response.stdout)
    except (KeyError, OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        raise SuiteError('CDC native observation unavailable') from None
    require(isinstance(pg, dict) and pg.get('identity_matches') is True,
            'CDC native source identity mismatch')
    require(isinstance(delay, dict) and delay.get('status') == 200
            and delay.get('request', {}).get('mbean') == bean
            and isinstance(delay.get('value'), dict), 'CDC native delay identity mismatch')
    return pg, delay['value']


def cdc_sample_time(value, now):
    try:
        sample = datetime.fromisoformat(value['sampled_at'].replace('Z', '+00:00'))
        expires = datetime.fromisoformat(value['expires_at'].replace('Z', '+00:00'))
        require(sample.tzinfo is not None and expires.tzinfo is not None and sample < expires,
                'CDC observation time invalid')
    except (KeyError, TypeError, ValueError, AttributeError):
        raise SuiteError('CDC observation time invalid') from None
    return sample if sample <= now < expires else None


def cdc_task_sample(client, task_id, engine_id, generation=None, *, after=None):
    task = client.request('GET', f'{OWNER}/task-definitions/{task_id}', (200,)).payload
    require(task.get('id') == task_id and task.get('config', {}).get('source', {}).get('locator') ==
            f'addp://engine/{engine_id}/path/public/continuous_source?type=table', 'CDC task source identity mismatch')
    capture = task.get('capture') or {}
    require(capture.get('source_type') == 'postgresql' and type(capture.get('generation')) is int
            and capture['generation'] > 0 and (generation is None or capture['generation'] == generation),
            'CDC task generation mismatch')
    require(set(capture) <= {'source_type', 'generation', 'status', 'connector_status', 'source_status',
            'last_observed_at', 'stopped_at', 'source_recovery', 'source_transactions',
            'postgresql_observation', 'delay_observation', 'buffer_observation'}, 'CDC capture exposed private fields')
    require(capture.get('status') == 'running', 'CDC capture generation is not running')
    assert_safe(capture)
    allowed = {
        'postgresql_observation': {'status', 'sampled_at', 'expires_at', 'slot_active', 'wal_status',
            'retained_wal_bytes', 'unacknowledged_wal_bytes', 'safe_wal_size_bytes', 'wal_keep_unlimited', 'risk_state'},
        'delay_observation': {'status', 'sampled_at', 'expires_at', 'connected', 'delay_seconds'},
        'buffer_observation': {'status', 'sampled_at', 'expires_at', 'earliest_offset', 'latest_offset',
            'committed_offset', 'lag_offsets', 'recovery_state'},
    }
    now = datetime.now(timezone.utc)
    for field, keys in allowed.items():
        value = capture.get(field)
        if value is None:
            return None
        require(isinstance(value, dict) and set(value) == keys, 'CDC observation fields invalid')
        sample = cdc_sample_time(value, now)
        if sample is None or value['status'] != 'available' or after is not None and sample <= after:
            return None
    pg, delay, buffer = (capture[field] for field in allowed)
    if buffer['recovery_state'] == 'not_started' and buffer['committed_offset'] is None:
        return None
    require(type(pg['slot_active']) is bool and type(pg['wal_keep_unlimited']) is bool
            and pg['wal_status'] in {'reserved', 'extended', 'unreserved', 'lost'}
            and pg['risk_state'] == pg['wal_status'], 'CDC source observation invalid')
    for field in ('retained_wal_bytes', 'unacknowledged_wal_bytes', 'safe_wal_size_bytes'):
        value = pg[field]
        require(value is None or isinstance(value, str) and re.fullmatch(r'0|[1-9][0-9]*', value)
                and int(value) <= 18446744073709551615, 'CDC WAL byte distance invalid')
    require(pg['retained_wal_bytes'] is not None and pg['unacknowledged_wal_bytes'] is not None,
            'CDC WAL byte distance missing')
    require(type(delay['connected']) is bool and (delay['delay_seconds'] is None or
            type(delay['delay_seconds']) in (int, float) and math.isfinite(delay['delay_seconds'])),
            'CDC capture delay invalid')
    for field in ('earliest_offset', 'latest_offset', 'committed_offset', 'lag_offsets'):
        require(isinstance(buffer[field], str) and re.fullmatch(r'0|[1-9][0-9]*', buffer[field])
                and int(buffer[field]) <= 9223372036854775807, 'CDC buffer position invalid')
    require(buffer['recovery_state'] == 'available'
            and int(buffer['earliest_offset']) <= int(buffer['committed_offset']) <= int(buffer['latest_offset'])
            and int(buffer['latest_offset']) - int(buffer['committed_offset']) == int(buffer['lag_offsets']),
            'CDC buffer positions inconsistent')
    if not pg['slot_active'] or not delay['connected']:
        return None
    return {'task_id': task_id, 'generation': capture['generation'], 'desired_state': task['desired_state'],
            'execution_id': canonical_uuid(task['last_execution_id']), 'source': dict(pg),
            'delay': dict(delay), 'buffer': dict(buffer)}


def cdc_observation(client, task_id, tenant, engine_id, generation=None, *, after=None, event_delay=False):
    sample = cdc_task_sample(client, task_id, engine_id, generation, after=after)
    if sample is None:
        return None
    binding = cdc_capture_binding(tenant, task_id, engine_id, sample['generation'])
    pg, delay = cdc_native_observation(binding)
    require(pg.get('slot_active') is True and pg.get('wal_status') == sample['source']['wal_status']
            and pg.get('wal_keep_unlimited') is sample['source']['wal_keep_unlimited'],
            'CDC native WAL state mismatch')
    for key in ('retained_wal_bytes', 'unacknowledged_wal_bytes', 'safe_wal_size_bytes'):
        require(pg.get(key) is None or isinstance(pg[key], str) and re.fullmatch(r'0|[1-9][0-9]*', pg[key]),
                'CDC native WAL distance invalid')
    require((pg['safe_wal_size_bytes'] is None) == (sample['source']['safe_wal_size_bytes'] is None),
            'CDC native WAL headroom mismatch')
    ms = delay.get('MilliSecondsBehindSource')
    require(delay.get('Connected') is True and type(ms) in (int, float) and math.isfinite(ms) and int(ms) == ms,
            'CDC native delay invalid')
    expected = None if ms == -1 else ms / 1000
    if sample['delay']['delay_seconds'] != expected or event_delay and expected is None:
        return None  # Only read again while the existing sampling window remains open.
    observed_at = datetime.now(timezone.utc)
    if any(cdc_sample_time(sample[field], observed_at) is None for field in ('source', 'delay', 'buffer')):
        return None
    require(all(name not in json.dumps(sample) for name in binding.values()), 'CDC task exposed private resource names')
    sample['native_source_identity_verified'] = True
    sample['native_wal_state_verified'] = True
    sample['exact_connector_delay_verified'] = True
    return sample


def verify_cdc_task_monitoring(client, task_id, tenant, engine_id, execution_id, launch, mutate, phase, observe_page):
    phase('cdc_monitor_initial')
    def initial_ready():
        value = cdc_observation(client, task_id, tenant, engine_id)
        return value if value is not None and value['buffer']['lag_offsets'] == '0' else None
    initial = until(initial_ready)
    require(initial['desired_state'] == 'running' and initial['execution_id'] == execution_id
            and initial['buffer']['lag_offsets'] == '0',
            'CDC initial task progress mismatch')
    browser = {'initial': observe_page('cdc_initial', initial)}
    generation = initial['generation']
    phase('cdc_pause')
    mutate(client, f'{OWNER}/task-definitions/{task_id}/pause')
    wait_status(client, execution_id, 'cancelled')
    paused_at = datetime.now(timezone.utc)
    paused = until(lambda: cdc_observation(client, task_id, tenant, engine_id, generation, after=paused_at))
    require(paused['desired_state'] == 'paused' and paused['execution_id'] == execution_id
            and paused['buffer']['lag_offsets'] == '0', 'CDC pause did not preserve the task position')
    phase('cdc_mutation')
    require(physical('mutate-cdc'), 'CDC mutation fixture failed')
    written_at = datetime.now(timezone.utc)
    phase('cdc_buffer_growth')
    def grown():
        value = cdc_observation(client, task_id, tenant, engine_id, generation, after=written_at, event_delay=True)
        if value is None or int(value['buffer']['lag_offsets']) < 3:
            return None
        require(value['desired_state'] == 'paused' and value['execution_id'] == execution_id
                and value['buffer']['committed_offset'] == paused['buffer']['committed_offset']
                and int(value['buffer']['latest_offset']) - int(paused['buffer']['latest_offset']) == 3
                and value['buffer']['lag_offsets'] == '3', 'CDC paused backlog did not match the event batch')
        require(physical('verify-cdc-initial'), 'CDC paused target changed')
        return value
    growth = until(grown)
    browser['growth'] = observe_page('cdc_growth', growth)
    phase('cdc_monitor_resume')
    resumed_id = launch(task_id, 'resume')
    require(resumed_id != execution_id, 'CDC resume reused execution identity')
    resumed_at = datetime.now(timezone.utc)
    def caught_up(after):
        value = cdc_observation(client, task_id, tenant, engine_id, generation, after=after, event_delay=True)
        if value is None or value['buffer']['lag_offsets'] != '0':
            return None
        require(value['desired_state'] == 'running' and value['execution_id'] == resumed_id
                and value['buffer']['committed_offset'] == growth['buffer']['latest_offset'],
                'CDC resume task progress mismatch')
        require(physical('verify-cdc-mutated'), 'CDC resumed target rowset mismatch')
        return value
    resumed = until(lambda: caught_up(resumed_at))
    sustained = until(lambda: caught_up(datetime.fromisoformat(resumed['buffer']['sampled_at'].replace('Z', '+00:00'))))
    browser['resumed'] = observe_page('cdc_resumed', sustained)
    return resumed_id, {'browser': browser, 'initial_execution_id': execution_id, 'resumed_execution_id': resumed_id,
                        'generation': generation, 'event_count': 3,
                        'samples': {'initial': initial, 'paused': paused, 'growth': growth,
                                    'resumed': resumed, 'sustained': sustained}}


SCAN_TABLES = frozenset({
    'continuous_scan_sibling', 'continuous_kafka_target', 'continuous_cdc_target', 'continuous_scheduled_target',
})
SCAN_SIBLING = 'continuous_scan_sibling'


def read_scan_catalog(engine_id, tenant, *, evidence=None):
    """Read only the owned physical scan facts, independently of tenant visibility."""
    postgres_id = os.environ.get('ADDP_ONLINE_SCAN_POSTGRES_ID', '')
    require(type(tenant) is int and tenant > 1 and type(engine_id) is int and engine_id > 0
            and os.environ.get('ONLINE_SUITE') == SUITE
            and os.environ.get('ADDP_ONLINE_TEST_TENANT_ID') == str(tenant)
            and os.environ.get('ADDP_ONLINE_CONTINUOUS_PG_ENGINE_ID') == str(engine_id)
            and re.fullmatch(r'[0-9a-f]{64}', postgres_id), 'scan state requires the exact Hosted fixture identities')
    if evidence is not None:
        evidence.update(source='hosted_postgres_readonly', state='read_started')
    postgres = import_module('scripts.test.execution-retention-readonly-online')
    names = ', '.join(f"'public.{name}'" for name in sorted(SCAN_TABLES))
    query = f"""BEGIN READ ONLY;
SET LOCAL statement_timeout='3s'; SET LOCAL lock_timeout='1s';
WITH RECURSIVE fixture_items AS (
  SELECT id, tenant_id, engine_id, node_id, item_type, full_name, fingerprint,
         row_count, size_bytes, data_updated_at, scanned_at, scanned_depth,
         (SELECT jsonb_agg(field->>'name')
          FROM jsonb_array_elements(CASE
            WHEN jsonb_typeof(attributes #> '{{type_info,table,fields}}') = 'array'
            THEN attributes #> '{{type_info,table,fields}}' ELSE '[]'::jsonb END) AS field) AS fields
  FROM meta.meta_item
  WHERE tenant_id = :'tenant'::bigint AND engine_id = :'engine'::bigint
    AND full_name IN ({names}) AND deleted_at IS NULL
  ORDER BY id LIMIT {len(SCAN_TABLES) + 1}
), ancestors AS (
  SELECT n.id, n.tenant_id, n.engine_id, n.parent_node_id, n.scan_status,
         n.scanned_at, n.scanned_depth, 1 AS level
  FROM meta.meta_node n JOIN fixture_items s ON n.id = s.node_id
    AND n.tenant_id = s.tenant_id AND n.engine_id = s.engine_id
  WHERE n.deleted_at IS NULL AND s.full_name = 'public.{SCAN_SIBLING}'
  UNION ALL
  SELECT n.id, n.tenant_id, n.engine_id, n.parent_node_id, n.scan_status,
         n.scanned_at, n.scanned_depth, a.level + 1
  FROM meta.meta_node n JOIN ancestors a ON n.id = a.parent_node_id
    AND n.tenant_id = a.tenant_id AND n.engine_id = a.engine_id
  WHERE n.deleted_at IS NULL AND a.level < 128
)
SELECT jsonb_build_object(
  'items', COALESCE((SELECT jsonb_agg(to_jsonb(i)) FROM fixture_items i), '[]'::jsonb),
  'ancestors', COALESCE((SELECT jsonb_agg(to_jsonb(a) - 'level' ORDER BY a.level) FROM ancestors a), '[]'::jsonb));
COMMIT;
"""
    try:
        raw = postgres.sql(query, expected_suite=SUITE, postgres_id=postgres_id,
                           variables={'tenant': tenant, 'engine': engine_id})
        require(0 < len(raw.encode()) <= 65536, 'bounded Hosted scan state read unavailable')
        snapshot = json.loads(raw)
    except (KeyError, OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        raise SuiteError('bounded Hosted scan state read unavailable') from None
    require(isinstance(snapshot, dict) and isinstance(snapshot.get('items'), list)
            and isinstance(snapshot.get('ancestors'), list), 'Hosted scan state response is invalid')
    if evidence is not None:
        evidence.update(state='identity_check', matching_items=len(snapshot['items']))

    items = {}
    ids = set()
    for item in snapshot['items']:
        full_name = item.get('full_name') if isinstance(item, dict) else None
        name = full_name[7:] if isinstance(full_name, str) and full_name.startswith('public.') else None
        require(name in SCAN_TABLES and item.get('tenant_id') == tenant and item.get('engine_id') == engine_id
                and item.get('item_type') == 'table' and bool(item.get('fingerprint')), 'Meta target identity changed')
        item_id = API.require_positive_int(item, 'id')
        API.require_positive_int(item, 'node_id')
        require(name not in items and item_id not in ids, 'Meta target identity is duplicated')
        ids.add(item_id)
        items[name] = item
    require(SCAN_SIBLING in items, 'physical scan sibling is missing')
    boundary = scan_boundary(items[SCAN_SIBLING], snapshot['ancestors'], engine_id, tenant)
    if evidence is not None:
        evidence.update(state='identity_verified')
    return items, boundary


def require_transfer_fixture(tenant, task_id):
    require(type(tenant) is int and tenant > 1 and type(task_id) is int and task_id > 0
            and os.environ.get('ONLINE_SUITE') == SUITE
            and os.environ.get('ADDP_ONLINE_TEST_TENANT_ID') == str(tenant),
            'Transfer observation requires the exact Hosted task scope')


def kafka_delete_facts(tenant, task_id, execution_id):
    require_transfer_fixture(tenant, task_id)
    execution_id = canonical_uuid(execution_id)
    postgres_id = os.environ.get('ADDP_ONLINE_SCAN_POSTGRES_ID', '')
    require(re.fullmatch(r'[0-9a-f]{64}', postgres_id), 'Kafka cleanup requires the exact Hosted PostgreSQL')
    postgres = import_module('scripts.test.execution-retention-readonly-online')
    statement = """BEGIN READ ONLY;
SET LOCAL statement_timeout='3s'; SET LOCAL lock_timeout='1s';
SELECT jsonb_build_object(
  'task_present', EXISTS(SELECT 1 FROM transfer.transfer_tasks
    WHERE tenant_id = :'tenant'::bigint AND id = :'task'::bigint AND deleted_at IS NULL),
  'sessions', count(*), 'open', count(*) FILTER (WHERE stopped_at IS NULL),
  'selected', count(*) FILTER (WHERE execution_id = :'execution'),
  'selected_open', count(*) FILTER (WHERE execution_id = :'execution' AND stopped_at IS NULL),
  'operations', (SELECT count(*) FROM transfer.cleanup_operations
    WHERE tenant_id = :'tenant'::bigint AND task_id = :'task'::bigint))
FROM transfer.runtime_sessions WHERE tenant_id = :'tenant'::bigint AND task_id = :'task'::bigint;
COMMIT;
"""
    try:
        facts = json.loads(postgres.sql(statement, expected_suite=SUITE, postgres_id=postgres_id,
                           variables={'tenant': tenant, 'task': task_id, 'execution': execution_id}))
    except (KeyError, OSError, ValueError, RuntimeError, subprocess.SubprocessError):
        raise SuiteError('Hosted Kafka cleanup facts unavailable') from None
    require(isinstance(facts, dict) and type(facts.get('task_present')) is bool
            and all(type(facts.get(key)) is int and facts[key] >= 0
                    for key in ('sessions', 'open', 'selected', 'selected_open', 'operations')),
            'Hosted Kafka cleanup facts are invalid')
    return facts


def kafka_dlq_exists(tenant, task_id):
    require_transfer_fixture(tenant, task_id)
    FAULTS.SUPPORT.deployment_secret()
    broker_id = os.environ.get('ADDP_ONLINE_DLQ_REDPANDA_ID', '')
    require(re.fullmatch(r'[0-9a-f]{64}', broker_id) and bool(os.environ.get('INFRA_KAFKA_TRANSFER_PASSWORD')),
            'Kafka DLQ observation requires the exact Hosted broker and Transfer identity')
    inspected = subprocess.run(['docker', 'inspect', 'addp-redpanda'], check=True,
                               capture_output=True, text=True, timeout=10)
    broker, = json.loads(inspected.stdout)
    labels = broker['Config']['Labels']
    require(broker['Id'] == broker_id and broker['State']['Running']
            and labels.get('com.docker.compose.project') == 'addp-infra'
            and labels.get('com.docker.compose.service') == 'redpanda'
            and labels.get('com.docker.compose.project.working_dir') == str(ROOT),
            'refusing Kafka outside this exclusive deployment')
    result = subprocess.run(['docker', 'exec', '-e', 'INFRA_KAFKA_TRANSFER_PASSWORD', broker_id, 'sh', '-c',
        'exec rpk topic list --format json -X brokers=redpanda:29092 -X user=transfer '
        '-X "pass=$INFRA_KAFKA_TRANSFER_PASSWORD" -X sasl.mechanism=SCRAM-SHA-256'],
        capture_output=True, text=True, check=True, timeout=20)
    require(len(result.stdout) <= 65536, 'Hosted DLQ observation exceeds its bound')
    topics = json.loads(result.stdout)
    require(type(topics) is list and len(topics) < 1000
            and all(type(item) is dict and isinstance(item.get('name'), str) for item in topics),
            'Hosted DLQ listing is invalid')
    return f'__addp_dlq.{tenant}.{task_id}' in {item['name'] for item in topics}


def verify_kafka_hard_delete(client, tenant, task_id, old_execution):
    path = f'{OWNER}/task-definitions/{task_id}'
    step, delete_status = 'pre_delete_facts', None
    try:
        def stopped_except_old():
            facts = kafka_delete_facts(tenant, task_id, old_execution)
            require(facts['task_present'] and facts['operations'] == 0, 'hard-exit task is missing or already has cleanup operations')
            require(facts['selected'] == facts['selected_open'] == 1, 'original Kafka invocation must lack an exit confirmation')
            return facts if facts['sessions'] >= 2 and facts['open'] == 1 else None

        before = until(stopped_except_old)
        # Only this DELETE must cover the Owner's existing 45-second stop window.
        deletion_client = API.GatewayClient(client.base_url, client.token, 60)
        step = 'delete_request'
        rejection = deletion_client.request('DELETE', path, (502, 503))
        delete_status = rejection.status
        step = 'delete_response'
        assert_safe(rejection.payload)
        step = 'definition_read'
        current = client.request('GET', path, (200,)).payload
        step = 'definition_verify'
        require(current.get('id') == task_id and current.get('desired_state') == 'stopped',
                'hard-exit deletion did not preserve the stopped task')
        API.assert_tenant(current, tenant, 'hard-exit Kafka task')
        step = 'post_delete_facts'
        after = kafka_delete_facts(tenant, task_id, old_execution)
        step = 'post_delete_verify'
        require(after == before, 'hard-exit deletion changed sessions or registered an external operation')
        return {'task_id': task_id, 'execution_id': old_execution, 'delete_status': rejection.status,
                'original_invocation_unconfirmed': True, 'task_retained': True, 'no_delete_dispatch': True}
    except Exception as error:
        raise KafkaHardDeleteFailure(step, error, delete_status) from error


def scan_timestamp(item):
    try:
        stamp = datetime.fromisoformat(item['scanned_at'].replace('Z', '+00:00'))
        if stamp.tzinfo is not None:
            return stamp
    except (KeyError, ValueError, TypeError, AttributeError):
        pass
    raise SuiteError('scan timestamp is invalid')


def scan_boundary(sibling, ancestors, engine_id, tenant):
    def scan_state(item, fields):
        scan_timestamp(item)
        require(item.get('scanned_depth') in ('basic', 'deep'), 'scan boundary requires a completed baseline scan depth')
        return {key: item.get(key) for key in fields.split()}

    require(2 <= len(ancestors) <= 128, 'scan baseline must include schema and root')
    parent_id = sibling['node_id']
    seen = set()
    nodes = []
    for node in ancestors:
        require(isinstance(node, dict) and type(parent_id) is int and parent_id > 0
                and parent_id not in seen and node.get('id') == parent_id
                and node.get('tenant_id') == tenant and node.get('engine_id') == engine_id,
                'scan boundary ancestor identity changed')
        require(node.get('scan_status') in ('pending', 'running', 'completed', 'failed'),
                'Hosted scan ancestor has an invalid scan status')
        nodes.append(scan_state(node, 'id tenant_id engine_id parent_node_id scan_status scanned_at scanned_depth'))
        seen.add(parent_id)
        parent_id = node.get('parent_node_id')
    require(parent_id is None, 'Hosted scan ancestor chain is incomplete')
    return {'sibling': scan_state(sibling, 'id tenant_id engine_id node_id item_type full_name fingerprint row_count size_bytes data_updated_at scanned_at scanned_depth'),
            'ancestors': nodes}


def read_unchanged_scan_catalog(engine_id, tenant, boundary):
    items, observed = read_scan_catalog(engine_id, tenant)
    require(observed == boundary, 'automatic target scan changed sibling facts or ancestor scan state')
    return items


def wait_target_scan(engine_id, tenant, name, expected_fields, boundary):
    sibling = boundary['sibling']
    after = max(scan_timestamp(item) for item in [sibling, *boundary['ancestors']])
    def scanned():
        # The target and outside facts come from the same PostgreSQL snapshot.
        item = read_unchanged_scan_catalog(engine_id, tenant, boundary).get(name)
        if item is None:
            return None
        require(item['node_id'] == sibling['node_id'] and item['id'] != sibling['id'], 'Meta target parent or identity changed')
        fields = item.get('fields')
        if (item.get('scanned_depth') != 'deep' or not item.get('scanned_at') or not isinstance(fields, list)
                or len(fields) != len(expected_fields) or not all(isinstance(field, str) for field in fields)
                or set(fields) != expected_fields or scan_timestamp(item) <= after):
            return None
        return {'target_locator': f'addp://engine/{engine_id}/path/public/{name}?type=table',
                'item_id': item['id'], 'scanned_at': item['scanned_at'], 'fields': sorted(fields)}
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
                              initial_scan_id, initial_reader, initial_principal, initiator_id, approver_id):
    require(int(initiator_id) > 0 and int(approver_id) > 0 and int(initiator_id) != int(approver_id),
            'schema approval requires two distinct User Principals')
    initial = initial_reader.request('GET', f'/api/v1/meta/executions/{initial_scan_id}', (200,)).payload
    require(initial.get('triggered_by') == int(initial_principal), 'initial scan changed its preparing User')
    for reader in (initiator, client):
        reader.request('GET', f'/api/v1/meta/executions/{initial_scan_id}', (404,))
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


def invisible(client, execution_id, owner_status, monitor_status, *, cursor=0):
    for suffix in ('', '/progress', f'/events?after={cursor}&limit=2'):
        response = client.request('GET', f'{OWNER}/executions/{execution_id}{suffix}', (owner_status,))
        assert_safe(response.payload)
        require('execution_id' not in response.payload and 'items' not in response.payload, 'Owner denial leaked execution facts')
    for suffix in ('', f'/events?after={cursor}&limit=2', '/tree'):
        response = client.request('GET', f'{MONITOR}/{execution_id}{suffix}', (monitor_status,))
        assert_safe(response.payload)
        require('execution_id' not in response.payload and 'items' not in response.payload, 'Monitor denial leaked execution facts')


def hidden_monitor_total(client):
    page = client.request('GET', '/api/v1/monitor/executions?module=transfer&task_type=sync&page=1&page_size=1', (200,)).payload
    require(isinstance(page, dict) and set(page) == {'executions', 'total', 'page', 'page_size'}
            and page['executions'] == [] and type(page['total']) is int and page['total'] == 0
            and type(page['page']) is int and page['page'] == 1
            and type(page['page_size']) is int and page['page_size'] == 1,
            'Monitor list or total exposed inaccessible executions')
    assert_safe(page)
    return page['total']


def identity(client, tenant, permissions):
    summary = API.validate_user_identity(client, tenant, permissions)
    context = client.request('GET', AUTH, (200,)).payload
    binding = (context.get('principal', {}).get('id'), context.get('context', {}).get('tenant_membership_id'))
    require(all(isinstance(part, str) and part.isdecimal() and int(part) > 0 for part in binding), 'User binding is incomplete')
    summary['principal_id'], summary['membership_id'] = binding
    return summary, binding


def revoke(reader, admin, login, tenant, execution_id, mutate):
    _, binding = identity(reader, tenant, {'monitor.execution.read', 'transfer.task.read'})
    owner_pages, monitor_pages = {}, {}
    owner_facts = events(reader, f'{OWNER}/executions', execution_id, evidence=owner_pages)
    monitor_facts = events(reader, MONITOR, execution_id, evidence=monitor_pages)
    require(owner_facts == monitor_facts and owner_pages == monitor_pages
            and monitor_pages['page_count'] >= 2 and monitor_pages['event_count'] > 2,
            'revocation must follow real multi-page Owner and Monitor event reads')
    cursor = monitor_pages['first_cursor']
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
    invisible(reader, execution_id, 401, 401, cursor=cursor)
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
    invisible(fresh, execution_id, 403, 404, cursor=cursor)
    total = hidden_monitor_total(fresh)
    return {'execution_id': execution_id, 'tenant_id': str(tenant), 'principal_id': binding[0],
            'owner_pages': owner_pages, 'monitor_pages': monitor_pages,
            'saved_cursor_denied_with_old_token': True, 'saved_cursor_denied_with_fresh_token': True,
            'fresh_monitor_only_total': total}


def wait_browser_marker(control, name, marker, process):
    deadline = time.monotonic() + 100
    path = control / (name + '.json')
    while not path.exists():
        require(process.poll() is None and time.monotonic() < deadline,
                'browser did not reach its controlled observation: ' + name)
        time.sleep(.2)
    require(json.loads(path.read_text()) == marker, 'browser control identity changed')


def write_browser_marker(control, name, marker):
    temporary = control / (name + '.tmp')
    temporary.write_text(json.dumps(marker))
    temporary.replace(control / (name + '.json'))


def observe_browser_owner_outage(control, marker, process, faults):
    def observation():
        write_browser_marker(control, 'owner_stopped', marker)
        wait_browser_marker(control, 'owner_failure_observed', marker, process)
    proof = faults.stop_owner_during(observation)
    require(isinstance(proof, dict) and set(proof) == {'owner_pid', 'stopped_verified', 'resumed_verified'}
            and type(proof['owner_pid']) is int and proof['owner_pid'] > 1
            and proof['stopped_verified'] is True and proof['resumed_verified'] is True,
            'Owner fault did not prove stopped and restored process identity')
    return proof


def run_browser(phase, execution_ids, subject, run_id, secret, action=None, *, faults=None, finish=None, cdc=None):
    cdc_phase = phase in {'cdc_initial', 'cdc_growth', 'cdc_resumed'}
    require(cdc_phase == (cdc is not None), 'CDC browser task binding changed')
    if cdc_phase:
        require(isinstance(cdc, dict) and set(cdc) == {'task_id', 'engine_id', 'generation', 'desired_state', 'committed_offset', 'latest_offset', 'lag_offsets'}
                and all(type(cdc[key]) is int and cdc[key] > 0 for key in ('task_id', 'engine_id', 'generation'))
                and cdc['desired_state'] == ('paused' if phase == 'cdc_growth' else 'running')
                and all(isinstance(cdc[key], str) and re.fullmatch(r'0|[1-9][0-9]*', cdc[key]) for key in ('committed_offset', 'latest_offset', 'lag_offsets'))
                and int(cdc['latest_offset']) - int(cdc['committed_offset']) == int(cdc['lag_offsets'])
                and cdc['lag_offsets'] == ('3' if phase == 'cdc_growth' else '0'), 'CDC browser observation invalid')
    require(phase in {'live', 'recovery', 'revocation', 'owner_unavailable', 'cdc_initial', 'cdc_growth', 'cdc_resumed'}, 'unknown browser phase')
    require((action is not None) == (phase in {'live', 'revocation'}), 'browser action binding changed')
    require((faults is not None) == (phase == 'owner_unavailable'), 'browser Owner fault binding changed')
    require((finish is not None) == (phase == 'live'), 'browser terminal action binding changed')
    execution_ids = [canonical_uuid(value) for value in execution_ids]
    require(len(execution_ids) == {'recovery': 3, 'owner_unavailable': 2}.get(phase, 1)
            and len(set(execution_ids)) == len(execution_ids), 'browser execution scope changed')
    required_checks = {
        'live': ['timeline', 'automatic_committed_progress', 'cursor_and_refresh_match',
                 'same_page_terminal_convergence', 'no_samples'],
        'recovery': ['old_failure', 'recovery_source', 'record_failure_code', 'no_samples'],
        'revocation': ['previously_readable', 'old_content_cleared', 'fresh_monitor_only_denied', 'no_samples'],
        'owner_unavailable': OWNER_OUTAGE_CHECKS,
        **{name: ['task_identity', 'task_source_slot_wal', 'task_capture_delay', 'task_buffer_positions', 'execution_binding', 'no_samples'] for name in ('cdc_initial', 'cdc_growth', 'cdc_resumed')},
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
        if cdc_phase:
            environment['ADDP_CONTINUOUS_BROWSER_CDC'] = json.dumps(cdc)
        marker = {'run_id': run_id, 'phase': phase, 'execution_id': execution_ids[0]}
        # Raw Playwright output may contain login fill arguments; keep it private.
        with (control / 'output.log').open('w') as output:
            process = subprocess.Popen(['npm', 'exec', '--', 'playwright', 'test',
                         '--config=playwright.online.config.js', '--output=' + str(control / 'playwright'),
                         'e2e/online/continuous-execution-events.spec.js'],
                         cwd=ROOT / 'console/frontend', env=environment, stdout=output,
                         stderr=subprocess.STDOUT, start_new_session=True)
            try:
                if action is not None or faults is not None:
                    wait_browser_marker(control, 'ready', marker, process)
                    if faults is not None:
                        fault_proof = observe_browser_owner_outage(control, marker, process, faults)
                    else:
                        action()
                    write_browser_marker(control, 'release', marker)
                if finish is not None:
                    wait_browser_marker(control, 'live_progress_observed', marker, process)
                    finish()
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
        if faults is not None:
            result = {**result, 'owner_process': fault_proof}
            evidence.write_text(json.dumps(result, sort_keys=True) + '\n')
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


def quiesce_task_for_cleanup(client, task_id, kind, name, current, mutate):
    require(kind in {'scheduled', 'kafka', 'cdc', 'kafka-graceful'}, 'unknown cleanup task kind')
    if kind == 'scheduled':
        if current.get('enabled') is True:
            mutate(client, f'{OWNER}/task-definitions/{task_id}/pause')
    elif current.get('desired_state') != 'stopped':
        mutate(client, f'{OWNER}/task-definitions/{task_id}/stop',
               {'confirmed': True, 'confirmation_text': name} if kind == 'cdc' else None)


def failure_diagnostic(phase, error):
    if isinstance(error, KafkaHardDeleteFailure):
        return {'phase': 'kafka_hard_delete', 'reason': 'kafka_hard_delete_' + error.diagnostic['reason']}
    # Only fixed classifications enter the public receipt, never exception text,
    # response bodies, subprocess output, source values or credentials.
    reasons = {'CDC task generation mismatch': 'cdc_generation_mismatch',
               'CDC task source identity mismatch': 'cdc_source_identity_mismatch',
               'CDC native source identity mismatch': 'cdc_native_source_identity_mismatch',
               'CDC observation Connect identity mismatch': 'cdc_connect_identity_mismatch',
               'CDC native delay identity mismatch': 'cdc_delay_identity_mismatch',
               'CDC native WAL state mismatch': 'cdc_wal_state_mismatch',
               'CDC paused backlog did not match the event batch': 'cdc_paused_backlog_mismatch',
               'CDC paused target changed': 'cdc_paused_target_changed',
               'CDC resume task progress mismatch': 'cdc_resume_progress_mismatch',
               'CDC stopped generation retained current observations': 'cdc_stopped_samples_retained',
               'Owner exposed arbitrary metadata': 'owner_metadata_contract_mismatch',
               'Owner exposed invalid initial authorization status facts': 'owner_authorization_status_contract_mismatch',
               'execution reached an unexpected terminal status': 'unexpected_terminal_status',
               'continuous execution did not converge before the deadline': 'convergence_timeout',
               'scan state requires the exact Hosted fixture identities': 'source_catalog_fixture_mismatch',
               'bounded Hosted scan state read unavailable': 'source_catalog_observer_unavailable',
               'Hosted scan state response is invalid': 'source_catalog_observer_response_invalid',
               'automatic target scan changed sibling facts or ancestor scan state': 'source_catalog_scan_scope_changed',
               'physical scan sibling is missing': 'source_catalog_sibling_unavailable',
               'Meta target identity is duplicated': 'source_catalog_identity_duplicated',
               'Meta target identity changed': 'source_catalog_identity_invalid',
               'continuous task cleanup failed': 'cleanup_incomplete',
               'continuous Hosted acceptance interrupted': 'interrupted'}
    reason = reasons.get(str(error)) if isinstance(error, SuiteError) else None
    if reason is None:
        reason = ('request_or_contract_failed' if isinstance(error, SuiteError) else
                  'subprocess_failed' if isinstance(error, subprocess.SubprocessError) else
                  'io_failed' if isinstance(error, OSError) else 'unexpected_failure')
    return {'phase': phase if phase in PHASES else 'preflight', 'reason': reason}


def cleanup_owned_tasks(client, tasks, mutate, report, checkpoint, *, hard_task=None, graceful=None):
    report['cleanup'] = 'started'
    report['cleanup_tasks'] = []
    checkpoint()
    for task_id, kind, name in reversed(tasks):
        outcome = {'task_id': task_id, 'kind': kind, 'step': 'read_definition', 'result': 'failed'}
        if task_id == hard_task:
            verified = report.get('kafka_hard_exit', {}).get('task_id') == task_id
            outcome.update(step='deployment_destruction', result='retained_for_deployment' if verified else 'failed')
            report['cleanup_tasks'].append(outcome)
            checkpoint()
            continue
        verify_graceful = graceful is not None and task_id == graceful[1]
        try:
            current = client.request('GET', f'{OWNER}/task-definitions/{task_id}', (200,)).payload
            outcome['step'] = 'quiesce'
            quiesce_task_for_cleanup(client, task_id, kind, name, current, mutate)
            outcome['step'] = 'wait_terminal_history'
            until(lambda: all(item['status'] in TERMINAL for item in history(client, task_id)))
            if verify_graceful:
                tenant, _, execution_id = graceful
                outcome['step'] = 'verify_graceful_exit'
                until(lambda: kafka_delete_facts(tenant, task_id, execution_id) ==
                      {'task_present': True, 'sessions': 1, 'open': 0, 'selected': 1, 'selected_open': 0, 'operations': 0})
                require(kafka_dlq_exists(tenant, task_id), 'graceful Kafka DLQ missing before deletion')
            outcome['step'] = 'delete_definition'
            client.request('DELETE', f'{OWNER}/task-definitions/{task_id}', (200,))
            outcome['step'] = 'verify_deleted'
            client.request('GET', f'{OWNER}/task-definitions/{task_id}', (404,))
            if verify_graceful:
                outcome['step'] = 'verify_dlq_absent'
                require(not kafka_dlq_exists(tenant, task_id), 'graceful Kafka DLQ survived deletion')
                report['kafka_graceful_exit'] = {'task_id': task_id, 'execution_id': execution_id,
                    'invocation_stopped': True, 'definition_deleted': True, 'dlq_absent': True}
            outcome.update(step='verified_absent', result='passed')
        except Exception as error:
            outcome['failure'] = failure_diagnostic('cleanup', error)
        report['cleanup_tasks'].append(outcome)
        checkpoint()
    count = sum(item['result'] == 'failed' for item in report['cleanup_tasks'])
    retained = sum(item['result'] == 'retained_for_deployment' for item in report['cleanup_tasks'])
    report.update(cleanup='failed' if count else 'passed', residual_resources=count + retained,
                  expected_deployment_residuals=retained)
    checkpoint()
    return count == 0


def run_suite(client, denied, foreign, peer, reader, admin, login, tenant, run_id, pg, kafka, report, checkpoint, faults, *, preparer):
    report.update(schema_version='addp.online-suite/v1', suite=SUITE, run_id=run_id, tenant_id=str(tenant),
                  result='failed', checks=[], resources=[], executions=[], cleanup='not_started',
                  history_cleanup='pending_deployment_destruction', uncertain_mutation=False)
    tasks = []
    scenario_failed = False
    hard_task = None
    graceful = None

    def phase(name):
        require(name in PHASES, 'invalid continuous acceptance phase')
        report['phase'] = name
        checkpoint()

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
                        {'name': name, 'task_type': 'sync', 'enabled': False, 'auto_scan_metadata': kind != 'kafka-graceful',
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
        phase('source_scan')
        # Scan physical PostgreSQL facts through Meta; no fabricated catalog/task/runtime rows.
        preparing_principal, _ = REGISTRATION.source_grant_participants(preparer, client, tenant, scan_preparation=True)
        scan = mutate(preparer, '/api/v1/meta/scan/run/manual',
                      {'engine_id': pg, 'scan_depth': 'deep', 'trigger_type': 'manual', 'force': True}, (201,))
        formal_source_scan_id = canonical_uuid(scan.get('execution_id'))
        def scanned():
            item = preparer.request('GET', f'/api/v1/meta/executions/{formal_source_scan_id}', (200,)).payload
            require(item.get('triggered_by') == int(preparing_principal), 'source scan lost its preparing User')
            require(item.get('status') not in {'failed', 'cancelled', 'timeout'}, 'physical source metadata scan failed')
            return item.get('status') == 'success'
        until(scanned)
        for actor in (client, peer):
            actor.request('GET', f'/api/v1/meta/executions/{formal_source_scan_id}', (404,))
        report['source_scan_preparation'] = {'execution_id': formal_source_scan_id,
            'principal_id': preparing_principal, 'consumer_read_denied': True, 'peer_read_denied': True}
        report['checks'].append('formal_source_catalog_scan')
        phase('source_catalog')
        report['source_catalog_baseline'] = {}
        catalog, boundary = read_scan_catalog(pg, tenant, evidence=report['source_catalog_baseline'])
        API.wait_meta_scan_tick([boundary['sibling'], *boundary['ancestors']])
        require(all(f'continuous_{kind}_target' not in catalog for kind in ('kafka', 'cdc')),
                'automatic scan requires a target absent from the baseline catalog')
        report['precise_scan_scope'] = {'sibling_item_id': boundary['sibling']['id'],
            'ancestor_nodes_verified': len(boundary['ancestors']), 'scans': [], 'target_only_verified': False}
        def target_scan(kind, fields={'id', 'label'}):
            return wait_target_scan(pg, tenant, f'continuous_{kind}_target', fields, boundary)
        phase('kafka_initial')
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
        phase('kafka_visibility')
        invisible(denied, initial, 403, 404)
        invisible(foreign, initial, 404, 404)
        report['hidden_monitor_totals'] = {'monitor_only': hidden_monitor_total(denied),
                                           'foreign_tenant': hidden_monitor_total(foreign)}
        report['checks'].append('monitor_list_and_total_hide_inaccessible_executions')
        denied.request('GET', f'{OWNER}/task-definitions/{kafka_task}/executions', (403,))
        hidden = foreign.request('GET', f'{OWNER}/executions?task_id={kafka_task}', (200,)).payload
        require(hidden.get('total') == 0 and hidden.get('data') == [], 'foreign history total leaked')
        for actor in (peer, reader):
            assert_projection(actor.request('GET', f'{MONITOR}/{initial}', (200,)).payload, initial, monitor=True, tenant=tenant)
            lifecycle_events(events(actor, f'{OWNER}/executions', initial))
        report['checks'].append('owner_permissions_foreign_tenant_and_peer_history')

        def browser_progress():
            def publish():
                require(physical('produce-after-recovery'), 'browser committed progress fixture failed')
                until(lambda: physical('verify-kafka-recovered'))
                until(lambda: any(e['kind'] == 'progress' and e['counters'].get('records_written', 0) == 4
                                  for e in events(client, f'{OWNER}/executions', initial)))

            pagination = {}
            observed = events(client, MONITOR, initial, after_first=publish, evidence=pagination)
            require(pagination['page_count'] >= 2 and pagination['last_cursor'] > pagination['first_cursor'],
                    'live pagination did not advance after a real Worker commit')
            require(observed == compare(initial), 'live pagination differs from settled Owner and Monitor facts')
            report['live_event_pagination'] = pagination
            report['checks'].append('live_monitor_cursor_matches_settled_owner_events')
            checkpoint()

        def browser_finish():
            nonlocal hard_task
            phase('worker_recovery')
            hard_task = kafka_task
            report['worker_fault'] = faults.kill_worker_and_replace()
            checkpoint()

        phase('kafka_browser')
        report['browser_live'] = run_browser('live', [initial], report['consumer_identity'],
                                             run_id, faults.secret, browser_progress, finish=browser_finish)
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
        phase('kafka_record_failure')
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
        phase('owner_unavailable')
        faults.stop_owner_during(owner_unavailable)
        compare(initial, 'failed')
        report['checks'].append('owner_unavailable_fail_closed')
        phase('reader_revocation')
        def revoke_reader():
            report['cursor_revocation'] = revoke(reader, admin, login, tenant, initial, mutate)
            checkpoint()
        report['browser_revocation'] = run_browser('revocation', [initial], report['reader_identity'],
            run_id, faults.secret, revoke_reader)
        report['checks'].append('revoked_token_and_fresh_monitor_only_reader_denied')
        report['checks'].append('real_event_pages_saved_cursor_and_revoked_list_total')
        phase('kafka_hard_delete')
        report['kafka_hard_exit'] = verify_kafka_hard_delete(client, tenant, kafka_task, initial)
        report['checks'].append('hard_exit_delete_refused_without_dlq_dispatch')

        phase('kafka_graceful')
        graceful_task = create('kafka-graceful')
        graceful_execution = launch(graceful_task)
        until(lambda: kafka_dlq_exists(tenant, graceful_task))
        until(lambda: any(event['kind'] == 'progress' and event['counters'].get('records_written', 0) > 0
                          for event in events(client, f'{OWNER}/executions', graceful_execution)))
        mutate(client, f'{OWNER}/task-definitions/{graceful_task}/stop')
        wait_status(client, graceful_execution, 'cancelled')
        graceful = (tenant, graceful_task, graceful_execution)

        phase('cdc_initial')
        cdc_task = create('cdc')
        cdc = launch(cdc_task)
        until(lambda: physical('verify-cdc-initial'))
        cdc_scan = target_scan('cdc')
        report['precise_scan_scope']['scans'].append({'phase': 'cdc_snapshot_completed', **cdc_scan})
        phase('cdc_owner_outage')
        report['browser_owner_unavailable'] = run_browser('owner_unavailable', [initial, cdc],
            report['consumer_identity'], run_id, faults.secret, faults=faults)
        compare(initial, 'failed')
        require(detail(client, cdc).get('status') == 'running', 'Owner outage stopped the continuous execution')
        compare(cdc)
        report['checks'].append('real_owner_outage_browser_clears_and_recovers_without_grant_cache')
        original_cdc = cdc
        def observe_cdc_page(browser_phase, sample):
            context = {key: sample[key] for key in ('task_id', 'generation', 'desired_state')}
            context.update(engine_id=pg, **{key: sample['buffer'][key] for key in ('committed_offset', 'latest_offset', 'lag_offsets')})
            return run_browser(browser_phase, [sample['execution_id']], report['consumer_identity'], run_id, faults.secret, cdc=context)
        cdc, report['cdc_task_monitoring'] = verify_cdc_task_monitoring(
            client, cdc_task, tenant, pg, original_cdc, launch, mutate, phase, observe_cdc_page)
        compare(original_cdc, 'cancelled')
        until(lambda: any(e['kind'] == 'progress' and e['counters'].get('records_written', 0) >= 3
                          for e in events(client, f'{OWNER}/executions', cdc)))
        report['checks'].append('cdc_task_slot_wal_delay_and_paused_backlog_resume')
        report['checks'].append('cdc_task_monitoring_browser_initial_pause_resume')
        require(target_scan('cdc') == cdc_scan, 'ordinary CDC events repeated the initial target scan')
        require(target_scan('kafka') == kafka_scan, 'CDC scans changed the Kafka sibling target')
        require(physical('alter-cdc'), 'CDC additive schema fixture failed')
        blocked = wait_status(client, cdc, 'failed')
        require(blocked.get('error_details', {}).get('code') == 'schema_change_blocked', 'CDC schema drift did not block')
        compare(cdc, 'failed')
        read_unchanged_scan_catalog(pg, tenant, boundary)
        API.wait_meta_scan_tick([cdc_scan])
        initiating_user = report['consumer_identity']['principal_id']
        approving_user = report['peer_identity']['principal_id']
        phase('cdc_approval')
        path, approval, scan_id = apply_cdc_schema_approval(peer, cdc_task, cdc, mutate, initiator=client,
            initial_scan_id=formal_source_scan_id, initial_reader=preparer, initial_principal=preparing_principal,
            initiator_id=initiating_user, approver_id=approving_user)
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
        phase('cdc_resume')
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
        phase('cdc_risk')
        verify_cdc_risk_closure(client, tenant, cdc_task, pg, cdc_resumed, name, run_id,
                                mutate, launch, report, checkpoint)
        report['checks'].append('cdc_loss_alert_trigger_unknown_ack_close_signed_delivery_and_generation_rebuild')
        phase('cdc_stop')
        wait_status(client, cdc_resumed, 'cancelled')
        compare(cdc_resumed, 'cancelled')
        require(physical('verify-clean'), 'CDC capture resources survived confirmed stop')
        stopped_capture = client.request('GET', f'{OWNER}/task-definitions/{cdc_task}', (200,)).payload.get('capture') or {}
        require(stopped_capture.get('status') == 'stopped' and all(field in stopped_capture and stopped_capture[field] is None
                for field in ('postgresql_observation', 'delay_observation', 'buffer_observation')),
                'CDC stopped generation retained current observations')
        report['cdc_task_monitoring']['stopped_current_samples_cleared'] = True
        report['checks'].append('cdc_snapshot_update_delete_insert_safe_events_and_confirmed_cleanup')
        # The existing Transfer deployment also runs its bounded worker. Exercise
        # a real owner timer, then pause before the next tick while this run drains.
        require('continuous_scheduled_target' not in read_unchanged_scan_catalog(pg, tenant, boundary),
                'scheduled target must be absent from the baseline catalog')
        phase('scheduled_create')
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
        phase('scheduled_admission')
        pending = until(scheduled_run)
        scheduled_id = canonical_uuid(pending.get('execution_id'))
        report['executions'].append(scheduled_id)
        checkpoint()
        phase('scheduled_pause')
        mutate(client, f'{OWNER}/task-definitions/{task_id}/pause')
        phase('scheduled_completion')
        execution = wait_status(client, scheduled_id, 'success')
        scan_feedback = execution.get('metadata', {}).get('metadata_scan', {})
        require(scan_feedback.get('status') == 'submitted', 'scheduled write did not submit its target scan')
        scheduled_scan_id = canonical_uuid(scan_feedback.get('execution_id'))
        scheduled_basic_id = canonical_uuid(scan_feedback.get('basic_execution_id'))
        require(scheduled_basic_id not in {scheduled_id, scheduled_scan_id}, 'scheduled scan stages reused an execution identity')
        phase('scheduled_scan')
        until(lambda: assert_scheduled_scan_origin(client, peer, execution, scheduled_scan_id, initiating_user))
        require(assert_scheduled_scan_origin(client, peer, execution, scheduled_basic_id, initiating_user),
                'scheduled deep completed without its basic identification')
        phase('scheduled_result')
        precise = wait_target_scan(pg, tenant, 'continuous_scheduled_target', {'id', 'label'}, boundary)
        require(physical('verify-scheduled'), 'scheduled transfer did not commit the expected physical rowset')
        paused = client.request('GET', f'{OWNER}/task-definitions/{task_id}', (200,)).payload
        require(paused.get('enabled') is False and not paused.get('next_run_at'), 'pause left the schedule armed')
        read_unchanged_scan_catalog(pg, tenant, boundary)
        require(target_scan('cdc', {'id', 'label', 'scan_extra'}) == migrated
                and target_scan('kafka') == kafka_scan, 'scheduled scan changed existing continuous targets')
        report['precise_scan_scope']['scans'].append({'phase': 'bounded_scheduled',
            'metadata_scan_execution_id': scheduled_scan_id, **precise})
        report['scheduled_scan_ownership'] = {'execution_id': scheduled_id, 'scan_execution_id': scheduled_scan_id,
            'basic_scan_execution_id': scheduled_basic_id, 'basic_and_deep_distinct_and_completed': True,
            'principal_id': initiating_user, 'trigger_type': 'scheduled', 'parent_and_user_verified': True,
            'parent_read_verified': True, 'peer_read_denied': True, 'physical_rowset_verified': True, 'schedule_paused': True}
        report['checks'].append('bounded_owner_schedule_current_subject_precise_scan_and_read_isolation')

    except Exception as error:
        scenario_failed = True
        if isinstance(error, KafkaHardDeleteFailure):
            report['kafka_hard_exit_diagnostic'] = error.diagnostic
        report['failure'] = {**failure_diagnostic(report['phase'], error),
                             'uncertain_mutation': report['uncertain_mutation']}
        checkpoint()
        raise
    else:
        phase('cleanup')
    finally:
        previously_uncertain = report['uncertain_mutation']
        clean = cleanup_owned_tasks(client, tasks, mutate, report, checkpoint, hard_task=hard_task, graceful=graceful)
        report['uncertain_mutation'] |= previously_uncertain
        if not clean and not scenario_failed:
            report['failure'] = failure_diagnostic('cleanup', SuiteError('continuous task cleanup failed'))
            checkpoint()
            raise SuiteError('continuous task cleanup failed')
    phase('deleted_history')
    compare(initial, 'failed')
    graceful_execution = graceful[2]
    compare(graceful_execution, 'cancelled')
    for execution_id in (initial, graceful_execution):
        assert_projection(peer.request('GET', f'{MONITOR}/{execution_id}', (200,)).payload,
                          execution_id, monitor=True, tenant=tenant)
        invisible(denied, execution_id, 403, 404)
    report['kafka_graceful_exit']['authorized_history_readable'] = True
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
        preparation_token = os.environ['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN']
        require(bool(preparation_token) and preparation_token not in tokens, 'a distinct preparing User token is required')
        preparer = API.GatewayClient(gateway, preparation_token, timeout)
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
        faults.wait_protection_ready(tenant)
        report['protection_projection_ready'] = True
        run_suite(client, denied, foreign, peer, reader, admin, API.GatewayClient(gateway, '', timeout),
                  tenant, run_id, pg, kafka, report, checkpoint, faults, preparer=preparer)
    except Exception as error:
        report['result'] = 'failed'
        report.setdefault('failure', failure_diagnostic(report.get('phase', 'preflight'), error))
        failure = report['failure']
        print('Continuous Hosted suite failed: ' + failure['phase'] + '/' + failure['reason'], file=sys.stderr)
        return 1
    finally:
        signal.alarm(0)
        checkpoint()
    print(json.dumps(report, sort_keys=True))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
