package scanruntime

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
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
	resources []scanresource.StorageResource,
	scanState *objectCatalogScanState,
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
			scanState.fail(itemPlan.FullName, err)
			continue
		}
		scanState.record(parentChain, scanPathPrefix, includeBucketScan)

		parentNode := parentChain[len(parentChain)-1]
		input := scanprocessor.ObjectCompositeInput(
			resource,
			tenantID,
			engineID,
			parentNode,
			itemPlan,
			composite,
			readableProvider,
			connInfo,
			scanDepth,
		)
		if composite.Item.Layout == format.LayoutSingle {
			found := false
			for _, source := range resources {
				if source.FullPath != itemPlan.FullName {
					continue
				}
				plan := scanresource.PlanObjectSingleItem(engineID, source, source.Path, itemTerm)
				plan.DataItem = itemPlan.DataItem
				metaattr.MergeDataItemAttributes(plan.Attributes, metaitem.AttributeInput(plan.DataItem))
				input = scanprocessor.ObjectSingleInput(resource, tenantID, engineID, parentNode, plan, source, plan.Attributes, source.Path, readableProvider, connInfo, scanDepth)
				found = true
				break
			}
			if !found {
				err := fmt.Errorf("single object source facts missing for %s", itemPlan.FullName)
				failures.Add(itemPlan.FullName, err)
				scanState.fail(itemPlan.FullName, err)
				continue
			}
		}
		result, err := scanprocessor.New(s.repo, s.indexer, s.log).WithContainerInspector(s.containerInspector).Process(ctx, input)
		if err != nil {
			extractionStats = scanflow.MergeExtractionCounts(extractionStats, result.Extraction)
			failures.Add(itemPlan.FullName, err)
			scanState.fail(itemPlan.FullName, err)
			continue
		}
		extractionStats = scanflow.MergeExtractionCounts(extractionStats, result.Extraction)
		count++
	}
	return count, extractionStats, failures.Err()
}
