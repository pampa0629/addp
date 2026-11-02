"""Real native MinIO account lifecycle and denial proof in owned T2 containers."""
import importlib.util
from datetime import datetime, timezone
import hashlib
import hmac
import json
import os
from pathlib import Path
import re
import subprocess
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('minio_observation', ROOT/'scripts/infra/minio-observation.py')
M = importlib.util.module_from_spec(spec); spec.loader.exec_module(M)
if os.environ.get('ADDP_MINIO_OBSERVATION_T2') != '1' or re.fullmatch('addp-minio-observation-t2-[a-f0-9]{12}', os.environ.get('T2_MINIO_PROJECT', '')) is None:
    raise SystemExit('MinIO lifecycle proof requires the standard isolated gate')
docker = M.deployment.Docker(os.environ)
project = os.environ['T2_MINIO_PROJECT']
working = ROOT/'scripts/test'
state_root = Path(os.environ['T2_MINIO_STATE'])


def database(scope):
    prefix = 'T2_MINIO_'+scope.upper()
    return M.Database(docker, project, working, os.environ[prefix+'_USER'], os.environ[prefix+'_PASSWORD'], service='minio-'+scope)


def assert_denied(db, state, command):
    try:
        db.mc(command, observer=state)
    except M.PermissionDenied:
        return
    except M.Error as error:
        raise AssertionError('denial could not be verified for '+command[0]) from error
    raise AssertionError('expected native authorization denial')


def s3(db, access, secret, method, path):
    """Signed real object API; validate native AccessDenied, not a CLI preflight."""
    now = datetime.now(timezone.utc)
    date, stamp = now.strftime('%Y%m%d'), now.strftime('%Y%m%dT%H%M%SZ')
    payload = hashlib.sha256(b'').hexdigest()
    headers = 'host:127.0.0.1:9000\nx-amz-content-sha256:'+payload+'\nx-amz-date:'+stamp+'\n'
    signed = 'host;x-amz-content-sha256;x-amz-date'
    canonical = '\n'.join((method, path, '', headers, signed, payload))
    scope = date+'/us-east-1/s3/aws4_request'
    message = 'AWS4-HMAC-SHA256\n'+stamp+'\n'+scope+'\n'+hashlib.sha256(canonical.encode()).hexdigest()
    key = ('AWS4'+secret).encode()
    for part in (date, 'us-east-1', 's3', 'aws4_request'):
        key = hmac.new(key, part.encode(), hashlib.sha256).digest()
    signature = hmac.new(key, message.encode(), hashlib.sha256).hexdigest()
    auth = 'AWS4-HMAC-SHA256 Credential='+access+'/'+scope+', SignedHeaders='+signed+', Signature='+signature
    config = 'url = "http://127.0.0.1:9000'+path+'"\nrequest = "'+method+'"\n'
    for header in ('Authorization: '+auth, 'x-amz-content-sha256: '+payload, 'x-amz-date: '+stamp):
        config += 'header = '+json.dumps(header)+'\n'
    if method == 'PUT': config += 'data = ""\n'
    db.same()
    raw = docker.run(['exec', '-i', db.identity, 'curl', '--silent', '--show-error', '--max-time', '5',
                      '--write-out', '\n%{http_code}', '--config', '-'], input=config)
    db.same()
    body, status = raw.rsplit('\n', 1)
    code = ET.fromstring(body).findtext('Code') if status != '200' else None
    return int(status), code


states = {}
for scope in ('infra', 'business'):
    manager = M.Lifecycle(scope, state_root/scope, lambda: database(scope))
    db = database(scope)
    db.mc(['mb', 'owned/permission-fixture'])
    db.mc(['cp', '/etc/hostname', 'owned/permission-fixture/fixture'])
    assert s3(db, db.parent, db.config['aliases']['owned']['secretKey'], 'GET', '/permission-fixture/fixture') == (200, None)
    assert s3(db, db.parent, db.config['aliases']['owned']['secretKey'], 'PUT', '/permission-fixture/writer-proof') == (200, None)
    manager.run(True)
    state = manager.read(); states[scope] = state
    invalid = {'root_access': db.parent, 'root_secret': db.config['aliases']['owned']['secretKey'],
               'access_key': state['access_key'], 'enabled': False, 'policy': {'Action': ['admin:*']}}
    result = docker.run(['exec', '-i', db.identity, 'minio-observation-status'], input=json.dumps(invalid), check=False)
    assert result.returncode == 1 and not result.stdout and result.stderr.strip() == 'MinIO observation status update rejected'
    M.Lifecycle.check(db.info(state['access_key']), state)
    assert db.info(state['access_key'])['accountStatus'] == 'on'
    token = M.jwt(state['access_key'], state['secret_key'])
    assert db.probe(token) == 200
    assert db.probe(M.jwt(state['access_key'], state['secret_key'], expired=True)) == 403
    assert db.probe(M.jwt(state['access_key'], 'wrong-secret')) == 403
    for command in [
        ['ls', 'owned/permission-fixture'],
        ['stat', 'owned/permission-fixture/fixture'],
        ['admin', 'user', 'add', 'owned', 'forbidden-user', 'forbidden-password'],
        ['admin', 'user', 'ls', 'owned'],
    ]:
        assert_denied(db, state, command)
    for method, path in [('GET', '/permission-fixture/fixture'), ('PUT', '/permission-fixture/forbidden-object-write')]:
        assert s3(db, state['access_key'], state['secret_key'], method, path) == (403, 'AccessDenied'), 'native S3 authorization was not denied'
    before = (state_root/scope/'owner.json').stat().st_mtime_ns
    manager.run(True)
    assert manager.read() == state
    assert (state_root/scope/'owner.json').stat().st_mtime_ns == before
    manager.run(False)
    assert db.probe(token) == 403
    manager.run(True)
    assert manager.read()['secret_key'] == state['secret_key']
    print('MinIO '+scope+': native metrics allowed; S3/admin denied; expiry/disable/idempotency PASS')

infra, business = states['infra'], states['business']
assert database('business').probe(M.jwt(infra['access_key'], infra['secret_key'])) == 403
assert database('infra').probe(M.jwt(business['access_key'], business['secret_key'])) == 403
M.Lifecycle('infra', state_root/'infra', lambda: database('infra')).run(False)
assert database('business').probe(M.jwt(business['access_key'], business['secret_key'])) == 200
M.Lifecycle('infra', state_root/'infra', lambda: database('infra')).run(True)
compose = ['docker', 'compose', '--project-name', project, '--project-directory', str(working), '-f', str(working/'docker-compose.minio-observation-t2.yml')]
# Replace only these two disposable containers; preserve their owned data volumes.
subprocess.run([*compose, 'up', '-d', '--force-recreate', '--wait', '--wait-timeout', '90'], check=True, stdout=subprocess.DEVNULL)
for scope, previous in states.items():
    manager = M.Lifecycle(scope, state_root/scope, lambda: database(scope))
    manager.run(True)
    assert manager.read() == previous
    manager.run(False)
    assert database(scope).probe(M.jwt(previous['access_key'], previous['secret_key'])) == 403
    assert not docker.run(['exec', database(scope).identity, 'sh', '-c', 'find /tmp -maxdepth 1 -name "addp-minio-observation.*" -print']).strip()
print('MinIO: independent scopes and native account persistence across container replacement PASS')
