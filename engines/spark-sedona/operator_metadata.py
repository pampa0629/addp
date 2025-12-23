"""
Spark Sedona 算子元数据定义
供前端工作流编辑器使用

NOTE: 元数据现在使用 Pydantic 模型定义在各个 operators 模块中
      本文件提供统一的 get_operator_metadata() 接口
"""

from typing import List, Dict, Any
from operators import OPERATORS


def get_operator_metadata() -> List[Dict[str, Any]]:
    """
    返回所有算子的元数据定义（标准格式）

    Returns:
        算子元数据列表，每个算子包含完整的元数据信息
    """
    metadata_list = []

    for op_name, op_meta in OPERATORS.items():
        # 从 detailed_description.parameters 提取参数信息
        detailed_desc = op_meta.get('detailed_description', {})
        parameters = []

        for param in detailed_desc.get('parameters', []):
            param_def = {
                "name": param['name'],
                "type": param['type'],
                "required": param.get('required', True),
                "description": param.get('description', ''),
            }
            if param.get('notes'):
                param_def['notes'] = param['notes']
            parameters.append(param_def)

        # 构建标准格式的元数据
        metadata = {
            "id": op_name,
            "name": op_name,
            "display_name": op_meta.get('description', op_name),
            "module": "spark",
            "category": op_meta.get('category', '未分类'),
            "description": op_meta.get('description', ''),
            "brief_description": op_meta.get('brief_description', ''),
            "detailed_description": detailed_desc,
            "parameters": parameters,
            "output_ports": [
                {
                    "name": "default",
                    "type": "dataframe",
                    "is_default": True,
                    "description": f"{op_meta.get('description', '')}结果"
                }
            ]
        }

        metadata_list.append(metadata)

    return metadata_list
