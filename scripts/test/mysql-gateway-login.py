"""Exercise the real Gateway and System with a disposable ordinary tenant user."""

import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import time
from urllib.error import HTTPError, URLError
from urllib.request import ProxyHandler, Request, build_opener

work = Path(sys.argv[1]).resolve()
system_url = sys.argv[2]
root = Path(__file__).resolve().parents[2]
facts = json.loads((work / "tenant.json").read_text())
http = build_opener(ProxyHandler({}))
publisher_env = {"SYSTEM_URL": system_url, "PROJECT_ROOT": str(work)}
subprocess.run(
    [str(work / "system-server"), "publish-module-declaration", "gateway"],
    cwd=work,
    env=publisher_env,
    input=json.dumps(
        {
            "secret": os.environ["ADDP_TEST_GATEWAY_PUBLISHER_SECRET"],
            "declaration": json.loads((root / "gateway/module-declaration.json").read_text()),
        }
    ),
    text=True,
    check=True,
    timeout=40,
)

with socket.socket() as s:
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
env = {
    key: os.environ[key]
    for key in (
        "ADDP_INFRA_DATABASE_PROVIDER",
        "ADDP_MYSQL_HOST",
        "ADDP_MYSQL_PORT",
        "GATEWAY_SERVICE_CLIENT_SECRET",
        "REDIS_PORT",
        "REDIS_PASSWORD",
    )
}
env.update(
    ENV="production",
    PROJECT_ROOT=str(work),
    TZ="Asia/Shanghai",
    SYSTEM_URL=system_url,
    GATEWAY_PORT=str(port),
    SERVICE_HOST="127.0.0.1",
    REDIS_HOST="127.0.0.1",
    ADDP_MYSQL_GATEWAY_USER="gateway_runtime",
    ADDP_MYSQL_GATEWAY_PASSWORD=os.environ["ADDP_TEST_GATEWAY_RUNTIME_PASSWORD"],
)


def request(path, body=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    req = Request(
        f"http://127.0.0.1:{port}{path}",
        headers=headers,
        data=json.dumps(body).encode() if body is not None else None,
    )
    try:
        reply = http.open(req, timeout=4)
    except HTTPError as err:
        reply = err
    with reply:
        return reply.status, json.load(reply)


with (work / "gateway-runtime.log").open("w+") as log:
    gateway = subprocess.Popen([str(work / "gateway")], cwd=work, env=env, stdout=log, stderr=log)
    try:
        deadline = time.monotonic() + 35
        while time.monotonic() < deadline and gateway.poll() is None:
            try:
                status, body = request("/health/ready")
                if status == 200 and body["module"] == "gateway" and body["status"] == "ready":
                    break
            except (URLError, TimeoutError):
                pass
            time.sleep(0.2)
        else:
            raise RuntimeError(f"Gateway failed to become ready, exit={gateway.poll()}")
        status, _ = request("/api/v1/system/auth/context")
        assert status == 401, f"anonymous tenant context returned {status}"
        status, issued = request(
            "/api/v1/system/login",
            {"username": "scan-member", "password": os.environ["ADDP_TEST_MYSQL_MEMBER_PASSWORD"]},
        )
        assert (
            status == 200 and issued.get("next_action") == "session_issued"
        ), f"Gateway login status={status} action={issued.get('next_action')}"
        status, identity = request("/api/v1/system/auth/context", token=issued["session"]["access_token"])
        assert status == 200, f"authenticated Gateway context returned {status}"
        assert identity["principal"]["type"] == "user" and identity["principal"]["id"] == str(
            facts["principal_id"]
        )
        assert identity["context"]["type"] == "tenant" and identity["context"]["tenant_id"] == str(
            facts["tenant_id"]
        )
        roles = identity["authorization"]["role_assignments"]
        assert [r["role_key"] for r in roles] == ["tenant.data_steward"]
        assert {"meta.scan_task.read", "meta.scan_task.execute"} <= set(roles[0]["permissions"])
        print(
            f"Gateway authenticated ordinary tenant {facts['tenant_id']} principal {facts['principal_id']} with the expected scan role",
            flush=True,
        )
    finally:
        if gateway.poll() is None:
            (work / "gateway-stop-time").write_text(str(time.time()))
            gateway.send_signal(signal.SIGTERM)
            try:
                gateway.wait(timeout=15)
            except subprocess.TimeoutExpired:
                gateway.kill()
                gateway.wait()
                raise RuntimeError("Gateway graceful shutdown timed out")
        if gateway.returncode:
            raise RuntimeError(f"Gateway exited {gateway.returncode}")
    log.seek(0)
    assert "Database connected successfully" in log.read(), "Gateway access database was unavailable"
