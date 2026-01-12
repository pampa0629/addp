package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// QuickView 快显模型
type QuickView struct {
	ID         uint           `gorm:"primaryKey" json:"id"`
	TenantID   uint           `gorm:"not null;index:idx_quick_view_tenant_resource" json:"tenant_id"`
	EngineID   uint           `gorm:"not null;index:idx_quick_view_tenant_resource" json:"engine_id"`
	SchemaName string         `gorm:"not null;size:255" json:"schema_name"`
	Table      string         `gorm:"column:table_name;not null;size:255" json:"table_name"`

	// 快显状态
	Status        string `gorm:"not null;default:'none';index:idx_quick_view_status" json:"status"` // none, generating, ready, failed, cancelled
	PreferredMode string `gorm:"default:'mvt';size:20" json:"preferred_mode"` // 用户偏好的显示模式：geojson | mvt
	ErrorMessage  string `gorm:"type:text" json:"error_message,omitempty"`

	// 缓存配置
	MinZoom       *int `gorm:"" json:"min_zoom,omitempty"`       // 自动计算得到
	MaxZoom       int  `gorm:"default:18" json:"max_zoom"`
	ActualMaxZoom *int `gorm:"" json:"actual_max_zoom,omitempty"` // 实际生成到第几层

	// 统计信息
	TotalTiles  int `gorm:"default:0" json:"total_tiles"`
	CachedTiles int `gorm:"default:0" json:"cached_tiles"`

	// 指纹（用于 MinIO 路径）
	Fingerprint string `gorm:"not null;size:64;index:idx_quick_view_fingerprint" json:"fingerprint"`

	// 空间范围（存储快照，避免依赖 meta）
	Extent     JSONFloatArray `gorm:"type:jsonb" json:"extent"`                               // [minLng, minLat, maxLng, maxLat]
	ExtentSRID int            `gorm:"column:extent_srid;default:4326" json:"extent_srid,omitempty"` // extent 的坐标系

	// 时间戳
	StartedAt   *time.Time `gorm:"" json:"started_at,omitempty"`
	CompletedAt *time.Time `gorm:"" json:"completed_at,omitempty"`
	CreatedAt   time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// TableName 指定表名
func (QuickView) TableName() string {
	return "manager.quick_view"
}

// JSONFloatArray 用于存储 float64 数组到 JSONB
type JSONFloatArray []float64

// Scan 实现 sql.Scanner 接口
func (j *JSONFloatArray) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	var arr []float64
	if err := json.Unmarshal(bytes, &arr); err != nil {
		return err
	}

	*j = arr
	return nil
}

// Value 实现 driver.Valuer 接口
func (j JSONFloatArray) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}

	bytes, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}

	return string(bytes), nil
}
