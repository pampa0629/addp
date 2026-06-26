from __future__ import annotations

from typing import Any, Dict

import geopandas as gpd
import pyarrow as pa
from pyproj import CRS
import shapely

GEOMETRY_BATCH_METADATA_PREFIX = "addp.geometry."
GEOMETRY_BATCH_ENCODING_WKB = "wkb"
GEOMETRY_BATCH_ENCODING_EWKB = "ewkb"


def decode_geometry_batch_arrow(payload: bytes) -> gpd.GeoDataFrame:
    if not payload:
        raise ValueError("binary_payload.data is required")

    reader = pa.ipc.open_stream(pa.BufferReader(payload))
    table = reader.read_all()
    if table.num_columns != 1:
        raise ValueError("geometry batch arrow payload must contain exactly one column")
    column_name = table.schema.names[0]
    metadata = _metadata_to_dict(table.schema.metadata)
    geometry_column = metadata.get(f"{GEOMETRY_BATCH_METADATA_PREFIX}column") or column_name
    encoding = _normalize_encoding(metadata.get(f"{GEOMETRY_BATCH_METADATA_PREFIX}encoding"))
    if encoding not in {GEOMETRY_BATCH_ENCODING_WKB, GEOMETRY_BATCH_ENCODING_EWKB}:
        raise ValueError(f"unsupported geometry batch encoding: {encoding}")
    source_crs = metadata.get(f"{GEOMETRY_BATCH_METADATA_PREFIX}source_crs", "")
    values = table.column(0).to_pylist()

    geometry = gpd.GeoSeries.from_wkb(values, crs=source_crs or None)
    frame = gpd.GeoDataFrame({geometry_column: geometry}, geometry=geometry_column, crs=source_crs or None)
    return frame


def geometry_batch_arrow_metadata(payload: bytes) -> Dict[str, Any]:
    if not payload:
        raise ValueError("binary_payload.data is required")
    reader = pa.ipc.open_stream(pa.BufferReader(payload))
    table = reader.read_all()
    return _metadata_to_dict(table.schema.metadata)


def encode_geometry_batch_arrow(
    gdf: gpd.GeoDataFrame,
    *,
    geometry_column: str | None = None,
    source_crs: str = "",
    target_crs: str = "",
    geometry_encoding: str = GEOMETRY_BATCH_ENCODING_EWKB,
) -> bytes:
    if gdf is None:
        raise ValueError("geometry batch is required")
    if not isinstance(gdf, gpd.GeoDataFrame):
        raise ValueError("geometry batch must be a GeoDataFrame")

    column_name = geometry_column or gdf.geometry.name or "geometry"
    normalized_encoding = _normalize_encoding(geometry_encoding)
    geometry = gdf.geometry
    if normalized_encoding == GEOMETRY_BATCH_ENCODING_EWKB:
        srid = _srid_from_crs(gdf.crs) or _srid_from_crs(source_crs) or _srid_from_crs(target_crs)
        if srid > 0:
            geometry = gpd.GeoSeries(
                [
                    shapely.set_srid(value, srid) if value is not None else None
                    for value in geometry
                ],
                crs=gdf.crs,
            )
    values = list(geometry.to_wkb(hex=False, include_srid=normalized_encoding == GEOMETRY_BATCH_ENCODING_EWKB))
    array = pa.array(values, type=pa.binary())
    metadata = {
        f"{GEOMETRY_BATCH_METADATA_PREFIX}column": column_name,
        f"{GEOMETRY_BATCH_METADATA_PREFIX}encoding": normalized_encoding,
        f"{GEOMETRY_BATCH_METADATA_PREFIX}source_crs": source_crs or _crs_text(gdf.crs),
        f"{GEOMETRY_BATCH_METADATA_PREFIX}target_crs": target_crs or _crs_text(gdf.crs),
    }
    schema = pa.schema([(column_name, pa.binary())], metadata=metadata)
    table = pa.Table.from_arrays([array], schema=schema)

    sink = pa.BufferOutputStream()
    writer = pa.ipc.new_stream(sink, schema)
    writer.write_table(table)
    writer.close()
    return sink.getvalue().to_pybytes()


def _metadata_to_dict(metadata: pa.KeyValueMetadata | None) -> Dict[str, Any]:
    if metadata is None:
        return {}
    result: Dict[str, Any] = {}
    for key, value in metadata.items():
        key_text = key.decode("utf-8") if isinstance(key, (bytes, bytearray)) else str(key)
        value_text = value.decode("utf-8") if isinstance(value, (bytes, bytearray)) else str(value)
        result[key_text] = value_text
    return result


def _normalize_encoding(value: Any) -> str:
    text = str(value or "").strip().lower()
    if not text:
        return GEOMETRY_BATCH_ENCODING_EWKB
    if text in {GEOMETRY_BATCH_ENCODING_WKB, GEOMETRY_BATCH_ENCODING_EWKB}:
        return text
    raise ValueError(f"unsupported geometry batch encoding: {text}")


def _crs_text(value: Any) -> str:
    if value is None:
        return ""
    try:
        crs = CRS.from_user_input(value)
    except Exception:
        return str(value).strip()
    return crs.to_string()


def _srid_from_crs(value: Any) -> int:
    if value is None:
        return 0
    try:
        epsg = CRS.from_user_input(value).to_epsg()
    except Exception:
        return 0
    return int(epsg or 0)
