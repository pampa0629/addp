package repository

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

// 初始参考数据是部署资源；运行期目录与译名的事实源是数据库。
//
//go:embed data/measurement_catalog.json
var measurementCatalogJSON []byte

const measurementCatalogMigration int64 = 2026101601

type measurementCatalog struct {
	Categories []struct {
		Code         string                  `json:"code"`
		SortOrder    int                     `json:"sort_order"`
		Translations models.UnitTranslations `json:"translations"`
	} `json:"categories"`
	Units []struct {
		Code         string                  `json:"code"`
		CategoryCode string                  `json:"category_code"`
		SortOrder    int                     `json:"sort_order"`
		Translations models.UnitTranslations `json:"translations"`
	} `json:"units"`
}

func mustUnitTranslationsJSON(v models.UnitTranslations) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func migrateMeasurementCatalog(tx *gorm.DB) error {
	if tx.Dialector.Name() != "postgres" {
		return nil
	}
	var applied int64
	if err := tx.Table("standard.data_migrations").Where("version = ?", measurementCatalogMigration).Count(&applied).Error; err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}
	var seed measurementCatalog
	if err := json.Unmarshal(measurementCatalogJSON, &seed); err != nil {
		return fmt.Errorf("decode measurement catalog: %w", err)
	}
	categoryIDs := map[string]int64{}
	categoryMoves := map[int64]int64{}
	legacyCategories := []models.MeasurementCategory{}
	legacyUnits := []models.Unit{}
	hasLegacy := tx.Migrator().HasColumn("standard.measurement_categories", "is_system")
	if hasLegacy {
		if err := tx.Where("is_system = true").Order("id").Find(&legacyCategories).Error; err != nil {
			return err
		}
		if err := tx.Where("is_system = true").Order("id").Find(&legacyUnits).Error; err != nil {
			return err
		}
	}
	for _, entry := range seed.Categories {
		row := models.MeasurementCategory{Code: entry.Code, SortOrder: entry.SortOrder, Translations: entry.Translations}
		// 仅合并仍保持原目录名称的系统项；Tenant 已改名的类别保留为自定义项。
		for _, old := range legacyCategories {
			if old.Code == entry.Code && old.Name == entry.Translations["zh-cn"].Name && old.Description == entry.Translations["zh-cn"].Description && old.SortOrder == entry.SortOrder && old.Version == 1 {
				row.ID = old.ID
				break
			}
		}
		if row.ID == 0 {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&models.MeasurementCategory{}).Where("id = ?", row.ID).Updates(map[string]interface{}{"tenant_id": nil, "name": "", "description": "", "translations": mustUnitTranslationsJSON(entry.Translations)}).Error; err != nil {
				return err
			}
		}
		categoryIDs[entry.Code] = row.ID
		for _, old := range legacyCategories {
			if old.Code == entry.Code && old.Name == entry.Translations["zh-cn"].Name && old.Description == entry.Translations["zh-cn"].Description && old.SortOrder == entry.SortOrder && old.Version == 1 {
				categoryMoves[old.ID] = row.ID
			}
		}
	}
	for _, entry := range seed.Units {
		row := models.Unit{Code: entry.Code, CategoryID: categoryIDs[entry.CategoryCode], SortOrder: entry.SortOrder, Translations: entry.Translations}
		matches := []int64{}
		for _, old := range legacyUnits {
			if categoryMoves[old.CategoryID] == row.CategoryID && old.Name == entry.Translations["zh-cn"].Name && old.Symbol == entry.Translations["zh-cn"].Symbol && old.Description == entry.Translations["zh-cn"].Description && old.SortOrder == entry.SortOrder && old.Version == 1 {
				matches = append(matches, old.ID)
			}
		}
		if len(matches) == 0 {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		} else {
			row.ID = matches[0]
			if err := tx.Model(&models.Unit{}).Where("id = ?", row.ID).Updates(map[string]interface{}{"tenant_id": nil, "code": row.Code, "category_id": row.CategoryID, "name": "", "symbol": "", "description": "", "translations": mustUnitTranslationsJSON(entry.Translations)}).Error; err != nil {
				return err
			}
			for _, oldID := range matches[1:] {
				for _, table := range []string{"standard.element_revisions", "standard.metric_definition_revisions"} {
					if err := tx.Table(table).Where("unit_id = ?", oldID).Update("unit_id", row.ID).Error; err != nil {
						return err
					}
				}
				if err := tx.Delete(&models.Unit{}, oldID).Error; err != nil {
					return err
				}
			}
		}
	}
	for oldID, newID := range categoryMoves {
		if oldID != newID {
			if err := tx.Model(&models.Unit{}).Where("category_id = ?", oldID).Update("category_id", newID).Error; err != nil {
				return err
			}
			if err := tx.Delete(&models.MeasurementCategory{}, oldID).Error; err != nil {
				return err
			}
		}
	}
	for _, table := range []string{"standard.units", "standard.measurement_categories"} {
		if err := tx.Exec("ALTER TABLE " + table + " DROP COLUMN IF EXISTS is_system").Error; err != nil {
			return err
		}
	}
	for _, statement := range []string{
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_platform_measurement_category_code ON standard.measurement_categories(code) WHERE tenant_id IS NULL",
		"CREATE UNIQUE INDEX IF NOT EXISTS uq_standard_platform_unit_code ON standard.units(code) WHERE tenant_id IS NULL",
		"ALTER TABLE standard.measurement_categories ADD CONSTRAINT ck_standard_measurement_category_owner CHECK (tenant_id IS NULL OR tenant_id > 0)",
		"ALTER TABLE standard.units ADD CONSTRAINT ck_standard_unit_owner CHECK (tenant_id IS NULL OR tenant_id > 0)",
		"ALTER TABLE standard.measurement_categories ADD CONSTRAINT ck_standard_platform_category_text CHECK (tenant_id IS NOT NULL OR (name = '' AND COALESCE(description,'') = '' AND COALESCE(translations->'zh-cn'->>'name','') <> '' AND COALESCE(translations->'en'->>'name','') <> ''))",
		"ALTER TABLE standard.units ADD CONSTRAINT ck_standard_platform_unit_text CHECK (tenant_id IS NOT NULL OR (code <> '' AND name = '' AND COALESCE(description,'') = '' AND COALESCE(symbol,'') = '' AND COALESCE(translations->'zh-cn'->>'name','') <> '' AND COALESCE(translations->'en'->>'name','') <> ''))",
	} {
		if err := tx.Exec(statement).Error; err != nil {
			return err
		}
	}
	return tx.Exec("INSERT INTO standard.data_migrations(version,name) VALUES (?,?)", measurementCatalogMigration, "standard_platform_measurement_catalog_v1").Error
}
