package service

import (
	"errors"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/pkg/utils"
	"gorm.io/gorm"
)

type TenantService struct {
	tenantRepo *repository.TenantRepository
	userRepo   *repository.UserRepository
	db         *gorm.DB
}

func NewTenantService(tenantRepo *repository.TenantRepository, userRepo *repository.UserRepository, db *gorm.DB) *TenantService {
	return &TenantService{
		tenantRepo: tenantRepo,
		userRepo:   userRepo,
		db:         db,
	}
}

func (s *TenantService) Create(req *models.TenantCreateRequest, currentUserID uint) (*models.Tenant, error) {
	// 验证当前用户是否为超级管理员
	currentUser, err := s.userRepo.GetByID(currentUserID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}

	if currentUser.UserType != models.UserTypeSuperAdmin {
		return nil, errors.New("只有超级管理员可以创建租户")
	}

	// 检查租户名是否已存在
	_, err = s.tenantRepo.GetByName(req.Name)
	if err == nil {
		return nil, errors.New("租户名已存在")
	}

	// 检查管理员用户名是否已存在
	_, err = s.userRepo.GetByUsername(req.AdminUsername)
	if err == nil {
		return nil, errors.New("管理员用户名已存在")
	}

	// 使用事务创建租户和租户管理员
	var tenant *models.Tenant
	err = s.db.Transaction(func(tx *gorm.DB) error {
		// 创建租户
		tenant = &models.Tenant{
			Name:        req.Name,
			Description: req.Description,
			IsActive:    true,
		}

		if err := tx.Create(tenant).Error; err != nil {
			return err
		}

		// 创建租户管理员
		passwordHash, err := utils.HashPassword(req.AdminPassword)
		if err != nil {
			return err
		}

		admin := &models.User{
			Username:     req.AdminUsername,
			Email:        req.AdminEmail,
			PasswordHash: passwordHash,
			FullName:     req.AdminFullName,
			IsActive:     true,
			UserType:     models.UserTypeTenantAdmin,
			TenantID:     &tenant.ID,
			IsSuperuser:  false,
		}

		if err := tx.Create(admin).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return tenant, nil
}

func (s *TenantService) GetByID(id uint, currentUserID uint) (*models.Tenant, error) {
	// 验证权限
	currentUser, err := s.userRepo.GetByID(currentUserID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}

	if currentUser.UserType != models.UserTypeSuperAdmin {
		return nil, errors.New("只有超级管理员可以查看租户")
	}

	return s.tenantRepo.GetByID(id)
}

func (s *TenantService) List(page, pageSize int, currentUserID uint) ([]models.Tenant, error) {
	// 验证权限
	currentUser, err := s.userRepo.GetByID(currentUserID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}

	if currentUser.UserType != models.UserTypeSuperAdmin {
		return nil, errors.New("只有超级管理员可以查看租户列表")
	}

	offset := (page - 1) * pageSize
	return s.tenantRepo.List(offset, pageSize)
}

func (s *TenantService) Update(id uint, req *models.TenantUpdateRequest, currentUserID uint) (*models.Tenant, error) {
	// 验证权限
	currentUser, err := s.userRepo.GetByID(currentUserID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}

	if currentUser.UserType != models.UserTypeSuperAdmin {
		return nil, errors.New("只有超级管理员可以修改租户")
	}

	tenant, err := s.tenantRepo.GetByID(id)
	if err != nil {
		return nil, errors.New("租户不存在")
	}

	if req.Name != nil {
		// 检查新名称是否与其他租户冲突
		existingTenant, err := s.tenantRepo.GetByName(*req.Name)
		if err == nil && existingTenant.ID != id {
			return nil, errors.New("租户名已存在")
		}
		tenant.Name = *req.Name
	}

	if req.Description != nil {
		tenant.Description = *req.Description
	}

	if req.IsActive != nil {
		tenant.IsActive = *req.IsActive
	}

	if err := s.tenantRepo.Update(tenant); err != nil {
		return nil, err
	}

	return tenant, nil
}

func (s *TenantService) Delete(id uint, currentUserID uint) error {
	// 验证权限
	currentUser, err := s.userRepo.GetByID(currentUserID)
	if err != nil {
		return errors.New("当前用户不存在")
	}

	if currentUser.UserType != models.UserTypeSuperAdmin {
		return errors.New("只有超级管理员可以删除租户")
	}

	// 检查租户是否存在
	_, err = s.tenantRepo.GetByID(id)
	if err != nil {
		return errors.New("租户不存在")
	}

	// 使用事务删除租户及其所有用户
	return s.db.Transaction(func(tx *gorm.DB) error {
		// 删除租户下的所有用户
		if err := tx.Where("tenant_id = ?", id).Delete(&models.User{}).Error; err != nil {
			return err
		}

		// 删除租户
		if err := tx.Delete(&models.Tenant{}, id).Error; err != nil {
			return err
		}

		return nil
	})
}
