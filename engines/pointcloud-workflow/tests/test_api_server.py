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


def test_workflow_endpoint_runs_asynchronously(client, monkeypatch):
    monkeypatch.setattr(api_server, "invoke_operator", lambda operator, params, timeout_seconds=None: {"operator": operator})
    response = client.post("/api/workflow", json={
        "workflow_def": {"tasks": [
            {"id": "convert", "operator": "las_to_copc", "params": {}, "depends_on": []}
        ]},
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
    assert status["result"] == {"operator": "las_to_copc"}
    assert status["all_results"] == {"convert": {"operator": "las_to_copc"}}
    assert status["task_order"] == ["convert"]


def test_workflow_endpoint_records_failure(client, monkeypatch):
    def fail(operator, params, timeout_seconds=None):
        raise RuntimeError("boom")

    monkeypatch.setattr(api_server, "invoke_operator", fail)
    response = client.post("/api/workflow", json={
        "workflow_def": {"tasks": [
            {"id": "convert", "operator": "las_to_copc", "params": {}, "depends_on": []}
        ]},
        "runtime": WRITE_RUNTIME,
    })
    execution_id = response.get_json()["execution_id"]
    deadline = time.time() + 2
    while time.time() < deadline:
        status = client.get(f"/api/executions/{execution_id}").get_json()
        if status["status"] == "failed":
            break
        time.sleep(0.01)
    assert status["error_code"] == "EXECUTION_FAILED"
    assert "boom" in status["error"]


def test_workflow_endpoint_rejects_insufficient_execution_authorization(client):
    response = client.post("/api/workflow", json={
        "workflow_def": {"tasks": [
            {"id": "convert", "operator": "las_to_copc", "params": {}, "depends_on": []}
        ]},
        "runtime": {"tenant_id": 7, "execution_authorization": {"id": 71, "effects": ["read"]}},
    })

    assert response.status_code == 400
    assert response.get_json()["error_code"] == "WORKFLOW_INVALID"


def test_register_to_system_with_retry_keeps_retrying_until_success(monkeypatch):
    attempts: list[int] = []
    waits: list[int] = []

    class FakeEvent:
        def wait(self, seconds):
            waits.append(seconds)

    def fake_register():
        attempts.append(len(attempts) + 1)
        return len(attempts) == 3

    monkeypatch.setattr(api_server, "converter_status", lambda: {"available": True})
    monkeypatch.setattr(api_server, "register_to_system", fake_register)
    monkeypatch.setattr(api_server.threading, "Event", lambda: FakeEvent())
    monkeypatch.setenv("REGISTRATION_RETRY_INTERVAL_SECONDS", "1")

    api_server.register_to_system_with_retry()

    assert attempts == [1, 2, 3]
    assert waits == [1, 1]


def test_register_to_system_with_retry_skips_when_pdal_unavailable(monkeypatch):
    called = False

    def fake_register():
        nonlocal called
        called = True
        return True

    monkeypatch.setattr(api_server, "converter_status", lambda: {"available": False, "details": "missing"})
    monkeypatch.setattr(api_server, "register_to_system", fake_register)

    api_server.register_to_system_with_retry()

    assert called is False
