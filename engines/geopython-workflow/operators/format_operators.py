"""
格式转换算子模块

提供 WKT 格式与 GeoDataFrame 之间的相互转换
"""

import geopandas as gpd
from pyproj import CRS
from pyproj.exceptions import CRSError
import shapely
from .base import (
    OperatorType,
    OperatorMetadata, OperatorParam, OperatorCategory, OutputPort, register_operator
)
from .geometric_operators import _active_geometry


class WKTInputError(ValueError):
    def __init__(self, reason, *, field=""):
        super().__init__(reason)
        self.reason = reason
        self.field = field

    @classmethod
    def input_layer(cls, _message):
        return cls("operator_input_invalid")


def load_from_wkt(
    wkt_list: list[str | None], properties: list[dict] | None = None, crs: str | None = "EPSG:4326",
) -> gpd.GeoDataFrame:
    """按输入位置解析 WKT，保留缺失几何。"""
    if not isinstance(wkt_list, list) or any(
        value is not None and (not isinstance(value, str) or not value.strip()) for value in wkt_list
    ):
        raise WKTInputError("operator_input_invalid")
    if properties is None:
        properties = [{} for _ in wkt_list]
    elif (not isinstance(properties, list) or len(properties) != len(wkt_list)
          or any(not isinstance(row, dict) for row in properties)):
        raise WKTInputError("operator_wkt_properties_invalid")
    if any("geometry" in row for row in properties):
        raise WKTInputError("operator_field_conflict", field="geometry")

    if crs is not None and not isinstance(crs, str):
        raise WKTInputError("operator_crs_invalid")
    try:
        spatial_reference = CRS.from_user_input(crs) if crs else None
    except CRSError:
        # An unavailable PROJ database also raises CRSError.
        CRS.from_epsg(4326)
        raise WKTInputError("operator_crs_invalid") from None

    geometries = shapely.from_wkt(wkt_list, on_invalid="ignore")
    if any(text is not None and geometry is None for text, geometry in zip(wkt_list, geometries)):
        raise WKTInputError("operator_input_invalid")
    return gpd.GeoDataFrame(properties, geometry=geometries, crs=spatial_reference)


def export_to_wkt(input_gdf: gpd.GeoDataFrame) -> list[str | None]:
    """按活动几何的行序输出 WKT 或缺失值。"""
    geometry = _active_geometry(input_gdf, input_error=WKTInputError.input_layer)
    return [None if value is None else value.wkt for value in geometry]


# ==================== 元数据定义 ====================

LOAD_FROM_WKT_METADATA = OperatorMetadata(
    name="load_from_wkt",
    type=OperatorType.GENERAL,
    category=OperatorCategory.VECTOR_FORMAT,
    description="WKT → GeoDataFrame",
    brief_description="从WKT文本列表创建GeoDataFrame,常用于文本格式几何数据导入",
    execution_modes=["workflow"],
    effects=["read"],

    overview="将Well-Known Text (WKT)格式的几何文本列表转换为GeoDataFrame。WKT是标准的几何文本表示格式,常用于数据交换和存储。",

    params=[
        OperatorParam(
            name="wkt_list",
            type="param",
            data_type="list[str|null]",
            required=True,
            description="WKT 字符串或 null 的列表",
            notes='null 表示缺失几何，"POINT EMPTY" 表示带类型的空几何'
        ),
        OperatorParam(
            name="properties",
            type="param",
            data_type="list[dict]",
            required=False,
            description="属性字典列表,每个字典对应一个几何的属性",
            notes="长度须与 wkt_list 相同，geometry 为保留字段"
        ),
        OperatorParam(
            name="crs",
            type="param",
            data_type="str",
            required=False,
            description='坐标参考系统（默认 "EPSG:4326"）',
            notes='例如 "EPSG:3857"，显式 null 表示未定义'
        )
    ],

    use_cases=[
        "API数据导入: 从REST API返回的WKT文本创建图层",
        "数据库导入: 读取数据库中WKT字段创建空间数据",
        "文本解析: 解析配置文件或日志中的几何文本",
        "格式转换: 将其他系统导出的WKT转为GeoDataFrame"
    ],

    notes=[
        "WKT格式必须符合OGC标准,否则解析失败",
        "常见WKT类型: POINT、LINESTRING、POLYGON、MULTIPOINT等",
        "保留列表项的顺序和缺失几何的位置",
        "默认坐标系为WGS84 (EPSG:4326)"
    ],

    workflow_example={
        'id': 'load_wkt_data',
        'operator': 'load_from_wkt',
        'params': {
            'wkt_list': [
                'POINT (120.5 30.2)',
                'POINT (121.0 31.0)'
            ],
            'properties': [
                {'name': 'Point A'},
                {'name': 'Point B'}
            ],
            'crs': 'EPSG:4326'
        }
    }
)


EXPORT_TO_WKT_METADATA = OperatorMetadata(
    name="export_to_wkt",
    type=OperatorType.GENERAL,
    category=OperatorCategory.VECTOR_FORMAT,
    description="GeoDataFrame → WKT",
    brief_description="将GeoDataFrame的几何导出为WKT文本列表,常用于数据交换",
    execution_modes=["workflow"],
    effects=["read"],

    overview="将GeoDataFrame中的几何对象转换为Well-Known Text (WKT)格式的文本列表。WKT是标准的几何文本表示,便于存储到数据库文本字段或传输给其他系统。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        )
    ],

    use_cases=[
        "数据库存储: 将几何导出为WKT存入数据库文本字段",
        "API响应: 将空间数据以WKT格式返回给客户端",
        "日志记录: 记录几何对象的文本表示到日志",
        "格式转换: 导出为其他GIS系统可识别的WKT格式"
    ],

    notes=[
        "按活动几何的行序返回 WKT 字符串或 null，EMPTY 保留为 WKT 文本",
        "WKT文本不包含坐标系信息,需单独记录CRS",
        "复杂几何的WKT文本可能很长",
        "属性信息不包含在WKT中,需单独处理"
    ],

    output_ports=[OutputPort(name="default", type="array", description="逐行输出 WKT 字符串或 null")],
    workflow_example={
        'id': 'export_wkt',
        'operator': 'export_to_wkt',
        'params': {
            'input_gdf': {'$ref': 'load_features'}
        },
        'depends_on': ['load_features']
    }
)


# ==================== 注册算子 ====================

OPERATORS = dict([
    register_operator(LOAD_FROM_WKT_METADATA, load_from_wkt),
    register_operator(EXPORT_TO_WKT_METADATA, export_to_wkt),
])
