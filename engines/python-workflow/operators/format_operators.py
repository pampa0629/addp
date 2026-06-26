"""
格式转换算子模块

提供 WKT 格式与 GeoDataFrame 之间的相互转换
"""

import geopandas as gpd
from typing import List, Dict
from .base import (
    OperatorType,
    OperatorMetadata, OperatorParam, OperatorCategory, register_operator
)


# ==================== 算子实现 ====================

def load_from_wkt(wkt_list: List[str], properties: List[Dict] = None, crs: str = "EPSG:4326") -> gpd.GeoDataFrame:
    """
    从 WKT 格式加载几何

    Args:
        wkt_list: WKT 字符串列表
        properties: 属性字典列表（可选）
        crs: 坐标系统（默认 EPSG:4326）

    Returns:
        GeoDataFrame
    """
    from shapely import wkt

    geometries = [wkt.loads(w) for w in wkt_list]

    if properties is None:
        properties = [{}] * len(geometries)

    gdf = gpd.GeoDataFrame(properties, geometry=geometries, crs=crs)
    return gdf


def export_to_wkt(input_gdf: gpd.GeoDataFrame) -> List[str]:
    """
    导出为 WKT 格式

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        WKT 字符串列表
    """
    return input_gdf['geometry'].apply(lambda g: g.wkt).tolist()


# ==================== 元数据定义 ====================

LOAD_FROM_WKT_METADATA = OperatorMetadata(
    name="load_from_wkt",
    type=OperatorType.GENERAL,
    category=OperatorCategory.FORMAT_CONVERSION,
    description="WKT → GeoDataFrame",
    brief_description="从WKT文本列表创建GeoDataFrame,常用于文本格式几何数据导入",
    execution_modes=["workflow"],

    overview="将Well-Known Text (WKT)格式的几何文本列表转换为GeoDataFrame。WKT是标准的几何文本表示格式,常用于数据交换和存储。",

    params=[
        OperatorParam(
            name="wkt_list",
            type="param",
            data_type="list[str]",
            required=True,
            description="WKT文本字符串列表",
            notes='每个字符串表示一个几何对象,如 "POINT (120.5 30.2)"'
        ),
        OperatorParam(
            name="properties",
            type="param",
            data_type="list[dict]",
            required=False,
            description="属性字典列表,每个字典对应一个几何的属性",
            notes="长度必须与wkt_list相同"
        ),
        OperatorParam(
            name="crs",
            type="param",
            data_type="str",
            required=False,
            description='坐标参考系统（默认 "EPSG:4326"）',
            notes='如 "EPSG:4326" (WGS84) 或 "EPSG:3857" (Web墨卡托)'
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
        "properties列表长度必须与wkt_list一致",
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
    category=OperatorCategory.FORMAT_CONVERSION,
    description="GeoDataFrame → WKT",
    brief_description="将GeoDataFrame的几何导出为WKT文本列表,常用于数据交换",
    execution_modes=["workflow"],

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
        "返回list[str],每个元素是一个WKT文本",
        "WKT文本不包含坐标系信息,需单独记录CRS",
        "复杂几何的WKT文本可能很长",
        "属性信息不包含在WKT中,需单独处理"
    ],

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
