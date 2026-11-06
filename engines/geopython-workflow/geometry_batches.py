from __future__ import annotations

from typing import Any, Dict

import geopandas as gpd
import pyarrow as pa
from pyproj import CRS
from pyproj.exceptions import CRSError
import shapely
from shapely.errors import GEOSException

GEOMETRY_BATCH_METADATA_PREFIX = "addp.geometry."
GEOMETRY_BATCH_ENCODING_WKB = "wkb"
GEOMETRY_BATCH_ENCODING_EWKB = "ewkb"


class GeometryBatchInputError(ValueError):
    """A request error identified at the binary geometry boundary."""


def parse_geometry_batch_crs(value: str | None) -> CRS | None:
    if value is None or isinstance(value, str) and not value.strip():
        return None
    if not isinstance(value, str):
        raise GeometryBatchInputError("crs")
    try:
        return CRS.from_user_input(value.strip())
    except CRSError:
        # PROJ also raises CRSError when its database is unavailable. A
        # failed known-good lookup must remain an execution error.
        CRS.from_epsg(4326)
        raise GeometryBatchInputError("crs") from None


def decode_geometry_batch_arrow(
    payload: bytes, *, expected_encoding: str = GEOMETRY_BATCH_ENCODING_EWKB,
    declared_column: str | None = None,
) -> gpd.GeoDataFrame:
    try:
        with pa.ipc.open_stream(pa.BufferReader(payload)) as reader:
            table = reader.read_all()
        metadata = _metadata_to_dict(table.schema.metadata)
    except (pa.ArrowInvalid, pa.ArrowTypeError, pa.ArrowNotImplementedError, UnicodeDecodeError):
        raise GeometryBatchInputError("arrow") from None
    if table.num_columns != 1 or table.schema.field(0).type != pa.binary():
        raise GeometryBatchInputError("column")
    column_name = table.schema.names[0]
    arrow_column = metadata.get(f"{GEOMETRY_BATCH_METADATA_PREFIX}column", column_name)
    if not column_name.strip() or arrow_column != column_name or (declared_column is not None and declared_column != column_name):
        raise GeometryBatchInputError("column")
    encoding = metadata.get(f"{GEOMETRY_BATCH_METADATA_PREFIX}encoding", "").strip().lower()
    if encoding != expected_encoding:
        raise GeometryBatchInputError("arrow_encoding")
    crs = parse_geometry_batch_crs(metadata.get(f"{GEOMETRY_BATCH_METADATA_PREFIX}source_crs"))
    try:
        values = shapely.from_wkb(table.column(0).to_pylist())
    except GEOSException:
        raise GeometryBatchInputError("ewkb") from None
    geometry = gpd.GeoSeries(values, crs=crs)
    return gpd.GeoDataFrame({column_name: geometry}, geometry=column_name, crs=crs)


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
