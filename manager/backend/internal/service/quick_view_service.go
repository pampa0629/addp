package service

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/logger"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/mvt"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/worker"
	"github.com/minio/minio-go/v7"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// QuickViewService 快显服务层
type QuickViewService struct {
	repo         *repository.QuickViewRepository
	taskQueue    *worker.TaskQueue
	systemClient *commonClient.SystemClient
	metaClient   *commonClient.MetaClient
	minioClient  *minio.Client
	minioBucket  string
	redisClient  *redis.Client
	cfg          *config.Config
}

// NewQuickViewService 创建快显服务
func NewQuickViewService(
	db *gorm.DB,
	taskQueue *worker.TaskQueue,
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
	minioClient *minio.Client,
	minioBucket string,
	redisClient *redis.Client,
	cfg *config.Config,
) *QuickViewService {
	return &QuickViewService{
		repo:         repository.NewQuickViewRepository(db),
		taskQueue:    taskQueue,
		systemClient: systemClient,
		metaClient:   metaClient,
		minioClient:  minioClient,
		minioBucket:  minioBucket,
		redisClient:  redisClient,
		cfg:          cfg,
	}
}

// TriggerQuickViewParams 触发快显参数
type TriggerQuickViewParams struct {
	TenantID           uint
	EngineID           uint
	SchemaName         string
	TableName          string
	MinZoom            *int                                // 必需，用户确认的最小缩放级别
	MaxZoom            int                                 // 必需，用户确认的最大缩放级别
	Concurrency        int                                 // 可选，默认从配置读取
	Priority           string                              // "critical", "default", "low"
	OptimizationConfig *commonModels.OptimizationConfig    // v2.0 优化配置
}

// TriggerQuickView 触发预缓存（第二步，必须先完成准备）
func (s *QuickViewService) TriggerQuickView(ctx context.Context, params TriggerQuickViewParams) error {
	// 1. 检查快显表记录是否存在
	exists, _ := s.repo.Exists(params.TenantID, params.EngineID, params.SchemaName, params.TableName)
	if !exists {
		return fmt.Errorf("quick view record not found, please run PrepareForCreateMVT first")
	}

	// 2. 检查准备是否完成
	prepCompleted, err := s.repo.IsPreparationCompleted(params.TenantID, params.EngineID, params.SchemaName, params.TableName)
	if err != nil {
		return fmt.Errorf("failed to check preparation status: %w", err)
	}
	if !prepCompleted {
		return fmt.Errorf("preparation not completed, please complete PrepareForCreateMVT first")
	}

	// 3. 检查是否已在生成中（并发控制）
	isGenerating, err := s.repo.IsGenerating(params.TenantID, params.EngineID, params.SchemaName, params.TableName)
	if err != nil {
		return fmt.Errorf("failed to check generating status: %w", err)
	}

	if isGenerating {
		return fmt.Errorf("quick view is already generating for this table")
	}

	// 4. 验证必需参数（前端必须提供用户确认的值）
	if params.MinZoom == nil {
		return fmt.Errorf("min_zoom is required (must be confirmed by user)")
	}
	if params.MaxZoom == 0 {
		return fmt.Errorf("max_zoom is required (must be confirmed by user)")
	}

	// 5. 从Meta获取空间元数据（GeomColumn、SRID、PrimaryKey 等）
	spatialMeta, err := s.GetSpatialMetadataFromMeta(ctx, params.TenantID, params.EngineID, params.SchemaName, params.TableName)
	if err != nil {
		return fmt.Errorf("failed to get spatial metadata: %w", err)
	}

	// 6. 计算fingerprint
	fingerprint := calculateFingerprint(params.EngineID, params.SchemaName, params.TableName)

	// 6. 设置默认并发数
	logger.L().Info("🔍 Service: 并发数配置检查",
		"table", fmt.Sprintf("%s.%s", params.SchemaName, params.TableName),
		"concurrency_from_api", params.Concurrency,
		"concurrency_from_config", s.cfg.PreCache.Concurrency)

	if params.Concurrency == 0 {
		params.Concurrency = s.cfg.PreCache.Concurrency
		logger.L().Info("✅ Service: 使用配置中的默认并发数",
			"table", fmt.Sprintf("%s.%s", params.SchemaName, params.TableName),
			"final_concurrency", params.Concurrency)
	}

	// 7. 初始化优化配置（如果用户未提供）
	if params.OptimizationConfig == nil {
		params.OptimizationConfig = &commonModels.OptimizationConfig{}
		*params.OptimizationConfig = commonModels.DefaultOptimizationConfig()
		logger.L().Info("✅ Service: 使用默认优化配置",
			"table", fmt.Sprintf("%s.%s", params.SchemaName, params.TableName))
	}

	// 8. 获取快显表记录
	qv, err := s.repo.GetByTable(params.TenantID, params.EngineID, params.SchemaName, params.TableName)
	if err != nil {
		return fmt.Errorf("failed to get quick view: %w", err)
	}

	// 9. 更新记录状态为生成中（只更新必要字段，保护 preparation_status 和 extent）
	now := time.Now()
	updates := map[string]interface{}{
		"status":              "generating",
		"started_at":          now,
		"min_zoom":            params.MinZoom,
		"max_zoom":            params.MaxZoom,
		"optimization_config": params.OptimizationConfig,
	}

	if err := s.repo.GetDB().Model(&models.QuickView{}).Where("id = ?", qv.ID).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update quick view: %w", err)
	}

	// 10. 清除之前的取消标志（如果存在）
	if s.redisClient != nil && fingerprint != "" {
		tracker := mvt.NewProgressTracker(s.redisClient, fingerprint)
		if err := tracker.ClearCancelFlag(ctx); err != nil {
			logger.L().Warn("Failed to clear cancel flag during trigger",
				"error", err,
				"fingerprint", fingerprint)
		} else {
			logger.L().Info("已清除之前的取消标志（重新触发预缓存）",
				"fingerprint", fingerprint)
		}
	}

	// 11. 入队预缓存任务
	payload := worker.QuickViewTaskPayload{
		TenantID:           params.TenantID,
		EngineID:           params.EngineID,
		SchemaName:         params.SchemaName,
		TableName:          params.TableName,
		GeomColumn:         spatialMeta.GeomColumn,
		SRID:               spatialMeta.SRID,
		PrimaryKey:         spatialMeta.PrimaryKey,
		Extent:             spatialMeta.Extent,
		MinZoom:            *params.MinZoom,
		MaxZoom:            params.MaxZoom,
		Concurrency:        params.Concurrency,
		Fingerprint:        fingerprint,
		OptimizationConfig: params.OptimizationConfig,
	}

	logger.L().Info("📤 Service: 准备入队预缓存任务",
		"table", fmt.Sprintf("%s.%s", params.SchemaName, params.TableName),
		"concurrency", payload.Concurrency,
		"priority", params.Priority)

	if params.Priority != "" {
		err = s.taskQueue.EnqueueQuickViewTaskWithPriority(ctx, payload, params.Priority)
	} else {
		err = s.taskQueue.EnqueueQuickViewTask(ctx, payload)
	}

	if err != nil {
		// 更新状态为失败
		s.repo.UpdateStatusOnly(qv.ID, "failed", err.Error())
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	logger.L().Info("✅ Service: 预缓存任务已入队",
		"engine_id", params.EngineID,
		"table", fmt.Sprintf("%s.%s", params.SchemaName, params.TableName),
		"min_zoom", *params.MinZoom,
		"max_zoom", params.MaxZoom,
		"concurrency", payload.Concurrency)

	return nil
}

// SpatialMetadataResult 空间元数据结果
type SpatialMetadataResult struct {
	GeomColumn  string
	SRID        int   // 表的原始坐标系
	ExtentSRID  int   // extent 的坐标系
	PrimaryKey  string
	Extent      []float64
	RecordCount int64 // 表记录数
}

// GetSpatialMetadataFromMeta 从Meta模块获取空间元数据（公开方法，供 API Handler 调用）
func (s *QuickViewService) GetSpatialMetadataFromMeta(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) (*SpatialMetadataResult, error) {
	if s.metaClient == nil {
		return nil, fmt.Errorf("meta client not initialized, cannot query spatial metadata")
	}

	// 设置租户 ID（用于服务间调用时的租户隔离）
	s.metaClient.SetTenantID(&tenantID)

	// 通过 Meta API 查询空间元数据
	spatialMeta, err := s.metaClient.GetTableSpatialMetadata(engineID, schema, table)
	if err != nil {
		return nil, fmt.Errorf("failed to get spatial metadata from Meta API: %w", err)
	}

	// 转换为 QuickViewService 内部的数据结构
	return &SpatialMetadataResult{
		GeomColumn:  spatialMeta.GeometryColumn,
		SRID:        spatialMeta.SRID,
		ExtentSRID:  spatialMeta.ExtentSRID,
		Extent:      spatialMeta.Extent,
		PrimaryKey:  spatialMeta.PrimaryKey,
		RecordCount: spatialMeta.RowCount, // 从 Meta API 获取的表记录数
	}, nil
}

// GetStatus 获取快显状态
func (s *QuickViewService) GetStatus(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) (*models.QuickView, error) {
	qv, err := s.repo.GetByTable(tenantID, engineID, schema, table)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 返回默认状态
			fingerprint := calculateFingerprint(engineID, schema, table)
			return &models.QuickView{
				TenantID:    tenantID,
				EngineID:  engineID,
				SchemaName:  schema,
				Table:       table,
				Status:      "none",
				Fingerprint: fingerprint,
			}, nil
		}
		return nil, err
	}

	return qv, nil
}

// ListAll 列出所有快显任务
func (s *QuickViewService) ListAll(
	ctx context.Context,
	tenantID uint,
	params repository.ListParams,
) ([]models.QuickView, int64, error) {
	return s.repo.ListAll(tenantID, params)
}

// GetStatistics 获取统计信息
func (s *QuickViewService) GetStatistics(
	ctx context.Context,
	tenantID uint,
) (*repository.Statistics, error) {
	return s.repo.GetStatistics(tenantID)
}

// ClearQuickView 清除快显缓存
func (s *QuickViewService) ClearQuickView(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) error {
	// 1. 获取快显记录
	qv, err := s.repo.GetByTable(tenantID, engineID, schema, table)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil // 记录不存在，无需清除
		}
		return fmt.Errorf("failed to get quick view: %w", err)
	}

	// 2. 删除 MinIO 中的瓦片
	if s.minioClient != nil && qv.Fingerprint != "" {
		prefix := fmt.Sprintf("mvt-tiles/%s/", qv.Fingerprint)

		// 列出所有对象
		objectsCh := s.minioClient.ListObjects(ctx, s.minioBucket, minio.ListObjectsOptions{
			Prefix:    prefix,
			Recursive: true,
		})

		// 删除所有对象
		deletedCount := 0
		for object := range objectsCh {
			if object.Err != nil {
				logger.L().Warn("Error listing object for deletion", "error", object.Err, "prefix", prefix)
				continue
			}

			if err := s.minioClient.RemoveObject(ctx, s.minioBucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
				logger.L().Warn("Failed to delete object", "error", err, "key", object.Key)
				continue
			}

			deletedCount++
		}

		logger.L().Info("Deleted tiles from MinIO",
			"fingerprint", qv.Fingerprint,
			"deleted_count", deletedCount)
	}

	// 3. 删除 Redis 进度和取消标志
	if s.redisClient != nil && qv.Fingerprint != "" {
		progressTracker := mvt.NewProgressTracker(s.redisClient, qv.Fingerprint)
		// 删除进度信息
		if err := progressTracker.DeleteProgress(ctx); err != nil {
			logger.L().Warn("Failed to delete progress from Redis", "error", err, "fingerprint", qv.Fingerprint)
		}
		// 同时删除取消标志
		if err := progressTracker.ClearCancelFlag(ctx); err != nil {
			logger.L().Warn("Failed to clear cancel flag from Redis", "error", err, "fingerprint", qv.Fingerprint)
		}
	}

	// 4. 删除数据库记录
	if err := s.repo.Delete(qv.ID); err != nil {
		return fmt.Errorf("failed to delete quick view: %w", err)
	}

	logger.L().Info("Quick view cleared",
		"engine_id", engineID,
		"table", fmt.Sprintf("%s.%s", schema, table),
		"fingerprint", qv.Fingerprint)

	return nil
}

// CancelQuickView 取消快显生成任务
func (s *QuickViewService) CancelQuickView(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) error {
	// 1. 获取快显记录
	qv, err := s.repo.GetByTable(tenantID, engineID, schema, table)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("快显记录不存在")
		}
		return fmt.Errorf("failed to get quick view: %w", err)
	}

	// 2. 检查状态是否为 generating
	if qv.Status != "generating" {
		return fmt.Errorf("只有 generating 状态的任务可以取消，当前状态: %s", qv.Status)
	}

	// 3. 更新状态为 cancelled
	if err := s.repo.UpdateStatus(qv.ID, "cancelled", "用户取消任务"); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	// 4. 设置 Redis 取消标志，通知 Worker 停止生成
	if s.redisClient != nil && qv.Fingerprint != "" {
		tracker := mvt.NewProgressTracker(s.redisClient, qv.Fingerprint)
		if err := tracker.SetCancelled(ctx); err != nil {
			// 改进：详细记录Redis错误，但不返回错误，因为：
			// 1. PG状态已改为cancelled
			// 2. Worker完成当前瓦片后会检查PG状态，最终一致
			// 3. 即使Redis失败，任务也会停止恢复
			logger.L().Error("Failed to set cancel flag in Redis",
				"error", err,
				"fingerprint", qv.Fingerprint,
				"engine_id", engineID,
				"table", fmt.Sprintf("%s.%s", schema, table))
			// 不返回错误，让取消操作继续，Worker会通过PG状态最终检查
		}
	}

	// 5. 尝试从 Asynq 队列删除任务
	if s.taskQueue != nil {
		if err := s.taskQueue.CancelQuickViewTask(ctx, qv.Fingerprint); err != nil {
			logger.L().Warn("Failed to cancel task from queue, may continue if already processing",
				"error", err,
				"fingerprint", qv.Fingerprint,
				"engine_id", engineID,
				"table", fmt.Sprintf("%s.%s", schema, table))
			// 不返回错误，因为：
			// 1. PG状态已改为cancelled
			// 2. 任务可能已被Worker获取（active状态），无法从队列删除
			// 3. Worker会检查PG状态发现cancelled，不会更新为ready
		}
	}

	logger.L().Info("Quick view cancelled",
		"engine_id", engineID,
		"table", fmt.Sprintf("%s.%s", schema, table),
		"fingerprint", qv.Fingerprint)

	return nil
}

// ResumeQuickView 恢复快显生成任务（增量生成）
func (s *QuickViewService) ResumeQuickView(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) error {
	// 1. 获取快显记录
	qv, err := s.repo.GetByTable(tenantID, engineID, schema, table)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("快显记录不存在")
		}
		return fmt.Errorf("failed to get quick view: %w", err)
	}

	// 2. 检查状态是否为 cancelled 或 failed
	if qv.Status != "cancelled" && qv.Status != "failed" {
		return fmt.Errorf("只有 cancelled 或 failed 状态的任务可以恢复，当前状态: %s", qv.Status)
	}

	// 3. 从Meta获取空间元数据（确保数据仍然有效）
	spatialMeta, err := s.GetSpatialMetadataFromMeta(ctx, tenantID, engineID, schema, table)
	if err != nil {
		return fmt.Errorf("failed to get spatial metadata: %w", err)
	}

	// 4. 更新状态为 generating 并设置 started_at
	err = s.repo.GetDB().Model(&models.QuickView{}).
		Where("id = ?", qv.ID).
		Updates(map[string]interface{}{
			"status":       "generating",
			"error_message": "",
			"started_at":    gorm.Expr("NOW()"),
		}).Error
	if err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	// 5. 清除 Redis 取消标志（Resume 是明确的重新执行操作）
	if s.redisClient != nil && qv.Fingerprint != "" {
		tracker := mvt.NewProgressTracker(s.redisClient, qv.Fingerprint)
		if err := tracker.ClearCancelFlag(ctx); err != nil {
			logger.L().Warn("Failed to clear cancel flag during resume",
				"error", err,
				"fingerprint", qv.Fingerprint)
			// 继续执行，不影响恢复流程
		} else {
			logger.L().Info("已清除取消标志，任务可以重新执行",
				"fingerprint", qv.Fingerprint)
		}
	}

	// 6. 重新入队任务（使用原有的配置）
	payload := worker.QuickViewTaskPayload{
		TenantID:        tenantID,
		EngineID:        engineID,
		SchemaName:      schema,
		TableName:       table,
		GeomColumn:      spatialMeta.GeomColumn,
		SRID:            spatialMeta.SRID,
		PrimaryKey:      spatialMeta.PrimaryKey,
		Extent:          spatialMeta.Extent,
		MinZoom:         *qv.MinZoom,
		MaxZoom:         qv.MaxZoom,
		Concurrency:     s.cfg.PreCache.Concurrency,
		Fingerprint:     qv.Fingerprint,
	}

	if err := s.taskQueue.EnqueueQuickViewTask(ctx, payload); err != nil {
		// 恢复失败，状态改回原状态
		s.repo.UpdateStatus(qv.ID, qv.Status, err.Error())
		return fmt.Errorf("failed to enqueue task: %w", err)
	}

	logger.L().Info("Quick view resumed",
		"engine_id", engineID,
		"table", fmt.Sprintf("%s.%s", schema, table),
		"fingerprint", qv.Fingerprint)

	return nil
}

// IncrementCachedTiles 增加缓存瓦片数（用于按需生成时更新统计）
func (s *QuickViewService) IncrementCachedTiles(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) error {
	return s.repo.IncrementCachedTiles(tenantID, engineID, schema, table)
}

// RunPreparationChecks 执行准备检查（仅诊断，不修改）
// 检查物化视图、空间索引和ANALYZE统计是否满足要求
func (s *QuickViewService) RunPreparationChecks(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) (*models.PreparationStatus, error) {
	// 1. 从Meta模块获取空间元数据（包括几何列名）
	spatialMeta, err := s.GetSpatialMetadataFromMeta(ctx, tenantID, engineID, schema, table)
	if err != nil {
		return nil, fmt.Errorf("failed to get spatial metadata: %w", err)
	}

	if spatialMeta == nil || spatialMeta.GeomColumn == "" {
		return nil, fmt.Errorf("geometry column not found in metadata")
	}

	// 2. 创建准备阶段服务
	resourceService := &systemClientResourceAdapter{
		systemClient: s.systemClient,
	}
	prepService := mvt.NewPreparationService(s.repo.GetDB(), resourceService)

	// 3. 执行所有检查，传递实际的几何列名
	prepStatus, err := prepService.RunPreparationChecks(
		ctx, tenantID, engineID, schema, table, spatialMeta.GeomColumn,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to run preparation checks: %w", err)
	}

	// 4. 如果诊断通过，创建快显表记录
	if prepStatus != nil && prepStatus.OverallStatus == "passed" {
		// 检查记录是否已存在
		exists, _ := s.repo.Exists(tenantID, engineID, schema, table)
		if !exists {
			// 创建新记录（包含 preparation_status 和空间范围）
			qv := models.QuickView{
				TenantID:           tenantID,
				EngineID:           engineID,
				SchemaName:         schema,
				Table:              table,
				Status:             "prepared",
				Fingerprint:        calculateFingerprint(engineID, schema, table),
				PreparationStatus:  prepStatus,
				Extent:             models.JSONFloatArray(spatialMeta.Extent),
				ExtentSRID:         spatialMeta.ExtentSRID,
				StartedAt:          &time.Time{},
				CompletedAt:        &time.Time{},
			}
			now := time.Now()
			qv.StartedAt = &now
			qv.CompletedAt = &now

			if err := s.repo.Create(&qv); err != nil {
				logger.L().Error("Failed to create quick view record", "error", err)
				// 不返回错误，只记录日志，诊断结果不受影响
			}
		}
	}

	return prepStatus, nil
}

// PrepareForCreateMVT 执行准备工作（启动异步任务）
// 创建物化视图、空间索引和执行ANALYZE
func (s *QuickViewService) PrepareForCreateMVT(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
) (string, error) {
	fingerprint := calculateFingerprint(engineID, schema, table)
	now := time.Now()

	// 1. 从Meta模块获取空间元数据（包括几何列名、extent等）
	spatialMeta, err := s.GetSpatialMetadataFromMeta(ctx, tenantID, engineID, schema, table)
	if err != nil {
		return "", fmt.Errorf("failed to get spatial metadata: %w", err)
	}

	if spatialMeta == nil || spatialMeta.GeomColumn == "" {
		return "", fmt.Errorf("geometry column not found in metadata")
	}

	// 2. 先创建或更新快显记录（status="preparing"，同时设置 extent）
	exists, _ := s.repo.Exists(tenantID, engineID, schema, table)

	if !exists {
		// 创建新记录，包含从 Meta 获取的 extent
		qv := models.QuickView{
			TenantID:    tenantID,
			EngineID:    engineID,
			SchemaName:  schema,
			Table:       table,
			Status:      "preparing",
			Fingerprint: fingerprint,
			StartedAt:   &now,
			// 从 Meta 同步 extent 信息
			Extent:     models.JSONFloatArray(spatialMeta.Extent),
			ExtentSRID: spatialMeta.ExtentSRID,
		}
		if err := s.repo.Create(&qv); err != nil {
			return "", fmt.Errorf("failed to create quick view record: %w", err)
		}
		logger.L().Info("✅ Created QuickView with extent from Meta",
			"table", fmt.Sprintf("%s.%s", schema, table),
			"extent", spatialMeta.Extent,
			"extent_srid", spatialMeta.ExtentSRID)
	} else {
		// 更新状态为准备中，同时更新 extent（以防元数据有更新）
		qv, err := s.repo.GetByTable(tenantID, engineID, schema, table)
		if err != nil {
			return "", fmt.Errorf("failed to get quick view record: %w", err)
		}

		// 使用 Updates 只更新必要字段，保护其他字段
		updates := map[string]interface{}{
			"status":      "preparing",
			"extent":      models.JSONFloatArray(spatialMeta.Extent),
			"extent_srid": spatialMeta.ExtentSRID,
		}
		if err := s.repo.GetDB().Model(&qv).Updates(updates).Error; err != nil {
			return "", fmt.Errorf("failed to update quick view: %w", err)
		}
		logger.L().Info("✅ Updated QuickView extent from Meta",
			"table", fmt.Sprintf("%s.%s", schema, table),
			"extent", spatialMeta.Extent,
			"extent_srid", spatialMeta.ExtentSRID)
	}

	// 3. 入队准备任务，传递实际的几何列名
	payload := worker.PrepareForCreateMVTTaskPayload{
		TenantID:    tenantID,
		EngineID:    engineID,
		SchemaName:  schema,
		TableName:   table,
		Fingerprint: fingerprint,
		GeomColumn:  spatialMeta.GeomColumn, // 传递实际的几何列名
	}

	err = s.taskQueue.EnqueuePrepareForCreateMVTTask(ctx, payload)
	if err != nil {
		// 准备失败，更新状态
		qv, _ := s.repo.GetByTable(tenantID, engineID, schema, table)
		if qv != nil {
			s.repo.UpdateStatus(qv.ID, "none", err.Error())
		}
		return "", fmt.Errorf("failed to enqueue prepare task: %w", err)
	}

	logger.L().Info("✅ Preparation task enqueued",
		"engine_id", engineID,
		"table", fmt.Sprintf("%s.%s", schema, table),
		"geom_column", spatialMeta.GeomColumn,
		"fingerprint", fingerprint)

	return fingerprint, nil
}

// calculateFingerprint 计算表的指纹（用于 MinIO 路径）
// 使用 common 模块的统一算法：SHA256(engineID:schema.table)
func calculateFingerprint(engineID uint, schema, table string) string {
	// 两步计算方式：先拼接 full_name，再计算指纹
	fullName := fmt.Sprintf("%s.%s", schema, table)
	return commonModels.GenerateItemFingerprint(engineID, fullName)
}

// UpdatePreferredMode 更新用户偏好的显示模式
func (s *QuickViewService) UpdatePreferredMode(
	ctx context.Context,
	tenantID, engineID uint,
	schema, table string,
	preferredMode string,
) error {
	// 1. 验证 preferredMode 参数
	if preferredMode != "geojson" && preferredMode != "mvt" {
		return fmt.Errorf("invalid preferred_mode: %s, must be 'geojson' or 'mvt'", preferredMode)
	}

	// 2. 调用 Repository 更新
	err := s.repo.UpdatePreferredMode(tenantID, engineID, schema, table, preferredMode)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("quick view record not found for engine_id=%d, schema=%s, table=%s",
				engineID, schema, table)
		}
		return fmt.Errorf("failed to update preferred mode: %w", err)
	}

	logger.L().Info("Preferred mode updated",
		"engine_id", engineID,
		"table", fmt.Sprintf("%s.%s", schema, table),
		"preferred_mode", preferredMode)

	return nil
}

// systemClientResourceAdapter 实现 mvt.ResourceService 接口
// 使用 SystemClient 获取引擎配置
type systemClientResourceAdapter struct {
	systemClient *commonClient.SystemClient
}

func (a *systemClientResourceAdapter) GetEngine(engineID, tenantID uint) (*commonModels.Engine, error) {
	// SystemClient.GetEngine 只需要 engineID（租户信息通过 token/auth 已经绑定）
	return a.systemClient.GetEngine(engineID)
}
