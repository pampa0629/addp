"""
工作流相关的 Pydantic 模型

定义了工作流生成过程中使用的所有数据结构
"""
from typing import List, Dict, Any, Optional
from pydantic import BaseModel, ConfigDict, Field


class Task(BaseModel):
    """
    工作流任务

    表示工作流 DAG 中的一个节点
    """
    id: str = Field(description="任务 ID，格式: task1, task2, ...")
    operator: str = Field(description="算子名称")
    params: Dict[str, Any] = Field(description="算子参数")
    depends_on: List[str] = Field(description="依赖的前驱任务 ID 列表；无依赖时必须显式传空数组")

    model_config = ConfigDict(json_schema_extra={
        "examples": [
            {
                "id": "task1",
                "operator": "load",
                "params": {
                    "locator": "addp://engine/1/path/public/test?type=table&item_id=102"
                },
                "depends_on": []
            },
            {
                "id": "task2",
                "operator": "buffer",
                "params": {
                    "input_gdf": {"$ref": "task1"},
                    "distance": 100
                },
                "depends_on": ["task1"]
            }
        ]
    })


class Workflow(BaseModel):
    """
    工作流定义

    包含完整的任务列表和可选的元数据
    """
    tasks: List[Task] = Field(description="任务列表")
    metadata: Optional[Dict[str, Any]] = Field(default=None, description="元数据（可选）")

    model_config = ConfigDict(json_schema_extra={
        "examples": [
            {
                "tasks": [
                    {
                        "id": "task1",
                        "operator": "load",
                        "params": {"locator": "addp://engine/1/path/public/test?type=table&item_id=102"},
                        "depends_on": []
                    },
                    {
                        "id": "task2",
                        "operator": "buffer",
                        "params": {"input_gdf": {"$ref": "task1"}, "distance": 100},
                        "depends_on": ["task1"]
                    },
                    {
                        "id": "task3",
                        "operator": "save",
                        "params": {
                            "input_df": {"$ref": "task2"},
                            "target_parent_locator": "addp://engine/1/path/public?type=schema&node_id=11",
                            "target_name": "test3"
                        },
                        "depends_on": ["task2"]
                    }
                ]
            }
        ]
    })


class ValidationError(BaseModel):
    """单个验证错误"""
    level: str = Field(description="错误级别（error、warning）")
    message: str = Field(description="错误消息")
    task_id: Optional[str] = Field(None, description="相关任务 ID")
    suggestion: Optional[str] = Field(None, description="修复建议")


class ValidationResult(BaseModel):
    """
    工作流验证结果

    包含验证状态、错误列表和修复建议
    """
    is_valid: bool = Field(description="是否验证通过")
    errors: List[str] = Field(default=[], description="错误消息列表")
    warnings: List[str] = Field(default=[], description="警告消息列表（可选）")
    suggestions: List[str] = Field(default=[], description="修复建议列表")

    model_config = ConfigDict(json_schema_extra={
        "examples": [
            {
                "is_valid": False,
                "errors": [
                    "任务 task2 依赖不存在的任务 task99",
                    "任务 task3 缺少必需参数 target_name"
                ],
                "warnings": [],
                "suggestions": [
                    "检查 task2 的 depends_on 字段，确保引用的任务存在",
                    "为 task3 添加 target_name 参数"
                ]
            }
        ]
    })
