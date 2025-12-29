package api

import (
	"net/http"
	"strconv"

	"github.com/addp/common/logger"
	"github.com/addp/manager/internal/repository"
	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// QuickViewHandler 快显API处理器
type QuickViewHandler struct {
	service *service.QuickViewService
}

// NewQuickViewHandler 创建快显处理器
func NewQuickViewHandler(service *service.QuickViewService) *QuickViewHandler {
	return &QuickViewHandler{
		service: service,
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

// GetQuickViewStatus 获取快显状态
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

	// 3. 查询状态
	qv, err := h.service.GetStatus(c.Request.Context(), tenantID, uint(engineID), schema, table)
	if err != nil {
		logger.L().Error("Failed to get quick view status", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, qv)
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
