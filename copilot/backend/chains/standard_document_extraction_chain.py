"""从带绝对行号的 Markdown 章节中提炼数据标准候选。"""

import json

from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.output_parsers import PydanticOutputParser

from addp_common.client.inference import ResponseSchema

from models.standard_document_models import (
    StandardDocumentExtractRequest,
    StandardDocumentExtractResponse,
)


class StandardDocumentExtractionChain:
    def __init__(self, llm):
        if llm is None:
            raise ValueError("standard document extraction requires an Inference ChatModel")
        self.llm = llm
        self.output_parser = PydanticOutputParser(
            pydantic_object=StandardDocumentExtractResponse
        )

    async def extract(
        self, request: StandardDocumentExtractRequest
    ) -> StandardDocumentExtractResponse:
        sections = [section.model_dump() for section in request.sections]
        prompt = f"""
文档名称：{request.document_name}
文档版次：{request.version_label or '未标注'}

请从下列 Markdown 章节中提炼可供人工审核的数据标准候选。每行文本已带 `L绝对行号:` 前缀：
{json.dumps(sections, ensure_ascii=False, indent=2)}

规则：
1. 只提炼原文明确定义或可直接计算的业务术语、数据元、码值集、指标；不得补造事实。
2. candidate_type 只能是 glossary、element、code_set、metric。
3. code 使用稳定的小写 snake_case 英文标识。
4. payload 只保存该类型的结构化补充信息；无法从原文确认的字符串字段置 null，列表字段置空数组，不得猜测。
5. 每个候选至少给出一条证据。section_path 必须逐字使用输入值，start_line/end_line 必须是输入章节范围内的绝对行号。
6. 指标必须在 payload 中尽量表达 calculation_formula、statistical_scope、aggregation、dimension、unit；不明确时不要猜测。
7. 码值集必须在 payload.items 中给出原文明确列出的 code、name；不得自行补齐枚举。

{self.output_parser.get_format_instructions()}
""".strip()
        response = await self.llm.ainvoke(
            [
                SystemMessage(
                    content="你是 ADDP 数据标准候选提炼器，只返回可追溯的结构化候选，不发布标准。"
                ),
                HumanMessage(content=prompt),
            ],
            response_schema=self._response_schema(),
        )
        return self.output_parser.parse(str(getattr(response, "content", response)))

    @staticmethod
    def _response_schema() -> ResponseSchema:
        nullable_string = {"anyOf": [{"type": "string"}, {"type": "null"}]}
        payload = {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "data_type": nullable_string,
                "value_domain_kind": nullable_string,
                "unit": nullable_string,
                "calculation_formula": nullable_string,
                "statistical_scope": nullable_string,
                "aggregation": nullable_string,
                "dimensions": {"type": "array", "items": {"type": "string"}},
                "items": {
                    "type": "array",
                    "items": {
                        "type": "object",
                        "additionalProperties": False,
                        "properties": {
                            "code": {"type": "string"},
                            "name": {"type": "string"},
                            "definition": {"type": "string"},
                        },
                        "required": ["code", "name", "definition"],
                    },
                },
            },
            "required": [
                "data_type", "value_domain_kind", "unit", "calculation_formula",
                "statistical_scope", "aggregation", "dimensions", "items",
            ],
        }
        evidence = {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "section_path": {"type": "string", "minLength": 1},
                "start_line": {"type": "integer", "minimum": 1},
                "end_line": {"type": "integer", "minimum": 1},
            },
            "required": ["section_path", "start_line", "end_line"],
        }
        candidate = {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "candidate_type": {"type": "string", "enum": ["glossary", "element", "code_set", "metric"]},
                "code": {"type": "string", "pattern": "^[a-z][a-z0-9_]*$"},
                "name": {"type": "string", "minLength": 1},
                "definition": {"type": "string", "minLength": 1},
                "payload": payload,
                "evidences": {"type": "array", "minItems": 1, "maxItems": 20, "items": evidence},
            },
            "required": ["candidate_type", "code", "name", "definition", "payload", "evidences"],
        }
        return ResponseSchema(
            name="addp_standard_document_candidates",
            description="带绝对行号证据的数据标准候选。",
            schema={
                "type": "object",
                "additionalProperties": False,
                "properties": {"candidates": {"type": "array", "maxItems": 200, "items": candidate}},
                "required": ["candidates"],
            },
            strict=True,
        )
