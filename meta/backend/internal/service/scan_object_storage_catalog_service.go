package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/extractor"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/metatext"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanchange"
	"gorm.io/gorm"
)

type objectCatalogInlineMetadataExtractor interface {
	ShouldExtract(key, contentType string, sizeBytes int64) bool
	Extract(ctx context.Context, resource *commonModels.Engine, bucket, key, contentType string, size int64, lastModified time.Time, etag string, openContent func() (io.ReadCloser, error)) map[string]interface{}
}

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

func ensureObjectCatalogNodeAggregate(stats map[uint]*objectCatalogNodeAggregate, node *models.MetaNode) *objectCatalogNodeAggregate {
	if agg, ok := stats[node.ID]; ok {
		return agg
	}
	agg := &objectCatalogNodeAggregate{Node: node}
	stats[node.ID] = agg
	return agg
}

func listObjectCatalogBucketNodes(ctx context.Context, resource *commonModels.Engine, catalogProvider plugin.CatalogProvider) ([]plugin.CatalogNode, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: resource.ID,
	}, plugin.ListOptions{})
	if err != nil {
		return nil, err
	}

	buckets := make([]plugin.CatalogNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == plugin.CatalogKindBucket {
			buckets = append(buckets, node)
		}
	}
	return buckets, nil
}

func listObjectCatalogItems(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	bucketName, prefix string,
	recursive bool,
) ([]plugin.CatalogNode, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectDirectoryPath(resource.ID, bucketName, prefix), plugin.ListOptions{Recursive: recursive})
	if err != nil {
		return nil, err
	}

	objects := make([]plugin.CatalogNode, 0, len(nodes))
	for _, node := range nodes {
		if node.IsItem {
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
	if err == nil && node != nil && node.IsItem {
		target.Object = objectPath
		return target, nil
	}
	target.Prefix = objectPath
	return target, nil
}

func readObjectCatalogItem(
	ctx context.Context,
	resource *commonModels.Engine,
	catalogProvider plugin.CatalogProvider,
	bucketName, objectPath string,
) ([]plugin.CatalogNode, error) {
	node, err := catalogProvider.ResolvePath(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectItemPath(resource.ID, bucketName, objectPath))
	if err != nil {
		return nil, err
	}
	if node == nil || !node.IsItem {
		return nil, nil
	}
	return []plugin.CatalogNode{*node}, nil
}

func objectCatalogNodesToStorageResources(
	ctx context.Context,
	objects []plugin.CatalogNode,
	bucket string,
	deepScan bool,
	resource *commonModels.Engine,
	inlineExtractor objectCatalogInlineMetadataExtractor,
) []metacatalog.StorageResource {
	resources := make([]metacatalog.StorageResource, 0, len(objects))
	for _, obj := range objects {
		catalogResource := metacatalog.ObjectStorageResourceFromNode(bucket, obj)
		if deepScan && inlineExtractor != nil && catalogResource.LastModified != nil && inlineExtractor.ShouldExtract(catalogResource.Path, catalogResource.ContentType, catalogResource.SizeBytes) {
			catalogResource.ExtractedAttributes = inlineExtractor.Extract(ctx, resource, bucket, catalogResource.Path, catalogResource.ContentType, catalogResource.SizeBytes, *catalogResource.LastModified, catalogResource.ETag, func() (io.ReadCloser, error) {
				return openObjectCatalogContent(ctx, resource, bucket, catalogResource.Path)
			})
		}
		resources = append(resources, catalogResource)
	}
	return resources
}

func openObjectCatalogContent(ctx context.Context, resource *commonModels.Engine, bucket, key string) (io.ReadCloser, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is nil")
	}
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return nil, err
	}
	contentReader, ok := enginePlugin.(plugin.ContentReadableProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}
	return contentReader.OpenContent(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), plugin.ObjectItemPath(resource.ID, bucket, key), plugin.ReadOptions{})
}

// ObjectStorageCatalogScanService 对象存储 catalog 扫描服务。
// 职责：按插件 catalog model 扫描 bucket/prefix/object 层级。
type ObjectStorageCatalogScanService struct {
	db                *gorm.DB
	log               *slog.Logger
	repo              *metaRepo.ScanRepository     // 数据访问层
	metadataExtractor *extractor.MetadataExtractor // 元数据提取器
	indexer           *IndexerService              // 索引服务
	inlineExtractor   *extractor.InlineObjectMetadataExtractor
}

// NewObjectStorageCatalogScanService 创建对象存储 catalog 扫描服务。
func NewObjectStorageCatalogScanService(
	db *gorm.DB,
	log *slog.Logger,
	repo *metaRepo.ScanRepository,
	metadataExtractor *extractor.MetadataExtractor,
	indexer *IndexerService,
) *ObjectStorageCatalogScanService {
	service := &ObjectStorageCatalogScanService{
		db:                db,
		log:               log,
		repo:              repo,
		metadataExtractor: metadataExtractor,
		indexer:           indexer,
	}
	service.inlineExtractor = extractor.NewInlineObjectMetadataExtractor(log)
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
) (int, int, error) {
	metaenrich.RegisterItemResolvers()

	resourceID := resource.ID

	// 标准化 scanDepth
	scanDepth = scanDepthOrDefault(scanDepth, "deep")

	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return 0, 0, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}
	itemTerm := catalogItemTermForPlugin(p, plugin.CatalogTermObject)

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
		return 0, 0, err
	}

	buckets, objects, err := s.scanObjectCatalogPaths(resource, tenantID, resourceID, catalogProvider, paths, scanDepth, force, reporter, itemTerm)
	if err != nil {
		return 0, 0, err
	}

	return buckets, objects, nil
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
) (int, int, error) {
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
	total := len(paths)
	completed := 0

	isDeepScan := strings.EqualFold(scanDepth, "deep")

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

		var objects []plugin.CatalogNode
		if target.Object != "" {
			objects, err = readObjectCatalogItem(context.Background(), resource, catalogProvider, bucketName, target.Object)
		} else {
			objects, err = listObjectCatalogItems(context.Background(), resource, catalogProvider, bucketName, prefix, isDeepScan)
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

		resources := objectCatalogNodesToStorageResources(context.Background(), objects, bucketName, isDeepScan, resource, s.inlineExtractor)

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

		fullBucket := prefix == "" && target.Object == ""
		if fullBucket {
			if !processedBuckets[bucketName] {
				if err := s.repo.ResetNodeState(bucketNode, "running"); err != nil {
					return totalBuckets, totalObjects, err
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
		objectCount, err := s.persistObjectResources(resource, tenantID, engineID, bucketNode, resources, nodeStats, fullBucket, scanDepth, force, scanPathPrefix, scannedFingerprints, itemTerm)
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
			deletedItems, err := s.repo.SoftDeleteObjectCatalogItemsMissingFingerprints(tenantID, engineID, bucketName, scannedFingerprints)
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

	return totalBuckets, totalObjects, nil
}

// persistObjectResources 持久化对象 catalog item 到数据库
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
//   - resources: 对象 catalog item 资源列表
//   - stats: 节点统计聚合map
//   - includeBucketAggregate: 是否包含bucket级别的聚合
//   - scanDepth: 扫描深度
//   - scanPathPrefix: 扫描路径前缀
//   - scannedFingerprints: 已扫描对象的指纹集合
//
// 返回：(持久化对象数量, error)
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
) (int, error) {
	objects := 0
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	readableProvider, _ := enginePlugin.(plugin.ContentReadableProvider)
	if strings.EqualFold(scanDepth, "deep") && readableProvider != nil {
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
			return objects, err
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
		return objects, err
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
					return objects, err
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
			return objects, err
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
			// 深度扫描：提取详细元数据（用于文件预览/搜索）
			enhancedAttrs = s.metadataExtractor.ExtractEnhancedMetadataWithCache(engineID, catalogResource, itemPlan.Attributes, catalogResource.Path)
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

		sizeVal := catalogResource.SizeBytes

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

		if strings.EqualFold(scanDepth, "deep") {
			if err := enrichObjectCatalogSingleResourceAttributes(context.Background(), enhancedAttrs, readableProvider, connInfo, engineID, catalogResource, itemPlan.DataItem, true); err != nil {
				s.log.Warn("提取对象 single 资源信息失败，保留基础属性", "bucket", catalogResource.RootName, "path", catalogResource.Path, "error", err)
			}
		} else {
			metaitem.ApplyContainerSummary(enhancedAttrs, itemPlan.DataItem)
		}
		extractedText := ""
		if strings.EqualFold(scanDepth, "deep") {
			extractedText = extractObjectCatalogDocumentText(context.Background(), enhancedAttrs, readableProvider, connInfo, engineID, catalogResource, itemPlan.DataItem)
		}

		rowCount := itemRowCountFromAttributes(enhancedAttrs)
		item, err := s.repo.UpsertItemWithDepth(tenantID, engineID, currentParent, itemPlan.ItemType, itemPlan.ItemName, itemPlan.FullName, enhancedAttrs, rowCount, &sizeVal, catalogResource.LastModified, scanDepth)
		if err != nil {
			return objects, err
		}

		// 只在deep扫描时索引（basic扫描的数据不完整）
		if scanDepth == "deep" {
			s.indexer.IndexObjectAsset(resource, tenantID, engineID, catalogResource, trimmed, itemPlan.FullName, item, extractedText)
		}

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
	return objects, nil
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
		rowCount := itemRowCountFromAttributes(itemPlan.Attributes)
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
	formatName = strings.ToLower(strings.TrimSpace(formatName))
	if formatName == "" || formatName == string(format.FormatUnknown) {
		return true
	}
	_, ok := format.GetFormatDescriptor(format.FormatType(formatName))
	return !ok
}

func itemRowCountFromAttributes(attrs map[string]interface{}) *int64 {
	rowCount := commonJSON.Int64(attrs, "type_info.table", "row_count")
	if rowCount <= 0 {
		return nil
	}
	return &rowCount
}

func extractObjectCatalogDocumentText(
	ctx context.Context,
	attrs models.JSONMap,
	readableProvider plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	catalogResource metacatalog.StorageResource,
	item *metaitem.DetectedItem,
) string {
	if attrs == nil || readableProvider == nil || item == nil || item.DataType != datatype.DataTypeDocument {
		return ""
	}
	formatName := strings.TrimSpace(item.Format)
	if formatName == "" {
		formatName = commonJSON.String(attrs, "item", "format")
	}
	if formatName == "" {
		return ""
	}
	reader, err := format.GetDocumentTextReader(format.FormatType(strings.ToLower(formatName)))
	if err != nil {
		return ""
	}
	rc, err := readableProvider.OpenContent(ctx, connInfo, catalogResource.CatalogPath, plugin.ReadOptions{})
	if err != nil {
		metaattr.SetExtraction(attrs, "text_extracted", false)
		metaattr.SetExtraction(attrs, "extractor_available", true)
		return ""
	}
	defer rc.Close()

	limit := int64(metatext.DocumentContentRuneLimit)
	text, truncated, err := reader.ReadDocumentText(ctx, rc, limit, nil)
	if err != nil {
		metaattr.SetExtraction(attrs, "text_extracted", false)
		metaattr.SetExtraction(attrs, "extractor_available", true)
		return ""
	}
	preview := metatext.PreviewText(text, metatext.DocumentPreviewRuneLimit)
	metaattr.SetExtraction(attrs, "extractor_available", true)
	metaattr.SetExtraction(attrs, "text_extracted", true)
	metaattr.SetExtraction(attrs, "extractor", "common_format:"+strings.ToLower(formatName))
	metaattr.SetExtraction(attrs, "plain_text_preview", preview)
	metaattr.SetExtraction(attrs, "text_truncated", truncated)
	metaattr.SetExtraction(attrs, "index_ref", "meilisearch:assets:"+itemFingerprintForExtraction(engineID, catalogResource))
	metaattr.MergeAttributeMaps(attrs, metaattr.DocumentInfoAttributes(&datatype.DocumentInfo{TextExtracted: true}))
	return text
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

func enrichObjectCatalogSingleResourceAttributes(
	ctx context.Context,
	attrs models.JSONMap,
	readableProvider plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	catalogResource metacatalog.StorageResource,
	item *metaitem.DetectedItem,
	includeContentIndex bool,
) error {
	if readableProvider == nil || item == nil {
		return nil
	}
	physicalPath := catalogResource.FullPath
	_, _, err := metaenrich.EnrichResourceAttributes(ctx, attrs, metaenrich.ResourceAttributesInput{
		ContentReader:       readableProvider,
		ConnInfo:            connInfo,
		EngineID:            engineID,
		Item:                item,
		PhysicalPath:        physicalPath,
		SizeBytes:           catalogResource.SizeBytes,
		IncludeContentIndex: includeContentIndex,
		CatalogPathFor: func(string) plugin.CatalogPath {
			return catalogResource.CatalogPath
		},
	})
	if err != nil {
		return err
	}
	metaattr.SetStorage(attrs, "bucket", catalogResource.RootName)
	dir, name := commonModels.SplitObjectPath(catalogResource.Path)
	metaattr.SetStorage(attrs, "path", dir)
	metaattr.SetStorage(attrs, "name", name)
	metaattr.SetStorage(attrs, "physical_path", physicalPath)
	if catalogResource.LastModified != nil {
		metaattr.SetStorage(attrs, "last_modified_at", catalogResource.LastModified)
	}
	return nil
}
