"""Real scrape, admission, mTLS, persistence and owned outage checks."""
import json
import hashlib
import ipaddress
import datetime
import math
import os
import re
import ssl
import socket
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

WORK = Path(os.environ['METRICS_T2_WORK'])
TLS = Path(os.environ['ADDP_METRICS_TLS_DIR'])
PROJECT = os.environ['METRICS_T2_PROJECT']
FILE = os.environ['METRICS_T2_COMPOSE']
FILES = [FILE]


def compose(*args, timeout=None, stderr=None):
    flags = [flag for path in FILES for flag in ('-f', path)]
    return subprocess.check_output(['docker', 'compose', '--env-file', '/dev/null', '-p', PROJECT,
                                    *flags, *args], text=True, timeout=timeout, stderr=stderr).strip()


def port(service, internal):
    return compose('port', service, str(internal)).rsplit(':', 1)[1]


state = json.loads(subprocess.check_output(['docker', 'inspect', compose('ps', '-q', 'prometheus')], text=True))[0]
engine = json.loads(subprocess.check_output(['docker', 'info', '--format', '{{json .}}'], text=True))
desktop_transport = __import__('platform').system() == 'Darwin' and engine['OperatingSystem'] == 'Docker Desktop'
process_loopback_ports = []
assert state['Config']['User'] == '65534:65534'
assert state['HostConfig']['ReadonlyRootfs']
assert state['HostConfig']['CapDrop'] == ['ALL']
assert not state['HostConfig']['Privileged']
assert state['HostConfig']['NanoCpus'] == 1_000_000_000
assert state['HostConfig']['Memory'] == 2 * 1024**3

context = ssl.create_default_context(cafile=str(TLS / 'ca.crt'))
context.load_cert_chain(str(TLS / 'health.crt'), str(TLS / 'health.key'))
base = 'https://localhost:' + port('prometheus', 9090)
source = 'https://localhost:' + port('metrics-source', 9443)
control = 'https://localhost:' + port('metrics-control', 9444)
deployment = Path(os.environ['ADDP_METRICS_DEPLOYMENT_DIR'])
original_metrics = (WORK / 'source/metrics').read_text()
source_context = ssl.create_default_context(cafile=str(deployment / 'source-ca.crt'))
source_context.load_cert_chain(str(deployment / 'collector.crt'), str(deployment / 'collector.key'))


def get(url, ctx=context):
    with urllib.request.urlopen(url, context=ctx, timeout=5) as response:
        return response.read()


def api(path):
    data = json.loads(get(base + '/api/v1/' + path))
    assert data['status'] == 'success', data
    return data['data']


def query(expression, *, at=None):
    params = {'query': expression}
    if at is not None:
        params['time'] = at
    return api('query?' + urllib.parse.urlencode(params))['result']


def job_is_up(job, expected):
    rows = query('up{job="' + job + '"}')
    return len(rows) == 1 and rows[0]['value'][1] == expected


def self_sample_ready():
    rows = query('process_resident_memory_bytes{job="prometheus"}')
    return (job_is_up('prometheus', '1') and len(rows) == 1
            and float(rows[0]['value'][1]) > 0)


def eventually(check, label, seconds=65):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            if check():
                print('Metrics T2: ' + label, flush=True)
                return
        except (urllib.error.URLError, TimeoutError, ConnectionError):
            pass
        time.sleep(1)
    raise AssertionError('Timed out: ' + label)


def rejected(url, ctx):
    try:
        get(url, ctx)
    except (urllib.error.URLError, ssl.SSLError, ConnectionError):
        return
    raise AssertionError('Unauthenticated access accepted: ' + url)


def tls_failure_kind(error, verify_code=None):
    """Classify without returning any part of an endpoint or peer message."""
    text=str(error).lower()
    if verify_code==9:return 'certificate_not_yet_valid'
    if verify_code==10:return 'certificate_expired'
    if verify_code in (62,64) or any(word in text for word in ('hostname mismatch','ip address mismatch','certificate is valid for','ip sans','cannot validate certificate for')):
        return 'certificate_name_mismatch'
    if any(word in text for word in ('unknown authority','local issuer certificate','self-signed certificate','self signed certificate')):
        return 'certificate_untrusted'
    if 'expired or is not yet valid' in text:return 'certificate_time_invalid'
    if 'expired' in text:return 'certificate_expired'
    if 'not yet valid' in text:return 'certificate_not_yet_valid'
    if 'certificate required' in text or "didn't provide a certificate" in text:return 'peer_certificate_required'
    if 'bad certificate' in text or 'certificate unknown' in text:return 'peer_certificate_rejected'
    if any(word in text for word in ('certificate','tls','ssl')):return 'tls_unknown'
    if 'timeout' in text or 'deadline' in text or isinstance(error,TimeoutError):return 'timeout'
    if 'connection' in text or isinstance(error,ConnectionError):return 'connection'
    code=re.search(r'\b([45][0-9]{2})\b',text)
    return 'http_'+code[1] if code else 'other' if text else 'none'


def source_certificate_readiness(context):
    """Two distinct verified handshakes, each with one absolute 5s budget."""
    assert context.check_hostname and context.verify_mode==ssl.CERT_REQUIRED, 'source_tls_verification_required'
    publication=json.loads((WORK/'source-tls-publication.json').read_text())
    assert set(publication)=={'atomic_identity_changed','same_second_mtime','prior_leaf_sha256','expected_leaf_sha256'}, 'source_tls_publication_invalid'
    assert type(publication['same_second_mtime']) is bool and all(re.fullmatch('[0-9a-f]{64}',publication[key]) for key in ('prior_leaf_sha256','expected_leaf_sha256')), 'source_tls_publication_invalid'
    expected=hashlib.sha256(ssl.PEM_cert_to_DER_cert((WORK/'source-server-final.crt').read_text())).hexdigest()
    assert publication['atomic_identity_changed'] is True and publication['expected_leaf_sha256']==expected, 'source_tls_publication_invalid'
    groups=json.loads((WORK/'source/discovery.json').read_text())
    address=groups[0]['targets'][0]
    source_ip=str(ipaddress.IPv4Address(address.rsplit(':',1)[0]))
    assert address==source_ip+':9443', 'source_tls_discovery_invalid'
    published_port=int(port('metrics-source',9443))
    proof={'publication':publication,'checks':[],'passed':False}
    def publish():
        pending=WORK/'source-tls-proof.pending'
        pending.write_text(json.dumps(proof,sort_keys=True));pending.replace(WORK/'source-tls-proof.json')
    for stage,name in (('localhost_leaf','localhost'),('source_ip',source_ip)):
        began=time.monotonic();deadline=began+5
        result={'stage':stage,'status':'failed','error_kind':'other'}
        try:
            with socket.create_connection(('127.0.0.1',published_port),timeout=5) as raw:
                remaining=deadline-time.monotonic()
                if remaining<=0:raise TimeoutError()
                raw.settimeout(remaining)
                with context.wrap_socket(raw,server_hostname=name) as secured:
                    leaf=secured.getpeercert(binary_form=True)
            if time.monotonic()>=deadline:raise TimeoutError()
            result['leaf_sha256']=hashlib.sha256(leaf).hexdigest()
            if result['leaf_sha256']!=expected:
                result['error_kind']='certificate_fingerprint_mismatch'
            else:
                result.update(status='passed',error_kind='none')
        except Exception as error:
            verify_code=getattr(error,'verify_code',None)
            result['error_kind']=tls_failure_kind(error,verify_code)
            if type(verify_code) is int and 0<=verify_code<=1000:result['verify_code']=verify_code
        result['duration_seconds']=round(time.monotonic()-began,6)
        proof['checks'].append(result);publish()
        print('Metrics T2: source TLS readiness '+json.dumps(result,sort_keys=True),flush=True)
        if result['status']!='passed':raise AssertionError('source_tls_readiness_failed')
    proof['passed']=True;publish()


def finish_public_preflight():
    if os.environ.get('ADDP_METRICS_T2_CASE')!='mtls-preflight':return
    assert not compose('ps','-aq','python-http-agent','python-http-copilot'), 'preflight_started_python_source'
    assert not list((WORK/'source').glob('process-*.json.requests')), 'preflight_started_go_business_source'
    print('Metrics T2: public mTLS preflight passed; zero business requests; stopping before source stages',flush=True)
    raise SystemExit(0)


# Production TLS forwarder uses the same hard resource/network boundary in T2.
forwarder = json.loads(subprocess.check_output(['docker', 'inspect', compose('ps', '-q', 'metrics-control')], text=True))[0]
assert forwarder['Config']['User'] == '65534:65534'
assert forwarder['HostConfig']['ReadonlyRootfs'] and forwarder['HostConfig']['CapDrop'] == ['ALL']
assert not forwarder['HostConfig']['Privileged']
assert forwarder['HostConfig']['NanoCpus'] == 250_000_000
assert forwarder['HostConfig']['Memory'] == 128 * 1024**2
assert all(not m['RW'] for m in forwarder['Mounts'])
control_context = ssl.create_default_context(cafile=str(deployment / 'control-ca.crt'))
for path, method, expected in (('/metrics', 'GET', 404),
                               ('/api/v1/system/oauth/token', 'GET', 405),
                               ('/api/v1/monitor/platform/metrics_discovery', 'POST', 405),
                               ('/api/v1/monitor/platform/metrics_discovery?x=1', 'GET', 400),
                               ('/api/v1/monitor/platform/metrics_discovery', 'GET', 401)):
    request = urllib.request.Request(control + path, method=method)
    try:
        urllib.request.urlopen(request, context=control_context, timeout=5)
    except urllib.error.HTTPError as error:
        assert error.code == expected, (path, error.code)
    else:
        raise AssertionError('Control forwarder accepted forbidden request')
print('Metrics T2: private TLS forwarding rejects paths, methods, queries and anonymous discovery', flush=True)

source_certificate_readiness(source_context)
assert get(base + '/-/ready')
assert get(source + '/metrics', source_context)
anonymous = ssl.create_default_context(cafile=str(TLS / 'ca.crt'))
wrong = ssl.create_default_context(cafile=str(TLS / 'ca.crt'))
wrong.load_cert_chain(str(WORK / 'untrusted.crt'), str(WORK / 'untrusted.key'))
for url in (base + '/-/ready', base + '/api/v1/query?query=up'):
    rejected(url, anonymous)
    rejected(url, wrong)
rejected(base.replace('https:', 'http:') + '/-/ready', anonymous)
source_anonymous = ssl.create_default_context(cafile=str(deployment / 'source-ca.crt'))
source_health = ssl.create_default_context(cafile=str(deployment / 'source-ca.crt'))
source_health.load_cert_chain(str(TLS / 'health.crt'), str(TLS / 'health.key'))
rejected(source + '/metrics', source_anonymous)
rejected(source + '/metrics', source_health)
print('Metrics T2: anonymous, foreign client CA and plaintext rejected', flush=True)
# Evaluate fixed-counter semantics first, before the longer discovery/outage cycle.
def check_resource_windows():
    backend = Path(__file__).resolve().parents[2] / 'monitor/backend'
    binary = WORK / 'resource-windows.test'
    environment = dict(os.environ, GOWORK='off')
    selection = '^TestIntegrationMetrics(CPUWindow|ProcessWindow|HTTPWindow|DiskWindow|DiskIOWindow|NetworkWindow|Filesystem|FilesystemInodes)$'
    print('Metrics window T2: build test binary (preparation limit 180s)', flush=True)
    subprocess.run(['go', 'test', '-c', './internal/resourcequery', '-o', str(binary)],
                   cwd=backend, env=environment, check=True, timeout=180)
    print('Metrics window T2: execute prepared checks (Go/process limit 60s)', flush=True)
    subprocess.run([str(binary), '-test.run=' + selection,
                    '-test.count=1', '-test.timeout=60s', '-test.v'],
                   cwd=backend / 'internal/resourcequery',
                   env=dict(environment, ADDP_METRICS_QUERY_INTEGRATION='1'),
                   check=True, timeout=60)


check_resource_windows()

flags = api('status/flags')
assert flags['storage.tsdb.retention.time'] == '1w', flags
assert flags['storage.tsdb.retention.size'] == '10GiB', flags
assert flags['query.timeout'] == '5s', flags
assert flags['query.max-concurrency'] == '8', flags
assert flags['web.enable-admin-api'] == 'false', flags
assert flags['web.enable-lifecycle'] == 'false', flags
assert flags['web.enable-remote-write-receiver'] == 'false', flags
config = api('status/config')['yaml']
for value in ('scrape_interval: 15s', 'scrape_timeout: 5s', 'sample_limit: 20000', 'body_size_limit: 10MiB',
              'label_limit: 20', 'label_name_length_limit: 128', 'label_value_length_limit: 512',
              'refresh_interval: 30s', 'target_limit: 9', 'client_id: addp-prometheus',
              'context_type: platform', 'audience: addp.api', 'follow_redirects: false'):
    assert value in config, (value, config)


def valid_sample():
    data = query('addp_fixture_resource_bytes{job="addp_nodes"}')
    if len(data) != 1 or data[0]['value'][1] != '4096':
        return False
    labels = data[0]['metric']
    expected = {'addp_node_id': '11111111-1111-4111-8111-111111111111',
                'addp_monitor_kind': 'host_resources', 'addp_source': 'node_exporter'}
    return (labels.get('exported_job') == 'spoof'
            and all(labels.get(key) == value for key, value in expected.items())
            and not any((re.fullmatch(r'(exported_)*addp_.*', key) and key not in expected)
                        or key.startswith(('__meta_', '__tmp_')) for key in labels))


def fixture_file_diagnostic(name):
    # Closed test-owned names; no endpoints, labels, contents or stderr escape.
    if name not in ('metrics', 'discovery.json'):
        raise ValueError('unsupported_fixture_file')
    host = {'state': 'read_error', 'read_bytes': None, 'prefix_sha256': None}
    try:
        with (WORK / 'source' / name).open('rb') as stream:
            value = stream.read(65537)
        host.update(state='oversized' if len(value) > 65536 else 'readable', read_bytes=len(value),
                    prefix_sha256=hashlib.sha256(value).hexdigest())
    except FileNotFoundError:
        host['state'] = 'missing'
    except PermissionError:
        host['state'] = 'denied'
    except OSError:
        pass
    source = {'state': 'unavailable', 'regular_file_visible': None, 'readable': None}
    script = 'path="/source/$1"; if [ -f "$path" ]; then printf "1 "; else printf "0 "; fi; if [ -r "$path" ]; then printf "1"; else printf "0"; fi'
    try:
        raw = compose('exec', '-T', 'metrics-source', 'sh', '-ec', script, '_', name,
                      timeout=5, stderr=subprocess.PIPE)
        if raw in ('0 0', '0 1', '1 0', '1 1'):
            source.update(state='observed', regular_file_visible=raw[0] == '1', readable=raw[2] == '1')
    except subprocess.TimeoutExpired:
        source['state'] = 'timeout'
    except (subprocess.CalledProcessError, OSError):
        pass
    print('Metrics T2: owned fixture file ' + json.dumps({'file': name, 'host': host, 'source': source}, sort_keys=True), flush=True)


def fixture_scrape_diagnostic():
    # Fixed shapes only: do not log target URLs, OAuth configuration or raw
    # scrape errors (which can contain endpoints or credentials).
    rows = query('addp_fixture_resource_bytes{job="addp_nodes"}')
    diagnostic = {'fixture_series_count': len(rows), 'fixture_value_matches':
                  len(rows) == 1 and rows[0]['value'][1] == '4096', 'targets': []}
    for target in api('targets?state=active')['activeTargets']:
        if target.get('labels', {}).get('job') != 'addp_nodes':
            continue
        error = target.get('lastError', '').lower()
        kind = tls_failure_kind(error)
        diagnostic['targets'].append({'health': target.get('health') if target.get('health') in
                                      ('up', 'down', 'unknown') else 'unknown', 'error_kind': kind})
    failures = query('prometheus_sd_http_failures_total')
    diagnostic['http_sd_failure_observed'] = any(float(row['value'][1]) > 0 for row in failures)
    print('Metrics T2: initial scrape diagnostic ' + json.dumps(diagnostic, sort_keys=True), flush=True)


try:
    eventually(valid_sample, 'real mTLS scrape and controlled job label')
except AssertionError:
    fixture_scrape_diagnostic()
    raise
# Scrape jobs have independent initial offsets; fixture readiness is not self readiness.
eventually(self_sample_ready, 'center self scrape and resident memory')
assert job_is_up('addp_nodes', '1')
finish_public_preflight()
initial_sample_time = query('addp_fixture_resource_bytes{job="addp_nodes"}')[0]['value'][0]
# Write atomically without replacing the bind-mounted source directory.
def source_text(text):
    pending = WORK / 'source/pending'
    pending.write_text(text)
    pending.replace(WORK / 'source/metrics')


def exceeded():
    targets = api('targets')['activeTargets']
    item = next(t for t in targets if t['labels']['job'] == 'addp_nodes')
    return item['health'] == 'down' and 'sample limit' in item['lastError'].lower()

source_text(''.join(f'addp_fixture_overflow{{index="{i}"}} {i}\n' for i in range(20001)))
try:
    eventually(exceeded, 'sample overflow is a visible scrape failure')
except AssertionError:
    fixture_file_diagnostic('metrics')
    try:
        targets = api('targets')['activeTargets']
        print('Metrics T2: overflow diagnostic ' + json.dumps({
            'targets': [{'health': item.get('health'),
                         'sample_limit_error': 'sample limit' in item.get('lastError', '').lower(),
                         'error_kind': tls_failure_kind(item.get('lastError', ''))}
                        for item in targets if item.get('labels', {}).get('job') == 'addp_nodes'],
        }, sort_keys=True), flush=True)
    except Exception:
        print('Metrics T2: overflow target diagnostic unavailable', flush=True)
    raise
assert query('addp_fixture_overflow') == [], 'overflow must not admit a partial sample set'
source_text(original_metrics)
try:
    eventually(lambda: job_is_up('addp_nodes', '1'), 'budget recovery')
except AssertionError:
    fixture_file_diagnostic('metrics')
    raise

compose('stop', 'metrics-source')
eventually(lambda: job_is_up('addp_nodes', '0'), 'source outage remains a collection failure')
assert get(base + '/-/ready')
compose('start', 'metrics-source')
eventually(valid_sample, 'source recovery')

# SD error must preserve the previous targets; only a successful empty array removes them.
original_discovery = (WORK / 'source/discovery.json').read_text()
def discovery_text(text):
    pending = WORK / 'source/discovery.pending'
    pending.write_text(text)
    pending.replace(WORK / 'source/discovery.json')


def native_process_cpu_history_ready(records):
    # Fixture readiness only: preserve genuine zero counters and require both
    # four recent samples and a minute-old boundary for each exact live source.
    # The Go test still applies every production identity/freshness/reset guard.
    cpu = 'process_cpu_seconds_total{job="addp_nodes",addp_monitor_kind="process_resources"}'
    rows = query(cpu + ' and (count_over_time(' + cpu + '[1m])>=4) and (' + cpu + ' offset 1m)', at=math.floor(time.time()))
    return (len(rows) == len(records) == 3 and
            {(row['metric']['addp_instance_id'], row['metric']['instance']) for row in rows} ==
            {(record['instance_id'], record['instance']) for record in records})


def native_process_history_diagnostics(records):
    diagnostic = {'host_unix': time.time(), 'center_unix': [float(row['value'][1]) for row in query('vector(time())')], 'processes': []}
    for record in records:
        selector = '{job="addp_nodes",addp_monitor_kind="process_resources",addp_instance_id='+json.dumps(record['instance_id'])+',instance='+json.dumps(record['instance'])+'}'
        cpu = 'process_cpu_seconds_total'+selector
        signals = {'up_count': 'count(up'+selector+')',
                   'up': 'max(up'+selector+')',
                   'identity_count': 'count(addp_process_identity_info'+selector+')',
                   'cpu_count': 'count('+cpu+')',
                   'cpu_window_samples': 'min(count_over_time('+cpu+'[1m]))',
                   'cpu_resets': 'max(resets('+cpu+'[1m]))',
                   'cpu_rate': 'max(rate('+cpu+'[1m]))',
                   'cpu_delta': 'max('+cpu+')-max('+cpu+' offset 1m)',
                   'cpu_age': 'time()-max(timestamp('+cpu+'))',
                   'cpu_previous_age': 'time()-max(timestamp('+cpu+' offset 1m))',
                   'start_difference': 'max(process_start_time_seconds'+selector+')-'+str(datetime.datetime.fromisoformat(record['started_at'].replace('Z', '+00:00')).timestamp())}
        # One bounded request per source. Only fixed signal names and numbers
        # survive; no endpoint, arbitrary metric label or credential is logged.
        rows = query(' or '.join('label_replace(('+expression+'),"addp_probe",'+json.dumps(key)+',"","")' for key, expression in signals.items()), at=math.floor(time.time()))
        diagnostic['processes'].append({key: [float(row['value'][1]) for row in rows if row['metric'].get('addp_probe') == key] for key in signals})
    return diagnostic


def cgroup_source_diagnostics(records, observations, expected):
    metrics = tuple(observations)
    sources = []
    for index, record in enumerate(records):
        row = {'ordinal': index + 1, 'declaration_present': record.get('cgroup') is not None,
               'sdk': {key: value for key, value in record.get('cgroup_diagnostic', {}).items()
                       if key in ('self_sample_valid', 'root_readable', 'cpu_stat_readable',
                                  'core_sched_field_present', 'root_usage_parse_valid') and type(value) is bool},
               'prometheus': {}}
        missing = False
        for metric, samples in observations.items():
            selected = [sample for sample in samples if
                        (sample['metric'].get('addp_instance_id'), sample['metric'].get('instance')) ==
                        (record['instance_id'], record['instance'])]
            row['prometheus'][metric] = {'count': len(selected), 'kernel_identity_matches':
                len(selected) == 1 and all(selected[0]['metric'].get(key) == value for key, value in expected.items())}
            missing = missing or len(selected) != 1
        if missing:
            # One bounded mTLS read per missing source distinguishes SDK output
            # from SD/scrape ingestion. Neither URLs nor raw bodies are logged.
            try:
                with urllib.request.urlopen('https://' + record['instance'] + '/metrics',
                                            context=source_context, timeout=5) as response:
                    body = response.read((256 << 10) + 1)
                if len(body) > 256 << 10:
                    row['direct_source'] = {'state': 'oversize'}
                else:
                    text = body.decode('utf-8')
                    row['direct_source'] = {'state': 'read', 'counts': {
                        metric: len(re.findall(r'^' + re.escape(metric) + r'(?:\{[^\n]*\})? ', text, re.M))
                        for metric in metrics}}
            except (OSError, UnicodeError, urllib.error.URLError):
                row['direct_source'] = {'state': 'unavailable'}
        sources.append(row)
    print('Metrics T2: cgroup source diagnostic ' + json.dumps({'sources': sources}, sort_keys=True), flush=True)


def check_self_cgroup_samples(records):
    # Independent kernel reads from the owned shared source container. Three
    # SDK processes intentionally share this boundary: none proves a dedicated
    # container or a quota allocation merely by exposing these raw samples.
    boot, group_id, namespace = compose('exec', '-T', 'metrics-source', 'sh', '-c',
        'cat /proc/sys/kernel/random/boot_id; stat -c %i /sys/fs/cgroup; readlink /proc/self/ns/pid').strip().splitlines()
    assert namespace.startswith('pid:[') and namespace.endswith(']')
    expected = {'kernel_boot_id': boot, 'cgroup_id': group_id, 'pid_namespace_id': namespace[5:-1]}
    assert re.fullmatch(r'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}', boot)
    assert all(re.fullmatch(r'[1-9][0-9]*', expected[key]) for key in ('cgroup_id', 'pid_namespace_id'))
    selector = '{job="addp_nodes",addp_monitor_kind="process_resources"}'
    observations = {metric: query(metric + selector) for metric in (
        'addp_cgroup_identity_info', 'addp_cgroup_cpu_usage_seconds_total', 'addp_cgroup_memory_current_bytes')}
    cgroup_source_diagnostics(records, observations, expected)
    for metric, rows in observations.items():
        assert len(rows) == len(records) == 3, 'all current SDK sources must expose their genuine cgroup sample'
        assert {(row['metric']['addp_instance_id'], row['metric']['instance']) for row in rows} == {(record['instance_id'], record['instance']) for record in records}
        for row in rows:
            assert {key: row['metric'].get(key) for key in expected} == expected, 'cgroup evidence must match kernel namespace and inode'
            value = float(row['value'][1])
            assert math.isfinite(value) and value >= 0
            if metric == 'addp_cgroup_identity_info':
                assert value == 1
            elif metric == 'addp_cgroup_memory_current_bytes':
                assert value.is_integer() and value > 0
    print('Metrics T2: three SDK sources match independently read shared cgroup identity and raw CPU/memory; no container association or quota inferred', flush=True)


def check_native_process_sources(*, process_alerts=False, cgroup_only=False):
    global base
    check_cgroup = cgroup_only or os.environ.get('ADDP_METRICS_T2_CASE') == 'all' and not process_alerts
    alert_binary = WORK / 'process-alert-lifecycle.test'
    if process_alerts:
        # Prepare before starting the finite-lived native sources. Compilation
        # cannot consume their collection or actual lifecycle-test deadline.
        print('Metrics process alerts T2: prepare lifecycle test', flush=True)
        subprocess.run(['go', 'test', '-c', './internal/service', '-o', str(alert_binary)],
                       cwd=Path(__file__).resolve().parents[2] / 'monitor/backend',
                       env=dict(os.environ, GOWORK='off'), check=True, timeout=180)
    # One Backend and two same-module Worker SDK processes in the owned non-root source
    # container. Loopback-only test ports also exercise Desktop's host transport;
    # there is no business process, workspace mount or extra service.
    source_id = compose('ps', '-q', 'metrics-source').strip()
    source_inspect = json.loads(subprocess.check_output(['docker', 'inspect', source_id], text=True))[0]
    assert source_inspect['HostConfig']['NanoCpus'] == 250_000_000
    assert source_inspect['HostConfig']['Memory'] == 128 * 1024**2
    assert source_inspect['HostConfig']['CgroupnsMode'] == 'private'
    if check_cgroup:
        kernel_tests = compose('exec', '-T', 'metrics-source', '/source/process-source.test',
                           '-test.run=^Test(CgroupMountCoordinatesNeverEscapeVisibleSubtree|CgroupUsageAcceptsKernelCoreSchedulingAccounting)$', '-test.v')
        assert '--- PASS: TestCgroupMountCoordinatesNeverEscapeVisibleSubtree' in kernel_tests
        assert '--- PASS: TestCgroupUsageAcceptsKernelCoreSchedulingAccounting' in kernel_tests
        assert '--- SKIP:' not in kernel_tests and '[no tests to run]' not in kernel_tests
        print(kernel_tests, flush=True)
    source_ip = next(iter(source_inspect['NetworkSettings']['Networks'].values()))['IPAddress']
    processes, records = [], []
    try:
        for index in (1, 2, 3):
            path = WORK / 'source' / f'process-{index}.json'
            for suffix in ('.stop', '.resource-mode'):
                Path(str(path) + suffix).unlink(missing_ok=True)
            path.write_text('')
            path.chmod(0o666)
            requests_path=Path(str(path)+'.requests')
            requests_path.write_text('0');requests_path.chmod(0o666)
            if index == 1:
                traffic_path=Path(str(path)+'.http-load.json')
                traffic_path.write_text('');traffic_path.chmod(0o666)
            address = source_ip + ':' + str(19440 + index)
            envs = {'ADDP_PROCESS_METRICS_T2_SOURCE': '1', 'ADDP_PROCESS_T2_INSTANCE': f'native-{index}',
                    'ADDP_PROCESS_T2_LISTEN': address, 'ADDP_PROCESS_T2_ENDPOINT': f'https://{address}/metrics',
                    'ADDP_PROCESS_T2_RECORD': '/source/' + path.name, 'ADDP_PROCESS_T2_ID': str(index), 'ADDP_PROCESS_T2_ROLE': 'backend' if index == 1 else 'worker'}
            if process_alerts and index == 3:
                envs['ADDP_PROCESS_T2_RESOURCE_CONTROL'] = '1'
            command = ['docker', 'compose', '--env-file', '/dev/null', '-p', os.environ['METRICS_T2_PROJECT'], '-f', os.environ['METRICS_T2_COMPOSE'], 'exec', '-T']
            for key, value in envs.items(): command.extend(['-e', key + '=' + value])
            command.extend(['metrics-source', '/source/process-source.test', '-test.run=^TestIntegrationMetricsSelfSource$', '-test.timeout=9m'])
            log = (WORK / 'source' / f'process-{index}.log').open('w')
            process = subprocess.Popen(command, stdout=log, stderr=subprocess.STDOUT)
            processes.append((process, log, path))
            eventually(lambda: bool(path.read_text()) or process.poll() is not None, 'native process starts')
            if process.poll() is not None:
                log.flush()
                print(path.with_suffix('.log').read_text()[-4000:], flush=True)
                raise AssertionError('native source failed at startup')
            records.append(json.loads(path.read_text()))
        projection = [{'targets': [source_ip + ':' + str(19440 + index)], 'labels': {
            '__scheme__': 'https', '__metrics_path__': '/metrics', 'addp_monitor_kind': 'process_resources',
            'addp_source': 'application', 'addp_module_name': 'monitor', 'addp_instance_id': f'native-{index}', 'addp_runtime_role': records[index - 1]['role']}} for index in (1, 2, 3)]
        if desktop_transport:
            process_loopback_ports = [port('metrics-source', 19440 + index) for index in (1, 2, 3)]
            os.environ['ADDP_METRICS_DESKTOP_LOOPBACK_PORTS'] = ','.join(process_loopback_ports)
            subprocess.run(['python3', str(Path(__file__).resolve().parents[1] / 'infra/generate-metrics-config.py')], check=True)
            compose('up', '-d', '--force-recreate', '--wait', '--wait-timeout', '90', 'prometheus')
            base = 'https://localhost:' + port('prometheus', 9090)
            for group, published in zip(projection, process_loopback_ports):
                group['targets'] = ['127.0.0.1:' + published]
        discovery_text(json.dumps(projection))
        for record, group in zip(records, projection): record['instance'] = group['targets'][0]
        process_metadata = WORK / 'source' / 'process-records.json'
        process_metadata.write_text(json.dumps(records))
        eventually(lambda: len(query('process_resident_memory_bytes{job="addp_nodes",addp_monitor_kind="process_resources"}')) == 3,
                   'three genuine Linux processes enter the sole SD job')
        if check_cgroup:
            check_self_cgroup_samples(records)
        if cgroup_only:
            env = dict(os.environ, GOWORK='off', ADDP_METRICS_QUERY_INTEGRATION='1', MONITOR_PROMETHEUS_URL=base,
                       MONITOR_PROMETHEUS_CA_FILE=str(TLS / 'ca.crt'), MONITOR_PROMETHEUS_CLIENT_CERT_FILE=str(WORK / 'query-tls/client.crt'),
                       MONITOR_PROMETHEUS_CLIENT_KEY_FILE=str(WORK / 'query-tls/client.key'), ADDP_PROCESS_T2_RECORDS=str(process_metadata))
            completed = subprocess.run(['go', 'test', './internal/resourcequery', '-run', '^TestIntegrationMetricsCgroupSources$', '-count=1', '-v', '-timeout=180s'],
                                      cwd=Path(__file__).resolve().parents[2] / 'monitor/backend', env=env,
                                      stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, timeout=240)
            print(completed.stdout, flush=True)
            completed.check_returncode()
            assert '--- PASS: TestIntegrationMetricsCgroupSources' in completed.stdout
            assert '--- SKIP:' not in completed.stdout and '[no tests to run]' not in completed.stdout
            return
        if desktop_transport:
            def desktop_process_targets():
                active = [t for t in api('targets')['activeTargets'] if t['labels'].get('addp_monitor_kind') == 'process_resources']
                return (len(active) == 3 and all(t['health'] == 'up' and
                        t['scrapeUrl'] == 'https://host.docker.internal:' + t['labels']['instance'].rsplit(':', 1)[1] + '/metrics' and
                        t['labels']['instance'] in {r['instance'] for r in records} and
                        t['labels']['addp_instance_id'] == 'native-' + str(next(r['id'] for r in records if r['instance'] == t['labels']['instance']))
                        for t in active))
            eventually(desktop_process_targets, 'three Desktop SDK endpoints preserve canonical identities through exact host transport')
            print('Metrics T2: three real SDK sources use distinct Desktop loopback ports in the sole Job, preserving instance identities', flush=True)
        env = dict(os.environ, GOWORK='off', ADDP_METRICS_QUERY_INTEGRATION='1', MONITOR_PROMETHEUS_URL=base,
                   MONITOR_PROMETHEUS_CA_FILE=str(TLS / 'ca.crt'), MONITOR_PROMETHEUS_CLIENT_CERT_FILE=str(WORK / 'query-tls/client.crt'),
                   MONITOR_PROMETHEUS_CLIENT_KEY_FILE=str(WORK / 'query-tls/client.key'), ADDP_PROCESS_T2_RECORDS=str(process_metadata))
        if process_alerts:
            # RSS appears on the first scrape; it cannot establish CPU history.
            # Finish fixture readiness in the existing 65s preparation wait,
            # before the unchanged 270s Go lifecycle/300s executable budgets.
            try:
                eventually(lambda: native_process_cpu_history_ready(records),
                           'three exact native sources have complete minute-old CPU history before alert lifecycle')
            except AssertionError:
                try:
                    print('Metrics T2 CPU preparation diagnostics: '+json.dumps(native_process_history_diagnostics(records), sort_keys=True), flush=True)
                except Exception:
                    print('Metrics T2 CPU preparation diagnostics unavailable', flush=True)
                raise
            completed = subprocess.run([str(alert_binary), '-test.run=^TestIntegrationMetricsProcessAlertLifecycle$', '-test.count=1', '-test.timeout=300s', '-test.v'],
                           cwd=Path(__file__).resolve().parents[2] / 'monitor/backend',
                           env=dict(env, ADDP_PROCESS_RULE_LIFECYCLE_INTEGRATION='1', ADDP_POSTGRES_INTEGRATION='1',
                                    ADDP_TEST_POSTGRES_HOST='127.0.0.1', ADDP_TEST_POSTGRES_PORT=port('postgres', 5432),
                                    ADDP_TEST_POSTGRES_DATABASE='addp_test', ADDP_TEST_POSTGRES_USER='addp',
                                    ADDP_TEST_POSTGRES_PASSWORD=os.environ['METRICS_T2_POSTGRES_PASSWORD'],
                                    ADDP_TEST_POSTGRES_SSLMODE='disable'), stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                           text=True, timeout=320)
            print(completed.stdout, flush=True)
            completed.check_returncode()
            assert '--- PASS: TestIntegrationMetricsProcessAlertLifecycle' in completed.stdout
            assert '--- SKIP:' not in completed.stdout and '[no tests to run]' not in completed.stdout
            return
        try:
            subprocess.run([str(WORK/'http-query.test'), '-test.run=^TestIntegrationMetricsProcessSources$', '-test.count=1', '-test.v'],
                           cwd=Path(__file__).resolve().parents[2] / 'monitor/backend', env=env, check=True, timeout=120)
        except subprocess.SubprocessError:
            # Preserve closed signals before owned processes/center are destroyed.
            # No labels, endpoints, request bodies or credentials enter diagnostics.
            try:
                diagnostic = {'host_unix':time.time(), 'center_unix':[row['value'][1] for row in query('vector(time())')], 'processes':[]}
                for record in records:
                    selector = '{job="addp_nodes",addp_monitor_kind="process_resources",addp_instance_id='+json.dumps(record['instance_id'])+',instance='+json.dumps(record['instance'])+'}'
                    cpu = 'process_cpu_seconds_total'+selector
                    signals = {}
                    for key, expression in {'up_count':'count(up'+selector+') or vector(0)',
                                            'up':'max(up'+selector+') or vector(0)',
                                            'sample_age':'time()-max(timestamp(up'+selector+'))',
                                            'identity_count':'count(addp_process_identity_info'+selector+') or vector(0)',
                                        'cpu_count':'count('+cpu+') or vector(0)',
                                        'cpu_window_samples':'min(count_over_time('+cpu+'[1m]))',
                                        'cpu_resets':'max(resets('+cpu+'[1m]))',
                                        'cpu_delta':'max('+cpu+')-max('+cpu+' offset 1m)',
                                        'cpu_age':'time()-max(timestamp('+cpu+'))',
                                        'cpu_previous_age':'time()-max(timestamp('+cpu+' offset 1m))',
                                            'start_difference':'max(process_start_time_seconds'+selector+')-'+str(datetime.datetime.fromisoformat(record['started_at'].replace('Z', '+00:00')).timestamp())}.items():
                        signals[key] = [row['value'][1] for row in query(expression)]
                    diagnostic['processes'].append(signals)
                print('Metrics T2 process query diagnostics: '+json.dumps(diagnostic, sort_keys=True), flush=True)
            except Exception:
                print('Metrics T2 process query diagnostics unavailable', flush=True)
            raise
        subprocess.run([str(WORK/'http-query.test'), '-test.run=^TestIntegrationMetricsHTTPRealSource$', '-test.count=1', '-test.v'],
                       cwd=Path(__file__).resolve().parents[2] / 'monitor/backend', env=env, check=True, timeout=120)
        subprocess.run([str(WORK/'http-alert.test'), '-test.run=^TestIntegrationMetricsHTTPAlertLifecycle$', '-test.count=1', '-test.timeout=300s', '-test.v'],
                       cwd=Path(__file__).resolve().parents[2] / 'monitor/backend',
                       env=dict(env, ADDP_HTTP_RULE_LIFECYCLE_INTEGRATION='1', ADDP_POSTGRES_INTEGRATION='1',
                                ADDP_TEST_POSTGRES_HOST='127.0.0.1', ADDP_TEST_POSTGRES_PORT=port('postgres', 5432),
                                ADDP_TEST_POSTGRES_DATABASE='addp_test', ADDP_TEST_POSTGRES_USER='addp',
                                ADDP_TEST_POSTGRES_PASSWORD=os.environ['METRICS_T2_POSTGRES_PASSWORD'],
                                ADDP_TEST_POSTGRES_SSLMODE='disable'), check=True, timeout=320)
        process, log, path = processes[0]
        Path(str(path) + '.stop').touch()
        assert process.wait(timeout=10) == 0
        # Consumers evaluate at whole seconds. A failed scrape later in the current
        # second must not release them to query the preceding healthy instant.
        eventually(lambda: bool(query('up{job="addp_nodes",addp_instance_id="native-1"} == 0', at=math.floor(time.time()))),
                   'stopped real process becomes a failed source')
        assert bool(query('up{job="addp_nodes",addp_instance_id="native-2"} == 1'))
        assert get('http://127.0.0.1:' + port('metrics-source', 8080) + '/alive') == b'fixture alive\n'
        records[0]['stopped'] = True
        process_metadata.write_text(json.dumps(records))
        env['ADDP_PROCESS_T2_STOPPED'] = '1'
        subprocess.run([str(WORK/'http-query.test'), '-test.run=^TestIntegrationMetricsHTTPRealSource$', '-test.count=1', '-test.v'],
                       cwd=Path(__file__).resolve().parents[2] / 'monitor/backend', env=env, check=True, timeout=30)
        subprocess.run([str(WORK/'http-query.test'), '-test.run=^TestIntegrationMetricsProcessSources$', '-test.count=1', '-test.v'],
                       cwd=Path(__file__).resolve().parents[2] / 'monitor/backend', env=env, check=True, timeout=30)
        process, log, path = processes[1]
        Path(str(path) + '.stop').touch()
        assert process.wait(timeout=10) == 0
        eventually(lambda: bool(query('up{job="addp_nodes",addp_instance_id="native-2"} == 0', at=math.floor(time.time()))),
                   'stopped worker becomes a failed source at the consumer instant')
        assert bool(query('up{job="addp_nodes",addp_instance_id="native-3"} == 1'))
        records[1]['stopped'] = True
        process_metadata.write_text(json.dumps(records))
        subprocess.run([str(WORK/'http-query.test'), '-test.run=^TestIntegrationMetricsProcessSources$', '-test.count=1', '-test.v'],
                       cwd=Path(__file__).resolve().parents[2] / 'monitor/backend', env=env, check=True, timeout=30)
        print('Metrics T2: Backend and two same-module Workers isolated; stopping Backend then one Worker clears only those sources', flush=True)
    finally:
        for process, log, path in processes:
            Path(str(path) + '.resource-mode').unlink(missing_ok=True)
            Path(str(path) + '.stop').touch()
            try:
                result = process.wait(timeout=10)
                if result != 0: raise RuntimeError('test-owned process source failed')
            finally: log.close()
        discovery_text(original_discovery)
    eventually(valid_sample, 'fixture scope restored after native process checks')


def prepare_http_test_binaries():
    # Compilation cannot consume the finite producers' request budget/history.
    for package,name in (('./internal/resourcequery','http-query.test'),('./internal/service','http-alert.test')):
        subprocess.run(['go','test','-c',package,'-o',str(WORK/name)],cwd=Path(__file__).resolve().parents[2]/'monitor/backend',env=dict(os.environ,GOWORK='off'),check=True,timeout=180)


def check_python_http_sources():
    import runpy
    runpy.run_path(str(Path(__file__).with_name('python-http-metrics-probe.py')))['run'](globals())


def resource_query_environment():
    return dict(os.environ, GOWORK='off', ADDP_METRICS_QUERY_INTEGRATION='1',
                MONITOR_PROMETHEUS_URL=base, MONITOR_PROMETHEUS_CA_FILE=str(TLS / 'ca.crt'),
                MONITOR_PROMETHEUS_CLIENT_CERT_FILE=str(WORK / 'query-tls/client.crt'),
                MONITOR_PROMETHEUS_CLIENT_KEY_FILE=str(WORK / 'query-tls/client.key'))


def check_controlled_host_sources(filesystem_only=False):
    query_env = resource_query_environment()
    # Six controlled source inputs exercise the actual service aggregation through
    # real HTTP SD / mTLS / Prometheus. No real host registrations or User grants.
    overview_ip = json.loads(original_discovery)[0]['targets'][0].rsplit(':', 1)[0]
    overview_groups = []
    values = (100000000,) if filesystem_only else (100000000, 90000000, 80000000, 40000000, 40000000, 10000000)
    for index, available in enumerate(values):
        (WORK / 'source' / f'overview-{index}.prom').write_text(
            'node_uname_info{sysname="Linux"} 1\n'
            'node_exporter_build_info{version="1.12.1"} 1\n'
            'node_cpu_seconds_total{cpu="0",mode="idle"} 0\n'
            'node_cpu_seconds_total{cpu="0",mode="user"} 0\n'
            'node_boot_time_seconds 1\n'
            'node_memory_MemTotal_bytes 100000000\n'
            f'node_memory_MemAvailable_bytes {available}\n')
        overview_groups.append({'targets': [f'{overview_ip}:{19450+index}'], 'labels': {
            'addp_node_id': f'00000000-0000-4000-8000-{index+1:012d}',
            'addp_monitor_kind': 'host_resources', 'addp_source': 'node_exporter',
            '__meta_addp_target_version': '1', '__meta_addp_node_version': '3'}})
    try:
        overview_started = time.time()
        discovery_text(json.dumps(overview_groups))
        expected_ids = {group['labels']['addp_node_id'] for group in overview_groups}
        expected_scopes = {(group['labels']['addp_node_id'], group['targets'][0]) for group in overview_groups}
        # WAL replay can leave a prior removed identity visible within lookback.
        # Readiness concerns this current projection, not all historic Job series.
        overview_selector = ('{job="addp_nodes",addp_monitor_kind="host_resources",addp_source="node_exporter",'
                             'addp_node_id=~"' + '|'.join(sorted(expected_ids)) + '"}')
        def overview_sources_ready():
            active = [row for row in api('targets')['activeTargets'] if row['labels'].get('job') == 'addp_nodes']
            if (len(active) != len(overview_groups) or any(row['health'] != 'up' or row['lastError'] for row in active)
                    or {(row['labels'].get('addp_node_id'), row['labels'].get('instance')) for row in active} != expected_scopes):
                return False
            for metric in ('up', 'node_memory_MemTotal_bytes'):
                rows = query('timestamp(' + metric + overview_selector + ')')
                if (len(rows) != len(overview_groups) or {(row['metric'].get('addp_node_id'), row['metric'].get('instance')) for row in rows} != expected_scopes
                        or any(float(row['value'][1]) < overview_started for row in rows)):
                    return False
            return True
        try:
            eventually(overview_sources_ready, f'{len(overview_groups)} controlled host inputs enter real HTTP SD')
        except AssertionError:
            fixture_file_diagnostic('discovery.json')
            # Only owned scope/status evidence; no URL credentials, TLS keys or OAuth data.
            print('Metrics T2 overview readiness: up=', query('up{job="addp_nodes"}'),
                  'memory=', query('node_memory_MemTotal_bytes{job="addp_nodes"}'), flush=True)
            active = api('targets')['activeTargets']
            print('Metrics T2 overview target status:', [
                {'node': row['labels'].get('addp_node_id'), 'health': row['health'],
                 'error': row['lastError']} for row in active if row['labels'].get('job') == 'addp_nodes'], flush=True)
            raise
        query_env.update(MONITOR_PROMETHEUS_URL=base, ADDP_METRICS_OVERVIEW_SOURCE_IP=overview_ip)
        if not filesystem_only:
            subprocess.run(['go', 'test', './internal/service', '-run',
                            '^TestIntegrationMetricsResourceOverview$', '-count=1', '-v'],
                           cwd=Path(__file__).resolve().parents[2] / 'monitor/backend',
                           env=query_env, check=True, timeout=180)
        # One controlled source now drives the real rule/database/HTTP delivery path.
        # The other five sources and the native exporter checks remain unchanged.
        group = overview_groups[0]
        lifecycles = [] if filesystem_only else ['^TestIntegrationMetricsResourceAlertLifecycle$']
        if filesystem_only or os.environ.get('ADDP_METRICS_T2_CASE') == 'all':
            lifecycles.append('^TestIntegrationMetricsFilesystemAlertLifecycle$')
        for lifecycle in lifecycles:
            subprocess.run(['go', 'test', './internal/service', '-run',
                            lifecycle, '-count=1', '-timeout=210s', '-v'],
                           cwd=Path(__file__).resolve().parents[2] / 'monitor/backend',
                           env=dict(query_env, ADDP_RESOURCE_RULE_LIFECYCLE_INTEGRATION='1',
                                    ADDP_POSTGRES_INTEGRATION='1', ADDP_TEST_POSTGRES_HOST='127.0.0.1',
                                    ADDP_TEST_POSTGRES_PORT=port('postgres', 5432),
                                    ADDP_TEST_POSTGRES_DATABASE='addp_test', ADDP_TEST_POSTGRES_USER='addp',
                                    ADDP_TEST_POSTGRES_PASSWORD=os.environ['METRICS_T2_POSTGRES_PASSWORD'],
                                    ADDP_TEST_POSTGRES_SSLMODE='disable',
                                    ADDP_RESOURCE_RULE_SOURCE_FILE=str(WORK / 'source/overview-0.prom'),
                                    ADDP_METRICS_QUERY_NODE_ID=group['labels']['addp_node_id'],
                                    ADDP_METRICS_QUERY_INSTANCE=group['targets'][0]),
                           check=True, timeout=240)
    finally:
        discovery_text(original_discovery)
    print('Metrics T2: filesystem capacity alert scope passed' if filesystem_only else 'Metrics T2: six-source overview ranks, independent coverage, target exclusion and pre-query budget passed', flush=True)

if os.environ.get('ADDP_METRICS_T2_CASE') in ('all', 'http-quality'):
    prepare_http_test_binaries()
    check_native_process_sources()
if os.environ.get('ADDP_METRICS_T2_CASE') in ('all', 'http-quality'):
    check_python_http_sources()
if os.environ.get('ADDP_METRICS_T2_CASE') in ('all', 'process-alerts'):
    check_native_process_sources(process_alerts=True)
if os.environ.get('ADDP_METRICS_T2_CASE') == 'cgroup-source':
    check_native_process_sources(cgroup_only=True)
if os.environ.get('ADDP_METRICS_T2_CASE') in ('http-quality', 'process-alerts', 'cgroup-source'):
    print('Metrics T2: native process scope complete; host-resource regression belongs to --case=host-resources or --case=all', flush=True)
    raise SystemExit(0)


if os.environ.get('ADDP_METRICS_T2_CASE') == 'filesystem-alerts':
    check_controlled_host_sources(filesystem_only=True)
    raise SystemExit(0)

def failures():
    return sum(float(row['value'][1]) for row in query('prometheus_sd_http_failures_total'))

baseline_failures = failures()
failed_requests = (WORK / 'source/requests.log').read_text().count('GET /api/v1/monitor/platform/metrics_discovery 503')
(WORK / 'source/discovery-failed').touch()
eventually(lambda: failures() > baseline_failures and
           (WORK / 'source/requests.log').read_text().count('GET /api/v1/monitor/platform/metrics_discovery 503') > failed_requests,
           'HTTP SD failure is visible')
assert any(t['labels']['job'] == 'addp_nodes' for t in api('targets')['activeTargets'])
assert valid_sample(), 'discovery failure must retain the last successful target list'
baseline_failures = failures()
redirect_requests = (WORK / 'source/requests.log').read_text().count('GET /api/v1/monitor/platform/metrics_discovery 302')
(WORK / 'source/discovery-failed').unlink()
(WORK / 'source/discovery-redirect').touch()
eventually(lambda: failures() > baseline_failures and
           (WORK / 'source/requests.log').read_text().count('GET /api/v1/monitor/platform/metrics_discovery 302') > redirect_requests,
           'HTTP SD redirect is a visible discovery failure')
assert any(t['labels']['job'] == 'addp_nodes' for t in api('targets')['activeTargets'])
assert 'GET /redirect-target' not in (WORK / 'source/requests.log').read_text()
discovery_text('[]')
(WORK / 'source/discovery-redirect').unlink()
eventually(lambda: not any(t['labels']['job'] == 'addp_nodes' for t in api('targets')['activeTargets']),
           'successful empty HTTP SD removes targets')
discovery_text(original_discovery)
eventually(valid_sample, 'HTTP SD scope restoration resumes collection')
requests = (WORK / 'source/requests.log').read_text()
assert requests.count('POST /api/v1/system/oauth/token 200') >= 2, 'native OAuth must reacquire expired tokens'
assert 'POST /api/v1/system/oauth/token 401' not in requests, 'OAuth must use the independent Basic client credential'
assert os.environ['PROMETHEUS_SERVICE_CLIENT_SECRET'] not in config + requests
print('Metrics T2: native OAuth renewal and owner labels verified; no Token or secret logged', flush=True)
# A real exporter uses the production base template without any host namespace/mount.
node = json.loads(subprocess.check_output(['docker', 'inspect', compose('ps', '-q', 'node-exporter')], text=True))[0]
assert node['Config']['User'] == '65534:65534'
assert node['HostConfig']['ReadonlyRootfs'] and node['HostConfig']['CapDrop'] == ['ALL']
assert not node['HostConfig']['Privileged'] and node['HostConfig']['PidMode'] != 'host'
assert node['HostConfig']['NetworkMode'] != 'host'
assert node['HostConfig']['NanoCpus'] == 250_000_000
assert node['HostConfig']['Memory'] == 256 * 1024**2
assert all(m['Source'] != '/' and not m['RW'] for m in node['Mounts'])
node_base = 'https://localhost:' + port('node-exporter', 9100)
eventually(lambda: b'node_memory_MemTotal_bytes' in get(node_base + '/metrics', source_context),
           'real node_exporter serves authenticated resource metrics')
admission = ssl.create_default_context(cafile=str(deployment / 'source-ca.crt'))
admission.load_cert_chain(str(WORK / 'admission.crt'), str(WORK / 'admission.key'))
assert b'node_cpu_seconds_total' in get(node_base + '/metrics', admission)
foreign_source = ssl.create_default_context(cafile=str(deployment / 'source-ca.crt'))
foreign_source.load_cert_chain(str(WORK / 'untrusted.crt'), str(WORK / 'untrusted.key'))
for ctx in (source_anonymous, source_health, foreign_source):
    rejected(node_base + '/metrics', ctx)
rejected(node_base.replace('https:', 'http:') + '/metrics', source_anonymous)
# The IP SAN, rather than an editable server_name, protects the discovery address.
node_ip = next(iter(node['NetworkSettings']['Networks'].values()))['IPAddress']
node_instance = node_ip + ':9100'
projection = json.loads(original_discovery)
projection[0]['targets'] = [node_instance]
discovery_text(json.dumps(projection))


def node_resource_samples():
    for metric in ('node_cpu_seconds_total', 'node_memory_MemTotal_bytes', 'node_load1',
                   'node_disk_reads_completed_total', 'node_network_receive_bytes_total',
                   'node_boot_time_seconds'):
        rows = query(metric + '{job="addp_nodes",instance="' + node_instance + '"}')
        if not rows or any(row['metric'].get('addp_source') != 'node_exporter' or
                           row['metric'].get('addp_node_id') != projection[0]['labels']['addp_node_id']
                           for row in rows):
            return False
    return True


eventually(node_resource_samples, 'real exporter resource samples enter the sole HTTP SD job')
query_env = resource_query_environment()
query_env.update(ADDP_METRICS_QUERY_NODE_ID=projection[0]['labels']['addp_node_id'], ADDP_METRICS_QUERY_INSTANCE=node_instance)
subprocess.run(['go', 'test', './internal/resourcequery', '-run',
                '^TestIntegrationMetricsResourceQueries$', '-count=1', '-v'],
               cwd=Path(__file__).resolve().parents[2] / 'monitor/backend',
               env=query_env, check=True, timeout=180)
subprocess.run(['go', 'test', './internal/service', '-run', '^TestIntegrationMetricsResourcePressureEvidence$', '-count=1', '-v'],
               cwd=Path(__file__).resolve().parents[2] / 'monitor/backend', env=query_env, check=True, timeout=90)
print('Metrics T2: native fixed catalog instant/range and source isolation', flush=True)

compose('stop', 'node-exporter')
eventually(lambda: bool(query('up{job="addp_nodes",instance="' + node_instance + '"} == 0')),
           'node exporter outage is visible without stopping the center')
subprocess.run(['go', 'test', './internal/service', '-run', '^TestIntegrationMetricsResourcePressureEvidence$', '-count=1', '-v'],
               cwd=Path(__file__).resolve().parents[2] / 'monitor/backend',
               env=dict(query_env, ADDP_RESOURCE_RULE_SOURCE_DOWN='1'), check=True, timeout=30)
assert get(base + '/-/ready')
compose('start', 'node-exporter')
eventually(lambda: bool(query('up{job="addp_nodes",instance="' + node_instance + '"} == 1')),
           'node exporter recovery')

# Reuse the same owned source and mTLS template, now with the Desktop VM
# deployment layer. A new projected node identity keeps the prior full-source
# history out of this limited-view check; this remains a T2 protocol fixture.
FILES.append(str(Path(__file__).resolve().parents[1] / 'infra/node-metrics-desktop.yml'))
os.environ.update(ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX='--no-collector',
                  ADDP_NODE_METRICS_PUBLISH_IP='127.0.0.1', ADDP_NODE_METRICS_PUBLISH_PORT=port('node-exporter', 9100))
compose('up', '-d', '--force-recreate', '--wait', '--wait-timeout', '30', 'node-exporter')
limited = json.loads(subprocess.check_output(['docker', 'inspect', compose('ps', '-q', 'node-exporter')], text=True))[0]
assert limited['HostConfig']['NetworkMode'] != 'host' and limited['HostConfig']['PidMode'] != 'host'
assert limited['HostConfig']['ReadonlyRootfs'] and limited['HostConfig']['CapDrop'] == ['ALL']
assert limited['HostConfig']['SecurityOpt'] == ['no-new-privileges:true']
assert not limited['HostConfig']['Privileged'] and limited['Config']['User'] == '65534:65534'
assert len(limited['Mounts']) == 4 and all(not m['RW'] and m['Source'] != '/' for m in limited['Mounts'])
assert limited['HostConfig']['Memory'] == 256 * 1024**2
assert limited['HostConfig']['NanoCpus'] == 250_000_000
assert len(limited['HostConfig']['PortBindings']['9100/tcp']) == 1
limited_ip = next(iter(limited['NetworkSettings']['Networks'].values()))['IPAddress']
extensions = WORK / 'node-extensions'
extensions.write_text('subjectAltName=DNS:localhost,DNS:host.docker.internal,IP:127.0.0.1,IP:' + limited_ip + '\nextendedKeyUsage=serverAuth\n')
subprocess.run(['openssl', 'x509', '-req', '-in', str(WORK / 'source-server.csr'),
                '-CA', str(deployment / 'source-ca.crt'), '-CAkey', str(WORK / 'source-ca.key'),
                '-CAserial', str(WORK / 'source-ca.srl'), '-days', '1', '-extfile', str(extensions),
                '-out', str(WORK / 'node-tls/server.crt')], check=True, capture_output=True)
# Recreate to apply an atomically replaced certificate bind mount.
compose('up', '-d', '--force-recreate', '--wait', '--wait-timeout', '30', 'node-exporter')
node_base = 'https://localhost:' + port('node-exporter', 9100)
eventually(lambda: b'node_memory_MemTotal_bytes' in get(node_base + '/metrics', source_context),
           'limited VM exporter serves kernel-global metrics')
def diskstats_snapshot():
    # An independent non-root container in the same Engine; no host mounts.
    text = compose('exec', '-T', 'metrics-source', 'cat', '/proc/diskstats')
    return {fields[2]: (tuple(fields[:2]), list(map(int, fields[3:])))
            for fields in (line.split() for line in text.splitlines())}


for namespace in ('pid', 'mnt', 'net'):
    path = '/proc/self/ns/' + namespace
    assert compose('exec', '-T', 'metrics-source', 'readlink', path) != compose(
        'exec', '-T', 'node-exporter', 'readlink', path), 'source shares fixture ' + namespace
before_disks = diskstats_snapshot()
raw = get(node_base + '/metrics', source_context).decode()
after_disks = diskstats_snapshot()
collectors = dict(re.findall(r'^node_scrape_collector_success\{collector="([^"]+)"\} (\S+)$', raw, re.M))
assert set(collectors) == {'cpu', 'meminfo', 'loadavg', 'diskstats', 'stat', 'uname', 'time'}, collectors
assert all(value == '1' for value in collectors.values()), collectors
assert not re.search(r'^node_(?:filesystem_|network_|netstat_)', raw, re.M)
# ProcDiskstats fields: completions, sectors (512 B), elapsed ticks (ms), busy ticks.
disk_fields = {'node_disk_reads_completed_total': (0, 1), 'node_disk_read_bytes_total': (2, 512),
               'node_disk_read_time_seconds_total': (3, .001), 'node_disk_writes_completed_total': (4, 1),
               'node_disk_written_bytes_total': (6, 512), 'node_disk_write_time_seconds_total': (7, .001),
               'node_disk_io_time_seconds_total': (9, .001)}
observed_devices = None
for metric, (index, scale) in disk_fields.items():
    samples = dict(re.findall(r'^' + metric + r'\{device="([^"]+)"\} (\S+)$', raw, re.M))
    assert samples, 'no kernel block-device samples: ' + metric
    if observed_devices is None:
        observed_devices = set(samples)
    assert set(samples) == observed_devices, 'incomplete kernel block-device counters'
    for device, sample in samples.items():
        assert device in before_disks and device in after_disks, 'foreign device: ' + device
        assert before_disks[device][0] == after_disks[device][0], 'device identity changed'
        value = float(sample)
        low, high = before_disks[device][1][index] * scale, after_disks[device][1][index] * scale
        assert math.isfinite(value) and 0 <= low <= high and low - 1e-9 <= value <= high + 1e-9, (
            metric, device, low, value, high)
assert any(after_disks[d][1][0] > 0 or after_disks[d][1][4] > 0 for d in observed_devices), 'only unused devices observed'
disk_identities = {}
for labels in re.findall(r'^node_disk_info\{([^}]+)\} 1$', raw, re.M):
    labels = dict(re.findall(r'(\w+)="([^"]*)"', labels))
    disk_identities[labels['device']] = (labels['major'], labels['minor'])
assert set(disk_identities) == observed_devices, 'incomplete disk identity evidence'
assert all(disk_identities[d] == after_disks[d][0] for d in observed_devices), 'foreign disk identity'
print(f'Metrics T2: {len(observed_devices)} devices and seven disk counters match independent namespace snapshots', flush=True)
cores = re.findall(r'^node_cpu_seconds_total\{cpu="([^"]+)",mode="idle"\} (\S+)$', raw, re.M)
assert len(cores) == engine['NCPU'], 'collector CPU quota was substituted for Engine capacity'
memory = float(re.search(r'^node_memory_MemTotal_bytes (\S+)$', raw, re.M).group(1))
assert abs(memory - engine['MemTotal']) <= 4096 and memory > limited['HostConfig']['Memory']
assert f'release="{engine["KernelVersion"]}"' in raw
for metric in ('node_load1', 'node_load5', 'node_load15', 'node_boot_time_seconds'):
    value = float(re.search(r'^' + metric + r' (\S+)$', raw, re.M).group(1))
    assert math.isfinite(value) and value >= 0
for ctx in (source_anonymous, source_health, foreign_source):
    rejected(node_base + '/metrics', ctx)
assert b'node_cpu_seconds_total' in get(node_base + '/metrics', admission)
print(f'Metrics T2: limited Engine={engine["OperatingSystem"]}, kernel={engine["KernelVersion"]}, '
      f'cores={len(cores)}, memory_bytes={int(memory)}; container quota=0.25 CPU/256 MiB', flush=True)

node_instance = limited_ip + ':9100'
if desktop_transport:
    # Exercise the production transformation through an actual loopback publish.
    loopback_port = port('node-exporter', 9100)
    os.environ['ADDP_METRICS_DESKTOP_LOOPBACK_PORTS'] = ','.join([loopback_port, *process_loopback_ports])
    subprocess.run(['python3', str(Path(__file__).resolve().parents[1] / 'infra/generate-metrics-config.py')], check=True)
    compose('up', '-d', '--force-recreate', '--wait', '--wait-timeout', '90', 'prometheus')
    base = 'https://localhost:' + port('prometheus', 9090)
    node_instance = '127.0.0.1:' + loopback_port
    print('Metrics T2: Desktop source uses loopback publish and the sole host transport, without LAN addressing', flush=True)
projection[0]['targets'] = [node_instance]
projection[0]['labels']['addp_node_id'] = '22222222-2222-4222-8222-222222222222'
discovery_text(json.dumps(projection))
def limited_samples():
    selector = '{job="addp_nodes",addp_node_id="' + projection[0]['labels']['addp_node_id'] + '"}'
    return (job_is_up('addp_nodes', '1') and len(query('node_memory_MemTotal_bytes' + selector)) == 1
            and bool(query('node_disk_written_bytes_total' + selector))
            and not query('node_filesystem_size_bytes' + selector))
eventually(limited_samples, 'limited VM disk metrics enter the sole HTTP SD job without filesystem samples')
query_env.update(MONITOR_PROMETHEUS_URL=base, ADDP_METRICS_QUERY_NODE_ID=projection[0]['labels']['addp_node_id'],
                 ADDP_METRICS_QUERY_INSTANCE=node_instance, ADDP_METRICS_QUERY_RESTRICTED_VM='1')
subprocess.run(['go', 'test', './internal/resourcequery', '-run',
                '^TestIntegrationMetricsResourceQueries$', '-count=1', '-v'],
               cwd=Path(__file__).resolve().parents[2] / 'monitor/backend',
               env=query_env, check=True, timeout=180)
compose('stop', 'node-exporter')
eventually(lambda: bool(query('up{job="addp_nodes",instance="' + node_instance + '"} == 0')),
           'limited VM source outage')
assert get(base + '/-/ready')
restarted_at = time.time()
compose('start', 'node-exporter')
assert port('node-exporter', 9100) == os.environ['ADDP_NODE_METRICS_PUBLISH_PORT'], 'limited source changed its explicit published port'
eventually(lambda: limited_samples() and
           float(query('timestamp(node_memory_MemTotal_bytes{job="addp_nodes",addp_node_id="' +
                       projection[0]['labels']['addp_node_id'] + '"})')[0]['value'][1]) >= restarted_at and
           all(float(row['value'][1]) >= restarted_at for row in query(
               'timestamp(node_disk_written_bytes_total{job="addp_nodes",addp_node_id="' +
               projection[0]['labels']['addp_node_id'] + '"})')),
           'limited VM source recovery with a new sample')
discovery_text(original_discovery)
eventually(valid_sample, 'fixture scope restored after real exporter checks')
# Only the disposable metrics center is killed; a separate fixture route stays live.
compose('kill', '-s', 'SIGKILL', 'prometheus')
assert get('http://127.0.0.1:' + port('metrics-source', 8080) + '/alive') == b'fixture alive\n'
compose('up', '-d', '--wait', '--wait-timeout', '90', 'prometheus')
base = 'https://localhost:' + port('prometheus', 9090)
eventually(lambda: bool(query('addp_fixture_resource_bytes{job="addp_nodes"}')), 'center recovery')
# An instant query evaluated at the first sample time proves WAL replay, not a new scrape.
result = api('query?' + urllib.parse.urlencode({'query': 'addp_fixture_resource_bytes{job="addp_nodes"}', 'time': initial_sample_time}))['result']
assert result and result[0]['value'][1] == '4096', result
print('Metrics T2: prior sample persisted after SIGKILL; independent fixture route remained available', flush=True)


check_controlled_host_sources()
