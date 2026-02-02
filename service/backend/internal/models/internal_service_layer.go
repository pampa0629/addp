package models

import (
	"database/sql/driver"
	"time"

	"github.com/lib/pq"
)

// InternalServiceLayer 内部服务发布的图层
type InternalServiceLayer struct {
	ID uint `gorm:"primarykey" json:"id"`

	// 关联服务
	ServiceID uint `gorm:"not null;index:idx_internal_service_layer_service" json:"service_id"`

	// 图层标识
	LayerName   string `gorm:"not null;size:255" json:"layer_name"`
	Title       string `gorm:"not null;size:255" json:"title"`
	Abstract    string `gorm:"type:text" json:"abstract"`
	Keywords    StringArray `gorm:"type:text[]" json:"keywords"`

	// 关联元数据
	MetaItemID *uint `gorm:"index:idx_internal_service_layer_meta_item" json:"meta_item_id,omitempty"`

	// 数据源（冗余存储便于快速访问）
	SchemaName      string `gorm:"not null;size:255;index:idx_internal_service_layer_table" json:"schema_name"`
	DBTableName     string `gorm:"not null;size:255;index:idx_internal_service_layer_table" json:"table_name"`
	GeometryColumn  string `gorm:"size:255" json:"geometry_column"`  // NULL表示非空间数据

	// 空间元数据（在发布时捕获）
	SRID           int `gorm:"not null" json:"srid"`
	Extent4326     JSONB `gorm:"type:jsonb" json:"extent_4326,omitempty"` // [minLng, minLat, maxLng, maxLat]
	GeometryTypes  StringArray `gorm:"type:text[]" json:"geometry_types"`

	// MVT瓦片配置
	MVTBuffer            int     `gorm:"default:256" json:"mvt_buffer"`                      // MVT瓦片缓冲区（像素）
	MVTExtent            int     `gorm:"default:4096" json:"mvt_extent"`                     // MVT瓦片范围
	MVTSimplifyTolerance *float64 `json:"mvt_simplify_tolerance,omitempty"`                  // 几何简化容差（米）
	CacheControl         string  `gorm:"size:50;default:'public, max-age=3600'" json:"cache_control"`  // HTTP缓存策略

	// 服务配置
	Queryable      bool `gorm:"default:true" json:"queryable"`
	MaxFeatures    *int `json:"max_features,omitempty"`
	FilterColumns  StringArray `gorm:"type:text[]" json:"filter_columns"`

	// 样式配置
	DefaultStyle   JSONB `gorm:"type:jsonb" json:"default_style,omitempty"`

	// 排序和状态
	DisplayOrder   int `gorm:"default:0" json:"display_order"`
	Enabled        bool `gorm:"default:true" json:"enabled"`

	// 审计
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名
func (InternalServiceLayer) TableName() string {
	return "service.internal_service_layers"
}

// StringArray 字符串数组类型（用于 PostgreSQL text[]）
type StringArray []string

// Value 实现 driver.Valuer 接口
func (sa StringArray) Value() (driver.Value, error) {
	if sa == nil {
		return pq.Array([]string{}).Value()
	}
	return pq.Array(sa).Value()
}

// Scan 实现 sql.Scanner 接口
func (sa *StringArray) Scan(value interface{}) error {
	// 将 *StringArray 转换为 *[]string，然后使用 pq.Array 扫描
	a := (*[]string)(sa)
	return pq.Array(a).Scan(value)
}

// AddLayerRequest 添加图层请求
type AddLayerRequest struct {
	LayerName   string `json:"layer_name" binding:"required"`
	Title       string `json:"title" binding:"required"`
	Abstract    string `json:"abstract"`
	Keywords    []string `json:"keywords"`

	MetaItemID *uint `json:"meta_item_id,omitempty"`

	SchemaName     string `json:"schema_name" binding:"required"`
	DBTableName    string `json:"table_name" binding:"required"`
	GeometryColumn string `json:"geometry_column"` // 可选，优先从 Meta 获取

	SRID          int      `json:"srid"`           // 可选，优先从 Meta 获取
	Extent4326    JSONB    `json:"extent_4326,omitempty"`
	GeometryTypes []string `json:"geometry_types"` // 可选，优先从 Meta 获取

	MVTBuffer            int      `json:"mvt_buffer,omitempty"`
	MVTExtent            int      `json:"mvt_extent,omitempty"`
	MVTSimplifyTolerance *float64 `json:"mvt_simplify_tolerance,omitempty"`
	CacheControl         string   `json:"cache_control,omitempty"`

	Queryable     bool     `json:"queryable" binding:"omitempty"`
	MaxFeatures   *int     `json:"max_features,omitempty"`
	FilterColumns []string `json:"filter_columns,omitempty"`

	DefaultStyle JSONB `json:"default_style,omitempty"`
	DisplayOrder int   `json:"display_order,omitempty"`
}

// UpdateLayerRequest 更新图层请求
type UpdateLayerRequest struct {
	Title       *string `json:"title,omitempty"`
	Abstract    *string `json:"abstract,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`

	Queryable     *bool `json:"queryable,omitempty"`
	MaxFeatures   *int `json:"max_features,omitempty"`
	FilterColumns []string `json:"filter_columns,omitempty"`

	DefaultStyle *JSONB `json:"default_style,omitempty"`
	DisplayOrder *int `json:"display_order,omitempty"`
	Enabled      *bool `json:"enabled,omitempty"`
}

// InternalServiceLayerDTO 内部服务图层 DTO
type InternalServiceLayerDTO struct {
	ID uint `json:"id"`

	ServiceID uint `json:"service_id"`

	LayerName   string `json:"layer_name"`
	Title       string `json:"title"`
	Abstract    string `json:"abstract"`
	Keywords    []string `json:"keywords"`

	MetaItemID *uint `json:"meta_item_id,omitempty"`

	SchemaName     string `json:"schema_name"`
	DBTableName    string `json:"table_name"`
	GeometryColumn string `json:"geometry_column"`

	SRID          int `json:"srid"`
	Extent4326    JSONB `json:"extent_4326,omitempty"`
	GeometryTypes []string `json:"geometry_types"`

	MVTBuffer            int      `json:"mvt_buffer"`
	MVTExtent            int      `json:"mvt_extent"`
	MVTSimplifyTolerance *float64 `json:"mvt_simplify_tolerance,omitempty"`
	CacheControl         string   `json:"cache_control"`

	Queryable     bool `json:"queryable"`
	MaxFeatures   *int `json:"max_features,omitempty"`
	FilterColumns []string `json:"filter_columns,omitempty"`

	DefaultStyle JSONB `json:"default_style,omitempty"`
	DisplayOrder int `json:"display_order"`
	Enabled      bool `json:"enabled"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
