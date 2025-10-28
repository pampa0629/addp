package transform

import (
	"fmt"
	"sort"
	"sync"

	"github.com/addp/transfer/pkg/pipeline"
)

var (
	registryMu           sync.RWMutex
	registeredTransforms = make(map[string]pipeline.TransformCapability)
)

// MustRegisterTransform 在初始化阶段注册转换器，重复注册会触发 panic。
func MustRegisterTransform(name string, factory pipeline.TransformFactory, capability pipeline.TransformCapability) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := registeredTransforms[name]; exists {
		panic(fmt.Sprintf("transform: already registered %q", name))
	}

	if err := pipeline.RegisterTransform(name, factory, capability); err != nil {
		panic(fmt.Sprintf("transform: register %q failed: %v", name, err))
	}

	if capability.Name == "" {
		capability.Name = name
	}
	registeredTransforms[name] = capability
}

// RegisteredNames 返回已注册的转换器名称列表。
func RegisteredNames() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(registeredTransforms))
	for name := range registeredTransforms {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Capabilities 返回已注册转换器的能力描述，按名称排列。
func Capabilities() []pipeline.TransformCapability {
	registryMu.RLock()
	defer registryMu.RUnlock()

	list := make([]pipeline.TransformCapability, 0, len(registeredTransforms))
	for _, cap := range registeredTransforms {
		list = append(list, cap)
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}
