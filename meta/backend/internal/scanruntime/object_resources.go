package scanruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanchange"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
	"github.com/addp/meta/internal/scanresource"
)

// persistObjectResources 持久化对象 catalog leaf 到数据库。
func (s *ObjectStorageCatalogRuntime) persistObjectResources(
	ctx context.Context,
	resource *commonModels.Engine,
	tenantID, engineID uint,
	bucketNode *models.MetaNode,
	resources []scanresource.StorageResource,
	scannedNodes map[uint]*models.MetaNode,
	includeBucketScan bool,
	scanDepth string,
	force bool,
	scanPathPrefix string,
	scannedFingerprints map[string]bool,
	itemTerm string,
) (int, scanflow.ExtractionCounts, error) {
	objects := 0
	extractionStats := scanflow.ExtractionCounts{}
	failures := &scanflow.FailedTargetCollector{}
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, extractionStats, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	readableProvider, _ := enginePlugin.(plugin.ContentReadableProvider)
	if readableProvider != nil {
		s.detectObjectCatalogResourceFormats(ctx, readableProvider, connInfo, resources)
	}

	if scanPathPrefix != "" {
		chain, err := s.repo.EnsureObjectCatalogPrefixChain(tenantID, engineID, bucketNode, scanPathPrefix)
		if err != nil {
			return objects, extractionStats, err
		}
		recordObjectCatalogScanNodes(scannedNodes, chain, scanPathPrefix, includeBucketScan)
	}
	var compositeWarnings []scanflow.ObjectCatalogCompositeDetectionError
	compositeSkipPaths, compositeItems, compositeWarnings := scanflow.DetectObjectCatalogCompositeItems(ctx, readableProvider, connInfo, engineID, resources, strings.EqualFold(scanDepth, "deep"))
	for _, warning := range compositeWarnings {
		s.log.Warn("对象 catalog 组合项检测失败", "bucket", warning.Bucket, "prefix", warning.Prefix, "error", warning.Err)
		failures.Add(strings.Trim(strings.Join([]string{warning.Bucket, warning.Prefix}, "/"), "/"), warning.Err)
	}
	compositeCount, compositeExtractionStats, err := s.persistObjectCatalogCompositeItems(ctx, resource, tenantID, engineID, bucketNode, compositeItems, scannedNodes, includeBucketScan, scanPathPrefix, scannedFingerprints, itemTerm, readableProvider, connInfo, scanDepth)
	objects += compositeCount
	extractionStats = scanflow.MergeExtractionCounts(extractionStats, compositeExtractionStats)
	if err != nil {
		failures.Add(scanPathPrefix, err)
	}

	for _, catalogResource := range resources {
		if err := ctx.Err(); err != nil {
			return objects, extractionStats, err
		}
		if catalogResource.NodeType == "bucket" {
			if includeBucketScan {
				scannedNodes[bucketNode.ID] = bucketNode
			}
			continue
		}
		if catalogResource.NodeType == "object" && compositeSkipPaths[catalogResource.Path] {
			continue
		}

		trimmed := metapath.SanitizeObjectPath(catalogResource.Path)
		var itemPlan scanresource.ObjectSingleItemPlan
		parentPath := trimmed
		if catalogResource.NodeType == "object" {
			itemPlan = scanresource.PlanObjectSingleItem(engineID, catalogResource, trimmed, itemTerm)
			parentPath = itemPlan.ParentPath
		}
		parentChain, err := s.repo.EnsureObjectCatalogPrefixChain(tenantID, engineID, bucketNode, parentPath)
		if err != nil {
			failures.Add(catalogResource.Path, err)
			continue
		}
		currentParent := parentChain[len(parentChain)-1]
		recordObjectCatalogScanNodes(scannedNodes, parentChain, scanPathPrefix, includeBucketScan)

		if catalogResource.NodeType != "object" {
			continue
		}
		if scannedFingerprints != nil {
			scannedFingerprints[itemPlan.Fingerprint] = true
		}

		existingItem, itemExists, err := s.repo.FindItemByFingerprintUnscoped(itemPlan.Fingerprint)
		if err != nil {
			failures.Add(catalogResource.Path, err)
			continue
		}
		needsUpdate := force || scanchange.ShouldUpdateStorageResource(existingItem, catalogResource) || !itemExists
		if existingItem != nil && existingItem.NodeID != currentParent.ID {
			needsUpdate = true
		}
		if strings.EqualFold(scanDepth, "deep") && existingItem != nil && existingItem.ScannedDepth != models.ScannedDepthDeep {
			needsUpdate = true
		}

		if itemExists && !needsUpdate {
			s.log.Debug("对象未变化，跳过更新",
				"bucket", catalogResource.RootName,
				"path", catalogResource.Path,
			)
			objects++
			continue
		}

		var enhancedAttrs models.JSONMap
		if strings.EqualFold(scanDepth, "deep") {
			enhancedAttrs = itemPlan.Attributes
		} else if itemExists {
			enhancedAttrs = existingItem.Attributes
			for k, v := range itemPlan.Attributes {
				enhancedAttrs[k] = v
			}
		} else {
			enhancedAttrs = itemPlan.Attributes
		}

		s.log.Info("计算fullName和父节点",
			"resource.RootName", catalogResource.RootName,
			"resource.Path", catalogResource.Path,
			"trimmed", trimmed,
			"scanPathPrefix", scanPathPrefix,
			"calculated_fullName", itemPlan.FullName,
			"currentParent_id", currentParent.ID,
			"currentParent_name", currentParent.Name,
			"objectName", itemPlan.ObjectName)

		result, err := scanprocessor.New(s.repo, s.indexer, s.log).WithContainerInspector(s.containerInspector).Process(ctx, scanprocessor.ObjectSingleInput(
			resource,
			tenantID,
			engineID,
			currentParent,
			itemPlan,
			catalogResource,
			enhancedAttrs,
			trimmed,
			readableProvider,
			connInfo,
			scanDepth,
		))
		if err != nil {
			extractionStats = scanflow.MergeExtractionCounts(extractionStats, result.Extraction)
			failures.Add(catalogResource.Path, err)
			continue
		}
		extractionStats = scanflow.MergeExtractionCounts(extractionStats, result.Extraction)

		objects++
	}

	if includeBucketScan {
		scannedNodes[bucketNode.ID] = bucketNode
	}
	return objects, extractionStats, failures.Err()
}
