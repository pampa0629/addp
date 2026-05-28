package metaenrich

import (
	"context"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

func EnrichSingleDocumentItem(
	ctx context.Context,
	attrs models.JSONMap,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	item *metaitem.DetectedItem,
	path string,
	catalogPathFor func(path string) plugin.CatalogPath,
) error {
	if attrs == nil || contentReader == nil || item == nil || path == "" {
		return nil
	}
	if catalogPathFor == nil {
		catalogPathFor = plugin.FileItemPathForEngine(engineID)
	}
	beforeDataType := item.DataType
	beforeFormat := item.Format
	if IsUnknownFormatName(item.Format) {
		detectedFormat, err := DetectSingleFileFormat(ctx, contentReader, connInfo, catalogPathFor(path), path)
		if err != nil {
			return err
		}
		ApplySingleFileFormat(item, detectedFormat)
	}
	if item.DataType == dataitem.DataTypeUnknown {
		item.DataType = dataitem.DetectDataType(item.Format)
	}
	if item.DataType != dataitem.DataTypeDocument {
		return nil
	}
	if item.DataType != beforeDataType || item.Format != beforeFormat {
		metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(item))
	}
	if IsUnknownFormatName(item.Format) {
		return nil
	}
	formatType := format.NormalizeFormat(item.Format)
	if formatType == format.FormatUnknown {
		return nil
	}
	provider, err := format.GetDocumentInfoProvider(formatType)
	if err != nil {
		return nil
	}
	rc, err := contentReader.OpenContent(ctx, connInfo, catalogPathFor(path), plugin.ReadOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()

	info, err := provider.DescribeDocument(ctx, rc, nil)
	if err != nil {
		return err
	}
	metadata := &plugin.ItemMetadata{
		Path:     catalogPathFor(path),
		Kind:     item.ItemType,
		Document: info,
	}
	item.Document = plugin.ItemMetadataDocumentInfo(metadata)
	metaitem.ApplyDocumentInfo(attrs, item)

	if formatProvider, err := format.GetFormatInfoProvider(formatType); err == nil {
		rc, err := contentReader.OpenContent(ctx, connInfo, catalogPathFor(path), plugin.ReadOptions{})
		if err != nil {
			return err
		}
		formatInfo, err := formatProvider.DescribeFormat(ctx, rc, nil)
		closeErr := rc.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		metaattr.MergeStandardAttributes(attrs, metaattr.FormatInfoAttributes(string(formatType), formatInfo))
	}
	return nil
}

func EnrichSingleMediaItem(
	ctx context.Context,
	attrs models.JSONMap,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	item *metaitem.DetectedItem,
	path string,
	catalogPathFor func(path string) plugin.CatalogPath,
) error {
	if attrs == nil || contentReader == nil || item == nil || path == "" {
		return nil
	}
	if catalogPathFor == nil {
		catalogPathFor = plugin.FileItemPathForEngine(engineID)
	}
	beforeDataType := item.DataType
	beforeFormat := item.Format
	if IsUnknownFormatName(item.Format) {
		detectedFormat, err := DetectSingleFileFormat(ctx, contentReader, connInfo, catalogPathFor(path), path)
		if err != nil {
			return err
		}
		ApplySingleFileFormat(item, detectedFormat)
	}
	if item.DataType == dataitem.DataTypeUnknown {
		item.DataType = dataitem.DetectDataType(item.Format)
	}
	if item.DataType != dataitem.DataTypeMedia {
		return nil
	}
	if item.DataType != beforeDataType || item.Format != beforeFormat {
		metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(item))
	}
	if IsUnknownFormatName(item.Format) {
		return nil
	}
	formatType := format.NormalizeFormat(item.Format)
	if formatType == format.FormatUnknown {
		return nil
	}
	provider, err := format.GetMediaInfoProvider(formatType)
	if err != nil {
		return nil
	}
	rc, err := contentReader.OpenContent(ctx, connInfo, catalogPathFor(path), plugin.ReadOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()

	info, err := provider.DescribeMedia(ctx, rc, nil)
	if err != nil {
		return err
	}
	metadata := &plugin.ItemMetadata{
		Path:  catalogPathFor(path),
		Kind:  item.ItemType,
		Media: info.Media,
	}
	item.Media = plugin.ItemMetadataMediaInfo(metadata)
	metaitem.ApplyMediaInfo(attrs, item, info.Spatial)
	return nil
}
