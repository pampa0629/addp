package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
	"github.com/addp/common/models"
	localModels "github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
)

var ErrInvalidCapabilityRegistration = errors.New("invalid capability registration")

// RegistryService 能力注册服务
type RegistryService struct {
	resourceRepo *repository.EngineRepository
}

// NewRegistryService 创建能力注册服务
func NewRegistryService(resourceRepo *repository.EngineRepository) *RegistryService {
	return &RegistryService{
		resourceRepo: resourceRepo,
	}
}

// RegisterCapability 注册能力（幂等操作）
// 返回引擎 ID，以便调用方触发连接测试
func (s *RegistryService) RegisterCapability(ctx context.Context, req *models.CapabilityRegistrationRequest) (uint, error) {
	capabilitiesJSON, err := s.prepareRegistrationCapabilities(req)
	if err != nil {
		return 0, err
	}

	// 1. 根据 engine_type + is_builtin 查询是否已存在（幂等检查）
	var existing *localModels.Engine
	if req.IsBuiltin {
		// 内置引擎：通过 engine_type + is_builtin 查找
		existing, err = s.resourceRepo.FindByEngineTypeAndBuiltin(ctx, req.EngineType)
		if err != nil && !errors.Is(err, commonapi.ErrNotFound) {
			return 0, fmt.Errorf("failed to query existing resource: %w", err)
		}
	}

	// 2. 准备 ConnectionInfo（对于计算引擎，可以为空）
	connectionInfo := localModels.ConnectionInfo{}
	if req.ConnectionInfo != nil {
		connectionInfo = req.ConnectionInfo
	}

	// 3. 幂等更新或创建
	if existing != nil {
		// 已存在，更新配置
		// 内置引擎使用插件的 DisplayName()，非内置引擎使用请求中的 Name
		engineName := req.Name
		if req.IsBuiltin {
			if p, err := plugin.Get(req.EngineType); err == nil && p != nil {
				engineName = p.DisplayName()
			}
		}

		updates := map[string]interface{}{
			"name":            engineName,
			"engine_type":     req.EngineType,
			"description":     req.Description,
			"is_builtin":      req.IsBuiltin,
			"capabilities":    capabilitiesJSON,
			"lifecycle_state": models.EngineLifecycleActive,
			"connection_info": connectionInfo,
		}

		if err := s.resourceRepo.UpdateByID(ctx, existing.ID, updates); err != nil {
			return 0, fmt.Errorf("failed to update existing resource: %w", err)
		}

		return existing.ID, nil
	}

	// 不存在，创建新记录
	engineOrigin := "general"
	if p, err := plugin.Get(req.EngineType); err == nil && p != nil {
		engineOrigin = p.EngineOrigin()
	}

	// 内置引擎使用插件的 DisplayName()，非内置引擎使用请求中的 Name
	engineName := req.Name
	if req.IsBuiltin {
		if p, err := plugin.Get(req.EngineType); err == nil && p != nil {
			engineName = p.DisplayName()
		}
	}

	resource := &localModels.Engine{
		Name:                   engineName,
		EngineType:             req.EngineType,
		EngineOrigin:           engineOrigin,
		Description:            req.Description,
		ConnectionInfo:         connectionInfo,
		IsBuiltin:              req.IsBuiltin,
		Capabilities:           toJSONStringPtrFromString(capabilitiesJSON),
		LifecycleState:         models.EngineLifecycleActive,
		ExternalArtifactPolicy: models.ExternalArtifactPolicyDelete,
		TenantID:               nil, // 能力注册不属于特定租户
	}

	if err := s.resourceRepo.CreateWithContext(ctx, resource); err != nil {
		return 0, fmt.Errorf("failed to create resource: %w", err)
	}

	return resource.ID, nil
}

func (s *RegistryService) prepareRegistrationCapabilities(req *models.CapabilityRegistrationRequest) (*string, error) {
	if req == nil {
		return nil, invalidCapabilityRegistrationError("capability registration request is required")
	}

	engineType := strings.ToLower(strings.TrimSpace(req.EngineType))
	if engineType == "" {
		return nil, invalidCapabilityRegistrationError("engine_type is required")
	}
	req.EngineType = engineType

	if req.Capabilities == nil {
		capabilitiesJSON, err := dbbridge.GenerateCapabilities(engineType)
		if err != nil {
			return nil, invalidCapabilityRegistrationError("capabilities is required for engine type %s: %v", engineType, err)
		}
		return &capabilitiesJSON, nil
	}

	if req.Capabilities.SchemaVersion != plugin.CapabilitiesSchemaVersion {
		return nil, invalidCapabilityRegistrationError("capabilities.schema_version must be %q", plugin.CapabilitiesSchemaVersion)
	}
	if req.Capabilities.EngineType == "" {
		return nil, invalidCapabilityRegistrationError("capabilities.engine_type is required")
	}
	if req.Capabilities.EngineType != engineType {
		return nil, invalidCapabilityRegistrationError("capabilities.engine_type %q does not match engine_type %q", req.Capabilities.EngineType, engineType)
	}

	capBytes, err := json.Marshal(req.Capabilities)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capStr := string(capBytes)
	return &capStr, nil
}

func invalidCapabilityRegistrationError(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrInvalidCapabilityRegistration, fmt.Sprintf(format, args...))
}

func toJSONStringPtrFromString(value *string) *localModels.JSONString {
	if value == nil {
		return nil
	}
	jsonValue := localModels.JSONString(*value)
	return &jsonValue
}

// ListCapabilities 查询能力列表（支持过滤）
func (s *RegistryService) ListCapabilities(ctx context.Context, filters map[string]interface{}) ([]*localModels.Engine, error) {
	engines, err := s.resourceRepo.FindByFilters(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list capabilities: %w", err)
	}

	return engines, nil
}

// ListComputeEngines 查询所有具有计算能力的引擎
func (s *RegistryService) ListComputeEngines(ctx context.Context) ([]*localModels.Engine, error) {
	filters := map[string]interface{}{
		"lifecycle_state": models.EngineLifecycleActive,
	}

	engines, err := s.resourceRepo.FindByFilters(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list compute engines: %w", err)
	}

	return filterComputeEngines(engines), nil
}

func filterComputeEngines(engines []*localModels.Engine) []*localModels.Engine {
	filtered := make([]*localModels.Engine, 0, len(engines))
	for _, engine := range engines {
		if engine == nil {
			continue
		}
		if len(engineselection.GetSupportedComputeEntrypoints((*models.Engine)(engine))) > 0 {
			filtered = append(filtered, engine)
		}
	}
	return filtered
}
