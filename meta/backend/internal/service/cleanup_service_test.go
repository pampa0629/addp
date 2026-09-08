package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

func TestCleanupScanReportsInvalidEngineScanTaskDefinitions(t *testing.T) {
	db := openObjectCatalogScanTestDB(t, metatest.WithLineageTables())
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

func TestCleanupScanReportsTaskOnlyInvalidEngineForAnyOwner(t *testing.T) {
	db := openObjectCatalogScanTestDB(t, metatest.WithLineageTables())
	createScanTaskTable(t, db)
	systemClient := newEmptyEngineSystemClient(t)

	task := &models.ScanTask{
		TenantID: 1, EngineID: 25, Name: "Business Neo4j 定时扫描",
		Schedule: "15 3 * * *", Enabled: true, OwnerModule: "meta", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("create scan task: %v", err)
	}

	svc := NewCleanupService(db, nil, systemClient, nil, CleanupConfig{Enabled: true})
	stats, err := svc.ScanReclaimCandidates(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates() error = %v", err)
	}
	if stats.ScanTaskDefinitions.Count != 1 {
		t.Fatalf("scan task definition count = %d, want 1", stats.ScanTaskDefinitions.Count)
	}
	if stats.InvalidEngines.Count != 0 {
		t.Fatalf("task-only invalid engine must not be reported as a Meta snapshot: %#v", stats.InvalidEngines)
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

func TestCleanupEngineScopedPhysicalDisablesInvalidEngineScanTaskDefinitions(t *testing.T) {
	db := openObjectCatalogScanTestDB(t, metatest.WithLineageTables())
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
	if result.DisabledScanTaskDefinitions != 1 || result.DeletedScanTaskDefinitions != 0 {
		t.Fatalf("cleanup result = %#v, want one disabled task", result)
	}

	var stored models.ScanTask
	if err := db.First(&stored, task.ID).Error; err != nil {
		t.Fatalf("load scan task: %v", err)
	}
	if stored.Enabled {
		t.Fatal("engine lifecycle cleanup must preserve and disable the task definition")
	}
}

func TestCleanupLogicalDeletesManagerContentProjection(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	systemClient := newEmptyEngineSystemClient(t)

	const tenantID = uint(1)
	const engineID = uint(91)
	node := models.MetaNode{TenantID: tenantID, EngineID: engineID, NodeType: "root", Name: "Deleted Engine", Depth: 0}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create meta node: %v", err)
	}

	var deletedEngineID uint64
	manager := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodDelete || request.URL.Path != "/api/v1/manager/runtime/content-documents" || request.Header.Get("Authorization") != "Bearer manager-token" {
			t.Fatalf("unexpected Manager request: %s %s", request.Method, request.URL.String())
		}
		deletedEngineID, _ = strconv.ParseUint(request.URL.Query().Get("engine_id"), 10, 64)
		response.WriteHeader(http.StatusNoContent)
	}))
	defer manager.Close()
	contentIndex := commonClient.NewManagerContentClient(manager.URL, commonClient.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) {
		return "manager-token", nil
	}), manager.Client())
	svc := NewCleanupService(db, nil, systemClient, contentIndex, CleanupConfig{Enabled: true})

	result, err := svc.ExecuteCleanup(context.Background(), tenantID, events.CleanupModeLogical, map[string]interface{}{"engine_id": engineID})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if deletedEngineID != uint64(engineID) {
		t.Fatalf("deleted engine ID = %d errors=%v, want %d", deletedEngineID, result.Errors, engineID)
	}
}

func TestCleanupPhysicalKeepsInvalidEngineSnapshotForTaskDefinitionCleanup(t *testing.T) {
	db := openObjectCatalogScanTestDB(t, metatest.WithLineageTables())
	createScanTaskTable(t, db)
	systemClient := newEmptyEngineSystemClient(t)

	const tenantID = uint(1)
	const engineID = uint(92)
	node := models.MetaNode{TenantID: tenantID, EngineID: engineID, NodeType: "root", Name: "Deleted Engine", Depth: 0}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create meta node: %v", err)
	}
	task := scantask.NewEngineScanTask(tenantID, 7, engineID, "Deleted Engine", "15 3 * * *", "deep", time.Now(), nil)
	if err := db.Create(task).Error; err != nil {
		t.Fatalf("create scan task: %v", err)
	}
	if err := db.Delete(&node).Error; err != nil {
		t.Fatalf("soft delete meta node: %v", err)
	}

	svc := NewCleanupService(db, nil, systemClient, nil, CleanupConfig{Enabled: true})
	result, err := svc.ExecuteCleanup(context.Background(), tenantID, events.CleanupModePhysical, nil)
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if result.DeletedScanTaskDefinitions != 1 {
		t.Fatalf("deleted scan task definitions = %d, want 1", result.DeletedScanTaskDefinitions)
	}
}

func TestMetaScanSummaryCountsResourcesUnderInvalidEngines(t *testing.T) {
	stats := &models.MetaCleanupStatistics{}
	stats.InvalidEngines.Count = 2
	stats.InvalidEngines.Details = []models.InvalidEngineDetail{
		{EngineID: 7, AffectedNodes: 3, AffectedItems: 11},
		{EngineID: 8, AffectedNodes: 2, AffectedItems: 5},
	}
	stats.OrphanItems.Count = 4
	summary := metaScanSummary(stats)
	if summary.ScannedItems != 25 {
		t.Fatalf("scanned items = %d, want 25", summary.ScannedItems)
	}
}

func TestMetaExecuteSummaryReportsLineageRetentionAsSkipped(t *testing.T) {
	summary := metaExecuteSummary(&models.MetaCleanupExecuteResult{
		DeletedItems:            3,
		RetainedLineageItems:    2,
		RetainedReferencedNodes: 1,
	})
	if summary.AffectedRecords != 3 || summary.SkippedItems != 3 || summary.ErrorCount != 0 {
		t.Fatalf("execute summary = %#v, want affected=3 skipped=3 errors=0", summary)
	}
}

func newEmptyEngineSystemClient(t *testing.T) *commonClient.SystemServiceClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "addp_at_meta", "token_type": "bearer", "expires_in": 300, "scope": "addp.api",
			})
			return
		}
		if r.URL.Path != "/api/v1/system/engines" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]interface{}{})
	}))
	t.Cleanup(server.Close)
	tokenSource, err := commonClient.NewOAuthServiceTokenSource(
		server.URL, "addp-meta", "meta-cleanup-test-client-secret-32-bytes", server.Client(),
	)
	if err != nil {
		t.Fatalf("create service token source: %v", err)
	}
	return commonClient.NewSystemServiceClient(server.URL, tokenSource, server.Client())
}
