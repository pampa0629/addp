package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeMetaChangeSource struct {
	responses map[string]*commonClient.MetaDataItemChangesResponse
}

func (f *fakeMetaChangeSource) ListDataItemChanges(_ context.Context, _ uint, cursor string, _ int) (*commonClient.MetaDataItemChangesResponse, error) {
	response := f.responses[cursor]
	if response == nil {
		return nil, fmt.Errorf("unexpected cursor %q", cursor)
	}
	return response, nil
}

func TestSourceSyncCreatesStableEntryAndAppliesMissingChange(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	observedAt := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	source := &fakeMetaChangeSource{responses: map[string]*commonClient.MetaDataItemChangesResponse{
		"": changeBatch("cursor-1", false, commonClient.MetaDataItemChange{
			ChangeID: "change-1", Operation: "upsert", SourceIdentity: "fingerprint-orders",
			SourceVersion: "00000000000000000001", ObservedAt: observedAt,
			Snapshot: map[string]interface{}{
				"item_id": float64(21), "engine_id": float64(7), "name": "orders", "item_type": "table",
				"fields": []interface{}{
					map[string]interface{}{"name": "id", "type": "int64", "ordinal_position": float64(1)},
					map[string]interface{}{"name": "amount", "type": "decimal", "ordinal_position": float64(2)},
				},
			},
		}),
		"cursor-1": changeBatch("cursor-2", false, commonClient.MetaDataItemChange{
			ChangeID: "change-2", Operation: "missing", SourceIdentity: "fingerprint-orders",
			SourceVersion: "00000000000000000002", ObservedAt: observedAt.Add(time.Hour),
			Snapshot: map[string]interface{}{"item_id": float64(21), "engine_id": float64(7), "name": "orders", "item_type": "table"},
		}),
	}}
	syncService := NewSourceSyncService(db, source)
	if err := syncService.SyncTenant(context.Background(), 7); err != nil {
		t.Fatalf("initial sync: %v", err)
	}

	var entry models.Entry
	if err := db.First(&entry).Error; err != nil {
		t.Fatalf("find entry: %v", err)
	}
	if entry.GovernanceStatus != models.GovernanceStatusDiscovered || entry.Visibility != models.VisibilityInventory || entry.Version != 1 {
		t.Fatalf("entry = %#v", entry)
	}
	var componentCount int64
	if err := db.Model(&models.Component{}).Where("catalog_entry_id = ? AND component_status = ?", entry.ID, models.SourceStatusActive).Count(&componentCount).Error; err != nil {
		t.Fatalf("count components: %v", err)
	}
	if componentCount != 2 {
		t.Fatalf("active component count = %d, want 2", componentCount)
	}

	if err := syncService.SyncTenant(context.Background(), 7); err != nil {
		t.Fatalf("missing sync: %v", err)
	}
	var binding models.SourceBinding
	if err := db.Where("catalog_entry_id = ? AND is_current = ?", entry.ID, true).First(&binding).Error; err != nil {
		t.Fatalf("find binding: %v", err)
	}
	if binding.SourceStatus != models.SourceStatusMissing || binding.SourceVersion != "00000000000000000002" || binding.MissingAt == nil {
		t.Fatalf("binding = %#v", binding)
	}
	if err := db.First(&entry, "id = ?", entry.ID).Error; err != nil {
		t.Fatalf("reload entry: %v", err)
	}
	if entry.Version != 2 {
		t.Fatalf("entry version = %d, want 2", entry.Version)
	}
	if err := db.Model(&models.Component{}).Where("catalog_entry_id = ? AND component_status = ?", entry.ID, models.SourceStatusMissing).Count(&componentCount).Error; err != nil {
		t.Fatalf("count missing components: %v", err)
	}
	if componentCount != 2 {
		t.Fatalf("missing component count = %d, want 2", componentCount)
	}

	if err := db.Model(&models.SourceCheckpoint{}).Where("tenant_id = ?", 7).Update("cursor", "").Error; err != nil {
		t.Fatalf("reset checkpoint: %v", err)
	}
	if err := syncService.SyncTenant(context.Background(), 7); err != nil {
		t.Fatalf("replay old change: %v", err)
	}
	if err := db.First(&binding, "id = ?", binding.ID).Error; err != nil {
		t.Fatalf("reload binding: %v", err)
	}
	if binding.SourceStatus != models.SourceStatusMissing || binding.SourceVersion != "00000000000000000002" {
		t.Fatalf("old replay rolled source backward: %#v", binding)
	}
	var entryCount int64
	if err := db.Model(&models.Entry{}).Count(&entryCount).Error; err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if entryCount != 1 {
		t.Fatalf("entry count = %d, want stable identity", entryCount)
	}
}

func TestSourceSyncRollsBackWholeBatchAndCheckpoint(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	now := time.Now().UTC()
	source := &fakeMetaChangeSource{responses: map[string]*commonClient.MetaDataItemChangesResponse{
		"": changeBatch("cursor-2", false,
			commonClient.MetaDataItemChange{
				ChangeID: "change-1", Operation: "upsert", SourceIdentity: "valid-fingerprint",
				SourceVersion: "00000000000000000001", ObservedAt: now, Snapshot: map[string]interface{}{"name": "orders"},
			},
			commonClient.MetaDataItemChange{
				ChangeID: "change-2", Operation: "upsert", SourceIdentity: "",
				SourceVersion: "00000000000000000002", ObservedAt: now, Snapshot: map[string]interface{}{"name": "invalid"},
			},
		),
	}}
	if err := NewSourceSyncService(db, source).SyncTenant(context.Background(), 7); err == nil {
		t.Fatal("sync should reject malformed change")
	}
	var entryCount int64
	if err := db.Model(&models.Entry{}).Count(&entryCount).Error; err != nil {
		t.Fatalf("count entries: %v", err)
	}
	if entryCount != 0 {
		t.Fatalf("entry count = %d, want transaction rollback", entryCount)
	}
	var checkpointCount int64
	if err := db.Model(&models.SourceCheckpoint{}).Count(&checkpointCount).Error; err != nil {
		t.Fatalf("count checkpoints: %v", err)
	}
	if checkpointCount != 0 {
		t.Fatalf("checkpoint count = %d, want transaction rollback", checkpointCount)
	}
}

func changeBatch(nextCursor string, hasMore bool, changes ...commonClient.MetaDataItemChange) *commonClient.MetaDataItemChangesResponse {
	return &commonClient.MetaDataItemChangesResponse{
		SchemaVersion: "meta.data_item_changes/v1", Changes: changes,
		NextCursor: nextCursor, HasMore: hasMore,
	}
}

func openCatalogServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:catalog-service-%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open SQLite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS catalog").Error; err != nil {
		t.Fatalf("attach catalog schema: %v", err)
	}
	statements := []string{
		`CREATE TABLE catalog.entries (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, entry_type TEXT NOT NULL, entry_status TEXT NOT NULL,
			merged_into_entry_id TEXT, recommended_successor_entry_id TEXT, business_name TEXT, business_description TEXT, governance_status TEXT NOT NULL,
			visibility TEXT NOT NULL, version INTEGER NOT NULL, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE catalog.source_bindings (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, catalog_entry_id TEXT NOT NULL, source_module TEXT NOT NULL,
			source_type TEXT NOT NULL, source_identity TEXT NOT NULL, source_status TEXT NOT NULL, source_version TEXT NOT NULL,
			is_current NUMERIC NOT NULL, bound_at DATETIME NOT NULL, missing_at DATETIME, replaced_binding_id TEXT,
			missing_reason TEXT, observed_snapshot JSON NOT NULL, observed_at DATETIME NOT NULL, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE catalog.components (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, catalog_entry_id TEXT NOT NULL, component_key TEXT NOT NULL,
			display_name TEXT NOT NULL, data_type TEXT, component_status TEXT NOT NULL, ordinal INTEGER NOT NULL,
			observed_snapshot JSON NOT NULL, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE catalog.source_checkpoints (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, source_module TEXT NOT NULL,
			feed_name TEXT NOT NULL, cursor TEXT NOT NULL, updated_at DATETIME)`,
		`CREATE TABLE catalog.projection_tasks (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, catalog_entry_id TEXT NOT NULL,
			projection TEXT NOT NULL, status TEXT NOT NULL, attempt_count INTEGER NOT NULL, available_at DATETIME NOT NULL,
			created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE catalog.audit_events (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, catalog_entry_id TEXT NOT NULL, event_type TEXT NOT NULL,
			actor_type TEXT NOT NULL, actor_id TEXT NOT NULL, details JSON NOT NULL, created_at DATETIME)`,
		`CREATE TABLE catalog.responsibilities (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, catalog_entry_id TEXT NOT NULL, role TEXT NOT NULL,
			subject_type TEXT NOT NULL, subject_id INTEGER NOT NULL, status TEXT NOT NULL, observed_snapshot JSON NOT NULL,
			verified_at DATETIME NOT NULL, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE catalog.governance_tasks (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, catalog_entry_id TEXT NOT NULL, task_type TEXT NOT NULL,
			responsibility_role TEXT NOT NULL, subject_type TEXT NOT NULL, subject_id INTEGER NOT NULL,
			status TEXT NOT NULL, reason TEXT NOT NULL, observed_snapshot JSON NOT NULL, opened_at DATETIME NOT NULL,
			resolved_at DATETIME, resolution TEXT, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE catalog.entry_marks (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, user_id INTEGER NOT NULL, catalog_entry_id TEXT NOT NULL,
			mark_type TEXT NOT NULL, created_at DATETIME)`,
		`CREATE TABLE catalog.collections (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, project_group_id INTEGER NOT NULL, name TEXT NOT NULL,
			description TEXT NOT NULL, version INTEGER NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE catalog.collection_entries (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, collection_id TEXT NOT NULL, catalog_entry_id TEXT NOT NULL,
			added_by INTEGER NOT NULL, created_at DATETIME)`,
		`CREATE TABLE catalog.collection_audit_events (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, collection_id TEXT NOT NULL, event_type TEXT NOT NULL,
			actor_id INTEGER NOT NULL, details JSON NOT NULL, created_at DATETIME NOT NULL)`,
		`CREATE TABLE catalog.semantic_associations (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, catalog_entry_id TEXT NOT NULL, semantic_type TEXT NOT NULL,
			semantic_id INTEGER NOT NULL, relation_role TEXT NOT NULL, observed_version INTEGER NOT NULL,
			observed_snapshot JSON NOT NULL, verified_at DATETIME NOT NULL, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE catalog.component_element_associations (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, catalog_entry_id TEXT NOT NULL, component_id TEXT NOT NULL,
			element_id INTEGER NOT NULL, observed_version INTEGER NOT NULL, observed_snapshot JSON NOT NULL,
			verified_at DATETIME NOT NULL, created_at DATETIME, updated_at DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create Catalog test table: %v", err)
		}
	}
	return db
}
