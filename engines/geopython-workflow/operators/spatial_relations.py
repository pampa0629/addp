"""
空间关系算子模块

提供空间关系判断算子：包含、相交、距离计算
"""

import geopandas as gpd
import shapely
from .base import (
    OperatorMetadata, OperatorParam, OperatorType, OperatorCategory, register_operator
)
from .geometric_operators import _require_projected_xy_crs, _finite_xy_geometry


# ==================== 算子实现 ====================

class DistanceInputError(ValueError):
    """Distance input admission errors localized by the execution adapter."""

    _messages = {
        'zh-cn': {
            'geometry_column_conflict': '源图层的活动几何列名 distance 与距离输出列冲突；请先手动重命名源几何列，再执行距离计算。',
            'projected_crs': '距离计算要求两个输入均具有已知的投影坐标系，且两个水平轴使用相同、有限且为正的线性单位；请先显式重投影。',
            'crs_mismatch': '距离计算要求两个输入使用相同的投影坐标系；请先显式重投影，不能仅因单位相同就直接计算。',
        },
        'en': {
            'geometry_column_conflict': 'Source active geometry column distance conflicts with the distance output column; '
                                        'rename the source geometry column before running distance calculation.',
            'projected_crs': 'Distance calculation requires both inputs to have a known projected CRS with matching, '
                             'finite positive horizontal linear units; reproject explicitly first.',
            'crs_mismatch': 'Distance calculation requires the same projected CRS for both inputs; '
                            'reproject explicitly first, even when their units match.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages[language][self.code]


class RelationInputError(ValueError):
    """Proven spatial-filter admission errors without business data."""

    _messages = {
        'zh-cn': {
            'source': '空间关系筛选的图层 A 须具有唯一活动几何列和唯一列名，几何须有效、非空且为有限二维 XY；请先整理源图层。',
            'reference': '空间关系筛选的图层 B 须具有唯一活动几何列和唯一列名，几何须有效、非空且为有限二维 XY；请先整理参考图层。',
            'crs': '空间关系筛选要求两个输入使用相同的已知二维地理或投影坐标系；请先显式统一坐标系。',
        },
        'en': {
            'source': 'Spatial relation filtering requires unique columns, one active geometry column and valid non-empty finite XY geometries in layer A; prepare the source layer first.',
            'reference': 'Spatial relation filtering requires unique columns, one active geometry column and valid non-empty finite XY geometries in layer B; prepare the reference layer first.',
            'crs': 'Spatial relation filtering requires the same known two-dimensional geographic or projected CRS on both inputs; explicitly align the CRSs first.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages['en' if language == 'en' else 'zh-cn'][self.code]


def _relation_filter(input_gdf, gdf_b, predicate):
    """Select original A rows with at least one exact matching B geometry."""
    geometries = []
    for frame, code in ((input_gdf, 'source'), (gdf_b, 'reference')):
        try:
            if (not isinstance(frame, gpd.GeoDataFrame) or not frame.columns.is_unique
                    or sum(isinstance(dtype, gpd.array.GeometryDtype) for dtype in frame.dtypes) != 1):
                raise ValueError('spatial filter requires one geometry and unique columns')
            geometry = _finite_xy_geometry(frame)
            if geometry.isna().any() or geometry.is_empty.any() or not shapely.is_valid(geometry.array).all():
                raise ValueError('invalid spatial filter geometry')
        except (ValueError, TypeError) as exc:
            raise RelationInputError(code) from exc
        geometries.append(geometry)
    left, right = geometries
    if (any(crs is None or not (crs.is_geographic or crs.is_projected) or len(crs.axis_info) != 2
            for crs in (left.crs, right.crs)) or not left.crs.equals(right.crs)):
        raise RelationInputError('crs')
    if left.empty or right.empty:
        return input_gdf.iloc[:0].copy()
    tree = shapely.STRtree(right.array)
    rows = []
    for position, geometry in enumerate(left.array):
        if any(predicate(geometry, right.array[index]) for index in tree.query(geometry)):
            rows.append(position)
    return input_gdf.iloc[rows].copy()


def contains(input_gdf: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """Keep each original A row containing at least one individual B geometry."""
    return _relation_filter(input_gdf, gdf_b, shapely.contains)


def intersects(input_gdf: gpd.GeoDataFrame, gdf_b: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """Keep each original A row intersecting at least one individual B geometry."""
    return _relation_filter(input_gdf, gdf_b, shapely.intersects)


def distance_to(input_gdf: gpd.GeoDataFrame, target_geom: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    距离计算（计算每个几何到目标几何的最短距离）

    Args:
        input_gdf: 输入 GeoDataFrame
        target_geom: 目标几何 GeoDataFrame

    Returns:
        GeoDataFrame 添加 distance 列
    """
    if input_gdf.geometry.name == 'distance':
        raise DistanceInputError('geometry_column_conflict')

    try:
        _require_projected_xy_crs(input_gdf.crs)
        _require_projected_xy_crs(target_geom.crs)
    except ValueError as exc:
        raise DistanceInputError('projected_crs') from exc
    if not input_gdf.crs.equals(target_geom.crs):
        raise DistanceInputError('crs_mismatch')

    result = input_gdf.copy()

    # 计算到目标几何集合中最近点的距离
    target_union = target_geom.geometry.unary_union
    result['distance'] = result.geometry.apply(lambda g: g.distance(target_union))

    return result


# ==================== 元数据定义 ====================

CONTAINS_METADATA = OperatorMetadata(
    name="contains",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_RELATION,
    description="包含判断",
    brief_description="判断图层A的几何是否包含图层B的几何,常用于点在面内判断",
    execution_modes=["workflow"],
    effects=["read"],

    overview="逐图层A要素判断是否包含任一图层B要素，返回命中的A原始行，完整保留属性、几何、索引及行序，不新增布尔列。使用精确contains语义，允许有效混合几何和GeometryCollection。",

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
        "地块筛选: 保留包含至少一个目标设施的地块"
    ],

    notes=[
        "面不包含仅在边界上的点或线；可以包含与其共边且内部位于其中的面，非空同一几何可包含自身",
        "两输入须为相同已知二维地理或投影CRS；不隐式重投影，地理坐标仍按平面关系计算",
        "输入几何须有效非空有限XY；空批合法，空参考输出保留A完整Schema的空结果",
        "按B空间索引筛候选后精确判断，命中任一B即保留一次A，不复制B属性或覆盖contains业务字段"
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
    category=OperatorCategory.VECTOR_RELATION,
    description="相交判断",
    brief_description="判断两个图层的几何是否相交(包括接触),常用于空间关系过滤",
    execution_modes=["workflow"],
    effects=["read"],

    overview="逐图层A要素判断是否与任一图层B要素相交，包括共点、共边、重叠及包含；返回命中的A原始行，完整保留属性、几何、索引及行序，不新增布尔列。允许有效混合几何和GeometryCollection。",

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
        "共点和共边均算相交；结果保留完整A原几何，不输出交集几何",
        "两输入须为相同已知二维地理或投影CRS；不隐式重投影，地理坐标仍按平面关系计算",
        "输入几何须有效非空有限XY；空批合法，空参考输出保留A完整Schema的空结果",
        "按B空间索引筛候选后精确判断，命中任一B即保留一次A，不复制B属性或覆盖intersects业务字段"
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
    category=OperatorCategory.VECTOR_RELATION,
    description="距离计算",
    brief_description="计算几何对象到目标几何的最短距离,常用于邻近度分析和缓冲区计算",
    execution_modes=["workflow"],
    effects=["read"],

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
            notes="必须与input_gdf使用相同的已知投影坐标系，两个水平轴的线性单位须相同、有限且为正；不自动重投影"
        )
    ],

    use_cases=[
        "设施可达性: 计算居民点到最近医院的距离",
        "噪音影响评估: 计算建筑物到道路的距离",
        "生态廊道: 计算栖息地斑块之间的距离",
        "配送范围: 计算客户位置到配送中心的距离"
    ],

    notes=[
        "距离保留源投影坐标系的水平单位：米制为米、英尺制为对应英尺，不自动换算米",
        "未知、地理、地心或不匹配坐标系明确拒绝；须先显式重投影，仅单位相同仍不能混算不同投影",
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
