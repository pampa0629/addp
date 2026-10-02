package iam

import (
	"context"

	commonapi "github.com/addp/common/api"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LockUserAuthorizationPrincipals locks the entire account set before any
// lower IAM facts. Readers use the same deterministic order as IAM writers.
func (r *Repository) LockUserAuthorizationPrincipals(ctx context.Context, ids ...int64) (map[int64]*Principal, error) {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); !ok || len(ids) == 0 {
		return nil, commonapi.ErrBadRequest
	}
	principals := make(map[int64]*Principal, len(ids))
	for _, id := range normalizedPrincipalIDs(ids) {
		if id <= 0 {
			return nil, commonapi.ErrBadRequest
		}
		var principal Principal
		if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).First(&principal, id).Error; err != nil {
			return nil, wrapRepositoryError(err)
		}
		principals[id] = &principal
	}
	return principals, nil
}

// LockTenantRecipientMembership follows the previously locked account set.
// This identifies a recipient, not an operator provenance/version snapshot.
func (r *Repository) LockTenantRecipientMembership(ctx context.Context, tenantID, principalID int64) (*TenantMembership, error) {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); !ok || tenantID <= 0 || principalID <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	var member TenantMembership
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).
		Where("tenant_id = ? AND principal_id = ?", tenantID, principalID).Take(&member).Error
	return &member, wrapRepositoryError(err)
}

// Organization qualification is read-only; it neither grants access nor
// requires members. Caller must acquire account/member/tenant locks first.
func (r *Repository) LockDepartmentAuthorizationRecipient(ctx context.Context, tenantID, id int64) (*Department, error) {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); !ok || tenantID <= 0 || id <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	var row Department
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).
		Where("tenant_id = ? AND id = ?", tenantID, id).Take(&row).Error
	return &row, wrapRepositoryError(err)
}

func (r *Repository) LockProjectGroupAuthorizationRecipient(ctx context.Context, tenantID, id int64) (*ProjectGroup, error) {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); !ok || tenantID <= 0 || id <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	var row ProjectGroup
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).
		Where("tenant_id = ? AND id = ?", tenantID, id).Take(&row).Error
	return &row, wrapRepositoryError(err)
}

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
	principals, err := r.LockUserAuthorizationPrincipals(ctx, principalID)
	if err != nil {
		return nil, nil, nil, err
	}
	var member TenantMembership
	if err := lock().First(&member, membershipID).Error; err != nil {
		return nil, nil, nil, wrapRepositoryError(err)
	}
	var tenant Tenant
	if err := lock().First(&tenant, tenantID).Error; err != nil {
		return nil, nil, nil, wrapRepositoryError(err)
	}
	return principals[principalID], &member, &tenant, nil
}
