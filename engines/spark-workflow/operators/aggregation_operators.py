"""
Spark 工作流聚合分析算子
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
# 聚合分析算子 (2个)
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
# 聚合算子元数据
# ========================================

# group_by 算子元数据
GROUP_BY_METADATA = OperatorMetadata(
    name="group_by",
    category=OperatorCategory.AGGREGATION,
    description="分组聚合",
    brief_description="按指定列分组并进行聚合计算,支持多种聚合函数组合",
    overview="group_by 算子实现 SQL GROUP BY 功能,按一列或多列分组,对每组数据进行聚合计算。支持 count、sum、avg、min、max 等标准聚合函数,也支持复杂的自定义表达式。",
    params=[
        OperatorParam(
            name="input_df",
            type="dataframe",
            required=True,
            description="输入 DataFrame",
            notes="必须包含分组列和聚合列"
        ),
        OperatorParam(
            name="group_columns",
            type="list",
            required=True,
            description="分组列名列表",
            notes="支持单列或多列,区分大小写"
        ),
        OperatorParam(
            name="agg_exprs",
            type="dict",
            required=True,
            description="聚合表达式字典 {输出列名: 聚合表达式}",
            notes='如 {"total": "sum(amount)", "avg_price": "avg(price)"}'
        )
    ],
    use_cases=[
        "省份统计: 按省份分组统计城市数量和总人口,GROUP BY province, 聚合 COUNT(*) 和 SUM(population)",
        "POI 分类汇总: 按类别(餐饮/购物/娱乐)分组统计 POI 数量和平均评分,输出 3 行统计结果",
        "空间密度分析: 按网格 ID 分组统计点位数量,找出密度最高的 10 个网格",
        "时序聚合: 按日期和省份双维度分组,统计每天各省的订单量和销售额"
    ],
    notes=[
        "分组列中的 NULL 值会单独成组",
        '聚合表达式支持嵌套函数,如 "round(avg(price), 2)"',
        "大数据集分组可能导致数据倾斜,注意分区策略",
        "多列分组时注意列的组合基数,避免产生过多组"
    ],
    input_desc="DataFrame",
    output_desc="DataFrame (聚合结果,行数=分组数)",
    workflow_example={
        'id': 'province_stats',
        'operator': 'group_by',
        'params': {
            'input_df': {'$ref': 'load_cities'},
            'group_columns': ['province'],
            'agg_exprs': {
                'city_count': 'count(*)',
                'total_population': 'sum(population)',
                'avg_area': 'avg(area)'
            }
        },
        'depends_on': ['load_cities']
    }
)

# join 算子元数据
JOIN_METADATA = OperatorMetadata(
    name="join",
    category=OperatorCategory.AGGREGATION,
    description="表连接",
    brief_description="关联两张表,支持内连接、左连接、右连接和全外连接",
    overview="join 算子实现 SQL JOIN 功能,根据连接条件关联两个 DataFrame。支持简单列名连接(on=\"id\")和复杂 SQL 表达式连接(on=\"a.id = b.user_id AND a.date = b.date\")。",
    params=[
        OperatorParam(
            name="left_df",
            type="dataframe",
            required=True,
            description="左表 DataFrame",
            notes="主表,left/right join 时作为基准表"
        ),
        OperatorParam(
            name="right_df",
            type="dataframe",
            required=True,
            description="右表 DataFrame",
            notes="关联表,列名冲突时会自动添加后缀"
        ),
        OperatorParam(
            name="on",
            type="str",
            required=True,
            description="连接条件",
            notes='简单列名(on="id")或 SQL 表达式(on="a.id = b.user_id")'
        ),
        OperatorParam(
            name="how",
            type="str",
            required=False,
            description="连接类型: inner/left/right/outer",
            notes="默认 inner,left 保留左表所有行"
        )
    ],
    use_cases=[
        "POI 类别关联: 将 100万 POI 点位表与类别字典表(50条)按 category_id 连接,补充类别名称",
        "空间属性关联: 将道路表与行政区表按 region_id 连接,为每条道路添加所属区域信息",
        "多表汇总: 将订单表、用户表、商品表 3 张表连接,生成订单明细报表",
        "左连接保全: 将城市表左连接 GDP 统计表,保留所有城市(即使没有 GDP 数据)"
    ],
    notes=[
        "inner join 只保留匹配的行,可能大幅减少数据量",
        "left join 保留左表所有行,右表不匹配时填充 NULL",
        "复杂条件连接时必须为表添加别名(a、b)",
        "大表连接注意 shuffle 性能,考虑 broadcast join 优化"
    ],
    input_desc="两个 DataFrame",
    output_desc="DataFrame (连接结果)",
    workflow_example={
        'id': 'poi_category_join',
        'operator': 'join',
        'params': {
            'left_df': {'$ref': 'load_poi'},
            'right_df': {'$ref': 'load_category'},
            'on': 'category_id',
            'how': 'inner'
        },
        'depends_on': ['load_poi', 'load_category']
    }
)

# 注册所有聚合算子
OPERATORS = dict([
    register_operator(GROUP_BY_METADATA, group_by),
    register_operator(JOIN_METADATA, join)
])
