package models

import "time"

// AuditLog 审计日志模型（共享定义）
type AuditLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	UserID     *uint     `gorm:"index" json:"user_id"`
	Username   string    `json:"username"`
	TenantID   *uint     `gorm:"index" json:"tenant_id"`
	Action     string    `gorm:"not null" json:"action"`
	EntityType string    `json:"entity_type"`
	EntityID   string    `json:"entity_id"`
	Details    string    `gorm:"type:text" json:"details"`
	IPAddress  string    `json:"ip_address"`
	ModuleName string    `gorm:"index" json:"module_name"` // 来源模块
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

// AuditLogCreateRequest 创建审计日志请求（用于跨模块调用）
type AuditLogCreateRequest struct {
	UserID     *uint   `json:"user_id"`
	Username   *string `json:"username"`
	TenantID   *uint   `json:"tenant_id"`
	Action     string  `json:"action" binding:"required"`
	EntityType string  `json:"entity_type"`
	EntityID   string  `json:"entity_id"`
	Details    string  `json:"details"`
	IPAddress  string  `json:"ip_address"`
	ModuleName string  `json:"module_name"` // 来源模块
}
