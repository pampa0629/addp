package repository

import (
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
)

type IssueRepository struct {
	db *gorm.DB
}

func NewIssueRepository(db *gorm.DB) *IssueRepository {
	return &IssueRepository{db: db}
}

func (r *IssueRepository) List(tenantID int64, status string, engineID int64) ([]models.Issue, error) {
	var items []models.Issue
	q := r.db.Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if engineID > 0 {
		q = q.Where("engine_id = ?", engineID)
	}
	err := q.Order("id desc").Find(&items).Error
	return items, err
}

func (r *IssueRepository) ListByExecution(executionID string) ([]models.Issue, error) {
	var items []models.Issue
	err := r.db.Where("execution_id = ?", executionID).Find(&items).Error
	return items, err
}

func (r *IssueRepository) Get(id, tenantID int64) (*models.Issue, error) {
	var item models.Issue
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *IssueRepository) Create(item *models.Issue) error {
	return r.db.Create(item).Error
}

func (r *IssueRepository) UpdateStatus(id, tenantID int64, status string) error {
	return r.db.Model(&models.Issue{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("status", status).Error
}

func (r *IssueRepository) BatchCreate(items []models.Issue) error {
	if len(items) == 0 {
		return nil
	}
	return r.db.Create(&items).Error
}
