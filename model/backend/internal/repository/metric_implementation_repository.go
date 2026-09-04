package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
)

type MetricImplementationRepository struct{ db *gorm.DB }

func NewMetricImplementationRepository(db *gorm.DB) *MetricImplementationRepository {
	return &MetricImplementationRepository{db: db}
}
func (r *MetricImplementationRepository) DB() *gorm.DB { return r.db }
func (r *MetricImplementationRepository) ListByFactTable(factTableID, tenantID int64) ([]models.MetricImplementation, error) {
	var items []models.MetricImplementation
	err := r.db.Where("fact_table_id = ? AND tenant_id = ?", factTableID, tenantID).Order("created_at ASC,id ASC").Find(&items).Error
	return items, commonrepo.WrapDBError(err)
}
func (r *MetricImplementationRepository) GetByID(id, factTableID, tenantID int64) (*models.MetricImplementation, error) {
	var item models.MetricImplementation
	err := r.db.Where("id = ? AND fact_table_id = ? AND tenant_id = ?", id, factTableID, tenantID).First(&item).Error
	return &item, commonrepo.WrapDBError(err)
}
func (r *MetricImplementationRepository) Create(item *models.MetricImplementation) error {
	return commonrepo.WrapDBError(r.db.Create(item).Error)
}
func (r *MetricImplementationRepository) Update(item *models.MetricImplementation) error {
	return commonrepo.WrapDBError(r.db.Model(&models.MetricImplementation{}).Where("id = ? AND fact_table_id = ? AND tenant_id = ?", item.ID, item.FactTableID, item.TenantID).Updates(map[string]interface{}{
		"metric_definition_id": item.MetricDefinitionID, "metric_definition_revision_id": item.MetricDefinitionRevisionID, "name": item.Name, "grain": item.Grain, "source_config": item.SourceConfig, "dimension_config": item.DimensionConfig, "filter_config": item.FilterConfig, "expression_config": item.ExpressionConfig, "status": item.Status, "note": item.Note, "updated_by": item.UpdatedBy,
	}).Error)
}
func (r *MetricImplementationRepository) Delete(id, factTableID, tenantID int64) error {
	result := r.db.Where("id = ? AND fact_table_id = ? AND tenant_id = ?", id, factTableID, tenantID).Delete(&models.MetricImplementation{})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}
