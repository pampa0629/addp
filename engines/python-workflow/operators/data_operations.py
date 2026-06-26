"""
数据操作算子模块

提供高级数据操作算子：裁剪、泰森多边形、按面积分割、融合、批量缓冲、批量质心
"""

import geopandas as gpd
from typing import Dict, List, Any
from .base import (
    OperatorMetadata, OperatorParam, OperatorType, OperatorCategory, register_operator, OutputPort
)
from .geometric_operators import buffer, centroid


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


# ==================== 元数据定义 ====================

CLIP_METADATA = OperatorMetadata(
    name="clip",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.DATA_OPERATION,
    description="裁剪几何",
    brief_description="用掩膜图层裁剪输入图层,保留掩膜范围内的部分,常用于研究区提取",
    execution_modes=["workflow"],

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
    category=OperatorCategory.DATA_OPERATION,
    description="泰森多边形",
    brief_description="生成点集的泰森多边形(Voronoi图),常用于服务范围划分和最近邻分析",
    execution_modes=["workflow"],

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
    category=OperatorCategory.DATA_OPERATION,
    description="按面积分割",
    brief_description="按面积阈值将数据分割为大小两组,常用于数据分类和质量检查",
    execution_modes=["workflow"],

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
    category=OperatorCategory.DATA_OPERATION,
    description="批量缓冲",
    brief_description="对同一图层使用多个缓冲距离批量生成缓冲区,常用于多级影响范围分析",
    execution_modes=["workflow"],

    overview="对输入GeoDataFrame使用多个缓冲距离批量生成缓冲区,返回包含多个缓冲结果的列表。适用于多级影响范围、服务等级划分等场景。",

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
            data_type="List[float]",
            required=True,
            description="缓冲距离列表",
            notes="如 [100, 300, 500] 表示生成100米、300米、500米三级缓冲区"
        ),
        OperatorParam(
            name="resolution",
            type="param",
            data_type="integer",
            required=False,
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
        "返回list[GeoDataFrame],每个元素对应一个缓冲距离的结果",
        "距离单位与坐标系一致",
        "地理坐标系建议先转投影坐标系",
        "批量处理比逐个调用buffer更高效"
    ],

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


BATCH_CENTROID_METADATA = OperatorMetadata(
    name="batch_centroid",
    type=OperatorType.SPATIAL,
    category=OperatorCategory.DATA_OPERATION,
    description="批量质心",
    brief_description="批量计算多个图层的质心,常用于多数据源的中心点提取",
    execution_modes=["workflow"],

    overview="对多个GeoDataFrame批量计算质心,返回质心结果列表。适用于多时相数据处理、多区域分析等场景,比逐个调用centroid更高效。",

    params=[
        OperatorParam(
            name="gdf_list",
            type="input",
            data_type="List[GeoDataFrame]",
            required=True,
            description="GeoDataFrame列表,包含需要计算质心的多个图层",
            notes="所有图层建议具有相同的坐标系"
        )
    ],

    use_cases=[
        "多时相中心点: 批量计算历年建成区的质心变化",
        "多区域标注: 为多个行政区批量生成标注点",
        "多数据源整合: 批量将多个面图层转为点图层用于聚合",
        "变化监测: 对比不同时期同一要素的质心位移"
    ],

    notes=[
        "返回list[GeoDataFrame],每个元素对应一个输入图层的质心结果",
        "质心可能位于几何对象外部(如环形面)",
        "批量处理比逐个调用centroid节省内存和时间",
        "输出结果顺序与输入gdf_list一致"
    ],

    workflow_example={
        'id': 'batch_calc_centroids',
        'operator': 'batch_centroid',
        'params': {
            'gdf_list': [
                {'$ref': 'load_area_2020'},
                {'$ref': 'load_area_2021'},
                {'$ref': 'load_area_2022'}
            ]
        },
        'depends_on': ['load_area_2020', 'load_area_2021', 'load_area_2022']
    }
)


# ==================== 注册算子 ====================

OPERATORS = dict([
    register_operator(CLIP_METADATA, clip),
    register_operator(VORONOI_METADATA, voronoi),
    register_operator(SPLIT_BY_AREA_METADATA, split_by_area),
    register_operator(BATCH_BUFFER_METADATA, batch_buffer),
    register_operator(BATCH_CENTROID_METADATA, batch_centroid),
])
