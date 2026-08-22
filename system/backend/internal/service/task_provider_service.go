package service

import (
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/models"
)

// TaskProviderService projects module definitions into callable TaskProviders.
// The declaration is persistent; availability and BaseURL are resolved from a
// current Backend lease on every read.
type TaskProviderService struct {
	modules *ModuleRegistryService
}

func NewTaskProviderService(modules *ModuleRegistryService) *TaskProviderService {
	return &TaskProviderService{modules: modules}
}

func (s *TaskProviderService) List() ([]*commonmodels.TaskProvider, error) {
	modules, err := s.modules.ListModules()
	if err != nil {
		return nil, err
	}
	providers := make([]*commonmodels.TaskProvider, 0, len(modules))
	for _, module := range modules {
		if module.TaskProvider != nil {
			providers = append(providers, projectTaskProvider(module))
		}
	}
	return providers, nil
}

func (s *TaskProviderService) GetByModuleName(moduleName string) (*commonmodels.TaskProvider, error) {
	module, err := s.modules.GetModule(strings.TrimSpace(moduleName))
	if err != nil {
		return nil, err
	}
	if module.TaskProvider == nil {
		return nil, commonapi.ErrNotFound
	}
	return projectTaskProvider(module), nil
}

func projectTaskProvider(module *models.ModuleInfo) *commonmodels.TaskProvider {
	provider := &commonmodels.TaskProvider{
		ID: module.ID, ModuleName: module.ModuleName, ModuleVersion: module.Version, Enabled: module.Enabled,
		TaskProviderDeclaration: *module.TaskProvider,
		CreatedAt:               module.CreatedAt, UpdatedAt: module.UpdatedAt,
	}
	if !module.Enabled {
		return provider
	}
	now := time.Now()
	for _, instance := range module.Instances {
		if instance.Role != models.ModuleRuntimeRoleBackend ||
			instance.Status != models.ModuleRuntimeStatusUp ||
			!instance.LeaseExpiresAt.After(now) || strings.TrimSpace(instance.ModuleURL) == "" {
			continue
		}
		provider.Available = true
		provider.BaseURL = strings.TrimRight(instance.ModuleURL, "/")
		provider.BackendInstanceID = instance.InstanceID
		break
	}
	return provider
}
