import copy
import json
from typing import Any

from jsonschema import Draft202012Validator
from addp_common.tools import get_tool, preview_resource_fact


CHECKPOINT_SCHEMA = "addp.agent-checkpoint/v1"
CHECKPOINT_MAX_BYTES = 256 * 1024


def validate_checkpoint_size(checkpoint: dict[str, Any]) -> None:
    encoded = json.dumps(checkpoint, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    if len(encoded) > CHECKPOINT_MAX_BYTES:
        raise ValueError(f"agent checkpoint exceeds {CHECKPOINT_MAX_BYTES} bytes")


def new_checkpoint() -> dict[str, Any]:
    return {
        "schema": CHECKPOINT_SCHEMA,
        "observed": {"workflow_engines": {}, "resources": {}, "platform_capabilities": {}, "operation_reviews": {}},
        "confirmed": {"workflow_engine": None, "resources": {}, "operation_reviews": {}},
    }


def normalize_checkpoint(value: Any) -> dict[str, Any]:
    checkpoint = new_checkpoint()
    if not isinstance(value, dict) or value.get("schema") != CHECKPOINT_SCHEMA:
        return checkpoint

    observed = value.get("observed") if isinstance(value.get("observed"), dict) else {}
    confirmed = value.get("confirmed") if isinstance(value.get("confirmed"), dict) else {}
    workflow_engines = observed.get("workflow_engines")
    resources = observed.get("resources")
    confirmed_resources = confirmed.get("resources")
    if isinstance(workflow_engines, dict):
        checkpoint["observed"]["workflow_engines"] = copy.deepcopy(workflow_engines)
    if isinstance(resources, dict):
        checkpoint["observed"]["resources"] = copy.deepcopy(resources)
    if isinstance(confirmed.get("workflow_engine"), dict):
        checkpoint["confirmed"]["workflow_engine"] = copy.deepcopy(confirmed["workflow_engine"])
    if isinstance(confirmed_resources, dict):
        checkpoint["confirmed"]["resources"] = copy.deepcopy(confirmed_resources)
    for key in ("platform_capabilities", "operation_reviews"):
        if isinstance(observed.get(key), dict):
            checkpoint["observed"][key] = copy.deepcopy(observed[key])
    if isinstance(confirmed.get("operation_reviews"), dict):
        checkpoint["confirmed"]["operation_reviews"] = copy.deepcopy(confirmed["operation_reviews"])
    return checkpoint


def _walk_objects(value: Any):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from _walk_objects(child)
    elif isinstance(value, list):
        for child in value:
            yield from _walk_objects(child)


def resource_locator(value: dict[str, Any]) -> str | None:
    locator = value.get("locator")
    if isinstance(locator, str) and locator.startswith("addp://"):
        return locator
    location = value.get("location")
    if isinstance(location, dict):
        locator = location.get("locator")
        if isinstance(locator, str) and locator.startswith("addp://"):
            return locator
    return None


def _compact_resource_fact(value: dict[str, Any], locator: str) -> dict[str, Any]:
    location = value.get("location") if isinstance(value.get("location"), dict) else {}
    fact = {"locator": locator}
    for key in ("engine_id", "engine_name", "asset_type", "item_type", "name", "full_name", "row_count"):
        field_value = value.get(key)
        if field_value is None:
            field_value = location.get(key)
        if field_value is not None:
            fact[key] = field_value
    return fact


def _compact_preview_fact(result: dict[str, Any]) -> tuple[str, dict[str, Any]] | None:
    fact = preview_resource_fact(result)
    if fact is None:
        return None
    return fact["locator"], fact


def _compact_formal_resource_fact(value: dict[str, Any], locator: str) -> dict[str, Any]:
    """投影正式 ResourceFacts；不沿任意嵌套对象寻找可持久化事实。"""
    fact: dict[str, Any] = {"locator": locator, "fact_tool": "resource.facts.get"}
    for key in (
        "engine_name", "source_engine_type", "item_type", "data_type", "full_name",
        "item_fingerprint", "scanned_depth", "schema_coverage", "geometry_column", "geometry_type", "crs",
    ):
        if isinstance(value.get(key), str):
            fact[key] = value[key]
    for key in ("engine_id", "item_id"):
        if type(value.get(key)) is int:
            fact[key] = value[key]
    if isinstance(value.get("query_names"), dict):
        fact["query_names"] = {
            key: item for key, item in value["query_names"].items() if isinstance(item, str)
        }
    if isinstance(value.get("fields"), list):
        fact["fields"] = []
        for value_field in value["fields"]:
            if not isinstance(value_field, dict) or not isinstance(value_field.get("name"), str) or not value_field["name"]:
                continue
            field: dict[str, Any] = {"name": value_field["name"]}
            for key in ("type", "element_type", "native_type", "comment"):
                if isinstance(value_field.get(key), str):
                    field[key] = value_field[key]
            for key in ("nullable", "primary_key", "generated"):
                if isinstance(value_field.get(key), bool):
                    field[key] = value_field[key]
            for key in ("size", "precision", "scale", "ordinal_position"):
                if type(value_field.get(key)) is int:
                    field[key] = value_field[key]
            path = value_field.get("path")
            if isinstance(path, list) and all(isinstance(segment, str) for segment in path):
                field["path"] = list(path)
            fact["fields"].append(field)
    return fact


def _merge_resource_fact(
    resources: dict[str, dict[str, Any]],
    locator: str,
    fact: dict[str, Any],
    delta: list[dict[str, Any]],
    *, replace: bool = False,
) -> None:
    if not replace and resources.get(locator, {}).get("fact_tool") == "resource.facts.get":
        return  # Discovery/preview evidence cannot rewrite a formal snapshot.
    merged = fact if replace else {**resources.get(locator, {}), **fact}
    if resources.get(locator) == merged:
        return
    resources[locator] = merged
    delta.append(copy.deepcopy(merged))


def capture_owner_facts(tool_name: str, result: Any, checkpoint: dict[str, Any]) -> dict[str, Any]:
    delta: dict[str, list[dict[str, Any]]] = {"workflow_engines": [], "resources": []}
    observed = checkpoint["observed"]

    if isinstance(result, dict) and isinstance(result.get("error"), dict):
        return {}

    if tool_name == "platform.capability.context" and isinstance(result, dict):
        if not list(Draft202012Validator(get_tool(tool_name).output_schema).iter_errors(result)):
            capability = result["capability"]
            observed["platform_capabilities"][capability] = copy.deepcopy(result)
            return {"platform_capabilities": [copy.deepcopy(result)]}

    if tool_name == "engine.list":
        for value in result if isinstance(result, list) else []:
            if not isinstance(value, dict):
                continue
            engine_id = value.get("id")
            engine_type = value.get("engine_type")
            if type(engine_id) is not int or not isinstance(engine_type, str):
                continue
            key = str(engine_id)
            if key in observed["workflow_engines"]:
                continue
            fact = {
                name: value[name]
                for name in ("id", "name", "engine_type", "lifecycle_state", "connection_status")
                if value.get(name) is not None
            }
            observed["workflow_engines"][key] = fact
            delta["workflow_engines"].append(copy.deepcopy(fact))

    if tool_name in {"data.search", "resource.ancestors.get", "data.preview"}:
        if tool_name == "data.preview" and isinstance(result, dict):
            preview_fact = _compact_preview_fact(result)
            if preview_fact is not None:
                _merge_resource_fact(
                    observed["resources"],
                    preview_fact[0],
                    preview_fact[1],
                    delta["resources"],
                )
        for value in _walk_objects(result):
            locator = resource_locator(value)
            if not locator:
                continue
            fact = _compact_resource_fact(value, locator)
            _merge_resource_fact(observed["resources"], locator, fact, delta["resources"])

    if tool_name == "resource.children.list" and isinstance(result, dict):
        children = result.get("children") if isinstance(result.get("children"), list) else []
        for node in [result, *children]:
            if not isinstance(node, dict):
                continue
            locator = node.get("locator")
            if not isinstance(locator, str) or not locator.startswith("addp://"):
                continue
            fact = {"locator": locator}
            for source, target in (("label", "name"), ("type", "item_type")):
                if isinstance(node.get(source), str):
                    fact[target] = node[source]
            _merge_resource_fact(observed["resources"], locator, fact, delta["resources"])

    if tool_name == "resource.facts.get" and isinstance(result, dict):
        locator = result.get("locator")
        if isinstance(locator, str) and locator.startswith("addp://"):
            fact = _compact_formal_resource_fact(result, locator)
            # A new formal snapshot must not retain fields absent from it.
            _merge_resource_fact(observed["resources"], locator, fact, delta["resources"], replace=True)

    return {key: facts for key, facts in delta.items() if facts}


def canonicalize_clarification_options(
    reason: str,
    options: list[dict[str, Any]],
    checkpoint: dict[str, Any],
) -> list[dict[str, Any]]:
    canonical: list[dict[str, Any]] = []
    observed = checkpoint["observed"]
    if any(isinstance(option.get("candidate"), dict) and "operation_review" in option["candidate"] for option in options):
        raise ValueError("operation_review_requires_runtime_preparation")
    if "workflow_engine" in reason:
        for option in options:
            value = option.get("value")
            try:
                engine_id = int(value)
            except (TypeError, ValueError) as exc:
                raise ValueError(f"工作流引擎选项缺少有效 id: {value}") from exc
            fact = observed["workflow_engines"].get(str(engine_id))
            if fact is None:
                raise ValueError(f"工作流引擎未由 engine.list 返回: {engine_id}")
            label = fact.get("name") or f"workflow engine {engine_id}"
            canonical.append({"label": str(label), "value": engine_id, "candidate": fact})
        return canonical

    has_resource_option = any(
        isinstance(option.get("value"), str) and option["value"].startswith("addp://")
        or isinstance(option.get("candidate"), dict) and resource_locator(option["candidate"]) is not None
        for option in options
    )
    if "data_source" in reason or "resource" in reason or has_resource_option:
        for option in options:
            candidate = option.get("candidate") if isinstance(option.get("candidate"), dict) else {}
            locator = option.get("value") if isinstance(option.get("value"), str) else None
            if not locator or not locator.startswith("addp://"):
                locator = resource_locator(candidate)
            if not locator:
                raise ValueError("数据候选缺少 locator")
            fact = observed["resources"].get(locator)
            if fact is None:
                raise ValueError(f"资源 locator 未由 owner Tool 返回: {locator}")
            label = fact.get("full_name") or fact.get("name") or locator
            canonical.append({"label": str(label), "value": locator, "candidate": fact})
        return canonical

    return options


def confirm_selection(checkpoint: dict[str, Any], answer: Any) -> None:
    if not isinstance(answer, dict):
        return
    candidate = answer.get("candidate") if isinstance(answer.get("candidate"), dict) else {}
    if "operation_review" in candidate:
        review = candidate["operation_review"]
        tool = review.get("tool") if isinstance(review, dict) else None
        if checkpoint["observed"]["operation_reviews"].get(tool) != review:
            raise ValueError("operation_review_not_observed")
        if answer.get("value") == review["fingerprint"]:
            checkpoint["confirmed"]["operation_reviews"][tool] = copy.deepcopy(review)
        else:
            checkpoint["confirmed"]["operation_reviews"].pop(tool, None)
        return
    locator = resource_locator(candidate)
    if locator:
        fact = checkpoint["observed"]["resources"].get(locator)
        if fact is None:
            raise ValueError(f"确认的资源不在已观察事实中: {locator}")
        checkpoint["confirmed"]["resources"][locator] = copy.deepcopy(fact)
        return

    engine_id = candidate.get("id")
    if isinstance(engine_id, int) and isinstance(candidate.get("engine_type"), str):
        fact = checkpoint["observed"]["workflow_engines"].get(str(engine_id))
        if fact is None:
            raise ValueError(f"确认的工作流引擎不在已观察事实中: {engine_id}")
        checkpoint["confirmed"]["workflow_engine"] = copy.deepcopy(fact)


def checkpoint_prompt(checkpoint: dict[str, Any]) -> str:
    observed = checkpoint["observed"]
    confirmed = checkpoint["confirmed"]
    if not any(observed.values()) and not any(confirmed.values()):
        return ""
    return "本 AgentRun 已持久化的受信任状态：\n" + json.dumps(
        {"observed": observed, "confirmed": confirmed},
        ensure_ascii=False,
        separators=(",", ":"),
    )
