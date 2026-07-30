package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/meta/internal/metacleanup"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

type recordingMetaSearchCleaner struct {
	executedEngineIDs []uint
}

func (c *recordingMetaSearchCleaner) Enabled() bool { return true }

func (c *recordingMetaSearchCleaner) ScanReclaimCandidates(context.Context, uint, []uint) (*metacleanup.MeilisearchReclaimStats, error) {
	return &metacleanup.MeilisearchReclaimStats{}, nil
}

func (c *recordingMetaSearchCleaner) ExecuteCleanup(_ context.Context, _ uint, engineIDs []uint) (int, error) {
	c.executedEngineIDs = append([]uint(nil), engineIDs...)
	return len(engineIDs), nil
}

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

func TestCleanupLogicalKeepsInvalidEngineSnapshotForSearchCleanup(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
	systemClient := newEmptyEngineSystemClient(t)

	const tenantID = uint(1)
	const engineID = uint(91)
	node := models.MetaNode{TenantID: tenantID, EngineID: engineID, NodeType: "root", Name: "Deleted Engine", Depth: 0}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create meta node: %v", err)
	}

	searchCleaner := &recordingMetaSearchCleaner{}
	svc := NewCleanupService(db, nil, systemClient, nil, CleanupConfig{Enabled: true})
	svc.searchCleaner = searchCleaner

	result, err := svc.ExecuteCleanup(context.Background(), tenantID, events.CleanupModeLogical, map[string]interface{}{"engine_id": engineID})
	if err != nil {
		t.Fatalf("ExecuteCleanup() error = %v", err)
	}
	if len(searchCleaner.executedEngineIDs) != 1 || searchCleaner.executedEngineIDs[0] != engineID {
		t.Fatalf("search cleanup engine IDs = %v, want [%d]", searchCleaner.executedEngineIDs, engineID)
	}
	if result.DeletedMeilisearchIndexes != 1 {
		t.Fatalf("deleted Meilisearch indexes = %d, want 1", result.DeletedMeilisearchIndexes)
	}
}

func TestCleanupPhysicalKeepsInvalidEngineSnapshotForTaskDefinitionCleanup(t *testing.T) {
	db := openObjectCatalogScanTestDB(t)
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
	stats.MeilisearchIndexes.Count = 6

	summary := metaScanSummary(stats)
	if summary.ScannedItems != 31 {
		t.Fatalf("scanned items = %d, want 31", summary.ScannedItems)
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
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []interface{}{}, "total": 0, "page": 1, "page_size": 100,
		})
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
