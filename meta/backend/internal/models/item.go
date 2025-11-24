package models

import (
	"time"

	"gorm.io/gorm"
)

// MetaItem 表示资源下的最终数据项，例如表或对象
type MetaItem struct {
	ID                uint           `gorm:"primaryKey" json:"id"`
	TenantID          uint           `gorm:"not null;index:idx_meta_item_res_tenant,priority:1" json:"tenant_id"`
	ResID             uint           `gorm:"not null;index:idx_meta_item_res_tenant,priority:2" json:"res_id"`
	NodeID            uint           `gorm:"not null;index" json:"node_id"`
	ItemType          string         `gorm:"size:64;not null;index:idx_meta_item_type" json:"item_type"`
	Name              string         `gorm:"size:255;not null" json:"name"`
	FullName          string         `gorm:"type:text" json:"full_name,omitempty"`
	Fingerprint       string         `gorm:"size:64;not null;uniqueIndex:idx_meta_item_fingerprint" json:"fingerprint" comment:"数据指纹：基于res_id+路径的SHA256哈希，用于去重和数据血缘追踪"`
	Status            string         `gorm:"size:20;default:'active'" json:"status"`
	MetaSchemaVersion int            `gorm:"default:1" json:"meta_schema_version"`
	RowCount          *int64         `json:"row_count,omitempty"`
	SizeBytes         *int64         `json:"size_bytes,omitempty"`
	ObjectSizeBytes   *int64         `json:"object_size_bytes,omitempty"`
	LastModifiedAt    *time.Time     `json:"last_modified_at,omitempty"`
	Attributes        JSONMap        `gorm:"type:jsonb" json:"attributes,omitempty"`
	SyncVersion       int64          `gorm:"default:0" json:"sync_version"`
	Source            string         `gorm:"size:64" json:"source,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MetaItem) TableName() string {
	return "metadata.meta_item"
}

// SpatialMetadata 空间表元数据（用于 attributes 字段存储）
type SpatialMetadata struct {
	GeometryColumn  string     `json:"geometry_column"`   // 几何列名
	SRID            int        `json:"srid"`              // 空间参考系
	Extent          []float64  `json:"extent"`            // 边界范围 [minX, minY, maxX, maxY]
	GeometryTypes   []string   `json:"geometry_types"`    // 几何类型列表
	HasSpatialIndex bool       `json:"has_spatial_index"` // 是否有空间索引
	IndexName       string     `json:"index_name"`        // 空间索引名称
	HasUpdatedAt    bool       `json:"has_updated_at"`    // 是否有更新时间字段（用于增量同步）
	UpdatedAtColumn string     `json:"updated_at_column"` // 更新时间字段名
}

// TableStats 表统计信息（用于变更检测）
type TableStats struct {
	TotalChanges int64     `json:"total_changes"` // 插入+更新+删除总次数
	LiveTuples   int64     `json:"live_tuples"`   // 活跃行数
	DeadTuples   int64     `json:"dead_tuples"`   // 死亡行数
	LastAnalyze  time.Time `json:"last_analyze"`  // 最后 ANALYZE 时间
}

