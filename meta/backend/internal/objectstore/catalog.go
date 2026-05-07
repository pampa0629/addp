package objectstore

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metapath"
)

type InlineMetadataExtractor interface {
	ShouldExtract(contentType string, sizeBytes int64) bool
	Extract(ctx context.Context, resource *commonModels.Engine, bucket, key, contentType string, size int64, lastModified time.Time, etag string) *format.ExtractedMetadata
}

func ListBuckets(ctx context.Context, resource *commonModels.Engine, catalogProvider plugin.CatalogProvider) ([]plugin.BucketInfo, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: resource.ID,
	}, plugin.ListOptions{})
	if err != nil {
		return nil, err
	}

	buckets := make([]plugin.BucketInfo, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == plugin.CatalogKindBucket {
			buckets = append(buckets, plugin.BucketInfo{Name: node.Name})
		}
	}
	return buckets, nil
}

func ListObjects(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	bucketName, prefix string,
	recursive bool,
) ([]plugin.ObjectInfo, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), CatalogPath(resource.ID, bucketName, prefix), plugin.ListOptions{Recursive: recursive})
	if err != nil {
		return nil, err
	}

	objects := make([]plugin.ObjectInfo, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsItem {
			continue
		}
		key := strings.TrimPrefix(node.Path.StringPath(), bucketName+"/")
		if raw := commonJSON.String(node.Attributes, "storage", "path"); raw != "" {
			_, parsedKey := metapath.SplitObjectPath(raw)
			key = parsedKey
		}
		size, _ := int64Stat(node.Stats, "size_bytes")
		object := plugin.ObjectInfo{
			Bucket:      bucketName,
			Key:         key,
			Size:        size,
			ContentType: commonJSON.String(node.Attributes, "storage", "content_type"),
		}
		if modifiedAt := commonJSON.TimePtr(node.Attributes, "storage", "modified_at"); modifiedAt != nil {
			object.LastModified = *modifiedAt
		}
		if etag := commonJSON.String(node.Attributes, "storage", "etag"); etag != "" {
			object.ETag = etag
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func CatalogPath(engineID uint, bucketName, prefix string) plugin.CatalogPath {
	path := plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
	}
	if bucketName == "" {
		return path
	}
	path.Segments = append(path.Segments, plugin.CatalogSegment{
		Term: plugin.CatalogTermBucket,
		Kind: plugin.CatalogKindBucket,
		Name: bucketName,
	})
	trimmed := strings.Trim(prefix, "/")
	if trimmed == "" {
		return path
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" {
			continue
		}
		path.Segments = append(path.Segments, plugin.CatalogSegment{
			Term: plugin.CatalogTermPrefix,
			Kind: plugin.CatalogKindPrefix,
			Name: part,
		})
	}
	return path
}

func ConvertObjectsToMetadata(
	ctx context.Context,
	objects []plugin.ObjectInfo,
	bucket string,
	deepScan bool,
	resource *commonModels.Engine,
	inlineExtractor InlineMetadataExtractor,
) []format.ObjectMetadata {
	metas := make([]format.ObjectMetadata, 0, len(objects))
	for _, obj := range objects {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(obj.Key)), ".")
		meta := format.ObjectMetadata{
			Bucket:       bucket,
			Path:         obj.Key,
			NodeType:     "object",
			FileType:     ext,
			SizeBytes:    obj.Size,
			ObjectCount:  1,
			LastModified: &obj.LastModified,
		}
		if deepScan && inlineExtractor != nil && inlineExtractor.ShouldExtract(obj.ContentType, obj.Size) {
			meta.ExtractedMetadata = inlineExtractor.Extract(ctx, resource, bucket, obj.Key, obj.ContentType, obj.Size, obj.LastModified, obj.ETag)
		}
		metas = append(metas, meta)
	}
	return metas
}

func int64Stat(stats map[string]interface{}, key string) (int64, bool) {
	if stats == nil {
		return 0, false
	}
	raw, ok := stats[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case uint:
		return int64(v), true
	case float64:
		return int64(v), true
	case string:
		var parsed int64
		if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}
