package repository

import (
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
)

type RuleApplicationRepository struct {
	db *gorm.DB
}

func NewRuleApplicationRepository(db *gorm.DB) *RuleApplicationRepository {
	return &RuleApplicationRepository{db: db}
}

func (r *RuleApplicationRepository) List(tenantID int64, engineID int64, schemaName, tableName string) ([]models.RuleApplication, error) {
	var items []models.RuleApplication
	q := r.db.Where("tenant_id = ?", tenantID)
	if engineID > 0 {
		q = q.Where("engine_id = ?", engineID)
	}
	if schemaName != "" {
		q = q.Where("schema_name = ?", schemaName)
	}
	if tableName != "" {
		q = q.Where("table_name = ?", tableName)
	}
	err := q.Order("id desc").Find(&items).Error
	return items, err
}

func (r *RuleApplicationRepository) Get(id, tenantID int64) (*models.RuleApplication, error) {
	var item models.RuleApplication
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *RuleApplicationRepository) Create(item *models.RuleApplication) error {
	return r.db.Create(item).Error
}

func (r *RuleApplicationRepository) Update(item *models.RuleApplication) error {
	return r.db.Save(item).Error
}

func (r *RuleApplicationRepository) Delete(id, tenantID int64) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.RuleApplication{}).Error
}

func (r *RuleApplicationRepository) ListByTask(tenantID, engineID int64, schemaName, tableName string) ([]models.RuleApplication, error) {
	var items []models.RuleApplication
	q := r.db.Where("tenant_id = ? AND enabled = true AND engine_id = ?", tenantID, engineID)
	if schemaName != "" {
		q = q.Where("schema_name = ?", schemaName)
	}
	if tableName != "" {
		q = q.Where("table_name = ?", tableName)
	}
	err := q.Find(&items).Error
	return items, err
}
