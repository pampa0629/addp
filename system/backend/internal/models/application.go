package models

import (
	"time"

	"github.com/lib/pq"
)

// Application 应用管理模型（用于外部应用的 API Key 管理）
type Application struct {
	ID                  uint           `gorm:"primaryKey" json:"id"`
	Name                string         `gorm:"type:varchar(255);not null" json:"name"`
	Description         string         `gorm:"type:text" json:"description,omitempty"`
	TenantID            uint           `gorm:"not null;index" json:"tenant_id"`
	AllowedServices     pq.StringArray `gorm:"type:text[]" json:"allowed_services,omitempty"` // 允许访问的服务列表
	RateLimitPerMinute  int            `gorm:"default:60" json:"rate_limit_per_minute"`       // 每分钟请求数限制
	Status              string         `gorm:"type:varchar(50);default:'active'" json:"status"`
	CreatedBy           *uint          `json:"created_by,omitempty"`
	CreatedAt           time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt           time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt           *time.Time     `gorm:"index" json:"deleted_at,omitempty"` // 软删除

	// 关联
	APIKeys []APIKey `gorm:"foreignKey:ApplicationID;constraint:OnDelete:CASCADE" json:"api_keys,omitempty"`
}

// TableName 指定表名
func (Application) TableName() string {
	return "system.applications"
}

// APIKey API Key 模型
type APIKey struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	ApplicationID uint       `gorm:"not null;index" json:"application_id"`
	KeyPrefix     string     `gorm:"type:varchar(20);not null" json:"key_prefix"` // "addp_live_" 前缀
	KeyHash       string     `gorm:"type:varchar(64);not null;uniqueIndex" json:"-"` // SHA256 hash（不返回给前端）
	Name          string     `gorm:"type:varchar(255)" json:"name,omitempty"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	Status        string     `gorm:"type:varchar(50);default:'active';index" json:"status"`
	CreatedBy     *uint      `json:"created_by,omitempty"`
	CreatedAt     time.Time  `gorm:"autoCreateTime" json:"created_at"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	RevokedBy     *uint      `json:"revoked_by,omitempty"`

	// 临时字段（仅在创建时返回明文 Key）
	PlainTextKey string `gorm:"-" json:"plain_text_key,omitempty"`
}

// TableName 指定表名
func (APIKey) TableName() string {
	return "system.api_keys"
}

// CreateApplicationRequest 创建应用请求
type CreateApplicationRequest struct {
	Name               string   `json:"name" binding:"required"`
	Description        string   `json:"description"`
	AllowedServices    []string `json:"allowed_services"`
	RateLimitPerMinute int      `json:"rate_limit_per_minute"`
}

// UpdateApplicationRequest 更新应用请求
type UpdateApplicationRequest struct {
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	AllowedServices    []string `json:"allowed_services"`
	RateLimitPerMinute int      `json:"rate_limit_per_minute"`
	Status             string   `json:"status"`
}

// CreateAPIKeyRequest 创建 API Key 请求
type CreateAPIKeyRequest struct {
	Name      string     `json:"name"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// APIKeyValidationRequest API Key 验证请求（仅供 Gateway 内部调用）
type APIKeyValidationRequest struct {
	KeyHash string `json:"key_hash" binding:"required"`
}

// APIKeyValidationResponse API Key 验证响应
type APIKeyValidationResponse struct {
	Valid              bool       `json:"valid"`
	AppID              uint       `json:"app_id"`
	AppName            string     `json:"app_name"`
	AllowedServices    []string   `json:"allowed_services"`
	RateLimitPerMinute int        `json:"rate_limit_per_minute"`
	ExpiresAt          *time.Time `json:"expires_at"`
}
