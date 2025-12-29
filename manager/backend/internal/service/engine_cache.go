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

// engineCacheEntry 缓存条目，包含引擎和过期时间
type engineCacheEntry struct {
	engine    *commonModels.Engine
	expiresAt time.Time
}

// EngineCacheService 引擎缓存服务
type EngineCacheService struct {
	systemURL       string
	internalClient  *commonClient.SystemClient
	cacheMu         sync.RWMutex
	engineCache     map[uint]*engineCacheEntry
	cacheTTL        time.Duration // 缓存生存时间，默认 3 分钟
	log             *slog.Logger
	eventSubscriber *events.EngineEventSubscriber
}

// NewEngineCacheService 创建引擎缓存服务
func NewEngineCacheService(systemURL, internalKey string, redisClient *redis.Client) *EngineCacheService {
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

	service := &EngineCacheService{
		systemURL:   systemURL,
		engineCache: make(map[uint]*engineCacheEntry),
		cacheTTL:    3 * time.Minute, // Manager 使用 3 分钟 TTL
		log:           logger.With("component", "engine_cache_service"),
	}

	if internalKey != "" {
		service.internalClient = commonClient.NewSystemClientWithInternalKey(systemURL, internalKey)
	}

	// 初始化 Redis 事件订阅器
	if redisClient != nil {
		service.eventSubscriber = events.NewEngineEventSubscriber(
			redisClient,
			service.handleEngineChangeEvent,
			service.log,
		)
		// 启动订阅（在后台 goroutine 中）
		go func() {
			if err := service.eventSubscriber.Start(); err != nil {
				service.log.Error("引擎事件订阅器启动失败", "error", err)
			}
		}()
		service.log.Info("引擎事件订阅器已启动")
	} else {
		service.log.Warn("Redis 未配置，引擎变更事件同步功能将被禁用")
	}

	return service
}

// GetEngine 获取引擎（带缓存）
func (s *EngineCacheService) GetEngine(engineID uint) (*commonModels.Engine, error) {
	if s.internalClient == nil {
		return nil, fmt.Errorf("internal client not configured")
	}

	// 检查缓存
	s.cacheMu.RLock()
	entry, ok := s.engineCache[engineID]
	s.cacheMu.RUnlock()

	// 如果缓存命中且未过期，返回缓存数据
	if ok && entry != nil && entry.engine != nil && time.Now().Before(entry.expiresAt) {
		engineCopy := *entry.engine
		s.log.Debug("引擎连接信息命中缓存",
			"engine_id", engineID,
			"expires_in_seconds", int(time.Until(entry.expiresAt).Seconds()),
		)
		return &engineCopy, nil
	}

	// 缓存未命中或已过期，从 System API 获取
	engine, err := s.internalClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine from System API: %w", err)
	}

	// 更新缓存
	s.cacheEngine(engine)
	s.log.Info("通过内部 API 获取引擎连接信息成功",
		"engine_id", engineID,
		"engine_type", engine.EngineType,
	)

	return engine, nil
}

// cacheEngine 缓存引擎
func (s *EngineCacheService) cacheEngine(engine *commonModels.Engine) {
	if engine == nil {
		return
	}
	engineCopy := *engine
	s.cacheMu.Lock()
	s.engineCache[engineCopy.ID] = &engineCacheEntry{
		engine:    &engineCopy,
		expiresAt: time.Now().Add(s.cacheTTL),
	}
	s.cacheMu.Unlock()
}

// ClearCache 清除所有引擎缓存
func (s *EngineCacheService) ClearCache() {
	s.cacheMu.Lock()
	s.engineCache = make(map[uint]*engineCacheEntry)
	s.cacheMu.Unlock()
	s.log.Info("引擎缓存已清除")
}

// ClearEngineCache 清除指定引擎的缓存
func (s *EngineCacheService) ClearEngineCache(engineID uint) {
	s.cacheMu.Lock()
	delete(s.engineCache, engineID)
	s.cacheMu.Unlock()
	s.log.Info("引擎缓存已清除", "engine_id", engineID)
}

// handleEngineChangeEvent 处理引擎变更事件（Redis 订阅回调）
func (s *EngineCacheService) handleEngineChangeEvent(event events.EngineChangeEvent) error {
	s.log.Info("收到引擎变更事件",
		"engine_id", event.EngineID,
		"action", event.Action,
		"timestamp", event.Timestamp)

	switch event.Action {
	case events.ActionCreate:
		// 引擎创建：不需要特殊处理，等待下次访问时自动加载
		s.log.Debug("引擎已创建，等待首次访问时加载", "engine_id", event.EngineID)

	case events.ActionUpdate:
		// 引擎更新：清除缓存，强制下次访问时重新获取
		s.ClearEngineCache(event.EngineID)
		s.log.Info("引擎已更新，缓存已清除", "engine_id", event.EngineID)

	case events.ActionDelete:
		// 引擎删除：清除缓存
		s.ClearEngineCache(event.EngineID)
		s.log.Info("引擎已删除，缓存已清除", "engine_id", event.EngineID)

	default:
		s.log.Warn("未知的引擎变更动作", "action", event.Action, "engine_id", event.EngineID)
	}

	return nil
}

// Stop 停止引擎缓存服务（清理资源）
func (s *EngineCacheService) Stop() {
	if s.eventSubscriber != nil {
		s.eventSubscriber.Stop()
		s.log.Info("引擎事件订阅器已停止")
	}
}
