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
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

HOST = "addp-orchestrator-meta.test"
UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")
MODES = {"pass", "lose_response", "hold_status", "hold_renewal", "drop_scope"}
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

    def record(self, command, parent=None, child=None, error=None, held=False, scope_dropped=False, cancelled=False):
        with self.lock:
            if self.witness.get("case_id") != command["case_id"]:
                self.witness = {"case_id": command["case_id"], "posts": 0, "child_execution_ids": [],
                                "held_status_requests": 0, "scope_drops": 0, "cancelled_status_requests": 0}
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
            if scope_dropped:
                self.witness["scope_drops"] += 1
            if cancelled:
                self.witness["cancelled_status_requests"] += 1
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
            if (command and command["mode"] == "drop_scope" and self.command == "GET"
                    and parsed.path == "/api/v1/meta/execution-read-scope"):
                # Refuse the connection before forwarding. Monitor must produce its own
                # unavailable response from a real transport failure, never a fake scope.
                server.record(command, scope_dropped=True)
                self.close_connection = True
                self.connection.shutdown(socket.SHUT_RDWR)
                return
            target_post = bool(command and self.command == "POST" and
                parsed.path == f'/api/v1/meta/task-provider/tasks/scan/{command["task_id"]}/execute')
            if target_post:
                parent = json.loads(body).get("parent_execution_id")
                if not isinstance(parent, str) or not UUID.fullmatch(parent):
                    raise ValueError("missing real parent identity")
                server.record(command, parent=parent)
            with server.lock:
                known_children = tuple(server.witness.get("child_execution_ids", []))
            if (command and command["mode"] in {"hold_status", "hold_renewal"} and self.command == "GET"
                    and parsed.path in ["/api/v1/meta/task-provider/executions/" + x for x in known_children]):
                server.record(command, held=True)
                deadline = time.monotonic() + (60 if command["mode"] == "hold_renewal" else 15)
                while not server.stopping.wait(0.05) and time.monotonic() < deadline:
                    if server.command()["mode"] != command["mode"]:
                        break
                    ready, _, _ = select.select([self.connection], [], [], 0)
                    if ready and self.connection.recv(1, socket.MSG_PEEK) == b"":
                        server.record(command, cancelled=True)
                        self.close_connection = True
                        return
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
        self.exited_pid = None
        self.renewal_schema = None
        self.renewal_process = None

    def pin_process(self):
        pid = int((self.root / ".dev-pids/orchestrator.pid").read_text().strip())
        if pid <= 1:
            raise ValueError("invalid Orchestrator PID")
        descriptor = os.pidfd_open(pid)
        try:
            validate_process(Path("/proc") / str(pid), self.root, self.secret)
        except BaseException:
            os.close(descriptor)
            raise
        return pid, descriptor

    def await_renewal_exit(self):
        if self.renewal_process is None:
            raise ValueError("renewal process is not pinned")
        pid, descriptor = self.renewal_process
        try:
            ready, _, _ = select.select([descriptor], [], [], 50)
            if not ready:
                raise ValueError("renewal failure did not stop its Backend")
            self.exited_pid = pid
        finally:
            os.close(descriptor)
            self.renewal_process = None

    def postgres(self, sql):
        # Never use the shared local database or accept an existing Infra container.
        if deployment_secret() != self.secret:
            raise ValueError("renewal fault deployment changed")
        inspected = subprocess.run(["docker", "inspect", "addp-postgres"], check=True,
                                   capture_output=True, text=True, timeout=10)
        container = json.loads(inspected.stdout)[0]
        labels = container["Config"]["Labels"]
        environment = dict(x.split("=", 1) for x in container["Config"]["Env"] if "=" in x)
        if (not container["State"]["Running"]
                or not os.environ.get("ADDP_ONLINE_ORCHESTRATOR_POSTGRES_ID")
                or container["Id"] != os.environ["ADDP_ONLINE_ORCHESTRATOR_POSTGRES_ID"]
                or labels.get("com.docker.compose.service") != "postgres"
                or labels.get("com.docker.compose.project.working_dir") != str(self.root)
                or environment.get("POSTGRES_DB") != "addp_online"
                or not environment.get("POSTGRES_PASSWORD")
                or any(environment.get(key) != os.environ.get(key) for key in ("POSTGRES_USER", "POSTGRES_PASSWORD"))):
            raise ValueError("renewal fault PostgreSQL is not owned by this deployment")
        result = subprocess.run(["docker", "exec", "-i", container["Id"], "psql", "-X", "-qAt",
            "-v", "ON_ERROR_STOP=1", "-U", environment["POSTGRES_USER"], "-d", "addp_online"],
            input=sql, check=True, capture_output=True, text=True, timeout=10)
        return result.stdout.strip()

    def reject_renewal(self, parent, tenant, task):
        if (not UUID.fullmatch(parent) or type(tenant) is not int or tenant <= 1
                or type(task) is not int or task <= 0 or self.renewal_schema is not None):
            raise ValueError("invalid renewal fault target")
        schema = "online_orch_renew_" + uuid.uuid4().hex
        self.renewal_process = self.pin_process()
        self.renewal_schema = schema
        # Sequence increments survive the rejected UPDATE transaction. No execution
        # fact, lease token, credential or result is copied into the fault evidence.
        self.postgres(f"""BEGIN;
DO $$ BEGIN
 IF NOT EXISTS (SELECT 1 FROM common.task_executions WHERE execution_id='{parent}'
  AND tenant_id={tenant} AND module='orchestrator' AND task_type='orchestration'
  AND source_task_id='{task}' AND status='running' AND lease_expires_at > now()
  AND metadata->'step_results'->'probe'->>'phase'='waiting') THEN
  RAISE EXCEPTION 'renewal fault target is not a live waiting parent';
 END IF;
END $$;
CREATE SCHEMA {schema};
CREATE SEQUENCE {schema}.rejections;
CREATE FUNCTION {schema}.reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 PERFORM nextval('{schema}.rejections');
 RAISE EXCEPTION 'Hosted renewal rejected' USING ERRCODE='55000';
END $$;
CREATE TRIGGER {schema} BEFORE UPDATE OF lease_expires_at ON common.task_executions
FOR EACH ROW WHEN (OLD.execution_id='{parent}' AND OLD.tenant_id={tenant}
 AND OLD.module='orchestrator' AND OLD.task_type='orchestration'
 AND OLD.status='running' AND NEW.status='running'
 AND NEW.lease_expires_at IS DISTINCT FROM OLD.lease_expires_at)
EXECUTE FUNCTION {schema}.reject();
COMMIT;""")

    def renewal_rejections(self):
        if self.renewal_schema is None:
            raise ValueError("renewal fault is not installed")
        return int(self.postgres(f"SELECT CASE WHEN is_called THEN last_value ELSE 0 END FROM {self.renewal_schema}.rejections;"))

    def clear_renewal(self):
        if self.renewal_process is not None:
            os.close(self.renewal_process[1])
            self.renewal_process = None
        if self.renewal_schema is None:
            return
        schema = self.renewal_schema
        self.postgres(f"""BEGIN;
DROP TRIGGER IF EXISTS {schema} ON common.task_executions;
DROP FUNCTION IF EXISTS {schema}.reject();
DROP SEQUENCE IF EXISTS {schema}.rejections;
DROP SCHEMA IF EXISTS {schema};
COMMIT;""")
        if self.postgres(f"SELECT count(*) FROM pg_namespace WHERE nspname='{schema}';") != "0":
            raise ValueError("renewal fault schema survived cleanup")
        self.renewal_schema = None

    def arm(self, case_id, task_id, mode):
        self.case_id = case_id
        write_json(self.directory / "command.json", {"case_id": case_id, "task_id": task_id, "mode": mode})

    def release(self):
        self.clear_renewal()
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
        with ExitStack() as stack:
            pid, descriptor = self.pin_process()
            stack.callback(os.close, descriptor)
            signal.pidfd_send_signal(descriptor, signal.SIGKILL)
            ready, _, _ = select.select([descriptor], [], [], 10)
            if not ready:
                raise ValueError("Orchestrator did not exit after SIGKILL")
            self.exited_pid = pid

    def restart(self):
        if self.exited_pid is None:
            raise ValueError("replacement requires a proven exited process")
        proxy = os.environ["ADDP_ONLINE_ORCHESTRATOR_PROXY_URL"]
        with (Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "orchestrator-replacement.log").open("a") as output:
            subprocess.run(["bash", "scripts/dev/start.sh", "-orchestrator"], cwd=self.root,
                           env={**os.environ, "HTTP_PROXY": proxy, "NO_PROXY": "127.0.0.1,localhost", "SKIP_MODTIDY": "1"},
                           stdout=output, stderr=subprocess.STDOUT, check=True, timeout=180)
        pid = int((self.root / ".dev-pids/orchestrator.pid").read_text().strip())
        if pid <= 1 or pid == self.exited_pid:
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
