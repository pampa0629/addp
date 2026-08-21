import json


def test_register_to_system_uses_canonical_runtime_identity(monkeypatch):
    calls = []

    monkeypatch.setenv("SYSTEM_URL", "http://system:8180")
    monkeypatch.setenv("GEOPYTHON_WORKFLOW_SERVICE_CLIENT_SECRET", "test-secret")
    monkeypatch.setenv("PORT", "8099")

    def fake_register_runtime_engine(system_url, client_id, client_secret, payload):
        calls.append((system_url, client_id, client_secret, payload))
        return 202, json.dumps({"success": True, "engine_id": 6})

    import addp_common.client as client

    monkeypatch.setattr(client, "register_runtime_engine", fake_register_runtime_engine)

    from api_server import register_to_system

    assert register_to_system() is True
    assert calls == [(
        "http://system:8180",
        "addp-geopython",
        "test-secret",
        {
            "engine_type": "geopython_workflow",
            "name": "GeoPython Workflow",
            "description": "基于 Python 地理计算生态的工作流引擎，支持 Pandas、GeoPandas、GDAL/OGR 等能力",
            "connection_info": {"protocol": "http", "port": 8099, "host": "localhost"},
            "capabilities": {
                "schema_version": "engine.capabilities/v1",
                "engine_type": "geopython_workflow",
                "engine_family": "workflow",
                "compute": {
                    "workflow": {
                        "supported": True,
                        "runtime_api": "addp.workflow/v1",
                        "dynamic_operators": True,
                    }
                },
            },
            "is_builtin": True,
        },
    )]
