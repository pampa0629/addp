package service

import (
	"context"
	"fmt"
	"log/slog"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/extractor"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/objectstore"
	metaRepo "github.com/addp/meta/internal/repository"
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
		buckets, err := s.listBuckets(resource, catalogProvider)
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
	nodeStats := make(map[uint]*nodeAggregate)

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

		objects, err := s.listObjects(resource, catalogProvider, bucketName, prefix, isDeepScan)
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

		// 转换为 format.ObjectMetadata 格式
		metas := s.convertToObjectMetadata(objects, bucketName, prefix, isDeepScan, engineID, resource)

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
				ensureNodeAggregate(nodeStats, bucketNode)
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
			var existingItems []models.MetaItem
			bucketNode := bucketNodes[bucketName]
			if bucketNode == nil {
				continue
			}

			if err := s.db.Where("tenant_id = ? AND engine_id = ? AND item_type = ?",
				tenantID, engineID, "object").
				Where("attributes->'storage'->>'bucket' = ?", bucketName).
				Find(&existingItems).Error; err != nil {
				s.log.Warn("查询已存在对象元数据失败",
					"bucket", bucketName,
					"error", err,
				)
				continue
			}

			for _, item := range existingItems {
				if !scannedFingerprints[item.Fingerprint] {
					s.log.Info("对象已不存在，标记删除",
						"bucket", bucketName,
						"fingerprint", item.Fingerprint,
						"name", item.Name,
					)
					if err := s.db.Delete(&item).Error; err != nil {
						s.log.Warn("软删除对象元数据失败",
							"bucket", bucketName,
							"fingerprint", item.Fingerprint,
							"error", err,
						)
					}
				}
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

		if err := s.repo.FinalizeNodeState(bucketNode, "completed", agg.itemCount, agg.totalSize, ""); err != nil {
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
		if agg.node.NodeType == "bucket" {
			continue
		}

		// 更新子目录节点的统计信息和扫描状态
		// 说明：扫描 bucket 时会遍历所有子目录，所以子目录的扫描状态也应该是"completed"
		now := time.Now()
		if err := s.db.Model(agg.node).Updates(map[string]interface{}{
			"item_count":       agg.itemCount,
			"total_size_bytes": agg.totalSize,
			"scan_status":      "completed",
			"scanned_at":       now,
		}).Error; err != nil {
			s.log.Warn("更新子目录节点统计信息失败",
				"node_id", nodeID,
				"node_name", agg.node.Name,
				"error", err,
			)
		} else {
			s.log.Debug("成功更新子目录节点统计",
				"node_id", nodeID,
				"node_name", agg.node.Name,
				"item_count", agg.itemCount,
				"total_size", agg.totalSize,
			)
		}
	}

	s.log.Info("对象存储路径扫描完成",
		"buckets", totalBuckets,
		"objects", totalObjects,
	)

	return totalBuckets, totalObjects, nil
}

func (s *ObjectStorageScanService) listBuckets(resource *commonModels.Engine, catalogProvider plugin.CatalogProvider) ([]plugin.BucketInfo, error) {
	nodes, err := catalogProvider.ListChildren(context.Background(), plugin.ConnectionInfo(resource.ConnectionInfo), plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: resource.ID,
	}, plugin.ListOptions{})
	if err != nil {
		return nil, err
	}
	buckets := make([]plugin.BucketInfo, 0, len(nodes))
	for _, node := range nodes {
		if node.Kind == plugin.CatalogKindBucket {
			buckets = append(buckets, plugin.BucketInfo{Name: node.Name})
		}
	}
	return buckets, nil
}

func (s *ObjectStorageScanService) listObjects(resource *commonModels.Engine, catalogProvider plugin.CatalogProvider, bucketName, prefix string, recursive bool) ([]plugin.ObjectInfo, error) {
	nodes, err := catalogProvider.ListChildren(context.Background(), plugin.ConnectionInfo(resource.ConnectionInfo), objectCatalogPath(resource.ID, bucketName, prefix), plugin.ListOptions{Recursive: recursive})
	if err != nil {
		return nil, err
	}
	objects := make([]plugin.ObjectInfo, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsItem {
			continue
		}
		key := strings.TrimPrefix(node.Path.StringPath(), bucketName+"/")
		if raw := commonJSON.String(node.Attributes, "storage", "path"); raw != "" {
			_, parsedKey := metapath.SplitObjectPath(raw)
			key = parsedKey
		}
		size, _ := int64Stat(node.Stats, "size_bytes")
		contentType := commonJSON.String(node.Attributes, "storage", "content_type")
		object := plugin.ObjectInfo{
			Bucket:      bucketName,
			Key:         key,
			Size:        size,
			ContentType: contentType,
		}
		if modifiedAt := commonJSON.TimePtr(node.Attributes, "storage", "modified_at"); modifiedAt != nil {
			object.LastModified = *modifiedAt
		}
		if etag := commonJSON.String(node.Attributes, "storage", "etag"); etag != "" {
			object.ETag = etag
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func objectCatalogPath(engineID uint, bucketName, prefix string) plugin.CatalogPath {
	path := plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
	}
	if bucketName == "" {
		return path
	}
	path.Segments = append(path.Segments, plugin.CatalogSegment{
		Term: plugin.CatalogTermBucket,
		Kind: plugin.CatalogKindBucket,
		Name: bucketName,
	})
	trimmed := strings.Trim(prefix, "/")
	if trimmed == "" {
		return path
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" {
			continue
		}
		path.Segments = append(path.Segments, plugin.CatalogSegment{
			Term: plugin.CatalogTermPrefix,
			Kind: plugin.CatalogKindPrefix,
			Name: part,
		})
	}
	return path
}

// convertToObjectMetadata 将 plugin.ObjectInfo 转换为 format.ObjectMetadata
func (s *ObjectStorageScanService) convertToObjectMetadata(
	objects []plugin.ObjectInfo,
	bucket, prefix string,
	isDeepScan bool,
	engineID uint,
	resource *commonModels.Engine,
) []format.ObjectMetadata {
	metas := make([]format.ObjectMetadata, 0, len(objects))

	for _, obj := range objects {
		// 相对路径：相对于 bucket 根目录的完整路径（不受扫描prefix影响）
		// 推断文件类型
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(obj.Key)), ".")

		// 构建 ObjectMetadata
		// 重要：Path 字段保存对象的完整Key（相对于bucket的路径）
		meta := format.ObjectMetadata{
			Bucket:       bucket,
			Path:         obj.Key, // ✅ 只保存相对路径，不包含 bucket
			NodeType:     "object",
			FileType:     ext,
			SizeBytes:    obj.Size,
			ObjectCount:  1,
			LastModified: &obj.LastModified,
		}

		// 深度扫描时提取元数据（仅针对图片，且文件大小<100MB）
		if isDeepScan && s.inlineExtractor.ShouldExtract(obj.ContentType, obj.Size) {
			s.log.Info("尝试提取图片元数据",
				"key", obj.Key,
				"content_type", obj.ContentType,
				"size", obj.Size)
			if extractedMeta := s.inlineExtractor.Extract(context.Background(), resource, bucket, obj.Key, obj.ContentType, obj.Size, obj.LastModified, obj.ETag); extractedMeta != nil {
				s.log.Info("成功提取图片元数据",
					"key", obj.Key,
					"width", extractedMeta.CustomAttrs["width"],
					"height", extractedMeta.CustomAttrs["height"])
				meta.ExtractedMetadata = extractedMeta
			} else {
				s.log.Warn("提取图片元数据失败", "key", obj.Key)
			}
		}

		metas = append(metas, meta)
	}

	return metas
}

// scanObjectStoragePaths 扫描对象存储路径（MinIO/S3等）- 保留原方法用于向后兼容
//
// 职责划分：
// 1. Bucket节点管理：创建/更新Bucket节点
// 2. 对象迭代：扫描指定路径下的所有对象
// 3. 元数据持久化：调用persistObjectMetas批量保存对象元数据
// 4. 去重处理：使用fingerprints避免重复扫描
// 5. 清理过期数据：软删除已移除的对象
// 6. 统计聚合：统计对象数量和总大小
func (s *ObjectStorageScanService) scanObjectStoragePaths(
	resource *commonModels.Engine,
	tenantID, engineID uint,
	objectScanner format.ObjectStorageScanner,
	paths []string,
	scanDepth string,
	reporter ScanProgressReporter,
) (int, int, error) {
	s.log.Info("🔍 进入scanObjectStoragePaths函数",
		"engine_id", engineID,
		"tenant_id", tenantID,
		"paths_count", len(paths),
		"scanDepth", scanDepth)

	bucketNodes := make(map[string]*models.MetaNode)
	processedBuckets := make(map[string]bool)
	nodeStats := make(map[uint]*nodeAggregate)

	// 记录本次扫描到的所有fingerprints，用于后续清理未扫描到的item
	scannedFingerprints := make(map[string]bool)

	// 如果scanner支持SetResourceID和SetScanDepth，设置扫描参数
	if s3Scanner, ok := objectScanner.(interface {
		SetResourceID(uint)
		SetScanDepth(string)
	}); ok {
		// 设置扫描深度
		s3Scanner.SetScanDepth(scanDepth)

		// 深度扫描时才启用详细元数据提取
		if strings.EqualFold(scanDepth, "deep") {
			s3Scanner.SetResourceID(engineID)
		} else {
			// 使用0关闭提取，避免浅度扫描额外读取对象内容
			s3Scanner.SetResourceID(0)
		}
	}

	totalBuckets := 0
	totalObjects := 0
	total := len(paths)
	completed := 0

	for _, rawPath := range paths {
		if reporter != nil {
			reporter.Message(fmt.Sprintf("扫描对象路径 %s", rawPath))
		}
		bucketName, relativePath := metapath.SplitObjectPath(rawPath)
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

		metas, err := objectScanner.ScanPath(rawPath)
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

		if !strings.EqualFold(scanDepth, "deep") {
			metas = metapath.FilterObjectMetasForDepth(metas, relativePath)
		}

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

		fullBucket := relativePath == ""
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
				ensureNodeAggregate(nodeStats, bucketNode)
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
		scanPathPrefix := relativePath
		s.log.Info("传递scanPathPrefix到persistObjectMetas",
			"rawPath", rawPath,
			"bucketName", bucketName,
			"relativePath", relativePath,
			"scanPathPrefix", scanPathPrefix,
			"fullBucket", fullBucket,
			"metasCount", len(metas),
			"scanDepth", scanDepth)
		objects, err := s.persistObjectMetas(resource, tenantID, engineID, bucketNode, metas, nodeStats, fullBucket, scanDepth, scanPathPrefix, scannedFingerprints)
		if err != nil {
			s.log.Error("对象存储元数据持久化失败",
				"engine_id", engineID,
				"tenant_id", tenantID,
				"bucket", bucketName,
				"error", err,
			)
			continue
		}
		totalObjects += objects
		completed++
		if reporter != nil {
			reporter.Advance(rawPath, completed, total, map[string]interface{}{"objects": objects})
		}
	}

	// 清理未在本次扫描中出现的旧item
	// 深度扫描时才执行清理，浅度扫描不应删除未扫描到的对象
	if strings.EqualFold(scanDepth, "deep") && len(scannedFingerprints) > 0 {
		// 查询本次扫描路径下的所有对象
		for bucketName := range processedBuckets {
			var existingItems []models.MetaItem
			bucketNode := bucketNodes[bucketName]
			if bucketNode == nil {
				continue
			}

			// 查询该 bucket 下的所有对象
			if err := s.db.Where("tenant_id = ? AND engine_id = ? AND item_type = ?",
				tenantID, engineID, "object").
				Where("attributes->'storage'->>'bucket' = ?", bucketName).
				Find(&existingItems).Error; err != nil {
				s.log.Warn("查询已存在对象元数据失败",
					"bucket", bucketName,
					"error", err,
				)
				continue
			}

			// 软删除未在本次扫描中出现的对象
			for _, item := range existingItems {
				if !scannedFingerprints[item.Fingerprint] {
					s.log.Info("对象已不存在，标记删除",
						"bucket", bucketName,
						"fingerprint", item.Fingerprint,
						"name", item.Name,
					)
					if err := s.db.Delete(&item).Error; err != nil {
						s.log.Warn("软删除对象元数据失败",
							"bucket", bucketName,
							"fingerprint", item.Fingerprint,
							"error", err,
						)
					}
				}
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

		if err := s.repo.FinalizeNodeState(bucketNode, "completed", agg.itemCount, agg.totalSize, ""); err != nil {
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
		if agg.node.NodeType == "bucket" {
			continue
		}

		// 更新子目录节点的统计信息和扫描状态
		// 说明：扫描 bucket 时会遍历所有子目录，所以子目录的扫描状态也应该是"completed"
		now := time.Now()
		if err := s.db.Model(agg.node).Updates(map[string]interface{}{
			"item_count":       agg.itemCount,
			"total_size_bytes": agg.totalSize,
			"scan_status":      "completed",
			"scanned_at":       now,
		}).Error; err != nil {
			s.log.Warn("更新子目录节点统计信息失败",
				"node_id", nodeID,
				"node_name", agg.node.Name,
				"error", err,
			)
		} else {
			s.log.Debug("成功更新子目录节点统计",
				"node_id", nodeID,
				"node_name", agg.node.Name,
				"item_count", agg.itemCount,
				"total_size", agg.totalSize,
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
	stats map[uint]*nodeAggregate,
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
		prefixSegments := strings.Split(metapath.SanitizeObjectPath(scanPathPrefix), "/")
		currentParent := bucketNode
		for idx, segment := range prefixSegments {
			fullName := metapath.ComposeNodeFullName(segment, currentParent, "/")
			pathSoFar := strings.Join(prefixSegments[:idx+1], "/")
			attrs := models.JSONMap{
				"bucket": bucketNode.Name,
				"path":   pathSoFar + "/", // ✅ 路径规范：目录路径必须以 / 结尾
			}
			childNode, err := s.repo.UpsertNode(tenantID, engineID, currentParent, "prefix", segment, &fullName, attrs)
			if err != nil {
				return objects, err
			}
			s.log.Info("创建/找到前缀节点",
				"segment", segment,
				"node_id", childNode.ID,
				"node_name", childNode.Name,
				"fullName", fullName)
			currentParent = childNode
			ensureNodeAggregate(stats, childNode)
		}
		basePrefixNode = currentParent
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
				ensureNodeAggregate(stats, bucketNode)
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

		// 重要：处理scanPathPrefix相关的路径，避免重复创建节点
		// Scanner在扫描子目录时会返回包含scanPathPrefix的路径
		var segmentsToProcess []string
		skipReason := ""
		if scanPathPrefix != "" && trimmed == scanPathPrefix {
			// 情况1：trimmed刚好等于scanPathPrefix（Scanner返回的prefix汇总对象）
			// 跳过segment处理，basePrefixNode已经创建了这个节点
			skipReason = "trimmed==scanPathPrefix"
			if meta.NodeType == "prefix" {
				ensureNodeAggregate(stats, basePrefixNode)
			}
			// 不处理segments
		} else if scanPathPrefix != "" && strings.HasPrefix(trimmed, scanPathPrefix+"/") {
			// 情况2：trimmed以"scanPathPrefix/"开头（Scanner的dirAgg中间目录）
			// 去掉scanPathPrefix前缀，只处理剩余部分
			skipReason = "trimmed以scanPathPrefix/开头，去掉前缀"
			remaining := strings.TrimPrefix(trimmed, scanPathPrefix+"/")
			if remaining != "" {
				segmentsToProcess = strings.Split(remaining, "/")
			}
		} else if trimmed != "" {
			// 情况3：正常路径，不包含scanPathPrefix前缀
			segmentsToProcess = strings.Split(trimmed, "/")
		} else if includeBucketAggregate {
			// 情况4：空路径，统计bucket
			skipReason = "空路径"
			ensureNodeAggregate(stats, bucketNode)
		}

		if skipReason != "" {
			s.log.Info("跳过或特殊处理",
				"reason", skipReason,
				"segmentsToProcess", segmentsToProcess)
		} else if len(segmentsToProcess) > 0 {
			s.log.Info("准备处理segments",
				"segmentsToProcess", segmentsToProcess)
		}

		// 处理segments（如果有）
		if len(segmentsToProcess) > 0 {
			for idx, segment := range segmentsToProcess {
				isLast := idx == len(segmentsToProcess)-1
				if meta.NodeType == "object" && isLast {
					break
				}
				fullName := metapath.ComposeNodeFullName(segment, currentParent, "/")
				attrs := models.JSONMap{
					"bucket": meta.Bucket,
					"path":   strings.Join(segmentsToProcess[:idx+1], "/") + "/", // ✅ 路径规范：目录路径必须以 / 结尾
				}
				childNode, err := s.repo.UpsertNode(tenantID, engineID, currentParent, "prefix", segment, &fullName, attrs)
				if err != nil {
					return objects, err
				}
				currentParent = childNode
				parentChain = append(parentChain, childNode)
				ensureNodeAggregate(stats, childNode)
			}
		}

		if meta.NodeType != "object" {
			continue
		}
		if compositeSkipPaths[meta.Path] {
			continue
		}

		objectName := pathpkg.Base(strings.Trim(meta.Path, "/"))
		if objectName == "" {
			objectName = trimmed
		}
		objectName = strings.Trim(objectName, "/")
		if objectName == "" {
			objectName = fmt.Sprintf("object_%d", meta.SizeBytes)
		}

		dataItem := metaitem.InferObjectStorageDataItem(meta, objectName)
		itemType := "object"
		itemName := objectName
		if inferredType := metaitem.ObjectStorageSingleFileItemType(dataItem); inferredType != "object" {
			itemType = inferredType
		}

		// 构建基础属性
		// 按照路径统一规范：拆分为 bucket、path（目录，以/结尾）、name（文件名）
		dir, name := commonModels.SplitObjectPath(meta.Path)
		attrs := models.JSONMap{
			"bucket":       meta.Bucket,
			"path":         dir,  // 目录路径（以 / 结尾）
			"name":         name, // 文件名（原始，含扩展名）
			"file_type":    meta.FileType,
			"object_count": meta.ObjectCount,
		}
		if meta.LastModified != nil {
			attrs["last_modified_at"] = meta.LastModified
		}
		mergeDataItemAttributes(attrs, dataItem)
		applyContainerSummary(attrs, dataItem)

		// 生成fingerprint - 两步计算方式
		fullName := commonModels.JoinObjectPath(meta.Bucket, dir, name)
		fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)
		if scannedFingerprints != nil {
			scannedFingerprints[fingerprint] = true
		}

		// 检查记录是否已存在（包括软删除的记录）
		var existingItem models.MetaItem
		itemExists := s.db.Unscoped().Where("fingerprint = ?", fingerprint).First(&existingItem).Error == nil

		// 增量更新逻辑：对比 LastModifiedAt 和 SizeBytes
		needsUpdate := !itemExists || // 新对象
			(existingItem.DataUpdatedAt != nil && meta.LastModified != nil && !existingItem.DataUpdatedAt.Equal(*meta.LastModified)) || // 修改时间变化
			(existingItem.SizeBytes != nil && *existingItem.SizeBytes != meta.SizeBytes) || // 大小变化
			(existingItem.DataUpdatedAt == nil && meta.LastModified != nil) || // 之前未记录修改时间
			(existingItem.SizeBytes == nil && meta.SizeBytes != 0) // 之前未记录大小

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
				agg := ensureNodeAggregate(stats, node)
				agg.itemCount++
				agg.totalSize += meta.SizeBytes
			}
			continue
		}

		// 根据扫描深度决定是否提取深度元数据
		var enhancedAttrs models.JSONMap
		if strings.EqualFold(scanDepth, "deep") {
			// 深度扫描：提取详细元数据（用于文件预览/搜索）
			// 传递meta.Path用于正确的fingerprint生成
			enhancedAttrs = s.metadataExtractor.ExtractEnhancedMetadataWithCache(engineID, meta, attrs, meta.Path)
		} else if itemExists {
			// 浅层扫描 + 记录已存在：保留原有attributes，只更新基础字段
			// 但仍需要更新node_id（文件可能移动到了不同的目录）
			enhancedAttrs = existingItem.Attributes // 使用已有的attributes
			for k, v := range attrs {
				enhancedAttrs[k] = v
			}
		} else {
			// 浅层扫描 + 新记录：使用基础属性
			enhancedAttrs = attrs
		}

		sizeVal := meta.SizeBytes

		// fullName 已在上方通过 JoinObjectPath(bucket, dir, name) 计算，直接复用
		// 不再基于 scanPathPrefix 重新拼接，避免前缀重复（如 bucket/prefix/prefix/file）

		s.log.Info("计算fullName和父节点",
			"meta.Bucket", meta.Bucket,
			"meta.Path", meta.Path,
			"trimmed", trimmed,
			"scanPathPrefix", scanPathPrefix,
			"calculated_fullName", fullName,
			"currentParent_id", currentParent.ID,
			"currentParent_name", currentParent.Name,
			"objectName", objectName)

		// 湖表属性（itemType 和 itemName 已在上方确定）
		if itemType == "lake_table" {
			// physical_path 保留原始路径（含扩展名），供 ReadFile 使用
			physicalPath := meta.Bucket + "/" + meta.Path
			metaattr.SetStorage(enhancedAttrs, "physical_path", physicalPath)
		}

		item, err := s.repo.UpsertItem(tenantID, engineID, currentParent, itemType, itemName, fullName, enhancedAttrs, nil, &sizeVal, meta.LastModified)
		if err != nil {
			return objects, err
		}

		// 只在deep扫描时索引（basic扫描的数据不完整）
		if scanDepth == "deep" {
			s.indexer.IndexObjectAsset(resource, tenantID, engineID, meta, trimmed, fullName, item)
		}

		objects++
		for idx, node := range parentChain {
			if !includeBucketAggregate && idx == 0 {
				continue
			}
			agg := ensureNodeAggregate(stats, node)
			agg.itemCount++
			agg.totalSize += meta.SizeBytes
		}
	}

	if includeBucketAggregate {
		ensureNodeAggregate(stats, bucketNode)
	}
	return objects, nil
}

func mergeDataItemAttributes(attrs models.JSONMap, item *dataitem.DetectedItem) {
	if item == nil {
		return
	}
	for k, v := range metaitem.BuildAttributes(item) {
		switch k {
		case "path", "size", "content_type":
			continue
		default:
			attrs[k] = v
		}
	}
}

func (s *ObjectStorageScanService) persistObjectStorageCompositeItems(
	tenantID, engineID uint,
	bucketNode, basePrefixNode *models.MetaNode,
	items []metaitem.ObjectStorageCompositeItem,
	stats map[uint]*nodeAggregate,
	includeBucketAggregate bool,
	scanPathPrefix string,
	scannedFingerprints map[string]bool,
) (int, error) {
	count := 0
	for _, composite := range items {
		if composite.Item == nil {
			continue
		}
		parentNode, err := s.ensureObjectPrefixNodes(tenantID, engineID, bucketNode, basePrefixNode, composite.Prefix, scanPathPrefix, stats)
		if err != nil {
			return count, err
		}

		itemName, objectPath := metaitem.ObjectStorageCompositeName(composite)
		parentPath := metaitem.ParentObjectPath(objectPath)
		fullName := commonModels.JoinObjectPath(composite.Bucket, parentPath, itemName)

		attrs := toJSONMap(metaitem.BuildAttributes(composite.Item))
		if len(composite.Item.Fields) > 0 {
			setSchemaFields(attrs, fieldAttributesFromFormat(composite.Item.Fields))
		}
		metaattr.SetStorage(attrs, "bucket", composite.Bucket)
		metaattr.SetStorage(attrs, "path", parentPath)
		metaattr.SetStorage(attrs, "name", itemName)
		metaattr.SetItem(attrs, "mode", metaitem.ObjectStorageCompositeMode(composite.Item))

		fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)
		if scannedFingerprints != nil {
			scannedFingerprints[fingerprint] = true
		}

		sizeVal := composite.Item.SizeBytes
		if _, err := s.repo.UpsertItem(tenantID, engineID, parentNode, composite.Item.ItemType, itemName, fullName, attrs, nil, &sizeVal, nil); err != nil {
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
			agg := ensureNodeAggregate(stats, node)
			agg.itemCount++
			agg.totalSize += composite.Item.SizeBytes
		}
	}
	return count, nil
}

func (s *ObjectStorageScanService) ensureObjectPrefixNodes(
	tenantID, engineID uint,
	bucketNode, basePrefixNode *models.MetaNode,
	prefix string,
	scanPathPrefix string,
	stats map[uint]*nodeAggregate,
) (*models.MetaNode, error) {
	parent := bucketNode
	if basePrefixNode != nil {
		parent = basePrefixNode
	}

	parentPrefix := metaitem.ParentObjectPath(prefix)
	relative := strings.Trim(parentPrefix, "/")
	if scanPathPrefix != "" && strings.HasPrefix(relative, strings.Trim(scanPathPrefix, "/")) {
		relative = strings.TrimPrefix(relative, strings.Trim(scanPathPrefix, "/"))
		relative = strings.Trim(relative, "/")
	}
	if relative == "" {
		return parent, nil
	}
	current := parent
	segments := strings.Split(relative, "/")
	for idx, segment := range segments {
		if segment == "" {
			continue
		}
		fullName := metapath.ComposeNodeFullName(segment, current, "/")
		pathSoFar := joinObjectPathParts(strings.Trim(scanPathPrefix, "/"), strings.Join(segments[:idx+1], "/"))
		attrs := models.JSONMap{
			"bucket": bucketNode.Name,
			"path":   pathSoFar + "/",
		}
		childNode, err := s.repo.UpsertNode(tenantID, engineID, current, "prefix", segment, &fullName, attrs)
		if err != nil {
			return nil, err
		}
		current = childNode
		ensureNodeAggregate(stats, childNode)
	}
	return current, nil
}

func joinObjectPathParts(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part == "" {
			continue
		}
		cleaned = append(cleaned, part)
	}
	return strings.Join(cleaned, "/")
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
