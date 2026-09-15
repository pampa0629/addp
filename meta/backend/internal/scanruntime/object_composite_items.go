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
	bucketNode *models.MetaNode,
	items []scanresource.ObjectCompositeItem,
	scannedNodes map[uint]*models.MetaNode,
	includeBucketScan bool,
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
		parentChain, err := s.repo.EnsureObjectCatalogPrefixChain(tenantID, engineID, bucketNode, itemPlan.ParentPath)
		if err != nil {
			failures.Add(itemPlan.FullName, err)
			continue
		}

		parentNode := parentChain[len(parentChain)-1]
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
		recordObjectCatalogScanNodes(scannedNodes, parentChain, scanPathPrefix, includeBucketScan)
	}
	return count, extractionStats, failures.Err()
}
