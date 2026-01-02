package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/addp/common/models"
	localModels "github.com/addp/system/internal/models"
	"github.com/addp/system/internal/repository"
)

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
func (s *RegistryService) RegisterCapability(ctx context.Context, req *models.CapabilityRegistrationRequest) error {
	// 1. 根据 engine_type + is_builtin 查询是否已存在（幂等检查）
	var existing *localModels.Engine
	if req.IsBuiltin {
		// 内置引擎：通过 engine_type + is_builtin 查找
		var err error
		existing, err = s.resourceRepo.FindByEngineTypeAndBuiltin(ctx, req.EngineType)
		if err != nil && err.Error() != "record not found" {
			return fmt.Errorf("failed to query existing resource: %w", err)
		}
	}

	// 2. 序列化 JSON 字段
	var capabilitiesJSON *string
	if req.Capabilities != nil {
		capBytes, err := json.Marshal(req.Capabilities)
		if err != nil {
			return fmt.Errorf("failed to marshal capabilities: %w", err)
		}
		capStr := string(capBytes)
		capabilitiesJSON = &capStr
	}

	// 3. 准备 ConnectionInfo（对于计算引擎，可以为空）
	connectionInfo := localModels.ConnectionInfo{}
	if req.ConnectionInfo != nil {
		connectionInfo = req.ConnectionInfo
	}

	// 4. 幂等更新或创建
	if existing != nil {
		// 已存在，更新配置
		updates := map[string]interface{}{
			"name":         req.Name,
			"engine_type":  req.EngineType,
			"description":  req.Description,
			"is_builtin":   req.IsBuiltin,
			"capabilities": capabilitiesJSON,
			"is_active":    true, // 注册时总是激活
		}

		if err := s.resourceRepo.UpdateByID(ctx, existing.ID, updates); err != nil {
			return fmt.Errorf("failed to update existing resource: %w", err)
		}

		return nil
	}

	// 不存在，创建新记录
	resource := &localModels.Engine{
		Name:           req.Name,
		EngineType:     req.EngineType,
		Description:    req.Description,
		ConnectionInfo: connectionInfo,
		IsBuiltin:      req.IsBuiltin,
		Capabilities:   capabilitiesJSON,
		IsActive:       true,
		TenantID:       nil, // 能力注册不属于特定租户
	}

	if err := s.resourceRepo.CreateWithContext(ctx, resource); err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	return nil
}

// ListCapabilities 查询能力列表（支持过滤）
func (s *RegistryService) ListCapabilities(ctx context.Context, filters map[string]interface{}) ([]*localModels.Engine, error) {
	engines, err := s.resourceRepo.FindByFilters(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list capabilities: %w", err)
	}

	return engines, nil
}

// GetCapabilityByIdentifier 根据 unique_identifier 查询能力
func (s *RegistryService) GetCapabilityByIdentifier(ctx context.Context, identifier string) (*localModels.Engine, error) {
	resource, err := s.resourceRepo.FindByUniqueIdentifier(ctx, identifier)
	if err != nil {
		return nil, fmt.Errorf("failed to get capability: %w", err)
	}

	return resource, nil
}

// ListComputeEngines 查询所有具有计算能力的引擎
func (s *RegistryService) ListComputeEngines(ctx context.Context) ([]*localModels.Engine, error) {
	// 查询条件:
	// 1. capabilities.compute 不为空（JSONB 查询）
	// 2. is_active = true
	filters := map[string]interface{}{
		"has_compute_capability": true,
		"is_active":              true,
	}

	engines, err := s.resourceRepo.FindByFilters(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list compute engines: %w", err)
	}

	return engines, nil
}
