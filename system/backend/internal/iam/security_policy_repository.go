package iam

import (
	"context"

	commonapi "github.com/addp/common/api"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) GetSecurityPolicy(ctx context.Context) (*SecurityPolicy, error) {
	var policy SecurityPolicy
	if err := r.db.WithContext(ctx).First(&policy, SecurityPolicySingletonID).Error; err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &policy, nil
}

func (r *Repository) LockSecurityPolicy(ctx context.Context) (*SecurityPolicy, error) {
	var policy SecurityPolicy
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&policy, SecurityPolicySingletonID).Error
	if err != nil {
		return nil, wrapRepositoryError(err)
	}
	return &policy, nil
}

func (r *Repository) MarkSecurityPolicyApplied(ctx context.Context, version int64) error {
	result := r.db.WithContext(ctx).Model(&SecurityPolicy{}).
		Where("id = ? AND version = ?", SecurityPolicySingletonID, version).
		Update("applied_version", version)
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}

func (r *Repository) UpdateSecurityPolicy(ctx context.Context, policy SecurityPolicy, expectedVersion int64) error {
	result := r.db.WithContext(ctx).Model(&SecurityPolicy{}).
		Where("id = ? AND version = ?", SecurityPolicySingletonID, expectedVersion).
		Updates(map[string]any{
			"version":                              policy.Version,
			"access_token_ttl_minutes":             policy.AccessTokenTTLMinutes,
			"delegated_access_token_ttl_minutes":   policy.DelegatedAccessTokenTTLMinutes,
			"resource_access_ticket_ttl_minutes":   policy.ResourceAccessTicketTTLMinutes,
			"refresh_token_ttl_days":               policy.RefreshTokenTTLDays,
			"oauth_authorization_code_ttl_minutes": policy.OAuthAuthorizationCodeTTLMinutes,
			"oauth_device_code_ttl_minutes":        policy.OAuthDeviceCodeTTLMinutes,
			"oauth_device_poll_interval_seconds":   policy.OAuthDevicePollIntervalSeconds,
			"tenant_invitation_ttl_hours":          policy.TenantInvitationTTLHours,
			"oauth_public_rate_limit_per_minute":   policy.OAuthPublicRateLimitPerMinute,
			"oauth_user_rate_limit_per_minute":     policy.OAuthUserRateLimitPerMinute,
			"updated_by_principal_id":              policy.UpdatedByPrincipalID,
			"updated_at":                           gorm.Expr("CURRENT_TIMESTAMP"),
		})
	if result.Error != nil {
		return wrapRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonapi.ErrConflict
	}
	return nil
}
