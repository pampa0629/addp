"""Exercise production HTTP, proxy, launcher and image with fixture identity/storage."""
import hashlib
import json
import os
import sys
import uuid
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace

import httpx
import nbformat

ROOT = Path("/tmp/fixture-store")
AUTH = {"Authorization": "Bearer fixture-develop"}
OWNER = "addp_nkc_" + "A" * 43
KEY = "tenant_7/notebooks/check.ipynb"
LOCATOR = "addp-infra://object/develop/" + KEY + "?type=object"
ORIGIN = "http://localhost:8097"
SUFFIXES = {"owner_api_endpoint": "engine-descriptors", "owner_catalog_api_endpoint": "catalog/children",
            "owner_table_scan_api_endpoint": "table-scans", "owner_record_scan_api_endpoint": "record-scans",
            "owner_query_api_endpoint": "queries", "owner_graph_sample_api_endpoint": "graph-samples",
            "owner_graph_query_api_endpoint": "graph-queries", "owner_content_read_api_endpoint": "content-reads",
            "owner_change_stream_api_endpoint": "change-streams"}


class Store:
    def check_ready(self):
        pass

    def open(self, key):
        return (ROOT / key).open("rb")

    def put(self, key, source, size, media_type):
        path = ROOT / key
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(source.read())


def post_fork(server, worker):
    import threading
    from http.server import BaseHTTPRequestHandler, HTTPServer
    sys.path.insert(0, "/app")
    from addp_common.infra.objectstore import bootstrap
    import addp_common.auth
    bootstrap.open_object_area = lambda area: Store()

    async def resolve(url, token):
        return SimpleNamespace(principal_type="service_principal", token_type="service_access_token",
                               client_id="addp-develop" if token == "fixture-develop" else "fixture-other",
                               context_type="tenant", tenant_id=7)
    addp_common.auth.resolve_authorization_context = resolve

    class OwnerHandler(BaseHTTPRequestHandler):
        def do_GET(self):
            good = self.headers.get("Authorization") == "Bearer " + OWNER and self.path.endswith("/engine-descriptors")
            self.send_response(200 if good else 403)
            self.end_headers()
            self.wfile.write(b"owner-scoped")

        def log_message(self, *args):
            pass

    threading.Thread(target=HTTPServer(("127.0.0.1", 9876), OwnerHandler).serve_forever, daemon=True).start()


def api(method, path, headers=AUTH, **kwargs):
    response = httpx.request(method, ORIGIN + path, headers=headers, timeout=100, **kwargs)
    if response.is_error:
        raise AssertionError(f"{method} {path} returned {response.status_code}: {response.text[:320]}")
    return response


@contextmanager
def session(filename):
    sid = str(uuid.uuid4())
    path = "/api/v1/develop/notebook-sessions/" + sid + "/"
    values = {"session_id": sid, "tenant_id": 7, "user_id": 9, "task_id": 11,
              "notebook_locator": LOCATOR.replace("check.ipynb", filename), "kernel": "python3",
              "base_path": path, "ttl_seconds": 180, "owner_capability_token": OWNER}
    values.update({k: "http://127.0.0.1:9876/api/v1/develop/notebook-kernel-sessions/" + sid + "/" + suffix
                   for k, suffix in SUFFIXES.items()})
    opened = api("POST", "/api/interactive-sessions", json=values).json()
    try:
        yield path, {"Authorization": "token " + opened["runtime_token"]}, sid
    finally:
        api("DELETE", "/api/interactive-sessions/" + sid)


def execute_cell(path, headers, code):
    import websocket
    kernel = api("POST", path + "api/kernels", headers, json={"name": "python3"}).json()["id"]
    msg = uuid.uuid4().hex
    conn = websocket.create_connection(ORIGIN.replace("http:", "ws:") + path + "api/kernels/" + kernel
                + "/channels", header=["Authorization: " + headers["Authorization"]], timeout=30)
    try:
        conn.send(json.dumps({"header": {"msg_id": msg, "session": msg, "username": "fixture",
                  "msg_type": "execute_request", "version": "5.3"}, "parent_header": {}, "metadata": {},
                  "channel": "shell", "content": {"code": code, "silent": False, "store_history": False,
                  "user_expressions": {}, "allow_stdin": False, "stop_on_error": True}}))
        out = ""
        idle = replied = False
        while not (idle and replied):
            event = json.loads(conn.recv())
            if event["parent_header"].get("msg_id") != msg:
                continue
            kind, data = event["msg_type"], event["content"]
            if kind == "error":
                raise AssertionError(data["ename"] + ": " + data["evalue"])
            if kind == "stream":
                out += data["text"]
            if kind == "execute_reply":
                assert data["status"] == "ok"
                replied = True
            if kind == "status" and data["execution_state"] == "idle":
                idle = True
        return out.strip()
    finally:
        conn.close()


def check():
    from textwrap import dedent
    boundary = dedent("""
        import os, pathlib
        from pathlib import Path
        assert os.getuid() == os.getgid() == 10001
        assert all(k not in os.environ for k in ('ADDP_SYNTHETIC_MARKER','MINIO_ROOT_PASSWORD','DOCKER_HOST'))
        assert not Path('/var/run/docker.sock').exists()
        assert not Path('/run/notebook-launcher/control.sock').exists()
        assert not Path('/fixture/probe.py').exists()
        assert not Path('/tmp/fixture-store').exists()
        assert not Path('/workspace/notebooks/control-only').exists()
        assert not any(row.split()[1] == '00000000' for row in Path('/proc/net/route').read_text().splitlines()[1:])
        status = Path('/proc/self/status').read_text().splitlines()
        assert int(next(s.split()[1] for s in status if s.startswith('CapEff:')), 16) == 0
        assert next(s.split()[1] for s in status if s.startswith('NoNewPrivs:')) == '1'
    """)
    path = ROOT / KEY
    path.parent.mkdir(parents=True, exist_ok=True)
    Path("/workspace/notebooks/control-only").touch()
    src = nbformat.writes(nbformat.v4.new_notebook(cells=[nbformat.v4.new_code_cell(boundary + "\nprint(6 * 7)")],
          metadata={"kernelspec": {"name": "python3", "display_name": "Python 3", "language": "python"}})).encode()
    assert os.environ["ADDP_SYNTHETIC_MARKER"] == "fixture-control-only"
    assert not Path("/var/run/docker.sock").exists()
    with httpx.Client(transport=httpx.HTTPTransport(uds="/run/notebook-launcher/control.sock")) as launcher:
        assert launcher.put("http://launcher/jobs/abc", json={"mode": "batch", "ttl": 10, "extra": 1}).status_code == 400
    path.write_bytes(src)
    path.with_name("other.ipynb").write_bytes(src)
    with session("check.ipynb") as (base, auth, sid), session("other.ipynb") as (other, other_auth, other_id):
        assert execute_cell(other, other_auth, "from pathlib import Path\nPath('other-only').touch()\nprint(1)") == "1"
        code = boundary + dedent("""
            import urllib.request
            assert not Path('other-only').exists()
            req = urllib.request.Request(os.environ['ADDP_NOTEBOOK_OWNER_API_ENDPOINT'],
                headers={'Authorization': 'Bearer ' + os.environ['ADDP_NOTEBOOK_OWNER_CAPABILITY_TOKEN']})
            assert urllib.request.urlopen(req, timeout=5).read() == b'owner-scoped'
            print(6 * 7)
        """)
        code += f"\nassert not Path('/workspace/notebooks/{other_id}/files/other-only').exists()"
        code += f"\nassert os.readlink('/proc/self/ns/pid') != {os.readlink('/proc/self/ns/pid')!r}"
        assert execute_cell(base, auth, code) == "42"
        assert httpx.get(ORIGIN + base + "api/status", headers=other_auth).status_code == 403
        assert httpx.get(ORIGIN + base + "api/status").status_code == 403
        document = api("GET", base + "api/contents/check.ipynb", auth).json()["content"]
        document["cells"].append(nbformat.v4.new_markdown_cell("interactive-save-42"))
        api("PUT", base + "api/contents/check.ipynb", auth, json={"type": "notebook", "content": document})
    assert "interactive-save-42" in path.read_text()
    response = api("POST", "/api/execute", json={"tenant_id": 7, "notebook_locator": LOCATOR,
                   "notebook_content_sha256": hashlib.sha256(path.read_bytes()).hexdigest()}).json()
    assert response["outputs"][0]["text"].strip() == "42"
    from addp_common.infra.objectstore import Ref
    saved = nbformat.read(ROOT / Ref.parse(response["output_locator"]).key, as_version=4)
    assert saved.cells[0].outputs[0].text.strip() == "42"
    failed = nbformat.v4.new_notebook(cells=[nbformat.v4.new_code_cell("raise ValueError('fixture failure')")],
                                       metadata=saved.metadata)
    path.write_text(nbformat.writes(failed))
    existing_outputs = set((ROOT / "tenant_7/executions").iterdir())
    rejected = httpx.post(ORIGIN + "/api/execute", headers=AUTH, timeout=100, json={"tenant_id": 7,
                           "notebook_locator": LOCATOR, "notebook_content_sha256": hashlib.sha256(path.read_bytes()).hexdigest()})
    assert rejected.status_code == 500
    assert set((ROOT / "tenant_7/executions").iterdir()) == existing_outputs
    assert list(Path("/workspace/notebooks").iterdir()) == [Path("/workspace/notebooks/control-only")]
    print("PASS: two isolated JupyterLab sessions, WebSocket execution, scoped callback, saved edits, batch saved output, failed batch cleanup")


if __name__ == "__main__":
    check()
