"""
实体抽取 Chain
从单个文本 chunk 中抽取符合本体定义的实体
"""
import json
import time
import traceback
from typing import List, Optional

from langchain.chains import LLMChain
from langchain.output_parsers import PydanticOutputParser, OutputFixingParser
from langchain.prompts import PromptTemplate

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
        self.fixing_parser = OutputFixingParser.from_llm(
            parser=self.output_parser,
            llm=self.llm
        )
        self.prompt_template = self._load_prompt_template()
        self.chain = LLMChain(
            llm=self.llm,
            prompt=self.prompt_template,
            verbose=False
        )

    def _load_prompt_template(self) -> PromptTemplate:
        try:
            import os
            prompt_path = os.path.join(os.path.dirname(__file__), "..", "prompts", "entity_extraction.txt")
            with open(prompt_path, "r", encoding="utf-8") as f:
                template = f.read()
        except FileNotFoundError:
            template = self._get_builtin_template()

        return PromptTemplate(
            template=template,
            input_variables=["text", "doc_context", "entity_types_json"],
            partial_variables={
                "format_instructions": self.output_parser.get_format_instructions()
            }
        )

    def _get_builtin_template(self) -> str:
        return """从以下文本中抽取符合本体定义的实体。

## 文档上下文
{doc_context}

## 实体类型定义
{entity_types_json}

## 文本
{text}

{format_instructions}
"""

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

        try:
            result = await self.chain.ainvoke({
                "text": text,
                "doc_context": doc_context or "（无）",
                "entity_types_json": entity_types_json,
            })

            llm_output = result.get("text", "") if isinstance(result, dict) else str(result)

            try:
                output = await self.fixing_parser.aparse(llm_output)
            except Exception:
                output = self.output_parser.parse(llm_output)

            entities = output.entities
            print(f"[EntityExtractionChain] 抽取到 {len(entities)} 个实体")
            return entities

        except Exception as e:
            print(f"[EntityExtractionChain] ❌ 抽取失败: {type(e).__name__}: {e}")
            traceback.print_exc()
            raise
