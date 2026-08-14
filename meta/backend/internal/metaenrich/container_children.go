package metaenrich

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

type ContainerInspector interface {
	InspectContainer(ctx context.Context, source *commonModels.Engine, tenantID uint, physicalPath, sourceFormat, sourceLayout string) (*format.ContainerDescribeResult, error)
}

// EnrichRuntimeContainerItem uses the single runtime-bound provider path for
// formats whose native dependencies cannot live in Meta.
func EnrichRuntimeContainerItem(
	ctx context.Context,
	attrs models.JSONMap,
	inspector ContainerInspector,
	source *commonModels.Engine,
	tenantID uint,
	item *metaitem.DetectedItem,
	physicalPath string,
) (bool, error) {
	if item == nil || item.DataType != datatype.Container {
		return false, nil
	}
	formatType := format.NormalizeFormat(item.Format)
	if _, err := format.GetRuntimeContainerInfoProviderFactory(formatType); err != nil {
		return false, nil
	}
	if inspector == nil {
		return true, fmt.Errorf("runtime container deep scan requires a configured inspector")
	}
	result, err := inspector.InspectContainer(ctx, source, tenantID, physicalPath, string(formatType), item.Layout)
	if err != nil {
		return true, err
	}
	if result == nil || result.Container == nil {
		return true, fmt.Errorf("runtime container inspector returned no container type info")
	}
	item.Container = result.Container.Clone()
	metaitem.ApplyContainerInfo(attrs, item)
	if len(result.FormatInfo) > 0 {
		metaattr.MergeStandardAttributes(attrs, metaattr.FormatInfoAttributes(string(formatType), result.FormatInfo))
	}
	return true, nil
}

const (
	containerChildLimit       = 100
	containerSampleChildLimit = 100
)

// EnrichContainerChildren 枚举容器内部对象，并写入 type_info.container.children。
func EnrichContainerChildren(ctx context.Context, attrs models.JSONMap, detected *metaitem.DetectedItem, reader io.Reader) error {
	if attrs == nil || detected == nil || reader == nil || detected.DataType != datatype.Container {
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
	if infoProvider, err := format.GetFormatInfoProvider(formatType); err == nil {
		formatInfo, err := infoProvider.DescribeFormat(ctx, bytes.NewReader(data), format.ContainerParseOptions(containerChildLimit, 0))
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
	detected.Container = info.Clone()
	metaitem.ApplyContainerInfo(attrs, detected)
	return nil
}
