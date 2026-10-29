package models

import "github.com/addp/common/engine/plugin"

// CreateOntologyRequest 创建本体请求
type CreateOntologyRequest struct {
	Name        string                 `json:"name" binding:"required"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// UpdateOntologyRequest 更新本体请求
type UpdateOntologyRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Status      string                 `json:"status"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// ConstraintDefinition 约束定义（结构化）
type ConstraintDefinition struct {
	Type  string `json:"type"`  // "unique" | "not_null"（not_null 需 Neo4j 企业版）
	Field string `json:"field"` // 属性字段名
	Name  string `json:"name"`  // 约束名（空则自动生成）
}

// CreateEntityTypeRequest 创建实体类型请求
type CreateEntityTypeRequest struct {
	Name               string                 `json:"name" binding:"required"`
	Label              string                 `json:"label"`
	Description        string                 `json:"description"`
	NodeLabels         []string               `json:"node_labels"`
	Color              string                 `json:"color"`
	Icon               string                 `json:"icon"`
	ParentID           *uint                  `json:"parent_id"`
	DisplayProperty    string                 `json:"display_property"`
	Properties         []PropertyDefinition   `json:"properties"`
	Constraints        []ConstraintDefinition `json:"constraints"`
	IsSpatialLayer     bool                   `json:"is_spatial_layer"`
	SpatialLayerConfig *SpatialLayerConfig    `json:"spatial_layer_config,omitempty"`
	SortOrder          int                    `json:"sort_order"`
}

// UpdateEntityTypeRequest 更新实体类型请求
type UpdateEntityTypeRequest struct {
	Name               string                 `json:"name"`
	Label              string                 `json:"label"`
	Description        string                 `json:"description"`
	NodeLabels         []string               `json:"node_labels"`
	Color              string                 `json:"color"`
	Icon               string                 `json:"icon"`
	ParentID           *uint                  `json:"parent_id"`
	DisplayProperty    string                 `json:"display_property"`
	Properties         []PropertyDefinition   `json:"properties"`
	Constraints        []ConstraintDefinition `json:"constraints"`
	IsSpatialLayer     bool                   `json:"is_spatial_layer"`
	SpatialLayerConfig *SpatialLayerConfig    `json:"spatial_layer_config,omitempty"`
	SortOrder          int                    `json:"sort_order"`
}

// CreateRelationTypeRequest 创建关系类型请求
type CreateRelationTypeRequest struct {
	Name         string                 `json:"name" binding:"required"`
	Label        string                 `json:"label"`
	Description  string                 `json:"description"`
	SourceTypeID *uint                  `json:"source_type_id"`
	TargetTypeID *uint                  `json:"target_type_id"`
	Directed     *bool                  `json:"directed"`
	Color        string                 `json:"color"`
	Properties   []PropertyDefinition   `json:"properties"`
	Constraints  []ConstraintDefinition `json:"constraints"`
	SortOrder    int                    `json:"sort_order"`
}

// UpdateRelationTypeRequest 更新关系类型请求
type UpdateRelationTypeRequest struct {
	Name         string                 `json:"name"`
	Label        string                 `json:"label"`
	Description  string                 `json:"description"`
	SourceTypeID *uint                  `json:"source_type_id"`
	TargetTypeID *uint                  `json:"target_type_id"`
	Directed     *bool                  `json:"directed"`
	Color        string                 `json:"color"`
	Properties   []PropertyDefinition   `json:"properties"`
	Constraints  []ConstraintDefinition `json:"constraints"`
	SortOrder    int                    `json:"sort_order"`
}

// PropertyDefinition 属性定义
type PropertyDefinition struct {
	Name        string      `json:"name"`
	Label       string      `json:"label"`
	DataType    string      `json:"data_type"` // string, integer, float, boolean, date, datetime, wkt
	Required    bool        `json:"required"`
	Unique      bool        `json:"unique"`
	Searchable  bool        `json:"searchable"`
	DefaultVal  interface{} `json:"default_val,omitempty"`
	Description string      `json:"description"`
}

// CreateKnowledgeGraphRequest 创建知识图谱实例请求
type CreateKnowledgeGraphRequest struct {
	OntologyID      uint                     `json:"ontology_id" binding:"required"`
	EngineID        uint                     `json:"engine_id" binding:"required"`
	CatalogPath     plugin.EngineCatalogPath `json:"catalog_path" binding:"required"`
	IndexManagement string                   `json:"index_management" binding:"required,oneof=managed external"`
	Name            string                   `json:"name" binding:"required"`
	Description     string                   `json:"description"`
}

// UpdateKnowledgeGraphRequest 更新知识图谱实例请求
type UpdateKnowledgeGraphRequest struct {
	Version     int64   `json:"version" binding:"required,gt=0"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	IsPublic    *bool   `json:"is_public,omitempty"` // 省略时保留现值，false 显式关闭公开 | Omit to retain; false disables public access
}

type CancelBuildRequest struct {
	ExecutionID string `json:"execution_id" binding:"required,uuid"`
	Attempt     *int   `json:"attempt" binding:"required,gte=0"`
}

type GraphControlRequest struct {
	Version int64 `json:"version" binding:"required,gt=0"`
}

// CreateOntologyVersionRequest 创建版本快照请求
type CreateOntologyVersionRequest struct {
	Version     string `json:"version" binding:"required"`
	Description string `json:"description"`
}

// OntologyDetail 本体详情（含实体类型和关系类型）
type OntologyDetail struct {
	Ontology
	EntityTypes   []EntityType   `json:"entity_types"`
	RelationTypes []RelationType `json:"relation_types"`
}

// SyncConstraintsRequest 同步约束到 Neo4j 的请求
type SyncConstraintsRequest struct {
	GraphID uint `json:"graph_id" binding:"required"`
}

// SyncSpatialLayersRequest 同步空间图层到 Neo4j 的请求
type SyncSpatialLayersRequest struct {
	GraphID uint `json:"graph_id" binding:"required"`
}

// ConstraintInfo 原生图约束信息
type ConstraintInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	EntityType string `json:"entity_type"`
	Field      string `json:"field"`
}

// CancelBuildResponse separates durable acceptance from an observed terminal state.
type CancelBuildResponse struct {
	ExecutionID           string `json:"execution_id"`
	Attempt               int    `json:"attempt"`
	Status                string `json:"status"`
	CancellationRequested bool   `json:"cancellation_requested"`
}

// BuildEnqueueResponse acknowledges durable admission before a worker claims it.
type BuildEnqueueResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
}
