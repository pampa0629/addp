"""PG-only real System/Meta/Manager source authorization lifecycle."""

from __future__ import annotations

import json
import base64
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
from bisect import bisect_right
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
from importlib import import_module
from pathlib import Path
from urllib.parse import urlencode, urlsplit, parse_qs

API = import_module('scripts.utils.online-api')
PREFLIGHT = import_module('scripts.test.online-preflight')
REGISTRATION = import_module('scripts.test.online-engine-registration')
REFRESH = import_module('scripts.test.metadata-refresh-scope-online')
CGROUP = import_module('scripts.utils.capacity_resources')
SuiteError = API.SuiteError
SUITE = 'pg-source-authorization'
READER_PERMISSIONS = {'manager.data_item.read', 'meta.catalog.read', 'meta.lineage.read', 'meta.scan_task.execute', 'meta.scan_task.read'}
READER_PERMISSIONS |= {'transfer.task.create', 'transfer.task.read', 'transfer.task.execute',
                       'transfer.task.delete', 'system.execution_authorization.create'}
OPERATOR_PERMISSIONS = REGISTRATION.SOURCE_INITIALIZER_PERMISSIONS | {
    'system.engine_access_grant.revoke',
    'catalog.entry.read', 'catalog.inventory.read', 'system.engine_access_fulfillment.create',
    'system.engine.read', 'system.engine_catalog.read',
    'meta.scan_task.create', 'meta.scan_task.update', 'meta.scan_task.delete',
    'meta.scan_task.read', 'meta.scan_task.execute', 'meta.scan_task.cancel',
    'iam.tenant_membership.read', 'iam.department.read', 'iam.project_group.read'}
CONFIRMER_PERMISSIONS = {'catalog.entry.read', 'catalog.entry.update', 'catalog.inventory.read',
                        'catalog.sharing_decision.create'}
ORGANIZATION_PERMISSIONS = {'iam.department_membership.read', 'iam.department_membership.close',
                            'iam.project_group_membership.read', 'iam.project_group_membership.close'}
BROWSER_CHECKS = ['department_exit_silent_refresh', 'project_exit_silent_refresh',
                  'same_iframe_without_relogin', 'denied_preview_clears_old_rows']
WORKSPACE_CHECKS = ['exclusive_workspace_tabs', 'integrated_create_picker_without_write',
                    'table_text_and_tree_filters', 'recipient_type_and_account_filters',
                    'browser_revocation_updates_current_and_history', 'browser_revocation_denies_manager_read']
PRODUCTION_CHECKS = ['new_pg_output_initial_grant_issued', 'new_pg_output_basic_only_identification',
                     'new_pg_output_manager_readable', 'output_survives_source_withdrawal',
                     'output_withdrawal_denies_read']
LINEAGE_CHECKS = ['pg_item_and_field_lineage_visible', 'pg_lineage_ungranted_root_hidden',
                  'pg_lineage_source_withdrawal_filters_current_and_history',
                  'pg_lineage_output_withdrawal_hides_root', 'pg_lineage_service_root_denied']
COLLECTION_CHECKS = ['meta_central_missing_management_denied', 'meta_central_manual_repeat',
                     'meta_central_deep_rejected', 'meta_central_basic_configuration_update',
                     'meta_central_source_rules_unchanged']
METADATA_CHECKS = ['metadata_only_visible_content_denied', 'read_withdrawal_preserves_metadata_grant',
                   'metadata_withdrawal_hides_identity', 'metadata_natural_expiry_hides_identity',
                   'catalog_metadata_confirmation_does_not_grant', 'catalog_metadata_issuance_cannot_upgrade',
                   'catalog_metadata_withdrawal_replay_stays_denied']

READ_CHECK = '/api/v1/system/engine-access/read-checks/manager-pg-preview'
LATENCY_ROUNDS, LATENCY_SAMPLES, LATENCY_WARMUPS = 3, 500, 10
LOAD_ROUNDS, LOAD_SAMPLES, LOAD_WARMUPS = 3, 100, 10
CPU_LOAD_PROFILE = 'single_target_cpu'
AUTH_CONTEXT_PHASES = ('route', 'rule_snapshot')
AUTH_CONTEXT_STAGES = ('transaction_begin', 'session_snapshot', 'departments', 'project_groups',
                       'role_permissions', 'projection', 'transaction_commit')
SNAPSHOT_QUERY_TIMINGS = ('session_snapshot_query_wall', 'session_snapshot_server_to_row',
                         'session_snapshot_before_query', 'session_snapshot_to_first_row',
                         'session_snapshot_after_first_row')
PG_CPU_SYSFS = Path('/sys/devices/system/cpu')


def set_pg_cpu3(online):
    try:
        subprocess.run(['sudo', '-n', 'tee', str(PG_CPU_SYSFS / 'cpu3/online')],
                       input=f'{int(online)}\n', text=True, stdout=subprocess.DEVNULL,
                       stderr=subprocess.DEVNULL, timeout=5, check=True)
    except (OSError, subprocess.SubprocessError):
        raise SuiteError('PG sampling CPU switch failed') from None


@contextmanager
def pg_load_cpu_window(arm):
    if not arm:
        yield
        return
    require(arm in {'3', '4'} and sys.platform == 'linux' and
            os.environ.get('ADDP_ONLINE_HOSTED') == '1' and os.environ.get('GITHUB_ACTIONS') == 'true',
            'PG sampling CPU switch requires the disposable Hosted runner')
    online = PG_CPU_SYSFS / 'online'
    before = online.read_text().strip()
    require(before == '0-3', 'PG sampling requires four online vCPUs before the window')
    expected = '0-2' if arm == '3' else before
    evidence = {'scope': 'single_target_load', 'requested_vcpus': arm, 'before': before}
    try:
        if arm == '3':
            set_pg_cpu3(False)
        evidence['during_start'] = online.read_text().strip()
        require(evidence['during_start'] == expected, 'PG sampling CPU arm did not take effect')
        yield
    finally:
        try:
            evidence['during_end'] = online.read_text().strip()
        finally:
            try:
                if arm == '3' and evidence.get('during_end') != before:
                    set_pg_cpu3(True)
            finally:
                evidence['after'] = online.read_text().strip()
                path = Path(os.environ['ADDP_ONLINE_ARTIFACT_DIR']) / 'cpu-arm.txt'
                path.write_text(''.join(f'{key}={value}\n' for key, value in evidence.items()))
        require(evidence['after'] == before, 'PG sampling CPU availability was not restored')
        if evidence.get('during_start') == expected:
            require(evidence['during_end'] == expected, 'PG sampling CPU availability changed during the window')

# Deliberately no query text/parameters, credentials, relation data or query plans
# in the published evidence. Counters belong to the whole disposable instance.
AUDIT_ACTIVITY_SQL = '''SELECT json_build_object(
 'at', clock_timestamp(), 'connections', (SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()),
 'activity', COALESCE((SELECT json_agg(row_to_json(s)) FROM (
 SELECT pid, backend_start, xact_start, query_start, state_change, state, wait_event_type, wait_event,
 pg_blocking_pids(pid) AS blocking_pids,
 CASE WHEN ltrim(query) ILIKE 'INSERT INTO "system"."audit_logs"%' THEN 'audit_insert'
      WHEN query ~* '^[[:space:]]*WITH input AS MATERIALIZED[[:space:]]*[(]'
           AND query LIKE '%AS targets, clock_timestamp() AS observed_at%' THEN 'source_rules'
      WHEN query ~* '^[[:space:]]*SELECT[[:space:]]+(access_token|ticket|delegated)[.]id AS credential_id'
           OR query ~* '^[[:space:]]*WITH RECURSIVE direct_memberships AS'
           OR (query ~* '^[[:space:]]*SELECT[[:space:]]+membership[.]id AS membership_id'
               AND query LIKE '%FROM system.project_group_memberships membership%')
           OR (query ~* '^[[:space:]]*SELECT[[:space:]]+assignment[.]id AS assignment_id'
               AND query LIKE '%FROM system.role_assignments assignment%')
           THEN 'auth_context'
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


def process_cpu(pid):
    # Linux /proc/PID/stat: comm may contain spaces and parentheses.
    fields = (Path('/proc') / str(pid) / 'stat').read_text().rsplit(')', 1)[1].split()
    if fields[0] in {'Z', 'X'}:
        raise ValueError('process exited')
    return {'pid': pid, 'parent_pid': int(fields[1]), 'user_ticks': int(fields[11]),
            'system_ticks': int(fields[12]), 'start_ticks': int(fields[19])}


class LoadCPUObservation:
    """Passive counters from the exact Hosted checkout and its owned PG container."""
    def __init__(self, root, postgres_pid, container_id):
        self.processes = {'runner_with_observer': process_cpu(os.getpid())}
        for unit in ('system', 'meta', 'gateway'):
            self.processes[unit] = self.processes[unit + '_capture'] = None
            try:
                raw = (root / '.dev-pids' / (unit + '.pid')).read_text().strip()
                if not re.fullmatch(r'[1-9][0-9]*', raw) or int(raw) <= 1:
                    continue
                pid = int(raw)
                proc = Path('/proc') / raw
                if (proc / 'exe').resolve() != root / '.dev-bins' / ('addp-' + unit) or (proc / 'cwd').resolve() != root:
                    continue
                self.processes[unit] = process_cpu(pid)
                captures = []
                for children in (proc / 'task').glob('*/children'):
                    for child in children.read_text().split():
                        if (Path('/proc') / child / 'exe').resolve() == root / '.dev-bins' / 'addp-runtime-log':
                            record = process_cpu(int(child))
                            if record['parent_pid'] == pid:
                                captures.append(record)
                if len(captures) == 1:
                    self.processes[unit + '_capture'] = captures[0]
            except (OSError, ValueError, IndexError):
                # A missing or raced process is an explicit gap, not zero CPU.
                continue
        self.postgres = None
        if type(postgres_pid) is int and postgres_pid > 1:
            try:
                group = CGROUP.cgroup_directory(postgres_pid)
                if group.name in {container_id, 'docker-' + container_id + '.scope'}:
                    self.postgres = group
            except (OSError, ValueError, CGROUP.ResourceError):
                pass

    def sample(self):
        at = time.time_ns()
        result = {}
        for name, identity in self.processes.items():
            result[name] = None
            if identity is not None:
                try:
                    current = process_cpu(identity['pid'])
                    if current['start_ticks'] == identity['start_ticks']:
                        result[name] = current
                except (OSError, ValueError, IndexError):
                    pass
        postgres = None
        if self.postgres is not None:
            try:
                counters = dict(line.split() for line in (self.postgres / 'cpu.stat').read_text().splitlines())
                postgres = {key: int(counters[key]) for key in ('usage_usec', 'user_usec', 'system_usec')}
                postgres.update({key: int(counters[key]) for key in ('nr_throttled', 'throttled_usec') if key in counters})
            except (OSError, ValueError, KeyError):
                pass
        return {'at_unix_ns': at, 'finished_unix_ns': time.time_ns(), 'processes': result, 'postgres_cgroup': postgres}


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
        self.cpu = None
        self.last_cpu_ns = 0
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
        if self.cpu is not None and started - self.last_cpu_ns >= 250_000_000:
            snapshot['cpu_resources'] = self.cpu.sample()
            if threading.current_thread() is self.thread:
                snapshot['cpu_resources']['observer_thread_cpu_ns'] = time.thread_time_ns()
            self.last_cpu_ns = started
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
            if self.file == 'pg-load-waits.jsonl':
                self.cpu = LoadCPUObservation(Path(__file__).resolve().parents[2], container['State'].get('Pid'), identifier)
                self.evidence.update(cpu_clock_ticks_per_second=os.sysconf('SC_CLK_TCK'), cpu_sample_interval_ms=250)
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


def collect_system_load_tail(directory, audits, *, rule_timings=None, wait_seconds=60):
    """Match existing redacted System HTTP records after load and rule cleanup."""
    if not audits or any(not isinstance(row, dict) for row in audits):
        return {'result': 'not_observed'}
    expected = {row.get('request_id') for row in audits}
    if any(not isinstance(value, str) or not re.fullmatch('[a-f0-9-]{36}', value) for value in expected):
        return {'result': 'not_observed'}
    directory = directory.resolve()
    deadline = time.monotonic() + wait_seconds
    matched, offsets = set(), {}
    read_bytes = 0
    reason = 'deadline'
    try:
        while True:
            for path in sorted(directory.glob('*/*.jsonl')):
                if path.is_symlink() or not path.resolve().is_relative_to(directory):
                    continue
                with path.open('rb') as stream:
                    stream.seek(offsets.get(path, 0))
                    while rule_timings is not None or len(matched) < len(expected):
                        raw = stream.readline(512 * 1024 + 1)
                        read_bytes += len(raw)
                        if len(raw) > 512 * 1024 or read_bytes > 64 * 1024 * 1024:
                            raise ValueError('source log read budget exceeded')
                        if not raw.endswith(b'\n'):
                            break
                        offsets[path] = stream.tell()
                        if b'http.request.completed' not in raw or READ_CHECK.encode() not in raw:
                            continue
                        entry = json.loads(raw)
                        request = entry.get('request_id')
                        if request not in expected or entry.get('module_name') != 'system' or entry.get('role') != 'backend':
                            continue
                        message = json.loads(entry['message'])
                        if (message.get('request_id'), message.get('msg'), message.get('method'), message.get('route')) != (
                                request, 'http.request.completed', 'POST', READ_CHECK):
                            continue
                        if rule_timings is not None:
                            diagnostics = message.get('diagnostics', {})
                            source = diagnostics.get('source_rule_query', {})
                            pid, start, finish = (source.get(key) for key in
                                                  ('backend_pid', 'started_unix_ns', 'finished_unix_ns'))
                            if (request in matched or type(pid) is not int or pid <= 0 or
                                    not all(isinstance(value, str) and re.fullmatch('[1-9][0-9]{0,18}', value)
                                            for value in (start, finish)) or int(start) >= int(finish)):
                                raise ValueError('invalid or duplicate rule query timing')
                            identity = {}
                            if set(diagnostics.get('auth_context_ns', {})) != set(AUTH_CONTEXT_PHASES):
                                raise ValueError('unexpected auth context resolution phases')
                            for phase in AUTH_CONTEXT_PHASES:
                                stages = diagnostics.get('auth_context_ns', {}).get(phase, {})
                                values = {stage: stages.get(stage) for stage in AUTH_CONTEXT_STAGES}
                                if (not all(type(value) is int and value >= 0 for value in values.values())
                                        or not 0 < sum(values.values()) <= 90_000_000_000):
                                    raise ValueError('invalid auth context stage timing')
                                paired = {key: stages.get(key) for key in SNAPSHOT_QUERY_TIMINGS}
                                wall, server, before, first, after = paired.values()
                                if (not all(type(value) is int and value >= 0 for value in paired.values())
                                        or not 0 < server <= first <= wall <= values['session_snapshot']
                                        or before + first + after != wall):
                                    raise ValueError('invalid paired session snapshot query timing')
                                values.update(paired)
                                identity[phase] = values
                            rule_timings[request] = {'backend_pid': pid, 'started_unix_ns': int(start),
                                                     'finished_unix_ns': int(finish), 'auth_context_ns': identity}
                        matched.add(request)
            if matched == expected or time.monotonic() >= deadline:
                break
            time.sleep(min(.25, max(0, deadline - time.monotonic())))
    except (OSError, ValueError, TypeError, KeyError, AttributeError):
        reason = 'read_failed_or_budget'
    complete = matched == expected and reason != 'read_failed_or_budget'
    return {'result': 'matched' if complete else 'incomplete',
            'expected': len(expected), 'matched': len(matched),
            'reason': 'all_load_audit_ids' if complete else reason}


def bind_source_rule_waits(timings, snapshots):
    """Attribute sampled activity on the same backend within one query interval."""
    by_pid = {}
    for request, record in timings.items():
        record.update(request_id=request, active_samples=0, wait_samples={})
        by_pid.setdefault(record['backend_pid'], []).append(record)
    starts = {}
    for pid, windows in by_pid.items():
        windows.sort(key=lambda record: record['started_unix_ns'])
        require(all(left['finished_unix_ns'] <= right['started_unix_ns']
                    for left, right in zip(windows, windows[1:])), 'overlapping rule timing on one backend')
        starts[pid] = [record['started_unix_ns'] for record in windows]

    epoch = datetime(1970, 1, 1, tzinfo=timezone.utc)

    def stamp_ns(value):
        elapsed = datetime.fromisoformat(value.replace('Z', '+00:00')) - epoch
        return (elapsed.days * 86_400 + elapsed.seconds) * 1_000_000_000 + elapsed.microseconds * 1_000

    for line in snapshots:
        snapshot = json.loads(line)
        at = stamp_ns(snapshot['db']['at'])
        for row in snapshot['db']['activity']:
            pid = row['pid']
            if pid not in by_pid or row['state'] != 'active' or row['query_class'] != 'source_rules':
                continue
            index = bisect_right(starts[pid], at) - 1
            if index < 0:
                continue
            record = by_pid[pid][index]
            if not record['started_unix_ns'] <= stamp_ns(row['query_start']) <= at <= record['finished_unix_ns']:
                continue
            record['active_samples'] += 1
            if row['wait_event_type']:
                category = row['wait_event_type']
                counts = record['wait_samples']
                counts[category] = counts.get(category, 0) + 1
    return list(timings.values())


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
            and row.get('catalog_request_id') is None
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


def hidden_metadata(reader, item_id):
    result = reader.request('GET', f'/api/v1/meta/items/{item_id}', (404,)).payload
    require(set(result) <= {'error', 'error_code'} and bool(result.get('error')),
            'withdrawn metadata exposed a hidden DataItem')


def metadata_identity(reader, tenant, engine, name, facts, *, expected_id=None):
    rows = reader.request('GET', f'/api/v1/meta/engines/{engine}/items', (200,), response_type=list).payload
    require(isinstance(rows, list) and len(rows) == 1 and isinstance(rows[0], dict)
            and type(rows[0].get('id')) is int and rows[0]['id'] > 0,
            'metadata list leaked an adjacent identity or omitted the approved DataItem')
    row = rows[0]
    detail = reader.request('GET', f'/api/v1/meta/items/{row["id"]}', (200,)).payload
    root_keys = {'id', 'tenant_id', 'engine_id', 'node_id', 'item_type', 'name', 'full_name',
                 'fingerprint', 'scanned_at', 'scanned_depth', 'attributes'}
    forbidden = {'row_count', 'size_bytes', 'data_updated_at', 'statistics', 'samples', 'sample_data',
                 'description', 'comment', 'default', 'default_value', 'expression', 'generated_expression',
                 'extent', 'bbox', 'bounds', 'min', 'max', 'null_count'}
    def inspect(value):
        if isinstance(value, dict):
            require(not (set(value) & forbidden), 'metadata observation leaked content or statistics')
            for child in value.values(): inspect(child)
        elif isinstance(value, list):
            for child in value: inspect(child)
    for value in (row, detail):
        require(isinstance(value, dict) and set(value) <= root_keys and value.get('id') == row['id']
                and value.get('tenant_id') == tenant and value.get('name') == name
                and value.get('engine_id') == engine and value.get('full_name') == 'public.' + name
                and value.get('item_type') == 'table' and value.get('fingerprint') == facts['item_fingerprint'],
                'metadata observation changed identity or leaked non-structural fields')
        attributes = value.get('attributes', {})
        require(isinstance(attributes, dict) and set(attributes) <= {'schema_version', 'item', 'type_info', 'capabilities'},
                'metadata attributes leaked an unrecognized section')
        inspect(value)
    require(expected_id is None or row['id'] == expected_id, 'metadata Grant changed DataItem identity')
    return row['id']


def hidden_metadata_list(reader, engine):
    rows = reader.request('GET', f'/api/v1/meta/engines/{engine}/items', (200,), response_type=list).payload
    require(rows == [], 'ungranted metadata list exposed an identity')


def production_lineage_schemas(current, engine, source, output, source_path, output_path):
    facts = current.get('metadata', {}).get('lineage_facts')
    require(isinstance(facts, dict) and facts.get('schema_version') == 'addp.lineage-facts/v1',
            'production execution lineage evidence missing')
    schemas = {}
    for key, identity, path in (('inputs', source, source_path), ('outputs', output, output_path)):
        refs = facts.get(key)
        require(isinstance(refs, list) and len(refs) == 1 and isinstance(refs[0], dict),
                'production lineage must bind one exact input and output')
        ref = refs[0]
        require(isinstance(ref.get('locator'), str), 'execution lineage locator missing')
        uri = urlsplit(ref['locator'])
        query = parse_qs(uri.query)
        expected_path = '/' + str(engine) + '/path/' + '/'.join(segment['name'] for segment in path['segments'] if segment['name'])
        require(uri.scheme == 'addp' and uri.netloc == 'engine' and uri.path == expected_path
                and query.get('type') == ['table'] and not uri.fragment
                and ('item_id' not in query or query['item_id'] == [str(identity)])
                and ('item_id' not in ref or ref['item_id'] == identity),
                'execution schema evidence belongs to a different DataItem')
        schema = ref.get('schema_snapshot')
        require(isinstance(schema, dict) and re.fullmatch('sha256:[0-9a-f]{64}', schema.get('hash', ''))
                and isinstance(schema.get('fields'), list)
                and len(schema['fields']) == 1 and isinstance(schema['fields'][0], dict)
                and schema['fields'][0].get('name') == 'person_id',
                'production field schema evidence missing or changed')
        schemas[identity] = schema['hash']
    return schemas


class ProductionLineageObservation:
    """Observe the existing production relation through current User credentials only."""

    def __init__(self, reader, outsider, source_id, output_id, execution, schemas):
        require(type(source_id) is int and type(output_id) is int and source_id > 0
                and output_id > 0 and source_id != output_id, 'distinct lineage DataItem identities required')
        self.reader, self.outsider = reader, outsider
        self.source, self.output, self.execution = source_id, output_id, execution
        self.token = reader.token
        self.as_of = None
        require(set(schemas) == {source_id, output_id}, 'lineage schema identities differ')
        self.schemas = dict(schemas)
        # Closed diagnostics only: never serialize payloads, keys, error text or credentials.
        self.diagnostic = {'result': 'pending', 'operation': 'not_started'}

    def check(self, condition, assertion, message, **matches):
        if not condition:
            self.diagnostic.update(result='failed', assertion=assertion, matches=matches)
        require(condition, message)  # Keep the original exception and fail-closed behavior.

    def request(self, client, query, expected):
        service = query['subject_kind'] == 'published_service'
        self.diagnostic.update(result='pending', stage='request', assertion=None, matches={},
            location=None, graph=None, consumer='reader' if client is self.reader else 'outsider',
            granularity=query.get('granularity'), subject='service' if service else
                'source' if query['item_id'] == self.source else 'output',
            historical='as_of' in query, expected_status=expected, observed_status=None)
        try:
            response = client.request('GET', '/api/v1/meta/lineage/graph?' + urlencode(query), (expected,))
        except Exception:
            # The client may reject HTTP/JSON/transport before returning a response.
            # Do not guess a status or publish potentially private exception text.
            self.diagnostic.update(result='failed', assertion='request_failed')
            raise
        status = getattr(response, 'status', None)
        self.diagnostic.update(stage='response', observed_status=status if type(status) is int else None)
        return response.payload

    def query(self, client, granularity, *, root=None, expected=200, as_of=None):
        query = {'subject_kind': 'data_item' if granularity == 'item' else 'field_ref',
                 'granularity': granularity, 'item_id': self.output if root is None else root,
                 'direction': 'upstream', 'depth': 2, 'limit': 2}
        if granularity == 'field':
            query['field_name'] = 'person_id'
            query['schema_snapshot_hash'] = self.schemas[query['item_id']]
        if as_of is not None:
            query['as_of'] = as_of
        result = self.request(client, query, expected)
        if expected != 200:
            self.check(set(result) == {'error', 'error_code'} and bool(result['error'])
                    and result['error_code'] == 'lineage_subject_not_found',
                    'hidden_root_response', 'hidden lineage root must have the ordinary not-found response')
        return result

    def validate(self, graph, granularity, *, source_visible, pending=False):
        self.diagnostic.update(stage='validation', graph={
            'node_count': len(graph['nodes']) if isinstance(graph, dict) and isinstance(graph.get('nodes'), list) else None,
            'edge_count': len(graph['edges']) if isinstance(graph, dict) and isinstance(graph.get('edges'), list) else None})
        self.check(isinstance(graph, dict) and set(graph) <= {
                    'granularity', 'field_lineage_status', 'subject', 'nodes', 'edges', 'truncated', 'permission_filtered', 'as_of'}
                and graph.get('granularity') == granularity and graph.get('truncated') is False
                and graph.get('permission_filtered') is (not source_visible)
                and isinstance(graph.get('nodes'), list) and isinstance(graph.get('edges'), list),
                'graph_envelope', 'invalid or truncated lineage response',
                object=isinstance(graph, dict), allowed_keys=isinstance(graph, dict) and set(graph) <= {
                    'granularity', 'field_lineage_status', 'subject', 'nodes', 'edges', 'truncated', 'permission_filtered', 'as_of'},
                granularity=isinstance(graph, dict) and graph.get('granularity') == granularity,
                not_truncated=isinstance(graph, dict) and graph.get('truncated') is False,
                permission_filtered=isinstance(graph, dict) and graph.get('permission_filtered') is (not source_visible),
                nodes_list=isinstance(graph, dict) and isinstance(graph.get('nodes'), list),
                edges_list=isinstance(graph, dict) and isinstance(graph.get('edges'), list))
        kind = 'data_item' if granularity == 'item' else 'field_ref'
        allowed = {self.output, self.source} if source_visible else {self.output}
        def node(value, location):
            self.diagnostic['location'] = location
            self.check(isinstance(value, dict) and set(value) <= {
                'kind', 'item_id', 'item_fingerprint', 'engine_id', 'engine_name', 'item_type', 'name', 'full_name',
                'field_name', 'schema_snapshot_hash', 'field_lineage_status',
                'hidden_upstream_count', 'hidden_downstream_count'}
                and value.get('kind') == kind and type(value.get('item_id')) is int
                and value['item_id'] in allowed
                and value.get('hidden_upstream_count') == 0 and value.get('hidden_downstream_count') == 0,
                'node_identity', 'lineage leaked a hidden/service node or neighbor count',
                object=isinstance(value, dict), allowed_keys=isinstance(value, dict) and set(value) <= {
                    'kind', 'item_id', 'item_fingerprint', 'engine_id', 'engine_name', 'item_type', 'name', 'full_name',
                    'field_name', 'schema_snapshot_hash', 'field_lineage_status', 'hidden_upstream_count', 'hidden_downstream_count'},
                kind=isinstance(value, dict) and value.get('kind') == kind,
                permitted_item=isinstance(value, dict) and type(value.get('item_id')) is int and value['item_id'] in allowed,
                zero_hidden_counts=isinstance(value, dict) and value.get('hidden_upstream_count') == 0
                    and value.get('hidden_downstream_count') == 0)
            if granularity == 'field':
                self.check(value.get('field_name') == 'person_id'
                        and value.get('schema_snapshot_hash') == self.schemas[value['item_id']],
                        'field_identity', 'field lineage lost the exact field and schema identity',
                        field_name=value.get('field_name') == 'person_id',
                        schema_snapshot=value.get('schema_snapshot_hash') == self.schemas[value['item_id']])
        node(graph.get('subject'), 'subject')
        self.check(graph['subject']['item_id'] == self.output, 'root_identity', 'lineage root changed')
        for value in graph['nodes']:
            node(value, 'nodes')
        ids = [value['item_id'] for value in graph['nodes']]
        self.check(len(ids) == len(set(ids)) and self.output in ids, 'node_set', 'lineage nodes duplicated or root missing')
        for edge in graph['edges']:
            self.diagnostic['location'] = 'edges'
            self.check(isinstance(edge, dict) and set(edge) <= {
                        'source', 'target', 'relation_kind', 'granularity', 'status', 'last_observed_at', 'evidence'}
                    and edge.get('granularity') == granularity and edge.get('relation_kind') == 'derive'
                    and edge.get('status') == 'active', 'edge_shape', 'lineage leaked expressions or unexpected relation',
                    object=isinstance(edge, dict), allowed_keys=isinstance(edge, dict) and set(edge) <= {
                        'source', 'target', 'relation_kind', 'granularity', 'status', 'last_observed_at', 'evidence'},
                    granularity=isinstance(edge, dict) and edge.get('granularity') == granularity,
                    relation=isinstance(edge, dict) and edge.get('relation_kind') == 'derive',
                    active=isinstance(edge, dict) and edge.get('status') == 'active')
            node(edge.get('source'), 'edge_source')
            node(edge.get('target'), 'edge_target')
            self.check(edge['source']['item_id'] == self.source and edge['target']['item_id'] == self.output,
                    'edge_endpoints', 'lineage relation targets changed')
            evidence = edge.get('evidence', {})
            self.check(isinstance(evidence, dict) and set(evidence) <= {'execution_id', 'write_mode', 'field_lineage_status'}
                    and evidence.get('execution_id') == self.execution
                    and all(isinstance(value, str) for value in evidence.values()),
                    'edge_evidence', 'lineage evidence leaked details or belongs to another execution',
                    object=isinstance(evidence, dict), allowed_keys=isinstance(evidence, dict) and set(evidence) <= {
                        'execution_id', 'write_mode', 'field_lineage_status'},
                    execution=isinstance(evidence, dict) and evidence.get('execution_id') == self.execution,
                    string_values=isinstance(evidence, dict) and all(isinstance(value, str) for value in evidence.values()))
        complete = set(ids) == allowed and len(graph['edges']) == (1 if source_visible else 0)
        self.check(complete or (pending and source_visible and ids == [self.output] and not graph['edges']),
                'visible_endpoints', 'lineage did not exactly match the currently visible endpoints',
                exact_items=set(ids) == allowed, exact_edge_count=len(graph['edges']) == (1 if source_visible else 0))
        return complete

    def issued(self):
        self.diagnostic['operation'] = 'issued'
        deadline = time.monotonic() + 120
        while True:
            item_complete = self.validate(self.query(self.reader, 'item'), 'item', source_visible=True, pending=True)
            # An explicit schema hash must be proven by the collected execution.
            # Wait for that exact item relation before asking for its fields;
            # authorization/protocol errors are still immediate failures.
            if item_complete and self.validate(self.query(self.reader, 'field'), 'field', source_visible=True):
                break
            self.check(time.monotonic() < deadline, 'collection_deadline', 'production lineage was not collected before deadline')
            time.sleep(1)
        self.as_of = datetime.now(timezone.utc).isoformat()
        for level in ('item', 'field'):
            self.query(self.outsider, level, expected=404)
        response = self.request(self.reader, {
            'subject_kind': 'published_service', 'service_id': 1, 'revision': 'test'}, 403)
        self.check(set(response) <= {'error', 'error_code'} and bool(response.get('error')),
                'service_root_response', 'service lineage denial leaked facts')

    def source_withdrawn(self):
        self.diagnostic['operation'] = 'source_withdrawn'
        self.check(self.as_of is not None, 'visibility_established', 'lineage visibility was not established before withdrawal')
        for level in ('item', 'field'):
            for at in (None, self.as_of):
                self.validate(self.query(self.reader, level, as_of=at), level, source_visible=False)
            self.query(self.reader, level, root=self.source, expected=404)

    def output_withdrawn(self, report):
        self.diagnostic['operation'] = 'output_withdrawn'
        for level in ('item', 'field'):
            self.query(self.reader, level, expected=404)
        self.check(self.reader.token == self.token, 'same_credential', 'lineage acceptance changed the consumer credential')
        self.diagnostic.update(result='passed', stage='complete', assertion=None, matches={})
        report['lineage_visibility'] = {'source_item_id': self.source, 'output_item_id': self.output,
            'execution_id': self.execution, 'visible_item_nodes': 2, 'visible_field_nodes': 2,
            'item_edges': 1, 'field_edges': 1, 'outsider_status': 404, 'service_root_status': 403,
            'source_withdrawn_nodes': 1, 'source_withdrawn_edges': 0,
            'history_uses_current_visibility': True, 'output_withdrawn_status': 404, 'same_credential': True}
        report['checks'].extend(LINEAGE_CHECKS)


def preview(reader, target, allowed, expected=None, *, columns=('person_id',), values=('A', 'B')):
    result = reader.request('GET', '/api/v1/manager/preview?' + urlencode({
        'locator': target, 'page': 1, 'page_size': 20}), (200,) if allowed else (403,)).payload
    if not allowed:
        require(set(result) <= {'error', 'error_code'} and bool(result.get('error')),
                'denied preview exposed data or metadata')
        require(result.get('error_code') != 'security_protection_required', 'Security failure is not source denial')
        return
    data = result.get('data', {})
    rows = data.get('rows', [])
    require(result.get('preview_type') == 'table' and data.get('columns') == list(columns)
            and sorted(row.get(columns[0]) for row in rows) == list(values),
            'Manager did not read the deterministic PG source')
    if expected is not None:
        total = data.get('total')
        require(len(rows) == expected and 'total' in data and (
            (type(total) is int and total == expected) or (total is None and data.get('has_more') is False)),
            'PG source preview is incomplete or its row count changed')


def mutate_source_content(action):
    """Ask the Business owner to mutate only its disposable PG source table."""
    repository = Path(__file__).resolve().parents[2]
    required = {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOSTED': '1',
                'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_TEST': '1', 'POSTGRES_DB': 'addp_online'}
    require(action in {'content-change', 'content-reset', 'node-future-create'} and sys.platform == 'linux'
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
    receipt = {'content-change': 'pg-content:changed', 'content-reset': 'pg-content:baseline', 'node-future-create': 'pg-node:created'}[action]
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
    """The operator navigates without writes, then withdraws its exact Grant."""
    def __init__(self, tenant, principal, target, engine, issuer, grant):
        super().__init__(tenant, principal, target)
        self.binding.update(engine_id=str(engine), issuer_id=str(issuer), grant_id=grant)
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
                        checks=WORKSPACE_CHECKS, login_requests=2, preview_statuses=[200, 403])
        require(result == expected, 'PG browser workspace evidence is incomplete or changed identity')
        return result


def collection_instant(value):
    """One RFC3339 interpretation, without Python's fractional-second truncation."""
    match = re.fullmatch(r'(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})', value) if type(value) is str else None
    require(match is not None, 'invalid scan timestamp')
    try:
        whole = datetime.fromisoformat(match[1] + match[3].replace('Z', '+00:00')).astimezone(timezone.utc)
    except ValueError:
        raise SuiteError('invalid scan timestamp') from None
    return whole, int((match[2] or '').ljust(9, '0'))


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
                and type(row['kind']) is str and row['kind'] in {'version_probe', 'unclassified', 'empty_query', 'empty_comment_ping'}
                and (row['category'] == 'protocol_empty') == (row['kind'] in {'empty_query', 'empty_comment_ping'})
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
                'kind',CASE WHEN sql=';' THEN 'empty_query' WHEN sql='-- ping' THEN 'empty_comment_ping'
                  WHEN btrim(sql)='SELECT version()' THEN 'version_probe' ELSE 'unclassified' END,
                'category',CASE WHEN sql IN (';','-- ping') THEN 'protocol_empty'
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

    def open(self, *, tables=('metric_people',)):
        require(type(tables) is tuple and tables and len(set(tables)) == len(tables)
                and set(tables) <= {'metric_people', 'metric_events'}, 'source lock requires exact owned fixture tables')
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
        self.process.stdin.write(('BEGIN; LOCK TABLE ' + ','.join('public.' + name for name in tables) + ''' IN ACCESS EXCLUSIVE MODE;
            SELECT json_build_object('pid',pg_backend_pid(),'locked',true,'at',clock_timestamp());
''').encode())
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
    """Tenant PG basic plans: current human management, tenant scheduling, explicit cancellation."""
    def __init__(self, lifecycle, issuer, run_id):
        self.life, self.issuer = lifecycle, issuer
        self.prefix = 'pg_collection_' + run_id + '_' + uuid.uuid4().hex[:12]
        self.tasks, self.executions = {}, {}
        self.attempts = []
        self.source_barriers = []

    def task_rows(self):
        return self.life.operator.request('GET', '/api/v1/meta/scan/tasks', (200,), response_type=list).payload

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

    def create_task(self, suffix):
        name = self.prefix + suffix
        body = {'name': name, 'description': 'Disposable centralized PG basic acceptance',
                'engine_id': self.life.engine, 'scope': {'type': 'engine'}, 'scan_depth': 'basic',
                'force': False, 'schedule': '', 'enabled': False}
        require(not any(row.get('name') == name for row in self.task_rows()), 'collection task already exists')
        self.tasks[name] = {'id': None, 'body': body}  # Own the unique name before POST, including unknown outcome.
        row = self.life.operator.request('POST', '/api/v1/meta/scan/tasks', (201,), body).payload
        self.recover_tasks()
        require(self.tasks[name]['id'] == row.get('id'), 'created collection task was not recovered exactly')
        return self.tasks[name]

    def update(self, task, **changes):
        task['body'] = dict(task['body'], **changes)
        row = self.life.operator.request('PUT', f'/api/v1/meta/scan/tasks/{task["id"]}', (200,), task['body']).payload
        require(row.get('id') == task['id'], 'task update changed identity')

    def observed_request(self, attempt, method, path, *, client=None, expected=(200,), body=None):
        try:
            result = (self.life.operator if client is None else client).request(method, path, expected, body)
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
        attempt = {'task_id': str(task), 'expected_terminal': 'cancelled' if allowed == 'cancelled' else 'success' if allowed else 'failed', 'stage': 'trigger'}
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
                                 run.get('triggered_by') == int(self.issuer)),
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
                require(run.get('status') == ('cancelled' if allowed == 'cancelled' else 'success' if allowed else 'failed') and run.get('completed_at'),
                        'collection execution did not reach the expected terminal state')
                attempt['stage'] = 'source_result' if allowed else 'authorization_denial'
                if allowed == 'cancelled':
                    require(metadata.get('items_scanned') == admitted_items and metadata.get('catalog_nodes_scanned') == 1 and metadata.get('fields_scanned') == 0, 'cancelled scan lost admitted results or read another catalog')
                elif allowed:
                    require(type(metadata.get('items_scanned')) is int and metadata['items_scanned'] >= 2
                            and type(metadata.get('catalog_nodes_scanned')) is int and metadata['catalog_nodes_scanned'] > 0,
                            'central basic task did not enumerate the actual engine')
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

    def scheduled_rows(self, task):
        result = self.life.operator.request('GET', f'/api/v1/meta/scan/runs?task_id={task}&page_size=100', (200,)).payload
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
                and rows[0].get('schedule', '') == owned['body']['schedule'], 'collection schedule did not stop')
        owned['body']['enabled'] = False

    def scheduled_run(self, owned, allowed, evidence, label):
        task = owned['id']
        known = {run for run, owner in self.executions.items() if owner == task}
        require({row['execution_id'] for row in self.scheduled_rows(task)} == known,
                'unexpected execution outside scheduled phase')
        attempt = {'task_id': str(task), 'expected_terminal': 'cancelled' if allowed == 'cancelled' else 'success' if allowed else 'failed',
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


    def run(self, report):
        report['phase'] = 'meta_central_collection'
        evidence = report['meta_collection'] = {'executions': {}, 'attempts': self.attempts}
        before = self.source_snapshot()
        task = self.create_task('_primary')
        evidence['task_id'] = str(task['id'])
        # Both reader and outsider have the execute function but no engine management
        # scope. Neither may create an execution, even if it can read data.
        prior = self.scheduled_rows(task['id'])
        for actor in (self.life.reader, self.life.outsider):
            response = actor.request('POST', f'/api/v1/meta/scan/tasks/{task["id"]}/trigger', (403,))
            require(response.payload.get('error_code') == 'permission_denied', 'management denial is not explicit')
        require(self.scheduled_rows(task['id']) == prior, 'denied trigger created an execution')
        report['checks'].append(COLLECTION_CHECKS[0])
        for label in ('first', 'repeat'):
            evidence['executions'][label] = self.trigger(task['id'], True)
        report['checks'].append(COLLECTION_CHECKS[1])
        invalid = dict(task['body'], scan_depth='deep')
        self.life.operator.request('PUT', f'/api/v1/meta/scan/tasks/{task["id"]}', (403,), invalid)
        current = [row for row in self.task_rows() if row.get('id') == task['id']]
        require(len(current)==1 and current[0].get('parameters',{}).get('scan_depth')=='basic', 'rejected config change persisted')
        report['checks'].append(COLLECTION_CHECKS[2])
        self.update(task, force=True)
        evidence['executions']['changed_basic'] = self.trigger(task['id'], True)
        report['checks'].append(COLLECTION_CHECKS[3])
        require(self.source_snapshot()==before, 'central collection changed source grants')
        evidence['source_rules_unchanged']=True
        report['checks'].append(COLLECTION_CHECKS[4])

    def run_scheduled(self, report):
        report['phase']='meta_central_scheduled'
        evidence=report['meta_collection']['scheduled']={'executions':{}}
        before=self.source_snapshot()
        task=self.create_task('_scheduled')
        for label in ('first','repeat'):
            self.scheduled_run(task,True,evidence,label)
        require(self.source_snapshot()==before,'tenant schedule changed data authorization')
        report['checks'].append('meta_central_tenant_schedule_repeat')

    def cancel(self, execution):
        result=self.life.operator.request('POST',f'/api/v1/meta/executions/{execution}/cancel',(200,202)).payload
        require(result.get('execution_id')==execution and result.get('status') in {'running','cancelled'},'cancellation target or state changed')
        return result

    def run_inflight(self, report):
        report['phase']='meta_central_inflight_cancel'
        evidence=report['meta_collection']['inflight']={'executions':{}}
        before=self.source_snapshot()
        task=self.create_task('_inflight')
        barrier=PGCollectionReadBarrier()
        self.source_barriers.append(barrier)
        evidence['lock']=barrier.open()
        execution,attempt=self.start_execution(task['id'],'cancelled')
        evidence['blocked']=barrier.wait_blocked()
        running=self.life.operator.request('GET',f'/api/v1/meta/executions/{execution}',(200,)).payload
        require(running.get('status')=='running' and running.get('source_task_id')==str(task['id']) and running.get('triggered_by')==int(self.issuer) and not running.get('completed_at'),'no exact admitted running execution')
        evidence['cancellation']=self.cancel(execution)
        require(evidence['cancellation']['status']=='running','admitted enumeration was terminated before release')
        barrier.release()
        admitted=report['meta_collection']['scheduled']['executions']['repeat']['items_scanned']
        evidence['executions']['inflight']=self.observe_execution(task['id'],execution,'cancelled',attempt,admitted_items=admitted)
        counts=barrier.counts()
        evidence.update(source_counter_baseline=barrier.baseline,source_counter_observed=counts)
        delta={key:counts['calls'].get(key,0)-barrier.baseline['calls'].get(key,0) for key in ('namespaces','tables','workspace_detection','other')}
        evidence['source_call_delta']=delta
        # Retain connection attribution even when the original strict counter check fails.
        evidence['connection_observation']=barrier.connection_observation(barrier.baseline,counts,evidence['blocked'])
        require(counts['reset_at']==barrier.baseline['reset_at'] and delta=={'namespaces':1,'tables':1,'workspace_detection':2,'other':0},'source read beyond admitted enumeration')
        require(evidence['connection_observation']['verified'] is True,'source attribution incomplete')
        report['checks'].append('meta_central_cancel_stops_next_catalog')
        barrier.close()
        require(barrier.closed and barrier.released and self.source_snapshot()==before,'source cleanup failed or rules changed')
        evidence['source_fixture_cleanup']='zero_source_locks_and_schema'
        report['checks'].append('meta_central_inflight_fixture_cleanup')

    def cleanup(self):
        errors=[]
        ownership_verified=False
        try:
            rows=self.recover_tasks()
            ownership_verified=True
            for owned in self.tasks.values():
                if owned['id'] is not None and any(row.get('id')==owned['id'] for row in rows):
                    self.pause_schedule(owned)
                    for run in self.scheduled_rows(owned['id']):
                        if run.get('status') in {'pending','running'}: self.cancel(run['execution_id'])
        except Exception: errors.append('collection pause/cancel cleanup failed')
        for barrier in self.source_barriers:
            try: barrier.release()
            except Exception: errors.append('collection source lock release failed')
        for owned in self.tasks.values() if ownership_verified else []:
            try:
                task=owned['id']
                if task is None: continue
                deadline=time.monotonic()+120
                while any(row.get('status') in {'pending','running'} for row in self.scheduled_rows(task)):
                    require(time.monotonic()<deadline,'active collection execution remains')
                    time.sleep(1)
                current=[row for row in self.task_rows() if row.get('id')==task]
                require(not current or current[0].get('enabled') is False,'enabled schedule remains')
                if current:
                    try: self.life.operator.request('DELETE',f'/api/v1/meta/scan/tasks/{task}',(200,))
                    except Exception: require(not any(row.get('id')==task for row in self.task_rows()),'unknown delete did not commit')
                require(not any(row.get('id')==task for row in self.task_rows()),'owned task remains')
            except Exception: errors.append('collection task cleanup failed')
        if not errors:
            for barrier in self.source_barriers:
                try: barrier.close()
                except Exception: errors.append('collection source fixture cleanup failed')
        require(not errors,'; '.join(errors))


def manager_pg_runtime(login, tenant):
    """Only the already-provisioned Manager runtime; no human role or Grant changes."""
    secret = os.environ['MANAGER_SERVICE_CLIENT_SECRET']
    basic = base64.b64encode(('addp-manager:' + secret).encode()).decode()
    payload = login.request('POST', '/api/v1/system/oauth/token', (200,),
                            encoded_body=urlencode({'grant_type': 'client_credentials', 'audience': 'addp.api',
                                                    'scope': 'addp.api',
                                                    'tenant_id': str(tenant)}).encode(),
                            content_type='application/x-www-form-urlencoded',
                            authorization_header='Basic ' + basic).payload
    require(isinstance(payload.get('access_token'), str) and payload.get('token_type', '').lower() == 'bearer',
            'Manager runtime token response invalid')
    runtime = API.GatewayClient(login.base_url, payload['access_token'], login.timeout)
    current = runtime.request('GET', '/api/v1/system/auth/context', (200,)).payload
    require(current.get('principal', {}).get('type') == 'service_principal'
            and current.get('client', {}).get('client_id') == 'addp-manager'
            and current.get('context', {}).get('type') == 'tenant'
            and current['context'].get('tenant_id') == str(tenant)
            and 'system.manager_pg_read_check.execute' in {p for r in tenant_functional_assignments(current, tenant) for p in r['permissions']},
            'Manager runtime does not have its exact tenant handoff qualification')
    return runtime, current['principal']['id']


def pg_read_bindings(paths, facts):
    require(len(paths) == len(facts) and 0 < len(paths) <= 200, 'complete PG read facts required')
    result = []
    for path, fact in zip(paths, facts):
        require(fact.get('basis_state') == 'complete'
                and fact.get('boundary') == {'layout': 'single', 'native_target': path},
                'PG handoff must use the formal Meta native table boundary')
        result.append({key: fact[key] for key in ('item_fingerprint', 'boundary_hash')}
                      | {'native_target': path})
    return result


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
        self.grants = []
        self.recipients = {('user', recipient)}
        self.source_observations = []
        self.expiry_grant = None
        self.production_tasks = {}
        self.collection = None
        self.scoped_refresh = None
        self.pg_runtime = None
        self.pg_runtime_principal = None

    def production_history(self, path, *, state=None):
        # Path history includes covering node Grants. Narrow candidates using
        # the unique output name; ownership still requires the full bindings.
        return self.listing(history=True, state=state, table_search=path['segments'][-1]['name'])

    def collect_production_grants(self):
        # Resolve even an unknown start response using only executions of our
        # captured task definition, never an arbitrary output Grant or prefix.
        for task, path in self.production_tasks.items():
            executions = self.reader.request('GET', f'/api/v1/transfer/task-definitions/{task}/executions?page_size=100', (200,)).payload
            require(executions.get('total') == len(executions.get('data', [])), 'production history was truncated')
            ids = {row['execution_id'] for row in executions['data'] if row.get('task_id') == task}
            rows = self.production_history(path)
            require(rows.get('total') == len(rows.get('data', [])), 'production Grant history was truncated')
            for row in rows['data']:
                require(row.get('approval_mode') == 'task_production' and row.get('producer_owner') == 'transfer'
                        and row.get('production_execution_id') in ids and row.get('catalog_path') == path
                        and row.get('recipient_type') == 'user' and row.get('recipient_id') == self.recipient,
                        'refusing to own a foreign output Grant')
                if row['request_id'] not in self.grants:
                    self.grants.append(row['request_id'])

    def production_flow(self, report, *, cpu_arm=''):
        report['phase'] = 'initial_output_authorization'
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        source_grant, source_body = self.grant(facts, seconds=600)
        source = item(self.reader, self.engine, 'metric_people')
        name = 'pg_initial_' + uuid.uuid4().hex
        path = table_path(self.engine, name)
        self.paths.append(path)  # Register the exact cleanup target before any task write.
        existing = self.operator.request('GET', self.base + '/data-items?page=1&page_size=100', (200,)).payload
        require(isinstance(existing.get('data'), list) and existing.get('total') == len(existing['data'])
                and existing.get('page') == 1 and existing.get('page_size') == 100,
                'authorizer identity inventory is incomplete')
        require(not any(row.get('full_name') == 'public.' + name for row in existing['data']),
                'output already exists in Meta')
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
        rows = self.production_history(path, state='active')
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
        report['phase'] = 'production_lineage_schema_evidence'
        schemas = production_lineage_schemas(current, self.engine, source['id'], output['id'], self.path, path)
        lineage = ProductionLineageObservation(self.reader, self.outsider, source['id'], output['id'], execution, schemas)
        report['phase'] = 'production_lineage_visibility'
        report['lineage_diagnostic'] = lineage.diagnostic
        lineage.issued()
        report['phase'] = 'concurrent_and_batch_read_load'
        with pg_load_cpu_window(cpu_arm):
            self.measure_read_load(report, [(self.path, facts, source_grant), (path, output_facts, row['request_id'])],
                                   profile=CPU_LOAD_PROFILE if cpu_arm else 'full')
        report['phase'] = 'output_source_withdrawal'
        self.revoke(source_grant)
        self.require_batch_denied(self.reader, [self.path, path])
        report['authorization_load']['mixed_withdrawn_batch_denied'] = True
        lineage.source_withdrawn()
        preview(self.reader, locator(self.engine, 'metric_people', source['id']), False)
        preview(self.reader, target, True, 2)
        require(self.production_history(path, state='active') == rows,
                'source withdrawal changed the output Grant')
        self.revoke(row['request_id'])
        preview(self.reader, target, False)
        revoked = self.production_history(path, state='revoked')
        require(revoked['total'] == 1 and revoked['data'][0]['request_id'] == row['request_id']
                and revoked['data'][0].get('revocation'), 'output revocation history missing')
        report['initial_production'] = {'task_id': task_id, 'execution_id': execution,
            'grant_id': row['request_id'], 'item_fingerprint': output_facts['item_fingerprint'],
            'boundary_hash': output_facts['boundary_hash'], 'scan_execution_id': children[0]['execution_id'],
            'scan_depth': 'basic', 'rows': 2, 'inherited_expiry': row['expires_at'],
            'source_withdrawn_output_readable': True, 'output_withdrawn_read_denied': True}
        report['checks'].extend(PRODUCTION_CHECKS)
        lineage.output_withdrawn(report)

    def listing(self, *, history=False, state=None, recipient_type='user', recipient_id=None,
                catalog_path=None, table_search=None):
        query = {'recipient_type': recipient_type, 'recipient_id': self.recipient if recipient_id is None else recipient_id,
                 'page_size': 100}
        if table_search is not None:
            require(catalog_path is None, 'Grant filters must not combine path and search')
            query['table_search'] = table_search
        else:
            query['catalog_path'] = json.dumps(self.path if catalog_path is None else catalog_path, separators=(',', ':'))
        if state:
            query['state'] = state
        return self.operator.request('GET', self.base + '/access_grants' + ('/history' if history else '')
                                     + '?' + urlencode(query), (200,)).payload

    def grant_command(self, facts, *, seconds=300, recipient_type='user', recipient_id=None,
                      catalog_path=None, action='read'):
        require(action in {'read', 'metadata_read'}, 'unsupported isolated Grant action')
        path = self.path if catalog_path is None else catalog_path
        command = str(uuid.uuid4())
        expires = datetime.now(timezone.utc) + timedelta(seconds=seconds)
        body = {'request_id': command, 'catalog_path': path,
                'item_fingerprint': facts['item_fingerprint'],
                'expected_boundary_hash': facts['boundary_hash'], 'recipient_type': recipient_type,
                'recipient_id': self.recipient if recipient_id is None else recipient_id,
                'action': action, 'expiry_mode': 'at_time', 'expires_at': expires.isoformat(),
                'reason': 'Disposable PG source authorization lifecycle'}
        self.grants.append(command)  # Track before the write, including unknown outcomes.
        self.recipients.add((body['recipient_type'], body['recipient_id']))
        return command, body

    def grant(self, facts, **options):
        command, body = self.grant_command(facts, **options)
        receipt = self.operator.request('POST', self.base + '/access_grants', (201,), body).payload
        for key in ('request_id', 'catalog_path', 'item_fingerprint',
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

    def read_check(self, user, targets, expected, sources=None):
        require(self.pg_runtime is not None, 'Manager PG runtime was not initialized')
        if sources is None:
            facts = [REGISTRATION.read_exact_record_authorization_facts(self.operator, path) for path in targets]
            sources = pg_read_bindings(targets, facts)
        return self.pg_runtime.request('POST', READ_CHECK, expected,
                                       {'user_credential': user.token, 'sources': sources})

    def require_batch_denied(self, client, targets):
        response = self.read_check(client, targets, (403,))
        require(response.status == 403 and set(response.payload) == {'error'}
                and isinstance(response.payload.get('error'), str) and bool(response.payload['error']),
                'batch denial must not expose partial data or substitute another boundary')

    def measure_read_load(self, report, bindings, *, profile='full'):
        observation = {}
        report['authorization_load'] = {'result': 'failed', 'audit_observer_enabled': True,
                                        'audit_observation': observation}
        with AuditLatencyObservation(observation, self.pg_runtime_principal, phase='load') as observer:
            self.sample_read_load(report, bindings, observer, observation, profile=profile)
        require(observation.get('result') == 'passed' and observation.get('connection_closed') is True,
                'read load observation did not finish cleanly')
        result = report['authorization_load']
        verify_load_audit_windows(result, observation['audit_rows'])
        result['result'] = 'passed'

    def sample_read_load(self, report, bindings, observer, observation, *, profile='full'):
        """Bounded closed-loop HTTP samples, using only already-issued fixture Grants."""
        require(profile in {'full', CPU_LOAD_PROFILE}, 'invalid PG authorization load profile')
        result = {'schema_version': 'addp.authorization-load/v1', 'result': 'failed', 'profile': profile,
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
            if time.monotonic() >= deadline:
                result['failure_code'] = 'read_load_budget_exceeded'
                raise SuiteError('read load exceeded its budget')
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
        source_map = {json.dumps(path, sort_keys=True): binding for path,binding in zip(targets,pg_read_bindings(targets,[facts for _,facts,_ in bindings]))}
        self.require_batch_denied(self.outsider, targets)
        result['outsider_batch_denied'] = True
        modes = [('single_target', [[targets[0]]])]
        if profile == 'full':
            modes.extend([('two_target_batch', [targets]), ('two_target_separate', [[targets[0]], [targets[1]]])])
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
                                response = self.read_check(self.reader,group,(200,),[source_map[json.dumps(path,sort_keys=True)] for path in group])
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
        sources = pg_read_bindings([self.path],[facts])

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
            with AuditLatencyObservation(observation, self.pg_runtime_principal) as observer:
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
                            response = self.read_check(self.reader,[self.path],(200,),sources)
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
        target = locator(self.engine, 'metric_people')
        command, body = self.grant(facts, recipient_id=principal)
        issued = self.listing(history=True, state='active', recipient_id=principal)
        require(issued.get('total') == 1 and len(issued.get('data', [])) == 1
                and issued['data'][0].get('request_id') == command
                and all(issued['data'][0].get(key) == body[key] for key in (
                    'catalog_path', 'item_fingerprint',
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
        command, body = self.grant(facts, seconds=300)
        target = locator(self.engine, 'metric_people', item(self.reader, self.engine, 'metric_people')['id'])
        with WorkspaceObservation(self.tenant, self.recipient, target, self.engine, issuer, command) as browser:
            report['workspace_browser'] = browser.finish()
        preview(self.reader, target, False)
        require(self.listing()['total'] == 0, 'browser withdrawal left current source rules')
        history = self.listing(history=True, state='revoked')
        require(any(row.get('request_id') == command and row.get('revocation') for row in history['data']),
                'browser withdrawal did not preserve its exact Grant history')
        report['checks'].extend(WORKSPACE_CHECKS)

    def cleanup(self):
        errors = []
        if self.scoped_refresh is not None:
            try:
                self.scoped_refresh.close()
            except Exception:
                errors.append('scoped refresh cleanup failed')
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

    def node_grant(self, path, *, recursive, action='read', seconds=300):
        require(type(recursive) is bool and action in {'read', 'metadata_read'}, 'explicit node scope required')
        command = str(uuid.uuid4())
        body = {'request_id': command, 'catalog_path': path, 'node_recursive': recursive,
                'recipient_type': 'user', 'recipient_id': self.recipient, 'action': action,
                'expiry_mode': 'at_time', 'expires_at': (datetime.now(timezone.utc) + timedelta(seconds=seconds)).isoformat(),
                'reason': 'Disposable PG persistent node scope'}
        self.grants.append(command)
        if path not in self.paths: self.paths.append(path)
        receipt = self.operator.request('POST', self.base + '/access_grants', (201,), body).payload
        require(all(receipt.get(key) == body[key] for key in ('request_id', 'catalog_path', 'node_recursive', 'recipient_type', 'recipient_id', 'action', 'expiry_mode'))
                and type(receipt.get('node_recursive')) is bool and receipt.get('approval_mode') == 'independent'
                and receipt.get('granted_at') and receipt.get('revocation') is None
                and not receipt.get('item_fingerprint') and not receipt.get('boundary_hash')
                and datetime.fromisoformat(receipt['expires_at'].replace('Z', '+00:00')) == datetime.fromisoformat(body['expires_at']),
                'persistent node Grant changed its scope or gained an item binding')
        return command, body

    def node_scope_flow(self, report):
        report['phase'] = 'persistent_pg_node_scope'
        token = self.reader.token
        schema = dict(self.path, segments=self.path['segments'][:2])
        root = dict(self.path, segments=self.path['segments'][:1])
        future = table_path(self.engine, 'metric_node_future')
        self.paths.append(future)  # Own the exact cleanup path before source creation.
        for path in self.paths:
            for kind, recipient in self.recipients:
                require(self.listing(history=True, state='active', catalog_path=path, recipient_type=kind, recipient_id=recipient)['total'] == 0,
                        'node scope scenario requires zero current source rules')
        nodes = self.operator.request('POST', self.base + '/authorization-nodes/query', (200,),
                {'target': schema, 'page': 1, 'page_size': 100}).payload
        require(nodes == {'parent': {'tenant_id': self.tenant, 'target': schema, 'exists': True},
                          'nodes': [], 'total': 0, 'page': 1, 'page_size': 100},
                'schema navigation is not an identity-only node page')
        def visible(name, facts, expected_id=None):
            row = item(self.reader, self.engine, name)
            require(row.get('tenant_id') == self.tenant and row.get('engine_id') == self.engine
                    and row.get('fingerprint') == facts['item_fingerprint']
                    and (expected_id is None or row['id'] == expected_id), 'node metadata identity changed')
            return row['id']
        meta, _ = self.node_grant(schema, recursive=False, action='metadata_read')
        hidden_metadata_list(self.outsider, self.engine)
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        visible('metric_people', facts)
        preview(self.reader, locator(self.engine, 'metric_people'), False)
        direct, _ = self.node_grant(root, recursive=False)
        preview(self.reader, locator(self.engine, 'metric_people'), False)
        self.revoke(direct)  # A non-recursive server never reaches schema tables.
        read, body = self.node_grant(root, recursive=True)
        preview(self.reader, locator(self.engine, 'metric_people'), True, 2)
        mutate_source_content('node-future-create')  # Only the disposable Business owner writes source SQL.
        scan = basic_scan(self.operator, self.engine, ['metric_node_future'])
        future_facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, future)
        future_item = visible('metric_node_future', future_facts)
        target = locator(self.engine, 'metric_node_future', future_item)
        preview(self.reader, target, True, 2, values=('N1', 'N2'))
        preview(self.outsider, target, False)
        history = self.listing(history=True, catalog_path=future)
        require(history['total'] == 2 and len(history['data']) == 2
                and {row['request_id'] for row in history['data']} == {meta, read}
                and all(row.get('node_recursive') is not None and row['catalog_path'] in (root, schema)
                        and not row.get('item_fingerprint') for row in history['data']),
                'future item created a leaf Grant or lost original node scope history')
        self.revoke(read)
        visible('metric_node_future', future_facts, expected_id=future_item)
        preview(self.reader, target, False)
        # The original revoked command may be observed, but never renewed.
        replay = self.operator.request('POST', self.base + '/access_grants', (201,), body).payload
        require(replay.get('request_id') == read and replay.get('revocation'), 'node replay restored withdrawn access')
        preview(self.reader, target, False)
        expiring, _ = self.node_grant(root, recursive=True, seconds=45)
        preview(self.reader, target, True, 2, values=('N1', 'N2'))
        deadline = time.monotonic() + 75
        while time.monotonic() < deadline:
            rows = self.listing(history=True, state='expired', catalog_path=root)['data']
            expired = [row for row in rows if row.get('request_id') == expiring]
            if expired: break
            time.sleep(1)
        else: raise SuiteError('persistent node Grant did not naturally expire')
        require(len(expired) == 1 and expired[0].get('node_recursive') is True and expired[0].get('revocation') is None,
                'node natural expiry was replaced by withdrawal')
        preview(self.reader, target, False)
        visible('metric_node_future', future_facts, expected_id=future_item)
        self.revoke(meta)
        hidden_metadata(self.reader, future_item)
        require(self.reader.token == token and self.listing(catalog_path=future)['total'] == 0,
                'node scope changed credentials or left current sources')
        report['node_scope'] = {'metadata_grant': meta, 'nonrecursive_server_grant': direct, 'recursive_grant': read,
            'expiry_grant': expiring, 'future_item_id': future_item, 'future_basic_scan': scan,
            'same_credential': True, 'future_item_rows': 2, 'no_leaf_grants': True, 'natural_expiry_without_revocation': True}
        report['checks'].extend(['node_identity_page_without_items', 'node_metadata_only_cannot_read',
            'node_nonrecursive_server_excludes_schema_tables', 'node_future_item_covered_without_leaf_grant',
            'node_ungranted_account_denied', 'node_withdrawal_preserves_metadata',
            'node_replay_cannot_restore_withdrawn_scope', 'node_natural_expiry_denies_read'])

    def metadata_access_flow(self, report, business, confirmer):
        report['phase'] = 'metadata_access_boundary'
        token = self.reader.token
        context = self.reader.request('GET', '/api/v1/system/auth/context', (200,)).payload
        evidence = report['metadata_access'] = {'direct': {}, 'catalog': {}}
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        require(self.listing(history=True, state='active')['total'] == 0, 'metadata scenario requires no current Grant')
        hidden_metadata_list(self.reader, self.engine)
        hidden_metadata_list(self.outsider, self.engine)
        target = locator(self.engine, 'metric_people')
        preview(self.reader, target, False)
        meta, _ = self.grant(facts, action='metadata_read')
        active = self.listing(history=True, state='active')
        require(active['total'] == 1 and active['data'][0]['action'] == 'metadata_read'
                and active['data'][0]['request_id'] == meta, 'metadata Grant history mismatch')
        item_id = metadata_identity(self.reader, self.tenant, self.engine, 'metric_people', facts)
        hidden_metadata_list(self.outsider, self.engine)
        preview(self.reader, target, False)
        read, _ = self.grant(facts, action='read')
        preview(self.reader, target, True, 2)
        metadata_identity(self.reader, self.tenant, self.engine, 'metric_people', facts, expected_id=item_id)
        self.revoke(read)
        require(self.listing(history=True, state='active') == active,
                'withdrawing read changed the independent metadata Grant')
        metadata_identity(self.reader, self.tenant, self.engine, 'metric_people', facts, expected_id=item_id)
        preview(self.reader, target, False)
        self.revoke(meta)
        hidden_metadata(self.reader, item_id)
        hidden_metadata_list(self.reader, self.engine)
        preview(self.reader, target, False)
        expiring, _ = self.grant(facts, action='metadata_read', seconds=45)
        metadata_identity(self.reader, self.tenant, self.engine, 'metric_people', facts, expected_id=item_id)
        deadline = time.monotonic() + 75
        while time.monotonic() < deadline:
            expired = self.listing(history=True, state='expired')
            rows = [row for row in expired['data'] if row.get('request_id') == expiring]
            if rows: break
            time.sleep(1)
        else: raise SuiteError('metadata Grant did not naturally expire')
        require(len(rows) == 1 and rows[0].get('action') == 'metadata_read' and rows[0].get('revocation') is None,
                'metadata expiry was replaced or manually revoked')
        require(self.listing(history=True, state='active')['total'] == 0, 'expired metadata Grant is still active')
        hidden_metadata(self.reader, item_id)
        hidden_metadata_list(self.reader, self.engine)
        preview(self.reader, target, False)
        evidence['direct'] = {'item_id': item_id, 'item_fingerprint': facts['item_fingerprint'],
            'boundary_hash': facts['boundary_hash'], 'metadata_grant': meta, 'read_grant': read,
            'expired_grant': expiring, 'metadata_unchanged_after_read_withdrawal': True}

        report['phase'] = 'catalog_metadata_approval'
        path = table_path(self.engine, 'metric_events')
        if path not in self.paths: self.paths.append(path)  # Own cleanup before any possible issuance.
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, path)
        route = '/api/v1/catalog/entries/' + report['catalog_approval']['entry_id']
        entry = business.request('GET', route, (200,)).payload
        decision_id, request_id = str(uuid.uuid4()), str(uuid.uuid4())
        body = {'action': 'metadata_read', 'decision_id': decision_id, 'version': str(entry['version']),
            'recipient_type': 'user', 'recipient_id': self.recipient, 'expiry_mode': 'at_time',
            'expires_at': (datetime.now(timezone.utc) + timedelta(minutes=5)).isoformat(),
            'expected_boundary_hash': facts['boundary_hash'], 'reason': 'Disposable PG metadata business approval'}
        decision = business.request('POST', route + '/sharing_decisions', (201,), body).payload
        require(decision.get('id') == decision_id and decision.get('action') == body['action']
                and decision.get('confirmed_by') == confirmer and decision.get('recipient_id') == self.recipient
                and decision.get('item_fingerprint') == facts['item_fingerprint']
                and decision.get('boundary_hash') == facts['boundary_hash'], 'metadata business approval binding changed')
        hidden_metadata_list(self.reader, self.engine)
        require(self.listing(history=True, state='active', catalog_path=path)['total'] == 0,
                'metadata business confirmation issued a Grant')
        self.grants.append(request_id)
        command = {'request_id': request_id, 'decision_id': decision_id}
        self.operator.request('POST', route + '/sharing_fulfillments', (400,), dict(command, action='read'))
        require(self.listing(history=True, state='active', catalog_path=path)['total'] == 0,
                'action override attempt issued a Grant')
        report['phase'] = 'catalog_metadata_issuance'
        first = self.operator.request('POST', route + '/sharing_fulfillments', (200, 202), command).payload
        require(first.get('request_id') == request_id and first.get('state') in {'pending', 'accepted'},
                'metadata fulfillment response changed the original command')
        deadline = time.monotonic() + 120
        while time.monotonic() < deadline:
            history = self.listing(history=True, state='active', catalog_path=path)
            if history['total']: break
            time.sleep(1)
        else: raise SuiteError('metadata business Grant was not issued')
        require(history['total'] == 1, 'metadata business approval issued multiple Grants')
        grant = history['data'][0]
        require(all(grant.get(key) == value for key, value in {
            'request_id': request_id, 'catalog_request_id': request_id, 'approval_mode': 'catalog',
            'engine_id': str(self.engine), 'catalog_path': path, 'action': 'metadata_read',
            'recipient_type': 'user', 'recipient_id': self.recipient, 'expiry_mode': body['expiry_mode'],
            'item_fingerprint': facts['item_fingerprint'], 'boundary_hash': facts['boundary_hash'],
            'operator_principal_id': report['principals']['operator'], 'revocation': None}.items())
            and bool(grant.get('granted_at'))
            and datetime.fromisoformat(grant['expires_at'].replace('Z', '+00:00')) == datetime.fromisoformat(body['expires_at']),
            'metadata business Grant changed its action, identity, recipient or expiry')
        settled = self.operator.request('GET', route + '/sharing_fulfillments/' + request_id, (200,)).payload
        require(settled.get('request_id') == request_id and settled.get('decision_id') == decision_id
                and settled.get('state') == 'accepted' and settled.get('action') == 'metadata_read'
                and settled.get('recipient_id') == self.recipient and settled.get('granted_at') == grant['granted_at'],
                'metadata fulfillment history differs from its approval and Grant')
        event_id = metadata_identity(self.reader, self.tenant, self.engine, 'metric_events', facts)
        target = locator(self.engine, 'metric_events', event_id)
        preview(self.reader, target, False)
        hidden_metadata_list(self.outsider, self.engine)
        self.operator.request('POST', route + '/sharing_fulfillments', (200,), command)
        require(self.listing(history=True, state='active', catalog_path=path) == history,
                'metadata fulfillment replay changed its Grant')
        self.revoke(request_id)
        self.operator.request('POST', route + '/sharing_fulfillments', (200,), command)
        hidden_metadata(self.reader, event_id)
        hidden_metadata_list(self.reader, self.engine)
        preview(self.reader, target, False)
        revoked = self.listing(history=True, state='revoked', catalog_path=path)
        require(any(row['request_id'] == request_id and row.get('granted_at') == grant['granted_at']
                    and row.get('revocation') for row in revoked['data']), 'metadata replay restored a revoked Grant')
        require(self.reader.token == token and self.reader.request('GET', '/api/v1/system/auth/context', (200,)).payload == context,
                'metadata scenario changed the original identity or permissions')
        evidence['catalog'] = {'entry_id': entry['id'], 'decision_id': decision_id, 'request_id': request_id,
            'item_id': event_id, 'item_fingerprint': facts['item_fingerprint'], 'boundary_hash': facts['boundary_hash'],
            'action': 'metadata_read', 'replay_unchanged': True, 'revoked_replay_denied': True}
        evidence['same_credential'] = True
        report['checks'].extend(METADATA_CHECKS)

    def catalog_flow(self, report, business, confirmer):
        report['phase'] = 'catalog_source_convergence'
        token = self.reader.token
        path = table_path(self.engine, 'metric_events')
        self.paths.append(path)  # Cleanup owns every target before any possible write.
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, path)
        target = locator(self.engine, 'metric_events')
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
        decision_id, request_id = str(uuid.uuid4()), str(uuid.uuid4())
        evidence = report['catalog_approval'] = {'entry_id': entry_id, 'decision_id': decision_id,
            'request_id': request_id, 'item_fingerprint': facts['item_fingerprint'], 'boundary_hash': facts['boundary_hash']}
        decision_body = {'action': 'read', 'decision_id': decision_id, 'version': str(entry['version']), 'recipient_type': 'user',
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
                and bool(grant.get('granted_at'))
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
        row = item(self.reader, self.engine, 'metric_events')
        require(row['fingerprint'] == facts['item_fingerprint'], 'business target differs from persisted DataItem')
        preview(self.outsider, target, False)
        self.catalog_outage_flow(report, path=path, facts=facts, route=route, command=command,
                                 history=history, read=read, target=target)
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
            'catalog_grant_withdrawal', 'catalog_replay_does_not_restore_revoked_grant'])

    def catalog_outage_flow(self, report, *, path, facts, route, command, history, read, target):
        report['phase'] = 'catalog_outage_existing_grant'
        token = self.reader.token
        outsider = self.outsider.request('GET', '/api/v1/system/auth/context', (200,)).payload['principal']['id']
        self.recipients.add(('user', outsider))
        blocked_request = str(uuid.uuid4())
        self.grants.append(blocked_request)
        direct_request, direct_body = self.grant_command(facts, catalog_path=path, recipient_id=outsider,
                                                        seconds=180)
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
            # A failed business request never creates a Grant. A separately explicit
            # direct command remains available to the qualified engine operator.
            require(self.listing(history=True, state='active', recipient_id=outsider, catalog_path=path)['total'] == 0,
                    'offline business request silently issued a new Grant')
            preview(self.outsider, target, False)
            direct = self.operator.request('POST', self.base + '/access_grants', (201,), direct_body).payload
            require(direct.get('request_id') == direct_request and direct.get('approval_mode') == 'independent'
                    and direct.get('recipient_id') == outsider and direct.get('item_fingerprint') == facts['item_fingerprint']
                    and direct.get('boundary_hash') == facts['boundary_hash'], 'offline direct Grant changed its binding')
            preview(self.outsider, target, True, 4, columns=('event_id', 'event_date'), values=('e1', 'e2', 'e3', 'old'))
            self.revoke(direct_request)
            preview(self.outsider, target, False)
            require(self.listing(history=True, state='active', catalog_path=path) == history,
                    'independent direct command changed the original business Grant')
            report['phase'] = 'catalog_outage_withdrawal'
            self.revoke(command['request_id'])
            read(False)
        report['phase'] = 'catalog_recovery_preserves_withdrawal'
        for recipient in (self.recipient, outsider):
            require(self.listing(history=True, state='active', recipient_id=recipient, catalog_path=path)['total'] == 0,
                    'Catalog recovery created or restored an active Grant')
            all_history = self.listing(history=True, recipient_id=recipient, catalog_path=path)
            require(not any(row.get('request_id') == blocked_request for row in all_history['data']),
                    'a rejected offline command left a source Grant history')
        absent = self.operator.request('GET', route + '/sharing_fulfillments/' + blocked_request, (404,)).payload
        require(bool(absent.get('error')) and not any(key in absent for key in ('state', 'granted_at')),
                'offline new request unexpectedly became a Catalog fulfillment')
        require(self.reader.token == token, 'Catalog outage changed the consumer credential')
        evidence.update(unavailable_status=unavailable.status, direct_granted_and_revoked=True,
                        same_credential=True, before=outage.before, after=outage.after)
        report['checks'].extend(['existing_grant_readable_without_catalog', 'catalog_offline_new_fulfillment_unavailable',
            'catalog_offline_explicit_direct_grant_and_revocation', 'catalog_offline_withdrawal',
            'catalog_recovery_preserves_original_rules'])

    def inspect_sources(self, facts, sources, reason='grant'):
        current = self.listing()
        if not sources:
            require(current['total'] == 0 and current['data'] == [], 'source-free account still has a current relation')
            self.source_observations.append({'reason': 'no_grant', 'sources': []})
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
        require('denies' not in observed, 'retired Deny leaked into current source inspection')
        self.source_observations.append({key: observed[key] for key in ('reason', 'sources')})

    def organization_flow(self, report, facts, target, ungranted):
        report['phase'] = 'organization_sources'
        report['source_observations'] = self.source_observations
        report['organization_recipients'] = self.organizations
        before = self.reader.token
        preview(self.outsider, target, False)
        sources = {}
        personal = ('user', self.recipient)
        department = ('department', self.organizations['department'])
        project = ('project_group', self.organizations['project_group'])
        for source in (department, project, personal):
            sources[source], _ = self.grant(facts, recipient_type=source[0], recipient_id=source[1])
            self.inspect_sources(facts, sources)
            preview(self.reader, target, True, 2)
            preview(self.outsider, target, False)
            preview(self.reader, ungranted, False)
        report['checks'].append('three_sources_merged_and_nonmember_denied')
        personal_id = sources.pop(personal)
        self.revoke(personal_id)
        self.inspect_sources(facts, sources)
        require(self.listing(history=True, state='active')['total'] == 0,
                'history expanded current organization sources into personal Grants')
        preview(self.reader, target, True, 2)
        report['checks'].append('personal_withdrawal_preserves_organizations')
        self.revoke(sources.pop(department))
        self.inspect_sources(facts, sources)
        preview(self.reader, target, True, 2)
        report['checks'].append('department_withdrawal_preserves_project')
        self.revoke(sources.pop(project))
        self.inspect_sources(facts, sources)
        preview(self.reader, target, False)
        preview(self.outsider, target, False)
        preview(self.reader, ungranted, False)
        require(self.reader.token == before, 'organization authorization changed the reader credential')
        report['checks'].append('all_sources_withdrawn_denies_read')

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
        self.inspect_sources(facts, sources)
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
                self.inspect_sources(facts, sources)
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
        report['phase'] = 'meta_recovered_original_grant_readable'
        preview(self.reader, target, True, 2)
        preview(self.reader, ungranted, False)
        report['phase'] = 'meta_recovery_explicit_withdrawal'
        self.revoke(grant)
        preview(self.reader, target, False)
        evidence.update({
            'facts_status': 503, 'unavailable_preview_status': 503, 'recovered_preview_status': 200, 'same_credential': True, 'verified_without_rescan': True,
            'grant_id': grant})
        report['checks'].extend(['meta_outage_blocks_existing_grant',
            'meta_recovery_preserves_identity_boundary_and_grant', 'original_grant_recovers_without_rescan',
            'meta_recovery_withdrawal_denies'])

    def content_change_flow(self, report, facts, original, target, ungranted):
        report['phase'] = 'pg_content_change_grant'
        token = self.reader.token
        grant, command = self.grant(facts)
        history = self.listing(history=True)
        scans = []
        def rescan(*, visible=False):
            scans.append(basic_scan(self.operator, self.engine, ['metric_people']))
            observed = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
            require(observed['item_fingerprint'] == original['fingerprint']
                    and observed['item_fingerprint'] == facts['item_fingerprint']
                    and observed['boundary_hash'] == facts['boundary_hash'],
                    'PG content update changed the approved DataItem identity or boundary')
            if visible:
                current = item(self.reader, self.engine, 'metric_people')
                require(current['id'] == original['id'] and current['fingerprint'] == original['fingerprint'],
                        'PG content update changed the persisted Meta identity')
            else:
                hidden_metadata(self.reader, original['id'])
            preview(self.reader, ungranted, False)
            preview(self.outsider, target, False)
        def unchanged(expected):
            require(self.listing(history=True) == expected, 'PG content update rewrote Grant history')
        try:
            mutate_source_content('content-change')
            rescan(visible=True)
            unchanged(history)
            preview(self.reader, target, True, 3, values=('A', 'B2', 'C'))
            report['phase'] = 'pg_content_reset_existing_grant'
            mutate_source_content('content-reset')
            rescan(visible=True)
            unchanged(history)
            preview(self.reader, target, True, 2)
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
            require(self.listing()['total'] == 0, 'withdrawn Grant returned after content change')
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
            'grant_request_id': grant}
        report['checks'].extend(['content_change_preserves_identity_and_boundary', 'old_grant_reads_changed_content',
            'existing_grant_reads_reset_content',
            'content_change_and_replay_do_not_restore_withdrawn_grant', 'source_baseline_restored_without_read_access'])

    def run(self, report, *, load_profile='full'):
        reader_before = self.reader.token
        report['phase'] = 'initial_protection_sync'
        API.wait_manager_read_boundary(self.reader, self.engine, time.monotonic() + 120, target_visible=False)
        report['phase'] = 'initial_basic_scan'
        report['initial_scan'] = basic_scan(self.operator, self.engine, ['metric_people', 'metric_events'])
        # Paths identify the request, never permission. Meta identities are read by
        # the consumer only after the formal source Grant has been issued.
        target = locator(self.engine, 'metric_people')
        ungranted = locator(self.engine, 'metric_events')
        REGISTRATION.read_exact_record_authorization_facts(self.operator, table_path(self.engine, 'metric_events'))
        preview(self.reader, target, False)
        preview(self.reader, ungranted, False)
        report['phase'] = 'grant_and_duplicate'
        facts = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        first, body = self.grant(facts)
        original = item(self.reader, self.engine, 'metric_people')
        require(original.get('fingerprint') == facts['item_fingerprint'], 'Meta identity differs from trusted boundary')
        target = locator(self.engine, 'metric_people', original['id'])
        preview(self.reader, target, True, 2)
        preview(self.reader, ungranted, False)
        duplicate = dict(body, request_id=str(uuid.uuid4()))
        self.grants.append(duplicate['request_id'])
        rejected = self.operator.request('POST', self.base + '/access_grants', (409,), duplicate).payload
        require(rejected.get('error_code') == 'engine_access_grant_relation_exists', 'duplicate rejected for the wrong reason')
        if load_profile == 'full':
            report['phase'] = 'fixed_target_read_latency'
            self.measure_read_latency(report, facts, first)
        report['phase'] = 'basic_rescan'
        report['basic_rescan'] = basic_scan(self.operator, self.engine, ['metric_people'])
        refreshed = item(self.reader, self.engine, 'metric_people')
        after = REGISTRATION.read_exact_record_authorization_facts(self.operator, self.path)
        require(refreshed['id'] == original['id'] and refreshed['fingerprint'] == original['fingerprint']
                and after['boundary_hash'] == facts['boundary_hash'], 'basic rescan changed the approved DataItem boundary')
        preview(self.reader, target, True, 2)
        report['phase'] = 'grant_withdrawal'
        require(self.listing(history=True, state='active')['total'] == 1, 'active Grant missing before withdrawal')
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
                              'revoke_denies_read', 'natural_expiry_denies_read'])
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
        cpu_arm = os.environ.get('ADDP_ONLINE_PG_CPU_ARM', '')
        require(cpu_arm in {'', '3', '4'}, 'invalid PG CPU arm')
        load_profile = CPU_LOAD_PROFILE if cpu_arm else 'full'
        if cpu_arm:
            report.update(cpu_vcpus=int(cpu_arm), full_load_acceptance='not_run')
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
        lifecycle.pg_runtime, lifecycle.pg_runtime_principal = manager_pg_runtime(login, tenant)
        report['principals']['manager_runtime'] = lifecycle.pg_runtime_principal
        lifecycle.run(report, load_profile=load_profile)
        lifecycle.function_permission_flow(report, business, confirmer)
        lifecycle.catalog_flow(report, business, confirmer)
        lifecycle.workspace_flow(report, issuer)
        lifecycle.metadata_access_flow(report, business, confirmer)
        lifecycle.collection = MetaCollectionScenario(lifecycle, issuer, run_id)
        lifecycle.collection.run(report)
        lifecycle.collection.run_scheduled(report)
        controller = API.GatewayClient(os.environ['GATEWAY_URL'], os.environ.pop('ADDP_ONLINE_PG_PROBE_ACCESS_TOKEN'), timeout)
        lifecycle.collection.run_probe_control(report, controller)
        del controller
        lifecycle.collection.run_inflight(report)
        lifecycle.node_scope_flow(report)
        report['phase'] = 'authorized_pg_metadata_refresh'
        def register_refresh(body):
            lifecycle.grants.append(body['request_id'])
            if body['catalog_path'] not in lifecycle.paths:
                lifecycle.paths.append(body['catalog_path'])
        lifecycle.scoped_refresh = REFRESH.ScopedRefreshScenario(operator, lifecycle.reader, tenant, recipient, engine,
            'postgresql', REFRESH.PersistedScanObservation(tenant, engine, 'postgresql'),
            report.setdefault('scoped_refresh', {}), register=register_refresh)
        lifecycle.scoped_refresh.precise_and_node_flow('public', 'public.metric_people',
            ['public.metric_events', 'public.metric_facts', 'public.metric_areas'])
        for cutoff in ('withdrawal', 'expiry'):
            barrier = PGCollectionReadBarrier()
            lifecycle.collection.source_barriers.append(barrier)
            lifecycle.scoped_refresh.inflight_items('public', ['public.metric_people', 'public.metric_events'],
                                                   cutoff, barrier)
        lifecycle.production_flow(report, cpu_arm=cpu_arm)
        report['result'] = 'passed'
    except (SuiteError, PREFLIGHT.PreflightError, REGISTRATION.RegistrationError, KeyError, ValueError, TypeError, OSError):
        print('PG source authorization acceptance failed; inspect the bounded phase evidence', file=sys.stderr)
    finally:
        if lifecycle is not None:
            report.update(grant_commands=lifecycle.grants)
            if lifecycle.collection is not None:
                report['meta_collection']['cleanup'] = 'failed'
            try:
                lifecycle.cleanup()
                report['cleanup'] = 'zero_current_source_rules'
                if lifecycle.collection is not None:
                    report['meta_collection']['cleanup'] = 'zero_owned_tasks_and_active_executions'
            except Exception:
                report.update(result='failed', cleanup='failed')
        if evidence is not None:
            evidence.write_text(json.dumps(report, sort_keys=True) + '\n')
            load = report.get('authorization_load')
            if load is not None:
                audits = load.get('audit_observation', {}).get('audit_rows', [])
                system_logs = Path(__file__).resolve().parents[2] / 'logs/runtime/system'
                timings = {} if load.get('profile') == CPU_LOAD_PROFILE else None
                load['system_log_tail'] = collect_system_load_tail(system_logs, audits, rule_timings=timings)
                if timings is not None:
                    proof = load['source_rule_query_observation'] = {'result': 'incomplete', 'matched': len(timings)}
                    if load['system_log_tail']['result'] == 'matched' and len(timings) == len(audits):
                        try:
                            with (evidence.parent / 'pg-load-waits.jsonl').open() as stream:
                                records = bind_source_rule_waits(timings, stream)
                            (evidence.parent / 'pg-source-rule-timings.json').write_text(json.dumps(records) + '\n')
                            proof.update(result='matched', sampled=sum(row['active_samples'] > 0 for row in records))
                        except (OSError, SuiteError, ValueError, TypeError, KeyError):
                            pass
                    if proof['result'] != 'matched':
                        report['result'] = 'failed'
                evidence.write_text(json.dumps(report, sort_keys=True) + '\n')
    print(json.dumps(report, sort_keys=True))
    return 0 if report['result'] == 'passed' else 1


if __name__ == '__main__':
    raise SystemExit(main())
