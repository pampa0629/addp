package plugin

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry 全局插件注册表
type Registry struct {
	mu      sync.RWMutex
	plugins map[string]EnginePlugin
}

// globalRegistry 全局单例注册表
var globalRegistry = &Registry{
	plugins: make(map[string]EnginePlugin),
}

// Register 注册引擎插件
// 通常在插件包的 init() 函数中调用
// 如果类型已注册，会被新插件覆盖（允许插件替换）
func Register(plugin EnginePlugin) {
	if plugin == nil {
		panic("cannot register nil plugin")
	}

	engineType := strings.ToLower(plugin.Type())
	if engineType == "" {
		panic("plugin type cannot be empty")
	}

	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	globalRegistry.plugins[engineType] = plugin
}

// Get 获取指定类型的引擎插件
// engineType: 引擎类型（不区分大小写）
// 返回插件实例，如果未找到返回 error
func Get(engineType string) (EnginePlugin, error) {
	engineType = strings.ToLower(engineType)

	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	plugin, ok := globalRegistry.plugins[engineType]
	if !ok {
		return nil, fmt.Errorf("unsupported engine type: %s (available types: %s)",
			engineType, strings.Join(List(), ", "))
	}

	return plugin, nil
}

// List 列出所有已注册的引擎类型
// 返回排序后的类型列表
func List() []string {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	types := make([]string, 0, len(globalRegistry.plugins))
	for engineType := range globalRegistry.plugins {
		types = append(types, engineType)
	}

	sort.Strings(types)
	return types
}

// GetAll 获取所有已注册的插件
// 返回 map[类型]插件实例
func GetAll() map[string]EnginePlugin {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	// 创建副本，避免外部修改
	plugins := make(map[string]EnginePlugin, len(globalRegistry.plugins))
	for dbType, plugin := range globalRegistry.plugins {
		plugins[dbType] = plugin
	}

	return plugins
}

// Has 检查指定类型的插件是否已注册
func Has(engineType string) bool {
	engineType = strings.ToLower(engineType)

	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	_, ok := globalRegistry.plugins[engineType]
	return ok
}

// Unregister 注销指定类型的插件（主要用于测试）
func Unregister(engineType string) {
	engineType = strings.ToLower(engineType)

	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	delete(globalRegistry.plugins, engineType)
}

// Clear 清空所有注册的插件（主要用于测试）
func Clear() {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	globalRegistry.plugins = make(map[string]EnginePlugin)
}
