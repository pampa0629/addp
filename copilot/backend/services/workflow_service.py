"""Workflow generation application service."""

from __future__ import annotations

from typing import Any

from addp_common.resources import ResourceFact

from chains.operator_selection_chain import OperatorSelectionChain
from chains.workflow_auto_fix import WorkflowAutoFixer
from chains.workflow_generation_chain import WorkflowGenerationChain
from chains.workflow_validation_chain import WorkflowValidationChain
from models.workflow_models import Workflow
from services.operator_catalog import OperatorCatalogService


class WorkflowService:
    """Coordinate operator selection, generation, validation and one repair route."""

    def __init__(self, llm: Any) -> None:
        operator_catalog = OperatorCatalogService()
        self.operator_selection_chain = OperatorSelectionChain(llm, operator_catalog)
        self.workflow_generation_chain = WorkflowGenerationChain(llm, operator_catalog)
        self.workflow_validation_chain = WorkflowValidationChain(operator_catalog)
        self.workflow_auto_fixer = WorkflowAutoFixer(
            llm=llm,
            validator=self.workflow_validation_chain,
            max_retries=2,
        )

    async def run(
        self,
        query: str,
        tenant_id: int,
        workflow_engine_id: int | None,
        resources: list[ResourceFact] | None = None,
    ) -> dict[str, Any]:
        if not workflow_engine_id:
            raise ValueError("workflow_engine_id 是 Copilot 工作流生成必需上下文")
        resources = resources or []
        if not resources:
            return {
                "status": "need_clarification",
                "clarification_reason": "resource_facts_required",
                "message": "请先确认工作流输入资源",
            }
        missing_crs_roles = [
            resource.role for resource in resources
            if resource.geometry_column and not resource.crs
        ]
        if missing_crs_roles:
            return {
                "status": "need_clarification",
                "clarification_reason": "resource_crs_required",
                "message": "以下空间资源缺少 CRS：" + "、".join(missing_crs_roles),
            }

        selected_operators = await self.operator_selection_chain.select(
            query,
            self._format_resources(resources),
            workflow_engine_id=workflow_engine_id,
            tenant_id=tenant_id,
        )
        workflow = await self.workflow_generation_chain.generate(
            query=query,
            resources=resources,
            selected_operators=selected_operators,
            workflow_engine_id=workflow_engine_id,
            tenant_id=tenant_id,
        )
        if not self._workflow_resource_facts_are_verified(workflow, resources):
            return {
                "status": "need_clarification",
                "clarification_reason": "resource_facts_unverified",
                "message": "候选工作流引用了 resources 之外的 locator",
            }

        validation = await self.workflow_validation_chain.validate(
            workflow,
            workflow_engine_id=workflow_engine_id,
            tenant_id=tenant_id,
        )
        if not validation.is_valid:
            workflow, validation = await self.workflow_auto_fixer.auto_fix(
                workflow,
                validation,
                workflow_engine_id=workflow_engine_id,
                tenant_id=tenant_id,
            )
        resource_values = [resource.model_dump(exclude_none=True) for resource in resources]
        if not validation.is_valid:
            return {
                "status": "validation_failed",
                "workflow": workflow.model_dump(),
                "errors": validation.errors,
                "warnings": validation.warnings,
                "suggestions": validation.suggestions,
                "message": "工作流生成但未通过验证",
                "resources": resource_values,
                "selected_operators": selected_operators,
                "validation_result": validation.model_dump(),
            }
        return {
            "status": "success",
            "workflow": workflow.model_dump(),
            "explanation": self._generate_explanation(workflow, resources),
            "resources": resource_values,
            "selected_operators": selected_operators,
            "validation_result": validation.model_dump(),
        }

    @staticmethod
    def _format_resources(resources: list[ResourceFact]) -> str:
        return "\n".join(
            f"- role={resource.role}; locator={resource.locator}; "
            f"geometry_column={resource.geometry_column or '无'}; crs={resource.crs or '未知'}"
            for resource in resources
        )

    @staticmethod
    def _workflow_resource_facts_are_verified(
        workflow: Workflow,
        resources: list[ResourceFact],
    ) -> bool:
        source_locators = {resource.locator for resource in resources}
        return all(
            task.operator != "load" or task.params.get("locator") in source_locators
            for task in workflow.tasks
        )

    @staticmethod
    def _generate_explanation(workflow: Workflow, resources: list[ResourceFact]) -> str:
        steps = [f"{index}. {task.operator}" for index, task in enumerate(workflow.tasks, 1)]
        return "\n".join([
            f"已生成包含 {len(workflow.tasks)} 个步骤的工作流：",
            *steps,
            "\n输入资源：" + "、".join(resource.role for resource in resources),
        ])


def create_workflow_service(llm: Any) -> WorkflowService:
    return WorkflowService(llm)
