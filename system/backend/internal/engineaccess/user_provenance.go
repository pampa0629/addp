package engineaccess

import (
	"context"
	"errors"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	if err := p.checkIdentity(now); err != nil {
		return err
	}
	if p.principal.AuthorizationVersion != p.source.AuthorizationVersion {
		return commonapi.ErrForbidden
	}
	return nil
}

// Identity continuity is independent of a historical authorization version.
// Operators use check above; business confirmers additionally require current
// confirmation Permissions, not equality with their audit-time version.
func (p *lockedUserProvenance) checkIdentity(now time.Time) error {
	if p == nil || !p.source.valid() || p.principal == nil || p.member == nil || p.tenant == nil ||
		p.principal.ID != p.source.PrincipalID || p.principal.PrincipalType != iam.PrincipalTypeUser ||
		p.principal.Status != iam.PrincipalStatusActive ||
		p.member.ID != p.source.MembershipID || p.member.TenantID != p.tenantID || p.member.PrincipalID != p.source.PrincipalID ||
		p.member.Status != iam.TenantMembershipStatusActive || (p.member.ExpiresAt != nil && !p.member.ExpiresAt.After(now)) ||
		p.tenant.ID != p.tenantID || p.tenant.Status != iam.TenantStatusActive {
		return commonapi.ErrForbidden
	}
	return nil
}

// lockedManagementScope proves only current engine administration scope, not
// a content Grant, independent fulfillment Permission or business approval.
type lockedManagementScope struct {
	tenantID, engineID, membershipID int64
	engine                           *models.Engine
	delegation                       *Delegation
}

// IAM must already be locked. SHARE permits concurrent checks for independent
// targets, but protects non-key lifecycle/revocation writes through commit.
func (r *Repository) lockManagementScope(ctx context.Context, tenantID, engineID, membershipID int64) (*lockedManagementScope, error) {
	if _, ok := r.db.Statement.ConnPool.(gorm.TxCommitter); !ok {
		return nil, errFulfillmentBinding
	}
	scope := &lockedManagementScope{tenantID: tenantID, engineID: engineID, membershipID: membershipID,
		engine: &models.Engine{}, delegation: &Delegation{}}
	err := r.db.WithContext(ctx).Table("system.engines").
		Where("tenant_id = ? AND id = ?", tenantID, engineID).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(scope.engine).Error
	if err == nil {
		err = r.db.WithContext(ctx).
			Where("tenant_id = ? AND engine_id = ? AND tenant_membership_id = ? AND status = 'active' AND expires_at > clock_timestamp()",
				tenantID, engineID, membershipID).
			Clauses(clause.Locking{Strength: "SHARE"}).Take(scope.delegation).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, commonapi.ErrForbidden
	}
	if err != nil {
		return nil, err
	}
	return scope, nil
}

func (s *lockedManagementScope) check(now time.Time) error {
	if s == nil || s.tenantID <= 0 || s.engineID <= 0 || s.membershipID <= 0 ||
		s.engine == nil || s.engine.TenantID == nil || int64(*s.engine.TenantID) != s.tenantID ||
		int64(s.engine.ID) != s.engineID || s.engine.LifecycleState != models.EngineLifecycleActive ||
		s.delegation == nil || s.delegation.ID <= 0 || s.delegation.TenantID != s.tenantID ||
		s.delegation.EngineID != s.engineID || s.delegation.TenantMembershipID != s.membershipID ||
		s.delegation.Status != "active" || s.delegation.GrantedAt.After(now) || !s.delegation.ExpiresAt.After(now) {
		return commonapi.ErrForbidden
	}
	return nil
}
