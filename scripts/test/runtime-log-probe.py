"""Bounded end-to-end probe of only gate-owned log infrastructure."""
import json
import re
import os
import sys
import subprocess
import time
import urllib.parse
import urllib.request
import urllib.error
from pathlib import Path

root = Path(sys.argv[1])
mode = sys.argv[2]
url = os.environ['LOKI_TEST_URL']
assert url.startswith('http://127.0.0.1:'), 'probe requires an owned loopback endpoint'

if mode == 'capacity-pressure':
    statuses = list(root.glob('capacity-pressure/*/status.json'))
    assert len(statuses) == 1, 'pressure fixture must have one process identity'
    status = json.loads(statuses[0].read_text())
    assert status['received'] == 32768, status
    assert status['received'] == status['written'] + status['dropped'], status
    assert status['written'] > 0 and status['source_files_early_cleaned'] > 0, status
    instance_bytes = sum(p.stat().st_size for p in statuses[0].parent.glob('*.jsonl'))
    node_bytes = sum(p.stat().st_size for p in root.glob('*/*/*.jsonl'))
    assert 0 < instance_bytes <= 131072 and node_bytes <= 1048576, (instance_bytes, node_bytes)
    for file in statuses[0].parent.glob('*.jsonl'):
        for line in file.read_text().splitlines():
            entry = json.loads(line)
            assert entry['instance_id'] == status['instance_id'] and entry['node_name'] == 'observer-t2'
    print('PASS: capacity-pressure', {key: status[key] for key in ('received', 'written', 'dropped', 'write_failures', 'source_files_early_cleaned')},
          {'instance_bytes': instance_bytes, 'node_bytes': node_bytes})
    sys.exit(0)


def owned_compose(*args):
    project = os.environ['LOKI_TEST_PROJECT']
    compose_file = Path(os.environ['LOKI_TEST_COMPOSE'])
    assert project.startswith('addp-runtime-log-t2-') and compose_file.is_absolute()
    result = subprocess.run(['docker', 'compose', '--env-file', '/dev/null', '-p', project, '-f', str(compose_file), *args],
                            check=True, capture_output=True, text=True, timeout=30)
    return result.stdout


def chunk_keys():
    # Credentials remain inside the owned container environment, not command
    # arguments, stdout or the retained fixture identity file. Only list objects.
    listing = owned_compose('exec', '-T', 'minio', 'sh', '-ec',
                            'mc alias set retention http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null; '
                            'mc ls --json --recursive retention/addp-runtime-logs-test/fake/')
    keys = set()
    for line in listing.splitlines():
        item = json.loads(line)
        assert item['status'] == 'success', 'owned object listing failed'
        if item.get('type') == 'file':
            keys.add(item['key'])
    return keys


def retention_query(fixture):
    params = urllib.parse.urlencode({'query': '{deployment="addp",module_name="retention-fixture",role='+json.dumps(fixture['role'])+'}',
                                   'start': str(fixture['stamp'] - 10**6), 'end': str(fixture['stamp'] + 10**6), 'limit': 10})
    req = urllib.request.Request(url+'/loki/api/v1/query_range?'+params,
                                 headers={'Authorization': 'Bearer '+os.environ['LOKI_READ_TOKEN']})
    with urllib.request.urlopen(req, timeout=10) as response:
        body = json.load(response)
    assert body['status'] == 'success'
    return [entry[1] for stream in body['data']['result'] for entry in stream['values']]


if mode.startswith('retention-'):
    identity_file = Path(sys.argv[3])
    if mode == 'retention-seed':
        fixtures = {}
        for name, hours in [('expired', 192), ('control', 144)]:
            stamp = (time.time_ns() // 10**6 - hours*3600*1000)*10**6
            fixture = {'stamp': stamp, 'role': name, 'message': 'owned-retention-'+name}
            fixtures[name] = fixture
            body = json.dumps({'streams': [{'stream': {'deployment': 'addp', 'module_name': 'retention-fixture', 'role': name},
                                           'values': [[str(stamp), fixture['message']]]}]}).encode()
            req = urllib.request.Request(url+'/loki/api/v1/push', data=body,
                                         headers={'Authorization': 'Bearer '+os.environ['LOKI_WRITE_TOKEN'], 'Content-Type': 'application/json'})
            with urllib.request.urlopen(req, timeout=10) as response:
                assert response.status == 204
        # Flush only the owned ingester via its private network. Never expose
        # management HTTP paths through the public read/write proxy.
        owned_compose('exec', '-T', 'minio', 'curl', '-fsS', '-X', 'POST', 'http://loki:3100/flush')
        deadline = time.monotonic()+90
        while True:
            keys = chunk_keys()
            for fixture in fixtures.values():
                # Loki's pinned v13 chunk key ends in hex From:Through:Checksum
                # (timestamps in ms). Identify physical objects independently
                # from query visibility rather than guessing from index names.
                stamp = format(fixture['stamp']//10**6, 'x')
                fixture['keys'] = sorted(key for key in keys if key.rsplit('/', 1)[-1].startswith(stamp+':'+stamp+':'))
            if all(fixture['keys'] for fixture in fixtures.values()):
                break
            assert time.monotonic() < deadline, 'retention fixture chunks not stored before cleanup'
            time.sleep(1)
        identity_file.write_text(json.dumps(fixtures))
        os.chmod(identity_file, 0o600)
        print('PASS: retention-seed, 8-day expired and 6-day control chunks physically stored')
    elif mode == 'retention-deleted':
        fixtures = json.loads(identity_file.read_text())
        expired, control = set(fixtures['expired']['keys']), set(fixtures['control']['keys'])
        deadline = time.monotonic()+240
        started = time.monotonic()
        while True:
            keys = chunk_keys()
            assert control <= keys, 'Compactor deleted a nonexpired control chunk'
            if not expired & keys:
                print('PASS: retention-deleted, actual expired chunk objects removed; control retained; elapsed=%.2fs' % (time.monotonic()-started))
                break
            assert time.monotonic() < deadline, 'Compactor did not physically remove expired chunks'
            time.sleep(2)
    elif mode in ('retention-stored', 'retention-query'):
        fixtures = json.loads(identity_file.read_text())
        deadline = time.monotonic()+60
        while True:
            expected_expired = [fixtures['expired']['message']] if mode == 'retention-stored' else []
            if retention_query(fixtures['expired']) == expected_expired and retention_query(fixtures['control']) == [fixtures['control']['message']]:
                assert set(fixtures['control']['keys']) <= chunk_keys(), 'control chunk missing'
                if mode == 'retention-stored':
                    assert set(fixtures['expired']['keys']) <= chunk_keys(), 'expired chunk missing before cleanup'
                print('PASS:', mode, 'physical objects and both query results verified after owned Loki restart')
                break
            assert time.monotonic() < deadline, 'retention query result differs from physical storage after restart'
            time.sleep(1)
    else:
        raise AssertionError('unknown retention mode')
    sys.exit(0)

if mode.startswith('retry-'):
    metrics_url = os.environ['ALLOY_TEST_URL']
    assert metrics_url.startswith('http://127.0.0.1:')
    deadline = time.monotonic()+45
    while True:
        with urllib.request.urlopen(metrics_url+'/metrics', timeout=5) as response:
            metrics = response.read().decode()
        count = sum(float(match.group(1)) for match in re.finditer(r'^loki_write_batch_retries_total(?:\{[^\n]*\})? ([0-9.eE+]+)$', metrics, re.M))
        if mode == 'retry-baseline':
            print(count)
            sys.exit(0)
        if count > float(sys.argv[3]):
            print('PASS: sending retry observed during owned endpoint outage')
            sys.exit(0)
        assert time.monotonic()<deadline, 'sending retry was not observable'
        time.sleep(1)

entries = []
for file in root.glob('manager/*/*.jsonl'):
    for line in file.read_text().splitlines():
        entry = json.loads(line)
        assert 'sample-secret' not in line, 'credential entered source'
        entries.append(entry)
first = next(e for e in entries if 'runtime-t2-first' in e['message'])['instance_id']
second = next((e['instance_id'] for e in entries if 'runtime-t2-second' in e['message']),None)
instances = {e['instance_id'] for e in entries}
if mode == 'restarted':
    assert len(instances) == 2, 'restart reused instance identity'

def request(token, query):
    params = urllib.parse.urlencode({'query': query, 'start': str(time.time_ns()-3600*10**9), 'end': str(time.time_ns()), 'limit': 100})
    req = urllib.request.Request(url+'/loki/api/v1/query_range?'+params)
    if token:
        req.add_header('Authorization', 'Bearer '+token)
    return urllib.request.urlopen(req, timeout=10)

for token in ['', os.environ['LOKI_WRITE_TOKEN']]:
    try:
        request(token, '{deployment="addp"}')
        raise AssertionError('unauthorized query succeeded')
    except urllib.error.HTTPError as e:
        assert e.code == 401

deadline = time.monotonic()+90
while True:
    try:
        with request(os.environ['LOKI_READ_TOKEN'], '{deployment="addp",module_name="manager"} | instance_id='+json.dumps(first)) as response:
            payload=json.load(response)
        logs=[json.loads(value[1]) for stream in payload['data']['result'] for value in stream['values']]
        if any('runtime-t2-first' in e['message'] for e in logs) and any(e['channel']=='stderr' and e['level']=='unknown' for e in logs):
            assert all(e['instance_id']==first for e in logs), 'instance query leaked another process'
            assert not any('runtime-t2-second' in e['message'] for e in logs)
            assert 'sample-secret' not in json.dumps(logs)
            break
    except (urllib.error.URLError, KeyError):
        pass
    if time.monotonic()>deadline:
        raise AssertionError('log pipeline failed to become queryable')
    time.sleep(1)


if mode == 'restarted':
    deadline = time.monotonic()+60
    while True:
        with request(os.environ['LOKI_READ_TOKEN'], '{deployment="addp",module_name="manager"} | instance_id='+json.dumps(second)) as response:
            payload=json.load(response)
        logs=[json.loads(value[1]) for stream in payload['data']['result'] for value in stream['values']]
        if any('runtime-t2-second' in e['message'] for e in logs):
            assert all(e['instance_id']==second for e in logs)
            assert not any('runtime-t2-first' in e['message'] for e in logs)
            break
        assert time.monotonic()<deadline, 'new instance log not collected'
        time.sleep(1)

if mode == 'outage-recovered':
    third = next(e['instance_id'] for e in entries if 'runtime-t2-outage' in e['message'])
    deadline = time.monotonic()+60
    while True:
        with request(os.environ['LOKI_READ_TOKEN'], '{deployment="addp",module_name="manager"} | instance_id='+json.dumps(third)) as response:
            payload=json.load(response)
        logs=[json.loads(value[1]) for stream in payload['data']['result'] for value in stream['values']]
        if any('runtime-t2-outage' in e['message'] for e in logs):
            assert all(e['instance_id']==third for e in logs)
            break
        assert time.monotonic()<deadline, 'retry did not recover within bounded outage'
        time.sleep(1)

print('PASS:', mode)
