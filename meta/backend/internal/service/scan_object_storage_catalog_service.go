package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanchange"
	"github.com/addp/meta/internal/scantask"
	"gorm.io/gorm"
)

type objectCatalogNodeAggregate struct {
	Node      *models.MetaNode
	ItemCount int
	TotalSize int64
}

type objectCatalogPathTarget struct {
	Bucket string
	Prefix string
	Object string
}

type ObjectCatalogScanResult struct {
	CatalogNodes int
	Items        int
	Extraction   scantask.ExtractionCounts
}

func ensureObjectCatalogNodeAggregate(stats map[uint]*objectCatalogNodeAggregate, node *models.MetaNode) *objectCatalogNodeAggregate {
	if agg, ok := stats[node.ID]; ok {
		return agg
	}
	agg := &objectCatalogNodeAggregate{Node: node}
	stats[node.ID] = agg
	return agg
}

func listObjectCatalogBucketNodes(ctx context.Context, resource *commonModels.Engine, catalogProvider plugin.CatalogProvider) ([]plugin.CatalogEntry, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectRootPath(resource.ID), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}

	buckets := make([]plugin.CatalogEntry, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == plugin.CatalogKindBucket {
			buckets = append(buckets, node)
		}
	}
	return buckets, nil
}

func listObjectCatalogLeaves(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	bucketName, prefix string,
	recursive bool,
) ([]plugin.CatalogEntry, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectDirectoryPath(resource.ID, bucketName, prefix), plugin.ListOptions{Recursive: recursive})
	if err != nil {
		return nil, err
	}

	objects := make([]plugin.CatalogEntry, 0, len(nodes))
	for _, node := range nodes {
		if node.Role == plugin.CatalogRoleLeaf {
			objects = append(objects, node)
		}
	}
	return objects, nil
}

func resolveObjectCatalogTarget(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	rawPath string,
) (objectCatalogPathTarget, error) {
	bucketName, objectPath := metapath.SplitObjectPath(rawPath)
	target := objectCatalogPathTarget{Bucket: bucketName}
	if bucketName == "" {
		return target, nil
	}
	if objectPath == "" {
		return target, nil
	}
	node, err := catalogProvider.ResolvePath(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectItemPath(resource.ID, bucketName, objectPath))
	if err == nil && node != nil && node.Role == plugin.CatalogRoleLeaf {
		target.Object = objectPath
		return target, nil
	}
	target.Prefix = objectPath
	return target, nil
}

func readObjectCatalogLeaf(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	bucketName, objectPath string,
) ([]plugin.CatalogEntry, error) {
	node, err := catalogProvider.ResolvePath(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectItemPath(resource.ID, bucketName, objectPath))
	if err != nil {
		return nil, err
	}
	if node == nil || node.Role != plugin.CatalogRoleLeaf {
		return nil, nil
	}
	return []plugin.CatalogEntry{*node}, nil
}

func objectCatalogEntriesToStorageResources(
	objects []plugin.CatalogEntry,
	bucket string,
) []metacatalog.StorageResource {
	resources := make([]metacatalog.StorageResource, 0, len(objects))
	for _, obj := range objects {
		resources = append(resources, metacatalog.ObjectStorageResourceFromNode(bucket, obj))
	}
	return resources
}

// ObjectStorageCatalogScanService 对象存储 catalog 扫描服务。
// 职责：按插件 catalog model 扫描 bucket/prefix/object 层级。
type ObjectStorageCatalogScanService struct {
	db      *gorm.DB
	log     *slog.Logger
	repo    *metaRepo.ScanRepository // 数据访问层
	indexer *IndexerService          // 索引服务
}

// NewObjectStorageCatalogScanService 创建对象存储 catalog 扫描服务。
func NewObjectStorageCatalogScanService(
	db *gorm.DB,
	log *slog.Logger,
	repo *metaRepo.ScanRepository,
	indexer *IndexerService,
) *ObjectStorageCatalogScanService {
	service := &ObjectStorageCatalogScanService{
		db:      db,
		log:     log,
		repo:    repo,
		indexer: indexer,
	}
	return service
}

// ============================================================================
// 公共接口方法
// ============================================================================

// ScanPaths 扫描对象存储 catalog 路径。
func (s *ObjectStorageCatalogScanService) ScanPaths(
	resource *commonModels.Engine,
	tenantID uint,
	catalogPaths, fallback []string,
	scanDepth string,
	force bool,
	reporter ScanProgressReporter,
) (ObjectCatalogScanResult, error) {
	metaenrich.RegisterItemResolvers()

	resourceID := resource.ID

	// 标准化 scanDepth
	scanDepth = scanDepthOrDefault(scanDepth, "deep")

	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return ObjectCatalogScanResult{}, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return ObjectCatalogScanResult{}, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}
	itemTerm := catalogLeafTermForPlugin(p, plugin.CatalogTermObject)

	paths, err := resolveCatalogScanPaths(
		context.Background(),
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
		return ObjectCatalogScanResult{}, err
	}

	return s.scanObjectCatalogPaths(resource, tenantID, resourceID, catalogProvider, paths, scanDepth, force, reporter, itemTerm)
}

// ============================================================================
// 核心扫描方法
// ============================================================================

// scanObjectCatalogPaths 使用 CatalogProvider 扫描对象存储 catalog 路径。
func (s *ObjectStorageCatalogScanService) scanObjectCatalogPaths(
	resource *commonModels.Engine,
	tenantID, engineID uint,
	catalogProvider plugin.CatalogProvider,
	paths []string,
	scanDepth string,
	force bool,
	reporter ScanProgressReporter,
	itemTerm string,
) (ObjectCatalogScanResult, error) {
	s.log.Info("进入 scanObjectCatalogPaths",
		"engine_id", engineID,
		"tenant_id", tenantID,
		"paths_count", len(paths),
		"scanDepth", scanDepth)

	bucketNodes := make(map[string]*models.MetaNode)
	processedBuckets := make(map[string]bool)
	nodeStats := make(map[uint]*objectCatalogNodeAggregate)

	// 记录本次扫描到的所有fingerprints，用于后续清理未扫描到的item
	scannedFingerprints := make(map[string]bool)

	totalBuckets := 0
	totalObjects := 0
	extractionStats := scantask.ExtractionCounts{}
	total := len(paths)
	completed := 0

	isDeepScan := strings.EqualFold(scanDepth, "deep")
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return ObjectCatalogScanResult{}, err
	}
	rootNode, err := ensureCatalogRootNode(s.repo, tenantID, resource, enginePlugin)
	if err != nil {
		return ObjectCatalogScanResult{}, err
	}

	for _, rawPath := range paths {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描对象路径 %s", rawPath))
		}
		target, err := resolveObjectCatalogTarget(context.Background(), resource, catalogProvider, rawPath)
		if err != nil {
			s.log.Warn("对象 catalog 路径解析失败",
				"engine_id", engineID,
				"tenant_id", tenantID,
				"path", rawPath,
				"error", err,
			)
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 解析失败: %v", rawPath, err))
			}
			completed++
			if reporter != nil {
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}
		bucketName := target.Bucket
		prefix := target.Prefix
		if bucketName == "" {
			s.log.Warn("对象 catalog 路径缺少 bucket，跳过刷新", "path", rawPath)
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 缺少 bucket 信息，已跳过", rawPath))
			}
			completed++
			if reporter != nil {
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}

		var objects []plugin.CatalogEntry
		if target.Object != "" {
			objects, err = readObjectCatalogLeaf(context.Background(), resource, catalogProvider, bucketName, target.Object)
		} else {
			objects, err = listObjectCatalogLeaves(context.Background(), resource, catalogProvider, bucketName, prefix, isDeepScan)
		}
		if err != nil {
			s.log.Warn("对象 catalog 路径扫描失败",
				"engine_id", engineID,
				"tenant_id", tenantID,
				"path", rawPath,
				"error", err,
			)
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 扫描失败: %v", rawPath, err))
			}
			completed++
			if reporter != nil {
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}

		resources := objectCatalogEntriesToStorageResources(objects, bucketName)

		bucketNode, ok := bucketNodes[bucketName]
		if !ok {
			attrs := models.JSONMap{"bucket": bucketName}
			bucketNode, err = s.repo.UpsertNode(tenantID, engineID, rootNode, "bucket", bucketName, &bucketName, attrs)
			if err != nil {
				return ObjectCatalogScanResult{CatalogNodes: totalBuckets, Items: totalObjects, Extraction: extractionStats}, err
			}
			bucketNodes[bucketName] = bucketNode
			totalBuckets++
		}

		fullBucket := prefix == "" && target.Object == ""
		if fullBucket {
			if !processedBuckets[bucketName] {
				if err := s.repo.ResetNodeState(bucketNode, "running"); err != nil {
					return ObjectCatalogScanResult{CatalogNodes: totalBuckets, Items: totalObjects, Extraction: extractionStats}, err
				}
			}
			processedBuckets[bucketName] = true
		}

		if len(resources) == 0 {
			if fullBucket {
				ensureObjectCatalogNodeAggregate(nodeStats, bucketNode)
			}
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 未发现新对象", rawPath))
			}
			completed++
			if reporter != nil {
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}

		// 传递扫描路径前缀，用于正确计算fullName
		scanPathPrefix := prefix
		if target.Object != "" {
			scanPathPrefix = metacatalog.ParentObjectPath(target.Object)
		}
		s.log.Info("传递scanPathPrefix到persistObjectResources",
			"rawPath", rawPath,
			"bucketName", bucketName,
			"prefix", prefix,
			"scanPathPrefix", scanPathPrefix,
			"fullBucket", fullBucket,
			"resourceCount", len(resources),
			"scanDepth", scanDepth)
		objectCount, pathExtractionStats, err := s.persistObjectResources(resource, tenantID, engineID, bucketNode, resources, nodeStats, fullBucket, scanDepth, force, scanPathPrefix, scannedFingerprints, itemTerm)
		if err != nil {
			s.log.Error("对象 catalog 元数据持久化失败",
				"engine_id", engineID,
				"tenant_id", tenantID,
				"bucket", bucketName,
				"error", err,
			)
			continue
		}
		totalObjects += objectCount
		extractionStats = mergeExtractionCounts(extractionStats, pathExtractionStats)
		completed++
		if reporter != nil {
			reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": objectCount})
		}
	}

	// 清理未在本次扫描中出现的旧item
	if isDeepScan && len(scannedFingerprints) > 0 {
		for bucketName := range processedBuckets {
			if bucketNodes[bucketName] == nil {
				continue
			}
			deletedItems, err := s.repo.SoftDeleteObjectMetaItemsMissingFingerprints(tenantID, engineID, bucketName, scannedFingerprints)
			if err != nil {
				s.log.Warn("查询已存在对象元数据失败",
					"bucket", bucketName,
					"error", err,
				)
				continue
			}
			for _, item := range deletedItems {
				s.log.Info("对象已不存在，标记删除",
					"bucket", bucketName,
					"fingerprint", item.Fingerprint,
					"name", item.Name,
				)
			}
		}
	}

	// 完成bucket节点的扫描状态更新
	for bucketName, bucketNode := range bucketNodes {
		if !processedBuckets[bucketName] {
			continue
		}

		agg, ok := nodeStats[bucketNode.ID]
		if !ok {
			continue
		}

		if err := s.repo.FinalizeNodeStateWithDepth(bucketNode, "completed", agg.ItemCount, agg.TotalSize, "", scanDepth); err != nil {
			s.log.Warn("完成bucket节点状态更新失败",
				"bucket", bucketName,
				"error", err,
			)
		}
	}

	// 更新所有子目录节点的统计信息和扫描状态
	// 遍历 nodeStats 中的所有节点，更新那些不是 bucket 的节点
	for nodeID, agg := range nodeStats {
		// 跳过 bucket 节点（已在上面处理）
		if agg.Node.NodeType == "bucket" {
			continue
		}

		// 更新子目录节点的统计信息和扫描状态
		// 说明：扫描 bucket 时会遍历所有子目录，所以子目录的扫描状态也应该是"completed"
		if err := s.repo.FinalizeObjectCatalogPrefixNodeWithDepth(agg.Node, agg.ItemCount, agg.TotalSize, scanDepth); err != nil {
			s.log.Warn("更新子目录节点统计信息失败",
				"node_id", nodeID,
				"node_name", agg.Node.Name,
				"error", err,
			)
		} else {
			s.log.Debug("成功更新子目录节点统计",
				"node_id", nodeID,
				"node_name", agg.Node.Name,
				"item_count", agg.ItemCount,
				"total_size", agg.TotalSize,
			)
		}
	}

	s.log.Info("对象 catalog 路径扫描完成",
		"buckets", totalBuckets,
		"objects", totalObjects,
	)

	return ObjectCatalogScanResult{CatalogNodes: totalBuckets, Items: totalObjects, Extraction: extractionStats}, nil
}

// persistObjectResources 持久化对象 catalog leaf 到数据库
//
// 职责划分：
// 1. 目录树构建：根据对象路径构建层级目录节点
// 2. 对象元数据持久化：保存对象的基本信息和增强元数据
// 3. 文档向量化：为支持的文档类型生成向量嵌入（如果启用）
// 4. 搜索索引更新：将对象信息同步到Meilisearch
// 5. 统计聚合：更新各层级节点的统计信息（对象数、总大小）
//
// 参数：
//   - resource: 数据源引擎配置
//   - tenantID: 租户ID
//   - engineID: 引擎ID
//   - bucketNode: Bucket节点
//   - resources: 对象 catalog leaf 资源列表
//   - stats: 节点统计聚合map
//   - includeBucketAggregate: 是否包含bucket级别的聚合
//   - scanDepth: 扫描深度
//   - scanPathPrefix: 扫描路径前缀
//   - scannedFingerprints: 已扫描对象的指纹集合
//
// 返回：(持久化对象数量, 文档抽取统计, error)
func (s *ObjectStorageCatalogScanService) persistObjectResources(
	resource *commonModels.Engine,
	tenantID, engineID uint,
	bucketNode *models.MetaNode,
	resources []metacatalog.StorageResource,
	stats map[uint]*objectCatalogNodeAggregate,
	includeBucketAggregate bool,
	scanDepth string,
	force bool,
	scanPathPrefix string,
	scannedFingerprints map[string]bool,
	itemTerm string,
) (int, scantask.ExtractionCounts, error) {
	objects := 0
	extractionStats := scantask.ExtractionCounts{}
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, extractionStats, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	readableProvider, _ := enginePlugin.(plugin.ContentReadableProvider)
	if readableProvider != nil {
		s.detectObjectCatalogResourceFormats(context.Background(), readableProvider, connInfo, resources)
	}

	// 重要：在循环外部只处理一次scanPathPrefix，建立基础父节点
	// 例如：扫描 "addp/shapefile" 时，scanPathPrefix = "shapefile"
	// 需要先创建/查找 "shapefile" 节点作为所有对象的基础父节点
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
			ensureObjectCatalogNodeAggregate(stats, node)
		}
		s.log.Info("scanPathPrefix处理完成",
			"basePrefixNode_id", basePrefixNode.ID,
			"basePrefixNode_name", basePrefixNode.Name)
	}
	var compositeSkipPaths map[string]bool
	var compositeItems []metacatalog.ObjectCatalogCompositeItem
	if strings.EqualFold(scanDepth, "deep") {
		var compositeWarnings []metacatalog.ObjectCatalogCompositeDetectionError
		compositeSkipPaths, compositeItems, compositeWarnings = metacatalog.DetectObjectCatalogCompositeItems(context.Background(), readableProvider, connInfo, engineID, resources)
		for _, warning := range compositeWarnings {
			s.log.Warn("对象 catalog 组合项检测失败", "bucket", warning.Bucket, "prefix", warning.Prefix, "error", warning.Err)
		}
	} else {
		compositeSkipPaths = map[string]bool{}
	}
	compositeCount, err := s.persistObjectCatalogCompositeItems(tenantID, engineID, bucketNode, basePrefixNode, compositeItems, stats, includeBucketAggregate, scanPathPrefix, scannedFingerprints, itemTerm)
	if err != nil {
		return objects, extractionStats, err
	}
	objects += compositeCount

	for _, catalogResource := range resources {
		if catalogResource.NodeType == "bucket" {
			if includeBucketAggregate {
				ensureObjectCatalogNodeAggregate(stats, bucketNode)
			}
			continue
		}

		// 使用基础前缀节点作为起点
		parentChain := []*models.MetaNode{bucketNode}
		if basePrefixNode != bucketNode {
			parentChain = append(parentChain, basePrefixNode)
		}
		currentParent := basePrefixNode

		// 然后处理相对路径（相对于scanPathPrefix）
		trimmed := metapath.SanitizeObjectPath(catalogResource.Path)

		// 调试日志：记录每个meta对象的处理
		s.log.Info("处理meta对象",
			"resource.NodeType", catalogResource.NodeType,
			"resource.Path", catalogResource.Path,
			"trimmed", trimmed,
			"scanPathPrefix", scanPathPrefix,
			"basePrefixNode", basePrefixNode.Name)

		pathPlan := metacatalog.PlanObjectCatalogRelativePath(trimmed, scanPathPrefix)
		if pathPlan.ExactBase && catalogResource.NodeType == "prefix" {
			ensureObjectCatalogNodeAggregate(stats, basePrefixNode)
		}
		if pathPlan.SkipReason == "空路径" && includeBucketAggregate {
			ensureObjectCatalogNodeAggregate(stats, bucketNode)
		}

		if pathPlan.SkipReason != "" {
			s.log.Info("跳过或特殊处理",
				"reason", pathPlan.SkipReason,
				"segmentsToProcess", pathPlan.Segments)
		} else if len(pathPlan.Segments) > 0 {
			s.log.Info("准备处理segments",
				"segmentsToProcess", pathPlan.Segments)
		}

		// 处理segments（如果有）
		if len(pathPlan.Segments) > 0 {
			for idx, segment := range pathPlan.Segments {
				isLast := idx == len(pathPlan.Segments)-1
				if catalogResource.NodeType == "object" && isLast {
					break
				}
				fullName := metapath.ComposeNodeFullName(segment, currentParent, "/")
				attrs := models.JSONMap{
					"bucket": catalogResource.RootName,
					"path":   strings.Join(pathPlan.Segments[:idx+1], "/") + "/", // 路径规范：目录路径必须以 / 结尾
				}
				childNode, err := s.repo.UpsertNode(tenantID, engineID, currentParent, "prefix", segment, &fullName, attrs)
				if err != nil {
					return objects, extractionStats, err
				}
				currentParent = childNode
				parentChain = append(parentChain, childNode)
				ensureObjectCatalogNodeAggregate(stats, childNode)
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

		// 检查记录是否已存在（包括软删除的记录）
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
			// 仍然需要统计
			objects++
			for idx, node := range parentChain {
				if !includeBucketAggregate && idx == 0 {
					continue
				}
				agg := ensureObjectCatalogNodeAggregate(stats, node)
				agg.ItemCount++
				agg.TotalSize += catalogResource.SizeBytes
			}
			continue
		}

		// 根据扫描深度决定是否提取深度元数据
		var enhancedAttrs models.JSONMap
		if strings.EqualFold(scanDepth, "deep") {
			// 深度扫描的主事实统一由 catalogSingleItemProcessor 通过 metaenrich.EnrichResourceAttributes 写入。
			enhancedAttrs = itemPlan.Attributes
		} else if itemExists {
			// 浅层扫描 + 记录已存在：保留原有attributes，只更新基础字段
			// 但仍需要更新node_id（文件可能移动到了不同的目录）
			enhancedAttrs = existingItem.Attributes // 使用已有的attributes
			for k, v := range itemPlan.Attributes {
				enhancedAttrs[k] = v
			}
		} else {
			// 浅层扫描 + 新记录：使用基础属性
			enhancedAttrs = itemPlan.Attributes
		}

		// fullName 已在上方通过 JoinObjectPath(bucket, dir, name) 计算，直接复用
		// 不再基于 scanPathPrefix 重新拼接，避免前缀重复（如 bucket/prefix/prefix/file）

		s.log.Info("计算fullName和父节点",
			"resource.RootName", catalogResource.RootName,
			"resource.Path", catalogResource.Path,
			"trimmed", trimmed,
			"scanPathPrefix", scanPathPrefix,
			"calculated_fullName", itemPlan.FullName,
			"currentParent_id", currentParent.ID,
			"currentParent_name", currentParent.Name,
			"objectName", itemPlan.ObjectName)

		result, err := catalogDataItemProcessor(s.repo, s.indexer, s.log).Process(context.Background(), catalogSingleItemInput{
			Resource:           resource,
			TenantID:           tenantID,
			EngineID:           engineID,
			ParentNode:         currentParent,
			ItemType:           itemPlan.ItemType,
			ItemName:           itemPlan.ItemName,
			FullName:           itemPlan.FullName,
			Attributes:         enhancedAttrs,
			Detected:           itemPlan.DataItem,
			ContentReader:      readableProvider,
			ConnInfo:           connInfo,
			CatalogPath:        catalogResource.CatalogPath,
			CatalogPathFor:     func(string) plugin.CatalogPath { return catalogResource.CatalogPath },
			PhysicalPath:       catalogResource.FullPath,
			IndexRootName:      catalogResource.RootName,
			IndexPath:          catalogResource.Path,
			IndexRelativePath:  trimmed,
			SizeBytes:          catalogResource.SizeBytes,
			DataUpdatedAt:      catalogResource.LastModified,
			ScanDepth:          scanDepth,
			IncludeAccessIndex: true,
		})
		if err != nil {
			return objects, extractionStats, err
		}
		extractionStats = mergeExtractionCounts(extractionStats, result.Extraction)

		objects++
		for idx, node := range parentChain {
			if !includeBucketAggregate && idx == 0 {
				continue
			}
			agg := ensureObjectCatalogNodeAggregate(stats, node)
			agg.ItemCount++
			agg.TotalSize += catalogResource.SizeBytes
		}
	}

	if includeBucketAggregate {
		ensureObjectCatalogNodeAggregate(stats, bucketNode)
	}
	return objects, extractionStats, nil
}

func (s *ObjectStorageCatalogScanService) persistObjectCatalogCompositeItems(
	tenantID, engineID uint,
	bucketNode, basePrefixNode *models.MetaNode,
	items []metacatalog.ObjectCatalogCompositeItem,
	stats map[uint]*objectCatalogNodeAggregate,
	includeBucketAggregate bool,
	scanPathPrefix string,
	scannedFingerprints map[string]bool,
	itemTerm string,
) (int, error) {
	count := 0
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
			return count, err
		}

		sizeVal := itemPlan.SizeBytes
		rowCount := itemRowCountFromMetaAttributes(itemPlan.Attributes)
		if _, err := s.repo.UpsertItemWithDepth(tenantID, engineID, parentNode, itemPlan.ItemType, itemPlan.ItemName, itemPlan.FullName, itemPlan.Attributes, rowCount, &sizeVal, nil, models.ScannedDepthDeep); err != nil {
			return count, err
		}
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
			agg := ensureObjectCatalogNodeAggregate(stats, node)
			agg.ItemCount++
			agg.TotalSize += itemPlan.SizeBytes
		}
	}
	return count, nil
}

func (s *ObjectStorageCatalogScanService) detectObjectCatalogResourceFormats(
	ctx context.Context,
	readableProvider plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	resources []metacatalog.StorageResource,
) {
	for i := range resources {
		if resources[i].NodeType != "object" || !needsContentFormatDetection(resources[i].Format) {
			continue
		}
		detected, err := detectObjectCatalogResourceFormat(ctx, readableProvider, connInfo, resources[i])
		if err != nil {
			if s.log != nil {
				s.log.Warn("对象内容格式嗅探失败，保留基础格式", "bucket", resources[i].RootName, "path", resources[i].Path, "error", err)
			}
			continue
		}
		if detected != "" {
			resources[i].Format = detected
		}
	}
}

func detectObjectCatalogResourceFormat(
	ctx context.Context,
	readableProvider plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	resource metacatalog.StorageResource,
) (string, error) {
	if readableProvider == nil {
		return "", nil
	}
	detected, err := metaenrich.DetectSingleFileFormat(ctx, readableProvider, connInfo, resource.CatalogPath, resource.Path)
	if err != nil {
		return "", err
	}
	if detected == format.FormatUnknown {
		return "", nil
	}
	return string(detected), nil
}

func needsContentFormatDetection(formatName string) bool {
	return format.NormalizeFormat(formatName) == format.FormatUnknown
}

func itemRowCountFromMetaAttributes(attrs map[string]interface{}) *int64 {
	tableInfo := tableInfoFromMetaAttributes(attrs)
	if tableInfo == nil || tableInfo.RowCount == nil || *tableInfo.RowCount <= 0 {
		return nil
	}
	rowCount := *tableInfo.RowCount
	return &rowCount
}

func tableInfoFromMetaAttributes(attrs map[string]interface{}) *datatype.TableInfo {
	return datatype.TableInfoFromPayload(commonJSON.Section(attrs, "type_info.table"), "")
}

func itemFingerprintForExtraction(engineID uint, catalogResource metacatalog.StorageResource) string {
	fullName := catalogResource.FullPath
	if fullName == "" {
		fullName = commonModels.JoinObjectPath(catalogResource.RootName, "", catalogResource.Path)
	}
	return commonModels.GenerateItemFingerprint(engineID, fullName)
}

func (s *ObjectStorageCatalogScanService) ensureObjectCatalogPrefixNodes(
	tenantID, engineID uint,
	bucketNode, basePrefixNode *models.MetaNode,
	parentPath string,
	scanPathPrefix string,
	stats map[uint]*objectCatalogNodeAggregate,
) (*models.MetaNode, error) {
	parent := bucketNode
	if basePrefixNode != nil {
		parent = basePrefixNode
	}
	parentNode, createdNodes, err := s.repo.EnsureObjectCatalogPrefixRelativePath(tenantID, engineID, bucketNode, parent, parentPath, scanPathPrefix)
	if err != nil {
		return nil, err
	}
	for _, node := range createdNodes {
		ensureObjectCatalogNodeAggregate(stats, node)
	}
	return parentNode, nil
}
