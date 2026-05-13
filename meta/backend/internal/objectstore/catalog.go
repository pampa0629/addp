package objectstore

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metapath"
)

type InlineMetadataExtractor interface {
	ShouldExtract(key, contentType string, sizeBytes int64) bool
	Extract(ctx context.Context, resource *commonModels.Engine, bucket, key, contentType string, size int64, lastModified time.Time, etag string) map[string]interface{}
}

func ListBucketNodes(ctx context.Context, resource *commonModels.Engine, catalogProvider plugin.CatalogProvider) ([]plugin.CatalogNode, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: resource.ID,
	}, plugin.ListOptions{})
	if err != nil {
		return nil, err
	}

	buckets := make([]plugin.CatalogNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == plugin.CatalogKindBucket {
			buckets = append(buckets, node)
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
) ([]plugin.CatalogNode, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), CatalogPath(resource.ID, bucketName, prefix), plugin.ListOptions{Recursive: recursive})
	if err != nil {
		return nil, err
	}

	objects := make([]plugin.CatalogNode, 0, len(nodes))
	for _, node := range nodes {
		if node.IsItem {
			objects = append(objects, node)
		}
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
	objects []plugin.CatalogNode,
	bucket string,
	deepScan bool,
	resource *commonModels.Engine,
	inlineExtractor InlineMetadataExtractor,
) []format.ObjectMetadata {
	metas := make([]format.ObjectMetadata, 0, len(objects))
	for _, obj := range objects {
		key := ObjectKeyFromNode(obj, bucket)
		size, _ := int64Stat(obj.Stats, "size_bytes")
		contentType := stringAttribute(obj.Attributes, "content_type")
		lastModified := timeAttribute(obj.Attributes, "modified_at")
		etag := stringAttribute(obj.Attributes, "etag")
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(key)), ".")
		meta := format.ObjectMetadata{
			Bucket:       bucket,
			Path:         key,
			NodeType:     "object",
			FileType:     ext,
			SizeBytes:    size,
			ObjectCount:  1,
			LastModified: lastModified,
		}
		if deepScan && inlineExtractor != nil && lastModified != nil && inlineExtractor.ShouldExtract(key, contentType, size) {
			meta.ExtractedAttributes = inlineExtractor.Extract(ctx, resource, bucket, key, contentType, size, *lastModified, etag)
		}
		metas = append(metas, meta)
	}
	return metas
}

func ObjectKeyFromNode(node plugin.CatalogNode, bucket string) string {
	rawPath := stringAttribute(node.Attributes, "path")
	if rawPath == "" {
		rawPath = node.Path.StringPath()
	}
	if rawPath != "" {
		_, key := metapath.SplitObjectPath(rawPath)
		if key != "" {
			return key
		}
	}
	return strings.TrimPrefix(node.Path.StringPath(), bucket+"/")
}

func stringAttribute(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	raw, ok := attrs[key]
	if !ok || raw == nil {
		return ""
	}
	if value, ok := raw.(string); ok {
		return value
	}
	return fmt.Sprintf("%v", raw)
}

func timeAttribute(attrs map[string]interface{}, key string) *time.Time {
	if attrs == nil {
		return nil
	}
	raw, ok := attrs[key]
	if !ok || raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case time.Time:
		return &value
	case string:
		parsed, err := time.Parse(time.RFC3339, value)
		if err == nil {
			return &parsed
		}
	}
	return nil
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
