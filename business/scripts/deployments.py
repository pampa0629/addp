"""Read-only, credential-free inventory of explicitly selected Business deployments."""
import argparse
from datetime import datetime, timezone
import ipaddress
import json
from pathlib import Path
import platform
import re
import subprocess
import sys
import time
import os


ROOT = Path(__file__).resolve().parents[1]
LIMIT = 16 * 1024
INFO = '{"id":{{json .ID}},"os_type":{{json .OSType}},"operating_system":{{json .OperatingSystem}}}'
CONTAINER = ('{"id":{{json .Id}},"running":{{json .State.Running}},'
             '"status":{{json .State.Status}},"started_at":{{json .State.StartedAt}},'
             '"restart_count":{{json .RestartCount}},'
             '"project":{{json (index .Config.Labels "com.docker.compose.project")}},'
             '"service":{{json (index .Config.Labels "com.docker.compose.service")}},'
             '"working_dir":{{json (index .Config.Labels "com.docker.compose.project.working_dir")}},'
             '"oneoff":{{json (index .Config.Labels "com.docker.compose.oneoff")}},'
             '"ports":{{json .NetworkSettings.Ports}}}')


class InventoryError(ValueError):
    """Safe operator-facing diagnosis, without raw Docker response contents."""


def identifier(value):
    return isinstance(value, str) and re.fullmatch(r'[0-9a-f]{64}', value) is not None


def runtime(info, system):
    if (not isinstance(info, dict) or info.get('os_type') != 'linux'
            or not isinstance(info.get('id'), str)
            or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9:_.-]{0,127}', info['id'])
            or not isinstance(info.get('operating_system'), str)):
        raise InventoryError('Docker Engine 来源信息无效')
    desktop = info['operating_system'] == 'Docker Desktop'
    if system == 'Darwin' and desktop:
        return 'desktop_linux_vm'
    if system == 'Linux' and 'desktop' not in info['operating_system'].lower():
        return 'linux_docker'
    raise InventoryError('仅支持本机 Linux Docker 或 macOS Docker Desktop Linux VM')


def container_evidence(value, service, root):
    if (not isinstance(value, dict) or not identifier(value.get('id'))
            or value.get('project') != 'business' or value.get('service') != service
            or value.get('oneoff') != 'False'
            or not isinstance(value.get('working_dir'), str)
            or not Path(value['working_dir']).is_absolute()
            or Path(value['working_dir']).resolve() != root.resolve()):
        raise InventoryError('容器不属于当前工作区的 Business 服务')
    if (value.get('running') is not True or value.get('status') != 'running'
            or type(value.get('restart_count')) is not int or value['restart_count'] < 0):
        raise InventoryError('所选 Business 服务没有稳定运行的容器')
    started = value.get('started_at')
    if (not isinstance(started, str) or not re.fullmatch(
            r'[1-9][0-9]{3}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?Z', started)):
        raise InventoryError('容器启动时间无效')
    datetime.fromisoformat(started.replace('Z', '+00:00'))
    ports = value.get('ports')
    bindings = ports.get('5432/tcp') if isinstance(ports, dict) else None
    if not isinstance(bindings, list) or not 1 <= len(bindings) <= 8:
        raise InventoryError('无法读取 PostgreSQL 实际 SQL 端口映射')
    published = []
    for binding in bindings:
        if not isinstance(binding, dict) or set(binding) != {'HostIp', 'HostPort'}:
            raise InventoryError('SQL 端口映射格式无效')
        address, port = binding['HostIp'], binding['HostPort']
        if (not isinstance(address, str) or str(ipaddress.ip_address(address)) != address
                or not isinstance(port, str) or not re.fullmatch(r'[1-9][0-9]{0,4}', port)
                or int(port) > 65535):
            raise InventoryError('SQL 端口映射地址无效')
        published.append({'address': address, 'port': int(port), 'container_port': 5432, 'protocol': 'tcp'})
    published.sort(key=lambda item: (item['address'], item['port']))
    if len({(item['address'], item['port']) for item in published}) != len(published):
        raise InventoryError('SQL 端口映射重复')
    return {'container_id': value['id'], 'started_at': started,
            'restart_count': value['restart_count'], 'published_ports': published}


class DockerReader:
    def __init__(self):
        self.deadline = time.monotonic() + 30

    def __call__(self, *args):
        remaining = self.deadline - time.monotonic()
        if remaining <= 0:
            raise InventoryError('部署来源读取超过 30 秒期限')
        try:
            result = subprocess.run(['docker', *args], stdout=subprocess.PIPE,
                                    stderr=subprocess.DEVNULL, check=True,
                                    timeout=min(10, remaining))
        except (OSError, subprocess.SubprocessError):
            raise InventoryError('无法读取 Docker 部署来源；请检查本地 Context 及服务状态') from None
        if len(result.stdout) > 64 * 1024:
            raise InventoryError('Docker 来源响应超过大小限制')
        return result.stdout.decode('utf-8').strip()


def export_inventory(service, *, env=None, reader=None, system=None, root=ROOT):
    # This first slice deliberately supports one declared deployment, not image/name guessing.
    if service != 'postgres':
        raise InventoryError('首期部署来源清单仅支持 postgres 服务')
    env = os.environ if env is None else env
    if any(env.get(key) for key in ('DOCKER_HOST', 'DOCKER_TLS_VERIFY', 'DOCKER_CERT_PATH')):
        raise InventoryError('请使用本地 Docker Context，不接受 DOCKER_HOST 或 TLS 环境覆盖')
    reader = DockerReader() if reader is None else reader
    context = reader('context', 'show')
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,127}', context):
        raise InventoryError('Docker Context 名称无效')
    endpoint = json.loads(reader('context', 'inspect', context, '--format', '{{json .Endpoints.docker.Host}}'))
    if not isinstance(endpoint, str) or not re.fullmatch(r'unix:///[^\x00-\x20?#]+', endpoint):
        raise InventoryError('只允许本地 Unix Socket Docker Context')

    def docker(*args):
        return reader('--context', context, *args)

    info = json.loads(docker('info', '--format', INFO))
    kind = runtime(info, platform.system() if system is None else system)

    def ids():
        values = docker('ps', '--all', '--no-trunc', '--filter', 'label=com.docker.compose.project=business',
                        '--filter', 'label=com.docker.compose.service=' + service,
                        '--format', '{{.ID}}').splitlines()
        if len(values) != 1 or not identifier(values[0]):
            raise InventoryError('所选服务必须恰有一个当前工作区容器；缺失或多容器时拒绝导出')
        return values

    container_ids = ids()
    first = json.loads(docker('inspect', '--type', 'container', '--format', CONTAINER, container_ids[0]))
    evidence = container_evidence(first, service, root)
    if evidence['container_id'] != container_ids[0]:
        raise InventoryError('容器运行身份不一致')
    second = json.loads(docker('inspect', '--type', 'container', '--format', CONTAINER, container_ids[0]))
    if (second != first or ids() != container_ids
            or json.loads(docker('info', '--format', INFO)) != info):
        raise InventoryError('导出期间部署发生变化，请等待服务稳定后重新导出')
    document = {'schema': 'addp.business-deployment-inventory/v1',
                'observed_at': datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z'),
                'source': {'type': 'docker_compose', 'runtime': kind,
                           'docker_engine_id': info['id'], 'project': 'business'},
                'services': [{'service': service, 'engine_type': 'postgresql', 'containers': [evidence]}]}
    encoded = json.dumps(document, ensure_ascii=False, indent=2) + '\n'
    if len(encoded.encode('utf-8')) > LIMIT:
        raise InventoryError('部署来源清单超过 16 KiB')
    return encoded


def main():
    parser = argparse.ArgumentParser(description='只读导出当前工作区 Business 部署来源，不接入平台或修改容器')
    commands = parser.add_subparsers(dest='command', required=True)
    export = commands.add_parser('export', help='导出不含凭据的当前部署来源清单')
    export.add_argument('--service', required=True, choices=['postgres'])
    args = parser.parse_args()
    try:
        encoded = export_inventory(args.service)
    except InventoryError as error:
        print('部署来源导出失败：' + str(error), file=sys.stderr)
        return 1
    except (ValueError, TypeError, KeyError):
        # Raw Docker/JSON errors may embed untrusted source content. Never echo it.
        print('部署来源导出失败：请确认本地 Linux Docker／Docker Desktop、当前工作区的单个运行服务及实际端口；不接受远程或环境覆盖，导出期间服务须保持稳定。', file=sys.stderr)
        return 1
    sys.stdout.write(encoded)
    return 0


if __name__ == '__main__':
    sys.exit(main())
