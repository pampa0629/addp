package repository

import (
	"errors"
	"os"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMigrateAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate() should be idempotent: %v", err)
	}

	for _, name := range []string{
		"uq_standard_domains_tenant_code",
		"uq_standard_code_items_set_code",
		"uq_standard_metric_dependencies_from_to",
		"uq_standard_dimension_hierarchy_levels_hierarchy_level",
	} {
		var count int64
		if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = 'standard' AND indexname = ?`, name).Scan(&count).Error; err != nil {
			t.Fatalf("query index %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("index %s count = %d, want 1", name, count)
		}
	}
	for _, name := range []string{
		"ck_standard_metric_dependencies_distinct",
		"ck_standard_dimension_hierarchy_levels_level_num",
	} {
		var count int64
		if err := db.Raw(`SELECT COUNT(*) FROM pg_constraint WHERE connamespace = 'standard'::regnamespace AND conname = ?`, name).Scan(&count).Error; err != nil {
			t.Fatalf("query constraint %s: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("constraint %s count = %d, want 1", name, count)
		}
	}
	for name, deleteAction := range map[string]string{
		"fk_standard_domains_parent":                       "r",
		"fk_standard_glossary_element_mappings_element":    "c",
		"fk_standard_metric_dependencies_to":               "r",
		"fk_standard_document_metric_mappings_metric":      "c",
		"fk_standard_dimension_hierarchy_levels_element":   "n",
		"fk_standard_dimension_hierarchy_levels_hierarchy": "c",
	} {
		var actual string
		if err := db.Raw(`SELECT confdeltype::text FROM pg_constraint WHERE connamespace = 'standard'::regnamespace AND conname = ?`, name).Scan(&actual).Error; err != nil {
			t.Fatalf("query constraint %s: %v", name, err)
		}
		if actual != deleteAction {
			t.Fatalf("constraint %s delete action = %q, want %q", name, actual, deleteAction)
		}
	}
	for _, legacyName := range []string{
		"fk_standard_dimension_hierarchies_levels",
		"fk_standard_units_category",
		"units_category_id_fkey",
	} {
		var count int64
		if err := db.Raw(`SELECT COUNT(*) FROM pg_constraint WHERE connamespace = 'standard'::regnamespace AND conname = ?`, legacyName).Scan(&count).Error; err != nil {
			t.Fatalf("query legacy constraint %s: %v", legacyName, err)
		}
		if count != 0 {
			t.Fatalf("legacy constraint %s count = %d, want 0", legacyName, count)
		}
	}
}

func TestMigrateRenamesLegacyDocumentVersion(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()

	if err := tx.Exec(`DROP SCHEMA IF EXISTS standard CASCADE`).Error; err != nil {
		t.Fatalf("drop standard schema in test transaction: %v", err)
	}
	if err := tx.Exec(`CREATE SCHEMA standard`).Error; err != nil {
		t.Fatalf("create standard schema in test transaction: %v", err)
	}
	if err := tx.Exec(`CREATE TABLE standard.documents (
		id BIGSERIAL PRIMARY KEY,
		tenant_id BIGINT NOT NULL,
		name VARCHAR(200) NOT NULL,
		version VARCHAR(50)
	)`).Error; err != nil {
		t.Fatalf("create legacy documents table: %v", err)
	}
	if err := tx.Exec(`INSERT INTO standard.documents (tenant_id, name, version) VALUES (7, 'Legacy document', '2025-R2')`).Error; err != nil {
		t.Fatalf("insert legacy document: %v", err)
	}

	if err := Migrate(tx); err != nil {
		t.Fatalf("Migrate() legacy documents error = %v", err)
	}

	var columns []struct {
		ColumnName string
		DataType   string
	}
	if err := tx.Raw(`SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'standard' AND table_name = 'documents'
		AND column_name IN ('document_version', 'version')
		ORDER BY column_name`).Scan(&columns).Error; err != nil {
		t.Fatalf("query migrated document columns: %v", err)
	}
	if len(columns) != 2 || columns[0].ColumnName != "document_version" || columns[0].DataType != "character varying" || columns[1].ColumnName != "version" || columns[1].DataType != "bigint" {
		t.Fatalf("migrated document columns = %#v", columns)
	}

	var migrated struct {
		DocumentVersion string
		Version         int64
	}
	if err := tx.Raw(`SELECT document_version, version FROM standard.documents WHERE tenant_id = 7`).Scan(&migrated).Error; err != nil {
		t.Fatalf("load migrated document: %v", err)
	}
	if migrated.DocumentVersion != "2025-R2" || migrated.Version != 1 {
		t.Fatalf("migrated document = %#v, want document_version 2025-R2 and version 1", migrated)
	}
}

func TestPostgresDeletePolicies(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	tenantID := int64(9_000_000_001)
	if err := db.Exec("DELETE FROM standard.domains WHERE tenant_id = ?", tenantID).Error; err != nil {
		t.Fatalf("clear test domains: %v", err)
	}
	parent := models.Domain{TenantID: tenantID, Name: "parent", Code: "delete-policy-parent", CreatedBy: 1}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent domain: %v", err)
	}
	child := models.Domain{TenantID: tenantID, Name: "child", Code: "delete-policy-child", ParentID: &parent.ID, CreatedBy: 1}
	if err := db.Create(&child).Error; err != nil {
		t.Fatalf("create child domain: %v", err)
	}

	err = NewDomainRepository(db).Delete(parent.ID, tenantID)
	if !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("Delete(parent) error = %v, want ErrConflict", err)
	}
	if err := NewDomainRepository(db).Delete(child.ID, tenantID); err != nil {
		t.Fatalf("delete child domain: %v", err)
	}
	if err := NewDomainRepository(db).Delete(parent.ID, tenantID); err != nil {
		t.Fatalf("delete unreferenced parent domain: %v", err)
	}

	glossary := models.Glossary{TenantID: tenantID, Name: "glossary", Definition: "delete policy", CreatedBy: 1}
	if err := db.Create(&glossary).Error; err != nil {
		t.Fatalf("create glossary: %v", err)
	}
	element := models.Element{TenantID: tenantID, Name: "element", Code: "delete-policy-element", DataType: "string", CreatedBy: 1}
	if err := db.Create(&element).Error; err != nil {
		t.Fatalf("create element: %v", err)
	}
	if err := db.Create(&models.GlossaryElementMapping{GlossaryID: glossary.ID, ElementID: element.ID}).Error; err != nil {
		t.Fatalf("create glossary-element mapping: %v", err)
	}
	hierarchy := models.DimensionHierarchy{TenantID: tenantID, Name: "hierarchy", Code: "delete-policy-hierarchy", CreatedBy: 1}
	if err := db.Create(&hierarchy).Error; err != nil {
		t.Fatalf("create dimension hierarchy: %v", err)
	}
	level := models.DimensionHierarchyLevel{HierarchyID: hierarchy.ID, LevelNum: 1, Name: "level", ElementID: &element.ID}
	if err := db.Create(&level).Error; err != nil {
		t.Fatalf("create hierarchy level: %v", err)
	}
	if err := NewElementRepository(db).Delete(element.ID, tenantID); err != nil {
		t.Fatalf("delete element: %v", err)
	}
	var mappingCount int64
	if err := db.Model(&models.GlossaryElementMapping{}).Where("glossary_id = ? AND element_id = ?", glossary.ID, element.ID).Count(&mappingCount).Error; err != nil {
		t.Fatalf("count glossary-element mapping: %v", err)
	}
	if mappingCount != 0 {
		t.Fatalf("glossary-element mapping count = %d, want 0", mappingCount)
	}
	if err := db.First(&level, level.ID).Error; err != nil {
		t.Fatalf("reload hierarchy level: %v", err)
	}
	if level.ElementID != nil {
		t.Fatalf("hierarchy level element_id = %d, want NULL", *level.ElementID)
	}
	if err := db.Delete(&hierarchy).Error; err != nil {
		t.Fatalf("delete dimension hierarchy: %v", err)
	}
	if err := db.Delete(&glossary).Error; err != nil {
		t.Fatalf("delete glossary: %v", err)
	}
}
