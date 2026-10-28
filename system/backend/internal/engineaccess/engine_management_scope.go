package engineaccess

import (
	"context"
	"errors"
	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"time"
)

func (s *Service) withEngineManagementScope(ctx context.Context, actor Actor, engineID int64, permission string,
	requireLiveCatalog bool, operation func(*Repository, func() error) error) error {
	return s.withEngineManagementRecipientScope(ctx, actor, engineID, permission, requireLiveCatalog, nil,
		func(tx *Repository, check func() error, _ *lockedFulfillmentRecipient) error {
			return operation(tx, check)
		})
}

// The complete Principal set precedes every Membership/Tenant/engine lock.
// Recipient eligibility is checked only for a new write, not historical replay.
func (s *Service) withEngineManagementRecipientScope(ctx context.Context, actor Actor, engineID int64, permission string,
	requireLiveCatalog bool, recipient *fulfillmentRequest, operation func(*Repository, func() error, *lockedFulfillmentRecipient) error) error {
	if !validActor(actor) || engineID <= 0 {
		return commonapi.ErrBadRequest
	}
	return s.repository.transaction(ctx, func(tx *Repository) error {
		ids := []int64{actor.PrincipalID}
		if recipient != nil && recipient.RecipientType == "user" {
			ids = append(ids, recipient.RecipientID)
		}
		principals, err := tx.identity().LockUserAuthorizationPrincipals(ctx, ids...)
		if err != nil {
			if errors.Is(err, commonapi.ErrNotFound) {
				return commonapi.ErrForbidden
			}
			return err
		}
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
		var lockedRecipient *lockedFulfillmentRecipient
		if recipient != nil {
			lockedRecipient, err = tx.lockFulfillmentRecipient(ctx, *recipient, principals)
			if err != nil {
				return err
			}
		}
		// Cross-Tenant engines are hidden, even from a qualified administrator.
		if _, err := tx.engine(ctx, actor.TenantID, engineID, false); err != nil {
			return err
		}
		scope, err := tx.lockManagementScope(ctx, actor.TenantID, engineID, actor.MembershipID)
		if err != nil {
			return err
		}
		if requireLiveCatalog && (s.catalogEligible == nil || !s.catalogEligible(scope.engine)) {
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
			if requireLiveCatalog {
				return scope.check(now)
			}
			return scope.checkManagement(now)
		}
		if err := check(); err != nil {
			return err
		}
		return operation(tx, check, lockedRecipient)
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
