package service

import (
	"time"

	commonModels "github.com/addp/common/models"
)

type engineCacheKey struct {
	tenantID uint
	engineID uint
}

type engineCacheEntry struct {
	resource  *commonModels.Engine
	expiresAt time.Time
}

func (s *EngineService) cacheEngine(tenantID uint, resource *commonModels.Engine) {
	if tenantID == 0 || resource == nil {
		return
	}
	resourceCopy := *resource
	s.cacheMu.Lock()
	s.engineCache[engineCacheKey{tenantID: tenantID, engineID: resourceCopy.ID}] = &engineCacheEntry{
		resource: &resourceCopy, expiresAt: time.Now().Add(s.cacheTTL),
	}
	s.cacheMu.Unlock()
}

func (s *EngineService) ClearCache() {
	s.cacheMu.Lock()
	s.engineCache = make(map[engineCacheKey]*engineCacheEntry)
	s.cacheMu.Unlock()
	s.log.Info("引擎缓存已清除")
}

// ClearEngineCache removes every tenant-keyed entry for an engine. Engine
// change events intentionally do not carry Tenant authorization facts.
func (s *EngineService) ClearEngineCache(engineID uint) {
	s.cacheMu.Lock()
	for key := range s.engineCache {
		if key.engineID == engineID {
			delete(s.engineCache, key)
		}
	}
	s.cacheMu.Unlock()
	s.log.Info("引擎缓存已清除", "engine_id", engineID)
}
