package service

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/mvt"
	"gorm.io/gorm"
)

// PreprocessService 预处理服务（协调器）
// 负责协调不同类型的预处理任务
type PreprocessService struct {
	db              *gorm.DB
	resourceService *ResourceService
	mvtPreprocessor *mvt.PreprocessService
	mvtUpdater      *mvt.IncrementalUpdater
}

// NewPreprocessService 创建预处理服务
func NewPreprocessService(db *gorm.DB, resourceService *ResourceService) (*PreprocessService, error) {
	// 创建 MVT 相关服务
	tileGen := mvt.NewTileGenerator(resourceService)

	// 从环境变量读取 MinIO 配置
	minioConfig := mvt.MinIOConfig{
		Endpoint:  os.Getenv("BUSINESS_MINIO_ENDPOINT"),
		AccessKey: os.Getenv("BUSINESS_MINIO_ACCESS_KEY"),
		SecretKey: os.Getenv("BUSINESS_MINIO_SECRET_KEY"),
		UseSSL:    os.Getenv("BUSINESS_MINIO_USE_SSL") == "true",
		Bucket:    os.Getenv("MVT_CACHE_BUCKET"),
	}

	// 设置默认值
	if minioConfig.Endpoint == "" {
		minioConfig.Endpoint = "localhost:9002"
	}
	if minioConfig.AccessKey == "" {
		minioConfig.AccessKey = "minioadmin"
	}
	if minioConfig.SecretKey == "" {
		minioConfig.SecretKey = "minioadmin"
	}
	if minioConfig.Bucket == "" {
		minioConfig.Bucket = "mvt-tiles"
	}

	cache, err := mvt.NewCacheService(minioConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache service: %w", err)
	}

	mvtPreprocessor := mvt.NewPreprocessService(tileGen, cache)
	mvtUpdater := mvt.NewIncrementalUpdater(tileGen, cache, mvtPreprocessor, resourceService)

	return &PreprocessService{
		db:              db,
		resourceService: resourceService,
		mvtPreprocessor: mvtPreprocessor,
		mvtUpdater:      mvtUpdater,
	}, nil
}

// ExecutePreprocess 执行预处理任务（供 Worker 调用）
func (s *PreprocessService) ExecutePreprocess(ctx context.Context, itemID, tenantID uint, preprocessType string) error {
	logger.L().Info("开始预处理任务",
		"item_id", itemID,
		"tenant_id", tenantID,
		"type", preprocessType)

	// 1. 查询 meta_item
	var item models.MetaItem
	if err := s.db.Where("id = ? AND tenant_id = ?", itemID, tenantID).First(&item).Error; err != nil {
		return fmt.Errorf("meta_item not found: %w", err)
	}

	// 2. 检查是否为空间表
	spatialMeta, ok := item.Attributes["spatial_metadata"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("item %d is not a spatial table", itemID)
	}

	// 3. 获取资源配置
	resource, err := s.resourceService.GetResourceByID(item.ResID, tenantID, "")
	if err != nil {
		return fmt.Errorf("failed to get resource: %w", err)
	}

	// 4. 检查预处理配置
	if resource.ScanConfig == nil ||
		resource.ScanConfig.Preprocessing == nil ||
		!resource.ScanConfig.Preprocessing.Enabled {
		return fmt.Errorf("preprocessing not enabled for resource %d", resource.ID)
	}

	preprocessing := resource.ScanConfig.Preprocessing

	// 5. 根据类型分发
	switch preprocessType {
	case "mvt_tiles":
		return s.executeMVTPreprocess(ctx, &item, tenantID, resource, preprocessing, spatialMeta)
	case "vector_embedding":
		// TODO: 实现向量嵌入预处理
		return fmt.Errorf("vector_embedding not implemented yet")
	default:
		return fmt.Errorf("unknown preprocess type: %s", preprocessType)
	}
}

// executeMVTPreprocess 执行 MVT 瓦片预处理
func (s *PreprocessService) executeMVTPreprocess(
	ctx context.Context,
	item *models.MetaItem,
	tenantID uint,
	resource *commonModels.Resource,
	preprocessing *commonModels.PreprocessingConfig,
	spatialMeta map[string]interface{},
) error {
	mvtConfig := preprocessing.MVTConfig
	if mvtConfig == nil {
		// 使用默认配置
		mvtConfig = &commonModels.MVTPreprocessConfig{
			MaxZoom:          18,
			Concurrency:      10,
			StopThresholdSec: 3.0,
			StopThresholdKB:  50.0,
		}
	}

	// 转换为 mvt.PreprocessConfig
	config := mvt.PreprocessConfig{
		MaxZoom:          mvtConfig.MaxZoom,
		Concurrency:      mvtConfig.Concurrency,
		StopThresholdSec: mvtConfig.StopThresholdSec,
		StopThresholdKB:  mvtConfig.StopThresholdKB,
	}

	// 检查是否需要增量更新
	preprocessMeta, ok := item.Attributes["preprocess_metadata"].(map[string]interface{})
	if ok {
		// 已有预处理元数据，尝试增量更新
		lastPreprocessedAt, _ := preprocessMeta["last_preprocessed_at"].(string)
		if lastPreprocessedAt != "" {
			logger.L().Info("检测到已有预处理数据，尝试增量更新",
				"item_id", item.ID,
				"last_preprocessed_at", lastPreprocessedAt)

			// 检查数据是否变更
			var oldStats *mvt.TableStats
			if statsMap, ok := preprocessMeta["table_stats"].(map[string]interface{}); ok {
				oldStats = &mvt.TableStats{
					TotalChanges: int64(statsMap["total_changes"].(float64)),
					LiveTuples:   int64(statsMap["live_tuples"].(float64)),
					DeadTuples:   int64(statsMap["dead_tuples"].(float64)),
				}
			}

			changed, newStats, err := s.mvtUpdater.DetectChanges(ctx, item, tenantID, oldStats)
			if err != nil {
				logger.L().Warn("变更检测失败，降级为全量预处理",
					"item_id", item.ID,
					"error", err)
			} else if !changed {
				logger.L().Info("数据未变更，跳过预处理",
					"item_id", item.ID)
				return nil
			} else {
				logger.L().Info("检测到数据变更，执行增量更新",
					"item_id", item.ID,
					"old_total_changes", oldStats.TotalChanges,
					"new_total_changes", newStats.TotalChanges)

				// TODO: 实现增量更新逻辑（基于 updated_at 字段）
				// 暂时降级为全量重新生成
				logger.L().Info("增量更新暂未实现，执行全量重新生成",
					"item_id", item.ID)
			}
		}
	}

	// 执行全量预处理
	metadata, err := s.mvtPreprocessor.StartPreprocess(ctx, item, tenantID, config)
	if err != nil {
		return fmt.Errorf("mvt preprocessing failed: %w", err)
	}

	// 6. 更新 meta_item 的 preprocess_metadata
	item.Attributes["preprocess_metadata"] = map[string]interface{}{
		"type":                  "mvt_tiles",
		"last_preprocessed_at":  metadata.CreatedAt,
		"completed_at":          metadata.CreatedAt.Add(time.Duration(metadata.GenerationSec * float64(time.Second))),
		"total_tiles":           metadata.TotalTiles,
		"generated_tiles":       metadata.TotalTiles,
		"empty_tiles":           0, // 从 metadata.ZoomLevels 中计算
		"max_zoom":              metadata.MaxZoomGenerated,
		"stop_reason":           metadata.StopReason,
		"duration_ms":           metadata.GenerationSec * 1000,
		"avg_tile_size_kb":      float64(metadata.TotalSizeBytes) / float64(metadata.TotalTiles*1024),
		"storage_size_mb":       float64(metadata.TotalSizeBytes) / (1024 * 1024),
		"fingerprint":           item.Fingerprint,
		"table_stats":           metadata, // 保存完整的元数据
		"preprocessing_config":  mvtConfig,
	}

	if err := s.db.Model(&models.MetaItem{}).
		Where("id = ?", item.ID).
		Update("attributes", item.Attributes).Error; err != nil {
		logger.L().Warn("更新预处理元数据失败", "item_id", item.ID, "error", err)
		// 不返回错误，预处理已成功
	}

	logger.L().Info("预处理任务完成",
		"item_id", item.ID,
		"type", "mvt_tiles",
		"total_tiles", metadata.TotalTiles,
		"duration_sec", metadata.GenerationSec)

	return nil
}
