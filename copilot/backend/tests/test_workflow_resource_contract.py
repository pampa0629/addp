import importlib.util
import asyncio
from pathlib import Path

import pytest
from pydantic import ValidationError

from models.workflow_models import Workflow, WorkflowResourceFact, Task

BACKEND_DIR = Path(__file__).resolve().parents[1]


def load_module(name: str, relative_path: str):
    spec = importlib.util.spec_from_file_location(name, BACKEND_DIR / relative_path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


workflow_validation = load_module("workflow_validation_chain_module", "chains/workflow_validation_chain.py")

WorkflowValidationChain = workflow_validation.WorkflowValidationChain


class FakeOperatorDetailTool:
    def __init__(self):
        self.seen_workflow_engine_ids = []

    async def _arun(self, operator_name: str, workflow_engine_id: int, tenant_id: int = 0):
        self.seen_workflow_engine_ids.append(workflow_engine_id)
        definitions = {
            "load": {
                "execution_modes": ["workflow"],
                "parameters": [
                    {"name": "connection_info", "required": False},
                    {"name": "schema", "required": False},
                    {"name": "table", "required": False},
                    {"name": "path", "required": False},
                ],
                "public_parameters": [
                    {"name": "数据源", "type": "ui", "param_type": "ui"},
                    {"name": "locator", "type": "string", "param_type": "resource"},
                ],
                "output_ports": [{"name": "default"}],
            },
            "save": {
                "execution_modes": ["workflow"],
                "parameters": [
                    {"name": "input_df", "required": True},
                    {"name": "connection_info", "required": False},
                    {"name": "schema", "required": False},
                    {"name": "table", "required": False},
                    {"name": "path", "required": False},
                    {"name": "mode", "required": False},
                ],
                "public_parameters": [
                    {"name": "input_df", "required": True},
                    {"name": "mode", "required": False},
                    {"name": "保存目标", "type": "ui", "param_type": "ui"},
                    {"name": "target_parent_locator", "type": "string", "param_type": "resource"},
                    {"name": "target_name", "type": "string", "param_type": "resource"},
                ],
                "output_ports": [{"name": "default"}],
            },
            "direct_only": {
                "execution_modes": ["direct"],
                "parameters": [],
                "public_parameters": [],
                "output_ports": [{"name": "default"}],
            },
        }
        return definitions.get(operator_name)


def test_workflow_resource_fact_preserves_multiple_verified_inputs():
    resources = [
        WorkflowResourceFact(
            role="railway",
            locator="addp://engine/8/path/public/railway?type=table&item_id=60",
            geometry_column="geom",
            crs=None,
        ),
        WorkflowResourceFact(
            role="farmland",
            locator="addp://engine/8/path/public/farmland?type=table&item_id=55",
            geometry_column="geometry",
            crs="EPSG:32650",
        ),
    ]

    assert [resource.role for resource in resources] == ["railway", "farmland"]
    assert resources[1].crs == "EPSG:32650"


def test_pipeline_stops_before_generation_when_spatial_resource_has_no_crs():
    from pipelines.workflow_pipeline import WorkflowPipeline

    class MustNotSelect:
        async def select(self, *_args, **_kwargs):
            raise AssertionError("CRS 缺失时不得进入算子选择")

    pipeline = WorkflowPipeline.__new__(WorkflowPipeline)
    pipeline.operator_selection_chain = MustNotSelect()
    result = asyncio.run(pipeline.run(
        "计算铁路缓冲区",
        tenant_id=1,
        workflow_engine_id=20,
        resources=[WorkflowResourceFact(
            role="railway",
            locator="addp://engine/8/path/public/railway?type=table&item_id=60",
            geometry_column="geom",
            crs=None,
        )],
    ))

    assert result["status"] == "need_clarification"
    assert result["clarification_reason"] == "resource_crs_required"


def test_task_requires_explicit_depends_on():
    with pytest.raises(ValidationError) as exc_info:
        Task(
            id="task1",
            operator="load",
            params={
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
                "locator": "addp://engine/12/path/public/roads?type=table",
            },
            depends_on=[],
        ),
        Task(
            id="task2",
            operator="save",
            params={
                "input_df": {"$ref": "task1"},
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


def test_validation_rejects_obsolete_resource_type_parameters():
    asyncio.run(_assert_validation_rejects_obsolete_resource_type_parameters())


async def _assert_validation_rejects_obsolete_resource_type_parameters():
    tool = FakeOperatorDetailTool()
    validator = WorkflowValidationChain(tool)
    workflow = Workflow(tasks=[
        Task(
            id="task1",
            operator="load",
            params={
                "source_type": "table",
                "locator": "addp://engine/3/path/data/roads.csv?type=file",
            },
            depends_on=[],
        )
    ])

    result = await validator.validate(workflow, workflow_engine_id=9)

    assert not result.is_valid
    assert any("未定义的参数" in error and "source_type" in error for error in result.errors)
    assert tool.seen_workflow_engine_ids == [9]


def test_validation_requires_workflow_engine_id():
    asyncio.run(_assert_validation_requires_workflow_engine_id())


async def _assert_validation_requires_workflow_engine_id():
    validator = WorkflowValidationChain(FakeOperatorDetailTool())
    workflow = Workflow(tasks=[
        Task(
            id="task1",
            operator="load",
            params={
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
    assert "source_type" not in prompt_text
    assert "target_type" not in prompt_text
    assert 'params": {{"table":' not in prompt_text
    assert "修改 save 任务的 table 参数" not in prompt_text


def test_auto_fix_prompt_describes_locator_resource_contract():
    source = (BACKEND_DIR / "chains" / "workflow_auto_fix.py").read_text(encoding="utf-8")

    assert "load 任务读取已有表、文件或对象时必须使用 `locator`" in source
    assert "save 任务创建新目标时必须使用 `target_parent_locator + target_name`" in source
    assert "不要在算子 params 中填写 `engine_id`" in source
    assert "source_type" not in source
    assert "target_type" not in source
    assert "不编造资源" in source
