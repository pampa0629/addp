package metaattr

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
)

func ContainerInfoAttributes(info *datatype.ContainerInfo) map[string]interface{} {
	if info == nil {
		return nil
	}
	attrs := commonJSON.MapFromStruct(containerAttributes{
		DefaultChild: info.DefaultChild,
	})
	if attrs == nil {
		attrs = map[string]interface{}{}
	}
	children := make([]map[string]interface{}, 0, len(info.Children))
	for _, child := range info.Children {
		if childAttrs := commonJSON.MapFromStruct(containerChildAttributes(child)); len(childAttrs) > 0 {
			children = append(children, childAttrs)
		}
	}
	attrs["children"] = children
	attrs["child_count"] = info.ChildCount
	attrs["resource_count"] = info.ResourceCount
	return attrs
}

type containerAttributes struct {
	DefaultChild string `json:"default_child,omitempty"`
}

type containerChildAttributesData struct {
	Name        string                       `json:"name,omitempty"`
	ChildKind   string                       `json:"child_kind,omitempty"`
	DataType    datatype.DataType            `json:"data_type,omitempty"`
	Format      string                       `json:"format,omitempty"`
	RowCount    *int64                       `json:"row_count,omitempty"`
	ColumnCount *int                         `json:"column_count,omitempty"`
	HasHeader   *bool                        `json:"has_header,omitempty"`
	Refs        []datatype.ContainerChildRef `json:"refs,omitempty"`
	Native      map[string]interface{}       `json:"native,omitempty"`
}

func containerChildAttributes(child datatype.ContainerChildInfo) containerChildAttributesData {
	childFormat := canonicalContainerChildFormat(child)
	return containerChildAttributesData{
		Name:        child.Name,
		ChildKind:   child.ChildKind,
		DataType:    child.DataType,
		Format:      childFormat,
		RowCount:    child.RowCount,
		ColumnCount: child.ColumnCount,
		HasHeader:   child.HasHeader,
		Refs:        child.Refs,
		Native:      filteredContainerChildNative(child.Native),
	}
}

func canonicalContainerChildFormat(child datatype.ContainerChildInfo) string {
	for _, candidate := range []string{
		child.Format,
		commonJSON.InterfaceString(child.Native["format"]),
	} {
		if normalized := format.NormalizeFormat(candidate); normalized != format.FormatUnknown {
			return string(normalized)
		}
	}
	return ""
}

func filteredContainerChildNative(values map[string]interface{}) map[string]interface{} {
	native := map[string]interface{}{}
	for key, value := range values {
		if isContainerChildProtocolProperty(key) || isContainerChildSchemaProperty(key) {
			continue
		}
		native[key] = value
	}
	if len(native) == 0 {
		return nil
	}
	return native
}

func isContainerChildProtocolProperty(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "name", "child_kind", "data_type", "format", "native", "refs", "ref_paths", "components", "component_paths", "organization":
		return true
	default:
		return false
	}
}

func isContainerChildSchemaProperty(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "columns", "fields", "schema", "table_info", "type_info":
		return true
	default:
		return false
	}
}
