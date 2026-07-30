"""
Math Workflow Engine - 基础模型定义

定义算子元数据的 Pydantic 模型，确保符合 ADDP 工作流计算引擎接口规范。
"""

from pydantic import BaseModel, Field, model_validator
from typing import List, Optional, Dict, Any


class OutputPortMetadata(BaseModel):
    """输出端口元数据"""
    name: str = Field(..., description="端口名称")
    type: str = Field(..., description="数据类型")
    description: str = Field(..., description="端口语义说明")
    is_default: bool = Field(True, description="是否为默认端口")


class ParameterMetadata(BaseModel):
    """参数元数据"""
    name: str = Field(..., description="参数名")
    type: str = Field(..., description="参数类型：string/integer/float/boolean/array/object")
    required: bool = Field(..., description="是否必填")
    default: Optional[Any] = Field(None, description="默认值")
    description: str = Field(..., description="参数说明")
    min: Optional[float] = Field(None, description="最小值（数值类型）")
    max: Optional[float] = Field(None, description="最大值（数值类型）")
    enum: Optional[List[str]] = Field(None, description="枚举值")
    pattern: Optional[str] = Field(None, description="正则校验")
    item_type: Optional[str] = Field(None, description="数组元素类型")
    properties: Optional[Dict[str, 'ParameterMetadata']] = Field(None, description="对象属性定义")
    depends_on: Optional[str] = Field(None, description="依赖的参数名")


class OperatorMetadata(BaseModel):
    """算子元数据（符合 ADDP 规范）"""
    id: str = Field(..., description="算子唯一标识")
    name: str = Field(..., description="算子名称")
    display_name: str = Field(..., description="中文显示名")
    engine_type: str = Field("math_workflow", description="所属扩展引擎类型")
    category: str = Field(..., description="分类名称")
    category_path: List[str] = Field(default_factory=list, description="多级分组目录")
    description: str = Field(..., description="功能描述")
    brief_description: Optional[str] = Field(None, description="简短描述")
    execution_modes: List[str] = Field(..., description="执行模式")
    effects: List[str] = Field(..., description="执行效果：read/write/ddl/external_effect")
    parameters: List[ParameterMetadata] = Field(..., description="参数定义列表")
    output_ports: List[OutputPortMetadata] = Field(..., description="输出端口定义")
    use_cases: Optional[List[str]] = Field(None, description="使用场景")
    notes: Optional[List[str]] = Field(None, description="注意事项")

    @model_validator(mode="after")
    def fill_category_path(self):
        if not self.category_path:
            self.category_path = [self.category]
        return self


# 允许递归定义
ParameterMetadata.model_rebuild()
