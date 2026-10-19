"""One read-only Docker source for deployment discovery and admission projections."""
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


REPOSITORY = Path(__file__).resolve().parents[2]
ROOT = REPOSITORY / 'business'
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


def export_inventory(service, *, env=None, reader=None, system=None, root=ROOT):
    if service != 'postgres':
        raise InventoryError('首期部署来源清单仅支持 postgres 服务')
    docker, info, kind = local_docker(env=env, reader=reader, system=system)

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
    if read() != first or ids() != container_ids or json.loads(docker('info', '--format', INFO)) != info:
        raise InventoryError('部署发现期间来源发生变化')
    services = {}
    for service, container in first.values():
        services.setdefault(service, []).append(container)
    if len(services) > 64 or any(len(containers) > 16 for containers in services.values()):
        raise InventoryError('部署发现服务数量超出预算')
    result = {'schema': 'addp.deployment-discovery/v1', 'scope': scope, 'state': 'available',
              'observed_at': observed_at(),
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


def unavailable_snapshot(scope):
    return {'schema': 'addp.deployment-discovery/v1', 'scope': scope, 'state': 'unavailable',
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
    return hashlib.sha256(Path(__file__).read_bytes()+json.dumps(env, sort_keys=True).encode()).hexdigest()


def terminate_observer(record):
    state = observer_state(record)
    if state == 'foreign':
        raise InventoryError('部署发现 PID 不属于当前工作区，拒绝停止')
    if state == 'owned':
        os.kill(record['pid'], signal.SIGTERM)
        for _ in range(100):
            state = observer_state(record)
            if state != 'owned':
                break
            time.sleep(0.05)
        if state != 'absent':
            raise InventoryError('部署发现进程尚未退出，不删除运行记录')
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
                time.sleep(30)
        finally:
            for scope in SCOPES:
                try:
                    # The controller invalidates both scopes after verifying
                    # exit. Never delay that verification behind another writer.
                    with private_lock(directory, scope+'.lock', timeout=0):
                        write_document(directory, scope+'.json', unavailable_snapshot(scope))
                except (InventoryError, OSError):
                    pass


def main():
    parser = argparse.ArgumentParser(description='只读发现当前工作区部署，不访问业务数据或修改容器')
    commands = parser.add_subparsers(dest='command', required=True)
    export = commands.add_parser('export', help='导出不含凭据的当前部署来源清单')
    export.add_argument('--service', required=True, choices=['postgres'])
    refresh = commands.add_parser('refresh', help='部署生命周期自动发布无凭据发现快照')
    refresh.add_argument('--scope', required=True, choices=sorted(SCOPES))
    refresh.add_argument('--directory', default=os.environ.get('ADDP_DEPLOYMENT_DISCOVERY_DIR') or str(REPOSITORY / '.dev-state/deployment-discovery'))
    for name in ('start', 'stop', 'watch'):
        command = commands.add_parser(name, help='部署发现观测进程的标准生命周期')
        command.add_argument('--directory', default=os.environ.get('ADDP_DEPLOYMENT_DISCOVERY_DIR') or str(REPOSITORY / '.dev-state/deployment-discovery'))
        if name == 'watch':
            command.add_argument('--observer-token', required=True)
    args = parser.parse_args()
    try:
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
        encoded = export_inventory(args.service)
    except InventoryError as error:
        print('部署来源读取失败：' + str(error), file=sys.stderr)
        return 1
    except (ValueError, TypeError, KeyError, OSError, subprocess.SubprocessError):
        # Raw Docker/JSON errors may embed untrusted source content. Never echo it.
        print('部署来源导出失败：请确认本地 Linux Docker／Docker Desktop、当前工作区的单个运行服务及实际端口；不接受远程或环境覆盖，导出期间服务须保持稳定。', file=sys.stderr)
        return 1
    sys.stdout.write(encoded)
    return 0


if __name__ == '__main__':
    sys.exit(main())
