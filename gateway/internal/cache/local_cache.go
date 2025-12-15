package cache

import (
	"sync"
	"time"

	"github.com/addp/gateway/pkg/client"
)

// CacheItem 缓存项
type CacheItem struct {
	Data      *client.APIKeyValidationResponse
	ExpiresAt time.Time
}

// LocalCache 本地内存缓存（用于 API Key 验证结果）
type LocalCache struct {
	items map[string]*CacheItem
	mu    sync.RWMutex
	ttl   time.Duration
}

// NewLocalCache 创建本地缓存
// ttl: 缓存过期时间（推荐 5 分钟）
func NewLocalCache(ttl time.Duration) *LocalCache {
	cache := &LocalCache{
		items: make(map[string]*CacheItem),
		ttl:   ttl,
	}

	// 启动后台清理goroutine
	go cache.startCleanup()

	return cache
}

// Get 获取缓存
// key: API Key 的 hash
func (c *LocalCache) Get(key string) *client.APIKeyValidationResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, exists := c.items[key]
	if !exists {
		return nil
	}

	// 检查是否过期
	if time.Now().After(item.ExpiresAt) {
		return nil
	}

	return item.Data
}

// Set 设置缓存
func (c *LocalCache) Set(key string, data *client.APIKeyValidationResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = &CacheItem{
		Data:      data,
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

// Delete 删除缓存项（用于 Key 撤销时主动清理）
func (c *LocalCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.items, key)
}

// Clear 清空所有缓存
func (c *LocalCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*CacheItem)
}

// Size 获取缓存项数量
func (c *LocalCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.items)
}

// startCleanup 启动后台清理任务（每分钟清理一次过期项）
func (c *LocalCache) startCleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanup()
	}
}

// cleanup 清理过期项
func (c *LocalCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, item := range c.items {
		if now.After(item.ExpiresAt) {
			delete(c.items, key)
		}
	}
}
