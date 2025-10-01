package service

import (
	"errors"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
)

type ResourceService struct {
	repo     *repository.ResourceRepository
	userRepo *repository.UserRepository
}

func NewResourceService(repo *repository.ResourceRepository, userRepo *repository.UserRepository) *ResourceService {
	return &ResourceService{
		repo:     repo,
		userRepo: userRepo,
	}
}

func (s *ResourceService) Create(req *models.ResourceCreateRequest, createdBy uint) (*models.Resource, error) {
	// 获取创建者信息以确定租户
	user, err := s.userRepo.GetByID(createdBy)
	if err != nil {
		return nil, errors.New("用户不存在")
	}

	resource := &models.Resource{
		Name:           req.Name,
		ResourceType:   req.ResourceType,
		ConnectionInfo: req.ConnectionInfo,
		Description:    req.Description,
		CreatedBy:      &createdBy,
		TenantID:       user.TenantID, // 继承用户的租户ID
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

func (s *ResourceService) List(page, pageSize int, resourceType string, currentUserID uint) ([]models.Resource, error) {
	offset := (page - 1) * pageSize

	// 获取当前用户信息
	currentUser, err := s.userRepo.GetByID(currentUserID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}

	// SuperAdmin可以查看所有资源
	if currentUser.UserType == models.UserTypeSuperAdmin {
		return s.repo.List(offset, pageSize, resourceType)
	}

	// 租户管理员和普通用户只能查看本租户的资源
	if currentUser.TenantID == nil {
		return []models.Resource{}, nil
	}
	return s.repo.ListByTenant(*currentUser.TenantID, offset, pageSize, resourceType)
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