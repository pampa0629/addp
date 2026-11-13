package iam

import (
	"context"
	"testing"

	"github.com/addp/common/authorization"
)

func TestAuthenticatedRequestIdentityIsCredentialBoundAndDetached(t *testing.T) {
	tenant := "3"
	identity := authorization.AuthContext{
		Principal: authorization.AuthPrincipal{Type: "user", ID: "12"},
		Context:   authorization.AuthSessionContext{Type: "tenant", TenantID: &tenant},
		Client:    authorization.ClientConstraints{Audiences: []string{"addp.api"}},
	}
	parent := context.Background()
	ctx := WithAuthenticatedRequestIdentity(parent, "addp_at_original", identity)
	*identity.Context.TenantID = "9"
	identity.Client.Audiences[0] = "changed"
	current, ok := AuthenticatedRequestIdentity(ctx, "addp_at_original")
	if !ok || *current.Context.TenantID != "3" || current.Client.Audiences[0] != "addp.api" {
		t.Fatal("request fact aliases its authenticated input")
	}
	*current.Context.TenantID = "10"
	current.Client.Audiences[0] = "changed-again"
	again, ok := AuthenticatedRequestIdentity(ctx, "addp_at_original")
	if !ok || *again.Context.TenantID != "3" || again.Client.Audiences[0] != "addp.api" {
		t.Fatal("consumer mutated the request's authenticated fact")
	}
	for _, candidate := range []struct {
		ctx        context.Context
		credential string
	}{
		{ctx, "addp_at_other"}, {ctx, ""}, {parent, "addp_at_original"},
		{context.Background(), "addp_at_original"},
	} {
		if _, ok := AuthenticatedRequestIdentity(candidate.ctx, candidate.credential); ok {
			t.Fatal("fact escaped its request or credential binding")
		}
	}
}
