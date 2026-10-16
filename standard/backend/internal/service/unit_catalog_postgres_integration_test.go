package service

import (
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
)

func TestPostgresUnitCatalogDeploymentAndPlatformExtension(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	tx := db.Begin()
	defer tx.Rollback()
	if err := tx.Exec("DROP SCHEMA IF EXISTS standard CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(tx); err != nil {
		t.Fatal(err)
	}
	svc := NewUnitService(repository.NewMeasurementCategoryRepository(tx), repository.NewUnitRepository(tx))
	platform := models.UnitContext{Type: "platform"}
	tenant := models.TenantUnitContext(71)
	categories, err := svc.ListCategories(tenant)
	if err != nil || len(categories) != 10 {
		t.Fatalf("fresh categories=%d %v", len(categories), err)
	}
	units, err := svc.ListUnits(tenant, nil)
	if err != nil || len(units) != 36 {
		t.Fatalf("fresh units=%d %v", len(units), err)
	}
	for _, unit := range units {
		if unit.TenantID != nil || unit.Code == "" {
			t.Fatalf("not a shared identity: %+v", unit)
		}
		unit.Localize("en")
		if unit.Name == "" || unit.Category.Name == "" {
			t.Fatalf("missing translation: %+v", unit)
		}
	}
	category, err := svc.CreateCategory(&models.CreateMeasurementCategoryRequest{Code: "energy", Translations: unitTestTranslations("能量", "Energy")}, platform)
	if err != nil {
		t.Fatal(err)
	}
	unit, err := svc.CreateUnit(&models.CreateUnitRequest{Code: "joule", CategoryID: category.ID, Translations: unitTestTranslations("焦耳", "Joule")}, platform)
	if err != nil {
		t.Fatal(err)
	}
	updatedText := models.UnitTranslations{"zh-cn": {Name: "焦耳", Symbol: "J"}, "en": {Name: "Joule energy", Symbol: "J"}}
	if _, err := svc.UpdateUnit(unit.ID, platform, &models.UpdateUnitRequest{Version: unit.Version, Translations: updatedText}); err != nil {
		t.Fatal(err)
	}
	duplicateErr := tx.Transaction(func(nested *gorm.DB) error {
		nestedService := NewUnitService(repository.NewMeasurementCategoryRepository(nested), repository.NewUnitRepository(nested))
		_, err := nestedService.CreateUnit(&models.CreateUnitRequest{Code: "joule", CategoryID: category.ID, Translations: updatedText}, platform)
		return err
	})
	if duplicateErr == nil {
		t.Fatal("duplicate platform code accepted")
	}
	if err := repository.Migrate(tx); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetUnit(unit.ID, models.TenantUnitContext(72))
	if err != nil {
		t.Fatal(err)
	}
	got.Localize("en")
	if got.Name != "Joule energy" {
		t.Fatal(got.Name)
	}
	var duplicated int64
	if err := tx.Model(&models.Unit{}).Where("tenant_id IS NOT NULL").Count(&duplicated).Error; err != nil || duplicated != 0 {
		t.Fatalf("catalog was copied to tenants: %d %v", duplicated, err)
	}
	// 管理员删除一个初始条目后，后续部署迁移也不能把它重新种回来。
	var kilogram models.Unit
	if err := tx.Where("code = ? AND tenant_id IS NULL", "kilogram").First(&kilogram).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteUnit(kilogram.ID, platform); err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(tx); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := tx.Model(&models.Unit{}).Where("code = ? AND tenant_id IS NULL", "kilogram").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("deleted seed resurrected: %d %v", count, err)
	}
	if err := svc.DeleteUnit(unit.ID, tenant); err == nil {
		t.Fatal("tenant deleted a platform unit")
	}
}
