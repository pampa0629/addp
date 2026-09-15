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
	items := []models.MetricImplementation{}
	q := r.db.Where("tenant_id = ?", tenantID)
	if factTableID > 0 {
		q = q.Where("fact_table_id = ?", factTableID)
	}
	if err := q.Order("id ASC").Find(&items).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	ids := make([]int64, len(items))
	for i := range items {
		ids[i] = items[i].ID
		items[i].Revisions = []models.MetricImplementationRevision{}
	}
	if len(ids) == 0 {
		return items, nil
	}
	var revisions []models.MetricImplementationRevision
	if err := r.db.Where("tenant_id = ? AND implementation_id IN ?", tenantID, ids).Order("revision_no DESC").Find(&revisions).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	positions := map[int64]int{}
	for i := range items {
		positions[items[i].ID] = i
	}
	for _, rev := range revisions {
		i := positions[rev.ImplementationID]
		items[i].Revisions = append(items[i].Revisions, rev)
	}
	return items, nil
}
func (r *MetricImplementationRepository) GetByID(id, tenantID int64) (*models.MetricImplementation, error) {
	var item models.MetricImplementation
	if err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	item.Revisions = []models.MetricImplementationRevision{}
	err := r.db.Where("implementation_id = ? AND tenant_id = ?", id, tenantID).Order("revision_no DESC").Find(&item.Revisions).Error
	return &item, commonrepo.WrapDBError(err)
}
func (r *MetricImplementationRepository) Create(item *models.MetricImplementation) error {
	return commonrepo.WrapDBError(r.db.Create(item).Error)
}
