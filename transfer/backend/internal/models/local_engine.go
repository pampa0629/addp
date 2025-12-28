package models

import "time"

// LocalEngine 用于存储 Transfer 模块私有的存储引擎配置
type LocalEngine struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	TenantID       uint      `gorm:"not null;index" json:"tenant_id"`
	Name           string    `gorm:"type:varchar(255);not null" json:"name"`
	EngineType     string    `gorm:"type:varchar(50);not null;column:engine_type" json:"engine_type"`
	Description    string    `gorm:"type:text" json:"description,omitempty"`
	IsActive       bool      `gorm:"default:true" json:"is_active"`
	ConnectionInfo JSONMap   `gorm:"type:jsonb;not null" json:"connection_info"`
	CreatedBy      *uint     `json:"created_by,omitempty"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (LocalEngine) TableName() string {
	return "transfer.local_engines"
}
