package pipeline

import (
	"fmt"
	"sync"
)

// TransformFactory Transform 工厂函数
type TransformFactory func(config map[string]interface{}) (Transform, error)

// TransformCapability 转换器能力描述
type TransformCapability struct {
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	SupportedTypes []string               `json:"supported_types"` // 支持的字段类型
	ConfigSchema   map[string]interface{} `json:"config_schema"`   // JSON Schema
	Version        string                 `json:"version"`
	Author         string                 `json:"author"`
}

// TransformRegistry 转换器注册表
// 管理所有转换器的注册与创建
type TransformRegistry struct {
	factories    map[string]TransformFactory
	capabilities map[string]TransformCapability
	mu           sync.RWMutex
}

// NewTransformRegistry 创建转换器注册表
func NewTransformRegistry() *TransformRegistry {
	return &TransformRegistry{
		factories:    make(map[string]TransformFactory),
		capabilities: make(map[string]TransformCapability),
	}
}

// RegisterTransform 注册转换器
func (r *TransformRegistry) RegisterTransform(
	name string,
	factory TransformFactory,
	capability TransformCapability,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("transform already registered: %s", name)
	}

	// 确保 capability.Name 与注册名称一致
	if capability.Name == "" {
		capability.Name = name
	}

	r.factories[name] = factory
	r.capabilities[name] = capability
	return nil
}

// NewTransform 创建转换器实例
func (r *TransformRegistry) NewTransform(name string, config map[string]interface{}) (Transform, error) {
	r.mu.RLock()
	factory, exists := r.factories[name]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("transform not registered: %s", name)
	}

	return factory(config)
}

// ListTransforms 列出所有已注册的转换器
func (r *TransformRegistry) ListTransforms() []TransformCapability {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]TransformCapability, 0, len(r.capabilities))
	for _, cap := range r.capabilities {
		list = append(list, cap)
	}
	return list
}

// GetCapability 获取转换器能力描述
func (r *TransformRegistry) GetCapability(name string) (TransformCapability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cap, exists := r.capabilities[name]
	return cap, exists
}

// HasTransform 检查是否已注册指定转换器
func (r *TransformRegistry) HasTransform(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.factories[name]
	return exists
}

// UnregisterTransform 取消注册转换器（主要用于测试）
func (r *TransformRegistry) UnregisterTransform(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.factories, name)
	delete(r.capabilities, name)
}

// 全局默认注册表实例
var defaultTransformRegistry = NewTransformRegistry()

// RegisterTransform 注册到全局注册表
func RegisterTransform(name string, factory TransformFactory, capability TransformCapability) error {
	return defaultTransformRegistry.RegisterTransform(name, factory, capability)
}

// NewTransformByName 从全局注册表创建转换器
func NewTransformByName(name string, config map[string]interface{}) (Transform, error) {
	return defaultTransformRegistry.NewTransform(name, config)
}

// ListAllTransforms 列出全局注册表中的所有转换器
func ListAllTransforms() []TransformCapability {
	return defaultTransformRegistry.ListTransforms()
}

// GetTransformCapability 获取转换器能力描述（从全局注册表）
func GetTransformCapability(name string) (TransformCapability, bool) {
	return defaultTransformRegistry.GetCapability(name)
}

// HasTransformRegistered 检查转换器是否已注册（从全局注册表）
func HasTransformRegistered(name string) bool {
	return defaultTransformRegistry.HasTransform(name)
}
