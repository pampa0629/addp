package metacatalog

import (
	"path"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaitem"
)

// StorageResource is Meta's normalized view of a catalog leaf that can be
// consumed by data item detectors. Engine-specific hierarchy stays in
// CatalogPath/CatalogEntry; this type only carries scan-time item facts.
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

func StorageFileRefFromEntry(node plugin.CatalogEntry) (metaitem.StorageFileRef, bool) {
	if node.Role != plugin.CatalogRoleLeaf {
		return metaitem.StorageFileRef{}, false
	}
	return metaitem.StorageFileRef{
		Name:        node.Name,
		Path:        catalogEntryStoragePath(node),
		CatalogPath: node.Path,
		Size:        catalogEntrySizeBytes(node),
		ModifiedAt:  catalogEntryUpdatedAt(node),
		ContentType: catalogEntryContentType(node),
	}, true
}

func StorageDirectoryRefFromEntry(node plugin.CatalogEntry) (metaitem.StorageDirectoryRef, bool) {
	if node.Role != plugin.CatalogRoleBranch {
		return metaitem.StorageDirectoryRef{}, false
	}
	return metaitem.StorageDirectoryRef{
		Name:        node.Name,
		Path:        catalogEntryStoragePath(node),
		CatalogPath: node.Path,
	}, true
}

func ObjectStorageResourceFromNode(bucket string, node plugin.CatalogEntry) StorageResource {
	itemPath := objectKeyFromCatalogEntry(node, bucket)
	contentType := catalogEntryContentType(node)
	lastModified := node.UpdatedAt
	etag := catalogEntryETag(node)
	formatName := ""
	if detected := format.DetectFormat(itemPath, nil); detected != format.FormatUnknown {
		formatName = string(detected)
	} else if detected := format.MIMEToFormat(contentType); detected != format.FormatUnknown {
		formatName = string(detected)
	}
	return StorageResource{
		RootName:     bucket,
		Path:         itemPath,
		FullPath:     joinCatalogPathParts(bucket, itemPath),
		NodeType:     node.Kind,
		Format:       formatName,
		SizeBytes:    catalogEntrySizeBytes(node),
		ObjectCount:  1,
		LastModified: lastModified,
		ContentType:  contentType,
		ETag:         etag,
		CatalogPath:  node.Path,
	}
}

func (r StorageResource) StorageFileRef() metaitem.StorageFileRef {
	modifiedAt := time.Time{}
	if r.LastModified != nil {
		modifiedAt = *r.LastModified
	}
	entryPath := r.FullPath
	if entryPath == "" {
		entryPath = r.Path
	}
	return metaitem.StorageFileRef{
		Name:        path.Base(r.Path),
		Path:        entryPath,
		CatalogPath: r.CatalogPath,
		Size:        r.SizeBytes,
		ModifiedAt:  modifiedAt,
		ContentType: r.ContentType,
	}
}

func objectKeyFromCatalogEntry(node plugin.CatalogEntry, bucket string) string {
	rawPath := ""
	if node.Storage != nil {
		rawPath = node.Storage.Path
	}
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

func catalogEntryStoragePath(node plugin.CatalogEntry) string {
	if node.Storage != nil && node.Storage.Path != "" {
		return node.Storage.Path
	}
	return node.Path.StringPath()
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

func catalogEntryContentType(node plugin.CatalogEntry) string {
	if node.Storage == nil {
		return ""
	}
	return node.Storage.ContentType
}

func catalogEntryETag(node plugin.CatalogEntry) string {
	if node.Storage == nil {
		return ""
	}
	return node.Storage.ETag
}

func catalogEntrySizeBytes(node plugin.CatalogEntry) int64 {
	if node.Storage == nil || node.Storage.SizeBytes == nil {
		return 0
	}
	return *node.Storage.SizeBytes
}

func catalogEntryUpdatedAt(node plugin.CatalogEntry) time.Time {
	if node.UpdatedAt == nil {
		return time.Time{}
	}
	return *node.UpdatedAt
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
