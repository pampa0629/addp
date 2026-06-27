package metaenrich

import (
	"context"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
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
	if item.DataType == datatype.Unknown {
		item.DataType = dataitem.DefaultDataTypeForFormat(item.Format)
	}
	if item.DataType != datatype.Document {
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
	item.Document = info.Clone()
	metaitem.ApplyDocumentInfo(attrs, item)

	if infoProvider, err := format.GetFormatInfoProvider(formatType); err == nil {
		rc, err := contentReader.OpenContent(ctx, connInfo, catalogPathFor(path), plugin.ReadOptions{})
		if err != nil {
			return err
		}
		formatInfo, err := infoProvider.DescribeFormat(ctx, rc, nil)
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
	if item.DataType == datatype.Unknown {
		item.DataType = dataitem.DefaultDataTypeForFormat(item.Format)
	}
	if item.DataType != datatype.Media {
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
	if info.Media != nil {
		item.Media = info.Media.Clone()
		metaitem.ApplyMediaInfo(attrs, item, info.Spatial)
	}
	if len(info.FormatInfo) > 0 {
		metaattr.MergeStandardAttributes(attrs, metaattr.FormatInfoAttributes(string(formatType), info.FormatInfo))
	}
	return nil
}

func EnrichSingleModel3DItem(
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
	if item.DataType == datatype.Unknown {
		item.DataType = dataitem.DefaultDataTypeForFormat(item.Format)
	}
	if item.DataType != datatype.Model3D {
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
	provider, err := format.GetModel3DInfoProvider(formatType)
	if err != nil {
		return nil
	}
	rc, err := contentReader.OpenContent(ctx, connInfo, catalogPathFor(path), plugin.ReadOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()

	info, err := provider.DescribeModel3D(ctx, rc, nil)
	if err != nil {
		return err
	}
	if info == nil {
		return nil
	}
	if info.Model3D != nil {
		item.Model3D = info.Model3D.Clone()
		metaitem.ApplyModel3DInfo(attrs, item, info.Spatial)
	}
	if len(info.FormatInfo) > 0 {
		metaattr.MergeStandardAttributes(attrs, metaattr.FormatInfoAttributes(string(formatType), info.FormatInfo))
	}
	return nil
}

func EnrichSinglePointCloudItem(
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
	if item.DataType == datatype.Unknown {
		item.DataType = dataitem.DefaultDataTypeForFormat(item.Format)
	}
	if item.DataType != datatype.PointCloud {
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
	provider, err := format.GetPointCloudInfoProvider(formatType)
	if err != nil {
		return nil
	}
	rc, err := contentReader.OpenContent(ctx, connInfo, catalogPathFor(path), plugin.ReadOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()

	info, err := provider.DescribePointCloud(ctx, rc, nil)
	if err != nil {
		return err
	}
	if info == nil {
		return nil
	}
	if info.PointCloud != nil {
		item.PointCloud = info.PointCloud.Clone()
		metaitem.ApplyPointCloudInfo(attrs, item, info.Spatial)
	}
	if len(info.FormatInfo) > 0 {
		metaattr.MergeStandardAttributes(attrs, metaattr.FormatInfoAttributes(string(formatType), info.FormatInfo))
	}
	return nil
}
