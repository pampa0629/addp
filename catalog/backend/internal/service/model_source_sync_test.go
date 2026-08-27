package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
)

type fakeModelChangeSource struct {
	module  string
	name    string
	schema  string
	batches []*ProfessionalChangeBatch
	index   int
}

func (s *fakeModelChangeSource) SourceModule() string  { return s.module }
func (s *fakeModelChangeSource) SourceName() string    { return s.name }
func (s *fakeModelChangeSource) SchemaVersion() string { return s.schema }

func (s *fakeModelChangeSource) ListCatalogResourceChanges(context.Context, uint, string, int) (*ProfessionalChangeBatch, error) {
	if s.index >= len(s.batches) {
		return &ProfessionalChangeBatch{SchemaVersion: s.schema, NextCursor: "MA"}, nil
	}
	result := s.batches[s.index]
	s.index++
	return result, nil
}

func TestModelSourceSyncCreatesUpdatesAndMarksMissing(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	now := time.Now().UTC()
	source := &fakeModelChangeSource{module: models.SourceModuleModel, name: "Model", schema: "model.catalog_resource_changes/v1", batches: []*ProfessionalChangeBatch{
		{SchemaVersion: "model.catalog_resource_changes/v1", NextCursor: "MQ", Changes: []ProfessionalResourceChange{
			{SourceType: "logical_table", SourceIdentity: "12", Operation: "upsert", SourceVersion: "00000000000000000001", ObservedAt: now, Snapshot: map[string]any{"name": "Orders", "table_type": "fact"}},
		}},
		{SchemaVersion: "model.catalog_resource_changes/v1", NextCursor: "Mg", Changes: []ProfessionalResourceChange{
			{SourceType: "logical_table", SourceIdentity: "12", Operation: "missing", SourceVersion: "00000000000000000002", ObservedAt: now.Add(time.Second), Snapshot: map[string]any{"name": "Orders", "table_type": "fact"}},
		}},
	}}
	syncService := NewProfessionalSourceSyncService(db, source)
	if err := syncService.SyncTenant(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	var entry models.Entry
	if err := db.First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if entry.EntryType != models.EntryTypeLogicalModel || entry.Version != 1 {
		t.Fatalf("entry = %#v", entry)
	}
	if err := syncService.SyncTenant(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	var binding models.SourceBinding
	if err := db.First(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if binding.SourceStatus != models.SourceStatusMissing || binding.SourceVersion != "00000000000000000002" {
		t.Fatalf("binding = %#v", binding)
	}
	if err := db.First(&entry, "id = ?", entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if entry.Version != 2 {
		t.Fatalf("entry version = %d", entry.Version)
	}
	var count int64
	if err := db.Model(&models.Entry{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("entry count = %d, err = %v", count, err)
	}
}

func TestStandardMetricSourceSyncCreatesIndependentEntryAndCheckpoint(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	now := time.Now().UTC()
	source := &fakeModelChangeSource{module: models.SourceModuleStandard, name: "Standard", schema: "standard.catalog_resource_changes/v1", batches: []*ProfessionalChangeBatch{{
		SchemaVersion: "standard.catalog_resource_changes/v1", NextCursor: "MQ", Changes: []ProfessionalResourceChange{{
			SourceType: "metric", SourceIdentity: "21", Operation: "upsert", SourceVersion: "00000000000000000001", ObservedAt: now,
			Snapshot: map[string]any{"name": "Order amount", "metric_type": "atomic", "domain_id": "30"},
		}},
	}}}
	if err := NewProfessionalSourceSyncService(db, source).SyncTenant(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	var entry models.Entry
	if err := db.Where("entry_type = ?", models.EntryTypeMetric).First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	var binding models.SourceBinding
	if err := db.Where("catalog_entry_id = ?", entry.ID).First(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if binding.SourceModule != models.SourceModuleStandard || binding.SourceType != models.SourceTypeMetric || binding.ObservedSnapshot["name"] != "Order amount" {
		t.Fatalf("binding = %#v", binding)
	}
	var checkpoint models.SourceCheckpoint
	if err := db.Where("tenant_id = ? AND source_module = ?", 7, models.SourceModuleStandard).First(&checkpoint).Error; err != nil || checkpoint.Cursor != "MQ" {
		t.Fatalf("checkpoint = %#v, err = %v", checkpoint, err)
	}
}

func TestServiceQueryServiceSourceSyncCreatesDataServiceEntry(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	now := time.Now().UTC()
	source := &fakeModelChangeSource{module: models.SourceModuleService, name: "Service", schema: "service.catalog_resource_changes/v1", batches: []*ProfessionalChangeBatch{{
		SchemaVersion: "service.catalog_resource_changes/v1", NextCursor: "MQ", Changes: []ProfessionalResourceChange{{
			SourceType: "query_service", SourceIdentity: "31", Operation: "upsert", SourceVersion: "00000000000000000001", ObservedAt: now,
			Snapshot: map[string]any{"name": "Orders API", "code": "orders", "service_status": "active"},
		}},
	}}}
	if err := NewProfessionalSourceSyncService(db, source).SyncTenant(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	var entry models.Entry
	if err := db.Where("entry_type = ?", models.EntryTypeDataService).First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	var binding models.SourceBinding
	if err := db.Where("catalog_entry_id = ?", entry.ID).First(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if binding.SourceModule != models.SourceModuleService || binding.SourceType != models.SourceTypeQueryService || binding.ObservedSnapshot["name"] != "Orders API" {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestDevelopDevTaskSourceSyncCreatesDevelopmentArtifactEntry(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	now := time.Now().UTC()
	source := &fakeModelChangeSource{module: models.SourceModuleDevelop, name: "Develop", schema: "develop.catalog_resource_changes/v1", batches: []*ProfessionalChangeBatch{{
		SchemaVersion: "develop.catalog_resource_changes/v1", NextCursor: "MQ", Changes: []ProfessionalResourceChange{{
			SourceType: "dev_task", SourceIdentity: "41", Operation: "upsert", SourceVersion: "00000000000000000001", ObservedAt: now,
			Snapshot: map[string]any{"name": "Orders workflow", "artifact_type": "workflow", "task_status": "active"},
		}},
	}}}
	if err := NewProfessionalSourceSyncService(db, source).SyncTenant(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	var entry models.Entry
	if err := db.Where("entry_type = ?", models.EntryTypeDevelopmentArtifact).First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	var binding models.SourceBinding
	if err := db.Where("catalog_entry_id = ?", entry.ID).First(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if binding.SourceModule != models.SourceModuleDevelop || binding.SourceType != models.SourceTypeDevTask || binding.ObservedSnapshot["artifact_type"] != "workflow" {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestWorkbenchDataApplicationSourceSyncCreatesDataApplicationEntry(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	now := time.Now().UTC()
	applicationID := "d6c30859-15c8-4b88-964b-f2dd315fb923"
	source := &fakeModelChangeSource{module: models.SourceModuleWorkbench, name: "Workbench", schema: "workbench.catalog_resource_changes/v1", batches: []*ProfessionalChangeBatch{
		{
			SchemaVersion: "workbench.catalog_resource_changes/v1", NextCursor: "MQ", Changes: []ProfessionalResourceChange{{
				SourceType: "data_application", SourceIdentity: applicationID, Operation: "upsert", SourceVersion: "00000000000000000001", ObservedAt: now,
				Snapshot: map[string]any{"name": "Orders application", "publication_status": "published", "revision_number": float64(1)},
			}},
		},
		{
			SchemaVersion: "workbench.catalog_resource_changes/v1", NextCursor: "Mg", Changes: []ProfessionalResourceChange{{
				SourceType: "data_application", SourceIdentity: applicationID, Operation: "upsert", SourceVersion: "00000000000000000002", ObservedAt: now.Add(time.Second),
				Snapshot: map[string]any{"name": "Orders application v2", "publication_status": "offline", "revision_number": float64(2)},
			}},
		},
	}}
	syncService := NewProfessionalSourceSyncService(db, source)
	if err := syncService.SyncTenant(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	var entry models.Entry
	if err := db.Where("entry_type = ?", models.EntryTypeDataApplication).First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	var binding models.SourceBinding
	if err := db.Where("catalog_entry_id = ?", entry.ID).First(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if binding.SourceModule != models.SourceModuleWorkbench || binding.SourceType != models.SourceTypeDataApplication || binding.SourceIdentity != applicationID || binding.ObservedSnapshot["name"] != "Orders application" {
		t.Fatalf("binding = %#v", binding)
	}
	if err := syncService.SyncTenant(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&entry, "id = ?", entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&binding, "id = ?", binding.ID).Error; err != nil {
		t.Fatal(err)
	}
	var entryCount int64
	if err := db.Model(&models.Entry{}).Count(&entryCount).Error; err != nil {
		t.Fatal(err)
	}
	if entryCount != 1 || entry.Version != 2 || binding.SourceStatus != models.SourceStatusActive || binding.SourceVersion != "00000000000000000002" || binding.ObservedSnapshot["publication_status"] != "offline" {
		t.Fatalf("republished application entry=%#v binding=%#v count=%d", entry, binding, entryCount)
	}
}
