package models

import "time"

const APIConsumerServiceTypeQuery = "query"

// APIConsumer 是 Tenant 为外部系统登记的数据面 API 调用方，不是 Principal。
type APIConsumer struct {
	ID                   uint                      `gorm:"primaryKey" json:"id"`
	TenantID             uint                      `gorm:"not null;index" json:"tenant_id"`
	Name                 string                    `gorm:"type:varchar(120);not null" json:"name"`
	Description          string                    `gorm:"type:varchar(500);not null;default:''" json:"description"`
	RateLimitPerMinute   int                       `gorm:"not null;default:60" json:"rate_limit_per_minute"`
	Status               string                    `gorm:"type:varchar(20);not null;default:'active'" json:"status"`
	CreatedByPrincipalID uint                      `gorm:"not null" json:"created_by_principal_id"`
	CreatedAt            time.Time                 `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time                 `gorm:"autoUpdateTime" json:"updated_at"`
	ServiceGrants        []APIConsumerServiceGrant `gorm:"foreignKey:APIConsumerID;constraint:OnDelete:CASCADE" json:"service_grants"`
	Credentials          []APIConsumerCredential   `gorm:"foreignKey:APIConsumerID;constraint:OnDelete:CASCADE" json:"credentials,omitempty"`
}

func (APIConsumer) TableName() string { return "system.api_consumers" }

type APIConsumerServiceGrant struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	APIConsumerID uint      `gorm:"not null;index" json:"api_consumer_id"`
	ServiceType   string    `gorm:"type:varchar(20);not null" json:"service_type"`
	ServiceID     uint      `gorm:"not null" json:"service_id"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (APIConsumerServiceGrant) TableName() string {
	return "system.api_consumer_service_grants"
}

type APIConsumerCredential struct {
	ID                   uint       `gorm:"primaryKey" json:"id"`
	APIConsumerID        uint       `gorm:"not null;index" json:"api_consumer_id"`
	KeyPrefix            string     `gorm:"type:varchar(20);not null" json:"key_prefix"`
	KeyHash              string     `gorm:"type:char(64);not null;uniqueIndex" json:"-"`
	Name                 string     `gorm:"type:varchar(120);not null;default:''" json:"name"`
	LastUsedAt           *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	Status               string     `gorm:"type:varchar(20);not null;default:'active';index" json:"status"`
	CreatedByPrincipalID uint       `gorm:"not null" json:"created_by_principal_id"`
	CreatedAt            time.Time  `gorm:"autoCreateTime" json:"created_at"`
	RevokedAt            *time.Time `json:"revoked_at,omitempty"`
	RevokedByPrincipalID *uint      `json:"revoked_by_principal_id,omitempty"`
	PlainTextCredential  string     `gorm:"-" json:"plain_text_credential,omitempty"`
}

func (APIConsumerCredential) TableName() string {
	return "system.api_consumer_credentials"
}

type APIConsumerServiceReference struct {
	ServiceType string `json:"service_type" binding:"required,oneof=query"`
	ServiceID   uint   `json:"service_id" binding:"required"`
}

type CreateAPIConsumerRequest struct {
	Name               string                        `json:"name" binding:"required,max=120"`
	Description        string                        `json:"description" binding:"max=500"`
	ServiceGrants      []APIConsumerServiceReference `json:"service_grants" binding:"required,min=1,dive"`
	RateLimitPerMinute int                           `json:"rate_limit_per_minute" binding:"omitempty,min=1,max=100000"`
}

type UpdateAPIConsumerRequest struct {
	Name               string                        `json:"name" binding:"omitempty,max=120"`
	Description        *string                       `json:"description" binding:"omitempty,max=500"`
	ServiceGrants      []APIConsumerServiceReference `json:"service_grants" binding:"omitempty,min=1,dive"`
	RateLimitPerMinute int                           `json:"rate_limit_per_minute" binding:"omitempty,min=1,max=100000"`
	Status             string                        `json:"status" binding:"omitempty,oneof=active suspended"`
}

type CreateAPIConsumerCredentialRequest struct {
	Name      string     `json:"name" binding:"omitempty,max=120"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type APIConsumerCredentialValidationResponse struct {
	Valid              bool                          `json:"valid"`
	APIConsumerID      uint                          `json:"api_consumer_id"`
	APIConsumerName    string                        `json:"api_consumer_name"`
	TenantID           uint                          `json:"tenant_id"`
	ServiceGrants      []APIConsumerServiceReference `json:"service_grants"`
	RateLimitPerMinute int                           `json:"rate_limit_per_minute"`
	ExpiresAt          *time.Time                    `json:"expires_at,omitempty"`
}
