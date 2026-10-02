package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	commonapi "github.com/addp/common/api"
	engineplugin "github.com/addp/common/engine/plugin"
	authorization "github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
)

var ErrApprovalRequirementExists = errors.Join(commonapi.ErrConflict, errors.New("approval requirement already initialized"))

// ApprovalRequirementView is System's configuration fact, not a Grant or a
// copy of Catalog responsibility. IAM/engine IDs retain decimal precision.
type ApprovalRequirementView struct {
	ID          uuid.UUID       `json:"id"`
	EngineID    int64           `json:"engine_id,string" swaggertype:"string"`
	CatalogPath json.RawMessage `json:"catalog_path" swaggertype:"object"`
	Mode        string          `json:"mode"`
	Version     int64           `json:"version"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type InitializeApprovalRequirementInput struct {
	Actor       Actor
	EngineID    int64
	CatalogPath engineplugin.EngineCatalogPath
	Reason      string
	Audit       iam.AuditMetadata
}

func approvalRequirementView(row *approvalRequirement) *ApprovalRequirementView {
	return &ApprovalRequirementView{ID: row.ID, EngineID: row.EngineID, CatalogPath: row.CatalogPath,
		Mode: row.Mode, Version: row.Version, UpdatedAt: row.UpdatedAt}
}

// All public operations use authenticated human provenance and current local
// IAM facts. No HTTP call, copied permission snapshot or Catalog readiness is
// part of this qualification.
func (s *Service) withApprovalRequirementScope(ctx context.Context, actor Actor, engineID int64, permission string,
	operation func(*Repository, func() error) error) error {
	if !validActor(actor) || engineID <= 0 {
		return commonapi.ErrBadRequest
	}
	return s.repository.transaction(ctx, func(tx *Repository) error {
		identity, err := tx.lockUserProvenance(ctx, actor.TenantID, userProvenance{
			PrincipalID: actor.PrincipalID, MembershipID: actor.MembershipID, AuthorizationVersion: actor.AuthorizationVersion})
		if err != nil {
			return err
		}
		now, err := tx.wallClock(ctx)
		if err != nil {
			return err
		}
		if err := identity.check(now); err != nil {
			return err
		}
		rows, err := tx.identity().ListEffectiveRoleAssignmentPermissions(ctx, actor.PrincipalID,
			iam.PrincipalTypeUser, iam.ContextTypeTenant, &actor.TenantID, &actor.MembershipID, now)
		if err != nil {
			return err
		}
		if !hasCurrentTenantPermission(rows, actor.TenantID, permission, now) {
			return commonapi.ErrForbidden
		}
		// Cross-Tenant engines are hidden, even from a qualified administrator.
		if _, err := tx.engine(ctx, actor.TenantID, engineID, false); err != nil {
			return err
		}
		scope, err := tx.lockManagementScope(ctx, actor.TenantID, engineID, actor.MembershipID)
		if err != nil {
			return err
		}
		if s.catalogEligible == nil || !s.catalogEligible(scope.engine) {
			return commonapi.ErrForbidden
		}
		check := func() error {
			now, err := tx.wallClock(ctx)
			if err != nil {
				return err
			}
			if err := identity.check(now); err != nil {
				return err
			}
			if !actor.TokenExpiresAt.After(now) {
				return commonapi.ErrUnauthorized
			}
			if !hasCurrentTenantPermission(rows, actor.TenantID, permission, now) {
				return commonapi.ErrForbidden
			}
			return scope.check(now)
		}
		if err := check(); err != nil {
			return err
		}
		return operation(tx, check)
	})
}

func hasCurrentTenantPermission(rows []iam.RoleAssignmentPermissionProjection, tenantID int64, permission string, now time.Time) bool {
	for _, row := range rows {
		if row.PermissionKey == permission && row.ScopeType == "tenant" && row.TenantID != nil && *row.TenantID == tenantID &&
			!row.ValidFrom.After(now) && (row.ValidUntil == nil || row.ValidUntil.After(now)) {
			return true
		}
	}
	return false
}

func (s *Service) InitializeApprovalRequirement(ctx context.Context, input InitializeApprovalRequirementInput) (*ApprovalRequirementView, error) {
	if input.EngineID <= 0 || int64(input.CatalogPath.EngineID) != input.EngineID || !validReason(input.Reason) {
		return nil, commonapi.ErrBadRequest
	}
	if _, err := encodeFulfillmentPath(input.CatalogPath); err != nil {
		return nil, commonapi.ErrBadRequest
	}
	var result *ApprovalRequirementView
	err := s.withApprovalRequirementScope(ctx, input.Actor, input.EngineID, authorization.PermissionSystemEngineAccessApprovalRequirementInitialize,
		func(tx *Repository, check func() error) error {
			audit := input.Audit
			actor := input.Actor
			principalType, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
			audit.PrincipalID, audit.PrincipalType = &actor.PrincipalID, &principalType
			audit.TenantID, audit.ContextType = &actor.TenantID, &contextType
			row, err := tx.changeApprovalRequirement(ctx, approvalRequirementChange{TenantID: actor.TenantID,
				Path: input.CatalogPath, Mode: approvalModeCatalog, ExpectedVersion: 0, Reason: input.Reason, Audit: audit},
				func(*Repository) error { return check() })
			if errors.Is(err, errApprovalRequirementVersion) {
				return ErrApprovalRequirementExists
			}
			if err != nil {
				return err
			}
			result = approvalRequirementView(row)
			return nil
		})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ListApprovalRequirements(ctx context.Context, actor Actor, engineID int64, page, size int) ([]ApprovalRequirementView, int64, error) {
	if page <= 0 || size <= 0 || size > 100 {
		return nil, 0, commonapi.ErrBadRequest
	}
	rows := make([]ApprovalRequirementView, 0)
	var total int64
	err := s.withApprovalRequirementScope(ctx, actor, engineID, authorization.PermissionSystemEngineAccessApprovalRequirementRead,
		func(tx *Repository, check func() error) error {
			facts, count, err := tx.listApprovalRequirements(ctx, actor.TenantID, engineID, page, size)
			if err != nil {
				return err
			}
			total = count
			for i := range facts {
				rows = append(rows, *approvalRequirementView(&facts[i]))
			}
			return check()
		})
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (s *Service) GetApprovalRequirement(ctx context.Context, actor Actor, engineID int64, id uuid.UUID) (*ApprovalRequirementView, error) {
	if id == uuid.Nil {
		return nil, commonapi.ErrBadRequest
	}
	var result *ApprovalRequirementView
	err := s.withApprovalRequirementScope(ctx, actor, engineID, authorization.PermissionSystemEngineAccessApprovalRequirementRead,
		func(tx *Repository, check func() error) error {
			row, err := tx.getApprovalRequirement(ctx, actor.TenantID, engineID, id)
			if err != nil {
				return err
			}
			if err := check(); err != nil {
				return err
			}
			result = approvalRequirementView(row)
			return nil
		})
	if err != nil {
		return nil, err
	}
	return result, nil
}
