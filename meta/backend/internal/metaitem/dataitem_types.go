package metaitem

import (
	"context"

	"github.com/addp/meta/internal/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

// CompositeItemInfo 是 Meta detector 提取出的 data item 元信息。
type CompositeItemInfo struct {
	Fields         []format.FieldInfo
	Attributes     map[string]interface{}
	Organization   dataitem.Organization
	DataType       dataitem.DataType
	Format         string
	EntryPath      string
	ComponentFiles []string
	SizeBytes      *int64
}

// ResourceClaimSet 记录 Meta detector 已认领的源资源路径。
type ResourceClaimSet map[string]bool

// DetectionResult 是 Meta 统一识别入口在一个扫描范围内产出的 item 集合。
type DetectionResult struct {
	Items     []*DetectedItem
	Claims    ResourceClaimSet
	Exclusive bool
}

// DetectedItem 是 Meta 识别后的标准化 data item 计划。
type DetectedItem struct {
	ItemType       string
	Organization   dataitem.Organization
	DataType       dataitem.DataType
	Format         string
	PhysicalPath   string
	EntryPath      string
	ComponentFiles []string
	SizeBytes      int64
	Fields         []format.FieldInfo
	Attributes     map[string]interface{}
}

// DirectoryResolveInput 是 Meta 扫描范围 item 识别的输入。
type DirectoryResolveInput struct {
	ContentReader plugin.ContentReadableProvider
	ConnInfo      plugin.ConnectionInfo
	EngineID      uint
	DirPath       string
	Files         []plugin.FileEntry
	Subdirs       []plugin.DirEntry
	// RecursiveFiles/RecursiveSubdirs 由扫描入口在需要识别 whole scope 时提供。
	// detector 只消费观察资源，不自行遍历存储引擎。
	RecursiveFiles   []plugin.FileEntry
	RecursiveSubdirs []plugin.DirEntry
}

// CompositeItemDetector 检测目录或文件组是否构成一个 Meta data item。
type CompositeItemDetector interface {
	Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool
	ExtractItemInfo(ctx context.Context, contentReader plugin.ContentReadableProvider,
		connInfo plugin.ConnectionInfo, engineID uint, dirPath string,
		files []plugin.FileEntry) (*CompositeItemInfo, error)
	Priority() int
	ItemType() string
}

// ScopeItemDetector 从一个扫描范围内识别 0..N 个 Meta data item。
type ScopeItemDetector interface {
	ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error)
	Priority() int
}

// FormatRuleProvider 声明 detector 背后的格式规则。
type FormatRuleProvider interface {
	Rule() dataitem.FormatRule
}

// FormatRulesProvider 声明 detector 背后的多条格式规则。
type FormatRulesProvider interface {
	Rules() []dataitem.FormatRule
}
