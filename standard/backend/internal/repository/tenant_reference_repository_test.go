package repository

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTenantReferenceRepositoryRejectsCrossTenantReferences(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatalf("attach standard schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE standard.domains (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, lifecycle_state TEXT NOT NULL DEFAULT 'active')`).Error; err != nil {
		t.Fatalf("create domains: %v", err)
	}
	if err := db.Exec(`CREATE TABLE standard.elements (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, lifecycle_state TEXT NOT NULL DEFAULT 'active')`).Error; err != nil {
		t.Fatalf("create elements: %v", err)
	}
	if err := db.Exec(`CREATE TABLE standard.element_revisions (id INTEGER PRIMARY KEY, element_id INTEGER NOT NULL, status TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME)`).Error; err != nil {
		t.Fatalf("create element revisions: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.domains (id, tenant_id) VALUES (1, 10), (2, 20)`).Error; err != nil {
		t.Fatalf("seed domains: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.elements (id, tenant_id) VALUES (1, 10), (2, 20)`).Error; err != nil {
		t.Fatalf("seed elements: %v", err)
	}
	if err := db.Exec(`INSERT INTO standard.element_revisions (id, element_id, status, effective_from) VALUES (101, 1, 'published', '2020-01-01 00:00:00'), (201, 2, 'published', '2020-01-01 00:00:00')`).Error; err != nil {
		t.Fatalf("seed element revisions: %v", err)
	}

	refs := NewTenantReferenceRepository(db)
	foreignDomain := int64(2)
	if err := refs.RequireDomain(10, &foreignDomain); err == nil {
		t.Fatal("expected cross-tenant domain to be rejected")
	}
	if err := refs.RequireElements(10, []int64{1, 2}); err == nil {
		t.Fatal("expected mixed-tenant elements to be rejected")
	}
	if err := refs.RequireElements(10, []int64{1, 1}); err != nil {
		t.Fatalf("duplicate same-tenant references should be accepted: %v", err)
	}

}
