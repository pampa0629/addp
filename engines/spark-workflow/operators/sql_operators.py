"""
Spark 工作流SQL 算子
"""

import logging
from typing import Dict, Any, List, Optional
from pyspark.sql import SparkSession, DataFrame
from pyspark.sql import functions as F
from pyspark.sql.window import Window

from spark_connector import get_spark_connector
from storage_adapters import StorageAdapter
from .base import OperatorMetadata, OperatorParam, OperatorCategory, register_operator

logger = logging.getLogger(__name__)


# ========================================
# SQL 算子 (2个)
# ========================================

def sql(engine_id: int, query: str) -> DataFrame:
    """
    自由SQL查询算子

    Args:
        engine_id: Spark引擎ID
        query: SQL查询语句

    Returns:
        查询结果DataFrame

    Example:
        result = sql(
            engine_id=34,
            query="SELECT country, COUNT(*) as city_count FROM cities GROUP BY country"
        )
    """
    connector = get_spark_connector()
    spark = connector.get_or_create_session(engine_id)

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
# SQL 算子元数据 (Pydantic 格式)
# ========================================

SQL_METADATA = OperatorMetadata(
    name="sql",
    category=OperatorCategory.SQL,
    description="SQL查询",
    brief_description="执行自由 SQL 查询,支持复杂的表关联和聚合分析",
    overview="sql 算子允许执行任意 Apache Spark 查询,支持 SELECT、JOIN、GROUP BY、窗口函数等标准 SQL 语法。适合复杂的数据分析和多表关联场景,也支持 Apache Sedona 的空间 SQL 函数。",
    params=[
        OperatorParam(
            name="engine_id",
            type="int",
            required=True,
            description="Spark引擎ID",
            notes="确保资源处于活跃状态"
        ),
        OperatorParam(
            name="query",
            type="str",
            required=True,
            description="SQL查询语句",
            notes="必须是合法的 Apache Spark 语法,表需要提前注册为临时视图"
        )
    ],
    use_cases=[
        "复杂多表关联: 关联 3 张表(城市、POI、道路)进行综合分析,SQL 比算子链更简洁",
        "空间 SQL 分析: SELECT ST_Buffer(geom, 500) FROM poi WHERE category=\"学校\",结合空间函数和过滤条件",
        "统计汇总: SELECT province, COUNT(*) as city_count, SUM(population) as total_pop FROM cities GROUP BY province",
        "窗口函数排名: SELECT *, ROW_NUMBER() OVER (PARTITION BY province ORDER BY population DESC) as rank FROM cities"
    ],
    notes=[
        "表必须先通过 create_temp_view 注册为临时视图",
        "支持 Sedona 空间 SQL 函数(ST_Buffer、ST_Intersection 等)",
        "查询结果返回 DataFrame,可继续用算子处理",
        "复杂查询可能触发多阶段计算,注意性能"
    ],
    input_desc="无(通过 SQL FROM 子句引用临时视图)",
    output_desc="DataFrame (查询结果)",
    workflow_example={
        "id": "sql_city_analysis",
        "operator": "sql",
        "params": {
            "engine_id": 34,
            "query": "SELECT province, COUNT(*) as city_count FROM cities GROUP BY province"
        },
        "depends_on": ["create_cities_view"]
    }
)

CREATE_TEMP_VIEW_METADATA = OperatorMetadata(
    name="create_temp_view",
    category=OperatorCategory.SQL,
    description="创建临时视图",
    brief_description="将 DataFrame 注册为 SQL 临时视图,供后续 SQL 查询使用",
    overview="create_temp_view 将 DataFrame 注册为 Apache Spark 临时视图,使其可以在 sql 算子中通过表名引用。视图在 Spark Session 生命周期内有效,支持替换已存在的同名视图。",
    params=[
        OperatorParam(
            name="input_df",
            type="dataframe",
            required=True,
            description="输入 DataFrame",
            notes="必须是有效的 Spark DataFrame"
        ),
        OperatorParam(
            name="view_name",
            type="str",
            required=True,
            description="视图名称",
            notes="命名规则同 SQL 表名,建议小写字母+下划线"
        )
    ],
    use_cases=[
        "多表关联准备: 将 3 个 DataFrame(cities、poi、roads)分别注册为视图,然后用 sql 算子关联",
        "复杂过滤条件: 将 load 的原始数据注册为视图,用 SQL WHERE 子句实现复杂过滤",
        "空间 SQL 桥接: 将算子处理后的结果注册为视图,供空间 SQL 函数进一步分析",
        "工作流分支: 将中间结果注册为视图,供多个下游 SQL 查询复用"
    ],
    notes=[
        "视图名称在 Session 内全局可见,避免命名冲突",
        "createOrReplaceTempView 会覆盖同名视图",
        "视图仅在当前 Spark Session 有效,Session 结束后失效",
        "视图不占用额外存储,仅是 DataFrame 的别名"
    ],
    input_desc="DataFrame",
    output_desc="Dict (执行结果: status, view_name)",
    workflow_example={
        "id": "create_cities_view",
        "operator": "create_temp_view",
        "params": {
            "input_df": {"$ref": "load_cities"},
            "view_name": "cities"
        },
        "depends_on": ["load_cities"]
    }
)

# ========================================
# 注册算子
# ========================================

OPERATORS = dict([
    register_operator(SQL_METADATA, sql),
    register_operator(CREATE_TEMP_VIEW_METADATA, create_temp_view)
])
