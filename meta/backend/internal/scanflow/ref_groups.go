package scanflow

import (
	"fmt"
	"path"
	"strings"

	"github.com/addp/common/contentio"
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
	directories := FileDirectoryRefsFromScanRefGroup(engineID, group)
	directoryPaths := make(map[string]bool, len(directories))
	for _, directory := range directories {
		directoryPaths[directory.Path] = true
	}
	return ContentCandidateSet{
		DirPath:        dirPath,
		Files:          FileRefsFromScanRefGroup(engineID, group),
		Subdirs:        directories,
		ResolveOptions: metaitem.ResolveOptions{IncludeSingleResources: true},
		CatalogPathFor: func(rawPath string) plugin.CatalogPath {
			normalized := metapath.SanitizeFSPath(rawPath)
			if directoryPaths[normalized] {
				return plugin.FileDirectoryPath(engineID, normalized)
			}
			return plugin.FileItemPath(engineID, normalized)
		},
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
		if strings.EqualFold(strings.TrimSpace(ref.Role), contentio.RoleScope) {
			continue
		}
		filePath := metapath.SanitizeFSPath(ref.Path)
		files = append(files, metaitem.StorageFileRef{
			Name:        path.Base(filePath),
			Path:        filePath,
			CatalogPath: plugin.FileItemPath(engineID, filePath),
		})
	}
	return files
}

func FileDirectoryRefsFromScanRefGroup(engineID uint, group models.ScanRefGroup) []metaitem.StorageDirectoryRef {
	refs := NormalizedScanRefs(group)
	directories := make([]metaitem.StorageDirectoryRef, 0, len(refs))
	for _, ref := range refs {
		if !strings.EqualFold(strings.TrimSpace(ref.Role), contentio.RoleScope) {
			continue
		}
		directoryPath := metapath.SanitizeFSPath(ref.Path)
		directories = append(directories, metaitem.StorageDirectoryRef{
			Name:        path.Base(directoryPath),
			Path:        directoryPath,
			CatalogPath: plugin.FileDirectoryPath(engineID, directoryPath),
		})
	}
	return directories
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
	for _, ref := range group.Refs {
		add(ref)
	}
	primary := strings.TrimSpace(group.Primary)
	if primary == "" {
		return refs
	}
	for i := range refs {
		if refs[i].Path != primary {
			continue
		}
		refs[i].Primary = true
		if i > 0 {
			primaryRef := refs[i]
			copy(refs[1:i+1], refs[0:i])
			refs[0] = primaryRef
		}
		return refs
	}
	refs = append([]models.ScanRef{{Path: primary, Role: contentio.RoleMain, Required: true, Primary: true}}, refs...)
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
