"""
关系抽取 Chain
基于已识别实体，从单个文本 chunk 中抽取符合本体定义的关系
"""
import json
import traceback
from typing import List

from langchain.chains import LLMChain
from langchain.output_parsers import PydanticOutputParser, OutputFixingParser
from langchain.prompts import PromptTemplate

from models.kg_models import RelationTypeInfo, ExtractedEntity, ExtractedRelation, RelationExtractionOutput


class RelationExtractionChain:
    """
    关系抽取 Chain

    输入：单个 chunk 文本 + 本体关系类型列表 + 当前 chunk 已识别实体
    输出：抽取到的关系列表（通过 temp_id 引用实体）
    """

    def __init__(self, llm):
        self.llm = llm
        self.output_parser = PydanticOutputParser(pydantic_object=RelationExtractionOutput)
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
            prompt_path = os.path.join(os.path.dirname(__file__), "..", "prompts", "relation_extraction.txt")
            with open(prompt_path, "r", encoding="utf-8") as f:
                template = f.read()
        except FileNotFoundError:
            template = self._get_builtin_template()

        return PromptTemplate(
            template=template,
            input_variables=["text", "doc_context", "relation_types_json", "entities_json"],
            partial_variables={
                "format_instructions": self.output_parser.get_format_instructions()
            }
        )

    def _get_builtin_template(self) -> str:
        return """基于已识别实体，从以下文本中抽取符合本体定义的关系。

## 文档上下文
{doc_context}

## 关系类型定义
{relation_types_json}

## 已识别实体
{entities_json}

## 文本
{text}

{format_instructions}
"""

    def _format_relation_types(self, relation_types: List[RelationTypeInfo]) -> str:
        types_data = []
        for rt in relation_types:
            rt_dict = {
                "name": rt.name,
                "label": rt.label,
                "source_type": rt.source_type,
                "target_type": rt.target_type,
                "description": rt.description,
                "properties": [
                    {"name": p.name, "label": p.label, "data_type": p.data_type}
                    for p in rt.properties
                ]
            }
            types_data.append(rt_dict)
        return json.dumps(types_data, ensure_ascii=False, indent=2)

    def _format_entities(self, entities: List[ExtractedEntity]) -> str:
        """格式化已识别实体，供关系抽取使用"""
        entities_data = []
        for e in entities:
            entities_data.append({
                "temp_id": e.temp_id,
                "type": e.type,
                "properties": e.properties
            })
        return json.dumps(entities_data, ensure_ascii=False, indent=2)

    async def extract(
        self,
        text: str,
        relation_types: List[RelationTypeInfo],
        entities: List[ExtractedEntity],
        doc_context: str = ""
    ) -> List[ExtractedRelation]:
        """
        从文本 chunk 中抽取关系

        Args:
            text: 待抽取的文本 chunk
            relation_types: 本体关系类型列表
            entities: 当前 chunk 已识别的实体
            doc_context: 文档头部上下文

        Returns:
            抽取到的关系列表
        """
        if not relation_types or not entities:
            return []

        relation_types_json = self._format_relation_types(relation_types)
        entities_json = self._format_entities(entities)

        try:
            result = await self.chain.ainvoke({
                "text": text,
                "doc_context": doc_context or "（无）",
                "relation_types_json": relation_types_json,
                "entities_json": entities_json,
            })

            llm_output = result.get("text", "") if isinstance(result, dict) else str(result)

            try:
                output = self.fixing_parser.parse(llm_output)
            except Exception:
                output = self.output_parser.parse(llm_output)

            # 过滤引用了不存在 temp_id 的关系
            valid_temp_ids = {e.temp_id for e in entities}
            valid_relations = []
            for rel in output.relations:
                if rel.source_temp_id in valid_temp_ids and rel.target_temp_id in valid_temp_ids:
                    valid_relations.append(rel)
                else:
                    print(f"[RelationExtractionChain] ⚠️ 跳过无效关系（temp_id 不存在）: {rel.source_temp_id} -> {rel.target_temp_id}")

            print(f"[RelationExtractionChain] 抽取到 {len(valid_relations)} 条有效关系")
            return valid_relations

        except Exception as e:
            print(f"[RelationExtractionChain] ❌ 抽取失败: {type(e).__name__}: {e}")
            traceback.print_exc()
            return []
