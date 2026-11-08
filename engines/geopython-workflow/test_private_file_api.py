import hashlib
import json
import os
import shlex
import shutil
import subprocess
import sys
from dataclasses import replace
from datetime import datetime, timedelta, timezone
from uuid import uuid4
from pathlib import Path

import httpx
import pytest
from flask import Flask

import api_server
import private_file_api
from addp_common.auth import AuthorizationContext, RoleAssignment
from addp_common.workflow_runtime import ExecutionRegistry
from private_file_api import PrivateFileAPI
from test_private_files import _manifest


@pytest.fixture
def private_api(tmp_path, monkeypatch):
    app = Flask("private-file-test")
    api = PrivateFileAPI(app, parent=tmp_path)
    monkeypatch.setattr(api_server, "private_file_api", api)
    monkeypatch.setattr(api_server, "executions", ExecutionRegistry())
    app.add_url_rule("/api/workflow", view_func=api_server.execute_workflow_api, methods=["POST"])
    app.add_url_rule("/api/executions/<execution_id>", view_func=api_server.get_execution_status)
    context = AuthorizationContext(
        principal_id=71, principal_type="service", context_type="tenant", tenant_id=2,
        client_id="addp-develop", authentication_methods=("client_secret",),
        expires_at=datetime.now(timezone.utc) + timedelta(minutes=5),
        role_assignments=(RoleAssignment(8, "tenant.develop_runtime", "tenant",
                                        ("system.workflow_private_files.execute",), 2),))
    state = {"context": context, "tokens": []}

    async def current_context(system_url, token):
        state["tokens"].append(token)
        if "failure" in state:
            raise state["failure"]
        return state["context"]

    monkeypatch.setattr(private_file_api, "resolve_authorization_context", current_context)
    headers = {"Authorization": "Bearer current-service-token", "Accept-Language": "en"}
    with app.test_client() as client:
        yield client, api, state, headers
    for files in api._files.values():
        files.cleanup(files.creator)


def _created(client, headers, tmp_path, *, save=True):
    raw, contents = _manifest(tmp_path, save=save)
    identifier = str(uuid4())
    path = "/api/private-files/" + identifier
    response = client.put(path, json=raw, headers=headers)
    assert response.status_code == 201, response.get_json()
    assert response.get_json()["execution_id"] == raw["execution_id"]
    assert response.get_json()["runtime_execution_id"] == identifier
    return path, raw, contents


def _runtime(path):
    return {"runtime": {"tenant_id": 2, "execution_authorization": {
        "id": 101, "effects": ["read"]},
        "private_files": {"execution_id": path.rsplit("/", 1)[1]}}}


def test_private_files_api_real_bytes_canonical_execute_and_private_status(private_api, tmp_path):
    client, api, state, headers = private_api
    path, raw, contents = _created(client, headers, tmp_path)
    for index, content in enumerate(contents):
        assert client.put(path + f"/inputs/{index}", data=content, content_type="application/octet-stream",
                          headers=headers).status_code == 200
    submitted = client.post("/api/workflow", json=_runtime(path), headers=headers)
    assert submitted.status_code == 202, submitted.get_json()
    identifier = submitted.get_json()["execution_id"]
    assert identifier == path.rsplit("/", 1)[1] and identifier != raw["execution_id"]
    assert api.lookup(identifier, ("71", "2")).worker.wait(50)
    observed = client.get("/api/executions/" + identifier, headers=headers)
    assert observed.status_code == 200 and observed.get_json()["status"] == "success", observed.get_json()
    encoded = json.dumps(observed.get_json())
    assert "sha256" not in encoded and str(tmp_path) not in encoded and "cleanup_pending" not in encoded
    receipt = client.get(path, headers=headers).get_json()["outputs"][0]
    response = client.get(path + "/outputs/0", headers=headers)
    try:
        data = response.get_data()
        assert response.status_code == 200 and len(data) == response.content_length == int(receipt["size_bytes"])
        assert hashlib.sha256(data).hexdigest() == receipt["sha256"]
        assert data.startswith(b"SQLite format 3\x00")
    finally:
        response.close()
    assert client.get("/api/executions/" + identifier).status_code == 401
    assert client.delete(path, headers=headers).get_json() == {"cleanup": "passed"}
    assert client.put(path, json=raw, headers=headers).status_code == 409
    assert client.post("/api/workflow", json=_runtime(path), headers=headers).status_code == 409
    assert len(state["tokens"]) >= 10 and set(state["tokens"]) == {"current-service-token"}


def test_private_files_api_read_only_negative_uses_same_runner_diagnostic(private_api, tmp_path):
    client, api, _, headers = private_api
    raw, contents = _manifest(tmp_path, save=False)
    workflow = json.loads(raw["workflow_json"])
    workflow["tasks"][2]["params"]["id_field"] = "user-secret-field"
    raw["workflow_json"] = json.dumps(workflow)
    identifier = str(uuid4())
    path = "/api/private-files/" + identifier
    assert client.put(path, json=raw, headers=headers).status_code == 201
    for index, content in enumerate(contents):
        assert client.put(path + f"/inputs/{index}", data=content, content_type="application/octet-stream",
                          headers=headers).status_code == 200
    assert client.post("/api/workflow", json=_runtime(path), headers=headers).status_code == 202
    assert api.lookup(identifier, ("71", "2")).worker.wait(50)
    status = client.get("/api/executions/" + identifier, headers=headers).get_json()
    assert status["status"] == "failed" and status["error_code"] == "INVALID_PARAMS"
    assert status["diagnostic"]["reason"] == "operator_input_invalid"
    assert "user-secret-field" not in json.dumps(status)
    assert client.get(path, headers=headers).get_json()["outputs"] == []
    assert client.get(path + "/outputs/0", headers=headers).status_code == 404


def test_private_files_api_does_not_downgrade_public_save_business_effect(private_api, tmp_path):
    from operators import OPERATORS

    client, api, _, headers = private_api
    raw, _ = _manifest(tmp_path)
    assert "write" in OPERATORS["save"]["effects"]
    response = client.post("/api/workflow", headers=headers, json={
        "workflow_def": json.loads(raw["workflow_json"]),
        "runtime": {"tenant_id": 2, "execution_authorization": {"id": 101, "effects": ["read"]}}})
    assert response.status_code == 400 and response.get_json()["error_code"] == "WORKFLOW_INVALID"
    assert api._files == {}


@pytest.mark.parametrize("change", ["user", "platform", "client", "permissions", "delegated", "audience", "expired"])
def test_private_files_api_requires_current_tenant_develop_service(private_api, tmp_path, change):
    client, api, state, headers = private_api
    raw, _ = _manifest(tmp_path)
    changes = {"user": {"principal_type": "user"}, "platform": {"context_type": "platform", "tenant_id": None},
               "client": {"client_id": "addp-geopython"}, "permissions": {"role_assignments": ()},
               "delegated": {"token_type": "delegated_access_token"}, "audience": {"audiences": ("other",)},
               "expired": {"expires_at": datetime.now(timezone.utc) - timedelta(seconds=1)}}
    state["context"] = replace(state["context"], **changes[change])
    response = client.put("/api/private-files/" + str(uuid4()), json=raw, headers=headers)
    assert response.status_code == (401 if change == "expired" else 403)
    assert api._files == {} and not list(tmp_path.glob("addp-workflow-*"))


def test_private_files_api_rechecks_permission_and_exact_creator(private_api, tmp_path):
    client, _, state, headers = private_api
    path, raw, _ = _created(client, headers, tmp_path)
    original = state["context"]
    state["context"] = replace(original, principal_id=72)
    assert client.get(path, headers=headers).status_code == 404
    assert client.put(path, json=raw, headers=headers).status_code == 404
    state["context"] = replace(original, tenant_id=3, role_assignments=(replace(original.role_assignments[0], scope_tenant_id=3),))
    assert client.delete(path, headers=headers).status_code == 404
    state["context"] = replace(original, role_assignments=())
    assert client.get(path, headers=headers).status_code == 403
    assert client.get("/api/executions/" + path.rsplit("/", 1)[1], headers=headers).status_code == 403
    state["context"] = original
    assert client.get(path, headers=headers).status_code == 200


@pytest.mark.parametrize("failure,status", [
    (httpx.ConnectError("unreachable"), 503), (ValueError("invalid auth context"), 503),
    (httpx.HTTPStatusError("expired", request=httpx.Request("GET", "http://system/auth"),
                          response=httpx.Response(401)), 401),
    (httpx.HTTPStatusError("unavailable", request=httpx.Request("GET", "http://system/auth"),
                          response=httpx.Response(500)), 503),
])
def test_private_files_api_never_allows_unverified_auth_context(private_api, failure, status):
    client, api, state, headers = private_api
    state["failure"] = failure
    response = client.get("/api/private-files/" + str(uuid4()), headers=headers)
    assert response.status_code == status and api._files == {}
    assert "unreachable" not in response.get_data(as_text=True)


@pytest.mark.parametrize("change", ["definition", "input_data", "authorization", "tenant", "effects", "write_effect"])
def test_private_files_api_rejects_dispatch_rebinding_and_incomplete_input(private_api, tmp_path, change):
    client, api, _, headers = private_api
    path, _, _ = _created(client, headers, tmp_path)
    request = _runtime(path)
    if change == "definition":
        request["workflow_def"] = {"tasks": []}
    elif change == "input_data":
        request["input_data"] = {"replacement": True}
    elif change == "authorization":
        request["runtime"]["execution_authorization"]["id"] = 102
    elif change == "tenant":
        request["runtime"]["tenant_id"] = 3
    elif change == "effects":
        request["runtime"]["execution_authorization"]["effects"].append("ddl")
    else:
        request["runtime"]["execution_authorization"]["effects"].append("write")
    assert client.post("/api/workflow", json=request, headers=headers).status_code == 400
    assert client.post("/api/workflow", json=_runtime(path), headers=headers).status_code == 409
    assert api.lookup(path.rsplit("/", 1)[1], ("71", "2")).worker is None


def test_private_files_api_upload_integrity_media_and_cleanup_status(private_api, tmp_path, monkeypatch):
    client, api, _, headers = private_api
    path, _, contents = _created(client, headers, tmp_path)
    assert client.put(path + "/inputs/0", data=contents[0], headers=headers).status_code == 415
    response = client.put(path + "/inputs/0", data=contents[0][:-1], headers=headers,
                          content_type="application/octet-stream")
    assert response.status_code == 400 and response.get_json()["error_code"] == "PRIVATE_FILES_INPUT_MISMATCH"
    assert client.get(path + "/outputs/0", headers=headers).status_code == 409
    import private_files
    with monkeypatch.context() as scoped:
        def unavailable(path):
            raise OSError("private path must not be shown")
        scoped.setattr(private_files.shutil, "rmtree", unavailable)
        response = client.delete(path, headers=headers)
        assert response.status_code == 202 and response.get_json()["cleanup"] == "pending"
    assert client.delete(path, headers=headers).status_code == 200


def test_private_files_api_openapi_covers_all_owned_routes_and_machine_permission(private_api):
    client, _, _, _ = private_api
    spec = json.loads((Path(__file__).parent / "private_files_openapi.json").read_text())
    assert spec["openapi"] == "3.0.3"
    routes = set()
    for rule in client.application.url_map.iter_rules():
        if rule.endpoint == "static":
            continue
        path = str(rule).replace("<uuid:runtime_id>", "{runtime_execution_id}")
        path = path.replace("<int:index>", "{index}").replace("<execution_id>", "{execution_id}")
        for method in rule.methods - {"HEAD", "OPTIONS"}:
            routes.add((path, method.lower()))
    operations = {(path, method) for path, item in spec["paths"].items()
                  for method in item if method != "parameters"}
    assert operations == routes
    for path, method in operations:
        op = spec["paths"][path][method]
        assert op["security"] == [{"ServiceBearer": []}]
        assert op["x-addp-required-permissions"] == ["system.workflow_private_files.execute"]
    manifest = spec["components"]["schemas"]["Manifest"]
    assert manifest["additionalProperties"] is False
    assert set(manifest["required"]) == set(manifest["properties"])
    assert spec["components"]["schemas"]["PrivateWorkflowRequest"]["additionalProperties"] is False


def test_private_files_api_production_copy_bundle_imports_real_entry(tmp_path):
    root = Path(__file__).resolve().parents[2]
    stage = tmp_path / "app"
    stage.mkdir()
    for line in (root / "engines/geopython-workflow/Dockerfile").read_text().splitlines():
        if not line.startswith("COPY engines/geopython-workflow/"):
            continue
        parts = shlex.split(line)[1:]
        if parts[-1] not in {".", "./", "./operators/"}:
            continue
        for relative in parts[:-1]:
            source = root / relative
            if source.is_dir():
                shutil.copytree(source, stage / "operators", ignore=shutil.ignore_patterns("__pycache__"))
            else:
                shutil.copyfile(source, stage / source.name)
    env = {**os.environ, "PYTHONPATH": str(root / "common-python")}
    code = ("from pathlib import Path; import api_server, private_execution, private_files, private_file_api; "
            "assert all(Path(m.__file__).resolve().is_relative_to(Path.cwd()) for m in "
            "[api_server,private_execution,private_files,private_file_api]); "
            "assert any(str(r).startswith('/api/private-files/') for r in api_server.app.url_map.iter_rules())")
    result = subprocess.run([sys.executable, "-c", code], cwd=stage, env=env, text=True, capture_output=True, timeout=30)
    assert result.returncode == 0, result.stderr
