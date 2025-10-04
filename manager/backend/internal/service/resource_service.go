package service

import (
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type ResourceService struct {
	repo *repository.ResourceRepository
}

func NewResourceService(repo *repository.ResourceRepository) *ResourceService {
	return &ResourceService{repo: repo}
}

// List 获取资源列表
func (s *ResourceService) List(page, pageSize int, resourceType string) ([]models.Resource, int64, error) {
	return s.repo.List(page, pageSize, resourceType)
}

// GetByID 根据ID获取资源
func (s *ResourceService) GetByID(id uint) (*models.Resource, error) {
	return s.repo.GetByID(id)
}
