package service

import (
	"testing"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestApplicationServiceEnforcesTenantOwnership(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:application-tenant?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS system").Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE system.applications (
			id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, description TEXT,
			tenant_id INTEGER NOT NULL, allowed_services TEXT, rate_limit_per_minute INTEGER,
			status TEXT, created_by INTEGER, created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
		)`,
		`CREATE TABLE system.api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT, application_id INTEGER NOT NULL,
			key_prefix TEXT, key_hash TEXT UNIQUE, name TEXT, last_used_at DATETIME,
			expires_at DATETIME, status TEXT, created_by INTEGER, created_at DATETIME,
			revoked_at DATETIME, revoked_by INTEGER
		)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	service := NewApplicationService(repository.NewApplicationRepository(db))
	created, err := service.CreateApplication(&models.CreateApplicationRequest{Name: "tenant-one"}, 1, 11)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.GetApplication(created.ID, 2); err == nil {
		t.Fatal("cross-tenant application read succeeded")
	}
	if _, err := service.UpdateApplication(created.ID, 2, &models.UpdateApplicationRequest{Name: "changed"}); err == nil {
		t.Fatal("cross-tenant application update succeeded")
	}
	if err := service.DeleteApplication(created.ID, 2); err == nil {
		t.Fatal("cross-tenant application delete succeeded")
	}
	if _, err := service.GenerateAPIKey(created.ID, 2, &models.CreateAPIKeyRequest{Name: "wrong-tenant"}, 22); err == nil {
		t.Fatal("cross-tenant API key creation succeeded")
	}
	if _, err := service.ListAPIKeys(created.ID, 2); err == nil {
		t.Fatal("cross-tenant API key listing succeeded")
	}

	loaded, err := service.GetApplication(created.ID, 1)
	if err != nil || loaded.Name != "tenant-one" || loaded.DeletedAt != nil {
		t.Fatalf("tenant application = %#v, %v", loaded, err)
	}
	key, err := service.GenerateAPIKey(created.ID, 1, &models.CreateAPIKeyRequest{Name: "tenant-key"}, 11)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.RevokeAPIKey(created.ID, key.ID, 2, 22); err == nil {
		t.Fatal("cross-tenant API key revocation succeeded")
	}
	if err := service.RevokeAPIKey(created.ID, key.ID, 1, 11); err != nil {
		t.Fatalf("tenant API key revocation error = %v", err)
	}
}
