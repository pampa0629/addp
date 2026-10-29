package models

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
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

	EntityTypes   []EntityType      `gorm:"foreignKey:OntologyID" json:"entity_types,omitempty"`
	RelationTypes []RelationType    `gorm:"foreignKey:OntologyID" json:"relation_types,omitempty"`
	Versions      []OntologyVersion `gorm:"foreignKey:OntologyID" json:"versions,omitempty"`
}

// EntityType 实体类型定义
type EntityType struct {
	ID                 uint           `gorm:"primaryKey" json:"id"`
	OntologyID         uint           `gorm:"not null;index" json:"ontology_id"`
	TenantID           uint           `gorm:"not null;index" json:"tenant_id"`
	Name               string         `gorm:"not null" json:"name"` // 本体概念标识
	Label              string         `json:"label"`                // 显示名称
	Description        string         `json:"description"`
	NodeLabels         datatypes.JSON `gorm:"type:jsonb;default:'[]'" json:"node_labels,omitempty"` // 图节点标签执行映射
	Color              string         `gorm:"default:'#5B8FF9'" json:"color"`                       // 可视化颜色
	Icon               string         `json:"icon"`
	ParentID           *uint          `json:"parent_id"`                                                     // 继承关系 (subClassOf)
	DisplayProperty    string         `json:"display_property"`                                              // 节点展示字段（可引用继承的字符串属性）
	Properties         datatypes.JSON `gorm:"type:jsonb;default:'[]'" json:"properties"`                     // 属性定义列表
	Constraints        datatypes.JSON `gorm:"type:jsonb;default:'[]'" json:"constraints"`                    // 约束规则列表
	IsSpatialLayer     bool           `gorm:"default:false" json:"is_spatial_layer"`                         // 是否为空间图层类型
	SpatialLayerConfig datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"spatial_layer_config,omitempty"` // 空间图层配置
	SortOrder          int            `gorm:"default:0" json:"sort_order"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`

	Parent   *EntityType  `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Children []EntityType `gorm:"foreignKey:ParentID" json:"children,omitempty"`
}

// SpatialLayerConfig 空间图层配置
// GeometryType == "point": 使用 LonField + LatField（经纬度独立字段）
// GeometryType == "wkt":   使用 GeomField（WKT 字符串，WGS-84）
type SpatialLayerConfig struct {
	GeometryType string `json:"geometry_type"` // "point" | "wkt"
	LayerName    string `json:"layer_name"`    // Neo4j spatial layer name，默认使用实体类型名
	LonField     string `json:"lon_field"`     // 经度字段名，默认 "lon"（point 类型）
	LatField     string `json:"lat_field"`     // 纬度字段名，默认 "lat"（point 类型）
	GeomField    string `json:"geom_field"`    // WKT 属性名，默认 "wkt"（wkt 类型）
}

// ParsedSpatialLayerConfig 解析空间图层配置，未配置时返回带默认值的实例
func (et *EntityType) ParsedSpatialLayerConfig() *SpatialLayerConfig {
	cfg := &SpatialLayerConfig{
		LayerName: et.Name,
		LonField:  "lon",
		LatField:  "lat",
		GeomField: "wkt",
	}
	if len(et.SpatialLayerConfig) == 0 {
		return cfg
	}
	var parsed SpatialLayerConfig
	if err := json.Unmarshal(et.SpatialLayerConfig, &parsed); err != nil {
		return cfg
	}
	if parsed.LayerName == "" {
		parsed.LayerName = et.Name
	}
	if parsed.LonField == "" {
		parsed.LonField = "lon"
	}
	if parsed.LatField == "" {
		parsed.LatField = "lat"
	}
	if parsed.GeomField == "" {
		parsed.GeomField = "wkt"
	}
	return &parsed
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

// ParsedNodeLabels returns the explicit Neo4j node label mapping for this entity type.
func (et *EntityType) ParsedNodeLabels() []string {
	if len(et.NodeLabels) == 0 {
		return nil
	}
	var labels []string
	if err := json.Unmarshal(et.NodeLabels, &labels); err != nil {
		return nil
	}
	return normalizeStringList(labels)
}

func normalizeStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// RelationType 关系类型定义
type RelationType struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	OntologyID   uint           `gorm:"not null;index" json:"ontology_id"`
	TenantID     uint           `gorm:"not null;index" json:"tenant_id"`
	Name         string         `gorm:"not null" json:"name"` // 内部标识符 (英文)
	Label        string         `json:"label"`                // 显示名称 (中文)
	Description  string         `json:"description"`
	SourceTypeID *uint          `json:"source_type_id"`               // 来源实体类型 (nil=任意)
	TargetTypeID *uint          `json:"target_type_id"`               // 目标实体类型 (nil=任意)
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
	AdmissionReconciledBy         uint                     `json:"-"`
	AdmissionReconciliationDigest string                   `gorm:"size:64" json:"-"`
	ID                            uint                     `gorm:"primaryKey" json:"id"`
	Version                       int64                    `gorm:"type:bigint;not null;default:1" json:"version"`
	AdmissionClosed               bool                     `gorm:"not null;default:false" json:"-"`
	AdmissionCause                string                   `json:"-"`
	AdmissionExecutionID          string                   `gorm:"size:64" json:"-"`
	AdmissionReconciledAt         *time.Time               `json:"-"`
	PhysicalTargetIdentity        string                   `json:"-"`
	Admission                     *GraphAdmissionView      `gorm:"-" json:"admission,omitempty"`
	TenantID                      uint                     `gorm:"not null;index;uniqueIndex:idx_graph_target" json:"tenant_id"`
	OntologyID                    uint                     `gorm:"not null;index" json:"ontology_id"`
	EngineID                      uint                     `gorm:"not null;uniqueIndex:idx_graph_target" json:"engine_id"`
	CatalogPath                   plugin.EngineCatalogPath `gorm:"serializer:json;type:jsonb;not null;uniqueIndex:idx_graph_target" json:"catalog_path"`
	IndexManagement               string                   `gorm:"not null" json:"index_management"`
	Name                          string                   `gorm:"not null" json:"name"`
	Description                   string                   `json:"description"`
	Status                        string                   `gorm:"default:'active'" json:"status"` // active, building, error, archived
	IsPublic                      bool                     `gorm:"column:is_public;default:false" json:"is_public"`
	Stats                         datatypes.JSON           `gorm:"type:jsonb;default:'{}'" json:"stats"` // 统计信息缓存
	CreatedAt                     time.Time                `json:"created_at"`
	UpdatedAt                     time.Time                `json:"updated_at"`

	Ontology *Ontology `gorm:"foreignKey:OntologyID" json:"ontology,omitempty"`
}

const (
	GraphIndexesManaged  = "managed"
	GraphIndexesExternal = "external"
)
