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
	resourceRepo *repository.ResourceRepository
}

// NewRegistryService 创建能力注册服务
func NewRegistryService(resourceRepo *repository.ResourceRepository) *RegistryService {
	return &RegistryService{
		resourceRepo: resourceRepo,
	}
}

// RegisterCapability 注册能力（幂等操作）
func (s *RegistryService) RegisterCapability(ctx context.Context, req *models.CapabilityRegistrationRequest) error {
	// 1. 根据 unique_identifier 查询是否已存在
	existing, err := s.resourceRepo.FindByUniqueIdentifier(ctx, req.UniqueIdentifier)
	if err != nil && err.Error() != "record not found" {
		return fmt.Errorf("failed to query existing resource: %w", err)
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

	var taskAPIConfigJSON *string
	if req.TaskAPIConfig != nil {
		configBytes, err := json.Marshal(req.TaskAPIConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal task_api_config: %w", err)
		}
		configStr := string(configBytes)
		taskAPIConfigJSON = &configStr
	}

	var healthCheckConfigJSON *string
	if req.HealthCheckConfig != nil {
		healthBytes, err := json.Marshal(req.HealthCheckConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal health_check_config: %w", err)
		}
		healthStr := string(healthBytes)
		healthCheckConfigJSON = &healthStr
	}

	// 3. 准备 ConnectionInfo（对于计算引擎，可以为空）
	connectionInfo := localModels.ConnectionInfo{}
	if req.ConnectionInfo != nil {
		connectionInfo = req.ConnectionInfo
	}

	// 4. 幂等更新或创建
	if existing != nil {
		// 已存在，更新配置
		// 如果未提供 display_name，默认等于 name
		displayName := req.DisplayName
		if displayName == "" {
			displayName = req.Name
		}

		updates := map[string]interface{}{
			"name":                req.Name,
			"display_name":        displayName,
			"resource_type":       req.ResourceType,
			"description":         req.Description,
			"is_builtin":          req.IsBuiltin,
			"capabilities":        capabilitiesJSON,
			"task_api_config":     taskAPIConfigJSON,
			"health_check_config": healthCheckConfigJSON,
			"is_active":           true,  // 注册时总是激活
			"deleted_at":          nil,    // 恢复软删除的记录
		}

		if err := s.resourceRepo.UpdateByID(ctx, existing.ID, updates); err != nil {
			return fmt.Errorf("failed to update existing resource: %w", err)
		}

		return nil
	}

	// 不存在，创建新记录
	// 如果未提供 display_name，默认等于 name
	displayName := req.DisplayName
	if displayName == "" {
		displayName = req.Name
	}

	resource := &localModels.Resource{
		Name:              req.Name,
		DisplayName:       displayName,
		ResourceType:      req.ResourceType,
		Description:       req.Description,
		ConnectionInfo:    connectionInfo,
		UniqueIdentifier:  &req.UniqueIdentifier,
		IsBuiltin:         req.IsBuiltin,
		Capabilities:      capabilitiesJSON,
		TaskAPIConfig:     taskAPIConfigJSON,
		HealthCheckConfig: healthCheckConfigJSON,
		IsActive:          true,
		TenantID:          nil, // 能力注册不属于特定租户
	}

	if err := s.resourceRepo.CreateWithContext(ctx, resource); err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	return nil
}

// ListCapabilities 查询能力列表（支持过滤）
func (s *RegistryService) ListCapabilities(ctx context.Context, filters map[string]interface{}) ([]*localModels.Resource, error) {
	resources, err := s.resourceRepo.FindByFilters(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list capabilities: %w", err)
	}

	return resources, nil
}

// GetCapabilityByIdentifier 根据 unique_identifier 查询能力
func (s *RegistryService) GetCapabilityByIdentifier(ctx context.Context, identifier string) (*localModels.Resource, error) {
	resource, err := s.resourceRepo.FindByUniqueIdentifier(ctx, identifier)
	if err != nil {
		return nil, fmt.Errorf("failed to get capability: %w", err)
	}

	return resource, nil
}

// ListComputeResources 查询所有具有计算能力的资源
func (s *RegistryService) ListComputeResources(ctx context.Context) ([]*localModels.Resource, error) {
	// 查询条件:
	// 1. capabilities.compute 不为空（JSONB 查询）
	// 2. is_active = true
	filters := map[string]interface{}{
		"has_compute_capability": true,
		"is_active":              true,
	}

	resources, err := s.resourceRepo.FindByFilters(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("failed to list compute resources: %w", err)
	}

	return resources, nil
}
