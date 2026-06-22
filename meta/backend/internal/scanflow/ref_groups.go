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
		ResolveOptions: metaitem.ResolveOptions{IncludeSingleResources: true},
		CatalogPathFor: plugin.FileItemPathForEngine(engineID),
	}
}

func ObjectRefGroupCandidateSet(engineID uint, bucket, objectPath string, resources []metacatalog.StorageResource) ContentCandidateSet {
	prefix := objectRefGroupPrefix(objectPath)
	files := metacatalog.ObjectResourcesByParentPrefix(resources)[bucket+"\x00"+prefix]
	if len(files) == 0 {
		files = resources
	}
	return ContentCandidateSet{
		DirPath:        prefix,
		Files:          objectStorageFileRefs(files),
		ResolveOptions: metaitem.ResolveOptions{IncludeSingleResources: true},
		CatalogPathFor: objectRefCatalogPathForBucket(engineID, bucket),
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
		refBucket, objectPath, err := plugin.SplitObjectRefPath(ref.Path)
		if err != nil {
			return nil, err
		}
		if refBucket != bucket {
			return nil, fmt.Errorf("ref group crosses object buckets: %s != %s", refBucket, bucket)
		}
		objectPath = strings.Trim(objectPath, "/")
		fullPath := strings.Trim(refBucket+"/"+objectPath, "/")
		resources = append(resources, metacatalog.StorageResource{
			RootName:    bucket,
			Path:        objectPath,
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
		add(models.ScanRef{Path: group.Primary, Role: "main", Required: true, Primary: true})
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

func objectRefGroupPrefix(objectPath string) string {
	return strings.Trim(metacatalog.ParentObjectPath(objectPath), "/")
}

func objectRefCatalogPathForBucket(engineID uint, bucket string) func(string) plugin.CatalogPath {
	return plugin.ObjectItemPathForBucketRef(engineID, bucket)
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
