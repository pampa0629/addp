package scanruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
)

func scanObjectPaths(
	ctx context.Context,
	runtime *ObjectStorageCatalogRuntime,
	repo *metaRepo.ScanRepository,
	resource *commonModels.Engine,
	tenantID uint,
	catalogPaths []string,
	fallback []string,
	scanDepth string,
	force bool,
	reporter scanflow.ProgressReporter,
) (scanflow.DispatchResult, error) {
	scanDepth = scanflow.ScanDepthOrDefault(scanDepth, "deep")
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return scanflow.DispatchResult{}, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	catalogProvider, ok := enginePlugin.(plugin.CatalogProvider)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}
	itemTerm := scanflow.CatalogLeafTermForPlugin(enginePlugin, plugin.CatalogTermObject)

	paths, err := scanflow.ResolveCatalogScanPaths(
		ctx,
		"未检测到可扫描的对象路径",
		catalogPaths,
		fallback,
		func(ctx context.Context) ([]string, error) {
			buckets, err := listObjectCatalogBucketNodes(ctx, resource, catalogProvider)
			if err != nil {
				return nil, fmt.Errorf("failed to list buckets: %w", err)
			}
			names := make([]string, 0, len(buckets))
			for _, b := range buckets {
				names = append(names, b.Name)
			}
			return names, nil
		},
		reporter,
	)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}

	return scanObjectCatalogPaths(ctx, runtime, repo, resource, tenantID, resource.ID, catalogProvider, paths, scanDepth, force, reporter, itemTerm)
}

func scanObjectCatalogPaths(
	ctx context.Context,
	runtime *ObjectStorageCatalogRuntime,
	repo *metaRepo.ScanRepository,
	resource *commonModels.Engine,
	tenantID, engineID uint,
	catalogProvider plugin.CatalogProvider,
	paths []string,
	scanDepth string,
	force bool,
	reporter scanflow.ProgressReporter,
	itemTerm string,
) (scanflow.DispatchResult, error) {
	bucketNodes := make(map[string]*models.MetaNode)
	processedBuckets := make(map[string]bool)
	processedPrefixes := make(map[string]map[string]bool)
	nodeStats := make(map[uint]*scanflow.ObjectCatalogNodeAggregate)
	scannedFingerprints := make(map[string]bool)

	result := scanflow.DispatchResult{}
	total := len(paths)
	completed := 0
	isDeepScan := strings.EqualFold(scanDepth, "deep")

	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	rootNode, err := metaRepo.EnsureCatalogRootNode(repo, tenantID, resource, enginePlugin)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}

	for _, rawPath := range paths {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描对象路径 %s", rawPath))
		}
		target, err := resolveObjectCatalogTarget(ctx, resource, catalogProvider, rawPath)
		if err != nil {
			completed++
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 解析失败: %v", rawPath, err))
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}
		bucketName := target.Bucket
		prefix := target.Prefix
		if bucketName == "" {
			completed++
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 缺少 bucket 信息，已跳过", rawPath))
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}

		var objects []plugin.CatalogEntry
		if target.Object != "" {
			objects, err = readObjectCatalogLeaf(ctx, resource, catalogProvider, bucketName, target.Object)
		} else {
			objects, err = listObjectCatalogLeaves(ctx, resource, catalogProvider, bucketName, prefix, isDeepScan)
		}
		if err != nil {
			completed++
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 扫描失败: %v", rawPath, err))
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}

		resources := objectCatalogEntriesToStorageResources(objects, bucketName)

		bucketNode, ok := bucketNodes[bucketName]
		if !ok {
			attrs := metacatalog.ObjectBucketNodeAttributes(bucketName)
			bucketNode, err = repo.UpsertNode(tenantID, engineID, rootNode, "bucket", bucketName, &bucketName, attrs)
			if err != nil {
				return result, err
			}
			bucketNodes[bucketName] = bucketNode
			result.CatalogNodes++
		}

		fullBucket := prefix == "" && target.Object == ""
		if fullBucket {
			if !processedBuckets[bucketName] {
				if err := repo.ResetNodeState(bucketNode, "running"); err != nil {
					return result, err
				}
			}
			processedBuckets[bucketName] = true
		} else if target.Object == "" {
			if processedPrefixes[bucketName] == nil {
				processedPrefixes[bucketName] = map[string]bool{}
			}
			processedPrefixes[bucketName][strings.Trim(prefix, "/")] = true
		}

		if len(resources) == 0 {
			if fullBucket {
				scanflow.EnsureObjectCatalogNodeAggregate(nodeStats, bucketNode)
			}
			completed++
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 未发现新对象", rawPath))
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}

		scanPathPrefix := prefix
		if target.Object != "" {
			scanPathPrefix = metacatalog.ParentObjectPath(target.Object)
		}
		objectCount, pathExtractionStats, err := runtime.persistObjectResources(resource, tenantID, engineID, bucketNode, resources, nodeStats, fullBucket, scanDepth, force, scanPathPrefix, scannedFingerprints, itemTerm)
		if err != nil {
			completed++
			if reporter != nil {
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}
		result.Items += objectCount
		result.Extraction = scanflow.MergeExtractionCounts(result.Extraction, pathExtractionStats)
		completed++
		if reporter != nil {
			reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": objectCount})
		}
	}

	if isDeepScan && len(scannedFingerprints) > 0 {
		for bucketName := range processedBuckets {
			if bucketNodes[bucketName] == nil {
				continue
			}
			if _, err := repo.SoftDeleteObjectMetaItemsMissingFingerprints(tenantID, engineID, bucketName, scannedFingerprints); err != nil {
				continue
			}
		}
		for bucketName, prefixes := range processedPrefixes {
			if processedBuckets[bucketName] || bucketNodes[bucketName] == nil {
				continue
			}
			for prefix := range prefixes {
				if prefix == "" {
					continue
				}
				if _, err := repo.SoftDeleteObjectMetaItemsMissingFingerprintsInPrefix(tenantID, engineID, bucketName, prefix, scannedFingerprints); err != nil {
					continue
				}
			}
		}
	}

	for bucketName, bucketNode := range bucketNodes {
		if !processedBuckets[bucketName] {
			continue
		}
		agg, ok := nodeStats[bucketNode.ID]
		if !ok {
			continue
		}
		_ = repo.FinalizeNodeStateWithDepth(bucketNode, "completed", agg.ItemCount, agg.TotalSize, "", scanDepth)
	}

	for _, agg := range nodeStats {
		if agg.Node.NodeType == "bucket" {
			continue
		}
		_ = repo.FinalizeObjectCatalogPrefixNodeWithDepth(agg.Node, agg.ItemCount, agg.TotalSize, scanDepth)
	}

	return result, nil
}
