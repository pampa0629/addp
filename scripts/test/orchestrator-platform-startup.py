"""Start the released Orchestrator process with only runtime database credentials."""
from http.client import HTTPConnection
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import sys
import time

binary = Path(sys.argv[1]).resolve()
env = {key: os.environ[key] for key in (
    "ADDP_INFRA_DATABASE_PROVIDER", "ADDP_DATABASE_RUNTIME_USER", "ADDP_DATABASE_RUNTIME_PASSWORD",
    "ADDP_MYSQL_HOST", "ADDP_MYSQL_PORT", "ADDP_MYSQL_SSLMODE", "POSTGRES_HOST", "POSTGRES_PORT",
    "POSTGRES_DB", "TZ",
) if key in os.environ}
with socket.socket() as reservation:
    reservation.bind(("127.0.0.1", 0))
    port = reservation.getsockname()[1]
env.update(ORCHESTRATOR_BACKEND_PORT=str(port), ORCHESTRATOR_SERVICE_CLIENT_SECRET=secrets.token_hex(32),
           SYSTEM_URL="http://127.0.0.1:1", REDIS_HOST="127.0.0.1", REDIS_PORT="1", GIN_MODE="release")

if sys.argv[2] == "missing":
    owner = sys.argv[3]
    result = subprocess.run([binary], env=env, cwd=binary.parent, capture_output=True, timeout=15)
    output = (result.stderr + result.stdout).decode(errors="replace")
    expected = (f"schema {owner} is not initialized" if env["ADDP_INFRA_DATABASE_PROVIDER"] == "postgres"
                else f"cannot read installed MySQL schema revision for {owner}")
    if result.returncode != 1 or expected not in output or f"{owner}.startup_schema_revision" not in output:
        raise RuntimeError(f"expected refusal for missing {owner} revision, exit={result.returncode}: {output[-2000:]}")
    print(f"Runtime rejected missing {owner} revision, exit=1")
    sys.exit(0)


def request(path):
    connection = HTTPConnection("127.0.0.1", port, timeout=2)
    try:
        connection.request("GET", path)
        response = connection.getresponse()
        return response.status, response.read()
    finally:
        connection.close()


logfile = binary.parent / "orchestrator-runtime.log"
with logfile.open("wb") as log:
    process = subprocess.Popen([binary], cwd=binary.parent, env=env, stdout=log, stderr=subprocess.STDOUT)
    try:
        deadline = time.monotonic() + 25
        while process.poll() is None and time.monotonic() < deadline:
            try:
                status, body = request("/health/ready")
                if status == 200 and json.loads(body)["module"] == "orchestrator":
                    break
            except OSError:
                pass
            time.sleep(.1)
        else:
            raise RuntimeError(f"Orchestrator failed to start: {logfile.read_text()[-2000:]}")
        if request("/api/v1/orchestrator/orchestrations")[0] != 401:
            raise RuntimeError("protected Orchestrator route did not return 401")
    finally:
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
    if process.returncode != 0:
        raise RuntimeError(f"Orchestrator shutdown failed with exit={process.returncode}")
    print("Orchestrator runtime: Ready 200, protected route 401, SIGTERM exit=0")
