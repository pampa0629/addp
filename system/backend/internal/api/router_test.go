package api

import (
	"testing"

	"github.com/addp/system/internal/iam"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSetupRouterUsesOnlyTargetIAMSurface(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:target-system-router?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS system").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&iam.SecurityPolicy{}); err != nil {
		t.Fatal(err)
	}
	policy := iam.DefaultSecurityPolicy()
	if err := db.Create(&policy).Error; err != nil {
		t.Fatal(err)
	}
	cfg := testIAMRuntimeConfig()
	router := SetupRouter(db, cfg)
	routes := make(map[string]struct{})
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}
	for _, required := range []string{
		"POST /api/v1/system/login",
		"GET /api/v1/system/platform/tenants",
		"GET /api/v1/system/platform/security_policy",
		"GET /api/v1/system/tenant/invitations",
		"POST /api/v1/system/tenant/invitations/registrations",
		"GET /api/v1/system/engines",
		"POST /api/v1/system/runtime/modules",
		"POST /api/v1/system/runtime/modules/heartbeat",
		"POST /api/v1/system/runtime/engines",
		"GET /api/v1/system/runtime/engine-descriptors",
		"GET /api/v1/system/runtime/engine-descriptors/:id",
		"POST /api/v1/system/tenant/audit/events",
	} {
		if _, exists := routes[required]; !exists {
			t.Fatalf("target route %q is missing", required)
		}
	}
	for _, forbidden := range []string{
		"POST /api/v1/system/register",
		"GET /api/v1/system/users",
		"GET /api/v1/system/tenants",
		"GET /api/v1/system/logs",
		"POST /api/v1/system/oauth/authorize",
		"GET /api/v1/internal/engines",
		"POST /api/v1/internal/audit-logs",
	} {
		if _, exists := routes[forbidden]; exists {
			t.Fatalf("legacy route %q is still registered", forbidden)
		}
	}
}
