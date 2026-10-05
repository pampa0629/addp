"""Real scrape, admission, mTLS, persistence and owned outage checks."""
import json
import os
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


def get(url, ctx=context):
    with urllib.request.urlopen(url, context=ctx, timeout=5) as response:
        return response.read()


def api(path):
    data = json.loads(get(base + '/api/v1/' + path))
    assert data['status'] == 'success', data
    return data['data']


def query(expression):
    return api('query?' + urllib.parse.urlencode({'query': expression}))['result']


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
assert get(source + '/metrics')
anonymous = ssl.create_default_context(cafile=str(TLS / 'ca.crt'))
wrong = ssl.create_default_context(cafile=str(TLS / 'ca.crt'))
wrong.load_cert_chain(str(WORK / 'untrusted.crt'), str(WORK / 'untrusted.key'))
for url in (base + '/-/ready', base + '/api/v1/query?query=up', source + '/metrics'):
    rejected(url, anonymous)
    rejected(url, wrong)
rejected(base.replace('https:', 'http:') + '/-/ready', anonymous)
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
              'label_limit: 20', 'label_name_length_limit: 128', 'label_value_length_limit: 512'):
    assert value in config, (value, config)


def valid_sample():
    data = query('addp_fixture_resource_bytes{job="fixture"}')
    return len(data) == 1 and data[0]['value'][1] == '4096' and data[0]['metric']['exported_job'] == 'spoof'


eventually(valid_sample, 'real mTLS scrape and controlled job label')
assert query('up{job="prometheus"}')[0]['value'][1] == '1'
assert float(query('process_resident_memory_bytes{job="prometheus"}')[0]['value'][1]) > 0
assert query('up{job="fixture"}')[0]['value'][1] == '1'
initial_sample_time = query('addp_fixture_resource_bytes{job="fixture"}')[0]['value'][0]
# Write atomically without replacing the bind-mounted source directory.
def source_text(text):
    pending = WORK / 'source/pending'
    pending.write_text(text)
    pending.replace(WORK / 'source/metrics')


def exceeded():
    targets = api('targets')['activeTargets']
    item = next(t for t in targets if t['labels']['job'] == 'fixture')
    return item['health'] == 'down' and 'sample limit' in item['lastError'].lower()

source_text(''.join(f'addp_fixture_overflow{{index="{i}"}} {i}\n' for i in range(20001)))
eventually(exceeded, 'sample overflow is a visible scrape failure')
assert query('addp_fixture_overflow') == [], 'overflow must not admit a partial sample set'
source_text('addp_fixture_resource_bytes{job="spoof"} 4096\n')
eventually(lambda: query('up{job="fixture"}')[0]['value'][1] == '1', 'budget recovery')

compose('stop', 'metrics-source')
eventually(lambda: query('up{job="fixture"}')[0]['value'][1] == '0', 'source outage remains a collection failure')
assert get(base + '/-/ready')
compose('start', 'metrics-source')
eventually(valid_sample, 'source recovery')
# Only the disposable metrics center is killed; a separate fixture route stays live.
compose('kill', '-s', 'SIGKILL', 'prometheus')
assert get('http://127.0.0.1:' + port('metrics-source', 8080) + '/alive') == b'fixture alive\n'
compose('up', '-d', '--wait', '--wait-timeout', '90', 'prometheus')
base = 'https://localhost:' + port('prometheus', 9090)
eventually(lambda: bool(query('addp_fixture_resource_bytes{job="fixture"}')), 'center recovery')
# An instant query evaluated at the first sample time proves WAL replay, not a new scrape.
result = api('query?' + urllib.parse.urlencode({'query': 'addp_fixture_resource_bytes{job="fixture"}', 'time': initial_sample_time}))['result']
assert result and result[0]['value'][1] == '4096', result
print('Metrics T2: prior sample persisted after SIGKILL; independent fixture route remained available', flush=True)
