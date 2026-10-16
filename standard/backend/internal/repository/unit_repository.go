package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

type MeasurementCategoryRepository struct{ db *gorm.DB }

func NewMeasurementCategoryRepository(db *gorm.DB) *MeasurementCategoryRepository {
	return &MeasurementCategoryRepository{db: db}
}

// unitOwnerQuery 表达平台目录与当前 Tenant 自定义资源的唯一可见性规则。
func unitOwnerQuery(db *gorm.DB, owner models.UnitContext, includePlatform bool) *gorm.DB {
	if owner.Type == "platform" {
		return db.Where("tenant_id IS NULL")
	}
	if includePlatform {
		return db.Where("(tenant_id IS NULL OR tenant_id = ?)", owner.TenantID)
	}
	return db.Where("tenant_id = ?", owner.TenantID)
}
func (r *MeasurementCategoryRepository) List(owner models.UnitContext) ([]models.MeasurementCategory, error) {
	var rows []models.MeasurementCategory
	err := unitOwnerQuery(r.db, owner, true).Order("sort_order ASC, id ASC").Find(&rows).Error
	return rows, err
}
func (r *MeasurementCategoryRepository) GetByID(id int64, owner models.UnitContext) (*models.MeasurementCategory, error) {
	var row models.MeasurementCategory
	err := unitOwnerQuery(r.db, owner, true).Where("id = ?", id).First(&row).Error
	return &row, commonrepo.WrapDBError(err)
}
func (r *MeasurementCategoryRepository) Create(row *models.MeasurementCategory) error {
	return wrapDBError(r.db.Create(row).Error)
}
func updateUnitVersioned(db *gorm.DB, model interface{}, id int64, tenantID *int64, version int64, updates map[string]interface{}) error {
	if tenantID != nil {
		return updateVersioned(db, model, id, *tenantID, version, updates)
	}
	updates["version"] = gorm.Expr("version + 1")
	result := db.Model(model).Where("id = ? AND tenant_id IS NULL AND version = ?", id, version).Updates(updates)
	if result.Error != nil {
		return wrapDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrVersionConflict
	}
	return nil
}
func (r *MeasurementCategoryRepository) Update(row *models.MeasurementCategory, version int64) error {
	updates := map[string]interface{}{"name": row.Name, "description": row.Description, "sort_order": row.SortOrder}
	if row.TenantID == nil {
		updates["name"] = ""
		updates["description"] = ""
		updates["translations"] = mustUnitTranslationsJSON(row.Translations)
	}
	if err := updateUnitVersioned(r.db, row, row.ID, row.TenantID, version, updates); err != nil {
		return err
	}
	row.Version = version + 1
	return nil
}
func (r *MeasurementCategoryRepository) Delete(id int64, owner models.UnitContext) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		return requireAffectedRow(unitOwnerQuery(tx, owner, false).Where("id = ?", id).Delete(&models.MeasurementCategory{}))
	}))
}
func (r *MeasurementCategoryRepository) ExistsByCode(code string, owner models.UnitContext, excludeID int64) (bool, error) {
	var count int64
	q := unitOwnerQuery(r.db.Model(&models.MeasurementCategory{}), owner, false).Where("code = ?", code)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	err := q.Count(&count).Error
	return count > 0, err
}

type UnitRepository struct{ db *gorm.DB }

func NewUnitRepository(db *gorm.DB) *UnitRepository { return &UnitRepository{db: db} }
func (r *UnitRepository) List(owner models.UnitContext, categoryID *int64) ([]models.Unit, error) {
	var rows []models.Unit
	q := unitOwnerQuery(r.db, owner, true)
	if categoryID != nil {
		q = q.Where("category_id = ?", *categoryID)
	}
	err := q.Preload("Category").Order("sort_order ASC,id ASC").Find(&rows).Error
	return rows, err
}
func (r *UnitRepository) GetByID(id int64, owner models.UnitContext) (*models.Unit, error) {
	var row models.Unit
	err := unitOwnerQuery(r.db, owner, true).Where("id = ?", id).Preload("Category").First(&row).Error
	return &row, commonrepo.WrapDBError(err)
}
func (r *UnitRepository) Create(row *models.Unit) error { return wrapDBError(r.db.Create(row).Error) }
func (r *UnitRepository) Update(row *models.Unit, version int64) error {
	updates := map[string]interface{}{"name": row.Name, "symbol": row.Symbol, "description": row.Description, "sort_order": row.SortOrder}
	if row.TenantID == nil {
		updates["name"] = ""
		updates["description"] = ""
		updates["symbol"] = ""
		updates["translations"] = mustUnitTranslationsJSON(row.Translations)
	}
	if err := updateUnitVersioned(r.db, row, row.ID, row.TenantID, version, updates); err != nil {
		return err
	}
	row.Version = version + 1
	return nil
}
func (r *UnitRepository) Delete(id int64, owner models.UnitContext) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		return requireAffectedRow(unitOwnerQuery(tx, owner, false).Where("id = ?", id).Delete(&models.Unit{}))
	}))
}
