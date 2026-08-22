package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	commonconfiguration "github.com/addp/common/configuration"
	"github.com/addp/common/logger"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	"github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
)

var ErrInvalidModuleRegistration = errors.New("invalid module registration")
var ErrModuleDefinitionVersionConflict = errors.New("module definition version conflict")

type ModuleRegistryService struct {
	repo          *repository.ModuleRegistryRepository
	leaseDuration time.Duration
}

func NewModuleRegistryService(repo *repository.ModuleRegistryRepository) *ModuleRegistryService {
	return &ModuleRegistryService{repo: repo, leaseDuration: models.ModuleRuntimeLeaseDuration}
}

func (s *ModuleRegistryService) Register(req *models.ModuleRegistrationRequest) error {
	if req == nil {
		return fmt.Errorf("%w: request is required", ErrInvalidModuleRegistration)
	}
	req.ModuleName = strings.TrimSpace(req.ModuleName)
	req.InstanceID = strings.TrimSpace(req.InstanceID)
	req.Role = strings.ToLower(strings.TrimSpace(req.Role))
	req.ModuleURL = strings.TrimRight(strings.TrimSpace(req.ModuleURL), "/")
	req.RoutePrefix = strings.TrimSpace(req.RoutePrefix)
	req.HealthCheckURL = strings.TrimRight(strings.TrimSpace(req.HealthCheckURL), "/")
	if req.ModuleName == "" || req.InstanceID == "" || req.Role == "" || req.RoutePrefix == "" {
		return fmt.Errorf("%w: module_name, instance_id, role and route_prefix are required", ErrInvalidModuleRegistration)
	}
	switch req.Role {
	case models.ModuleRuntimeRoleBackend, models.ModuleRuntimeRoleWorker, models.ModuleRuntimeRoleScheduler:
	default:
		return fmt.Errorf("%w: role must be backend, worker or scheduler", ErrInvalidModuleRegistration)
	}
	if req.Role == models.ModuleRuntimeRoleBackend && req.ModuleURL == "" {
		return fmt.Errorf("%w: backend module_url is required", ErrInvalidModuleRegistration)
	}
	if err := commonconfiguration.ValidateManagementDeclaration(req.ModuleName, req.ConfigurationManagement); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidModuleRegistration, err)
	}
	if req.Role != models.ModuleRuntimeRoleBackend && req.TaskProvider != nil {
		return fmt.Errorf("%w: only backend instances may publish task_provider", ErrInvalidModuleRegistration)
	}
	if err := taskprovider.ValidateDeclaration(req.TaskProvider); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidModuleRegistration, err)
	}
	if err := s.repo.Register(req, s.leaseDuration); err != nil {
		logger.L().Error("模块运行实例注册失败", "module", req.ModuleName, "instance_id", req.InstanceID, "error", err)
		return err
	}
	logger.L().Info("模块运行实例注册成功", "module", req.ModuleName, "instance_id", req.InstanceID, "role", req.Role)
	return nil
}

func (s *ModuleRegistryService) SendHeartbeat(moduleName, instanceID string) error {
	if strings.TrimSpace(moduleName) == "" || strings.TrimSpace(instanceID) == "" {
		return fmt.Errorf("module_name and instance_id are required")
	}
	if err := s.repo.UpdateHeartbeat(moduleName, instanceID, s.leaseDuration); err != nil {
		logger.L().Error("模块运行实例心跳更新失败", "module", moduleName, "instance_id", instanceID, "error", err)
		return err
	}
	return nil
}

func (s *ModuleRegistryService) GetModule(moduleName string) (*models.ModuleInfo, error) {
	definition, err := s.repo.GetModule(moduleName)
	if err != nil {
		return nil, err
	}
	return s.convertToModuleInfo(definition, false), nil
}

func (s *ModuleRegistryService) ListModules() ([]*models.ModuleInfo, error) {
	return s.listModules(false)
}

func (s *ModuleRegistryService) UpdateModuleDefinition(moduleName string, req *models.ModuleDefinitionUpdateRequest) (*models.ModuleInfo, error) {
	moduleName = strings.TrimSpace(moduleName)
	if moduleName == "" || req == nil || req.Enabled == nil || req.Version < 1 {
		return nil, fmt.Errorf("%w: enabled and positive version are required", ErrInvalidModuleRegistration)
	}
	definition, err := s.repo.UpdateEnabled(moduleName, *req.Enabled, req.Version)
	if errors.Is(err, repository.ErrModuleDefinitionVersionConflict) {
		return nil, ErrModuleDefinitionVersionConflict
	}
	if err != nil {
		return nil, err
	}
	return s.convertToModuleInfo(definition, false), nil
}

// ListActiveModules 只返回已启用且至少有一个租约有效 Backend 实例的模块。
func (s *ModuleRegistryService) ListActiveModules() ([]*models.ModuleInfo, error) {
	return s.listModules(true)
}

func (s *ModuleRegistryService) listModules(activeOnly bool) ([]*models.ModuleInfo, error) {
	definitions, err := s.repo.ListModules()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	result := make([]*models.ModuleInfo, 0, len(definitions))
	for index := range definitions {
		info := s.convertToModuleInfo(&definitions[index], activeOnly)
		if activeOnly && (!info.Enabled || !hasRoutableBackend(info, now)) {
			continue
		}
		result = append(result, info)
	}
	return result, nil
}

func hasRoutableBackend(module *models.ModuleInfo, now time.Time) bool {
	if module == nil || !module.Enabled {
		return false
	}
	for _, instance := range module.Instances {
		if instance.Role == models.ModuleRuntimeRoleBackend && instance.Status == models.ModuleRuntimeStatusUp && instance.LeaseExpiresAt.After(now) && instance.ModuleURL != "" {
			return true
		}
	}
	return false
}

func (s *ModuleRegistryService) ListConfigurationManagementEntries(contextType string, permissions map[string]struct{}) ([]models.ConfigurationManagementEntryView, error) {
	modules, err := s.ListModules()
	if err != nil {
		return nil, err
	}
	entries := make([]models.ConfigurationManagementEntryView, 0)
	for _, module := range modules {
		if module.ConfigurationManagement == nil {
			continue
		}
		if err := commonconfiguration.ValidateManagementDeclaration(module.ModuleName, module.ConfigurationManagement); err != nil {
			return nil, err
		}
		available := hasRoutableBackend(module, time.Now())
		status := models.ModuleRuntimeStatusDown
		if available {
			status = models.ModuleRuntimeStatusUp
		}
		for _, entry := range module.ConfigurationManagement.Entries {
			if !commonconfiguration.EntryVisibleInContext(entry, contextType) {
				continue
			}
			if _, allowed := permissions[entry.ReadPermission]; !allowed {
				continue
			}
			entries = append(entries, models.ConfigurationManagementEntryView{
				ManagementEntry: entry, ModuleStatus: status, Available: available,
			})
		}
	}
	return entries, nil
}

func (s *ModuleRegistryService) StartCleanupTask(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := s.repo.MarkStaleModules(now); err != nil {
				logger.L().Error("标记超时模块运行实例失败", "error", err)
			}
		}
	}
}

func (s *ModuleRegistryService) convertToModuleInfo(definition *models.ModuleDefinition, activeInstancesOnly bool) *models.ModuleInfo {
	var configuration *commonconfiguration.ManagementDeclaration
	if len(definition.ConfigurationManagement) > 0 {
		var declaration *commonconfiguration.ManagementDeclaration
		if json.Unmarshal(definition.ConfigurationManagement, &declaration) == nil {
			configuration = declaration
		}
	}
	var provider *commonmodels.TaskProviderDeclaration
	if len(definition.TaskProvider) > 0 {
		var declaration *commonmodels.TaskProviderDeclaration
		if json.Unmarshal(definition.TaskProvider, &declaration) == nil {
			provider = declaration
		}
	}
	now := time.Now()
	instances := make([]models.ModuleRuntimeInstanceInfo, 0, len(definition.RuntimeInstances))
	for _, instance := range definition.RuntimeInstances {
		status := instance.Status
		if !instance.LeaseExpiresAt.After(now) {
			status = models.ModuleRuntimeStatusDown
		}
		if activeInstancesOnly && status != models.ModuleRuntimeStatusUp {
			continue
		}
		var metadata map[string]interface{}
		if len(instance.Metadata) > 0 {
			_ = json.Unmarshal(instance.Metadata, &metadata)
		}
		instances = append(instances, models.ModuleRuntimeInstanceInfo{
			ID: instance.ID, InstanceID: instance.InstanceID, Role: instance.Role,
			ModuleURL: instance.ModuleURL, HealthCheckURL: instance.HealthCheckURL,
			Status: status, LastHeartbeat: instance.LastHeartbeat, LeaseExpiresAt: instance.LeaseExpiresAt,
			Metadata: metadata, RegisteredAt: instance.RegisteredAt, UpdatedAt: instance.UpdatedAt,
		})
	}
	return &models.ModuleInfo{
		ID: definition.ID, ModuleName: definition.ModuleName, RoutePrefix: definition.RoutePrefix,
		Enabled: definition.Enabled, Version: definition.Version,
		Instances: instances, ConfigurationManagement: configuration,
		TaskProvider: provider,
		CreatedAt:    definition.CreatedAt, UpdatedAt: definition.UpdatedAt,
	}
}
