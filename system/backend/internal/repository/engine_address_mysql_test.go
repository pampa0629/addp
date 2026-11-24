package repository_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/gorm"
)

func TestEngineAddressObservationAgainstMySQL(t *testing.T) {
	if os.Getenv("ADDP_TEST_MYSQL_ENGINE_OBSERVATION") != "1" {
		t.Skip("requires a disposable MySQL System installation")
	}
	db, err := bootstrap.OpenPlatformRuntime(config.LoadPlatformDatabase(), "system", &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	repo := repository.NewEngineRepository(tx)
	engine := &models.Engine{Name: t.Name(), EngineType: "postgresql", ConnectionInfo: models.ConnectionInfo{"host": "probe.source", "port": 5432, "database": "orders"}}
	if err := repo.Create(engine); err != nil {
		t.Fatal(err)
	}
	current, err := repo.GetByID(engine.ID)
	if err != nil {
		t.Fatal(err)
	}
	found, err := repo.FindByIdentityKey(engine.EngineType, engine.TenantID, current.IdentityKey)
	if err != nil || found.ID != engine.ID {
		t.Fatalf("find current address: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if applied, err := repo.UpdateConnectionObservation(engine.ID, models.EngineConnectionOnline, now, "reachable", &current.IdentityKey, "15"); err != nil || !applied {
		t.Fatalf("current address observation: applied=%t err=%v", applied, err)
	}
	stale := models.JSONString(`{"host":"former.source"}`)
	if _, err := repo.FindByIdentityKey(engine.EngineType, engine.TenantID, stale); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("stale address lookup: %v", err)
	}
	if applied, err := repo.UpdateConnectionObservation(engine.ID, models.EngineConnectionOffline, now.Add(time.Second), "stale", &stale, ""); err != nil || applied {
		t.Fatalf("stale address observation: applied=%t err=%v", applied, err)
	}
	observed, err := repo.GetByID(engine.ID)
	if err != nil || observed.ConnectionStatus != models.EngineConnectionOnline || observed.Version != current.Version {
		t.Fatalf("online observation and aggregate version: %v", err)
	}
}
