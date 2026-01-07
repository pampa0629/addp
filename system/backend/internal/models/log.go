package models

import (
	"time"
)

type AuditLog struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	CreatedAt    time.Time `gorm:"index" json:"created_at"`

	// 用户信息 (允许 NULL)
	UserID   *uint  `gorm:"index" json:"user_id"`
	Username string `json:"username"`
	TenantID *uint  `gorm:"index" json:"tenant_id"` // 租户ID,SuperAdmin操作为null

	// 操作信息 (必填)
	HTTPMethod   string `gorm:"size:10;not null" json:"http_method"`   // GET/POST/PUT/DELETE/PATCH
	ResourcePath string `gorm:"not null" json:"resource_path"`         // /api/users/123
	HTTPStatus   int    `gorm:"not null;index" json:"http_status"`     // HTTP状态码
	DurationMs   int    `gorm:"not null" json:"duration_ms"`           // 请求耗时(毫秒)

	// 资源标识 (可选)
	EntityType string `gorm:"size:50" json:"entity_type"`  // 资源类型（如：user, engine, tenant）
	EntityID   string `gorm:"size:255" json:"entity_id"`   // 资源ID

	// 请求详情
	RequestBody  string `gorm:"type:text" json:"request_body"`  // 脱敏的JSON请求体
	QueryParams  string `gorm:"type:text" json:"query_params"`  // URL查询参数
	UserAgent    string `gorm:"size:500" json:"user_agent"`     // 客户端User-Agent
	IPAddress    string `gorm:"type:inet;not null" json:"ip_address"` // 客户端IP(使用INET类型)

	// 错误和追踪
	LogLevel     string `gorm:"default:'INFO';index" json:"log_level"`  // 日志级别
	ErrorMessage string `gorm:"type:text" json:"error_message"`         // 错误信息
	RequestID    string `gorm:"size:64;not null;index" json:"request_id"` // 请求追踪ID
	ModuleName   string `gorm:"size:50;not null;index" json:"module_name"` // 来源模块（如：system, manager, meta）
}

type AuditLogCreateRequest struct {
	UserID       *uint   `json:"user_id"`
	Username     *string `json:"username"`
	TenantID     *uint   `json:"tenant_id"`
	HTTPMethod   string  `json:"http_method" binding:"required"`   // HTTP方法
	ResourcePath string  `json:"resource_path" binding:"required"` // 资源路径
	HTTPStatus   int     `json:"http_status" binding:"required"`   // HTTP状态码
	DurationMs   int     `json:"duration_ms" binding:"required"`   // 请求耗时
	EntityType   string  `json:"entity_type"`                      // 资源类型
	EntityID     string  `json:"entity_id"`                        // 资源ID
	RequestBody  string  `json:"request_body"`                     // 操作详情
	QueryParams  string  `json:"query_params"`                     // URL查询参数
	UserAgent    string  `json:"user_agent"`                       // 客户端UA
	IPAddress    string  `json:"ip_address" binding:"required"`    // 客户端IP
	ModuleName   string  `json:"module_name" binding:"required"`   // 来源模块
	LogLevel     string  `json:"log_level"`                        // 日志级别
	ErrorMessage string  `json:"error_message"`                    // 错误信息
	RequestID    string  `json:"request_id" binding:"required"`    // 请求追踪ID
}

// AuditLogFilters 审计日志查询过滤条件
type AuditLogFilters struct {
	UserID       *uint  // 用户ID
	TenantID     *uint  // 租户ID
	StartTime    string // 开始时间
	EndTime      string // 结束时间
	HTTPMethod   string // HTTP方法（POST/PUT/DELETE等）
	ResourcePath string // 资源路径（前缀匹配）
	EntityType   string // 资源类型
	Username     string // 用户名
	IPAddress    string // IP地址
	ModuleName   string // 来源模块
	StatusCode   string // HTTP状态码范围（如 "2xx", "4xx", "5xx"）
	LogLevel     string // 日志级别（INFO/WARN/ERROR）
	RequestID    string // 请求追踪ID
	MinDuration  int    // 最小耗时（慢请求分析）
}