"""Exercise the installed Inference executable using only its runtime login."""
import base64
from http.client import HTTPConnection
import os
from pathlib import Path
import secrets
import socket
import subprocess
import sys
import time

binary = Path(sys.argv[1]).resolve()
with socket.socket() as reservation:
    reservation.bind(("127.0.0.1", 0))
    port = reservation.getsockname()[1]

env = {key: os.environ[key] for key in (
    "ADDP_INFRA_DATABASE_PROVIDER", "ADDP_DATABASE_RUNTIME_USER", "ADDP_DATABASE_RUNTIME_PASSWORD",
    "ADDP_MYSQL_HOST", "ADDP_MYSQL_PORT", "ADDP_MYSQL_SSLMODE", "POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_DB", "TZ"
) if key in os.environ}
env.update(INFERENCE_BACKEND_PORT=str(port), ENCRYPTION_KEY=base64.b64encode(secrets.token_bytes(32)).decode())
if len(sys.argv) == 3 and sys.argv[2] == "unprepared":
    result = subprocess.run([binary], env=env, cwd=binary.parent, capture_output=True, timeout=10)
    if result.returncode != 1 or b"startup_schema_revision" not in result.stderr + result.stdout:
        raise RuntimeError("expected startup refusal at missing Inference schema revision")
    print("Inference refused unprepared database")
    sys.exit(0)


def status(path):
    conn = HTTPConnection("127.0.0.1", port, timeout=2)
    try:
        conn.request("GET", path)
        return conn.getresponse().status
    finally:
        conn.close()


for cycle in range(2):
    with (binary.parent / "inference-runtime.log").open("ab") as log:
        process = subprocess.Popen([binary], cwd=binary.parent, env=env, stdout=log, stderr=subprocess.STDOUT)
        try:
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline and process.poll() is None:
                try:
                    if status("/health/ready") == 200:
                        break
                except OSError:
                    pass
                time.sleep(.1)
            else:
                raise RuntimeError("Inference failed to become ready")
            if status("/api/v1/inference/provider-connections") != 401:
                raise RuntimeError("expected unauthenticated request to be denied")
            print(f"Inference runtime cycle {cycle+1}: Ready 200, protected route 401")
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=8)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
        if process.returncode != 0:
            raise RuntimeError(f"ungraceful Inference shutdown: {process.returncode}")
