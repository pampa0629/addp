import json
import math
from typing import Any


CATALOG_ID = "addp.catalog/v1"
A2UI_VERSION = "v0.9"
TABLE_MAX_COLUMNS = 50
TABLE_MAX_ROWS = 100
MAP_MAX_FEATURES = 200
MAP_MAX_COORDINATE_VALUES = 5000
PRESENTATION_STRING_MAX_LENGTH = 2000
_GEOJSON_GEOMETRY_TYPES = {
    "Point",
    "MultiPoint",
    "LineString",
    "MultiLineString",
    "Polygon",
    "MultiPolygon",
}


def _surface(surface_id: str, component: dict[str, Any]) -> list[dict[str, Any]]:
    return [
        {
            "version": A2UI_VERSION,
            "createSurface": {"surfaceId": surface_id, "catalogId": CATALOG_ID},
        },
        {
            "version": A2UI_VERSION,
            "updateComponents": {
                "surfaceId": surface_id,
                "components": [{"id": "root", **component}],
            },
        },
    ]


def workflow_dag_surface(surface_id: str, workflow: dict[str, Any]) -> list[dict[str, Any]]:
    return _surface(surface_id, {"component": "WorkflowDag", "workflow": workflow, "height": 400})


def clarification_surface(
    surface_id: str,
    *,
    interaction_id: str,
    prompt: str,
    options: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    return _surface(
        surface_id,
        {"component": "ClarificationChoice", "interactionId": interaction_id, "prompt": prompt, "options": options},
    )


def resource_picker_surface(
    surface_id: str,
    *,
    interaction_id: str,
    prompt: str,
    options: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    if not options or len(options) > 50:
        raise ValueError("ResourcePicker options must contain 1 to 50 observed resources")
    projected_options: list[dict[str, Any]] = []
    for option in options:
        value = option.get("value")
        candidate = option.get("candidate") if isinstance(option.get("candidate"), dict) else {}
        if (
            not isinstance(value, str)
            or not value.startswith("addp://")
            or len(value) > 2048
            or candidate.get("locator") != value
        ):
            raise ValueError("ResourcePicker option must use its observed candidate locator")
        projected_candidate: dict[str, Any] = {"locator": value}
        engine_id = candidate.get("engine_id")
        if engine_id is not None:
            if not isinstance(engine_id, int) or isinstance(engine_id, bool) or engine_id <= 0:
                raise ValueError("ResourcePicker candidate engine_id must be a positive integer")
            projected_candidate["engine_id"] = engine_id
        for key in ("name", "full_name", "asset_type", "item_type"):
            candidate_value = candidate.get(key)
            if candidate_value is not None:
                if not isinstance(candidate_value, str):
                    raise ValueError(f"ResourcePicker candidate {key} must be a string")
                projected_candidate[key] = candidate_value[:PRESENTATION_STRING_MAX_LENGTH]
        projected_options.append(
            {
                "label": str(option.get("label") or value)[:500],
                "value": value,
                "candidate": projected_candidate,
            }
        )
    return _surface(
        surface_id,
        {
            "component": "ResourcePicker",
            "interactionId": interaction_id,
            "prompt": prompt[:PRESENTATION_STRING_MAX_LENGTH],
            "options": projected_options,
        },
    )


def approval_request_surface(
    surface_id: str,
    *,
    interaction_id: str,
    owner: str,
    owner_interaction_id: str,
    open_url: str,
    request_fingerprint: str,
    request_summary: dict[str, Any],
    expires_at: str | None,
) -> list[dict[str, Any]]:
    return _surface(
        surface_id,
        {
            "component": "ApprovalRequest",
            "interactionId": interaction_id,
            "owner": owner,
            "ownerInteractionId": owner_interaction_id,
            "openUrl": open_url,
            "requestFingerprint": request_fingerprint,
            "requestSummary": request_summary,
            "expiresAt": expires_at,
        },
    )


def _preview_data(result: Any) -> dict[str, Any] | None:
    if not isinstance(result, dict):
        return None
    data = result.get("data")
    if isinstance(data, dict) and isinstance(data.get("rows"), list):
        return data
    if isinstance(result.get("rows"), list):
        return result
    return None


def _presentation_scalar(value: Any) -> Any:
    if value is None or isinstance(value, bool) or isinstance(value, int):
        return value
    if isinstance(value, float):
        return value if math.isfinite(value) else None
    if isinstance(value, str):
        return value[:PRESENTATION_STRING_MAX_LENGTH]
    return None


def _is_presentation_scalar(value: Any) -> bool:
    return (
        value is None
        or isinstance(value, (bool, int, str))
        or (isinstance(value, float) and math.isfinite(value))
    )


def _coordinate_value_count(value: Any, *, depth: int = 0) -> int | None:
    if depth > 4 or not isinstance(value, list) or not value:
        return None
    count = 0
    for item in value:
        if isinstance(item, bool):
            return None
        if isinstance(item, (int, float)):
            if not math.isfinite(float(item)):
                return None
            count += 1
        else:
            nested = _coordinate_value_count(item, depth=depth + 1)
            if nested is None:
                return None
            count += nested
        if count > MAP_MAX_COORDINATE_VALUES:
            return None
    return count


def preview_presentations(result: Any) -> list[dict[str, Any]]:
    data = _preview_data(result)
    if data is None:
        return []
    raw_columns = data.get("columns")
    raw_rows = data.get("rows")
    if not isinstance(raw_columns, list) or not isinstance(raw_rows, list):
        return []
    columns = [
        column[:200]
        for column in raw_columns
        if isinstance(column, str) and column and len(column) <= 200
    ][:TABLE_MAX_COLUMNS]
    if not columns:
        return []
    rows: list[dict[str, Any]] = []
    for raw_row in raw_rows[:TABLE_MAX_ROWS]:
        if not isinstance(raw_row, dict):
            continue
        rows.append({column: _presentation_scalar(raw_row.get(column)) for column in columns})
    total_value = data.get("total", len(raw_rows))
    total = (
        total_value
        if isinstance(total_value, int) and not isinstance(total_value, bool) and total_value >= 0
        else len(raw_rows)
    )
    truncated = len(raw_columns) > len(columns) or len(raw_rows) > len(rows) or total > len(rows)
    presentations = [
        {
            "kind": "table_preview",
            "columns": columns,
            "rows": rows,
            "total": total,
            "truncated": truncated,
        }
    ]

    geometry_column = data.get("geometry_column")
    source_crs = str(data.get("source_crs") or "").upper()
    if not isinstance(geometry_column, str) or not geometry_column or source_crs != "EPSG:4326":
        return presentations
    features: list[dict[str, Any]] = []
    coordinate_value_count = 0
    for index, raw_row in enumerate(raw_rows[:MAP_MAX_FEATURES]):
        if not isinstance(raw_row, dict):
            continue
        geometry = raw_row.get(geometry_column)
        if isinstance(geometry, str):
            try:
                geometry = json.loads(geometry)
            except (TypeError, ValueError):
                continue
        if isinstance(geometry, dict) and geometry.get("type") == "Feature":
            geometry = geometry.get("geometry")
        geometry_coordinate_count = (
            _coordinate_value_count(geometry.get("coordinates"))
            if isinstance(geometry, dict) and "coordinates" in geometry
            else None
        )
        if (
            not isinstance(geometry, dict)
            or geometry.get("type") not in _GEOJSON_GEOMETRY_TYPES
            or geometry_coordinate_count is None
            or coordinate_value_count + geometry_coordinate_count > MAP_MAX_COORDINATE_VALUES
        ):
            continue
        coordinate_value_count += geometry_coordinate_count
        properties = {
            column: raw_row.get(column)
            for column in columns
            if column != geometry_column and _is_presentation_scalar(raw_row.get(column))
        }
        properties = {key: _presentation_scalar(value) for key, value in properties.items()}
        features.append(
            {
                "type": "Feature",
                "id": index,
                "geometry": {
                    "type": geometry["type"],
                    "coordinates": geometry["coordinates"],
                },
                "properties": properties,
            }
        )
    if features:
        presentations.append(
            {
                "kind": "map_view",
                "crs": "EPSG:4326",
                "features": features,
                "height": 360,
                "truncated": len(raw_rows) > len(features) or total > len(features),
            }
        )
    return presentations


def table_preview_surface(surface_id: str, presentation: dict[str, Any]) -> list[dict[str, Any]]:
    return _surface(
        surface_id,
        {
            "component": "TablePreview",
            "columns": presentation["columns"],
            "rows": presentation["rows"],
            "total": presentation["total"],
            "truncated": presentation["truncated"],
        },
    )


def map_view_surface(surface_id: str, presentation: dict[str, Any]) -> list[dict[str, Any]]:
    return _surface(
        surface_id,
        {
            "component": "MapView",
            "crs": presentation["crs"],
            "features": presentation["features"],
            "height": presentation["height"],
            "truncated": presentation["truncated"],
        },
    )
