package plugin

import (
	"sync"
	"time"
)

// PoolManager 全局连接池管理器
// 用于缓存和复用连接池，避免重复创建
type PoolManager struct {
	mu    sync.RWMutex
	pools map[uint]*ManagedPool // 资源ID -> 连接池
}

// ManagedPool 托管的连接池
type ManagedPool struct {
	DB         interface{} // 数据库连接（*gorm.DB）
	ResourceID uint
	LastAccess time.Time
	CreateTime time.Time
}

var globalPoolManager = &PoolManager{
	pools: make(map[uint]*ManagedPool),
}

// GetOrCreatePool 获取或创建连接池
// 如果连接池已存在且未过期，返回缓存的连接池
// 否则创建新连接池
func (pm *PoolManager) GetOrCreatePool(resource *Resource, config *PoolConfig) (interface{}, error) {
	pm.mu.RLock()
	pool, exists := pm.pools[resource.ID]
	pm.mu.RUnlock()

	if exists {
		// 检查连接池是否过期（超过配置的生命周期未使用）
		if time.Since(pool.LastAccess) < config.ConnMaxLifetime {
			pool.LastAccess = time.Now()
			return pool.DB, nil
		}

		// 过期，关闭旧连接池
		pm.ClosePool(resource.ID)
	}

	// 创建新连接池
	db, err := CreateConnectionPool(resource, config)
	if err != nil {
		return nil, err
	}

	pm.mu.Lock()
	pm.pools[resource.ID] = &ManagedPool{
		DB:         db,
		ResourceID: resource.ID,
		LastAccess: time.Now(),
		CreateTime: time.Now(),
	}
	pm.mu.Unlock()

	return db, nil
}

// ClosePool 关闭指定资源的连接池
func (pm *PoolManager) ClosePool(resourceID uint) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pool, exists := pm.pools[resourceID]
	if !exists {
		return nil
	}

	// 关闭GORM连接
	if db, ok := pool.DB.(interface {
		DB() (*interface{}, error)
	}); ok {
		if sqlDB, err := db.DB(); err == nil {
			if closer, ok := (*sqlDB).(interface{ Close() error }); ok {
				closer.Close()
			}
		}
	}

	delete(pm.pools, resourceID)
	return nil
}

// CloseAll 关闭所有连接池（用于优雅关闭）
func (pm *PoolManager) CloseAll() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	for id, pool := range pm.pools {
		// 关闭GORM连接
		if db, ok := pool.DB.(interface {
			DB() (*interface{}, error)
		}); ok {
			if sqlDB, err := db.DB(); err == nil {
				if closer, ok := (*sqlDB).(interface{ Close() error }); ok {
					closer.Close()
				}
			}
		}
		delete(pm.pools, id)
	}
}

// GetPoolStats 获取连接池统计信息（用于监控）
func (pm *PoolManager) GetPoolStats() map[uint]PoolStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	stats := make(map[uint]PoolStats)
	for id, pool := range pm.pools {
		stats[id] = PoolStats{
			ResourceID: pool.ResourceID,
			CreateTime: pool.CreateTime,
			LastAccess: pool.LastAccess,
			Age:        time.Since(pool.CreateTime),
			IdleTime:   time.Since(pool.LastAccess),
		}
	}
	return stats
}

// PoolStats 连接池统计信息
type PoolStats struct {
	ResourceID uint
	CreateTime time.Time
	LastAccess time.Time
	Age        time.Duration // 连接池存在时长
	IdleTime   time.Duration // 空闲时长
}

// === 公开的API ===

// GetOrCreatePool 全局函数：获取或创建连接池
// 这是推荐的获取连接池的方式，会自动管理连接池的生命周期
func GetOrCreatePool(resource *Resource, config *PoolConfig) (interface{}, error) {
	if config == nil {
		config = DefaultPoolConfig()
	}
	return globalPoolManager.GetOrCreatePool(resource, config)
}

// ClosePool 全局函数：关闭指定资源的连接池
// 通常在资源被删除或更新时调用
func ClosePool(resourceID uint) error {
	return globalPoolManager.ClosePool(resourceID)
}

// CloseAllPools 全局函数：关闭所有连接池
// 在应用关闭时调用，确保优雅关闭
func CloseAllPools() {
	globalPoolManager.CloseAll()
}

// GetPoolStats 全局函数：获取所有连接池的统计信息
func GetPoolStats() map[uint]PoolStats {
	return globalPoolManager.GetPoolStats()
}

// CreateConnectionPool 创建连接池（内部使用）
// 通过插件系统创建对应类型的连接池
// 注意：这个方法返回 interface{} 以便在 PoolManager 内部使用
func CreateConnectionPool(resource *Resource, config *PoolConfig) (interface{}, error) {
	// 调用 factory.go 中的 CreateConnectionPoolDirect
	// 这里需要注意类型转换
	return CreateConnectionPoolDirect(resource, config)
}
