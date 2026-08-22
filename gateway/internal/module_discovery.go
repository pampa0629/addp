package internal

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/gateway/internal/proxy"
)

// ModuleDiscovery 模块发现管理器
type ModuleDiscovery struct {
	systemClient  moduleLister
	modules       map[string]*client.ModuleInfo // moduleName -> ModuleInfo
	backendPools  map[string][]moduleBackend
	nextBackend   map[string]uint64
	mu            sync.RWMutex
	refreshTicker *time.Ticker
	ctx           context.Context
	cancel        context.CancelFunc
	now           func() time.Time
}

type moduleBackend struct {
	instanceID     string
	moduleURL      string
	leaseExpiresAt time.Time
	proxy          *proxy.ServiceProxy
}

type moduleLister interface {
	GetModules() ([]*client.ModuleInfo, error)
}

// NewModuleDiscovery 创建模块发现管理器
func NewModuleDiscovery(systemClient moduleLister) *ModuleDiscovery {
	ctx, cancel := context.WithCancel(context.Background())

	return &ModuleDiscovery{
		systemClient: systemClient,
		modules:      make(map[string]*client.ModuleInfo),
		backendPools: make(map[string][]moduleBackend),
		nextBackend:  make(map[string]uint64),
		ctx:          ctx,
		cancel:       cancel,
		now:          time.Now,
	}
}

// Start 启动模块发现（加载模块列表并定期刷新）
func (md *ModuleDiscovery) Start(refreshInterval time.Duration) error {
	initialErr := md.refreshModules()
	md.refreshTicker = time.NewTicker(refreshInterval)
	go func() {
		for {
			select {
			case <-md.ctx.Done():
				return
			case <-md.refreshTicker.C:
				if err := md.refreshModules(); err != nil {
					// 日志记录错误，但不中断服务
					fmt.Printf("刷新模块列表失败: %v\n", err)
				}
			}
		}
	}()

	if initialErr != nil {
		return fmt.Errorf("初始化模块列表失败: %w", initialErr)
	}
	return nil
}

// Stop 停止模块发现
func (md *ModuleDiscovery) Stop() {
	if md.refreshTicker != nil {
		md.refreshTicker.Stop()
	}
	md.cancel()
}

// refreshModules 从 System 获取模块列表并更新代理
func (md *ModuleDiscovery) refreshModules() error {
	// 获取所有活跃模块
	modules, err := md.systemClient.GetModules()
	if err != nil {
		return err
	}

	md.mu.Lock()
	defer md.mu.Unlock()

	// 更新模块列表
	newModules := make(map[string]*client.ModuleInfo)
	newBackendPools := make(map[string][]moduleBackend)
	for _, mod := range modules {
		instances := selectRoutableBackends(mod, md.now())
		if len(instances) == 0 {
			continue
		}
		newModules[mod.ModuleName] = mod
		existing := indexModuleBackends(md.backendPools[mod.ModuleName])
		pool := make([]moduleBackend, 0, len(instances))
		for _, instance := range instances {
			backend := moduleBackend{
				instanceID: instance.InstanceID, moduleURL: instance.ModuleURL,
				leaseExpiresAt: instance.LeaseExpiresAt,
			}
			if previous, ok := existing[instance.InstanceID]; ok && previous.moduleURL == instance.ModuleURL {
				backend.proxy = previous.proxy
			} else {
				backend.proxy = proxy.NewServiceProxy(instance.ModuleURL)
			}
			pool = append(pool, backend)
		}
		newBackendPools[mod.ModuleName] = pool
		if _, exists := md.nextBackend[mod.ModuleName]; !exists {
			md.nextBackend[mod.ModuleName] = 0
		}
	}

	for moduleName := range md.nextBackend {
		if _, exists := newModules[moduleName]; !exists {
			delete(md.nextBackend, moduleName)
		}
	}

	md.modules = newModules
	md.backendPools = newBackendPools

	fmt.Printf("模块列表已刷新: %d 个活跃模块\n", len(md.modules))
	return nil
}

func selectRoutableBackends(module *client.ModuleInfo, now time.Time) []client.ModuleRuntimeInstanceInfo {
	if module == nil || !module.Enabled {
		return nil
	}
	candidates := make([]client.ModuleRuntimeInstanceInfo, 0)
	for _, instance := range module.Instances {
		if instance.Role == client.ModuleRuntimeRoleBackend && instance.Status == "up" && instance.LeaseExpiresAt.After(now) && instance.ModuleURL != "" {
			candidates = append(candidates, instance)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].InstanceID < candidates[j].InstanceID })
	return candidates
}

func indexModuleBackends(backends []moduleBackend) map[string]moduleBackend {
	result := make(map[string]moduleBackend, len(backends))
	for _, backend := range backends {
		result[backend.instanceID] = backend
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
		if backend.proxy == nil || !backend.leaseExpiresAt.After(md.now()) {
			continue
		}
		md.nextBackend[moduleName] = index + 1
		return backend.proxy, nil
	}
	return nil, fmt.Errorf("模块 %s 没有有效 Backend 租约", moduleName)
}

// GetModules 获取所有活跃模块信息（用于展示）
func (md *ModuleDiscovery) GetModules() map[string]*client.ModuleInfo {
	md.mu.RLock()
	defer md.mu.RUnlock()

	// 返回当前仍有有效 Backend 租约的模块列表副本。
	result := make(map[string]*client.ModuleInfo, len(md.modules))
	for k, v := range md.modules {
		for _, backend := range md.backendPools[k] {
			if backend.leaseExpiresAt.After(md.now()) {
				result[k] = v
				break
			}
		}
	}

	return result
}
