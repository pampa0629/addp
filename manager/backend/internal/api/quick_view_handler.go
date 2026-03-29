package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/addp/common/logger"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/manager/internal/mvt"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// QuickViewHandler 快显API处理器
type QuickViewHandler struct {
	service     *service.QuickViewService
	redisClient *redis.Client // Redis 客户端用于查询进度
}

// NewQuickViewHandler 创建快显处理器
func NewQuickViewHandler(service *service.QuickViewService, redisClient *redis.Client) *QuickViewHandler {
	return &QuickViewHandler{
		service:     service,
		redisClient: redisClient,
	}
}

// TriggerQuickViewRequest 触发快显请求
type TriggerQuickViewRequest struct {
	MinZoom            *int                                `json:"min_zoom"`              // 可选
	MaxZoom            int                                 `json:"max_zoom"`              // 默认18
	Concurrency        int                                 `json:"concurrency"`           // 默认10
	Priority           string                              `json:"priority"`              // "critical", "default", "low"
	OptimizationConfig *commonmodels.OptimizationConfig    `json:"optimization_config,omitempty"` // v2.0 优化配置
}

// UpdatePreferredModeRequest 更新显示模式偏好请求
type UpdatePreferredModeRequest struct {
	PreferredMode string `json:"preferred_mode" binding:"required,oneof=geojson mvt"`
}

// TriggerQuickView 触发快显缓存生成
// POST /api/engines/:id/spatial/:schema/:table/quick-view
// @Summary TriggerQuickView
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /triggerquickview [get]
// @Security BearerAuth
func (h *QuickViewHandler) TriggerQuickView(c *gin.Context) {
	// 1. 解析路径参数
	engineID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	schema := c.Param("schema")
	table := c.Param("table")

	if schema == "" || table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schema and table are required"})
		return
	}

	// 2. 解析请求体（可选参数）
	var req TriggerQuickViewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 请求体为空也允许，使用默认值
		req = TriggerQuickViewRequest{}
	}

	// 🔍 日志：记录收到的并发数参数
	logger.L().Info("📥 API 收到快显触发请求",
		"engine_id", engineID,
		"schema", schema,
		"table", table,
		"concurrency_from_request", req.Concurrency,
		"priority", req.Priority)

	// 3. 获取租户ID（从context或JWT中）
	tenantID := uint(1) // TODO: 从JWT或context中获取
	if val, exists := c.Get("tenant_id"); exists {
		if tid, ok := val.(uint); ok {
			tenantID = tid
		}
	}

	// 4. 调用服务层
	params := service.TriggerQuickViewParams{
		TenantID:           tenantID,
		EngineID:           uint(engineID),
		SchemaName:         schema,
		TableName:          table,
		MinZoom:            req.MinZoom,
		MaxZoom:            req.MaxZoom,
		Concurrency:        req.Concurrency,
		Priority:           req.Priority,
		OptimizationConfig: req.OptimizationConfig, // v2.0 传递优化配置
	}

	if err := h.service.TriggerQuickView(c.Request.Context(), params); err != nil {
		logger.L().Error("Failed to trigger quick view", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Quick view task enqueued successfully",
		"status":  "generating",
	})
}

// GetQuickViewStatus 获取快显状态（包含实时进度）
// GET /api/engines/:id/spatial/:schema/:table/quick-view/status
// @Summary GetQuickViewStatus
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /getquickviewstatus [get]
// @Security BearerAuth
func (h *QuickViewHandler) GetQuickViewStatus(c *gin.Context) {
	// 1. 解析路径参数
	engineID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	schema := c.Param("schema")
	table := c.Param("table")

	// 2. 获取租户ID
	tenantID := uint(1) // TODO: 从JWT或context中获取
	if val, exists := c.Get("tenant_id"); exists {
		if tid, ok := val.(uint); ok {
			tenantID = tid
		}
	}

	// 3. 查询数据库中的状态
	qv, err := h.service.GetStatus(c.Request.Context(), tenantID, uint(engineID), schema, table)
	if err != nil {
		logger.L().Error("Failed to get quick view status", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 4. 如果状态是 generating，尝试从 Redis 获取实时进度
	var progress *mvt.QuickViewProgress
	if qv.Status == "generating" && h.redisClient != nil && qv.Fingerprint != "" {
		tracker := mvt.NewProgressTracker(h.redisClient, qv.Fingerprint)
		progress, _ = tracker.GetProgress(context.Background())
	}

	// 5. 合并响应
	response := gin.H{
		"id":               qv.ID,
		"engine_id":        qv.EngineID,
		"schema_name":      qv.SchemaName,
		"table_name":       qv.Table,
		"status":           qv.Status,
		"preferred_mode":   qv.PreferredMode,
		"error_message":    qv.ErrorMessage,
		"min_zoom":         qv.MinZoom,
		"max_zoom":         qv.MaxZoom,
		"actual_max_zoom":  qv.ActualMaxZoom,
		"total_tiles":      qv.TotalTiles,
		"cached_tiles":     qv.CachedTiles,
		"fingerprint":      qv.Fingerprint,
		"extent":           qv.Extent,
		"extent_srid":      qv.ExtentSRID,
		"started_at":       qv.StartedAt,
		"completed_at":     qv.CompletedAt,
		"created_at":       qv.CreatedAt,
		"updated_at":       qv.UpdatedAt,
		"preparation_status": qv.PreparationStatus, // v4.0 准备阶段状态
	}

	// 如果有进度信息，添加到响应中
	if progress != nil {
		response["progress"] = progress
	}

	c.JSON(http.StatusOK, response)
}

// ClearQuickView 清除快显缓存
// DELETE /api/engines/:id/spatial/:schema/:table/quick-view
// @Summary ClearQuickView
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /clearquickview [get]
// @Security BearerAuth
func (h *QuickViewHandler) ClearQuickView(c *gin.Context) {
	// 1. 解析路径参数
	engineID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	schema := c.Param("schema")
	table := c.Param("table")

	// 2. 获取租户ID
	tenantID := uint(1) // TODO: 从JWT或context中获取
	if val, exists := c.Get("tenant_id"); exists {
		if tid, ok := val.(uint); ok {
			tenantID = tid
		}
	}

	// 3. 清除缓存
	if err := h.service.ClearQuickView(c.Request.Context(), tenantID, uint(engineID), schema, table); err != nil {
		logger.L().Error("Failed to clear quick view", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Quick view cleared successfully",
	})
}

// CancelQuickView 取消快显生成任务
// POST /api/engines/:id/spatial/:schema/:table/pre-cache/cancel
// @Summary CancelQuickView
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /cancelquickview [get]
// @Security BearerAuth
func (h *QuickViewHandler) CancelQuickView(c *gin.Context) {
	// 1. 解析路径参数
	engineID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	schema := c.Param("schema")
	table := c.Param("table")

	// 2. 获取租户ID
	tenantID := uint(1) // TODO: 从JWT或context中获取
	if val, exists := c.Get("tenant_id"); exists {
		if tid, ok := val.(uint); ok {
			tenantID = tid
		}
	}

	// 3. 取消任务
	if err := h.service.CancelQuickView(c.Request.Context(), tenantID, uint(engineID), schema, table); err != nil {
		logger.L().Error("Failed to cancel quick view", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Quick view task cancelled successfully",
	})
}

// ResumeQuickView 恢复快显生成任务
// POST /api/engines/:id/spatial/:schema/:table/pre-cache/resume
// @Summary ResumeQuickView
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /resumequickview [get]
// @Security BearerAuth
func (h *QuickViewHandler) ResumeQuickView(c *gin.Context) {
	// 1. 解析路径参数
	engineID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	schema := c.Param("schema")
	table := c.Param("table")

	// 2. 获取租户ID
	tenantID := uint(1) // TODO: 从JWT或context中获取
	if val, exists := c.Get("tenant_id"); exists {
		if tid, ok := val.(uint); ok {
			tenantID = tid
		}
	}

	// 3. 恢复任务
	if err := h.service.ResumeQuickView(c.Request.Context(), tenantID, uint(engineID), schema, table); err != nil {
		logger.L().Error("Failed to resume quick view", "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Quick view task resumed successfully",
	})
}

// ListQuickViewTasks 列出所有快显任务
// GET /api/quick-view/tasks
// @Summary ListQuickViewTasks
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /listquickviewtasks [get]
// @Security BearerAuth
func (h *QuickViewHandler) ListQuickViewTasks(c *gin.Context) {
	// 1. 解析查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	status := c.Query("status")
	engineIDStr := c.Query("engine_id")

	var engineID uint
	if engineIDStr != "" {
		rid, err := strconv.ParseUint(engineIDStr, 10, 32)
		if err == nil {
			engineID = uint(rid)
		}
	}

	// 2. 获取租户ID
	tenantID := uint(1) // TODO: 从JWT或context中获取
	if val, exists := c.Get("tenant_id"); exists {
		if tid, ok := val.(uint); ok {
			tenantID = tid
		}
	}

	// 3. 查询任务列表
	params := repository.ListParams{
		Status:     status,
		EngineID: engineID,
		Page:       page,
		PageSize:   pageSize,
	}

	tasks, total, err := h.service.ListAll(c.Request.Context(), tenantID, params)
	if err != nil {
		logger.L().Error("Failed to list quick view tasks", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  tasks,
		"total": total,
		"page":  page,
		"page_size": pageSize,
	})
}

// GetStatistics 获取快显统计信息
// GET /api/quick-view/statistics
// @Summary GetStatistics
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /getstatistics [get]
// @Security BearerAuth
func (h *QuickViewHandler) GetStatistics(c *gin.Context) {
	// 1. 获取租户ID
	tenantID := uint(1) // TODO: 从JWT或context中获取
	if val, exists := c.Get("tenant_id"); exists {
		if tid, ok := val.(uint); ok {
			tenantID = tid
		}
	}

	// 2. 查询统计信息
	stats, err := h.service.GetStatistics(c.Request.Context(), tenantID)
	if err != nil {
		logger.L().Error("Failed to get statistics", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// UpdatePreferredMode 更新用户偏好的显示模式
// PATCH /api/manager/engines/:id/spatial/:schema/:table/pre-cache/mode
// @Summary UpdatePreferredMode
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /updatepreferredmode [get]
// @Security BearerAuth
func (h *QuickViewHandler) UpdatePreferredMode(c *gin.Context) {
	// 1. 解析路径参数
	engineID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	schema := c.Param("schema")
	table := c.Param("table")

	if schema == "" || table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schema and table are required"})
		return
	}

	// 2. 解析请求体
	var req UpdatePreferredModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body: " + err.Error()})
		return
	}

	// 3. 获取租户ID
	tenantID := uint(1)
	if val, exists := c.Get("tenant_id"); exists {
		if tid, ok := val.(uint); ok {
			tenantID = tid
		}
	}

	// 4. 调用服务层更新
	if err := h.service.UpdatePreferredMode(
		c.Request.Context(),
		tenantID,
		uint(engineID),
		schema,
		table,
		req.PreferredMode,
	); err != nil {
		logger.L().Error("Failed to update preferred mode", "error", err)
		statusCode := http.StatusInternalServerError
		if err.Error() == "quick view record not found" {
			statusCode = http.StatusNotFound
		} else if err.Error() == "invalid preferred_mode" {
			statusCode = http.StatusBadRequest
		}
		c.JSON(statusCode, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Preferred mode updated successfully",
		"preferred_mode": req.PreferredMode,
	})
}

// CheckPreparation 检查准备状态（诊断，如果通过则创建快显表记录）
// GET /api/manager/engines/:id/spatial/:schema/:table/pre-cache/check
// @Summary CheckPreparation
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /checkpreparation [get]
// @Security BearerAuth
func (h *QuickViewHandler) CheckPreparation(c *gin.Context) {
	// 1. 解析路径参数
	engineID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	schema := c.Param("schema")
	table := c.Param("table")

	if schema == "" || table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schema and table are required"})
		return
	}

	// 2. 获取租户ID
	tenantID := uint(1)
	if val, exists := c.Get("tenant_id"); exists {
		if tid, ok := val.(uint); ok {
			tenantID = tid
		}
	}

	// 3. 执行准备检查（如果通过，Service 层会自动创建快显表记录）
	prepStatus, err := h.service.RunPreparationChecks(
		c.Request.Context(),
		tenantID,
		uint(engineID),
		schema,
		table,
	)
	if err != nil {
		logger.L().Error("Failed to run preparation checks", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 4. 返回准备状态
	c.JSON(http.StatusOK, prepStatus)
}

// PrepareForCreateMVT 启动准备工作任务
// POST /api/manager/engines/:id/spatial/:schema/:table/pre-cache/prepare
// @Summary PrepareForCreateMVT
// @Tags Manager
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /prepareforcreatemvt [get]
// @Security BearerAuth
func (h *QuickViewHandler) PrepareForCreateMVT(c *gin.Context) {
	// 1. 解析路径参数
	engineID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid resource id"})
		return
	}

	schema := c.Param("schema")
	table := c.Param("table")

	if schema == "" || table == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "schema and table are required"})
		return
	}

	// 2. 获取租户ID
	tenantID := uint(1)
	if val, exists := c.Get("tenant_id"); exists {
		if tid, ok := val.(uint); ok {
			tenantID = tid
		}
	}

	// 3. 启动准备工作
	fingerprint, err := h.service.PrepareForCreateMVT(
		c.Request.Context(),
		tenantID,
		uint(engineID),
		schema,
		table,
	)
	if err != nil {
		logger.L().Error("Failed to start preparation", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 4. 返回任务信息
	c.JSON(http.StatusOK, gin.H{
		"status": "preparing",
		"message": "准备工作已启动",
		"fingerprint": fingerprint,
	})
}

