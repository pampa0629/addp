package service

import (
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
)

type LogService struct {
	repo *repository.LogRepository
}

func NewLogService(repo *repository.LogRepository) *LogService {
	return &LogService{repo: repo}
}

func (s *LogService) Create(log *models.AuditLog) error {
	return s.repo.Create(log)
}

func (s *LogService) List(page, pageSize int, userID *uint) ([]models.AuditLog, error) {
	offset := (page - 1) * pageSize
	return s.repo.List(offset, pageSize, userID)
}

func (s *LogService) GetByID(id uint) (*models.AuditLog, error) {
	return s.repo.GetByID(id)
}