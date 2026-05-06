package dataitem

import "github.com/addp/common/format"

// Organization 描述引擎资源如何组织成一个 data item。
type Organization string

const (
	OrganizationSingle Organization = "single"
	OrganizationMulti  Organization = "multi"
	OrganizationWhole  Organization = "whole"
)

// DataType 描述 data item 在用户观感和处理方式上的主数据类型。
type DataType string

const (
	DataTypeTable     DataType = "table"
	DataTypeDocument  DataType = "document"
	DataTypeMedia     DataType = "media"
	DataTypeContainer DataType = "container"
	DataTypeGraph     DataType = "graph"
	DataTypeUnknown   DataType = "unknown"
)

// CompositeItemInfo 是组合形态 detector 提取出的元信息。
type CompositeItemInfo struct {
	Fields         []format.FieldInfo
	Attributes     map[string]interface{}
	Organization   Organization
	DataType       DataType
	Format         string
	EntryPath      string
	ComponentFiles []string
	SizeBytes      *int64
}

// ResourceClaimSet 记录 detector 已认领的源资源路径。
type ResourceClaimSet map[string]bool

// ComponentMatchScope 描述多文件组件匹配范围。
type ComponentMatchScope string

const (
	ComponentMatchScopeSameDirectory ComponentMatchScope = "same_directory"
	ComponentMatchScopeSamePrefix    ComponentMatchScope = "same_prefix"
	ComponentMatchScopeRecursive     ComponentMatchScope = "recursive"
)

// ComponentMatchKey 描述多文件组件之间的匹配键。
type ComponentMatchKey string

const (
	ComponentMatchKeyBaseName ComponentMatchKey = "base_name"
	ComponentMatchKeyManifest ComponentMatchKey = "manifest"
)

// EntryRule 声明格式入口资源如何识别。
type EntryRule struct {
	Extensions []string
	MIMETypes  []string
}

// ComponentRule 声明 multi_file 组合的组件规则。
type ComponentRule struct {
	MatchScope         ComponentMatchScope
	MatchKey           ComponentMatchKey
	RequiredExtensions []string
	OptionalExtensions []string
	EntryExtension     string
	AllowRecursive     bool
}

// ContainerRule 声明容器型数据的内部对象表达规则，不改变 organization。
type ContainerRule struct {
	ExpandInternalItems bool
}

// WholeScopeRule 声明 whole scope 独占和递归规则。
type WholeScopeRule struct {
	AllowRecursive       bool
	IgnoredFileNames     []string
	RequiresStrongMatch  bool
	ExclusiveOnStrongHit bool
}

// FormatRule 是格式实现层对组织方式、入口和组件规则的一次性声明。
type FormatRule struct {
	Format       string
	DataType     DataType
	ItemType     string
	Organization Organization
	Priority     int

	Entry      EntryRule
	Components *ComponentRule
	Container  *ContainerRule
	WholeScope *WholeScopeRule
}

// DetectionResult 是统一识别入口在一个扫描范围内产出的 item 集合。
type DetectionResult struct {
	Items     []*DetectedItem
	Claims    ResourceClaimSet
	Exclusive bool
}

// DetectedItem 是组合形态推断后的标准化 item 表达。
type DetectedItem struct {
	ItemType       string
	Organization   Organization
	DataType       DataType
	Format         string
	PhysicalPath   string
	EntryPath      string
	ComponentFiles []string
	SizeBytes      int64
	Fields         []format.FieldInfo
	Attributes     map[string]interface{}
}
