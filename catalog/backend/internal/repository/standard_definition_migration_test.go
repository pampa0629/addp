package repository

import (
	"errors"
	"github.com/addp/catalog/internal/models"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestStandardDefinitionMigrationArchivesExactTextAndIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS catalog").Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE catalog.entries (id TEXT PRIMARY KEY,tenant_id INTEGER,entry_type TEXT,entry_status TEXT,merged_into_entry_id TEXT,recommended_successor_entry_id TEXT,business_name TEXT,business_description TEXT,governance_status TEXT,business_responsibility_established BOOLEAN,visibility TEXT,version INTEGER,created_at DATETIME,updated_at DATETIME)`,
		`CREATE TABLE catalog.source_bindings (id TEXT PRIMARY KEY,tenant_id INTEGER,catalog_entry_id TEXT,source_module TEXT,source_type TEXT,source_identity TEXT,source_status TEXT,source_version TEXT,is_current NUMERIC,bound_at DATETIME,missing_at DATETIME,replaced_binding_id TEXT,missing_reason TEXT,observed_snapshot JSON,observed_at DATETIME,created_at DATETIME,updated_at DATETIME)`,
		`CREATE TABLE catalog.audit_events (id TEXT PRIMARY KEY,tenant_id INTEGER,catalog_entry_id TEXT,event_type TEXT,actor_type TEXT,actor_id TEXT,details JSON,created_at DATETIME)`,
		`CREATE TABLE catalog.projection_tasks (id INTEGER PRIMARY KEY AUTOINCREMENT,tenant_id INTEGER,catalog_entry_id TEXT,projection TEXT,status TEXT,attempt_count INTEGER,available_at DATETIME,created_at DATETIME,updated_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}

	now := time.Now().UTC()
	name, description := "人工名称  \n", "原始说明\n😀  "
	entry := models.Entry{ID: uuid.New(), TenantID: 7, EntryType: "metric", EntryStatus: "active", GovernanceStatus: "curated", Visibility: "tenant", Version: 9, BusinessName: &name, BusinessDescription: &description, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatal(err)
	}
	source := models.SourceBinding{ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, SourceModule: "standard", SourceType: "metric", SourceIdentity: "21", SourceVersion: "00000000000000000001", SourceStatus: "active", IsCurrent: true, ObservedSnapshot: commonModels.JSONMap{"name": "Owner"}, ObservedAt: now, BoundAt: now}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:reject_archive", func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" {
			tx.AddError(errors.New("archive unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(migrateStandardDefinitionOwnership); err == nil {
		t.Fatal("migration cleared active definitions without history")
	}
	db.Callback().Create().Remove("test:reject_archive")
	var unchanged models.Entry
	if err := db.First(&unchanged, "id=?", entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.BusinessName == nil || *unchanged.BusinessName != name || unchanged.Version != 9 {
		t.Fatal("failed archive changed the entry")
	}
	for i := 0; i < 2; i++ {
		if err := db.Transaction(migrateStandardDefinitionOwnership); err != nil {
			t.Fatal(err)
		}
	}
	var restored models.Entry
	if err := db.First(&restored, "id=?", entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if restored.BusinessName != nil || restored.BusinessDescription != nil || restored.Version != 10 || restored.GovernanceStatus != "curated" {
		t.Fatalf("entry=%+v", restored)
	}
	var audits []models.AuditEvent
	if err := db.Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 1 || audits[0].Details["business_name"] != name || audits[0].Details["business_description"] != description || audits[0].Details["source_identity"] != "21" {
		t.Fatalf("history=%+v", audits)
	}
	// An unresolvable historical owner must block clearing, without guessing.
	ambiguous := entry
	ambiguous.ID = uuid.New()
	ambiguous.Version = 1
	if err := db.Create(&ambiguous).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(migrateStandardDefinitionOwnership); err == nil {
		t.Fatal("missing source was silently discarded")
	}
	var retained models.Entry
	if err := db.First(&retained, "id=?", ambiguous.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retained.BusinessName == nil || *retained.BusinessName != name || retained.BusinessDescription == nil || *retained.BusinessDescription != description || retained.Version != 1 {
		t.Fatal("ambiguous source lost its original definition")
	}

}
