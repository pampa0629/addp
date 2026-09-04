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

func (r *LogicalTableRepository) GetByIDs(ids []int64, tenantID int64) ([]models.LogicalTable, error) {
	if len(ids) == 0 {
		return []models.LogicalTable{}, nil
	}
	var tables []models.LogicalTable
	err := r.db.Where("tenant_id = ? AND id IN ?", tenantID, ids).Order("id ASC").Find(&tables).Error
	return tables, commonrepo.WrapDBError(err)
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
		return nil, 0, commonrepo.WrapDBError(err)
	}

	offset := (opts.Page - 1) * opts.PageSize

	var tables []models.LogicalTable
	err := query.Order("created_at DESC, id DESC").Offset(offset).Limit(opts.PageSize).Find(&tables).Error
	return tables, total, commonrepo.WrapDBError(err)
}

func (r *LogicalTableRepository) ListApproved(tenantID int64) ([]models.LogicalTable, error) {
	var tables []models.LogicalTable
	err := r.db.Where("tenant_id = ? AND status = ?", tenantID, "approved").
		Order("created_at DESC, id DESC").Find(&tables).Error
	return tables, commonrepo.WrapDBError(err)
}

func (r *LogicalTableRepository) Update(table *models.LogicalTable) error {
	result := r.db.Model(&models.LogicalTable{}).
		Where("id = ? AND tenant_id = ? AND version = ?", table.ID, table.TenantID, table.Version).
		Updates(map[string]interface{}{
			"domain_id": table.DomainID, "entity_id": table.EntityID, "name": table.Name,
			"description": table.Description, "table_type": table.TableType, "layer": table.Layer,
			"grain_description": table.GrainDescription, "scd_type": table.SCDType,
			"materialization": table.Materialization, "updated_by": table.UpdatedBy,
			"version": gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	table.Version++
	return nil
}

func (r *LogicalTableRepository) Delete(id, tenantID, version int64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var table models.LogicalTable
		if err := tx.Select("id").Where("id = ? AND tenant_id = ?", id, tenantID).First(&table).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if err := tx.Where("fact_table_id = ? AND tenant_id = ?", id, tenantID).Delete(&models.FactMetricMapping{}).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if err := tx.Where("tenant_id = ? AND (source_table = ? OR target_table = ?)", tenantID, id, id).Delete(&models.TableRelation{}).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if err := tx.Where("table_id = ? AND tenant_id = ?", id, tenantID).Delete(&models.DimensionHierarchy{}).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if err := tx.Where("table_id = ?", id).Delete(&models.LogicalField{}).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		result := tx.Where("id = ? AND tenant_id = ? AND version = ?", id, tenantID, version).Delete(&models.LogicalTable{})
		if result.Error != nil {
			return commonrepo.WrapDBError(result.Error)
		}
		if result.RowsAffected == 0 {
			return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
		}
		return nil
	})
}

func (r *LogicalTableRepository) UpdateStatus(id, tenantID, version int64, status string, updatedBy int64) error {
	result := r.db.Model(&models.LogicalTable{}).
		Where("id = ? AND tenant_id = ? AND version = ?", id, tenantID, version).
		Updates(map[string]interface{}{"status": status, "updated_by": updatedBy, "version": gorm.Expr("version + 1")})
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
	return count > 0, commonrepo.WrapDBError(err)
}

// GetFields 获取逻辑表字段列表
func (r *LogicalTableRepository) GetFields(tableID int64) ([]models.LogicalField, error) {
	var fields []models.LogicalField
	err := r.db.Where("table_id = ?", tableID).Order("sort_order ASC, id ASC").Find(&fields).Error
	return fields, commonrepo.WrapDBError(err)
}

func (r *LogicalTableRepository) FreezeFieldElementRevisions(tableID int64, bindings map[int64]int64) error {
	if err := r.db.Model(&models.LogicalField{}).Where("table_id = ?", tableID).Update("element_revision_id", nil).Error; err != nil {
		return commonrepo.WrapDBError(err)
	}
	for elementID, revisionID := range bindings {
		result := r.db.Model(&models.LogicalField{}).
			Where("table_id = ? AND element_id = ?", tableID, elementID).
			Update("element_revision_id", revisionID)
		if result.Error != nil {
			return commonrepo.WrapDBError(result.Error)
		}
		if result.RowsAffected == 0 {
			return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
		}
	}
	return nil
}

func (r *LogicalTableRepository) ClearFieldElementRevisions(tableID int64) error {
	return commonrepo.WrapDBError(r.db.Model(&models.LogicalField{}).Where("table_id = ?", tableID).Update("element_revision_id", nil).Error)
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
