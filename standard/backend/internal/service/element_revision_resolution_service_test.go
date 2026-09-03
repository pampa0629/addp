package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestElementRevisionResolutionReturnsEffectiveSnapshotAndExactCodeSet(t *testing.T) {
	db := openElementResolutionTestDB(t)
	asOf := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	for _, statement := range []string{
		`INSERT INTO standard.elements (id, tenant_id, scope_type, owner_domain_id, code, lifecycle_state) VALUES (10, 7, 'domain', 3, 'gender', 'active'), (20, 7, 'tenant_common', NULL, 'future', 'active'), (30, 8, 'tenant_common', NULL, 'foreign', 'active')`,
		`INSERT INTO standard.element_revisions (id, element_id, revision_no, status, name, definition, data_type, nullable, default_value, format, value_domain_kind, code_set_revision_id, effective_from, effective_to)
		 VALUES (101, 10, 2, 'published', 'Gender', 'Canonical gender', 'string', 0, '', '', 'enumeration', 501, '2026-01-01T00:00:00Z', NULL),
		        (201, 20, 1, 'published', 'Future', 'Future definition', 'string', 1, '', '', 'unrestricted', NULL, '2027-01-01T00:00:00Z', NULL),
		        (301, 30, 1, 'published', 'Foreign', 'Foreign definition', 'string', 1, '', '', 'unrestricted', NULL, '2026-01-01T00:00:00Z', NULL)`,
		`INSERT INTO standard.code_sets (id, tenant_id, scope_type, owner_domain_id, origin, code, lifecycle_state) VALUES (50, 7, 'domain', 3, 'tenant', 'gender_codes', 'active')`,
		`INSERT INTO standard.code_set_revisions (id, code_set_id, revision_no, status, name, description, value_type, effective_from, effective_to)
		 VALUES (501, 50, 4, 'withdrawn', 'Gender codes', 'Exact historical code set', 'string', '2026-01-01T00:00:00Z', NULL)`,
		`INSERT INTO standard.code_set_revision_items (id, code_set_revision_id, code, label, definition, sort_order, status, replacement_item_id)
		 VALUES (1, 501, 'F', 'Female', '', 1, 'active', NULL), (2, 501, 'M', 'Male', '', 2, 'active', NULL)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := NewElementRevisionResolutionService(repository.NewElementRepository(db), repository.NewCodeSetRepository(db))
	results, err := svc.Resolve(context.Background(), 7, []int64{20, 10, 30, 999}, asOf)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(results) != 4 || results[0].Found || !results[1].Found || results[2].Found || results[3].Found {
		t.Fatalf("results = %#v", results)
	}
	snapshot := results[1].Snapshot
	if snapshot == nil || snapshot.ElementRevisionID != 101 || snapshot.ScopeType != models.StandardScopeDomain || snapshot.OwnerDomainID == nil || *snapshot.OwnerDomainID != 3 || snapshot.ValueDomainKind != "enumeration" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.CodeSetRevision == nil || snapshot.CodeSetRevision.RevisionID != 501 || snapshot.CodeSetRevision.ScopeType != models.StandardScopeDomain || snapshot.CodeSetRevision.OwnerDomainID == nil || *snapshot.CodeSetRevision.OwnerDomainID != 3 || snapshot.CodeSetRevision.Origin != models.CodeSetOriginTenant || snapshot.CodeSetRevision.Status != "withdrawn" || len(snapshot.CodeSetRevision.Items) != 2 || snapshot.CodeSetRevision.Items[0].Code != "F" {
		t.Fatalf("code set snapshot = %#v", snapshot.CodeSetRevision)
	}
}

func TestElementRevisionResolutionRejectsInvalidBatch(t *testing.T) {
	db := openElementResolutionTestDB(t)
	svc := NewElementRevisionResolutionService(repository.NewElementRepository(db), repository.NewCodeSetRepository(db))
	_, err := svc.Resolve(context.Background(), 7, []int64{10, 10}, time.Now().UTC())
	if !errors.Is(err, ErrInvalidElementRevisionResolutionRequest) {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func openElementResolutionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE standard.elements (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, lifecycle_state TEXT NOT NULL)`,
		`CREATE TABLE standard.element_revisions (
			id INTEGER PRIMARY KEY, element_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL,
			name TEXT NOT NULL, definition TEXT NOT NULL, data_type TEXT NOT NULL, nullable NUMERIC NOT NULL,
			default_value TEXT, format TEXT, value_domain_kind TEXT NOT NULL, code_set_revision_id INTEGER,
			effective_from DATETIME, effective_to DATETIME)`,
		`CREATE TABLE standard.code_sets (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, scope_type TEXT NOT NULL, owner_domain_id INTEGER, origin TEXT NOT NULL, code TEXT NOT NULL, lifecycle_state TEXT NOT NULL)`,
		`CREATE TABLE standard.code_set_revisions (
			id INTEGER PRIMARY KEY, code_set_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL,
			name TEXT NOT NULL, description TEXT NOT NULL, value_type TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME)`,
		`CREATE TABLE standard.code_set_revision_items (
			id INTEGER PRIMARY KEY, code_set_revision_id INTEGER NOT NULL, code TEXT NOT NULL, label TEXT NOT NULL,
			definition TEXT, sort_order INTEGER, status TEXT, replacement_item_id INTEGER)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
