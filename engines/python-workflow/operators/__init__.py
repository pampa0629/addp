"""
GeoPandas 算子模块

提供 42 个算子（24 个空间算子 + 18 个非空间算子）,支持：
- 数据 I/O、几何处理、空间关系分析
- 属性计算、数据筛选、统计分析
模块化重构后的算子按功能分类管理。
"""

from .base import OperatorMetadata, OperatorParam, OperatorCategory, OperatorType, OutputPort

# 导入所有空间算子实现
from .io_operators import load, save
from .geometric_operators import (
    buffer, intersection, union, centroid, difference,
    simplify, convex_hull, envelope, dissolve
)
from .spatial_relations import contains, intersects, distance_to
from .properties_operators import get_area, get_length, get_bounds
from .format_operators import load_from_wkt, export_to_wkt
from .data_operations import (
    clip, voronoi, split_by_area,
    batch_buffer, batch_centroid
)

# 导入所有非空间算子实现
from .attribute_operators import (
    add_field, calculate_field, rename_fields, drop_fields,
    type_cast, fill_null, normalize_field, encode_categorical, bin_field
)
from .filter_operators import (
    filter_by_attribute, sort_by_field, select_top_n, drop_duplicates,
    sample, filter_by_geometry_type, drop_null_geometry, random_split
)

# 导入所有算子元数据
from .io_operators import OPERATORS as IO_OPERATORS
from .geometric_operators import OPERATORS as GEOMETRIC_OPERATORS
from .spatial_relations import OPERATORS as SPATIAL_OPERATORS
from .properties_operators import OPERATORS as PROPERTIES_OPERATORS
from .format_operators import OPERATORS as FORMAT_OPERATORS
from .data_operations import OPERATORS as DATA_OPERATORS
from .attribute_operators import OPERATORS as ATTRIBUTE_OPERATORS
from .filter_operators import OPERATORS as FILTER_OPERATORS

# 合并所有算子元数据
OPERATORS = {}
for ops_dict in [
    IO_OPERATORS,
    GEOMETRIC_OPERATORS,
    SPATIAL_OPERATORS,
    PROPERTIES_OPERATORS,
    FORMAT_OPERATORS,
    DATA_OPERATORS,
    ATTRIBUTE_OPERATORS,
    FILTER_OPERATORS
]:
    OPERATORS.update(ops_dict)


def get_operator(operator_name: str):
    """获取算子函数（向后兼容）"""
    if operator_name not in OPERATORS:
        raise ValueError(f"Unknown operator: {operator_name}")
    return OPERATORS[operator_name]['function']


def list_operators():
    """
    列出所有算子 - 返回符合统一标准的算子元数据列表

    转换格式与旧版 API 兼容
    """
    operators_list = []

    # Python类型到标准类型的映射
    type_mapping = {
        'float': 'float',
        'int': 'integer',
        'bool': 'boolean',
        'str': 'string',
        'geodataframe': 'object',
        'GeoDataFrame': 'object',
        'list[float]': 'array',
        'list[str]': 'array',
    }

    for name, meta in OPERATORS.items():
        # 使用新的 param_schema
        parameters = []
        if 'param_schema' in meta:
            for param in meta['param_schema']:
                # param 已经是字典（来自 to_legacy_dict()），直接使用
                param_meta = {
                    "name": param['name'],
                    "type": type_mapping.get(param['type'], 'string'),
                    "required": param.get('required', True),
                    "description": param.get('description', '')
                }

                # 数组类型需要指定item_type
                if param['type'] in ['list[float]', 'list[str]']:
                    param_meta["item_type"] = "float" if param['type'] == 'list[float]' else "string"

                # 添加 UI 相关字段（如果存在）
                if param.get('ui_type'):
                    param_meta["ui_type"] = param['ui_type']
                if param.get('ui_config'):
                    param_meta["ui_config"] = param['ui_config']
                if param.get('enum'):
                    param_meta["enum"] = param['enum']
                if param.get('default') is not None:
                    param_meta["default"] = param['default']
                if param.get('notes'):
                    param_meta["notes"] = param['notes']

                # 添加依赖和条件显示字段（如果存在）
                if param.get('depends_on'):
                    param_meta["depends_on"] = param['depends_on']
                if param.get('show_when'):
                    param_meta["show_when"] = param['show_when']

                parameters.append(param_meta)

        # 处理输出端口
        if 'output_ports' in meta:
            # 多输出算子：使用自定义端口定义
            output_ports = meta['output_ports']
        else:
            # 单输出算子：自动生成 default 端口
            output_ports = [{
                "name": "default",
                "type": "geodataframe",
                "description": f"{meta['description']}结果",
                "is_default": True
            }]

        # 构建标准化的算子元数据
        operator = {
            "id": name,
            "name": name,
            "display_name": meta['description'],
            "type": meta.get('type', 'general'),  # 从元数据读取类型，默认为 general
            "category": meta['category'],
            "description": meta['description'],
            "module": "python",
            "parameters": parameters,
            "inputs": ["geodataframe"],
            "output_ports": output_ports
        }

        # 添加简要描述 (如果存在)
        if 'brief_description' in meta:
            operator['brief_description'] = meta['brief_description']

        # 添加详细描述 (如果存在)
        if 'detailed_description' in meta:
            operator['detailed_description'] = meta['detailed_description']

        operators_list.append(operator)

    return operators_list


__all__ = [
    # 核心接口
    'OPERATORS',
    'get_operator',
    'list_operators',

    # 基础类型
    'OperatorMetadata',
    'OperatorParam',
    'OperatorCategory',
    'OperatorType',
    'OutputPort',

    # I/O 算子
    'load',
    'save',

    # 几何处理算子
    'buffer',
    'intersection',
    'union',
    'centroid',
    'difference',
    'simplify',
    'convex_hull',
    'envelope',

    # 空间关系算子
    'contains',
    'intersects',
    'distance_to',

    # 几何属性算子
    'get_area',
    'get_length',
    'get_bounds',

    # 格式转换算子
    'load_from_wkt',
    'export_to_wkt',

    # 数据操作算子
    'clip',
    'voronoi',
    'split_by_area',
    'dissolve',
    'batch_buffer',
    'batch_centroid',

    # 属性计算算子
    'add_field',
    'calculate_field',
    'rename_fields',
    'drop_fields',
    'type_cast',
    'fill_null',
    'normalize_field',
    'encode_categorical',
    'bin_field',

    # 数据筛选算子
    'filter_by_attribute',
    'sort_by_field',
    'select_top_n',
    'drop_duplicates',
    'sample',
    'filter_by_geometry_type',
    'drop_null_geometry',
    'random_split',
]
