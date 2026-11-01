"""Deterministic transport tests; these are not native Hosted effect evidence."""
import hashlib
import http.client
import importlib
import io
import json
import socket
import struct
import threading
import unittest
import uuid
import time
from email.message import Message
from types import SimpleNamespace
from unittest.mock import patch
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

M = importlib.import_module('scripts.test.native-effect-proxy')


def varint(value):
    output = bytearray()
    while value > 127: output.append((value & 127) | 128); value >>= 7
    output.append(value)
    return bytes(output)


def request(version, topics, correlation=17):
    header = struct.pack('>hhi', 20, version, correlation) + struct.pack('>h', 4) + b'test'
    if version >= 4: header += b'\x00'
    body = varint(len(topics) + 1) if version >= 4 else struct.pack('>i', len(topics))
    for topic in topics:
        data = topic.encode()
        body += (varint(len(data) + 1) if version >= 4 else struct.pack('>h', len(data))) + data
        if version >= 6: body += bytes(16) + b'\x00'
    body += struct.pack('>i', 15000) + (b'\x00' if version >= 4 else b'')
    return struct.pack('>i', len(header + body)) + header + body


class NativeEffectProxyTest(unittest.TestCase):
    def test_bounded_http_body_framing(self):
        def read(headers, wire):
            message = Message()
            for key, value in headers: message[key] = value
            connection = SimpleNamespace(gettimeout=lambda: None, settimeout=lambda _: None)
            return M.read_http_body(SimpleNamespace(headers=message, connection=connection, rfile=io.BytesIO(wire)))
        valid = [([], b'', b''),
                 ([('Content-Length', '3')], b'abc', b'abc'),
                 ([('Transfer-Encoding', 'chunked')], b'1\r\na\r\n2\r\nbc\r\n0\r\n\r\n', b'abc'),
                 ([('Transfer-Encoding', 'CHUNKED')], b'0\r\n\r\n', b''),
                 ([('Transfer-Encoding', 'chunked')], b'8\r\n12345678\r\n0\r\n\r\n', b'12345678')]
        with patch.object(M, 'LIMIT', 8):
            for headers, wire, expected in valid:
                with self.subTest(headers=headers, wire=wire): self.assertEqual(read(headers, wire), expected)
            invalid = [([('Content-Length', '9')], b''),
                       ([('Content-Length', '3')], b'ab'),
                       ([('Content-Length', '-1')], b''),
                       ([('Content-Length', '3'), ('Content-Length', '3')], b'abc'),
                       ([('Content-Length', '3'), ('Transfer-Encoding', 'chunked')], b'abc'),
                       ([('Transfer-Encoding', 'chunked'), ('Transfer-Encoding', 'chunked')], b''),
                       ([('Transfer-Encoding', 'gzip, chunked')], b''),
                       ([('Transfer-Encoding', 'identity')], b''),
                       ([('Transfer-Encoding', 'chunked'), ('Trailer', 'X-Test')], b'0\r\n\r\n')]
            for headers, wire in invalid:
                with self.subTest(headers=headers, wire=wire), self.assertRaises(ValueError): read(headers, wire)
            for wire in (b'9\r\n', b'5\r\n12345\r\n4\r\n', b'z\r\n', b'1;ext=x\r\na\r\n0\r\n\r\n',
                         b'1\r\n', b'1\r\na', b'1\r\na\n0\r\n\r\n', b'0\r\n', b'0\r\nX-Test: value\r\n\r\n',
                         b'FFFFFFFFFFFFFFFFF\r\n', b'FFFFFFFFFFFFFFFF\r\n'):
                with self.subTest(wire=wire), self.assertRaises(ValueError): read([('Transfer-Encoding', 'chunked')], wire)
        # Many tiny chunks cannot evade the separate bounded framing budget.
        with self.assertRaises(ValueError):
            read([('Transfer-Encoding', 'chunked')], b'1\r\nx\r\n' * 14000 + b'0\r\n\r\n')
        with patch.object(M.time, 'monotonic', side_effect=[0, 31]), self.assertRaises(ValueError):
            read([('Content-Length', '1')], b'x')

    def test_chunked_sdk_json_reaches_upstream_as_the_same_bounded_payload(self):
        observed = []
        class Native(BaseHTTPRequestHandler):
            def log_message(self, *_): pass
            def do_POST(self):
                observed.append((self.path, self.headers.get('Transfer-Encoding'),
                                 self.headers.get('Authorization'), self.rfile.read(int(self.headers['Content-Length']))))
                self.send_response(200); self.send_header('Content-Length', '2'); self.end_headers(); self.wfile.write(b'{}')
        native = ThreadingHTTPServer(('127.0.0.1', 0), Native)
        proxy = ThreadingHTTPServer(('127.0.0.1', 0), M.HTTPProxy)
        proxy.upstream, proxy.fault = native.server_address, M.Fault('native-1')
        threads = [threading.Thread(target=s.serve_forever, daemon=True) for s in (native, proxy)]
        for thread in threads: thread.start()
        body = b'{"filter":"tenant_id = 2","limit":256}'
        try:
            client = http.client.HTTPConnection(*proxy.server_address, timeout=2)
            client.request('POST', '/indexes/content/documents/fetch', iter([body[:4], body[4:]]),
                           headers={'Authorization': 'Bearer fixture'}, encode_chunked=True)
            response = client.getresponse(); self.assertEqual(response.status, 200); response.read(); client.close()
            self.assertEqual(observed, [('/indexes/content/documents/fetch', None, 'Bearer fixture', body)])
            self.assertEqual(proxy.fault.state()['forwarded'], 0)
        finally:
            for server in (proxy, native): server.shutdown(); server.server_close()
            for thread in threads: thread.join(timeout=2)

    def binding(self, **extra):
        return dict(key=str(uuid.uuid4()), run_id='native-1', protocol='kafka', topic='__addp_dlq.2.3', **extra)

    def test_immutable_binding_rejects_foreign_run_and_non_dlq_mutations(self):
        for update in ({'run_id': 'foreign'}, {'topic': 'business-topic'}, {'topic': '__addp_dlq.0.3'},
                       {'key': 'bad'}, {'extra': 'private'}, {'protocol': 'tcp'}):
            with self.subTest(update=update), self.assertRaises((ValueError, KeyError)):
                fault = M.Fault('native-1'); value = self.binding(); value.update(update); fault.arm(value)
        fault = M.Fault('native-1'); fault.arm(self.binding())
        with self.assertRaises(ValueError): fault.arm(self.binding())
        self.assertEqual(fault.take('kafka', topic='unrelated'), 'pass')
        self.assertEqual(fault.take('kafka', topic='__addp_dlq.2.3'), 'drop')
        self.assertFalse(fault.state()['upstream_response'])
        self.assertEqual(fault.take('kafka', topic='__addp_dlq.2.3'), 'block')
        self.assertEqual(fault.state()['forwarded'], 1)
        self.assertEqual(fault.state()['blocked_replays'], 1)

    def test_delete_topics_decodes_all_pinned_versions_and_rejects_truncation(self):
        for version in range(7):
            packet = request(version, ['__addp_dlq.2.3'])
            self.assertEqual(M.delete_topics(packet), (17, ['__addp_dlq.2.3']))
            for cut in range(4, len(packet)):
                with self.assertRaises((ValueError, struct.error)):
                    M.delete_topics(packet[:cut])
            with self.assertRaises(ValueError): M.delete_topics(packet + b'\x00')
        with self.assertRaises(ValueError): M.delete_topics(request(7, ['__addp_dlq.2.3']))
        self.assertEqual(M.delete_topics(request(3, ['a', 'b'])), (17, ['a', 'b']))

    def test_real_http_forward_discards_only_bound_response_and_blocks_repeat(self):
        effects = []
        class Native(BaseHTTPRequestHandler):
            def log_message(self, *_): pass
            def do_DELETE(self):
                effects.append((self.path, self.headers.get('Host')))
                self.send_response(204); self.end_headers()
        native = ThreadingHTTPServer(('127.0.0.1', 0), Native)
        proxy = ThreadingHTTPServer(('127.0.0.1', 0), M.HTTPProxy)
        proxy.upstream, proxy.fault = native.server_address, M.Fault('native-1')
        proxy.fault.arm(dict(key=str(uuid.uuid4()), run_id='native-1', protocol='http', method='DELETE',
                             path='/manager/owned.glb', correlated=False, body_sha256=hashlib.sha256(b'').hexdigest()))
        threads = [threading.Thread(target=s.serve_forever, daemon=True) for s in (native, proxy)]
        for thread in threads: thread.start()
        try:
            client = http.client.HTTPConnection(*proxy.server_address, timeout=2)
            client.request('DELETE', '/manager/owned.glb', headers={'Host': 'signed-host'})
            with self.assertRaises(http.client.RemoteDisconnected): client.getresponse()
            client.close()
            self.assertEqual(effects, [('/manager/owned.glb', 'signed-host')])
            self.assertTrue(proxy.fault.state()['upstream_response'])
            client = http.client.HTTPConnection(*proxy.server_address, timeout=2)
            client.request('DELETE', '/manager/owned.glb')
            self.assertEqual(client.getresponse().status, 503); client.close()
            self.assertEqual(len(effects), 1)
        finally:
            for server in (proxy, native): server.shutdown(); server.server_close()
            for thread in threads: thread.join(timeout=2)

    def test_http_head_preserves_size_and_meili_fault_requires_bound_filter_and_correlation(self):
        effects = []
        class Native(BaseHTTPRequestHandler):
            def log_message(self, *_): pass
            def do_HEAD(self):
                self.send_response(200); self.send_header('Content-Length', '17'); self.end_headers()
            def do_POST(self):
                effects.append((self.path, self.rfile.read(int(self.headers['Content-Length']))))
                self.send_response(202); self.send_header('Content-Length', '2'); self.end_headers(); self.wfile.write(b'{}')
        native = ThreadingHTTPServer(('127.0.0.1', 0), Native)
        proxy = ThreadingHTTPServer(('127.0.0.1', 0), M.HTTPProxy)
        proxy.upstream, proxy.fault = native.server_address, M.Fault('native-1')
        path = '/indexes/content/documents/delete'
        body = b'{"filter":"tenant_id = 2 AND engine_id = 9"}'
        proxy.fault.arm(dict(key=str(uuid.uuid4()), run_id='native-1', protocol='http', method='POST', path=path,
            correlated=True, body_sha256=hashlib.sha256(body).hexdigest()))
        threads = [threading.Thread(target=s.serve_forever, daemon=True) for s in (native, proxy)]
        for thread in threads: thread.start()
        try:
            client = http.client.HTTPConnection(*proxy.server_address, timeout=2)
            client.request('HEAD', '/manager/owned.glb')
            response = client.getresponse(); self.assertEqual(response.getheader('Content-Length'), '17')
            response.read(); client.close()
            # Same endpoint with another filter is ordinary traffic, not this fault.
            client = http.client.HTTPConnection(*proxy.server_address, timeout=2)
            client.request('POST', path + '?customMetadata=' + str(uuid.uuid4()), b'{"filter":"engine_id = 10"}')
            self.assertEqual(client.getresponse().status, 202); client.close()
            self.assertEqual(proxy.fault.state()['forwarded'], 0)
            client = http.client.HTTPConnection(*proxy.server_address, timeout=2)
            client.request('POST', path + '?customMetadata=' + str(uuid.uuid4()), iter([body[:7], body[7:]]), encode_chunked=True)
            with self.assertRaises(http.client.RemoteDisconnected): client.getresponse()
            client.close()
            self.assertEqual(len(effects), 2)
            self.assertTrue(proxy.fault.state()['upstream_response'])
        finally:
            for server in (proxy, native): server.shutdown(); server.server_close()
            for thread in threads: thread.join(timeout=2)

    def test_actual_put_stays_before_upstream_until_bound_release_then_writes_once(self):
        effects, outcomes = [], []
        class Native(BaseHTTPRequestHandler):
            def log_message(self, *_): pass
            def do_PUT(self):
                effects.append((self.path, self.rfile.read(int(self.headers['Content-Length']))))
                self.send_response(200); self.send_header('Content-Length', '0'); self.end_headers()
        native = ThreadingHTTPServer(('127.0.0.1', 0), Native)
        proxy = ThreadingHTTPServer(('127.0.0.1', 0), M.HTTPProxy)
        proxy.upstream, proxy.fault = native.server_address, M.Fault('native-1')
        key = str(uuid.uuid4()); path = '/manager/tenant_2/model3d-quick-view/owned/execution.glb'
        proxy.fault.arm(dict(key=key, run_id='native-1', protocol='http-write', path=path))
        threads = [threading.Thread(target=s.serve_forever, daemon=True) for s in (native, proxy)]
        for thread in threads: thread.start()
        def write():
            client = http.client.HTTPConnection(*proxy.server_address, timeout=4)
            try:
                client.request('PUT', path, b'glTF-owned-output')
                outcomes.append(client.getresponse().status)
            except Exception as error: outcomes.append(type(error).__name__)
            finally: client.close()
        writer = threading.Thread(target=write, daemon=True); writer.start()
        try:
            deadline = time.monotonic() + 2
            while proxy.fault.state()['entered'] != 1 and time.monotonic() < deadline: time.sleep(.005)
            self.assertEqual(proxy.fault.state()['entered'], 1)
            self.assertEqual(effects, []); self.assertEqual(proxy.fault.state()['forwarded'], 0)
            with self.assertRaises(ValueError): proxy.fault.release({'run_id': 'foreign', 'key': key})
            self.assertEqual(effects, [])
            proxy.fault.release({'run_id': 'native-1', 'key': key})
            writer.join(timeout=3)
            self.assertEqual(outcomes, [200]); self.assertEqual(effects, [(path, b'glTF-owned-output')])
            client = http.client.HTTPConnection(*proxy.server_address, timeout=2)
            client.request('PUT', path, b'late-repeat')
            self.assertEqual(client.getresponse().status, 503); client.close()
            self.assertEqual(len(effects), 1)
            self.assertEqual(proxy.fault.state(), dict(run_id='native-1', key=key, forwarded=1,
                upstream_response=True, blocked_replays=1, entered=1, released=True, expired=False))
        finally:
            proxy.fault.release_event.set()
            for server in (proxy, native): server.shutdown(); server.server_close()
            for thread in threads: thread.join(timeout=2)
            writer.join(timeout=2)

    def test_writer_timeout_never_forwards_and_rejects_late_release_or_rebind(self):
        fault = M.Fault('native-1'); key = str(uuid.uuid4())
        path = '/manager/tenant_2/model3d-quick-view/fp/result.glb'
        for invalid in ('/other/result.glb', '/manager/tenant_0/model3d-quick-view/a.glb',
                        '/manager/tenant_2/model3d-quick-view/../other.glb',
                        '/manager/tenant_2/model3d-quick-view/%2e%2e/other.glb'):
            with self.subTest(path=invalid), self.assertRaises(ValueError):
                M.Fault('native-1').arm(dict(run_id='native-1', key=key, protocol='http-write', path=invalid))
        binding = dict(run_id='native-1', key=key, protocol='http-write', path=path)
        fault.arm(binding)
        with self.assertRaises(ValueError): fault.arm(binding)
        self.assertEqual(fault.take_write('PUT', '/unrelated'), 'pass')
        self.assertEqual(fault.take_write('GET', path), 'pass')
        self.assertEqual(fault.take_write('PUT', path), 'defer')
        with patch.object(M, 'WRITE_WAIT_SECONDS', 0): self.assertFalse(fault.await_write())
        self.assertTrue(fault.state()['expired']); self.assertEqual(fault.state()['forwarded'], 0)
        with self.assertRaises(ValueError): fault.release({'run_id': 'native-1', 'key': key})
        self.assertEqual(fault.take_write('PUT', path), 'block')
        self.assertEqual(fault.state()['forwarded'], 0)

    def test_kafka_forward_binds_response_correlation_before_discarding(self):
        native = socket.socket(); native.bind(('127.0.0.1', 0)); native.listen()
        effects, errors = [], []
        def broker():
            try:
                connection, _ = native.accept()
                with connection:
                    packet = M.frame(connection); effects.append(M.delete_topics(packet))
                    response = struct.pack('>i', 17) + b'native-response'
                    connection.sendall(struct.pack('>i', len(response)) + response)
            except Exception as error: errors.append(type(error).__name__)
        broker_thread = threading.Thread(target=broker, daemon=True); broker_thread.start()
        proxy = M.TCPServer(('127.0.0.1', 0), M.KafkaProxy)
        proxy.upstream, proxy.fault = native.getsockname(), M.Fault('native-1'); proxy.fault.arm(self.binding())
        thread = threading.Thread(target=proxy.serve_forever, daemon=True); thread.start()
        try:
            with socket.create_connection(proxy.server_address, timeout=2) as client:
                client.sendall(request(6, ['__addp_dlq.2.3']))
                self.assertEqual(client.recv(4), b'')
            self.assertEqual(effects, [(17, ['__addp_dlq.2.3'])])
            self.assertTrue(proxy.fault.state()['upstream_response'])
            self.assertEqual(proxy.fault.state()['forwarded'], 1)
            self.assertEqual(errors, [])
        finally:
            proxy.shutdown(); proxy.server_close(); native.close()
            thread.join(timeout=2); broker_thread.join(timeout=2)


if __name__ == '__main__': unittest.main()
