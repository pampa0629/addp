"""Pinned-product Notebook storage acceptance through Gateway and real Kernel."""
from __future__ import annotations

from datetime import datetime, timezone
import hashlib
import base64
from http.cookiejar import CookieJar
from importlib import import_module
import json
import os
from pathlib import Path
import subprocess
import sys
import time
from urllib.error import HTTPError
from urllib.parse import quote, unquote, urlencode, urlsplit
from urllib.request import HTTPCookieProcessor, Request, build_opener
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
api = import_module("scripts.utils.online-api")
objects = import_module("scripts.test.standard-document-storage-online")
ROOT = "/api/v1/develop"
BUCKET = "notebook-http-documents"
PERMISSIONS = {"develop.notebook.create", "develop.notebook.read", "develop.notebook.update",
               "develop.notebook.delete", "develop.task.read", "system.engine.read"}


def require(value, message):
    if not value:
        raise api.SuiteError(message)


class IdentityClient(api.GatewayClient):
    def request(self, *args, **kwargs):
        response = super().request(*args, **kwargs)
        self.context = response.payload
        return response


def validate_notebook_identity(token, tenant):
    client = IdentityClient(os.environ["GATEWAY_URL"], token, 30)
    identity = api.validate_user_identity(client, tenant, PERMISSIONS)
    actual = {permission for assignment in client.context["authorization"]["role_assignments"]
              for permission in assignment["permissions"]}
    require(actual == PERMISSIONS, "Notebook User actual permissions differ from approved six")
    require("tenant.engine_management_reader" in identity["roles"], "Notebook User minimum builtin reader missing")
    return identity


class Client:
    def __init__(self, token):
        self.base = os.environ["GATEWAY_URL"].rstrip("/")
        self.token = token
        self.cookies = CookieJar()
        self.opener = build_opener(HTTPCookieProcessor(self.cookies))

    def request(self, method, path, expected, body=None, *, raw=False, data=None, content_type="application/json", headers=None):
        request_headers = {"Authorization": "Bearer " + self.token, "Content-Type": content_type}
        request_headers.update(headers or {})
        encoded = json.dumps(body).encode() if body is not None else data
        request = Request(self.base + path, method=method, data=encoded, headers=request_headers)
        try:
            response = self.opener.open(request, timeout=60)
        except HTTPError as exc:
            response = exc
        with response:
            status, payload = response.code, response.read()
        require(status in expected, f"{method} Notebook endpoint returned unexpected HTTP {status}")
        if raw:
            return payload
        return json.loads(payload) if payload else {}

    def cookie_header(self, path):
        request = Request(self.base + path)
        self.cookies.add_cookie_header(request)
        return request.get_header("Cookie", "")


def check_bytes(actual, expected, label):
    require(actual == expected, label + " bytes differ")
    return {"bytes": len(actual), "sha256": hashlib.sha256(actual).hexdigest()}


def session_resources(kind, session_id):
    command = ["docker", "ps", "-aq", "--no-trunc"] if kind == "container" else ["docker", "network", "ls", "-q", "--no-trunc"]
    for label in ("addp.jupyter.launcher=addp-runtimes", "addp.jupyter.job=" + session_id):
        command.extend(["--filter", "label=" + label])
    return subprocess.check_output(command, text=True, timeout=10).splitlines()


def process_identity(session_id):
    require(str(uuid.UUID(session_id)) == session_id, "invalid session identity")
    containers = session_resources("container", session_id)
    networks = session_resources("network", session_id)
    require(len(containers) == len(networks) == 1, "unique owned session container/network missing")
    # The disposable observer captures only the issued capability for the existing
    # revocation oracle. It is never part of the archived process evidence.
    inspect_job = r"""
import json, os
from pathlib import Path
work = Path("/job/files")
processes = {}
for entry in Path("/proc").iterdir():
    if not entry.name.isdigit() or entry.name == str(os.getpid()):
        continue
    try:
        if (entry / "cwd").resolve() != work:
            continue
        env = (entry / "environ").read_bytes().split(b"\0")
        assert not any(v.startswith((b"MINIO_", b"ADDP_INFRA_OBJECTSTORE_")) for v in env)
        processes[entry.name] = (entry / "stat").read_text().split(") ", 1)[1].split()[19]
    except FileNotFoundError:
        continue
assert processes and os.getuid() == 10001 and not Path("/var/run/docker.sock").exists()
cap = json.loads(Path("/job/job.json").read_bytes())["session_environment"]["ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN"]
print(json.dumps([processes, cap]))
"""
    processes, capability = json.loads(subprocess.check_output(
        ["docker", "exec", containers[0], "python", "-I", "-c", inspect_job], timeout=15))
    return {"workspace": "/job/files", "processes": {int(k): v for k, v in processes.items()},
            "session_id": session_id, "container": containers[0], "network": networks[0]}, capability


def process_gone(identity):
    key = identity["session_id"]
    if session_resources("container", key) or session_resources("network", key):
        return False
    check = subprocess.run(["docker", "exec", "jupyter-engine", "test", "!", "-e",
                            "/workspace/notebooks/" + key], capture_output=True, timeout=10)
    require(check.returncode in (0, 1) and not check.stderr, "unable to verify API workspace cleanup")
    return check.returncode == 0


def wait_cleanup(identity, deadline):
    while not process_gone(identity):
        require(time.monotonic() < deadline, "session process/workspace cleanup timed out")
        time.sleep(1)


def physical_put(bucket, key, content):
    result = subprocess.run(["docker", "exec", "-i", "addp-minio", "mc", "pipe", f"local/{bucket}/{key}"],
                            input=content, capture_output=True, timeout=30)
    require(result.returncode == 0, "physical sentinel upload failed")


def kernel_roundtrip(client, session, notebook_name):
    # websocket-client is part of the real Jupyter environment; no HTTP substitute.
    import websocket
    base = ROOT + "/notebook-sessions/" + session["id"] + "/"
    kernel = client.request("POST", base + "api/kernels", (201,), {"name": "python3"})
    channel_path = base + "api/kernels/" + kernel["id"] + "/channels?session_id=" + str(uuid.uuid4())
    endpoint = client.base.replace("http://", "ws://", 1).replace("https://", "wss://", 1) + channel_path
    message_id = str(uuid.uuid4())
    code = 'import json,os; print(json.dumps({"sum":1+1,"pid":os.getpid(),"cwd":os.getcwd()}))'
    message = {"header": {"msg_id": message_id, "username": "acceptance", "session": str(uuid.uuid4()),
                           "msg_type": "execute_request", "version": "5.3", "date": datetime.now(timezone.utc).isoformat()},
               "parent_header": {}, "metadata": {}, "channel": "shell",
               "content": {"code": code, "silent": False, "store_history": False, "user_expressions": {},
                           "allow_stdin": False, "stop_on_error": True}}
    connection = websocket.create_connection(endpoint, cookie=client.cookie_header(channel_path), origin=client.base, timeout=30)
    output, replied, idle = "", False, False
    try:
        connection.send(json.dumps(message))
        deadline = time.monotonic() + 45
        while not (replied and idle):
            require(time.monotonic() < deadline, "Kernel execution timed out")
            response = json.loads(connection.recv())
            if response.get("parent_header", {}).get("msg_id") != message_id:
                continue
            kind, content = response["header"]["msg_type"], response["content"]
            require(kind != "error", "real Kernel execution failed")
            if kind == "stream":
                output += content["text"]
            elif kind == "execute_reply":
                require(content["status"] == "ok", "Kernel reply failed")
                replied = True
            elif kind == "status" and content["execution_state"] == "idle":
                idle = True
    finally:
        connection.close()
    proof = json.loads(output.strip())
    require(proof.get("sum") == 2 and proof.get("pid", 0) > 0, "Kernel result missing")
    file_path = base + "api/contents/" + quote(notebook_name)
    notebook = client.request("GET", file_path, (200,))["content"]
    notebook["cells"].append({"cell_type": "markdown", "metadata": {}, "source": "Notebook storage acceptance 保存"})
    client.request("PUT", file_path, (200,), {"type": "notebook", "format": "json", "content": notebook})
    client.request("GET", base + "lab/tree/" + quote(notebook_name), (200,), raw=True)
    return proof


def closed_websocket(client, session, kernel_id, cookie):
    import websocket
    path = ROOT + "/notebook-sessions/" + session + "/api/kernels/" + kernel_id + "/channels"
    endpoint = client.base.replace("http://", "ws://", 1).replace("https://", "wss://", 1) + path
    try:
        connection = websocket.create_connection(endpoint, cookie=cookie, origin=client.base, timeout=10)
    except websocket.WebSocketBadStatusException as exc:
        require(exc.status_code == 401, "closed WebSocket returned unexpected status")
        return
    connection.close()
    raise api.SuiteError("closed session WebSocket was accepted")


def control_plane_rejections(tenant):
    tokens = {}
    system = api.GatewayClient(os.environ["GATEWAY_URL"], "", 30)
    for owner in ("develop", "manager"):
        credentials = base64.b64encode(("addp-" + owner + ":" + os.environ[owner.upper() + "_SERVICE_CLIENT_SECRET"]).encode()).decode()
        response = system.request("POST", "/api/v1/system/oauth/token", (200,), encoded_body=urlencode({
            "grant_type": "client_credentials", "scope": "addp.api", "audience": "addp.api", "tenant_id": str(tenant)
        }).encode(), content_type="application/x-www-form-urlencoded", authorization_header="Basic " + credentials).payload
        tokens[owner] = response["access_token"]
    runtime = api.GatewayClient("http://127.0.0.1:8097", tokens["manager"], 30)
    runtime.request("POST", "/api/execute", (403,), {})
    runtime.token = tokens["develop"]
    payload = {"tenant_id": tenant, "kernel": "python3", "notebook_content_sha256": "0" * 64}
    runtime.request("POST", "/api/execute", (400,), {**payload, "notebook_path": "legacy.ipynb"})
    for area, object_tenant, expected in (("manager", tenant, 403), ("develop", tenant + 1, 403)):
        runtime.request("POST", "/api/execute", (expected,), {**payload, "notebook_locator":
            f"addp-infra://object/{area}/tenant_{object_tenant}/notebooks/forbidden.ipynb?type=object"})
    runtime.request("POST", "/api/execute", (400,), {**payload, "notebook_locator": "../forbidden.ipynb"})
    return {"wrong_service": 403, "legacy_field": 400, "wrong_area": 403, "wrong_tenant": 403, "malformed_reference": 400}


def main():
    require(os.environ.get("ADDP_ONLINE_HOSTED") == "1" and sys.platform == "linux", "owned Hosted Linux is required")
    product = Path(os.environ["RUNNER_TEMP"]) / "notebook-http-product"
    require(subprocess.check_output(["git", "-C", str(product), "rev-parse", "HEAD"], text=True).strip()
            == os.environ["NOTEBOOK_HTTP_SOURCE_COMMIT"], "pinned product identity changed")
    require(os.environ.get("POSTGRES_DB") == "addp_online", "isolated platform database required")
    tenant, foreign_tenant = int(os.environ["ADDP_ONLINE_TEST_TENANT_ID"]), int(os.environ["ADDP_ONLINE_NOTEBOOK_FOREIGN_TENANT_ID"])
    require(tenant != foreign_tenant and min(tenant, foreign_tenant) > 1, "distinct nondefault tenants required")
    tokens = [os.environ[name] for name in ("ADDP_ONLINE_TEST_USER_ACCESS_TOKEN", "ADDP_ONLINE_NOTEBOOK_PEER_TOKEN", "ADDP_ONLINE_NOTEBOOK_FOREIGN_TOKEN")]
    clients = [Client(token) for token in tokens]
    owner, peer, foreign = clients
    identities = [validate_notebook_identity(token, owner_tenant)
                  for token, owner_tenant in zip(tokens, (tenant, tenant, foreign_tenant))]
    engines = owner.request("GET", ROOT + "/notebook-engines", (200,))
    require(any(engine["id"] == int(os.environ["ADDP_ONLINE_NOTEBOOK_ENGINE_ID"]) for engine in engines), "fixture Script Engine not visible")
    engine_id = int(os.environ["ADDP_ONLINE_NOTEBOOK_ENGINE_ID"])
    report = {"schema_version": "addp.online-suite/v1", "suite": "develop-notebook-storage", "result": "failed",
              "product_sha": os.environ["NOTEBOOK_HTTP_SOURCE_COMMIT"], "harness_sha": os.environ["GITHUB_SHA"],
              "physical_bucket": BUCKET, "engine_id": engine_id, "identities": identities, "observations": []}
    report["runtime_rejections"] = control_plane_rejections(tenant)
    tasks, sessions, retained = [], [], {}
    artifact = Path(os.environ["ADDP_ONLINE_ARTIFACT_DIR"]) / "notebook-storage-evidence.json"
    try:
        foreign_task = foreign.request("POST", ROOT + "/notebooks", (201,), {"name": "storage-foreign-sentinel", "engine_id": engine_id, "kernel": "python3"})["dev_task"]
        tasks.append((foreign, foreign_task["id"]))
        foreign_key = unquote(urlsplit(foreign_task["content"]["notebook_locator"]).path.removeprefix("/develop/"))
        foreign_bytes = foreign.request("GET", ROOT + f"/notebooks/{foreign_task['id']}/download", (200,), raw=True)
        check_bytes(objects.object_bytes(BUCKET, foreign_key), foreign_bytes, "foreign sentinel")
        retained[foreign_key] = foreign_bytes
        # New and upload use their actual owner routes; the uploaded bytes are preserved exactly.
        created = owner.request("POST", ROOT + "/notebooks", (201,), {"name": "storage-new", "engine_id": engine_id, "kernel": "python3"})["dev_task"]
        tasks.append((owner, created["id"]))
        content = b'{"cells":[],"metadata":{"kernelspec":{"name":"python3","display_name":"Python 3","language":"python"}},"nbformat":4,"nbformat_minor":5}\n'
        boundary = "notebook-storage-boundary"
        parts = []
        for name, value in (("name", "storage-upload"), ("engine_id", str(engine_id)), ("kernel", "python3")):
            parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n'.encode())
        parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="source.ipynb"\r\nContent-Type: application/x-ipynb+json\r\n\r\n'.encode() + content + b'\r\n')
        uploaded = owner.request("POST", ROOT + "/notebooks/upload", (200,), data=b''.join(parts) + f'--{boundary}--\r\n'.encode(), content_type=f"multipart/form-data; boundary={boundary}")["dev_task"]
        tasks.append((owner, uploaded["id"]))
        for task, initial in ((created, None), (uploaded, content)):
            task_id = task["id"]
            locator = task["content"]["notebook_locator"]
            parsed = urlsplit(locator)
            key = unquote(parsed.path.removeprefix("/develop/"))
            require(parsed.scheme == "addp-infra" and parsed.netloc == "object" and parsed.query == "type=object"
                    and key.startswith(f"tenant_{tenant}/notebooks/"), "task reference scope changed")
            path = ROOT + f"/notebooks/{task_id}"
            original = owner.request("GET", path + "/download", (200,), raw=True)
            if initial is not None:
                check_bytes(original, initial, "uploaded HTTP")
            initial_physical = check_bytes(objects.object_bytes(BUCKET, key), original, "initial physical")
            sentinel = b"default-bucket-sentinel:" + str(task_id).encode()
            physical_put("develop", key, sentinel)
            check_bytes(peer.request("GET", path + "/download", (200,), raw=True), original, "same tenant shared download")
            foreign.request("GET", path + "/download", (404,))
            foreign.request("POST", path + "/sessions", (404,))
            session = owner.request("POST", path + "/sessions", (201,))
            sessions.append((owner, task_id, session["id"]))
            peer.request("POST", path + "/sessions", (409,))
            peer.request("DELETE", path + "/sessions/" + session["id"], (404,))
            foreign.request("DELETE", path + "/sessions/" + session["id"], (404,))
            proxy = ROOT + "/notebook-sessions/" + session["id"] + "/api/contents"
            peer.request("GET", proxy, (401,))
            proof = kernel_roundtrip(owner, session, Path(key).name)
            identity, capability = process_identity(session["id"])
            require(proof["pid"] in identity["processes"] and proof["cwd"] == identity["workspace"], "Kernel process not in owned workspace")
            saved = Path(identity["workspace"], Path(key).name).read_bytes()
            require(b"Notebook storage acceptance" in saved and saved != original, "HTTP edit was not saved")
            observation = {"task_id": task_id, "session_id": session["id"], "locator": locator, "key": key,
                           "initial_physical": initial_physical, "kernel": proof, "session_processes": identity}
            report["observations"].append(observation)
            capability_path = ROOT + "/notebook-kernel-sessions/" + session["id"] + "/engine-descriptors"
            owner.request("GET", capability_path, (200,), headers={"Authorization": "Bearer " + capability})
            owner.request("POST", capability_path.removesuffix("engine-descriptors") + "catalog/children", (401,), {
                "engine_id": engine_id,
                "path": {"version": "catalog.path/v1", "engine_id": engine_id,
                         "segments": [{"term": "root", "kind": "root", "name": ""}]},
                "options": {"limit": 1},
            }, headers={"Authorization": "Bearer addp_nkc_" + "A" * 43})
            old_cookie = owner.cookie_header(proxy)
            owner.request("DELETE", path + "/sessions/" + session["id"], (204,))
            sessions.remove((owner, task_id, session["id"]))
            wait_cleanup(identity, time.monotonic() + 30)
            owner.request("GET", proxy, (401,), headers={"Cookie": old_cookie})
            closed_websocket(owner, session["id"], "closed-kernel", old_cookie)
            owner.request("GET", capability_path, (401,), headers={"Authorization": "Bearer " + capability})
            check_bytes(owner.request("GET", path + "/download", (200,), raw=True), saved, "closed HTTP")
            observation.update({**check_bytes(objects.object_bytes(BUCKET, key), saved, "closed physical"),
                                "explicit_cleanup": "passed"})
            check_bytes(objects.object_bytes("develop", key), sentinel, "default bucket sentinel")
            check_bytes(objects.object_bytes(BUCKET, foreign_key), foreign_bytes, "foreign retained object")
            retained[key] = saved
        # TTL is natural: no product clock, in-memory expiry, or PG timestamps are edited.
        task_id = uploaded["id"]
        path = ROOT + f"/notebooks/{task_id}"
        expired = owner.request("POST", path + "/sessions", (201,))
        sessions.append((owner, task_id, expired["id"]))
        ttl_key = unquote(urlsplit(uploaded["content"]["notebook_locator"]).path.removeprefix("/develop/"))
        kernel_roundtrip(owner, expired, Path(ttl_key).name)
        expired_identity, expired_capability = process_identity(expired["id"])
        ttl_saved = Path(expired_identity["workspace"], Path(ttl_key).name).read_bytes()
        require(ttl_saved != retained[ttl_key], "TTL case did not save a new local edit")
        expired_cookie = owner.cookie_header(ROOT + "/notebook-sessions/" + expired["id"] + "/api/contents")
        expiry = datetime.fromisoformat(expired["expires_at"].replace("Z", "+00:00"))
        require(240 < (expiry - datetime.now(timezone.utc)).total_seconds() <= 301, "actual TTL differs from approved 300 seconds")
        time.sleep(45)
        control = peer.request("POST", ROOT + f"/notebooks/{created['id']}/sessions", (201,))
        sessions.append((peer, created["id"], control["id"]))
        control_identity, control_capability = process_identity(control["id"])
        wrong_proxy = ROOT + "/notebook-sessions/" + expired["id"] + "/api/contents"
        wrong_cookie = peer.cookie_header(ROOT + "/notebook-sessions/" + control["id"] + "/api/contents")
        peer.request("GET", wrong_proxy, (401,), headers={"Cookie": wrong_cookie})
        control_cap_path = ROOT + "/notebook-kernel-sessions/" + control["id"] + "/engine-descriptors"
        owner.request("GET", control_cap_path, (401,), headers={"Authorization": "Bearer " + expired_capability})
        expiry_deadline = time.monotonic() + max(0, (expiry - datetime.now(timezone.utc)).total_seconds()) + 30
        wait_cleanup(expired_identity, expiry_deadline)
        control_expiry = datetime.fromisoformat(control["expires_at"].replace("Z", "+00:00"))
        require(datetime.now(timezone.utc) < control_expiry and not process_gone(control_identity), "TTL control was not observed while still alive")
        owner.request("GET", wrong_proxy, (401,), headers={"Cookie": expired_cookie})
        closed_websocket(owner, expired["id"], "expired-kernel", expired_cookie)
        owner.request("GET", ROOT + "/notebook-kernel-sessions/" + expired["id"] + "/engine-descriptors", (401,), headers={"Authorization": "Bearer " + expired_capability})
        peer.request("GET", control_cap_path, (200,), headers={"Authorization": "Bearer " + control_capability})
        report["natural_ttl"] = {"session_id": expired["id"], "expires_at": expired["expires_at"],
                                 "control_session_id": control["id"], "control_expires_at": control["expires_at"],
                                 "observed_at": datetime.now(timezone.utc).isoformat(), "cleanup": "passed",
                                 **check_bytes(objects.object_bytes(BUCKET, ttl_key), ttl_saved, "TTL physical save")}
        check_bytes(owner.request("GET", path + "/download", (200,), raw=True), ttl_saved, "TTL HTTP save")
        retained[ttl_key] = ttl_saved
        check_bytes(objects.object_bytes(BUCKET, foreign_key), foreign_bytes, "TTL foreign sentinel")
        report["foreign_sentinel"] = {"task_id": foreign_task["id"], "tenant_id": foreign_tenant, "key": foreign_key,
                                      **check_bytes(objects.object_bytes(BUCKET, foreign_key), foreign_bytes, "foreign sentinel")}
        for observation in report["observations"]:
            check_bytes(objects.object_bytes("develop", observation["key"]), b"default-bucket-sentinel:" + str(observation["task_id"]).encode(), "TTL sentinel")
        report["result"] = "passed"
    finally:
        # Every cleanup attempt is independent; preserve the original failure and cleanup status.
        cleanup_errors = []
        for client, task_id, session_id in reversed(sessions):
            try:
                client.request("DELETE", ROOT + f"/notebooks/{task_id}/sessions/{session_id}", (204, 404))
            except Exception:
                cleanup_errors.append("session")
        for client, task_id in reversed(tasks):
            try:
                client.request("DELETE", ROOT + f"/notebooks/{task_id}", (200,))
                client.request("GET", ROOT + f"/notebooks/{task_id}/download", (404,))
            except Exception:
                cleanup_errors.append("task")
        for key, expected in retained.items():
            try:
                check_bytes(objects.object_bytes(BUCKET, key), expected, "soft delete retained object")
            except Exception:
                cleanup_errors.append("retained object")
        report["cleanup"] = "failed" if cleanup_errors else "passed"
        if cleanup_errors:
            report["result"] = "failed"
        artifact.write_text(json.dumps(report, ensure_ascii=False, indent=2))
        require(not cleanup_errors, "Notebook HTTP cleanup failed; inspect redacted report")
    print(json.dumps(report, ensure_ascii=False))


if __name__ == "__main__":
    main()
