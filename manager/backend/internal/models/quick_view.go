package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"

	commonmodels "github.com/addp/common/models"
)

// QuickView 快显模型
type QuickView struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	TenantID   uint   `gorm:"not null;index:idx_quick_view_tenant_resource" json:"tenant_id"`
	EngineID   uint   `gorm:"not null;index:idx_quick_view_tenant_resource" json:"engine_id"`
	SchemaName string `gorm:"not null;size:255" json:"schema_name"`
	Table      string `gorm:"column:table_name;not null;size:255" json:"table_name"`

	// 快显状态
	Status        string `gorm:"not null;default:'none';index:idx_quick_view_status" json:"status"` // none, generating, ready, failed, cancelled
	PreferredMode string `gorm:"default:'mvt';size:20" json:"preferred_mode"`                       // 用户偏好的显示模式：geojson | mvt
	ErrorMessage  string `gorm:"type:text" json:"error_message,omitempty"`

	// 缓存配置
	MinZoom       *int `gorm:"" json:"min_zoom,omitempty"` // 自动计算得到
	MaxZoom       int  `gorm:"default:18" json:"max_zoom"`
	ActualMaxZoom *int `gorm:"" json:"actual_max_zoom,omitempty"` // 实际生成到第几层

	// 统计信息
	TotalTiles  int `gorm:"default:0" json:"total_tiles"`
	CachedTiles int `gorm:"default:0" json:"cached_tiles"`

	// 指纹（用于 MinIO 路径）
	Fingerprint string `gorm:"not null;size:64;index:idx_quick_view_fingerprint" json:"fingerprint"`

	// 空间范围（存储快照，避免依赖 meta）
	Extent     JSONFloatArray `gorm:"type:jsonb" json:"extent"`                                     // [minLng, minLat, maxLng, maxLat]
	ExtentSRID int            `gorm:"column:extent_srid;default:4326" json:"extent_srid,omitempty"` // extent 的坐标系

	// 优化配置（v2.0 新增）
	OptimizationConfig *commonmodels.OptimizationConfig `gorm:"type:jsonb" json:"optimization_config,omitempty"`

	// 准备阶段状态（v4.0 新增）
	PreparationStatus *PreparationStatus `gorm:"type:jsonb" json:"preparation_status,omitempty"`

	// 时间戳
	StartedAt   *time.Time `gorm:"" json:"started_at,omitempty"`
	CompletedAt *time.Time `gorm:"" json:"completed_at,omitempty"`
	CreatedAt   time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (QuickView) TableName() string {
	return "manager.quick_view"
}

// PreparedQueryInfo 准备完成后的查询信息（避免瓦片生成时重复检查）
type PreparedQueryInfo struct {
	MaterializedViewExists bool   `json:"materialized_view_exists"` // 是否使用物化视图
	QueryTable             string `json:"query_table"`              // 实际查询的表名
	QueryGeomColumn        string `json:"query_geom_column"`        // 实际查询的几何列名
	QuerySRID              int    `json:"query_srid"`               // 实际查询的SRID
}

// PreparationExecution 准备阶段执行信息（用于故障排查）
type PreparationExecution struct {
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	DurationSec float64   `json:"duration_sec"`
	WorkerID    string    `json:"worker_id"` // 哪个 Worker 执行
	TaskID      string    `json:"task_id"`   // Asynq TaskID
	RetryCount  int       `json:"retry_count"`
	LastError   string    `json:"last_error,omitempty"`
}

// PreparationStatus 准备阶段检查结果
type PreparationStatus struct {
	Version       string                `json:"version"`                  // "1.0"
	Checks        []PreparationCheck    `json:"checks"`                   // 检查项列表
	OverallStatus string                `json:"overall_status"`           // "passed" | "failed"
	Summary       string                `json:"summary"`                  // 摘要
	CompletedAt   time.Time             `json:"completed_at"`             // 完成时间
	QueryInfo     *PreparedQueryInfo    `json:"query_info,omitempty"`     // 准备完成后的查询信息
	ExecutionInfo *PreparationExecution `json:"execution_info,omitempty"` // 故障排查信息
}

// Scan 实现 sql.Scanner 接口，用于从 JSONB 数据库字段读取
func (ps *PreparationStatus) Scan(value interface{}) error {
	if value == nil {
		*ps = PreparationStatus{}
		return nil
	}

	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	return json.Unmarshal(bytes, ps)
}

// Value 实现 driver.Valuer 接口，用于将数据写入 JSONB 数据库字段
func (ps PreparationStatus) Value() (driver.Value, error) {
	return json.Marshal(ps)
}

// PreparationCheck 单个检查项
type PreparationCheck struct {
	Name      string                 `json:"name"`       // "materialized_view" | "spatial_index" | "analyze"
	Status    string                 `json:"status"`     // "passed" | "failed" | "skipped"
	Message   string                 `json:"message"`    // 检查信息
	Details   map[string]interface{} `json:"details"`    // 详细信息
	CheckedAt time.Time              `json:"checked_at"` // 检查时间
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
