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


def capacity_enabled(env):
    value = env.get('ADDP_NODE_BLOCK_CAPACITY_ENABLED', 'false')
    if value not in ('true', 'false'):
        raise ValueError('ADDP_NODE_BLOCK_CAPACITY_ENABLED must be true or false')
    return value == 'true'


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


def admission_document(env, runtime, node_kind=None, display_name=None, addresses=(), modules=()):
    if runtime != 'native' and deployment(env) is None:
        raise ValueError('Node metrics must be explicitly enabled')
    if runtime == 'desktop':
        if node_kind not in (None, 'virtual'):
            raise ValueError('Docker Desktop is a virtual Linux node')
        node_kind = 'virtual'
        if display_name is None:
            display_name = 'Docker Desktop Linux VM'
    elif runtime not in ('linux', 'native') or node_kind not in ('physical', 'virtual'):
        raise ValueError('Native Linux requires an explicit physical or virtual node kind')
    if display_name is None:
        display_name = platform.node()
    if (not isinstance(display_name, str) or not display_name.strip()
            or display_name != display_name.strip() or len(display_name) > 255
            or any(ord(char) < 32 or ord(char) == 127 for char in display_name)):
        raise ValueError('Invalid host display name')
    addresses = list(addresses)
    if len(addresses) > 32 or len(set(addresses)) != len(addresses):
        raise ValueError('Invalid explicit host addresses')
    for address in addresses:
        try:
            valid = str(ipaddress.ip_address(address)) == address
        except ValueError:
            valid = (len(address) <= 253 and re.fullmatch(
                r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)*', address))
        if not valid:
            raise ValueError('Invalid explicit host address')
    modules = list(modules)
    if len(modules) > 64 or len(set(modules)) != len(modules) or any(not isinstance(name, str) or not re.fullmatch(r'[a-z][a-z0-9_-]{0,49}', name) for name in modules):
        raise ValueError('Invalid explicit module declarations')
    result = {'schema': 'addp.host-admission/v2', 'host': {
        'display_name': display_name, 'node_kind': node_kind,
        'addresses': addresses, 'allowed_module_bindings': [{'module_name': name, 'client_id': 'addp-'+name} for name in modules]}}
    if runtime == 'native':
        return result
    result['collection'] = {'monitor_kind': 'host_resources', 'source': {
            'type': 'node_exporter', 'endpoint': 'https://' + env['ADDP_NODE_METRICS_LISTEN'] + '/metrics'}}
    return result


def write_admission(value):
    serialized = json.dumps(value, ensure_ascii=False, indent=2)
    if len(serialized.encode('utf-8')) > 16384:
        raise ValueError('Admission document exceeds size budget')
    print(serialized)


def run(action, env, *, node_kind=None, display_name=None, addresses=(), host_only=False, modules=()):
    if (host_only or modules) and action != 'export-admission':
        raise ValueError('Admission options require export-admission')
    if host_only:
        if platform.system() not in ('Darwin', 'Linux'):
            raise ValueError('Native admission supports macOS and Linux only')
        write_admission(admission_document(env, 'native', node_kind, display_name, addresses, modules))
        return
    if action == 'up' and deployment(env) is None:
        print('Node metrics: Disabled')
        return
    if action == 'export-admission' and deployment(env) is None:
        raise ValueError('Cannot export a disabled source')
    # Never operate a remote Docker host, including for status/down.
    endpoint = None if env.get('DOCKER_CONTEXT') else env.get('DOCKER_HOST')
    if not endpoint:
        context = docker_json('context', 'inspect')
        endpoint = context[0]['Endpoints']['docker']['Host']
    if not endpoint.startswith('unix:///'):
        raise ValueError('Node metrics lifecycle requires a local Docker endpoint')
    runtime = 'desktop' if platform.system() == 'Darwin' else 'linux'
    if action in ('up', 'export-admission'):
        info = docker_json('info', '--format', '{{json .}}')
        runtime = deployment_runtime(platform.system(), info['KernelVersion'], endpoint, info['OperatingSystem'])
        if (info['OSType'] != 'linux'
                or runtime == 'linux' and info['KernelVersion'] != platform.release()):
            raise ValueError('Node metrics Docker Engine must share the current Linux kernel')
    ids = subprocess.check_output(['docker', 'ps', '-aq', '--filter',
                                  'label=com.docker.compose.project=' + PROJECT], text=True).split()
    containers = docker_json('inspect', *ids) if ids else []
    for item in containers:
        if (item['Config']['Labels'].get('io.addp.node-metrics.owner') != str(ROOT)
                or item['Config']['Labels'].get('com.docker.compose.service') not in ('node-exporter', 'block-capacity')):
            raise ValueError('Node metrics project belongs to another workspace')
    capacity = capacity_enabled(env) if action in ('up', 'export-admission') else False
    if action == 'export-admission':
        expected = {'node-exporter', 'block-capacity'} if capacity else {'node-exporter'}
        services = [item['Config']['Labels'].get('com.docker.compose.service') for item in containers]
        if (set(services) != expected or len(services) != len(expected)
                or any(item.get('State', {}).get('Running') is not True for item in containers)):
            raise ValueError('Admission export requires the owned running node source')
        write_admission(admission_document(env, runtime, node_kind, display_name, addresses, modules))
        return
    desktop = runtime == 'desktop'
    values = dict(env, ADDP_NODE_METRICS_OWNER=str(ROOT),
                  ADDP_NODE_METRICS_TEXTFILE='--collector.textfile' if capacity else '--no-collector.textfile',
                  ADDP_NODE_METRICS_KERNEL=info['KernelVersion'] if action == 'up' else 'unused',
                  ADDP_NODE_CAPACITY_SYSFS='/sys' if desktop else '/source-sys',
                  ADDP_NODE_CAPACITY_PROCFS='/proc' if desktop else '/source-proc',
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
    # Always include the owned extension on down/status, so disabled inputs do
    # not strand its resources. No user-supplied path or profile is consumed.
    extension = capacity or action in ('down', 'status')
    if extension:
        args += ['-f', str(ROOT / 'scripts/infra/node-block-capacity.yml')]
        if not desktop:
            args += ['-f', str(ROOT / 'scripts/infra/node-block-capacity-linux.yml')]
    commands = {'up': ['up', '-d', '--remove-orphans', '--force-recreate', '--wait', '--wait-timeout', '30', 'node-exporter'] + (['block-capacity'] if capacity else []),
                'down': ['down'], 'status': ['ps']}
    # Validate ownership before any mutation, including removal of a volume left
    # after an interrupted previous deployment. The name is project-local.
    volume_name = PROJECT + '_block-capacity'
    volumes = subprocess.check_output(['docker', 'volume', 'ls', '-q', '--filter', 'name=^' + volume_name + '$'], text=True).split()
    if volumes:
        if volumes != [volume_name]:
            raise ValueError('Unexpected node volume')
        volume = docker_json('volume', 'inspect', volume_name)[0]
        if (volume.get('Labels') or {}).get('io.addp.node-metrics.owner') != str(ROOT):
            raise ValueError('Node capacity volume belongs to another workspace')
    subprocess.run(args + commands[action], env=values, check=True)
    if volumes and (action == 'down' or action == 'up' and not capacity):
        subprocess.run(['docker', 'volume', 'rm', volume_name], env=values, check=True)
    if action == 'up':
        scope = 'Docker Desktop VM (kernel-global metrics only)' if desktop else 'native Linux node'
        print(f'Node metrics: deployed {scope}; source readiness and Monitor admission require mTLS verification')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('up', 'down', 'status', 'export-admission'))
    parser.add_argument('--host-only', action='store_true', help='Export the current native host without Docker or host collection')
    parser.add_argument('--module', action='append', default=[], help='Explicit ADDP module allowed on this host')
    parser.add_argument('--node-kind', choices=('physical', 'virtual'))
    parser.add_argument('--display-name')
    parser.add_argument('--address', action='append', default=[])
    args = parser.parse_args()
    if args.action != 'export-admission' and (args.node_kind or args.display_name is not None or args.address or args.host_only or args.module):
        parser.error('Host attributes are only accepted for export-admission')
    try:
        run(args.action, os.environ, node_kind=args.node_kind, display_name=args.display_name, addresses=args.address, host_only=args.host_only, modules=args.module)
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError):
        sys.exit('Node metrics deployment rejected; check deployment environment, explicit inputs and project ownership')
