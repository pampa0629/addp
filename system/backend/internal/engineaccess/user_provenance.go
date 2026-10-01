package engineaccess

import (
	"context"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
)

// userProvenance is an immutable reference to the human operation's source,
// NOT proof that a supplied body came from that user. The production consumer
// must obtain it from the owner's persisted, authenticated operation. No token,
// role/permission snapshot or Catalog responsibility is copied here.
type userProvenance struct {
	PrincipalID          int64 `json:"principal_id,string"`
	MembershipID         int64 `json:"tenant_membership_id,string"`
	AuthorizationVersion int64 `json:"authorization_version,string"`
}

func (p userProvenance) valid() bool {
	return p.PrincipalID > 0 && p.MembershipID > 0 && p.AuthorizationVersion > 0
}

type lockedUserProvenance struct {
	source    userProvenance
	tenantID  int64
	principal *iam.Principal
	member    *iam.TenantMembership
	tenant    *iam.Tenant
}

// Only the owner's transaction may use these locks. They precede engine,
// request and exact-target locks and remain held through outcome commit.
func (r *Repository) lockUserProvenance(ctx context.Context, tenantID int64, source userProvenance) (*lockedUserProvenance, error) {
	principal, member, tenant, err := r.identity().LockUserAuthorizationSource(ctx, source.PrincipalID, source.MembershipID, tenantID)
	if err != nil {
		return nil, err
	}
	return &lockedUserProvenance{source: source, tenantID: tenantID, principal: principal, member: member, tenant: tenant}, nil
}

// IAM locks keep lifecycle/version facts stable, not the wall clock. Reuse the
// same identity predicate in synchronous delegation management and acceptance.
// This predicate deliberately grants no Permission or engine delegation.
func (p *lockedUserProvenance) check(now time.Time) error {
	if p == nil || !p.source.valid() || p.principal == nil || p.member == nil || p.tenant == nil ||
		p.principal.ID != p.source.PrincipalID || p.principal.PrincipalType != iam.PrincipalTypeUser ||
		p.principal.Status != iam.PrincipalStatusActive || p.principal.AuthorizationVersion != p.source.AuthorizationVersion ||
		p.member.ID != p.source.MembershipID || p.member.TenantID != p.tenantID || p.member.PrincipalID != p.source.PrincipalID ||
		p.member.Status != iam.TenantMembershipStatusActive || (p.member.ExpiresAt != nil && !p.member.ExpiresAt.After(now)) ||
		p.tenant.ID != p.tenantID || p.tenant.Status != iam.TenantStatusActive {
		return commonapi.ErrForbidden
	}
	return nil
}
