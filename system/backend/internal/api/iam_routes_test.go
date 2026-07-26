package api

import (
	"net/http"
	"sort"
	"testing"

	"github.com/addp/system/internal/config"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRegisterIAMRoutesExposesOnlyTargetIAMSurface(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:iam-routes?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := testIAMRuntimeConfig()
	cfg.OAuthPublicRateLimitPerMinute = 60
	cfg.OAuthUserRateLimitPerMinute = 30
	runtime, err := NewIAMRuntime(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	api := router.Group("/api/v1/system")
	if err := RegisterIAMRoutes(api, runtime, nil, cfg); err != nil {
		t.Fatalf("RegisterIAMRoutes() error = %v", err)
	}

	actual := make([]string, 0)
	for _, route := range router.Routes() {
		actual = append(actual, route.Method+" "+route.Path)
	}
	sort.Strings(actual)
	want := []string{
		http.MethodDelete + " /api/v1/system/oauth/authorization_requests/:request_id",
		http.MethodGet + " /api/v1/system/auth/context",
		http.MethodGet + " /api/v1/system/auth/context-options",
		http.MethodGet + " /api/v1/system/oauth/authorization_requests/:request_id",
		http.MethodGet + " /api/v1/system/users/me",
		http.MethodPost + " /api/v1/system/auth/context-selections",
		http.MethodPost + " /api/v1/system/auth/context-switches",
		http.MethodPost + " /api/v1/system/auth/delegations",
		http.MethodPost + " /api/v1/system/auth/mfa-verifications",
		http.MethodPost + " /api/v1/system/login",
		http.MethodPost + " /api/v1/system/logout",
		http.MethodPost + " /api/v1/system/oauth/authorization_requests",
		http.MethodPost + " /api/v1/system/oauth/authorizations",
		http.MethodPost + " /api/v1/system/oauth/device/authorizations",
		http.MethodPost + " /api/v1/system/oauth/device/code",
		http.MethodPost + " /api/v1/system/oauth/revoke",
		http.MethodPost + " /api/v1/system/oauth/token",
		http.MethodPost + " /api/v1/system/refresh",
		http.MethodPost + " /api/v1/system/tenant/invitations/acceptances",
		http.MethodPost + " /api/v1/system/tenant/invitations/enrollments",
		http.MethodPost + " /api/v1/system/tenant/invitations/registrations",
		http.MethodPut + " /api/v1/system/users/me/password",
	}
	sort.Strings(want)
	if len(actual) != len(want) {
		t.Fatalf("route count = %d, want %d: %#v", len(actual), len(want), actual)
	}
	for index := range want {
		if actual[index] != want[index] {
			t.Fatalf("routes = %#v, want %#v", actual, want)
		}
	}
	for _, forbidden := range []string{
		"POST /api/v1/system/register",
		"POST /api/v1/system/oauth/authorize",
	} {
		for _, route := range actual {
			if route == forbidden {
				t.Fatalf("legacy route %q was registered", forbidden)
			}
		}
	}
}

func TestRegisterIAMRoutesRejectsIncompleteComposition(t *testing.T) {
	router := gin.New()
	api := router.Group("/api/v1/system")
	cfg := &config.Config{OAuthPublicRateLimitPerMinute: 60, OAuthUserRateLimitPerMinute: 30}
	if err := RegisterIAMRoutes(api, nil, nil, cfg); err == nil {
		t.Fatal("RegisterIAMRoutes() accepted nil runtime")
	}
}
