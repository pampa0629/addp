package pipeline

import (
	"fmt"
	"sync"
)

// ConnectorRegistry 连接器注册表
// 管理所有 Reader 和 Writer 的注册与创建
type ConnectorRegistry struct {
	readers map[string]ReaderFactory
	writers map[string]WriterFactory
	mu      sync.RWMutex
}

// NewConnectorRegistry 创建连接器注册表
func NewConnectorRegistry() *ConnectorRegistry {
	return &ConnectorRegistry{
		readers: make(map[string]ReaderFactory),
		writers: make(map[string]WriterFactory),
	}
}

// RegisterReader 注册 Reader 工厂
func (r *ConnectorRegistry) RegisterReader(connectorType string, factory ReaderFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.readers[connectorType]; exists {
		return fmt.Errorf("reader already registered for type: %s", connectorType)
	}

	r.readers[connectorType] = factory
	return nil
}

// RegisterWriter 注册 Writer 工厂
func (r *ConnectorRegistry) RegisterWriter(connectorType string, factory WriterFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.writers[connectorType]; exists {
		return fmt.Errorf("writer already registered for type: %s", connectorType)
	}

	r.writers[connectorType] = factory
	return nil
}

// NewReader 创建 Reader 实例
func (r *ConnectorRegistry) NewReader(config ConnectorConfig) (Reader, error) {
	r.mu.RLock()
	factory, exists := r.readers[config.Type]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no reader registered for type: %s", config.Type)
	}

	return factory(config)
}

// NewWriter 创建 Writer 实例
func (r *ConnectorRegistry) NewWriter(config ConnectorConfig) (Writer, error) {
	r.mu.RLock()
	factory, exists := r.writers[config.Type]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("no writer registered for type: %s", config.Type)
	}

	return factory(config)
}

// ListReaders 列出所有已注册的 Reader 类型
func (r *ConnectorRegistry) ListReaders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.readers))
	for t := range r.readers {
		types = append(types, t)
	}
	return types
}

// ListWriters 列出所有已注册的 Writer 类型
func (r *ConnectorRegistry) ListWriters() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.writers))
	for t := range r.writers {
		types = append(types, t)
	}
	return types
}

// HasReader 检查是否支持指定类型的 Reader
func (r *ConnectorRegistry) HasReader(connectorType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.readers[connectorType]
	return exists
}

// HasWriter 检查是否支持指定类型的 Writer
func (r *ConnectorRegistry) HasWriter(connectorType string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.writers[connectorType]
	return exists
}
