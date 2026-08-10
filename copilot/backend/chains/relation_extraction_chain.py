"""Extract ontology-constrained relations between known chunk entities."""
import json
from pathlib import Path
from typing import List

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.output_parsers import PydanticOutputParser
from langchain_core.prompts import PromptTemplate

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
        self.prompt_template = self._load_prompt_template()

    def _load_prompt_template(self) -> PromptTemplate:
        template = (Path(__file__).parent.parent / "prompts" / "relation_extraction.txt").read_text(
            encoding="utf-8"
        )

        return PromptTemplate(
            template=template,
            input_variables=["text", "doc_context", "relation_types_json", "entities_json"],
            partial_variables={
                "format_instructions": self.output_parser.get_format_instructions()
            }
        )

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

        prompt = self.prompt_template.format(
            text=text,
            doc_context=doc_context or "（无）",
            relation_types_json=relation_types_json,
            entities_json=entities_json,
        )
        response = await self.llm.ainvoke([
            SystemMessage(content="你是 ADDP 知识图谱关系抽取器，只能引用输入中已存在的实体。"),
            HumanMessage(content=prompt),
        ])
        output = self.output_parser.parse(str(getattr(response, "content", response)))
        valid_temp_ids = {entity.temp_id for entity in entities}
        return [
            relation for relation in output.relations
            if relation.source_temp_id in valid_temp_ids
            and relation.target_temp_id in valid_temp_ids
        ]
