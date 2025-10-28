package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

// Preview modes
const (
	PreviewModeNode   = "node"
	PreviewModeTable  = "table"
	PreviewModeObject = "object"
)

// ErrNoPreviewProvider is returned when no provider can handle the request.
var ErrNoPreviewProvider = errors.New("no preview provider registered for request")

// PreviewRequest 包含生成预览所需的上下文信息。
type PreviewRequest struct {
	Resource *models.Resource
	Schema   string
	Table    string
	Page     int
	PageSize int
	TenantID *uint
}

// Mode 根据请求推断预览模式。
func (r *PreviewRequest) Mode() string {
	if r == nil {
		return ""
	}

	if r.Table == "" {
		return PreviewModeNode
	}

	if r.Resource != nil && isObjectStorageType(r.Resource.ResourceType) {
		return PreviewModeObject
	}

	return PreviewModeTable
}

// PreviewProvider 数据预览插件需要实现的接口。
type PreviewProvider interface {
	Name() string
	Priority() int
	Supports(*PreviewRequest) bool
	Preview(context.Context, *PreviewRequest) (*models.TablePreview, error)
}

// PreviewRegistry 维护已注册的预览插件。
type PreviewRegistry struct {
	mu        sync.RWMutex
	providers []PreviewProvider
}

// NewPreviewRegistry 创建空白注册表。
func NewPreviewRegistry() *PreviewRegistry {
	return &PreviewRegistry{}
}

// Register 注册新的预览插件。
func (r *PreviewRegistry) Register(provider PreviewProvider) {
	if provider == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.providers = append(r.providers, provider)
	sort.SliceStable(r.providers, func(i, j int) bool {
		return r.providers[i].Priority() > r.providers[j].Priority()
	})
}

// Resolve 根据请求选择合适的插件。
func (r *PreviewRegistry) Resolve(req *PreviewRequest) (PreviewProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, provider := range r.providers {
		if provider.Supports(req) {
			return provider, nil
		}
	}

	return nil, ErrNoPreviewProvider
}

// Providers 返回已注册插件的名称列表。
func (r *PreviewRegistry) Providers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for _, provider := range r.providers {
		names = append(names, provider.Name())
	}
	return names
}

// sanitizeResourceType 统一资源类型比较。
func sanitizeResourceType(resourceType string) string {
	return strings.ToLower(strings.TrimSpace(resourceType))
}

// ProviderFactory 预览插件工厂函数类型
type ProviderFactory func(*repository.MetadataRepository, *ObjectContentRegistry) (PreviewProvider, error)

// 全局预览插件工厂注册表
var (
	globalProviderFactories = make(map[string]ProviderFactory)
	globalFactoryMu         sync.RWMutex
)

// RegisterPreviewProvider 注册预览插件工厂到全局注册表
// 通常在插件包的 init() 函数中调用
func RegisterPreviewProvider(name string, factory ProviderFactory) {
	globalFactoryMu.Lock()
	defer globalFactoryMu.Unlock()

	globalProviderFactories[name] = factory
}

// GetPreviewProviderFactory 获取已注册的插件工厂
func GetPreviewProviderFactory(name string) ProviderFactory {
	globalFactoryMu.RLock()
	defer globalFactoryMu.RUnlock()

	return globalProviderFactories[name]
}

// ListPreviewProviderFactories 列出所有已注册的插件工厂名称
func ListPreviewProviderFactories() []string {
	globalFactoryMu.RLock()
	defer globalFactoryMu.RUnlock()

	names := make([]string, 0, len(globalProviderFactories))
	for name := range globalProviderFactories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RegisterBuiltinProviders 将所有全局注册的插件工厂实例化并注册到指定注册表
func RegisterBuiltinProviders(registry *PreviewRegistry, metadataRepo *repository.MetadataRepository, contentRegistry *ObjectContentRegistry) error {
	globalFactoryMu.RLock()
	factories := make(map[string]ProviderFactory, len(globalProviderFactories))
	for name, factory := range globalProviderFactories {
		factories[name] = factory
	}
	globalFactoryMu.RUnlock()

	for _, factory := range factories {
		provider, err := factory(metadataRepo, contentRegistry)
		if err != nil {
			return err
		}
		registry.Register(provider)
	}

	return nil
}
