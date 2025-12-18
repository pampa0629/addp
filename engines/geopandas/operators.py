"""
空间算子实现
使用 GeoPandas 和 Shapely 实现 21 个空间算子
"""

import geopandas as gpd
from shapely.geometry import shape, mapping, Point, LineString, Polygon
from shapely.ops import unary_union
from typing import Dict, Any, List, Union
import json


# ========================================
# 几何处理算子 (8个)
# ========================================

def buffer(input_gdf: gpd.GeoDataFrame, distance: float, resolution: int = 16) -> gpd.GeoDataFrame:
    """
    缓冲区分析

    Args:
        input_gdf: 输入 GeoDataFrame
        distance: 缓冲距离（米）
        resolution: 圆弧段数

    Returns:
        GeoDataFrame 缓冲区结果
    """
    result = input_gdf.copy()
    result['geometry'] = result['geometry'].buffer(distance, resolution=resolution)
    return result


def intersection(gdf_a: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    几何相交（叠加分析）

    Args:
        gdf_a: GeoDataFrame A
        gdf_b: GeoDataFrame B

    Returns:
        GeoDataFrame 交集结果
    """
    return gpd.overlay(gdf_a, gdf_b, how='intersection')


def union(gdf_list: List[gpd.GeoDataFrame]) -> gpd.GeoDataFrame:
    """
    几何合并（联合多个 GeoDataFrame）

    Args:
        gdf_list: GeoDataFrame 列表

    Returns:
        GeoDataFrame 合并结果
    """
    if len(gdf_list) == 0:
        raise ValueError("gdf_list cannot be empty")

    result = gdf_list[0].copy()
    for gdf in gdf_list[1:]:
        result = gpd.overlay(result, gdf, how='union')

    return result


def centroid(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    计算质心

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 质心点结果
    """
    result = input_gdf.copy()
    result['geometry'] = result['geometry'].centroid
    return result


def difference(gdf_a: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    几何差集（A - B）

    Args:
        gdf_a: GeoDataFrame A
        gdf_b: GeoDataFrame B

    Returns:
        GeoDataFrame 差集结果
    """
    return gpd.overlay(gdf_a, gdf_b, how='difference')


def simplify(input_gdf: gpd.GeoDataFrame, tolerance: float, preserve_topology: bool = True) -> gpd.GeoDataFrame:
    """
    简化几何（Douglas-Peucker 算法）

    Args:
        input_gdf: 输入 GeoDataFrame
        tolerance: 简化容差
        preserve_topology: 是否保持拓扑

    Returns:
        GeoDataFrame 简化结果
    """
    result = input_gdf.copy()
    result['geometry'] = result['geometry'].simplify(tolerance, preserve_topology=preserve_topology)
    return result


def convex_hull(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    计算凸包

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 凸包结果
    """
    result = input_gdf.copy()
    result['geometry'] = result['geometry'].convex_hull
    return result


def envelope(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    计算最小外接矩形（MBR）

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        GeoDataFrame 外接矩形结果
    """
    result = input_gdf.copy()
    result['geometry'] = result['geometry'].envelope
    return result


# ========================================
# 空间关系算子 (3个)
# ========================================

def contains(gdf_a: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    包含关系判断（返回 A 中包含 B 的记录）

    Args:
        gdf_a: GeoDataFrame A
        gdf_b: GeoDataFrame B

    Returns:
        GeoDataFrame A 中包含 B 的记录
    """
    result = gdf_a.copy()
    result['contains'] = result['geometry'].apply(
        lambda geom: any(geom.contains(g) for g in gdf_b['geometry'])
    )
    return result[result['contains']]


def intersects(gdf_a: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    相交关系判断（返回 A 中与 B 相交的记录）

    Args:
        gdf_a: GeoDataFrame A
        gdf_b: GeoDataFrame B

    Returns:
        GeoDataFrame A 中与 B 相交的记录
    """
    result = gdf_a.copy()
    result['intersects'] = result['geometry'].apply(
        lambda geom: any(geom.intersects(g) for g in gdf_b['geometry'])
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


# ========================================
# 几何属性算子 (3个)
# ========================================

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


# ========================================
# 格式转换算子 (2个)
# ========================================

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


# ========================================
# 批处理算子 (2个)
# ========================================

def batch_buffer(input_gdf: gpd.GeoDataFrame, distances: List[float], resolution: int = 16) -> List[gpd.GeoDataFrame]:
    """
    批量缓冲区分析（多个距离）

    Args:
        input_gdf: 输入 GeoDataFrame
        distances: 距离列表
        resolution: 圆弧段数

    Returns:
        GeoDataFrame 列表
    """
    return [buffer(input_gdf, dist, resolution) for dist in distances]


def batch_centroid(gdf_list: List[gpd.GeoDataFrame]) -> List[gpd.GeoDataFrame]:
    """
    批量计算质心

    Args:
        gdf_list: GeoDataFrame 列表

    Returns:
        质心 GeoDataFrame 列表
    """
    return [centroid(gdf) for gdf in gdf_list]


# ========================================
# 高级算子 (3个)
# ========================================

def dissolve(input_gdf: gpd.GeoDataFrame, by: Union[str, List[str]] = None) -> gpd.GeoDataFrame:
    """
    融合（dissolve）几何，按字段分组

    Args:
        input_gdf: 输入 GeoDataFrame
        by: 分组字段（可选）

    Returns:
        GeoDataFrame 融合结果
    """
    if by is None:
        # 全部融合为一个几何
        result = gpd.GeoDataFrame(
            {'geometry': [input_gdf['geometry'].unary_union]},
            crs=input_gdf.crs
        )
    else:
        # 按字段分组融合
        result = input_gdf.dissolve(by=by, as_index=False)

    return result


def clip(input_gdf: gpd.GeoDataFrame, mask_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    裁剪几何（使用 mask 裁剪 input）

    Args:
        input_gdf: 输入 GeoDataFrame
        mask_gdf: 裁剪 mask GeoDataFrame

    Returns:
        GeoDataFrame 裁剪结果
    """
    return gpd.clip(input_gdf, mask_gdf)


def voronoi(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    泰森多边形（Voronoi 图）

    Args:
        input_gdf: 输入点 GeoDataFrame

    Returns:
        GeoDataFrame Voronoi 多边形结果
    """
    from shapely.ops import voronoi_diagram

    # 合并所有点
    points = input_gdf['geometry'].unary_union

    # 计算 Voronoi 图
    voronoi_polys = voronoi_diagram(points)

    # 转换为 GeoDataFrame
    result = gpd.GeoDataFrame(
        {'geometry': list(voronoi_polys.geoms)},
        crs=input_gdf.crs
    )

    return result


# ========================================
# 算子注册表（供 API 使用）
# ========================================

OPERATORS = {
    # 几何处理 (8个)
    'buffer': {
        'function': buffer,
        'params': {'distance': 'float', 'resolution': 'int'},
        'category': '几何处理',
        'description': '缓冲区分析'
    },
    'intersection': {
        'function': intersection,
        'params': {'gdf_b': 'geodataframe'},
        'category': '几何处理',
        'description': '几何相交'
    },
    'union': {
        'function': union,
        'params': {'gdf_list': 'list[geodataframe]'},
        'category': '几何处理',
        'description': '几何合并'
    },
    'centroid': {
        'function': centroid,
        'params': {},
        'category': '几何处理',
        'description': '计算质心'
    },
    'difference': {
        'function': difference,
        'params': {'gdf_b': 'geodataframe'},
        'category': '几何处理',
        'description': '几何差集'
    },
    'simplify': {
        'function': simplify,
        'params': {'tolerance': 'float', 'preserve_topology': 'bool'},
        'category': '几何处理',
        'description': '简化几何'
    },
    'convex_hull': {
        'function': convex_hull,
        'params': {},
        'category': '几何处理',
        'description': '凸包'
    },
    'envelope': {
        'function': envelope,
        'params': {},
        'category': '几何处理',
        'description': '最小外接矩形'
    },

    # 空间关系 (3个)
    'contains': {
        'function': contains,
        'params': {'gdf_b': 'geodataframe'},
        'category': '空间关系',
        'description': '包含判断'
    },
    'intersects': {
        'function': intersects,
        'params': {'gdf_b': 'geodataframe'},
        'category': '空间关系',
        'description': '相交判断'
    },
    'distance_to': {
        'function': distance_to,
        'params': {'target_geom': 'geodataframe'},
        'category': '空间关系',
        'description': '距离计算'
    },

    # 几何属性 (3个)
    'get_area': {
        'function': get_area,
        'params': {},
        'category': '几何属性',
        'description': '计算面积'
    },
    'get_length': {
        'function': get_length,
        'params': {},
        'category': '几何属性',
        'description': '计算长度'
    },
    'get_bounds': {
        'function': get_bounds,
        'params': {},
        'category': '几何属性',
        'description': '获取边界框'
    },

    # 格式转换 (2个)
    'load_from_wkt': {
        'function': load_from_wkt,
        'params': {'wkt_list': 'list[str]', 'properties': 'list[dict]', 'crs': 'str'},
        'category': '格式转换',
        'description': 'WKT → GeoDataFrame'
    },
    'export_to_wkt': {
        'function': export_to_wkt,
        'params': {},
        'category': '格式转换',
        'description': 'GeoDataFrame → WKT'
    },

    # 批处理 (2个)
    'batch_buffer': {
        'function': batch_buffer,
        'params': {'distances': 'list[float]', 'resolution': 'int'},
        'category': '批处理',
        'description': '批量缓冲'
    },
    'batch_centroid': {
        'function': batch_centroid,
        'params': {'gdf_list': 'list[geodataframe]'},
        'category': '批处理',
        'description': '批量质心'
    },

    # 高级算子 (3个)
    'dissolve': {
        'function': dissolve,
        'params': {'by': 'str or list[str]'},
        'category': '高级算子',
        'description': '融合几何'
    },
    'clip': {
        'function': clip,
        'params': {'mask_gdf': 'geodataframe'},
        'category': '高级算子',
        'description': '裁剪几何'
    },
    'voronoi': {
        'function': voronoi,
        'params': {},
        'category': '高级算子',
        'description': '泰森多边形'
    }
}


def get_operator(operator_name: str):
    """获取算子函数"""
    if operator_name not in OPERATORS:
        raise ValueError(f"Unknown operator: {operator_name}")
    return OPERATORS[operator_name]['function']


def list_operators():
    """列出所有算子 - 返回符合统一标准的算子元数据列表"""
    operators_list = []

    # Python类型到标准类型的映射
    type_mapping = {
        'float': 'float',
        'int': 'integer',
        'bool': 'boolean',
        'str': 'string',
        'geodataframe': 'object',
        'list[float]': 'array',
        'list[str]': 'array',
    }

    for name, meta in OPERATORS.items():
        # 转换参数格式
        parameters = []
        for param_name, param_type in meta['params'].items():
            param_meta = {
                "name": param_name,
                "type": type_mapping.get(param_type, 'string'),
                "required": True if param_name == "input_gdf" else False,
                "description": f"{param_name}参数"
            }

            # 数组类型需要指定item_type
            if param_type in ['list[float]', 'list[str]']:
                param_meta["item_type"] = "float" if param_type == 'list[float]' else "string"

            parameters.append(param_meta)

        # 构建标准化的算子元数据
        operator = {
            "id": name,
            "name": name,
            "display_name": meta['description'],
            "type": "spatial",
            "category": meta['category'],
            "description": meta['description'],
            "module": "geopandas",
            "parameters": parameters,
            "inputs": ["geodataframe"],
            "outputs": ["geodataframe"]
        }
        operators_list.append(operator)

    return operators_list
