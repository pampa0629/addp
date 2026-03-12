package service

import (
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

type TaskProviderService struct {
	db *gorm.DB
}

func NewTaskProviderService(db *gorm.DB) *TaskProviderService {
	return &TaskProviderService{db: db}
}

// RegisterOrUpdate 注册或更新任务提供者
func (s *TaskProviderService) RegisterOrUpdate(provider *models.TaskProvider) (*models.TaskProvider, error) {
	var existing models.TaskProvider
	result := s.db.Where("module_name = ?", provider.ModuleName).First(&existing)

	if result.Error == gorm.ErrRecordNotFound {
		// 不存在，创建新记录
		if err := s.db.Create(provider).Error; err != nil {
			return nil, err
		}
		return provider, nil
	} else if result.Error != nil {
		return nil, result.Error
	}

	// 已存在，更新配置（保持用户的 is_enabled 设置）
	updates := map[string]interface{}{
		"display_name":           provider.DisplayName,
		"description":            provider.Description,
		"base_url":               provider.BaseURL,
		"task_list_endpoint":     provider.TaskListEndpoint,
		"task_detail_endpoint":   provider.TaskDetailEndpoint,
		"task_execute_endpoint":  provider.TaskExecuteEndpoint,
		"task_status_endpoint":   provider.TaskStatusEndpoint,
		"task_cancel_endpoint":   provider.TaskCancelEndpoint,
		"capabilities":           provider.Capabilities,
	}

	if err := s.db.Model(&existing).Updates(updates).Error; err != nil {
		return nil, err
	}

	// 重新查询更新后的记录
	s.db.Where("module_name = ?", provider.ModuleName).First(&existing)
	return &existing, nil
}

// ListEnabled 查询所有启用的任务提供者
func (s *TaskProviderService) ListEnabled() ([]*models.TaskProvider, error) {
	var providers []*models.TaskProvider
	err := s.db.Where("is_enabled = ?", true).Order("module_name").Find(&providers).Error
	return providers, err
}

// GetByModuleName 根据模块名获取任务提供者
func (s *TaskProviderService) GetByModuleName(moduleName string) (*models.TaskProvider, error) {
	var provider models.TaskProvider
	err := s.db.Where("module_name = ? AND is_enabled = ?", moduleName, true).First(&provider).Error
	return &provider, err
}
