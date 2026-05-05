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
