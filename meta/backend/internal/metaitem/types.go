package metaitem

import (
	"context"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

// CompositeItemInfo 是 Meta resolver 提取出的 data item 元信息。
type CompositeItemInfo struct {
	Fields       []format.FieldInfo
	Attributes   map[string]interface{}
	Organization dataitem.Organization
	DataType     dataitem.DataType
	Format       string
	EntryPath    string
	RefFiles     []string
	SizeBytes    *int64
}

// ResourceClaimSet 记录 Meta resolver 已认领的源资源路径。
type ResourceClaimSet map[string]bool

// DetectionResult 是 Meta 统一识别入口在一个扫描范围内产出的 item 集合。
type DetectionResult struct {
	Items     []*DetectedItem
	Claims    ResourceClaimSet
	Exclusive bool
}

// DetectedItem 是 Meta 识别后的标准化 data item 计划。
type DetectedItem struct {
	dataitem.ResolvedItem
	PhysicalPath string
	Fields       []format.FieldInfo
	Attributes   map[string]interface{}
}

func (item *DetectedItem) Size() int64 {
	if item == nil || item.SizeBytes == nil {
		return 0
	}
	return *item.SizeBytes
}

func (item *DetectedItem) RefFilePaths() []string {
	if item == nil || len(item.RefList) == 0 {
		return nil
	}
	paths := make([]string, 0, len(item.RefList))
	for _, ref := range item.RefList {
		if ref.Path != "" {
			paths = append(paths, ref.Path)
		}
	}
	return paths
}

func DetectedItemFromCompositeInfo(info *CompositeItemInfo, physicalPath string, fallbackSize int64) *DetectedItem {
	if info == nil {
		return nil
	}
	sizeBytes := fallbackSize
	if info.SizeBytes != nil {
		sizeBytes = *info.SizeBytes
	}
	return &DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Organization: info.Organization,
			DataType:     info.DataType,
			Format:       info.Format,
			EntryPath:    info.EntryPath,
			SizeBytes:    &sizeBytes,
			RefList:      ItemRefsFromPaths(info.RefFiles),
		},
		PhysicalPath: physicalPath,
		Fields:       info.Fields,
		Attributes:   info.Attributes,
	}
}

func ItemRefsFromPaths(paths []string) []dataitem.ItemRef {
	if len(paths) == 0 {
		return nil
	}
	refs := make([]dataitem.ItemRef, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		refs = append(refs, dataitem.ItemRef{Path: path})
	}
	return refs
}

// DirectoryResolveInput 是 Meta 扫描范围 item 识别的输入。
type DirectoryResolveInput struct {
	ContentReader  plugin.ContentReadableProvider
	ConnInfo       plugin.ConnectionInfo
	EngineID       uint
	CatalogPathFor func(path string) plugin.CatalogPath
	DirPath        string
	Files          []plugin.FileEntry
	Subdirs        []plugin.DirEntry
	// RecursiveFiles/RecursiveSubdirs 由扫描入口在需要识别 whole scope 时提供。
	// resolver 只消费观察资源，不自行遍历存储引擎。
	RecursiveFiles   []plugin.FileEntry
	RecursiveSubdirs []plugin.DirEntry
}

// ItemResolver 是 Meta item 识别器的最小公共接口。
type ItemResolver interface {
	Priority() int
}

// ScopeItemResolver 从一个扫描范围内识别 0..N 个 Meta data item。
type ScopeItemResolver interface {
	ItemResolver
	ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error)
}

// FormatRuleProvider 声明 resolver 背后的格式规则。
type FormatRuleProvider interface {
	Rule() dataitem.FormatRule
}

// FormatRulesProvider 声明 resolver 背后的多条格式规则。
type FormatRulesProvider interface {
	Rules() []dataitem.FormatRule
}
