package models

import "time"

// AuditLog 审计日志模型（共享定义）
type AuditLog struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	UserID       *uint     `gorm:"index" json:"user_id"`
	Username     string    `json:"username"`
	TenantID     *uint     `gorm:"index" json:"tenant_id"`
	Action       string    `gorm:"not null" json:"action"`
	EntityType   string    `json:"entity_type"`
	EntityID     string    `json:"entity_id"`
	Details      string    `gorm:"type:text" json:"details"`
	IPAddress    string    `json:"ip_address"`
	ModuleName   string    `gorm:"index" json:"module_name"`                  // 来源模块
	HTTPStatus   *int      `gorm:"index:idx_audit_logs_status" json:"http_status"` // HTTP状态码
	DurationMs   *int      `json:"duration_ms"`                               // 请求耗时（毫秒）
	LogLevel     string    `gorm:"default:'INFO';index:idx_audit_logs_level" json:"log_level"` // 日志级别
	ErrorMessage string    `gorm:"type:text" json:"error_message"`            // 错误信息
	RequestID    string    `gorm:"size:64" json:"request_id"`                 // 请求追踪ID
	CreatedAt    time.Time `gorm:"index" json:"created_at"`
}

// AuditLogCreateRequest 创建审计日志请求（用于跨模块调用）
type AuditLogCreateRequest struct {
	UserID       *uint   `json:"user_id"`
	Username     *string `json:"username"`
	TenantID     *uint   `json:"tenant_id"`
	Action       string  `json:"action" binding:"required"`
	EntityType   string  `json:"entity_type"`
	EntityID     string  `json:"entity_id"`
	Details      string  `json:"details"`
	IPAddress    string  `json:"ip_address"`
	ModuleName   string  `json:"module_name"`   // 来源模块
	HTTPStatus   *int    `json:"http_status"`   // HTTP状态码
	DurationMs   *int    `json:"duration_ms"`   // 请求耗时（毫秒）
	LogLevel     string  `json:"log_level"`     // 日志级别
	ErrorMessage string  `json:"error_message"` // 错误信息
	RequestID    string  `json:"request_id"`    // 请求追踪ID
}
