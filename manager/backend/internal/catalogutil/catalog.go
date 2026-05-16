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

func Int64Stat(stats map[string]interface{}, key string) int64 {
	if stats == nil {
		return 0
	}
	return commonJSON.InterfaceInt64(stats[key])
}

func attributeSectionsForKey(key string) []string {
	switch key {
	case "organization", "data_type", "format", "component_files", "file_count", "scope_exclusive", "claim_policy":
		return []string{"item"}
	case "bucket", "path", "name", "physical_path", "size_bytes", "size", "total_size", "content_type", "last_modified_at", "etag":
		return []string{"storage"}
	case "fields", "primary_key", "indexes", "row_count", "document_count":
		return []string{"type_info.table"}
	case "width", "height", "duration", "codec", "page_count", "word_count":
		return []string{"type_info.media", "type_info.document"}
	case "spatial", "geometry_columns", "primary_geometry_column", "extent", "has_spatial_index":
		return []string{"capabilities.spatial"}
	case "metadata_extracted", "extractor_available", "extracted_metadata", "plain_text_preview":
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
