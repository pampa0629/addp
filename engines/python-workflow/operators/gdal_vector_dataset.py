"""Stateless GDAL vector dataset batch operators for format providers."""

from __future__ import annotations

import base64
import datetime as dt
import json
import math
import shutil
import sys
import tempfile
from pathlib import Path
from typing import Any

from osgeo import gdal, ogr, osr
from shapely import from_wkb, set_srid, to_wkb

from addp_common.workflow_access import (
    WorkflowAccessError,
    require_source_plan,
    require_target_plan,
    stage_source_directory,
    stage_source_file,
)

from .base import OperatorCategory, OperatorMetadata, OperatorParam, OperatorType, register_operator


BATCH_PROTOCOL = "gdal.vector-batch/v1"
INSPECTION_SCHEMA = "gdal.vector-dataset.inspect/v1"
INSPECT_OPERATORS = ("vector_dataset.inspect",)
READ_OPERATORS = (
    "vector_dataset.read_open",
    "vector_dataset.read_batch",
    "vector_dataset.read_close",
)
WRITE_OPERATORS = (
    "vector_dataset.write_open",
    "vector_dataset.write_batch",
    "vector_dataset.write_close",
    "vector_dataset.write_abort",
)

gdal.UseExceptions()


def inspect(access_plan: dict[str, Any], child_limit: int = 100) -> dict[str, Any]:
    if isinstance(child_limit, bool) or not isinstance(child_limit, int) or child_limit <= 0 or child_limit > 1000:
        raise ValueError("child_limit must be an integer between 1 and 1000")
    plan = require_source_plan({"access_plan": access_plan})
    with _opened_source(plan) as dataset:
        layer_count = dataset.GetLayerCount()
        children: list[dict[str, Any]] = []
        valid_child_count = 0
        skipped_layer_count = 0
        for index in range(layer_count):
            try:
                layer = dataset.GetLayerByIndex(index)
                if layer is None:
                    skipped_layer_count += 1
                    continue
                fields, spatial = _describe_layer(layer)
                row_count = layer.GetFeatureCount(force=1)
            except RuntimeError:
                skipped_layer_count += 1
                continue
            valid_child_count += 1
            if len(children) >= child_limit:
                continue
            geometry = _primary_geometry(spatial)
            native: dict[str, Any] = {"table": layer.GetName()}
            if geometry:
                native.update({
                    "geometry_column": geometry.get("name"),
                    "geometry_type": geometry.get("geometry_type"),
                    "dimension": geometry.get("dimension"),
                })
                if geometry.get("srid"):
                    native["srid"] = geometry["srid"]
            children.append({
                "name": layer.GetName(),
                "child_kind": "feature_class" if geometry else "table",
                "data_type": "table",
                "row_count": row_count,
                "column_count": len(fields),
                "native": native,
            })
        driver = dataset.GetDriver()
        source_format = plan["source"]["format"]
        return {
            "schema_version": INSPECTION_SCHEMA,
            "format": source_format,
            "container": {
                "child_count": valid_child_count,
                "default_child": children[0]["name"] if children else "",
                "resource_count": 1,
                "children": children,
            },
            "format_info": {
                "driver": driver.GetDescription() if driver is not None else "",
                "gdal_version": gdal.VersionInfo("RELEASE_NAME"),
                "layer_count": layer_count,
                "skipped_layer_count": skipped_layer_count,
                "children_truncated": valid_child_count > len(children),
            },
        }


def read_open(protocol: str, access_plan: dict[str, Any], layer: str) -> dict[str, Any]:
    _require_protocol(protocol)
    plan = require_source_plan({"access_plan": access_plan})
    with _opened_source(plan) as dataset:
        source_layer = _required_layer(dataset, layer)
        fields, spatial = _describe_layer(source_layer)
        return {
            "protocol": BATCH_PROTOCOL,
            "fields": fields,
            "spatial": spatial,
            "row_count": source_layer.GetFeatureCount(force=1),
        }


def read_batch(
    protocol: str,
    access_plan: dict[str, Any],
    layer: str,
    offset: int,
    limit: int,
) -> dict[str, Any]:
    _require_protocol(protocol)
    if isinstance(offset, bool) or not isinstance(offset, int) or offset < 0:
        raise ValueError("offset must be a non-negative integer")
    if isinstance(limit, bool) or not isinstance(limit, int) or limit <= 0:
        raise ValueError("limit must be a positive integer")
    plan = require_source_plan({"access_plan": access_plan})
    with _opened_source(plan) as dataset:
        source_layer = _required_layer(dataset, layer)
        fields, spatial = _describe_layer(source_layer)
        if source_layer.SetNextByIndex(offset) != ogr.OGRERR_NONE:
            source_layer.ResetReading()
            for _ in range(offset):
                if source_layer.GetNextFeature() is None:
                    return {"protocol": BATCH_PROTOCOL, "rows": []}
        geometry_field = spatial.get("primary_geometry_column")
        srid = spatial.get("srid") or 0
        rows: list[dict[str, Any]] = []
        for _ in range(limit):
            feature = source_layer.GetNextFeature()
            if feature is None:
                break
            rows.append(_feature_row(feature, fields, geometry_field, srid))
        return {"protocol": BATCH_PROTOCOL, "rows": rows}


def read_close(protocol: str, access_plan: dict[str, Any], layer: str) -> dict[str, Any]:
    _require_protocol(protocol)
    require_source_plan({"access_plan": access_plan})
    _require_text(layer, "layer")
    return {"protocol": BATCH_PROTOCOL, "closed": True}


def write_open(
    protocol: str,
    access_plan: dict[str, Any],
    layer: str,
    fields: list[dict[str, Any]],
    spatial: dict[str, Any] | None = None,
) -> dict[str, Any]:
    _require_protocol(protocol)
    plan = require_target_plan({"access_plan": access_plan})
    target = plan["target"]
    if target.get("format") != "filegdb":
        raise ValueError("GDAL vector writer supports filegdb target only")
    path = _mounted_target_path(target)
    if path.exists():
        if target["write_mode"] == "create":
            raise WorkflowAccessError(f"target directory already exists: {path}")
        shutil.rmtree(path)
    path.parent.mkdir(parents=True, exist_ok=True)

    driver = _required_driver("OpenFileGDB", writable=True)
    dataset = driver.Create(str(path), 0, 0, 0, gdal.GDT_Unknown)
    if dataset is None:
        raise RuntimeError(f"create FileGDB failed: {path}")
    try:
        layer_name = _require_text(layer, "layer")
        spatial = spatial if isinstance(spatial, dict) else {}
        primary_geometry = _primary_geometry(spatial)
        geometry_type = _ogr_geometry_type(primary_geometry.get("geometry_type")) if primary_geometry else ogr.wkbNone
        target_layer = dataset.CreateLayer(layer_name, srs=_spatial_reference(spatial), geom_type=geometry_type)
        if target_layer is None:
            raise RuntimeError(f"create FileGDB layer failed: {layer_name}")
        geometry_field = primary_geometry.get("name")
        for field in fields:
            if not isinstance(field, dict) or field.get("type") == "geometry" or field.get("name") == geometry_field:
                continue
            if target_layer.CreateField(_ogr_field_definition(field)) != ogr.OGRERR_NONE:
                raise RuntimeError(f"create FileGDB field failed: {field.get('name')}")
    finally:
        dataset = None
    return {"protocol": BATCH_PROTOCOL, "created": True, "path": str(path)}


def write_batch(
    protocol: str,
    access_plan: dict[str, Any],
    layer: str,
    fields: list[dict[str, Any]],
    spatial: dict[str, Any] | None,
    offset: int,
    rows: list[dict[str, Any]],
) -> dict[str, Any]:
    _require_protocol(protocol)
    plan = require_target_plan({"access_plan": access_plan})
    path = _mounted_target_path(plan["target"])
    dataset = gdal.OpenEx(str(path), gdal.OF_VECTOR | gdal.OF_UPDATE, allowed_drivers=["OpenFileGDB"])
    if dataset is None:
        raise RuntimeError(f"open FileGDB target failed: {path}")
    try:
        target_layer = _required_layer(dataset, layer)
        current_count = target_layer.GetFeatureCount(force=1)
        if current_count != offset:
            raise RuntimeError(f"FileGDB target row count {current_count} does not match batch offset {offset}")
        geometry_field = _primary_geometry(spatial or {}).get("name")
        srs = _spatial_reference(spatial or {})
        if target_layer.StartTransaction() != ogr.OGRERR_NONE:
            raise RuntimeError("start FileGDB batch transaction failed")
        try:
            for row in rows:
                _append_feature(target_layer, row, fields, geometry_field, srs)
            if target_layer.CommitTransaction() != ogr.OGRERR_NONE:
                raise RuntimeError("commit FileGDB batch transaction failed")
        except Exception:
            target_layer.RollbackTransaction()
            raise
    finally:
        dataset = None
    return {"protocol": BATCH_PROTOCOL, "written": len(rows), "offset": offset}


def write_close(
    protocol: str,
    access_plan: dict[str, Any],
    layer: str,
    fields: list[dict[str, Any]],
    spatial: dict[str, Any] | None,
    expected_row_count: int,
) -> dict[str, Any]:
    del fields, spatial
    _require_protocol(protocol)
    plan = require_target_plan({"access_plan": access_plan})
    path = _mounted_target_path(plan["target"])
    dataset = gdal.OpenEx(str(path), gdal.OF_VECTOR, allowed_drivers=["OpenFileGDB"])
    if dataset is None:
        raise RuntimeError(f"open completed FileGDB failed: {path}")
    try:
        target_layer = _required_layer(dataset, layer)
        row_count = target_layer.GetFeatureCount(force=1)
        if row_count != expected_row_count:
            raise RuntimeError(f"FileGDB target row count {row_count} does not match expected {expected_row_count}")
        extent = _layer_extent(target_layer)
    finally:
        dataset = None
    return {"protocol": BATCH_PROTOCOL, "closed": True, "row_count": row_count, "extent": extent}


def write_abort(
    protocol: str,
    access_plan: dict[str, Any],
    layer: str,
    fields: list[dict[str, Any]],
    spatial: dict[str, Any] | None,
) -> dict[str, Any]:
    del layer, fields, spatial
    _require_protocol(protocol)
    plan = require_target_plan({"access_plan": access_plan})
    path = _mounted_target_path(plan["target"])
    if path.exists():
        shutil.rmtree(path)
    return {"protocol": BATCH_PROTOCOL, "aborted": True}


class _opened_source:
    def __init__(self, plan: dict[str, Any]):
        self.plan = plan
        self.temp_dir: tempfile.TemporaryDirectory[str] | None = None
        self.dataset = None

    def __enter__(self):
        source = self.plan["source"]
        self.temp_dir = tempfile.TemporaryDirectory(prefix="addp-gdal-vector-")
        work_dir = Path(self.temp_dir.name)
        path = stage_source_directory(self.plan, work_dir) if source.get("kind") == "directory" else stage_source_file(self.plan, work_dir)
        driver_name = {"filegdb": "OpenFileGDB", "pgeo": "PGeo"}.get(source.get("format"))
        if not driver_name:
            raise ValueError(f"unsupported GDAL vector source format: {source.get('format')}")
        _required_driver(driver_name, writable=False)
        self.dataset = gdal.OpenEx(str(path), gdal.OF_VECTOR, allowed_drivers=[driver_name])
        if self.dataset is None:
            raise RuntimeError(f"open {source.get('format')} dataset failed: {path}")
        return self.dataset

    def __exit__(self, exc_type, exc_value, traceback):
        self.dataset = None
        if self.temp_dir is not None:
            self.temp_dir.cleanup()


def _required_driver(name: str, *, writable: bool):
    driver = gdal.GetDriverByName(name)
    if driver is None:
        raise RuntimeError(f"GDAL driver is unavailable: {name}")
    if writable and driver.GetMetadataItem(gdal.DCAP_CREATE) != "YES":
        raise RuntimeError(f"GDAL driver is not writable: {name}")
    return driver


def _required_layer(dataset, name: str):
    layer_name = _require_text(name, "layer")
    layer = dataset.GetLayerByName(layer_name)
    if layer is None:
        available = [dataset.GetLayerByIndex(index).GetName() for index in range(dataset.GetLayerCount())]
        raise ValueError(f"layer not found: {layer_name}; available={available}")
    return layer


def _describe_layer(layer) -> tuple[list[dict[str, Any]], dict[str, Any]]:
    definition = layer.GetLayerDefn()
    fields = [_field_info(definition.GetFieldDefn(index), index + 1) for index in range(definition.GetFieldCount())]
    if ogr.GT_Flatten(definition.GetGeomType()) == ogr.wkbNone:
        return fields, {}
    geometry_name = layer.GetGeometryColumn() or "geometry"
    geometry_type = _geometry_type_name(definition.GetGeomType())
    fields.append({
        "name": geometry_name,
        "type": "geometry",
        "native_type": ogr.GeometryTypeToName(definition.GetGeomType()),
        "nullable": True,
        "ordinal_position": len(fields) + 1,
    })
    srs = layer.GetSpatialRef()
    srid = _authority_code(srs)
    geometry_column: dict[str, Any] = {
        "name": geometry_name,
        "geometry_type": geometry_type,
        "dimension": 3 if ogr.GT_HasZ(definition.GetGeomType()) else 2,
        "nullable": True,
    }
    spatial: dict[str, Any] = {
        "geometry_columns": [geometry_column],
        "primary_geometry_column": geometry_name,
        "extent": _layer_extent(layer),
    }
    if srid > 0:
        spatial["srid"] = srid
        spatial["crs_ref"] = f"epsg:{srid}"
        geometry_column["srid"] = srid
        geometry_column["crs_ref"] = f"epsg:{srid}"
    if srs is not None:
        definition_text = srs.ExportToWkt()
        if definition_text:
            crs_id = spatial.get("crs_ref") or "gdal:wkt"
            spatial["crs_ref"] = crs_id
            geometry_column["crs_ref"] = crs_id
            spatial["crs_definitions"] = [{
                "id": crs_id,
                "definition_encoding": "wkt",
                "definition": definition_text,
                "source": "gdal_vector_dataset",
            }]
    return fields, spatial


def _field_info(definition, ordinal: int) -> dict[str, Any]:
    field_type = definition.GetType()
    result = {
        "name": definition.GetName(),
        "type": _addp_field_type(field_type, definition.GetSubType()),
        "native_type": definition.GetFieldTypeName(field_type),
        "nullable": bool(definition.IsNullable()),
        "ordinal_position": ordinal,
    }
    if definition.GetWidth() > 0:
        result["size"] = definition.GetWidth()
        result["precision"] = definition.GetWidth()
    if definition.GetPrecision() > 0:
        result["scale"] = definition.GetPrecision()
    return result


def _addp_field_type(field_type: int, subtype: int) -> str:
    if subtype == ogr.OFSTBoolean:
        return "bool"
    return {
        ogr.OFTInteger: "int",
        ogr.OFTInteger64: "bigint",
        ogr.OFTReal: "double",
        ogr.OFTBinary: "bytes",
        ogr.OFTDate: "date",
        ogr.OFTTime: "time",
        ogr.OFTDateTime: "timestamp",
        ogr.OFTStringList: "array",
        ogr.OFTIntegerList: "array",
        ogr.OFTInteger64List: "array",
        ogr.OFTRealList: "array",
    }.get(field_type, "string")


def _feature_row(feature, fields: list[dict[str, Any]], geometry_field: str | None, srid: int) -> dict[str, Any]:
    row: dict[str, Any] = {}
    for field in fields:
        name = field["name"]
        if field["type"] == "geometry":
            continue
        index = feature.GetFieldIndex(name)
        if index < 0 or not feature.IsFieldSetAndNotNull(index):
            row[name] = None
            continue
        value = feature.GetField(index)
        if isinstance(value, (dt.date, dt.datetime, dt.time)):
            value = value.isoformat()
        elif field["type"] == "bytes" and isinstance(value, (bytes, bytearray, memoryview)):
            value = base64.b64encode(bytes(value)).decode("ascii")
        row[name] = value
    geometry = feature.GetGeometryRef()
    if geometry_field:
        row[geometry_field] = (
            None
            if _is_missing_geometry(geometry)
            else base64.b64encode(_ewkb(geometry, srid)).decode("ascii")
        )
    return row


def _ewkb(geometry, srid: int) -> bytes:
    shape = from_wkb(bytes(geometry.ExportToWkb()))
    if srid > 0:
        shape = set_srid(shape, srid)
    return to_wkb(shape, hex=False, include_srid=srid > 0, flavor="extended", output_dimension=3)


def _append_feature(layer, row: dict[str, Any], fields: list[dict[str, Any]], geometry_field: str | None, srs) -> None:
    feature = ogr.Feature(layer.GetLayerDefn())
    try:
        for field in fields:
            name = field.get("name")
            if not name or field.get("type") == "geometry" or name == geometry_field:
                continue
            value = row.get(name)
            if value is None:
                continue
            if field.get("type") in {"json", "array"} and not isinstance(value, str):
                value = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
            elif field.get("type") == "bytes" and isinstance(value, str):
                value = base64.b64decode(value)
            feature.SetField(name, value)
        if geometry_field and row.get(geometry_field) is not None:
            encoded = row[geometry_field]
            ewkb = base64.b64decode(encoded) if isinstance(encoded, str) else bytes(encoded)
            shape = from_wkb(ewkb)
            geometry = ogr.CreateGeometryFromWkb(to_wkb(shape, hex=False, include_srid=False, flavor="iso", output_dimension=3))
            if srs is not None:
                geometry.AssignSpatialReference(srs)
            feature.SetGeometry(geometry)
        if layer.CreateFeature(feature) != ogr.OGRERR_NONE:
            raise RuntimeError("append FileGDB feature failed")
    finally:
        feature = None


def _ogr_field_definition(field: dict[str, Any]):
    name = _require_text(field.get("name"), "field.name")
    field_type = {
        "bool": ogr.OFTInteger,
        "int": ogr.OFTInteger,
        "bigint": ogr.OFTInteger64,
        "float": ogr.OFTReal,
        "double": ogr.OFTReal,
        "decimal": ogr.OFTReal,
        "bytes": ogr.OFTBinary,
        "date": ogr.OFTDate,
        "time": ogr.OFTTime,
        "timestamp": ogr.OFTDateTime,
    }.get(field.get("type"), ogr.OFTString)
    definition = ogr.FieldDefn(name, field_type)
    if field.get("type") == "bool":
        definition.SetSubType(ogr.OFSTBoolean)
    if isinstance(field.get("size"), int) and field["size"] > 0:
        definition.SetWidth(field["size"])
    if isinstance(field.get("scale"), int) and field["scale"] > 0:
        definition.SetPrecision(field["scale"])
    definition.SetNullable(bool(field.get("nullable", True)))
    return definition


def _primary_geometry(spatial: dict[str, Any]) -> dict[str, Any]:
    columns = spatial.get("geometry_columns")
    if not isinstance(columns, list):
        return {}
    primary = spatial.get("primary_geometry_column")
    for column in columns:
        if isinstance(column, dict) and column.get("name") == primary:
            return column
    return columns[0] if columns and isinstance(columns[0], dict) else {}


def _spatial_reference(spatial: dict[str, Any]):
    srid = spatial.get("srid") or _primary_geometry(spatial).get("srid")
    reference = osr.SpatialReference()
    if isinstance(srid, int) and srid > 0 and reference.ImportFromEPSG(srid) == ogr.OGRERR_NONE:
        return reference
    definitions = spatial.get("crs_definitions")
    if isinstance(definitions, list):
        for definition in definitions:
            if isinstance(definition, dict) and definition.get("definition"):
                if reference.ImportFromWkt(definition["definition"]) == ogr.OGRERR_NONE:
                    return reference
    return None


def _authority_code(srs) -> int:
    if srs is None:
        return 0
    try:
        code = srs.GetAuthorityCode(None)
        if code:
            return int(code)
    except (TypeError, ValueError, RuntimeError):
        pass
    try:
        srs.AutoIdentifyEPSG()
    except RuntimeError:
        pass
    try:
        code = srs.GetAuthorityCode(None)
        if code:
            return int(code)
        for match, confidence in srs.FindMatches():
            if confidence != 100 or match.GetAuthorityName(None) != "EPSG":
                continue
            code = match.GetAuthorityCode(None)
            if code:
                return int(code)
        return 0
    except (TypeError, ValueError, RuntimeError):
        return 0


def _is_missing_geometry(geometry) -> bool:
    if geometry is None or geometry.IsEmpty():
        return True
    envelope = geometry.GetEnvelope()
    return any(
        not math.isfinite(value) or abs(value) >= sys.float_info.max / 2
        for value in envelope
    )


def _geometry_type_name(geometry_type: int) -> str:
    flat = ogr.GT_Flatten(geometry_type)
    return {
        ogr.wkbPoint: "Point",
        ogr.wkbLineString: "LineString",
        ogr.wkbPolygon: "Polygon",
        ogr.wkbMultiPoint: "MultiPoint",
        ogr.wkbMultiLineString: "MultiLineString",
        ogr.wkbMultiPolygon: "MultiPolygon",
        ogr.wkbGeometryCollection: "GeometryCollection",
    }.get(flat, "Geometry")


def _ogr_geometry_type(name: Any) -> int:
    normalized = str(name or "Geometry").replace(" ", "").lower()
    return {
        "point": ogr.wkbPoint,
        "linestring": ogr.wkbLineString,
        "polygon": ogr.wkbPolygon,
        "multipoint": ogr.wkbMultiPoint,
        "multilinestring": ogr.wkbMultiLineString,
        "multipolygon": ogr.wkbMultiPolygon,
        "geometrycollection": ogr.wkbGeometryCollection,
    }.get(normalized, ogr.wkbUnknown)


def _layer_extent(layer) -> list[float]:
    extent = layer.GetExtent(force=1)
    return [] if extent is None else [extent[0], extent[2], extent[1], extent[3]]


def _mounted_target_path(target: dict[str, Any]) -> Path:
    access = target.get("access")
    if not isinstance(access, dict) or access.get("method") != "mounted_path":
        raise WorkflowAccessError("stateless GDAL vector writer currently requires mounted_path target")
    path = Path(_require_text(access.get("path"), "access_plan.target.access.path"))
    if path.suffix.lower() != ".gdb":
        raise WorkflowAccessError("FileGDB target path must end with .gdb")
    return path


def _require_protocol(protocol: str) -> None:
    if protocol != BATCH_PROTOCOL:
        raise ValueError(f"protocol must be {BATCH_PROTOCOL}")


def _require_text(value: Any, name: str) -> str:
    text = value.strip() if isinstance(value, str) else ""
    if not text:
        raise ValueError(f"{name} is required")
    return text


def _param(name: str, data_type: str, description: str, *, required: bool = True, default: Any = None) -> OperatorParam:
    return OperatorParam(
        name=name,
        type="param",
        data_type=data_type,
        required=required,
        default=default,
        description=description,
    )


def _metadata(name: str, effects: list[str], params: list[OperatorParam]) -> OperatorMetadata:
    return OperatorMetadata(
        name=name,
        type=OperatorType.GENERAL,
        category=OperatorCategory.DATA_IO,
        description=name,
        brief_description="GDAL vector dataset batch protocol",
        execution_modes=["direct"],
        effects=effects,
        overview="由 ADDP 格式 Provider 调用的 GDAL vector dataset 批处理算子。",
        params=params,
        use_cases=["FileGDB child 读取", "FileGDB child 写出", "PGeo child 只读"],
        notes=["仅供受控 direct 调用", "geometry 固定 EWKB", "Runtime 不解析 locator"],
        workflow_example={},
        attributes={"protocol": BATCH_PROTOCOL, "runtime_session": "stateless"},
    )


_ACCESS_PLAN_PARAM = _param("access_plan", "object", "addp.workflow.access-plan/v1")
_PROTOCOL_PARAM = _param("protocol", "string", BATCH_PROTOCOL)
_LAYER_PARAM = _param("layer", "string", "由容器 child 解析得到的真实入口")
_FIELDS_PARAM = _param("fields", "object", "ADDP FieldInfo 数组")
_SPATIAL_PARAM = _param("spatial", "object", "ADDP SpatialInfo", required=False)


OPERATORS = dict([
    register_operator(_metadata(INSPECT_OPERATORS[0], ["read"], [
        _ACCESS_PLAN_PARAM,
        _param("child_limit", "int", "最多返回的轻量 child 数量", required=False, default=100),
    ]), inspect),
    register_operator(_metadata(READ_OPERATORS[0], ["read"], [_PROTOCOL_PARAM, _ACCESS_PLAN_PARAM, _LAYER_PARAM]), read_open),
    register_operator(_metadata(READ_OPERATORS[1], ["read"], [
        _PROTOCOL_PARAM, _ACCESS_PLAN_PARAM, _LAYER_PARAM,
        _param("offset", "int", "逻辑起始行"),
        _param("limit", "int", "本批最大行数"),
    ]), read_batch),
    register_operator(_metadata(READ_OPERATORS[2], ["read"], [_PROTOCOL_PARAM, _ACCESS_PLAN_PARAM, _LAYER_PARAM]), read_close),
    register_operator(_metadata(WRITE_OPERATORS[0], ["write", "ddl"], [
        _PROTOCOL_PARAM, _ACCESS_PLAN_PARAM, _LAYER_PARAM, _FIELDS_PARAM, _SPATIAL_PARAM,
    ]), write_open),
    register_operator(_metadata(WRITE_OPERATORS[1], ["write"], [
        _PROTOCOL_PARAM, _ACCESS_PLAN_PARAM, _LAYER_PARAM, _FIELDS_PARAM, _SPATIAL_PARAM,
        _param("offset", "int", "目标当前行数"),
        _param("rows", "object", "BatchData 行数组"),
    ]), write_batch),
    register_operator(_metadata(WRITE_OPERATORS[2], ["write"], [
        _PROTOCOL_PARAM, _ACCESS_PLAN_PARAM, _LAYER_PARAM, _FIELDS_PARAM, _SPATIAL_PARAM,
        _param("expected_row_count", "int", "提交时预期总行数"),
    ]), write_close),
    register_operator(_metadata(WRITE_OPERATORS[3], ["write", "ddl"], [
        _PROTOCOL_PARAM, _ACCESS_PLAN_PARAM, _LAYER_PARAM, _FIELDS_PARAM, _SPATIAL_PARAM,
    ]), write_abort),
])
