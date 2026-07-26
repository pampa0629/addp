package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	commonauth "github.com/addp/common/authorization"
	commoni18n "github.com/addp/common/middleware/i18n"
	requestidmiddleware "github.com/addp/common/middleware/requestid"
	"github.com/gin-gonic/gin"
)

func TestCanonicalAuthContextMiddlewareResolvesAndInjectsDetachedContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var requestedMethod string
	var requestedPath string
	var authorizationHeader string
	var languageHeader string
	var requestIDHeader string
	var internalAPIKeyHeader string
	var cookieHeader string

	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestedMethod = request.Method
		requestedPath = request.URL.Path
		authorizationHeader = request.Header.Get("Authorization")
		languageHeader = request.Header.Get("Accept-Language")
		requestIDHeader = request.Header.Get(requestidmiddleware.RequestIDHeader)
		internalAPIKeyHeader = request.Header.Get("X-Internal-API-Key")
		cookieHeader = request.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(testCanonicalAuthContext())
	}))
	defer systemServer.Close()

	middleware, err := NewMiddleware(MiddlewareConfig{SystemURL: systemServer.URL})
	if err != nil {
		t.Fatalf("NewMiddleware() error = %v", err)
	}
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	router.Use(requestidmiddleware.RequestIDMiddleware())
	router.Use(middleware)
	router.GET("/resource", func(c *gin.Context) {
		authContext, exists := AuthContextFromGin(c)
		principal, principalExists := PrincipalFromGin(c)
		sessionContext, contextExists := AuthSessionContextFromGin(c)
		if !exists || !principalExists || !contextExists || principal.ID != "12" ||
			sessionContext.TenantID == nil || *sessionContext.TenantID != "3" {
			t.Fatalf("canonical helpers context=%#v principal=%#v session=%#v", authContext, principal, sessionContext)
		}
		for _, legacyKey := range []string{
			"user_id",
			"username",
			"tenant_id",
			"authorization_context",
		} {
			if _, exists := c.Get(legacyKey); exists {
				t.Fatalf("legacy Gin Context key %q was injected", legacyKey)
			}
		}
		authContext.Authorization.RoleAssignments[0].Permissions[0] = "changed.permission.read"
		again, _ := AuthContextFromGin(c)
		if again.Authorization.RoleAssignments[0].Permissions[0] != "manager.content.read" {
			t.Fatal("AuthContextFromGin returned shared mutable state")
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/resource", nil)
	request.Header.Set("Authorization", "bearer addp_at_access")
	request.Header.Set("Accept-Language", "en-US,en;q=0.9")
	request.Header.Set(requestidmiddleware.RequestIDHeader, "request-42")
	request.Header.Set("X-Internal-API-Key", "must-not-forward")
	request.AddCookie(&http.Cookie{Name: "secret", Value: "must-not-forward"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || requestedMethod != http.MethodGet ||
		requestedPath != "/api/v1/system/auth/context" ||
		authorizationHeader != "Bearer addp_at_access" ||
		languageHeader != "en-US,en;q=0.9" || requestIDHeader != "request-42" ||
		internalAPIKeyHeader != "" || cookieHeader != "" {
		t.Fatalf(
			"status=%d method=%q path=%q auth=%q language=%q request_id=%q internal=%q cookie=%q body=%s",
			response.Code,
			requestedMethod,
			requestedPath,
			authorizationHeader,
			languageHeader,
			requestIDHeader,
			internalAPIKeyHeader,
			cookieHeader,
			response.Body.String(),
		)
	}
}

func TestCanonicalAuthContextMiddlewareMapsAuthenticationAndServiceFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name       string
		header     string
		status     int
		body       string
		wantStatus int
		wantCode   string
		wantCalls  int64
	}{
		{
			name:       "missing bearer",
			status:     http.StatusOK,
			wantStatus: http.StatusUnauthorized,
			wantCode:   "authentication_required",
		},
		{
			name:       "malformed bearer",
			header:     "Bearer token extra",
			status:     http.StatusOK,
			wantStatus: http.StatusUnauthorized,
			wantCode:   "authentication_required",
		},
		{
			name:       "system rejects token",
			header:     "Bearer addp_at_invalid",
			status:     http.StatusUnauthorized,
			body:       `{"error":"secret token state"}`,
			wantStatus: http.StatusUnauthorized,
			wantCode:   "authentication_required",
			wantCalls:  1,
		},
		{
			name:       "unexpected forbidden",
			header:     "Bearer addp_at_access",
			status:     http.StatusForbidden,
			body:       `{"error":"secret authorization state"}`,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "authorization_service_unavailable",
			wantCalls:  1,
		},
		{
			name:       "system failure",
			header:     "Bearer addp_at_access",
			status:     http.StatusInternalServerError,
			body:       `{"error":"database password"}`,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "authorization_service_unavailable",
			wantCalls:  1,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var calls atomic.Int64
			systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(testCase.body))
			}))
			defer systemServer.Close()
			middleware, err := NewMiddleware(MiddlewareConfig{SystemURL: systemServer.URL})
			if err != nil {
				t.Fatalf("NewMiddleware() error = %v", err)
			}
			response := performCanonicalMiddlewareRequest(middleware, testCase.header, "en")
			if response.Code != testCase.wantStatus ||
				!strings.Contains(response.Body.String(), `"error_code":"`+testCase.wantCode+`"`) ||
				calls.Load() != testCase.wantCalls ||
				strings.Contains(response.Body.String(), "secret") ||
				strings.Contains(response.Body.String(), "database password") {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, calls.Load(), response.Body.String())
			}
			if testCase.wantStatus == http.StatusServiceUnavailable &&
				!strings.Contains(response.Body.String(), "Authorization service is temporarily unavailable") {
				t.Fatalf("service unavailable response is not localized: %s", response.Body.String())
			}
		})
	}
}

func TestCanonicalAuthContextMiddlewareRejectsInvalidOrOversizedSystemResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	valid, err := json.Marshal(testCanonicalAuthContext())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commonauth.DecodeAuthContext(strings.NewReader(string(valid))); err != nil {
		t.Fatalf("baseline AuthContext is invalid: %v", err)
	}
	withUnknown := strings.Replace(string(valid), `"schema_version":`, `"unknown":true,"schema_version":`, 1)
	for _, testCase := range []struct {
		name string
		body string
	}{
		{name: "unknown schema field", body: withUnknown},
		{name: "multiple documents", body: string(valid) + string(valid)},
		{name: "invalid JSON", body: `{"schema_version":`},
		{name: "oversized response", body: strings.Repeat("x", maxAuthContextResponseBytes+1)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(testCase.body))
			}))
			defer systemServer.Close()
			middleware, err := NewMiddleware(MiddlewareConfig{SystemURL: systemServer.URL})
			if err != nil {
				t.Fatalf("NewMiddleware() error = %v", err)
			}
			response := performCanonicalMiddlewareRequest(middleware, "Bearer addp_at_access", "zh-cn")
			if response.Code != http.StatusServiceUnavailable ||
				!strings.Contains(response.Body.String(), `"error_code":"authorization_service_unavailable"`) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestCanonicalAuthContextMiddlewareMapsTransportFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	middleware, err := NewMiddleware(MiddlewareConfig{
		SystemURL: "http://system.invalid",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial secret host failed")
		})},
	})
	if err != nil {
		t.Fatalf("NewMiddleware() error = %v", err)
	}
	response := performCanonicalMiddlewareRequest(middleware, "Bearer addp_at_access", "zh-cn")
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), `"error_code":"authorization_service_unavailable"`) ||
		strings.Contains(response.Body.String(), "secret host") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestCanonicalAuthContextMiddlewareDoesNotFollowRedirectsOrUseClientCookies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var redirectedCalls atomic.Int64
	redirectedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectedCalls.Add(1)
		_ = json.NewEncoder(w).Encode(testCanonicalAuthContext())
	}))
	defer redirectedServer.Close()

	var sourceCookie string
	sourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		sourceCookie = request.Header.Get("Cookie")
		http.Redirect(w, request, redirectedServer.URL, http.StatusFound)
	}))
	defer sourceServer.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	sourceURL, err := url.Parse(sourceServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(sourceURL, []*http.Cookie{{Name: "secret", Value: "must-not-forward"}})
	middleware, err := NewMiddleware(MiddlewareConfig{
		SystemURL:  sourceServer.URL,
		HTTPClient: &http.Client{Jar: jar},
	})
	if err != nil {
		t.Fatalf("NewMiddleware() error = %v", err)
	}
	response := performCanonicalMiddlewareRequest(middleware, "Bearer addp_at_access", "zh-cn")
	if response.Code != http.StatusServiceUnavailable || redirectedCalls.Load() != 0 || sourceCookie != "" ||
		!strings.Contains(response.Body.String(), `"error_code":"authorization_service_unavailable"`) {
		t.Fatalf(
			"status=%d redirected_calls=%d source_cookie=%q body=%s",
			response.Code,
			redirectedCalls.Load(),
			sourceCookie,
			response.Body.String(),
		)
	}
}

func TestCanonicalAuthContextMiddlewareDoesNotCacheAcrossRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls atomic.Int64
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(testCanonicalAuthContext())
	}))
	defer systemServer.Close()
	middleware, err := NewMiddleware(MiddlewareConfig{SystemURL: systemServer.URL})
	if err != nil {
		t.Fatalf("NewMiddleware() error = %v", err)
	}
	for range 2 {
		response := performCanonicalMiddlewareRequest(middleware, "Bearer addp_at_access", "zh-cn")
		if response.Code != http.StatusNoContent {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("System AuthContext calls = %d, want 2", calls.Load())
	}
}

func TestCanonicalAuthContextMiddlewareRejectsInvalidConfiguration(t *testing.T) {
	for _, systemURL := range []string{
		"",
		" http://system:8180",
		"system:8180",
		"ftp://system:8180",
		"http://user:password@system:8180",
		"http://system:8180?token=secret",
		"http://system:8180#fragment",
	} {
		if _, err := NewMiddleware(MiddlewareConfig{SystemURL: systemURL}); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("NewMiddleware(%q) error = %v", systemURL, err)
		}
	}
}

func TestCanonicalAuthContextRolePermissionHelpers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	authContext := testCanonicalAuthContext()
	tenantID := *authContext.Context.TenantID
	departmentID := "9"
	authContext.Authorization.RoleAssignments = append(
		authContext.Authorization.RoleAssignments,
		commonauth.RoleAssignment{
			AssignmentID: "403",
			RoleKey:      "tenant.data_steward",
			Scope: commonauth.AssignmentScope{
				Type:         "department",
				TenantID:     &tenantID,
				DepartmentID: &departmentID,
			},
			Permissions: []string{"manager.content.read", "manager.data_item.update"},
			SourceType:  "manual",
			ValidFrom:   authContext.Authorization.RoleAssignments[0].ValidFrom,
		},
		commonauth.RoleAssignment{
			AssignmentID: "404",
			RoleKey:      "tenant.data_viewer",
			Scope: commonauth.AssignmentScope{
				Type:         "department",
				TenantID:     &tenantID,
				DepartmentID: &departmentID,
			},
			Permissions: []string{"manager.content.read"},
			SourceType:  "manual",
			ValidFrom:   authContext.Authorization.RoleAssignments[0].ValidFrom,
		},
	)
	if err := commonauth.ValidateAuthContext(authContext); err != nil {
		t.Fatalf("test AuthContext is invalid: %v", err)
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	setCanonicalAuthContext(context, authContext)

	if !HasRolePermission(context, "manager.content.read") ||
		!HasAllRolePermissions(context, "manager.content.read", "manager.data_item.update") ||
		HasAllRolePermissions(context) ||
		HasRolePermission(context, "manager.content.delete") ||
		HasRolePermission(context, "manager.read") {
		t.Fatal("Role Permission helper result is incorrect")
	}
	scopes := RolePermissionScopes(context, "manager.content.read")
	if len(scopes) != 2 || scopes[0].Type != "tenant" || scopes[1].Type != "department" ||
		scopes[1].DepartmentID == nil || *scopes[1].DepartmentID != "9" {
		t.Fatalf("Role Permission scopes = %#v", scopes)
	}
	*scopes[0].TenantID = "99"
	again := RolePermissionScopes(context, "manager.content.read")
	if again[0].TenantID == nil || *again[0].TenantID != "3" {
		t.Fatal("RolePermissionScopes returned shared mutable state")
	}

	emptyContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	if _, exists := AuthContextFromGin(emptyContext); exists ||
		HasRolePermission(emptyContext, "manager.content.read") ||
		RolePermissionScopes(emptyContext, "manager.content.read") != nil {
		t.Fatal("helpers accepted a missing canonical AuthContext")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func performCanonicalMiddlewareRequest(
	middleware gin.HandlerFunc,
	authorizationHeader string,
	language string,
) *httptest.ResponseRecorder {
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	router.Use(middleware)
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/resource", nil)
	if authorizationHeader != "" {
		request.Header.Set("Authorization", authorizationHeader)
	}
	if language != "" {
		request.Header.Set("Accept-Language", language)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func testCanonicalAuthContext() commonauth.AuthContext {
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
			Departments: []commonauth.DepartmentMembership{{
				MembershipID:   "71",
				DepartmentID:   "9",
				MembershipType: "primary",
				RelationRole:   "member",
				AncestorIDs:    []string{"4"},
			}},
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
