package metaenrich

import (
	"bytes"
	"context"
	"io"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
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
	formatType := format.NormalizeFormat(detected.Format)
	if formatType == format.FormatUnknown {
		return nil
	}
	if _, err := format.GetContainerInfoProvider(formatType); err != nil {
		return nil
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if formatProvider, err := format.GetFormatInfoProvider(formatType); err == nil {
		formatInfo, err := formatProvider.DescribeFormat(ctx, bytes.NewReader(data), format.ContainerParseOptions(containerChildLimit, 0))
		if err != nil {
			return err
		}
		metaattr.MergeStandardAttributes(attrs, metaattr.FormatInfoAttributes(string(formatType), formatInfo))
	}
	return enrichContainerChildrenFromProvider(ctx, attrs, detected, formatType, bytes.NewReader(data), format.ContainerParseOptions(containerChildLimit, 0))
}

func enrichContainerChildrenFromProvider(
	ctx context.Context,
	attrs models.JSONMap,
	detected *metaitem.DetectedItem,
	formatType format.FormatType,
	reader io.Reader,
	options *format.ParseOptions,
) error {
	if detected == nil {
		return nil
	}
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
	metadata := &plugin.ItemMetadata{
		Kind:      detected.ItemType,
		Container: info,
	}
	detected.Container = plugin.ItemMetadataContainerInfo(metadata)
	metaitem.ApplyContainerInfo(attrs, detected)
	return nil
}
