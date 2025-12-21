package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
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
