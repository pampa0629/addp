package catalogutil

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
	if modelProvider, ok := p.(plugin.CatalogModelProvider); ok {
		return plugin.CatalogItemTerm(modelProvider.CatalogModel()) == term
	}
	capabilities := p.Capabilities()
	if capabilities.Storage == nil || capabilities.Storage.CatalogModel == nil {
		return false
	}
	return plugin.CatalogItemTerm(*capabilities.Storage.CatalogModel) == term
}

func ObjectMetadata(ctx context.Context, metadataProvider plugin.ItemMetadataProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket, objectPath string) (*plugin.FileMetadata, error) {
	if metadataProvider == nil {
		return nil, fs.ErrNotExist
	}
	item, err := metadataProvider.DescribeItem(ctx, connInfo, plugin.ObjectItemPath(engineID, bucket, objectPath), plugin.MetadataOptions{})
	if err != nil {
		return nil, err
	}
	return ItemMetadataToFileMetadata(item, bucket+"/"+strings.Trim(objectPath, "/")), nil
}

func OpenObjectContent(ctx context.Context, contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket, objectPath string) (io.ReadCloser, error) {
	if contentReader == nil {
		return nil, fs.ErrNotExist
	}
	return contentReader.OpenContent(ctx, connInfo, plugin.ObjectItemPath(engineID, bucket, objectPath), plugin.ReadOptions{})
}

func NodePhysicalPath(node plugin.CatalogNode) string {
	if path := StringAttribute(node.Attributes, "path"); path != "" {
		return path
	}
	return node.Path.StringPath()
}

func ItemMetadataToFileMetadata(item *plugin.ItemMetadata, fallbackPath string) *plugin.FileMetadata {
	if item == nil {
		return &plugin.FileMetadata{Name: pathBase(fallbackPath), Path: fallbackPath}
	}
	name := StringAttribute(item.Attributes, "name")
	if name == "" {
		name = pathBase(fallbackPath)
	}
	path := StringAttribute(item.Attributes, "path")
	if path == "" {
		path = fallbackPath
	}
	updatedAt := time.Time{}
	if item.UpdatedAt != nil {
		updatedAt = *item.UpdatedAt
	}
	return &plugin.FileMetadata{
		Name:        name,
		Path:        path,
		Size:        Int64Stat(item.Stats, "size_bytes"),
		ModifiedAt:  updatedAt,
		ContentType: StringAttribute(item.Attributes, "content_type"),
		ETag:        StringAttribute(item.Attributes, "etag"),
	}
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

func Int64Stat(stats map[string]interface{}, key string) int64 {
	if stats == nil {
		return 0
	}
	return commonJSON.InterfaceInt64(stats[key])
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
	case "extractor_available", "text_extracted", "status", "reason", "extractor", "plain_text_preview", "text_truncated", "index_ref":
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
