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
    monkeypatch.setenv("RUNTIME_PUBLIC_PORT", "18105")

    assert api_server.register_to_system() is True
    assert captured["client_id"] == "addp-document"
    assert captured["payload"]["engine_type"] == "document_workflow"
    assert captured["payload"]["connection_info"]["port"] == 18105


def test_registration_retry_registers_when_libreoffice_unavailable(monkeypatch):
    called = False

    def fake_register():
        nonlocal called
        called = True
        return True

    monkeypatch.setattr(api_server, "converter_status", lambda: {"available": False, "details": "missing"})
    monkeypatch.setattr(api_server, "register_to_system", fake_register)
    api_server.register_to_system_with_retry()
    assert called is True


@pytest.mark.parametrize("available,code,status", [(True, 200, "healthy"), (False, 503, "unhealthy")])
def test_health_uses_whole_runtime_readiness(client, monkeypatch, available, code, status):
    monkeypatch.setattr(api_server, "converter_status", lambda: {"available": available, "path": "/private/runtime", "details": "private details"})
    response = client.get("/health")
    assert response.status_code == code
    body = response.get_json()
    assert body["status"] == status
    assert body["service"] and body["version"]
    assert "conversion_ready" not in body
    fact = body["dependencies"]["libreoffice"]
    assert fact["required"] is True and fact["available"] is available
    assert "/private" not in response.get_data(as_text=True)


def test_unhealthy_runtime_still_registers_identity(monkeypatch):
    monkeypatch.setattr(api_server, "converter_status", lambda: {"available": False})
    calls = []
    monkeypatch.setattr("addp_common.client.register_runtime_engine", lambda *args: (calls.append(args) or (202, "accepted")))
    assert api_server.register_to_system() is True
    assert len(calls) == 1


def test_script_entry_defers_registration_until_listener(monkeypatch):
    import api_server
    import runpy
    from unittest.mock import patch
    from addp_common.client.runtime_registration import register_after_listener

    monkeypatch.setenv("PORT", "28101")
    with patch("threading.Thread") as thread, patch("flask.Flask.run") as run:
        runpy.run_path(api_server.__file__, run_name="__main__")
    thread.assert_called_once()
    assert thread.call_args.kwargs["target"] is register_after_listener
    callback, port = thread.call_args.kwargs["args"]
    assert callable(callback) and port == 28101
    assert thread.call_args.kwargs["daemon"] is True
    run.assert_called_once()
