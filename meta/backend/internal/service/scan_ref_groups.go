package service

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

func (s *FilesystemCatalogScanService) ScanRefGroups(
	resource *commonModels.Engine,
	tenantID uint,
	groups []models.ScanRefGroup,
	scanDepth string,
	force bool,
	reporter ScanProgressReporter,
) (int, int, scantask.ExtractionCounts, error) {
	metaenrich.RegisterItemResolvers()
	_ = force
	scanDepth = scanDepthOrDefault(scanDepth, "deep")

	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, 0, scantask.ExtractionCounts{}, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	contentReader, ok := enginePlugin.(plugin.ContentReadableProvider)
	if !ok {
		return 0, 0, scantask.ExtractionCounts{}, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}
	itemTerm := catalogLeafTermForPlugin(enginePlugin, plugin.CatalogTermFile)
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	totalNodes := 0
	totalItems := 0
	extractionStats := scantask.ExtractionCounts{}
	seenNodes := map[uint]bool{}

	for i, group := range groups {
		primary := scanRefGroupPrimaryPath(group)
		if primary == "" {
			continue
		}
		dirPath := metapath.SanitizeFSPath(path.Dir(primary))
		if dirPath == "." {
			dirPath = ""
		}
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描内容引用组 %s", primary))
		}

		_, parentNode, err := s.ensureFilesystemScanRoot(tenantID, resource, enginePlugin, dirPath)
		if err != nil {
			return totalNodes, totalItems, extractionStats, err
		}
		if parentNode != nil && !seenNodes[parentNode.ID] {
			seenNodes[parentNode.ID] = true
			totalNodes++
		}

		files := fileRefsFromScanRefGroup(resource.ID, group)
		detection, err := metaitem.ResolveItems(context.Background(), metaitem.DirectoryResolveInput{
			ContentReader:  contentReader,
			ConnInfo:       connInfo,
			EngineID:       resource.ID,
			CatalogPathFor: plugin.FileItemPathForEngine(resource.ID),
			DirPath:        dirPath,
			Files:          files,
		})
		if err != nil {
			return totalNodes, totalItems, extractionStats, err
		}
		for _, detected := range detection.Items {
			persisted, _, itemExtractionStats := s.persistFileCatalogDetectedItem(context.Background(), resource, tenantID, parentNode, dirPath, detected, itemTerm, contentReader, connInfo, scanDepth)
			if persisted {
				totalItems++
			}
			extractionStats = mergeExtractionCounts(extractionStats, itemExtractionStats)
		}
		if reporter != nil {
			reporter.Advance(primary, i+1, len(groups), map[string]interface{}{"items": totalItems})
		}
	}
	return totalNodes, totalItems, extractionStats, nil
}

func (s *ObjectStorageCatalogScanService) ScanRefGroups(
	resource *commonModels.Engine,
	tenantID uint,
	groups []models.ScanRefGroup,
	scanDepth string,
	force bool,
	reporter ScanProgressReporter,
) (ObjectCatalogScanResult, error) {
	metaenrich.RegisterItemResolvers()
	_ = force
	scanDepth = scanDepthOrDefault(scanDepth, "deep")

	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return ObjectCatalogScanResult{}, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	contentReader, ok := enginePlugin.(plugin.ContentReadableProvider)
	if !ok {
		return ObjectCatalogScanResult{}, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}
	itemTerm := catalogLeafTermForPlugin(enginePlugin, plugin.CatalogTermObject)
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	rootNode, err := ensureCatalogRootNode(s.repo, tenantID, resource, enginePlugin)
	if err != nil {
		return ObjectCatalogScanResult{}, err
	}

	result := ObjectCatalogScanResult{}
	stats := map[uint]*objectCatalogNodeAggregate{}
	seenBuckets := map[string]*models.MetaNode{}
	scannedFingerprints := map[string]bool{}

	for i, group := range groups {
		primary := scanRefGroupPrimaryPath(group)
		if primary == "" {
			continue
		}
		bucket, objectPath, err := splitObjectRefPath(primary)
		if err != nil {
			return result, err
		}
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描对象引用组 %s", primary))
		}

		bucketNode := seenBuckets[bucket]
		if bucketNode == nil {
			attrs := metacatalog.ObjectBucketNodeAttributes(bucket)
			bucketNode, err = s.repo.UpsertNode(tenantID, resource.ID, rootNode, "bucket", bucket, &bucket, attrs)
			if err != nil {
				return result, err
			}
			seenBuckets[bucket] = bucketNode
			result.CatalogNodes++
		}

		resources, err := objectResourcesFromScanRefGroup(resource.ID, bucket, group)
		if err != nil {
			return result, err
		}
		s.detectObjectCatalogResourceFormats(context.Background(), contentReader, connInfo, resources)

		prefix := strings.Trim(metacatalog.ParentObjectPath(objectPath), "/")
		files := metacatalog.ObjectResourcesByParentPrefix(resources)[bucket+"\x00"+prefix]
		if len(files) == 0 {
			files = resources
		}
		detection, err := metaitem.ResolveItems(context.Background(), metaitem.DirectoryResolveInput{
			ContentReader:  contentReader,
			ConnInfo:       connInfo,
			EngineID:       resource.ID,
			CatalogPathFor: plugin.ObjectItemPathForBucket(resource.ID, bucket),
			DirPath:        prefix,
			Files:          objectStorageFileRefs(files),
		})
		if err != nil {
			return result, err
		}
		composites := make([]metacatalog.ObjectCatalogCompositeItem, 0, len(detection.Items))
		for _, detected := range detection.Items {
			if detected == nil {
				continue
			}
			composites = append(composites, metacatalog.ObjectCatalogCompositeItem{
				Bucket: bucket,
				Prefix: prefix,
				Item:   detected,
				Claims: detection.Claims,
			})
		}
		count, extractionStats, err := s.persistObjectCatalogCompositeItems(resource, tenantID, resource.ID, bucketNode, bucketNode, composites, stats, false, prefix, scannedFingerprints, itemTerm, contentReader, connInfo, scanDepth)
		if err != nil {
			return result, err
		}
		result.Items += count
		result.Extraction = mergeExtractionCounts(result.Extraction, extractionStats)
		if reporter != nil {
			reporter.Advance(primary, i+1, len(groups), map[string]interface{}{"items": result.Items})
		}
	}
	return result, nil
}

func fileRefsFromScanRefGroup(engineID uint, group models.ScanRefGroup) []metaitem.StorageFileRef {
	refs := normalizedScanRefs(group)
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

func objectResourcesFromScanRefGroup(engineID uint, bucket string, group models.ScanRefGroup) ([]metacatalog.StorageResource, error) {
	refs := normalizedScanRefs(group)
	resources := make([]metacatalog.StorageResource, 0, len(refs))
	for _, ref := range refs {
		refBucket, objectPath, err := splitObjectRefPath(ref.Path)
		if err != nil {
			return nil, err
		}
		if refBucket != bucket {
			return nil, fmt.Errorf("ref group crosses object buckets: %s != %s", refBucket, bucket)
		}
		resources = append(resources, metacatalog.StorageResource{
			RootName:    bucket,
			Path:        objectPath,
			FullPath:    strings.Trim(bucket+"/"+objectPath, "/"),
			NodeType:    plugin.CatalogKindObject,
			ObjectCount: 1,
			CatalogPath: plugin.ObjectItemPath(engineID, bucket, objectPath),
		})
	}
	return resources, nil
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

func normalizedScanRefs(group models.ScanRefGroup) []models.ScanRef {
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

func scanRefGroupPrimaryPath(group models.ScanRefGroup) string {
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

func splitObjectRefPath(refPath string) (bucket, objectPath string, err error) {
	trimmed := strings.Trim(refPath, "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("object ref path must be bucket/object: %s", refPath)
	}
	return strings.TrimSpace(parts[0]), strings.Trim(parts[1], "/"), nil
}
