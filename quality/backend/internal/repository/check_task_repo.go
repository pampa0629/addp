package repository

import (
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
)

type CheckTaskRepository struct {
	db *gorm.DB
}

func NewCheckTaskRepository(db *gorm.DB) *CheckTaskRepository {
	return &CheckTaskRepository{db: db}
}

func (r *CheckTaskRepository) List(tenantID int64) ([]models.CheckTask, error) {
	var items []models.CheckTask
	err := r.db.Where("tenant_id = ?", tenantID).Order("id desc").Find(&items).Error
	return items, err
}

func (r *CheckTaskRepository) Get(id, tenantID int64) (*models.CheckTask, error) {
	var item models.CheckTask
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *CheckTaskRepository) Create(item *models.CheckTask) error {
	return r.db.Create(item).Error
}

func (r *CheckTaskRepository) Update(item *models.CheckTask) error {
	return r.db.Save(item).Error
}

func (r *CheckTaskRepository) Delete(id, tenantID int64) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.CheckTask{}).Error
}
