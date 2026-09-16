package scanruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
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

	catalogProvider, ok := enginePlugin.(plugin.EngineCatalogProvider)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("engine %s does not implement EngineCatalogProvider", resource.EngineType)
	}
	itemTerm := scanflow.EngineCatalogLeafTermForPlugin(enginePlugin, plugin.EngineCatalogTermObject)

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
	catalogProvider plugin.EngineCatalogProvider,
	paths []string,
	scanDepth string,
	force bool,
	reporter scanflow.ProgressReporter,
	itemTerm string,
) (result scanflow.DispatchResult, scanErr error) {
	bucketNodes := make(map[string]*models.MetaNode)
	processedBuckets := make(map[string]bool)
	processedPrefixes := make(map[string]map[string]bool)
	scanState := newObjectCatalogScanState()
	scannedFingerprints := make(map[string]bool)

	failures := &scanflow.FailedTargetCollector{}
	total := len(paths)
	completed := 0
	isDeepScan := strings.EqualFold(scanDepth, "deep")

	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	rootNode, err := metaRepo.EnsureEngineCatalogRootNode(repo, tenantID, resource, enginePlugin)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}
	activePath := ""
	defer func() {
		var targetErrors *scanflow.FailedTargetsError
		if scanErr != nil && activePath != "" && !errors.As(scanErr, &targetErrors) {
			scanState.fail(activePath, scanErr)
		}
		for _, node := range scanState.nodes {
			status, message := "completed", ""
			if err := scanState.errorFor(node.FullName); err != nil {
				status, message = "failed", err.Error()
			}
			if err := repo.FinalizeNodeStateWithDepth(node, status, message, scanDepth); err != nil {
				scanErr = errors.Join(scanErr, err)
			}
		}
	}()

	for _, rawPath := range paths {
		activePath = rawPath
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描对象路径 %s", rawPath))
		}
		target, err := resolveObjectCatalogTarget(ctx, resource, catalogProvider, rawPath)
		if err != nil {
			failures.Add(rawPath, err)
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
			failures.Add(rawPath, fmt.Errorf("object path is missing bucket"))
			completed++
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 缺少 bucket 信息，已跳过", rawPath))
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}

		bucketNode, ok := bucketNodes[bucketName]
		if !ok {
			attrs := scanresource.ObjectBucketNodeAttributes(bucketName)
			bucketNode, err = repo.UpsertNode(tenantID, engineID, rootNode, "bucket", bucketName, &bucketName, attrs)
			if err != nil {
				return result, err
			}
			bucketNodes[bucketName] = bucketNode
			result.CatalogNodes++
		}

		fullBucket := prefix == "" && target.Object == ""
		if fullBucket {
			processedBuckets[bucketName] = true
		} else if target.Object == "" {
			if processedPrefixes[bucketName] == nil {
				processedPrefixes[bucketName] = map[string]bool{}
			}
			processedPrefixes[bucketName][strings.Trim(prefix, "/")] = true
		}
		if target.Object == "" {
			chain, err := repo.EnsureObjectCatalogPrefixChain(tenantID, engineID, bucketNode, prefix)
			if err != nil {
				return result, err
			}
			node := chain[len(chain)-1]
			scanState.record(chain, prefix, fullBucket)
			if err := repo.ResetNodeState(node, "running"); err != nil {
				return result, err
			}
		}

		var objects []plugin.EngineCatalogEntry
		if target.Object != "" {
			objects, err = readObjectCatalogLeaf(ctx, resource, catalogProvider, bucketName, target.Object)
		} else {
			objects, err = listObjectCatalogLeaves(ctx, resource, catalogProvider, bucketName, prefix, isDeepScan)
		}
		if err != nil {
			failures.Add(rawPath, err)
			scanState.fail(rawPath, err)
			completed++
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 扫描失败: %v", rawPath, err))
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}
		resources := objectCatalogEntriesToStorageResources(objects, bucketName)

		if len(resources) == 0 {
			completed++
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 未发现新对象", rawPath))
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}

		scanPathPrefix := prefix
		scopeState := scanState
		if target.Object != "" {
			scanPathPrefix = scanresource.ParentObjectPath(target.Object)
			// 单个对象并未枚举完整父目录，不能更新父目录扫描状态。
			scopeState = nil
		}
		objectCount, pathExtractionStats, err := runtime.persistObjectResources(ctx, resource, tenantID, engineID, bucketNode, resources, scopeState, fullBucket, scanDepth, force, scanPathPrefix, scannedFingerprints, itemTerm)
		result.Items += objectCount
		result.Extraction = scanflow.MergeExtractionCounts(result.Extraction, pathExtractionStats)
		if err != nil {
			failures.Add(rawPath, err)
			// 逐项失败已按完整路径记录；只有无法定位的范围错误才影响整个目标范围。
			var targetErrors *scanflow.FailedTargetsError
			if !errors.As(err, &targetErrors) {
				scanState.fail(rawPath, err)
			}
			completed++
			if reporter != nil {
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": objectCount})
			}
			continue
		}
		completed++
		if reporter != nil {
			reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": objectCount})
		}
	}

	if isDeepScan && len(scannedFingerprints) > 0 {
		for bucketName := range processedBuckets {
			if bucketNodes[bucketName] == nil || scanState.errorFor(bucketName) != nil {
				continue
			}
			if _, err := repo.SoftDeleteObjectMetaItemsMissingFingerprints(tenantID, engineID, bucketName, scannedFingerprints); err != nil {
				failures.Add(bucketName, err)
				scanState.fail(bucketName, err)
				continue
			}
		}
		for bucketName, prefixes := range processedPrefixes {
			if processedBuckets[bucketName] || bucketNodes[bucketName] == nil {
				continue
			}
			for prefix := range prefixes {
				if prefix == "" || scanState.errorFor(bucketName+"/"+prefix) != nil {
					continue
				}
				if _, err := repo.SoftDeleteObjectMetaItemsMissingFingerprintsInPrefix(tenantID, engineID, bucketName, prefix, scannedFingerprints); err != nil {
					failures.Add(bucketName+"/"+prefix, err)
					scanState.fail(bucketName+"/"+prefix, err)
					continue
				}
			}
		}
		if err := repo.HardDeleteInvalidEngineGraph(tenantID, engineID); err != nil {
			failures.Add(resource.Name, err)
			for _, node := range scanState.nodes {
				scanState.fail(node.FullName, err)
			}
			if reporter != nil {
				reporter.Message(fmt.Sprintf("清理对象 catalog 陈旧节点失败: %v", err))
			}
		}
	}

	return result, failures.Err()
}
