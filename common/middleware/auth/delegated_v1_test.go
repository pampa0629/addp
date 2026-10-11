package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	commoni18n "github.com/addp/common/middleware/i18n"
	"github.com/gin-gonic/gin"
)

func TestDelegatedRouteGuardAllowsMatchingDelegationAndOrdinaryUserToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 24, 8, 1, 0, 0, time.UTC)
	guard, err := NewDelegatedRouteGuard(DelegatedRouteGuardConfig{
		Audience:            "develop",
		RequiredScopes:      []string{"workflow.run"},
		RequiredPermissions: []string{"develop.task.execute"},
		Now:                 func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewDelegatedRouteGuard() error = %v", err)
	}

	for _, authContext := range []commonauth.AuthContext{
		testDelegatedAuthContext(),
		testCanonicalAuthContext(),
	} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			setCanonicalAuthContext(c, authContext)
			c.Next()
		})
		router.POST("/tool", guard, func(c *gin.Context) { c.Status(http.StatusNoContent) })
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/tool", nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("token=%s status=%d body=%s", authContext.Token.Type, response.Code, response.Body.String())
		}
	}
}

func TestDelegatedRouteGuardRejectsInvalidDelegatedBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 24, 8, 1, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name        string
		authContext *commonauth.AuthContext
		mutate      func(*commonauth.AuthContext)
		wantStatus  int
		wantCode    string
	}{
		{name: "missing context", wantStatus: http.StatusUnauthorized, wantCode: "authentication_required"},
		{name: "wrong audience", authContext: delegatedContextPointer(), mutate: func(value *commonauth.AuthContext) { value.Client.Audiences = []string{"manager"} }, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "missing scope", authContext: delegatedContextPointer(), mutate: func(value *commonauth.AuthContext) { value.Client.Scopes = []string{"workflow.validate"} }, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "extra scope", authContext: delegatedContextPointer(), mutate: func(value *commonauth.AuthContext) {
			value.Client.Scopes = []string{"workflow.run", "workflow.validate"}
		}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "permission denied", authContext: delegatedContextPointer(), mutate: func(value *commonauth.AuthContext) {
			value.Authorization.RoleAssignments[0].Permissions = []string{"develop.task.read"}
		}, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "client mismatch", authContext: delegatedContextPointer(), mutate: func(value *commonauth.AuthContext) { value.Delegation.DelegatedByClientID = "addp-cli" }, wantStatus: http.StatusForbidden, wantCode: "permission_denied"},
		{name: "expired", authContext: delegatedContextPointer(), mutate: func(value *commonauth.AuthContext) { value.Token.ExpiresAt = now }, wantStatus: http.StatusUnauthorized, wantCode: "authentication_required"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			guard, err := NewDelegatedRouteGuard(DelegatedRouteGuardConfig{
				Audience:            "develop",
				RequiredScopes:      []string{"workflow.run"},
				RequiredPermissions: []string{"develop.task.execute"},
				Now:                 func() time.Time { return now },
			})
			if err != nil {
				t.Fatalf("NewDelegatedRouteGuard() error = %v", err)
			}
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			if testCase.authContext != nil {
				candidate := commonauth.CloneAuthContext(*testCase.authContext)
				if testCase.mutate != nil {
					testCase.mutate(&candidate)
				}
				router.Use(func(c *gin.Context) {
					setCanonicalAuthContext(c, candidate)
					c.Next()
				})
			}
			router.POST("/tool", guard, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/tool", nil))
			if response.Code != testCase.wantStatus ||
				!strings.Contains(response.Body.String(), `"error_code":"`+testCase.wantCode+`"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestDelegatedRouteGuardValidatesConfiguration(t *testing.T) {
	for _, config := range []DelegatedRouteGuardConfig{
		{Audience: "Develop", RequiredScopes: []string{"workflow.run"}, RequiredPermissions: []string{"develop.task.execute"}},
		{Audience: "develop", RequiredPermissions: []string{"develop.task.execute"}},
		{Audience: "develop", RequiredScopes: []string{"workflow"}, RequiredPermissions: []string{"develop.task.execute"}},
		{Audience: "develop", RequiredScopes: []string{"workflow.run", "workflow.run"}, RequiredPermissions: []string{"develop.task.execute"}},
		{Audience: "develop", RequiredScopes: []string{"workflow.run", "workflow.validate"}, RequiredPermissions: []string{"develop.task.execute"}},
		{Audience: "develop", RequiredScopes: []string{"workflow.run"}},
		{Audience: "develop", RequiredScopes: []string{"workflow.run"}, RequiredPermissions: []string{"meta.*.read"}},
		{Audience: "develop", RequiredScopes: []string{"workflow.run"}, RequiredPermissions: []string{"meta.catalog.read", "meta.catalog.read"}},
	} {
		if _, err := NewDelegatedRouteGuard(config); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("NewDelegatedRouteGuard(%#v) error = %v, want bad request", config, err)
		}
	}
}

func TestDelegatedRouteGuardChecksCrossOwnerIntersectionWithoutExpandingToken(t *testing.T) {
	permissions := []string{"meta.catalog.read", "system.engine.read", "transfer.task.create"}
	now := time.Date(2026, 7, 24, 8, 1, 0, 0, time.UTC)
	guard, err := NewDelegatedRouteGuard(DelegatedRouteGuardConfig{
		Audience: "transfer", RequiredScopes: []string{"transfer.task.config.check"}, RequiredPermissions: permissions,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "meta.catalog.read", "system.engine.read", "transfer.task.create", "wrong_audience", "extra_scope"} {
		t.Run(scenario, func(t *testing.T) {
			subject := testDelegatedAuthContext()
			subject.Client.Audiences = []string{"transfer"}
			subject.Client.Scopes = []string{"transfer.task.config.check"}
			keys := make([]string, 0, len(permissions))
			for _, permission := range permissions {
				if permission != scenario {
					keys = append(keys, permission)
				}
			}
			sort.Strings(keys)
			subject.Authorization.RoleAssignments[0].Permissions = keys
			if scenario == "wrong_audience" {
				subject.Client.Audiences = []string{"meta"}
			}
			if scenario == "extra_scope" {
				subject.Client.Scopes = append(subject.Client.Scopes, "transfer.task.create")
			}
			router := gin.New()
			router.Use(func(c *gin.Context) { setCanonicalAuthContext(c, subject); c.Next() })
			router.POST("/check", guard, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/check", nil))
			want := http.StatusForbidden
			if scenario == "valid" {
				want = http.StatusNoContent
			}
			if response.Code != want {
				t.Fatalf("status=%d want=%d body=%s", response.Code, want, response.Body)
			}
		})
	}
}

func testDelegatedAuthContext() commonauth.AuthContext {
	authContext := testCanonicalAuthContext()
	authContext.Client.Audiences = []string{"develop"}
	authContext.Client.ScopeMode = "restricted"
	authContext.Client.Scopes = []string{"workflow.run"}
	authContext.Authorization.RoleAssignments[0].Permissions = []string{"develop.task.execute"}
	authContext.Token.Type = "delegated_access_token"
	authContext.Delegation = &commonauth.DelegationFacts{
		DelegatedByClientID: "addp-web",
		AgentRunID:          "run-1",
		ToolCallID:          "call-1",
	}
	return authContext
}

func delegatedContextPointer() *commonauth.AuthContext {
	authContext := testDelegatedAuthContext()
	return &authContext
}
