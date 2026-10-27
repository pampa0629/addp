package repository_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/nfs"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/internal/service"
	"github.com/addp/system/internal/testsupport"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type capabilityRefreshNFSFixture struct {
	*nfs.NFSPlugin
	rangeRead bool
}

func (p *capabilityRefreshNFSFixture) Type() string { return "cap_refresh_nfs_fixture" }
func (p *capabilityRefreshNFSFixture) TestConnection(context.Context, engineplugin.ConnectionInfo) error {
	return nil
}
func (p *capabilityRefreshNFSFixture) Capabilities() engineplugin.EngineCapabilities {
	caps := engineplugin.NewFileCapabilities(p.Type())
	caps.Storage.Store.RangeRead = p.rangeRead
	return caps
}

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

	t.Run("startup refresh keeps JSONB-equivalent source version", func(t *testing.T) {
		fixture := &capabilityRefreshNFSFixture{NFSPlugin: &nfs.NFSPlugin{}, rangeRead: true}
		engineplugin.Register(fixture)
		t.Cleanup(func() { engineplugin.Unregister(fixture.Type()) })
		encoded, err := engineplugin.MarshalEngineCapabilities(fixture.Capabilities())
		if err != nil {
			t.Fatal(err)
		}
		declaration := models.JSONString(encoded)
		source := &models.Engine{Name: t.Name(), EngineType: fixture.Type(), EngineOrigin: "general", Version: 236,
			LifecycleState: models.EngineLifecycleActive, ConnectionStatus: models.EngineConnectionOnline,
			ConnectionInfo: models.ConnectionInfo{}, Capabilities: &declaration}
		if err := repo.Create(source); err != nil {
			t.Fatal(err)
		}
		before, err := repo.GetByID(source.ID)
		if err != nil {
			t.Fatal(err)
		}
		if string(*before.Capabilities) == encoded {
			t.Fatal("fixture must exercise PostgreSQL JSONB serialization")
		}
		svc := service.NewEngineService(repo, nil, nil)
		for i := 0; i < 2; i++ {
			if !svc.CheckAndUpdateConnectionStatus(source.ID) {
				t.Fatal("startup/explicit refresh failed")
			}
			current, err := repo.GetByID(source.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Version != before.Version || !current.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("unchanged capabilities invalidated source version: before=%d after=%d", before.Version, current.Version)
			}
			if current.LastCheckAt == nil || current.ConnectionStatus != models.EngineConnectionOnline {
				t.Fatal("refresh did not persist connectivity observation")
			}
		}
		fixture.rangeRead = false
		if !svc.CheckAndUpdateConnectionStatus(source.ID) {
			t.Fatal("changed capability refresh failed")
		}
		changed, err := repo.GetByID(source.ID)
		if err != nil || changed.Version != before.Version+1 {
			t.Fatalf("real capability change must advance version: %v", err)
		}
		if !svc.CheckAndUpdateConnectionStatus(source.ID) {
			t.Fatal("repeat refresh failed")
		}
		repeated, err := repo.GetByID(source.ID)
		if err != nil || repeated.Version != changed.Version {
			t.Fatalf("repeated changed declaration advanced version: %v", err)
		}
	})
}
