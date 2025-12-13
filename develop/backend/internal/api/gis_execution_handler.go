package api

import (
	"net/http"
	"strconv"

	"github.com/addp/develop/backend/internal/models"
	"github.com/addp/develop/backend/internal/service"
	"github.com/gin-gonic/gin"
)

// GISExecutionHandler GIS 执行处理器
type GISExecutionHandler struct {
	gisExecutionService *service.GISExecutionService
}

// NewGISExecutionHandler 创建 GIS 执行处理器
func NewGISExecutionHandler(gisExecutionService *service.GISExecutionService) *GISExecutionHandler {
	return &GISExecutionHandler{
		gisExecutionService: gisExecutionService,
	}
}

// ListExecutions 查询执行历史列表
// GET /api/gis-executions
func (h *GISExecutionHandler) ListExecutions(c *gin.Context) {
	// 从上下文获取租户ID
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id not found"})
		return
	}

	// 解析查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var taskID *uint
	if taskIDStr := c.Query("task_id"); taskIDStr != "" {
		if id, err := strconv.ParseUint(taskIDStr, 10, 32); err == nil {
			taskIDUint := uint(id)
			taskID = &taskIDUint
		}
	}

	req := &models.ListExecutionsRequest{
		Page:        page,
		PageSize:    pageSize,
		TaskID:      taskID,
		Status:      c.Query("status"),
		TriggerType: c.Query("trigger_type"),
		StartDate:   c.Query("start_date"),
		EndDate:     c.Query("end_date"),
	}

	// 查询执行列表
	response, err := h.gisExecutionService.ListExecutions(req, tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetExecution 查询执行详情
// GET /api/gis-executions/:id
func (h *GISExecutionHandler) GetExecution(c *gin.Context) {
	// 从上下文获取租户ID
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id not found"})
		return
	}

	// 解析执行ID
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid execution id"})
		return
	}

	// 查询执行详情
	execution, err := h.gisExecutionService.GetExecution(uint(id), tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "execution not found"})
		return
	}

	c.JSON(http.StatusOK, execution)
}

// GetExecutionLogs 查询执行日志
// GET /api/gis-executions/:id/logs
func (h *GISExecutionHandler) GetExecutionLogs(c *gin.Context) {
	// 从上下文获取租户ID
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id not found"})
		return
	}

	// 解析执行ID
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid execution id"})
		return
	}

	// 查询日志
	logs, err := h.gisExecutionService.GetExecutionLogs(uint(id), tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "execution not found"})
		return
	}

	// 检查是否下载
	if c.Query("download") == "true" {
		c.Header("Content-Disposition", "attachment; filename=execution_"+c.Param("id")+"_logs.txt")
		c.Header("Content-Type", "text/plain")
		c.String(http.StatusOK, logs)
		return
	}

	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// RetryExecution 重试失败的执行
// POST /api/gis-executions/:id/retry
func (h *GISExecutionHandler) RetryExecution(c *gin.Context) {
	// 从上下文获取用户信息
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user_id not found"})
		return
	}

	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id not found"})
		return
	}

	// 解析执行ID
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid execution id"})
		return
	}

	// 重试执行
	newExecution, err := h.gisExecutionService.RetryExecution(uint(id), userID.(uint), tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status":       "success",
		"message":      "execution retry started",
		"execution_id": newExecution.ID,
	})
}

// CancelExecution 取消运行中的执行
// POST /api/gis-executions/:id/cancel
func (h *GISExecutionHandler) CancelExecution(c *gin.Context) {
	// 从上下文获取租户ID
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id not found"})
		return
	}

	// 解析执行ID
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid execution id"})
		return
	}

	// 取消执行
	if err := h.gisExecutionService.CancelExecution(uint(id), tenantID.(uint)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "execution cancelled",
	})
}

// DeleteExecution 删除执行记录
// DELETE /api/gis-executions/:id
func (h *GISExecutionHandler) DeleteExecution(c *gin.Context) {
	// 从上下文获取租户ID
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id not found"})
		return
	}

	// 解析执行ID
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid execution id"})
		return
	}

	// 删除执行记录
	if err := h.gisExecutionService.DeleteExecution(uint(id), tenantID.(uint)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "execution deleted",
	})
}

// GetExecutionStatistics 获取执行统计信息
// GET /api/gis-executions/statistics
func (h *GISExecutionHandler) GetExecutionStatistics(c *gin.Context) {
	// 从上下文获取租户ID
	tenantID, exists := c.Get("tenant_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "tenant_id not found"})
		return
	}

	// 获取统计信息
	stats, err := h.gisExecutionService.GetExecutionStatistics(tenantID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "success",
		"statistics": stats,
	})
}
