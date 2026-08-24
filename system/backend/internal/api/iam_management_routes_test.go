package api

import (
	"sort"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRegisterIAMManagementRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:iam-management-routes?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewIAMRuntime(db, testIAMRuntimeConfig(), testIAMSecurityPolicy())
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	api := router.Group("/api/v1/system")
	if err := RegisterIAMManagementRoutes(api, runtime, NewModuleRegistryHandler(nil)); err != nil {
		t.Fatalf("RegisterIAMManagementRoutes() error = %v", err)
	}

	actual := make([]string, 0, len(router.Routes()))
	for _, route := range router.Routes() {
		actual = append(actual, route.Method+" "+route.Path)
	}
	sort.Strings(actual)
	want := []string{
		"GET /api/v1/system/platform/audit/events",
		"GET /api/v1/system/platform/audit/events/:id",
		"GET /api/v1/system/platform/audit/events/export",
		"GET /api/v1/system/platform/audit/events/summary",
		"GET /api/v1/system/platform/audit/events/trends",
		"GET /api/v1/system/platform/identity_changes",
		"GET /api/v1/system/platform/identity_changes/:id",
		"GET /api/v1/system/platform/modules",
		"GET /api/v1/system/platform/modules/:module_name",
		"GET /api/v1/system/platform/modules/:module_name/instances",
		"GET /api/v1/system/platform/security_policy",
		"GET /api/v1/system/platform/tenant_administrator_candidates",
		"GET /api/v1/system/platform/tenants",
		"GET /api/v1/system/platform/tenants/:id",
		"GET /api/v1/system/platform/users",
		"GET /api/v1/system/platform/users/:id",
		"GET /api/v1/system/tenant/audit/events",
		"GET /api/v1/system/tenant/audit/events/:id",
		"GET /api/v1/system/tenant/audit/events/export",
		"GET /api/v1/system/tenant/audit/events/summary",
		"GET /api/v1/system/tenant/audit/events/trends",
		"GET /api/v1/system/tenant/memberships",
		"GET /api/v1/system/tenant/memberships/:id",
		"GET /api/v1/system/tenant/role_assignments",
		"GET /api/v1/system/tenant/role_permissions",
		"GET /api/v1/system/tenant/roles",
		"GET /api/v1/system/tenant/invitations",
		"GET /api/v1/system/tenant/invitations/:id",
		"POST /api/v1/system/platform/tenants",
		"POST /api/v1/system/platform/identity_changes",
		"POST /api/v1/system/platform/identity_changes/:id/approve",
		"POST /api/v1/system/platform/identity_changes/:id/reject",
		"POST /api/v1/system/platform/tenants/:id/close",
		"POST /api/v1/system/platform/tenants/:id/initialization",
		"POST /api/v1/system/platform/tenants/:id/restore",
		"POST /api/v1/system/platform/tenants/:id/suspend",
		"POST /api/v1/system/platform/users",
		"POST /api/v1/system/platform/users/:id/reset-password",
		"POST /api/v1/system/platform/users/:id/reset-mfa",
		"POST /api/v1/system/platform/users/:id/reactivate",
		"POST /api/v1/system/platform/users/:id/suspend",
		"POST /api/v1/system/tenant/memberships/:id/close",
		"POST /api/v1/system/tenant/memberships/:id/restore",
		"POST /api/v1/system/tenant/memberships/:id/suspend",
		"POST /api/v1/system/tenant/invitations",
		"POST /api/v1/system/tenant/invitations/:id/revoke",
		"POST /api/v1/system/tenant/role_assignments",
		"POST /api/v1/system/tenant/role_assignments/:id/revoke",
		"POST /api/v1/system/tenant/roles",
		"DELETE /api/v1/system/tenant/roles/:id",
		"PUT /api/v1/system/platform/tenants/:id",
		"PUT /api/v1/system/platform/security_policy",
		"PUT /api/v1/system/platform/modules/:module_name",
		"PUT /api/v1/system/platform/users/:id",
		"PUT /api/v1/system/tenant/memberships/:id",
		"PUT /api/v1/system/tenant/roles/:id",
	}
	sort.Strings(want)
	if len(actual) != len(want) {
		t.Fatalf("routes = %#v, want %#v", actual, want)
	}
	for index := range want {
		if actual[index] != want[index] {
			t.Fatalf("routes = %#v, want %#v", actual, want)
		}
	}
}
