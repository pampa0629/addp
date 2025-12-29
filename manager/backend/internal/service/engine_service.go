package service

import (
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type EngineService struct {
	repo *repository.EngineRepository
}

func NewEngineService(repo *repository.EngineRepository) *EngineService {
	return &EngineService{repo: repo}
}

// List 获取引擎列表
func (s *EngineService) List(page, pageSize int, engineType string) ([]models.Engine, int64, error) {
	return s.repo.List(page, pageSize, engineType)
}

// GetByID 根据ID获取引擎
func (s *EngineService) GetByID(id uint) (*models.Engine, error) {
	return s.repo.GetByID(id)
}
