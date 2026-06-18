package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/events"
	commonExecution "github.com/addp/common/execution"
	_ "github.com/addp/common/format/plugins/csv"
	"github.com/addp/transfer/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTransferCleanupScanFindsTaskDefinitionsByEngineContext(t *testing.T) {
	t.Parallel()

	db := newTransferCleanupTestDB(t)
	svc := NewTransferCleanupService(db, nil, nil)
	createTransferCleanupTestTask(t, db, 7, "match-source", 1, 2, true)
	createTransferCleanupTestTask(t, db, 7, "match-target", 3, 1, true)
	createTransferCleanupTestTask(t, db, 7, "other", 3, 4, true)

	stats, err := svc.ScanReclaimCandidates(context.Background(), 7, map[string]interface{}{"engine_id": 1})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() error = %v", err)
	}
	if stats.TaskDefinitions != 2 {
		t.Fatalf("TaskDefinitions = %d, want 2", stats.TaskDefinitions)
	}

	stats, err = svc.ScanReclaimCandidates(context.Background(), 7, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() without context error = %v", err)
	}
	if stats.TaskDefinitions != 0 {
		t.Fatalf("TaskDefinitions without lifecycle context = %d, want 0", stats.TaskDefinitions)
	}
}

func TestTransferCleanupLogicalDisablesTaskDefinitions(t *testing.T) {
	t.Parallel()

	db := newTransferCleanupTestDB(t)
	svc := NewTransferCleanupService(db, nil, nil)
	task := createTransferCleanupTestTask(t, db, 7, "match", 1, 2, true)
	createTransferCleanupTestTask(t, db, 7, "other", 3, 4, true)

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModeLogical, map[string]interface{}{"engine_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DisabledTaskDefinitions != 1 || stats.DeletedTaskDefinitions != 0 {
		t.Fatalf("stats = %#v, want one disabled", stats)
	}

	var updated models.TransferTask
	if err := db.First(&updated, task.ID).Error; err != nil {
		t.Fatalf("load updated task: %v", err)
	}
	if updated.Enabled || updated.NextRunAt != nil || updated.Status != models.TaskStatusIdle {
		t.Fatalf("updated task = %#v, want disabled idle task without next_run_at", updated)
	}
}

func TestTransferCleanupPhysicalDeletesTaskDefinitions(t *testing.T) {
	t.Parallel()

	db := newTransferCleanupTestDB(t)
	svc := NewTransferCleanupService(db, nil, nil)
	task := createTransferCleanupTestTask(t, db, 7, "match", 1, 2, true)
	other := createTransferCleanupTestTask(t, db, 7, "other", 3, 4, true)

	stats, err := svc.ExecuteCleanup(context.Background(), 7, events.CleanupModePhysical, map[string]interface{}{"engine_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if stats.DeletedTaskDefinitions != 1 || stats.DisabledTaskDefinitions != 0 {
		t.Fatalf("stats = %#v, want one deleted", stats)
	}

	var deleted models.TransferTask
	if err := db.First(&deleted, task.ID).Error; err == nil {
		t.Fatal("physically cleaned task should be deleted from active query")
	}
	if err := db.Unscoped().First(&deleted, task.ID).Error; err == nil {
		t.Fatal("physically cleaned task should be deleted from table")
	}

	var kept models.TransferTask
	if err := db.First(&kept, other.ID).Error; err != nil {
		t.Fatalf("other task should remain active: %v", err)
	}
}

func newTransferCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS transfer").Error; err != nil {
		t.Fatalf("attach transfer schema: %v", err)
	}
	stmt := `CREATE TABLE transfer.transfer_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		description TEXT,
		task_type TEXT NOT NULL,
		config JSON,
		schedule TEXT,
		batch_size INTEGER,
		enabled BOOLEAN,
		auto_scan_metadata BOOLEAN,
		status TEXT,
		progress REAL,
		created_by INTEGER,
		last_execution_id TEXT,
		last_execution_status TEXT,
		last_run_at DATETIME,
		next_run_at DATETIME,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME
	)`
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("create transfer task table: %v", err)
	}
	return db
}

func createTransferCleanupTestTask(t *testing.T, db *gorm.DB, tenantID uint, name string, sourceEngineID uint, targetEngineID uint, enabled bool) models.TransferTask {
	t.Helper()
	nextRunAt := time.Now().Add(time.Hour)
	task := models.TransferTask{
		TenantID:  tenantID,
		Name:      name,
		TaskType:  commonExecution.TaskTypeImport,
		Config:    transferCleanupTestTaskConfig(sourceEngineID, targetEngineID),
		BatchSize: 100,
		Enabled:   enabled,
		Status:    models.TaskStatusRunning,
		NextRunAt: &nextRunAt,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("create transfer task: %v", err)
	}
	return task
}

func transferCleanupTestTaskConfig(sourceEngineID uint, targetEngineID uint) models.JSONMap {
	return models.JSONMap{
		"mode": "batch",
		"source": map[string]interface{}{
			"locator":        transferCleanupTableLocator(sourceEngineID, "public", "roads"),
			"data_type":      "table",
			"representation": "native",
		},
		"target": map[string]interface{}{
			"parent_locator": transferCleanupDirectoryLocator(targetEngineID, "exports"),
			"name":           "roads.csv",
			"data_type":      "table",
			"representation": "encoded",
			"format":         "csv",
			"policy":         map[string]interface{}{"write_mode": "overwrite"},
		},
	}
}

func transferCleanupTableLocator(engineID uint, schema string, table string) string {
	return "addp://engine/" + uintText(engineID) + "/path/" + schema + "/" + table + "?type=table"
}

func transferCleanupDirectoryLocator(engineID uint, path string) string {
	return "addp://engine/" + uintText(engineID) + "/path/" + path + "?type=directory"
}

func uintText(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
