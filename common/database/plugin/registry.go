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
	plugins map[string]DatabasePlugin
}

// globalRegistry 全局单例注册表
var globalRegistry = &Registry{
	plugins: make(map[string]DatabasePlugin),
}

// Register 注册数据库插件
// 通常在插件包的 init() 函数中调用
// 如果类型已注册，会被新插件覆盖（允许插件替换）
func Register(plugin DatabasePlugin) {
	if plugin == nil {
		panic("cannot register nil plugin")
	}

	dbType := strings.ToLower(plugin.Type())
	if dbType == "" {
		panic("plugin type cannot be empty")
	}

	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	globalRegistry.plugins[dbType] = plugin
}

// Get 获取指定类型的数据库插件
// dbType: 数据库类型（不区分大小写）
// 返回插件实例，如果未找到返回 error
func Get(dbType string) (DatabasePlugin, error) {
	dbType = strings.ToLower(dbType)

	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	plugin, ok := globalRegistry.plugins[dbType]
	if !ok {
		return nil, fmt.Errorf("unsupported database type: %s (available types: %s)",
			dbType, strings.Join(List(), ", "))
	}

	return plugin, nil
}

// List 列出所有已注册的数据库类型
// 返回排序后的类型列表
func List() []string {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	types := make([]string, 0, len(globalRegistry.plugins))
	for dbType := range globalRegistry.plugins {
		types = append(types, dbType)
	}

	sort.Strings(types)
	return types
}

// GetAll 获取所有已注册的插件
// 返回 map[类型]插件实例
func GetAll() map[string]DatabasePlugin {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	// 创建副本，避免外部修改
	plugins := make(map[string]DatabasePlugin, len(globalRegistry.plugins))
	for dbType, plugin := range globalRegistry.plugins {
		plugins[dbType] = plugin
	}

	return plugins
}

// Has 检查指定类型的插件是否已注册
func Has(dbType string) bool {
	dbType = strings.ToLower(dbType)

	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	_, ok := globalRegistry.plugins[dbType]
	return ok
}

// Unregister 注销指定类型的插件（主要用于测试）
func Unregister(dbType string) {
	dbType = strings.ToLower(dbType)

	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	delete(globalRegistry.plugins, dbType)
}

// Clear 清空所有注册的插件（主要用于测试）
func Clear() {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()

	globalRegistry.plugins = make(map[string]DatabasePlugin)
}
