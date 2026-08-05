package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	commonconfiguration "github.com/addp/common/configuration"
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
	if err := commonconfiguration.ValidateManagementDeclaration(req.ModuleName, req.ConfigurationManagement); err != nil {
		return err
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

func (s *ModuleRegistryService) ListConfigurationManagementEntries(contextType string, permissions map[string]struct{}) ([]models.ConfigurationManagementEntryView, error) {
	modules, err := s.repo.ListModules()
	if err != nil {
		return nil, err
	}
	entries := make([]models.ConfigurationManagementEntryView, 0)
	for index := range modules {
		module := &modules[index]
		if len(module.ConfigurationManagement) == 0 {
			continue
		}
		var declaration commonconfiguration.ManagementDeclaration
		if err := json.Unmarshal(module.ConfigurationManagement, &declaration); err != nil {
			return nil, fmt.Errorf("decode configuration management declaration for %s: %w", module.ModuleName, err)
		}
		if err := commonconfiguration.ValidateManagementDeclaration(module.ModuleName, &declaration); err != nil {
			return nil, err
		}
		for _, entry := range declaration.Entries {
			if !commonconfiguration.EntryVisibleInContext(entry, contextType) {
				continue
			}
			if _, allowed := permissions[entry.ReadPermission]; !allowed {
				continue
			}
			entries = append(entries, models.ConfigurationManagementEntryView{
				ManagementEntry: entry,
				ModuleStatus:    module.Status,
				Available:       module.Status == "up",
			})
		}
	}
	return entries, nil
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
	var configurationManagement *commonconfiguration.ManagementDeclaration
	if len(module.ConfigurationManagement) > 0 {
		var declaration commonconfiguration.ManagementDeclaration
		if json.Unmarshal(module.ConfigurationManagement, &declaration) == nil {
			configurationManagement = &declaration
		}
	}

	return &models.ModuleInfo{
		ID:                      module.ID,
		ModuleName:              module.ModuleName,
		ModuleURL:               module.ModuleURL,
		RoutePrefix:             module.RoutePrefix,
		HealthCheckURL:          module.HealthCheckURL,
		Status:                  module.Status,
		LastHeartbeat:           module.LastHeartbeat,
		Metadata:                metadata,
		ConfigurationManagement: configurationManagement,
		CreatedAt:               module.CreatedAt,
		UpdatedAt:               module.UpdatedAt,
	}
}
