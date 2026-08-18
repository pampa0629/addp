"""
空间转换算子模块

提供几何坐标参考转换算子。
"""

import geopandas as gpd
from pyproj import CRS

from .base import (
    OperatorCategory,
    OperatorMetadata,
    OperatorParam,
    OperatorType,
    register_operator,
)


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


OPERATORS = dict([
    register_operator(VECTOR_REPROJECT_METADATA, vector_reproject),
])
