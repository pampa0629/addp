package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
)

// ModuleRegistryService 模块注册业务逻辑层
type ModuleRegistryService struct {
	repo *repository.ModuleRegistryRepository
}

// NewModuleRegistryService 创建模块注册Service
func NewModuleRegistryService(repo *repository.ModuleRegistryRepository) *ModuleRegistryService {
	return &ModuleRegistryService{repo: repo}
}

// Register 注册模块
func (s *ModuleRegistryService) Register(req *models.ModuleRegistrationRequest) error {
	// 验证请求参数
	if req.ModuleName == "" || req.ModuleURL == "" || req.RoutePrefix == "" {
		return fmt.Errorf("module_name, module_url and route_prefix are required")
	}

	// 调用repository注册模块
	if err := s.repo.Register(req); err != nil {
		logger.L().Error("模块注册失败", "module", req.ModuleName, "error", err)
		return err
	}

	logger.L().Info("模块注册成功", "module", req.ModuleName, "url", req.ModuleURL)
	return nil
}

// SendHeartbeat 发送心跳
func (s *ModuleRegistryService) SendHeartbeat(moduleName string) error {
	if err := s.repo.UpdateHeartbeat(moduleName); err != nil {
		logger.L().Error("心跳更新失败", "module", moduleName, "error", err)
		return err
	}

	logger.L().Debug("心跳更新成功", "module", moduleName)
	return nil
}

// GetModule 获取单个模块信息
func (s *ModuleRegistryService) GetModule(moduleName string) (*models.ModuleInfo, error) {
	module, err := s.repo.GetModule(moduleName)
	if err != nil {
		return nil, err
	}

	return s.convertToModuleInfo(module), nil
}

// ListModules 获取所有模块列表
func (s *ModuleRegistryService) ListModules() ([]*models.ModuleInfo, error) {
	modules, err := s.repo.ListModules()
	if err != nil {
		return nil, err
	}

	result := make([]*models.ModuleInfo, len(modules))
	for i, module := range modules {
		result[i] = s.convertToModuleInfo(&module)
	}

	return result, nil
}

// ListActiveModules 获取所有活跃模块
func (s *ModuleRegistryService) ListActiveModules() ([]*models.ModuleInfo, error) {
	modules, err := s.repo.ListActiveModules()
	if err != nil {
		return nil, err
	}

	result := make([]*models.ModuleInfo, len(modules))
	for i, module := range modules {
		result[i] = s.convertToModuleInfo(&module)
	}

	return result, nil
}

// DeleteModule 删除模块注册
func (s *ModuleRegistryService) DeleteModule(moduleName string) error {
	if err := s.repo.DeleteModule(moduleName); err != nil {
		logger.L().Error("模块注销失败", "module", moduleName, "error", err)
		return err
	}

	logger.L().Info("模块注销成功", "module", moduleName)
	return nil
}

// StartCleanupTask 启动模块清理定时任务
func (s *ModuleRegistryService) StartCleanupTask(ctx context.Context, timeout time.Duration) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	logger.L().Info("模块清理任务已启动", "timeout", timeout)

	for {
		select {
		case <-ctx.Done():
			logger.L().Info("模块清理任务已停止")
			return
		case <-ticker.C:
			if err := s.repo.MarkStaleModules(timeout); err != nil {
				logger.L().Error("标记超时模块失败", "error", err)
			}
		}
	}
}

// convertToModuleInfo 转换为ModuleInfo响应
func (s *ModuleRegistryService) convertToModuleInfo(module *models.ModuleRegistry) *models.ModuleInfo {
	var metadata map[string]interface{}
	if module.Metadata != nil {
		_ = json.Unmarshal(module.Metadata, &metadata)
	}

	return &models.ModuleInfo{
		ID:             module.ID,
		ModuleName:     module.ModuleName,
		ModuleURL:      module.ModuleURL,
		RoutePrefix:    module.RoutePrefix,
		HealthCheckURL: module.HealthCheckURL,
		Status:         module.Status,
		LastHeartbeat:  module.LastHeartbeat,
		Metadata:       metadata,
		CreatedAt:      module.CreatedAt,
		UpdatedAt:      module.UpdatedAt,
	}
}
