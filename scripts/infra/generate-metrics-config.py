"""Render the sole Prometheus configuration from explicit deployment inputs."""
from pathlib import Path
import hmac
import importlib.util
import ipaddress
import json
import os
import platform
import subprocess
import re
import sys
import tempfile
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[2]
FILES = ('control-ca.crt', 'source-ca.crt', 'collector.crt', 'collector.key',
         'prometheus-client-secret')


class TargetBudgetExceeded(ValueError):
    pass


def check_target_budget(env, root=ROOT):
    value = env.get('ADDP_METRICS_TARGET_LIMIT', '12')
    if not re.fullmatch(r'[1-9]|[12][0-9]|3[0-4]', value):
        raise ValueError('ADDP_METRICS_TARGET_LIMIT must be an integer from 1 to 34')
    raw_plan = env.get('ADDP_DEV_PROCESS_METRICS_PLAN', '')
    if not raw_plan:
        return value

    spec = importlib.util.spec_from_file_location('metrics_sources', ROOT / 'scripts/infra/metrics-sources.py')
    sources = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(sources)
    launcher = sources.load(ROOT / 'scripts/dev/process-metrics-launch.py')
    processes = len(launcher.parse_plan(raw_plan))
    hosts = int(sources.enabled('node', env))
    engines = sum(sources.enabled(kind, env) for _, kind in sources.selection(['infra'], []))
    if env.get('ADDP_ONLINE_HOST') != '1' and (root / 'business/.env').exists():
        business = sources.environment('business', env, root)
        engines += sum(sources.enabled(kind, business) for _, kind in sources.selection(['business'], []))
    total = processes + hosts + engines
    if total > int(value):
        raise TargetBudgetExceeded(
            f'指标采集预算超限：计划 {total} 个（进程 {processes}、主机 {hosts}、引擎 {engines}），上限 {value}。'
        )
    return value


def origin(value, scheme="https"):
    if not value or len(value) > 512 or not value.isascii() or any(c.isspace() for c in value):
        raise ValueError('Metrics origin must be canonical with explicit port')
    parsed = urlsplit(value)
    if (parsed.scheme != scheme or parsed.username is not None or parsed.password is not None
            or parsed.path or parsed.query or parsed.fragment or '%' in value or '@' in value):
        raise ValueError('Metrics origin must contain only the selected scheme and authority')
    host, port = parsed.hostname, parsed.port
    if not host or not port:
        raise ValueError('Metrics control origin requires a host and explicit port')
    try:
        address = ipaddress.ip_address(host)
    except ValueError:
        address = None
    if address is not None:
        if address.is_unspecified or address.is_multicast or address.is_link_local:
            raise ValueError('Metrics control origin uses forbidden address')
        authority = f'[{address}]:{port}' if address.version == 6 else f'{address}:{port}'
    else:
        if not re.fullmatch(r'[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?', host) or len(host) > 253:
            raise ValueError('Metrics control origin host is not canonical') from None
        if all(c in '0123456789.' for c in host):
            raise ValueError('Metrics control origin has invalid IP address')
        if any(not re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', label)
               for label in host.split('.')):
            raise ValueError('Metrics control origin has invalid DNS labels')
        authority = f'{host}:{port}'
    if value != scheme + '://' + authority:
        raise ValueError('Metrics control origin is not canonical')
    return value


def desktop_loopback_rules(env):
    value = env.get('ADDP_METRICS_DESKTOP_LOOPBACK_PORTS', '')
    if not value:
        return ''
    ports = value.split(',')
    if (len(ports) > 64 or len(set(ports)) != len(ports)
            or any(not re.fullmatch(r'[1-9][0-9]{0,4}', port) or int(port) > 65535 for port in ports)):
        raise ValueError('Desktop loopback ports must be unique canonical ports')
    if platform.system() != 'Darwin':
        raise ValueError('Desktop loopback transport requires macOS Docker Desktop')
    endpoint = env.get('DOCKER_HOST') if not env.get('DOCKER_CONTEXT') else None
    if not endpoint:
        contexts = json.loads(subprocess.check_output(['docker', 'context', 'inspect'], text=True))
        endpoint = contexts[0]['Endpoints']['docker']['Host']
    if not endpoint.startswith('unix:///'):
        raise ValueError('Desktop loopback transport requires a local Engine')
    info = json.loads(subprocess.check_output(['docker', 'info', '--format', '{{json .}}'], text=True))
    if info.get('OperatingSystem') != 'Docker Desktop' or info.get('OSType') != 'linux':
        raise ValueError('Desktop loopback transport requires Docker Desktop Linux Engine')
    # Preserve the canonical Monitor source scope before changing only transport.
    return ('    relabel_configs:\n'
            '      - source_labels: [__address__]\n'
            '        target_label: instance\n' + ''.join(
                '      - source_labels: [__address__]\n'
                '        regex: ' + json.dumps(r'127\.0\.0\.1:' + port) + '\n'
                '        target_label: __address__\n'
                '        replacement: ' + json.dumps('host.docker.internal:' + port) + '\n'
                for port in sorted(ports, key=int)))


def atomic_config(path, content):
    with tempfile.NamedTemporaryFile(mode='w', dir=path.parent, prefix='.metrics-', delete=False) as output:
        pending = Path(output.name)
        try:
            os.chmod(pending, 0o644)  # Public addresses and paths only.
            output.write(content)
            output.flush()
            os.fsync(output.fileno())
            pending.replace(path)
        finally:
            pending.unlink(missing_ok=True)


def render(env, root=ROOT, *, check_only=False):
    target_limit = check_target_budget(env, root)
    mtls = env.get('ADDP_METRICS_CONTROL_MTLS_ENABLED', 'false')
    if mtls not in ('true', 'false'):
        raise ValueError('Control mTLS setting must be boolean')
    files = FILES + (('control-client.crt', 'control-client.key') if mtls == 'true' else ())
    directory = Path(env.get('ADDP_METRICS_DEPLOYMENT_DIR', ''))
    if (not directory.is_absolute() or not directory.is_dir()
            or directory.resolve().is_relative_to(root.resolve())):
        raise ValueError('ADDP_METRICS_DEPLOYMENT_DIR must be an existing directory outside the repository')
    allowed = set(files) | {'prometheus.yml'}
    if any(p.name not in allowed for p in directory.iterdir()):
        raise ValueError('Metrics deployment directory contains unexpected files')
    content = {}
    for name in files:
        path = directory / name
        if path.is_symlink() or not path.is_file():
            raise ValueError('Missing regular metrics deployment file: ' + name)
        with path.open('rb') as file:
            content[name] = file.read((1 << 20) + 1)
        if not content[name] or len(content[name]) > 1 << 20:
            raise ValueError('Metrics deployment file exceeds size budget: ' + name)
    secret = env.get('PROMETHEUS_SERVICE_CLIENT_SECRET', '')
    if not re.fullmatch(r'[a-zA-Z0-9_-]{32,72}', secret):
        raise ValueError('Invalid independent Prometheus OAuth secret')
    if not hmac.compare_digest(content['prometheus-client-secret'], secret.encode()):
        raise ValueError('Prometheus OAuth secret file must exactly match System deployment input')
    if any(value == secret for key, value in env.items()
           if key.endswith('_SERVICE_CLIENT_SECRET') and key != 'PROMETHEUS_SERVICE_CLIENT_SECRET'):
        raise ValueError('Prometheus OAuth secret must be independent')
    health = Path(env.get('ADDP_METRICS_TLS_DIR', '')) / 'health.crt'
    if health.is_file() and health.read_bytes() == content['collector.crt']:
        raise ValueError('Metrics collector must not reuse the center health certificate')
    if mtls == 'true':
        if content['control-client.crt'] == content['collector.crt'] or (
                health.is_file() and content['control-client.crt'] == health.read_bytes()):
            raise ValueError('Control mTLS identity must be independent')
    config = (root / 'scripts/infra/prometheus.yml').read_text().replace('@@TARGET_LIMIT@@', target_limit)
    config = config.replace('@@DESKTOP_LOOPBACK@@', desktop_loopback_rules(env))
    for name, path in (('SYSTEM', '/api/v1/system/oauth/token'),
                       ('MONITOR', '/api/v1/monitor/platform/metrics_discovery')):
        config = config.replace('@@' + name + '_URL@@', json.dumps(origin(env.get('PROMETHEUS_' + name + '_URL', '')) + path))
    for marker, indent in (('@@CONTROL_MTLS@@', 10), ('@@OAUTH_MTLS@@', 12)):
        tls = ''
        if mtls == 'true':
            tls = ''.join(' ' * indent + key + ': /etc/prometheus/deployment/control-client.' + ext + '\n'
                          for key, ext in (('cert_file', 'crt'), ('key_file', 'key')))
        config = config.replace(marker, tls)
    if not check_only:
        atomic_config(directory / 'prometheus.yml', config)


if __name__ == '__main__':
    try:
        if sys.argv[1:] == ['--check-budget']:
            check_target_budget(os.environ)
        elif sys.argv[1:] in ([], ['--check']):
            render(os.environ, check_only=len(sys.argv) == 2)
        else:
            sys.exit('Usage: generate-metrics-config.py [--check | --check-budget]')
    except TargetBudgetExceeded as err:
        sys.exit(str(err))
    except (OSError, ValueError, subprocess.SubprocessError):
        # Neither raw origins nor secret/file contents belong in startup diagnostics.
        sys.exit('Metrics discovery deployment input is missing or invalid')
