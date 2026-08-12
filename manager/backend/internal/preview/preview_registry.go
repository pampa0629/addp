package preview

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/dataprofile"
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
	Locator string
	Engine  *models.Engine
	// EnginePlugin is resolved for this concrete Engine Instance. Database
	// previews must use it instead of looking up a plugin by engine type.
	EnginePlugin    plugin.EnginePlugin
	Schema          string
	Table           string
	Page            int
	PageSize        int
	TenantID        *uint
	ItemFingerprint string
	ItemType        string                   // 数据项类型（如 "table"），用于预览路由
	ItemRowCount    *int64                   // 表/集合行数，来自 MetaItem.RowCount
	ScannedDepth    string                   // Meta item/node 当前扫描深度
	NodeType        string                   // 节点类型（来自 locator type 参数，如 "prefix"/"object"/"bucket"）
	ProviderPath    plugin.CatalogPath       // provider 调用使用的显式 root CatalogPath
	PhysicalPath    string                   // 物理路径（来自 meta_item.attributes.storage.physical_path），单文件表直接读取
	ScopePath       string                   // 范围路径（来自 meta_item.attributes.storage.physical_path），目录型表读取 scope
	ChildName       string                   // 容器内部 child 名称，例如 Excel sheet
	RefPath         string                   // multi child 内的单个ref 路径，指向容器内原始对象
	NestedChildPath string                   // 当前 child 是容器时，继续寻址其内部 child 的相对路径
	GraphSample     plugin.GraphSampleFilter // 图预览样本过滤条件
	DataScope       dataprofile.DataScope    // Manager 剖析内部使用的数据范围
	Attributes      map[string]interface{}   // 来自 meta_item/meta_node 的标准属性分区
}

// Mode 根据请求推断预览模式。
func (r *PreviewRequest) Mode() string {
	if r == nil {
		return ""
	}

	if r.Table == "" {
		return PreviewModeNode
	}

	if r.ItemType == "object" || r.ItemType == "file" {
		return PreviewModeObject
	}

	return PreviewModeTable
}

// PreviewProvider 数据预览插件需要实现的接口。
type PreviewProvider interface {
	Name() string
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

	for index, existing := range r.providers {
		if existing != nil && existing.Name() == provider.Name() {
			r.providers[index] = provider
			return
		}
	}
	r.providers = append(r.providers, provider)
}

func (r *PreviewRegistry) Unregister(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	filtered := r.providers[:0]
	for _, provider := range r.providers {
		if provider != nil && provider.Name() == name {
			continue
		}
		filtered = append(filtered, provider)
	}
	r.providers = filtered
}

// GetByName 根据 provider 名称返回插件。
func (r *PreviewRegistry) GetByName(name string) (PreviewProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	name = strings.TrimSpace(name)
	for _, provider := range r.providers {
		if provider.Name() == name {
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
