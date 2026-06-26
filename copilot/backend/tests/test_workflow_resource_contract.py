import importlib.util
import asyncio
from pathlib import Path

import pytest
from pydantic import ValidationError

from models.workflow_models import DataSourceCandidate, DataSourceContext, DataSourceLocation, Workflow, Task

BACKEND_DIR = Path(__file__).resolve().parents[1]


def load_module(name: str, relative_path: str):
    spec = importlib.util.spec_from_file_location(name, BACKEND_DIR / relative_path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


workflow_validation = load_module("workflow_validation_chain_module", "chains/workflow_validation_chain.py")
resource_locator = load_module("resource_locator_module", "utils/resource_locator.py")

WorkflowValidationChain = workflow_validation.WorkflowValidationChain
bucket_locator = resource_locator.bucket_locator
object_locator = resource_locator.object_locator
schema_locator = resource_locator.schema_locator
table_locator = resource_locator.table_locator


class FakeOperatorDetailTool:
    def __init__(self):
        self.seen_workflow_engine_ids = []

    async def _arun(self, operator_name: str, workflow_engine_id: int):
        self.seen_workflow_engine_ids.append(workflow_engine_id)
        definitions = {
            "load": {
                "execution_modes": ["workflow"],
                "parameters": [
                    {"name": "source_type", "required": True},
                    {"name": "locator", "required": True},
                ],
                "output_ports": [{"name": "default"}],
            },
            "save": {
                "execution_modes": ["workflow"],
                "parameters": [
                    {"name": "input_df", "required": True},
                    {"name": "target_type", "required": True},
                    {"name": "target_parent_locator", "required": True},
                    {"name": "target_name", "required": True},
                ],
                "output_ports": [{"name": "default"}],
            },
            "direct_only": {
                "execution_modes": ["direct"],
                "parameters": [],
                "output_ports": [{"name": "default"}],
            },
        }
        return definitions.get(operator_name)


def test_resource_locators_are_constructed_from_standard_contract():
    assert table_locator(12, "public", "roads") == "addp://engine/12/path/public/roads?type=table"
    assert schema_locator(12, "public") == "addp://engine/12/path/public?type=schema"
    assert object_locator(4, "addp", "lake/roads.parquet") == "addp://engine/4/path/addp/lake/roads.parquet?type=object"
    assert bucket_locator(4, "addp") == "addp://engine/4/path/addp?type=bucket"


def test_data_source_location_uses_namespace_field():
    location = DataSourceLocation(namespace="public", table="roads")
    payload = location.model_dump(exclude_none=True)

    assert payload == {"namespace": "public", "table": "roads"}
    assert "schema" not in payload


def test_data_source_context_uses_typed_candidates():
    candidate = DataSourceCandidate(
        engine_id=12,
        engine_name="PostgreSQL",
        engine_type="postgresql",
        location=DataSourceLocation(
            namespace="public",
            table="roads",
            locator="addp://engine/12/path/public/roads?type=table",
        ),
        confidence=0.7,
    )
    context = DataSourceContext(
        engine_id=0,
        engine_name="unknown",
        engine_type="unknown",
        location=DataSourceLocation(),
        confidence=0.5,
        alternatives=[candidate],
    )

    payload = context.model_dump(exclude_none=True)

    assert payload["alternatives"][0]["location"]["namespace"] == "public"
    assert "schema" not in payload["alternatives"][0]["location"]


def test_task_requires_explicit_depends_on():
    with pytest.raises(ValidationError) as exc_info:
        Task(
            id="task1",
            operator="load",
            params={
                "source_type": "table",
                "locator": "addp://engine/12/path/public/roads?type=table",
            },
        )

    assert "depends_on" in str(exc_info.value)


def test_validation_rejects_runtime_derived_resource_params():
    asyncio.run(_assert_validation_rejects_runtime_derived_resource_params())


async def _assert_validation_rejects_runtime_derived_resource_params():
    tool = FakeOperatorDetailTool()
    validator = WorkflowValidationChain(tool)
    workflow = Workflow(tasks=[
        Task(
            id="task1",
            operator="load",
            params={
                "source_type": "table",
                "engine_id": 12,
                "schema": "public",
                "table": "roads",
            },
            depends_on=[],
        )
    ])

    result = await validator.validate(workflow, workflow_engine_id=9)

    assert not result.is_valid
    assert any("运行时派生参数" in error for error in result.errors)
    assert any("locator" in error for error in result.errors)
    assert tool.seen_workflow_engine_ids == []


def test_validation_accepts_locator_resource_contract():
    asyncio.run(_assert_validation_accepts_locator_resource_contract())


async def _assert_validation_accepts_locator_resource_contract():
    tool = FakeOperatorDetailTool()
    validator = WorkflowValidationChain(tool)
    workflow = Workflow(tasks=[
        Task(
            id="task1",
            operator="load",
            params={
                "source_type": "table",
                "locator": "addp://engine/12/path/public/roads?type=table",
            },
            depends_on=[],
        ),
        Task(
            id="task2",
            operator="save",
            params={
                "input_df": {"$ref": "task1"},
                "target_type": "table",
                "target_parent_locator": "addp://engine/12/path/public?type=schema",
                "target_name": "roads_buffer",
            },
            depends_on=["task1"],
        ),
    ])

    result = await validator.validate(workflow, workflow_engine_id=9)

    assert result.is_valid
    assert result.errors == []
    assert tool.seen_workflow_engine_ids == [9, 9]


def test_validation_rejects_storage_engine_type_as_source_type():
    asyncio.run(_assert_validation_rejects_storage_engine_type_as_source_type())


async def _assert_validation_rejects_storage_engine_type_as_source_type():
    tool = FakeOperatorDetailTool()
    validator = WorkflowValidationChain(tool)
    workflow = Workflow(tasks=[
        Task(
            id="task1",
            operator="load",
            params={
                "source_type": "nfs",
                "locator": "addp://engine/3/path/data/roads.csv?type=file",
            },
            depends_on=[],
        )
    ])

    result = await validator.validate(workflow, workflow_engine_id=9)

    assert not result.is_valid
    assert any("source_type" in error and "file" in error for error in result.errors)
    assert tool.seen_workflow_engine_ids == []


def test_validation_requires_workflow_engine_id():
    asyncio.run(_assert_validation_requires_workflow_engine_id())


async def _assert_validation_requires_workflow_engine_id():
    validator = WorkflowValidationChain(FakeOperatorDetailTool())
    workflow = Workflow(tasks=[
        Task(
            id="task1",
            operator="load",
            params={
                "source_type": "table",
                "locator": "addp://engine/12/path/public/roads?type=table",
            },
            depends_on=[],
        )
    ])

    result = await validator.validate(workflow)

    assert not result.is_valid
    assert any("workflow_engine_id" in error for error in result.errors)


def test_validation_rejects_direct_only_operator():
    asyncio.run(_assert_validation_rejects_direct_only_operator())


async def _assert_validation_rejects_direct_only_operator():
    tool = FakeOperatorDetailTool()
    validator = WorkflowValidationChain(tool)
    workflow = Workflow(tasks=[
        Task(
            id="task1",
            operator="direct_only",
            params={},
            depends_on=[],
        )
    ])

    result = await validator.validate(workflow, workflow_engine_id=9)

    assert not result.is_valid
    assert any("不支持工作流编排" in error for error in result.errors)
    assert tool.seen_workflow_engine_ids == [9]


def test_workflow_prompts_describe_locator_resource_contract():
    prompt_files = [
        BACKEND_DIR / "prompts" / "workflow_generation.txt",
        BACKEND_DIR / "prompts" / "workflow_modification.txt",
    ]
    prompt_text = "\n".join(path.read_text(encoding="utf-8") for path in prompt_files)

    assert "locator" in prompt_text
    assert "target_parent_locator + target_name" in prompt_text
    assert "禁止在算子 params 中填写 `engine_id`" in prompt_text
    assert "每个任务都必须显式包含 `depends_on`" in prompt_text
    assert '使用对象格式 `{{"$ref": "task_id"}}`' in prompt_text
    assert 'params": {{"table":' not in prompt_text
    assert "修改 save 任务的 table 参数" not in prompt_text


def test_auto_fix_prompt_describes_locator_resource_contract():
    source = (BACKEND_DIR / "chains" / "workflow_auto_fix.py").read_text(encoding="utf-8")

    assert "load 任务读取已有表、文件或对象时必须使用 `locator`" in source
    assert "save 任务创建新目标时必须使用 `target_parent_locator + target_name`" in source
    assert "不要在算子 params 中填写 `engine_id`" in source
    assert "不编造资源" in source
