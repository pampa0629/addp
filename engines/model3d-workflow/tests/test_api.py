import sys
from pathlib import Path

import pytest


for parent in Path(__file__).resolve().parents:
    contract_path = parent / "docs" / "workflow_operator_contract.py"
    if contract_path.exists():
        sys.path.insert(0, str(contract_path.parent))
        break

from workflow_operator_contract import assert_operator_metadata_contract


@pytest.fixture
def client():
    from api_server import app, executions

    app.config["TESTING"] = True
    executions.clear()
    with app.test_client() as client:
        yield client


def test_health(client):
    response = client.get("/health")
    assert response.status_code == 200
    data = response.get_json()
    assert data["status"] in {"healthy", "degraded"}
    assert data["service"] == "model3d-workflow-engine"
    assert data["operators_count"] == 6
    assert "conversion_ready" in data
    assert data["dependencies"]["converter"]["binding"] == "model3d_workflow"


def test_get_operators(client):
    response = client.get("/api/operators")
    assert response.status_code == 200
    data = response.get_json()
    assert data["status"] == "success"
    assert data["count"] == 6
    assert_operator_metadata_contract(data["operators"], expected_engine_type="model3d_workflow")


def test_workflow_endpoint_is_present_but_unsupported(client):
    response = client.post("/api/workflow", json={"workflow_def": {}})
    assert response.status_code == 400
    data = response.get_json()
    assert data["error_code"] == "WORKFLOW_NOT_SUPPORTED"


def test_invoke_unknown_operator(client):
    response = client.post("/api/operators/unknown/invoke", json={"params": {}})
    assert response.status_code == 404
    data = response.get_json()
    assert data["error_code"] == "OPERATOR_NOT_FOUND"
    assert "execution_id" in data


def test_invoke_requires_params_object(client):
    response = client.post("/api/operators/osgb_scene_to_3dtiles/invoke", json={})
    assert response.status_code == 400
    data = response.get_json()
    assert data["error_code"] == "INVALID_PARAMS"


def test_invoke_scene_success_and_execution_status(client, tmp_path, monkeypatch):
    import operators

    source = tmp_path / "scene"
    target = tmp_path / "tiles"
    source.mkdir()

    def fake_run_command(command, timeout_seconds):
        target.mkdir(exist_ok=True)
        (target / "tileset.json").write_text("{}", encoding="utf-8")
        (target / "0.b3dm").write_bytes(b"tile")
        return operators.CommandResult(returncode=0, stdout="ok")

    monkeypatch.setattr(operators, "run_command", fake_run_command)
    monkeypatch.setenv("MODEL3D_CONVERTER_BIN", sys.executable)

    response = client.post(
        "/api/operators/osgb_scene_to_3dtiles/invoke",
        json={
            "params": {
                "access_plan": {
                    "source": {"root_uri": str(source)},
                    "target": {"dataset_root_uri": str(target)},
                }
            }
        },
    )

    assert response.status_code == 200
    data = response.get_json()
    assert data["status"] == "success"
    assert data["result"]["tileset_ref"] == "tileset.json"
    assert data["result"]["tile_count"] == 1

    status_response = client.get(f"/api/executions/{data['execution_id']}")
    assert status_response.status_code == 200
    status = status_response.get_json()
    assert status["status"] == "success"
    assert status["progress"] == 100


def test_converter_unavailable_response(client, tmp_path, monkeypatch):
    monkeypatch.setenv("MODEL3D_CONVERTER_BIN", str(tmp_path / "missing_converter"))

    response = client.post(
        "/api/operators/osgb_scene_to_3dtiles/invoke",
        json={
            "params": {
                "access_plan": {
                    "source": {"root_uri": str(tmp_path / "source")},
                    "target": {"dataset_root_uri": str(tmp_path / "target")},
                }
            }
        },
    )

    assert response.status_code == 503
    data = response.get_json()
    assert data["status"] == "failed"
    assert data["error_code"] == "CONVERTER_UNAVAILABLE"


def test_register_to_system_posts_model3d_workflow_payload(monkeypatch):
    import json

    import api_server

    calls = []

    class FakeResponseContext:
        status = 202

        def __enter__(self):
            return self

        def __exit__(self, exc_type, exc, traceback):
            return False

        def read(self):
            return b'{"engine_id":1}'

    def fake_urlopen(request, timeout):
        calls.append(
            {
                "url": request.full_url,
                "json": json.loads(request.data.decode("utf-8")),
                "headers": dict(request.header_items()),
                "timeout": timeout,
            }
        )
        return FakeResponseContext()

    monkeypatch.setattr(api_server.urlrequest, "urlopen", fake_urlopen)
    monkeypatch.setenv("SYSTEM_URL", "http://system:8180")
    monkeypatch.setenv("INTERNAL_API_KEY", "internal-key")
    monkeypatch.setenv("PORT", "8101")
    monkeypatch.setenv("MODEL3D_CONVERTER_BIN", sys.executable)

    assert api_server.register_to_system() is True
    assert calls == [
        {
            "url": "http://system:8180/api/v1/internal/engines/register",
            "json": {
                "engine_type": "model3d_workflow",
                "name": "Model3D 工作流引擎",
                "description": "三维模型转换专用工作流运行时，提供 OSGB 快显和 OSGB Scene 转 3D Tiles direct 算子",
                "connection_info": {"protocol": "http", "port": 8101},
                "is_builtin": True,
            },
            "headers": {"Content-type": "application/json", "X-internal-api-key": "internal-key"},
            "timeout": 10,
        }
    ]
