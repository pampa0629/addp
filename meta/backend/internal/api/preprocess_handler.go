package api

import (
	"net/http"
	"strconv"

	"github.com/addp/meta/internal/middleware"
	"github.com/addp/meta/internal/service"
	"github.com/addp/meta/internal/worker"
	"github.com/gin-gonic/gin"
)

// PreprocessHandler 预处理 HTTP 处理器
type PreprocessHandler struct {
	preprocessSvc *service.PreprocessService
	taskQueue     *worker.TaskQueue
}

// NewPreprocessHandler 创建预处理处理器
func NewPreprocessHandler(preprocessSvc *service.PreprocessService, taskQueue *worker.TaskQueue) *PreprocessHandler {
	return &PreprocessHandler{
		preprocessSvc: preprocessSvc,
		taskQueue:     taskQueue,
	}
}

// TriggerPreprocessRequest 触发预处理请求
type TriggerPreprocessRequest struct {
	Type     string `json:"type" binding:"required"` // "mvt_tiles", "vector_embedding"
	Priority string `json:"priority"`                // "critical", "default", "low"
}

// TriggerPreprocess 触发预处理任务
// POST /api/meta/preprocess/items/:item_id
func (h *PreprocessHandler) TriggerPreprocess(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	itemIDStr := c.Param("item_id")
	itemID, err := strconv.ParseUint(itemIDStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid item_id"})
		return
	}

	var req TriggerPreprocessRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 验证预处理类型
	if req.Type != "mvt_tiles" && req.Type != "vector_embedding" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid type, must be mvt_tiles or vector_embedding"})
		return
	}

	// 将任务加入队列（手动触发时不关联 scanRunID）
	ctx := c.Request.Context()

	var enqueueErr error
	if req.Priority != "" {
		enqueueErr = h.taskQueue.EnqueuePreprocessTaskWithPriority(ctx, uint(itemID), tenantID, req.Type, req.Priority, nil)
	} else {
		enqueueErr = h.taskQueue.EnqueuePreprocessTask(ctx, uint(itemID), tenantID, req.Type, nil)
	}

	if enqueueErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to enqueue preprocess task", "details": enqueueErr.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message":   "Preprocessing task enqueued",
		"item_id":   itemID,
		"tenant_id": tenantID,
		"type":      req.Type,
	})
}

// GetPreprocessStatus 查询预处理状态
// GET /api/meta/preprocess/items/:item_id/status
func (h *PreprocessHandler) GetPreprocessStatus(c *gin.Context) {
	// TODO: 实现查询预处理状态
	// 当前版本：直接查询 meta_item 的 preprocess_metadata 字段
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented yet"})
}

// ClearCache 清除预处理缓存
// DELETE /api/meta/preprocess/items/:item_id/cache
func (h *PreprocessHandler) ClearCache(c *gin.Context) {
	// TODO: 实现清除预处理缓存
	// 当前版本：调用 CacheService.DeleteAllTiles
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented yet"})
}
