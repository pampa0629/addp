"""
数据操作算子模块

提供高级数据操作算子：裁剪、泰森多边形、按面积分割、多距缓冲
"""

import geopandas as gpd
import pandas as pd
from math import isfinite
from numbers import Integral, Real
from typing import Dict, List, Any
from .base import (
    OperatorMetadata, OperatorParam, OperatorType, OperatorCategory, register_operator, OutputPort
)
from .geometric_operators import buffer


# ==================== 算子实现 ====================

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


def split_by_area(input_gdf: gpd.GeoDataFrame, threshold: float) -> Dict[str, gpd.GeoDataFrame]:
    """
    按面积阈值分割数据（多输出算子示例）

    Args:
        input_gdf: 输入 GeoDataFrame
        threshold: 面积阈值（平方单位，取决于 CRS）

    Returns:
        Dict[str, GeoDataFrame]: 包含两个输出端口
            - "large": 面积大于阈值的要素
            - "small": 面积小于等于阈值的要素
    """
    result = input_gdf.copy()
    result['area'] = result['geometry'].area

    large = result[result['area'] > threshold].copy()
    small = result[result['area'] <= threshold].copy()

    # 返回命名输出端口
    return {
        "large": large,
        "small": small
    }


class MultiBufferInputError(ValueError):
    """Multi-distance buffer admission errors localized by the execution adapter."""

    _messages = {
        'zh-cn': {
            'source': '多距缓冲要求具有活动几何列且列名唯一的 GeoDataFrame 输入。',
            'distance_conflict': '源图层的列名 distance 与多距缓冲输出列冲突；请先手动重命名源列，再执行多距缓冲。',
            'distances': '多距缓冲的距离列表必须是有限数值数组，不接受布尔值、字符串或空值。',
            'resolution': '多距缓冲的圆弧段数必须是正整数。',
        },
        'en': {
            'source': 'Multi-distance buffer requires a GeoDataFrame with an active geometry column and unique column names.',
            'distance_conflict': 'Source column distance conflicts with the multi-distance buffer output column; '
                                 'rename the source column before running multi-distance buffer.',
            'distances': 'Multi-distance buffer distances must be an array of finite numbers; '
                         'booleans, strings and null values are rejected.',
            'resolution': 'Multi-distance buffer resolution must be a positive integer.',
        },
    }

    def __init__(self, code):
        self.code = code
        super().__init__(self.message('zh-cn'))

    def message(self, language):
        return self._messages[language][self.code]


def batch_buffer(input_gdf: gpd.GeoDataFrame, distances: List[float], resolution: int = 16) -> gpd.GeoDataFrame:
    """Expand full buffers into one layer, ordered by distance and original row."""
    if not isinstance(input_gdf, gpd.GeoDataFrame) or not input_gdf.columns.is_unique:
        raise MultiBufferInputError('source')
    try:
        input_gdf.geometry
    except AttributeError as exc:
        raise MultiBufferInputError('source') from exc
    if 'distance' in input_gdf.columns:
        raise MultiBufferInputError('distance_conflict')
    if not isinstance(distances, list):
        raise MultiBufferInputError('distances')
    normalized = []
    for value in distances:
        if isinstance(value, bool) or not isinstance(value, Real):
            raise MultiBufferInputError('distances')
        try:
            distance = float(value)
        except (ValueError, OverflowError) as exc:
            raise MultiBufferInputError('distances') from exc
        if not isfinite(distance):
            raise MultiBufferInputError('distances')
        normalized.append(distance)
    if isinstance(resolution, bool) or not isinstance(resolution, Integral) or resolution <= 0:
        raise MultiBufferInputError('resolution')

    if not normalized or input_gdf.empty:
        result = input_gdf.iloc[:0].copy().reset_index(drop=True)
        result['distance'] = pd.Series(dtype='float64')
        return result

    buffers = []
    for distance in normalized:
        result = buffer(input_gdf, distance, int(resolution))
        result['distance'] = distance
        buffers.append(result)
    return pd.concat(buffers, ignore_index=True)


# ==================== 元数据定义 ====================

CLIP_METADATA = OperatorMetadata(
    name="clip",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_OVERLAY,
    description="裁剪几何",
    brief_description="用掩膜图层裁剪输入图层,保留掩膜范围内的部分,常用于研究区提取",
    execution_modes=["workflow"],
    effects=["read"],

    overview="使用掩膜图层(mask)裁剪输入图层,只保留落在掩膜范围内的几何部分。类似GIS中的Clip工具,常用于提取研究区数据、行政区数据裁剪等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="被裁剪的输入图层"
        ),
        OperatorParam(
            name="mask_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="掩膜图层,定义裁剪范围",
            notes="需与input_gdf具有相同的坐标系,通常是面图层"
        )
    ],

    use_cases=[
        "研究区提取: 用行政区边界裁剪全国数据得到省级数据",
        "项目范围裁剪: 用项目红线裁剪地形数据",
        "海陆分离: 用陆地边界裁剪数据去除海洋部分",
        "缓冲区内裁剪: 用缓冲区裁剪要素得到影响范围内的数据"
    ],

    notes=[
        "裁剪后保留input_gdf的属性,mask_gdf只提供范围",
        "与intersection的区别:clip会裁切几何,intersection保留完整几何",
        "掩膜图层如有多个面,会按union后的总范围裁剪",
        "大数据集裁剪建议先用空间索引过滤候选对象"
    ],

    workflow_example={
        'id': 'clip_by_province',
        'operator': 'clip',
        'params': {
            'input_gdf': {'$ref': 'load_national_roads'},
            'mask_gdf': {'$ref': 'load_province_boundary'}
        },
        'depends_on': ['load_national_roads', 'load_province_boundary']
    }
)


VORONOI_METADATA = OperatorMetadata(
    name="voronoi",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_CONSTRUCTION,
    description="泰森多边形",
    brief_description="生成点集的泰森多边形(Voronoi图),常用于服务范围划分和最近邻分析",
    execution_modes=["workflow"],
    effects=["read"],

    overview="为输入的点集生成泰森多边形(Voronoi Diagram)。每个多边形内的所有位置到对应点的距离都比到其他点更近。适用于服务范围划分、影响区域分析、最近设施查询等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的点图层"
        )
    ],

    use_cases=[
        "服务范围划分: 为医院、学校等设施生成服务范围多边形",
        "势力范围分析: 分析各门店的潜在影响区域",
        "最近设施查询: 快速定位距离最近的服务点",
        "气象插值: 为气象站点生成影响区域用于空间插值"
    ],

    notes=[
        "输入必须是点几何,线和面会报错",
        "泰森多边形会覆盖整个凸包范围,边界多边形会很大",
        "如需限定范围,建议后续用clip裁剪到研究区",
        "点位重复会导致结果异常,建议先去重"
    ],

    workflow_example={
        'id': 'gen_service_areas',
        'operator': 'voronoi',
        'params': {
            'input_gdf': {'$ref': 'load_hospitals'}
        },
        'depends_on': ['load_hospitals']
    }
)


SPLIT_BY_AREA_METADATA = OperatorMetadata(
    name="split_by_area",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_SELECTION,
    description="按面积分割",
    brief_description="按面积阈值将数据分割为大小两组,常用于数据分类和质量检查",
    execution_modes=["workflow"],
    effects=["read"],

    overview="根据面积阈值将输入要素分为两组:大于阈值的要素和小于等于阈值的要素。这是一个多输出算子示例,返回两个独立的输出端口。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的面几何数据"
        ),
        OperatorParam(
            name="threshold",
            type="param",
            data_type="float",
            required=True,
            description="面积阈值,单位与坐标系一致",
            notes="投影坐标系单位为平方米,地理坐标系单位为平方度"
        )
    ],

    use_cases=[
        "地块筛选: 分离大地块和小地块用于不同规划策略",
        "建筑物分类: 区分大型建筑和小型建筑",
        "数据质量检查: 识别面积异常的要素",
        "图斑分级: 按面积阈值对图斑进行分级统计"
    ],

    notes=[
        "返回两个输出端口: large(大于阈值) 和 small(小于等于阈值)",
        "面积单位与坐标系一致,地理坐标系建议先转投影坐标系",
        "对于点和线几何,面积为0,全部归入small组",
        "多输出算子需要在工作流中正确连接不同的输出端口"
    ],

    workflow_example={
        'id': 'split_parcels',
        'operator': 'split_by_area',
        'params': {
            'input_gdf': {'$ref': 'load_parcels'},
            'threshold': 10000
        },
        'depends_on': ['load_parcels']
    },

    output_ports=[
        OutputPort(
            name="large",
            type="geodataframe",
            description="面积大于阈值的要素",
            is_default=False
        ),
        OutputPort(
            name="small",
            type="geodataframe",
            description="面积小于等于阈值的要素",
            is_default=False
        )
    ]
)



BATCH_BUFFER_METADATA = OperatorMetadata(
    name="batch_buffer",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.VECTOR_CONSTRUCTION,
    description="多距缓冲",
    brief_description="对同一图层生成多个距离的完整缓冲区,汇总为带 distance 字段的单图层",
    execution_modes=["workflow"],
    effects=["read"],

    overview="对输入图层按距离数组顺序逐一生成完整缓冲区,保留源属性和CRS,以单个GeoDataFrame返回并追加distance字段,可直接连接数据保存。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="distances",
            type="param",
            data_type="list[float]",
            required=True,
            description="缓冲距离列表",
            notes="如 [100, 300, 500]；单位沿用源CRS,不自动换算为米。保留顺序和重复距离,允许零、负值及空数组。"
        ),
        OperatorParam(
            name="resolution",
            type="param",
            data_type="int",
            required=False,
            default=16,
            description="缓冲区圆弧段数",
            notes="值越大越平滑,建议8-32"
        )
    ],

    use_cases=[
        "多级影响范围: 分析设施500米、1000米、2000米三级服务范围",
        "噪音等级划分: 道路50米、100米、200米三级噪音影响区",
        "辐射范围分析: 基站200米、500米、1000米覆盖范围",
        "风险分区: 危化品仓库100米、300米、500米风险等级区"
    ],

    notes=[
        "返回一个GeoDataFrame,依距离顺序再依源行顺序展开,保留原属性、类型、活动几何列和CRS",
        "追加float64 distance列；任意源列重名时明确报错,须先在上游手动改名",
        "生成完整缓冲区而非环带,不融合不同源要素；零和负距离沿用普通缓冲语义",
        "距离单位与源CRS一致,不自动重投影；空数组或空源返回同Schema空图层"
    ],

    output_ports=[OutputPort(name='default', type='geodataframe', description='带distance字段的单图层完整缓冲区', is_default=True)],

    workflow_example={
        'id': 'multi_level_buffer',
        'operator': 'batch_buffer',
        'params': {
            'input_gdf': {'$ref': 'load_facilities'},
            'distances': [500, 1000, 2000],
            'resolution': 16
        },
        'depends_on': ['load_facilities']
    }
)


# ==================== 注册算子 ====================

OPERATORS = dict([
    register_operator(CLIP_METADATA, clip),
    register_operator(VORONOI_METADATA, voronoi),
    register_operator(SPLIT_BY_AREA_METADATA, split_by_area),
    register_operator(BATCH_BUFFER_METADATA, batch_buffer),
])
