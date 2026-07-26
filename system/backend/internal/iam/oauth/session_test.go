package oauth

import (
	"testing"
	"time"

	"github.com/ory/fosite"
)

func TestIAMSessionCloneIsDeepCopy(t *testing.T) {
	membershipID := int64(17)
	original := NewIAMSession()
	original.PrincipalID = 11
	original.TenantMembershipID = &membershipID
	original.Subject = "subject-11"
	original.AuthenticationMethods = []string{"password", "totp"}
	original.OIDCExtraClaims = map[string]interface{}{
		"profile": map[string]interface{}{"locale": "zh-CN"},
	}
	original.SetExpiresAt(fosite.AccessToken, time.Now().UTC().Add(time.Hour))
	original.IDTokenHeaders().Add("kid", "key-1")

	clone, ok := original.Clone().(*IAMSession)
	if !ok {
		t.Fatalf("Clone() type = %T", original.Clone())
	}
	clone.AuthenticationMethods[0] = "changed"
	clone.OIDCExtraClaims["profile"].(map[string]interface{})["locale"] = "en-US"
	*clone.TenantMembershipID = 18
	clone.IDTokenHeaders().Add("kid", "key-2")

	if original.AuthenticationMethods[0] != "password" ||
		original.OIDCExtraClaims["profile"].(map[string]interface{})["locale"] != "zh-CN" ||
		*original.TenantMembershipID != 17 || original.IDTokenHeaders().Get("kid") != "key-1" {
		t.Fatal("Clone() shared mutable state with the original session")
	}
}

func TestIAMSessionProjectsStableOIDCClaims(t *testing.T) {
	authenticatedAt := time.Now().UTC().Add(-time.Minute)
	requestedAt := authenticatedAt.Add(time.Minute)
	session := NewIAMSession()
	session.Subject = "subject-1"
	session.AuthenticationMethods = []string{"password"}
	session.AssuranceLevel = "aal1"
	session.AuthenticatedAt = authenticatedAt
	session.RequestedAt = requestedAt
	session.OIDCNonce = "nonce-value"
	session.OIDCExtraClaims = map[string]interface{}{"tenant": "tenant-1"}

	claims := session.IDTokenClaims()
	if claims.Subject != session.Subject || claims.Nonce != session.OIDCNonce ||
		!claims.AuthTime.Equal(authenticatedAt) || !claims.RequestedAt.Equal(requestedAt) ||
		claims.AuthenticationContextClassReference != "aal1" ||
		len(claims.AuthenticationMethodsReferences) != 1 ||
		claims.Extra["tenant"] != "tenant-1" {
		t.Fatalf("unexpected projected claims: %#v", claims)
	}
	if session.GetUsername() != "" {
		t.Fatalf("GetUsername() = %q, want empty", session.GetUsername())
	}
}
