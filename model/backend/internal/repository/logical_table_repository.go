package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
)

type LogicalTableRepository struct {
	db *gorm.DB
}

func NewLogicalTableRepository(db *gorm.DB) *LogicalTableRepository {
	return &LogicalTableRepository{db: db}
}

func (r *LogicalTableRepository) DB() *gorm.DB {
	return r.db
}

func (r *LogicalTableRepository) Create(table *models.LogicalTable) error {
	return commonrepo.WrapDBError(r.db.Create(table).Error)
}

func (r *LogicalTableRepository) GetByID(id, tenantID int64) (*models.LogicalTable, error) {
	var table models.LogicalTable
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&table).Error
	return &table, commonrepo.WrapDBError(err)
}

type ListLogicalTableOptions struct {
	DomainID  *int64
	Layer     string
	TableType string
	Status    string
	Keyword   string
	Page      int
	PageSize  int
}

func (r *LogicalTableRepository) List(tenantID int64, opts ListLogicalTableOptions) ([]models.LogicalTable, int64, error) {
	if opts.Page <= 0 {
		opts.Page = 1
	}
	if opts.PageSize <= 0 {
		opts.PageSize = 20
	}
	if opts.PageSize > 100 {
		opts.PageSize = 100
	}
	query := r.db.Model(&models.LogicalTable{}).Where("tenant_id = ?", tenantID)

	if opts.DomainID != nil {
		query = query.Where("domain_id = ?", *opts.DomainID)
	}
	if opts.Layer != "" {
		query = query.Where("layer = ?", opts.Layer)
	}
	if opts.TableType != "" {
		query = query.Where("table_type = ?", opts.TableType)
	}
	if opts.Status != "" {
		query = query.Where("status = ?", opts.Status)
	}
	if opts.Keyword != "" {
		query = query.Where("name ILIKE ? OR code ILIKE ? OR description ILIKE ?",
			"%"+opts.Keyword+"%", "%"+opts.Keyword+"%", "%"+opts.Keyword+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (opts.Page - 1) * opts.PageSize

	var tables []models.LogicalTable
	err := query.Order("created_at DESC, id DESC").Offset(offset).Limit(opts.PageSize).Find(&tables).Error
	return tables, total, err
}

func (r *LogicalTableRepository) Update(table *models.LogicalTable) error {
	return commonrepo.WrapDBError(r.db.Save(table).Error)
}

func (r *LogicalTableRepository) Delete(id, tenantID int64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var table models.LogicalTable
		if err := tx.Select("id").Where("id = ? AND tenant_id = ?", id, tenantID).First(&table).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if err := tx.Where("fact_table_id = ? AND tenant_id = ?", id, tenantID).Delete(&models.FactMetricMapping{}).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id = ? AND (source_table = ? OR target_table = ?)", tenantID, id, id).Delete(&models.TableRelation{}).Error; err != nil {
			return err
		}
		if err := tx.Where("table_id = ?", id).Delete(&models.LogicalField{}).Error; err != nil {
			return err
		}
		result := tx.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.LogicalTable{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
		}
		return nil
	})
}

func (r *LogicalTableRepository) UpdateStatus(id, tenantID int64, status string, updatedBy int64) error {
	result := r.db.Model(&models.LogicalTable{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{"status": status, "updated_by": updatedBy})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *LogicalTableRepository) ExistsByCode(code string, tenantID int64, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.LogicalTable{}).Where("code = ? AND tenant_id = ?", code, tenantID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}

// GetFields 获取逻辑表字段列表
func (r *LogicalTableRepository) GetFields(tableID int64) ([]models.LogicalField, error) {
	var fields []models.LogicalField
	err := r.db.Where("table_id = ?", tableID).Order("sort_order ASC, id ASC").Find(&fields).Error
	return fields, err
}

// CreateField 创建字段
func (r *LogicalTableRepository) CreateField(field *models.LogicalField) error {
	return commonrepo.WrapDBError(r.db.Create(field).Error)
}

// GetFieldByID 按 ID 获取字段
func (r *LogicalTableRepository) GetFieldByID(fieldID, tableID int64) (*models.LogicalField, error) {
	var field models.LogicalField
	err := r.db.Where("id = ? AND table_id = ?", fieldID, tableID).First(&field).Error
	return &field, commonrepo.WrapDBError(err)
}

// UpdateField 更新字段
func (r *LogicalTableRepository) UpdateField(field *models.LogicalField) error {
	return commonrepo.WrapDBError(r.db.Save(field).Error)
}

// DeleteField 删除字段
func (r *LogicalTableRepository) DeleteField(fieldID, tableID int64) error {
	result := r.db.Where("id = ? AND table_id = ?", fieldID, tableID).Delete(&models.LogicalField{})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}
