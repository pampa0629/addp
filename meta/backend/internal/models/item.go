package models

import (
	"time"

	"gorm.io/gorm"
)

// MetaItem 表示引擎下的最终数据项，例如表或对象
type MetaItem struct {
	ID            uint           `gorm:"primaryKey" json:"id"`
	TenantID      uint           `gorm:"not null;index:idx_meta_item_engine_tenant,priority:1" json:"tenant_id"`
	EngineID      uint           `gorm:"column:engine_id;not null;index:idx_meta_item_engine_tenant,priority:2" json:"engine_id"`
	NodeID        uint           `gorm:"not null;index" json:"node_id"`
	ItemType      string         `gorm:"size:64;not null;index:idx_meta_item_type" json:"item_type"`
	Name          string         `gorm:"size:255;not null" json:"name"`
	FullName      string         `gorm:"type:text" json:"full_name,omitempty"`
	Fingerprint   string         `gorm:"size:64;not null;uniqueIndex:idx_meta_item_fingerprint" json:"fingerprint" comment:"数据指纹：基于engine_id+路径的SHA256哈希，用于去重和数据血缘追踪"`
	RowCount      *int64         `json:"row_count,omitempty"`
	SizeBytes     *int64         `json:"size_bytes,omitempty"`
	DataUpdatedAt *time.Time     `json:"data_updated_at,omitempty" comment:"被扫描数据的最后更新时间"`
	ScannedAt     *time.Time     `json:"scanned_at,omitempty" comment:"数据项的扫描时间"`
	ScannedDepth  string         `gorm:"size:10;default:'none';index" json:"scanned_depth" comment:"已完成扫描深度：none/basic/deep"`
	Attributes    JSONMap        `gorm:"type:jsonb" json:"attributes,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MetaItem) TableName() string {
	return "metadata.meta_item"
}

// SpatialMetadata 空间表元数据（用于 attributes 字段存储）
type SpatialMetadata struct {
	GeometryColumn  string    `json:"geometry_column"`   // 几何列名
	SRID            int       `json:"srid"`              // 表的原始空间参考系
	ExtentSRID      int       `json:"extent_srid"`       // extent 的坐标系（总是 4326）
	Extent          []float64 `json:"extent"`            // 边界范围 [minX, minY, maxX, maxY]（WGS84 度）
	GeometryTypes   []string  `json:"geometry_types"`    // 几何类型列表
	HasSpatialIndex bool      `json:"has_spatial_index"` // 是否有空间索引
	IndexName       string    `json:"index_name"`        // 空间索引名称
	HasUpdatedAt    bool      `json:"has_updated_at"`    // 是否有更新时间字段（用于增量同步）
	UpdatedAtColumn string    `json:"updated_at_column"` // 更新时间字段名
}

// TableStats 表统计信息（用于变更检测）
type TableStats struct {
	TotalChanges int64     `json:"total_changes"` // 插入+更新+删除总次数
	LiveTuples   int64     `json:"live_tuples"`   // 活跃行数
	DeadTuples   int64     `json:"dead_tuples"`   // 死亡行数
	LastAnalyze  time.Time `json:"last_analyze"`  // 最后 ANALYZE 时间
}
