package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	sharedauth "github.com/addp/common/middleware/auth"
	"github.com/gin-gonic/gin"
)

func TestIAMAuthenticationMiddlewareInjectsOnlyCanonicalContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authContext := testIAMAuthContext()
	resolver := &fakeIAMAuthContextResolver{authContext: &authContext}
	middleware, err := NewIAMAuthenticationMiddleware(resolver)
	if err != nil {
		t.Fatalf("NewIAMAuthenticationMiddleware() error = %v", err)
	}

	router := gin.New()
	router.Use(middleware)
	router.GET("/resource", func(c *gin.Context) {
		resolved, exists := IAMAuthContextFromGin(c)
		if !exists || resolved.Principal.ID != "12" {
			t.Fatalf("IAM AuthContext = %#v, exists = %t", resolved, exists)
		}
		for _, legacyKey := range []string{"user_id", "username", "user_type", "tenant_id", "authorization_context"} {
			if _, exists := c.Get(legacyKey); exists {
				t.Fatalf("legacy Gin Context key %q was injected", legacyKey)
			}
		}

		resolved.Authorization.RoleAssignments[0].Permissions[0] = "changed.permission.read"
		again, _ := IAMAuthContextFromGin(c)
		if again.Authorization.RoleAssignments[0].Permissions[0] != "manager.content.read" {
			t.Fatal("IAMAuthContextFromGin returned shared mutable state")
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/resource", nil)
	request.Header.Set("Authorization", "bearer addp_at_access")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || resolver.accessToken != "addp_at_access" {
		t.Fatalf("status = %d, token = %q, body = %s", response.Code, resolver.accessToken, response.Body.String())
	}
}

func TestIAMAuthenticationMiddlewareRejectsInvalidRequestsAndResolverFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name       string
		header     string
		resolver   *fakeIAMAuthContextResolver
		wantStatus int
		wantType   string
	}{
		{
			name:       "missing bearer",
			resolver:   &fakeIAMAuthContextResolver{},
			wantStatus: http.StatusUnauthorized,
			wantType:   "authentication_required",
		},
		{
			name:       "malformed bearer",
			header:     "Bearer token extra",
			resolver:   &fakeIAMAuthContextResolver{},
			wantStatus: http.StatusUnauthorized,
			wantType:   "authentication_required",
		},
		{
			name:       "invalid token",
			header:     "Bearer addp_at_invalid",
			resolver:   &fakeIAMAuthContextResolver{err: commonapi.ErrUnauthorized},
			wantStatus: http.StatusUnauthorized,
			wantType:   "authentication_required",
		},
		{
			name:       "repository failure",
			header:     "Bearer addp_at_access",
			resolver:   &fakeIAMAuthContextResolver{err: errors.New("database password leaked")},
			wantStatus: http.StatusInternalServerError,
			wantType:   "internal_error",
		},
		{
			name:       "invalid projected context",
			header:     "Bearer addp_at_access",
			resolver:   &fakeIAMAuthContextResolver{authContext: &commonauth.AuthContext{}},
			wantStatus: http.StatusInternalServerError,
			wantType:   "internal_error",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			middleware, err := NewIAMAuthenticationMiddleware(testCase.resolver)
			if err != nil {
				t.Fatalf("NewIAMAuthenticationMiddleware() error = %v", err)
			}
			router := gin.New()
			router.Use(middleware)
			router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			request := httptest.NewRequest(http.MethodGet, "/resource", nil)
			if testCase.header != "" {
				request.Header.Set("Authorization", testCase.header)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus || !responseContainsErrorCode(response, testCase.wantType) {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			if testCase.wantType == "internal_error" && strings.Contains(response.Body.String(), "database password leaked") {
				t.Fatal("internal resolver error was exposed")
			}
		})
	}
}

func TestIAMRouteGuards(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Run("missing canonical context is unauthorized", func(t *testing.T) {
		for _, guard := range []gin.HandlerFunc{RequireIAMAuthenticated(), RequireIAMSelf()} {
			response := performIAMGuardRequest(guard)
			if response.Code != http.StatusUnauthorized || !responseContainsErrorCode(response, "authentication_required") {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		}
	})

	t.Run("self requires a user principal", func(t *testing.T) {
		authContext := testIAMAuthContext()
		authContext.Principal.Type = "service_principal"
		authContext.Authentication = commonauth.AuthenticationFacts{
			Methods:         []string{"service_secret"},
			AssuranceLevel:  "not_applicable",
			AuthenticatedAt: authContext.Authentication.AuthenticatedAt,
		}
		authContext.Token.Type = "service_access_token"
		authContext.Client.ClientID = nil
		response := performIAMGuardRequest(withIAMAuthContext(authContext), RequireIAMSelf())
		if response.Code != http.StatusForbidden || !responseContainsErrorCode(response, "permission_denied") {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
	})

	t.Run("authenticated and self accept a user", func(t *testing.T) {
		authContext := testIAMAuthContext()
		for _, guard := range []gin.HandlerFunc{RequireIAMAuthenticated(), RequireIAMSelf()} {
			response := performIAMGuardRequest(withIAMAuthContext(authContext), guard)
			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		}
	})
}

func TestIAMPermissionGuardRequiresAllPermissionsAcrossAssignments(t *testing.T) {
	gin.SetMode(gin.TestMode)
	guard, err := NewIAMPermissionGuard("system.engine.execute", "manager.content.read")
	if err != nil {
		t.Fatalf("NewIAMPermissionGuard() error = %v", err)
	}
	authContext := testIAMAuthContext()
	tenantID := *authContext.Context.TenantID
	authContext.Authorization.RoleAssignments = append(
		authContext.Authorization.RoleAssignments,
		commonauth.RoleAssignment{
			AssignmentID: "403",
			RoleKey:      "tenant.infrastructure_administrator",
			Scope:        commonauth.AssignmentScope{Type: "tenant", TenantID: &tenantID},
			Permissions:  []string{"system.engine.execute"},
			SourceType:   "manual",
			ValidFrom:    authContext.Authorization.RoleAssignments[0].ValidFrom,
		},
	)
	allowed := performIAMGuardRequest(withIAMAuthContext(authContext), guard)
	if allowed.Code != http.StatusNoContent {
		t.Fatalf("allowed status = %d, body = %s", allowed.Code, allowed.Body.String())
	}

	authContext.Authorization.RoleAssignments[1].Permissions = []string{"system.engine.read"}
	denied := performIAMGuardRequest(withIAMAuthContext(authContext), guard)
	if denied.Code != http.StatusForbidden || !responseContainsErrorCode(denied, "permission_denied") {
		t.Fatalf("denied status = %d, body = %s", denied.Code, denied.Body.String())
	}
}

func TestIAMCredentialGuardAllowsOnlyConfiguredTokenTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	guard, err := NewIAMCredentialGuard("first_party_access_token", "oauth_access_token")
	if err != nil {
		t.Fatalf("NewIAMCredentialGuard() error = %v", err)
	}

	authContext := testIAMAuthContext()
	allowed := performIAMGuardRequest(withIAMAuthContext(authContext), guard)
	if allowed.Code != http.StatusNoContent {
		t.Fatalf("first-party status = %d, body = %s", allowed.Code, allowed.Body.String())
	}

	authContext.Token.Type = "oauth_access_token"
	authContext.Client.ScopeMode = "restricted"
	authContext.Client.Scopes = []string{"addp.api"}
	oauthAllowed := performIAMGuardRequest(withIAMAuthContext(authContext), guard)
	if oauthAllowed.Code != http.StatusNoContent {
		t.Fatalf("OAuth status = %d, body = %s", oauthAllowed.Code, oauthAllowed.Body.String())
	}

	authContext.Token.Type = "delegated_access_token"
	authContext.Client.Audiences = []string{"develop"}
	authContext.Client.ScopeMode = "restricted"
	authContext.Client.Scopes = []string{"workflow.run"}
	authContext.Delegation = &commonauth.DelegationFacts{
		DelegatedByClientID: *authContext.Client.ClientID,
		AgentRunID:          "agent-run-1",
		ToolCallID:          "tool-call-1",
	}
	denied := performIAMGuardRequest(withIAMAuthContext(authContext), guard)
	if denied.Code != http.StatusForbidden || !responseContainsErrorCode(denied, "permission_denied") {
		t.Fatalf("delegated status = %d, body = %s", denied.Code, denied.Body.String())
	}

	missing := performIAMGuardRequest(guard)
	if missing.Code != http.StatusUnauthorized || !responseContainsErrorCode(missing, "authentication_required") {
		t.Fatalf("missing context status = %d, body = %s", missing.Code, missing.Body.String())
	}
}

func TestIAMPermissionGuardRejectsInvalidConfiguration(t *testing.T) {
	for _, permissions := range [][]string{
		nil,
		{"manager.read"},
		{"manager.content.read", "manager.content.read"},
	} {
		if _, err := NewIAMPermissionGuard(permissions...); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("NewIAMPermissionGuard(%v) error = %v", permissions, err)
		}
	}
	if _, err := NewIAMAuthenticationMiddleware(nil); !errors.Is(err, commonapi.ErrBadRequest) {
		t.Fatalf("NewIAMAuthenticationMiddleware(nil) error = %v", err)
	}
	for _, tokenTypes := range [][]string{
		nil,
		{""},
		{"oauth_access_token", "oauth_access_token"},
	} {
		if _, err := NewIAMCredentialGuard(tokenTypes...); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("NewIAMCredentialGuard(%v) error = %v", tokenTypes, err)
		}
	}
}

type fakeIAMAuthContextResolver struct {
	accessToken string
	authContext *commonauth.AuthContext
	err         error
}

func (resolver *fakeIAMAuthContextResolver) ResolveAuthContext(
	_ context.Context,
	accessToken string,
) (*commonauth.AuthContext, error) {
	resolver.accessToken = accessToken
	return resolver.authContext, resolver.err
}

func testIAMAuthContext() commonauth.AuthContext {
	tenantID := "3"
	membershipID := "28"
	clientID := "addp-web"
	issuedAt := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	return commonauth.AuthContext{
		SchemaVersion: commonauth.AuthContextSchemaVersion,
		Principal:     commonauth.AuthPrincipal{Type: "user", ID: "12"},
		Context: commonauth.AuthSessionContext{
			Type:               "tenant",
			TenantID:           &tenantID,
			TenantMembershipID: &membershipID,
		},
		Authentication: commonauth.AuthenticationFacts{
			Methods:         []string{"password"},
			AssuranceLevel:  "aal1",
			AuthenticatedAt: issuedAt.Add(-time.Minute),
		},
		Client: commonauth.ClientConstraints{
			ClientID:  &clientID,
			Audiences: []string{"addp.api"},
			ScopeMode: "unrestricted",
			Scopes:    []string{},
		},
		Organization: commonauth.OrganizationContext{
			Departments:   []commonauth.DepartmentMembership{},
			ProjectGroups: []commonauth.ProjectGroupMembership{},
		},
		Authorization: commonauth.AuthorizationFacts{
			AuthorizationVersion: "42",
			RoleAssignments: []commonauth.RoleAssignment{{
				AssignmentID: "402",
				RoleKey:      "tenant.data_viewer",
				Scope:        commonauth.AssignmentScope{Type: "tenant", TenantID: &tenantID},
				Permissions:  []string{"manager.content.read", "manager.data_item.read"},
				SourceType:   "manual",
				ValidFrom:    issuedAt.Add(-time.Hour),
			}},
		},
		Token: commonauth.TokenFacts{
			Type:      "first_party_access_token",
			IssuedAt:  issuedAt,
			ExpiresAt: issuedAt.Add(15 * time.Minute),
		},
	}
}

func withIAMAuthContext(authContext commonauth.AuthContext) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := sharedauth.SetAuthContextForGin(c, authContext); err != nil {
			panic(err)
		}
		c.Next()
	}
}

func performIAMGuardRequest(handlers ...gin.HandlerFunc) *httptest.ResponseRecorder {
	router := gin.New()
	router.GET("/resource", append(handlers, func(c *gin.Context) { c.Status(http.StatusNoContent) })...)
	request := httptest.NewRequest(http.MethodGet, "/resource", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func responseContainsErrorCode(response *httptest.ResponseRecorder, errorCode string) bool {
	return strings.Contains(response.Body.String(), `"error_code":"`+errorCode+`"`)
}
