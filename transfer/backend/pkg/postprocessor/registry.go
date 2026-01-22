package postprocessor

import (
	"fmt"
	"sync"
)

// PostProcessorRegistry 后处理器注册表
// 管理不同引擎类型的 PostProcessor 实现
// 采用工厂模式，支持按引擎类型动态创建实例
//
// 架构模式：与 pipeline.ConnectorRegistry 保持一致
// - 线程安全：使用 RWMutex 保护并发访问
// - 工厂模式：存储工厂函数而非实例
// - 类型路由：通过 engineType 动态选择实现
type PostProcessorRegistry struct {
	processors map[string]PostProcessorFactory // engineType -> Factory
	mu         sync.RWMutex
}

// NewPostProcessorRegistry 创建后处理器注册表
func NewPostProcessorRegistry() *PostProcessorRegistry {
	return &PostProcessorRegistry{
		processors: make(map[string]PostProcessorFactory),
	}
}

// Register 注册后处理器工厂
// engineType: 引擎类型（postgresql, mysql, doris 等）
// factory: 工厂函数
// 返回：如果该类型已注册，返回错误
func (r *PostProcessorRegistry) Register(engineType string, factory PostProcessorFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.processors[engineType]; exists {
		return fmt.Errorf("postprocessor already registered for engine: %s", engineType)
	}

	r.processors[engineType] = factory
	return nil
}

// MustRegister 注册后处理器（panic on error）
// 用于启动时注册内置实现，如果重复注册会 panic
// engineType: 引擎类型
// factory: 工厂函数
func (r *PostProcessorRegistry) MustRegister(engineType string, factory PostProcessorFactory) {
	if err := r.Register(engineType, factory); err != nil {
		panic(fmt.Sprintf("failed to register postprocessor for %s: %v", engineType, err))
	}
}

// NewPostProcessor 创建后处理器实例
// config: 后处理器配置（必须包含 EngineType）
// 返回：PostProcessor 实例或错误
// 错误情况：引擎类型未注册、工厂函数创建失败
func (r *PostProcessorRegistry) NewPostProcessor(config PostProcessorConfig) (PostProcessor, error) {
	r.mu.RLock()
	factory, exists := r.processors[config.EngineType]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no postprocessor registered for engine: %s", config.EngineType)
	}

	return factory(config)
}

// HasPostProcessor 检查是否支持指定引擎
// engineType: 引擎类型
// 返回：true 表示已注册
func (r *PostProcessorRegistry) HasPostProcessor(engineType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.processors[engineType]
	return exists
}

// ListEngines 列出所有支持的引擎类型
// 返回：引擎类型列表
func (r *PostProcessorRegistry) ListEngines() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	engines := make([]string, 0, len(r.processors))
	for engine := range r.processors {
		engines = append(engines, engine)
	}
	return engines
}

// 全局注册表（用于内置注册）
var globalRegistry = NewPostProcessorRegistry()

// GlobalRegistry 获取全局注册表
// 返回：全局 PostProcessorRegistry 实例
func GlobalRegistry() *PostProcessorRegistry {
	return globalRegistry
}

// MustRegisterPostProcessor 全局注册（用于 plugins/builtin_registration.go）
// engineType: 引擎类型
// factory: 工厂函数
// panic：如果该类型已注册
func MustRegisterPostProcessor(engineType string, factory PostProcessorFactory) {
	globalRegistry.MustRegister(engineType, factory)
}
