"""
数据筛选算子模块

提供条件筛选、排序、去重、采样等非空间过滤操作
"""

from typing import Any, List, Dict, Union
import geopandas as gpd
from .base import (
    OperatorType,
    OperatorMetadata,
    OperatorParam,
    OperatorCategory,
    OutputPort,
    register_operator
)


# ========== 1. filter_by_attribute ==========
def filter_by_attribute(input_gdf: gpd.GeoDataFrame, field: str, operator: str, value: Any) -> gpd.GeoDataFrame:
    """
    属性条件筛选

    Args:
        input_gdf: 输入 GeoDataFrame
        field: 字段名
        operator: 比较运算符（'>', '<', '>=', '<=', '==', '!=', 'in', 'not in'）
        value: 比较值

    Returns:
        筛选后的 GeoDataFrame
    """
    result = input_gdf.copy()

    if operator == '>':
        return result[result[field] > value]
    elif operator == '<':
        return result[result[field] < value]
    elif operator == '>=':
        return result[result[field] >= value]
    elif operator == '<=':
        return result[result[field] <= value]
    elif operator == '==':
        return result[result[field] == value]
    elif operator == '!=':
        return result[result[field] != value]
    elif operator == 'in':
        return result[result[field].isin(value)]
    elif operator == 'not in':
        return result[~result[field].isin(value)]
    else:
        raise ValueError(f"Unsupported operator: {operator}")


FILTER_BY_ATTRIBUTE_METADATA = OperatorMetadata(
    name="filter_by_attribute",
    type=OperatorType.NON_SPATIAL,
    category=OperatorCategory.FILTER_OPERATION,
    description="属性条件筛选",
    brief_description="根据属性字段条件筛选要素,常用于数据过滤和条件查询",

    overview="根据属性字段的条件表达式筛选 GeoDataFrame 中的要素。"
              "支持常见的比较运算符（>, <, ==等）和集合运算符（in, not in）。不改变几何。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="field",
            type="param",
            data_type="str",
            required=True,
            description="筛选字段名"
        ),
        OperatorParam(
            name="operator",
            type="param",
            data_type="str",
            required=True,
            description="比较运算符",
            notes="支持: '>', '<', '>=', '<=', '==', '!=', 'in', 'not in'"
        ),
        OperatorParam(
            name="value",
            type="param",
            data_type="object",
            required=True,
            description="比较值（数值、字符串或列表）",
            notes="使用 in/not in 时需传入列表"
        )
    ],

    use_cases=[
        "筛选大面积地块: field='area', operator='>', value=1000",
        "筛选特定类型: field='type', operator='==', value='residential'",
        "筛选多个城市: field='city', operator='in', value=['北京', '上海', '广州']",
        "排除特定值: field='status', operator='!=', value='deleted'"
    ],

    notes=[
        "筛选前确保字段存在,否则会抛出 KeyError",
        "数值比较需确保字段类型正确,可先用 type_cast 转换",
        "in/not in 运算符要求 value 参数为列表类型",
        "筛选后索引不会重置,如需连续索引可用 reset_index()"
    ],

    workflow_example={
        'id': 'filter_large_areas',
        'operator': 'filter_by_attribute',
        'params': {
            'input_gdf': {'$ref': 'get_area'},
            'field': 'area',
            'operator': '>',
            'value': 1000
        },
        'depends_on': ['get_area']
    }
)


# ========== 2. sort_by_field ==========
def sort_by_field(input_gdf: gpd.GeoDataFrame, field: str, ascending: bool = True) -> gpd.GeoDataFrame:
    """
    按字段排序

    Args:
        input_gdf: 输入 GeoDataFrame
        field: 排序字段名
        ascending: 是否升序（默认 True）

    Returns:
        排序后的 GeoDataFrame
    """
    result = input_gdf.copy()
    result = result.sort_values(by=field, ascending=ascending).reset_index(drop=True)
    return result


SORT_BY_FIELD_METADATA = OperatorMetadata(
    name="sort_by_field",
    type=OperatorType.NON_SPATIAL,
    category=OperatorCategory.FILTER_OPERATION,
    description="按字段排序",
    brief_description="根据指定字段对数据排序,常用于数据展示和排名分析",

    overview="根据指定字段对 GeoDataFrame 进行升序或降序排序。"
              "排序后重置索引确保连续。常用于 Top N 分析、数据展示、优先级排序等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="field",
            type="param",
            data_type="str",
            required=True,
            description="排序字段名"
        ),
        OperatorParam(
            name="ascending",
            type="param",
            data_type="bool",
            required=False,
            description="是否升序（默认 True）",
            notes="True: 升序（从小到大）, False: 降序（从大到小）"
        )
    ],

    use_cases=[
        "面积降序: field='area', ascending=False 找出最大地块",
        "人口升序: field='population', ascending=True 从少到多排序",
        "价格降序: field='price', ascending=False 高价优先展示",
        "时间升序: field='created_at', ascending=True 按时间顺序排列"
    ],

    notes=[
        "排序后索引会被重置为 0, 1, 2, ...",
        "NaN 值默认排在末尾",
        "大数据集排序可能较慢,建议先筛选再排序",
        "排序不改变几何列,只调整行顺序"
    ],

    workflow_example={
        'id': 'sort_by_area',
        'operator': 'sort_by_field',
        'params': {
            'input_gdf': {'$ref': 'get_area'},
            'field': 'area',
            'ascending': False
        },
        'depends_on': ['get_area']
    }
)


# ========== 3. select_top_n ==========
def select_top_n(input_gdf: gpd.GeoDataFrame, field: str, n: int, ascending: bool = False) -> gpd.GeoDataFrame:
    """
    选择 Top N

    Args:
        input_gdf: 输入 GeoDataFrame
        field: 排序字段名
        n: 选择数量
        ascending: 是否升序（默认 False，即选择最大的 N 个）

    Returns:
        Top N 记录
    """
    result = input_gdf.copy()
    result = result.sort_values(by=field, ascending=ascending).head(n).reset_index(drop=True)
    return result


SELECT_TOP_N_METADATA = OperatorMetadata(
    name="select_top_n",
    type=OperatorType.NON_SPATIAL,
    category=OperatorCategory.FILTER_OPERATION,
    description="选择 Top N",
    brief_description="选择排名前 N 的记录,常用于排名分析和数据筛选",

    overview="根据指定字段排序后选择前 N 条记录。默认选择最大的 N 个（降序）。"
              "常用于 Top 10 分析、重点区域筛选、异常值识别等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="field",
            type="param",
            data_type="str",
            required=True,
            description="排序字段名"
        ),
        OperatorParam(
            name="n",
            type="param",
            data_type="int",
            required=True,
            description="选择数量"
        ),
        OperatorParam(
            name="ascending",
            type="param",
            data_type="bool",
            required=False,
            description="是否升序（默认 False）",
            notes="False: 选择最大的 N 个, True: 选择最小的 N 个"
        )
    ],

    use_cases=[
        "前 10 大地块: field='area', n=10, ascending=False",
        "前 20 高人口区: field='population', n=20, ascending=False",
        "前 5 低价地块: field='price', n=5, ascending=True",
        "前 100 近距离点: field='distance', n=100, ascending=True"
    ],

    notes=[
        "如果记录总数小于 N,返回所有记录",
        "结果索引会被重置为 0, 1, 2, ..., N-1",
        "等效于 sort_by_field + head(n) 的组合",
        "大数据集中该算子性能优于先排序全部数据"
    ],

    workflow_example={
        'id': 'top10_areas',
        'operator': 'select_top_n',
        'params': {
            'input_gdf': {'$ref': 'get_area'},
            'field': 'area',
            'n': 10,
            'ascending': False
        },
        'depends_on': ['get_area']
    }
)


# ========== 4. drop_duplicates ==========
def drop_duplicates(input_gdf: gpd.GeoDataFrame, field_names: List[str] = None, keep: str = 'first') -> gpd.GeoDataFrame:
    """
    去重

    Args:
        input_gdf: 输入 GeoDataFrame
        field_names: 去重依据字段（None 表示所有字段）
        keep: 保留策略（'first', 'last', False）

    Returns:
        去重后的 GeoDataFrame
    """
    result = input_gdf.copy()
    result = result.drop_duplicates(subset=field_names, keep=keep).reset_index(drop=True)
    return result


DROP_DUPLICATES_METADATA = OperatorMetadata(
    name="drop_duplicates",
    type=OperatorType.NON_SPATIAL,
    category=OperatorCategory.FILTER_OPERATION,
    description="去重",
    brief_description="删除重复记录,常用于数据清洗和唯一性保证",

    overview="根据指定字段删除重复记录。可以基于单个或多个字段去重，"
              "支持保留首次出现、最后出现或删除所有重复。常用于数据清洗、ID 唯一性保证等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="field_names",
            type="param",
            data_type="list[str]",
            required=False,
            description="去重依据字段（None 表示所有字段）",
            notes="示例: ['id'] 或 ['name', 'type']"
        ),
        OperatorParam(
            name="keep",
            type="param",
            data_type="str",
            required=False,
            description="保留策略（默认 'first'）",
            notes="'first': 保留首次出现, 'last': 保留最后出现, False: 删除所有重复"
        )
    ],

    use_cases=[
        "按 ID 去重: field_names=['id'], keep='first' 保留每个 ID 的首条记录",
        "按名称+类型去重: field_names=['name', 'type'] 删除名称和类型都相同的记录",
        "完全去重: field_names=None 删除所有字段完全相同的记录",
        "保留最新记录: 先按时间排序,再 field_names=['id'], keep='last'"
    ],

    notes=[
        "去重后索引会被重置为连续整数",
        "geometry 字段也参与比较（field_names=None 时）",
        "keep=False 会删除所有重复项（包括首次出现）",
        "大数据集去重可能较慢,建议只对必要字段去重"
    ],

    workflow_example={
        'id': 'dedup_by_id',
        'operator': 'drop_duplicates',
        'params': {
            'input_gdf': {'$ref': 'load_data'},
            'field_names': ['id'],
            'keep': 'first'
        },
        'depends_on': ['load_data']
    }
)


# ========== 5. sample ==========
def sample(input_gdf: gpd.GeoDataFrame, n: int = None, frac: float = None, random_state: int = None) -> gpd.GeoDataFrame:
    """
    随机采样

    Args:
        input_gdf: 输入 GeoDataFrame
        n: 采样数量（与 frac 二选一）
        frac: 采样比例 0-1（与 n 二选一）
        random_state: 随机种子

    Returns:
        采样后的 GeoDataFrame
    """
    if n is None and frac is None:
        raise ValueError("Must specify either 'n' or 'frac'")

    result = input_gdf.copy()
    result = result.sample(n=n, frac=frac, random_state=random_state).reset_index(drop=True)
    return result


SAMPLE_METADATA = OperatorMetadata(
    name="sample",
    type=OperatorType.NON_SPATIAL,
    category=OperatorCategory.FILTER_OPERATION,
    description="随机采样",
    brief_description="随机抽取指定数量或比例的记录,常用于数据探索和测试",

    overview="从 GeoDataFrame 中随机抽取指定数量或比例的记录。"
              "支持设置随机种子确保结果可复现。常用于数据探索、快速测试、采样分析等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="n",
            type="param",
            data_type="int",
            required=False,
            description="采样数量（与 frac 二选一）",
            notes="示例: 100 表示随机抽取 100 条记录"
        ),
        OperatorParam(
            name="frac",
            type="param",
            data_type="float",
            required=False,
            description="采样比例 0-1（与 n 二选一）",
            notes="示例: 0.1 表示随机抽取 10% 的记录"
        ),
        OperatorParam(
            name="random_state",
            type="param",
            data_type="int",
            required=False,
            description="随机种子（可选）",
            notes="设置固定值（如 42）可确保结果可复现"
        )
    ],

    use_cases=[
        "快速预览: n=100 随机抽取 100 条记录快速查看数据分布",
        "比例采样: frac=0.1 抽取 10% 数据用于快速测试算法",
        "可复现采样: n=1000, random_state=42 确保每次抽取相同记录",
        "减少数据量: frac=0.5 减少 50% 数据量加速后续处理"
    ],

    notes=[
        "n 和 frac 必须且只能指定一个",
        "如果 n 大于总记录数会报错",
        "采样后索引会被重置为连续整数",
        "不设置 random_state 时每次结果不同"
    ],

    workflow_example={
        'id': 'sample_data',
        'operator': 'sample',
        'params': {
            'input_gdf': {'$ref': 'load_data'},
            'n': 1000,
            'random_state': 42
        },
        'depends_on': ['load_data']
    }
)


# ========== 6. filter_by_geometry_type ==========
def filter_by_geometry_type(input_gdf: gpd.GeoDataFrame, geom_type: str) -> gpd.GeoDataFrame:
    """
    按几何类型过滤

    Args:
        input_gdf: 输入 GeoDataFrame
        geom_type: 几何类型（'Point', 'LineString', 'Polygon', 'MultiPoint', 'MultiLineString', 'MultiPolygon'）

    Returns:
        筛选后的 GeoDataFrame
    """
    result = input_gdf.copy()
    result = result[result.geometry.geom_type == geom_type].reset_index(drop=True)
    return result


FILTER_BY_GEOMETRY_TYPE_METADATA = OperatorMetadata(
    name="filter_by_geometry_type",
    type=OperatorType.NON_SPATIAL,
    category=OperatorCategory.FILTER_OPERATION,
    description="按几何类型过滤",
    brief_description="筛选指定几何类型的要素,常用于数据分类和类型检查",

    overview="根据几何类型（点、线、面等）筛选 GeoDataFrame 中的要素。"
              "常用于混合几何类型数据的分离、数据质量检查、类型匹配等场景。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="geom_type",
            type="param",
            data_type="str",
            required=True,
            description="几何类型",
            notes="支持: 'Point', 'LineString', 'Polygon', 'MultiPoint', 'MultiLineString', 'MultiPolygon'"
        )
    ],

    use_cases=[
        "只保留多边形: geom_type='Polygon' 过滤掉点和线要素",
        "提取点要素: geom_type='Point' 从混合数据中提取采样点",
        "过滤线要素: geom_type='LineString' 提取道路或河流",
        "数据质量检查: 确保数据集中只包含预期的几何类型"
    ],

    notes=[
        "几何类型名称区分大小写,必须精确匹配",
        "Multi* 类型与单类型不同,需分别筛选",
        "如果数据中没有指定类型,返回空 GeoDataFrame",
        "筛选后索引会被重置为连续整数"
    ],

    workflow_example={
        'id': 'filter_polygons',
        'operator': 'filter_by_geometry_type',
        'params': {
            'input_gdf': {'$ref': 'load_data'},
            'geom_type': 'Polygon'
        },
        'depends_on': ['load_data']
    }
)


# ========== 7. drop_null_geometry ==========
def drop_null_geometry(input_gdf: gpd.GeoDataFrame) -> gpd.GeoDataFrame:
    """
    删除空几何

    Args:
        input_gdf: 输入 GeoDataFrame

    Returns:
        删除空几何后的 GeoDataFrame
    """
    result = input_gdf.copy()
    result = result[~result.geometry.isna()].reset_index(drop=True)
    return result


DROP_NULL_GEOMETRY_METADATA = OperatorMetadata(
    name="drop_null_geometry",
    type=OperatorType.NON_SPATIAL,
    category=OperatorCategory.FILTER_OPERATION,
    description="删除空几何",
    brief_description="删除几何为空的记录,常用于数据清洗和质量控制",

    overview="删除 GeoDataFrame 中几何列为 None 或 NaN 的记录。"
              "常用于数据清洗、避免空间算子报错、确保数据完整性等场景。",

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
        "数据清洗: 删除导入时几何解析失败的记录",
        "空间分析准备: 在缓冲区或相交分析前删除空几何避免报错",
        "数据质量检查: 统计并删除几何缺失的记录",
        "导出准备: 确保导出的 Shapefile 或 GeoJSON 不包含空几何"
    ],

    notes=[
        "删除后索引会被重置为连续整数",
        "建议在数据加载后立即执行,避免后续算子报错",
        "空几何与空属性不同,只检查 geometry 列",
        "大数据集过滤速度快（向量化操作）"
    ],

    workflow_example={
        'id': 'clean_geometry',
        'operator': 'drop_null_geometry',
        'params': {
            'input_gdf': {'$ref': 'load_data'}
        },
        'depends_on': ['load_data']
    }
)


# ========== 8. random_split (多输出算子) ==========
def random_split(input_gdf: gpd.GeoDataFrame, train_ratio: float = 0.8, random_state: int = None) -> Dict[str, gpd.GeoDataFrame]:
    """
    随机分割数据集（多输出算子）

    Args:
        input_gdf: 输入 GeoDataFrame
        train_ratio: 训练集比例（0-1）
        random_state: 随机种子

    Returns:
        Dict[str, GeoDataFrame]: 包含两个输出端口
            - "train": 训练集
            - "test": 测试集
    """
    shuffled = input_gdf.sample(frac=1, random_state=random_state).reset_index(drop=True)
    split_idx = int(len(shuffled) * train_ratio)

    train_gdf = shuffled.iloc[:split_idx].copy()
    test_gdf = shuffled.iloc[split_idx:].copy()

    return {
        "train": train_gdf,
        "test": test_gdf
    }


RANDOM_SPLIT_METADATA = OperatorMetadata(
    name="random_split",
    type=OperatorType.NON_SPATIAL,
    category=OperatorCategory.FILTER_OPERATION,
    description="随机分割数据集",
    brief_description="将数据集随机分割为训练集和测试集,常用于机器学习数据准备",

    overview="将输入 GeoDataFrame 随机分割为训练集和测试集,支持设置分割比例和随机种子。"
              "适用于机器学习模型训练和验证的数据准备。返回两个独立的输出端口。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="train_ratio",
            type="param",
            data_type="float",
            required=False,
            description="训练集比例（0-1）,默认 0.8"
        ),
        OperatorParam(
            name="random_state",
            type="param",
            data_type="int",
            required=False,
            description="随机种子,用于结果可复现"
        )
    ],

    output_ports=[
        OutputPort(
            name="train",
            type="geodataframe",
            description="训练集（train_ratio 比例）",
            is_default=False
        ),
        OutputPort(
            name="test",
            type="geodataframe",
            description="测试集（1 - train_ratio 比例）",
            is_default=False
        )
    ],

    use_cases=[
        "机器学习准备: 将 1000 个建筑物分为 800 训练 + 200 测试",
        "模型验证: 8:2 分割土地利用数据用于分类模型训练",
        "可复现实验: 设置 random_state=42 确保每次分割结果一致",
        "交叉验证准备: 先分割数据再分别训练和评估模型"
    ],

    notes=[
        "train_ratio 范围为 0-1,建议 0.6-0.9 之间",
        "设置 random_state 可确保结果可复现",
        "分割前会打乱数据顺序,避免顺序偏差",
        "两个输出端口需分别引用: {'$ref': 'split_task', 'port': 'train'}"
    ],

    workflow_example={
        'id': 'split_data',
        'operator': 'random_split',
        'params': {
            'input_gdf': {'$ref': 'load_data'},
            'train_ratio': 0.8,
            'random_state': 42
        },
        'depends_on': ['load_data']
    }
)


# ========== 注册所有算子 ==========
OPERATORS = dict([
    register_operator(FILTER_BY_ATTRIBUTE_METADATA, filter_by_attribute),
    register_operator(SORT_BY_FIELD_METADATA, sort_by_field),
    register_operator(SELECT_TOP_N_METADATA, select_top_n),
    register_operator(DROP_DUPLICATES_METADATA, drop_duplicates),
    register_operator(SAMPLE_METADATA, sample),
    register_operator(FILTER_BY_GEOMETRY_TYPE_METADATA, filter_by_geometry_type),
    register_operator(DROP_NULL_GEOMETRY_METADATA, drop_null_geometry),
    register_operator(RANDOM_SPLIT_METADATA, random_split),
])
