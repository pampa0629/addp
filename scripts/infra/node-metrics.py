"""Explicit Linux node / Docker Desktop VM metrics; independent of the center."""
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


def deployment_runtime(system, kernel, endpoint, docker_os):
    version = re.match(r'^(\d+)\.(\d+)', kernel)
    if (not version or tuple(map(int, version.groups())) < (5, 12)
            or not endpoint.startswith('unix:///')):
        raise ValueError('Node metrics requires Linux 5.12+ and a local Docker Engine')
    if system == 'Darwin' and docker_os == 'Docker Desktop':
        return 'desktop'
    if system == 'Linux' and 'desktop' not in docker_os.lower():
        return 'linux'
    raise ValueError('Unsupported node metrics deployment environment')


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
    runtime = 'desktop' if platform.system() == 'Darwin' else 'linux'
    if action == 'up':
        info = docker_json('info', '--format', '{{json .}}')
        runtime = deployment_runtime(platform.system(), info['KernelVersion'], endpoint, info['OperatingSystem'])
        if (info['OSType'] != 'linux'
                or runtime == 'linux' and info['KernelVersion'] != platform.release()):
            raise ValueError('Node metrics Docker Engine must share the current Linux kernel')
    ids = subprocess.check_output(['docker', 'ps', '-aq', '--filter',
                                  'label=com.docker.compose.project=' + PROJECT], text=True).split()
    if ids:
        for item in docker_json('inspect', *ids):
            if item['Config']['Labels'].get('io.addp.node-metrics.owner') != str(ROOT):
                raise ValueError('Node metrics project belongs to another workspace')
    desktop = runtime == 'desktop'
    values = dict(env, ADDP_NODE_METRICS_OWNER=str(ROOT),
                  ADDP_NODE_METRICS_ROOTFS='/' if desktop else '/host',
                  ADDP_NODE_METRICS_PROCFS='/proc' if desktop else '/host/proc',
                  ADDP_NODE_METRICS_SYSFS='/sys' if desktop else '/host/sys',
                  ADDP_NODE_METRICS_NAMESPACED_COLLECTOR_PREFIX='--no-collector' if desktop else '--collector')
    if desktop:
        listen = env['ADDP_NODE_METRICS_LISTEN'] if action == 'up' else '127.0.0.1:9100'
        address, port = listen.rsplit(':', 1)
        values.update(ADDP_NODE_METRICS_LISTEN=':9100',
                      ADDP_NODE_METRICS_PUBLISH_IP=address.strip('[]'),
                      ADDP_NODE_METRICS_PUBLISH_PORT=port)
    # Do not inherit arbitrary profile activation or source paths from the caller.
    values.pop('COMPOSE_PROFILES', None)
    args = ['docker', 'compose', '--env-file', '/dev/null', '-p', PROJECT,
            '-f', str(ROOT / 'scripts/infra/node-metrics.yml'),
            '-f', str(ROOT / f'scripts/infra/node-metrics-{runtime}.yml')]
    commands = {'up': ['up', '-d', '--force-recreate', '--wait', '--wait-timeout', '30', 'node-exporter'],
                'down': ['down'], 'status': ['ps']}
    subprocess.run(args + commands[action], env=values, check=True)
    if action == 'up':
        scope = 'Docker Desktop VM (kernel-global metrics only)' if desktop else 'native Linux node'
        print(f'Node metrics: deployed {scope}; source readiness and Monitor admission require mTLS verification')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('up', 'down', 'status'))
    args = parser.parse_args()
    try:
        run(args.action, os.environ)
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError):
        sys.exit('Node metrics deployment rejected; check deployment environment, explicit inputs and project ownership')
