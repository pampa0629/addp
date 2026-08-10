package api

import (
	"testing"

	"github.com/addp/common/authorization/authtest"
	developauthorization "github.com/addp/develop/backend/internal/authorization"
	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/repository"
	developservice "github.com/addp/develop/backend/internal/service"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAssetDiscoverableRouteAuthenticationAndTenantContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatalf("attach develop schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE develop.dev_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL, display_name TEXT, dev_type TEXT NOT NULL,
			content JSON, execution_config JSON, editor_layout JSON, timeout INTEGER,
			description TEXT, tags TEXT, created_by INTEGER, updated_by INTEGER,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME, status TEXT,
			last_execution_id TEXT, last_execution_status TEXT, last_run_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create develop.dev_tasks: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO develop.dev_tasks (tenant_id, name, display_name, dev_type, status) VALUES
			(7, 'seven', 'tenant-seven', 'query', 'active'),
			(8, 'eight', 'tenant-eight', 'query', 'active')
	`).Error; err != nil {
		t.Fatalf("seed develop.dev_tasks: %v", err)
	}

	systemServer := authtest.NewTenantAuthContextServer(t, "7", developauthorization.PermissionDevelopTaskRead)
	defer systemServer.Close()
	cfg := &config.Config{}
	cfg.SystemServiceURL = systemServer.URL
	devTaskService := developservice.NewDevTaskService(repository.NewDevTaskRepository(db), nil)
	router := SetupRouter(cfg, db, nil, nil, nil, nil, nil, nil, nil, devTaskService, nil)

	authtest.AssertAssetDiscoverableContract(t, router, "/api/v1/develop/assets/discoverable", "tenant-seven")
}
