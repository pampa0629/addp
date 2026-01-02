package service

import (
	"errors"

	commonservice "github.com/addp/common/service"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/pkg/utils"
	"gorm.io/gorm"
)

type UserService struct {
	repo    repository.UserRepositoryInterface
	authSvc *commonservice.AuthService
}

func NewUserService(repo repository.UserRepositoryInterface) *UserService {
	return &UserService{
		repo:    repo,
		authSvc: commonservice.NewAuthService(),
	}
}

func (s *UserService) Create(req *models.UserCreateRequest, creatorID uint) (*models.User, error) {
	// 检查用户名是否已存在
	_, err := s.repo.GetByUsername(req.Username)
	if err == nil {
		return nil, errors.New("用户名已存在")
	}

	// 获取创建者信息以验证权限
	creator, err := s.repo.GetByID(creatorID)
	if err != nil {
		return nil, errors.New("创建者不存在")
	}

	// 验证创建权限 (多租户逻辑)
	if err := s.validateCreatePermission(creator, req.UserType); err != nil {
		return nil, err
	}

	// Hash 密码
	passwordHash, err := utils.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	// 设置默认用户类型
	userType := req.UserType
	if userType == "" {
		userType = models.UserTypeUser
	}

	user := &models.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: passwordHash,
		FullName:     req.FullName,
		IsActive:     true,
		UserType:     userType,
		TenantID:     creator.TenantID, // 继承创建者的租户ID
		IsSuperuser:  userType == models.UserTypeSuperAdmin,
	}

	if err := s.repo.Create(user); err != nil {
		return nil, err
	}

	return user, nil
}

// validateCreatePermission 验证创建用户的权限 (多租户版本)
func (s *UserService) validateCreatePermission(creator *models.User, targetUserType models.UserType) error {
	// 超级管理员不能创建普通用户，只能通过创建租户来创建租户管理员
	if creator.UserType == models.UserTypeSuperAdmin {
		return errors.New("超级管理员不能直接创建用户，请通过创建租户来添加用户")
	}

	// 租户管理员只能创建普通用户
	if creator.UserType == models.UserTypeTenantAdmin {
		if targetUserType != models.UserTypeUser && targetUserType != "" {
			return errors.New("租户管理员只能创建普通用户")
		}
		return nil
	}

	// 普通用户不能创建用户
	return errors.New("没有权限创建用户")
}

func (s *UserService) GetByID(id uint, currentUserID uint) (*models.User, error) {
	user, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	// 获取当前用户
	currentUser, err := s.repo.GetByID(currentUserID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}

	// 超级管理员可以查看所有用户
	if s.authSvc.IsSuperAdmin(currentUser) {
		return user, nil
	}

	// 租户管理员只能查看同租户的用户
	if s.authSvc.IsTenantAdmin(currentUser) {
		if err := s.authSvc.CheckTenantAccess(currentUser, user.TenantID); err != nil {
			return nil, errors.New("没有权限查看该用户")
		}
		return user, nil
	}

	// 普通用户只能查看自己
	if user.ID != currentUserID {
		return nil, errors.New("没有权限查看该用户")
	}

	return user, nil
}

func (s *UserService) List(page, pageSize int, currentUserID uint) ([]models.User, error) {
	offset := (page - 1) * pageSize

	// 获取当前用户
	currentUser, err := s.repo.GetByID(currentUserID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}

	// 超级管理员不查看普通用户列表，应该查看租户列表
	if s.authSvc.IsSuperAdmin(currentUser) {
		return []models.User{}, nil
	}

	// 租户管理员只能查看同租户的用户
	if s.authSvc.IsTenantAdmin(currentUser) {
		users, _, err := s.repo.ListByTenant(*currentUser.TenantID, offset, pageSize)
		return users, err
	}

	// 普通用户只能查看自己
	return []models.User{*currentUser}, nil
}

func (s *UserService) Update(id uint, req *models.UserUpdateRequest, currentUserID uint) (*models.User, error) {
	user, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	// 获取当前用户并验证权限
	currentUser, err := s.repo.GetByID(currentUserID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}

	// 验证更新权限
	if err := s.validateUpdatePermission(currentUser, user, req); err != nil {
		return nil, err
	}

	// 更新基本字段
	if req.Email != nil {
		user.Email = *req.Email
	}
	if req.FullName != nil {
		user.FullName = *req.FullName
	}
	if req.Password != nil {
		passwordHash, err := utils.HashPassword(*req.Password)
		if err != nil {
			return nil, err
		}
		user.PasswordHash = passwordHash
	}

	// 只有管理员可以修改激活状态和用户类型（且不能修改自己）
	if (s.authSvc.IsSuperAdmin(currentUser) || s.authSvc.IsTenantAdmin(currentUser)) && currentUser.ID != user.ID {
		if req.IsActive != nil {
			user.IsActive = *req.IsActive
		}
		if req.UserType != nil {
			// 验证用户类型修改权限
			if err := s.validateCreatePermission(currentUser, *req.UserType); err != nil {
				return nil, err
			}
			user.UserType = *req.UserType
			user.IsSuperuser = *req.UserType == models.UserTypeSuperAdmin
		}
	}

	if err := s.repo.Update(user); err != nil {
		return nil, err
	}

	return user, nil
}

// validateUpdatePermission 验证更新权限
func (s *UserService) validateUpdatePermission(currentUser, targetUser *models.User, req *models.UserUpdateRequest) error {
	// 超级管理员可以修改所有用户
	if s.authSvc.IsSuperAdmin(currentUser) {
		return nil
	}

	// 租户管理员只能修改同租户的用户
	if s.authSvc.IsTenantAdmin(currentUser) {
		if err := s.authSvc.CheckTenantAccess(currentUser, targetUser.TenantID); err != nil {
			return errors.New("只能修改同租户的用户")
		}
		// 不能修改其他租户管理员
		if targetUser.UserType == models.UserTypeTenantAdmin && targetUser.ID != currentUser.ID {
			return errors.New("不能修改其他租户管理员")
		}
		return nil
	}

	// 普通用户只能修改自己的信息
	if currentUser.ID != targetUser.ID {
		return errors.New("只能修改自己的信息")
	}

	// 普通用户不能修改激活状态和用户类型
	if req.IsActive != nil || req.UserType != nil {
		return errors.New("没有权限修改用户状态和类型")
	}

	return nil
}

func (s *UserService) Delete(id uint, currentUserID uint) error {
	// 获取要删除的用户
	targetUser, err := s.repo.GetByID(id)
	if err != nil {
		return errors.New("用户不存在")
	}

	// 不能删除SuperAdmin用户
	if targetUser.Username == "SuperAdmin" {
		return errors.New("不能删除SuperAdmin用户")
	}

	// 使用 AuthService 获取当前用户
	currentUser, err := s.repo.GetByID(currentUserID)
	if err != nil {
		return err
	}

	// 超级管理员可以删除所有用户（除了SuperAdmin）
	if s.authSvc.IsSuperAdmin(currentUser) {
		return s.repo.Delete(id)
	}

	// 租户管理员只能删除同租户的普通用户
	if s.authSvc.IsTenantAdmin(currentUser) {
		if err := s.authSvc.CheckTenantAccess(currentUser, targetUser.TenantID); err != nil {
			return errors.New("只能删除同租户的用户")
		}
		// 不能删除租户管理员
		if targetUser.UserType == models.UserTypeTenantAdmin {
			return errors.New("不能删除租户管理员")
		}
		return s.repo.Delete(id)
	}

	// 普通用户不能删除用户
	return errors.New("没有权限删除用户")
}

func (s *UserService) Register(req *models.UserCreateRequest) (*models.User, error) {
	// 检查用户名是否已存在
	_, err := s.repo.GetByUsername(req.Username)
	if err == nil {
		return nil, errors.New("用户名已存在")
	}

	// Hash 密码
	passwordHash, err := utils.HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	user := &models.User{
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: passwordHash,
		FullName:     req.FullName,
		IsActive:     true,
		UserType:     models.UserTypeUser,
		TenantID:     nil, // 注册用户没有租户
		IsSuperuser:  false,
	}

	if err := s.repo.Create(user); err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UserService) Authenticate(username, password string) (*models.User, error) {
	user, err := s.repo.GetByUsername(username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("用户名或密码错误")
		}
		return nil, err
	}

	if !utils.CheckPassword(password, user.PasswordHash) {
		return nil, errors.New("用户名或密码错误")
	}

	if !user.IsActive {
		return nil, errors.New("用户已被禁用")
	}

	return user, nil
}

// ChangePassword 修改密码（需要验证旧密码）
func (s *UserService) ChangePassword(userID uint, req *models.ChangePasswordRequest, currentUserID uint) error {
	// 只能修改自己的密码
	if userID != currentUserID {
		return errors.New("只能修改自己的密码")
	}

	user, err := s.repo.GetByID(userID)
	if err != nil {
		return errors.New("用户不存在")
	}

	// 验证旧密码
	if !utils.CheckPassword(req.OldPassword, user.PasswordHash) {
		return errors.New("旧密码错误")
	}

	// Hash 新密码
	passwordHash, err := utils.HashPassword(req.NewPassword)
	if err != nil {
		return err
	}

	user.PasswordHash = passwordHash
	return s.repo.Update(user)
}
