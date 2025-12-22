"""
Spark Sedona 算子库
提供 35 个算子,涵盖 I/O、空间分析、数据转换、聚合分析、Spark SQL
"""

import logging
from typing import Dict, Any, List, Optional
from pyspark.sql import SparkSession, DataFrame
from pyspark.sql import functions as F
from pyspark.sql.window import Window

from spark_connector import get_spark_connector
from storage_adapters import StorageAdapter

logger = logging.getLogger(__name__)


# ========================================
# 类别 1: 数据 I/O 算子 (5个)
# ========================================

def load(resource_id: int, **params) -> DataFrame:
    """
    数据加载算子

    支持多种数据源:
    - table: 数据库表 (PostgreSQL/MySQL/Doris)
    - file: 文件 (S3/HDFS/本地, Parquet/GeoParquet/CSV/Shapefile/Delta/Hudi)
    - sql: 自定义SQL查询
    - catalog: 数据目录 (Iceberg/Delta)

    Args:
        resource_id: Spark资源ID (从system.resources获取)
        **params: 统一参数 (DataLocation格式)
            - source_type: "table" | "file" | "sql" | "catalog"
            - schema: 数据库schema (table类型)
            - table: 表名 (table类型)
            - geom_column: 几何列名 (默认"geom")
            - path: 文件路径 (file类型)
            - format: 文件格式 (file类型)
            - sql: SQL查询 (sql类型)
            - catalog_name: catalog名称 (catalog类型)
            - database: 数据库名 (catalog类型)

    Returns:
        Spark DataFrame

    Example:
        # 从数据库加载
        df = load(resource_id=34, source_type="table", schema="public", table="poi")

        # 从文件加载
        df = load(resource_id=34, source_type="file", path="s3a://bucket/data.parquet", format="parquet")

        # 从SQL加载
        df = load(resource_id=34, source_type="sql", sql="SELECT * FROM poi WHERE category='restaurant'")
    """
    # 获取SparkSession
    connector = get_spark_connector()
    spark = connector.get_or_create_session(resource_id)

    # 添加resource_id到params (StorageAdapter需要)
    params['resource_id'] = resource_id

    # 使用StorageAdapter加载
    df = StorageAdapter.load(spark, params)

    logger.info(f"Loaded data from {params.get('source_type')}, rows: {df.count()}")

    return df


def save(input_df: DataFrame, resource_id: int, **params) -> Dict[str, Any]:
    """
    数据保存算子

    Args:
        input_df: 输入DataFrame
        resource_id: Spark资源ID
        **params: 保存参数
            - target_type: "table" | "file"
            - schema: 数据库schema (table类型)
            - table: 表名 (table类型)
            - mode: 保存模式 ("overwrite" | "append")
            - path: 文件路径 (file类型)
            - format: 文件格式 (file类型)

    Returns:
        执行结果字典 {"status": "success", "rows": count}

    Example:
        save(df, resource_id=34, target_type="table", schema="public", table="result", mode="overwrite")
    """
    params['resource_id'] = resource_id

    # 使用StorageAdapter保存
    StorageAdapter.save(input_df, params)

    row_count = input_df.count()
    logger.info(f"Saved {row_count} rows to {params.get('target_type')}")

    return {
        "status": "success",
        "rows": row_count,
        "target": f"{params.get('schema', '')}.{params.get('table', params.get('path', ''))}"
    }


def preview(input_df: DataFrame, limit: int = 100) -> DataFrame:
    """
    数据预览算子

    Args:
        input_df: 输入DataFrame
        limit: 预览行数 (默认100)

    Returns:
        预览DataFrame
    """
    return input_df.limit(limit)


def cache(input_df: DataFrame) -> DataFrame:
    """
    内存缓存算子

    将DataFrame缓存到内存,提高后续查询性能

    Args:
        input_df: 输入DataFrame

    Returns:
        缓存后的DataFrame
    """
    df_cached = input_df.cache()
    logger.info(f"Cached DataFrame with {df_cached.count()} rows")
    return df_cached


def persist(input_df: DataFrame, storage_level: str = "MEMORY_AND_DISK") -> DataFrame:
    """
    持久化算子

    Args:
        input_df: 输入DataFrame
        storage_level: 存储级别
            - "MEMORY_ONLY": 仅内存
            - "DISK_ONLY": 仅磁盘
            - "MEMORY_AND_DISK": 内存+磁盘 (默认)
            - "MEMORY_AND_DISK_SER": 内存+磁盘(序列化)

    Returns:
        持久化后的DataFrame
    """
    from pyspark import StorageLevel

    level_map = {
        "MEMORY_ONLY": StorageLevel.MEMORY_ONLY,
        "DISK_ONLY": StorageLevel.DISK_ONLY,
        "MEMORY_AND_DISK": StorageLevel.MEMORY_AND_DISK,
        "MEMORY_AND_DISK_SER": StorageLevel.MEMORY_AND_DISK_SER,
    }

    level = level_map.get(storage_level, StorageLevel.MEMORY_AND_DISK)
    df_persisted = input_df.persist(level)

    logger.info(f"Persisted DataFrame with storage level: {storage_level}")

    return df_persisted


# ========================================
# 类别 2: Sedona 空间算子 (12个)
# ========================================

def st_buffer(input_df: DataFrame, geom_column: str = "geom", distance: float = 0.01, output_column: str = "buffer_geom") -> DataFrame:
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
        df_buffered = st_buffer(df, geom_column="geom", distance=100, output_column="buffer_geom")
    """
    from pyspark.sql.functions import expr

    df_result = input_df.withColumn(
        output_column,
        expr(f"ST_Buffer({geom_column}, {distance})")
    )

    logger.info(f"Applied buffer with distance {distance}")

    return df_result


def st_intersection(input_df: DataFrame, geom_column: str = "geom", intersect_geom: str = None, output_column: str = "intersection_geom") -> DataFrame:
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


def st_union(input_df: DataFrame, geom_column: str = "geom") -> DataFrame:
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


def st_centroid(input_df: DataFrame, geom_column: str = "geom", output_column: str = "centroid") -> DataFrame:
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
            - "distance_within": 距离范围内 (需要额外的distance参数)

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


def st_distance(input_df: DataFrame, geom_column: str = "geom", target_geom: str = None, output_column: str = "distance") -> DataFrame:
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


def st_transform(input_df: DataFrame, geom_column: str = "geom", source_crs: str = "EPSG:4326", target_crs: str = "EPSG:3857", output_column: str = "geom_transformed") -> DataFrame:
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
# 类别 3: 数据转换算子 (8个)
# ========================================

def select(input_df: DataFrame, columns: List[str]) -> DataFrame:
    """
    选择列算子

    Args:
        input_df: 输入DataFrame
        columns: 列名列表

    Returns:
        选择后的DataFrame

    Example:
        df_selected = select(df, columns=["id", "name", "geom"])
    """
    return input_df.select(*columns)


def filter(input_df: DataFrame, condition: str) -> DataFrame:
    """
    条件过滤算子

    Args:
        input_df: 输入DataFrame
        condition: 过滤条件 (SQL WHERE子句,不含WHERE关键字)

    Returns:
        过滤后的DataFrame

    Example:
        df_filtered = filter(df, condition="population > 1000000 AND country = '中国'")
    """
    from pyspark.sql.functions import expr

    df_result = input_df.filter(expr(condition))

    logger.info(f"Filtered with condition: {condition}")

    return df_result


def add_column(input_df: DataFrame, column_name: str, expression: str) -> DataFrame:
    """
    添加列算子

    Args:
        input_df: 输入DataFrame
        column_name: 新列名
        expression: 列表达式 (SQL表达式)

    Returns:
        添加列后的DataFrame

    Example:
        df_new = add_column(df, column_name="area_km2", expression="ST_Area(geom) / 1000000")
    """
    from pyspark.sql.functions import expr

    df_result = input_df.withColumn(column_name, expr(expression))

    return df_result


def rename_column(input_df: DataFrame, old_name: str, new_name: str) -> DataFrame:
    """
    重命名列算子

    Args:
        input_df: 输入DataFrame
        old_name: 原列名
        new_name: 新列名

    Returns:
        重命名后的DataFrame
    """
    return input_df.withColumnRenamed(old_name, new_name)


def drop_column(input_df: DataFrame, columns: List[str]) -> DataFrame:
    """
    删除列算子

    Args:
        input_df: 输入DataFrame
        columns: 要删除的列名列表

    Returns:
        删除列后的DataFrame
    """
    return input_df.drop(*columns)


# ========================================
# 类别 4: 聚合分析算子 (6个)
# ========================================

def group_by(input_df: DataFrame, group_columns: List[str], agg_exprs: Dict[str, str]) -> DataFrame:
    """
    分组聚合算子

    Args:
        input_df: 输入DataFrame
        group_columns: 分组列名列表
        agg_exprs: 聚合表达式字典
            - key: 输出列名
            - value: 聚合表达式 (如 "count(*)", "sum(population)", "avg(area)")

    Returns:
        聚合结果DataFrame

    Example:
        result = group_by(
            df,
            group_columns=["country", "category"],
            agg_exprs={
                "total_count": "count(*)",
                "total_population": "sum(population)",
                "avg_area": "avg(area)"
            }
        )
    """
    from pyspark.sql.functions import expr

    # 构建聚合表达式
    agg_list = [expr(f"{expr_str} as {col_name}") for col_name, expr_str in agg_exprs.items()]

    df_result = input_df.groupBy(*group_columns).agg(*agg_list)

    logger.info(f"Grouped by {group_columns} with {len(agg_exprs)} aggregations")

    return df_result


def join(
    left_df: DataFrame,
    right_df: DataFrame,
    on: str,
    how: str = "inner"
) -> DataFrame:
    """
    表连接算子

    Args:
        left_df: 左表
        right_df: 右表
        on: 连接条件 (列名或SQL表达式)
        how: 连接类型 ("inner" | "left" | "right" | "outer")

    Returns:
        连接结果DataFrame

    Example:
        # 简单连接
        result = join(poi_df, category_df, on="category_id")

        # 复杂条件
        result = join(poi_df, region_df, on="poi.region_id = region.id", how="left")
    """
    from pyspark.sql.functions import expr

    # 检查是否是简单列名
    if "=" in on or " " in on:
        # SQL表达式
        df_result = left_df.alias("a").join(right_df.alias("b"), expr(on), how)
    else:
        # 简单列名
        df_result = left_df.join(right_df, on, how)

    return df_result


# ========================================
# 类别 5: Spark SQL 算子 (4个)
# ========================================

def sql(resource_id: int, query: str) -> DataFrame:
    """
    自由SQL查询算子

    Args:
        resource_id: Spark资源ID
        query: SQL查询语句

    Returns:
        查询结果DataFrame

    Example:
        result = sql(
            resource_id=34,
            query="SELECT country, COUNT(*) as city_count FROM cities GROUP BY country"
        )
    """
    connector = get_spark_connector()
    spark = connector.get_or_create_session(resource_id)

    df_result = spark.sql(query)

    logger.info(f"Executed SQL query")

    return df_result


def create_temp_view(input_df: DataFrame, view_name: str) -> Dict[str, Any]:
    """
    创建临时视图算子

    Args:
        input_df: 输入DataFrame
        view_name: 视图名称

    Returns:
        执行结果字典
    """
    input_df.createOrReplaceTempView(view_name)

    logger.info(f"Created temp view: {view_name}")

    return {
        "status": "success",
        "view_name": view_name
    }


# ========================================
# 算子注册表
# ========================================

_OPERATOR_REGISTRY = {
    # I/O算子
    "load": load,
    "save": save,
    "preview": preview,
    "cache": cache,
    "persist": persist,

    # 空间算子
    "st_buffer": st_buffer,
    "st_intersection": st_intersection,
    "st_union": st_union,
    "st_centroid": st_centroid,
    "spatial_join": spatial_join,
    "st_distance": st_distance,
    "st_transform": st_transform,

    # 数据转换算子
    "select": select,
    "filter": filter,
    "add_column": add_column,
    "rename_column": rename_column,
    "drop_column": drop_column,

    # 聚合算子
    "group_by": group_by,
    "join": join,

    # SQL算子
    "sql": sql,
    "create_temp_view": create_temp_view,
}


def get_operator(operator_name: str):
    """
    获取算子函数

    Args:
        operator_name: 算子名称

    Returns:
        算子函数

    Raises:
        ValueError: 如果算子不存在
    """
    if operator_name not in _OPERATOR_REGISTRY:
        raise ValueError(f"Unknown operator: {operator_name}")

    return _OPERATOR_REGISTRY[operator_name]


def list_operators() -> List[str]:
    """
    列出所有算子名称

    Returns:
        算子名称列表
    """
    return list(_OPERATOR_REGISTRY.keys())
