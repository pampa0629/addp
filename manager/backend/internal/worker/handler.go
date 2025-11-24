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
	"gorm.io/gorm"
)

// TaskHandler 任务处理器
type TaskHandler struct {
	db              *gorm.DB
	quickViewService *mvt.QuickViewService
	cfg             *config.Config
}

// NewTaskHandler 创建任务处理器
func NewTaskHandler(db *gorm.DB, cfg *config.Config) *TaskHandler {
	// 创建 SystemClient 用于获取资源连接信息
	systemClient := commonClient.NewSystemClient(cfg.SystemServiceURL, cfg.InternalAPIKey)

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
		Bucket:    "mvt-tiles",
	})
	if err != nil {
		logger.L().Error("Failed to create QuickViewService", "error", err)
	}

	return &TaskHandler{
		db:              db,
		quickViewService: quickViewService,
		cfg:             cfg,
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
		"resource_id", payload.ResourceID,
		"table", fmt.Sprintf("%s.%s", payload.SchemaName, payload.TableName))

	// 1. 更新状态为 generating
	if err := h.updateQuickViewStatus(payload, "generating", "", nil); err != nil {
		logger.L().Error("Failed to update status to generating", "error", err)
	}

	// 2. 执行快显缓存生成
	result, err := h.quickViewService.Generate(ctx, mvt.QuickViewConfig{
		ResourceID:      payload.ResourceID,
		TenantID:        payload.TenantID,
		Schema:          payload.SchemaName,
		Table:           payload.TableName,
		GeomColumn:      payload.GeomColumn,
		SRID:            payload.SRID,
		PrimaryKey:      payload.PrimaryKey,
		Extent:          payload.Extent,
		MinZoom:         payload.MinZoom,
		MaxZoom:         payload.MaxZoom,
		Concurrency:     payload.Concurrency,
		StopThresholdMs: payload.StopThresholdMs,
		StopThresholdKB: payload.StopThresholdKB,
		Fingerprint:     payload.Fingerprint,
	})

	if err != nil {
		// 更新状态为 failed
		h.updateQuickViewStatus(payload, "failed", err.Error(), nil)
		return fmt.Errorf("quick view generation failed: %w", err)
	}

	// 3. 更新状态为 ready
	if err := h.updateQuickViewStatus(payload, "ready", "", result); err != nil {
		logger.L().Error("Failed to update status to ready", "error", err)
	}

	logger.L().Info("快显任务完成",
		"resource_id", payload.ResourceID,
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

	err := h.db.Where("tenant_id = ? AND resource_id = ? AND schema_name = ? AND table_name = ?",
		payload.TenantID, payload.ResourceID, payload.SchemaName, payload.TableName).
		First(&qv).Error

	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// 记录不存在，创建新记录
			qv = models.QuickView{
				TenantID:    payload.TenantID,
				ResourceID:  payload.ResourceID,
				SchemaName:  payload.SchemaName,
				Table:       payload.TableName,
				Status:      status,
				MinZoom:     &payload.MinZoom,
				MaxZoom:     payload.MaxZoom,
				Fingerprint: payload.Fingerprint,
				Extent:      payload.Extent,
				StopThresholdTimeMs: payload.StopThresholdMs,
				StopThresholdSizeKB: payload.StopThresholdKB,
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

	if result != nil {
		updates["actual_max_zoom"] = result.ActualMaxZoom
		updates["total_tiles"] = result.TotalTiles
		updates["cached_tiles"] = result.CachedTiles
		updates["last_zoom_avg_time_ms"] = result.LastZoomAvgTimeMs
		updates["last_zoom_avg_size_kb"] = result.LastZoomAvgSizeKB
		updates["completed_at"] = gorm.Expr("NOW()")
	}

	return h.db.Model(&qv).Updates(updates).Error
}

// resourceServiceAdapter 资源服务适配器，将 SystemClient 适配为 ResourceService 接口
type resourceServiceAdapter struct {
	systemClient *commonClient.SystemClient
}

func (a *resourceServiceAdapter) GetResource(resourceID, tenantID uint) (*commonModels.Resource, error) {
	return a.systemClient.GetResource(resourceID)
}
