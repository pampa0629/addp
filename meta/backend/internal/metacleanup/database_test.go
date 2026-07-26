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

	cleaner := NewDatabaseCleaner(db, commonClient.NewSystemClientWithInternalKey(server.URL, "secret"), nil)
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

	if got, want := cleaner.InvalidEngineIDs(t.Context(), 7), []uint{2, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Fatalf("InvalidEngineIDs() = %v, want %v", got, want)
	}
}

func TestDatabaseCleanerScopedInvalidEngineIDsStillChecksEligibility(t *testing.T) {
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

	cleaner := NewDatabaseCleaner(db, commonClient.NewSystemClientWithInternalKey(server.URL, "secret"), nil)
	if got := cleaner.InvalidEngineIDsWithScope(t.Context(), 7, CleanupScope{EngineID: 1}); len(got) != 0 {
		t.Fatalf("valid scoped engine IDs = %v, want none", got)
	}
	if got, want := cleaner.InvalidEngineIDsWithScope(t.Context(), 7, CleanupScope{EngineID: 2}), []uint{2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("invalid scoped engine IDs = %v, want %v", got, want)
	}
}

func metaCleanupTestEngine(id uint, name string, active bool, storage bool) commonModels.Engine {
	capabilities := commonModels.JSONString(`{"schema_version":"engine.capabilities/v1","engine_type":"test","engine_family":"test"}`)
	if storage {
		capabilities = commonModels.JSONString(`{"schema_version":"engine.capabilities/v1","engine_type":"test","engine_family":"test","storage":{}}`)
	}
	return commonModels.Engine{
		ID:           id,
		Name:         name,
		EngineType:   "test",
		IsActive:     active,
		Capabilities: &capabilities,
	}
}

func newMetaCleanupSystemServer(t *testing.T, engines []commonModels.Engine) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/internal/engines" {
			t.Errorf("request path = %q, want /api/v1/internal/engines", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("X-Internal-API-Key"); got != "secret" {
			t.Errorf("internal key = %q, want secret", got)
		}
		if got := r.URL.Query().Get("tenant_id"); got != "7" {
			t.Errorf("tenant_id = %q, want 7", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(engines); err != nil {
			t.Errorf("encode engines: %v", err)
		}
	}))
}
