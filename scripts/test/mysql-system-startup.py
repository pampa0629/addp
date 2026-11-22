"""Observe the installed System process using only its own runtime environment."""
import base64
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
from urllib.request import urlopen

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

def fetch(path):
    try:
        with urlopen(f"http://127.0.0.1:{port}{path}", timeout=2) as response:
            return response.status, json.load(response)
    except HTTPError as error:
        return error.code, None

log_path = work / "system-runtime.log"
with log_path.open("w") as log:
    process = subprocess.Popen([str(work / "system-server")], cwd=work, env=env, stdout=log, stderr=log)
    try:
        deadline = time.monotonic()+40
        while process.poll() is None and time.monotonic()<deadline:
            try:
                status, health = fetch("/health/ready")
            except (URLError, TimeoutError):
                time.sleep(0.2)
                continue
            if status == 200 and health["status"] == "ready" and health["module"] == "system":
                assert any(c["name"] == "redis" and c["status"] == "ready" for c in health["checks"]), health
                break
            time.sleep(0.2)
        else:
            raise RuntimeError(f"System failed to become ready, exit={process.poll()}")
        assert fetch("/api/v1/system/engines")[0] == 401, "unauthenticated engine request was not rejected"
        # Wait through the registration cleanup tick, then confirm the same live process.
        try:
            process.wait(timeout=11)
        except subprocess.TimeoutExpired:
            pass
        assert process.poll() is None and fetch("/health/ready")[0] == 200
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
