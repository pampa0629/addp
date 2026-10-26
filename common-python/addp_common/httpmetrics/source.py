"""Opt-in, serial mTLS source using the existing process deployment contract."""
from __future__ import annotations

import ipaddress
import json
import logging
import os
from pathlib import Path
import re
import socket
import ssl
import stat
import threading
import time
from datetime import datetime
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlsplit

from prometheus_client.core import GaugeMetricFamily
from . import MetricsUnavailable, process

logger = logging.getLogger('addp.httpmetrics')
SCHEMA = 'addp.process-metrics/v1'
ENV = 'ADDP_PROCESS_METRICS_DEPLOYMENTS'
_NAME = re.compile(r'[a-z0-9_-]{1,50}\Z')


def _unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate_deployment_field')
        result[key] = value
    return result


def deployment(raw, module, role):
    if not raw:
        return None
    if len(raw.encode()) > 256 << 10:
        raise ValueError('deployment_limit')
    entries = json.loads(raw, object_pairs_hook=_unique)
    if not isinstance(entries, list) or len(entries) > 1000:
        raise ValueError('deployment_limit')
    seen, listeners, endpoints = set(), set(), set()
    selected = None
    required = {'module_name','role','listen','endpoint','tls_dir'}
    for entry in entries:
        if (not isinstance(entry, dict) or not required <= entry.keys() or entry.keys()-required-{'cgroup_v2'}
                or any(not isinstance(entry[k], str) or not 0 < len(entry[k]) <= 1024 for k in required)
                or 'cgroup_v2' in entry and type(entry['cgroup_v2']) is not bool):
            raise ValueError('deployment_invalid')
        name, kind = entry['module_name'], entry['role']
        endpoint = urlsplit(entry['endpoint'])
        host, port = _address(entry['listen'])
        if (not _NAME.fullmatch(name) or kind not in {'backend','worker','scheduler','ingress'}
                or not entry['endpoint'].startswith('https://') or len(entry['endpoint']) > 512
                or any(ord(c)<=32 or ord(c)==127 for c in entry['endpoint'])
                or endpoint.scheme != 'https' or not endpoint.hostname or endpoint.port is None
                or endpoint.path != '/metrics' or endpoint.query or endpoint.fragment or endpoint.username is not None
                or any(c in entry['endpoint'] for c in '%@?#\\\r\n\t ')
                or not Path(entry['tls_dir']).is_absolute() or os.path.normpath(entry['tls_dir']) != entry['tls_dir']):
            raise ValueError('deployment_invalid')
        canonical = ('['+endpoint.hostname+']' if ':' in endpoint.hostname else endpoint.hostname)+':'+str(endpoint.port)
        if endpoint.netloc != canonical:
            raise ValueError('endpoint_invalid')
        if not 0 < endpoint.port <= 65535:
            raise ValueError('deployment_invalid')
        try:
            address = ipaddress.ip_address(endpoint.hostname)
            if address.is_unspecified or address.is_multicast or address.is_link_local or getattr(address,'ipv4_mapped',None):
                raise ValueError('endpoint_invalid')
        except ValueError:
            if len(endpoint.hostname)>253 or any(not re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?', label) for label in endpoint.hostname.split('.')) or endpoint.hostname.replace('.','').isdigit():
                raise ValueError('endpoint_invalid') from None
        key = (name, kind)
        if key in seen or entry['listen'] in listeners or entry['endpoint'] in endpoints:
            raise ValueError('deployment_duplicate')
        seen.add(key);listeners.add(entry['listen']);endpoints.add(entry['endpoint'])
        if key == (module, role):
            if kind != 'backend' or entry.get('cgroup_v2'):
                raise ValueError('python_source_scope_unsupported')
            selected = dict(entry)
    return selected


def _address(value):
    url = urlsplit('http://' + value)
    address = ipaddress.ip_address(url.hostname or '')
    port = url.port
    canonical = f'[{address}]:{port}' if address.version == 6 else f'{address}:{port}'
    if (not port or address.is_unspecified or address.is_multicast or address.is_link_local
            or getattr(address,'ipv4_mapped',None) or '%' in value or canonical != value):
        raise ValueError('listen_invalid')
    return str(address), port


def source_tls(directory, endpoint):
    path = Path(directory)
    root = Path(__file__).resolve().parents[3]
    info = path.lstat()
    if (not stat.S_ISDIR(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077
            or path.resolve().is_relative_to(root) or set(os.listdir(path)) != {'ca.crt','server.crt','server.key'}):
        raise ValueError('source_tls_invalid')
    for name in ('ca.crt','server.crt','server.key'):
        fd = os.open(path/name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            info = os.fstat(fd)
            if (not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_nlink != 1
                    or not 0 < info.st_size <= 256 << 10 or name == 'server.key' and stat.S_IMODE(info.st_mode) != 0o600):
                raise ValueError('source_tls_invalid')
            if len(os.read(fd, (256 << 10)+1)) > 256 << 10:
                raise ValueError('source_tls_invalid')
        finally:
            os.close(fd)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.verify_mode = ssl.CERT_REQUIRED
    context.load_verify_locations(cafile=str(path/'ca.crt'))
    context.load_cert_chain(str(path/'server.crt'),str(path/'server.key'))
    # Check the leaf validity, purpose and advertised SAN before publishing.
    # Monitor and the center verify its chain against their own source trust.
    from cryptography import x509
    from cryptography.x509.oid import ExtendedKeyUsageOID
    from datetime import timezone
    leaf = x509.load_pem_x509_certificate((path/'server.crt').read_bytes())
    now = datetime.now(timezone.utc)
    if not leaf.not_valid_before_utc <= now < leaf.not_valid_after_utc:
        raise ValueError('source_tls_invalid')
    try:
        if leaf.extensions.get_extension_for_class(x509.BasicConstraints).value.ca:
            raise ValueError('source_tls_invalid')
    except x509.ExtensionNotFound:
        pass
    if ExtendedKeyUsageOID.SERVER_AUTH not in leaf.extensions.get_extension_for_class(x509.ExtendedKeyUsage).value:
        raise ValueError('source_tls_invalid')
    host = urlsplit(endpoint).hostname
    sans = leaf.extensions.get_extension_for_class(x509.SubjectAlternativeName).value
    try:
        matched = ipaddress.ip_address(host) in sans.get_values_for_type(x509.IPAddress)
    except ValueError:
        # Match canonical DNS SANs with the standard single-label wildcard rule.
        matched = any(host == value or value.startswith('*.') and host.endswith(value[1:])
                      and host.count('.') == value.count('.') for value in sans.get_values_for_type(x509.DNSName))
    if not matched:
        raise ValueError('source_tls_invalid')
    return context


class Identity:
    def __init__(self, registration):
        import sys
        if sys.platform not in {'darwin','linux'}:
            raise ValueError('source_platform_unsupported')
        self.labels = [registration.module_name, registration.role, registration.instance_id, sys.platform, SCHEMA]
        started = datetime.fromisoformat(registration.process_started_at.replace('Z','+00:00'))
        if started.tzinfo is None or not registration.instance_id.strip() or registration.instance_id != registration.instance_id.strip() or any(ord(c)<32 for c in registration.instance_id):
            raise ValueError('source_identity_invalid')
        self.started = started.timestamp()
        if not 0 < self.started <= time.time():
            raise ValueError('source_identity_invalid')

    def collect(self):
        info = GaugeMetricFamily('addp_process_identity_info', 'Current registered process identity.',
            labels=['module_name','runtime_role','runtime_instance_id','operating_system','schema_version'])
        info.add_metric(self.labels, 1)
        yield info
        yield GaugeMetricFamily('process_start_time_seconds','Registered process start time.',value=self.started)


class _Server(HTTPServer):
    request_queue_size = 1

    def get_request(self):
        connection, address = super().get_request()
        self.request_deadline = time.monotonic() + 3
        connection.settimeout(2)
        try:
            secured = self.context.wrap_socket(connection, server_side=True)
            self.current_connection = secured
            return secured, address
        except BaseException:
            connection.close()
            raise

    def handle_error(self, *_):
        logger.warning('http_metrics_source_request_failed')


class _Headers:
    """Bound the complete header read by bytes and one absolute deadline."""
    def __init__(self, raw, connection, deadline):
        self.raw, self.connection, self.deadline = raw, connection, deadline
        self.count = 0

    def readline(self, limit=-1):
        line = bytearray()
        while limit < 0 or len(line) < limit:
            remaining = self.deadline-time.monotonic()
            if remaining <= 0 or self.count >= 8192:
                raise TimeoutError('source_request_limit')
            self.connection.settimeout(remaining)
            value = self.raw.read(1)
            self.count += len(value)
            line.extend(value)
            if not value or value == b'\n':
                break
        return bytes(line)

    def close(self):
        self.raw.close()


class _Handler(BaseHTTPRequestHandler):
    def setup(self):
        super().setup()
        self.rfile = _Headers(self.rfile, self.connection, self.server.request_deadline)

    def log_message(self, *_):
        pass

    def do_GET(self):
        self.close_connection = True
        if self.path != '/metrics':
            self.send_error(404);return
        if ('Transfer-Encoding' in self.headers or self.headers.get('Content-Length','0') != '0'
                or sum(len(k)+len(v)+4 for k,v in self.headers.items()) > 8192):
            self.send_error(400);return
        try:
            began = time.monotonic()
            payload = self.server.metrics.render()
            if time.monotonic()-began >= 3:
                raise MetricsUnavailable('http_metrics_render_timeout')
        except Exception:
            self.send_error(503);return
        self.send_response(200)
        self.send_header('Content-Type','text/plain; version=0.0.4; charset=utf-8')
        self.send_header('Cache-Control','no-store')
        self.send_header('Content-Length',str(len(payload)))
        self.end_headers()
        remaining = self.server.request_deadline-time.monotonic()
        if remaining <= 0:
            return
        self.connection.settimeout(remaining)
        self.wfile.write(payload)

    def do_POST(self):
        self.send_error(405)

    do_HEAD = do_PUT = do_DELETE = do_PATCH = do_OPTIONS = do_CONNECT = do_TRACE = do_QUERY = do_POST


class Source:
    def __init__(self, server, thread, declaration):
        self.server, self.thread, self.declaration = server, thread, declaration
        self.closed = False

    def close(self):
        if self.closed:
            return
        connection = getattr(self.server, 'current_connection', None)
        if connection is not None:
            try:
                connection.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            connection.close()
        # A header reader is interrupted above; an in-progress TLS handshake
        # has its original 2s socket timeout. No business socket is touched.
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=3)
        self.server.metrics.registry.unregister(self.server.identity)
        if self.thread.is_alive():
            raise MetricsUnavailable('http_metrics_source_not_stopped')
        self.closed = True


def start_optional(registration):
    from ..client.module_registry import ProcessMetricsDeclaration
    try:
        entry = deployment(os.environ.get(ENV,''), registration.module_name, registration.role)
        if entry is None:
            return None
        metrics = process()
        identity = Identity(registration)
        context = source_tls(entry['tls_dir'], entry['endpoint'])
        host, port = _address(entry['listen'])
        server_type = type('IPv6Source',(_Server,),{'address_family':socket.AF_INET6}) if ':' in host else _Server
        server = server_type((host,port), _Handler)
        server.context, server.metrics, server.identity = context, metrics, identity
        metrics.registry.register(identity)
        thread = threading.Thread(target=server.serve_forever, kwargs={'poll_interval':.05}, daemon=True, name='addp-http-metrics')
        thread.start()
        return Source(server, thread, ProcessMetricsDeclaration(schema_version=SCHEMA, endpoint=entry['endpoint']))
    except Exception:
        if 'server' in locals():
            server.server_close()
        logger.warning('http_metrics_source_unavailable')
        return None
