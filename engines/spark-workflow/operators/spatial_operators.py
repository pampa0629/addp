"""
Spark 工作流空间分析算子
"""

import logging
from typing import Dict, Any, List, Optional

from .base import OperatorMetadata, OperatorParam, OperatorCategory, register_operator
from .spark_types import DataFrame

logger = logging.getLogger(__name__)


# ========================================
# 空间分析算子 (7个)
# ========================================

def buffer(input_df: DataFrame, geom_column: str = "geom", distance: float = 0.01, output_column: str = "buffer_geom") -> DataFrame:
    """
    缓冲区算子

    Args:
        input_df: 输入DataFrame
        geom_column: 几何列名
        distance: 缓冲距离 (单位与坐标系一致)
        output_column: 输出几何列名

    Returns:
        包含缓冲区的DataFrame

    Example:
        df_buffered = buffer(df, geom_column="geom", distance=100, output_column="buffer_geom")
    """
    from pyspark.sql.functions import expr

    df_result = input_df.withColumn(
        output_column,
        expr(
            f"ST_SetSRID(ST_Buffer({geom_column}, {distance}), "
            f"ST_SRID({geom_column}))"
        )
    )

    logger.info(f"Applied buffer with distance {distance}")

    return df_result




def intersection(input_df: DataFrame, geom_column: str = "geom", intersect_geom: str = None, output_column: str = "intersection_geom") -> DataFrame:
    """
    相交算子

    Args:
        input_df: 输入DataFrame
        geom_column: 几何列名
        intersect_geom: 相交几何 (WKT格式字符串或列名)
        output_column: 输出几何列名

    Returns:
        相交结果DataFrame
    """
    from pyspark.sql.functions import expr, lit

    if intersect_geom.startswith("POINT") or intersect_geom.startswith("POLYGON") or intersect_geom.startswith("LINESTRING"):
        # WKT字符串
        df_result = input_df.withColumn(
            output_column,
            expr(f"ST_Intersection({geom_column}, ST_GeomFromWKT('{intersect_geom}'))")
        )
    else:
        # 列名
        df_result = input_df.withColumn(
            output_column,
            expr(f"ST_Intersection({geom_column}, {intersect_geom})")
        )

    return df_result




def union(input_df: DataFrame, geom_column: str = "geom") -> DataFrame:
    """
    几何合并算子

    将所有几何对象合并为一个

    Args:
        input_df: 输入DataFrame
        geom_column: 几何列名

    Returns:
        合并结果DataFrame (单行)
    """
    from pyspark.sql.functions import expr

    df_result = input_df.agg(expr(f"ST_Union_Aggr({geom_column}) as union_geom"))

    return df_result




def centroid(input_df: DataFrame, geom_column: str = "geom", output_column: str = "centroid") -> DataFrame:
    """
    质心算子

    Args:
        input_df: 输入DataFrame
        geom_column: 几何列名
        output_column: 输出质心列名

    Returns:
        包含质心的DataFrame
    """
    from pyspark.sql.functions import expr

    df_result = input_df.withColumn(
        output_column,
        expr(f"ST_Centroid({geom_column})")
    )

    return df_result




def spatial_join(
    left_df: DataFrame,
    right_df: DataFrame,
    left_geom: str = "geom",
    right_geom: str = "geom",
    join_type: str = "intersects"
) -> DataFrame:
    """
    空间连接算子 (核心算子)

    Args:
        left_df: 左表DataFrame
        right_df: 右表DataFrame
        left_geom: 左表几何列名
        right_geom: 右表几何列名
        join_type: 连接类型
            - "intersects": 相交
            - "contains": 包含
            - "within": 被包含
            - "overlaps": 重叠

    Returns:
        空间连接结果DataFrame

    Example:
        # POI与区域相交
        result = spatial_join(poi_df, region_df, join_type="intersects")
    """
    from pyspark.sql.functions import expr

    # 为左右表添加别名,避免列名冲突
    left_alias = left_df.alias("a")
    right_alias = right_df.alias("b")

    # 构建空间谓词
    if join_type == "intersects":
        predicate = expr(f"ST_Intersects(a.{left_geom}, b.{right_geom})")
    elif join_type == "contains":
        predicate = expr(f"ST_Contains(a.{left_geom}, b.{right_geom})")
    elif join_type == "within":
        predicate = expr(f"ST_Within(a.{left_geom}, b.{right_geom})")
    elif join_type == "overlaps":
        predicate = expr(f"ST_Overlaps(a.{left_geom}, b.{right_geom})")
    else:
        raise ValueError(f"Unsupported join_type: {join_type}")

    # 执行空间连接
    df_result = left_alias.join(right_alias, predicate, "inner")

    logger.info(f"Spatial join completed with type: {join_type}")

    return df_result




def distance(input_df: DataFrame, geom_column: str = "geom", target_geom: str = None, output_column: str = "distance") -> DataFrame:
    """
    距离计算算子

    Args:
        input_df: 输入DataFrame
        geom_column: 几何列名
        target_geom: 目标几何 (WKT格式或列名)
        output_column: 输出距离列名

    Returns:
        包含距离的DataFrame
    """
    from pyspark.sql.functions import expr

    if target_geom.startswith("POINT"):
        # WKT字符串
        df_result = input_df.withColumn(
            output_column,
            expr(f"ST_Distance({geom_column}, ST_GeomFromWKT('{target_geom}'))")
        )
    else:
        # 列名
        df_result = input_df.withColumn(
            output_column,
            expr(f"ST_Distance({geom_column}, {target_geom})")
        )

    return df_result




def transform(input_df: DataFrame, geom_column: str = "geom", source_crs: str = "EPSG:4326", target_crs: str = "EPSG:3857", output_column: str = "geom_transformed") -> DataFrame:
    """
    坐标转换算子

    Args:
        input_df: 输入DataFrame
        geom_column: 几何列名
        source_crs: 源坐标系 (默认WGS84)
        target_crs: 目标坐标系 (默认Web Mercator)
        output_column: 输出列名

    Returns:
        坐标转换后的DataFrame
    """
    from pyspark.sql.functions import expr

    df_result = input_df.withColumn(
        output_column,
        expr(f"ST_Transform({geom_column}, '{source_crs}', '{target_crs}')")
    )

    return df_result


# ========================================
# 空间算子元数据 (Pydantic 格式)
# ========================================

BUFFER_METADATA = OperatorMetadata(
    name="buffer",
    category=OperatorCategory.SPATIAL_ANALYSIS,
    description="缓冲区分析",
    brief_description="在几何对象周围创建指定距离的缓冲区,用于影响范围分析",
    execution_modes=["workflow"],
    effects=["read"],
    overview="buffer 是最常用的空间分析算子,在点、线、面周围创建指定距离的缓冲区。广泛应用于影响范围分析、服务半径计算、安全距离评估等场景。",
    params=[
        OperatorParam(name="input_df", type="dataframe", required=True, description="输入 DataFrame", notes="必须包含有效的几何列"),
        OperatorParam(name="geom_column", type="str", required=False, description="几何列名", notes='默认"geom",必须是 Geometry 类型'),
        OperatorParam(name="distance", type="float", required=True, description="缓冲距离", notes="单位与坐标系一致:地理坐标系(度),投影坐标系(米)"),
        OperatorParam(name="output_column", type="str", required=False, description="输出几何列名", notes='默认"buffer_geom",避免覆盖原始几何')
    ],
    use_cases=[
        "学校服务半径: 学校点位缓冲 1000米,分析覆盖的小区数量和人口",
        "河流污染影响: 河流中心线缓冲 500米,统计影响范围内的农田面积",
        "道路噪声评估: 高速公路缓冲 200米,计算受噪声影响的居民数",
        "基站覆盖分析: 5G 基站缓冲 300米,评估网络覆盖率"
    ],
    notes=[
        "地理坐标系(EPSG:4326)缓冲效果不准确,建议先用 transform 转投影坐标系",
        "投影坐标系单位为米,地理坐标系单位为度(1度≈111km)",
        "缓冲区会产生复杂的多边形,后续操作可能较慢",
        "大数据集缓冲建议设置合理的 distance,避免内存溢出"
    ],
    input_desc="DataFrame (包含 Geometry 列)",
    output_desc="DataFrame (新增缓冲区几何列)",
    workflow_example={
        'id': 'school_buffer_analysis',
        'operator': 'buffer',
        'params': {
            'input_df': {'$ref': 'load_schools'},
            'geom_column': 'geom',
            'distance': 1000,
            'output_column': 'buffer_geom'
        },
        'depends_on': ['load_schools']
    }
)


INTERSECTION_METADATA = OperatorMetadata(
    name="intersection",
    category=OperatorCategory.SPATIAL_ANALYSIS,
    description="几何相交",
    brief_description="计算两个几何对象的相交部分,用于叠加分析",
    execution_modes=["workflow"],
    effects=["read"],
    overview="intersection 计算输入几何与另一个几何的相交部分,返回交集几何。常用于行政区边界裁剪、土地利用叠加分析等场景。",
    params=[
        OperatorParam(name="input_df", type="dataframe", required=True, description="输入 DataFrame", notes="包含待相交的几何列"),
        OperatorParam(name="geom_column", type="str", required=False, description="几何列名", notes='默认"geom"'),
        OperatorParam(name="intersect_geom", type="str", required=True, description="相交几何", notes="WKT字符串或另一列名"),
        OperatorParam(name="output_column", type="str", required=False, description="输出几何列名", notes='默认"intersection_geom"')
    ],
    use_cases=[
        "行政区裁剪: 将 200万条全国道路网络与北京市边界相交,提取北京市内的道路",
        "土地利用叠加: 将 5万块建设用地与生态红线相交,统计违规建设面积",
        "洪水淹没分析: 将 20平方公里洪水淹没范围与建筑物相交,计算受灾建筑",
        "研究区提取: 将 1000万条全国 POI 数据与研究区边界相交,提取研究区内的 POI"
    ],
    notes=[
        "相交结果可能是 Point、LineString、Polygon,取决于输入几何类型",
        "不相交时返回空几何(EMPTY),需要过滤掉",
        "相交计算较慢,大数据集建议先用空间索引优化",
        "配合 spatial_join 使用效果更好"
    ],
    input_desc="DataFrame (包含 Geometry 列)",
    output_desc="DataFrame (新增相交几何列)",
    workflow_example={
        'id': 'clip_roads_by_city',
        'operator': 'intersection',
        'params': {
            'input_df': {'$ref': 'load_roads'},
            'geom_column': 'geom',
            'intersect_geom': 'city_boundary',
            'output_column': 'clipped_geom'
        },
        'depends_on': ['load_roads']
    }
)


UNION_METADATA = OperatorMetadata(
    name="union",
    category=OperatorCategory.SPATIAL_ANALYSIS,
    description="几何合并",
    brief_description="将所有几何对象合并为单个几何,用于生成整体边界",
    execution_modes=["workflow"],
    effects=["read"],
    overview="union 将 DataFrame 中的所有几何对象合并为一个几何。常用于生成区域的整体边界、合并相邻地块、构建宏观轮廓等场景。",
    params=[
        OperatorParam(name="input_df", type="dataframe", required=True, description="输入 DataFrame", notes="包含待合并的几何列"),
        OperatorParam(name="geom_column", type="str", required=False, description="几何列名", notes='默认"geom"')
    ],
    use_cases=[
        "省级边界生成: 将一个省内 16 个市级边界合并,生成省级整体边界",
        "地块合并: 将相邻的 50 个小地块合并为一个大地块",
        "建成区提取: 将城市内 30万栋建筑物合并,生成建成区轮廓",
        "道路网络合并: 将城市内 80万条道路段合并为一个 MultiLineString"
    ],
    notes=[
        "合并操作计算密集,大数据集(>10万几何)可能很慢",
        "结果是单个几何对象,DataFrame 只有一行",
        "合并前建议先按区域分组,避免全局合并",
        "使用 ST_Union_Aggr 聚合函数实现"
    ],
    input_desc="DataFrame (包含 Geometry 列)",
    output_desc="DataFrame (单行,包含合并后的几何)",
    workflow_example={
        'id': 'union_city_districts',
        'operator': 'union',
        'params': {
            'input_df': {'$ref': 'load_districts'},
            'geom_column': 'geom'
        },
        'depends_on': ['load_districts']
    }
)


CENTROID_METADATA = OperatorMetadata(
    name="centroid",
    category=OperatorCategory.SPATIAL_ANALYSIS,
    description="质心计算",
    brief_description="计算几何对象的几何中心点,用于标注和聚合分析",
    execution_modes=["workflow"],
    effects=["read"],
    overview="centroid 计算几何对象的几何中心(质心)。对于多边形,质心可能在边界外部(凹多边形);对于 MultiPolygon,返回所有部分的加权中心。",
    params=[
        OperatorParam(name="input_df", type="dataframe", required=True, description="输入 DataFrame", notes="包含几何列"),
        OperatorParam(name="geom_column", type="str", required=False, description="几何列名", notes='默认"geom"'),
        OperatorParam(name="output_column", type="str", required=False, description="输出质心列名", notes='默认"centroid"')
    ],
    use_cases=[
        "行政区标注: 将 34 个省级边界转为质心点,用于地图标注省名",
        "聚类分析: 将 50万栋建筑物面转为质心点,进行 K-means 聚类",
        "点位汇总: 将 3000 个行政区边界转为质心,按质心位置进行空间统计",
        "POI 简化: 将 2万座商场建筑面转为质心点,简化为 POI 点位"
    ],
    notes=[
        "凹多边形的质心可能在多边形外部",
        "质心计算不考虑洞(Polygon Holes)",
        "对于线,质心是线的中点;对于点,质心是自身",
        "如需保证质心在多边形内,使用 ST_PointOnSurface"
    ],
    input_desc="DataFrame (包含 Geometry 列)",
    output_desc="DataFrame (新增质心列)",
    workflow_example={
        'id': 'province_centroid',
        'operator': 'centroid',
        'params': {
            'input_df': {'$ref': 'load_provinces'},
            'geom_column': 'geom',
            'output_column': 'label_point'
        },
        'depends_on': ['load_provinces']
    }
)


SPATIAL_JOIN_METADATA = OperatorMetadata(
    name="spatial_join",
    category=OperatorCategory.SPATIAL_ANALYSIS,
    description="空间连接",
    brief_description="基于空间关系连接两个表,是最核心的空间分析算子",
    execution_modes=["workflow"],
    effects=["read"],
    overview="spatial_join 是空间分析的核心算子,根据空间关系(相交、包含、被包含、重叠)连接两个 DataFrame。等价于 SQL 的 JOIN,但连接条件是空间谓词而非字段相等。",
    params=[
        OperatorParam(name="left_df", type="dataframe", required=True, description="左表 DataFrame", notes="主表,空间分析的主体"),
        OperatorParam(name="right_df", type="dataframe", required=True, description="右表 DataFrame", notes="参考表,提供空间关系"),
        OperatorParam(name="left_geom", type="str", required=False, description="左表几何列名", notes='默认"geom"'),
        OperatorParam(name="right_geom", type="str", required=False, description="右表几何列名", notes='默认"geom"'),
        OperatorParam(name="join_type", type="str", required=False, description="空间连接类型", notes="intersects(相交)/contains(包含)/within(被包含)/overlaps(重叠)")
    ],
    use_cases=[
        "POI 行政区关联: 将 100万 POI 点与 300 个行政区连接(within),为每个 POI 添加所属区域",
        "建筑物统计: 将 50万建筑物与街道边界连接(intersects),统计每条街道的建筑数",
        "土地利用分类: 将 20万块地块与土地利用规划区连接(within),为地块添加规划属性",
        "道路行政区归属: 将 100万条道路与 3000 个区域连接(intersects),统计每个区域的道路长度"
    ],
    notes=[
        "Sedona 自动使用空间索引(R-Tree)优化连接性能",
        "intersects 是最常用的连接类型,性能最好",
        "contains 和 within 是相反的关系,注意主表选择",
        "大表连接(>1000万)建议先过滤数据或分区处理"
    ],
    input_desc="两个 DataFrame (都包含 Geometry 列)",
    output_desc="DataFrame (连接结果,包含左右表所有列)",
    workflow_example={
        'id': 'poi_district_join',
        'operator': 'spatial_join',
        'params': {
            'left_df': {'$ref': 'load_poi'},
            'right_df': {'$ref': 'load_districts'},
            'left_geom': 'geom',
            'right_geom': 'boundary',
            'join_type': 'within'
        },
        'depends_on': ['load_poi', 'load_districts']
    }
)


DISTANCE_METADATA = OperatorMetadata(
    name="distance",
    category=OperatorCategory.SPATIAL_ANALYSIS,
    description="距离计算",
    brief_description="计算几何对象到目标点/线/面的距离,用于邻近度分析",
    execution_modes=["workflow"],
    effects=["read"],
    overview="distance 计算每个几何对象到目标几何的最短距离。常用于'最近的地铁站'、'到市中心的距离'等邻近度分析。",
    params=[
        OperatorParam(name="input_df", type="dataframe", required=True, description="输入 DataFrame", notes="包含几何列"),
        OperatorParam(name="geom_column", type="str", required=False, description="几何列名", notes='默认"geom"'),
        OperatorParam(name="target_geom", type="str", required=True, description="目标几何", notes='WKT字符串(如"POINT(116.4 39.9)")或列名'),
        OperatorParam(name="output_column", type="str", required=False, description="输出距离列名", notes='默认"distance"')
    ],
    use_cases=[
        "地铁站邻近度: 计算 10万个小区到最近地铁站的距离,筛选距离<500米的小区",
        "医院可达性: 计算 2万个村庄到乡镇卫生院的距离,评估医疗服务覆盖",
        "噪声影响评估: 计算 30万栋居民楼到高速公路的距离,评估噪声影响程度",
        "配送距离: 计算 100万条订单配送地址到仓库的距离,用于配送费计算"
    ],
    notes=[
        "地理坐标系(EPSG:4326)计算的距离单位是度,需转投影坐标系才能得到米",
        "EPSG:3857 等投影坐标系计算距离单位为米",
        "距离计算是最短欧式距离,不考虑道路网络",
        "大数据集计算距离较慢,建议先用缓冲区过滤"
    ],
    input_desc="DataFrame (包含 Geometry 列)",
    output_desc="DataFrame (新增距离列)",
    workflow_example={
        'id': 'distance_to_city_center',
        'operator': 'distance',
        'params': {
            'input_df': {'$ref': 'load_communities'},
            'geom_column': 'geom',
            'target_geom': 'POINT(116.407526 39.904030)',
            'output_column': 'distance_to_center'
        },
        'depends_on': ['load_communities']
    }
)


TRANSFORM_METADATA = OperatorMetadata(
    name="transform",
    category=OperatorCategory.SPATIAL_ANALYSIS,
    description="坐标转换",
    brief_description="在不同坐标系之间转换几何对象,是空间分析的前置步骤",
    execution_modes=["workflow"],
    effects=["read"],
    overview="transform 在不同坐标参考系统(CRS)之间转换几何坐标。地理坐标系(EPSG:4326 WGS84)适合存储,投影坐标系(EPSG:3857 Web Mercator)适合计算。",
    params=[
        OperatorParam(name="input_df", type="dataframe", required=True, description="输入 DataFrame", notes="包含几何列"),
        OperatorParam(name="geom_column", type="str", required=False, description="几何列名", notes='默认"geom"'),
        OperatorParam(name="source_crs", type="str", required=False, description="源坐标系", notes='默认"EPSG:4326"(WGS84)'),
        OperatorParam(name="target_crs", type="str", required=False, description="目标坐标系", notes='默认"EPSG:3857"(Web Mercator)'),
        OperatorParam(name="output_column", type="str", required=False, description="输出几何列名", notes='默认"geom_transformed"')
    ],
    use_cases=[
        "缓冲区计算前置: 将 WGS84 坐标转为 EPSG:3857,缓冲500米更准确",
        "面积计算: 将 WGS84 多边形转为 EPSG:3857,计算平方米面积",
        "中国区域分析: 将 WGS84 转为 EPSG:4547(CGCS2000 / 3-degree Gauss-Kruger zone 39)进行中国区域精确计算",
        "Web 地图展示: 将其他坐标系转为 EPSG:3857,适配 Leaflet/OpenLayers"
    ],
    notes=[
        "EPSG:4326(WGS84)是最常见的地理坐标系,单位是度",
        "EPSG:3857(Web Mercator)是 Web 地图标准,单位是米",
        "中国推荐使用 EPSG:4547-4559(CGCS2000)系列投影",
        "坐标转换可能产生精度损失,不要反复转换"
    ],
    input_desc="DataFrame (包含 Geometry 列)",
    output_desc="DataFrame (新增转换后的几何列)",
    workflow_example={
        'id': 'transform_to_web_mercator',
        'operator': 'transform',
        'params': {
            'input_df': {'$ref': 'load_poi'},
            'geom_column': 'geom',
            'source_crs': 'EPSG:4326',
            'target_crs': 'EPSG:3857',
            'output_column': 'geom_3857'
        },
        'depends_on': ['load_poi']
    }
)


# ========================================
# 注册所有算子
# ========================================

OPERATORS = dict([
    register_operator(BUFFER_METADATA, buffer),
    register_operator(INTERSECTION_METADATA, intersection),
    register_operator(UNION_METADATA, union),
    register_operator(CENTROID_METADATA, centroid),
    register_operator(SPATIAL_JOIN_METADATA, spatial_join),
    register_operator(DISTANCE_METADATA, distance),
    register_operator(TRANSFORM_METADATA, transform)
])
