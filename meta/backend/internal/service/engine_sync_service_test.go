package service

import (
	"testing"
	"time"

	"github.com/addp/common/events"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

func TestEngineSyncServiceDoesNotCreateAutomaticTaskOnCreateEvent(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	tenantID := uint(1)
	engineID := uint(9)
	engineSvc := NewEngineService(db, nil)
	engineSvc.cacheEngine(tenantID, &commonModels.Engine{
		ID:             engineID,
		TenantID:       &tenantID,
		Name:           "Business MinIO",
		EngineType:     "s3",
		LifecycleState: "active",
	})
	syncSvc := NewEngineSyncService(nil, engineSvc)

	if err := syncSvc.handleEngineChangeEvent(events.EngineChangeEvent{
		EngineID:  engineID,
		Action:    events.ActionCreate,
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("handleEngineChangeEvent() error = %v", err)
	}
	if _, err := engineSvc.GetResourceByID(engineID, tenantID); err == nil {
		t.Fatal("create event must clear cached engine details and must not reload without a tenant request")
	}

	var count int64
	if err := db.Model(&models.ScanTask{}).
		Where("owner_module = ? AND owner_ref = ?", "system", scantask.AutomaticTaskOwnerRef(engineID)).
		Count(&count).Error; err != nil {
		t.Fatalf("count automatic task: %v", err)
	}
	if count != 0 {
		t.Fatalf("automatic task count = %d, want 0", count)
	}
}

func TestEngineSyncServiceDoesNotDeleteAutomaticTaskOnUpdateEvent(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	tenantID := uint(1)
	engineID := uint(10)
	now := time.Now()
	existing := scantask.NewEngineScanTask(tenantID, 7, engineID, "Business MinIO", "15 3 * * *", "deep", now, nil)
	if err := db.Create(existing).Error; err != nil {
		t.Fatalf("create existing engine scan task: %v", err)
	}

	engineSvc := NewEngineService(db, nil)
	engineSvc.cacheEngine(tenantID, &commonModels.Engine{
		ID:             engineID,
		TenantID:       &tenantID,
		Name:           "Business MinIO",
		EngineType:     "s3",
		LifecycleState: "active",
	})
	syncSvc := NewEngineSyncService(nil, engineSvc)

	if err := syncSvc.handleEngineChangeEvent(events.EngineChangeEvent{
		EngineID:  engineID,
		Action:    events.ActionUpdate,
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("handleEngineChangeEvent() error = %v", err)
	}
	if _, err := engineSvc.GetResourceByID(engineID, tenantID); err == nil {
		t.Fatal("update event must clear cached engine details")
	}

	var count int64
	if err := db.Model(&models.ScanTask{}).
		Where("owner_module = ? AND owner_ref = ?", "system", scantask.AutomaticTaskOwnerRef(engineID)).
		Count(&count).Error; err != nil {
		t.Fatalf("count automatic task: %v", err)
	}
	if count != 1 {
		t.Fatalf("automatic task count = %d, want 1", count)
	}
}

func TestEngineSyncServiceDoesNotDeleteAutomaticTaskOnDeleteEvent(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	createScanTaskTable(t, db)

	tenantID := uint(1)
	engineID := uint(11)
	now := time.Now()
	existing := scantask.NewEngineScanTask(tenantID, 7, engineID, "Business MinIO", "15 3 * * *", "deep", now, nil)
	if err := db.Create(existing).Error; err != nil {
		t.Fatalf("create existing engine scan task: %v", err)
	}

	engineSvc := NewEngineService(db, nil)
	syncSvc := NewEngineSyncService(nil, engineSvc)
	if err := syncSvc.handleEngineChangeEvent(events.EngineChangeEvent{
		EngineID:  engineID,
		Action:    events.ActionDelete,
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("handleEngineChangeEvent() error = %v", err)
	}

	var count int64
	if err := db.Model(&models.ScanTask{}).
		Where("owner_module = ? AND owner_ref = ?", "system", scantask.AutomaticTaskOwnerRef(engineID)).
		Count(&count).Error; err != nil {
		t.Fatalf("count automatic task: %v", err)
	}
	if count != 1 {
		t.Fatalf("automatic task count = %d, want 1", count)
	}
}
