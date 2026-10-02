package engineaccess

import (
	"context"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
)

// The source must come from the Catalog owner's immutable decision, not a
// browser body or operator identity. This IAM check establishes no responsibility
// relationship and cannot replace the future trusted owner-basis consumer.
// The authorization version is historical audit evidence, not a Permission
// snapshot. Keep the operator's strict version check separate.
type lockedBusinessConfirmer struct {
	identity    *lockedUserProvenance
	permissions []iam.RoleAssignmentPermissionProjection
}

func (r *Repository) loadBusinessConfirmer(ctx context.Context, source userProvenance, tenantID int64) (*lockedBusinessConfirmer, error) {
	confirmer, err := r.lockUserProvenance(ctx, tenantID, source)
	if err != nil {
		return nil, err
	}
	now, err := r.wallClock(ctx)
	if err != nil {
		return nil, err
	}
	if err := confirmer.checkIdentity(now); err != nil {
		return nil, err
	}
	rows, err := r.identity().ListEffectiveRoleAssignmentPermissions(ctx, confirmer.source.PrincipalID,
		iam.PrincipalTypeUser, iam.ContextTypeTenant, &confirmer.tenantID, &confirmer.source.MembershipID, now)
	if err != nil {
		return nil, err
	}
	return &lockedBusinessConfirmer{identity: confirmer, permissions: rows}, nil
}

// The earlier Principal SHARE lock stabilizes role writes. These rows remain
// transaction-local; their natural validity is rechecked after all waiting and
// business verification, using the same database clock as the other predicates.
func (c *lockedBusinessConfirmer) check(now time.Time) error {
	if c == nil {
		return commonapi.ErrForbidden
	}
	if err := c.identity.checkIdentity(now); err != nil {
		return err
	}
	if !hasBusinessConfirmationPermissions(c.permissions, c.identity.tenantID, now) {
		return commonapi.ErrForbidden
	}
	return nil
}

func hasBusinessConfirmationPermissions(rows []iam.RoleAssignmentPermissionProjection, tenantID int64, now time.Time) bool {
	return hasCurrentTenantPermission(rows, tenantID, "catalog.entry.read", now) &&
		hasCurrentTenantPermission(rows, tenantID, "catalog.sharing_decision.create", now)
}
