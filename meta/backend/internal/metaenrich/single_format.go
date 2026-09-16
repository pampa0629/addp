package metaenrich

import (
	"context"
	"io"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

const singleFileFormatPeekBytes int64 = 8192

// identifySingleFile 在任何类型 provider 被调用前确认单文件身份。
// 容器插件的表能力属于 child，不能沿用历史快照中误写的父级 table 身份。
func identifySingleFile(ctx context.Context, attrs models.JSONMap, input ResourceAttributesInput) error {
	item := input.Item
	if item.Layout != format.LayoutSingle {
		return nil
	}
	beforeType, beforeFormat := item.DataType, item.Format
	if IsUnknownFormatName(item.Format) && input.ContentReader != nil && input.EngineCatalogPathFor != nil && input.PhysicalPath != "" {
		detected, err := DetectSingleFileFormat(ctx, input.ContentReader, input.ConnInfo, input.EngineCatalogPathFor(input.PhysicalPath), input.PhysicalPath)
		if err != nil {
			return err
		}
		ApplySingleFileFormat(item, detected)
	}
	declaredType := dataitem.DefaultDataTypeForFormat(item.Format)
	if item.DataType == datatype.Unknown || declaredType == datatype.Container {
		item.DataType = declaredType
	}
	if beforeType != item.DataType || beforeFormat != item.Format {
		metaattr.ClearContentDerivedAttributes(attrs)
		metaattr.ClearContentDerivedAttributes(item.Attributes)
		item.Fields = nil
		metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(item))
	}
	return nil
}

// DetectSingleFileFormat 通过文件内容前缀识别 single 文件的格式。
func DetectSingleFileFormat(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	catalogPath plugin.EngineCatalogPath,
	fallbackPath string,
) (format.FormatType, error) {
	peek, err := readSingleFilePeek(ctx, contentReader, connInfo, catalogPath)
	if err != nil {
		return format.FormatUnknown, err
	}
	return format.DetectFormat(fallbackPath, peek), nil
}

func readSingleFilePeek(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	catalogPath plugin.EngineCatalogPath,
) ([]byte, error) {
	if contentReader == nil {
		return nil, nil
	}

	reader, err := openSingleFilePeekReader(ctx, contentReader, connInfo, catalogPath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	peek, err := io.ReadAll(io.LimitReader(reader, singleFileFormatPeekBytes))
	if err != nil {
		return nil, err
	}
	return peek, nil
}

// ApplySingleFileFormat 将识别出的格式写回 detected item。
func ApplySingleFileFormat(item *metaitem.DetectedItem, formatType format.FormatType) {
	if item == nil || formatType == format.FormatUnknown {
		return
	}
	item.Format = string(formatType)
	item.DataType = dataitem.DefaultDataTypeForFormat(item.Format)
}

func openSingleFilePeekReader(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	catalogPath plugin.EngineCatalogPath,
) (io.ReadCloser, error) {
	if rangeReader, ok := contentReader.(plugin.RangeReadableProvider); ok {
		return rangeReader.OpenRange(ctx, connInfo, catalogPath, plugin.ReadOptions{Length: singleFileFormatPeekBytes})
	}
	return contentReader.OpenContent(ctx, connInfo, catalogPath, plugin.ReadOptions{})
}

func IsUnknownFormatName(formatName string) bool {
	return format.NormalizeFormat(formatName) == format.FormatUnknown
}
