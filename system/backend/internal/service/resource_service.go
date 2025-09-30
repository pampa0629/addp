package service

import (
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
)

type ResourceService struct {
	repo *repository.ResourceRepository
}

func NewResourceService(repo *repository.ResourceRepository) *ResourceService {
	return &ResourceService{repo: repo}
}

func (s *ResourceService) Create(req *models.ResourceCreateRequest, createdBy uint) (*models.Resource, error) {
	resource := &models.Resource{
		Name:           req.Name,
		ResourceType:   req.ResourceType,
		ConnectionInfo: req.ConnectionInfo,
		Description:    req.Description,
		CreatedBy:      &createdBy,
		IsActive:       true,
	}

	if err := s.repo.Create(resource); err != nil {
		return nil, err
	}

	return resource, nil
}

func (s *ResourceService) GetByID(id uint) (*models.Resource, error) {
	return s.repo.GetByID(id)
}

func (s *ResourceService) List(page, pageSize int, resourceType string) ([]models.Resource, error) {
	offset := (page - 1) * pageSize
	return s.repo.List(offset, pageSize, resourceType)
}

func (s *ResourceService) Update(id uint, req *models.ResourceUpdateRequest) (*models.Resource, error) {
	resource, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		resource.Name = *req.Name
	}
	if req.ConnectionInfo != nil {
		resource.ConnectionInfo = *req.ConnectionInfo
	}
	if req.Description != nil {
		resource.Description = *req.Description
	}
	if req.IsActive != nil {
		resource.IsActive = *req.IsActive
	}

	if err := s.repo.Update(resource); err != nil {
		return nil, err
	}

	return resource, nil
}

func (s *ResourceService) Delete(id uint) error {
	return s.repo.Delete(id)
}