package service

import (
	"testing"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUnitListsInitializeTenantDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, statement := range []string{
		"ATTACH DATABASE ':memory:' AS standard",
		`CREATE TABLE standard.measurement_categories (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, name TEXT, code TEXT, description TEXT, sort_order INTEGER, is_system BOOLEAN, created_at DATETIME, updated_at DATETIME, version INTEGER DEFAULT 1, UNIQUE(tenant_id, code))`,
		`CREATE TABLE standard.units (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, category_id INTEGER NOT NULL, name TEXT, symbol TEXT, description TEXT, sort_order INTEGER, is_system BOOLEAN, created_at DATETIME, updated_at DATETIME, version INTEGER DEFAULT 1)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := NewUnitService(repository.NewMeasurementCategoryRepository(db), repository.NewUnitRepository(db))
	units, err := svc.ListUnits(7, nil)
	if err != nil || len(units) != 31 {
		t.Fatalf("first units read: count=%d err=%v", len(units), err)
	}
	categories, err := svc.ListCategories(7)
	if err != nil || len(categories) != 10 {
		t.Fatalf("categories: count=%d err=%v", len(categories), err)
	}
	for _, unit := range units {
		if !unit.IsSystem || unit.Category == nil || unit.Category.TenantID != 7 {
			t.Fatalf("invalid default: %+v", unit)
		}
	}
	if err := db.Model(&categories[0]).Update("name", "我的计数").Error; err != nil {
		t.Fatal(err)
	}
	custom := models.Unit{TenantID: 7, CategoryID: categories[0].ID, Name: "箱", Symbol: "箱"}
	if err := db.Create(&custom).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&units[0]).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		got, err := svc.ListUnits(7, nil)
		if err != nil || len(got) != 32 {
			t.Fatalf("repair/idempotence: count=%d err=%v", len(got), err)
		}
		cats, err := svc.ListCategories(7)
		if err != nil || cats[0].ID != categories[0].ID || cats[0].Name != "我的计数" {
			t.Fatalf("existing category changed: %+v %v", cats, err)
		}
	}
	other, err := svc.ListCategories(8)
	if err != nil || len(other) != 10 || other[0].ID == categories[0].ID {
		t.Fatalf("tenant isolation: %+v %v", other, err)
	}
	filtered, err := svc.ListUnits(8, &categories[0].ID)
	if err != nil || len(filtered) != 0 {
		t.Fatalf("foreign category: %+v %v", filtered, err)
	}
	if _, err := svc.ListCategories(0); err == nil {
		t.Fatal("missing tenant must be rejected")
	}
	// 故障发生在类别插入后时，整个目录必须回滚。
	if err := db.Exec("DROP TABLE standard.units").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListCategories(9); err == nil {
		t.Fatal("initialization failure must reach the caller")
	}
	var count int64
	if err := db.Model(&models.MeasurementCategory{}).Where("tenant_id = ?", 9).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("partial initialization survived rollback: count=%d err=%v", count, err)
	}
}
