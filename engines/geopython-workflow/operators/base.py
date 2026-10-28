"""
算子基础模块

提供 Pydantic 模型、枚举类型和辅助函数，用于算子元数据管理和注册。
"""

from pydantic import BaseModel, Field
from typing import Any, Dict, List, Optional, Callable
from enum import Enum


class OperatorType(str, Enum):
    """算子类型枚举"""
    SPATIAL = "spatial"          # 空间算子（涉及几何处理、空间关系）
    NON_SPATIAL = "non_spatial"  # 非空间算子（纯属性操作、筛选）
    GENERAL = "general"          # 通用算子（I/O、格式转换）


class OperatorCategory(Enum):
    """Runtime 拥有的两级算子分类：处理对象、操作能力。"""
    COMMON_IO = ("通用处理", "数据读写")
    COMMON_FIELDS = ("通用处理", "字段处理")
    COMMON_CLEAN = ("通用处理", "数据清洗")
    COMMON_FILTER = ("通用处理", "数据筛选")
    COMMON_NUMERIC = ("通用处理", "数值统计")
    VECTOR_GEOMETRY = ("矢量处理", "几何处理")
    VECTOR_OVERLAY = ("矢量处理", "叠加分析")
    VECTOR_RELATION = ("矢量处理", "空间关系")
    VECTOR_MEASUREMENT = ("矢量处理", "几何量测")
    VECTOR_SELECTION = ("矢量处理", "要素筛选")
    VECTOR_PROJECTION = ("矢量处理", "坐标转换")
    VECTOR_FORMAT = ("矢量处理", "格式转换")
    VECTOR_OUTPUT = ("矢量处理", "成果生成")
    RASTER_IO = ("栅格处理", "数据读写")
    RASTER_GRID = ("栅格处理", "网格处理")
    RASTER_EXTENT = ("栅格处理", "范围处理")
    RASTER_BAND = ("栅格处理", "波段处理")
    RASTER_CALCULATION = ("栅格处理", "基础计算")
    RASTER_CLASSIFICATION = ("栅格处理", "分类处理")
    RASTER_TERRAIN = ("栅格处理", "地形分析")
    RASTER_FLOW = ("栅格处理", "水流分析")
    RASTER_CATCHMENT = ("栅格处理", "汇水分析")
    RASTER_STREAM = ("栅格处理", "河网分析")
    RASTER_FORMAT = ("栅格处理", "格式转换")
    RASTER_STATISTICS = ("栅格处理", "栅格统计")
    RASTER_INSPECTION = ("栅格处理", "栅格检查")
    RASTER_OUTPUT = ("栅格处理", "成果生成")


class OperatorParam(BaseModel):
    """算子参数定义模型"""
    name: str = Field(description="参数名称")
    type: str = Field(description="参数类型：input/output/param")
    data_type: str = Field(description="数据类型：GeoDataFrame/str/float/int 等")
    required: bool = Field(default=True, description="是否必填")
    description: str = Field(description="参数说明")
    notes: Optional[str] = Field(None, description="注意事项")

    enum: Optional[List[str]] = Field(None, description="枚举值列表")
    default: Optional[Any] = Field(None, description="默认值")

    # 参数依赖和条件显示
    depends_on: Optional[str] = Field(None, description="依赖的参数名称")
    show_when: Optional[Dict[str, Any]] = Field(None, description="显示条件，格式: {param_name: value_or_list}")


class OutputPort(BaseModel):
    """输出端口定义模型"""
    name: str = Field(description="端口名称")
    type: str = Field(description="输出类型：geodataframe/string/number")
    description: str = Field(description="端口描述")
    is_default: bool = Field(default=True, description="是否为默认端口")


class OperatorMetadata(BaseModel):
    """
    算子元数据模型

    使用 Pydantic 提供类型安全和自动验证
    """
    name: str = Field(description="算子唯一标识符")
    type: OperatorType = Field(description="算子类型")
    category: OperatorCategory = Field(description="算子分类")
    description: str = Field(description="算子简要描述")
    brief_description: str = Field(description="简短说明（一句话）")

    # 详细元数据
    overview: str = Field(description="功能概述（50-100字）")
    params: List[OperatorParam] = Field(description="参数列表")
    use_cases: List[str] = Field(description="真实使用场景（4-5个）")
    notes: List[str] = Field(description="常见问题和优化建议（4-5个）")
    workflow_example: Dict[str, Any] = Field(description="DAG 示例")

    # 可选字段
    output_ports: Optional[List[OutputPort]] = Field(None, description="多输出端口定义")
    execution_modes: List[str] = Field(description="执行模式：workflow/direct")
    effects: List[str] = Field(description="执行效果：read/write/ddl/external_effect")
    attributes: Optional[Dict[str, Any]] = Field(None, description="引擎自定义扩展属性")

    # 运行时绑定（不参与序列化）
    function: Optional[Callable] = Field(None, exclude=True, description="实际执行函数")

    def to_runtime_dict(self) -> dict:
        """
        转换为运行时算子注册字典

        返回的结构由本包的 list_operators() 统一转换为 addp.workflow/v1 算子元数据。
        """
        # 转换参数格式为内部 param_schema，便于统一生成运行时 API 元数据。
        param_schema = []
        for param in self.params:
            param_dict = {
                "name": param.name,
                "type": param.data_type,  # 数据类型（用于类型校验）
                "param_type": param.type,  # 参数类型：input/output/param
                "required": param.required,
                "description": param.description
            }
            if param.notes:
                param_dict["notes"] = param.notes

            if param.enum:
                param_dict["enum"] = param.enum
            if param.default is not None:
                param_dict["default"] = param.default

            # 添加依赖和条件显示字段（如果存在）
            if param.depends_on:
                param_dict["depends_on"] = param.depends_on
            if param.show_when:
                param_dict["show_when"] = param.show_when

            param_schema.append(param_dict)

        # 构建基础字典
        runtime_dict = {
            'function': self.function,
            'type': self.type.value,  # 添加算子类型
            'param_schema': param_schema,
            'category': self.category.value[-1],
            'category_path': list(self.category.value),
            'execution_modes': list(self.execution_modes),
            'effects': list(self.effects),
            'description': self.description,
            'brief_description': self.brief_description,
            'detailed_description': {
                'overview': self.overview,
                'parameters': param_schema,
                'use_cases': self.use_cases,
                'notes': self.notes,
                'workflow_example': self.workflow_example
            }
        }

        if self.attributes:
            runtime_dict['attributes'] = self.attributes

        # 添加可选的 output_ports
        if self.output_ports:
            runtime_dict['output_ports'] = [port.model_dump() for port in self.output_ports]

        return runtime_dict


def register_operator(metadata: OperatorMetadata, func: Callable) -> tuple[str, dict]:
    """
    注册算子的辅助函数

    将 Pydantic 元数据模型和实现函数绑定，并转换为运行时注册格式

    Args:
        metadata: 算子元数据（Pydantic 模型）
        func: 算子实现函数

    Returns:
        (算子名称, 运行时注册字典)

    Example:
        >>> BUFFER_METADATA = OperatorMetadata(name="buffer", ...)
        >>> OPERATORS = dict([register_operator(BUFFER_METADATA, buffer)])
    """
    metadata.function = func
    return metadata.name, metadata.to_runtime_dict()
