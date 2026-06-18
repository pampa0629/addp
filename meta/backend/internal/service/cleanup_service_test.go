package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/events"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

func TestCleanupScanReportsInvalidEngineScanTaskDefinitions(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	const tenantID = uint(1)
	const engineID = uint(11)
	now := time.Now()
	task := scantask.NewEngineScanTask(tenantID, 7, engineID, "Business MinIO", "15 3 * * *", "deep", now, nil)
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("create scan task: %v", err)
	}

	svc := NewCleanupService(db, nil, nil, nil, CleanupConfig{Enabled: true})
	stats, err := svc.ScanReclaimCandidates(context.Background(), tenantID, map[string]interface{}{"engine_id": engineID})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() error = %v", err)
	}
	if stats.ScanTaskDefinitions.Count != 1 {
		t.Fatalf("scan task definition count = %d, want 1", stats.ScanTaskDefinitions.Count)
	}
}

func TestCleanupLogicalDisablesInvalidEngineScanTaskDefinitions(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	const tenantID = uint(1)
	const engineID = uint(12)
	now := time.Now()
	nextRunAt := now.Add(time.Hour)
	task := scantask.NewEngineScanTask(tenantID, 7, engineID, "Business MinIO", "15 3 * * *", "deep", now, &nextRunAt)
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("create scan task: %v", err)
	}

	svc := NewCleanupService(db, nil, nil, nil, CleanupConfig{Enabled: true})
	result, err := svc.ExecuteCleanup(context.Background(), tenantID, events.CleanupModeLogical, map[string]interface{}{"engine_id": engineID})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if result.DisabledScanTaskDefinitions != 1 {
		t.Fatalf("disabled scan task definitions = %d, want 1", result.DisabledScanTaskDefinitions)
	}

	var stored models.ScanTask
	if err := db.First(&stored, task.ID).Error; err != nil {
		t.Fatalf("load scan task: %v", err)
	}
	if stored.Enabled {
		t.Fatalf("scan task remains enabled")
	}
	if stored.NextRunAt != nil {
		t.Fatalf("next_run_at = %v, want nil", stored.NextRunAt)
	}
}

func TestCleanupPhysicalDeletesInvalidEngineScanTaskDefinitions(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	const tenantID = uint(1)
	const engineID = uint(13)
	now := time.Now()
	task := scantask.NewEngineScanTask(tenantID, 7, engineID, "Business MinIO", "15 3 * * *", "deep", now, nil)
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("create scan task: %v", err)
	}

	svc := NewCleanupService(db, nil, nil, nil, CleanupConfig{Enabled: true})
	result, err := svc.ExecuteCleanup(context.Background(), tenantID, events.CleanupModePhysical, map[string]interface{}{"engine_id": engineID})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if result.DeletedScanTaskDefinitions != 1 {
		t.Fatalf("deleted scan task definitions = %d, want 1", result.DeletedScanTaskDefinitions)
	}

	var count int64
	if err := db.Model(&models.ScanTask{}).
		Where("id = ?", task.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("count scan task: %v", err)
	}
	if count != 0 {
		t.Fatalf("scan task count = %d, want 0", count)
	}
}
