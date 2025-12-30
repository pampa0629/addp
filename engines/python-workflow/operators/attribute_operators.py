"""
属性计算算子模块

提供字段添加、计算、转换、重命名等非空间操作
"""

from typing import Any, Dict, List, Union
import geopandas as gpd
import pandas as pd
from simpleeval import simple_eval
from .base import (
    OperatorMetadata,
    OperatorParam,
    OperatorCategory,
    register_operator
)


# ========== 1. add_field ==========
def add_field(input_gdf: gpd.GeoDataFrame, field_name: str, value: Any = None) -> gpd.GeoDataFrame:
    """
    添加新字段

    Args:
        input_gdf: 输入 GeoDataFrame
        field_name: 新字段名
        value: 字段值（常量或 None）

    Returns:
        添加字段后的 GeoDataFrame
    """
    result = input_gdf.copy()
    result[field_name] = value
    return result


ADD_FIELD_METADATA = OperatorMetadata(
    name="add_field",
    category=OperatorCategory.ATTRIBUTE_CALCULATION,
    description="添加新字段",
    brief_description="向 GeoDataFrame 添加新字段并赋值,常用于数据准备和分类标注",

    overview="向输入 GeoDataFrame 添加新字段,支持常量值或 None（用于后续计算）。"
              "新字段可用于后续分类、统计和可视化。不影响几何列和空间索引。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="field_name",
            type="param",
            data_type="str",
            required=True,
            description="新字段名称"
        ),
        OperatorParam(
            name="value",
            type="param",
            data_type="object",
            required=False,
            description="字段值（常量或 None）",
            notes="可以是字符串、数字、布尔值或 None"
        )
    ],

    use_cases=[
        "添加分类字段: 为建筑物添加 'type' 字段值为 'residential'",
        "添加时间戳: 为采集点添加 'survey_date' 字段值为 '2025-12-23'",
        "添加标记字段: 为筛选后的数据添加 'selected' 字段值为 True",
        "数据准备: 为机器学习添加 'label' 字段值为 1,用于后续分类"
    ],

    notes=[
        "如果字段已存在,会覆盖原有值",
        "value 参数支持 None,用于后续通过 calculate_field 计算",
        "字段名建议遵循命名规范,避免使用特殊字符和保留字",
        "新字段不会影响几何列和空间索引,性能开销极小"
    ],

    workflow_example={
        'id': 'add_category',
        'operator': 'add_field',
        'params': {
            'input_gdf': {'$ref': 'load_buildings'},
            'field_name': 'type',
            'value': 'residential'
        },
        'depends_on': ['load_buildings']
    }
)


# ========== 2. calculate_field ==========
def calculate_field(input_gdf: gpd.GeoDataFrame, field_name: str, expression: str) -> gpd.GeoDataFrame:
    """
    字段计算（表达式）

    Args:
        input_gdf: 输入 GeoDataFrame
        field_name: 新字段名或已有字段名
        expression: 计算表达式（使用字段名，如 "population / area"）

    Returns:
        计算后的 GeoDataFrame
    """
    result = input_gdf.copy()

    # 使用 simpleeval 安全计算表达式
    result[field_name] = result.apply(
        lambda row: simple_eval(expression, names=row.to_dict()),
        axis=1
    )

    return result


CALCULATE_FIELD_METADATA = OperatorMetadata(
    name="calculate_field",
    category=OperatorCategory.ATTRIBUTE_CALCULATION,
    description="字段计算（表达式）",
    brief_description="基于表达式计算新字段或更新已有字段,常用于派生指标和数据转换",

    overview="使用数学表达式基于现有字段计算新字段。支持基本算术运算（+、-、*、/、**）"
              "和常用函数。使用 simpleeval 库确保安全执行,防止代码注入攻击。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="field_name",
            type="param",
            data_type="str",
            required=True,
            description="新字段名或已有字段名"
        ),
        OperatorParam(
            name="expression",
            type="param",
            data_type="str",
            required=True,
            description="计算表达式（使用字段名）",
            notes="示例: 'population / area', 'price * 1.1', 'length + width'"
        )
    ],

    use_cases=[
        "计算人口密度: field_name='density', expression='population / area'",
        "价格上涨 10%: field_name='new_price', expression='price * 1.1'",
        "计算周长: field_name='perimeter', expression='(length + width) * 2'",
        "归一化指标: field_name='norm_value', expression='(value - min_val) / (max_val - min_val)'"
    ],

    notes=[
        "表达式中使用的字段名必须存在于 GeoDataFrame 中",
        "支持运算符: +, -, *, /, //, %, **, 以及括号",
        "不支持复杂函数调用,如需复杂计算建议使用 Python 自定义算子",
        "大数据集性能较慢（逐行计算）,小于 10 万行时性能可接受"
    ],

    workflow_example={
        'id': 'calc_density',
        'operator': 'calculate_field',
        'params': {
            'input_gdf': {'$ref': 'get_area'},
            'field_name': 'density',
            'expression': 'population / area'
        },
        'depends_on': ['get_area']
    }
)


# ========== 3. rename_fields ==========
def rename_fields(input_gdf: gpd.GeoDataFrame, field_mapping: Dict[str, str]) -> gpd.GeoDataFrame:
    """
    批量重命名字段

    Args:
        input_gdf: 输入 GeoDataFrame
        field_mapping: 字段映射字典 {旧名: 新名}

    Returns:
        重命名后的 GeoDataFrame
    """
    result = input_gdf.copy()
    result = result.rename(columns=field_mapping)
    return result


RENAME_FIELDS_METADATA = OperatorMetadata(
    name="rename_fields",
    category=OperatorCategory.ATTRIBUTE_CALCULATION,
    description="批量重命名字段",
    brief_description="批量修改字段名称,常用于数据标准化和命名规范统一",

    overview="批量修改 GeoDataFrame 中的字段名称。支持一次重命名多个字段,"
              "常用于数据标准化、中英文转换、命名规范统一等场景。不影响数据内容。",

    params=[
        OperatorParam(
            name="input_gdf",
            type="input",
            data_type="GeoDataFrame",
            required=True,
            description="输入的地理数据"
        ),
        OperatorParam(
            name="field_mapping",
            type="param",
            data_type="object",
            required=True,
            description="字段映射字典 {旧名: 新名}",
            notes="示例: {'pop': 'population', 'area_sqm': 'area'}"
        )
    ],

    use_cases=[
        "中英文转换: {'人口': 'population', '面积': 'area'}",
        "标准化命名: {'AREA_SQM': 'area', 'POP_TOTAL': 'population'}",
        "简化字段名: {'very_long_field_name': 'short_name'}",
        "数据集成: 将不同数据源的字段名统一为标准命名"
    ],

    notes=[
        "如果旧字段名不存在,会被忽略而不会报错",
        "新字段名不能与已有字段冲突（除非被重命名）",
        "geometry 字段建议不要重命名,保持默认名称",
        "重命名操作不会影响数据类型和索引"
    ],

    workflow_example={
        'id': 'rename_cols',
        'operator': 'rename_fields',
        'params': {
            'input_gdf': {'$ref': 'load_data'},
            'field_mapping': {'pop': 'population', 'area_sqm': 'area'}
        },
        'depends_on': ['load_data']
    }
)


# ========== 4. drop_fields ==========
def drop_fields(input_gdf: gpd.GeoDataFrame, field_names: List[str]) -> gpd.GeoDataFrame:
    """
    删除字段

    Args:
        input_gdf: 输入 GeoDataFrame
        field_names: 要删除的字段名列表

    Returns:
        删除字段后的 GeoDataFrame
    """
    result = input_gdf.copy()
    # 只删除存在的字段
    existing_fields = [f for f in field_names if f in result.columns]
    if existing_fields:
        result = result.drop(columns=existing_fields)
    return result


DROP_FIELDS_METADATA = OperatorMetadata(
    name="drop_fields",
    category=OperatorCategory.ATTRIBUTE_CALCULATION,
    description="删除字段",
    brief_description="批量删除不需要的字段,常用于数据清洗和减少存储空间",

    overview="批量删除 GeoDataFrame 中的指定字段。常用于清理临时字段、"
              "减少数据体积、保护敏感信息等场景。不影响几何列。",

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
            required=True,
            description="要删除的字段名列表",
            notes="示例: ['temp_field', 'unused_column']"
        )
    ],

    use_cases=[
        "删除临时字段: ['temp_area', 'temp_distance'] 用于中间计算的字段",
        "减少数据体积: 删除不需要的冗余字段,减少存储和传输成本",
        "保护敏感信息: 删除 ['phone', 'address'] 等敏感字段后再分享数据",
        "数据导出准备: 只保留必要字段,删除其余所有字段"
    ],

    notes=[
        "如果字段不存在会被忽略,不会报错",
        "不能删除 geometry 字段,否则会失去地理数据特性",
        "建议先用 get_bounds 或 get_area 保存几何属性再删除 geometry",
        "删除字段不可恢复,建议先备份或确认无需使用"
    ],

    workflow_example={
        'id': 'drop_temps',
        'operator': 'drop_fields',
        'params': {
            'input_gdf': {'$ref': 'calculate_all'},
            'field_names': ['temp_area', 'temp_distance']
        },
        'depends_on': ['calculate_all']
    }
)


# ========== 5. type_cast ==========
def type_cast(input_gdf: gpd.GeoDataFrame, field: str, target_type: str) -> gpd.GeoDataFrame:
    """
    字段类型转换

    Args:
        input_gdf: 输入 GeoDataFrame
        field: 字段名
        target_type: 目标类型（'int', 'float', 'str', 'bool'）

    Returns:
        类型转换后的 GeoDataFrame
    """
    result = input_gdf.copy()

    type_map = {
        'int': int,
        'float': float,
        'str': str,
        'bool': bool
    }

    if target_type not in type_map:
        raise ValueError(f"Unsupported target type: {target_type}. Supported: {list(type_map.keys())}")

    result[field] = result[field].astype(type_map[target_type])
    return result


TYPE_CAST_METADATA = OperatorMetadata(
    name="type_cast",
    category=OperatorCategory.ATTRIBUTE_CALCULATION,
    description="字段类型转换",
    brief_description="转换字段数据类型,常用于数据清洗和类型匹配",

    overview="将指定字段转换为目标数据类型（int、float、str、bool）。"
              "常用于修复数据类型错误、格式统一、满足算子输入要求等场景。",

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
            description="要转换的字段名"
        ),
        OperatorParam(
            name="target_type",
            type="param",
            data_type="str",
            required=True,
            description="目标类型",
            notes="支持: 'int', 'float', 'str', 'bool'"
        )
    ],

    use_cases=[
        "修复字符串数字: field='population', target_type='int' 将 '1000' 转为 1000",
        "精度调整: field='price', target_type='float' 将整数转为浮点数",
        "数值转字符串: field='id', target_type='str' 将数字 ID 转为字符串",
        "布尔值转换: field='is_valid', target_type='bool' 将 0/1 转为 False/True"
    ],

    notes=[
        "转换失败会抛出异常,建议先检查数据是否可转换",
        "字符串转数字时空值或非法值会报错,建议先用 fill_null 处理",
        "int 类型不支持 NaN,如有空值建议转为 float 或先填充",
        "大数据集转换速度快,性能开销小（Pandas 向量化操作）"
    ],

    workflow_example={
        'id': 'cast_pop',
        'operator': 'type_cast',
        'params': {
            'input_gdf': {'$ref': 'load_data'},
            'field': 'population',
            'target_type': 'int'
        },
        'depends_on': ['load_data']
    }
)


# ========== 6. fill_null ==========
def fill_null(input_gdf: gpd.GeoDataFrame, field: str, fill_value: Any) -> gpd.GeoDataFrame:
    """
    填充空值

    Args:
        input_gdf: 输入 GeoDataFrame
        field: 字段名
        fill_value: 填充值

    Returns:
        填充后的 GeoDataFrame
    """
    result = input_gdf.copy()
    result[field] = result[field].fillna(fill_value)
    return result


FILL_NULL_METADATA = OperatorMetadata(
    name="fill_null",
    category=OperatorCategory.ATTRIBUTE_CALCULATION,
    description="填充空值",
    brief_description="填充字段中的 NULL/NaN 值,常用于数据清洗和缺失值处理",

    overview="将指定字段中的空值（NULL、NaN、None）替换为指定值。"
              "常用于数据清洗、避免计算错误、满足非空约束等场景。",

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
            description="要处理的字段名"
        ),
        OperatorParam(
            name="fill_value",
            type="param",
            data_type="object",
            required=True,
            description="填充值",
            notes="可以是数字、字符串、布尔值等"
        )
    ],

    use_cases=[
        "填充默认值: field='population', fill_value=0 将空人口填充为 0",
        "填充字符串: field='type', fill_value='unknown' 将空类型填充为未知",
        "填充布尔值: field='is_valid', fill_value=False 将空值填充为 False",
        "填充平均值: 先计算均值,再填充 field='price', fill_value=average_price"
    ],

    notes=[
        "只填充指定字段的空值,不影响其他字段",
        "填充值类型应与字段类型兼容,避免类型错误",
        "可以先用 fillna() 统计空值数量,再决定填充策略",
        "填充后的值与原始值无法区分,建议先标记或备份"
    ],

    workflow_example={
        'id': 'fill_population',
        'operator': 'fill_null',
        'params': {
            'input_gdf': {'$ref': 'load_data'},
            'field': 'population',
            'fill_value': 0
        },
        'depends_on': ['load_data']
    }
)


# ========== 7. normalize_field ==========
def normalize_field(input_gdf: gpd.GeoDataFrame, field: str, method: str = 'minmax') -> gpd.GeoDataFrame:
    """
    字段归一化

    Args:
        input_gdf: 输入 GeoDataFrame
        field: 字段名
        method: 归一化方法（'minmax' 或 'zscore'）

    Returns:
        归一化后的 GeoDataFrame（添加 {field}_norm 字段）
    """
    result = input_gdf.copy()

    if method == 'minmax':
        # Min-Max 归一化到 [0, 1]
        min_val = result[field].min()
        max_val = result[field].max()
        if max_val > min_val:
            result[f'{field}_norm'] = (result[field] - min_val) / (max_val - min_val)
        else:
            result[f'{field}_norm'] = 0
    elif method == 'zscore':
        # Z-Score 标准化（均值 0，标准差 1）
        mean_val = result[field].mean()
        std_val = result[field].std()
        if std_val > 0:
            result[f'{field}_norm'] = (result[field] - mean_val) / std_val
        else:
            result[f'{field}_norm'] = 0
    else:
        raise ValueError(f"Unsupported method: {method}. Supported: ['minmax', 'zscore']")

    return result


NORMALIZE_FIELD_METADATA = OperatorMetadata(
    name="normalize_field",
    category=OperatorCategory.ATTRIBUTE_CALCULATION,
    description="字段归一化",
    brief_description="将数值字段归一化到标准范围,常用于数据分析和机器学习",

    overview="对数值字段进行归一化或标准化处理。支持 Min-Max 归一化（缩放到 [0, 1]）"
              "和 Z-Score 标准化（均值 0、标准差 1）。常用于机器学习、数据可视化、多指标对比。",

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
            description="要归一化的字段名"
        ),
        OperatorParam(
            name="method",
            type="param",
            data_type="str",
            required=False,
            description="归一化方法",
            notes="'minmax'（默认）: 缩放到 [0, 1]; 'zscore': 均值 0 标准差 1"
        )
    ],

    use_cases=[
        "机器学习准备: field='population', method='minmax' 将人口归一化后训练模型",
        "多指标对比: 将人口、面积、GDP 归一化后可视化对比",
        "异常检测: field='price', method='zscore' 识别超过 3 倍标准差的异常值",
        "热力图绘制: 归一化后的值作为颜色映射的权重"
    ],

    notes=[
        "结果保存在 {field}_norm 新字段中,不覆盖原字段",
        "minmax 方法对异常值敏感,zscore 方法更鲁棒",
        "字段中不能有 NaN 值,建议先用 fill_null 处理",
        "如果所有值相同（方差为 0）,归一化结果为 0"
    ],

    workflow_example={
        'id': 'norm_pop',
        'operator': 'normalize_field',
        'params': {
            'input_gdf': {'$ref': 'load_data'},
            'field': 'population',
            'method': 'minmax'
        },
        'depends_on': ['load_data']
    }
)


# ========== 8. encode_categorical ==========
def encode_categorical(input_gdf: gpd.GeoDataFrame, field: str) -> gpd.GeoDataFrame:
    """
    分类编码

    Args:
        input_gdf: 输入 GeoDataFrame
        field: 要编码的字段名

    Returns:
        编码后的 GeoDataFrame（添加 {field}_code 字段）
    """
    result = input_gdf.copy()

    # 使用 Pandas Categorical 编码
    result[f'{field}_code'] = pd.Categorical(result[field]).codes

    return result


ENCODE_CATEGORICAL_METADATA = OperatorMetadata(
    name="encode_categorical",
    category=OperatorCategory.ATTRIBUTE_CALCULATION,
    description="分类编码",
    brief_description="将分类字段编码为数字,常用于机器学习和数据分析",

    overview="将字符串或分类字段转换为整数编码（从 0 开始的连续整数）。"
              "常用于机器学习模型输入、减少存储空间、提升查询性能等场景。",

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
            description="要编码的字段名"
        )
    ],

    use_cases=[
        "土地利用分类: field='land_use' 将 ['农田', '建设用地', '林地'] 编码为 [0, 1, 2]",
        "机器学习准备: 将类别特征转为数字后训练决策树模型",
        "数据压缩: 将长字符串类型编码为整数,减少存储空间",
        "可视化准备: 编码后的整数可直接用于颜色映射"
    ],

    notes=[
        "结果保存在 {field}_code 新字段中,不覆盖原字段",
        "编码顺序按首次出现顺序,不保证字母序",
        "NaN 值会被编码为 -1",
        "如需保留编码映射关系,建议导出 categories 属性"
    ],

    workflow_example={
        'id': 'encode_type',
        'operator': 'encode_categorical',
        'params': {
            'input_gdf': {'$ref': 'load_data'},
            'field': 'land_use'
        },
        'depends_on': ['load_data']
    }
)


# ========== 9. bin_field ==========
def bin_field(input_gdf: gpd.GeoDataFrame, field: str, bins: Union[int, List[float]], labels: List[str] = None) -> gpd.GeoDataFrame:
    """
    字段分箱

    Args:
        input_gdf: 输入 GeoDataFrame
        field: 要分箱的字段名
        bins: 分箱数量（int）或分箱边界（list）
        labels: 分箱标签（可选）

    Returns:
        分箱后的 GeoDataFrame（添加 {field}_bin 字段）
    """
    result = input_gdf.copy()

    result[f'{field}_bin'] = pd.cut(result[field], bins=bins, labels=labels)

    return result


BIN_FIELD_METADATA = OperatorMetadata(
    name="bin_field",
    category=OperatorCategory.ATTRIBUTE_CALCULATION,
    description="字段分箱",
    brief_description="将连续数值字段分箱为离散区间,常用于数据分级和可视化",

    overview="将连续数值字段划分为离散的区间（分箱）。支持等频分箱（指定箱数）"
              "和自定义分箱（指定边界）。常用于数据分级、专题地图制作、统计分析。",

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
            description="要分箱的字段名"
        ),
        OperatorParam(
            name="bins",
            type="param",
            data_type="object",
            required=True,
            description="分箱数量（int）或分箱边界（list）",
            notes="示例: 5 或 [0, 100, 500, 1000, 5000]"
        ),
        OperatorParam(
            name="labels",
            type="param",
            data_type="list[str]",
            required=False,
            description="分箱标签（可选）",
            notes="示例: ['极低', '低', '中', '高', '极高']"
        )
    ],

    use_cases=[
        "人口密度分级: bins=[0, 100, 500, 1000, 5000], labels=['低', '中低', '中', '中高', '高']",
        "面积分类: bins=5 自动将面积分为 5 个等宽区间",
        "价格分段: bins=[0, 1000, 5000, 10000, 50000] 划分价格区间",
        "专题地图制作: 分箱后按区间着色,制作分级统计图"
    ],

    notes=[
        "结果保存在 {field}_bin 新字段中,类型为 Categorical",
        "bins 为 int 时自动计算等宽区间边界",
        "bins 为 list 时需要 n+1 个边界值（n 个区间）",
        "超出边界的值会被标记为 NaN,建议先检查数据范围"
    ],

    workflow_example={
        'id': 'bin_population',
        'operator': 'bin_field',
        'params': {
            'input_gdf': {'$ref': 'load_data'},
            'field': 'population',
            'bins': [0, 100, 500, 1000, 5000],
            'labels': ['极低', '低', '中', '高']
        },
        'depends_on': ['load_data']
    }
)


# ========== 注册所有算子 ==========
OPERATORS = dict([
    register_operator(ADD_FIELD_METADATA, add_field),
    register_operator(CALCULATE_FIELD_METADATA, calculate_field),
    register_operator(RENAME_FIELDS_METADATA, rename_fields),
    register_operator(DROP_FIELDS_METADATA, drop_fields),
    register_operator(TYPE_CAST_METADATA, type_cast),
    register_operator(FILL_NULL_METADATA, fill_null),
    register_operator(NORMALIZE_FIELD_METADATA, normalize_field),
    register_operator(ENCODE_CATEGORICAL_METADATA, encode_categorical),
    register_operator(BIN_FIELD_METADATA, bin_field),
])
