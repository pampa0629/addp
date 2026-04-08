"""
KG 图谱构建相关 Pydantic 数据模型
"""
from typing import List, Dict, Any, Optional
from pydantic import BaseModel, Field


class PropertyInfo(BaseModel):
    """属性定义（来自本体模型）"""
    name: str = Field(description="属性名（英文标识符）")
    label: str = Field(description="显示名称")
    data_type: str = Field(description="数据类型: string/integer/float/boolean/date/datetime/wkt")
    unique: bool = Field(default=False, description="是否为唯一标识属性（驱动 Neo4j MERGE 的唯一键）")
    required: bool = Field(default=False, description="是否必填")
    description: str = Field(default="", description="属性说明")


class EntityTypeInfo(BaseModel):
    """实体类型定义（来自本体模型）"""
    name: str = Field(description="实体类型名（对应 Neo4j Label）")
    label: str = Field(description="显示名称")
    description: str = Field(default="", description="实体类型说明")
    properties: List[PropertyInfo] = Field(default_factory=list, description="属性定义列表（含 unique 标记）")


class RelationTypeInfo(BaseModel):
    """关系类型定义（来自本体模型）"""
    name: str = Field(description="关系类型名（对应 Neo4j 关系类型）")
    label: str = Field(description="显示名称")
    source_type: str = Field(description="源实体类型名")
    target_type: str = Field(description="目标实体类型名")
    description: str = Field(default="", description="关系类型说明")
    properties: List[PropertyInfo] = Field(default_factory=list)


class OntologySchema(BaseModel):
    """本体 Schema（传递给 LLM 的抽取指导）"""
    entity_types: List[EntityTypeInfo] = Field(description="实体类型列表")
    relation_types: List[RelationTypeInfo] = Field(description="关系类型列表")


class KGExtractRequest(BaseModel):
    """KG 抽取请求（单个 chunk，由 Graph 后端分块后调用）"""
    text: str = Field(description="待抽取的文本（单个 chunk，约 1000 字符）")
    doc_context: str = Field(default="", description="文档头部上下文（前 N 字），帮助 LLM 理解主题和简称")
    ontology: OntologySchema = Field(description="本体 Schema（实体类型+关系类型+属性定义）")
    graph_id: int = Field(description="知识图谱 ID")
    confidence_threshold: float = Field(default=0.7, description="置信度阈值（参考值，具体分类由 Graph 后端处理）")


class ExtractedEntity(BaseModel):
    """抽取出的实体"""
    temp_id: str = Field(description="局部临时 ID（供同一 chunk 内的关系引用，格式: e1/e2/...）")
    type: str = Field(description="实体类型名（必须与 OntologySchema 中的 name 一致）")
    properties: Dict[str, Any] = Field(description="属性值（必须包含所有 unique=true 的属性）")
    confidence: float = Field(ge=0.0, le=1.0, description="置信度 0.0~1.0")
    source_text: str = Field(description="实体在原文中的来源文本片段（约 50 字以内）")


class ExtractedRelation(BaseModel):
    """抽取出的关系"""
    type: str = Field(description="关系类型名（必须与 OntologySchema 中的 name 一致）")
    source_temp_id: str = Field(description="源实体的 temp_id")
    target_temp_id: str = Field(description="目标实体的 temp_id")
    properties: Dict[str, Any] = Field(default_factory=dict, description="关系属性值")
    confidence: float = Field(ge=0.0, le=1.0, description="置信度 0.0~1.0")
    source_text: str = Field(description="关系在原文中的来源文本片段")


class KGExtractResponse(BaseModel):
    """KG 抽取响应"""
    entities: List[ExtractedEntity] = Field(default_factory=list)
    relations: List[ExtractedRelation] = Field(default_factory=list)
    processing_time: float = Field(description="处理耗时（秒）")


# ---- 用于 LLM 输出解析的中间模型 ----

class EntityExtractionOutput(BaseModel):
    """实体抽取 LLM 输出"""
    entities: List[ExtractedEntity] = Field(description="抽取到的实体列表")


class RelationExtractionOutput(BaseModel):
    """关系抽取 LLM 输出"""
    relations: List[ExtractedRelation] = Field(description="抽取到的关系列表")
