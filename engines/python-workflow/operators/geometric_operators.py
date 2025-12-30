"""
几何处理算子模块

提供几何变换和处理算子：缓冲、相交、合并、质心、差集、简化、凸包、外接矩形
"""

import geopandas as gpd
from typing import List
from .base import (
    OperatorMetadata, OperatorParam, OperatorCategory, register_operator
)


# ==================== 算子实现 ====================

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


# ==================== 元数据定义 ====================

BUFFER_METADATA = OperatorMetadata(
    name="buffer",
    category=OperatorCategory.GEOMETRIC,
    description="缓冲区分析",
    brief_description="在几何对象周围创建指定距离的缓冲区,常用于影响范围分析和邻域查询",

    overview="在输入几何对象周围创建指定距离的缓冲区多边形。支持点、线、面等各类几何类型,缓冲距离可正可负(负值仅对面有效,表示内缩)。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="distance",
            type="param",
            data_type="float",
            required=True,
            description="缓冲距离,单位与坐标系一致",
            notes="投影坐标系单位为米,地理坐标系单位为度(不推荐,建议先转投影坐标系)"
        ),
        OperatorParam(
            name="resolution",
            type="param",
            data_type="integer",
            required=False,
            description="缓冲区圆弧段数",
            notes="值越大缓冲区越平滑但计算越慢,建议范围 8-32"
        )
    ],

    use_cases=[
        "河流污染影响范围: 河流中心线缓冲500米",
        "学校服务范围: 学校点位缓冲1000米",
        "道路噪音影响区: 道路线缓冲200米",
        "保护区核心区: 边界内缩100米(负值缓冲)"
    ],

    notes=[
        "地理坐标系(EPSG:4326)需先用 to_crs 转投影坐标系再缓冲",
        "大数据集建议先用空间过滤减少要素数量再缓冲",
        "负值缓冲仅对面几何有效,点和线无效",
        "缓冲结果可能产生自相交,建议后续用 simplify 简化"
    ],

    workflow_example={
        'id': 'buffer_rivers',
        'operator': 'buffer',
        'params': {
            'input_gdf': {'$ref': 'load_rivers'},
            'distance': 500,
            'resolution': 16
        },
        'depends_on': ['load_rivers']
    }
)


INTERSECTION_METADATA = OperatorMetadata(
    name="intersection",
    category=OperatorCategory.GEOMETRIC,
    description="几何相交",
    brief_description="计算两个几何图层的空间交集,提取重叠区域,常用于叠加分析",

    overview="计算输入图层A与图层B的几何交集,返回两者重叠的部分。结果保留图层A的属性,可选择性保留图层B的属性。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入图层A(被裁剪对象)"
        ),
        OperatorParam(
            name="gdf_b",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入图层B(裁剪对象)"
        )
    ],

    use_cases=[
        "土地利用分析: 提取保护区内的耕地",
        "行政区划统计: 提取某市范围内的河流",
        "灾害风险评估: 提取洪水淹没区内的建筑物",
        "多图层叠加: 提取同时满足多个条件的区域"
    ],

    notes=[
        "两个图层的坐标系必须一致,不一致需先用 to_crs 统一",
        "结果只包含有交集的要素,无交集的要素会被剔除",
        "如果需要保留无交集的要素,使用 spatial_join 代替",
        "大数据集建议先用空间索引优化性能"
    ],

    workflow_example={
        'id': 'intersect_protected_area',
        'operator': 'intersection',
        'params': {
            'input_gdf': {'$ref': 'load_farmland'},
            'gdf_b': {'$ref': 'load_protected_area'}
        },
        'depends_on': ['load_farmland', 'load_protected_area']
    }
)


UNION_METADATA = OperatorMetadata(
    name="union",
    category=OperatorCategory.GEOMETRIC,
    description="几何合并",
    brief_description="将多个图层的几何对象合并为一个图层,常用于数据整合和范围叠加",

    overview="将多个GeoDataFrame的所有几何对象合并到一个GeoDataFrame中。所有输入图层的要素会保留在输出中,属性也会保留。适用于多数据源整合、多时相数据合并等场景。",

    params=[
        OperatorParam(
            name="gdf_list",
            type="input",
            data_type="List[GeoDataFrame]",
            required=True,
            description="GeoDataFrame列表,包含需要合并的多个图层",
            notes="所有输入图层应具有相同的坐标系,否则建议先用to_crs统一坐标系"
        )
    ],

    use_cases=[
        "多城市数据整合: 将各市的建筑物数据合并为全省数据",
        "历史数据汇总: 将多年的土地利用变化数据合并为时序数据集",
        "分区数据拼接: 将分区采集的GPS轨迹点合并为完整轨迹",
        "多源数据融合: 将不同部门提供的POI数据整合为统一数据库"
    ],

    notes=[
        "合并后的属性列为所有输入图层属性列的并集,缺失值填充为NaN",
        "如果输入图层坐标系不一致,会导致几何错误,需先统一坐标系",
        "不同于dissolve,union不会融合几何,只是简单拼接",
        "大数据集合并可能消耗大量内存,建议分批处理"
    ],

    workflow_example={
        'id': 'union_cities',
        'operator': 'union',
        'params': {
            'gdf_list': [
                {'$ref': 'load_city_a'},
                {'$ref': 'load_city_b'},
                {'$ref': 'load_city_c'}
            ]
        },
        'depends_on': ['load_city_a', 'load_city_b', 'load_city_c']
    }
)


CENTROID_METADATA = OperatorMetadata(
    name="centroid",
    category=OperatorCategory.GEOMETRIC,
    description="计算质心",
    brief_description="计算几何对象的质心(中心点),常用于位置标注和点化处理",

    overview="计算每个几何对象的质心坐标,将面或线转换为点。质心是几何对象的几何中心,适用于标注位置、聚合分析等场景。",

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
        "行政区标注: 计算省市边界质心用于地图标注",
        "建筑物点化: 将建筑物面转为中心点用于密度分析",
        "河流中心线提取: 计算河流面的中心线用于网络分析",
        "地块代表点: 为每个地块生成代表点用于空间连接"
    ],

    notes=[
        "质心可能位于几何对象外部(如环形面),如需保证点在内部请使用representative_point",
        "线的质心是沿线中点,不一定是线的端点",
        "点的质心就是自身",
        "质心坐标单位与输入坐标系一致"
    ],

    workflow_example={
        'id': 'get_province_centers',
        'operator': 'centroid',
        'params': {
            'input_gdf': {'$ref': 'load_provinces'}
        },
        'depends_on': ['load_provinces']
    }
)


DIFFERENCE_METADATA = OperatorMetadata(
    name="difference",
    category=OperatorCategory.GEOMETRIC,
    description="几何差集",
    brief_description="计算两个图层的几何差集(A-B),保留A中不与B重叠的部分",

    overview="从图层A中减去图层B的重叠部分,返回A中独有的几何区域。常用于排除分析、净用地计算等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="被减图层(图层A)"
        ),
        OperatorParam(
            name="gdf_b",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="减去的图层(图层B)",
            notes="需与input_gdf具有相同的坐标系"
        )
    ],

    use_cases=[
        "净用地计算: 建设用地减去道路用地得到净建设用地",
        "排除保护区: 可开发区域减去自然保护区得到可用区域",
        "去除已建区: 规划范围减去已建成区得到待开发区",
        "水体擦除: 行政区边界减去水体得到陆地范围"
    ],

    notes=[
        "结果保留input_gdf的属性,gdf_b的属性不保留",
        "如果A与B完全不相交,返回A的原始几何",
        "如果A完全被B包含,返回空几何",
        "大数据集差集运算较慢,建议先用空间索引过滤"
    ],

    workflow_example={
        'id': 'exclude_protected',
        'operator': 'difference',
        'params': {
            'input_gdf': {'$ref': 'load_land'},
            'gdf_b': {'$ref': 'load_protected'}
        },
        'depends_on': ['load_land', 'load_protected']
    }
)


SIMPLIFY_METADATA = OperatorMetadata(
    name="simplify",
    category=OperatorCategory.GEOMETRIC,
    description="简化几何",
    brief_description="简化几何对象的顶点数量,减小数据量,常用于地图制图和性能优化",

    overview="使用Douglas-Peucker算法简化几何对象,减少顶点数量同时保持形状特征。适用于大比例尺制图、Web地图加载优化等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="tolerance",
            type="param",
            data_type="float",
            required=True,
            description="简化容差,单位与坐标系一致",
            notes="值越大简化越激进。投影坐标系单位为米,建议10-100;地理坐标系单位为度,建议0.0001-0.001"
        ),
        OperatorParam(
            name="preserve_topology",
            type="param",
            data_type="boolean",
            required=False,
            description="是否保持拓扑一致性",
            notes="True时保证简化后几何不自交,但速度较慢;False时速度快但可能产生自交"
        )
    ],

    use_cases=[
        "Web地图优化: 简化省市边界用于在线地图快速加载",
        "制图综合: 将1:1万地图简化为1:50万比例尺",
        "数据压缩: 减少GPS轨迹点数量降低存储空间",
        "性能优化: 简化复杂面几何加速空间运算"
    ],

    notes=[
        "tolerance过大会导致几何变形严重甚至崩溃",
        "地理坐标系(EPSG:4326)简化效果较差,建议先转投影坐标系",
        "preserve_topology=True时邻接面不会产生缝隙",
        "简化后的几何可能不满足原始精度要求,需权衡数据量和精度"
    ],

    workflow_example={
        'id': 'simplify_boundaries',
        'operator': 'simplify',
        'params': {
            'input_gdf': {'$ref': 'load_provinces'},
            'tolerance': 100,
            'preserve_topology': True
        },
        'depends_on': ['load_provinces']
    }
)


CONVEX_HULL_METADATA = OperatorMetadata(
    name="convex_hull",
    category=OperatorCategory.GEOMETRIC,
    description="凸包",
    brief_description="计算几何对象的最小凸包(凸多边形),常用于范围分析和形状概括",

    overview="计算包含几何对象所有顶点的最小凸多边形。凸包类似用橡皮筋包裹几何对象的外轮廓,适用于范围估算、离群点检测等场景。",

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
        "活动范围估算: 计算GPS轨迹点的凸包估算人员活动范围",
        "建筑群轮廓: 计算建筑物点集的凸包生成建筑群外轮廓",
        "数据质量检查: 检测离群点(凸包面积异常大)",
        "服务范围概算: 用设施点的凸包快速估算覆盖范围"
    ],

    notes=[
        "凸包会忽略凹陷部分,不能精确表示复杂形状",
        "如需精确边界,建议使用alpha_shape或buffer",
        "点的凸包是点本身,线的凸包可能是线或面",
        "凸包面积通常大于原始几何面积"
    ],

    workflow_example={
        'id': 'get_activity_range',
        'operator': 'convex_hull',
        'params': {
            'input_gdf': {'$ref': 'load_gps_points'}
        },
        'depends_on': ['load_gps_points']
    }
)


ENVELOPE_METADATA = OperatorMetadata(
    name="envelope",
    category=OperatorCategory.GEOMETRIC,
    description="最小外接矩形",
    brief_description="计算几何对象的最小外接矩形(MBR),常用于快速范围查询和空间索引",

    overview="计算包含几何对象的最小轴对齐矩形(Minimum Bounding Rectangle),边平行于坐标轴。常用于空间索引、粗略范围查询等场景。",

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
        "空间索引构建: 为复杂几何创建快速索引矩形",
        "地图瓦片切割: 计算要素范围用于地图瓦片分配",
        "粗略相交判断: 用矩形快速过滤候选对象再精确计算",
        "图框生成: 为地图要素生成标准矩形图框"
    ],

    notes=[
        "矩形边平行于坐标轴,对旋转的狭长几何可能产生很大冗余",
        "如需最小面积外接矩形,使用minimum_rotated_rectangle",
        "envelope面积通常大于原始几何面积",
        "点和线的envelope可能退化为线或点"
    ],

    workflow_example={
        'id': 'get_bounds_rect',
        'operator': 'envelope',
        'params': {
            'input_gdf': {'$ref': 'load_buildings'}
        },
        'depends_on': ['load_buildings']
    }
)


# ==================== 注册算子 ====================

OPERATORS = dict([
    register_operator(BUFFER_METADATA, buffer),
    register_operator(INTERSECTION_METADATA, intersection),
    register_operator(UNION_METADATA, union),
    register_operator(CENTROID_METADATA, centroid),
    register_operator(DIFFERENCE_METADATA, difference),
    register_operator(SIMPLIFY_METADATA, simplify),
    register_operator(CONVEX_HULL_METADATA, convex_hull),
    register_operator(ENVELOPE_METADATA, envelope),
])
