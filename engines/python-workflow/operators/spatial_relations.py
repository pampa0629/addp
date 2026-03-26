"""
空间关系算子模块

提供空间关系判断算子：包含、相交、距离计算
"""

import geopandas as gpd
from .base import (
    OperatorMetadata, OperatorParam, OperatorType, OperatorCategory, register_operator
)


# ==================== 算子实现 ====================

def contains(input_gdf: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    包含关系判断（返回 A 中包含 B 的记录）

    Args:
        input_gdf: 输入图层 A（主图层）
        gdf_b: 输入图层 B（参考图层）

    Returns:
        GeoDataFrame A 中包含 B 的记录
    """
    result = input_gdf.copy()
    geom_col = result.geometry.name
    result['contains'] = result.geometry.apply(
        lambda geom: any(geom.contains(g) for g in gdf_b.geometry)
    )
    return result[result['contains']]


def intersects(input_gdf: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    相交关系判断（返回 A 中与 B 相交的记录）

    Args:
        input_gdf: 输入图层 A（主图层）
        gdf_b: 输入图层 B（参考图层）

    Returns:
        GeoDataFrame A 中与 B 相交的记录
    """
    result = input_gdf.copy()
    geom_col = result.geometry.name
    result['intersects'] = result.geometry.apply(
        lambda geom: any(geom.intersects(g) for g in gdf_b.geometry)
    )
    return result[result['intersects']]


def distance_to(input_gdf: gpd.GeoDataFrame, target_geom: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    距离计算（计算每个几何到目标几何的最短距离）

    Args:
        input_gdf: 输入 GeoDataFrame
        target_geom: 目标几何 GeoDataFrame

    Returns:
        GeoDataFrame 添加 distance 列
    """
    result = input_gdf.copy()

    # 计算到目标几何集合中最近点的距离
    target_union = target_geom['geometry'].unary_union
    result['distance'] = result['geometry'].apply(lambda g: g.distance(target_union))

    return result


# ==================== 元数据定义 ====================

CONTAINS_METADATA = OperatorMetadata(
    name="contains",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.SPATIAL_RELATION,
    description="包含判断",
    brief_description="判断图层A的几何是否包含图层B的几何,常用于点在面内判断",

    overview="对图层A和图层B进行空间包含关系判断,返回布尔值表示A是否完全包含B。常用于点在面内查询、边界检查等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="容器图层(图层A,通常是面)"
        ),
        OperatorParam(
            name="gdf_b",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="被包含图层(图层B,通常是点)",
            notes="需与input_gdf具有相同的坐标系"
        )
    ],

    use_cases=[
        "点在面内查询: 判断POI点是否在行政区内",
        "建筑物归属: 判断建筑物是否完全在地块内",
        "数据质量检查: 检查子区域是否完全在父区域内",
        "权限验证: 判断用户操作范围是否在授权区域内"
    ],

    notes=[
        "包含关系要求B完全在A内部,边界接触不算包含",
        "如需包含边界接触情况,使用covers关系",
        "大数据集判断建议先用空间索引过滤",
        "返回布尔Series,可用于过滤数据"
    ],

    workflow_example={
        'id': 'check_points_in_area',
        'operator': 'contains',
        'params': {
            'input_gdf': {'$ref': 'load_districts'},
            'gdf_b': {'$ref': 'load_poi_points'}
        },
        'depends_on': ['load_districts', 'load_poi_points']
    }
)


INTERSECTS_METADATA = OperatorMetadata(
    name="intersects",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.SPATIAL_RELATION,
    description="相交判断",
    brief_description="判断两个图层的几何是否相交(包括接触),常用于空间关系过滤",

    overview="判断图层A和图层B的几何对象是否存在空间相交关系(包括重叠、接触、包含等)。返回布尔值,是最常用的空间关系判断算子。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="图层A"
        ),
        OperatorParam(
            name="gdf_b",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="图层B",
            notes="需与input_gdf具有相同的坐标系"
        )
    ],

    use_cases=[
        "灾害影响分析: 判断洪水淹没区与建筑物是否相交",
        "规划冲突检测: 检查新规划地块是否与现状用地冲突",
        "设施服务范围: 判断服务设施缓冲区与居民区是否相交",
        "交通可达性: 判断道路网络与目标区域是否连通"
    ],

    notes=[
        "intersects包含所有非分离情况:重叠、接触、包含、被包含",
        "边界接触也算相交,与contains不同",
        "是最宽松的空间关系判断,适用于初步筛选",
        "大数据集建议先用空间索引过滤候选对象"
    ],

    workflow_example={
        'id': 'check_flood_impact',
        'operator': 'intersects',
        'params': {
            'input_gdf': {'$ref': 'load_flood_zone'},
            'gdf_b': {'$ref': 'load_buildings'}
        },
        'depends_on': ['load_flood_zone', 'load_buildings']
    }
)


DISTANCE_TO_METADATA = OperatorMetadata(
    name="distance_to",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.SPATIAL_RELATION,
    description="距离计算",
    brief_description="计算几何对象到目标几何的最短距离,常用于邻近度分析和缓冲区计算",

    overview="计算图层A中每个几何对象到图层B中最近几何对象的欧氏距离。返回距离值,适用于可达性分析、服务范围评估等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="源图层"
        ),
        OperatorParam(
            name="target_geom",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="目标图层",
            notes="需与input_gdf具有相同的坐标系,且建议使用投影坐标系以获得准确距离"
        )
    ],

    use_cases=[
        "设施可达性: 计算居民点到最近医院的距离",
        "噪音影响评估: 计算建筑物到道路的距离",
        "生态廊道: 计算栖息地斑块之间的距离",
        "配送范围: 计算客户位置到配送中心的距离"
    ],

    notes=[
        "距离单位与坐标系一致:投影坐标系为米,地理坐标系为度",
        "地理坐标系(EPSG:4326)的度单位不直观,建议先转投影坐标系",
        "返回的是最近距离,如需到所有目标的距离需遍历",
        "大数据集建议先用空间索引加速最近邻查询"
    ],

    workflow_example={
        'id': 'calc_distance_to_hospital',
        'operator': 'distance_to',
        'params': {
            'input_gdf': {'$ref': 'load_communities'},
            'target_geom': {'$ref': 'load_hospitals'}
        },
        'depends_on': ['load_communities', 'load_hospitals']
    }
)


# ==================== 注册算子 ====================

OPERATORS = dict([
    register_operator(CONTAINS_METADATA, contains),
    register_operator(INTERSECTS_METADATA, intersects),
    register_operator(DISTANCE_TO_METADATA, distance_to),
])
