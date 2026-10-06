"""Explicit native-Linux node metrics lifecycle; never called by center startup."""
import argparse
import ipaddress
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
PROJECT = 'addp-node-metrics'


def deployment(env):
    selected = env.get('ADDP_NODE_METRICS_ENABLED', 'false')
    if selected not in ('true', 'false'):
        raise ValueError('ADDP_NODE_METRICS_ENABLED must be true or false')
    if selected == 'false':
        return None
    directory = Path(env.get('ADDP_NODE_METRICS_TLS_DIR', ''))
    names = {'ca.crt', 'server.crt', 'server.key'}
    if (not directory.is_absolute() or not directory.is_dir()
            or directory.resolve().is_relative_to(ROOT)):
        raise ValueError('Node metrics TLS directory must be absolute and outside repository')
    if {p.name for p in directory.iterdir()} != names:
        raise ValueError('Node metrics TLS directory must contain only its three source files')
    for name in names:
        path = directory / name
        if (path.is_symlink() or not path.is_file() or not os.access(path, os.R_OK)
                or not 0 < path.stat().st_size <= 1 << 20):
            raise ValueError('Missing or invalid node metrics certificate file')
    listen = env.get('ADDP_NODE_METRICS_LISTEN', '')
    match = re.fullmatch(r'(\[[0-9a-f:]+\]|[0-9.]+):([1-9][0-9]{0,4})', listen)
    if not match:
        raise ValueError('Node metrics listen address requires canonical IP and explicit port')
    address = ipaddress.ip_address(match[1].strip('[]'))
    port = int(match[2])
    authority = f'[{address}]:{port}' if address.version == 6 else f'{address}:{port}'
    if (listen != authority or port > 65535 or address.is_unspecified
            or address.is_link_local or address.is_multicast):
        raise ValueError('Node metrics listen address is forbidden or noncanonical')
    return directory


def native_host(system, kernel, endpoint, docker_os):
    version = re.match(r'^(\d+)\.(\d+)', kernel)
    if (system != 'Linux' or not version or tuple(map(int, version.groups())) < (5, 12)
            or not endpoint.startswith('unix:///') or 'desktop' in docker_os.lower()):
        raise ValueError('Node metrics requires native Linux 5.12+ and a local Docker Engine')


def docker_json(*args):
    return json.loads(subprocess.check_output(['docker', *args], text=True, stderr=subprocess.DEVNULL))


def run(action, env):
    if action == 'up' and deployment(env) is None:
        print('Node metrics: Disabled')
        return
    # Never operate a remote Docker host, including for status/down.
    endpoint = None if env.get('DOCKER_CONTEXT') else env.get('DOCKER_HOST')
    if not endpoint:
        context = docker_json('context', 'inspect')
        endpoint = context[0]['Endpoints']['docker']['Host']
    if not endpoint.startswith('unix:///'):
        raise ValueError('Node metrics lifecycle requires a local Docker endpoint')
    if action == 'up':
        info = docker_json('info', '--format', '{{json .}}')
        native_host(platform.system(), info['KernelVersion'], endpoint, info['OperatingSystem'])
        if info['OSType'] != 'linux' or info['KernelVersion'] != platform.release():
            raise ValueError('Node metrics Docker Engine must share the current Linux kernel')
    ids = subprocess.check_output(['docker', 'ps', '-aq', '--filter',
                                  'label=com.docker.compose.project=' + PROJECT], text=True).split()
    if ids:
        for item in docker_json('inspect', *ids):
            if item['Config']['Labels'].get('io.addp.node-metrics.owner') != str(ROOT):
                raise ValueError('Node metrics project belongs to another workspace')
    values = dict(env, ADDP_NODE_METRICS_OWNER=str(ROOT), ADDP_NODE_METRICS_ROOTFS='/host',
                  ADDP_NODE_METRICS_PROCFS='/host/proc', ADDP_NODE_METRICS_SYSFS='/host/sys')
    # Do not inherit arbitrary profile activation or source paths from the caller.
    values.pop('COMPOSE_PROFILES', None)
    args = ['docker', 'compose', '--env-file', '/dev/null', '-p', PROJECT,
            '-f', str(ROOT / 'scripts/infra/node-metrics.yml'),
            '-f', str(ROOT / 'scripts/infra/node-metrics-linux.yml')]
    commands = {'up': ['up', '-d', '--force-recreate', '--wait', '--wait-timeout', '30', 'node-exporter'],
                'down': ['down'], 'status': ['ps']}
    subprocess.run(args + commands[action], env=values, check=True)
    if action == 'up':
        print('Node metrics: deployed; source readiness and Monitor admission require mTLS verification')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('up', 'down', 'status'))
    args = parser.parse_args()
    try:
        run(args.action, os.environ)
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError):
        sys.exit('Node metrics deployment rejected; check native host, explicit inputs and project ownership')
