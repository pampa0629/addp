package scanruntime

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanresource"
)

func scanObjectRefGroups(
	ctx context.Context,
	runtime *ObjectStorageCatalogRuntime,
	repo *metaRepo.ScanRepository,
	resource *commonModels.Engine,
	tenantID uint,
	groups []models.ScanRefGroup,
	scanDepth string,
	reporter scanflow.ProgressReporter,
) (scanflow.DispatchResult, error) {
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return scanflow.DispatchResult{}, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	contentReader, ok := enginePlugin.(plugin.ContentReadableProvider)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}
	catalogProvider, ok := enginePlugin.(plugin.EngineCatalogProvider)
	if !ok {
		return scanflow.DispatchResult{}, fmt.Errorf("engine %s does not implement EngineCatalogProvider", resource.EngineType)
	}
	itemTerm := scanflow.EngineCatalogLeafTermForPlugin(enginePlugin, plugin.EngineCatalogTermObject)
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	rootNode, err := metaRepo.EnsureEngineCatalogRootNode(repo, tenantID, resource, enginePlugin)
	if err != nil {
		return scanflow.DispatchResult{}, err
	}

	result := scanflow.DispatchResult{}
	stats := map[uint]*scanflow.ObjectCatalogNodeAggregate{}
	seenBuckets := map[string]*models.MetaNode{}
	scannedFingerprints := map[string]bool{}

	for i, group := range groups {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		primary := scanflow.ScanRefGroupPrimaryPath(group)
		if primary == "" {
			continue
		}
		bucket, objectPath, err := plugin.SplitObjectRefPath(primary)
		if err != nil {
			return result, err
		}
		if err := ensureObjectRefGroupBucket(ctx, catalogProvider, connInfo, resource.ID, bucket); err != nil {
			return result, err
		}
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描对象引用组 %s", primary))
		}

		bucketNode := seenBuckets[bucket]
		if bucketNode == nil {
			attrs := scanresource.ObjectBucketNodeAttributes(bucket)
			bucketNode, err = repo.UpsertNode(tenantID, resource.ID, rootNode, "bucket", bucket, &bucket, attrs)
			if err != nil {
				return result, err
			}
			seenBuckets[bucket] = bucketNode
			result.CatalogNodes++
		}

		resources, err := scanflow.ObjectResourcesFromScanRefGroup(resource.ID, bucket, group)
		if err != nil {
			return result, err
		}
		runtime.detectObjectCatalogResourceFormats(ctx, contentReader, connInfo, resources)

		candidates := scanflow.ObjectRefGroupCandidateSet(resource.ID, bucket, objectPath, resources)
		detection, err := scanflow.ResolveContentCandidates(ctx, contentReader, connInfo, resource.ID, candidates)
		if err != nil {
			return result, err
		}
		composites := make([]scanresource.ObjectCompositeItem, 0, len(detection.Items))
		for _, detected := range detection.Items {
			if detected == nil {
				continue
			}
			composites = append(composites, scanresource.ObjectCompositeItem{
				Bucket: bucket,
				Prefix: candidates.DirPath,
				Item:   detected,
				Claims: detection.Claims,
			})
		}
		count, extractionStats, err := runtime.persistObjectCatalogCompositeItems(ctx, resource, tenantID, resource.ID, bucketNode, bucketNode, composites, stats, false, candidates.DirPath, scannedFingerprints, itemTerm, contentReader, connInfo, scanDepth)
		if err != nil {
			return result, err
		}
		result.Items += count
		result.Extraction = scanflow.MergeExtractionCounts(result.Extraction, extractionStats)
		if reporter != nil {
			reporter.Advance(primary, i+1, len(groups), map[string]interface{}{"items": result.Items})
		}
	}
	return result, nil
}

func ensureObjectRefGroupBucket(ctx context.Context, catalogProvider plugin.EngineCatalogProvider, connInfo plugin.ConnectionInfo, engineID uint, bucket string) error {
	if catalogProvider == nil {
		return fmt.Errorf("object ref group scan requires EngineCatalogProvider")
	}
	entry, err := catalogProvider.ResolvePath(ctx, connInfo, plugin.ObjectDirectoryPath(engineID, bucket, ""))
	if err != nil {
		return fmt.Errorf("resolve object ref group bucket %q: %w", bucket, err)
	}
	if entry == nil {
		return fmt.Errorf("object ref group bucket %q does not exist", bucket)
	}
	if entry.Kind != plugin.EngineCatalogKindBucket && entry.Term != plugin.EngineCatalogTermBucket {
		return fmt.Errorf("object ref group bucket %q resolved to %s/%s", bucket, entry.Term, entry.Kind)
	}
	return nil
}
