package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
	"github.com/addp/orchestrator/internal/models"
)

// TaskProviderRegistry 任务提供者注册表(从 System 动态加载)
type TaskProviderRegistry struct {
	systemClient *commonClient.SystemClient
	providers    map[string]*commonModels.TaskProvider // key: module_name
	mu           sync.RWMutex
	cacheTTL     time.Duration
	lastRefresh  time.Time
}

// NewTaskProviderRegistry 创建任务提供者注册表
func NewTaskProviderRegistry(systemURL, internalAPIKey string, cacheTTL time.Duration) *TaskProviderRegistry {
	if cacheTTL == 0 {
		cacheTTL = 5 * time.Minute // 默认缓存 5 分钟
	}

	return &TaskProviderRegistry{
		systemClient: commonClient.NewSystemClientWithInternalKey(systemURL, internalAPIKey),
		providers:    make(map[string]*commonModels.TaskProvider),
		cacheTTL:     cacheTTL,
	}
}

// GetProvider 根据 module_name 获取任务提供者配置
func (r *TaskProviderRegistry) GetProvider(ctx context.Context, moduleName string) (*commonModels.TaskProvider, error) {
	// 先检查缓存
	r.mu.RLock()
	if provider, ok := r.providers[moduleName]; ok && time.Since(r.lastRefresh) < r.cacheTTL {
		r.mu.RUnlock()
		return provider, nil
	}
	r.mu.RUnlock()

	// 缓存未命中或过期,从 System 查询
	provider, err := r.systemClient.GetTaskProvider(moduleName)
	if err != nil {
		return nil, fmt.Errorf("failed to get task provider %s: %w", moduleName, err)
	}

	// 更新缓存
	r.mu.Lock()
	r.providers[moduleName] = provider
	r.mu.Unlock()

	return provider, nil
}

// RefreshCache 刷新所有任务提供者缓存
func (r *TaskProviderRegistry) RefreshCache(ctx context.Context) error {
	// 从 System 查询所有任务提供者
	providers, err := r.systemClient.ListTaskProviders()
	if err != nil {
		return fmt.Errorf("failed to list task providers: %w", err)
	}

	// 更新缓存
	r.mu.Lock()
	defer r.mu.Unlock()

	// 清空现有缓存
	r.providers = make(map[string]*commonModels.TaskProvider)

	// 重新填充
	for _, provider := range providers {
		r.providers[provider.ModuleName] = provider
	}

	r.lastRefresh = time.Now()
	return nil
}

// ListAllProviders 列出所有已注册的任务提供者
func (r *TaskProviderRegistry) ListAllProviders(ctx context.Context) ([]*commonModels.TaskProvider, error) {
	// 检查缓存是否有效
	r.mu.RLock()
	cacheValid := time.Since(r.lastRefresh) < r.cacheTTL
	r.mu.RUnlock()

	if !cacheValid {
		// 刷新缓存
		if err := r.RefreshCache(ctx); err != nil {
			return nil, fmt.Errorf("failed to refresh task provider cache: %w", err)
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	providers := make([]*commonModels.TaskProvider, 0, len(r.providers))
	for _, provider := range r.providers {
		providers = append(providers, provider)
	}

	return providers, nil
}

// ValidateStepTaskReferences 校验编排步骤引用的 provider/task_type 已由 TaskProvider capabilities 声明。
func (r *TaskProviderRegistry) ValidateStepTaskReferences(ctx context.Context, steps models.Steps) error {
	capabilityCache := map[string]*taskprovider.TaskCapability{}
	for i, step := range steps {
		providerName := strings.TrimSpace(step.Provider)
		taskType := strings.TrimSpace(step.TaskType)
		if providerName == "" || taskType == "" {
			return &StepTaskValidationError{Code: StepTaskMissingReference, StepIndex: i, Provider: providerName, TaskType: taskType}
		}

		key := providerName + "\x00" + taskType
		taskTypeCapability, exists := capabilityCache[key]
		if !exists {
			provider, err := r.GetProvider(ctx, providerName)
			if err != nil {
				return &StepTaskValidationError{Code: StepTaskProviderUnavailable, StepIndex: i, Provider: providerName, TaskType: taskType, Cause: err}
			}

			taskTypeCapability, err = providerTaskCapability(provider, taskType)
			if err != nil {
				return &StepTaskValidationError{Code: StepTaskCapabilitiesInvalid, StepIndex: i, Provider: providerName, TaskType: taskType, Cause: err}
			}
			capabilityCache[key] = taskTypeCapability
		}
		if taskTypeCapability == nil {
			return &StepTaskValidationError{Code: StepTaskTypeUndeclared, StepIndex: i, Provider: providerName, TaskType: taskType}
		}
		if taskTypeCapability.Deprecated {
			return &StepTaskValidationError{Code: StepTaskTypeDeprecated, StepIndex: i, Provider: providerName, TaskType: taskType}
		}
		if err := validateStepParametersByExecutionSchema(step, taskTypeCapability.ExecutionSchema, true); err != nil {
			return &StepTaskValidationError{Code: StepTaskParametersInvalid, StepIndex: i, Provider: providerName, TaskType: taskType, Cause: err}
		}

	}
	return nil
}

func providerTaskCapability(provider *commonModels.TaskProvider, taskType string) (*taskprovider.TaskCapability, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is nil")
	}
	if provider.Capabilities == nil || strings.TrimSpace(string(*provider.Capabilities)) == "" {
		return nil, fmt.Errorf("capabilities is required")
	}

	capabilities, err := taskprovider.ParseCapabilities(string(*provider.Capabilities))
	if err != nil {
		return nil, err
	}
	return capabilities.CapabilityFor(taskType), nil
}

func validateStepParametersByExecutionSchema(step models.Step, executionSchema map[string]interface{}, allowTemplateStrings bool) error {
	return taskprovider.ValidateExecutionParameters(executionSchema, step.Parameters, taskprovider.ParameterValidationOptions{
		AllowTemplateStrings: allowTemplateStrings,
	})
}
