"""把已验证的强类型 MongoDB 查询计划确定性编译为 MQL。"""

from __future__ import annotations

import json
import re
from copy import deepcopy
from typing import Any

from services.query_clarification import (
    ClarificationOption,
    QueryClarification,
    QueryClarificationRequired,
)


class MQLPlanError(ValueError):
    """计划不符合资源事实或当前编译器能力。"""


class MQLCompiler:
    FILTER_OPERATORS = {
        "eq", "ne", "gt", "gte", "lt", "lte", "in", "contains", "regex", "exists", "not_empty",
    }
    METRIC_OPERATIONS = {
        "none",
        "count_documents",
        "count_array_elements",
        "count_distinct_array_elements",
        "count_distinct_documents",
        "count_distinct_document_and_array_elements",
        "directional_overlap_rate",
        "distinct_count",
        "sum",
        "avg",
        "min",
        "max",
    }
    SET_COMPARISON_METRICS = {
        "unspecified",
        "intersection_count",
        "jaccard",
        "overlap_coefficient",
    }
    NUMERIC_TYPES = {"int", "bigint", "float", "double", "decimal", "integer", "number"}

    @classmethod
    def parse_plan(cls, output: str, decode_object) -> dict[str, Any]:
        parsed = decode_object(output)
        allowed = {
            "collection", "filters", "select_fields", "sort", "limit", "metric",
            "set_comparison", "assumptions", "clarification",
        }
        required = {"collection", "filters", "metric", "set_comparison", "assumptions", "clarification"}
        unknown = set(parsed) - allowed
        missing = required - set(parsed)
        if unknown:
            raise MQLPlanError("MQL semantic plan contains unknown fields: " + ", ".join(sorted(unknown)))
        if missing:
            raise MQLPlanError("MQL semantic plan is missing fields: " + ", ".join(sorted(missing)))
        if not isinstance(parsed["collection"], str) or not parsed["collection"].strip():
            raise MQLPlanError("MQL semantic plan collection must be a non-empty string")
        parsed.setdefault("select_fields", [])
        parsed.setdefault("sort", [])
        parsed.setdefault("limit", None)
        for key in ("filters", "select_fields", "sort", "assumptions"):
            if not isinstance(parsed[key], list):
                raise MQLPlanError(f"MQL semantic plan {key} must be an array")
        if not isinstance(parsed["metric"], dict):
            raise MQLPlanError("MQL semantic plan metric must be an object")
        if parsed["set_comparison"] is not None and not isinstance(parsed["set_comparison"], dict):
            raise MQLPlanError("MQL semantic plan set_comparison must be null or an object")
        if parsed["limit"] is not None and (
            not isinstance(parsed["limit"], int) or isinstance(parsed["limit"], bool) or parsed["limit"] <= 0
        ):
            raise MQLPlanError("MQL semantic plan limit must be null or a positive integer")
        if parsed["clarification"] is not None and not isinstance(parsed["clarification"], str):
            raise MQLPlanError("MQL semantic plan clarification must be null or a string")
        return parsed

    @classmethod
    def compile(
        cls,
        plan: dict[str, Any],
        resources: list[dict[str, Any]],
        *,
        clarification_answers: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        plan = cls._apply_clarification_answers(plan, clarification_answers or {})
        clarification = str(plan.get("clarification") or "").strip()
        assumptions = [str(item).strip() for item in plan.get("assumptions", []) if str(item).strip()]
        if clarification:
            raise QueryClarificationRequired(QueryClarification(
                key="query.semantic_details",
                category="semantic_ambiguity",
                prompt=clarification,
                control="text",
            ))
        if assumptions:
            raise QueryClarificationRequired(QueryClarification(
                key="query.assumptions",
                category="semantic_ambiguity",
                prompt="查询含义存在未经确认的假设，请补充或修正：" + "；".join(assumptions),
                control="text",
            ))

        collection = plan["collection"].strip()
        resource = cls._resource_for_collection(collection, resources)
        fields = cls._field_map(resource)
        set_comparison = cls._validate_set_comparison(plan["set_comparison"], plan, fields)
        if set_comparison is not None:
            command, parameters = cls._compile_set_comparison(collection, set_comparison)
            return {
                "query": json.dumps(command, ensure_ascii=False, separators=(",", ":")),
                "query_parameters": parameters,
                "explanation": "查询由已验证字段事实按集合比较语义确定性编译生成",
                "warnings": [],
                "plan": plan,
            }
        filters, parameters = cls._compile_filters(plan["filters"], fields)
        select_fields = cls._validate_field_list(plan["select_fields"], fields, "select_fields")
        sort = cls._compile_sort(plan["sort"], fields)
        metric = cls._validate_metric(plan["metric"], fields)
        cls._validate_filter_metric_roles(plan["filters"], metric)
        if metric["operation"] in {
            "count_distinct_array_elements",
            "count_distinct_document_and_array_elements",
            "directional_overlap_rate",
        }:
            element_filter_query, element_parameters = cls._compile_filters(
                metric["element_filters"],
                fields,
                existing_parameter_names={item["name"] for item in parameters},
            )
            metric["_element_filter_query"] = element_filter_query
            parameters.extend(element_parameters)
        if metric["operation"] == "directional_overlap_rate":
            parameters.extend([
                {"name": "entity_1", "type": "string", "default": metric["entity_values"][0]},
                {"name": "entity_2", "type": "string", "default": metric["entity_values"][1]},
            ])
        limit = plan["limit"]

        command = cls._compile_command(collection, filters, select_fields, sort, limit, metric)
        return {
            "query": json.dumps(command, ensure_ascii=False, separators=(",", ":")),
            "query_parameters": parameters,
            "explanation": "查询由已验证的 MongoDB 字段事实确定性编译生成",
            "warnings": [],
            "plan": plan,
        }

    @classmethod
    def _apply_clarification_answers(
        cls,
        plan: dict[str, Any],
        answers: dict[str, Any],
    ) -> dict[str, Any]:
        unsupported_keys = set(answers) - {
            "set_comparison.metric",
            "query.semantic_details",
            "query.assumptions",
        }
        if unsupported_keys:
            raise MQLPlanError(
                "MQL clarification answer key is unsupported: " + ", ".join(sorted(unsupported_keys))
            )
        result = deepcopy(plan)
        metric_answer = answers.get("set_comparison.metric")
        if metric_answer is not None:
            allowed = cls.SET_COMPARISON_METRICS - {"unspecified"}
            if not isinstance(metric_answer, str) or metric_answer not in allowed:
                raise MQLPlanError("MQL clarification answer is unsupported: set_comparison.metric")
            comparison = result.get("set_comparison")
            if not isinstance(comparison, dict):
                raise MQLPlanError("MQL clarification answer does not match the semantic plan")
            planned_metric = str(comparison.get("metric") or "").strip()
            if planned_metric not in {"unspecified", metric_answer}:
                raise MQLPlanError("MQL clarification answer conflicts with the semantic plan")
            comparison["metric"] = metric_answer
        return result

    @staticmethod
    def _resource_for_collection(collection: str, resources: list[dict[str, Any]]) -> dict[str, Any]:
        matches = [
            resource for resource in resources
            if isinstance(resource.get("query_names"), dict)
            and resource["query_names"].get("mql") == collection
        ]
        if len(matches) != 1:
            raise MQLPlanError(f"MQL collection is not uniquely verified: {collection}")
        if matches[0].get("schema_coverage") not in {"complete", "sampled"}:
            raise QueryClarificationRequired(QueryClarification(
                key="resource.schema",
                category="resource_facts",
                prompt=f"集合 {collection} 缺少可验证的字段结构，请先扫描元数据后再生成查询",
                control="notice",
            ))
        return matches[0]

    @staticmethod
    def _field_map(resource: dict[str, Any]) -> dict[str, dict[str, Any]]:
        result: dict[str, dict[str, Any]] = {}
        for field in resource.get("fields") or []:
            if not isinstance(field, dict):
                continue
            name = str(field.get("name") or "").strip()
            if name:
                result[name] = field
        return result

    @classmethod
    def _compile_filters(
        cls,
        filters: list[Any],
        fields: dict[str, dict[str, Any]],
        *,
        existing_parameter_names: set[str] | None = None,
    ) -> tuple[dict[str, Any], list[dict[str, Any]]]:
        clauses: list[dict[str, Any]] = []
        parameters: list[dict[str, Any]] = []
        parameter_names: set[str] = set(existing_parameter_names or set())
        for index, item in enumerate(filters):
            if not isinstance(item, dict):
                raise MQLPlanError(f"MQL filter[{index}] must be an object")
            required = {"field", "operator", "value"}
            if set(item) != required:
                raise MQLPlanError(f"MQL filter[{index}] fields must be {', '.join(sorted(required))}")
            field_name = str(item["field"] or "").strip()
            field = cls._verified_field(field_name, fields)
            operator = str(item["operator"] or "").strip()
            if operator not in cls.FILTER_OPERATORS:
                raise MQLPlanError(f"MQL filter operator is unsupported: {operator}")
            cls._validate_filter_type(field_name, field, operator)
            value = item["value"]
            cls._validate_filter_value(operator, value)
            if operator == "not_empty":
                clauses.append({field_name: {"$exists": True, "$nin": [None, ""]}})
                continue
            if operator == "in":
                operands: list[dict[str, str]] = []
                for item_value in value:
                    parameter_name = cls._parameter_name(field_name, parameter_names)
                    parameter_type = cls._parameter_type(item_value)
                    if parameter_type is None:
                        raise MQLPlanError(f"MQL filter parameter value type is unsupported: {parameter_name}")
                    operands.append({"$param": parameter_name})
                    parameters.append({"name": parameter_name, "type": parameter_type, "default": item_value})
                    parameter_names.add(parameter_name)
                clauses.append({field_name: {"$in": operands}})
                continue
            parameter_name = cls._parameter_name(field_name, parameter_names)
            parameter_type = cls._parameter_type(value)
            if parameter_type is None:
                raise MQLPlanError(f"MQL filter parameter value type is unsupported: {parameter_name}")
            operand: Any = {"$param": parameter_name}
            parameters.append({"name": parameter_name, "type": parameter_type, "default": value})
            parameter_names.add(parameter_name)
            clauses.append({field_name: cls._compile_filter_expression(operator, operand)})
        if not clauses:
            return {}, parameters
        if len(clauses) == 1:
            return clauses[0], parameters
        return {"$and": clauses}, parameters

    @classmethod
    def _validate_filter_type(cls, name: str, field: dict[str, Any], operator: str) -> None:
        field_type = str(field.get("type") or "unknown").lower()
        if operator in {"contains", "regex"} and field_type != "string":
            raise MQLPlanError(f"MQL operator {operator} requires a string field: {name}")
        if operator in {"gt", "gte", "lt", "lte"} and field_type not in cls.NUMERIC_TYPES | {"date", "time", "timestamp"}:
            raise MQLPlanError(f"MQL operator {operator} requires a numeric or temporal field: {name}")

    @staticmethod
    def _validate_filter_value(operator: str, value: Any) -> None:
        if operator in {"exists", "not_empty"}:
            if not isinstance(value, bool):
                raise MQLPlanError(f"MQL {operator} filter value must be boolean")
            return
        if operator == "in":
            if not isinstance(value, list) or not value:
                raise MQLPlanError("MQL in filter value must be a non-empty array")
            return
        if value is None or isinstance(value, (dict, list)):
            raise MQLPlanError(f"MQL {operator} filter value must be a scalar")

    @staticmethod
    def _compile_filter_expression(operator: str, operand: Any) -> Any:
        if operator == "eq":
            return operand
        mapping = {
            "ne": "$ne", "gt": "$gt", "gte": "$gte", "lt": "$lt", "lte": "$lte",
            "in": "$in", "exists": "$exists", "regex": "$regex", "contains": "$regex",
        }
        expression: dict[str, Any] = {mapping[operator]: operand}
        if operator == "contains":
            expression["$options"] = "i"
        return expression

    @staticmethod
    def _parameter_type(value: Any) -> str | None:
        if isinstance(value, bool):
            return "boolean"
        if isinstance(value, int):
            return "integer"
        if isinstance(value, float):
            return "number"
        if isinstance(value, str) and value:
            return "string"
        return None

    @staticmethod
    def _parameter_name(field_name: str, existing: set[str]) -> str:
        base = re.sub(r"[^A-Za-z0-9_]+", "_", field_name.split(".")[-1]).strip("_").lower() or "value"
        if base[0].isdigit():
            base = "value_" + base
        name = base
        suffix = 2
        while name in existing:
            name = f"{base}_{suffix}"
            suffix += 1
        return name

    @classmethod
    def _validate_field_list(
        cls,
        values: list[Any],
        fields: dict[str, dict[str, Any]],
        label: str,
    ) -> list[str]:
        result: list[str] = []
        for value in values:
            name = str(value or "").strip()
            cls._verified_field(name, fields)
            if name not in result:
                result.append(name)
        return result

    @classmethod
    def _compile_sort(cls, values: list[Any], fields: dict[str, dict[str, Any]]) -> dict[str, int]:
        result: dict[str, int] = {}
        for index, item in enumerate(values):
            if not isinstance(item, dict) or set(item) != {"field", "direction"}:
                raise MQLPlanError(f"MQL sort[{index}] must contain field and direction")
            name = str(item["field"] or "").strip()
            cls._verified_field(name, fields)
            direction = item["direction"]
            if direction not in {-1, 1}:
                raise MQLPlanError(f"MQL sort direction must be 1 or -1: {name}")
            result[name] = direction
        return result

    @classmethod
    def _validate_metric(cls, metric: dict[str, Any], fields: dict[str, dict[str, Any]]) -> dict[str, Any]:
        if isinstance(metric, dict) and metric.get("operation") == "count_distinct_documents":
            return cls._validate_distinct_document_metric(metric, fields)
        if isinstance(metric, dict) and metric.get("operation") == "count_distinct_document_and_array_elements":
            return cls._validate_distinct_document_and_array_metric(metric, fields)
        if isinstance(metric, dict) and metric.get("operation") == "directional_overlap_rate":
            return cls._validate_directional_overlap_metric(metric, fields)
        if not isinstance(metric, dict) or "operation" not in metric or "field" not in metric:
            raise MQLPlanError("MQL metric fields must contain operation and field")
        operation = str(metric["operation"] or "").strip()
        field_name = str(metric["field"] or "").strip()
        result_key = cls._metric_result_key(operation, field_name)
        if operation not in cls.METRIC_OPERATIONS:
            raise MQLPlanError(f"MQL metric operation is unsupported: {operation}")
        if operation == "count_distinct_array_elements":
            return cls._validate_distinct_array_metric(metric, fields)
        if set(metric) != {"operation", "field"}:
            raise MQLPlanError("MQL metric fields must be operation and field")
        if operation == "none":
            if field_name or result_key:
                raise MQLPlanError("MQL metric none must not declare field or result_key")
            return {"operation": operation, "field": "", "result_key": ""}
        if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", result_key):
            raise MQLPlanError("MQL metric result_key is invalid")
        if operation != "count_documents":
            field = cls._verified_field(field_name, fields)
            field_type = str(field.get("type") or "unknown").lower()
            if operation == "count_array_elements" and field_type != "array":
                raise MQLPlanError(f"MQL count_array_elements requires an array field: {field_name}")
            if operation in {"sum", "avg", "min", "max"} and field_type not in cls.NUMERIC_TYPES:
                raise MQLPlanError(f"MQL metric {operation} requires a numeric field: {field_name}")
        elif field_name:
            raise MQLPlanError("MQL count_documents must not declare a field")
        return {"operation": operation, "field": field_name, "result_key": result_key}

    @classmethod
    def _validate_distinct_array_metric(
        cls,
        metric: dict[str, Any],
        fields: dict[str, dict[str, Any]],
    ) -> dict[str, Any]:
        allowed = {"operation", "field", "group_by", "distinct_by", "element_filters"}
        unknown = set(metric) - allowed
        if unknown:
            raise MQLPlanError(
                "MQL count_distinct_array_elements metric contains unknown fields: "
                + ", ".join(sorted(unknown))
            )
        array_field = str(metric["field"] or "").strip()
        array = cls._verified_field(array_field, fields)
        if str(array.get("type") or "unknown").lower() != "array":
            raise MQLPlanError(
                "MQL count_distinct_array_elements requires an array field: " + array_field
            )
        group_by = metric.get("group_by")
        distinct_by = metric.get("distinct_by")
        element_filters = metric.get("element_filters")
        if not isinstance(group_by, list) or not group_by:
            raise MQLPlanError("MQL count_distinct_array_elements group_by must be a non-empty array")
        if not isinstance(distinct_by, list) or not distinct_by:
            raise MQLPlanError("MQL count_distinct_array_elements distinct_by must be a non-empty array")
        if not isinstance(element_filters, list):
            raise MQLPlanError("MQL count_distinct_array_elements element_filters must be an array")

        for label, paths in (("group_by", group_by), ("distinct_by", distinct_by)):
            for index, path in enumerate(paths):
                name = str(path or "").strip()
                if not name:
                    raise MQLPlanError(f"MQL {label}[{index}] must be a non-empty field path")
                field = cls._verified_field(name, fields)
                field_type = str(field.get("type") or "unknown").lower()
                if field_type in {"array", "object", "json"}:
                    raise MQLPlanError(f"MQL {label} field must be scalar: {name}")
                if label == "group_by" and not name.startswith(array_field + "."):
                    raise MQLPlanError(
                        f"MQL count_distinct_array_elements group_by field must belong to {array_field}: {name}"
                    )

        for index, item in enumerate(element_filters):
            if not isinstance(item, dict) or set(item) != {"field", "operator", "value"}:
                raise MQLPlanError(
                    f"MQL element_filters[{index}] fields must be field, operator and value"
                )
            name = str(item["field"] or "").strip()
            if not name.startswith(array_field + "."):
                raise MQLPlanError(
                    f"MQL element filter field must belong to {array_field}: {name}"
                )
            field = cls._verified_field(name, fields)
            operator = str(item["operator"] or "").strip()
            if operator not in cls.FILTER_OPERATORS:
                raise MQLPlanError(f"MQL filter operator is unsupported: {operator}")
            cls._validate_filter_type(name, field, operator)
            cls._validate_filter_value(operator, item["value"])
        result_key = cls._metric_result_key(metric["operation"], array_field)
        if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", result_key):
            raise MQLPlanError("MQL metric result_key is invalid")
        return {
            "operation": metric["operation"],
            "field": array_field,
            "group_by": list(dict.fromkeys(str(item).strip() for item in group_by)),
            "distinct_by": list(dict.fromkeys(str(item).strip() for item in distinct_by)),
            "group_by_types": [
                str(fields[str(item).strip()].get("type") or "unknown").lower()
                for item in dict.fromkeys(str(item).strip() for item in group_by)
            ],
            "distinct_by_types": [
                str(fields[str(item).strip()].get("type") or "unknown").lower()
                for item in dict.fromkeys(str(item).strip() for item in distinct_by)
            ],
            "element_filters": element_filters,
            "result_key": result_key,
        }

    @classmethod
    def _validate_distinct_document_metric(
        cls,
        metric: dict[str, Any],
        fields: dict[str, dict[str, Any]],
    ) -> dict[str, Any]:
        allowed = {"operation", "field", "group_by", "distinct_by"}
        unknown = set(metric) - allowed
        if unknown:
            raise MQLPlanError(
                "MQL count_distinct_documents metric contains unknown fields: "
                + ", ".join(sorted(unknown))
            )
        if str(metric.get("field") or "").strip():
            raise MQLPlanError("MQL count_distinct_documents field must be empty")
        group_by = metric.get("group_by")
        distinct_by = metric.get("distinct_by")
        if not isinstance(group_by, list) or not group_by:
            raise MQLPlanError("MQL count_distinct_documents group_by must be a non-empty array")
        if not isinstance(distinct_by, list) or not distinct_by:
            raise MQLPlanError("MQL count_distinct_documents distinct_by must be a non-empty array")
        normalized_group_by = list(dict.fromkeys(str(item).strip() for item in group_by))
        normalized_distinct_by = list(dict.fromkeys(str(item).strip() for item in distinct_by))
        for label, paths in (("group_by", normalized_group_by), ("distinct_by", normalized_distinct_by)):
            for index, name in enumerate(paths):
                if not name:
                    raise MQLPlanError(f"MQL count_distinct_documents {label}[{index}] must be non-empty")
                field = cls._verified_field(name, fields)
                if str(field.get("type") or "unknown").lower() in {"array", "object", "json"}:
                    raise MQLPlanError(f"MQL count_distinct_documents {label} field must be scalar: {name}")
        return {
            "operation": metric["operation"],
            "field": "",
            "group_by": normalized_group_by,
            "distinct_by": normalized_distinct_by,
            "group_by_types": [str(fields[name].get("type") or "unknown").lower() for name in normalized_group_by],
            "distinct_by_types": [str(fields[name].get("type") or "unknown").lower() for name in normalized_distinct_by],
            "result_key": "distinct_document_count",
        }

    @classmethod
    def _validate_distinct_document_and_array_metric(
        cls,
        metric: dict[str, Any],
        fields: dict[str, dict[str, Any]],
    ) -> dict[str, Any]:
        allowed = {
            "operation", "field", "group_by", "document_group_by", "distinct_by", "element_filters",
        }
        unknown = set(metric) - allowed
        if unknown:
            raise MQLPlanError(
                "MQL count_distinct_document_and_array_elements metric contains unknown fields: "
                + ", ".join(sorted(unknown))
            )
        array_field = str(metric.get("field") or "").strip()
        array = cls._verified_field(array_field, fields)
        if str(array.get("type") or "unknown").lower() != "array":
            raise MQLPlanError(
                "MQL count_distinct_document_and_array_elements requires an array field: " + array_field
            )
        group_by = metric.get("group_by")
        document_group_by = metric.get("document_group_by")
        distinct_by = metric.get("distinct_by")
        element_filters = metric.get("element_filters")
        if not isinstance(group_by, list) or len(group_by) != 1 or not str(group_by[0] or "").strip():
            raise MQLPlanError(
                "MQL count_distinct_document_and_array_elements group_by must contain exactly one field"
            )
        if not isinstance(document_group_by, list) or len(document_group_by) != 1 or not str(document_group_by[0] or "").strip():
            raise MQLPlanError(
                "MQL count_distinct_document_and_array_elements document_group_by must contain exactly one field"
            )
        if not isinstance(distinct_by, list) or not distinct_by:
            raise MQLPlanError(
                "MQL count_distinct_document_and_array_elements distinct_by must be a non-empty array"
            )
        if not isinstance(element_filters, list):
            raise MQLPlanError(
                "MQL count_distinct_document_and_array_elements element_filters must be an array"
            )

        normalized_group_by = [str(group_by[0]).strip()]
        normalized_document_group_by = [str(document_group_by[0]).strip()]
        normalized_distinct_by = list(dict.fromkeys(str(item).strip() for item in distinct_by))
        array_group_field = cls._verified_field(normalized_group_by[0], fields)
        if not normalized_group_by[0].startswith(array_field + "."):
            raise MQLPlanError(
                "MQL count_distinct_document_and_array_elements group_by field must belong to "
                + array_field
            )
        if str(array_group_field.get("type") or "unknown").lower() in {"array", "object", "json"}:
            raise MQLPlanError("MQL array group_by field must be scalar: " + normalized_group_by[0])
        document_group_field = cls._verified_field(normalized_document_group_by[0], fields)
        if str(document_group_field.get("type") or "unknown").lower() in {"array", "object", "json"}:
            raise MQLPlanError(
                "MQL document_group_by field must be scalar: " + normalized_document_group_by[0]
            )
        for index, name in enumerate(normalized_distinct_by):
            if not name:
                raise MQLPlanError(
                    f"MQL count_distinct_document_and_array_elements distinct_by[{index}] must be non-empty"
                )
            field = cls._verified_field(name, fields)
            if str(field.get("type") or "unknown").lower() in {"array", "object", "json"}:
                raise MQLPlanError("MQL distinct_by field must be scalar: " + name)

        for index, item in enumerate(element_filters):
            if not isinstance(item, dict) or set(item) != {"field", "operator", "value"}:
                raise MQLPlanError(
                    "MQL count_distinct_document_and_array_elements element_filters["
                    f"{index}] fields must be field, operator and value"
                )
            name = str(item["field"] or "").strip()
            if not name.startswith(array_field + "."):
                raise MQLPlanError(
                    "MQL document-and-array element filter field must belong to " + array_field + ": " + name
                )
            field = cls._verified_field(name, fields)
            operator = str(item["operator"] or "").strip()
            if operator not in cls.FILTER_OPERATORS:
                raise MQLPlanError(f"MQL filter operator is unsupported: {operator}")
            cls._validate_filter_type(name, field, operator)
            cls._validate_filter_value(operator, item["value"])

        return {
            "operation": metric["operation"],
            "field": array_field,
            "group_by": normalized_group_by,
            "document_group_by": normalized_document_group_by,
            "distinct_by": normalized_distinct_by,
            "group_by_type": str(array_group_field.get("type") or "unknown").lower(),
            "document_group_by_type": str(document_group_field.get("type") or "unknown").lower(),
            "distinct_by_types": [
                str(fields[name].get("type") or "unknown").lower() for name in normalized_distinct_by
            ],
            "element_filters": element_filters,
            "result_key": "distinct_document_and_array_count",
        }

    @classmethod
    def _validate_directional_overlap_metric(
        cls,
        metric: dict[str, Any],
        fields: dict[str, dict[str, Any]],
    ) -> dict[str, Any]:
        allowed = {
            "operation", "field", "entity_field", "entity_values",
            "activity_id_field", "element_filters",
        }
        unknown = set(metric) - allowed
        if unknown:
            raise MQLPlanError(
                "MQL directional_overlap_rate metric contains unknown fields: "
                + ", ".join(sorted(unknown))
            )
        array_field = str(metric.get("field") or "").strip()
        array = cls._verified_field(array_field, fields)
        if str(array.get("type") or "unknown").lower() != "array":
            raise MQLPlanError("MQL directional_overlap_rate requires an array field: " + array_field)
        entity_field = str(metric.get("entity_field") or "").strip()
        if not entity_field.startswith(array_field + "."):
            raise MQLPlanError("MQL directional_overlap_rate entity_field must belong to " + array_field)
        entity = cls._verified_field(entity_field, fields)
        if str(entity.get("type") or "unknown").lower() != "string":
            raise MQLPlanError("MQL directional_overlap_rate entity_field must be a string field")
        entity_values = metric.get("entity_values")
        if (
            not isinstance(entity_values, list)
            or len(entity_values) != 2
            or any(not isinstance(value, str) or not value.strip() for value in entity_values)
            or entity_values[0] == entity_values[1]
        ):
            raise MQLPlanError("MQL directional_overlap_rate requires two distinct non-empty entity_values")
        activity_id_field = str(metric.get("activity_id_field") or "").strip()
        activity = cls._verified_field(activity_id_field, fields)
        if str(activity.get("type") or "unknown").lower() in {"array", "object", "json"}:
            raise MQLPlanError("MQL directional_overlap_rate activity_id_field must be scalar")
        element_filters = metric.get("element_filters")
        if not isinstance(element_filters, list) or not element_filters:
            raise MQLPlanError("MQL directional_overlap_rate element_filters must be a non-empty array")
        for index, item in enumerate(element_filters):
            if not isinstance(item, dict) or set(item) != {"field", "operator", "value"}:
                raise MQLPlanError(
                    f"MQL directional_overlap_rate element_filters[{index}] fields must be field, operator and value"
                )
            name = str(item["field"] or "").strip()
            if not name.startswith(array_field + "."):
                raise MQLPlanError("MQL directional_overlap_rate element filter must belong to " + array_field)
            field = cls._verified_field(name, fields)
            operator = str(item["operator"] or "").strip()
            if operator not in cls.FILTER_OPERATORS:
                raise MQLPlanError(f"MQL filter operator is unsupported: {operator}")
            cls._validate_filter_type(name, field, operator)
            cls._validate_filter_value(operator, item["value"])
        return {
            "operation": metric["operation"],
            "field": array_field,
            "entity_field": entity_field,
            "entity_values": [value.strip() for value in entity_values],
            "activity_id_field": activity_id_field,
            "element_filters": element_filters,
            "result_key": "directional_overlap_rate",
        }

    @staticmethod
    def _metric_result_key(operation: str, field_name: str) -> str:
        if operation == "none":
            return ""
        if operation == "count_documents":
            return "document_count"
        if operation == "count_distinct_documents":
            return "distinct_document_count"
        if operation == "count_distinct_document_and_array_elements":
            return "distinct_document_and_array_count"
        if operation == "directional_overlap_rate":
            return "directional_overlap_rate"
        leaf = re.sub(r"[^A-Za-z0-9_]+", "_", field_name.split(".")[-1]).strip("_").lower() or "value"
        return f"{leaf}_{operation}"

    @staticmethod
    def _validate_filter_metric_roles(filters: list[Any], metric: dict[str, Any]) -> None:
        if metric["operation"] not in {
            "count_array_elements",
            "count_distinct_array_elements",
            "count_distinct_document_and_array_elements",
            "directional_overlap_rate",
        }:
            return
        array_field = metric["field"]
        for item in filters:
            filter_field = str(item.get("field") or "")
            if filter_field.startswith(array_field + "."):
                raise MQLPlanError(
                    "count_array_elements filters must identify the owning record, "
                    f"not an element field under {array_field}: {filter_field}"
                )

    @staticmethod
    def _verified_field(name: str, fields: dict[str, dict[str, Any]]) -> dict[str, Any]:
        if not name or name not in fields:
            raise MQLPlanError(f"MQL field is not verified: {name}")
        return fields[name]

    @classmethod
    def _compile_command(
        cls,
        collection: str,
        filters: dict[str, Any],
        select_fields: list[str],
        sort: dict[str, int],
        limit: int | None,
        metric: dict[str, Any],
    ) -> dict[str, Any]:
        operation = metric["operation"]
        if operation == "none":
            command: dict[str, Any] = {"find": collection, "filter": filters}
            if select_fields:
                command["projection"] = {field: 1 for field in select_fields}
            if sort:
                command["sort"] = sort
            if limit is not None:
                command["limit"] = limit
            return command
        if operation == "count_documents":
            return {"count": collection, "query": filters}

        pipeline: list[dict[str, Any]] = []
        if filters:
            pipeline.append({"$match": filters})
        field_name = metric["field"]
        result_key = metric["result_key"]
        if operation == "count_array_elements":
            pipeline.extend([
                {"$project": {"_element_count": {"$size": {"$ifNull": [f"${field_name}", []]}}}},
                {"$group": {"_id": None, result_key: {"$sum": "$_element_count"}}},
                {"$project": {"_id": 0, result_key: 1}},
            ])
        elif operation == "count_distinct_array_elements":
            array_field = metric["field"]
            pipeline.append({"$unwind": f"${array_field}"})
            if metric.get("_element_filter_query"):
                pipeline.append({"$match": metric["_element_filter_query"]})
            identity_paths = list(dict.fromkeys(metric["group_by"] + metric["distinct_by"]))
            identity_types = {
                path: field_type
                for path, field_type in zip(
                    metric["group_by"] + metric["distinct_by"],
                    metric["group_by_types"] + metric["distinct_by_types"],
                    strict=True,
                )
            }
            identity_match: dict[str, Any] = {}
            for path in identity_paths:
                if identity_types[path] == "string":
                    condition: dict[str, Any] = {"$exists": True, "$nin": [None, ""]}
                else:
                    condition = {"$exists": True, "$ne": None}
                identity_match[path] = condition
            pipeline.append({"$match": identity_match})
            group_id = {
                f"group_{index}": f"${path}"
                for index, path in enumerate(metric["group_by"])
            }
            distinct_id = {
                f"distinct_{index}": f"${path}"
                for index, path in enumerate(metric["distinct_by"])
            }
            grouped_id = {**group_id, **distinct_id}
            second_group_id = {
                key: f"$_id.{key}" for key in group_id
            }
            projection = {"_id": 0}
            used_aliases: set[str] = set()
            for index, path in enumerate(metric["group_by"]):
                leaf = re.sub(r"[^A-Za-z0-9_]+", "_", path.split(".")[-1]).strip("_").lower() or f"group_{index}"
                alias = leaf
                suffix = 2
                while alias in used_aliases:
                    alias = f"{leaf}_{suffix}"
                    suffix += 1
                used_aliases.add(alias)
                projection[alias] = f"$_id.group_{index}"
            projection[metric["result_key"]] = "$_distinct_count"
            pipeline.extend([
                {"$group": {"_id": grouped_id}},
                {"$group": {
                    "_id": second_group_id,
                    "_distinct_count": {"$sum": 1},
                }},
                {"$project": projection},
            ])
        elif operation == "count_distinct_document_and_array_elements":
            array_group_field = metric["group_by"][0]
            document_group_field = metric["document_group_by"][0]
            distinct_paths = metric["distinct_by"]

            def identity_condition(field_type: str) -> dict[str, Any]:
                if field_type == "string":
                    return {"$exists": True, "$nin": [None, ""]}
                return {"$exists": True, "$ne": None}

            member_identity_match = {
                array_group_field: identity_condition(metric["group_by_type"]),
                **{
                    path: identity_condition(field_type)
                    for path, field_type in zip(
                        distinct_paths, metric["distinct_by_types"], strict=True
                    )
                },
            }
            document_identity_match = {
                document_group_field: identity_condition(metric["document_group_by_type"]),
                **{
                    path: identity_condition(field_type)
                    for path, field_type in zip(
                        distinct_paths, metric["distinct_by_types"], strict=True
                    )
                },
            }

            def distinct_expression(prefix: str) -> Any:
                if len(distinct_paths) == 1:
                    return f"${prefix}{distinct_paths[0]}"
                return {
                    f"distinct_{index}": f"${prefix}{path}"
                    for index, path in enumerate(distinct_paths)
                }

            pipeline.extend([
                {"$unwind": f"${metric['field']}"},
            ])
            if metric.get("_element_filter_query"):
                pipeline.append({"$match": metric["_element_filter_query"]})
            pipeline.extend([
                {"$match": member_identity_match},
                {"$project": {
                    "_metric_group": f"${array_group_field}",
                    "_metric_distinct": distinct_expression(""),
                }},
                {"$unionWith": {
                    "coll": collection,
                    "pipeline": [
                        *([{"$match": filters}] if filters else []),
                        {"$match": document_identity_match},
                        {"$project": {
                            "_metric_group": f"${document_group_field}",
                            "_metric_distinct": distinct_expression(""),
                        }},
                    ],
                }},
                {"$group": {
                    "_id": {"group": "$_metric_group", "distinct": "$_metric_distinct"},
                }},
                {"$group": {
                    "_id": "$_id.group",
                    "_distinct_count": {"$sum": 1},
                }},
            ])
            group_alias = re.sub(
                r"[^A-Za-z0-9_]+", "_", document_group_field.split(".")[-1]
            ).strip("_").lower() or "group"
            pipeline.append({"$project": {
                "_id": 0,
                group_alias: "$_id",
                metric["result_key"]: "$_distinct_count",
            }})
        elif operation == "directional_overlap_rate":
            pipeline.extend(cls._compile_directional_overlap_pipeline(metric))
        elif operation == "count_distinct_documents":
            identity_paths = list(dict.fromkeys(metric["group_by"] + metric["distinct_by"]))
            identity_types = {
                path: field_type
                for path, field_type in zip(
                    metric["group_by"] + metric["distinct_by"],
                    metric["group_by_types"] + metric["distinct_by_types"],
                    strict=True,
                )
            }
            identity_match: dict[str, Any] = {}
            for path in identity_paths:
                if identity_types[path] == "string":
                    identity_match[path] = {"$exists": True, "$nin": [None, ""]}
                else:
                    identity_match[path] = {"$exists": True, "$ne": None}
            pipeline.append({"$match": identity_match})
            grouped_id = {
                **{f"group_{index}": f"${path}" for index, path in enumerate(metric["group_by"])},
                **{f"distinct_{index}": f"${path}" for index, path in enumerate(metric["distinct_by"])},
            }
            second_group_id = {
                f"group_{index}": f"$_id.group_{index}"
                for index in range(len(metric["group_by"]))
            }
            projection = {"_id": 0}
            used_aliases: set[str] = set()
            for index, path in enumerate(metric["group_by"]):
                leaf = re.sub(r"[^A-Za-z0-9_]+", "_", path.split(".")[-1]).strip("_").lower() or f"group_{index}"
                alias = leaf
                suffix = 2
                while alias in used_aliases:
                    alias = f"{leaf}_{suffix}"
                    suffix += 1
                used_aliases.add(alias)
                projection[alias] = f"$_id.group_{index}"
            projection[metric["result_key"]] = "$_distinct_count"
            pipeline.extend([
                {"$group": {"_id": grouped_id}},
                {"$group": {"_id": second_group_id, "_distinct_count": {"$sum": 1}}},
                {"$project": projection},
            ])
        elif operation == "distinct_count":
            pipeline.extend([
                {"$group": {"_id": f"${field_name}"}},
                {"$count": result_key},
            ])
        else:
            pipeline.extend([
                {"$group": {"_id": None, result_key: {f"${operation}": f"${field_name}"}}},
                {"$project": {"_id": 0, result_key: 1}},
            ])
        return {"aggregate": collection, "pipeline": pipeline}

    @classmethod
    def _compile_directional_overlap_pipeline(cls, metric: dict[str, Any]) -> list[dict[str, Any]]:
        entity_field = metric["entity_field"]
        activity_id_field = metric["activity_id_field"]
        entity_1, entity_2 = metric["entity_values"]
        pipeline: list[dict[str, Any]] = [
            {"$unwind": f"${metric['field']}"},
        ]
        if metric.get("_element_filter_query"):
            pipeline.append({"$match": metric["_element_filter_query"]})
        pipeline.extend([
            {"$match": {
                entity_field: {"$in": [{"$param": "entity_1"}, {"$param": "entity_2"}]},
                activity_id_field: {"$exists": True, "$nin": [None, ""]},
            }},
            {"$match": {
                entity_field: {"$exists": True, "$nin": [None, ""]},
            }},
            {"$facet": {
                "_entities": [
                    {"$group": {
                        "_id": {"entity": f"${entity_field}", "activity": f"${activity_id_field}"},
                    }},
                    {"$group": {
                        "_id": "$_id.entity",
                        "activities": {"$addToSet": "$_id.activity"},
                    }},
                    {"$project": {"_id": 0, "entity": "$_id", "activities": 1}},
                ],
            }},
        ])

        def values(parameter: str) -> dict[str, Any]:
            return {
                "$let": {
                    "vars": {
                        "entity": {
                            "$arrayElemAt": [
                                {
                                    "$filter": {
                                        "input": "$_entities",
                                        "as": "entry",
                                        "cond": {"$eq": ["$$entry.entity", {"$param": parameter}]},
                                    },
                                },
                                0,
                            ],
                        },
                    },
                    "in": {"$ifNull": ["$$entity.activities", []]},
                },
            }

        pipeline.extend([
            {"$project": {
                "_id": 0,
                "left": values("entity_1"),
                "right": values("entity_2"),
            }},
            {"$project": {
                "_id": 0,
                "left_count": {"$size": "$left"},
                "right_count": {"$size": "$right"},
                "intersection_count": {"$size": {"$setIntersection": ["$left", "$right"]}},
            }},
            {"$project": {
                "_id": 0,
                "left_count": 1,
                "right_count": 1,
                "intersection_count": 1,
                "overlap_rate_from_left": {"$cond": [
                    {"$eq": ["$left_count", 0]},
                    0,
                    {"$divide": ["$intersection_count", "$left_count"]},
                ]},
                "overlap_rate_from_right": {"$cond": [
                    {"$eq": ["$right_count", 0]},
                    0,
                    {"$divide": ["$intersection_count", "$right_count"]},
                ]},
            }},
        ])
        return pipeline

    @classmethod
    def _validate_set_comparison(
        cls,
        comparison: dict[str, Any] | None,
        plan: dict[str, Any],
        fields: dict[str, dict[str, Any]],
    ) -> dict[str, Any] | None:
        if comparison is None:
            return None
        required = {"entity_field", "entity_values", "set_fields", "metric"}
        if set(comparison) != required:
            raise MQLPlanError("MQL set_comparison fields must be entity_field, entity_values, set_fields and metric")
        if plan["filters"] or plan["select_fields"] or plan["sort"] or plan["limit"] is not None:
            raise MQLPlanError("MQL set_comparison cannot be combined with filters, projection, sort or limit")
        metric = str(comparison["metric"] or "").strip()
        if metric not in cls.SET_COMPARISON_METRICS:
            raise MQLPlanError(f"MQL set comparison metric is unsupported: {metric}")
        if metric == "unspecified":
            raise QueryClarificationRequired(QueryClarification(
                key="set_comparison.metric",
                category="calculation_rule",
                prompt="当前集合比较存在多种计算规则，请选择本次使用的口径。",
                control="single_choice",
                options=(
                    ClarificationOption(
                        "intersection_count",
                        "共同元素数量",
                        "统计两个集合去重后的交集数量",
                    ),
                    ClarificationOption(
                        "jaccard",
                        "Jaccard 相似度",
                        "交集数量除以并集数量",
                    ),
                    ClarificationOption(
                        "overlap_coefficient",
                        "重叠系数",
                        "交集数量除以较小集合的元素数量",
                    ),
                ),
            ))
        entity_field = str(comparison["entity_field"] or "").strip()
        entity = cls._verified_field(entity_field, fields)
        if str(entity.get("type") or "").lower() != "string":
            raise MQLPlanError("MQL set comparison entity_field must be a string field")
        entity_values = comparison["entity_values"]
        if (
            not isinstance(entity_values, list)
            or len(entity_values) != 2
            or any(not isinstance(value, str) or not value.strip() for value in entity_values)
            or entity_values[0] == entity_values[1]
        ):
            raise MQLPlanError("MQL set comparison requires two distinct non-empty entity_values")
        set_fields = comparison["set_fields"]
        if not isinstance(set_fields, list) or not set_fields:
            raise MQLPlanError("MQL set comparison set_fields must be a non-empty array")
        normalized_fields: list[str] = []
        normalized_field_types: list[str] = []
        for value in set_fields:
            field_name = str(value or "").strip()
            field = cls._verified_field(field_name, fields)
            if str(field.get("type") or "").lower() in {"array", "json", "object"}:
                raise MQLPlanError(f"MQL set comparison identity field must be scalar: {field_name}")
            if "." not in field_name:
                raise MQLPlanError(f"MQL set comparison identity field must be an array element path: {field_name}")
            array_name = field_name.rsplit(".", 1)[0]
            array_field = cls._verified_field(array_name, fields)
            if str(array_field.get("type") or "").lower() != "array":
                raise MQLPlanError(f"MQL set comparison parent field must be an array: {array_name}")
            if field_name not in normalized_fields:
                normalized_fields.append(field_name)
                normalized_field_types.append(str(field.get("type") or "unknown").lower())
        metric_plan = cls._validate_metric(plan["metric"], fields)
        if metric_plan["operation"] != "none":
            raise MQLPlanError("MQL set_comparison requires metric.operation none")
        return {
            "entity_field": entity_field,
            "entity_values": [value.strip() for value in entity_values],
            "set_fields": normalized_fields,
            "set_field_types": normalized_field_types,
            "metric": metric,
        }

    @classmethod
    def _compile_set_comparison(
        cls,
        collection: str,
        comparison: dict[str, Any],
    ) -> tuple[dict[str, Any], list[dict[str, Any]]]:
        parameters = [
            {"name": "entity_1", "type": "string", "default": comparison["entity_values"][0]},
            {"name": "entity_2", "type": "string", "default": comparison["entity_values"][1]},
        ]

        mapped_sets = []
        for field_name, field_type in zip(
            comparison["set_fields"],
            comparison["set_field_types"],
            strict=True,
        ):
            array_name, leaf_name = field_name.rsplit(".", 1)
            mapped_values = {
                "$map": {
                    "input": {"$ifNull": [f"${array_name}", []]},
                    "as": "item",
                    "in": f"$$item.{leaf_name}",
                },
            }
            mapped_sets.append({
                "$filter": {
                    "input": mapped_values,
                    "as": "value",
                    "cond": cls._valid_set_value_condition(field_type),
                },
            })

        def entity_values(parameter: str) -> dict[str, Any]:
            return {
                "$let": {
                    "vars": {
                        "entity": {
                            "$arrayElemAt": [
                                {
                                    "$filter": {
                                        "input": "$_entities",
                                        "as": "entry",
                                        "cond": {"$eq": ["$$entry.entity", {"$param": parameter}]},
                                    },
                                },
                                0,
                            ],
                        },
                    },
                    "in": {"$ifNull": ["$$entity.values", []]},
                },
            }

        pipeline: list[dict[str, Any]] = [
            {"$match": {
                comparison["entity_field"]: {
                    "$in": [{"$param": "entity_1"}, {"$param": "entity_2"}],
                },
            }},
            {"$project": {
                "_entity": f"${comparison['entity_field']}",
                "_values": {"$setUnion": mapped_sets},
            }},
            {"$group": {"_id": "$_entity", "_value_sets": {"$push": "$_values"}}},
            {"$project": {
                "_id": 0,
                "entity": "$_id",
                "values": {
                    "$reduce": {
                        "input": "$_value_sets",
                        "initialValue": [],
                        "in": {"$setUnion": ["$$value", "$$this"]},
                    },
                },
            }},
            {"$group": {
                "_id": None,
                "_entities": {"$push": {"entity": "$entity", "values": "$values"}},
            }},
            {"$project": {
                "_id": 0,
                "left": entity_values("entity_1"),
                "right": entity_values("entity_2"),
            }},
            {"$project": {
                "left_count": {"$size": "$left"},
                "right_count": {"$size": "$right"},
                "intersection_count": {"$size": {"$setIntersection": ["$left", "$right"]}},
                "union_count": {"$size": {"$setUnion": ["$left", "$right"]}},
            }},
        ]
        metric = comparison["metric"]
        if metric == "intersection_count":
            pipeline.append({"$project": {"_id": 0, "left_count": 1, "right_count": 1, "intersection_count": 1}})
        elif metric == "jaccard":
            pipeline.append({"$project": {
                "_id": 0,
                "left_count": 1,
                "right_count": 1,
                "intersection_count": 1,
                "union_count": 1,
                "jaccard": {"$cond": [
                    {"$eq": ["$union_count", 0]},
                    0,
                    {"$divide": ["$intersection_count", "$union_count"]},
                ]},
            }})
        else:
            pipeline.extend([
                {"$set": {"_overlap_denominator": {"$min": ["$left_count", "$right_count"]}}},
                {"$project": {
                    "_id": 0,
                    "left_count": 1,
                    "right_count": 1,
                    "intersection_count": 1,
                    "overlap_coefficient": {"$cond": [
                        {"$eq": ["$_overlap_denominator", 0]},
                        0,
                        {"$divide": ["$intersection_count", "$_overlap_denominator"]},
                    ]},
                }},
            ])
        return {"aggregate": collection, "pipeline": pipeline}, parameters

    @staticmethod
    def _valid_set_value_condition(field_type: str) -> dict[str, Any]:
        bson_types = {
            "string": ["string"],
            "int": ["int", "long"],
            "integer": ["int", "long"],
            "bigint": ["long", "int"],
            "float": ["double", "decimal", "int", "long"],
            "double": ["double", "decimal", "int", "long"],
            "decimal": ["decimal", "double", "int", "long"],
            "number": ["double", "decimal", "int", "long"],
            "bool": ["bool"],
            "boolean": ["bool"],
            "date": ["date"],
            "time": ["date"],
            "timestamp": ["date"],
            "objectid": ["objectId"],
        }.get(field_type)
        conditions: list[dict[str, Any]] = [
            {"$ne": ["$$value", None]},
            {"$ne": [{"$type": "$$value"}, "missing"]},
        ]
        if bson_types:
            conditions.append({"$in": [{"$type": "$$value"}, bson_types]})
        if field_type == "string":
            conditions.append({"$ne": ["$$value", ""]})
        return {"$and": conditions}
