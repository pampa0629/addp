#!/usr/bin/env python3
"""Deployment-owned, metrics-only MinIO Access Key lifecycle; no object access."""
import argparse
import base64
import hashlib
import hmac
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import secrets
import sys
import tarfile
import time

ROOT = Path(__file__).resolve().parents[2]


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


private = load('minio_observation_private', ROOT/'scripts/utils/private-state.py')
deployment = load('minio_observation_docker', ROOT/'scripts/utils/deployment-docker.py')
Error = deployment.DeploymentError
RELEASE = 'RELEASE.2025-10-15T17-29-55Z'
POLICY = json.loads((ROOT/'scripts/infra/minio-observation/policy.json').read_text())
# The pinned native policy parser expands the submitted wildcard on persistence.
NATIVE_POLICY = {'Version': '2012-10-17', 'Statement': [{'Effect': 'Allow', 'Action': ['admin:Prometheus'], 'Resource': ['**']}]}
METRICS_PATH = '/minio/metrics/v3/system/process'


class PermissionDenied(Error):
    """Native server authorization denial, distinct from transport failure."""


def unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise Error('Duplicate observation field')
        result[key] = value
    return result


def decode(text):
    return json.loads(text, object_pairs_hook=unique)


def jwt(access, secret, now=None, *, expired=False):
    now = int(time.time() if now is None else now)
    encode = lambda value: base64.urlsafe_b64encode(value).rstrip(b'=')
    body = encode(b'{"alg":"HS512","typ":"JWT"}') + b'.' + encode(json.dumps(
        {'iss': 'prometheus', 'sub': access, 'exp': now - 60 if expired else now + 300}, separators=(',', ':')).encode())
    return (body + b'.' + encode(hmac.new(secret.encode(), body, hashlib.sha512).digest())).decode()


def state_directory(env, scope):
    value = env.get('ADDP_MINIO_METRICS_STATE_DIR', '')
    parent = Path(value) if value else Path.home().resolve()/'.local/state/addp'/hashlib.sha256(str(ROOT).encode()).hexdigest()[:16]/'minio-observation'
    if not parent.is_absolute() or '..' in parent.parts or parent.resolve().is_relative_to(ROOT):
        raise Error('MinIO observation state must be outside the workspace')
    return parent/scope


class Database:
    def __init__(self, docker, project, working, access, secret, *, service='minio'):
        self.docker, self.parent = docker, access
        if not all(isinstance(v, str) and 3 <= len(v) <= 512 and all(32 <= ord(c) < 127 for c in v) for v in (access, secret)):
            raise Error('MinIO deployment credential rejected')
        self.config = {'version': '10', 'aliases': {'owned': {'url': 'http://127.0.0.1:9000', 'accessKey': access, 'secretKey': secret, 'api': 's3v4', 'path': 'auto'}}}
        ids = docker.run(['ps', '-aq', '--no-trunc', '--filter', 'label=com.docker.compose.project='+project,
                          '--filter', 'label=com.docker.compose.service='+service]).split()
        if len(ids) != 1 or not deployment.inventory.identifier(ids[0]):
            raise Error('MinIO deployment is not uniquely available')
        self.identity = ids[0]
        self.facts = docker.fact(self.identity)
        labels = self.facts['labels']
        if (not self.facts['running'] or labels.get('com.docker.compose.project') != project
                or labels.get('com.docker.compose.service') != service
                or labels.get('com.docker.compose.project.working_dir') != str(Path(working).resolve())
                or labels.get('com.docker.compose.oneoff', '').lower() != 'false'):
            raise Error('MinIO deployment ownership rejected')
        mounts = [m for m in self.facts['mounts'] if m.get('Destination') == '/data']
        if len(mounts) != 1 or mounts[0].get('Type') != 'volume' or mounts[0].get('RW') is not True:
            raise Error('MinIO observation requires an owned named data volume')
        self.volume = mounts[0]['Name']
        volume = decode(docker.run(['volume', 'inspect', self.volume]))[0]
        if (volume.get('Driver') != 'local' or (volume.get('Labels') or {}).get('com.docker.compose.project') != project
                or not isinstance(volume.get('CreatedAt'), str) or not volume['CreatedAt']):
            raise Error('MinIO data volume ownership rejected')
        self.key = {'engine_id': docker.info['id'], 'project': project, 'working_dir': str(Path(working).resolve()),
                    'service': service, 'volume': self.volume, 'volume_created_at': volume['CreatedAt']}
        if RELEASE not in docker.run(['exec', self.identity, 'minio', '--version']):
            raise Error('MinIO observation version rejected')
        if docker.run(['exec', self.identity, 'minio-observation-status', '--version']).strip() != 'addp.minio-observation-status/v1':
            raise Error('MinIO observation status tool unavailable')

    def same(self):
        if self.docker.fact(self.identity) != self.facts:
            raise Error('MinIO deployment changed during observation management')

    def mc(self, args, *, observer=None, missing=False):
        self.same()
        config = self.config
        if observer is not None:
            config = json.loads(json.dumps(self.config))
            config['aliases']['owned'].update(accessKey=observer['access_key'], secretKey=observer['secret_key'])
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode='w') as archive:
            for name, data in {'config.json': json.dumps(config), 'policy.json': json.dumps(POLICY)}.items():
                data = data.encode(); info = tarfile.TarInfo(name); info.size = len(data); info.mode = 0o600
                archive.addfile(info, io.BytesIO(data))
        # Private stdin archive and transient directory; secrets never enter argv.
        script = '''set -eu
umask 077
d=$(mktemp -d /tmp/addp-minio-observation.XXXXXX)
trap 'rm -rf "$d"' EXIT
base64 -d | tar -x -C "$d"
case "$1" in
 create) shift; mc --json --config-dir "$d" admin user svcacct add owned "$@" --policy "$d/policy.json" ;;
 *) mc --json --config-dir "$d" "$@" ;;
esac'''
        result = self.docker.run(['exec', '-i', self.identity, 'sh', '-c', script, 'minio-observation', *args],
                                 input=base64.b64encode(stream.getvalue()).decode(), check=False)
        self.same()
        try:
            values = [decode(line) for line in result.stdout.splitlines() if line.strip()]
            if not values or not all(isinstance(value, dict) for value in values):
                raise ValueError('invalid response')
        except (ValueError, TypeError):
            raise Error('MinIO management response rejected') from None
        if result.returncode or any(value.get('status') != 'success' for value in values):
            if missing and len(values) == 1 and self._missing(values[0]):
                return None
            if any(self._denied(value) for value in values):
                raise PermissionDenied('MinIO native authorization denied')
            raise Error('MinIO management request rejected')
        return values[-1]

    @staticmethod
    def _missing(value):
        if isinstance(value, dict):
            if value.get('Code') == 'XMinioInvalidIAMCredentials' and value.get('Message') == 'The specified service account is not found (Specified service account does not exist)':
                return True
            return any(Database._missing(v) for v in value.values())
        return False

    @staticmethod
    def _denied(value):
        if isinstance(value, dict):
            if value.get('Code') in {'AccessDenied', 'XMinioAdminAccessDenied'}:
                return True
            return any(Database._denied(v) for v in value.values())
        return False

    def info(self, key):
        return self.mc(['admin', 'user', 'svcacct', 'info', 'owned', key], missing=True)

    def create(self, state):
        return self.mc(['create', self.parent, '--access-key', state['access_key'], '--name', 'addp-'+state['scope']+'-observer', '--description', state['owner']])

    def enable(self, state, enabled):
        self.same()
        alias = self.config['aliases']['owned']
        result = self.docker.run(['exec', '-i', self.identity, 'minio-observation-status'], input=json.dumps(
            {'root_access': alias['accessKey'], 'root_secret': alias['secretKey'], 'access_key': state['access_key'], 'enabled': enabled}))
        self.same()
        if decode(result) != {'status': 'success'}:
            raise Error('MinIO observation status response rejected')

    def probe(self, token):
        self.same()
        config = 'url = "http://127.0.0.1:9000'+METRICS_PATH+'"\nheader = "Authorization: Bearer '+token+'"\n'
        result = self.docker.run(['exec', '-i', self.identity, 'curl', '--silent', '--show-error', '--max-time', '5',
                                 '--output', '/dev/null', '--write-out', '%{http_code}', '--config', '-'], input=config)
        self.same()
        if re.fullmatch('[0-9]{3}', result) is None:
            raise Error('MinIO metrics authentication response rejected')
        return int(result)


class Lifecycle:
    def __init__(self, scope, directory, factory):
        self.scope, self.directory, self.factory = scope, Path(directory), factory

    def write(self, state):
        path = self.directory/'owner.json'
        previous = private.read(path, 0o600)[0] if os.path.lexists(path) else None
        private.atomic(path, json.dumps(state, sort_keys=True), 0o600, previous=previous)

    def read(self):
        path = self.directory/'owner.json'
        if not os.path.lexists(path):
            return None
        value = decode(private.read(path, 0o600)[0])
        fields = {'schema', 'scope', 'owner', 'access_key', 'secret_key', 'source_key', 'parent_user', 'phase'}
        if (set(value) != fields or value['schema'] != 'addp.minio-observation/v1' or value['scope'] != self.scope
                or re.fullmatch('[a-f0-9]{32}', value['owner']) is None or re.fullmatch('ADDP[A-F0-9]{16}', value['access_key']) is None
                or value['phase'] not in {'prepared', 'enabled', 'disabled'} or not isinstance(value['secret_key'], str)
                or value['phase'] != 'prepared' and not 8 <= len(value['secret_key']) <= 128):
            raise Error('MinIO private account state rejected')
        return value

    @staticmethod
    def check(info, state):
        if not isinstance(info, dict):
            raise Error('MinIO observation account is unavailable')
        checks = {'accessKey': info.get('accessKey') == state['access_key'],
                  'parentUser': info.get('parentUser') == state['parent_user'],
                  'description': info.get('description') == state['owner'],
                  'name': info.get('name') == 'addp-'+state['scope']+'-observer',
                  'impliedPolicy': info.get('impliedPolicy', False) is False,
                  'policy': info.get('policy') == NATIVE_POLICY,
                  'expiration': info.get('expiration') is None,
                  'accountStatus': info.get('accountStatus') in {'on', 'off'}}
        failures = [key for key, valid in checks.items() if not valid]
        if failures:
            raise Error('MinIO observation account ownership or policy drift: '+','.join(failures))

    def stop(self, db, state):
        self.check(db.info(state['access_key']), state)
        db.enable(state, False)
        info = db.info(state['access_key']); self.check(info, state)
        if info['accountStatus'] != 'off':
            raise Error('MinIO observation identity did not stop')
        if not state['secret_key']:
            raise Error('MinIO observation credentials were not committed')
        if db.probe(jwt(state['access_key'], state['secret_key'])) != 403:
            raise Error('Disabled MinIO observation identity remained authorized')
        state['phase'] = 'disabled'; self.write(state)

    def run(self, enabled):
        with private.locked(self.directory):
            state, db = self.read(), self.factory()
            if state is None:
                if not enabled:
                    return
                state = {'schema': 'addp.minio-observation/v1', 'scope': self.scope, 'owner': secrets.token_hex(16),
                         'access_key': 'ADDP'+secrets.token_hex(8).upper(), 'secret_key': '', 'source_key': db.key,
                         'parent_user': db.parent, 'phase': 'prepared'}
                if db.info(state['access_key']) is not None:
                    raise Error('MinIO observation account is occupied')
                self.write(state)
                try:
                    created = db.create(state)
                    if created.get('accessKey') != state['access_key'] or not isinstance(created.get('secretKey'), str) or not 8 <= len(created['secretKey']) <= 128:
                        raise Error('MinIO created credential rejected')
                    state['secret_key'] = created['secretKey']
                    self.write(state)
                except Exception:
                    info = db.info(state['access_key'])
                    if info is not None:
                        self.check(info, state); db.enable(state, False)
                    raise
            if state['source_key'] != db.key or state['parent_user'] != db.parent:
                raise Error('MinIO observation source identity changed')
            info = db.info(state['access_key']); self.check(info, state)
            if enabled and state['phase'] == 'enabled' and info['accountStatus'] == 'on':
                try:
                    if db.probe(jwt(state['access_key'], state['secret_key'])) != 200:
                        raise Error('MinIO observation identity no longer ready')
                except Exception:
                    self.stop(db, state)
                    raise
                return
            self.stop(db, state)
            if not enabled:
                return
            try:
                db.enable(state, True)
                info = db.info(state['access_key']); self.check(info, state)
                if info['accountStatus'] != 'on' or db.probe(jwt(state['access_key'], state['secret_key'])) != 200:
                    raise Error('MinIO observation identity did not become ready')
                state['phase'] = 'enabled'; self.write(state)
            except Exception:
                self.stop(db, state)
                raise


def run(action, scope, env):
    selected = env.get('ADDP_MINIO_METRICS_ENABLED', 'false')
    if selected not in {'true', 'false'}:
        raise Error('MinIO observation selection must be true or false')
    directory = state_directory(env, scope)
    enabled = action == 'up' and selected == 'true'
    if not enabled and not os.path.lexists(directory/'owner.json'):
        print('MinIO observation '+scope+': not installed; disabled')
        return
    docker = deployment.Docker(env)
    project, working = deployment.inventory.SCOPES[scope]
    factory = lambda: Database(docker, project, working, env.get('MINIO_ROOT_USER', ''), env.get('MINIO_ROOT_PASSWORD', ''))
    Lifecycle(scope, directory, factory).run(enabled)
    print('MinIO observation '+scope+(': account ready; metrics source not yet deployed' if enabled else ': account disabled'))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['up', 'down'])
    parser.add_argument('--scope', choices=['infra', 'business'], required=True)
    args = parser.parse_args()
    try:
        run(args.action, args.scope, os.environ)
    except (ValueError, OSError, KeyError, TypeError):
        print('MinIO observation account unavailable; core services remain independent', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
