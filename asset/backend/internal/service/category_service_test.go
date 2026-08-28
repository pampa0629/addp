package service

import (
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openCategoryServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS asset").Error; err != nil {
		t.Fatalf("attach asset schema: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE asset.categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			parent_id INTEGER,
			sort_order INTEGER NOT NULL DEFAULT 0,
			description TEXT NOT NULL DEFAULT '',
			version INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE asset.assets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			category_id INTEGER
		)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create category service table: %v", err)
		}
	}
	return db
}

func TestCategoryServiceUsesOptimisticConcurrency(t *testing.T) {
	service := NewCategoryService(openCategoryServiceTestDB(t))
	category, err := service.Create(7, &CreateAssetCategoryRequest{Name: "Education"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if category.Version != 1 {
		t.Fatalf("created version = %d, want 1", category.Version)
	}

	name := "Schools"
	updated, err := service.Update(7, category.ID, &UpdateAssetCategoryRequest{Version: 1, Name: &name})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != name || updated.Version != 2 {
		t.Fatalf("updated category = %#v", updated)
	}
	if _, err := service.Update(7, category.ID, &UpdateAssetCategoryRequest{Version: 1, Name: &name}); !errors.Is(err, ErrAssetCategoryVersionConflict) {
		t.Fatalf("stale Update() error = %v", err)
	}
	if err := service.Delete(7, category.ID, 1); !errors.Is(err, ErrAssetCategoryVersionConflict) {
		t.Fatalf("stale Delete() error = %v", err)
	}
	if err := service.Delete(7, category.ID, 2); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}
