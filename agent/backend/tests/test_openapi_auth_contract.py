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

    health = paths["/health"]["get"]
    assert health["x-addp-auth-mode"] == "public"
    assert "x-addp-required-permissions" not in health
