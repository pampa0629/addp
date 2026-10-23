"""Owned MinIO objects for the existing exact-model Hosted acceptance suite."""
from contextlib import contextmanager
from importlib import import_module
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import time
import urllib.request

SEED = import_module('business.ftp.seed')
IMAGE = 'addp-minio:RELEASE.2025-10-15T17-29-55Z'
NAME = 'addp-ftp-model-source'
BUCKET = 'model-source'
SUITE = 'ftp-consumer-flow'


def guard():
    run = os.environ.get('ADDP_ONLINE_TEST_RUN_ID', '')
    root = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
    if (os.environ.get('ADDP_ONLINE_HOSTED') != '1' or os.environ.get('GITHUB_ACTIONS') != 'true'
            or not re.fullmatch(r'ftp-\d+-\d+', run) or root.is_symlink() or not root.is_dir()
            or root.stat().st_mode & 0o077):
        raise RuntimeError('MinIO model fixture requires the private Hosted owner')
    return root, run


def docker(*args, data=None, env=None):
    result = subprocess.run(['docker', *args], input=data, capture_output=True, timeout=90, env=env)
    if result.returncode:
        raise RuntimeError('owned MinIO fixture Docker operation failed')
    return result.stdout


def owned():
    root, run = guard()
    result = docker('ps', '-aq', '--filter', 'name=^/' + NAME + '$').strip()
    if not result:
        return None
    facts, = json.loads(docker('inspect', NAME))
    labels = facts['Config'].get('Labels', {})
    if labels.get('com.addp.online-fixture') != SUITE or labels.get('com.addp.online-run-id') != run:
        raise RuntimeError('refusing a foreign MinIO fixture')
    return facts


def mc(*args, data=None):
    root, _ = guard()
    if owned() is None:
        raise RuntimeError('owned MinIO source is absent')
    entry = json.loads((root / 'ftp-minio-engine.json').read_text())['connection_info']
    env = dict(os.environ, MC_HOST_fixture=f"http://{entry['access_key']}:{entry['secret_key']}@127.0.0.1:9000")
    return docker('exec', '-i', '-e', 'MC_HOST_fixture', NAME, 'mc', *args, data=data, env=env)


def read(key):
    return mc('cat', f'fixture/{BUCKET}/{key}')


def put(key, data):
    mc('pipe', f'fixture/{BUCKET}/{key}', data=data)


def start():
    root, run = guard()
    if owned() is not None:
        raise RuntimeError('refusing to reuse MinIO source')
    user, password = 'model-source', secrets.token_hex(32)
    envfile = root / 'model-source.env'
    with os.fdopen(os.open(envfile, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), 'w') as stream:
        stream.write(f'MINIO_ROOT_USER={user}\nMINIO_ROOT_PASSWORD={password}\n')
    docker('run', '-d', '--name', NAME, '--label', f'com.addp.online-fixture={SUITE}',
           '--label', f'com.addp.online-run-id={run}', '--env-file', str(envfile),
           '--mount', 'type=tmpfs,destination=/data', '-p', '127.0.0.1::9000', IMAGE, 'server', '/data')
    facts = owned()
    port, = facts['NetworkSettings']['Ports']['9000/tcp']
    if port['HostIp'] != '127.0.0.1':
        raise RuntimeError('source port must bind loopback')
    endpoint = '127.0.0.1:' + port['HostPort']
    descriptor = {'name':'Hosted MinIO', 'engine_type':'minio', 'engine_origin':'general', 'connection_info':{
        'endpoint':endpoint, 'access_key':user, 'secret_key':password, 'use_ssl':False},
        'description':'Disposable exact-object model source'}
    with os.fdopen(os.open(root / 'ftp-minio-engine.json', os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600), 'w') as stream:
        json.dump(descriptor, stream)
    deadline = time.monotonic() + 60
    while True:
        try:
            with urllib.request.urlopen('http://' + endpoint + '/minio/health/ready', timeout=3) as response:
                if response.status == 200:
                    break
        except OSError:
            pass
        if time.monotonic() >= deadline:
            raise RuntimeError('MinIO source readiness failed')
        time.sleep(1)
    mc('mb', f'fixture/{BUCKET}')
    directory = root / 'minio-seed'
    directory.mkdir(mode=0o700)
    SEED.write_samples(directory)
    samples = [{'format':file.suffix[1:],'full_name':'examples/'+file.name,'size':file.stat().st_size,
                'sha256':hashlib.sha256(file.read_bytes()).hexdigest()} for file in sorted((directory/'examples').iterdir())]
    models = SEED.write_model_samples(directory)
    for manifest in (samples, models):
        for sample in manifest:
            key = sample['full_name']
            data = (directory / key).read_bytes()
            put(key, data)
            if read(key) != data:
                raise RuntimeError('MinIO source bytes differ')
            sample['full_name'] = BUCKET + '/' + key
    (root / 'minio-samples.json').write_text(json.dumps(samples))
    (root / 'minio-model-samples.json').write_text(json.dumps(models))


def stop():
    root, _ = guard()
    if owned() is not None:
        docker('rm', '-f', '-v', NAME)
    if owned() is not None:
        raise RuntimeError('MinIO model fixture residue')
    (root / 'minio-cleanup.json').write_text(json.dumps({'container_removed':True, 'persistent_volumes':0}))


@contextmanager
def changed_texture(model):
    import copy
    root, _ = guard()
    fixture = Path(__file__).resolve().parents[2] / 'business/fixtures/manager'
    kind = model['format']
    if kind not in {'3ds', 'dae'}:
        raise RuntimeError('composition mutation requires a fixed declared texture model')
    key = f'models/{kind}/model.{kind}'
    texture = f'models/{kind}/texture.png'
    replacement_name = 'changed.png' if kind == 'dae' else ' tex.png   '
    replacement = f'models/{kind}/{replacement_name}'
    original = (fixture / f'addp_online_model_fixture.{kind}').read_bytes()
    png = (fixture / 'addp_online_exchange_texture.png').read_bytes()
    if model['full_name'] != BUCKET+'/'+key or read(key)!=original or read(texture)!=png:
        raise RuntimeError('MinIO mutation requires the fixed unchanged fixture')
    listing = mc('ls', '--json', f'fixture/{BUCKET}/models/{kind}/').decode().splitlines()
    if any(json.loads(line).get('key') == replacement_name for line in listing):
        raise RuntimeError('replacement object already exists')
    changed = original.replace(b'texture.png', replacement_name.encode())
    try:
        put(replacement, png)
        put(key, changed)
        projected = copy.deepcopy(model)
        projected.update(sha256=hashlib.sha256(changed).hexdigest(), size=len(changed))
        for ref in projected['composition']:
            if ref['role'] == 'texture':
                ref['path'] = BUCKET+'/'+replacement
        yield projected
    finally:
        if read(key) not in (original, changed) or read(replacement)!=png:
            raise RuntimeError('MinIO mutation cleanup refused changed objects')
        put(key, original)
        mc('rm', f'fixture/{BUCKET}/{replacement}')
        if read(key)!=original:
            raise RuntimeError('MinIO restore bytes differ')


@contextmanager
def invalid_ifc(model):
    key = 'models/ifc/model.ifc'
    fixture = Path(__file__).resolve().parents[2]/'business/fixtures/manager/addp_online_model_fixture.ifc'
    original = fixture.read_bytes()
    if model['full_name'] != BUCKET+'/'+key or read(key)!=original:
        raise RuntimeError('IFC mutation requires unchanged owned source')
    invalid = b'INVALID_IFC_SOURCE\n'.ljust(len(original), b' ')
    try:
        put(key, invalid)
        yield
    finally:
        if read(key) not in (original,invalid):
            raise RuntimeError('refusing to replace changed IFC object')
        put(key,original)


if __name__ == '__main__':
    import sys
    if len(sys.argv)!=2 or sys.argv[1] not in {'start','stop'}:
        raise SystemExit('requires start or stop')
    globals()[sys.argv[1]]()
