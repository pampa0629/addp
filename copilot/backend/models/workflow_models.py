"""
工作流相关的 Pydantic 模型

定义了工作流生成过程中使用的所有数据结构
"""
from typing import List, Dict, Any, Optional
from pydantic import BaseModel, Field


class DataSourceLocation(BaseModel):
    """
    数据源位置信息（根据引擎类型不同而不同）

    - 关系数据库：使用 schema + table
    - 对象存储：使用 bucket + path
    - 其他类型：使用 resource_identifier
    """
    # 关系数据库字段
    schema: Optional[str] = Field(None, description="数据库 Schema 名称（PostgreSQL、MySQL 等）")
    table: Optional[str] = Field(None, description="表名")

    # 对象存储字段
    bucket: Optional[str] = Field(None, description="Bucket 名称（MinIO、S3、OSS）")
    path: Optional[str] = Field(None, description="对象路径/前缀")

    # 通用字段
    resource_identifier: Optional[str] = Field(None, description="通用资源标识符")

    class Config:
        json_schema_extra = {
            "examples": [
                {"schema": "public", "table": "users"},
                {"bucket": "gis-data", "path": "rivers.shp"}
            ]
        }


class DataSourceContext(BaseModel):
    """
    数据源上下文

    包含完整的数据源信息，包括引擎 ID、类型、位置信息和置信度
    """
    engine_id: int = Field(description="引擎 ID")
    engine_name: str = Field(description="引擎名称")
    engine_type: str = Field(description="引擎类型（postgresql、minio、s3、python_workflow 等）")
    location: DataSourceLocation = Field(description="数据源位置信息")
    confidence: float = Field(description="置信度（0-1），< 0.8 表示需要用户确认")
    alternatives: List[Dict[str, Any]] = Field(default=[], description="如果模糊匹配，返回候选项列表")

    class Config:
        json_schema_extra = {
            "examples": [
                {
                    "engine_id": 1,
                    "engine_name": "PostgreSQL 主库",
                    "engine_type": "postgresql",
                    "location": {"schema": "public", "table": "test"},
                    "confidence": 0.95,
                    "alternatives": []
                }
            ]
        }


class Task(BaseModel):
    """
    工作流任务

    表示工作流 DAG 中的一个节点
    """
    id: str = Field(description="任务 ID，格式: task1, task2, ...")
    operator: str = Field(description="算子名称")
    params: Dict[str, Any] = Field(description="算子参数")
    depends_on: List[str] = Field(default=[], description="依赖的前驱任务 ID 列表")

    class Config:
        json_schema_extra = {
            "examples": [
                {
                    "id": "task1",
                    "operator": "load",
                    "params": {
                        "source_type": "table",
                        "engine_id": 1,
                        "schema": "public",
                        "table": "test"
                    },
                    "depends_on": []
                },
                {
                    "id": "task2",
                    "operator": "buffer",
                    "params": {
                        "input_gdf": "task1",
                        "distance": 100
                    },
                    "depends_on": ["task1"]
                }
            ]
        }


class Workflow(BaseModel):
    """
    工作流定义

    包含完整的任务列表和可选的元数据
    """
    tasks: List[Task] = Field(description="任务列表")
    metadata: Optional[Dict[str, Any]] = Field(default=None, description="元数据（可选）")

    class Config:
        json_schema_extra = {
            "examples": [
                {
                    "tasks": [
                        {
                            "id": "task1",
                            "operator": "load",
                            "params": {"source_type": "table", "engine_id": 1, "schema": "public", "table": "test"},
                            "depends_on": []
                        },
                        {
                            "id": "task2",
                            "operator": "buffer",
                            "params": {"input_gdf": "task1", "distance": 100},
                            "depends_on": ["task1"]
                        },
                        {
                            "id": "task3",
                            "operator": "save",
                            "params": {
                                "data": {"$ref": "task2"},
                                "target_type": "table",
                                "engine_id": 1,
                                "schema": "public",
                                "table": "test3"
                            },
                            "depends_on": ["task2"]
                        }
                    ]
                }
            ]
        }


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

    class Config:
        json_schema_extra = {
            "examples": [
                {
                    "is_valid": False,
                    "errors": [
                        "任务 task2 依赖不存在的任务 task99",
                        "任务 task3 缺少必需参数 target_type"
                    ],
                    "warnings": [],
                    "suggestions": [
                        "检查 task2 的 depends_on 字段，确保引用的任务存在",
                        "为 task3 添加 target_type 参数（如 'table' 或 'file'）"
                    ]
                }
            ]
        }
