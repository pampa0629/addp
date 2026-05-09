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
		e.log.Debug("正在获取文件元数据提取器", "content_type", contentType, "key", key)
	}
	parser := format.GetExtractor(contentType)
	if parser == nil && strings.HasPrefix(contentType, "image/") {
		parser = format.GetExtractor("image/*")
	}
	if parser == nil {
		if e.log != nil {
			e.log.Debug("无可用的文件元数据提取器",
				"content_type", contentType,
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

	extractedMeta, err := parser.Extract(ctx, format.ExtractInput{
		ObjectKey:    key,
		ContentType:  contentType,
		Size:         size,
		Reader:       obj,
		LastModified: lastModified,
		ETag:         etag,
	})
	if err != nil {
		if e.log != nil {
			e.log.Debug("提取对象元数据失败",
				"bucket", bucket,
				"key", key,
				"error", err)
		}
		return nil
	}
	if extractedMeta == nil {
		return nil
	}
	if extractedMeta.BasicInfo.FileName == "" {
		extractedMeta.BasicInfo.FileName = filepath.Base(key)
	}
	if extractedMeta.BasicInfo.ContentType == "" {
		extractedMeta.BasicInfo.ContentType = contentType
	}
	if extractedMeta.BasicInfo.Size == 0 {
		extractedMeta.BasicInfo.Size = size
	}
	if extractedMeta.BasicInfo.LastModified.IsZero() {
		extractedMeta.BasicInfo.LastModified = lastModified
	}
	if extractedMeta.BasicInfo.ETag == "" {
		extractedMeta.BasicInfo.ETag = etag
	}

	if e.log != nil {
		e.log.Debug("成功提取对象元数据",
			"bucket", bucket,
			"key", key,
			"attrs", extractedMeta.CustomAttrs)
	}

	return extractedMeta
}
