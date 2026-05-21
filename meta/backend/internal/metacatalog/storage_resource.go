package metacatalog

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

// StorageResource is Meta's normalized view of a catalog item that can be
// consumed by data item detectors. Engine-specific hierarchy stays in
// CatalogPath/CatalogNode; this type only carries scan-time item facts.
type StorageResource struct {
	RootName            string
	Path                string
	FullPath            string
	NodeType            string
	Format              string
	SizeBytes           int64
	ObjectCount         int64
	LastModified        *time.Time
	ContentType         string
	ETag                string
	CatalogPath         plugin.CatalogPath
	ExtractedAttributes map[string]interface{}
}

func ObjectStorageResourceFromNode(bucket string, node plugin.CatalogNode) StorageResource {
	itemPath := objectKeyFromCatalogNode(node, bucket)
	contentType := catalogNodeStringAttribute(node.Attributes, "content_type")
	lastModified := catalogNodeTimeAttribute(node.Attributes, "modified_at")
	etag := catalogNodeStringAttribute(node.Attributes, "etag")
	formatName := strings.TrimPrefix(strings.ToLower(filepath.Ext(itemPath)), ".")
	if formatName == "" {
		if detected := format.MIMEToFormat(contentType); detected != format.FormatUnknown {
			formatName = string(detected)
		}
	}
	return StorageResource{
		RootName:     bucket,
		Path:         itemPath,
		FullPath:     joinCatalogPathParts(bucket, itemPath),
		NodeType:     node.Kind,
		Format:       formatName,
		SizeBytes:    storageResourceInt64Stat(node.Stats, "size_bytes"),
		ObjectCount:  1,
		LastModified: lastModified,
		ContentType:  contentType,
		ETag:         etag,
		CatalogPath:  node.Path,
	}
}

func (r StorageResource) FileEntry() plugin.FileEntry {
	modifiedAt := time.Time{}
	if r.LastModified != nil {
		modifiedAt = *r.LastModified
	}
	entryPath := r.FullPath
	if entryPath == "" {
		entryPath = r.Path
	}
	return plugin.FileEntry{
		Name:        path.Base(r.Path),
		Path:        entryPath,
		CatalogPath: r.CatalogPath,
		Size:        r.SizeBytes,
		ModifiedAt:  modifiedAt,
		ContentType: r.ContentType,
	}
}

func objectKeyFromCatalogNode(node plugin.CatalogNode, bucket string) string {
	rawPath := catalogNodeStringAttribute(node.Attributes, "path")
	if rawPath == "" {
		rawPath = node.Path.StringPath()
	}
	if rawPath != "" {
		if key := trimCatalogRoot(rawPath, bucket); key != "" {
			return key
		}
	}
	return strings.TrimPrefix(node.Path.StringPath(), strings.Trim(bucket, "/")+"/")
}

func trimCatalogRoot(value, root string) string {
	value = strings.Trim(value, "/")
	root = strings.Trim(root, "/")
	if root == "" {
		return value
	}
	if value == root {
		return ""
	}
	return strings.TrimPrefix(value, root+"/")
}

func catalogNodeStringAttribute(attrs map[string]interface{}, key string) string {
	if attrs == nil {
		return ""
	}
	if storageRaw, ok := attrs["storage"].(map[string]interface{}); ok {
		if value := catalogNodeStringAttribute(storageRaw, key); value != "" {
			return value
		}
	}
	raw, ok := attrs[key]
	if !ok || raw == nil {
		return ""
	}
	if value, ok := raw.(string); ok {
		return value
	}
	return strings.TrimSpace(toString(raw))
}

func catalogNodeTimeAttribute(attrs map[string]interface{}, key string) *time.Time {
	if attrs == nil {
		return nil
	}
	if storageRaw, ok := attrs["storage"].(map[string]interface{}); ok {
		if value := catalogNodeTimeAttribute(storageRaw, key); value != nil {
			return value
		}
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

func storageResourceInt64Stat(stats map[string]interface{}, key string) int64 {
	if stats == nil {
		return 0
	}
	switch value := stats[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case uint:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func joinCatalogPathParts(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, "/")
}

func toString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(value)
	}
}
