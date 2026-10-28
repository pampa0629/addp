#!/usr/bin/env python3
"""Manual formal-identity Trace acceptance on a disposable local deployment.

This entry never dispatches Online/Hosted, touches personal lifecycle state, or
runs a capacity model. Product images and Compose contracts remain authoritative.
"""
from __future__ import annotations

import base64
from contextlib import contextmanager
import copy
import hashlib
import importlib
import json
import math
import os
from pathlib import Path
import platform
import re
import resource
import secrets
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import uuid
import urllib.parse

ROOT = Path(__file__).resolve().parents[2]
LABEL = 'com.addp.execution-trace-local'
BACKENDS = ('system-backend', 'gateway', 'security-backend', 'develop-backend', 'orchestrator-backend', 'monitor-backend')
FRONTENDS = ('console', 'system-frontend', 'monitor-frontend', 'nginx')
TRACE = ('minio', 'runtime-log-store-init', 'loki', 'runtime-log-api', 'trace-store-init', 'tempo', 'trace-query', 'alloy')
# All concurrently running services, browser and the sampling helper fit 8 CPU/12 GiB.
LIMITS = {name: (.25, 512) for name in BACKENDS} | {name: (.025, 32) for name in FRONTENDS} | {
    'system-backend': (.5, 768), 'postgres': (.5, 768), 'business-postgres': (.25, 256), 'redis': (.1, 128),
    'minio': (.5, 512), 'runtime-log-store-init': (.1, 128), 'loki': (.5, 512), 'runtime-log-api': (.1, 64),
    'trace-store-init': (.1, 128), 'tempo': (2, 2048), 'trace-query': (.25, 128), 'alloy': (1, 1024)}
OWN_INPUTS = (
    'Makefile', 'nginx/nginx.conf', 'scripts/ci/check-online-ci-registration.py', 'scripts/ci/check-execution-trace-local-registration_test.py',
    'scripts/test/monitor-traces-local-gate.py', 'scripts/test/monitor-traces-local-gate_test.py',
    'scripts/test/monitor-traces-local-online.py', 'scripts/test/monitor-traces-local-online_test.py',
    'scripts/test/monitor-traces-gate.sh', 'scripts/infra/init-postgresql.sql', 'scripts/infra/Dockerfile.minio', 'common/httptrace/execution.go', 'common/httptrace/execution_test.go',
    'common/httptrace/tracetest/runtime.go', 'develop/backend/internal/repository/execution_repository.go',
    'develop/backend/internal/repository/execution_trace.go', 'develop/backend/internal/repository/execution_trace_test.go',
    'develop/backend/internal/service/execution_trace.go', 'develop/backend/internal/service/execution_trace_test.go',
    'orchestrator/backend/internal/service/execution_advance.go', 'orchestrator/backend/internal/service/execution_ownership.go',
    'orchestrator/backend/internal/service/execution_service.go', 'orchestrator/backend/internal/service/execution_trace.go',
    'orchestrator/backend/internal/service/execution_trace_test.go', 'system/backend/go.mod', 'system/backend/go.sum', 'system/backend/cmd/online-test-fixture/main.go',
    'system/backend/cmd/online-test-fixture/execution_trace_local.go', 'system/backend/cmd/online-test-fixture/execution_trace_local_test.go',
    'console/frontend/e2e/online/execution-trace-local.spec.js', 'console/frontend/e2e/online/execution-trace-local-support.mjs',
    'console/frontend/tests/online/executionTraceLocalBrowser.test.js',
)
MAX_SECONDS = 3600
VOLUME_STOP = 4 * 1024**3
SAMPLER_CPU = .25


class GateError(RuntimeError):
    """Only fixed, non-secret diagnostics cross the private evidence boundary."""


def require(condition, code):
    if not condition:
        raise GateError(code)


def owned_list_identity(args, run_id, label=LABEL):
    """Semantic API signature for the only owned list route, never raw argv."""
    for kind, path in (('container', '/containers/json'), ('volume', '/volumes')):
        expected = ['docker', kind, 'ls', *(['-a'] if kind == 'container' else []),
                    '-q', '--filter', f'label={label}={run_id}']
        if list(args) == expected:
            query = {'filters': {'label': [f'{label}={run_id}']}}
            if kind == 'container':
                query['all'] = True
            canonical = json.dumps({'method': 'GET', 'path': path, 'query': query}, sort_keys=True, separators=(',', ':'))
            return {'operation': 'list_owned_containers' if kind == 'container' else 'list_owned_volumes',
                    'method': 'GET', 'api_family': path,
                    'request_signature_sha256': hashlib.sha256(canonical.encode()).hexdigest()}
    return None


class CommandEvidence:
    """Bounded owned-list journal; sample commands share these same records."""
    def __init__(self, limit=4096, *, label=LABEL):
        self.label = label
        self.limit = limit
        self.records = []
        self.active = set()
        self.sequence = 0
        self.lock = threading.Lock()

    def begin(self, args, run_id, phase):
        identity = owned_list_identity(args, run_id, self.label)
        with self.lock:
            if identity:
                signature = identity['request_signature_sha256']
                require(signature not in self.active, 'owned_list_command_overlap')
                # Cleanup has four normal list calls; reserve eight, not an
                # unbounded bypass of the failure that stopped the workload.
                require(len(self.records) < self.limit + (8 if phase == 'cleanup' else 0), 'command_evidence_limit')
                self.active.add(signature)
            self.sequence += 1
            record = {'command_id': self.sequence, 'run_id': run_id,
                      'phase': phase if re.fullmatch('[a-z0-9_]{1,80}', phase) else 'unclassified',
                      'pid': None, 'record_complete': False}
            if identity:
                record.update(identity)
                self.records.append(record)
            return record

    def finish(self, record):
        with self.lock:
            record['record_complete'] = True
            if 'request_signature_sha256' in record:
                self.active.remove(record['request_signature_sha256'])

    def snapshot(self):
        with self.lock:
            # Even a worker that failed to stop must not erase the error report.
            # An unfinished record stays explicitly incomplete, never successful.
            return copy.deepcopy(self.records)


def require_apiproxy_clock_semantics(proof, desktop_version):
    # This validates a source-audited proof, not the existence of log lines.
    # There is deliberately no environment flag or "timestamps look close" path.
    require(isinstance(proof, dict) and proof.get('desktop_version') == desktop_version and
            proof.get('component') == 'apiproxy' and proof.get('timestamp_origin') == 'request_event' and
            proof.get('events_within_request_lifetime') is True and
            isinstance(proof.get('source_url'), str) and proof['source_url'].startswith('https://') and
            isinstance(proof.get('source_sha256'), str) and re.fullmatch('[a-f0-9]{64}', proof['source_sha256']),
            'apiproxy_clock_semantics_unconfirmed')


def apiproxy_clock_interval(host_started, host_finished, proxy_started, proxy_finished):
    require(all(type(v) is int for v in (host_started, host_finished, proxy_started, proxy_finished)) and
            host_started <= host_finished and proxy_started <= proxy_finished, 'clock_event_order_invalid')
    interval = (host_started-proxy_started, host_finished-proxy_finished)
    require(interval[0] <= interval[1], 'clock_interval_empty')
    return interval


def apiproxy_clock_envelope(before, after):
    require(all(len(v) == 2 and all(type(n) is int for n in v) and v[0] <= v[1] for v in (before, after)),
            'clock_interval_invalid')
    require(max(before[0], after[0]) <= min(before[1], after[1]), 'clock_intervals_inconsistent')
    envelope = min(before[0], after[0]), max(before[1], after[1])
    require(envelope[1]-envelope[0] <= 50_000_000, 'clock_uncertainty_exceeded')
    return envelope


def validate_clock_anchors(anchors):
    previous = None
    for began, wall, finished in anchors:
        require(all(type(n) is int for n in (began, wall, finished)) and began <= finished, 'clock_anchor_invalid')
        current = wall-finished, wall-began
        if previous:
            require(max(previous[0], current[0])-min(previous[1], current[1]) <= 50_000_000, 'clock_wall_jump')
        previous = current


def pair_apiproxy_events(events):
    pending, pairs = {}, []
    seen = set()
    for event in sorted(events, key=lambda row: row['timestamp_ns']):
        signature, stamp = event['request_signature_sha256'], event['timestamp_ns']
        require(type(stamp) is int and re.fullmatch('[a-f0-9]{64}', signature), 'api_event_invalid')
        key = signature, stamp, event['direction']
        require(key not in seen, 'duplicate_api_event')
        seen.add(key)
        if event['direction'] == '>>':
            require(signature not in pending, 'overlapping_api_events_without_request_id')
            pending[signature] = stamp
        else:
            require(event['direction'] == '<<' and signature in pending, 'api_event_without_begin')
            pairs.append({'request_signature_sha256': signature, 'started_ns': pending.pop(signature), 'finished_ns': stamp})
    require(not pending, 'api_event_without_end')
    return pairs


def attribute_apiproxy_requests(commands, pairs, offset):
    require(offset[0] <= offset[1] and offset[1]-offset[0] <= 50_000_000, 'clock_uncertainty_exceeded')
    result, used = {}, set()
    for command in sorted(commands, key=lambda row: row['entered_monotonic_ns']):
        require(command['result'] == 'passed' and command['command_id'] not in result, 'command_not_correlatable')
        candidates = [i for i, pair in enumerate(pairs)
            if pair['request_signature_sha256'] == command['request_signature_sha256'] and
            max(offset[0], command['entered_monotonic_ns']-pair['started_ns']) <=
            min(offset[1], command['decision_monotonic_ns']-pair['finished_ns'])]
        require(len(candidates) == 1, 'api_request_missing_or_ambiguous')
        require(candidates[0] not in used, 'api_request_reused')
        result[command['command_id']] = candidates[0]
        used.add(candidates[0])
    require(len(used) == len(pairs), 'unassigned_owned_api_request')
    return result


HOST_CLI_BUFFER_LIMIT = 16 * 1024**2
HOST_CLI_OUTPUT_LIMIT = 5 * 1024**2


def readonly_docker_endpoint(environment, docker_home):
    """Resolve the same CLI context without spending an extra Docker invocation."""
    def read(path):
        require(path.is_file() and path.stat().st_size <= 65536, 'host_cli_context_file_invalid')
        with path.open('rb') as stream:
            raw = stream.read(65537)
        require(len(raw) <= 65536, 'host_cli_context_file_invalid')
        result = json.loads(raw)
        require(isinstance(result, dict), 'host_cli_context_file_invalid')
        return result
    def context(name):
        if name is None:
            config = docker_home/'config.json'
            name = read(config).get('currentContext', 'default') if config.exists() else 'default'
        require(isinstance(name, str) and bool(name), 'host_cli_context_file_invalid')
        if name == 'default':
            return 'unix:///var/run/docker.sock'
        key = hashlib.sha256(name.encode()).hexdigest()
        metadata = read(docker_home/'contexts/meta'/key/'meta.json')
        require(metadata.get('Name') == name, 'host_cli_context_identity_mismatch')
        return metadata.get('Endpoints', {}).get('docker', {}).get('Host')
    return effective_docker_endpoint(environment, context)


def host_cli_usage():
    parent = resource.getrusage(resource.RUSAGE_SELF)
    children = resource.getrusage(resource.RUSAGE_CHILDREN)
    # getrusage RSS is bytes on Darwin, KiB on Linux. This is a cumulative
    # high-water mark, not a per-command RSS delta or a sum of simultaneous RSS.
    require(sys.platform in ('darwin', 'linux'), 'host_cli_platform_unsupported')
    scale = 1 if sys.platform == 'darwin' else 1024
    return {'parent_user_seconds': parent.ru_utime, 'parent_system_seconds': parent.ru_stime,
            'children_user_seconds': children.ru_utime, 'children_system_seconds': children.ru_stime,
            'parent_rss_high_water_bytes': int(parent.ru_maxrss)*scale,
            'children_rss_high_water_bytes': int(children.ru_maxrss)*scale}


def require_no_child_processes():
    try:
        os.waitpid(-1, os.WNOHANG)
    except ChildProcessError:
        return
    raise GateError('host_cli_extra_child')


def host_cli_cpu_delta(before, after, role):
    result = {}
    for mode in ('user', 'system'):
        key = role+'_'+mode+'_seconds'
        require(all(type(n) in (int, float) and math.isfinite(n) for n in (before[key], after[key])) and
                0 <= before[key] <= after[key], 'host_cli_cpu_counter_invalid')
        result[mode] = after[key]-before[key]
    return result


def check_host_cli_usage(origin, usage, report):
    parent = host_cli_cpu_delta(origin, usage, 'parent')
    children = host_cli_cpu_delta(origin, usage, 'children')
    peak = report.setdefault('resource_peak', {})
    total = sum(parent.values())+sum(children.values())
    peak['total_cpu_seconds'] = max(peak.get('total_cpu_seconds', 0), total)
    for key in ('parent_rss_high_water_bytes', 'children_rss_high_water_bytes'):
        require(type(usage[key]) is int and usage[key] >= 0, 'host_cli_rss_invalid')
        peak[key] = max(peak.get(key, 0), usage[key])
    require(total < 30, 'host_cli_cpu_limit')
    require(max(usage['parent_rss_high_water_bytes'], usage['children_rss_high_water_bytes']) < 256*1024**2,
            'host_cli_rss_limit')


def check_host_cli_buffers(life, report):
    seen = set()
    def size(value):
        if id(value) in seen:
            return 0
        seen.add(id(value))
        amount = sys.getsizeof(value)
        if isinstance(value, dict):
            amount += sum(size(k)+size(v) for k, v in value.items())
        elif isinstance(value, (list, tuple)):
            amount += sum(size(v) for v in value)
        return amount
    # Reserve bounded config decode and command output/decoding buffers, while
    # measuring the live report/environment graph and JSON serialization space.
    buffers = size((report, life.env))+1024**2+3*65536
    require(buffers <= HOST_CLI_BUFFER_LIMIT, 'host_cli_buffer_limit')
    raw = json.dumps(report, separators=(',', ':')).encode()
    require(len(raw) <= HOST_CLI_OUTPUT_LIMIT, 'host_cli_output_limit')
    buffers += 2*len(raw)
    require(buffers <= HOST_CLI_BUFFER_LIMIT, 'host_cli_buffer_limit')
    peak = report.setdefault('resource_peak', {})
    peak['application_buffer_accounted_bytes'] = max(peak.get('application_buffer_accounted_bytes', 0), buffers)


def run_host_cli_comparison(life, report, window_started_ns, usage_origin):
    """One serial, empty-selector host measurement; never starts a deployment.

    The outside driver owns an exclusive attempt receipt and private-file cleanup.
    No VM logs, calibration, resource monitor, sibling subprocesses, or retries.
    """
    report.update(measurement_result='failed', groups=[], commands=life.command_evidence.records)
    require(life.thread is None and life.sample_thread_id is None and threading.active_count() == 1 and
            not life.command_evidence.records, 'host_cli_independent_process_required')
    life.phase = 'host_cli_readonly'
    request_deadline = window_started_ns+118*10**9
    last_start = None

    def check(deadline=request_deadline):
        require(time.monotonic_ns() < request_deadline, 'host_cli_request_deadline_exceeded')
        require(time.monotonic_ns() < deadline, 'host_cli_group_deadline_exceeded')
        check_host_cli_usage(usage_origin, host_cli_usage(), report)
        check_host_cli_buffers(life, report)

    try:
        check()
        for index in range(20):
            group = {'index': index+1, 'started_monotonic_ns': time.monotonic_ns(), 'result': 'failed', 'command_ids': []}
            report['groups'].append(group)
            group_deadline = min(group['started_monotonic_ns']+4*10**9, request_deadline)
            try:
                for kind in ('container', 'volume'):
                    check(group_deadline)
                    if last_start is not None:
                        target = last_start+10**9
                        while time.monotonic_ns() < target:
                            check(group_deadline)
                            time.sleep(min(.05, max(0, (min(target, group_deadline)-time.monotonic_ns())/1e9)))
                    require_no_child_processes()
                    before = host_cli_usage()
                    count = len(life.command_evidence.records)
                    require(count < 40, 'host_cli_command_limit')
                    check(group_deadline)
                    try:
                        objects = life.owned(kind, timeout=(group_deadline-time.monotonic_ns())/1e9, output_limit=65536)
                    finally:
                        after = host_cli_usage()
                        require_no_child_processes()
                        require(len(life.command_evidence.records) == count+1, 'host_cli_command_record_missing')
                        record = life.command_evidence.records[-1]
                        delta = host_cli_cpu_delta(before, after, 'children')
                        record.update(child_user_cpu_seconds=delta['user'], child_system_cpu_seconds=delta['system'],
                                      children_rss_high_water_bytes=after['children_rss_high_water_bytes'])
                        group['command_ids'].append(record['command_id'])
                        last_start = record['spawn_started_monotonic_ns']
                        check_host_cli_usage(usage_origin, after, report)
                    require(not objects, 'host_cli_unexpected_objects')
                    check(group_deadline)
            finally:
                group['finished_monotonic_ns'] = time.monotonic_ns()
                group['duration_seconds'] = (group['finished_monotonic_ns']-group['started_monotonic_ns'])/1e9
            require(group['duration_seconds'] < 4, 'host_cli_group_deadline_exceeded')
            group['result'] = 'passed'
        starts = [r['spawn_started_monotonic_ns'] for r in report['commands']]
        require(len(report['groups']) == 20 and len(starts) == 40 and
                all(b-a >= 10**9 for a, b in zip(starts, starts[1:])) and
                all(r['result'] == 'passed' and r['record_complete'] for r in report['commands']),
                'host_cli_samples_incomplete')
        check()
        report['measurement_result'] = 'passed'
    except BaseException:
        report['measurement_result'] = 'failed'
        raise


def diagnostic_markers(raw):
    return [marker for marker in ('permission denied','operation not permitted','chown','setgid','setuid','cannot load certificate',
        'is a directory','failed parsing config','unknown flag','read-only file system','no such file or directory',
        'failed to solve','failed to resolve','failed to authorize','certificate signed','no space left',
        'context canceled','connection refused','proxyconnect','tls handshake','unexpected eof',
        'not found','unsupported','traceback','syntaxerror','keyerror','no builder','timeout','timed out',
        'exec format','cannot connect','not accessible','binary not found','error response from daemon',
        'failed to initialize database','failed to ensure export session store','does not exist','sqlstate',
        'psql:','connection to server','server closed the connection unexpectedly','database system is starting up',
        'authentication failed','resource temporarily unavailable','too many open files','exec failed',
        'protection readiness','protection initialization',
        'failed to compute cache key','failed to calculate checksum','content digest',
        'failed to fetch anonymous token','unexpected status from head request',
        'failed to read dockerfile','error exporting','failed to export','failed to copy',
        '初始化失败','监听绑定失败','配置无效') if marker.encode() in raw.lower()]



def public_build_failure_diagnostic(raw):
    """Closed fields from public BuildKit failures, never arbitrary log text."""
    text = raw[-131072:].decode(errors='replace').lower()
    errors = '\n'.join(line for line in text.splitlines() if 'error:' in line or 'failed to solve:' in line)
    frames = '\n'.join(line.strip() for line in text.splitlines() if line.lstrip().startswith('> '))
    reasons = [name for name, pattern in (
        ('context_input_missing', r'failed to calculate checksum[^\n]*not found'),
        ('content_digest_missing', r'content digest sha256:[a-f0-9]{64}[^\n]*not found'),
        ('base_metadata_resolution', r'failed to resolve source metadata'),
        ('registry_authorization', r'failed to authorize|failed to fetch anonymous token'),
        ('registry_http_status', r'unexpected status from'),
        ('dockerfile_process_exit', r'did not complete successfully: exit code:'),
        ('export_failed', r'failed to export'),
    ) if re.search(pattern, errors)]
    steps = [name for name, pattern in (
        ('base_metadata', r'load metadata for'), ('dockerfile', r'load build definition'),
        ('context', r'load build context'), ('copy', r'\] copy '),
        ('package_install', r'\] run .*apt-get'), ('run', r'\] run '),
        ('export', r'exporting to (?:image|docker image format)|failed to export'),
    ) if re.search(pattern, frames + '\n' + errors)]
    inputs = sorted('dist/release-linux-arm64/' + name.removesuffix('-backend') for name in BACKENDS + ('runtime-log',)
                    if 'dist/release-linux-arm64/' + name.removesuffix('-backend') in errors)
    lines = sorted({int(number) for number in re.findall(r'(?m)^\s*(?:[^\s]+/)?dockerfile(?:\.prebuilt)?:([1-9][0-9]{0,4})\s*$', text)})
    statuses = sorted({int(number) for number in re.findall(r'unexpected status[^\n]*?:\s+([45][0-9]{2})(?:\s|$)', errors)})
    codes = sorted({int(number) for number in re.findall(r'did not complete successfully: exit code:\s*([0-9]{1,3})(?:\D|$)', errors) if int(number) <= 255})
    return {'reasons': reasons or ['unclassified'], 'steps': steps, 'inputs': inputs,
            'dockerfile_lines': lines[:8], 'http_statuses': statuses[:8], 'process_exit_codes': codes[:8]}


def minio_product_contract(source):
    pattern = (r'git clone --depth 1 --branch (?P<release>\S+) '
        r'https://github\.com/minio/(?P<name>minio|mc)\.git (?P=name) &&\s*\\\s*'
        r'test "\$\(git -C (?P=name) rev-parse HEAD\)" = (?P<commit>[0-9a-f]{40})\b')
    rows = [match.groupdict() for match in re.finditer(pattern,source)]
    require(len(rows)==2 and {row['name'] for row in rows}=={'minio','mc'},'minio_product_recipe_invalid')
    require(all(re.fullmatch(r'RELEASE\.\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}Z',row['release']) for row in rows),
            'minio_product_recipe_invalid')
    return {row['name']:{'release':row['release'],'source_commit':row['commit']} for row in rows}


def verify_minio_product(life):
    expected=minio_product_contract((life.checkout/'scripts/infra/Dockerfile.minio').read_text())
    tag='addp-minio:'+expected['minio']['release']
    metadata=json.loads(life.docker('image','inspect',tag,'--format',
        '{"id":{{json .Id}},"architecture":{{json .Architecture}},"os":{{json .Os}}}'))
    image_id=metadata.get('id','')
    require(re.fullmatch(r'sha256:[0-9a-f]{64}',image_id) and metadata.get('architecture')=='arm64' and
            metadata.get('os')=='linux','minio_product_image_identity_invalid')
    proof={'image_id':image_id,'platform':'linux/arm64','components':{}}
    for name in ('minio','mc'):
        output=life.docker('run','--pull','never','--name',life.run_id+'-verify-'+name,
            '--label',f'{LABEL}={life.run_id}','--label',LABEL+'.role=oneshot',
            '--cpus','.25','--memory','256m','--network','none','--read-only',
            '--tmpfs','/data:rw,noexec,nosuid,size=1m','--tmpfs','/tmp:rw,noexec,nosuid,size=16m',
            '--entrypoint','/usr/local/bin/'+name,image_id,'--version',timeout=30)
        rows=re.findall(r'^'+name+r' version (\S+) \(commit-id=([0-9a-f]{40})\)\r?$',output,re.M)
        require(len(rows)==1 and rows[0]==(expected[name]['release'],expected[name]['source_commit']),
                'minio_product_'+name+'_version_mismatch')
        proof['components'][name]={**expected[name],'version_output_sha256':hashlib.sha256(output.encode()).hexdigest()}
    life.minio_image=image_id
    life.minio_provenance=proof


def module(name, root=ROOT):
    import importlib.util
    spec = importlib.util.spec_from_file_location(name.replace('-', '_'), root / 'scripts/test' / (name + '.py'))
    value = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = value
    spec.loader.exec_module(value)
    return value


def safe_environment():
    return {k: v for k, v in os.environ.items() if k in {'PATH', 'HOME', 'TMPDIR', 'DOCKER_HOST', 'DOCKER_CONTEXT'}}


def validate_profile(environment, machine, daemon):
    require(not any(environment.get(k) for k in ('GITHUB_ACTIONS', 'ADDP_ONLINE_HOSTED', 'ADDP_ONLINE_OWNER_MANAGED',
                                                'ADDP_ONLINE_HOST', 'ADDP_ONLINE_LOCAL_SGM')), 'github_or_online_profile_forbidden')
    require(machine in ('arm64', 'aarch64') and daemon in ('arm64', 'aarch64'), 'native_arm64_docker_required')


def effective_docker_endpoint(environment, context_reader):
    # DOCKER_CONTEXT takes precedence over DOCKER_HOST in the Docker CLI.
    if environment.get('DOCKER_CONTEXT'):
        endpoint=context_reader(environment['DOCKER_CONTEXT'])
    elif environment.get('DOCKER_HOST'):
        endpoint=environment['DOCKER_HOST']
    else:
        endpoint=context_reader(None)
    require(isinstance(endpoint,str) and endpoint.startswith('unix://') and endpoint[7:].startswith('/'), 'local_unix_docker_required')
    return endpoint


def daemon_build_proxies(info, *, client=False):
    """Reuse only the local daemon's credential-free public build transport."""
    result = {}
    for field, key in (('HTTPProxy', 'HTTP_PROXY'), ('HTTPSProxy', 'HTTPS_PROXY')):
        value = info.get(field)
        if not value:
            continue
        value = value if '://' in value else 'http://' + value
        parsed = urllib.parse.urlsplit(value)
        hosts = ('localhost', '127.0.0.1', '::1') if client else ('http.docker.internal', 'localhost', '127.0.0.1', '::1')
        require(parsed.scheme in ('http', 'https') and parsed.hostname in hosts and
                not parsed.username and not parsed.password and parsed.path in ('', '/') and
                not parsed.query and not parsed.fragment, 'credential_free_local_daemon_proxy_required')
        result[key] = value
    return result


def private_docker_client(life, endpoint):
    directory=life.work/'docker-client'
    directory.mkdir(mode=0o700)
    config={'auths':{},'cliPluginsExtraDirs':[str(Path.home()/'.docker/cli-plugins')]}
    path=directory/'config.json'
    with open(path,'x',opener=lambda p,f:os.open(p,f,0o600)) as stream:
        json.dump(config,stream)
    life.env.pop('DOCKER_CONTEXT',None)
    life.env.update(DOCKER_HOST=endpoint,DOCKER_CONFIG=str(directory))
    return directory


def validate_resources(rows, allowed_exits=(), *, normal_stops=(), label=LABEL):
    cpu = memory = 0
    for row in rows:
        host, state = row['HostConfig'], row['State']
        require(not state.get('OOMKilled'), 'owned_container_oom')
        if state.get('Status') in ('exited', 'dead'):
            # BuildKit's verified normal `buildx stop` exits 1. Only that
            # explicitly stopped, exact owned builder may use this status.
            planned_builder = row.get('Id') in allowed_exits
            require(state.get('ExitCode') == 0 or planned_builder and state.get('Status')=='exited' and state.get('ExitCode')==1, 'owned_container_failed')
            labels=row.get('Config',{}).get('Labels',{}) or {}
            require(row.get('Id') in allowed_exits or row.get('Id') in normal_stops and state.get('ExitCode') == 0 or labels.get('com.docker.compose.service') in ('runtime-log-store-init','trace-store-init') or labels.get(label+'.role')=='oneshot', 'owned_service_exited')
        if not state.get('Running'):
            continue
        quota = host.get('NanoCpus', 0) / 1e9
        if not quota and host.get('CpuQuota', 0) > 0 and host.get('CpuPeriod', 0) > 0:
            quota = host['CpuQuota'] / host['CpuPeriod']
        require(quota > 0 and host.get('Memory', 0) > 0, 'owned_container_without_hard_limits')
        cpu += quota
        memory += host['Memory']
    require(cpu <= 8 + 1e-7 and memory <= 12 * 1024**3, 'aggregate_hard_limit_exceeded')
    return cpu, memory


def isolate(document, run_id, images, *, label=LABEL, limits=LIMITS):
    result = {'name': run_id, 'services': {}, 'networks': {
        'data': {'internal': True, 'labels': {label: run_id}},
        'entry': {'labels': {label: run_id}}}, 'volumes': {}}
    for name, original in document.items():
        service = copy.deepcopy(original)
        require(name in limits and not service.get('privileged') and 'network_mode' not in service, 'unexpected_service_contract')
        service.update(container_name=run_id + '-' + name, image=images.get(name, service.get('image')),
                       restart='no', labels={**service.get('labels', {}), label: run_id},
                       networks={'data': {'aliases': [name]}}, cpus=limits[name][0], mem_limit=f'{limits[name][1]}m')
        require(isinstance(service.get('image'),str) and service['image'], 'every_service_requires_built_image')
        for key in ('build', 'depends_on', 'ports', 'profiles', 'extends'):
            service.pop(key, None)
        mounts = []
        for mount in service.get('volumes', []):
            target = mount['target']
            if target == '/var/log/addp':
                source = 'runtime-logs'
            elif mount['type'] == 'volume':
                source = str(mount['source'])
            else:
                # Only product configuration and this run's private TLS are mounts.
                require(mount.get('read_only') and not target.startswith('/capacity'), 'unexpected_writable_bind')
                mounts.append(mount)
                continue
            result['volumes'][source] = {'labels': {label: run_id}}
            mounts.append({'type': 'volume', 'source': source, 'target': target, 'read_only': mount.get('read_only', False), 'volume':{'nocopy':True}})
        service['volumes'] = mounts
        if name in ('postgres', 'system-backend', 'gateway', 'nginx'):
            service['networks']['entry'] = {}
            port = {'postgres': 5432, 'system-backend': 8180, 'gateway': 8000, 'nginx': 80}[name]
            service['ports'] = [{'target': port, 'host_ip': '127.0.0.1', 'published': '0'}]
        result['services'][name] = service
    return result


class Lifecycle:
    label = LABEL
    run_prefix = 'addp-trace-local-'
    strict_resources = False
    work_seconds = MAX_SECONDS
    planned_stops = frozenset()
    product_services = BACKENDS+FRONTENDS
    compile_services = BACKENDS+('runtime-log',)

    @property
    def sampling_image(self):
        return self.minio_image

    @contextmanager
    def sampling_lease(self, began, *, deadline=None):
        seconds=max(0, min(began+4,deadline)-time.monotonic()) if deadline is not None else (max(0, 4-(time.monotonic()-began)) if self.strict_resources else -1)
        require(self.sample_lock.acquire(timeout=seconds), 'resource_sample_deadline_exceeded')
        try:
            yield
        finally:
            self.sample_lock.release()

    def __init__(self):
        self.run_id = self.run_prefix + uuid.uuid4().hex
        self.uuid = str(uuid.uuid4())
        self.work = Path(tempfile.mkdtemp(prefix=self.run_id + '-')).resolve()
        self.work.chmod(0o700)
        require(not self.work.is_relative_to(ROOT), 'private_work_must_be_external')
        self.started = time.monotonic()
        self.phase = 'admission'
        self.builder = self.run_id
        self.builder_ids = set()
        self.builder_created = False
        self.builder_stopped = False
        self.builder_volumes = set()
        self.tags = []
        self.env = safe_environment()
        self.stop = threading.Event()
        self.failure = None
        self.samples = []
        self.thread = None
        self.sample_lock = threading.Lock()
        self.sampler_volumes = ()
        self.sampler_started = False
        self.sample_deadline = None
        self.sample_thread_id = None
        self.command_evidence = CommandEvidence(label=self.label)
        self.checkout = self.work / 'source'

    def check(self):
        require(time.monotonic() - self.started < self.work_seconds, "total_deadline_exceeded")
        require(self.failure is None, self.failure or "resource_guard_failed")

    def request_timeout(self, seconds=10):
        self.check()
        return min(seconds, self.work_seconds - (time.monotonic() - self.started))

    def wait(self, seconds):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            self.check()
            time.sleep(min(.1, max(0, deadline-time.monotonic())))

    def command(self, args, *, env=None, cwd=None, data=None, timeout=600, guarded=True, output_limit=None, process_observer=None):
        entered_ns = time.monotonic_ns()
        began = entered_ns / 1e9
        deadline = began + timeout
        # The product command thread must not inherit the observer's deadline or
        # write into its diagnostics while both threads run concurrently.
        sampling = self.sample_thread_id == threading.get_ident()
        if sampling:
            deadline = min(deadline, self.sample_deadline)
        timing = self.command_evidence.begin(args, self.run_id, self.sample_phase if sampling else self.phase)
        timing.update(dict.fromkeys(('spawn_seconds', 'wait_seconds', 'output_seconds', 'cancel_cleanup_seconds'), 0.0))
        timing.update(dict.fromkeys(('spawn_started_monotonic_ns', 'spawn_finished_monotonic_ns',
            'wait_started_monotonic_ns', 'wait_finished_monotonic_ns', 'exit_observed_monotonic_ns',
            'output_started_monotonic_ns', 'output_finished_monotonic_ns', 'decision_monotonic_ns',
            'cancel_started_monotonic_ns', 'cancel_finished_monotonic_ns'), None))
        timing['entered_monotonic_ns'] = entered_ns
        timing['result'] = 'failed'
        if sampling:
            self.sample_step['commands'].append(timing)

        def check_deadline():
            now = time.monotonic()
            if guarded:
                require(self.failure is None, self.failure or 'resource_guard_failed')
                require(now - self.started < self.work_seconds, 'total_deadline_exceeded')
            require(now < deadline, 'command_deadline_exceeded')

        try:
            check_deadline()
            with tempfile.TemporaryFile() as output:
                timing['spawn_started_monotonic_ns'] = time.monotonic_ns()
                try:
                    p = subprocess.Popen(args, cwd=cwd or self.checkout if self.checkout.exists() else ROOT,
                                         env=env or self.env, stdin=subprocess.PIPE if data is not None else subprocess.DEVNULL,
                                         stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
                    timing['pid'] = p.pid
                finally:
                    timing['spawn_finished_monotonic_ns'] = time.monotonic_ns()
                    timing['spawn_seconds'] = round((timing['spawn_finished_monotonic_ns']-timing['spawn_started_monotonic_ns'])/1e9, 6)
                try:
                    if process_observer is not None:process_observer(p)
                    check_deadline()
                    timing['wait_started_monotonic_ns'] = time.monotonic_ns()
                    try:
                        if data is not None:
                            p.stdin.write(data.encode() if isinstance(data, str) else data)
                            p.stdin.close()
                        while True:
                            check_deadline()
                            if output_limit is not None:
                                require(os.fstat(output.fileno()).st_size <= output_limit, 'command_output_limit_exceeded')
                            try:
                                p.wait(timeout=min(.05, deadline-time.monotonic()))
                                timing['exit_observed_monotonic_ns'] = time.monotonic_ns()
                                break
                            except subprocess.TimeoutExpired:
                                continue
                    finally:
                        timing['wait_finished_monotonic_ns'] = time.monotonic_ns()
                        timing['wait_seconds'] = round((timing['wait_finished_monotonic_ns']-timing['wait_started_monotonic_ns'])/1e9, 6)
                    check_deadline()
                finally:
                    if p.poll() is None:
                        timing['cancel_started_monotonic_ns'] = time.monotonic_ns()
                        try:
                            os.killpg(p.pid, signal.SIGTERM)
                            try:
                                p.wait(timeout=1)
                            except subprocess.TimeoutExpired:
                                os.killpg(p.pid, signal.SIGKILL)
                                p.wait(timeout=1)
                            timing['exit_observed_monotonic_ns'] = time.monotonic_ns()
                        finally:
                            timing['cancel_finished_monotonic_ns'] = time.monotonic_ns()
                            timing['cancel_cleanup_seconds'] = round((timing['cancel_finished_monotonic_ns']-timing['cancel_started_monotonic_ns'])/1e9, 6)
                    elif timing['exit_observed_monotonic_ns'] is None:
                        timing['exit_observed_monotonic_ns'] = time.monotonic_ns()
                timing['output_started_monotonic_ns'] = time.monotonic_ns()
                try:
                    output.seek(0)
                    raw = output.read() if output_limit is None else output.read(output_limit+1)
                    require(output_limit is None or len(raw) <= output_limit, 'command_output_limit_exceeded')
                    text = raw.decode(errors='replace')
                finally:
                    timing['output_finished_monotonic_ns'] = time.monotonic_ns()
                    timing['output_seconds'] = round((timing['output_finished_monotonic_ns']-timing['output_started_monotonic_ns'])/1e9, 6)
                check_deadline()
            if p.returncode:
                # Raw output may carry login/SQL details. Export closed fields only.
                self.last_failure = {'exit_code': p.returncode, 'output_sha256': hashlib.sha256(raw).hexdigest(),
                                     'diagnostic_markers': diagnostic_markers(raw)}
                if self.phase.startswith('product_image_'):
                    self.last_failure['public_build']=public_build_failure_diagnostic(raw)
                if args[0] == 'du':
                    diagnostics=[line for line in text.splitlines() if not re.fullmatch(r'\d+\s+.+',line)]
                    self.last_failure['du_missing_file'] = bool(diagnostics) and all(line.startswith('du:') and line.endswith('No such file or directory') for line in diagnostics)
                raise GateError('command_failed_' + str(p.returncode))
            check_deadline()
            timing['decision_monotonic_ns'] = time.monotonic_ns()
            timing['result'] = 'passed'
            return text
        finally:
            if timing['decision_monotonic_ns'] is None:
                timing['decision_monotonic_ns'] = time.monotonic_ns()
            timing['finished_monotonic_ns'] = time.monotonic_ns()
            timing['duration_seconds'] = round((timing['finished_monotonic_ns']-entered_ns)/1e9, 6)
            self.command_evidence.finish(timing)

    def docker(self, *args, **kw):
        if args and args[0]=='run' and self.label+'.role=oneshot' in args:
            require('--rm' not in args,'oneshot_exit_state_must_be_retained')
        return self.command(['docker', *args], **kw)

    def private_json(self, name, value):
        p = self.work / name
        with open(p, 'x', opener=lambda path, flags: os.open(path, flags, 0o600)) as f:
            json.dump(value, f)
        return p

    def owned(self, kind, **kw):
        return self.docker(kind, 'ls', *(['-a'] if kind == 'container' else []), '-q', '--filter',
                           f'label={self.label}={self.run_id}', guarded=False, **kw).split()

    def resume_builder(self):
        require(self.builder_stopped and bool(self.builder_ids),'owned_builder_resume_requires_planned_stop')
        self.docker('buildx','inspect',self.builder,'--bootstrap')
        rows=json.loads(self.docker('inspect',*sorted(self.builder_ids)))
        require({row['Id'] for row in rows}==self.builder_ids and all(row['State'].get('Running') and not row['State'].get('OOMKilled') for row in rows),
                'owned_builder_resume_not_healthy')
        with self.sample_lock:
            self.builder_stopped=False
        self.sample()

    def owned_bind_bytes(self, *, timeout):
        output=self.command(['du','-sk',str(self.work)],guarded=False,timeout=timeout)
        return int(output.split()[0])*1024

    def prepare_volume_sampler(self, volumes, *, timeout):
        require(not self.sampler_started, 'volume_sampler_already_started')
        volumes=tuple(sorted(set(volumes)))
        args = ['run', '--pull', 'never', '-d', '--rm', '--name', self.run_id + '-space', '--label', f'{self.label}={self.run_id}',
                '--label', self.label+'.role=sampler', '--cpus', str(SAMPLER_CPU), '--memory', '64m',
                '--network', 'none', '--tmpfs', '/data:rw,noexec,nosuid,size=1m', '--entrypoint', '/bin/sh']
        for i,name in enumerate(volumes):
            args += ['--mount',f'type=volume,source={name},target=/space/{i},readonly']
        args += [self.sampling_image,'-ec','while :; do sleep 3600; done']
        self.docker(*args,guarded=False,timeout=timeout)
        self.sampler_started=True
        self.sampler_volumes=volumes

    def sample(self, *, deadline=None):
        waiting = time.monotonic()
        with self.sampling_lease(waiting,deadline=deadline):
            if self.strict_resources and self.samples:
                require(waiting-self.samples[-1]['sample_started_monotonic'] <= 5, 'resource_sample_cadence_exceeded')
            acquired = time.monotonic()
            self.sample_began = waiting if self.strict_resources or deadline is not None else acquired
            self.sample_deadline = min(self.sample_began+4,deadline) if deadline is not None else self.sample_began+4
            self.sample_thread_id = threading.get_ident()
            self.sample_phase = self.phase
            self.sample_steps = []
            self.sample_counts = {'container_count': None, 'volume_count': None}
            self.sample_measured = {}
            self.sample_lock_wait = acquired - waiting
            self.sample_no_retry = self.strict_resources or deadline is not None
            try:
                return self._sample()
            except Exception as error:
                self.resource_failure={'operation':getattr(self,'sample_operation','admission'),
                    'error_code':str(error) if isinstance(error,GateError) else 'resource_sample_failed',
                    'elapsed_seconds':round(time.monotonic()-self.started,2),
                    **self.sample_diagnostics(), 'measured_resources': self.sample_measured,
                    'exception_type':type(error).__name__}
                if hasattr(self,'measurement_failure'):
                    self.resource_failure['measurement'] = self.measurement_failure
                raise
            finally:
                self.sample_thread_id = None
                self.sample_deadline = None
                del self.sample_no_retry

    def sample_diagnostics(self):
        finished = time.monotonic()
        return {'phase': self.sample_phase, 'budget_seconds': 4,
                'sample_duration_seconds': round(finished-self.sample_began, 6),
                'sample_started_monotonic': self.sample_began, 'sample_finished_monotonic': finished,
                'lock_wait_seconds': round(self.sample_lock_wait, 6),
                **self.sample_counts, 'steps': self.sample_steps}

    @contextmanager
    def measure_step(self, operation):
        self.sample_operation = operation
        began = time.monotonic()
        self.sample_step = {'operation': operation, 'commands': [], 'result': 'failed'}
        self.sample_steps.append(self.sample_step)
        try:
            yield
            require(time.monotonic() < self.sample_deadline, 'resource_sample_deadline_exceeded')
            if self.sample_step['result'] != 'retry':
                self.sample_step['result'] = 'passed'
        except BaseException:
            self.sample_step['result'] = 'failed'
            raise
        finally:
            self.sample_step['duration_seconds'] = round(time.monotonic()-began, 6)
            self.sample_step['processing_seconds'] = round(max(0, self.sample_step['duration_seconds'] -
                sum(command['duration_seconds'] for command in self.sample_step['commands'])), 6)

    def stop_builder(self):
        before=json.loads(self.docker('inspect',*sorted(self.builder_ids)))
        require(all(r['State'].get('Running') and not r['State'].get('OOMKilled') for r in before),'builder_not_running_before_stop')
        self.builder_stopped=True
        self.docker('buildx','stop',self.builder)
        rows=json.loads(self.docker('inspect',*sorted(self.builder_ids)))
        self.builder_stop_states=[{'id':r['Id'],'status':r['State'].get('Status'),
            'exit_code':r['State'].get('ExitCode'),'oom':r['State'].get('OOMKilled')} for r in rows]
        validate_resources(rows,self.builder_ids)

    def _sample(self):
        retries={'volume':0,'private':0}
        no_retry=getattr(self,'sample_no_retry',self.strict_resources)
        if hasattr(self,'measurement_failure'):del self.measurement_failure
        def remaining():
            value=self.sample_deadline-time.monotonic()
            require(value>0, 'resource_sample_deadline_exceeded')
            return value
        with self.measure_step('list_owned_containers'):
            if self.builder_created and not self.builder_ids:
                self.builder_ids.update(self.docker('ps','-aq','--filter','name=^buildx_buildkit_'+self.builder+'0$',guarded=False,timeout=remaining()).split())
            ids = set(self.owned('container', timeout=remaining())) | self.builder_ids
            self.sample_counts['container_count'] = len(ids)
        with self.measure_step('inspect_owned_containers'):
            rows = json.loads(self.docker('inspect', *(['--size'] if self.strict_resources else []), *sorted(ids), guarded=False, timeout=remaining())) if ids else []
            remaining()
        with self.measure_step('validate_container_states'):
            try:
                cpu, memory = validate_resources(rows, self.builder_ids if self.builder_stopped else (), normal_stops=self.planned_stops, label=self.label)
            except GateError:
                self.failed_container_states=[{'id':r['Id'],'service':(r.get('Config',{}).get('Labels') or {}).get('com.docker.compose.service'),
                    'status':r['State'].get('Status'),'exit_code':r['State'].get('ExitCode'),'oom':r['State'].get('OOMKilled')} for r in rows]
                raise
            remaining()
            self.sample_measured.update(cpu_hard_limit=cpu, memory_hard_limit_bytes=memory)
        with self.measure_step('list_owned_volumes'):
            volumes = self.owned('volume', timeout=remaining())
            if self.builder_ids:
                for row in rows:
                    if row['Id'] in self.builder_ids:
                        builder_volumes = [m['Name'] for m in row.get('Mounts', []) if m['Type'] == 'volume']
                        self.builder_volumes.update(builder_volumes)
                        volumes += builder_volumes
            volumes = tuple(sorted(set(volumes)))
            self.sample_counts['volume_count'] = len(volumes)
        writable = 0
        if self.strict_resources:
            require(all(type(row.get('SizeRw')) is int and row['SizeRw'] >= 0 for row in rows), 'owned_writable_layer_measurement_missing')
            writable = sum(row['SizeRw'] for row in rows)
            self.sample_measured['container_writable_bytes'] = writable
        total = writable
        if volumes:
            # Keep Docker create/start outside repeated du measurements. Rebuild
            # this owned read-only helper only when its exact mount set changes.
            if volumes != self.sampler_volumes:
                with self.measure_step('prepare_volume_sampler'):
                    if self.sampler_started:
                        self.docker('rm','-fv',self.run_id+'-space',guarded=False,timeout=remaining())
                        self.sampler_started = False
                    self.prepare_volume_sampler(volumes,timeout=remaining())
            while True:
                with self.measure_step('measure_owned_volumes'):
                    measured = self.docker('exec',self.run_id+'-space','/bin/sh','-ec',
                        'du -sk /space/* | awk \'{s+=$1} END {print s*1024}\'',guarded=False,timeout=remaining()).strip()
                    lines=measured.splitlines()
                    if len(lines)==1 and re.fullmatch(r'\d+(?:\.\d+)?(?:[eE][+]?[0-9]+)?',measured):
                        total=writable+int(float(measured));remaining();break
                    self.measurement_failure = {'output_sha256':hashlib.sha256(measured.encode()).hexdigest(),
                        'line_count':len(lines),'du_diagnostic':any(line.startswith('du:') for line in lines)}
                    diagnostics=[line for line in lines if not re.fullmatch(r'\d+(?:\.\d+)?(?:[eE][+]?[0-9]+)?',line)]
                    if diagnostics and all(line.startswith('du:') and line.endswith('No such file or directory') for line in diagnostics):
                        require(not no_retry, 'owned_resource_measurement_failed')
                        self.sample_step['result'] = 'retry'
                        retries['volume']+=1;remaining();continue
                    raise GateError('owned_volume_measurement_invalid') from None
        while True:
            with self.measure_step('measure_private_bindings'):
                try:
                    total += self.owned_bind_bytes(timeout=remaining());remaining();break
                except GateError:
                    if no_retry or not getattr(self,'last_failure',{}).get('du_missing_file'):raise
                    self.sample_step['result'] = 'retry'
                    retries['private']+=1;remaining()
        if hasattr(self,'measurement_failure'):del self.measurement_failure
        if getattr(self,'last_failure',{}).get('du_missing_file'):del self.last_failure
        self.sample_measured['volume_allocated_bytes'] = total
        with self.measure_step('final_validation'):
            require(total < VOLUME_STOP, 'owned_volume_stop_line_reached')
            remaining()
        sample = {'elapsed_seconds': round(time.monotonic()-self.started, 2), **self.sample_diagnostics(),
                  'cpu_hard_limit': cpu, 'memory_hard_limit_bytes': memory,
                  'volume_allocated_bytes': total, 'container_writable_bytes': writable, 'complete_scan_retries': retries}
        remaining()
        self.samples.append(sample)

    def monitor(self):
        while not self.stop.is_set():
            began=time.monotonic()
            try:
                self.check()
                self.sample()
            except Exception as error:
                self.failure = str(error) if isinstance(error, GateError) else 'resource_sample_failed'
                if self.failure == 'command_deadline_exceeded':
                    self.failure = 'resource_sample_deadline_exceeded'
                if hasattr(self,'resource_failure'):
                    self.resource_failure['error_code'] = self.failure
                else:
                    self.resource_failure = {'operation':getattr(self,'sample_operation','admission'),
                        'error_code':self.failure,'elapsed_seconds':round(time.monotonic()-self.started,2),
                        'sample_duration_seconds':round(time.monotonic()-began,3), 'exception_type':type(error).__name__}
                return
            self.stop.wait(max(0, 4-time.monotonic()+began))

    def cleanup(self):
        self.stop.set()
        errors = []
        child=getattr(self,'cache_reclaim_process',None)
        try:child_running=child is not None and child.poll() is None
        except OSError:
            if hasattr(self,'cache_reclaim_receipt'):self.cache_reclaim_receipt['cleanup_child_stopped']=False
            return ['cache_reclaim_child_status_unknown']
        if child_running:
            try:
                try:os.killpg(child.pid,signal.SIGKILL)
                except ProcessLookupError:pass
                child.wait(timeout=1)
            except (OSError,subprocess.TimeoutExpired):
                if hasattr(self,'cache_reclaim_receipt'):self.cache_reclaim_receipt['cleanup_child_stopped']=False
                return ['cache_reclaim_child_not_stopped']
            if child.poll() is None:
                if hasattr(self,'cache_reclaim_receipt'):self.cache_reclaim_receipt['cleanup_child_stopped']=False
                return ['cache_reclaim_child_not_stopped']
        if child is not None and hasattr(self,'cache_reclaim_receipt'):self.cache_reclaim_receipt['cleanup_child_stopped']=True
        if getattr(self,'cache_reclaim_lock_held',False):
            self.sample_lock.release()
            self.cache_reclaim_lock_held=False
        if self.thread:
            self.thread.join(timeout=10 if self.strict_resources else 6)
            if self.thread.is_alive():
                errors.append('resource_guard_not_stopped')
                if self.strict_resources:return errors
        self.phase = 'cleanup'
        # Release read-only mounts before Buildx removes its state volume,
        # including a helper created just before its client timed out.
        try:
            helpers=self.docker('ps','-aq','--filter',f'label={self.label}={self.run_id}',
                '--filter','name=^'+self.run_id+'-space$',guarded=False).split()
            if helpers:self.docker('rm','-fv',*helpers,guarded=False)
            self.sampler_started=False
        except GateError:
            errors.append('volume_sampler')
        try:
            self.docker('buildx', 'rm', '-f', self.builder, guarded=False)
        except GateError:
            # Absence is expected before builder creation; verify below.
            if self.builder_created:
                errors.append('builder')
        try:
            require(not self.docker('ps','-aq','--filter','name=^buildx_buildkit_'+self.builder+'0$',guarded=False).strip(), 'builder_container_residue')
            for volume in self.builder_volumes:
                require(not self.docker('volume','ls','-q','--filter','name=^'+re.escape(volume)+'$',guarded=False).strip(), 'builder_volume_residue')
        except GateError:
            errors.append('builder_residue')
        for kind, operation in [('container', ['rm', '-fv']), ('network', ['rm']), ('volume', ['rm'])]:
            try:
                ids = self.owned(kind)
                if ids:
                    self.docker(kind, *operation, *ids, guarded=False)
                require(not self.owned(kind), kind + '_residue')
            except GateError:
                errors.append(kind)
        for tag in reversed(self.tags):
            try:
                if self.docker('image', 'ls', '-q', tag, guarded=False).strip():
                    self.docker('image', 'rm', tag, guarded=False)
            except GateError:
                errors.append('image_tag')
        try:
            shutil.rmtree(self.work)
            require(not self.work.exists(), 'private_directory_residue')
        except OSError:
            errors.append('private_directory')
        return errors


def isolated_defaults(life):
    text = life.command(['bash', '-c', 'set -a; source .env.example; env -0'], env=life.env)
    env = dict(item.split('=', 1) for item in text.split('\0') if '=' in item)
    # Do not retain user's shell environment from the shell defaults.
    env.update(life.env)
    for key in env:
        if key.endswith(('_PASSWORD', '_SECRET', '_KEY', '_PEPPER')):
            env[key] = secrets.token_hex(32)
    for key in ('ENCRYPTION_KEY', 'IAM_MFA_ENCRYPTION_KEY', 'OAUTH_USER_CODE_PEPPER'):
        env[key] = base64.b64encode(secrets.token_bytes(32)).decode()
    return env


def defaults(life):
    env = isolated_defaults(life)
    secret = secrets.token_hex(32)
    env.update(ENV='test', POSTGRES_DB='addp_online', POSTGRES_USER='trace_local', POSTGRES_PASSWORD=secrets.token_hex(24),
               POSTGRES_HOST='127.0.0.1', POSTGRES_PORT='5432', ADDP_EXECUTION_TRACE_LOCAL='1',
               ADDP_EXECUTION_TRACE_RUN_ID=life.uuid, COMPOSE_DISABLE_ENV_FILE='1',
               ADDP_PUBLIC_ORIGIN='http://nginx', ALLOWED_ORIGINS='http://nginx',
               ADDP_HOST_NODE_NAME='execution-trace-local', ADDP_RUNTIME_LOG_ROOT=str(life.work/'unused-logs'),
               ADDP_OBSERVABILITY_LOGS_ENABLED='true', ADDP_OBSERVABILITY_METRICS_ENABLED='false',
               LOKI_TEST_SECRET=secret, LOKI_READ_TOKEN=secret+'-read', LOKI_WRITE_TOKEN=secret+'-write',
               MINIO_ROOT_USER='test-admin', MINIO_ROOT_PASSWORD=secret, LOKI_RETENTION_HOURS='168',
               LOKI_URL='http://runtime-log-api:3100', TEMPO_S3_SECRET_KEY=secrets.token_hex(32),
               ADDP_TRACE_DEPLOYMENT_DIR=str(life.work/'deployment'), LOKI_TEST_SOURCE=str(life.work/'unused-logs'),
               TRACE_T2_WORK=str(life.work),
               ADDP_RUNTIME_LOG_OWNER='0:0', IMAGE_TAG=life.run_id, REGISTRY='localhost:5001',
               NO_PROXY='127.0.0.1,localhost', no_proxy='127.0.0.1,localhost')
    return env


def committed_inputs(life, commit):
    integration={'Makefile':b'test-monitor-traces-local:', 'scripts/ci/check-online-ci-registration.py':b'def validate_local_execution_trace_profile', 'system/backend/cmd/online-test-fixture/main.go':b'return prepareExecutionTraceLocalIdentities(db, cfg, *output)'}
    result={}
    for relative in OWN_INPUTS:
        expected=life.command(['git','show',commit+':'+relative]).encode()
        # Shared integration files can contain another session's uncommitted
        # hunks. The committed source is the only build input; the suite's own
        # new files and private owner paths must match it byte for byte.
        if relative in integration:
            require(integration[relative] in expected, 'committed_local_integration_missing')
        else:
            require((ROOT/relative).read_bytes()==expected, 'commit_all_local_inputs_before_acceptance')
        result[relative]=hashlib.sha256(expected).hexdigest()
    return result


def build(life):
    life.phase = 'source_snapshot'
    commit = life.command(['git', 'rev-parse', 'HEAD']).strip()
    life.input_hashes=committed_inputs(life,commit)
    life.command(['bash','-c','umask 022; exec "$@"','_', 'git', 'clone', '--no-local', '--depth', '1', '--single-branch', '--branch', 'main', ROOT.as_uri(), str(life.checkout)])
    require(life.command(['git', 'rev-parse', 'HEAD']).strip() == commit, 'snapshot_commit_changed')
    env = defaults(life)
    life.env = env
    life.source_commit = commit
    life.phase = 'prebuilt_minio_verification'
    verify_minio_product(life)
    # Reject product configuration errors before the expensive compilation.
    life.deployment_documents=prepare_deployment_documents(life,env)
    verify_telemetry_configuration(life)
    references={name:f'localhost:5001/addp-{name}:{life.run_id}' for name in BACKENDS+FRONTENDS}
    prepare_isolated_configuration(life,env,references)
    prepare_sampling_volumes(life)
    proxies = prepare_builder(life)
    life.phase='initial_resource_sample'
    life.sample()
    life.thread=threading.Thread(target=life.monitor,daemon=True)
    life.thread.start()
    life.phase='isolated_dependencies_start'
    start_dependencies(life.compose,(life.checkout/'scripts/infra/init-postgresql.sql').read_text(),
                       lambda phase:setattr(life,'phase',phase))
    life.dependencies_ready=True
    life.phase='builder_stop_guard_preflight'
    life.stop_builder()
    images=build_products(life,env,proxies)
    life.phase='product_nginx_route_contract'
    verify_nginx_routes(life,images['nginx'])
    return commit,env,images


def prepare_builder(life):
    # All builds use the product Make entry, with a private bounded BuildKit worker.
    life.phase = 'bounded_builder'
    config = life.work/'buildkit.toml'
    config.write_text('[registry."localhost:5001"]\n  http = true\n  insecure = true\n')
    proxies=daemon_build_proxies(json.loads(life.docker('info','--format',
        '{"HTTPProxy":{{json .HTTPProxy}},"HTTPSProxy":{{json .HTTPSProxy}}}')))
    proxy_options=[part for key,value in proxies.items() for part in ('--driver-opt','env.'+key+'='+value)]
    life.docker('buildx', 'create', '--name', life.builder, '--driver', 'docker-container', '--driver-opt',
                'network=host,cpu-period=100000,cpu-quota=100000,memory=2g', *proxy_options, '--buildkitd-config', str(config))
    life.builder_created = True
    life.docker('buildx', 'inspect', life.builder, '--bootstrap')
    ids = life.docker('ps', '-aq', '--filter', 'name=^buildx_buildkit_' + life.builder + '0$').split()
    require(len(ids) == 1, 'owned_builder_identity_missing')
    life.builder_ids = set(json.loads(life.docker('inspect', *ids))[0]['Id'] for _ in [0])
    life.phase='volume_sampler_admission'
    rows=json.loads(life.docker('inspect',*sorted(life.builder_ids)))
    life.builder_volumes.update(m['Name'] for row in rows for m in row.get('Mounts',[]) if m['Type']=='volume')
    life.prepare_volume_sampler(life.owned('volume')+sorted(life.builder_volumes),timeout=30)
    return proxies


def reclaim_go_build_cache(life):
    """One owned cache route: fresh full check, then bounded exclusive deletion."""
    target=life.checkout/'.cache/go-build'
    receipt={'target':'owned_go_build_cache','result':'failed','precheck':{'result':'failed'},
             'deletion':{'result':'not_started','attempted':False}}
    life.cache_reclaim_receipt=receipt
    def valid_target():
        require(life.work.is_absolute() and life.work.resolve()==life.work and not life.work.is_relative_to(ROOT) and
            life.checkout==life.work/'source' and target.is_dir() and target.resolve()==target and target.is_relative_to(life.work),
            'go_cache_reclaim_target_invalid')
    def closed_error(error):
        return str(error) if isinstance(error,GateError) and re.fullmatch('[a-z0-9_]+',str(error)) else type(error).__name__
    life.phase='go_cache_precheck'
    began=time.monotonic();precheck=receipt['precheck'];precheck.update(budget_seconds=4,started_monotonic=began)
    try:
        life.check();valid_target()
        life.sample(deadline=began+4)
        require(time.monotonic()<began+4,'resource_sample_deadline_exceeded')
        life.check();precheck['result']='passed'
    except BaseException as error:
        precheck['error_code']=closed_error(error);raise
    finally:
        finished=time.monotonic()
        precheck.update(finished_monotonic=finished,elapsed_seconds=round(finished-began,6))
        if precheck['result']=='passed' and finished>=began+4:
            precheck.update(result='failed',error_code='resource_sample_deadline_exceeded')
            raise GateError('resource_sample_deadline_exceeded')
    life.phase='go_cache_reclaim'
    began=time.monotonic();deadline=began+4;step=receipt['deletion']
    step.update(result='failed',budget_seconds=4,started_monotonic=began,lock_wait_seconds=None,child_stopped=True)
    def remaining():
        seconds=deadline-time.monotonic()
        require(seconds>0,'go_cache_reclaim_deadline_exceeded')
        return seconds
    try:
        life.check()
        require(life.sample_lock.acquire(timeout=remaining()),'go_cache_reclaim_deadline_exceeded')
        life.cache_reclaim_lock_held=True
        step['lock_wait_seconds']=round(time.monotonic()-began,6)
        life.check();valid_target()
        step['attempted']=True
        def observe(child):
            life.cache_reclaim_process=child
            step.update(child_pid=child.pid,child_started_monotonic=time.monotonic())
        life.command([sys.executable,'-I','-c','import shutil,sys; shutil.rmtree(sys.argv[1])',str(target)],
            timeout=remaining(),process_observer=observe)
        remaining();life.check()
        require(not target.exists(),'go_cache_reclaim_residue')
        receipt['result']=step['result']='passed'
    except BaseException as error:
        step['error_code']=closed_error(error);raise
    finally:
        child=getattr(life,'cache_reclaim_process',None)
        try:step['child_stopped']=child is None or child.poll() is not None
        except OSError:step['child_stopped']=False
        if getattr(life,'cache_reclaim_lock_held',False) and step['child_stopped']:
            life.sample_lock.release();life.cache_reclaim_lock_held=False
        finished=time.monotonic()
        step.update(finished_monotonic=finished,elapsed_seconds=round(finished-began,6),
            lock_retained_for_cleanup=bool(getattr(life,'cache_reclaim_lock_held',False)))
        if receipt['result']=='passed' and (finished>=deadline or not step['child_stopped']):
            receipt['result']=step['result']='failed'
            step['error_code']='go_cache_reclaim_deadline_exceeded' if finished>=deadline else 'go_cache_reclaim_child_not_stopped'
            raise GateError(step['error_code'])


def build_products(life,env,proxies):
    docker_path = shutil.which('docker', path=env['PATH'])
    shim = life.work/'tools';shim.mkdir()
    wrapper = shim/'docker'
    wrapper.write_text('#!/usr/bin/env python3\nimport os,sys\na=sys.argv[1:]\n'
       'if a and a[0]=="build":\n'
       ' p=[v for k in ("HTTP_PROXY","HTTPS_PROXY") if os.environ.get("TRACE_LOCAL_"+k) for key in (k,k.lower()) for v in ("--build-arg",key+"="+os.environ["TRACE_LOCAL_"+k])]\n'
       ' a=["buildx","--builder",os.environ["TRACE_LOCAL_BUILDER"],"build","--load",*p,*a[1:]]\n'
       'elif a and a[0]=="run": a=["run","--cpus","1","--memory","2g","--label",os.environ["TRACE_LOCAL_LABEL"],"--label",os.environ["TRACE_LOCAL_ROLE_LABEL"],*[v for v in a[1:] if v!="--rm"]]\n'
       'if a and a[0]=="run":\n'
       ' a=[v+":ro" if v.endswith(":/go/pkg/mod") else v for v in a]\n'
       'os.execv(os.environ["TRACE_LOCAL_DOCKER"],[os.environ["TRACE_LOCAL_DOCKER"],*a])\n')
    wrapper.chmod(0o700)
    life.public_modcache=Path(life.command(['go','env','GOMODCACHE']).strip()).resolve()
    require(life.public_modcache.is_dir() and (life.public_modcache/'cache/download').is_dir(), 'public_readonly_go_cache_required')
    # Registry token exchange occurs in the host Buildx client. A daemon-only
    # proxy hostname is not a host client transport; select the separately
    # validated credential-free loopback proxy instead of inheriting the shell.
    client_proxies=daemon_build_proxies({'HTTPProxy':os.environ.get('HTTP_PROXY') or os.environ.get('http_proxy'),
                                       'HTTPSProxy':os.environ.get('HTTPS_PROXY') or os.environ.get('https_proxy')},client=True)
    build_env={**env, **client_proxies, 'PATH':str(shim)+os.pathsep+env['PATH'], 'TRACE_LOCAL_DOCKER':docker_path, 'TRACE_LOCAL_BUILDER':life.builder, 'TRACE_LOCAL_LABEL':life.label+'='+life.run_id, 'TRACE_LOCAL_ROLE_LABEL':life.label+'.role=oneshot', 'ADDP_BUILD_GOMODCACHE':str(life.public_modcache), **{'TRACE_LOCAL_'+k:v for k,v in proxies.items()}}
    life.phase='product_compile'
    life.docker('run', '--name', life.run_id+'-compile', '--label', f'{life.label}={life.run_id}', '--label', life.label+'.role=oneshot', '--cpus', '2', '--memory', '3g',
                '--mount', f'type=bind,source={life.checkout},target=/workspace',
                '--mount', f'type=bind,source={life.public_modcache},target=/dependencies,readonly',
                '-e','ADDP_BUILD_GOMODCACHE=/dependencies','-w', '/workspace',
                '-e', 'GOMAXPROCS=2', '-e', 'GOFLAGS=-p=2', 'golang:1.24',
                'make', 'build', 'BUILD_ARGS=--arch arm64 --services '+','.join(life.compile_services), timeout=2200)
    # Product binaries are complete; reclaim only this run's disposable Go
    # object cache before the next bounded build consumes the same disk budget.
    reclaim_go_build_cache(life)
    life.phase='bounded_builder_resume'
    life.resume_builder()
    life.phase='product_images'
    services=life.product_services
    references=[f'localhost:5001/addp-{name}:{life.run_id}' for name in services]
    life.tags.extend(references)
    for service in services:
        life.phase='product_image_'+service.replace('-','_')
        life.command(['make','build-images','IMAGE_BUILD_ARGS=--verify --jobs 1 --services '+service+
                      ' --registry localhost:5001 --tag '+life.run_id],env=build_env,timeout=600)
        # Only this run's cache is pruned, keeping the approved disk stop line
        # meaningful without accumulating nine independent npm build trees.
        life.docker('buildx','prune','--builder',life.builder,'--all','--force',timeout=60)
    images={name:life.docker('image','inspect',tag,'--format','{{.Id}}').strip() for name,tag in zip(services,references)}
    life.stop_builder()
    life.phase='browser_dependencies'
    version=json.loads((life.checkout/'console/frontend/package-lock.json').read_text())['packages']['node_modules/@playwright/test']['version']
    life.browser_image='mcr.microsoft.com/playwright:v'+version+'-noble'
    life.docker('run','--name',life.run_id+'-browser-prepare','--label',f'{life.label}={life.run_id}',
                '--label',life.label+'.role=oneshot','--cpus','1','--memory','2g',
                '--mount',f'type=bind,source={life.checkout},target=/workspace','-w','/workspace/console/frontend',
                *[part for k,v in proxies.items() for key in (k,k.lower()) for part in ('-e',key+'='+v)],
                life.browser_image,'npm','ci','--no-audit','--no-fund',timeout=300)
    return images


def verify_nginx_routes(life, image):
    """Exercise the product config with private URI echoes, never user APIs."""
    network=life.run_id+'-route-contract'
    echo=life.run_id+'-route-echo'
    entry=life.run_id+'-route-entry'
    root=life.checkout if life.checkout.exists() else ROOT
    configuration=life.work/'route-echo.conf'
    configuration.write_text('events {}\nhttp { server { listen 80; listen 8000; return 200 "$request_uri"; } }\n')
    life.docker('network','create','--internal','--label',LABEL+'='+life.run_id,network)
    aliases=('console','gateway','system-frontend','manager-frontend','meta-frontend',
             'transfer-frontend','orchestrator-frontend','develop-frontend','monitor-frontend')
    common=['--label',LABEL+'='+life.run_id,'--label',LABEL+'.role=oneshot',
            '--cpus','.1','--memory','64m','--network',network]
    life.docker('run','-d','--name',echo,*common,
        *[part for alias in aliases for part in ('--network-alias',alias)],
        '--mount','type=bind,source='+str(configuration)+',target=/etc/nginx/nginx.conf,readonly',image)
    life.docker('run','-d','--name',entry,*common,
        '--mount','type=bind,source='+str(root/'nginx/nginx.conf')+',target=/etc/nginx/nginx.conf,readonly',image)
    ready=life.docker('exec',entry,'sh','-ec',
        'n=0; while [ "$n" -lt 40 ]; do if wget -qO /tmp/route-ready http://127.0.0.1/route-ready 2>/dev/null; then cat /tmp/route-ready; exit 0; fi; n=$((n+1)); sleep .1; done; exit 1',timeout=15)
    require(ready.strip()=='/route-ready','nginx_console_route_invalid')
    cases=[('/module-ui/'+name+'/nested/page?first=a%20b&second=2','/nested/page?first=a%20b&second=2')
           for name in ('system','manager','meta','transfer','orchestrator','develop')]
    cases += [('/module-ui/system/assets/app.js?version=1','/assets/app.js?version=1'),
              ('/module-ui/manager/plugins/viewer/file.js?version=2','/plugins/viewer/file.js?version=2'),
              ('/module-ui/manager/wasm/reader.wasm?version=3','/wasm/reader.wasm?version=3'),
              ('/module-ui/monitor/assets/app.js?version=4','/assets/app.js?version=4'),
              ('/api/v1/route-contract?version=5','/api/v1/route-contract?version=5')]
    for index,(path,expected) in enumerate(cases):
        life.phase='nginx_route_contract_case_'+str(index)
        actual=life.docker('exec',entry,'wget','-qO-','http://127.0.0.1'+path,timeout=10)
        require(actual.strip()==expected,'nginx_proxy_uri_or_query_changed')
    missing=life.docker('exec',entry,'sh','-ec',
        'wget -S -O /dev/null http://127.0.0.1/module-ui/service/missing 2>&1 || true',timeout=15)
    require(re.search(r'HTTP/1\.[01] 502(?: |\r|\n)',missing) is not None,'nginx_missing_module_must_return_502')
    require(life.docker('exec',entry,'wget','-qO-','http://127.0.0.1/module-ui/system/still-ready',timeout=10).strip()=='/still-ready',
            'nginx_missing_module_disrupted_selected_module')
    life.sample()
    life.docker('kill','--signal=QUIT',entry,echo)
    require(life.docker('wait',entry,echo,timeout=15).split()==['0','0'],'nginx_route_probe_exit_failed')
    life.sample()
    life.nginx_route_assertions={'uri_and_query_cases':len(cases),'missing_module_502':True,'selected_module_remains_readable':True}


def nginx_route_contract():
    """Generic bounded T2 check; does not invoke formal local admission or login."""
    life=Lifecycle();life.started-=MAX_SECONDS-120
    try:
        endpoint=effective_docker_endpoint(os.environ,lambda name: json.loads(life.docker('context','inspect',*([name] if name else [])))[0]['Endpoints']['docker']['Host'])
        private_docker_client(life,endpoint)
        source=(ROOT/'scripts/infra/traces.yml').read_text()
        match=re.search(r'^    image: (nginx:[^\s]+)$',source,re.M)
        require(match is not None,'pinned_nginx_contract_image_missing')
        verify_nginx_routes(life,match.group(1))
        print('Nginx route contract passed: '+json.dumps(life.nginx_route_assertions),flush=True)
    finally:
        errors=life.cleanup()
        require(not errors,'nginx_route_contract_cleanup_failed')
        print('Nginx route contract: zero owned resources',flush=True)


def prepare_sampling_volumes(life):
    life.phase='owned_volume_preparation'
    for logical in sorted(life.deployment_config['volumes']):
        name=life.run_id+'_'+logical
        labels={life.label:life.run_id,'com.docker.compose.project':life.run_id,'com.docker.compose.volume':logical}
        args=[part for key,value in labels.items() for part in ('--label',key+'='+value)]
        require(life.docker('volume','create',*args,name).strip()==name,'owned_volume_preparation_identity_mismatch')
        actual=json.loads(life.docker('volume','inspect',name))[0]
        require(actual.get('Name')==name and all(actual.get('Labels',{}).get(key)==value for key,value in labels.items()),
                'owned_volume_preparation_ownership_mismatch')


def start_dependencies(compose, initialization_sql, progress=lambda phase:None):
    progress('isolated_databases_start')
    compose('up','-d','--wait','--no-build','postgres','business-postgres','redis','minio')
    progress('isolated_platform_database_initialization')
    compose('exec','-T','postgres','sh','-ec',
            'exec psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB"',data=initialization_sql)
    for name in ('runtime-log-store-init','trace-store-init'):
        progress('isolated_'+name.replace('-','_'))
        compose('run','--no-deps',name)
    progress('isolated_telemetry_start')
    compose('up','-d','--wait','--no-build','loki','runtime-log-api','tempo','trace-query','alloy')


def start_deployment(compose):
    for group in [('system-backend',),('gateway','security-backend','develop-backend','orchestrator-backend','monitor-backend'),FRONTENDS]:
        compose('up','-d','--wait','--no-build',*group)


def verify_telemetry_configuration(life):
    for name in ('loki','trace-query'):
        life.phase='telemetry_configuration_'+name.replace('-','_')
        service=life.deployment_documents[name]
        args=['run','--name',life.run_id+'-validate-'+name,'--label',f'{LABEL}={life.run_id}',
            '--label',LABEL+'.role=oneshot','--cpus','.25','--memory','512m','--network','none',
            '--read-only','--tmpfs','/tmp:size=16m,mode=1777']
        if service.get('user'):args+=['--user',service['user']]
        for capability in service.get('cap_drop',[]):args+=['--cap-drop',capability]
        for mount in service.get('volumes',[]):
            if mount['type']=='bind':args+=['--mount',f'type=bind,source={mount["source"]},target={mount["target"]},readonly']
        for key,value in service.get('environment',{}).items():args+=['-e',key+'='+str(value)]
        if name=='loki':args += [service['image'],*service['command'],'-verify-config=true']
        else:args += ['--add-host','tempo:127.0.0.1','--entrypoint','nginx',service['image'],'-t']
        life.docker(*args,timeout=30)


def prepare_deployment_documents(life, env):
    life.phase='isolated_deployment_credentials'
    (life.work/'unused-logs').mkdir(mode=0o700)
    life.command([sys.executable,'scripts/test/trace-deployment-fixture.py',str(life.work)])
    env.update(ADDP_OBSERVABILITY_TRACES_ENABLED='true', MONITOR_TEMPO_URL='https://trace-query:8443',
               MONITOR_TRACE_TLS_DIR=str(life.work/'query-tls'),
               ADDP_TRACE_DEPLOYMENTS=json.dumps([{'module_name':m,'role':'ingress' if m=='gateway' else 'backend',
                  'endpoint':'https://alloy:4318/v1/traces','tls_dir':str(life.work/'source-tls')} for m in
                  ('system','gateway','security','develop','orchestrator','monitor')]))
    life.phase='isolated_deployment_trace_configuration'
    life.command([sys.executable,'scripts/prod/generate-trace-platform.py'],env=env)
    documents={}
    for filename,names in [('docker-compose.infra.yml',('postgres','redis')),('docker-compose.yml',BACKENDS+FRONTENDS),
                           ('scripts/test/docker-compose.monitor-traces-t2.yml',TRACE)]:
        life.phase='isolated_deployment_compose_'+('infra' if filename=='docker-compose.infra.yml' else 'product' if filename=='docker-compose.yml' else 'traces')
        doc=json.loads(life.command(['docker','compose','--env-file','/dev/null','-f',filename,'config','--format','json'],env=env))
        documents.update({name:doc['services'][name] for name in names})
    # The query proxy has no capabilities. Match its private TLS owner like
    # the standard Trace T2, rather than running a capability-less root master.
    documents['trace-query']['user']=str(os.getuid())+':'+str(os.getgid())
    # The same product PostgreSQL image owns the synthetic read-only query source.
    documents['business-postgres']=copy.deepcopy(documents['postgres'])
    documents['business-postgres']['environment'].update(POSTGRES_DB='trace_fixture')
    documents['business-postgres']['volumes'][0]['source']='business-postgres-data'
    documents['postgres']['command']+=['-c','cluster_name=addp-execution-trace-local-'+life.uuid]
    overlay=json.loads((life.work/'deployment/platform-traces.json').read_text())
    for name,row in overlay['services'].items():
        documents[name]['environment'].update(row['environment'])
        documents[name]['volumes']+=row['volumes']
    # Shared runtime source is a fresh named volume, consumed by receiver and Alloy.
    for name in TRACE:
        documents[name]['volumes']=[m for m in documents[name].get('volumes',[]) if not m['target'].startswith('/capacity')]
    documents['alloy']['volumes']=[m for m in documents['alloy']['volumes'] if m['target']!='/var/log/addp']+[
        {'type':'volume','source':'runtime-logs','target':'/var/log/addp','read_only':True}]
    return documents


def prepare_isolated_configuration(life, env, images):
    documents=life.deployment_documents
    for name in ('minio','runtime-log-store-init','trace-store-init'):
        images[name]=life.minio_image
    life.phase='isolated_deployment_projection'
    config=isolate(documents,life.run_id,images)
    # System log reads use the standard product runtime source path, not host logs.
    config['services']['system-backend']['environment'].update(LOKI_URL='http://runtime-log-api:3100',LOKI_READ_TOKEN=env['LOKI_READ_TOKEN'])
    path=life.private_json('compose.json',config)
    life.deployment_config=config
    life.compose_path=path
    life.compose=lambda *args,**kwargs:life.command(['docker','compose','--env-file','/dev/null','-f',str(path),*args],env=env,timeout=300,**kwargs)
    return config


def setup(life, env, images):
    config=life.deployment_config
    for name,image in images.items():config['services'][name]['image']=image
    life.compose_path.write_text(json.dumps(config))
    life.phase='isolated_deployment_start'
    start_deployment(life.compose)
    rows=json.loads(life.docker('inspect',*[life.run_id+'-'+name for name in BACKENDS]))
    require(all(row['Image']==images[name] for row,name in zip(rows,BACKENDS)), 'module_image_identity_changed')
    published={}
    for name,port in [('system-backend',8180),('gateway',8000),('nginx',80)]:
        binding=json.loads(life.docker('inspect',life.run_id+'-'+name))[0]['NetworkSettings']['Ports'][str(port)+'/tcp']
        require(len(binding)==1 and binding[0]['HostIp']=='127.0.0.1','loopback_publication_required')
        published[name]='http://127.0.0.1:'+binding[0]['HostPort']
    env.update(SYSTEM_URL=published['system-backend'],GATEWAY_URL=published['gateway'],CONSOLE_URL='http://nginx')
    probe='\n'.join('curl -fsS http://'+name+':'+str(port)+'/health/live; echo' for name,port in
                       [('system-backend',8180),('gateway',8000),('security-backend',8194),
                        ('develop-backend',8185),('orchestrator-backend',8084),('monitor-backend',8100)])
    output=life.docker('run','--name',life.run_id+'-build-probe','--label',f'{LABEL}={life.run_id}',
                      '--label',LABEL+'.role=oneshot','--cpus','.05','--memory','64m',
                      '--network',life.run_id+'_data','--entrypoint','/bin/sh',life.minio_image,'-ec',probe)
    facts=[json.loads(line) for line in output.splitlines() if line.strip()]
    require(len(facts)==len(BACKENDS) and all(row.get('git_commit')==life.source_commit and row.get('status')=='live' for row in facts),
            'module_live_build_commit_mismatch')
    life.module_commits={row['module']:row['git_commit'] for row in facts}
    return config


def summarize_resource_diagnostic(samples):
    """Summarize completed scans only; failed-tick details stay in resource_failure."""
    def distribution(values):
        values = sorted(values)
        return {**{f'p{q}': values[(len(values)*q+99)//100-1] if values else None for q in (50,95,99)},
                'max': max(values, default=None)}
    phases = {}
    for sample in samples:
        phase = sample['phase']
        name = ('compile' if phase=='product_compile' else 'images' if phase.startswith('product_image_') else
                'startup' if phase.startswith('diagnostic_startup_') else
                'idle' if phase=='diagnostic_idle' else 'steady' if phase=='diagnostic_steady' else 'preparation')
        phases.setdefault(name, []).append(sample)
    summary = {'sample_count': len(samples), 'phases': {}}
    starts = sorted(sample['sample_started_monotonic'] for sample in samples)
    summary['max_start_interval_seconds'] = max((b-a for a,b in zip(starts,starts[1:])), default=0)
    summary['max_complete_interval_seconds'] = max((b-a for a,b in zip(sorted(s['sample_finished_monotonic'] for s in samples),
        sorted(s['sample_finished_monotonic'] for s in samples)[1:])), default=0)
    summary['max_sample_seconds'] = max((s['sample_finished_monotonic']-s['sample_started_monotonic'] for s in samples), default=0)
    for name, rows in phases.items():
        operations = {}
        for row in rows:
            for step in row['steps']:
                operations.setdefault(step['operation'], []).append(step)
        summary['phases'][name] = {
            'sample_count':len(rows),
            'sample_seconds':distribution([s['sample_finished_monotonic']-s['sample_started_monotonic'] for s in rows]),
            'lock_wait_seconds':distribution([s['lock_wait_seconds'] for s in rows]),
            'container_count':{'min':min(s['container_count'] for s in rows),'max':max(s['container_count'] for s in rows)},
            'volume_count':{'min':min(s['volume_count'] for s in rows),'max':max(s['volume_count'] for s in rows)},
            'complete_scan_retries':{key:sum(s['complete_scan_retries'][key] for s in rows) for key in ('volume','private')},
            'operations':{operation:{
                'count':len(steps), 'duration_seconds':distribution([s['duration_seconds'] for s in steps]),
                'processing_seconds':distribution([s['processing_seconds'] for s in steps]),
                **{key:distribution([c[key] for s in steps for c in s['commands']])
                   for key in ('spawn_seconds','wait_seconds','output_seconds','cancel_cleanup_seconds')}}
                for operation,steps in operations.items()}}
    return summary


def validate_resource_diagnostic(summary, required_phases):
    require(summary['max_sample_seconds'] < 4, 'diagnostic_sample_deadline_exceeded')
    require(summary['max_start_interval_seconds'] <= 5, 'diagnostic_sample_cadence_exceeded')
    minimums = {'compile':12,'images':12,'idle':25,'steady':65}
    for phase in required_phases:
        require(summary['phases'].get(phase,{}).get('sample_count',0) >= minimums[phase],
                'diagnostic_phase_samples_insufficient')


def run_resource_guard_diagnostic(life, report, window_started):
    """One approved busy-stage round; disposable external driver owns cleanup."""
    command = life.command
    sample = life.sample
    stage_deadline = window_started + 1800
    compose = None
    def bounded_command(args, **kwargs):
        if kwargs.get('guarded',True):
            remaining = min(window_started+2400, stage_deadline)-time.monotonic()
            require(remaining > 0, 'diagnostic_phase_deadline_exceeded')
            kwargs['timeout'] = min(kwargs.get('timeout',600), remaining)
        return command(args, **kwargs)
    life.command = bounded_command
    def checked_sample():
        result = sample()
        starts = sorted(row['sample_started_monotonic'] for row in life.samples)
        require(all(b-a <= 5 for a,b in zip(starts,starts[1:])), 'diagnostic_sample_cadence_exceeded')
        return result
    life.sample = checked_sample
    try:
        commit, env, images = build(life)
        require(time.monotonic() < stage_deadline, 'diagnostic_build_deadline_exceeded')
        report.update(source_commit=commit, input_sha256=life.input_hashes, images=images)
        report['summary'] = summarize_resource_diagnostic(life.samples)
        validate_resource_diagnostic(report['summary'], ('compile','images'))
        stage_deadline = window_started + 2400
        life.phase = 'diagnostic_idle'
        life.wait(120)
        life.sample()
        report['summary'] = summarize_resource_diagnostic(life.samples)
        validate_resource_diagnostic(report['summary'], ('compile','images','idle'))
        compose = life.compose
        groups = [('system-backend',), ('gateway','security-backend','develop-backend','orchestrator-backend','monitor-backend'), FRONTENDS]
        report['startup_checks'] = []
        def checked_compose(*args, **kwargs):
            index = len(report['startup_checks'])//2
            require(index < len(groups) and args == ('up','-d','--wait','--no-build',*groups[index]),
                    'diagnostic_startup_group_mismatch')
            life.phase = 'diagnostic_startup_'+str(index+1)
            life.sample()
            report['startup_checks'].append({'group':index+1,'boundary':'before'})
            result = compose(*args, **kwargs)
            life.sample()
            report['startup_checks'].append({'group':index+1,'boundary':'after'})
            return result
        life.compose = checked_compose
        stage_deadline = min(window_started+2400, time.monotonic()+180)
        setup(life,env,images)
        require(time.monotonic() < stage_deadline, 'diagnostic_startup_deadline_exceeded')
        require(len(report['startup_checks']) == 6, 'diagnostic_startup_checks_incomplete')
        report['module_commits'] = life.module_commits
        stage_deadline = window_started+2400
        life.phase = 'diagnostic_steady'
        life.wait(300)
        life.sample()
        life.check()
        require(time.monotonic() < stage_deadline, 'diagnostic_window_deadline_exceeded')
        report['summary'] = summarize_resource_diagnostic(life.samples)
        validate_resource_diagnostic(report['summary'], ('compile','images','idle','steady'))
    finally:
        life.command = command
        life.sample = sample
        if compose is not None:
            life.compose = compose


def identity(life, env, *, suite="execution-trace-local", prefix="ADDP_EXECUTION_TRACE_"):
    life.phase='formal_identity_preparation'
    # Helper has this run's PG network namespace; localhost never means shared PG.
    identity_env=life.work/'identity.env'
    helper_env={k:v for k,v in env.items() if k not in ('PATH','HOME','TMPDIR','DOCKER_HOST','DOCKER_CONTEXT')}
    helper_env.update(GOWORK='off',GOMAXPROCS='1',GOFLAGS='-p=1',POSTGRES_HOST='127.0.0.1',POSTGRES_PORT='5432',
                      GOCACHE='/private/helper-go-cache',GOPATH='/private/helper-go-path',GOMODCACHE='/dependencies')
    private=life.work/'helper.env';private.write_text('\n'.join(k+'='+v for k,v in helper_env.items())+'\n');private.chmod(0o600)
    life.docker('run','--name',life.run_id+'-identity','--label',f'{life.label}={life.run_id}',
        '--label',life.label+'.role=oneshot','--user',str(os.getuid())+':'+str(os.getgid()),'--cpus','.5','--memory','1g','--network','container:'+life.run_id+'-postgres',
        '--env-file',str(private),'--mount',f'type=bind,source={life.checkout},target=/workspace',
        '--mount',f'type=bind,source={life.public_modcache},target=/dependencies,readonly',
        '--mount',f'type=bind,source={life.work},target=/private','-w','/workspace/system/backend','golang:1.24',
        'go','run','./cmd/online-test-fixture','--suite',suite,'--output','/private/identity.env',timeout=300)
    # The helper emits shell-quoted values; source only that private owned file.
    text=life.command(['bash','-c','set -a; source "$1"; env -0','_',str(identity_env)],env=life.env)
    env.update({k:v for item in text.split('\0') if '=' in item for k,v in [item.split('=',1)] if k.startswith(prefix)})


def main():
    require(len(sys.argv)==1,'local_entry_has_no_capacity_or_hosted_options')
    remaining=int(os.environ.get('MONITOR_TRACE_LOCAL_REMAINING_SECONDS',str(MAX_SECONDS)))
    require(0<remaining<=MAX_SECONDS,'remaining_budget_cannot_exceed_sixty_minutes')
    life=Lifecycle()
    life.started-=MAX_SECONDS-remaining
    report={'schema':'addp.execution-trace-local/v1','run_id':life.run_id,'result':'failed',
                            'formal_runner_t4':False,'hosted':False,'capacity':False}
    destination=os.environ.get('MONITOR_TRACE_LOCAL_REPORT')
    target=Path(tempfile.gettempdir())/(life.run_id+'-report.json')
    try:
      if destination:
        candidate=Path(destination)
        require(candidate.is_absolute() and not candidate.is_symlink(), 'external_report_required')
        candidate=candidate.resolve()
        require(not candidate.is_relative_to(ROOT) and not candidate.is_relative_to(life.work) and not candidate.exists(), 'external_report_required')
        target=candidate
    except Exception:
        shutil.rmtree(life.work)
        raise
    try:
        endpoint=effective_docker_endpoint(os.environ,lambda name: json.loads(life.docker('context','inspect',*([name] if name else [])))[0]['Endpoints']['docker']['Host'])
        private_docker_client(life,endpoint)
        validate_profile(os.environ,platform.machine(),life.docker('info','--format','{{.Architecture}}').strip())
        commit,env,images=build(life)
        life.source_commit=commit
        report.update(source_commit=commit,images=images,input_sha256=life.input_hashes)
        config=setup(life,env,images)
        report['module_commits']=life.module_commits
        identity(life,env)
        # Shared HTTP helpers must never send local Bearer credentials through
        # a shell-configured public proxy. This process owns only this fixture.
        os.environ['NO_PROXY']=os.environ['no_proxy']='127.0.0.1,localhost'
        business=module('monitor-traces-local-online',life.checkout)
        browser=business.run(life,env,report)
        life.phase='real_console_browser'
        browser_path=life.private_json('browser.json',browser)
        secret_env=life.work/'browser.env'
        selected={k:v for k,v in env.items() if k.startswith('ADDP_EXECUTION_TRACE_ADMIN_')}
        selected.update(ADDP_EXECUTION_TRACE_LOCAL='1',CONSOLE_URL='http://nginx',
                        ADDP_EXECUTION_TRACE_BROWSER_CONFIG_FILE='/private/browser.json',
                        ADDP_EXECUTION_TRACE_BROWSER_REPORT_FILE='/private/browser-report.json',
                        ADDP_ONLINE_ARTIFACT_DIR='/tmp/trace-browser')
        secret_env.write_text('\n'.join(k+'='+v for k,v in selected.items())+'\n');secret_env.chmod(0o600)
        version=json.loads((life.checkout/'console/frontend/package-lock.json').read_text())['packages']['node_modules/@playwright/test']['version']
        browser_image='mcr.microsoft.com/playwright:v'+version+'-noble'
        life.docker('run','--name',life.run_id+'-browser','--label',f'{LABEL}={life.run_id}',
            '--label',LABEL+'.role=oneshot','--cpus','.5','--memory','1g','--network',life.run_id+'_data','--env-file',str(secret_env),
            '--mount',f'type=bind,source={life.checkout},target=/workspace',
            '--mount',f'type=bind,source={life.work},target=/private','-w','/workspace/console/frontend',browser_image,
            'node','node_modules/@playwright/test/cli.js','test','--config=playwright.online.config.js','execution-trace-local.spec.js','--reporter=line',timeout=300)
        report['browser']=json.loads((life.work/'browser-report.json').read_text())
        require(report['browser'].get('result')=='passed','browser_report_not_passed')
        business.finish(life,env,report)
        life.sample()
        require(life.failure is None,life.failure or 'resource_guard_failed')
        report['result']='passed'
    except BaseException as error:
        report.update(stage=life.phase,error_code=str(error) if (isinstance(error,GateError) or type(error).__name__=='LocalTraceAssertionFailure') and re.fullmatch('[a-z0-9_]+',str(error)) else type(error).__name__)
        if hasattr(life,'last_failure'):report['command_failure']=life.last_failure
        if hasattr(life,'api_failure'):report['api_failure']=life.api_failure
        diagnostics=[]
        for state in getattr(life,'failed_container_states',[]):
            remaining=MAX_SECONDS-(time.monotonic()-life.started)
            if remaining<=0:break
            if state.get('service') in TRACE+BACKENDS+FRONTENDS and state['id'] not in life.builder_ids and state.get('status') in ('exited','dead') and state.get('exit_code'):
                try:
                    raw=life.docker('logs','--tail','20',state['id'],guarded=False,timeout=min(1,remaining)).encode()
                    item={'service':state['service'],'output_sha256':hashlib.sha256(raw).hexdigest(),'markers':diagnostic_markers(raw)}
                    diagnostics.append(item)
                    inspected=json.loads(life.docker('inspect',state['id'],guarded=False,timeout=1))[0]
                    item['runtime_user']=inspected['Config'].get('User','')
                    item['data_mounts']=[]
                    for mount in inspected.get('Mounts',[]):
                        if mount['Type']=='volume' and mount.get('Name') in life.sampler_volumes:
                            index=life.sampler_volumes.index(mount['Name'])
                            if mount['Destination']=='/var/log/addp':
                                module_name=state['service'].removesuffix('-backend')
                                captured=life.docker('exec',life.run_id+'-space','sh','-ec',
                                    'find /space/'+str(index)+'/'+module_name+' -type f -name "*.jsonl" -exec tail -n 8 {} + | tail -c 65536',
                                    guarded=False,timeout=1).encode()
                                item['runtime_log_diagnostics']={'output_sha256':hashlib.sha256(captured).hexdigest(),'markers':diagnostic_markers(captured)}
                            value=life.docker('exec',life.run_id+'-space','stat','-c','%u %g %a','/space/'+str(index),guarded=False,timeout=1).strip()
                            if re.fullmatch(r'[0-9]+ [0-9]+ [0-7]+',value):
                                uid,gid,mode=value.split();item['data_mounts'].append({'target':mount['Destination'],'uid':int(uid),'gid':int(gid),'mode':mode,'writable':mount.get('RW')})
                except GateError:pass
        if diagnostics:report['failed_service_diagnostics']=diagnostics
    finally:
        if hasattr(life,'cache_reclaim_receipt'):report['go_cache_reclaim']=life.cache_reclaim_receipt
        report['resource_samples']=life.samples
        if hasattr(life,'source_commit'):report['source_commit']=life.source_commit
        if hasattr(life,'minio_provenance'):report['minio_product']=life.minio_provenance
        if hasattr(life,'resource_failure'):report['resource_guard_failure']=life.resource_failure
        if hasattr(life,'failed_container_states'):report['failed_container_states']=life.failed_container_states
        if hasattr(life,'builder_stop_states'):report['builder_stop_states']=life.builder_stop_states
        report['dependencies_ready']=getattr(life,'dependencies_ready',False)
        if hasattr(life,'nginx_route_assertions'):report['nginx_routes']=life.nginx_route_assertions
        errors=life.cleanup()
        report['owned_list_commands']=life.command_evidence.snapshot()
        report['cleanup']={'zero_owned_resources':not errors,'failures':errors}
        if errors:report['result']='failed'
        target.parent.mkdir(parents=True,exist_ok=True)
        with open(target,'x',opener=lambda path,flags:os.open(path,flags,0o600)) as f:json.dump(report,f,indent=2)
    print('Local execution Trace: '+report['result']+'; report='+str(target),flush=True)
    return 0 if report['result']=='passed' else 1


def interrupted(signum, frame):
    raise GateError('local_acceptance_interrupted')


if __name__=='__main__':
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    raise SystemExit(main())
