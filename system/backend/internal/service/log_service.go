package service

import (
	"errors"

	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
)

type LogService struct {
	repo     *repository.LogRepository
	userRepo *repository.UserRepository
}

func NewLogService(repo *repository.LogRepository, userRepo *repository.UserRepository) *LogService {
	return &LogService{
		repo:     repo,
		userRepo: userRepo,
	}
}

func (s *LogService) Create(log *models.AuditLog) error {
	return s.repo.Create(log)
}

func (s *LogService) List(page, pageSize int, userID *uint, currentUserID uint) ([]models.AuditLog, error) {
	offset := (page - 1) * pageSize

	// 获取当前用户信息
	currentUser, err := s.userRepo.GetByID(currentUserID)
	if err != nil {
		return nil, errors.New("当前用户不存在")
	}

	// SuperAdmin可以查看所有日志
	if currentUser.UserType == models.UserTypeSuperAdmin {
		return s.repo.List(offset, pageSize, userID)
	}

	// 租户管理员和普通用户只能查看本租户的日志
	if currentUser.TenantID == nil {
		return []models.AuditLog{}, nil
	}
	return s.repo.ListByTenant(*currentUser.TenantID, offset, pageSize, userID)
}

func (s *LogService) GetByID(id uint) (*models.AuditLog, error) {
	return s.repo.GetByID(id)
}