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

type Resource struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Name           string         `gorm:"not null;index" json:"name"`
	ResourceType   string         `gorm:"not null" json:"resource_type"` // database, compute_engine
	ConnectionInfo ConnectionInfo `gorm:"type:json;not null" json:"connection_info"`
	Description    string         `gorm:"type:text" json:"description"`
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
}

type ResourceUpdateRequest struct {
	Name           *string         `json:"name"`
	ConnectionInfo *ConnectionInfo `json:"connection_info"`
	Description    *string         `json:"description"`
	IsActive       *bool           `json:"is_active"`
}