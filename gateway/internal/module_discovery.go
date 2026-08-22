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
	modules       map[string]*client.ModuleInfo  // moduleName -> ModuleInfo
	proxies       map[string]*proxy.ServiceProxy // moduleName -> proxy
	mu            sync.RWMutex
	refreshTicker *time.Ticker
	ctx           context.Context
	cancel        context.CancelFunc
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
		proxies:      make(map[string]*proxy.ServiceProxy),
		ctx:          ctx,
		cancel:       cancel,
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
	for _, mod := range modules {
		backend, ok := selectRoutableBackend(mod, time.Now())
		if !ok {
			continue
		}
		newModules[mod.ModuleName] = mod

		if existingProxy, exists := md.proxies[mod.ModuleName]; !exists || existingProxy.GetTargetURL() != backend.ModuleURL {
			md.proxies[mod.ModuleName] = proxy.NewServiceProxy(backend.ModuleURL)
		}
	}

	// 移除已下线的模块代理
	for moduleName := range md.proxies {
		if _, exists := newModules[moduleName]; !exists {
			delete(md.proxies, moduleName)
		}
	}

	md.modules = newModules

	fmt.Printf("模块列表已刷新: %d 个活跃模块\n", len(md.modules))
	return nil
}

func selectRoutableBackend(module *client.ModuleInfo, now time.Time) (*client.ModuleRuntimeInstanceInfo, bool) {
	if module == nil || !module.Enabled {
		return nil, false
	}
	candidates := make([]client.ModuleRuntimeInstanceInfo, 0)
	for _, instance := range module.Instances {
		if instance.Role == client.ModuleRuntimeRoleBackend && instance.Status == "up" && instance.LeaseExpiresAt.After(now) && instance.ModuleURL != "" {
			candidates = append(candidates, instance)
		}
	}
	if len(candidates) == 0 {
		return nil, false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].InstanceID < candidates[j].InstanceID })
	return &candidates[0], true
}

// GetProxy 获取模块代理（用于路由转发）
func (md *ModuleDiscovery) GetProxy(moduleName string) (*proxy.ServiceProxy, error) {
	md.mu.RLock()
	defer md.mu.RUnlock()

	p, exists := md.proxies[moduleName]
	if !exists {
		return nil, fmt.Errorf("模块 %s 不可用或未注册", moduleName)
	}

	return p, nil
}

// GetModules 获取所有活跃模块信息（用于展示）
func (md *ModuleDiscovery) GetModules() map[string]*client.ModuleInfo {
	md.mu.RLock()
	defer md.mu.RUnlock()

	// 返回模块列表的副本
	result := make(map[string]*client.ModuleInfo, len(md.modules))
	for k, v := range md.modules {
		result[k] = v
	}

	return result
}
