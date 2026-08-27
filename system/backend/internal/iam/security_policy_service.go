package iam

import (
	"context"
	"fmt"
	"strconv"

	commonapi "github.com/addp/common/api"
)

type SecurityPolicyService struct{ repository *Repository }

func NewSecurityPolicyService(repository *Repository) *SecurityPolicyService {
	return &SecurityPolicyService{repository: repository}
}

func (s *SecurityPolicyService) LoadAndMarkApplied(ctx context.Context) (*SecurityPolicy, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: IAM security policy repository is required", commonapi.ErrBadRequest)
	}
	var applied *SecurityPolicy
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		policy, err := tx.LockSecurityPolicy(ctx)
		if err != nil {
			return err
		}
		if err := ValidateSecurityPolicy(*policy); err != nil {
			return fmt.Errorf("stored IAM security policy is invalid: %w", err)
		}
		if policy.AppliedVersion != policy.Version {
			if err := tx.MarkSecurityPolicyApplied(ctx, policy.Version); err != nil {
				return err
			}
			policy.AppliedVersion = policy.Version
		}
		applied = policy
		return nil
	})
	return applied, err
}

func (s *SecurityPolicyService) Get(ctx context.Context) (*SecurityPolicy, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: IAM security policy repository is required", commonapi.ErrBadRequest)
	}
	policy, err := s.repository.GetSecurityPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if err := ValidateSecurityPolicy(*policy); err != nil {
		return nil, fmt.Errorf("stored IAM security policy is invalid: %w", err)
	}
	return policy, nil
}

func (s *SecurityPolicyService) Update(ctx context.Context, input UpdateSecurityPolicyInput) (*SecurityPolicy, error) {
	if s == nil || s.repository == nil || input.ExpectedVersion <= 0 || input.UpdatedByPrincipalID <= 0 {
		return nil, fmt.Errorf("%w: IAM security policy version and actor are required", commonapi.ErrBadRequest)
	}
	candidate := input.policy()
	if err := ValidateSecurityPolicy(candidate); err != nil {
		return nil, err
	}
	var updated *SecurityPolicy
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		current, err := tx.LockSecurityPolicy(ctx)
		if err != nil {
			return err
		}
		if current.Version != input.ExpectedVersion {
			return commonapi.ErrConflict
		}
		candidate.Version = current.Version + 1
		candidate.AppliedVersion = current.AppliedVersion
		candidate.UpdatedByPrincipalID = &input.UpdatedByPrincipalID
		if err := tx.UpdateSecurityPolicy(ctx, candidate, current.Version); err != nil {
			return err
		}
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata: input.Audit, EventName: "iam.security_policy.updated",
			Result: AuditResultSucceeded, RiskLevel: AuditRiskMedium, ModuleName: "system",
			EntityType: "iam_security_policy", EntityID: strconv.Itoa(int(SecurityPolicySingletonID)),
			Details: map[string]any{
				"previous_version": current.Version,
				"version":          candidate.Version,
				"applied_version":  current.AppliedVersion,
				"pending_restart":  true,
				"changes":          securityPolicyChanges(*current, candidate),
			},
		}); err != nil {
			return err
		}
		updated, err = tx.GetSecurityPolicy(ctx)
		return err
	})
	return updated, err
}

func ValidateSecurityPolicy(policy SecurityPolicy) error {
	checks := []struct {
		name       string
		value, min int
		max        int
	}{
		{"access_token_ttl_minutes", policy.AccessTokenTTLMinutes, 1, 60},
		{"delegated_access_token_ttl_minutes", policy.DelegatedAccessTokenTTLMinutes, 1, 2},
		{"resource_access_ticket_ttl_minutes", policy.ResourceAccessTicketTTLMinutes, 1, 60},
		{"refresh_token_ttl_days", policy.RefreshTokenTTLDays, 1, 365},
		{"oauth_authorization_code_ttl_minutes", policy.OAuthAuthorizationCodeTTLMinutes, 1, 5},
		{"oauth_device_code_ttl_minutes", policy.OAuthDeviceCodeTTLMinutes, 5, 30},
		{"oauth_device_poll_interval_seconds", policy.OAuthDevicePollIntervalSeconds, 5, 60},
		{"tenant_invitation_ttl_hours", policy.TenantInvitationTTLHours, 1, 720},
		{"oauth_public_rate_limit_per_minute", policy.OAuthPublicRateLimitPerMinute, 1, 10000},
		{"oauth_user_rate_limit_per_minute", policy.OAuthUserRateLimitPerMinute, 1, 10000},
	}
	for _, check := range checks {
		if check.value < check.min || check.value > check.max {
			return fmt.Errorf("%w: %s must be between %d and %d", commonapi.ErrBadRequest, check.name, check.min, check.max)
		}
	}
	if policy.ResourceAccessTicketTTLMinutes > policy.AccessTokenTTLMinutes {
		return fmt.Errorf("%w: resource_access_ticket_ttl_minutes must not exceed access_token_ttl_minutes", commonapi.ErrBadRequest)
	}
	return nil
}

func securityPolicyChanges(before, after SecurityPolicy) map[string]any {
	changes := make(map[string]any)
	add := func(key string, oldValue, newValue int) {
		if oldValue != newValue {
			changes[key] = map[string]int{"from": oldValue, "to": newValue}
		}
	}
	add("access_token_ttl_minutes", before.AccessTokenTTLMinutes, after.AccessTokenTTLMinutes)
	add("delegated_access_token_ttl_minutes", before.DelegatedAccessTokenTTLMinutes, after.DelegatedAccessTokenTTLMinutes)
	add("resource_access_ticket_ttl_minutes", before.ResourceAccessTicketTTLMinutes, after.ResourceAccessTicketTTLMinutes)
	add("refresh_token_ttl_days", before.RefreshTokenTTLDays, after.RefreshTokenTTLDays)
	add("oauth_authorization_code_ttl_minutes", before.OAuthAuthorizationCodeTTLMinutes, after.OAuthAuthorizationCodeTTLMinutes)
	add("oauth_device_code_ttl_minutes", before.OAuthDeviceCodeTTLMinutes, after.OAuthDeviceCodeTTLMinutes)
	add("oauth_device_poll_interval_seconds", before.OAuthDevicePollIntervalSeconds, after.OAuthDevicePollIntervalSeconds)
	add("tenant_invitation_ttl_hours", before.TenantInvitationTTLHours, after.TenantInvitationTTLHours)
	add("oauth_public_rate_limit_per_minute", before.OAuthPublicRateLimitPerMinute, after.OAuthPublicRateLimitPerMinute)
	add("oauth_user_rate_limit_per_minute", before.OAuthUserRateLimitPerMinute, after.OAuthUserRateLimitPerMinute)
	return changes
}
