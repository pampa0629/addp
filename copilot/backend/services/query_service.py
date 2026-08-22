"""查询语言生成服务：先规划，再基于已验证资源事实生成和校验候选查询。"""

from __future__ import annotations

import json
import logging
import math
import re
from typing import Any

import sqlglot
from sqlglot import exp

from langchain_core.messages import HumanMessage, SystemMessage
from sqlalchemy.orm import Session

from addp_common.client.inference import ResponseSchema
from services.inference_service import CopilotInferenceService
from services.mql_compiler import MQLCompiler, MQLPlanError

logger = logging.getLogger(__name__)


class QueryService:
    """根据当前 Query Engine 生成候选查询文本，不执行查询。"""

    _allowed_plan_operations = {
        "filter",
        "list",
        "project",
        "sort",
        "limit",
        "group",
        "aggregate",
        "join",
        "union",
        "intersection",
        "ratio",
        "count",
        "distinct",
    }
    _field_dependent_plan_operations = {
        "filter",
        "list",
        "project",
        "sort",
        "group",
        "join",
        "union",
        "intersection",
        "ratio",
        "distinct",
    }
    _query_parameter_types = {"string", "integer", "number", "boolean"}

    _plan_prompt = (
        "你是 ADDP 查询规划器。把用户需求完整拆成一个可校验的 Query Plan，不生成查询语言。"
        "每个用户子问题都必须映射到 operations 和 result_keys；collections 只能逐字复制 resources[].query_name，"
        "field_paths 只能逐字复制 resources[].fields[].name 或 path；只要 operations 包含字段相关操作，"
        "field_paths 就不能为空。"
        "operations 只能使用 filter、list、project、sort、limit、group、aggregate、join、union、"
        "intersection、ratio、count、distinct。五个字段的每一项都只能是 JSON 字符串，禁止返回对象或键值结构。"
        "用户要求重叠度但未指定定义时，默认采用 Jaccard intersection/union，并写入 assumptions。"
        "例如 operations 必须写成 [\"filter\",\"intersection\",\"ratio\"]。只返回 JSON 对象："
        '{"collections":[],"field_paths":[],"operations":[],"result_keys":[],"assumptions":[]}。'
    )

    _system_prompt = (
        "你是 ADDP 查询工作台的查询生成助手。严格按照 Query Plan、当前查询引擎、真实 capability、"
        "查询语言和已验证资源事实生成一个只读候选查询。不得遗漏 Query Plan 中的操作和结果；"
        "不得编造表、集合、字段、locator、连接信息、几何列或 CRS；不得把资源 locator 写进查询；"
        "不得假定 PostgreSQL、public、geom、geometry 或任何固定空间函数。"
    )

    _language_instructions = {
        "sql": (
            "SQL 只返回一条只读 SQL 语句；使用已验证资源的原生表/模式名称和字段，"
            "参数使用 :name，不要输出连接信息、代码围栏或解释性前后缀。"
        ),
        "mql": (
            "MQL 的 query 必须是单个 JSON command object。数据库由执行目标选择，"
            "find/aggregate/count/distinct 只填写已验证 collection 名称。复杂筛选、多个结果集、交集或比例"
            "必须使用 aggregate pipeline；多个结果集优先使用 $facet，比例使用显式分子、分母和除零保护。"
            "如果提供 current_query，默认保留其主 collection。禁止 Mongo Shell、JavaScript、BSON 构造器、"
            "函数调用、$out、$merge、locator 和 connection_info；参数只能使用 {\"$param\":\"name\"}。"
            "只能使用已验证的字面字段路径，嵌套数组字段直接使用已验证 dotted path 筛选；"
            "禁止用 $objectToArray 或其他记录键枚举绕过字段事实。"
            "用 $facet 生成多个活动/对象集合时，每个分支必须先投影同一个标量结果，"
            "facet 后再用 $map 取出标量数组并计算 $setIntersection/$setUnion，不得直接对文档数组计算重叠度。"
        ),
        "cypher": (
            "Cypher 只返回一条只读 Cypher 语句；参数使用 $name，不要输出连接信息、代码围栏或解释性前后缀。"
        ),
    }

    async def generate(
        self,
        *,
        query: str,
        engine: dict[str, Any],
        query_language: str,
        resources: list[dict[str, Any]],
        current_query: str | None,
        tenant_id: int,
        db: Session,
    ) -> dict[str, Any]:
        language = query_language.strip().lower()
        query_name_key = "federated_sql" if language == "sql" and self._is_federated_query_engine(engine) else language
        llm = CopilotInferenceService.chat_model(
            db,
            tenant_id=tenant_id,
            scenario_code="query_generation",
            temperature=0.1,
            max_output_tokens=5000,
        )
        context_payload = {
            "engine": {
                "id": engine.get("id"),
                "name": engine.get("name"),
                "engine_type": engine.get("engine_type"),
                "capabilities": engine.get("capabilities"),
            },
            "query_language": language,
            "resources": self._resources_for_context(resources, query_name_key),
        }
        if current_query and current_query.strip():
            context_payload["current_query"] = current_query.strip()
        context = json.dumps(context_payload, ensure_ascii=False, default=str)
        allowed_parameter_types = self._engine_query_parameter_types(engine, language)

        if language == "mql":
            return await self._generate_compiled_mql(llm, context, query, resources)

        plan_response = await llm.ainvoke([
            SystemMessage(content=self._plan_prompt),
            HumanMessage(content=f"当前查询上下文:\n{context}\n\n用户需求:\n{query}"),
        ], response_schema=self._plan_response_schema())
        plan_output = str(getattr(plan_response, "content", plan_response))
        plan_resources = self._resources_with_active_query_name(resources, language, query_name_key)
        plan, plan_errors = self._parse_and_validate_plan(plan_output, plan_resources)
        if plan_errors:
            logger.warning(
                "query plan rejected: errors=%s verified_fields=%s plan_output=%s",
                plan_errors,
                sorted(self._resource_field_names(resources)),
                plan_output,
            )
            repair_response = await llm.ainvoke([
                SystemMessage(content=self._plan_prompt),
                HumanMessage(content=(
                    "上一个 Query Plan 未通过确定性校验。只能修正列出的错误；"
                    "不得新增、改写或推断资源事实，只返回完整 Query Plan JSON。\n"
                    f"Plan 校验错误:\n{json.dumps(plan_errors, ensure_ascii=False)}\n\n"
                    f"当前查询上下文:\n{context}\n\n"
                    f"上一个 Query Plan:\n{plan_output}\n\n用户需求:\n{query}"
                )),
            ], response_schema=self._plan_response_schema())
            plan_output = str(getattr(repair_response, "content", repair_response))
            plan, plan_errors = self._parse_and_validate_plan(plan_output, plan_resources)
            if plan_errors:
                logger.warning(
                    "query plan repair rejected: errors=%s verified_fields=%s plan_output=%s",
                    plan_errors,
                    sorted(self._resource_field_names(resources)),
                    plan_output,
                )
                raise ValueError("query plan validation failed: " + "; ".join(plan_errors))
        if plan is None:
            raise ValueError("query plan validation failed")

        candidate_output = await self._generate_candidate(llm, context, query, language, plan)
        candidate, errors = self._parse_and_validate_candidate(
            candidate_output,
            language,
            plan_resources,
            plan,
            allowed_parameter_types=allowed_parameter_types,
        )
        if errors:
            candidate_output = await self._repair_candidate(
                llm,
                context=context,
                query=query,
                language=language,
                plan=plan,
                candidate=candidate_output,
                errors=errors,
            )
            candidate, errors = self._parse_and_validate_candidate(
                candidate_output,
                language,
                plan_resources,
                plan,
                allowed_parameter_types=allowed_parameter_types,
            )
            if errors:
                raise ValueError("generated query validation failed: " + "; ".join(errors))
        if candidate is None:
            raise ValueError("generated query validation failed")
        candidate["plan"] = plan
        return candidate

    async def _generate_compiled_mql(
        self,
        llm,
        context: str,
        query: str,
        resources: list[dict[str, Any]],
    ) -> dict[str, Any]:
        prompt = self._mql_semantic_plan_prompt()
        response = await llm.ainvoke([
            SystemMessage(content=prompt),
            HumanMessage(content=f"当前查询上下文:\n{context}\n\n用户需求:\n{query}"),
        ], response_schema=self._mql_semantic_plan_response_schema())
        output = str(getattr(response, "content", response))
        try:
            plan = MQLCompiler.parse_plan(output, self._decode_object)
            return MQLCompiler.compile(plan, resources)
        except MQLPlanError as error:
            logger.warning("MQL semantic plan rejected: error=%s output=%s", error, output)
            repair = await llm.ainvoke([
                SystemMessage(content=prompt),
                HumanMessage(content=(
                    "上一个 MQL 语义计划未通过确定性校验。只能依据校验错误和资源事实修正；"
                    "不能改写用户值、扩展同义词或直接生成 MQL。若无法无假设修正，填写 clarification。\n"
                    f"校验错误:\n{error}\n\n当前查询上下文:\n{context}\n\n"
                    f"上一个语义计划:\n{output}\n\n用户需求:\n{query}"
                )),
            ], response_schema=self._mql_semantic_plan_response_schema())
            repaired_output = str(getattr(repair, "content", repair))
            plan = MQLCompiler.parse_plan(repaired_output, self._decode_object)
            return MQLCompiler.compile(plan, resources)

    @staticmethod
    def _mql_semantic_plan_prompt() -> str:
        return (
            "你是 ADDP MongoDB 查询语义规划器。你只负责把用户需求映射为强类型语义计划，绝不生成 MQL。"
            "collection 只能逐字复制 resources[].query_name；field 只能逐字复制 resources[].fields[].name。"
            "必须依据字段 path、type、element_type、comment 和用户句法识别实体、条件与统计对象。"
            "人名或昵称条件应优先映射到明确的人名/昵称字段，不能把人名改成活动关键词。"
            "用户值必须原样保留，禁止翻译、增加英文、同义词、正则备选或其他扩展。"
            "用户未明确要求包含、模糊或正则匹配时使用 eq；contains/regex 只能用于 string 字段。"
            "统计某个记录的数组成员数量时使用 count_array_elements，统计匹配文档数时使用 count_documents。"
            "比较两个实体各自拥有的去重元素集合时使用 set_comparison；entity_field 是实体标识字段，"
            "entity_values 必须原样保留两个实体值，set_fields 是一个或多个数组元素身份字段。"
            "set_comparison.metric 只能是 intersection_count、jaccard、overlap_coefficient 或 unspecified。"
            "用户只说‘重叠度’、‘重合度’或同等含义但未明确计算口径时必须选择 unspecified，禁止自行默认 Jaccard；"
            "只有用户明确要求共同元素数量、Jaccard 或以较少一方为基准的重叠系数时才能选择对应指标。"
            "无法从资源事实唯一确定含义时必须填写 clarification；不得把猜测写成可执行计划。"
            "assumptions 必须为空，否则系统会要求用户澄清。参数名和结果列名由编译器生成，不要输出。"
            "metric.operation 只能是 none、count_documents、count_array_elements、distinct_count、sum、avg、min、max。"
            "filter.operator 只能是 eq、ne、gt、gte、lt、lte、in、contains、regex、exists。"
            "统计某记录拥有的数组元素数量时，filters 必须标识拥有该数组的记录，禁止选择该数组的子字段。"
            "普通查询的 set_comparison 必须为 null；集合比较时 filters/select_fields/sort 必须为空、limit 必须为 null、"
            "metric.operation 必须为 none。只返回 JSON：collection、filters、metric、set_comparison、assumptions、clarification；"
            "filters 每项只有 field/operator/value，metric 只有 operation/field。"
        )

    @staticmethod
    def _mql_semantic_plan_response_schema() -> ResponseSchema:
        scalar = {
            "anyOf": [
                {"type": "string"}, {"type": "integer"}, {"type": "number"},
                {"type": "boolean"},
                {"type": "array", "items": {"anyOf": [
                    {"type": "string"}, {"type": "integer"}, {"type": "number"}, {"type": "boolean"},
                ]}},
            ],
        }
        filter_item = {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "field": {"type": "string"},
                "operator": {"type": "string", "enum": sorted(MQLCompiler.FILTER_OPERATORS)},
                "value": scalar,
            },
            "required": ["field", "operator", "value"],
        }
        sort_item = {
            "type": "object",
            "additionalProperties": False,
            "properties": {"field": {"type": "string"}, "direction": {"type": "integer", "enum": [-1, 1]},},
            "required": ["field", "direction"],
        }
        metric = {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "operation": {"type": "string", "enum": sorted(MQLCompiler.METRIC_OPERATIONS)},
                "field": {"type": "string"},
            },
            "required": ["operation", "field"],
        }
        set_comparison = {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "entity_field": {"type": "string"},
                "entity_values": {
                    "type": "array",
                    "minItems": 2,
                    "maxItems": 2,
                    "items": {"type": "string"},
                },
                "set_fields": {
                    "type": "array",
                    "minItems": 1,
                    "items": {"type": "string"},
                },
                "metric": {"type": "string", "enum": sorted(MQLCompiler.SET_COMPARISON_METRICS)},
            },
            "required": ["entity_field", "entity_values", "set_fields", "metric"],
        }
        return ResponseSchema(
            name="addp_mql_semantic_plan",
            description="ADDP 强类型 MongoDB 查询语义计划。",
            schema={
                "type": "object",
                "additionalProperties": False,
                "properties": {
                    "collection": {"type": "string"},
                    "filters": {"type": "array", "items": filter_item},
                    "metric": metric,
                    "set_comparison": {"anyOf": [set_comparison, {"type": "null"}]},
                    "assumptions": {"type": "array", "items": {"type": "string"}},
                    "clarification": {"anyOf": [{"type": "string"}, {"type": "null"}]},
                },
                "required": ["collection", "filters", "metric", "set_comparison", "assumptions", "clarification"],
            },
            strict=True,
        )

    async def _generate_candidate(self, llm, context: str, query: str, language: str, plan: dict[str, Any]):
        response = await llm.ainvoke([
            SystemMessage(content=self._generation_system_prompt(language)),
            HumanMessage(content=(
                f"当前查询上下文:\n{context}\n\nQuery Plan:\n"
                f"{json.dumps(plan, ensure_ascii=False)}\n\n用户需求:\n{query}"
            )),
        ], response_schema=self._candidate_response_schema(language))
        return str(getattr(response, "content", response))

    async def _repair_candidate(
        self,
        llm,
        *,
        context: str,
        query: str,
        language: str,
        plan: dict[str, Any],
        candidate: str,
        errors: list[str],
    ) -> dict[str, Any]:
        response = await llm.ainvoke([
            SystemMessage(content=self._generation_system_prompt(language)),
            HumanMessage(content=(
                "上一个候选未通过确定性校验。只能修正列出的错误，仍须完整遵守 Query Plan 和资源事实。\n"
                f"校验错误:\n{json.dumps(errors, ensure_ascii=False)}\n\n"
                f"当前查询上下文:\n{context}\n\nQuery Plan:\n{json.dumps(plan, ensure_ascii=False)}\n\n"
                f"上一个候选:\n{candidate}\n\n用户需求:\n{query}"
            )),
        ], response_schema=self._candidate_response_schema(language))
        return str(getattr(response, "content", response))

    @staticmethod
    def _plan_response_schema() -> ResponseSchema:
        string_array = {"type": "array", "items": {"type": "string"}}
        return ResponseSchema(
            name="addp_query_plan",
            description="ADDP 查询计划。",
            schema={
                "type": "object",
                "additionalProperties": False,
                "properties": {
                    "collections": string_array,
                    "field_paths": string_array,
                    "operations": string_array,
                    "result_keys": string_array,
                    "assumptions": string_array,
                },
                "required": ["collections", "field_paths", "operations", "result_keys", "assumptions"],
            },
            strict=True,
        )

    @staticmethod
    def _candidate_response_schema(language: str) -> ResponseSchema:
        query_description = (
            "只读 MQL command 的 JSON 字符串；不要使用代码围栏。"
            if language == "mql" else "只读查询语句字符串；不要使用代码围栏。"
        )
        parameter = {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "name": {"type": "string"},
                "type": {"type": "string", "enum": ["string", "integer", "number", "boolean"]},
                "default": {
                    "anyOf": [
                        {"type": "string"},
                        {"type": "integer"},
                        {"type": "number"},
                        {"type": "boolean"},
                    ],
                },
                "title": {"type": ["string", "null"]},
                "description": {"type": ["string", "null"]},
            },
            "required": ["name", "type", "default", "title", "description"],
        }
        return ResponseSchema(
            name="addp_query_candidate",
            description=query_description,
            schema={
                "type": "object",
                "additionalProperties": False,
                "properties": {
                    "query": {"type": "string", "description": query_description},
                    "query_parameters": {"type": "array", "items": parameter},
                    "explanation": {"type": "string"},
                    "warnings": {"type": "array", "items": {"type": "string"}},
                },
                "required": ["query", "query_parameters", "explanation", "warnings"],
            },
            strict=True,
        )

    def _generation_system_prompt(self, language: str) -> str:
        return (
            self._system_prompt
            + "\n"
            + self._language_instructions.get(language, "严格遵守当前引擎 capability 声明的查询语言语法。")
            + '\n只返回 JSON 对象：{"query":<查询字符串>,"query_parameters":[{"name":"...","type":"string|integer|number|boolean","default":<默认值>}],"explanation":"...","warnings":[]}；MQL 的 query 必须是已序列化的 JSON command 字符串；query_parameters 必须与查询中的参数引用完全一致，没有引用时为 []。'
        )

    @classmethod
    def _parse_plan(cls, output: str) -> dict[str, Any]:
        parsed = cls._decode_object(output)
        keys = ("collections", "field_paths", "operations", "result_keys", "assumptions")
        plan: dict[str, Any] = {}
        for key in keys:
            value = parsed.get(key)
            if not isinstance(value, list):
                raise ValueError(f"query plan {key} must be an array")
            if any(not isinstance(item, str) for item in value):
                raise ValueError(f"query plan {key} items must be strings")
            plan[key] = [item.strip() for item in value if item.strip()]
        return plan

    @classmethod
    def _parse_and_validate_plan(
        cls,
        output: str,
        resources: list[dict[str, Any]],
    ) -> tuple[dict[str, Any] | None, list[str]]:
        try:
            plan = cls._parse_plan(output)
        except ValueError as error:
            return None, [str(error)]
        return plan, cls._validate_plan(plan, resources)

    @classmethod
    def _resources_for_context(cls, resources: list[dict[str, Any]], query_name_key: str) -> list[dict[str, Any]]:
        contextual: list[dict[str, Any]] = []
        for resource in resources:
            item = dict(resource)
            query_names = resource.get("query_names")
            if isinstance(query_names, dict) and isinstance(query_names.get(query_name_key), str):
                item["query_name"] = query_names[query_name_key]
            contextual.append(item)
        return contextual

    @staticmethod
    def _resources_with_active_query_name(resources: list[dict[str, Any]], language: str, query_name_key: str) -> list[dict[str, Any]]:
        active: list[dict[str, Any]] = []
        for resource in resources:
            item = dict(resource)
            names = dict(resource.get("query_names") or {})
            if query_name_key in names:
                names[language] = names[query_name_key]
            item["query_names"] = names
            active.append(item)
        return active

    @staticmethod
    def _is_federated_query_engine(engine: dict[str, Any]) -> bool:
        capabilities = engine.get("capabilities")
        if isinstance(capabilities, str):
            try:
                capabilities = json.loads(capabilities)
            except json.JSONDecodeError:
                return False
        federation = (((capabilities or {}).get("compute") or {}).get("query") or {}).get("federation") or {}
        return federation.get("supported") is True

    @classmethod
    def _parse_and_validate_candidate(
        cls,
        output: str,
        language: str,
        resources: list[dict[str, Any]],
        plan: dict[str, Any],
        *,
        allowed_parameter_types: set[str] | None = None,
    ) -> tuple[dict[str, Any] | None, list[str]]:
        try:
            candidate = cls._parse_output(output, query_language=language)
        except ValueError as error:
            return None, [str(error)]
        return candidate, cls._validate_candidate(
            candidate,
            language,
            resources,
            plan,
            allowed_parameter_types=allowed_parameter_types,
        )

    @classmethod
    def _parse_output(cls, output: str, *, query_language: str | None = None) -> dict[str, Any]:
        parsed = cls._decode_object(output)
        query_value = parsed.get("query")
        language = (query_language or "").strip().lower()
        if language == "mql" and isinstance(query_value, dict):
            query_text = json.dumps(query_value, ensure_ascii=False, separators=(",", ":"))
        elif isinstance(query_value, str) and query_value.strip():
            query_text = query_value.strip()
        else:
            raise ValueError("query generation response must contain a non-empty query")
        if "addp://" in query_text or re.search(r"\b(connection_info|engine_id)\s*[:=]", query_text, re.IGNORECASE):
            raise ValueError("generated query contains internal resource facts")
        if language == "mql":
            command = cls._parse_mql(query_text)
            if not isinstance(command, dict):
                raise ValueError("generated MQL must be a JSON command object")
        query_parameters = cls._parse_query_parameters(parsed.get("query_parameters", []))
        warnings = parsed.get("warnings")
        if not isinstance(warnings, list):
            warnings = []
        return {
            "query": query_text,
            "explanation": str(parsed.get("explanation") or "").strip(),
            "warnings": [str(item).strip() for item in warnings if str(item).strip()],
            "query_parameters": query_parameters,
        }

    @classmethod
    def _parse_query_parameters(cls, value: Any) -> list[dict[str, Any]]:
        if not isinstance(value, list):
            raise ValueError("query_parameters must be an array")
        parameters: list[dict[str, Any]] = []
        names: set[str] = set()
        for index, item in enumerate(value):
            if not isinstance(item, dict):
                raise ValueError(f"query_parameters[{index}] must be an object")
            unknown = set(item) - {"name", "type", "default", "title", "description"}
            if unknown:
                raise ValueError(f"query_parameters[{index}] contains unknown fields: {', '.join(sorted(unknown))}")
            name = str(item.get("name") or "").strip()
            if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", name):
                raise ValueError(f"query_parameters[{index}].name is invalid")
            if name in names:
                raise ValueError(f"query_parameters contains duplicate name: {name}")
            parameter_type = str(item.get("type") or "").strip().lower()
            if parameter_type not in cls._query_parameter_types:
                raise ValueError(f"query_parameters[{index}].type is unsupported: {parameter_type}")
            if "default" not in item:
                raise ValueError(f"query_parameters[{index}].default is required")
            default = item["default"]
            valid_default = (
                parameter_type == "string" and isinstance(default, str) and bool(default.strip())
            ) or (
                parameter_type == "integer" and isinstance(default, int) and not isinstance(default, bool)
            ) or (
                parameter_type == "number" and isinstance(default, (int, float))
                and not isinstance(default, bool) and math.isfinite(default)
            ) or (
                parameter_type == "boolean" and isinstance(default, bool)
            )
            if not valid_default:
                raise ValueError(f"query_parameters[{index}].default does not match type {parameter_type}")
            definition = {"name": name, "type": parameter_type, "default": default}
            for key in ("title", "description"):
                text = str(item.get(key) or "").strip()
                if text:
                    definition[key] = text
            parameters.append(definition)
            names.add(name)
        return parameters

    @classmethod
    def _engine_query_parameter_types(cls, engine: dict[str, Any], language: str) -> set[str]:
        capabilities = engine.get("capabilities")
        if isinstance(capabilities, str):
            try:
                capabilities = json.loads(capabilities)
            except json.JSONDecodeError:
                return set()
        if not isinstance(capabilities, dict):
            return set()
        query = (capabilities.get("compute") or {}).get("query") or {}
        parameters = query.get("parameters") or {}
        languages = {
            str(item).strip().lower()
            for item in parameters.get("languages") or []
            if str(item).strip()
        }
        if not parameters.get("supported") or language not in languages:
            return set()
        return {
            str(item).strip().lower()
            for item in parameters.get("types") or []
            if str(item).strip().lower() in cls._query_parameter_types
        }

    @staticmethod
    def _decode_object(output: str) -> dict[str, Any]:
        cleaned = output.strip()
        match = re.search(r"```(?:json)?\s*(.*?)\s*```", cleaned, re.DOTALL | re.IGNORECASE)
        if match:
            cleaned = match.group(1).strip()
        try:
            parsed = json.loads(cleaned)
        except json.JSONDecodeError as original_error:
            decoder = json.JSONDecoder()
            candidates: list[dict[str, Any]] = []
            for index, character in enumerate(cleaned):
                if character != "{":
                    continue
                try:
                    candidate, end = decoder.raw_decode(cleaned, index)
                except json.JSONDecodeError:
                    continue
                prefix = cleaned[:index]
                suffix = cleaned[end:]
                if (
                    isinstance(candidate, dict)
                    and "{" not in prefix
                    and "}" not in prefix
                    and "{" not in suffix
                    and "}" not in suffix
                ):
                    candidates.append(candidate)
            if len(candidates) != 1:
                raise ValueError("query generation response must contain exactly one JSON object") from original_error
            parsed = candidates[0]
        if not isinstance(parsed, dict):
            raise ValueError("query generation response must be an object")
        return parsed

    @staticmethod
    def _validate_plan(plan: dict[str, Any], resources: list[dict[str, Any]]) -> list[str]:
        errors: list[str] = []
        allowed_collections = QueryService._resource_collection_names(resources)
        allowed_fields = QueryService._resource_field_names(resources)
        for collection in plan["collections"]:
            if collection not in allowed_collections:
                errors.append(f"plan collection is not verified: {collection}")
        for field in plan["field_paths"]:
            if not QueryService._field_is_allowed(field, allowed_fields):
                errors.append(f"plan field is not verified: {field}")
        for operation in plan["operations"]:
            if operation not in QueryService._allowed_plan_operations:
                errors.append(f"plan operation is not supported: {operation}")
        if not plan["collections"]:
            errors.append("plan collections must not be empty")
        if not plan["operations"]:
            errors.append("plan operations must not be empty")
        if (
            not plan["field_paths"]
            and any(operation in QueryService._field_dependent_plan_operations for operation in plan["operations"])
        ):
            errors.append("plan field_paths must not be empty for field-dependent operations")
        return errors

    @classmethod
    def _validate_candidate(
        cls,
        candidate: dict[str, Any],
        language: str,
        resources: list[dict[str, Any]],
        plan: dict[str, Any],
        *,
        allowed_parameter_types: set[str] | None = None,
    ) -> list[str]:
        errors = cls._validate_query_parameters(
            candidate,
            language,
            allowed_parameter_types=allowed_parameter_types,
        )
        if language not in {"sql", "mql", "cypher"}:
            errors.append(f"query language is unsupported: {language}")
            return errors
        if language == "sql":
            return errors + cls._validate_sql(candidate["query"], resources, plan)
        if language == "cypher":
            return errors + cls._validate_cypher(candidate["query"], resources, plan)
        if language != "mql":
            return errors
        command = cls._parse_mql(candidate["query"])
        command_names = [name for name in ("find", "aggregate", "count", "distinct") if name in command]
        if len(command_names) != 1 or not isinstance(command.get(command_names[0] if command_names else ""), str):
            errors.append("MQL must declare exactly one supported read-only command")
            return errors
        if cls._contains_key(command, {"$out", "$merge"}):
            errors.append("MQL contains a write aggregation stage")
        if cls._contains_dynamic_mql_field_name(command):
            errors.append("MQL contains a dynamic field name")
        if cls._contains_key(command, {"$objectToArray", "$arrayToObject"}):
            errors.append("MQL contains dynamic record key enumeration")
        if cls._contains_non_positive_mql_limit(command):
            errors.append("MQL contains a non-positive limit")

        verified_collections = cls._resource_query_names(resources, "mql")
        referenced_collections = cls._mql_collection_references(command)
        for collection in referenced_collections:
            if collection not in verified_collections:
                errors.append(f"MQL collection is not verified: {collection}")
        for collection in plan["collections"]:
            if collection not in referenced_collections:
                errors.append(f"MQL does not cover planned collection: {collection}")

        verified_fields = cls._resource_field_names(resources)
        referenced_fields, unverified_fields = cls._mql_field_usage(command, verified_fields)
        for field in sorted(unverified_fields):
            errors.append(f"MQL field is not verified: {field}")

        query_text = candidate["query"]
        for field in plan["field_paths"]:
            if not cls._field_is_allowed(field, referenced_fields):
                errors.append(f"MQL does not cover planned field: {field}")
        for result_key in plan["result_keys"]:
            if result_key not in query_text:
                errors.append(f"MQL does not expose planned result: {result_key}")

        required_tokens = {
            "filter": ("\"filter\"", "\"$match\""),
            "list": ("\"projection\"", "\"$project\"", "\"$facet\""),
            "project": ("\"projection\"", "\"$project\""),
            "sort": ("\"sort\"", "\"$sort\""),
            "limit": ("\"limit\"", "\"$limit\""),
            "group": ("\"$group\"",),
            "aggregate": ("\"aggregate\"",),
            "join": ("\"$lookup\"", "\"$graphLookup\""),
            "union": ("\"$unionWith\"", "\"$setUnion\""),
            "intersection": ("\"$and\"", "\"$setIntersection\"", "\"$all\""),
            "ratio": ("\"$divide\"",),
            "count": ("\"count\"", "\"$count\"", "\"$sum\""),
            "distinct": ("\"distinct\"", "\"$group\"", "\"$setUnion\""),
        }
        for operation in plan["operations"]:
            tokens = required_tokens.get(operation)
            if tokens and not any(token in query_text for token in tokens):
                errors.append(f"MQL does not implement planned operation: {operation}")
        return errors

    @classmethod
    def _validate_sql(cls, query: str, resources: list[dict[str, Any]], plan: dict[str, Any]) -> list[str]:
        try:
            statements = sqlglot.parse(query)
        except Exception as error:
            return [f"SQL syntax is invalid: {error}"]
        if len(statements) != 1:
            return ["SQL must contain exactly one statement"]
        statement = statements[0]
        errors: list[str] = []
        if any(statement.find(node) for node in (exp.Insert, exp.Update, exp.Delete, exp.Create, exp.Drop, exp.Alter, exp.Merge, exp.Command)):
            errors.append("SQL must be a read-only query")
        names = cls._resource_query_names(resources, "sql")
        cte_names = {cte.alias_or_name for cte in statement.find_all(exp.CTE) if cte.alias_or_name}
        tables = {
            cls._sql_table_name(table)
            for table in statement.find_all(exp.Table)
            if table.name not in cte_names
        }
        if not names:
            errors.append("SQL resource query_names.sql is not verified")
        errors.extend(f"SQL table is not verified: {table}" for table in tables - names)
        allowed_fields = cls._resource_field_names(resources)
        if not allowed_fields:
            errors.append("SQL fields are not verified")
        derived_fields = {alias.alias for alias in statement.find_all(exp.Alias) if alias.alias}
        for column in statement.find_all(exp.Column):
            if column.name != "*" and column.name not in derived_fields and not cls._field_is_allowed(column.name, allowed_fields):
                errors.append(f"SQL field is not verified: {column.name}")
        query_upper = query.upper()
        for operation in plan["operations"]:
            token = {"filter": "WHERE", "sort": "ORDER BY", "limit": "LIMIT", "join": "JOIN", "group": "GROUP BY", "aggregate": "SELECT", "count": "COUNT", "distinct": "DISTINCT"}.get(operation)
            if token and token not in query_upper:
                errors.append(f"SQL does not implement planned operation: {operation}")
        for collection in plan["collections"]:
            if collection not in tables:
                errors.append(f"SQL does not cover planned collection: {collection}")
        for field in plan["field_paths"]:
            if not cls._field_is_allowed(field, allowed_fields) or field not in query:
                errors.append(f"SQL does not cover planned field: {field}")
        for result_key in plan["result_keys"]:
            if result_key not in query:
                errors.append(f"SQL does not expose planned result: {result_key}")
        return sorted(set(errors))

    @staticmethod
    def _sql_table_name(table: exp.Table) -> str:
        return ".".join(part for part in (table.catalog, table.db, table.name) if part)

    @classmethod
    def _validate_cypher(cls, query: str, resources: list[dict[str, Any]], plan: dict[str, Any]) -> list[str]:
        errors: list[str] = []
        if re.search(r"\b(CREATE|MERGE|DELETE|DETACH|SET|REMOVE|DROP|FOREACH|LOAD\s+CSV)\b", query, re.I):
            errors.append("Cypher contains a write operation")
        if not cls._resource_query_names(resources, "cypher"):
            errors.append("Cypher resource query_names.cypher is not verified")
        allowed_fields = cls._resource_field_names(resources)
        for field in re.findall(r"\b[a-zA-Z_][a-zA-Z0-9_]*\.([a-zA-Z_][a-zA-Z0-9_]*)", query):
            if not cls._field_is_allowed(field, allowed_fields):
                errors.append(f"Cypher field is not verified: {field}")
        for field in plan["field_paths"]:
            if not cls._field_is_allowed(field, allowed_fields) or field not in query:
                errors.append(f"Cypher does not cover planned field: {field}")
        for result_key in plan["result_keys"]:
            if result_key not in query:
                errors.append(f"Cypher does not expose planned result: {result_key}")
        return sorted(set(errors))

    @classmethod
    def _validate_query_parameters(
        cls,
        candidate: dict[str, Any],
        language: str,
        *,
        allowed_parameter_types: set[str] | None,
    ) -> list[str]:
        references = cls._query_parameter_references(language, candidate["query"])
        definitions = candidate.get("query_parameters") or []
        by_name = {str(item.get("name") or ""): item for item in definitions if isinstance(item, dict)}
        errors: list[str] = []
        prefix = language.upper() if language else "query"
        for name in sorted(references - set(by_name)):
            errors.append(f"{prefix} query parameter is undefined: {name}")
        for name in sorted(set(by_name) - references):
            errors.append(f"{prefix} query parameter is unused: {name}")
        supported_types = cls._query_parameter_types if allowed_parameter_types is None else allowed_parameter_types
        for name, definition in by_name.items():
            parameter_type = str(definition.get("type") or "")
            if parameter_type not in supported_types:
                errors.append(f"{prefix} query parameter type is not supported: {name}={parameter_type}")
        return errors

    @classmethod
    def _query_parameter_references(cls, language: str, query_text: str) -> set[str]:
        if language == "mql":
            references: set[str] = set()

            def visit(value: Any) -> None:
                if isinstance(value, list):
                    for child in value:
                        visit(child)
                    return
                if not isinstance(value, dict):
                    return
                if "$param" in value:
                    name = value.get("$param")
                    if isinstance(name, str) and re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", name.strip()):
                        references.add(name.strip())
                    return
                for child in value.values():
                    visit(child)

            visit(cls._parse_mql(query_text))
            return references
        if language == "sql":
            return cls._text_query_parameter_references(query_text, ":")
        if language == "cypher":
            return cls._text_query_parameter_references(query_text, "$", slash_line_comments=True)
        return set()

    @staticmethod
    def _text_query_parameter_references(
        query_text: str,
        prefix: str,
        *,
        slash_line_comments: bool = False,
    ) -> set[str]:
        references: set[str] = set()
        index = 0
        while index < len(query_text):
            current = query_text[index]
            if current in {"'", '"', "`"}:
                quote = current
                index += 1
                while index < len(query_text):
                    if query_text[index] == "\\":
                        index += 2
                        continue
                    if query_text[index] == quote:
                        if index + 1 < len(query_text) and query_text[index + 1] == quote:
                            index += 2
                            continue
                        index += 1
                        break
                    index += 1
                continue
            if query_text.startswith("--", index) or (
                slash_line_comments and query_text.startswith("//", index)
            ):
                newline = query_text.find("\n", index + 2)
                index = len(query_text) if newline < 0 else newline + 1
                continue
            if query_text.startswith("/*", index):
                comment_end = query_text.find("*/", index + 2)
                index = len(query_text) if comment_end < 0 else comment_end + 2
                continue
            if (
                current == prefix
                and (index == 0 or query_text[index - 1] != prefix)
                and index + 1 < len(query_text)
                and re.match(r"[A-Za-z_]", query_text[index + 1])
            ):
                end = index + 2
                while end < len(query_text) and re.match(r"[A-Za-z0-9_]", query_text[end]):
                    end += 1
                references.add(query_text[index + 1:end])
                index = end
                continue
            index += 1
        return references

    @staticmethod
    def _parse_mql(query_text: str) -> dict[str, Any]:
        try:
            parsed = json.loads(query_text)
        except json.JSONDecodeError as error:
            raise ValueError("generated MQL must be a JSON command object") from error
        if not isinstance(parsed, dict):
            raise ValueError("generated MQL must be a JSON command object")
        return parsed

    @staticmethod
    def _resource_collection_names(resources: list[dict[str, Any]]) -> set[str]:
        names: set[str] = set()
        for resource in resources:
            query_names = resource.get("query_names")
            if isinstance(query_names, dict):
                names.update(str(value).strip() for value in query_names.values() if str(value).strip())
        return names

    @staticmethod
    def _resource_query_names(resources: list[dict[str, Any]], language: str) -> set[str]:
        names: set[str] = set()
        for resource in resources:
            query_names = resource.get("query_names")
            if isinstance(query_names, dict):
                value = query_names.get(language)
                if isinstance(value, str) and value.strip():
                    names.add(value.strip())
        return names

    @staticmethod
    def _resource_field_names(resources: list[dict[str, Any]]) -> set[str]:
        names: set[str] = set()
        for resource in resources:
            for field in resource.get("fields") or []:
                if not isinstance(field, dict):
                    continue
                name = str(field.get("name") or "").strip()
                if name:
                    names.add(name)
                path = field.get("path")
                if isinstance(path, list):
                    rendered = ".".join(str(item).strip() for item in path if str(item).strip())
                    if rendered:
                        names.add(rendered)
        return names

    @staticmethod
    def _field_is_allowed(field: str, allowed_fields: set[str]) -> bool:
        return field in allowed_fields or any(value.startswith(field + ".") for value in allowed_fields)

    @classmethod
    def _mql_field_usage(
        cls,
        command: dict[str, Any],
        source_fields: set[str],
    ) -> tuple[set[str], set[str]]:
        references: set[str] = set()
        unknown: set[str] = set()

        def is_known(field: str, derived_fields: set[str]) -> bool:
            return (
                cls._field_is_allowed(field, source_fields)
                or field in derived_fields
                or any(field.startswith(alias + ".") for alias in derived_fields)
            )

        def add_reference(value: Any, derived_fields: set[str]) -> None:
            field = str(value or "").strip()
            if field.startswith("$$"):
                return
            if field.startswith("$"):
                field = field[1:]
            if not field:
                return
            if cls._field_is_allowed(field, source_fields):
                references.add(field)
            elif not is_known(field, derived_fields):
                unknown.add(field)

        def visit_expression(value: Any, derived_fields: set[str]) -> None:
            if isinstance(value, list):
                for child in value:
                    visit_expression(child, derived_fields)
                return
            if isinstance(value, str):
                if value.startswith("$"):
                    add_reference(value, derived_fields)
                return
            if isinstance(value, dict):
                for operator, child in value.items():
                    if operator == "$getField":
                        field = child.get("field") if isinstance(child, dict) else child
                        if isinstance(field, str):
                            add_reference(field, derived_fields)
                        if isinstance(child, dict):
                            visit_expression(child.get("input"), derived_fields)
                        continue
                    visit_expression(child, derived_fields)

        def visit_query_document(
            value: Any,
            derived_fields: set[str],
            prefix: tuple[str, ...] = (),
        ) -> None:
            if isinstance(value, list):
                for child in value:
                    visit_query_document(child, derived_fields, prefix)
                return
            if not isinstance(value, dict):
                visit_expression(value, derived_fields)
                return
            for key, child in value.items():
                if key.startswith("$"):
                    if key in {"$and", "$or", "$nor", "$not", "$elemMatch"}:
                        visit_query_document(child, derived_fields, prefix)
                    else:
                        visit_expression(child, derived_fields)
                    continue
                segments = tuple(part for part in key.split(".") if part)
                field_path = ".".join(prefix + segments)
                add_reference(field_path, derived_fields)
                if isinstance(child, dict):
                    visit_query_document(child, derived_fields, prefix + segments)
                else:
                    visit_expression(child, derived_fields)

        def visit_projection(value: Any, derived_fields: set[str], *, computed_aliases: bool) -> None:
            if not isinstance(value, dict):
                return
            for key, child in value.items():
                if child in (0, 1, False, True):
                    if key != "_id" or child in (1, True):
                        add_reference(key, derived_fields)
                    continue
                visit_expression(child, derived_fields)
                if computed_aliases and key != "_id":
                    derived_fields.add(key)

        def visit_pipeline(pipeline: Any, inherited_fields: set[str]) -> set[str]:
            derived_fields = set(inherited_fields)
            if not isinstance(pipeline, list):
                return derived_fields
            for stage in pipeline:
                if not isinstance(stage, dict):
                    continue
                for operator, value in stage.items():
                    if operator == "$match":
                        visit_query_document(value, derived_fields)
                    elif operator == "$sort":
                        visit_query_document(value, derived_fields)
                    elif operator == "$project":
                        visit_projection(value, derived_fields, computed_aliases=True)
                    elif operator in {"$set", "$addFields"} and isinstance(value, dict):
                        for alias, expression in value.items():
                            visit_expression(expression, derived_fields)
                            derived_fields.add(alias)
                    elif operator == "$group" and isinstance(value, dict):
                        for expression in value.values():
                            visit_expression(expression, derived_fields)
                        derived_fields.update(str(alias) for alias in value)
                    elif operator == "$facet" and isinstance(value, dict):
                        for facet_name, facet_pipeline in value.items():
                            visit_pipeline(facet_pipeline, derived_fields)
                            derived_fields.add(str(facet_name))
                    elif operator in {"$lookup", "$graphLookup"} and isinstance(value, dict):
                        if operator == "$lookup":
                            add_reference(value.get("localField"), derived_fields)
                            add_reference(value.get("foreignField"), derived_fields)
                            if isinstance(value.get("let"), dict):
                                visit_expression(value["let"], derived_fields)
                            visit_pipeline(value.get("pipeline"), set())
                        else:
                            visit_expression(value.get("startWith"), derived_fields)
                            add_reference(value.get("connectFromField"), derived_fields)
                            add_reference(value.get("connectToField"), derived_fields)
                            visit_query_document(value.get("restrictSearchWithMatch"), derived_fields)
                        alias = str(value.get("as") or "").strip()
                        if alias:
                            derived_fields.add(alias)
                    elif operator == "$unionWith" and isinstance(value, dict):
                        visit_pipeline(value.get("pipeline"), set())
                    elif operator == "$count" and isinstance(value, str) and value.strip():
                        derived_fields.add(value.strip())
                    elif operator == "$unset":
                        fields = value if isinstance(value, list) else [value]
                        for field in fields:
                            add_reference(field, derived_fields)
                    else:
                        visit_expression(value, derived_fields)
            return derived_fields

        derived_fields: set[str] = set()
        if isinstance(command.get("find"), str):
            visit_query_document(command.get("filter"), derived_fields)
            visit_query_document(command.get("sort"), derived_fields)
            visit_projection(command.get("projection"), derived_fields, computed_aliases=False)
        elif isinstance(command.get("count"), str):
            visit_query_document(command.get("query"), derived_fields)
        elif isinstance(command.get("distinct"), str):
            add_reference(command.get("key"), derived_fields)
            visit_query_document(command.get("query"), derived_fields)
        elif isinstance(command.get("aggregate"), str):
            visit_pipeline(command.get("pipeline"), derived_fields)
        return references, unknown

    @classmethod
    def _mql_collection_references(cls, command: dict[str, Any]) -> set[str]:
        references = {
            str(command[name]).strip()
            for name in ("find", "aggregate", "count", "distinct")
            if isinstance(command.get(name), str) and str(command[name]).strip()
        }

        def visit(value: Any) -> None:
            if isinstance(value, list):
                for child in value:
                    visit(child)
            elif isinstance(value, dict):
                for key, child in value.items():
                    if key in {"$lookup", "$graphLookup"} and isinstance(child, dict):
                        source = child.get("from")
                        if isinstance(source, str) and source.strip():
                            references.add(source.strip())
                    elif key == "$unionWith":
                        if isinstance(child, str) and child.strip():
                            references.add(child.strip())
                        elif isinstance(child, dict) and isinstance(child.get("coll"), str):
                            references.add(child["coll"].strip())
                    visit(child)

        visit(command.get("pipeline"))
        return references

    @classmethod
    def _contains_key(cls, value: Any, keys: set[str]) -> bool:
        if isinstance(value, list):
            return any(cls._contains_key(child, keys) for child in value)
        if isinstance(value, dict):
            return any(key in keys or cls._contains_key(child, keys) for key, child in value.items())
        return False

    @classmethod
    def _contains_dynamic_mql_field_name(cls, value: Any) -> bool:
        if isinstance(value, list):
            return any(cls._contains_dynamic_mql_field_name(child) for child in value)
        if not isinstance(value, dict):
            return False
        for key, child in value.items():
            if key in {"$getField", "$setField"}:
                field = child.get("field") if isinstance(child, dict) else child
                if not isinstance(field, str) or not field.strip() or field.startswith("$"):
                    return True
            if cls._contains_dynamic_mql_field_name(child):
                return True
        return False

    @classmethod
    def _contains_non_positive_mql_limit(cls, command: dict[str, Any]) -> bool:
        top_level_limit = command.get("limit")
        if isinstance(top_level_limit, (int, float)) and not isinstance(top_level_limit, bool):
            if top_level_limit <= 0:
                return True

        def visit(value: Any) -> bool:
            if isinstance(value, list):
                return any(visit(child) for child in value)
            if not isinstance(value, dict):
                return False
            for key, child in value.items():
                if key == "$limit" and isinstance(child, (int, float)) and not isinstance(child, bool):
                    if child <= 0:
                        return True
                if visit(child):
                    return True
            return False

        return visit(command.get("pipeline"))


query_service = QueryService()
