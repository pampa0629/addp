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
	ModuleName   string    `gorm:"index" json:"module_name"`                  // 来源模块（如：system, manager, meta）
	HTTPStatus   *int      `gorm:"index:idx_audit_logs_status" json:"http_status"` // HTTP状态码
	DurationMs   *int      `json:"duration_ms"`                               // 请求耗时（毫秒）
	LogLevel     string    `gorm:"default:'INFO';index:idx_audit_logs_level" json:"log_level"` // 日志级别
	ErrorMessage string    `gorm:"type:text" json:"error_message"`            // 错误信息
	RequestID    string    `gorm:"size:64" json:"request_id"`                 // 请求追踪ID
	CreatedAt    time.Time `gorm:"index" json:"created_at"`
}

type AuditLogCreateRequest struct {
	UserID       *uint   `json:"user_id"`
	Username     *string `json:"username"`
	TenantID     *uint   `json:"tenant_id"`
	Action       string  `json:"action" binding:"required"`
	EntityType   string  `json:"entity_type"`    // 资源类型
	EntityID     string  `json:"entity_id"`      // 资源ID
	Details      string  `json:"details"`        // 操作详情
	IPAddress    string  `json:"ip_address"`
	ModuleName   string  `json:"module_name"`    // 来源模块
	HTTPStatus   *int    `json:"http_status"`    // HTTP状态码（新增）
	DurationMs   *int    `json:"duration_ms"`    // 请求耗时（新增）
	LogLevel     string  `json:"log_level"`      // 日志级别（新增）
	ErrorMessage string  `json:"error_message"`  // 错误信息（新增）
	RequestID    string  `json:"request_id"`     // 请求追踪ID（新增）
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
	StatusCode string // HTTP状态码范围（如 "2xx", "4xx", "5xx"）
}