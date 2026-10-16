package repository_test

import (
	"errors"
	"os"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestEngineCapabilityRefreshPreservesObservationAgainstPostgres(t *testing.T) {
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
	repo := repository.NewEngineRepository(tx)
	caps := engineplugin.NewWorkflowCapabilities("cap_refresh_fixture", "addp.workflow/v1")
	initial, err := engineplugin.MarshalEngineCapabilities(caps)
	if err != nil {
		t.Fatal(err)
	}
	raw := models.JSONString(initial)
	instance := &models.Engine{Name: t.Name(), EngineType: "cap_refresh_fixture", EngineOrigin: "extension", LifecycleState: models.EngineLifecycleDisabled, Version: 1, ConnectionInfo: models.ConnectionInfo{"host": "localhost", "port": 18098, "protocol": "http"}, Capabilities: &raw}
	if err := repo.Create(instance); err != nil {
		t.Fatal(err)
	}
	stale, err := repo.GetByID(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	checkedAt := time.Now().UTC().Truncate(time.Microsecond)
	if updated, err := repo.UpdateConnectionObservation(instance.ID, models.EngineConnectionOnline, checkedAt, "fresh observation", &stale.IdentityKey, "HighGo fixture 4.5"); err != nil || !updated {
		t.Fatalf("observation: %v updated=%v", err, updated)
	}
	if observed, err := repo.GetByID(instance.ID); err != nil || observed.Version != stale.Version || !observed.UpdatedAt.Equal(stale.UpdatedAt) {
		t.Fatalf("probe advanced administrative revision: %+v err=%v", observed, err)
	}
	caps.Compute.Workflow.StorageAccess = &engineplugin.WorkflowStorageAccessCapability{ReadEngineTypes: []string{"minio"}, WriteEngineTypes: []string{"minio"}}
	encoded, _ := engineplugin.MarshalEngineCapabilities(caps)
	updatedRaw := models.JSONString(encoded)
	stale.Capabilities = &updatedRaw
	if err := repo.UpdateCapabilities(stale); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.GetByID(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != instance.ID || stored.Version != 2 || stored.LifecycleState != models.EngineLifecycleDisabled || stored.ConnectionStatus != models.EngineConnectionOnline || stored.CheckMessage != "fresh observation" || stored.LastCheckAt == nil || !stored.LastCheckAt.Equal(checkedAt) || stored.ServerVersion != "HighGo fixture 4.5" || stored.ServerVersionCheckedAt == nil || !stored.ServerVersionCheckedAt.Equal(checkedAt) {
		t.Fatalf("capability refresh overwrote instance or observation: %+v", stored)
	}
	parsed, err := engineplugin.ParseEngineCapabilities(string(*stored.Capabilities))
	if err != nil || !parsed.Compute.Workflow.StorageAccess.Supports("minio", "write") {
		t.Fatalf("JSONB declaration did not round trip: %v", err)
	}
	// An offline probe must retain the time of the last successful version observation.
	if updated, err := repo.UpdateConnectionObservation(instance.ID, models.EngineConnectionOffline, checkedAt.Add(time.Minute), "offline", nil, "untrusted failed result"); err != nil || !updated {
		t.Fatal(updated, err)
	}
	oldVersion := *stored
	stored.LifecycleState = models.EngineLifecycleActive
	stored.ServerVersion = "spoofed edit"
	stored.ServerVersionCheckedAt = nil
	if err := repo.Update(stored); err != nil {
		t.Fatal(err)
	}
	fresh, err := repo.GetByID(instance.ID)
	if err != nil || fresh.ServerVersion != "HighGo fixture 4.5" || fresh.ServerVersionCheckedAt == nil || !fresh.ServerVersionCheckedAt.Equal(checkedAt) {
		t.Fatalf("administrative update overwrote native version: %+v err=%v", fresh, err)
	}
	staleKey := models.JSONString(`{"host":"old-host"}`)
	if applied, err := repo.UpdateConnectionObservation(instance.ID, models.EngineConnectionOnline, checkedAt.Add(2*time.Minute), "stale", &staleKey, "stale version"); err != nil || applied {
		t.Fatal("stale address probe was accepted", applied, err)
	}
	if err := repo.UpdateCapabilities(&oldVersion); !errors.Is(err, repository.ErrEngineVersionConflict) {
		t.Fatalf("stale capability writer ignored an administrative update: %v", err)
	}
}
