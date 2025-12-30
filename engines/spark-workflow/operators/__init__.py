"""
Spark 工作流算子模块

将所有算子实现和元数据整合到一个统一的命名空间
"""

# 导入所有算子函数
from .io_operators import load, save, preview, cache, persist
from .spatial_operators import (
    st_buffer, st_intersection, st_union, st_centroid,
    spatial_join, st_distance, st_transform
)
from .transform_operators import (
    select, filter, add_column, rename_column, drop_column
)
from .aggregation_operators import group_by, join
from .sql_operators import sql, create_temp_view

# 导入各模块的 OPERATORS 字典
from .io_operators import OPERATORS as IO_OPERATORS
from .spatial_operators import OPERATORS as SPATIAL_OPERATORS
from .transform_operators import OPERATORS as TRANSFORM_OPERATORS
from .aggregation_operators import OPERATORS as AGGREGATION_OPERATORS
from .sql_operators import OPERATORS as SQL_OPERATORS

# 算子注册表(用于动态调用)
_OPERATOR_REGISTRY = {
    # I/O 算子
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

    # SQL 算子
    "sql": sql,
    "create_temp_view": create_temp_view,
}

# 元数据注册表(合并所有模块的元数据)
OPERATORS = {}
OPERATORS.update(IO_OPERATORS)
OPERATORS.update(SPATIAL_OPERATORS)
OPERATORS.update(TRANSFORM_OPERATORS)
OPERATORS.update(AGGREGATION_OPERATORS)
OPERATORS.update(SQL_OPERATORS)


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
        available = list(_OPERATOR_REGISTRY.keys())
        raise ValueError(f"Operator '{operator_name}' not found. Available operators: {available}")

    return _OPERATOR_REGISTRY[operator_name]


def list_operators():
    """
    列出所有算子名称

    Returns:
        算子名称列表
    """
    return list(_OPERATOR_REGISTRY.keys())


__all__ = [
    # 算子函数
    "load", "save", "preview", "cache", "persist",
    "st_buffer", "st_intersection", "st_union", "st_centroid",
    "spatial_join", "st_distance", "st_transform",
    "select", "filter", "add_column", "rename_column", "drop_column",
    "group_by", "join",
    "sql", "create_temp_view",

    # 注册表和辅助函数
    "_OPERATOR_REGISTRY",
    "OPERATORS",
    "get_operator",
    "list_operators",
]


