package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/extractor"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/objectstore"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanchange"
	"github.com/addp/meta/internal/scanstats"
	"gorm.io/gorm"
)

// ObjectStorageScanService 对象存储扫描服务
// 职责：扫描 MinIO、S3 等对象存储的 Bucket 和 Object
type ObjectStorageScanService struct {
	db                *gorm.DB
	log               *slog.Logger
	repo              *metaRepo.ScanRepository     // 数据访问层
	metadataExtractor *extractor.MetadataExtractor // 元数据提取器
	indexer           *IndexerService              // 索引服务
	clientManager     *objectstore.ClientManager
	inlineExtractor   *extractor.InlineObjectMetadataExtractor
}

// NewObjectStorageScanService 创建对象存储扫描服务
func NewObjectStorageScanService(
	db *gorm.DB,
	log *slog.Logger,
	repo *metaRepo.ScanRepository,
	metadataExtractor *extractor.MetadataExtractor,
	indexer *IndexerService,
) *ObjectStorageScanService {
	service := &ObjectStorageScanService{
		db:                db,
		log:               log,
		repo:              repo,
		metadataExtractor: metadataExtractor,
		indexer:           indexer,
		clientManager:     objectstore.NewClientManager(db),
	}
	service.inlineExtractor = extractor.NewInlineObjectMetadataExtractor(service.clientManager, log)
	return service
}

// ============================================================================
// 公共接口方法
// ============================================================================

// ScanPaths 扫描对象存储路径
func (s *ObjectStorageScanService) ScanPaths(
	resource *commonModels.Engine,
	tenantID uint,
	objectPaths, fallback []string,
	scanDepth string,
	reporter ScanProgressReporter,
) (int, int, error) {
	resourceID := resource.ID

	// 标准化 scanDepth
	if scanDepth == "" {
		scanDepth = "deep"
	}

	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return 0, 0, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}

	// 确定扫描路径
	paths := objectPaths
	if len(paths) == 0 {
		paths = fallback
	}

	// 如果仍然没有路径，列出所有 buckets
	if len(paths) == 0 {
		buckets, err := objectstore.ListBuckets(context.Background(), resource, catalogProvider)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to list buckets: %w", err)
		}
		for _, b := range buckets {
			paths = append(paths, b.Name)
		}
	}

	if len(paths) == 0 {
		if reporter != nil {
			reporter.Message("未检测到可扫描的对象路径")
			reporter.SetTotal(0)
		}
		return 0, 0, nil
	}

	if reporter != nil {
		reporter.SetTotal(len(paths))
	}

	buckets, objects, err := s.scanObjectStoragePathsWithCatalog(resource, tenantID, resourceID, catalogProvider, paths, scanDepth, reporter)
	if err != nil {
		return 0, 0, err
	}

	return buckets, objects, nil
}

// ============================================================================
// 核心扫描方法
// ============================================================================

// scanObjectStoragePathsWithCatalog 使用 CatalogProvider 扫描对象存储路径
func (s *ObjectStorageScanService) scanObjectStoragePathsWithCatalog(
	resource *commonModels.Engine,
	tenantID, engineID uint,
	catalogProvider plugin.CatalogProvider,
	paths []string,
	scanDepth string,
	reporter ScanProgressReporter,
) (int, int, error) {
	s.log.Info("进入 scanObjectStoragePathsWithCatalog",
		"engine_id", engineID,
		"tenant_id", tenantID,
		"paths_count", len(paths),
		"scanDepth", scanDepth)

	bucketNodes := make(map[string]*models.MetaNode)
	processedBuckets := make(map[string]bool)
	nodeStats := make(map[uint]*scanstats.NodeAggregate)

	// 记录本次扫描到的所有fingerprints，用于后续清理未扫描到的item
	scannedFingerprints := make(map[string]bool)

	totalBuckets := 0
	totalObjects := 0
	total := len(paths)
	completed := 0

	isDeepScan := strings.EqualFold(scanDepth, "deep")

	for _, rawPath := range paths {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描对象路径 %s", rawPath))
		}
		bucketName, prefix := metapath.SplitObjectPath(rawPath)
		if bucketName == "" {
			s.log.Warn("对象存储路径缺少 bucket，跳过刷新", "path", rawPath)
			if reporter != nil {
				reporter.Message(fmt.Sprintf("对象路径 %s 缺少 bucket 信息，已跳过", rawPath))
			}
			completed++
			if reporter != nil {
				reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": 0})
			}
			continue
		}

		objects, err := objectstore.ListObjects(context.Background(), resource, catalogProvider, bucketName, prefix, isDeepScan)
		if err != nil {
			s.log.Warn("对象存储路径扫描失败",
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

		metas := objectstore.ConvertObjectsToMetadata(context.Background(), objects, bucketName, isDeepScan, resource, s.inlineExtractor)

		bucketNode, ok := bucketNodes[bucketName]
		if !ok {
			attrs := models.JSONMap{"bucket": bucketName}
			bucketNode, err = s.repo.UpsertNode(tenantID, engineID, nil, "bucket", bucketName, &bucketName, attrs)
			if err != nil {
				return totalBuckets, totalObjects, err
			}
			bucketNodes[bucketName] = bucketNode
			totalBuckets++
		}

		fullBucket := prefix == ""
		if fullBucket {
			if !processedBuckets[bucketName] {
				if err := s.repo.ResetNodeState(bucketNode, "running"); err != nil {
					return totalBuckets, totalObjects, err
				}
			}
			processedBuckets[bucketName] = true
		}

		if len(metas) == 0 {
			if fullBucket {
				scanstats.EnsureNodeAggregate(nodeStats, bucketNode)
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
		s.log.Info("传递scanPathPrefix到persistObjectMetas",
			"rawPath", rawPath,
			"bucketName", bucketName,
			"prefix", prefix,
			"scanPathPrefix", scanPathPrefix,
			"fullBucket", fullBucket,
			"metasCount", len(metas),
			"scanDepth", scanDepth)
		objectCount, err := s.persistObjectMetas(resource, tenantID, engineID, bucketNode, metas, nodeStats, fullBucket, scanDepth, scanPathPrefix, scannedFingerprints)
		if err != nil {
			s.log.Error("对象存储元数据持久化失败",
				"engine_id", engineID,
				"tenant_id", tenantID,
				"bucket", bucketName,
				"error", err,
			)
			continue
		}
		totalObjects += objectCount
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
			deletedItems, err := s.repo.SoftDeleteObjectItemsMissingFingerprints(tenantID, engineID, bucketName, scannedFingerprints)
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

		if err := s.repo.FinalizeNodeState(bucketNode, "completed", agg.ItemCount, agg.TotalSize, ""); err != nil {
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
		if err := s.repo.FinalizeObjectPrefixNode(agg.Node, agg.ItemCount, agg.TotalSize); err != nil {
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

	s.log.Info("对象存储路径扫描完成",
		"buckets", totalBuckets,
		"objects", totalObjects,
	)

	return totalBuckets, totalObjects, nil
}

// persistObjectMetas 持久化对象元数据到数据库
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
//   - metas: 对象元数据列表
//   - stats: 节点统计聚合map
//   - includeBucketAggregate: 是否包含bucket级别的聚合
//   - scanDepth: 扫描深度
//   - scanPathPrefix: 扫描路径前缀
//   - scannedFingerprints: 已扫描对象的指纹集合
//
// 返回：(持久化对象数量, error)
func (s *ObjectStorageScanService) persistObjectMetas(
	resource *commonModels.Engine,
	tenantID, engineID uint,
	bucketNode *models.MetaNode,
	metas []format.ObjectMetadata,
	stats map[uint]*scanstats.NodeAggregate,
	includeBucketAggregate bool,
	scanDepth string,
	scanPathPrefix string,
	scannedFingerprints map[string]bool,
) (int, error) {
	objects := 0
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	readableProvider, _ := enginePlugin.(plugin.ContentReadableProvider)

	// 重要：在循环外部只处理一次scanPathPrefix，建立基础父节点
	// 例如：扫描 "addp/shapefile" 时，scanPathPrefix = "shapefile"
	// 需要先创建/查找 "shapefile" 节点作为所有对象的基础父节点
	basePrefixNode := bucketNode
	if scanPathPrefix != "" {
		s.log.Info("处理scanPathPrefix建立基础父节点",
			"scanPathPrefix", scanPathPrefix,
			"bucket", bucketNode.Name)
		node, err := s.repo.EnsureObjectPrefixPath(tenantID, engineID, bucketNode, scanPathPrefix)
		if err != nil {
			return objects, err
		}
		if node != nil && node != bucketNode {
			basePrefixNode = node
			scanstats.EnsureNodeAggregate(stats, node)
		}
		s.log.Info("scanPathPrefix处理完成",
			"basePrefixNode_id", basePrefixNode.ID,
			"basePrefixNode_name", basePrefixNode.Name)
	}
	compositeSkipPaths, compositeItems, compositeWarnings := metaitem.DetectObjectStorageCompositeItems(context.Background(), readableProvider, connInfo, engineID, metas)
	for _, warning := range compositeWarnings {
		s.log.Warn("对象存储组合项检测失败", "bucket", warning.Bucket, "prefix", warning.Prefix, "error", warning.Err)
	}
	compositeCount, err := s.persistObjectStorageCompositeItems(tenantID, engineID, bucketNode, basePrefixNode, compositeItems, stats, includeBucketAggregate, scanPathPrefix, scannedFingerprints)
	if err != nil {
		return objects, err
	}
	objects += compositeCount

	for _, meta := range metas {
		if meta.NodeType == "bucket" {
			if includeBucketAggregate {
				scanstats.EnsureNodeAggregate(stats, bucketNode)
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
		trimmed := metapath.SanitizeObjectPath(meta.Path)

		// 调试日志：记录每个meta对象的处理
		s.log.Info("处理meta对象",
			"meta.NodeType", meta.NodeType,
			"meta.Path", meta.Path,
			"trimmed", trimmed,
			"scanPathPrefix", scanPathPrefix,
			"basePrefixNode", basePrefixNode.Name)

		pathPlan := metaitem.PlanObjectStorageRelativePath(trimmed, scanPathPrefix)
		if pathPlan.ExactBase && meta.NodeType == "prefix" {
			scanstats.EnsureNodeAggregate(stats, basePrefixNode)
		}
		if pathPlan.SkipReason == "空路径" && includeBucketAggregate {
			scanstats.EnsureNodeAggregate(stats, bucketNode)
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
				if meta.NodeType == "object" && isLast {
					break
				}
				fullName := metapath.ComposeNodeFullName(segment, currentParent, "/")
				attrs := models.JSONMap{
					"bucket": meta.Bucket,
					"path":   strings.Join(pathPlan.Segments[:idx+1], "/") + "/", // 路径规范：目录路径必须以 / 结尾
				}
				childNode, err := s.repo.UpsertNode(tenantID, engineID, currentParent, "prefix", segment, &fullName, attrs)
				if err != nil {
					return objects, err
				}
				currentParent = childNode
				parentChain = append(parentChain, childNode)
				scanstats.EnsureNodeAggregate(stats, childNode)
			}
		}

		if meta.NodeType != "object" {
			continue
		}
		if compositeSkipPaths[meta.Path] {
			continue
		}

		itemPlan := metaitem.PlanObjectStorageSingleItem(engineID, meta, trimmed)
		if scannedFingerprints != nil {
			scannedFingerprints[itemPlan.Fingerprint] = true
		}

		// 检查记录是否已存在（包括软删除的记录）
		existingItem, itemExists, err := s.repo.FindItemByFingerprintUnscoped(itemPlan.Fingerprint)
		if err != nil {
			return objects, err
		}
		needsUpdate := scanchange.ShouldUpdateObject(existingItem, meta)

		// 浅度扫描时，如果对象未变化，跳过更新（保留已有的深度元数据）
		if !strings.EqualFold(scanDepth, "deep") && itemExists && !needsUpdate {
			s.log.Debug("对象未变化，跳过更新",
				"bucket", meta.Bucket,
				"path", meta.Path,
			)
			// 仍然需要统计
			objects++
			for idx, node := range parentChain {
				if !includeBucketAggregate && idx == 0 {
					continue
				}
				agg := scanstats.EnsureNodeAggregate(stats, node)
				agg.ItemCount++
				agg.TotalSize += meta.SizeBytes
			}
			continue
		}

		// 根据扫描深度决定是否提取深度元数据
		var enhancedAttrs models.JSONMap
		if strings.EqualFold(scanDepth, "deep") {
			// 深度扫描：提取详细元数据（用于文件预览/搜索）
			// 传递meta.Path用于正确的fingerprint生成
			enhancedAttrs = s.metadataExtractor.ExtractEnhancedMetadataWithCache(engineID, meta, itemPlan.Attributes, meta.Path)
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

		sizeVal := meta.SizeBytes

		// fullName 已在上方通过 JoinObjectPath(bucket, dir, name) 计算，直接复用
		// 不再基于 scanPathPrefix 重新拼接，避免前缀重复（如 bucket/prefix/prefix/file）

		s.log.Info("计算fullName和父节点",
			"meta.Bucket", meta.Bucket,
			"meta.Path", meta.Path,
			"trimmed", trimmed,
			"scanPathPrefix", scanPathPrefix,
			"calculated_fullName", itemPlan.FullName,
			"currentParent_id", currentParent.ID,
			"currentParent_name", currentParent.Name,
			"objectName", itemPlan.ObjectName)

		// 湖表属性（itemType 和 itemName 已在上方确定）
		if itemPlan.ItemType == "lake_table" {
			// physical_path 保留原始路径（含扩展名），供 ReadFile 使用
			physicalPath := meta.Bucket + "/" + meta.Path
			metaattr.SetStorage(enhancedAttrs, "physical_path", physicalPath)
		}
		metaitem.ApplyContainerSummary(enhancedAttrs, itemPlan.DataItem)
		if itemPlan.DataItem != nil && itemPlan.DataItem.DataType == dataitem.DataTypeContainer && readableProvider != nil {
			reader, err := readableProvider.OpenContent(context.Background(), connInfo, objectCatalogPathForContent(engineID, meta.Bucket, meta.Path), plugin.ReadOptions{})
			if err != nil {
				s.log.Warn("枚举对象容器内部对象失败，保留容器摘要", "bucket", meta.Bucket, "path", meta.Path, "error", err)
			} else {
				if err := metaitem.EnrichContainerChildren(context.Background(), enhancedAttrs, itemPlan.DataItem, reader); err != nil {
					s.log.Warn("枚举对象容器内部对象失败，保留容器摘要", "bucket", meta.Bucket, "path", meta.Path, "error", err)
				}
				_ = reader.Close()
			}
		}

		item, err := s.repo.UpsertItem(tenantID, engineID, currentParent, itemPlan.ItemType, itemPlan.ItemName, itemPlan.FullName, enhancedAttrs, nil, &sizeVal, meta.LastModified)
		if err != nil {
			return objects, err
		}

		// 只在deep扫描时索引（basic扫描的数据不完整）
		if scanDepth == "deep" {
			s.indexer.IndexObjectAsset(resource, tenantID, engineID, meta, trimmed, itemPlan.FullName, item)
		}

		objects++
		for idx, node := range parentChain {
			if !includeBucketAggregate && idx == 0 {
				continue
			}
			agg := scanstats.EnsureNodeAggregate(stats, node)
			agg.ItemCount++
			agg.TotalSize += meta.SizeBytes
		}
	}

	if includeBucketAggregate {
		scanstats.EnsureNodeAggregate(stats, bucketNode)
	}
	return objects, nil
}

func (s *ObjectStorageScanService) persistObjectStorageCompositeItems(
	tenantID, engineID uint,
	bucketNode, basePrefixNode *models.MetaNode,
	items []metaitem.ObjectStorageCompositeItem,
	stats map[uint]*scanstats.NodeAggregate,
	includeBucketAggregate bool,
	scanPathPrefix string,
	scannedFingerprints map[string]bool,
) (int, error) {
	count := 0
	for _, composite := range items {
		if composite.Item == nil {
			continue
		}
		itemPlan, ok := metaitem.PlanObjectStorageCompositeItem(engineID, composite)
		if !ok {
			continue
		}

		if scannedFingerprints != nil {
			scannedFingerprints[itemPlan.Fingerprint] = true
		}
		parentNode, err := s.ensureObjectPrefixNodes(tenantID, engineID, bucketNode, basePrefixNode, composite.Prefix, scanPathPrefix, stats)
		if err != nil {
			return count, err
		}

		sizeVal := itemPlan.SizeBytes
		if _, err := s.repo.UpsertItem(tenantID, engineID, parentNode, itemPlan.ItemType, itemPlan.ItemName, itemPlan.FullName, itemPlan.Attributes, nil, &sizeVal, nil); err != nil {
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
			agg := scanstats.EnsureNodeAggregate(stats, node)
			agg.ItemCount++
			agg.TotalSize += itemPlan.SizeBytes
		}
	}
	return count, nil
}

func (s *ObjectStorageScanService) ensureObjectPrefixNodes(
	tenantID, engineID uint,
	bucketNode, basePrefixNode *models.MetaNode,
	prefix string,
	scanPathPrefix string,
	stats map[uint]*scanstats.NodeAggregate,
) (*models.MetaNode, error) {
	parent := bucketNode
	if basePrefixNode != nil {
		parent = basePrefixNode
	}
	parentNode, createdNodes, err := s.repo.EnsureObjectPrefixRelativePath(tenantID, engineID, bucketNode, parent, metaitem.ParentObjectPath(prefix), scanPathPrefix)
	if err != nil {
		return nil, err
	}
	for _, node := range createdNodes {
		scanstats.EnsureNodeAggregate(stats, node)
	}
	return parentNode, nil
}

// ============================================================================
// MinIO 客户端管理
// ============================================================================

// FetchObjectContent 获取对象内容
func (s *ObjectStorageScanService) FetchObjectContent(
	ctx context.Context,
	engineID, tenantID uint,
	bucket, objectPath string,
	maxSize int64,
) ([]byte, string, error) {
	return s.clientManager.FetchObjectContent(ctx, engineID, tenantID, bucket, objectPath, maxSize)
}

func objectCatalogPathForContent(engineID uint, bucket, objectPath string) plugin.CatalogPath {
	segments := []plugin.CatalogSegment{{
		Term: plugin.CatalogTermBucket,
		Kind: plugin.CatalogKindBucket,
		Name: bucket,
	}}
	parts := strings.Split(strings.Trim(objectPath, "/"), "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		term := plugin.CatalogTermPrefix
		kind := plugin.CatalogKindPrefix
		if i == len(parts)-1 {
			term = plugin.CatalogTermObject
			kind = plugin.CatalogKindObject
		}
		segments = append(segments, plugin.CatalogSegment{Term: term, Kind: kind, Name: part})
	}
	return plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
		Segments: segments,
	}
}
