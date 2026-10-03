"""External fault tools owned exclusively by the disposable Orchestrator T4."""

from __future__ import annotations

import argparse
from contextlib import ExitStack
import http.client
import json
import os
import re
import select
import signal
import socket
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

HOST = "addp-orchestrator-meta.test"
UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
MODES = {"pass", "lose_response", "hold_status"}
HOP_HEADERS = {"connection", "proxy-connection", "proxy-authorization", "keep-alive", "transfer-encoding", "upgrade"}


def write_json(path, value):
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(value, sort_keys=True) + "\n")
    temporary.chmod(0o600)
    temporary.replace(path)


def deployment_secret():
    if (sys.platform != "linux" or os.environ.get("GITHUB_ACTIONS") != "true"
            or os.environ.get("RUNNER_OS") != "Linux" or os.environ.get("ADDP_ONLINE_HOSTED") != "1"
            or os.environ.get("ADDP_ONLINE_HOST") != "1" or os.environ.get("POSTGRES_DB") != "addp_online"):
        raise ValueError("faults require the disposable Hosted Online deployment")
    secret = Path(os.environ["ADDP_ONLINE_SECRET_DIR"]).resolve(strict=True)
    runner = Path(os.environ["RUNNER_TEMP"]).resolve(strict=True)
    if (secret.parent != runner or not re.fullmatch(r"addp-online-secret-[A-Za-z0-9_.-]+", secret.name)
            or secret.stat().st_uid != os.getuid() or secret.stat().st_mode & 0o777 != 0o700
            or secret.is_relative_to(Path(__file__).resolve().parents[2])):
        raise ValueError("fault secret directory is not owned by this deployment")
    return secret


def network_alias(secret, action):
    # sudo may edit only this one fixed hosts entry. Removal preserves all other entries.
    line = f"127.0.0.1 {HOST} # {secret.name}\n"
    program = '''import pathlib, sys
p = pathlib.Path('/etc/hosts')
line, host, action = sys.argv[1:]
text = p.read_text()
if action == 'add':
    if any(host in s.split('#', 1)[0].split() for s in text.splitlines()):
        raise SystemExit('fault alias already exists')
    p.write_text(text + ('' if text.endswith('\\n') else '\\n') + line)
else:
    if text.splitlines(keepends=True).count(line) > 1:
        raise SystemExit('fault alias ownership is ambiguous')
    p.write_text(''.join(s for s in text.splitlines(keepends=True) if s != line))
'''
    subprocess.run(["sudo", "-n", "python3", "-c", program, line, HOST, action],
                   check=True, capture_output=True, timeout=10)


class FaultProxy(ThreadingHTTPServer):
    daemon_threads = True
    block_on_close = False

    def __init__(self, directory, target_port):
        self.directory, self.target_port = directory, target_port
        self.lock, self.stopping = threading.Lock(), threading.Event()
        self.witness = {}
        super().__init__(("127.0.0.1", 0), ProxyHandler)

    def command(self):
        path = self.directory / "command.json"
        if not path.exists():
            return None
        command = json.loads(path.read_text())
        if (set(command) != {"case_id", "task_id", "mode"}
                or not re.fullmatch(r"[a-z_]+", command["case_id"])
                or not isinstance(command["task_id"], int) or command["task_id"] <= 0
                or command["mode"] not in MODES):
            raise ValueError("invalid fault command")
        return command

    def record(self, command, parent=None, child=None, error=None, held=False):
        with self.lock:
            if self.witness.get("case_id") != command["case_id"]:
                self.witness = {"case_id": command["case_id"], "posts": 0, "child_execution_ids": [],
                                "held_status_requests": 0}
            if parent is not None:
                self.witness["posts"] += 1
                if self.witness.get("parent_execution_id", parent) != parent:
                    error = "parent_mismatch"
                self.witness["parent_execution_id"] = parent
            if child:
                self.witness["child_execution_ids"].append(child)
            if error:
                self.witness["error"] = error
            if held:
                self.witness["held_status_requests"] += 1
            write_json(self.directory / "witness.json", self.witness)


class ProxyHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):
        pass  # Headers, bodies and URLs never enter process logs or evidence.

    def do_GET(self):
        self.forward()

    def do_POST(self):
        self.forward()

    def forward(self):
        server, connection, command = self.server, None, None
        try:
            parsed = urlsplit(self.path)
            if (parsed.scheme != "http" or parsed.hostname != HOST or parsed.port != server.target_port
                    or parsed.username is not None or parsed.fragment):
                self.send_error(403, "unowned proxy target")
                return
            length = int(self.headers.get("Content-Length", "0"))
            if length < 0 or length > 1024 * 1024 or self.headers.get("Transfer-Encoding"):
                self.send_error(413, "invalid proxy body")
                return
            self.connection.settimeout(10)
            body = self.rfile.read(length)
            if len(body) != length:
                raise ValueError("incomplete body")
            command = server.command()
            target_post = bool(command and self.command == "POST" and
                parsed.path == f'/api/v1/meta/task-provider/tasks/scan/{command["task_id"]}/execute')
            if target_post:
                parent = json.loads(body).get("parent_execution_id")
                if not isinstance(parent, str) or not UUID.fullmatch(parent):
                    raise ValueError("missing real parent identity")
                server.record(command, parent=parent)
            with server.lock:
                known_children = tuple(server.witness.get("child_execution_ids", []))
            if (command and command["mode"] == "hold_status" and self.command == "GET"
                    and parsed.path in ["/api/v1/meta/task-provider/executions/" + x for x in known_children]):
                server.record(command, held=True)
                deadline = time.monotonic() + 15
                while not server.stopping.wait(0.05) and time.monotonic() < deadline:
                    if server.command()["mode"] != "hold_status":
                        break
            headers = {k: v for k, v in self.headers.items() if k.lower() not in HOP_HEADERS | {"host"}}
            connection = http.client.HTTPConnection("127.0.0.1", server.target_port, timeout=35)
            connection.request(self.command, parsed.path + ("?" + parsed.query if parsed.query else ""), body, headers)
            response = connection.getresponse()
            raw = response.read(4 * 1024 * 1024 + 1)
            if len(raw) > 4 * 1024 * 1024:
                raise ValueError("oversized upstream response")
            if target_post:
                child = json.loads(raw).get("execution_id")
                if response.status != 202 or not isinstance(child, str) or not UUID.fullmatch(child):
                    server.record(command, error="acceptance_unproven")
                    raise ValueError("upstream did not prove acceptance")
                server.record(command, child=child)
                if command["mode"] == "lose_response":
                    # Real Owner has committed its execution. No response bytes reach Orchestrator.
                    self.close_connection = True
                    self.connection.shutdown(socket.SHUT_RDWR)
                    return
            self.send_response(response.status)
            for key, value in response.getheaders():
                if key.lower() not in HOP_HEADERS | {"content-length"}:
                    self.send_header(key, value)
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)
        except (OSError, ValueError, KeyError, TypeError, http.client.HTTPException):
            self.close_connection = True
            # Errors before downstream acceptance cannot masquerade as the requested fault.
            if command and not isinstance(sys.exc_info()[1], (BrokenPipeError, ConnectionResetError)):
                server.record(command, error="proxy_failure")
        finally:
            if connection is not None:
                connection.close()


def validate_process(process, root, secret):
    executable = (process / "exe").resolve(strict=True)
    environment = dict(x.split("=", 1) for x in (process / "environ").read_bytes().decode().split("\0") if "=" in x)
    if (executable != root / ".dev-bins/addp-orchestrator" or process.stat().st_uid != os.getuid()
            or environment.get("ADDP_ONLINE_SECRET_DIR") != str(secret)
            or environment.get("POSTGRES_DB") != "addp_online"
            or environment.get("ADDP_ONLINE_HOSTED") != "1"):
        raise ValueError("refusing to signal an unowned Orchestrator process")


class HostedFaults:
    def __init__(self):
        self.secret = deployment_secret()
        self.directory = self.secret / "orchestrator-faults"
        self.root = Path(__file__).resolve().parents[2]
        self.case_id = None
        self.crashed_pid = None

    def arm(self, case_id, task_id, mode):
        self.case_id = case_id
        write_json(self.directory / "command.json", {"case_id": case_id, "task_id": task_id, "mode": mode})

    def release(self):
        path = self.directory / "command.json"
        if path.exists():
            command = json.loads(path.read_text())
            command["mode"] = "pass"
            write_json(path, command)

    def witness(self):
        path = self.directory / "witness.json"
        if not path.exists():
            return {}
        value = json.loads(path.read_text())
        return value if value.get("case_id") == self.case_id else {}

    def crash(self):
        pid = int((self.root / ".dev-pids/orchestrator.pid").read_text().strip())
        if pid <= 1:
            raise ValueError("invalid Orchestrator PID")
        with ExitStack() as stack:
            descriptor = os.pidfd_open(pid)
            stack.callback(os.close, descriptor)
            validate_process(Path("/proc") / str(pid), self.root, self.secret)
            signal.pidfd_send_signal(descriptor, signal.SIGKILL)
            ready, _, _ = select.select([descriptor], [], [], 10)
            if not ready:
                raise ValueError("Orchestrator did not exit after SIGKILL")
            self.crashed_pid = pid

    def restart(self):
        if self.crashed_pid is None:
            raise ValueError("replacement requires a proven crashed process")
        proxy = os.environ["ADDP_ONLINE_ORCHESTRATOR_PROXY_URL"]
        with (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "orchestrator-replacement.log").open("a") as output:
            subprocess.run(["bash", "scripts/dev/start.sh", "-orchestrator"], cwd=self.root,
                           env={**os.environ, "HTTP_PROXY": proxy, "NO_PROXY": "127.0.0.1,localhost", "SKIP_MODTIDY": "1"},
                           stdout=output, stderr=subprocess.STDOUT, check=True, timeout=180)
        pid = int((self.root / ".dev-pids/orchestrator.pid").read_text().strip())
        if pid <= 1 or pid == self.crashed_pid:
            raise ValueError("replacement did not create a distinct Backend process")
        validate_process(Path("/proc") / str(pid), self.root, self.secret)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("alias-add", "alias-remove", "proxy"))
    parser.add_argument("--target-port", type=int, default=8082)
    args = parser.parse_args()
    try:
        secret = deployment_secret()
        if args.action != "proxy":
            network_alias(secret, "add" if args.action == "alias-add" else "remove")
            return 0
        if not 1 <= args.target_port <= 65535:
            raise ValueError("invalid Meta target port")
        directory = secret / "orchestrator-faults"
        directory.mkdir(mode=0o700)
        with FaultProxy(directory, args.target_port) as server:
            for signum in (signal.SIGTERM, signal.SIGINT):
                signal.signal(signum, lambda *_: server.stopping.set())
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            environment = secret / "orchestrator-proxy.env"
            environment.write_text(f"ADDP_ONLINE_ORCHESTRATOR_PROXY_URL=http://127.0.0.1:{server.server_port}\n")
            environment.chmod(0o600)
            server.stopping.wait()
            server.shutdown()
            thread.join(timeout=5)
        return 0
    except (OSError, ValueError, KeyError, subprocess.SubprocessError):
        print("Orchestrator Hosted fault tool failed", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
