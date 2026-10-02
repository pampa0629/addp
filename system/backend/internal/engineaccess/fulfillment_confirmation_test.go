package engineaccess

import (
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
)

func TestBusinessConfirmerUsesCurrentQualificationNotAuditVersion(t *testing.T) {
	now := time.Now().UTC()
	tenantID := int64(9)
	valid := func() *lockedBusinessConfirmer {
		return &lockedBusinessConfirmer{
			identity: &lockedUserProvenance{
				source: userProvenance{PrincipalID: 7, MembershipID: 8, AuthorizationVersion: 3}, tenantID: tenantID,
				principal: &iam.Principal{ID: 7, PrincipalType: iam.PrincipalTypeUser, Status: iam.PrincipalStatusActive, AuthorizationVersion: 4},
				member:    &iam.TenantMembership{ID: 8, TenantID: tenantID, PrincipalID: 7, Status: iam.TenantMembershipStatusActive},
				tenant:    &iam.Tenant{ID: tenantID, Status: iam.TenantStatusActive},
			},
			permissions: []iam.RoleAssignmentPermissionProjection{
				{ScopeType: "tenant", TenantID: &tenantID, PermissionKey: "catalog.entry.read", ValidFrom: now},
				{ScopeType: "tenant", TenantID: &tenantID, PermissionKey: "catalog.sharing_decision.create", ValidFrom: now},
			},
		}
	}
	if err := valid().check(now); err != nil {
		t.Fatalf("unrelated authorization version change invalidated confirmation: %v", err)
	}
	if err := valid().identity.check(now); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatalf("confirmer policy weakened the operator's bound version: %v", err)
	}
	for name, mutate := range map[string]func(*lockedBusinessConfirmer){
		"missing identity":          func(c *lockedBusinessConfirmer) { c.identity = nil },
		"invalid historical source": func(c *lockedBusinessConfirmer) { c.identity.source.AuthorizationVersion = 0 },
		"machine identity": func(c *lockedBusinessConfirmer) {
			c.identity.principal.PrincipalType = iam.PrincipalTypeServicePrincipal
		},
		"suspended account":              func(c *lockedBusinessConfirmer) { c.identity.principal.Status = iam.PrincipalStatusSuspended },
		"new membership is not original": func(c *lockedBusinessConfirmer) { c.identity.member.ID++ },
		"other tenant":                   func(c *lockedBusinessConfirmer) { c.identity.member.TenantID++ },
		"suspended membership":           func(c *lockedBusinessConfirmer) { c.identity.member.Status = iam.TenantMembershipStatusSuspended },
		"expired membership":             func(c *lockedBusinessConfirmer) { c.identity.member.ExpiresAt = &now },
		"suspended tenant":               func(c *lockedBusinessConfirmer) { c.identity.tenant.Status = iam.TenantStatusSuspended },
		"no permissions":                 func(c *lockedBusinessConfirmer) { c.permissions = nil },
		"missing read":                   func(c *lockedBusinessConfirmer) { c.permissions = c.permissions[1:] },
		"missing confirmation":           func(c *lockedBusinessConfirmer) { c.permissions = c.permissions[:1] },
		"department scope":               func(c *lockedBusinessConfirmer) { c.permissions[1].ScopeType = "department" },
		"project scope":                  func(c *lockedBusinessConfirmer) { c.permissions[1].ScopeType = "project_group" },
		"missing scope tenant":           func(c *lockedBusinessConfirmer) { c.permissions[1].TenantID = nil },
		"other scope tenant":             func(c *lockedBusinessConfirmer) { other := int64(10); c.permissions[1].TenantID = &other },
		"not yet effective":              func(c *lockedBusinessConfirmer) { c.permissions[1].ValidFrom = now.Add(time.Second) },
		"assignment reached deadline":    func(c *lockedBusinessConfirmer) { c.permissions[1].ValidUntil = &now },
	} {
		t.Run(name, func(t *testing.T) {
			c := valid()
			mutate(c)
			if err := c.check(now); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("invalid confirmer qualification allowed: %v", err)
			}
		})
	}
	if err := (*lockedBusinessConfirmer)(nil).check(now); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("nil confirmation must fail closed")
	}
	c := valid()
	expires := now.Add(time.Minute)
	c.permissions[1].ValidUntil = &expires
	if err := c.check(now); err != nil {
		t.Fatal(err)
	}
	if err := c.check(expires); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal("principal lock must not freeze a role assignment's natural expiry")
	}
	// A separate current assignment may preserve qualification after one ends.
	c.permissions = append(c.permissions, valid().permissions[1])
	if err := c.check(expires); err != nil {
		t.Fatalf("expired assignment hid another valid confirmation permission: %v", err)
	}
}
