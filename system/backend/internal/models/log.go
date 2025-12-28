package models

import (
	"time"
)

type AuditLog struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UserID       *uint     `gorm:"index" json:"user_id"`
	Username     string    `json:"username"`
	TenantID     *uint     `gorm:"index" json:"tenant_id"` // 租户ID,SuperAdmin操作为null
	Action     string    `gorm:"not null" json:"action"`
	EngineType string    `json:"engine_type"`
	EngineID   string    `json:"engine_id"`
	Details    string    `gorm:"type:text" json:"details"`
	IPAddress    string    `json:"ip_address"`
	CreatedAt    time.Time `gorm:"index" json:"created_at"`
}

type AuditLogCreateRequest struct {
	Action     string `json:"action" binding:"required"`
	EngineType string `json:"engine_type"`
	EngineID   string `json:"engine_id"`
	Details    string `json:"details"`
}