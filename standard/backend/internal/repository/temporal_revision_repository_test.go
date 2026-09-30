package repository

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestStandardAggregateRetainsLatestHistoricalContent(t *testing.T) {
	for _, kind := range []struct {
		name, identity, revisions, ownerColumn, extraColumns, extraValues string
		read                                                              func(*gorm.DB, time.Time) (any, error)
	}{
		{"element", "elements", "element_revisions", "element_id", ", definition, data_type, value_domain_kind", ", '定义', 'string', 'unrestricted'", func(db *gorm.DB, at time.Time) (any, error) { return NewElementRepository(db).GetAggregateAt(1, 7, at) }},
		{"code_set", "code_sets", "code_set_revisions", "code_set_id", ", description, value_type", ", '定义', 'string'", func(db *gorm.DB, at time.Time) (any, error) { return NewCodeSetRepository(db).GetAggregateAt(1, 7, at) }},
		{"metric", "metric_definitions", "metric_definition_revisions", "metric_definition_id", ", definition, statistical_caliber, metric_type", ", '定义', '口径', 'derived'", func(db *gorm.DB, at time.Time) (any, error) { return NewMetricRepository(db).GetAggregateAt(1, 7, at) }},
		{"document", "documents", "document_revisions", "document_id", ", description, file_name", ", '定义', 'history.md'", func(db *gorm.DB, at time.Time) (any, error) {
			return NewDocumentRepository(db).GetAggregateAt(1, 7, at)
		}},
	} {
		for _, state := range []string{"withdrawn", "future", "expired"} {
			t.Run(kind.name+"/"+state, func(t *testing.T) {
				db := openTemporalRevisionTestDB(t)
				identityColumns, identityValues := "", ""
				if kind.name == "code_set" {
					identityColumns, identityValues = ", origin", ", 'tenant'"
				}
				if err := db.Exec(fmt.Sprintf("INSERT INTO standard.%s (id, tenant_id, code, version, lifecycle_state%s) VALUES (1, 7, 'history', 1, 'active'%s)", kind.identity, identityColumns, identityValues)).Error; err != nil {
					t.Fatal(err)
				}
				from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
				end := from.AddDate(1, 0, 0)
				status := models.RevisionStatusPublished
				var to *time.Time = &end
				if state == "withdrawn" {
					status, to = models.RevisionStatusWithdrawn, nil
				}
				if state == "future" {
					from, to = from.AddDate(10, 0, 0), nil
				}
				query := fmt.Sprintf("INSERT INTO standard.%s (id, %s, revision_no, status, name, change_summary, effective_from, effective_to%s) VALUES (?, 1, ?, ?, ?, 'change', ?, ?%s)", kind.revisions, kind.ownerColumn, kind.extraColumns, kind.extraValues)
				if err := db.Exec(query, 9, 1, models.RevisionStatusPublished, "旧内容", end.AddDate(-2, 0, 0), end.AddDate(-1, 0, 0)).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Exec(query, 2, 2, status, "最新历史内容", from, to).Error; err != nil {
					t.Fatal(err)
				}
				if kind.name == "code_set" {
					if err := db.Exec("INSERT INTO standard.code_set_revision_items (id, code_set_revision_id, code, label, sort_order, status) VALUES (1, 2, 'x', '保留码值', 1, 'active')").Error; err != nil {
						t.Fatal(err)
					}
				}
				if kind.name == "metric" {
					if err := db.Exec("INSERT INTO standard.metric_definition_revision_dependencies (id, metric_definition_revision_id, dependency_definition_id, dependency_revision_id, relation_kind) VALUES (1, 2, 10, 100, 'base')").Error; err != nil {
						t.Fatal(err)
					}
				}
				aggregate, err := kind.read(db, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
				if err != nil {
					t.Fatal(err)
				}
				payload, err := json.Marshal(aggregate)
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Latest *struct {
						ID                                    int64
						RevisionNo                            int64 `json:"revision_no"`
						Name, Status, Definition, Description string
						FileName                              string `json:"file_name"`
						Items, Dependencies                   []json.RawMessage
					} `json:"latest_revision"`
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(payload, &fields); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(payload, &result); err != nil {
					t.Fatal(err)
				}
				if len(fields["current_revision"]) != 0 || len(fields["draft_revision"]) != 0 || result.Latest == nil || result.Latest.ID != 2 || result.Latest.RevisionNo != 2 || result.Latest.Name != "最新历史内容" || result.Latest.Status != status {
					t.Fatalf("historical content missing or treated as effective: %s", payload)
				}
				if result.Latest.Definition != "定义" && result.Latest.Description != "定义" {
					t.Fatalf("historical definition lost: %s", payload)
				}
				if kind.name == "document" && result.Latest.FileName != "history.md" {
					t.Fatalf("historical file metadata lost: %s", payload)
				}
				if kind.name == "code_set" && len(result.Latest.Items) != 1 {
					t.Fatalf("historical items lost: %s", payload)
				}
				if kind.name == "metric" && len(result.Latest.Dependencies) != 1 {
					t.Fatalf("frozen dependencies lost: %s", payload)
				}
			})
		}
	}
}

func TestElementPublishKeepsPublishedHistoryAndResolvesByAsOf(t *testing.T) {
	db := openTemporalRevisionTestDB(t)
	january := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	february := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Exec(`INSERT INTO standard.elements (id, tenant_id, code, draft_revision_id, version, lifecycle_state) VALUES (1, 7, 'customer_id', 102, 1, 'active')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO standard.element_revisions
		(id, element_id, revision_no, status, name, definition, data_type, value_domain_kind, change_summary, effective_from)
		VALUES (101, 1, 1, 'published', 'Customer ID v1', 'v1', 'bigint', 'unrestricted', 'v1', ?),
		       (102, 1, 2, 'in_review', 'Customer ID v2', 'v2', 'bigint', 'unrestricted', 'v2', ?)`, january, february).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewElementRepository(db)
	if err := repo.PublishRevision(1, 102, 7, 9, 1, models.JSONB{}); err != nil {
		t.Fatalf("PublishRevision() error = %v", err)
	}

	var first, second models.ElementRevision
	if err := db.First(&first, 101).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&second, 102).Error; err != nil {
		t.Fatal(err)
	}
	if first.Status != models.RevisionStatusPublished || first.EffectiveTo == nil || !first.EffectiveTo.Equal(february) || second.Status != models.RevisionStatusPublished {
		t.Fatalf("published revisions = first %#v second %#v", first, second)
	}
	before, err := repo.GetAggregateAt(1, 7, january.Add(time.Hour))
	if err != nil || before.CurrentRevision == nil || before.CurrentRevision.ID != 101 {
		t.Fatalf("January aggregate = %#v, err=%v", before, err)
	}
	after, err := repo.GetAggregateAt(1, 7, february)
	if err != nil || after.CurrentRevision == nil || after.CurrentRevision.ID != 102 || after.DraftRevision != nil {
		t.Fatalf("February aggregate = %#v, err=%v", after, err)
	}
}

func TestGlossaryPublishKeepsPublishedHistoryAndResolvesByAsOf(t *testing.T) {
	db := openTemporalRevisionTestDB(t)
	january := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	february := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Exec(`INSERT INTO standard.glossaries (id, tenant_id, code, draft_revision_id, version, lifecycle_state) VALUES (3, 7, 'customer', 302, 1, 'active')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO standard.glossary_revisions
		(id, glossary_id, revision_no, status, name, definition, change_summary, effective_from)
		VALUES (301, 3, 1, 'published', 'Customer v1', 'v1', 'v1', ?),
		       (302, 3, 2, 'in_review', 'Customer v2', 'v2', 'v2', ?)`, january, february).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewGlossaryRepository(db)
	if err := repo.PublishRevision(3, 302, 7, 9, 1); err != nil {
		t.Fatalf("PublishRevision() error = %v", err)
	}
	var first models.GlossaryRevision
	if err := db.First(&first, 301).Error; err != nil {
		t.Fatal(err)
	}
	if first.Status != models.RevisionStatusPublished || first.EffectiveTo == nil || !first.EffectiveTo.Equal(february) {
		t.Fatalf("first revision = %#v", first)
	}
	before, err := repo.GetAggregateAt(3, 7, january.Add(time.Hour))
	if err != nil || before.CurrentRevision == nil || before.CurrentRevision.ID != 301 {
		t.Fatalf("January glossary = %#v, err=%v", before, err)
	}
	after, err := repo.GetAggregateAt(3, 7, february)
	if err != nil || after.CurrentRevision == nil || after.CurrentRevision.ID != 302 || after.DraftRevision != nil || after.Version != 2 {
		t.Fatalf("February glossary = %#v, err=%v", after, err)
	}
}

func TestCodeSetAggregateResolvesPublishedRevisionByAsOf(t *testing.T) {
	db := openTemporalRevisionTestDB(t)
	january := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	february := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Exec(`INSERT INTO standard.code_sets (id, tenant_id, code, origin, version, lifecycle_state) VALUES (2, 7, 'gender', 'tenant', 1, 'active')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO standard.code_set_revisions
		(id, code_set_id, revision_no, status, name, description, value_type, change_summary, effective_from, effective_to)
		VALUES (201, 2, 1, 'published', 'Gender v1', 'v1', 'string', 'v1', ?, ?),
		       (202, 2, 2, 'published', 'Gender v2', 'v2', 'string', 'v2', ?, NULL)`, january, february, february).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewCodeSetRepository(db)
	before, err := repo.GetAggregateAt(2, 7, january.Add(time.Hour))
	if err != nil || before.CurrentRevision == nil || before.CurrentRevision.ID != 201 {
		t.Fatalf("January code set = %#v, err=%v", before, err)
	}
	after, err := repo.GetAggregateAt(2, 7, february)
	if err != nil || after.CurrentRevision == nil || after.CurrentRevision.ID != 202 {
		t.Fatalf("February code set = %#v, err=%v", after, err)
	}
}

func TestMetricPublishClosesHistoryAndFreezesDependencyRevision(t *testing.T) {
	db := openTemporalRevisionTestDB(t)
	january := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	february := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Exec(`INSERT INTO standard.metric_definitions
		(id, tenant_id, code, draft_revision_id, version, lifecycle_state)
		VALUES (1, 7, 'conversion_rate', 102, 1, 'active'),
		       (2, 7, 'orders', NULL, 1, 'active')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO standard.metric_definition_revisions
		(id, metric_definition_id, revision_no, status, metric_type, name, definition, statistical_caliber, change_summary, effective_from)
		VALUES (101, 1, 1, 'published', 'derived', 'Conversion rate v1', 'v1', 'all orders', 'v1', ?),
		       (102, 1, 2, 'in_review', 'derived', 'Conversion rate v2', 'v2', 'paid orders', 'v2', ?),
		       (201, 2, 1, 'published', 'atomic', 'Orders', 'orders', 'all orders', 'v1', ?)`, january, february, january).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO standard.metric_definition_revision_dependencies
		(id, metric_definition_revision_id, dependency_definition_id, dependency_revision_id, relation_kind)
		VALUES (301, 102, 2, NULL, 'base')`).Error; err != nil {
		t.Fatal(err)
	}

	repo := NewMetricRepository(db)
	if err := repo.PublishRevision(1, 102, 7, 9, 1); err != nil {
		t.Fatalf("PublishRevision() error = %v", err)
	}

	var first, second models.MetricDefinitionRevision
	if err := db.First(&first, 101).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&second, 102).Error; err != nil {
		t.Fatal(err)
	}
	if first.Status != models.RevisionStatusPublished || first.EffectiveTo == nil || !first.EffectiveTo.Equal(february) || second.Status != models.RevisionStatusPublished {
		t.Fatalf("published metric revisions = first %#v second %#v", first, second)
	}
	var dependency models.MetricDefinitionRevisionDependency
	if err := db.Where("metric_definition_revision_id = ?", 102).First(&dependency).Error; err != nil {
		t.Fatal(err)
	}
	if dependency.DependencyRevisionID == nil || *dependency.DependencyRevisionID != 201 {
		t.Fatalf("frozen dependency revision = %v, want 201", dependency.DependencyRevisionID)
	}
	before, err := repo.GetAggregateAt(1, 7, january.Add(time.Hour))
	if err != nil || before.CurrentRevision == nil || before.CurrentRevision.ID != 101 {
		t.Fatalf("January metric aggregate = %#v, err=%v", before, err)
	}
	after, err := repo.GetAggregateAt(1, 7, february)
	if err != nil || after.CurrentRevision == nil || after.CurrentRevision.ID != 102 || after.DraftRevision != nil || after.Version != 2 {
		t.Fatalf("February metric aggregate = %#v, err=%v", after, err)
	}
}

func TestEffectiveIntervalsUseHalfOpenBoundaries(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	boundary := start.Add(time.Hour)
	end := boundary.Add(time.Hour)
	if intervalsOverlap(start, &boundary, boundary, &end) {
		t.Fatal("adjacent half-open intervals overlap")
	}
	if !intervalsOverlap(start, &end, boundary, nil) {
		t.Fatal("intersecting intervals did not overlap")
	}
}

func openTemporalRevisionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE standard.documents (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, draft_revision_id INTEGER, version INTEGER NOT NULL, lifecycle_state TEXT NOT NULL)`,
		`CREATE TABLE standard.document_revisions (id INTEGER PRIMARY KEY, document_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, description TEXT, file_name TEXT, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME)`,
		`CREATE TABLE standard.glossaries (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, draft_revision_id INTEGER, version INTEGER NOT NULL, lifecycle_state TEXT NOT NULL, updated_by INTEGER, updated_at DATETIME)`,
		`CREATE TABLE standard.glossary_revisions (id INTEGER PRIMARY KEY, glossary_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, alias TEXT, definition TEXT NOT NULL, example TEXT, note TEXT, related_ids TEXT, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.elements (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, draft_revision_id INTEGER, version INTEGER NOT NULL, lifecycle_state TEXT NOT NULL, updated_by INTEGER, updated_at DATETIME)`,
		`CREATE TABLE standard.element_revisions (id INTEGER PRIMARY KEY, element_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, data_type TEXT NOT NULL, value_domain_kind TEXT NOT NULL, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, compiled_quality_rules TEXT, published_by INTEGER, published_at DATETIME, updated_by INTEGER, updated_at DATETIME)`,
		`CREATE TABLE standard.code_sets (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, origin TEXT NOT NULL, draft_revision_id INTEGER, version INTEGER NOT NULL, lifecycle_state TEXT NOT NULL, updated_by INTEGER, updated_at DATETIME)`,
		`CREATE TABLE standard.code_set_revisions (id INTEGER PRIMARY KEY, code_set_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL, value_type TEXT NOT NULL, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, published_by INTEGER, published_at DATETIME, updated_by INTEGER, updated_at DATETIME)`,
		`CREATE TABLE standard.code_set_revision_items (id INTEGER PRIMARY KEY, code_set_revision_id INTEGER NOT NULL, code TEXT NOT NULL, label TEXT NOT NULL, sort_order INTEGER, status TEXT)`,
		`CREATE TABLE standard.metric_definitions (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, draft_revision_id INTEGER, version INTEGER NOT NULL, lifecycle_state TEXT NOT NULL, updated_by INTEGER, updated_at DATETIME)`,
		`CREATE TABLE standard.metric_definition_revisions (id INTEGER PRIMARY KEY, metric_definition_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, metric_type TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, statistical_caliber TEXT NOT NULL, semantic_formula TEXT, unit_id INTEGER, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.metric_definition_revision_dependencies (id INTEGER PRIMARY KEY, metric_definition_revision_id INTEGER NOT NULL, dependency_definition_id INTEGER NOT NULL, dependency_revision_id INTEGER, relation_kind TEXT NOT NULL, coefficient REAL, note TEXT, created_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
