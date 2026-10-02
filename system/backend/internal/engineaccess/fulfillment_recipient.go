package engineaccess

import (
	"context"
	"errors"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
)

// Current recipient facts remain transaction-local. No versions, members or
// permissions are copied into the immutable request or its outcome.
type lockedFulfillmentRecipient struct {
	tenantID, id int64
	kind         string
	principal    *iam.Principal
	member       *iam.TenantMembership
	department   *iam.Department
	group        *iam.ProjectGroup
}

func (r *Repository) lockFulfillmentRecipient(ctx context.Context, request fulfillmentRequest,
	principals map[int64]*iam.Principal,
) (*lockedFulfillmentRecipient, error) {
	value := &lockedFulfillmentRecipient{tenantID: request.TenantID, id: request.RecipientID, kind: request.RecipientType}
	var err error
	switch value.kind {
	case "user":
		value.principal = principals[value.id]
		value.member, err = r.identity().LockTenantRecipientMembership(ctx, value.tenantID, value.id)
	case "department":
		value.department, err = r.identity().LockDepartmentAuthorizationRecipient(ctx, value.tenantID, value.id)
	case "project_group":
		value.group, err = r.identity().LockProjectGroupAuthorizationRecipient(ctx, value.tenantID, value.id)
	default:
		return nil, errFulfillmentBinding
	}
	if errors.Is(err, commonapi.ErrNotFound) {
		return nil, commonapi.ErrForbidden
	}
	return value, err
}

func (v *lockedFulfillmentRecipient) check(now time.Time) error {
	if v == nil || v.tenantID <= 0 || v.id <= 0 {
		return commonapi.ErrForbidden
	}
	switch v.kind {
	case "user":
		if v.principal != nil && v.principal.ID == v.id && v.principal.PrincipalType == iam.PrincipalTypeUser &&
			v.principal.Status == iam.PrincipalStatusActive && v.member != nil && v.member.TenantID == v.tenantID &&
			v.member.PrincipalID == v.id && v.member.Status == iam.TenantMembershipStatusActive &&
			(v.member.ExpiresAt == nil || v.member.ExpiresAt.After(now)) {
			return nil
		}
	case "department":
		if v.department != nil && v.department.ID == v.id && v.department.TenantID == v.tenantID && v.department.Status == iam.DepartmentStatusActive {
			return nil
		}
	case "project_group":
		if v.group != nil && v.group.ID == v.id && v.group.TenantID == v.tenantID && v.group.Status == iam.ProjectGroupStatusActive {
			return nil
		}
	}
	return commonapi.ErrForbidden
}
