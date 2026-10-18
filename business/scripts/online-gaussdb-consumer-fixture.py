#!/usr/bin/env python3
"""Owned GaussDB read-only fixture shared by Provider T2 and Hosted consumer T4."""
from __future__ import annotations
import argparse
import importlib.util
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
SPEC = importlib.util.spec_from_file_location('gaussdb_image', ROOT / 'business/gaussdb/image.py')
IMAGE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(IMAGE)
LABEL = 'com.addp.gaussdb-readonly-fixture'


def run(*args, **kwargs):
    result = subprocess.run(args, capture_output=True, text=True, **kwargs)
    if result.returncode:
        # SQL and Docker inspection can contain credentials; never echo either.
        raise RuntimeError('GaussDB fixture command failed: ' + args[0] + (' ' + args[1] if len(args)>1 and args[0]=='docker' else ''))
    return result.stdout.strip()


def directory():
    path = Path(os.environ['ADDP_GAUSSDB_FIXTURE_DIR'])
    if not path.is_absolute() or not path.is_dir() or path.resolve() != path or path.is_relative_to(ROOT) or path.stat().st_mode & 0o077:
        raise RuntimeError('canonical private external fixture directory required')
    return path


def private(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as output:
        json.dump(value, output)


def inspect(kind, name):
    result = subprocess.run(['docker', kind, 'inspect', name], capture_output=True, text=True)
    if result.returncode:
        run('docker', 'info')  # Missing resource and unavailable daemon differ.
        if 'no such' not in result.stderr.lower() and 'not found' not in result.stderr.lower():
            raise RuntimeError('unknown Docker resource state')
        return None
    return json.loads(result.stdout)[0]


def owned(kind, state):
    value = inspect(kind, state['name'] + (':local' if kind == 'image' else ''))
    if value is not None:
        labels = value.get('Config', {}).get('Labels', {}) if kind in ('container', 'image') else value.get('Labels', {})
        if (labels or {}).get(LABEL) != state['run_id']:
            raise RuntimeError('refusing foreign fixture resource: ' + kind)
    return value


def sql(state, statement):
    owned('container', state)
    return run('docker', 'exec', '-i', state['name'], 'gsql', '-X', '-v', 'ON_ERROR_STOP=1', '-h', '/data/run', '-p', '5432', '-U', 'omm', '-d', 'business', '-At', input=statement, timeout=30)


def start():
    path = directory()
    state = {'run_id': uuid.uuid4().hex}
    state['name'] = 'addp-gaussdb-readonly-' + state['run_id']
    private(path / 'gaussdb-state.json', state)
    architecture = IMAGE.go_architecture(platform.machine())
    IMAGE.verify_daemon_architecture(architecture, run('docker', 'info', '--format', '{{.OSType}}/{{.Architecture}}'))
    facts = IMAGE.media_facts()
    name = state['name']
    if any(inspect(kind, name + (':local' if kind == 'image' else '')) is not None for kind in ('container','volume','network','image')):
        raise RuntimeError('refusing existing fixture resource')
    with tempfile.TemporaryDirectory(prefix='addp-gaussdb-readonly-image-') as temp:
        IMAGE.prepare_context(Path(temp), architecture, Path(os.environ.get('ADDP_GAUSSDB_MEDIA_CACHE', '/tmp/addp-gaussdb-official-media')))
        # Same Business build inputs and entrypoint as production/local and T5.
        run('docker', 'build', '--build-arg', 'GAUSSDB_BASE_IMAGE=' + facts['GAUSSDB_OFFICIAL_BASE_IMAGE'], '--build-arg', 'GAUSSDB_VERSION=' + facts['GAUSSDB_OFFICIAL_VERSION'], '--label', LABEL + '=' + state['run_id'], '-t', name + ':local', temp, timeout=900)
    run('docker', 'network', 'create', '--label', LABEL + '=' + state['run_id'], name)
    run('docker', 'volume', 'create', '--label', LABEL + '=' + state['run_id'], name)
    env = {**os.environ, 'GAUSSDB_PASSWORD':'Ac9@'+secrets.token_hex(12), 'GAUSSDB_APP_PASSWORD':'Ab9@'+secrets.token_hex(12), 'GAUSSDB_USER':'fixture_owner'}
    run('docker','run','-d','--name',name,'--network',name,'--label',LABEL+'='+state['run_id'],'--shm-size=256m','--mount','type=volume,source='+name+',target=/data','-p','127.0.0.1::5432','-e','GAUSSDB_PASSWORD','-e','GAUSSDB_APP_PASSWORD','-e','GAUSSDB_USER',name+':local',env=env)
    deadline = time.monotonic()+240
    while time.monotonic()<deadline:
        data=owned('container',state)
        if not data or not data['State']['Running']:
            raise RuntimeError('GaussDB Business initialization exited')
        health=subprocess.run(['docker','exec',name,'bash','/addp/gaussdb/entrypoint.sh','--health'],capture_output=True,timeout=15)
        if health.returncode==0:
            break
        time.sleep(2)
    else:
        raise RuntimeError('GaussDB Business readiness deadline exceeded')
    password='Ar9@'+secrets.token_hex(12)
    sql(state,"""CREATE ROLE fixture_reader LOGIN PASSWORD '%s';
CREATE SCHEMA addp_acceptance;
CREATE TABLE addp_acceptance.source(id BIGINT, name TEXT, amount NUMERIC(38,18), optional_text TEXT, PRIMARY KEY(name,id));
INSERT INTO addp_acceptance.source VALUES (9007199254740993,'高斯正式接入中文',12345678901234567890.123456789012345678,NULL), (2,'保留尾空格  ',-0.000000000000000001,'');
CREATE TABLE addp_acceptance.hidden(id INT PRIMARY KEY);
CREATE SCHEMA private_schema;
CREATE TABLE private_schema.hidden(id INT PRIMARY KEY);
GRANT USAGE ON SCHEMA addp_acceptance TO fixture_reader;
GRANT SELECT ON addp_acceptance.source TO fixture_reader;
""" % password)
    data=owned('container',state)
    mapping=data['NetworkSettings']['Ports']['5432/tcp']
    if len(mapping)!=1 or mapping[0]['HostIp']!='127.0.0.1':
        raise RuntimeError('fixture requires a random loopback-only port')
    info={'host':'127.0.0.1','port':int(mapping[0]['HostPort']),'database':'business','user':'fixture_reader','password':password,'sslmode':'disable'}
    private(path/'gaussdb-engine.json', {'name':'Hosted GaussDB read-only','engine_type':'gaussdb','engine_origin':'general','description':'Disposable read-only consumer acceptance','connection_info':info})
    print('GAUSSDB_FIXTURE_READY architecture='+architecture,flush=True)


def stop():
    path=directory()
    file=path/'gaussdb-state.json'
    if not file.exists():
        return
    state=json.loads(file.read_text())
    # Fail closed for foreign resources; do not delete anything else.
    for kind, args in [('container',('rm','-fv')),('volume',('volume','rm')),('network',('network','rm')),('image',('image','rm'))]:
        if owned(kind,state) is not None:
            name=state['name']+(':local' if kind=='image' else '')
            run('docker',*args,name,timeout=90)
        if inspect(kind,state['name']+(':local' if kind=='image' else '')) is not None:
            raise RuntimeError('GaussDB fixture residual resource: '+kind)
    private(path/'gaussdb-cleanup.json',{'result':'passed','remaining_resources':0})
    file.unlink()
    (path/'gaussdb-engine.json').unlink(missing_ok=True)
    print('GAUSSDB_FIXTURE_CLEANUP_PASS remaining_resources=0',flush=True)


if __name__=='__main__':
    parser=argparse.ArgumentParser()
    parser.add_argument('action',choices=['start','stop'])
    args=parser.parse_args()
    try:
        globals()[args.action]()
    except (RuntimeError, OSError, subprocess.SubprocessError):
        # Preserve ownership state for the caller's EXIT cleanup, never print secrets.
        raise SystemExit('GaussDB owned fixture '+args.action+' failed; retained ownership state for cleanup') from None
