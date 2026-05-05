package dataitem

import (
	"context"

	"github.com/addp/common/engine/plugin"
)

// CompositeItemDetector 检测目录或文件组是否构成一个组合数据项。
type CompositeItemDetector interface {
	// Detect 根据目录内容判断是否匹配，不读取文件内容。
	Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool

	// ExtractItemInfo 匹配后提取元信息。需要读内容时通过 ContentReadableProvider 读取。
	ExtractItemInfo(ctx context.Context, contentReader plugin.ContentReadableProvider,
		connInfo plugin.ConnectionInfo, engineID uint, dirPath string,
		files []plugin.FileEntry) (*CompositeItemInfo, error)

	// Priority 优先级，越大越先检测。
	Priority() int

	// ItemType 对应 MetaItem.item_type。
	ItemType() string
}

// ScopeItemDetector 从一个扫描范围内识别 0..N 个组合数据项。
// 新 detector 应优先实现该接口，避免把“观察范围”误当成 item 边界。
type ScopeItemDetector interface {
	ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error)
	Priority() int
}

// FormatRuleProvider 声明 detector 背后的格式规则。
type FormatRuleProvider interface {
	Rule() FormatRule
}
