"""
几何属性算子模块

提供计算几何对象属性的算子：面积、长度、边界框
"""

import geopandas as gpd
from .base import (
    OperatorType,
    OperatorMetadata, OperatorParam, OperatorCategory, register_operator
)


# ==================== 算子实现 ====================

def get_area(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    计算面积

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 添加 area 列
    """
    result = input_gdf.copy()
    result['area'] = result['geometry'].area
    return result


def get_length(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    计算长度/周长

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 添加 length 列
    """
    result = input_gdf.copy()
    result['length'] = result['geometry'].length
    return result


def get_bounds(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    获取边界框（minx, miny, maxx, maxy）

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 添加 bounds 列
    """
    result = input_gdf.copy()
    bounds = result['geometry'].bounds
    result['minx'] = bounds['minx']
    result['miny'] = bounds['miny']
    result['maxx'] = bounds['maxx']
    result['maxy'] = bounds['maxy']
    return result


# ==================== 元数据定义 ====================

GET_AREA_METADATA = OperatorMetadata(
    name="get_area",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.PROPERTIES,
    description="计算面积",
    brief_description="计算面几何的面积,常用于土地统计和资源核算",
    execution_modes=["workflow"],

    overview="计算GeoDataFrame中每个面几何的面积,返回数值。适用于土地利用统计、资源清单、规划指标核算等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的面几何数据"
        )
    ],

    use_cases=[
        "土地利用统计: 计算各地类的总面积",
        "规划指标核算: 统计建设用地面积是否超标",
        "生态资源清查: 计算林地、湿地面积",
        "房产测绘: 计算建筑物占地面积"
    ],

    notes=[
        "面积单位与坐标系一致:投影坐标系为平方米,地理坐标系为平方度",
        "地理坐标系(EPSG:4326)的平方度不直观,建议先转投影坐标系(如UTM)",
        "对于点和线几何,面积为0",
        "大范围区域面积计算建议使用等面积投影(如Albers)"
    ],

    workflow_example={
        'id': 'calc_landuse_area',
        'operator': 'get_area',
        'params': {
            'input_gdf': {'$ref': 'load_landuse'}
        },
        'depends_on': ['load_landuse']
    }
)


GET_LENGTH_METADATA = OperatorMetadata(
    name="get_length",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.PROPERTIES,
    description="计算长度",
    brief_description="计算线几何的长度,常用于道路统计和网络分析",
    execution_modes=["workflow"],

    overview="计算GeoDataFrame中每个线几何的长度,返回数值。适用于道路统计、河流长度计算、管网清单等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的线几何数据"
        )
    ],

    use_cases=[
        "道路里程统计: 计算各级道路总长度",
        "河流长度计算: 统计流域内河流总长",
        "管网清单: 计算供水、燃气管道长度",
        "步行距离: 计算步行路径的实际长度"
    ],

    notes=[
        "长度单位与坐标系一致:投影坐标系为米,地理坐标系为度",
        "地理坐标系(EPSG:4326)的度单位不直观,建议先转投影坐标系",
        "对于点几何,长度为0;对于面几何,返回周长",
        "大范围线要素长度计算建议使用等距投影"
    ],

    workflow_example={
        'id': 'calc_road_length',
        'operator': 'get_length',
        'params': {
            'input_gdf': {'$ref': 'load_roads'}
        },
        'depends_on': ['load_roads']
    }
)


GET_BOUNDS_METADATA = OperatorMetadata(
    name="get_bounds",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.PROPERTIES,
    description="获取边界框",
    brief_description="获取几何对象的边界框坐标(minx, miny, maxx, maxy),常用于范围查询",
    execution_modes=["workflow"],

    overview="返回每个几何对象的边界框(Bounding Box)坐标,包含minx、miny、maxx、maxy四个值。适用于地图范围定位、数据裁剪、空间索引构建等场景。",

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
        "地图定位: 获取要素范围用于地图缩放定位",
        "数据裁剪: 用边界框快速过滤候选数据",
        "瓦片计算: 计算要素所在的地图瓦片编号",
        "空间索引: 构建R-Tree等空间索引的边界矩形"
    ],

    notes=[
        "返回DataFrame包含minx、miny、maxx、maxy四列",
        "边界框是轴对齐矩形,不随几何旋转",
        "坐标单位与输入坐标系一致",
        "点的边界框minx=maxx, miny=maxy"
    ],

    workflow_example={
        'id': 'get_feature_bounds',
        'operator': 'get_bounds',
        'params': {
            'input_gdf': {'$ref': 'load_features'}
        },
        'depends_on': ['load_features']
    }
)


# ==================== 注册算子 ====================

OPERATORS = dict([
    register_operator(GET_AREA_METADATA, get_area),
    register_operator(GET_LENGTH_METADATA, get_length),
    register_operator(GET_BOUNDS_METADATA, get_bounds),
])
