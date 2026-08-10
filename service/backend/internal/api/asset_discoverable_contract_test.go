package api

import (
	"testing"

	"github.com/addp/common/authorization/authtest"
	serviceauthorization "github.com/addp/service/internal/authorization"
	"github.com/addp/service/internal/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAssetDiscoverableRouteAuthenticationAndTenantContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS service").Error; err != nil {
		t.Fatalf("attach service schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE service.query_services (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			service_name TEXT NOT NULL, title TEXT NOT NULL, description TEXT,
			status TEXT, created_at DATETIME, updated_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create service.query_services: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO service.query_services (tenant_id, service_name, title, status) VALUES
			(7, 'seven', 'tenant-seven', 'active'),
			(8, 'eight', 'tenant-eight', 'active')
	`).Error; err != nil {
		t.Fatalf("seed service.query_services: %v", err)
	}

	systemServer := authtest.NewTenantAuthContextServer(t, "7", serviceauthorization.PermissionServiceDefinitionRead)
	defer systemServer.Close()
	cfg := &config.Config{}
	cfg.SystemServiceURL = systemServer.URL
	router := SetupRouter(cfg, db, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	authtest.AssertAssetDiscoverableContract(t, router, "/api/v1/service/assets/discoverable", "tenant-seven")
}
