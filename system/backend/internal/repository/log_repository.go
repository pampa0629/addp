package repository

import (
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

type LogRepository struct {
	db *gorm.DB
}

func NewLogRepository(db *gorm.DB) *LogRepository {
	return &LogRepository{db: db}
}

func (r *LogRepository) Create(log *models.AuditLog) error {
	return r.db.Create(log).Error
}

func (r *LogRepository) List(offset, limit int, userID *uint) ([]models.AuditLog, error) {
	var logs []models.AuditLog
	query := r.db.Order("created_at DESC")

	if userID != nil {
		query = query.Where("user_id = ?", *userID)
	}

	err := query.Offset(offset).Limit(limit).Find(&logs).Error
	return logs, err
}

func (r *LogRepository) GetByID(id uint) (*models.AuditLog, error) {
	var log models.AuditLog
	err := r.db.First(&log, id).Error
	if err != nil {
		return nil, err
	}
	return &log, nil
}