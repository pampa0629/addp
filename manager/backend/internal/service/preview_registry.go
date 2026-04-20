package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/addp/manager/internal/models"
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
	Engine   *models.Engine
	Schema   string
	Table    string
	Page     int
	PageSize int
	TenantID *uint
	ItemType string // 数据项类型（如 "lake_table"），用于特殊预览路由
	NodeType string // 节点类型（来自 locator type 参数，如 "prefix"/"object"/"bucket"）
}

// Mode 根据请求推断预览模式。
func (r *PreviewRequest) Mode() string {
	if r == nil {
		return ""
	}

	if r.Table == "" {
		return PreviewModeNode
	}

	if r.Engine != nil && isObjectStorageType(r.Engine.EngineType) {
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

// sanitizeEngineType 统一引擎类型比较。
func sanitizeEngineType(engineType string) string {
	return strings.ToLower(strings.TrimSpace(engineType))
}

// sanitizeResourceType 兼容旧名称
func sanitizeResourceType(resourceType string) string {
	return sanitizeEngineType(resourceType)
}

