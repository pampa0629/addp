"""Disposable native Cleanup transport lifecycle; never run against a user deployment.

Configurations and immutable pins remain in the Hosted private directory. The
only Infra change is the new fixture broker's internal advertised listener.
"""
import argparse
from copy import deepcopy
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
CASES = ('manager-success', 'manager-object-unknown', 'manager-index-unknown',
         'transfer-engine', 'transfer-dlq-unknown', 'transfer-capture-unknown')
PROXIES = {'object': ('http', 19000, 'minio', 9000),
           'index': ('http', 17700, 'meilisearch', 7700),
           'capture': ('http', 18083, 'kafka-connect', 8083),
           'dlq': ('kafka', 29092, 'redpanda', 29092)}
PREFIX = 'addp-cleanup-native-'
PROBE = PREFIX + 'kafka-probe'


def require(value, message):
    if not value:
        raise ValueError(message)


def private_write(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as output:
        json.dump(value, output, sort_keys=True)
        output.write('\n')


def advertised_config(config):
    """Keep the actual canonical Compose graph; change one exact listener only."""
    result = deepcopy(config)
    require(result.get('name') == 'addp-infra', 'native fixture requires canonical Infra project')
    command = result['services']['redpanda']['command']
    require(isinstance(command, list) and command.count('--advertise-kafka-addr') == 1,
            'ambiguous Kafka advertised listener')
    position = command.index('--advertise-kafka-addr') + 1
    require(position < len(command) and re.fullmatch(
        r'internal://redpanda:29092,external://localhost:[0-9]+', command[position]),
        'unexpected Kafka listener contract')
    command[position] = command[position].replace('internal://redpanda:29092',
                                                 'internal://cleanup-native-dlq-proxy:29092')
    return result


def published(config, service, target):
    ports = [p for p in config['services'][service].get('ports', []) if p.get('target') == target]
    require(len(ports) == 1 and ports[0].get('host_ip') == '127.0.0.1'
            and str(ports[0].get('published', '')).isdigit(), 'native probe must use a sole loopback listener')
    return '127.0.0.1:' + str(ports[0]['published'])


def pin(row):
    require(re.fullmatch(r'[0-9a-f]{64}', row['Id']) and row['State']['Running'], 'invalid running container')
    return dict(id=row['Id'], name=row['Name'].removeprefix('/'), image=row['Image'],
                started_at=row['State']['StartedAt'])


def verify_pin(row, expected, run_id, *, running=True):
    require(set(expected) == {'id', 'name', 'image', 'started_at'}, 'invalid private container pin')
    require(row['Id'] == expected['id'] and row['Name'] == '/' + expected['name']
            and row['Image'] == expected['image'] and row['State']['StartedAt'] == expected['started_at']
            and (not running or row['State']['Running'])
            and row['Config'].get('Labels', {}).get('com.addp.online-native-run') == run_id,
            'native fixture container identity changed')


class Lifecycle:
    def __init__(self):
        require(sys.platform == 'linux' and os.environ.get('GITHUB_ACTIONS') == 'true'
                and os.environ.get('RUNNER_OS') == 'Linux' and os.environ.get('ADDP_ONLINE_HOSTED') == '1'
                and os.environ.get('ADDP_ONLINE_HOST') == '1' and os.environ.get('POSTGRES_DB') == 'addp_online'
                and os.environ.get('ONLINE_SUITE') == 'cleanup-native-effects', 'exclusive native Hosted scope required')
        self.case, self.run_id = os.environ.get('ADDP_ONLINE_NATIVE_CASE'), os.environ.get('ADDP_ONLINE_TEST_RUN_ID', '')
        require(self.case in CASES and re.fullmatch(r'[A-Za-z0-9_-]{1,100}', self.run_id), 'explicit native case/run required')
        original = Path(os.environ['ADDP_ONLINE_SECRET_DIR'])
        self.secret = original.resolve(strict=True)
        require(not original.is_symlink() and self.secret.parent == Path(os.environ['RUNNER_TEMP']).resolve(strict=True)
                and self.secret.stat().st_mode & 0o777 == 0o700 and self.secret.stat().st_uid == os.getuid()
                and re.fullmatch(r'addp-online-secret-[A-Za-z0-9_.-]+', self.secret.name)
                and not self.secret.is_relative_to(ROOT), 'private native ownership mismatch')
        self.state_file = self.secret / 'native-container-pins.json'
        self.state = {'run_id': self.run_id, 'pins': {}}
        if self.state_file.exists():
            require(not self.state_file.is_symlink() and self.state_file.stat().st_mode & 0o777 == 0o600
                    and self.state_file.stat().st_size <= 8192, 'invalid native recovery state')
            self.state = json.loads(self.state_file.read_text())
            require(set(self.state) == {'run_id', 'pins'} and self.state['run_id'] == self.run_id
                    and set(self.state['pins']).issubset({PREFIX + k + '-proxy' for k in PROXIES} | {PROBE}),
                    'foreign native recovery state')

    def command(self, args, *, timeout=30, input=None):
        result = subprocess.run(args, input=input, capture_output=True, text=True, timeout=timeout)
        require(result.returncode == 0, 'private native lifecycle command failed')
        return result.stdout

    def inspect(self, name):
        rows = json.loads(self.command(['docker', 'inspect', name]))
        require(isinstance(rows, list) and len(rows) == 1, 'ambiguous native container')
        return rows[0]

    def save(self):
        temporary = self.secret / 'native-container-pins.next.json'
        private_write(temporary, self.state)
        temporary.replace(self.state_file)

    def start(self, name, args):
        require(name not in self.state['pins'], 'native container already started')
        self.state['pins'][name] = None  # Durable intent precedes a possibly uncertain Docker response.
        self.save()
        identifier = self.command(['docker', 'run', '-d', '--name', name,
            '--label', 'com.addp.online-native-run=' + self.run_id,
            '--label', 'com.addp.online-fixture=cleanup-native-effects',
            '--label', 'com.addp.online-run=' + self.run_id,
            '--label', 'com.docker.compose.project.working_dir=' + str(ROOT), *args]).strip()
        require(re.fullmatch(r'[0-9a-f]{64}', identifier), 'native Docker run did not return an immutable ID')
        row = self.inspect(identifier)
        expected = pin(row)
        verify_pin(row, expected, self.run_id)
        self.state['pins'][name] = expected
        self.save()

    def prepare(self):
        require(not self.state['pins'], 'native fixture must be new')
        raw = json.loads(self.command(['docker', 'compose', '--project-directory', str(ROOT),
            '-f', str(ROOT / 'docker-compose.infra.yml'), 'config', '--format', 'json']))
        config = advertised_config(raw)
        config_path = self.secret / 'native-infra-compose.json'
        private_write(config_path, config)
        for kind, (protocol, port, upstream, upstream_port) in PROXIES.items():
            config_file = self.secret / ('native-' + kind + '-proxy.json')
            private_write(config_file, dict(run_id=self.run_id, protocol=protocol, listen_port=port,
                                            upstream_host=upstream, upstream_port=upstream_port))
            self.start(PREFIX + kind + '-proxy', ['--network', 'addp-network', '--network-alias',
                'cleanup-native-' + kind + '-proxy', '--user', f'{os.getuid()}:{os.getgid()}',
                '--read-only', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges',
                '-e', 'GITHUB_ACTIONS=true', '-e', 'RUNNER_OS=Linux', '-e', 'ADDP_ONLINE_HOSTED=1',
                '-e', 'ADDP_ONLINE_TEST=1', '-e', 'ADDP_ONLINE_TEST_RUN_ID=' + self.run_id,
                '-v', str(config_file) + ':/fixture/config.json:ro',
                '-v', str(ROOT / 'scripts/test/native-effect-proxy.py') + ':/fixture/native-effect-proxy.py:ro',
                'python:3.12-alpine', 'python', '-B', '/fixture/native-effect-proxy.py', '--config', '/fixture/config.json'])
        # Proxy must already listen when broker health follows internal metadata.
        for kind in PROXIES:
            expected = self.state['pins'][PREFIX + kind + '-proxy']
            ready = False
            for _ in range(30):
                try:
                    self.command(['docker', 'exec', expected['id'], 'python', '-c',
                        "import urllib.request,json; r=urllib.request.Request('http://127.0.0.1:18087/state',data=json.dumps({'run_id': '" + self.run_id + "'}).encode(),headers={'Content-Type':'application/json'}); urllib.request.urlopen(r,timeout=2).read()"])
                    ready = True
                    break
                except (ValueError, subprocess.TimeoutExpired):
                    time.sleep(1)
            require(ready, 'native proxy readiness failed')
        self.command(['docker', 'compose', '--project-directory', str(ROOT), '-f', str(config_path),
                      'up', '-d', '--no-deps', '--force-recreate', '--wait', 'redpanda'], timeout=180)
        self.start(PROBE, ['--network', 'host', '--read-only', '--cap-drop', 'ALL',
            '--security-opt', 'no-new-privileges', '--entrypoint', '/bin/sh',
            config['services']['redpanda']['image'], '-c', 'exec sleep infinity'])

    def manifest(self):
        config = json.loads((self.secret / 'native-infra-compose.json').read_text())
        for name, expected in self.state['pins'].items():
            verify_pin(self.inspect(expected['id']), expected, self.run_id)
        require(len(self.state['pins']) == 5, 'incomplete native transport deployment')
        manager = self.case.startswith('manager-')
        native = dict(minio=dict(endpoint=published(config, 'minio', 9000),
                                access_key=os.environ['MINIO_ROOT_USER'], secret_key=os.environ['MINIO_ROOT_PASSWORD']),
                      meili=dict(url='http://' + published(config, 'meilisearch', 7700),
                                 key=os.environ['MEILISEARCH_MASTER_KEY'],
                                 index=os.environ.get('MEILISEARCH_MANAGER_CONTENT_INDEX', 'manager_content_documents')),
                      connect=dict(url='http://' + published(config, 'kafka-connect', 8083),
                                   username=os.environ.get('KAFKA_CONNECT_USERNAME', ''),
                                   password=os.environ.get('KAFKA_CONNECT_PASSWORD', '')),
                      kafka=dict(container=self.state['pins'][PROBE], brokers=published(config, 'redpanda', 9092),
                                 username=os.environ['INFRA_KAFKA_ADMIN_USERNAME'], password=os.environ['INFRA_KAFKA_ADMIN_PASSWORD']))
        if manager:
            native['source_minio'] = dict(endpoint='127.0.0.1:' + os.environ['ADDP_ONLINE_MANAGER_MINIO_PORT'],
                access_key=os.environ['ADDP_ONLINE_MANAGER_MINIO_ACCESS_KEY'], secret_key=os.environ['ADDP_ONLINE_MANAGER_MINIO_SECRET_KEY'])
        else:
            row = self.inspect('addp-continuous-pg-online-disposable')
            labels = row['Config']['Labels']
            require(labels.get('com.addp.online-fixture') == 'continuous'
                    and labels.get('com.addp.online-run') == self.secret.name, 'foreign business source')
            native['pg_container'] = pin(row)
        descriptors = {'minio': 'manager-engine.json'} if manager else {'pg': 'continuous-pg.json', 'kafka': 'continuous-kafka.json'}
        for filename in descriptors.values():
            path = self.secret / filename
            require(not path.is_symlink() and path.is_file() and path.stat().st_mode & 0o777 == 0o600, 'missing private source descriptor')
        private_write(self.secret / 'cleanup-native-fixture.json',
            dict(schema='addp.cleanup-native-fixture/v1', run_id=self.run_id, case=self.case,
                 descriptors=descriptors, source_bucket='addp-online',
                 proxies={kind: self.state['pins'][PREFIX + kind + '-proxy'] for kind in PROXIES}, native=native))

    def stop(self):
        failed = False
        for name, expected in list(self.state['pins'].items()):
            try:
                if expected is None:
                    found = subprocess.run(['docker', 'container', 'inspect', name], capture_output=True, text=True, timeout=10)
                    if found.returncode != 0:
                        require(found.returncode == 1 and 'No such container' in found.stderr, 'unverifiable native start intent')
                        self.state['pins'].pop(name); self.save()
                        continue
                    rows = json.loads(found.stdout)
                    require(len(rows) == 1, 'ambiguous native start intent')
                    row = rows[0]
                    require(row['Name'] == '/' + name and row['Config'].get('Labels', {}).get('com.addp.online-native-run') == self.run_id
                            and row['Config']['Labels'].get('com.docker.compose.project.working_dir') == str(ROOT), 'foreign native start intent')
                    expected = dict(id=row['Id'], name=name, image=row['Image'], started_at=row['State']['StartedAt'])
                else:
                    row = self.inspect(expected['id'])
                verify_pin(row, expected, self.run_id, running=False)
                self.command(['docker', 'rm', '-fv', expected['id']])
                self.state['pins'].pop(name)
                self.save()
            except (ValueError, KeyError, OSError, subprocess.SubprocessError):
                failed = True
        # A substituted name is never removed, but makes destruction fail.
        for name in {PREFIX + k + '-proxy' for k in PROXIES} | {PROBE}:
            result = subprocess.run(['docker', 'container', 'inspect', name], capture_output=True, timeout=10)
            if result.returncode != 1 or b'No such container' not in result.stderr:
                failed = True
        require(not failed, 'native destruction or zero-residual verification failed')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=('prepare', 'manifest', 'stop'))
    args = parser.parse_args()
    try:
        getattr(Lifecycle(), args.action)()
    except (ValueError, KeyError, OSError, subprocess.SubprocessError, json.JSONDecodeError) as error:
        print('Native fixture lifecycle failed: ' + type(error).__name__, file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
