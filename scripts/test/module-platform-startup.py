"""Start the released module process with only runtime database credentials."""
from http.client import HTTPConnection
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import sys
import tempfile
import time

binary = Path(sys.argv[1]).resolve()
module = binary.name
route = {"orchestrator": "orchestrations", "monitor": "executions"}[module]
env = {key: os.environ[key] for key in (
    "ADDP_INFRA_DATABASE_PROVIDER", "ADDP_DATABASE_RUNTIME_USER", "ADDP_DATABASE_RUNTIME_PASSWORD",
    "ADDP_MYSQL_HOST", "ADDP_MYSQL_PORT", "ADDP_MYSQL_SSLMODE", "POSTGRES_HOST", "POSTGRES_PORT",
    "POSTGRES_DB", "TZ",
) if key in os.environ}
with socket.socket() as reservation:
    reservation.bind(("127.0.0.1", 0))
    port = reservation.getsockname()[1]
env[module.upper() + "_BACKEND_PORT"] = str(port)
env[module.upper() + "_SERVICE_CLIENT_SECRET"] = secrets.token_hex(32)
env.update(SYSTEM_URL="http://127.0.0.1:1", REDIS_HOST="127.0.0.1", REDIS_PORT="1", GIN_MODE="release")

if sys.argv[2] == "missing-credentials":
    # A development checkout may have credentials in its root .env. A service
    # receives its role from the launcher and must never import that file again.
    with tempfile.TemporaryDirectory() as root:
        Path(root, ".env").write_text("ADDP_DATABASE_RUNTIME_USER=fixture\nADDP_DATABASE_RUNTIME_PASSWORD=fixture\n")
        env.pop("ADDP_DATABASE_RUNTIME_USER", None)
        env.pop("ADDP_DATABASE_RUNTIME_PASSWORD", None)
        env["PROJECT_ROOT"] = root
        result = subprocess.run([binary], env=env, cwd=root, capture_output=True, timeout=15)
    if result.returncode != 1 or b"platform database runtime credentials are required" not in result.stderr:
        raise RuntimeError(f"runtime did not refuse missing injected credentials, exit={result.returncode}")
    print("Runtime refused missing injected credentials despite a populated root .env, exit=1")
    sys.exit(0)

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


logfile = binary.parent / f"{module}-runtime.log"
with logfile.open("wb") as log:
    process = subprocess.Popen([binary], cwd=binary.parent, env=env, stdout=log, stderr=subprocess.STDOUT)
    try:
        deadline = time.monotonic() + 25
        while process.poll() is None and time.monotonic() < deadline:
            try:
                status, body = request("/health/ready")
                if status == 200 and json.loads(body)["module"] == module:
                    break
            except OSError:
                pass
            time.sleep(.1)
        else:
            raise RuntimeError(f"{module} failed to start: {logfile.read_text()[-2000:]}")
        if request(f"/api/v1/{module}/{route}")[0] != 401:
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
        raise RuntimeError(f"{module} shutdown failed with exit={process.returncode}")
    print(f"{module} runtime: Ready 200, protected route 401, SIGTERM exit=0")
