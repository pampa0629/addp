package internal

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/addp/common/models"
	"github.com/addp/gateway/internal/proxy"
)

// ModuleDiscovery 模块发现管理器
type ModuleDiscovery struct {
	systemClient moduleWatcher
	backendPools map[string][]moduleBackend
	nextBackend  map[string]uint64
	revision     int64
	hasSnapshot  bool
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	now          func() time.Time
}

type moduleBackend struct {
	endpointID string
	moduleURL  string
	expiresAt  time.Time
	proxy      *proxy.ServiceProxy
}

type moduleWatcher interface {
	WatchModules(ctx context.Context, revision int64, wait time.Duration) (*models.ModuleRoutingSnapshot, error)
}

// NewModuleDiscovery 创建模块发现管理器
func NewModuleDiscovery(systemClient moduleWatcher) *ModuleDiscovery {
	ctx, cancel := context.WithCancel(context.Background())

	return &ModuleDiscovery{
		systemClient: systemClient,
		backendPools: make(map[string][]moduleBackend),
		nextBackend:  make(map[string]uint64),
		ctx:          ctx,
		cancel:       cancel,
		now:          time.Now,
	}
}

// Start 启动模块发现：先获取完整快照，再持续通过 revision 长轮询收敛。
func (md *ModuleDiscovery) Start(watchTimeout time.Duration) error {
	if watchTimeout <= 0 {
		watchTimeout = 10 * time.Second
	}
	initialErr := md.refreshModules(0)
	go md.watchLoop(watchTimeout)

	if initialErr != nil {
		return fmt.Errorf("初始化模块列表失败: %w", initialErr)
	}
	return nil
}

// Stop 停止模块发现
func (md *ModuleDiscovery) Stop() {
	md.cancel()
}

func (md *ModuleDiscovery) watchLoop(watchTimeout time.Duration) {
	for {
		if err := md.refreshModules(watchTimeout); err != nil {
			if md.ctx.Err() != nil {
				return
			}
			fmt.Printf("监听模块路由快照失败: %v\n", err)
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-md.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

// refreshModules 从 System 获取完整路由快照并原子更新代理。
func (md *ModuleDiscovery) refreshModules(wait time.Duration) error {
	md.mu.RLock()
	revision := md.revision
	now := md.now()
	for _, pool := range md.backendPools {
		for _, ep := range pool {
			if remaining := ep.expiresAt.Sub(now); remaining > 0 {
				wait = min(wait, remaining/2)
			}
		}
	}
	md.mu.RUnlock()
	// Limit the entire exchange, including dial and token acquisition, so one
	// stalled connection cannot block renewal through a surviving System replica.
	ctx, cancel := context.WithTimeout(md.ctx, wait+2*time.Second)
	defer cancel()
	snapshot, err := md.systemClient.WatchModules(ctx, revision, wait)
	if err != nil {
		return err
	}
	if snapshot == nil {
		return fmt.Errorf("System 返回空模块路由快照")
	}
	endpoints := snapshot.Endpoints

	md.mu.Lock()
	defer md.mu.Unlock()

	if md.hasSnapshot && snapshot.Revision < md.revision {
		return fmt.Errorf("路由快照 revision %d 早于已应用的 %d", snapshot.Revision, md.revision)
	}

	// 更新模块列表
	newBackendPools := make(map[string][]moduleBackend)
	now = md.now()
	existing := make(map[string]map[string]moduleBackend)
	for _, e := range endpoints {
		if !e.ExpiresAt.After(now) {
			continue
		}
		if existing[e.ModuleName] == nil {
			existing[e.ModuleName] = indexModuleBackends(md.backendPools[e.ModuleName])
		}
		backend := existing[e.ModuleName][e.EndpointID]
		if backend.proxy == nil || backend.moduleURL != e.URL {
			backend = moduleBackend{endpointID: e.EndpointID, moduleURL: e.URL, proxy: proxy.NewServiceProxy(e.URL)}
		}
		backend.expiresAt = e.ExpiresAt
		newBackendPools[e.ModuleName] = append(newBackendPools[e.ModuleName], backend)
	}
	for _, pool := range newBackendPools {
		sort.Slice(pool, func(i, j int) bool { return pool[i].endpointID < pool[j].endpointID })
	}

	for moduleName := range md.nextBackend {
		if _, exists := newBackendPools[moduleName]; !exists {
			delete(md.nextBackend, moduleName)
		}
	}

	md.backendPools = newBackendPools
	md.revision = snapshot.Revision
	md.hasSnapshot = true

	fmt.Printf("模块路由快照已应用: revision=%d, active_modules=%d\n", md.revision, len(md.backendPools))
	return nil
}

func (md *ModuleDiscovery) HasSnapshot() bool {
	md.mu.RLock()
	defer md.mu.RUnlock()
	return md.hasSnapshot
}

func indexModuleBackends(backends []moduleBackend) map[string]moduleBackend {
	result := make(map[string]moduleBackend, len(backends))
	for _, backend := range backends {
		result[backend.endpointID] = backend
	}
	return result
}

// GetProxy 从当前有效 Backend 池轮询选择代理；不在 Gateway 内重放失败请求。
func (md *ModuleDiscovery) GetProxy(moduleName string) (*proxy.ServiceProxy, error) {
	md.mu.Lock()
	defer md.mu.Unlock()

	pool := md.backendPools[moduleName]
	if len(pool) == 0 {
		return nil, fmt.Errorf("模块 %s 不可用或未注册", moduleName)
	}
	start := md.nextBackend[moduleName] % uint64(len(pool))
	for offset := uint64(0); offset < uint64(len(pool)); offset++ {
		index := (start + offset) % uint64(len(pool))
		backend := pool[index]
		if backend.proxy == nil || !backend.expiresAt.After(md.now()) {
			continue
		}
		md.nextBackend[moduleName] = index + 1
		return backend.proxy, nil
	}
	return nil, fmt.Errorf("模块 %s 没有有效 Backend 租约", moduleName)
}

// RouteAddresses reports one currently usable address per module for the Gateway landing page.
func (md *ModuleDiscovery) RouteAddresses() map[string]string {
	md.mu.RLock()
	defer md.mu.RUnlock()
	result := make(map[string]string, len(md.backendPools))
	now := md.now()
	for name, pool := range md.backendPools {
		for _, backend := range pool {
			if backend.expiresAt.After(now) {
				result[name] = backend.moduleURL
				break
			}
		}
	}
	return result
}
