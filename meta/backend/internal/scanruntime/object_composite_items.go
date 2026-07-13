package scanruntime

import (
	"context"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
)

func (s *ObjectStorageCatalogRuntime) persistObjectCatalogCompositeItems(
	resource *commonModels.Engine,
	tenantID, engineID uint,
	bucketNode, basePrefixNode *models.MetaNode,
	items []metacatalog.ObjectCatalogCompositeItem,
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
	for _, composite := range items {
		if composite.Item == nil {
			continue
		}
		itemPlan, ok := metacatalog.PlanObjectCatalogCompositeItem(engineID, composite, itemTerm)
		if !ok {
			continue
		}

		if scannedFingerprints != nil {
			scannedFingerprints[itemPlan.Fingerprint] = true
		}
		parentNode, err := s.ensureObjectCatalogPrefixNodes(tenantID, engineID, bucketNode, basePrefixNode, itemPlan.ParentPath, scanPathPrefix, stats)
		if err != nil {
			return count, extractionStats, err
		}

		result, err := scanprocessor.New(s.repo, s.indexer, s.log).WithCADInspector(s.cadInspector).Process(context.Background(), scanprocessor.ObjectCompositeInput(
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
			return count, extractionStats, err
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
	return count, extractionStats, nil
}
