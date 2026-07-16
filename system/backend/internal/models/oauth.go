package models

import (
	"time"

	"github.com/lib/pq"
)

const (
	OAuthClientTypePublic = "public"

	OAuthDeviceStatusPending  = "pending"
	OAuthDeviceStatusApproved = "approved"
	OAuthDeviceStatusDenied   = "denied"
	OAuthDeviceStatusUsed     = "used"
)

type OAuthClient struct {
	ClientID          string         `gorm:"primaryKey;type:varchar(100)" json:"client_id"`
	Name              string         `gorm:"type:varchar(255);not null" json:"name"`
	ClientType        string         `gorm:"type:varchar(20);not null" json:"client_type"`
	RedirectURIs      pq.StringArray `gorm:"type:text[];not null" json:"redirect_uris"`
	AllowedScopes     pq.StringArray `gorm:"type:text[];not null" json:"allowed_scopes"`
	DeviceFlowEnabled bool           `gorm:"not null;default:false" json:"device_flow_enabled"`
	IsActive          bool           `gorm:"not null;default:true" json:"is_active"`
	CreatedAt         time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt         time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
}

func (OAuthClient) TableName() string { return "system.oauth_clients" }

type OAuthAuthorizationCode struct {
	ID                  string         `gorm:"primaryKey;type:uuid" json:"id"`
	CodeHash            string         `gorm:"type:char(64);not null;uniqueIndex" json:"-"`
	ClientID            string         `gorm:"type:varchar(100);not null;index" json:"client_id"`
	UserID              uint           `gorm:"not null;index" json:"user_id"`
	TenantID            *uint          `gorm:"index" json:"tenant_id"`
	RedirectURI         string         `gorm:"type:text;not null" json:"redirect_uri"`
	Scopes              pq.StringArray `gorm:"type:text[];not null" json:"scopes"`
	CodeChallenge       string         `gorm:"type:varchar(128);not null" json:"-"`
	CodeChallengeMethod string         `gorm:"type:varchar(10);not null" json:"-"`
	ExpiresAt           time.Time      `gorm:"not null;index" json:"expires_at"`
	UsedAt              *time.Time     `json:"used_at"`
	CreatedAt           time.Time      `gorm:"autoCreateTime" json:"created_at"`
}

func (OAuthAuthorizationCode) TableName() string { return "system.oauth_authorization_codes" }

type OAuthDeviceAuthorization struct {
	ID             string         `gorm:"primaryKey;type:uuid" json:"id"`
	DeviceCodeHash string         `gorm:"type:char(64);not null;uniqueIndex" json:"-"`
	UserCodeHash   string         `gorm:"type:char(64);not null;uniqueIndex" json:"-"`
	ClientID       string         `gorm:"type:varchar(100);not null;index" json:"client_id"`
	UserID         *uint          `gorm:"index" json:"user_id"`
	TenantID       *uint          `gorm:"index" json:"tenant_id"`
	Scopes         pq.StringArray `gorm:"type:text[];not null" json:"scopes"`
	Status         string         `gorm:"type:varchar(20);not null;index" json:"status"`
	IntervalSecs   int            `gorm:"not null" json:"interval"`
	LastPolledAt   *time.Time     `json:"last_polled_at"`
	ExpiresAt      time.Time      `gorm:"not null;index" json:"expires_at"`
	CreatedAt      time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
}

func (OAuthDeviceAuthorization) TableName() string { return "system.oauth_device_authorizations" }

type RefreshTokenFamily struct {
	ID            string         `gorm:"primaryKey;type:uuid" json:"id"`
	UserID        uint           `gorm:"not null;index" json:"user_id"`
	TenantID      *uint          `gorm:"index" json:"tenant_id"`
	ClientID      *string        `gorm:"type:varchar(100);index" json:"client_id"`
	AuthType      string         `gorm:"type:varchar(50);not null" json:"auth_type"`
	Audiences     pq.StringArray `gorm:"type:text[];not null" json:"audiences"`
	Scopes        pq.StringArray `gorm:"type:text[];not null" json:"scopes"`
	ExpiresAt     time.Time      `gorm:"not null;index" json:"expires_at"`
	RevokedAt     *time.Time     `gorm:"index" json:"revoked_at"`
	RevokedReason *string        `gorm:"type:varchar(100)" json:"revoked_reason"`
	CreatedAt     time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
}

func (RefreshTokenFamily) TableName() string { return "system.refresh_token_families" }

type RefreshToken struct {
	ID                string     `gorm:"primaryKey;type:uuid" json:"id"`
	FamilyID          string     `gorm:"type:uuid;not null;index" json:"family_id"`
	TokenHash         string     `gorm:"type:char(64);not null;uniqueIndex" json:"-"`
	ParentTokenID     *string    `gorm:"type:uuid" json:"parent_token_id"`
	ReplacedByTokenID *string    `gorm:"type:uuid" json:"replaced_by_token_id"`
	ExpiresAt         time.Time  `gorm:"not null;index" json:"expires_at"`
	UsedAt            *time.Time `json:"used_at"`
	RevokedAt         *time.Time `gorm:"index" json:"revoked_at"`
	CreatedAt         time.Time  `gorm:"autoCreateTime" json:"created_at"`
}

func (RefreshToken) TableName() string { return "system.refresh_tokens" }

type AccessToken struct {
	ID        string         `gorm:"primaryKey;type:uuid" json:"id"`
	TokenHash string         `gorm:"type:char(64);not null;uniqueIndex" json:"-"`
	FamilyID  string         `gorm:"type:uuid;not null;index" json:"family_id"`
	UserID    uint           `gorm:"not null;index" json:"user_id"`
	TenantID  *uint          `gorm:"index" json:"tenant_id"`
	ClientID  *string        `gorm:"type:varchar(100);index" json:"client_id"`
	AuthType  string         `gorm:"type:varchar(50);not null" json:"auth_type"`
	Audiences pq.StringArray `gorm:"type:text[];not null" json:"audiences"`
	Scopes    pq.StringArray `gorm:"type:text[];not null" json:"scopes"`
	ExpiresAt time.Time      `gorm:"not null;index" json:"expires_at"`
	RevokedAt *time.Time     `gorm:"index" json:"revoked_at"`
	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
}

func (AccessToken) TableName() string { return "system.access_tokens" }

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

type OAuthAuthorizationRequest struct {
	ClientID            string `json:"client_id" binding:"required"`
	RedirectURI         string `json:"redirect_uri" binding:"required"`
	Scope               string `json:"scope" binding:"required"`
	State               string `json:"state" binding:"required"`
	CodeChallenge       string `json:"code_challenge" binding:"required"`
	CodeChallengeMethod string `json:"code_challenge_method" binding:"required"`
}

type OAuthAuthorizationResponse struct {
	RedirectURL string `json:"redirect_url"`
}

type DeviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type DeviceApprovalRequest struct {
	UserCode string `json:"user_code" binding:"required"`
	Approve  bool   `json:"approve"`
}
