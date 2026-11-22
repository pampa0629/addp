"""Check database startup through the normal Backend and Worker binaries twice."""

import json
import os
from pathlib import Path
import re
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request


def run(binary, env, live_url):
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory)
        log = path / "output"
        with log.open("w") as output:
            proc = subprocess.Popen([binary], cwd=path, env=dict(env, PROJECT_ROOT=directory), stdout=output, stderr=output)
            try:
                deadline = time.monotonic() + 50
                while time.monotonic() < deadline:
                    if proc.poll() is not None:
                        raise RuntimeError(f"{binary.name} exited {proc.returncode} before startup")
                    if live_url:
                        try:
                            with urllib.request.urlopen(live_url, timeout=1) as response:
                                if response.status == 200 and json.load(response).get("status") == "live":
                                    break
                        except (OSError, urllib.error.URLError):
                            pass
                    elif "meta bounded worker started" in log.read_text():
                        break
                    time.sleep(0.15)
                else:
                    raise RuntimeError(f"{binary.name} startup timed out")
            finally:
                if proc.poll() is None:
                    proc.send_signal(signal.SIGTERM)
                try:
                    proc.wait(timeout=15)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait()
                    raise RuntimeError(f"{binary.name} did not stop on SIGTERM")
        if proc.returncode != 0:
            raise RuntimeError(f"{binary.name} stopped with exit {proc.returncode}")
        # Never emit raw process logs (DSNs may contain deployment credentials).
        issues = []
        for message in ("SQLSTATE 42501", "Error 1142", "Error 1143", "Error 1054", "Error 1064", "Error 1146"):
            if message in log.read_text():
                relations = sorted(set(re.findall(r"permission denied for (?:table|schema|sequence) ([a-z_]+)|(?:command denied.*?for table |Table )'([a-z_.]+)'", log.read_text())))
                issues.append(f"{binary.name} reported database error {message}, relations={relations}")
        return issues


def main():
    work, provider = Path(sys.argv[1]).resolve(), sys.argv[2]
    issues = set()
    for i in range(2):
        for binary in ("meta-worker", "meta-server"):
            with socket.socket() as sock:
                sock.bind(("127.0.0.1", 0))
                port = sock.getsockname()[1]
            env = dict(ADDP_INFRA_DATABASE_PROVIDER=provider, META_BACKEND_PORT=str(port), TZ="Asia/Shanghai",
                       SYSTEM_URL="http://127.0.0.1:1", REDIS_HOST="127.0.0.1", REDIS_PORT="1",
                       MINIO_ENDPOINT="127.0.0.1:1", MINIO_ROOT_USER="isolated", MINIO_ROOT_PASSWORD="isolated-test",
                       META_SERVICE_CLIENT_SECRET="x"*32)
            for key in ("ADDP_DATABASE_RUNTIME_USER", "ADDP_DATABASE_RUNTIME_PASSWORD"):
                env[key] = os.environ[key]
            if provider == "postgres":
                env.update(POSTGRES_HOST="127.0.0.1", POSTGRES_DB="addp_test", POSTGRES_PORT=os.environ["POSTGRES_PORT"])
            elif provider == "mysql":
                env.update(ADDP_MYSQL_HOST="127.0.0.1", ADDP_MYSQL_PORT=os.environ["ADDP_TEST_MYSQL_PORT"])
            else:
                raise ValueError("unknown platform database")
            live = f"http://127.0.0.1:{port}/health/live" if binary == "meta-server" else None
            issues.update(run(work/binary, env, live))
            print(f"{provider} {binary} startup {i+1}: {'HTTP live' if live else 'worker started'}, stopped cleanly", flush=True)

    if issues:
        raise SystemExit("\n".join(sorted(issues)))


if __name__ == "__main__":
    main()
