"""Extract ontology-constrained entities from one text chunk."""
import json
from pathlib import Path
from typing import List

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.output_parsers import PydanticOutputParser
from langchain_core.prompts import PromptTemplate

from models.kg_models import EntityTypeInfo, ExtractedEntity, EntityExtractionOutput


class EntityExtractionChain:
    """
    实体抽取 Chain

    输入：单个 chunk 文本 + 本体实体类型列表（含属性/unique 标记）
    输出：抽取到的实体列表（含 temp_id、confidence、source_text）
    """

    def __init__(self, llm):
        self.llm = llm
        self.output_parser = PydanticOutputParser(pydantic_object=EntityExtractionOutput)
        self.prompt_template = self._load_prompt_template()

    def _load_prompt_template(self) -> PromptTemplate:
        template = (Path(__file__).parent.parent / "prompts" / "entity_extraction.txt").read_text(
            encoding="utf-8"
        )

        return PromptTemplate(
            template=template,
            input_variables=["text", "doc_context", "entity_types_json"],
            partial_variables={
                "format_instructions": self.output_parser.get_format_instructions()
            }
        )

    def _format_entity_types(self, entity_types: List[EntityTypeInfo]) -> str:
        """将实体类型定义格式化为 JSON 字符串传给 LLM"""
        types_data = []
        for et in entity_types:
            et_dict = {
                "name": et.name,
                "label": et.label,
                "description": et.description,
                "properties": []
            }
            for prop in et.properties:
                et_dict["properties"].append({
                    "name": prop.name,
                    "label": prop.label,
                    "data_type": prop.data_type,
                    "unique": prop.unique,
                    "required": prop.required,
                    "description": prop.description
                })
            types_data.append(et_dict)
        return json.dumps(types_data, ensure_ascii=False, indent=2)

    async def extract(
        self,
        text: str,
        entity_types: List[EntityTypeInfo],
        doc_context: str = ""
    ) -> List[ExtractedEntity]:
        """
        从文本 chunk 中抽取实体

        Args:
            text: 待抽取的文本 chunk
            entity_types: 本体实体类型列表
            doc_context: 文档头部上下文

        Returns:
            抽取到的实体列表
        """
        if not entity_types:
            return []

        entity_types_json = self._format_entity_types(entity_types)

        prompt = self.prompt_template.format(
            text=text,
            doc_context=doc_context or "（无）",
            entity_types_json=entity_types_json,
        )
        response = await self.llm.ainvoke([
            SystemMessage(content="你是 ADDP 知识图谱实体抽取器，只能按给定本体返回结构化结果。"),
            HumanMessage(content=prompt),
        ])
        return self.output_parser.parse(str(getattr(response, "content", response))).entities
