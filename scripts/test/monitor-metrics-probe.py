"""Real scrape, admission, mTLS, persistence and owned outage checks."""
import json
import math
import os
import re
import ssl
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


def compose(*args):
    flags = [flag for path in FILES for flag in ('-f', path)]
    return subprocess.check_output(['docker', 'compose', '--env-file', '/dev/null', '-p', PROJECT,
                                    *flags, *args], text=True).strip()


def port(service, internal):
    return compose('port', service, str(internal)).rsplit(':', 1)[1]


state = json.loads(subprocess.check_output(['docker', 'inspect', compose('ps', '-q', 'prometheus')], text=True))[0]
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


def query(expression):
    return api('query?' + urllib.parse.urlencode({'query': expression}))['result']


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
subprocess.run(['go', 'test', './internal/resourcequery', '-run',
                '^TestIntegrationMetrics(CPUWindow|DiskWindow|DiskIOWindow|NetworkWindow|Filesystem|FilesystemInodes)$', '-count=1', '-v'],
               cwd=Path(__file__).resolve().parents[2] / 'monitor/backend',
               env=dict(os.environ, GOWORK='off', ADDP_METRICS_QUERY_INTEGRATION='1'),
               check=True, timeout=60)


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


eventually(valid_sample, 'real mTLS scrape and controlled job label')
# Scrape jobs have independent initial offsets; fixture readiness is not self readiness.
eventually(self_sample_ready, 'center self scrape and resident memory')
assert job_is_up('addp_nodes', '1')
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
eventually(exceeded, 'sample overflow is a visible scrape failure')
assert query('addp_fixture_overflow') == [], 'overflow must not admit a partial sample set'
source_text(original_metrics)
eventually(lambda: job_is_up('addp_nodes', '1'), 'budget recovery')

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
query_env = dict(os.environ, GOWORK='off', ADDP_METRICS_QUERY_INTEGRATION='1',
                 MONITOR_PROMETHEUS_URL=base, MONITOR_PROMETHEUS_CA_FILE=str(TLS / 'ca.crt'),
                 MONITOR_PROMETHEUS_CLIENT_CERT_FILE=str(WORK / 'query-tls/client.crt'),
                 MONITOR_PROMETHEUS_CLIENT_KEY_FILE=str(WORK / 'query-tls/client.key'),
                 ADDP_METRICS_QUERY_NODE_ID=projection[0]['labels']['addp_node_id'],
                 ADDP_METRICS_QUERY_INSTANCE=node_instance)
subprocess.run(['go', 'test', './internal/resourcequery', '-run',
                '^TestIntegrationMetricsResourceQueries$', '-count=1', '-v'],
               cwd=Path(__file__).resolve().parents[2] / 'monitor/backend',
               env=query_env, check=True, timeout=180)
print('Metrics T2: native fixed catalog instant/range and source isolation', flush=True)

compose('stop', 'node-exporter')
eventually(lambda: bool(query('up{job="addp_nodes",instance="' + node_instance + '"} == 0')),
           'node exporter outage is visible without stopping the center')
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
engine = json.loads(subprocess.check_output(['docker', 'info', '--format', '{{json .}}'], text=True))
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
if __import__('platform').system() == 'Darwin' and engine['OperatingSystem'] == 'Docker Desktop':
    # Exercise the production transformation through an actual loopback publish.
    loopback_port = port('node-exporter', 9100)
    os.environ['ADDP_METRICS_DESKTOP_LOOPBACK_PORT'] = loopback_port
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
