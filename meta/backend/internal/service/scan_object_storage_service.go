package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/gorm"
)

// ObjectStorageScanService 对象存储扫描服务
// 职责：扫描 MinIO、S3 等对象存储的 Bucket 和 Object
type ObjectStorageScanService struct {
	db                *gorm.DB
	log               *slog.Logger
	repo              *ScanRepository     // 数据访问层
	metadataExtractor *MetadataExtractor  // 元数据提取器
	indexer           *IndexerService     // 索引服务
	objectClients     map[uint]*minio.Client
	objectClientMu    sync.Mutex
}

// NewObjectStorageScanService 创建对象存储扫描服务
func NewObjectStorageScanService(
	db *gorm.DB,
	log *slog.Logger,
	repo *ScanRepository,
	metadataExtractor *MetadataExtractor,
	indexer *IndexerService,
) *ObjectStorageScanService {
	return &ObjectStorageScanService{
		db:                db,
		log:               log,
		repo:              repo,
		metadataExtractor: metadataExtractor,
		indexer:           indexer,
		objectClients:     make(map[uint]*minio.Client),
	}
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

	// 获取 ObjectStoragePlugin
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return 0, 0, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}

	objPlugin, ok := p.(plugin.ObjectStoragePlugin)
	if !ok {
		return 0, 0, fmt.Errorf("engine %s does not implement ObjectStoragePlugin", resource.EngineType)
	}

	// 确定扫描路径
	paths := objectPaths
	if len(paths) == 0 {
		paths = fallback
	}

	// 如果仍然没有路径，列出所有 buckets
	if len(paths) == 0 {
		buckets, err := objPlugin.ListBuckets(context.Background(), plugin.ConnectionInfo(resource.ConnectionInfo))
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

	buckets, objects, err := s.scanObjectStoragePathsWithPlugin(resource, tenantID, resourceID, objPlugin, paths, scanDepth, reporter)
	if err != nil {
		return 0, 0, err
	}

	return buckets, objects, nil
}

// ============================================================================
// 核心扫描方法
// ============================================================================

// scanObjectStoragePathsWithPlugin 使用 ObjectStoragePlugin 扫描对象存储路径
func (s *ObjectStorageScanService) scanObjectStoragePathsWithPlugin(
	resource *commonModels.Engine,
	tenantID, engineID uint,
	objPlugin plugin.ObjectStoragePlugin,
	paths []string,
	scanDepth string,
	reporter ScanProgressReporter,
) (int, int, error) {
	s.log.Info("🔍 进入scanObjectStoragePathsWithPlugin函数",
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
		bucketName, prefix := s.splitObjectPath(rawPath)
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

		// 直接使用 plugin 列出对象
		objects, err := objPlugin.ListObjects(
			context.Background(),
			plugin.ConnectionInfo(resource.ConnectionInfo),
			bucketName,
			prefix,
			isDeepScan, // 深度扫描时递归
		)
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
				Where("attributes->>'bucket' = ?", bucketName).
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
			Path:         obj.Key,        // ✅ 只保存相对路径，不包含 bucket
			NodeType:     "object",
			FileType:     ext,
			SizeBytes:    obj.Size,
			ObjectCount:  1,
			LastModified: &obj.LastModified,
		}

		// 深度扫描时提取元数据（仅针对图片，且文件大小<100MB）
		if isDeepScan && s.shouldExtractMetadata(obj.ContentType, obj.Size) {
			s.log.Info("尝试提取图片元数据",
				"key", obj.Key,
				"content_type", obj.ContentType,
				"size", obj.Size)
			if extractedMeta := s.extractObjectMetadataInline(context.Background(), resource, bucket, obj.Key, obj.ContentType, obj.Size, obj.LastModified, obj.ETag); extractedMeta != nil {
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

// shouldExtractMetadata 判断是否应该在扫描时提取元数据
func (s *ObjectStorageScanService) shouldExtractMetadata(contentType string, sizeBytes int64) bool {
	// 只对图片类型进行提取
	if !strings.HasPrefix(contentType, "image/") {
		s.log.Debug("跳过非图片类型", "content_type", contentType)
		return false
	}

	// 限制文件大小（100MB）
	const maxSizeForExtraction = 100 * 1024 * 1024
	if sizeBytes > maxSizeForExtraction {
		s.log.Debug("文件过大，跳过元数据提取", "size", sizeBytes)
		return false
	}

	return true
}

// extractObjectMetadataInline 内联提取对象元数据（在扫描时调用）
func (s *ObjectStorageScanService) extractObjectMetadataInline(
	ctx context.Context,
	resource *commonModels.Engine,
	bucket, key, contentType string,
	size int64,
	lastModified time.Time,
	etag string,
) *format.ExtractedMetadata {
	// 获取对象信息解析器
	s.log.Debug("正在获取对象信息解析器", "content_type", contentType, "key", key)
	parser, err := format.GetObjectInfoParser(contentType)
	if err != nil {
		s.log.Debug("获取解析器失败，尝试通配符匹配",
			"content_type", contentType,
			"error", err,
			"key", key)
		// 支持通配符匹配（如 "image/*"）
		if strings.HasPrefix(contentType, "image/") {
			s.log.Debug("尝试使用 image/* 通配符", "key", key)
			parser, err = format.GetObjectInfoParser("image/*")
			if err != nil {
				s.log.Warn("通配符解析器也失败",
					"content_type", contentType,
					"error", err,
					"key", key)
				return nil
			}
			s.log.Debug("成功获取 image/* 解析器", "key", key)
		} else {
			s.log.Debug("无可用的元数据解析器",
				"content_type", contentType,
				"key", key)
			return nil
		}
	} else {
		s.log.Debug("成功获取解析器", "content_type", contentType, "key", key)
	}

	// 获取或创建 MinIO 客户端
	s.objectClientMu.Lock()
	client, ok := s.objectClients[resource.ID]
	if !ok {
		// 创建新客户端
		var createErr error
		client, createErr = s.createMinIOClient(resource.ConnectionInfo)
		if createErr != nil {
			s.objectClientMu.Unlock()
			s.log.Warn("创建对象存储客户端失败",
				"engine_id", resource.ID,
				"error", createErr)
			return nil
		}
		s.objectClients[resource.ID] = client
	}
	s.objectClientMu.Unlock()

	// 只读取前 16KB 用于元数据提取（对于图片足够了）
	const headerSize = 16 * 1024
	opts := minio.GetObjectOptions{}
	if size > headerSize {
		// 使用 Range 请求只获取头部
		opts.SetRange(0, headerSize-1)
	}

	// 获取对象内容
	obj, err := client.GetObject(ctx, bucket, key, opts)
	if err != nil {
		s.log.Warn("获取对象内容失败",
			"bucket", bucket,
			"key", key,
			"error", err)
		return nil
	}
	defer obj.Close()

	// 构建基础信息
	basicInfo := format.ObjectBasicInfo{
		Key:         key,
		SizeBytes:   size,
		ContentType: contentType,
		ETag:        etag,
		ModifiedAt:  lastModified,
	}

	// 调用解析器提取元数据
	objectInfo, err := parser.ParseObjectInfo(ctx, obj, basicInfo)
	if err != nil {
		s.log.Debug("提取对象元数据失败",
			"bucket", bucket,
			"key", key,
			"error", err)
		return nil
	}

	// 转换为 ExtractedMetadata
	extractedMeta := &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{
			FileName:     filepath.Base(key),
			FileType:     contentType,
			Size:         size,
			ContentType:  contentType,
			LastModified: lastModified,
			ETag:         etag,
		},
		CustomAttrs: make(map[string]interface{}),
	}

	// 提取 ImageInfo
	if imageInfo := objectInfo.GetImageInfo(); imageInfo != nil {
		extractedMeta.CustomAttrs["width"] = imageInfo.Width
		extractedMeta.CustomAttrs["height"] = imageInfo.Height
		extractedMeta.CustomAttrs["format"] = imageInfo.Format
		extractedMeta.CustomAttrs["color_space"] = imageInfo.ColorSpace
		if imageInfo.BitDepth > 0 {
			extractedMeta.CustomAttrs["bit_depth"] = imageInfo.BitDepth
		}
		if imageInfo.HasAlpha {
			extractedMeta.CustomAttrs["has_alpha"] = true
		}
	}

	s.log.Debug("成功提取对象元数据",
		"bucket", bucket,
		"key", key,
		"attrs", extractedMeta.CustomAttrs)

	return extractedMeta
}

// createMinIOClient 创建 MinIO 客户端
func (s *ObjectStorageScanService) createMinIOClient(connInfo commonModels.ConnectionInfo) (*minio.Client, error) {
	endpoint := getStringFromConn(connInfo, "endpoint")
	accessKey := getStringFromConn(connInfo, "access_key")
	secretKey := getStringFromConn(connInfo, "secret_key")
	useSSL := getBoolFromConn(connInfo, "use_ssl")

	if endpoint == "" || accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("missing required fields: endpoint, access_key, secret_key")
	}

	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	}

	return minio.New(endpoint, opts)
}

// splitObjectPath 分割对象路径为 bucket 和 prefix
func (s *ObjectStorageScanService) splitObjectPath(path string) (bucket, prefix string) {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "/")

	parts := strings.SplitN(path, "/", 2)
	bucket = parts[0]
	if len(parts) > 1 {
		prefix = parts[1]
	}
	return
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
		bucketName, relativePath := splitObjectPath(rawPath)
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
			metas = filterObjectMetasForDepth(metas, relativePath)
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
				Where("attributes->>'bucket' = ?", bucketName).
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

	// 重要：在循环外部只处理一次scanPathPrefix，建立基础父节点
	// 例如：扫描 "addp/shapefile" 时，scanPathPrefix = "shapefile"
	// 需要先创建/查找 "shapefile" 节点作为所有对象的基础父节点
	basePrefixNode := bucketNode
	if scanPathPrefix != "" {
		s.log.Info("处理scanPathPrefix建立基础父节点",
			"scanPathPrefix", scanPathPrefix,
			"bucket", bucketNode.Name)
		prefixSegments := strings.Split(sanitizeObjectPath(scanPathPrefix), "/")
		currentParent := bucketNode
		for idx, segment := range prefixSegments {
			fullName := composeNodeFullName(segment, currentParent, "/")
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
		trimmed := sanitizeObjectPath(meta.Path)

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
				fullName := composeNodeFullName(segment, currentParent, "/")
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

		objectName := pathpkg.Base(strings.Trim(meta.Path, "/"))
		if objectName == "" {
			objectName = trimmed
		}
		objectName = strings.Trim(objectName, "/")
		if objectName == "" {
			objectName = fmt.Sprintf("object_%d", meta.SizeBytes)
		}

		// 提前判断是否为湖表格式，以便正确计算 fullName 和 fingerprint
		// 湖表的逻辑名称去掉扩展名，与 NFS 保持一致
		itemType := "object"
		itemName := objectName
		if isLakeTableFormat(meta.FileType) {
			itemType = "lake_table"
			itemName = logicalLakeTableName(objectName)
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

		// 生成fingerprint - 两步计算方式
		// 湖表使用逻辑名称（去掉扩展名）计算 fullName，与 NFS 保持一致
		logicalName := name
		if itemType == "lake_table" {
			logicalName = logicalLakeTableName(name)
		}
		fullName := commonModels.JoinObjectPath(meta.Bucket, dir, logicalName)
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
			enhancedAttrs["format"] = meta.FileType
			enhancedAttrs["mode"] = "file"
			// physical_path 保留原始路径（含扩展名），供 ReadFile 使用
			enhancedAttrs["physical_path"] = meta.Bucket + "/" + meta.Path
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
	// 获取对象存储客户端
	client, err := s.getObjectClient(engineID, tenantID)
	if err != nil {
		return nil, "", err
	}

	// 获取对象
	obj, err := client.GetObject(ctx, bucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("failed to get object: %w", err)
	}
	defer obj.Close()

	// 读取对象内容（限制大小）
	var content []byte
	if maxSize > 0 {
		content = make([]byte, maxSize)
		n, err := io.ReadFull(obj, content)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return nil, "", fmt.Errorf("failed to read object: %w", err)
		}
		content = content[:n]
	} else {
		content, err = io.ReadAll(obj)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read object: %w", err)
		}
	}

	// 推断 MIME 类型
	mimeType := detectMimeType(objectPath, content)

	return content, mimeType, nil
}

// getObjectClient 获取或创建 MinIO 客户端
func (s *ObjectStorageScanService) getObjectClient(engineID, tenantID uint) (*minio.Client, error) {
	s.objectClientMu.Lock()
	defer s.objectClientMu.Unlock()

	// 检查缓存
	if client, ok := s.objectClients[engineID]; ok {
		return client, nil
	}

	// 查询引擎配置
	var resource commonModels.Engine
	if err := s.db.Where("id = ? AND tenant_id = ?", engineID, tenantID).First(&resource).Error; err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 解析对象存储配置
	cfg, err := parseObjectStorageConfig(resource.ConnectionInfo)
	if err != nil {
		return nil, err
	}

	// 创建 MinIO 客户端
	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	}
	if cfg.Region != "" {
		opts.Region = cfg.Region
	}
	if cfg.PathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}

	client, err := minio.New(cfg.Endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	// 缓存客户端
	s.objectClients[engineID] = client
	return client, nil
}

// ============================================================================
// 辅助方法
// ============================================================================

func detectMimeType(path string, content []byte) string {
	// 基于扩展名推断
	ext := pathpkg.Ext(path)
	if ext != "" {
		// 这里可以使用 mime.TypeByExtension，但为了简单起见先返回扩展名
		return "application/octet-stream"
	}
	return "application/octet-stream"
}

// objectStorageConfig MinIO/S3 配置
type objectStorageConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
	PathStyle bool
}

// parseObjectStorageConfig 解析对象存储配置
func parseObjectStorageConfig(info commonModels.ConnectionInfo) (*objectStorageConfig, error) {
	cfg := &objectStorageConfig{}

	cfg.Endpoint = getStringFromConn(info, "endpoint")
	cfg.AccessKey = getStringFromConn(info, "access_key")
	cfg.SecretKey = getStringFromConn(info, "secret_key")
	cfg.Region = getStringFromConn(info, "region")
	cfg.UseSSL = getBoolFromConn(info, "use_ssl")
	cfg.PathStyle = getBoolFromConn(info, "path_style")

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("object storage endpoint is empty")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("object storage credentials missing")
	}

	return cfg, nil
}

// getStringFromConn 从连接配置中获取字符串值
func getStringFromConn(info commonModels.ConnectionInfo, key string) string {
	if raw, ok := info[key]; ok {
		switch v := raw.(type) {
		case string:
			return v
		case fmt.Stringer:
			return v.String()
		case float64:
			return fmt.Sprintf("%.0f", v)
		case int64:
			return fmt.Sprintf("%d", v)
		case int:
			return fmt.Sprintf("%d", v)
		case bool:
			if v {
				return "true"
			}
			return "false"
		}
	}
	return ""
}

// getBoolFromConn 从连接配置中获取布尔值
func getBoolFromConn(info commonModels.ConnectionInfo, key string) bool {
	if raw, ok := info[key]; ok {
		switch v := raw.(type) {
		case bool:
			return v
		case string:
			lower := strings.ToLower(strings.TrimSpace(v))
			return lower == "true" || lower == "1" || lower == "yes"
		case float64:
			return v != 0
		case int:
			return v != 0
		case int64:
			return v != 0
		}
	}
	return false
}

func isLakeTableFormat(fileType string) bool {
	switch strings.ToLower(strings.TrimSpace(fileType)) {
	case "parquet", "orc", "avro":
		return true
	default:
		return false
	}
}

func logicalLakeTableName(fileName string) string {
	ext := filepath.Ext(fileName)
	if ext == "" {
		return fileName
	}
	base := strings.TrimSuffix(fileName, ext)
	if strings.TrimSpace(base) == "" {
		return fileName
	}
	return base
}
