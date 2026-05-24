package extractor

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
)

type InlineObjectMetadataExtractor struct {
	log *slog.Logger
}

func NewInlineObjectMetadataExtractor(log *slog.Logger) *InlineObjectMetadataExtractor {
	return &InlineObjectMetadataExtractor{log: log}
}

func (e *InlineObjectMetadataExtractor) ShouldExtract(key, contentType string, sizeBytes int64) bool {
	formatType := format.MIMEToFormat(contentType)
	if formatType == format.FormatUnknown {
		formatType = format.DetectFormat(key, nil)
	}
	capability, ok := format.GetFormatCapability(formatType)
	if !ok || capability.DataType != format.FormatDataTypeMedia {
		if e.log != nil {
			e.log.Debug("跳过非媒体类型", "content_type", contentType, "format", formatType, "key", key)
		}
		return false
	}
	if _, err := format.GetMediaInfoProvider(formatType); err != nil {
		if e.log != nil {
			e.log.Debug("跳过无媒体信息 Provider 的格式", "content_type", contentType, "format", formatType, "key", key)
		}
		return false
	}

	const maxSizeForExtraction = 100 * 1024 * 1024
	if sizeBytes > maxSizeForExtraction {
		if e.log != nil {
			e.log.Debug("文件过大，跳过元数据提取", "size", sizeBytes)
		}
		return false
	}
	return true
}

func (e *InlineObjectMetadataExtractor) Extract(
	ctx context.Context,
	resource *commonModels.Engine,
	bucket, key, contentType string,
	size int64,
	lastModified time.Time,
	etag string,
	openContent func() (io.ReadCloser, error),
) map[string]interface{} {
	if e == nil || openContent == nil {
		return nil
	}

	formatType := format.MIMEToFormat(contentType)
	if formatType == format.FormatUnknown {
		formatType = format.DetectFormat(key, nil)
	}
	provider, err := format.GetMediaInfoProvider(formatType)
	if err != nil {
		if e.log != nil {
			e.log.Debug("无可用的媒体信息 Provider",
				"content_type", contentType,
				"format", formatType,
				"key", key)
		}
		return nil
	}

	obj, err := openContent()
	if err != nil {
		if e.log != nil {
			e.log.Warn("获取对象内容失败",
				"bucket", bucket,
				"key", key,
				"error", err)
		}
		return nil
	}
	defer obj.Close()

	mediaInfo, err := provider.DescribeMedia(ctx, obj, nil)
	if err != nil {
		if e.log != nil {
			e.log.Debug("提取媒体信息失败",
				"bucket", bucket,
				"key", key,
				"format", formatType,
				"error", err)
		}
		return nil
	}
	attrs := MediaInfoAttributes(mediaInfo)
	if len(attrs) == 0 {
		return nil
	}
	if contentType != "" {
		metaattr.SetStorage(attrs, "content_type", contentType)
	}
	metaattr.SetStorage(attrs, "etag", etag)

	if e.log != nil {
		e.log.Debug("成功提取媒体信息",
			"bucket", bucket,
			"key", key,
			"attrs", attrs)
	}

	return attrs
}

func MediaInfoAttributes(info *format.MediaDescribeResult) models.JSONMap {
	attrs := models.JSONMap{}
	if info == nil || info.Media == nil {
		return attrs
	}
	media := map[string]interface{}{}
	if info.Media.Kind != "" {
		media["kind"] = string(info.Media.Kind)
	}
	if info.Media.Width > 0 {
		media["width"] = info.Media.Width
	}
	if info.Media.Height > 0 {
		media["height"] = info.Media.Height
	}
	if info.Media.DurationMS != nil {
		media["duration_ms"] = *info.Media.DurationMS
	}
	if info.Media.Encoding != "" {
		media["encoding"] = info.Media.Encoding
	}
	if info.Media.ColorSpace != "" {
		media["color_space"] = info.Media.ColorSpace
	}
	if info.Media.MIMEType != "" {
		media["mime_type"] = info.Media.MIMEType
	}
	if info.Media.SizeBytes != nil {
		media["size_bytes"] = *info.Media.SizeBytes
	}
	if len(media) > 0 {
		metaattr.UpsertNested(attrs, "type_info", "media", media)
	}
	if spatialAttrs := spatialInfoAttributes(info.Spatial); len(spatialAttrs) > 0 {
		metaattr.UpsertNested(attrs, "capabilities", "spatial", spatialAttrs)
	}
	return attrs
}

func spatialInfoAttributes(info *datatype.SpatialInfo) map[string]interface{} {
	if info == nil {
		return nil
	}
	attrs := map[string]interface{}{}
	if len(info.GeometryColumns) > 0 {
		geometryColumns := make([]map[string]interface{}, 0, len(info.GeometryColumns))
		for _, column := range info.GeometryColumns {
			columnAttrs := map[string]interface{}{}
			if column.Name != "" {
				columnAttrs["name"] = column.Name
			}
			if column.GeometryType != "" {
				columnAttrs["geometry_type"] = column.GeometryType
			}
			if column.SRID != nil {
				columnAttrs["srid"] = *column.SRID
				if len(info.GeometryColumns) == 1 && column.Name == "" {
					attrs["srid"] = *column.SRID
				}
			}
			if column.CRS != "" {
				columnAttrs["crs"] = column.CRS
				if len(info.GeometryColumns) == 1 && column.Name == "" {
					attrs["crs"] = column.CRS
				}
			}
			if column.Dimension != nil {
				columnAttrs["dimension"] = *column.Dimension
			}
			if column.Nullable != nil {
				columnAttrs["nullable"] = *column.Nullable
			}
			if len(columnAttrs) > 0 {
				geometryColumns = append(geometryColumns, columnAttrs)
			}
		}
		if len(geometryColumns) > 0 && geometryColumns[0]["name"] != nil {
			attrs["geometry_columns"] = geometryColumns
		}
	}
	if info.PrimaryGeometryColumn != "" {
		attrs["primary_geometry_column"] = info.PrimaryGeometryColumn
	}
	if info.Extent != nil {
		bbox := *info.Extent
		attrs["extent"] = []float64{bbox[0], bbox[1], bbox[2], bbox[3]}
	}
	if info.HasSpatialIndex != nil {
		attrs["has_spatial_index"] = *info.HasSpatialIndex
	}
	if info.IndexName != "" {
		attrs["index_name"] = info.IndexName
	}
	return attrs
}
