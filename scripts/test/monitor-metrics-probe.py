"""Real scrape, admission, mTLS, persistence and owned outage checks."""
import json
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


def compose(*args):
    return subprocess.check_output(['docker', 'compose', '--env-file', '/dev/null', '-p', PROJECT,
                                    '-f', FILE, *args], text=True).strip()


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
control = 'https://localhost:' + port('metrics-source', 9444)
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
try:
    get(control + '/api/v1/monitor/platform/metrics_discovery', anonymous)
except urllib.error.HTTPError as err:
    assert err.code == 401, err.code
else:
    raise AssertionError('Anonymous HTTP SD was accepted')
print('Metrics T2: anonymous, foreign client CA and plaintext rejected', flush=True)

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
