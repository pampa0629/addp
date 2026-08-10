import re
from typing import Any


_GEOMETRY_TYPE_PATTERN = re.compile(r"^(?:geometry|geography)\s*\(\s*([^,)]+)", re.IGNORECASE)


def preview_resource_fact(result: Any) -> dict[str, Any] | None:
    """将 Manager data.preview 结果收敛为可持久化的资源事实。"""
    if not isinstance(result, dict):
        return None
    metadata = result.get("metadata") if isinstance(result.get("metadata"), dict) else {}
    data = result.get("data") if isinstance(result.get("data"), dict) else {}
    locator = metadata.get("locator")
    if not isinstance(locator, str) or not locator.startswith("addp://"):
        return None

    fact: dict[str, Any] = {"locator": locator}
    preview_type = result.get("preview_type")
    if isinstance(preview_type, str) and preview_type:
        fact["preview_type"] = preview_type
    for key in ("engine_name", "resource_type", "full_name", "item_id", "item_fingerprint"):
        value = metadata.get(key)
        if value is not None:
            fact[key] = value

    for key in ("geometry_columns", "geometry_column", "source_srid", "source_crs", "total", "schema", "table"):
        value = data.get(key)
        if value is not None:
            fact[key] = value
    if data.get("engineType") is not None:
        fact["engine_type"] = data["engineType"]

    column_metadata = data.get("column_metadata")
    if isinstance(column_metadata, list):
        compact_columns = [_compact_column(item) for item in column_metadata]
        compact_columns = [item for item in compact_columns if item is not None]
        if compact_columns:
            fact["fields"] = compact_columns

    geometry_type = data.get("geometry_type")
    if not isinstance(geometry_type, str) or not geometry_type.strip():
        geometry_type = _geometry_type_from_fields(fact.get("geometry_column"), fact.get("fields"))
    if geometry_type:
        fact["geometry_type"] = geometry_type
    return fact


def _compact_column(value: Any) -> dict[str, Any] | None:
    if not isinstance(value, dict):
        return None
    name = value.get("column_name")
    if not isinstance(name, str) or not name:
        return None
    field: dict[str, Any] = {"name": name}
    if isinstance(value.get("type"), str) and value["type"]:
        field["type"] = value["type"]
    if isinstance(value.get("nullable"), bool):
        field["nullable"] = value["nullable"]
    if value.get("primary_key") is True:
        field["primary_key"] = True
    if isinstance(value.get("comment"), str) and value["comment"]:
        field["comment"] = value["comment"]
    return field


def _geometry_type_from_fields(geometry_column: Any, fields: Any) -> str | None:
    if not isinstance(geometry_column, str) or not isinstance(fields, list):
        return None
    for field in fields:
        if not isinstance(field, dict) or field.get("name") != geometry_column:
            continue
        field_type = field.get("type")
        if not isinstance(field_type, str):
            return None
        match = _GEOMETRY_TYPE_PATTERN.match(field_type.strip())
        return match.group(1).strip() if match else None
    return None
