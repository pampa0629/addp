package repository_test

import (
	"errors"
	"os"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestEngineCredentialUpdateAfterJSONBAddressAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS system").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.Engine{}); err != nil {
		t.Fatal(err)
	}
	const kind = "jsonb_address_fixture"
	connection := models.ConnectionInfo{"protocol": "http", "host": "runtime.internal", "port": 8080, "user": "sysdba"}
	key, err := engineplugin.BuildConnectionIdentityKey(kind, engineplugin.ConnectionInfo(connection))
	if err != nil {
		t.Fatal(err)
	}
	caps, err := engineplugin.MarshalEngineCapabilities(engineplugin.NewWorkflowCapabilities(kind, "addp.workflow/v1"))
	if err != nil {
		t.Fatal(err)
	}
	raw := models.JSONString(caps)
	tenantID := uint(7)
	instance := &models.Engine{TenantID: &tenantID, Name: t.Name(), EngineType: kind, EngineOrigin: "extension", LifecycleState: models.EngineLifecycleActive,
		Version: 1, ConnectionInfo: connection, IdentityKey: models.JSONString(key), Capabilities: &raw, ConnectionStatus: models.EngineConnectionOnline}
	repo := repository.NewEngineRepository(tx)
	if err := repo.Create(instance); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetByID(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.IdentityKey == models.JSONString(key) {
		t.Fatal("fixture did not exercise JSONB text normalization")
	}
	svc := service.NewEngineService(repo, nil, nil)
	connection["user"] = "business"
	updated, err := svc.Update(stored.ID, tenantID, &models.EngineUpdateRequest{Version: stored.Version, ConnectionInfo: &connection})
	if err != nil {
		t.Fatalf("credential change on same physical address: %v", err)
	}
	if updated.ID != stored.ID || updated.ConnectionStatus != models.EngineConnectionOnline || updated.ConnectionInfo["user"] != "business" {
		t.Fatalf("credential update changed identity or observation: %#v", updated)
	}
	connection["port"] = 8081
	if _, err := svc.Update(updated.ID, tenantID, &models.EngineUpdateRequest{Version: updated.Version, ConnectionInfo: &connection}); !errors.Is(err, service.ErrEngineRelocationUnconfirmed) {
		t.Fatalf("actual move must still require confirmation: %v", err)
	}
}
