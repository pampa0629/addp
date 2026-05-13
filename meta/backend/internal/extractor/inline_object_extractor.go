package extractor

import (
	"context"
	"log/slog"
	"time"

	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/objectstore"
	"github.com/minio/minio-go/v7"
)

type InlineObjectMetadataExtractor struct {
	clientManager *objectstore.ClientManager
	log           *slog.Logger
}

func NewInlineObjectMetadataExtractor(clientManager *objectstore.ClientManager, log *slog.Logger) *InlineObjectMetadataExtractor {
	return &InlineObjectMetadataExtractor{clientManager: clientManager, log: log}
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
) map[string]interface{} {
	if e == nil || e.clientManager == nil {
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

	client, err := e.clientManager.GetByResource(resource)
	if err != nil {
		if e.log != nil {
			e.log.Warn("创建对象存储客户端失败",
				"engine_id", resource.ID,
				"error", err)
		}
		return nil
	}

	const headerSize = 16 * 1024
	opts := minio.GetObjectOptions{}
	if size > headerSize {
		opts.SetRange(0, headerSize-1)
	}

	obj, err := client.GetObject(ctx, bucket, key, opts)
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

func MediaInfoAttributes(info *format.MediaInfo) models.JSONMap {
	attrs := models.JSONMap{}
	if info == nil {
		return attrs
	}
	media := map[string]interface{}{}
	if info.MediaType != "" {
		media["kind"] = info.MediaType
	}
	if info.Width > 0 {
		media["width"] = info.Width
	}
	if info.Height > 0 {
		media["height"] = info.Height
	}
	if info.DurationMS != nil {
		media["duration_ms"] = *info.DurationMS
	}
	if info.Encoding != "" {
		media["encoding"] = info.Encoding
	}
	if info.ColorSpace != "" {
		media["color_space"] = info.ColorSpace
	}
	if info.MIMEType != "" {
		media["mime_type"] = info.MIMEType
	}
	if info.SizeBytes != nil {
		media["size_bytes"] = *info.SizeBytes
	}
	if len(media) > 0 {
		metaattr.UpsertNested(attrs, "type_info", "media", media)
	}
	if len(info.SpatialAttrs) > 0 {
		metaattr.UpsertNested(attrs, "capabilities", "spatial", info.SpatialAttrs)
	}
	return attrs
}
