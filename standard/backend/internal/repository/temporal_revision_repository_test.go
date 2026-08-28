package repository

import (
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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
		`CREATE TABLE standard.elements (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, draft_revision_id INTEGER, version INTEGER NOT NULL, lifecycle_state TEXT NOT NULL, updated_by INTEGER, updated_at DATETIME)`,
		`CREATE TABLE standard.element_revisions (id INTEGER PRIMARY KEY, element_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, data_type TEXT NOT NULL, value_domain_kind TEXT NOT NULL, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, compiled_quality_rules TEXT, published_by INTEGER, published_at DATETIME, updated_by INTEGER, updated_at DATETIME)`,
		`CREATE TABLE standard.code_sets (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, code TEXT NOT NULL, origin TEXT NOT NULL, draft_revision_id INTEGER, version INTEGER NOT NULL, lifecycle_state TEXT NOT NULL, updated_by INTEGER, updated_at DATETIME)`,
		`CREATE TABLE standard.code_set_revisions (id INTEGER PRIMARY KEY, code_set_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL, value_type TEXT NOT NULL, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, published_by INTEGER, published_at DATETIME, updated_by INTEGER, updated_at DATETIME)`,
		`CREATE TABLE standard.code_set_revision_items (id INTEGER PRIMARY KEY, code_set_revision_id INTEGER NOT NULL, code TEXT NOT NULL, label TEXT NOT NULL, sort_order INTEGER, status TEXT)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
