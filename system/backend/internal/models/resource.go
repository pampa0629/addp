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
	Enabled        bool     `json:"enabled"`                   // 是否启用扫描
	ScheduleType   string   `json:"schedule_type"`             // manual, daily, weekly, monthly, cron
	CronExpression string   `json:"cron_expression,omitempty"` // Cron 表达式（schedule_type=cron 时使用）
	ScheduleTime   string   `json:"schedule_time,omitempty"`   // 执行时间 HH:mm（daily/weekly/monthly 时使用）
	ScheduleValue  []int    `json:"schedule_value,omitempty"`  // 周几（0-6）或月几（1-31）
	ScanDepth      string   `json:"scan_depth"`                // shallow, deep
	SchemaNames    []string `json:"schema_names,omitempty"`    // PostgreSQL schemas（留空表示扫描所有）
	ObjectPaths    []string `json:"object_paths,omitempty"`    // MinIO prefixes（留空表示扫描根目录）
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
	ResourceType   string         `gorm:"not null" json:"resource_type"` // database, compute_engine
	ConnectionInfo ConnectionInfo `gorm:"type:json;not null" json:"connection_info"`
	Description    string         `gorm:"type:text" json:"description"`
	ScanConfig     *ScanConfig    `gorm:"type:json" json:"scan_config,omitempty"` // 元数据扫描配置（可选）
	CreatedBy      *uint          `json:"created_by"`
	TenantID       *uint          `gorm:"index" json:"tenant_id"` // 租户ID,SuperAdmin创建的资源为null
	IsActive       bool           `gorm:"default:true" json:"is_active"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type ResourceCreateRequest struct {
	Name           string         `json:"name" binding:"required"`
	ResourceType   string         `json:"resource_type" binding:"required"`
	ConnectionInfo ConnectionInfo `json:"connection_info" binding:"required"`
	Description    string         `json:"description"`
	ScanConfig     *ScanConfig    `json:"scan_config"` // 扫描配置（可选）
}

type ResourceUpdateRequest struct {
	Name           *string         `json:"name"`
	ConnectionInfo *ConnectionInfo `json:"connection_info"`
	Description    *string         `json:"description"`
	IsActive       *bool           `json:"is_active"`
	ScanConfig     *ScanConfig     `json:"scan_config"` // 扫描配置（可选）
}