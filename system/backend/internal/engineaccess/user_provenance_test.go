package engineaccess

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
)

func TestUserProvenanceIdentityPredicate(t *testing.T) {
	now := time.Now().UTC()
	valid := func() *lockedUserProvenance {
		expires := now.Add(time.Minute)
		return &lockedUserProvenance{
			source: userProvenance{PrincipalID: 7, MembershipID: 8, AuthorizationVersion: 3}, tenantID: 9,
			principal: &iam.Principal{ID: 7, PrincipalType: iam.PrincipalTypeUser, Status: iam.PrincipalStatusActive, AuthorizationVersion: 3},
			member:    &iam.TenantMembership{ID: 8, TenantID: 9, PrincipalID: 7, Status: iam.TenantMembershipStatusActive, ExpiresAt: &expires},
			tenant:    &iam.Tenant{ID: 9, Status: iam.TenantStatusActive},
		}
	}
	if err := valid().check(now); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*lockedUserProvenance){
		func(p *lockedUserProvenance) { p.source = userProvenance{} },
		func(p *lockedUserProvenance) { p.principal = nil },
		func(p *lockedUserProvenance) { p.member = nil },
		func(p *lockedUserProvenance) { p.tenant = nil },
		func(p *lockedUserProvenance) { p.principal.ID++ },
		func(p *lockedUserProvenance) { p.principal.PrincipalType = iam.PrincipalTypeServicePrincipal },
		func(p *lockedUserProvenance) { p.principal.Status = iam.PrincipalStatusSuspended },
		func(p *lockedUserProvenance) { p.principal.AuthorizationVersion++ },
		func(p *lockedUserProvenance) { p.member.ID++ },
		func(p *lockedUserProvenance) { p.member.TenantID++ },
		func(p *lockedUserProvenance) { p.member.PrincipalID++ },
		func(p *lockedUserProvenance) { p.member.Status = iam.TenantMembershipStatusSuspended },
		func(p *lockedUserProvenance) { p.member.ExpiresAt = &now },
		func(p *lockedUserProvenance) { p.tenant.ID++ },
		func(p *lockedUserProvenance) { p.tenant.Status = iam.TenantStatusSuspended },
	} {
		p := valid()
		mutate(p)
		if err := p.check(now); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("invalid identity allowed: %+v error=%v", p, err)
		}
	}
	if err := (*lockedUserProvenance)(nil).check(now); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("nil identity must fail closed")
	}
	if err := valid().check(now.Add(time.Minute)); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("locks must not freeze the membership's time validity")
	}
}

func TestUserProvenanceBindingPreservesBigintAndHasNoCredential(t *testing.T) {
	source := userProvenance{PrincipalID: 9007199254740993, MembershipID: 9007199254740995, AuthorizationVersion: 9007199254740997}
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]string
	if err := json.Unmarshal(encoded, &fields); err != nil || len(fields) != 3 ||
		fields["principal_id"] != "9007199254740993" || fields["tenant_membership_id"] != "9007199254740995" ||
		fields["authorization_version"] != "9007199254740997" {
		t.Fatalf("source identity is not exact decimal strings: %s, %v", encoded, err)
	}
	for _, forbidden := range []string{"token", "role", "permission", "department", "responsibility"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("provenance copied credential or authority snapshot: %s", encoded)
		}
	}
}

func TestManagementScopePredicate(t *testing.T) {
	now := time.Now().UTC()
	valid := func() *lockedManagementScope {
		tenantID := uint(9)
		return &lockedManagementScope{tenantID: 9, engineID: 10, membershipID: 8,
			engine: &models.Engine{ID: 10, TenantID: &tenantID, LifecycleState: models.EngineLifecycleActive},
			delegation: &Delegation{ID: 11, TenantID: 9, EngineID: 10, TenantMembershipID: 8,
				Status: "active", GrantedAt: now, ExpiresAt: now.Add(time.Minute)}}
	}
	if err := valid().check(now); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*lockedManagementScope){
		"invalid tenant":      func(s *lockedManagementScope) { s.tenantID = 0 },
		"invalid engine":      func(s *lockedManagementScope) { s.engineID = 0 },
		"invalid membership":  func(s *lockedManagementScope) { s.membershipID = 0 },
		"missing engine":      func(s *lockedManagementScope) { s.engine = nil },
		"missing tenant":      func(s *lockedManagementScope) { s.engine.TenantID = nil },
		"other tenant engine": func(s *lockedManagementScope) { v := uint(12); s.engine.TenantID = &v },
		"other engine":        func(s *lockedManagementScope) { s.engine.ID++ },
		"deleting engine":     func(s *lockedManagementScope) { s.engine.LifecycleState = models.EngineLifecycleDeleting },
		"deleted engine":      func(s *lockedManagementScope) { s.engine.LifecycleState = models.EngineLifecycleDeleted },
		"missing delegation":  func(s *lockedManagementScope) { s.delegation = nil },
		"invalid delegation":  func(s *lockedManagementScope) { s.delegation.ID = 0 },
		"other tenant":        func(s *lockedManagementScope) { s.delegation.TenantID++ },
		"other target":        func(s *lockedManagementScope) { s.delegation.EngineID++ },
		"other membership":    func(s *lockedManagementScope) { s.delegation.TenantMembershipID++ },
		"revoked":             func(s *lockedManagementScope) { s.delegation.Status = "revoked" },
		"not yet effective":   func(s *lockedManagementScope) { s.delegation.GrantedAt = now.Add(time.Second) },
		"deadline reached":    func(s *lockedManagementScope) { s.delegation.ExpiresAt = now },
	} {
		t.Run(name, func(t *testing.T) {
			s := valid()
			mutate(s)
			if err := s.check(now); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("invalid management scope allowed: %+v error=%v", s, err)
			}
			if err := s.checkManagement(now); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("invalid withdrawal scope allowed: %+v error=%v", s, err)
			}
		})
	}
	if err := (*lockedManagementScope)(nil).check(now); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("nil scope must fail closed")
	}
	if err := valid().check(now.Add(time.Minute)); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("a row lock must not freeze delegation time validity")
	}
	disabled := valid()
	disabled.engine.LifecycleState = models.EngineLifecycleDisabled
	if err := disabled.checkManagement(now); err != nil {
		t.Fatalf("disabled engine prevents withdrawal: %v", err)
	}
	if err := disabled.check(now); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("withdrawal qualification must not allow new access on disabled engine")
	}
	disabled.delegation.Status = "revoked"
	if err := disabled.checkManagement(now); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("disabled engine bypassed delegation revocation")
	}
	if err := (*lockedManagementScope)(nil).checkManagement(now); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("nil withdrawal scope must fail closed")
	}
}
