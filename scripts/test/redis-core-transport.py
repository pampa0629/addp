#!/usr/bin/env python3
"""Private Redis fault transports and byte-preserving HTTP counters for R1 T4.

Usage: python3 -m scripts.test.redis-core-transport CONFIG.json
Only the owner-only Unix socket controls faults; no product debug routes exist.
"""
from __future__ import annotations

import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import select
import signal
import socket
import socketserver
import threading
from urllib.parse import urlsplit


class FaultProxy(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, upstream, port=0):
        self.upstream = tuple(upstream)
        self.available = True
        self.connections = set()
        self.lock = threading.Lock()
        owner = self

        class Handler(socketserver.BaseRequestHandler):
            def handle(self):
                outgoing = None
                try:
                    with owner.lock:
                        if not owner.available:
                            return
                        owner.connections.add(self.request)
                    outgoing = socket.create_connection(owner.upstream, timeout=2)
                    with owner.lock:
                        if not owner.available:
                            return
                        owner.connections.add(outgoing)
                    while True:
                        ready, _, _ = select.select([self.request, outgoing], [], [], 1)
                        for source in ready:
                            data = source.recv(65536)
                            if not data:
                                return
                            (outgoing if source is self.request else self.request).sendall(data)
                except (OSError, ValueError):
                    pass  # A closed transport is exactly the injected failure.
                finally:
                    with owner.lock:
                        for connection in (self.request, outgoing):
                            if connection is not None:
                                owner.connections.discard(connection)
                                connection.close()

        super().__init__(("127.0.0.1", port), Handler)

    def set_available(self, available):
        if type(available) is not bool:
            raise ValueError("fault availability must be boolean")
        with self.lock:
            self.available = available
            if not available:
                for connection in list(self.connections):
                    try:
                        connection.shutdown(socket.SHUT_RDWR)
                    except OSError:
                        pass
                    connection.close()
        return {"available": available}


HOP_HEADERS = {"connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
               "te", "trailer", "transfer-encoding", "upgrade"}


class HTTPObserver(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, upstream, paths, port=0):
        parsed = urlsplit(upstream)
        if parsed.scheme != "http" or parsed.hostname not in {"127.0.0.1", "localhost"} or parsed.path:
            raise ValueError("observer upstream must be an owned loopback HTTP origin")
        self.upstream = (parsed.hostname, parsed.port or 80)
        self.paths = set(paths)
        self.records = {}
        self.invalid = False
        self.lock = threading.Lock()
        owner = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def relay(self):
                identity = self.headers.get("X-Request-ID", "")
                tracked = (self.command, self.path.split("?", 1)[0]) in owner.paths
                row = None
                if tracked and identity:
                    with owner.lock:
                        if len(owner.records) >= 4096 or identity in owner.records:
                            owner.invalid = True
                        else:
                            row = {"received": 1, "forwarded": 0, "completed": 0, "status": None}
                            owner.records[identity] = row
                upstream = http.client.HTTPConnection(*owner.upstream, timeout=70)
                try:
                    # Product Query and Token clients use Content-Length. Do not
                    # silently forward unsupported request framing as empty data.
                    if self.headers.get("Transfer-Encoding"):
                        raise ValueError("unsupported request framing")
                    length = int(self.headers.get("Content-Length", "0"))
                    if length < 0 or length > 1048576:
                        raise ValueError("observer request too large")
                    body = self.rfile.read(length) if length else None
                    forbidden = HOP_HEADERS | {x.strip().lower() for x in self.headers.get("Connection", "").split(",")}
                    headers = {k: v for k, v in self.headers.items() if k.lower() not in forbidden}
                    upstream.connect()
                    upstream.request(self.command, self.path, body=body, headers=headers)
                    if row is not None:
                        with owner.lock:
                            row["forwarded"] += 1
                    response = upstream.getresponse()
                    content = response.read(1048577)
                    if len(content) > 1048576:
                        raise ValueError("observer response too large")
                    if row is not None:
                        with owner.lock:
                            row.update(completed=1, status=response.status)
                    self.send_response(response.status)
                    blocked = HOP_HEADERS | {"content-length"}
                    blocked |= {x.strip().lower() for x in response.getheader("Connection", "").split(",")}
                    for key, value in response.getheaders():
                        if key.lower() not in blocked:
                            self.send_header(key, value)
                    self.send_header("Content-Length", str(len(content)))
                    self.end_headers()
                    if self.command != "HEAD":
                        self.wfile.write(content)
                except (OSError, ValueError, http.client.HTTPException):
                    # Mark observation incomplete even if the caller got 502.
                    # A transport error cannot satisfy an accepted business status.
                    if tracked and identity:
                        with owner.lock:
                            owner.invalid = True
                    self.close_connection = True
                    try:
                        self.send_error(502, "observer_transport_failed")
                    except OSError:
                        pass
                finally:
                    upstream.close()

            do_GET = do_POST = do_PUT = do_DELETE = do_PATCH = do_HEAD = do_OPTIONS = relay

            def log_message(self, *_):
                pass  # Never retain URLs, credentials, bodies or raw errors.

        super().__init__(("127.0.0.1", port), Handler)

    def snapshot(self):
        with self.lock:
            return {"valid": not self.invalid, "records": {k: dict(v) for k, v in self.records.items()}}


class Rig:
    def __init__(self, config):
        self.faults = {name: FaultProxy(config["redis_upstream"], config.get("ports", {}).get(name, 0))
                       for name in ("gateway", "system")}
        self.http = {name: HTTPObserver(config[name + "_upstream"],
                        [tuple(x) for x in config[name + "_paths"]], config.get("ports", {}).get(name + "_http", 0))
                     for name in ("system", "service")}
        self.threads = []

    def start(self):
        for server in [*self.faults.values(), *self.http.values()]:
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            self.threads.append(thread)
        return {name: server.server_address[1] for name, server in self.faults.items()} | {
            name + "_http": server.server_address[1] for name, server in self.http.items()}

    def close(self):
        for server in self.faults.values():
            server.set_available(False)
        for server in [*self.faults.values(), *self.http.values()]:
            server.shutdown()
            server.server_close()
        for thread in self.threads:
            thread.join(timeout=3)

    def command(self, value):
        if value.get("action") == "fault" and value.get("owner") in self.faults:
            return self.faults[value["owner"]].set_available(value["available"])
        if value == {"action": "snapshot"}:
            return {name: server.snapshot() for name, server in self.http.items()}
        raise ValueError("invalid private transport command")


def control(path, command):
    with socket.socket(socket.AF_UNIX) as connection:
        connection.settimeout(3)
        connection.connect(str(path))
        connection.sendall(json.dumps(command).encode() + b"\n")
        with connection.makefile("rb") as stream:
            value = json.loads(stream.readline(1 << 20))
    if not value.get("ok"):
        raise RuntimeError("private transport control failed")
    return value["result"]


def main(config_path):
    os.umask(0o077)
    config_path = Path(config_path)
    if config_path.stat().st_mode & 0o077:
        raise ValueError("transport configuration must be owner-only")
    config = json.loads(config_path.read_text())
    rig = Rig(config)
    path = Path(config["control"])
    stop = threading.Event()

    class Control(socketserver.StreamRequestHandler):
        def handle(self):
            try:
                result = rig.command(json.loads(self.rfile.readline(8192)))
                answer = {"ok": True, "result": result}
            except (ValueError, KeyError, TypeError):
                answer = {"ok": False}
            self.wfile.write(json.dumps(answer).encode() + b"\n")

    server = socketserver.ThreadingUnixStreamServer(str(path), Control)
    server.daemon_threads = True
    try:
        ports = rig.start()
        Path(config["ready"]).write_text(json.dumps(ports))
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        for signum in (signal.SIGTERM, signal.SIGINT):
            signal.signal(signum, lambda *_: stop.set())
        stop.wait()
        server.shutdown()
        thread.join(timeout=3)
    finally:
        server.server_close()
        rig.close()
        path.unlink(missing_ok=True)


if __name__ == "__main__":
    import sys
    main(sys.argv[1])
