from __future__ import annotations

import time

import api_server
import pytest


WRITE_RUNTIME = {
    "tenant_id": 7,
    "execution_authorization": {"id": 71, "effects": ["read", "write"]},
}


@pytest.fixture
def client():
    api_server.app.config["TESTING"] = True
    api_server.executions.clear()
    with api_server.app.test_client() as test_client:
        yield test_client


def test_health_reports_libreoffice_dependency(client, monkeypatch):
    monkeypatch.setattr(api_server, "converter_status", lambda: {"available": True, "version": "LibreOffice 7"})
    response = client.get("/health")
    assert response.status_code == 200
    assert response.get_json()["dependencies"]["libreoffice"]["version"] == "LibreOffice 7"


def test_workflow_endpoint_runs_asynchronously(client, monkeypatch):
    monkeypatch.setattr(api_server, "invoke_operator", lambda operator, params, timeout_seconds=None: {"operator": operator})
    response = client.post("/api/workflow", json={
        "workflow_def": {"tasks": [{"id": "convert", "operator": "document_to_pdf", "params": {}, "depends_on": []}]},
        "runtime": WRITE_RUNTIME,
    })
    assert response.status_code == 202
    execution_id = response.get_json()["execution_id"]
    deadline = time.time() + 2
    while time.time() < deadline:
        status = client.get(f"/api/executions/{execution_id}").get_json()
        if status["status"] == "success":
            break
        time.sleep(0.01)
    assert status["result"] == {"operator": "document_to_pdf"}
    assert status["all_results"] == {"convert": {"operator": "document_to_pdf"}}


def test_workflow_rejects_missing_write_authorization(client):
    response = client.post("/api/workflow", json={
        "workflow_def": {"tasks": [{"id": "convert", "operator": "document_to_pdf", "params": {}, "depends_on": []}]},
        "runtime": {"tenant_id": 7, "execution_authorization": {"id": 71, "effects": ["read"]}},
    })
    assert response.status_code == 400
    assert response.get_json()["error_code"] == "WORKFLOW_INVALID"


def test_direct_invocation_records_typed_failure(client, monkeypatch):
    def fail(operator, params, timeout_seconds=None):
        raise api_server.ConverterError("CONVERSION_FAILED", "failed", details="boom", http_status=500)

    monkeypatch.setattr(api_server, "invoke_operator", fail)
    response = client.post("/api/operators/document_to_pdf/invoke", json={"params": {}})
    assert response.status_code == 500
    payload = response.get_json()
    assert payload["error_code"] == "CONVERSION_FAILED"
    execution = client.get(f"/api/executions/{payload['execution_id']}").get_json()
    assert execution["status"] == "failed"
    assert execution["details"] == "boom"


def test_register_to_system_uses_document_identity(monkeypatch):
    captured = {}

    def fake_register(system_url, client_id, client_secret, payload):
        captured.update(system_url=system_url, client_id=client_id, client_secret=client_secret, payload=payload)
        return 202, {"status": "accepted"}

    monkeypatch.setattr(api_server, "converter_status", lambda: {"available": True})
    monkeypatch.setattr("addp_common.client.register_runtime_engine", fake_register)
    monkeypatch.setenv("SYSTEM_URL", "http://system:8180")
    monkeypatch.setenv("DOCUMENT_WORKFLOW_SERVICE_CLIENT_SECRET", "secret")
    monkeypatch.setenv("RUNTIME_HOST", "document-workflow-engine")

    assert api_server.register_to_system() is True
    assert captured["client_id"] == "addp-document"
    assert captured["payload"]["engine_type"] == "document_workflow"
    assert captured["payload"]["connection_info"]["port"] == 8105


def test_registration_retry_skips_when_libreoffice_unavailable(monkeypatch):
    called = False

    def fake_register():
        nonlocal called
        called = True
        return True

    monkeypatch.setattr(api_server, "converter_status", lambda: {"available": False, "details": "missing"})
    monkeypatch.setattr(api_server, "register_to_system", fake_register)
    api_server.register_to_system_with_retry()
    assert called is False

