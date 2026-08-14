package repository

import (
	"errors"
	"testing"

	"github.com/addp/model/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupStandardReferenceGuardTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS model").Error; err != nil {
		t.Fatalf("attach model schema: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE model.standard_reference_guards (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			resource_type TEXT NOT NULL,
			resource_id INTEGER NOT NULL,
			state TEXT NOT NULL DEFAULT 'open',
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE (tenant_id, resource_type, resource_id)
		)`,
		`CREATE TABLE model.entities (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			domain_id INTEGER
		)`,
		`CREATE TABLE model.logical_tables (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			domain_id INTEGER
		)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create guard test table: %v", err)
		}
	}
	return db
}

func TestStandardReferenceGuardStateMachineAndImpactScan(t *testing.T) {
	db := setupStandardReferenceGuardTestDB(t)
	for _, statement := range []string{
		`INSERT INTO model.entities (tenant_id, domain_id) VALUES (7, 42), (8, 42)`,
		`INSERT INTO model.logical_tables (tenant_id, domain_id) VALUES (7, 42)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed reference: %v", err)
		}
	}

	repo := NewStandardReferenceGuardRepository(db)
	impact, err := repo.SetState(7, models.StandardResourceDomain, 42, models.StandardReferenceGuardFrozen)
	if err != nil {
		t.Fatalf("freeze guard: %v", err)
	}
	if impact.State != models.StandardReferenceGuardFrozen || impact.ReferenceCount != 2 {
		t.Fatalf("freeze impact = %+v, want frozen with 2 references", impact)
	}
	if len(impact.Summary) != 2 || len(impact.Sample) != 2 || impact.SampleTruncated {
		t.Fatalf("freeze impact details = %+v", impact)
	}
	if err := LockStandardReferences(db, 7, models.StandardReference{ResourceType: models.StandardResourceDomain, ResourceID: 42}); !errors.Is(err, ErrStandardReferenceFrozen) {
		t.Fatalf("tenant 7 lock error = %v, want ErrStandardReferenceFrozen", err)
	}
	if err := LockStandardReferences(db, 8, models.StandardReference{ResourceType: models.StandardResourceDomain, ResourceID: 42}); err != nil {
		t.Fatalf("tenant 8 lock should remain open: %v", err)
	}

	opened, err := repo.SetState(7, models.StandardResourceDomain, 42, models.StandardReferenceGuardOpen)
	if err != nil || opened.State != models.StandardReferenceGuardOpen {
		t.Fatalf("open guard = %+v, err=%v", opened, err)
	}
	if err := LockStandardReferences(db, 7, models.StandardReference{ResourceType: models.StandardResourceDomain, ResourceID: 42}); err != nil {
		t.Fatalf("lock reopened guard: %v", err)
	}
	if _, err := repo.SetState(7, models.StandardResourceDomain, 42, models.StandardReferenceGuardFrozen); err != nil {
		t.Fatalf("freeze reopened guard: %v", err)
	}
	deleted, err := repo.SetState(7, models.StandardResourceDomain, 42, models.StandardReferenceGuardDeleted)
	if err != nil || deleted.State != models.StandardReferenceGuardDeleted {
		t.Fatalf("delete guard = %+v, err=%v", deleted, err)
	}
	if _, err := repo.SetState(7, models.StandardResourceDomain, 42, models.StandardReferenceGuardDeleted); err != nil {
		t.Fatalf("repeat deleted transition should be idempotent: %v", err)
	}
	if _, err := repo.SetState(7, models.StandardResourceDomain, 42, models.StandardReferenceGuardOpen); !errors.Is(err, ErrStandardReferenceGuardTerminal) {
		t.Fatalf("deleted guard reopen error = %v, want ErrStandardReferenceGuardTerminal", err)
	}
	if err := LockStandardReferences(db, 7, models.StandardReference{ResourceType: models.StandardResourceDomain, ResourceID: 42}); !errors.Is(err, ErrStandardReferenceFrozen) {
		t.Fatalf("deleted guard lock error = %v, want ErrStandardReferenceFrozen", err)
	}
}
