import json
from pathlib import Path

import yaml

from workflow_operator_contract import assert_operator_metadata_contract


ROOT = Path(__file__).resolve().parent


def load_spec():
    with (ROOT / "workflow-engine-api-v1.yaml").open(encoding="utf-8") as f:
        return yaml.safe_load(f)


def test_openapi_uses_addp_workflow_v1_paths():
    spec = load_spec()

    assert "/health" in spec["paths"]
    assert "/api/operators" in spec["paths"]
    assert "/api/workflow" in spec["paths"]
    assert "/api/operators/{name}/invoke" in spec["paths"]
    assert "/api/executions/{execution_id}" in spec["paths"]

    paths = set(spec["paths"].keys())
    assert "/api/spatial/workflow" not in paths
    assert "/api/spatial/operators/{name}/execute" not in paths


def test_task_definition_requires_explicit_dag_fields():
    spec = load_spec()
    task = spec["components"]["schemas"]["TaskDefinition"]

    assert set(task["required"]) == {"id", "operator", "params", "depends_on"}
    assert task["properties"]["depends_on"]["type"] == "array"


def test_operator_metadata_uses_engine_type_and_category_path():
    spec = load_spec()
    operator = spec["components"]["schemas"]["OperatorMetadata"]

    assert "module" not in operator["properties"]
    assert "outputs" not in operator["properties"]
    assert {"engine_type", "category_path", "execution_modes", "effects"}.issubset(operator["required"])
    assert set(operator["properties"]["effects"]["items"]["enum"]) == {
        "read", "write", "ddl", "external_effect"
    }


def test_parameter_metadata_exposes_resource_picker_contract():
    spec = load_spec()
    parameter = spec["components"]["schemas"]["ParameterMetadata"]["properties"]

    for field in ["param_type", "ui_type", "ui_config", "depends_on", "show_when", "item_type"]:
        assert field in parameter
    assert "engine_families" in parameter["ui_config"]["example"]


def test_error_codes_include_direct_not_supported():
    spec = load_spec()
    error_codes = spec["components"]["schemas"]["ErrorResponse"]["properties"]["error_code"]["enum"]

    assert "DIRECT_NOT_SUPPORTED" in error_codes
    assert "EXECUTION_NOT_FOUND" in error_codes


def test_workflow_response_documents_json_serializable_result_summary():
    spec = load_spec()
    response = spec["components"]["schemas"]["WorkflowExecuteResponse"]["properties"]

    assert "JSON 序列化" in response["final_result"]["description"]
    assert "轻量摘要" in response["all_results"]["description"]


def test_execution_status_uses_single_status_field():
    spec = load_spec()
    response = spec["components"]["schemas"]["ExecutionStatusResponse"]

    assert "task_status" not in response["properties"]
    assert "progress" in response["required"]
    assert "execution_time_ms" in response["properties"]
    assert "cancelled" in response["properties"]["status"]["enum"]


def test_workflow_execute_response_allows_async_acceptance_status():
    spec = load_spec()
    status_enum = spec["components"]["schemas"]["WorkflowExecuteResponse"]["properties"]["status"]["enum"]

    assert {"success", "failed", "running", "pending"}.issubset(status_enum)


def test_workflow_execute_request_requires_execution_authorization():
    spec = load_spec()
    request = spec["components"]["schemas"]["WorkflowExecuteRequest"]
    runtime = spec["components"]["schemas"]["WorkflowRuntimeContext"]
    authorization = spec["components"]["schemas"]["WorkflowExecutionAuthorization"]

    assert {"workflow_def", "runtime"}.issubset(request["required"])
    assert runtime["additionalProperties"] is False
    assert runtime["required"] == ["execution_authorization"]
    assert authorization["additionalProperties"] is False
    assert set(authorization["required"]) == {"id", "effects"}
    assert authorization["properties"]["id"]["minimum"] == 1
    assert authorization["properties"]["effects"]["minItems"] == 1
    assert authorization["properties"]["effects"]["uniqueItems"] is True
    assert set(authorization["properties"]["effects"]["items"]["enum"]) == {
        "read", "write", "ddl", "external_effect"
    }


def test_operator_list_example_has_no_module_field():
    with (ROOT / "examples" / "operator-list-response.json").open(encoding="utf-8") as f:
        payload = json.load(f)

    assert payload["status"] == "success"
    assert_operator_metadata_contract(payload["operators"])


def test_execution_status_example_uses_single_status_field():
    with (ROOT / "examples" / "execution-status-response.json").open(encoding="utf-8") as f:
        payload = json.load(f)

    assert payload["status"] == "success"
    assert "task_status" not in payload
    assert "execution_time_ms" in payload
