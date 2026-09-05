package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/common/events"
	"github.com/addp/standard/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupStandardCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatalf("attach standard schema: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE standard.reference_deletions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			resource_type TEXT NOT NULL,
			resource_id INTEGER NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at DATETIME NOT NULL,
			last_error TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE standard.domains (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			code TEXT NOT NULL,
			description TEXT,
			parent_id INTEGER,
			icon TEXT,
			sort_order INTEGER,
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			version INTEGER NOT NULL DEFAULT 1,
			lifecycle_state TEXT NOT NULL DEFAULT 'active'
		)`,
		`CREATE TABLE standard.glossaries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			scope_type TEXT NOT NULL,
			owner_domain_id INTEGER,
			code TEXT NOT NULL,
			steward_id INTEGER,
			tags TEXT,
			draft_revision_id INTEGER,
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			version INTEGER NOT NULL DEFAULT 1,
			lifecycle_state TEXT NOT NULL DEFAULT 'active'
		)`,
		`CREATE TABLE standard.glossary_revisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, glossary_id INTEGER NOT NULL, revision_no INTEGER NOT NULL,
			status TEXT NOT NULL, name TEXT NOT NULL, alias TEXT, definition TEXT NOT NULL, example TEXT, note TEXT,
			related_ids TEXT, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME,
			submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME,
			created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE standard.glossary_element_mappings (
			glossary_id INTEGER NOT NULL,
			element_id INTEGER NOT NULL,
			PRIMARY KEY (glossary_id, element_id)
		)`,
		`CREATE TABLE standard.elements (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			scope_type TEXT NOT NULL,
			owner_domain_id INTEGER,
			code TEXT NOT NULL,
			steward_id INTEGER,
			tags TEXT,
			draft_revision_id INTEGER,
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			version INTEGER NOT NULL DEFAULT 1,
			lifecycle_state TEXT NOT NULL DEFAULT 'active'
		)`,
		`CREATE TABLE standard.element_revisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, element_id INTEGER NOT NULL, revision_no INTEGER NOT NULL,
			status TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, data_type TEXT NOT NULL,
			length INTEGER, precision_num INTEGER, scale INTEGER, nullable BOOLEAN, default_value TEXT, format TEXT,
			value_domain_kind TEXT NOT NULL, range_constraint TEXT, code_set_revision_id INTEGER, unit_id INTEGER,
			example_values TEXT, extra_quality_rules TEXT,
			compiled_quality_rules TEXT, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME,
			submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME,
			created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE standard.code_sets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			scope_type TEXT NOT NULL,
			owner_domain_id INTEGER,
			code TEXT NOT NULL,
			origin TEXT NOT NULL,
			steward_id INTEGER,
			tags TEXT,
			draft_revision_id INTEGER,
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			version INTEGER NOT NULL DEFAULT 1,
			lifecycle_state TEXT NOT NULL DEFAULT 'active'
		)`,
		`CREATE TABLE standard.code_set_revisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, code_set_id INTEGER NOT NULL, revision_no INTEGER NOT NULL,
			status TEXT NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL, value_type TEXT NOT NULL,
			change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER,
			submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL,
			updated_by INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE standard.code_set_revision_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			code_set_revision_id INTEGER NOT NULL,
			code TEXT NOT NULL,
			label TEXT NOT NULL,
			definition TEXT,
			sort_order INTEGER,
			status TEXT,
			replacement_item_id INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE standard.measurement_categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			code TEXT NOT NULL,
			description TEXT,
			sort_order INTEGER,
			is_system BOOLEAN,
			created_at DATETIME,
			updated_at DATETIME,
			version INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE standard.units (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			category_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			symbol TEXT,
			description TEXT,
			sort_order INTEGER,
			is_system BOOLEAN,
			created_at DATETIME,
			updated_at DATETIME,
			version INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE standard.metric_categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			code TEXT NOT NULL,
			description TEXT,
			parent_id INTEGER,
			sort_order INTEGER,
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			version INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE TABLE standard.metric_definitions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			category_id INTEGER,
			scope_type TEXT NOT NULL,
			owner_domain_id INTEGER,
			code TEXT NOT NULL,
			steward_id INTEGER,
			tags TEXT,
			draft_revision_id INTEGER,
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME,
			version INTEGER NOT NULL DEFAULT 1,
			lifecycle_state TEXT NOT NULL DEFAULT 'active'
		)`,
		`CREATE TABLE standard.metric_definition_revisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			metric_definition_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL,
			metric_type TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL,
			statistical_caliber TEXT NOT NULL, semantic_formula TEXT, unit_id INTEGER,
			change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME,
			submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME,
			created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE standard.metric_definition_revision_dependencies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			metric_definition_revision_id INTEGER NOT NULL,
			dependency_definition_id INTEGER NOT NULL,
			dependency_revision_id INTEGER,
			relation_kind TEXT NOT NULL,
			coefficient REAL,
			note TEXT,
			created_at DATETIME
		)`,
		`CREATE TABLE standard.documents (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			scope_type TEXT NOT NULL,
			owner_domain_id INTEGER,
			code TEXT NOT NULL,
			doc_type TEXT NOT NULL,
			source_org TEXT,
			steward_id INTEGER,
			tags TEXT,
			draft_revision_id INTEGER,
			version INTEGER NOT NULL DEFAULT 1,
			lifecycle_state TEXT NOT NULL DEFAULT 'active',
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE standard.document_revisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT, document_id INTEGER NOT NULL, revision_no INTEGER NOT NULL,
			status TEXT NOT NULL, name TEXT NOT NULL, version_label TEXT, publish_date DATETIME, description TEXT,
			file_key TEXT, file_name TEXT, file_size INTEGER, media_type TEXT, content_sha256 TEXT,
			change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME,
			submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME,
			created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE standard.document_element_mappings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			document_id INTEGER NOT NULL,
			element_id INTEGER NOT NULL,
			reference_location TEXT,
			created_at DATETIME
		)`,
		`CREATE TABLE standard.document_glossary_mappings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			document_id INTEGER NOT NULL,
			glossary_id INTEGER NOT NULL,
			reference_location TEXT,
			created_at DATETIME
		)`,
		`CREATE TABLE standard.document_metric_mappings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			document_id INTEGER NOT NULL,
			metric_id INTEGER NOT NULL,
			reference_location TEXT,
			created_at DATETIME
		)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create standard cleanup table: %v", err)
		}
	}
	return db
}

func TestStandardCleanupScanWithoutTenantLifecycleContextReturnsNoCandidates(t *testing.T) {
	db := setupStandardCleanupTestDB(t)
	seedStandardCleanupTenantState(t, db, 1, false)

	svc := NewCleanupService(db, nil, nil, nil)
	stats, err := svc.ScanReclaimCandidates(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates: %v", err)
	}
	if standardCandidateRecordCount(stats) != 0 {
		t.Fatalf("expected no candidates without tenant lifecycle context, got %+v", stats)
	}
}

func TestStandardCleanupEngineDeletedReturnsNoCandidates(t *testing.T) {
	db := setupStandardCleanupTestDB(t)
	seedStandardCleanupTenantState(t, db, 1, false)

	svc := NewCleanupService(db, nil, nil, nil)
	stats, err := svc.ScanReclaimCandidates(context.Background(), 1, map[string]interface{}{"engine_id": 7})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates: %v", err)
	}
	if standardCandidateRecordCount(stats) != 0 {
		t.Fatalf("expected no candidates for engine lifecycle, got %+v", stats)
	}
}

func TestStandardCleanupTenantDeletedLogicalDeprecatesStatefulDefinitions(t *testing.T) {
	db := setupStandardCleanupTestDB(t)
	ids := seedStandardCleanupTenantState(t, db, 1, false)

	svc := NewCleanupService(db, nil, nil, nil)
	stats, err := svc.ExecuteCleanup(context.Background(), 1, events.CleanupModeLogical, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup: %v", err)
	}
	if stats.DeprecatedGlossaries != 1 || stats.DeprecatedElements != 1 || stats.DeprecatedMetrics != 1 || stats.DeprecatedDocuments != 1 || stats.SkippedItems != 2 {
		t.Fatalf("unexpected logical cleanup stats: %+v", stats)
	}

	var glossaryRevision models.GlossaryRevision
	if err := db.Where("glossary_id = ?", ids.glossaryID).First(&glossaryRevision).Error; err != nil {
		t.Fatalf("load glossary revision: %v", err)
	}
	if glossaryRevision.Status != models.RevisionStatusWithdrawn {
		t.Fatalf("expected glossary revision withdrawn, got %s", glossaryRevision.Status)
	}
	var element models.Element
	if err := db.First(&element, ids.elementID).Error; err != nil {
		t.Fatalf("load element: %v", err)
	}
	var elementRevision models.ElementRevision
	if err := db.Where("element_id = ?", ids.elementID).First(&elementRevision).Error; err != nil {
		t.Fatalf("load element revision: %v", err)
	}
	if elementRevision.Status != models.RevisionStatusWithdrawn {
		t.Fatalf("expected element revision withdrawn, got %s", elementRevision.Status)
	}
	var metricRevision models.MetricDefinitionRevision
	if err := db.Where("metric_definition_id = ?", ids.metricID).First(&metricRevision).Error; err != nil {
		t.Fatalf("load metric revision: %v", err)
	}
	if metricRevision.Status != models.RevisionStatusWithdrawn {
		t.Fatalf("expected metric revision withdrawn, got %s", metricRevision.Status)
	}

	var documentRevision models.DocumentRevision
	if err := db.Where("document_id = ?", ids.documentID).First(&documentRevision).Error; err != nil {
		t.Fatalf("load document revision: %v", err)
	}
	if documentRevision.Status != models.RevisionStatusWithdrawn {
		t.Fatalf("expected document revision withdrawn, got %s", documentRevision.Status)
	}
	if documentRevision.FileKey != "" {
		t.Fatalf("logical cleanup must not modify document revision file_key, got %s", documentRevision.FileKey)
	}
}

func TestStandardCleanupTenantDeletedPhysicalDeletesOwnedState(t *testing.T) {
	db := setupStandardCleanupTestDB(t)
	seedStandardCleanupTenantState(t, db, 1, false)
	seedStandardCleanupTenantState(t, db, 2, false)

	svc := NewCleanupService(db, nil, nil, nil)
	stats, err := svc.ScanReclaimCandidates(context.Background(), 1, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates: %v", err)
	}
	if standardCandidateRecordCount(stats) != 20 {
		t.Fatalf("expected 20 scanned records, got %+v", stats)
	}

	stats, err = svc.ExecuteCleanup(context.Background(), 1, events.CleanupModePhysical, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup: %v", err)
	}
	if stats.DeletedRecords != 20 {
		t.Fatalf("expected 20 deleted records, got %+v", stats)
	}
	assertStandardCleanupCounts(t, db, standardCleanupCountExpectation{
		tenantID:                 2,
		domains:                  1,
		glossaries:               2,
		glossaryElementMappings:  1,
		elements:                 1,
		codeSets:                 1,
		codeItems:                1,
		measurementCategories:    1,
		units:                    1,
		metricCategories:         1,
		metrics:                  2,
		metricRevisions:          2,
		metricDependencies:       1,
		documents:                1,
		documentElementMappings:  1,
		documentGlossaryMappings: 1,
		documentMetricMappings:   1,
		referenceDeletions:       1,
	})
}

func TestStandardCleanupPhysicalPreservesDocumentWhenMinIOUnavailable(t *testing.T) {
	db := setupStandardCleanupTestDB(t)
	ids := seedStandardCleanupTenantState(t, db, 1, true)

	svc := NewCleanupService(db, nil, nil, nil)
	stats, err := svc.ExecuteCleanup(context.Background(), 1, events.CleanupModePhysical, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup: %v", err)
	}
	if len(stats.Errors) == 0 || stats.SkippedItems != 1 {
		t.Fatalf("expected file cleanup error and skipped document, got %+v", stats)
	}
	var doc models.Document
	if err := db.First(&doc, ids.documentID).Error; err != nil {
		t.Fatalf("expected document to be preserved after file cleanup failure: %v", err)
	}
	var revision models.DocumentRevision
	if err := db.Where("document_id = ?", doc.ID).First(&revision).Error; err != nil || revision.FileKey == "" {
		t.Fatalf("expected preserved document revision to keep file_key: revision=%+v err=%v", revision, err)
	}
}

type standardCleanupSeedIDs struct {
	glossaryID int64
	elementID  int64
	metricID   int64
	documentID int64
}

func seedStandardCleanupTenantState(t *testing.T, db *gorm.DB, tenantID int64, withFile bool) standardCleanupSeedIDs {
	t.Helper()
	suffix := string(rune('a' + tenantID))

	domain := models.Domain{TenantID: tenantID, Name: "Domain " + suffix, Code: "domain_" + suffix, CreatedBy: 1}
	if err := db.Create(&domain).Error; err != nil {
		t.Fatalf("create domain: %v", err)
	}
	if err := db.Create(&models.StandardReferenceDeletion{
		TenantID: tenantID, ResourceType: "domain", ResourceID: domain.ID, NextAttemptAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create reference deletion: %v", err)
	}
	glossary := models.Glossary{TenantID: tenantID, ScopeType: models.StandardScopeDomain, OwnerDomainID: &domain.ID, Code: "glossary_" + suffix, CreatedBy: 1, LifecycleState: "active"}
	if err := db.Create(&glossary).Error; err != nil {
		t.Fatalf("create glossary: %v", err)
	}
	glossaryRevision := models.GlossaryRevision{GlossaryID: glossary.ID, RevisionNo: 1, Status: models.RevisionStatusPublished, Name: "Glossary " + suffix, Definition: "definition", ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&glossaryRevision).Error; err != nil {
		t.Fatalf("create glossary revision: %v", err)
	}
	deprecatedGlossary := models.Glossary{TenantID: tenantID, ScopeType: models.StandardScopeDomain, OwnerDomainID: &domain.ID, Code: "old_glossary_" + suffix, CreatedBy: 1, LifecycleState: "active"}
	if err := db.Create(&deprecatedGlossary).Error; err != nil {
		t.Fatalf("create deprecated glossary: %v", err)
	}
	deprecatedGlossaryRevision := models.GlossaryRevision{GlossaryID: deprecatedGlossary.ID, RevisionNo: 1, Status: models.RevisionStatusWithdrawn, Name: "Old Glossary " + suffix, Definition: "definition", ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&deprecatedGlossaryRevision).Error; err != nil {
		t.Fatalf("create deprecated glossary revision: %v", err)
	}
	codeSet := models.CodeSet{TenantID: tenantID, ScopeType: models.StandardScopeDomain, OwnerDomainID: &domain.ID, Code: "codeset_" + suffix, Origin: models.CodeSetOriginTenant, CreatedBy: 1, LifecycleState: "active"}
	if err := db.Create(&codeSet).Error; err != nil {
		t.Fatalf("create code set: %v", err)
	}
	codeSetRevision := models.CodeSetRevision{CodeSetID: codeSet.ID, RevisionNo: 1, Status: models.RevisionStatusPublished, Name: "Code Set " + suffix, Description: "definition", ValueType: "string", ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&codeSetRevision).Error; err != nil {
		t.Fatalf("create code set revision: %v", err)
	}
	codeItem := models.CodeSetRevisionItem{CodeSetRevisionID: codeSetRevision.ID, Code: "item_" + suffix, Label: "Item " + suffix, Status: models.CodeItemStatusActive}
	if err := db.Create(&codeItem).Error; err != nil {
		t.Fatalf("create code item: %v", err)
	}
	measurementCategory := models.MeasurementCategory{TenantID: tenantID, Name: "Length " + suffix, Code: "length_" + suffix}
	if err := db.Create(&measurementCategory).Error; err != nil {
		t.Fatalf("create measurement category: %v", err)
	}
	unit := models.Unit{TenantID: tenantID, CategoryID: measurementCategory.ID, Name: "Meter " + suffix, Symbol: "m"}
	if err := db.Create(&unit).Error; err != nil {
		t.Fatalf("create unit: %v", err)
	}
	element := models.Element{
		TenantID: tenantID, ScopeType: models.StandardScopeDomain, OwnerDomainID: &domain.ID, Code: "amount_" + suffix,
		CreatedBy: 1, LifecycleState: "active",
	}
	if err := db.Create(&element).Error; err != nil {
		t.Fatalf("create element: %v", err)
	}
	elementRevision := models.ElementRevision{ElementID: element.ID, RevisionNo: 1, Status: models.RevisionStatusPublished, Name: "Amount " + suffix, Definition: "amount definition", DataType: "decimal", ValueDomainKind: models.ValueDomainUnrestricted, UnitID: &unit.ID, ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&elementRevision).Error; err != nil {
		t.Fatalf("create element revision: %v", err)
	}
	if err := db.Create(&models.GlossaryElementMapping{GlossaryID: glossary.ID, ElementID: element.ID}).Error; err != nil {
		t.Fatalf("create glossary element mapping: %v", err)
	}
	metricCategory := models.MetricCategory{TenantID: tenantID, Name: "Metric Category " + suffix, Code: "metric_cat_" + suffix, CreatedBy: 1}
	if err := db.Create(&metricCategory).Error; err != nil {
		t.Fatalf("create metric category: %v", err)
	}
	metric := models.MetricDefinition{TenantID: tenantID, CategoryID: &metricCategory.ID, ScopeType: models.StandardScopeDomain, OwnerDomainID: &domain.ID, Code: "revenue_" + suffix, Tags: models.StringArray{}, CreatedBy: 1, LifecycleState: "active"}
	if err := db.Create(&metric).Error; err != nil {
		t.Fatalf("create metric: %v", err)
	}
	metricRevision := models.MetricDefinitionRevision{MetricDefinitionID: metric.ID, RevisionNo: 1, Status: models.RevisionStatusPublished, MetricType: models.MetricTypeAtomic, Name: "Revenue " + suffix, Definition: "Revenue", StatisticalCaliber: "All revenue", ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&metricRevision).Error; err != nil {
		t.Fatalf("create metric revision: %v", err)
	}
	deprecatedMetric := models.MetricDefinition{TenantID: tenantID, CategoryID: &metricCategory.ID, ScopeType: models.StandardScopeDomain, OwnerDomainID: &domain.ID, Code: "old_revenue_" + suffix, Tags: models.StringArray{}, CreatedBy: 1, LifecycleState: "active"}
	if err := db.Create(&deprecatedMetric).Error; err != nil {
		t.Fatalf("create deprecated metric: %v", err)
	}
	deprecatedMetricRevision := models.MetricDefinitionRevision{MetricDefinitionID: deprecatedMetric.ID, RevisionNo: 1, Status: models.RevisionStatusWithdrawn, MetricType: models.MetricTypeAtomic, Name: "Old Revenue " + suffix, Definition: "Old revenue", StatisticalCaliber: "Legacy", ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&deprecatedMetricRevision).Error; err != nil {
		t.Fatalf("create deprecated metric revision: %v", err)
	}
	if err := db.Create(&models.MetricDefinitionRevisionDependency{MetricDefinitionRevisionID: metricRevision.ID, DependencyDefinitionID: deprecatedMetric.ID, DependencyRevisionID: &deprecatedMetricRevision.ID, RelationKind: models.MetricDependencyComponent}).Error; err != nil {
		t.Fatalf("create metric dependency: %v", err)
	}
	fileKey := ""
	fileSize := int64(0)
	if withFile {
		fileKey = "tenant_1/documents/1/spec.pdf"
		fileSize = 2048
	}
	document := models.Document{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: "spec_" + suffix, DocType: "reference", LifecycleState: "active", CreatedBy: 1}
	if err := db.Create(&document).Error; err != nil {
		t.Fatalf("create document: %v", err)
	}
	documentRevision := models.DocumentRevision{DocumentID: document.ID, RevisionNo: 1, Status: models.RevisionStatusPublished, Name: "Spec " + suffix, FileKey: fileKey, FileName: "spec.md", FileSize: fileSize, MediaType: "text/markdown", ContentSHA256: "fixture", ChangeSummary: "initial", CreatedBy: 1}
	if err := db.Create(&documentRevision).Error; err != nil {
		t.Fatalf("create document revision: %v", err)
	}
	if err := db.Create(&models.DocumentElementMapping{DocumentID: document.ID, ElementID: element.ID}).Error; err != nil {
		t.Fatalf("create document element mapping: %v", err)
	}
	if err := db.Create(&models.DocumentGlossaryMapping{DocumentID: document.ID, GlossaryID: glossary.ID}).Error; err != nil {
		t.Fatalf("create document glossary mapping: %v", err)
	}
	if err := db.Create(&models.DocumentMetricMapping{DocumentID: document.ID, MetricID: metric.ID}).Error; err != nil {
		t.Fatalf("create document metric mapping: %v", err)
	}
	return standardCleanupSeedIDs{
		glossaryID: glossary.ID,
		elementID:  element.ID,
		metricID:   metric.ID,
		documentID: document.ID,
	}
}

type standardCleanupCountExpectation struct {
	tenantID                 int64
	domains                  int64
	glossaries               int64
	glossaryElementMappings  int64
	elements                 int64
	codeSets                 int64
	codeItems                int64
	measurementCategories    int64
	units                    int64
	metricCategories         int64
	metrics                  int64
	metricRevisions          int64
	metricDependencies       int64
	documents                int64
	documentElementMappings  int64
	documentGlossaryMappings int64
	documentMetricMappings   int64
	referenceDeletions       int64
}

func assertStandardCleanupCounts(t *testing.T, db *gorm.DB, expected standardCleanupCountExpectation) {
	t.Helper()
	assertStandardCleanupCount(t, db, &models.Domain{}, "tenant_id = ?", []interface{}{expected.tenantID}, expected.domains, "domains")
	assertStandardCleanupCount(t, db, &models.Glossary{}, "tenant_id = ?", []interface{}{expected.tenantID}, expected.glossaries, "glossaries")
	assertStandardCleanupCount(t, db, &models.Element{}, "tenant_id = ?", []interface{}{expected.tenantID}, expected.elements, "elements")
	assertStandardCleanupCount(t, db, &models.CodeSet{}, "tenant_id = ?", []interface{}{expected.tenantID}, expected.codeSets, "code sets")
	assertStandardCleanupCount(t, db, &models.MeasurementCategory{}, "tenant_id = ?", []interface{}{expected.tenantID}, expected.measurementCategories, "measurement categories")
	assertStandardCleanupCount(t, db, &models.Unit{}, "tenant_id = ?", []interface{}{expected.tenantID}, expected.units, "units")
	assertStandardCleanupCount(t, db, &models.MetricCategory{}, "tenant_id = ?", []interface{}{expected.tenantID}, expected.metricCategories, "metric categories")
	assertStandardCleanupCount(t, db, &models.MetricDefinition{}, "tenant_id = ?", []interface{}{expected.tenantID}, expected.metrics, "metrics")
	assertStandardCleanupCount(t, db, &models.Document{}, "tenant_id = ?", []interface{}{expected.tenantID}, expected.documents, "documents")
	assertStandardCleanupCount(t, db, &models.StandardReferenceDeletion{}, "tenant_id = ?", []interface{}{expected.tenantID}, expected.referenceDeletions, "reference deletions")
	assertStandardCleanupCount(t, db, &models.CodeSetRevisionItem{}, "", nil, expected.codeItems, "code items")
	assertStandardCleanupCount(t, db, &models.GlossaryElementMapping{}, "", nil, expected.glossaryElementMappings, "glossary element mappings")
	assertStandardCleanupCount(t, db, &models.MetricDefinitionRevision{}, "", nil, expected.metricRevisions, "metric revisions")
	assertStandardCleanupCount(t, db, &models.MetricDefinitionRevisionDependency{}, "", nil, expected.metricDependencies, "metric dependencies")
	assertStandardCleanupCount(t, db, &models.DocumentElementMapping{}, "", nil, expected.documentElementMappings, "document element mappings")
	assertStandardCleanupCount(t, db, &models.DocumentGlossaryMapping{}, "", nil, expected.documentGlossaryMappings, "document glossary mappings")
	assertStandardCleanupCount(t, db, &models.DocumentMetricMapping{}, "", nil, expected.documentMetricMappings, "document metric mappings")
}

func assertStandardCleanupCount(t *testing.T, db *gorm.DB, model interface{}, where string, args []interface{}, expected int64, name string) {
	t.Helper()
	var count int64
	query := db.Model(model)
	if where != "" {
		query = query.Where(where, args...)
	}
	if err := query.Count(&count).Error; err != nil {
		t.Fatalf("count %s: %v", name, err)
	}
	if count != expected {
		t.Fatalf("expected %s count %d, got %d", name, expected, count)
	}
}
