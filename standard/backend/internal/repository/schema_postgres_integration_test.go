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
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`DROP SCHEMA IF EXISTS standard CASCADE`,
		`CREATE SCHEMA standard`,
		`CREATE TABLE standard.code_sets (
			id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL, code VARCHAR(100) NOT NULL,
			name VARCHAR(200) NOT NULL, type VARCHAR(20), description TEXT,
			created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ, version BIGINT NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE standard.code_items (
			id BIGSERIAL PRIMARY KEY, code_set_id BIGINT NOT NULL, code VARCHAR(100) NOT NULL,
			value VARCHAR(200) NOT NULL, description TEXT, sort_order INTEGER, is_active BOOLEAN,
			parent_id BIGINT, created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ
		)`,
		`CREATE TABLE standard.elements (
			id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL, domain_id BIGINT,
			name VARCHAR(200) NOT NULL, code VARCHAR(100) NOT NULL, data_type VARCHAR(50) NOT NULL,
			length INTEGER, precision_num INTEGER, scale INTEGER, nullable BOOLEAN,
			default_value TEXT, format VARCHAR(200), value_range JSONB, unit_id BIGINT,
			security_level VARCHAR(10), classification_id BIGINT, code_set_id BIGINT,
			definition TEXT, example_values JSONB, quality_rules JSONB, status VARCHAR(20),
			steward_id BIGINT, tags JSONB, created_by BIGINT NOT NULL, updated_by BIGINT,
			created_at TIMESTAMPTZ, updated_at TIMESTAMPTZ, version BIGINT NOT NULL DEFAULT 1,
			lifecycle_state VARCHAR(16) NOT NULL DEFAULT 'active'
		)`,
		`INSERT INTO standard.code_sets (id, tenant_id, code, name, type, description, version)
		 VALUES (101, 7, 'gender', 'Gender', 'custom', 'Gender codes', 3)`,
		`INSERT INTO standard.code_items (id, code_set_id, code, value, description, sort_order, is_active)
		 VALUES (1001, 101, 'M', 'Male', 'Male gender', 1, TRUE)`,
		`INSERT INTO standard.elements (
			id, tenant_id, name, code, data_type, nullable, code_set_id, definition,
			example_values, quality_rules, status, created_by, version
		) VALUES (
			201, 7, 'Gender', 'gender', 'string', FALSE, 101, 'Customer gender',
			'["M"]'::jsonb,
			'{"schema_version":"addp.quality.rules/v1","rules":[]}'::jsonb,
			'approved', 1, 4
		)`,
	} {
		if err := tx.Exec(statement).Error; err != nil {
			t.Fatalf("prepare legacy schema with %q: %v", statement, err)
		}
	}
	if err := Migrate(tx); err != nil {
		t.Fatalf("Migrate() legacy standard schema error = %v", err)
	}
	if err := Migrate(tx); err != nil {
		t.Fatalf("second Migrate() should be idempotent: %v", err)
	}
	var migrated struct {
		ElementRevisionID int64
		ElementStatus     string
		CodeSetRevisionID int64
		CodeSetStatus     string
		ItemLabel         string
	}
	if err := tx.Raw(`SELECT er.id AS element_revision_id, er.status AS element_status,
		csr.id AS code_set_revision_id, csr.status AS code_set_status, item.label AS item_label
		FROM standard.elements e
		JOIN standard.element_revisions er ON er.id = e.current_revision_id
		JOIN standard.code_set_revisions csr ON csr.id = er.code_set_revision_id
		JOIN standard.code_set_revision_items item ON item.code_set_revision_id = csr.id
		WHERE e.id = 201`).Scan(&migrated).Error; err != nil {
		t.Fatalf("load migrated revision graph: %v", err)
	}
	if migrated.ElementRevisionID == 0 || migrated.ElementStatus != models.RevisionStatusPublished || migrated.CodeSetRevisionID == 0 || migrated.CodeSetStatus != models.RevisionStatusPublished || migrated.ItemLabel != "Male" {
		t.Fatalf("migrated revision graph = %#v", migrated)
	}
	var legacyColumns int64
	if err := tx.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = 'standard' AND ((table_name = 'elements' AND column_name IN ('name','data_type','status','quality_rules','code_set_id')) OR (table_name = 'code_sets' AND column_name IN ('name','type','description')))`).Scan(&legacyColumns).Error; err != nil {
		t.Fatalf("query legacy columns: %v", err)
	}
	if legacyColumns != 0 {
		t.Fatalf("legacy standard columns remaining = %d", legacyColumns)
	}
	var legacyItemTable int64
	if err := tx.Raw(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'standard' AND table_name = 'code_items'`).Scan(&legacyItemTable).Error; err != nil {
		t.Fatalf("query legacy code item table: %v", err)
	}
	if legacyItemTable != 0 {
		t.Fatal("legacy standard.code_items table still exists")
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
		created_by BIGINT NOT NULL,
		version VARCHAR(50)
	)`).Error; err != nil {
		t.Fatalf("create legacy documents table: %v", err)
	}
	if err := tx.Exec(`INSERT INTO standard.documents (tenant_id, name, created_by, version) VALUES (7, 'Legacy document', 1, '2025-R2')`).Error; err != nil {
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
	element := models.Element{TenantID: tenantID, Code: "delete-policy-element", CreatedBy: 1, LifecycleState: "active"}
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
