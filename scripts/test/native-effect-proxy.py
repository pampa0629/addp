"""Private disposable transport: bind one exact mutation to a bounded fault barrier.

No product API or database writes. Each instance has a fixed upstream and one
immutable fault binding. Control listens only on container loopback; the Hosted
lifecycle owns the process, network, configuration and destruction.
"""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import socket
import socketserver
import struct
import sys
import threading
import uuid
from urllib.parse import urlsplit, parse_qs

LIMIT = 8 * 1024 * 1024
WRITE_WAIT_SECONDS = 180


class Fault:
    def __init__(self, run_id):
        if not re.fullmatch(r'[A-Za-z0-9_-]{1,100}', run_id):
            raise ValueError('invalid run identity')
        self.run_id, self.lock, self.binding = run_id, threading.Lock(), None
        self.forwarded, self.upstream_response, self.blocked = 0, False, 0
        self.entered, self.expired = 0, False
        self.release_event = threading.Event()

    def arm(self, value):
        with self.lock:
            if self.binding is not None or value.get('run_id') != self.run_id:
                raise ValueError('fault already bound or foreign run')
            if str(uuid.UUID(value['key'])) != value['key'] or uuid.UUID(value['key']).int == 0:
                raise ValueError('invalid operation identity')
            if value.get('protocol') == 'http':
                if (set(value) != {'key', 'run_id', 'protocol', 'method', 'path', 'body_sha256', 'correlated'}
                        or value['method'] not in {'DELETE', 'POST'}
                        or not isinstance(value['path'], str) or not value['path'].startswith('/') or '#' in value['path']
                        or type(value['correlated']) is not bool
                        or value['correlated'] and (value['method'] != 'POST' or not re.fullmatch(r'/indexes/[A-Za-z0-9_-]+/documents/delete', value['path']))
                        or not re.fullmatch(r'[0-9a-f]{64}', value['body_sha256'])):
                    raise ValueError('invalid exact HTTP target')
            elif value.get('protocol') == 'http-write':
                if (set(value) != {'key', 'run_id', 'protocol', 'path'}
                        or not isinstance(value['path'], str)
                        or not re.fullmatch(r'/manager/tenant_[1-9][0-9]*/model3d-quick-view/[A-Za-z0-9_./-]+\.glb', value['path'])
                        or '..' in value['path'].split('/')):
                    raise ValueError('invalid exact GLB writer target')
            elif value.get('protocol') == 'kafka':
                if (set(value) != {'key', 'run_id', 'protocol', 'topic'}
                        or not re.fullmatch(r'__addp_dlq\.[1-9][0-9]*\.[1-9][0-9]*', value['topic'])):
                    raise ValueError('invalid task-owned DLQ target')
            else:
                raise ValueError('unsupported transport')
            self.binding = dict(value)

    def take(self, protocol, **target):
        with self.lock:
            expected = self.binding
            if expected is None or expected['protocol'] != protocol:
                return 'pass'
            if any(expected.get(k) != v for k, v in target.items()):
                return 'pass'
            if self.forwarded:
                self.blocked += 1
                return 'block'
            self.forwarded = 1
            return 'drop'

    def take_write(self, method, path):
        with self.lock:
            if not self.binding or self.binding['protocol'] != 'http-write' or method != 'PUT' or path != self.binding['path']:
                return 'pass'
            if self.entered:
                self.blocked += 1
                return 'block'
            self.entered = 1
            return 'defer'

    def release(self, value):
        with self.lock:
            if (not self.binding or self.binding['protocol'] != 'http-write'
                    or value != {'run_id': self.run_id, 'key': self.binding['key']} or self.expired):
                raise ValueError('foreign or expired writer release')
            self.release_event.set()

    def await_write(self):
        if not self.release_event.wait(WRITE_WAIT_SECONDS):
            with self.lock: self.expired = True
            return False
        with self.lock: self.forwarded = 1
        return True

    def received(self):
        with self.lock:
            self.upstream_response = True

    def state(self):
        with self.lock:
            value = dict(run_id=self.run_id, key=self.binding['key'] if self.binding else None,
                         forwarded=self.forwarded, upstream_response=self.upstream_response, blocked_replays=self.blocked)
            if self.binding and self.binding['protocol'] == 'http-write':
                value.update(entered=self.entered, released=self.release_event.is_set(), expired=self.expired)
            return value


class Control(BaseHTTPRequestHandler):
    def log_message(self, *_): pass

    def do_POST(self):
        try:
            size = int(self.headers.get('Content-Length', '0'))
            if not 0 < size < 4096: raise ValueError('invalid control size')
            value = json.loads(self.rfile.read(size))
            if self.path == '/arm': self.server.fault.arm(value)
            elif self.path == '/release': self.server.fault.release(value)
            elif self.path != '/state' or value != {'run_id': self.server.fault.run_id}:
                raise ValueError('invalid control identity')
            data = json.dumps(self.server.fault.state()).encode()
            self.send_response(200); self.send_header('Content-Length', str(len(data)))
            self.end_headers(); self.wfile.write(data)
        except (ValueError, KeyError, TypeError):
            self.send_error(400, 'invalid private control')


class HTTPProxy(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'
    def log_message(self, *_): pass

    def relay(self):
        upstream = None
        try:
            if self.headers.get('Transfer-Encoding'): raise ValueError('unbounded body')
            size = int(self.headers.get('Content-Length', '0'))
            if not 0 <= size <= LIMIT or not self.path.startswith('/'): raise ValueError('invalid request')
            body = self.rfile.read(size)
            if len(body) != size: raise ValueError('incomplete body')
            binding = self.server.fault.binding
            path, correlated = self.path, False
            if binding and binding.get('correlated'):
                url = urlsplit(self.path); query = parse_qs(url.query, strict_parsing=True)
                if set(query) == {'customMetadata'} and len(query['customMetadata']) == 1:
                    correlation = query['customMetadata'][0]
                    if str(uuid.UUID(correlation)) == correlation:
                        path, correlated = url.path, True
            action = self.server.fault.take_write(self.command, urlsplit(self.path).path)
            if action == 'pass':
                action = self.server.fault.take('http', method=self.command, path=path, correlated=correlated,
                                               body_sha256=hashlib.sha256(body).hexdigest())
            if action == 'block':
                self.send_error(503, 'owned mutation remains isolated'); return
            if action == 'defer' and not self.server.fault.await_write():
                self.close_connection = True; return
            upstream = http.client.HTTPConnection(*self.server.upstream, timeout=30)
            headers = {k: v for k, v in self.headers.items() if k.lower() not in {'connection', 'proxy-connection'}}
            upstream.request(self.command, self.path, body, headers)
            response = upstream.getresponse()
            if action == 'defer': self.server.fault.received()
            if action == 'drop':
                # Getting the actual upstream response proves a sent request;
                # an independent native probe must still prove the effect.
                self.server.fault.received()
                self.close_connection = True
                self.connection.shutdown(socket.SHUT_RDWR)
                return
            self.send_response(response.status)
            for key, value in response.getheaders():
                if key.lower() not in {'connection', 'transfer-encoding'}:
                    self.send_header(key, value)
            self.send_header('Connection', 'close'); self.end_headers()
            while data := response.read(65536): self.wfile.write(data)
            self.close_connection = True
        except (OSError, ValueError, http.client.HTTPException):
            self.close_connection = True
        finally:
            if upstream is not None: upstream.close()

    do_GET = do_POST = do_DELETE = do_PUT = do_HEAD = relay


def read_exact(connection, size):
    if not 0 <= size <= LIMIT: raise ValueError('frame exceeds budget')
    data = bytearray()
    while len(data) < size:
        chunk = connection.recv(size - len(data))
        if not chunk: raise EOFError
        data.extend(chunk)
    return bytes(data)


def frame(connection):
    header = read_exact(connection, 4)
    data = read_exact(connection, struct.unpack('>i', header)[0])
    return header + data


class Cursor:
    def __init__(self, data): self.data, self.pos = data, 0
    def take(self, size):
        if size < 0 or self.pos + size > len(self.data): raise ValueError('truncated Kafka request')
        value = self.data[self.pos:self.pos + size]; self.pos += size
        return value
    def number(self, fmt): return struct.unpack(fmt, self.take(struct.calcsize(fmt)))[0]
    def varint(self):
        value = 0
        for shift in range(0, 35, 7):
            byte = self.take(1)[0]; value |= (byte & 127) << shift
            if byte < 128: return value
        raise ValueError('invalid Kafka varint')
    def tags(self):
        count = self.varint()
        if count > 100: raise ValueError('Kafka tag budget exceeded')
        for _ in range(count): self.varint(); self.take(self.varint())


def delete_topics(packet):
    """Decode the pinned kmsg DeleteTopics v0-v6 layout, never substring-match."""
    cur = Cursor(packet[4:])
    api, version, correlation = cur.number('>h'), cur.number('>h'), cur.number('>i')
    if api != 20: return correlation, None
    if not 0 <= version <= 6: raise ValueError('unreviewed DeleteTopics version')
    client_len = cur.number('>h')
    if client_len < -1: raise ValueError('invalid Kafka client')
    if client_len >= 0: cur.take(client_len)
    flexible = version >= 4
    if flexible: cur.tags()
    count = cur.varint() - 1 if flexible else cur.number('>i')
    if not 1 <= count <= 100: raise ValueError('invalid DeleteTopics targets')
    topics = []
    for _ in range(count):
        length = cur.varint() - 1 if flexible else cur.number('>h')
        if length <= 0: raise ValueError('DeleteTopics requires an exact topic name')
        topics.append(cur.take(length).decode('utf8'))
        if version >= 6:
            if cur.take(16) != bytes(16): raise ValueError('unexpected topic UUID')
            cur.tags()
    cur.take(4)  # timeout_ms
    if flexible: cur.tags()
    if cur.pos != len(cur.data): raise ValueError('unexpected Kafka request tail')
    return correlation, topics


class KafkaProxy(socketserver.BaseRequestHandler):
    def handle(self):
        upstream = socket.create_connection(self.server.upstream, timeout=30)
        pending, lock = {}, threading.Lock()
        self.request.settimeout(30)
        def responses():
            try:
                while True:
                    packet = frame(upstream)
                    correlation = struct.unpack('>i', packet[4:8])[0]
                    with lock:
                        if correlation not in pending: raise ValueError('unknown Kafka response correlation')
                        action = pending.pop(correlation)
                    if action == 'drop':
                        self.server.fault.received()
                        self.request.shutdown(socket.SHUT_RDWR)
                        upstream.shutdown(socket.SHUT_RDWR)
                        return
                    self.request.sendall(packet)
            except (OSError, ValueError, EOFError):
                try: self.request.shutdown(socket.SHUT_RDWR)
                except OSError: pass
        thread = threading.Thread(target=responses, daemon=True); thread.start()
        try:
            while True:
                packet = frame(self.request)
                correlation, topics = delete_topics(packet)
                action = 'pass'
                if topics is not None:
                    state = self.server.fault.binding
                    if state and state.get('topic') in topics:
                        if topics != [state['topic']]: raise ValueError('fault cannot delete multiple targets')
                        action = self.server.fault.take('kafka', topic=topics[0])
                if action == 'block': break
                with lock:
                    if correlation in pending or len(pending) >= 256: raise ValueError('invalid in-flight Kafka correlation budget')
                    pending[correlation] = action
                upstream.sendall(packet)
        except (OSError, ValueError, EOFError, struct.error): pass
        finally:
            try: self.request.shutdown(socket.SHUT_RDWR)
            except OSError: pass
            try: upstream.shutdown(socket.SHUT_RDWR)
            except OSError: pass
            upstream.close(); thread.join(timeout=2)


class TCPServer(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


def main():
    parser = argparse.ArgumentParser(); parser.add_argument('--config', required=True)
    args = parser.parse_args(); path = Path(args.config)
    if (os.environ.get('ADDP_ONLINE_HOSTED') != '1' or os.environ.get('ADDP_ONLINE_TEST') != '1'
            or os.environ.get('GITHUB_ACTIONS') != 'true' or os.environ.get('RUNNER_OS') != 'Linux'
            or sys.platform != 'linux' or os.uname().machine != 'x86_64'):
        raise ValueError('disposable Hosted profile required')
    if path.is_symlink() or path.stat().st_mode & 0o777 != 0o600 or path.stat().st_size > 4096:
        raise ValueError('private fixed-upstream config required')
    config = json.loads(path.read_text())
    if set(config) != {'run_id', 'protocol', 'listen_port', 'upstream_host', 'upstream_port'}:
        raise ValueError('invalid proxy config')
    if config['run_id'] != os.environ.get('ADDP_ONLINE_TEST_RUN_ID') or config['protocol'] not in {'http', 'kafka'}:
        raise ValueError('proxy identity mismatch')
    if any(type(config[k]) is not int or not 1024 <= config[k] <= 65535 for k in ('listen_port', 'upstream_port')):
        raise ValueError('invalid proxy port')
    fault = Fault(config['run_id'])
    server = (ThreadingHTTPServer(('0.0.0.0', config['listen_port']), HTTPProxy) if config['protocol'] == 'http'
              else TCPServer(('0.0.0.0', config['listen_port']), KafkaProxy))
    server.fault, server.upstream = fault, (config['upstream_host'], config['upstream_port'])
    control = ThreadingHTTPServer(('127.0.0.1', 18087), Control); control.fault = fault
    threading.Thread(target=control.serve_forever, daemon=True).start()
    server.serve_forever()


if __name__ == '__main__': main()
