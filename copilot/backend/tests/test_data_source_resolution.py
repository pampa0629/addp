import asyncio
import importlib.util
from pathlib import Path


BACKEND_DIR = Path(__file__).resolve().parents[1]


def load_module(name: str, relative_path: str):
    spec = importlib.util.spec_from_file_location(name, BACKEND_DIR / relative_path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


data_source_stage_module = load_module("data_source_resolution_stage", "pipelines/data_source_stage.py")
workflow_pipeline_module = load_module("data_source_resolution_pipeline", "pipelines/workflow_pipeline.py")

DataSourceStage = data_source_stage_module.DataSourceStage
QueryAnalysis = data_source_stage_module.QueryAnalysis
WorkflowPipeline = workflow_pipeline_module.WorkflowPipeline
DataSourceContext = data_source_stage_module.DataSourceContext
DataSourceLocation = data_source_stage_module.DataSourceLocation
Workflow = workflow_pipeline_module.Workflow


class StaticEngineTool:
    async def _arun(self, tenant_id: int):
        return [{"id": 8, "name": "Business PostgreSQL", "type": "postgresql"}]


class EmptyMetadataSearchTool:
    async def _arun(self, **_kwargs):
        return []


class VerifiedMetadataSearchTool:
    async def _arun(self, **_kwargs):
        return [{
            "name": "roads",
            "type": "table",
            "engine_id": 8,
            "schema": "public",
            "score": 0,
            "locator": "addp://engine/8/path/public/roads?type=table&item_id=91",
            "target_parent_locator": "addp://engine/8/path/public?type=schema&node_id=7",
        }]


async def _analysis(_query: str):
    return QueryAnalysis(
        engine_keywords=[],
        engine_type_hint=None,
        table_name=None,
        schema_name="public",
        bucket_name=None,
        file_path=None,
        confidence=0.9,
    )


async def _assert_stage_does_not_construct_locator_without_metadata_fact():
    stage = DataSourceStage(
        llm=None,
        engine_tool=StaticEngineTool(),
        metadata_search_tool=EmptyMetadataSearchTool(),
    )
    stage._analyze_query = _analysis

    result = await stage.understand("计算铁路缓冲区")

    assert result.location.locator is None
    assert result.location.target_parent_locator is None
    assert result.alternatives == []


def test_stage_does_not_construct_locator_without_metadata_fact():
    asyncio.run(_assert_stage_does_not_construct_locator_without_metadata_fact())


async def _assert_stage_selects_single_verified_metadata_fact():
    stage = DataSourceStage(
        llm=None,
        engine_tool=StaticEngineTool(),
        metadata_search_tool=VerifiedMetadataSearchTool(),
    )
    stage._analyze_query = _analysis

    result = await stage.understand("roads")

    assert result.engine_id == 8
    assert result.location.locator == "addp://engine/8/path/public/roads?type=table&item_id=91"
    assert result.location.target_parent_locator == "addp://engine/8/path/public?type=schema&node_id=7"
    assert result.confidence == 1.0
    assert result.alternatives == []


def test_stage_selects_single_verified_metadata_fact():
    asyncio.run(_assert_stage_selects_single_verified_metadata_fact())


class UnresolvedDataSourceAgent:
    async def understand(self, _query: str, _tenant_id: int):
        return DataSourceContext(
            engine_id=0,
            engine_name="unknown",
            engine_type="unknown",
            location=DataSourceLocation(),
            confidence=0.0,
            alternatives=[],
        )


class MustNotRunSelection:
    async def select(self, *_args, **_kwargs):
        raise AssertionError("未验证数据源时不得进入算子选择")


async def _assert_pipeline_requires_verified_locator():
    pipeline = WorkflowPipeline.__new__(WorkflowPipeline)
    pipeline.enable_data_source_understanding = True
    pipeline.data_source_agent = UnresolvedDataSourceAgent()
    pipeline.operator_selection_chain = MustNotRunSelection()

    result = await pipeline.run("test", tenant_id=1, workflow_engine_id=20)

    assert result == {
        "status": "need_clarification",
        "clarification_reason": "data_source_not_found",
        "data_source_candidates": [],
    }


def test_pipeline_requires_verified_locator():
    asyncio.run(_assert_pipeline_requires_verified_locator())


class VerifiedDataSourceAgent:
    async def understand(self, _query: str, _tenant_id: int):
        return DataSourceContext(
            engine_id=8,
            engine_name="Business PostgreSQL",
            engine_type="postgresql",
            location=DataSourceLocation(
                locator="addp://engine/8/path/public/railway?type=table&item_id=60",
                target_parent_locator="addp://engine/8/path/public?type=schema&node_id=58",
            ),
            confidence=1.0,
            alternatives=[],
        )


class StaticSelection:
    async def select(self, *_args, **_kwargs):
        return ["load"]


class GenerationWithInventedLocator:
    async def generate(self, **_kwargs):
        return Workflow.model_validate({
            "tasks": [{
                "id": "task1",
                "operator": "load",
                "params": {
                    "locator": "addp://engine/8/path/public/farmland?type=table"
                },
                "depends_on": [],
            }]
        })


class MustNotRunValidation:
    async def validate(self, *_args, **_kwargs):
        raise AssertionError("未验证 locator 不得进入工作流验证")


async def _assert_pipeline_rejects_generated_unverified_locator():
    pipeline = WorkflowPipeline.__new__(WorkflowPipeline)
    pipeline.enable_data_source_understanding = True
    pipeline.data_source_agent = VerifiedDataSourceAgent()
    pipeline.operator_selection_chain = StaticSelection()
    pipeline.workflow_generation_chain = GenerationWithInventedLocator()
    pipeline.workflow_validation_chain = MustNotRunValidation()

    result = await pipeline.run("test", tenant_id=1, workflow_engine_id=20)

    assert result == {
        "status": "need_clarification",
        "clarification_reason": "data_source_unverified",
        "data_source_candidates": [],
    }


def test_pipeline_rejects_generated_unverified_locator():
    asyncio.run(_assert_pipeline_rejects_generated_unverified_locator())
