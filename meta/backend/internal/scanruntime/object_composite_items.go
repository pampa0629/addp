package scanruntime

import (
	"context"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
	"github.com/addp/meta/internal/scanresource"
)

func (s *ObjectStorageCatalogRuntime) persistObjectCatalogCompositeItems(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID, engineID uint,
	bucketNode, basePrefixNode *models.MetaNode,
	items []scanresource.ObjectCompositeItem,
	stats map[uint]*scanflow.ObjectCatalogNodeAggregate,
	includeBucketAggregate bool,
	scanPathPrefix string,
	scannedFingerprints map[string]bool,
	itemTerm string,
	readableProvider plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	scanDepth string,
) (int, scanflow.ExtractionCounts, error) {
	count := 0
	extractionStats := scanflow.ExtractionCounts{}
	failures := &scanflow.FailedTargetCollector{}
	for _, composite := range items {
		if err := ctx.Err(); err != nil {
			return count, extractionStats, err
		}
		if composite.Item == nil {
			continue
		}
		itemPlan, ok := scanresource.PlanObjectCompositeItem(engineID, composite, itemTerm)
		if !ok {
			continue
		}

		if scannedFingerprints != nil {
			scannedFingerprints[itemPlan.Fingerprint] = true
		}
		parentNode, err := s.ensureObjectCatalogPrefixNodes(tenantID, engineID, bucketNode, basePrefixNode, itemPlan.ParentPath, scanPathPrefix, stats)
		if err != nil {
			failures.Add(itemPlan.FullName, err)
			continue
		}

		result, err := scanprocessor.New(s.repo, s.indexer, s.log).WithContainerInspector(s.containerInspector).Process(ctx, scanprocessor.ObjectCompositeInput(
			resource,
			tenantID,
			engineID,
			parentNode,
			itemPlan,
			composite,
			readableProvider,
			connInfo,
			scanDepth,
		))
		if err != nil {
			extractionStats = scanflow.MergeExtractionCounts(extractionStats, result.Extraction)
			failures.Add(itemPlan.FullName, err)
			continue
		}
		extractionStats = scanflow.MergeExtractionCounts(extractionStats, result.Extraction)
		count++
		updatedNodes := map[uint]bool{}
		for _, node := range []*models.MetaNode{bucketNode, parentNode} {
			if node == nil || updatedNodes[node.ID] {
				continue
			}
			if !includeBucketAggregate && node.ID == bucketNode.ID {
				continue
			}
			updatedNodes[node.ID] = true
			agg := scanflow.EnsureObjectCatalogNodeAggregate(stats, node)
			agg.ItemCount++
			agg.TotalSize += itemPlan.SizeBytes
		}
	}
	return count, extractionStats, failures.Err()
}
