package service

import (
	"errors"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
	"github.com/addp/system/pkg/utils"
	"gorm.io/gorm"
)

type UserService struct {
	repo *repository.UserRepository
}

func NewUserService(repo *repository.UserRepository) *UserService {
	return &UserService{repo: repo}
}

func (s *UserService) Create(req *models.UserCreateRequest) (*models.User, error) {
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
		IsSuperuser:  req.IsSuperuser,
	}

	if err := s.repo.Create(user); err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UserService) GetByID(id uint) (*models.User, error) {
	return s.repo.GetByID(id)
}

func (s *UserService) List(page, pageSize int) ([]models.User, error) {
	offset := (page - 1) * pageSize
	return s.repo.List(offset, pageSize)
}

func (s *UserService) Update(id uint, req *models.UserUpdateRequest) (*models.User, error) {
	user, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

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
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}
	if req.IsSuperuser != nil {
		user.IsSuperuser = *req.IsSuperuser
	}

	if err := s.repo.Update(user); err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UserService) Delete(id uint) error {
	return s.repo.Delete(id)
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