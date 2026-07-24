package iam

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
)

type CreateTenantInput struct {
	Code        string
	Name        string
	Description string
	Audit       AuditMetadata
}

type EstablishTenantMembershipInput struct {
	TenantID             int64
	PrincipalID          int64
	SourceType           TenantMembershipSource
	SourceRef            *string
	ExpiresAt            *time.Time
	CreatedByPrincipalID *int64
	Audit                AuditMetadata
}

type ChangeTenantMembershipInput struct {
	TenantID    int64
	PrincipalID int64
	ExpiresAt   *time.Time
	Audit       AuditMetadata
}

type TenantMembershipChangeResult struct {
	Membership           TenantMembership
	AuthorizationVersion int64
	RevokedFamilyCount   int64
}

type TenantMembershipService struct {
	repository *Repository
	now        func() time.Time
}

func NewTenantMembershipService(repository *Repository, now func() time.Time) *TenantMembershipService {
	if now == nil {
		now = time.Now
	}
	return &TenantMembershipService{repository: repository, now: now}
}

func (s *TenantMembershipService) CreateTenant(
	ctx context.Context,
	input CreateTenantInput,
) (*Tenant, error) {
	if s == nil || s.repository == nil {
		return nil, fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("%w: tenant name is required", commonapi.ErrBadRequest)
	}
	tenant := &Tenant{
		Code:        input.Code,
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		Status:      TenantStatusActive,
	}
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		if err := tx.CreateTenant(ctx, tenant); err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata:   input.Audit,
			EventName:  "iam.tenant.created",
			Result:     AuditResultSucceeded,
			RiskLevel:  AuditRiskMedium,
			ModuleName: "system",
			EntityType: "tenant",
			EntityID:   strconv.FormatInt(tenant.ID, 10),
			Details:    map[string]any{"tenant_code": tenant.Code},
		})
	})
	if err != nil {
		return nil, err
	}
	return tenant, nil
}

func (s *TenantMembershipService) EstablishMembership(
	ctx context.Context,
	input EstablishTenantMembershipInput,
) (*TenantMembershipChangeResult, error) {
	if err := s.validateMembershipTarget(input.TenantID, input.PrincipalID); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if input.ExpiresAt != nil && !input.ExpiresAt.After(now) {
		return nil, fmt.Errorf("%w: membership expiry must be in the future", commonapi.ErrBadRequest)
	}

	var changed *TenantMembershipChangeResult
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, tenant, err := lockActiveMembershipTarget(ctx, tx, input.PrincipalID, input.TenantID)
		if err != nil {
			return err
		}
		membership := &TenantMembership{
			TenantID:             tenant.ID,
			PrincipalID:          principal.ID,
			Status:               TenantMembershipStatusActive,
			SourceType:           input.SourceType,
			SourceRef:            input.SourceRef,
			JoinedAt:             now,
			ExpiresAt:            input.ExpiresAt,
			CreatedByPrincipalID: input.CreatedByPrincipalID,
		}
		if err := tx.CreateTenantMembership(ctx, membership); err != nil {
			return err
		}
		return s.finishMembershipChange(
			ctx, tx, membership, input.Audit,
			"iam.tenant_membership.established", AuditRiskMedium, "membership_established", now, &changed,
		)
	})
	if err != nil {
		return nil, err
	}
	return changed, nil
}

func (s *TenantMembershipService) SuspendMembership(
	ctx context.Context,
	input ChangeTenantMembershipInput,
) (*TenantMembershipChangeResult, error) {
	return s.changeMembership(ctx, input, TenantMembershipStatusSuspended)
}

func (s *TenantMembershipService) EndMembership(
	ctx context.Context,
	input ChangeTenantMembershipInput,
) (*TenantMembershipChangeResult, error) {
	return s.changeMembership(ctx, input, TenantMembershipStatusEnded)
}

func (s *TenantMembershipService) RestoreMembership(
	ctx context.Context,
	input ChangeTenantMembershipInput,
) (*TenantMembershipChangeResult, error) {
	return s.changeMembership(ctx, input, TenantMembershipStatusActive)
}

func (s *TenantMembershipService) changeMembership(
	ctx context.Context,
	input ChangeTenantMembershipInput,
	targetStatus TenantMembershipStatus,
) (*TenantMembershipChangeResult, error) {
	if err := s.validateMembershipTarget(input.TenantID, input.PrincipalID); err != nil {
		return nil, err
	}
	now := s.now().UTC()
	if targetStatus == TenantMembershipStatusActive && input.ExpiresAt != nil && !input.ExpiresAt.After(now) {
		return nil, fmt.Errorf("%w: restored membership expiry must be in the future", commonapi.ErrBadRequest)
	}

	var changed *TenantMembershipChangeResult
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		principal, err := tx.LockPrincipal(ctx, input.PrincipalID)
		if err != nil {
			return err
		}
		tenant, err := tx.LockTenant(ctx, input.TenantID)
		if err != nil {
			return err
		}
		membership, err := tx.LockTenantMembership(ctx, tenant.ID, principal.ID)
		if err != nil {
			return err
		}

		endedAt := membership.EndedAt
		expiresAt := membership.ExpiresAt
		eventName := ""
		revocationReason := ""
		riskLevel := AuditRiskMedium
		switch targetStatus {
		case TenantMembershipStatusSuspended:
			if membership.Status != TenantMembershipStatusActive {
				return fmt.Errorf("%w: only an active membership can be suspended", commonapi.ErrConflict)
			}
			endedAt = nil
			eventName = "iam.tenant_membership.suspended"
			revocationReason = "membership_suspended"
			riskLevel = AuditRiskHigh
		case TenantMembershipStatusEnded:
			if membership.Status == TenantMembershipStatusEnded {
				return fmt.Errorf("%w: membership is already ended", commonapi.ErrConflict)
			}
			endedAt = &now
			eventName = "iam.tenant_membership.ended"
			revocationReason = "membership_ended"
			riskLevel = AuditRiskCritical
		case TenantMembershipStatusActive:
			if membership.Status == TenantMembershipStatusActive {
				return fmt.Errorf("%w: membership is already active", commonapi.ErrConflict)
			}
			if principal.Status != PrincipalStatusActive || tenant.Status != TenantStatusActive {
				return fmt.Errorf("%w: principal and tenant must be active to restore membership", commonapi.ErrConflict)
			}
			endedAt = nil
			expiresAt = input.ExpiresAt
			eventName = "iam.tenant_membership.restored"
			revocationReason = "membership_restored"
		default:
			return fmt.Errorf("%w: unsupported membership status", commonapi.ErrBadRequest)
		}

		if err := tx.UpdateTenantMembershipLifecycle(
			ctx, membership.ID, targetStatus, endedAt, expiresAt,
		); err != nil {
			return err
		}
		membership.Status = targetStatus
		membership.EndedAt = endedAt
		membership.ExpiresAt = expiresAt
		return s.finishMembershipChange(
			ctx, tx, membership, input.Audit,
			eventName, riskLevel, revocationReason, now, &changed,
		)
	})
	if err != nil {
		return nil, err
	}
	return changed, nil
}

func (s *TenantMembershipService) finishMembershipChange(
	ctx context.Context,
	tx *Repository,
	membership *TenantMembership,
	audit AuditMetadata,
	eventName string,
	riskLevel AuditRiskLevel,
	revocationReason string,
	changedAt time.Time,
	result **TenantMembershipChangeResult,
) error {
	principal, err := tx.GetPrincipal(ctx, membership.PrincipalID)
	if err != nil {
		return err
	}
	revokedFamilyCount, err := tx.RevokeActiveTokenFamilies(
		ctx, membership.PrincipalID, changedAt, revocationReason,
	)
	if err != nil {
		return err
	}
	if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
		Metadata:   audit,
		EventName:  eventName,
		Result:     AuditResultSucceeded,
		RiskLevel:  riskLevel,
		ModuleName: "system",
		EntityType: "tenant_membership",
		EntityID:   strconv.FormatInt(membership.ID, 10),
		Details: map[string]any{
			"tenant_id":             membership.TenantID,
			"principal_id":          membership.PrincipalID,
			"status":                membership.Status,
			"authorization_version": principal.AuthorizationVersion,
			"revoked_family_count":  revokedFamilyCount,
		},
	}); err != nil {
		return err
	}
	*result = &TenantMembershipChangeResult{
		Membership:           *membership,
		AuthorizationVersion: principal.AuthorizationVersion,
		RevokedFamilyCount:   revokedFamilyCount,
	}
	return nil
}

func (s *TenantMembershipService) validateMembershipTarget(tenantID int64, principalID int64) error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	if tenantID <= 0 || principalID <= 0 {
		return fmt.Errorf("%w: tenant and principal are required", commonapi.ErrBadRequest)
	}
	return nil
}

func lockActiveMembershipTarget(
	ctx context.Context,
	tx *Repository,
	principalID int64,
	tenantID int64,
) (*Principal, *Tenant, error) {
	principal, err := tx.LockPrincipal(ctx, principalID)
	if err != nil {
		return nil, nil, err
	}
	if principal.Status != PrincipalStatusActive {
		return nil, nil, fmt.Errorf("%w: principal must be active", commonapi.ErrConflict)
	}
	tenant, err := tx.LockTenant(ctx, tenantID)
	if err != nil {
		return nil, nil, err
	}
	if tenant.Status != TenantStatusActive {
		return nil, nil, fmt.Errorf("%w: tenant must be active", commonapi.ErrConflict)
	}
	return principal, tenant, nil
}
