package metacleanup

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
)

func TestDatabaseCleanerClassifiesInvalidMetaEngines(t *testing.T) {
	t.Parallel()

	engines := []commonModels.Engine{
		metaCleanupTestEngine(1, "PostgreSQL", true, true),
		metaCleanupTestEngine(2, "Spark Workflow", true, false),
		metaCleanupTestEngine(3, "Disabled MySQL", false, true),
	}
	server := newMetaCleanupSystemServer(t, engines)
	defer server.Close()

	db := metatest.OpenMetadataDB(t)
	for _, node := range []models.MetaNode{
		{TenantID: 7, EngineID: 1, NodeType: "root", Name: "PostgreSQL", Depth: 0},
		{TenantID: 7, EngineID: 2, NodeType: "root", Name: "Old PostgreSQL", Depth: 0},
		{TenantID: 7, EngineID: 3, NodeType: "root", Name: "Disabled MySQL", Depth: 0},
		{TenantID: 7, EngineID: 4, NodeType: "root", Name: "Deleted Doris", Depth: 0},
	} {
		if err := db.Create(&node).Error; err != nil {
			t.Fatalf("create meta node for engine %d: %v", node.EngineID, err)
		}
	}
	if err := db.Create(&models.MetaItem{
		TenantID: 7, EngineID: 2, NodeID: 2, ItemType: "table", Name: "orders", Fingerprint: "orders",
	}).Error; err != nil {
		t.Fatalf("create meta item: %v", err)
	}
	deletedItem := models.MetaItem{
		TenantID: 7, EngineID: 2, NodeID: 2, ItemType: "table", Name: "old_orders", Fingerprint: "old_orders",
	}
	if err := db.Create(&deletedItem).Error; err != nil {
		t.Fatalf("create deleted meta item: %v", err)
	}
	if err := db.Delete(&deletedItem).Error; err != nil {
		t.Fatalf("soft delete meta item: %v", err)
	}
	deletedNode := models.MetaNode{TenantID: 7, EngineID: 5, NodeType: "root", Name: "Already Cleaned", Depth: 0}
	if err := db.Create(&deletedNode).Error; err != nil {
		t.Fatalf("create deleted meta node: %v", err)
	}
	if err := db.Delete(&deletedNode).Error; err != nil {
		t.Fatalf("soft delete meta node: %v", err)
	}

	cleaner := NewDatabaseCleaner(db, newMetaCleanupSystemClient(t, server), nil)
	details, err := cleaner.ScanInvalidEngines(t.Context(), 7)
	if err != nil {
		t.Fatalf("ScanInvalidEngines() error = %v", err)
	}

	if len(details) != 3 {
		t.Fatalf("invalid engine details = %#v, want 3 entries", details)
	}
	wantReasons := map[uint]string{
		2: "引擎不具备存储能力",
		3: "引擎已禁用",
		4: "引擎已删除",
	}
	for _, detail := range details {
		if got := detail.Reason; got != wantReasons[detail.EngineID] {
			t.Errorf("engine %d reason = %q, want %q", detail.EngineID, got, wantReasons[detail.EngineID])
		}
		if detail.EngineID == 2 && (detail.EngineName != "Spark Workflow" || detail.AffectedNodes != 1 || detail.AffectedItems != 1) {
			t.Errorf("workflow engine detail = %#v", detail)
		}
	}

	if got, want := cleaner.InvalidEngineIDs(t.Context(), 7), []uint{2, 3, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("InvalidEngineIDs() = %v, want %v", got, want)
	}
}

func TestDatabaseCleanerScopedInvalidEngineIDsTrustsDeletionContext(t *testing.T) {
	t.Parallel()

	engines := []commonModels.Engine{
		metaCleanupTestEngine(1, "PostgreSQL", true, true),
		metaCleanupTestEngine(2, "Spark Workflow", true, false),
	}
	server := newMetaCleanupSystemServer(t, engines)
	defer server.Close()

	db := metatest.OpenMetadataDB(t)
	for _, node := range []models.MetaNode{
		{TenantID: 7, EngineID: 1, NodeType: "root", Name: "PostgreSQL", Depth: 0},
		{TenantID: 7, EngineID: 2, NodeType: "root", Name: "Old PostgreSQL", Depth: 0},
	} {
		if err := db.Create(&node).Error; err != nil {
			t.Fatalf("create meta node for engine %d: %v", node.EngineID, err)
		}
	}

	cleaner := NewDatabaseCleaner(db, newMetaCleanupSystemClient(t, server), nil)
	if got, want := cleaner.InvalidEngineIDsWithScope(t.Context(), 7, CleanupScope{EngineID: 1}), []uint{1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped engine IDs = %v, want %v", got, want)
	}
	if got, want := cleaner.InvalidEngineIDsWithScope(t.Context(), 7, CleanupScope{EngineID: 2}), []uint{2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid scoped engine IDs = %v, want %v", got, want)
	}
}

func TestDatabaseCleanerLogicalCleanupSoftDeletesOrphanItems(t *testing.T) {
	t.Parallel()

	db := metatest.OpenMetadataDB(t)
	node := models.MetaNode{TenantID: 7, EngineID: 1, NodeType: "root", Name: "PostgreSQL", Depth: 0}
	if err := db.Create(&node).Error; err != nil {
		t.Fatalf("create meta node: %v", err)
	}
	item := models.MetaItem{
		TenantID: 7, EngineID: 1, NodeID: node.ID, ItemType: "table", Name: "orders", Fingerprint: "orders",
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create meta item: %v", err)
	}
	if err := db.Unscoped().Delete(&node).Error; err != nil {
		t.Fatalf("physically delete parent node: %v", err)
	}

	cleaner := NewDatabaseCleaner(db, nil, nil)
	result, err := cleaner.ExecuteSoftDelete(t.Context(), 7, nil)
	if err != nil {
		t.Fatalf("ExecuteSoftDelete() error = %v", err)
	}
	if result.DeletedItems != 1 || len(result.Errors) != 0 {
		t.Fatalf("ExecuteSoftDelete() result = %#v, want one soft-deleted item", result)
	}

	var activeCount int64
	if err := db.Model(&models.MetaItem{}).Where("id = ?", item.ID).Count(&activeCount).Error; err != nil {
		t.Fatalf("count active orphan item: %v", err)
	}
	if activeCount != 0 {
		t.Fatalf("active orphan item count = %d, want 0", activeCount)
	}

	var preserved models.MetaItem
	if err := db.Unscoped().First(&preserved, item.ID).Error; err != nil {
		t.Fatalf("read preserved orphan item: %v", err)
	}
	if !preserved.DeletedAt.Valid {
		t.Fatal("orphan item was not marked deleted")
	}

	orphans, err := cleaner.ScanOrphanItems(t.Context(), 7)
	if err != nil {
		t.Fatalf("ScanOrphanItems() error = %v", err)
	}
	if len(orphans) != 0 {
		t.Fatalf("soft-deleted orphan items = %#v, want none", orphans)
	}
}

func metaCleanupTestEngine(id uint, name string, active bool, storage bool) commonModels.Engine {
	lifecycleState := commonModels.EngineLifecycleDisabled
	if active {
		lifecycleState = commonModels.EngineLifecycleActive
	}
	capabilities := commonModels.JSONString(`{"schema_version":"engine.capabilities/v1","engine_type":"test","engine_family":"test"}`)
	if storage {
		capabilities = commonModels.JSONString(`{"schema_version":"engine.capabilities/v1","engine_type":"test","engine_family":"test","storage":{}}`)
	}
	return commonModels.Engine{
		ID:             id,
		Name:           name,
		EngineType:     "test",
		LifecycleState: lifecycleState,
		Capabilities:   &capabilities,
	}
}

func newMetaCleanupSystemServer(t *testing.T, engines []commonModels.Engine) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/oauth/token" {
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
			}
			if r.Form.Get("tenant_id") != "7" {
				t.Errorf("token tenant_id = %q, want 7", r.Form.Get("tenant_id"))
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "addp_at_meta", "token_type": "bearer", "expires_in": 300, "scope": "addp.api",
			})
			return
		}
		if r.URL.Path != "/api/v1/system/engines" {
			t.Errorf("request path = %q, want /api/v1/system/engines", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer addp_at_meta" {
			t.Errorf("Authorization = %q, want service Bearer", got)
		}
		if r.Header.Get("X-Internal-API-Key") != "" || r.Header.Get("X-Tenant-ID") != "" {
			t.Error("legacy authentication header was sent")
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"data": engines, "total": len(engines), "page": 1, "page_size": 100,
		}); err != nil {
			t.Errorf("encode engines: %v", err)
		}
	}))
}

func newMetaCleanupSystemClient(t *testing.T, server *httptest.Server) *commonClient.SystemServiceClient {
	t.Helper()
	source, err := commonClient.NewOAuthServiceTokenSource(
		server.URL, "addp-meta", "meta-cleanup-test-client-secret-32-bytes", server.Client(),
	)
	if err != nil {
		t.Fatalf("create service token source: %v", err)
	}
	return commonClient.NewSystemServiceClient(server.URL, source, server.Client())
}
