package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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
	resource := commonModels.Engine{
		ID:         engineID,
		TenantID:   &tenantID,
		Name:       "Business MinIO",
		EngineType: "s3",
		IsActive:   true,
	}

	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if got := r.Header.Get("X-Internal-API-Key"); got != "secret" {
			t.Errorf("internal api key = %q, want secret", got)
		}
		if got := r.URL.Path; got != "/api/v1/internal/engines/9" {
			t.Errorf("path = %q, want /api/v1/internal/engines/9", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resource); err != nil {
			t.Fatalf("encode resource: %v", err)
		}
	}))
	defer server.Close()

	engineSvc := NewEngineService(db, server.URL, "secret")
	syncSvc := NewEngineSyncService(nil, engineSvc)

	if err := syncSvc.handleEngineChangeEvent(events.EngineChangeEvent{
		EngineID:  engineID,
		Action:    events.ActionCreate,
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("handleEngineChangeEvent() error = %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("system requests = %d, want 1", got)
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

	resource := commonModels.Engine{
		ID:         engineID,
		TenantID:   &tenantID,
		Name:       "Business MinIO",
		EngineType: "s3",
		IsActive:   true,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resource); err != nil {
			t.Fatalf("encode resource: %v", err)
		}
	}))
	defer server.Close()

	engineSvc := NewEngineService(db, server.URL, "secret")
	syncSvc := NewEngineSyncService(nil, engineSvc)

	if err := syncSvc.handleEngineChangeEvent(events.EngineChangeEvent{
		EngineID:  engineID,
		Action:    events.ActionUpdate,
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

	engineSvc := NewEngineService(db, "", "")
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
