package scanruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanadapter"
	"github.com/addp/meta/internal/scanchange"
	"github.com/addp/meta/internal/scanflow"
	"github.com/addp/meta/internal/scanprocessor"
)

// PersistObjectResources 持久化对象 catalog leaf 到数据库。
func (s *ObjectStorageCatalogRuntime) PersistObjectResources(
	resource *commonModels.Engine,
	tenantID, engineID uint,
	bucketNode *models.MetaNode,
	resources []metacatalog.StorageResource,
	stats map[uint]*scanadapter.ObjectCatalogNodeAggregate,
	includeBucketAggregate bool,
	scanDepth string,
	force bool,
	scanPathPrefix string,
	scannedFingerprints map[string]bool,
	itemTerm string,
) (int, scanflow.ExtractionCounts, error) {
	objects := 0
	extractionStats := scanflow.ExtractionCounts{}
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, extractionStats, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	readableProvider, _ := enginePlugin.(plugin.ContentReadableProvider)
	if readableProvider != nil {
		s.DetectObjectCatalogResourceFormats(context.Background(), readableProvider, connInfo, resources)
	}

	basePrefixNode := bucketNode
	if scanPathPrefix != "" {
		s.log.Info("处理scanPathPrefix建立基础父节点",
			"scanPathPrefix", scanPathPrefix,
			"bucket", bucketNode.Name)
		node, err := s.repo.EnsureObjectCatalogPrefixPath(tenantID, engineID, bucketNode, scanPathPrefix)
		if err != nil {
			return objects, extractionStats, err
		}
		if node != nil && node != bucketNode {
			basePrefixNode = node
			scanadapter.EnsureObjectCatalogNodeAggregate(stats, node)
		}
		s.log.Info("scanPathPrefix处理完成",
			"basePrefixNode_id", basePrefixNode.ID,
			"basePrefixNode_name", basePrefixNode.Name)
	}
	var compositeWarnings []scanflow.ObjectCatalogCompositeDetectionError
	compositeSkipPaths, compositeItems, compositeWarnings := scanflow.DetectObjectCatalogCompositeItems(context.Background(), readableProvider, connInfo, engineID, resources, strings.EqualFold(scanDepth, "deep"))
	for _, warning := range compositeWarnings {
		s.log.Warn("对象 catalog 组合项检测失败", "bucket", warning.Bucket, "prefix", warning.Prefix, "error", warning.Err)
	}
	compositeCount, compositeExtractionStats, err := s.PersistObjectCatalogCompositeItems(resource, tenantID, engineID, bucketNode, basePrefixNode, compositeItems, stats, includeBucketAggregate, scanPathPrefix, scannedFingerprints, itemTerm, readableProvider, connInfo, scanDepth)
	if err != nil {
		return objects, extractionStats, err
	}
	objects += compositeCount
	extractionStats = scanflow.MergeExtractionCounts(extractionStats, compositeExtractionStats)

	for _, catalogResource := range resources {
		if catalogResource.NodeType == "bucket" {
			if includeBucketAggregate {
				scanadapter.EnsureObjectCatalogNodeAggregate(stats, bucketNode)
			}
			continue
		}

		parentChain := []*models.MetaNode{bucketNode}
		if basePrefixNode != bucketNode {
			parentChain = append(parentChain, basePrefixNode)
		}
		currentParent := basePrefixNode

		trimmed := metapath.SanitizeObjectPath(catalogResource.Path)

		s.log.Info("处理meta对象",
			"resource.NodeType", catalogResource.NodeType,
			"resource.Path", catalogResource.Path,
			"trimmed", trimmed,
			"scanPathPrefix", scanPathPrefix,
			"basePrefixNode", basePrefixNode.Name)

		pathPlan := metacatalog.PlanObjectCatalogRelativePath(trimmed, scanPathPrefix)
		if pathPlan.ExactBase && catalogResource.NodeType == "prefix" {
			scanadapter.EnsureObjectCatalogNodeAggregate(stats, basePrefixNode)
		}
		if pathPlan.SkipReason == "空路径" && includeBucketAggregate {
			scanadapter.EnsureObjectCatalogNodeAggregate(stats, bucketNode)
		}

		if pathPlan.SkipReason != "" {
			s.log.Info("跳过或特殊处理",
				"reason", pathPlan.SkipReason,
				"segmentsToProcess", pathPlan.Segments)
		} else if len(pathPlan.Segments) > 0 {
			s.log.Info("准备处理segments",
				"segmentsToProcess", pathPlan.Segments)
		}

		if len(pathPlan.Segments) > 0 {
			for idx, segment := range pathPlan.Segments {
				isLast := idx == len(pathPlan.Segments)-1
				if catalogResource.NodeType == "object" && isLast {
					break
				}
				fullName := metapath.ComposeNodeFullName(segment, currentParent, "/")
				attrs := metacatalog.ObjectPrefixNodeAttributes(catalogResource.RootName, strings.Join(pathPlan.Segments[:idx+1], "/")+"/")
				childNode, err := s.repo.UpsertNode(tenantID, engineID, currentParent, "prefix", segment, &fullName, attrs)
				if err != nil {
					return objects, extractionStats, err
				}
				currentParent = childNode
				parentChain = append(parentChain, childNode)
				scanadapter.EnsureObjectCatalogNodeAggregate(stats, childNode)
			}
		}

		if catalogResource.NodeType != "object" {
			continue
		}
		if compositeSkipPaths[catalogResource.Path] {
			continue
		}

		itemPlan := metacatalog.PlanObjectCatalogSingleItem(engineID, catalogResource, trimmed, itemTerm)
		if scannedFingerprints != nil {
			scannedFingerprints[itemPlan.Fingerprint] = true
		}

		existingItem, itemExists, err := s.repo.FindItemByFingerprintUnscoped(itemPlan.Fingerprint)
		if err != nil {
			return objects, extractionStats, err
		}
		needsUpdate := force || scanchange.ShouldUpdateStorageResource(existingItem, catalogResource) || !itemExists
		if strings.EqualFold(scanDepth, "deep") && existingItem != nil && existingItem.ScannedDepth != models.ScannedDepthDeep {
			needsUpdate = true
		}

		if itemExists && !needsUpdate {
			s.log.Debug("对象未变化，跳过更新",
				"bucket", catalogResource.RootName,
				"path", catalogResource.Path,
			)
			objects++
			for idx, node := range parentChain {
				if !includeBucketAggregate && idx == 0 {
					continue
				}
				agg := scanadapter.EnsureObjectCatalogNodeAggregate(stats, node)
				agg.ItemCount++
				agg.TotalSize += catalogResource.SizeBytes
			}
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

		result, err := scanprocessor.New(s.repo, s.indexer, s.log).Process(context.Background(), scanprocessor.ObjectSingleInput(
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
			return objects, extractionStats, err
		}
		extractionStats = scanflow.MergeExtractionCounts(extractionStats, result.Extraction)

		objects++
		for idx, node := range parentChain {
			if !includeBucketAggregate && idx == 0 {
				continue
			}
			agg := scanadapter.EnsureObjectCatalogNodeAggregate(stats, node)
			agg.ItemCount++
			agg.TotalSize += catalogResource.SizeBytes
		}
	}

	if includeBucketAggregate {
		scanadapter.EnsureObjectCatalogNodeAggregate(stats, bucketNode)
	}
	return objects, extractionStats, nil
}
