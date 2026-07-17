from pathlib import Path

import yaml


def load_spec():
    return yaml.safe_load(Path("swagger.yaml").read_text())


def definition(spec, name):
    return spec["definitions"][f"github_com_addp_develop_backend_internal_models.{name}"]


def ref_name(ref):
    return ref.rsplit("/", 1)[-1].replace("github_com_addp_develop_backend_internal_models.", "")


def test_dev_task_requests_use_explicit_swagger_contracts():
    spec = load_spec()
    create_ref = spec["paths"]["/task-definitions"]["post"]["parameters"][0]["schema"]["$ref"]
    update_ref = spec["paths"]["/task-definitions/{id}"]["put"]["parameters"][1]["schema"]["$ref"]

    assert ref_name(create_ref) == "CreateDevTaskSwaggerRequest"
    assert ref_name(update_ref) == "UpdateDevTaskSwaggerRequest"


def test_dev_task_responses_use_explicit_swagger_contracts():
    spec = load_spec()
    create_ref = spec["paths"]["/task-definitions"]["post"]["responses"]["200"]["schema"]["$ref"]
    detail_ref = spec["paths"]["/task-definitions/{id}"]["get"]["responses"]["200"]["schema"]["$ref"]
    update_ref = spec["paths"]["/task-definitions/{id}"]["put"]["responses"]["200"]["schema"]["$ref"]
    list_ref = spec["paths"]["/task-definitions"]["get"]["responses"]["200"]["schema"]["$ref"]
    provider_detail_ref = spec["paths"]["/tasks/{task_type}/{id}"]["get"]["responses"]["200"]["schema"]["$ref"]
    provider_list_ref = spec["paths"]["/tasks"]["get"]["responses"]["200"]["schema"]["$ref"]

    assert ref_name(create_ref) == "DevTaskSwagger"
    assert ref_name(detail_ref) == "DevTaskSwagger"
    assert ref_name(update_ref) == "DevTaskSwagger"
    assert ref_name(list_ref) == "ListDevTasksSwaggerResponse"
    assert ref_name(provider_detail_ref) == "ProviderDevTaskSwagger"
    assert ref_name(provider_list_ref) == "ListProviderDevTasksSwaggerResponse"


def test_notebook_task_responses_use_explicit_swagger_contracts():
    spec = load_spec()
    upload_ref = spec["paths"]["/notebooks/upload"]["post"]["responses"]["200"]["schema"]["$ref"]
    list_ref = spec["paths"]["/notebooks"]["get"]["responses"]["200"]["schema"]["$ref"]

    assert ref_name(upload_ref) == "UploadNotebookSwaggerResponse"
    assert ref_name(list_ref) == "ListDevTasksSwaggerResponse"

    upload = definition(spec, "UploadNotebookSwaggerResponse")
    dev_task_ref = upload["properties"]["dev_task"]["$ref"]
    assert ref_name(dev_task_ref) == "DevTaskSwagger"


def test_execution_request_uses_explicit_swagger_contract():
    spec = load_spec()
    execute_ref = spec["paths"]["/executions"]["post"]["parameters"][0]["schema"]["$ref"]

    assert ref_name(execute_ref) == "CreateExecutionSwaggerRequest"


def test_execution_responses_use_explicit_swagger_contracts():
    spec = load_spec()
    detail_ref = spec["paths"]["/executions/{execution_id}"]["get"]["responses"]["200"]["schema"]["$ref"]
    list_ref = spec["paths"]["/executions"]["get"]["responses"]["200"]["schema"]["$ref"]

    assert ref_name(detail_ref) == "ExecutionWithDevTaskSwagger"
    assert ref_name(list_ref) == "ListExecutionsSwaggerResponse"

    detail = definition(spec, "ExecutionWithDevTaskSwagger")
    dev_task_ref = detail["properties"]["dev_task"]["$ref"]
    assert ref_name(dev_task_ref) == "DevTaskSwagger"


def test_workflow_swagger_schema_requires_standard_task_fields():
    spec = load_spec()
    workflow = definition(spec, "WorkflowDefinitionSwagger")
    assert "tasks" in workflow["required"]

    task_ref = workflow["properties"]["tasks"]["items"]["$ref"]
    task = definition(spec, ref_name(task_ref))

    for field in ("id", "operator", "params", "depends_on"):
        assert field in task["properties"]
        assert field in task["required"]
    assert task["properties"]["depends_on"]["items"]["type"] == "string"


def test_create_and_execution_swagger_requests_expose_conditional_execution_fields():
    spec = load_spec()
    create = definition(spec, "CreateDevTaskSwaggerRequest")
    execution = definition(spec, "CreateExecutionSwaggerRequest")

    for field in ("name", "dev_type", "content"):
        assert field in create["required"]
    for field in (
        "dev_type",
        "trigger_type",
        "execution_config",
        "approval_id",
        "request_fingerprint",
    ):
        assert field in execution["properties"]
    assert "required" not in execution


def test_tool_approval_swagger_contract_exposes_owner_projection_and_single_decision_route():
    spec = load_spec()
    approval_get = spec["paths"]["/approvals/{id}"]["get"]
    approval_decision = spec["paths"]["/approvals/{id}/decision"]["post"]

    get_ref = approval_get["responses"]["200"]["schema"]["$ref"]
    decision_ref = approval_decision["responses"]["200"]["schema"]["$ref"]
    assert ref_name(get_ref) == "ToolApprovalResponse"
    assert ref_name(decision_ref) == "ToolApprovalResponse"

    approval = definition(spec, "ToolApprovalResponse")
    for field in ("id", "status", "request_fingerprint", "request_summary"):
        assert field in approval["properties"]
    assert "request_payload" not in approval["properties"]
