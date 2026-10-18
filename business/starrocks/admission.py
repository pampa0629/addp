#!/usr/bin/env python3
"""Own only the disposable official StarRocks protocol-admission resources."""
from __future__ import annotations

import json
import os
from pathlib import Path
import platform
import secrets
import subprocess
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[2]
IMAGE = 'starrocks/allin1-ubuntu:3.5.21@sha256:faf5f6b17a301b276b829482af11d350004833dc9162232e1083a2aad0fa8b2a'
LABEL = 'com.addp.starrocks-admission'


def run(*args, evidence=None, **kwargs):
    result = subprocess.run(args, capture_output=True, text=True, **kwargs)
    if evidence is not None:
        # Only the fixed FROM/LABEL build uses this; SQL/inspection never does.
        private(evidence, {'stdout': result.stdout, 'stderr': result.stderr})
    if result.returncode:
        raise RuntimeError('StarRocks admission command failed: ' + ' '.join(args[:2]))
    return result.stdout.strip()


def directory():
    path = Path(os.environ['ADDP_STARROCKS_ADMISSION_DIR'])
    if not path.is_absolute() or not path.is_dir() or path.resolve() != path or path.is_relative_to(ROOT) or path.stat().st_mode & 0o077:
        raise RuntimeError('canonical private external admission directory required')
    return path


def private(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w') as output:
        json.dump(value, output)


def inspect(kind, name):
    result = subprocess.run(['docker', kind, 'inspect', name], capture_output=True, text=True)
    if result.returncode:
        run('docker', 'info')
        if 'no such' not in result.stderr.lower() and 'not found' not in result.stderr.lower():
            raise RuntimeError('unknown Docker resource state')
        return None
    return json.loads(result.stdout)[0]


def owned(kind, name, state):
    value = inspect(kind, name)
    labels = (value.get('Config', {}).get('Labels', {}) if kind != 'volume' else value.get('Labels', {})) if value else {}
    if value and (labels or {}).get(LABEL) != state['run_id']:
        raise RuntimeError('refusing foreign StarRocks admission resource')
    return value


def sql(name, statement, *, evidence=None):
    return run('docker', 'exec', '-i', name, 'mysql', '--default-character-set=utf8mb4', '--batch', '--raw', '-h127.0.0.1', '-P9030', '-uroot', input=statement, timeout=30, evidence=evidence)


def start():
    path = directory()
    architecture = {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'amd64', 'amd64': 'amd64'}.get(platform.machine().lower())
    daemon = run('docker', 'info', '--format', '{{.OSType}}/{{.Architecture}}')
    if architecture is None or daemon not in ('linux/' + architecture, 'linux/' + ('aarch64' if architecture == 'arm64' else 'x86_64')):
        raise RuntimeError('native matching Linux Docker architecture required')
    state = {'run_id': uuid.uuid4().hex}
    name = 'addp-starrocks-admission-' + state['run_id']
    state.update(name=name, image=name + ':local', volumes=[name + '-fe', name + '-be'])
    private(path / 'state.json', state)
    for kind, resource in [('container', name), ('image', state['image']), *[('volume', v) for v in state['volumes']]]:
        if inspect(kind, resource) is not None:
            raise RuntimeError('refusing preexisting admission resource')
    with tempfile.TemporaryDirectory(prefix='addp-starrocks-admission-build-') as temporary:
        Path(temporary, 'Dockerfile').write_text(f'FROM {IMAGE}\nLABEL {LABEL}="{state["run_id"]}"\n')
        run('docker', 'build', '-t', state['image'], temporary, timeout=900, evidence=path / 'image-build.json')
    image = owned('image', state['image'], state)
    if not image or image.get('Os') != 'linux' or image.get('Architecture') != architecture:
        raise RuntimeError('official image must match the native daemon architecture')
    for volume in state['volumes']:
        run('docker', 'volume', 'create', '--label', LABEL + '=' + state['run_id'], volume)
    run('docker', 'run', '-d', '--name', name, '--label', LABEL + '=' + state['run_id'], '--memory=4g', '--shm-size=256m',
        '--mount', 'type=volume,source=' + state['volumes'][0] + ',target=/data/deploy/starrocks/fe/meta',
        '--mount', 'type=volume,source=' + state['volumes'][1] + ',target=/data/deploy/starrocks/be/storage',
        '-p', '127.0.0.1::9030', state['image'])
    deadline = time.monotonic() + 300
    while time.monotonic() < deadline:
        value = owned('container', name, state)
        if not value or not value['State']['Running']:
            raise RuntimeError('StarRocks official entrypoint exited')
        try:
            lines = sql(name, 'SHOW BACKENDS;').splitlines()
            rows = [dict(zip(lines[0].split('\t'), row.split('\t'))) for row in lines[1:]] if lines else []
            if len(rows) == 1 and rows[0].get('Alive', '').lower() == 'true':
                break
        except RuntimeError:
            pass
        time.sleep(2)
    else:
        raise RuntimeError('StarRocks FE/BE readiness deadline exceeded')
    sql(name, """CREATE DATABASE addp_acceptance;
CREATE TABLE addp_acceptance.source(name VARCHAR(128),id BIGINT,amount DECIMAL(38,18),optional_text VARCHAR(128))
PRIMARY KEY(name,id) DISTRIBUTED BY HASH(name) BUCKETS 1 PROPERTIES('replication_num'='1');
INSERT INTO addp_acceptance.source VALUES ('正式接入中文',9007199254740993,12345678901234567890.123456789012345678,NULL),('保留尾空格  ',2,-0.000000000000000001,'');
CREATE TABLE addp_acceptance.write_probe(id BIGINT) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 1 PROPERTIES('replication_num'='1');
""", evidence=path / 'schema-initialization.json')
    password = 'Sr9@' + secrets.token_hex(12)
    sql(name, f"""CREATE USER fixture_reader IDENTIFIED BY '{password}';
GRANT SELECT ON TABLE addp_acceptance.source TO USER fixture_reader;
""")
    value = owned('container', name, state)
    mapping = value['NetworkSettings']['Ports']['9030/tcp']
    if len(mapping) != 1 or mapping[0]['HostIp'] != '127.0.0.1':
        raise RuntimeError('random loopback-only SQL binding required')
    private(path / 'connection.json', {'host': '127.0.0.1', 'port': int(mapping[0]['HostPort']), 'reader_password': password})
    private(path / 'facts.json', {'architecture': architecture, 'image': IMAGE, 'image_id': value['Image'], 'server_version': sql(name, 'SELECT current_version();')})
    print('STARROCKS_ADMISSION_READY architecture=' + architecture, flush=True)


def stop():
    path = directory()
    file = path / 'state.json'
    if not file.exists():
        return
    state = json.loads(file.read_text())
    resources = [('container', state['name'], ('rm', '-fv')), *[('volume', v, ('volume', 'rm')) for v in state['volumes']], ('image', state['image'], ('image', 'rm'))]
    for kind, name, command in resources:
        if owned(kind, name, state) is not None:
            run('docker', *command, name, timeout=90)
        if inspect(kind, name) is not None:
            raise RuntimeError('StarRocks admission residual: ' + kind)
    private(path / 'cleanup.json', {'result': 'passed', 'remaining_resources': 0})
    file.unlink()
    print('STARROCKS_ADMISSION_CLEANUP_PASS remaining_resources=0', flush=True)


if __name__ == '__main__':
    import sys
    try:
        {'start': start, 'stop': stop}[sys.argv[1]]()
    except (RuntimeError, OSError, ValueError, KeyError, IndexError) as error:
        print(str(error), file=sys.stderr)
        raise SystemExit(1)
