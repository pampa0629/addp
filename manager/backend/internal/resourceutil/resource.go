package resourceutil

import (
	"context"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
)

func ItemTermMatches(engineType, term string) bool {
	if strings.TrimSpace(term) == "" {
		return false
	}
	p, err := plugin.Get(strings.TrimSpace(engineType))
	if err != nil {
		return false
	}
	if modelProvider, ok := p.(plugin.EngineCatalogModelProvider); ok {
		return plugin.EngineCatalogLeafTerm(modelProvider.EngineCatalogModel()) == term
	}
	capabilities := p.Capabilities()
	if capabilities.Storage == nil || capabilities.Storage.CatalogModel == nil {
		return false
	}
	return plugin.EngineCatalogLeafTerm(*capabilities.Storage.CatalogModel) == term
}

func ObjectStorageFacts(ctx context.Context, factsProvider plugin.EngineCatalogFactsProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket, objectPath string) (*plugin.StorageObjectFacts, error) {
	if factsProvider == nil {
		return nil, fs.ErrNotExist
	}
	item, err := factsProvider.DescribeEngineCatalogFacts(ctx, connInfo, plugin.ObjectItemPath(engineID, bucket, objectPath), plugin.EngineCatalogFactsOptions{})
	if err != nil {
		return nil, err
	}
	return EngineCatalogFactsToStorageObjectFacts(item, bucket+"/"+strings.Trim(objectPath, "/")), nil
}

func OpenObjectContent(ctx context.Context, contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket, objectPath string) (io.ReadCloser, error) {
	if contentReader == nil {
		return nil, fs.ErrNotExist
	}
	return contentReader.OpenContent(ctx, connInfo, plugin.ObjectItemPath(engineID, bucket, objectPath), plugin.ReadOptions{})
}

func NodePhysicalPath(node plugin.EngineCatalogEntry) string {
	if node.Storage != nil && node.Storage.Path != "" {
		return node.Storage.Path
	}
	return node.Path.StringPath()
}

func EngineCatalogFactsToStorageObjectFacts(item *plugin.EngineCatalogFacts, fallbackPath string) *plugin.StorageObjectFacts {
	if item == nil {
		return &plugin.StorageObjectFacts{Name: pathBase(fallbackPath), Path: fallbackPath}
	}
	storage := item.Storage
	name := ""
	if storage != nil {
		name = storage.Name
	}
	if name == "" {
		name = pathBase(fallbackPath)
	}
	path := ""
	if storage != nil {
		path = storage.Path
	}
	if path == "" {
		path = fallbackPath
	}
	updatedAt := time.Time{}
	if item.UpdatedAt != nil {
		updatedAt = *item.UpdatedAt
	}
	return &plugin.StorageObjectFacts{
		Name:        name,
		Path:        path,
		Size:        engineCatalogStorageSizeBytes(storage),
		ModifiedAt:  updatedAt,
		ContentType: engineCatalogStorageContentType(storage),
		ETag:        engineCatalogStorageETag(storage),
	}
}

func engineCatalogStorageSizeBytes(storage *plugin.EngineCatalogStorageFacts) int64 {
	if storage == nil || storage.SizeBytes == nil {
		return 0
	}
	return *storage.SizeBytes
}

func engineCatalogStorageContentType(storage *plugin.EngineCatalogStorageFacts) string {
	if storage == nil {
		return ""
	}
	return storage.ContentType
}

func engineCatalogStorageETag(storage *plugin.EngineCatalogStorageFacts) string {
	if storage == nil {
		return ""
	}
	return storage.ETag
}

func StringAttribute(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	for _, section := range attributeSectionsForKey(key) {
		if value := commonJSON.StringFromSections(attrs, key, section); value != "" {
			return value
		}
	}
	return ""
}

func MapAttribute(attrs map[string]interface{}, key string) map[string]interface{} {
	if attrs == nil {
		return nil
	}
	for _, section := range attributeSectionsForKey(key) {
		if sectionAttrs := commonJSON.Section(attrs, section); len(sectionAttrs) > 0 {
			if value := commonJSON.InterfaceMap(sectionAttrs[key]); len(value) > 0 {
				return value
			}
		}
	}
	return nil
}

func StringSliceAttribute(attrs map[string]interface{}, key string) []string {
	if attrs == nil {
		return nil
	}
	for _, section := range attributeSectionsForKey(key) {
		if values := stringSlice(commonJSON.Value(attrs, section, key)); len(values) > 0 {
			return values
		}
	}
	return nil
}

func Int64Attribute(attrs map[string]interface{}, key string) int64 {
	if attrs == nil {
		return 0
	}
	for _, section := range attributeSectionsForKey(key) {
		if value := commonJSON.Int64(attrs, section, key); value != 0 {
			return value
		}
	}
	return 0
}

func stringSlice(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := commonJSON.InterfaceString(item); text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

// attributeSectionsForKey 仅用于 Manager 已持有 Meta snapshot 时的展示/预览字段读取。
// 新的跨模块查询语义应优先由 Meta API 提供，不应通过在这里追加 key 来绕过 Meta 边界。
func attributeSectionsForKey(key string) []string {
	switch key {
	case "layout", "data_type", "format", "refs", "file_count", "scope_exclusive", "claim_policy":
		return []string{"item"}
	case "bucket", "path", "name", "physical_path", "size", "total_size", "content_type", "last_modified_at", "etag":
		return []string{"storage"}
	case "fields", "primary_key", "row_count":
		return []string{"type_info.table"}
	case "kind", "width", "height", "duration_ms", "mime_type", "color_space":
		return []string{"type_info.media"}
	case "size_bytes":
		return []string{"storage", "type_info.media"}
	case "encoding":
		return []string{"type_info.document", "type_info.media"}
	case "page_count", "word_count":
		return []string{"type_info.document"}
	case "spatial", "geometry_columns", "primary_geometry_column", "extent", "has_spatial_index":
		return []string{"capabilities.spatial"}
	case "extractor_available", "text_extracted", "status", "reason", "extractor", "text_truncated", "index_ref":
		return []string{"capabilities.extraction"}
	default:
		return nil
	}
}

func pathBase(path string) string {
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		return ""
	}
	idx := strings.LastIndex(trimmed, "/")
	if idx < 0 {
		return trimmed
	}
	return trimmed[idx+1:]
}
