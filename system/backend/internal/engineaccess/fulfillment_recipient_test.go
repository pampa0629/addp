package engineaccess

import (
	"errors"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/system/internal/iam"
)

func TestFulfillmentRecipientUsesCurrentIdentityAndStrictExpiry(t *testing.T) {
	now := time.Now().UTC()
	valid := func() *lockedFulfillmentRecipient {
		return &lockedFulfillmentRecipient{tenantID: 1, id: 2, kind: "user",
			principal: &iam.Principal{ID: 2, PrincipalType: iam.PrincipalTypeUser, Status: iam.PrincipalStatusActive, AuthorizationVersion: 99},
			member:    &iam.TenantMembership{TenantID: 1, PrincipalID: 2, Status: iam.TenantMembershipStatusActive}}
	}
	if err := valid().check(now); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*lockedFulfillmentRecipient){
		"missing principal":   func(v *lockedFulfillmentRecipient) { v.principal = nil },
		"machine":             func(v *lockedFulfillmentRecipient) { v.principal.PrincipalType = iam.PrincipalTypeServicePrincipal },
		"suspended":           func(v *lockedFulfillmentRecipient) { v.principal.Status = iam.PrincipalStatusSuspended },
		"foreign membership":  func(v *lockedFulfillmentRecipient) { v.member.TenantID++ },
		"other account":       func(v *lockedFulfillmentRecipient) { v.member.PrincipalID++ },
		"ended membership":    func(v *lockedFulfillmentRecipient) { v.member.Status = iam.TenantMembershipStatusEnded },
		"expires exactly now": func(v *lockedFulfillmentRecipient) { v.member.ExpiresAt = &now },
		"unknown type":        func(v *lockedFulfillmentRecipient) { v.kind = "tenant" },
	} {
		t.Run(name, func(t *testing.T) {
			v := valid()
			mutate(v)
			if err := v.check(now); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatalf("invalid recipient accepted: %v", err)
			}
		})
	}
	if err := (*lockedFulfillmentRecipient)(nil).check(now); !errors.Is(err, commonapi.ErrForbidden) {
		t.Fatal(err)
	}
}
