package models

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

// Ontology 本体模型（一套知识图谱的概念体系）
type Ontology struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	TenantID    uint           `gorm:"not null;index" json:"tenant_id"`
	Name        string         `gorm:"not null" json:"name"`
	Description string         `json:"description"`
	Status      string         `gorm:"default:'active'" json:"status"` // active, archived
	Metadata    datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`

	EntityTypes   []EntityType   `gorm:"foreignKey:OntologyID" json:"entity_types,omitempty"`
	RelationTypes []RelationType `gorm:"foreignKey:OntologyID" json:"relation_types,omitempty"`
	Versions      []OntologyVersion `gorm:"foreignKey:OntologyID" json:"versions,omitempty"`
}

// EntityType 实体类型定义
type EntityType struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	OntologyID  uint           `gorm:"not null;index" json:"ontology_id"`
	TenantID    uint           `gorm:"not null;index" json:"tenant_id"`
	Name        string         `gorm:"not null" json:"name"`        // 内部标识符 (英文)
	Label       string         `json:"label"`                        // 显示名称 (中文)
	Description string         `json:"description"`
	Color       string         `gorm:"default:'#5B8FF9'" json:"color"` // 可视化颜色
	Icon        string         `json:"icon"`
	ParentID    *uint          `json:"parent_id"` // 继承关系 (subClassOf)
	Properties  datatypes.JSON `gorm:"type:jsonb;default:'[]'" json:"properties"` // 属性定义列表
	Constraints datatypes.JSON `gorm:"type:jsonb;default:'[]'" json:"constraints"` // 约束规则列表
	SortOrder   int            `gorm:"default:0" json:"sort_order"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`

	Parent   *EntityType  `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Children []EntityType `gorm:"foreignKey:ParentID" json:"children,omitempty"`
}

// ParsedProperties 解析 JSONB 中的属性定义列表
func (et *EntityType) ParsedProperties() ([]PropertyDefinition, error) {
	if len(et.Properties) == 0 {
		return nil, nil
	}
	var props []PropertyDefinition
	if err := json.Unmarshal(et.Properties, &props); err != nil {
		return nil, err
	}
	return props, nil
}

// RelationType 关系类型定义
type RelationType struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	OntologyID   uint           `gorm:"not null;index" json:"ontology_id"`
	TenantID     uint           `gorm:"not null;index" json:"tenant_id"`
	Name         string         `gorm:"not null" json:"name"`   // 内部标识符 (英文)
	Label        string         `json:"label"`                   // 显示名称 (中文)
	Description  string         `json:"description"`
	SourceTypeID *uint          `json:"source_type_id"` // 来源实体类型 (nil=任意)
	TargetTypeID *uint          `json:"target_type_id"` // 目标实体类型 (nil=任意)
	Directed     bool           `gorm:"default:true" json:"directed"` // 是否有向
	Color        string         `gorm:"default:'#8B8B8B'" json:"color"`
	Properties   datatypes.JSON `gorm:"type:jsonb;default:'[]'" json:"properties"`
	Constraints  datatypes.JSON `gorm:"type:jsonb;default:'[]'" json:"constraints"`
	SortOrder    int            `gorm:"default:0" json:"sort_order"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`

	SourceType *EntityType `gorm:"foreignKey:SourceTypeID" json:"source_type,omitempty"`
	TargetType *EntityType `gorm:"foreignKey:TargetTypeID" json:"target_type,omitempty"`
}

// OntologyVersion 本体版本记录
type OntologyVersion struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	OntologyID  uint           `gorm:"not null;index" json:"ontology_id"`
	TenantID    uint           `gorm:"not null;index" json:"tenant_id"`
	Version     string         `gorm:"not null" json:"version"` // e.g. "1.0.0"
	Description string         `json:"description"`
	Snapshot    datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"snapshot"` // 版本快照（完整本体JSON）
	CreatedBy   uint           `json:"created_by"`
	CreatedAt   time.Time      `json:"created_at"`
}

// KnowledgeGraph 知识图谱实例
type KnowledgeGraph struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	TenantID    uint           `gorm:"not null;index" json:"tenant_id"`
	OntologyID  uint           `gorm:"not null;index" json:"ontology_id"`
	EngineID    uint           `gorm:"not null" json:"engine_id"` // Neo4j 引擎 ID (来自 System)
	Database    string         `gorm:"not null" json:"database"`   // Neo4j database 名称
	Name        string         `gorm:"not null" json:"name"`
	Description string         `json:"description"`
	Status      string         `gorm:"default:'active'" json:"status"` // active, building, error, archived
	IsPublic    bool           `gorm:"column:is_public;default:false" json:"is_public"`
	Stats       datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"stats"` // 统计信息缓存
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`

	Ontology *Ontology `gorm:"foreignKey:OntologyID" json:"ontology,omitempty"`
}
