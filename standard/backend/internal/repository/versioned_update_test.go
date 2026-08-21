package repository

import (
	"errors"
	"fmt"
	"testing"

	commonapi "github.com/addp/common/api"
	"github.com/addp/standard/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openVersionedTestDB(t *testing.T, statements ...string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatalf("attach standard schema: %v", err)
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare schema with %q: %v", statement, err)
		}
	}
	return db
}

func TestDomainDeleteRequiresCurrentVersionAndTenant(t *testing.T) {
	db := openVersionedTestDB(t,
		`CREATE TABLE standard.domains (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, version INTEGER NOT NULL, deleted_at DATETIME)`,
		`INSERT INTO standard.domains (id, tenant_id, version) VALUES (1, 7, 2)`,
	)
	repo := NewDomainRepository(db)

	if err := db.Transaction(func(tx *gorm.DB) error {
		return repo.DeleteVersionedTx(tx, 1, 7, 1)
	}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale delete error = %v, want ErrVersionConflict", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return repo.DeleteVersionedTx(tx, 1, 8, 2)
	}); !errors.Is(err, commonapi.ErrNotFound) {
		t.Fatalf("cross-tenant delete error = %v, want not found", err)
	}
	var count int64
	if err := db.Table("standard.domains").Where("id = ?", 1).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("domain after rejected deletes count=%d error=%v, want 1", count, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return repo.DeleteVersionedTx(tx, 1, 7, 2)
	}); err != nil {
		t.Fatalf("current-version delete: %v", err)
	}
	if err := db.Table("standard.domains").Where("id = ?", 1).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("domain after accepted delete count=%d error=%v, want 0", count, err)
	}
}

func TestDirectResourceUpdatesRejectStaleVersion(t *testing.T) {
	tests := []struct {
		name       string
		createSQL  string
		insertSQL  string
		table      string
		firstValue string
		update     func(*gorm.DB, string, int64) error
	}{
		{
			name: "domain", table: "domains", firstValue: "First domain",
			createSQL: `CREATE TABLE standard.domains (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT, description TEXT, parent_id INTEGER, icon TEXT, sort_order INTEGER, updated_by INTEGER, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1)`,
			insertSQL: `INSERT INTO standard.domains (id, tenant_id, name, version) VALUES (1, 7, 'Original', 1)`,
			update: func(db *gorm.DB, value string, version int64) error {
				return NewDomainRepository(db).Update(&models.Domain{ID: 1, TenantID: 7, Name: value}, version)
			},
		},
		{
			name: "measurement category", table: "measurement_categories", firstValue: "First category",
			createSQL: `CREATE TABLE standard.measurement_categories (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT, description TEXT, sort_order INTEGER, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1)`,
			insertSQL: `INSERT INTO standard.measurement_categories (id, tenant_id, name, version) VALUES (1, 7, 'Original', 1)`,
			update: func(db *gorm.DB, value string, version int64) error {
				return NewMeasurementCategoryRepository(db).Update(&models.MeasurementCategory{ID: 1, TenantID: 7, Name: value}, version)
			},
		},
		{
			name: "unit", table: "units", firstValue: "First unit",
			createSQL: `CREATE TABLE standard.units (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT, symbol TEXT, description TEXT, sort_order INTEGER, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1)`,
			insertSQL: `INSERT INTO standard.units (id, tenant_id, name, version) VALUES (1, 7, 'Original', 1)`,
			update: func(db *gorm.DB, value string, version int64) error {
				return NewUnitRepository(db).Update(&models.Unit{ID: 1, TenantID: 7, Name: value}, version)
			},
		},
		{
			name: "classification", table: "classifications", firstValue: "First classification",
			createSQL: `CREATE TABLE standard.classifications (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT, description TEXT, parent_id INTEGER, sort_order INTEGER, updated_by INTEGER, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1)`,
			insertSQL: `INSERT INTO standard.classifications (id, tenant_id, name, version) VALUES (1, 7, 'Original', 1)`,
			update: func(db *gorm.DB, value string, version int64) error {
				return NewClassificationRepository(db).Update(&models.Classification{ID: 1, TenantID: 7, Name: value}, version)
			},
		},
		{
			name: "grading level", table: "grading_levels", firstValue: "First grading level",
			createSQL: `CREATE TABLE standard.grading_levels (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT, description TEXT, color TEXT, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1)`,
			insertSQL: `INSERT INTO standard.grading_levels (id, tenant_id, name, version) VALUES (1, 7, 'Original', 1)`,
			update: func(db *gorm.DB, value string, version int64) error {
				return NewGradingLevelRepository(db).Update(1, 7, &models.UpdateGradingLevelRequest{Version: version, Name: value})
			},
		},
		{
			name: "metric category", table: "metric_categories", firstValue: "First metric category",
			createSQL: `CREATE TABLE standard.metric_categories (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT, description TEXT, parent_id INTEGER, sort_order INTEGER, updated_by INTEGER, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1)`,
			insertSQL: `INSERT INTO standard.metric_categories (id, tenant_id, name, version) VALUES (1, 7, 'Original', 1)`,
			update: func(db *gorm.DB, value string, version int64) error {
				return NewMetricCategoryRepository(db).Update(&models.MetricCategory{ID: 1, TenantID: 7, Name: value}, version)
			},
		},
		{
			name: "document", table: "documents", firstValue: "First document",
			createSQL: `CREATE TABLE standard.documents (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT, doc_type TEXT, source_org TEXT, document_version TEXT, description TEXT, updated_by INTEGER, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1)`,
			insertSQL: `INSERT INTO standard.documents (id, tenant_id, name, version) VALUES (1, 7, 'Original', 1)`,
			update: func(db *gorm.DB, value string, version int64) error {
				return NewDocumentRepository(db).Update(&models.Document{ID: 1, TenantID: 7, Name: value}, version)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openVersionedTestDB(t, tt.createSQL, tt.insertSQL)
			if err := tt.update(db, tt.firstValue, 1); err != nil {
				t.Fatalf("first update: %v", err)
			}
			if err := tt.update(db, "Stale update", 1); !errors.Is(err, ErrVersionConflict) {
				t.Fatalf("stale update error = %v, want ErrVersionConflict", err)
			}
			var actual struct {
				Name    string
				Version int64
			}
			if err := db.Raw(fmt.Sprintf("SELECT name, version FROM standard.%s WHERE id = 1", tt.table)).Scan(&actual).Error; err != nil {
				t.Fatalf("load current resource: %v", err)
			}
			if actual.Name != tt.firstValue || actual.Version != 2 {
				t.Fatalf("current resource = name %q version %d, want %q version 2", actual.Name, actual.Version, tt.firstValue)
			}
		})
	}
}

func TestElementUpdateRejectsStaleVersion(t *testing.T) {
	db := openVersionedTestDB(t,
		`CREATE TABLE standard.elements (
			id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, domain_id INTEGER, name TEXT, data_type TEXT,
			length INTEGER, precision_num INTEGER, scale INTEGER, nullable BOOLEAN, default_value TEXT, format TEXT,
			value_range TEXT, unit_id INTEGER, security_level TEXT, classification_id INTEGER, code_set_id INTEGER,
			definition TEXT, example_values TEXT, quality_rules TEXT, steward_id INTEGER, tags TEXT, updated_by INTEGER,
			updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1
		)`,
		`INSERT INTO standard.elements (id, tenant_id, name, data_type, version) VALUES (1, 7, 'Original', 'string', 1)`,
	)
	repo := NewElementRepository(db)
	if err := repo.Update(&models.Element{ID: 1, TenantID: 7, Name: "First update", DataType: "string"}, 1); err != nil {
		t.Fatalf("first update element: %v", err)
	}
	if err := repo.Update(&models.Element{ID: 1, TenantID: 7, Name: "Stale update", DataType: "string"}, 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale update element error = %v, want ErrVersionConflict", err)
	}
	var current struct {
		Name    string
		Version int64
	}
	if err := db.Raw(`SELECT name, version FROM standard.elements WHERE id = 1`).Scan(&current).Error; err != nil {
		t.Fatalf("load current element: %v", err)
	}
	if current.Name != "First update" || current.Version != 2 {
		t.Fatalf("current element = name %q version %d, want first update version 2", current.Name, current.Version)
	}
}

func TestCodeItemMutationRejectsStaleParentVersionWithoutSideEffect(t *testing.T) {
	db := openVersionedTestDB(t,
		`CREATE TABLE standard.code_sets (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE standard.code_items (id INTEGER PRIMARY KEY, code_set_id INTEGER NOT NULL, value TEXT, description TEXT, sort_order INTEGER, is_active BOOLEAN, updated_at DATETIME)`,
		`INSERT INTO standard.code_sets (id, tenant_id, version) VALUES (1, 7, 1)`,
		`INSERT INTO standard.code_items (id, code_set_id, value) VALUES (1, 1, 'Original')`,
	)
	repo := NewCodeSetRepository(db)
	first := &models.CodeItem{ID: 1, CodeSetID: 1, Value: "First", IsActive: true}
	if err := repo.UpdateItem(first, 7, 1); err != nil {
		t.Fatalf("first update item: %v", err)
	}
	stale := &models.CodeItem{ID: 1, CodeSetID: 1, Value: "Stale", IsActive: true}
	if err := repo.UpdateItem(stale, 7, 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale update item error = %v, want ErrVersionConflict", err)
	}
	assertParentVersionAndChildValue(t, db, "code_sets", "code_items", "value", "First")
}

func TestHierarchyLevelMutationRejectsStaleParentVersionWithoutSideEffect(t *testing.T) {
	db := openVersionedTestDB(t,
		`CREATE TABLE standard.dimension_hierarchies (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE standard.dimension_hierarchy_levels (id INTEGER PRIMARY KEY, hierarchy_id INTEGER NOT NULL, level_num INTEGER, name TEXT, element_id INTEGER, description TEXT, sort_order INTEGER, updated_at DATETIME)`,
		`INSERT INTO standard.dimension_hierarchies (id, tenant_id, version) VALUES (1, 7, 1)`,
		`INSERT INTO standard.dimension_hierarchy_levels (id, hierarchy_id, level_num, name) VALUES (1, 1, 1, 'Original')`,
	)
	repo := NewDimensionHierarchyRepository(db)
	first := &models.DimensionHierarchyLevel{ID: 1, HierarchyID: 1, LevelNum: 1, Name: "First"}
	if err := repo.UpdateLevel(first, 7, 1); err != nil {
		t.Fatalf("first update level: %v", err)
	}
	stale := &models.DimensionHierarchyLevel{ID: 1, HierarchyID: 1, LevelNum: 1, Name: "Stale"}
	if err := repo.UpdateLevel(stale, 7, 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale update level error = %v, want ErrVersionConflict", err)
	}
	assertParentVersionAndChildValue(t, db, "dimension_hierarchies", "dimension_hierarchy_levels", "name", "First")
}

func assertParentVersionAndChildValue(t *testing.T, db *gorm.DB, parentTable, childTable, childColumn, expectedValue string) {
	t.Helper()
	var version int64
	if err := db.Raw(fmt.Sprintf("SELECT version FROM standard.%s WHERE id = 1", parentTable)).Scan(&version).Error; err != nil {
		t.Fatalf("load parent version: %v", err)
	}
	var value string
	if err := db.Raw(fmt.Sprintf("SELECT %s FROM standard.%s WHERE id = 1", childColumn, childTable)).Scan(&value).Error; err != nil {
		t.Fatalf("load child value: %v", err)
	}
	if version != 2 || value != expectedValue {
		t.Fatalf("parent version = %d, child value = %q, want version 2 and value %q", version, value, expectedValue)
	}
}

func TestDocumentMappingsRejectStaleVersionWithoutReplacingMappings(t *testing.T) {
	db := openVersionedTestDB(t,
		`CREATE TABLE standard.documents (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE standard.document_element_mappings (id INTEGER PRIMARY KEY AUTOINCREMENT, document_id INTEGER NOT NULL, element_id INTEGER NOT NULL, reference_location TEXT, created_at DATETIME)`,
		`CREATE TABLE standard.document_glossary_mappings (id INTEGER PRIMARY KEY AUTOINCREMENT, document_id INTEGER NOT NULL, glossary_id INTEGER NOT NULL, reference_location TEXT, created_at DATETIME)`,
		`CREATE TABLE standard.document_metric_mappings (id INTEGER PRIMARY KEY AUTOINCREMENT, document_id INTEGER NOT NULL, metric_id INTEGER NOT NULL, reference_location TEXT, created_at DATETIME)`,
		`INSERT INTO standard.documents (id, tenant_id, version) VALUES (1, 7, 1)`,
		`INSERT INTO standard.document_element_mappings (document_id, element_id) VALUES (1, 10)`,
	)
	repo := NewDocumentRepository(db)
	if err := repo.SetMappings(1, 7, 1, []int64{20}, nil, nil, nil); err != nil {
		t.Fatalf("first set mappings: %v", err)
	}
	if err := repo.SetMappings(1, 7, 1, []int64{30}, nil, nil, nil); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale set mappings error = %v, want ErrVersionConflict", err)
	}
	var ids []int64
	if err := db.Raw(`SELECT element_id FROM standard.document_element_mappings WHERE document_id = 1 ORDER BY element_id`).Scan(&ids).Error; err != nil {
		t.Fatalf("load document mappings: %v", err)
	}
	if len(ids) != 1 || ids[0] != 20 {
		t.Fatalf("document element mappings = %v, want [20]", ids)
	}
	var version int64
	if err := db.Raw(`SELECT version FROM standard.documents WHERE id = 1`).Scan(&version).Error; err != nil {
		t.Fatalf("load document version: %v", err)
	}
	if version != 2 {
		t.Fatalf("document version = %d, want 2", version)
	}
}

func TestDocumentFileReplacementRejectsStaleVersionWithoutCleanupSideEffect(t *testing.T) {
	db := openVersionedTestDB(t,
		`CREATE TABLE standard.documents (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, file_key TEXT, file_name TEXT, file_size INTEGER, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE standard.document_file_cleanups (id INTEGER PRIMARY KEY AUTOINCREMENT, object_key TEXT NOT NULL UNIQUE, attempts INTEGER NOT NULL DEFAULT 0, next_attempt_at DATETIME NOT NULL, last_error TEXT, created_at DATETIME, updated_at DATETIME)`,
		`INSERT INTO standard.documents (id, tenant_id, file_key, file_name, file_size, version) VALUES (1, 7, 'old-key', 'old.pdf', 10, 1)`,
	)
	repo := NewDocumentRepository(db)
	cleanup, err := repo.ReplaceFile(1, 7, 99, "new-key", "new.pdf", 20)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale replace file error = %v, want ErrVersionConflict", err)
	}
	if cleanup != nil {
		t.Fatalf("stale replace cleanup = %#v, want nil", cleanup)
	}
	var actual struct {
		FileKey  string
		FileName string
		FileSize int64
		Version  int64
	}
	if err := db.Raw(`SELECT file_key, file_name, file_size, version FROM standard.documents WHERE id = 1`).Scan(&actual).Error; err != nil {
		t.Fatalf("load document: %v", err)
	}
	if actual.FileKey != "old-key" || actual.FileName != "old.pdf" || actual.FileSize != 10 || actual.Version != 1 {
		t.Fatalf("document after stale replacement = %#v", actual)
	}
	var cleanupCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM standard.document_file_cleanups`).Scan(&cleanupCount).Error; err != nil {
		t.Fatalf("count cleanup records: %v", err)
	}
	if cleanupCount != 0 {
		t.Fatalf("cleanup records = %d, want 0", cleanupCount)
	}
}
