package service

import (
	"os"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

// engineCacheEntry 缓存条目，包含 engine 和过期时间。
type engineCacheEntry struct {
	resource  *commonModels.Engine
	expiresAt time.Time
}

// ensureInternalClient 尝试按需初始化内部客户端（用于本地脚本未显式传入密钥的情况）。
func (s *EngineService) ensureInternalClient() {
	if s.internalClient != nil {
		return
	}

	if key := os.Getenv("INTERNAL_API_KEY"); key != "" {
		s.internalClient = commonClient.NewSystemClientWithInternalKey(s.systemURL, key)
	}
}

func (s *EngineService) cacheEngine(resource *commonModels.Engine) {
	if resource == nil {
		return
	}
	resourceCopy := *resource
	s.cacheMu.Lock()
	s.engineCache[resourceCopy.ID] = &engineCacheEntry{
		resource:  &resourceCopy,
		expiresAt: time.Now().Add(s.cacheTTL),
	}
	s.cacheMu.Unlock()
}

// ClearCache 清除所有引擎缓存。
func (s *EngineService) ClearCache() {
	s.cacheMu.Lock()
	s.engineCache = make(map[uint]*engineCacheEntry)
	s.cacheMu.Unlock()
	s.log.Info("引擎缓存已清除")
}

// ClearEngineCache 清除指定资源的缓存。
func (s *EngineService) ClearEngineCache(engineID uint) {
	s.cacheMu.Lock()
	delete(s.engineCache, engineID)
	s.cacheMu.Unlock()
	s.log.Info("引擎缓存已清除", "engine_id", engineID)
}

func (s *EngineService) snapshotCache() map[uint]*commonModels.Engine {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	if len(s.engineCache) == 0 {
		return nil
	}

	now := time.Now()
	result := make(map[uint]*commonModels.Engine, len(s.engineCache))
	for id, entry := range s.engineCache {
		if entry == nil || entry.resource == nil {
			continue
		}
		if now.After(entry.expiresAt) {
			continue
		}
		resourceCopy := *entry.resource
		result[id] = &resourceCopy
	}
	return result
}
