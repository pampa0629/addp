package repository

import (
	"context"
	"testing"
	"time"

	"github.com/addp/orchestrator/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestClaimDueAdvancesNextRunAt(t *testing.T) {
	db := newOrchestrationScheduleTestDB(t)
	repo := NewOrchestrationRepository(db)

	dueAt := time.Now().Add(-time.Minute)
	nextRunAt := time.Now().Add(time.Hour)
	orch := models.Orchestration{
		TenantID:  7,
		Name:      "nightly",
		Steps:     models.Steps{{ID: "s1", Name: "step", Provider: "meta", TaskType: "scan", TaskID: 1}},
		Enabled:   true,
		Schedule:  "0 2 * * *",
		NextRunAt: &dueAt,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := repo.Create(&orch); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	ids, err := repo.ListDueIDs(context.Background(), time.Now(), 100)
	if err != nil {
		t.Fatalf("ListDueIDs() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != orch.ID {
		t.Fatalf("due ids = %#v, want [%d]", ids, orch.ID)
	}

	claimed, err := repo.ClaimDue(context.Background(), orch.ID, orch.Schedule, time.Now(), &nextRunAt)
	if err != nil {
		t.Fatalf("ClaimDue() error = %v", err)
	}
	if claimed == nil || claimed.ID != orch.ID {
		t.Fatalf("claimed = %#v, want orchestration %d", claimed, orch.ID)
	}

	refreshed, err := repo.GetByID(orch.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if refreshed.NextRunAt == nil || !refreshed.NextRunAt.After(dueAt) {
		t.Fatalf("next_run_at = %#v, want after %s", refreshed.NextRunAt, dueAt)
	}
}

func newOrchestrationScheduleTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS orchestrator").Error; err != nil {
		t.Fatalf("attach orchestrator schema: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE orchestrator.orchestrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			steps JSON NOT NULL,
			enabled BOOLEAN,
			schedule TEXT,
			last_run_at DATETIME,
			next_run_at DATETIME,
			last_execution_id TEXT,
			last_execution_status TEXT,
			created_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create orchestrations table: %v", err)
	}
	return db
}
