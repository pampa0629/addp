package format

import "strings"

// ContainerInfo 描述容器格式内部对象的结构化元数据。
//
// 容器本身仍是一个 data item；Children 只描述容器内部可寻址对象，
// 不决定 Meta 是否把 child 物化为独立 item。
type ContainerInfo struct {
	Format        FormatType
	ChildCount    int
	DefaultChild  string
	ResourceCount int
	Children      []ContainerChildInfo
	FormatInfo    map[string]interface{}
}

// ContainerChildInfo 描述容器内部的一个子对象，例如 Excel sheet、SQLite table。
type ContainerChildInfo struct {
	Name        string
	Kind        string
	DataType    string
	Format      FormatType
	Layout      string
	RowCount    *int64
	ColumnCount *int
	HasHeader   *bool
	Fields      []FieldInfo
	Refs        []ContainerChildRef
	Properties  map[string]interface{}
}

type ContainerChildRef struct {
	Role      string
	Path      string
	Required  bool
	Primary   bool
	Extension string
}

const (
	ContainerChildLimitParam = "container_child_limit"
	ContainerRowLimitParam   = "container_row_limit"
	ChildNameParam           = "child_name"
	ChildTableParam          = "child_table"
	ChildKindParam           = "child_kind"
)

// ContainerParseOptions builds generic options for listing container children.
func ContainerParseOptions(childLimit, rowLimit int) *ParseOptions {
	opts := DefaultParseOptions()
	opts.ExtraParams = map[string]interface{}{
		ContainerChildLimitParam: childLimit,
		ContainerRowLimitParam:   rowLimit,
	}
	return opts
}

// ChildTableParseOptions builds generic options for reading a selected container child.
// Format plugins translate these generic child hints into their native selection model.
func ChildTableParseOptions(childName string, child map[string]interface{}) *ParseOptions {
	opts := DefaultParseOptions()
	childName = strings.TrimSpace(childName)
	tableName := strings.TrimSpace(interfaceString(child["table"]))
	if childName == "" {
		childName = strings.TrimSpace(interfaceString(child["name"]))
	}
	if tableName == "" {
		tableName = childName
	}
	opts.SheetName = childName
	opts.ExtraParams = map[string]interface{}{
		ChildNameParam:  childName,
		ChildTableParam: tableName,
		ChildKindParam:  strings.TrimSpace(interfaceString(child["kind"])),
	}
	return opts
}

func interfaceString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}
