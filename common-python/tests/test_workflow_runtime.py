import threading

import pytest

from addp_common.workflow_runtime import (
    ExecutionRegistry,
    WorkflowRunner,
    WorkflowValidationError,
    validate_execution_authorization,
)


def test_runner_executes_dag_and_resolves_ports():
    runner = WorkflowRunner(
        {"add", "double"},
        lambda operator, params: params["a"] + params["b"] if operator == "add" else params["value"] * 2,
    )
    result = runner.execute({
        "tasks": [
            {"id": "a", "operator": "add", "params": {"a": 2, "b": 3}, "depends_on": []},
            {"id": "b", "operator": "double", "params": {"value": {"$ref": "a"}}, "depends_on": ["a"]},
        ]
    })
    assert result.task_order == ["a", "b"]
    assert result.all_results == {"a": 5, "b": 10}
    assert result.final_result == 10


def test_validation_rejects_undeclared_reference_dependency():
    runner = WorkflowRunner({"echo"}, lambda _operator, params: params)
    with pytest.raises(WorkflowValidationError, match="未在 depends_on 中声明"):
        runner.execute({
            "tasks": [
                {"id": "a", "operator": "echo", "params": {}, "depends_on": []},
                {"id": "b", "operator": "echo", "params": {"value": {"$ref": "a"}}, "depends_on": []},
            ]
        })


def test_execution_registry_runs_asynchronously():
    threads = []

    class DeferredThread:
        def __init__(self, *, target, args, daemon):
            self.target = target
            self.args = args
            self.daemon = daemon
            threads.append(self)

        def start(self):
            return None

    registry = ExecutionRegistry()
    runner = WorkflowRunner({"echo"}, lambda _operator, params: params["value"])
    submitted = registry.submit(
        runner,
        {"tasks": [{"id": "a", "operator": "echo", "params": {"value": 7}, "depends_on": []}]},
        thread_factory=DeferredThread,
    )
    assert submitted.status == "pending"
    threads[0].target(*threads[0].args)
    completed = registry.get(submitted.execution_id)
    assert completed is not None
    assert completed.status == "success"
    assert completed.result == 7
    assert completed.progress == 100


def test_execution_authorization_covers_dag_effects():
    workflow = {
        "tasks": [
            {"id": "load", "operator": "load", "params": {}, "depends_on": []},
            {"id": "save", "operator": "save", "params": {}, "depends_on": ["load"]},
        ]
    }
    effects = validate_execution_authorization(
        workflow,
        operator_effects={"load": ["read"], "save": ["write"]},
        runtime={"tenant_id": 7, "execution_authorization": {"id": 71, "effects": ["read", "write"]}},
    )
    assert effects == ["read", "write"]


def test_execution_authorization_rejects_missing_effect():
    workflow = {
        "tasks": [{"id": "save", "operator": "save", "params": {}, "depends_on": []}]
    }
    with pytest.raises(WorkflowValidationError, match="write"):
        validate_execution_authorization(
            workflow,
            operator_effects={"save": ["write"]},
            runtime={"tenant_id": 7, "execution_authorization": {"id": 71, "effects": ["read"]}},
        )


def test_execution_authorization_rejects_missing_tenant_context():
    workflow = {
        "tasks": [{"id": "load", "operator": "load", "params": {}, "depends_on": []}]
    }
    with pytest.raises(WorkflowValidationError, match="runtime.tenant_id"):
        validate_execution_authorization(
            workflow,
            operator_effects={"load": ["read"]},
            runtime={"execution_authorization": {"id": 71, "effects": ["read"]}},
        )
