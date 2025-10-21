package api

import (
	"net/http"
	"strconv"

	"github.com/addp/transfer/internal/service"
	"github.com/gin-gonic/gin"
)

// ExecutionHandler 执行记录管理 API Handler
type ExecutionHandler struct {
	executionService *service.ExecutionService
}

// NewExecutionHandler 创建 ExecutionHandler
func NewExecutionHandler(executionService *service.ExecutionService) *ExecutionHandler {
	return &ExecutionHandler{
		executionService: executionService,
	}
}

// GetExecution 获取执行记录详情
// GET /api/executions/:id
func (h *ExecutionHandler) GetExecution(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid execution ID"})
		return
	}

	tenantID := c.GetUint("tenant_id")

	execution, err := h.executionService.GetExecution(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Execution not found"})
		return
	}

	c.JSON(http.StatusOK, execution)
}

// ListExecutions 获取执行记录列表
// GET /api/executions?task_id=1&status=running&page=1&page_size=20
func (h *ExecutionHandler) ListExecutions(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	// 如果指定了 task_id，调用 GetTaskExecutions
	if taskIDStr := c.Query("task_id"); taskIDStr != "" {
		if taskID, err := strconv.ParseUint(taskIDStr, 10, 32); err == nil {
			executions, total, err := h.executionService.ListExecutions(c.Request.Context(), uint(taskID), tenantID, page, pageSize)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"data":       executions,
				"total":      total,
				"page":       page,
				"page_size":  pageSize,
				"total_page": (total + int64(pageSize) - 1) / int64(pageSize),
			})
			return
		}
	}

	// TODO: 实现全局执行列表（所有任务的执行记录）
	c.JSON(http.StatusNotImplemented, gin.H{"error": "Global execution listing not yet implemented. Please specify task_id parameter."})
}

// GetTaskExecutions 获取指定任务的执行记录
// GET /api/tasks/:id/executions
func (h *ExecutionHandler) GetTaskExecutions(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	tenantID := c.GetUint("tenant_id")

	// 分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	// 使用 ListExecutions 方法
	executions, total, err := h.executionService.ListExecutions(c.Request.Context(), uint(taskID), tenantID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":       executions,
		"total":      total,
		"page":       page,
		"page_size":  pageSize,
		"total_page": (total + int64(pageSize) - 1) / int64(pageSize),
	})
}

// CancelExecution 取消执行
// POST /api/executions/:id/cancel
func (h *ExecutionHandler) CancelExecution(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid execution ID"})
		return
	}

	tenantID := c.GetUint("tenant_id")

	if err := h.executionService.CancelExecution(c.Request.Context(), uint(id), tenantID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Execution cancelled successfully"})
}

// RetryExecution 重试失败的执行
// POST /api/executions/:id/retry
func (h *ExecutionHandler) RetryExecution(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid execution ID"})
		return
	}

	tenantID := c.GetUint("tenant_id")
	userID := c.GetUint("user_id")

	newExecution, err := h.executionService.RetryExecution(c.Request.Context(), uint(id), tenantID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, newExecution)
}

// GetExecutionProgress 获取执行进度
// GET /api/executions/:id/progress
func (h *ExecutionHandler) GetExecutionProgress(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid execution ID"})
		return
	}

	tenantID := c.GetUint("tenant_id")

	progress, err := h.executionService.GetExecutionProgress(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, progress)
}

// GetExecutionLogs 获取执行日志
// GET /api/executions/:id/logs
func (h *ExecutionHandler) GetExecutionLogs(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid execution ID"})
		return
	}

	tenantID := c.GetUint("tenant_id")

	// GetExecutionLogs 返回完整日志字符串
	logs, err := h.executionService.GetExecutionLogs(c.Request.Context(), uint(id), tenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// GetExecutionStatistics 获取执行统计
// GET /api/executions/statistics?task_id=1&start_time=2024-01-01&end_time=2024-12-31
func (h *ExecutionHandler) GetExecutionStatistics(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	filters := make(map[string]interface{})
	if taskID := c.Query("task_id"); taskID != "" {
		if id, err := strconv.ParseUint(taskID, 10, 32); err == nil {
			filters["task_id"] = uint(id)
		}
	}
	if startTime := c.Query("start_time"); startTime != "" {
		filters["start_time"] = startTime
	}
	if endTime := c.Query("end_time"); endTime != "" {
		filters["end_time"] = endTime
	}

	stats, err := h.executionService.GetExecutionStatistics(c.Request.Context(), tenantID, filters)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}
