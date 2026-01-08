package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/addp/common/logger"
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
	MinZoom     *int   `json:"min_zoom"`      // 可选
	MaxZoom     int    `json:"max_zoom"`      // 默认18
	Concurrency int    `json:"concurrency"`   // 默认10
	Priority    string `json:"priority"`      // "critical", "default", "low"
}

// TriggerQuickView 触发快显缓存生成
// POST /api/engines/:id/spatial/:schema/:table/quick-view
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

	// 3. 获取租户ID（从context或JWT中）
	tenantID := uint(1) // TODO: 从JWT或context中获取
	if val, exists := c.Get("tenant_id"); exists {
		if tid, ok := val.(uint); ok {
			tenantID = tid
		}
	}

	// 4. 调用服务层
	params := service.TriggerQuickViewParams{
		TenantID:    tenantID,
		EngineID:  uint(engineID),
		SchemaName:  schema,
		TableName:   table,
		MinZoom:     req.MinZoom,
		MaxZoom:     req.MaxZoom,
		Concurrency: req.Concurrency,
		Priority:    req.Priority,
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
	}

	// 如果有进度信息，添加到响应中
	if progress != nil {
		response["progress"] = progress
	}

	c.JSON(http.StatusOK, response)
}

// ClearQuickView 清除快显缓存
// DELETE /api/engines/:id/spatial/:schema/:table/quick-view
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
