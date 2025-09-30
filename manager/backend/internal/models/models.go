package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// DataSource 从 System 模块引用的存储引擎
type DataSource struct {
	ID             uint           `json:"id" gorm:"primaryKey"`
	SystemResourceID uint         `json:"system_resource_id" gorm:"uniqueIndex"` // System 模块的资源 ID
	Name           string         `json:"name"`
	ResourceType   string         `json:"resource_type"` // postgresql, minio
	ConnectionInfo ConnectionInfo `json:"connection_info" gorm:"type:jsonb"`
	Description    string         `json:"description"`
	Status         string         `json:"status"` // active, inactive, error
	LastChecked    *time.Time     `json:"last_checked"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// Directory 目录结构
type Directory struct {
	ID        uint       `json:"id" gorm:"primaryKey"`
	Name      string     `json:"name"`
	ParentID  *uint      `json:"parent_id"`
	Path      string     `json:"path" gorm:"index"`
	Type      string     `json:"type"` // folder, file
	Size      int64      `json:"size"`
	MimeType  string     `json:"mime_type"`
	StorageID *uint      `json:"storage_id"` // 关联的存储引擎 ID
	CreatedBy uint       `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// ConnectionInfo 存储连接信息
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

// DataSourceListResponse API 响应
type DataSourceListResponse struct {
	Data  []DataSource `json:"data"`
	Total int          `json:"total"`
}

// SystemResource System 模块的资源结构
type SystemResource struct {
	ID             uint           `json:"id"`
	Name           string         `json:"name"`
	ResourceType   string         `json:"resource_type"`
	ConnectionInfo ConnectionInfo `json:"connection_info"`
	Description    string         `json:"description"`
	IsActive       bool           `json:"is_active"`
	CreatedAt      time.Time      `json:"created_at"`
}