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

type CreateTenantInput struct {
	Code                            string
	Name                            string
	Description                     string
	InitialAdministratorPrincipalID int64
	ActorPrincipalID                int64
	Audit                           AuditMetadata
}

type ChangeTenantStatusInput struct {
	TenantID int64
	Reason   string
	Audit    AuditMetadata
}

type TenantStatusChangeResult struct {
	Tenant             ManagedTenant
	AffectedPrincipals int
	RevokedFamilyCount int64
}

type InitializeTenantInput struct {
	TenantID                        int64
	InitialAdministratorPrincipalID int64
	ActorPrincipalID                int64
	Audit                           AuditMetadata
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
) ([]ManagedTenant, int64, error) {
	if err := s.validate(); err != nil {
		return nil, 0, err
	}
	if err := validateManagementPagination(page, pageSize); err != nil {
		return nil, 0, err
	}
	var tenants []ManagedTenant
	var total int64
	err := s.repository.ReadOnlyRepeatableReadTransaction(ctx, func(tx *Repository) error {
		var err error
		tenants, total, err = tx.ListManagedTenantViews(ctx, page, pageSize, search, status)
		return err
	})
	return tenants, total, err
}

func (s *PlatformTenantService) Get(ctx context.Context, tenantID int64) (*ManagedTenant, error) {
	if err := s.validateTenantID(tenantID); err != nil {
		return nil, err
	}
	return s.repository.GetManagedTenantView(ctx, tenantID)
}

func (s *PlatformTenantService) Create(ctx context.Context, input CreateTenantInput) (*ManagedTenant, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) == "" || input.InitialAdministratorPrincipalID <= 0 || input.ActorPrincipalID <= 0 {
		return nil, fmt.Errorf("%w: tenant name, initial administrator and actor are required", commonapi.ErrBadRequest)
	}
	tenant := &Tenant{Code: input.Code, Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), Status: TenantStatusActive}
	var now time.Time
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		var err error
		now, err = tx.CurrentDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if err := lockTenantInitializationPrincipals(ctx, tx, input.ActorPrincipalID, input.InitialAdministratorPrincipalID); err != nil {
			return err
		}
		if err := validateInitialTenantAdministrator(ctx, tx, input.InitialAdministratorPrincipalID, now); err != nil {
			return err
		}
		if err := tx.CreateTenant(ctx, tenant); err != nil {
			return err
		}
		if err := s.initializeTenantTx(ctx, tx, tenant, input.InitialAdministratorPrincipalID, input.ActorPrincipalID, now, input.Audit, false); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.repository.GetManagedTenantView(ctx, tenant.ID)
}

func (s *PlatformTenantService) Initialize(ctx context.Context, input InitializeTenantInput) (*ManagedTenant, error) {
	if err := s.validateTenantID(input.TenantID); err != nil {
		return nil, err
	}
	if input.InitialAdministratorPrincipalID <= 0 || input.ActorPrincipalID <= 0 {
		return nil, fmt.Errorf("%w: initial administrator and actor are required", commonapi.ErrBadRequest)
	}
	var now time.Time
	err := s.repository.Transaction(ctx, func(tx *Repository) error {
		var err error
		now, err = tx.CurrentDatabaseTime(ctx)
		if err != nil {
			return err
		}
		if err := lockTenantInitializationPrincipals(ctx, tx, input.ActorPrincipalID, input.InitialAdministratorPrincipalID); err != nil {
			return err
		}
		if err := validateInitialTenantAdministrator(ctx, tx, input.InitialAdministratorPrincipalID, now); err != nil {
			return err
		}
		tenant, err := tx.LockTenantForUpdate(ctx, input.TenantID)
		if err != nil {
			return err
		}
		if tenant.Status == TenantStatusClosed {
			return fmt.Errorf("%w: closed tenant cannot be initialized", commonapi.ErrConflict)
		}
		if tenant.InitializedAt != nil || tenant.InitializedByPrincipalID != nil {
			return fmt.Errorf("%w: tenant is already initialized", commonapi.ErrConflict)
		}
		hasFacts, err := tx.TenantHasMembershipOrAssignment(ctx, tenant.ID)
		if err != nil {
			return err
		}
		if hasFacts {
			return fmt.Errorf("%w: tenant has already entered membership or authorization lifecycle", commonapi.ErrConflict)
		}
		return s.initializeTenantTx(ctx, tx, tenant, input.InitialAdministratorPrincipalID, input.ActorPrincipalID, now, input.Audit, true)
	})
	if err != nil {
		return nil, err
	}
	return s.repository.GetManagedTenantView(ctx, input.TenantID)
}

func (s *PlatformTenantService) ListAdministratorCandidates(ctx context.Context, search string) ([]TenantAdministratorCandidate, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s.repository.ListTenantAdministratorCandidates(ctx, search, 50)
}

func (s *PlatformTenantService) initializeTenantTx(ctx context.Context, tx *Repository, tenant *Tenant, administratorID, actorID int64, now time.Time, audit AuditMetadata, existing bool) error {
	membership := &TenantMembership{
		TenantID: tenant.ID, PrincipalID: administratorID, Status: TenantMembershipStatusActive,
		SourceType: TenantMembershipSourceManual, JoinedAt: now, CreatedByPrincipalID: &actorID,
	}
	if err := tx.CreateTenantMembership(ctx, membership); err != nil {
		return err
	}
	role, err := tx.GetActiveBuiltinRoleByKey(ctx, tenantAdministratorRoleKey)
	if err != nil {
		return err
	}
	tenantID := tenant.ID
	assignment := &RoleAssignment{
		PrincipalID: administratorID, RoleID: role.ID, ScopeType: "tenant", TenantID: &tenantID,
		Status: "active", ValidFrom: now, SourceType: "manual", CreatedByPrincipalID: &actorID,
		Reason: "initial tenant administrator",
	}
	if err := tx.CreateTenantRoleAssignment(ctx, assignment); err != nil {
		return err
	}
	serviceBindings, err := tx.ListBuiltinServiceRuntimeBindings(ctx)
	if err != nil {
		return err
	}
	if len(serviceBindings) != len(builtinServiceClientIDs) {
		return fmt.Errorf("%w: built-in service runtime bindings are incomplete", commonapi.ErrConflict)
	}
	for _, binding := range serviceBindings {
		serviceMembership := &TenantMembership{
			TenantID: tenant.ID, PrincipalID: binding.PrincipalID,
			Status: TenantMembershipStatusActive, SourceType: TenantMembershipSourceBootstrap,
			JoinedAt: now, CreatedByPrincipalID: &actorID,
		}
		if err := tx.CreateTenantMembership(ctx, serviceMembership); err != nil {
			return err
		}
		serviceAssignment := &RoleAssignment{
			PrincipalID: binding.PrincipalID, RoleID: binding.RoleID,
			ScopeType: "tenant", TenantID: &tenantID, Status: "active", ValidFrom: now,
			SourceType: "bootstrap", Reason: "built-in service runtime",
		}
		if err := tx.CreateTenantRoleAssignment(ctx, serviceAssignment); err != nil {
			return err
		}
	}
	if err := tx.MarkTenantInitialized(ctx, tenant.ID, actorID, now); err != nil {
		return err
	}
	tenant.InitializedAt = &now
	tenant.InitializedByPrincipalID = &actorID
	principal, err := tx.GetPrincipal(ctx, administratorID)
	if err != nil {
		return err
	}
	writer := NewAuditWriter(tx)
	if !existing {
		if err := writer.Write(ctx, AuditEvent{Metadata: audit, EventName: "iam.tenant.created", Result: AuditResultSucceeded, RiskLevel: AuditRiskMedium, ModuleName: "system", EntityType: "tenant", EntityID: strconv.FormatInt(tenant.ID, 10), Details: map[string]any{"tenant_code": tenant.Code, "initial_administrator_principal_id": administratorID, "service_runtime_count": len(serviceBindings)}}); err != nil {
			return err
		}
	}
	if err := writer.Write(ctx, AuditEvent{Metadata: audit, EventName: "iam.tenant_membership.established", Result: AuditResultSucceeded, RiskLevel: AuditRiskMedium, ModuleName: "system", EntityType: "tenant_membership", EntityID: strconv.FormatInt(membership.ID, 10), Details: map[string]any{"tenant_id": tenant.ID, "principal_id": administratorID, "source_type": TenantMembershipSourceManual}}); err != nil {
		return err
	}
	if err := writer.Write(ctx, AuditEvent{Metadata: audit, EventName: "iam.tenant_role_assignment.created", Result: AuditResultSucceeded, RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "role_assignment", EntityID: strconv.FormatInt(assignment.ID, 10), Details: map[string]any{"tenant_id": tenant.ID, "principal_id": administratorID, "role_key": tenantAdministratorRoleKey, "authorization_version": principal.AuthorizationVersion, "authorization_version_changed": true}}); err != nil {
		return err
	}
	if existing {
		if err := writer.Write(ctx, AuditEvent{Metadata: audit, EventName: "iam.tenant.initialized", Result: AuditResultSucceeded, RiskLevel: AuditRiskHigh, ModuleName: "system", EntityType: "tenant", EntityID: strconv.FormatInt(tenant.ID, 10), Details: map[string]any{"initial_administrator_principal_id": administratorID, "membership_id": membership.ID, "role_assignment_id": assignment.ID, "service_runtime_count": len(serviceBindings)}}); err != nil {
			return err
		}
	}
	return nil
}

func lockTenantInitializationPrincipals(ctx context.Context, repository *Repository, actorID, targetID int64) error {
	ids := []int64{actorID, targetID}
	if ids[0] > ids[1] {
		ids[0], ids[1] = ids[1], ids[0]
	}
	for index, id := range ids {
		if index > 0 && ids[index-1] == id {
			continue
		}
		principal, err := repository.LockPrincipal(ctx, id)
		if err != nil {
			return err
		}
		if principal.Status != PrincipalStatusActive || principal.PrincipalType != PrincipalTypeUser {
			return commonapi.ErrForbidden
		}
	}
	return nil
}

func validateInitialTenantAdministrator(ctx context.Context, repository *Repository, principalID int64, now time.Time) error {
	hasPlatformRole, err := repository.HasEffectivePlatformRole(ctx, principalID, now)
	if err != nil {
		return err
	}
	if hasPlatformRole {
		return fmt.Errorf("%w: platform administrator cannot be tenant initial administrator", commonapi.ErrForbidden)
	}
	return nil
}

func (s *PlatformTenantService) Update(ctx context.Context, input UpdateTenantInput) (*ManagedTenant, error) {
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
	return s.repository.GetManagedTenantView(ctx, input.TenantID)
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
			Tenant:             ManagedTenant{Tenant: *tenant},
			AffectedPrincipals: len(principalIDs),
			RevokedFamilyCount: revoked,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	view, err := s.repository.GetManagedTenantView(ctx, input.TenantID)
	if err != nil {
		return nil, err
	}
	changed.Tenant = *view
	return changed, nil
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
