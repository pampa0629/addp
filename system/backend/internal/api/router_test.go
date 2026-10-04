package api

import (
	"testing"
	"time"

	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSetupRouterUsesOnlyTargetIAMSurface(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// ATTACH is connection-local; the delayed deletion scan uses this pool too.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Exec("ATTACH DATABASE ':memory:' AS system").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&iam.SecurityPolicy{}, &models.Engine{}); err != nil {
		t.Fatal(err)
	}
	policy := iam.DefaultSecurityPolicy()
	if err := db.Create(&policy).Error; err != nil {
		t.Fatal(err)
	}
	cfg := testIAMRuntimeConfig()
	waitForRouterDeletionRecovery(t, db)
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
		"POST /api/v1/system/engine-access/read-checks/manager-preview",
		"GET /api/v1/system/engine-types",
		"POST /api/v1/system/runtime/modules",
		"POST /api/v1/system/runtime/modules/heartbeat",
		"POST /api/v1/system/runtime/engines",
		"GET /api/v1/system/runtime/tenants",
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

// SetupRouter starts a delayed recovery scan. These fixtures have no deleting
// engines, so this query is its final database operation.
func waitForRouterDeletionRecovery(t *testing.T, db *gorm.DB) {
	t.Helper()
	done := make(chan error, 1)
	if err := db.Callback().Query().After("gorm:query").Register("test:router_deletion_recovery", func(tx *gorm.DB) {
		if tx.Statement.Table == "engines" {
			done <- tx.Error
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("router deletion recovery: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("router deletion recovery did not finish")
		}
	})
}
