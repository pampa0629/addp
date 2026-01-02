package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

// EngineRegistry 引擎注册表（从 System 动态加载计算引擎配置）
type EngineRegistry struct {
	systemClient *commonClient.SystemClient
	engines      map[string]*commonModels.Engine // key: engine_type
	mu           sync.RWMutex
	cacheTTL     time.Duration
	lastRefresh  time.Time
}

// NewEngineRegistry 创建引擎注册表
func NewEngineRegistry(systemURL, internalAPIKey string, cacheTTL time.Duration) *EngineRegistry {
	if cacheTTL == 0 {
		cacheTTL = 5 * time.Minute // 默认缓存 5 分钟
	}

	return &EngineRegistry{
		systemClient: commonClient.NewSystemClientWithInternalKey(systemURL, internalAPIKey),
		engines:      make(map[string]*commonModels.Engine),
		cacheTTL:     cacheTTL,
	}
}

// GetEngine 根据 engine_type 获取引擎配置
func (r *EngineRegistry) GetEngine(ctx context.Context, identifier string) (*commonModels.Engine, error) {
	// 先检查缓存
	r.mu.RLock()
	if engine, ok := r.engines[identifier]; ok && time.Since(r.lastRefresh) < r.cacheTTL {
		r.mu.RUnlock()
		return engine, nil
	}
	r.mu.RUnlock()

	// 缓存未命中或过期，从 System 查询
	engine, err := r.systemClient.GetCapabilityByIdentifier(identifier)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine %s: %w", identifier, err)
	}

	// 更新缓存
	r.mu.Lock()
	r.engines[identifier] = engine
	r.mu.Unlock()

	return engine, nil
}

// RefreshCache 刷新所有引擎缓存
func (r *EngineRegistry) RefreshCache(ctx context.Context) error {
	// 从 System 查询所有计算引擎
	filters := map[string]string{
		"resource_type": "compute_engine",
		"is_active":     "true",
	}

	engines, err := r.systemClient.ListCapabilities(filters)
	if err != nil {
		return fmt.Errorf("failed to list capabilities: %w", err)
	}

	// 更新缓存
	r.mu.Lock()
	defer r.mu.Unlock()

	// 清空现有缓存
	r.engines = make(map[string]*commonModels.Engine)

	// 重新填充
	for _, engine := range engines {
		r.engines[engine.EngineType] = engine
	}

	r.lastRefresh = time.Now()
	return nil
}

// ListAllEngines 列出所有已注册的引擎
func (r *EngineRegistry) ListAllEngines(ctx context.Context) ([]*commonModels.Engine, error) {
	// 检查缓存是否有效
	r.mu.RLock()
	cacheValid := time.Since(r.lastRefresh) < r.cacheTTL
	r.mu.RUnlock()

	if !cacheValid {
		// 刷新缓存
		if err := r.RefreshCache(ctx); err != nil {
			return nil, fmt.Errorf("failed to refresh engine cache: %w", err)
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	engines := make([]*commonModels.Engine, 0, len(r.engines))
	for _, engine := range r.engines {
		engines = append(engines, engine)
	}

	return engines, nil
}

// unmarshalCapability 辅助函数：解析 Capabilities JSON 字符串
func unmarshalCapability(capabilitiesJSON *string, result *commonModels.Capability) error {
	if capabilitiesJSON == nil {
		return fmt.Errorf("capabilities is nil")
	}

	if err := json.Unmarshal([]byte(*capabilitiesJSON), result); err != nil {
		return fmt.Errorf("failed to unmarshal capabilities: %w", err)
	}

	return nil
}
