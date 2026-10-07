package engineaccess

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	commonapi "github.com/addp/common/api"
	authorization "github.com/addp/system/internal/authorization"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
)

var (
	ErrVersionConflict = errors.Join(commonapi.ErrConflict, errors.New("engine delegation version conflict"))
	ErrOverlap         = errors.Join(commonapi.ErrConflict, errors.New("overlapping engine delegation"))
	ErrUnavailable     = errors.Join(commonapi.ErrConflict, errors.New("engine delegation target unavailable"))
	ErrExpired         = errors.Join(commonapi.ErrConflict, errors.New("engine delegation history is read only"))
	ErrExpiry          = errors.Join(commonapi.ErrBadRequest, errors.New("explicit future expiry within membership validity required"))
)

type Actor struct {
	TenantID, PrincipalID, MembershipID, AuthorizationVersion int64
	TokenExpiresAt                                            time.Time
}
type CreateInput struct {
	Actor                        Actor
	EngineID, TenantMembershipID int64
	ExpiresAt                    time.Time
	Reason                       string
	Audit                        iam.AuditMetadata
}
type RevokeInput struct {
	Actor                 Actor
	EngineID, ID, Version int64
	Reason                string
	Audit                 iam.AuditMetadata
}
type Service struct {
	repository        *Repository
	catalogEligible   func(*models.Engine) bool
	fulfillmentBasis  FulfillmentBasisReader
	independentTarget IndependentTargetVerifier
}

func NewService(repository *Repository, catalogEligible func(*models.Engine) bool) *Service {
	return &Service{repository: repository, catalogEligible: catalogEligible}
}
func (s *Service) List(ctx context.Context, tenantID, engineID int64, page, size int) ([]View, int64, error) {
	if tenantID <= 0 || engineID <= 0 || page <= 0 || size <= 0 || size > 100 {
		return nil, 0, commonapi.ErrBadRequest
	}
	if _, err := s.repository.engine(ctx, tenantID, engineID, false); err != nil {
		return nil, 0, err
	}
	return s.repository.list(ctx, tenantID, engineID, page, size)
}
func (s *Service) Get(ctx context.Context, tenantID, engineID, id int64) (*View, error) {
	if tenantID <= 0 || engineID <= 0 || id <= 0 {
		return nil, commonapi.ErrBadRequest
	}
	return s.repository.view(ctx, tenantID, engineID, id)
}
func (s *Service) Create(ctx context.Context, input CreateInput) (*View, error) {
	if !validActor(input.Actor) || input.EngineID <= 0 || input.TenantMembershipID <= 0 || !validReason(input.Reason) {
		return nil, commonapi.ErrBadRequest
	}
	if input.ExpiresAt.IsZero() {
		return nil, ErrExpiry
	}
	var created *View
	err := s.repository.transaction(ctx, func(tx *Repository) error {
		member, now, err := lockManagementActors(ctx, tx, input.Actor, input.TenantMembershipID, authorization.PermissionSystemEngineAccessDelegationCreate)
		if err != nil {
			return err
		}
		if member.Status != iam.TenantMembershipStatusActive || (member.ExpiresAt != nil && !member.ExpiresAt.After(now)) {
			return ErrUnavailable
		}
		if !input.ExpiresAt.After(now) || input.ExpiresAt.Year() > 9999 || (member.ExpiresAt != nil && input.ExpiresAt.After(*member.ExpiresAt)) {
			return ErrExpiry
		}
		engine, err := tx.engine(ctx, input.Actor.TenantID, input.EngineID, true)
		if err != nil {
			return err
		}
		if engine.LifecycleState != models.EngineLifecycleActive || s.catalogEligible == nil || !s.catalogEligible(engine) {
			return ErrUnavailable
		}
		// Re-read the wall clock after potentially waiting for the engine lock.
		member, now, err = lockManagementActors(ctx, tx, input.Actor, input.TenantMembershipID, authorization.PermissionSystemEngineAccessDelegationCreate)
		if err != nil {
			return err
		}
		if !input.ExpiresAt.After(now) || (member.ExpiresAt != nil && !member.ExpiresAt.After(now)) {
			return ErrExpiry
		}
		d := &Delegation{TenantID: input.Actor.TenantID, EngineID: input.EngineID, TenantMembershipID: member.ID,
			Status: "active", Version: 1, GrantedByPrincipalID: input.Actor.PrincipalID,
			GrantedAt: now, ExpiresAt: input.ExpiresAt.UTC(), GrantReason: strings.TrimSpace(input.Reason)}
		if err := tx.insert(ctx, d); err != nil {
			return err
		}
		if err := managementAudit(ctx, tx, input.Actor, input.Audit, d, member.PrincipalID, now, "system.engine_access_delegation.created", d.GrantReason); err != nil {
			return err
		}
		created, err = tx.view(ctx, d.TenantID, d.EngineID, d.ID)
		return err
	})
	return created, err
}
func (s *Service) Revoke(ctx context.Context, input RevokeInput) (*View, error) {
	if !validActor(input.Actor) || input.EngineID <= 0 || input.ID <= 0 || input.Version <= 0 || !validReason(input.Reason) {
		return nil, commonapi.ErrBadRequest
	}
	var updated *View
	err := s.repository.transaction(ctx, func(tx *Repository) error {
		current, err := tx.get(ctx, input.Actor.TenantID, input.EngineID, input.ID, false)
		if err != nil {
			return err
		}
		member, _, err := lockManagementActors(ctx, tx, input.Actor, current.TenantMembershipID, authorization.PermissionSystemEngineAccessDelegationRevoke)
		if err != nil {
			return err
		}
		if _, err := tx.engine(ctx, current.TenantID, current.EngineID, true); err != nil {
			return err
		}
		current, err = tx.get(ctx, current.TenantID, current.EngineID, current.ID, true)
		if err != nil {
			return err
		}
		member, now, err := lockManagementActors(ctx, tx, input.Actor, current.TenantMembershipID, authorization.PermissionSystemEngineAccessDelegationRevoke)
		if err != nil {
			return err
		}
		if current.Version != input.Version {
			return ErrVersionConflict
		}
		if current.Status != "active" || !current.ExpiresAt.After(now) {
			return ErrExpired
		}
		if !input.Actor.TokenExpiresAt.After(now) {
			return commonapi.ErrUnauthorized
		}
		reason := strings.TrimSpace(input.Reason)
		if err := tx.revoke(ctx, current, input.Version, input.Actor.PrincipalID, reason, now); err != nil {
			return err
		}
		if err := managementAudit(ctx, tx, input.Actor, input.Audit, current, member.PrincipalID, now, "system.engine_access_delegation.revoked", reason); err != nil {
			return err
		}
		updated, err = tx.view(ctx, current.TenantID, current.EngineID, current.ID)
		return err
	})
	return updated, err
}

func validActor(a Actor) bool {
	return a.TenantID > 0 && a.PrincipalID > 0 && a.MembershipID > 0 && a.AuthorizationVersion > 0 && !a.TokenExpiresAt.IsZero()
}
func validReason(reason string) bool {
	return strings.TrimSpace(reason) != "" && utf8.RuneCountInString(reason) <= 2000
}

// Serialize delegation mutations by locking principals and memberships in
// stable order, then hold the tenant lifecycle boundary for current validation.
func lockManagementActors(ctx context.Context, tx *Repository, a Actor, targetMembershipID int64, permission string) (*iam.TenantMembership, time.Time, error) {
	identity := tx.identity()
	target, err := tx.membership(ctx, a.TenantID, targetMembershipID)
	if err != nil {
		return nil, time.Time{}, err
	}
	ids := []int64{a.PrincipalID}
	if target.PrincipalID != a.PrincipalID {
		ids = append(ids, target.PrincipalID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	principals := make(map[int64]*iam.Principal)
	for _, id := range ids {
		principals[id], err = identity.LockPrincipal(ctx, id)
		if err != nil {
			return nil, time.Time{}, err
		}
	}
	memberIDs := []int64{a.MembershipID}
	if target.ID != a.MembershipID {
		memberIDs = append(memberIDs, target.ID)
	}
	sort.Slice(memberIDs, func(i, j int) bool { return memberIDs[i] < memberIDs[j] })
	members := make(map[int64]*iam.TenantMembership)
	for _, id := range memberIDs {
		members[id], err = identity.LockTenantMembershipByID(ctx, id)
		if err != nil {
			return nil, time.Time{}, err
		}
	}
	tenant, err := identity.LockTenant(ctx, a.TenantID)
	if err != nil {
		return nil, time.Time{}, err
	}
	now, err := tx.wallClock(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	actor, actorMember := principals[a.PrincipalID], members[a.MembershipID]
	provenance := lockedUserProvenance{source: userProvenance{PrincipalID: a.PrincipalID, MembershipID: a.MembershipID,
		AuthorizationVersion: a.AuthorizationVersion}, tenantID: a.TenantID, principal: actor, member: actorMember, tenant: tenant}
	if err := provenance.check(now); err != nil {
		return nil, time.Time{}, err
	}
	if !a.TokenExpiresAt.After(now) {
		return nil, time.Time{}, commonapi.ErrForbidden
	}
	rows, err := identity.ListEffectiveRoleAssignmentPermissions(ctx, a.PrincipalID, iam.PrincipalTypeUser, iam.ContextTypeTenant, &a.TenantID, &a.MembershipID, now)
	if err != nil {
		return nil, time.Time{}, err
	}
	allowed := false
	for _, row := range rows {
		if row.ScopeType == "tenant" && row.PermissionKey == permission {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, time.Time{}, commonapi.ErrForbidden
	}
	target = members[targetMembershipID]
	if target.TenantID != a.TenantID {
		return nil, time.Time{}, commonapi.ErrNotFound
	}
	// Revocation remains possible when the target account or membership became
	// unavailable; creation additionally requires the active User boundary.
	if permission == authorization.PermissionSystemEngineAccessDelegationCreate && (principals[target.PrincipalID].PrincipalType != iam.PrincipalTypeUser || principals[target.PrincipalID].Status != iam.PrincipalStatusActive) {
		return nil, time.Time{}, ErrUnavailable
	}
	return target, now, nil
}
func managementAudit(ctx context.Context, tx *Repository, actor Actor, metadata iam.AuditMetadata, d *Delegation, recipient int64, now time.Time, event, reason string) error {
	identity := tx.identity()
	if _, err := identity.IncrementPrincipalAuthorizationVersion(ctx, recipient); err != nil {
		return err
	}
	if _, err := identity.RevokeActiveTokenFamilies(ctx, recipient, now, "engine_access_delegation_changed"); err != nil {
		return err
	}
	principalType, contextType := iam.PrincipalTypeUser, iam.ContextTypeTenant
	metadata.PrincipalID, metadata.PrincipalType, metadata.TenantID, metadata.ContextType = &actor.PrincipalID, &principalType, &actor.TenantID, &contextType
	return iam.NewAuditWriter(identity).Write(ctx, iam.AuditEvent{Metadata: metadata,
		EventName: event, Result: iam.AuditResultSucceeded, RiskLevel: iam.AuditRiskHigh, ModuleName: "system",
		EntityType: "engine_access_delegation", EntityID: strconv.FormatInt(d.ID, 10),
		Details: map[string]any{"engine_id": d.EngineID, "tenant_membership_id": d.TenantMembershipID, "expires_at": d.ExpiresAt, "reason": reason}})
}
