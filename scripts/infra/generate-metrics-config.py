"""Render the sole Prometheus configuration from explicit deployment inputs."""
from pathlib import Path
import hmac
import ipaddress
import json
import os
import re
import sys
import tempfile
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[2]
FILES = ('control-ca.crt', 'source-ca.crt', 'collector.crt', 'collector.key',
         'prometheus-client-secret')


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


def control_config(env, root):
    selected = env.get('ADDP_METRICS_CONTROL_ENABLED', 'false')
    if selected == 'false':
        return None
    if selected != 'true':
        raise ValueError('ADDP_METRICS_CONTROL_ENABLED must be true or false')
    directory = Path(env.get('ADDP_METRICS_CONTROL_DIR', ''))
    if (not directory.is_absolute() or not directory.is_dir()
            or directory.resolve().is_relative_to(root.resolve())):
        raise ValueError('Metrics control directory must exist outside repository')
    if any(p.name not in {'server.crt', 'server.key', 'nginx.conf'} for p in directory.iterdir()):
        raise ValueError('Metrics control directory contains unexpected files')
    for name in ('server.crt', 'server.key'):
        file = directory / name
        if file.is_symlink() or not file.is_file() or not 0 < file.stat().st_size <= 1 << 20:
            raise ValueError('Missing regular metrics control certificate file')
        with file.open('rb') as stream:
            stream.read(1)
    for name in ('SYSTEM', 'MONITOR'):
        if env.get('PROMETHEUS_' + name + '_URL') != 'https://metrics-control:9444':
            raise ValueError('Selected private control entry requires its exact HTTPS origin')
    upstream = origin(env.get('ADDP_METRICS_CONTROL_GATEWAY_URL', ''), scheme='http')
    template = (root / 'scripts/infra/metrics-control.conf').read_text()
    return directory / 'nginx.conf', template.replace('@@GATEWAY_URL@@', upstream)


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


def render(env, root=ROOT):
    control = control_config(env, root)
    directory = Path(env.get('ADDP_METRICS_DEPLOYMENT_DIR', ''))
    if (not directory.is_absolute() or not directory.is_dir()
            or directory.resolve().is_relative_to(root.resolve())):
        raise ValueError('ADDP_METRICS_DEPLOYMENT_DIR must be an existing directory outside the repository')
    allowed = set(FILES) | {'prometheus.yml'}
    if any(p.name not in allowed for p in directory.iterdir()):
        raise ValueError('Metrics deployment directory contains unexpected files')
    content = {}
    for name in FILES:
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
    config = (root / 'scripts/infra/prometheus.yml').read_text()
    for name, path in (('SYSTEM', '/api/v1/system/oauth/token'),
                       ('MONITOR', '/api/v1/monitor/platform/metrics_discovery')):
        config = config.replace('@@' + name + '_URL@@', json.dumps(origin(env.get('PROMETHEUS_' + name + '_URL', '')) + path))
    if control:
        atomic_config(*control)
    atomic_config(directory / 'prometheus.yml', config)


if __name__ == '__main__':
    try:
        render(os.environ)
    except (OSError, ValueError):
        # Neither raw origins nor secret/file contents belong in startup diagnostics.
        sys.exit('Metrics discovery deployment input is missing or invalid')
