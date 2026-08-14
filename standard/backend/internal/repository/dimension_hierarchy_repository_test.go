package repository

import (
	"github.com/addp/standard/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
)

func TestDimensionHierarchyListIncludesOrderedLevels(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatalf("attach standard schema: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE standard.dimension_hierarchies (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, domain_id INTEGER, name TEXT NOT NULL, code TEXT NOT NULL, description TEXT, created_by INTEGER NOT NULL DEFAULT 0, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL DEFAULT 'active')`,
		`CREATE TABLE standard.dimension_hierarchy_levels (id INTEGER PRIMARY KEY, hierarchy_id INTEGER NOT NULL, level_num INTEGER NOT NULL, name TEXT NOT NULL, element_id INTEGER, description TEXT, sort_order INTEGER NOT NULL DEFAULT 0)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create test table: %v", err)
		}
	}

	hierarchy := models.DimensionHierarchy{
		TenantID: 10,
		Name:     "地区",
		Code:     "region",
	}
	if err := db.Create(&hierarchy).Error; err != nil {
		t.Fatalf("create hierarchy: %v", err)
	}
	if err := db.Create(&models.DimensionHierarchyLevel{HierarchyID: hierarchy.ID, LevelNum: 2, Name: "市", SortOrder: 2}).Error; err != nil {
		t.Fatalf("create city level: %v", err)
	}
	if err := db.Create(&models.DimensionHierarchyLevel{HierarchyID: hierarchy.ID, LevelNum: 1, Name: "省", SortOrder: 1}).Error; err != nil {
		t.Fatalf("create province level: %v", err)
	}

	list, err := NewDimensionHierarchyRepository(db).List(10)
	if err != nil {
		t.Fatalf("list hierarchies: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("hierarchy count = %d, want 1", len(list))
	}
	if len(list[0].Levels) != 2 {
		t.Fatalf("level count = %d, want 2", len(list[0].Levels))
	}
	if list[0].Levels[0].Name != "省" || list[0].Levels[1].Name != "市" {
		t.Fatalf("levels order = %#v, want 省 -> 市", list[0].Levels)
	}
}
