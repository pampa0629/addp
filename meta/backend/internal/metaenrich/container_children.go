package metaenrich

import (
	"context"
	"io"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
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
		children = append(children, commonJSON.MapFromStruct(buildContainerChildAttributes(child)))
	}
	containerAttrs := commonJSON.MapFromStruct(containerAttributes{
		DefaultChild: info.DefaultChild,
		Native:       info.Native,
	})
	if containerAttrs == nil {
		containerAttrs = map[string]interface{}{}
	}
	containerAttrs["children"] = children
	containerAttrs["child_count"] = info.ChildCount
	containerAttrs["resource_count"] = info.ResourceCount
	metaattr.UpsertNested(attrs, "type_info", "container", containerAttrs)
	return nil
}

type containerAttributes struct {
	DefaultChild string                 `json:"default_child,omitempty"`
	Native       map[string]interface{} `json:"native,omitempty"`
}

type containerChildAttributes struct {
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

func buildContainerChildAttributes(child datatype.ContainerChildInfo) containerChildAttributes {
	childFormat := format.FormatType(strings.TrimSpace(child.Format))
	if childFormat == "" {
		childFormat = format.FormatType(strings.TrimSpace(interfaceString(child.Native["format"])))
	}
	return containerChildAttributes{
		Name:        child.Name,
		ChildKind:   child.ChildKind,
		DataType:    child.DataType,
		Format:      string(childFormat),
		RowCount:    child.RowCount,
		ColumnCount: child.ColumnCount,
		HasHeader:   child.HasHeader,
		Refs:        child.Refs,
		Native:      filteredContainerChildNative(child.Native),
	}
}

func filteredContainerChildNative(values map[string]interface{}) map[string]interface{} {
	native := map[string]interface{}{}
	for key, value := range values {
		if isContainerChildProtocolProperty(key) {
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
