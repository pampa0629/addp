"""Deployment-owned discovery and intent publication; no application control."""
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
import tempfile
import stat
import fcntl
from contextlib import contextmanager
import signal
import uuid
import hashlib
import importlib.util


REPOSITORY = Path(__file__).resolve().parents[2]
ROOT = REPOSITORY / 'business'
INFO = '{"id":{{json .ID}},"os_type":{{json .OSType}},"operating_system":{{json .OperatingSystem}}}'
CONTAINER = ('{"id":{{json .Id}},"running":{{json .State.Running}},'
             '"status":{{json .State.Status}},"started_at":{{json .State.StartedAt}},'
             '"restart_count":{{json .RestartCount}},'
             '"project":{{json (index .Config.Labels "com.docker.compose.project")}},'
             '"service":{{json (index .Config.Labels "com.docker.compose.service")}},'
             '"working_dir":{{json (index .Config.Labels "com.docker.compose.project.working_dir")}},'
             '"oneoff":{{json (index .Config.Labels "com.docker.compose.oneoff")}},'
             '"ports":{{json .NetworkSettings.Ports}}}')
# Deliberately selected labels, never Config.Env, arbitrary labels or Secrets.
METRICS_CONTAINER = CONTAINER[:-1] + (
    ',"owner":{{json (index .Config.Labels "io.addp.postgres-metrics.owner")}}'
    ',"scope":{{json (index .Config.Labels "io.addp.postgres-metrics.scope")}}'
    ',"database_id":{{json (index .Config.Labels "io.addp.postgres-metrics.database-id")}}'
    ',"database_started_at":{{json (index .Config.Labels "io.addp.postgres-metrics.database-started-at")}}'
    ',"network_mode":{{json .HostConfig.NetworkMode}}}')
SCOPES = {'infra': ('addp-infra', REPOSITORY), 'business': ('business', ROOT)}
DISCOVERY_LIMIT = 256 * 1024
_observer_children = {}  # Reap observers started by this interpreter when stopped.
# Explicit product services; unknown services remain visible as unclassified.
ENGINE_TYPES = {
    'postgres': 'postgresql', 'redis': 'redis', 'falkordb': 'falkordb', 'minio': 'minio',
    'meilisearch': 'meilisearch', 'redpanda': 'kafka', 'business-redpanda': 'kafka',
    'oracle': 'oracle', 'supermap-postgresql': 'postgresql', 'doris-fe': 'doris',
    'clickhouse': 'clickhouse', 'mongodb': 'mongodb', 'mysql': 'mysql', 'oceanbase': 'oceanbase',
    'tidb': 'tidb', 'opengauss': 'opengauss', 'gaussdb': 'gaussdb', 'highgo': 'highgo',
    'kingbase': 'kingbase', 'dameng': 'dameng', 'elasticsearch': 'elasticsearch', 'neo4j': 'neo4j',
}


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


def local_docker(*, env=None, reader=None, system=None):
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
    return docker, info, kind


def observed_at():
    return datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z')


def discovery_container(value, project, root):
    service = value.get('service')
    if (not identifier(value.get('id')) or value.get('project') != project
            or not isinstance(service, str) or not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}', service)
            or value.get('oneoff') != 'False' or not isinstance(value.get('working_dir'), str)
            or not Path(value['working_dir']).is_absolute()
            or Path(value['working_dir']).resolve() != root.resolve()):
        raise InventoryError('发现的服务不属于当前工作区')
    status = value.get('status')
    if (status not in ('created', 'running', 'paused', 'restarting', 'exited', 'dead', 'removing')
            or type(value.get('running')) is not bool
            or type(value.get('restart_count')) is not int or value['restart_count'] < 0):
        raise InventoryError('部署运行状态无效')
    if status == 'running' and not value['running']:
        raise InventoryError('部署运行状态不一致')
    started = value.get('started_at')
    if not isinstance(started, str):
        raise InventoryError('部署启动时间无效')
    if started == '0001-01-01T00:00:00Z':
        started = ''
    elif (not re.fullmatch(r'[1-9][0-9]{3}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?Z', started)
          or not datetime.fromisoformat(started.replace('Z', '+00:00'))):
        raise InventoryError('部署启动时间无效')
    if value['running'] and not started:
        raise InventoryError('运行中的部署缺少启动时间')
    ports = value.get('ports')
    if not isinstance(ports, dict) or len(ports) > 32:
        raise InventoryError('部署端口映射无效')
    published = []
    for target, bindings in ports.items():
        match = re.fullmatch(r'([1-9][0-9]{0,4})/(tcp|udp|sctp)', target)
        if not match or int(match[1]) > 65535:
            raise InventoryError('部署端口映射无效')
        if bindings is None:
            continue
        if not isinstance(bindings, list) or len(bindings) > 8:
            raise InventoryError('部署端口映射无效')
        for binding in bindings:
            if not isinstance(binding, dict) or set(binding) != {'HostIp', 'HostPort'}:
                raise InventoryError('部署端口映射无效')
            address, port = binding['HostIp'], binding['HostPort']
            if (not isinstance(address, str) or '%' in address or str(ipaddress.ip_address(address)) != address
                    or not isinstance(port, str) or not re.fullmatch(r'[1-9][0-9]{0,4}', port) or int(port) > 65535):
                raise InventoryError('部署端口映射无效')
            published.append({'address': address, 'port': int(port), 'container_port': int(match[1]), 'protocol': match[2]})
    published.sort(key=lambda port: (port['container_port'], port['protocol'], port['address'], port['port']))
    if len(published) > 64 or len({tuple(port.items()) for port in published}) != len(published):
        raise InventoryError('部署端口映射重复或过多')
    return service, {'container_id': value['id'], 'status': status, 'started_at': started,
                     'restart_count': value['restart_count'], 'published_ports': published}


def postgres_metrics(docker, scope, containers):
    """A missing/invalid optional source never invalidates core deployment facts."""
    missing = {'state': 'not_deployed'}
    unavailable = {'state': 'unavailable'}
    try:
        project = 'addp-postgres-metrics-' + scope
        ids = docker('ps', '--all', '--no-trunc', '--filter',
                     'label=com.docker.compose.project='+project, '--format', '{{.ID}}').splitlines()
        if not ids:
            return missing
        if len(ids) != 2 or len(set(ids)) != 2 or any(not identifier(cid) for cid in ids):
            return unavailable
        databases = [value for service, value in containers.values() if service == 'postgres']
        if len(databases) != 1 or databases[0]['status'] != 'running':
            return unavailable
        database = databases[0]
        sources = {}
        for cid in ids:
            value = json.loads(docker('inspect', '--type', 'container', '--format', METRICS_CONTAINER, cid))
            service = value.get('service')
            if (value.get('id') != cid or value.get('project') != project or value.get('oneoff') != 'False'
                    or value.get('owner') != str(REPOSITORY) or value.get('scope') != scope
                    or value.get('database_id') != database['container_id']
                    or value.get('database_started_at') != database['started_at']
                    or value.get('running') is not True or value.get('status') != 'running'
                    or service not in ('postgres-metrics', 'postgres-metrics-web') or service in sources):
                return unavailable
            sources[service] = value
        exporter, web = sources['postgres-metrics'], sources['postgres-metrics-web']
        if web.get('network_mode') != 'container:'+exporter['id']:
            return unavailable
        ports = exporter.get('ports')
        if (not isinstance(ports, dict) or len(ports) > 8 or not isinstance(ports.get('9443/tcp'), list)
                or len(ports['9443/tcp']) != 1 or any(bindings for port, bindings in ports.items() if port != '9443/tcp')
                or any((web.get('ports') or {}).values())):
            return unavailable
        binding = ports['9443/tcp'][0]
        if not isinstance(binding, dict) or set(binding) != {'HostIp', 'HostPort'}:
            return unavailable
        ip, port = ipaddress.ip_address(binding['HostIp']), binding['HostPort']
        if (str(ip) != binding['HostIp'] or ip.is_unspecified or ip.is_multicast or ip.is_link_local
                or getattr(ip, 'ipv4_mapped', None) or '%' in str(ip)
                or not (ip.is_loopback or any(ip in network for network in
                    (ipaddress.ip_network('10.0.0.0/8'), ipaddress.ip_network('172.16.0.0/12'), ipaddress.ip_network('192.168.0.0/16'), ipaddress.ip_network('fc00::/7')) if ip.version == network.version))
                or not isinstance(port, str) or not re.fullmatch(r'[1-9][0-9]{0,4}', port) or int(port) > 65535):
            return unavailable
        host = '['+str(ip)+']' if ip.version == 6 else str(ip)
        return {'state': 'available', 'endpoint': 'https://'+host+':'+port+'/metrics',
                'container_id': database['container_id'], 'started_at': database['started_at']}
    except (ValueError, TypeError, KeyError, OSError):
        return unavailable


def discover(scope, *, env=None, reader=None, system=None, root=None):
    if scope not in SCOPES:
        raise InventoryError('部署发现仅支持 infra 或 business')
    project, workspace = SCOPES[scope]
    workspace = workspace if root is None else root
    docker, info, kind = local_docker(env=env, reader=reader, system=system)

    def ids():
        values = docker('ps', '--all', '--no-trunc', '--filter', 'label=com.docker.compose.project='+project,
                        '--format', '{{.ID}}').splitlines()
        if len(values) > 128 or len(set(values)) != len(values) or any(not identifier(value) for value in values):
            raise InventoryError('部署发现容器数量或身份无效')
        return sorted(values)

    container_ids = ids()
    def read():
        result = {}
        for cid in container_ids:
            value = json.loads(docker('inspect', '--type', 'container', '--format', CONTAINER, cid))
            if value.get('id') != cid:
                raise InventoryError('部署发现运行身份不一致')
            result[cid] = discovery_container(value, project, workspace)
        return result

    first = read()
    metrics = postgres_metrics(docker, scope, first)
    if (read() != first or postgres_metrics(docker, scope, first) != metrics or ids() != container_ids
            or json.loads(docker('info', '--format', INFO)) != info):
        raise InventoryError('部署发现期间来源发生变化')
    services = {}
    for service, container in first.values():
        services.setdefault(service, []).append(container)
    if len(services) > 64 or any(len(containers) > 16 for containers in services.values()):
        raise InventoryError('部署发现服务数量超出预算')
    result = {'schema': 'addp.deployment-discovery/v2', 'scope': scope, 'state': 'available',
              'observed_at': observed_at(),
              'postgresql_metrics': metrics,
              'source': {'type': 'docker_compose', 'runtime': kind, 'docker_engine_id': info['id'], 'project': project},
              'services': [{'service': service, 'engine_type': ENGINE_TYPES.get(service, 'unclassified'),
                            'containers': containers} for service, containers in sorted(services.items())]}
    if len(json.dumps(result).encode('utf-8')) > DISCOVERY_LIMIT:
        raise InventoryError('部署发现快照超过预算')
    return result


def private_directory(directory):
    if not directory.is_absolute() or directory.is_symlink():
        raise InventoryError('部署发现目录必须为绝对且非符号链接的路径')
    directory.mkdir(parents=True, mode=0o700, exist_ok=True)
    info = directory.stat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise InventoryError('部署发现目录须仅由当前部署用户访问')


@contextmanager
def private_lock(directory, name, *, timeout=5):
    lock = directory / name
    fd = os.open(lock, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_nlink != 1 or info.st_mode & 0o077:
            raise InventoryError('部署发现锁文件权限无效')
        deadline = time.monotonic()+timeout
        while True:
            try:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except BlockingIOError:
                if time.monotonic() >= deadline:
                    raise InventoryError('部署发现正在刷新')
                time.sleep(0.05)
        yield
    finally:
        os.close(fd)


def write_document(directory, name, value):
    target = directory / name
    if target.is_symlink() or (target.exists() and not target.is_file()):
        raise InventoryError('部署发现目标不是普通文件')
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(mode='w', encoding='utf-8', dir=directory, prefix='.'+name+'-', delete=False) as out:
            temporary = Path(out.name)
            out.write(json.dumps(value, ensure_ascii=False, separators=(',', ':'))+'\n')
            out.flush()
            os.fsync(out.fileno())
        os.replace(temporary, target)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def service_expectations(directory, groups, *, stop_modules=None, stop_all=False, replace_running=False):
    """Persist deployment intent, never infer it from surviving processes."""
    directory = Path(directory)
    private_directory(directory)
    with private_lock(directory, 'service-expectations.lock'):
        target = directory / 'service-expectations.json'
        previous = None
        if target.exists() or target.is_symlink():
            fd = os.open(target, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
            with os.fdopen(fd, 'rb') as source:
                info = os.fstat(source.fileno())
                if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077 or info.st_nlink != 1:
                    raise InventoryError('服务部署意图文件权限无效')
                raw = source.read(DISCOVERY_LIMIT + 1)
            if len(raw) > DISCOVERY_LIMIT:
                raise InventoryError('服务部署意图超过预算')
            previous = json.loads(raw, object_pairs_hook=unique_service_intent_object)
            validate_service_expectations(previous)
        owner = hashlib.sha256(str(REPOSITORY).encode()).hexdigest()
        if previous and previous['deployment_id'] != owner:
            raise InventoryError('服务部署意图不属于当前工作区')
        current = {tuple(row[k] for k in ('module_name', 'role', 'runtime_name')): dict(row)
                   for row in previous['groups']} if previous else {}
        now = observed_at()
        if stop_all or stop_modules is not None:
            if groups or not isinstance(stop_all, bool) or (stop_modules is not None and
                    (not isinstance(stop_modules, list) or not all(service_intent_name(x) for x in stop_modules))):
                raise InventoryError('服务停止范围无效')
            for row in current.values():
                if (stop_all or row['module_name'] in stop_modules) and row['desired_state'] != 'planned_stopped':
                    row.update(desired_state='planned_stopped', changed_at=now)
        else:
            if not isinstance(groups, list) or (not groups and not replace_running) or len(groups) > 1000:
                raise InventoryError('服务启动范围无效')
            seen = set()
            for group in groups:
                if not isinstance(group, dict) or set(group) != {'module_name', 'role', 'runtime_name', 'expected_replicas'}:
                    raise InventoryError('服务启动声明无效')
                row = dict(group, desired_state='running', changed_at=now)
                validate_service_intent_group(row)
                key = tuple(row[k] for k in ('module_name', 'role', 'runtime_name'))
                if key in seen:
                    raise InventoryError('服务启动声明重复')
                seen.add(key)
                old = current.get(key)
                if old and all(old[k] == row[k] for k in group) and old['desired_state'] == 'running':
                    row = old
                current[key] = row
        if replace_running:
            if stop_modules is not None or stop_all:
                raise InventoryError('服务部署声明范围无效')
            for key, row in current.items():
                if key not in seen and row['desired_state'] != 'planned_stopped':
                    row.update(desired_state='planned_stopped', changed_at=now)
        rows = [current[k] for k in sorted(current)]
        if previous and rows == previous['groups']:
            return previous
        document = {'schema': 'addp.service-expectations/v1', 'deployment_id': owner,
                    'revision': previous['revision'] + 1 if previous else 1, 'groups': rows}
        validate_service_expectations(document)
        if len(json.dumps(document).encode()) > DISCOVERY_LIMIT:
            raise InventoryError('服务部署意图超过预算')
        write_document(directory, 'service-expectations.json', document)
        return document


# Formal launch names, shared by native launch units and Compose declarations.
SERVICE_WORKER_GROUPS = {
    'meta-worker': ('meta', 'scan'),
    'quality-worker': ('quality', 'quality-bounded'),
    'transfer-bounded-worker': ('transfer', 'sync'),
    'transfer-continuous-worker': ('transfer', 'continuous_sync'),
}


def service_group_for_launch(service, replicas):
    if service in SERVICE_WORKER_GROUPS:
        module, purpose = SERVICE_WORKER_GROUPS[service]
        role = 'worker'
    elif isinstance(service, str) and service.endswith('-backend') and service_intent_name(service[:-8]):
        module, purpose, role = service[:-8], '', 'backend'
    else:
        return None
    if type(replicas) is not int or not 0 <= replicas <= 1000:
        raise InventoryError('服务声明副本数无效')
    if replicas == 0:
        return None
    return {'module_name': module, 'role': role, 'runtime_name': purpose, 'expected_replicas': replicas}


def service_groups_from_units(units):
    if not units or len(units) > 1000 or len(set(units)) != len(units):
        raise InventoryError('服务启动单元无效')
    counts = {}
    for unit in units:
        if unit in SERVICE_WORKER_GROUPS:
            raise InventoryError('Worker必须使用正式编号启动单元')
        match = re.fullmatch(r'(.+)-([12])', unit)
        service = match[1] if match and match[1] in SERVICE_WORKER_GROUPS else unit
        if service_group_for_launch(service, 1) is None:
            raise InventoryError('服务启动单元不在声明范围')
        counts[service] = counts.get(service, 0) + 1
    return [service_group_for_launch(name, count) for name, count in sorted(counts.items())]


def service_groups_from_compose(document, profiles):
    if not isinstance(document, dict) or not isinstance(document.get('services'), dict) or len(document['services']) > 1000:
        raise InventoryError('平台Compose服务声明无效')
    groups = []
    for name, service in document['services'].items():
        if not isinstance(service, dict):
            raise InventoryError('平台Compose服务声明无效')
        selected = service.get('profiles', [])
        if not isinstance(selected, list) or not all(isinstance(x, str) and x for x in selected):
            raise InventoryError('平台Compose Profile无效')
        if selected and '*' not in profiles and not set(selected).intersection(profiles):
            continue
        deploy = service.get('deploy', {})
        if not isinstance(deploy, dict):
            raise InventoryError('平台Compose副本声明无效')
        replicas = service.get('scale', deploy.get('replicas', 1))
        if 'scale' in service and 'replicas' in deploy and (type(service['scale']) is not int or type(deploy['replicas']) is not int or service['scale'] != deploy['replicas']):
            raise InventoryError('平台Compose副本声明冲突')
        group = service_group_for_launch(name, replicas)
        if group:
            groups.append(group)
    return groups


def publish_platform_service_expectations(directory, compose_files):
    files = [Path(name) for name in compose_files]
    if not files or len(files) > 16 or any(p.is_symlink() or not p.is_file() or p.stat().st_size > DISCOVERY_LIMIT*4 for p in files):
        raise InventoryError('平台Compose来源文件无效')
    command = ['docker', 'compose']
    for path in files:
        command.extend(['-f', str(path)])
    # Render declarations only. Never inspect live container Env, SQL or Secrets;
    # the private rendered configuration is not logged or persisted.
    try:
        result = subprocess.run(command + ['config', '--format', 'json'], stdout=subprocess.PIPE,
                                stderr=subprocess.DEVNULL, timeout=15, check=True)
    except (OSError, subprocess.SubprocessError):
        raise InventoryError('平台Compose声明不可读') from None
    if len(result.stdout) > 4*1024*1024:
        raise InventoryError('平台Compose声明超过预算')
    document = json.loads(result.stdout, object_pairs_hook=unique_service_intent_object)
    profiles = {x.strip() for x in os.environ.get('COMPOSE_PROFILES', '').split(',') if x.strip()}
    groups = service_groups_from_compose(document, profiles)
    directory = Path(directory)
    private_directory(directory)
    overlay = {'services': {'system-backend': {
        'environment': {'ADDP_DEPLOYMENT_DISCOVERY_DIR': '/etc/addp/deployment-discovery'},
        'volumes': [{'type': 'bind', 'source': str(directory), 'target': '/etc/addp/deployment-discovery',
                     'read_only': True, 'bind': {'create_host_path': False}}]}}}
    write_document(directory, 'platform-service-expectations.json', overlay)
    return service_expectations(directory, groups, replace_running=True)


def unique_service_intent_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise InventoryError('服务部署意图字段重复')
        result[key] = value
    return result


def service_intent_name(value):
    return isinstance(value, str) and re.fullmatch(r'[a-z][a-z0-9_-]{0,49}', value) is not None


def validate_service_intent_group(row):
    if (not isinstance(row, dict) or set(row) != {'module_name', 'role', 'runtime_name', 'expected_replicas', 'desired_state', 'changed_at'}
            or not service_intent_name(row['module_name']) or row['role'] not in ('backend', 'worker')
            or (row['role'] == 'backend' and row['runtime_name'] != '')
            or (row['role'] == 'worker' and not service_intent_name(row['runtime_name']))
            or type(row['expected_replicas']) is not int or not 1 <= row['expected_replicas'] <= 1000
            or row['desired_state'] not in ('running', 'planned_stopped')):
        raise InventoryError('服务部署意图分组无效')
    try:
        if not isinstance(row['changed_at'], str) or not re.fullmatch(r'[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?Z', row['changed_at']):
            raise ValueError()
        stamp = datetime.fromisoformat(row['changed_at'].replace('Z', '+00:00'))
        if not row['changed_at'].endswith('Z') or stamp.tzinfo is None or stamp > datetime.now(timezone.utc):
            raise ValueError()
    except (ValueError, TypeError, AttributeError):
        raise InventoryError('服务部署意图时间无效') from None


def validate_service_expectations(document):
    if (not isinstance(document, dict) or set(document) != {'schema', 'deployment_id', 'revision', 'groups'}
            or document['schema'] != 'addp.service-expectations/v1' or not identifier(document['deployment_id'])
            or type(document['revision']) is not int or not 1 <= document['revision'] <= 9007199254740991
            or not isinstance(document['groups'], list) or len(document['groups']) > 1000):
        raise InventoryError('服务部署意图文档无效')
    keys = []
    for row in document['groups']:
        validate_service_intent_group(row)
        keys.append(tuple(row[k] for k in ('module_name', 'role', 'runtime_name')))
    if keys != sorted(set(keys)):
        raise InventoryError('服务部署意图分组顺序或唯一性无效')


def unavailable_snapshot(scope):
    return {'schema': 'addp.deployment-discovery/v2', 'scope': scope, 'state': 'unavailable',
            'observed_at': observed_at(), 'reason': 'source_unavailable', 'services': []}


def publish(scope, directory, **kwargs):
    if scope not in SCOPES:
        raise InventoryError('部署发现仅支持 infra 或 business')
    directory = Path(directory)
    private_directory(directory)
    with private_lock(directory, scope+'.lock'):
        failed = False
        try:
            value = discover(scope, **kwargs)
        except (ValueError, TypeError, KeyError, OSError):
            # Never retain an earlier successful snapshot as the current result.
            failed = True
            value = unavailable_snapshot(scope)
        write_document(directory, scope+'.json', value)
        if failed:
            raise InventoryError('部署发现来源不可用，已清除当前有效候选')
        return value


def publish_container_proofs(directory):
    """Optional host capability; absence clears proofs, never blocks engines."""
    directory = Path(directory)
    private_directory(directory)
    with private_lock(directory, 'container-proofs.lock'):
        value = {'schema': 'addp.container-proofs/v1', 'observed_at': observed_at(), 'proofs': []}
        try:
            if platform.system() == 'Linux':
                docker, info, kind = local_docker()
                if kind != 'linux_docker':
                    raise InventoryError('完整宿主层级不可用')
                helper = Path(__file__).with_name('container-proof.py')
                spec = importlib.util.spec_from_file_location('addp_container_proof', helper)
                module = importlib.util.module_from_spec(spec)
                spec.loader.exec_module(module)
                previous = None
                # The previous proof only extends a verified unchanged interval.
                target = directory/'container-proofs.json'
                if target.exists() and not target.is_symlink() and target.stat().st_size <= DISCOVERY_LIMIT:
                    previous = json.loads(target.read_text())
                value = module.discover(docker, info, REPOSITORY, value['observed_at'], previous)
                if len(json.dumps(value).encode()) > DISCOVERY_LIMIT:
                    raise InventoryError('容器证明超过预算')
        except (ValueError, TypeError, KeyError, OSError):
            value['proofs'] = []
        write_document(directory, 'container-proofs.json', value)


def read_private_document(directory, name):
    try:
        fd = os.open(directory / name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    except FileNotFoundError:
        return None
    with os.fdopen(fd, 'r', encoding='utf-8') as source:
        info = os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077 or info.st_size > 4096:
            raise InventoryError('部署发现运行记录权限或大小无效')
        return json.loads(source.read(4097))


def observer_record(directory):
    value = read_private_document(directory, 'observer.json')
    if value is None:
        return None
    if (not isinstance(value, dict) or set(value) != {'pid', 'token', 'script', 'directory', 'configuration'}
            or type(value['pid']) is not int or value['pid'] <= 1
            or not isinstance(value['token'], str) or not re.fullmatch(r'[0-9a-f]{32}', value['token'])
            or value['script'] != str(Path(__file__).resolve()) or value['directory'] != str(directory)
            or not isinstance(value['configuration'], str) or not re.fullmatch(r'[0-9a-f]{64}', value['configuration'])):
        raise InventoryError('部署发现运行记录与当前工作区不符')
    return value


def observer_state(record):
    result = subprocess.run(['ps', '-ww', '-p', str(record['pid']), '-o', 'stat=', '-o', 'command='],
                            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, timeout=3)
    if result.returncode == 1 and not result.stdout.strip():
        return 'absent'
    if result.returncode != 0 or len(result.stdout) > 16384:
        raise InventoryError('无法核验部署发现进程归属')
    state, _, command = result.stdout.strip().partition(' ')
    if state.startswith('Z'):
        return 'absent'
    if (record['script'] not in command or record['directory'] not in command
            or not re.search(r'(?<!\S)--observer-token '+record['token']+r'(?!\S)', command)):
        return 'foreign'
    return 'owned'


def observer_environment():
    keys = ('PATH', 'HOME', 'TMPDIR', 'USER', 'LOGNAME', 'LANG', 'LC_ALL', 'XDG_CONFIG_HOME',
            'DOCKER_CONFIG', 'DOCKER_CONTEXT', 'DOCKER_HOST', 'DOCKER_TLS_VERIFY', 'DOCKER_CERT_PATH')
    return {key: os.environ[key] for key in keys if key in os.environ}


def observer_configuration(env):
    helper = Path(__file__).with_name('container-proof.py')
    return hashlib.sha256(Path(__file__).read_bytes()+helper.read_bytes()+json.dumps(env, sort_keys=True).encode()).hexdigest()


def observer_proc_text(pid, name):
    """Bounded Linux diagnostics only; never read command lines or environment."""
    paths = {'state': 'stat', 'wait': 'wchan', 'signals': 'status', 'children': f'task/{pid}/children'}
    try:
        fd = os.open(f'/proc/{pid}/{paths[name]}', os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, 'rb') as source:
            info = os.fstat(source.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid():
                return None
            value = source.read(8193)
        return value.decode('utf-8') if len(value) <= 8192 else None
    except (OSError, UnicodeError):
        return None


def observer_exit_diagnostic(record, last_state):
    result = {'last_identity': last_state if last_state in ('owned', 'foreign', 'absent') else 'unknown'}
    if platform.system() != 'Linux':
        return dict(result, proc_state='unavailable')
    pid = record['pid']
    # /proc/stat comm is arbitrary text, including parentheses; discard it all.
    fields = (observer_proc_text(pid, 'state') or '').rpartition(')')[2].split()
    result['proc_state'] = fields[0] if fields and fields[0] in ('R', 'S', 'D', 'T', 't', 'Z', 'X', 'I') else 'unknown'
    channel = (observer_proc_text(pid, 'wait') or '').strip()
    known = ('do_wait', 'wait_for_vfork_done', 'pipe_read', 'futex_wait_queue', 'futex_wait_queue_me',
             'hrtimer_nanosleep', 'locks_lock_inode_wait', 'io_schedule', 'wait_on_page_bit_common', '0')
    result['wait'] = channel if channel in known else 'other' if channel else 'unknown'
    masks = {}
    for line in (observer_proc_text(pid, 'signals') or '').splitlines():
        key, _, value = line.partition(':')
        if key in ('SigPnd', 'ShdPnd', 'SigBlk', 'SigIgn', 'SigCgt') and re.fullmatch(r'[0-9a-fA-F]{1,16}', value.strip()):
            masks[key] = int(value.strip(), 16)
    term = 1 << (signal.SIGTERM - 1)
    for output, keys in (('term_pending', ('SigPnd', 'ShdPnd')), ('term_blocked', ('SigBlk',)),
                         ('term_ignored', ('SigIgn',)), ('term_caught', ('SigCgt',))):
        result[output] = any(masks[key] & term for key in keys) if all(key in masks for key in keys) else 'unknown'
    children = observer_proc_text(pid, 'children')
    children = children.strip() if children is not None else None
    result['children'] = (len(children.split()) if children and re.fullmatch(r'[0-9]+(?:\s+[0-9]+)*', children)
                          else 0 if children == '' else 'unknown')
    return result


def terminate_observer(record):
    state = observer_state(record)
    if state == 'foreign':
        raise InventoryError('部署发现 PID 不属于当前工作区，拒绝停止')
    if state == 'owned':
        try:
            os.kill(record['pid'], signal.SIGTERM)
        except ProcessLookupError:
            pass  # Exit may race the signal; absence still has to be verified.
        for _ in range(100):
            state = observer_state(record)
            # ps can lose command identity while the signaled process exits.
            # Only observe after the first signal: never signal a foreign PID.
            if state == 'absent':
                break
            time.sleep(0.05)
        if state != 'absent':
            diagnostic = observer_exit_diagnostic(record, state)
            raise InventoryError('部署发现进程尚未退出，不删除运行记录；退出诊断=' + json.dumps(diagnostic, sort_keys=True, separators=(',', ':')))
    child = _observer_children.pop(record['pid'], None)
    if child is not None:
        child.wait(timeout=3)


def start_observer(directory):
    directory = Path(directory)
    private_directory(directory)
    directory = directory.resolve()
    with private_lock(directory, 'observer-control.lock'):
        env = observer_environment()
        configuration = observer_configuration(env)
        record = observer_record(directory)
        if record is not None:
            state = observer_state(record)
            if state == 'owned' and record['configuration'] == configuration:
                return
            if state == 'owned':
                terminate_observer(record)
            elif state != 'absent':
                raise InventoryError('部署发现 PID 不属于当前工作区，拒绝接管')
        token = uuid.uuid4().hex
        # The source observer has no ADDP/SQL credentials in its environment.
        process = subprocess.Popen([sys.executable, str(Path(__file__).resolve()), 'watch', '--directory', str(directory),
                                    '--observer-token', token], cwd=REPOSITORY, env=env,
                                   stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                   close_fds=True, start_new_session=True)
        try:
            record = {'pid': process.pid, 'token': token, 'script': str(Path(__file__).resolve()),
                      'directory': str(directory), 'configuration': configuration}
            write_document(directory, 'observer.json', record)
            for _ in range(100):
                if process.poll() is not None:
                    raise InventoryError('部署发现进程启动失败')
                ready = read_private_document(directory, 'observer-ready.json')
                if ready == {'token': token}:
                    _observer_children[process.pid] = process
                    return
                time.sleep(0.05)
            raise InventoryError('部署发现进程启动超过期限')
        except BaseException:
            # The unreaped Popen child cannot have its PID reused; never leave
            # an unregistered observer alive if publishing its record fails.
            if process.poll() is None:
                process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=3)
            raise


def stop_observer(directory):
    directory = Path(directory)
    if not directory.exists():
        return
    private_directory(directory)
    directory = directory.resolve()
    with private_lock(directory, 'observer-control.lock'):
        record = observer_record(directory)
        if record is not None:
            terminate_observer(record)
            (directory / 'observer.json').unlink(missing_ok=True)
            (directory / 'observer-ready.json').unlink(missing_ok=True)
        for scope in SCOPES:
            with private_lock(directory, scope+'.lock'):
                write_document(directory, scope+'.json', unavailable_snapshot(scope))
        with private_lock(directory, 'container-proofs.lock'):
            write_document(directory, 'container-proofs.json', {'schema':'addp.container-proofs/v1','observed_at':observed_at(),'proofs':[]})


def watch(directory, token):
    directory = Path(directory)
    private_directory(directory)
    directory = directory.resolve()
    if not re.fullmatch(r'[0-9a-f]{32}', token):
        raise InventoryError('部署发现运行标识无效')
    def terminate(_signum, _frame):
        raise SystemExit(0)
    signal.signal(signal.SIGTERM, terminate)
    signal.signal(signal.SIGINT, terminate)
    with private_lock(directory, 'observer-runtime.lock'):
        for _ in range(100):
            record = observer_record(directory)
            if record is not None and record['pid'] == os.getpid() and record['token'] == token:
                if record['configuration'] != observer_configuration(observer_environment()):
                    raise InventoryError('部署发现源码或运行环境已改变')
                break
            time.sleep(0.05)
        else:
            raise InventoryError('部署发现运行记录缺失')
        write_document(directory, 'observer-ready.json', {'token': token})
        try:
            while True:
                if observer_record(directory) != record:
                    raise InventoryError('部署发现运行记录已改变')
                for scope in SCOPES:
                    try:
                        publish(scope, directory)
                    except (InventoryError, OSError):
                        pass  # The failure snapshot or freshness deadline clears candidates.
                publish_container_proofs(directory)
                time.sleep(30)
        finally:
            try:
                with private_lock(directory, 'container-proofs.lock', timeout=0):
                    write_document(directory, 'container-proofs.json', {'schema':'addp.container-proofs/v1','observed_at':observed_at(),'proofs':[]})
            except (InventoryError, OSError):
                pass
            for scope in SCOPES:
                try:
                    # The controller invalidates both scopes after verifying
                    # exit. Never delay that verification behind another writer.
                    with private_lock(directory, scope+'.lock', timeout=0):
                        write_document(directory, scope+'.json', unavailable_snapshot(scope))
                except (InventoryError, OSError):
                    pass


def main():
    parser = argparse.ArgumentParser(description='发布当前工作区部署发现与运行预期，不访问业务数据或修改容器')
    commands = parser.add_subparsers(dest='command', required=True)
    refresh = commands.add_parser('refresh', help='部署生命周期自动发布无凭据发现快照')
    refresh.add_argument('--scope', required=True, choices=sorted(SCOPES))
    refresh.add_argument('--directory', default=os.environ.get('ADDP_DEPLOYMENT_DISCOVERY_DIR') or str(REPOSITORY / '.dev-state/deployment-discovery'))
    intents = commands.add_parser('service-expectations', help='标准部署入口发布服务预期；不操作进程')
    intents.add_argument('--state', required=True, choices=('running', 'planned_stopped'))
    intents.add_argument('--modules', nargs='+')
    intents.add_argument('--all', action='store_true')
    source = intents.add_mutually_exclusive_group()
    source.add_argument('--units', nargs='+')
    source.add_argument('--compose-file', action='append')
    intents.add_argument('--directory', default=os.environ.get('ADDP_DEPLOYMENT_DISCOVERY_DIR') or str(REPOSITORY / '.dev-state/deployment-discovery'))
    for name in ('start', 'stop', 'watch'):
        command = commands.add_parser(name, help='部署发现观测进程的标准生命周期')
        command.add_argument('--directory', default=os.environ.get('ADDP_DEPLOYMENT_DISCOVERY_DIR') or str(REPOSITORY / '.dev-state/deployment-discovery'))
        if name == 'watch':
            command.add_argument('--observer-token', required=True)
    args = parser.parse_args()
    try:
        if args.command == 'service-expectations':
            if args.state == 'running':
                if args.modules or args.all:
                    raise InventoryError('服务启动声明不接受停止范围')
                if args.units:
                    service_expectations(args.directory, service_groups_from_units(args.units))
                    return 0
                if args.compose_file:
                    publish_platform_service_expectations(args.directory, args.compose_file)
                    return 0
                raw = sys.stdin.buffer.read(DISCOVERY_LIMIT + 1)
                if len(raw) > DISCOVERY_LIMIT:
                    raise InventoryError('服务启动声明超过预算')
                service_expectations(args.directory, json.loads(raw, object_pairs_hook=unique_service_intent_object))
            else:
                if args.units or args.compose_file or bool(args.modules) == args.all:
                    raise InventoryError('服务停止声明必须选择唯一范围')
                service_expectations(args.directory, [], stop_modules=args.modules, stop_all=args.all)
            return 0
        if args.command == 'refresh':
            publish(args.scope, args.directory)
            return 0
        if args.command in ('start', 'stop', 'watch'):
            if args.command == 'watch':
                watch(args.directory, args.observer_token)
            elif args.command == 'start':
                start_observer(args.directory)
            else:
                stop_observer(args.directory)
            return 0
    except InventoryError as error:
        prefix = '服务部署意图发布失败：' if args.command == 'service-expectations' else '部署来源读取失败：'
        print(prefix + str(error), file=sys.stderr)
        return 1
    except (ValueError, TypeError, KeyError, OSError, subprocess.SubprocessError):
        # Raw Docker/JSON errors may embed untrusted source content. Never echo it.
        message = ('服务部署意图发布失败：请检查受管目录、声明配置及写入权限。' if args.command == 'service-expectations'
                   else '部署来源读取失败：请检查本地 Docker Context、工作区来源及实际端口。')
        print(message, file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
