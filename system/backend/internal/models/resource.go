package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

type ConnectionInfo map[string]interface{}

func (c ConnectionInfo) Value() (driver.Value, error) {
	return json.Marshal(c)
}

func (c *ConnectionInfo) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, c)
}

// ScanConfig 元数据扫描配置
type ScanConfig struct {
	Enabled        bool                 `json:"enabled"`                   // 是否启用扫描（总开关，兼容旧版）
	ImmediateScan  bool                 `json:"immediate_scan"`            // 注册后立即扫描
	ImmediateDepth string               `json:"immediate_depth,omitempty"` // 立即扫描深度：basic（基础）或 deep（深度）
	ScheduledScan  bool                 `json:"scheduled_scan"`            // 启用定时扫描（深度固定为 deep）
	ScheduleType   string               `json:"schedule_type"`             // daily, weekly, monthly, cron（仅当scheduled_scan=true时有效）
	CronExpression string               `json:"cron_expression,omitempty"` // Cron 表达式（schedule_type=cron 时使用）
	ScheduleTime   string               `json:"schedule_time,omitempty"`   // 执行时间 HH:mm（daily/weekly/monthly 时使用）
	ScheduleValue  []int                `json:"schedule_value,omitempty"`  // 周几（0-6）或月几（1-31）
	ScanDepth      string               `json:"scan_depth"`                // 兼容旧版字段：shallow, deep
	SchemaNames    []string             `json:"schema_names,omitempty"`    // 已废弃：PostgreSQL schemas（系统自动过滤）
	ObjectPaths    []string             `json:"object_paths,omitempty"`    // 已废弃：MinIO prefixes（系统自动过滤）
	Preprocessing  *PreprocessingConfig `json:"preprocessing,omitempty"`   // 预处理配置（可选）
}

// PreprocessingConfig 预处理配置
// 预处理是指在扫描完成后对数据进行额外的处理，以优化后续的访问性能
type PreprocessingConfig struct {
	Enabled     bool              `json:"enabled"`              // 是否启用预处理
	AutoTrigger bool              `json:"auto_trigger"`         // 扫描完成后自动触发预处理
	Types       []string          `json:"types"`                // 预处理类型列表 ["mvt_tiles", "vector_embedding"]
	MVTConfig   *MVTPreprocessConfig `json:"mvt_config,omitempty"` // MVT 瓦片预处理配置
}

// MVTPreprocessConfig MVT 瓦片预处理配置
// MVT (Mapbox Vector Tiles) 用于空间数据的高性能可视化
type MVTPreprocessConfig struct {
	MaxZoom          int     `json:"max_zoom"`           // 最大缩放级别 (0-18, 默认 18)
	Concurrency      int     `json:"concurrency"`        // 并发生成数 (1-20, 默认 10)
	StopThresholdSec float64 `json:"stop_threshold_sec"` // 自适应停止阈值：平均生成时间（秒）< 该值则停止（默认 3.0）
	StopThresholdKB  float64 `json:"stop_threshold_kb"`  // 自适应停止阈值：平均瓦片大小（KB）< 该值则停止（默认 50.0）
}

func (s ScanConfig) Value() (driver.Value, error) {
	return json.Marshal(s)
}

func (s *ScanConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, s)
}

type Resource struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Name           string         `gorm:"not null;index" json:"name"`
	DisplayName    string         `gorm:"not null;size:255" json:"display_name"` // 中文显示名称
	ResourceType   string         `gorm:"not null" json:"resource_type"` // database, compute_engine, object_storage
	ConnectionInfo ConnectionInfo `gorm:"type:json;not null" json:"connection_info"`
	Description    string         `gorm:"type:text" json:"description"`
	ScanConfig     *ScanConfig    `gorm:"type:json" json:"scan_config,omitempty"` // 元数据扫描配置（可选）

	// 能力注册字段（用于 Orchestrator 动态发现）
	UniqueIdentifier  *string        `gorm:"size:255;uniqueIndex:idx_unique_identifier" json:"unique_identifier,omitempty"` // 逻辑标识符（如 "meta.scanner.default"）
	IsBuiltin         bool           `gorm:"default:false;index" json:"is_builtin"`                                        // 是否为内置引擎（内置引擎不可删除）
	Capabilities      *string        `gorm:"type:jsonb" json:"capabilities,omitempty"`                                     // 能力声明（JSONB）
	TaskAPIConfig     *string        `gorm:"type:jsonb" json:"task_api_config,omitempty"`                                  // 任务 API 配置（JSONB，仅计算引擎）
	HealthCheckConfig *string        `gorm:"type:jsonb" json:"health_check_config,omitempty"`                              // 健康检查配置（JSONB）

	CreatedBy      *uint          `json:"created_by"`
	TenantID       *uint          `gorm:"index" json:"tenant_id"` // 租户ID,SuperAdmin创建的资源为null
	IsActive       bool           `gorm:"default:true" json:"is_active"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type ResourceCreateRequest struct {
	Name           string         `json:"name" binding:"required"`
	DisplayName    string         `json:"display_name"` // 中文显示名称（可选，未提供时默认等于 name）
	ResourceType   string         `json:"resource_type" binding:"required"`
	ConnectionInfo ConnectionInfo `json:"connection_info" binding:"required"`
	Description    string         `json:"description"`
	ScanConfig     *ScanConfig    `json:"scan_config"` // 扫描配置（可选）
}

type ResourceUpdateRequest struct {
	Name           *string         `json:"name"`
	DisplayName    *string         `json:"display_name"` // 支持更新显示名称
	ConnectionInfo *ConnectionInfo `json:"connection_info"`
	Description    *string         `json:"description"`
	IsActive       *bool           `json:"is_active"`
	ScanConfig     *ScanConfig     `json:"scan_config"` // 扫描配置（可选）
}