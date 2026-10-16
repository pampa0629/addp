package execution_test

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/execution"
)

func TestUserOriginRequiresVerifiedTenantUserFacts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*authorization.AuthContext, *execution.TaskExecution)
	}{
		{"overflow_principal", func(a *authorization.AuthContext, _ *execution.TaskExecution) { a.Principal.ID = "9223372036854775808" }},
		{"overflow_version", func(a *authorization.AuthContext, _ *execution.TaskExecution) {
			a.Authorization.AuthorizationVersion = "9223372036854775808"
		}},
		{"service", func(a *authorization.AuthContext, _ *execution.TaskExecution) { a.Principal.Type = "service_principal" }},
		{"platform", func(a *authorization.AuthContext, _ *execution.TaskExecution) { a.Context.Type = "platform" }},
		{"membership", func(a *authorization.AuthContext, _ *execution.TaskExecution) { a.Context.TenantMembershipID = nil }},
		{"version", func(a *authorization.AuthContext, _ *execution.TaskExecution) {
			a.Authorization.AuthorizationVersion = "0"
		}},
		{"tenant", func(_ *authorization.AuthContext, e *execution.TaskExecution) { e.TenantID = 8 }},
		{"actor", func(_ *authorization.AuthContext, e *execution.TaskExecution) { v := 32; e.TriggeredBy = &v }},
		{"ticket", func(a *authorization.AuthContext, _ *execution.TaskExecution) {
			a.Token.Type = "resource_access_ticket"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := authtest.NewTenantUserAuthContext("7", "31", nil)
			row := &execution.TaskExecution{TenantID: 7}
			ctx := execution.WithUserOrigin(context.Background(), facts)
			tc.mutate(&facts, row)
			ctx = execution.WithUserOrigin(ctx, facts)
			if err := execution.ApplyUserOrigin(ctx, row); err == nil {
				t.Fatal("invalid origin accepted")
			}
			if row.ActorPrincipalID != nil {
				t.Fatal("failed capture wrote origin")
			}
		})
	}
	if err := execution.ApplyUserOrigin(context.Background(), &execution.TaskExecution{TenantID: 7}); err == nil {
		t.Fatal("missing source accepted")
	}
}

func TestCopyActorOriginCopiesOnlyConsistentDetachedIdentity(t *testing.T) {
	facts := authtest.NewTenantUserAuthContext("7", "31", nil)
	parent := &execution.TaskExecution{TenantID: 7}
	if err := execution.ApplyUserOrigin(execution.WithUserOrigin(context.Background(), facts), parent); err != nil {
		t.Fatal(err)
	}
	grant, expiry, token := int64(99), time.Now().Add(time.Hour), "lease-secret"
	parent.ExecutionAuthorizationID, parent.AuthorizationExpiresAt, parent.LeaseToken = &grant, &expiry, &token
	child := &execution.TaskExecution{TenantID: 7}
	if err := execution.CopyActorOrigin(child, parent); err != nil {
		t.Fatal(err)
	}
	if *child.TriggeredBy != 31 || *child.ActorPrincipalID != 31 || *child.ActorTenantMembershipID != *parent.ActorTenantMembershipID || *child.IssuedAuthorizationVersion != *parent.IssuedAuthorizationVersion {
		t.Fatal("origin mismatch")
	}
	if child.ExecutionAuthorizationID != nil || child.AuthorizationExpiresAt != nil || child.LeaseToken != nil {
		t.Fatal("origin copied authorization or lease")
	}
	*parent.ActorPrincipalID = 32
	if *child.ActorPrincipalID != 31 {
		t.Fatal("origin pointers were shared")
	}
	if err := execution.CopyActorOrigin(&execution.TaskExecution{TenantID: 7}, parent); err == nil {
		t.Fatal("contradictory initiator accepted")
	}
	*parent.ActorPrincipalID = 31
	if err := execution.CopyActorOrigin(&execution.TaskExecution{TenantID: 8}, parent); err == nil {
		t.Fatal("cross-tenant origin accepted")
	}
	parent.TriggeredBy = nil
	if err := execution.CopyActorOrigin(&execution.TaskExecution{TenantID: 7}, parent); err == nil {
		t.Fatal("missing initiator accepted")
	}
}
