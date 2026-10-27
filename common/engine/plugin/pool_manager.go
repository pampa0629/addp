package plugin

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

// PoolManager 按引擎配置缓存 SQL 连接池，拥有底层连接池的关闭责任。
// 同一引擎的创建、替换与关闭串行执行，不阻塞其他引擎的连接建立。
type PoolManager struct {
	mu      sync.RWMutex
	pools   map[uint]*ManagedPool
	pending map[uint]chan struct{}
	closing chan struct{}
}

// ManagedPool 托管的连接池。字段仅在 PoolManager 锁下访问。
type ManagedPool struct {
	DB         interface{} // *gorm.DB
	EngineID   uint
	LastAccess time.Time
	CreateTime time.Time
	signature  [sha256.Size]byte
}

var globalPoolManager = &PoolManager{}

// acquire 串行化单个引擎的生命周期操作；CloseAll 期间不接纳新操作。
func (pm *PoolManager) acquire(id uint) func() {
	for {
		pm.mu.Lock()
		wait := pm.closing
		if wait == nil {
			wait = pm.pending[id]
		}
		if wait != nil {
			pm.mu.Unlock()
			<-wait
			continue
		}
		if pm.pending == nil {
			pm.pending = make(map[uint]chan struct{})
		}
		done := make(chan struct{})
		pm.pending[id] = done
		pm.mu.Unlock()
		return func() {
			pm.mu.Lock()
			delete(pm.pending, id)
			close(done)
			pm.mu.Unlock()
		}
	}
}

// GetOrCreatePool 仅复用类型、完整连接配置和池参数都一致的连接池。
// ConnMaxLifetime 由 database/sql 管理，不用于连接池缓存淘汰。
func (pm *PoolManager) GetOrCreatePool(resource *Engine, config *PoolConfig) (interface{}, error) {
	if resource == nil {
		return nil, errors.New("engine cannot be nil")
	}
	if config == nil {
		config = DefaultPoolConfig()
	}
	settings := *config
	encoded, err := json.Marshal(struct {
		EngineType     string
		ConnectionInfo ConnectionInfo
		Config         PoolConfig
	}{resource.EngineType, resource.ConnectionInfo, settings})
	if err != nil {
		return nil, errors.New("cannot encode connection pool configuration")
	}
	signature := sha256.Sum256(encoded)
	release := pm.acquire(resource.ID)
	defer release()

	pm.mu.Lock()
	pool := pm.pools[resource.ID]
	if pool != nil && pool.signature == signature {
		pool.LastAccess = time.Now()
		pm.mu.Unlock()
		return pool.DB, nil
	}
	delete(pm.pools, resource.ID)
	pm.mu.Unlock()
	if pool != nil {
		if err := closeManagedPool(pool.DB); err != nil {
			return nil, fmt.Errorf("close replaced connection pool: %w", err)
		}
	}

	db, err := CreateConnectionPoolDirect(resource, &settings)
	if err != nil {
		if db != nil {
			err = errors.Join(err, closeManagedPool(db))
		}
		return nil, err
	}
	if db == nil || db.Config == nil {
		return nil, gorm.ErrInvalidDB
	}
	if native, err := db.DB(); err != nil || native == nil {
		if err != nil {
			return nil, err
		}
		return nil, gorm.ErrInvalidDB
	}
	now := time.Now()
	pm.mu.Lock()
	if pm.pools == nil {
		pm.pools = make(map[uint]*ManagedPool)
	}
	pm.pools[resource.ID] = &ManagedPool{
		DB:         db,
		EngineID:   resource.ID,
		LastAccess: now,
		CreateTime: now,
		signature:  signature,
	}
	pm.mu.Unlock()
	return db, nil
}

func closeManagedPool(value interface{}) error {
	db, ok := value.(*gorm.DB)
	if !ok || db == nil || db.Config == nil {
		return gorm.ErrInvalidDB
	}
	native, err := db.DB()
	if err != nil {
		return err
	}
	if native == nil {
		return gorm.ErrInvalidDB
	}
	return native.Close()
}

// ClosePool 等待该引擎的创建或替换完成，移除缓存并关闭底层 sql.DB。
func (pm *PoolManager) ClosePool(engineID uint) error {
	release := pm.acquire(engineID)
	defer release()
	pm.mu.Lock()
	pool := pm.pools[engineID]
	delete(pm.pools, engineID)
	pm.mu.Unlock()
	if pool == nil {
		return nil
	}
	return closeManagedPool(pool.DB)
}

// CloseAll 阻止新建池，等待已开始的生命周期操作，然后关闭全部底层池。
// 返回后新请求可以重新创建连接池。
func (pm *PoolManager) CloseAll() {
	pm.mu.Lock()
	if pm.closing != nil {
		done := pm.closing
		pm.mu.Unlock()
		<-done
		return
	}
	done := make(chan struct{})
	pm.closing = done
	pending := make([]chan struct{}, 0, len(pm.pending))
	for _, wait := range pm.pending {
		pending = append(pending, wait)
	}
	pm.mu.Unlock()
	for _, wait := range pending {
		<-wait
	}
	pm.mu.Lock()
	pools := pm.pools
	pm.pools = nil
	pm.mu.Unlock()
	for _, pool := range pools {
		_ = closeManagedPool(pool.DB)
	}
	pm.mu.Lock()
	pm.closing = nil
	close(done)
	pm.mu.Unlock()
}

// GetPoolStats 获取连接池统计信息（用于监控）
func (pm *PoolManager) GetPoolStats() map[uint]PoolStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	stats := make(map[uint]PoolStats)
	for id, pool := range pm.pools {
		stats[id] = PoolStats{
			EngineID:   pool.EngineID,
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
	EngineID   uint
	CreateTime time.Time
	LastAccess time.Time
	Age        time.Duration // 连接池存在时长
	IdleTime   time.Duration // 空闲时长
}

// === 公开的API ===

// GetOrCreatePool 全局函数：获取或创建连接池
// 这是推荐的获取连接池的方式，会自动管理连接池的生命周期
func GetOrCreatePool(resource *Engine, config *PoolConfig) (interface{}, error) {
	if config == nil {
		config = DefaultPoolConfig()
	}
	return globalPoolManager.GetOrCreatePool(resource, config)
}

// ClosePool 全局函数：关闭指定引擎的连接池
// 通常在引擎被删除或更新时调用
func ClosePool(engineID uint) error {
	return globalPoolManager.ClosePool(engineID)
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

// CreateConnectionPool 直接创建连接池，不加入缓存，由调用方负责关闭。
// 需要托管和复用时使用 GetOrCreatePool。
func CreateConnectionPool(resource *Engine, config *PoolConfig) (interface{}, error) {
	return CreateConnectionPoolDirect(resource, config)
}
