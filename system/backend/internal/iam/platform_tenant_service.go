package iam

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
)

type UpdateTenantInput struct {
	TenantID    int64
	Name        string
	Description string
	Audit       AuditMetadata
}

type ChangeTenantStatusInput struct {
	TenantID int64
	Reason   string
	Audit    AuditMetadata
}

type TenantStatusChangeResult struct {
	Tenant             Tenant
	AffectedPrincipals int
	RevokedFamilyCount int64
}

type PlatformTenantService struct {
	repository *Repository
	now        func() time.Time
}

func NewPlatformTenantService(repository *Repository, now func() time.Time) *PlatformTenantService {
	if now == nil {
		now = time.Now
	}
	return &PlatformTenantService{repository: repository, now: now}
}

func (s *PlatformTenantService) List(
	ctx context.Context,
	page int,
	pageSize int,
	search string,
	status *TenantStatus,
) ([]Tenant, int64, error) {
	if err := s.validate(); err != nil {
		return nil, 0, err
	}
	if err := validateManagementPagination(page, pageSize); err != nil {
		return nil, 0, err
	}
	var tenants []Tenant
	var total int64
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		var err error
		tenants, total, err = tx.ListManagedTenants(ctx, page, pageSize, search, status)
		return err
	})
	return tenants, total, err
}

func (s *PlatformTenantService) Get(ctx context.Context, tenantID int64) (*Tenant, error) {
	if err := s.validateTenantID(tenantID); err != nil {
		return nil, err
	}
	return s.repository.GetTenant(ctx, tenantID)
}

func (s *PlatformTenantService) Create(ctx context.Context, input CreateTenantInput) (*Tenant, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	return NewTenantMembershipService(s.repository, s.now).CreateTenant(ctx, input)
}

func (s *PlatformTenantService) Update(ctx context.Context, input UpdateTenantInput) (*Tenant, error) {
	if err := s.validateTenantID(input.TenantID); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: tenant name is required", commonapi.ErrBadRequest)
	}
	description := strings.TrimSpace(input.Description)
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		tenant, err := tx.LockTenantForUpdate(ctx, input.TenantID)
		if err != nil {
			return err
		}
		if tenant.Status == TenantStatusClosed {
			return fmt.Errorf("%w: closed tenant cannot be updated", commonapi.ErrConflict)
		}
		if err := tx.UpdateTenantDetails(ctx, tenant.ID, name, description); err != nil {
			return err
		}
		return NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata:   input.Audit,
			EventName:  "iam.tenant.updated",
			Result:     AuditResultSucceeded,
			RiskLevel:  AuditRiskMedium,
			ModuleName: "system",
			EntityType: "tenant",
			EntityID:   strconv.FormatInt(tenant.ID, 10),
			Details:    map[string]any{},
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repository.GetTenant(ctx, input.TenantID)
}

func (s *PlatformTenantService) Suspend(
	ctx context.Context,
	input ChangeTenantStatusInput,
) (*TenantStatusChangeResult, error) {
	return s.changeStatus(ctx, input, TenantStatusSuspended)
}

func (s *PlatformTenantService) Restore(
	ctx context.Context,
	input ChangeTenantStatusInput,
) (*TenantStatusChangeResult, error) {
	return s.changeStatus(ctx, input, TenantStatusActive)
}

func (s *PlatformTenantService) Close(
	ctx context.Context,
	input ChangeTenantStatusInput,
) (*TenantStatusChangeResult, error) {
	return s.changeStatus(ctx, input, TenantStatusClosed)
}

func (s *PlatformTenantService) changeStatus(
	ctx context.Context,
	input ChangeTenantStatusInput,
	target TenantStatus,
) (*TenantStatusChangeResult, error) {
	if err := s.validateTenantID(input.TenantID); err != nil {
		return nil, err
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: reason is required", commonapi.ErrBadRequest)
	}
	now := s.now().UTC()
	var changed *TenantStatusChangeResult
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		tenant, err := tx.LockTenantForUpdate(ctx, input.TenantID)
		if err != nil {
			return err
		}
		if err := validateTenantStatusTransition(tenant.Status, target); err != nil {
			return err
		}
		principalIDs, err := tx.LockTenantPrincipalIDs(ctx, tenant.ID)
		if err != nil {
			return err
		}
		if err := tx.UpdateTenantStatus(ctx, tenant.ID, target); err != nil {
			return err
		}
		var revoked int64
		for _, principalID := range principalIDs {
			if _, err := tx.IncrementPrincipalAuthorizationVersion(ctx, principalID); err != nil {
				return err
			}
			count, err := tx.RevokeActiveTokenFamilies(ctx, principalID, now, "tenant_"+string(target))
			if err != nil {
				return err
			}
			revoked += count
		}
		eventName, risk := tenantStatusAudit(target)
		if err := NewAuditWriter(tx).Write(ctx, AuditEvent{
			Metadata:   input.Audit,
			EventName:  eventName,
			Result:     AuditResultSucceeded,
			RiskLevel:  risk,
			ModuleName: "system",
			EntityType: "tenant",
			EntityID:   strconv.FormatInt(tenant.ID, 10),
			Details: map[string]any{
				"reason":               reason,
				"status":               target,
				"affected_principals":  len(principalIDs),
				"revoked_family_count": revoked,
			},
		}); err != nil {
			return err
		}
		tenant.Status = target
		changed = &TenantStatusChangeResult{
			Tenant:             *tenant,
			AffectedPrincipals: len(principalIDs),
			RevokedFamilyCount: revoked,
		}
		return nil
	})
	return changed, err
}

func (s *PlatformTenantService) validate() error {
	if s == nil || s.repository == nil {
		return fmt.Errorf("%w: IAM repository is required", commonapi.ErrBadRequest)
	}
	return nil
}

func (s *PlatformTenantService) validateTenantID(tenantID int64) error {
	if err := s.validate(); err != nil {
		return err
	}
	if tenantID <= 0 {
		return fmt.Errorf("%w: tenant is required", commonapi.ErrBadRequest)
	}
	return nil
}

func validateTenantStatusTransition(current TenantStatus, target TenantStatus) error {
	valid := (current == TenantStatusActive && target == TenantStatusSuspended) ||
		(current == TenantStatusSuspended && target == TenantStatusActive) ||
		((current == TenantStatusActive || current == TenantStatusSuspended) && target == TenantStatusClosed)
	if !valid {
		return fmt.Errorf("%w: invalid tenant status transition from %s to %s", commonapi.ErrConflict, current, target)
	}
	return nil
}

func tenantStatusAudit(status TenantStatus) (string, AuditRiskLevel) {
	switch status {
	case TenantStatusSuspended:
		return "iam.tenant.suspended", AuditRiskHigh
	case TenantStatusActive:
		return "iam.tenant.restored", AuditRiskHigh
	default:
		return "iam.tenant.closed", AuditRiskCritical
	}
}

func validateManagementPagination(page int, pageSize int) error {
	if page < 1 || pageSize < 1 || pageSize > 10000 {
		return fmt.Errorf("%w: invalid pagination", commonapi.ErrBadRequest)
	}
	return nil
}
