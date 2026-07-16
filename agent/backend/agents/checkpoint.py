import copy
import json
from typing import Any


CHECKPOINT_SCHEMA = "addp.agent-checkpoint/v1"


def new_checkpoint() -> dict[str, Any]:
    return {
        "schema": CHECKPOINT_SCHEMA,
        "observed": {"workflow_engines": {}, "resources": {}},
        "confirmed": {"workflow_engine": None, "resources": {}},
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
    metadata = result.get("metadata") if isinstance(result.get("metadata"), dict) else {}
    data = result.get("data") if isinstance(result.get("data"), dict) else {}
    locator = metadata.get("locator")
    if not isinstance(locator, str) or not locator.startswith("addp://"):
        return None
    fact = _compact_resource_fact(metadata, locator)
    for key in (
        "preview_type",
        "geometry_columns",
        "geometry_column",
        "source_srid",
        "source_crs",
        "column_metadata",
        "total",
        "schema",
        "table",
        "engine_type",
    ):
        field_value = result.get(key)
        if field_value is None:
            field_value = data.get(key)
        if field_value is not None:
            fact[key] = copy.deepcopy(field_value)
    return locator, fact


def _merge_resource_fact(
    resources: dict[str, dict[str, Any]],
    locator: str,
    fact: dict[str, Any],
    delta: list[dict[str, Any]],
) -> None:
    merged = {**resources.get(locator, {}), **fact}
    if resources.get(locator) == merged:
        return
    resources[locator] = merged
    delta.append(copy.deepcopy(merged))


def capture_owner_facts(tool_name: str, result: Any, checkpoint: dict[str, Any]) -> dict[str, Any]:
    delta: dict[str, list[dict[str, Any]]] = {"workflow_engines": [], "resources": []}
    observed = checkpoint["observed"]

    if tool_name == "engine.list":
        for value in _walk_objects(result):
            engine_id = value.get("id")
            engine_type = value.get("engine_type") or value.get("type")
            if not isinstance(engine_id, int) or not isinstance(engine_type, str):
                continue
            key = str(engine_id)
            if key in observed["workflow_engines"]:
                continue
            fact = {
                name: value[name]
                for name in ("id", "name", "engine_type", "is_active", "connection_status")
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

    return {key: facts for key, facts in delta.items() if facts}


def canonicalize_clarification_options(
    reason: str,
    options: list[dict[str, Any]],
    checkpoint: dict[str, Any],
) -> list[dict[str, Any]]:
    canonical: list[dict[str, Any]] = []
    observed = checkpoint["observed"]
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

    if "data_source" in reason or "resource" in reason:
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
    if not any((observed["workflow_engines"], observed["resources"], confirmed["workflow_engine"], confirmed["resources"])):
        return ""
    return "本 AgentRun 已持久化的受信任状态：\n" + json.dumps(
        {"observed": observed, "confirmed": confirmed},
        ensure_ascii=False,
        separators=(",", ":"),
    )
