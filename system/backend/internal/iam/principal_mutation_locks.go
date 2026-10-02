package iam

import (
	"context"
	"sort"

	commonapi "github.com/addp/common/api"
)

// Account locks must precede lower authorization facts, including writes made
// by SQL triggers. Discovery queries identify lock targets, not authority.
func (r *Repository) lockPrincipalsInOrder(ctx context.Context, ids ...int64) (map[int64]*Principal, error) {
	ordered := normalizedPrincipalIDs(ids)
	locked := make(map[int64]*Principal, len(ordered))
	for _, id := range ordered {
		if id <= 0 {
			return nil, commonapi.ErrBadRequest
		}
		principal, err := r.LockPrincipal(ctx, id)
		if err != nil {
			return nil, err
		}
		locked[id] = principal
	}
	return locked, nil
}

func normalizedPrincipalIDs(ids []int64) []int64 {
	ordered := append([]int64(nil), ids...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	unique := ordered[:0]
	for _, id := range ordered {
		if len(unique) == 0 || unique[len(unique)-1] != id {
			unique = append(unique, id)
		}
	}
	return unique
}

func verifyMutationPrincipalSet(expected, current []int64, err error) error {
	if err != nil {
		return err
	}
	expected, current = normalizedPrincipalIDs(expected), normalizedPrincipalIDs(current)
	if len(expected) != len(current) {
		return commonapi.ErrConflict
	}
	for i := range expected {
		if expected[i] != current[i] {
			return commonapi.ErrConflict
		}
	}
	return nil
}

func mutationActorIDs(audit AuditMetadata, ids ...int64) []int64 {
	if audit.PrincipalID != nil {
		ids = append(ids, *audit.PrincipalID)
	}
	return ids
}

func (r *Repository) lockMembershipPrincipal(ctx context.Context, tenantID, membershipID int64, actorIDs ...int64) (*TenantMembership, error) {
	source, err := r.GetTenantMembershipByID(ctx, membershipID)
	if err != nil {
		return nil, err
	}
	if source.TenantID != tenantID {
		return nil, commonapi.ErrNotFound
	}
	if _, err := r.lockPrincipalsInOrder(ctx, append(actorIDs, source.PrincipalID)...); err != nil {
		return nil, err
	}
	current, err := r.LockTenantMembershipByID(ctx, membershipID)
	if err != nil {
		return nil, err
	}
	if current.TenantID != source.TenantID || current.PrincipalID != source.PrincipalID {
		return nil, commonapi.ErrConflict
	}
	return current, nil
}

func (r *Repository) listOrganizationPrincipalIDs(ctx context.Context, tenantID, organizationID int64, department bool) ([]int64, error) {
	table, column := "system.project_group_memberships", "project_group_id"
	if department {
		table, column = "system.department_memberships", "department_id"
	}
	var ids []int64
	err := r.db.WithContext(ctx).Table(table+" organization_membership").
		Joins("JOIN system.tenant_memberships membership ON membership.id = organization_membership.tenant_membership_id AND membership.tenant_id = organization_membership.tenant_id").
		Where("organization_membership.tenant_id = ? AND organization_membership."+column+" = ? AND organization_membership.status = ?", tenantID, organizationID, OrganizationMembershipStatusActive).
		Distinct("membership.principal_id").Order("membership.principal_id").Pluck("membership.principal_id", &ids).Error
	return ids, wrapRepositoryError(err)
}

func (r *Repository) lockOrganizationMembershipPrincipals(ctx context.Context, tenantID, organizationID, membershipID, actorID int64, department bool) (*ManagedOrganizationMembership, error) {
	var source *ManagedOrganizationMembership
	var err error
	if department {
		source, err = r.GetDepartmentMembership(ctx, tenantID, organizationID, membershipID)
	} else {
		source, err = r.GetProjectGroupMembership(ctx, tenantID, organizationID, membershipID)
	}
	if err != nil {
		return nil, err
	}
	if _, err := r.lockPrincipalsInOrder(ctx, source.PrincipalID, actorID); err != nil {
		return nil, err
	}
	return source, nil
}

func (r *Repository) lockRoleMutationPrincipals(ctx context.Context, tenantID, roleID, actorID int64) (*Role, []int64, error) {
	// Check tenant ownership before discovering holders of a role.
	if _, err := r.GetTenantRole(ctx, tenantID, roleID); err != nil {
		return nil, nil, err
	}
	holders, err := r.ListActiveRoleHolderPrincipalIDs(ctx, roleID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := r.lockPrincipalsInOrder(ctx, append([]int64{actorID}, holders...)...); err != nil {
		return nil, nil, err
	}
	if _, err := r.LockTenantForUpdate(ctx, tenantID); err != nil {
		return nil, nil, err
	}
	role, err := r.LockTenantRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, nil, err
	}
	current, err := r.ListActiveRoleHolderPrincipalIDs(ctx, roleID)
	if err := verifyMutationPrincipalSet(holders, current, err); err != nil {
		return nil, nil, err
	}
	return role, holders, nil
}
