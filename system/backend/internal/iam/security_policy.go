package iam

import "time"

const SecurityPolicySingletonID int16 = 1

type SecurityPolicy struct {
	ID                               int16     `gorm:"column:id;primaryKey" json:"-"`
	Version                          int64     `gorm:"column:version;not null" json:"version"`
	AppliedVersion                   int64     `gorm:"column:applied_version;not null" json:"applied_version"`
	AccessTokenTTLMinutes            int       `gorm:"column:access_token_ttl_minutes;not null" json:"access_token_ttl_minutes"`
	DelegatedAccessTokenTTLMinutes   int       `gorm:"column:delegated_access_token_ttl_minutes;not null" json:"delegated_access_token_ttl_minutes"`
	ResourceAccessTicketTTLMinutes   int       `gorm:"column:resource_access_ticket_ttl_minutes;not null" json:"resource_access_ticket_ttl_minutes"`
	RefreshTokenTTLDays              int       `gorm:"column:refresh_token_ttl_days;not null" json:"refresh_token_ttl_days"`
	OAuthAuthorizationCodeTTLMinutes int       `gorm:"column:oauth_authorization_code_ttl_minutes;not null" json:"oauth_authorization_code_ttl_minutes"`
	OAuthDeviceCodeTTLMinutes        int       `gorm:"column:oauth_device_code_ttl_minutes;not null" json:"oauth_device_code_ttl_minutes"`
	OAuthDevicePollIntervalSeconds   int       `gorm:"column:oauth_device_poll_interval_seconds;not null" json:"oauth_device_poll_interval_seconds"`
	TenantInvitationTTLHours         int       `gorm:"column:tenant_invitation_ttl_hours;not null" json:"tenant_invitation_ttl_hours"`
	OAuthPublicRateLimitPerMinute    int       `gorm:"column:oauth_public_rate_limit_per_minute;not null" json:"oauth_public_rate_limit_per_minute"`
	OAuthUserRateLimitPerMinute      int       `gorm:"column:oauth_user_rate_limit_per_minute;not null" json:"oauth_user_rate_limit_per_minute"`
	UpdatedByPrincipalID             *int64    `gorm:"column:updated_by_principal_id" json:"updated_by_principal_id,omitempty"`
	CreatedAt                        time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt                        time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (SecurityPolicy) TableName() string { return "system.iam_security_policy" }

func DefaultSecurityPolicy() SecurityPolicy {
	return SecurityPolicy{
		ID: SecurityPolicySingletonID, Version: 1, AppliedVersion: 1,
		AccessTokenTTLMinutes: 15, DelegatedAccessTokenTTLMinutes: 2,
		ResourceAccessTicketTTLMinutes: 15, RefreshTokenTTLDays: 30,
		OAuthAuthorizationCodeTTLMinutes: 5, OAuthDeviceCodeTTLMinutes: 10,
		OAuthDevicePollIntervalSeconds: 5, TenantInvitationTTLHours: 168,
		OAuthPublicRateLimitPerMinute: 60, OAuthUserRateLimitPerMinute: 30,
	}
}

type UpdateSecurityPolicyInput struct {
	ExpectedVersion                  int64
	AccessTokenTTLMinutes            int
	DelegatedAccessTokenTTLMinutes   int
	ResourceAccessTicketTTLMinutes   int
	RefreshTokenTTLDays              int
	OAuthAuthorizationCodeTTLMinutes int
	OAuthDeviceCodeTTLMinutes        int
	OAuthDevicePollIntervalSeconds   int
	TenantInvitationTTLHours         int
	OAuthPublicRateLimitPerMinute    int
	OAuthUserRateLimitPerMinute      int
	UpdatedByPrincipalID             int64
	Audit                            AuditMetadata
}

func (input UpdateSecurityPolicyInput) policy() SecurityPolicy {
	return SecurityPolicy{
		ID:                               SecurityPolicySingletonID,
		AccessTokenTTLMinutes:            input.AccessTokenTTLMinutes,
		DelegatedAccessTokenTTLMinutes:   input.DelegatedAccessTokenTTLMinutes,
		ResourceAccessTicketTTLMinutes:   input.ResourceAccessTicketTTLMinutes,
		RefreshTokenTTLDays:              input.RefreshTokenTTLDays,
		OAuthAuthorizationCodeTTLMinutes: input.OAuthAuthorizationCodeTTLMinutes,
		OAuthDeviceCodeTTLMinutes:        input.OAuthDeviceCodeTTLMinutes,
		OAuthDevicePollIntervalSeconds:   input.OAuthDevicePollIntervalSeconds,
		TenantInvitationTTLHours:         input.TenantInvitationTTLHours,
		OAuthPublicRateLimitPerMinute:    input.OAuthPublicRateLimitPerMinute,
		OAuthUserRateLimitPerMinute:      input.OAuthUserRateLimitPerMinute,
	}
}
