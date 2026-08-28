package authtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	commonauth "github.com/addp/common/authorization"
)

const (
	AssetServiceToken             = "asset-service"
	AssetServiceNoPermissionToken = "asset-service-no-permission"
	OtherServiceToken             = "other-service"
	UserToken                     = "user"
)

// NewTenantAuthContextServer resolves deterministic test Bearer tokens through
// the same canonical AuthContext HTTP contract used by production middleware.
func NewTenantAuthContextServer(t testing.TB, tenantID, permission string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/auth/context" {
			http.NotFound(w, r)
			return
		}

		var authContext commonauth.AuthContext
		switch r.Header.Get("Authorization") {
		case "Bearer " + AssetServiceToken:
			authContext = tenantAuthContext("service_principal", "addp-asset", tenantID, permission)
		case "Bearer " + AssetServiceNoPermissionToken:
			authContext = tenantAuthContext("service_principal", "addp-asset", tenantID, "asset.category.read")
		case "Bearer " + OtherServiceToken:
			authContext = tenantAuthContext("service_principal", "addp-meta", tenantID, permission)
		case "Bearer " + UserToken:
			authContext = tenantAuthContext("user", "addp-web", tenantID, permission)
		default:
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(authContext); err != nil {
			t.Errorf("encode AuthContext: %v", err)
		}
	}))
}

// NewTenantUserAuthContextServer resolves caller-defined User Bearer tokens.
// It is intended for production-router Permission contract tests.
func NewTenantUserAuthContextServer(t testing.TB, tenantID string, tokenPermissions map[string][]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/auth/context" {
			http.NotFound(w, r)
			return
		}

		token := r.Header.Get("Authorization")
		permissions, exists := tokenPermissions[token]
		if !exists {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(NewTenantUserAuthContext(tenantID, "9", permissions)); err != nil {
			t.Errorf("encode AuthContext: %v", err)
		}
	}))
}

type TenantServiceIdentity struct {
	ClientID    string
	Permissions []string
}

// NewTenantServiceAuthContextServer resolves caller-defined Service Principal
// Bearer tokens for production-router client and Permission contract tests.
func NewTenantServiceAuthContextServer(t testing.TB, tenantID string, tokenIdentities map[string]TenantServiceIdentity) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/system/auth/context" {
			http.NotFound(w, r)
			return
		}

		identity, exists := tokenIdentities[r.Header.Get("Authorization")]
		if !exists {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		authContext := tenantAuthContext("service_principal", identity.ClientID, tenantID, "iam.user.read")
		authContext.Authorization.RoleAssignments[0].Permissions = append([]string(nil), identity.Permissions...)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(authContext); err != nil {
			t.Errorf("encode AuthContext: %v", err)
		}
	}))
}

// NewTenantUserAuthContext builds a canonical tenant User AuthContext for tests.
func NewTenantUserAuthContext(tenantID, principalID string, permissions []string) commonauth.AuthContext {
	authContext := tenantAuthContext("user", "addp-web", tenantID, "iam.user.read")
	authContext.Principal.ID = principalID
	authContext.Authorization.RoleAssignments[0].Permissions = append([]string(nil), permissions...)
	sort.Strings(authContext.Authorization.RoleAssignments[0].Permissions)
	return authContext
}

func tenantAuthContext(principalType, clientID, tenantID, permission string) commonauth.AuthContext {
	now := time.Now().UTC().Truncate(time.Second)
	membershipID := "1"
	authentication := commonauth.AuthenticationFacts{
		Methods: []string{"service_secret"}, AssuranceLevel: "not_applicable", AuthenticatedAt: now,
	}
	tokenType := "service_access_token"
	roleKey := "tenant.asset_runtime"
	if principalType == "user" {
		authentication = commonauth.AuthenticationFacts{
			Methods: []string{"password"}, AssuranceLevel: "aal1", AuthenticatedAt: now,
		}
		tokenType = "first_party_access_token"
		roleKey = "tenant.administrator"
	}

	return commonauth.AuthContext{
		SchemaVersion: commonauth.AuthContextSchemaVersion,
		Principal:     commonauth.AuthPrincipal{Type: principalType, ID: "9"},
		Context: commonauth.AuthSessionContext{
			Type: "tenant", TenantID: &tenantID, TenantMembershipID: &membershipID,
		},
		Authentication: authentication,
		Client: commonauth.ClientConstraints{
			ClientID: &clientID, Audiences: []string{"addp.api"}, ScopeMode: "unrestricted", Scopes: []string{},
		},
		Organization: commonauth.OrganizationContext{
			Departments: []commonauth.DepartmentMembership{}, ProjectGroups: []commonauth.ProjectGroupMembership{},
		},
		Authorization: commonauth.AuthorizationFacts{
			AuthorizationVersion: "1",
			RoleAssignments: []commonauth.RoleAssignment{{
				AssignmentID: "1", RoleKey: roleKey,
				Scope:       commonauth.AssignmentScope{Type: "tenant", TenantID: &tenantID},
				Permissions: []string{permission}, SourceType: "manual", ValidFrom: now.Add(-time.Hour),
			}},
		},
		Token: commonauth.TokenFacts{Type: tokenType, IssuedAt: now, ExpiresAt: now.Add(time.Hour)},
	}
}
