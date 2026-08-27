package repository

import (
	"os"
	"testing"

	"github.com/addp/catalog/internal/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCatalogMigrateAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("CATALOG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("CATALOG_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("DROP SCHEMA IF EXISTS catalog CASCADE").Error; err != nil {
		t.Fatalf("drop Catalog schema: %v", err)
	}
	if err := Migrate(tx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := Migrate(tx); err != nil {
		t.Fatalf("second Migrate() must be idempotent: %v", err)
	}

	for _, name := range []string{
		"ix_catalog_entries_recommended_successor",
		"uq_catalog_current_source_identity", "uq_catalog_entry_current_source",
		"uq_catalog_component_key", "uq_catalog_checkpoint", "uq_catalog_responsibility",
		"uq_catalog_open_governance_task",
		"uq_catalog_entry_mark",
		"uq_catalog_collection_name", "uq_catalog_collection_entry",
		"uq_catalog_semantic_association", "uq_catalog_primary_domain", "uq_catalog_component_element",
	} {
		var count int64
		if err := tx.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = 'catalog' AND indexname = ?`, name).Scan(&count).Error; err != nil {
			t.Fatalf("query index %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("index %s count = %d, want 1", name, count)
		}
	}
	for _, name := range []string{
		"ck_catalog_entries_merge_shape", "ck_catalog_entries_successor_shape", "ck_catalog_entries_governance_visibility",
		"ck_catalog_source_shape",
		"ck_catalog_semantic_shape", "ck_catalog_responsibility_shape",
		"ck_catalog_governance_task_type", "ck_catalog_governance_task_status", "ck_catalog_governance_task_reason", "ck_catalog_governance_task_resolution",
		"ck_catalog_entry_mark_type",
		"ck_catalog_collection_shape",
		"ck_catalog_collection_entry_shape", "ck_catalog_collection_audit_shape",
		"fk_catalog_entries_recommended_successor", "fk_catalog_source_entry", "fk_catalog_components_entry", "fk_catalog_audit_entry",
		"fk_catalog_governance_tasks_entry",
		"fk_catalog_entry_marks_entry",
		"fk_catalog_collection_entries_collection", "fk_catalog_collection_entries_entry",
		"fk_catalog_semantic_entry", "fk_catalog_component_element_entry", "fk_catalog_component_element_component",
	} {
		var count int64
		if err := tx.Raw(`SELECT COUNT(*) FROM pg_constraint WHERE connamespace = 'catalog'::regnamespace AND conname = ?`, name).Scan(&count).Error; err != nil {
			t.Fatalf("query constraint %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("constraint %s count = %d, want 1", name, count)
		}
	}

	entry := models.Entry{
		ID: uuid.New(), TenantID: 7, EntryType: models.EntryTypeDataItem, EntryStatus: models.EntryStatusActive,
		GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory, Version: 1,
	}
	if err := tx.Create(&entry).Error; err != nil {
		t.Fatalf("insert entry: %v", err)
	}
	binding := models.SourceBinding{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: entry.ID, SourceModule: models.SourceModuleMeta,
		SourceType: models.SourceTypeDataItem, SourceIdentity: "fingerprint-1", SourceStatus: models.SourceStatusActive,
		SourceVersion: "00000000000000000001", IsCurrent: true, BoundAt: entry.CreatedAt,
		ObservedSnapshot: map[string]interface{}{"name": "orders"}, ObservedAt: entry.CreatedAt,
	}
	if err := tx.Create(&binding).Error; err != nil {
		t.Fatalf("insert binding: %v", err)
	}
	modelEntry := models.Entry{
		ID: uuid.New(), TenantID: 7, EntryType: models.EntryTypeLogicalModel, EntryStatus: models.EntryStatusActive,
		GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory, Version: 1,
	}
	if err := tx.Create(&modelEntry).Error; err != nil {
		t.Fatalf("insert Model entry: %v", err)
	}
	modelBinding := models.SourceBinding{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: modelEntry.ID, SourceModule: models.SourceModuleModel,
		SourceType: models.SourceTypeLogicalTable, SourceIdentity: "12", SourceStatus: models.SourceStatusActive,
		SourceVersion: "00000000000000000002", IsCurrent: true, BoundAt: modelEntry.CreatedAt,
		ObservedSnapshot: map[string]interface{}{"name": "orders_model", "domain_id": "31"}, ObservedAt: modelEntry.CreatedAt,
	}
	if err := tx.Create(&modelBinding).Error; err != nil {
		t.Fatalf("insert Model binding: %v", err)
	}
	metricEntry := models.Entry{
		ID: uuid.New(), TenantID: 7, EntryType: models.EntryTypeMetric, EntryStatus: models.EntryStatusActive,
		GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory, Version: 1,
	}
	if err := tx.Create(&metricEntry).Error; err != nil {
		t.Fatalf("insert Metric entry: %v", err)
	}
	metricBinding := models.SourceBinding{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: metricEntry.ID, SourceModule: models.SourceModuleStandard,
		SourceType: models.SourceTypeMetric, SourceIdentity: "21", SourceStatus: models.SourceStatusActive,
		SourceVersion: "00000000000000000003", IsCurrent: true, BoundAt: metricEntry.CreatedAt,
		ObservedSnapshot: map[string]interface{}{"name": "order_amount", "domain_id": "31"}, ObservedAt: metricEntry.CreatedAt,
	}
	if err := tx.Create(&metricBinding).Error; err != nil {
		t.Fatalf("insert Standard Metric binding: %v", err)
	}
	serviceEntry := models.Entry{
		ID: uuid.New(), TenantID: 7, EntryType: models.EntryTypeDataService, EntryStatus: models.EntryStatusActive,
		GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory, Version: 1,
	}
	if err := tx.Create(&serviceEntry).Error; err != nil {
		t.Fatalf("insert Service entry: %v", err)
	}
	serviceBinding := models.SourceBinding{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: serviceEntry.ID, SourceModule: models.SourceModuleService,
		SourceType: models.SourceTypeQueryService, SourceIdentity: "31", SourceStatus: models.SourceStatusActive,
		SourceVersion: "00000000000000000004", IsCurrent: true, BoundAt: serviceEntry.CreatedAt,
		ObservedSnapshot: map[string]interface{}{"name": "orders_api"}, ObservedAt: serviceEntry.CreatedAt,
	}
	if err := tx.Create(&serviceBinding).Error; err != nil {
		t.Fatalf("insert Service QueryService binding: %v", err)
	}
	developEntry := models.Entry{
		ID: uuid.New(), TenantID: 7, EntryType: models.EntryTypeDevelopmentArtifact, EntryStatus: models.EntryStatusActive,
		GovernanceStatus: models.GovernanceStatusDiscovered, Visibility: models.VisibilityInventory, Version: 1,
	}
	if err := tx.Create(&developEntry).Error; err != nil {
		t.Fatalf("insert Develop entry: %v", err)
	}
	developBinding := models.SourceBinding{
		ID: uuid.New(), TenantID: 7, CatalogEntryID: developEntry.ID, SourceModule: models.SourceModuleDevelop,
		SourceType: models.SourceTypeDevTask, SourceIdentity: "41", SourceStatus: models.SourceStatusActive,
		SourceVersion: "00000000000000000005", IsCurrent: true, BoundAt: developEntry.CreatedAt,
		ObservedSnapshot: map[string]interface{}{"name": "orders_workflow", "artifact_type": "workflow"}, ObservedAt: developEntry.CreatedAt,
	}
	if err := tx.Create(&developBinding).Error; err != nil {
		t.Fatalf("insert Develop DevTask binding: %v", err)
	}
	collection := models.Collection{
		ID: uuid.New(), TenantID: 7, ProjectGroupID: 9, Name: "Critical Data", Description: "", Version: 1, CreatedBy: 40,
	}
	if err := tx.Create(&collection).Error; err != nil {
		t.Fatalf("insert collection: %v", err)
	}
	member := models.CollectionEntry{ID: uuid.New(), TenantID: 7, CollectionID: collection.ID, CatalogEntryID: entry.ID, AddedBy: 40}
	if err := tx.Create(&member).Error; err != nil {
		t.Fatalf("insert collection entry: %v", err)
	}
	if err := tx.SavePoint("duplicate_collection_entry").Error; err != nil {
		t.Fatal(err)
	}
	duplicateMember := member
	duplicateMember.ID = uuid.New()
	if err := tx.Create(&duplicateMember).Error; err == nil {
		t.Fatal("duplicate collection entry must be rejected")
	}
	if err := tx.RollbackTo("duplicate_collection_entry").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.SavePoint("duplicate_collection_name").Error; err != nil {
		t.Fatal(err)
	}
	duplicateCollection := collection
	duplicateCollection.ID = uuid.New()
	duplicateCollection.Name = "critical data"
	if err := tx.Create(&duplicateCollection).Error; err == nil {
		t.Fatal("case-insensitive duplicate collection name must be rejected")
	}
	if err := tx.RollbackTo("duplicate_collection_name").Error; err != nil {
		t.Fatal(err)
	}
	secondEntry := entry
	secondEntry.ID = uuid.New()
	if err := tx.Create(&secondEntry).Error; err != nil {
		t.Fatalf("insert second entry: %v", err)
	}
	if err := tx.SavePoint("invalid_successor_shape").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Model(&models.Entry{}).Where("id = ?", entry.ID).Update("recommended_successor_entry_id", secondEntry.ID).Error; err == nil {
		t.Fatal("non-deprecated entry must reject a recommended successor")
	}
	if err := tx.RollbackTo("invalid_successor_shape").Error; err != nil {
		t.Fatal(err)
	}
	duplicate := binding
	duplicate.ID = uuid.New()
	duplicate.CatalogEntryID = secondEntry.ID
	if err := tx.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate current source identity must be rejected")
	}
}
