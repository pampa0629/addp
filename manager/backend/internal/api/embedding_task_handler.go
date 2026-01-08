package api

import (
	"net/http"
	"strconv"

	"github.com/addp/manager/internal/service"
	"github.com/gin-gonic/gin"
)

// EmbeddingTaskHandler 向量化任务历史查询处理器
type EmbeddingTaskHandler struct {
	embeddingService *service.EmbeddingService
}

// NewEmbeddingTaskHandler 创建处理器
func NewEmbeddingTaskHandler(embeddingService *service.EmbeddingService) *EmbeddingTaskHandler {
	return &EmbeddingTaskHandler{
		embeddingService: embeddingService,
	}
}

// ListTasks GET /api/manager/embedding/tasks
// 查询参数：
// - engine_id: 引擎ID（可选）
// - page: 页码（默认1）
// - page_size: 每页数量（默认20）
func (h *EmbeddingTaskHandler) ListTasks(c *gin.Context) {
	tenantID := c.GetUint("tenant_id")

	// 解析查询参数
	var engineID *uint
	if engineIDStr := c.Query("engine_id"); engineIDStr != "" {
		if id, err := strconv.ParseUint(engineIDStr, 10, 32); err == nil {
			engineIDUint := uint(id)
			engineID = &engineIDUint
		}
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}

	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// 查询任务列表
	tasks, total, err := h.embeddingService.ListEmbeddingTasks(
		c.Request.Context(),
		engineID,
		&tenantID,
		page,
		pageSize,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "success",
		"data":      tasks,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetTask GET /api/manager/embedding/tasks/:task_id
func (h *EmbeddingTaskHandler) GetTask(c *gin.Context) {
	taskID := c.Param("task_id")

	task, err := h.embeddingService.GetEmbeddingTask(c.Request.Context(), taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"status":  "error",
			"message": "任务不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   task,
	})
}
