package metaenrich

import (
	"context"
	"io"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

const (
	containerChildLimit       = 100
	containerSampleChildLimit = 100
)

// EnrichContainerChildren 枚举容器内部对象，并写入 type_info.container.children。
func EnrichContainerChildren(ctx context.Context, attrs models.JSONMap, detected *metaitem.DetectedItem, reader io.Reader) error {
	if attrs == nil || detected == nil || reader == nil || detected.DataType != dataitem.DataTypeContainer {
		return nil
	}
	formatType := format.FormatType(detected.Format)
	if _, err := format.GetContainerInfoProvider(formatType); err != nil {
		return nil
	}
	return enrichContainerChildrenFromProvider(ctx, attrs, formatType, reader, format.ContainerParseOptions(containerChildLimit, 0))
}

func enrichContainerChildrenFromProvider(
	ctx context.Context,
	attrs models.JSONMap,
	formatType format.FormatType,
	reader io.Reader,
	options *format.ParseOptions,
) error {
	provider, err := format.GetContainerInfoProvider(formatType)
	if err != nil {
		return err
	}
	info, err := provider.DescribeContainer(ctx, reader, options)
	if err != nil {
		return err
	}
	if info == nil {
		return nil
	}

	children := make([]map[string]interface{}, 0, len(info.Children))
	for _, child := range info.Children {
		childFormat := format.FormatType(strings.TrimSpace(child.Format))
		if childFormat == "" {
			childFormat = format.FormatType(strings.TrimSpace(interfaceString(child.Native["format"])))
		}
		attrs := map[string]interface{}{
			"name":       child.Name,
			"child_kind": child.ChildKind,
			"data_type":  child.DataType,
		}
		if childFormat != "" {
			attrs["format"] = string(childFormat)
		}
		if len(child.Refs) > 0 {
			attrs["refs"] = containerChildRefAttributes(child.Refs)
		}
		if child.RowCount != nil {
			attrs["row_count"] = *child.RowCount
		}
		if child.ColumnCount != nil {
			attrs["column_count"] = *child.ColumnCount
		}
		if child.HasHeader != nil {
			attrs["has_header"] = *child.HasHeader
		}
		native := map[string]interface{}{}
		for key, value := range child.Native {
			if isContainerChildProtocolProperty(key) {
				continue
			}
			native[key] = value
		}
		if len(native) > 0 {
			attrs["native"] = native
		}
		children = append(children, attrs)
	}
	containerAttrs := map[string]interface{}{
		"children":       children,
		"child_count":    info.ChildCount,
		"default_child":  info.DefaultChild,
		"resource_count": info.ResourceCount,
	}
	if len(info.Native) > 0 {
		containerAttrs["native"] = info.Native
	}
	metaattr.UpsertNested(attrs, "type_info", "container", containerAttrs)
	return nil
}

func containerChildRefAttributes(refs []datatype.ContainerChildRef) []map[string]interface{} {
	if len(refs) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, 0, len(refs))
	for _, ref := range refs {
		item := map[string]interface{}{
			"path":     ref.Path,
			"required": ref.Required,
			"primary":  ref.Primary,
		}
		if ref.Role != "" {
			item["role"] = ref.Role
		}
		if ref.Extension != "" {
			item["extension"] = ref.Extension
		}
		result = append(result, item)
	}
	return result
}

func isContainerChildProtocolProperty(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "name", "child_kind", "data_type", "format", "native", "refs", "ref_paths", "components", "component_paths", "organization":
		return true
	default:
		return false
	}
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
