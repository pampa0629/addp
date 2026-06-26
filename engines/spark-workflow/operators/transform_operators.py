"""
Spark 工作流数据转换算子
"""

import logging
from typing import Dict, Any, List, Optional

from .base import OperatorMetadata, OperatorParam, OperatorCategory, register_operator
from .spark_types import DataFrame

logger = logging.getLogger(__name__)


# ========================================
# 数据转换算子 (5个)
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
# 数据转换算子元数据 (Pydantic 格式)
# ========================================

SELECT_METADATA = OperatorMetadata(
    name='select',
    category=OperatorCategory.DATA_TRANSFORM,
    description='选择列',
    brief_description='从 DataFrame 中选择指定的列,类似 SQL SELECT',
    execution_modes=["workflow"],
    overview='select 算子用于投影操作,从 DataFrame 中选择需要的列,丢弃其他列。支持简单列名和表达式,常用于减少数据传输量和提取关键字段。',
    params=[
        OperatorParam(
            name='input_df',
            type='dataframe',
            required=True,
            description='输入 DataFrame',
            notes='必须包含所选列'
        ),
        OperatorParam(
            name='columns',
            type='list',
            required=True,
            description='要选择的列名列表',
            notes='区分大小写,列名不存在会报错'
        )
    ],
    use_cases=[
        '字段裁剪: 从 50 列的宽表中选择 5 个关键列,减少 90% 数据传输量',
        '几何列提取: 从 500万行复杂 POI 表中仅选择 id、name、geom 三列,用于空间分析',
        '属性表简化: 从 100万条道路网络表选择 road_id、length、level 列,生成简化的属性表',
        '前端展示优化: 从百列数据表中选择 10 个展示列,加快前端渲染'
    ],
    notes=[
        'select 不修改列顺序,按列表顺序输出',
        '选择不存在的列会抛出 AnalysisException',
        '可以重复选择同一列,结果会包含重复列',
        '配合 add_column 使用可实现列重命名+选择'
    ],
    input_desc='DataFrame',
    output_desc='DataFrame (仅包含选定列)',
    workflow_example={
        'id': 'select_key_columns',
        'operator': 'select',
        'params': {
            'input_df': {'$ref': 'load_poi'},
            'columns': ['id', 'name', 'category', 'geom']
        },
        'depends_on': ['load_poi']
    }
)

FILTER_METADATA = OperatorMetadata(
    name='filter',
    category=OperatorCategory.DATA_TRANSFORM,
    description='条件过滤',
    brief_description='根据条件表达式过滤行,类似 SQL WHERE',
    execution_modes=["workflow"],
    overview='filter 算子根据条件表达式过滤 DataFrame 的行,保留满足条件的记录。支持复杂的逻辑运算(AND/OR/NOT)和丰富的比较操作(=、>、<、IN、LIKE 等)。',
    params=[
        OperatorParam(
            name='input_df',
            type='dataframe',
            required=True,
            description='输入 DataFrame',
            notes='包含待过滤的数据'
        ),
        OperatorParam(
            name='condition',
            type='str',
            required=True,
            description='过滤条件表达式(SQL WHERE 子句,不含 WHERE)',
            notes='如 "population > 1000000 AND province = \'北京\'"'
        )
    ],
    use_cases=[
        '人口筛选: 从 300 个城市中筛选人口超过 100万的城市,过滤后剩余 50 个',
        'POI 分类过滤: 从 500万 POI 中筛选餐饮类(category="餐饮"),输出 120万条',
        '空间范围过滤: 筛选经度在 116-117、纬度在 39-40 的点位,提取北京市数据',
        '组合条件: 筛选评分>4.5 且评论数>100 的高质量 POI'
    ],
    notes=[
        '字符串值需要用单引号,如 category=\'餐饮\'',
        '支持 IN 操作: category IN (\'餐饮\', \'购物\')',
        '支持 LIKE 模糊匹配: name LIKE \'%酒店%\'',
        'NULL 值判断用 IS NULL / IS NOT NULL'
    ],
    input_desc='DataFrame',
    output_desc='DataFrame (满足条件的行)',
    workflow_example={
        'id': 'filter_megacities',
        'operator': 'filter',
        'params': {
            'input_df': {'$ref': 'load_cities'},
            'condition': 'population > 1000000 AND province IN (\'北京\', \'上海\', \'广东\')'
        },
        'depends_on': ['load_cities']
    }
)

ADD_COLUMN_METADATA = OperatorMetadata(
    name='add_column',
    category=OperatorCategory.DATA_TRANSFORM,
    description='添加列',
    brief_description='通过表达式计算添加新列,支持算术运算和函数调用',
    execution_modes=["workflow"],
    overview='add_column 算子通过 SQL 表达式计算生成新列,支持算术运算、字符串操作、日期函数和空间函数。新列会追加到 DataFrame 末尾,不影响原有列。',
    params=[
        OperatorParam(
            name='input_df',
            type='dataframe',
            required=True,
            description='输入 DataFrame',
            notes='表达式中引用的列必须存在'
        ),
        OperatorParam(
            name='column_name',
            type='str',
            required=True,
            description='新列名',
            notes='如果列名已存在会覆盖原列'
        ),
        OperatorParam(
            name='expression',
            type='str',
            required=True,
            description='计算表达式(SQL 表达式)',
            notes='如 "price * 0.8" 或 "ST_Area(geom) / 1000000"'
        )
    ],
    use_cases=[
        '面积单位转换: 添加 area_km2 列,表达式 "ST_Area(geom) / 1000000",将平方米转为平方公里',
        '价格折扣: 添加 discount_price 列,表达式 "price * 0.8",计算8折价格',
        '人口密度: 为 300 个城市添加 density 列,表达式 "population / area",计算人口密度',
        '时间解析: 为 5 年轨迹明细添加 year 列,表达式 "year(create_time)",提取年份'
    ],
    notes=[
        '列名已存在时会覆盖原列,无警告',
        '表达式错误会导致整个作业失败',
        '空间函数(ST_*)需要几何列为 Geometry 类型',
        '复杂表达式可能影响性能,注意优化'
    ],
    input_desc='DataFrame',
    output_desc='DataFrame (新增一列)',
    workflow_example={
        'id': 'add_area_column',
        'operator': 'add_column',
        'params': {
            'input_df': {'$ref': 'load_polygons'},
            'column_name': 'area_km2',
            'expression': 'ST_Area(geom) / 1000000'
        },
        'depends_on': ['load_polygons']
    }
)

RENAME_COLUMN_METADATA = OperatorMetadata(
    name='rename_column',
    category=OperatorCategory.DATA_TRANSFORM,
    description='重命名列',
    brief_description='修改指定列名,保持列内容和行顺序不变',
    execution_modes=["workflow"],
    overview='rename_column 算子修改列名,常用于规范化字段命名、避免列名冲突、适配下游系统要求。操作高效,不涉及数据复制。',
    params=[
        OperatorParam(
            name='input_df',
            type='dataframe',
            required=True,
            description='输入 DataFrame',
            notes='必须包含原列名'
        ),
        OperatorParam(
            name='old_name',
            type='str',
            required=True,
            description='原列名',
            notes='区分大小写,列名不存在会报错'
        ),
        OperatorParam(
            name='new_name',
            type='str',
            required=True,
            description='新列名',
            notes='建议使用小写字母+下划线'
        )
    ],
    use_cases=[
        '中文列名转英文: 将 20 个中文指标列重命名为英文列名,适配国际化系统',
        '缩写规范化: 将 5 个缩写列如 pop 重命名为 population,提高可读性',
        '避免列名冲突: JOIN 前将 2 张表的 id 重命名为 city_id 和 region_id,避免冲突',
        '几何列标准化: 将 10 个来源表的 geometry 重命名为 geom,符合 Sedona 惯例'
    ],
    notes=[
        '原列名不存在会抛出 AnalysisException',
        '新列名已存在会导致重名,Spark 不报错但可能引起混淆',
        'rename 操作不涉及数据复制,性能开销极小',
        '建议在工作流早期统一列名,避免后续混乱'
    ],
    input_desc='DataFrame',
    output_desc='DataFrame (列名已修改)',
    workflow_example={
        'id': 'rename_geom_column',
        'operator': 'rename_column',
        'params': {
            'input_df': {'$ref': 'load_shapefile'},
            'old_name': 'geometry',
            'new_name': 'geom'
        },
        'depends_on': ['load_shapefile']
    }
)

DROP_COLUMN_METADATA = OperatorMetadata(
    name='drop_column',
    category=OperatorCategory.DATA_TRANSFORM,
    description='删除列',
    brief_description='删除指定的列,减少数据体积和传输开销',
    execution_modes=["workflow"],
    overview='drop_column 算子删除一个或多个列,常用于清理冗余字段、减少数据传输量、保护敏感信息。操作高效,不涉及行过滤。',
    params=[
        OperatorParam(
            name='input_df',
            type='dataframe',
            required=True,
            description='输入 DataFrame',
            notes='包含待删除的列'
        ),
        OperatorParam(
            name='columns',
            type='list',
            required=True,
            description='要删除的列名列表',
            notes='列名不存在时会被忽略(不报错)'
        )
    ],
    use_cases=[
        '去除中间列: 删除 3 个 buffer_geom、temp_id 等临时计算列,清理最终结果',
        '敏感信息过滤: 删除 5 个 password、phone 等敏感列,符合数据安全要求',
        '冗余字段清理: 删除 create_time、update_time 等无关时间戳,减少 30% 数据量',
        '宽表瘦身: 从 100 列宽表中删除 80 个无用列,提升查询性能'
    ],
    notes=[
        '删除不存在的列不会报错,Spark 会静默忽略',
        '删除后无法恢复,建议先 cache 原始数据',
        '删除几何列后无法再进行空间分析',
        'drop_column 与 select 效果类似,但语义相反'
    ],
    input_desc='DataFrame',
    output_desc='DataFrame (已删除指定列)',
    workflow_example={
        'id': 'drop_temp_columns',
        'operator': 'drop_column',
        'params': {
            'input_df': {'$ref': 'buffer_analysis'},
            'columns': ['buffer_geom', 'temp_distance', 'create_time']
        },
        'depends_on': ['buffer_analysis']
    }
)

# 使用 register_operator 构建 OPERATORS 字典
OPERATORS = dict([
    register_operator(SELECT_METADATA, select),
    register_operator(FILTER_METADATA, filter),
    register_operator(ADD_COLUMN_METADATA, add_column),
    register_operator(RENAME_COLUMN_METADATA, rename_column),
    register_operator(DROP_COLUMN_METADATA, drop_column)
])
