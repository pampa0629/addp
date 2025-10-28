package connector

import (
	"fmt"
	"sort"
	"sync"

	"github.com/addp/transfer/pkg/pipeline"
)

var (
	registryMu      sync.RWMutex
	readerFactories = make(map[string]pipeline.ReaderFactory)
	writerFactories = make(map[string]pipeline.WriterFactory)
)

// MustRegisterReader 用于在程序启动阶段注册全局 Reader 工厂。
// 如果重复注册同名 Reader，将触发 panic，提前暴露冲突。
func MustRegisterReader(connectorType string, factory pipeline.ReaderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := readerFactories[connectorType]; exists {
		panic(fmt.Sprintf("connector: reader already registered for type %q", connectorType))
	}

	readerFactories[connectorType] = factory
}

// MustRegisterWriter 用于在程序启动阶段注册全局 Writer 工厂。
func MustRegisterWriter(connectorType string, factory pipeline.WriterFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := writerFactories[connectorType]; exists {
		panic(fmt.Sprintf("connector: writer already registered for type %q", connectorType))
	}

	writerFactories[connectorType] = factory
}

// MustRegisterConnector 同时注册 Reader/Writer；允许任一为 nil。
func MustRegisterConnector(connectorType string, reader pipeline.ReaderFactory, writer pipeline.WriterFactory) {
	if reader != nil {
		MustRegisterReader(connectorType, reader)
	}
	if writer != nil {
		MustRegisterWriter(connectorType, writer)
	}
}

// RegisterAllConnectors 将全局已注册的工厂写入运行时注册表。
func RegisterAllConnectors(registry *pipeline.ConnectorRegistry) error {
	registryMu.RLock()
	defer registryMu.RUnlock()

	for connectorType, factory := range readerFactories {
		if err := registry.RegisterReader(connectorType, factory); err != nil {
			return fmt.Errorf("register reader %q: %w", connectorType, err)
		}
	}

	for connectorType, factory := range writerFactories {
		if err := registry.RegisterWriter(connectorType, factory); err != nil {
			return fmt.Errorf("register writer %q: %w", connectorType, err)
		}
	}

	return nil
}

// RegisteredReaders 返回当前已注册的 Reader 类型（按字典序）。
func RegisteredReaders() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	types := make([]string, 0, len(readerFactories))
	for t := range readerFactories {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// RegisteredWriters 返回当前已注册的 Writer 类型（按字典序）。
func RegisteredWriters() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	types := make([]string, 0, len(writerFactories))
	for t := range writerFactories {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}
