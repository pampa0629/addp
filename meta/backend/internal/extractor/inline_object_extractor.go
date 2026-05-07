package extractor

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
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

func (e *InlineObjectMetadataExtractor) ShouldExtract(contentType string, sizeBytes int64) bool {
	if !strings.HasPrefix(contentType, "image/") {
		if e.log != nil {
			e.log.Debug("跳过非图片类型", "content_type", contentType)
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
) *format.ExtractedMetadata {
	if e == nil || e.clientManager == nil {
		return nil
	}

	if e.log != nil {
		e.log.Debug("正在获取对象信息解析器", "content_type", contentType, "key", key)
	}
	parser, err := format.GetObjectInfoParser(contentType)
	if err != nil {
		if e.log != nil {
			e.log.Debug("获取解析器失败，尝试通配符匹配",
				"content_type", contentType,
				"error", err,
				"key", key)
		}
		if strings.HasPrefix(contentType, "image/") {
			parser, err = format.GetObjectInfoParser("image/*")
			if err != nil {
				if e.log != nil {
					e.log.Warn("通配符解析器也失败",
						"content_type", contentType,
						"error", err,
						"key", key)
				}
				return nil
			}
		} else {
			if e.log != nil {
				e.log.Debug("无可用的元数据解析器",
					"content_type", contentType,
					"key", key)
			}
			return nil
		}
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

	basicInfo := format.ObjectBasicInfo{
		Key:         key,
		SizeBytes:   size,
		ContentType: contentType,
		ETag:        etag,
		ModifiedAt:  lastModified,
	}

	objectInfo, err := parser.ParseObjectInfo(ctx, obj, basicInfo)
	if err != nil {
		if e.log != nil {
			e.log.Debug("提取对象元数据失败",
				"bucket", bucket,
				"key", key,
				"error", err)
		}
		return nil
	}

	extractedMeta := &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{
			FileName:     filepath.Base(key),
			FileType:     contentType,
			Size:         size,
			ContentType:  contentType,
			LastModified: lastModified,
			ETag:         etag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	if imageInfo := objectInfo.GetImageInfo(); imageInfo != nil {
		extractedMeta.CustomAttrs["width"] = imageInfo.Width
		extractedMeta.CustomAttrs["height"] = imageInfo.Height
		extractedMeta.CustomAttrs["format"] = imageInfo.Format
		extractedMeta.CustomAttrs["color_space"] = imageInfo.ColorSpace
		if imageInfo.BitDepth > 0 {
			extractedMeta.CustomAttrs["bit_depth"] = imageInfo.BitDepth
		}
		if imageInfo.HasAlpha {
			extractedMeta.CustomAttrs["has_alpha"] = true
		}
	}

	if e.log != nil {
		e.log.Debug("成功提取对象元数据",
			"bucket", bucket,
			"key", key,
			"attrs", extractedMeta.CustomAttrs)
	}

	return extractedMeta
}
