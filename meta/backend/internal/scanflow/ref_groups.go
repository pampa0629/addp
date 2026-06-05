package scanflow

import (
	"fmt"
	"path"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
)

func FileRefGroupCandidateSet(engineID uint, primary string, group models.ScanRefGroup) ContentCandidateSet {
	dirPath := metapath.SanitizeFSPath(path.Dir(primary))
	if dirPath == "." {
		dirPath = ""
	}
	return ContentCandidateSet{
		DirPath:        dirPath,
		Files:          FileRefsFromScanRefGroup(engineID, group),
		CatalogPathFor: plugin.FileItemPathForEngine(engineID),
	}
}

func ObjectRefGroupCandidateSet(engineID uint, bucket, objectPath string, resources []metacatalog.StorageResource) ContentCandidateSet {
	prefix := objectRefGroupPrefix(bucket, objectPath)
	files := metacatalog.ObjectResourcesByParentPrefix(resources)[bucket+"\x00"+prefix]
	if len(files) == 0 {
		files = resources
	}
	return ContentCandidateSet{
		DirPath:        prefix,
		Files:          objectStorageFileRefs(files),
		CatalogPathFor: objectRefCatalogPathForEngine(engineID),
	}
}

func FileRefsFromScanRefGroup(engineID uint, group models.ScanRefGroup) []metaitem.StorageFileRef {
	refs := NormalizedScanRefs(group)
	files := make([]metaitem.StorageFileRef, 0, len(refs))
	for _, ref := range refs {
		filePath := metapath.SanitizeFSPath(ref.Path)
		files = append(files, metaitem.StorageFileRef{
			Name:        path.Base(filePath),
			Path:        filePath,
			CatalogPath: plugin.FileItemPath(engineID, filePath),
		})
	}
	return files
}

func ObjectResourcesFromScanRefGroup(engineID uint, bucket string, group models.ScanRefGroup) ([]metacatalog.StorageResource, error) {
	refs := NormalizedScanRefs(group)
	resources := make([]metacatalog.StorageResource, 0, len(refs))
	for _, ref := range refs {
		refBucket, objectPath, err := SplitObjectRefPath(ref.Path)
		if err != nil {
			return nil, err
		}
		if refBucket != bucket {
			return nil, fmt.Errorf("ref group crosses object buckets: %s != %s", refBucket, bucket)
		}
		fullPath := strings.Trim(refBucket+"/"+objectPath, "/")
		resources = append(resources, metacatalog.StorageResource{
			RootName:    bucket,
			Path:        fullPath,
			FullPath:    fullPath,
			NodeType:    plugin.CatalogKindObject,
			ObjectCount: 1,
			CatalogPath: plugin.ObjectItemPath(engineID, bucket, objectPath),
		})
	}
	return resources, nil
}

func NormalizedScanRefs(group models.ScanRefGroup) []models.ScanRef {
	seen := map[string]bool{}
	refs := make([]models.ScanRef, 0, len(group.Refs)+1)
	add := func(ref models.ScanRef) {
		ref.Path = strings.TrimSpace(ref.Path)
		if ref.Path == "" || seen[ref.Path] {
			return
		}
		seen[ref.Path] = true
		refs = append(refs, ref)
	}
	if group.Primary != "" {
		add(models.ScanRef{Path: group.Primary, Role: "main", Required: true})
	}
	for _, ref := range group.Refs {
		add(ref)
	}
	return refs
}

func ScanRefGroupPrimaryPath(group models.ScanRefGroup) string {
	if primary := strings.TrimSpace(group.Primary); primary != "" {
		return primary
	}
	for _, ref := range group.Refs {
		if path := strings.TrimSpace(ref.Path); path != "" {
			return path
		}
	}
	return ""
}

func SplitObjectRefPath(refPath string) (bucket, objectPath string, err error) {
	trimmed := strings.Trim(refPath, "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("object ref path must be bucket/object: %s", refPath)
	}
	return strings.TrimSpace(parts[0]), strings.Trim(parts[1], "/"), nil
}

func objectRefGroupPrefix(bucket, objectPath string) string {
	primaryPath := strings.Trim(bucket+"/"+objectPath, "/")
	return strings.Trim(metacatalog.ParentObjectPath(primaryPath), "/")
}

func objectRefCatalogPathForEngine(engineID uint) func(string) plugin.CatalogPath {
	return func(refPath string) plugin.CatalogPath {
		bucket, objectPath, err := SplitObjectRefPath(refPath)
		if err != nil {
			return plugin.ObjectItemPath(engineID, "", strings.Trim(refPath, "/"))
		}
		return plugin.ObjectItemPath(engineID, bucket, objectPath)
	}
}

func objectStorageFileRefs(resources []metacatalog.StorageResource) []metaitem.StorageFileRef {
	files := make([]metaitem.StorageFileRef, 0, len(resources))
	for _, resource := range resources {
		files = append(files, metaitem.StorageFileRef{
			Name:        path.Base(resource.Path),
			Path:        strings.Trim(resource.Path, "/"),
			CatalogPath: resource.CatalogPath,
			Size:        resource.SizeBytes,
			ContentType: resource.ContentType,
		})
	}
	return files
}
