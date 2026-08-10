import asyncio
import importlib.util
from pathlib import Path

import pytest
from fastapi import HTTPException
from addp_common.resources import ResourceFact


BACKEND_DIR = Path(__file__).resolve().parents[1]


def load_module(name: str, relative_path: str):
    spec = importlib.util.spec_from_file_location(name, BACKEND_DIR / relative_path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


workflow_generation = load_module("workflow_generation_contract_module", "chains/workflow_generation_chain.py")
operator_selection = load_module("operator_selection_contract_module", "chains/operator_selection_chain.py")
workflow_api = load_module("workflow_agent_api_contract_module", "api/workflow_agent_api.py")

WorkflowGenerationChain = workflow_generation.WorkflowGenerationChain
OperatorSelectionChain = operator_selection.OperatorSelectionChain
workflow_api.WorkflowGenerationRequest.model_rebuild(_types_namespace={"ResourceFact": ResourceFact})


def live_load_descriptor():
    return {
        "name": "load",
        "brief_description": "从数据库表或文件资源加载数据",
        "category": "数据I/O",
        "detailed_description": {
            "notes": ["connection_info、schema/table 或 path 由 Develop Adapter 注入"],
            "workflow_example": {
                "id": "load_roads",
                "operator": "load",
                "params": {"connection_info": {}, "schema": "public", "table": "roads"},
                "depends_on": [],
            },
        },
        "parameters": [
            {"name": "connection_info", "type": "object"},
            {"name": "schema", "type": "string"},
            {"name": "table", "type": "string"},
            {"name": "path", "type": "string"},
        ],
        "public_parameters": [
            {"name": "数据源", "type": "ui", "param_type": "ui"},
            {"name": "locator", "type": "string", "param_type": "resource"},
        ],
        "output_ports": [{"name": "default", "type": "geodataframe"}],
    }


def test_generation_uses_public_parameters_and_ignores_runtime_example():
    chain = WorkflowGenerationChain.__new__(WorkflowGenerationChain)

    formatted = chain._format_operator_details([live_load_descriptor()])

    assert "`locator`" in formatted
    assert "`connection_info`" not in formatted
    assert "`schema`" not in formatted
    assert "`table`" not in formatted
    assert "`数据源`" not in formatted
    assert "load_roads" not in formatted


def test_generation_rejects_missing_public_operator_contract():
    chain = WorkflowGenerationChain.__new__(WorkflowGenerationChain)
    descriptor = live_load_descriptor()
    del descriptor["public_parameters"]

    with pytest.raises(ValueError, match="public_parameters"):
        chain._format_operator_details([descriptor])


class RaisingLLMChain:
    async def ainvoke(self, _payload):
        raise RuntimeError("upstream insufficient_balance")


class StaticOperatorCatalog:
    seen_tenant_id = None

    async def list_operators(self, workflow_engine_id: int, tenant_id: int = 0):
        self.seen_tenant_id = tenant_id
        return [{"name": "load", "brief": "load", "category": "I/O"}]


async def _assert_operator_selection_propagates_llm_error():
    catalog = StaticOperatorCatalog()
    chain = OperatorSelectionChain(RaisingLLMChain(), catalog)

    with pytest.raises(RuntimeError, match="insufficient_balance"):
        await chain.select("load roads", workflow_engine_id=20, tenant_id=3)
    assert catalog.seen_tenant_id == 3


def test_operator_selection_propagates_llm_error():
    asyncio.run(_assert_operator_selection_propagates_llm_error())


class FailingService:
    async def run(self, **_kwargs):
        raise RuntimeError("upstream insufficient_balance")


async def _assert_workflow_api_returns_stable_error(monkeypatch):
    from addp_common.auth import AuthorizationContext

    monkeypatch.setattr(workflow_api, "get_workflow_service", lambda *_args: FailingService())
    request = workflow_api.WorkflowGenerationRequest(
        query="load roads",
        workflow_engine_id=20,
        resources=[{
            "role": "roads",
            "locator": "addp://engine/8/path/public/roads?type=table&item_id=91",
        }],
    )

    with pytest.raises(HTTPException) as exc_info:
        await workflow_api.generate_workflow(
            request,
            AuthorizationContext(principal_id=7, tenant_id=3, tenant_membership_id=9),
            workflow_api.HTTPAuthorizationCredentials(scheme="Bearer", credentials="addp_dat_test"),
            None,
        )

    assert exc_info.value.status_code == 500
    assert exc_info.value.detail == "工作流生成失败"


def test_workflow_api_returns_stable_error(monkeypatch):
    asyncio.run(_assert_workflow_api_returns_stable_error(monkeypatch))


def test_workflow_service_exposes_validation_failure_without_pipeline_compatibility():
    source = (BACKEND_DIR / "services" / "workflow_service.py").read_text(encoding="utf-8")
    assert '"status": "validation_failed"' in source
    assert not any((BACKEND_DIR / "pipelines").glob("*.py"))
