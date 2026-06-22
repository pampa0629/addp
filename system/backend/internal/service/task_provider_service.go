package service

import (
	"fmt"
	"strings"

	"github.com/addp/common/taskprovider"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

type TaskProviderValidationError struct {
	Message string
}

func (e *TaskProviderValidationError) Error() string {
	return e.Message
}

type TaskProviderService struct {
	db *gorm.DB
}

func NewTaskProviderService(db *gorm.DB) *TaskProviderService {
	return &TaskProviderService{db: db}
}

// RegisterOrUpdate 注册或更新任务提供者
func (s *TaskProviderService) RegisterOrUpdate(provider *models.TaskProvider) (*models.TaskProvider, error) {
	if err := validateTaskProvider(provider); err != nil {
		return nil, err
	}

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
		"display_name":          provider.DisplayName,
		"description":           provider.Description,
		"base_url":              provider.BaseURL,
		"task_list_endpoint":    provider.TaskListEndpoint,
		"task_detail_endpoint":  provider.TaskDetailEndpoint,
		"task_execute_endpoint": provider.TaskExecuteEndpoint,
		"task_status_endpoint":  provider.TaskStatusEndpoint,
		"task_cancel_endpoint":  provider.TaskCancelEndpoint,
		"capabilities":          provider.Capabilities,
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

func validateTaskProvider(provider *models.TaskProvider) error {
	if provider == nil {
		return validationError("task provider is required")
	}
	if strings.TrimSpace(provider.ModuleName) == "" {
		return validationError("module_name is required")
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		return validationError("base_url is required")
	}
	if strings.TrimSpace(provider.TaskListEndpoint) == "" ||
		strings.TrimSpace(provider.TaskDetailEndpoint) == "" ||
		strings.TrimSpace(provider.TaskExecuteEndpoint) == "" ||
		strings.TrimSpace(provider.TaskStatusEndpoint) == "" {
		return validationError("task list/detail/execute/status endpoints are required")
	}
	if err := validateTaskProviderEndpoints(provider); err != nil {
		return err
	}
	if err := validateTaskProviderCapabilities(provider.Capabilities); err != nil {
		return err
	}
	capabilities, err := parseTaskProviderCapabilities(provider.Capabilities)
	if err != nil {
		return err
	}
	if capabilities.HasCancelableTaskType() && strings.TrimSpace(provider.TaskCancelEndpoint) == "" {
		return validationError("task_cancel_endpoint is required when any task_type supports cancel")
	}
	if !capabilities.HasCancelableTaskType() && strings.TrimSpace(provider.TaskCancelEndpoint) != "" {
		return validationError("task_cancel_endpoint must be empty when no task_type supports cancel")
	}
	return nil
}

func validateTaskProviderEndpoints(provider *models.TaskProvider) error {
	endpoints := map[string]string{
		"task_list_endpoint":    strings.TrimSpace(provider.TaskListEndpoint),
		"task_detail_endpoint":  strings.TrimSpace(provider.TaskDetailEndpoint),
		"task_execute_endpoint": strings.TrimSpace(provider.TaskExecuteEndpoint),
		"task_status_endpoint":  strings.TrimSpace(provider.TaskStatusEndpoint),
	}

	for field, endpoint := range endpoints {
		if !strings.HasPrefix(endpoint, "/") {
			return validationError("%s must start with /", field)
		}
		if strings.Contains(endpoint, "/provider/tasks") {
			return validationError("%s must use standard /tasks/{task_type}/{id} endpoint, not /provider/tasks", field)
		}
	}
	if !strings.Contains(endpoints["task_detail_endpoint"], "{task_type}") || !strings.Contains(endpoints["task_detail_endpoint"], "{id}") {
		return validationError("task_detail_endpoint must contain {task_type} and {id}")
	}
	if !strings.Contains(endpoints["task_execute_endpoint"], "{task_type}") || !strings.Contains(endpoints["task_execute_endpoint"], "{id}") {
		return validationError("task_execute_endpoint must contain {task_type} and {id}")
	}
	if !strings.Contains(endpoints["task_status_endpoint"], "{execution_id}") {
		return validationError("task_status_endpoint must contain {execution_id}")
	}
	expectedSuffixes := map[string]string{
		"task_list_endpoint":    "/tasks",
		"task_detail_endpoint":  "/tasks/{task_type}/{id}",
		"task_execute_endpoint": "/tasks/{task_type}/{id}/execute",
		"task_status_endpoint":  "/executions/{execution_id}",
	}
	for field, suffix := range expectedSuffixes {
		if !strings.HasSuffix(endpoints[field], suffix) {
			return validationError("%s must use standard endpoint suffix %s", field, suffix)
		}
	}
	if strings.TrimSpace(provider.TaskCancelEndpoint) != "" {
		cancelEndpoint := strings.TrimSpace(provider.TaskCancelEndpoint)
		if !strings.HasPrefix(cancelEndpoint, "/") {
			return validationError("task_cancel_endpoint must start with /")
		}
		if strings.Contains(cancelEndpoint, "/provider/tasks") {
			return validationError("task_cancel_endpoint must use standard execution cancel endpoint, not /provider/tasks")
		}
		if !strings.Contains(cancelEndpoint, "{execution_id}") {
			return validationError("task_cancel_endpoint must contain {execution_id}")
		}
		if !strings.HasSuffix(cancelEndpoint, "/executions/{execution_id}/cancel") {
			return validationError("task_cancel_endpoint must use standard endpoint suffix /executions/{execution_id}/cancel")
		}
	}
	return nil
}

func validateTaskProviderCapabilities(capabilities *models.JSONString) error {
	_, err := parseTaskProviderCapabilities(capabilities)
	return err
}

func parseTaskProviderCapabilities(capabilities *models.JSONString) (*taskprovider.Capabilities, error) {
	if capabilities == nil {
		return nil, validationError("capabilities is required")
	}
	parsed, err := taskprovider.ParseCapabilities(string(*capabilities))
	if err != nil {
		return nil, validationError("%s", err.Error())
	}
	return parsed, nil
}

func validationError(format string, args ...interface{}) error {
	return &TaskProviderValidationError{Message: fmt.Sprintf(format, args...)}
}
