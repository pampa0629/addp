package service

import (
	"errors"
	"os"
	"testing"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
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
	for _, consumer := range []string{"element", "metric"} {
		t.Run(consumer+"_reference", func(t *testing.T) {
			referenced, err := svc.CreateUnit(&models.CreateUnitRequest{Code: "joule_" + consumer, CategoryID: category.ID, Translations: unitTestTranslations("焦耳", "Joule")}, platform)
			if err != nil {
				t.Fatal(err)
			}
			refs := repository.NewTenantReferenceRepository(tx)
			var savedUnitID *int64
			if consumer == "element" {
				elements := NewElementService(repository.NewElementRepository(tx), nil, refs, nil)
				created, err := elements.CreateElement(&models.CreateElementRequest{ScopeType: models.StandardScopeTenantCommon, Code: "energy_element", Name: "Energy", Definition: "Energy measurement", DataType: "float", ValueDomainKind: models.ValueDomainUnrestricted, UnitID: &referenced.ID}, tenant.TenantID, 1, "Create shared unit reference")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := elements.UpdateRevision(created.ID, created.DraftRevision.ID, tenant.TenantID, 1, &models.UpdateElementRevisionRequest{Version: created.Version, Name: "Energy", Definition: "Updated energy measurement", DataType: "float", ValueDomainKind: models.ValueDomainUnrestricted, UnitID: &referenced.ID, ChangeSummary: "Save shared unit reference"}); err != nil {
					t.Fatal(err)
				}
				reopened, err := elements.GetElement(created.ID, tenant.TenantID)
				if err != nil {
					t.Fatal(err)
				}
				savedUnitID = reopened.DraftRevision.UnitID
			} else {
				metrics := NewMetricService(repository.NewMetricCategoryRepository(tx), repository.NewMetricRepository(tx), refs, nil)
				created, err := metrics.CreateMetric(&models.CreateMetricRequest{ScopeType: models.StandardScopeTenantCommon, Code: "energy_metric", MetricType: models.MetricTypeAtomic, Name: "Energy", Definition: "Energy measurement", StatisticalCaliber: "All measurements", UnitID: &referenced.ID}, tenant.TenantID, 1, "Create shared unit reference")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := metrics.UpdateRevision(created.ID, created.DraftRevision.ID, tenant.TenantID, 1, &models.UpdateMetricRevisionRequest{Version: created.Version, MetricType: models.MetricTypeAtomic, Name: "Energy", Definition: "Updated energy measurement", StatisticalCaliber: "All measurements", UnitID: &referenced.ID, ChangeSummary: "Save shared unit reference"}); err != nil {
					t.Fatal(err)
				}
				reopened, err := metrics.GetMetric(created.ID, tenant.TenantID)
				if err != nil {
					t.Fatal(err)
				}
				savedUnitID = reopened.DraftRevision.UnitID
			}
			if savedUnitID == nil || *savedUnitID != referenced.ID {
				t.Fatalf("saved unit identity changed: %v", savedUnitID)
			}
			for lang, expected := range map[string]string{"zh-cn": "焦耳", "en": "Joule"} {
				localized, err := svc.GetUnit(*savedUnitID, tenant)
				if err != nil {
					t.Fatal(err)
				}
				localized.Localize(lang)
				if localized.ID != referenced.ID || localized.Name != expected {
					t.Fatalf("localized reference changed: %+v", localized)
				}
			}
			if err := svc.DeleteUnit(referenced.ID, platform); !errors.Is(err, ErrUnitReferenced) {
				t.Fatalf("referenced unit deletion: %v", err)
			}
			if _, err := svc.GetUnit(referenced.ID, tenant); err != nil {
				t.Fatalf("referenced unit lost after rejected deletion: %v", err)
			}
		})
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
