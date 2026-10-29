"""PG-only real System/Meta/Manager source authorization lifecycle."""

from __future__ import annotations

import json
import math
import os
import re
import select
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import uuid
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timedelta, timezone
from importlib import import_module
from pathlib import Path
from urllib.parse import urlencode

API = import_module('scripts.utils.online-api')
PREFLIGHT = import_module('scripts.test.online-preflight')
REGISTRATION = import_module('scripts.test.online-engine-registration')
SuiteError = API.SuiteError
SUITE = 'pg-source-authorization'
READER_PERMISSIONS = {'manager.data_item.read', 'meta.catalog.read', 'meta.scan_task.execute', 'meta.scan_task.read'}
READER_PERMISSIONS |= {'transfer.task.create', 'transfer.task.read', 'transfer.task.execute',
                       'transfer.task.delete', 'system.execution_authorization.create'}
OPERATOR_PERMISSIONS = REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS | {
    'system.engine_access_grant.revoke', 'system.engine_access_deny.create', 'system.engine_access_deny.release',
    'catalog.entry.read', 'catalog.inventory.read', 'system.engine_access_fulfillment.create',
    'system.engine.read', 'system.engine_catalog.read', 'system.engine_access_direct_permission.read',
    'meta.scan_task.create', 'meta.scan_task.update', 'meta.scan_task.delete',
    'system.meta_collection_qualification.read', 'system.meta_collection_qualification.approve',
    'system.meta_collection_qualification.revoke',
    'iam.tenant_membership.read', 'iam.department.read', 'iam.project_group.read'}
CONFIRMER_PERMISSIONS = {'catalog.entry.read', 'catalog.entry.update', 'catalog.inventory.read',
                        'catalog.sharing_decision.create'}
ORGANIZATION_PERMISSIONS = {'iam.department_membership.read', 'iam.department_membership.close',
                            'iam.project_group_membership.read', 'iam.project_group_membership.close'}
BROWSER_CHECKS = ['department_exit_silent_refresh', 'project_exit_silent_refresh',
                  'same_iframe_without_relogin', 'denied_preview_clears_old_rows']
WORKSPACE_CHECKS = ['direct_permission_tab_read_only_filters', 'exclusive_workspace_tabs', 'integrated_create_picker_without_write',
                    'table_text_and_tree_filters', 'recipient_type_and_account_filters',
                    'browser_direct_permission_cancel_zero_write', 'browser_direct_permission_conflict_requires_review',
                    'browser_direct_permission_close_preserves_grant',
                    'browser_revocation_updates_current_and_history', 'browser_revocation_denies_manager_read']
PRODUCTION_CHECKS = ['new_pg_output_initial_grant_issued', 'new_pg_output_basic_only_identification',
                     'new_pg_output_manager_readable', 'output_survives_source_withdrawal',
                     'output_withdrawal_denies_read']
COLLECTION_CHECKS = ['meta_collection_unapproved_denied', 'meta_collection_approved_repeat',
                     'meta_collection_copied_task_denied', 'meta_collection_changed_configuration_denied',
                     'meta_collection_revoked_denied', 'meta_collection_source_rules_unchanged']
COLLECTION_BROWSER_CHECKS = ['collection_workspace_read_only_navigation', 'collection_browser_exact_task_facts',
    'collection_browser_same_command_recovery', 'collection_browser_record_reload',
    'collection_browser_revoke_and_history', 'collection_browser_worker_boundary']
COLLECTION_EXPIRY_CHECKS = ['meta_collection_before_expiry_readable', 'meta_collection_natural_expiry_denied',
                          'meta_collection_expired_replay_preserves_history']
COLLECTION_SCHEDULED_CHECKS = ['meta_collection_scheduled_unapproved_denied', 'meta_collection_scheduled_approved_repeat',
                             'meta_collection_scheduled_revoked_denied', 'meta_collection_scheduled_source_rules_unchanged']
COLLECTION_INFLIGHT_CHECKS = ['meta_collection_inflight_revoke_stops_next_catalog',
                             'meta_collection_inflight_retry_has_no_source_io', 'meta_collection_inflight_fixture_cleanup']
COLLECTION_INFLIGHT_EXPIRY_CHECKS = ['meta_collection_inflight_expiry_stops_next_catalog',
    'meta_collection_inflight_expired_replay_has_no_source_io', 'meta_collection_inflight_expiry_fixture_cleanup']
READ_CHECK = '/api/v1/system/engine-access/read-checks/manager-preview'
LATENCY_ROUNDS, LATENCY_SAMPLES, LATENCY_WARMUPS = 3, 500, 10
LOAD_ROUNDS, LOAD_SAMPLES, LOAD_WARMUPS = 3, 100, 10

# Deliberately no query text/parameters, credentials, relation data or query plans
# in the published evidence. Counters belong to the whole disposable instance.
AUDIT_ACTIVITY_SQL = '''SELECT json_build_object(
 'at', clock_timestamp(), 'connections', (SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()),
 'activity', COALESCE((SELECT json_agg(row_to_json(s)) FROM (
 SELECT pid, backend_start, xact_start, query_start, state_change, state, wait_event_type, wait_event,
 pg_blocking_pids(pid) AS blocking_pids,
 CASE WHEN ltrim(query) ILIKE 'INSERT INTO "system"."audit_logs"%' THEN 'audit_insert'
      WHEN lower(btrim(query)) IN ('commit', 'commit;', 'rollback', 'rollback;') THEN 'transaction_end'
      ELSE 'other' END AS query_class
 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()
 AND backend_type='client backend' AND state<>'idle' ORDER BY pid LIMIT 256) s), '[]'::json),
 'wal', (SELECT row_to_json(w) FROM pg_stat_wal w),
 'bgwriter', (SELECT row_to_json(b) FROM pg_stat_bgwriter b),
 'database', (SELECT json_build_object('commits',xact_commit,'rollbacks',xact_rollback,
 'blocks_read',blks_read,'blocks_hit',blks_hit,'read_ms',blk_read_time,'write_ms',blk_write_time,
 'deadlocks',deadlocks,'stats_reset',stats_reset) FROM pg_stat_database WHERE datname=current_database()));'''


def disk_observation():
    disks = {}
    for line in Path('/proc/diskstats').read_text().splitlines():
        fields = line.split()
        if len(fields) >= 14 and not fields[2].startswith(('loop', 'ram', 'fd', 'sr')):
            disks[fields[0] + ':' + fields[1]] = {'device': fields[2], 'counters': list(map(int, fields[3:]))}
    require(bool(disks) and len(disks) <= 128, 'disk observation missing or exceeds budget')
    return {'at': datetime.now(timezone.utc).isoformat(), 'disks': disks,
            'io_pressure': Path('/proc/pressure/io').read_text().strip(),
            'cpu_pressure': Path('/proc/pressure/cpu').read_text().strip(),
            'load_average': Path('/proc/loadavg').read_text().strip(),
            'cpu': Path('/proc/stat').read_text().splitlines()[0]}


class AuditLatencyObservation:
    """One bounded, read-only psql session; never touches a personal deployment."""
    def __init__(self, evidence, principal, *, phase='serial'):
        self.evidence = evidence
        require(re.fullmatch(r'[1-9][0-9]*', principal) is not None, 'invalid audit observation principal')
        require(phase in {'serial', 'load'}, 'invalid audit observation phase')
        self.file = 'pg-audit-waits.jsonl' if phase == 'serial' else 'pg-load-waits.jsonl'
        self.audit_limit = 1531 if phase == 'serial' else LOAD_ROUNDS * (LOAD_SAMPLES + LOAD_WARMUPS) * 8 + 1
        self.principal = principal
        self.process = self.thread = self.stream = None
        self.stop = threading.Event()
        self.failure = None
        self.buffer = b''
        self.samples, self.bytes, self.query_max_ms, self.max_gap_ms = 0, 0, 0, 0
        self.last_sample_ns = None
        self.started = datetime.now(timezone.utc).isoformat()
        evidence.update(result='failed', target_interval_ms=50, samples=0, connection_closed=False,
                        scope='disposable_infra_instance_and_host_disks', audit_rows=[],
                        note='sampled waits are not exhaustive; cumulative counters include other sessions')

    def query(self, sql):
        started = time.monotonic()
        self.process.stdin.write((sql + '\n').encode())
        self.process.stdin.flush()
        while b'\n' not in self.buffer:
            remaining = 3 - (time.monotonic() - started)
            require(remaining > 0, 'audit observation query timed out')
            ready, _, _ = select.select([self.process.stdout], [], [], remaining)
            require(bool(ready), 'audit observation query timed out')
            chunk = os.read(self.process.stdout.fileno(), 65536)
            require(bool(chunk), 'audit observation connection closed')
            self.buffer += chunk
            require(len(self.buffer) <= 512 * 1024, 'audit observation response exceeds budget')
        line, self.buffer = self.buffer.split(b'\n', 1)
        self.query_max_ms = max(self.query_max_ms, (time.monotonic() - started) * 1000)
        return json.loads(line)

    def sample(self):
        require(self.samples < 2000, 'audit observation sample budget exceeded')
        started = time.monotonic_ns()
        snapshot = {'started_unix_ns': time.time_ns(), 'db': self.query(AUDIT_ACTIVITY_SQL), 'host': disk_observation()}
        snapshot['finished_unix_ns'] = time.time_ns()
        snapshot['observer_ms'] = (time.monotonic_ns() - started) / 1000000
        require(type(snapshot['db'].get('connections')) is int and 0 < snapshot['db']['connections'] <= 257
                and isinstance(snapshot['db'].get('activity'), list), 'audit activity response missing or truncated')
        if self.last_sample_ns is not None:
            self.max_gap_ms = max(self.max_gap_ms, (started - self.last_sample_ns) / 1000000)
        self.last_sample_ns = started
        encoded = json.dumps(snapshot, separators=(',', ':')) + '\n'
        self.bytes += len(encoded.encode())
        require(self.bytes <= 32 * 1024 * 1024, 'audit observation byte budget exceeded')
        self.stream.write(encoded)
        self.stream.flush()
        self.samples += 1

    def healthy(self):
        require(self.failure is None and self.process.poll() is None, 'audit observation failed')

    def collect(self):
        started = time.monotonic()
        try:
            while not self.stop.wait(.05):
                require(time.monotonic() - started < 95, 'audit observation time budget exceeded')
                self.sample()
        except Exception:
            # psql stderr or exception text can include connection credentials.
            self.failure = 'audit_observation_failed'
            self.stop.set()

    def __enter__(self):
        try:
            require(all(os.environ.get(key) == value for key, value in {
                'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_TEST': '1',
                'ADDP_ONLINE_HOSTED': '1', 'ADDP_ONLINE_HOST': '1', 'POSTGRES_DB': 'addp_online'}.items())
                and sys.platform == 'linux' and not (Path(__file__).resolve().parents[2] / '.env').exists(),
                'audit observation requires a fresh Hosted deployment')
            inspected = subprocess.run(['docker', 'inspect', '--format', '{{json .}}', 'addp-postgres'],
                                       capture_output=True, text=True, timeout=5, check=True)
            container = json.loads(inspected.stdout)
            labels = container['Config']['Labels']
            configured = dict(entry.split('=', 1) for entry in container['Config']['Env'] if '=' in entry)
            require(labels.get('com.docker.compose.project') == 'addp-infra'
                    and labels.get('com.docker.compose.service') == 'postgres' and container['State']['Running']
                    and labels.get('com.docker.compose.project.working_dir') == str(Path(__file__).resolve().parents[2])
                    and configured.get('POSTGRES_DB') == 'addp_online'
                    and bool(configured.get('POSTGRES_PASSWORD'))
                    and all(configured.get(key) == os.environ.get(key) for key in ('POSTGRES_USER', 'POSTGRES_PASSWORD')),
                    'audit observation container ownership differs')
            identifier = container['Id']
            require(re.fullmatch('[a-f0-9]{64}', identifier) is not None, 'invalid observation container identity')
            directory = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']).resolve()
            require(directory.is_absolute() and not directory.is_relative_to(Path(__file__).resolve().parents[2]),
                    'audit observation evidence must be outside checkout')
            self.stream = (directory / self.file).open('x')
            environment = dict(os.environ, PGPASSWORD=os.environ['POSTGRES_PASSWORD'], PGCONNECT_TIMEOUT='3',
                PGOPTIONS='-c default_transaction_read_only=on -c statement_timeout=2000 -c lock_timeout=100 -c application_name=addp_pg_latency_observer')
            self.process = subprocess.Popen(['docker', 'exec', '-i', '-e', 'PGPASSWORD', '-e', 'PGOPTIONS',
                '-e', 'PGCONNECT_TIMEOUT', identifier, 'psql', '-XqAt', '-P', 'pager=off', '-v', 'ON_ERROR_STOP=1',
                '-h', '127.0.0.1', '-U', os.environ['POSTGRES_USER'], '-d', 'addp_online'],
                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, env=environment)
            settings = self.query("""SELECT json_build_object('database',current_database(), 'pid',pg_backend_pid(),
                'server_started_at',pg_postmaster_start_time(), 'server_version',current_setting('server_version'),
                'read_only',current_setting('default_transaction_read_only'), 'fsync',current_setting('fsync'),
                'synchronous_commit',current_setting('synchronous_commit'),'wal_sync_method',current_setting('wal_sync_method'),
                'track_io_timing',current_setting('track_io_timing'),'track_wal_io_timing',current_setting('track_wal_io_timing'));""")
            require(settings['database'] == 'addp_online' and settings['read_only'] == 'on'
                    and settings['track_io_timing'] == settings['track_wal_io_timing'] == 'on',
                    'audit observation settings were not effective')
            self.evidence.update(settings=settings, container_id=identifier, file=self.file)
            mounts = [mount for mount in container['Mounts'] if mount['Destination'] == '/var/lib/postgresql/data']
            require(len(mounts) == 1 and mounts[0]['Type'] == 'volume'
                    and mounts[0]['Name'] == 'addp-infra_postgres_data', 'audit observation volume ownership differs')
            candidates = []
            for line in Path('/proc/self/mountinfo').read_text().splitlines():
                left, right = line.split(' - ', 1)
                fields = left.split()
                if Path(mounts[0]['Source']).is_relative_to(fields[4]):
                    candidates.append((len(fields[4]), fields[2], right.split()[0]))
            require(bool(candidates), 'PostgreSQL volume filesystem could not be mapped')
            _, device, filesystem = max(candidates)
            self.evidence.update(volume=mounts[0]['Name'], volume_host_device=device, volume_host_filesystem=filesystem)
            self.sample()
            self.thread = threading.Thread(target=self.collect, name='pg-audit-observation', daemon=True)
            self.thread.start()
            return self
        except Exception:
            self.close()
            raise SuiteError('audit observation initialization failed') from None

    def close(self):
        self.stop.set()
        try:
            if self.thread is not None:
                self.thread.join(4)
                require(not self.thread.is_alive(), 'audit observation thread did not stop')
            if self.process is not None:
                if self.process.poll() is None:
                    self.process.stdin.write(b'\\q\n')
                    self.process.stdin.flush()
                self.process.wait(timeout=4)
                require(self.process.returncode == 0, 'audit observation process failed')
            self.evidence['connection_closed'] = True
        finally:
            if self.process is not None:
                if self.process.poll() is None:
                    self.process.kill()
                    self.process.wait(timeout=4)
                self.process.stdin.close()
                self.process.stdout.close()
            if self.stream is not None:
                self.stream.close()

    def __exit__(self, kind, value, traceback):
        try:
            self.stop.set()
            self.thread.join(4)
            require(not self.thread.is_alive() and self.failure is None, 'audit observation failed')
            self.sample()
            # Only metadata for this fixture principal, exact route and window.
            self.evidence['audit_rows'] = self.query(f"""SELECT COALESCE(json_agg(row_to_json(s)), '[]'::json) FROM (
                SELECT id,request_id,created_at,(details->>'duration_ms')::bigint AS pre_audit_ms FROM system.audit_logs
                WHERE principal_id={self.principal} AND event_name='http.request.completed'
                AND http_method='POST' AND resource_path='{READ_CHECK}' AND created_at>='{self.started}'::timestamptz
                ORDER BY id LIMIT {self.audit_limit}) s;""")
            self.evidence['result'] = 'passed' if kind is None else 'failed'
        finally:
            self.evidence.update(samples=self.samples, bytes=self.bytes, max_observer_query_ms=self.query_max_ms,
                                 max_sample_gap_ms=self.max_gap_ms)
            try:
                self.close()
            except Exception:
                self.evidence['result'] = 'failed'
                raise
        return False


def bind_audit_requests(requests, audits):
    require(len(requests) == len(audits) and len({row['request_id'] for row in audits}) == len(audits),
            'audit observation request count or identity differs')
    for request, audit in zip(requests, audits):
        stamp = datetime.fromisoformat(audit['created_at'].replace('Z', '+00:00'))
        require(stamp.tzinfo is not None and re.fullmatch('[a-f0-9-]{36}', audit['request_id']) is not None
                and request['started_unix_ns'] - 1000000 <= stamp.timestamp() * 1000000000
                    <= request['finished_unix_ns'] + 1000000,
                'audit observation request time window differs')
        request.update(request_id=audit['request_id'], audit_id=audit['id'], pre_audit_ms=audit['pre_audit_ms'])


def verify_load_audit_windows(result, audits):
    expected = sum(row['http_attempted_count'] for cell in result['cells'] for row in cell['rounds'])
    require(len(audits) == expected and len({row['request_id'] for row in audits}) == expected
            and len({row['id'] for row in audits}) == expected,
            'read load audit count or identity differs')
    matched = set()
    for cell in result['cells']:
        requests = [row for round_ in cell['rounds'] for row in round_['requests']]
        start = min(row['started_unix_ns'] for row in requests) - 1000000
        end = max(row['finished_unix_ns'] for row in requests) + 1000000
        selected = []
        for audit in audits:
            stamp = datetime.fromisoformat(audit['created_at'].replace('Z', '+00:00'))
            require(stamp.tzinfo is not None and re.fullmatch('[a-f0-9-]{36}', audit['request_id']) is not None,
                    'invalid read load audit metadata')
            if start <= stamp.timestamp() * 1000000000 <= end:
                selected.append(audit['id'])
        require(len(selected) == sum(row['http_attempted_count'] for row in cell['rounds'])
                and not matched.intersection(selected), 'read load audit cell window differs')
        matched.update(selected)
    require(len(matched) == expected, 'read load audit outside measurement windows')
    result['audit_correlation'] = 'cell_time_windows_only_not_individual_requests'


def require(condition, message):
    if not condition:
        raise SuiteError(message)


def latency_statistics(samples):
    require(bool(samples) and all(type(value) in (int, float) and math.isfinite(value) and value >= 0
                                  for value in samples), 'latency samples must be finite nonnegative milliseconds')
    ordered = sorted(samples)
    return {'sample_count': len(samples), 'p50_ms': ordered[math.ceil(len(samples) * .50) - 1],
            'p95_ms': ordered[math.ceil(len(samples) * .95) - 1], 'min_ms': ordered[0], 'max_ms': ordered[-1]}


def tenant_functional_assignments(current, tenant):
    roles = (current.get('authorization') or {}).get('role_assignments')
    require(isinstance(roles, list) and bool(roles)
            and all(isinstance(role, dict) and role.get('scope') == {'type': 'tenant', 'tenant_id': str(tenant)}
                    for role in roles),
            'fixture functional roles must use only the current Tenant scope')
    return roles


def identity(client, tenant, permissions):
    summary = API.validate_user_identity(client, tenant, permissions)
    current = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    actual = {key for role in tenant_functional_assignments(current, tenant) for key in role['permissions']}
    require(actual == permissions, 'fixture User has unexpected additional permissions')
    return str(current['principal']['id']), summary


def organization_memberships(client, expected):
    current = client.request('GET', '/api/v1/system/auth/context', (200,)).payload['organization']
    actual = {('department', row['department_id']) for row in current['departments']}
    actual |= {('project_group', row['project_group_id']) for row in current['project_groups']}
    require(actual == set(expected.items()), 'fixture organization membership differs from its declared recipients')


def organization_administrator(client, tenant):
    current = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
    roles = tenant_functional_assignments(current, tenant)
    permissions = {key for role in roles for key in role['permissions']}
    require(current['principal']['type'] == 'user' and current['context']['type'] == 'tenant'
            and current['context']['tenant_id'] == str(tenant)
            and current['token']['type'] == 'first_party_access_token'
            and {role['role_key'] for role in roles} == {'tenant.administrator'}
            and ORGANIZATION_PERMISSIONS <= permissions,
            'membership mutations require the existing dedicated fixture Tenant administrator')
    return current['principal']['id'], {'role': 'tenant.administrator', 'tenant_id': str(tenant),
                                       'permissions_verified': sorted(ORGANIZATION_PERMISSIONS)}


def table_path(engine, name):
    return {'version': 'catalog.path/v1', 'engine_id': engine, 'segments': [
        {'term': 'server', 'kind': 'server', 'name': ''},
        {'term': 'schema', 'kind': 'namespace', 'name': 'public'},
        {'term': 'table', 'kind': 'table', 'name': name}]}


def locator(engine, name, item_id=None):
    query = {'type': 'table'}
    if item_id is not None:
        query['item_id'] = item_id
    return f'addp://engine/{engine}/path/public/{name}?' + urlencode(query)


def production_task(engine, source, name):
    require(re.fullmatch(r'pg_initial_[a-f0-9]{32}', name), 'invalid owned production name')
    return {'name': name, 'task_type': 'sync', 'enabled': False, 'schedule': '', 'auto_scan_metadata': False,
            'config': {'runtime': {'boundary': 'bounded'}, 'load': {'mode': 'snapshot'},
                'source': {'locator': locator(engine, 'metric_people', source['id']),
                           'data_type': 'table', 'representation': 'native'},
                'target': {'parent_locator': f'addp://engine/{engine}/path/public?' + urlencode(
                    {'type': 'schema', 'node_id': source['node_id']}), 'name': name,
                    'data_type': 'table', 'representation': 'native', 'policy': {'apply_mode': 'replace'}},
                'transforms': [{'type': 'field_mapping', 'version': 'v1', 'mode': 'project', 'fields': [
                    {'source': 'person_id', 'target': 'person_id', 'target_type': 'string', 'nullable': False}]}],
                'batch_size': 100}}


def validate_production_grant(row, *, execution, path, recipient, facts, source_expiry):
    require(row.get('approval_mode') == 'task_production' and row.get('producer_owner') == 'transfer'
            and row.get('production_execution_id') == execution and row.get('catalog_path') == path
            and row.get('item_fingerprint') == facts['item_fingerprint']
            and row.get('boundary_hash') == facts['boundary_hash']
            and row.get('recipient_type') == 'user' and row.get('recipient_id') == recipient
            and row.get('operator_principal_id') == recipient and row.get('action') == 'read'
            and row.get('direct_permission_version') == '0' and row.get('direct_permission_source') == '' and row.get('catalog_request_id') is None
            and row.get('expiry_mode') == 'at_time' and row.get('revocation') is None
            and re.fullmatch(r'[1-9][0-9]*', row.get('issuer_principal_id', ''))
            and re.fullmatch(r'[1-9][0-9]*', row.get('issuer_membership_id', ''))
            and row['issuer_principal_id'] != recipient,
            'initial Grant did not bind the exact output, human origin and independent machine issuer')
    require(str(uuid.UUID(row['request_id'])) == row['request_id']
            and datetime.fromisoformat(row['expires_at'].replace('Z', '+00:00'))
                == datetime.fromisoformat(source_expiry.replace('Z', '+00:00')),
            'initial Grant changed the inherited source expiry')


def basic_scan(reader, engine, names):
    created = reader.request('POST', '/api/v1/meta/scan/run/manual', (201,), {
        'engine_id': engine, 'targets': [locator(engine, name) for name in names],
        'scan_depth': 'basic', 'force': True, 'trigger_type': 'manual', 'source': 'meta'}).payload
    execution = created.get('execution_id')
    require(isinstance(execution, str) and str(uuid.UUID(execution)) == execution, 'scan execution UUID is missing')
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        current = reader.request('GET', f'/api/v1/meta/executions/{execution}', (200,)).payload
        require(current.get('execution_id') == execution, 'scan execution identity changed')
        if current.get('status') == 'success':
            require(current.get('scan_context', {}).get('scan_depth') == 'basic', 'scan was not basic')
            metadata = current.get('metadata', {})
            require(metadata.get('items_scanned') == len(names) and metadata.get('catalog_nodes_scanned') == 0,
                    'exact scan expanded beyond its selected leaves')
            return execution
        require(current.get('status') in {'pending', 'running'}, 'basic scan failed')
        time.sleep(1)
    raise SuiteError('basic scan exceeded its deadline')


def item(reader, engine, name):
    values = reader.request('GET', f'/api/v1/meta/engines/{engine}/items?branch=public', (200,), response_type=list).payload
    matches = [row for row in values if row.get('full_name') == 'public.' + name and row.get('item_type') == 'table']
    require(len(matches) == 1 and isinstance(matches[0].get('id'), int) and matches[0]['id'] > 0,
            'one persisted ordinary PG DataItem is required')
    return matches[0]


def preview(reader, target, allowed, expected=None, *, columns=('person_id',), values=('A', 'B')):
    result = reader.request('GET', '/api/v1/manager/preview?' + urlencode({
        'locator': target, 'page': 1, 'page_size': 20}), (200,) if allowed else (403,)).payload
    if not allowed:
        require(set(result) <= {'error', 'error_code'} and bool(result.get('error')),
                'denied preview exposed data or metadata')
        require(result.get('error_code') != 'security_protection_required', 'Security failure is not source denial')
        return
    data = result.get('data', {})
    require(result.get('preview_type') == 'table' and data.get('columns') == list(columns)
            and sorted(row.get(columns[0]) for row in data.get('rows', [])) == list(values),
            'Manager did not read the deterministic PG source')
    if expected is not None:
        require(data.get('total') == expected, 'PG source row count changed')


def mutate_source_content(action):
    """Ask the Business owner to mutate only its disposable PG source table."""
    repository = Path(__file__).resolve().parents[2]
    required = {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOSTED': '1',
                'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_TEST': '1', 'POSTGRES_DB': 'addp_online'}
    require(action in {'content-change', 'content-reset'} and sys.platform == 'linux'
            and os.uname().machine == 'x86_64' and not (repository / '.env').exists()
            and all(os.environ.get(key) == value for key, value in required.items()),
            'PG content changes require the isolated Hosted suite')
    # Physical fixture operations need Docker, not any platform User or Service credential.
    environment = dict(required, PATH=os.environ['PATH'], ONLINE_SUITE=SUITE)
    try:
        result = subprocess.run(['bash', str(repository / 'business/scripts/online-metric-postgres-fixture.sh'), action],
            cwd=repository, env=environment, capture_output=True, text=True, timeout=30)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise SuiteError('PG content fixture did not complete') from exc
    receipt = 'pg-content:changed' if action == 'content-change' else 'pg-content:baseline'
    require(result.returncode == 0 and result.stdout.strip() == receipt, 'PG content fixture failed its source oracle')


class DependencyOutage:
    """Fault one declared PG dependency in the existing disposable Hosted deployment."""

    def __init__(self, module):
        require(module in {'meta', 'catalog'}, 'PG outage only permits Meta or Catalog')
        self.module = module
        self.port = {'meta': 8082, 'catalog': 8192}[module]
        self.repository = Path(__file__).resolve().parents[2]
        required = {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOSTED': '1',
                    'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_TEST': '1', 'POSTGRES_DB': 'addp_online',
                    'META_URL': 'http://127.0.0.1:8082', 'MANAGER_URL': 'http://127.0.0.1:8081',
                    'CATALOG_URL': 'http://127.0.0.1:8192',
                    'SYSTEM_URL': 'http://127.0.0.1:8180', 'GATEWAY_URL': 'http://127.0.0.1:8000'}
        require(all(os.environ.get(key) == value for key, value in required.items()) and sys.platform == 'linux'
                and os.uname().machine == 'x86_64' and not (self.repository / '.env').exists(),
                'PG dependency outage requires the isolated Hosted deployment')
        self.secret = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
        require(self.secret.is_absolute() and self.secret.is_dir() and not self.secret.resolve().is_relative_to(self.repository),
                'PG dependency outage requires an external Hosted Secret directory')
        self.secret = self.secret.resolve()
        self.commit = PREFLIGHT.git(self.repository, 'rev-parse', 'HEAD')
        self.stopped = False

    def snapshot(self, module):
        service = PREFLIGHT.parse_service(module + '=' + os.environ[module.upper() + '_URL'])
        payload = API.GatewayClient(service.base_url, '', 10).request('GET', '/health/ready', (200,)).payload
        PREFLIGHT.validate_health(service, payload, self.commit, 'ready')
        fields = ('started_at', 'build_id', 'git_commit', 'source_fingerprint')
        # System/Gateway standalone health publishes build/start identity, not registration identity.
        if module in {'meta', 'manager', 'catalog'}:
            require(payload.get('role') == 'backend' and payload.get('registration_state') == 'registered'
                    and str(uuid.UUID(payload['instance_id'])) == payload['instance_id'], 'invalid Hosted backend identity')
            fields += ('instance_id',)
        return {key: payload[key] for key in fields}

    def run(self, operation):
        require(operation in {'stop', 'start'}, 'PG outage permits only standard stop/start')
        environment = {key: value for key, value in os.environ.items() if not key.startswith((
            'ADDP_ONLINE_TEST_USER_', 'ADDP_ONLINE_FIXTURE_', 'ADDP_ONLINE_ADMIN_USER_'))}
        environment['SKIP_MODTIDY'] = '1'
        environment['PGAPPNAME'] = source_application_name(self.module)
        with (self.secret / f'pg-{self.module}-lifecycle.log').open('a') as log:
            os.chmod(log.name, 0o600)
            try:
                result = subprocess.run(['bash', f'scripts/dev/{operation}.sh', '-' + self.module], cwd=self.repository,
                    env=environment, stdout=log, stderr=subprocess.STDOUT, timeout=120, check=False)
            except subprocess.TimeoutExpired as error:
                raise SuiteError('PG dependency lifecycle exceeded its bounded deadline') from error
        require(result.returncode == 0, 'PG dependency lifecycle failed')

    def __enter__(self):
        self.before = {module: self.snapshot(module) for module in ('meta', 'system', 'gateway', 'manager', 'catalog')}
        self.stopped = True  # Also restore an unknown stop outcome.
        try:
            self.run('stop')
            try:
                with socket.create_connection(('127.0.0.1', self.port), timeout=3):
                    raise SuiteError('PG dependency Backend still accepts connections')
            except ConnectionRefusedError:
                pass
            return self
        except Exception:
            self.__exit__(None, None, None)
            raise

    def __exit__(self, *_):
        if self.stopped:
            self.run('start')
            self.stopped = False
            self.after = {module: self.snapshot(module) for module in self.before}
            before, after = self.before[self.module], self.after[self.module]
            require(before['instance_id'] != after['instance_id']
                    and datetime.fromisoformat(after['started_at']) > datetime.fromisoformat(before['started_at'])
                    and all(before[key] == after[key] for key in ('build_id', 'git_commit', 'source_fingerprint')),
                    'PG dependency recovery did not prove a new process with identical build inputs')
            require(all(self.before[module] == self.after[module] for module in self.before if module != self.module),
                    'PG dependency outage changed another running owner')


class BrowserObservation:
    """Keep the real UI open while the owner performs its existing IAM commands."""

    def __init__(self, tenant, principal, target):
        self.binding = {'run_id': os.environ['ADDP_ONLINE_TEST_RUN_ID'], 'tenant_id': str(tenant),
                        'principal_id': str(principal), 'locator': target}
        self.evidence = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / 'pg-authorization-browser.json'
        self.secret = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
        require(self.secret.is_absolute() and self.secret.is_dir(), 'browser requires the Hosted Secret directory')
        self.process = self.output = self.temporary = None
        self.test_name = 'PG member exits'
        self.login_keys = ('ADDP_ONLINE_TEST_USER_USERNAME', 'ADDP_ONLINE_TEST_USER_PASSWORD')

    def __enter__(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='pg-browser-', dir=self.secret)
        self.control = Path(self.temporary.name)
        self.evidence.unlink(missing_ok=True)
        # The browser must log in as the reader, not inherit the API fixture or administrator Token.
        environment = {key: value for key, value in os.environ.items() if key in {
            'PATH', 'HOME', 'TMPDIR', 'TMP', 'TEMP', 'CI', 'PLAYWRIGHT_BROWSERS_PATH', 'NODE_EXTRA_CA_CERTS'}}
        environment.update({key: os.environ[key] for key in (
            'ADDP_ONLINE_HOSTED', 'ADDP_ONLINE_TEST', 'ADDP_ONLINE_ARTIFACT_DIR', 'CONSOLE_URL', *self.login_keys)})
        environment.update(ADDP_PG_BROWSER_BINDING=json.dumps(self.binding), ADDP_PG_BROWSER_CONTROL=str(self.control))
        try:
            self.output = (self.control / 'output.log').open('w')
            self.process = subprocess.Popen(['npm', 'exec', '--', 'playwright', 'test',
                '--config=playwright.online.config.js', '--grep', self.test_name,
                '--output=' + str(self.control / 'playwright'),
                'e2e/online/pg-source-authorization.spec.js'],
                cwd=Path(__file__).resolve().parents[2] / 'console/frontend', env=environment,
                stdout=self.output, stderr=subprocess.STDOUT, start_new_session=True)
            return self
        except Exception:
            self.__exit__(None, None, None)
            raise

    def marker(self, kind):
        require(kind in {'department', 'project_group'}, 'invalid browser member-exit phase')
        return dict(self.binding, phase=kind)

    def read_ready(self, kind):
        ready = self.control / ('ready-' + kind + '.json')
        deadline = time.monotonic() + 120
        while not ready.exists():
            require(self.process.poll() is None and time.monotonic() < deadline,
                    'browser did not reach its member-exit observation: ' + kind)
            time.sleep(.2)
        return json.loads(ready.read_text())

    def wait(self, kind):
        require(self.read_ready(kind) == self.marker(kind), 'browser control binding changed')

    def release(self, kind):
        path = self.control / ('release-' + kind + '.tmp')
        path.write_text(json.dumps(self.marker(kind)))
        path.replace(self.control / ('release-' + kind + '.json'))

    def finish(self):
        try:
            require(self.process.wait(timeout=180) == 0, 'PG browser member-exit acceptance failed')
        except subprocess.TimeoutExpired as error:
            raise SuiteError('PG browser member-exit acceptance timed out') from error
        require(self.evidence.is_file(), 'PG browser report missing')
        result = json.loads(self.evidence.read_text())
        anonymous = result.pop('anonymous_refreshes', None)
        expected = dict(self.binding, schema_version='addp.pg-source-authorization-browser/v1', result='passed',
                        checks=BROWSER_CHECKS, login_requests=1,
                        observations=[{'closed_type': 'department', 'refreshes': 1, 'preview_status': 200},
                                      {'closed_type': 'project_group', 'refreshes': 1, 'preview_status': 403}])
        require(result == expected and type(anonymous) is int and anonymous in {0, 1},
                'PG browser evidence does not prove the exact member-exit flow')
        return dict(result, anonymous_refreshes=anonymous)

    def __exit__(self, *_):
        try:
            if self.process is not None and self.process.poll() is None:
                os.killpg(self.process.pid, signal.SIGTERM)
                try:
                    self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    os.killpg(self.process.pid, signal.SIGKILL)
                    self.process.wait(timeout=5)
        finally:
            if self.output is not None:
                self.output.close()
            if self.temporary is not None:
                self.temporary.cleanup()


class WorkspaceObservation(BrowserObservation):
    """Two administrator sessions edit one permit; only the tracked Grant is revoked."""

    def __init__(self, tenant, principal, target, engine, issuer, grant, permission, facts):
        super().__init__(tenant, principal, target)
        self.binding.update(engine_id=str(engine), issuer_id=str(issuer), grant_id=grant, direct_permission=dict(permission),
                            permission_target={'catalog_path': facts['boundary']['native_target'], 'boundary_hash': facts['boundary_hash']})
        self.evidence = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / 'pg-authorization-workspace.json'
        self.test_name = 'PG authorization workspace'
        self.login_keys += ('ADDP_ONLINE_PG_OPERATOR_USERNAME', 'ADDP_ONLINE_PG_OPERATOR_PASSWORD')

    def finish(self):
        try:
            require(self.process.wait(timeout=180) == 0, 'PG browser workspace acceptance failed')
        except subprocess.TimeoutExpired as error:
            raise SuiteError('PG browser workspace acceptance timed out') from error
        require(self.evidence.is_file(), 'PG browser workspace report missing')
        result = json.loads(self.evidence.read_text())
        expected = dict(self.binding, schema_version='addp.pg-authorization-workspace/v1', result='passed',
                        checks=WORKSPACE_CHECKS, login_requests=3, preview_statuses=[200, 403],
                        direct_permission_edit=self.expected_permission_edit(),
                        direct_permission_observations=[{'search': 'metric_people', 'state': state, 'total': total,
                            'item_fingerprints': [self.binding['direct_permission']['item_fingerprint']] if total else []}
                            for state, total in [('', 1), ('allowed', 1), ('denied', 0)]])
        require(result == expected, 'PG browser workspace evidence is incomplete or changed identity')
        return result

    def expected_permission_edit(self):
        initial = self.binding['direct_permission']
        version = int(initial['version'])
        require(initial['source'] == 'explicit' and initial['state'] == 'allowed', 'workspace requires explicit baseline permission')
        return {'item_fingerprint': initial['item_fingerprint'], 'boundary_hash': self.binding['permission_target']['boundary_hash'],
                'versions': [str(version + offset) for offset in range(3)],
                'writes': [{'expected_version': str(version), 'allowed': False, 'status': 200},
                           {'expected_version': str(version), 'allowed': False, 'status': 409},
                           {'expected_version': str(version + 1), 'allowed': True, 'status': 200}],
                'conflict_code': 'resource_version_conflict', 'cancelled_writes': 0, 'review_writes': 0,
                'grant_unchanged': True, 'closed_preview_status': 200, 'same_reader_credential': True, 'final_state': 'allowed'}


class CollectionObservation(BrowserObservation):
    """Real approval UI; register its command before allowing its network send."""
    def __init__(self, tenant, issuer, engine, task, digest, purpose, expires_at):
        super().__init__(tenant, issuer, '')
        self.binding.pop('locator')
        self.binding.update(engine_id=str(engine), task_id=str(task), task_definition_digest=digest,
                            purpose=purpose, expires_at=expires_at)
        self.evidence = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / 'pg-collection-browser.json'
        self.test_name = 'PG collection qualification workspace'
        self.login_keys = ('ADDP_ONLINE_PG_OPERATOR_USERNAME', 'ADDP_ONLINE_PG_OPERATOR_PASSWORD')
        self.command_id = None

    def marker(self, kind):
        require(kind in {'command', 'committed', 'approved', 'revoked'}, 'invalid collection browser phase')
        return dict(self.binding, phase=kind)

    def capture_command(self):
        marker = self.read_ready('command')
        require(isinstance(marker, dict) and set(marker) == set(self.marker('command')) | {'command'}
                and {key: value for key, value in marker.items() if key != 'command'} == self.marker('command'),
                'collection browser command binding changed')
        body = marker['command']
        expected = {'task_id': self.binding['task_id'],
            'expected_task_definition_digest': self.binding['task_definition_digest'],
            'purpose': self.binding['purpose'], 'expires_at': self.binding['expires_at']}
        require(isinstance(body, dict) and set(body) == set(expected) | {'command_id'}
                and isinstance(body.get('command_id'), str)
                and re.fullmatch(r'[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}', body['command_id'])
                and all(body.get(key) == value for key, value in expected.items() if key != 'expires_at')
                and collection_instant(body.get('expires_at')) == collection_instant(expected['expires_at']),
                'browser approval changed its inspected task, purpose or exact expiry')
        self.command_id = body['command_id']
        return body

    def committed_row(self):
        marker = self.read_ready('committed')
        require(isinstance(marker, dict) and set(marker) == set(self.marker('committed')) | {'qualification'}
                and {key: value for key, value in marker.items() if key != 'qualification'} == self.marker('committed')
                and isinstance(marker.get('qualification'), dict), 'collection committed response binding changed')
        return marker['qualification']

    def finish(self):
        try:
            require(self.process.wait(timeout=180) == 0, 'PG collection browser acceptance failed')
        except subprocess.TimeoutExpired as error:
            raise SuiteError('PG collection browser acceptance timed out') from error
        require(self.command_id is not None and self.evidence.is_file(), 'PG collection browser report missing')
        result = json.loads(self.evidence.read_text())
        expected = dict(self.binding, schema_version='addp.pg-collection-browser/v1', result='passed',
            command_id=self.command_id, checks=COLLECTION_BROWSER_CHECKS[:-1], login_requests=1,
            approval_requests=2, revocation_requests=1, dropped_approval_responses=1,
            navigation_writes=0, unchanged_identity=True)
        require(result == expected and all(type(result.get(key)) is int for key in (
            'login_requests', 'approval_requests', 'revocation_requests', 'dropped_approval_responses', 'navigation_writes'))
            and result.get('unchanged_identity') is True, 'PG collection browser evidence incomplete or changed binding')
        return result


def collection_instant(value):
    """One RFC3339 interpretation, without Python's fractional-second truncation."""
    match = re.fullmatch(r'(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})', value) if type(value) is str else None
    require(match is not None, 'invalid qualification timestamp')
    try:
        whole = datetime.fromisoformat(match[1] + match[3].replace('Z', '+00:00')).astimezone(timezone.utc)
    except ValueError:
        raise SuiteError('invalid qualification timestamp') from None
    return whole, int((match[2] or '').ljust(9, '0'))


def same_collection_row(left, right):
    if set(left) != set(right):
        return False
    left, right = dict(left), dict(right)
    for key in ('granted_at', 'expires_at', 'revoked_at'):
        if key in left:
            left[key], right[key] = collection_instant(left[key]), collection_instant(right[key])
    return left == right


SOURCE_MODULES = {'system', 'gateway', 'meta', 'security', 'manager', 'catalog', 'transfer'}
SOURCE_CATEGORIES = ('namespaces', 'tables', 'workspace_detection', 'other')


def source_application_name(module):
    prefix = os.environ.get('ADDP_ONLINE_PG_SOURCE_PREFIX', '')
    require(re.fullmatch(r'pg_auth_[a-f0-9]{16}', prefix) and module in SOURCE_MODULES,
            'invalid isolated source application identity')
    return prefix + '_' + module


def source_connection_summary(rows, delta, blocked):
    """Public projection only. Labels are observations, never authorization identities."""
    require(type(rows) is list and len(rows) <= 256 and type(delta) is dict
            and all(k in SOURCE_CATEGORIES and type(v) is int and v >= 0 for k, v in delta.items()),
            'invalid source connection observation')
    totals = dict.fromkeys(SOURCE_CATEGORIES, 0)
    by_module, protocol_empty_calls = {}, {}
    for row in rows:
        require(type(row) is dict and set(row) == {'module', 'category', 'kind', 'pid', 'session_id', 'at', 'query_id'}
                and type(row['module']) is str and row['module'] in SOURCE_MODULES | {'unknown'}
                and type(row['category']) is str and row['category'] in (*SOURCE_CATEGORIES, 'protocol_empty')
                and type(row['kind']) is str and row['kind'] in {'version_probe', 'unclassified', 'empty_query'}
                and (row['category'] == 'protocol_empty') == (row['kind'] == 'empty_query')
                and type(row['pid']) is int and row['pid'] > 0
                and type(row['session_id']) is str and re.fullmatch(r'[a-f0-9]+\.[a-f0-9]+', row['session_id'])
                and type(row['query_id']) is str and re.fullmatch(r'-?[0-9]{1,20}', row['query_id']),
                'invalid source connection event')
        collection_instant(row['at'])
        if row['category'] == 'protocol_empty':
            protocol_empty_calls[row['module']] = protocol_empty_calls.get(row['module'], 0) + 1
            continue
        totals[row['category']] += 1
        by_module.setdefault(row['module'], dict.fromkeys(SOURCE_CATEGORIES, 0))[row['category']] += 1
    tables = [row for row in rows if row['category'] == 'tables']
    matched = blocked is None or (len(tables) == 1 and tables[0]['module'] == 'meta'
                                  and tables[0]['pid'] == blocked['pid'])
    reconciled = totals == {key: delta.get(key, 0) for key in SOURCE_CATEGORIES}
    return {'events': rows, 'record_count': len(rows), 'observed_calls': totals, 'by_module': by_module,
            'protocol_empty_calls': protocol_empty_calls,
            'reconciled': reconciled, 'blocked_pid_matched': matched,
            'verified': reconciled and matched and 'unknown' not in by_module and 'unknown' not in protocol_empty_calls}


class PGCollectionReadBarrier:
    """Bounded source-owned lock + aggregate observations, only on disposable PG."""
    def __init__(self):
        self.schema = 'zz_pg_collection_' + uuid.uuid4().hex[:16]
        self.application = 'pg_collection_barrier_' + uuid.uuid4().hex[:16]
        self.container = self.process = self.pid = None
        self.schema_owned = self.released = self.closed = False
        self.log_offset = None

    def begin_observation(self):
        # The log stays private inside the admitted disposable source container.
        source_application_name('meta')
        result = self.query("""WITH checkpoint AS MATERIALIZED (
            SELECT (pg_stat_file('pg_source_private/statements.json')).size AS size)
            SELECT json_build_object('path',pg_current_logfile('jsonlog'),'size',size,
              'complete',size=0 OR pg_read_binary_file('pg_source_private/statements.json',greatest(size-1,0),1)=decode('0a','hex'))
            FROM checkpoint;""")
        require(result.get('path') == 'pg_source_private/statements.json'
                and type(result.get('size')) is int and result['size'] >= 0 and result.get('complete') is True,
                'private source log is unavailable')
        self.log_offset = result['size']

    def connection_observation(self, before, after, blocked=None):
        require(type(self.log_offset) is int and self.log_offset >= 0, 'source observation was not started')
        prefix = source_application_name('meta').removesuffix('_meta')
        # Classification happens inside PostgreSQL. No raw SQL, parameter or
        # untrusted application text leaves its private source log directory.
        # Collector writes are asynchronous; bounded reads wait only for the
        # already-completed statements included in the original counter sample.
        delta = {key: after['calls'].get(key, 0) - before['calls'].get(key, 0) for key in SOURCE_CATEGORIES}
        deadline = time.monotonic() + 2
        while True:
            sample = self.query(f"""WITH data AS MATERIALIZED (
              SELECT pg_read_file('pg_source_private/statements.json',{self.log_offset},262145) AS raw),
            lines AS MATERIALIZED (
              SELECT line::jsonb AS j, ordinal FROM data,
                LATERAL regexp_split_to_table(raw,E'\\n') WITH ORDINALITY parts(line,ordinal)
              WHERE octet_length(raw)<=262144 AND (raw='' OR right(raw,1)=E'\\n') AND line<>''),
            completed AS MATERIALIZED (
              SELECT j, ordinal, regexp_replace(j->>'message','^duration: [0-9.]+ ms  (statement|execute [^:]+): ','') AS sql
              FROM lines WHERE j->>'user'='metric_reader' AND j->>'dbname'='metric_fixture'
                AND j->>'message' ~ '^duration: [0-9.]+ ms  (statement|execute [^:]+): ')
            SELECT json_build_object('bytes',(SELECT octet_length(raw) FROM data),
              'complete',(SELECT raw='' OR right(raw,1)=E'\\n' FROM data),
              'count',(SELECT count(*) FROM completed),'events',COALESCE((SELECT json_agg(json_build_object(
                'module',CASE WHEN j->>'application_name' IN ({','.join(chr(39)+source_application_name(m)+chr(39) for m in sorted(SOURCE_MODULES))})
                  THEN substr(j->>'application_name',{len(prefix)+2}) ELSE 'unknown' END,
                'pid',(j->>'pid')::integer,'session_id',j->>'session_id',
                'at',to_char((j->>'timestamp')::timestamptz AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
                'query_id',COALESCE(j->>'query_id','0'),
                'kind',CASE WHEN sql=';' THEN 'empty_query'
                  WHEN btrim(sql)='SELECT version()' THEN 'version_probe' ELSE 'unclassified' END,
                'category',CASE WHEN sql=';' THEN 'protocol_empty'
                  WHEN sql LIKE '%FROM information_schema.tables t%' AND sql LIKE '%pg_total_relation_size%'
                  THEN 'tables' WHEN sql LIKE '%pg_catalog.pg_namespace) s%' THEN 'namespaces'
                  WHEN sql LIKE '%SELECT COUNT(*)%' AND sql LIKE '%lower(table_name) IN%' THEN 'workspace_detection'
                  ELSE 'other' END) ORDER BY ordinal) FROM (SELECT * FROM completed ORDER BY ordinal LIMIT 257) bounded),'[]'::json));""")
            require(type(sample) is dict and set(sample) == {'bytes', 'complete', 'count', 'events'}
                    and type(sample.get('complete')) is bool
                    and type(sample.get('bytes')) is int and 0 <= sample['bytes'] <= 262144
                    and type(sample.get('events')) is list and type(sample.get('count')) is int
                    and len(sample['events']) == sample['count'] <= 256,
                    'source connection observation exceeded its complete bounded window')
            if sample.get('complete') is True:
                result = source_connection_summary(sample['events'], delta, blocked)
                if result['reconciled'] or time.monotonic() >= deadline:
                    self.log_offset += sample['bytes']
                    return result
            require(time.monotonic() < deadline, 'source connection log remained incomplete')
            time.sleep(.05)

    def command(self):
        require(isinstance(self.container, str) and re.fullmatch('[a-f0-9]{64}', self.container),
                'source barrier has no admitted container')
        return ['docker', 'exec', '-i', '-e', 'PGOPTIONS', self.container, 'psql', '-XqAt', '-P', 'pager=off',
                '-v', 'ON_ERROR_STOP=1', '-U', 'postgres', '-d', 'metric_fixture']

    def query(self, sql, *, write=False):
        environment = dict(os.environ, PGOPTIONS='-c statement_timeout=3000 -c lock_timeout=1000'
                           + ('' if write else ' -c default_transaction_read_only=on'))
        try:
            result = subprocess.run(self.command(), input=sql, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                    text=True, timeout=6, env=environment, check=False)
        except (OSError, subprocess.SubprocessError):
            raise SuiteError('source barrier query transport failed') from None
        require(result.returncode == 0 and len(result.stdout) <= 65536, 'source barrier query failed')
        return json.loads(result.stdout)

    def admit(self):
        require(all(os.environ.get(k) == v for k, v in {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux',
            'ADDP_ONLINE_TEST': '1', 'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1',
            'POSTGRES_DB': 'addp_online', 'ONLINE_SUITE': SUITE}.items()) and sys.platform == 'linux'
            and not (Path(__file__).resolve().parents[2] / '.env').exists(), 'source barrier requires its Hosted suite')
        inspected = subprocess.run(['docker', 'inspect', '--format', '{{json .}}', 'addp-metric-online-disposable'],
            capture_output=True, text=True, timeout=5, check=True)
        owner = json.loads(inspected.stdout)
        require(owner['Config']['Labels'].get('com.addp.online-fixture') == 'metric' and owner['State']['Running']
                and 'POSTGRES_DB=metric_fixture' in owner['Config']['Env']
                and owner['NetworkSettings']['Ports'].get('5432/tcp') == [{'HostIp': '127.0.0.1', 'HostPort': '55433'}],
                'refusing a foreign source barrier container')
        self.container = owner['Id']

    def open(self):
        self.admit()
        prior = self.query(f"SELECT json_build_object('exists',to_regnamespace('{self.schema}') IS NOT NULL);")
        require(prior == {'exists': False}, 'source barrier schema already exists')
        self.schema_owned = True  # Own the unique target before an unknown CREATE outcome.
        self.query(f'''BEGIN; CREATE SCHEMA {self.schema}; CREATE TABLE {self.schema}.canary(id integer);
            GRANT USAGE ON SCHEMA {self.schema} TO metric_reader;
            GRANT SELECT ON {self.schema}.canary TO metric_reader; COMMIT;
            SELECT json_build_object('created',true);''', write=True)
        self.begin_observation()
        self.baseline = self.counts()
        environment = dict(os.environ, PGOPTIONS='-c statement_timeout=3000 -c lock_timeout=1000'
            ' -c idle_in_transaction_session_timeout=30000 -c application_name=' + self.application)
        self.process = subprocess.Popen(self.command(), stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.DEVNULL, env=environment)
        self.process.stdin.write(b'''BEGIN; LOCK TABLE public.metric_people IN ACCESS EXCLUSIVE MODE;
            SELECT json_build_object('pid',pg_backend_pid(),'locked',true,'at',clock_timestamp());
''')
        self.process.stdin.flush()
        locked = self.read_lock_reply()
        require(locked.get('locked') is True and type(locked.get('pid')) is int and locked['pid'] > 0,
                'source barrier did not acquire the exact lock')
        self.pid = locked['pid']
        return locked

    def read_lock_reply(self):
        deadline, buffer = time.monotonic() + 5, b''
        while b'\n' not in buffer:
            remaining = deadline - time.monotonic()
            require(remaining > 0, 'source lock handshake timed out')
            ready, _, _ = select.select([self.process.stdout], [], [], remaining)
            require(bool(ready), 'source lock handshake timed out')
            chunk = os.read(self.process.stdout.fileno(), 4096)
            require(chunk and len(buffer) + len(chunk) <= 8192, 'source lock connection failed')
            buffer += chunk
        require(not buffer.split(b'\n', 1)[1].strip(), 'unexpected source lock output')
        return json.loads(buffer)

    def counts(self):
        result = self.query('''WITH source_statements AS MATERIALIZED (
            SELECT queryid::text AS query_id, toplevel AS top_level, calls,
              CASE WHEN btrim(query) = 'SELECT version()' THEN 'version_probe' ELSE 'unclassified' END AS kind,
              CASE WHEN query LIKE '%FROM information_schema.tables t%' AND query LIKE '%pg_total_relation_size%'
              THEN 'tables' WHEN query LIKE '%pg_catalog.pg_namespace) s%' THEN 'namespaces'
              WHEN query LIKE '%SELECT COUNT(*)%' AND query LIKE '%lower(table_name) IN%' THEN 'workspace_detection'
              ELSE 'other' END AS category
            FROM addp_observer.pg_stat_statements WHERE userid=(SELECT oid FROM pg_roles WHERE rolname='metric_reader')
              AND dbid=(SELECT oid FROM pg_database WHERE datname=current_database()))
          SELECT json_build_object('reset_at',i.stats_reset,'calls',
          COALESCE((SELECT json_object_agg(category,calls) FROM (
            SELECT category,sum(calls) AS calls FROM source_statements GROUP BY 1) s),'{}'::json),
          'other_statement_count',(SELECT count(*) FROM source_statements WHERE category='other'),
          'other_statements',COALESCE((SELECT json_agg(json_build_object('query_id',query_id,'top_level',top_level,
              'kind',kind,'calls',calls) ORDER BY query_id,top_level) FROM (
            SELECT query_id,top_level,kind,calls FROM source_statements WHERE category='other'
              ORDER BY query_id,top_level LIMIT 128) s),'[]'::json))
          FROM addp_observer.pg_stat_statements_info i;''')
        require(isinstance(result, dict)
                and set(result) == {'calls', 'reset_at', 'other_statement_count', 'other_statements'}
                and isinstance(result.get('calls'), dict) and result.get('reset_at')
                and all(k in {'tables', 'namespaces', 'workspace_detection', 'other'} and type(v) is int and v >= 0
                        for k, v in result['calls'].items()), 'invalid source statement counters')
        rows = result.get('other_statements')
        require(isinstance(rows, list) and type(result.get('other_statement_count')) is int
                and len(rows) == result['other_statement_count'] <= 128
                and all(isinstance(row, dict) and set(row) == {'query_id', 'top_level', 'kind', 'calls'}
                        and type(row['query_id']) is str and re.fullmatch(r'-?[0-9]{1,20}', row['query_id'])
                        and type(row['top_level']) is bool and type(row['kind']) is str
                        and row['kind'] in {'version_probe', 'unclassified'}
                        and type(row['calls']) is int and row['calls'] >= 0 for row in rows)
                and len({(row['query_id'], row['top_level']) for row in rows}) == len(rows)
                and sum(row['calls'] for row in rows) == result['calls'].get('other', 0),
                'invalid or truncated source other statement diagnostics')
        return result

    def wait_blocked(self):
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            require(self.process.poll() is None, 'source lock session exited before revocation')
            result = self.query(f'''SELECT json_build_object('at',clock_timestamp(),'blocked',COALESCE((SELECT
                json_agg(json_build_object('pid',pid,'started_at',query_start)) FROM pg_stat_activity
                WHERE usename='metric_reader' AND datname='metric_fixture' AND state='active'
                  AND wait_event_type='Lock' AND {self.pid}=ANY(pg_blocking_pids(pid))
                  AND query LIKE '%pg_total_relation_size%' AND query LIKE '%FROM information_schema.tables t%'),
                '[]'::json));''')
            require(isinstance(result.get('blocked'), list) and len(result['blocked']) <= 1,
                    'ambiguous source enumeration barrier')
            if result['blocked']:
                blocked = result['blocked'][0]
                require(type(blocked.get('pid')) is int and blocked['pid'] > 0 and blocked.get('started_at'),
                        'source enumeration has no exact backend identity')
                return dict(blocked, observed_at=result['at'], blocker_pid=self.pid)
            time.sleep(.1)
        raise SuiteError('source enumeration did not reach its bounded in-flight barrier')

    def release(self):
        if self.process is not None:
            try:
                if self.process.poll() is None:
                    self.process.stdin.write(b'ROLLBACK; \\q\n')
                    self.process.stdin.flush()
                self.process.wait(timeout=5)
            finally:
                if self.process.poll() is None:
                    self.process.kill()
                    self.process.wait(timeout=5)
                self.process.stdin.close()
                self.process.stdout.close()
                self.process = None
        if self.container is not None:
            result = self.query(f"SELECT json_build_object('sessions',count(*)) FROM pg_stat_activity WHERE application_name='{self.application}';")
            require(result == {'sessions': 0}, 'source lock backend remains after release')
        self.released = True

    def close(self):
        if self.closed:
            return
        self.release()
        if self.schema_owned:
            exists = self.query(f"SELECT json_build_object('exists',to_regnamespace('{self.schema}') IS NOT NULL);")
            if exists.get('exists') is True:
                self.query(f'''BEGIN; DROP TABLE {self.schema}.canary; DROP SCHEMA {self.schema}; COMMIT;
                    SELECT json_build_object('dropped',true);''', write=True)
            require(self.query(f"SELECT json_build_object('exists',to_regnamespace('{self.schema}') IS NOT NULL);")
                    == {'exists': False}, 'source barrier schema remains')
        self.closed = True


class MetaCollectionScenario:
    """Task-bound governance approval through real Users and the Meta Worker."""
    def __init__(self, lifecycle, issuer, run_id):
        self.life, self.issuer = lifecycle, issuer
        self.route = lifecycle.base + '/meta_collection_qualifications'
        self.prefix = 'pg_collection_' + run_id + '_' + uuid.uuid4().hex[:12]
        self.tasks, self.commands, self.executions = {}, {}, {}
        self.attempts = []
        self.source_barriers = []

    def task_rows(self):
        return self.life.reader.request('GET', '/api/v1/meta/scan/tasks', (200,), response_type=list).payload

    def recover_tasks(self):
        rows = self.task_rows()
        for name, owned in self.tasks.items():
            matches = [row for row in rows if row.get('name') == name]
            require(len(matches) <= 1, 'ambiguous collection task recovery')
            if matches:
                row = matches[0]
                require(type(row.get('id')) is int and row['id'] > 0
                        and row.get('engine_id') == self.life.engine and row.get('tenant_id') == self.life.tenant
                        and str(row.get('created_by')) == self.issuer
                        and (owned['id'] is None or owned['id'] == row['id']), 'refusing foreign collection task')
                owned['id'] = row['id']
        return rows

    def qualification_rows(self, diagnostic=None):
        rows = []
        for page in range(1, 11):
            path = self.route + f'?page={page}&page_size=100'
            result = (self.life.operator.request('GET', path, (200,)).payload if diagnostic is None else
                      self.observed_request(diagnostic, 'GET', path, client=self.life.operator))
            require(type(result.get('total')) is int and result['total'] <= 1000
                    and result.get('page') == page and result.get('page_size') == 100
                    and isinstance(result.get('data'), list), 'invalid collection history pagination')
            rows.extend(result['data'])
            if len(rows) == result['total']:
                require(len({row['id'] for row in rows}) == len(rows), 'duplicate qualification history')
                return rows
        raise SuiteError('collection history exceeded its bounded pages')

    def row(self, command, diagnostic=None):
        rows = [row for row in self.qualification_rows(diagnostic) if row.get('id') == command]
        if diagnostic is not None:
            diagnostic.update(stage='history_row_identity', matching_rows=len(rows))
        require(len(rows) == 1, 'owned collection qualification missing')
        row, body = rows[0], self.commands[command]
        if diagnostic is not None:
            diagnostic.update(stage='history_row_binding', binding={
                'engine': row.get('engine_id') == str(self.life.engine), 'task': row.get('task_id') == body['task_id'],
                'digest': row.get('task_definition_digest') == body['expected_task_definition_digest'],
                'purpose': row.get('purpose') == body['purpose'], 'depth': row.get('scan_depth') == 'basic',
                'scope': row.get('scope_kind') == 'engine', 'approver': row.get('granted_by_principal_id') == self.issuer,
                'engine_version': bool(re.fullmatch(r'[1-9][0-9]*', row.get('engine_version', ''))),
                'version': bool(re.fullmatch(r'[1-9][0-9]*', row.get('version', ''))),
                'expiry': collection_instant(row.get('expires_at')) == collection_instant(body['expires_at'])})
        require(row.get('engine_id') == str(self.life.engine) and row.get('task_id') == body['task_id']
                and row.get('task_definition_digest') == body['expected_task_definition_digest']
                and row.get('purpose') == body['purpose'] and row.get('scan_depth') == 'basic'
                and row.get('scope_kind') == 'engine' and row.get('granted_by_principal_id') == self.issuer
                and re.fullmatch(r'[1-9][0-9]*', row.get('engine_version', ''))
                and re.fullmatch(r'[1-9][0-9]*', row.get('version', ''))
                and collection_instant(row['expires_at']) == collection_instant(body['expires_at']),
                'qualification changed its approved binding')
        return row

    def create_task(self, suffix):
        name = self.prefix + suffix
        body = {'name': name, 'description': 'Disposable Meta collection qualification acceptance',
                'engine_id': self.life.engine, 'scope': {'type': 'engine'}, 'scan_depth': 'basic',
                'force': False, 'schedule': '', 'enabled': False}
        require(not any(row.get('name') == name for row in self.task_rows()), 'collection task already exists')
        self.tasks[name] = {'id': None, 'body': body}  # Own the unique name before POST, including unknown outcome.
        row = self.life.operator.request('POST', '/api/v1/meta/scan/tasks', (201,), body).payload
        self.recover_tasks()
        require(self.tasks[name]['id'] == row.get('id'), 'created collection task was not recovered exactly')
        return self.tasks[name]

    def facts(self, task):
        facts = self.life.operator.request('GET', self.life.base + f'/meta_collection_task_facts/{task}', (200,)).payload
        require(facts.get('schema_version') == 'meta.scan_task_facts/v1'
                and facts.get('task_id') == str(task) and facts.get('engine_id') == str(self.life.engine)
                and facts.get('scope_kind') == 'engine' and facts.get('scan_depth') == 'basic'
                and type(facts.get('force')) is bool and facts.get('read_options') == {}
                and re.fullmatch(r'[a-f0-9]{64}', facts.get('task_definition_digest', '')),
                'System did not return exact PG basic task facts')
        return facts

    def update(self, task, **changes):
        task['body'] = dict(task['body'], **changes)
        row = self.life.operator.request('PUT', f'/api/v1/meta/scan/tasks/{task["id"]}', (200,), task['body']).payload
        require(row.get('id') == task['id'], 'task update changed identity')

    def observed_request(self, attempt, method, path, *, client=None, expected=(200,), body=None):
        try:
            result = (self.life.reader if client is None else client).request(method, path, expected, body)
        except SuiteError as error:
            # Client errors can contain transport text. Publish only bounded
            # machine status/code, never the exception, response body or URL.
            match = re.search(r' returned HTTP ([1-5][0-9]{2}) \(([A-Za-z0-9_.-]{1,80})\)$', str(error))
            if match:
                attempt.update(http_status=int(match[1]), http_error_code=match[2])
            else:
                attempt['request_failed'] = True
            raise
        attempt['http_status'] = result.status
        return result.payload

    def trigger(self, task, allowed):
        execution, attempt = self.start_execution(task, allowed)
        return self.observe_execution(task, execution, allowed, attempt)

    def start_execution(self, task, allowed):
        attempt = {'task_id': str(task), 'expected_terminal': 'success' if allowed else 'failed', 'stage': 'trigger'}
        self.attempts.append(attempt)  # Keep the failing attempt, not only successful return values.
        created = self.observed_request(attempt, 'POST', f'/api/v1/meta/scan/tasks/{task}/trigger')
        attempt['stage'] = 'execution_identity'
        execution = created['execution_id']
        require(str(uuid.UUID(execution)) == execution and execution not in self.executions, 'scan execution is not unique')
        self.executions[execution] = task
        attempt['execution_id'] = execution
        return execution, attempt

    def observe_execution(self, task, execution, allowed, attempt, *, scheduled_due=None, admitted_items=None):
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            attempt['stage'] = 'execution_read'
            run = self.observed_request(attempt, 'GET', f'/api/v1/meta/executions/{execution}')
            attempt['stage'] = 'execution_binding'
            binding = {'execution': run.get('execution_id') == execution, 'task': run.get('source_task_id') == str(task),
                       'module': run.get('module') == 'meta', 'task_type': run.get('task_type') == 'scan',
                       'tenant': run.get('tenant_id') == self.life.tenant,
                       'actor': (run.get('triggered_by') is None if scheduled_due is not None else
                                 run.get('triggered_by') == int(self.life.recipient)),
                       'scope': run.get('scan_context') == {'engine_id': self.life.engine, 'scan_depth': 'basic'}}
            if scheduled_due is not None:
                binding.update(trigger=run.get('trigger_type') == 'scheduled', source=run.get('source') == 'meta',
                    boundary=run.get('execution_boundary') == 'bounded', parent=run.get('parent_execution_id') is None,
                    retry=run.get('retry_of_execution_id') is None)
                created = collection_instant(run.get('created_at'))
                require(created >= collection_instant(scheduled_due), 'scheduled execution predates its due time')
            metadata = run.get('metadata', {})
            category = run.get('error_details', {}).get('category')
            safe_categories = {'permission_denied', 'execution_failed', 'owner_unavailable', 'connection_failed',
                               'timeout', 'cancelled', 'invalid_input', 'coordinator_lost', 'resource_exhausted'}
            attempt.update(binding=binding,
                status=run.get('status') if run.get('status') in {'pending', 'running', 'success', 'failed', 'timeout', 'cancelled'} else 'invalid',
                completed=bool(run.get('completed_at')),
                error_category=category if category in safe_categories else 'absent_or_unknown',
                counters={key: value if type(value) is int and 0 <= value <= 10**9 else 'absent_or_invalid'
                          for key in ('items_scanned', 'catalog_nodes_scanned', 'fields_scanned')
                          for value in [metadata.get(key)]})
            require(all(binding.values()), 'collection execution changed its task, actor or scope')
            if run.get('status') not in {'pending', 'running'}:
                attempt['stage'] = 'terminal_state'
                require(run.get('status') == ('success' if allowed else 'failed') and run.get('completed_at'),
                        'collection execution did not reach the expected terminal state')
                attempt['stage'] = 'source_result' if allowed else 'authorization_denial'
                if allowed:
                    require(type(metadata.get('items_scanned')) is int and metadata['items_scanned'] >= 2
                            and type(metadata.get('catalog_nodes_scanned')) is int and metadata['catalog_nodes_scanned'] > 0,
                            'approved basic task did not enumerate the actual engine')
                else:
                    require(run.get('error_details', {}).get('category') == 'permission_denied'
                            and (all(metadata.get(key) in (None, 0) for key in
                                     ('items_scanned', 'catalog_nodes_scanned', 'fields_scanned')) if admitted_items is None else
                                 metadata.get('items_scanned') == admitted_items and metadata.get('catalog_nodes_scanned') == 1
                                 and metadata.get('fields_scanned') == 0), 'failed scan has an invalid authorization boundary')
                attempt['stage'] = 'task_summary'
                tasks = [row for row in self.task_rows() if row.get('id') == task]
                attempt['summary_matches'] = (len(tasks) == 1 and tasks[0].get('last_execution_id') == execution
                                              and tasks[0].get('last_execution_status') == run['status'])
                require(attempt['summary_matches'], 'task summary did not reach terminal state')
                timing = {}
                if scheduled_due is not None:
                    attempt['stage'] = 'scheduled_timing'
                    attempt['server_times'] = {}
                    for key, value in dict(next_run_at=scheduled_due,
                            **{key: run.get(key) for key in ('created_at', 'started_at', 'completed_at')}).items():
                        try:
                            collection_instant(value)
                        except SuiteError:
                            attempt['server_times'][key] = 'absent_or_invalid'
                        else:
                            attempt['server_times'][key] = value
                    require(created <= collection_instant(run.get('started_at')) <= collection_instant(run.get('completed_at')),
                            'scheduled execution has invalid server times')
                    timing = {key: run[key] for key in ('created_at', 'started_at', 'completed_at', 'trigger_type', 'source')}
                    timing.update(triggered_by=None, next_run_at=scheduled_due)
                attempt['stage'] = 'verified'
                return {'execution_id': execution, 'status': run['status'],
                        'error_category': run.get('error_details', {}).get('category'),
                        'items_scanned': metadata.get('items_scanned', 0),
                        'catalog_nodes_scanned': metadata.get('catalog_nodes_scanned', 0), **timing}
            time.sleep(1)
        raise SuiteError('collection task exceeded its bounded terminal deadline')

    def revoke(self, command):
        prior = self.row(command)
        if prior['status'] == 'active' and datetime.fromisoformat(prior['expires_at'].replace('Z', '+00:00')) > datetime.now(timezone.utc):
            try:
                self.life.operator.request('DELETE', self.route + '/' + command, (200,),
                    {'version': prior['version'], 'reason': 'Disposable Meta collection acceptance cleanup'})
            except Exception:
                # A lost DELETE response is resolved by history, never by a blind retry.
                require(self.row(command).get('status') == 'revoked', 'unknown collection revocation did not commit')
            current = self.row(command)
            require(current.get('status') == 'revoked' and current.get('version') == str(int(prior['version']) + 1)
                    and current.get('revoked_at') and current.get('revoked_by_principal_id') == self.issuer,
                    'collection revocation did not close the exact version')
        else:
            current = prior
        return current

    def source_snapshot(self):
        result = []
        for path in self.life.paths:
            for kind, recipient in sorted(self.life.recipients):
                rows = self.life.listing(history=True, catalog_path=path, recipient_type=kind, recipient_id=recipient)
                require(rows.get('total') == len(rows.get('data', [])), 'source rule history was truncated')
                result.append(rows)
                current = self.life.listing(catalog_path=path, recipient_type=kind, recipient_id=recipient)
                require(current.get('total') == len(current.get('data', [])), 'current source rules were truncated')
                # Inspection timestamps are observations, not mutable rule facts.
                result.append([{key: value for key, value in row.get('inspection', {}).items() if key != 'observed_at'}
                               for row in current['data']])
        return json.dumps(result, sort_keys=True)

    def run(self, report):
        report['phase'] = 'meta_collection_qualification'
        evidence = report['meta_collection'] = {'executions': {}, 'qualification_commands': [], 'attempts': self.attempts}
        before = self.source_snapshot()
        task = self.create_task('_primary')
        evidence['task_id'] = str(task['id'])
        facts = self.facts(task['id'])
        evidence['task_definition_digest'] = facts['task_definition_digest']
        evidence['executions']['unapproved'] = self.trigger(task['id'], False)
        report['checks'].append(COLLECTION_CHECKS[0])
        command = str(uuid.uuid4())
        body = {'command_id': command, 'task_id': str(task['id']),
                'expected_task_definition_digest': facts['task_definition_digest'],
                'purpose': 'Disposable PG basic governance qualification',
                'expires_at': (datetime.now(timezone.utc) + timedelta(minutes=10)).isoformat()}
        denied = self.life.reader.request('POST', self.route, (403,), body).payload
        require(set(denied) <= {'error', 'error_code'} and denied.get('error'), 'executor can self-approve or denial leaks data')
        self.commands[command] = body
        evidence['qualification_commands'].append(command)
        approval = evidence['approval'] = {'stage': 'operator_post', 'command_id': command}
        issued = self.observed_request(approval, 'POST', self.route, client=self.life.operator, expected=(201,), body=body)
        approval.update(stage='history_read', post_status=approval['http_status'])
        original = self.row(command, approval)
        approval.update(stage='post_history_comparison', raw_equal=issued == original,
                        semantic_equal=same_collection_row(issued, original), key_set_matches=set(issued) == set(original),
                        initial_state_matches=original.get('status') == 'active' and original.get('version') == '1',
                        differing_fields=[key for key in sorted(set(issued) | set(original))
                            if issued.get(key) != original.get(key) and key in {
                                'id', 'engine_id', 'task_id', 'task_definition_digest', 'engine_version', 'scan_depth',
                                'scope_kind', 'purpose', 'status', 'version', 'granted_at', 'expires_at',
                                'granted_by_principal_id', 'revoked_at', 'revoked_by_principal_id', 'revoked_reason'}])
        require(approval['semantic_equal'] and original.get('status') == 'active' and original.get('version') == '1',
                'approval did not persist one immutable qualification')
        approval['stage'] = 'verified'
        evidence['expires_at'] = original['expires_at']
        evidence['executions']['approved_first'] = self.trigger(task['id'], True)
        self.update(task, description='Renamed metadata only; approval must not renew')
        require(self.facts(task['id']) == facts, 'non-functional task change altered approval digest')
        evidence['executions']['approved_repeat'] = self.trigger(task['id'], True)
        require(same_collection_row(self.row(command), original), 'repeat run changed qualification or renewed its expiry')
        report['checks'].append(COLLECTION_CHECKS[1])
        copied = self.create_task('_copy')
        evidence['copy_task_id'] = str(copied['id'])
        evidence['executions']['copied'] = self.trigger(copied['id'], False)
        report['checks'].append(COLLECTION_CHECKS[2])
        self.update(task, force=True)
        require(self.facts(task['id'])['task_definition_digest'] != facts['task_definition_digest'], 'force change did not change digest')
        evidence['executions']['changed'] = self.trigger(task['id'], False)
        require(same_collection_row(self.row(command), original), 'configuration change silently replaced approval')
        report['checks'].append(COLLECTION_CHECKS[3])
        self.update(task, force=False)
        require(self.facts(task['id']) == facts, 'restored functional config differs')
        revoked = self.revoke(command)
        replay = self.life.operator.request('POST', self.route, (201,), body).payload
        require(same_collection_row(replay, revoked), 'same command revived or renewed revoked qualification')
        evidence['executions']['revoked'] = self.trigger(task['id'], False)
        report['checks'].append(COLLECTION_CHECKS[4])
        require(self.source_snapshot() == before, 'collection approval modified source Grant/Deny history')
        evidence['source_rules_unchanged'] = True
        report['checks'].append(COLLECTION_CHECKS[5])

    def run_browser(self, report):
        report['phase'] = 'meta_collection_browser'
        evidence = report['meta_collection']['browser_flow'] = {'executions': {}}
        before = self.source_snapshot()
        task = self.create_task('_browser')
        facts = self.facts(task['id'])
        evidence['task_id'] = str(task['id'])
        evidence['executions']['unapproved'] = self.trigger(task['id'], False)
        expires = (datetime.now(timezone.utc) + timedelta(minutes=10)).replace(microsecond=0).isoformat()
        with CollectionObservation(self.life.tenant, self.issuer, self.life.engine, task['id'],
                facts['task_definition_digest'], 'Disposable PG browser basic qualification', expires) as browser:
            body = browser.capture_command()
            command = body['command_id']
            require(command not in self.commands, 'browser reused an existing qualification command')
            self.commands[command] = body  # Own the UI command before allowing any POST.
            report['meta_collection']['qualification_commands'].append(command)
            evidence['command_id'] = command
            browser.release('command')
            committed = browser.committed_row()
            original = self.row(command)
            require(same_collection_row(committed, original) and original['status'] == 'active'
                    and original['version'] == '1', 'lost browser response did not commit the exact qualification')
            browser.release('committed')
            browser.wait('approved')
            require(same_collection_row(self.row(command), original), 'browser recovery changed immutable approval')
            evidence['executions']['approved_first'] = self.trigger(task['id'], True)
            evidence['executions']['approved_repeat'] = self.trigger(task['id'], True)
            self.update(task, force=True)
            require(self.facts(task['id'])['task_definition_digest'] != facts['task_definition_digest'], 'browser task force did not change digest')
            evidence['executions']['changed'] = self.trigger(task['id'], False)
            require(same_collection_row(self.row(command), original), 'UI or execution silently renewed qualification')
            self.update(task, force=False)
            require(self.facts(task['id']) == facts, 'browser task config did not restore exactly')
            browser.release('approved')
            browser.wait('revoked')
            revoked = self.row(command)
            require(revoked['status'] == 'revoked' and revoked['version'] == '2'
                    and revoked.get('revoked_by_principal_id') == self.issuer
                    and revoked.get('revoked_reason') == 'Disposable PG browser qualification withdrawal'
                    and collection_instant(revoked['expires_at']) == collection_instant(original['expires_at']),
                    'UI revocation changed expiry or failed to close exact qualification')
            evidence['executions']['revoked'] = self.trigger(task['id'], False)
            browser.release('revoked')
            evidence['browser'] = browser.finish()
        require(self.source_snapshot() == before, 'collection browser modified source Grant/Deny rules')
        evidence['source_rules_unchanged'] = True
        report['checks'].extend(COLLECTION_BROWSER_CHECKS)

    def run_expiry(self, report):
        report['phase'] = 'meta_collection_natural_expiry'
        evidence = report['meta_collection']['natural_expiry'] = {'executions': {}}
        before, credential = self.source_snapshot(), self.life.reader.token
        task = self.create_task('_expiry')
        facts = self.facts(task['id'])
        command = str(uuid.uuid4())
        body = {'command_id': command, 'task_id': str(task['id']),
                'expected_task_definition_digest': facts['task_definition_digest'],
                'purpose': 'Disposable PG basic natural qualification expiry',
                'expires_at': (datetime.now(timezone.utc) + timedelta(seconds=45)).isoformat()}
        self.commands[command] = body  # Unknown POST outcome must still be cleanable.
        report['meta_collection']['qualification_commands'].append(command)
        evidence.update(task_id=str(task['id']), command_id=command, task_definition_digest=facts['task_definition_digest'],
                        expires_at=body['expires_at'])
        issued = self.life.operator.request('POST', self.route, (201,), body).payload
        original = self.row(command)
        require(same_collection_row(issued, original) and original['status'] == 'active' and original['version'] == '1',
                'expiry fixture did not approve one exact qualification')
        expiry = collection_instant(original['expires_at'])

        def observe_times(outcome, *, before_expiry):
            run = self.life.reader.request('GET', f'/api/v1/meta/executions/{outcome["execution_id"]}', (200,)).payload
            require(run.get('execution_id') == outcome['execution_id'] and run.get('source_task_id') == str(task['id'])
                    and run.get('module') == 'meta' and run.get('task_type') == 'scan'
                    and run.get('tenant_id') == self.life.tenant and run.get('triggered_by') == int(self.life.recipient)
                    and run.get('scan_context') == {'engine_id': self.life.engine, 'scan_depth': 'basic'}
                    and run.get('status') == outcome['status'], 'expiry execution time observation changed binding')
            started, completed = collection_instant(run.get('started_at')), collection_instant(run.get('completed_at'))
            require(started <= completed and (completed < expiry if before_expiry else started >= expiry),
                    'execution did not occur on the required side of qualification expiry')
            return dict(outcome, started_at=run['started_at'], completed_at=run['completed_at'])

        evidence['executions']['before'] = observe_times(self.trigger(task['id'], True), before_expiry=True)
        require(same_collection_row(self.row(command), original), 'pre-expiry run changed qualification history')
        report['checks'].append(COLLECTION_EXPIRY_CHECKS[0])
        started_wait = time.monotonic()
        deadline = started_wait + 75
        while collection_instant(datetime.now(timezone.utc).isoformat()) < expiry:
            require(time.monotonic() < deadline, 'qualification natural expiry wait exceeded deadline')
            time.sleep(1)
        evidence['waited_ms'] = round((time.monotonic() - started_wait) * 1000, 3)
        require(self.life.reader.token == credential and self.facts(task['id']) == facts,
                'natural qualification expiry changed credential or task facts')
        require(same_collection_row(self.row(command), original), 'manual revocation or renewal replaced natural expiry')
        replay = self.life.operator.request('POST', self.route, (201,), body).payload
        rows = [row for row in self.qualification_rows() if row.get('task_id') == str(task['id'])]
        require(same_collection_row(replay, original) and len(rows) == 1 and same_collection_row(rows[0], original)
                and same_collection_row(self.row(command), original),
                'expired approval replay renewed or altered history')
        # Run only after historical replay, so an accidental revival cannot hide
        # behind a denial observed before the replay.
        evidence['executions']['after'] = observe_times(self.trigger(task['id'], False), before_expiry=False)
        require(self.life.reader.token == credential, 'expiry denial used another credential')
        report['checks'].append(COLLECTION_EXPIRY_CHECKS[1])
        require(self.source_snapshot() == before, 'qualification expiry changed source Grant/Deny rules')
        evidence.update(same_credential=True, history_unchanged=True, source_rules_unchanged=True,
                        stored_status=original['status'], version=original['version'], no_revocation=True)
        report['checks'].append(COLLECTION_EXPIRY_CHECKS[2])

    def scheduled_rows(self, task):
        result = self.life.reader.request('GET', f'/api/v1/meta/scan/runs?task_id={task}&page_size=100', (200,)).payload
        rows = result.get('items')
        require(isinstance(rows, list) and type(result.get('total')) is int and result['total'] == len(rows)
                and len(rows) <= 100 and all(row.get('source_task_id') == str(task) for row in rows),
                'truncated or foreign scheduled executions')
        ids = [row.get('execution_id') for row in rows]
        require(all(isinstance(value, str) and str(uuid.UUID(value)) == value for value in ids)
                and len(set(ids)) == len(ids), 'invalid or duplicate scheduled execution identity')
        return rows

    def pause_schedule(self, owned):
        # Update owns its attempted body before sending. A lost response must
        # resolve only this exact task; never ignore a still-enabled schedule.
        try:
            self.update(owned, enabled=False)
        except Exception:
            self.recover_tasks()
        rows = [row for row in self.task_rows() if row.get('id') == owned['id']]
        require(len(rows) == 1 and rows[0].get('enabled') is False
                and rows[0].get('schedule') == owned['body']['schedule'], 'collection schedule did not stop')
        owned['body']['enabled'] = False

    def scheduled_run(self, owned, allowed, evidence, label):
        task = owned['id']
        known = {run for run, owner in self.executions.items() if owner == task}
        require({row['execution_id'] for row in self.scheduled_rows(task)} == known,
                'unexpected execution outside scheduled phase')
        attempt = {'task_id': str(task), 'expected_terminal': 'success' if allowed else 'failed',
                   'stage': 'schedule_enable', 'trigger_type': 'scheduled'}
        self.attempts.append(attempt)
        self.update(owned, schedule='* * * * *', enabled=True)
        rows = [row for row in self.task_rows() if row.get('id') == task]
        require(len(rows) == 1 and rows[0].get('enabled') is True and rows[0].get('schedule') == '* * * * *',
                'exact collection schedule was not enabled')
        due = rows[0].get('next_run_at')
        require(collection_instant(due) > collection_instant(rows[0].get('updated_at')), 'schedule has no future due time')
        attempt.update(stage='schedule_wait', next_run_at=due)
        deadline = time.monotonic() + 150  # One minute to due + the unchanged one-minute owner poll.
        while time.monotonic() < deadline:
            rows = self.scheduled_rows(task)
            observed = {row['execution_id'] for row in rows}
            require(known <= observed, 'scheduled history lost a prior execution')
            new = observed - known
            if new:
                require(len(new) == 1, 'multiple scheduled executions in one isolated phase')
                execution = new.pop()
                self.executions[execution] = task
                attempt.update(execution_id=execution, stage='schedule_pause')
                self.pause_schedule(owned)
                evidence['executions'][label] = self.observe_execution(task, execution, allowed, attempt, scheduled_due=due)
                require({row['execution_id'] for row in self.scheduled_rows(task)} == known | {execution},
                        'schedule produced another execution after pause')
                return
            time.sleep(1)
        raise SuiteError('scheduled collection exceeded its bounded creation deadline')

    def run_scheduled(self, report):
        report['phase'] = 'meta_collection_scheduled'
        evidence = report['meta_collection']['scheduled'] = {'executions': {}}
        before, credential = self.source_snapshot(), self.life.reader.token
        task = self.create_task('_scheduled')
        facts = self.facts(task['id'])
        evidence.update(task_id=str(task['id']), task_definition_digest=facts['task_definition_digest'])
        self.scheduled_run(task, False, evidence, 'unapproved')
        require(self.facts(task['id']) == facts, 'schedule changed functional task facts')
        report['checks'].append(COLLECTION_SCHEDULED_CHECKS[0])
        command = str(uuid.uuid4())
        body = {'command_id': command, 'task_id': str(task['id']),
                'expected_task_definition_digest': facts['task_definition_digest'],
                'purpose': 'Disposable PG basic scheduled qualification',
                'expires_at': (datetime.now(timezone.utc) + timedelta(minutes=10)).isoformat()}
        self.commands[command] = body
        report['meta_collection']['qualification_commands'].append(command)
        evidence['command_id'] = command
        issued = self.life.operator.request('POST', self.route, (201,), body).payload
        original = self.row(command)
        require(same_collection_row(issued, original) and original['status'] == 'active' and original['version'] == '1',
                'scheduled approval did not persist exact qualification')
        evidence['expires_at'] = original['expires_at']
        for label in ('approved_first', 'approved_repeat'):
            self.scheduled_run(task, True, evidence, label)
            require(self.facts(task['id']) == facts and same_collection_row(self.row(command), original),
                    'scheduled repeat changed task facts or qualification history')
        report['checks'].append(COLLECTION_SCHEDULED_CHECKS[1])
        revoked = self.revoke(command)
        replay = self.life.operator.request('POST', self.route, (201,), body).payload
        require(same_collection_row(replay, revoked), 'scheduled replay revived revoked qualification')
        self.scheduled_run(task, False, evidence, 'revoked')
        require(same_collection_row(self.row(command), revoked) and self.facts(task['id']) == facts,
                'revoked scheduled execution changed task facts or history')
        report['checks'].append(COLLECTION_SCHEDULED_CHECKS[2])
        require(self.life.reader.token == credential and self.source_snapshot() == before,
                'scheduled collection changed credential or source Grant/Deny rules')
        evidence.update(same_credential=True, history_unchanged=True, source_rules_unchanged=True, schedule_disabled=True)
        report['checks'].append(COLLECTION_SCHEDULED_CHECKS[3])

    def run_inflight(self, report):
        self._run_inflight(report, natural_expiry=False)

    def run_inflight_expiry(self, report):
        self._run_inflight(report, natural_expiry=True)

    def run_probe_control(self, report, controller):
        report['phase'] = 'pg_connection_probe_control'
        evidence = report['meta_collection']['connection_probe_control'] = {'stage': 'identity'}
        API.validate_user_identity(controller, self.life.tenant, {'system.engine.execute', 'system.engine.read'})
        context = controller.request('GET', '/api/v1/system/auth/context', (200,)).payload
        roles = tenant_functional_assignments(context, self.life.tenant)
        principal = context['principal']['id']
        require(context['principal']['type'] == 'user' and context['token']['type'] == 'first_party_access_token'
                and context['context'].get('type') == 'tenant'
                and context['context'].get('tenant_id') == str(self.life.tenant)
                and {role['role_key'] for role in roles} == {'tenant.infrastructure_administrator'}
                and str(principal) not in {str(value) for value in report['principals'].values()},
                'connection probe requires the separate existing fixture infrastructure User')
        credential = controller.token
        path = f'/api/v1/system/engines/{self.life.engine}'
        engine = controller.request('GET', path, (200,)).payload
        connection = engine.get('connection_info')
        require(engine.get('id') == self.life.engine and engine.get('tenant_id') == self.life.tenant
                and engine.get('engine_type') == 'postgresql' and type(engine.get('version')) is int
                and engine['version'] > 0 and isinstance(connection, dict)
                and all(connection.get(key) == value for key, value in {'host': '127.0.0.1', 'port': 55433,
                    'database': 'metric_fixture', 'user': 'metric_reader'}.items()),
                'connection probe target differs from the registered disposable PG source')
        fields = ('id', 'tenant_id', 'engine_type', 'version', 'connection_info', 'capabilities', 'lifecycle_state')
        binding = {key: engine.get(key) for key in fields}
        rules = self.source_snapshot()
        evidence.update(principal_id=str(principal), tenant_id=str(self.life.tenant), engine_id=self.life.engine,
                        engine_version=engine['version'], role='tenant.infrastructure_administrator')
        barrier = PGCollectionReadBarrier()
        self.source_barriers.append(barrier)
        barrier.admit()  # Observe only: do not create a schema or hold a source lock.
        barrier.begin_observation()
        baseline = evidence['source_counter_baseline'] = barrier.counts()
        evidence.update(stage='probe_request', started_at=datetime.now(timezone.utc).isoformat())
        try:
            response = controller.request('POST', path + '/test', (200,))
            evidence.update(http_status=response.status, success=response.payload.get('success') is True)
        finally:
            evidence['completed_at'] = datetime.now(timezone.utc).isoformat()
            observed = evidence['source_counter_observed'] = barrier.counts()
            evidence['connection_observation'] = barrier.connection_observation(baseline, observed)
        require(response.payload.get('success') is True, 'formal connection probe did not succeed')
        delta = {key: observed['calls'].get(key, 0) - baseline['calls'].get(key, 0)
                 for key in ('namespaces', 'tables', 'workspace_detection', 'other')}
        evidence.update(stage='delta_check', source_call_delta=delta)
        before = {(row['query_id'], row['top_level']): row for row in baseline['other_statements']}
        after = {(row['query_id'], row['top_level']): row for row in observed['other_statements']}
        increments = []
        require(before.keys() <= after.keys(), 'connection probe statistics lost a prior statement')
        for key, row in after.items():
            prior = before.get(key)
            change = row['calls'] - (prior['calls'] if prior else 0)
            require(change >= 0 and (prior is None or prior['kind'] == row['kind']),
                    'connection probe statement counters or classification changed')
            if change:
                increments.append(dict(row, calls=change))
        evidence['statement_increments'] = increments
        require(observed['reset_at'] == baseline['reset_at']
                and delta == {'namespaces': 0, 'tables': 0, 'workspace_detection': 0, 'other': 1}
                and len(increments) == 1 and increments[0]['kind'] == 'version_probe'
                and increments[0]['top_level'] is True and increments[0]['calls'] == 1,
                'connection probe did not uniquely produce one version-probe other increment')
        require(evidence['connection_observation']['verified'] is True
                and evidence['connection_observation']['by_module'].get('system', {}).get('other') == 1,
                'connection probe source connection attribution is incomplete')
        connection = evidence['connection_observation']
        events = connection['events']
        require(connection['protocol_empty_calls'] == {'system': 1} and len(events) == 2
                and events[0]['kind'] == 'empty_query' and events[1]['kind'] == 'version_probe'
                and all(row['module'] == 'system' for row in events)
                and (events[0]['pid'], events[0]['session_id']) == (events[1]['pid'], events[1]['session_id'])
                and events[1]['query_id'] == increments[0]['query_id'],
                'connection probe must contain one empty Ping and one version SQL from the same connection')
        current = controller.request('GET', path, (200,)).payload
        require({key: current.get(key) for key in fields} == binding
                and controller.request('GET', '/api/v1/system/auth/context', (200,)).payload == context
                and controller.token == credential and self.source_snapshot() == rules,
                'connection probe changed engine configuration, identity or source rules')
        barrier.close()
        evidence.update(stage='verified', source_rules_unchanged=True, engine_unchanged=True,
                        identity_unchanged=True, source_fixture_cleanup='zero_source_locks_and_schema')
        report['checks'].append('pg_connection_probe_other_increment_identified')

    def _run_inflight(self, report, *, natural_expiry):
        key = 'inflight_expiry' if natural_expiry else 'inflight'
        checks = COLLECTION_INFLIGHT_EXPIRY_CHECKS if natural_expiry else COLLECTION_INFLIGHT_CHECKS
        report['phase'] = 'meta_collection_' + key
        evidence = report['meta_collection'][key] = {'executions': {}}
        before, credential = self.source_snapshot(), self.life.reader.token
        task = self.create_task('_' + key)
        facts = self.facts(task['id'])
        command = str(uuid.uuid4())
        body = {'command_id': command, 'task_id': str(task['id']),
                'expected_task_definition_digest': facts['task_definition_digest'],
                'purpose': 'Disposable PG basic in-flight qualification ' + ('expiry' if natural_expiry else 'withdrawal'),
                'expires_at': (datetime.now(timezone.utc) + timedelta(seconds=12 if natural_expiry else 300)).isoformat()}
        self.commands[command] = body
        report['meta_collection']['qualification_commands'].append(command)
        evidence.update(task_id=str(task['id']), command_id=command, task_definition_digest=facts['task_definition_digest'])
        issued = self.life.operator.request('POST', self.route, (201,), body).payload
        original = self.row(command)
        require(same_collection_row(issued, original) and original['status'] == 'active' and original['version'] == '1',
                'in-flight fixture did not approve exact qualification')
        barrier = PGCollectionReadBarrier()
        self.source_barriers.append(barrier)  # Own native resources before any creation or lock request.
        evidence['lock'] = barrier.open()
        execution, attempt = self.start_execution(task['id'], False)
        attempt['denial_boundary'] = 'after_admitted_catalog'
        evidence['blocked'] = barrier.wait_blocked()
        running = self.life.reader.request('GET', f'/api/v1/meta/executions/{execution}', (200,)).payload
        require(running.get('execution_id') == execution and running.get('source_task_id') == str(task['id'])
                and running.get('tenant_id') == self.life.tenant and running.get('module') == 'meta'
                and running.get('task_type') == 'scan' and running.get('triggered_by') == int(self.life.recipient)
                and running.get('scan_context') == {'engine_id': self.life.engine, 'scan_depth': 'basic'}
                and running.get('status') == 'running' and running.get('started_at') and not running.get('completed_at'),
                'source barrier has no exact running Worker execution')
        evidence['running_verified'] = True
        expiry = collection_instant(original['expires_at'])
        require(collection_instant(running['started_at']) <= collection_instant(evidence['blocked']['started_at'])
                <= collection_instant(evidence['blocked']['observed_at']) < expiry,
                'source enumeration was not admitted before qualification expiry')
        if natural_expiry:
            evidence['expires_at'] = original['expires_at']
            waiting, deadline = time.monotonic(), time.monotonic() + 20
            while collection_instant(datetime.now(timezone.utc).isoformat()) < expiry:
                require(time.monotonic() < deadline, 'in-flight natural expiry wait exceeded deadline')
                time.sleep(.1)
            # Re-observe the SAME in-flight source after the original deadline;
            # a local clock or a lock session that timed out is not proof.
            after = barrier.wait_blocked()
            require(all(after.get(k) == evidence['blocked'].get(k) for k in ('pid', 'blocker_pid'))
                    and collection_instant(after['started_at']) == collection_instant(evidence['blocked']['started_at'])
                    and collection_instant(after['observed_at']) >= expiry,
                    'original enumeration did not remain blocked across natural expiry')
            evidence.update(blocked_after_expiry=after, waited_ms=round((time.monotonic() - waiting) * 1000, 3))
            terminal_history = original
            require(same_collection_row(self.row(command), original), 'natural expiry changed qualification history')
        else:
            terminal_history = self.revoke(command)
            require(terminal_history['status'] == 'revoked' and terminal_history['version'] == '2'
                    and collection_instant(evidence['blocked']['observed_at']) <= collection_instant(terminal_history['revoked_at']) < expiry,
                    'in-flight revocation did not precede release or expired instead')
            evidence['revoked_at'] = terminal_history['revoked_at']
        barrier.release()
        admitted = report['meta_collection']['scheduled']['executions']['approved_repeat']['items_scanned']
        require(type(admitted) is int and admitted > 0, 'missing prior successful source enumeration count')
        evidence['executions']['inflight'] = self.observe_execution(task['id'], execution, False, attempt, admitted_items=admitted)
        # Keep bounded observations before assertions: an incomplete report
        # must distinguish transport, reset and unexpected native source I/O.
        evidence['source_counter_stage'] = 'query'
        evidence['source_counter_baseline'] = barrier.baseline
        counts = barrier.counts()
        evidence['source_counter_observed'] = counts
        delta = {key: counts['calls'].get(key, 0) - barrier.baseline['calls'].get(key, 0)
                 for key in ('namespaces', 'tables', 'workspace_detection', 'other')}
        evidence.update(source_counter_stage='reset_check', source_call_delta=delta)
        require(counts['reset_at'] == barrier.baseline['reset_at'], 'source statistics were reset during in-flight denial')
        evidence['connection_observation'] = barrier.connection_observation(barrier.baseline, counts, evidence['blocked'])
        evidence['source_counter_stage'] = 'delta_check'
        require(delta == {'namespaces': 1, 'tables': 1, 'workspace_detection': 2, 'other': 0},
                'source opened another Catalog read beyond the admitted enumeration')
        require(evidence['connection_observation']['verified'] is True,
                'in-flight source connection attribution is incomplete')
        evidence.update(source_counter_stage='verified', next_catalog_denied=True, admitted_enumeration_finished=True)
        report['checks'].append(checks[0])
        if natural_expiry:
            replay = self.life.operator.request('POST', self.route, (201,), body).payload
            rows = [row for row in self.qualification_rows() if row.get('task_id') == str(task['id'])]
            require(same_collection_row(replay, original) and len(rows) == 1 and same_collection_row(rows[0], original),
                    'in-flight expired replay renewed or changed qualification')
            evidence.update(stored_status=original['status'], version=original['version'], no_revocation=True)
        evidence['executions']['expired_repeat' if natural_expiry else 'revoked_repeat'] = self.trigger(task['id'], False)
        evidence.update(repeat_source_counter_stage='query', repeat_source_counter_available=False)
        repeated = barrier.counts()
        evidence.update(repeat_source_counter_observed=repeated, repeat_source_counter_available=True,
                        repeat_source_counter_stage='equality_check')
        evidence['repeat_source_call_delta'] = {
            key: repeated['calls'].get(key, 0) - counts['calls'].get(key, 0)
            for key in ('namespaces', 'tables', 'workspace_detection', 'other')}
        prior_rows = {(row['query_id'], row['top_level']): row for row in counts['other_statements']}
        repeated_rows = {(row['query_id'], row['top_level']): row for row in repeated['other_statements']}
        evidence['repeat_other_statement_delta'] = [
            {'query_id': key[0], 'top_level': key[1], 'before': prior_rows.get(key), 'after': repeated_rows.get(key),
             'calls_delta': (repeated_rows[key]['calls'] if key in repeated_rows else 0)
                           - (prior_rows[key]['calls'] if key in prior_rows else 0)}
            for key in sorted(prior_rows.keys() | repeated_rows.keys())
            if prior_rows.get(key) != repeated_rows.get(key)]
        evidence['repeat_connection_observation'] = barrier.connection_observation(counts, repeated)
        require(repeated == counts, 'denied repeat touched source or altered statistics')
        require(evidence['repeat_connection_observation']['verified'] is True,
                'denied repeat source connection attribution is incomplete')
        evidence['repeat_source_counter_stage'] = 'verified'
        report['checks'].append(checks[1])
        barrier.close()
        require(barrier.closed and barrier.released and self.facts(task['id']) == facts
                and same_collection_row(self.row(command), terminal_history) and self.life.reader.token == credential
                and self.source_snapshot() == before, 'in-flight cleanup changed source rules, facts or credential')
        evidence.update(source_fixture_cleanup='zero_source_locks_and_schema', source_rules_unchanged=True,
                        same_credential=True, history_unchanged=True)
        report['checks'].append(checks[2])

    def cleanup(self):
        errors = []
        try:
            rows = self.recover_tasks()
            for owned in self.tasks.values():
                if owned['id'] is not None and owned['body'].get('schedule'):
                    if any(row.get('id') == owned['id'] for row in rows):
                        self.pause_schedule(owned)
        except Exception:
            errors.append('collection schedule cleanup failed')
        for command in self.commands:
            try:
                rows = [row for row in self.qualification_rows() if row.get('id') == command]
                if rows:
                    self.revoke(command)
            except Exception:
                errors.append('collection qualification cleanup failed')
        for barrier in self.source_barriers:
            try:
                barrier.release()
            except Exception:
                errors.append('collection source lock release failed')
        try:
            rows = self.recover_tasks()
        except Exception:
            rows = []
            errors.append('collection task recovery failed')
        settled = True
        for owned in self.tasks.values():
            try:
                task = owned['id']
                if task is None:
                    continue
                current_tasks = [row for row in self.task_rows() if row.get('id') == task]
                require(not owned['body'].get('schedule') or not current_tasks or current_tasks[0].get('enabled') is False,
                        'refusing to delete a still-enabled collection schedule')
                qualifications = [row for row in self.qualification_rows() if row.get('task_id') == str(task)]
                require(all(row['id'] in self.commands for row in qualifications), 'foreign qualification on owned task')
                require(not any(row['status'] == 'active'
                                and datetime.fromisoformat(row['expires_at'].replace('Z', '+00:00')) > datetime.now(timezone.utc)
                                for row in qualifications), 'current collection qualification remains')
                deadline = time.monotonic() + 120
                while True:
                    runs = self.life.reader.request('GET', f'/api/v1/meta/scan/runs?task_id={task}&page_size=100', (200,)).payload
                    require(runs.get('total') == len(runs.get('items', []))
                            and all(row.get('source_task_id') == str(task) for row in runs['items']), 'truncated or foreign collection runs')
                    if not any(row.get('status') in {'pending', 'running'} for row in runs['items']):
                        break
                    require(time.monotonic() < deadline, 'collection cleanup left active executions')
                    time.sleep(1)
                if any(row.get('id') == task for row in rows):
                    try:
                        self.life.operator.request('DELETE', f'/api/v1/meta/scan/tasks/{task}', (200,))
                    except Exception:
                        require(not any(row.get('id') == task for row in self.task_rows()), 'unknown task deletion did not commit')
                require(not any(row.get('id') == task for row in self.task_rows()), 'collection task remains after cleanup')
            except Exception:
                settled = False
                errors.append('collection task cleanup or residual verification failed')
        if settled:
            for barrier in self.source_barriers:
                try:
                    barrier.close()
                except Exception:
                    errors.append('collection source fixture cleanup failed')
        require(not errors, '; '.join(errors))


class Lifecycle:
    def __init__(self, operator, reader, tenant, engine, recipient, organizations, outsider,
                 organization_operator, login, username, password):
        self.operator, self.reader, self.tenant, self.engine, self.recipient = operator, reader, tenant, engine, recipient
        self.organizations, self.outsider = organizations, outsider
        self.organization_operator, self.login = organization_operator, login
        require(username == 'external-online-consumer' and bool(password), 'dedicated reader login required')
        self.username, self.password = username, password
        self.path = table_path(engine, 'metric_people')
        self.paths = [self.path]
        self.base = f'/api/v1/system/engines/{engine}'
        self.grants, self.denies = [], []
        self.recipients = {('user', recipient)}
        self.source_observations = []
        self.expiry_grant = None
        self.production_tasks = {}
        self.collection = None

    def collect_production_grants(self):
        # Resolve even an unknown start response using only executions of our
        # captured task definition, never an arbitrary output Grant or prefix.
        for task, path in self.production_tasks.items():
            executions = self.reader.request('GET', f'/api/v1/transfer/task-definitions/{task}/executions?page_size=100', (200,)).payload
            require(executions.get('total') == len(executions.get('data', [])), 'production history was truncated')
            ids = {row['execution_id'] for row in executions['data'] if row.get('task_id') == task}
            rows = self.listing(history=True, catalog_path=path)
            require(rows.get('total') == len(rows.get('data', [])), 'production Grant history was truncated')
            for row in rows['data']:
                require(row.get('approval_mode') == 'task_production' and row.get('producer_owner') == 'transfer'
                        and row.get('production_execution_id') in ids and row.get('catalog_path') == path
                        and row.get('recipient_type') == 'user' and row.get('recipient_id') == self.recipient,
                        'refusing to own a foreign output Grant')
                if row['request_id'] not in self.grants:
                    self.grants.append(row['request_id'])

    def production_flow(self, report):
        report['phase'] = 'initial_output_authorization'
        source = item(self.reader, self.engine, 'metric_people')
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        source_grant, source_body = self.grant(facts, seconds=600)
        name = 'pg_initial_' + uuid.uuid4().hex
        path = table_path(self.engine, name)
        self.paths.append(path)  # Register the exact cleanup target before any task write.
        existing = self.reader.request('GET', f'/api/v1/meta/engines/{self.engine}/items?branch=public',
                                       (200,), response_type=list).payload
        require(not any(row.get('full_name') == 'public.' + name for row in existing), 'output already exists in Meta')
        task = self.reader.request('POST', '/api/v1/transfer/task-definitions', (201,),
                                   production_task(self.engine, source, name)).payload
        task_id = task.get('id')
        require(type(task_id) is int and task_id > 0, 'production task identity missing')
        self.production_tasks[task_id] = path
        started = self.reader.request('POST', f'/api/v1/transfer/task-definitions/{task_id}/start', (200,)).payload
        execution = started.get('execution_id')
        require(str(uuid.UUID(execution)) == execution, 'production execution identity missing')
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            current = self.reader.request('GET', f'/api/v1/transfer/executions/{execution}', (200,)).payload
            require(current.get('execution_id') == execution and current.get('task_id') == task_id,
                    'production execution binding changed')
            require(current.get('status') in {'pending', 'running', 'success'}, 'physical production failed')
            state = current.get('metadata', {}).get('initial_authorization')
            if current['status'] == 'success':
                require(current.get('records_read') == 2 and current.get('records_written') == 2,
                        'production did not write the exact two rows')
                require(isinstance(state, dict) and set(state) <= {'status', 'code'}
                        and state.get('status') in {'pending', 'issued'}, 'initial authorization rejected or leaked private state')
                if state['status'] == 'issued':
                    break
            time.sleep(1)
        else:
            raise SuiteError('initial production authorization exceeded its deadline')
        self.collect_production_grants()
        rows = self.listing(history=True, state='active', catalog_path=path)
        require(rows.get('total') == 1 and len(rows.get('data', [])) == 1, 'initial output needs exactly one active Grant')
        row = rows['data'][0]
        output = item(self.reader, self.engine, name)
        output_facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, path)
        validate_production_grant(row, execution=execution, path=path, recipient=self.recipient,
                                  facts=output_facts, source_expiry=source_body['expires_at'])
        scans = self.reader.request('GET', '/api/v1/meta/scan/runs?page_size=100', (200,)).payload
        require(scans.get('total') == len(scans.get('items', [])), 'basic scan history was truncated')
        children = [scan for scan in scans['items'] if scan.get('parent_execution_id') == execution]
        require(len(children) == 1 and children[0].get('status') == 'success'
                and children[0].get('source') == 'transfer' and children[0].get('triggered_by') == int(self.recipient)
                and children[0].get('scan_context') == {'engine_id': self.engine, 'scan_depth': 'basic'}
                and children[0].get('metadata', {}).get('items_scanned') == 1
                and children[0].get('metadata', {}).get('catalog_nodes_scanned') == 0,
                'output was not identified by one exact basic-only child scan')
        target = locator(self.engine, name, output['id'])
        preview(self.reader, target, True, 2)
        preview(self.outsider, target, False)
        report['phase'] = 'concurrent_and_batch_read_load'
        self.measure_read_load(report, [(self.path, facts, source_grant),
                                       (path, output_facts, row['request_id'])])
        report['phase'] = 'output_source_withdrawal'
        self.revoke(source_grant)
        self.require_batch_denied(self.reader, [self.path, path])
        report['authorization_load']['mixed_withdrawn_batch_denied'] = True
        preview(self.reader, locator(self.engine, 'metric_people', source['id']), False)
        preview(self.reader, target, True, 2)
        require(self.listing(history=True, state='active', catalog_path=path) == rows,
                'source withdrawal changed the output Grant')
        self.revoke(row['request_id'])
        preview(self.reader, target, False)
        revoked = self.listing(history=True, state='revoked', catalog_path=path)
        require(revoked['total'] == 1 and revoked['data'][0]['request_id'] == row['request_id']
                and revoked['data'][0].get('revocation'), 'output revocation history missing')
        report['initial_production'] = {'task_id': task_id, 'execution_id': execution,
            'grant_id': row['request_id'], 'item_fingerprint': output_facts['item_fingerprint'],
            'boundary_hash': output_facts['boundary_hash'], 'scan_execution_id': children[0]['execution_id'],
            'scan_depth': 'basic', 'rows': 2, 'inherited_expiry': row['expires_at'],
            'source_withdrawn_output_readable': True, 'output_withdrawn_read_denied': True}
        report['checks'].extend(PRODUCTION_CHECKS)

    def listing(self, *, history=False, state=None, recipient_type='user', recipient_id=None, catalog_path=None):
        query = {'catalog_path': json.dumps(self.path if catalog_path is None else catalog_path, separators=(',', ':')),
                 'recipient_type': recipient_type, 'recipient_id': self.recipient if recipient_id is None else recipient_id,
                 'page_size': 100}
        if state:
            query['state'] = state
        return self.operator.request('GET', self.base + '/access_grants' + ('/history' if history else '')
                                     + '?' + urlencode(query), (200,)).payload

    def grant_command(self, facts, *, seconds=300, recipient_type='user', recipient_id=None,
                      catalog_path=None, permission=None):
        path = self.path if catalog_path is None else catalog_path
        permission = permission if permission is not None else REGISTRATION.ensure_direct_permission(self.operator, path, facts)
        command = str(uuid.uuid4())
        expires = datetime.now(timezone.utc) + timedelta(seconds=seconds)
        body = {'request_id': command, 'catalog_path': self.path if catalog_path is None else catalog_path,
                'item_fingerprint': facts['item_fingerprint'],
                'expected_boundary_hash': facts['boundary_hash'], 'direct_permission_source': permission['source'],
                'direct_permission_version': permission['version'], 'recipient_type': recipient_type,
                'recipient_id': self.recipient if recipient_id is None else recipient_id,
                'action': 'read', 'expiry_mode': 'at_time', 'expires_at': expires.isoformat(),
                'reason': 'Disposable PG source authorization lifecycle'}
        self.grants.append(command)  # Track before the write, including unknown outcomes.
        self.recipients.add((body['recipient_type'], body['recipient_id']))
        return command, body

    def grant(self, facts, **options):
        command, body = self.grant_command(facts, **options)
        receipt = self.operator.request('POST', self.base + '/access_grants', (201,), body).payload
        for key in ('request_id', 'catalog_path', 'item_fingerprint', 'direct_permission_source', 'direct_permission_version',
                    'recipient_type', 'recipient_id', 'action', 'expiry_mode'):
            require(receipt.get(key) == body[key], 'Grant receipt changed its bound target or recipient')
        require(receipt.get('boundary_hash') == facts['boundary_hash'] and receipt.get('revocation') is None,
                'Grant receipt changed its approved boundary')
        require(datetime.fromisoformat(receipt['expires_at'].replace('Z', '+00:00'))
                == datetime.fromisoformat(body['expires_at']), 'Grant expiry changed')
        return command, body

    def revoke(self, command):
        self.operator.request('POST', self.base + f'/access_grants/{command}/revoke', (200,),
                              {'reason': 'Disposable PG lifecycle source withdrawal'})

    def require_batch_denied(self, client, targets):
        response = client.request('POST', READ_CHECK, (403,), {'targets': targets})
        require(response.status == 403 and set(response.payload) == {'error'}
                and isinstance(response.payload.get('error'), str) and bool(response.payload['error']),
                'batch denial must not expose partial data or substitute another boundary')

    def measure_read_load(self, report, bindings):
        observation = {}
        report['authorization_load'] = {'result': 'failed', 'audit_observer_enabled': True,
                                        'audit_observation': observation}
        with AuditLatencyObservation(observation, self.recipient, phase='load') as observer:
            self.sample_read_load(report, bindings, observer, observation)
        require(observation.get('result') == 'passed' and observation.get('connection_closed') is True,
                'read load observation did not finish cleanly')
        result = report['authorization_load']
        verify_load_audit_windows(result, observation['audit_rows'])
        result['result'] = 'passed'

    def sample_read_load(self, report, bindings, observer, observation):
        """Bounded closed-loop HTTP samples, using only already-issued fixture Grants."""
        result = {'schema_version': 'addp.authorization-load/v1', 'result': 'failed',
                  'measurement': 'gateway_client_http_and_json_roundtrip', 'unit': 'ms',
                  'principal_id': self.recipient, 'same_principal': True,
                  'endpoint': READ_CHECK, 'quantile_method': 'nearest_rank',
                  'budget_seconds': 90, 'request_timeout_seconds': self.reader.timeout,
                  'audit_observer_enabled': True, 'audit_observation': observation, 'round_count': LOAD_ROUNDS,
                  'samples_per_round': LOAD_SAMPLES, 'warmups_per_round': LOAD_WARMUPS,
                  'targets': [{'catalog_path': path, 'item_fingerprint': facts['item_fingerprint'],
                               'boundary_hash': facts['boundary_hash'], 'grant_id': command}
                              for path, facts, command in bindings], 'cells': []}
        report['authorization_load'] = result
        require(len(bindings) == 2 and bindings[0][0] != bindings[1][0]
                and bindings[0][1]['item_fingerprint'] != bindings[1][1]['item_fingerprint'],
                'load comparison requires two distinct DataItems')
        deadline = time.monotonic() + 90
        tokens = (self.reader.token, self.operator.token, self.outsider.token)

        def guard():
            observer.healthy()
            require(time.monotonic() < deadline, 'read load exceeded its budget')
            require((self.reader.token, self.operator.token, self.outsider.token) == tokens,
                    'read load credentials changed')

        def snapshot():
            guard()
            principal, _ = identity(self.reader, self.tenant, READER_PERMISSIONS)
            require(principal == self.recipient, 'read load principal changed')
            context = self.reader.request('GET', '/api/v1/system/auth/context', (200,)).payload
            observations = [context]
            for path, facts, command in bindings:
                current = REGISTRATION.read_exact_record_authorization_facts(self.operator, path)
                require(all(current.get(key) == facts.get(key) for key in ('item_fingerprint', 'boundary_hash')),
                        'read load Meta boundary changed')
                history = self.listing(history=True, state='active', catalog_path=path)
                require(history.get('total') == 1 and len(history.get('data', [])) == 1,
                        'read load requires exactly its original Grant per target')
                grant = history['data'][0]
                require(all(grant.get(key) == value for key, value in {
                    'request_id': command, 'catalog_path': path, 'recipient_type': 'user',
                    'recipient_id': self.recipient, 'action': 'read', 'revocation': None,
                    'item_fingerprint': facts['item_fingerprint'], 'boundary_hash': facts['boundary_hash']}.items()),
                    'read load Grant binding changed')
                observations += [history, {key: value for key, value in current.items() if key != 'observed_at'}]
            guard()
            return json.dumps(observations, sort_keys=True)

        baseline = snapshot()
        targets = [path for path, _, _ in bindings]
        self.require_batch_denied(self.outsider, targets)
        result['outsider_batch_denied'] = True
        modes = [('single_target', [[targets[0]]]), ('two_target_batch', [targets]),
                 ('two_target_separate', [[targets[0]], [targets[1]]])]
        for mode, groups in modes:
            for concurrency in ((1,) if mode == 'two_target_separate' else (1, 5, 10)):
                require(snapshot() == baseline, 'read load control drifted before a cell')
                cell = {'mode': mode, 'concurrency_limit': concurrency, 'target_count': len({
                    json.dumps(path, sort_keys=True) for group in groups for path in group}),
                    'http_requests_per_operation': len(groups), 'rounds': [], 'failed_count': 0}
                result['cells'].append(cell)
                for round_number in range(1, LOAD_ROUNDS + 1):
                    row = {'round': round_number, 'requests': [], 'http_attempted_count': 0,
                           'peak_in_flight': 0, 'failed_count': 0}
                    cell['rounds'].append(row)
                    stop, lock = threading.Event(), threading.Lock()
                    active = 0

                    def operation(index, warmup):
                        nonlocal active
                        if stop.is_set():
                            return
                        began = time.perf_counter_ns()
                        wall_start = time.time_ns()
                        entered = False
                        try:
                            guard()
                            with lock:
                                active += 1
                                entered = True
                                row['peak_in_flight'] = max(row['peak_in_flight'], active)
                            for group in groups:
                                guard()
                                if stop.is_set():
                                    return
                                with lock:
                                    row['http_attempted_count'] += 1
                                response = self.reader.request('POST', READ_CHECK, (200,), {'targets': group})
                                payload = response.payload
                                require(response.status == 200 and set(payload) == {'observed_at'},
                                        'invalid read load response shape')
                                observed = datetime.fromisoformat(payload['observed_at'].replace('Z', '+00:00'))
                                require(observed.tzinfo is not None and observed.year > 1970,
                                        'invalid read load observation time')
                            guard()
                            sample = {'index': index, 'warmup': warmup, 'started_unix_ns': wall_start,
                                      'finished_unix_ns': time.time_ns(),
                                      'duration_ms': (time.perf_counter_ns() - began) / 1000000}
                            with lock:
                                row['requests'].append(sample)
                        except Exception:
                            stop.set()
                            with lock:
                                row['failed_count'] += 1
                        finally:
                            if entered:
                                with lock:
                                    active -= 1

                    # Drain every in-flight request before any control read or rule cleanup.
                    with ThreadPoolExecutor(max_workers=concurrency) as pool:
                        list(pool.map(lambda index: operation(index, True), range(LOAD_WARMUPS)))
                        measured_start = time.perf_counter_ns()
                        if not stop.is_set():
                            list(pool.map(lambda index: operation(index, False), range(LOAD_SAMPLES)))
                        row['wall_ms'] = (time.perf_counter_ns() - measured_start) / 1000000
                    cell['failed_count'] += row['failed_count']
                    require(not stop.is_set(), 'read load request failed; no retry performed')
                    measured = [sample['duration_ms'] for sample in row['requests'] if not sample['warmup']]
                    require(len(measured) == LOAD_SAMPLES and len(row['requests']) == LOAD_SAMPLES + LOAD_WARMUPS
                            and row['http_attempted_count'] == (LOAD_SAMPLES + LOAD_WARMUPS) * len(groups)
                            and 1 <= row['peak_in_flight'] <= concurrency
                            and (concurrency == 1 or row['peak_in_flight'] > 1),
                            'incomplete read load or no observed concurrency')
                    row.update(latency_statistics(measured))
                    require(row['wall_ms'] > 0, 'invalid read load wall clock')
                    row['observed_operations_per_second'] = LOAD_SAMPLES * 1000 / row['wall_ms']
                require(snapshot() == baseline, 'read load identity, rules or facts drifted')
                cell.update(latency_statistics([sample['duration_ms'] for row in cell['rounds']
                    for sample in row['requests'] if not sample['warmup']]))
                for quantile in ('p50_ms', 'p95_ms'):
                    values = [row[quantile] for row in cell['rounds']]
                    cell['round_' + quantile.removesuffix('_ms') + '_range_ms'] = [min(values), max(values)]
                cell['result'] = 'passed'
        result.update(unchanged_identity_rules_boundary=True)

    def measure_read_latency(self, report, facts, command):
        """Observe one existing Grant; never prepare or repair authorization here."""
        result = {'schema_version': 'addp.authorization-latency/v1', 'result': 'failed',
                  'measurement': 'gateway_client_http_and_json_roundtrip', 'unit': 'ms',
                  'endpoint': READ_CHECK, 'catalog_path': self.path, 'principal_id': self.recipient,
                  'grant_id': command, 'item_fingerprint': facts['item_fingerprint'],
                  'boundary_hash': facts['boundary_hash'], 'concurrency': 1,
                  'round_count': LATENCY_ROUNDS, 'samples_per_round': LATENCY_SAMPLES,
                  'warmups_per_round': LATENCY_WARMUPS,
                  'request_interval_ms': 10, 'round_interval_ms': 1000, 'budget_seconds': 90,
                  'quantile_method': 'nearest_rank', 'request_timeout_seconds': self.reader.timeout,
                  'started_at': datetime.now(timezone.utc).isoformat(),
                  'phase': 'controls_before', 'attempted_count': 0,
                  'sample_count': 0, 'warmup_count': 0, 'failed_count': 0, 'rounds': [], 'requests': []}
        report['authorization_latency'] = result
        deadline = time.monotonic() + 90
        credentials = (self.reader.token, self.operator.token)
        body = {'targets': [json.loads(json.dumps(self.path))]}

        def within_budget():
            require(time.monotonic() < deadline, 'fixed-target latency sampling exceeded its budget')

        def snapshot():
            within_budget()
            require((self.reader.token, self.operator.token) == credentials,
                    'latency sampling credential changed')
            context = self.reader.request('GET', '/api/v1/system/auth/context', (200,)).payload
            require(context.get('principal') == {'type': 'user', 'id': self.recipient}
                    and context.get('context', {}).get('tenant_id') == str(self.tenant)
                    and context.get('context', {}).get('type') == 'tenant',
                    'latency sampling identity changed')
            roles = tenant_functional_assignments(context, self.tenant)
            require({key for role in roles for key in role['permissions']} == READER_PERMISSIONS,
                    'latency sampling functional permissions changed')
            current = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
            require(all(current.get(key) == facts.get(key) for key in ('item_fingerprint', 'boundary_hash')),
                    'latency sampling DataItem boundary changed')
            history = self.listing(history=True, state='active')
            require(history.get('total') == 1 and len(history.get('data', [])) == 1,
                    'latency sampling requires exactly its original active Grant')
            grant = history['data'][0]
            require(all(grant.get(key) == value for key, value in {
                'request_id': command, 'catalog_path': self.path, 'recipient_type': 'user',
                'recipient_id': self.recipient, 'action': 'read', 'revocation': None,
                'item_fingerprint': facts['item_fingerprint'], 'boundary_hash': facts['boundary_hash']}.items()),
                'latency sampling Grant binding changed')
            within_budget()
            # Stable immutable snapshots, excluding only the Meta observation clock.
            return json.dumps([context, history, {key: value for key, value in current.items()
                                                 if key != 'observed_at'}], sort_keys=True)

        baseline = snapshot()
        try:
            observation = result['audit_observation'] = {}
            with AuditLatencyObservation(observation, self.recipient) as observer:
                for round_number in range(1, LATENCY_ROUNDS + 1):
                    row = {'round': round_number, 'warmup_count': 0, 'samples_ms': []}
                    result['rounds'].append(row)
                    for number in range(LATENCY_SAMPLES + LATENCY_WARMUPS):
                        within_budget()
                        observer.healthy()
                        require((self.reader.token, self.operator.token) == credentials,
                                'latency sampling credential changed')
                        result['phase'] = f'round_{round_number}_' + ('warmup' if number < LATENCY_WARMUPS else 'sample')
                        result['attempted_count'] += 1
                        wall_start = time.time_ns()
                        started = time.perf_counter_ns()
                        try:
                            response = self.reader.request('POST', READ_CHECK, (200,), body)
                            elapsed = (time.perf_counter_ns() - started) / 1000000
                            wall_end = time.time_ns()
                            payload = response.payload
                            require(response.status == 200 and isinstance(payload, dict) and set(payload) == {'observed_at'}
                                    and isinstance(payload['observed_at'], str), 'invalid read-check observation')
                            observed = datetime.fromisoformat(payload['observed_at'].replace('Z', '+00:00'))
                            require(observed.tzinfo is not None and observed.year > 1970, 'invalid read-check timestamp')
                            require(math.isfinite(elapsed) and elapsed >= 0, 'invalid latency clock')
                        except (SuiteError, ValueError, TypeError):
                            result['failed_count'] += 1
                            # Do not copy remote diagnostics or credentials into public evidence.
                            raise SuiteError('fixed-target latency request failed; no retry performed') from None
                        result['requests'].append({'round': round_number, 'index': number,
                            'warmup': number < LATENCY_WARMUPS, 'started_unix_ns': wall_start,
                            'finished_unix_ns': wall_end, 'duration_ms': elapsed})
                        if number < LATENCY_WARMUPS:
                            row['warmup_count'] += 1
                            result['warmup_count'] += 1
                        else:
                            row['samples_ms'].append(elapsed)
                            result['sample_count'] += 1
                        within_budget()
                        time.sleep(.01)
                    row.update(latency_statistics(row['samples_ms']))
                    result['phase'] = f'round_{round_number}_controls_after'
                    require(snapshot() == baseline, 'latency sampling identity, rules or facts drifted')
                    if round_number < LATENCY_ROUNDS:
                        time.sleep(1)
            require(observation.get('result') == 'passed' and observation.get('connection_closed') is True,
                    'audit observation did not finish cleanly')
            bind_audit_requests(result['requests'], observation['audit_rows'])
            samples = [sample for row in result['rounds'] for sample in row['samples_ms']]
            result.update(latency_statistics(samples))
            for quantile in ('p50_ms', 'p95_ms'):
                values = [row[quantile] for row in result['rounds']]
                result['round_' + quantile.removesuffix('_ms') + '_range_ms'] = [min(values), max(values)]
            result.update(result='passed', unchanged_identity_rules_boundary=True, phase='completed',
                          finished_at=datetime.now(timezone.utc).isoformat())
        except SuiteError:
            result['result'] = 'failed'
            raise

    def function_permission_flow(self, report, business, confirmer):
        report['phase'] = 'data_grant_without_read_function'
        principal, _ = identity(business, self.tenant, CONFIRMER_PERMISSIONS)
        require(principal == confirmer and principal != self.recipient,
                'function boundary must use the existing independent business User')
        credential = business.token
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        target = locator(self.engine, 'metric_people', item(self.reader, self.engine, 'metric_people')['id'])
        command, body = self.grant(facts, recipient_id=principal)
        issued = self.listing(history=True, state='active', recipient_id=principal)
        require(issued.get('total') == 1 and len(issued.get('data', [])) == 1
                and issued['data'][0].get('request_id') == command
                and all(issued['data'][0].get(key) == body[key] for key in (
                    'catalog_path', 'item_fingerprint', 'direct_permission_source', 'direct_permission_version',
                    'recipient_type', 'recipient_id', 'action', 'expiry_mode'))
                and issued['data'][0].get('boundary_hash') == facts['boundary_hash']
                and issued['data'][0].get('revocation') is None,
                'function boundary requires the exact active DataItem Grant')
        response = business.request('GET', '/api/v1/manager/preview?' + urlencode({
            'locator': target, 'page': 1, 'page_size': 20}), (403,))
        denied = response.payload
        require(set(denied) == {'error', 'error_code'} and isinstance(denied['error'], str)
                and bool(denied['error']) and denied['error_code'] == 'permission_denied',
                'missing read function must deny without data or source-error substitution')
        require(self.listing(history=True, state='active', recipient_id=principal) == issued,
                'functional denial changed or lost the active source Grant')
        observed_principal, _ = identity(business, self.tenant, CONFIRMER_PERMISSIONS)
        require(observed_principal == principal and business.token == credential,
                'function boundary changed the User, permissions or credential')
        self.revoke(command)
        revoked = self.listing(history=True, state='revoked', recipient_id=principal)
        require(any(row.get('request_id') == command and row.get('revocation') for row in revoked['data'])
                and self.listing(history=True, state='active', recipient_id=principal)['total'] == 0,
                'function boundary Grant was not withdrawn with history preserved')
        report['function_permission_boundary'] = {'principal_id': principal, 'grant_id': command,
            'preview_status': response.status, 'error_code': denied['error_code'],
            'grant_still_active': True, 'same_credential': True, 'withdrawn': True}
        report['checks'].append('data_grant_without_read_function_denied')

    def workspace_flow(self, report, issuer):
        report['phase'] = 'workspace_isolation'
        # Member exits deliberately retain organization Grants. Isolate the next
        # scenario through the existing owned-rule cleanup, never by deleting history.
        self.cleanup()
        report['workspace_isolation'] = 'zero_current_source_rules'
        report['phase'] = 'authorization_workspace_browser'
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        target = locator(self.engine, 'metric_people', item(self.reader, self.engine, 'metric_people')['id'])
        command, body = self.grant(facts, seconds=300)
        permission = REGISTRATION.read_direct_permission(self.operator, self.engine, facts['item_fingerprint'])
        require(permission['state'] == 'allowed' and permission['source'] == body['direct_permission_source']
                and permission['version'] == body['direct_permission_version'], 'workspace permission changed after its Grant')
        try:
            with WorkspaceObservation(self.tenant, self.recipient, target, self.engine, issuer, command, permission, facts) as browser:
                report['workspace_browser'] = browser.finish()
            current = REGISTRATION.read_direct_permission(self.operator, self.engine, facts['item_fingerprint'])
            require(current == dict(permission, version=str(int(permission['version']) + 2)),
                    'workspace browser did not restore the exact permission with two transitions')
        finally:
            # The context has terminated its browsers before restoring our disposable setting.
            report['workspace_permission_cleanup'] = 'failed'
            self.restore_workspace_permission(permission, facts)
            report['workspace_permission_cleanup'] = 'allowed'
        preview(self.reader, target, False)
        require(self.listing()['total'] == 0, 'browser withdrawal left current source rules')
        history = self.listing(history=True, state='revoked')
        require(any(row.get('request_id') == command and row.get('revocation') for row in history['data']),
                'browser withdrawal did not preserve its exact Grant history')
        report['checks'].extend(WORKSPACE_CHECKS)

    def restore_workspace_permission(self, baseline, facts):
        current = REGISTRATION.read_direct_permission(self.operator, self.engine, facts['item_fingerprint'])
        version = int(baseline['version'])
        expected = [dict(baseline, state=state, version=str(version + offset))
                    for offset, state in enumerate(('allowed', 'denied', 'allowed'))]
        require(current in expected, 'workspace cleanup encountered an unowned permission version or state')
        if current == expected[1]:
            current = REGISTRATION.set_direct_permission(self.operator, self.path, facts, True, current=current)
            require(current == expected[2], 'workspace cleanup could not restore its allowed permission')
            require(REGISTRATION.read_direct_permission(self.operator, self.engine, facts['item_fingerprint']) == expected[2],
                    'workspace cleanup permission readback did not match restoration')

    def deny(self, facts, *, recipient_type='user', recipient_id=None, catalog_path=None):
        command = str(uuid.uuid4())
        self.denies.append(command)
        body = {'deny_id': command, 'catalog_path': self.path if catalog_path is None else catalog_path, 'item_fingerprint': facts['item_fingerprint'],
                'recipient_type': recipient_type, 'recipient_id': self.recipient if recipient_id is None else recipient_id,
                'action': 'read', 'expiry_mode': 'at_time',
                'expires_at': (datetime.now(timezone.utc) + timedelta(seconds=180)).isoformat(),
                'reason': 'Disposable PG lifecycle Deny priority'}
        self.recipients.add((body['recipient_type'], body['recipient_id']))
        receipt = self.operator.request('POST', self.base + '/access_denies', (200,), body).payload
        require(receipt.get('deny_id') == command, 'Deny receipt identity changed')
        return command

    def release(self, command, *, cleanup=False):
        response = self.operator.request('POST', self.base + f'/access_denies/{command}/release', (200, 404, 409) if cleanup else (200,),
                                         {'reason': 'Disposable PG lifecycle Deny release'})
        require(response.status != 200 or response.payload.get('deny_id') == command,
                'Deny release receipt identity changed')
        require(response.status != 409 or response.payload.get('error_code') == 'engine_access_deny_expired',
                'Deny cleanup conflict is not natural expiry')

    def cleanup(self):
        errors = []
        if self.collection is not None:
            try:
                self.collection.cleanup()
            except Exception:
                errors.append('Meta collection cleanup failed')
        if self.production_tasks:
            try:
                # Close our source first. The shared source lock prevents a
                # still-delivering claim from issuing after this observation.
                for row in self.listing(history=True, state='active')['data']:
                    require(row['request_id'] in self.grants, 'unowned production source Grant')
                    self.revoke(row['request_id'])
                self.collect_production_grants()
            except Exception:
                errors.append('initial production Grant recovery failed')
        for command in self.denies:
            try:
                self.release(command, cleanup=True)
            except Exception:
                errors.append('Deny release failed')
        for path, kind, recipient in ((path, kind, recipient) for path in self.paths for kind, recipient in sorted(self.recipients)):
            try:
                active = self.listing(history=True, state='active', recipient_type=kind, recipient_id=recipient, catalog_path=path)
                for row in active['data']:
                    if row['request_id'] not in self.grants:
                        errors.append('refusing to revoke an unowned Grant')
                        continue
                    self.revoke(row['request_id'])
            except Exception:
                errors.append('Grant cleanup failed')
        for path, kind, recipient in ((path, kind, recipient) for path in self.paths for kind, recipient in sorted(self.recipients)):
            try:
                require(self.listing(recipient_type=kind, recipient_id=recipient, catalog_path=path)['total'] == 0,
                        'current source rules remain after cleanup')
            except Exception:
                errors.append('residual verification failed')
        for task in self.production_tasks:
            try:
                self.reader.request('DELETE', f'/api/v1/transfer/task-definitions/{task}', (200,))
                self.reader.request('GET', f'/api/v1/transfer/task-definitions/{task}', (404,))
            except Exception:
                errors.append('production task cleanup failed')
        require(not errors, '; '.join(errors))

    def catalog_flow(self, report, business, confirmer):
        report['phase'] = 'catalog_source_convergence'
        token = self.reader.token
        path = table_path(self.engine, 'metric_events')
        self.paths.append(path)  # Cleanup owns every target before any possible write.
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, path)
        row = item(self.reader, self.engine, 'metric_events')
        require(row['fingerprint'] == facts['item_fingerprint'], 'business target differs from persisted DataItem')
        target = locator(self.engine, 'metric_events', row['id'])
        def read(allowed):
            preview(self.reader, target, allowed, 4 if allowed else None,
                    columns=('event_id', 'event_date'), values=('e1', 'e2', 'e3', 'old'))
        read(False)
        query = urlencode({'view': 'inventory', 'source_identity': facts['item_fingerprint'], 'page_size': 2})
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            listing = business.request('GET', '/api/v1/catalog/entries?' + query, (200,)).payload
            require(isinstance(listing.get('data'), list) and len(listing['data']) <= 1,
                    'one DataItem must resolve to at most one Catalog entry')
            if listing['data']:
                break
            time.sleep(1)
        else:
            raise SuiteError('Catalog did not consume the basic scan before its deadline')
        entry_id = listing['data'][0]['id']
        require(str(uuid.UUID(entry_id)) == entry_id, 'Catalog entry identity is not canonical')
        route = '/api/v1/catalog/entries/' + entry_id
        entry = business.request('GET', route, (200,)).payload
        require(entry.get('governance_status') == 'discovered' and not entry.get('responsibilities')
                and not entry.get('semantic_links'), 'refusing to replace pre-existing curation')
        report['phase'] = 'catalog_business_responsibility'
        entry = business.request('PUT', route, (200,), {
            'version': entry['version'], 'business_name': 'Disposable PG business approval',
            'business_description': 'Single DataItem authorization acceptance', 'governance_status': 'discovered',
            'visibility': entry['visibility'], 'domains': [], 'glossary_ids': [],
            'responsibilities': [{'role': 'business_owner', 'subject_type': 'user', 'subject_id': confirmer}]}).payload
        require(any(value.get('role') == 'business_owner' and value.get('subject_id') == confirmer
                    for value in entry.get('responsibilities', [])), 'business ownership was not saved')
        boundary = business.request('GET', route + '/sharing_boundary', (200,)).payload
        require(boundary == {'item_fingerprint': facts['item_fingerprint'], 'boundary_hash': facts['boundary_hash'],
                             'native_target': path}, 'Catalog and System disagree on the Meta boundary')
        permission = REGISTRATION.set_direct_permission(self.operator, path, facts, False)
        decision_id, request_id = str(uuid.uuid4()), str(uuid.uuid4())
        evidence = report['catalog_approval'] = {'entry_id': entry_id, 'decision_id': decision_id,
            'request_id': request_id, 'item_fingerprint': facts['item_fingerprint'], 'boundary_hash': facts['boundary_hash']}
        decision_body = {'decision_id': decision_id, 'version': str(entry['version']), 'recipient_type': 'user',
            'recipient_id': self.recipient, 'expiry_mode': 'at_time',
            'expires_at': (datetime.now(timezone.utc) + timedelta(minutes=5)).isoformat(),
            'expected_boundary_hash': facts['boundary_hash'], 'reason': 'Disposable PG business read approval'}
        report['phase'] = 'catalog_confirmation_does_not_grant'
        decision = business.request('POST', route + '/sharing_decisions', (201,), decision_body).payload
        require(decision.get('id') == decision_id and decision.get('confirmed_by') == confirmer
                and decision.get('recipient_id') == self.recipient and decision.get('self_beneficiary') is False
                and decision.get('item_fingerprint') == facts['item_fingerprint']
                and decision.get('boundary_hash') == facts['boundary_hash'], 'business confirmation binding changed')
        read(False)
        require(self.listing(catalog_path=path)['total'] == 0, 'confirmation alone created a source Grant')
        self.grants.append(request_id)  # Includes an unknown outcome after acceptance or automatic issuance.
        command = {'request_id': request_id, 'decision_id': decision_id}
        report['phase'] = 'catalog_automatic_issuance'
        first = self.operator.request('POST', route + '/sharing_fulfillments', (200, 202), command).payload
        require(first.get('request_id') == request_id and first.get('state') in {'pending', 'accepted'},
                'Catalog did not retain the exact fulfillment request')
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            history = self.listing(history=True, state='active', catalog_path=path)
            if history['total']:
                break
            time.sleep(1)
        else:
            raise SuiteError('Catalog did not automatically issue the PG Grant')
        require(history['total'] == 1 and len(history['data']) == 1, 'business approval duplicated source Grants')
        grant = history['data'][0]
        require(grant.get('request_id') == request_id and grant.get('catalog_request_id') == request_id
                and grant.get('approval_mode') == 'catalog' and grant.get('catalog_path') == path
                and grant.get('engine_id') == str(self.engine) and grant.get('action') == 'read'
                and grant.get('direct_permission_version') == '0' and grant.get('direct_permission_source') == '' and bool(grant.get('granted_at'))
                and grant.get('operator_principal_id') == report['principals']['operator']
                and grant.get('item_fingerprint') == facts['item_fingerprint'] and grant.get('boundary_hash') == facts['boundary_hash']
                and grant.get('recipient_type') == 'user' and grant.get('recipient_id') == self.recipient
                and grant.get('expiry_mode') == 'at_time' and grant.get('revocation') is None
                and datetime.fromisoformat(grant['expires_at'].replace('Z', '+00:00')) == datetime.fromisoformat(decision_body['expires_at']),
                'business Grant does not preserve its decision and Meta boundary')
        settled = self.operator.request('GET', route + '/sharing_fulfillments/' + request_id, (200,)).payload
        require(settled.get('request_id') == request_id and settled.get('state') == 'accepted'
                and settled.get('decision_id') == decision_id and 'requirement_version' not in settled
                and settled.get('recipient_id') == self.recipient and settled.get('action') == 'read'
                and settled.get('granted_at') == grant.get('granted_at'), 'Catalog cannot observe the original issuance')
        self.operator.request('POST', route + '/sharing_fulfillments', (200,), command)
        require(self.listing(history=True, state='active', catalog_path=path) == history,
                'same-parameter fulfillment replay changed issuance history')
        read(True)
        preview(self.outsider, target, False)
        self.direct_permission_flow(report, business=business, path=path, facts=facts, route=route,
            permission=permission, command=command, history=history, read=read, target=target)
        self.catalog_outage_flow(report, path=path, facts=facts, route=route, command=command,
                                 history=history, read=read, target=target, permission_version='3')
        self.operator.request('POST', route + '/sharing_fulfillments', (200,), command)
        read(False)
        revoked = self.listing(history=True, state='revoked', catalog_path=path)
        require(revoked['total'] == 1 and revoked['data'][0]['request_id'] == request_id
                and revoked['data'][0]['granted_at'] == grant['granted_at'] and self.reader.token == token,
                'historical replay restored, renewed or replaced the revoked Grant')
        evidence.update({'rows': 4, 'approval_mode': 'catalog', 'same_credential': True,
                         'grant_replay_unchanged': True, 'revoked_replay_denied': True})
        report['checks'].extend(['catalog_confirmation_alone_denied', 'catalog_automatic_grant_bound_to_dataitem',
            'catalog_grant_actual_preview_readable', 'catalog_replay_preserves_issuance',
            'catalog_grant_deny_release_and_withdrawal', 'catalog_replay_does_not_restore_revoked_grant'])

    def direct_permission_flow(self, report, *, business, path, facts, route, permission, command, history, read, target):
        report['phase'] = 'direct_permission_explicit_update'
        update_route = self.base + f"/data-items/{facts['item_fingerprint']}/direct-permission"
        outsider = self.outsider.request('GET', '/api/v1/system/auth/context', (200,)).payload['principal']['id']
        token = self.outsider.token
        def outsider_read(allowed):
            preview(self.outsider, target, allowed, 4 if allowed else None,
                    columns=('event_id', 'event_date'), values=('e1', 'e2', 'e3', 'old'))
        def observe(state, version):
            current = REGISTRATION.read_direct_permission(self.operator, self.engine, facts['item_fingerprint'])
            require(current['state'] == state and current['source'] == 'explicit' and current['version'] == str(version),
                    'direct permission observation changed state or version')
            require(self.listing(history=True, state='active', catalog_path=path) == history,
                    'direct permission change rewrote the original business Grant')
            read(True)
            return current
        def switch(version, allowed, *, expected_version):
            current = dict(permission, source='explicit', state='allowed' if version == 2 else 'denied', version=str(version))
            result = REGISTRATION.set_direct_permission(self.operator, path, facts, allowed, current=current)
            require(result['version'] == str(expected_version), 'unexpected direct permission version')
            observe('allowed' if allowed else 'denied', expected_version)
            return result
        def rejected_grant(version):
            request_id, body = self.grant_command(facts, catalog_path=path, recipient_id=outsider,
                                                  permission={'source': 'explicit', 'version': str(version)})
            rejected = self.operator.request('POST', self.base + '/access_grants', (409,), body).payload
            require(rejected.get('error_code') == 'engine_access_grant_approval_changed',
                    'new direct approval did not reject its stale or business basis')
            require(not any(row.get('request_id') == request_id for row in self.listing(
                history=True, recipient_id=outsider, catalog_path=path)['data']), 'rejected handoff command issued a Grant')
            outsider_read(False)
            return request_id
        forbidden = business.request('PUT', update_route, (403,), {
            'catalog_path': path, 'expected_boundary_hash': facts['boundary_hash'], 'allowed': True,
            'expected_source': 'explicit', 'expected_version': '1', 'reason': 'Disposable PG direct permission update check'}).payload
        require(bool(forbidden.get('error')), 'missing update permission did not return a denial')
        observe('denied', 1)
        switch(1, True, expected_version=2)
        outsider_read(False)
        stale_direct = rejected_grant(1)
        # A new business approval remains usable while direct issuance is enabled.
        # Use a different recipient, then withdraw its actual Grant before the
        # direct-flow checks; never leave a duplicate pending issuance behind.
        business_decision = dict(decision_id=str(uuid.uuid4()),
            version=str(business.request('GET', route, (200,)).payload['version']),
            recipient_type='user', recipient_id=outsider, expiry_mode='at_time',
            expires_at=(datetime.now(timezone.utc) + timedelta(minutes=5)).isoformat(),
            expected_boundary_hash=facts['boundary_hash'], reason='Business approval independent of direct permission')
        created = business.request('POST', route + '/sharing_decisions', (201,), business_decision).payload
        require(created.get('id') == business_decision['decision_id'] and created.get('recipient_id') == outsider,
                'independent business decision changed recipient')
        business_command = {'request_id': str(uuid.uuid4()), 'decision_id': created['id']}
        self.grants.append(business_command['request_id'])
        result = self.operator.request('POST', route + '/sharing_fulfillments', (200, 202), business_command).payload
        require(result.get('request_id') == business_command['request_id'] and result.get('state') in {'pending', 'accepted'},
                'direct configuration blocked Catalog business acceptance')
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            business_history = self.listing(history=True, state='active', recipient_id=outsider, catalog_path=path)
            if business_history['total']:
                break
            time.sleep(1)
        require(business_history['total'] == 1 and len(business_history['data']) == 1,
                'independent business approval did not issue exactly one Grant')
        business_grant = business_history['data'][0]
        require(business_grant.get('request_id') == business_command['request_id']
                and business_grant.get('catalog_request_id') == business_command['request_id']
                and business_grant.get('approval_mode') == 'catalog' and business_grant.get('direct_permission_version') == '0' and business_grant.get('direct_permission_source') == ''
                and business_grant.get('item_fingerprint') == facts['item_fingerprint']
                and business_grant.get('boundary_hash') == facts['boundary_hash']
                and business_grant.get('recipient_type') == 'user' and business_grant.get('recipient_id') == outsider,
                'independent business Grant binding changed')
        outsider_read(True)
        self.revoke(business_command['request_id'])
        outsider_read(False)
        require(self.listing(history=True, state='active', catalog_path=path) == history,
                'independent business approval changed the original recipient Grant')
        report['phase'] = 'direct_permission_current_grant'
        direct_id, direct_body = self.grant(facts, catalog_path=path, recipient_id=outsider, permission={'source': 'explicit', 'version': '2'})
        direct_history = self.listing(history=True, state='active', recipient_id=outsider, catalog_path=path)
        require(direct_history['total'] == 1 and direct_history['data'][0].get('request_id') == direct_id
                and direct_history['data'][0].get('direct_permission_version') == '2'
                and direct_history['data'][0].get('direct_permission_source') == 'explicit'
                and direct_history['data'][0].get('approval_mode') == 'independent',
                'current-version direct approval changed its issuance basis')
        outsider_read(True)
        observe('allowed', 2)
        report['phase'] = 'direct_permission_disabled'
        current = switch(2, False, expected_version=3)
        require(self.listing(history=True, state='active', recipient_id=outsider, catalog_path=path) == direct_history,
                'disabling direct permission rewrote the existing direct Grant')
        outsider_read(True)
        report['phase'] = 'direct_permission_same_value_replay'
        repeated = switch(3, False, expected_version=3)
        require(repeated == current, 'same-value command mutated direct permission observation')
        original = self.operator.request('POST', route + '/sharing_fulfillments', (200,), command).payload
        require(original.get('request_id') == command['request_id'] and original.get('state') == 'accepted',
                'permission change invalidated the already accepted original business command')
        observe('denied', 3)
        stale_update = self.operator.request('PUT', update_route, (409,), {
            'catalog_path': path, 'expected_boundary_hash': facts['boundary_hash'], 'allowed': True,
            'expected_source': 'explicit', 'expected_version': '2', 'reason': 'Disposable PG stale permission version'}).payload
        require(stale_update.get('error_code') == 'resource_version_conflict', 'stale switch did not conflict explicitly')
        observe('denied', 3)
        self.operator.request('POST', self.base + '/access_grants', (201,), direct_body)
        require(self.listing(history=True, state='active', recipient_id=outsider, catalog_path=path) == direct_history,
                'original direct command replay renewed or replaced its issuance')
        self.revoke(direct_id)
        outsider_read(False)
        revoked = self.listing(history=True, state='revoked', recipient_id=outsider, catalog_path=path)
        rejected_grant(3)
        self.operator.request('POST', self.base + '/access_grants', (201,), direct_body)
        outsider_read(False)
        require(self.listing(history=True, state='revoked', recipient_id=outsider, catalog_path=path) == revoked,
                'revoked original direct replay changed withdrawal or issuance history')
        report['direct_permission_changes'] = {'item_fingerprint': facts['item_fingerprint'], 'versions': [1, 2, 3],
            'direct_request_id': direct_id, 'stale_direct_request_id': stale_direct,
            'independent_catalog_request_id': business_command['request_id'], 'same_consumer_credentials': self.outsider.token == token,
            'old_business_grant_unchanged': True, 'old_direct_grant_unchanged': True}
        require(self.outsider.token == token, 'handoff changed the separately granted consumer credential')
        report['checks'].extend(['direct_permission_update_permission_required', 'direct_permission_change_preserves_business_grant',
            'old_version_new_direct_grant_rejected', 'current_version_direct_grant_actual_read',
            'disabled_permission_preserves_direct_grant', 'same_value_update_preserves_version',
            'stale_permission_version_rejected', 'original_direct_replay_preserves_issuance_and_withdrawal',
            'new_direct_grant_blocked_after_permission_disabled'])
        report['checks'].append('catalog_acceptance_independent_of_direct_configuration')

    def catalog_outage_flow(self, report, *, path, facts, route, command, history, read, target, permission_version):
        report['phase'] = 'catalog_outage_existing_grant'
        token = self.reader.token
        outsider = self.outsider.request('GET', '/api/v1/system/auth/context', (200,)).payload['principal']['id']
        self.recipients.add(('user', outsider))
        blocked_request = str(uuid.uuid4())
        self.grants.append(blocked_request)
        direct_request, direct_body = self.grant_command(facts, catalog_path=path, recipient_id=outsider,
                                                        permission={'source': 'explicit', 'version': permission_version}, seconds=180)
        evidence = report['catalog_outage'] = {'unaccepted_request_id': blocked_request,
                                             'direct_request_id': direct_request}
        with DependencyOutage('catalog') as outage:
            read(True)
            require(self.listing(history=True, state='active', catalog_path=path) == history,
                    'Catalog outage changed the existing business Grant')
            report['phase'] = 'catalog_outage_new_fulfillment_blocked'
            unavailable = self.operator.request('POST', route + '/sharing_fulfillments', (502, 503),
                dict(command, request_id=blocked_request))
            require(set(unavailable.payload) == {'error'} and bool(unavailable.payload['error']),
                    'offline business request did not return the Gateway dependency error')
            # Use an ungranted recipient so a duplicate-relation conflict cannot mask the approval check.
            direct = self.operator.request('POST', self.base + '/access_grants', (409,), direct_body).payload
            require(set(direct) == {'error', 'error_code'} and bool(direct['error'])
                    and direct['error_code'] == 'engine_access_grant_approval_changed',
                    'direct authorization bypassed the explicitly denied direct permission')
            permission = REGISTRATION.read_direct_permission(self.operator, self.engine, facts['item_fingerprint'])
            require(permission['state'] == 'denied' and permission['source'] == 'explicit'
                    and permission['version'] == permission_version, 'Catalog outage implicitly changed direct permission')
            require(self.listing(history=True, state='active', recipient_id=outsider, catalog_path=path)['total'] == 0
                    and self.listing(history=True, state='active', catalog_path=path) == history,
                    'offline request silently issued a new Grant')
            preview(self.outsider, target, False)
            report['phase'] = 'catalog_outage_deny_release_and_withdrawal'
            deny = self.deny(facts, catalog_path=path)
            read(False)
            self.release(deny)
            read(True)
            self.revoke(command['request_id'])
            read(False)
        report['phase'] = 'catalog_recovery_preserves_withdrawal'
        for recipient in (self.recipient, outsider):
            require(self.listing(history=True, state='active', recipient_id=recipient, catalog_path=path)['total'] == 0,
                    'Catalog recovery created or restored an active Grant')
            all_history = self.listing(history=True, recipient_id=recipient, catalog_path=path)
            require(not any(row.get('request_id') in {blocked_request, direct_request} for row in all_history['data']),
                    'a rejected offline command left a source Grant history')
        absent = self.operator.request('GET', route + '/sharing_fulfillments/' + blocked_request, (404,)).payload
        require(bool(absent.get('error')) and not any(key in absent for key in ('state', 'granted_at')),
                'offline new request unexpectedly became a Catalog fulfillment')
        require(self.reader.token == token, 'Catalog outage changed the consumer credential')
        evidence.update(unavailable_status=unavailable.status, direct_error_code=direct['error_code'],
                        same_credential=True, before=outage.before, after=outage.after)
        report['checks'].extend(['existing_grant_readable_without_catalog', 'catalog_offline_new_fulfillment_unavailable',
            'catalog_offline_no_direct_approval_fallback', 'catalog_offline_deny_release_withdrawal',
            'catalog_recovery_preserves_original_rules'])

    def inspect_sources(self, facts, sources, denies, reason='grant'):
        current = self.listing()
        if not sources and not denies:
            require(current['total'] == 0 and current['data'] == [], 'source-free account still has a current relation')
            self.source_observations.append({'reason': 'no_grant', 'sources': [], 'denies': []})
            return
        require(current['total'] == 1 and len(current['data']) == 1, 'current sources were not merged to one DataItem')
        row = current['data'][0]
        require(row['catalog_path'] == self.path and row['item_fingerprint'] == facts['item_fingerprint']
                and row['recipient_type'] == 'user' and row['recipient_id'] == self.recipient
                and row['grant_count'] == len(sources) and 'request_id' not in row,
                'current relation target, recipient or grant count differs')
        observed = row['inspection']
        require(observed['account_id'] == self.recipient and observed['catalog_path'] == self.path
                and observed['item_fingerprint'] == facts['item_fingerprint'] and bool(observed['observed_at'])
                and observed['reason'] == reason and observed['rule_covered'] == (reason == 'grant'),
                'current source verdict differs from the expected rules')
        actual = {(source['recipient_type'], source['recipient_id']): source['request_id'] for source in observed['sources']}
        require(actual == sources and len(observed['sources']) == len(sources)
                and all(source['grant_count'] == 1 for source in observed['sources']), 'Grant source anchors differ')
        actual_denies = {(deny['recipient_type'], deny['recipient_id']): deny['deny_id'] for deny in observed['denies']}
        require(actual_denies == denies and len(observed['denies']) == len(denies), 'Deny source anchors differ')
        require(all(set(deny) == {'deny_id', 'recipient_type', 'recipient_id', 'expiry_mode', 'expires_at'}
                    for deny in observed['denies']), 'Deny summary exposed establishment details')
        self.source_observations.append({key: observed[key] for key in ('reason', 'sources', 'denies')})

    def organization_flow(self, report, facts, target, ungranted):
        report['phase'] = 'organization_sources'
        report['source_observations'] = self.source_observations
        report['organization_recipients'] = self.organizations
        before = self.reader.token
        preview(self.outsider, target, False)
        sources, denies = {}, {}
        personal = ('user', self.recipient)
        department = ('department', self.organizations['department'])
        project = ('project_group', self.organizations['project_group'])
        for source in (department, project, personal):
            sources[source], _ = self.grant(facts, recipient_type=source[0], recipient_id=source[1])
            self.inspect_sources(facts, sources, denies)
            preview(self.reader, target, True, 2)
            preview(self.outsider, target, False)
            preview(self.reader, ungranted, False)
        report['checks'].append('three_sources_merged_and_nonmember_denied')
        personal_id = sources.pop(personal)
        self.revoke(personal_id)
        self.inspect_sources(facts, sources, denies)
        require(self.listing(history=True, state='active')['total'] == 0,
                'history expanded current organization sources into personal Grants')
        preview(self.reader, target, True, 2)
        report['checks'].append('personal_withdrawal_preserves_organizations')
        for source in (personal, project):
            denies[source] = self.deny(facts, recipient_type=source[0], recipient_id=source[1])
            self.inspect_sources(facts, sources, denies, 'explicit_deny')
            preview(self.reader, target, False)
        self.release(denies.pop(personal))
        self.inspect_sources(facts, sources, denies, 'explicit_deny')
        preview(self.reader, target, False)
        report['checks'].append('one_deny_release_preserves_other_deny')
        self.release(denies.pop(project))
        self.inspect_sources(facts, sources, denies)
        preview(self.reader, target, True, 2)
        report['checks'].append('all_denies_released_restore_organization_read')
        self.revoke(sources.pop(department))
        self.inspect_sources(facts, sources, denies)
        preview(self.reader, target, True, 2)
        report['checks'].append('department_withdrawal_preserves_project')
        denies[personal] = self.deny(facts)
        self.revoke(sources.pop(project))
        self.inspect_sources(facts, sources, denies, 'explicit_deny')
        preview(self.reader, target, False)
        report['checks'].append('last_grant_withdrawn_preserves_deny_only_anchor')
        self.release(denies.pop(personal))
        self.inspect_sources(facts, sources, denies)
        preview(self.reader, target, False)
        preview(self.outsider, target, False)
        preview(self.reader, ungranted, False)
        require(self.reader.token == before, 'organization authorization changed the reader credential')
        report['checks'].append('all_sources_withdrawn_and_release_does_not_grant')

    def close_membership(self, kind, before):
        collection = 'departments' if kind == 'department' else 'project_groups'
        base = f'/api/v1/system/tenant/{collection}/{self.organizations[kind]}/memberships'
        listing = self.organization_operator.request('GET', base + '?page_size=100&status=active', (200,)).payload
        rows = listing['data']
        require(listing['total'] == 1 and len(rows) == 1, 'fixture organization must contain only its reader')
        row = rows[0]
        membership_id = row.get('id')
        version = row.get('version')
        require(isinstance(membership_id, str) and re.fullmatch(r'[1-9][0-9]*', membership_id)
                and type(version) is int and version > 0 and row.get('status') == 'active'
                and row.get('principal_id') == self.recipient
                and row.get('tenant_membership_id') == before['context']['tenant_membership_id'],
                'organization mutation does not identify the current reader membership')
        response = self.organization_operator.request('POST', base + f'/{membership_id}/close', (200,), {
            'version': version, 'reason': 'Disposable PG member-exit acceptance'}).payload
        require(all(response.get(key) == row[key] for key in ('id', 'principal_id', 'tenant_membership_id'))
                and response.get('status') == 'ended' and response.get('ended_at')
                and response.get('version') == version + 1, 'membership close receipt changed its binding')
        remaining = self.organization_operator.request('GET', base + '?page_size=100&status=active', (200,)).payload
        require(remaining['total'] == 0 and remaining['data'] == [], 'closed membership remains active')

    def relogin_reader(self, before, target, expected_organizations):
        # Deliberately keep the old token for both probes; do not auto-refresh it.
        for path in ('/api/v1/system/auth/context', '/api/v1/manager/preview?' + urlencode({
                'locator': target, 'page': 1, 'page_size': 20})):
            denied = self.reader.request('GET', path, (401,)).payload
            require(set(denied) <= {'error', 'error_code'} and bool(denied.get('error')),
                    'stale credential exposed content or identity')
        issued = self.login.request('POST', '/api/v1/system/login', (200,), {
            'username': self.username, 'password': self.password}).payload
        token = (issued.get('session') or {}).get('access_token')
        require(issued.get('next_action') == 'session_issued' and isinstance(token, str)
                and token and token != self.reader.token, 'formal reader login did not issue a fresh credential')
        fresh = API.GatewayClient(self.reader.base_url, token, self.reader.timeout)
        principal, _ = identity(fresh, self.tenant, READER_PERMISSIONS)
        current = fresh.request('GET', '/api/v1/system/auth/context', (200,)).payload
        require(principal == self.recipient and current['principal'] == before['principal']
                and current['context'] == before['context']
                and current['authorization']['role_assignments'] == before['authorization']['role_assignments']
                and int(current['authorization']['authorization_version']) > int(before['authorization']['authorization_version']),
                'reader login changed identity, roles, membership or failed to advance authorization version')
        organization_memberships(fresh, expected_organizations)
        self.reader = fresh

    def membership_flow(self, report, facts, target, ungranted):
        report['phase'] = 'organization_membership_changes'
        sources = {}
        for kind, recipient in self.organizations.items():
            sources[(kind, recipient)], _ = self.grant(facts, recipient_type=kind, recipient_id=recipient)
        issued_sources = dict(sources)
        self.inspect_sources(facts, sources, {})
        preview(self.reader, target, True, 2)
        remaining = dict(self.organizations)
        observations = report['membership_observations'] = []
        with BrowserObservation(self.tenant, self.recipient, target) as browser:
            for kind in ('department', 'project_group'):
                browser.wait(kind)
                before = self.reader.request('GET', '/api/v1/system/auth/context', (200,)).payload
                self.close_membership(kind, before)
                remaining.pop(kind)
                self.relogin_reader(before, target, remaining)
                sources.pop((kind, self.organizations[kind]))
                self.inspect_sources(facts, sources, {})
                preview(self.reader, target, bool(sources), 2)
                preview(self.reader, ungranted, False)
                preview(self.outsider, target, False)
                # Leaving an organization removes inheritance, not that organization's Grant.
                for recipient_type, recipient_id in self.organizations.items():
                    history = self.listing(history=True, state='active', recipient_type=recipient_type, recipient_id=recipient_id)
                    require(history['total'] == 1 and len(history['data']) == 1
                            and history['data'][0]['request_id'] == issued_sources[(recipient_type, recipient_id)]
                            and history['data'][0].get('revocation') is None,
                            'member exit revoked or lost an organization Grant')
                    require(self.listing(recipient_type=recipient_type, recipient_id=recipient_id)['total'] == 1,
                            'member exit removed the direct organization relation')
                observations.append({'closed_type': kind, 'old_token_status': 401,
                                     'remaining_sources': len(sources), 'preview_status': 200 if sources else 403})
                report['checks'].append('department_exit_invalidates_old_token_and_preserves_project_read' if sources else
                                        'last_organization_exit_invalidates_old_token_and_denies_read')
                browser.release(kind)
            report['browser'] = browser.finish()
            report['checks'].extend(BROWSER_CHECKS)
        report['checks'].append('organization_grants_and_history_survive_member_exit')

    def meta_recovery_flow(self, report, facts, target, ungranted):
        report['phase'] = 'meta_dependency_recovery'
        token = self.reader.token
        grant, _ = self.grant(facts)
        evidence = report['meta_recovery'] = {'grant_id': grant}
        preview(self.reader, target, True, 2)
        with DependencyOutage('meta') as outage:
            evidence['before'] = outage.before
            report['phase'] = 'meta_outage_facts_probe'
            response = self.operator.request('GET', self.base + '/data-items/' + facts['item_fingerprint']
                                             + '/authorization-facts', (503,)).payload
            require(set(response) == {'error', 'error_code'} and response['error_code'] == 'authorization_facts_unavailable',
                    'Meta outage was not a bounded facts dependency failure')
            report['phase'] = 'meta_outage_preview_probe'
            response = self.reader.request('GET', '/api/v1/manager/preview?' + urlencode({
                'locator': target, 'page': 1, 'page_size': 20}), (503,)).payload
            require(set(response) == {'error', 'error_code'} and response['error_code'] == 'authorization_service_unavailable',
                    'Meta outage leaked content or masqueraded as an absent scan')
            report['phase'] = 'meta_outage_known_target_deny'
            deny = self.deny(facts)
            evidence['deny_id'] = deny
            report['phase'] = 'meta_dependency_restart'
        # No scan, re-login, new Grant, or rule repair is permitted during recovery.
        evidence['after'] = outage.after
        report['phase'] = 'meta_recovery_binding_and_history'
        after = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        require(after['item_fingerprint'] == facts['item_fingerprint'] and after['boundary_hash'] == facts['boundary_hash']
                and self.reader.token == token, 'Meta recovery changed the resource binding or reader credential')
        active = self.listing(history=True, state='active')['data']
        require(len(active) == 1 and active[0]['request_id'] == grant and active[0].get('revocation') is None
                and active[0]['boundary_hash'] == facts['boundary_hash'], 'recovery altered the original Grant')
        report['phase'] = 'meta_recovered_deny_still_blocks'
        preview(self.reader, target, False)
        preview(self.reader, ungranted, False)
        report['phase'] = 'meta_recovery_explicit_release_and_withdrawal'
        self.release(deny)
        preview(self.reader, target, True, 2)
        self.revoke(grant)
        preview(self.reader, target, False)
        evidence.update({
            'facts_status': 503, 'unavailable_preview_status': 503, 'recovered_preview_status': 403,
            'released_preview_status': 200, 'same_credential': True, 'verified_without_rescan': True,
            'grant_id': grant, 'deny_id': deny})
        report['checks'].extend(['meta_outage_blocks_existing_grant', 'known_target_deny_created_offline',
            'meta_recovery_preserves_identity_boundary_and_grant', 'deny_survives_meta_recovery_without_rescan',
            'explicit_release_restores_original_grant_then_withdrawal_denies'])

    def content_change_flow(self, report, facts, original, target, ungranted):
        report['phase'] = 'pg_content_change_grant'
        token = self.reader.token
        grant, command = self.grant(facts)
        history = self.listing(history=True)
        scans = []
        def rescan():
            scans.append(basic_scan(self.reader, self.engine, ['metric_people']))
            current = item(self.reader, self.engine, 'metric_people')
            observed = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
            require(current['id'] == original['id'] and current['fingerprint'] == original['fingerprint']
                    and observed['item_fingerprint'] == facts['item_fingerprint']
                    and observed['boundary_hash'] == facts['boundary_hash'],
                    'PG content update changed the approved DataItem identity or boundary')
            preview(self.reader, ungranted, False)
            preview(self.outsider, target, False)
        def unchanged(expected):
            require(self.listing(history=True) == expected, 'PG content update rewrote Grant history')
        try:
            mutate_source_content('content-change')
            rescan()
            unchanged(history)
            preview(self.reader, target, True, 3, values=('A', 'B2', 'C'))
            report['phase'] = 'pg_content_change_deny'
            deny = self.deny(facts)
            mutate_source_content('content-reset')
            rescan()
            unchanged(history)
            current = self.listing()['data']
            require(len(current) == 1 and current[0]['inspection']['reason'] == 'explicit_deny'
                    and [row['deny_id'] for row in current[0]['inspection']['denies']] == [deny],
                    'Deny did not survive the content update and basic rescan')
            preview(self.reader, target, False)
            self.release(deny)
            preview(self.reader, target, True, 2)
            unchanged(history)
            self.revoke(grant)
            withdrawn = self.listing(history=True)
            report['phase'] = 'pg_content_change_withdrawn'
            mutate_source_content('content-change')
            rescan()
            preview(self.reader, target, False)
            unchanged(withdrawn)
            self.operator.request('POST', self.base + '/access_grants', (201,), command)
            preview(self.reader, target, False)
            unchanged(withdrawn)
            require(self.listing()['total'] == 0, 'withdrawn Grant or released Deny returned after content change')
        finally:
            mutate_source_content('content-reset')
        report['phase'] = 'pg_content_baseline_restored'
        rescan()
        preview(self.reader, target, False)
        unchanged(withdrawn)
        require(self.reader.token == token and self.listing()['total'] == 0,
                'content acceptance changed the credential or left a current source rule')
        report['content_changes'] = {'basic_scans': scans, 'same_identity_and_boundary': True,
            'same_credential': True, 'changed_row_count': 3, 'baseline_row_count': 2, 'source_baseline_restored': True,
            'grant_request_id': grant, 'deny_id': deny}
        report['checks'].extend(['content_change_preserves_identity_and_boundary', 'old_grant_reads_changed_content',
            'deny_survives_content_change_and_basic_rescan', 'release_restores_current_content',
            'content_change_and_replay_do_not_restore_withdrawn_grant', 'source_baseline_restored_without_read_access'])

    def run(self, report):
        reader_before = self.reader.token
        report['phase'] = 'initial_protection_sync'
        API.wait_manager_read_boundary(self.reader, self.engine, time.monotonic() + 120)
        report['phase'] = 'initial_basic_scan'
        report['initial_scan'] = basic_scan(self.reader, self.engine, ['metric_people', 'metric_events'])
        original = item(self.reader, self.engine, 'metric_people')
        other = item(self.reader, self.engine, 'metric_events')
        target = locator(self.engine, 'metric_people', original['id'])
        ungranted = locator(self.engine, 'metric_events', other['id'])
        preview(self.reader, target, False)
        preview(self.reader, ungranted, False)
        report['phase'] = 'grant_and_duplicate'
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        require(original.get('fingerprint') == facts['item_fingerprint'], 'Meta identity differs from trusted boundary')
        first, body = self.grant(facts)
        preview(self.reader, target, True, 2)
        preview(self.reader, ungranted, False)
        duplicate = dict(body, request_id=str(uuid.uuid4()))
        self.grants.append(duplicate['request_id'])
        rejected = self.operator.request('POST', self.base + '/access_grants', (409,), duplicate).payload
        require(rejected.get('error_code') == 'engine_access_grant_relation_exists', 'duplicate rejected for the wrong reason')
        report['phase'] = 'fixed_target_read_latency'
        self.measure_read_latency(report, facts, first)
        report['phase'] = 'basic_rescan'
        report['basic_rescan'] = basic_scan(self.reader, self.engine, ['metric_people'])
        refreshed = item(self.reader, self.engine, 'metric_people')
        after = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        require(refreshed['id'] == original['id'] and refreshed['fingerprint'] == original['fingerprint']
                and after['boundary_hash'] == facts['boundary_hash'], 'basic rescan changed the approved DataItem boundary')
        preview(self.reader, target, True, 2)
        report['phase'] = 'deny_release_revoke'
        deny = self.deny(facts)
        preview(self.reader, target, False)
        require(self.listing(history=True, state='active')['total'] == 1, 'Deny test lost its active Grant')
        self.release(deny)
        preview(self.reader, target, True, 2)
        self.revoke(first)
        preview(self.reader, target, False)
        require(self.listing(history=True, state='revoked')['total'] == 1, 'withdrawal history is missing')
        report['phase'] = 'natural_expiry'
        self.expiry_grant, _ = self.grant(facts, seconds=45)
        preview(self.reader, target, True, 2)
        deadline = time.monotonic() + 75
        while time.monotonic() < deadline:
            expired = self.listing(history=True, state='expired')
            if any(row.get('request_id') == self.expiry_grant for row in expired['data']):
                break
            time.sleep(1)
        else:
            raise SuiteError('Grant did not naturally expire within deadline')
        preview(self.reader, target, False)
        preview(self.reader, ungranted, False)
        require(self.reader.token == reader_before and self.listing()['total'] == 0,
                'natural expiry changed the credential or left current authorization')
        row = next(row for row in expired['data'] if row.get('request_id') == self.expiry_grant)
        require(row.get('revocation') is None, 'manual withdrawal replaced natural expiry')
        report.update(grant_commands=self.grants, item_fingerprint=facts['item_fingerprint'], boundary_hash=facts['boundary_hash'],
                      rows=2, same_credential_through_grant_expiry=True, exact_target_only=True,
                      checks=['ungranted_denied', 'granted_readable', 'duplicate_rejected', 'basic_rescan_readable',
                              'deny_overrides_grant', 'release_restores_read', 'revoke_denies_read', 'natural_expiry_denies_read'])
        self.organization_flow(report, facts, target, ungranted)
        self.membership_flow(report, facts, target, ungranted)
        self.meta_recovery_flow(report, facts, target, ungranted)
        self.content_change_flow(report, facts, original, target, ungranted)


def main():
    report = {'schema_version': 'addp.pg-source-authorization/v1', 'suite': SUITE, 'result': 'failed'}
    lifecycle = None
    evidence = None
    def interrupted(signum, frame):
        raise SuiteError('PG source authorization acceptance interrupted')
    try:
        require(all(os.environ.get(key) == value for key, value in {
            'ADDP_ONLINE_TEST': '1', 'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1',
            'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'POSTGRES_DB': 'addp_online'}.items()),
            'use the disposable Hosted PG authorization gate')
        tenant, engine = int(os.environ['ADDP_ONLINE_TEST_TENANT_ID']), int(os.environ['ADDP_ONLINE_CONSUMER_ENGINE_ID'])
        require(tenant > 1 and engine > 0, 'nondefault Tenant and positive Engine required')
        directory = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR'])
        require(directory.is_absolute() and not directory.resolve().is_relative_to(Path(__file__).resolve().parents[2]),
                'evidence must be outside checkout')
        directory = directory.resolve()
        run_id = os.environ['ADDP_ONLINE_TEST_RUN_ID']
        require(re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,63}', run_id), 'invalid Run ID')
        directory.mkdir(parents=True, exist_ok=True)
        evidence = directory / (SUITE + '.json')
        timeout = float(os.environ.get('ADDP_ONLINE_TEST_HTTP_TIMEOUT_SECONDS', '10'))
        require(math.isfinite(timeout) and 0 < timeout <= 30, 'invalid HTTP timeout')
        report['phase'] = 'fixture_identity_validation'
        business = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_FIXTURE_CONFIRMER_ACCESS_TOKEN'], timeout)
        reader = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_TEST_USER_ACCESS_TOKEN'], timeout)
        operator = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_FIXTURE_SOURCE_ACCESS_TOKEN'], timeout)
        outsider = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_FIXTURE_OUTSIDER_ACCESS_TOKEN'], timeout)
        organization_operator = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ['ADDP_ONLINE_ADMIN_USER_ACCESS_TOKEN'], timeout)
        login = API.GatewayClient(os.environ['GATEWAY_URL'], '', timeout)
        organizations = {kind: os.environ[key] for kind, key in (
            ('department', 'ADDP_ONLINE_FIXTURE_DEPARTMENT_ID'), ('project_group', 'ADDP_ONLINE_FIXTURE_PROJECT_GROUP_ID'))}
        require(all(re.fullmatch(r'[1-9][0-9]*', value) for value in organizations.values()), 'invalid organization fixture IDs')
        recipient, report['reader'] = identity(reader, tenant, READER_PERMISSIONS)
        issuer, report['operator'] = identity(operator, tenant, OPERATOR_PERMISSIONS)
        observer, report['outsider'] = identity(outsider, tenant, READER_PERMISSIONS)
        confirmer, report['confirmer'] = identity(business, tenant, CONFIRMER_PERMISSIONS)
        coordinator, report['organization_operator'] = organization_administrator(organization_operator, tenant)
        require(len({issuer, recipient, observer, coordinator, confirmer}) == 5
                and len({reader.token, operator.token, outsider.token, organization_operator.token, business.token}) == 5,
                'source operator, reader, outsider, membership operator and confirmer must be distinct Users')
        organization_memberships(reader, organizations)
        organization_memberships(outsider, {})
        for signum in (signal.SIGTERM, signal.SIGINT):
            signal.signal(signum, interrupted)
        report.update(run_id=run_id, tenant_id=str(tenant), engine_id=engine)
        report['principals'] = {'reader': recipient, 'operator': issuer, 'outsider': observer,
                                'organization_operator': coordinator, 'confirmer': confirmer}
        lifecycle = Lifecycle(operator, reader, tenant, engine, recipient, organizations, outsider,
                              organization_operator, login, os.environ['ADDP_ONLINE_TEST_USER_USERNAME'],
                              os.environ['ADDP_ONLINE_TEST_USER_PASSWORD'])
        lifecycle.run(report)
        lifecycle.function_permission_flow(report, business, confirmer)
        lifecycle.catalog_flow(report, business, confirmer)
        lifecycle.workspace_flow(report, issuer)
        lifecycle.production_flow(report)
        lifecycle.collection = MetaCollectionScenario(lifecycle, issuer, run_id)
        lifecycle.collection.run(report)
        lifecycle.collection.run_browser(report)
        lifecycle.collection.run_expiry(report)
        lifecycle.collection.run_scheduled(report)
        controller = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ.pop('ADDP_ONLINE_PG_PROBE_ACCESS_TOKEN'), timeout)
        lifecycle.collection.run_probe_control(report, controller)
        del controller
        lifecycle.collection.run_inflight(report)
        lifecycle.collection.run_inflight_expiry(report)
        report['result'] = 'passed'
    except (SuiteError, PREFLIGHT.PreflightError, REGISTRATION.RegistrationError, KeyError, ValueError, TypeError, OSError):
        print('PG source authorization acceptance failed; inspect the bounded phase evidence', file=sys.stderr)
    finally:
        if lifecycle is not None:
            report.update(grant_commands=lifecycle.grants, deny_commands=lifecycle.denies)
            if lifecycle.collection is not None:
                report['meta_collection']['cleanup'] = 'failed'
            try:
                lifecycle.cleanup()
                report['cleanup'] = 'zero_current_source_rules'
                if lifecycle.collection is not None:
                    report['meta_collection']['cleanup'] = 'zero_current_qualifications_and_tasks'
            except Exception:
                report.update(result='failed', cleanup='failed')
        if evidence is not None:
            evidence.write_text(json.dumps(report, sort_keys=True) + '\n')
    print(json.dumps(report, sort_keys=True))
    return 0 if report['result'] == 'passed' else 1


if __name__ == '__main__':
    raise SystemExit(main())
