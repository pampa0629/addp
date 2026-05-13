package metaitem

import (
	"context"
	"io"

	"github.com/addp/common/format"
	"github.com/addp/meta/internal/dataitem"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
)

const (
	containerChildLimit       = 100
	containerSampleChildLimit = 100
)

// EnrichContainerChildren 枚举容器内部对象，并写入 type_info.container.children。
func EnrichContainerChildren(ctx context.Context, attrs models.JSONMap, detected *DetectedItem, reader io.Reader) error {
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
		attrs := map[string]interface{}{
			"name":      child.Name,
			"kind":      child.Kind,
			"data_type": child.DataType,
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
		for key, value := range child.Properties {
			attrs[key] = value
		}
		children = append(children, attrs)
	}
	metaattr.UpsertNested(attrs, "type_info", "container", map[string]interface{}{
		"children":       children,
		"child_count":    info.ChildCount,
		"default_child":  info.DefaultChild,
		"resource_count": info.ResourceCount,
	})
	if len(info.FormatInfo) > 0 {
		metaattr.UpsertNested(attrs, "format_info", string(formatType), info.FormatInfo)
	}
	return nil
}
