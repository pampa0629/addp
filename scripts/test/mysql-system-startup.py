"""Observe the installed System process using only its own runtime environment."""
import base64
from datetime import datetime
import json
import os
from pathlib import Path
import re
import secrets
import signal
import socket
import subprocess
import sys
import time
from urllib.error import HTTPError, URLError
from urllib.request import ProxyHandler, Request, build_opener
from uuid import uuid4

work = Path(sys.argv[1]).resolve()
root = Path(__file__).resolve().parents[2]
with socket.socket() as reservation:
    reservation.bind(("127.0.0.1", 0))
    port = reservation.getsockname()[1]

env = {name: os.environ[name] for name in (
    "ADDP_INFRA_DATABASE_PROVIDER", "ADDP_MYSQL_HOST", "ADDP_MYSQL_PORT",
    "ADDP_MYSQL_SYSTEM_USER", "ADDP_MYSQL_SYSTEM_PASSWORD", "REDIS_PASSWORD", "REDIS_PORT", "IAM_MFA_ENCRYPTION_KEY",
)}
env.update(ENV="production", PROJECT_ROOT=str(work), TZ="Asia/Shanghai", REDIS_HOST="127.0.0.1",
           SYSTEM_BACKEND_PORT=str(port), SERVICE_HOST="127.0.0.1")
for key in ("ENCRYPTION_KEY", "OAUTH_USER_CODE_PEPPER"):
    env[key] = base64.b64encode(secrets.token_bytes(32)).decode()
for key in re.findall(r"^([A-Z][A-Z0-9_]*_SERVICE_CLIENT_SECRET)=", (root/".env.example").read_text(), re.M):
    env[key] = secrets.token_hex(32)

# Probes address the disposable loopback listener directly on both Linux and macOS.
http = build_opener(ProxyHandler({}))

def fetch(path, phase):
    request_id = str(uuid4())
    request = Request(f"http://127.0.0.1:{port}{path}", headers={"X-Request-ID": request_id})
    try:
        response = http.open(request, timeout=3)
    except HTTPError as error:
        response = error
    with response:
        body = json.load(response)
        observed_id = response.headers.get("X-Request-ID", request_id)
        print(f"System HTTP phase={phase} status={response.status} request_id={observed_id}", flush=True)
        if path == "/health/ready" and response.status != 200:
            print("System readiness checks:", [(c["name"], c["status"], c.get("error_code")) for c in body["checks"]], flush=True)
        return response.status, body

log_path = work / "system-runtime.log"
with log_path.open("w") as log:
    process = subprocess.Popen([str(work / "system-server")], cwd=work, env=env, stdout=log, stderr=log)
    try:
        deadline = time.monotonic()+40
        while process.poll() is None and time.monotonic()<deadline:
            try:
                status, health = fetch("/health/ready", "startup")
            except (URLError, TimeoutError):
                time.sleep(0.2)
                continue
            if status == 200 and health["status"] == "ready" and health["module"] == "system":
                assert any(c["name"] == "redis" and c["status"] == "ready" for c in health["checks"]), health
                break
            time.sleep(0.2)
        else:
            raise RuntimeError(f"System failed to become ready, exit={process.poll()}")
        assert fetch("/api/v1/system/engines", "unauthenticated")[0] == 401, "unauthenticated engine request was not rejected"
        login_env = {key: os.environ[key] for key in (
            "ADDP_TEST_GATEWAY_PUBLISHER_SECRET", "ADDP_TEST_GATEWAY_RUNTIME_PASSWORD", "ADDP_TEST_MYSQL_MEMBER_PASSWORD")}
        for key in ("ADDP_INFRA_DATABASE_PROVIDER","ADDP_MYSQL_HOST","ADDP_MYSQL_PORT","GATEWAY_SERVICE_CLIENT_SECRET","REDIS_PORT","REDIS_PASSWORD"):
            login_env[key] = env[key]
        subprocess.run([sys.executable, str(root/"scripts/test/mysql-gateway-login.py"), str(work), f"http://127.0.0.1:{port}"],
                       env=login_env, check=True, timeout=100)
        # Wait through the registration cleanup tick, then confirm the same live process.
        try:
            process.wait(timeout=11)
        except subprocess.TimeoutExpired:
            pass
        assert process.poll() is None and fetch("/health/ready", "after-maintenance")[0] == 200
    finally:
        if process.poll() is None:
            process.send_signal(signal.SIGTERM)
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
        log.flush()
        output = log_path.read_text(errors="replace")
        bad = [line for line in output.splitlines()
               if re.search(r'panic:|Error [0-9]{4} |"level":"ERROR"|level=ERROR|\[error\]|FATAL', line, re.I)]
        # The System watch handler ends without writing if Gateway disconnects.
        # HTTP metrics classify that canceled long poll as an error with status 200.
        stopped_at = work / "gateway-stop-time"
        if stopped_at.exists():
            shutdown = float(stopped_at.read_text())
            unexpected = []
            for line in bad:
                record = json.loads(line) if line.startswith("{") else {}
                if (record.get("msg") == "http.request.completed" and record.get("method") == "GET"
                        and record.get("route") == "/api/v1/system/runtime/modules/watch"
                        and record.get("status") == 200 and datetime.fromisoformat(record["time"]).timestamp() >= shutdown):
                    print("Gateway disconnected its registry watch on shutdown, request_id=" + record["request_id"])
                else:
                    unexpected.append(line)
            bad = unexpected
        if bad or process.returncode:
            diagnostic = "\n".join(bad or output.splitlines()[-6:])
            for key, value in env.items():
                if value and any(word in key for word in ("PASSWORD","SECRET","KEY","PEPPER")):
                    diagnostic = diagnostic.replace(value,"[redacted]")
            # SQL parameters or generated credential hashes must stay out of the CI log.
            diagnostic = re.sub(r'\$2[aby]\$[./A-Za-z0-9$]{56}', '[hash]', diagnostic)
            raise RuntimeError(f"System runtime exit={process.returncode}: {diagnostic[:2500]}")

for message in ("执行过程事件保留清理完成", "执行概览只读保留评估完成"):
    assert message in output, f"missing startup maintenance result: {message}"
print("System MySQL normal entry: ready with Redis, protected route returned 401, maintenance and graceful shutdown observed")
