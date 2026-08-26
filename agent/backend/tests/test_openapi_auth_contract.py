def test_agent_openapi_declares_authorization_contracts():
    from main import app

    specification = app.openapi()
    paths = specification["paths"]

    expected = {
        ("get", "/api/v1/agent/sessions"): ("permission", ["agent.session.read"]),
        ("post", "/api/v1/agent/sessions"): ("permission", ["agent.session.create"]),
        ("get", "/api/v1/agent/sessions/{session_id}"): ("permission", ["agent.session.read"]),
        ("delete", "/api/v1/agent/sessions/{session_id}"): ("permission", ["agent.session.delete"]),
        ("get", "/api/v1/agent/sessions/{session_id}/messages"): ("permission", ["agent.session.read"]),
        ("post", "/api/v1/agent/chat"): (
            "permission",
            ["agent.run.create", "agent.run.execute"],
        ),
        ("get", "/api/v1/agent/runs/{agent_run_id}"): ("permission", ["agent.run.read"]),
        ("get", "/api/v1/agent/runs/{agent_run_id}/events"): ("permission", ["agent.run.read"]),
        ("post", "/api/v1/agent/runs/{agent_run_id}/cancel"): ("permission", ["agent.run.cancel"]),
        ("post", "/api/v1/agent/runs/{agent_run_id}/retry"): ("permission", ["agent.run.execute"]),
        ("get", "/api/v1/agent/settings/inference-bindings/{scenario_code}"): (
            "permission",
            ["agent.configuration.read"],
        ),
        ("put", "/api/v1/agent/settings/inference-bindings/{scenario_code}"): (
            "permission",
            ["agent.configuration.update"],
        ),
    }
    for (method, path), (mode, permissions) in expected.items():
        operation = paths[path][method]
        assert operation["x-addp-auth-mode"] == mode
        assert operation["x-addp-required-permissions"] == permissions

    for health_path in ("/health/live", "/health/ready"):
        health = paths[health_path]["get"]
        assert health["x-addp-auth-mode"] == "public"
        assert "x-addp-required-permissions" not in health


def test_agent_health_and_business_gate_are_distinct_before_registration():
    from fastapi.testclient import TestClient
    from main import app

    client = TestClient(app)
    live = client.get("/health/live")
    ready = client.get("/health/ready")
    business = client.get("/api/v1/agent/sessions")

    assert live.status_code == 200
    assert live.json()["status"] == "live"
    assert ready.status_code == 503
    assert ready.json()["registration_state"] == "starting"
    assert business.status_code == 503
    assert business.json()["error_code"] == "module_not_ready"
