package worker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/addp/common/logger"
	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/mvt"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// TaskHandler 任务处理器
type TaskHandler struct {
	db               *gorm.DB
	quickViewService *mvt.QuickViewService
	cfg              *config.Config
	redisClient      *redis.Client // Redis 客户端用于进度跟踪
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

	// 创建 ResourceService 适配器
	resourceService := &resourceServiceAdapter{
		systemClient: systemClient,
	}

	// 创建 TileGenerator
	tileGen := mvt.NewTileGenerator(resourceService)

	// 创建 QuickViewService
	quickViewService, err := mvt.NewQuickViewService(tileGen, mvt.MinIOConfig{
		Endpoint:  cfg.MinioEndpoint,
		AccessKey: cfg.MinioAccessKey,
		SecretKey: cfg.MinioSecretKey,
		UseSSL:    cfg.MinioUseSSL,
		Bucket:    "manager",
	})
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
		"has_api_key", cfg.InternalAPIKey != "",
		"auth_mode", "internal_key",
		"minio_endpoint", cfg.MinioEndpoint,
		"redis_addr", fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort))

	return &TaskHandler{
		db:               db,
		quickViewService: quickViewService,
		cfg:              cfg,
		redisClient:      redisClient,
	}
}

// RegisterHandlers 注册任务处理器
func (h *TaskHandler) RegisterHandlers(mux *asynq.ServeMux) {
	mux.HandleFunc(TypeQuickViewTask, h.HandleQuickViewTask)
}

// HandleQuickViewTask 处理快显任务
func (h *TaskHandler) HandleQuickViewTask(ctx context.Context, task *asynq.Task) error {
	var payload QuickViewTaskPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	logger.L().Info("开始处理快显任务",
		"engine_id", payload.EngineID,
		"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName))

	// 1. 更新状态为 generating
	if err := h.updateQuickViewStatus(payload, "generating", "", nil); err != nil {
		logger.L().Error("Failed to update status to generating", "error", err)
	}

	// 2. 创建进度跟踪器
	progressTracker := mvt.NewProgressTracker(h.redisClient, payload.Fingerprint)

	// 3. 执行快显缓存生成（使用混合入队模式）
	result, err := h.quickViewService.GenerateMixed(ctx, mvt.QuickViewConfig{
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
	}, progressTracker)

	if err != nil {
		// 更新状态为 failed
		h.updateQuickViewStatus(payload, "failed", err.Error(), nil)
		// 更新进度为失败
		progressTracker.UpdateProgress(ctx, &mvt.QuickViewProgress{
			Status:       "failed",
			ErrorMessage: err.Error(),
		})
		return fmt.Errorf("quick view generation failed: %w", err)
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

	// 5. 更新状态为 ready
	if err := h.updateQuickViewStatus(payload, "ready", "", result); err != nil {
		logger.L().Error("Failed to update status to ready", "error", err)
	}

	logger.L().Info("快显任务完成",
		"engine_id", payload.EngineID,
		"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName),
		"total_tiles", result.TotalTiles,
		"cached_tiles", result.CachedTiles,
		"duration_sec", result.GenerationSec)

	return nil
}

// updateQuickViewStatus 更新快显状态
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
