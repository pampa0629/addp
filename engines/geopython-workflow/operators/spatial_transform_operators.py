"""
空间转换算子模块

提供几何坐标参考转换和 CRS 定义表达转换算子。
"""

import hashlib
import json
import re

import geopandas as gpd
from pyproj import CRS, network

from .base import (
    OperatorCategory,
    OperatorMetadata,
    OperatorParam,
    OperatorType,
    OutputPort,
    register_operator,
)


_EPSG_CRS_REF = re.compile(r"^EPSG:([1-9][0-9]*)$", re.IGNORECASE)
_ADDP_CRS_REF = re.compile(r"^ADDP:CRS:([0-9a-f]{64})$", re.IGNORECASE)
_SUPPORTED_DEFINITION_ENCODINGS = {"wkt", "esri_wkt", "proj4", "projjson"}


def _crs_from_user_input(value: str | CRS | None) -> CRS | None:
    text = str(value or "").strip()
    if not text:
        return None
    return CRS.from_user_input(text)


def vector_reproject(
    input_gdf: gpd.GeoDataFrame,
    source_crs: str = "",
    target_crs: str = "EPSG:4326",
) -> gpd.GeoDataFrame:
    """
    将输入几何批重投影到目标 CRS。

    direct 调用时，输入由 Arrow/EWKB geometry batch 解码为 GeoDataFrame。
    """
    if input_gdf is None:
        raise ValueError("input_gdf is required")
    if not isinstance(input_gdf, gpd.GeoDataFrame):
        raise ValueError("input_gdf must be a GeoDataFrame")
    if input_gdf.geometry.name == "":
        raise ValueError("input_gdf requires an active geometry column")

    result = input_gdf.copy()
    current_crs = _crs_from_user_input(result.crs)
    declared_source = _crs_from_user_input(source_crs)

    if current_crs is None and declared_source is None:
        raise ValueError("source_crs is required when input_gdf has no CRS")
    if current_crs is None and declared_source is not None:
        result = result.set_crs(declared_source, allow_override=False)
        current_crs = declared_source
    elif current_crs is not None and declared_source is not None and not current_crs.equals(declared_source):
        raise ValueError("input_gdf CRS does not match source_crs")

    target = _crs_from_user_input(target_crs) or CRS.from_user_input("EPSG:4326")
    if current_crs is None:
        current_crs = _crs_from_user_input(source_crs)
    if current_crs is None:
        raise ValueError("source_crs is required when input_gdf has no CRS")

    if current_crs.equals(target):
        return result
    return result.to_crs(target)


def crs_to_projjson(
    crs_ref: str,
    definition_encoding: str = "",
    definition: str = "",
) -> dict:
    """把同一 CRS 的定义规范化为 PROJJSON，不改变任何 geometry 坐标。"""
    # 在解析任何 authority 或定义之前关闭 PROJ 网络能力，确保本算子只依赖
    # Runtime 镜像内随附的本地 PROJ database。
    network.set_network_enabled(active=False)
    normalized_ref, authority, code = _normalize_crs_ref(crs_ref)
    source_definition = str(definition or "").strip()
    source_encoding = str(definition_encoding or "").strip().lower()

    if source_definition:
        if source_encoding not in _SUPPORTED_DEFINITION_ENCODINGS:
            raise ValueError(
                "definition_encoding must be one of wkt, esri_wkt, proj4, projjson"
            )
        source_crs = _parse_crs_definition(source_encoding, source_definition)
    else:
        if source_encoding:
            raise ValueError("definition is required when definition_encoding is provided")
        if authority != "EPSG":
            raise ValueError("definition is required for non-EPSG crs_ref")
        source_crs = None

    if authority == "EPSG":
        canonical_crs = CRS.from_epsg(int(code))
        if source_crs is not None and not source_crs.equals(canonical_crs, ignore_axis_order=True):
            raise ValueError(f"CRS definition does not match {normalized_ref}")
        output_crs = canonical_crs
    else:
        expected_hash = hashlib.sha256(source_definition.encode("utf-8")).hexdigest()
        if expected_hash != code.removeprefix("CRS:").lower():
            raise ValueError("ADDP CRS ref does not match the source definition")
        output_crs = source_crs

    projjson = output_crs.to_json_dict()
    if not isinstance(projjson, dict) or not str(projjson.get("type") or "").strip():
        raise ValueError("PROJ returned an invalid PROJJSON object")
    projjson["id"] = {"authority": authority, "code": int(code) if authority == "EPSG" else code}
    canonical_definition = json.dumps(
        projjson,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    )
    return {
        "crs_ref": normalized_ref,
        "definition_encoding": "projjson",
        "definition": canonical_definition,
    }


def _normalize_crs_ref(crs_ref: str) -> tuple[str, str, str]:
    value = str(crs_ref or "").strip()
    epsg_match = _EPSG_CRS_REF.fullmatch(value)
    if epsg_match:
        code = str(int(epsg_match.group(1)))
        return f"EPSG:{code}", "EPSG", code
    addp_match = _ADDP_CRS_REF.fullmatch(value)
    if addp_match:
        digest = addp_match.group(1).lower()
        return f"ADDP:CRS:{digest}", "ADDP", f"CRS:{digest}"
    raise ValueError("crs_ref must be EPSG:<code> or ADDP:CRS:<sha256>")


def _parse_crs_definition(encoding: str, definition: str) -> CRS:
    if encoding in {"wkt", "esri_wkt"}:
        return CRS.from_wkt(definition)
    if encoding == "proj4":
        return CRS.from_proj4(definition)
    if encoding == "projjson":
        return CRS.from_json(definition)
    raise ValueError(f"unsupported CRS definition encoding: {encoding}")


VECTOR_REPROJECT_METADATA = OperatorMetadata(
    name="vector_reproject",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.SPATIAL_TRANSFORM,
    description="空间坐标参考转换",
    brief_description="将几何批从源 CRS 重投影到目标 CRS",
    execution_modes=["workflow", "direct"],
    effects=["read"],
    overview="对输入几何批执行 CRS 重投影。该算子只负责 geometry 转换，不接管 Transfer 的 load/save、checkpoint、属性字段或中间落盘。",
    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入几何批",
            notes="workflow 调用时由上游节点传入；direct 调用时由 Arrow/EWKB geometry batch 解码得到",
        ),
        OperatorParam(
            name="source_crs",
            type="param",
            data_type="str",
            required=False,
            description="源 CRS",
            notes="当输入批没有 CRS 时必须显式提供",
        ),
        OperatorParam(
            name="target_crs",
            type="param",
            data_type="str",
            required=False,
            description="目标 CRS",
            default="EPSG:4326",
        ),
    ],
    use_cases=[
        "Shapefile 到 GeoJSON 的空间坐标参考转换",
        "将非 4326 几何批转换为 GeoJSON 兼容坐标",
        "批量矢量数据的 CRS 重投影",
    ],
    notes=[
        "只处理几何批，不承接属性字段的筛选、映射或写出",
        "输入批必须提供 CRS 或显式 source_crs",
        "转换失败必须直接报错，不能静默保留源坐标",
    ],
    workflow_example={
        "id": "reproject_roads",
        "operator": "vector_reproject",
        "params": {
            "input_gdf": {"$ref": "load_roads"},
            "source_crs": "EPSG:3857",
            "target_crs": "EPSG:4326",
        },
        "depends_on": ["load_roads"],
    },
    attributes={
        "direct_binary": {
            "content_type": "application/vnd.apache.arrow.stream",
            "encoding": "arrow",
            "input_name": "geometry_batch",
            "output_name": "geometry_batch",
            "geometry_column": "geometry",
            "geometry_encoding": "ewkb",
        }
    },
)


CRS_TO_PROJJSON_METADATA = OperatorMetadata(
    name="crs_to_projjson",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.SPATIAL_TRANSFORM,
    description="CRS 定义转换为 PROJJSON",
    brief_description="在不改变坐标的前提下规范化 CRS 定义表达",
    execution_modes=["direct"],
    effects=["read"],
    overview="使用 Runtime 本地 PROJ database 把 WKT、ESRI WKT、Proj4 或 EPSG authority 定义规范化为 PROJJSON；不读取 geometry，也不执行坐标重投影。",
    params=[
        OperatorParam(
            name="crs_ref",
            type="param",
            data_type="str",
            required=True,
            description="平台 CRS 身份",
            notes="支持 EPSG:<code> 或带源定义的 ADDP:CRS:<sha256>",
        ),
        OperatorParam(
            name="definition_encoding",
            type="param",
            data_type="str",
            required=False,
            description="源 CRS 定义编码",
            enum=["wkt", "esri_wkt", "proj4", "projjson"],
        ),
        OperatorParam(
            name="definition",
            type="param",
            data_type="str",
            required=False,
            description="源 CRS 定义文本",
            notes="只有 EPSG:<code> 可以省略",
        ),
    ],
    output_ports=[
        OutputPort(
            name="default",
            type="object",
            description="PROJJSON CRS 定义",
            is_default=True,
        )
    ],
    use_cases=[
        "PostGIS WKT CRS 定义写入 GeoParquet",
        "Shapefile ESRI WKT CRS 定义写入 GeoParquet",
        "从 Runtime 本地 EPSG database 生成 PROJJSON",
    ],
    notes=[
        "只转换 CRS 定义表达，不改变 geometry 坐标",
        "禁止联网解析 CRS 或下载 grid",
        "CRS identity 冲突时必须失败",
    ],
    workflow_example={},
)


OPERATORS = dict([
    register_operator(VECTOR_REPROJECT_METADATA, vector_reproject),
    register_operator(CRS_TO_PROJJSON_METADATA, crs_to_projjson),
])
