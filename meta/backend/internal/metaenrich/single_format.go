package metaenrich

import (
	"context"
	"io"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaitem"
)

const singleFileFormatPeekBytes int64 = 8192

// DetectSingleFileFormat 通过文件内容前缀识别 single 文件的格式。
func DetectSingleFileFormat(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	catalogPath plugin.EngineCatalogPath,
	fallbackPath string,
) (format.FormatType, error) {
	if contentReader == nil {
		return format.FormatUnknown, nil
	}

	reader, err := openSingleFilePeekReader(ctx, contentReader, connInfo, catalogPath)
	if err != nil {
		return format.FormatUnknown, err
	}
	defer reader.Close()

	peek, err := io.ReadAll(io.LimitReader(reader, singleFileFormatPeekBytes))
	if err != nil {
		return format.FormatUnknown, err
	}
	return format.DetectFormat(fallbackPath, peek), nil
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
