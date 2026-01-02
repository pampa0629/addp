package service

import (
	"errors"

	"github.com/addp/common/models"
)

var (
	ErrPermissionDenied = errors.New("没有权限执行此操作")
	ErrUserNotFound     = errors.New("用户不存在")
)

// AuthService 权限检查服务
// 消除 Service 层 20+ 处重复的权限检查逻辑
// 注意：此服务完全无状态，不依赖Repository，用户对象由调用方传入
type AuthService struct{}

// NewAuthService 创建权限服务
func NewAuthService() *AuthService {
	return &AuthService{}
}

// IsSuperAdmin 判断是否为超级管理员
func (s *AuthService) IsSuperAdmin(user models.UserInterface) bool {
	return user != nil && user.GetUserType() == "super_admin"
}

// IsTenantAdmin 判断是否为租户管理员
func (s *AuthService) IsTenantAdmin(user models.UserInterface) bool {
	return user != nil && user.GetUserType() == "tenant_admin"
}

// IsNormalUser 判断是否为普通用户
func (s *AuthService) IsNormalUser(user models.UserInterface) bool {
	return user != nil && user.GetUserType() == "user"
}

// CheckTenantAccess 检查租户访问权限
// 验证用户是否有权限访问指定租户的资源
func (s *AuthService) CheckTenantAccess(user models.UserInterface, resourceTenantID *uint) error {
	if user == nil {
		return ErrUserNotFound
	}

	// 超级管理员可以访问所有租户资源
	if s.IsSuperAdmin(user) {
		return nil
	}

	// 检查租户 ID 是否匹配
	userTenantID := user.GetTenantID()
	if userTenantID == nil || resourceTenantID == nil {
		return errors.New("租户信息不完整")
	}

	if *userTenantID != *resourceTenantID {
		return errors.New("没有权限访问该租户的资源")
	}

	return nil
}

// CheckResourceOwnership 检查资源所有权
// 验证用户是否为资源的创建者或管理员
func (s *AuthService) CheckResourceOwnership(user models.UserInterface, creatorID *uint) error {
	if user == nil {
		return ErrUserNotFound
	}

	// 超级管理员和租户管理员可以管理所有资源
	if s.IsSuperAdmin(user) || s.IsTenantAdmin(user) {
		return nil
	}

	// 普通用户只能管理自己创建的资源
	if creatorID == nil || user.GetID() != *creatorID {
		return errors.New("只能管理自己创建的资源")
	}

	return nil
}

// CheckCreatePermission 检查创建权限
func (s *AuthService) CheckCreatePermission(user models.UserInterface, resourceType string) error {
	if user == nil {
		return ErrUserNotFound
	}

	// 超级管理员和租户管理员可以创建大部分资源
	if s.IsSuperAdmin(user) || s.IsTenantAdmin(user) {
		return nil
	}

	// 普通用户创建权限受限
	allowedResources := map[string]bool{
		"profile": true, // 可以创建/编辑自己的个人信息
	}

	if !allowedResources[resourceType] {
		return errors.New("没有权限创建此类资源")
	}

	return nil
}

// CheckUpdatePermission 检查更新权限
func (s *AuthService) CheckUpdatePermission(user models.UserInterface, resourceCreatorID *uint, resourceTenantID *uint) error {
	if user == nil {
		return ErrUserNotFound
	}

	// 超级管理员可以更新所有资源
	if s.IsSuperAdmin(user) {
		return nil
	}

	// 租户管理员可以更新同租户的资源
	if s.IsTenantAdmin(user) {
		return s.CheckTenantAccess(user, resourceTenantID)
	}

	// 普通用户只能更新自己创建的资源
	return s.CheckResourceOwnership(user, resourceCreatorID)
}

// CheckDeletePermission 检查删除权限
func (s *AuthService) CheckDeletePermission(user models.UserInterface, resourceCreatorID *uint, resourceTenantID *uint, isBuiltin bool) error {
	if user == nil {
		return ErrUserNotFound
	}

	// 内置资源不可删除
	if isBuiltin {
		return errors.New("内置资源不可删除")
	}

	// 超级管理员可以删除所有资源
	if s.IsSuperAdmin(user) {
		return nil
	}

	// 租户管理员可以删除同租户的资源
	if s.IsTenantAdmin(user) {
		return s.CheckTenantAccess(user, resourceTenantID)
	}

	// 普通用户只能删除自己创建的资源
	return s.CheckResourceOwnership(user, resourceCreatorID)
}
