"""Start the released module process with only runtime database credentials."""
from contextlib import ExitStack, contextmanager
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
route = {"orchestrator": "orchestrations", "monitor": "executions", "standard": "elements", "model": "entities", "service": "registered", "asset": "categories", "catalog": "entries", "graph": "ontologies", "ontology": "ontologies/release-check", "develop": "task-definitions", "quality": "rules", "transfer": "task-definitions", "workbench": "data_applications"}[module]
env = {key: os.environ[key] for key in (
    "ADDP_INFRA_DATABASE_PROVIDER", "ADDP_DATABASE_RUNTIME_USER", "ADDP_DATABASE_RUNTIME_PASSWORD",
    "ADDP_MYSQL_HOST", "ADDP_MYSQL_PORT", "ADDP_MYSQL_SSLMODE", "POSTGRES_HOST", "POSTGRES_PORT",
    "POSTGRES_DB", "TZ", "MEILISEARCH_URL", "MEILISEARCH_MASTER_KEY", "INFRA_FALKORDB_ADDRESS", "INFRA_FALKORDB_PASSWORD",
) if key in os.environ}
env[module.upper() + "_SERVICE_CLIENT_SECRET"] = secrets.token_hex(32)
if module == "ontology":
    env.setdefault("INFRA_FALKORDB_PASSWORD", secrets.token_hex(24))
if module in ("standard", "develop", "transfer"):
    env.update(MINIO_ROOT_USER="fixture", MINIO_ROOT_PASSWORD="fixture-secret",MINIO_ENDPOINT="127.0.0.1:1")
if module == "standard":
    env.update(MINIO_ROOT_USER="fixture", MINIO_ROOT_PASSWORD="fixture-secret", MINIO_ENDPOINT="127.0.0.1:1", STANDARD_DOCUMENT_STORAGE_TIMEOUT="250ms")
if module == "monitor":
    env["GATEWAY_URL"] = "http://127.0.0.1:1"
env.update(SYSTEM_URL="http://127.0.0.1:1", REDIS_HOST="127.0.0.1", REDIS_PORT="1", GIN_MODE="release")

def allocate_loopback_port():
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        return reservation.getsockname()[1]


# Some modules validate the configured port before checking the database.
if sys.argv[2] in ("missing-credentials", "missing", "outdated"):
    env[module.upper() + "_BACKEND_PORT"] = str(allocate_loopback_port())

if sys.argv[2] == "missing-credentials":
    # A development checkout may have credentials in its root .env. A service
    # receives its role from the launcher and must never import that file again.
    with tempfile.TemporaryDirectory() as root:
        Path(root, ".env").write_text("ADDP_DATABASE_RUNTIME_USER=fixture\nADDP_DATABASE_RUNTIME_PASSWORD=fixture\n")
        env.pop("ADDP_DATABASE_RUNTIME_USER", None)
        env.pop("ADDP_DATABASE_RUNTIME_PASSWORD", None)
        env["PROJECT_ROOT"] = root
        result = subprocess.run([binary], env=env, cwd=root, capture_output=True, timeout=15)
    if result.returncode != 1 or b"platform database runtime credentials are required" not in (result.stderr + result.stdout):
        raise RuntimeError(f"runtime did not refuse missing injected credentials, exit={result.returncode}")
    print("Runtime refused missing injected credentials despite a populated root .env, exit=1")
    sys.exit(0)

if sys.argv[2] in ("missing", "outdated"):
    owner = sys.argv[3]
    result = subprocess.run([binary], env=env, cwd=binary.parent, capture_output=True, timeout=15)
    output = (result.stderr + result.stdout).decode(errors="replace")
    if sys.argv[2] == "outdated":
        installed, required = sys.argv[4:]
        expected = [f"schema {owner} version mismatch: database={installed} process={required}"]
    else:
        expected = [f"{owner}.startup_schema_revision", (
            f"schema {owner} is not initialized" if env["ADDP_INFRA_DATABASE_PROVIDER"] == "postgres"
            else f"cannot read installed MySQL schema revision for {owner}"
        )]
    if result.returncode != 1 or any(message not in output for message in expected):
        raise RuntimeError(f"expected refusal for {sys.argv[2]} {owner} revision, exit={result.returncode}: {output[-2000:]}")
    print(f"Runtime rejected {sys.argv[2]} {owner} revision, exit=1")
    sys.exit(0)


def request(port, path):
    connection = HTTPConnection("127.0.0.1", port, timeout=2)
    try:
        connection.request("GET", path)
        response = connection.getresponse()
        return response.status, response.read()
    finally:
        connection.close()


def check_http(port):
    status, body = request(port, "/health/ready")
    if status != 200 or json.loads(body)["module"] != module:
        raise RuntimeError(f"{module} did not report Ready")
    if request(port, f"/api/v1/{module}/{route}")[0] != 401:
        raise RuntimeError(f"protected {module} route did not return 401")


@contextmanager
def runtime(label):
    port = allocate_loopback_port()
    logfile = binary.parent / f"{module}-{label}.log"
    with logfile.open("wb") as log:
        process = subprocess.Popen([binary], cwd=binary.parent, env={
            **env, module.upper() + "_BACKEND_PORT": str(port),
        }, stdout=log, stderr=subprocess.STDOUT)
        try:
            deadline = time.monotonic() + 25
            while process.poll() is None and time.monotonic() < deadline:
                try:
                    status, body = request(port, "/health/ready")
                    if status == 200 and json.loads(body)["module"] == module:
                        break
                except OSError:
                    pass
                time.sleep(.1)
            else:
                raise RuntimeError(f"{module} failed to start: {logfile.read_text()[-2000:]}")
            check_http(port)
            yield port, logfile
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


def await_monitor_election(log):
    started = time.monotonic()
    while "Monitor periodic evaluation started" not in log.read_text():
        if time.monotonic() - started >= 20:
            raise RuntimeError("Monitor failed to acquire periodic evaluation")
        time.sleep(.1)
    return time.monotonic() - started


with ExitStack() as replicas:
    with runtime("primary") as (first, first_log):
        if module == "monitor":
            await_monitor_election(first_log)
            second, second_log = replicas.enter_context(runtime("standby"))
            check_http(first)
            if "Monitor periodic evaluation started" in second_log.read_text():
                raise RuntimeError("standby acquired evaluation while primary was alive")
    if module == "monitor":
        elapsed = await_monitor_election(second_log)
        check_http(second)
        print(f"Monitor dual HTTP Ready 200/protected route 401; standby acquired evaluation {elapsed:.2f}s after primary exit")
print(f"{module} runtime: Ready 200, protected route 401, SIGTERM exit=0")
