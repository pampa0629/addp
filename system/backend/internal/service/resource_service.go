package service

import (
	"errors"
	"fmt"

	commonutils "github.com/addp/common/utils"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"gorm.io/gorm"
)

var (
	ErrResourceNotFound  = errors.New("资源不存在")
	ErrResourceForbidden = errors.New("没有权限访问该资源")
)

type ResourceService struct {
	repo          *repository.ResourceRepository
	userRepo      *repository.UserRepository
	encryptionKey []byte
}

func NewResourceService(repo *repository.ResourceRepository, userRepo *repository.UserRepository, encryptionKey []byte) *ResourceService {
	return &ResourceService{
		repo:          repo,
		userRepo:      userRepo,
		encryptionKey: encryptionKey,
	}
}

func (s *ResourceService) Create(req *models.ResourceCreateRequest, createdBy uint) (*models.Resource, error) {
	// 获取创建者信息以确定租户
	user, err := s.userRepo.GetByID(createdBy)
	if err != nil {
		return nil, errors.New("用户不存在")
	}

	if err := s.ensureResourceManagementPermission(user); err != nil {
		return nil, err
	}

	// 加密敏感字段
	encryptedConnInfo, err := s.encryptSensitiveFields(req.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("加密连接信息失败: %w", err)
	}

	resource := &models.Resource{
		Name:           req.Name,
		ResourceType:   req.ResourceType,
		ConnectionInfo: encryptedConnInfo,
		Description:    req.Description,
		CreatedBy:      &createdBy,
		TenantID:       user.TenantID, // 继承用户的租户ID
		IsActive:       true,
	}

	if err := s.repo.Create(resource); err != nil {
		return nil, err
	}

	return s.sanitizeResource(resource), nil
}

func (s *ResourceService) GetByID(id uint, currentUserID uint) (*models.Resource, error) {
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return nil, err
	}

	resource, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(resource, currentUser); err != nil {
		return nil, err
	}

	return s.sanitizeResource(resource), nil
}

func (s *ResourceService) List(page, pageSize int, resourceType string, currentUserID uint) ([]models.Resource, error) {
	offset := (page - 1) * pageSize

	// 获取当前用户信息
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return nil, err
	}

	var resources []models.Resource

	// SuperAdmin可以查看所有资源
	if currentUser.UserType == models.UserTypeSuperAdmin {
		resources, err = s.repo.List(offset, pageSize, resourceType)
	} else {
		if currentUser.TenantID == nil {
			return nil, errors.New("当前用户未关联租户，无法访问资源")
		}
		resources, err = s.repo.ListByTenant(*currentUser.TenantID, offset, pageSize, resourceType)
	}

	if err != nil {
		return nil, err
	}

	// 脱敏敏感字段
	sanitized := make([]models.Resource, 0, len(resources))
	for i := range resources {
		sanitized = append(sanitized, *s.sanitizeResource(&resources[i]))
	}

	return sanitized, nil
}

func (s *ResourceService) Update(id uint, req *models.ResourceUpdateRequest, currentUserID uint) (*models.Resource, error) {
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return nil, err
	}

	resource, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(resource, currentUser); err != nil {
		return nil, err
	}

	if err := s.ensureResourceManagementPermission(currentUser); err != nil {
		return nil, err
	}

	if req.Name != nil {
		resource.Name = *req.Name
	}
	if req.ConnectionInfo != nil {
		// 加密敏感字段
		encryptedConnInfo, err := s.encryptSensitiveFields(*req.ConnectionInfo)
		if err != nil {
			return nil, fmt.Errorf("加密连接信息失败: %w", err)
		}
		resource.ConnectionInfo = encryptedConnInfo
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

	return s.sanitizeResource(resource), nil
}

func (s *ResourceService) Delete(id uint, currentUserID uint) error {
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return err
	}

	resource, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrResourceNotFound
		}
		return err
	}

	if err := s.authorizeResourceAccess(resource, currentUser); err != nil {
		return err
	}

	if err := s.ensureResourceManagementPermission(currentUser); err != nil {
		return err
	}

	return s.repo.Delete(id)
}

// ListInternal 内部服务调用的资源列表查询（不做租户权限检查）
func (s *ResourceService) ListInternal(resourceType string, tenantID uint) ([]models.Resource, error) {
	var resources []models.Resource
	var err error

	if tenantID > 0 {
		// 按租户过滤
		resources, err = s.repo.ListByTenant(tenantID, 0, 9999, resourceType)
	} else {
		// 返回所有资源
		resources, err = s.repo.List(0, 9999, resourceType)
	}

	if err != nil {
		return nil, err
	}

	// 解密所有资源的敏感字段
	for i := range resources {
		decryptedConnInfo, err := s.decryptSensitiveFields(resources[i].ConnectionInfo)
		if err != nil {
			return nil, fmt.Errorf("解密资源 %d 连接信息失败: %w", resources[i].ID, err)
		}
		resources[i].ConnectionInfo = decryptedConnInfo
	}

	return resources, nil
}

// GetByIDInternal 内部服务直接访问资源详情（返回解密信息）
func (s *ResourceService) GetByIDInternal(id uint) (*models.Resource, error) {
	resource, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	decryptedConnInfo, err := s.decryptSensitiveFields(resource.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("解密连接信息失败: %w", err)
	}

	resourceCopy := *resource
	resourceCopy.ConnectionInfo = decryptedConnInfo
	return &resourceCopy, nil
}

// GetForConnection 返回带解密信息的资源，用于当前用户执行连接测试
func (s *ResourceService) GetForConnection(id uint, currentUserID uint) (*models.Resource, error) {
	currentUser, err := s.getCurrentUser(currentUserID)
	if err != nil {
		return nil, err
	}

	resource, err := s.repo.GetByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}

	if err := s.authorizeResourceAccess(resource, currentUser); err != nil {
		return nil, err
	}

	if err := s.ensureResourceManagementPermission(currentUser); err != nil {
		return nil, err
	}

	decryptedConnInfo, err := s.decryptSensitiveFields(resource.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("解密连接信息失败: %w", err)
	}

	resourceCopy := *resource
	resourceCopy.ConnectionInfo = decryptedConnInfo
	return &resourceCopy, nil
}

// encryptSensitiveFields 加密连接信息中的敏感字段
func (s *ResourceService) encryptSensitiveFields(connInfo models.ConnectionInfo) (models.ConnectionInfo, error) {
	encrypted := make(models.ConnectionInfo)
	for k, v := range connInfo {
		encrypted[k] = v
	}

	// 定义需要加密的敏感字段
	sensitiveFields := []string{"password", "access_key", "secret_key", "token", "api_key"}

	for _, field := range sensitiveFields {
		if val, exists := connInfo[field]; exists {
			if strVal, ok := val.(string); ok && strVal != "" {
				encryptedVal, err := commonutils.Encrypt(strVal, s.encryptionKey)
				if err != nil {
					return nil, fmt.Errorf("加密字段 %s 失败: %w", field, err)
				}
				encrypted[field] = encryptedVal
			}
		}
	}

	return encrypted, nil
}

// decryptSensitiveFields 解密连接信息中的敏感字段
func (s *ResourceService) decryptSensitiveFields(connInfo models.ConnectionInfo) (models.ConnectionInfo, error) {
	decrypted := make(models.ConnectionInfo)
	for k, v := range connInfo {
		decrypted[k] = v
	}

	// 定义需要解密的敏感字段
	sensitiveFields := []string{"password", "access_key", "secret_key", "token", "api_key"}

	for _, field := range sensitiveFields {
		if val, exists := connInfo[field]; exists {
			if strVal, ok := val.(string); ok && strVal != "" {
				decryptedVal, err := commonutils.Decrypt(strVal, s.encryptionKey)
				if err != nil {
					// 如果解密失败，可能是未加密的旧数据，保持原值
					// 在生产环境中应该记录日志
					decrypted[field] = strVal
					continue
				}
				decrypted[field] = decryptedVal
			}
		}
	}

	return decrypted, nil
}

func (s *ResourceService) maskSensitiveFields(connInfo models.ConnectionInfo) models.ConnectionInfo {
	if connInfo == nil {
		return nil
	}

	masked := make(models.ConnectionInfo)
	for k, v := range connInfo {
		if s.isSensitiveField(k) && v != nil {
			masked[k] = "******"
			continue
		}
		masked[k] = v
	}
	return masked
}

func (s *ResourceService) sanitizeResource(resource *models.Resource) *models.Resource {
	if resource == nil {
		return nil
	}

	copyResource := *resource
	copyResource.ConnectionInfo = s.maskSensitiveFields(resource.ConnectionInfo)
	return &copyResource
}

func (s *ResourceService) getCurrentUser(userID uint) (*models.User, error) {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}
	return user, nil
}

func (s *ResourceService) authorizeResourceAccess(resource *models.Resource, user *models.User) error {
	if user.UserType == models.UserTypeSuperAdmin {
		return nil
	}

	if user.TenantID == nil || resource.TenantID == nil {
		return ErrResourceForbidden
	}

	if *user.TenantID != *resource.TenantID {
		return ErrResourceForbidden
	}

	return nil
}

func (s *ResourceService) isSensitiveField(field string) bool {
	switch field {
	case "password", "access_key", "secret_key", "token", "api_key":
		return true
	default:
		return false
	}
}

func (s *ResourceService) ensureResourceManagementPermission(user *models.User) error {
	if user.UserType == models.UserTypeSuperAdmin || user.UserType == models.UserTypeTenantAdmin {
		return nil
	}
	return ErrResourceForbidden
}
