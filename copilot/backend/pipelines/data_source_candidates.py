"""Data source candidate builders for Copilot workflow generation."""

from typing import Any

from models.workflow_models import DataSourceCandidate, DataSourceLocation


RELATIONAL_ENGINE_TYPES = {"postgresql", "mysql", "doris", "clickhouse"}
OBJECT_ENGINE_TYPES = {"minio", "s3", "oss"}


def metadata_search_query(query: str, analysis: Any) -> str:
    """Return the most specific metadata search query available."""
    for attr in ("table_name", "file_path", "bucket_name"):
        value = getattr(analysis, attr, None)
        if value:
            return str(value)
    return query


def metadata_type_filter(analysis: Any) -> str | None:
    if getattr(analysis, "table_name", None):
        return "table"
    if getattr(analysis, "bucket_name", None) or getattr(analysis, "file_path", None):
        return "file"
    return None


def build_data_source_candidates(
    metadata_results: list[dict[str, Any]],
    engines: list[dict[str, Any]],
    default_namespace: str | None = None,
    max_candidates: int = 5,
) -> list[DataSourceCandidate]:
    engine_by_id = {
        int(engine["id"]): engine
        for engine in engines
        if engine.get("id") is not None
    }

    candidates: list[DataSourceCandidate] = []
    for result in metadata_results:
        candidate = _build_candidate(result, engine_by_id, default_namespace)
        if candidate is not None:
            candidates.append(candidate)
        if len(candidates) >= max_candidates:
            break

    return candidates


def _build_candidate(
    result: dict[str, Any],
    engine_by_id: dict[int, dict[str, Any]],
    default_namespace: str | None,
) -> DataSourceCandidate | None:
    engine_id = _to_int(result.get("engine_id"))
    if engine_id is None:
        return None

    engine = engine_by_id.get(engine_id)
    if engine is None:
        return None
    engine_type = _first_value(result, "engine_type") or engine.get("type") or engine.get("engine_type")
    if not engine_type:
        return None

    location = _build_location(result, engine_id, str(engine_type), default_namespace)
    if location is None:
        return None
    resource_name = _first_value(result, "name", "file_name", "title")
    if not resource_name:
        return None

    return DataSourceCandidate(
        engine_id=engine_id,
        engine_name=engine.get("name") or _first_value(result, "engine_name"),
        engine_type=str(engine_type),
        resource_name=str(resource_name),
        location=location,
        confidence=_normalize_score(result.get("score")),
        reason=_first_value(result, "reason") or "元数据搜索匹配",
    )


def _build_location(
    result: dict[str, Any],
    engine_id: int,
    engine_type: str,
    default_namespace: str | None,
) -> DataSourceLocation | None:
    result_type = str(result.get("type") or "").lower()
    locator = _first_value(result, "locator")
    target_parent_locator = _first_value(result, "target_parent_locator")
    if not locator or not target_parent_locator:
        return None

    if engine_type in RELATIONAL_ENGINE_TYPES:
        table = _first_value(result, "table", "table_name")
        if not table and result_type in {"table", "view"}:
            table = _first_value(result, "name")
        if not table:
            return None

        namespace = _first_value(result, "namespace", "schema", "schema_name") or default_namespace or "public"
        return DataSourceLocation(
            namespace=str(namespace),
            table=str(table),
            locator=str(locator),
            target_parent_locator=str(target_parent_locator),
        )

    if engine_type in OBJECT_ENGINE_TYPES:
        bucket = _first_value(result, "bucket", "bucket_name")
        path = _first_value(result, "path", "object_path", "file_path")
        if not path and result_type in {"file", "object"}:
            path = _first_value(result, "name")
        if not bucket and not path:
            return None

        return DataSourceLocation(
            bucket=str(bucket) if bucket else None,
            path=str(path) if path else None,
            locator=str(locator),
            target_parent_locator=str(target_parent_locator),
        )

    return DataSourceLocation(
        resource_identifier=str(locator),
        locator=str(locator),
        target_parent_locator=str(target_parent_locator),
    )


def _first_value(result: dict[str, Any], *keys: str) -> Any:
    metadata = result.get("metadata")
    if not isinstance(metadata, dict):
        metadata = {}

    for key in keys:
        value = result.get(key)
        if value not in (None, ""):
            return value

        value = metadata.get(key)
        if value not in (None, ""):
            return value

    return None


def _normalize_score(value: Any) -> float | None:
    if value is None:
        return None
    try:
        score = float(value)
    except (TypeError, ValueError):
        return None
    return max(0.0, min(1.0, score))


def _to_int(value: Any) -> int | None:
    try:
        return int(value)
    except (TypeError, ValueError):
        return None
