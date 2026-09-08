"""从带绝对行号的 Markdown 章节中提炼数据标准候选。"""

import json
import re

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
        known_candidates = [
            candidate.model_dump() for candidate in request.known_candidates
        ]
        namespace_rule = (
            f"所有候选 code 必须以 `{request.code_namespace}_` 开头；"
            "枚举数据元的 code_set_code 也必须引用满足此前缀的同批码值集候选。"
            if request.code_namespace is not None
            else "本次没有领域编码前缀约束，仍须使用稳定的小写 snake_case 英文标识。"
        )
        prompt = f"""
文档名称：{request.document_name}
文档版次：{request.version_label or '未标注'}
候选编码命名空间：{request.code_namespace or '无领域前缀'}
同一文档中可复用的既有候选编码：
{json.dumps(known_candidates, ensure_ascii=False, indent=2)}

请从下列 Markdown 章节中提炼可供人工审核的数据标准候选。每行文本已带 `L绝对行号:` 前缀：
{json.dumps(sections, ensure_ascii=False, indent=2)}

规则：
1. 只提炼原文明确定义或可直接计算的业务术语、数据元、码值集、指标；不得补造事实。
2. candidate_type 只能是 glossary、element、code_set、metric。
3. code 使用稳定的小写 snake_case 英文标识。{namespace_rule}
4. 先按 candidate_type、名称和定义判断是否与既有候选表达同一业务概念；相同时必须逐字复用既有 code，不得另造近义编码。只有新概念才生成新 code。既有候选只是编码复用约束，不是正式标准，不得改变原文提炼范围。
5. payload 只保存该类型的结构化补充信息；无法从原文确认的字符串字段置 null，列表字段置空数组，不得猜测。
6. 每个候选至少给出一条证据。section_path 必须逐字使用输入值，start_line/end_line 必须是输入章节范围内的绝对行号。
7. 数据元的 data_type 只允许 string、int、bigint、float、decimal、date、datetime、bool、json、text；码值集的 data_type 只允许 string、int、bigint；业务术语和指标的 data_type 必须为 null。identifier 是业务语义，应写入名称或定义；原文只能确定 numeric、date_or_datetime 等上位类型时必须置 null，不得猜测具体类型；numeric、date_or_datetime 不是值域类型。
8. 数据元的 value_domain_kind 只允许 unrestricted、range、enumeration，原文没有明确范围或枚举约束时使用 unrestricted；非数据元候选必须为 null。枚举数据元的 code_set_code 必须引用同一响应中的码值集候选；非枚举数据元及其他候选的 code_set_code 必须为 null，不得生成数据库 ID。
9. 指标必须在 payload 中尽量表达 calculation_formula、statistical_scope、aggregation、dimensions、unit；不明确时不要猜测。
10. 码值集必须在 payload.items 中给出原文明确列出的 code、name；不得自行补齐枚举。

{self.output_parser.get_format_instructions()}
""".strip()
        response = await self.llm.ainvoke(
            [
                SystemMessage(
                    content="你是 ADDP 数据标准候选提炼器，只返回可追溯的结构化候选，不发布标准。"
                ),
                HumanMessage(content=prompt),
            ],
            response_schema=self._response_schema(request.code_namespace),
        )
        return self.output_parser.parse(str(getattr(response, "content", response)))

    @staticmethod
    def _response_schema(code_namespace: str | None = None) -> ResponseSchema:
        nullable_string = {"anyOf": [{"type": "string"}, {"type": "null"}]}
        nullable_element_data_type = {
            "anyOf": [
                {
                    "type": "string",
                    "enum": [
                        "string",
                        "int",
                        "bigint",
                        "float",
                        "decimal",
                        "date",
                        "datetime",
                        "bool",
                        "json",
                        "text",
                    ],
                },
                {"type": "null"},
            ]
        }
        nullable_code_set_data_type = {
            "anyOf": [
                {"type": "string", "enum": ["string", "int", "bigint"]},
                {"type": "null"},
            ]
        }
        nullable_value_domain_kind = {
            "anyOf": [
                {
                    "type": "string",
                    "enum": ["unrestricted", "range", "enumeration"],
                },
                {"type": "null"},
            ]
        }
        nullable_code_set_code = {
            "anyOf": [
                {
                    "type": "string",
                    "minLength": 1,
                    "maxLength": 100,
                    "pattern": "^[a-z][a-z0-9_]*$",
                },
                {"type": "null"},
            ]
        }
        null_only = {"type": "null"}

        def payload_schema(
            data_type_schema, value_domain_kind_schema, code_set_code_schema
        ):
            return {
                "type": "object",
                "additionalProperties": False,
                "properties": {
                    "data_type": data_type_schema,
                    "value_domain_kind": value_domain_kind_schema,
                    "code_set_code": code_set_code_schema,
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
                    "data_type",
                    "value_domain_kind",
                    "code_set_code",
                    "unit",
                    "calculation_formula",
                    "statistical_scope",
                    "aggregation",
                    "dimensions",
                    "items",
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

        def candidate_schema(
            candidate_type,
            data_type_schema,
            value_domain_kind_schema,
            code_set_code_schema,
        ):
            code_pattern = (
                rf"^{re.escape(code_namespace)}_[a-z0-9_]+$"
                if code_namespace is not None
                else "^[a-z][a-z0-9_]*$"
            )
            return {
                "type": "object",
                "additionalProperties": False,
                "properties": {
                    "candidate_type": {"type": "string", "enum": [candidate_type]},
                    "code": {"type": "string", "maxLength": 100, "pattern": code_pattern},
                    "name": {"type": "string", "minLength": 1},
                    "definition": {"type": "string", "minLength": 1},
                    "payload": payload_schema(
                        data_type_schema,
                        value_domain_kind_schema,
                        code_set_code_schema,
                    ),
                    "evidences": {
                        "type": "array",
                        "minItems": 1,
                        "maxItems": 20,
                        "items": evidence,
                    },
                },
                "required": [
                    "candidate_type",
                    "code",
                    "name",
                    "definition",
                    "payload",
                    "evidences",
                ],
            }

        candidate = {
            "anyOf": [
                candidate_schema("glossary", null_only, null_only, null_only),
                candidate_schema(
                    "element",
                    nullable_element_data_type,
                    nullable_value_domain_kind,
                    nullable_code_set_code,
                ),
                candidate_schema(
                    "code_set", nullable_code_set_data_type, null_only, null_only
                ),
                candidate_schema("metric", null_only, null_only, null_only),
            ]
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
