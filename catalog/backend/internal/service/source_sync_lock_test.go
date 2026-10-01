package service

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	commonClient "github.com/addp/common/client"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestSourceSyncLocksAggregatesBeforeBindings(t *testing.T) {
	assertSourceSyncLockOrder(t, openCatalogServiceTestDB(t))
}

// The same contract runs against SQLite T1 and the registered PostgreSQL T2.
func assertSourceSyncLockOrder(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, module := range []string{models.SourceModuleMeta, models.SourceModuleModel} {
		func() {
			now := time.Now().UTC()
			var bindings []models.SourceBinding
			for _, identity := range []string{"100001", "100002"} {
				entry, _ := createEditableCatalogEntry(t, db, 7)
				if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ?", entry.ID).Updates(map[string]any{
					"source_module": module, "source_identity": identity, "source_type": sourceTypeForLockTest(module),
				}).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Model(&models.Entry{}).Where("id = ?", entry.ID).Updates(map[string]any{
					"entry_type":        entryTypeForLockTest(module),
					"governance_status": models.GovernanceStatusDeprecated, "business_responsibility_established": true,
				}).Error; err != nil {
					t.Fatal(err)
				}
				var binding models.SourceBinding
				if err := db.Where("catalog_entry_id = ?", entry.ID).First(&binding).Error; err != nil {
					t.Fatal(err)
				}
				bindings = append(bindings, binding)
			}
			// Event order must not become lock order: reverse the aggregate UUIDs.
			sort.Slice(bindings, func(i, j int) bool { return bindings[i].CatalogEntryID.String() > bindings[j].CatalogEntryID.String() })
			locks := []string{}
			callback := "test:source_sync_lock_order"
			if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
				locking, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking)
				if !ok || locking.Strength != "UPDATE" || tx.Statement.Schema == nil {
					return
				}
				table := tx.Statement.Schema.Table
				if table != (models.Entry{}).TableName() && table != (models.SourceBinding{}).TableName() {
					return
				}
				locks = append(locks, table)
				order, ok := tx.Statement.Clauses["ORDER BY"].Expression.(clause.OrderBy)
				if !ok || len(order.Columns) != 1 || order.Columns[0].Column.Name != "id ASC" {
					t.Errorf("%s locks are not in stable UUID order: %#v", table, order)
				}
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Query().Remove(callback)
			if module == models.SourceModuleMeta {
				changes := []commonClient.MetaDataItemChange{}
				for _, binding := range bindings {
					changes = append(changes, commonClient.MetaDataItemChange{Operation: "upsert", SourceIdentity: binding.SourceIdentity,
						SourceVersion: "00000000000000000002", ObservedAt: now, Snapshot: map[string]any{"name": "Observed"}})
				}
				svc := NewSourceSyncService(db, nil)
				if err := svc.applyBatch(context.Background(), 7, "", changeBatch("lock-meta", false, changes...)); err != nil {
					t.Fatal(err)
				}
			} else {
				changes := []ProfessionalResourceChange{}
				for _, binding := range bindings {
					changes = append(changes, ProfessionalResourceChange{SourceType: binding.SourceType, SourceIdentity: binding.SourceIdentity,
						Operation: "upsert", SourceVersion: "00000000000000000002", ObservedAt: now, Snapshot: map[string]any{"name": "Observed"}})
				}
				svc := NewProfessionalSourceSyncService(db, &fakeModelChangeSource{module: module, name: "Model"})
				if err := svc.applyBatch(context.Background(), 7, "", &ProfessionalChangeBatch{NextCursor: "lock-model", Changes: changes}); err != nil {
					t.Fatal(err)
				}
			}
			if want := []string{(models.Entry{}).TableName(), (models.SourceBinding{}).TableName()}; !reflect.DeepEqual(locks, want) {
				t.Errorf("%s locked %v, want %v before applying any event", module, locks, want)
			}
			for _, binding := range bindings {
				assertBusinessResponsibilityEvidence(t, db, binding.CatalogEntryID, boolEvidence(true))
				assertEntryVersionAndGovernance(t, db, binding.CatalogEntryID, 2, models.GovernanceStatusDeprecated)
			}
		}()
	}
}

func sourceTypeForLockTest(module string) string {
	if module == models.SourceModuleMeta {
		return models.SourceTypeDataItem
	}
	return models.SourceTypeLogicalTable
}

func entryTypeForLockTest(module string) string {
	if module == models.SourceModuleMeta {
		return models.EntryTypeDataItem
	}
	return models.EntryTypeLogicalModel
}

func TestSourceSyncRejectsBindingMovedOutsideLockedEntries(t *testing.T) {
	assertSourceSyncRejectsMovedBinding(t, openCatalogServiceTestDB(t))
}

func assertSourceSyncRejectsMovedBinding(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, module := range []string{models.SourceModuleMeta, models.SourceModuleModel} {
		func() {
			const tenantID = 701
			entry, _ := createEditableCatalogEntry(t, db, tenantID)
			other, _ := createEditableCatalogEntry(t, db, tenantID)
			if err := db.Model(&models.Entry{}).Where("id IN ?", []any{entry.ID, other.ID}).UpdateColumn("entry_type", entryTypeForLockTest(module)).Error; err != nil {
				t.Fatal(err)
			}
			var binding models.SourceBinding
			if err := db.Where("catalog_entry_id = ?", entry.ID).First(&binding).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Model(&binding).Updates(map[string]any{"source_module": module, "source_type": sourceTypeForLockTest(module), "source_identity": "200001"}).Error; err != nil {
				t.Fatal(err)
			}
			moved := false
			callback := "test:move_binding_before_entry_lock"
			// Deterministically interleave a move after read-only location. This
			// tests revalidation, not a real concurrent connection schedule.
			if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
				locking, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking)
				if moved || !ok || locking.Strength != "UPDATE" || tx.Statement.Schema == nil || tx.Statement.Schema.Table != (models.Entry{}).TableName() {
					return
				}
				moved = true
				// Rebinding closes the target's old current binding before
				// moving the replacement, preserving PostgreSQL uniqueness.
				if err := tx.Session(&gorm.Session{NewDB: true}).Model(&models.SourceBinding{}).
					Where("catalog_entry_id = ? AND is_current = ?", other.ID, true).UpdateColumn("is_current", false).Error; err != nil {
					tx.AddError(err)
					return
				}
				tx.AddError(tx.Session(&gorm.Session{NewDB: true}).Model(&models.SourceBinding{}).
					Where("id = ?", binding.ID).UpdateColumn("catalog_entry_id", other.ID).Error)
			}); err != nil {
				t.Fatal(err)
			}
			defer db.Callback().Query().Remove(callback)
			now := time.Now().UTC()
			var err error
			if module == models.SourceModuleMeta {
				err = NewSourceSyncService(db, nil).applyBatch(context.Background(), tenantID, "", changeBatch("moved", false,
					commonClient.MetaDataItemChange{SourceIdentity: "200001", Operation: "upsert", SourceVersion: "00000000000000000002", ObservedAt: now, Snapshot: map[string]any{"name": "Changed"}}))
			} else {
				err = NewProfessionalSourceSyncService(db, &fakeModelChangeSource{module: module, name: "Model"}).applyBatch(context.Background(), tenantID, "",
					&ProfessionalChangeBatch{NextCursor: "moved", Changes: []ProfessionalResourceChange{{SourceType: sourceTypeForLockTest(module), SourceIdentity: "200001",
						Operation: "upsert", SourceVersion: "00000000000000000002", ObservedAt: now, Snapshot: map[string]any{"name": "Changed"}}}})
			}
			if !moved || err == nil || !strings.Contains(err.Error(), "source bindings changed concurrently") {
				t.Fatalf("%s moved=%v, error=%v, want rejected moved binding", module, moved, err)
			}
			var reloaded models.SourceBinding
			if err := db.Where("id = ?", binding.ID).First(&reloaded).Error; err != nil {
				t.Fatal(err)
			}
			if reloaded.CatalogEntryID != entry.ID || reloaded.SourceVersion != "00000000000000000001" {
				t.Fatalf("failed batch changed binding: %#v", reloaded)
			}
			var count int64
			if err := db.Model(&models.SourceCheckpoint{}).Where("tenant_id = ? AND source_module = ?", tenantID, module).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("failed batch left checkpoint: count=%d err=%v", count, err)
			}
			assertEntryVersionAndGovernance(t, db, entry.ID, 1, models.GovernanceStatusDiscovered)
			assertEntryVersionAndGovernance(t, db, other.ID, 1, models.GovernanceStatusDiscovered)
		}()
	}
}

func TestSourceSyncRepeatedIdentityWithinBatch(t *testing.T) {
	assertSourceSyncRepeatedIdentity(t, openCatalogServiceTestDB(t))
}

func assertSourceSyncRepeatedIdentity(t *testing.T, db *gorm.DB) {
	t.Helper()
	const tenantID = 702
	now := time.Now().UTC()
	metaChanges := []commonClient.MetaDataItemChange{}
	for _, version := range []string{"00000000000000000001", "00000000000000000002", "00000000000000000002", "00000000000000000001"} {
		metaChanges = append(metaChanges, commonClient.MetaDataItemChange{SourceIdentity: "300001", Operation: "upsert", SourceVersion: version, ObservedAt: now, Snapshot: map[string]any{"name": version}})
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return applyMetaDataItemChanges(tx, tenantID, metaChanges...) }); err != nil {
		t.Fatal(err)
	}
	professionalChanges := []ProfessionalResourceChange{}
	// One numeric identity can name both an entity and a logical table. Batch
	// deduplication must keep source type in its key, and skip older versions.
	for _, sourceType := range []string{models.SourceTypeEntity, models.SourceTypeLogicalTable} {
		for _, version := range []string{"00000000000000000001", "00000000000000000002", "00000000000000000002", "00000000000000000001"} {
			professionalChanges = append(professionalChanges, ProfessionalResourceChange{SourceType: sourceType, SourceIdentity: "300001", Operation: "upsert", SourceVersion: version,
				ObservedAt: now, Snapshot: map[string]any{"name": version}})
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return applyProfessionalResourceChanges(tx, tenantID, models.SourceModuleModel, professionalChanges...)
	}); err != nil {
		t.Fatal(err)
	}
	var bindings []models.SourceBinding
	if err := db.Where("tenant_id = ?", tenantID).Find(&bindings).Error; err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 3 {
		t.Fatalf("batch created %d bindings, want three distinct source references", len(bindings))
	}
	for _, binding := range bindings {
		if binding.SourceVersion != "00000000000000000002" || binding.ObservedSnapshot["name"] != "00000000000000000002" {
			t.Fatalf("replayed event rolled binding back: %#v", binding)
		}
		assertEntryVersionAndGovernance(t, db, binding.CatalogEntryID, 2, models.GovernanceStatusDiscovered)
		assertBusinessResponsibilityEvidence(t, db, binding.CatalogEntryID, boolEvidence(false))
		var count int64
		if err := db.Model(&models.ProjectionTask{}).Where("catalog_entry_id = ?", binding.CatalogEntryID).Count(&count).Error; err != nil || count != 2 {
			t.Fatalf("replay enqueued projection: count=%d err=%v", count, err)
		}
	}
}
