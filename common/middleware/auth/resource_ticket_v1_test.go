package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestResourceTicketMiddlewareResolvesEveryRequestAndInjectsCanonicalContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 24, 8, 1, 0, 0, time.UTC)
	var calls atomic.Int64
	var authorizationHeader string
	var cookieHeader string
	var internalAPIKeyHeader string
	var languageHeader string
	var requestIDHeader string
	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		authorizationHeader = request.Header.Get("Authorization")
		cookieHeader = request.Header.Get("Cookie")
		internalAPIKeyHeader = request.Header.Get("X-Internal-API-Key")
		languageHeader = request.Header.Get("Accept-Language")
		requestIDHeader = request.Header.Get(requestidmiddleware.RequestIDHeader)
		_ = json.NewEncoder(w).Encode(testResourceTicketAuthContext("manager"))
	}))
	defer systemServer.Close()

	middleware, err := NewResourceTicketMiddleware(ResourceTicketMiddlewareConfig{
		SystemURL:           systemServer.URL,
		Owner:               "manager",
		RequiredPermissions: []string{"manager.data_item.read", "manager.content.read"},
		Now:                 func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewResourceTicketMiddleware() error = %v", err)
	}
	router := gin.New()
	router.Use(commoni18n.I18nMiddleware())
	router.Use(requestidmiddleware.RequestIDMiddleware())
	router.GET("/native", middleware, func(c *gin.Context) {
		authContext, exists := AuthContextFromGin(c)
		if !exists || authContext.Token.Type != "resource_access_ticket" ||
			authContext.Client.Audiences[0] != "manager" {
			t.Fatalf("injected AuthContext = %#v", authContext)
		}
		c.Status(http.StatusNoContent)
	})

	for range 2 {
		request := httptest.NewRequest(http.MethodGet, "/native", nil)
		request.AddCookie(&http.Cookie{Name: BrowserResourceAccessTicketCookieName, Value: "addp_rat_manager"})
		request.AddCookie(&http.Cookie{Name: "other_cookie", Value: "must-not-forward"})
		request.Header.Set("Accept-Language", "en")
		request.Header.Set(requestidmiddleware.RequestIDHeader, "resource-request")
		request.Header.Set("X-Internal-API-Key", "must-not-forward")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("resource status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if calls.Load() != 2 || authorizationHeader != "Bearer addp_rat_manager" || cookieHeader != "" ||
		internalAPIKeyHeader != "" || languageHeader != "en" || requestIDHeader != "resource-request" {
		t.Fatalf("calls=%d auth=%q cookie=%q internal=%q language=%q request_id=%q",
			calls.Load(), authorizationHeader, cookieHeader, internalAPIKeyHeader, languageHeader, requestIDHeader)
	}
}

func TestResourceTicketMiddlewareRejectsCredentialRouteAndPermissionFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 7, 24, 8, 1, 0, 0, time.UTC)
	for _, testCase := range []struct {
		name          string
		method        string
		cookies       []string
		authorization *string
		systemStatus  int
		context       commonauth.AuthContext
		permissions   []string
		wantStatus    int
		wantCode      string
		wantCalls     int64
	}{
		{name: "non resource method", method: http.MethodPost, cookies: []string{"addp_rat_manager"}, systemStatus: http.StatusOK, context: testResourceTicketAuthContext("manager"), permissions: []string{"manager.content.read"}, wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
		{name: "missing cookie", method: http.MethodGet, systemStatus: http.StatusOK, context: testResourceTicketAuthContext("manager"), permissions: []string{"manager.content.read"}, wantStatus: http.StatusUnauthorized, wantCode: "authentication_required"},
		{name: "duplicate cookie", method: http.MethodGet, cookies: []string{"addp_rat_manager", "addp_rat_other"}, systemStatus: http.StatusOK, context: testResourceTicketAuthContext("manager"), permissions: []string{"manager.content.read"}, wantStatus: http.StatusUnauthorized, wantCode: "authentication_required"},
		{name: "authorization header", method: http.MethodGet, cookies: []string{"addp_rat_manager"}, authorization: stringTestPointer(""), systemStatus: http.StatusOK, context: testResourceTicketAuthContext("manager"), permissions: []string{"manager.content.read"}, wantStatus: http.StatusUnauthorized, wantCode: "authentication_required"},
		{name: "system rejects ticket", method: http.MethodGet, cookies: []string{"addp_rat_manager"}, systemStatus: http.StatusUnauthorized, context: testResourceTicketAuthContext("manager"), permissions: []string{"manager.content.read"}, wantStatus: http.StatusUnauthorized, wantCode: "authentication_required", wantCalls: 1},
		{name: "system unavailable", method: http.MethodGet, cookies: []string{"addp_rat_manager"}, systemStatus: http.StatusInternalServerError, context: testResourceTicketAuthContext("manager"), permissions: []string{"manager.content.read"}, wantStatus: http.StatusServiceUnavailable, wantCode: "authorization_service_unavailable", wantCalls: 1},
		{name: "wrong owner", method: http.MethodGet, cookies: []string{"addp_rat_manager"}, systemStatus: http.StatusOK, context: testResourceTicketAuthContext("standard"), permissions: []string{"manager.content.read"}, wantStatus: http.StatusUnauthorized, wantCode: "authentication_required", wantCalls: 1},
		{name: "permission denied", method: http.MethodGet, cookies: []string{"addp_rat_manager"}, systemStatus: http.StatusOK, context: testResourceTicketAuthContext("manager"), permissions: []string{"manager.content.write"}, wantStatus: http.StatusForbidden, wantCode: "permission_denied", wantCalls: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var calls atomic.Int64
			systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(testCase.systemStatus)
				if testCase.systemStatus == http.StatusOK {
					_ = json.NewEncoder(w).Encode(testCase.context)
				} else {
					_, _ = w.Write([]byte(`{"error":"secret downstream state"}`))
				}
			}))
			defer systemServer.Close()
			middleware, err := NewResourceTicketMiddleware(ResourceTicketMiddlewareConfig{
				SystemURL:           systemServer.URL,
				Owner:               "manager",
				RequiredPermissions: testCase.permissions,
				Now:                 func() time.Time { return now },
			})
			if err != nil {
				t.Fatalf("NewResourceTicketMiddleware() error = %v", err)
			}
			router := gin.New()
			router.Use(commoni18n.I18nMiddleware())
			router.Any("/native", middleware, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			request := httptest.NewRequest(testCase.method, "/native", nil)
			for _, value := range testCase.cookies {
				request.AddCookie(&http.Cookie{Name: BrowserResourceAccessTicketCookieName, Value: value})
			}
			if testCase.authorization != nil {
				request.Header["Authorization"] = []string{*testCase.authorization}
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus || calls.Load() != testCase.wantCalls ||
				!strings.Contains(response.Body.String(), `"error_code":"`+testCase.wantCode+`"`) ||
				strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, calls.Load(), response.Body.String())
			}
		})
	}
}

func TestResourceTicketMiddlewareValidatesConfiguration(t *testing.T) {
	for _, config := range []ResourceTicketMiddlewareConfig{
		{SystemURL: "http://system", Owner: "Manager", RequiredPermissions: []string{"manager.content.read"}},
		{SystemURL: "http://system", Owner: "manager"},
		{SystemURL: "http://system", Owner: "manager", RequiredPermissions: []string{"invalid"}},
		{SystemURL: "http://system", Owner: "manager", RequiredPermissions: []string{"manager.content.read", "manager.content.read"}},
	} {
		if _, err := NewResourceTicketMiddleware(config); !errors.Is(err, commonapi.ErrBadRequest) {
			t.Fatalf("NewResourceTicketMiddleware(%#v) error = %v, want bad request", config, err)
		}
	}
}

func testResourceTicketAuthContext(owner string) commonauth.AuthContext {
	authContext := testCanonicalAuthContext()
	authContext.Client.Audiences = []string{owner}
	authContext.Client.ScopeMode = "restricted"
	authContext.Client.Scopes = []string{commonauth.BrowserResourceAccessScope}
	authContext.Token.Type = "resource_access_ticket"
	return authContext
}

func stringTestPointer(value string) *string {
	return &value
}
