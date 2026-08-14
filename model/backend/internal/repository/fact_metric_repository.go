package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
)

type FactMetricRepository struct {
	db *gorm.DB
}

func NewFactMetricRepository(db *gorm.DB) *FactMetricRepository {
	return &FactMetricRepository{db: db}
}

func (r *FactMetricRepository) DB() *gorm.DB { return r.db }

// ListByFactTable 获取事实表关联的所有指标
func (r *FactMetricRepository) ListByFactTable(factTableID, tenantID int64) ([]models.FactMetricMapping, error) {
	var mappings []models.FactMetricMapping
	err := r.db.Where("fact_table_id = ? AND tenant_id = ?", factTableID, tenantID).
		Order("created_at ASC").
		Find(&mappings).Error
	return mappings, commonrepo.WrapDBError(err)
}

// Create 创建关联
func (r *FactMetricRepository) Create(m *models.FactMetricMapping) error {
	return commonrepo.WrapDBError(r.db.Create(m).Error)
}

// Exists 判断关联是否已存在
func (r *FactMetricRepository) Exists(factTableID, metricID, tenantID int64) (bool, error) {
	var count int64
	err := r.db.Model(&models.FactMetricMapping{}).
		Where("fact_table_id = ? AND metric_id = ? AND tenant_id = ?", factTableID, metricID, tenantID).
		Count(&count).Error
	return count > 0, commonrepo.WrapDBError(err)
}

// Delete 删除关联（按 ID）
func (r *FactMetricRepository) Delete(id, factTableID, tenantID int64) error {
	result := r.db.Where("id = ? AND fact_table_id = ? AND tenant_id = ?", id, factTableID, tenantID).
		Delete(&models.FactMetricMapping{})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}
