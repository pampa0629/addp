package service

import (
	"context"
	"testing"

	"github.com/addp/common/events"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDevelopCleanupEngineContextDoesNotTreatUserTasksAsReclaimCandidates(t *testing.T) {
	t.Parallel()

	db := newDevelopCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	task := createDevelopCleanupTask(t, db, 7, "query-match", "query", map[string]interface{}{"engine_id": float64(12)})
	createDevelopCleanupTask(t, db, 7, "content-only", "workflow", nil)

	stats, err := svc.ScanReclaimCandidates(context.Background(), 7, map[string]interface{}{"engine_id": uint(12)})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() error = %v", err)
	}
	if stats.DevTasks != 0 {
		t.Fatalf("DevTasks for engine lifecycle = %d, want 0", stats.DevTasks)
	}

	stats, err = svc.ExecuteCleanup(context.Background(), 7, events.CleanupModeLogical, map[string]interface{}{"engine_id": uint(12)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DevTasks != 0 {
		t.Fatalf("ExecuteCleanup for engine lifecycle DevTasks = %d, want 0", stats.DevTasks)
	}

	var updated models.DevTask
	if err := db.First(&updated, task.ID).Error; err != nil {
		t.Fatalf("load dev task: %v", err)
	}
	if updated.Status != "active" {
		t.Fatalf("task status=%q, want active", updated.Status)
	}

	stats, err = svc.ScanReclaimCandidates(context.Background(), 7, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() without context error = %v", err)
	}
	if stats.DevTasks != 0 {
		t.Fatalf("DevTasks without lifecycle context = %d, want 0", stats.DevTasks)
	}
}

func TestDevelopCleanupPhysicalDeletesTenantOwnedTasks(t *testing.T) {
	t.Parallel()

	db := newDevelopCleanupTestDB(t)
	svc := NewCleanupService(db, nil, nil)
	task := createDevelopCleanupTask(t, db, 7, "tenant-query", "query", map[string]interface{}{"engine_id": float64(12)})
	otherTenantTask := createDevelopCleanupTask(t, db, 8, "other-query", "query", map[string]interface{}{"engine_id": float64(12)})

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"tenant_id": uint(7)})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DeletedTasks != 1 {
		t.Fatalf("DeletedTasks = %d, want 1", stats.DeletedTasks)
	}
	var count int64
	if err := db.Unscoped().Model(&models.DevTask{}).Where("id = ?", task.ID).Count(&count).Error; err != nil {
		t.Fatalf("count deleted task: %v", err)
	}
	if count != 0 {
		t.Fatal("tenant dev task should be physically deleted")
	}
	if err := db.First(&models.DevTask{}, otherTenantTask.ID).Error; err != nil {
		t.Fatalf("other tenant task should remain: %v", err)
	}
}

func newDevelopCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatalf("attach develop schema: %v", err)
	}
	statements := []string{
		`CREATE TABLE develop.dev_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			display_name TEXT,
			dev_type TEXT NOT NULL,
			content JSON NOT NULL,
			execution_config JSON,
			timeout INTEGER,
			description TEXT,
			tags TEXT,
			created_by INTEGER,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME,
			status TEXT,
			last_execution_id TEXT,
			last_execution_status TEXT,
			last_run_at DATETIME
		)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}
	return db
}

func createDevelopCleanupTask(t *testing.T, db *gorm.DB, tenantID uint, name string, devType string, executionConfig map[string]interface{}) models.DevTask {
	t.Helper()
	item := models.DevTask{
		TenantID:        tenantID,
		Name:            name,
		DevType:         devType,
		Content:         models.DevTaskContent{},
		ExecutionConfig: models.DevTaskContent(executionConfig),
		Timeout:         300,
		Status:          "active",
	}
	if item.ExecutionConfig == nil {
		item.ExecutionConfig = models.DevTaskContent{}
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create dev task: %v", err)
	}
	return item
}
