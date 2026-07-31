package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/orchestrator/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOrchestrationRepositoryEnforcesTenantOnCRUD(t *testing.T) {
	db := newOrchestrationScheduleTestDB(t)
	repo := NewOrchestrationRepository(db)

	tenantSeven := models.Orchestration{
		TenantID: 7,
		Name:     "tenant-seven",
		Steps:    models.Steps{{ID: "s1", Name: "step", Provider: "meta", TaskType: "scan", TaskID: 1}},
		EditorLayout: commonModels.JSONMap{
			"nodes": map[string]interface{}{"s1": map[string]interface{}{"x": 10, "y": 20}},
		},
	}
	tenantEight := models.Orchestration{TenantID: 8, Name: "tenant-eight", Steps: models.Steps{{ID: "s1", Name: "step", Provider: "meta", TaskType: "scan", TaskID: 1}}}
	if err := repo.Create(&tenantSeven); err != nil {
		t.Fatalf("create tenant seven: %v", err)
	}
	if err := repo.Create(&tenantEight); err != nil {
		t.Fatalf("create tenant eight: %v", err)
	}

	listed, err := repo.List(7)
	if err != nil || len(listed) != 1 || listed[0].ID != tenantSeven.ID {
		t.Fatalf("tenant seven list = %#v, error = %v", listed, err)
	}
	if _, err := repo.GetByIDAndTenant(tenantSeven.ID, 8); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant get error = %v, want record not found", err)
	}

	attackUpdate := tenantSeven
	attackUpdate.TenantID = 8
	attackUpdate.Name = "cross-tenant-update"
	if err := repo.UpdateForTenant(&attackUpdate); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant update error = %v, want record not found", err)
	}
	unchanged, err := repo.GetByIDAndTenant(tenantSeven.ID, 7)
	if err != nil || unchanged.Name != "tenant-seven" {
		t.Fatalf("tenant seven after cross update = %#v, error = %v", unchanged, err)
	}

	if err := repo.DeleteForTenant(tenantSeven.ID, 8); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("cross-tenant delete error = %v, want record not found", err)
	}
	if _, err := repo.GetByIDAndTenant(tenantSeven.ID, 7); err != nil {
		t.Fatalf("tenant seven should remain after cross delete: %v", err)
	}
	tenantSeven.EditorLayout = commonModels.JSONMap{
		"nodes": map[string]interface{}{"s1": map[string]interface{}{"x": 120, "y": 240}},
	}
	if err := repo.UpdateForTenant(&tenantSeven); err != nil {
		t.Fatalf("update editor layout: %v", err)
	}
	withLayout, err := repo.GetByIDAndTenant(tenantSeven.ID, 7)
	if err != nil || withLayout.EditorLayout["nodes"] == nil {
		t.Fatalf("editor layout was not persisted: %#v, error = %v", withLayout, err)
	}
	nodes := withLayout.EditorLayout["nodes"].(map[string]interface{})
	position := nodes["s1"].(map[string]interface{})
	if position["x"] != float64(120) || position["y"] != float64(240) {
		t.Fatalf("editor layout position = %#v, want x=120 y=240", position)
	}
	if err := repo.DeleteForTenant(tenantSeven.ID, 7); err != nil {
		t.Fatalf("tenant delete: %v", err)
	}
	if _, err := repo.GetByIDAndTenant(tenantSeven.ID, 7); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted tenant get error = %v, want record not found", err)
	}
}

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

	refreshed, err := repo.GetByIDInternal(orch.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if refreshed.NextRunAt == nil || !refreshed.NextRunAt.After(dueAt) {
		t.Fatalf("next_run_at = %#v, want after %s", refreshed.NextRunAt, dueAt)
	}
}

func newOrchestrationScheduleTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
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
			editor_layout JSON NOT NULL DEFAULT '{}',
			enabled BOOLEAN,
			schedule TEXT,
			last_run_at DATETIME,
			next_run_at DATETIME,
			last_execution_id TEXT,
			last_execution_status TEXT,
			created_by INTEGER,
			authorization_ref TEXT,
			authorization_subject_id INTEGER,
			authorization_definition_hash TEXT,
			authorization_principal_id INTEGER,
			authorization_membership_id INTEGER,
			authorization_version INTEGER,
			authorized_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create orchestrations table: %v", err)
	}
	return db
}
