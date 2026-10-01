package iam

import (
	"context"

	commonapi "github.com/addp/common/api"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LockUserAuthorizationSource stabilizes existing IAM facts for a read-only
// qualification check in the caller's transaction. SHARE blocks lifecycle and
// authorization-version writes but permits audit foreign-key KEY SHARE locks
// and independent readers. It does not establish provenance or grant access.
// Acquire this source before engine/request/target locks, never after them.
func (r *Repository) LockUserAuthorizationSource(ctx context.Context, principalID, membershipID, tenantID int64) (*Principal, *TenantMembership, *Tenant, error) {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); !ok || principalID <= 0 || membershipID <= 0 || tenantID <= 0 {
		return nil, nil, nil, commonapi.ErrBadRequest
	}
	lock := func() *gorm.DB {
		return r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"})
	}
	var principal Principal
	if err := lock().First(&principal, principalID).Error; err != nil {
		return nil, nil, nil, wrapRepositoryError(err)
	}
	var member TenantMembership
	if err := lock().First(&member, membershipID).Error; err != nil {
		return nil, nil, nil, wrapRepositoryError(err)
	}
	var tenant Tenant
	if err := lock().First(&tenant, tenantID).Error; err != nil {
		return nil, nil, nil, wrapRepositoryError(err)
	}
	return &principal, &member, &tenant, nil
}
