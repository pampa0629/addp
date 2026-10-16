package service

import (
	"errors"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
)

func setupUnitCatalogDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, sql := range []string{
		"ATTACH DATABASE ':memory:' AS standard",
		`CREATE TABLE standard.measurement_categories (id INTEGER PRIMARY KEY AUTOINCREMENT,tenant_id INTEGER,name TEXT NOT NULL DEFAULT '',code TEXT,description TEXT,sort_order INTEGER,translations TEXT,created_at DATETIME,updated_at DATETIME,version INTEGER DEFAULT 1,UNIQUE(tenant_id,code))`,
		`CREATE TABLE standard.units (id INTEGER PRIMARY KEY AUTOINCREMENT,tenant_id INTEGER,category_id INTEGER,name TEXT NOT NULL DEFAULT '',code TEXT,symbol TEXT,description TEXT,sort_order INTEGER,translations TEXT,created_at DATETIME,updated_at DATETIME,version INTEGER DEFAULT 1)`,
		`CREATE UNIQUE INDEX standard.uq_platform_category ON measurement_categories(code) WHERE tenant_id IS NULL`,
		`CREATE UNIQUE INDEX standard.uq_platform_unit ON units(code) WHERE tenant_id IS NULL`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func unitTestTranslations(zh, en string) models.UnitTranslations {
	return models.UnitTranslations{"zh-cn": {Name: zh, Symbol: "个"}, "en": {Name: en, Symbol: "item"}}
}
func TestUnitCatalogSharedIdentityAndTranslations(t *testing.T) {
	db := setupUnitCatalogDB(t)
	svc := NewUnitService(repository.NewMeasurementCategoryRepository(db), repository.NewUnitRepository(db))
	platform := models.UnitContext{Type: "platform"}
	tenant := models.TenantUnitContext(7)
	before, err := svc.ListCategories(tenant)
	if err != nil || len(before) != 0 {
		t.Fatalf("read must not initialize: %+v %v", before, err)
	}
	category, err := svc.CreateCategory(&models.CreateMeasurementCategoryRequest{Code: "count", Translations: unitTestTranslations("计数", "Count")}, platform)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := svc.CreateUnit(&models.CreateUnitRequest{Code: "item", CategoryID: category.ID, Translations: unitTestTranslations("个", "Item")}, platform)
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []models.UnitContext{tenant, models.TenantUnitContext(8), platform} {
		rows, err := svc.ListUnits(owner, nil)
		if err != nil || len(rows) != 1 || rows[0].ID != unit.ID || rows[0].TenantID != nil {
			t.Fatalf("shared directory: %+v %v", rows, err)
		}
		rows[0].Localize("en")
		if rows[0].Name != "Item" || rows[0].Category.Name != "Count" {
			t.Fatalf("English projection: %+v", rows[0])
		}
		rows[0].Localize("zh-cn")
		if rows[0].Name != "个" {
			t.Fatal(rows[0].Name)
		}
	}
	if _, err := svc.UpdateUnit(unit.ID, tenant, &models.UpdateUnitRequest{Version: 1, Name: "改写"}); !errors.Is(err, ErrPlatformUnitReadOnly) {
		t.Fatalf("tenant updated global unit: %v", err)
	}
	if err := svc.DeleteCategory(category.ID, tenant); !errors.Is(err, ErrPlatformCategoryReadOnly) {
		t.Fatalf("tenant deleted global category: %v", err)
	}
	custom, err := svc.CreateUnit(&models.CreateUnitRequest{CategoryID: category.ID, Name: "箱", Symbol: "箱"}, tenant)
	if err != nil {
		t.Fatal(err)
	}
	custom.Localize("en")
	if custom.Name != "箱" {
		t.Fatal("custom text was translated")
	}
	for _, owner := range []models.UnitContext{platform, models.TenantUnitContext(8)} {
		rows, err := svc.ListUnits(owner, nil)
		if err != nil || len(rows) != 1 {
			t.Fatalf("custom unit leaked: %+v %v", rows, err)
		}
	}
	if err := svc.DeleteUnit(custom.ID, models.TenantUnitContext(8)); err == nil {
		t.Fatal("cross-tenant delete allowed")
	}
	changed := unitTestTranslations("个", "Individual")
	if _, err := svc.UpdateUnit(unit.ID, platform, &models.UpdateUnitRequest{Version: unit.Version, Translations: changed}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetUnit(unit.ID, tenant)
	if err != nil {
		t.Fatal(err)
	}
	got.Localize("en")
	if got.Name != "Individual" {
		t.Fatalf("platform edit not immediately visible: %+v", got)
	}
	if _, err := svc.ListUnits(models.UnitContext{}, nil); err == nil {
		t.Fatal("invalid context accepted")
	}
}

func TestCandidateUnitMatchesCatalogIdentityAndTranslations(t *testing.T) {
	for _, text := range []string{"metre", "米", "Metre", "m"} {
		if !matchesCandidateUnit(text, "metre", "", "米", "Metre", "m") {
			t.Fatalf("same unit rejected: %s", text)
		}
	}
	if matchesCandidateUnit("kg", "metre", "", "米", "Metre", "m") {
		t.Fatal("different units conflated")
	}
}
