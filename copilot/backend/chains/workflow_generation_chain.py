"""Generate a workflow definition from verified resources and Public Operator Specs."""

from __future__ import annotations

import asyncio
from pathlib import Path
from typing import Any

from addp_common.resources import ResourceFact
from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.output_parsers import PydanticOutputParser
from langchain_core.prompts import PromptTemplate

from models.workflow_models import Workflow
from services.operator_catalog import OperatorCatalogService
from utils.operator_contract import public_workflow_parameters


class WorkflowGenerationChain:
    """Perform one strict LLM transformation; parsing has no text fallback route."""

    def __init__(self, llm: Any, operator_catalog: OperatorCatalogService) -> None:
        self.llm = llm
        self.operator_catalog = operator_catalog
        self.output_parser = PydanticOutputParser(pydantic_object=Workflow)
        prompt_content = (
            Path(__file__).parent.parent / "prompts" / "workflow_generation.txt"
        ).read_text(encoding="utf-8")
        self.prompt = PromptTemplate(
            template=prompt_content,
            input_variables=["query", "data_source_info", "operators_detail"],
            partial_variables={"format_instructions": self.output_parser.get_format_instructions()},
        )

    async def generate(
        self,
        query: str,
        resources: list[ResourceFact],
        selected_operators: list[str],
        workflow_engine_id: int | None = None,
        tenant_id: int = 0,
    ) -> Workflow:
        if not workflow_engine_id:
            raise ValueError("workflow_engine_id 是算子详情获取必需上下文")
        operator_details = await self._fetch_operator_details(
            selected_operators,
            workflow_engine_id,
            tenant_id,
        )
        if not operator_details:
            raise ValueError("无法获取算子详情，无法生成工作流")
        rendered = self.prompt.format(
            query=query,
            data_source_info=self._format_resource_info(resources),
            operators_detail=self._format_operator_details(operator_details),
        )
        response = await self.llm.ainvoke([
            SystemMessage(content="你是 ADDP 工作流定义生成器，只能使用输入中的资源事实和公开算子契约。"),
            HumanMessage(content=rendered),
        ])
        try:
            return self.output_parser.parse(str(getattr(response, "content", response)))
        except Exception as error:
            raise ValueError("工作流生成结果不符合结构化契约") from error

    async def _fetch_operator_details(
        self,
        operator_names: list[str],
        workflow_engine_id: int,
        tenant_id: int,
    ) -> list[dict[str, Any]]:
        results = await asyncio.gather(*(
            self.operator_catalog.get_operator(name, workflow_engine_id, tenant_id)
            for name in operator_names
        ))
        details: list[dict[str, Any]] = []
        for name, result in zip(operator_names, results):
            if result is None:
                raise ValueError(f"工作流引擎 {workflow_engine_id} 不存在算子 {name}")
            details.append(result)
        return details

    def _format_operator_details(self, operator_details: list[dict[str, Any]]) -> str:
        lines: list[str] = []
        for index, operator in enumerate(operator_details, 1):
            lines.append(f"### {index}. {operator['name']} - {operator.get('brief_description', '')}")
            lines.append(f"描述: {operator.get('description', '')}")
            lines.append(f"分类: {operator.get('category', '未分类')}")
            detailed = operator.get("detailed_description")
            if isinstance(detailed, dict) and detailed.get("notes"):
                lines.append("注意事项: " + "；".join(str(note) for note in detailed["notes"]))
            parameters = public_workflow_parameters(operator)
            if parameters:
                lines.append("参数:")
            for parameter in parameters:
                required = "必需" if parameter.get("required") else "可选"
                line = f"- `{parameter['name']}` ({parameter.get('type', '未知')}, {required})"
                if parameter.get("description"):
                    line += f": {parameter['description']}"
                if parameter.get("enum"):
                    line += "；可选值=" + ", ".join(str(value) for value in parameter["enum"])
                lines.append(line)
            if operator.get("output_ports"):
                lines.append("输出: " + ", ".join(
                    str(output.get("name")) for output in operator["output_ports"]
                    if isinstance(output, dict) and output.get("name")
                ))
            lines.append("---")
        return "\n".join(lines)

    @staticmethod
    def _format_resource_info(resources: list[ResourceFact]) -> str:
        return "\n".join(
            str(resource.model_dump(exclude_none=True))
            for resource in resources
        )
