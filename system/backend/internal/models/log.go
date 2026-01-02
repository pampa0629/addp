package models

import (
	"time"
)

type AuditLog struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UserID       *uint     `gorm:"index" json:"user_id"`
	Username     string    `json:"username"`
	TenantID     *uint     `gorm:"index" json:"tenant_id"` // 租户ID,SuperAdmin操作为null
	Action       string    `gorm:"not null" json:"action"`
	EntityType   string    `json:"entity_type"`              // 资源类型（如：user, engine, tenant）
	EntityID     string    `json:"entity_id"`                // 资源ID
	Details      string    `gorm:"type:text" json:"details"` // 操作详情
	IPAddress    string    `json:"ip_address"`
	ModuleName   string    `gorm:"index" json:"module_name"` // 来源模块（如：system, manager, meta）
	CreatedAt    time.Time `gorm:"index" json:"created_at"`
}

type AuditLogCreateRequest struct {
	UserID     *uint   `json:"user_id"`
	Username   *string `json:"username"`
	TenantID   *uint   `json:"tenant_id"`
	Action     string  `json:"action" binding:"required"`
	EntityType string  `json:"entity_type"` // 资源类型
	EntityID   string  `json:"entity_id"`   // 资源ID
	Details    string  `json:"details"`     // 操作详情
	IPAddress  string  `json:"ip_address"`
	ModuleName string  `json:"module_name"` // 来源模块
}

// AuditLogFilters 审计日志查询过滤条件
type AuditLogFilters struct {
	UserID     *uint  // 用户ID
	StartTime  string // 开始时间
	EndTime    string // 结束时间
	Action     string // 操作类型（HTTP方法）
	EntityType string // 资源类型
	Username   string // 用户名
	IPAddress  string // IP地址
	ModuleName string // 来源模块
}