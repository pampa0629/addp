package service

import (
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/events"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/redis/go-redis/v9"
)

// resourceCacheEntry 缓存条目，包含资源和过期时间
type resourceCacheEntry struct {
	resource  *commonModels.Engine
	expiresAt time.Time
}

// ResourceCacheService 资源缓存服务
type ResourceCacheService struct {
	systemURL       string
	internalClient  *commonClient.SystemClient
	cacheMu         sync.RWMutex
	resourceCache   map[uint]*resourceCacheEntry
	cacheTTL        time.Duration // 缓存生存时间，默认 3 分钟
	log             *slog.Logger
	eventSubscriber *events.ResourceEventSubscriber
}

// NewResourceCacheService 创建资源缓存服务
func NewResourceCacheService(systemURL, internalKey string, redisClient *redis.Client) *ResourceCacheService {
	// 默认从环境变量读取
	if systemURL == "" {
		systemURL = os.Getenv("SYSTEM_SERVICE_URL")
		if systemURL == "" {
			systemURL = "http://localhost:8080"
		}
	}
	if internalKey == "" {
		internalKey = os.Getenv("INTERNAL_API_KEY")
	}

	service := &ResourceCacheService{
		systemURL:     systemURL,
		resourceCache: make(map[uint]*resourceCacheEntry),
		cacheTTL:      3 * time.Minute, // Manager 使用 3 分钟 TTL
		log:           logger.With("component", "resource_cache_service"),
	}

	if internalKey != "" {
		service.internalClient = commonClient.NewSystemClientWithInternalKey(systemURL, internalKey)
	}

	// 初始化 Redis 事件订阅器
	if redisClient != nil {
		service.eventSubscriber = events.NewResourceEventSubscriber(
			redisClient,
			service.handleResourceChangeEvent,
			service.log,
		)
		// 启动订阅（在后台 goroutine 中）
		go func() {
			if err := service.eventSubscriber.Start(); err != nil {
				service.log.Error("资源事件订阅器启动失败", "error", err)
			}
		}()
		service.log.Info("资源事件订阅器已启动")
	} else {
		service.log.Warn("Redis 未配置，资源变更事件同步功能将被禁用")
	}

	return service
}

// GetResource 获取资源（带缓存）
func (s *ResourceCacheService) GetResource(engineID uint) (*commonModels.Engine, error) {
	if s.internalClient == nil {
		return nil, fmt.Errorf("internal client not configured")
	}

	// 检查缓存
	s.cacheMu.RLock()
	entry, ok := s.resourceCache[resourceID]
	s.cacheMu.RUnlock()

	// 如果缓存命中且未过期，返回缓存数据
	if ok && entry != nil && entry.resource != nil && time.Now().Before(entry.expiresAt) {
		resourceCopy := *entry.resource
		s.log.Debug("资源连接信息命中缓存",
			"engine_id", engineID,
			"expires_in_seconds", int(time.Until(entry.expiresAt).Seconds()),
		)
		return &resourceCopy, nil
	}

	// 缓存未命中或已过期，从 System API 获取
	resource, err := s.internalClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource from System API: %w", err)
	}

	// 更新缓存
	s.cacheResource(resource)
	s.log.Info("通过内部 API 获取资源连接信息成功",
		"engine_id", engineID,
		"resource_type", resource.EngineType,
	)

	return resource, nil
}

// cacheResource 缓存资源
func (s *ResourceCacheService) cacheResource(resource *commonModels.Engine) {
	if resource == nil {
		return
	}
	resourceCopy := *resource
	s.cacheMu.Lock()
	s.resourceCache[resourceCopy.ID] = &resourceCacheEntry{
		resource:  &resourceCopy,
		expiresAt: time.Now().Add(s.cacheTTL),
	}
	s.cacheMu.Unlock()
}

// ClearCache 清除所有资源缓存
func (s *ResourceCacheService) ClearCache() {
	s.cacheMu.Lock()
	s.resourceCache = make(map[uint]*resourceCacheEntry)
	s.cacheMu.Unlock()
	s.log.Info("资源缓存已清除")
}

// ClearResourceCache 清除指定资源的缓存
func (s *ResourceCacheService) ClearResourceCache(engineID uint) {
	s.cacheMu.Lock()
	delete(s.resourceCache, engineID)
	s.cacheMu.Unlock()
	s.log.Info("资源缓存已清除", "engine_id", engineID)
}

// handleResourceChangeEvent 处理资源变更事件（Redis 订阅回调）
func (s *ResourceCacheService) handleResourceChangeEvent(event events.ResourceChangeEvent) error {
	s.log.Info("收到资源变更事件",
		"engine_id", event.EngineID,
		"action", event.Action,
		"timestamp", event.Timestamp)

	switch event.Action {
	case events.ActionCreate:
		// 资源创建：不需要特殊处理，等待下次访问时自动加载
		s.log.Debug("资源已创建，等待首次访问时加载", "engine_id", event.EngineID)

	case events.ActionUpdate:
		// 资源更新：清除缓存，强制下次访问时重新获取
		s.ClearResourceCache(event.EngineID)
		s.log.Info("资源已更新，缓存已清除", "engine_id", event.EngineID)

	case events.ActionDelete:
		// 资源删除：清除缓存
		s.ClearResourceCache(event.EngineID)
		s.log.Info("资源已删除，缓存已清除", "engine_id", event.EngineID)

	default:
		s.log.Warn("未知的资源变更动作", "action", event.Action, "engine_id", event.EngineID)
	}

	return nil
}

// Stop 停止资源缓存服务（清理资源）
func (s *ResourceCacheService) Stop() {
	if s.eventSubscriber != nil {
		s.eventSubscriber.Stop()
		s.log.Info("资源事件订阅器已停止")
	}
}
