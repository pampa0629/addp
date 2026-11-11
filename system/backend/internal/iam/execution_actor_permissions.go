package iam

import (
	"context"

	"gorm.io/gorm"
)

// CheckCurrentExecutionActorPermissions rechecks the live user and required tenant permissions.
func CheckCurrentExecutionActorPermissions(ctx context.Context, db *gorm.DB, tenant, principalID, membershipID, version int64, required []string) error {
	repo := NewRepository(db)
	principal, membership, now, err := lockAndValidateExecutionActor(ctx, repo, principalID, membershipID, tenant, version)
	if err != nil {
		return err
	}
	rows, err := repo.ListEffectiveRoleAssignmentPermissions(ctx, principal.ID, principal.PrincipalType, ContextTypeTenant, &tenant, &membership.ID, now)
	if err != nil {
		return err
	}
	permissions := make(map[string]bool)
	for _, row := range rows {
		if row.ScopeType == "tenant" {
			permissions[row.PermissionKey] = true
		}
	}
	for _, permission := range required {
		if !permissions[permission] {
			return ErrExecutionAuthorizationPermissionDenied
		}
	}
	return nil
}
