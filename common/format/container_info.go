package format

import (
	"strings"
)

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
	if tableName == "" {
		if native := interfaceMap(child["native"]); native != nil {
			tableName = strings.TrimSpace(interfaceString(native["table"]))
		}
	}
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
		ChildKindParam:  containerChildKindFromMap(child),
	}
	return opts
}

func containerChildKindFromMap(child map[string]interface{}) string {
	return strings.TrimSpace(interfaceString(child["child_kind"]))
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

func interfaceMap(value interface{}) map[string]interface{} {
	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}
	return nil
}
