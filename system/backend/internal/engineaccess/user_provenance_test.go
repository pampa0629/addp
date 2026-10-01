package engineaccess

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
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
