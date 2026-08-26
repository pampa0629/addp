package api

import (
	"testing"

	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/modulelifecycle"
	standardauthorization "github.com/addp/standard/internal/authorization"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAssetDiscoverableRouteAuthenticationAndTenantContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatalf("attach standard schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE standard.metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL, code TEXT NOT NULL, type TEXT NOT NULL,
			definition TEXT, formula TEXT, status TEXT, tags JSON,
			created_by INTEGER NOT NULL, created_at DATETIME, updated_at DATETIME,
			version INTEGER NOT NULL DEFAULT 1
		)
	`).Error; err != nil {
		t.Fatalf("create standard.metrics: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO standard.metrics (tenant_id, name, code, type, definition, status, created_by) VALUES
			(7, 'tenant-seven', 'seven', 'atomic', 'seven', 'approved', 1),
			(8, 'tenant-eight', 'eight', 'atomic', 'eight', 'approved', 1)
	`).Error; err != nil {
		t.Fatalf("seed standard.metrics: %v", err)
	}

	systemServer := authtest.NewTenantAuthContextServer(t, "7", standardauthorization.PermissionStandardMetricRead)
	defer systemServer.Close()
	router := SetupRouter(db, nil, nil, nil, nil, nil, nil, nil, nil, nil, systemServer.URL, modulelifecycle.NewStandalone("standard"))

	authtest.AssertAssetDiscoverableContract(t, router, "/api/v1/standard/assets/discoverable", "tenant-seven")
}
