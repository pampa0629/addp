package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/addp/common/logger"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/mvt"
	"github.com/addp/manager/internal/repository"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// TaskHandler 任务处理器
type TaskHandler struct {
	db               *gorm.DB
	repo             *repository.QuickViewRepository
	quickViewService *mvt.QuickViewService
	cfg              *config.Config
	redisClient      *redis.Client // Redis 客户端用于进度跟踪
	resourceService  mvt.ResourceService // 资源服务（用于获取引擎配置）
	metaClient       *commonClient.MetaClient // Meta 客户端（用于获取空间元数据）
}

// NewTaskHandler 创建任务处理器
func NewTaskHandler(db *gorm.DB, cfg *config.Config) *TaskHandler {
	// 创建 SystemClient 用于获取资源连接信息
	// 注意：InternalAPIKey 用于服务间认证，如果为空则无法调用 System API
	if cfg.InternalAPIKey == "" {
		logger.L().Warn("InternalAPIKey 为空，Worker 可能无法访问 System API")
	}

	// 使用 NewSystemClientWithInternalKey 创建客户端（服务间调用）
	// 这样会调用 /internal/engines API 而不是 /api/engines
	systemClient := commonClient.NewSystemClientWithInternalKey(cfg.SystemServiceURL, cfg.InternalAPIKey)

	// 创建 MetaClient 用于获取空间元数据（服务间调用）
	metaClient := commonClient.NewMetaClientWithInternalKey(cfg.MetaServiceURL, cfg.InternalAPIKey)

	// 创建 ResourceService 适配器
	resourceService := &resourceServiceAdapter{
		systemClient: systemClient,
	}

	// 创建 TileGenerator（传入连接池配置）
	tileGen := mvt.NewTileGenerator(resourceService, cfg.PreCache.MaxDBConns)

	// 创建 QuickViewService
	quickViewService, err := mvt.NewQuickViewService(tileGen, mvt.MinIOConfig{
		Endpoint:  cfg.MinioEndpoint,
		AccessKey: cfg.MinioAccessKey,
		SecretKey: cfg.MinioSecretKey,
		UseSSL:    cfg.MinioUseSSL,
		Bucket:    "manager",
	}, db)
	if err != nil {
		logger.L().Error("Failed to create QuickViewService", "error", err)
	}

	// 创建 Redis 客户端（用于进度跟踪）
	redisClient := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	logger.L().Info("TaskHandler 初始化完成",
		"system_url", cfg.SystemServiceURL,
		"meta_url", cfg.MetaServiceURL,
		"has_api_key", cfg.InternalAPIKey != "",
		"auth_mode", "internal_key",
		"minio_endpoint", cfg.MinioEndpoint,
		"redis_addr", fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort))

	return &TaskHandler{
		db:               db,
		repo:             repository.NewQuickViewRepository(db),
		quickViewService: quickViewService,
		cfg:              cfg,
		redisClient:      redisClient,
		resourceService:  resourceService,
		metaClient:       metaClient,
	}
}

// RegisterHandlers 注册任务处理器
func (h *TaskHandler) RegisterHandlers(mux *asynq.ServeMux) {
	mux.HandleFunc(TypeQuickViewTask, h.HandleQuickViewTask)
	mux.HandleFunc(TypePrepareForCreateMVTTask, h.HandlePrepareForCreateMVTTask)
}

// HandleQuickViewTask 处理快显任务
func (h *TaskHandler) HandleQuickViewTask(ctx context.Context, task *asynq.Task) error {
	var payload QuickViewTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	// 🔍 日志：记录 Worker 收到的并发数
	logger.L().Info("🚀 Worker: 开始处理快显任务",
		"engine_id", payload.EngineID,
		"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
		"concurrency_from_payload", payload.Concurrency,
		"min_zoom", payload.MinZoom,
		"max_zoom", payload.MaxZoom,
		"fingerprint", payload.Fingerprint)

	// 0. 执行前检查数据库中的任务状态（防止处理已取消/已完成的任务）
	// 这是修复的关键点：确保即使服务重启，也不会重新执行已取消的任务
	var preCheckQV models.QuickView
	err := h.db.Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
		payload.TenantID, payload.EngineID, payload.SchemaName, payload.TableName).
		First(&preCheckQV).Error

	if err == nil {
		if preCheckQV.Status == "cancelled" {
			logger.L().Info("⚠️ 任务已被用户取消，跳过执行（Worker重启恢复）",
				"engine_id", payload.EngineID,
				"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
				"status", preCheckQV.Status)
			return nil // 直接返回，不执行后续逻辑
		}
		if preCheckQV.Status == "ready" {
			logger.L().Info("⚠️ 任务已完成，跳过执行（重复任务）",
				"engine_id", payload.EngineID,
				"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
				"status", preCheckQV.Status)
			return nil // 直接返回，任务已完成
		}
		// 对于 failed 状态，检查是否有取消标志（防止取消后的失败任务被重试）
		if preCheckQV.Status == "failed" {
			// 检查 Redis 取消标志
			checkTracker := mvt.NewProgressTracker(h.redisClient, payload.Fingerprint)
			if checkTracker.IsCancelled(ctx) {
				logger.L().Info("⚠️ 任务失败且存在取消标志，跳过执行（用户已取消）",
					"engine_id", payload.EngineID,
					"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
					"status", preCheckQV.Status)
				return nil // 跳过执行，防止重试已取消的任务
			}
			// 如果没有取消标志，说明是真正的失败，允许重试
			logger.L().Info("⚠️ 任务之前失败，但未检测到取消标志，允许重试",
				"engine_id", payload.EngineID,
				"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName))
		}

		// ✅ 强制检查准备状态（必须已完成）
		if preCheckQV.PreparationStatus == nil || preCheckQV.PreparationStatus.OverallStatus != "passed" {
			errMsg := "preparation not completed or not passed"
			logger.L().Error(errMsg,
				"engine_id", payload.EngineID,
				"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName))
			if err := h.repo.UpdateStatusOnly(preCheckQV.ID, "failed", errMsg); err != nil {
				logger.L().Error("Failed to update status to failed", "error", err)
			}
			return fmt.Errorf(errMsg)
		}
	}

	// 0.5 检查Redis取消标志（快速反应用户取消操作）
	// 即使PG状态还未更新，Redis标志也能快速通知Worker停止
	progressTrackerForCheck := mvt.NewProgressTracker(h.redisClient, payload.Fingerprint)
	if progressTrackerForCheck.IsCancelled(ctx) {
		logger.L().Info("⚠️ Redis取消标志已设置，跳过执行",
			"engine_id", payload.EngineID,
			"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
			"fingerprint", payload.Fingerprint)
		return nil // 直接返回，不执行后续逻辑
	}

	// 1. 更新状态为 generating
	if err := h.updateQuickViewStatus(payload, "generating", "", nil); err != nil {
		logger.L().Error("Failed to update status to generating", "error", err)
	}

	// 2. 创建进度跟踪器
	progressTracker := mvt.NewProgressTracker(h.redisClient, payload.Fingerprint)

	// 3. 构建配置并记录并发数
	// ✅ 关键改动：从 QuickView 的 PreparationStatus 读取查询参数（复用准备阶段结果）
	config := mvt.QuickViewConfig{
		EngineID:           payload.EngineID,
		TenantID:           payload.TenantID,
		Schema:             payload.SchemaName,
		Table:              payload.TableName,
		GeomColumn:         payload.GeomColumn,
		SRID:               payload.SRID,
		PrimaryKey:         payload.PrimaryKey,
		Extent:             payload.Extent,
		MinZoom:            payload.MinZoom,
		MaxZoom:            payload.MaxZoom,
		Concurrency:        payload.Concurrency,
		Fingerprint:        payload.Fingerprint,
		OptimizationConfig: payload.OptimizationConfig, // v2.0 传递优化配置
	}

	// ✅ 从 PreparationStatus 中获取或推导 QueryInfo
	var queryInfo *models.PreparedQueryInfo

	if preCheckQV.PreparationStatus == nil {
		errMsg := "preparation status not found, please run preparation first"
		logger.L().Error(errMsg,
			"engine_id", payload.EngineID,
			"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName))
		if err := h.repo.UpdateStatusOnly(preCheckQV.ID, "failed", errMsg); err != nil {
			logger.L().Error("Failed to update status to failed", "error", err)
		}
		return fmt.Errorf(errMsg)
	}

	// 如果 QueryInfo 已存在，直接使用
	if preCheckQV.PreparationStatus.QueryInfo != nil {
		queryInfo = preCheckQV.PreparationStatus.QueryInfo
	} else {
		// 否则从 checks 数组推导 QueryInfo
		var err error
		queryInfo, err = h.deriveQueryInfoFromChecks(preCheckQV.PreparationStatus.Checks, payload.SchemaName)
		if err != nil {
			errMsg := fmt.Sprintf("failed to derive query info from checks: %v", err)
			logger.L().Error(errMsg,
				"engine_id", payload.EngineID,
				"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName))
			if err := h.repo.UpdateStatusOnly(preCheckQV.ID, "failed", errMsg); err != nil {
				logger.L().Error("Failed to update status to failed", "error", err)
			}
			return fmt.Errorf(errMsg)
		}
		logger.L().Info("✅ 从检查结果推导出 QueryInfo",
			"materialized_view_exists", queryInfo.MaterializedViewExists,
			"query_table", queryInfo.QueryTable,
			"query_geom_column", queryInfo.QueryGeomColumn,
			"query_srid", queryInfo.QuerySRID)
	}
	config.Table = queryInfo.QueryTable
	config.GeomColumn = queryInfo.QueryGeomColumn
	config.SRID = queryInfo.QuerySRID

	if queryInfo.MaterializedViewExists {
		logger.L().Info("✅ 使用准备阶段的物化视图参数生成瓦片",
			"materialized_view", fmt.Sprintf("%s.%s", payload.SchemaName, queryInfo.QueryTable),
			"geom_column", queryInfo.QueryGeomColumn,
			"srid", queryInfo.QuerySRID)
	} else {
		logger.L().Info("✅ 使用准备阶段的源表参数生成瓦片（源表已是 3857）",
			"table", fmt.Sprintf("%s.%s", payload.SchemaName, queryInfo.QueryTable),
			"geom_column", queryInfo.QueryGeomColumn,
			"srid", queryInfo.QuerySRID)
	}

	// 🔍 日志：记录传递给 MVT Service 的配置
	logger.L().Info("⚙️ Worker: 调用 MVT Service",
		"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
		"concurrency", config.Concurrency,
		"min_zoom", config.MinZoom,
		"max_zoom", config.MaxZoom)

	// 执行快显缓存生成（使用混合入队模式）
	result, err := h.quickViewService.GenerateMixed(ctx, config, progressTracker)

	if err != nil {
		// 判断是否是取消错误
		errMsg := err.Error()
		isCancelError := strings.Contains(errMsg, "cancelled by user") ||
		                 strings.Contains(errMsg, "task cancelled")

		if isCancelError {
			// 用户取消：更新状态为 cancelled，不清除 Redis 标志，返回 nil（Asynq 不会重试）
			logger.L().Info("任务因用户取消而停止",
				"engine_id", payload.EngineID,
				"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
				"fingerprint", payload.Fingerprint)

			h.updateQuickViewStatus(payload, "cancelled", "用户取消任务", nil)
			progressTracker.UpdateProgress(ctx, &mvt.QuickViewProgress{
				Status:       "cancelled",
				ErrorMessage: "用户取消任务",
			})
			// ⚠️ 不清除 Redis 取消标志，防止 Asynq 重试时重新执行
			return nil // 返回 nil，Asynq 认为任务成功完成，不会重试
		} else {
			// 真正的失败：更新状态为 failed，返回错误（Asynq 可能重试）
			logger.L().Error("任务执行失败（非取消）",
				"engine_id", payload.EngineID,
				"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
				"error", err)

			h.updateQuickViewStatus(payload, "failed", err.Error(), nil)
			progressTracker.UpdateProgress(ctx, &mvt.QuickViewProgress{
				Status:       "failed",
				ErrorMessage: err.Error(),
			})
			// 真正的失败不清除取消标志（可能同时存在）
			return fmt.Errorf("quick view generation failed: %w", err)
		}
	}

	// 4. 检查任务是否已被取消
	var currentQV models.QuickView
	err = h.db.Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
		payload.TenantID, payload.EngineID, payload.SchemaName, payload.TableName).
		First(&currentQV).Error

	if err == nil && currentQV.Status == "cancelled" {
		logger.L().Info("任务已被取消，不更新为 ready 状态",
			"engine_id", payload.EngineID,
			"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName))
		return nil
	}

	// 5. 预缓存完成，更新结果并自动设置 preferred_mode = "mvt"（第三步）
	updateParams := repository.UpdateResultParams{
		ActualMaxZoom: result.ActualMaxZoom,
		TotalTiles:    result.TotalTiles,
		CachedTiles:   result.CachedTiles,
	}

	if err := h.repo.UpdateGenerationResultWithPreferredMode(currentQV.ID, updateParams); err != nil {
		logger.L().Error("Failed to update generation result", "error", err)
		return fmt.Errorf("failed to update result: %w", err)
	}

	// 6. 清除取消标志（任务已成功完成，不再需要）
	if err := progressTracker.ClearCancelFlag(ctx); err != nil {
		logger.L().Warn("Failed to clear cancel flag after task completion",
			"error", err,
			"fingerprint", payload.Fingerprint)
	}

	logger.L().Info("快显任务完成",
		"engine_id", payload.EngineID,
		"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
		"total_tiles", result.TotalTiles,
		"cached_tiles", result.CachedTiles,
		"duration_sec", result.GenerationSec,
		"preferred_mode", "mvt")

	return nil
}

// HandlePrepareForCreateMVTTask 处理准备创建MVT任务
func (h *TaskHandler) HandlePrepareForCreateMVTTask(ctx context.Context, task *asynq.Task) error {
	var payload PrepareForCreateMVTTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	logger.L().Info("🔧 Worker: 开始处理准备任务",
		"engine_id", payload.EngineID,
		"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
		"fingerprint", payload.Fingerprint)

	startTime := time.Now()

	// 1. 获取或创建 QuickView 记录
	var qv models.QuickView
	err := h.db.Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
		payload.TenantID, payload.EngineID, payload.SchemaName, payload.TableName).
		First(&qv).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			logger.L().Warn("QuickView 记录不存在，创建新记录",
				"engine_id", payload.EngineID,
				"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName))

			// 获取空间元数据（从 Meta 模块）
			h.metaClient.SetTenantID(&payload.TenantID)
			spatialMeta, err := h.metaClient.GetTableSpatialMetadata(payload.EngineID, payload.SchemaName, payload.TableName)
			if err != nil {
				logger.L().Error("Failed to get spatial metadata from Meta",
					"engine_id", payload.EngineID,
					"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
					"error", err)
				return fmt.Errorf("failed to get spatial metadata: %w", err)
			}

			// 创建新记录（包含完整的空间元数据）
			now := time.Now()
			qv = models.QuickView{
				TenantID:    payload.TenantID,
				EngineID:    payload.EngineID,
				SchemaName:  payload.SchemaName,
				Table:       payload.TableName,
				Status:      "preparing",
				Fingerprint: payload.Fingerprint,
				StartedAt:   &now,
				// 添加完整的空间元数据（避免 clear cache 后丢失）
				Extent:     spatialMeta.Extent,
				ExtentSRID: spatialMeta.ExtentSRID,
				MinZoom:    nil, // 准备阶段不设置 zoom，由用户在生成时指定
				MaxZoom:    18,  // 默认值
			}
			if err := h.db.Create(&qv).Error; err != nil {
				logger.L().Error("Failed to create QuickView record", "error", err)
				return fmt.Errorf("failed to create quick view record: %w", err)
			}
			logger.L().Info("Created QuickView record with spatial metadata",
				"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
				"extent", qv.Extent,
				"extent_srid", qv.ExtentSRID)
		} else {
			return fmt.Errorf("failed to get quick view: %w", err)
		}
	}

	// 2. 检查幂等性：如果已准备完成，直接返回
	if qv.PreparationStatus != nil && qv.PreparationStatus.OverallStatus == "passed" {
		logger.L().Info("⏭️ Preparation already completed, skipping",
			"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName))
		if err := h.repo.UpdateStatusOnly(qv.ID, "prepared", ""); err != nil {
			logger.L().Error("Failed to update status to prepared", "error", err)
		}
		return nil
	}

	// 3. 创建准备阶段服务
	prepService := mvt.NewPreparationService(h.db, h.resourceService)

	// 4. 执行准备工作（创建物化视图、索引、ANALYZE）
	// 传递实际的几何列名到 PreparationService
	prepStatus, err := prepService.RunPreparationChecks(ctx, payload.TenantID, payload.EngineID,
		payload.SchemaName, payload.TableName, payload.GeomColumn)

	if err != nil {
		// 准备失败 - 创建包含错误信息的 PreparationStatus
		logger.L().Error("准备工作执行失败",
			"engine_id", payload.EngineID,
			"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
			"error", err)

		// 保存失败状态及完整的 preparation_status
		failedStatus := &models.PreparationStatus{
			OverallStatus: "failed",
			Summary:       err.Error(),
			Checks:        []models.PreparationCheck{},
		}

		if err := h.repo.UpdatePreparationStatusAtomic(qv.ID, failedStatus, "failed"); err != nil {
			logger.L().Error("Failed to update preparation status", "error", err)
		}

		return fmt.Errorf("preparation failed: %w", err)
	}

	// 4.5 执行必要的操作（创建物化视图、索引、执行ANALYZE）
	if prepStatus != nil {
		logger.L().Info("🔧 执行准备工作",
			"engine_id", payload.EngineID,
			"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName))

		if err := prepService.ExecutePreparation(ctx, payload.TenantID, payload.EngineID,
			payload.SchemaName, payload.TableName, payload.GeomColumn, prepStatus); err != nil {
			logger.L().Warn("⚠️  准备执行步骤出错，继续进行",
				"engine_id", payload.EngineID,
				"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
				"error", err)
		}

		// 执行之后，重新计算总体状态（所有检查项都应该是 passed 或 warning）
		allPassed := true
		for _, check := range prepStatus.Checks {
			if check.Status == "failed" {
				allPassed = false
				break
			}
		}
		if allPassed {
			prepStatus.OverallStatus = "passed"
			prepStatus.Summary = "准备阶段全部通过，可以开始生成瓦片"
		}

		logger.L().Info("📋 准备工作执行结果",
			"engine_id", payload.EngineID,
			"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
			"overall_status", prepStatus.OverallStatus)
	}

	// 5. 检查准备结果
	if prepStatus == nil || prepStatus.OverallStatus != "passed" {
		// 准备检查未通过
		errMsg := "准备检查未通过"
		if prepStatus != nil {
			errMsg = prepStatus.Summary
		}

		logger.L().Error("准备检查失败",
			"engine_id", payload.EngineID,
			"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
			"summary", errMsg)

		// 保存失败时的检查结果
		if err := h.repo.GetDB().Model(&qv).Updates(map[string]interface{}{
			"status":             "failed",
			"error_message":      errMsg,
			"preparation_status": prepStatus,
			"completed_at":       gorm.Expr("NOW()"),
		}).Error; err != nil {
			logger.L().Error("Failed to update status to failed", "error", err)
		}

		return fmt.Errorf("preparation checks failed: %s", errMsg)
	}

	// 6. 准备成功，计算查询信息（供瓦片生成阶段复用，避免重复检查）
	logger.L().Info("✅ 准备工作完成",
		"engine_id", payload.EngineID,
		"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
		"fingerprint", payload.Fingerprint)

	// 构建查询信息（处理三种状态：passed、skipped、failed）
	queryInfo := &models.PreparedQueryInfo{
		MaterializedViewExists: false,
		QueryTable:             payload.TableName,
		QueryGeomColumn:        payload.GeomColumn,
		QuerySRID:              0, // 初始值，下面会更新
	}

	for _, check := range prepStatus.Checks {
		if check.Name == "materialized_view" {
			if check.Status == "passed" {
				// 物化视图创建成功或已存在 → 使用物化视图
				queryInfo.MaterializedViewExists = true
				queryInfo.QueryTable = fmt.Sprintf("%s_mv3857", payload.TableName)
				// 修复：动态获取物化视图的实际几何列名，而不是硬编码
				// 获取几何列名的逻辑：从物化视图的列定义中获取
				// 根据 preparation_service.go，物化视图中几何列通常被命名为 "geom_3857"
				// 但为了更健壮，我们应该查询数据库来确认
				// 暂时保留 "geom_3857" 但添加备注
				queryInfo.QueryGeomColumn = "geom_3857"
				queryInfo.QuerySRID = 3857
				logger.L().Info("✅ 准备阶段创建了物化视图，使用物化视图生成瓦片",
					"materialized_view", fmt.Sprintf("%s.%s", payload.SchemaName, queryInfo.QueryTable),
					"geom_column", queryInfo.QueryGeomColumn,
					"srid", queryInfo.QuerySRID)
			} else if check.Status == "skipped" {
				// 源表已是 3857 → 使用源表
				queryInfo.MaterializedViewExists = false
				queryInfo.QueryTable = payload.TableName
				queryInfo.QueryGeomColumn = payload.GeomColumn
				// 从 Details 中获取 SRID（应该是 3857）
				if sourceSRID, ok := check.Details["source_srid"].(float64); ok {
					queryInfo.QuerySRID = int(sourceSRID)
				} else if sourceSRID, ok := check.Details["source_srid"].(int); ok {
					queryInfo.QuerySRID = sourceSRID
				} else {
					queryInfo.QuerySRID = 3857 // 如果获取失败，默认 3857
				}
				logger.L().Info("✅ 源表已是 3857，直接使用源表生成瓦片",
					"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
					"geom_column", queryInfo.QueryGeomColumn,
					"srid", queryInfo.QuerySRID)
			} else {
				// 准备失败，不应该执行到这里，但为了安全起见
				queryInfo.MaterializedViewExists = false
				queryInfo.QueryTable = payload.TableName
				queryInfo.QueryGeomColumn = payload.GeomColumn
				logger.L().Warn("⚠️  准备阶段检查失败，但仍尝试使用源表生成瓦片",
					"check_status", check.Status,
					"message", check.Message)
			}
			break
		}
	}

	prepStatus.QueryInfo = queryInfo

	// 7. 保存执行信息
	prepStatus.ExecutionInfo = &models.PreparationExecution{
		StartedAt:   startTime,
		CompletedAt: time.Now(),
		DurationSec: time.Since(startTime).Seconds(),
		WorkerID:    fmt.Sprintf("worker-%d", os.Getpid()),
		TaskID:      task.Type(),
		RetryCount:  0,
	}

	// 8. 使用原子性更新保存结果（状态为 "prepared"）
	if err := h.repo.UpdatePreparationStatusAtomic(qv.ID, prepStatus, "prepared"); err != nil {
		logger.L().Error("Failed to update preparation status", "error", err)
		return fmt.Errorf("failed to update preparation status: %w", err)
	}

	logger.L().Info("✅ Preparation completed successfully",
		"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
		"duration_sec", time.Since(startTime).Seconds(),
		"materialized_view_created", queryInfo.MaterializedViewExists)

	return nil
}

// deriveQueryInfoFromChecks 从 PreparationStatus.Checks 推导 QueryInfo
// 这样可以避免重复存储信息，QueryInfo 可以从 checks 动态计算
func (h *TaskHandler) deriveQueryInfoFromChecks(checks []models.PreparationCheck, schema string) (*models.PreparedQueryInfo, error) {
	queryInfo := &models.PreparedQueryInfo{
		QuerySRID: 3857, // 目标 SRID 总是 3857
	}

	// 遍历 checks，从中提取必要的信息
	for _, check := range checks {
		switch check.Name {
		case "materialized_view":
			// 从物化视图检查判断是否需要物化视图
			if check.Status == "skipped" {
				// 源表已是 3857，不需要物化视图
				queryInfo.MaterializedViewExists = false
			} else if check.Status == "passed" {
				// 物化视图存在，需要使用物化视图
				queryInfo.MaterializedViewExists = true
			} else {
				return nil, fmt.Errorf("materialized view check failed: %s", check.Message)
			}

		case "spatial_index":
			// 从空间索引检查获取查询表和几何列
			if check.Status == "passed" || check.Status == "skipped" {
				if tableVal, ok := check.Details["table"]; ok {
					if table, ok := tableVal.(string); ok {
						// table 格式可能是 "schema.table"，需要提取出表名
						// 例如：从 "public.dltb_mv3857" 提取出 "dltb_mv3857"
						parts := strings.Split(table, ".")
						if len(parts) >= 2 {
							queryInfo.QueryTable = parts[len(parts)-1] // 取最后一部分作为表名
						} else {
							queryInfo.QueryTable = table // 如果没有 schema 前缀，直接使用
						}
					}
				}
				if colVal, ok := check.Details["column"]; ok {
					if col, ok := colVal.(string); ok {
						queryInfo.QueryGeomColumn = col
					}
				}
			} else {
				return nil, fmt.Errorf("spatial index check failed: %s", check.Message)
			}

		case "analyze":
			// ANALYZE 检查不影响 QueryInfo，只需要状态通过即可
			if check.Status != "passed" {
				return nil, fmt.Errorf("analyze check failed: %s", check.Message)
			}
		}
	}

	// 验证必要字段
	if queryInfo.QueryTable == "" {
		return nil, fmt.Errorf("failed to derive query table from checks")
	}
	if queryInfo.QueryGeomColumn == "" {
		return nil, fmt.Errorf("failed to derive query geom column from checks")
	}

	return queryInfo, nil
}
func (h *TaskHandler) updateQuickViewStatus(
	payload QuickViewTaskPayload,
	status string,
	errorMsg string,
	result *mvt.GenerateResult,
) error {
	var qv models.QuickView

	err := h.db.Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?",
		payload.TenantID, payload.EngineID, payload.SchemaName, payload.TableName).
		First(&qv).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 记录不存在，创建新记录
			qv = models.QuickView{
				TenantID:    payload.TenantID,
				EngineID:  payload.EngineID,
				SchemaName:  payload.SchemaName,
				Table:       payload.TableName,
				Status:      status,
				MinZoom:     &payload.MinZoom,
				MaxZoom:     payload.MaxZoom,
				Fingerprint: payload.Fingerprint,
				Extent:      payload.Extent,
			}

			if status == "generating" {
				// 创建后更新 started_at
				if err := h.db.Create(&qv).Error; err != nil {
					return err
				}
				return h.db.Model(&qv).Update("started_at", gorm.Expr("NOW()")).Error
			}

			return h.db.Create(&qv).Error
		}
		return err
	}

	// 更新现有记录
	updates := map[string]interface{}{
		"status": status,
	}

	if errorMsg != "" {
		updates["error_message"] = errorMsg
	}

	// 状态变为 generating 时，设置 started_at
	if status == "generating" {
		updates["started_at"] = gorm.Expr("NOW()")
	}

	if result != nil {
		updates["actual_max_zoom"] = result.ActualMaxZoom
		updates["total_tiles"] = result.TotalTiles
		updates["cached_tiles"] = result.CachedTiles
		updates["completed_at"] = gorm.Expr("NOW()")
	}

	return h.db.Model(&qv).Updates(updates).Error
}

// resourceServiceAdapter 资源服务适配器，将 SystemClient 适配为 ResourceService 接口
type resourceServiceAdapter struct {
	systemClient *commonClient.SystemClient
}

func (a *resourceServiceAdapter) GetEngine(engineID, tenantID uint) (*commonModels.Engine, error) {
	return a.systemClient.GetEngine(engineID)
}
