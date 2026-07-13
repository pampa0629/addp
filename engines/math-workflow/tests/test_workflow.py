"""Math Workflow 对 common-python 公共执行核心的集成测试。"""

import pytest

from addp_common.workflow_runtime import WorkflowRunner, WorkflowValidationError, validate_workflow_def
from operators import OPERATORS, get_operator_function


def math_runner() -> WorkflowRunner:
    return WorkflowRunner(set(OPERATORS), lambda operator, params: get_operator_function(operator)(**params))


def test_math_workflow_uses_common_runner():
    workflow_def = {
        "tasks": [
            {"id": "t1", "operator": "add", "params": {"a": 5, "b": 3}, "depends_on": []},
            {"id": "t2", "operator": "multiply", "params": {"a": {"$ref": "t1"}, "b": 2}, "depends_on": ["t1"]},
        ]
    }
    result = math_runner().execute(workflow_def)
    assert result.final_result == 16
    assert result.all_results == {"t1": 8, "t2": 16}
    assert result.task_order == ["t1", "t2"]


def test_math_workflow_supports_parallel_dependencies():
    workflow_def = {
        "tasks": [
            {"id": "t1", "operator": "add", "params": {"a": 10, "b": 20}, "depends_on": []},
            {"id": "t2", "operator": "multiply", "params": {"a": 5, "b": 6}, "depends_on": []},
            {"id": "t3", "operator": "add", "params": {"a": {"$ref": "t1"}, "b": {"$ref": "t2"}}, "depends_on": ["t1", "t2"]},
        ]
    }
    result = math_runner().execute(workflow_def)
    assert result.final_result == 60
    assert result.task_order[-1] == "t3"
    assert set(result.task_order[:2]) == {"t1", "t2"}


@pytest.mark.parametrize(
    "workflow_def, expected",
    [
        ({}, "tasks"),
        ({"tasks": []}, "非空数组"),
        ({"tasks": [{"operator": "add", "params": {}, "depends_on": []}]}, "id"),
        ({"tasks": [{"id": "t1", "operator": "missing", "params": {}, "depends_on": []}]}, "不存在"),
        ({"tasks": [{"id": "t1", "operator": "add", "params": {}, "depends_on": ["t1"]}]}, "不能依赖自身"),
    ],
)
def test_common_validation_rejects_invalid_math_workflows(workflow_def, expected):
    with pytest.raises(WorkflowValidationError, match=expected):
        validate_workflow_def(workflow_def, operator_ids=set(OPERATORS))
