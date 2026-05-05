package dataitem

import "github.com/addp/common/format"

// CompositionType 描述一套资源如何组成一个 meta item。
type CompositionType string

const (
	CompositionTypeSingleFile      CompositionType = "single_file"
	CompositionTypeMultiFile       CompositionType = "multi_file"
	CompositionTypeContainerFile   CompositionType = "container_file"
	CompositionTypeDirectoryTree   CompositionType = "directory_tree"
	CompositionTypeMixedCollection CompositionType = "mixed_collection"
)

// DataFamily 描述 item 的主数据家族。空间等能力应作为扩展语义挂载。
type DataFamily string

const (
	DataFamilyTabular  DataFamily = "tabular"
	DataFamilyImage    DataFamily = "image"
	DataFamilyVideo    DataFamily = "video"
	DataFamilyDocument DataFamily = "document"
	DataFamilyAudio    DataFamily = "audio"
	DataFamilyGraph    DataFamily = "graph"
	DataFamilyUnknown  DataFamily = "unknown"
)

// CompositeItemInfo 是组合形态 detector 提取出的元信息。
type CompositeItemInfo struct {
	Fields          []format.FieldInfo
	Attributes      map[string]interface{}
	CompositionType CompositionType
	DataFamily      DataFamily
	Format          string
	EntryPath       string
	ComponentFiles  []string
	SizeBytes       *int64
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

// ContainerRule 声明 container_file 内部对象展开规则。
type ContainerRule struct {
	ExpandInternalItems bool
}

// DirectoryTreeRule 声明 directory_tree 独占和递归规则。
type DirectoryTreeRule struct {
	AllowRecursive       bool
	IgnoredFileNames     []string
	RequiresStrongMatch  bool
	ExclusiveOnStrongHit bool
}

// CollectionRule 声明 mixed_collection 的复杂组件规则。
type CollectionRule struct {
	AllowRecursive bool
}

// FormatRule 是格式实现层对组合形态、入口和组件规则的一次性声明。
type FormatRule struct {
	Format          string
	DataFamily      DataFamily
	ItemType        string
	CompositionType CompositionType
	Priority        int

	Entry         EntryRule
	Components    *ComponentRule
	Container     *ContainerRule
	DirectoryTree *DirectoryTreeRule
	Collection    *CollectionRule
}

// DetectionResult 是统一识别入口在一个扫描范围内产出的 item 集合。
type DetectionResult struct {
	Items     []*DetectedItem
	Claims    ResourceClaimSet
	Exclusive bool
}

// DetectedItem 是组合形态推断后的标准化 item 表达。
type DetectedItem struct {
	ItemType        string
	CompositionType CompositionType
	DataFamily      DataFamily
	Format          string
	PhysicalPath    string
	EntryPath       string
	ComponentFiles  []string
	SizeBytes       int64
	Fields          []format.FieldInfo
	Attributes      map[string]interface{}
}
