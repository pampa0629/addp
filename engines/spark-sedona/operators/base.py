"""
Spark Sedona 算子基础模块

提供 Pydantic 模型、枚举类型和辅助函数，用于算子元数据管理和注册。
参考 GeoPandas 引擎的设计，针对 Spark Sedona 的特点进行调整。
"""

from pydantic import BaseModel, Field
from typing import Any, Dict, List, Optional, Callable
from enum import Enum


class OperatorCategory(str, Enum):
    """算子分类枚举"""
    IO = "I/O"
    SPATIAL_ANALYSIS = "空间分析"
    DATA_TRANSFORM = "数据转换"
    AGGREGATION = "聚合分析"
    SQL = "SQL查询"


class OperatorParam(BaseModel):
    """算子参数定义模型"""
    name: str = Field(description="参数名称")
    type: str = Field(description="参数类型：dataframe/int/str/float/list 等")
    required: bool = Field(default=True, description="是否必填")
    description: str = Field(description="参数说明")
    notes: Optional[str] = Field(None, description="注意事项")


class OperatorMetadata(BaseModel):
    """
    算子元数据模型

    使用 Pydantic 提供类型安全和自动验证
    """
    name: str = Field(description="算子唯一标识符")
    category: OperatorCategory = Field(description="算子分类")
    description: str = Field(description="算子简要描述")
    brief_description: str = Field(description="简短说明（一句话）")

    # 详细元数据
    overview: str = Field(description="功能概述（50-100字）")
    params: List[OperatorParam] = Field(description="参数列表")
    use_cases: List[str] = Field(description="真实使用场景（4-5个）")
    notes: List[str] = Field(description="常见问题和优化建议（4-5个）")

    # 输入输出说明（Spark Sedona 特有）
    input_desc: str = Field(description="输入说明")
    output_desc: str = Field(description="输出说明")

    # 工作流示例
    workflow_example: Dict[str, Any] = Field(description="DAG 示例")

    # 运行时绑定（不参与序列化）
    function: Optional[Callable] = Field(None, exclude=True, description="实际执行函数")

    def to_dict(self) -> dict:
        """
        转换为字典格式（简化版，不考虑向后兼容）

        返回完整的元数据字典，供 operator_metadata.py 使用
        """
        # 构建 params 字典（简单格式）
        params_dict = {}
        for param in self.params:
            params_dict[param.name] = param.type

        # 构建 parameters 列表（详细格式）
        parameters_list = []
        for param in self.params:
            param_dict = {
                "name": param.name,
                "type": param.type,
                "required": param.required,
                "description": param.description
            }
            if param.notes:
                param_dict["notes"] = param.notes
            parameters_list.append(param_dict)

        # 返回完整字典
        return {
            'function': self.function,
            'params': params_dict,
            'category': self.category.value,
            'description': self.description,
            'brief_description': self.brief_description,
            'detailed_description': {
                'overview': self.overview,
                'parameters': parameters_list,
                'use_cases': self.use_cases,
                'notes': self.notes,
                'input': self.input_desc,
                'output': self.output_desc,
                'workflow_example': self.workflow_example
            }
        }


def register_operator(metadata: OperatorMetadata, func: Callable) -> tuple[str, dict]:
    """
    注册算子的辅助函数

    将 Pydantic 元数据模型和实现函数绑定，并转换为字典格式

    Args:
        metadata: 算子元数据（Pydantic 模型）
        func: 算子实现函数

    Returns:
        (算子名称, 元数据字典)

    Example:
        >>> BUFFER_METADATA = OperatorMetadata(name="st_buffer", ...)
        >>> OPERATORS = dict([register_operator(BUFFER_METADATA, st_buffer)])
    """
    metadata.function = func
    return metadata.name, metadata.to_dict()
