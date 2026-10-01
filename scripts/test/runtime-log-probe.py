"""Bounded end-to-end probe of only gate-owned log infrastructure."""
import json
import re
import os
import sys
import time
import urllib.parse
import urllib.request
import urllib.error
from pathlib import Path

root = Path(sys.argv[1])
mode = sys.argv[2]
url = os.environ['LOKI_TEST_URL']
assert url.startswith('http://127.0.0.1:'), 'probe requires an owned loopback endpoint'
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
