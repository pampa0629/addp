package dataitem

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

// ComponentRule 声明 multi 组织方式的组件规则。
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
	Organization Organization
	Priority     int

	Entry      EntryRule
	Components *ComponentRule
	Container  *ContainerRule
	WholeScope *WholeScopeRule
}
